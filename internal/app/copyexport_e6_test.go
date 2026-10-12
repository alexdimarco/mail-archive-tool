package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/util"
)

// The extraction workspace's slice E6 (Extract attachments, design
// docs/design-extraction-workspace.md §4.12, §10) lands catalog-first: each case starts
// here as a skip encoder and is replaced by its real test as the piece it covers is built.
// What stays here at the end is what an owner decision closes, each with the row it cites.
//
// The plan refuses Extract attachments until the owner rules on the test that pins the
// slice's earlier state (PlanCopyExport's R-h), so these tests turn the option on in the
// plan they make, as a run that was planned with it would carry it.

// e6Part is one part of a test message. body is the part's decoded content; it is written
// base64 unless raw is set, which is the encoded body exactly as it goes on the wire.
type e6Part struct {
	name  string // the file name, "" for none
	ctype string
	cte   string // the Content-Transfer-Encoding, "base64" when empty and raw is empty
	disp  string // "attachment" (the default) or "inline"
	cid   string
	body  []byte
	raw   string
}

// e6Mime is a multipart/mixed message with a short text body and the parts.
func e6Mime(id, subject string, parts ...e6Part) []byte {
	var b strings.Builder
	b.WriteString("From: A <a@example.com>\r\nTo: b@example.com\r\nSubject: " + subject + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n" +
		"Date: Sat, 28 Mar 2026 09:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"E6\"\r\n\r\n")
	b.WriteString("--E6\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nsee the attached files\r\n")
	for _, p := range parts {
		ct := p.ctype
		if ct == "" {
			ct = "application/octet-stream"
		}
		disp := p.disp
		if disp == "" {
			disp = "attachment"
		}
		b.WriteString("--E6\r\nContent-Type: " + ct)
		if p.name != "" {
			b.WriteString("; name=\"" + p.name + "\"")
		}
		b.WriteString("\r\nContent-Disposition: " + disp)
		if p.name != "" {
			b.WriteString("; filename=\"" + p.name + "\"")
		}
		if p.cid != "" {
			b.WriteString("\r\nContent-ID: <" + p.cid + ">")
		}
		switch {
		case p.raw != "":
			if p.cte != "" {
				b.WriteString("\r\nContent-Transfer-Encoding: " + p.cte)
			}
			b.WriteString("\r\n\r\n" + p.raw + "\r\n")
		default:
			b.WriteString("\r\nContent-Transfer-Encoding: base64\r\n\r\n" + e5Wrap(base64.StdEncoding.EncodeToString(p.body)) + "\r\n")
		}
	}
	b.WriteString("--E6--\r\n")
	return []byte(b.String())
}

// e6Run runs msgs with Extract attachments on (and Readable HTML when html is true), the
// bodies served by body.
func e6Run(t *testing.T, e *CopyExportEnv, msgs []CopyMessage, html bool, body func(id string) []byte) (*CopyResult, *CopyPlan) {
	t.Helper()
	src := newFakeSrc()
	src.body = body
	p := cePlan(t, e, src, msgs)
	p.Options.Attachments, p.Options.ReadableHTML = true, html
	return mustRun(t, e, p, src), p
}

// e6Derived is the derived-attachment files of one message line.
func e6Derived(line *ceMsgLine) []ceFileRec {
	var out []ceFileRec
	for _, f := range line.Files {
		if f.Role == roleDerivedAttach {
			out = append(out, f)
		}
	}
	return out
}

