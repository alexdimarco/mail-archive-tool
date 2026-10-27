package keyring

import (
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
