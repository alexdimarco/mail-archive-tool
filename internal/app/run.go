// Package app orchestrates a full export run: input discovery, per-file reading
// (with optional snapshotting), and manifest-tracked writing. Both the CLI and
// the GUI drive the exporter through Run so behaviour stays identical.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// Options describes a single export run.
type Options struct {
	Inputs    []string    // files and/or directories to read
	Auto      bool        // also auto-discover default Outlook locations
	Out       string      // output root directory
	Mode      export.Mode // incremental or full
	Since     time.Time   // zero means no date filter
	CopyFirst bool        // snapshot each file before reading (avoids locks)
	Manifest  string      // manifest path override (default <Out>/.mailarchive-manifest.json)
	Index     bool        // build/update the search index (search.db)
	Pages     bool        // generate browsable folder index.html pages

	// KeepRaw also preserves each message's original RFC 822 bytes as
	// <stem>.eml (mbox/maildir/Graph sources; a PST item has none).
	KeepRaw bool

	// CheckpointEvery saves the manifest and flushes the index every N
	// exported messages inside a store walk (R5: a hard crash keeps the
	// progress made). Zero means the default (1000).
	CheckpointEvery int

	// Warnings are problems the caller found with this run's setup that do not
	// stop it (a credential inside the archive, design-local-confidentiality
	// P10). The run records them in its last-run record, where `status` WARNs
	// them.
	Warnings []state.RunWarning
	// OutlookSkips are the folders and accounts `-outlook`'s Outlook export did not
	// carry into its PSTs (outlookcom.CreatePSTs): the run records each as a gap
	// (outlook-export-skipped, fillable; or outlook-account-skipped, info), so the
	// archive is never silently smaller than the mailbox and the run is partial
	// (FC28).
	OutlookSkips []outlookcom.Skip
	// OutlookExported are the PSTs `-outlook`'s Outlook export wrote in THIS run
	// (outlookcom.CreatePSTs' stores). Such a PST is a fresh image of the mailbox, so a
	// folder Outlook once failed to export that it neither carries nor skips again is
	// no longer in Outlook, and a complete walk of it closes that gap (NF1). A PST that
	// is merely left over from an earlier export says nothing about Outlook today.
	OutlookExported []string
	// Unattended marks a scheduled run (the CLI's -unattended, the GUI's
	// headless job). A discovery refusal (a vanished -input, no sources found)
	// on an archive that already exists is then recorded as a failed run, so
	// `status` reports it (N13, R18); see RecordRefusedRun.
	Unattended bool

	// FollowLinks names the inputs whose directory links are followed (owner ruling
	// OR6: the per-input opt-in; the CLI's -follow-links, the wizard's "Follow folder
	// links" answer, a job file's follow_links). By default a local store is read
	// without following a directory link, and each such link is a terminal gap whose
	// remedy names this option. An input listed here keeps its token and its archive.
	// A file link out of the store and a special file stay refused for it too (R25).
	FollowLinks []string

	// Key, when non-nil, makes this an ENCRYPTED archive's run: content is sealed
	// into units under the archive's key, which must open the vault in Out
	// (design-encrypted-archives-phase2a). Nil — every run today — is a plaintext
	// archive. Reaching it from production code is gated by assureblock's
	// reachability test until Phase 3.
	Key *vault.Key
	// CredentialPaths are the files and folders an encrypted run's caller keeps
	// credentials or settings in (a -config folder, a client-secret file): a keyed
	// run refuses when any of them, or MailArchive's own settings or working folder,
	// is at or under Out (P2a-C14). Unused by a plaintext run.
	CredentialPaths []string
	// RecordedID is the archive id this computer recorded for Out (A15), set with
	// Key by internal/keyring: the keyed preflight refuses a key whose id is not it
	// (phase3 §6). Zero only in tests that build a key directly, which then check
	// the key against its own id.
	RecordedID vault.ID
	// RecordedGen is the highest generation of the encrypted archive's sealed record
	// this machine committed, recorded outside the archive (A25; Phases 3-4 persist
	// it). Zero: none recorded.
	RecordedGen uint64
	// ProgramCopies is the person's choice to keep copies of MailArchive in an
	// encrypted archive (program design §6, R21): on or off is sealed by this run's
	// commit; unset leaves what the record sealed. Unused by a plaintext run.
	ProgramCopies ProgramChoice
	// ProgramCopiesIfUnchosen is the choice an archive was made with, sealed only while
	// the record holds none — a run job's, carried until a run seals it, never over the
	// person's later change (the PG4 pass, SEC-1).
	ProgramCopiesIfUnchosen ProgramChoice
	// ProgramSkip, when closed, ends the program step's wait for its download, and only
	// the wait: the run goes on with this computer's own copy (R20). OnProgramWait is
	// told when that wait starts and ends, so a front end offers the skip only then.
	ProgramSkip   <-chan struct{}
	OnProgramWait func(waiting bool)
	// ProgramDownload, when set, is the context the run's program download runs under,
	// so it outlives the run — the Desktop's own, where a skipped download carries on
	// for the next backup (the PG4 pass, DUR-1). Unset: the run's own.
	ProgramDownload context.Context
	// settingOnly: a setting's keyed change (a pause note, keeping program copies), not a
	// backup — the note beside the archive is left for the next backup to check.
	settingOnly bool

	// BeforeRead, when non-nil, runs once after the archive lock is held and the
	// run has begun, and before any source is read: the opt-in Thunderbird
	// pre-sync (start Thunderbird out of sight, wait for its mail files to go
	// quiet, ask it to quit, then return). It NEVER fails the run — it recovers
	// its own panics — and only ctx cancellation stops the run. Its warnings are
	// recorded; it is nil in every path but the opt-in one (design
	// docs/design-thunderbird-presync.md §4.1).
	BeforeRead func(ctx context.Context, files []string) BeforeReadResult
}

// BeforeReadResult is what the pre-sync hook returns: warnings for the run to
// record. The session summary stored in the last-run record is added by a later
// slice (it is additive).
type BeforeReadResult struct {
	Warnings []state.RunWarning
	// Release, when non-nil, is called by Run AFTER the sources are read (always, via
	// defer — even on error or panic): the Thunderbird pre-sync holds a per-profile
	// lock across the pre-sync AND the read, so a second scheduled run cannot pre-sync
	// and compact that profile while this run reads it (adversarial finding C2).
	Release func()
}

// followsLinks says whether path is one of the inputs that opted in to following
// directory links, compared as the canonical source id (the same input spelled
// another way is the same input).
func followsLinks(opted []string, path string) bool {
	if len(opted) == 0 {
		return false
	}
	id := state.PathSourceID(path)
	for _, o := range opted {
		if state.PathSourceID(o) == id {
			return true
		}
	}
	return false
}

// FollowLinksRefusal refuses a directory-link opt-in that names no input of the run:
// it would be read as no opt-in at all, and the operator would believe their links
// were being read. opted and inputs are compared as canonical source ids.
func FollowLinksRefusal(opted, inputs []string) error {
	for _, o := range opted {
		id, found := state.PathSourceID(o), false
		for _, in := range inputs {
			if state.PathSourceID(in) == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("-follow-links %s is not one of the inputs being archived: name a mail directory exactly as it is given as an -input", util.StripControl(o))
		}
	}
	return nil
}

// Result summarizes a completed (or cancelled) run.
type Result struct {
	Stats        export.Stats
	Files        int
	ManifestSize int
	// ManifestBefore is the manifest's size when the run began, so the summary
	// can tell how many of the run's exports were new messages and how many
	// replaced an earlier copy of the same key (F13).
	ManifestBefore int
	Indexed        int  // messages in the search index (0 if indexing disabled)
	IndexErrors    int  // messages the index refused (R8 parity broken; surfaced, not swallowed)
	IndexBehind    bool // an encrypted archive's indexed run ended with its sealed index still missing messages
	// Program is what an encrypted archive's program step did (program design R1).
	Program    ProgramOutcome
	Issues     int    // rows in the regenerated verification report
	ReportPath string // path to the verification report ("" when nothing to report)
	Fillable   int    // manifest records still missing content an on-demand source may deliver (a record that is only partly read is counted in Unreadable instead)
	Terminal   int    // records missing content the source can never deliver (recorded, never retried)
	Unknown    int    // legacy records not yet re-examined (migrated from a version-1 manifest)

	// Fault isolation (§4.2). Unreadable and Unparsed are the manifest's partly-read
	// and unparsed records; Gaps is what this run observed in the capture-gap
	// ledger; Partial says the run observed something it could not capture (a
	// fillable or terminal gap, or a partly-read record written or re-examined), so
	// it returns *PartialError (S1a: a failure, §4.9).
	Unreadable int
	Unparsed   int
	Gaps       GapSummary
	Partial    bool

	// Parse fidelity (design-mime-fidelity §4.9, MC19). Undecodable is the manifest's
	// records with a part this version cannot decode, kept as received (counted apart
	// from Terminal: a parse gap is not "source-empty"); FidelityFallback those written
	// from the earlier parser's content. Both are informational: neither makes a run
	// partial.
	Undecodable      int
	FidelityFallback int
	// FillableParseGap is how many of the Fillable records are so because of a fillable parse
	// gap (a part cut short, a multipart with no boundary), which is also counted in
	// Undecodable; FillableOnlyParseGap how many are fillable for nothing else. A download in the
	// mail app does not cure these in a mailbox file (friction F3).
	FillableParseGap     int
	FillableOnlyParseGap int

	// The gaps this run closed (§4.3): because their item was captured, and because a
	// completed walk of their folder no longer found them. The producers count them;
	// finish puts them in Gaps.
	gapsRecovered, gapsClosedGone int
	// encrypted is set by finish for a run that wrote an encrypted archive, which
	// has no attachments-report.tsv for a partial run's text to point at.
	encrypted bool
}

