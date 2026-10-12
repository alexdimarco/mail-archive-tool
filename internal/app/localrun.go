package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The local run's handling of what goes wrong (fault-isolation §4.6, FC12, FC29).
// A fault in reading or writing ONE item or folder costs that item or folder: it is
// recorded in the capture-gap ledger and the walk goes on. A fault of the archive
// itself — the volume full, read-only or gone, an unsafe store token, a run of
// writes that all fail — stops the run, because the next item would fail the same
// way and "recording" thousands of them would only hide it.

// writeBreaker is how many failed write attempts in a row end a source's walk: item
// writes, and each folder's first attempt (its later items are not attempted).
const writeBreaker = 50

// probeEvery is how many write faults pass between two checks that the archive is
// still there; the first fault of a run is always checked.
const probeEvery = 50

// RunFatalError ends a run: the archive's location or volume can no longer be
// written, or the writes keep failing. Msg is the whole refusal, in the words of
// §7.5, naming the location and what to do.
type RunFatalError struct {
	Msg string
	Err error
}

func (e *RunFatalError) Error() string { return e.Msg }
func (e *RunFatalError) Unwrap() error { return e.Err }

// SourceStoppedError ends ONE source's walk, not the run: the local breaker's refusal
// (50 writes in a row failed at this input). What it stopped is recorded in the
// ledger and kept; the next source is read. Writes that fail like this at the
// archive's own volume are run-fatal first (RunFatalError), and a vanished location
// is caught by the probe, so what reaches the breaker is the file's own failures as
// often as the drive's (S2A-AC3).
type SourceStoppedError struct {
	Msg string
	Err error
}

func (e *SourceStoppedError) Error() string { return e.Msg }
func (e *SourceStoppedError) Unwrap() error { return e.Err }

// cleanText strips what a data file's own text must not carry into a terminal, the
// last-run record or BACKUP-NEEDS-ATTENTION.txt: control characters (C0, DEL, C1,
// the 8-bit CSI among them) and the bidi overrides (S2A-AO5).
func cleanText(s string) string { return util.StripBidi(util.StripControl(s)) }

const keptNote = "Messages written before this point are kept; nothing was recorded as archived that was not fully written."

// runFatalFor words err (which export.IsRunFatal or the probe judged fatal) for the
// operator: a full drive, a read-only location, an unsafe token, or one that is
// gone, each with its remedy (§7.5).
func runFatalFor(out string, kind export.FatalKind, err error) *RunFatalError {
	where := writtenPath(err)
	// The words carry paths that include the data file's own folder names: cleaned
	// where they are built (S2A-AO5).
	switch kind {
	case export.FatalToken:
		return &RunFatalError{Msg: cleanText(err.Error()), Err: err}
	case export.FatalFull:
		return &RunFatalError{Err: err, Msg: cleanText(fmt.Sprintf("the archive drive is full (writing %s): free space on the drive that holds %s, or choose another archive location, then re-run. %s", where, out, keptNote))}
	case export.FatalReadOnly:
		return &RunFatalError{Err: err, Msg: cleanText(fmt.Sprintf("the archive location %s is read-only (writing %s): make it writable or choose another location, then re-run.", out, where))}
	}
	return unavailable(out, err)
}

// unavailable is the refusal for an archive location that is gone (a removed
// drive, a dropped share, a root deleted under the run).
func unavailable(out string, cause error) *RunFatalError {
	return &RunFatalError{Err: cause, Msg: cleanText(fmt.Sprintf("the archive location %s is unavailable (%v) — reconnect the drive or share and re-run. %s", out, cause, keptNote))}
}

// writtenPath is the file or directory an export error names ("" if none).
func writtenPath(err error) string {
	var fe *export.FolderError
	if errors.As(err, &fe) {
		return fe.Dir
	}
	var ie *export.ItemError
	if errors.As(err, &ie) {
		return ie.Path
	}
	var pe *fs.PathError // a -copy-first snapshot's write
	if errors.As(err, &pe) {
		return pe.Path
	}
	return ""
}

