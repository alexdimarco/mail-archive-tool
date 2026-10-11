package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reconciliation, the launch sweep, Finish as partial, Check and finish, Locate and
// "This folder is gone" (design §4.13, §4.16, §4.19; RR2-C1..C4, RR2-C7, RR2-C12).
// Status comes only from the journal and the run record: the export directory is
// probed for presence, the marker and identity, and is never parsed for status, ids,
// hashes or a fingerprint (XC4). Reconciliation only ever downgrades. The background
// goroutine that calls it once per launch, and the endpoints that call the rest,
// are slice E3's.

// copyProbeTimeout bounds one probe of an export folder (a hung probe leaks only its
// own goroutine). It is a variable so a test can shorten it.
var copyProbeTimeout = 5 * time.Second

// probeFolderFn is the probe itself, and probeHook, when set, runs inside it first: the
// seams a test uses to make a folder's stat block (MA-448).
var (
	probeFolderFn = probeFolder
	probeHook     func(path string)
)

// SetProbeHook makes fn run inside every folder probe, before it looks at the folder,
// and returns what undoes it. It is a test seam: a hook that blocks stands for a stat
// on a dead network mount (XC4's background reconciler must not make the page wait),
// and a short timeout stands for the 5 s bound.
func SetProbeHook(fn func(path string), timeout time.Duration) (restore func()) {
	oldHook, oldTimeout := probeHook, copyProbeTimeout
	probeHook = fn
	if timeout > 0 {
		copyProbeTimeout = timeout
	}
	return func() { probeHook, copyProbeTimeout = oldHook, oldTimeout }
}

// Observation kinds.
const (
	obsNotFound   = "not-found"
	obsNoAnswer   = "no-answer"
	obsUnconfirm  = "unconfirmed"
	obsMoved      = "moved"
	obsGone       = "gone"
	obsEmpty      = "interrupted-empty"
	obsInterrupt  = "interrupted"
	obsFinishing  = "finished-writing"
	obsRecovered  = "recovered"
	obsChanged    = "changed"
	obsNewer      = "newer-format"
	obsNoRecord   = "no-record"
	obsSettledRun = "settled"
)

// CopyObservation is what reconciling a run found: its label, whether it is
// terminal, and the actions the activity page may offer.
type CopyObservation struct {
	Run         string
	Kind        string
	Label       string
	Terminal    bool // a marker-and-seal outcome, journaled once and not probed again
	Actions     []string
	Status      CopyStatus // the sealed status, when there is a seal
	Fingerprint string
	At          time.Time
}

// Actions the page offers.
const (
	ActFinishAsPartial = "finish-as-partial"
	ActCheckAndFinish  = "check-and-finish"
	ActLookAgain       = "look-again"
	ActLocate          = "locate"
	ActGone            = "this-folder-is-gone"
	ActExportFailed    = "export-failed-again"
	ActExportRemaining = "export-remaining"
	ActExportThese     = "export-these-again"
	ActOpenFolder      = "open-folder"
)

// folderPath is where the run's export folder is looked for: the path the user
// located it at, else the root recorded at creation.
func folderPath(s *ceRecSummary) string {
	if n := len(s.Located); n > 0 {
		return s.Located[n-1].Path
	}
	return s.Header.Root
}

// probeBounded probes a folder with a 5 s bound.
func probeBounded(path string) (ceFolderSeen, error, bool) {
	type res struct {
		seen ceFolderSeen
		err  error
	}
	ch := make(chan res, 1)
	hook, timeout := probeHook, copyProbeTimeout
	go func() {
		if hook != nil {
			hook(path)
		}
		s, err := probeFolderFn(path)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		return r.seen, r.err, false
	case <-time.After(timeout):
		return ceFolderSeen{}, nil, true
	}
}

