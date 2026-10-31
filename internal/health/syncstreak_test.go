package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// syncInput is a healthy plaintext archive whose schedule syncs Thunderbird, on goos,
// read by surface, with streak.
func syncInput(now time.Time, goos, surface string, streak state.SyncStreak) Input {
	in := healthyInput(now)
	in.Desc.Presync = &schedule.PresyncDesc{Kind: "thunderbird"}
	in.GOOS, in.Surface, in.SyncStreak = goos, surface, streak
	return in
}

// staleStreak is a missed streak 3 days long whose last miss was name.
func staleStreak(name string) state.SyncStreak {
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	s := state.SyncStreak{}
	for d := 0; d <= 3; d++ {
		s = s.Miss(name, t0.Add(time.Duration(d)*24*time.Hour))
	}
	return s
}

func reasonWith(rep Report, code string) string {
	for i, c := range rep.Codes {
		if c == code {
			return rep.Reasons[i]
		}
	}
	return ""
}

// The §5.4 remedies in desktop words where Run now syncs, written out from the design.
var wantRemedy = map[string]string{
	"owner-active-skipped":  "Click Run now on the Scheduled backup card to sync while you are here. To stop this coming back, change the backup's time in Settings to when you are usually away, or leave Thunderbird open at that time.",
	"leftover-found":        "A Thunderbird an earlier backup started is still open out of sight. Quit it from the Dock, then click Run now.",
	"other-profile-running": "Thunderbird was open with another profile at backup time. Quit it before the backup time, or leave this profile's Thunderbird open.",
	"app-not-found":         "Thunderbird was not found in its usual place. Install it there, then click Run now.",
	"refused-elevated":      "The backup ran as administrator, and the sync never runs that way. Click Repair on the Scheduled backup card.",
	"start-blocked":         "Thunderbird could not be started from the way MailArchive was started. Click Repair on the Scheduled backup card; if you use Run now, start MailArchive Desktop from its shortcut.",
	"profile-unusable":      "This Thunderbird profile could not be used. Open Thunderbird once, then click Run now.",
	"no-desktop-session":    "No one was logged in at backup time, so Thunderbird could not be started. Stay logged in at the backup time.",
	"not-set-up":            "The sync could not be prepared on this computer. Click Repair on the Scheduled backup card.",
	"profile-busy":          "Another backup kept this Thunderbird profile busy. Shut down MailArchive Desktop, open it again, then click Run now.",
	"cannot-start":          "Click Run now on the Scheduled backup card to sync while you watch.",
	"internal-error":        "Click Run now on the Scheduled backup card to sync while you watch.",
}

