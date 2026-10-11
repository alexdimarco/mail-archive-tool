package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A run (design §4.7 Start, §4.13, §5.1): the checks Start repeats, the export root
// created through the destination's handle, the run record, the journal's start and
// the marker, then one message at a time, then finalization and the seal-and-finish
// step. The write order is W1..W12 of §5.1; each step's name is a fault hook a test
// can kill the run at.

// copyFaults are a test's fault hooks. step is called after the named write
// completes; a run killed at a step is left exactly as that point leaves it.
type copyFaults struct {
	killAt string
	hook   func(step string)
	// appendErr, when it returns an error for a state file's path, fails the append
	// to it (a full or unwritable state volume).
	appendErr func(path string) error
}

// ceKilled is the panic a test's kill raises, recovered by Run with no finalization.
type ceKilled struct{ step string }

// step marks a point of the write order for the fault hooks.
func (e *CopyExportEnv) step(name string) {
	if f := e.faults; f != nil {
		if f.hook != nil {
			f.hook(name)
		}
		if f.killAt == name {
			panic(ceKilled{name})
		}
	}
}

// CopyProgress is a snapshot of a running export, served from memory only (XC4).
type CopyProgress struct {
	Selected int
	Done     int // messages finished, exported or listed
	Failed   int
	Bytes    int64
	Folder   string
	Started  time.Time
	ETA      time.Duration
}

// CopyRun is one export being run.
type CopyRun struct {
	env  *CopyExportEnv
	plan *CopyPlan
	src  CopySource

	// ConfirmPosture is asked, on a share whose preview said its prediction may
	// differ, whether the realized posture is acceptable; nil declines a broader one.
	ConfirmPosture func(CopyPosture) bool

	mu          sync.Mutex
	cancel      context.CancelFunc
	cancelled   bool
	pendingStop *CopyStop // a stop asked for before the run's watch existed

	started time.Time
	done    atomic.Int64
	failed  atomic.Int64
	bytes   atomic.Int64
	folder  atomic.Value // string

	runID    string
	chainID  string
	rootName string
	rootAbs  string
	fs       ceFS
	rec      *ceRecord
	namer    *ceNamer
	made     map[string]bool
	dirs     []string // directories created, for the batched sync before SHA256SUMS.txt
	watch    *ceWatch
	hdr      ceHeaderLine

	consecWrite  int
	consecServer int
	lastWriteErr string
	postureStop  *CopyStop // R-w2, found by the realized check before the first message
}

// NewCopyRun prepares a run of plan against src.
func (e *CopyExportEnv) NewCopyRun(plan *CopyPlan, src CopySource) *CopyRun {
	return &CopyRun{env: e, plan: plan, src: src}
}

// Cancel stops the run after the message in flight (its download aborted, its temp
// file removed). It is safe to call at any time, also before Run.
func (r *CopyRun) Cancel() {
	r.mu.Lock()
	r.cancelled = true
	c := r.cancel
	r.mu.Unlock()
	if c != nil {
		c()
	}
}

// StopForSignInChange stops the run run-fatal with R-z after aborting the message in
// flight (its download cancelled through that message's own context, never the walk's, so
// it is never read as the user's Cancel): the Microsoft 365 sign-in was cleared, or
// changed to now, in the dashboard (XC7). The run finalizes (manifest `failed`, marker
// STOPPED) and is retryable from its record. now is "" when the sign-in was cleared.
func (r *CopyRun) StopForSignInChange(now string) {
	stop := &CopyStop{RID: "R-z", Class: stopClassSignInMoved, Message: now}
	r.mu.Lock()
	w := r.watch
	if w == nil {
		r.pendingStop = stop
	}
	r.mu.Unlock()
	if w != nil {
		w.fatalStop(stop)
	}
}

// Progress is the run's counters, ETA included.
func (r *CopyRun) Progress() CopyProgress {
	r.mu.Lock()
	started := r.started // Run sets it while the UI polls
	r.mu.Unlock()
	p := CopyProgress{Selected: len(r.plan.Messages), Done: int(r.done.Load()), Failed: int(r.failed.Load()),
		Bytes: r.bytes.Load(), Started: started}
	if f, _ := r.folder.Load().(string); f != "" {
		p.Folder = f
	}
	if p.Done > 0 && !started.IsZero() {
		per := r.env.nowTime().Sub(started) / time.Duration(p.Done)
		p.ETA = per * time.Duration(p.Selected-p.Done)
	}
	return p
}