// ReconcileRun reconciles one run from its run record, the journal and a bounded
// probe of its folder, and appends only what the outcome calls for: a missing
// ledger line as soon as a sealed record is read, and, for a marker-and-seal outcome,
// the journal `end` (when a probe sees the marker absent beside a sealed record) and
// one terminal `reconciled` record. A positive mismatch, "not found", "did not
// answer" and "couldn't confirm" are observations: cached by the caller, never
// journaled, and probed again.
func (e *CopyExportEnv) ReconcileRun(run string) (CopyObservation, error) {
	obs := CopyObservation{Run: run, At: e.nowTime()}
	sum, err := readSummary(e.recordPath(run))
	if errors.Is(err, errCopyNewerFormat) {
		obs.Kind, obs.Label = obsNewer, "made by a newer MailArchive Desktop — update to retry"
		return obs, nil
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			obs.Kind, obs.Label = obsNoRecord, "this export's run record is missing — Check this export still works"
			return obs, nil
		}
		return obs, err
	}
	if sum.Sealed != nil {
		e.ledgerMu.Lock()
		err := e.ledgerFromSealedLocked(run, sum.Sealed)
		e.ledgerMu.Unlock()
		if err != nil {
			return obs, err
		}
		obs.Status, obs.Fingerprint = sum.Sealed.Status, sum.Sealed.Fingerprint
	}
	if sum.Gone {
		obs.Kind, obs.Label, obs.Terminal = obsGone, "This folder is gone", true
		obs.Actions = e.goneActions(sum)
		return obs, nil
	}
	if j := e.terminalReconciled(run); j != nil && !e.userChanged(sum, j) {
		obs.Kind, obs.Label, obs.Terminal, obs.Fingerprint = j.Status, j.Reason, true, j.Fingerprint
		obs.Actions = e.actionsFor(sum, j.Status)
		return obs, nil
	}

	path := folderPath(sum)
	seen, perr, timedOut := probeBounded(path)
	switch {
	case timedOut || (perr != nil && !errors.Is(perr, fs.ErrNotExist)):
		obs.Kind, obs.Label = obsNoAnswer, "the folder did not answer (at "+obs.At.Format("15:04")+")"
		obs.Actions = []string{ActLookAgain}
		return obs, nil
	case !seen.Exists:
		obs.Kind, obs.Label = obsNotFound, "folder not found (at "+obs.At.Format("15:04")+")"
		obs.Actions = []string{ActLookAgain, ActLocate, ActGone}
		return obs, nil
	}
	switch v, why := judgeIdentity(run, sum.Header.Identity, seen); v {
	case idMismatch:
		obs.Kind, obs.Label = obsMoved, "this folder was moved or replaced since the export"
		_ = why
		obs.Actions = []string{ActLocate, ActGone, ActLookAgain}
		return obs, nil
	case idUnconfirmed:
		obs.Kind, obs.Label = obsUnconfirm, "couldn't confirm this is the same folder — Look again"
		obs.Actions = []string{ActLookAgain, ActLocate, ActGone}
		return obs, nil
	}

	// The folder is the run's own. The outcome is the marker and the seal.
	switch {
	case sum.Sealed == nil && sum.Progress == 0 && !seen.HasReadme && !seen.HasMarker:
		obs.Kind, obs.Label = obsEmpty, "interrupted: empty export folder — the app stopped before writing anything"
		obs.Actions = []string{ActFinishAsPartial}
	case sum.Sealed == nil && seen.HasMarker:
		reason := "the app stopped while this export was running"
		if sum.Stopped != nil {
			reason = sum.Stopped.Reason
		}
		obs.Kind, obs.Label = obsInterrupt, "interrupted: "+reason
		if sum.Stopped == nil {
			obs.Label = "interrupted (" + reason + ")"
		}
		obs.Actions = []string{ActFinishAsPartial}
	case sum.Sealed == nil:
		obs.Kind, obs.Label = obsChanged, "changed after the export: its marker is gone — run Check this export (fingerprint not recorded at export time)"
		obs.Actions = []string{ActFinishAsPartial}
	case seen.HasMarker && markerStays(sum.Sealed.Status) && e.journalHas(run, "end"):
		// A cancelled, failed or finished-as-partial run keeps its marker (rewritten
		// CANCELLED or STOPPED) once it has ended: nothing is owed, and the card keeps
		// its own label and retry offers (EW-AC10). Not journaled: a settled run is
		// read from its own journal lines, not from this observation.
		obs.Kind = obsSettledRun
		obs.Label = copyLabel(sum.Sealed.Status, sum.Sealed.Counts, sum.Sealed.Stop)
		obs.Actions = e.actionsFor(sum, obsRecovered)
		return obs, nil
	case seen.HasMarker:
		obs.Kind = obsFinishing
		obs.Label = "Finished writing (" + copyLabel(sum.Sealed.Status, sum.Sealed.Counts, sum.Sealed.Stop) + ") — this folder's marker may still need updating"
		obs.Actions = []string{ActCheckAndFinish}
	default:
		obs.Kind = obsRecovered
		obs.Label = copyLabel(sum.Sealed.Status, sum.Sealed.Counts, sum.Sealed.Stop) + " (recovered)"
		obs.Actions = e.actionsFor(sum, obsRecovered)
		// A probe that sees the marker absent beside a sealed record appends the end.
		if !e.journalHas(run, "end") {
			if err := e.journalAppend(ceJournalLine{Run: run, Event: "end", Status: string(sum.Sealed.Status),
				Planned: sum.Sealed.Counts.Selected, Exported: sum.Sealed.Counts.Exported, Failed: sum.Sealed.Counts.Failed,
				Fingerprint: sum.Sealed.Fingerprint, Reason: stopReasonText(sum.Sealed.Stop)}); err != nil {
				return obs, err
			}
		}
	}
	// A marker-and-seal outcome is terminal: journaled once, and not probed again
	// unless a user action changes it.
	obs.Terminal = true
	if err := e.journalAppend(ceJournalLine{Run: run, Event: "reconciled", Status: obs.Kind, Reason: obs.Label, Fingerprint: obs.Fingerprint}); err != nil {
		return obs, err
	}
	return obs, nil
}

