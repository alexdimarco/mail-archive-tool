package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tty"
	"mail-archive-tool/internal/vault"
)

// keyFlags are export's and graph's encryption flags (design-encrypted-archives-phase3
// §3.4, §5.2). They are kept out of exportFlags and graphFlags, which a scheduled job
// is parsed with, and parseJob refuses them by name: a backup into an encrypted
// archive needs a person at the terminal in this version (O5).
type keyFlags struct {
	encrypt         *bool
	remember        *bool
	passwordFile    *string
	recoveryKeyFile *string
	recoveryKeyOut  *string
	recoveryFileOut *string
	programCopies   *string // "on", "off" or "" (program design §6, R21)
	// rememberGiven: -remember was on the command line, either way, so the
	// question is not asked.
	rememberGiven bool
}

// keyFlagNames are the flags parseJob refuses in a scheduled job.
var keyFlagNames = []string{"encrypt", "remember", "password-file", "recovery-key-file", "recovery-key-out", "recovery-file-out", "program-copies"}

func addKeyFlags(fs *flag.FlagSet) *keyFlags {
	k := &keyFlags{}
	k.encrypt = fs.Bool("encrypt", false, "make -out a new ENCRYPTED archive (a new or empty folder), at a terminal: you choose a password and are shown a recovery key to save")
	k.remember = fs.Bool("remember", false, "remember an encrypted archive's key on this computer, so backups you start here and opening it do not ask for the password (anyone using this computer's account can then open it); -encrypt asks when this is not given")
	addSecretFileFlags(fs, k)
	k.recoveryKeyOut = fs.String("recovery-key-out", "", "with -encrypt: also save the new recovery key to this NEW file, readable only by you, outside the archive")
	addRecoveryFileFlag(fs, k)
	k.programCopies = fs.String("program-copies", "", "on or off: keep copies of MailArchive for Mac, Windows and Linux in the encrypted archive, so it can be opened years from now (about 134 MB each time a new version updates them); -encrypt keeps them unless this is off")
	return k
}

// copies is -program-copies as given, or "".
func (k *keyFlags) copies() string {
	if k.programCopies == nil {
		return ""
	}
	return *k.programCopies
}

// programChoice is the run's choice of program copies: as given, else on for a new
// encrypted archive (on by default, O1), else the sealed choice is left as it is.
func (k *keyFlags) programChoice(creating bool) app.ProgramChoice {
	switch {
	case k.copies() == "on", k.copies() == "" && creating:
		return app.ProgramCopiesOn
	case k.copies() == "off":
		return app.ProgramCopiesOff
	}
	return app.ProgramCopiesUnset
}

// addRecoveryFileFlag adds -recovery-file-out (§9.4): where a new recovery key's
// recovery file is saved.
func addRecoveryFileFlag(fs *flag.FlagSet, k *keyFlags) {
	k.recoveryFileOut = fs.String("recovery-file-out", "", "save the new recovery key's recovery file to this NEW file, outside the archive: with the recovery key it puts back the archive's key files if they are lost")
}

// addSecretFileFlags adds the two ways to give an encrypted archive's secret without
// typing it.
func addSecretFileFlags(fs *flag.FlagSet, k *keyFlags) {
	k.passwordFile = fs.String("password-file", "", "an encrypted archive's password, read from this file (readable only by you; one trailing line ending is ignored) instead of typed")
	k.recoveryKeyFile = fs.String("recovery-key-file", "", "an encrypted archive's recovery key, read from this file (readable only by you), instead of the password")
}

// usedAny reports whether any encryption flag was given.
func (k *keyFlags) usedAny(fs *flag.FlagSet) bool {
	for _, n := range keyFlagNames {
		if flagWasSet(fs, n) {
			return true
		}
	}
	return false
}

// refuseEncryptWith refuses -encrypt with what cannot make an encrypted archive
// (§5.2).
func refuseEncryptWith(encrypt, unattended, manifest, list bool) error {
	if !encrypt {
		return nil
	}
	switch {
	case unattended:
		return errors.New("-encrypt cannot run unattended: an encrypted archive is made by you at a terminal, choosing its password and saving its recovery key")
	case manifest:
		return errors.New("-encrypt cannot take -manifest: an encrypted archive keeps its file list inside itself")
	case list:
		return errors.New("-encrypt cannot take -list: -list only reads the mail sources; run it without -encrypt")
	}
	return nil
}

