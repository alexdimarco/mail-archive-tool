package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-545, R18, S29
// A "running" last-run record — written only by an export or a Graph run — is
// judged by WHO holds the archive lock, not just whether someone does: only an
// export or a graph run holding it, or a holder that names no verb (status's
// own probe, or a binary older than the verb), can be that run, so the record
// reads as a run in progress (the note run_in_progress, and RED run_stuck once
// it is older than the schedule period). While a verify, a reindex, an
// extract or any other verb holds the lock — "purge" stands for a verb this
// build does not know, so the live set is an allowlist, never a list of the
// verbs that are not live — the export that wrote the record was killed: RED
// run_never_finished at once, naming the remedy, never the in-progress note
// and never a period's wait for run_stuck. status's human `Last run:` line
// (Summary, which the CLI, the GUI and the desktop view print) says the same:
// "→ running" only while the record reads as a run in progress, "→ never
// finished" for every other holder and with the lock free, never "→ running"
// above the RED. The record is the one a killed export leaves: a real export,
// then the real beginRun of a second one that never finished. Positive twin
// first: the lock really held as export, graph and no verb.
func TestRunningExportIsJudgedByTheLockHolder(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := tmpDir(t)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	beginRun(opts) // a second export took the lock, wrote "running" and was killed
	if lr, st, err := state.ReadLastRun(out); err != nil || st != state.LastRunPresent || lr.Status != state.RunRunning {
		t.Fatalf("fixture: last-run record %q (state %v, err %v), want the running record a killed export leaves", lr.Status, st, err)
	}

	pastThePeriod := time.Now().Add(3 * 24 * time.Hour) // no schedule: the period is a day
	for _, tc := range []struct {
		holder string
		live   bool
	}{
		{"export", true},
		{"graph", true},
		{"", true},
		{"verify", false},
		{"reindex", false},
		{"extract", false},
		{"purge", false}, // a verb this build does not know
	} {
		held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), tc.holder)
		if err != nil {
			t.Fatal(err)
		}
		in := health.Gather(out, "")
		held.Release()
		if !in.LockHeld {
			t.Fatalf("holder %q: status did not see the archive lock held", tc.holder)
		}
		now, later := health.Assess(in, time.Now()), health.Assess(in, pastThePeriod)
		who := "the lock held by " + strconv.Quote(tc.holder)
		line := assure.Reached(t, summaryLine(in, now, "Last run:"), "status's Last run line with "+who)
		if tc.live {
			assure.Reached(t, reasonFor(now, "run_in_progress"), "the run_in_progress note with "+who)
			assure.Reached(t, reasonFor(later, "run_stuck"), "the run_stuck RED past the period with "+who)
			if r := reasonFor(now, "run_never_finished"); r != "" {
				t.Errorf("holder %q: a run that may still be running is reported never finished: %s", tc.holder, r)
			}
			if !strings.HasSuffix(line, "→ running") {
				t.Errorf("holder %q: Summary %q, want the run shown as running", tc.holder, line)
			}
			continue
		}
		never := assure.Reached(t, reasonFor(now, "run_never_finished"), "the run_never_finished RED with "+who)
		if now.Posture != "RED" || !strings.Contains(never, "run again") {
			t.Errorf("holder %q: posture %s, reason %q — want RED naming the remedy", tc.holder, now.Posture, never)
		}
		if r := reasonFor(now, "run_in_progress"); r != "" {
			t.Errorf("holder %q: the killed export's record reads as a run in progress: %s", tc.holder, r)
		}
		if r := reasonFor(later, "run_stuck"); r != "" || reasonFor(later, "run_never_finished") == "" {
			t.Errorf("holder %q: past the period the record is %v, want run_never_finished and never run_stuck", tc.holder, later.Codes)
		}
		if !strings.HasSuffix(line, "→ never finished") {
			t.Errorf("holder %q: Summary %q above %q, want the run shown as never finished", tc.holder, line, never)
		}
	}

	// With the lock free the run that wrote the record is gone: the same RED,
	// and the same "never finished" on the Last run line.
	in := health.Gather(out, "")
	if in.LockHeld {
		t.Fatalf("the archive lock is still held (verb %q) after every holder released it", in.LockVerb)
	}
	rep := health.Assess(in, time.Now())
	never := assure.Reached(t, reasonFor(rep, "run_never_finished"), "the run_never_finished RED with the lock free")
	if line := summaryLine(in, rep, "Last run:"); !strings.HasSuffix(line, "→ never finished") {
		t.Errorf("lock free: Summary %q above %q, want the run shown as never finished", line, never)
	}
}