// markerStays reports whether a sealed status leaves the app's own marker in the folder
// for good (rewritten CANCELLED or STOPPED), as against complete and partial, which
// remove it.
func markerStays(s CopyStatus) bool {
	return s == CopyStatusCancelled || s == CopyStatusFailed || s == CopyStatusFinishedAsPartial
}

// terminalKinds are the marker-and-seal outcomes.
var terminalKinds = map[string]bool{obsEmpty: true, obsInterrupt: true, obsFinishing: true, obsRecovered: true, obsChanged: true, obsGone: true}

// terminalReconciled returns run's latest terminal `reconciled` journal line.
func (e *CopyExportEnv) terminalReconciled(run string) *ceJournalLine {
	lines, _ := e.readJournal()
	var out *ceJournalLine
	for i := range lines {
		if lines[i].Run == run && lines[i].Event == "reconciled" && terminalKinds[lines[i].Status] {
			out = &lines[i]
		}
	}
	return out
}

// userChanged reports whether a user action (a located path, finishing the run)
// postdates a terminal reconciled line, so the run is probed again.
func (e *CopyExportEnv) userChanged(sum *ceRecSummary, j *ceJournalLine) bool {
	if n := len(sum.Located); n > 0 && sum.Located[n-1].At.After(j.At) {
		return true
	}
	if j.Status == obsFinishing || j.Status == obsEmpty || j.Status == obsInterrupt || j.Status == obsChanged {
		// These are outcomes about an unfinished folder: sealing it (Finish as partial,
		// Check and finish) changes what the folder and the record say.
		if sum.Sealed != nil && (j.Status != obsFinishing) {
			return true
		}
		if sum.Sealed != nil && e.journalHas(sum.Header.Run, "end") {
			return true
		}
	}
	return false
}

// actionsFor are the actions a recovered or settled run's card may offer.
func (e *CopyExportEnv) actionsFor(sum *ceRecSummary, kind string) []string {
	switch kind {
	case obsEmpty, obsInterrupt, obsChanged:
		return []string{ActFinishAsPartial}
	case obsFinishing:
		return []string{ActCheckAndFinish}
	case obsGone:
		return e.goneActions(sum)
	}
	acts := []string{ActOpenFolder}
	if sum.Sealed != nil {
		c := sum.Sealed.Counts
		switch sum.Sealed.Status {
		case CopyStatusPartial:
			if c.Failed > 0 {
				acts = append(acts, ActExportFailed)
			}
		case CopyStatusCancelled:
			if c.Failed > 0 {
				acts = append(acts, ActExportFailed)
			}
			acts = append(acts, ActExportRemaining)
		case CopyStatusFailed, CopyStatusFinishedAsPartial:
			acts = append(acts, ActExportRemaining)
		}
	}
	return acts
}