// localRun is what one Run keeps across its local sources.
type localRun struct {
	exp    *export.Exporter
	out    string
	runAt  time.Time
	logger *log.Logger
	lock   *lockfile.Lock // the archive lock the probe asks "still held?"; nil in a test

	outlookFresh map[string]bool // source ids (state.PathSourceID) of the PSTs this run's Outlook export wrote

	faults      int // faults recorded: they count as processed for the checkpoint gate (FC12)
	writeFaults int // folder and item write faults, which pace the probe
	recovered   int // gaps closed because their item was captured
	closedGone  int // gaps closed because a completed walk no longer found them

	// onFault runs after every recorded fault (Run's checkpoint, which re-checks the
	// lock too, so a stretch of nothing but faults still does).
	onFault func()
	// onLockLost runs when the probe finds the archive lock removed or replaced; Run
	// then ends without writing shared state.
	onLockLost func(error)
}

func (lr *localRun) noteFault() {
	lr.faults++
	if lr.onFault != nil {
		lr.onFault()
	}
}

// probe checks the archive is still there before a write fault is recorded as
// that item's or folder's own: the first fault of the run and every 50th after. A
// removed USB drive or a dropped share is then run-fatal, never a folder of
// fillable gaps (FC12).
func (lr *localRun) probe(s *sourceRun) error {
	n := lr.writeFaults
	lr.writeFaults++
	if n%probeEvery != 0 {
		return nil
	}
	if _, err := os.Lstat(lr.out); err != nil {
		return unavailable(lr.out, err)
	}
	// The store's directory is expected only where this store has written before: a
	// first write that failed to create it says nothing about the archive.
	if s.wrote || lr.exp.Manifest.HasToken(s.token) {
		if _, err := os.Lstat(filepath.Join(lr.out, s.token)); err != nil {
			return unavailable(lr.out, err)
		}
	}
	if lr.lock != nil {
		if err := lr.lock.StillHeld(); err != nil {
			// Another run may own the archive now: the run writes nothing more into it, not
			// even its manifest, exactly as when the checkpoint finds it (INT-CC-2, S2A-AC6).
			if lr.onLockLost != nil {
				lr.onLockLost(err)
			}
			return unavailable(lr.out, err)
		}
	}
	return nil
}

// sourceRun is one source's walk.
type sourceRun struct {
	lr    *localRun
	token string
	path  string

	reportsFaults bool            // the reader reports what it could not read (source.FaultWalker)
	local         bool            // the reader is an mbox, maildir or Evolution reader (its faults are the local readers' reasons; the PST reader's are the PST's)
	wrote         bool            // this store has written a message this run: its directory exists
	unwritable    map[string]bool // output directories that could not be created, this run
	streak        int             // consecutive failed write attempts (the local breaker)
	folderFaults  []string        // folders a folder-scope fault was recorded in this run, once each, in the ledger's spelling
	folderSeen    map[string]bool // the same, for the "once each"
	folderFlood   bool            // more folders faulted than folderFaultCap: the run cannot say which it read, so it closes nothing
	seenFolders   map[string]bool // the folders this walk reached (whole paths and last segments), for what -outlook says it left out
	freshOutlook  bool            // the file is a PST this run's Outlook export wrote: a fresh image of the mailbox (NF1)
}

// noteFolder records a folder the reader reached. Each path is kept as its joined
// spelling and as its last segment, which is how -outlook names a skipped folder.
func (s *sourceRun) noteFolder(path []string) {
	if len(path) == 0 {
		return
	}
	if s.seenFolders == nil {
		s.seenFolders = map[string]bool{}
	}
	s.seenFolders[state.GapFolder(strings.Join(path, "/"))] = true
	s.seenFolders[state.GapFolder(path[len(path)-1])] = true
}

// folderFaultCap bounds the list of folders that faulted. A hostile source whose
// faults name a different folder each time (a hierarchy of 100,000 loop rows under
// distinct names) would otherwise grow it a string per fault, and the closure guard
// scans it for every older gap (S2A-AO3).
const folderFaultCap = 2000

