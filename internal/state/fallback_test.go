package state

import (
	"path/filepath"
	"testing"
)

// covers: MA-494, R1, R7, R18
// RV-10: the manifest's per-record body_fallback is the source of truth for how
// many pages are shown as source, per reason; records without one (every
// message that rendered as HTML, and every record from before the field) count
// nothing, and the count survives a save and a reload.
func TestFallbackCountsPerReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.FallbackCounts(); len(got) != 0 {
		t.Fatalf("empty manifest: counts = %v, want none", got)
	}
	m.Add("a", Record{Path: "a.html", Folder: "F"}) // positive twin: rendered as HTML
	m.Add("b", Record{Path: "b.html", Folder: "F", BodyFallback: "too-deeply-nested"})
	m.Add("c", Record{Path: "c.html", Folder: "F", BodyFallback: "too-deeply-nested"})
	m.Add("d", Record{Path: "d.html", Folder: "F", BodyFallback: "unparseable"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := m2.FallbackCounts()
	if len(got) != 2 || got["too-deeply-nested"] != 2 || got["unparseable"] != 1 {
		t.Errorf("counts after reload = %v, want too-deeply-nested:2 unparseable:1", got)
	}
}
