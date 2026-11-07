package health

import (
	"encoding/json"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1146, MA-1145, R18, S48, S29
// `status` (and the Desktop's Status & health, which prints the same Summary, X10) tells a
// partial run as one that did not save everything, in the kinds' words — never "FAILED:
// partial: … see attachments-report.tsv" — while every machine-readable code stays: the reason
// code is still run_failed and the posture still RED (the S1a mapping; alerts unchanged), and
// `status -json` adds last_run.partial with the kinds beside the gap counts it already had.
// Positive twin first: a run that failed outright is still "FAILED: <its reason>".
func TestStatusTellsAPartialRunByItsKinds(t *testing.T) {
	in := healthyInput(gapNow)
	in.LastRun.Status, in.LastRun.Error = state.RunFailed, "the archive drive is full"
	rep := Assess(in, gapNow)
	i := codeIndex(rep, "run_failed")
	if rep.Posture != "RED" || i < 0 || !strings.Contains(rep.Reasons[i], "FAILED: the archive drive is full") {
		t.Fatalf("the positive twin: %v %v", rep.Posture, rep.Reasons)
	}

	p := &state.PartialCounts{Mistake: state.PartialKind{Messages: 4}}
	in.LastRun.Error, in.LastRun.Partial = state.PartialText(*p, true), p
	in.LastRun.GapsObserved, in.LastRun.GapsTerminal = 4, 4
	for _, surface := range []string{"", "desktop"} {
		in.Surface = surface
		rep = Assess(in, gapNow)
		i = codeIndex(rep, "run_failed")
		if rep.Posture != "RED" || i < 0 {
			t.Fatalf("surface %q: the codes changed: %v %v", surface, rep.Posture, rep.Codes)
		}
		r := rep.Reasons[i]
		if !strings.Contains(r, ") did not save everything: 4 messages could not be saved because of a mistake in MailArchive — "+state.MistakeRemedy+".") ||
			strings.Contains(r, "FAILED") || strings.Contains(r, "partial:") {
			t.Errorf("surface %q: the reason %q", surface, r)
		}
		sum := strings.Join(Summary(in, rep), "\n")
		if !strings.Contains(sum, "→ incomplete: 4 messages could not be saved") || strings.Contains(sum, "→ failed") {
			t.Errorf("surface %q: the Last run line:\n%s", surface, sum)
		}
	}
	b, _ := json.Marshal(JSON(in, rep))
	var doc struct {
		Codes   []string `json:"reason_codes"`
		LastRun struct {
			Status       string               `json:"status"`
			GapsObserved int                  `json:"gaps_observed"`
			Partial      *state.PartialCounts `json:"partial"`
		} `json:"last_run"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, doc.LastRun.Status == state.RunFailed && doc.LastRun.GapsObserved == 4 && doc.LastRun.Partial != nil &&
		*doc.LastRun.Partial == *p && strings.Contains(strings.Join(doc.Codes, ","), "run_failed"),
		"status -json keeps its codes and adds last_run.partial: "+string(b))
	if !strings.Contains(string(b), `"partial":{"mailarchive_mistake":{"messages":4}}`) {
		t.Errorf("the partial object's shape: %s", b)
	}
}
