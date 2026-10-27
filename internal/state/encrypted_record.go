package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// The failure kinds an encrypted archive's run records, alerts and notices carry
// INSTEAD of error text (design-encrypted-archives-phase2a §4, P2a-C16): a CLOSED
// list, so no source path, mailbox address, store name or server text can reach a
// file beside an encrypted archive through an error message. app.FailureKind maps
// every run error onto it; anything it does not recognise is FailRunFailed.
const (
	FailEncryptedNeedsKey  = "encrypted_needs_key"  // a run without the archive's key
	FailEncryptedRefused   = "encrypted_refused"    // the key, vault, manifest or units do not allow a safe write
	FailNoSources          = "no_sources"           // discovery found no mail to read
	FailSourceUnreachable  = "source_unreachable"   // an -input is gone (an unplugged drive, a moved folder)
	FailSourceUnreadable   = "source_unreadable"    // an -input or one of its files cannot be read
	FailArchiveUnwritable  = "archive_unwritable"   // the archive's folder or volume cannot be written
	FailArchiveAbsent      = "archive_absent"       // the archive's folder or drive is not there (an unplugged drive)
	FailScratch            = "scratch_unwritable"   // MailArchive's working folder (for -copy-first copies) is full or cannot be written
	FailArchiveBusy        = "archive_busy"         // another run holds the archive
	FailLockLost           = "lock_lost"            // the archive lock was taken away mid-run
	FailManifestUnreadable = "manifest_unreadable"  // the archive's record of its messages cannot be read
	FailManifestSave       = "manifest_save_failed" // the run's progress could not be saved
	FailInventory          = "inventory_failed"     // the record was saved, but its file list was not (the next save retries)
	FailRecordOlder        = "record_older"         // the record is older than the archive around it (a restored older copy, F12)
	FailRecordNewer        = "record_newer"         // the archive was written by a newer MailArchive
	FailIndex              = "index_failed"         // the search index refused (its own kinds say which)
	FailSignIn             = "signin_failed"        // the Microsoft 365 sign-in or settings refused
	FailMailboxFailed      = "mailbox_failed"       // a mailbox could not be read
	FailPartial            = "partial"              // the run kept going but some messages are incomplete
	FailRunFailed          = "run_failed"           // any failure not named above
	// FailRecordUnusable: this computer's record of encrypted archives cannot be
	// used, so a folder holding MailArchive files is not written (phase3 §4.3).
	FailRecordUnusable = "encrypted_record_unusable"

	// NoteIndexBehind is the one kind a run that SUCCEEDED records: its search index
	// still misses messages a later run adds (P2b-C16; owner ruling 2026-10-25, "keep
	// and warn"), so status warns.
	NoteIndexBehind = "index_behind"
)

// failureText is each kind's one fixed sentence: the only words an encrypted
// archive's notices and alerts say about a failure.
var failureText = map[string]string{
	FailEncryptedNeedsKey:         "the archive is encrypted and this run did not have its key",
	FailEncryptedRefused:          "the archive is encrypted and its key or key file did not allow a safe backup",
	FailNoSources:                 "no mail to back up was found",
	FailSourceUnreachable:         "the mail being backed up could not be found (a disconnected drive or a moved folder)",
	FailSourceUnreadable:          "the mail being backed up could not be read",
	FailArchiveUnwritable:         "the archive's folder or drive could not be written",
	FailArchiveAbsent:             "the archive's drive or folder is not there — connect it",
	FailScratch:                   "MailArchive's working folder for copies of the mail is full or could not be written",
	FailArchiveBusy:               "another backup of this archive was running",
	FailLockLost:                  "another program took over the archive during the backup",
	FailManifestUnreadable:        "the archive's record of its messages could not be read",
	FailManifestSave:              "the backup's progress could not be saved",
	FailInventory:                 "this backup's file list could not be saved; the next backup retries",
	FailRecordOlder:               "the archive's record is older than the archive — an older copy may have been restored; nothing was changed",
	FailRecordNewer:               "the archive was written by a newer MailArchive — upgrade MailArchive; nothing was changed",
	FailIndex:                     "the archive's search index could not be used",
	ErrKindIndexBusy:              "another program was using the archive's search index",
	ErrKindIndexUnwritable:        "the archive's search index could not be written",
	ErrKindIndexDamaged:           "the archive's search index is damaged",
	ErrKindIndexDamagedNoManifest: "the archive's search index is damaged",
	ErrKindServiceUnavailable:     "Microsoft 365 or the network could not be reached",
	FailSignIn:                    "signing in to Microsoft 365 failed",
	FailMailboxFailed:             "a mailbox could not be read",
	FailPartial:                   "some messages could not be backed up completely",
	FailRunFailed:                 "the backup failed",
	FailRecordUnusable:            "MailArchive's record of encrypted archives on this computer cannot be used, so nothing was written — run `mailarchive status` to see the fix",
	NoteIndexBehind:               "the backup finished, but some of the archive's messages are not in its search index yet, so search does not find them — the next backup adds them; if the drive that holds MailArchive's working folder is full, free some space on it",
}

