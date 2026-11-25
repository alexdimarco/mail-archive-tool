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

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/delmark"
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
	"mail-archive-tool/internal/tbcomplete"
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
	// OwnFiles are the files and folders of MailArchive's own that this run's caller
	// keeps in Out — a scheduled run's -log and the scheduler's stderr file beside it,
	// `-outlook`'s _outlook-pst folder, the Desktop's settings folder (CC16). The
	// emptied-folder guard does not count them as the person's files
	// (RefuseEmptiedFolder, design-archive-delete §7).
	OwnFiles []string
	// ArchiveAtEntry says the caller's own first check of Out — made before the work it
	// does outside the archive lock (its run log, the Outlook PST export, a sign-in) —
	// found an archive there (ArchiveHere). An unattended run that found one, there or at
	// its own checks, refuses under its lock an Out that holds none any more, whatever is
	// left in it: a run that checked before a delete and got the lock after it is refused
	// (design-archive-delete §7, MA-956). OwnFiles excuse only a run that found none.
	ArchiveAtEntry bool
	// DeletionsLog is the deletion log the run asks whether a delete of Out did not finish, at its
	// start and again under its lock (RefuseUnfinishedDelete, MA-1044): "" is MailArchive's own,
	// as the terminal's and run-job's runs ask; the Desktop passes its own (a Desktop started with
	// -config keeps its log beside its settings).
	DeletionsLog string

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
	// PresyncWanted says the job asks for the Thunderbird sync, and PresyncAttended
	// that the person started this run at this computer (R38). Set by the caller's
	// options builder; never written (design-presync-attended §4.1, C-33, C-34).
	PresyncWanted, PresyncAttended bool
	// OnReadStart, when set, is called once right before the source loop, after the
	// sweeps, the index open and the sealed catch-up — the moment mail reading starts
	// (design-presync-attended §3.2, C-76). The run checks for a cancel right after it.
	OnReadStart func()
}

// PresyncOutcome is the Thunderbird sync's outcome for the run's caller, in memory
// only: it carries no JSON tag, and the last-run record converts it to its own
// type (design-presync-attended §4.1, C-36, C-80).
type PresyncOutcome struct {
	Code, Name, Class string // Class: "none" | "note" | "warn"
	Synced            bool   // the outcome counts as synced (§5.2), from the outcome alone
	// Left: none | exited | closed | left-open | left-open-by-choice |
	// left-open-by-rule | may-be-starting.
	Left      string
	Minimized bool   // Windows: the backstop minimize succeeded
	Verdict   string // the wait's verdict, "" if none
	Line      string // the owner-facing line, in the surface's and platform's words; no path
	Started   time.Time
	Waited    time.Duration
}

// presyncRecordFor is the session's outcome as the plaintext last-run record keeps it
// (design-presync-attended §4.1, §4.2, C-80): its code, name, result and start only —
// never the line, what was left open or anything that says how the run was started
// (C-36). nil for no outcome.
func presyncRecordFor(o *PresyncOutcome) *state.PresyncRecord {
	if o == nil {
		return nil
	}
	return &state.PresyncRecord{Code: o.Code, Name: o.Name, Result: presyncResult(*o), Since: o.Started}
}

// presyncResult is the record's result for o, from the outcome alone (§4.2, §5.2,
// C-72): a cancelled session is "cancelled" whatever it left open, a wait the person
// stopped is "wait_ended" (OQ-6), and otherwise Synced decides. The two names are
// presync's OutcomeCancelled and OutcomeWaitEnded (MA-903 pins them).
func presyncResult(o PresyncOutcome) string {
	switch {
	case o.Name == "cancelled":
		return state.PresyncResultCancelled
	case o.Name == "wait-ended":
		return state.PresyncResultWaitEnded
	case o.Synced:
		return state.PresyncResultSynced
	}
	return state.PresyncResultNotSynced
}

// BeforeReadResult is what the pre-sync hook returns: warnings for the run to
// record, and the session's outcome for the caller (Result.Presync).
type BeforeReadResult struct {
	Warnings []state.RunWarning
	// Release, when non-nil, is called by Run AFTER the sources are read (always, via
	// defer — even on error or panic): the Thunderbird pre-sync holds a per-profile
	// lock across the pre-sync AND the read, so a second scheduled run cannot pre-sync
	// and compact that profile while this run reads it (adversarial finding C2).
	Release func()
	// Outcome is the session's outcome, handed back to the caller as Result.Presync.
	Outcome *PresyncOutcome
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
	// FillableFrom is the kind of mail the run read (a state.From* value; "" not known), so the
	// words for its messages not downloaded in full say where they came from (MA-1157).
	FillableFrom string

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

	// Presync is the Thunderbird sync's outcome when the run had a pre-sync hook that
	// ran, on every return (design-presync-attended §4.1, C-12); nil otherwise. In
	// memory only.
	Presync *PresyncOutcome

	// Thunderbird is each Thunderbird store's completeness verdict (docs/design-thunderbird-
	// completeness.md, slice C2), for a run whose walk of every source ended; a store that is
	// not_applicable (Local Folders, POP3, a plain mail folder) has none. In memory: a plaintext
	// run that ends ok records it (recordRun); for an encrypted archive it is the only place it
	// is kept, for the screen while unlocked (O1). tbcomplete.RunLead gives the run's lead.
	Thunderbird []tbcomplete.Result
	// tbPrev is the previous run's completeness record, read before this run's "running"
	// record replaced it, for TC-19's carried fingerprints.
	tbPrev []state.TBAccount
	// tbIDs are the identities of the accounts that own this run's Thunderbird stores, beside each
	// store's id (tbIdentities; docs/design-per-mailbox-archives.md revision 3): what a plaintext run
	// that ends ok records. In memory only.
	tbIDs []state.TBID
	// TBNotices are the Thunderbird accounts whose behind-streak this run found due an email
	// (the "still downloading" or "stopped" email, design §5, slice C5): only accounts this run
	// found behind, of a plaintext archive, and only when the run wrote the streak record, so the
	// email's flag can be set in it. A scheduled run's alert layer sends them
	// (backupalert.ReportTBBehind), then MarkTBNoticesSent sets the flags. In memory only.
	TBNotices []state.TBNotice

	// Skipped are the listed inputs this run did not find and skipped (DiscoverInputs,
	// design-1.7.5-fixes §1.2, MA-1471), in list order, on every return after discovery: the
	// summaries say them from here (§1.3). In memory only; for an encrypted archive this is the
	// only place their paths and names are kept.
	Skipped []SkippedInput
}