// finish fills the manifest-derived fields of a Result and regenerates the
// verification report (R1). When scaffold is true it also (re)writes the archive
// README; a Graph run passes false when no mailbox succeeded, so a run that
// captured nothing leaves no empty browsable scaffold behind. Shared by the
// local and Graph runners. The single operator-facing "Verification:" line is
// printed by the caller's summary (cmd/mailarchive printSummary), not here, so
// the run log carries it exactly once.
func finish(out string, r *Result, exp *export.Exporter, manifest *state.Manifest, idx *index.Index, indexErrors int, scaffold bool, logger *log.Logger) {
	// An encrypted archive (a keyed run, which prepareEncrypted proved against the
	// archive's vault) gets no layout README and no report: both describe, in
	// cleartext, what the units hide (phase2a §4). Its note is written by refreshNote
	// as the run opens, its digest sealed, and by the program step (program design R5):
	// never here, where it would not match the digest.
	enc := exp.Units != nil
	r.encrypted = enc
	if scaffold && !enc {
		writeArchiveReadme(out, logger)
	}
	r.Stats = exp.Stats
	r.ManifestSize = manifest.Len()
	r.Indexed = indexCount(idx) // commits any pending batch first
	// The failed Adds the caller counted, plus every row whose Add succeeded but
	// whose batch was lost afterwards (F49), so every message this run exported
	// has its row added or replaced in the index, or is counted. Indexed is the
	// index's total, so the index's growth plus IndexErrors == exported (on a
	// fresh archive, Indexed + IndexErrors == exported) only when the run
	// exports each key once and none is already indexed: a fill, a re-capture
	// of a legacy "unknown" record, a full-mode re-export, and — on a fresh
	// archive too — a key the same run exports twice (full mode over a source
	// holding a message twice, or an in-run fill of an earlier copy's gap) all
	// export a message again under the same key, and idx.Add replaces that
	// key's row instead of adding one.
	r.IndexErrors = indexErrors
	if idx != nil {
		r.IndexErrors += idx.Lost()
	}
	r.Fillable, r.Terminal, r.Unknown = manifest.Counts()
	// A partly-read record is fillable too, but it is not "missing content" to
	// download: it has its own count, its own line and its own remedy.
	r.Fillable -= manifest.UnreadableOnlyCount()
	r.Unreadable, r.Unparsed = manifest.UnreadableCount(), manifest.UnparsedCount()
	r.Undecodable, r.FidelityFallback = manifest.UndecodableCount(), manifest.FidelityFallbackCount()
	r.FillableParseGap, r.FillableOnlyParseGap = manifest.FillableParseGapCounts()
	if enc {
		// The report lists folders, subjects, attachment names and source ids; an
		// encrypted archive's gaps are shown by MailArchive from its manifest.
		r.ReportPath, r.Issues = "", 0
	} else {
		r.ReportPath, r.Issues = writeReport(out, manifest, manifest.Migrated > 0, logger)
	}
	// What this run observed in the capture-gap ledger (its own runAt is the stamp
	// every observation carries). The stuck count uses a daily period: `status`
	// judges it against the installed schedule's.
	r.Gaps = summarizeGaps(manifest, out, exp.RunAt, time.Now().UTC(), 24*time.Hour)
	r.Gaps.Recovered, r.Gaps.ClosedGone = r.gapsRecovered, r.gapsClosedGone
	r.Partial = r.partial()
}