func (e *CopyExportEnv) goneActions(sum *ceRecSummary) []string { return []string{ActExportThese} }

// ---- identity-confirmed access to a run's folder ----------------------------

// R-r and R-r2 (design §7.6).
func refusalR(path string) *CopyRefusal {
	return refuse("R-r", fmt.Sprintf("This export's folder was moved or replaced since the export (%s) — MailArchive won't open it. If the folder is somewhere else now, click Locate… on Export activity and choose it; if it's lost, click This folder is gone.", path))
}

func refusalR2(path string) *CopyRefusal {
	return refuse("R-r2", fmt.Sprintf("MailArchive couldn't confirm that %s is this export's folder (the drive may have been plugged in again, or the folder's marker and README are missing) — it won't open a folder it can't confirm. Click Look again on Export activity, or find the folder yourself.", path))
}

// confirmedRoot opens run's export folder as a contained root, only when §4.9's
// tuple confirms it: a positive mismatch is R-r, an identity it could not confirm is
// R-r2, and a folder that is not there is R-r2 too ("look again").
func (e *CopyExportEnv) confirmedRoot(sum *ceRecSummary) (ceFS, string, error) {
	path := folderPath(sum)
	seen, err, timedOut := probeBounded(path)
	switch {
	case timedOut || (err != nil && !errors.Is(err, fs.ErrNotExist)) || !seen.Exists:
		return nil, path, refusalR2(path)
	}
	switch v, _ := judgeIdentity(sum.Header.Run, sum.Header.Identity, seen); v {
	case idMismatch:
		return nil, path, refusalR(path)
	case idUnconfirmed:
		return nil, path, refusalR2(path)
	}
	// The probe judged the path; the folder written through is the one this open
	// reaches, so it is judged again on the handle itself. A folder swapped for a link
	// or another directory after the probe is refused here, before anything is written
	// (EW-AI1).
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, path, refusalR2(path)
	}
	switch v, _ := judgeIdentity(sum.Header.Run, sum.Header.Identity, seenFromRoot(root)); v {
	case idMismatch:
		root.Close()
		return nil, path, refusalR(path)
	case idUnconfirmed:
		root.Close()
		return nil, path, refusalR2(path)
	}
	return e.fsFromRoot(root, path), path, nil
}

// FinishAsPartial completes an unsealed run from its run record alone, once §4.9's
// tuple confirms its root: README, manifest and sums list exactly the recorded files
// that are present and whose bytes, re-hashed, equal the recorded hash; a present
// file whose hash differs is a listed failure ("damaged when the computer stopped"),
// never a sums line; the remaining ids are not attempted; and the run ends with the
// seal-and-finish step, status finished-as-partial, so the marker is rewritten
// STOPPED. It covers a killed run, a run whose finalization failed, a panicked run
// and a wedged one (RR2-C7).
func (e *CopyExportEnv) FinishAsPartial(run string) (*CopyResult, error) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		return nil, err
	}
	if sum.Sealed != nil {
		return nil, fmt.Errorf("this export is already sealed: use Check and finish")
	}
	fsys, path, err := e.confirmedRoot(sum)
	if err != nil {
		return nil, err
	}
	defer fsys.Close()
	rec, err := e.openRecord(run)
	if err != nil {
		return nil, err
	}
	defer rec.Close()
	var stop *CopyStop
	if sum.Stopped != nil {
		stop = &CopyStop{RID: sum.Stopped.RID, Class: sum.Stopped.Class, Message: sum.Stopped.Reason}
	}
	fin := &ceFinish{env: e, fs: fsys, rec: rec, hdr: sum.Header, started: sum.Header.Started}
	// Finishing writes README, manifest and sums, which a failed finalization may
	// have left half-way: its own earlier copies are replaced, in this confirmed
	// root and nowhere else.
	res, err := fin.run(CopyStatusFinishedAsPartial, stop, true, true)
	if res != nil {
		res.RunID, res.Root, res.RootName = run, path, sum.Header.RootName
	}
	return res, err
}