// RunID is the run's id once Run has started it.
func (r *CopyRun) RunID() string { return r.runID }

// Run does Start's checks and runs the export to its end. A *CopyRefusal means
// nothing was exported; otherwise the result says how the run ended. It blocks.
func (r *CopyRun) Run(parent context.Context) (res *CopyResult, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = r.recovered(p)
		}
	}()
	e := r.env
	walk, cancel := context.WithCancel(parent)
	defer cancel()
	r.mu.Lock()
	r.cancel = cancel
	was := r.cancelled
	r.started = e.nowTime()
	r.mu.Unlock()
	if was {
		cancel()
	}

	if refusal := r.start(); refusal != nil {
		return nil, refusal
	}
	// From here the root exists and the run record names it.
	release := holdAwake()
	defer release()
	wctx, wcancel := context.WithCancel(context.WithoutCancel(walk))
	w := &ceWatch{env: e, src: r.src}
	r.mu.Lock()
	r.watch = w
	pending := r.pendingStop
	r.mu.Unlock()
	if pending != nil {
		w.fatalStop(pending) // a stop asked for before the run reached its first message
	}
	go w.loop(wctx)
	defer wcancel()

	status, stop := r.loop(walk)
	wcancel()
	return r.finalize(status, stop)
}

// recovered turns a panic into the run's end: a test's kill leaves everything as it
// is; any other panic is the DC3 recover, which records the stop and leaves the marker.
func (r *CopyRun) recovered(p any) (*CopyResult, error) {
	if k, ok := p.(ceKilled); ok {
		if r.fs != nil {
			r.fs.Close()
		}
		if r.rec != nil {
			r.rec.Close()
		}
		return &CopyResult{RunID: r.runID, Root: r.rootAbs, RootName: r.rootName, Killed: k.step}, nil
	}
	stop := &CopyStop{RID: "internal", Class: stopClassInternal}
	if r.runID != "" && r.rec != nil {
		stop.Message = refusalText("internal", int(r.done.Load()), len(r.plan.Messages), "", r.plan, r.env.StateDir)
		_ = r.rec.append(ceStoppedLine{T: "stopped", RID: stop.RID, Class: stop.Class, Reason: stopReason("internal"), At: r.env.nowTime()})
		_ = r.env.journalAppend(ceJournalLine{Run: r.runID, Event: "stopped", Reason: "internal, " + stopClassInternal})
	}
	return &CopyResult{RunID: r.runID, Root: r.rootAbs, RootName: r.rootName, Status: CopyStatusFailed,
		Outcome: CopyOutcomeFailed, Stop: stop, Label: "Stopped: " + stopReason("internal")}, nil
}