// indexOpenError is the refusal of an export or a Graph run whose search.db
// at idxPath cannot be opened for writing (index.Open's err). Like reindex's,
// it names the file and the remedy its cause has (R12, X6), never SQLite's
// bare error:
//   - a busy index is in use, not damaged — another program kept it locked
//     past the writable open's wait — so the remedy is to retry, never the
//     rebuild (MA-398);
//   - a newer format's refusal already names the file and the upgrade, and
//     stands: a rebuild would replace a newer index with an older one;
//   - a file or folder SQLite cannot write (index.ReadOnly: a read-only
//     remount, changed permissions, a read-only search.db) or a disk that
//     refuses the write (index.DiskError: full, a file-size limit, a failing
//     drive) is not damage: the rebuild writes a new index to the same place
//     and fails the same way, so the remedy is the writable folder or the
//     free space (F1);
//   - anything else, a damaged file (SQLITE_NOTADB) above all, is rebuilt
//     from the archive's own files, which needs the archive's manifest
//     (Rebuild refuses without one). With no manifest in out there is nothing
//     to rebuild from, and nothing in that file a run needs, so the remedy is
//     to move it aside: the next run builds a fresh index.
func indexOpenError(idxPath, out string, err error) error {
	switch {
	case errors.Is(err, index.ErrNewerFormat):
		return err
	case index.Busy(err):
		return &IndexOpenError{Kind: IndexBusy, Out: out, Err: err,
			msg: fmt.Sprintf("search index %s is busy (%v): another program kept it locked — close any other program that has it open, then run again", idxPath, err)}
	case index.ReadOnly(err) || index.DiskError(err):
		remedy := unwritableRemedy(idxPath, out, err)
		return &IndexOpenError{Kind: IndexUnwritable, Out: out, Err: err, remedy: remedy,
			msg: fmt.Sprintf("search index %s cannot be written (%v): %s, then run again", idxPath, err, remedy)}
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, serr := os.Stat(mpath); errors.Is(serr, fs.ErrNotExist) {
		return &IndexOpenError{Kind: IndexDamagedNoManifest, Out: out, Err: err,
			msg: fmt.Sprintf("search index %s cannot be opened (%v), and there is no manifest (%s) to rebuild it from: move it out of %s, then run again", idxPath, err, mpath, out)}
	}
	return &IndexOpenError{Kind: IndexDamaged, Out: out, Err: err,
		msg: fmt.Sprintf("search index %s cannot be opened (%v): rebuild it from the archive with `mailarchive reindex -rebuild -out %q`, then run again", idxPath, err, out)}
}

// IndexOpenKind says which refusal an IndexOpenError is.
type IndexOpenKind int

const (
	IndexBusy              IndexOpenKind = iota // another program holds search.db
	IndexUnwritable                             // read-only file or folder, full or failing disk
	IndexDamaged                                // unreadable, a manifest to rebuild from
	IndexDamagedNoManifest                      // unreadable, nothing to rebuild from
)

// IndexOpenError is indexOpenError's refusal. Error() is the full text for the
// CLI and the log (SQLite's own words, the `mailarchive reindex` command);
// ForOwner is the same refusal in the words of an app whose owner has no
// command line (F3). It unwraps to SQLite's error.
type IndexOpenError struct {
	Kind   IndexOpenKind
	Out    string // the archive folder
	Err    error  // SQLite's error
	remedy string // IndexUnwritable: what to fix, in plain words
	msg    string
}

func (e *IndexOpenError) Error() string { return e.msg }
func (e *IndexOpenError) Unwrap() error { return e.Err }

// ForOwner renders the refusal for an app owner: the folder, the cause in plain
// terms, and the remedy that app offers — rebuildAction names its own rebuild
// control (for example "Maintenance → Rebuild search index") — with no command
// line and no SQLite text.
func (e *IndexOpenError) ForOwner(rebuildAction string) string {
	switch e.Kind {
	case IndexBusy:
		return fmt.Sprintf("The search index in %s is in use by another program. Close any other program that has it open, then try again.", e.Out)
	case IndexUnwritable:
		return fmt.Sprintf("Your archive in %s cannot be written to: %s, then try again.", e.Out, e.remedy)
	case IndexDamagedNoManifest:
		return fmt.Sprintf("The search index in %s is damaged, and the archive has no manifest to rebuild it from. Move search.db out of that folder, then try again — a fresh index is built.", e.Out)
	}
	return fmt.Sprintf("The search index in %s is damaged. Choose %s to rebuild it from your archive, then try again.", e.Out, rebuildAction)
}

// unwritableRemedy names what to fix when SQLite cannot write the index at
// dbPath in folder out (err is index.ReadOnly or index.DiskError): the disk
// (full or failing), search.db itself (opened read-only because the file is
// not writable), the folder (SQLite cannot create the -wal/-shm or the file
// there), or, when neither can be told, both.
func unwritableRemedy(dbPath, out string, err error) string {
	if index.DiskError(err) {
		return fmt.Sprintf("the disk holding %s is full or failing — free space on it (or check the drive)", out)
	}
	// search.db and the -wal/-shm pair a reader left beside it: SQLite creates
	// those with the file's own mode, so a read-only search.db that a status or
	// search read first leaves read-only sidecars, which keep refusing after
	// search.db alone is made writable (the re-verification of F7).
	var roFiles []string
	for _, s := range []string{"", "-wal", "-shm"} {
		if fileReadOnly(dbPath + s) {
			roFiles = append(roFiles, filepath.Base(dbPath)+s)
		}
	}
	dirRO := index.ReadOnlyDir(err)
	switch {
	case len(roFiles) == 1 && dirRO:
		return fmt.Sprintf("%s and the archive folder %s are read-only — make both writable (or remount the drive read-write)", roFiles[0], out)
	case len(roFiles) > 1 && dirRO:
		return fmt.Sprintf("%s and the archive folder %s are read-only — make all of them writable (or remount the drive read-write)", strings.Join(roFiles, ", "), out)
	case len(roFiles) == 1:
		return fmt.Sprintf("%s is read-only — make it writable (on Windows, clear its Read-only attribute)", roFiles[0])
	case len(roFiles) > 1:
		return fmt.Sprintf("%s are read-only — make them writable (on Windows, clear their Read-only attribute)", strings.Join(roFiles, ", "))
	case dirRO:
		return fmt.Sprintf("the archive folder %s is read-only — make it writable (or remount the drive read-write)", out)
	}
	return fmt.Sprintf("%s or the archive folder %s is not writable — make both writable", filepath.Base(dbPath), out)
}

// fileReadOnly reports whether the file at path exists and refuses to be
// opened for writing for lack of permission (a 0444 file, Windows' Read-only
// attribute). Opening it write-only and closing it changes nothing.
func fileReadOnly(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return errors.Is(err, fs.ErrPermission)
}

// exporterSetup is a seam for tests: set, it is called with the run's Exporter
// before the first message, so a test can inject a write fault through
// Exporter.BeforeWrite (a full disk, a vanished volume) without a real one. It is
// nil in production.
var exporterSetup func(*export.Exporter)

// ProgressFunc, if provided, is called after each processed message with the
// running stats. Keep it cheap; it runs inline with the export loop.
type ProgressFunc func(stats export.Stats)

// Run performs the export described by opts. If ctx is cancelled it stops
// promptly, saves the manifest, and returns the partial Result with
// context.Canceled.
func Run(ctx context.Context, opts Options, logger *log.Logger, onProgress ProgressFunc) (result Result, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	// A paused archive is hard read-only: refuse every content-mutating run before
	// any write, lock, or last-run record (archive-manager S2-C1/C2).
	if err := seal.RefuseWrite(opts.Out); err != nil {
		return Result{}, err
	}
	if err := RefuseUnfinishedCopy(opts.Out); err != nil {
		return Result{}, err
	}
	// An encrypted archive is written only with its key: without one, refuse
	// before discovery, any write, lock or record (phase2a §5). A scheduled run's
	// refusal is still recorded (A14, N13).
	if opts.Key == nil {
		if err := RefuseKeyless("archive into it", opts.Out, manifestDir(opts.Manifest)); err != nil {
			if opts.Unattended {
				RecordRefusedRun(opts, err)
			}
			return Result{}, err
		}
	}
	// A keyed run proves its key and target before discovery, any folder, lock or
	// record: a refused manual run writes nothing (P2a-C10). A scheduled one whose
	// archive's drive is not there is told so first, not that its key failed.
	if opts.Key != nil {
		if opts.Unattended {
			if err := RequireExistingOut(opts.Out); err != nil {
				return Result{}, err
			}
		}
		if err := preflightEncrypted(opts); err != nil {
			if opts.Unattended {
				RecordRefusedRun(opts, err)
			}
			return Result{}, err
		}
	}

	// Validate the inputs BEFORE creating anything (friction #2). A missing
	// -input path, or no sources at all, must refuse naming the problem while
	// creating no output dir, no lock, and no last-run record — a guaranteed
	// failure must not first leave a half-built archive and a failed run behind.
	// A scheduled run's refusal is still a run (N13): recorded failed when the
	// archive already exists, so it never leaves the previous success standing.
	files, err := DiscoverInputs(opts.Inputs, opts.Auto)
	if err == nil && len(files) == 0 {
		err = withKind(state.FailNoSources, errors.New("no input mail sources found (a .pst/.ost file, an mbox file, or a mail directory; or enable auto-discovery)"))
	} else if err != nil {
		err = withKind(state.FailSourceUnreadable, err) // a gone -input is named by its own type first
	}
	if err != nil {
		if opts.Unattended {
			RecordRefusedRun(opts, err)
		}
		return Result{}, err
	}

	// PM4: a SCHEDULED run whose archive -out is gone (an unmounted backup drive) must refuse,
	// never recreate the archive on the local disk and silently back up into a decoy.
	if opts.Unattended {
		if err := RequireExistingOut(opts.Out); err != nil {
			return Result{}, err
		}
	}
	// Where -copy-first snapshots go (an encrypted archive's are outside it, P2a-C12),
	// and — for an encrypted archive — whether its largest one fits, before anything
	// is written.
	snapDir, err := snapshotDir(opts)
	if err == nil && snapDir != opts.Out {
		err = checkScratchSpace(snapDir, files)
	}
	if err != nil {
		if opts.Unattended {
			RecordRefusedRun(opts, err)
		}
		return Result{}, err
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, withKind(state.FailArchiveUnwritable, fmt.Errorf("create output dir: %w", err))
	}
	// One run per archive at a time (R5): a scheduled run overlapping a manual
	// one would otherwise interleave manifest/index/file writes.
	lockPath := filepath.Join(opts.Out, lockfile.Name)
	lock, err := lockfile.AcquireAs(lockPath, lockVerb(opts, "export"))
	if err != nil {
		return Result{}, withKind(state.FailArchiveUnwritable, busyRefusal(lockPath, err)) // a held lock is named by ErrHeld first
	}
	defer lock.Release()
	// Again under the lock: an encryption that finished while this run waited
	// (a conversion's swap) is not a plaintext archive either (P2a-C9).
	if opts.Key == nil {
		if err := RefuseKeyless("archive into it", opts.Out, manifestDir(opts.Manifest)); err != nil {
			return Result{}, err
		}
	}
	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	// A keyed run's manifest and unit checks run here, before its "running" record:
	// a refused manual run writes nothing (P2a-C10), a scheduled one only its
	// minimal failed run (A14).
	var manifest *state.Manifest
	var units *export.Units
	var sealed *container.Store
	var download *programDownload
	encStart := time.Now().UTC()
	if opts.Key != nil {
		if manifest, units, sealed, err = openEncrypted(opts, lock.StillHeld, encStart, logger, nil); err != nil {
			if opts.Unattended {
				writeRefusedRun(opts, err)
			}
			return Result{}, err
		}
		// The program copies' download starts now, outside the step that waits for it
		// (program design §5, R17).
		download = startProgramDownload(opts.downloadContext(ctx), sealed)
		// A backup into an archive whose last check found damage says so at its end
		// (P3-8 friction, B19).
		defer func() {
			if err == nil {
				tellStandingDamage(opts, logger)
			}
		}()
	}
	// Every run that begins is recorded (R18): "running" now, before any early
	// return, finalized on the way out — so a run that never finishes is
	// visible as such to `status`. The record's start is the run's own timestamp,
	// the stamp every capture-gap observation carries: a gap whose LastRun equals
	// it was attempted by this run, and `status` tells attempted from not attempted
	// by comparing with the last-run record's start (FC14).
	begun := beginRun(opts)
	// The opt-in Thunderbird pre-sync runs here, after the lock and the run's
	// start and before any source is read; nil unless a caller opts in, and it
	// never fails the run (design thunderbird-presync §4.1). Its warnings join
	// this run's record before the deferred finalizer captures it.
	var presyncRelease func()
	if opts.BeforeRead != nil {
		// A panic in the pre-sync session is recovered here, so it can never fail
		// the archive (R32, design §4.9.1): the run records presync_internal_error
		// and reads the sources anyway.
		func() {
			defer func() {
				if r := recover(); r != nil {
					begun.Warnings = append(begun.Warnings, state.RunWarning{Code: "presync_internal_error", Message: "the Thunderbird pre-sync failed unexpectedly; the archive ran anyway"})
					logger.Printf("warning: the Thunderbird pre-sync panicked and was recovered (%v); the archive ran anyway", r)
				}
			}()
			br := opts.BeforeRead(ctx, files)
			begun.Warnings = append(begun.Warnings, br.Warnings...)
			presyncRelease = br.Release
		}()
	}
	// C2: release the pre-sync's per-profile lock AFTER the sources are read. The defer
	// runs at Run's return (after the read, index and pages), always — even on error or
	// panic — so a second scheduled run cannot pre-sync and compact this profile while
	// this run reads it, and the lock can never leak the run.
	if presyncRelease != nil {
		defer presyncRelease()
	}
	defer recordRun(opts, begun, &result, &err, logger)
	runAt := begun.Started

	// On -auto, warn up front when a live Exchange/IMAP .ost is about to be read
	// by the direct go-pst reader, which cannot read every .ost — the -outlook
	// remedy should not wait for a failure to appear (friction #1).
	if opts.Auto {
		_, classicOutlook := outlookcom.Detect()
		if adv := ostAdvisory(runtime.GOOS, files, classicOutlook); adv != "" {
			logger.Printf("%s", adv)
		}
	}

	// What a crashed earlier run may have left behind (R5): stale temps and
	// orphan zips. Only files older than this run's start are touched.
	if n := export.SweepOrphans(opts.Out, time.Now(), logger, programSweepSkip(opts)...); n > 0 {
		logger.Printf("swept %d orphaned temp/zip file(s) from an interrupted run", n)
	}
	// An encrypted archive's snapshots live in its scratch folder: a crashed run's are
	// reaped there, and the folder is removed (when empty) on every exit.
	if snapDir != opts.Out {
		if err := os.MkdirAll(snapDir, 0o700); err != nil {
			return Result{}, scratchFatalFor(snapDir, err)
		}
		defer func() { // each only when empty: an Outlook export may share the archive's folder
			_ = os.Remove(snapDir)
			_ = os.Remove(filepath.Dir(snapDir))
		}()
		if n := export.SweepTemps(snapDir, time.Now(), logger); n > 0 {
			logger.Printf("swept %d leftover working cop(ies) from an interrupted run", n)
		}
	}

	if manifest == nil {
		if manifest, err = state.Load(mpath); err != nil {
			return Result{}, withKind(state.FailManifestUnreadable, err)
		}
	}
	manifestBefore := manifest.Len()

	exp := &export.Exporter{
		OutDir:   opts.Out,
		Manifest: manifest,
		Mode:     opts.Mode,
		Since:    opts.Since,
		Log:      logger,
		KeepRaw:  opts.KeepRaw,
		RunAt:    runAt,
		Units:    units,
	}

	if exporterSetup != nil {
		exporterSetup(exp)
	}

	// One-time upgrade logs BEFORE the index repair, so cause (the manifest
	// re-scope) precedes effect (the index-key migration) — matching reindex and
	// the README sample (friction #17).
	if manifest.StoresMigrated > 0 {
		logger.Printf("canonicalized %d store path%s (one-time upgrade)",
			manifest.StoresMigrated, plural(manifest.StoresMigrated, "", "s"))
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}
	// An older build's collapsed "(unreadable message)" record stands for no message
	// in particular: it becomes the info label legacy-placeholder, once, here and
	// only in a run — `status` never writes (FC27). Its page stays; the items it
	// stood for are now listed one by one when the source is read again.
	if n := manifest.ReclassLegacyPlaceholders(model.LegacyStubIdentity()); n > 0 {
		logger.Printf("re-classed %d legacy placeholder(s); this version records each unreadable message separately", n)
	}
	// What -outlook's export skipped is this run's observation: recorded before the
	// walk, under the token of the PST it belongs to.
	recordOutlookSkips(manifest, opts.OutlookSkips, runAt, logger)

	// Optional search index, fed as each message is written.
	var idx *index.Index
	var si *sealedIndex
	var indexErrors int
	if !opts.Index && sealed != nil && sealed.HasIndex() {
		// Records this run commits are not added to the sealed index (P2b-C16).
		sealed.MarkIndexBehind()
	}
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		if sealed != nil {
			if si, err = openSealedIndex(sealed, opts.Out, manifest.Len(), false, logger); err == nil && si != nil {
				idx = si.idx
			}
		} else {
			idx, err = index.Open(idxPath)
		}
		if err != nil {
			return Result{}, withKind(state.FailIndex, indexOpenError(idxPath, opts.Out, err))
		}
	}
	if idx != nil { // nil for a sealed index with no room to work on it: marked behind
		defer closeRunIndex(idx, si != nil, logger)
		// Repair legacy index keys to match the re-scoped manifest, driven by
		// the manifest's own re-key signal (force) — the only trigger that
		// survives an old-binary excursion — or the index's own version (F2).
		if _, rkErr := idx.RepairKeys(manifest.Rekeyed > 0, state.MigrateKey, logger); rkErr != nil {
			if si == nil {
				return Result{}, withKind(state.FailIndex, fmt.Errorf("migrate search index keys: %w", rkErr))
			}
			si.drop(sealed, rkErr, logger) // a sealed index never stops the backup
			idx, si = nil, nil
		}
	}
	if idx != nil {
		// A sealed index marked behind — or none at all beside records (indexing turned
		// on for an archive that never had it) — is caught up from the sealed units
		// before the walk; the run's commits seal it (phase2b §6, P2b-C16).
		if sealed != nil && (sealed.IndexBehind() || (!sealed.HasIndex() && manifest.Len() > 0)) {
			si.catchUp(manifest, units, sealed, logger)
		}
		warns := newIndexWarnings(logger)
		defer warns.flush()
		exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
			if addErr := indexExported(idx, si, sealed, logger, store, folderPath, m, relPath, key); addErr != nil {
				indexErrors++
				warns.add(addErr)
			}
		}
	}
	if manifest.Migrated > 0 {
		logger.Printf("%d manifest entr%s predate completeness tracking; they will be re-examined by this and following incremental runs",
			manifest.Migrated, plural(manifest.Migrated, "y", "ies"))
	}

	// Checkpoint inside a store walk so a hard crash keeps the progress made
	// (R5) — not only at store boundaries, where a single huge .pst would
	// otherwise leave hours of work unrecorded. The cadence scales with the
	// archive size (effectiveEvery) so a small archive checkpoints often and a
	// huge one does not pay a full save every configured-N messages.
	floor := opts.CheckpointEvery
	if floor <= 0 {
		floor = defaultCheckpointEvery
	}
	lastCheckpoint := 0
	var lockLost error
	// saveErr is the outcome of the most recent manifest save: a failed
	// mid-walk checkpoint is healed by a later save that succeeds, but a run
	// whose LAST save failed has kept no progress and must fail (F53).
	var saveErr error
	// The local run's own state: what it records of faults and closes of gaps. A
	// fault counts as processed for the checkpoint gate below, so a stretch of
	// nothing but faults still saves and still re-checks the lock (FC12).
	lr := &localRun{exp: exp, out: opts.Out, snapDir: snapDir, runAt: runAt, logger: logger, lock: lock}
	for _, p := range opts.OutlookExported {
		if lr.outlookFresh == nil {
			lr.outlookFresh = map[string]bool{}
		}
		lr.outlookFresh[state.PathSourceID(p)] = true
	}
	checkpoint := func(stats export.Stats) {
		processed := stats.Exported + lr.faults
		if processed-lastCheckpoint < effectiveEvery(floor, manifest.Len()) {
			return
		}
		lastCheckpoint = processed
		// The lock file is the run's guarantee of exclusivity; if it was removed
		// or replaced under us, another run may now hold a fresh one. Stop
		// rather than race it (R5).
		if err := lock.StillHeld(); err != nil && lockLost == nil {
			lockLost = err
			return
		}
		// A one-shot local import keeps no go-back timeline (its dedup is
		// folder-scoped, §3.6), so there is no history writer to sync here.
		saveErr = commit(nil, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)
	}

	lr.onFault = func() { checkpoint(exp.Stats) }
	lr.onLockLost = func(err error) {
		if lockLost == nil {
			lockLost = err
		}
	}
	result = Result{Files: len(files), ManifestBefore: manifestBefore}
	var failures int
	var firstFailed string
	var firstErr error
	var fatal *RunFatalError        // a run-level fault of the archive itself: it ends the run
	var stopped *SourceStoppedError // the first source the local breaker ended: the run goes on without it
	for _, f := range files {
		runErr := runFile(ctx, lr, f, opts.CopyFirst, followsLinks(opts.FollowLinks, f), func(stats export.Stats) {
			checkpoint(stats)
			if onProgress != nil {
				onProgress(stats)
			}
		}, func() error { return lockLost })
		if lockLost != nil {
			// The lock was removed or replaced mid-run: another run may now own
			// this archive. Do NOT write shared state (manifest/index/README) into
			// it — the deferred recordRun marks this run failed, and the message
			// files already written are reconciled by the next incremental run
			// (index-before-manifest ordering leaves no orphaned manifest rows).
			// (INT-CC-2/R5.)
			return result, withKind(state.FailLockLost, lockLost)
		}
		saveErr = commit(nil, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)

		if errors.Is(runErr, context.Canceled) {
			result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			if saveErr != nil {
				// "progress was kept" would be false: record it failed.
				return result, manifestSaveFailure(mpath, saveErr)
			}
			return result, context.Canceled
		}
		if errors.As(runErr, &fatal) {
			// The archive's location or volume can no longer be written: the next source
			// would fail the same way, so the run stops here, with what it kept (§4.6).
			logger.Printf("error: %s: %v", f, fatal)
			break
		}
		var stop *SourceStoppedError
		if errors.As(runErr, &stop) {
			// The local breaker ended this source: the next one is read (S2A-AC3).
			logger.Printf("error: %s: %v", f, stop)
			if stopped == nil {
				stopped = stop
			}
			continue
		}
		if runErr != nil {
			failures++
			if failures == 1 {
				firstFailed, firstErr = f, runErr
			}
			logger.Printf("error: %s: %v%s", f, runErr, dataFileRemedyFor(f, runErr))
		}
	}

	// Browsable folder index pages, built from the finished index. Regenerate
	// them only when this run exported something or the root index.html is not
	// yet there (nas-02): a zero-change re-run must not rewrite every page.
	// An encrypted archive has no folder pages: they are its folder tree, subjects and
	// senders in cleartext (phase2a §3); MailArchive's reader renders them (Phase 3).
	if gaps := gapBannerCount(manifest); exp.Units == nil && idx != nil && opts.Pages && pagesNeeded(opts.Out, exp.Stats.Exported, gaps) {
		if pErr := pages.Generate(opts.Out, idx, logger, gaps); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}

	if hErr := healLateDamage(si, sealed, manifest, units, logger); hErr != nil && saveErr == nil {
		saveErr = hErr
	}
	result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
	finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
	if sealed != nil {
		// No index write may follow the run's last seal (phase2b §6).
		if gErr := guardLateIndexWrite(si, sealed, manifest, logger); gErr != nil && saveErr == nil {
			saveErr = gErr
		}
		result.IndexBehind = opts.Index && sealed.IndexBehind()
		// The program copies, under the lock, after the last save (program design R1).
		if fatal == nil && lockLost == nil && saveErr == nil && ctx.Err() == nil {
			result.Program = programStep(programEnv{ctx: ctx, out: opts.Out, store: sealed, save: manifest.Save,
				stillHeld: lock.StillHeld, logger: logger, runStart: encStart, download: download,
				skip: opts.ProgramSkip, onWait: opts.OnProgramWait})
		}
	}
	// What ended the run or its sources, most serious first: the archive's own fault, a
	// source the breaker stopped (with the files that failed to open, if any), the
	// files that failed to open.
	var end error
	switch {
	case fatal != nil:
		end = fatal
	case stopped != nil && failures > 0:
		end = fmt.Errorf("%w; %w", stopped, fileFailureError(failures, firstFailed, firstErr))
	case stopped != nil:
		end = stopped
	case failures > 0:
		end = fileFailureError(failures, firstFailed, firstErr)
	}
	if saveErr != nil {
		if end != nil {
			return result, fmt.Errorf("%w; %w", end, manifestSaveFailure(mpath, saveErr))
		}
		return result, manifestSaveFailure(mpath, saveErr)
	}
	if end != nil {
		return result, end
	}
	return result, partialOutcome(result)
}

