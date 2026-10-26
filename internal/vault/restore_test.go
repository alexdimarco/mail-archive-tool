package vault

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

const restoredPassword = "the restored passphrase"

// sealedArchive is an archive folder with a key file and a sealed record pointer
// (the frozen "MAPF" header; container's own test pins it against container.Seal),
// and its recovery file and recovery key.
func sealedArchive(t *testing.T) (out string, k *Key, rk RecoveryKey, recoveryFile []byte) {
	t.Helper()
	out, k, rk = newArchive(t)
	writePointer(t, out, k, pointerNames[0], 3)
	v, err := Parse(readFile(t, out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if recoveryFile, err = v.RecoveryFile(); err != nil {
		t.Fatal(err)
	}
	return out, k, rk, recoveryFile
}

func writePointer(t *testing.T, out string, k *Key, name string, gen uint64) {
	t.Helper()
	sealed, err := k.SealPointer(ContainerManifest, gen, []byte("record"))
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(pointerMagic), 0, 0)
	binary.BigEndian.PutUint16(raw[4:6], FormatVersion)
	raw = binary.BigEndian.AppendUint64(raw, gen)
	writeFile(t, out, name, append(raw, sealed...))
}

func dropKeyFiles(t *testing.T, out string) {
	t.Helper()
	removeFile(t, out, FileName)
	removeFile(t, out, BackupName)
}

func fromFile(t *testing.T, file []byte, rk RecoveryKey) RestoreSource {
	t.Helper()
	e, err := ParseRecoveryFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return RecoveryEntrySource{Entry: e, RecoveryKey: rk.String()}
}

// covers: MA-791, R37
// A recovery file holds only the recovery entry; with its recovery key it puts back
// a folder's lost key files, proven against the sealed record, setting a new
// password and a new recovery key at a time-based generation; the old password and
// recovery key no longer open it.
func TestARecoveryFilePutsBackALostKeyFile(t *testing.T) {
	lowCost(t)
	out, k, rk, file := sealedArchive(t)
	if strings.Contains(string(file), "password") {
		t.Fatalf("a recovery file holds a password entry: %s", file)
	}
	dropKeyFiles(t, out)
	before := time.Now().Unix()
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(out, FileName)) {
		t.Fatal("PrepareRestore wrote the key file")
	}
	newRK := p.RecoveryKey()
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out, ID{}, PasswordSecret(restoredPassword))
	if err != nil {
		t.Fatalf("the new password: %v", err)
	}
	assure.Reached(t, o.Key.master == k.master && o.Vault.Generation >= uint64(before), "the same master key at a time-based generation")
	o.Key.Destroy()
	if _, err := Open(out, ID{}, RecoverySecret(newRK.String())); err != nil {
		t.Fatalf("the new recovery key: %v", err)
	}
	if _, err := Open(out, ID{}, PasswordSecret(testPassword)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old password still opens: %v", err)
	}
	if _, err := Open(out, ID{}, RecoverySecret(rk.String())); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old recovery key still opens: %v", err)
	}
	if err := p.Commit(); !errors.Is(err, ErrPendingUsed) {
		t.Fatalf("a second Commit: %v", err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// covers: MA-791, R37
// Each state a restore must not go over is refused, writing nothing: a copy from a
// newer MailArchive, a copy of another archive, a copy that cannot be read, a copy
// that still opens, no sealed record at a folder not recorded as the archive's, a
// record another archive's key sealed, a clock before this build, a wrong recovery
// key, and a recovery file of another archive than the one recorded.
func TestARestoreRefusesWhatItMustNotGoOver(t *testing.T) {
	lowCost(t)
	other, _, _ := newArchive(t)
	_, otherKey, _ := newArchive(t)
	cases := map[string]struct {
		setup func(t *testing.T, out string, k *Key) ID
		src   func(file []byte, rk RecoveryKey) RestoreSource
		want  error
	}{
		"a newer copy": {setup: func(t *testing.T, out string, k *Key) ID {
			writeFile(t, out, FileName, []byte(`{"format": 99}`))
			return ID{}
		}, want: ErrNewerVault},
		"another archive's copy": {setup: func(t *testing.T, out string, k *Key) ID {
			writeFile(t, out, FileName, readFile(t, other, FileName))
			return ID{}
		}, want: ErrTampered},
		"a copy that still opens": {setup: func(t *testing.T, out string, k *Key) ID {
			return ID{}
		}, want: ErrKeyFileOpens},
		"no record, not recorded here": {setup: func(t *testing.T, out string, k *Key) ID {
			removeFile(t, out, pointerNames[0])
			return ID{}
		}, want: ErrNotProven},
		"another archive's record": {setup: func(t *testing.T, out string, k *Key) ID {
			writePointer(t, out, otherKey, pointerNames[0], 3)
			return ID{}
		}, want: ErrNotProven},
		"a record from a newer MailArchive": {setup: func(t *testing.T, out string, k *Key) ID {
			pointerAt(t, out, pointerNames[1], FormatVersion+1)
			return ID{}
		}, want: ErrNewerVault},
		"a record that is not one, even at the recorded path": {setup: func(t *testing.T, out string, k *Key) ID {
			removeFile(t, out, pointerNames[0])
			writeFile(t, out, pointerNames[1], []byte("not a record"))
			return k.ArchiveID()
		}, want: ErrNotProven},
		"another archive's record, even at the recorded path": {setup: func(t *testing.T, out string, k *Key) ID {
			writePointer(t, out, otherKey, pointerNames[0], 3)
			return k.ArchiveID()
		}, want: ErrNotProven},
		"a recovery file of another archive than recorded": {setup: func(t *testing.T, out string, k *Key) ID {
			return otherKey.ArchiveID()
		}, want: ErrTampered},
		"a wrong recovery key": {setup: func(t *testing.T, out string, k *Key) ID { return ID{} },
			src: func(file []byte, rk RecoveryKey) RestoreSource {
				e, _ := ParseRecoveryFile(file)
				var wrong RecoveryKey
				wrong[0] = rk[0] ^ 1
				return RecoveryEntrySource{Entry: e, RecoveryKey: wrong.String()}
			}, want: ErrWrongSecret},
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		cases["a copy that cannot be read"] = struct {
			setup func(t *testing.T, out string, k *Key) ID
			src   func(file []byte, rk RecoveryKey) RestoreSource
			want  error
		}{setup: func(t *testing.T, out string, k *Key) ID {
			writeFile(t, out, FileName, []byte("x"))
			os.Chmod(filepath.Join(out, FileName), 0)
			t.Cleanup(func() { os.Chmod(filepath.Join(out, FileName), 0o600) })
			return ID{}
		}, want: ErrKeyFileUnreadable}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, k, rk, file := sealedArchive(t)
			if name != "a copy that still opens" {
				dropKeyFiles(t, out)
			}
			want := c.setup(t, out, k)
			src := fromFile(t, file, rk)
			if c.src != nil {
				src = c.src(file, rk)
			}
			snap := snapshot(out)
			_, err := PrepareRestore(out, want, src, restoredPassword)
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.NoSideEffect(func() bool {
				now := snapshot(out)
				return string(now[0]) == string(snap[0]) && string(now[1]) == string(snap[1])
			}))
			if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}

	// The clock before this build, and, its positive twin, exactly at it.
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	orig := clock
	defer func() { clock = orig }()
	clock = func() time.Time { return releaseFloor.Add(-time.Second) }
	_, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("clock"), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return s[0] == nil && s[1] == nil
	}))
	if !errors.Is(err, ErrClock) {
		t.Fatalf("want ErrClock, got %v", err)
	}
	clock = func() time.Time { return releaseFloor }
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatalf("the clock at this build's date: %v", err)
	}
	p.Discard()
}

