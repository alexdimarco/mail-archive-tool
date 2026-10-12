package app

import (
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-334, MA-348, R1, R7, R26
// The report's row for each parse-gap kind (design-mime-fidelity §4.9, MA-348): the
// row's kind is the gap's kind (never "empty-attachment"), its class follows the list the
// label sits in, its detail is the label's detail followed by the why and the what to
// do, and a record that has more than 32 keeps its summary row. An older binary's view of
// the same record is an ordinary attachment row.
func TestReportRowsForParseGaps(t *testing.T) {
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	l := func(kind, detail string) string { return state.ParseGapLabel(kind, detail) }
	m.Add("k1", state.Record{Path: "s/a.html", Folder: "Inbox", Subject: "A", Terminal: []string{
		l(state.GapKindUndecodedPart, "note.txt.undecoded — transfer encoding \"x-foo\" is not supported"),
		l(state.GapKindStructureLimit, "and 5 more undecodable parts"),
	}})
	m.Add("k2", state.Record{Path: "s/b.html", Folder: "Inbox", Subject: "B", Missing: []string{l(state.GapKindCorruptPart, "cut.pdf.partial — cut short")}})
	m.Add("k3", state.Record{Path: "s/c.html", Folder: "Inbox", Subject: "C", Terminal: []string{l(state.GapKindCorruptPart, "cut.pdf.partial — cut short")}})
	m.Add("k4", state.Record{Path: "s/d.html", Folder: "Inbox", Subject: "D", Missing: []string{l(state.GapKindUnsplitMultipart, "no boundary")}})
	m.Add("k5", state.Record{Path: "s/e.html", Folder: "Inbox", Subject: "E", Terminal: []string{
		l(state.GapKindFidelityFallback, "held less"), l(state.GapKindIdentityStub, "stub")}})
	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if rows != 7 {
		t.Fatalf("%d report rows, want 7", rows)
	}
	type want struct{ class, detail string }
	got := map[string]want{}
	for _, line := range readLines(t, path)[1:] {
		f := strings.Split(line, "\t")
		got[f[0]+"/"+f[1]+"/"+f[3+1]] = want{f[1], f[5]}
	}
	for _, c := range []struct{ key, detailPrefix, why, remedy string }{
		{"undecoded-part/terminal/A", "note.txt.undecoded — transfer encoding", "does not decode", "nothing to do: the part is in the attachments zip as received"},
		{"structure-limit/terminal/A", "and 5 more undecodable parts", "more parts, or nests them deeper", "nothing to do: the rest is in the attachments zip"},
		{"corrupt-part/fillable/B", "cut.pdf.partial — cut short", "cut short or damaged", "download the complete message in your mail app, then run again"},
		{"corrupt-part/terminal/C", "cut.pdf.partial — cut short", "cut short or damaged", "nothing to do: the source cannot deliver a complete copy"},
		{"unsplit-multipart/fillable/D", "no boundary", "could not be told apart", "download the complete message in your mail app, then run again"},
		{"fidelity-fallback/terminal/E", "held less", "as an earlier version made them", "nothing is lost"},
		{"identity-stub/terminal/E", "stub", "placeholder", "the page shows the message as this version reads it"},
	} {
		g, ok := got[c.key]
		if !ok {
			t.Errorf("no %q row (rows: %v)", c.key, got)
			continue
		}
		if !strings.HasPrefix(g.detail, c.detailPrefix) || !strings.Contains(g.detail, " · why: ") || !strings.Contains(g.detail, c.why) ||
			!strings.Contains(g.detail, " · what to do: ") || !strings.Contains(g.detail, c.remedy) || !strings.Contains(g.detail, "the original is kept") {
			t.Errorf("row %q: detail %q does not read as detail · why · what to do (%q, %q, %q)", c.key, g.detail, c.detailPrefix, c.why, c.remedy)
		}
		if strings.ContainsRune(g.detail, 0) {
			t.Errorf("row %q: the label's separator reached the report: %q", c.key, g.detail)
		}
	}
}
