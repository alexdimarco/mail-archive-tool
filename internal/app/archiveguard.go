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
// asks more (endUnheld, the round-5 review's K3): whether the folder still holds the "running"
// record this run wrote at its start, and the manifest (an encrypted archive's record pointer) the
// run last read or wrote. Then the failure is recorded there; otherwise nothing is.
//
// A proof is asked only before a write (the 1.7.2 review's P1): never on a timer while a run only
// reads, so a network folder that cannot be reached for a moment while a backup walks mail it
// already has does not fail it. And it tells two answers apart: "read, and not this run's" (the
// lock file holds another run's mark, or is gone from a folder that is there) stops the run at
// once; "could not read" (the folder or the lock file: gone, a permission, an I/O error, a share
// reconnecting) is asked again for about 30 seconds while nothing is written, and only then ends
// the run as the archive location being unavailable.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
)

// archiveRecheckEvery is how long a plaintext walk may write new message files into its folder
// before it proves again, right before its next write, that the folder is still its own: another
// archive swapped in receives at most this much of the run's new files. A walk that only reads
// asks nothing. A var so a test can make a proof due before every message's write.
var archiveRecheckEvery = 10 * time.Second

// archiveUnreadableFor is how long a proof that could not read the folder or its lock file keeps
// asking before the folder counts as unavailable (the 1.7.2 review's P1): long enough for a network
// folder to reconnect. Nothing is written while it asks. A var so a test of the run-fatal ending
// need not wait it out.
var archiveUnreadableFor = 30 * time.Second

// archiveProofPauses are the pauses between the asks of a proof that could not read: they grow,
// and the last repeats until archiveUnreadableFor has passed.
var archiveProofPauses = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

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

// archiveGuard proves, before every save of a run's progress, before its writes during its walk
// and before each write of its tail, that its folder is still there and that the lock file in it
// is still the run's own.
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
	// began is the digest of the "running" record the run wrote into its folder at its start
	// (noteBegun); beganKnown says it was read.
	began      [32]byte
	beganKnown bool
	// lost is the answer of the proof that stopped the run: nothing after it asks, or waits, again.
	lost error
}

func newArchiveGuard(opts Options, stillHeld func() error) *archiveGuard {
	return &archiveGuard{out: opts.Out, stillHeld: stillHeld, proved: time.Now(), everyWrite: opts.Key != nil}
}

// noteBegun keeps the digest of the "running" last-run record the run has just written into its
// folder (beginRun), while the folder holds the run's lock. The record names the run's own start,
// to the nanosecond, so no other folder holds it and no other run leaves it there: it binds a
// brand-new archive that has no manifest yet (the 1.7.2 review's P6), and it tells apart two
// archives whose manifests hold the same bytes (P7).
func (g *archiveGuard) noteBegun() {
	g.began, g.beganKnown = fileDigest(filepath.Join(g.out, state.LastRunName))
}

// prove returns why the run must write nothing more into its folder, nil when it may go on. A
// definite answer (settle) is kept: every proof after it gives it at once.
func (g *archiveGuard) prove() error {
	if g.lost != nil {
		return g.lost
	}
	if err := g.settle(); err != nil {
		g.lost = err
		return err
	}
	g.proved = time.Now()
	return nil
}

// settle asks until it has an answer: nil (the folder is there and its lock file is the run's),
// the lock's error (read, and not the run's), or, once archiveUnreadableFor has passed with
// nothing but "could not read", the run-fatal "unavailable" of a folder that cannot be reached.
func (g *archiveGuard) settle() error {
	deadline := time.Now().Add(archiveUnreadableFor)
	for i := 0; ; i++ {
		unreadable, err := g.ask()
		if !unreadable {
			return err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return unavailable(g.out, err)
		}
		time.Sleep(min(archiveProofPauses[min(i, len(archiveProofPauses)-1)], left))
	}
}

