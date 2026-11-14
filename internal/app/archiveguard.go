package app

// Proving, before a run writes its progress, that its folder still holds the archive it began with
// (the B3 adversarial pass, round 3's H1). A drive of a rotation can be swapped at the same path or
// letter, or the folder renamed, at any moment of a run: while it waits for Thunderbird, while it
// walks its sources (minutes for a large store, even when nothing is new), or after. The lock file
// goes with the old folder, so the folder at the run's path then holds another archive, or none.
// A plaintext save writes by path, so the run proves it first; so do its pages, README and report
// in the run's tail (round 4's J1). An encrypted archive's saves prove the lock at their own commit
// point (container.ErrLockLost), but its sealed message files, its record and its run history are
// written by path between them, so an encrypted run proves the same (round 4's J2).

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
)

// archiveRecheckEvery is how often, at most, a plaintext run's walk proves again that its folder
// still holds its archive between saves, so a long walk stops writing message files into another
// archive soon after a swap. A var so a test can make it due at every message.
var archiveRecheckEvery = 10 * time.Second

// afterWalkForTest, when a test sets it, is called once a local run has walked every source,
// before its last proof and its pages and README. Production code never sets it (runPause's
// steps are pinned by other tests, so this is a seam of its own).
var afterWalkForTest func()

// tailForTest, when a test sets it, is called at the start of a run's tail, which writes its
// folder pages, README and report (the B3 adversarial pass, round 4's J1): in a local run once it
// has passed the proof that follows its walk, in a Microsoft 365 capture once it has made its last
// manifest save. Production code never sets it.
var tailForTest func()

// archiveChangedFor says the folder opts.Out no longer holds the archive a plaintext run began
// with (began, the id its "running" record carries): its record carries another id, or carries
// none while the run's lock file is gone or replaced too (an archive an older MailArchive wrote).
// An encrypted run's is encryptedArchiveChangedFor: its record has no id.
func archiveChangedFor(opts Options, began string, stillHeld func() error) func() bool {
	if opts.Key != nil {
		return encryptedArchiveChangedFor(stillHeld)
	}
	return func() bool {
		if began == "" {
			return false
		}
		id := state.ArchiveID(opts.Out)
		if id == began {
			return false
		}
		return id != "" || stillHeld() != nil
	}
}

// encryptedArchiveChangedFor says an encrypted run's folder may no longer hold its archive (the B3
// adversarial pass, round 4's J2): its lock file there is gone or no longer its own. An encrypted
// archive's record has no id, and the vault's is not read here (no production code outside the
// unlock path may call vault.Detect, MA-726's gate), so the binding is the key, which the run's
// saves prove at their commit point, and the lock: a run whose lock is no longer its own records
// nothing in the folder, whichever archive is there now (the review's recommended fix). It needs
// no archive id, so a release before 1.8 can take it.
func encryptedArchiveChangedFor(stillHeld func() error) func() bool {
	return func() bool { return stillHeld() != nil }
}

// archiveGuard proves, before every save of a run's progress and on time during its walk, that
// its folder still holds its archive and that the lock is still its own. An encrypted run proves
// it too (round 4's J2): between its sealed saves it writes its sealed message files by path.
type archiveGuard struct {
	out       string
	stillHeld func() error
	// changed says the folder holds another archive (archiveChangedFor); nil where the run keeps
	// no archive id, when the lock alone is the proof.
	changed func() bool
	proved  time.Time
	// everyWrite: an encrypted run, which proves after every message it writes (dueAfter); written
	// is how many it had written at its last such proof.
	everyWrite bool
	written    int
}

func newArchiveGuard(opts Options, stillHeld func() error, changed func() bool) *archiveGuard {
	if opts.Key != nil {
		// An encrypted run's proof is its lock alone, told in the lock's own words: its changed is
		// the lock too (encryptedArchiveChangedFor), and cannot tell another archive from a lock
		// another run took.
		changed = nil
	}
	return &archiveGuard{out: opts.Out, stillHeld: stillHeld, changed: changed, proved: time.Now(), everyWrite: opts.Key != nil}
}

// prove returns why the run must write nothing more into its folder, nil when it may go on: the
// folder is gone (a drive pulled with none in its place: the run-fatal "unavailable" its next
// write would meet, never "another archive"), the folder holds another archive, or the lock file
// is no longer the run's.
func (g *archiveGuard) prove() error {
	g.proved = time.Now()
	if _, err := os.Lstat(g.out); err != nil {
		return unavailable(g.out, err)
	}
	if g.changed != nil && g.changed() {
		return archiveChangedDuringError(g.out)
	}
	return g.stillHeld()
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
// every message it wrote too (round 4's J2), so another archive swapped in receives at most the
// sealed files of the one message in flight, not a whole store's until the save. Its proof is a
// few small reads beside the message's sealed writes; a message only skipped writes nothing and
// asks no proof. A plaintext run proves on time, as round 3's H1 settled.
func (g *archiveGuard) dueAfter(stats export.Stats) bool {
	if written := stats.Exported + stats.Filled; g.everyWrite && written != g.written {
		g.written = written
		return true
	}
	return g.due()
}

// writePages writes the folder pages of the run's tail, by path, proving first, on time between
// pages, and again before the root page that lists the archive's folders (the B3 adversarial pass,
// round 4's J1). It returns the proof's error when one stopped it: nothing more may be written
// into the folder. A page that could not be written is a warning, as it always was.
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

// archiveChangedDuringError is a run's stop when its folder came to hold another archive after
// it began reading: its manifest, record and README were not written there. Message files it wrote
// before the proof that stopped it stay where they were written, as after any lost lock (R5); the
// proof on time bounds them to about archiveRecheckEvery of the walk.
func archiveChangedDuringError(out string) error {
	return fmt.Errorf("the archive folder %s now holds another archive than the one this backup began with (its drive was changed, or the folder was moved, during the backup): this backup stopped and recorded nothing in it. Run the backup again", out)
}

// changedEnd is the error a run's deferred record gives a run that would have ended ok, or partly
// ok, when the folder changed under it: a plaintext run's archiveChangedDuringError; an encrypted
// run's lock words, since its lock alone cannot tell another archive from a lock another run took.
func changedEnd(opts Options, stillHeld func() error) error {
	if opts.Key != nil {
		if err := stillHeld(); err != nil {
			return err
		}
	}
	return archiveChangedDuringError(opts.Out)
}