// start is Start: the checks, the root, the record, the journal's start and the
// marker. It returns a refusal when nothing was exported.
func (r *CopyRun) start() *CopyRefusal {
	e, p := r.env, r.plan
	if !p.Expires.IsZero() && e.nowTime().After(p.Expires) {
		return refuse("R-k", "This preview is out of date (older than 30 minutes, or the selection or destination changed) — click Preview copy export again.")
	}
	// The state directory must take the run record before anything is written.
	if err := os.MkdirAll(e.StateDir, 0o700); err != nil {
		return refusalD(e.StateDir)
	}
	if sfree, err := e.freeOnState(); err == nil && int64(sfree) < p.RecordBound {
		return refusalD(e.StateDir)
	}
	free, err := e.freeOnDest(p.Dest)
	if err != nil {
		return destMissing(p.Dest)
	}
	if need := p.EstimateBytes + p.Reserve; int64(free) < need {
		return refusalG2(p.Dest, free, need)
	}
	if now := e.destinationPosture(p.Dest, p.FSType); now.broaderThan(p.Posture) {
		return refuse("R-w2", refusalText("R-w2", 0, 0, postureNames(now), p, ""))
	}
	// Open the destination once as a directory handle, check that it is the folder
	// the preview saw, and create the root through that handle: no path is walked
	// again between the check and the mkdir (XC32, RR2-C13).
	droot, err := os.OpenRoot(p.Dest)
	if err != nil {
		return destMissing(p.Dest)
	}
	defer droot.Close()
	if df, err := droot.Open("."); err != nil {
		return destMissing(p.Dest)
	} else {
		cur, _ := readIdentity(df)
		df.Close()
		if !sameDir(cur, p.DestIdentity) {
			return refuse("R-k", "This preview is out of date (older than 30 minutes, or the selection or destination changed) — click Preview copy export again.")
		}
	}
	// The destination is still outside the archives, by path and by identity: the
	// walk the preview did is not repeated, the destination is read again (MA-444).
	if err := p.Outside.Recheck(e, p.Dest); err != nil {
		if ref, ok := err.(*CopyRefusal); ok {
			return ref
		}
		return destMissing(p.Dest)
	}
	name := p.ExportName
	made := false
	for i := 1; i <= 99; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", name, i)
		}
		merr := droot.Mkdir(cand, 0o700)
		if merr == nil {
			r.rootName, made = cand, true
			break
		}
		if !errors.Is(merr, fs.ErrExist) {
			return refusalW(p.Dest, leaveName(merr))
		}
	}
	if !made {
		return refusalT(p.Dest, name)
	}
	r.rootAbs = filepath.Join(p.Dest, r.rootName)
	sub, err := droot.OpenRoot(r.rootName)
	if err != nil {
		return refusalW(p.Dest, leaveName(err))
	}
	r.fs = e.wrapFS(&rootFS{root: sub, abs: r.rootAbs, randHex: e.rand, syncFile: e.syncFile})

	id, ierr := r.fs.Identity()
	if ierr != nil {
		return refusalW(p.Dest, leaveName(ierr))
	}
	r.runID = e.rand(16)
	r.chainID = p.Chain.ID
	if r.chainID == "" {
		r.chainID = r.runID
	}
	e.step("W2")

	r.hdr = ceHeaderLine{T: "header", Run: r.runID, Chain: r.chainID, ChainParts: p.Chain.Parts, RetryOf: p.Chain.RetryOf,
		Host: e.Host, Tool: e.Version, Started: r.started, Source: p.Source, Dest: p.Dest, Root: r.rootAbs,
		RootName: r.rootName, Identity: id, FSType: id.FSType, Options: p.Options, Folders: p.Folders, Planned: len(p.Messages)}
	rec, rerr := e.createRecord(r.hdr, p.Messages)
	if rerr != nil {
		return r.refusedStart(nil)
	}
	r.rec = rec
	if jerr := e.journalAppend(ceJournalLine{Run: r.runID, Event: "start", SourceKind: p.Source.Kind, Mailbox: p.Source.Mailbox,
		DestDir: r.rootAbs, RootID: &id, Planned: len(p.Messages)}); jerr != nil {
		return r.refusedStart(rec)
	}
	e.step("W3")

	r.namer = newCeNamer(p.Options.PreserveFolders, filepath.ToSlash(r.rootAbs))
	r.made = map[string]bool{}
	if err := r.writeMarker(false); err != nil {
		// The record names the root and the marker is missing: it is reconciled as
		// interrupted and finished by "Finish as partial". Nothing was exported.
		return r.refusedStart(rec)
	}
	e.step("W4")
	return r.checkRealized()
}

// refusedStart is R-d: the record or the journal could not be written, so nothing
// was exported. A refused line goes into the fingerprint ledger and then the record
// is removed, so the launch sweep never adopts a refused start; if the ledger
// append fails too, the record is kept and adopted as an interrupted empty export
// folder (design §4.16). The empty root is left in place.
func (r *CopyRun) refusedStart(rec *ceRecord) *CopyRefusal {
	e := r.env
	if rec != nil {
		rec.Close()
		if err := e.appendJSONL(e.ledgerPath(), ceLedgerLine{V: 1, Run: r.runID, Chain: r.chainID, Kind: "refused", At: e.nowTime()}); err == nil {
			os.Remove(rec.path)
		}
	}
	if r.fs != nil {
		r.fs.Close()
	}
	ref := refusalD(e.StateDir)
	ref.Root = r.rootAbs
	return ref
}