// ask is one look: nil when the folder is the run's; otherwise why not, and unreadable when the
// only answer is that the folder or its lock file could not be read (gone, a link whose target is
// gone, a permission, an I/O error, a share reconnecting). The folder is followed through a link
// (os.Stat, the review's P8).
func (g *archiveGuard) ask() (unreadable bool, err error) {
	if _, err := os.Stat(g.out); err != nil {
		return true, err
	}
	err = g.stillHeld()
	var lost *lockfile.LostError
	if err == nil || !errors.As(err, &lost) {
		return false, err
	}
	if lost.Unreadable() {
		return true, lost.Err
	}
	if lost.Removed() {
		// A lock file not found is an answer only while its folder is there: a share that
		// dropped between the two looks says "not found" of both.
		if _, serr := os.Stat(g.out); serr != nil {
			return true, serr
		}
	}
	return false, err
}

// gone tells the guard that the run's walk already ended on its folder's being unavailable (a
// write that found it gone): when the folder cannot be looked at now either, no proof after waits
// for it again, and the run ends on the walk's own words.
func (g *archiveGuard) gone(fatal *RunFatalError) {
	if g.lost != nil || fatal == nil {
		return
	}
	if _, err := os.Stat(g.out); err != nil {
		g.lost = fatal
	}
}

// endUnheld is the deferred record's question (the round-5 review's K3, the lead's call): nil
// when the run's lock is still its own, so its record is written as always. Otherwise end is the
// error the run ends with — the stopped run's plain words (stoppedError), or the run-fatal
// "unavailable" when the folder could not be read — and here says whether the folder still holds
// the archive this run began in (sameArchive): then its failure is recorded there, as 1.7.1
// recorded a lost lock (a lock file removed from under a run, with no swap, is "failed", never a
// crash). Not here, nothing is written into the folder: no record, run history entry,
// BACKUP-NEEDS-ATTENTION.txt or streak; it may hold another archive, or another run that took the
// lock and records its own.
func (g *archiveGuard) endUnheld() (end error, here bool) {
	err := g.prove()
	if err == nil {
		return nil, true
	}
	here = g.sameArchive()
	var fatal *RunFatalError
	if errors.As(err, &fatal) {
		return err, here
	}
	return stoppedError(g.out, err, here), here
}

// sameArchive says the folder still holds the archive this run began in: its last-run record is
// still the "running" record the run wrote at its start (noteBegun: no other run has begun there
// since, and no other folder holds it), and the file that binds the run to its archive (recorded: a
// plaintext archive's manifest, an encrypted archive's record pointer) hashes to what the run last
// read from it or wrote to it. A brand-new archive that has no manifest yet is bound by its running
// record alone. Absent, unreadable, different, or nothing to compare: no.
func (g *archiveGuard) sameArchive() bool {
	if g.beganKnown {
		if sum, ok := fileDigest(filepath.Join(g.out, state.LastRunName)); !ok || sum != g.began {
			return false
		}
	}
	if g.recorded != nil {
		if path, want, known := g.recorded(); known && path != "" {
			sum, ok := fileDigest(path)
			return ok && sum == want
		}
	}
	return g.beganKnown
}

// fileDigest is the SHA-256 of the file at path; ok is false when it cannot be read.
func fileDigest(path string) (sum [32]byte, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return sum, false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, false
	}
	copy(sum[:], h.Sum(nil))
	return sum, true
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

// proofStop is the run's error when prove (or a checkpoint's lock check) stopped it: the archive
// location unavailable stays its run-fatal refusal; anything else is a lost lock (whose words the
// deferred record puts plainly, endUnheld).
func proofStop(err error) error {
	var fatal *RunFatalError
	if errors.As(err, &fatal) {
		return err
	}
	return withKind(state.FailLockLost, err)
}

// due says the last proof is archiveRecheckEvery old: a write now proves first.
func (g *archiveGuard) due() bool {
	return time.Since(g.proved) >= archiveRecheckEvery
}

