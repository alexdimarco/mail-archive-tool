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

// PSTRepairRemedy is the repair advice for a damaged .pst: in place, or the copy
// at the SAME path, because the store token is seeded from the input path (a
// copy from another path would start a second archive tree).
const PSTRepairRemedy = "repair the file in place with Outlook's Inbox Repair Tool (scanpst.exe, Windows; it keeps a .bak backup), or put the repaired copy at the same path, then run again — a copy archived from a different path starts a separate archive tree"

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
		return "a newer read of this item failed or was only partly readable, so the archived copy was kept"
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
			return "rename it in your mail app, or archive to an NTFS/APFS drive"
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
			return PSTRepairRemedy
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
		return PSTRepairRemedy
	case "symlink-outside-store", "symlinked-folder-skipped":
		return "links are not followed: copy the mail into the store, or archive its real location as its own input"
	case "not-a-message-file":
		return "nothing to do"
	case "outlook-export-skipped":
		return "open the folder in Outlook, then run again"
	case "outlook-account-skipped":
		return "archive that .pst file directly"
	case "overflow":
		return remedyOverflow
	}
	return remedyUnknown
}

// The two remedies that point at the report; the report's own rows cannot.
const (
	remedyOverflow = "see the report; fix the cause of the first failures, then run again"
	remedyUnknown  = "see the report, then run again"
)

// GapReportRemedy is GapRemedy as the report's own row says it (friction R3): the
// two remedies that send the reader to the report would be circular there.
func GapReportRemedy(reason, sourceKind, errnoClass string) string {
	switch r := GapRemedy(reason, sourceKind, errnoClass); r {
	case remedyOverflow:
		return "fix the cause of the failures listed for this folder above, then run again; the rest are archived on the runs after that"
	case remedyUnknown:
		return "run again; if it is listed again, keep the original, because this version of mailarchive does not know the cause"
	default:
		return r
	}
}

// RecordRowWhy says in plain words why a record row of the report exists, by its
// kind (the report's first column), in the register of GapWhy (owner ruling §13.11,
// ESC-16): a record row used to carry only a bare code.
func RecordRowWhy(kind string) string {
	switch kind {
	case "missing-body":
		return "the message was archived without a body"
	case "unreadable-body":
		return "the message body could only be partly read from the source"
	case "unreadable-attachments":
		return "the attachments could only be partly read from the source"
	case "unparsed-message":
		return "the message could not be parsed, so its page shows only what could be read"
	case "empty-attachment":
		return "the attachment came out empty (nothing was downloaded, or its stream failed)"
	case "unknown":
		return "this message was archived by an older version that did not track whether anything was missing"
	case "superseded":
		return "a complete copy of this message was archived later, so this partly-read page no longer misses anything"
	case "unreadable-legacy-placeholder":
		return "an earlier version of mailarchive kept every message it could not read as this one page, so it stands for no message in particular"
	case "unresolved-inline-image":
		return "the message's HTML shows an inline image that is not in the message (common in replies and forwards)"
	case "html-shown-as-source":
		return "the message's HTML could not be shown safely as a page, so the page shows it as text"
	case GapKindUndecodedPart:
		return "the part uses a transfer encoding this version of mailarchive does not decode (for example x-uuencode), so it is kept exactly as it was received"
	case GapKindCorruptPart:
		return "the part's data is cut short or damaged, so only the readable start is kept, under a name ending .partial"
	case GapKindUnsplitMultipart:
		return "the message's parts could not be told apart (its boundary is missing or wrong), so its whole text is shown as one body"
	case GapKindStructureLimit:
		return "the message has more parts, or nests them deeper, than this version of mailarchive reads, so the rest is kept unread, exactly as it was received"
	case GapKindFidelityFallback:
		return "this version of mailarchive could not decode the message in full, so the page and attachments are as an earlier version made them"
	case GapKindIdentityStub:
		return "an earlier version of mailarchive could not read this message at all, so it is filed under the placeholder that version used; the page shows what this version reads"
	}
	return "this version of mailarchive does not know why this record has a gap"
}

