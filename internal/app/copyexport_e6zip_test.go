package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/util"
)

// Extract attachments in zip mode (owner ruling 2026-10-12: one zip per message, the default,
// or loose files; catalog rows MA-533, MA-534, MA-535). Catalog-first: each case started as a
// skip encoder citing its row and is replaced by its real test as the piece it covers is built.
// Like slice E6's own tests, these turn the option on in the plan's options: the plan still
// refuses Extract attachments until the owner flips the gate (PlanCopyExport's R-h).

// e6zRun runs msgs with Extract attachments on in zip mode (and Readable HTML when html is true).
func e6zRun(t *testing.T, e *CopyExportEnv, msgs []CopyMessage, html bool, body func(id string) []byte) (*CopyResult, *CopyPlan) {
	t.Helper()
	src := newFakeSrc()
	src.body = body
	p := cePlan(t, e, src, msgs)
	p.Options.Attachments, p.Options.AttachmentsAsZip, p.Options.ReadableHTML = true, true, html
	return mustRun(t, e, p, src), p
}

// e6zRec is the derived attachments zip of one message line.
func e6zRec(line *ceMsgLine) (ceFileRec, bool) {
	for _, f := range line.Files {
		if f.Role == roleDerivedAttachZip {
			return f, true
		}
	}
	return ceFileRec{}, false
}

// e6zEntries reads a zip's entries in order, with their bytes.
func e6zEntries(t *testing.T, b []byte) ([]*zip.File, map[string][]byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("the attachments zip does not open: %v", err)
	}
	data := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		data[f.Name] = body
	}
	return zr.File, data
}

// e6zNoise is n bytes that do not compress, the same for a seed.
func e6zNoise(n int, seed uint64) []byte {
	b := make([]byte, n)
	x := seed*0x9e3779b97f4a7c15 + 1
	for i := range b {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		b[i] = byte(x >> 24)
	}
	return b
}

// e6zOnTemp calls onTemp as each temp file is made under messages/: the originals', and the zips'
// (the finalization files are made in the root).
type e6zOnTemp struct {
	ceFS
	onTemp func()
}

func (h *e6zOnTemp) CreateTemp(dir string) (ceFile, string, error) {
	if strings.HasPrefix(dir, "messages") {
		h.onTemp()
	}
	return h.ceFS.CreateTemp(dir)
}

