package desktop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-272
// The Scheduled backup tile says each of its states in plain words (the friction review
// of the 1.5.2 rulings, 2026-10-31, finding 3). Positive twin first: with an archive
// chosen, an installed schedule reads "Installed" and a missing one "Not installed", in
// the warning colour, as before. With no archive chosen it reads "Not set up yet: choose
// an archive first" with no warning colour, as the Microsoft 365 tile already does
// (MA-938): a fresh Desktop has nothing to schedule yet, and a schedule the wizard just
// made follows its archive, not this page. A scheduler that does not answer is named as
// such: "Couldn't check: the computer's scheduler didn't answer", never "Scheduler
// unavailable".
func TestScheduledBackupTileSaysEachStateInPlainWords(t *testing.T) {
	tile := func(cfg Config) card {
		t.Helper()
		for _, c := range cfg.cards() {
			if c.Label == "Scheduled backup" {
				return c
			}
		}
		t.Fatal("no Scheduled backup tile")
		return card{}
	}
	out := t.TempDir()
	base := func() Config {
		return Config{Out: out, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}
	}

	// Positive twin: an archive is chosen.
	inst := base()
	inst.BackupInstalledFor = func(string) bool { return true }
	if got := tile(inst); got != (card{"Scheduled backup", "Installed", "good"}) {
		t.Errorf("an installed schedule: tile %+v", got)
	}
	none := base()
	none.BackupInstalledFor = func(string) bool { return false }
	if got := tile(none); got != (card{"Scheduled backup", "Not installed", "warn"}) {
		t.Errorf("an archive with no schedule: tile %+v", got)
	}

	// No archive chosen: nothing to schedule yet, and no warning colour.
	fresh := Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}
	fresh.BackupInstalledFor = func(string) bool { return false }
	got := tile(fresh)
	assure.Reached(t, got.Value, "the fresh Desktop's Scheduled backup tile")
	if got != (card{"Scheduled backup", "Not set up yet: choose an archive first", ""}) {
		t.Errorf("a fresh Desktop's Scheduled backup tile is %+v, want \"Not set up yet: choose an archive first\" with no warning colour", got)
	}

	// The scheduler does not answer.
	down := base()
	down.BackupInstalledFor = func(string) bool { return false }
	down.EntryState = func(string) schedule.State { return schedule.SchedulerUnavailable }
	if got := tile(down); got != (card{"Scheduled backup", "Couldn't check: the computer's scheduler didn't answer", "warn"}) {
		t.Errorf("a scheduler that does not answer: tile %+v", got)
	}
}

// covers: MA-272
// The Scheduled backup card's state line agrees with the tile on a fresh Desktop (the 1.5.2
// Windows smoke check of Make a new archive…, 2026-10-31: with no archive chosen the tile read
// "Not set up yet: choose an archive first" while the card below it still read "Not installed.").
// Positive twin first: with an archive chosen, the state line is drawn from the scheduler as
// before. With none chosen, the state endpoint says so and the page leaves the line hidden; the
// card's own sentence ("Choose an archive location in Settings first…") is what it shows.
func TestScheduledBackupCardSaysNotInstalledOnlyForAnArchive(t *testing.T) {
	state := func(cfg Config) string {
		t.Helper()
		d := &dashboard{cfg: cfg, csrf: "x", cap: &captureState{}}
		rec := httptest.NewRecorder()
		d.scheduleState(rec, httptest.NewRequest(http.MethodGet, "/api/schedule-state", nil))
		return rec.Body.String()
	}

	chosen := Config{Out: t.TempDir(), ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}
	chosen.EntryState = func(string) schedule.State { return schedule.NotInstalled }
	got := assure.Reached(t, state(chosen), "the state line's answer with an archive chosen")
	if !strings.Contains(got, `"state":"not installed"`) || strings.Contains(got, `"noArchive"`) {
		t.Errorf("with an archive chosen the state line must come from the scheduler: %s", got)
	}

	fresh := Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}
	got = assure.Reached(t, state(fresh), "the state line's answer with no archive chosen")
	if !strings.Contains(got, `"noArchive":true`) {
		t.Errorf("with no archive chosen the state endpoint must say so: %s", got)
	}
	// The page script hides the line for that answer before it writes any state into it.
	js := dashboardJS[strings.Index(dashboardJS, "async function schedState("):]
	hide, show := strings.Index(js, "if(d.noArchive){n.hidden=true;return;}"), strings.Index(js, "n.hidden=false;")
	if hide < 0 || show < 0 || hide > show {
		t.Errorf("schedState must hide its line when no archive is chosen, before it shows any state")
	}
}
