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
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
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
	// ctx is the run's: a cancel (Cancel, Ctrl-C, MailArchive Desktop's Shut down) ends a wait at
	// once (the 1.7.2 re-check's Q2). nil in a test that builds a guard by hand: never cancelled.
	ctx context.Context
	// logger hears when a proof starts and stops waiting for the folder (the re-check's Q5); nil
	// says nothing.
	logger *log.Logger
}

func newArchiveGuard(ctx context.Context, opts Options, logger *log.Logger, stillHeld func() error) *archiveGuard {
	return &archiveGuard{out: opts.Out, stillHeld: stillHeld, proved: time.Now(), everyWrite: opts.Key != nil, ctx: ctx, logger: logger}
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
// definite answer (settle) is kept: every proof after it gives it at once. A cancel is the run's
// own answer, returned as it is and never kept.
func (g *archiveGuard) prove() error {
	if g.lost != nil {
		return g.lost
	}
	if err := g.settle(); err != nil {
		if !cancelled(err) {
			g.lost = err
		}
		return err
	}
	g.proved = time.Now()
	return nil
}

// cancelled says err is a run's cancel (its context's), not an answer about its folder.
func cancelled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// settle asks until it has an answer: nil (the folder is there and its lock file is the run's),
// the lock's error (read, and not the run's), or, once archiveUnreadableFor has passed with
// nothing but "could not read", the run-fatal "unavailable" of a folder that cannot be reached. A
// cancel of the run ends the wait at once and is the answer (the re-check's Q2), so a Cancel or a
// Shut down during it is seen at once and the run is recorded cancelled. The wait is told in the
// run's log as it starts and as it ends (Q5).
func (g *archiveGuard) settle() error {
	deadline := time.Now().Add(archiveUnreadableFor)
	var began time.Time
	for i := 0; ; i++ {
		unreadable, err := g.ask()
		if !unreadable {
			if !began.IsZero() {
				g.say("the archive folder can be reached again after %s.", durationWords(time.Since(began)))
			}
			return err
		}
		if began.IsZero() {
			if g.ctx != nil && g.ctx.Err() != nil {
				return g.ctx.Err() // a run already cancelled does not start a wait
			}
			began = time.Now()
			g.say("waiting up to %s for the archive folder to be reachable again… (%v)", waitWords(), err)
		}
		left := time.Until(deadline)
		if left <= 0 {
			g.say("the archive folder was still not reachable after %s.", waitWords())
			return unavailable(g.out, err)
		}
		pause := time.NewTimer(min(archiveProofPauses[min(i, len(archiveProofPauses)-1)], left))
		select {
		case <-g.done():
			pause.Stop()
			// One more look, without waiting (the 1.7.2 final check's security-2): a folder
			// that is back answers as any proof does, so a run whose lock is still its own
			// goes on to its own cancel, which saves its progress first. The look is bounded
			// (cancelLookFor, the last check's sec-2): one that has not answered is given up
			// and the cancel returned.
			if answered, unreadable, err := g.askWithin(cancelLookFor); answered && !unreadable {
				g.say("the archive folder can be reached again after %s.", durationWords(time.Since(began)))
				return err
			}
			g.say("stopped waiting for the archive folder: the backup was cancelled.")
			return g.ctx.Err()
		case <-pause.C:
		}
	}
}

// cancelLookFor bounds the one more look a cancel takes in a wait (the 1.7.2 last check's sec-2):
// on a network share that has stopped answering, each look can wait for the client's timeout, and
// a Cancel or MailArchive Desktop's Shut down must not wait for it.
var cancelLookFor = 250 * time.Millisecond

// askWithin asks once and waits at most d for the answer: answered says it came in time. A look
// given up goes on until the system answers it; its answer is dropped.
func (g *archiveGuard) askWithin(d time.Duration) (answered, unreadable bool, err error) {
	type answer struct {
		unreadable bool
		err        error
	}
	got := make(chan answer, 1) // room for the answer, so a look given up still ends
	go func() {
		unreadable, err := g.ask()
		got <- answer{unreadable, err}
	}()
	limit := time.NewTimer(d)
	defer limit.Stop()
	select {
	case a := <-got:
		return true, a.unreadable, a.err
	case <-limit.C:
		return false, true, nil
	}
}

// done is the run's cancel channel; nil, which never fires, for a guard with no context.
func (g *archiveGuard) done() <-chan struct{} {
	if g.ctx == nil {
		return nil
	}
	return g.ctx.Done()
}

// say writes one line of the wait into the run's log.
func (g *archiveGuard) say(format string, args ...any) {
	if g.logger != nil {
		g.logger.Printf(format, args...)
	}
}

// durationWords is how long a wait took, in words: "less than a second", "1 second", "4
// seconds" (the final check's wording-9).
func durationWords(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case d < time.Second:
		return "less than a second"
	case s == 1:
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", s)
}

// waitWords is archiveUnreadableFor in words: "30 seconds".
func waitWords() string {
	s := int(archiveUnreadableFor.Round(time.Second) / time.Second)
	if s <= 1 {
		return "a second"
	}
	return fmt.Sprintf("%d seconds", s)
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
		if unreadable, rerr := g.recheckRemoved(lost.Path); unreadable {
			return true, rerr
		}
	}
	return false, err
}

