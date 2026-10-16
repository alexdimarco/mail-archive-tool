package state

import (
	"path/filepath"
	"testing"
)

// covers: MA-348, R1, R18
// F3 of the MF-S2 friction review (cells OW-3, OW-11, CA-2). The run summary and `status` need to say
// which still-missing messages are only missing because a parse gap (a part cut short, a multipart with
// no boundary) is fillable, since the "download for offline use" remedy is for a message a source has not
// delivered, not for one read in full from a damaged mailbox file. The manifest counts the fillable
// records that carry a fillable parse gap (also), and those whose EVERY fillable label is one (only). A
// terminal parse gap, a legacy sentinel, a partly-read record and an ordinary missing attachment count
// for neither.
func TestFillableParseGapCountsSeparateParseGapsFromDownloads(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	cut := ParseGapLabel(GapKindCorruptPart, "c.partial — cut")
	if a, o := m.FillableParseGapCounts(); a != 0 || o != 0 {
		t.Fatalf("an empty manifest counts %d, %d", a, o)
	}
	m.Add("only", Record{Path: "s/a.html", Missing: []string{cut, ParseGapLabel(GapKindUnsplitMultipart, "no boundary")}})
	m.Add("mixed", Record{Path: "s/b.html", Missing: []string{cut, "report.pdf"}})
	m.Add("download", Record{Path: "s/c.html", Missing: []string{"body"}})
	m.Add("terminal", Record{Path: "s/d.html", Terminal: []string{cut}})
	m.Add("legacy", Record{Path: "s/e.html", Missing: []string{UnknownSentinel}})
	m.Add("partly", Record{Path: "s/f.html", Missing: []string{"unreadable:body"}})
	m.Add("forged", Record{Path: "s/g.html", Missing: []string{"\x00forged\x00x"}})
	also, only := m.FillableParseGapCounts()
	if also != 2 || only != 1 {
		t.Errorf("also %d only %d, want 2 (the only-record and the mixed one) and 1 (the only-record)", also, only)
	}
}