// CheckAndFinish completes a sealed run whose marker action or journal end is still
// owed: it verifies sha256(SUMS on disk) equals the sealed fingerprint and every
// listed file's hash, then runs the seal-and-finish step, whose marker action
// removes the app's own marker for complete or partial and rewrites it CANCELLED or
// STOPPED for the rest, never removing it then. It refuses, with the marker kept,
// when anything differs.
func (e *CopyExportEnv) CheckAndFinish(run string) (*CopyResult, error) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		return nil, err
	}
	if sum.Sealed == nil {
		return nil, fmt.Errorf("this export is not sealed: use Finish as partial")
	}
	fsys, path, err := e.confirmedRoot(sum)
	if err != nil {
		return nil, err
	}
	defer fsys.Close()
	if err := checkSums(fsys, sum.Sealed.Fingerprint); err != nil {
		return nil, err
	}
	rec, err := e.openRecord(run)
	if err != nil {
		return nil, err
	}
	defer rec.Close()
	seal := &ceSeal{env: e, fs: fsys, rec: rec, hdr: sum.Header, status: sum.Sealed.Status, counts: sum.Sealed.Counts, stop: sum.Sealed.Stop,
		fingerprint: sum.Sealed.Fingerprint}
	if err := seal.run(); err != nil {
		return nil, err
	}
	return &CopyResult{RunID: run, Root: path, RootName: sum.Header.RootName, Status: sum.Sealed.Status, Outcome: sum.Sealed.Status.Outcome(),
		Counts: sum.Sealed.Counts, Fingerprint: sum.Sealed.Fingerprint, Stop: sum.Sealed.Stop,
		Label: copyLabel(sum.Sealed.Status, sum.Sealed.Counts, sum.Sealed.Stop)}, nil
}

// errCheckFailed is Check and finish's refusal; its text names what differs.
type errCheckFailed struct{ what string }

func (e *errCheckFailed) Error() string {
	return "this export's checksums don't match what MailArchive recorded when it finished it (" + e.what + ") — the marker is kept. Run Check this export, or export the messages again."
}

// IsCheckFailed reports whether err is Check and finish's refusal: the folder's files do not
// match what was recorded when the export finished, and the marker was kept.
func IsCheckFailed(err error) bool {
	var c *errCheckFailed
	return errors.As(err, &c)
}

// IsNewerFormat reports whether err says a run record was written by a newer MailArchive
// Desktop than this one.
func IsNewerFormat(err error) bool { return errors.Is(err, errCopyNewerFormat) }

// checkSums verifies SHA256SUMS.txt against the sealed fingerprint and every file it
// lists against its line.
func checkSums(fsys ceFS, fingerprint string) error {
	f, err := fsys.Open(copySumsName)
	if err != nil {
		return &errCheckFailed{"SHA256SUMS.txt is missing"}
	}
	defer f.Close()
	data, err := readAll(f)
	if err != nil {
		return &errCheckFailed{"SHA256SUMS.txt can't be read"}
	}
	if got := hexSHA256(data); got != fingerprint {
		return &errCheckFailed{"SHA256SUMS.txt is not the one that was sealed"}
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		hash, path, ok := strings.Cut(line, "  ")
		if !ok {
			return &errCheckFailed{"a line of SHA256SUMS.txt is malformed"}
		}
		got, err := hashInRoot(fsys, path)
		if errors.Is(err, errNotRegular) {
			return &errCheckFailed{path + " is not a regular file any more"}
		}
		if err != nil {
			return &errCheckFailed{path + " is missing"}
		}
		if got != hash {
			return &errCheckFailed{path + " has changed"}
		}
	}
	return nil
}

// ---- gone, located ----------------------------------------------------------

