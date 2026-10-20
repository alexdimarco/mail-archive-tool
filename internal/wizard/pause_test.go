package wizard

import (
	"testing"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
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
