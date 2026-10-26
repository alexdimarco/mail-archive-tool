package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// keyAnswer stands, in a script, for the recovery key last shown.
const keyAnswer = "<the key shown>"

// scripted is the person at the terminal, answering from a script: each secret or
// line takes the next answer. It records what was said and every key shown.
type scripted struct {
	isAttended bool
	answers    []string
	prompts    []string
	said       []string
	shown      []string
}

func (s *scripted) attended() bool { return s.isAttended }

func (s *scripted) next(prompt string) (string, error) {
	s.prompts = append(s.prompts, prompt)
	if len(s.answers) == 0 {
		return "", fmt.Errorf("the script ran out at %q", prompt)
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	if a == keyAnswer && len(s.shown) > 0 {
		a = s.shown[len(s.shown)-1]
	}
	return a, nil
}

func (s *scripted) secret(prompt string) (string, error) { return s.next(prompt) }
func (s *scripted) line(prompt string) (string, error)   { return s.next(prompt) }
func (s *scripted) say(format string, a ...any)          { s.said = append(s.said, fmt.Sprintf(format, a...)) }
func (s *scripted) showKey(key string) error             { s.shown = append(s.shown, key); return nil }

func (s *scripted) transcript() string {
	return strings.Join(append(append([]string{}, s.prompts...), s.said...), "\n")
}

const flowPassword = "cli flow test passphrase"

// flowRecord gives a test its own record and secret store.
func flowRecord(t *testing.T) vault.Store {
	t.Helper()
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	store := graphconfig.NewFileSecretStore(t.TempDir())
	orig := secretStore
	secretStore = func() (vault.Store, error) { return store, nil }
	t.Cleanup(func() { secretStore = orig })
	return store
}

func noKeyFlags() *keyFlags {
	f, empty := false, ""
	return &keyFlags{encrypt: &f, remember: &f, passwordFile: &empty, recoveryKeyFile: &empty, recoveryKeyOut: &empty}
}

func recordedAt(t *testing.T, out string) string {
	t.Helper()
	rec, err := encrecord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := rec.Lookup(out); ok {
		return m.ID
	}
	return ""
}

// covers: MA-786, R37
func TestTheCLIMakesAnEncryptedArchiveAtTheTerminal(t *testing.T) {
	store := flowRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	keyFile := filepath.Join(t.TempDir(), "recovery.txt")
	k := noKeyFlags()
	k.recoveryKeyOut = &keyFile
	// A short password, then a mismatch, then the password twice; remember (Enter:
	// yes); Enter alone shows the key again; then the key.
	a := &scripted{isAttended: true, answers: []string{"short", flowPassword, "not the same", flowPassword, flowPassword, "", "", keyAnswer}}
	u, err := createEncrypted(a, out, k, false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	defer u.Destroy()
	tr := a.transcript()
	assure.Reached(t, len(a.shown) == 2 && a.shown[0] == a.shown[1], "the key shown, and shown again on Enter alone")
	for _, want := range []string{passwordHint, "at least 12", "did not match", "Remember the key"} {
		if !strings.Contains(tr, want) {
			t.Fatalf("the terminal never said %q:\n%s", want, tr)
		}
	}
	if recordedAt(t, out) != u.ArchiveID().String() {
		t.Fatal("the new archive was not recorded")
	}
	if _, err := store.Get(vault.Account(u.ArchiveID())); err != nil {
		t.Fatalf("the key was not remembered: %v", err)
	}
	b, err := os.ReadFile(keyFile)
	if err != nil || !strings.Contains(string(b), a.shown[0]) {
		t.Fatalf("-recovery-key-out: %q %v", b, err)
	}
	if fi, _ := os.Stat(keyFile); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("-recovery-key-out is %v, want 0600", fi.Mode().Perm())
	}
}

// covers: MA-786, R37
func TestTheCLIMakesNothingUntilTheRecoveryKeyIsEnteredAgain(t *testing.T) {
	flowRecord(t)
	keyFile := func() string { return filepath.Join(t.TempDir(), "recovery.txt") }
	cases := map[string]struct {
		attended bool
		answers  []string
		out      func(string) string
		keyOut   func(string) string
		names    []string
	}{
		"no terminal":               {false, nil, nil, nil, []string{"terminal"}},
		"three short passwords":     {true, []string{"a", "b", "c"}, nil, nil, []string{"no password was chosen"}},
		"the key never entered":     {true, []string{flowPassword, flowPassword, "n", "wrong", "wrong again", "still wrong"}, nil, nil, []string{"ran out"}},
		"the key out inside it":     {true, []string{flowPassword, flowPassword, "n"}, nil, func(out string) string { return filepath.Join(out, "rk.txt") }, []string{"inside the archive folder"}},
		"the key out already there": {true, []string{flowPassword, flowPassword, "n"}, nil, func(string) string { p := keyFile(); os.WriteFile(p, nil, 0o600); return p }, []string{"already exists"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "archive")
			k := noKeyFlags()
			if c.keyOut != nil {
				p := c.keyOut(out)
				k.recoveryKeyOut = &p
			}
			a := &scripted{isAttended: c.attended, answers: c.answers}
			_, err := createEncrypted(a, out, k, false, nil)
			code, msg := 0, ""
			if err != nil {
				code, msg = 1, err.Error()
			}
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(c.names...),
				assure.NoSideEffect(func() bool {
					_, err := os.Lstat(filepath.Join(out, vault.FileName))
					return errors.Is(err, os.ErrNotExist) && recordedAt(t, out) == ""
				}))
		})
	}
}