// observe records one gap of this source in the ledger and counts it as processed.
func (s *sourceRun) observe(g state.CaptureGap) {
	g.Token = s.token
	if g.Scope == state.GapScopeItem && strings.HasPrefix(g.Locator, "ident:") {
		// Another folder's copy of the same message already lost its write in this run
		// and holds the plain locator: this copy's loss is its own gap.
		key := state.GapKey(s.token, state.GapScopeItem, g.Locator)
		if f, seen := s.lr.exp.Manifest.GapObservedIn(key, s.lr.runAt); seen && f != state.GapFolder(g.Folder) {
			g.Locator += identFolderSep + util.ShortHash(g.Folder)
		}
	}
	s.lr.exp.Manifest.ObserveGap(g, s.lr.runAt)
	if g.Scope == state.GapScopeFolder {
		// The ledger stores a folder stripped of control and bidi characters and cut to a
		// bound; the guard compares in that spelling, or it never matches (S2A-AI7).
		p := state.GapFolder(g.Folder)
		switch {
		case s.folderSeen[p]:
		case len(s.folderFaults) >= folderFaultCap:
			s.folderFlood = true
		default:
			if s.folderSeen == nil {
				s.folderSeen = map[string]bool{}
			}
			s.folderSeen[p] = true
			s.folderFaults = append(s.folderFaults, p)
		}
	}
	s.lr.noteFault()
}

// onReaderFault records a fault the reader delivered during the walk (FC29).
func (s *sourceRun) onReaderFault(f source.Fault) {
	s.observe(state.CaptureGap{
		Folder: f.FolderPath(), Scope: f.Scope, Locator: f.Locator, Reason: f.Reason,
		Class: f.Class, Subject: f.Subject, Detail: f.Detail, Skipped: f.Skipped,
	})
}

// itemLocator is the ledger locator of a message the export step failed on: its
// own Locator when the reader gave one, else a post-parse ident: locator (FC8).
func itemLocator(m *model.Message) string {
	if m.Locator != "" {
		return m.Locator
	}
	return "ident:" + m.Identity()
}

func itemGap(m *model.Message, folderKey, reason, class, detail string) state.CaptureGap {
	g := state.CaptureGap{Folder: folderKey, Scope: state.GapScopeItem, Locator: itemLocator(m), Reason: reason, Class: class, Subject: m.Subject, Detail: detail}
	if d := m.Date(); !d.IsZero() {
		g.Received = d.UTC().Format(time.RFC3339)
	}
	return g
}

// recovered closes the gaps this message's capture answers: one at its Locator
// and one at its ident: (an export that failed before). Each is counted
// (`Recovered`), and nothing is written or deleted — the entry just goes (§4.3).
//
// A message's own Locator names one item, so its gap closes whoever writes it. The
// ident: locator is token-wide: the same message in folders A and B has ONE such
// locator and two copies, so a copy closes only the gap recorded for its own folder
// (the other copy is still missing), and a second copy that failed in the same run
// has a gap of its own at ident:<identity>#<hash of its folder> (S2A-AI1).
func (s *sourceRun) recovered(m *model.Message, folderKey string) {
	mf := s.lr.exp.Manifest
	if m.Locator != "" && mf.CloseGap(state.GapKey(s.token, state.GapScopeItem, m.Locator)) {
		s.lr.recovered++
	}
	// A maildir file that could not be read on an earlier run is recorded at the place
	// the reader found it: its capture answers that gap too.
	if s.closeReadGap(m) {
		s.lr.recovered++
	}
	ident := "ident:" + m.Identity()
	if ident == "ident:" {
		return
	}
	for _, loc := range []string{ident, ident + identFolderSep + util.ShortHash(folderKey)} {
		if mf.CloseGapInFolder(state.GapKey(s.token, state.GapScopeItem, loc), folderKey) {
			s.lr.recovered++
		}
	}
}

// closeReadGap closes the gap recorded at a local reader's own place for the message
// (a maildir file's folder and unique part), and says whether it closed one.
//
// That locator names a message, not a file: new/abc and cur/abc:2,S, a client's rename,
// share it. So a gap THIS run observed is answered only by a capture when the gap says
// the file vanished (the walk then found it under its new name); a file the run could
// not read (file-unreadable) is not answered by the capture of another file of the same
// unique part, which is another message (S2B-AO2). A gap from an earlier run is closed
// by any capture of the place, as before.
func (s *sourceRun) closeReadGap(m *model.Message) bool {
	if m.ReadLocator == "" || m.ReadLocator == m.Locator {
		return false
	}
	mf := s.lr.exp.Manifest
	key := state.GapKey(s.token, state.GapScopeItem, m.ReadLocator)
	g, ok := mf.GapFor(key)
	if !ok {
		return false
	}
	if g.LastRun.Equal(s.lr.runAt) && g.Reason != "vanished-during-read" {
		return false
	}
	return mf.CloseGapReason(key, g.Reason)
}

