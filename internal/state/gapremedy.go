package state

import "strings"

// The words a capture gap is told in (fault-isolation §4.10). One table serves
// the run summary, `status`, the report and the extraction workspace's copy
// export, so the same fault reads the same on every surface (FC20); app.GapRemedy
// is its exported entry point. It lives here, below both app and health, because
// health must not import app.

// Errno classes a remedy is keyed by (FC23(6)); "" is any other cause.
const (
	ErrnoPerm    = "perm"    // EACCES, EPERM: permissions
	ErrnoIO      = "io"      // EIO: the disk returned an error
	ErrnoSpace   = "nospace" // ENOSPC, EDQUOT: the disk is full
	ErrnoTooLong = "toolong" // ENAMETOOLONG: a name or path the file system cannot hold
)

// GapErrnoClass reads the errno class out of a gap's Detail (the error text the
// producer recorded): the classes a remedy differs by. "" for anything else.
func GapErrnoClass(detail string) string {
	d := strings.ToLower(detail)
	switch {
	case strings.Contains(d, "permission denied"), strings.Contains(d, "operation not permitted"), strings.Contains(d, "access is denied"):
		return ErrnoPerm
	case strings.Contains(d, "input/output error"), strings.Contains(d, "i/o error"):
		return ErrnoIO
	case strings.Contains(d, "no space left"), strings.Contains(d, "disk quota"), strings.Contains(d, "not enough space"), strings.Contains(d, "disk is full"):
		return ErrnoSpace
	case strings.Contains(d, "file name too long"), strings.Contains(d, "filename too long"), strings.Contains(d, "name is too long"):
		return ErrnoTooLong
	}
	return ""
}

// GapDeadlineRemedy is shown at the FIRST sighting of a fillable Microsoft 365
// gap: a user who is about to lose the mailbox must not have to wait for the
// stuck threshold to learn there is a way to save the item (FC23(1)).
const GapDeadlineRemedy = "If you are about to lose access to this mailbox, save a copy from Outlook on the web now."

// scanpstRemedy is the repair advice for a damaged .pst: in place, or the copy
// at the SAME path, because the store token is seeded from the input path (a
// copy from another path would start a second archive tree).
const scanpstRemedy = "repair the file in place with Outlook's Inbox Repair Tool (scanpst.exe, Windows; it keeps a .bak backup), or put the repaired copy at the same path, then run again — a copy archived from a different path starts a separate archive tree"

// scratchRemedy is the remedy for an item lost from the .pst that -outlook's
// Outlook export writes: that file is rebuilt every run, so repairing it is
// pointless.
const scratchRemedy = "the item could not be read from Outlook's export; open it in Outlook (it is still in your mailbox), or try again"

// GapWhy says in plain words why a gap exists, by its reason (§4.1). The reason
// is the producer's; an unknown one reads generically.
func GapWhy(reason string) string {
	switch reason {
	case "fetch-failed":
		return "Microsoft 365 returned an error for this message"
	case "fetch-transport":
		return "the download from Microsoft 365 failed (a network or service problem)"
	case "fetch-timeout":
		return "Microsoft 365 did not deliver this message in time"
	case "fetch-too-large":
		return "the message is larger than the download limit"
	case "vanished-at-fetch":
		return "the message was deleted from the mailbox while the run was reading it"
	case "refresh-failed":
		return "a newer read of this item was only partly readable, so the archived copy was kept"
	case "folder-list-failed":
		return "the folder's message list could not be read from Microsoft 365"
	case "export-failed":
		return "the message could not be written to the archive"
	case "export-panic":
		return "mailarchive failed while archiving this message"
	case "folder-unwritable":
		return "the folder could not be written to the archive"
	case "folder-unreadable":
		return "the folder could not be read from the source"
	case "folder-partially-read":
		return "the folder could only be partly read from the source"
	case "vanished-during-read":
		return "the message file disappeared while the run was reading it"
	case "pst-message-unreadable":
		return "the message could not be read from the .pst file"
	case "pst-messages-unreadable":
		return "messages in this folder could not be read from the .pst file"
	case "pst-row-unreadable", "pst-row-invalid":
		return "damaged entries in this folder's table could not be read"
	case "file-unreadable":
		return "the message file could not be read"
	case "folder-cycle":
		return "the folder tree loops back on itself, so the repeated folder was not read again"
	case "folder-too-deep":
		return "the folder tree is nested too deeply, so the folders below the limit were not read"
	case "symlink-outside-store":
		return "a link in the store points outside it and was not followed"
	case "symlinked-folder-skipped":
		return "a folder link in the store was not followed"
	case "not-a-message-file":
		return "not a mail file (a pipe, socket or device): nothing is missing"
	case "outlook-export-skipped":
		return "Outlook could not export this folder"
	case "outlook-account-skipped":
		return "this account is already a .pst file"
	case "overflow":
		return "more items than the per-source limit could not be captured"
	}
	return "the item could not be captured"
}