// e6Read reads a file of the export.
func e6Read(t *testing.T, root, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// e6Png is a few bytes that look like an inline image and are not text.
var e6Png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x01\x02\x03 inline image bytes \xff\xfe\x00")

// covers: MA-424, R4, R1, R29, S52
// A Microsoft 365 message with Extract attachments on: every parsed attachment, inline images
// included, is a loose decoded file under <stem>.attachments/, role derived-attachment, hashed as
// written, with sanitized FoldKey-unique names and the parsed part's bytes. A message with no
// attachment gets no directory. The record, the manifest and the sums list every file with its
// hash, and with Readable HTML on the page links the directory and no longer says the attachments
// are inside the .eml; with it off no page is made.
func TestExtractedAttachmentsAreLooseDecodedFilesUnderTheStem(t *testing.T) {
	pdf1, pdf2 := []byte("%PDF-1.4 first\r\n\x00\x01binary"), []byte("%PDF-1.4 second\r\n\xfe\xff")
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "with files",
			e6Part{name: "img1.png", ctype: "image/png", disp: "inline", cid: "img1", body: e6Png},
			e6Part{name: "report.pdf", ctype: "application/pdf", body: pdf1},
			e6Part{name: "Report.PDF", ctype: "application/pdf", body: pdf2}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "no files"),
	}
	run := func(html bool) (*CopyResult, map[int]*ceMsgLine) {
		e := newCEEnv(t)
		res, _ := e6Run(t, e, msgs, html, func(id string) []byte { return body[id] })
		if res.Counts.Exported != 2 || res.Outcome != CopyOutcomeComplete {
			t.Fatalf("%s, %+v", res.Label, res.Counts)
		}
		return res, e5Files(t, e, res)
	}

	res, lines := run(true)
	first := assure.Reached(t, lines[1], "the record line of the first message")
	derived := e6Derived(first)
	if len(first.Files) != 5 || first.Files[0].Role != roleOriginal || first.Files[4].Role != roleDerivedHTML || len(derived) != 3 {
		t.Fatalf("the first message's files: %+v", first.Files)
	}
	stem := strings.TrimSuffix(first.Files[0].Path, ".eml")
	want := []struct {
		name string
		body []byte
	}{{"img1", e6Png}, {"report.pdf", pdf1}, {"Report (2).PDF", pdf2}} // the walker names an inline image by its Content-ID
	for i, w := range want {
		f := derived[i]
		if f.Path != stem+".attachments/"+w.name {
			t.Errorf("file %d is %q, want %q", i, f.Path, stem+".attachments/"+w.name)
		}
		got := e6Read(t, res.Root, f.Path)
		sum := sha256.Sum256(got)
		if !bytes.Equal(got, w.body) {
			t.Errorf("%s: %d bytes, want the part's %d", w.name, len(got), len(w.body))
		}
		if f.SHA256 != hex.EncodeToString(sum[:]) || f.Size != int64(len(got)) {
			t.Errorf("%s: the record says %s / %d, the file is %x / %d", w.name, f.SHA256, f.Size, sum, len(got))
		}
	}
	// the bytes are the parsed parts' own
	parsed := source.ParseRFC822(body[msgs[0].SourceID])
	for i, a := range parsed.Attachments {
		var buf bytes.Buffer
		if _, err := a.WriteTo(&buf); err != nil || !bytes.Equal(buf.Bytes(), e6Read(t, res.Root, derived[i].Path)) {
			t.Errorf("attachment %d (%s): the file is not the parsed part's bytes (%v)", i, a.Filename, err)
		}
	}
	// the message with no attachment has no directory
	second := assure.Reached(t, lines[2], "the record line of the second message")
	if len(e6Derived(second)) != 0 || exists(res.Root, strings.TrimSuffix(second.Files[0].Path, ".eml")+".attachments") {
		t.Errorf("the second message has no attachment and gets no directory: %+v", second.Files)
	}
	for p := range treeFiles(t, res.Root) {
		if strings.Contains(p, ".attachments/") && !strings.HasPrefix(p, path.Dir(first.Files[0].Path)+"/") {
			t.Errorf("an extracted file outside the first message's month directory: %s", p)
		}
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	man := manifestOf(t, res.Root)
	if man["options"].(map[string]any)["extract_attachments"] != true {
		t.Errorf("the manifest's options: %v", man["options"])
	}
	files := man["messages"].([]any)[0].(map[string]any)["files"].([]any)
	if len(files) != 5 || files[2].(map[string]any)["role"] != roleDerivedAttach {
		t.Errorf("the manifest's files: %v", files)
	}
	page := string(e6Read(t, res.Root, first.Files[4].Path))
	if !strings.Contains(page, `<a href="`+path.Base(stem)+`.attachments/">extracted files</a>`) || strings.Contains(page, "attachments are inside the .eml file") {
		t.Errorf("the page does not link the extracted files:\n%s", page)
	}
	if p := string(e6Read(t, res.Root, second.Files[1].Path)); strings.Contains(p, "extracted files") {
		t.Errorf("a message with no attachment links a directory:\n%s", p)
	}

	// Readable HTML off: the originals and the files, no page
	res, lines = run(false)
	if f := lines[1].Files; len(f) != 4 || f[0].Role != roleOriginal || len(e6Derived(lines[1])) != 3 || len(treeFilesWith(t, res.Root, ".html")) != 0 {
		t.Errorf("with Readable HTML off: %+v", f)
	}
}

