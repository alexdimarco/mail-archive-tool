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

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
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
// another archive installs its entry. The page then shows the choice selected, the Overview's
// Scheduled backup tile names the choice in the neutral colour (before the save it said Not
// installed in the warning colour), the Scheduled
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
	// The Overview's Scheduled backup tile, before any backup is saved: not installed, in the
	// warning colour (the twin of the tile below, which must not read the choice that way).
	const notInstalledTile = `<div class="label">Scheduled backup</div><div class="value warn">Not installed</div>`
	manualTile := `<div class="label">Scheduled backup</div><div class="value ">` + manualTileValue + `</div>`
	if p := g.page(); !strings.Contains(p, notInstalledTile) {
		t.Fatal("the twin: with no backup saved, the Overview's Scheduled backup tile does not say Not installed")
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
	if !strings.Contains(page, manualTile) {
		t.Errorf("the Overview's Scheduled backup tile does not say %q in the neutral colour", manualTileValue)
	}
	for _, not := range []string{`id="schedRepair"`, `id="schedKind"`, `id="settingsAutoPaused"`, `<option value="">No scheduled backup</option>`, notInstalledTile} {
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
// is refused: there is no schedule to repair. On Windows, with the tables as shipped (Run now
// starts Thunderbird since lab MA-918 run 2), saving it with the sync adds no note and Run
// now's tooltip is the Windows sync text; with Windows' start entry off, saving it with the
// sync says what to do instead, and Run now's tooltip too.
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

	// Windows, the tables as shipped: Run now starts Thunderbird there since lab MA-918 run 2,
	// so the save says nothing more and Run now's tooltip is the Windows sync text, whole.
	if !presync.AttendedStartAllowed("windows") {
		t.Fatal("Windows' start entry, as shipped, is not on")
	}
	win := newA3Rig(t, tbProfileArchive(t), "windows")
	if r := win.manualReply(`{"interval":"manual","time":"03:00","presync":true}`); r["ok"] != true || r["note"] != nil {
		t.Fatalf("Windows, Run now only with the sync, as shipped: %v", r)
	}
	if p := win.page(); !shows(p, runNowTitle("windows", true)) || shows(p, runNowManualStartOffTitle) || shows(p, runNowStartOffTitle) {
		t.Error("Windows, as shipped: Run now's tooltip is not the Windows sync text")
	}
	// Windows with its start entry off: saving it with the sync says what to do instead, and
	// Run now's tooltip too.
	restoreWin := presync.OverrideTablesForTest(nil, map[string]bool{"windows": false})
	defer restoreWin()
	off := newA3Rig(t, tbProfileArchive(t), "windows")
	if r := off.manualReply(`{"interval":"manual","time":"03:00","presync":true}`); r["ok"] != true || r["note"] != manualSyncStartOffNote {
		t.Fatalf("Windows, start entry off, Run now only with the sync: %v", r)
	}
	if p := off.page(); !shows(p, runNowManualStartOffTitle) {
		t.Error("Windows, start entry off: Run now's tooltip does not say what to do while it does not start Thunderbird")
	}
}

// covers: MA-949, R14, R12
// When the scheduler can't say whether the archive's entry is there (Windows' schtasks failing
// without its usual answer), saving "Only when I click Run now" still tries to take the entry
// out: only a definite "not installed" lets the save skip it. Positive twin first: an archive
// with no entry saves it without asking the scheduler to remove anything. With a Daily entry and
// a scheduler that can't answer, a removal that fails refuses the save in plain words — the
// entry, the record and the job file as they were — and the Overview's tile and Status & health
// still read the entry as there; a removal that works saves it.
func TestRunNowOnlyTakesTheEntryOutWhenTheSchedulerCantSay(t *testing.T) {
	isolateHome(t)
	failRemove := false
	mod := func(c *Config) {
		fake := c.RemoveBackupFor
		c.RemoveBackupFor = func(out string) error {
			if failRemove {
				return errors.New("the Task Scheduler service is not available")
			}
			return fake(out)
		}
	}
	twin := newA3Rig(t, tbProfileArchive(t), "windows", mod)
	if r := twin.manualReply(`{"interval":"manual","time":"03:00"}`); r["ok"] != true || len(twin.fake.removed) != 0 {
		t.Fatalf("the positive twin: no entry, nothing to take out: %v, removed %v", r, twin.fake.removed)
	}

	g := newA3Rig(t, tbProfileArchive(t), "windows", mod)
	if r := g.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true || !g.fake.tasks[g.out] {
		t.Fatalf("daily: %v", r)
	}
	g.recordLast()
	g.fake.unknown = true // the entry is there; the scheduler can't say so
	failRemove = true
	u := g.snapshot()
	r := g.manualReply(`{"interval":"manual","time":"04:00"}`)
	msg, _ := r["error"].(string)
	rc := 0
	if r["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("The backup schedule was not changed: its scheduler entry could not be taken out (the Task Scheduler service is not available), so it still runs as before."),
		assure.NoSideEffect(u.holds))
	if descriptorOf(t, g.out).IsManual() {
		t.Error("the record says Run now only while the entry may still run")
	}

	failRemove = false
	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true || g.fake.tasks[g.out] || !slices.Contains(g.fake.removed, g.out) {
		t.Fatalf("a removal that works: %v, tasks %v, removed %v", r, g.fake.tasks, g.fake.removed)
	}
	if !descriptorOf(t, g.out).IsManual() {
		t.Error("the record does not say Run now only after the entry was taken out")
	}
}

// covers: MA-949, R14
// A Run now only record with its scheduler entry still there (put back by hand, say) is not
// shown as the person's choice on the Overview: its tile says the entry is installed (Status &
// health's warning for it is proven in internal/health). Positive twin first: with no entry,
// the tile names the choice.
func TestRunNowOnlyWithItsEntryStillThereIsShown(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"manual","time":"03:00"}`); r["ok"] != true {
		t.Fatalf("Run now only: %v", r)
	}
	if p := g.page(); !strings.Contains(p, `<div class="value ">`+manualTileValue+`</div>`) {
		t.Fatal("the positive twin: the tile does not name the choice")
	}
	g.fake.tasks[g.out] = true // an entry put back by hand
	if p := g.page(); !strings.Contains(p, `<div class="label">Scheduled backup</div><div class="value good">Installed</div>`) {
		t.Error("with its entry there, the tile does not say Installed")
	}
}

// covers: MA-949, R14
// A Run now only backup whose settings file is gone (deleted, or the archive opened on another
// computer) is told as the backup it is, never as a scheduled backup: the card names the choice
// and offers Repair, with no "scheduled backup:" kind line, and Repair makes the job file again
// from the archive's own record, keeps the choice and makes no scheduler entry. A damaged
// settings file is told the same way. Positive twin first: a Daily backup's missing file is told
// as its scheduled backup, with its kind line.
func TestRunNowOnlyWithItsSettingsFileMissingNamesNoSchedule(t *testing.T) {
	isolateHome(t)
	twin := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := twin.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("daily: %v", r)
	}
	twin.recordLast()
	os.Remove(twin.jobPath)
	if p := twin.page(); !shows(p, "This archive's scheduled backup can't run: its settings file "+twin.jobPath+" is missing.") || !strings.Contains(p, `id="schedKind"`) {
		t.Fatal("the positive twin: a Daily backup's missing settings file is not told as its scheduled backup")
	}

	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"manual","time":"03:00"}`); r["ok"] != true {
		t.Fatalf("Run now only: %v", r)
	}
	os.Remove(g.jobPath)
	page := g.page()
	if !shows(page, "This archive's backup (Only when I click Run now) can't run: its settings file "+g.jobPath+" is missing. Repair makes it again from this archive's own record.") {
		t.Error("the missing settings file is not told as the Run now only backup it is")
	}
	if !strings.Contains(page, `id="schedRepair"`) || strings.Contains(page, `id="schedKind"`) || shows(page, "This archive's scheduled backup") {
		t.Error("the card offers no Repair, or calls the Run now only backup a scheduled backup")
	}
	body := g.post(`{}`)
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, recreatedNote) {
		t.Fatalf("the Repair: %s", body)
	}
	if _, err := job.Read(g.jobPath); err != nil {
		t.Errorf("the Repair did not make the job file again: %v", err)
	}
	if !descriptorOf(t, g.out).IsManual() || len(g.installs) != 0 || len(g.fake.tasks) != 0 {
		t.Errorf("the Repair did not keep Run now only, or made an entry: installs %v, tasks %v", g.installs, g.fake.tasks)
	}

	if err := os.WriteFile(g.jobPath, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := g.page(); !shows(p, "This archive's backup (Only when I click Run now) can't run. This archive's backup settings file "+g.jobPath+" can't be read") ||
		strings.Contains(p, `id="schedKind"`) || shows(p, "This archive's scheduled backup") {
		t.Error("a damaged settings file is told as a scheduled backup")
	}
}

