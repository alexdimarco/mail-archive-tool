package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// manualInput is a run-job archive whose descriptor records "Only when I click Run now"
// (MA-949): no scheduler entry, its job file corroborated on this computer.
func manualInput(t *testing.T, now time.Time, goos, surface string, sync bool) Input {
	t.Helper()
	jp := filepath.Join(t.TempDir(), "mailarchive-1234abcd.json")
	j := job.Job{Name: "mailarchive-1234abcd", Inputs: []string{"/Users/a/Mail"}, Out: "/a", Mode: "incremental"}
	if sync {
		j.ThunderbirdSync = &job.ThunderbirdSync{}
	}
	if err := job.Write(jp, j); err != nil {
		t.Fatal(err)
	}
	in := healthyInput(now)
	in.GOOS, in.Surface = goos, surface
	in.Desc.Interval, in.Desc.Job = string(schedule.Manual), []string{"-job", jp}
	in.SchedState = schedule.NotInstalled // nothing is installed: the person's choice
	in.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: jp}
	return in
}

// scheduleFaults are the codes that read a backup as a missing or broken schedule.
var scheduleFaults = []string{"not_installed", "stale", "never_ran", "exe_missing", "exe_moved", "exe_version_skew",
	"scheduler_unavailable", "other_host", "presync_not_synced", "presync_nothing_arrived", "no_schedule", "manual_job_missing"}

// covers: MA-950, MA-949, R18, X6
// status and Status & health treat "Only when I click Run now" as the person's choice, not a
// missing or broken schedule (owner ruling 2026-10-31, 1.5.2). Positive twin first: the same
// archive with a daily schedule whose entry is gone, last run 10 days ago, a program moved and a
// 3-day sync streak is WARN for each. Recorded as Run now only (its job file corroborated), it is
// GREEN: no "not installed", stale, never ran, program, other host or 3-day sync warning, only a
// note naming the choice in each surface's words (the command line's points to Run now in
// MailArchive Desktop, never to `run-job`, which runs as the schedule does);
// status's Schedule line says it runs only on Run now. Its sync note promises the sync only where
// Run now starts Thunderbird. A record saying so with no job file on this computer warns that Run
// now has nothing to run.
func TestRunNowOnlyIsTheOwnersChoiceInStatus(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	t0 := now.Add(-10 * 24 * time.Hour)
	streak := state.SyncStreak{}
	for d := 0; d <= 3; d++ {
		streak = streak.Miss("owner-active-skipped", t0.Add(time.Duration(d)*24*time.Hour))
	}
	nothing := state.SyncStreak{}.NoChangeSeen(t0).NoChangeSeen(t0.Add(24 * time.Hour)).NoChangeSeen(t0.Add(3 * 24 * time.Hour))
	age := func(in *Input) {
		in.LastRun.Started, in.LastRun.Finished = t0, t0.Add(time.Hour)
		in.SyncStreak = streak
		in.ExeIsThis = false // the recorded program moved
	}

	// Positive twin: a daily schedule in the same state warns.
	daily := manualInput(t, now, "darwin", "", true)
	daily.Desc.Interval = string(schedule.Daily)
	age(&daily)
	rep := Assess(daily, now)
	for _, code := range []string{"not_installed", "stale", "exe_moved", "presync_not_synced"} {
		if !hasCode(rep, code) {
			t.Errorf("the positive twin: a daily schedule in this state has no %s (%v)", code, rep.Codes)
		}
	}

	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": true, "windows": false})
	defer restore()
	for _, surface := range []string{"", "desktop", "gui"} {
		in := manualInput(t, now, "darwin", surface, true)
		age(&in)
		rep := Assess(in, now)
		for _, code := range scheduleFaults {
			if hasCode(rep, code) {
				t.Errorf("surface %q: Run now only reads as a broken schedule: %s %q", surface, code, reasonWith(rep, code))
			}
		}
		if rep.Posture != "GREEN" {
			t.Errorf("surface %q: posture %s (%v)", surface, rep.Posture, rep.Codes)
		}
		note := assure.Reached(t, reasonWith(rep, "manual_backup"), "the Run now only note")
		if !strings.Contains(note, "this archive is backed up only when you click Run now") ||
			!strings.Contains(note, "that is its How often setting (Only when I click Run now), so nothing runs by itself and no scheduled backup is expected") {
			t.Errorf("surface %q: the note %q", surface, note)
		}
		switch surface {
		case "":
			// Run now in MailArchive Desktop, and never `run-job`: that is the scheduled path,
			// which sends the scheduled backup's alerts and counts the 3-day sync streak.
			if !strings.Contains(note, "only when you click Run now in MailArchive Desktop") ||
				strings.Contains(note, "run-job") || strings.Contains(note, "`") || strings.Contains(note, in.KindEv.JobPath) {
				t.Errorf("the command line's note: %q", note)
			}
		case "desktop":
			if !strings.Contains(note, "click Run now on the Scheduled backup card") || strings.Contains(note, "`") {
				t.Errorf("the Desktop's note: %q", note)
			}
		}
		if got, want := reasonWith(rep, "presync_syncs"), "this archive's backup syncs Thunderbird first each time you click Run now: it starts Thunderbird, even while you are using the computer, waits for its mail, then backs up, and leaves Thunderbird open for you to quit"; got != want {
			t.Errorf("surface %q: the sync note %q, want %q", surface, got, want)
		}
		if !summaryHas(in, rep, manualScheduleLine) || summaryHas(in, rep, "Program:") || summaryHas(in, rep, "not installed") {
			t.Errorf("surface %q: status's Schedule line: %v", surface, Summary(in, rep))
		}
	}
	// The softer warning is the scheduled sessions' too.
	soft := manualInput(t, now, "darwin", "desktop", true)
	soft.SyncStreak = nothing
	if rep := Assess(soft, now); hasCode(rep, "presync_nothing_arrived") {
		t.Error("Run now only carries the 3-day nothing-arrived warning")
	}
	// No run yet is no fault either: nothing runs until Run now is clicked.
	fresh := manualInput(t, now, "darwin", "desktop", false)
	fresh.LastRunState, fresh.LastRun = state.LastRunAbsent, state.LastRun{}
	if rep := Assess(fresh, now); hasCode(rep, "never_ran") || hasCode(rep, "presync_syncs") {
		t.Errorf("a Run now only archive with no run yet: %v", rep.Codes)
	}

	// Where Run now does not start Thunderbird yet, the note says what to do instead.
	win := manualInput(t, now, "windows", "desktop", true)
	if got, want := reasonWith(Assess(win, now), "presync_syncs"), "this archive's backup is set to sync Thunderbird first, but on this computer Run now does not start Thunderbird yet: open Thunderbird and let its mail arrive before you click Run now"; got != want {
		t.Errorf("Windows' sync note %q, want %q", got, want)
	}

	// The record alone, with no job file here: Run now has nothing to run.
	lost := manualInput(t, now, "darwin", "", false)
	lost.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{}}
	got := reasonWith(Assess(lost, now), "manual_job_missing")
	if !strings.Contains(got, "this archive is set to back up only when you click Run now, but its backup settings file is not on this computer, so Run now has nothing to run — open MailArchive Desktop on this computer") || hasCode(Assess(lost, now), "manual_backup") {
		t.Errorf("a record with no job file: %q", got)
	}
}

