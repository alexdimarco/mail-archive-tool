package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// The key verbs (design-encrypted-archives-phase3 §4.3, §4.6, §9): each needs a
// person at a terminal, as Desktop's Encryption card does by its nature, and exits 0
// when done, 1 when refused or failed, 130/143 when interrupted (X1).

// keyVerbFlags parses a key verb's -out and, for a verb that unlocks the archive
// (opens), the secret-file flags and -remember (§3.4: "-remember is taken by any
// keyed open"; the P3-1a pass, FID-10). A verb that does not unlock takes neither.
func keyVerbFlags(name, usage string, args []string, opens bool, extra func(*flag.FlagSet)) (string, *keyFlags, error) {
	fs := flag.NewFlagSet("mailarchive "+name, flag.ContinueOnError)
	out := fs.String("out", "", "the encrypted archive's folder (required)")
	k := &keyFlags{}
	if opens {
		addSecretFileFlags(fs, k)
		k.remember = fs.Bool("remember", false, "also remember the archive's key on this computer, so backups you start here and opening it do not ask for the password")
	}
	if extra != nil {
		extra(fs)
	}
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive %s -out DIR [options]\n\n%s\n\nOptions:\n", name, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	if fs.NArg() > 0 {
		return "", nil, fmt.Errorf("unexpected argument %q (the folder is given with -out)", fs.Arg(0))
	}
	if *out == "" {
		return "", nil, errors.New("-out is required (the encrypted archive's folder)")
	}
	k.rememberGiven = flagWasSet(fs, "remember")
	return abspath(*out), k, nil
}

// remembered asks the K13 confirmation when the remembered key, not a secret the
// person gave, authorises a key change.
func confirmByRemembered(a asker, u *keyring.Unlocked, what string) error {
	question := keyring.KeyChangeQuestion(u.How(), false, what)
	if question == "" {
		return nil
	}
	ok, err := askYes(a, question, false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nothing was changed")
	}
	return nil
}

func runChangePassword(args []string) error {
	out, k, err := keyVerbFlags("change-password", "Set a new password for an encrypted archive. It asks for the current password (or recovery key) unless this computer remembers the key; then for the new one, twice.", args, true, nil)
	if err != nil {
		return err
	}
	return interrupted(changePassword(stdAsker(), out, k), os.Stderr)
}

func changePassword(a asker, out string, k *keyFlags) error {
	u, err := unlockForWrite(a, out, "changing its password", k)
	if err != nil {
		return err
	}
	defer u.Destroy()
	if err := confirmByRemembered(a, u, "the password can be changed"); err != nil {
		return err
	}
	pw, err := askNewPassword(a)
	if err != nil {
		return err
	}
	switch err := keyring.ChangePassword(u, pw); {
	case errors.Is(err, vault.ErrPartiallySaved):
		a.say("The new password is in effect; the second copy of the key file is rewritten at the next opening.")
	case err != nil:
		return err
	default:
		a.say("The password of %s is changed. A copy of the archive made before now still opens with the old one.", out)
	}
	return nil
}

// newKeyCopyFlags adds the two places a new recovery key may also be saved.
func newKeyCopyFlags(k *keyFlags) func(*flag.FlagSet) {
	return func(fs *flag.FlagSet) {
		k.recoveryKeyOut = fs.String("recovery-key-out", "", "also save the new recovery key to this NEW file, readable only by you, outside the archive")
		addRecoveryFileFlag(fs, k)
	}
}

func runNewRecoveryKey(args []string) error {
	copies := &keyFlags{}
	out, k, err := keyVerbFlags("new-recovery-key", "Make a new recovery key for an encrypted archive; the old one stops opening its key file. It asks for the current password (or recovery key) unless this computer remembers the key.", args, true, newKeyCopyFlags(copies))
	if err != nil {
		return err
	}
	k.recoveryKeyOut, k.recoveryFileOut = copies.recoveryKeyOut, copies.recoveryFileOut
	return interrupted(newRecoveryKey(stdAsker(), out, k), os.Stderr)
}

