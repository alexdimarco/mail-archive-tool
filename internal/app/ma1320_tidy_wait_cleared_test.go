package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1320, R45
// A backup of the archive that gets past the manifest ends this computer's wait for its tidy-up,
// whatever started it (the B3 adversarial pass, round 2's G4): the engine ends it, so a backup at a
// terminal, one a `mailarchive schedule` job runs and the Desktop's Run now end it as the scheduled
// run-job does, and a later tidy-up's wait starts afresh. Only that archive's wait ends: another
// archive's wait kept for the same folder (a drive of a rotation) stays. Positive twin first: a
// backup refused because the archive is still at version 8 keeps the wait.
func TestABackupPastTheManifestEndsTheTidyUpWaitWhateverStartedIt(t *testing.T) {
	ma1316Settings(t)
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := filepath.Join(tmpDir(t), "archive")
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	ma1316SetVersion(t, out, 8)
	began := time.Now().Add(-time.Hour)
	if _, err := gmailanswer.NoteTidyWait(out, began); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); !TidyingElsewhere(err) {
		t.Fatalf("positive twin: the run met the tidy-up: %v", err)
	}
	if _, ok := gmailanswer.ReadTidyWait(out); !ok {
		t.Fatal("positive twin: a backup refused at version 8 ended the wait")
	}
	// Another archive's wait for the same folder (the second drive of a rotation).
	other := state.NewArchiveID()
	if _, err := gmailanswer.NoteTidyWaitFor(out, other, began); err != nil {
		t.Fatal(err)
	}

	// The tidy-up finished: an attended backup (as at a terminal or Run now) ends the wait.
	ma1316SetVersion(t, out, 6)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if w, ok := gmailanswer.ReadTidyWait(out); ok {
		t.Fatalf("the backup that went through left the wait: %+v", w)
	}
	if _, ok := gmailanswer.ReadTidyWaitFor(out, other); !ok {
		t.Fatal("the backup ended another archive's wait kept for the same folder")
	}
}
