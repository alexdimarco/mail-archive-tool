package tbrestore

// The writer's words (design §7.5 and its folded rows, with foldcheck3's S4, S5 and S6
// folded in slice R4). Each is shown only in the state it names, so none says something
// false. Placeholders in capitals are filled by the caller: NAME is the archive folder's
// name as in "Restored — NAME", PATH the Local Folders folder, DATE the earlier
// restore's start.
const (
	wNames       = "Local Folders already holds 'Restored — NAME' and 98 more like it, so nothing was restored. Delete or rename the restored folders you no longer need in Thunderbird, then try again."
	wLongPath    = "N folders are nested too deep for Windows to hold under this profile, so they are not restored: FOLDERS."
	wLongPathAll = "Thunderbird's Local Folders (PATH) is so deep in its drive that Windows cannot hold a restored folder under it, so nothing was restored. Choose a profile whose Local Folders is nearer the top of its drive."

	wDone      = "Restored N into Thunderbird as the folder 'TOP' under Local Folders. Open Thunderbird to see it; the first time, it may take a minute to show every message. MailArchive checked every message it wrote."
	wCheckFail = "After writing, MailArchive checked every message, and FILE (the folder FOLDER) was missing or changed — often antivirus moving a file it flagged. Nothing was added to Thunderbird. Look in your antivirus's quarantine, or leave that folder out, then try again."
	// wCheckFailEnc is W-CHECKFAIL for an encrypted archive: no file or folder named (N7).
	wCheckFailEnc = "After writing, MailArchive checked every message, and one of the restored files (in one of the folders) was missing or changed — often antivirus moving a file it flagged. Nothing was added to Thunderbird. Look in your antivirus's quarantine, or leave that folder out, then try again."
	// wTreeRemoved follows W-CHECKFAIL when the cleanup removed what was written.
	wTreeRemoved = " The part already written was removed."

	wTaken = "Something made a folder named 'TOP' in Local Folders while the restore ran. MailArchive had already written and checked every message, so it removed nothing: Thunderbird SHOWS the restored folders inside that 'TOP'. They are yours to keep, or to delete in Thunderbird. MailArchive backs them up like your other mail. To restore under a new name as well, click Check again."
	// wTakenGone (S5, folded 2026-11-07): a live W-TAKEN whose entry has gone again by the
	// cleanup's first step. The "taken" line still means remove nothing (S3).
	wTakenGone = "Something made, then removed again, a folder named 'TOP' in Local Folders while the restore ran. MailArchive had already written and checked every message, and removed nothing: the restored folders are in 'TOP.sbd' in PATH, which Thunderbird does not show. Close Thunderbird and delete that folder yourself when you no longer need it, then click Check again to restore."
	// wNameAppeared (S5, folded 2026-11-07): a live Cancel, failure or W-CHECKFAIL whose
	// cleanup meets an entry of the parent name (no "taken" line: MailArchive never tried
	// to make it). Neither W-TAKEN's "had already written and checked" nor W-VISIBLE's "An
	// earlier restore" is true there.
	wNameAppeared = "While the restore ran, something made an entry named 'TOP' in Local Folders, so MailArchive stopped and removed nothing. Thunderbird may show the folders MailArchive had written inside it, and some messages in them may be missing or cut short. They are yours: if you moved mail into them, move it out first, then delete them in Thunderbird and restore again. Until then MailArchive backs them up like your other mail."

	wCommitErr = "MailArchive wrote and checked every message, but could not make sure the file that makes the folder show in Thunderbird was saved (WHY), so it stopped there and removed nothing. The restored mail is complete, but Thunderbird may or may not show it as 'TOP'. If it does not, close Thunderbird, delete the folder 'TOP.sbd' in PATH, and restore again."

	// W-LEFT, the step-3 form (nothing removed) and, S4 folded 2026-11-07, the step-4
	// form (part removed before something new was found).
	wLeft        = "MailArchive found N entries it did not make in the folder 'TOP.sbd' (FIRST NAMES), in PATH, so it removed nothing there. Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no longer need those files."
	wLeftEnc     = "MailArchive found N entries it did not make in the folder 'TOP.sbd', in PATH, so it removed nothing there. Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no longer need those files."
	wLeftPart    = "MailArchive removed part of what it wrote in the folder 'TOP.sbd', in PATH, then found something there it did not make (FIRST NAMES), so it removed nothing more. Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no longer need those files."
	wLeftPartEnc = "MailArchive removed part of what it wrote in the folder 'TOP.sbd', in PATH, then found something there it did not make, so it removed nothing more. Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no longer need those files."

	wListBad = "MailArchive could not read its list of what an earlier restore wrote, so it removed nothing. Thunderbird does not show that folder ('TOP.sbd' in PATH); close Thunderbird and delete it yourself when you are ready."

	wBlocked    = "Windows would not let MailArchive remove FILE (it was in use — often by antivirus scanning), so it stopped clearing what the restore wrote in 'TOP.sbd'. Thunderbird does not show that folder. MailArchive tries again at your next restore."
	wBlockedEnc = "Windows would not let MailArchive remove one of the restored files (it was in use — often by antivirus scanning), so it stopped clearing what the restore wrote in 'TOP.sbd'. Thunderbird does not show that folder. MailArchive tries again at your next restore."

	// W-VISIBLE's two forms; the unchecked form says "stopped before it finished" (S6:
	// the crash may have come during the post-write check, after the writing).
	wVisibleChecked   = "An earlier restore into Thunderbird (DATE) wrote and checked every message, but a folder named 'TOP' that MailArchive did not make is now in Local Folders, so MailArchive removed nothing. Thunderbird SHOWS the restored folders inside 'TOP'. They are yours to keep, or to delete in Thunderbird. MailArchive backs them up like your other mail."
	wVisibleUnchecked = "An earlier restore into Thunderbird (DATE) stopped before it finished, and a folder named 'TOP' that MailArchive did not make is now in Local Folders, so MailArchive removed nothing. Thunderbird SHOWS those folders inside 'TOP', and some messages in them may be missing or cut short. They are yours: if you moved mail into them, move it out first, then delete them in Thunderbird and restore again. Until then MailArchive backs them up like your other mail."

	// W-ELSEWHERE: "may not have finished" (S6: with the drive gone a committed restore
	// cannot be told from an unfinished one).
	wElsewhereGone  = "An earlier restore into this Thunderbird profile may not have finished (DATE). It wrote into PATH, which is on a drive that isn't connected, so MailArchive left it alone. Connect that drive, and MailArchive deals with it at your next restore into this profile."
	wElsewhereMoved = "An earlier restore into this Thunderbird profile may not have finished (DATE). It wrote into PATH, which is no longer this profile's Local Folders, so MailArchive left it alone. If you no longer need it, delete the folder 'TOP.sbd' there yourself."
	wWentAhead      = " Your new restore went ahead."

	wUnfinished = "An earlier restore into Thunderbird did not finish (DATE). Thunderbird does not show it. At the start of your next restore into that Thunderbird profile, MailArchive removes what it wrote there; if anything else has been put in that folder, it leaves the whole folder and tells you. To remove it yourself instead, close Thunderbird and delete the folder 'TOP.sbd' in PATH."
	wTriesAgain = " MailArchive tries again at your next restore."

	wLogBad  = "MailArchive's restore record (restores.log in FOLDER) can't be read, so it can't tell whether an earlier restore left anything behind, and nothing was restored. Move that file somewhere you keep records, then try again. After that, MailArchive's backups no longer know which folders it restored into Thunderbird, and back them up like your other mail, together with any hidden folder an unfinished restore left. Before you move the file, delete in Thunderbird the restored folders you no longer need."
	wLogFull = "MailArchive's restore record (restores.log in FOLDER) is full, so nothing was restored. Copy it somewhere you keep records, delete it, then try again. After that, MailArchive's backups no longer know which folders it restored into Thunderbird, and back them up like your other mail, together with any hidden folder an unfinished restore left. Before you move the file, delete in Thunderbird the restored folders you no longer need."
)