func newRecoveryKey(a asker, out string, k *keyFlags) error {
	// Where the key is to be saved is judged before anything is asked.
	if err := checkKeyCopies(k, out); err != nil {
		return err
	}
	u, err := unlockForWrite(a, out, "making a new recovery key", k)
	if err != nil {
		return err
	}
	defer u.Destroy()
	if err := confirmByRemembered(a, u, "a new recovery key can be made"); err != nil {
		return err
	}
	if err := askRecoveryFileOut(a, k, out); err != nil {
		return err
	}
	p, err := keyring.NewRecoveryKey(u)
	if err != nil {
		return err
	}
	// Discard does nothing once the change is spent; on every other way out it
	// destroys the pending key (the P3-1a pass, SEC-05).
	defer p.Discard()
	if err := confirmRecoveryKey(a, p.RecoveryKey()); err != nil {
		return err
	}
	saved, err := saveKeyCopies(a, k, out, u.ArchiveID(), p.RecoveryKey(), p.RecoveryFile)
	if err != nil {
		return err
	}
	err = p.Commit()
	switch {
	case errors.Is(err, vault.ErrPartiallySaved):
		a.say("The new recovery key is in effect; the second copy of the key file is rewritten at the next opening.")
	case err != nil:
		// Not in effect: the person must not keep the key just shown (CON-03).
		voidFiles(a, saved)
		a.say("The recovery key just shown is NOT in effect; your earlier recovery key still opens the archive.")
		return err
	default:
		a.say("The new recovery key is in effect.")
	}
	sayWithdrawal(a, k)
	return nil
}

// sayWithdrawal says what a new recovery key does and does not withdraw, by whether
// its recovery file was saved (§9.2, §9.4; the P3-1b pass, CRIT-01): the master key
// is never re-keyed.
func sayWithdrawal(a asker, k *keyFlags) {
	if k.recoveryFileOut == nil || *k.recoveryFileOut == "" {
		a.say("%s", keyring.NoRecoveryFileWarning("`mailarchive new-recovery-key -out DIR -recovery-file-out FILE`"))
		return
	}
	a.say("%s", keyring.EarlierKeyWarning)
	a.say("%s", keyring.RecoveryFileNote)
}

// askRecoveryFileOut offers to save the new recovery key's recovery file when
// -recovery-file-out was not given (§5.1 step 5, §9.2; the P3-1b pass, CRIT-01): a
// new file outside the archive, judged as the flag is. Enter alone skips.
func askRecoveryFileOut(a asker, k *keyFlags, out string) error {
	if k.recoveryFileOut != nil && *k.recoveryFileOut != "" {
		return nil
	}
	for tries := 0; tries < 3; tries++ {
		ans, err := a.line("Save the new recovery key's recovery file as (a new file outside the archive; with the recovery key it puts back lost key files — Enter alone skips): ")
		if err != nil {
			return err
		}
		if ans = strings.TrimSpace(ans); ans == "" {
			return nil
		}
		if _, err := keyring.CheckNewFileOut("The recovery file", "the recovery file", ans, out); err != nil {
			a.say("%v", err)
			continue
		}
		k.recoveryFileOut = &ans
		return nil
	}
	return errors.New("no place to save the recovery file was chosen; nothing was changed")
}

func runRestoreKeyFile(args []string) error {
	var from *string
	var fromRemembered *bool
	copies := &keyFlags{}
	out, k, err := keyVerbFlags("restore-key-file", "Put back an encrypted archive's lost or damaged key files: from its recovery file (-from FILE) and its recovery key, or from the key this computer remembers (-remembered). It sets a new password and a new recovery key.", args, false, func(fs *flag.FlagSet) {
		from = fs.String("from", "", "the archive's recovery file (saved with -recovery-file-out)")
		fromRemembered = fs.Bool("remembered", false, "rebuild from the key this computer remembers for the archive, instead of a recovery file")
		copies.recoveryKeyFile = fs.String("recovery-key-file", "", "the recovery key for -from, read from this file (readable only by you), instead of typed")
		newKeyCopyFlags(copies)(fs)
	})
	if err != nil {
		return err
	}
	k.recoveryKeyFile, k.recoveryKeyOut, k.recoveryFileOut = copies.recoveryKeyFile, copies.recoveryKeyOut, copies.recoveryFileOut
	switch {
	case (*from == "") == !*fromRemembered:
		return errors.New("give either -from FILE (the recovery file) or -remembered")
	case *fromRemembered && *k.recoveryKeyFile != "":
		return errors.New("-recovery-key-file goes with -from; -remembered needs no recovery key")
	}
	return interrupted(restoreKeyFile(stdAsker(), out, *from, k), os.Stderr)
}

