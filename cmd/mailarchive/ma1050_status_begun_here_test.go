package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1050, R36
// DM-10's "begun here" as the real program (docs/design-archive-delete.md §15.4's DM-10 note; §15.10
// slice 5; lead's words, owner-delegated 2026-11-02): `status` of a folder holding a delete marker
// whose run this computer's deletion log began, though no line of it matches the folder now (another
// path and another device number), says it was begun on this computer and how to list the folder,
// never "on another computer or user account". Positive twin first: with no such run in the log, the
// marker-only line.
func TestMA1050_StatusSaysADeleteBegunHereWasBegunHere(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Name 1")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	ma1048Plant(t, dir) // run 0123456789abcdef
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, "this computer's deletion log has no record of it") {
		t.Fatalf("positive twin:\n%s", got)
	}
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T15:04:05Z", "event": "started", "run": "0123456789abcdef",
		"folder": filepath.Join(filepath.Dir(dir), "Name"), "kind": "plain", "identity": map[string]any{"volume": 1, "file_id": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	got, code := statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED: a delete of this archive begun "+began+" did not finish; it was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name) — run mailarchive delete-archive -out "+encmark.CommandPath(dir)+" (add it first with mailarchive add-archive if it is not in Your archives), or click Delete… on its row in MailArchive Desktop, to finish it.") ||
		strings.Contains(got, "another computer") {
		t.Fatalf("status:\n%s", got)
	}
}