// FailureText is kind's fixed sentence; an unknown kind reads as FailRunFailed.
func FailureText(kind string) string {
	if t, ok := failureText[kind]; ok {
		return t
	}
	return failureText[FailRunFailed]
}

// The program step's outcome as an encrypted archive's record names it (program
// design §4, §8): a CLOSED list, like the failure kinds.
const (
	ProgramUpdated    = "program_updated"
	ProgramUnchanged  = "program_unchanged"
	ProgramIncomplete = "program_incomplete"
	ProgramSkipped    = "program_skipped"
	ProgramUnread     = "program_unread" // a set a newer MailArchive keeps
	ProgramStopped    = "program_stopped"
	ProgramCancelled  = "program_cancelled"
)

// programText is each program outcome's fixed sentence for status and the Desktop.
var programText = map[string]string{
	ProgramUpdated:    "the copies of MailArchive kept in this archive were updated",
	ProgramUnchanged:  "the copies of MailArchive kept in this archive are as they should be",
	ProgramIncomplete: "the copies of MailArchive kept in this archive are incomplete — a copy is missing, cannot be read, or something stands in its place; open the archive in MailArchive to see which",
	ProgramSkipped:    "the copies of MailArchive kept in this archive were not refreshed — its program folder is not a plain folder, or MailArchive ran from inside it",
	ProgramUnread:     "the copies of MailArchive in this archive are kept by a newer MailArchive and were left as they are",
	ProgramStopped:    "an update of the copies of MailArchive kept in this archive did not finish; the next backup finishes it",
	ProgramCancelled:  "the update of the copies of MailArchive kept in this archive was cancelled; the next backup tries again",
}

// ProgramText is a program outcome's fixed sentence, or "" for none.
func ProgramText(code string) string { return programText[code] }

// ProgramWarns says a program outcome is one status warns about.
func ProgramWarns(code string) bool {
	switch code {
	case ProgramIncomplete, ProgramSkipped, ProgramStopped:
		return true
	}
	return false
}

// encryptedLastRun is the ONLY shape an encrypted archive's last-run record takes on
// disk: the allow-list of frozen A6 — times, the outcome and the failure kind
// (P2a-C16) — and, by the program design's §8, the program step's outcome from its
// closed list. Counts, the job, the program path, warnings and error text are never
// written beside an encrypted archive.
type encryptedLastRun struct {
	Version   int       `json:"version"`
	Status    string    `json:"status"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished,omitempty"`
	ErrorKind string    `json:"error_kind,omitempty"`
	Program   string    `json:"program,omitempty"`
	Encrypted bool      `json:"encrypted"`
}

// marshalLastRun is the bytes WriteLastRun writes: r as it is, or — for an
// encrypted archive — only its allow-listed fields.
func marshalLastRun(r LastRun) ([]byte, error) {
	if !r.Encrypted {
		return json.MarshalIndent(r, "", "  ")
	}
	program := r.Program
	if _, known := programText[program]; !known {
		program = "" // only the closed list is ever written
	}
	return json.MarshalIndent(encryptedLastRun{
		Version: r.Version, Status: r.Status, Started: r.Started, Finished: r.Finished,
		ErrorKind: r.ErrorKind, Program: program, Encrypted: true,
	}, "", "  ")
}

// encryptedAttention is the attention notice of an encrypted archive: the time and
// the kind's fixed sentence, no archive path, error text or command line.
func encryptedAttention(r LastRun) string {
	return fmt.Sprintf("This email backup FAILED and needs your attention.\n\n"+
		"When:    %s\n"+
		"Reason:  %s.\n\n"+
		"This archive is encrypted. The messages already in it are unaffected — open\n"+
		"it in MailArchive with its password or recovery key to read them. This notice\n"+
		"is about the most recent backup run only: it means new mail may not have been\n"+
		"added, not that the archive is damaged.\n\n"+
		"What to do: open MailArchive and look at this archive's status for the\n"+
		"remedy. This file is removed automatically after the next run that succeeds.\n",
		r.Finished.UTC().Format(time.RFC3339), FailureText(r.ErrorKind))
}
