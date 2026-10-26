package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tty"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
	"mail-archive-tool/internal/winsec"
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
}

// keyFlagNames are the flags parseJob refuses in a scheduled job.
var keyFlagNames = []string{"encrypt", "remember", "password-file", "recovery-key-file", "recovery-key-out"}

func addKeyFlags(fs *flag.FlagSet) *keyFlags {
	k := &keyFlags{}
	k.encrypt = fs.Bool("encrypt", false, "make -out a new ENCRYPTED archive (a new or empty folder), at a terminal: you choose a password and are shown a recovery key to save")
	k.remember = fs.Bool("remember", false, "remember an encrypted archive's key on this computer, so backups you start here and opening it do not ask for the password (anyone using this computer's account can then open it); -encrypt asks when this is not given")
	addSecretFileFlags(fs, k)
	k.recoveryKeyOut = fs.String("recovery-key-out", "", "with -encrypt: also save the new recovery key to this NEW file, readable only by you, outside the archive")
	return k
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
		return errors.New("-remember, -password-file, -recovery-key-file and -recovery-key-out are for an encrypted archive: make one with -encrypt, or give -out an encrypted archive")
	case keyed && *k.recoveryKeyOut != "":
		return errors.New("-recovery-key-out saves the recovery key of a NEW encrypted archive (-encrypt); for a new recovery key run `mailarchive new-recovery-key`")
	case creating && (*k.passwordFile != "" || *k.recoveryKeyFile != ""):
		return errors.New("-encrypt asks for the new archive's password at the terminal; -password-file and -recovery-key-file open an existing one")
	}
	return nil
}

// openForBackup makes, or unlocks, the encrypted archive a backup runs into, at the
// terminal; nil for a plaintext archive.
func openForBackup(fs *flag.FlagSet, k *keyFlags, out string, keyed, creating bool) (*keyring.Unlocked, error) {
	var u *keyring.Unlocked
	var err error
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
	for _, p := range []*string{k.passwordFile, k.recoveryKeyFile, k.recoveryKeyOut} {
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

// recoveryWarning is the G4 warning, shown in full with every recovery key.
const recoveryWarning = "Save this recovery key in your password manager now. If you lose both your password and this key, the archive can never be opened — not by you, not by us. There is no reset."

func (a ttyAsker) showKey(key string) error {
	erase := tty.CanErase(a.out)
	if erase {
		fmt.Fprint(a.out, "\x1b[?1049h\x1b[H\x1b[2J")
	}
	fmt.Fprintf(a.out, "Your recovery key:\n\n    %s\n\n%s\n\n", key, recoveryWarning)
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
// terminal, before discovery, a log or any write (MA-733's order).
func requireAttended(out string, keyed, creating bool) error {
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

// passwordHint is said before a new password is asked for (§5.1 step 2).
const passwordHint = "Use at least 12 characters; a longer phrase is stronger. A copy of the archive made before you change the password keeps the old one."

// askNewPassword asks for a new password twice, checking its length before any key
// is made from it.
func askNewPassword(a asker) (string, error) {
	a.say("%s", passwordHint)
	for tries := 0; tries < 3; tries++ {
		pw, err := a.secret("Choose a password: ")
		if err != nil {
			return "", err
		}
		if n := len([]rune(norm.NFC.String(pw))); n < vault.MinPasswordRunes {
			a.say("That has %d characters; use at least %d (a passphrase of several words is easiest).", n, vault.MinPasswordRunes)
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

// writeRecoveryKeyFile saves the recovery key to a NEW file outside the archive and
// every encrypted archive, owner-only, flushed and its folder synced before the key
// is committed (§5.2). A Windows access list that cannot be set is a warning.
func writeRecoveryKeyFile(a asker, path, out, id, key string) error {
	abs := abspath(path)
	if util.Within(util.ResolveExisting(out), util.ResolveExisting(abs)) {
		return fmt.Errorf("-recovery-key-out %s is inside the archive folder %s; save the recovery key outside it", abs, out)
	}
	if dir, why, _ := encmark.AtOrAbove(filepath.Dir(abs)); dir != "" {
		return fmt.Errorf("-recovery-key-out %s is inside the encrypted archive %s (%s); save the recovery key outside every archive", abs, dir, why)
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("-recovery-key-out %s already exists; choose a new file", abs)
		}
		return fmt.Errorf("-recovery-key-out %s: %w", abs, err)
	}
	_, werr := fmt.Fprintf(f, "MailArchive recovery key for the encrypted archive %s\n%s\n", id, key)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(abs)
		return fmt.Errorf("-recovery-key-out %s could not be written: %w", abs, werr)
	}
	util.SyncDir(filepath.Dir(abs))
	if err := winsec.RestrictToOwner(abs); err != nil {
		a.say("warning: %s could not be made readable only by you (%v); check who can read it", abs, err)
	}
	return nil
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
	committed := false
	defer func() {
		if !committed {
			p.Discard()
		}
	}()
	if *k.recoveryKeyOut != "" {
		if err := writeRecoveryKeyFile(a, *k.recoveryKeyOut, out, p.ArchiveID().String(), p.RecoveryKey()); err != nil {
			return nil, err
		}
	}
	if err := confirmRecoveryKey(a, p.RecoveryKey()); err != nil {
		return nil, err
	}
	u, err := p.Commit()
	if err != nil {
		return nil, err
	}
	committed = true
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
	o := keyring.OpenOptions{Write: true, MayHeal: true, Store: store, Remember: remember,
		ConfirmCost: func(p vault.Params) bool {
			ok, _ := askYes(a, fmt.Sprintf("This archive's key file asks for %s to open, more than MailArchive's own (%s), and this computer has not recorded it. Open it anyway?", p, vault.CostFloor()), false)
			return ok
		},
		ConfirmReRecord: func(string) bool {
			ok, _ := askYes(a, "You told MailArchive to forget this encrypted archive. Record it again on this computer?", false)
			return ok
		},
	}
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
			u, err = keyring.Open(out, keyring.Typed(typed), o)
			if !errors.Is(err, vault.ErrWrongSecret) {
				break
			}
			a.say("That does not open this archive.")
		}
	}
	if err != nil {
		return nil, err
	}
	for _, w := range u.Warnings() {
		a.say("warning: %s", w)
	}
	return u, nil
}
