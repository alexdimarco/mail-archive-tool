package wizard

import (
	"testing"

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