// sameDir compares two directories' identity, by dev and inode or by volume and file id.
func sameDir(a, b ceIdentity) bool {
	if a.isWindowsID() || b.isWindowsID() {
		return a.Vol == b.Vol && a.FileID == b.FileID
	}
	return a.Dev == b.Dev && a.Ino == b.Ino
}

// checkRealized reads the realized posture of the root and the marker (macOS: their
// inherited ACL entries) and appends it to the run record. If it grants read to
// anyone the line did not name, the run stops before W5 with R-w2 (RR2-C6); on a share
// whose preview said its prediction may differ, the realized line is confirmed
// instead. The refusal is returned as a stop through r.postureStop.
func (r *CopyRun) checkRealized() *CopyRefusal {
	e, p := r.env, r.plan
	rp := e.realizedPosture(r.rootAbs, true, r.hdr.FSType)
	if mp := e.realizedPosture(filepath.Join(r.rootAbs, copyMarkerName), false, r.hdr.FSType); mp.broaderThan(rp) {
		rp = mp
	}
	_ = r.rec.append(cePostureLine{T: "posture", Kind: rp.Kind, Readers: rp.Readers, Line: rp.Line})
	if rp.broaderThan(p.Posture) {
		if p.Posture.Predicted && r.ConfirmPosture != nil && r.ConfirmPosture(rp) {
			return nil
		}
		r.postureStop = &CopyStop{RID: "R-w2", Class: stopClassPosture, Message: refusalText("R-w2", 0, len(p.Messages), postureNames(rp), p, "")}
	}
	return nil
}

// markerText is the marker's content: the run id on the first line (the content
// nonce, §4.9), then what it means. A cancelled or stopped run's marker is rewritten
// with a status line and counts (§4.13 step 8).
func (r *CopyRun) markerText(status string, counts *CopyCounts, reason string) string {
	var b strings.Builder
	b.WriteString(copyRunLinePrefix + r.runID + "\n")
	if status == "" {
		b.WriteString("NOT FINISHED: MailArchive Desktop is still writing this export, or stopped before it finished.\n")
	} else {
		fmt.Fprintf(&b, "%s at %s: %d of %d exported", status, r.env.nowTime().UTC().Format(time.RFC3339), counts.Exported, counts.Selected)
		if reason != "" {
			fmt.Fprintf(&b, ", reason: %s", reason)
		}
		b.WriteString("\n")
	}
	b.WriteString("Do not hand this folder over as complete: its README.txt, manifest and checksums are valid only when this file is absent.\n")
	b.WriteString("This file is removed when the export is complete. To finish or retry it, use Export activity in MailArchive Desktop; the folder can also be deleted.\n")
	return b.String()
}

// writeMarker writes the marker first (W4): a temp file, fsync, a no-replace rename,
// then a directory sync. With replace it is the run's own marker rewritten by one
// atomic replacing rename, never removed first (P6's exception).
func (r *CopyRun) writeMarker(replace bool) error {
	if err := writeTempFile(r.fs, "", copyMarkerName, []byte(r.markerText("", nil, "")), replace); err != nil {
		return err
	}
	return r.fs.SyncDir("")
}

// ceHashWriter is the writer a download streams into: it hashes and counts what it
// writes, and counts every body byte as progress for the breaker.
type ceHashWriter struct {
	w     ceFile
	h     hash.Hash
	n     int64
	watch *ceWatch
	total *atomic.Int64
}

func (hw *ceHashWriter) Write(p []byte) (int, error) {
	n, err := hw.w.Write(p)
	if n > 0 {
		hw.h.Write(p[:n])
		hw.n += int64(n)
		hw.total.Add(int64(n))
		hw.watch.bump()
	}
	if err != nil {
		return n, &ceWriteError{err}
	}
	return n, nil
}

