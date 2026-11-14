package wizard

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
)

// covers: MA-1315, R45
// An encrypted archive's Run now carries a yes the Desktop keeps in memory for the open archive
// (RunNow.GmailYes, the owner's ruling on F18) as the run's own yes, and hands the run's Gmail
// outcome back (RunNow.OnGmail), in memory only, so the card can ask. Positive twin first: with
// no yes kept, the run carries none.
func TestAnEncryptedRunNowCarriesTheDesktopsYes(t *testing.T) {
	out := t.TempDir()
	path := filepath.Join(t.TempDir(), "job.json")
	if err := job.Write(path, job.Job{Name: "a", Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	quiet := log.New(io.Discard, "", 0)
	var yes []bool
	told := 0
	for _, kept := range []bool{false, true} {
		RunJobInProcessKeyed(context.Background(), context.Background(), path, quiet,
			RunNow{GmailYes: kept, OnGmail: func(app.GmailOutcome) { told++ }},
			func(o *app.Options) (func(), error) {
				yes = append(yes, o.GmailYes)
				return func() {}, nil // no key: the run refuses, and still says what it did about Gmail
			})
	}
	if len(yes) != 2 || yes[0] || !yes[1] {
		t.Fatalf("the run's yes, without then with the Desktop's: %v", yes)
	}
	if told != 2 {
		t.Fatalf("the run's Gmail outcome was handed back %d times, want 2", told)
	}
}