// covers: MA-909, R32, X6, X10, S65
// The 3-day WARN (design-presync-attended §5.4; C-20, C-37, C-38): positive twin first,
// a plaintext archive whose schedule syncs Thunderbird, with misses spanning 3 days,
// shows WARN presync_not_synced — never RED — naming the date, the count, the closed
// map's phrase and the remedy for the last miss's reason: every reason of §5.4's table,
// in the Desktop's words where Run now syncs (written out); where Run now does not sync
// on this platform, no remedy tells the person to click Run now to sync (C-20); the
// command line's words name the command and no button, the Desktop's no command; Windows
// words name the taskbar, never the Dock. A hostile last name shows the generic phrase
// and remedy and is never echoed. Nothing when the sync is not configured, when the
// streak is not stale, or for an encrypted archive. The health views (desktop, gui)
// say the same.
func TestTheNotSyncedWarning(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": true, "windows": true})
	defer restore()

	in := syncInput(now, "darwin", "desktop", staleStreak("leftover-found"))
	rep := Assess(in, now)
	msg := reasonWith(rep, "presync_not_synced")
	if rep.Posture != "WARN" || msg == "" {
		t.Fatalf("positive twin: posture %s, reasons %v; want WARN presync_not_synced", rep.Posture, rep.Reasons)
	}
	want := "WARN: Thunderbird has not been synced before this archive's scheduled backups since " +
		time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC).Local().Format("Jan 2, 2006") +
		" (4 backups in a row; most recently: Thunderbird not synced: an earlier copy is still open). The backups still ran, but mail that had only reached the mail server may be missing. Do this: " +
		wantRemedy["leftover-found"]
	if msg != want {
		t.Errorf("the WARN reads\n  %q\nwant\n  %q", msg, want)
	}

	for name, remedy := range wantRemedy {
		got := reasonWith(Assess(syncInput(now, "darwin", "desktop", staleStreak(name)), now), "presync_not_synced")
		if !strings.HasSuffix(got, "Do this: "+remedy) {
			t.Errorf("%s, desktop, Run now syncs: %q\n  want the remedy %q", name, got, remedy)
		}
		if !strings.Contains(got, "most recently: "+presync.TagPhrase(name)+")") {
			t.Errorf("%s: the reason is not the closed map's phrase: %q", name, got)
		}
		for _, surface := range []string{"desktop", "", "gui"} {
			for _, goos := range []string{"darwin", "windows"} {
				for _, runNow := range []bool{true, false} {
					r := presync.OverrideTablesForTest(nil, map[string]bool{goos: runNow})
					rep := Assess(syncInput(now, goos, surface, staleStreak(name)), now)
					r()
					got := reasonWith(rep, "presync_not_synced")
					ctx := name + "/" + surface + "/" + goos
					if got == "" || rep.Posture != "WARN" {
						t.Errorf("%s: posture %s, no presync_not_synced: %v", ctx, rep.Posture, rep.Reasons)
						continue
					}
					if surface == "desktop" && (strings.Contains(got, "mailarchive") || strings.Contains(got, "`")) {
						t.Errorf("%s: the Desktop's words name a command: %q", ctx, got)
					}
					if surface != "desktop" && (strings.Contains(got, "click") || strings.Contains(got, "Click") || strings.Contains(got, "Run now")) {
						t.Errorf("%s: the command line's words name a button: %q", ctx, got)
					}
					// Where Run now does not start Thunderbird, it is named only after the
					// person has opened Thunderbird themselves.
					if !runNow && (strings.Contains(got, "to sync while you") ||
						strings.Count(got, "click Run now") != strings.Count(got, "open Thunderbird, let its mail arrive, then click Run now")) {
						t.Errorf("%s: Run now does not sync here, yet the remedy says it does (C-20): %q", ctx, got)
					}
					if goos == "windows" && (strings.Contains(got, "Dock") || strings.Contains(got, "out of sight")) {
						t.Errorf("%s: a Windows remedy names the Dock: %q", ctx, got)
					}
					if goos == "darwin" && strings.Contains(got, "taskbar") {
						t.Errorf("%s: a macOS remedy names the taskbar: %q", ctx, got)
					}
					// One program name (X17; the friction review's O1, applied 2026-10-31).
					if strings.Contains(got, "Mail Archive") {
						t.Errorf("%s: a remedy names the program \"Mail Archive\", not \"MailArchive\": %q", ctx, got)
					}
				}
			}
		}
	}
	if got := reasonWith(Assess(syncInput(now, "windows", "desktop", staleStreak("leftover-found")), now), "presync_not_synced"); !strings.HasSuffix(got,
		"A Thunderbird an earlier backup started is still open minimized. Quit it from the taskbar or the notification area, then click Run now.") {
		t.Errorf("the Windows leftover remedy: %q", got)
	}

	// A hostile name: the generic phrase and remedy, never echoed.
	for _, hostile := range []string{"evil-name", "<b>x</b>", "cancelled", "presync_leftover_found"} {
		got := reasonWith(Assess(syncInput(now, "darwin", "desktop", staleStreak(hostile)), now), "presync_not_synced")
		if !strings.Contains(got, "most recently: Thunderbird not synced)") || !strings.HasSuffix(got, "Do this: "+wantRemedy["cannot-start"]) || strings.Contains(got, hostile) {
			t.Errorf("hostile %q: %q", hostile, got)
		}
	}

	// Nothing when not configured, not stale, or encrypted.
	off := syncInput(now, "darwin", "desktop", staleStreak("leftover-found"))
	off.Desc.Presync = nil
	short := syncInput(now, "darwin", "desktop", state.SyncStreak{}.Miss("x", now.Add(-48*time.Hour)).Miss("x", now.Add(-time.Hour)))
	enc := syncInput(now, "darwin", "desktop", staleStreak("leftover-found"))
	enc.Encrypted, enc.EncryptedWhy = true, "test"
	noDesc := syncInput(now, "darwin", "desktop", staleStreak("leftover-found"))
	noDesc.HasDescriptor = false
	for label, in := range map[string]Input{"not configured": off, "not 3 days": short, "encrypted": enc, "no schedule": noDesc} {
		if rep := Assess(in, now); reasonWith(rep, "presync_not_synced") != "" {
			t.Errorf("%s: presync_not_synced shown: %v", label, rep.Reasons)
		}
	}
}

