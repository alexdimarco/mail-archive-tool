package export

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

func gapAtt(name, kind string) model.Attachment {
	return model.Attachment{Filename: name, GapKind: kind, GapDetail: "detail",
		WriteTo: func(w io.Writer) (int64, error) { n, err := w.Write([]byte("bytes")); return int64(n), err }}
}

func plainAtt(name string) model.Attachment {
	return model.Attachment{Filename: name, WriteTo: func(w io.Writer) (int64, error) { n, err := w.Write([]byte("bytes")); return int64(n), err }}
}

// covers: MA-348, MA-90, R1, R7
// F8 and F9 of the MF-S2 friction review (cells IN-1, OW-2, OW-9). An archive's message page lists
// a part kept as received (.undecoded) or cut short (.partial) under the name the zip holds it by,
// and says in one inert line, in the tool's own mailarchive- namespace, what such a file is and
// where the report that says which part and why is. An inheritor with only the page needs no tool
// for that. The positive twin: a page whose attachments carry no parse gap has no such line, and a
// SENDER's attachment named like the tool's (x.undecoded, no gap) is only an attachment: the line is
// driven by the gap, never by a name.
func TestAPageSaysWhatAKeptOrCutPartIs(t *testing.T) {
	ctx := RenderContext{RootRel: "../../", FolderIndexRel: "index.html", ZipName: "x-attachments.zip"}
	render := func(atts ...model.Attachment) string {
		rr, err := RenderWith(&model.Message{Subject: "S", PlainBody: "hi", Attachments: atts}, ctx)
		if err != nil {
			t.Fatal(err)
		}
		return string(rr.HTML)
	}

	// Positive twin: no gap, and a name that only looks like the tool's, say nothing.
	plain := render(plainAtt("report.pdf"), plainAtt("sender-chose.undecoded"), plainAtt("x.partial"))
	if strings.Contains(plain, "mailarchive-partnote") || strings.Contains(plain, "attachments-report.tsv") || strings.Contains(plain, "kept exactly as received") {
		t.Fatalf("a page with no parse gap carries the note:\n%s", plain)
	}
	if !strings.Contains(plain, "sender-chose.undecoded") {
		t.Fatal("positive twin: the plain attachment is not listed")
	}

	page := render(gapAtt("invoice.dat.undecoded", state.GapKindUndecodedPart), gapAtt("contract.pdf.partial", state.GapKindCorruptPart),
		gapAtt("remaining-parts.undecoded", state.GapKindStructureLimit), plainAtt("report.pdf"))
	i := strings.Index(page, `mailarchive-partnote"`)
	if i < 0 {
		t.Fatalf("no part note:\n%s", page)
	}
	note := page[i : i+strings.Index(page[i:], "</div>")]
	for _, want := range []string{"invoice.dat.undecoded", "kept exactly as received", "contract.pdf.partial", "readable start", "attachments-report.tsv"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "report.pdf") {
		t.Errorf("the note names a part with no gap:\n%s", note)
	}
	if strings.Contains(note, "<a ") || strings.Contains(note, "<script") {
		t.Errorf("the note is not inert text:\n%s", note)
	}
}

// covers: MA-348, R1, R7, R6
// F9 of the MF-S2 friction review (cell OW-9), the report half. The record's parse-gap label,
// which is what the report row's detail begins with, names the part by its zip member name, so
// a person who searches the zip for the name the report gives finds it. The zip written by the
// same export holds that name.
func TestAParseGapLabelNamesThePartByItsZipMember(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	m := &model.Message{Subject: "Reserved", Received: testDate, InternetMessageID: "<res@x>", PlainBody: "body",
		Attachments: []model.Attachment{gapAtt("CON.undecoded", state.GapKindUndecodedPart)}}
	if _, err := e.Export("store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	rec, ok := manifest.Get(state.Key("store", "Inbox", m.Identity()))
	if !ok {
		t.Fatal("no record")
	}
	var detail string
	for _, l := range append(append([]string(nil), rec.Missing...), rec.Terminal...) {
		if kind, d, ok := state.ParseGapParts(l); ok && kind == state.GapKindUndecodedPart {
			detail = d
		}
	}
	if !strings.HasPrefix(detail, "_CON.undecoded — ") {
		t.Fatalf("the parse-gap label names the part %q, want its zip member _CON.undecoded", detail)
	}
	zips, _ := filepath.Glob(filepath.Join(out, "store", "Inbox", "*-attachments.zip"))
	if len(zips) != 1 {
		t.Fatalf("zips %v, want 1", zips)
	}
	zr, err := zip.OpenReader(zips[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "_CON.undecoded" {
		t.Errorf("the zip holds %v, want _CON.undecoded", zr.File)
	}
}

// covers: MA-348, MA-90, R1, R7, R6
// F9 of the MF-S2 friction review (cell OW-9). The page lists a part kept as received under the
// name the attachments zip holds it by (Windows device names are prefixed, controls and a trailing
// dot are gone), so the name on the page is a name in the zip. A plain attachment keeps its
// sender-chosen listing, as before.
func TestAPageListsAKeptPartUnderItsZipMemberName(t *testing.T) {
	ctx := RenderContext{RootRel: "../../", FolderIndexRel: "index.html", ZipName: "x-attachments.zip"}
	rr, err := RenderWith(&model.Message{Subject: "S", PlainBody: "hi", Attachments: []model.Attachment{
		gapAtt("CON.undecoded", state.GapKindUndecodedPart), gapAtt("bell\x07name.dat.partial", state.GapKindCorruptPart), plainAtt("nul.txt"),
	}}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := string(rr.HTML)
	list := s[strings.Index(s, `class="mailarchive-attachments"`):]
	list = list[:strings.Index(list, "</div>")]
	for _, want := range []string{"_CON.undecoded", "bell name.dat.partial", "nul.txt"} {
		if !strings.Contains(list, want) {
			t.Errorf("the attachment list lacks %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, ">CON.undecoded") || strings.Contains(list, ", CON.undecoded") || strings.Contains(list, "\x07") {
		t.Errorf("the list gives a kept part under a name the zip does not hold:\n%s", list)
	}
}