// SkippedInput is one listed input a run skipped because it is gone (MA-1471): its path as
// listed, the account's name when the profile's settings still name an account for that folder
// ("" otherwise), and when it was first found gone (MA-1472).
type SkippedInput struct {
	Path  string
	Name  string
	Since time.Time
	// Listed: the profile's settings still name an account for the folder, so Name came from them
	// and status says to let Thunderbird download it (the 1.7.5 friction review's F4). Name may also
	// come from the previous record (F3), where Listed is false.
	Listed bool
	// SettingsUnsure: the profile's Thunderbird settings could not be read for certain, so whether
	// the account was moved could not be told (the 1.7.5 re-verify's RV-04); status says so.
	SettingsUnsure bool
}

// Label is how the run's words name a skipped input: "NAME (PATH)", or PATH alone when no name
// is known.
func (s SkippedInput) Label() string {
	if s.Name == "" {
		return s.Path
	}
	return s.Name + " (" + s.Path + ")"
}

// finish fills the manifest-derived fields of a Result and regenerates the
// verification report (R1). When scaffold is true it also (re)writes the archive
// README; a Graph run passes false when no mailbox succeeded, so a run that
// captured nothing leaves no empty browsable scaffold behind. Shared by the
// local and Graph runners. The single operator-facing "Verification:" line is
// printed by the caller's summary (cmd/mailarchive printSummary), not here, so
// the run log carries it exactly once.
func finish(out string, r *Result, exp *export.Exporter, manifest *state.Manifest, idx *index.Index, indexErrors int, scaffold bool, logger *log.Logger) {
	_ = finishChecked(out, r, exp, manifest, idx, indexErrors, scaffold, logger, nil)
}

