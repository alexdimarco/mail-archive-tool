package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-955, R37
// Switching away from an archive whose scheduled backup is a run-job asks nothing (the owner's
// ruling for 1.5.3, 2026-10-31: a run-job backs up the folder fixed in its own job file, so a
// switch never affects it, and "keep" was the only right answer). The switch saves, the run-job's
// entry and job file are kept, and no BackupFor is recorded. Positive twin first: a Microsoft 365
// capture, which follows the current archive, still asks (MA-523's words).
func TestSwitchingAwayFromARunJobArchiveAsksNothingAndKeepsIt(t *testing.T) {
	e := newWsEnv(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()

	// Positive twin: a capture installed for c still asks before the location changes.
	fake := &backupFake{tasks: map[string]bool{c: true}}
	useBackupFake(e, fake)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: c}); err != nil {
		t.Fatal(err)
	}
	_, ask := e.post("/api/settings", map[string]any{"out": b})
	if ask["confirm"] != true || !strings.Contains(ask["error"].(string), "will now archive") {
		t.Fatalf("a capture must still ask before a switch: %v", ask)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != c {
		t.Fatalf("a capture's question saved before the answer: out %q", s.Out)
	}

	// The run-job: installed for a, its job file names a; the switch to b asks nothing.
	jp := plantRunJob(t, a)
	fake.tasks = map[string]bool{a: true}
	fake.removed = nil
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	_, body := e.post("/api/settings", map[string]any{"out": b})
	assure.Reached(t, body, "the switch's reply")
	if body["confirm"] == true || body["ok"] != true {
		t.Fatalf("a switch away from a run-job's archive must save with no question: %v", body)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != b || s.BackupFor != "" {
		t.Errorf("after the switch: out %q, BackupFor %q; want %s and no BackupFor", s.Out, s.BackupFor, b)
	}
	if !fake.tasks[a] || len(fake.removed) != 0 {
		t.Errorf("the switch touched A's run-job entry: tasks %v, removed %v", fake.tasks, fake.removed)
	}
	if _, err := os.Stat(jp); err != nil {
		t.Errorf("A's job file is gone after the switch: %v", err)
	}
}

// covers: MA-955
// A Switch to this archive (and Open an archive folder…, and Use this archive) shows what the save
// says beside the button pressed, not in the Settings card further down the page: before it saves,
// the page moves the Settings note and its question into the pressed button's own row or card, and
// Save settings moves them back to the Settings card. (Found by the owner on 1.5.2: the switch's
// question appeared out of sight, so the switch looked as if it had failed.)
func TestTheSwitchShowsItsNoteAndQuestionBesideTheButton(t *testing.T) {
	js := dashboardJS
	fn := strings.Index(js, "function noteBeside(")
	if fn < 0 {
		t.Fatal("the page has no noteBeside: the save's note and question stay in the Settings card")
	}
	body := js[fn:]
	body = body[:strings.Index(body, "\n}")]
	for _, want := range []string{"n=$('settingsNote')", "c=$('settingsConfirm')", "if(n)el.append(n);", "if(c)el.append(c);"} {
		if !strings.Contains(body, want) {
			t.Errorf("noteBeside does not move the Settings note and its question into el: %q is missing", want)
		}
	}
	for _, h := range []struct{ name, start string }{
		{"Switch to this archive", "document.querySelectorAll('[data-switch-archive]')"},
		{"Open an archive folder…", "document.querySelectorAll('[data-open-archive]')"},
		{"Use this archive", "if(useOffer)useOffer.addEventListener('click'"},
		{"Save settings", "if(save)save.addEventListener('click'"},
		// The owner's second report the same night: "Forget this archive does not work for the
		// encrypted archive"; Forget, Pause and Resume wrote to the same out-of-sight note.
		{"Forget this encrypted archive here", "document.querySelectorAll('[data-forget-archive]')"},
		{"Pause (make read-only) and Resume", "const archAction=(sel,path,label,text)=>"},
	} {
		i := strings.Index(js, h.start)
		if i < 0 {
			t.Errorf("%s: handler not found", h.name)
			continue
		}
		handler := js[i:]
		if len(handler) > 600 {
			handler = handler[:600]
		}
		nb, bz := strings.Index(handler, "noteBeside("), strings.Index(handler, "busy(")
		if nb < 0 || bz < 0 || nb > bz {
			t.Errorf("%s: the handler must call noteBeside before it shows its busy note: %s", h.name, handler)
		}
	}
	// A row lays its children out in a wrapping line; the note and question take a line of their own.
	page := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	if !strings.Contains(page, ".archive-row>.note{flex-basis:100%}") {
		t.Error("a note moved into an archive row is not given its own line")
	}
}
