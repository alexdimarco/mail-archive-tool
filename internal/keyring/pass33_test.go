package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The P3-3 adversarial pass's keyring findings, each kept as a guard.

// covers: MA-817, R37
// Forget-all forgets every key this computer remembers (K8; FID-01): the key of an
// archive the record lost goes too, before the lost list that points at it.
func TestForgetAllForgetsALostArchivesKeyToo(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, w := made(t, s)
	id := w.ArchiveID()
	if err := Remember(w, s); err != nil {
		t.Fatal(err)
	}
	w.Destroy()
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id.String()); return nil })
	encrecord.MarkLost(id.String())
	if !HasRemembered(out, s) {
		t.Fatal("fixture: the lost archive's key is remembered")
	}
	if _, err := ForgetAll(s); err != nil {
		t.Fatal(err)
	}
	lost, _ := encrecord.Lost()
	_, gerr := s.Get(vault.Account(id))
	assure.Reached(t, gerr != nil && len(lost) == 0, "the lost archive's remembered key and its mark are gone")
}

// covers: MA-818, R37
// A recovery key or file that cannot be made readable only by its owner is not saved,
// and at the command line the refusal names leaving out the flag — the one remedy
// that always works (REG-03); the Desktop's words name no flag.
func TestANewFileThatCannotBeMadeOwnerOnlyNamesItsRemedy(t *testing.T) {
	orig := restrictToOwner
	restrictToOwner = func(string) error { return errors.New("this drive keeps no permissions") }
	defer func() { restrictToOwner = orig }()
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery-key.txt")
	_, err := WriteNewFile("-recovery-key-out", "the recovery key", path, t.TempDir(), []byte("key"))
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(path, "leave out -recovery-key-out"),
		assure.NoSideEffect(func() bool { return !exists(path) }))
	_, err = WriteNewFile("The file chosen", "the recovery file", path, t.TempDir(), []byte("file"))
	if err == nil || strings.Contains(err.Error(), "leave out") {
		t.Fatalf("the Desktop's words: %v", err)
	}
}

// covers: MA-817, R37
// extract takes an encrypted archive's lock with no verb (OUT-C): nothing left beside
// it says readable mail was copied out.
func TestExtractLeavesNoVerbBesideAnEncryptedArchive(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	if _, err := Extract(w, app.FormatEML, filepath.Join(t.TempDir(), "x"), false, quiet, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, lockfile.Name))
	assure.Reached(t, err == nil && strings.Contains(string(b), "pid=") && !strings.Contains(string(b), "verb="), "the lock file holds a bare holder line")
}

// covers: MA-818, R37
// A recovery key that does not read as one is refused before the recovery file is
// opened, in words that quote none of what was typed (HTTP-01).
func TestARestoreRefusesAMistypedRecoveryKeyWithoutQuotingIt(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	w.Destroy()
	// One of the wrong length, and one of the right length with a character outside the
	// recovery key's alphabet: the parse's own detail names the length, or the
	// character and its place.
	for _, typed := range []string{"hunter2-Q!", strings.Repeat("A", 26) + "1"} {
		_, err := RestoreFromFile(out, []byte("not looked at"), RecoveryKey(typed), "a new password here", RestoreOptions{})
		if err == nil {
			t.Fatalf("%q was accepted as a recovery key", typed)
		}
		for _, leak := range []string{"character", "hunter", "'1'", "27"} {
			if strings.Contains(err.Error(), leak) {
				t.Fatalf("the refusal quotes what was typed (%q): %v", leak, err)
			}
		}
		assure.Reached(t, errors.Is(err, vault.ErrRecoveryFormat) || errors.Is(err, vault.ErrRecoveryCheck), "refused as not a recovery key")
	}
}