// MarkGone records the user's answer "This folder is gone": a `gone` line in the run
// record (fsynced, so it outlives the journal and compaction), a `gone` line in the
// fingerprint ledger for a run that never sealed, and a terminal `reconciled` journal
// record (design §4.16, RR-2b re-verification).
func (e *CopyExportEnv) MarkGone(run string) (CopyObservation, error) {
	rec, err := e.openRecord(run)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return e.goneFromJournal(run)
		}
		return CopyObservation{}, err
	}
	defer rec.Close()
	sum, err := readSummary(rec.path)
	if err != nil {
		return CopyObservation{}, err
	}
	if err := rec.append(ceGoneLine{T: "gone", At: e.nowTime()}); err != nil {
		return CopyObservation{}, err
	}
	e.ledgerMu.Lock()
	ledger, _ := e.readLedger()
	var lerr error
	if !ledgerHas(ledger, run) {
		lerr = e.appendJSONL(e.ledgerPath(), ceLedgerLine{V: 1, Run: run, Chain: sum.Header.Chain, Kind: "gone", At: e.nowTime()})
	}
	e.ledgerMu.Unlock()
	if lerr != nil {
		return CopyObservation{}, lerr
	}
	if err := e.journalAppend(ceJournalLine{Run: run, Event: "reconciled", Status: obsGone, Reason: "This folder is gone"}); err != nil {
		return CopyObservation{}, err
	}
	sum.Gone = true
	return CopyObservation{Run: run, Kind: obsGone, Label: "This folder is gone", Terminal: true, Actions: e.goneActions(sum)}, nil
}

// Locate accepts the folder at a new path for run, only when the identity check
// passes, and appends that path to the run record; the run is then reconciled at that
// path. A folder that fails the check is refused (R-r or R-r2) and nothing is recorded.
func (e *CopyExportEnv) Locate(run, path string) error {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return e.locateFromJournal(run, path)
		}
		return err
	}
	if !filepath.IsAbs(path) {
		return refusalR2(path)
	}
	seen, perr, timedOut := probeBounded(path)
	if timedOut || perr != nil || !seen.Exists {
		return refusalR2(path)
	}
	switch v, _ := judgeIdentity(run, sum.Header.Identity, seen); v {
	case idMismatch:
		return refusalR(path)
	case idUnconfirmed:
		return refusalR2(path)
	}
	rec, err := e.openRecord(run)
	if err != nil {
		return err
	}
	defer rec.Close()
	return rec.append(ceLocatedLine{T: "located", Path: path, Identity: seen.Cur, At: e.nowTime()})
}

// goneFromJournal is "This folder is gone" for a settled run whose record is deleted
// (§4.19 deletes a settled, chain-complete run's record at once): there is no record to
// write to, so the answer is a terminal `reconciled` journal line.
func (e *CopyExportEnv) goneFromJournal(run string) (CopyObservation, error) {
	if _, err := e.folderSpec(run); err != nil {
		return CopyObservation{}, err
	}
	if err := e.journalAppend(ceJournalLine{Run: run, Event: "reconciled", Status: obsGone, Reason: "This folder is gone"}); err != nil {
		return CopyObservation{}, err
	}
	return CopyObservation{Run: run, Kind: obsGone, Label: "This folder is gone", Terminal: true}, nil
}

// locateFromJournal is Locate… for a run whose record is deleted: the journal run id is
// the nonce, and the start line's dest_dir and root_id are the recorded identity; an
// accepted folder is recorded as a `located` journal line, since there is no record.
func (e *CopyExportEnv) locateFromJournal(run, path string) error {
	spec, err := e.folderSpec(run)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return refusalR2(path)
	}
	seen, perr, timedOut := probeBounded(path)
	if timedOut || perr != nil || !seen.Exists {
		return refusalR2(path)
	}
	switch v, _ := judgeIdentity(run, spec.Identity, seen); v {
	case idMismatch:
		return refusalR(path)
	case idUnconfirmed:
		return refusalR2(path)
	}
	cur := seen.Cur
	return e.journalAppend(ceJournalLine{Run: run, Event: "located", DestDir: path, RootID: &cur})
}

// ---- the launch sweep and the record lifetime -------------------------------

