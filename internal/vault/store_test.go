package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-709, R37
func TestCreateAtWritesTwoIdenticalPrivateCopies(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	primary, mirror := readFile(t, out, FileName), readFile(t, out, BackupName)
	if !bytes.Equal(primary, mirror) {
		t.Fatal("the mirror must be byte-identical to the primary")
	}
	v, err := Parse(primary)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(k); err != nil {
		t.Fatalf("the written vault must authenticate under the returned key: %v", err)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{FileName, BackupName} {
			fi, err := os.Stat(filepath.Join(out, name))
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("%s mode %04o, want 0600", name, fi.Mode().Perm())
			}
		}
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range assure.Reached(t, entries, "archive folder entries") {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a temp file survived: %s", e.Name())
		}
	}
}

// covers: MA-709, R37
func TestVaultTempNameIsOneTheExporterSweeps(t *testing.T) {
	out := t.TempDir()
	f, err := os.CreateTemp(out, tempPattern)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(f.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	if n := export.SweepOrphans(out, time.Now(), nil); n != 1 {
		t.Fatalf("SweepOrphans removed %d files, want the 1 crash-leftover vault temp", n)
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatalf("the leftover vault temp %s was not reaped", f.Name())
	}
}

// covers: MA-709, R37
func TestCrashMidPasswordChangeNeverRollsBack(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	gen1 := readFile(t, out, BackupName)
	const newPassword = "the replacement passphrase"
	if err := ChangePasswordAt(out, PasswordSecret(testPassword), newPassword); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, BackupName, gen1) // the crash: primary at gen 2, mirror still gen 1

	o, err := Open(out, PasswordSecret(newPassword))
	if err != nil {
		t.Fatalf("the new password must open after the crash: %v", err)
	}
	if o.Vault.Generation != 2 || !o.NeedsHeal {
		t.Fatalf("want generation 2 with NeedsHeal, got %d / %v", o.Vault.Generation, o.NeedsHeal)
	}
	_, err = Open(out, PasswordSecret(testPassword))
	if !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the superseded password must be refused, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("replaced by a newer one"))
	if err := Heal(out, PasswordSecret(testPassword)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the superseded password must not be able to heal (roll back), got %v", err)
	}
	if !bytes.Equal(readFile(t, out, BackupName), gen1) {
		t.Fatal("a refused heal must write nothing")
	}

	if err := Heal(out, PasswordSecret(newPassword)); err != nil {
		t.Fatal(err)
	}
	s := snapshot(out)
	if !bytes.Equal(s[0], s[1]) {
		t.Fatal("after Heal both copies must be identical")
	}
	if o, err := Open(out, PasswordSecret(newPassword)); err != nil || o.NeedsHeal || o.Vault.Generation != 2 {
		t.Fatalf("after Heal: want generation 2 and no heal needed, got %v", err)
	}
}