// covers: MA-791, R37
// A folder with no sealed record (an archive whose first backup never ran) is put
// back only when this computer recorded it, proven, as this archive's: want is the
// key's id.
func TestAFolderWithNoRecordIsRestoredOnlyAtItsRecordedPath(t *testing.T) {
	lowCost(t)
	out, k, rk, file := sealedArchive(t)
	removeFile(t, out, pointerNames[0])
	dropKeyFiles(t, out)
	p, err := PrepareRestore(out, k.ArchiveID(), fromFile(t, file, rk), restoredPassword)
	assure.Reached(t, p, "the restore at its recorded path")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-791, R37
// An old key file put back after a restore is outranked, never used — its
// password is refused as superseded — and a heal rewrites it from the restored
// one; a second restore on a clock behind the first still outranks it.
func TestAnOldKeyFilePutBackIsOutranked(t *testing.T) {
	lowCost(t)
	out, _, rk, file := sealedArchive(t)
	old := readFile(t, out, FileName)
	dropKeyFiles(t, out)
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatal(err)
	}
	newRK := p.RecoveryKey()
	newFile, _ := p.RecoveryFile()
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, out, FileName)
	writeFile(t, out, BackupName, old)
	if _, err := Open(out, ID{}, PasswordSecret(testPassword)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old key file's password opened: %v", err)
	}
	o, err := Open(out, ID{}, PasswordSecret(restoredPassword))
	if err != nil {
		t.Fatal(err)
	}
	if !o.NeedsHeal {
		t.Fatal("the old copy beside the restored one is not owed a heal")
	}
	if err := Heal(out, ID{}, KeySecret(o.Key)); err != nil {
		t.Fatal(err)
	}
	o.Key.Destroy()
	assure.Reached(t, string(readFile(t, out, BackupName)) == string(first), "the old copy healed over")

	// A second restore, the clock now behind the first's generation.
	v, _ := Parse(first)
	dropKeyFiles(t, out)
	writeFile(t, out, BackupName, first)
	writeFile(t, out, FileName, []byte("damaged"))
	orig := clock
	clock = func() time.Time { return releaseFloor.Add(time.Hour) }
	defer func() { clock = orig }()
	p2, err := PrepareRestore(out, ID{}, fromFile(t, newFile, newRK), "a second restored passphrase")
	if err == nil {
		t.Fatal("a restore over a copy that still opens with the key went ahead")
	}
	removeFile(t, out, BackupName)
	writeFile(t, out, BackupName, []byte(strings.Replace(string(first), `"mac": "`, `"mac": "00`, 1)))
	p2, err = PrepareRestore(out, ID{}, fromFile(t, newFile, newRK), "a second restored passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.Commit(); err != nil {
		t.Fatal(err)
	}
	second, _ := Parse(readFile(t, out, FileName))
	if second.Generation <= v.Generation {
		t.Fatalf("the second restore's generation %d does not outrank the first's %d", second.Generation, v.Generation)
	}
}

