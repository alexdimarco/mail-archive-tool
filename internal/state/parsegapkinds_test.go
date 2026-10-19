package state

import (
	"path/filepath"
	"testing"
)

// covers: MA-348, R1
// F11 of the MF-S2 friction review (cell SU-1). The manifest counts records per parse-gap kind, so a
// monitor can tell a fallback from a lost part: a record with two gaps of one kind counts once for
// the kind, one with gaps of two kinds counts once for each, and an ordinary attachment gap or a
// label that only looks like a parse gap counts for none. With no parse gap the answer is empty.
func TestParseGapKindCountsCountsRecordsPerKind(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ParseGapKindCounts(); len(got) != 0 {
		t.Fatalf("an empty manifest has kinds %v", got)
	}
	m.Add("k1", Record{Path: "s/a.html", Terminal: []string{
		ParseGapLabel(GapKindUndecodedPart, "a.undecoded"), ParseGapLabel(GapKindUndecodedPart, "b.undecoded"), ParseGapLabel(GapKindStructureLimit, "rest")}})
	m.Add("k2", Record{Path: "s/b.html", Missing: []string{ParseGapLabel(GapKindCorruptPart, "c.partial")}, Terminal: []string{"report.pdf"}})
	m.Add("k3", Record{Path: "s/c.html", Terminal: []string{ParseGapLabel(GapKindFidelityFallback, "held less"), ParseGapLabel(GapKindUndecodedPart, "d.undecoded")}})
	m.Add("k4", Record{Path: "s/d.html", Terminal: []string{"\x00forged-kind\x00detail", "plain.pdf"}})
	want := map[string]int{GapKindUndecodedPart: 2, GapKindStructureLimit: 1, GapKindCorruptPart: 1, GapKindFidelityFallback: 1}
	got := m.ParseGapKindCounts()
	if len(got) != len(want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("kind %s counts %d, want %d (all: %v)", k, got[k], n, got)
		}
	}
}
