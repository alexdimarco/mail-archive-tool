package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1083
// Scheduling a backup into a cloud-synced folder prints the short "Keep it local"
// recommendation naming the sync app and the reason (owner's ruling of 2026-11-13);
// it is a note, never a refusal. The positive twin: a local folder gets no note.
func TestScheduleIntoASyncedFolderSaysKeepItLocal(t *testing.T) {
	synced := filepath.Join(t.TempDir(), "Dropbox", "Mail")
	note := keepLocalNote(synced)
	for _, want := range []string{
		"Keep it local: this archive is in Dropbox's synced folder",
		"sync apps can lose some of the thousands of small files a backup writes",
		"without any error",
		"A folder on this computer that Dropbox does not sync is safer",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the schedule note does not say %q:\n%q", want, note)
		}
	}

	if note := keepLocalNote(filepath.Join(t.TempDir(), "Mail")); note != "" {
		t.Errorf("a local folder got a keep-it-local note: %q", note)
	}
}
