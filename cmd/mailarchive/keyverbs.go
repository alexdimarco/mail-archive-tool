package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The key verbs (design-encrypted-archives-phase3 §4.3, §4.6, §9): each needs a
// person at a terminal, as Desktop's Encryption card does by its nature, and exits 0
// when done, 1 when refused or failed, 130/143 when interrupted (X1).

// keyVerbFlags parses a key verb's -out and secret-file flags.
func keyVerbFlags(name, usage string, args []string, extra func(*flag.FlagSet)) (string, *keyFlags, error) {
	fs := flag.NewFlagSet("mailarchive "+name, flag.ContinueOnError)
	out := fs.String("out", "", "the encrypted archive's folder (required)")
	k := &keyFlags{}
	addSecretFileFlags(fs, k)
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
	return abspath(*out), k, nil
}

// remembered asks the K13 confirmation when the remembered key, not a secret the
// person gave, authorises a key change.
func confirmByRemembered(a asker, u *keyring.Unlocked, what string) error {
	if u.How() != keyring.ByRememberedKey {
		return nil
	}
	ok, err := askYes(a, "This computer remembers the archive's key, so "+what+" without the current password. Anyone using this computer's account could do the same: encryption protects a copy of the archive that leaves this computer, not this computer. Go ahead?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nothing was changed")
	}
	return nil
}

func runChangePassword(args []string) error {
	out, k, err := keyVerbFlags("change-password", "Set a new password for an encrypted archive. It asks for the current password (or recovery key) unless this computer remembers the key; then for the new one, twice.", args, nil)
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

func runNewRecoveryKey(args []string) error {
	var keyOut *string
	out, k, err := keyVerbFlags("new-recovery-key", "Make a new recovery key for an encrypted archive; the old one stops opening its key file. It asks for the current password (or recovery key) unless this computer remembers the key.", args, func(fs *flag.FlagSet) {
		keyOut = fs.String("recovery-key-out", "", "also save the new recovery key to this NEW file, readable only by you, outside the archive")
	})
	if err != nil {
		return err
	}
	k.recoveryKeyOut = keyOut
	return interrupted(newRecoveryKey(stdAsker(), out, k), os.Stderr)
}

func newRecoveryKey(a asker, out string, k *keyFlags) error {
	u, err := unlockForWrite(a, out, "making a new recovery key", k)
	if err != nil {
		return err
	}
	defer u.Destroy()
	if err := confirmByRemembered(a, u, "a new recovery key can be made"); err != nil {
		return err
	}
	p, err := keyring.NewRecoveryKey(u)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			p.Discard()
		}
	}()
	if k.recoveryKeyOut != nil && *k.recoveryKeyOut != "" {
		if err := writeRecoveryKeyFile(a, *k.recoveryKeyOut, out, u.ArchiveID().String(), p.RecoveryKey()); err != nil {
			return err
		}
	}
	if err := confirmRecoveryKey(a, p.RecoveryKey()); err != nil {
		return err
	}
	err = p.Commit()
	committed = true
	switch {
	case errors.Is(err, vault.ErrPartiallySaved):
		a.say("The new recovery key is in effect; the second copy of the key file is rewritten at the next opening.")
	case err != nil:
		return err
	default:
		a.say("The new recovery key is in effect.")
	}
	a.say("The earlier recovery key no longer opens this archive's key file. A copy of the archive, or of its key file, saved with the earlier key still opens with it — destroy those.")
	return nil
}

func runForgetKey(args []string) error {
	out, _, err := keyVerbFlags("forget-key", "Stop remembering an encrypted archive's key on this computer: backups you start and opening the archive will ask for the password. The archive and this computer's record of it are unchanged.", args, nil)
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
	out, _, err := keyVerbFlags("forget-archive", "Forget that this folder holds one of this computer's encrypted archives (for example after you removed it on purpose). Until then MailArchive refuses to back up into the folder without the archive's key.", args, nil)
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