// e6Reserved reports whether name's base, as Windows reads it, is a device name. It is the test's
// own list, written apart from the product's.
func e6Reserved(name string) bool {
	head := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		head = name[:i]
	}
	head = strings.ToUpper(strings.TrimRight(head, " "))
	switch head {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$", "COM0", "LPT0", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return true
	}
	for _, p := range []string{"COM", "LPT"} {
		if len(head) == 4 && strings.HasPrefix(head, p) && head[3] >= '1' && head[3] <= '9' {
			return true
		}
	}
	return false
}

// covers: MA-424, R4, R29
// Hostile attachment names (.., separators, device names, bidi controls, 300 bytes of CJK, a
// name that differs only in case, none at all) become distinct, creatable, single-segment names
// inside <stem>.attachments/, each file holding its own part's bytes, and nothing is written
// outside the export root.
func TestExtractedAttachmentNamesAreSanitizedAndContained(t *testing.T) {
	names := []string{
		"../../escape.txt", `..\\..\\win.txt`, "/etc/passwd", "..", "CON", "COM1.txt", "NUL.txt", "CON .txt", "COM¹.txt",
		"invoice‮gpj.exe", strings.Repeat("漢", 100) + ".pdf", "trailing. ", "report.pdf", "REPORT.PDF", "",
	}
	var parts []e6Part
	for i, n := range names {
		parts = append(parts, e6Part{name: n, ctype: "application/octet-stream", body: []byte(fmt.Sprintf("body of part %d", i))})
	}
	msgs := ceMsgs(1)
	mime := e6Mime(msgs[0].SourceID, "hostile names", parts...)
	e := newCEEnv(t)
	res, p := e6Run(t, e, msgs, false, func(string) []byte { return mime })
	if res.Counts.Exported != 1 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	line := assure.Reached(t, e5Files(t, e, res)[1], "the record line")
	files := e6Derived(line)
	if len(files) != len(names) {
		t.Fatalf("%d files extracted, want %d: %+v", len(files), len(names), line.Notes)
	}
	attachDir := strings.TrimSuffix(line.Files[0].Path, ".eml") + ".attachments"
	seen := map[string]string{}
	for i, f := range files {
		base := path.Base(f.Path)
		if path.Dir(f.Path) != attachDir {
			t.Errorf("file %d (%q) is at %q, not directly in %s", i, names[i], f.Path, attachDir)
		}
		key := util.FoldKey(base)
		if other, dup := seen[key]; dup {
			t.Errorf("file %d (%q) and %q fold to one name %q", i, names[i], other, base)
		}
		seen[key] = base
		switch {
		case base == "" || base == "." || base == "..", strings.ContainsAny(base, "/\\\x00"), len(base) > 255,
			strings.HasSuffix(base, " "), strings.HasSuffix(base, "."), e6Reserved(base):
			t.Errorf("file %d (%q) became %q, which a filesystem cannot take or reads as something else", i, names[i], base)
		}
		for _, r := range base {
			if r < 0x20 || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
				t.Errorf("file %d (%q) became %q, which holds the control or bidi character %U", i, names[i], base, r)
			}
		}
		want := fmt.Sprintf("body of part %d", i)
		if got := string(e6Read(t, res.Root, f.Path)); got != want {
			t.Errorf("file %d (%q) holds %q, want %q", i, names[i], got, want)
		}
	}
	// nothing outside the export root: the destination holds only it
	entries, err := os.ReadDir(p.Dest)
	if err != nil || len(entries) != 1 || entries[0].Name() != res.RootName {
		t.Errorf("the destination holds %v (%v), want only %s", entries, err, res.RootName)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-424, R1
// A zero-byte part, a part whose stream fails halfway, one that panics and one with no stream at
// all each write no file and are an entry note that names the part and what holds it, never
// silent; the parts around them are extracted, no temp file is left, and a message whose only
// part is such a part has no directory. The message stays exported.
func TestAZeroByteOrFailedPartIsANoteNeverSilent(t *testing.T) {
	real := originalParser
	t.Cleanup(func() { originalParser = real })
	originalParser = func(b []byte) *model.Message {
		m := real(b)
		for i := range m.Attachments {
			switch m.Attachments[i].Filename {
			case "torn.bin":
				m.Attachments[i].WriteTo = func(w io.Writer) (int64, error) {
					n, _ := w.Write([]byte("half of it"))
					return int64(n), errors.New("the stream was torn")
				}
			case "panics.bin":
				m.Attachments[i].WriteTo = func(w io.Writer) (int64, error) { panic("a decoder fell over") }
			case "nothing.bin":
				m.Attachments[i].WriteTo = nil
			}
		}
		return m
	}
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "mixed",
			e6Part{name: "empty.txt", ctype: "text/plain"},
			e6Part{name: "ok.pdf", ctype: "application/pdf", body: []byte("%PDF fine")},
			e6Part{name: "torn.bin", body: []byte("whole")},
			e6Part{name: "panics.bin", body: []byte("whole")},
			e6Part{name: "nothing.bin", body: []byte("whole")}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "only empty", e6Part{name: "empty.txt", ctype: "text/plain"}),
	}
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, false, func(id string) []byte { return body[id] })
	if res.Counts.Exported != 2 || res.Counts.Failed != 0 {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	first := assure.Reached(t, lines[1], "the first message's line")
	files := e6Derived(first)
	if first.Status != statusExported || len(files) != 1 || path.Base(files[0].Path) != "ok.pdf" {
		t.Fatalf("the first message: %s, %+v", first.Status, first.Files)
	}
	for name, reason := range map[string]string{"empty.txt": "has no content", "torn.bin": "could not be read from the message (the stream was torn)",
		"panics.bin": "could not be read from the message (a decoder fell over", "nothing.bin": "nothing of it could be kept"} {
		var note string
		for _, n := range first.Notes {
			if strings.Contains(n, `"`+name+`"`) {
				note = n
			}
		}
		if note == "" || !strings.HasPrefix(note, attachNotePrefix) || !strings.Contains(note, reason) || !strings.Contains(note, ".eml beside it") {
			t.Errorf("%s: the note is %q, want one naming it, %q and what holds it", name, note, reason)
		}
		if exists(res.Root, path.Dir(files[0].Path)+"/"+name) {
			t.Errorf("%s: a file was written for it", name)
		}
	}
	if len(first.Notes) != 4 {
		t.Errorf("the first message's notes: %q", first.Notes)
	}
	// the directory holds the one good file, and no temp is left anywhere
	if got := treeFilesWith(t, res.Root, ".attachments/ok.pdf"); len(got) != 1 {
		t.Errorf("the attachments directory: %v", treeFiles(t, res.Root))
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("temp files left: %v", r)
	}
	// a message whose only part is empty: a note, no directory
	second := assure.Reached(t, lines[2], "the second message's line")
	if len(second.Files) != 1 || len(second.Notes) != 1 || !strings.Contains(second.Notes[0], "has no content") ||
		exists(res.Root, strings.TrimSuffix(second.Files[0].Path, ".eml")+".attachments") {
		t.Errorf("the second message: %+v, %q", second.Files, second.Notes)
	}
	// the notes are in the manifest
	man := manifestOf(t, res.Root)
	if n := man["messages"].([]any)[0].(map[string]any)["notes"].([]any); len(n) != 4 {
		t.Errorf("the manifest's notes: %v", n)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// e6Corpus reads a message of the fidelity corpus (testdata/mime).
func e6Corpus(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mime", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// covers: MA-424, R1
// A part the walker kept as .undecoded (a transfer encoding it does not decode) or .partial (a
// base64 part cut short) is extracted under that name, holding the bytes the walker kept, and is
// noted as not the complete decoded attachment, with what holds the message; the parts after it
// are extracted as usual and carry no note.
func TestKeptPartsAreExtractedAsWhatTheyAreAndNoted(t *testing.T) {
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Corpus(t, "f24-cte-x-uuencode.eml"),
		msgs[1].SourceID: e6Corpus(t, "f24-cte-base64-truncated.eml"),
	}
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, false, func(id string) []byte { return body[id] })
	if res.Counts.Exported != 2 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	// what the walker kept, by name, is what the file holds
	kept := func(id, name string) []byte {
		t.Helper()
		for _, a := range source.ParseRFC822(body[id]).Attachments {
			if a.Filename == name {
				var buf bytes.Buffer
				if _, err := a.WriteTo(&buf); err != nil {
					t.Fatal(err)
				}
				return buf.Bytes()
			}
		}
		t.Fatalf("the parser kept no %q", name)
		return nil
	}
	check := func(line *ceMsgLine, name string, want []byte) {
		t.Helper()
		for _, f := range e6Derived(line) {
			if path.Base(f.Path) == name {
				if got := e6Read(t, res.Root, f.Path); !bytes.Equal(got, want) {
					t.Errorf("%s holds %q, want the walker's %q", name, got, want)
				}
				return
			}
		}
		t.Errorf("no file %s among %+v", name, line.Files)
	}

	first := assure.Reached(t, lines[1], "the first message's line")
	undecoded := kept(msgs[0].SourceID, "note.txt.undecoded")
	if !bytes.HasPrefix(undecoded, []byte("begin 644 note.txt")) {
		t.Fatalf("the part as received is %q", undecoded)
	}
	check(first, "note.txt.undecoded", undecoded)
	check(first, "r.pdf", kept(msgs[0].SourceID, "r.pdf"))
	if len(first.Notes) != 1 || !strings.Contains(first.Notes[0], `"note.txt.undecoded"`) || !strings.Contains(first.Notes[0], "exactly as received, not a decoded attachment") ||
		!strings.Contains(first.Notes[0], ".eml beside it") {
		t.Errorf("the undecoded part's notes: %q", first.Notes)
	}

	second := assure.Reached(t, lines[2], "the second message's line")
	partial := kept(msgs[1].SourceID, "cut.pdf.partial")
	if !bytes.HasPrefix(partial, []byte("%PDF-1.4")) || len(partial) < 40 {
		t.Fatalf("the cut part's readable start is %q", partial)
	}
	check(second, "cut.pdf.partial", partial)
	if len(second.Notes) != 1 || !strings.Contains(second.Notes[0], `"cut.pdf.partial"`) || !strings.Contains(second.Notes[0], "only the readable start") ||
		!strings.Contains(second.Notes[0], "not the complete attachment") {
		t.Errorf("the cut part's notes: %q", second.Notes)
	}
}

// e6Disk is a destination of a fixed size: the free bytes are what is left of capacity after
// the bytes the files under dest hold.
func e6Disk(dest string, capacity int64) func(string) (uint64, error) {
	return func(string) (uint64, error) {
		var used int64
		_ = filepath.WalkDir(dest, func(_ string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if fi, ierr := d.Info(); ierr == nil {
					used += fi.Size()
				}
			}
			return nil
		})
		return uint64(max(capacity-used, 0)), nil
	}
}

