package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-871, R37
// Status reads the source streak between runs, never against now (R3C-11): a computer that was
// off 3 days, or 30, after a failed run shows no source-loss warning when it boots, however late
// status is read. Positive twin first: unreachable runs spanning 3 days warn.
func TestAComputerOffForDaysShowsNoSourceLossOnBoot(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	warns := func(s state.SourceStreak, now time.Time) bool {
		out := t.TempDir()
		if err := state.WriteSourceStreak(out, s); err != nil {
			t.Fatal(err)
		}
		in := healthyInput(now)
		in.Out = out
		return strings.Contains(strings.Join(Assess(in, now).Reasons, "\n"), "has not been reachable since")
	}
	if !warns(state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.SurfaceAfter)}, t0.Add(state.SurfaceAfter)) {
		t.Fatal("the positive twin: unreachable runs 3 days apart must warn")
	}
	for _, off := range []time.Duration{3 * 24 * time.Hour, 30 * 24 * time.Hour} {
		// Two failed runs an hour apart, then the computer off.
		s := state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(time.Hour)}
		if warns(s, t0.Add(off)) {
			t.Errorf("a computer off %v after its failed runs warns on boot", off)
		}
	}
}