// finishChecked is finish with check asked right before each file it writes into the archive
// folder, the README and the report (a run's archive guard, the B3 adversarial pass, round 4's
// J1): when check answers no, neither is written from then on and its error is returned; the
// Result's counts are filled all the same. A nil check asks nothing.
func finishChecked(out string, r *Result, exp *export.Exporter, manifest *state.Manifest, idx *index.Index, indexErrors int, scaffold bool, logger *log.Logger, check func() error) error {
	var stopped error
	may := func() bool {
		if stopped == nil && check != nil {
			stopped = check()
		}
		return stopped == nil
	}
	// An encrypted archive (a keyed run, which prepareEncrypted proved against the
	// archive's vault) gets no layout README and no report: both describe, in
	// cleartext, what the units hide (phase2a §4). Its note is written by refreshNote
	// as the run opens, its digest sealed, and by the program step (program design R5):
	// never here, where it would not match the digest.
	enc := exp.Units != nil
	r.encrypted = enc
	if scaffold && !enc && may() {
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
	if enc || !may() {
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
	return stopped
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

// runPause, when a test sets it, is called at named steps between the run's start and
// its source loop ("index-open", "sealed-catch-up"), so a test can cancel the run
// during them (design-presync-attended MA-902). Nil in production.
var runPause func(point string)

// OverrideRunPauseForTest sets runPause for a test in another package — the
// Desktop's Lock during an encrypted Run now's sealed catch-up (design-presync-
// attended MA-913, C-76) — and returns what restores it. For tests only; production
// code never sets runPause.
func OverrideRunPauseForTest(f func(point string)) (restore func()) {
	old := runPause
	runPause = f
	return func() { runPause = old }
}

// pauseAt calls runPause at point when a test set it.
func pauseAt(point string) {
	if runPause != nil {
		runPause(point)
	}
}

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

	// Not into a folder whose delete did not finish: refused before any write, lock or
	// record, and asked again under the lock (MA-1044; the 1.5.3 re-walk's guards pass, G2).
	if err := RefuseUnfinishedDelete(opts.Out, opts.DeletionsLog); err != nil {
		return Result{}, err
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
	// Not into a folder of the person's files that an archive was deleted from: the
	// emptied-folder guard comes first (design-archive-delete §7, MA-956).
	guard := newOutGuard(opts)
	if opts.Unattended {
		if err := guard.before(false); err != nil {
			return Result{}, err
		}
	}
	files, gone, err := DiscoverInputsFor(opts.Inputs, opts.Auto, RecordedNamesAt(opts.Out))
	if err == nil && len(gone) > 0 {
		// A listed mailbox that is gone was skipped (MA-1471): said straight after discovery, in
		// the log the run's "Reading" lines go to, and handed back on every return from here.
		// That the others were backed up is said only by the summaries of a run that finished.
		skipped := skippedInputs(gone)
		// When each was first found gone, kept in the run's own warnings, which every record the
		// run writes from here carries (MA-1472). A fresh slice: the caller's is never written.
		if start := time.Now().UTC(); opts.Key != nil || encryptedAt(opts.Out) {
			resolveEncryptedGone(opts.Out, skipped, start) // one time for the archive (MA-1478)
		} else {
			resolveGoneSince(opts.Out, skipped, start)
		}
		opts.Warnings = append(append([]state.RunWarning(nil), opts.Warnings...), goneWarnings(skipped)...)
		// By its path alone: this log is a scheduled run's log file too, and no log file holds the
		// name Thunderbird shows for an account (MA-1288); the summaries name it on screen.
		for _, s := range skipped {
			logger.Printf("Not found, so skipped: %s. It is no longer on this computer; the mail already saved from it stays in the archive.", s.Path)
		}
		defer func() { result.Skipped = skipped }()
	}
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
	// never recreate the archive on the local disk and silently back up into a decoy. Nor does
	// it make a new archive among the person's files in a folder an archive was deleted from
	// (the emptied-folder guard, design-archive-delete §7, MA-956).
	if opts.Unattended {
		if err := guard.before(true); err != nil {
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
	// An unattended run never creates -out: it was checked above, and one gone since is
	// refused at the lock below, never recreated (design-archive-delete §7).
	if !opts.Unattended {
		if err := makeArchiveDir(opts.Out, 0o755); err != nil {
			return Result{}, withKind(state.FailArchiveUnwritable, fmt.Errorf("create output dir: %w", err))
		}
	}
	// One run per archive at a time (R5): a scheduled run overlapping a manual
	// one would otherwise interleave manifest/index/file writes.
	lockPath := filepath.Join(opts.Out, lockfile.Name)
	lock, err := acquireArchiveLock(lockPath, lockVerb(opts, "export"))
	if err != nil {
		return Result{}, lockRefusal(opts, lockPath, err)
	}
	defer lock.Release()
	// Again under the lock (MA-1044, G2): a delete that began after this run's first ask and
	// stopped part way before the lock was free is caught here, before the run's record or any
	// write; a delete that begins from now on is refused by this run's lock. Before the
	// emptied-folder guard (DM-25): a run that queued while a delete removed the archive's anchor
	// is told "Delete not finished", not that the folder holds no archive.
	if err := RefuseUnfinishedDelete(opts.Out, opts.DeletionsLog); err != nil {
		return Result{}, err
	}
	// Again under the lock (design-archive-delete §7, NF-3): a delete that finished
	// between this run's checks and its lock left no archive to write into; a run that saw
	// one, at its checks or its caller's, is refused whatever the delete left (outGuard).
	if opts.Unattended {
		if err := guard.underLock(); err != nil {
			return Result{}, err
		}
	}
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
	// The previous run's Thunderbird completeness is read first, before the "running"
	// record replaces it (completeness design TC-19: fingerprints carried forward).
	tbPrev := previousThunderbird(opts)
	begun := beginRun(opts)
	// Before every save of the run's progress, before its writes during its walk and before each
	// write of its tail, the folder is proved to still be this run's: there, and its lock file
	// still this run's own (archiveguard.go). The pre-sync below waits minutes for Thunderbird and
	// the walk reads for minutes; a drive of a rotation may be swapped at the same path or letter,
	// or the folder renamed, in that time, and nothing of this archive's may then be written into
	// the other (1.7.2). The "running" record just written binds the run to its folder.
	proof := newArchiveGuard(ctx, opts, logger, lock.StillHeld)
	proof.noteBegun()
	// What binds the run to its archive's folder once its lock is no longer its own (endUnheld):
	// armed only by the first proof that follows the manifest's load, so the bytes it names were
	// read while the folder was the run's (a manifest read after a swap is the other archive's).
	binding := func() (string, [32]byte, bool) { return recordedFile(opts, mpath, manifest, sealed) }
	// The opt-in Thunderbird pre-sync runs here, after the lock and the run's
	// start and before any source is read; nil unless a caller opts in, and it
	// never fails the run (design thunderbird-presync §4.1). Its warnings join
	// this run's record before the deferred finalizer captures it. A run already
	// cancelled never starts it (design-presync-attended C-10): the T16 check below
	// records it cancelled.
	var presyncRelease func()
	// presyncOut is the session's outcome, kept in memory and handed back as
	// Result.Presync on every return (design-presync-attended §4.1, C-12, C-53).
	var presyncOut *PresyncOutcome
	if opts.BeforeRead != nil && ctx.Err() == nil {
		// A panic in the pre-sync session is recovered here, so it can never fail
		// the archive (R32, design §4.9.1): the run records presync_internal_error
		// and reads the sources anyway.
		func() {
			defer func() {
				if r := recover(); r != nil {
					begun.Warnings = append(begun.Warnings, state.RunWarning{Code: "presync_internal_error", Message: "The Thunderbird sync failed unexpectedly; the archive ran anyway."})
					presyncOut = &PresyncOutcome{Name: "internal-error", Class: "warn"}
					logger.Printf("warning: the Thunderbird pre-sync panicked and was recovered (%v); the archive ran anyway", r)
				}
			}()
			br := opts.BeforeRead(ctx, files)
			begun.Warnings = append(begun.Warnings, br.Warnings...)
			presyncRelease = br.Release
			presyncOut = br.Outcome
		}()
		// The outcome rides on the record value, converted to the record's own type,
		// as the warnings do, before recordRun captures it (design-presync-attended
		// §4.1, C-12, C-80): every return below records it. recordRun drops it for a
		// keyed run (C-16).
		begun.Presync = presyncRecordFor(presyncOut)
	}
	// C2: release the pre-sync's per-profile lock AFTER the sources are read. The defer
	// runs at Run's return (after the read, index and pages), always — even on error or
	// panic — so a second scheduled run cannot pre-sync and compact this profile while
	// this run reads it, and the lock can never leak the run.
	if presyncRelease != nil {
		defer presyncRelease()
	}
	rec := begun
	defer func() {
		// A run whose lock is no longer its own is not this archive's success: one that would
		// otherwise end ok, or partly ok, or that stopped on its lost lock, ends in the plain
		// words of what it found (or "unavailable", the folder not reachable). Its failure is
		// recorded only in a folder that still holds the archive it began in; nothing at all
		// goes into one that may hold another archive now (a drive swapped at the same path or
		// letter, the folder moved) or that another run took over (1.7.2; endUnheld).
		if !proof.closeOut(&err) {
			return
		}
		recordRun(opts, rec, &result, &err, logger)
	}()
	// Registered after recordRun so it runs first: every return below, the early ones
	// that build a fresh Result included, hands the caller the session's outcome.
	defer func() { result.Presync = presyncOut }()
	// The same for the Thunderbird completeness verdicts, set once every source was walked
	// (nil on any earlier return), and the previous record recordRun carries forward from.
	var tbResults []tbcomplete.Result
	var tbIDs []state.TBID
	defer func() { result.Thunderbird, result.tbPrev, result.tbIDs = tbResults, tbPrev, tbIDs }()
	// T16 (design-presync-attended §4.1, C-11): a cancel during the pre-sync, or before
	// it, stops the run here, before any sweep, index or source. It comes after the
	// deferred recordRun and Release, so the record ends cancelled, never running, and
	// the profile lock is released.
	if ctx.Err() != nil {
		return result, context.Canceled
	}
	// Once the pre-sync's wait is over, before the sweeps or anything else is read or written, the
	// folder is proved to still be this run's, encrypted or not (an encrypted run's manifest was
	// loaded before the wait, so the proof after the load below is a plaintext run's only: the
	// round-5 review's K4).
	if perr := proof.prove(); perr != nil {
		return Result{}, proofStop(perr)
	}
	if manifest != nil { // an encrypted run's, opened under the lock before the wait
		proof.recorded = binding
	}
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
	// orphan zips. Only files older than this run's start are touched, and each
	// removal is proved first: the sweep walks the whole folder by path, and after a
	// swap that is the other archive's (1.7.2; the patch review's P5).
	n, serr := export.SweepOrphansChecked(opts.Out, time.Now(), logger, func(string) error { return proof.prove() }, programSweepSkip(opts)...)
	if n > 0 {
		logger.Printf("swept %d orphaned temp/zip file(s) from an interrupted run", n)
	}
	if serr != nil {
		return Result{}, proofStop(serr)
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
		// Again once the manifest has loaded, before the index or anything is written: the
		// folder is still this run's (a swap during the pre-sync, which waits minutes, is caught
		// here, before the run reads another archive's manifest as its own any further).
		if perr := proof.prove(); perr != nil {
			return Result{}, proofStop(perr)
		}
		proof.recorded = binding
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
		// The run began on a folder that is there: it never makes it again (F5).
		KeepOutDir: true,
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
		pauseAt("index-open")
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
			pauseAt("sealed-catch-up")
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
			// Between saves, an encrypted walk proves after every message it wrote (round 4's
			// J2). Nothing is proved on a timer while a walk only reads (the patch review's P1): a
			// plaintext walk proves right before its next write once 10 s have passed
			// (exp.BeforeMessageWrite), so a long walk that writes stops soon after a swap
			// (round 3's H1).
			if lockLost == nil && proof.dueAfter(stats) {
				lockLost = proof.prove()
			}
			return
		}
		lastCheckpoint = processed
		// The lock file is the run's guarantee of exclusivity; if it was removed
		// or replaced under us, another run may now hold a fresh one. Stop
		// rather than race it (R5). The proof asks the lock, after the folder still
		// holding its archive (round 3's H1).
		err := proof.prove()
		if err != nil && lockLost == nil {
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
	// Right before a file already in the folder is written over or removed, the folder is proved
	// at once: a removal or an overwrite is what the other archive's own next backup could not
	// undo (the round-5 review's K1). New files are proved before a message's first write once
	// 10 s have passed since the last proof (the patch review's P1).
	exp.BeforeReplace = proof.replaceGuard(&lockLost)
	exp.BeforeMessageWrite = proof.writeGuard(&lockLost)
	result = Result{Files: len(files), ManifestBefore: manifestBefore, FillableFrom: fillableFrom(files)}
	var failures int
	var firstFailed string
	var firstErr error
	var fatal *RunFatalError        // a run-level fault of the archive itself: it ends the run
	var stopped *SourceStoppedError // the first source the local breaker ended: the run goes on without it
	// cancelled ends a cancelled run once its last commit was attempted: what the run
	// did is kept, its report regenerated, and it returns cancelled — unless that last
	// save failed, when "progress was kept" would be false and it is recorded failed.
	cancelled := func() (Result, error) {
		result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
		// The README and the report are proved first, as the end's are (round 4's J1).
		if perr := finishChecked(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger, proof.prove); perr != nil {
			return result, proofStop(perr)
		}
		if saveErr != nil {
			return result, manifestSaveFailure(mpath, saveErr)
		}
		return result, context.Canceled
	}
	// Mail reading starts here (design-presync-attended §3.2, C-76): the caller is told,
	// then a cancel during the sweeps, the index open, the key repair or the sealed
	// catch-up stops the run before any source is opened, through the source loop's own
	// cancel path, so the catch-up's work is kept as a cancel during the first source
	// would keep it.
	if opts.OnReadStart != nil {
		opts.OnReadStart()
	}
	if ctx.Err() != nil {
		if perr := proof.prove(); perr != nil { // before any save (round 3's H1)
			return result, proofStop(perr)
		}
		saveErr = commit(nil, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)
		return cancelled()
	}
	for _, f := range files {
		runErr := runFile(ctx, lr, f, opts.CopyFirst, followsLinks(opts.FollowLinks, f), func(stats export.Stats) {
			checkpoint(stats)
			if onProgress != nil {
				onProgress(stats)
			}
		}, func() error { return lockLost })
		// A walk that found the folder gone: no proof waits for it again.
		var ended *RunFatalError
		if errors.As(runErr, &ended) {
			proof.gone(ended)
		}
		if lockLost != nil {
			// The lock was removed or replaced mid-run: another run may now own
			// this archive. Do NOT write shared state (manifest/index/README) into
			// it — the deferred recordRun marks this run failed, and the message
			// files already written are reconciled by the next incremental run
			// (index-before-manifest ordering leaves no orphaned manifest rows).
			// (INT-CC-2/R5.)
			return result, proofStop(lockLost) // a lost lock, or the archive location gone (H1)
		}
		// Before the save, the folder is proved to still hold this run's archive and the lock to
		// be its own (round 3's H1): a plaintext save writes by path, and a store's walk may be the
		// whole run. A source that ended on the archive's own fault says so first.
		if perr := proof.prove(); perr != nil {
			if errors.As(runErr, &fatal) {
				return result, fatal
			}
			return result, proofStop(perr)
		}
		saveErr = commit(nil, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)

		if errors.Is(runErr, context.Canceled) {
			return cancelled()
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
	if afterWalkForTest != nil {
		afterWalkForTest()
	}
	// stopIn ends the run when a proof failed: the archive's own fault, when a source ended on it,
	// says so first.
	stopIn := func(perr error) (Result, error) {
		if fatal != nil {
			return result, fatal
		}
		return result, proofStop(perr)
	}
	// And before the pages and the README (round 3's H1).
	if perr := proof.prove(); perr != nil {
		return stopIn(perr)
	}
	if tailForTest != nil {
		tailForTest()
	}
	// Thunderbird completeness (slice C2): once every store is walked, so its folder cache is
	// read after the walk, once per profile (TC-21). It reads; it writes nothing here.
	if fatal == nil {
		tbResults = assessThunderbird(lr.tb, time.Now())
		tbIDs = tbIdentities(lr.tb, tbResults)
	}

	// Browsable folder index pages, built from the finished index. Regenerate
	// them only when this run exported something or the root index.html is not
	// yet there (nas-02): a zero-change re-run must not rewrite every page.
	// An encrypted archive has no folder pages: they are its folder tree, subjects and
	// senders in cleartext (phase2a §3); MailArchive's reader renders them (Phase 3).
	// The pages are written by path, so the folder is proved to still hold this archive before
	// them, on time between them and before the root page (round 4's J1).
	if gaps := gapBannerCount(manifest); exp.Units == nil && idx != nil && opts.Pages && pagesNeeded(opts.Out, exp.Stats.Exported, gaps) {
		if perr := proof.writePages(idx, logger, gaps); perr != nil {
			return stopIn(perr)
		}
	}

	if hErr := healLateDamage(si, sealed, manifest, units, logger); hErr != nil && saveErr == nil {
		saveErr = hErr
	}
	result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
	// And before the README and the report (J1).
	if perr := finishChecked(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger, proof.prove); perr != nil {
		return stopIn(perr)
	}
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
		lr := state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), Encrypted: true, Program: program}
		// A mailbox this run skipped because it is gone: that one was, the time one was first
		// found gone and how many, from the run's warnings in memory, never their paths or names
		// (MA-1478; owner, Q3, Q5 and Q6).
		if n, since := goneOf(opts.Warnings); n > 0 {
			lr.MailboxGone, lr.MailboxGoneSince, lr.MailboxGoneCount = true, since, n
		}
		return lr
	}
	exe, _ := os.Executable()
	mode := "incremental"
	if opts.Mode == export.Full {
		mode = "full"
	}
	lr := state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), PID: os.Getpid(), Exe: exe, Mode: mode, Job: canonicalJob(opts, mode), NoIndex: !opts.Index, Warnings: opts.Warnings}
	// Each Thunderbird folder's account name, carried from the previous record into this one and so
	// into every record the run writes from it (running, refused, failed, cancelled; an ok run with
	// completeness entries of its own keeps the names there instead): a moved account is then still
	// told from a removed one after a stopped backup (the 1.7.5 re-verify, round 3's RV3-01). In the
	// record only, never in a log file (MA-1288).
	// The accounts' identities too (docs/design-per-mailbox-archives.md revision 3), as the names are.
	if opts.Out != "" {
		if prev, st, _ := state.ReadLastRun(opts.Out); st == state.LastRunPresent {
			lr.ThunderbirdNames = state.TBNamesOf(prev)
			lr.ThunderbirdIDs = state.TBIDsOf(prev)
		}
	}
	return lr
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
	// archive takes no write, no lock and no record (S2-C1, F21). Nor is a folder that holds the
	// delete marker, or whose marker cannot be looked up: a half-deleted archive (DM-14).
	if insideEncrypted(opts.Out) || seal.IsSealed(opts.Out) {
		return
	}
	if !noDeleteMarker(opts.Out) {
		return
	}
	lock, err := acquireArchiveLock(filepath.Join(opts.Out, lockfile.Name), lockVerb(opts, "export"))
	if err != nil {
		return
	}
	defer lock.Release()
	// Again under the lock (the slice-2 pass's LOW-1): a delete that wrote its marker after the
	// ask above and stopped part way before the lock was free.
	if !noDeleteMarker(opts.Out) {
		return
	}
	writeRefusedRun(opts, cause)
}

// noDeleteMarker reports whether out is confirmed to hold no delete marker (DM-14): a marker
// there, or a lookup of it that failed, is false.
func noDeleteMarker(out string) bool {
	st, err := delmark.Present(out)
	return err == nil && st == delmark.Absent
}

// writeRefusedRun writes RecordRefusedRun's failed record and history entry; the
// caller holds the archive lock. An encrypted archive's record carries the failure
// kind, never the error text (P2a-C16).
func writeRefusedRun(opts Options, cause error) {
	lr := newRunRecord(opts)
	lr.Status, lr.Finished, lr.Error = state.RunFailed, lr.Started, cause.Error()
	if lr.Encrypted {
		lr.Error, lr.ErrorKind = "", FailureKind(cause)
	} else {
		// A moved or removed Thunderbird account's stop keeps its reason, a fixed code, so status, the
		// banner and the 30-day pause say that reason's words (F-03); plaintext records only.
		lr.SourceReason = sourceReasonOf(cause)
	}
	_ = state.WriteLastRun(opts.Out, lr)
	// A refused scheduled run is a failure the history should show too (MA-701), best effort.
	_ = appendRunHistory(opts.Out, state.RunHistoryEntryFrom(lr))
	// Every caller is a scheduled run, and a run refused because its local source is gone is the
	// commonest source loss: it moves the source streak like a finished run would (§5.7), or a
	// lost Thunderbird folder would never warn or auto-pause (design-desktop-settings-card 3.7,
	// MA-871). Any other refusal is inconclusive and leaves the streak.
	updateSourceStreak(opts.Out, cause, lr.Finished, lr.Encrypted, nil)
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

// oneFolderNameKept is, for an ok plaintext run of exactly one listed folder with auto-discovery off,
// the account name the previous record gave that folder (carried, from newRunRecord), kept when this
// run found no live account for it: its completeness result says no_match and names no account
// (docs/design-per-mailbox-archives.md revision 2.2, the lead's call on the builder's finding;
// MA-1504). Without it, a backup between "account removed" and "account added again" would write a
// record that names none, and C2 could never stop the run after the re-add. A live account's own name
// refreshes it (nothing is kept). An auto-discovery run never carries one: it finds a re-added
// account's new folder by itself, and a carried name would make a later removal of the old folder
// look like a move. nil otherwise.
func oneFolderNameKept(opts Options, results []tbcomplete.Result, carried []state.TBName) []state.TBName {
	if opts.Auto || len(opts.Inputs) != 1 || len(carried) == 0 {
		return nil
	}
	in := absOrSame(opts.Inputs[0])
	profile, ok := findTBProfile(in)
	if !ok {
		return nil
	}
	id := storeID(profile, in)
	for _, r := range results {
		if r.StoreID != id || r.Name != "" || r.Reason != tbcomplete.ReasonNoMatch {
			continue
		}
		for _, n := range carried {
			if n.StoreID == id && n.Name != "" {
				return []state.TBName{n}
			}
		}
	}
	return nil
}

// oneFolderIDKept is oneFolderNameKept for the account's identity (docs/design-per-mailbox-archives.md
// revision 3: "carried across a no-owner run as the name is"): for an ok plaintext run of exactly one
// listed folder with auto-discovery off, the identity the previous record kept for that folder
// (carried), when this run found no live account for it (its completeness result says no_match) and
// so has none of its own (own). An auto-discovery run never keeps one. nil otherwise.
func oneFolderIDKept(opts Options, results []tbcomplete.Result, own, carried []state.TBID) []state.TBID {
	if opts.Auto || len(opts.Inputs) != 1 || len(carried) == 0 {
		return nil
	}
	in := absOrSame(opts.Inputs[0])
	profile, ok := findTBProfile(in)
	if !ok {
		return nil
	}
	id := storeID(profile, in)
	for _, e := range own {
		if e.StoreID == id {
			return nil
		}
	}
	for _, r := range results {
		if r.StoreID != id || r.Reason != tbcomplete.ReasonNoMatch {
			continue
		}
		for _, c := range carried {
			if c.StoreID == id {
				return []state.TBID{c}
			}
		}
	}
	return nil
}

// recordRun finalizes the last-run record from the run's outcome.
// writeLastRun is the finalizing record write; a var so a test can refuse it.
var writeLastRun = state.WriteLastRun

// appendRunHistory adds a finalized run to the archive's rolling history (MA-701); a var
// so a test can observe or refuse it. Best effort — a lost history never fails a run.
var appendRunHistory = state.AppendRunHistory

func recordRun(opts Options, lr state.LastRun, result *Result, err *error, logger *log.Logger) {
	lr.Finished = time.Now().UTC()
	// Nothing about the Thunderbird sync is written beside an encrypted archive: any
	// outcome says the archive is made from Thunderbird on this computer, a source kind
	// (design-presync-attended §4.4, C-16). Dropped here, by code, so no change to the
	// encrypted writer can leak it.
	if opts.Key != nil || lr.Encrypted {
		lr.Presync = nil
	}
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
		// How much of each Thunderbird IMAP account Thunderbird had downloaded (completeness
		// design §7, slice C2): a plaintext run that ended ok only. Nothing about it is written
		// beside an encrypted archive (O1, R44); a run that failed says its failure (§11).
		if !lr.Encrypted && opts.Key == nil && len(result.Thunderbird) > 0 {
			carried := lr.ThunderbirdNames // the previous record's names (newRunRecord)
			carriedIDs := lr.ThunderbirdIDs
			lr.Thunderbird = tbRecords(result.Thunderbird, result.tbPrev, lr.Finished)
			lr.ThunderbirdMore = tbRecordsMore(result.Thunderbird)
			lr.ThunderbirdNames = nil // this run's own entries keep the names (RV3-01): written as before
			// …except a one-folder archive's name, kept across a run that found no live account for
			// its folder (design revision 2.2; MA-1504).
			lr.ThunderbirdNames = oneFolderNameKept(opts, result.Thunderbird, carried)
			// Each store's account identity, this run's own, and a one-folder archive's kept as its name
			// is (design revision 3).
			lr.ThunderbirdIDs = append(append([]state.TBID(nil), result.tbIDs...), oneFolderIDKept(opts, result.Thunderbird, result.tbIDs, carriedIDs)...)
		}
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
		// A partial run's kinds, so every surface tells it in their words (design-plain-
		// failure-words, MA-1145); additive, and never beside an encrypted archive.
		var pe *PartialError
		if errors.As(*err, &pe) && !lr.Encrypted {
			if p := pe.Counts(); !p.Empty() {
				lr.Partial = &p
			}
		}
		if lr.Encrypted {
			// An encrypted archive records the failure kind, never its text (P2a-C16).
			lr.Error, lr.ErrorKind = "", FailureKind(*err)
		}
	}
	lr.Exported, lr.Filled = result.Stats.Exported, result.Stats.Filled
	lr.Fillable, lr.Terminal, lr.Unknown, lr.IndexErrors = result.Fillable, result.Terminal, result.Unknown, result.IndexErrors
	if !lr.Encrypted { // where the run read its mail: plaintext only, beside its count (MA-1157)
		lr.FillableFrom = result.FillableFrom
	}
	// What this run observed in the capture-gap ledger (additive; a monitor keys on
	// gaps_new, FC23(2)). A partial run is recorded as the failure it maps to in
	// S1a: status "failed" with the *PartialError's one-line text.
	g := result.Gaps
	lr.GapsObserved, lr.GapsNew, lr.GapsRecovered = g.Observed, g.New, g.Recovered
	lr.GapsFillable, lr.GapsTerminal, lr.GapsStuck, lr.Unreadable = g.Fillable, g.Terminal, g.Stuck, result.Stats.Unreadable
	recordCounts(&lr, *result)
	// How long each Thunderbird account has been behind (completeness design §5, M10):
	// plaintext only, a run that ended ok only, best effort. Folded before the last-run record
	// is written, so a streak record the run could not use, or one too full to start a behind
	// store's streak, is a WARN that status shows (the C2 pass's ADV-C2-05 and -10, round 3's
	// R3-04), never silent.
	// The accounts it finds due an email go back in the result for the scheduled run's alert
	// layer (slice C5).
	ws, notices := foldTBStreak(opts, result.Thunderbird, lr.Encrypted, *err == nil, lr.Finished, logger)
	if len(ws) > 0 && !lr.Encrypted && opts.Key == nil {
		lr.Warnings = append(lr.Warnings, ws...)
	}
	result.TBNotices = notices
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
		logger.Printf("warning: could not record this run in %s (%v): `mailarchive status` may show this run as \"did not finish\" or still show the previous result, whatever this log says about how it ended — run again once the file can be written%s", filepath.Join(opts.Out, state.LastRunName), werr, hint)
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
	// And how long its scheduled backups have gone without a Thunderbird sync
	// (design-presync-attended §5, slice AS5): plaintext only, best effort.
	updateSyncStreak(opts, result.Presync, lr.Encrypted, lr.Finished, logger)
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
func runFile(ctx context.Context, lr *localRun, path string, copyFirst, followLinks bool, onProgress ProgressFunc, abort func() error) (err error) {
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
		opts := source.WalkOptions{
			Faults:     src.onReaderFault,
			Abort:      func() error { return src.stop(ctx, abort) },
			NoStoreUID: noStoreUID(path),
			Folder:     src.noteFolder,
		}
		// A local mbox or maildir store may be a Thunderbird account's: the completeness check
		// is planned from the input path as given (TC-11), and the walk counts as it reads. A
		// store under a Thunderbird profile that another local reader opened (the Evolution
		// one) counts nothing, and is told unknown, never left out (the C2 pass's ADV-C2-16;
		// which reader opens it is unchanged here).
		var tb *tbStore
		switch {
		case tbCheckOff || !src.local:
		case source.IsMboxReader(reader):
			tb = planTB(path)
			opts.Counts = tb.counts
		default:
			tb = planTBOther(path)
		}
		defer func() {
			if tb != nil {
				// Every store is told after every walk (TC-21); one whose walk ended with an
				// error is told unknown (walk_failed), never left out.
				tb.walkFailed = err != nil
				lr.tb = append(lr.tb, tb)
			}
		}()
		// MailArchive's own restores into Thunderbird are left out (R41,
		// design-restore-thunderbird §7.2): the record is restores.log in MailArchive's
		// default settings folder, whatever -config says (tbrestore.LogPath).
		skipped := 0
		if rs := lr.restoreSkipSet(); rs != nil {
			opts.RestoreSkip = rs
			opts.RestoreSkipped = func([]string) { skipped++ }
		}
		err := src.finish(fw.WalkWithFaults(handler, opts))
		if skipped > 0 {
			logger.Printf("Left out %d %s that MailArchive is restoring, or has restored, into Thunderbird (not backed up; not a fault).", skipped, plural(skipped, "folder", "folders"))
		}
		return err
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
//
// With auto set, a listed input that is gone is skipped instead of refused when
// autoplace.GoneSkippable accepts it (it is gone, not a dangling link; it lies where
// auto-discovery looks; its parent folder is still there, as a real folder, in that
// place) and a mailbox remains after the skips: skipped lists them, in list order
// (design-1.7.5-fixes §1.2, MA-1471). Otherwise a gone input refuses as before, with
// the typed sourceUnreachableError naming the first listed input the rule does not
// skip, or, when only "a mailbox remains" fails, the first gone one. Every caller gets
// skipped: one that does not report it discards it on purpose, saying why. A gone input
// whose Thunderbird account was moved, not removed, is never skipped, and its refusal says
// where the account went (autoplace.MovedTo, MA-1481).
func DiscoverInputs(inputs []string, auto bool) (files, skipped []string, err error) {
	return DiscoverInputsFor(inputs, auto, nil)
}

// RecordedAccount is what an archive's previous record kept of the account of an input's folder: its
// name (autoplace.RecordedName) and, since docs/design-per-mailbox-archives.md revision 3, its fixed
// identity (autoplace.RecordedID). Either may be "".
type RecordedAccount struct {
	Name, ID string
}

// RecordedNamesAt reads the previous last-run record of the archive at out once and gives, for an
// input, the account name it recorded for that folder (autoplace.RecordedName) and, since revision 3,
// that account's identity (autoplace.RecordedID); nil when out is "". The run and the command line's
// up-front check pass it to DiscoverInputsFor (RV2-01).
func RecordedNamesAt(out string) func(string) RecordedAccount {
	if out == "" {
		return nil
	}
	prev, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent {
		return nil
	}
	return func(in string) RecordedAccount {
		return RecordedAccount{Name: autoplace.RecordedName(prev, in), ID: autoplace.RecordedID(prev, in)}
	}
}

// movedAway reports, for an archive of the one folder in (there, auto-discovery off) whose previous
// record kept rec, where its account keeps its mail now when Thunderbird no longer uses in (C2, MA-1504):
// the settings say for certain that no live account uses the folder (autoplace.NoLiveOwner, F-01), and
// the account moved: by the identity the record kept (autoplace.MovedByID, revision 3: exactly one live
// account has it, its folder elsewhere), else, for a record with a name and no identity (every record
// before revision 3), by that name as revision 2.2 decides it (autoplace.MovedHowAs, decided by the
// name).
func movedAway(in string, rec RecordedAccount) (string, bool) {
	if (rec.ID == "" && rec.Name == "") || !autoplace.NoLiveOwner(in) {
		return "", false
	}
	if rec.ID != "" {
		return autoplace.MovedByID(in, rec.ID)
	}
	dir, moved, byName := autoplace.MovedHowAs(in, rec.Name)
	return dir, moved && byName
}

// DiscoverInputsFor is DiscoverInputs with nameOf, what the archive's previous record kept of each
// input's folder's account (nil, or "" fields, for none): its name decides first whether a gone
// Thunderbird folder's account was moved or removed (autoplace.GoneSkippableAs; the 1.7.5 re-verify's
// RV2-01); for an archive of one listed folder, auto-discovery off, its identity decides C2, F-08 and
// C13 where the record keeps one (docs/design-per-mailbox-archives.md revision 3).
func DiscoverInputsFor(inputs []string, auto bool, nameOf func(string) RecordedAccount) (files, skipped []string, err error) {
	recorded := func(in string) RecordedAccount {
		if nameOf == nil {
			return RecordedAccount{}
		}
		return nameOf(in)
	}
	seen := map[string]bool{}
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

	// An archive of one listed folder, auto-discovery off: the previous record's name for its folder
	// decides whether Thunderbird stopped using it (docs/design-per-mailbox-archives.md §11, C2, F-08,
	// C13; MA-1504, MA-1506).
	one := !auto && len(inputs) == 1
	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				var rec RecordedAccount
				if auto || one {
					rec = recorded(in)
				}
				name := rec.Name
				if auto && autoplace.GoneSkippableAs(in, name) {
					skipped = append(skipped, in)
					continue
				}
				if auto {
					// A Thunderbird account moved, not removed: stopped as 1.7.4 did, in words that say
					// where it went (MA-1481; the owner, 2026-11-20, Q7: "Stop the backup").
					if dir, moved, byName := autoplace.MovedHowAs(in, name); moved {
						return nil, nil, &sourceUnreachableError{path: in, movedTo: dir, sameServer: !byName}
					}
				}
				switch {
				case one && rec.ID != "":
					// Revision 3: the account known by the identity the record kept. F-08: exactly one
					// live account has it, keeping its mail elsewhere: the moved stop's words. C13: no
					// live account has it (a renamed account is not removed, INS-05): the removed
					// account's words. Either way the typed stop, as before.
					if dir, moved := autoplace.MovedByID(in, rec.ID); moved {
						return nil, nil, &sourceUnreachableError{path: in, movedTo: dir, one: true}
					}
					if autoplace.RemovedByID(in, rec.ID) {
						return nil, nil, &sourceUnreachableError{path: in, removed: true}
					}
				case one && name != "":
					// A record with a name and no identity (revision 2.2's rules). F-08: the folder gone,
					// and the account the record named now keeps its mail elsewhere: the moved stop's
					// words. C13: no live account has that name any more: the removed account's words.
					// Either way the typed stop, as before.
					if dir, moved, byName := autoplace.MovedHowAs(in, name); moved && byName {
						return nil, nil, &sourceUnreachableError{path: in, movedTo: dir, one: true}
					}
					if autoplace.RemovedAs(in, name) {
						return nil, nil, &sourceUnreachableError{path: in, removed: true}
					}
				}
				return nil, nil, &sourceUnreachableError{path: in}
			}
			// An input that cannot be examined (any error but "does not exist", which the skip rule
			// judges): refused as a gone input the rule does not skip is, typed, so the run is recorded
			// failed in the refusal's words and the streak moves. Windows CI at 1ba95c8: a dangling link
			// spelled with a trailing "\" gives ERROR_DIRECTORY, and the run ended untyped. It was the
			// plain error "input PATH: ERR", also a refusal; an input that exists but cannot be read
			// passes this check, as before.
			// Narrowed by the lead, 2026-11-21: only an error that says the path leads nowhere (a
			// component that is not a folder: ENOTDIR, or Windows' ERROR_DIRECTORY) is the typed refusal.
			// A permission error or any other failure to look stays the plain error, inconclusive about
			// the source (classifySourceReach: a present-but-unreadable source is never "gone"), so it
			// never moves the streak toward the warning and the 30-day pause, and is never told "does
			// not exist".
			if pathLeadsNowhere(err) {
				return nil, nil, &sourceUnreachableError{path: in}
			}
			return nil, nil, fmt.Errorf("input %s: %w", in, err)
		}
		// C2 (the owner's Q7 extended, MA-1504): the folder is still there, but Thunderbird no longer
		// uses it. Stopped, as a moved account's run is, when the record names the folder's account,
		// the profile's settings say for certain that no live account uses the folder (F-01), and
		// exactly one live account is that account, with its folder elsewhere: by the identity the
		// record kept (revision 3), else by its name (movedAway).
		if one && info.IsDir() {
			if dir, moved := movedAway(in, recorded(in)); moved {
				return nil, nil, &sourceUnreachableError{path: in, movedTo: dir, one: true}
			}
		}
		if info.IsDir() {
			if source.IsMailStoreDir(in) {
				add(in) // Thunderbird/mbox store: one source, don't expand
			} else {
				found, err := scanDir(in)
				if err != nil {
					return nil, nil, err
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
	// (d): a mailbox must remain after the skips, or the run refuses as before, naming the
	// first gone input.
	if len(skipped) > 0 && len(files) == 0 {
		return nil, nil, &sourceUnreachableError{path: skipped[0]}
	}
	return files, skipped, nil
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
	// running Outlook is kept (DataFileReadable is lock-tolerant). The patterns live
	// in internal/autoplace, beside the rule that skips a gone input in their places
	// (MA-1471), so the two cannot disagree.
	for _, g := range autoplace.OutlookGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if source.DataFileReadable(m) {
				add(m)
			}
		}
	}

	// Thunderbird and Evolution account/store directories (only real stores).
	for _, g := range append(autoplace.ThunderbirdGlobs(), autoplace.EvolutionGlobs()...) {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.IsDir() && source.IsMailStoreDir(m) {
				add(m)
			}
		}
	}
	return out
}

func isDataFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".pst" || ext == ".ost"
}
