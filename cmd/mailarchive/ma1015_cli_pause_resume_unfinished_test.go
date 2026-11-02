package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-1015, R36
// `mailarchive pause -out DIR` and `mailarchive resume -out DIR` refuse a folder whose delete did
// not finish, in MA-1015's words, with the terminal's Delete again (the 1.5.3 re-walk's
// adversarial pass, F1): before, both acted on it — pause wrote its marker into the half-deleted
// folder, and resume cleared it and put back the scheduled backup the delete stopped, whose next
// run passes the emptied-folder guard because the manifest goes last. Exit 1; pause writes no
// marker, resume leaves the folder paused. Run as the real program in this package's throwaway
// home, whose deletions.log it reads. Positive twins first: another folder is paused and resumed
// with that unfinished delete in the same log.
func TestTerminalPauseAndResumeRefuseADeleteNotFinished(t *testing.T) {
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// The home is the package's: put its log back as it was.
	before, rerr := os.ReadFile(logPath)
	t.Cleanup(func() {
		if rerr != nil {
			os.Remove(logPath)
			return
		}
		os.WriteFile(logPath, before, 0o600)
	})
	half := filepath.Join(t.TempDir(), "Half deleted")
	other := filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".mailarchive-manifest.json"), []byte(`{"version":1,"records":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	line, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "cli1015", "folder": half, "row": half, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(append(append([]byte(nil), before...), line...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if u, err := archdelete.UnfinishedDelete(logPath, half, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", half, u, err)
	}

	// Positive twins.
	if code, stderr := runCLI("pause", "-out", other); code != 0 || !seal.IsSealed(other) {
		t.Fatalf("positive twin: pause of another folder (%d): %s", code, stderr)
	}
	if code, stderr := runCLI("resume", "-out", other); code != 0 || seal.IsSealed(other) {
		t.Fatalf("positive twin: resume of another folder (%d): %s", code, stderr)
	}

	code, stderr := runCLI("pause", "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not paused; nothing was changed.",
			"At this terminal, Delete again is: mailarchive delete-archive -out"),
		assure.NoSideEffect(func() bool { return !seal.IsSealed(half) }))

	if err := seal.Seal(half, "parked before the delete"); err != nil {
		t.Fatal(err)
	}
	code, stderr = runCLI("resume", "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not resumed; nothing was changed.",
			"At this terminal, Delete again is: mailarchive delete-archive -out"),
		assure.NoSideEffect(func() bool { return seal.IsSealed(half) }))
}
