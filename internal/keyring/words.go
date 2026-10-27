package keyring

import (
	"errors"
	"fmt"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/vault"
)

// What MailArchive says about keys, in one place for the command line and the
// Desktop, so both say the same (design-encrypted-archives-phase3 §5.1, §9; the P3-3
// plan, 3a).

// PasswordHint is said before a new password is asked for (§5.1 step 2).
const PasswordHint = "Use at least 12 characters; a longer phrase is stronger. A copy of the archive made before you change the password keeps the old one."

// RecoveryWarning is said with a new recovery key (K2).
const RecoveryWarning = "Save this recovery key in your password manager now. If you lose both your password and this key, the archive can never be opened — not by you, not by us. There is no reset."

// RecoveryFileNote is said when a recovery file is saved (§9.4).
const RecoveryFileNote = "The recovery file opens nothing on its own: with the recovery key it puts back the archive's key files if they are lost (`mailarchive restore-key-file`). Keep it with your backups, and the recovery key apart from it."

// EarlierKeyWarning is what a new recovery key does not withdraw (§9.2, §9.4): the
// earlier one still opens what was saved with it.
const EarlierKeyWarning = "The earlier recovery key no longer opens this archive's key file. But together with a recovery file, a key file or a copy of the archive saved with it, it still opens the archive — destroy recovery files saved with your earlier recovery key, and those copies."

// NoRecoveryFileWarning replaces EarlierKeyWarning when no recovery file was saved
// for the new key: the earlier pair is then the only way to put back lost key files
// (the P3-1b pass, CRIT-01). saveOne says how to save a new recovery file.
func NoRecoveryFileWarning(saveOne string) string {
	return "The earlier recovery key no longer opens this archive's key file. No recovery file was saved for the new recovery key, so it cannot put back lost key files — your earlier recovery file still can, with your earlier recovery key: keep that pair together and safe. If it was exposed, destroy it, and save a new recovery file now with " + saveOne + ". A copy of the archive or a key file saved with the earlier recovery key still opens with it."
}

// KeyChangeQuestion is the confirmation a key change asks when no secret the person
// typed authorises it (K13): the remembered key, or — in the Desktop — an archive
// unlocked here. what says the change ("its password can be changed"). It is "" when
// the person typed the password or recovery key for this change.
func KeyChangeQuestion(how How, unlockedHere bool, what string) string {
	switch {
	case how == ByRememberedKey:
		return "This computer remembers the archive's key, so " + what + " without the current password. Anyone using this computer's account could do the same: encryption protects a copy of the archive that leaves this computer, not this computer. Go ahead?"
	case unlockedHere:
		return "This archive is unlocked here, so " + what + " without typing the current password. Anyone using this computer while it is unlocked could do the same: encryption protects a copy of the archive that leaves this computer, not this computer. Go ahead?"
	}
	return ""
}

// ReRecordQuestion is asked before an archive the person told MailArchive to forget
// is recorded again (§4.2), saying what recording does and what no leaves (P3-8
// friction review, Tier C).
const ReRecordQuestion = "Earlier, you told MailArchive to forget this encrypted archive, so this computer stopped protecting its folder. Record it again on this computer, so the folder is protected as one of its encrypted archives again? (Answering n opens nothing and changes nothing.)"

// RecoveryKeyRetry says, without quoting it, why what was typed back is not the
// recovery key just shown (K2), with what to do — never the vault's own words
// (P3-8 friction review, B14).
func RecoveryKeyRetry(err error) string {
	switch {
	case errors.Is(err, vault.ErrRecoveryCheck):
		return "That has a typing mistake: compare it with the key you saved, group by group, and type it again."
	case errors.Is(err, vault.ErrRecoveryFormat):
		return "That is not a recovery key: a recovery key is groups of letters and digits separated by dashes. Type it again."
	}
	return "That is a recovery key, but not the one just shown: type the one you saved from this screen."
}

// NewPasswordProblem says what is wrong with pw as a new password, before any key
// is made from it, or "" when it will do.
func NewPasswordProblem(pw string) string {
	if n := len([]rune(norm.NFC.String(pw))); n < vault.MinPasswordRunes {
		return fmt.Sprintf("That has %d characters; use at least %d (a passphrase of several words is easiest).", n, vault.MinPasswordRunes)
	}
	if CheckNewPassword(pw) != nil {
		return "That password reads as a recovery key, so it could not be typed at a prompt; choose another."
	}
	return ""
}