// covers: MA-791, MA-793, R37
// Anything a restore judged that changes between Prepare and Commit makes Commit
// refuse with ErrConflict, writing nothing, and spends the restore — each state
// Prepare refuses, appearing after it, among them (the P3-1b pass, FID-03).
func TestARestoreRefusesAFolderThatChangedSinceItWasPrepared(t *testing.T) {
	lowCost(t)
	other, _, _ := newArchive(t)
	changes := map[string]func(t *testing.T, out string, k *Key, orig []byte){
		"a copy put back":  func(t *testing.T, out string, k *Key, orig []byte) { writeFile(t, out, BackupName, []byte("{}")) },
		"the record moved": func(t *testing.T, out string, k *Key, orig []byte) { writePointer(t, out, k, pointerNames[1], 4) },
		"a newer copy": func(t *testing.T, out string, k *Key, orig []byte) {
			writeFile(t, out, BackupName, []byte(`{"format": 99}`))
		},
		"another archive's copy": func(t *testing.T, out string, k *Key, orig []byte) {
			writeFile(t, out, BackupName, readFile(t, other, FileName))
		},
		"a copy that opens": func(t *testing.T, out string, k *Key, orig []byte) { writeFile(t, out, BackupName, orig) },
		"the record removed": func(t *testing.T, out string, k *Key, orig []byte) {
			removeFile(t, out, pointerNames[0])
		},
		"the record's mirror unreadable": func(t *testing.T, out string, k *Key, orig []byte) {
			if err := os.Mkdir(filepath.Join(out, pointerNames[1]), 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			out, k, rk, file := sealedArchive(t)
			orig := readFile(t, out, FileName)
			dropKeyFiles(t, out)
			p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
			if err != nil {
				t.Fatal(err)
			}
			change(t, out, k, orig)
			err = p.Commit()
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names("changed"),
				assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, FileName)) }))
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("want ErrConflict, got %v", err)
			}
			if err := p.Commit(); !errors.Is(err, ErrPendingUsed) {
				t.Fatalf("a refused restore can still be committed: %v", err)
			}
		})
	}
}

