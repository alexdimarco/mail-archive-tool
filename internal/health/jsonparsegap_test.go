package health

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1, R18, X6
// F11 of the MF-S2 friction review (cell SU-1). README.md says `status -json`'s `last_run` carries the
// same two parse-gap counts as the top level when they are not zero, and the object had neither; and a
// monitor could not tell a fallback from a lost part without parsing the report. `last_run` now has
// `undecodable` and `fidelity_fallback` (omitted when zero, as the last-run record omits them), and a new
// top-level `parse_gaps` gives the number of records per parse-gap kind (omitted when none). Additive: the
// version stays 2 and no existing key moves. The positive twin: with no parse gap none of the three keys
// is there.
func TestStatusJSONLastRunCarriesTheParseGapCountsAndAPerKindSplit(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	doc := func(in Input) map[string]any {
		b, err := json.Marshal(JSON(in, Assess(in, now)))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	plain := doc(healthyInput(now))
	lr, _ := plain["last_run"].(map[string]any)
	if lr == nil {
		t.Fatal("positive twin: no last_run object")
	}
	for _, k := range []string{"undecodable", "fidelity_fallback"} {
		if _, ok := lr[k]; ok {
			t.Errorf("last_run has %q with no parse gap", k)
		}
	}
	if _, ok := plain["parse_gaps"]; ok {
		t.Error("parse_gaps is present with no parse gap")
	}

	in := healthyInput(now)
	in.Undecodable, in.FidelityFallback = 5, 2
	in.LastRun.Undecodable, in.LastRun.FidelityFallback = 5, 2
	in.ParseGapKinds = map[string]int{"undecoded-part": 3, "corrupt-part": 1, "unsplit-multipart": 1, "fidelity-fallback": 2}
	got := doc(in)
	lr, _ = got["last_run"].(map[string]any)
	if lr["undecodable"] != float64(5) || lr["fidelity_fallback"] != float64(2) {
		t.Errorf("last_run undecodable %v fidelity_fallback %v, want 5 and 2", lr["undecodable"], lr["fidelity_fallback"])
	}
	kinds, _ := got["parse_gaps"].(map[string]any)
	if kinds["undecoded-part"] != float64(3) || kinds["corrupt-part"] != float64(1) || kinds["unsplit-multipart"] != float64(1) || kinds["fidelity-fallback"] != float64(2) || len(kinds) != 4 {
		t.Errorf("parse_gaps %v, want the four kinds with their counts", kinds)
	}
	if got["version"] != plain["version"] {
		t.Errorf("version moved from %v to %v: the additions are additive", plain["version"], got["version"])
	}
	if got["undecodable"] != float64(5) || got["fidelity_fallback"] != float64(2) {
		t.Errorf("the top-level counts moved: %v %v", got["undecodable"], got["fidelity_fallback"])
	}
}

// covers: MA-348, R1, R18, X6
// F11, the real path: Gather reads the manifest's parse-gap kinds into the input, so `status -json`
// of an archive on disk carries the per-kind split a monitor keys on.
func TestGatherReadsThePerKindParseGapCountsFromTheManifest(t *testing.T) {
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("k1", state.Record{Path: "s/a.html", Terminal: []string{state.ParseGapLabel(state.GapKindUndecodedPart, "a.undecoded")}})
	m.Add("k2", state.Record{Path: "s/b.html", Terminal: []string{state.ParseGapLabel(state.GapKindFidelityFallback, "x")}, FidelityFallback: 2})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if in.ParseGapKinds[state.GapKindUndecodedPart] != 1 || in.ParseGapKinds[state.GapKindFidelityFallback] != 1 || len(in.ParseGapKinds) != 2 {
		t.Errorf("Gather read the kinds %v, want undecoded-part 1 and fidelity-fallback 1", in.ParseGapKinds)
	}
}