// covers: MA-950, MA-949, R18
// A record saying "Only when I click Run now" while a scheduler entry for the archive is still
// there (its removal skipped, or the entry put back by hand) is not the person's choice: the
// entry runs the backup by itself. Positive twin first: with no entry, and with a scheduler that
// can't answer, it is the GREEN note. With the entry installed it warns, in each surface's words,
// how to take the entry out, and the note saying nothing runs by itself is not shown.
func TestRunNowOnlyWithAnEntryStillThereWarns(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, st := range []schedule.State{schedule.NotInstalled, schedule.SchedulerUnavailable} {
		in := manualInput(t, now, "darwin", "desktop", false)
		in.SchedState = st
		rep := Assess(in, now)
		if hasCode(rep, "manual_entry_running") || !hasCode(rep, "manual_backup") || rep.Posture != "GREEN" {
			t.Fatalf("the positive twin (%v): %s %v", st, rep.Posture, rep.Codes)
		}
	}
	for surface, remedy := range map[string]string{
		"desktop": "in Settings, choose Only when I click Run now and click Save settings again to take the entry out",
		"":        "open MailArchive Desktop on this computer and, in Settings, choose Only when I click Run now and click Save settings again to take the entry out",
	} {
		in := manualInput(t, now, "darwin", surface, false)
		in.SchedState = schedule.Installed
		rep := Assess(in, now)
		got := assure.Reached(t, reasonWith(rep, "manual_entry_running"), "the entry-still-there warning")
		want := "WARN: a scheduler entry still runs this archive's backup by itself, although its How often is Only when I click Run now — " + remedy
		if got != want || rep.Posture != "WARN" || hasCode(rep, "manual_backup") {
			t.Errorf("surface %q: %s %v, warning %q, want %q", surface, rep.Posture, rep.Codes, got, want)
		}
	}
}

// covers: MA-950, MA-949, R18
// The same through status's own reading of a real archive (Gather): a descriptor written as
// Run now only beside a job file in this computer's settings folder, and no scheduler entry,
// reads back as Manual — never as the daily schedule an unknown cadence used to read as — and
// is judged the person's choice. Positive twin first: an unknown cadence still reads as daily.
func TestStatusReadsRunNowOnlyFromTheArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	out := filepath.Join(t.TempDir(), "arch")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Stores = map[string]string{"path:/Users/a/Mail": "Mail"}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(jp), 0o700)
	if err := job.Write(jp, job.Job{Name: name, Inputs: []string{"/Users/a/Mail"}, Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	spec := schedule.Spec{Name: name, Interval: schedule.Daily, At: "03:00", Exe: "/opt/mailarchive", Args: []string{"-job", jp}, Out: out}

	// Positive twin: a cadence MailArchive does not know still reads as daily.
	odd := schedule.DescribeSpec(spec)
	odd.Interval = "fortnightly"
	if err := schedule.WriteDescriptor(out, odd); err != nil {
		t.Fatal(err)
	}
	if in := Gather(out, ""); in.Desc.Interval != string(schedule.Daily) || in.Desc.IsManual() {
		t.Fatalf("the positive twin: an unknown cadence reads as %q", in.Desc.Interval)
	}

	if err := schedule.WriteDescriptor(out, schedule.DescribeManual(spec)); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if !in.Desc.IsManual() || !in.KindEv.Has(schedule.KindRunJob) {
		t.Fatalf("read back: %q, run-job corroborated %v", in.Desc.Interval, in.KindEv.Has(schedule.KindRunJob))
	}
	rep := Assess(in, time.Now())
	for _, code := range scheduleFaults {
		if hasCode(rep, code) {
			t.Errorf("status reads Run now only as a broken schedule: %s %q", code, reasonWith(rep, code))
		}
	}
	assure.Reached(t, reasonWith(rep, "manual_backup"), "the Run now only note")
	if !summaryHas(in, rep, manualScheduleLine) {
		t.Errorf("status's Schedule line: %v", Summary(in, rep))
	}
}