// identFolderSep joins an ident: locator to the hash of the folder it was lost in,
// when another folder's copy of the same message already owns the plain locator.
const identFolderSep = "#"

// handle exports one message and sorts what goes wrong by whose fault it is.
func (s *sourceRun) handle(ctx context.Context, abort func() error, folderPath []string, m *model.Message) (err error) {
	if err := s.stop(ctx, abort); err != nil {
		return err
	}
	lr := s.lr
	folderKey := strings.Join(folderPath, "/")
	dir := filepath.Join(append([]string{lr.out, s.token}, folderPath...)...)

	// A folder the archive could not create this run: its later items are counted
	// into the gap, not attempted — except one the archive already holds, which an
	// Incremental run's exporter skips without writing (FC12). A Full run re-writes
	// every message, so there it spares nothing (S2A-AC5).
	if s.unwritable[dir] && (lr.exp.Mode != export.Incremental || !lr.exp.Manifest.Has(state.Key(s.token, folderKey, m.Identity()))) {
		s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: "folder:" + folderKey, Reason: "folder-unwritable", Class: state.ClassFillable, Skipped: 1})
		return nil
	}

	// A panic exporting one crafted message must not abort the run (R10): it is a
	// terminal export-panic gap, counted, never only a log line (F33).
	defer func() {
		if r := recover(); r != nil {
			// The folder name and the panic's words can both be the data file's own bytes.
			lr.logger.Printf("warning: recovered from panic on a message in %s (skipped): %s", cleanText(folderKey), cleanText(fmt.Sprint(r)))
			lr.exp.Stats.ExportPanics++
			s.observe(itemGap(m, folderKey, "export-panic", state.ClassTerminal, fmt.Sprint(r)))
			err = nil
		}
	}()
	written, exportErr := lr.exp.Export(s.token, folderPath, m)
	if exportErr == nil {
		if written {
			s.wrote, s.streak = true, 0
			s.recovered(m, folderKey)
		}
		return nil
	}

	tokenDir := filepath.Join(lr.out, s.token)
	if kind := export.FatalKindAt(exportErr, lr.out, tokenDir); kind != export.FatalNone {
		return runFatalFor(lr.out, kind, exportErr)
	}
	var fe *export.FolderError
	var ie *export.ItemError
	switch {
	case errors.As(exportErr, &fe):
		if err := lr.probe(s); err != nil {
			return err
		}
		if s.unwritable == nil {
			s.unwritable = map[string]bool{}
		}
		s.unwritable[fe.Dir] = true
		lr.exp.Stats.ExportFaults++
		s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: "folder:" + folderKey, Reason: "folder-unwritable",
			Class: state.ClassFillable, Detail: exportErr.Error(), Skipped: 1})
	case errors.As(exportErr, &ie):
		if err := lr.probe(s); err != nil {
			return err
		}
		lr.exp.Stats.ExportFaults++
		s.observe(itemGap(m, folderKey, "export-failed", state.ClassFillable, exportErr.Error()))
	default:
		return exportErr
	}
	s.streak++
	if s.streak >= writeBreaker {
		return &SourceStoppedError{Err: exportErr, Msg: cleanText(fmt.Sprintf("stopped reading %s after %d writes in a row failed (last: %v) — check that the archive drive is connected and writable, then re-run.", s.path, writeBreaker, exportErr))}
	}
	return nil
}

// stop is the abort check made before every item: a cancelled context or a lost
// lock ends the walk (R5, INT-CC-2).
func (s *sourceRun) stop(ctx context.Context, abort func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if abort != nil {
		return abort()
	}
	return nil
}

// exportStageGap says whether a gap is one the run itself records while exporting:
// the reasons every local walk can see again, whatever its reader.
func exportStageGap(reason string) bool {
	switch reason {
	case "export-failed", "export-panic", "folder-unwritable", "refresh-failed":
		return true
	}
	return false
}

// pstReaderGap says whether a walk of a PST can see again the gap a reason names:
// the faults the PST reader produces, the overflow entry that counts the ones beyond
// the ledger's caps, and what -outlook left out of the export (whose closure is
// decided by covered, since the file must now hold the folder).
func pstReaderGap(reason string) bool {
	switch reason {
	case "pst-message-unreadable", "pst-messages-unreadable", "pst-row-unreadable", "pst-row-invalid",
		"folder-unreadable", "folder-partially-read", "folder-cycle", "folder-too-deep",
		"overflow", "outlook-export-skipped", "outlook-account-skipped":
		return true
	}
	return false
}