// manifestSaveFailure is the run error when the run's final manifest save
// failed (F53): the manifest is the run's whole persisted result, so nothing
// this run did is recorded and the next run repeats it. It names the file and
// the remedy; recordRun finalizes the last-run record failed with it, so
// `status` reports RED instead of a green "ok".
// stopOnLostLock ends the run as a lost lock when a sealed save found the lock gone
// at its commit point (container.ErrLockLost), as a checkpoint's own check does —
// so no more is written into an archive another program may now own (CON-3).
func stopOnLostLock(saveErr error, lockLost *error) {
	if errors.Is(saveErr, container.ErrLockLost) && *lockLost == nil {
		*lockLost = saveErr
	}
}

func manifestSaveFailure(mpath string, err error) error {
	if errors.Is(err, container.ErrInventoryWrite) {
		// The record itself was saved: only its file list was not (P2b-C7).
		return withKind(state.FailInventory, fmt.Errorf("%w: %s", err, mpath))
	}
	if errors.Is(err, container.ErrLockLost) {
		// The sealed save re-proves the lock right before its commit point: another
		// program took the archive, not an unwritable record (CON-3).
		return withKind(state.FailLockLost, fmt.Errorf("%w: %s", err, mpath))
	}
	return withKind(state.FailManifestSave, fmt.Errorf("could not save the manifest %s (%w): this run's progress was not recorded and the next run will repeat it — make sure the manifest file and its folder are writable (on Windows, that no other program holds the manifest open), then run again", mpath, err))
}