// covers: MA-709, R37
func TestDamagedOrMissingPrimaryFallsBackToTheMirror(t *testing.T) {
	lowCost(t)
	for label, damage := range map[string]func(out string){
		"garbage primary": func(out string) { writeFile(t, out, FileName, []byte("\x00garbage")) },
		"missing primary": func(out string) { os.Remove(filepath.Join(out, FileName)) },
		"empty primary":   func(out string) { writeFile(t, out, FileName, nil) },
	} {
		out, k, _ := newArchive(t)
		good := readFile(t, out, BackupName)
		damage(out)
		info, ok, err := Detect(out)
		if err != nil || !ok || info.ArchiveID != k.ArchiveID() {
			t.Fatalf("%s: Detect must still recognise the archive from the mirror: %v %v", label, ok, err)
		}
		o, err := Open(out, PasswordSecret(testPassword))
		if err != nil {
			t.Fatalf("%s: must open from the mirror: %v", label, err)
		}
		if !o.NeedsHeal {
			t.Fatalf("%s: must report NeedsHeal", label)
		}
		if err := Heal(out, PasswordSecret(testPassword)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(readFile(t, out, FileName), good) {
			t.Fatalf("%s: Heal must restore the primary from the trusted copy", label)
		}
	}
}

// covers: MA-709, R37
func TestAHandRaisedGenerationIsNotTrusted(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	var m map[string]any
	if err := json.Unmarshal(readFile(t, out, BackupName), &m); err != nil {
		t.Fatal(err)
	}
	m["generation"] = 99
	forged, _ := json.Marshal(m)
	writeFile(t, out, BackupName, forged)

	info, ok, err := Detect(out)
	if err != nil || !ok {
		t.Fatalf("Detect: %v %v", ok, err)
	}
	if info.Generation != 99 {
		t.Fatalf("Detect reports what is on disk without authenticating (got %d) — the point is it trusts nothing", info.Generation)
	}
	o, err := Open(out, PasswordSecret(testPassword))
	if err != nil {
		t.Fatalf("the authentic primary must still open: %v", err)
	}
	if o.Vault.Generation != 1 || !o.NeedsHeal {
		t.Fatalf("want the authentic generation 1 with NeedsHeal, got %d / %v", o.Vault.Generation, o.NeedsHeal)
	}
	if err := Heal(out, PasswordSecret(testPassword)); err != nil {
		t.Fatal(err)
	}
	if v, err := Parse(readFile(t, out, BackupName)); err != nil || v.Generation != 1 {
		t.Fatalf("Heal must replace the forged mirror with the authentic generation 1: %v", err)
	}
}

// covers: MA-709, R37
func TestNoVaultAndUnreadableVaults(t *testing.T) {
	lowCost(t)
	empty := t.TempDir()
	if _, ok, err := Detect(empty); ok || err != nil {
		t.Fatalf("a plain folder: want not-encrypted and no error, got %v %v", ok, err)
	}
	_, err := Open(empty, PasswordSecret(testPassword))
	if !errors.Is(err, ErrNoVault) {
		t.Fatalf("Open of a plain folder: want ErrNoVault, got %v", err)
	}

	both := t.TempDir()
	writeFile(t, both, FileName, []byte("x"))
	writeFile(t, both, BackupName, []byte("y"))
	_, _, err = Detect(both)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("two damaged copies: want ErrCorrupt, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(FileName))

	big := t.TempDir()
	writeFile(t, big, FileName, bytes.Repeat([]byte(" "), maxVaultBytes+1))
	if _, _, err := Detect(big); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("an oversize vault: want ErrCorrupt, got %v", err)
	}

	if runtime.GOOS != "windows" {
		out, _, _ := newArchive(t)
		real := filepath.Join(t.TempDir(), "elsewhere.json")
		if err := os.Rename(filepath.Join(out, FileName), real); err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(out, BackupName))
		if err := os.Symlink(real, filepath.Join(out, FileName)); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := Detect(out); ok || err == nil {
			t.Fatalf("a symlinked vault must be refused, not followed: %v %v", ok, err)
		}
	}
}

// covers: MA-708, MA-709, R37
func TestAForgottenPasswordIsResetWithTheRecoveryKey(t *testing.T) {
	lowCost(t)
	out, _, rk := newArchive(t)
	if _, err := Open(out, RecoverySecret(rk.String())); err != nil {
		t.Fatalf("the recovery key shown at create must open the archive: %v", err)
	}
	const newPassword = "remembered this time, honestly"
	if err := ChangePasswordAt(out, RecoverySecret(rk.String()), newPassword); err != nil {
		t.Fatalf("a reset with the recovery key: %v", err)
	}
	if _, err := Open(out, PasswordSecret(newPassword)); err != nil {
		t.Fatalf("the reset password must open: %v", err)
	}
	if _, err := Open(out, PasswordSecret(testPassword)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the forgotten password must be refused, got %v", err)
	}
	if _, err := Open(out, RecoverySecret(rk.String())); err != nil {
		t.Fatalf("a password reset leaves the recovery key working: %v", err)
	}
	if s := snapshot(out); !bytes.Equal(s[0], s[1]) {
		t.Fatal("the reset must rewrite both copies identically")
	}
}

