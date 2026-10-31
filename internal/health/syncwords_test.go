package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-939, S65
// The Thunderbird sync's remedies in each surface's own words (the wording pass before
// 1.5.2). Positive twin first: the Desktop keeps its button words. The wizard's health
// view (Surface "gui") is an owner surface with no command line: for a schedule the
// Desktop or the wizard made (a corroborated run-job) it sends the person to MailArchive
// Desktop's Run now and Repair, never a backtick command, a job-file path or "from the
// wizard" (they are in it); where the Desktop's Run now does not start Thunderbird it says
// to open Thunderbird first. The command line's repair follows the schedule's route, as
// repairHow does: an export schedule is re-installed with its own command, never sent to
// a Desktop Repair button that is not shown for it.
func TestTheSyncRemediesSpeakEachSurfacesWords(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	nc := state.SyncStreak{}.NoChangeSeen(t0).NoChangeSeen(t0.Add(24 * time.Hour)).NoChangeSeen(t0.Add(3 * 24 * time.Hour))
	const out = "/Volumes/Backup/Mail"
	const tb = "/Users/a/Library/Thunderbird/Profiles/x.default/Mail/Local Folders"
	jp := filepath.Join(t.TempDir(), "mailarchive-1234abcd.json")
	if err := job.Write(jp, job.Job{Name: "mailarchive-1234abcd", Inputs: []string{tb}, Out: out, Mode: "incremental", ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	runJob := func(goos, surface string, s state.SyncStreak) Input {
		in := syncInput(now, goos, surface, s)
		in.Out = out
		in.Desc.Presync = nil
		in.Desc.Job = []string{"run-job", jp}
		in.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: jp}
		return in
	}

	// Positive twin: the Desktop's own words.
	if got := reasonWith(Assess(runJob("darwin", "desktop", staleStreak("refused-elevated")), now), "presync_not_synced"); !strings.HasSuffix(got, "Do this: The backup ran as administrator, and the sync never runs that way. Click Repair on the Scheduled backup card.") {
		t.Fatalf("positive twin: the Desktop's repair words: %q", got)
	}

	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": true, "windows": false})
	defer restore()
	for _, c := range []struct {
		goos, name, want string
		s                state.SyncStreak
		code             string
	}{
		{"darwin", "cannot-start", "Do this: To back up the mail now, open MailArchive Desktop and click Run now on this archive's Scheduled backup card; it syncs Thunderbird while you watch.", staleStreak("cannot-start"), "presync_not_synced"},
		{"windows", "cannot-start", "Do this: To back up the mail now, open Thunderbird, let its mail arrive, then open MailArchive Desktop and click Run now on this archive's Scheduled backup card.", staleStreak("cannot-start"), "presync_not_synced"},
		{"darwin", "refused-elevated", "Do this: The backup ran as administrator, and the sync never runs that way. Open MailArchive Desktop and click Repair on this archive's Scheduled backup card.", staleStreak("refused-elevated"), "presync_not_synced"},
		{"windows", "not-set-up", "Do this: The sync could not be prepared on this computer. Open MailArchive Desktop and click Repair on this archive's Scheduled backup card.", staleStreak("not-set-up"), "presync_not_synced"},
		{"darwin", "nothing arrived", "answer it, then open MailArchive Desktop and click Run now.", nc, "presync_nothing_arrived"},
	} {
		got := reasonWith(Assess(runJob(c.goos, "gui", c.s), now), c.code)
		if !strings.HasSuffix(got, c.want) {
			t.Errorf("the wizard, %s, %s: %q\n  want it to end %q", c.goos, c.name, got, c.want)
		}
		if strings.Contains(got, "`") || strings.Contains(got, jp) || strings.Contains(got, "from the wizard") || strings.Contains(got, "by hand") {
			t.Errorf("the wizard, %s, %s: names a command, a job file or the wizard itself: %q", c.goos, c.name, got)
		}
	}

	// The command line: an export schedule is repaired with its own command.
	export := syncInput(now, "darwin", "", staleStreak("refused-elevated"))
	export.Out = out
	export.Desc.Job = []string{"-out", out, "-mode", "incremental", "-unattended", "-input", tb, "-thunderbird-sync"}
	want := "Do this: The backup ran as administrator, and the sync never runs that way. Re-run " + scheduleCommand(jobWithOut(export.Desc.Job, out), "darwin") + "."
	if got := reasonWith(Assess(export, now), "presync_not_synced"); !strings.HasSuffix(got, want) || strings.Contains(got, "Scheduled backup card") || strings.Contains(got, "from the wizard") {
		t.Errorf("the command line, an export schedule: %q\n  want it to end %q", got, want)
	}
}

// covers: MA-939, S65
// Status says what the scheduled sync really does and where a schedule is set up (the
// wording pass before 1.5.2). Positive twin first: a schedule that syncs gets its note.
// The note names both skips — Thunderbird already open, and someone using the computer
// (presync session S4 skips at once, it never waits) — the likeliest cause of the 3-day
// warning. No schedule recorded names the Desktop's Settings (the one schedule editor)
// and the wizard's real question, never "the GUI".
func TestStatusSaysWhatTheSyncAndTheScheduleReallyDo(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	in := syncInput(now, "darwin", "", state.SyncStreak{})
	note := reasonWith(Assess(in, now), "presync_syncs")
	const want = "this archive's scheduled backup syncs Thunderbird before it runs: it starts Thunderbird, waits for its mail to stop arriving, then asks it to quit, so the backup includes mail that had only reached the server; if Thunderbird is already open, or someone has used the keyboard or mouse in the last 2 minutes, that backup skips the sync and saves the mail already on this computer"
	if !strings.HasSuffix(note, want) {
		t.Errorf("the sync note: %q\n  want %q", note, want)
	}

	none := healthyInput(now)
	none.HasDescriptor = false
	msg := reasonWith(Assess(none, now), "no_schedule")
	if !strings.Contains(msg, "no schedule is recorded for this archive — choose How often in MailArchive Desktop's Settings, or answer \"Keep this archive current automatically?\" in the wizard, or keep it current by scheduling the same job that made this archive") ||
		!strings.HasSuffix(msg, "; ignore this if a systemd timer, NAS task or other scheduler already keeps it current") || strings.Contains(msg, "GUI") {
		t.Errorf("no schedule: %q", msg)
	}
}