// refuseStrayKeyFlags refuses encryption flags on a run that neither makes nor opens
// an encrypted archive, and a flag that does not fit the one it does.
func refuseStrayKeyFlags(fs *flag.FlagSet, k *keyFlags, keyed, creating bool) error {
	switch {
	case !keyed && !creating && k.usedAny(fs):
		// The refusal names the flags given (R12; the P3-1b pass, REG-04).
		var given []string
		for _, n := range keyFlagNames {
			if flagWasSet(fs, n) {
				given = append(given, "-"+n)
			}
		}
		return fmt.Errorf("%s: for an encrypted archive only — make one with -encrypt, or give -out an encrypted archive", strings.Join(given, ", "))
	case keyed && (*k.recoveryKeyOut != "" || *k.recoveryFileOut != ""):
		return errors.New("-recovery-key-out and -recovery-file-out save the recovery key of a NEW encrypted archive (-encrypt); for a new recovery key run `mailarchive new-recovery-key`")
	case creating && (*k.passwordFile != "" || *k.recoveryKeyFile != ""):
		return errors.New("-encrypt asks for the new archive's password at the terminal; -password-file and -recovery-key-file open an existing one")
	case k.copies() != "" && k.copies() != "on" && k.copies() != "off":
		return fmt.Errorf("-program-copies is on or off, not %q", k.copies())
	}
	return nil
}

// openForBackup makes, or unlocks, the encrypted archive a backup runs into, at the
// terminal; nil for a plaintext archive.
func openForBackup(fs *flag.FlagSet, k *keyFlags, out string, keyed, creating bool) (*keyring.Unlocked, error) {
	var u *keyring.Unlocked
	var err error
	k.rememberGiven = flagWasSet(fs, "remember")
	switch {
	case creating:
		u, err = createEncrypted(stdAsker(), abspath(out), k, flagWasSet(fs, "remember"), k.secretPaths())
	case keyed:
		u, err = unlockForWrite(stdAsker(), abspath(out), "a backup into it", k)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, interrupted(err, os.Stderr)
	}
	return u, nil
}

// secretPaths are the secret files given, for the run's CredentialPaths: none may be
// inside the archive (P2a-C14).
func (k *keyFlags) secretPaths() []string {
	var ps []string
	for _, p := range []*string{k.passwordFile, k.recoveryKeyFile, k.recoveryKeyOut, k.recoveryFileOut} {
		if p != nil && *p != "" {
			ps = append(ps, abspath(*p))
		}
	}
	return ps
}

// asker is the person at the terminal, as the encrypted-archive verbs ask and tell
// them things. ttyAsker is the real one; tests script one.
type asker interface {
	// attended: stdin and stderr are a terminal, and this process is in its
	// foreground (K3).
	attended() bool
	// secret asks with typing hidden; line asks with typing shown.
	secret(prompt string) (string, error)
	line(prompt string) (string, error)
	say(format string, a ...any)
	// showKey shows the recovery key, on a screen of its own where the terminal can
	// take it away again, and waits for Enter (K2).
	showKey(key string) error
}

type ttyAsker struct {
	in  *os.File
	out *os.File
}

func stdAsker() asker { return ttyAsker{in: os.Stdin, out: os.Stderr} }

func (a ttyAsker) attended() bool {
	return tty.IsTerminal(a.in) && tty.Foreground(a.in) && tty.IsTerminal(a.out)
}

func (a ttyAsker) secret(prompt string) (string, error) {
	b, err := tty.ReadSecret(a.in, a.out, prompt)
	defer clear(b)
	return string(b), err
}

func (a ttyAsker) line(prompt string) (string, error) { return tty.ReadLine(a.in, a.out, prompt) }

func (a ttyAsker) say(format string, args ...any) { fmt.Fprintf(a.out, format+"\n", args...) }

// showKey shows the recovery key with the G4 warning in full (keyring.RecoveryWarning).
func (a ttyAsker) showKey(key string) error {
	erase := tty.CanErase(a.out)
	if erase {
		fmt.Fprint(a.out, "\x1b[?1049h\x1b[H\x1b[2J")
	}
	fmt.Fprintf(a.out, "Your recovery key:\n\n    %s\n\n%s\n\n", key, keyring.RecoveryWarning)
	if !erase {
		fmt.Fprintln(a.out, "This terminal cannot take the key off the screen afterwards: once you have saved it, clear the screen and its scrollback yourself.")
	}
	_, err := tty.ReadSecret(a.in, a.out, "Press Enter once you have saved it. ")
	if erase {
		fmt.Fprint(a.out, "\x1b[?1049l")
	}
	return err
}