// restoreKeyFile puts back out's key files (§9.4, §9.5): the source judged and
// proven, a new password, the new recovery key shown, taken away and entered
// again, its copies saved, then the commit — saying what it does and does not
// withdraw. from is the recovery file; "" rebuilds from the remembered key (K15).
func restoreKeyFile(a asker, out, from string, k *keyFlags) error {
	if !a.attended() {
		return errNeedsTerminal(out, "putting its key file back")
	}
	if err := checkKeyCopies(k, out); err != nil {
		return err
	}
	o := keyring.RestoreOptions{
		ConfirmReRecord: func(string) (bool, error) {
			return askYes(a, "You told MailArchive to forget this encrypted archive. Record it again on this computer?", false)
		},
		ConfirmCost: func(p vault.Params) (bool, error) {
			return askYes(a, fmt.Sprintf("This recovery file asks for %s to open, more than MailArchive's own (%s), and this computer has not recorded this archive. Use it anyway?", p, vault.CostFloor()), false)
		},
	}
	var prepare func(newPassword string) (*keyring.PendingRestore, error)
	// askKey asks for the recovery key again after one that does not open the
	// recovery file; nil when it came from a file.
	var askKey func() error
	if from != "" {
		// The recovery file and the recovery key's typing are checked before the new
		// password is asked, and a mistyped key is asked again (the P3-1b pass,
		// CRIT-06).
		file, err := util.ReadRegularFile(abspath(from), 4<<10)
		if err != nil {
			return fmt.Errorf("the recovery file %s cannot be read: %w", abspath(from), err)
		}
		if _, err := vault.ParseRecoveryFile(file); err != nil {
			return fmt.Errorf("%s: %w", abspath(from), err)
		}
		var rk keyring.Secret
		if k.recoveryKeyFile != nil && *k.recoveryKeyFile != "" {
			if rk, err = keyring.RecoveryKeyFile(*k.recoveryKeyFile); err != nil {
				return err
			}
		} else {
			askKey = func() error {
				for tries := 0; tries < 3; tries++ {
					typed, err := a.secret("The recovery key saved with this recovery file: ")
					if err != nil {
						return err
					}
					if _, err := vault.ParseRecoveryKey(typed); err != nil {
						a.say("That is not a recovery key (%v).", err)
						continue
					}
					rk = keyring.RecoveryKey(typed)
					return nil
				}
				return errors.New("no recovery key was entered; nothing was changed")
			}
			if err := askKey(); err != nil {
				return err
			}
		}
		prepare = func(pw string) (*keyring.PendingRestore, error) { return keyring.RestoreFromFile(out, file, rk, pw, o) }
	} else {
		store, err := secretStore()
		if err != nil {
			return err
		}
		// Said only when it is so: judged before any question (P3-8 friction review, B6).
		if !keyring.HasRemembered(abspath(out), store) {
			return fmt.Errorf("this computer does not remember a key for an encrypted archive at %s, so nothing was changed — put the key files back from the archive's recovery file and recovery key instead: mailarchive restore-key-file -out %q -from FILE", abspath(out), abspath(out))
		}
		ok, err := askYes(a, "This computer remembers the archive's key, so its key files can be rebuilt without the password or recovery key. Anyone using this computer's account could do the same: encryption protects a copy of the archive that leaves this computer, not this computer. Go ahead?", false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("nothing was changed")
		}
		prepare = func(pw string) (*keyring.PendingRestore, error) {
			return keyring.RestoreFromRemembered(out, store, pw, o)
		}
	}
	if err := askRecoveryFileOut(a, k, out); err != nil {
		return err
	}
	a.say("The archive gets a new password and a new recovery key.")
	pw, err := askNewPassword(a)
	if err != nil {
		return err
	}
	var p *keyring.PendingRestore
	for tries := 0; ; tries++ {
		a.say("Proving the key against the archive (this takes a few seconds)...")
		if p, err = prepare(pw); err == nil {
			break
		}
		// A recovery key that does not open the recovery file is asked again,
		// keeping the new password (CRIT-06).
		if !errors.Is(err, vault.ErrWrongSecret) || askKey == nil || tries >= 2 {
			return err
		}
		a.say("That recovery key does not open this recovery file.")
		if err := askKey(); err != nil {
			return err
		}
	}
	defer p.Discard()
	if err := confirmRecoveryKey(a, p.RecoveryKey()); err != nil {
		return err
	}
	saved, err := saveKeyCopies(a, k, out, p.ArchiveID(), p.RecoveryKey(), p.RecoveryFile)
	if err != nil {
		return err
	}
	err = p.Commit()
	switch {
	case err == nil:
		a.say("The key file of %s is back, with your new password and new recovery key.", out)
	case errors.Is(err, vault.ErrPartiallySaved) && !errors.Is(err, vault.ErrUnconfirmed) && !errors.Is(err, keyring.ErrRestoredNotRecorded):
		a.say("The key file is back and in effect; its second copy is rewritten at the next opening.")
		err = nil
	case keyring.InEffect(err):
		// Written, and only what followed failed: the new password and recovery key
		// are the ones that count, so their saved copies stay (the P3-1b pass, OUT-01,
		// CON-02, FID-02).
		a.say("The key file of %s was put back: your new password and new recovery key are the ones that count now.", out)
		for _, f := range saved {
			a.say("The new recovery key is in force; %s is kept.", f)
		}
	default:
		voidFiles(a, saved)
		a.say("The key file was NOT put back; the recovery key just shown is not in effect.")
		return err
	}
	a.say("A password used before no longer opens it.")
	sayWithdrawal(a, k)
	return err
}

