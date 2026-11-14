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
// folder that never had one holds none), so the lock alone catches the swap. It cannot tell another
// archive from this archive whose lock file was removed, so once the lock is lost the run's record
// asks one thing more (endUnheld, the round-5 review's K3): whether the folder's manifest (an
// encrypted archive's record pointer) still holds the bytes the run last read or wrote. Then the
// failure is recorded there; otherwise nothing is.

import (
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/container"
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
	// recorded is the file that binds the run to its archive's folder and its digest as the run
	// last read or wrote it (recordedFile); nil before the run has opened its manifest.
	recorded func() (path string, sum [32]byte, known bool)
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

// lockGone is why the run's lock is no longer its own (the proof's retries included), nil when it
// is.
func (g *archiveGuard) lockGone() error { return retryProof(g.stillHeld) }

// endUnheld is the deferred record's question (the round-5 review's K3, the lead's call): nil
// when the run's lock is still its own, so its record is written as always. Otherwise end is the
// error the run ends with when it would have ended ok or partly ok — the lock's words, or the
// run-fatal "unavailable" when the folder is gone (a drive pulled, none in its place) — and here
// says whether the folder still holds the archive this run last read or wrote (sameArchive): then
// its failure is recorded there, as 1.7.1 recorded a lost lock (a lock file removed from under a
// run, with no swap, is told as "failed, lock lost", never as a crash). Not here, nothing is
// written into the folder: no record, run history entry, BACKUP-NEEDS-ATTENTION.txt or streak; it
// may hold another archive, or another run that took the lock has saved since and records its
// own.
func (g *archiveGuard) endUnheld() (end error, here bool) {
	gone := g.lockGone()
	if gone == nil {
		return nil, true
	}
	if _, err := os.Lstat(g.out); err != nil {
		return unavailable(g.out, err), false
	}
	return withKind(state.FailLockLost, gone), g.sameArchive()
}

// sameArchive says the folder still holds the archive this run last read or wrote: the file that
// binds them (recorded: a plaintext archive's manifest, an encrypted archive's record pointer)
// hashes to what the run last read from it or wrote to it. Absent, unreadable, different, or
// nothing read or written yet: no.
func (g *archiveGuard) sameArchive() bool {
	if g.recorded == nil {
		return false
	}
	path, want, known := g.recorded()
	if !known || path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	var got [32]byte
	copy(got[:], h.Sum(nil))
	return got == want
}

// recordedFile is the file whose bytes bind a run to its archive's folder (sameArchive) and their
// digest as the run last read or wrote them: an encrypted archive's record pointer
// (container.PointerName); a plaintext archive's manifest when it is in the folder (one kept
// elsewhere by -manifest says nothing of the folder, so the run records nothing).
func recordedFile(opts Options, mpath string, manifest *state.Manifest, sealed *container.Store) (string, [32]byte, bool) {
	if sealed != nil {
		sum, ok := sealed.RecordDigest()
		return filepath.Join(opts.Out, container.PointerName), sum, ok
	}
	if manifest == nil || !PathIn(mpath, opts.Out) {
		return "", [32]byte{}, false
	}
	sum, ok := manifest.FileDigest()
	return mpath, sum, ok
}

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

// writePages writes the folder pages of the run's tail, by path, proving first and again before
// every page, the root page that lists the archive's folders last: a page is written over the
// page of the same folder in whatever archive the folder holds, so each is proved, not only on
// time (the round-5 review's K2; a proof costs about a tenth of a millisecond on a local disk and
// one or two over SMB, small beside a page write). It returns the proof's error when one stopped
// it: nothing more may be written into the folder. A page that could not be written is a
// warning, as it always was.
func (g *archiveGuard) writePages(idx *index.Index, logger *log.Logger, gaps int) error {
	if err := g.prove(); err != nil {
		return err
	}
	var stop error
	err := pages.GenerateChecked(g.out, idx, logger, gaps, func(string) error {
		stop = g.prove()
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

// replaceGuard is the exporter's BeforeReplace for a run (the round-5 review's K1): right before
// a file already in the folder is written over or removed, the folder is proved to still be the
// run's, whenever the proof on time last ran; a refusal is kept in lost (the walk's lost lock) and
// stops the walk. New files keep the on-time bound.
func (g *archiveGuard) replaceGuard(lost *error) func(string) error {
	return func(string) error {
		if *lost == nil {
			*lost = g.prove()
		}
		return *lost
	}
}

// lockGoneWarning is the log line of a run whose deferred record found its lock no longer its own:
// nothing was recorded in the folder.
func lockGoneWarning(logger *log.Logger, out, what string) {
	if logger != nil {
		logger.Printf("warning: %s no longer holds this %s's lock (its drive was changed, the folder was moved, or another backup took it over); nothing was recorded in it", out, what)
	}
}