// covers: MA-791, R37
// The key this computer remembers rebuilds a lost key file the same way (K15).
func TestTheRememberedKeyRebuildsALostKeyFile(t *testing.T) {
	lowCost(t)
	out, k, _, _ := sealedArchive(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(store, k); err != nil {
		t.Fatal(err)
	}
	dropKeyFiles(t, out)
	p, err := PrepareRestore(out, ID{}, RememberedSource{Store: store, Claimed: k.ArchiveID()}, restoredPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out, ID{}, PasswordSecret(restoredPassword))
	assure.Reached(t, o, "opened after the rebuild")
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareRestore(t.TempDir(), ID{}, RememberedSource{Store: graphconfig.NewFileSecretStore(t.TempDir()), Claimed: k.ArchiveID()}, restoredPassword)
	if !errors.Is(err, ErrKeyNotRetrievable) {
		t.Fatalf("no remembered key: %v", err)
	}
}

// covers: MA-791
// A recovery file is read strictly: another JSON file, an unknown field, a newer
// format and a password entry are refused.
func TestARecoveryFileIsReadStrictly(t *testing.T) {
	lowCost(t)
	_, _, _, file := sealedArchive(t)
	if _, err := ParseRecoveryFile(file); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	for name, text := range map[string]string{
		"not one":          strings.Replace(string(file), recoveryFileKind, "something-else", 1),
		"an unknown field": strings.Replace(string(file), `"kind"`, `"extra": 1, "kind"`, 1),
		"a password entry": strings.Replace(string(file), `"kind": "recovery"`, `"kind": "password"`, 1),
	} {
		if _, err := ParseRecoveryFile([]byte(text)); !errors.Is(err, ErrRecoveryFile) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseRecoveryFile([]byte(`{"format": 9}`)); !errors.Is(err, ErrNewerVault) {
		t.Fatalf("a newer recovery file: %v", err)
	}
	assure.Reached(t, true, "recovery files judged")
}

// covers: MA-791, MA-777, R37
// OpenWithin refuses, before any derivation and on the bytes it would derive from,
// a copy asking for more than the limit, naming its cost (the P3-1a pass, OUT-04).
func TestOpenWithinRefusesAboveTheLimitBeforeDeriving(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	if o, err := OpenWithin(out, ID{}, PasswordSecret(testPassword), floor); err != nil {
		t.Fatalf("positive twin: %v", err)
	} else {
		o.Key.Destroy()
	}
	runs := countDerivations(t)
	tight := Params{MemoryKiB: floor.MemoryKiB - 1, Time: floor.Time, Threads: floor.Threads}
	_, err := OpenWithin(out, ID{}, PasswordSecret(testPassword), tight)
	var ce *CostError
	if !errors.As(err, &ce) || ce.Params != floor {
		t.Fatalf("want a CostError naming %s, got %v", floor, err)
	}
	assure.Reached(t, len(*runs) == 0, "no derivation above the limit")
}

// covers: MA-791
// A rewrite never takes the generation to its cap.
func TestARewriteRefusesTheGenerationCap(t *testing.T) {
	lowCost(t)
	_, k, _ := newArchive(t)
	v := &Vault{ArchiveID: k.ArchiveID(), Generation: maxGeneration - 1}
	v.Password = wrapAt(t, k, entryPassword, passwordBytes(testPassword), floor)
	v.Recovery = wrapAt(t, k, entryRecovery, []byte("x"), floor)
	v.mac = v.computeMAC(k)
	if _, err := v.WithPassword(k, restoredPassword); err == nil {
		t.Fatal("a password change passed the generation cap")
	}
	if _, _, err := v.WithNewRecovery(k); err == nil {
		t.Fatal("a new recovery key passed the generation cap")
	}
	assure.Reached(t, true, "both rewrites refused")
}
