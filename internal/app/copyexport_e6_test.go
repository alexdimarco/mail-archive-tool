package app

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
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

// e6ZipEntry is one entry of a test zip. By default it is deflated from data; store keeps it
// as it is. raw makes the entry a lie: the bytes are written as the entry's compressed data and
// the header says declared bytes of uncompressed data with the CRC of data, whatever they are.
type e6ZipEntry struct {
	name     string
	data     []byte
	store    bool
	raw      bool
	declared uint64
	crc      uint32 // the header's CRC for a raw entry; the CRC of data when zero
	flags    uint16
}

// e6Zip writes a zip of entries.
func e6Zip(t *testing.T, entries ...e6ZipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, en := range entries {
		var w io.Writer
		var err error
		switch {
		case en.raw:
			var packed bytes.Buffer
			fw, _ := flate.NewWriter(&packed, flate.BestCompression)
			fw.Write(en.data)
			fw.Close()
			crc := en.crc
			if crc == 0 {
				crc = crc32.ChecksumIEEE(en.data)
			}
			w, err = zw.CreateRaw(&zip.FileHeader{Name: en.name, Method: zip.Deflate, CRC32: crc, CompressedSize64: uint64(packed.Len()),
				UncompressedSize64: en.declared, Flags: en.flags})
			if err == nil {
				_, err = w.Write(packed.Bytes())
			}
		case en.store:
			w, err = zw.CreateHeader(&zip.FileHeader{Name: en.name, Method: zip.Store, Flags: en.flags})
			if err == nil {
				_, err = w.Write(en.data)
			}
		default:
			w, err = zw.CreateHeader(&zip.FileHeader{Name: en.name, Method: zip.Deflate, Flags: en.flags})
			if err == nil {
				_, err = w.Write(en.data)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// e6WithZip replaces the zip of the archive's "zip only" record, with the fixity the archive
// would have recorded for it, and exports it with Extract attachments on. wrap, when set, wraps
// the contained writer. It returns the run's result, its manifest entry and the key.
func e6WithZip(t *testing.T, a *ceArchive, e *CopyExportEnv, zb []byte) (*CopyResult, map[string]any, string) {
	t.Helper()
	_, _, zipPath := a.files("zip-only")
	if err := os.WriteFile(zipPath, zb, 0o644); err != nil {
		t.Fatal(err)
	}
	a.setRec("zip-only", func(r *state.Record) { r.Fixity.Zip = digestOf(zb) })
	s := a.open()
	key := a.key("zip-only")
	p := a.plan(e, s, []string{key})
	p.Options.Attachments = true
	res := a.run(e, p, s)
	return res, entryOf(t, manifestOf(t, res.Root), key), key
}

// e6Sizes is the sizes of extracted files by name, for a failure message that must not print them.
func e6Sizes(files map[string][]byte) map[string]int {
	out := map[string]int{}
	for name, b := range files {
		out[name] = len(b)
	}
	return out
}

// e6Extracted is the derived-attachment files of a manifest entry, by name, with their bytes.
func e6Extracted(t *testing.T, root string, ent map[string]any) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, f := range ent["files"].([]any) {
		if m := f.(map[string]any); m["role"] == roleDerivedAttach {
			p := m["path"].(string)
			out[path.Base(p)] = e6Read(t, root, p)
		}
	}
	return out
}

// e6Counting counts the bytes written into temp files of attachments directories, as a co-writer
// of the disk sees them: what an expansion costs before anything is checked afterwards.
type e6Counting struct {
	ceFS
	n *int64
}

type e6CountedFile struct {
	ceFile
	n *int64
}

func (f *e6CountedFile) Write(p []byte) (int, error) {
	k, err := f.ceFile.Write(p)
	*f.n += int64(k)
	return k, err
}

func (c *e6Counting) CreateTemp(dir string) (ceFile, string, error) {
	f, name, err := c.ceFS.CreateTemp(dir)
	if err == nil && strings.HasSuffix(dir, ".attachments") {
		f = &e6CountedFile{ceFile: f, n: c.n}
	}
	return f, name, err
}

// covers: MA-424, R4, R1, R29, S52
// An archive record without an original: the zip is copied byte-exact (role
// archived-attachments-zip, the archive's own hash) and its entries are also extracted, each
// flattened to its base name, sanitized and made unique, directory entries left out, each file
// holding its entry's bytes. The bounds hold: at most 1,000 entries; per-entry uncompressed and
// per-message total sizes (here scaled down: the code's defaults are the design's 1 GiB and 4
// GiB, which a test asserts directly); a compression ratio of at most 200:1, whether the header
// tells the truth about a bomb (skipped before a byte is written) or lies low (stopped at the
// size it states). A breach skips that entry with a note and the entries beside it are extracted;
// a damaged entry, an empty one and an encrypted one are notes too.
func TestArchiveZipEntriesAreExtractedWithinBounds(t *testing.T) {
	if b := (&CopyExportEnv{}).zipBounds(); b != (attachBounds{entries: 1000, entryBytes: 1 << 30, totalBytes: 4 << 30, ratio: 200}) {
		t.Errorf("the default bounds are %+v, want 1,000 entries, 1 GiB an entry, 4 GiB a message and 200:1", b)
	}
	note := func(t *testing.T, ent map[string]any, name, want string) {
		t.Helper()
		for _, n := range ent["notes"].([]any) {
			if s := n.(string); strings.Contains(s, `"`+name+`"`) && strings.Contains(s, want) && strings.HasPrefix(s, attachNotePrefix) && strings.Contains(s, "the zip beside it") {
				return
			}
		}
		t.Errorf("no note names %q and says %q: %v", name, want, ent["notes"])
	}
	// written is what the run wrote into attachments directories, expanded or not
	run := func(t *testing.T, e *CopyExportEnv, entries ...e6ZipEntry) (res *CopyResult, ent map[string]any, got map[string][]byte, written int64, zb []byte) {
		t.Helper()
		a := buildE6Archive(t)
		e.fsWrap = func(f ceFS) ceFS { return &e6Counting{ceFS: f, n: &written} }
		zb = e6Zip(t, entries...)
		res, ent, _ = e6WithZip(t, a, e, zb)
		if res.Counts.Exported != 1 || res.Status != CopyStatusComplete {
			t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
		}
		z := fileOf(ent, roleArchivedZip)
		if ent["status"] != statusWithoutOrig || z == nil || z["sha256"] != digestOf(zb).SHA256 || !bytes.Equal(e6Read(t, res.Root, z["path"].(string)), zb) {
			t.Errorf("the zip is not copied byte-exact under its role: %v", ent["files"])
		}
		if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
			t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
		}
		return res, ent, e6Extracted(t, res.Root, ent), written, zb
	}
	random := func(n int, seed byte) []byte { return append([]byte{seed}, e5Image(n-1)...) }

	t.Run("entries are flattened, sanitized and unique", func(t *testing.T) {
		report1, report2, evil, win, deep := []byte("%PDF one"), []byte("%PDF two"), []byte("evil"), []byte("win"), []byte("deep")
		_, ent, got, _, _ := run(t, newCEEnv(t),
			e6ZipEntry{name: "report.pdf", data: report1}, e6ZipEntry{name: "Report.PDF", data: report2},
			e6ZipEntry{name: "../../evil.txt", data: evil}, e6ZipEntry{name: `dir\sub\win.txt`, data: win},
			e6ZipEntry{name: "sub/dir/"}, e6ZipEntry{name: "a/b/deep.txt", data: deep, store: true}, e6ZipEntry{name: "CON", data: []byte("device")})
		want := map[string][]byte{"report.pdf": report1, "Report (2).PDF": report2, "evil.txt": evil, "win.txt": win, "deep.txt": deep, "_CON": []byte("device")}
		if len(got) != len(want) {
			t.Errorf("extracted %d files %v, want %d", len(got), e6Sizes(got), len(want))
		}
		for name, w := range want {
			if !bytes.Equal(got[name], w) {
				t.Errorf("%s holds %q, want %q", name, got[name], w)
			}
		}
		for _, n := range ent["notes"].([]any) {
			if strings.HasPrefix(n.(string), attachNotePrefix) {
				t.Errorf("a note for a clean zip: %v", n)
			}
		}
	})

	t.Run("a bomb whose header tells the truth is skipped before a byte is written", func(t *testing.T) {
		bomb := make([]byte, 20<<20) // zeros: about 1,000:1
		_, ent, got, written, zb := run(t, newCEEnv(t), e6ZipEntry{name: "bomb.bin", data: bomb}, e6ZipEntry{name: "ok.txt", data: []byte("fine")})
		if _, ok := got["bomb.bin"]; ok || string(got["ok.txt"]) != "fine" || len(got) != 1 {
			t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
		}
		note(t, ent, "bomb.bin", "zip bomb")
		if written > 1<<20 || len(zb) > 1<<20 {
			t.Errorf("%d bytes were written for a %d-byte zip: the bomb was expanded", written, len(zb))
		}
	})

	t.Run("a bomb whose header lies low is stopped at the size it states", func(t *testing.T) {
		_, ent, got, written, _ := run(t, newCEEnv(t), e6ZipEntry{name: "liar.bin", data: make([]byte, 20<<20), raw: true, declared: 10}, e6ZipEntry{name: "ok.txt", data: []byte("fine")})
		if _, ok := got["liar.bin"]; ok || string(got["ok.txt"]) != "fine" {
			t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
		}
		note(t, ent, "liar.bin", "its data does not match the size its header states")
		if written > 1<<10 {
			t.Errorf("%d bytes were written for an entry whose header says 10: it was expanded", written)
		}
	})

	t.Run("an entry over the per-entry bound is skipped and the rest are extracted", func(t *testing.T) {
		e := newCEEnv(t)
		e.zipLimit = attachBounds{entryBytes: 100 << 10}
		big, small := random(150<<10, 1), random(50<<10, 2)
		_, ent, got, written, _ := run(t, e, e6ZipEntry{name: "big.bin", data: big}, e6ZipEntry{name: "small.bin", data: small})
		if len(got) != 1 || !bytes.Equal(got["small.bin"], small) {
			t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
		}
		note(t, ent, "big.bin", "larger than 100 KiB uncompressed")
		if written > int64(len(small))+1<<10 {
			t.Errorf("%d bytes written, want only the small entry's", written)
		}
	})

	t.Run("an entry that would pass the per-message total is skipped", func(t *testing.T) {
		e := newCEEnv(t)
		e.zipLimit = attachBounds{totalBytes: 120 << 10}
		a1, a2, a3, tail := random(50<<10, 1), random(50<<10, 2), random(50<<10, 3), []byte("tail")
		_, ent, got, _, _ := run(t, e, e6ZipEntry{name: "a1.bin", data: a1}, e6ZipEntry{name: "a2.bin", data: a2}, e6ZipEntry{name: "a3.bin", data: a3}, e6ZipEntry{name: "tail.txt", data: tail})
		if len(got) != 3 || !bytes.Equal(got["a1.bin"], a1) || !bytes.Equal(got["a2.bin"], a2) || !bytes.Equal(got["tail.txt"], tail) {
			t.Errorf("extracted %v, want a1, a2 and the tail that still fits", e6Sizes(got))
		}
		note(t, ent, "a3.bin", "past 120 KiB")
	})

	t.Run("a zip with more than 1,000 entries has its first 1,000 extracted", func(t *testing.T) {
		var entries []e6ZipEntry
		for i := 0; i < 1002; i++ {
			entries = append(entries, e6ZipEntry{name: fmt.Sprintf("f%04d.txt", i), data: []byte(fmt.Sprintf("n%d", i)), store: true})
		}
		_, ent, got, _, _ := run(t, newCEEnv(t), entries...)
		if len(got) != 1000 || string(got["f0999.txt"]) != "n999" || got["f1000.txt"] != nil {
			t.Errorf("extracted %d files", len(got))
		}
		found := false
		for _, n := range ent["notes"].([]any) {
			found = found || (strings.Contains(n.(string), "1,002 entries") && strings.Contains(n.(string), "first 1,000"))
		}
		if !found {
			t.Errorf("no note says the zip has 1,002 entries and 1,000 were extracted: %v", ent["notes"])
		}
	})

	t.Run("a damaged, an empty and an encrypted entry are notes", func(t *testing.T) {
		_, ent, got, _, _ := run(t, newCEEnv(t),
			e6ZipEntry{name: "bad.bin", data: []byte("some data"), raw: true, declared: 9, crc: 12345},
			e6ZipEntry{name: "empty.txt", store: true},
			e6ZipEntry{name: "locked.bin", data: []byte("secret"), store: true, flags: 0x1},
			e6ZipEntry{name: "ok.txt", data: []byte("fine")})
		if len(got) != 1 || string(got["ok.txt"]) != "fine" {
			t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
		}
		note(t, ent, "bad.bin", "could not be read from the zip (its data does not match its checksum, so it is damaged)")
		note(t, ent, "empty.txt", "has no content")
		note(t, ent, "locked.bin", "encrypted")
	})
}

// e6Latin1CSV is a Latin-1 table that is not valid UTF-8.
var e6Latin1CSV = []byte("name;city\r\ncaf\xe9;Z\xfcrich\r\n")

// buildE6Archive is a real archive of two stores: "alice", captured with each message's original
// kept, holds a message with two attachments (a PDF and a Latin-1 table) and a plain one; "bob",
// captured without, holds a message with a PDF attachment (so it has a zip) and a plain one.
func buildE6Archive(t *testing.T) *ceArchive {
	t.Helper()
	src := tmpDir(t)
	alice, bob := filepath.Join(src, "alice"), filepath.Join(src, "bob")
	writeMaildirMsg(t, alice, "a1.eml", e6Mime("A1", "alice attach",
		e6Part{name: "report.pdf", ctype: "application/pdf", body: []byte("%PDF-1.4 alice\x00\x01")},
		e6Part{name: "data.csv", ctype: "text/csv; charset=iso-8859-1", body: e6Latin1CSV}))
	writeMaildirMsg(t, alice, "a2.eml", msg("plain original", "id2", "body two"))
	writeMaildirMsg(t, bob, "b1.eml", attachMsg("zip only", "id4"))
	writeMaildirMsg(t, bob, "b2.eml", msg("plain page", "id5", "body five"))
	a := &ceArchive{t: t, dir: tmpDir(t), src: src}
	a.capture(alice, true, export.Incremental)
	a.capture(bob, false, export.Incremental)
	return a
}

// covers: MA-424, MA-425, R29, R20, S52
// An archive record that has an original extracts from its .eml read back from the destination,
// as a Microsoft 365 message does: the files are under <stem>.attachments/ beside the copied
// original, hold the parts' own bytes (the Latin-1 table byte-identical), and the archive is not
// written by it.
func TestAnArchiveRecordWithAnOriginalExtractsFromItsEml(t *testing.T) {
	a := buildE6Archive(t)
	e := newCEEnv(t)
	s := a.open()
	before := treeDigest(t, a.dir, "search.db-wal", "search.db-shm")
	keys := []string{a.key("alice-attach"), a.key("plain-original")}
	p := a.plan(e, s, keys)
	p.Options.Attachments = true
	res := a.run(e, p, s)
	if res.Status != CopyStatusComplete || res.Counts.Exported != 2 {
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	man := manifestOf(t, res.Root)
	ent := entryOf(t, man, keys[0])
	var derived []map[string]any
	for _, f := range ent["files"].([]any) {
		if m := f.(map[string]any); m["role"] == roleDerivedAttach {
			derived = append(derived, m)
		}
	}
	orig := fileOf(ent, roleOriginal)
	if ent["status"] != "exported" || orig == nil || len(derived) != 2 {
		t.Fatalf("the first record: %v", ent)
	}
	attachDir := strings.TrimSuffix(orig["path"].(string), ".eml") + ".attachments/"
	want := map[string][]byte{"report.pdf": []byte("%PDF-1.4 alice\x00\x01"), "data.csv": e6Latin1CSV}
	for _, d := range derived {
		p := d["path"].(string)
		name := strings.TrimPrefix(p, attachDir)
		if w, ok := want[name]; !ok || !strings.HasPrefix(p, attachDir) || readText(t, res.Root, p) != string(w) {
			t.Errorf("%s: %q is not one of the parts' own bytes under %s", p, readText(t, res.Root, p), attachDir)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("not extracted: %v", want)
	}
	if plain := entryOf(t, man, keys[1]); len(plain["files"].([]any)) != 1 || plain["status"] != "exported" {
		t.Errorf("the record with no attachment: %v", plain)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	if got := treeDigest(t, a.dir, "search.db-wal", "search.db-shm"); got != before {
		t.Error("the export changed the archive")
	}
}

// covers: MA-424, R1
// README says, with Extract attachments on, how many files were extracted from how many
// messages, names the messages that have an attachment note, says what the files are (decoded
// conveniences, with .undecoded and .partial parts told apart) and names the caveats: parts
// dropped or unnamed, zips written before the walker that may hold re-encoded text, and PST
// forwarded-as-attachment stand-ins the export cannot detect. With the option off README says
// none of it. The sums verify with it in.
func TestReadmeNamesTheExtractedAttachmentsAndTheirCaveats(t *testing.T) {
	phrases := []string{"Extract attachments: 1 file from 1 message", "Messages with an attachment that was not extracted, or only part of it: 1",
		`subject M00001 (Inbox): "empty.txt" has no content`, "The files in a .attachments folder are made by MailArchive", ".undecoded", ".partial",
		"A part the reader dropped is not among them", "Content-ID", "re-encoded as UTF-8", "forwarded as an attachment inside an Outlook data file", "cannot tell you which ones"}
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "with files", e6Part{name: "ok.pdf", ctype: "application/pdf", body: []byte("%PDF fine")}, e6Part{name: "empty.txt", ctype: "text/plain"}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "plain"),
	}
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, true, func(id string) []byte { return body[id] })
	readme := readText(t, res.Root, copyReadmeName)
	for _, want := range phrases {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}

	// the same caveats where the files come from an archived zip
	res2, _, _ := e6WithZip(t, buildE6Archive(t), newCEEnv(t), e6Zip(t, e6ZipEntry{name: "r.pdf", data: []byte("%PDF")}))
	readme2 := readText(t, res2.Root, copyReadmeName)
	for _, want := range []string{"Extract attachments: 1 file from 1 message", "from the archived attachments zip", "re-encoded as UTF-8", "forwarded as an attachment inside an Outlook data file"} {
		if !strings.Contains(readme2, want) {
			t.Errorf("README of a zip export lacks %q", want)
		}
	}

	// the option off: none of it
	e3, src := newCEEnv(t), newFakeSrc()
	src.body = func(id string) []byte { return body[id] }
	res3 := mustRun(t, e3, cePlan(t, e3, src, msgs), src)
	readme3 := readText(t, res3.Root, copyReadmeName)
	for _, banned := range []string{"Extract attachments", ".attachments folder", "re-encoded as UTF-8", "Outlook data file (.pst) is kept"} {
		if strings.Contains(readme3, banned) {
			t.Errorf("README of a run without the option says %q", banned)
		}
	}
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