// SweepRunRecords is the launch sweep (design §4.16, RR2-C12): it lists the run
// records in the state directory and adopts any that no journal line names (a kill
// inside W3, a lost or damaged journal, a compaction stopped before its deletions),
// rebuilding its `start` line from the record's header and, from a `gone` line, its
// terminal `reconciled` record. It indexes the records by chain id, appends every
// missing ledger line (from a sealed line, or from a `gone` line for a run that never
// sealed), and deletes the record of any run that is settled and whose chain is
// complete. A record is deleted only after its ledger line exists. It returns the
// run ids it adopted.
func (e *CopyExportEnv) SweepRunRecords() (adopted []string, err error) {
	entries, err := os.ReadDir(e.recordsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, stateErr(err)
	}
	e.ledgerMu.Lock()
	e.chainsBuilt = false
	e.chains = nil
	e.ledgerMu.Unlock()
	var runs []string
	for _, ent := range entries {
		if run, ok := strings.CutSuffix(ent.Name(), ".jsonl"); ok {
			runs = append(runs, run)
		}
	}
	journal, _ := e.readJournal()
	named := map[string]bool{}
	for _, l := range journal {
		named[l.Run] = true
	}
	for _, run := range runs {
		sum, serr := readSummary(e.recordPath(run))
		if serr != nil {
			continue // unreadable or newer: never reinterpreted, never deleted
		}
		if sum.Header.Host != "" && sum.Header.Host != e.Host {
			continue
		}
		e.noteRecord(sum.Header.Chain, run)
		if !named[run] {
			id := sum.Header.Identity
			if jerr := e.journalAppend(ceJournalLine{Run: run, Event: "start", At: sum.Header.Started, SourceKind: sum.Header.Source.Kind,
				Mailbox: sum.Header.Source.Mailbox, DestDir: sum.Header.Root, RootID: &id, Planned: sum.Header.Planned}); jerr != nil {
				return adopted, jerr
			}
			if sum.Gone {
				if jerr := e.journalAppend(ceJournalLine{Run: run, Event: "reconciled", Status: obsGone, Reason: "This folder is gone"}); jerr != nil {
					return adopted, jerr
				}
			}
			adopted = append(adopted, run)
		}
		e.ledgerMu.Lock()
		ledger, _ := e.readLedger()
		var lerr error
		switch {
		case ledgerHas(ledger, run):
		case sum.Sealed != nil:
			lerr = e.ledgerFromSealedLocked(run, sum.Sealed)
		case sum.Gone:
			lerr = e.appendJSONL(e.ledgerPath(), ceLedgerLine{V: 1, Run: run, Chain: sum.Header.Chain, Kind: "gone", At: e.nowTime()})
		}
		e.ledgerMu.Unlock()
		if lerr != nil {
			return adopted, lerr
		}
	}
	// Deletion is a second pass, once every ledger line the first pass could append is there.
	// Every record is judged before any is removed: a part's record is what shows that
	// another part's failed message was exported, so deleting one first would change
	// the verdict on its chain's other parts.
	var deletable []string
	for _, run := range runs {
		if e.recordDeletable(run) {
			deletable = append(deletable, run)
		}
	}
	for _, run := range deletable {
		os.Remove(e.recordPath(run))
	}
	return adopted, nil
}

// settledRun reports whether a run is settled (design §4.19 Lifetime): its ledger
// line exists and either it is sealed and its journal `end` exists (no marker action
// is left), or the user has said its folder is gone, or its start was refused.
func (e *CopyExportEnv) settledRun(sum *ceRecSummary, ledger []ceLedgerLine) bool {
	if !ledgerHas(ledger, sum.Header.Run) {
		return false
	}
	if sum.Gone {
		return true
	}
	return sum.Sealed != nil && e.journalHas(sum.Header.Run, "end")
}

