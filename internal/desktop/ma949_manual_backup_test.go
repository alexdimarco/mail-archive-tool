package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/wizard"
)

// "Only when I click Run now" (owner ruling 2026-10-31: "It may be fine to have a manual archive
// where someone updates at their own schedule when they desire to do so", then "yes, 1.5.2";
// MA-949), driven through the Desktop's real handlers with the stand-in scheduler.

const manualOption = `<option value="manual">Only when I click Run now</option>`

// manualReply posts a Settings save of How often and returns its reply.
func (g *a3Rig) manualReply(body string) map[string]any {
	g.t.Helper()
	var r map[string]any
	if err := json.Unmarshal([]byte(g.post(body)), &r); err != nil {
		g.t.Fatal(err)
	}
	return r
}

// descriptorOf is the archive's record, read as status reads it.
func descriptorOf(t *testing.T, out string) schedule.Descriptor {
	t.Helper()
	d, err := schedule.ReadDescriptor(out)
	if err != nil {
		t.Fatalf("the archive's record: %v", err)
	}
	return d
}

// covers: MA-949, R14
// Settings offers "Only when I click Run now" wherever it edits a run-job — on a Mac and on
// Windows — with a line saying nothing runs by itself, and never on a Microsoft 365 capture's
// form, nor on Linux, where Settings does not set up a backup of mail files. Positive twin
// first: the capture's form is drawn, with its How often.
func TestRunNowOnlyIsOfferedWhereSettingsEditsARunJob(t *testing.T) {
	isolateHome(t)
	e := newWsEnv(t)
	page, _ := getPage(t, e.h)
	if !strings.Contains(page, `id="schedInterval"`) {
		t.Fatal("the positive twin: the capture's form is not drawn")
	}
	if strings.Contains(page, `value="manual"`) {
		t.Error("a Microsoft 365 capture's form offers Only when I click Run now")
	}
	for _, goos := range []string{"darwin", "windows"} {
		p := newA3Rig(t, tbProfileArchive(t), goos).page()
		if !strings.Contains(p, manualOption) || !shows(p, manualHint) || !strings.Contains(p, `id="schedManualHint" data-manual-hint hidden`) {
			t.Errorf("%s: the local form does not offer Only when I click Run now with its line", goos)
		}
	}
	if p := newA3Rig(t, tbProfileArchive(t), "linux").page(); strings.Contains(p, `value="manual"`) {
		t.Error("linux: Settings offers Only when I click Run now where it sets up no backup of mail files")
	}
}

