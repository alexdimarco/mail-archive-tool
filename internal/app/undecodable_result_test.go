package app

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1, R18, X6
// A run's Result counts the records with a part this version cannot decode and the
// records written from v1's content, apart from the source-empty ones: the same numbers
// the summary, `status` and the last-run record carry (X6). A parse gap alone is not
// "source-empty", and none of this makes a run partial.
func TestResultCountsUndecodableAndFallbackRecords(t *testing.T) {
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	m.Add("k1", state.Record{Path: "s/a.html", Folder: "Inbox", Terminal: []string{state.ParseGapLabel(state.GapKindUndecodedPart, "a")}})
	m.Add("k2", state.Record{Path: "s/b.html", Folder: "Inbox", Missing: []string{state.ParseGapLabel(state.GapKindCorruptPart, "b")}})
	m.Add("k3", state.Record{Path: "s/c.html", Folder: "Inbox", Terminal: []string{"report.pdf"}})
	m.Add("k4", state.Record{Path: "s/d.html", Folder: "Inbox", Terminal: []string{state.ParseGapLabel(state.GapKindFidelityFallback, "d")}, FidelityFallback: 1})
	var r Result
	exp := &export.Exporter{OutDir: out, Manifest: m, Log: discard()}
	finish(out, &r, exp, m, nil, 0, false, discard())
	if r.Undecodable != 3 || r.FidelityFallback != 1 || r.Terminal != 1 || r.Fillable != 1 {
		t.Errorf("Undecodable %d FidelityFallback %d Terminal %d Fillable %d, want 3 1 1 1", r.Undecodable, r.FidelityFallback, r.Terminal, r.Fillable)
	}
	if r.Partial {
		t.Error("parse gaps made the run partial: they are informational")
	}

	// The last-run record carries the same two counts, and an absent count stays absent.
	lr := state.LastRun{}
	recordCounts(&lr, r)
	if lr.Undecodable != 3 || lr.FidelityFallback != 1 {
		t.Errorf("last run: undecodable %d fidelity_fallback %d, want 3 and 1", lr.Undecodable, lr.FidelityFallback)
	}
}