// defaultCheckpointEvery aligns with the index's own batch size.
const defaultCheckpointEvery = 1000

// effectiveEvery is the checkpoint cadence for an archive currently holding n
// records: roughly an eighth of the archive, clamped to at least floor (the
// configured CheckpointEvery, default 1000) and at most 25000. A small archive
// checkpoints every floor messages; a very large one caps the save frequency so
// the durability writes never dominate a long run (nas-03).
func effectiveEvery(floor, n int) int {
	e := n / 8
	if e < floor {
		e = floor
	}
	if e > 25000 {
		e = 25000
	}
	return e
}

// pagesNeeded reports whether the browsable folder pages should be regenerated:
// only when this run exported at least one message, the root index.html does
// not yet exist, or the gap count it was stamped with is not the manifest's now
// (a ledger-only change must reach the banner, FC24). A zero-change re-run then
// leaves the pages (and their mtimes) untouched (nas-02). reindex regenerates
// unconditionally — it reconciles the pages to what survives on disk.
func pagesNeeded(out string, exported, gaps int) bool {
	if exported > 0 {
		return true
	}
	stamped, ok := pages.StampedGaps(out)
	return !ok || stamped != gaps
}

// ostAdvisory returns the up-front guidance to print before an -auto run reads
// its inputs, when a live Exchange/IMAP Outlook .ost is about to be routed into
// the direct go-pst reader (which cannot read every .ost) and the classic
// Outlook that could export a clean PST is present. It is a pure function of the
// OS, the discovered paths, and whether classic Outlook was detected, so the
// decision is unit-tested without a Windows host. Empty when it does not apply
// (not Windows, no .ost among the inputs, or no classic Outlook to run -outlook).
func ostAdvisory(goos string, paths []string, classicOutlook bool) string {
	if goos != "windows" || !classicOutlook {
		return ""
	}
	hasOST := false
	for _, p := range paths {
		if strings.EqualFold(filepath.Ext(p), ".ost") {
			hasOST = true
			break
		}
	}
	if !hasOST {
		return ""
	}
	return "Note: a live Exchange/IMAP Outlook .ost was auto-discovered. Some .ost caches " +
		"cannot be read directly; if this run reports read errors or missing mail, re-run " +
		"with -outlook to have classic Outlook export a clean PST first."
}

