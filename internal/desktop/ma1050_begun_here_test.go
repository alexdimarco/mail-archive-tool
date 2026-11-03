package desktop

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1050, R36
// DM-10's "begun here" on the Desktop (docs/design-archive-delete.md §15.4's DM-10 note; §15.10
// slice 5; lead's words, owner-delegated 2026-11-02): a folder holding a delete marker whose run this
// Desktop's deletion log began — though no line of the log matches the folder now (another path and
// another device number) — is said to be begun on this computer, in the row and in Status & health,
// never "on another computer or user account"; Archive now refuses it in the begun-here words.
// Positive twin first: with no such run in the log, the marker-only words.
func TestMA1050_TheDesktopSaysADeleteBegunHereWasBegunHere(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Name 1")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	ma1048Plant(t, dir) // run 0123456789abcdef
	if posture, summary := ma1049Health(t, e); posture != "RED" || !strings.Contains(summary, "this computer's deletion log has no record of it") {
		t.Fatalf("positive twin: no such run in this Desktop's log: %s\n%s", posture, summary)
	}

	// This Desktop's log began that delete, under the folder's old name and identity.
	lp := e.cfg.deletionsLogPath()
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T15:04:05Z", "event": "started", "run": "0123456789abcdef",
		"folder": filepath.Join(filepath.Dir(dir), "Name"), "kind": "plain", "identity": map[string]any{"volume": 1, "file_id": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lp, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	posture, summary := ma1049Health(t, e)
	if posture != "RED" || !strings.Contains(summary, "RED: a delete of this archive begun "+began+" did not finish; it was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name) — click Delete… on its row in Your archives to finish it (add the folder first with Open an archive folder… if it is not listed).") ||
		strings.Contains(summary, "another computer") {
		t.Fatalf("Status & health: %s\n%s", posture, summary)
	}
	row := html.UnescapeString(rowFor(t, e.page(), dir))
	if !strings.Contains(row, "Delete not finished: a delete of this archive begun "+began+" did not finish. It was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name). Click Delete… to finish it.") ||
		strings.Contains(row, "another computer") {
		t.Fatalf("the row: %s", row)
	}
	if got := e.d.unfinishedRefusal(dir); !strings.Contains(got, "Delete not finished: a delete of this archive was begun on this computer and did not finish, so it was not backed up; nothing was changed.") ||
		strings.Contains(got, "another computer") {
		t.Fatalf("Archive now's refusal: %q", got)
	}
}
