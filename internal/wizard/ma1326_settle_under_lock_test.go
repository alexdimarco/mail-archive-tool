package wizard

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1326, R45, R37
// The Desktop's Run now spends a used yes (gmailanswer.Settle rewrites the archive's job file)
// only while it holds the job file lock the Desktop passes in (RunNow.HoldJobFile, the lock Save
// settings takes; the B3 adversarial pass, round 2's G8). settleUnder runs Settle inside the hold:
// before the hold the yes is still in the job file, at the release it is gone. RunJobInProcess
// takes the hold once per run. Positive twin first: with no hold passed in, Settle still spends
// the yes (the scheduled run-job, another process, takes no Desktop lock).
func TestTheRunNowSpendsAYesUnderTheJobFileLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	out := filepath.Join(home, "archive")
	id := lastRunWithID(t, out)
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	spentYes := func() {
		t.Helper()
		if err := job.Write(jp, job.Job{Name: schedule.DefaultNameFor(out), Out: out, Mode: "incremental", GmailYes: []string{"imap.gmail.com"}, GmailArchive: id}); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOn: []string{"imap.gmail.com"}, Archive: id}); err != nil {
			t.Fatal(err)
		}
	}
	yesInJob := func() bool { j, _ := job.Read(jp); return reflect.DeepEqual(j.GmailYes, []string{"imap.gmail.com"}) }

	spentYes()
	settleUnder(out, nil)
	if yesInJob() {
		t.Fatal("positive twin: with no hold, the spent yes was not taken out of the job file")
	}

	spentYes()
	var beforeHold, atRelease bool
	holds := 0
	settleUnder(out, func() func() {
		holds++
		beforeHold = yesInJob()
		return func() { atRelease = yesInJob() }
	})
	if holds != 1 || !beforeHold || atRelease {
		t.Fatalf("Settle did not run inside the hold: holds %d, yes before the hold %v, yes at the release %v", holds, beforeHold, atRelease)
	}

	// RunJobInProcess takes the hold once, whatever the run's outcome.
	g := newLocalRunRig(t)
	jobPath := filepath.Join(g.dir, "job.json")
	if err := job.Write(jobPath, job.Job{Name: "a", Out: g.out, Mode: "incremental", Inputs: []string{g.mbox}}); err != nil {
		t.Fatal(err)
	}
	runHolds, released := 0, 0
	if _, err := RunJobInProcess(context.Background(), context.Background(), jobPath, log.New(io.Discard, "", 0),
		RunNow{HoldJobFile: func() func() { runHolds++; return func() { released++ } }}); err != nil {
		t.Fatal(err)
	}
	if runHolds != 1 || released != 1 {
		t.Fatalf("the Run now took the job file lock %d times and released it %d times, want 1 and 1", runHolds, released)
	}
}
