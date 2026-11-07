package tbengine

import (
	"strings"

	"mail-archive-tool/internal/tbrestore"
)

// The engine's words (design §6.2, §6.4, §7.1, §7.2, §7.5 and their folded rows). The
// owner-delegated rule: what happened first, then the remedy. Placeholders in capitals
// are filled here. Rows marked "the builder's" are slice R6's, where the design names a
// refusal or a line but gives no words.
const (
	wElevated = "MailArchive is running as administrator, so the restore did not start: files it wrote could not be changed by Thunderbird afterwards. Start MailArchive normally and try again."
	// wElevatedUnix: the same refusal for root on macOS and Linux (the builder's).
	wElevatedUnix = "MailArchive is running as root, so the restore did not start: files it wrote could not be changed by Thunderbird afterwards. Run it as yourself, without sudo, and try again."

	wBusy   = "A backup of this archive is running, so the restore did not start. Wait until it finishes (its row in Your archives says when), then click Restore again."
	wLocked = "This archive is locked, so the restore did not start. Unlock it, then click Check again."
	wTick   = "The restored mail will not be encrypted, and the box saying you understand was not ticked, so nothing was restored. Tick it and click Restore again."
	// wNotAgain: ask-twice was owed and not given (the builder's).
	wNotAgain = "Thunderbird's Local Folders may leave this computer (REASONS), and the restored mail will not be encrypted. That was not confirmed a second time, so nothing was restored. Confirm it again, or choose a profile on this computer's own drive."
	wPST      = "This archive was made from Outlook data files, which keep no original messages, so none of it can go back into Thunderbird."
	wNoRaw    = "This archive was made without keeping the original messages, so none of it can go back into Thunderbird. Turn on keeping originals in Settings so that future backups can be restored."
	wSpace    = "There is not enough free space on DRIVE for this restore: it needs about X and DRIVE has Y free. Free some space, or choose fewer folders, then click Check again."
	// wSpaceUnknown: the free space could not be read (the builder's, the refusing direction).
	wSpaceUnknown = "MailArchive could not read how much free space DRIVE has (WHY), so nothing was restored. Check that the drive is connected, then click Check again."
	wFAT32        = "FOLDER is SIZE, too big for the drive that holds Local Folders: that drive uses the FAT32 format, which cannot hold a file of 4 GB or more. It is not restored. Choose fewer folders, or move Local Folders to a drive formatted another way."
	wTooMany      = "This restore would make N folders in Thunderbird, more than MailArchive makes in one restore (10,000), so nothing was restored. Choose fewer folders."
	wTooDeep      = "This restore would make folders N levels deep, more than MailArchive makes in one restore (300), so nothing was restored. Leave out the most deeply nested folders."
	// The builder's: an archive folder that is not there or holds no archive.
	wNoArchive = "There is no archive at OUT (WHY), so nothing was restored. Check the folder's path."
	// The builder's: a -profile-name that names no usable profile, or several.
	wNoProfName  = "No Thunderbird profile on this computer is named 'NAME', so nothing was restored. Run `mailarchive restore-thunderbird -list-profiles` to see the names, or name the profile's folder with -profile."
	wProfNames   = "More than one Thunderbird profile on this computer is named 'NAME', so nothing was restored. Name the profile's folder with -profile instead (`mailarchive restore-thunderbird -list-profiles` lists them)."
	wChooseProf  = "This computer has more than one Thunderbird profile (NAMES), so MailArchive did not pick one. Choose one with -profile-name NAME or -profile FOLDER."
	wNoFolder    = "The archive has no folder 'FOLDER' (as its tree spells it), so nothing was restored. Leave out -folder to restore the whole archive, or check the folder's name."
	wTwoStores   = "More than one part of this archive has a folder 'FOLDER' (STORES), so nothing was restored. Add -store with one of those names."
	wInsideEnc   = "Thunderbird's Local Folders (PATH) is inside an encrypted archive, so nothing was restored: the restored mail would not be encrypted there. Choose another profile."
	wStopEarly   = "The restore was stopped before it began, so nothing was written."
	wWentAhead   = " Your new restore went ahead."
	wWillGoAhead = " Your restore will go ahead."
	// wNotEncrypted, as settled 2026-11-07 [R6-01]: it said "anyone who can open this
	// Thunderbird profile", not who that is; the restore now makes its files readable by
	// this account alone, and says so.
	wNotEncrypted = "The restored mail in Thunderbird is NOT encrypted. MailArchive makes its files readable only by your account on this computer, so anyone who can sign in as you, or open this Thunderbird profile, can read it. If you stop the restore or it fails, MailArchive removes what it wrote, but that cannot undo a copy already uploaded by a sync service, kept in a backup or snapshot, or left on the disk."
	// wNotEncryptedShared: the same, where the drive cannot limit who reads a file (R6-01).
	wNotEncryptedShared = "The restored mail in Thunderbird is NOT encrypted, and this drive cannot limit who reads it: other accounts can read it as well as yours. If you stop the restore or it fails, MailArchive removes what it wrote, but that cannot undo a copy already uploaded by a sync service, kept in a backup or snapshot, or left on the disk."
	// The lead's words (owner-delegated), settled 2026-11-07:
	// wReaders (R6-01) is a reason to ask twice; wNotAgainReaders is W-NOTAGAIN with one.
	wReaders = "PATH is on a drive whose format (FSTYPE) cannot limit who reads a file, so other accounts on this computer, and on any computer the drive is connected to, can read the restored mail"
	// wReadersOwners (R7-01; the lead's words, owner-delegated): a macOS drive set to
	// ignore ownership.
	wReadersOwners   = "PATH is on a drive set to ignore ownership, so every account on this computer, and on any computer the drive is connected to, can read the restored mail"
	wNotAgainReaders = "The restored mail will not be encrypted, and REASONS. That was not confirmed a second time, so nothing was restored. Confirm it again, or choose a profile on this computer's own drive."
	// wRecaptureUnknown (R6-02): nothing names what backs the archive up.
	wRecaptureUnknown = "MailArchive cannot tell whether this archive backs up this Thunderbird profile. If it does, its backups leave the restored folder out, and mail you later move into that folder in Thunderbird is not backed up while it is there."
	// wReadOnly (R6-13): an archive on a read-only drive, read without its lock.
	wReadOnly = "The archive is on a drive that cannot be written (read-only), so MailArchive reads it without taking its lock: no backup can write it there while the restore reads it."
	// wArchLock (R6-13): the archive's lock cannot be taken, and the drive is not read-only.
	wArchLock = "MailArchive cannot take this archive's lock, so nothing was restored: the archive folder (OUT) or its lock file cannot be changed by your account, which a backup needs too. Check that the folder is yours and not read-only, then click Check again."
	// wShared (R6-06): another profile uses the same Local Folders.
	wShared = "Another Thunderbird profile, 'NAME' (DIR), uses this same Local Folders. MailArchive keeps Thunderbird out of it too while the restore writes."
	// wFAT32Enc (R6-12): W-FAT32 for an encrypted archive, a count only.
	wFAT32Enc     = "Of the folders chosen, N IS 4 GB or more, too big for the drive that holds Local Folders: that drive uses the FAT32 format, which cannot hold a file of 4 GB or more, so nothing was restored. Choose fewer folders, or move Local Folders to a drive formatted another way."
	wCloud        = "This Thunderbird profile is in a folder SERVICE keeps in sync, so the restored mail will be uploaded there too."
	wAllMail      = "Gmail's All Mail folder holds another copy of every message in your other Gmail folders. Restoring it too puts those messages in Thunderbird twice. Untick it if you only want your folders."
	wRecapture    = "This archive backs up this Thunderbird profile. Its backups will leave the restored folder out, so the restored mail is not copied into the archive again. Mail you later move into that folder in Thunderbird is not backed up while it is there."
	wScheduled    = "A backup scheduled while this runs is skipped without an error and runs at its next time."
	wChecks       = "After writing, MailArchive reads back every message it wrote and checks it against the archive's original, before Thunderbird can show the folder."
	wDamagedLater = "Originals that turn out to be damaged are found while restoring and named in the result."
	wClosed       = "Thunderbird is closed."
	wM365Unread   = "Messages restored from Microsoft 365 show as unread. To mark them read, right-click the folder in Thunderbird and choose Mark Folder Read."
	wDeletedInTB  = "N of them were deleted in Thunderbird before they were backed up, so Thunderbird keeps them hidden, and may remove them from the restored folder the next time it compacts it. The archive keeps them."
)

// leaveWords words a reason the Local Folders may leave this computer, for the restore.
func leaveWords(code, service, path string) string {
	switch code {
	case leavesSynced:
		return path + " is synced to " + service + ": the restored mail will be uploaded there"
	case leavesMayICloud:
		return path + " may be synced to iCloud (when iCloud keeps your Desktop and Documents folders): the restored mail may be uploaded there"
	case leavesRemovable:
		return path + " is on a removable or external drive: the restored mail leaves this computer with it"
	case leavesShare:
		return path + " is on a network share: the restored mail is written to another computer"
	}
	return path + " may leave this computer"
}

func fill(words string, kv ...string) string { return strings.NewReplacer(kv...).Replace(words) }

// refusal is a restore that does not go ahead, in the person's words.
func refusal(code, words string, cause error) error {
	return &tbrestore.Refusal{Code: code, Words: words, Err: cause}
}