// errNeedsTerminal refuses a keyed write without a person at the terminal (K3, O5).
func errNeedsTerminal(out, what string) error {
	return fmt.Errorf("%s is an encrypted archive: %s needs you at a terminal to unlock it with its password or recovery key (or MailArchive Desktop) — scheduled and unattended backups into an encrypted archive are not available in this version", out, what)
}

// errEncryptNeedsTerminal refuses -encrypt without a person at the terminal.
var errEncryptNeedsTerminal = errors.New("-encrypt needs you at a terminal: you choose a password and save a recovery key — run it in a terminal, or use MailArchive Desktop")

// requireAttended refuses a keyed backup or a create without a person at the
// terminal, before discovery, a log or any write (MA-733's order). A create's
// folder is judged here too, before any password is asked (the P3-1a pass,
// REG-03).
func requireAttended(out string, keyed, creating bool, credentials []string) error {
	if creating {
		if _, err := keyring.CheckCreateFolder(abspath(out), credentials); err != nil {
			return err
		}
	}
	switch {
	case !keyed && !creating, stdAsker().attended():
		return nil
	case creating:
		return errEncryptNeedsTerminal
	}
	return errNeedsTerminal(abspath(out), "a backup into it")
}

// interrupted turns a prompt ended by a signal into the exit a signal gives (130 for
// Ctrl-C, 143 for SIGTERM), after saying nothing was changed.
func interrupted(err error, stderr io.Writer) error {
	var ie *tty.ErrInterrupted
	if !errors.As(err, &ie) {
		return err
	}
	fmt.Fprintln(stderr, "mailarchive: interrupted; nothing was changed")
	return &exitError{code: signalExit(ie.Signal)}
}

// askYes asks a yes-or-no question; Enter alone answers def.
func askYes(a asker, question string, def bool) (bool, error) {
	hint := " [y/N] "
	if def {
		hint = " [Y/n] "
	}
	for {
		ans, err := a.line(question + hint)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		a.say("Please answer y or n.")
	}
}

// askNewPassword asks for a new password twice, checking it before any key is made
// from it.
func askNewPassword(a asker) (string, error) {
	a.say("%s", keyring.PasswordHint)
	for tries := 0; tries < 3; tries++ {
		pw, err := a.secret("Choose a password: ")
		if err != nil {
			return "", err
		}
		if problem := keyring.NewPasswordProblem(pw); problem != "" {
			a.say("%s", problem)
			continue
		}
		again, err := a.secret("Type it again: ")
		if err != nil {
			return "", err
		}
		if again != pw {
			a.say("The two did not match.")
			continue
		}
		return pw, nil
	}
	return "", errors.New("no password was chosen; nothing was changed")
}

// confirmRecoveryKey shows the recovery key, takes it off the screen, and asks for it
// again from where it was saved (K2). Enter alone shows it again.
func confirmRecoveryKey(a asker, key string) error {
	for shows := 0; shows < 5; shows++ {
		if err := a.showKey(key); err != nil {
			return err
		}
		for tries := 0; tries < 3; tries++ {
			typed, err := a.secret("Type or paste the recovery key you saved (Enter alone shows it again): ")
			if err != nil {
				return err
			}
			if strings.TrimSpace(typed) == "" {
				break
			}
			if err := keyring.CheckRecoveryKey(key, typed); err != nil {
				a.say("That is not the recovery key shown (%v).", err)
				continue
			}
			return nil
		}
	}
	return errors.New("the recovery key was not entered again, so nothing was saved")
}

// checkRecoveryKeyOut refuses a -recovery-key-out inside the archive folder or any
// encrypted archive, or over a file that exists, before anything is asked.
func checkRecoveryKeyOut(path, out string) (string, error) {
	return keyring.CheckNewFileOut("-recovery-key-out", "the recovery key", path, out)
}

// writeRecoveryKeyFile saves the recovery key to a NEW file outside the archive and
// every encrypted archive (§5.2), once the key was entered again and just before it
// is committed (the P3-1a pass, SEC-01).
func writeRecoveryKeyFile(path, out string, id vault.ID, key string) (string, error) {
	return keyring.WriteNewFile("-recovery-key-out", "the recovery key", path, out, []byte(keyring.RecoveryKeyFileText(id, key)))
}

