package vault

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// covers: MA-718, R37
func TestCommitHandsTheKeyOverAndADeadKeyRefusesEveryUse(t *testing.T) {
	lowCost(t)
	out := t.TempDir()
	p, err := PrepareCreate(out, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	p.Discard() // the usual `defer p.Discard()` running after a successful Commit
	sealed, err := k.SealUnit(UnitMessage, "msg-1", []byte("body"))
	if err != nil {
		t.Fatalf("the key Commit handed over must survive a later Discard: %v", err)
	}
	o, err := Open(out, k.ArchiveID(), PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := o.Key.OpenUnit(UnitMessage, "msg-1", sealed); err != nil || string(pt) != "body" {
		t.Fatalf("what the handed-over key sealed must open with the archive's key: %q %v", pt, err)
	}
	if _, err := p.Commit(); !errors.Is(err, ErrPendingUsed) {
		t.Fatalf("a second Commit: want ErrPendingUsed, got %v", err)
	}

	dead := testKey(t, fixedID(), 0x42)
	dead.Destroy()
	var zero Key
	store := graphconfig.NewFileSecretStore(t.TempDir())
	for label, k := range map[string]*Key{"destroyed": dead, "zero": &zero, "nil": nil} {
		uses := map[string]error{}
		_, uses["SealUnit"] = k.SealUnit(UnitMessage, "m", []byte("x"))
		_, uses["OpenUnit"] = k.OpenUnit(UnitMessage, "m", sealed)
		_, uses["SealChunk"] = k.SealChunk(ContainerIndex, 1, 0, []byte("x"))
		_, uses["OpenChunk"] = k.OpenChunk(ContainerIndex, 1, 0, sealed)
		_, uses["SealPointer"] = k.SealPointer(ContainerIndex, 1, []byte("x"))
		_, uses["OpenPointer"] = k.OpenPointer(ContainerIndex, 1, sealed)
		_, uses["UnitName"] = k.UnitName(UnitMessage, "0123456789abcdef0123456789abcdef.1")
		uses["Remember"] = Remember(store, k)
		uses["Verify"] = o.Vault.Verify(k)
		for use, err := range uses {
			if !errors.Is(err, ErrKeyDestroyed) {
				t.Fatalf("%s key, %s: want ErrKeyDestroyed (never an all-zero key), got %v", label, use, err)
			}
		}
	}
}

// covers: MA-714, R37
func TestANewRecoveryKeyCommitIsRetryableUntilItLands(t *testing.T) {
	lowCost(t)
	out, k, oldRK := newArchive(t)
	p, err := PrepareNewRecovery(out, k.ArchiveID(), PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); !errors.Is(err, lockfile.ErrHeld) {
		t.Fatalf("while a backup holds the lock: want lockfile.ErrHeld, got %v", err)
	}
	held.Release()
	if err := p.Commit(); err != nil {
		t.Fatalf("the retry after the lock is free must commit the key already shown: %v", err)
	}
	if _, err := Open(out, k.ArchiveID(), RecoverySecret(p.RecoveryKey().String())); err != nil {
		t.Fatalf("the shown key must open: %v", err)
	}
	if _, err := Open(out, k.ArchiveID(), RecoverySecret(oldRK.String())); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old key must be refused, got %v", err)
	}
	if err := p.Commit(); !errors.Is(err, ErrPendingUsed) {
		t.Fatalf("a Commit after success: want ErrPendingUsed, got %v", err)
	}

	q, err := PrepareNewRecovery(out, k.ArchiveID(), PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	q.Discard()
	if err := q.Commit(); !errors.Is(err, ErrPendingUsed) || errors.Is(err, ErrTampered) {
		t.Fatalf("a Commit after Discard: want ErrPendingUsed (never a false 'altered'), got %v", err)
	}
}

// covers: MA-714, R37
func TestARetriedCreateIsRefusedBeforeAnyNewKeyIsShown(t *testing.T) {
	lowCost(t)
	out, k, firstRK := newArchive(t)
	removeFile(t, out, BackupName) // an earlier attempt finished the primary only
	p, err := PrepareCreate(out, testPassword)
	if !errors.Is(err, ErrExists) || p != nil {
		t.Fatalf("a retry over an earlier attempt's vault must be refused before a key is made: %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(FileName, "password and recovery key you saved then open it"))
	if _, err := Open(out, k.ArchiveID(), RecoverySecret(firstRK.String())); err != nil {
		t.Fatalf("the first attempt's recovery key must open what it left: %v", err)
	}
}

// covers: MA-714, R37
func TestTheProductionVaultWriterIsTheSweepableAtomicWriter(t *testing.T) {
	if reflect.ValueOf(writeCopy).Pointer() != reflect.ValueOf(util.WriteFileAtomic0600Temp).Pointer() {
		t.Fatal("writeCopy must be util.WriteFileAtomic0600Temp: the writer that honours the sweepable temp name (A8)")
	}
}

// covers: MA-718, R37
func TestKeysSecretsAndPendingChangesPrintRedacted(t *testing.T) {
	lowCost(t)
	out := t.TempDir()
	const pw = "hunter2-hunter2-hunter2"
	pc, err := PrepareCreate(out, pw)
	if err != nil {
		t.Fatal(err)
	}
	k, err := pc.Commit()
	if err != nil {
		t.Fatal(err)
	}
	pr, err := PrepareNewRecovery(out, k.ArchiveID(), PasswordSecret(pw))
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Discard()
	secrets := []string{
		hex.EncodeToString(k.master[:]), hex.EncodeToString(k.data[:]), hex.EncodeToString(k.names[:]), pw,
		pc.RecoveryKey().String(), pr.RecoveryKey().String(),
		strings.ReplaceAll(pc.RecoveryKey().String(), "-", ""),
	}
	for label, v := range map[string]any{
		"*Key": k, "password Secret": PasswordSecret(pw), "recovery Secret": RecoverySecret(pc.RecoveryKey().String()),
		"key Secret": KeySecret(k), "*PendingCreate": pc, "*PendingRecovery": pr,
	} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(verb, v)
			assure.Reached(t, got, label+" printed")
			for _, s := range secrets {
				if strings.Contains(strings.ToLower(got), strings.ToLower(s)) {
					t.Fatalf("%s printed with %s leaks a secret: %q", label, verb, got)
				}
			}
			if !strings.Contains(got, "redacted") {
				t.Fatalf("%s printed with %s must say it is redacted: %q", label, verb, got)
			}
		}
	}
}