// RecordRowRemedy is what to do about a record row, by its kind and class (the
// report's first and second columns): an honest "nothing to do" where there is none.
func RecordRowRemedy(kind, class string) string {
	const kept = "the original is kept"
	terminal := class == ClassTerminal
	switch kind {
	case "missing-body":
		if terminal {
			return "nothing to do: the source has no body to give; " + kept
		}
		return "download the message in your mail app, then run again; " + kept
	case "unreadable-body", "unreadable-attachments":
		if terminal {
			return "nothing to do: the source would not deliver it as it stands; " + kept
		}
		return "open the message in your mail app to check it, then run again; " + kept
	case "unparsed-message":
		return "nothing to do: " + kept
	case "empty-attachment":
		if terminal {
			return "nothing to do: the source cannot deliver it; " + kept
		}
		return "download it in your mail app, then run again; " + kept
	case "unknown":
		return "run again: it is checked once and this row closes; " + kept
	case "superseded":
		return "nothing to do: the complete copy is kept"
	case "unreadable-legacy-placeholder":
		return "nothing to do: when it reads the source again this version lists each message it cannot read as its own row of this report; this page is kept"
	case "unresolved-inline-image":
		return "nothing to do: it is usually not recoverable; " + kept
	case "html-shown-as-source":
		return "nothing to do: the words are kept and searchable; " + kept
	case GapKindUndecodedPart:
		return "nothing to do: the part is in the attachments zip as received (its name ends .undecoded), and a program that knows the encoding can open it; " + kept
	case GapKindCorruptPart:
		if terminal {
			return "nothing to do: the source cannot deliver a complete copy; the readable start is in the attachments zip (its name ends .partial); " + kept
		}
		return "download the complete message in your mail app, then run again; the readable start is in the attachments zip (its name ends .partial); " + kept
	case GapKindUnsplitMultipart:
		if terminal {
			return "nothing to do: the source cannot deliver a better copy; the text is on the page; " + kept
		}
		return "download the complete message in your mail app, then run again; the text is on the page; " + kept
	case GapKindStructureLimit:
		return "nothing to do: the rest is in the attachments zip as received (a name ending .undecoded); " + kept
	case GapKindFidelityFallback:
		return "nothing to do now: nothing is lost, and a later version of mailarchive reads the message again; if you see this often, please report it; " + kept
	case GapKindIdentityStub:
		return "nothing to do: the page shows the message as this version reads it; " + kept
	}
	return "run again; if it is listed again, keep the original, because this version of mailarchive does not know the cause"
}

// GapOriginal says whether the original still exists, by reason and source kind
// (friction F12): the owner's first question about an item "NOT in this archive"
// is whether it is lost or only not archived. "" when there is nothing true and
// useful to say (a deleted message, a non-message file, the scratch .pst whose
// remedy already says so, an unknown source).
func GapOriginal(reason, sourceKind string) string {
	switch reason {
	case "vanished-at-fetch", "vanished-during-read", "not-a-message-file", "refresh-failed":
		return ""
	case "export-failed", "export-panic", "folder-unwritable":
		return "the original is untouched"
	}
	switch sourceKind {
	case SourceGraph:
		return "the original is still in your mailbox"
	case SourcePST:
		return "the .pst file is the only copy: keep a copy of it before repairing"
	case SourceLocal:
		return "the original is still in the source folder"
	}
	return ""
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
		case "vanished-at-fetch":
			return "Microsoft 365 still lists this item but answers 'not found' when asked for it, on every run; save a copy from Outlook on the web. The row stays listed while the item remains in the mailbox"
		}
	}
	return GapRemedy(reason, sourceKind, errnoClass)
}

// GapStuckWhy is GapWhy for a gap that has failed on every run for the stuck
// threshold. A Graph item whose download answers 'not found' on every run was not
// deleted while the run was reading it: the mailbox still lists it (S3-AI5). Every
// other reason reads as it does before.
func GapStuckWhy(reason, sourceKind string) string {
	if sourceKind == SourceGraph && reason == "vanished-at-fetch" {
		return "the mailbox still lists this message, but Microsoft 365 reports it missing when asked for it"
	}
	return GapWhy(reason)
}