// writeRecoveryFileOut saves the recovery file (§9.4) to a NEW file, the same way:
// it opens nothing without the recovery key, but is kept as carefully.
func writeRecoveryFileOut(path, out string, data []byte) (string, error) {
	return keyring.WriteNewFile("-recovery-file-out", "the recovery file", path, out, data)
}

// saveKeyCopies writes the -recovery-key-out and -recovery-file-out copies of a new
// recovery key, after it was entered again and before its commit; on a failure
// the first one written is voided. It returns the files written.
func saveKeyCopies(a asker, k *keyFlags, out string, id vault.ID, key string, file func() ([]byte, error)) ([]string, error) {
	var written []string
	if k.recoveryKeyOut != nil && *k.recoveryKeyOut != "" {
		p, err := writeRecoveryKeyFile(*k.recoveryKeyOut, out, id, key)
		if err != nil {
			return nil, err
		}
		written = append(written, p)
	}
	if k.recoveryFileOut != nil && *k.recoveryFileOut != "" {
		data, err := file()
		if err == nil {
			var p string
			if p, err = writeRecoveryFileOut(*k.recoveryFileOut, out, data); err == nil {
				written = append(written, p)
			}
		}
		if err != nil {
			voidFiles(a, written)
			return nil, err
		}
	}
	return written, nil
}

// checkKeyCopies refuses either copy's path before anything is asked.
func checkKeyCopies(k *keyFlags, out string) error {
	if k.recoveryKeyOut != nil && *k.recoveryKeyOut != "" {
		if _, err := checkRecoveryKeyOut(*k.recoveryKeyOut, out); err != nil {
			return err
		}
	}
	if k.recoveryFileOut != nil && *k.recoveryFileOut != "" {
		if _, err := keyring.CheckNewFileOut("-recovery-file-out", "the recovery file", *k.recoveryFileOut, out); err != nil {
			return err
		}
	}
	return nil
}

// voidFiles voids each saved copy of a recovery key that did not take effect.
func voidFiles(a asker, paths []string) {
	for _, p := range paths {
		voidRecoveryKeyFile(a, p)
	}
}

// voidRecoveryKeyFile overwrites and removes a -recovery-key-out file whose key never
// took effect, so it is not later mistaken for the archive's recovery key, and says
// so (the P3-1a pass, SEC-01, CON-03).
func voidRecoveryKeyFile(a asker, path string) {
	if path == "" {
		return
	}
	if err := keyring.VoidFile(path); err != nil {
		a.say("warning: %s holds a recovery key that is NOT in effect and could not be removed (%v) — delete it yourself", path, err)
		return
	}
	a.say("%s was removed: the recovery key it held is not in effect.", path)
}

// secretStore is the OS secret store remembered keys live in.
var secretStore = func() (vault.Store, error) {
	s, err := graphconfig.DefaultSecretStore()
	if err != nil {
		return nil, err
	}
	return s, nil
}