// covers: MA-949, R38
// Where Run now does not start Thunderbird yet, a Run now only backup's end note never names
// scheduled backups it does not have. Positive twin first: a Daily backup's Run now says
// scheduled backups still sync Thunderbird first; Run now only says to open Thunderbird first.
// The Thunderbird session is a scripted fake through presyncjob's seam, so the run reaches the
// start check on every build (Linux has no pre-sync adapter of its own), and it is never called.
func TestRunNowOnlysNoteNamesNoSchedule(t *testing.T) {
	isolateHome(t)
	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	defer restore()
	tb := newTBScript()
	t.Cleanup(presyncjob.OverrideSessionForTest(tb.run, filepath.Join(t.TempDir(), "presync")))
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
	if n := tb.calls(); n != 0 {
		t.Errorf("Thunderbird was started %d times where Run now does not start it", n)
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

// covers: MA-949, R14
// Remove's busy words name "the backup", not "the scheduled backup", where the backup runs only
// when Run now is clicked (the manual-only review's leftover, applied by the lead 2026-10-31).
// Positive twin first: a Daily backup's Remove keeps "Removing the scheduled backup…" (the
// script's own words, with no data-busy on its button).
func TestRemovesBusyWordsNameNoScheduleForRunNowOnly(t *testing.T) {
	isolateHome(t)
	const removeBtn = `id="schedRemove"`
	const busyAttr = `data-busy="Removing the backup…"`
	if !strings.Contains(dashboardJS, `busy(sr,'Removing…',$('schedState'),sr.dataset.busy||'Removing the scheduled backup…'),'Removed.')`) {
		t.Error("the page script does not take Remove's busy words from its button, with the scheduled words as the default")
	}
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"daily","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("daily: %v", r)
	}
	if p := g.page(); !strings.Contains(p, removeBtn) || strings.Contains(p, "data-busy=") {
		t.Fatal("the positive twin: a Daily backup's Remove is not shown, or carries busy words of its own")
	}
	if r := g.manualReply(`{"interval":"manual","time":"04:00"}`); r["ok"] != true {
		t.Fatalf("Run now only: %v", r)
	}
	if p := g.page(); !strings.Contains(p, removeBtn+` `+busyAttr) {
		t.Errorf("a Run now only backup's Remove does not say %q while it works", "Removing the backup…")
	}
}

// covers: MA-949, MA-692
// The source-lost banner on Your archives says what to do for an archive backed up only when
// Run now is clicked: nothing resumes by itself, so it says to click Run now once the source is
// back (the manual-only review's leftover, applied by the lead 2026-10-31). Positive twin first:
// a scheduled archive's banner keeps "to resume backups". The record is read with the bounded,
// link-refusing read the source streak uses. The Scheduled backup card shows only the current
// archive ("The one marked current is the one this page shows and changes"), so a lost archive
// that is not the current one is told to click Switch to this archive first: a Run now on the
// card it can see would back up the current archive instead (review finding, 2026-10-31).
// Each case is drawn as the current archive and as another one beside it.
func TestSourceLostBannerSaysRunNowForRunNowOnly(t *testing.T) {
	lost := func(interval string) string {
		dir := t.TempDir()
		writeManifest(t, dir)
		if err := state.WriteSourceStreak(dir, state.SourceStreak{
			ConsecutiveFails: 2,
			FirstUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC),
			LastUnreachable:  time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC).Add(state.SurfaceAfter),
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, schedule.DescriptorName), []byte(`{"interval":"`+interval+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	const scheduledWords = "— reconnect the source (its mailbox or mail folder) to resume backups."
	const runNowWords = "— reconnect the source (its mailbox or mail folder), then click Run now on its Scheduled backup card: its backup runs only when you click Run now."
	const switchWords = "— reconnect the source (its mailbox or mail folder), then click Switch to this archive and click Run now on its Scheduled backup card: its backup runs only when you click Run now."
	for _, c := range []struct {
		interval string
		current  bool
		want     string
		not      []string
	}{
		{"daily", true, scheduledWords, []string{runNowWords, switchWords}}, // the positive twins
		{"daily", false, scheduledWords, []string{runNowWords, switchWords}},
		{"manual", true, runNowWords, []string{scheduledWords, switchWords}},
		{"manual", false, switchWords, []string{scheduledWords, runNowWords}},
	} {
		dir := lost(c.interval)
		other := t.TempDir() // a second archive, so Your archives is drawn either way
		writeManifest(t, other)
		idxPath := filepath.Join(t.TempDir(), archindex.Name)
		if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: dir}, {Path: other}}}); err != nil {
			t.Fatal(err)
		}
		settings := filepath.Join(t.TempDir(), "s.json")
		current := other
		if c.current {
			current = dir
		}
		if err := SaveSettings(settings, Settings{Out: current}); err != nil {
			t.Fatal(err)
		}
		cfg := Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}
		rows := cfg.archiveRows(archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: dir}}})
		if len(rows) != 1 || rows[0].Current != c.current || rows[0].SourceLostSince == "" {
			t.Fatalf("%s, current %v: the lost archive's row %+v", c.interval, c.current, rows)
		}
		page, _ := getPage(t, DashboardHandler(cfg))
		if !strings.Contains(page, "hasn't been reachable since") {
			t.Fatalf("%s, current %v: no source-lost banner", c.interval, c.current)
		}
		if !strings.Contains(page, c.want) {
			t.Errorf("%s, current %v: the banner does not say %q", c.interval, c.current, c.want)
		}
		for _, n := range c.not {
			if strings.Contains(page, n) {
				t.Errorf("%s, current %v: the banner says %q", c.interval, c.current, n)
			}
		}
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