// covers: MA-424, R29
// Each derived attachment is checked against the free space left (less the reserve) before it is
// written: on a destination with room for the original and the first of two 100 kB attachments,
// the first is extracted, the second is not written at all and is a note that says no space was
// left, and the original, the next message and the run are unharmed (the run is not stopped: the
// free-space stop is the originals').
func TestDerivedAttachmentsAreCheckedAgainstFreeSpace(t *testing.T) {
	big1, big2 := e5Image(100000), append([]byte("second"), e5Image(99994)...)
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "two big files",
			e6Part{name: "first.bin", body: big1}, e6Part{name: "second.bin", body: big2}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "after it"),
	}
	e := newCEEnv(t)
	src := newFakeSrc()
	src.body = func(id string) []byte { return body[id] }
	p := cePlan(t, e, src, msgs)
	p.Options.Attachments = true
	e.freeDest = e6Disk(p.Dest, p.Reserve+int64(len(body[msgs[0].SourceID])+len(body[msgs[1].SourceID]))+160000)
	res := mustRun(t, e, p, src)
	if res.Counts.Exported != 2 || res.Counts.Failed != 0 || res.Stop != nil {
		t.Fatalf("%s, %+v, stop %+v", res.Label, res.Counts, res.Stop)
	}
	lines := e5Files(t, e, res)
	first := assure.Reached(t, lines[1], "the first message's line")
	files := e6Derived(first)
	if first.Status != statusExported || len(files) != 1 || path.Base(files[0].Path) != "first.bin" || !bytes.Equal(e6Read(t, res.Root, files[0].Path), big1) {
		t.Fatalf("the first message: %s, %+v", first.Status, first.Files)
	}
	if len(first.Notes) != 1 || !strings.Contains(first.Notes[0], `"second.bin"`) || !strings.Contains(first.Notes[0], "no space was left") || !strings.Contains(first.Notes[0], "export this message again") {
		t.Errorf("the notes: %q", first.Notes)
	}
	if exists(res.Root, path.Dir(files[0].Path)+"/second.bin") {
		t.Error("the part that did not fit was written")
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("temp files left: %v", r)
	}
	if second := assure.Reached(t, lines[2], "the second message's line"); second.Status != statusExported || len(second.Files) != 1 {
		t.Errorf("the second message: %s, %+v", second.Status, second.Files)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-424, R4, R1, R29, S52
// An archive record without an original: the zip is copied byte-exact and its entries are also
// extracted within the bounds; a bomb entry is skipped with a note and never expanded.
func TestArchiveZipEntriesAreExtractedWithinBounds(t *testing.T) {
	t.Skip("MA-424: zip entry extraction is not built; lands in EW-E6")
}

// covers: MA-424, R29
// An archive record that has an original extracts from its .eml read back from the destination,
// as a Microsoft 365 message does.
func TestAnArchiveRecordWithAnOriginalExtractsFromItsEml(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// README and the manifest say what extracted files are and name the caveats.
func TestReadmeNamesTheExtractedAttachmentsAndTheirCaveats(t *testing.T) {
	t.Skip("MA-424: the README text is not written; lands in EW-E6")
}

// covers: MA-424, R29
// The plan sizes the derived files: the estimate and the reserve grow with the option on.
func TestThePlanSizesExtractedAttachments(t *testing.T) {
	t.Skip("MA-424: the plan does not size extracted attachments; lands in EW-E6")
}

// covers: MA-424, R12
// The plan accepts the option and carries it. Held for the owner: TestCopyExportPlanIsADryRun
// pins the plan's R-h refusal of it until slice E6 (rulings §0, rule 2).
func TestThePlanAcceptsExtractAttachments(t *testing.T) {
	t.Skip("MA-424: PlanCopyExport refuses the option (R-h) and TestCopyExportPlanIsADryRun asserts that; the owner rules on that test before it changes")
}

// covers: MA-425, R1, R29
// A Latin-1 text/csv attachment extracts byte-identical to the MIME part's transfer-decoded bytes,
// whichever transfer encoding carried it (base64, quoted-printable, 8-bit) and whether it is named
// as an attachment or inline: C1-1 (the v1 reader re-encoded a declared charset's bytes as UTF-8)
// is fixed by the walker, and this is the guard that keeps it so. The bytes are not valid UTF-8,
// so a conversion cannot pass for the original.
func TestALatin1CsvAttachmentExtractsByteIdentical(t *testing.T) {
	latin1 := []byte("name;city\r\ncaf\xe9;Z\xfcrich\r\nna\xefve;\xc5ngstr\xf6m\r\n")
	single := []byte("caf\xe9;Z\xfcrich;na\xefve")
	for _, b := range [][]byte{latin1, single} {
		if utf8.Valid(b) {
			t.Fatalf("the fixture %q is valid UTF-8: a conversion could not be told from the original", b)
		}
	}
	csv := "text/csv; charset=iso-8859-1"
	msgs := ceMsgs(1)
	mime := e6Mime(msgs[0].SourceID, "latin-1 files",
		e6Part{name: "base64.csv", ctype: csv, body: latin1},
		e6Part{name: "qp.csv", ctype: csv, cte: "quoted-printable", raw: "caf=E9;Z=FCrich;na=EFve"},
		e6Part{name: "eightbit.csv", ctype: csv, cte: "8bit", raw: string(single)},
		e6Part{name: "inline.csv", ctype: csv, disp: "inline", body: latin1},
		e6Part{name: "plain.txt", ctype: "text/plain; charset=windows-1252", body: single})
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, false, func(string) []byte { return mime })
	if res.Counts.Exported != 1 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	line := assure.Reached(t, e5Files(t, e, res)[1], "the record line")
	// an inline part keeps the name v1 gave it, its Content-ID or "inline" (F25, which README names)
	want := map[string][]byte{"base64.csv": latin1, "qp.csv": single, "eightbit.csv": single, "inline": latin1, "plain.txt": single}
	files := e6Derived(line)
	if len(files) != len(want) {
		t.Fatalf("%d files extracted, want %d: %+v", len(files), len(want), line.Files)
	}
	for _, f := range files {
		name := path.Base(f.Path)
		w, ok := want[name]
		if !ok {
			t.Errorf("an unexpected file %q", name)
			continue
		}
		if got := e6Read(t, res.Root, f.Path); !bytes.Equal(got, w) {
			t.Errorf("%s: the file holds %q, want the part's own bytes %q", name, got, w)
		}
	}
}
