package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// typedPath is path as a person types it on goos's command line in the notice's command: on
// Windows in double quotes as it is (a Windows path holds no double quote), elsewhere in single
// quotes, an embedded single quote closed and reopened. Never Go's %q, which doubled every
// Windows backslash (friction review of plain-failure-words, 2026-11-07).
//
// A Windows folder that ends in a backslash (a drive root E:\, a share root) would have that
// backslash read as escaping the closing quote by the C runtime's rule, so its trailing
// backslashes are doubled inside the quotes, which the rule reads back as one each (the same
// rule as encmark.CommandPath; MA-1158).
func typedPath(path, goos string) string {
	if goos == "windows" {
		trail := len(path) - len(strings.TrimRight(path, `\`))
		return `"` + path + strings.Repeat(`\`, trail) + `"`
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// noticeTime is when a run ended as a notice says it: the reader's own local time with its
// offset from UTC, as the alert email gives local time (the 1.6.1 friction review's P5,
// MA-1155), never UTC in ISO form.
func noticeTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05 -07:00")
}

// LastRunName is the archive-local record of the most recent run (R18).
const LastRunName = ".mailarchive-lastrun.json"

// LastVerifyName is the archive-local record of the most recent `verify` run
// (R18): a SEPARATE record from the export last-run, so a scheduled verify's
// verdict is visible to `status` without disturbing the export record's
// staleness clock, completeness counts or attention sidecar.
const LastVerifyName = ".mailarchive-lastverify.json"

// AttentionName is a plain-text sidecar written at the archive root when a run
// finalizes failed, and removed when one finalizes ok: a desktop user must not
// have to open a JSON file (or run a command) to learn last night's backup did
// not work.
const AttentionName = "BACKUP-NEEDS-ATTENTION.txt"

// IntegrityAttentionName is verify's own plain-text sidecar, kept distinct from
// the export sidecar (they answer different questions: "did the last backup
// succeed?" vs "is the archive intact right now?"). It is written when a verify
// finds modified or missing files and removed when a verify attests.
const IntegrityAttentionName = "ARCHIVE-INTEGRITY-ATTENTION.txt"

// LastRun statuses.
const (
	RunRunning   = "running"
	RunOK        = "ok"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

// LastRun.ErrorKind values: the search-index refusals a failed run records, and
// the Graph breaker's refusal (ErrKindServiceUnavailable: the run failed because
// Microsoft 365 or the network was unreachable, whatever the server's text said).
const (
	ErrKindServiceUnavailable     = "service_unavailable"       // the Graph breaker tripped
	ErrKindIndexBusy              = "index_busy"                // another program holds search.db
	ErrKindIndexUnwritable        = "index_unwritable"          // read-only file or folder, full or failing disk
	ErrKindIndexDamaged           = "index_damaged"             // unreadable, a manifest to rebuild from
	ErrKindIndexDamagedNoManifest = "index_damaged_no_manifest" // unreadable, nothing to rebuild from
)

// LastRun is written as "running" the moment a run holds the archive lock —
// before any early return — and finalized when it ends, so `status` can tell
// a run that never finished (power loss, kill) from one that succeeded.
type LastRun struct {
	Version  int       `json:"version"`
	Status   string    `json:"status"` // running | ok | failed | cancelled
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	PID      int       `json:"pid"`
	Exe      string    `json:"exe,omitempty"`
	Mode     string    `json:"mode,omitempty"`
	// Job is the run's canonical export flags (e.g. -out … -auto, or -out …
	// -input …), recorded so `status` can shape a "keep it current" remedy from
	// the job that actually made this archive instead of hard-coding -auto.
	// Absent on older records and on runs with no determinable local job (a
	// Graph run); status then falls back to a generic phrase.
	Job         []string `json:"job,omitempty"`
	Exported    int      `json:"exported"`
	Filled      int      `json:"filled"`
	Fillable    int      `json:"fillable"`
	Terminal    int      `json:"terminal"`
	Unknown     int      `json:"unknown"`
	IndexErrors int      `json:"index_errors"`
	Error       string   `json:"error,omitempty"`
	// Warnings are problems the run found with its own setup that did not stop
	// it (additive and optional: absent on older records and on a clean run).
	Warnings []RunWarning `json:"warnings,omitempty"`
	// ErrorKind names the refusal Error carries when it is one the app owner's
	// surfaces say in their own words (an ErrKind* constant), so the desktop's and
	// mailarchive-gui's health views need not print the CLI command and SQLite's
	// text recorded in Error. Absent on older records and on any other failure.
	ErrorKind string `json:"error_kind,omitempty"`
	// SourceReason is why a plaintext run was refused on a Thunderbird folder its account no longer
	// uses, as a fixed code with no name in it (docs/design-per-mailbox-archives.md §11 "The later
	// words for both stops", revision 2.1's F-03): SourceReasonMoved (the account now keeps its mail
	// in another folder) or SourceReasonRemoved (the account is gone). Status, the Your-archives
	// banner and the 30-day pause read it to say the reason's words in place of "reconnect the
	// source". Written in a plaintext refused record only; an encrypted record's allow-list never
	// holds it (MA-734). Additive: absent on every other record, which is written as before.
	SourceReason string `json:"source_reason,omitempty"`
	// NoIndex records that the run was told not to build the search index
	// (-index=false), so `status` does not warn that an archive deliberately
	// kept without search.db has lost it. Absent (false) on older records.
	NoIndex bool `json:"no_index,omitempty"`

	// The capture gaps this run observed (fault-isolation §4.2, additive, omitted
	// when zero): how many fillable and terminal ledger gaps it observed, how many
	// of them were new this run (what a monitor keys on, FC23(2)), how many earlier
	// gaps it recovered, how many fillable ones are stuck, and how many partly-read
	// records it wrote or re-examined.
	GapsObserved  int `json:"gaps_observed,omitempty"`
	GapsNew       int `json:"gaps_new,omitempty"`
	GapsRecovered int `json:"gaps_recovered,omitempty"`
	GapsFillable  int `json:"gaps_fillable,omitempty"`
	GapsTerminal  int `json:"gaps_terminal,omitempty"`
	GapsStuck     int `json:"gaps_stuck,omitempty"`
	Unreadable    int `json:"unreadable,omitempty"`

	// Partial is what a partial run could not save, by kind (design-plain-failure-words,
	// MA-1145; additive): set only on a plaintext run that ended partial, so every surface
	// tells it in the kinds' words. Status stays "failed" (the S1a mapping), and an encrypted
	// archive's record never carries it (its allow-list, encryptedLastRun).
	Partial *PartialCounts `json:"partial,omitempty"`
	// FillableFrom is the kind of mail the run read (a From* value, MA-1157; additive), so the
	// words for its messages not downloaded in full yet name a mail app only when it read one's
	// store. Absent on an older record, for a mix of kinds, and on an encrypted archive's.
	FillableFrom string `json:"fillable_from,omitempty"`

	// Undecodable and FidelityFallback (design-mime-fidelity §4.9, MC19; additive,
	// omitted when zero) are how many records, at the end of this run, carry a part this
	// version cannot decode (kept as received) and how many were written from the earlier
	// parser's content. Informational: neither makes the run partial.
	Undecodable      int `json:"undecodable,omitempty"`
	FidelityFallback int `json:"fidelity_fallback,omitempty"`

	// Presync is the Thunderbird pre-sync's trace in this run (design §4.9.2):
	// the in-flight "syncing" state while the session runs, then the final
	// outcome code and the streak counters. Additive: nil when the feature is off
	// or on an older record, so `status` reads a record without it as "not
	// configured" and an older binary ignores it.
	Presync *PresyncRecord `json:"presync,omitempty"`

	// Thunderbird is how much of each Thunderbird IMAP account of the run Thunderbird had
	// downloaded (the completeness design §7's records row, slice C2): plaintext runs that
	// ended ok only, additive, and never beside an encrypted archive (O1; encryptedLastRun
	// has no such field).
	Thunderbird []TBAccount `json:"thunderbird,omitempty"`
	// ThunderbirdMore is how many of the run's accounts the record left out past TBAccountsMax
	// (the C2 pass round 2's R2-06); the record keeps the weakest first. A reader that builds a
	// lead from the record takes each one left out as unknown (tbcomplete.RunLeadWith), never
	// as complete.
	ThunderbirdMore int `json:"thunderbird_more,omitempty"`
	// ThunderbirdNames is each Thunderbird folder's account name beside its store's id, in a record
	// whose run has no completeness entries of its own (a running, refused, failed or cancelled run,
	// or an ok one that assessed none): carried from the previous record (TBNamesOf), so a moved
	// account is still told from a removed one after a stopped backup (the 1.7.5 re-verify, round
	// 3's RV3-01; MA-1481). A small list beside Thunderbird, not a copy of it: every surface reads
	// Thunderbird as this run's completeness. Plaintext records only: the encrypted writer's
	// allow-list never writes it (the owner's Q3, Q5 and Q6). Names stay in the record and never
	// reach a log file (MA-1288). Additive: an older reader ignores it (1.7.4 reads the record with
	// json.Unmarshal).
	// Revision 2.2 of docs/design-per-mailbox-archives.md (MA-1504): an ok run of one listed folder
	// with auto-discovery off, which found no live account for that folder, also keeps here, beside
	// its entries, the name the previous record gave the folder; TBNamesOf reads it only for a store
	// whose own entry names no account.
	ThunderbirdNames []TBName `json:"thunderbird_names,omitempty"`
	// ThunderbirdIDs is, for each Thunderbird folder a plaintext run read, the fixed identity of the
	// account that owned it, beside the store's id (docs/design-per-mailbox-archives.md revision 3; the
	// adversarial pass's INS-03 to INS-06): a hash of the account's type, user name and server, never
	// the name or address itself. An ok run writes its own; every other record a run writes carries the
	// previous record's (TBIDsOf), and an ok run of one listed folder with auto-discovery off that found
	// no live account for that folder keeps the previous one for it, as ThunderbirdNames is kept
	// (revision 2.2). "Moved", "removed" and "reused" are decided by it where the record has one; a
	// record without one keeps revision 2.2's name rules. Plaintext records only: the encrypted
	// writer's allow-list never writes it (MA-734). Additive: an older reader ignores it.
	ThunderbirdIDs []TBID `json:"thunderbird_ids,omitempty"`

	// Program is an encrypted archive's program step outcome, one of the Program*
	// codes (program design §4, §8). Absent when the step did not run.
	Program string `json:"program,omitempty"`

	// Encrypted marks the record of an encrypted archive: WriteLastRun then writes
	// only the allow-listed fields (encryptedLastRun) and the attention notice says
	// the failure kind in fixed words (P2a-C16). The caller sets it — state does not
	// recognise an encrypted archive itself. Absent (false) on a plaintext record,
	// whose bytes it leaves unchanged.
	Encrypted bool `json:"encrypted,omitempty"`

	// MailboxGone, MailboxGoneSince and MailboxGoneCount are an ENCRYPTED archive's record of a
	// gone mailbox (docs/design-1.7.5-fixes.md §1.3, MA-1478; owner, 2026-11-20, Q3, Q5 and Q6):
	// this run skipped a mailbox that is gone, the time one was first found gone, and how many it
	// skipped. No path, no name. Set only by a run of an encrypted archive, from its skip warnings
	// held in memory; a plaintext record never sets them (its warnings say each mailbox), so it is
	// written byte for byte as before. encryptedLastRun writes them only when MailboxGone is true.
	MailboxGone      bool      `json:"mailbox_gone,omitempty"`
	MailboxGoneSince time.Time `json:"mailbox_gone_since,omitzero"`
	MailboxGoneCount int       `json:"mailbox_gone_count,omitempty"`
}

// PresyncRecord is the Thunderbird pre-sync's record in one run (design §4.9.2).
// It is display and judgement only — never read to drive behaviour.
//
// A plaintext run writes the session's outcome here (design-presync-attended §4.2,
// slice AS7, C-80): its Code, Name, Result and Since only. Nothing says how the run
// was started (no "attended", C-36), and an encrypted archive's record never holds
// it (§4.4). State, ExpectedUntil and the two streaks are not written; they are kept
// so a record that has them still reads. The times are left out when zero, so a
// session that started no Thunderbird writes no since.
type PresyncRecord struct {
	State         string    `json:"state,omitempty"`         // "syncing" in flight; "" at the final outcome
	Code          string    `json:"code,omitempty"`          // the outcome code (presync_*); "" for a clean ok
	Name          string    `json:"name,omitempty"`          // the outcome's name, shown only through presync.TagPhrase
	Result        string    `json:"result,omitempty"`        // one of the PresyncResult* values
	Since         time.Time `json:"since,omitzero"`          // when the session started Thunderbird
	ExpectedUntil time.Time `json:"expected_until,omitzero"` // when it should be done
	WarnStreak    int       `json:"warn_streak,omitempty"`
	NoteStreak    int       `json:"note_streak,omitempty"`
}

// The PresyncRecord.Result values (design-presync-attended §4.2): a session whose
// outcome counts as synced (§5.2), one that does not, a cancelled one whatever it
// left open, and a wait the person stopped (OQ-6), which never reads as a failure.
const (
	PresyncResultSynced    = "synced"
	PresyncResultNotSynced = "not_synced"
	PresyncResultCancelled = "cancelled"
	PresyncResultWaitEnded = "wait_ended"
)

// RunWarning is one problem a run found with its own setup that does not stop
// it — a credential, or MailArchive Desktop's settings and sign-in, kept inside
// the archive (design-local-confidentiality P10). The run records it and
// `status` WARNs it by Code; it never latches, because the next run that no
// longer has the problem records none.
type RunWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Path, Name and Since are a mailbox_gone warning's (MailboxGoneCode, docs/design-1.7.5-
	// fixes.md §1.3, MA-1472): the input as the job lists it, the account's name when the
	// profile's settings still named one ("" otherwise), and when it was first found gone.
	// Additive and left out when empty, so a warning without them is written byte for byte as
	// before (Since is omitzero: omitempty does not leave out a zero time).
	Path  string    `json:"path,omitempty"`
	Name  string    `json:"name,omitempty"`
	Since time.Time `json:"since,omitzero"`
	// Listed: Thunderbird's settings still list an account for the folder when the run looked,
	// so status says to let Thunderbird download it, not to add it back (the 1.7.5 friction
	// review's F4). Amended beside the fields above: Name may now also come from the previous
	// record (F3), where Listed is false. Additive and left out when false.
	Listed bool `json:"listed,omitempty"`
	// SettingsUnsure: the profile's Thunderbird settings could not be read for certain, so a move
	// could not be told; status says so (the 1.7.5 re-verify's RV-04). Additive, left out when false.
	SettingsUnsure bool `json:"settings_unsure,omitempty"`
}

// MailboxGoneCode is the warning a plaintext run records for each listed input it skipped
// because it is gone (MA-1471, MA-1472). Status builds its words from Path, Name and Since, and
// never shows the recorded Message for it.
const MailboxGoneCode = "mailbox_gone"

// MaxGoneWarnings is how many of a record's mailbox_gone warnings status says and a run reads when
// it carries first-found dates (the 1.7.5 adversarial pass's INS-06): a planted or damaged record
// may hold thousands; status says the rest as one count. As the completeness record caps its
// accounts.
const MaxGoneWarnings = 64

// MailboxGoneNoteAfter is how long a gone mailbox's WARN lasts before it becomes a note: the
// owner's ruling of 2026-11-20 (docs/design-1.7.5-fixes.md §0, Q2), 14 days counted between
// backups, the last run's start minus when the mailbox was first found gone, never compared with
// now, as SurfaceAfter and AutoPauseAfter are.
const MailboxGoneNoteAfter = 14 * 24 * time.Hour

// LastRunState says what ReadLastRun found.
type LastRunState int

const (
	LastRunAbsent LastRunState = iota
	LastRunUnreadable
	LastRunPresent
)

// WriteLastRun writes the record atomically into out. Its rename waits out a
// moment of Windows file sharing (replaceFile) — `status` reading the record
// while a run finalizes must not cost the run its "ok" — and its callers keep
// it best effort: a refusal that persists is returned, never fails a run.
func WriteLastRun(out string, r LastRun) error {
	r.Version = 1
	data, err := marshalLastRun(r)
	if err != nil {
		return err
	}
	path := filepath.Join(out, LastRunName)
	tmp, err := os.CreateTemp(out, ".mailarchive-lastrun-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := replaceFile(tmp.Name(), path); err != nil {
		return err
	}
	updateAttentionSidecar(out, r)
	return nil
}

// updateAttentionSidecar writes (on a failed run) or removes (on an ok run) the
// human-facing BACKUP-NEEDS-ATTENTION.txt at the archive root. A running or
// cancelled record leaves it as-is — a failure stays flagged until the next
// successful run clears it. Best effort: the archive is intact without it.
func updateAttentionSidecar(out string, r LastRun) {
	path := filepath.Join(out, AttentionName)
	switch r.Status {
	case RunFailed:
		if r.Encrypted {
			_ = writeSidecarAtomic(path, []byte(encryptedAttention(r)))
			return
		}
		reason := r.Error
		if reason == "" {
			reason = "the run failed (no error text was recorded)"
		}
		if r.Partial != nil && !r.Partial.Empty() {
			// A partial run saved everything else it found: say what happened first, then the
			// kinds' words with their remedies (design-plain-failure-words, MA-1146).
			_ = writeSidecarAtomic(path, []byte(partialAttention(out, r)))
			return
		}
		body := fmt.Sprintf("This email backup FAILED and needs your attention.\n\n"+
			"Archive: %s\n"+
			"When:    %s\n"+
			"Reason:  %s\n\n"+
			"The messages already in this folder are unaffected — open index.html in a\n"+
			"web browser to read them. This notice is about the most recent backup run\n"+
			"only: it means new mail may not have been added, not that the archive is\n"+
			"damaged.\n\n"+
			"What to do: open MailArchive Desktop and look at Status & health, or run\n"+
			"  mailarchive status -out %s\n"+
			"It says what went wrong and how to fix it. If this computer no longer has\n"+
			"MailArchive, download it from its official page:\n"+
			"  https://github.com/alexdimarco/mail-archive-tool/releases\n"+
			"This file is removed automatically after the next backup that succeeds.\n",
			out, noticeTime(r.Finished), reason, typedPath(out, runtime.GOOS))
		_ = writeSidecarAtomic(path, []byte(body))
	case RunOK:
		_ = os.Remove(path)
	}
}

// partialAttention is BACKUP-NEEDS-ATTENTION.txt after a partial plaintext run: what happened
// first (the backup did not save everything, and the rest was saved), then what it could not
// save, by kind, each with its remedy, the report named as the details.
func partialAttention(out string, r LastRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This email backup did not save everything.\n\n"+
		"Archive: %s\n"+
		"When:    %s\n\n"+
		"Everything else it found was saved, and the messages already in this folder\n"+
		"are unaffected — open index.html in a web browser to read them. What it could\n"+
		"not save, and what to do:\n\n", out, noticeTime(r.Finished))
	for _, s := range PartialSentences(*r.Partial) {
		b.WriteString("  - " + s + "\n")
	}
	fmt.Fprintf(&b, "\n%s\n\n"+
		"MailArchive Desktop's Status & health, or\n"+
		"  mailarchive status -out %s\n"+
		"says the same with each message named. If this computer no longer has\n"+
		"MailArchive, download it from its official page:\n"+
		"  https://github.com/alexdimarco/mail-archive-tool/releases\n"+
		"This file is removed automatically after the next backup that succeeds.\n", PartialDetails, typedPath(out, runtime.GOOS))
	return b.String()
}

// writeSidecarAtomic writes data to path via a temp file and rename, so a torn
// write never leaves a half-written attention notice. The rename waits out a
// moment of Windows file sharing (replaceFile): a notice open in a viewer must
// not keep a stale one standing.
func writeSidecarAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mailarchive-attention-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return replaceFile(tmp.Name(), path)
}

// ReadLastRun reads the record, distinguishing absent from unreadable (a torn
// or hand-edited file) so a status surface can fail closed on each.
func ReadLastRun(out string) (LastRun, LastRunState, error) {
	var r LastRun
	// Read as a planted file may be: never through a link, never waiting on a pipe,
	// never whole past its bound (the 2b-4 residual, phase3 §6 E8) — status runs it
	// unattended.
	data, err := util.ReadRegularFile(filepath.Join(out, LastRunName), maxLastRunSize)
	if errors.Is(err, fs.ErrNotExist) {
		return r, LastRunAbsent, nil
	}
	if err != nil {
		return r, LastRunUnreadable, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, LastRunUnreadable, err
	}
	// The record is writable by anyone who can write the archive directory and
	// its strings are rendered into terminals and into pasteable remedies
	// (health.scheduleCommand builds a `schedule … -install` line from Job).
	// Strip control characters at this choke point so an ANSI escape or an
	// embedded newline cannot forge output on any consumer (INS2-2).
	for i := range r.Job {
		r.Job[i] = util.StripControl(r.Job[i])
	}
	r.Error, r.Exe, r.Mode = util.StripControl(r.Error), util.StripControl(r.Exe), util.StripControl(r.Mode)
	for i := range r.Warnings {
		r.Warnings[i].Code, r.Warnings[i].Message = util.StripControl(r.Warnings[i].Code), util.StripControl(r.Warnings[i].Message)
		// A gone mailbox's path and account name are said by status (MA-1472): cleaned here too.
		// Amended by the 1.7.5 adversarial pass (INS-06): the name as the completeness record's
		// account names are (CleanFolderName: no bidi overrides or invisible characters, capped), the
		// path with no bidi override.
		r.Warnings[i].Path = util.StripBidi(util.StripControl(r.Warnings[i].Path))
		r.Warnings[i].Name = CleanFolderName(util.StripControl(r.Warnings[i].Name))
	}
	// A planted record's kinds are said only when they are counts (design-plain-failure-words).
	r.Partial = r.Partial.sane()
	r.FillableFrom = FillableFrom(r.FillableFrom) // a closed set (MA-1157)
	r.SourceReason = saneSourceReason(r.SourceReason)
	// The sync's strings too (design-presync-attended §4.2, C-37): every surface shows
	// its name only through a closed map, and none of them is echoed raw.
	if p := r.Presync; p != nil {
		p.State, p.Code = util.StripControl(p.State), util.StripControl(p.Code)
		p.Name, p.Result = util.StripControl(p.Name), util.StripControl(p.Result)
	}
	// The completeness record's names are server text and its counts may be planted: cleaned,
	// bounded and never negative here, at the same choke point.
	var cut int
	r.Thunderbird, cut = saneTB(r.Thunderbird)
	r.ThunderbirdMore = min(max(0, r.ThunderbirdMore), 1<<20) + cut // planted: never negative, never wrapped
	r.ThunderbirdNames = saneTBNames(r.ThunderbirdNames)
	r.ThunderbirdIDs = saneTBIDs(r.ThunderbirdIDs)
	return r, LastRunPresent, nil
}

// The LastRun.SourceReason codes (F-03): a closed set, with no name in either.
const (
	SourceReasonMoved   = "moved"
	SourceReasonRemoved = "removed"
)

// saneSourceReason is a record's source_reason as read: one of the closed set, else "" (a planted or
// damaged record keeps today's words).
func saneSourceReason(r string) string {
	switch r {
	case SourceReasonMoved, SourceReasonRemoved:
		return r
	}
	return ""
}

// LastVerify is the archive-local record of the most recent `verify`: written
// "running" the moment verify holds the archive lock and commits to checking,
// then finalized with the verdict (attested + the per-category counts + the
// process exit code). It is separate from LastRun so a scheduled verify's
// result reaches `status` without touching the export record.
type LastVerify struct {
	Version    int        `json:"version"`
	Status     string     `json:"status"` // running | done
	Started    time.Time  `json:"started"`
	Finished   *time.Time `json:"finished"` // null while running
	Attested   bool       `json:"attested"`
	Records    int        `json:"records"`
	WithFixity int        `json:"with_fixity"`
	Checked    int        `json:"checked"`
	OK         int        `json:"ok"`
	Modified   int        `json:"modified"`
	Missing    int        `json:"missing"`
	Unrecorded int        `json:"unrecorded"`
	Unexpected int        `json:"unexpected"`
	Recorded   int        `json:"recorded"`
	ExitCode   int        `json:"exit_code"`
	// Outcome is an encrypted archive's keyless verdict, one of the closed list
	// (Outcome*); Encrypted says the record is one, written in its allow-listed
	// shape only (A18, encrypted_verify.go). Both absent from a plaintext record.
	Outcome   string `json:"outcome,omitempty"`
	Encrypted bool   `json:"encrypted,omitempty"`
	// Tier is an encrypted archive's check: 1 without the key, 2 with it (phase3
	// §8); absent from a plaintext record and from one written before tiers were.
	Tier int `json:"tier,omitempty"`
	// LastChecked is when a check without the key last finished while a red a check
	// with the key found stood: that red is kept with its own times, so staleness
	// reads this (the P3-2b pass, REC-01). Encrypted records only.
	LastChecked *time.Time `json:"last_checked,omitempty"`
	// Standing is a red a check with the key found, kept beneath a newer red a check
	// without the key found: neither hides the other, and it returns to the top once
	// that newer red is gone (the P3-2b pass, REC-06). Encrypted records only.
	Standing *LastVerify `json:"standing,omitempty"`
	// Previous carries the last FINISHED verdict inside a "running" record, so
	// a verify that never finishes (crash, kill, power loss) cannot erase it:
	// status keeps judging it until a new verdict is final (F79). Absent on a
	// done record and when no verify ever finished.
	Previous *LastVerify `json:"previous,omitempty"`
}

// LastVerdict returns the most recent FINISHED verify verdict recorded in out
// — the done record itself, or the one a running record carries — with no
// further nesting, or nil when there is none. A new verify stores it in its
// running record before it starts checking.
func LastVerdict(out string) *LastVerify {
	v, st, _ := ReadLastVerify(out)
	if st != LastVerifyPresent {
		return nil
	}
	if v.Status != VerifyDone {
		return v.Previous
	}
	v.Previous = nil
	return &v
}

// flatStanding is a standing verdict with nothing nested in it.
func flatStanding(s *LastVerify) *LastVerify {
	if s == nil {
		return nil
	}
	c := *s
	c.Previous, c.Standing = nil, nil
	return &c
}

// StandingRed is the red a check with the key found that still stands in v (the
// owner's ruling, design-encrypted-archives-phase3 §8): v itself when it is one,
// else the one it keeps beneath a newer red; nil when none stands.
func StandingRed(v *LastVerify) *LastVerify {
	switch {
	case v == nil:
		return nil
	case v.Status == VerifyDone && TierOf(v) == TierFull && OutcomeIsRed(v.Outcome):
		return flatStanding(v)
	}
	return flatStanding(v.Standing)
}

// CheckedAt is when v's check last finished: its own finish, or a later check
// without the key that kept it standing (LastChecked); zero while none finished.
func (v LastVerify) CheckedAt() time.Time {
	var t time.Time
	if v.Finished != nil {
		t = *v.Finished
	}
	if v.LastChecked != nil && v.LastChecked.After(t) {
		t = *v.LastChecked
	}
	return t
}

// LastVerify statuses. A stopped check was ended on purpose (Ctrl-C, or Desktop's
// Cancel; phase3 §8): like a running record it carries the last finished verdict
// and has no outcome of its own.
const (
	VerifyRunning = "running"
	VerifyDone    = "done"
	VerifyStopped = "stopped"
)

// WriteLastVerify writes the verify record atomically into out and, when the
// record is finalized, updates verify's own integrity sidecar. Its rename waits
// out a moment of Windows file sharing (replaceFile), as WriteLastRun's does.
func WriteLastVerify(out string, v LastVerify) error {
	v.Version = 1
	v.Status = util.StripControl(v.Status)
	v.Standing = flatStanding(v.Standing)
	if v.Previous != nil {
		p := *v.Previous
		p.Status, p.Previous = util.StripControl(p.Status), nil
		p.Standing = flatStanding(p.Standing)
		v.Previous = &p
	}
	data, err := marshalLastVerify(v)
	if err != nil {
		return err
	}
	if err := writeLastVerifyFile(out, data); err != nil {
		return err
	}
	if v.Status == VerifyDone {
		updateIntegritySidecar(out, v)
	}
	return nil
}

// writeLastVerifyFile replaces the last-verify record with data, atomically.
func writeLastVerifyFile(out string, data []byte) error {
	tmp, err := os.CreateTemp(out, ".mailarchive-lastverify-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return replaceFile(tmp.Name(), filepath.Join(out, LastVerifyName))
}

// SnapshotLastVerify keeps the last-verify record as it is now and returns what
// puts it back exactly: its own bytes, or no record when there was none — so a
// check whose key was locked part way records nothing, its running record
// included (design-encrypted-archives-phase3 §8; the P3-2b pass, KEY-01, REC-02).
// A record that could not be read is not rewritten from what was read of it: the
// running record is removed, and the archive has no record.
func SnapshotLastVerify(out string) (putBack func() error) {
	path := filepath.Join(out, LastVerifyName)
	data, err := util.ReadRegularFile(path, maxLastVerifySize)
	if err != nil {
		return func() error {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		}
	}
	return func() error { return writeLastVerifyFile(out, data) }
}

// updateIntegritySidecar writes verify's ARCHIVE-INTEGRITY-ATTENTION.txt when a
// finalized verify found modified or missing files (real corruption, a RED
// posture) and removes it when a verify attests. An unrecorded-only result
// (not attested, but nothing modified or missing) leaves any existing sidecar
// as it is: that is a coverage gap, not corruption. Best effort.
func updateIntegritySidecar(out string, v LastVerify) {
	path := filepath.Join(out, IntegrityAttentionName)
	switch {
	case v.Encrypted && OutcomeIsRed(v.Outcome):
		_ = writeSidecarAtomic(path, []byte(encryptedIntegrityAttention(v)))
	case v.Encrypted && v.Outcome == OutcomeIntact:
		_ = os.Remove(path)
	case v.Encrypted && TierOf(&v) == TierFull && v.Outcome == OutcomeNotAttested && v.Standing == nil:
		// A check with the key replaces the verdict before it, and opened every message
		// intact: an earlier red notice no longer holds (the P3-2b pass, REC-03). One
		// that kept a red standing did not, and the notice stays (CRIT-04).
		_ = os.Remove(path)
	case v.Encrypted:
		// not_attested or newer_format: no new finding, so an earlier notice stays.
	case v.Modified+v.Missing > 0:
		when := ""
		if v.Finished != nil {
			when = v.Finished.UTC().Format(time.RFC3339)
		}
		body := fmt.Sprintf("This email archive has integrity problems and needs your attention.\n\n"+
			"Archive:  %s\n"+
			"Checked:  %s\n"+
			"Findings: %d file(s) modified · %d file(s) missing\n\n"+
			"Some archived files no longer match the checksums recorded when they were\n"+
			"written. What to do:\n"+
			"  - Restore the affected files from a backup, or re-export them from the\n"+
			"    original mail source.\n"+
			"  - Then run\n"+
			"      mailarchive verify -out %s\n"+
			"    again — it names each affected file and confirms the archive is intact.\n"+
			"This file is removed automatically once a verify attests (every file intact).\n",
			out, when, v.Modified, v.Missing, typedPath(out, runtime.GOOS))
		_ = writeSidecarAtomic(path, []byte(body))
	case v.Attested:
		_ = os.Remove(path)
	}
}

// LastVerifyState says what ReadLastVerify found.
type LastVerifyState int

const (
	LastVerifyAbsent LastVerifyState = iota
	LastVerifyUnreadable
	LastVerifyPresent
)

// ReadLastVerify reads the verify record, distinguishing absent from unreadable
// so a status surface can fail closed on each.
//
// The record is writable by anyone who can write the folder, so it is read as one
// would read a planted file: a link or a pipe at its name is unreadable, never
// followed or waited on, and the read is bounded (the adversarial pass, OUT-08).
// Its status is one of the two a verify writes — a carried verdict is a finished
// one — and an encrypted record's finished verdict is one of the closed list;
// anything else is not a record status can judge, and no text of it is echoed
// (LEAK-02, LEAK-03).
func ReadLastVerify(out string) (LastVerify, LastVerifyState, error) {
	var v LastVerify
	data, err := util.ReadRegularFile(filepath.Join(out, LastVerifyName), maxLastVerifySize)
	if errors.Is(err, fs.ErrNotExist) {
		return v, LastVerifyAbsent, nil
	}
	if err != nil {
		return v, LastVerifyUnreadable, err
	}
	if json.Unmarshal(data, &v) != nil {
		// Never the decoder's own words: they quote the record's text, a time field's
		// included (the fix re-verification, FV-R-01).
		return v, LastVerifyUnreadable, errors.New("it is not a record MailArchive writes")
	}
	if v.Previous != nil {
		v.Previous.Previous = nil
		v.Previous.Standing = flatStanding(v.Previous.Standing)
	}
	v.Standing = flatStanding(v.Standing)
	if err := checkVerdicts(v); err != nil {
		return v, LastVerifyUnreadable, err
	}
	return v, LastVerifyPresent, nil
}

// maxLastVerifySize bounds the last-verify record: a plaintext one carrying a
// previous verdict is under 2 KiB.
const maxLastVerifySize = 64 << 10

// maxLastRunSize bounds the last-run record a reader takes in. A real one is a few
// kilobytes; its warnings list is bounded by the run.
const maxLastRunSize = 4 << 20

// checkVerdicts refuses a record a verify does not write: a status other than
// running or done, a carried verdict that is not a finished one, and on an
// encrypted record a finished verdict whose outcome is not in the closed list. A
// running record has no outcome yet.
func checkVerdicts(v LastVerify) error {
	if v.Status != VerifyRunning && v.Status != VerifyDone && v.Status != VerifyStopped {
		return errors.New("its status is not one MailArchive writes")
	}
	if v.Previous != nil && v.Previous.Status != VerifyDone {
		return errors.New("the verdict it carries is not a finished one")
	}
	for _, o := range []*LastVerify{&v, v.Previous} {
		if o == nil || !o.Encrypted {
			continue
		}
		if (o.Status == VerifyDone && !KnownOutcome(o.Outcome)) || (o.Status != VerifyDone && o.Outcome != "") {
			return errors.New("its verify outcome is not one MailArchive writes")
		}
		if o.Tier != 0 && o.Tier != TierKeyless && o.Tier != TierFull {
			return errors.New("its check's tier is not one MailArchive writes")
		}
		// A standing verdict is only ever a finished red a check with the key found.
		if s := o.Standing; s != nil && (!s.Encrypted || s.Status != VerifyDone || s.Tier != TierFull || !OutcomeIsRed(s.Outcome)) {
			return errors.New("the verdict it keeps standing is not one MailArchive writes")
		}
	}
	for _, o := range []*LastVerify{&v, v.Previous} {
		if o != nil && !o.Encrypted && (o.Standing != nil || o.LastChecked != nil) {
			return errors.New("its verify record is not one MailArchive writes")
		}
	}
	return nil
}