// covers: MA-533, R4, R1, R29, S52
// A Microsoft 365 message with Extract attachments on in zip mode: every parsed attachment, inline
// images included, is an entry of ONE <stem>.attachments.zip beside the original, role
// derived-attachments-zip, hashed as written, and there is no .attachments directory and no loose
// file; a message with no attachment gets no zip. The record, the manifest and the sums list the
// zip with its own hash, and the entries hold the parsed parts' bytes under the loose mode's names.
func TestZipModeWritesOneZipPerMessageAndNoLooseFiles(t *testing.T) {
	pdf1, pdf2 := []byte("%PDF-1.4 first\r\n\x00\x01binary"), []byte("%PDF-1.4 second\r\n\xfe\xff")
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "with files",
			e6Part{name: "img1.png", ctype: "image/png", disp: "inline", cid: "img1", body: e6Png},
			e6Part{name: "report.pdf", ctype: "application/pdf", body: pdf1},
			e6Part{name: "Report.PDF", ctype: "application/pdf", body: pdf2}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "no files"),
	}
	e := newCEEnv(t)
	res, _ := e6zRun(t, e, msgs, false, func(id string) []byte { return body[id] })
	if res.Counts.Exported != 2 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	first := assure.Reached(t, lines[1], "the record line of the first message")
	if len(first.Files) != 2 || first.Files[0].Role != roleOriginal || first.Files[1].Role != roleDerivedAttachZip || len(e6Derived(first)) != 0 {
		t.Fatalf("the first message's files: %+v", first.Files)
	}
	stem := strings.TrimSuffix(first.Files[0].Path, ".eml")
	zrec := first.Files[1]
	if zrec.Path != stem+".attachments.zip" {
		t.Errorf("the zip is %q, want %q", zrec.Path, stem+".attachments.zip")
	}
	zb := e6Read(t, res.Root, zrec.Path)
	sum := sha256.Sum256(zb)
	if zrec.SHA256 != hex.EncodeToString(sum[:]) || zrec.Size != int64(len(zb)) {
		t.Errorf("the record says %s / %d, the zip is %x / %d", zrec.SHA256, zrec.Size, sum, len(zb))
	}
	files, data := e6zEntries(t, zb)
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if want := []string{"img1", "report.pdf", "Report (2).PDF"}; strings.Join(names, "|") != strings.Join(want, "|") { // the walker names an inline image by its Content-ID
		t.Errorf("the entries are %q, want %q", names, want)
	}
	if !bytes.Equal(data["img1"], e6Png) || !bytes.Equal(data["report.pdf"], pdf1) || !bytes.Equal(data["Report (2).PDF"], pdf2) {
		t.Errorf("an entry does not hold its part's bytes")
	}
	// the bytes are the parsed parts' own
	for i, a := range source.ParseRFC822(body[msgs[0].SourceID]).Attachments {
		var buf bytes.Buffer
		if _, err := a.WriteTo(&buf); err != nil || !bytes.Equal(buf.Bytes(), data[names[i]]) {
			t.Errorf("attachment %d (%s): the entry is not the parsed part's bytes (%v)", i, a.Filename, err)
		}
	}
	// no directory and no loose file, and none for the message with no attachment
	for p := range treeFiles(t, res.Root) {
		if strings.Contains(p, ".attachments/") {
			t.Errorf("a loose file in zip mode: %s", p)
		}
	}
	if exists(res.Root, stem+".attachments") {
		t.Error("an attachments directory was made in zip mode")
	}
	second := assure.Reached(t, lines[2], "the record line of the second message")
	if len(second.Files) != 1 || exists(res.Root, strings.TrimSuffix(second.Files[0].Path, ".eml")+".attachments.zip") {
		t.Errorf("the second message has no attachment and gets no zip: %+v", second.Files)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("temp files left: %v", r)
	}
	man := manifestOf(t, res.Root)
	mf := man["messages"].([]any)[0].(map[string]any)["files"].([]any)
	if len(mf) != 2 || mf[1].(map[string]any)["role"] != "derived-attachments-zip" {
		t.Errorf("the manifest's files: %v", mf)
	}
	if opts := man["options"].(map[string]any); opts["extract_attachments"] != true || opts["attachments_as_zip"] != true {
		t.Errorf("the manifest's options: %v", opts)
	}

	// the twin: with the zip switch off the same message is MA-424's loose files, and no zip
	e2 := newCEEnv(t)
	res2, _ := e6Run(t, e2, msgs, false, func(id string) []byte { return body[id] })
	l2 := assure.Reached(t, e5Files(t, e2, res2)[1], "the loose run's line")
	if _, ok := e6zRec(l2); ok || len(e6Derived(l2)) != 3 {
		t.Errorf("loose mode: %+v", l2.Files)
	}
}

