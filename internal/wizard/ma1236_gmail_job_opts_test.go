package wizard

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1236, R45
// A job run (a scheduled backup, and the Desktop's Run now) carries the Gmail answers this
// computer keeps for the archive (slice B3): the card's yes for its stores, and Not now, kept in
// the job file for the archive the folder holds (its id, MA-1302), and the archive the yes was
// given for, which the run checks again under its lock. It never asks: no job run has someone to
// ask at a terminal. Positive twin first: a job with no answer carries none.
func TestAJobRunCarriesTheGmailAnswersKeptHere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	logger := log.New(io.Discard, "", 0)
	out := filepath.Join(home, "a")
	id := lastRunWithID(t, out)
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	for _, mode := range []runMode{runScheduled, runAttended} {
		plain := job.Job{Out: out, Mode: "incremental"}
		if err := job.Write(jp, plain); err != nil {
			t.Fatal(err)
		}
		o := optsForJob(plain, nil, logger, runHooks{}, mode)
		if len(o.GmailYesStores) != 0 || o.GmailNotNow || o.GmailYes || o.AskGmail != nil {
			t.Fatalf("positive twin (mode %v): %+v %v %v", mode, o.GmailYesStores, o.GmailNotNow, o.AskGmail != nil)
		}
		answered := job.Job{Out: out, Mode: "incremental", GmailYes: []string{"gmail-store"}, GmailNotNow: true, GmailArchive: id}
		if err := job.Write(jp, answered); err != nil {
			t.Fatal(err)
		}
		o = optsForJob(answered, nil, logger, runHooks{}, mode)
		if !reflect.DeepEqual(o.GmailYesStores, []string{"gmail-store"}) || !o.GmailNotNow || o.GmailYesFor != id || o.AskGmail != nil {
			t.Fatalf("mode %v: the answers kept here are not carried: %+v %v %q", mode, o.GmailYesStores, o.GmailNotNow, o.GmailYesFor)
		}
	}
}

// lastRunWithID makes out an archive folder whose last backup ended ok, as a run records it, and
// returns the archive's id.
func lastRunWithID(t *testing.T, out string) string {
	t.Helper()
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	id := state.NewArchiveID()
	now := time.Now().UTC()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, Archive: id}); err != nil {
		t.Fatal(err)
	}
	return id
}