// covers: MA-949, MA-950, R14, R18
// Saving "Only when I click Run now" saves the backup — the job file Run now runs, and the
// archive's record of the choice — with no scheduler entry. Positive twin first: Daily on
// another archive installs its entry. The page then shows the choice selected, the Scheduled
// backup card says Run now only in place of a schedule's kind, with Run now and Remove and no
// Repair, its state line names the choice (never "Not installed"), Backup alerts says no alert
// is sent for it, nothing asks Save to put an entry back, and Status & health is GREEN with the
// choice in a note, never "not present in this host's scheduler". Run now runs the backup, and
// still nothing is installed. A 3-day sync streak left from before is removed.
func TestRunNowOnlySavesTheBackupWithNoSchedulerEntry(t *testing.T) {
	isolateHome(t)
	twin := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := twin.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true || len(twin.installs) != 1 || !twin.fake.tasks[twin.out] {
		t.Fatalf("the positive twin: Daily installs its entry: %v", r)
	}

	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if err := state.WriteSyncStreak(g.out, state.SyncStreak{Missed: 4, FirstMissed: time.Now().Add(-96 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if r := g.manualReply(`{"interval":"manual","time":"03:00","drawnFor":` + quoteJSON(g.out) + `}`); r["ok"] != true || r["note"] != nil {
		t.Fatalf("save Only when I click Run now: %v", r)
	}
	if len(g.installs) != 0 || len(g.fake.tasks) != 0 {
		t.Fatalf("a scheduler entry was made: installs %v, tasks %v", g.installs, g.fake.tasks)
	}
	j, err := job.Read(g.jobPath)
	if err != nil || !schedule.SameArchivePath(j.Out, g.out, "darwin") || j.ThunderbirdSync != nil {
		t.Fatalf("the job file Run now runs: %+v %v", j, err)
	}
	d := descriptorOf(t, g.out)
	if !d.IsManual() || d.Name != schedule.DefaultNameFor(g.out) || schedule.NameBound(g.out, d) != nil || !slices.Equal(d.Job, []string{"-job", g.jobPath}) {
		t.Fatalf("the archive's record: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(g.out, state.SyncStreakName)); !os.IsNotExist(err) {
		t.Error("the 3-day sync streak, which counts scheduled runs, was kept")
	}

	page := g.page()
	for _, want := range []string{`<option value="manual" selected>Only when I click Run now</option>`, `id="schedManualHint" data-manual-hint>`,
		`id="runBackupBtn"`, `id="schedRemove"`, `id="navRunNow"`, `id="alertsManual"`, `data-must-post=""`,
		`To back it up now, click <b>Run now</b> on the <a href="#sec-backup">Scheduled backup</a> card.`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	for _, text := range []string{manualKindLine, manualAlertsLine, runNowManualTitle} {
		if !shows(page, text) {
			t.Errorf("the page does not say %q", text)
		}
	}
	for _, not := range []string{`id="schedRepair"`, `id="schedKind"`, `id="settingsAutoPaused"`, `<option value="">No scheduled backup</option>`} {
		if strings.Contains(page, not) {
			t.Errorf("the page shows %s for a Run now only backup", not)
		}
	}
	st := apiGet(g.h, "/api/schedule-state")
	if !strings.Contains(st, `"manual":true`) || !strings.Contains(st, manualStateLine) {
		t.Errorf("the card's state line reads it as not installed: %s", st)
	}
	health := apiGet(g.h, "/api/health")
	if !strings.Contains(health, `"posture":"GREEN"`) || !strings.Contains(health, "this archive is backed up only when you click Run now on the Scheduled backup card") ||
		strings.Contains(health, "not present in this host") || strings.Contains(health, "no schedule is recorded") {
		t.Errorf("Status & health: %s", health)
	}

	a := assure.Reached(t, runJobAndWait(t, g.h, g.csrf, "/api/run-backup"), "Run now's result")
	if !strings.Contains(a, `"error":""`) || !strings.Contains(a, "Backup finished") {
		t.Errorf("Run now on a Run now only backup: %s", a)
	}
	if len(g.installs) != 0 || len(g.fake.tasks) != 0 {
		t.Errorf("Run now made a scheduler entry: %v %v", g.installs, g.fake.tasks)
	}
}

// covers: MA-949, R14
// Switching between "Only when I click Run now" and a schedule installs and removes the
// scheduler entry, and never leaves one running beside a record that says nothing runs by
// itself: Daily → Run now only takes the entry out and keeps the job file as it was; → Weekly
// puts an entry back (Repair returns to the card); → Run now only with the sync ticks the sync
// in the job file and installs nothing.
func TestSwitchingToAndFromRunNowOnlyLeavesNoStaleEntry(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("daily: %v", r)
	}
	g.recordLast()
	jobBefore := fileBytes(g.jobPath)
	if !g.fake.tasks[g.out] {
		t.Fatal("fixture: the daily entry is not installed")
	}

	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("daily → Run now only: %v", r)
	}
	if g.fake.tasks[g.out] || !slices.Contains(g.fake.removed, g.out) {
		t.Fatalf("the daily entry was left running: tasks %v, removed %v", g.fake.tasks, g.fake.removed)
	}
	if !descriptorOf(t, g.out).IsManual() || string(fileBytes(g.jobPath)) != string(jobBefore) {
		t.Fatal("the record does not say Run now only, or the job file changed with only the cadence")
	}

	n := len(g.installs)
	if r := g.manualReply(`{"interval":"weekly","time":"03:00"}`); r["ok"] != true || len(g.installs) != n+1 || !g.fake.tasks[g.out] {
		t.Fatalf("Run now only → weekly: %v (installs %d, tasks %v)", r, len(g.installs), g.fake.tasks)
	}
	if last := g.installs[len(g.installs)-1]; last.Interval != schedule.Weekly || last.Args[0] != "-job" {
		t.Fatalf("not a weekly run-job: %+v", last)
	}
	g.recordLast()
	if p := g.page(); !strings.Contains(p, `id="schedRepair"`) || strings.Contains(p, `id="schedManual"`) {
		t.Error("back on a schedule, the card does not offer Repair again, or still says Run now only")
	}

	n = len(g.installs)
	if r := g.manualReply(`{"interval":"manual","time":"03:00","presync":true}`); r["ok"] != true || r["note"] != nil {
		t.Fatalf("weekly → Run now only with the sync: %v", r)
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Fatalf("the sync is not in the job file Run now runs: %+v %v", j, err)
	}
	if len(g.installs) != n || g.fake.tasks[g.out] || !descriptorOf(t, g.out).IsManual() {
		t.Fatalf("Run now only with the sync made an entry, or left one: installs %d, tasks %v", len(g.installs), g.fake.tasks)
	}
	if r := g.manualReply(`{"interval":"manual","time":"03:00","presync":false}`); r["ok"] != true {
		t.Fatalf("the sync switched off: %v", r)
	}
	if j, _ := job.Read(g.jobPath); j.ThunderbirdSync != nil || len(g.installs) != n {
		t.Fatal("the sync is still on, or switching it off made an entry")
	}
}

// covers: MA-949, R14, R12
// What would leave a scheduler entry running beside a Run now only record, or turn the choice
// into a schedule, is refused with nothing changed. Positive twins first: a Daily backup's entry
// is taken out when Run now only is saved, and a Daily backup's Repair reinstalls its entry.
// When the entry can't be taken out, the save is refused in plain words — the entry, the record
// and the job file as they were. Repair, posted for a Run now only backup (the card shows none),
// is refused: there is no schedule to repair. On Windows, where Run now does not start
// Thunderbird yet, saving it with the sync says what to do instead, and Run now's tooltip too.
func TestRunNowOnlyRefusesWhatWouldLeaveAnEntryRunning(t *testing.T) {
	isolateHome(t)
	failRemove := false
	g := newA3Rig(t, tbProfileArchive(t), "darwin", func(c *Config) {
		fake := c.RemoveBackupFor
		c.RemoveBackupFor = func(out string) error {
			if failRemove {
				return errors.New("access is denied")
			}
			return fake(out)
		}
	})
	daily := func() {
		if r := g.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true {
			t.Fatalf("daily: %v", r)
		}
		g.recordLast()
	}
	daily()
	n := len(g.installs)
	if r := g.manualReply(`{}`); r["ok"] != true || len(g.installs) != n+1 {
		t.Fatalf("the positive twin: a daily backup's Repair: %v", r)
	}
	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true || g.fake.tasks[g.out] {
		t.Fatalf("the positive twin: the entry is taken out: %v", r)
	}
	daily()

	failRemove = true
	u := g.snapshot()
	r := g.manualReply(`{"interval":"manual","time":"04:00"}`)
	msg, _ := r["error"].(string)
	rc := 0
	if r["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("The backup schedule was not changed: its scheduler entry could not be taken out (access is denied), so it still runs as before."),
		assure.NoSideEffect(u.holds))
	if descriptorOf(t, g.out).IsManual() {
		t.Error("the record says Run now only while the entry still runs")
	}

	failRemove = false
	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("Run now only: %v", r)
	}
	u = g.snapshot()
	r = g.manualReply(`{"drawnFor":` + quoteJSON(g.out) + `}`) // the card's Repair, posted as the script posts it
	msg, _ = r["error"].(string)
	rc = 0
	if r["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names(wizard.ManualNoRepair), assure.NoSideEffect(u.holds))

	win := newA3Rig(t, tbProfileArchive(t), "windows")
	if r := win.manualReply(`{"interval":"manual","time":"03:00","presync":true}`); r["ok"] != true || r["note"] != manualSyncStartOffNote {
		t.Fatalf("Windows, Run now only with the sync: %v", r)
	}
	if p := win.page(); !shows(p, runNowManualStartOffTitle) {
		t.Error("Windows: Run now's tooltip does not say what to do while it does not start Thunderbird")
	}
}

// covers: MA-949, R38
// Where Run now does not start Thunderbird yet, a Run now only backup's end note never names
// scheduled backups it does not have. Positive twin first: a Daily backup's Run now says
// scheduled backups still sync Thunderbird first; Run now only says to open Thunderbird first.
func TestRunNowOnlysNoteNamesNoSchedule(t *testing.T) {
	isolateHome(t)
	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	defer restore()
	for _, c := range []struct{ interval, want, not string }{
		{"daily", startNotAllowedLine, startNotAllowedManualLine},
		{"manual", startNotAllowedManualLine, "Scheduled backups still sync"},
	} {
		g := newA3Rig(t, tbProfileArchive(t), "darwin")
		if r := g.manualReply(`{"interval":"` + c.interval + `","time":"04:00","presync":true}`); r["ok"] != true {
			t.Fatalf("%s with the sync: %v", c.interval, r)
		}
		if c.interval == "daily" {
			g.recordLast()
		}
		a := assure.Reached(t, runJobAndWait(t, g.h, g.csrf, "/api/run-backup"), "Run now's end")
		if !strings.Contains(a, c.want) || strings.Contains(a, c.not) {
			t.Errorf("%s: Run now's end does not say %q, or says %q: %s", c.interval, c.want, c.not, a)
		}
	}
}

// covers: MA-949, R14
// Remove removes a Run now only backup: its record and its job file go, nothing is said to be
// left, and Settings offers No scheduled backup again with no Run now. Positive twin first:
// Remove of a Daily backup takes out its entry and job file.
func TestRemoveRemovesTheRunNowOnlyBackup(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("daily: %v", r)
	}
	g.recordLast()
	if body := g.removePost(g.out); !strings.Contains(body, `"removed":true`) || g.fake.tasks[g.out] || fileBytes(g.jobPath) != nil {
		t.Fatalf("the positive twin: Remove of a daily backup: %s", body)
	}

	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("Run now only: %v", r)
	}
	body := g.removePost(g.out)
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, `"removed":true`) || strings.Contains(body, `"note"`) {
		t.Fatalf("Remove of a Run now only backup: %s", body)
	}
	if _, err := os.Stat(filepath.Join(g.out, schedule.DescriptorName)); !os.IsNotExist(err) || fileBytes(g.jobPath) != nil {
		t.Fatal("Remove left the record or the job file")
	}
	if p := g.page(); !strings.Contains(p, `<option value="" selected>No scheduled backup</option>`) || strings.Contains(p, `id="runBackupBtn"`) {
		t.Error("after Remove, Settings does not start from No scheduled backup, or Run now is still shown")
	}
}

// covers: MA-949, MA-945, X11
// The guide and the release notes say what "Only when I click Run now" does, where Settings
// offers it: on a Mac and on Windows, never on Linux.
func TestTheGuideNamesRunNowOnly(t *testing.T) {
	const line = "For an archive made from mail files, How often can also be Only when I click Run now: nothing then runs by itself, and the backup runs each time you click Run now on the Scheduled backup card."
	for goos, want := range map[string]bool{"darwin": true, "windows": true, "linux": false} {
		h := readHelpCard(t, getOverview(t, Config{GOOS: goos, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}))
		if got := strings.Contains(h.text, line); got != want {
			t.Errorf("%s: the guide says it %v, want %v", goos, got, want)
		}
	}
	notes, err := os.ReadFile(filepath.Join("..", "..", "packaging", "release-notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notes), "- **Back up only when you click Run now.** On a Mac and on Windows, **How often** in Settings now offers **Only when I click Run now**") {
		t.Error("the release notes' New in 1.5.2 does not name Only when I click Run now")
	}
}

func quoteJSON(s string) string { b, _ := json.Marshal(s); return string(b) }