// commit makes progress durable in the crash-safe order (§3.5): the history log
// is appended+fsync'd FIRST, THEN the index is flushed, THEN the manifest is
// saved as the trailing anchor. So the manifest — the fold-to-now projection —
// never advances past the history events that explain it: after a crash the
// history is a superset of the manifest and the index is a superset too, and the
// next run re-observes the tail idempotently (a duplicate history event folds
// away; idx.Add replaces by key). A move is therefore never lost. hist and idx
// may be nil (a one-shot local run writes no timeline; indexing may be off). It
// returns the manifest save's error, so a run whose final save failed fails.
func commit(hist state.Timeline, idx *index.Index, manifest *state.Manifest, logger *log.Logger) error {
	var syncHistory, flushIndex, saveManifest func() error
	if hist != nil {
		syncHistory = hist.Sync
	}
	if idx != nil {
		flushIndex = idx.Flush
	}
	if manifest != nil {
		saveManifest = manifest.Save
	}
	return commitInOrder(syncHistory, flushIndex, saveManifest, logger)
}

// commitInOrder is the single definition of the crash-safe durability order
// (history → index → manifest, §3.5); commit wires the real stores to it and a
// test drives it with recording doubles to prove the order holds. A nil step is
// skipped. Each error is logged and does not abort the later steps: the
// invariant here is only about ORDER (the manifest is written last), never that
// every step succeeds. The manifest save's error is also returned: a mid-walk
// checkpoint may fail and be healed by a later save, but the caller fails a run
// whose last save failed (F53).
func commitInOrder(syncHistory, flushIndex, saveManifest func() error, logger *log.Logger) error {
	if syncHistory != nil {
		if err := syncHistory(); err != nil {
			logger.Printf("warning: history sync: %v", err)
		}
	}
	if flushIndex != nil {
		if err := flushIndex(); err != nil {
			logger.Printf("warning: index flush: %v", err)
		}
	}
	if saveManifest != nil {
		if err := saveManifest(); err != nil {
			logger.Printf("warning: could not save manifest: %v", err)
			return err
		}
	}
	return nil
}

// beginRun writes the "running" last-run record and returns it for recordRun.
func beginRun(opts Options) state.LastRun {
	lr := newRunRecord(opts)
	_ = state.WriteLastRun(opts.Out, lr) // best effort: the run itself must not fail on this
	return lr
}

// newRunRecord is the "running" last-run record for a run of opts, starting now. The
// record of an encrypted archive — a keyed run, or any run whose out is one — is
// marked Encrypted and carries nothing beyond its allow-list (P2a-C16): no program
// path, job, mode or warnings.
func newRunRecord(opts Options) state.LastRun {
	if opts.Key != nil || encryptedAt(opts.Out) {
		// The program copies' line the last run left, kept until this run's step says
		// another (the PG4 pass, LEAK-3).
		prev, st, _ := state.ReadLastRun(opts.Out)
		program := ""
		if st == state.LastRunPresent {
			program = prev.Program
		}
		return state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), Encrypted: true, Program: program}
	}
	exe, _ := os.Executable()
	mode := "incremental"
	if opts.Mode == export.Full {
		mode = "full"
	}
	return state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), PID: os.Getpid(), Exe: exe, Mode: mode, Job: canonicalJob(opts, mode), NoIndex: !opts.Index, Warnings: opts.Warnings}
}

// RecordRefusedRun records a scheduled run that refused before it began — its
// -input vanished, or discovery found no sources — as a FAILED run of the
// archive at opts.Out (N13, R18): the last-run record names the cause and the
// attention sidecar is written, so `status` reports RED run_failed instead of
// the previous success. It writes only into an archive that already exists (a
// manifest or a last-run record is present): a first run that refuses still
// creates nothing (friction #2, MA-103). It takes the archive lock for the
// write and skips it when another run holds the lock — that run's own record
// is authoritative. Best effort, like every last-run write.
func RecordRefusedRun(opts Options, cause error) {
	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	_, mErr := os.Stat(mpath)
	_, lrErr := os.Stat(filepath.Join(opts.Out, state.LastRunName))
	if mErr != nil && lrErr != nil {
		return
	}
	// An out inside an encrypted archive is never written: its record would break
	// the outer archive's allow-listed tree (P2a-C8). Nor is a paused one: a paused
	// archive takes no write, no lock and no record (S2-C1, F21).
	if insideEncrypted(opts.Out) || seal.IsSealed(opts.Out) {
		return
	}
	lock, err := lockfile.AcquireAs(filepath.Join(opts.Out, lockfile.Name), lockVerb(opts, "export"))
	if err != nil {
		return
	}
	defer lock.Release()
	writeRefusedRun(opts, cause)
}

// writeRefusedRun writes RecordRefusedRun's failed record and history entry; the
// caller holds the archive lock. An encrypted archive's record carries the failure
// kind, never the error text (P2a-C16).
func writeRefusedRun(opts Options, cause error) {
	lr := newRunRecord(opts)
	lr.Status, lr.Finished, lr.Error = state.RunFailed, lr.Started, cause.Error()
	if lr.Encrypted {
		lr.Error, lr.ErrorKind = "", FailureKind(cause)
	}
	_ = state.WriteLastRun(opts.Out, lr)
	// A refused scheduled run is a failure the history should show too (MA-701), best effort.
	_ = appendRunHistory(opts.Out, state.RunHistoryEntryFrom(lr))
}

// lockVerb is the verb the archive lock records for a run of opts: none for an
// encrypted archive, whose lock must not say what kind of source it is backed up
// from (only host= is disclosed, A18).
func lockVerb(opts Options, verb string) string {
	if opts.Key != nil || encryptedAt(opts.Out) {
		return ""
	}
	return verb
}

// insideEncrypted reports whether a folder ABOVE out is an encrypted archive — out
// as given, or out with its links resolved (a link into an encrypted archive).
func insideEncrypted(out string) bool {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = filepath.Clean(out)
	}
	for _, p := range []string{abs, util.ResolveExisting(abs)} {
		if parent := filepath.Dir(p); parent != p {
			if dir, _, _ := encmark.AtOrAbove(parent); dir != "" {
				return true
			}
		}
	}
	return false
}

// canonicalJob reconstructs the run's export flags in the same shape the
// scheduler stores them, so `status` can offer a "schedule the same job"
// remedy. It returns nil when there is no determinable local export job — a
// Graph run (shared beginRun, no inputs) or a bare run — so status falls back
// to a generic phrase rather than inventing a wrong command.
func canonicalJob(opts Options, mode string) []string {
	if len(opts.Inputs) == 0 && !opts.Auto {
		return nil
	}
	job := []string{"-out", absOrSame(opts.Out), "-mode", mode}
	if opts.Auto {
		job = append(job, "-auto")
	}
	for _, in := range opts.Inputs {
		job = append(job, "-input", absOrSame(in))
	}
	if opts.CopyFirst {
		job = append(job, "-copy-first")
	}
	for _, in := range opts.FollowLinks {
		job = append(job, "-follow-links", absOrSame(in))
	}
	if opts.KeepRaw {
		job = append(job, "-raw")
	}
	return job
}

