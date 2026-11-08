package wizard

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// covers: MA-1156, MA-1157, R18, S29
// Run now after a backup that did not save everything also says what is not downloaded in full
// yet, as it does after a backup that finished (the 1.6.1 friction review's P6: the partial run's
// note left the sentence out), worded by where the run read its mail (P7: a plain mbox store is
// a mail file, so no mail app is blamed). The run's own words — what the record, the history and
// the email carry — are unchanged, and the result still matches every partial code. Positive
// twin first: a finished Run now says the sentence.
func TestAPartialRunNowSaysWhatIsNotDownloadedToo(t *testing.T) {
	ma956JobEnv(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "mstore")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	mbox := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(store, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mbox("A", "From a@b Mon Jan 01 00:00:00 2024\nFrom: a@b\nSubject: whole\nMessage-ID: <whole@y>\n\nbody\n\n"+
		"From a@b Mon Jan 01 00:00:00 2024\nFrom: a@b\nSubject: headers only\nMessage-ID: <headers@y>\n\n")
	out := filepath.Join(dir, "archive")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("the first backup: %v", err)
	}
	jp := filepath.Join(dir, "mailarchive-partial.json")
	if err := job.Write(jp, job.Job{Name: "mailarchive-partial", Out: out, Mode: "incremental", Inputs: []string{store}}); err != nil {
		t.Fatal(err)
	}
	quiet := log.New(io.Discard, "", 0)
	sentence := state.NotDownloadedSentence(1, state.FromMailFile)
	res, err := RunJobInProcess(context.Background(), context.Background(), jp, quiet, RunNow{})
	if err != nil || !strings.Contains(res, "\n"+sentence) {
		t.Fatalf("the positive twin: a finished Run now: %v\n%s", err, res)
	}

	// A new folder B whose place in the archive a file holds: B cannot be written.
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil || len(m.Stores) != 1 {
		t.Fatalf("the manifest's stores: %v %v", m.Stores, err)
	}
	var tok string
	for _, v := range m.Stores {
		tok = v
	}
	mbox("B", "From a@b Mon Jan 01 00:00:00 2024\nFrom: a@b\nSubject: blocked\nMessage-ID: <blocked@y>\n\nbody\n\n")
	if err := os.WriteFile(filepath.Join(out, tok, "B"), []byte("a file, not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = RunJobInProcess(context.Background(), context.Background(), jp, quiet, RunNow{})
	var pe *app.PartialError
	if !errors.Is(err, app.ErrPartial) || !errors.As(err, &pe) {
		t.Fatalf("want a partial run, got %v", err)
	}
	words := pe.Error()
	assure.Reached(t, strings.HasPrefix(words, state.PartialLead) && err.Error() == words+"\n"+sentence,
		"the partial Run now says the kinds' words, then the not-downloaded sentence: "+err.Error())
	lr, _, _ := state.ReadLastRun(out)
	if lr.Error != words || strings.Contains(lr.Error, sentence) || lr.FillableFrom != state.FromMailFile {
		t.Errorf("the record keeps the run's own words: %q (from %q)", lr.Error, lr.FillableFrom)
	}
	if _, skipped := app.BusySkipped(err); skipped {
		t.Error("a partial run read as a skip")
	}
}