func runForgetKey(args []string) error {
	out, _, err := keyVerbFlags("forget-key", "Stop remembering an encrypted archive's key on this computer: backups you start and opening the archive will ask for the password. The archive and this computer's record of it are unchanged.", args, false, nil)
	if err != nil {
		return err
	}
	return interrupted(forgetKey(stdAsker(), out), os.Stderr)
}

func forgetKey(a asker, out string) error {
	if !a.attended() {
		return errNeedsTerminal(out, "forgetting its key")
	}
	store, err := secretStore()
	if err != nil {
		return err
	}
	id, err := keyring.ForgetKey(out, store)
	if err != nil {
		return err
	}
	a.say("This computer no longer remembers the key of the encrypted archive %s (%s); backups you start and opening it will ask for the password.", out, id)
	return nil
}

func runForgetArchive(args []string) error {
	out, _, err := keyVerbFlags("forget-archive", "Forget that this folder holds one of this computer's encrypted archives (for example after you removed it on purpose). Until then MailArchive refuses to back up into the folder without the archive's key.", args, false, nil)
	if err != nil {
		return err
	}
	return interrupted(forgetArchive(stdAsker(), out), os.Stderr)
}

// forgetArchive asks every confirmation of §4.6 first, holding nothing, then
// forgets; the forget itself refuses if anything changed meanwhile.
func forgetArchive(a asker, out string) error {
	if !a.attended() {
		return errNeedsTerminal(out, "forgetting it")
	}
	store, serr := secretStore()
	if serr != nil {
		store = nil
	}
	p, err := keyring.PlanForget(out, store)
	if err != nil {
		return err
	}
	a.say("This computer recorded the encrypted archive %s at:\n    %s", p.ID, p.Spelling)
	if len(p.Others) > 0 {
		a.say("Its other folders stay protected:")
		for _, o := range p.Others {
			a.say("    %s (recorded %s)", o.Spelling, o.Recorded.Local().Format("2006-01-02"))
		}
	}
	a.say("Another copy of this archive that used this same folder path, such as another drive mounted at the same place, loses this computer's protection at this path too.")
	if p.Strong {
		a.say("This folder may have been tampered with: it no longer holds the archive this computer made there.")
		typed, err := a.line("To forget it anyway, type the folder's path exactly as shown above: ")
		if err != nil {
			return err
		}
		if !encrecord.Names(strings.TrimSpace(typed), p.Spelling) {
			return errors.New("the path typed does not match; nothing was changed")
		}
	} else {
		ok, err := askYes(a, "Forget this folder?", false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("nothing was changed")
		}
	}
	var o keyring.ForgetOptions
	if p.Last {
		whole, err := askYes(a, "It is the last folder this computer recorded for the archive. Forget the whole archive on this computer?", false)
		if err != nil {
			return err
		}
		o.Whole = whole
		if whole && p.Remembered {
			if o.Keys, err = askYes(a, "Also forget its remembered key?", false); err != nil {
				return err
			}
			o.Store = store
		}
	}
	if err := keyring.Forget(p, o); err != nil {
		return err
	}
	switch {
	case o.Whole:
		a.say("This computer forgot the encrypted archive %s.", p.ID)
	default:
		a.say("This computer no longer protects %s as an encrypted archive.", p.Spelling)
	}
	return nil
}

func runRepairRecord(args []string) error {
	fs := flag.NewFlagSet("mailarchive repair-record", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive repair-record\n\nRebuild this computer's damaged record of encrypted archives from what still reads. The damaged copies are kept beside it; every rebuilt folder stays protected until its archive is opened with its password.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	rep, err := encrecord.Repair()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Rebuilt this computer's record of encrypted archives: %d folder(s), %d forgotten archive(s).\n", rep.Paths, rep.Tombstones)
	for _, p := range rep.MovedAside {
		fmt.Fprintf(os.Stderr, "The damaged copy %s was kept aside.\n", p)
	}
	for _, n := range rep.NotRebuilt {
		fmt.Fprintf(os.Stderr, "Not rebuilt: %s\n", n)
	}
	if rep.Paths > 0 {
		fmt.Fprintln(os.Stderr, "Open each archive once with its password (a backup into it, or `mailarchive change-password`) to confirm it.")
	}
	return nil
}

func runEncrypt(args []string) error {
	return errors.New("to make a new encrypted archive, run a backup with -encrypt into a new or empty folder (mailarchive -encrypt -out DIR -input …); encrypting an existing archive comes in a later version")
}
