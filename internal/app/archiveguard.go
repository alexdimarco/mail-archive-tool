package app

// Proving, before a run writes its progress, that its folder is still the one it began in (the
// Gmail de-dup B3 adversarial pass, round 3's H1 and round 4's J1 to J3, back-ported to 1.7.2). A
// drive of a rotation can be swapped at the same path or letter, or the folder renamed, at any
// moment of a run: while it waits for Thunderbird, while it walks its sources (minutes for a large
// store, even when nothing is new), or after. The lock file goes with the old folder, so the folder
// at the run's path then holds another archive, or none. A plaintext save writes by path, so the
// run proves it first; so do its pages, README and report in the run's tail. An encrypted archive's
// saves prove the lock at their own commit point (container.ErrLockLost), but its sealed message
// files, its record and its run history are written by path between them, so an encrypted run
// proves the same.
//
// 1.7 keeps no archive id (it came with Gmail de-dup's slice B3), so the binding is the run's lock
// alone, plaintext or encrypted: the lock file in the folder must still hold this run's own nonce.
// A swapped-in folder never holds it (its own lock file, from its own last run, holds another; a
// folder that never had one holds none), so the lock alone catches the swap; it cannot tell another
// archive from this archive whose lock another run took, so neither gets anything of this run's.

import (
	"errors"
	"log"
	"os"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
)

// archiveRecheckEvery is how often, at most, a run's walk proves again that its folder is still
// its own between saves, so a long walk stops writing message files into another archive soon
// after a swap. A var so a test can make it due at every message.
var archiveRecheckEvery = 10 * time.Second

// archiveProofRetries are the pauses before a proof that failed is asked again, so a moment in
// which a healthy archive's folder or lock file cannot be read (a network folder reconnecting, a
// scanner) is not taken for a lost lock: a proof stops the run only when it fails every time, about
// a second and a quarter. Nothing is written while it waits (the proof is asked before the write).
// A var so a test can watch the run wait for it.
var archiveProofRetries = []time.Duration{250 * time.Millisecond, time.Second}

// afterWalkForTest, when a test sets it, is called once a local run has walked every source,
// before its last proof and its pages and README; in a Microsoft 365 capture once it has walked
// every mailbox, before the proof that comes before its footer and last save. Production code
// never sets it (runPause's steps are pinned by other tests, so this is a seam of its own).
var afterWalkForTest func()

// tailForTest, when a test sets it, is called at the start of a run's tail, which writes its
// folder pages, README and report: in a local run once it has passed the proof that follows its
// walk, in a Microsoft 365 capture once it has made its last manifest save. Production code never
// sets it.
var tailForTest func()

// archiveGuard proves, before every save of a run's progress, on time during its walk and before
// each write of its tail, that its folder is still there and that the lock file in it is still the
// run's own.
type archiveGuard struct {
	out       string
	stillHeld func() error
	proved    time.Time
	// everyWrite: an encrypted run, which proves after every message it writes (dueAfter); written
	// is how many it had written at its last such proof.
	everyWrite bool
	written    int
}

func newArchiveGuard(opts Options, stillHeld func() error) *archiveGuard {
	return &archiveGuard{out: opts.Out, stillHeld: stillHeld, proved: time.Now(), everyWrite: opts.Key != nil}
}

// prove returns why the run must write nothing more into its folder, nil when it may go on: the
// folder is gone (a drive pulled with none in its place: the run-fatal "unavailable" its next
// write would meet), or the lock file is no longer the run's (removed, or replaced: another
// archive's folder in its place, or another run). A failed proof is asked again
// (archiveProofRetries) before it counts.
func (g *archiveGuard) prove() error {
	g.proved = time.Now()
	return retryProof(func() error {
		if _, err := os.Lstat(g.out); err != nil {
			return unavailable(g.out, err)
		}
		return g.stillHeld()
	})
}

// lockGone is the deferred record's question: why the run's lock is no longer its own (the
// proof's retries included), nil when it is. A run whose lock is gone writes no record, run
// history entry, BACKUP-NEEDS-ATTENTION.txt or streak into the folder: it may hold another
// archive now, and when another run took the lock that run records its own.
func (g *archiveGuard) lockGone() error { return retryProof(g.stillHeld) }

// retryProof asks check, and again after each of archiveProofRetries while it fails; it returns
// the last answer.
func retryProof(check func() error) error {
	err := check()
	for _, pause := range archiveProofRetries {
		if err == nil {
			return nil
		}
		time.Sleep(pause)
		err = check()
	}
	return err
}

// proofStop is the run's error when prove (or a checkpoint's lock check) stopped it: the archive
// location gone stays its run-fatal refusal; anything else is a lost lock.
func proofStop(err error) error {
	var fatal *RunFatalError
	if errors.As(err, &fatal) {
		return err
	}
	return withKind(state.FailLockLost, err)
}

// due says a walk should prove again now, between saves.
func (g *archiveGuard) due() bool {
	return time.Since(g.proved) >= archiveRecheckEvery
}

// dueAfter is due for a walk that has written stats so far: an encrypted run proves again after
// every message it wrote too, so another archive swapped in receives at most the sealed files of
// the one message in flight, not a whole store's until the save. Its proof is a few small reads
// beside the message's sealed writes; a message only skipped writes nothing and asks no proof. A
// plaintext run proves on time.
func (g *archiveGuard) dueAfter(stats export.Stats) bool {
	if written := stats.Exported + stats.Filled; g.everyWrite && written != g.written {
		g.written = written
		return true
	}
	return g.due()
}

// writePages writes the folder pages of the run's tail, by path, proving first, on time between
// pages, and again before the root page that lists the archive's folders. It returns the proof's
// error when one stopped it: nothing more may be written into the folder. A page that could not be
// written is a warning, as it always was.
func (g *archiveGuard) writePages(idx *index.Index, logger *log.Logger, gaps int) error {
	if err := g.prove(); err != nil {
		return err
	}
	var stop error
	err := pages.GenerateChecked(g.out, idx, logger, gaps, func(rel string) error {
		if rel == pages.RootPage || g.due() {
			stop = g.prove()
		}
		return stop
	})
	if stop != nil {
		return stop
	}
	if err != nil && logger != nil {
		logger.Printf("warning: folder pages: %v", err)
	}
	return nil
}

// lockGoneWarning is the log line of a run whose deferred record found its lock no longer its own:
// nothing was recorded in the folder.
func lockGoneWarning(logger *log.Logger, out, what string) {
	if logger != nil {
		logger.Printf("warning: %s no longer holds this %s's lock (its drive was changed, the folder was moved, or another backup took it over); nothing was recorded in it", out, what)
	}
}