// loop runs the messages and returns how the run ended.
func (r *CopyRun) loop(walk context.Context) (CopyStatus, *CopyStop) {
	e, p := r.env, r.plan
	if r.postureStop != nil {
		return CopyStatusFailed, r.postureStop
	}
	n := len(p.Messages)
	for i := 0; i < n; i++ {
		m := &p.Messages[i]
		if walk.Err() != nil {
			return CopyStatusCancelled, nil
		}
		if s := r.watch.stoppedFatal(); s != nil {
			return CopyStatusFailed, r.complete(s, i, "")
		}
		if i%100 == 0 {
			if upn, ok := r.src.SignedIn(); !ok || !strings.EqualFold(upn, p.Source.Mailbox) {
				now := upn
				if !ok {
					now = ""
				}
				return CopyStatusFailed, r.complete(&CopyStop{RID: "R-z", Class: stopClassSignInMoved}, i, now)
			}
		}
		for e.busy() { // a capture is running: the export backs off, uncounted
			if err := e.pause(walk, e.backoffFirst()); err != nil {
				return CopyStatusCancelled, nil
			}
		}
		r.folder.Store(m.FolderPath())
		if stop := r.preMessage(i); stop != nil {
			return CopyStatusFailed, stop
		}
		line, stop, cancelled := r.exportOne(walk, i+1, m)
		if cancelled {
			return CopyStatusCancelled, nil
		}
		if line != nil {
			if err := r.rec.append(line); err != nil {
				return CopyStatusFailed, r.complete(&CopyStop{RID: "R-d2", Class: stopClassState, InFlight: m.SourceID}, i+1, "")
			}
			if line.Status == statusFailed {
				r.failed.Add(1)
			}
			r.done.Add(1)
			e.step(fmt.Sprintf("W6:%d", i+1))
		}
		if stop != nil {
			stop.InFlight = ""
			if line != nil {
				stop.InFlight = m.SourceID
			}
			return CopyStatusFailed, stop
		}
	}
	return CopyStatusComplete, nil
}

// preMessage is the free-space and state-volume check before every message (RR2-C5,
// RR2-C7): the destination's budget is the free bytes less the reserve, and the state
// volume must hold the run record's remaining bound. A stop here is before the
// message's first byte, so the message stays first in not_attempted.
func (r *CopyRun) preMessage(i int) *CopyStop {
	p := r.plan
	if budget, err := r.budget(); err != nil {
		return r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination, Message: leaveName(err)}, i, leaveName(err))
	} else if m := &p.Messages[i]; budget <= 0 || (m.SizeHint >= 0 && budget < m.SizeHint) {
		return r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination}, i, "no space left")
	}
	remaining := int64(len(p.Messages)-i)*copyRecordLineBound + 4096
	if sfree, err := r.env.freeOnState(); err == nil && int64(sfree) < remaining {
		return r.complete(&CopyStop{RID: "R-d2", Class: stopClassState}, i, "")
	}
	return nil
}

// budget is the destination's free bytes less the finalization reserve.
func (r *CopyRun) budget() (int64, error) {
	free, err := r.env.freeOnDest(r.plan.Dest)
	if err != nil {
		return 0, err
	}
	b := int64(free) - r.plan.Reserve
	if b < 0 {
		return 0, nil
	}
	return b, nil
}

// complete fills a stop's message (design §7.6) from what is known when it is made.
func (r *CopyRun) complete(s *CopyStop, done int, extra string) *CopyStop {
	if s.ready {
		return s
	}
	arg := extra
	if (s.RID == "R-u3" || s.RID == "R-z") && s.Message != "" {
		arg = s.Message // the redirect's host, or the account now signed in: left in Message by the stop
	}
	s.Message = refusalText(s.RID, done, len(r.plan.Messages), arg, r.plan, r.env.StateDir)
	s.StoppedAt = r.env.nowTime().UTC().Format(time.RFC3339)
	s.ready = true
	return s
}

// ensureDir makes dir (relative to the export root, forward slashes) one level at a
// time, each mkdir exclusive: an existing directory that this run did not make is a
// failure, never swallowed (XC13).
func (r *CopyRun) ensureDir(dir string) error {
	if dir == "" || r.made[dir] {
		return nil
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		if r.made[sub] {
			continue
		}
		if err := r.fs.Mkdir(sub); err != nil {
			return err
		}
		r.made[sub] = true
		r.dirs = append(r.dirs, sub)
	}
	return nil
}

