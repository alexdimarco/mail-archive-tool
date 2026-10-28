package wizard

import (
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// covers: MA-675
// The Pause/Resume lifecycle: PauseArchive makes the archive hard read-only (sealed,
// writes refused, reason recorded) and ResumeArchive makes it writable again; both are
// idempotent. The schedule removal (RemoveLiveEntry, keeping descriptors) and the backup
// restore (repairSchedule) are composed from already-covered primitives (MA-674, MA-617);
// this exercises the seal-transition contract the UI and CLI both rely on.
func TestMA675_PauseResumeSealTransitions(t *testing.T) {
	out := t.TempDir() // no schedule installed: PauseArchive just seals

	if err := PauseArchive(out, "parked by the owner"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !seal.IsSealed(out) {
		t.Fatal("after Pause the archive must be sealed (read-only)")
	}
	if seal.RefuseWrite(out) == nil {
		t.Fatal("a paused archive must refuse writes")
	}
	if got := seal.Reason(out); got != "parked by the owner" {
		t.Fatalf("the pause reason must round-trip, got %q", got)
	}

	// Pause is idempotent.
	if err := PauseArchive(out, "again"); err != nil {
		t.Fatalf("second pause must be idempotent: %v", err)
	}

	if err := ResumeArchive(out); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if seal.IsSealed(out) {
		t.Fatal("after Resume the archive must be writable")
	}
	if seal.RefuseWrite(out) != nil {
		t.Fatal("a resumed archive must not refuse writes")
	}

	// Resume is idempotent.
	if err := ResumeArchive(out); err != nil {
		t.Fatalf("second resume must be idempotent: %v", err)
	}
}

// covers: MA-680
// PauseArchive must act on a scheduler entry only when the recorded name BINDS to this
// archive (bound.go), never the raw descriptor name — a handed-over folder can set that name
// to a foreign OS task (Windows) or another archive's derived entry. A non-binding name is
// skipped (no live entry of ours lives under it) and the archive still seals; this archive's
// own bound entry is removed. Encodes the 2026-10-20 security pass HIGH finding.
func TestMA680_PauseBindsScheduleNameBeforeRemoving(t *testing.T) {
	var removed []string
	orig := removeLiveEntry
	removeLiveEntry = func(s schedule.Spec) error { removed = append(removed, s.Name); return nil }
	defer func() { removeLiveEntry = orig }()

	// Positive twin: a legitimate archive whose descriptor records its OWN derived name
	// binds, so Pause removes exactly that entry.
	legit := t.TempDir()
	if err := schedule.WriteDescriptor(legit, schedule.Descriptor{
		Name: schedule.DefaultNameFor(legit), Interval: "daily", At: "02:00",
		Job: []string{"mailarchive", "-out", legit},
	}); err != nil {
		t.Fatalf("seed legit descriptor: %v", err)
	}
	if err := PauseArchive(legit, "parked"); err != nil {
		t.Fatalf("pause legit: %v", err)
	}
	if len(removed) != 1 || removed[0] != schedule.DefaultNameFor(legit) {
		t.Fatalf("pause must remove this archive's own bound entry, got %v", removed)
	}
	if !seal.IsSealed(legit) {
		t.Fatal("legit archive must be sealed after pause")
	}

	// The attack: a handed-over archive whose descriptor records ANOTHER archive's derived
	// entry name (the airtight cross-archive case, rejected on every platform by bound.go).
	removed = nil
	other := t.TempDir() // a different archive, only to borrow its derived name
	victim := t.TempDir()
	if err := schedule.WriteDescriptor(victim, schedule.Descriptor{
		Name: schedule.DefaultNameFor(other), Interval: "daily", At: "02:00",
		Job: []string{"mailarchive", "-out", victim},
	}); err != nil {
		t.Fatalf("seed foreign descriptor: %v", err)
	}
	if err := PauseArchive(victim, "parked"); err != nil {
		t.Fatalf("pausing a handed-over archive must still succeed: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("pause must NOT act on an entry named by an unbound (foreign) descriptor, but it tried to remove %v", removed)
	}
	if !seal.IsSealed(victim) {
		t.Fatal("handed-over archive must still seal even though no entry was removed")
	}
}

// covers: MA-690
// Auto-pause after a long source-loss streak (archive-manager Slice 3c, S3-C5): when the
// source's unreachable runs span state.AutoPauseAfter (30 days, at least two runs, measured
// between runs), AutoPauseIfDue stops the live backup schedule — ONLY the bound live entry
// (never the raw descriptor name, MA-680), KEEPING the descriptor so it is reversible — and
// NEVER seals. Below the threshold it does nothing. Amended 2026-10-28 (design-desktop-
// settings-card 3.7, slice A4, R3C-9 accepted by the owner): it counted 30 runs until then.
func TestMA690_AutoPauseRemovesScheduleWhenDue(t *testing.T) {
	out := t.TempDir()
	rec := recordRemovals(t, schedule.DefaultNameFor(out)) // the entry is installed

	if err := schedule.WriteDescriptor(out, schedule.Descriptor{
		Name: schedule.DefaultNameFor(out), Interval: "daily", At: "02:00",
		Job: []string{"mailarchive", "-out", out},
	}); err != nil {
		t.Fatal(err)
	}

	// Below the threshold: no auto-pause, nothing removed.
	t0 := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 700, FirstUnreachable: t0, LastUnreachable: t0.Add(state.AutoPauseAfter - time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if paused, err := AutoPauseIfDue(out, true); err != nil || paused || len(*rec) != 0 {
		t.Fatalf("must not auto-pause below the threshold: paused=%v err=%v removed=%v", paused, err, *rec)
	}

	// At the threshold: auto-pause removes this archive's bound entry, keeps the descriptor,
	// and does NOT seal.
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.AutoPauseAfter)}); err != nil {
		t.Fatal(err)
	}
	paused, err := AutoPauseIfDue(out, true)
	if err != nil || !paused {
		t.Fatalf("must auto-pause at the threshold: paused=%v err=%v", paused, err)
	}
	if len(*rec) != 1 || (*rec)[0] != schedule.DefaultNameFor(out) {
		t.Fatalf("must remove this archive's bound entry, got %v", *rec)
	}
	if seal.IsSealed(out) {
		t.Fatal("auto-pause must NOT seal the archive (S3-C5)")
	}
	if _, derr := schedule.ReadDescriptor(out); derr != nil {
		t.Fatal("auto-pause must KEEP the descriptor so the schedule can be reinstalled (reversible)")
	}
}
