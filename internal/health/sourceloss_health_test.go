package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-693
// The headless status surfaces source-loss too (archive-manager Slice 3c, S3-C6): when the
// archive-local source-loss streak has crossed the surface threshold, Assess adds a WARN
// naming the since-date and the remedy — so a CLI/headless operator reading `status` sees it,
// not only the Desktop banner. No streak, or one below the threshold, adds no such WARN.
// Amended 2026-10-28 (design-desktop-settings-card 3.7, slice A4, R3C-9 accepted by the owner):
// the threshold is the unreachable runs spanning 3 days (at least two runs), not 3 runs.
func TestMA693_HealthSurfacesSourceLoss(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	lost := t.TempDir()
	if err := state.WriteSourceStreak(lost, state.SourceStreak{
		ConsecutiveFails: 2,
		FirstUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC),
		LastUnreachable:  time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC).Add(state.SurfaceAfter),
	}); err != nil {
		t.Fatal(err)
	}
	in := healthyInput(now)
	in.Out = lost
	rep := Assess(in, now)
	if rep.Posture != "WARN" {
		t.Fatalf("a source-lost archive must read WARN, got %s (%v)", rep.Posture, rep.Reasons)
	}
	if !strings.Contains(strings.Join(rep.Reasons, "\n"), "has not been reachable since") {
		t.Fatalf("health must name the source-loss, got %v", rep.Reasons)
	}

	// Below the threshold: no source-loss WARN (the archive stays GREEN).
	ok := t.TempDir()
	if err := state.WriteSourceStreak(ok, state.SourceStreak{ConsecutiveFails: 1}); err != nil {
		t.Fatal(err)
	}
	in2 := healthyInput(now)
	in2.Out = ok
	if rep2 := Assess(in2, now); strings.Contains(strings.Join(rep2.Reasons, "\n"), "has not been reachable since") {
		t.Fatalf("below the threshold there must be no source-loss WARN: %v", rep2.Reasons)
	}
}
