package wizard

import (
	"io"
	"log"
	"reflect"
	"testing"

	"mail-archive-tool/internal/job"
)

// covers: MA-1236, R45
// A job run (a scheduled backup, and the Desktop's Run now) carries the Gmail answers this
// computer keeps for the archive (slice B3): the card's yes for its stores, and Not now. It never
// asks: no job run has someone to ask at a terminal. Positive twin first: a job with no answer
// carries none.
func TestAJobRunCarriesTheGmailAnswersKeptHere(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	for _, mode := range []runMode{runScheduled, runAttended} {
		o := optsForJob(job.Job{Out: "/a", Mode: "incremental"}, nil, logger, runHooks{}, mode)
		if len(o.GmailYesStores) != 0 || o.GmailNotNow || o.GmailYes || o.AskGmail != nil {
			t.Fatalf("positive twin (mode %v): %+v %v %v", mode, o.GmailYesStores, o.GmailNotNow, o.AskGmail != nil)
		}
		o = optsForJob(job.Job{Out: "/a", Mode: "incremental", GmailYes: []string{"gmail-store"}, GmailNotNow: true}, nil, logger, runHooks{}, mode)
		if !reflect.DeepEqual(o.GmailYesStores, []string{"gmail-store"}) || !o.GmailNotNow || o.AskGmail != nil {
			t.Fatalf("mode %v: the answers kept here are not carried: %+v %v", mode, o.GmailYesStores, o.GmailNotNow)
		}
	}
}
