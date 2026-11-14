package wizard

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
)

// covers: MA-1315, MA-1324, R45
// An encrypted archive's Run now carries a yes the Desktop keeps in memory for the open archive
// (RunNow.GmailYesStores, the owner's ruling on F18) as the run's yes for the stores the question
// named, never as a yes for every Gmail account the run reads (the B3 adversarial pass, round 2's
// G6), and hands the run's Gmail outcome back (RunNow.OnGmail), in memory only, so the card can
// ask. Positive twin first: with no yes kept, the run carries none.
func TestAnEncryptedRunNowCarriesTheDesktopsYes(t *testing.T) {
	out := t.TempDir()
	path := filepath.Join(t.TempDir(), "job.json")
	if err := job.Write(path, job.Job{Name: "a", Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	quiet := log.New(io.Discard, "", 0)
	var carried []app.Options
	told := 0
	for _, kept := range [][]string{nil, {"imap.gmail.com"}} {
		RunJobInProcessKeyed(context.Background(), context.Background(), path, quiet,
			RunNow{GmailYesStores: kept, OnGmail: func(app.GmailOutcome) { told++ }},
			func(o *app.Options) (func(), error) {
				carried = append(carried, *o)
				return func() {}, nil // no key: the run refuses, and still says what it did about Gmail
			})
	}
	if len(carried) != 2 || len(carried[0].GmailYesStores) != 0 || carried[0].GmailYes {
		t.Fatalf("positive twin: with no yes kept the run carries %v / %v", carried[0].GmailYesStores, carried[0].GmailYes)
	}
	if !slices.Equal(carried[1].GmailYesStores, []string{"imap.gmail.com"}) || carried[1].GmailYes {
		t.Fatalf("the Desktop's yes is carried as %v (a yes for every store: %v), want the named store only", carried[1].GmailYesStores, carried[1].GmailYes)
	}
	if told != 2 {
		t.Fatalf("the run's Gmail outcome was handed back %d times, want 2", told)
	}
}
