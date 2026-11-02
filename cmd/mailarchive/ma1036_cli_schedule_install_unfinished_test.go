package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1036, R36
// `mailarchive schedule -install` refuses a folder whose delete did not finish (the 1.5.3
// re-walk's guards pass, G1): before, it installed the scheduled backup the delete had stopped,
// exit 0 — the harm MA-1015 closed for `resume` — and that backup's run writes into the
// half-deleted folder, since the emptied-folder guard lets it in while the manifest is there. Now
// it refuses in MA-1015's words with the terminal's Delete again, exit 1, and no scheduler entry
// is made. Run as the real program against stand-in scheduler programs (sandboxScheduler), in a
// throwaway home whose deletions.log it reads. Positive twin first: another folder, with that
// unfinished delete in the same log, is installed.
func TestTerminalScheduleInstallRefusesADeleteNotFinished(t *testing.T) {
	sandboxScheduler(t)
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
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
	line, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "cli1036", "folder": half, "row": half, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if u, err := archdelete.UnfinishedDelete(logPath, half, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", half, u, err)
	}

	// Positive twin.
	if code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", other, "-auto"); code != 0 || schedule.Query(schedule.DefaultNameFor(other)) != schedule.Installed {
		t.Fatalf("positive twin: schedule -install of another folder (%d): %s%s", code, stdout, stderr)
	}

	code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", half, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so its schedule was not installed; nothing was changed.",
			"At this terminal, Delete again is: mailarchive delete-archive -out"),
		assure.NoSideEffect(func() bool {
			_, derr := os.Stat(filepath.Join(half, schedule.DescriptorName))
			return schedule.Query(schedule.DefaultNameFor(half)) != schedule.Installed && os.IsNotExist(derr)
		}))
	if t.Failed() {
		t.Logf("stdout: %s", stdout)
	}
}
