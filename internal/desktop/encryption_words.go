package desktop

import "mail-archive-tool/internal/app"

// The words that tell the recovery key from the recovery file on the Encryption card (MA-1071; the
// owner's report of 2026-11-03: the person saved the recovery file, opened it, and pasted its
// contents where the recovery key is typed back). Lead's words, owner-delegated 2026-11-02: what
// happened first, then what to do next. The recovery key is the short groups of letters and digits
// typed back; the recovery file is opened by MailArchive itself, never typed or pasted, and does not
// hold the recovery key (vault.RecoveryFile holds only the recovery-wrapped key).

// recoveryFileWhat is said beside the recovery key, above the button that saves the recovery file.
const recoveryFileWhat = "The recovery file is something else: a file MailArchive opens itself, with your recovery key, if this archive's key files are ever lost. You never type or paste it. It does not contain the recovery key, so save the recovery key as well."

// recoveryFileButtonTitle is the save button's tooltip.
const recoveryFileButtonTitle = "Save a recovery file in a folder you choose, outside the archive. MailArchive opens it, with your recovery key, to put back lost key files; you never type or paste it, and it does not contain the recovery key."

// recoveryFilePickTitle is the title of the folder window the recovery file is saved from.
const recoveryFilePickTitle = "Choose a folder for the recovery file, outside the archive (MailArchive opens this file itself; it is not the recovery key)"

// recoveryFileSavedNote follows "Saved the recovery file: PATH." on the card.
const recoveryFileSavedNote = "MailArchive opens it, with your recovery key, if this archive's key files are ever lost; you never type or paste it. It does not contain your recovery key: keep the recovery key too, apart from it."

// recoveryFileTypedBack answers the recovery file's contents typed back where the new recovery key
// is asked, quoting none of it.
const recoveryFileTypedBack = "That is the recovery file's contents, not the recovery key. Type the recovery key you saved: the groups of letters and digits (like ABCD-EFGH-…). Show it again brings it back."

// recoveryFileInKeyBox answers the recovery file's contents typed in the recovery key box of Put
// back the key file, quoting none of it.
const recoveryFileInKeyBox = "That is the recovery file's contents, not the recovery key. Type the recovery key in this box (the groups of letters and digits, like ABCD-EFGH-…); MailArchive asks for the recovery file itself, in a window, when you click " + app.RestoreKeyWords + "."

// recoveryFileInUnlockBox answers the recovery file's contents typed in the Unlock box ("Password or
// recovery key") once they did not open the archive, quoting none of it (MA-1075; the review's LOW-2).
const recoveryFileInUnlockBox = "That is the recovery file's contents, not the password or the recovery key, so nothing was opened. Type the password, or the recovery key you saved (the groups of letters and digits, like ABCD-EFGH-…). MailArchive opens the recovery file itself, only to put back lost key files."

// recoveryFileLastNote is the last line about the recovery file once a key saved with one takes effect
// on the Desktop (MA-1075; the review's LOW-3): the Desktop's words, as the key's box and the saved line
// say them, never the command line's (keyring.RecoveryFileNote, with its command and "Keep it with your
// backups").
const recoveryFileLastNote = "The recovery file you saved is opened by MailArchive, with your recovery key, if this archive's key files are ever lost; you never type or paste it. Keep the recovery file and the recovery key apart from each other."

// firstBackupRowLine ends "archive made" when the new archive's first backup runs (MA-1072): the page
// drawn next lands on its row in Your archives and follows the backup there.
const firstBackupRowLine = "Its first backup is running now: its row in Your archives says how it goes. Switch to it to read the archive."

// restoreHowLine is said in Put back the key file, under the recovery key box's label.
const restoreHowLine = "Type the recovery key in the box. When you click " + app.RestoreKeyWords + ", a window opens for you to choose the recovery file: you never open or paste the file yourself."