// covers: MA-533, R4, R29
// The entries are named as loose files are: hostile names (.., separators, device names, bidi
// controls, 300 bytes of CJK, a name that differs only in case, none at all) become distinct,
// single-segment, FoldKey-unique names with no directory entry; each entry holds its own part's
// bytes; and the zip of one message is the same bytes every time (a fixed timestamp per entry, the
// message's own date). A message of 1,205 attachments is still one file, with 1,205 entries.
func TestZipModeEntriesAreNamedLikeLooseFilesAndTheZipIsReproducible(t *testing.T) {
	names := []string{
		"../../escape.txt", `..\\..\\win.txt`, "/etc/passwd", "..", "CON", "COM1.txt", "NUL.txt", "CON .txt", "COM¹.txt",
		"invoice‮gpj.exe", strings.Repeat("漢", 100) + ".pdf", "trailing. ", "report.pdf", "REPORT.PDF", "", "", "", "",
		"a/b/c.txt", "dir/", "x:y.txt", "Straße.txt", "STRASSE.TXT",
	}
	var parts []e6Part
	for i, n := range names {
		parts = append(parts, e6Part{name: n, disp: "attachment", body: []byte(fmt.Sprintf("body of part %d", i))})
	}
	msgs := ceMsgs(1)
	mime := e6Mime(msgs[0].SourceID, "hostile names", parts...)
	var sums []string
	for run := 0; run < 2; run++ {
		e := newCEEnv(t)
		res, p := e6zRun(t, e, msgs, false, func(string) []byte { return mime })
		if res.Counts.Exported != 1 || res.Outcome != CopyOutcomeComplete {
			t.Fatalf("%s, %+v", res.Label, res.Counts)
		}
		line := assure.Reached(t, e5Files(t, e, res)[1], "the record line")
		zrec, ok := e6zRec(line)
		if !ok {
			t.Fatalf("no zip: %+v %q", line.Files, line.Notes)
		}
		sums = append(sums, zrec.SHA256)
		if run == 1 {
			break
		}
		files, data := e6zEntries(t, e6Read(t, res.Root, zrec.Path))
		if len(files) != len(names) {
			t.Fatalf("%d entries, want %d: %q", len(files), len(names), line.Notes)
		}
		seen := map[string]string{}
		for i, f := range files {
			base := f.Name
			if strings.ContainsAny(base, "/\\\x00") || f.FileInfo().IsDir() {
				t.Errorf("entry %d (%q) is %q: a path or a directory", i, names[i], base)
			}
			key := util.FoldKey(base)
			if other, dup := seen[key]; dup {
				t.Errorf("entry %d (%q) and %q fold to one name %q", i, names[i], other, base)
			}
			seen[key] = base
			switch {
			case base == "" || base == "." || base == "..", len(base) > 255, strings.HasSuffix(base, " "), strings.HasSuffix(base, "."), e6Reserved(base):
				t.Errorf("entry %d (%q) became %q, which a filesystem cannot take once the zip is opened", i, names[i], base)
			}
			for _, r := range base {
				if r < 0x20 || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
					t.Errorf("entry %d (%q) became %q, which holds the control or bidi character %U", i, names[i], base, r)
				}
			}
			if want := fmt.Sprintf("body of part %d", i); string(data[base]) != want {
				t.Errorf("entry %d (%q) holds %q, want %q", i, names[i], data[base], want)
			}
			if !f.Modified.Equal(files[0].Modified) || f.Method != zip.Deflate {
				t.Errorf("entry %d: modified %v, method %d", i, f.Modified, f.Method)
			}
			if d := f.Modified.Sub(msgs[0].Received); d < -2*time.Second || d > 2*time.Second {
				t.Errorf("entry %d is dated %v, not the message's %v", i, f.Modified, msgs[0].Received)
			}
		}
		if entries, err := os.ReadDir(p.Dest); err != nil || len(entries) != 1 || entries[0].Name() != res.RootName {
			t.Errorf("the destination holds %v (%v), want only %s", entries, err, res.RootName)
		}
		if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
			t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
		}
	}
	if sums[0] != sums[1] {
		t.Errorf("the same message gave two different zips: %s and %s", sums[0], sums[1])
	}

	// a message with more attachments than a folder may hold is one file (XC12 holds by construction)
	var many []e6Part
	for i := 0; i < copyFilesPerDir+205; i++ {
		many = append(many, e6Part{name: fmt.Sprintf("f%04d.bin", i), body: []byte(fmt.Sprintf("part %d", i))})
	}
	mime = e6Mime(msgs[0].SourceID, "a thousand and more", many...)
	e := newCEEnv(t)
	res, _ := e6zRun(t, e, msgs, false, func(string) []byte { return mime })
	perDir := map[string]int{}
	for p := range treeFiles(t, res.Root) {
		perDir[path.Dir(p)]++
	}
	for dir, n := range perDir {
		if n > copyFilesPerDir {
			t.Errorf("%s holds %d files, over the %d bound (XC12)", dir, n, copyFilesPerDir)
		}
	}
	line := assure.Reached(t, e5Files(t, e, res)[1], "the record line")
	zrec, ok := e6zRec(line)
	if !ok || len(line.Files) != 2 {
		t.Fatalf("the files: %+v", line.Files)
	}
	if files, _ := e6zEntries(t, e6Read(t, res.Root, zrec.Path)); len(files) != len(many) {
		t.Errorf("%d entries, want %d", len(files), len(many))
	}
}