// covers: MA-909, S65
// The remedies with the tables as shipped, no override (design-presync-attended §5.4,
// §14 gate 4; C-20, OQ-7): macOS's start entry is true since 2026-10-31, so a Mac's
// Desktop remedies — the WARN and the not-synced email, which share SyncAlertWords — are
// the design's own "click Run now" words for every reason of §5.4's table. Windows
// follows its own entry: while it is false, no Windows remedy says Run now syncs.
func TestTheShippedRemedies(t *testing.T) {
	if !presync.AttendedStartAllowed("darwin") {
		t.Fatal("macOS's start entry, as shipped, is not on")
	}
	for name, remedy := range wantRemedy {
		if _, got := SyncAlertWords("darwin", staleStreak(name)); got != remedy {
			t.Errorf("%s, a Mac: %q\n  want %q", name, got, remedy)
		}
		_, win := SyncAlertWords("windows", staleStreak(name))
		if !presync.AttendedStartAllowed("windows") && (strings.Contains(win, "to sync while you") ||
			strings.Count(win, "click Run now") != strings.Count(win, "open Thunderbird, let its mail arrive, then click Run now")) {
			t.Errorf("%s, Windows with its start entry off: the remedy says Run now syncs: %q", name, win)
		}
	}
}

// covers: MA-926, R32, S65
// The softer no-change warning (owner ruling OQ-5, §5.4; C-68): positive twin first,
// scheduled syncs that fetched nothing for 3 days show WARN presync_nothing_arrived —
// never RED — whose first sentence says the backups ran and which names a password or
// sign-in window; in the Desktop's words it says click Run now, in the command line's it
// names the command; Windows says "started minimized". It is left out while
// presync_not_synced shows, and absent when not configured, not 3 days, or encrypted.
// It sends no alert email: no alert sender reads it. It says it clears by itself at the
// next backup that brings new mail, on every surface (owner ruling 4 of 2026-10-31).
func TestTheNothingArrivedWarning(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	nc := state.SyncStreak{}.NoChangeSeen(t0).NoChangeSeen(t0.Add(24 * time.Hour)).NoChangeSeen(t0.Add(3 * 24 * time.Hour))

	rep := Assess(syncInput(now, "darwin", "desktop", nc), now)
	msg := reasonWith(rep, "presync_nothing_arrived")
	want := "WARN: This is not a failed backup: the backups still ran. Since " + t0.Local().Format("Jan 2, 2006") +
		", Thunderbird was started before each of this archive's scheduled backups, but no new mail arrived (3 backups in a row). That can mean there was no new mail, or that Thunderbird is waiting for a password or sign-in that nobody has answered: it was started out of sight, so its window is easy to miss. This warning clears by itself at the next backup that brings new mail. Do this: open Thunderbird; if it asks for a password or a sign-in, answer it, then click Run now."
	if rep.Posture != "WARN" || msg != want {
		t.Fatalf("positive twin: posture %s, the warning\n  %q\nwant\n  %q", rep.Posture, msg, want)
	}
	// syncInput's schedule carries no job the command line could print, so it says so
	// rather than print a partial command (the friction review's O2, applied 2026-10-31).
	if got := reasonWith(Assess(syncInput(now, "windows", "", nc), now), "presync_nothing_arrived"); !strings.Contains(got, "it was started minimized, so its window is easy to miss. This warning clears by itself at the next backup that brings new mail. Do this:") ||
		!strings.HasSuffix(got, "answer it, then "+noWholeCommand+".") || strings.Contains(got, "Run now") || strings.Contains(got, "out of sight") {
		t.Errorf("Windows, command line: %q", got)
	}

	both := nc
	for _, s := range []time.Duration{0, 24 * time.Hour, 3 * 24 * time.Hour} {
		both = both.Miss("owner-active-skipped", t0.Add(s))
	}
	if rep := Assess(syncInput(now, "darwin", "desktop", both), now); reasonWith(rep, "presync_not_synced") == "" || reasonWith(rep, "presync_nothing_arrived") != "" {
		t.Errorf("with both stale, want only presync_not_synced: %v", rep.Codes)
	}
	off := syncInput(now, "darwin", "desktop", nc)
	off.Desc.Presync = nil
	short := syncInput(now, "darwin", "desktop", state.SyncStreak{}.NoChangeSeen(t0).NoChangeSeen(t0.Add(time.Hour)))
	enc := syncInput(now, "darwin", "desktop", nc)
	enc.Encrypted, enc.EncryptedWhy = true, "test"
	for label, in := range map[string]Input{"not configured": off, "not 3 days": short, "encrypted": enc} {
		if rep := Assess(in, now); reasonWith(rep, "presync_nothing_arrived") != "" {
			t.Errorf("%s: presync_nothing_arrived shown: %v", label, rep.Reasons)
		}
	}

	// No alert email: no sender of alert emails reads the no-change warning (§5.4).
	for _, dir := range []string{"../backupalert", "../alert", "../wizard"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "NothingArrived") || strings.Contains(string(b), "presync_nothing_arrived") {
				t.Errorf("%s/%s reads the no-change warning: it must send no email", dir, e.Name())
			}
		}
	}
}

