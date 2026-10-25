package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

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

	// Encrypted marks the record of an encrypted archive: WriteLastRun then writes
	// only the allow-listed fields (encryptedLastRun) and the attention notice says
	// the failure kind in fixed words (P2a-C16). The caller sets it — state does not
	// recognise an encrypted archive itself. Absent (false) on a plaintext record,
	// whose bytes it leaves unchanged.
	Encrypted bool `json:"encrypted,omitempty"`
}

// PresyncRecord is the Thunderbird pre-sync's record in one run (design §4.9.2).
// A session in flight carries State "syncing" and ExpectedUntil; a finished one
// carries the outcome Code. It is display and judgement only — never read to
// drive behaviour.
type PresyncRecord struct {
	State         string    `json:"state,omitempty"`          // "syncing" in flight; "" at the final outcome
	Code          string    `json:"code,omitempty"`           // the outcome code (presync_*); "" for a clean ok
	Since         time.Time `json:"since,omitempty"`          // when the session started
	ExpectedUntil time.Time `json:"expected_until,omitempty"` // when it should be done
	WarnStreak    int       `json:"warn_streak,omitempty"`
	NoteStreak    int       `json:"note_streak,omitempty"`
}

// RunWarning is one problem a run found with its own setup that does not stop
// it — a credential, or MailArchive Desktop's settings and sign-in, kept inside
// the archive (design-local-confidentiality P10). The run records it and
// `status` WARNs it by Code; it never latches, because the next run that no
// longer has the problem records none.
type RunWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

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
		body := fmt.Sprintf("This email backup FAILED and needs your attention.\n\n"+
			"Archive: %s\n"+
			"When:    %s\n"+
			"Reason:  %s\n\n"+
			"The messages already in this folder are unaffected — open index.html in a\n"+
			"web browser to read them. This notice is about the most recent backup run\n"+
			"only: it means new mail may not have been added, not that the archive is\n"+
			"damaged.\n\n"+
			"What to do: run\n"+
			"  mailarchive status -out %q\n"+
			"for the full posture and the remedy. To resume backups you need the\n"+
			"mailarchive program (a single self-contained executable) — get it from\n"+
			"wherever you originally obtained it. This file is removed automatically\n"+
			"after the next run that succeeds.\n",
			out, r.Finished.UTC().Format(time.RFC3339), reason, out)
		_ = writeSidecarAtomic(path, []byte(body))
	case RunOK:
		_ = os.Remove(path)
	}
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
	data, err := os.ReadFile(filepath.Join(out, LastRunName))
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
	}
	return r, LastRunPresent, nil
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

// LastVerify statuses.
const (
	VerifyRunning = "running"
	VerifyDone    = "done"
)

// WriteLastVerify writes the verify record atomically into out and, when the
// record is finalized, updates verify's own integrity sidecar. Its rename waits
// out a moment of Windows file sharing (replaceFile), as WriteLastRun's does.
func WriteLastVerify(out string, v LastVerify) error {
	v.Version = 1
	v.Status = util.StripControl(v.Status)
	if v.Previous != nil {
		p := *v.Previous
		p.Status, p.Previous = util.StripControl(p.Status), nil
		v.Previous = &p
	}
	data, err := marshalLastVerify(v)
	if err != nil {
		return err
	}
	path := filepath.Join(out, LastVerifyName)
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
	if err := replaceFile(tmp.Name(), path); err != nil {
		return err
	}
	if v.Status == VerifyDone {
		updateIntegritySidecar(out, v)
	}
	return nil
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
			"      mailarchive verify -out %q\n"+
			"    again — it names each affected file and confirms the archive is intact.\n"+
			"This file is removed automatically once a verify attests (every file intact).\n",
			out, when, v.Modified, v.Missing, out)
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
func ReadLastVerify(out string) (LastVerify, LastVerifyState, error) {
	var v LastVerify
	data, err := os.ReadFile(filepath.Join(out, LastVerifyName))
	if errors.Is(err, fs.ErrNotExist) {
		return v, LastVerifyAbsent, nil
	}
	if err != nil {
		return v, LastVerifyUnreadable, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, LastVerifyUnreadable, err
	}
	v.Status = util.StripControl(v.Status)
	if v.Previous != nil {
		v.Previous.Status, v.Previous.Previous = util.StripControl(v.Previous.Status), nil
	}
	// An encrypted record's verdict is one of the closed list, or it is not one
	// status can judge: the record is writable by anyone who can write the folder.
	for _, o := range []*LastVerify{&v, v.Previous} {
		if o != nil && o.Encrypted && o.Outcome != "" && !KnownOutcome(o.Outcome) {
			return v, LastVerifyUnreadable, fmt.Errorf("the verify outcome %q is not one MailArchive writes", util.StripControl(o.Outcome))
		}
	}
	return v, LastVerifyPresent, nil
}
