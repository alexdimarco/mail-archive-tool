package desktop

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/health"
)

// covers: MA-1068, R36
// DM-27's words on the Desktop (design-archive-delete §15.4; the slice-5 adversarial pass's LOW-4;
// lead's words, owner-delegated 2026-11-02): this Desktop's deletion log has a delete of the current
// archive's folder that wrote its `marked` line, and the folder holds its archive with no note (a
// rotation drive, or the note removed by hand). The row, Status & health and Archive now's refusal
// name both ways out — plug in the drive the delete began on and finish it there, or finish it here —
// never "click Delete again". Positive twin first: the same started line with no `marked` line keeps
// the log's own words.
func TestMA1068_TheDesktopNamesTheDriveTheDeleteBeganOn(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Rotation")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	lp := e.cfg.deletionsLogPath()
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	line := func(m map[string]any) {
		t.Helper()
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(lp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, werr := f.Write(append(b, '\n'))
		if cerr := f.Close(); werr != nil || cerr != nil {
			t.Fatal(werr, cerr)
		}
	}
	const run = "d1068aaaaaaaaaaa"
	line(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": run, "folder": dir, "row": dir, "kind": "plain"})

	// Positive twin: no `marked` line.
	if posture, summary := ma1049Health(t, e); posture != "RED" || !strings.Contains(summary, "RED: "+health.DeleteUnfinishedLine(dir, "desktop")) {
		t.Fatalf("positive twin, Status & health: %s\n%s", posture, summary)
	}
	if row := html.UnescapeString(rowFor(t, e.page(), dir)); !strings.Contains(row, "Delete not finished: click Delete again") {
		t.Fatalf("positive twin, the row: %s", row)
	}
	if got := e.d.unfinishedRefusal(dir); got != runUnfinishedText {
		t.Fatalf("positive twin, Archive now's refusal: %q", got)
	}

	line(map[string]any{"at": "2026-11-02T10:00:01Z", "event": "marked", "run": run, "folder": dir, "first_run": run})
	posture, summary := ma1049Health(t, e)
	if posture != "RED" || !strings.Contains(summary, "RED: "+health.DeleteDisagreesLine(dir, "desktop")) || !strings.Contains(summary, "plug in the drive the delete began on") {
		t.Fatalf("Status & health: %s\n%s", posture, summary)
	}
	row := html.UnescapeString(rowFor(t, e.page(), dir))
	if !strings.Contains(row, "Delete not finished: a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left. If this is a different drive, plug in the drive the delete began on and finish the delete there; if it is the same drive and the note was removed, click Delete… to finish it.") ||
		strings.Contains(row, "click Delete again") {
		t.Fatalf("the row: %s", row)
	}
	if got := e.d.unfinishedRefusal(dir); got != archdelete.DisagreesWords(delmark.WhatNotBackedUp) {
		t.Fatalf("Archive now's refusal: %q", got)
	}
}