// covers: MA-533, R1
// A zero-byte part, a part whose stream fails halfway, one that panics and one with no stream at
// all are no entry and an entry note that names the part and what holds it, never silent; the parts
// around them are entries; no temp is left; a message whose only part is such a part has no zip.
// A part the walker kept as .undecoded or .partial is an entry under that name, holding the bytes
// the walker kept, with the note that it is not the complete decoded attachment.
func TestZipModeNotesAnEmptyFailedOrKeptPart(t *testing.T) {
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
	msgs := ceMsgs(4)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "mixed",
			e6Part{name: "empty.txt", ctype: "text/plain"},
			e6Part{name: "ok.pdf", ctype: "application/pdf", body: []byte("%PDF fine")},
			e6Part{name: "torn.bin", body: []byte("whole")},
			e6Part{name: "panics.bin", body: []byte("whole")},
			e6Part{name: "nothing.bin", body: []byte("whole")}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "only empty", e6Part{name: "empty.txt", ctype: "text/plain"}),
		msgs[2].SourceID: e6Corpus(t, "f24-cte-x-uuencode.eml"),
		msgs[3].SourceID: e6Corpus(t, "f24-cte-base64-truncated.eml"),
	}
	e := newCEEnv(t)
	res, _ := e6zRun(t, e, msgs, false, func(id string) []byte { return body[id] })
	if res.Counts.Exported != 4 || res.Counts.Failed != 0 {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	lines := e5Files(t, e, res)
	first := assure.Reached(t, lines[1], "the first message's line")
	zrec, ok := e6zRec(first)
	if !ok || first.Status != statusExported {
		t.Fatalf("the first message: %s, %+v", first.Status, first.Files)
	}
	if files, _ := e6zEntries(t, e6Read(t, res.Root, zrec.Path)); len(files) != 1 || files[0].Name != "ok.pdf" {
		t.Errorf("the zip's entries: %v", files)
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
	}
	if len(first.Notes) != 4 {
		t.Errorf("the first message's notes: %q", first.Notes)
	}
	second := assure.Reached(t, lines[2], "the second message's line")
	if _, ok := e6zRec(second); ok || len(second.Files) != 1 || len(second.Notes) != 1 || !strings.Contains(second.Notes[0], "has no content") {
		t.Errorf("the second message: %+v, %q", second.Files, second.Notes)
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("temp files left: %v", r)
	}

	// the walker's kept parts are entries under the names it gave them, and noted
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
	third := assure.Reached(t, lines[3], "the third message's line")
	z3, ok := e6zRec(third)
	if !ok {
		t.Fatalf("no zip for the uuencoded message: %+v %q", third.Files, third.Notes)
	}
	_, d3 := e6zEntries(t, e6Read(t, res.Root, z3.Path))
	if !bytes.Equal(d3["note.txt.undecoded"], kept(msgs[2].SourceID, "note.txt.undecoded")) || !bytes.Equal(d3["r.pdf"], kept(msgs[2].SourceID, "r.pdf")) {
		t.Errorf("the entries of the uuencoded message: %v", e6Sizes(d3))
	}
	if len(third.Notes) != 1 || !strings.Contains(third.Notes[0], `"note.txt.undecoded"`) || !strings.Contains(third.Notes[0], "exactly as received, not a decoded attachment") ||
		!strings.Contains(third.Notes[0], ".eml beside it") {
		t.Errorf("the undecoded part's notes: %q", third.Notes)
	}
	fourth := assure.Reached(t, lines[4], "the fourth message's line")
	z4, ok := e6zRec(fourth)
	if !ok {
		t.Fatalf("no zip for the cut message: %+v %q", fourth.Files, fourth.Notes)
	}
	_, d4 := e6zEntries(t, e6Read(t, res.Root, z4.Path))
	if !bytes.Equal(d4["cut.pdf.partial"], kept(msgs[3].SourceID, "cut.pdf.partial")) {
		t.Errorf("the entries of the cut message: %v", e6Sizes(d4))
	}
	if len(fourth.Notes) != 1 || !strings.Contains(fourth.Notes[0], `"cut.pdf.partial"`) || !strings.Contains(fourth.Notes[0], "only the readable start") {
		t.Errorf("the cut part's notes: %q", fourth.Notes)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-533, R1, R29
// The free space left is judged before a part goes into the zip: on a destination with room for the
// original and the first of two 100 kB attachments, the first is an entry, the second is not and is
// a note that says no space was left, and the original, the next message and the run are unharmed.
// Cancel and Abandon are seen inside the zip being made (a message of 40 attachments, the run
// stopped as the zip's temp file is made): no zip stays, no temp stays, a note says the message's
// attachments were not extracted because the export was stopped, and an abandoned run writes
// nothing after the call.
func TestZipModeChecksFreeSpaceAndObservesCancelAndAbandon(t *testing.T) {
	t.Run("space", func(t *testing.T) {
		big1, big2 := e6zNoise(100000, 1), e6zNoise(100000, 2)
		msgs := ceMsgs(2)
		body := map[string][]byte{
			msgs[0].SourceID: e6Mime(msgs[0].SourceID, "two big files", e6Part{name: "first.bin", body: big1}, e6Part{name: "second.bin", body: big2}),
			msgs[1].SourceID: e6Mime(msgs[1].SourceID, "after it"),
		}
		e := newCEEnv(t)
		src := newFakeSrc()
		src.body = func(id string) []byte { return body[id] }
		p := cePlan(t, e, src, msgs)
		p.Options.Attachments, p.Options.AttachmentsAsZip = true, true
		e.freeDest = e6Disk(p.Dest, p.Reserve+int64(len(body[msgs[0].SourceID])+len(body[msgs[1].SourceID]))+160000)
		res := mustRun(t, e, p, src)
		if res.Counts.Exported != 2 || res.Counts.Failed != 0 || res.Stop != nil {
			t.Fatalf("%s, %+v, stop %+v", res.Label, res.Counts, res.Stop)
		}
		lines := e5Files(t, e, res)
		first := assure.Reached(t, lines[1], "the first message's line")
		zrec, ok := e6zRec(first)
		if !ok || first.Status != statusExported {
			t.Fatalf("the first message: %s, %+v %q", first.Status, first.Files, first.Notes)
		}
		files, data := e6zEntries(t, e6Read(t, res.Root, zrec.Path))
		if len(files) != 1 || files[0].Name != "first.bin" || !bytes.Equal(data["first.bin"], big1) {
			t.Errorf("the zip's entries: %v", e6Sizes(data))
		}
		if len(first.Notes) != 1 || !strings.Contains(first.Notes[0], `"second.bin"`) || !strings.Contains(first.Notes[0], "no space was left") || !strings.Contains(first.Notes[0], "export this message again") {
			t.Errorf("the notes: %q", first.Notes)
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
	})

	for _, abandon := range []bool{false, true} {
		name := "cancel"
		if abandon {
			name = "abandon"
		}
		t.Run(name, func(t *testing.T) {
			msgs := ceMsgs(2)
			var parts []e6Part
			for i := 0; i < 40; i++ {
				parts = append(parts, e6Part{name: fmt.Sprintf("f%02d.bin", i), body: []byte(fmt.Sprintf("part %d of the message", i))})
			}
			mime := e6Mime(msgs[0].SourceID, "forty files", parts...)
			e := newCEEnv(t)
			src := newFakeSrc()
			src.body = func(id string) []byte {
				if id == msgs[0].SourceID {
					return mime
				}
				return e6Mime(id, "second")
			}
			p := cePlan(t, e, src, msgs)
			p.Options.Attachments, p.Options.AttachmentsAsZip = true, true
			run := e.NewCopyRun(p, src)
			real := originalParser
			t.Cleanup(func() { originalParser = real })
			streamed := 0 // the parts read after the call are work the stop should have saved
			originalParser = func(b []byte) *model.Message {
				m := real(b)
				for i := range m.Attachments {
					inner := m.Attachments[i].WriteTo
					m.Attachments[i].WriteTo = func(w io.Writer) (int64, error) { streamed++; return inner(w) }
				}
				return m
			}
			made := 0
			e.fsWrap = func(f ceFS) ceFS {
				return &e6zOnTemp{ceFS: f, onTemp: func() {
					if made++; made == 2 { // the first message's original is the first temp, its zip the second
						if abandon {
							run.Abandon()
						} else {
							run.Cancel()
						}
					}
				}}
			}
			res, err := run.Run(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if made > 2 {
				t.Errorf("%d temp files were made: the work went on after the call", made)
			}
			if streamed > 2 {
				t.Errorf("%d of 40 parts were read: the work went on after the call", streamed)
			}
			for p := range treeFiles(t, res.Root) {
				if strings.HasSuffix(p, ".attachments.zip") {
					t.Errorf("a zip of a stopped message stays: %s", p)
				}
			}
			if r := tempResidue(t, res.Root); len(r) != 0 {
				t.Errorf("temp files left: %v", r)
			}
			if abandon {
				if res.Label != "Stopped: the destination is not responding" {
					t.Errorf("the abandoned run ended %q", res.Label)
				}
				return
			}
			if res.Status != CopyStatusCancelled || res.Counts.Exported != 1 {
				t.Fatalf("the cancelled run: %s, %+v", res.Label, res.Counts)
			}
			first := assure.Reached(t, e5Files(t, e, res)[1], "the first message's line")
			if _, ok := e6zRec(first); ok || len(first.Files) != 1 {
				t.Errorf("the line lists %+v", first.Files)
			}
			if !e6advHasNote(first.Notes, "export was stopped", "were not extracted", ".eml beside it") {
				t.Errorf("no note says the attachments were not extracted because the export was stopped: %q", first.Notes)
			}
			if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
				t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
			}
		})
	}
}

// covers: MA-533, R7
// With Readable HTML on, the page links the one zip as a download and does not link a directory or
// say the attachments are inside the .eml; a message with no attachment links nothing.
func TestZipModeReadableCopyLinksTheZip(t *testing.T) {
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "with files", e6Part{name: "report.pdf", ctype: "application/pdf", body: []byte("%PDF fine")}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "no files"),
	}
	e := newCEEnv(t)
	res, _ := e6zRun(t, e, msgs, true, func(id string) []byte { return body[id] })
	lines := e5Files(t, e, res)
	first := assure.Reached(t, lines[1], "the first message's line")
	if len(first.Files) != 3 || first.Files[1].Role != roleDerivedAttachZip || first.Files[2].Role != roleDerivedHTML {
		t.Fatalf("the files: %+v", first.Files)
	}
	page := string(e6Read(t, res.Root, first.Files[2].Path))
	if !strings.Contains(page, `<a href="`+path.Base(first.Files[1].Path)+`">download zip</a>`) || strings.Contains(page, "extracted files") || strings.Contains(page, "attachments are inside the .eml file") {
		t.Errorf("the page does not link the zip:\n%s", page)
	}
	second := assure.Reached(t, lines[2], "the second message's line")
	if p := string(e6Read(t, res.Root, second.Files[1].Path)); strings.Contains(p, "download zip") || strings.Contains(p, "extracted files") {
		t.Errorf("a message with no attachment links something:\n%s", p)
	}
}

// covers: MA-534, R4, R1
func TestZipModeCopiesAnArchivedZipByteExactAndNothingMore(t *testing.T) {
	t.Skip("MA-534: zip mode is not built yet")
}

// covers: MA-534, MA-424
func TestLooseModeIsUnchangedByTheZipModeSwitch(t *testing.T) {
	t.Skip("MA-534: zip mode is not built yet")
}

// covers: MA-535, R12
func TestThePlanSizesAndPlacesAZipModeExport(t *testing.T) {
	t.Skip("MA-535: zip mode is not built yet")
}

// covers: MA-535, R1
func TestReadmeSaysWhatTheAttachmentsZipHolds(t *testing.T) {
	t.Skip("MA-535: zip mode is not built yet")
}