// recordDeletable is true when run is settled and its chain is complete: every part
// of the chain is settled, and no part's card offers an export action any more.
func (e *CopyExportEnv) recordDeletable(run string) bool {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		return false
	}
	ledger, _ := e.readLedger()
	if !e.settledRun(sum, ledger) {
		return false
	}
	e.ledgerMu.Lock()
	parts := e.chainRunsLocked(sum.Header.Chain)
	e.ledgerMu.Unlock()
	covered := map[string]bool{}
	var sums []*ceRecSummary
	for _, p := range parts {
		ps, err := readSummary(e.recordPath(p))
		if errors.Is(err, fs.ErrNotExist) {
			continue // a part whose record is gone was deleted settled, with its chain complete
		}
		if err != nil {
			return false // an unreadable part keeps the chain open
		}
		if !e.settledRun(ps, ledger) {
			return false
		}
		sums = append(sums, ps)
		if ps.Sealed != nil && !ps.Gone {
			walkMessages(e.recordPath(p), func(pl *cePlanLine, m *ceMsgLine) error {
				if m != nil && (m.Status == statusExported || m.Status == statusWithoutOrig || m.Status == statusOriginalOnly) {
					covered[pl.SourceID] = true
				}
				return nil
			})
		}
	}
	// No card offers an export action: every message of every part's plan was
	// exported in a sealed part not marked gone, or failed with a terminal class.
	complete := true
	for _, ps := range sums {
		walkMessages(e.recordPath(ps.Header.Run), func(pl *cePlanLine, m *ceMsgLine) error {
			if covered[pl.SourceID] || (m != nil && m.Status == statusFailed && m.Class == classTerminal) {
				return nil
			}
			complete = false
			return nil
		})
	}
	return complete
}

// RunRemaining is what "Export the remaining messages" reads from a run's record
// (P4's second admission path, XC2; never the destination's manifest): for a cancelled
// run its not_attempted ids; for a run that stopped run-fatal, was finished as
// partial, or never sealed, the not_attempted ids, then the run's other retryable
// failures, with the message that was in flight last (RR2b-C6), so a stop's own remedy
// reaches every message it left. For a part marked gone it is the part's plan minus
// only the messages exported in the chain's other sealed parts that are not marked gone.
func (e *CopyExportEnv) RunRemaining(run string) ([]CopyMessage, error) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		return nil, err
	}
	path := e.recordPath(run)
	if sum.Gone {
		exported := map[string]bool{}
		e.ledgerMu.Lock()
		parts := e.chainRunsLocked(sum.Header.Chain)
		e.ledgerMu.Unlock()
		for _, p := range parts {
			if p == run {
				continue
			}
			ps, perr := readSummary(e.recordPath(p))
			if perr != nil || ps.Sealed == nil || ps.Gone {
				continue
			}
			walkMessages(e.recordPath(p), func(pl *cePlanLine, m *ceMsgLine) error {
				if m != nil && (m.Status == statusExported || m.Status == statusWithoutOrig || m.Status == statusOriginalOnly) {
					exported[pl.SourceID] = true
				}
				return nil
			})
		}
		var out []CopyMessage
		walkMessages(path, func(pl *cePlanLine, m *ceMsgLine) error {
			if !exported[pl.SourceID] {
				out = append(out, pl.CopyMessage)
			}
			return nil
		})
		return out, nil
	}
	cancelled := sum.Sealed != nil && sum.Sealed.Status == CopyStatusCancelled
	inFlight := ""
	if sum.Sealed != nil && sum.Sealed.Stop != nil {
		inFlight = sum.Sealed.Stop.InFlight
	}
	var notDone, retry []CopyMessage
	var last *CopyMessage
	err = walkMessages(path, func(pl *cePlanLine, m *ceMsgLine) error {
		switch {
		case m == nil || m.Status == statusNotAttempted:
			notDone = append(notDone, pl.CopyMessage)
		case m.Status == statusFailed && m.Class == classRetryable && !cancelled:
			if pl.SourceID == inFlight || (inFlight == "" && strings.HasPrefix(m.Reason, "the export stopped while this message")) {
				cm := pl.CopyMessage
				last = &cm
			} else {
				retry = append(retry, pl.CopyMessage)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := append(notDone, retry...)
	if last != nil {
		out = append(out, *last)
	}
	return out, nil
}

// RunFailed is what "Export failed messages again" reads: the run's retryable
// failures, from its run record.
func (e *CopyExportEnv) RunFailed(run string) ([]CopyMessage, error) {
	var out []CopyMessage
	err := walkMessages(e.recordPath(run), func(pl *cePlanLine, m *ceMsgLine) error {
		if m != nil && m.Status == statusFailed && m.Class == classRetryable {
			out = append(out, pl.CopyMessage)
		}
		return nil
	})
	return out, err
}