// absOrSame returns p made absolute (so a recorded job is pasteable from any
// directory), or p itself when it cannot be resolved.
func absOrSame(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// recordRun finalizes the last-run record from the run's outcome.
// writeLastRun is the finalizing record write; a var so a test can refuse it.
var writeLastRun = state.WriteLastRun

// appendRunHistory adds a finalized run to the archive's rolling history (MA-701); a var
// so a test can observe or refuse it. Best effort — a lost history never fails a run.
var appendRunHistory = state.AppendRunHistory

func recordRun(opts Options, lr state.LastRun, result *Result, err *error, logger *log.Logger) {
	lr.Finished = time.Now().UTC()
	if lr.Encrypted {
		// When the step did not run (an early end, a cancel), the copies are as the last
		// run that ran it left them, and so is the line newRunRecord carried (LEAK-3).
		if result.Program.Class != "" {
			lr.Program = programCode(result.Program.Class) // program design §4, §8
		}
	}
	switch {
	case *err == nil:
		lr.Status = state.RunOK
		if lr.Encrypted && result.IndexBehind {
			// The backup finished, but its search index misses messages until a later
			// run catches it up: recorded by kind, so status warns (owner ruling
			// 2026-10-25, "keep and warn").
			lr.ErrorKind = state.NoteIndexBehind
		}
	case errors.Is(*err, context.Canceled):
		lr.Status = state.RunCancelled
	default:
		lr.Status = state.RunFailed
		lr.Error = (*err).Error()
		lr.ErrorKind = errorKindOf(*err)
		if lr.Encrypted {
			// An encrypted archive records the failure kind, never its text (P2a-C16).
			lr.Error, lr.ErrorKind = "", FailureKind(*err)
		}
	}
	lr.Exported, lr.Filled = result.Stats.Exported, result.Stats.Filled
	lr.Fillable, lr.Terminal, lr.Unknown, lr.IndexErrors = result.Fillable, result.Terminal, result.Unknown, result.IndexErrors
	// What this run observed in the capture-gap ledger (additive; a monitor keys on
	// gaps_new, FC23(2)). A partial run is recorded as the failure it maps to in
	// S1a: status "failed" with the *PartialError's one-line text.
	g := result.Gaps
	lr.GapsObserved, lr.GapsNew, lr.GapsRecovered = g.Observed, g.New, g.Recovered
	lr.GapsFillable, lr.GapsTerminal, lr.GapsStuck, lr.Unreadable = g.Fillable, g.Terminal, g.Stuck, result.Stats.Unreadable
	recordCounts(&lr, *result)
	// Best effort: a refused write never fails the run, but it is logged (F12),
	// because the "running" record then stays and status reads it as a run that
	// never finished although this log says how it ended.
	if werr := writeLastRun(opts.Out, lr); werr != nil && logger != nil {
		// This line comes before the run's own Done/FAILED line (recordRun runs
		// from a defer), so it must not refer to lines "above".
		hint := ""
		if runtime.GOOS == "windows" {
			hint = " (a sharing violation: exclude the archive folder from antivirus real-time scanning)"
		}
		logger.Printf("warning: could not record this run in %s (%v): `mailarchive status` may show this run as \"never finished\" or still show the previous result, whatever this log says about how it ended — run again once the file can be written%s", filepath.Join(opts.Out, state.LastRunName), werr, hint)
	}
	// Add this run to the archive's rolling history (MA-701), best effort like the
	// last-run write above: the dashboard and `status` read it to show recent runs, not
	// just the latest. A lost history never fails the run.
	if herr := appendRunHistory(opts.Out, state.RunHistoryEntryFrom(lr)); herr != nil && logger != nil {
		logger.Printf("warning: could not add this run to the backup history in %s (%v)", filepath.Join(opts.Out, state.RunHistoryName), herr)
	}
	// Track how long this archive's SOURCE has been reachable (Slice 3, §5.7): only SCHEDULED
	// (unattended) runs move the streak, and only a definitive reach or unreachable verdict
	// moves it — a non-source failure leaves it. Best effort, like the last-run write above.
	if opts.Unattended {
		updateSourceStreak(opts.Out, *err, lr.Finished, lr.Encrypted, logger)
	}
}

// recordCounts puts a run's parse-fidelity counts in its last-run record, the numbers
// the summary and `status` show (X6).
func recordCounts(lr *state.LastRun, r Result) {
	lr.Undecodable, lr.FidelityFallback = r.Undecodable, r.FidelityFallback
}

// errorKindOf names the search-index refusal err is (state.ErrKind*), or ""
// for any other failure, for the last-run record: an owner surface's health
// view renders the refusal in its own words from the kind.
func errorKindOf(err error) string {
	var outage *outageError
	if errors.As(err, &outage) {
		return state.ErrKindServiceUnavailable
	}
	var ioe *IndexOpenError
	if !errors.As(err, &ioe) {
		return ""
	}
	switch ioe.Kind {
	case IndexBusy:
		return state.ErrKindIndexBusy
	case IndexUnwritable:
		return state.ErrKindIndexUnwritable
	case IndexDamagedNoManifest:
		return state.ErrKindIndexDamagedNoManifest
	}
	return state.ErrKindIndexDamaged
}

// RequireExistingOut is the unattended-run guard (PM4): a SCHEDULED run whose archive -out is
// not present (the backup drive is not mounted, the share is down) must not quietly create a
// new archive on the local disk and report success. app.Run and app.RunGraph call it before
// any MkdirAll when opts.Unattended; the CLI verbs guard with the same check before the engine.
func RequireExistingOut(out string) error {
	if fi, err := os.Stat(out); err != nil || !fi.IsDir() {
		return withKind(state.FailArchiveAbsent, fmt.Errorf("archive directory %s is not present — is the backup drive mounted? refusing to create a new archive elsewhere (unattended run)", out))
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func indexCount(idx *index.Index) int {
	if idx == nil {
		return 0
	}
	n, err := idx.Count()
	if err != nil {
		return 0
	}
	return n
}

// ostOpenRemedy returns guidance to append when a source fails to open/parse and
// the input is an Outlook `.ost`: go-pst cannot read every live Cached-Exchange
// `.ost` (invariant R16 / scenario S19), so a bare "corrupt or unsupported" error
// strands a CLI user with no next step (the GUI already steers here). Point the
// operator at the reliable paths — Outlook's own export on Windows, or the Graph
// device path for a Microsoft 365 mailbox. Pure function of the path so a test can
// assert the wording without a real `.ost`; empty for any non-`.ost` source.
func ostOpenRemedy(path string) string {
	if !strings.EqualFold(filepath.Ext(path), ".ost") {
		return ""
	}
	return " — a live Exchange / Microsoft 365 .ost often cannot be read directly;" +
		" on Windows run `mailarchive -outlook` (Outlook exports a clean .pst first)," +
		" or archive a Microsoft 365 mailbox server-side with `mailarchive graph -auth device`"
}

// dataFileRemedy is the guidance appended to the run log's error line when a
// data file fails to open or read: a `.ost` gets ostOpenRemedy; a damaged `.pst`
// gets the in-place repair — repair it with Outlook's Inbox Repair Tool, or put
// the repaired copy at the SAME path, because the store's token is seeded from
// the input path and a copy archived from another path starts a separate archive
// tree (fault-isolation §4.4). Any other source gets none.
func dataFileRemedy(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ost":
		return ostOpenRemedy(path)
	case ".pst":
		return " — a damaged .pst can usually be repaired: " + state.PSTRepairRemedy
	}
	return ""
}

// dataFileRemedyFor is dataFileRemedy keyed by the cause (friction F1): the
// in-place repair is for a file that opens and does not parse. A file that cannot
// be opened at all (permissions, another program holding it, a wrong path, a disk
// error) is told what to do about THAT, because a repair tool cannot help it, and
// neither can a change of route (an .ost held by Outlook or locked is no more
// readable through -outlook; FI-S1a re-verification, OP-5). An .ost whose cause is a parse or
// format error keeps ostOpenRemedy and any other source gets none.
func dataFileRemedyFor(path string, err error) string {
	ext := strings.ToLower(filepath.Ext(path))
	if (ext != ".pst" && ext != ".ost") || err == nil {
		return dataFileRemedy(path)
	}
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, fs.ErrPermission), state.GapErrnoClass(text) == state.ErrnoPerm:
		return " — the file could not be opened because of its permissions: fix the permissions on the file (and that your account may read its folder), then run again"
	case strings.Contains(text, "used by another process"), strings.Contains(text, "sharing violation"), strings.Contains(text, "text file busy"):
		return " — another program has the file open: close Outlook (or whichever program is using it), then run again"
	case errors.Is(err, fs.ErrNotExist), strings.Contains(text, "no such file"), strings.Contains(text, "cannot find the"):
		return " — the file was not found: check the path (and that the drive or share is connected), then run again"
	case state.GapErrnoClass(text) == state.ErrnoIO:
		return " — the disk returned a read error: check the drive the file is on, then run again"
	}
	return dataFileRemedy(path)
}

// shortFileRemedy is dataFileRemedyFor for the run's returned error, which the
// run log prints twice more (the FAILED line and the final error): the two long
// generic remedies (the .pst repair with its path rule, the .ost routes) shrink to
// one clause, and the full text stays on the per-file `error:` line. The
// cause-specific remedies are already one clause.
func shortFileRemedy(path string, err error) string {
	full := dataFileRemedyFor(path, err)
	if full == "" || full != dataFileRemedy(path) {
		return full
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pst":
		return " — repair it in place with Outlook's Inbox Repair Tool (scanpst.exe, Windows), keeping its path, then run again"
	case ".ost":
		return " — on Windows run `mailarchive -outlook`, or use `mailarchive graph -auth device` for a Microsoft 365 mailbox"
	}
	return full
}