// createEncrypted makes out a new encrypted archive at the terminal (§5.1, §5.2):
// the password twice, whether to remember the key, the recovery key shown, taken off
// the screen and entered again, then the key file, the record and the remembered key.
// The archive comes back unlocked for its first backup.
func createEncrypted(a asker, out string, k *keyFlags, rememberSet bool, credentials []string) (*keyring.Unlocked, error) {
	if !a.attended() {
		return nil, errEncryptNeedsTerminal
	}
	// The folder and -recovery-key-out are judged before anything is asked (§5.1
	// step 1; the P3-1a pass, REG-03).
	if _, err := keyring.CheckCreateFolder(out, credentials); err != nil {
		return nil, err
	}
	if err := checkKeyCopies(k, out); err != nil {
		return nil, err
	}
	pw, err := askNewPassword(a)
	if err != nil {
		return nil, err
	}
	remember := *k.remember
	if !rememberSet {
		remember, err = askYes(a, "Remember the key on this computer? Backups you start and opening the archive here will not ask for the password. Anyone using this computer's account can open it.", true)
		if err != nil {
			return nil, err
		}
	}
	var store vault.Store
	if remember {
		if store, err = secretStore(); err != nil {
			a.say("warning: the key cannot be remembered (%v); every backup and opening will ask for the password", err)
			remember = false
		}
	}
	a.say("Making the archive's key (this takes a few seconds)...")
	p, err := keyring.Create(out, pw, keyring.CreateOptions{Remember: remember, Store: store, CredentialPaths: credentials})
	if err != nil {
		return nil, err
	}
	// Discard does nothing once Commit wrote the key file.
	defer p.Discard()
	if err := confirmRecoveryKey(a, p.RecoveryKey()); err != nil {
		return nil, err
	}
	saved, err := saveKeyCopies(a, k, out, p.ArchiveID(), p.RecoveryKey(), p.RecoveryFile)
	if err != nil {
		return nil, err
	}
	u, err := p.Commit()
	if err != nil {
		// The key file may have been written though the record was not: the archive
		// exists and its recovery key is in force, so its saved copies stay (CRIT-04).
		if errors.Is(err, keyring.ErrCreatedNotRecorded) {
			for _, f := range saved {
				a.say("The recovery key is in force; %s is kept.", f)
			}
			return nil, err
		}
		voidFiles(a, saved)
		return nil, err
	}
	if k.recoveryFileOut != nil && *k.recoveryFileOut != "" {
		a.say("%s", keyring.RecoveryFileNote)
	}
	for _, w := range u.Warnings() {
		a.say("warning: %s", w)
	}
	a.say("The encrypted archive is ready at %s. Its first backup starts now.", out)
	return u, nil
}

// unlockForWrite opens the encrypted archive at out for a backup or a key change, at
// the terminal (§3.4): a password or recovery-key file first, then the key this
// computer remembers, then the password typed (a recovery key typed is taken as
// one). The folder is recorded if it was not (adoption) and a damaged copy of its
// key file healed.
func unlockForWrite(a asker, out, what string, k *keyFlags) (*keyring.Unlocked, error) {
	if !a.attended() {
		return nil, errNeedsTerminal(out, what)
	}
	store, serr := secretStore()
	if serr != nil {
		store = nil
	}
	remember := k.remember != nil && *k.remember
	if remember && store == nil {
		return nil, fmt.Errorf("the key cannot be remembered: %w", serr)
	}
	// The questions asked inside the open pass an interruption on, so Ctrl-C there
	// ends as an interrupt, not as "no" (the P3-1a pass, CON-08).
	o := keyring.OpenOptions{Write: true, MayHeal: true, Store: store, Remember: remember,
		ConfirmCost: func(p vault.Params) (bool, error) {
			return askYes(a, fmt.Sprintf("This archive's key file asks for %s to open, more than MailArchive's own (%s), and this computer has not recorded it. Open it anyway?", p, vault.CostFloor()), false)
		},
		ConfirmReRecord: func(string) (bool, error) {
			return askYes(a, "You told MailArchive to forget this encrypted archive. Record it again on this computer?", false)
		},
	}
	u, err := openBySecret(a, out, k, store, o)
	if err != nil {
		return nil, err
	}
	for _, w := range u.Warnings() {
		a.say("warning: %s", w)
	}
	// Setting up finished, or an archive handed over, is offered Remember again,
	// on by default (§5.1 step 7; the P3-1a pass, CON-07).
	if u.Adopted() && !k.rememberGiven && !keyring.HasRemembered(out, store) && store != nil {
		yes, err := askYes(a, "Remember the key on this computer? Backups you start and opening the archive here will not ask for the password. Anyone using this computer's account can open it.", true)
		if err != nil {
			u.Destroy()
			return nil, err
		}
		if yes {
			if err := keyring.Remember(u, store); err != nil {
				a.say("warning: the key could not be remembered (%v); every backup and opening will ask for the password", err)
			}
		}
	}
	return u, nil
}

// openBySecret opens out in the order of §3.4: a password or recovery-key file,
// then the key this computer remembers, then the password typed (a recovery key
// typed is taken as one), three tries.
func openBySecret(a asker, out string, k *keyFlags, store vault.Store, o keyring.OpenOptions) (*keyring.Unlocked, error) {
	open := func(s keyring.Secret, err error) (*keyring.Unlocked, error) {
		if err != nil {
			return nil, err
		}
		return keyring.Open(out, s, o)
	}
	var u *keyring.Unlocked
	var err error
	switch {
	case k.passwordFile != nil && *k.passwordFile != "":
		u, err = open(keyring.PasswordFile(*k.passwordFile))
	case k.recoveryKeyFile != nil && *k.recoveryKeyFile != "":
		u, err = open(keyring.RecoveryKeyFile(*k.recoveryKeyFile))
	case keyring.HasRemembered(out, store):
		u, err = keyring.Remembered(out, o)
	default:
		for tries := 0; tries < 3; tries++ {
			typed, perr := a.secret(fmt.Sprintf("Password (or recovery key) for the encrypted archive %s: ", out))
			if perr != nil {
				return nil, perr
			}
			u, err = keyring.OpenTyped(out, typed, o)
			if !errors.Is(err, vault.ErrWrongSecret) {
				break
			}
			a.say("That does not open this archive.")
		}
	}
	return u, err
}