// covers: MA-909, X10, S65
// The Status line and status -json (§4.7; C-37, C-55): a plaintext archive whose last
// backup recorded a sync gets one line, "Thunderbird sync at the last backup (DATE): "
// and the closed map's phrase, never a WARN by itself; a cancelled sync and no sync give
// none; an encrypted archive gets none. status -json's thunderbird_sync carries the
// setting and, for a plaintext archive with a recorded sync, last — its name (a known one,
// else "unknown"), result, phrase and times; an encrypted archive carries the setting
// only, even with a presync planted in its record. JSONVersion stays 2.
func TestTheSyncStatusLineAndJSON(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	since := now.Add(-90 * time.Minute)
	in := syncInput(now, "darwin", "", state.SyncStreak{})
	in.LastRun.Presync = &state.PresyncRecord{Name: "ok", Result: state.PresyncResultSynced, Since: since}
	rep := Assess(in, now)
	line := reasonWith(rep, "presync_last")
	if rep.Posture != "GREEN" || line != "Thunderbird sync at the last backup ("+in.LastRun.Started.Local().Format(localStamp)+"): Thunderbird synced first" {
		t.Fatalf("positive twin: posture %s, line %q", rep.Posture, line)
	}
	doc := JSON(in, rep)
	if doc.Version != 2 || doc.ThunderbirdSync == nil || !doc.ThunderbirdSync.Enabled || doc.ThunderbirdSync.Last == nil {
		t.Fatalf("status -json: version %d, thunderbird_sync %+v", doc.Version, doc.ThunderbirdSync)
	}
	l := doc.ThunderbirdSync.Last
	if l.Name != "ok" || l.Result != "synced" || l.Phrase != "Thunderbird synced first" || l.Since == nil || !l.Since.Equal(since) || !l.Run.Equal(in.LastRun.Started) {
		t.Errorf("thunderbird_sync.last = %+v", l)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	ts, ok := raw["thunderbird_sync"].(map[string]any)
	if !ok || ts["enabled"] != true {
		t.Fatalf("thunderbird_sync in the JSON: %v", raw["thunderbird_sync"])
	}
	if last, ok := ts["last"].(map[string]any); !ok || last["name"] != "ok" || last["result"] != "synced" {
		t.Errorf("thunderbird_sync.last in the JSON: %v", ts["last"])
	}

	// A hostile record: never echoed.
	h := in
	h.LastRun.Presync = &state.PresyncRecord{Name: "<b>evil</b>", Result: "pwned"}
	hrep := Assess(h, now)
	if hl := reasonWith(hrep, "presync_last"); !strings.HasSuffix(hl, ": Thunderbird not synced") || strings.Contains(hl, "evil") {
		t.Errorf("a hostile name's line: %q", hl)
	}
	if hl := JSON(h, hrep).ThunderbirdSync.Last; hl.Name != "unknown" || hl.Result != "unknown" || hl.Phrase != "Thunderbird not synced" {
		t.Errorf("a hostile name in status -json: %+v", hl)
	}

	// None: a cancelled sync, no sync, an encrypted archive.
	c := in
	c.LastRun.Presync = &state.PresyncRecord{Name: "cancelled", Result: state.PresyncResultCancelled}
	n := in
	n.LastRun.Presync = nil
	for label, x := range map[string]Input{"cancelled": c, "no sync": n} {
		if l := reasonWith(Assess(x, now), "presync_last"); l != "" {
			t.Errorf("%s: a Status line %q", label, l)
		}
	}
	if d := JSON(n, Assess(n, now)).ThunderbirdSync; d == nil || !d.Enabled || d.Last != nil {
		t.Errorf("no sync recorded, configured: thunderbird_sync %+v, want enabled and no last", d)
	}
	nn := n
	nn.Desc.Presync = nil
	if d := JSON(nn, Assess(nn, now)).ThunderbirdSync; d != nil {
		t.Errorf("sync off, nothing recorded: thunderbird_sync %+v, want none", d)
	}

	enc := in
	enc.Encrypted, enc.EncryptedWhy, enc.RunNowSync = true, "test", true
	erep := Assess(enc, now)
	if l := reasonWith(erep, "presync_last"); l != "" {
		t.Errorf("an encrypted archive's Status line: %q", l)
	}
	if d := JSON(enc, erep).ThunderbirdSync; d == nil || !d.Enabled || d.Last != nil {
		t.Errorf("encrypted: thunderbird_sync %+v, want the setting only", d)
	}
	enc.RunNowSync = false
	if d := JSON(enc, Assess(enc, now)).ThunderbirdSync; d != nil {
		t.Errorf("encrypted, sync off: thunderbird_sync %+v", d)
	}
}

// covers: MA-909, MA-908
// A run whose sync could not be prepared (not-set-up, C-34) surfaces its recorded
// warning as WARN, never RED, with its code in status -json — like every other warn-class
// sync code. Positive twin first: the same record without the warning is GREEN.
func TestTheNotSetUpWarningSurfaces(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	if rep := Assess(in, now); rep.Posture != "GREEN" {
		t.Fatalf("positive twin: %s %v", rep.Posture, rep.Reasons)
	}
	msg, _ := presync.WarnText(presync.OutcomeNotSetUp)
	in.LastRun.Warnings = []state.RunWarning{{Code: presync.OutcomeNotSetUp.Code, Message: msg}}
	rep := Assess(in, now)
	if rep.Posture != "WARN" || reasonWith(rep, "presync_not_set_up") != "WARN: "+msg {
		t.Errorf("posture %s, reasons %v; want WARN carrying the not-set-up message", rep.Posture, rep.Reasons)
	}
	if doc := JSON(in, rep); !contains(doc.ReasonCodes, "presync_not_set_up") {
		t.Errorf("status -json reason_codes %v lack presync_not_set_up", doc.ReasonCodes)
	}
}

// noWholeCommand is the command line's words when it cannot print a whole command
// (the friction review's O2, applied 2026-10-31), written out.
const noWholeCommand = "run this archive's backup by hand, adding `-thunderbird-sync` to the command you back it up with; status cannot print that whole command for this archive"

// covers: MA-909, MA-926, S65
// The command line's "back up the mail now" names a whole command (the friction review's
// O2, applied by the lead 2026-10-31): never the bare `mailarchive -thunderbird-sync`,
// which is not a command on its own. Positive twin first: an export schedule whose job
// carries -thunderbird-sync gets that job with THIS archive's folder as its -out (a job
// recorded for another folder is re-pointed), each token shell-quoted for the OS, in both
// the 3-day WARN and the softer one; a Desktop- or wizard-made schedule (a run-job
// corroborated on this computer, whose job file carries the sync) gets `mailarchive
// run-job JOBFILE`, the very job the schedule runs. Where neither is known — no job that
// carries the sync, a run-job file without the sync, or a Windows token no quoting makes
// inert — it says it cannot print a whole command, and prints no partial one. The Desktop
// still names no command.
func TestTheCommandLineNamesTheWholeCommand(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	nc := state.SyncStreak{}.NoChangeSeen(t0).NoChangeSeen(t0.Add(24 * time.Hour)).NoChangeSeen(t0.Add(3 * 24 * time.Hour))
	const out = "/Volumes/Backup/Mail"
	const tb = "/Users/a/Library/Thunderbird/Profiles/x.default/Mail/Local Folders"
	export := func(goos, surface string, s state.SyncStreak, jobOut string) Input {
		in := syncInput(now, goos, surface, s)
		in.Out = out
		in.Desc.Job = []string{"-out", jobOut, "-mode", "incremental", "-unattended", "-input", tb, "-thunderbird-sync"}
		return in
	}
	const wantCmd = "`mailarchive -out /Volumes/Backup/Mail -mode incremental -unattended -input '" + tb + "' -thunderbird-sync`"

	got := reasonWith(Assess(export("darwin", "", staleStreak("cannot-start"), out), now), "presync_not_synced")
	if !strings.HasSuffix(got, "Do this: To back up the mail now, open Thunderbird, let its mail arrive, then run the backup by hand with "+wantCmd+".") {
		t.Fatalf("positive twin: the export schedule's WARN\n  %q\nwant it to end with the whole command %s", got, wantCmd)
	}
	if got := reasonWith(Assess(export("darwin", "", nc, out), now), "presync_nothing_arrived"); !strings.HasSuffix(got, "answer it, then run the backup by hand with "+wantCmd+".") ||
		!strings.Contains(got, " This warning clears by itself at the next backup that brings new mail. Do this: ") {
		t.Errorf("the softer warning: %q\n  want the whole command %s", got, wantCmd)
	}
	// A job recorded for another folder: the command backs up THIS archive.
	if got := reasonWith(Assess(export("darwin", "gui", staleStreak("cannot-start"), "/Volumes/Old"), now), "presync_not_synced"); !strings.Contains(got, wantCmd) || strings.Contains(got, "/Volumes/Old -mode") {
		t.Errorf("a job recorded for another folder: %q\n  want %s", got, wantCmd)
	}
	// Windows quoting.
	win := export("windows", "", staleStreak("cannot-start"), `D:\Mail Archive`)
	win.Out = `D:\Mail Archive`
	win.Desc.Job[6] = `C:\Users\Alex\AppData\Roaming\Thunderbird\Profiles\x.default\Mail\Local Folders`
	if got := reasonWith(Assess(win, now), "presync_not_synced"); !strings.HasSuffix(got,
		"then run the backup by hand with `mailarchive -out \"D:\\Mail Archive\" -mode incremental -unattended -input \"C:\\Users\\Alex\\AppData\\Roaming\\Thunderbird\\Profiles\\x.default\\Mail\\Local Folders\" -thunderbird-sync`.") {
		t.Errorf("Windows: %q", got)
	}
	// Every reason whose remedy runs the backup by hand names the whole command.
	for name := range wantRemedy {
		got := reasonWith(Assess(export("darwin", "", staleStreak(name), out), now), "presync_not_synced")
		if strings.Contains(got, "by hand") && !strings.Contains(got, "by hand with "+wantCmd) {
			t.Errorf("%s: %q\n  runs the backup by hand without the whole command", name, got)
		}
	}

	// A run-job schedule, corroborated on this computer, whose job file carries the sync.
	jp := filepath.Join(t.TempDir(), "mailarchive-1234abcd.json")
	if err := job.Write(jp, job.Job{Name: "mailarchive-1234abcd", Inputs: []string{tb}, Out: out, Mode: "incremental", ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	runJob := func(surface string, s state.SyncStreak) Input {
		in := syncInput(now, "darwin", surface, s)
		in.Out = out
		in.Desc.Presync = nil // the run-job's own job file says it syncs
		in.Desc.Job = []string{"run-job", jp}
		in.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: jp}
		return in
	}
	wantRun := "`mailarchive run-job " + shellQuoteToken(jp, "darwin") + "`"
	if got := reasonWith(Assess(runJob("", staleStreak("cannot-start")), now), "presync_not_synced"); !strings.HasSuffix(got, "then run the backup by hand with "+wantRun+".") {
		t.Errorf("the run-job schedule: %q\n  want %s", got, wantRun)
	}
	if got := reasonWith(Assess(runJob("", nc), now), "presync_nothing_arrived"); !strings.HasSuffix(got, "answer it, then run the backup by hand with "+wantRun+".") {
		t.Errorf("the run-job schedule, softer warning: %q", got)
	}
	if got := reasonWith(Assess(runJob("desktop", staleStreak("cannot-start")), now), "presync_not_synced"); strings.Contains(got, "mailarchive") || strings.Contains(got, "`") {
		t.Errorf("the Desktop names a command: %q", got)
	}

	// Neither known: says so, prints no partial command.
	noSync := runJob("", staleStreak("cannot-start"))
	noSync.Desc.Presync = &schedule.PresyncDesc{Kind: "thunderbird"} // configured, but its job file lacks the sync
	jp2 := filepath.Join(t.TempDir(), "mailarchive-5678abcd.json")
	if err := job.Write(jp2, job.Job{Name: "mailarchive-5678abcd", Inputs: []string{tb}, Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	noSync.KindEv.JobPath = jp2
	unsafe := export("windows", "", staleStreak("cannot-start"), `C:\%TEMP%\mail`)
	unsafe.Out = `C:\%TEMP%\mail`
	for label, in := range map[string]Input{
		"no job carries the sync":           syncInput(now, "darwin", "", staleStreak("cannot-start")),
		"a run-job file without the sync":   noSync,
		"a Windows token quoting can't fix": unsafe,
	} {
		got := reasonWith(Assess(in, now), "presync_not_synced")
		if !strings.HasSuffix(got, "let its mail arrive, then "+noWholeCommand+".") {
			t.Errorf("%s: %q\n  want it to say it cannot print a whole command", label, got)
		}
		if strings.Contains(got, "`mailarchive") || strings.Contains(got, unsafeTokenPlaceholder) {
			t.Errorf("%s: a partial command is printed: %q", label, got)
		}
	}
}

// covers: MA-908, MA-909
// Gather reads a plaintext archive's sync streak (and nothing for an encrypted one), so
// status and the Desktop's health view judge the same record.
func TestGatherReadsTheSyncStreak(t *testing.T) {
	out := t.TempDir()
	s := staleStreak("leftover-found")
	if err := state.WriteSyncStreak(out, s); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if in.SyncStreak.Missed != s.Missed || in.SyncStreak.LastName != "leftover-found" || !in.SyncStreak.Stale() {
		t.Errorf("Gather read %+v, want %+v", in.SyncStreak, s)
	}
}