// dueAfter says an encrypted walk wrote a message since its last such proof: it proves again
// after every message it wrote, so another archive swapped in receives at most the sealed files of
// the one message in flight, not a whole store's until the save (round 4's J2). Its proof is a few
// small reads beside the message's sealed writes; a message only skipped writes nothing and asks
// no proof. Never on time: a walk that only reads asks nothing (the 1.7.2 review's P1), and a
// plaintext walk proves before its next write instead (writeGuard).
func (g *archiveGuard) dueAfter(stats export.Stats) bool {
	if written := stats.Exported + stats.Filled; g.everyWrite && written != g.written {
		g.written = written
		return true
	}
	return false
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
// run's, whenever the last proof ran; a refusal is kept in lost (the walk's lost lock) and stops
// the walk.
func (g *archiveGuard) replaceGuard(lost *error) func(string) error {
	return func(string) error {
		if *lost == nil {
			*lost = g.prove()
		}
		return *lost
	}
}

// writeGuard is the exporter's BeforeMessageWrite for a run (the 1.7.2 review's P1): right before
// a message's files are written, the folder is proved when the last proof is archiveRecheckEvery
// old, so a walk that writes proves at least every 10 seconds of writing and a walk that only reads
// proves not at all. A refusal is kept in lost and stops the walk.
func (g *archiveGuard) writeGuard(lost *error) func() error {
	return func() error {
		if *lost == nil && g.due() {
			*lost = g.prove()
		}
		return *lost
	}
}

// The words of a backup stopped because its folder is no longer its own (the 1.7.2 friction
// review's F1 and F6), here only: plain words first, by what the run found at its end, then the
// lock's own sentence after "Details:", without the system's error.
const (
	stoppedChangedWords   = "Backup stopped: the archive folder changed during the backup. Another drive or folder is now at %s (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: %s."
	stoppedDeletedWords   = "Backup stopped: MailArchive's lock file in the archive folder (%s) was deleted during the backup, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder. Details: %s."
	stoppedRewrittenWords = "Backup stopped: MailArchive's lock file in the archive folder (%s) was changed during the backup: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished. Details: %s."
	stoppedWarningWords   = "warning: the folder at %s changed during this backup (another drive or folder is there now, or another backup took it over), so this backup's result was not written into it."
)

// stoppedErr is a backup stopped because its lock is no longer its own: its words (Error) are the
// person's; it unwraps to the lock's own error, and its kind is lock_lost.
type stoppedErr struct {
	msg string
	err error
}

func (e *stoppedErr) Error() string { return e.msg }
func (e *stoppedErr) Unwrap() error { return e.err }

// stoppedError words lost, the reason a proof found the run's lock no longer its own, for the
// person: here, the folder is this run's own archive whose lock file was deleted or changed;
// otherwise another drive or folder is at out, or another backup took it over.
func stoppedError(out string, lost error, here bool) error {
	details := lost.Error()
	removed := false
	var le *lockfile.LostError
	if errors.As(lost, &le) {
		details, removed = le.Short(), le.Removed()
	}
	var msg string
	switch {
	case !here:
		msg = fmt.Sprintf(stoppedChangedWords, out, details)
	case removed:
		msg = fmt.Sprintf(stoppedDeletedWords, lockfile.Name, details)
	default:
		msg = fmt.Sprintf(stoppedRewrittenWords, lockfile.Name, details)
	}
	return withKind(state.FailLockLost, &stoppedErr{msg: cleanText(msg), err: lost})
}

// stoppedWarning is the log line of a run whose deferred record found its folder no longer its
// own (the friction review's F4): its result was not written there.
func stoppedWarning(logger *log.Logger, out string) {
	if logger != nil {
		logger.Printf(stoppedWarningWords, cleanText(out))
	}
}

// replacesEnd says the deferred record's end replaces err, the run's own error: when the run would
// have ended ok or partly ok, or ended on a lost lock or on its folder being unavailable, whose
// words the end puts as what the run found at its end.
func replacesEnd(err error) bool {
	if err == nil || errors.Is(err, ErrPartial) || FailureKind(err) == state.FailLockLost {
		return true
	}
	var fatal *RunFatalError
	return errors.As(err, &fatal) && fatal.gone
}