// localReaderGap says whether a walk of an mbox, maildir or Evolution store can see
// again the gap a reason and locator name: the faults the local readers produce, and
// the overflow entry that counts the ones beyond the ledger's caps. A maildir file's
// gap is seen again only at the locator the reader gives it (maildir:<folder>/<unique>):
// a gap at another spelling is not one this reader would meet, so its walk says
// nothing about it (S2A-AI4's rule: a walk closes only what it could have seen).
func localReaderGap(reason, locator string) bool {
	switch reason {
	case "file-unreadable", "vanished-during-read":
		return strings.HasPrefix(locator, "maildir:")
	case "folder-unreadable", "folder-partially-read", "symlink-outside-store", "symlinked-folder-skipped",
		"not-a-message-file", "folder-cycle", "folder-too-deep", "overflow":
		return true
	}
	return false
}

// noStoreUID says whether the file's identity is not stable across runs, so its
// zero-content failures are counted per folder instead of by NID (FC8): the .pst
// that -outlook's Outlook export rebuilds every run, and an .ost Outlook rebuilds
// in place.
func noStoreUID(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".ost") {
		return true
	}
	return state.SourceKind(state.PathSourceID(path)) == state.SourceOutlookScratch
}

// finish sorts the walk's outcome. The faults a reader without a callback returns
// are recorded one by one, and neither they nor the faults delivered during the
// walk fail the source (FC29). Only a walk that completed closes gaps: those of
// this source that this run did not observe and whose folder it read fully, as "no
// longer in the source" (§4.3 step 3); a walk aborted by a run-level fault closes
// nothing.
func (s *sourceRun) finish(walkErr error) error {
	var fe *source.FaultsError
	if errors.As(walkErr, &fe) {
		for _, f := range fe.Faults {
			s.onReaderFault(f)
		}
		if fe.Dropped > 0 {
			s.lr.logger.Printf("warning: %s: %d more fault(s) were not listed", s.path, fe.Dropped)
		}
		walkErr = fe.Err
	}
	if walkErr != nil {
		return walkErr
	}
	s.lr.closedGone += s.lr.exp.Manifest.CloseUnobserved(s.token, s.covered, s.lr.runAt, s.lr.exp.Since)
	return nil
}

// covered says whether this run covered the gap: it read the gap's folder fully (no
// folder-scope fault was recorded this run in that folder or any ancestor of it), and
// the walk could have SEEN what the gap records. Every reader reports its faults (the
// PST reader and, since FI-S2b, the mbox, maildir and Evolution readers), and each
// sees the reasons it produces (pstReaderGap, localReaderGap); the export-stage
// reasons, which the run itself observes for any source, are closed by a completed
// walk that no longer meets them. A reason or a locator the reader would not produce
// is one its walk says nothing about, and is kept.
func (s *sourceRun) covered(g state.CaptureGap) bool {
	if !s.reportsFaults && !exportStageGap(g.Reason) {
		return false
	}
	if s.folderFlood {
		return false
	}
	// A reader that reports its faults sees the reasons it produces and the run's own;
	// a reason it cannot see (a newer build's, another source's) is kept (S2A-AI4).
	if s.reportsFaults && !exportStageGap(g.Reason) {
		if s.local && !localReaderGap(g.Reason, g.Locator) || !s.local && !pstReaderGap(g.Reason) {
			return false
		}
	}
	// What Outlook left out of the export is not in the file: the file answers it only
	// by now holding the folder. The exception is a file this run's own export wrote: it
	// is Outlook's image today, so a folder it neither holds nor skipped again (the gap
	// was not observed this run, or it would not be here) is no longer in Outlook, or
	// is one Outlook could not even name (NF1).
	if g.Reason == "outlook-export-skipped" && g.Folder != "" && !s.freshOutlook && !s.seenFolders[g.Folder] {
		return false
	}
	for _, p := range s.folderFaults {
		if p == "" || g.Folder == p || strings.HasPrefix(g.Folder, p+"/") {
			return false
		}
	}
	return true
}