// covers: MA-709, R37
func TestHealOnAHealthyArchiveWritesNothing(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	before := snapshot(out)
	fi, err := os.Stat(filepath.Join(out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	old := fi.ModTime().Add(-time.Hour)
	for _, name := range []string{FileName, BackupName} {
		if err := os.Chtimes(filepath.Join(out, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	w := hookWrites(t, "")
	if err := Heal(out, PasswordSecret(testPassword)); err != nil {
		t.Fatal(err)
	}
	if len(w.names) != 0 {
		t.Fatalf("Heal on two good copies wrote %v", w.names)
	}
	for _, name := range []string{FileName, BackupName} {
		fi, err := os.Stat(filepath.Join(out, name))
		if err != nil || !fi.ModTime().Equal(old) {
			t.Fatalf("%s was touched by a no-op heal", name)
		}
	}
	if s := snapshot(out); !bytes.Equal(s[0], before[0]) || !bytes.Equal(s[1], before[1]) {
		t.Fatal("a no-op heal changed the vault")
	}
}

// covers: MA-710, R37
func TestALockLostMidChangeWritesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to remove a lock file another handle holds open; the lost-lock case cannot arise there")
	}
	lowCost(t)
	out, _, _ := newArchive(t)
	before := snapshot(out)
	lockPath := filepath.Join(out, lockfile.Name)
	var intruder *lockfile.Lock
	orig := argon2Key
	argon2Key = func(pw, salt []byte, tm, mem uint32, th uint8, n uint32) []byte {
		if intruder == nil { // mid-change: a sync client deletes the lock, another run takes a fresh one
			os.Remove(lockPath)
			l, err := lockfile.AcquireAs(lockPath, "export")
			if err != nil {
				t.Errorf("fixture: the intruder could not take the fresh lock: %v", err)
			}
			intruder = l
		}
		return orig(pw, salt, tm, mem, th, n)
	}
	t.Cleanup(func() {
		argon2Key = orig
		intruder.Release()
	})

	err := ChangePasswordAt(out, PasswordSecret(testPassword), "a fresh long passphrase")
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("nothing was written", lockPath), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1])
	}))
}

// covers: MA-709, R37
func TestAMissingPrimaryBesideADamagedMirrorNamesTheMirror(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	os.Remove(filepath.Join(out, FileName))
	writeFile(t, out, BackupName, []byte("{not a vault"))
	_, _, err := Detect(out)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(BackupName, "damaged"))
}

// covers: MA-710, R37
func TestVaultRewritesWaitForTheArchiveLock(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	lockPath := filepath.Join(out, lockfile.Name)
	held, err := lockfile.AcquireAs(lockPath, "export")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(out)
	unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }

	for label, run := range map[string]func() error{
		"change password":  func() error { return ChangePasswordAt(out, PasswordSecret(testPassword), "a fresh long passphrase") },
		"new recovery key": func() error { _, err := RegenerateRecoveryAt(out, PasswordSecret(testPassword)); return err },
		"heal":             func() error { return Heal(out, PasswordSecret(testPassword)) },
	} {
		err := run()
		if !errors.Is(err, lockfile.ErrHeld) {
			t.Fatalf("%s while an export holds the lock: want lockfile.ErrHeld, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(lockPath), assure.NoSideEffect(unchanged))
	}

	fresh := t.TempDir()
	heldFresh, err := lockfile.AcquireAs(filepath.Join(fresh, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = CreateAt(fresh, testPassword)
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(lockfile.Name), assure.NoSideEffect(func() bool {
		s := snapshot(fresh)
		return s[0] == nil && s[1] == nil
	}))
	heldFresh.Release()

	held.Release()
	if err := ChangePasswordAt(out, PasswordSecret(testPassword), "a fresh long passphrase"); err != nil {
		t.Fatalf("positive twin: once the lock is free the change goes through: %v", err)
	}
}

// covers: MA-710, R37
func TestCreateAtRefusesAnExistingVaultOrAbsentFolder(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	before := snapshot(out)
	_, _, err := CreateAt(out, testPassword)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(FileName), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1])
	}))

	onlyMirror := t.TempDir()
	writeFile(t, onlyMirror, BackupName, before[1])
	if _, _, err := CreateAt(onlyMirror, testPassword); !errors.Is(err, ErrExists) {
		t.Fatalf("a lone mirror is a vault fragment: want ErrExists, got %v", err)
	}

	absent := filepath.Join(t.TempDir(), "unmounted-drive")
	_, _, err = CreateAt(absent, testPassword)
	rc, msg = refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(absent, "not present"), assure.NoSideEffect(func() bool {
		_, statErr := os.Stat(absent)
		return os.IsNotExist(statErr)
	}))
}
