package wizard

import (
	"io"
	"log"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
)

// covers: MA-1292, R45
// A job run carries the Gmail answer this computer keeps beside its job files as well as the one
// in the job file (the B3 friction review's F2 and F7): a yes given on the card before the archive
// had a job file, and a Not now typed at a terminal. Positive twin first: nothing kept, nothing
// carried.
func TestAJobRunCarriesTheGmailAnswerKeptBesideTheJobFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	logger := log.New(io.Discard, "", 0)
	out := filepath.Join(home, "archive")
	id := lastRunWithID(t, out) // every answer names its archive (MA-1302)
	j := job.Job{Out: out, Mode: "incremental"}
	if o := optsForJob(j, nil, logger, runHooks{}, runScheduled); len(o.GmailYesStores) != 0 || o.GmailNotNow {
		t.Fatalf("positive twin: %v %v", o.GmailYesStores, o.GmailNotNow)
	}
	if err := gmailanswer.KeepHere(out, gmailanswer.Answer{Archive: id, Yes: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	if o := optsForJob(j, nil, logger, runHooks{}, runAttended); !reflect.DeepEqual(o.GmailYesStores, []string{"gmail-store"}) {
		t.Fatalf("the yes kept beside the job files is not carried: %v", o.GmailYesStores)
	}
	if err := gmailanswer.KeepHere(out, gmailanswer.Answer{Archive: id, NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if o := optsForJob(j, nil, logger, runHooks{}, runScheduled); !o.GmailNotNow {
		t.Fatal("the Not now kept beside the job files is not carried")
	}
}