// recheckRemoved looks again before a lock file "not found" counts as deleted (the 1.7.2
// re-check's Q3): over a network a client can say "not found" of what it cannot reach just now,
// and answer a look at the folder itself from its cache. So the folder's parent must be there, the
// folder must be listed afresh (a listing goes to the drive or share), and the lock file must be
// missing from that listing; otherwise the answer is "could not read", and the proof waits.
func (g *archiveGuard) recheckRemoved(lockPath string) (unreadable bool, err error) {
	if _, err := os.Stat(filepath.Dir(g.out)); err != nil {
		return true, err
	}
	f, err := os.Open(g.out)
	if err != nil {
		return true, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		return true, err
	}
	if slices.Contains(names, filepath.Base(lockPath)) {
		return true, fmt.Errorf("%s is listed in its folder but could not be found", lockPath)
	}
	return false, nil
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
// lock and records its own. A run cancelled while its folder cannot be read ends cancelled, and
// is recorded so only where the folder still holds its archive.
func (g *archiveGuard) endUnheld() (end error, here bool) {
	err := g.prove()
	if err == nil {
		return nil, true
	}
	here = g.sameArchive()
	var fatal *RunFatalError
	if errors.As(err, &fatal) || cancelled(err) {
		return err, here
	}
	return stoppedError(g.out, err, here), here
}

// closeOut is the deferred record's step of Run and RunGraph: it asks endUnheld, puts its end into
// *err where the run would otherwise have ended ok, partly ok, on a lost lock or on its folder being
// unavailable, logs the warning of a run whose folder is there but no longer its own (F4, G6), and
// says whether the run's record may be written into the folder.
func (g *archiveGuard) closeOut(err *error) bool {
	end, here := g.endUnheld()
	if end == nil {
		// The lock is the run's after all: a run that stopped on a lock it could not read at the
		// time (the walk's own probe, lockfile.StillHeld's raw sentence) says what it met, the
		// archive location unreachable for a moment, in the plain sentence, never the lock's
		// internal one (the 1.7.2 final check's wording-6).
		var lost *lockfile.LostError
		if FailureKind(*err) == state.FailLockLost && errors.As(*err, &lost) && lost.Err != nil && !isStopped(*err) {
			*err = unavailable(g.out, lost.Err)
		}
		return true
	}
	if replacesEnd(*err) {
		*err = end
	}
	if here {
		return true
	}
	var fatal *RunFatalError
	if !errors.As(end, &fatal) && !cancelled(end) {
		stoppedWarning(g.logger, g.out)
	}
	return false
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
	if errors.As(err, &fatal) || cancelled(err) {
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
// review's F1 and F6, its re-walk's G1, G2 and G6, the final check's wording-7 and wording-13),
// here only: plain words by what the run found at its end, a clause the surfaces lead into
// ("Backup failed: …", "Reason: …"); the folder-changed sentence ends on a short "Details:" (what
// was found), without the lock file's path or the system's error; the two own-lock sentences say
// it all in their lead and have none.
const (
	stoppedChangedWords   = "the archive folder changed during the backup. Another drive or folder is now at %s (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: %s."
	stoppedDeletedWords   = "MailArchive's lock file in the archive folder (%s) was deleted during the backup, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder."
	stoppedRewrittenWords = "MailArchive's lock file in the archive folder (%s) was changed during the backup: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished."
	stoppedWarningWords   = "warning: this backup's result was not written into %s: another drive or folder is there now, or another backup took it over."
)

// stoppedErr is a backup stopped because its lock is no longer its own: its words (Error) are the
// person's; it unwraps to the lock's own error, and its kind is lock_lost.
type stoppedErr struct {
	msg string
	err error
}

func (e *stoppedErr) Error() string { return e.msg }
func (e *stoppedErr) Unwrap() error { return e.err }

// lockCause is what a failed lock check met, for a sentence's "Details:": the system's error
// when the lock file could not be read, the lock's short sentence otherwise; never the lock's
// internal sentence (the final check's wording-6).
func lockCause(err error) error {
	var le *lockfile.LostError
	if !errors.As(err, &le) {
		return err
	}
	if le.Err != nil {
		return le.Err
	}
	return errors.New(le.Short())
}

// isStopped says err already carries a stopped backup's plain words.
func isStopped(err error) bool {
	var s *stoppedErr
	return errors.As(err, &s)
}

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
		msg = fmt.Sprintf(stoppedDeletedWords, lockfile.Name)
	default:
		msg = fmt.Sprintf(stoppedRewrittenWords, lockfile.Name)
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
