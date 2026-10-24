package vault

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// writeLog records each vault write save makes, in order.
type writeLog struct{ names, patterns []string }

// hookWrites observes every vault write and makes the write of failName (a base
// name; "" for none) fail before it touches the disk. Each call replaces the
// previous hook; every hook wraps the real writer.
func hookWrites(t *testing.T, failName string) *writeLog {
	t.Helper()
	w := &writeLog{}
	writeCopy = func(path string, data []byte, pattern string) error {
		w.names = append(w.names, filepath.Base(path))
		w.patterns = append(w.patterns, pattern)
		if filepath.Base(path) == failName {
			return errors.New("injected: disk full")
		}
		return util.WriteFileAtomic0600Temp(path, data, pattern)
	}
	t.Cleanup(func() { writeCopy = util.WriteFileAtomic0600Temp })
	return w
}

// covers: MA-714, R37
func TestSaveWritesThePrimaryThenTheMirrorWithSweepableTemps(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	w := hookWrites(t, "")
	if err := ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), "the replacement passphrase"); err != nil {
		t.Fatal(err)
	}
	names := assure.Reached(t, w.names, "vault writes")
	if len(names) != 2 || names[0] != FileName || names[1] != BackupName {
		t.Fatalf("writes %v, want [%s %s] — the primary is the commit point (A7)", names, FileName, BackupName)
	}
	for _, p := range w.patterns {
		if p != ".mailarchive-*.tmp" {
			t.Fatalf("temp pattern %q, want .mailarchive-*.tmp (A8: the name SweepOrphans reaps)", p)
		}
	}
}

// covers: MA-714, R37
func TestCreateIsPreparedThenCommitted(t *testing.T) {
	lowCost(t)
	out := t.TempDir()
	p, err := PrepareCreate(out, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if s := snapshot(out); s[0] != nil || s[1] != nil {
		t.Fatal("PrepareCreate must write nothing — the recovery key is not shown yet")
	}
	assure.Reached(t, p.RecoveryKey().String(), "recovery key to show")
	if p.ArchiveID() == (ID{}) {
		t.Fatal("the prepared vault must carry its archive id, to record outside the archive")
	}
	p.Discard()
	if s := snapshot(out); s[0] != nil || s[1] != nil {
		t.Fatal("a discarded create must leave the folder untouched")
	}

	p, err = PrepareCreate(out, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	o, err := Open(out, ID{}, RecoverySecret(p.RecoveryKey().String()))
	if err != nil {
		t.Fatalf("the recovery key shown before Commit must open the committed vault: %v", err)
	}
	if o.Key.master != k.master || o.Vault.ArchiveID != p.ArchiveID() {
		t.Fatal("the committed vault is not the prepared one")
	}
}

// covers: MA-714, R37
func TestAFailedCreateLeavesNothingSoARetryWorks(t *testing.T) {
	lowCost(t)
	for _, failing := range []string{FileName, BackupName} {
		out := t.TempDir()
		hookWrites(t, failing)
		_, _, err := createAt(out, testPassword)
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("none was kept"), assure.NoSideEffect(func() bool {
			s := snapshot(out)
			return s[0] == nil && s[1] == nil
		}))
		hookWrites(t, "")
		if _, _, err := createAt(out, testPassword); err != nil {
			t.Fatalf("with %s failing once, a retry must succeed on the clean folder: %v", failing, err)
		}
	}
}

// covers: MA-714, R37
func TestANewRecoveryKeyIsPreparedThenCommitted(t *testing.T) {
	lowCost(t)
	out, _, oldRK := newArchive(t)
	p, err := PrepareNewRecovery(out, ID{}, PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(out)
	if _, err := Open(out, ID{}, RecoverySecret(oldRK.String())); err != nil {
		t.Fatalf("until Commit the old recovery key must keep working: %v", err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(out); bytes.Equal(s[0], before[0]) || !bytes.Equal(s[0], s[1]) {
		t.Fatal("Commit must rewrite both copies identically")
	}
	if _, err := Open(out, ID{}, RecoverySecret(p.RecoveryKey().String())); err != nil {
		t.Fatalf("the new recovery key must open: %v", err)
	}
	if _, err := Open(out, ID{}, RecoverySecret(oldRK.String())); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old recovery key must be refused after Commit, got %v", err)
	}

	stale, err := PrepareNewRecovery(out, ID{}, PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	if err := ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), "changed meanwhile, elsewhere"); err != nil {
		t.Fatal(err)
	}
	changed := snapshot(out)
	err = stale.Commit()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a commit over a vault that changed since prepare: want ErrConflict, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("make it again"), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return bytes.Equal(s[0], changed[0]) && bytes.Equal(s[1], changed[1])
	}))
}

// covers: MA-714, R37
func TestAHalfSavedChangeSaysItIsInEffect(t *testing.T) {
	lowCost(t)

	out, _, oldRK := newArchive(t)
	hookWrites(t, BackupName)
	rk, err := regenerateRecoveryAt(out, ID{}, PasswordSecret(testPassword))
	if !errors.Is(err, ErrPartiallySaved) {
		t.Fatalf("mirror write failed: want ErrPartiallySaved, got %v", err)
	}
	if rk == (RecoveryKey{}) {
		t.Fatal("a recovery key already in effect must be returned with the error, never dropped")
	}
	hookWrites(t, "")
	if _, err := Open(out, ID{}, RecoverySecret(rk.String())); err != nil {
		t.Fatalf("the returned recovery key must open: %v", err)
	}
	if _, err := Open(out, ID{}, RecoverySecret(oldRK.String())); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the replaced key must be refused, got %v", err)
	}
	if err := Heal(out, ID{}, RecoverySecret(rk.String())); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(out); !bytes.Equal(s[0], s[1]) {
		t.Fatal("Heal must bring the mirror up to the committed change")
	}

	out2, _, _ := newArchive(t)
	hookWrites(t, BackupName)
	const newPassword = "the replacement passphrase"
	err = ChangePasswordAt(out2, ID{}, PasswordSecret(testPassword), newPassword)
	if !errors.Is(err, ErrPartiallySaved) {
		t.Fatalf("mirror write failed: want ErrPartiallySaved, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(FileName, "holds the change", BackupName))
	hookWrites(t, "")
	if _, err := Open(out2, ID{}, PasswordSecret(newPassword)); err != nil {
		t.Fatalf("ErrPartiallySaved means the new password is in effect: %v", err)
	}

	out3, _, _ := newArchive(t)
	before := snapshot(out3)
	hookWrites(t, FileName)
	err = ChangePasswordAt(out3, ID{}, PasswordSecret(testPassword), newPassword)
	if err == nil || errors.Is(err, ErrPartiallySaved) {
		t.Fatalf("a failed primary write is a plain failure, nothing in effect, got %v", err)
	}
	if s := snapshot(out3); !bytes.Equal(s[0], before[0]) || !bytes.Equal(s[1], before[1]) {
		t.Fatal("a failed primary write must change nothing")
	}
}