// placeRetry gives a temp file its final name, retrying in place a rename that a
// sharing violation or an access-denied refuses (OneDrive, an antivirus scanner):
// 5 tries, 100 ms doubling to 2 s, before the message fails (XC10).
func (r *CopyRun) placeRetry(ctx context.Context, dir, tmp, final string) error {
	delay := 100 * time.Millisecond
	var err error
	for try := 0; try < 5; try++ {
		if err = r.fs.Place(dir, tmp, final); err == nil || !sharingViolation(runtime.GOOS, err) {
			return err
		}
		if perr := r.env.pause(ctx, delay); perr != nil {
			return perr
		}
		delay = min(delay*2, 2*time.Second)
	}
	return err
}

// exportOne copies one message's original. It returns the progress line to record
// (nil when the message was not started), a run-fatal stop, or cancelled.
func (r *CopyRun) exportOne(walk context.Context, n int, m *CopyMessage) (line *ceMsgLine, stop *CopyStop, cancelled bool) {
	e := r.env
	pl := r.namer.place(m, 1)
	final := pl.Stem + ".eml"
	start := r.watch.awake()
	cuts := 0
	refreshed := false
	backoff := e.backoffFirst()
	failure := func(class, reason string) *ceMsgLine {
		return &ceMsgLine{T: "msg", N: n, Status: statusFailed, Class: class, Reason: reason}
	}
	for {
		if walk.Err() != nil {
			return nil, nil, true
		}
		if s := r.watch.stoppedFatal(); s != nil {
			return failure(classRetryable, stopLineReason(s)), r.complete(s, int(r.done.Load()), s.Message), false
		}
		budget, berr := r.budget()
		if berr != nil || budget <= 0 || (m.SizeHint >= 0 && budget < m.SizeHint) {
			// Before this attempt's first byte: the disk stopped the run, not the
			// message, so it stays first in not_attempted (RR2b-C6).
			why := "no space left"
			if berr != nil {
				why = leaveName(berr)
			}
			return nil, r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination}, int(r.done.Load()), why), false
		}
		maxBytes := min(e.cap(), budget)

		mctx, mcancel := context.WithCancelCause(walk)
		r.watch.setMessage(mcancel)
		sus := r.watch.suspends.Load()

		var hw *ceHashWriter
		var tmpFile ceFile
		var tmpName string
		var ferr error
		if ferr = r.ensureDir(pl.Dir); ferr == nil {
			tmpFile, tmpName, ferr = r.fs.CreateTemp(pl.Dir)
		}
		var got int64
		if ferr == nil {
			hw = &ceHashWriter{w: tmpFile, h: sha256.New(), watch: r.watch, total: &r.bytes}
			got, ferr = r.src.Fetch(mctx, m, hw, maxBytes)
			if ferr == nil {
				e.step(fmt.Sprintf("W5pre:%d", n))
				if err := tmpFile.Sync(); err != nil {
					ferr = &ceWriteError{err}
				}
			}
			if cerr := tmpFile.Close(); ferr == nil && cerr != nil {
				ferr = &ceWriteError{cerr}
			}
		} else if tmpFile == nil && !errors.Is(ferr, context.Canceled) {
			ferr = &ceWriteError{ferr} // the directory or the temp file could not be made
		}
		spanned := r.watch.suspends.Load() != sus
		delivered := int64(0)
		if hw != nil {
			delivered = hw.n
		}
		if ferr == nil {
			// Placement: no-replace, relative to the directory handle.
			e.step(fmt.Sprintf("W5tmp:%d", n))
			placed, perr := r.placeFinal(mctx, pl.Dir, tmpName, final)
			if perr == nil {
				mcancel(nil)
				r.watch.setMessage(nil)
				r.watch.bump()
				r.consecWrite, r.consecServer = 0, 0
				f := ceFileRec{Path: pl.Dir + "/" + placed, Role: roleOriginal, SHA256: hex.EncodeToString(hw.h.Sum(nil)), Size: got}
				e.step(fmt.Sprintf("W5:%d", n))
				return &ceMsgLine{T: "msg", N: n, Status: statusExported, Files: []ceFileRec{f}}, nil, false
			}
			ferr = &ceWriteError{perr}
			r.fs.Remove(ceJoin(pl.Dir, tmpName))
		} else if tmpName != "" {
			r.fs.Remove(ceJoin(pl.Dir, tmpName)) // the attempt's temp is removed before anything else (RR2b-C5)
		}

		v := r.classifyFetch(ferr, mctx, walk, delivered, spanned, refreshed)
		mcancel(nil)
		r.watch.setMessage(nil)
		switch v.kind {
		case verdictCancelled:
			return nil, nil, true
		case verdictAuth:
			if rerr := r.src.RefreshAuth(walk); rerr != nil {
				if walk.Err() != nil {
					return nil, nil, true
				}
				s := r.complete(&CopyStop{RID: "R-c", Class: stopClassSignIn}, int(r.done.Load()), "")
				return failure(classRetryable, stopLineReason(s)), s, false
			}
			refreshed = true
			continue
		case verdictFatal:
			s := r.complete(v.stop, int(r.done.Load()), v.stop.Message)
			return failure(classRetryable, stopLineReason(s)), s, false
		case verdictList:
			if _, isWrite := asWriteErr(ferr); isWrite {
				if s := r.writeFailed(ferr, pl.Dir); s != nil {
					return failure(classRetryable, v.reason), s, false
				}
			} else {
				r.consecWrite = 0
			}
			if v.serverClass {
				r.consecServer++
				if r.consecServer >= 25 {
					s := r.complete(&CopyStop{RID: "R-u2", Class: stopClassServer}, int(r.done.Load()), "")
					return failure(classRetryable, stopLineReason(s)), s, false
				}
			} else {
				r.consecServer = 0
			}
			return failure(v.class, v.reason), nil, false
		}
		// Transport class: back off on the same message. It is not a failure, it
		// counts toward the no-progress window, and a cut counts toward the
		// message's download bound (RR2b-C1).
		if v.headerStall {
			r.watch.noteHeaderStall()
		}
		if v.cut {
			cuts++
		}
		if cuts >= 5 {
			return failure(classRetryable, "the download from Microsoft 365 kept stopping partway (5 tries)"), nil, false
		}
		if r.watch.awake()-start >= e.messageBound() {
			return failure(classRetryable, "didn't finish downloading within 2 hours of download time"), nil, false
		}
		if err := e.pause(walk, backoff); err != nil {
			return nil, nil, true
		}
		if s := r.watch.stoppedFatal(); s != nil {
			return failure(classRetryable, stopLineReason(s)), r.complete(s, int(r.done.Load()), s.Message), false
		}
		backoff = min(backoff*2, e.backoffCeiling())
	}
}