// unlockForRead opens the encrypted archive at out to read it — serve, search,
// extract (§3.4, §7.5): a password or recovery-key file first, then the key this
// computer remembers, then the password typed at a terminal. It heals nothing and
// records nothing — unless -remember asks it to remember the key, which records
// the folder (§3.3). Without a terminal it asks nothing: with no file and no
// remembered key it is refused, naming the files that would do.
func unlockForRead(a asker, out, what string, k *keyFlags) (*keyring.Unlocked, error) {
	store, serr := secretStore()
	if serr != nil {
		store = nil
	}
	remember := k.remember != nil && *k.remember
	if remember && store == nil {
		return nil, fmt.Errorf("the key cannot be remembered: %w", serr)
	}
	o := keyring.OpenOptions{Store: store, Remember: remember}
	attended := a.attended()
	if attended {
		o.ConfirmCost = func(p vault.Params) (bool, error) {
			return askYes(a, fmt.Sprintf("This archive's key file asks for %s to open, more than MailArchive's own (%s), and this computer has not recorded it. Open it anyway?", p, vault.CostFloor()), false)
		}
		o.ConfirmReRecord = func(string) (bool, error) {
			return askYes(a, "You told MailArchive to forget this encrypted archive. Record it again on this computer?", false)
		}
	}
	if err := readKeyAvailable(a, out, what, k); err != nil {
		return nil, err
	}
	u, err := openBySecret(a, out, k, store, o)
	if err != nil {
		return nil, err
	}
	for _, w := range u.Warnings() {
		a.say("warning: %s", w)
	}
	return u, nil
}

// readKeyAvailable refuses a keyed read with no way to unlock the archive: no
// terminal to ask at, no secret file and no remembered key (§3.4) — before anything
// else is judged or asked.
func readKeyAvailable(a asker, out, what string, k *keyFlags) error {
	if a.attended() || (k.passwordFile != nil && *k.passwordFile != "") || (k.recoveryKeyFile != nil && *k.recoveryKeyFile != "") {
		return nil
	}
	store, err := secretStore()
	if err == nil && keyring.HasRemembered(out, store) {
		return nil
	}
	return errNeedsKeyToRead(out, what)
}

// errNeedsKeyToRead refuses a keyed read with no secret to open it by and no
// terminal to ask at (§3.4).
func errNeedsKeyToRead(out, what string) error {
	return fmt.Errorf("%s is an encrypted archive: %s needs its password or recovery key — run it at a terminal, or give -password-file or -recovery-key-file (a file readable only by you)", out, what)
}

// readKeyFlags adds a read verb's key flags: the two secret files and -remember.
func readKeyFlags(fs *flag.FlagSet) *keyFlags {
	k := &keyFlags{}
	addSecretFileFlags(fs, k)
	k.remember = fs.Bool("remember", false, "for an encrypted archive: also remember its key on this computer, so opening it here does not ask for the password (anyone using this computer's account can then open it)")
	return k
}

// refuseReadKeyFlags refuses a read verb's key flags on an archive that is not
// encrypted, naming those given (R12).
func refuseReadKeyFlags(fs *flag.FlagSet) error {
	if given := readKeyFlagsGiven(fs); given != "" {
		return fmt.Errorf("%s: for an encrypted archive only, and this archive is not encrypted", given)
	}
	return nil
}

// readKeyFlagsGiven names the read verbs' key flags given, "" for none.
func readKeyFlagsGiven(fs *flag.FlagSet) string {
	var given []string
	for _, n := range []string{"password-file", "recovery-key-file", "remember"} {
		if flagWasSet(fs, n) {
			given = append(given, "-"+n)
		}
	}
	return strings.Join(given, ", ")
}
