package wizard

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/state"
)

// outcomeSession is a scripted pre-sync (the external binary's stand-in, C-41) that
// ends every session as the test says, and counts its calls. It starts nothing.
type outcomeSession struct {
	mu    sync.Mutex
	res   presync.Result
	calls int
}

func (f *outcomeSession) run(context.Context, presync.Config) presync.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.res
}

func (f *outcomeSession) set(o presync.Outcome) {
	f.mu.Lock()
	f.res = presync.Result{Outcome: o, Left: presync.LeftNone}
	f.mu.Unlock()
}

// covers: MA-908, R32, S65
// The sync streak through the real entry points (design-presync-attended §5.3, slice
// AS5; C-33, C-34): positive twin first, a scheduled RunHeadless of a sync job whose
// session missed starts the streak; the plaintext Run now of the same job, attended,
// that missed does not move it (C-33), and one that synced resets it; a scheduled run
// whose sync could not be prepared runs no session, records not-set-up with its
// warning, extends the streak and never deletes it (C-34); a scheduled run of a job with
// the sync off removes it.
func TestTheEntryPointsMoveTheSyncStreak(t *testing.T) {
	out, mbox := headlessFixture(t)
	fake := &outcomeSession{}
	fake.set(presync.OutcomeOwnerActiveSkipped)
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	r := presync.OverrideTablesForTest(nil, map[string]bool{runtime.GOOS: true})
	defer r()

	jobPath := filepath.Join(t.TempDir(), "mailarchive-nightly.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-nightly", Out: out, Mode: "incremental",
		Inputs: []string{mbox}, ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	streak := func(label string, missed int, last string) {
		t.Helper()
		s, err := state.ReadSyncStreak(out)
		if err != nil || s.Missed != missed || (last != "" && s.LastName != last) {
			t.Fatalf("%s: streak %+v (%v), want missed %d, last %q", label, s, err, missed, last)
		}
	}
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("the scheduled run exit = %d", code)
	}
	streak("positive twin: a scheduled run that missed", 1, "owner-active-skipped")

	logger := log.New(io.Discard, "", 0)
	if _, err := RunJobInProcess(context.Background(), nil, jobPath, logger, RunNow{}); err != nil {
		t.Fatal(err)
	}
	streak("a plaintext Run now that missed (C-33)", 1, "owner-active-skipped")
	fake.set(presync.OutcomeAlreadyRunning)
	if _, err := RunJobInProcess(context.Background(), nil, jobPath, logger, RunNow{}); err != nil {
		t.Fatal(err)
	}
	streak("a plaintext Run now that synced", 0, "")
	calls := fake.calls
	restore()

	// The state folder cannot be found: not-set-up.
	restore = presyncjob.OverrideSessionForTest(fake.run, "")
	defer restore()
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("the not-set-up scheduled run exit = %d", code)
	}
	if fake.calls != calls {
		t.Errorf("a session ran although the sync could not be prepared")
	}
	streak("a scheduled run whose sync could not be prepared", 1, "not-set-up")
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || lr.Presync == nil || lr.Presync.Name != "not-set-up" {
		t.Fatalf("the record: %+v (%v)", lr.Presync, err)
	}
	found := false
	for _, w := range lr.Warnings {
		found = found || w.Code == "presync_not_set_up"
	}
	if !found {
		t.Errorf("the record's warnings %+v lack presync_not_set_up", lr.Warnings)
	}

	// The sync switched off: the scheduled run removes the streak.
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-nightly", Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("the sync-off scheduled run exit = %d", code)
	}
	if _, err := os.Lstat(filepath.Join(out, state.SyncStreakName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a scheduled run of a job with the sync off left the sync streak (%v)", err)
	}
}