// makeArchive makes an encrypted archive through the CLI's own flow, not
// remembering its key, and returns its folder.
func makeArchive(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "archive")
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", keyAnswer}}
	u, err := createEncrypted(a, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("make the archive: %v\n%s", err, a.transcript())
	}
	u.Destroy()
	return out
}

// covers: MA-787, R37
func TestTheCLIUnlocksForABackupFromEachSecretSource(t *testing.T) {
	store := flowRecord(t)
	out := makeArchive(t)
	pwFile := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte(flowPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	k := noKeyFlags()
	k.passwordFile = &pwFile
	a := &scripted{isAttended: true}
	u, err := unlockForWrite(a, out, "a backup into it", k)
	if err != nil {
		t.Fatalf("by -password-file: %v", err)
	}
	if len(a.prompts) != 0 {
		t.Fatalf("a password file still asked: %v", a.prompts)
	}
	u.Destroy()

	// Typed: a wrong password, then the right one; -remember files the key.
	yes := true
	k = noKeyFlags()
	k.remember = &yes
	a = &scripted{isAttended: true, answers: []string{"not the password at all", flowPassword}}
	u, err = unlockForWrite(a, out, "a backup into it", k)
	if err != nil {
		t.Fatalf("typed: %v\n%s", err, a.transcript())
	}
	id := u.ArchiveID()
	u.Destroy()
	if !strings.Contains(a.transcript(), "does not open this archive") {
		t.Fatalf("a wrong password was not told:\n%s", a.transcript())
	}
	if _, err := store.Get(vault.Account(id)); err != nil {
		t.Fatalf("-remember did not remember the key: %v", err)
	}

	// Remembered: no prompt at all.
	a = &scripted{isAttended: true}
	u, err = unlockForWrite(a, out, "a backup into it", noKeyFlags())
	assure.Reached(t, u, "the archive opened by the remembered key")
	if err != nil || len(a.prompts) != 0 || u.How() != keyring.ByRememberedKey {
		t.Fatalf("by the remembered key: %v, prompts %v", err, a.prompts)
	}
	u.Destroy()

	// No terminal: refused, naming why, before any secret is read.
	a = &scripted{isAttended: false}
	_, err = unlockForWrite(a, out, "a backup into it", k)
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "terminal", "not available in this version"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
}

// covers: MA-787, R37
func TestTheCLIKeyVerbs(t *testing.T) {
	store := flowRecord(t)
	out := makeArchive(t)

	// change-password, by the password typed.
	next := "the second cli passphrase"
	a := &scripted{isAttended: true, answers: []string{flowPassword, next, next}}
	if err := changePassword(a, out, noKeyFlags()); err != nil {
		t.Fatalf("change-password: %v\n%s", err, a.transcript())
	}
	if u, err := keyring.Open(out, keyring.Password(next), keyring.OpenOptions{}); err != nil {
		t.Fatalf("the new password does not open: %v", err)
	} else {
		u.Destroy()
	}

	// new-recovery-key: shown, entered again, in effect.
	a = &scripted{isAttended: true, answers: []string{next, keyAnswer}}
	if err := newRecoveryKey(a, out, noKeyFlags()); err != nil {
		t.Fatalf("new-recovery-key: %v\n%s", err, a.transcript())
	}
	if u, err := keyring.Open(out, keyring.RecoveryKey(a.shown[0]), keyring.OpenOptions{}); err != nil {
		t.Fatalf("the new recovery key does not open: %v", err)
	} else {
		u.Destroy()
	}
	assure.Reached(t, strings.Contains(a.transcript(), "destroy those"), "the earlier key's warning")

	// Authorised by the remembered key, a change asks first (K13); no leaves it.
	yes := true
	k := noKeyFlags()
	k.remember = &yes
	a = &scripted{isAttended: true, answers: []string{next}}
	u, err := unlockForWrite(a, out, "", k)
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	a = &scripted{isAttended: true, answers: []string{"n"}}
	err = changePassword(a, out, noKeyFlags())
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("nothing was changed"),
		assure.NoSideEffect(func() bool {
			u, err := keyring.Open(out, keyring.Password(next), keyring.OpenOptions{})
			if err == nil {
				u.Destroy()
			}
			return err == nil
		}))
	if !strings.Contains(a.transcript(), "not this computer") {
		t.Fatalf("the K13 question must carry the R-E2 disclosure:\n%s", a.transcript())
	}

	// forget-key, then forget-archive (the last path: the whole archive, its key).
	a = &scripted{isAttended: true}
	if err := forgetKey(a, out); err != nil {
		t.Fatal(err)
	}
	if keyring.HasRemembered(out, store) {
		t.Fatal("forget-key left the key remembered")
	}
	a = &scripted{isAttended: true, answers: []string{"y", "y"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("forget-archive: %v\n%s", err, a.transcript())
	}
	if recordedAt(t, out) != "" {
		t.Fatal("forget-archive left the folder recorded")
	}
}

// covers: MA-787, R37
func TestForgetArchiveAsksForThePathWhenTheFolderMayHaveBeenTamperedWith(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &scripted{isAttended: true, answers: []string{"/some/other/path"}}
	err := forgetArchive(a, out)
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("does not match", "nothing was changed"),
		assure.NoSideEffect(func() bool { return recordedAt(t, out) != "" }))
	if !strings.Contains(a.transcript(), "tampered") {
		t.Fatalf("the strong confirmation must say why:\n%s", a.transcript())
	}
	a = &scripted{isAttended: true, answers: []string{out, "n"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("the path typed back: %v\n%s", err, a.transcript())
	}
	assure.Reached(t, recordedAt(t, out) == "", "the folder forgotten once its path was typed back")
}

// covers: MA-786
func TestEncryptionFlagsAreRefusedWhereTheyCannotWork(t *testing.T) {
	for name, err := range map[string]error{
		"-encrypt -unattended": refuseEncryptWith(true, true, false, false),
		"-encrypt -manifest":   refuseEncryptWith(true, false, true, false),
		"-encrypt -list":       refuseEncryptWith(true, false, false, true),
	} {
		if err == nil {
			t.Fatalf("%s was allowed", name)
		}
	}
	assure.Reached(t, refuseEncryptWith(false, true, true, true) == nil, "no refusal without -encrypt")

	// A key flag on a run that neither makes nor opens an encrypted archive, and one
	// that does not fit the run, are refused; the fitting ones pass.
	stray := func(keyed, creating bool, args ...string) error {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		k := addKeyFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return refuseStrayKeyFlags(fs, k, keyed, creating)
	}
	for name, err := range map[string]error{
		"-password-file on a plaintext archive": stray(false, false, "-password-file", "x"),
		"-remember on a plaintext archive":      stray(false, false, "-remember"),
		"-recovery-key-out on an existing one":  stray(true, false, "-recovery-key-out", "x"),
		"-password-file with -encrypt":          stray(false, true, "-encrypt", "-password-file", "x"),
	} {
		if err == nil {
			t.Fatalf("%s was allowed", name)
		}
	}
	for name, err := range map[string]error{
		"nothing":                              stray(false, false),
		"-password-file on an encrypted":       stray(true, false, "-password-file", "x"),
		"-encrypt -remember -recovery-key-out": stray(false, true, "-encrypt", "-remember", "-recovery-key-out", "x"),
	} {
		if err != nil {
			t.Fatalf("%s was refused: %v", name, err)
		}
	}
	code, stderr := runCLI("-out", t.TempDir(), "-input", "../../testdata/support.pst", "-password-file", "x")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-password-file", "encrypted archive"))
	for _, flag := range keyFlagNames {
		_, err := parseJob([]string{"-out", t.TempDir(), "-input", t.TempDir(), "-" + flag})
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("-"+flag, "cannot be scheduled"))
	}
	for _, verb := range []string{"change-password", "new-recovery-key", "forget-key", "forget-archive", "repair-record", "encrypt"} {
		if _, err := parseJob([]string{verb, "-out", t.TempDir()}); err == nil || !strings.Contains(err.Error(), "cannot be scheduled") {
			t.Fatalf("%s scheduled: %v", verb, err)
		}
	}
}