// GapRemedy is what to do about a gap, keyed by its reason, the kind of source
// it came from (SourceGraph, SourcePST, SourceOutlookScratch, SourceLocal; "" is
// unknown) and the errno class of its cause (Err*; "" is any other). A reason
// that is the archive's destination ("could not be written") names the archive
// location, never "read" (FC20). Remedies never promise what the tool does not
// do: a fillable gap is retried whenever the archive runs again.
func GapRemedy(reason, sourceKind, errnoClass string) string {
	switch reason {
	case "fetch-failed":
		return "it is retried when you run again"
	case "fetch-transport", "fetch-timeout":
		return "check the network and that Microsoft 365 is reachable; it is retried when you run again"
	case "fetch-too-large":
		return "save a copy from Outlook on the web; this archive cannot hold a message that large"
	case "vanished-at-fetch":
		return "nothing to do unless it should still exist; the row closes on the next run"
	case "refresh-failed":
		return "nothing is lost: the earlier, complete copy is kept"
	case "folder-list-failed":
		return "run again; the messages in this folder are not checked for deletion until it can be read"
	case "export-failed":
		switch errnoClass {
		case ErrnoSpace:
			return "free space on the archive drive, then run again"
		case ErrnoIO:
			return "the disk returned a write error — check the archive drive, then run again"
		case ErrnoPerm:
			return "make the archive location writable, then run again"
		}
		return "check that the archive location is writable and has space, then run again"
	case "export-panic":
		return "this is a mistake in mailarchive, not in your mail: the original is untouched — please report it"
	case "folder-unwritable":
		switch errnoClass {
		case ErrnoTooLong:
			return "rename the folder in your mail app, or archive to an NTFS/APFS drive"
		case ErrnoPerm:
			return "make the archive location writable, then run again"
		case ErrnoSpace:
			return "free space on the archive drive, then run again"
		}
		return "check that the archive location is writable, then run again"
	case "vanished-during-read":
		return "nothing to do: your mail app moved or deleted the file; the row closes on the next run"
	case "file-unreadable":
		switch errnoClass {
		case ErrnoPerm:
			return "fix the permissions on the file, then run again"
		case ErrnoIO:
			return "the disk returned a read error — check the drive, then run again"
		}
		return "check the file, then run again"
	case "folder-unreadable", "folder-partially-read":
		switch {
		case sourceKind == SourcePST:
			return scanpstRemedy
		case sourceKind == SourceOutlookScratch:
			return scratchRemedy
		case errnoClass == ErrnoPerm:
			return "fix the permissions on the folder, then run again"
		case errnoClass == ErrnoIO:
			return "the disk returned a read error — check the drive, then run again"
		}
		return "check the folder in your mail app, then run again"
	case "pst-message-unreadable", "pst-messages-unreadable", "pst-row-unreadable", "pst-row-invalid", "folder-cycle", "folder-too-deep":
		if sourceKind == SourceOutlookScratch {
			return scratchRemedy
		}
		return scanpstRemedy
	case "symlink-outside-store", "symlinked-folder-skipped":
		return "links are not followed: copy the mail into the store, or archive its real location as its own input"
	case "not-a-message-file":
		return "nothing to do"
	case "outlook-export-skipped":
		return "open the folder in Outlook, then run again"
	case "outlook-account-skipped":
		return "archive that .pst file directly"
	case "overflow":
		return "see the report; fix the cause of the first failures, then run again"
	}
	return "see the report, then run again"
}

// GapStuckRemedy is the remedy once a fillable gap has failed on every run for
// the stuck threshold (§4.10): the reasons that a retry cannot fix say what to do
// instead. For a Microsoft 365 error the honest line is that the service will not
// export this item (FC18); the row stays listed while the item remains.
func GapStuckRemedy(reason, sourceKind, errnoClass string) string {
	if sourceKind == SourceGraph {
		switch reason {
		case "fetch-failed":
			return "Microsoft 365 will not export this item; save a copy from Outlook on the web. The row stays listed while the item remains in the mailbox"
		case "fetch-transport", "fetch-timeout":
			return "Microsoft 365 has not delivered this item on any run; save a copy from Outlook on the web. The row stays listed while the item remains in the mailbox"
		}
	}
	return GapRemedy(reason, sourceKind, errnoClass)
}
