package health

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-458, R18, R14, S29
// verify_stale is judged against the scheduled verify's own cadence, read from
// its own descriptor (owner ruling FB-4), never the backup's. With the backup
// daily and the verify weekly, a verify that finished 10 days ago is not stale
// (against the daily backup it would be) and one that finished 15 days ago is:
// the WARN names the weekly period, the verify schedule and a pasteable
// re-install of this archive's verify (its cadence, time and -record and -json flags, no
// -name; a time the descriptor could not parse is left out), and names a shared
// -name as a cause only off cron hosts, where it is not detected. With the
// verify daily and the backup weekly, one that finished 3 days
// ago is stale. A verify with no run recorded yet is judged from the
// descriptor's install time: not stale 3 days after a weekly verify was
// installed, stale 20 days after; with no install time recorded it is not
// judged. A last verify still recorded as running is judged from its start: one
// running for an hour with the archive lock held is neither stale nor flagged
// (the positive twin); one started a day ago with the lock free never finished
// (verify_never_finished, naming its start, its log and `verify -out`) and is
// not yet stale; one started 30 days ago is stale ("started 30 days ago", "has
// not finished"), with the lock free (and verify_never_finished too) or held. A
// weekly verify installed 3 days ago, after a last verify of 30 days ago
// (finished, or started and never finished), keeps verify_stale (MA-466) but
// says the schedule was installed after the last verify and has not run since,
// naming its next run and `verify -out` now, never "stopped running" or a
// re-install; one re-installed 20 days ago with nothing since says no verify
// has run since the install, with the re-install remedy. A
// last-verify record that cannot be read is a WARN (lastverify_unreadable)
// naming the file and `verify -out`, never GREEN. Older installs stay truthful,
// with no false WARN: a verify an earlier
// release installed records no descriptor, so a last verify 30 days old raises
// no verify_stale; a verify an even older release recorded in the backup's
// descriptor is still judged against that record (MA-155), and the verify's own
// descriptor wins when both exist. A verify descriptor that cannot be read
// (its real reader error) is a WARN naming the file and its remedy, and gives
// no cadence. Summary and status -json name the verify schedule.
func TestVerifyStaleJudgedAgainstTheVerifySchedule(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	daysAgo := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	const verifyName = "mailarchive-1234abcd-verify"
	lastVerify := func(in *Input, finished time.Time) {
		in.LastVerifyState = state.LastVerifyPresent
		in.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: finished, Finished: &finished, Attested: true}
	}
	// withVerify is the healthy daily-backup archive plus a recorded verify.
	withVerify := func(interval string, installed time.Time) Input {
		in := healthyInput(now)
		in.GOOS = "darwin"
		in.HasVerifyDescriptorFile, in.HasVerifyDescriptor = true, true
		in.VerifyDesc = schedule.Descriptor{Version: 1, Name: verifyName, Interval: interval, At: "05:00",
			Exe: "/usr/local/bin/mailarchive", Job: []string{"verify", "-out", "/a", "-unattended", "-record", "-json", "-log", "/a/" + verifyName + ".log"},
			Log: "/a/" + verifyName + ".log", InstalledAt: installed, Host: "box"}
		in.VerifySchedState, in.VerifyExeExists, in.VerifyExeIsThis = schedule.Installed, true, true
		return in
	}
	staleReason := func(in Input) (bool, string) {
		rep := Assess(in, now)
		for i, c := range rep.Codes {
			if c == "verify_stale" {
				return true, rep.Reasons[i]
			}
		}
		return false, ""
	}
	expect := func(step string, in Input, want bool) string {
		t.Helper()
		got, reason := staleReason(in)
		if got != want {
			t.Errorf("%s: verify_stale=%v, want %v (reasons: %v)", step, got, want, Assess(in, now).Reasons)
		}
		return reason
	}

	// The verify's weekly cadence, not the backup's daily one.
	weekly10 := withVerify("weekly", daysAgo(60))
	lastVerify(&weekly10, daysAgo(10))
	expect("weekly verify, last run 10 days ago (the backup is daily)", weekly10, false)
	weekly15 := withVerify("weekly", daysAgo(60))
	lastVerify(&weekly15, daysAgo(15))
	reason := expect("weekly verify, last run 15 days ago", weekly15, true)
	for _, want := range []string{
		"15 days", "weekly verify schedule period", verifyName,
		"`mailarchive schedule -interval weekly -at 05:00 -install -- verify -out /a -record -json`",
		"another archive's verify", "`mailarchive verify -out \"/a\"`",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("the stale-verify WARN lacks %q:\n%s", want, reason)
		}
	}
	// cron reads its blocks back, so a shared -name is no cause there; a time
	// the descriptor could not parse ("??:??") is left out of the re-install.
	cron := weekly15
	cron.GOOS = "linux"
	if _, reason := staleReason(cron); !strings.Contains(reason, "stopped running here") || strings.Contains(reason, "another archive") {
		t.Errorf("on a cron host the stale-verify WARN blames a shared -name cron would have refused:\n%s", reason)
	}
	noTime := weekly15
	noTime.VerifyDesc.At = "??:??"
	if _, reason := staleReason(noTime); !strings.Contains(reason, "`mailarchive schedule -interval weekly -install -- verify -out /a -record -json`") {
		t.Errorf("with an unparseable time, the re-install remedy is not the cadence and the verify's flags without -at:\n%s", reason)
	}
	daily3 := withVerify("daily", daysAgo(60))
	daily3.Desc.Interval = "weekly"
	lastVerify(&daily3, daysAgo(3))
	expect("daily verify, last run 3 days ago (the backup is weekly)", daily3, true)

	// No run recorded yet: judged from the install time.
	expect("weekly verify installed 3 days ago, no run yet", withVerify("weekly", daysAgo(3)), false)
	reason = expect("weekly verify installed 20 days ago, no run yet", withVerify("weekly", daysAgo(20)), true)
	if !strings.Contains(reason, "since") || !strings.Contains(reason, verifyName) || !strings.Contains(reason, "verify -out") {
		t.Errorf("the never-ran WARN does not say since when, which schedule, and the remedy:\n%s", reason)
	}
	expect("no install time recorded, no run yet", withVerify("weekly", time.Time{}), false)

	// A last verify that never finished: judged from when it started.
	reasonFor := func(in Input, code string) (bool, string) {
		rep := Assess(in, now)
		for i, c := range rep.Codes {
			if c == code {
				return true, rep.Reasons[i]
			}
		}
		return false, ""
	}
	running := func(started time.Time, lockHeld bool) Input {
		in := withVerify("weekly", daysAgo(60))
		in.LastVerifyState = state.LastVerifyPresent
		in.LastVerify = state.LastVerify{Status: state.VerifyRunning, Started: started}
		in.LockHeld = lockHeld
		return in
	}
	for _, c := range []struct {
		step                 string
		in                   Input
		stale, neverFinished bool
	}{
		{"a verify running for an hour, the lock held (the positive twin)", running(now.Add(-time.Hour), true), false, false},
		{"a verify started a day ago, the lock free", running(daysAgo(1), false), false, true},
		{"a verify started 30 days ago, the lock free", running(daysAgo(30), false), true, true},
		{"a verify started 30 days ago, the lock still held", running(daysAgo(30), true), true, false},
	} {
		got, reason := reasonFor(c.in, "verify_stale")
		if got != c.stale {
			t.Errorf("%s: verify_stale=%v, want %v (reasons: %v)", c.step, got, c.stale, Assess(c.in, now).Reasons)
		}
		if got && (!strings.Contains(reason, "started 30 days ago") || !strings.Contains(reason, "has not finished") || !strings.Contains(reason, verifyName)) {
			t.Errorf("%s: the stale-verify WARN does not say when the unfinished verify started:\n%s", c.step, reason)
		}
		got, reason = reasonFor(c.in, "verify_never_finished")
		if got != c.neverFinished {
			t.Errorf("%s: verify_never_finished=%v, want %v (reasons: %v)", c.step, got, c.neverFinished, Assess(c.in, now).Reasons)
		}
		if got {
			started := c.in.LastVerify.Started.Local().Format("2006-01-02 15:04")
			for _, want := range []string{started, "never finished", "/a/" + verifyName + ".log", "`mailarchive verify -out \"/a\"`"} {
				if !strings.Contains(reason, want) {
					t.Errorf("%s: the never-finished WARN lacks %q:\n%s", c.step, want, reason)
				}
			}
		}
	}

	// A verify schedule installed after the last verify ran has not stopped: the
	// WARN stays (MA-466), but says it has not run yet, and a verify now clears
	// it; a re-install would change nothing, so it is not the remedy.
	fresh := withVerify("weekly", daysAgo(3))
	lastVerify(&fresh, daysAgo(30))
	freshRunning := withVerify("weekly", daysAgo(3))
	freshRunning.LastVerifyState = state.LastVerifyPresent
	freshRunning.LastVerify = state.LastVerify{Status: state.VerifyRunning, Started: daysAgo(30)}
	for _, c := range []struct {
		step, last string
		in         Input
	}{
		{"weekly verify installed 3 days ago, the last verify finished 30 days ago", "the last verify was 30 days ago", fresh},
		{"weekly verify installed 3 days ago, the last verify started 30 days ago", "the last verify started 30 days ago and has not finished", freshRunning},
	} {
		reason := expect(c.step, c.in, true)
		for _, want := range []string{c.last, verifyName, "installed 3 days ago, after the last verify", "has not run since", "wait for its next run (weekly at 05:00)", "`mailarchive verify -out \"/a\"` now"} {
			if !strings.Contains(reason, want) {
				t.Errorf("%s: the WARN lacks %q:\n%s", c.step, want, reason)
			}
		}
		for _, bad := range []string{"stopped running", "re-install"} {
			if strings.Contains(reason, bad) {
				t.Errorf("%s: the WARN says %q of a schedule installed after the last verify:\n%s", c.step, bad, reason)
			}
		}
	}
	// Installed after the last verify, but two periods ago, with nothing since: stopped.
	refreshed := withVerify("weekly", daysAgo(20))
	lastVerify(&refreshed, daysAgo(30))
	reason = expect("weekly verify re-installed 20 days ago, the last verify 30 days old", refreshed, true)
	for _, want := range []string{"no verify has run in the 20 days since", verifyName, "the last verify was 30 days ago, before the install", "re-install it"} {
		if !strings.Contains(reason, want) {
			t.Errorf("a verify re-installed 20 days ago with nothing since: the WARN lacks %q:\n%s", want, reason)
		}
	}

	// A last-verify record that cannot be read: a WARN naming it, never GREEN.
	unreadable := withVerify("weekly", daysAgo(60))
	unreadable.LastVerifyState, unreadable.LastVerifyErr = state.LastVerifyUnreadable, errors.New("unexpected end of JSON input")
	if got, reason := reasonFor(unreadable, "lastverify_unreadable"); !got || !strings.Contains(reason, filepath.Join("/a", state.LastVerifyName)) ||
		!strings.Contains(reason, "unreadable") || !strings.Contains(reason, "`mailarchive verify -out \"/a\"`") {
		t.Errorf("an unreadable last-verify record is not a WARN naming the file and the remedy (%v):\n%s", got, reason)
	}
	if rep := Assess(unreadable, now); rep.Posture == "GREEN" {
		t.Errorf("an unreadable last-verify record left the posture GREEN: %v", rep.Reasons)
	}

	// Older installs: no false WARN.
	earlier := healthyInput(now) // an earlier release's verify: no verify descriptor
	lastVerify(&earlier, daysAgo(30))
	expect("verify installed by an earlier release (no verify descriptor)", earlier, false)
	oldest := healthyInput(now) // an older release recorded the verify in the backup's place
	oldest.Desc.Job, oldest.Desc.Interval = []string{"verify", "-out", "/a"}, "daily"
	lastVerify(&oldest, daysAgo(3))
	if reason := expect("verify recorded in the backup descriptor by an older release", oldest, true); !strings.Contains(reason, "more than twice the daily verify schedule period") {
		t.Errorf("the older release's record is not judged as MA-155 judges it:\n%s", reason)
	}
	both := withVerify("weekly", daysAgo(60))
	both.Desc.Job, both.Desc.Interval = []string{"verify", "-out", "/a"}, "daily"
	lastVerify(&both, daysAgo(3))
	expect("the verify's own weekly descriptor beside an older daily record", both, false)

	// An unreadable verify descriptor: named with its remedy, no cadence from it.
	dir := t.TempDir()
	broken := filepath.Join(dir, schedule.VerifyDescriptorName)
	if err := os.WriteFile(broken, []byte("{\"version\":1,\"name\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, derr := schedule.ReadVerifyDescriptor(dir)
	assure.Reached(t, derr, "the reader's error for a verify descriptor cut short")
	bad := healthyInput(now)
	bad.HasVerifyDescriptorFile, bad.VerifyDescErr = true, derr
	lastVerify(&bad, daysAgo(30))
	rep := Assess(bad, now)
	found := false
	for i, c := range rep.Codes {
		if c == "verify_descriptor_unreadable" {
			found = true
			for _, want := range []string{broken, "unreadable", "delete it if no verify schedule"} {
				if !strings.Contains(rep.Reasons[i], want) {
					t.Errorf("the unreadable-verify-descriptor WARN lacks %q:\n%s", want, rep.Reasons[i])
				}
			}
		}
	}
	if !found || rep.Posture == "GREEN" {
		t.Errorf("an unreadable verify descriptor is not a WARN: %s %v", rep.Posture, rep.Reasons)
	}
	expect("an unreadable verify descriptor", bad, false)
	if lines := strings.Join(Summary(bad, rep), "\n"); !strings.Contains(lines, "Verify schedule: (descriptor present but unreadable") {
		t.Errorf("the summary does not say the verify descriptor is unreadable:\n%s", lines)
	}

	// Summary and status -json name the verify schedule; nothing when none is recorded.
	lines := strings.Join(Summary(weekly10, Assess(weekly10, now)), "\n")
	if !strings.Contains(lines, `Verify schedule: "`+verifyName+`" installed · weekly at 05:00`) {
		t.Errorf("the summary does not name the verify schedule:\n%s", lines)
	}
	data, err := json.Marshal(JSON(weekly10, Assess(weekly10, now)))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		VerifySchedule *JSONSchedule `json:"verify_schedule"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.VerifySchedule == nil ||
		doc.VerifySchedule.Name != verifyName || doc.VerifySchedule.State != "installed" || doc.VerifySchedule.Interval != "weekly" {
		t.Errorf("status -json does not carry the verify schedule (%v):\n%s", err, data)
	}
	if data, _ := json.Marshal(JSON(earlier, Assess(earlier, now))); strings.Contains(string(data), "verify_schedule") {
		t.Errorf("status -json carries a verify schedule none recorded:\n%s", data)
	}
	if lines := strings.Join(Summary(earlier, Assess(earlier, now)), "\n"); strings.Contains(lines, "Verify schedule:") {
		t.Errorf("the summary names a verify schedule none recorded:\n%s", lines)
	}
}

// covers: MA-466, R18, S29
// `status` watches the scheduled verify's entry as it watches the backup's (the
// README and MA-466 say so): on this host a verify recorded but absent from the
// scheduler is a WARN at once (verify_not_installed) naming the re-install and
// the removal that clears the record; a scheduler that cannot be queried is
// scheduler_unavailable, once even beside the backup's; a recorded program that
// is gone or is not this binary is verify_exe_missing or verify_exe_moved,
// naming the re-install — WARNs, as a stale verify is, never RED. A verify
// recorded on another host is verify_other_host, and its state and program are
// not judged from here. The positive twin first: a healthy recorded verify
// raises none of these, and the posture stays GREEN. The last verify ran a day
// ago throughout, so verify_stale is never the reason.
func TestVerifyEntryWatchedAsTheBackupIs(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	const verifyName = "mailarchive-1234abcd-verify"
	entryCodes := []string{"verify_other_host", "verify_not_installed", "scheduler_unavailable", "verify_exe_missing", "verify_exe_moved"}
	healthy := func() Input {
		in := healthyInput(now)
		in.GOOS = "linux"
		in.HasVerifyDescriptorFile, in.HasVerifyDescriptor = true, true
		in.VerifyDesc = schedule.Descriptor{Version: 1, Name: verifyName, Interval: "weekly", At: "05:00",
			Exe: "/usr/local/bin/mailarchive", Job: []string{"verify", "-out", "/a", "-unattended", "-record"},
			InstalledAt: now.Add(-60 * 24 * time.Hour), Host: "box"}
		in.VerifySchedState, in.VerifyExeExists, in.VerifyExeIsThis = schedule.Installed, true, true
		finished := now.Add(-24 * time.Hour)
		in.LastVerifyState = state.LastVerifyPresent
		in.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: finished, Finished: &finished, Attested: true}
		return in
	}
	count := func(rep Report, code string) (n int, reason string) {
		for i, c := range rep.Codes {
			if c == code {
				n, reason = n+1, rep.Reasons[i]
			}
		}
		return n, reason
	}
	reinstall := "`mailarchive schedule -interval weekly -at 05:00 -install -- verify -out /a -record`"

	// Positive twin: a healthy recorded verify raises none of the entry's WARNs.
	rep := Assess(healthy(), now)
	for _, code := range entryCodes {
		if n, _ := count(rep, code); n != 0 {
			t.Errorf("a healthy scheduled verify raised %s: %v", code, rep.Reasons)
		}
	}
	if rep.Posture != "GREEN" {
		t.Errorf("a healthy scheduled verify is not GREEN: %s %v", rep.Posture, rep.Reasons)
	}

	for _, c := range []struct {
		step   string
		mutate func(in *Input)
		code   string
		wants  []string
		others []string // entry codes that must not appear
	}{
		{"recorded but absent from the scheduler", func(in *Input) { in.VerifySchedState = schedule.NotInstalled }, "verify_not_installed",
			[]string{verifyName, "not present in this host's scheduler", reinstall, "`mailarchive schedule -name " + verifyName + " -remove -- verify -out /a`"}, nil},
		{"the scheduler cannot be queried, no backup recorded", func(in *Input) {
			in.VerifySchedState, in.HasDescriptor, in.DescErr = schedule.SchedulerUnavailable, false, os.ErrNotExist
		}, "scheduler_unavailable", []string{"cannot be queried", "scheduled verify"}, nil},
		{"the recorded program is gone", func(in *Input) { in.VerifyExeExists, in.VerifyExeIsThis = false, false }, "verify_exe_missing",
			[]string{"/usr/local/bin/mailarchive", "no longer exists", reinstall}, []string{"verify_exe_moved"}},
		{"the recorded program is not this binary", func(in *Input) { in.VerifyExeIsThis = false }, "verify_exe_moved",
			[]string{"/usr/local/bin/mailarchive", "not this binary", reinstall}, []string{"verify_exe_missing"}},
		{"recorded on another host", func(in *Input) {
			in.VerifyDesc.Host, in.VerifySchedState, in.VerifyExeExists = "nas-01", schedule.NotInstalled, false
		}, "verify_other_host", []string{verifyName, `"nas-01"`, "cannot be checked from here"}, []string{"verify_not_installed", "verify_exe_missing"}},
	} {
		in := healthy()
		c.mutate(&in)
		rep := Assess(in, now)
		n, reason := count(rep, c.code)
		if n != 1 {
			t.Errorf("%s: %d %s reason(s), want 1 (reasons: %v)", c.step, n, c.code, rep.Reasons)
			continue
		}
		for _, want := range c.wants {
			if !strings.Contains(reason, want) {
				t.Errorf("%s: the %s WARN lacks %q:\n%s", c.step, c.code, want, reason)
			}
		}
		for _, other := range c.others {
			if n, _ := count(rep, other); n != 0 {
				t.Errorf("%s: also raised %s: %v", c.step, other, rep.Reasons)
			}
		}
		if rep.Posture != "WARN" {
			t.Errorf("%s: posture %s, want WARN (a verify that cannot run is a WARN, never RED): %v", c.step, rep.Posture, rep.Reasons)
		}
		if n, _ := count(rep, "verify_stale"); n != 0 {
			t.Errorf("%s: verify_stale raised for a verify that ran a day ago: %v", c.step, rep.Reasons)
		}
	}

	// Beside the backup's own scheduler_unavailable, said once.
	both := healthy()
	both.SchedState, both.VerifySchedState = schedule.SchedulerUnavailable, schedule.SchedulerUnavailable
	if n, _ := count(Assess(both, now), "scheduler_unavailable"); n != 1 {
		t.Errorf("an unqueryable scheduler is reported %d times for the backup and the verify, want once: %v", n, Assess(both, now).Reasons)
	}
}
