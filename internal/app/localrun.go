package app

import (
	"context"
	"errors"
	"fmt"
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

const keptNote = "Messages written before this point are kept; nothing was recorded as archived that was not fully written."

// runFatalFor words err (which export.IsRunFatal or the probe judged fatal) for the
// operator: a full drive, a read-only location, an unsafe token, or one that is
// gone, each with its remedy (§7.5).
func runFatalFor(out string, kind export.FatalKind, err error) *RunFatalError {
	where := writtenPath(err)
	switch kind {
	case export.FatalToken:
		return &RunFatalError{Msg: err.Error(), Err: err}
	case export.FatalFull:
		return &RunFatalError{Err: err, Msg: fmt.Sprintf("the archive drive is full (writing %s): free space on the drive that holds %s, or choose another archive location, then re-run. %s", where, out, keptNote)}
	case export.FatalReadOnly:
		return &RunFatalError{Err: err, Msg: fmt.Sprintf("the archive location %s is read-only (writing %s): make it writable or choose another location, then re-run.", out, where)}
	}
	return unavailable(out, err)
}

// unavailable is the refusal for an archive location that is gone (a removed
// drive, a dropped share, a root deleted under the run).
func unavailable(out string, cause error) *RunFatalError {
	return &RunFatalError{Err: cause, Msg: fmt.Sprintf("the archive location %s is unavailable (%v) — reconnect the drive or share and re-run. %s", out, cause, keptNote)}
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
	return ""
}

// localRun is what one Run keeps across its local sources.
type localRun struct {
	exp    *export.Exporter
	out    string
	runAt  time.Time
	logger *log.Logger
	lock   *lockfile.Lock // the archive lock the probe asks "still held?"; nil in a test

	faults      int // faults recorded: they count as processed for the checkpoint gate (FC12)
	writeFaults int // folder and item write faults, which pace the probe
	recovered   int // gaps closed because their item was captured
	closedGone  int // gaps closed because a completed walk no longer found them

	// onFault runs after every recorded fault (Run's checkpoint, which re-checks the
	// lock too, so a stretch of nothing but faults still does).
	onFault func()
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
	wrote         bool            // this store has written a message this run: its directory exists
	unwritable    map[string]bool // output directories that could not be created, this run
	streak        int             // consecutive failed write attempts (the local breaker)
	folderFaults  []string        // folders a folder-scope fault was recorded in, this run
}

// observe records one gap of this source in the ledger and counts it as processed.
func (s *sourceRun) observe(g state.CaptureGap) {
	g.Token = s.token
	s.lr.exp.Manifest.ObserveGap(g, s.lr.runAt)
	if g.Scope == state.GapScopeFolder {
		s.folderFaults = append(s.folderFaults, g.Folder)
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
func (s *sourceRun) recovered(m *model.Message) {
	for _, loc := range []string{m.Locator, "ident:" + m.Identity()} {
		if loc == "ident:" || loc == "" {
			continue
		}
		if s.lr.exp.Manifest.CloseGap(state.GapKey(s.token, state.GapScopeItem, loc)) {
			s.lr.recovered++
		}
	}
}

// handle exports one message and sorts what goes wrong by whose fault it is.
func (s *sourceRun) handle(ctx context.Context, abort func() error, folderPath []string, m *model.Message) (err error) {
	if err := s.stop(ctx, abort); err != nil {
		return err
	}
	lr := s.lr
	folderKey := strings.Join(folderPath, "/")
	dir := filepath.Join(append([]string{lr.out, s.token}, folderPath...)...)

	// A folder the archive could not create this run: its later items are counted
	// into the gap, not attempted — except one the archive already holds, which the
	// exporter skips without writing (FC12).
	if s.unwritable[dir] && !lr.exp.Manifest.Has(state.Key(s.token, folderKey, m.Identity())) {
		s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: "folder:" + folderKey, Reason: "folder-unwritable", Class: state.ClassFillable, Skipped: 1})
		return nil
	}

	// A panic exporting one crafted message must not abort the run (R10): it is a
	// terminal export-panic gap, counted, never only a log line (F33).
	defer func() {
		if r := recover(); r != nil {
			lr.logger.Printf("warning: recovered from panic on a message in %s (skipped): %v", folderKey, r)
			lr.exp.Stats.ExportPanics++
			s.observe(itemGap(m, folderKey, "export-panic", state.ClassTerminal, fmt.Sprint(r)))
			err = nil
		}
	}()
	written, exportErr := lr.exp.Export(s.token, folderPath, m)
	if exportErr == nil {
		if written {
			s.wrote, s.streak = true, 0
			s.recovered(m)
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
		return &RunFatalError{Err: exportErr, Msg: fmt.Sprintf("stopped reading %s after %d writes in a row failed (last: %v) — check that the archive drive is connected and writable, then re-run.", s.path, writeBreaker, exportErr)}
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
// the walk could have SEEN what the gap records. A reader that reports its faults
// (the PST reader) sees every reader-stage reason; the others (mbox, maildir,
// Evolution, until FI-S2b) report none, so their walk says nothing about a
// reader-stage gap — only the export-stage reasons, which the run itself observes
// for any source, are closed by a walk that no longer meets them.
func (s *sourceRun) covered(g state.CaptureGap) bool {
	if !s.reportsFaults && !exportStageGap(g.Reason) {
		return false
	}
	for _, p := range s.folderFaults {
		if p == "" || g.Folder == p || strings.HasPrefix(g.Folder, p+"/") {
			return false
		}
	}
	return true
}
