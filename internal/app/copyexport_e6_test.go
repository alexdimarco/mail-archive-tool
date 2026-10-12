package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/source"
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

// covers: MA-424, R4, R29
// Hostile attachment names (.., separators, device names, bidi controls, 300 bytes of CJK) become
// distinct, creatable names inside <stem>.attachments/, and a link planted where the directory is
// is not written through.
func TestExtractedAttachmentNamesAreSanitizedAndContained(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// A zero-byte part and a part whose stream fails are entry notes, never silent, and write no file.
func TestAZeroByteOrFailedPartIsANoteNeverSilent(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// A part the walker kept as .undecoded or .partial is extracted under that name as what it is and
// noted as not the complete decoded attachment.
func TestKeptPartsAreExtractedAsWhatTheyAreAndNoted(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R29
// Each derived attachment is checked against the free space left before it is written; a part
// that does not fit is a note, and the original and the run are unharmed.
func TestDerivedAttachmentsAreCheckedAgainstFreeSpace(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
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