// fileFailureError is the run's error when data files failed to open or read
// (friction F2). It names the first failing file, the cause in a few words and
// the remedy for that cause, on one line, so the last-run record, `status` and
// BACKUP-NEEDS-ATTENTION.txt (which all carry this text and send the reader to
// `status`) are not a dead end. The run log still has every file.
func fileFailureError(failures int, firstPath string, firstErr error) error {
	cause := ""
	if firstErr != nil {
		cause = firstErr.Error()
		// go-pst and os wrap the path in front of the cause two or three times.
		for {
			i := strings.LastIndex(cause, firstPath+": ")
			if i < 0 {
				break
			}
			cause = cause[i+len(firstPath)+2:]
		}
	}
	msg := fmt.Sprintf("%d file(s) failed: %s", failures, util.StripControl(firstPath))
	if cause != "" {
		msg += ": " + util.StripControl(cause)
	}
	msg += util.StripControl(shortFileRemedy(firstPath, firstErr))
	if failures > 1 {
		msg += fmt.Sprintf(" (the run log names the other %d)", failures-1)
	}
	return withKind(state.FailSourceUnreadable, errors.New(msg))
}

// runFile opens one data file (optionally via a temp snapshot) and exports every
// message it contains, honouring ctx cancellation between messages. What goes
// wrong with one item or folder is recorded in the capture-gap ledger and the walk
// goes on; what goes wrong with the archive itself ends the source (localrun.go).
func runFile(ctx context.Context, lr *localRun, path string, copyFirst, followLinks bool, onProgress ProgressFunc, abort func() error) error {
	exp, out, logger := lr.exp, lr.out, lr.logger
	openPath := path
	if copyFirst {
		// Snapshotting only applies to single files (e.g. a locked .ost);
		// mail-store directories are read in place.
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			dir := lr.snapDir
			if dir == "" {
				dir = out
			}
			snap, cleanup, err := snapshot(path, dir)
			if err != nil {
				// A failure of the ARCHIVE's volume (full, read-only, gone) is the run's, as
				// when an export write meets it; one reading the source file is the file's
				// own (S2A-AC9). An encrypted archive's snapshot is on MailArchive's working
				// folder's drive, and is worded as that drive's.
				var pe *fs.PathError
				toArchive := errors.As(err, &pe) && strings.HasPrefix(filepath.Base(pe.Path), snapshotPrefix)
				if kind := export.FatalKindAt(err, dir); kind != export.FatalNone && toArchive {
					if dir != out {
						return scratchFatalFor(dir, err)
					}
					return runFatalFor(out, kind, err)
				}
				return err
			}
			defer cleanup()
			openPath = snap
		}
	}

	reader, err := source.OpenWith(openPath, source.OpenOptions{FollowLinks: followLinks})
	if err != nil {
		return err
	}
	defer reader.Close()

	store := reader.StoreName()
	// The token scopes this source's identity and names its on-disk tree. Its
	// seed is the CANONICAL source id (state.PathSourceID) of the original input
	// path (never the -copy-first snapshot, whose name changes every run), so a
	// different spelling of the same file — relative vs absolute, a symlink, a
	// trailing slash — hits the same token instead of minting a duplicate tree
	// (INT-CC-1/R2), while two distinct sources with the same display name still
	// get distinct, sticky trees (F1). The source is still READ through the
	// original path.
	token := exp.Manifest.Token(state.PathSourceID(path), store)
	if err := refuseStoreIntoEncrypted(exp, token); err != nil {
		return err
	}
	logger.Printf("Reading %s (store: %s)", path, store)

	src := &sourceRun{lr: lr, token: token, path: path, freshOutlook: lr.outlookFresh[state.PathSourceID(path)]}
	handler := func(folderPath []string, m *model.Message) error {
		if err := src.handle(ctx, abort, folderPath, m); err != nil {
			return err
		}
		if onProgress != nil {
			onProgress(exp.Stats)
		}
		return nil
	}
	// A reader that reports its faults as they happen (the PST reader) records them
	// during the walk, so they reach the checkpoints of an hours-long file (FC29);
	// the others return what they hit as an error, as they always did.
	if fw, ok := reader.(source.FaultWalker); ok {
		src.reportsFaults = true
		_, isPST := reader.(*source.Reader)
		src.local = !isPST
		return src.finish(fw.WalkWithFaults(handler, source.WalkOptions{
			Faults:     src.onReaderFault,
			Abort:      func() error { return src.stop(ctx, abort) },
			NoStoreUID: noStoreUID(path),
			Folder:     src.noteFolder,
		}))
	}
	return src.finish(reader.Walk(handler))
}

// snapshotPrefix starts the name of a -copy-first snapshot's temp file, which is how a
// failure of its WRITE (the archive's volume) is told from one reading the source, and
// how the sweep finds one a crash left behind.
const snapshotPrefix = export.SnapshotPrefix

// snapshotCopy is the copy a -copy-first snapshot makes; a var so a test can fail it
// the way a full drive does.
var snapshotCopy = io.Copy

// snapshot copies src to a temporary file in dir — the archive itself, ON THE
// ARCHIVE'S VOLUME (the drive the operator chose and sized — a multi-gigabyte
// .ost must not land in a small system temp directory at 02:00), or an encrypted
// archive's scratch folder (snapshotDir) — returning its path and a cleanup func.
func snapshot(src, dir string) (string, func(), error) {
	// The operator's own spelling may be a link (the file it names is copied); the open
	// neither follows one nor waits on a pipe: a FIFO given as the input held the run, and
	// the archive lock, until a writer appeared (FC2, S2B-AO4).
	real := src
	if p, err := filepath.EvalSymlinks(src); err == nil {
		real = p
	}
	in, err := util.OpenRegularFile(real)
	if err != nil {
		return "", nil, fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(dir, snapshotPrefix+"*"+filepath.Ext(src))
	if err != nil {
		return "", nil, fmt.Errorf("create snapshot: %w", err)
	}
	if _, err := snapshotCopy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("copy snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}
	name := tmp.Name()
	return name, func() { os.Remove(name) }, nil
}

// DiscoverInputs expands the given files/directories and, if auto is set, the
// default Outlook, Thunderbird, and Evolution locations into a deduplicated list
// of data files/stores.
func DiscoverInputs(inputs []string, auto bool) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			files = append(files, p)
		}
	}

	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, &sourceUnreachableError{path: in}
			}
			return nil, fmt.Errorf("input %s: %w", in, err)
		}
		if info.IsDir() {
			if source.IsMailStoreDir(in) {
				add(in) // Thunderbird/mbox store: one source, don't expand
			} else {
				found, err := scanDir(in)
				if err != nil {
					return nil, err
				}
				for _, f := range found {
					add(f)
				}
			}
		} else {
			add(in)
		}
	}

	if auto {
		for _, f := range autoDiscover() {
			add(f)
		}
	}
	return files, nil
}

func scanDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isDataFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// autoDiscover returns default mail-source locations: Outlook data files
// (Windows), and Thunderbird and Evolution account/store directories
// (Linux/macOS/Windows).
func autoDiscover() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	// Outlook data files. Skip any that can't be parsed — Outlook leaves orphaned
	// or stub .ost files behind (e.g. from removed accounts) that would otherwise
	// pad the picker with unreadable entries. A file that is merely locked by a
	// running Outlook is kept (DataFileReadable is lock-tolerant).
	for _, g := range outlookGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if source.DataFileReadable(m) {
				add(m)
			}
		}
	}

	// Thunderbird and Evolution account/store directories (only real stores).
	for _, g := range append(thunderbirdGlobs(), evolutionGlobs()...) {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.IsDir() && source.IsMailStoreDir(m) {
				add(m)
			}
		}
	}
	return out
}

func outlookGlobs() []string {
	var globs []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.ost"))
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.pst"))
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		globs = append(globs, filepath.Join(up, "Documents", "Outlook Files", "*.pst"))
	}
	return globs
}

func thunderbirdGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(home, ".thunderbird"),
		filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
		filepath.Join(home, ".mozilla-thunderbird"),
		filepath.Join(home, "Library", "Thunderbird", "Profiles"),
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		roots = append(roots, filepath.Join(ad, "Thunderbird", "Profiles"))
	}
	var globs []string
	for _, r := range roots {
		// Each account under ImapMail/ and Mail/ (incl. "Local Folders") is a store.
		globs = append(globs, filepath.Join(r, "*", "ImapMail", "*"))
		globs = append(globs, filepath.Join(r, "*", "Mail", "*"))
	}
	return globs
}

// evolutionGlobs returns the default Evolution store locations: the local
// Maildir++ store and each IMAP account's disk cache (native and Flatpak paths).
func evolutionGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "evolution", "mail", "local"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "data", "evolution", "mail", "local"),
		filepath.Join(home, ".cache", "evolution", "mail", "*"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "cache", "evolution", "mail", "*"),
	}
}

func isDataFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".pst" || ext == ".ost"
}