// placeFinal gives the temp its final name. An existing name (a file a co-writer put
// there) is never replaced: the stem moves to the next unique variant, and the
// message fails when none is free (XC13). It returns the name that was used.
func (r *CopyRun) placeFinal(ctx context.Context, dir, tmp, final string) (string, error) {
	err := r.placeRetry(ctx, dir, tmp, final)
	if err == nil {
		return final, nil
	}
	stem := strings.TrimSuffix(final, ".eml")
	for k := 2; isExist(err) && k <= 9; k++ {
		alt := fmt.Sprintf("%s~%d.eml", stem, k)
		if err = r.placeRetry(ctx, dir, tmp, alt); err == nil {
			return alt, nil
		}
	}
	return "", err
}

func asWriteErr(err error) (*ceWriteError, bool) {
	var we *ceWriteError
	return we, errors.As(err, &we)
}

// writeFailed handles a write error of the destination that is not in the fatal
// table: the message is listed and the root probed (a failed probe is run-fatal),
// and 5 consecutive write failures of any class are run-fatal (XC10).
func (r *CopyRun) writeFailed(err error, dir string) *CopyStop {
	r.consecWrite++
	r.lastWriteErr = leaveName(err)
	if perr := r.fs.Probe(); perr != nil {
		return r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination}, int(r.done.Load()), leaveName(perr))
	}
	if r.consecWrite >= 5 {
		return r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination}, int(r.done.Load()), r.lastWriteErr)
	}
	return nil
}

// stopLineReason is the reason an in-flight message carries when a run-fatal stop
// aborted it: the stop's id and class, so the retry reads why (RR2b-C6).
func stopLineReason(s *CopyStop) string {
	return fmt.Sprintf("the export stopped while this message was being exported (%s, %s)", s.RID, s.Class)
}
