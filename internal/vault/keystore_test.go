package vault

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-711, R37
func TestARememberedKeyOpensTheArchiveUnattended(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(store, k); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get("archive:" + k.ArchiveID().String())
	if err != nil {
		t.Fatalf("the key must be filed under archive:<id>: %v", err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(stored); !bytes.Equal(raw, k.master[:]) {
		t.Fatal("the remembered secret must be the master key (A10)")
	}

	sealed, err := k.SealUnit(UnitMessage, "msg-1", []byte("archived"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := RequireRetrievableKey(out, k.ArchiveID(), store)
	if err != nil {
		t.Fatalf("the remembered key must open the archive: %v", err)
	}
	if got.NeedsHeal || got.Forked {
		t.Fatalf("a healthy archive opened by the gate must need no heal, got heal=%v fork=%v", got.NeedsHeal, got.Forked)
	}
	if pt, err := got.Key.OpenUnit(UnitMessage, "msg-1", sealed); err != nil || string(pt) != "archived" {
		t.Fatalf("the retrieved key must open the archive's units: %q %v", pt, err)
	}

	if err := ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), "the replacement passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireRetrievableKey(out, k.ArchiveID(), store); err != nil {
		t.Fatalf("a password change must leave the remembered key working (A10): %v", err)
	}
}

// covers: MA-711, R37
func TestRequireRetrievableKeyFailsClosed(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	_, foreign, _ := newArchive(t)
	before := snapshot(out)
	unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }

	healthy := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(healthy, k); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireRetrievableKey(out, k.ArchiveID(), healthy); err != nil {
		t.Fatalf("positive twin: a correct remembered key passes the gate: %v", err)
	}

	account := Account(k.ArchiveID())
	for label, prepare := range map[string]func(s graphconfig.SecretStore){
		"nothing stored":        func(graphconfig.SecretStore) {},
		"a damaged entry":       func(s graphconfig.SecretStore) { s.Set(account, "not base64 !!") },
		"a short entry":         func(s graphconfig.SecretStore) { s.Set(account, base64.StdEncoding.EncodeToString([]byte("short"))) },
		"another archive's key": func(s graphconfig.SecretStore) { s.Set(account, base64.StdEncoding.EncodeToString(foreign.master[:])) },
		"forgotten":             func(s graphconfig.SecretStore) { Remember(s, k); Forget(s, k.ArchiveID()) },
	} {
		store := graphconfig.NewFileSecretStore(t.TempDir())
		prepare(store)
		key, err := RequireRetrievableKey(out, k.ArchiveID(), store)
		if !errors.Is(err, ErrKeyNotRetrievable) {
			t.Fatalf("%s: want ErrKeyNotRetrievable, got %v", label, err)
		}
		if key != nil {
			t.Fatalf("%s: a refused gate returned a key", label)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names(out, k.ArchiveID().String(), "password or recovery key"),
			assure.NoSideEffect(unchanged))
	}

	if _, err := RequireRetrievableKey(out, ID{}, healthy); !errors.Is(err, ErrKeyNotRetrievable) {
		t.Fatalf("no recorded id: want ErrKeyNotRetrievable, got %v", err)
	}
}

// covers: MA-717, R37
func TestAnArchiveRecordedAsEncryptedNeverReadsAsPlaintext(t *testing.T) {
	lowCost(t)
	foreignDir, foreign, _ := newArchive(t) // its key is remembered too, so only the id check can refuse it
	for label, attack := range map[string]func(out string){
		"both vault files deleted": func(out string) {
			os.Remove(filepath.Join(out, FileName))
			os.Remove(filepath.Join(out, BackupName))
		},
		"both copies replaced by another archive's vault": func(out string) {
			writeFile(t, out, FileName, readFile(t, foreignDir, FileName))
			writeFile(t, out, BackupName, readFile(t, foreignDir, BackupName))
		},
		"both copies damaged": func(out string) {
			writeFile(t, out, FileName, []byte("x"))
			writeFile(t, out, BackupName, []byte("y"))
		},
	} {
		out, k, _ := newArchive(t)
		store := graphconfig.NewFileSecretStore(t.TempDir())
		if err := Remember(store, k); err != nil {
			t.Fatal(err)
		}
		if err := Remember(store, foreign); err != nil {
			t.Fatal(err)
		}
		if _, err := RequireRetrievableKey(out, k.ArchiveID(), store); err != nil {
			t.Fatalf("%s: positive twin: the intact archive passes: %v", label, err)
		}
		attack(out)
		after := snapshot(out)
		key, err := RequireRetrievableKey(out, k.ArchiveID(), store)
		if !errors.Is(err, ErrKeyNotRetrievable) {
			t.Fatalf("%s: want ErrKeyNotRetrievable (never 'not encrypted'), got %v", label, err)
		}
		if errors.Is(err, ErrNoVault) {
			t.Fatalf("%s: the gate's refusal must never match ErrNoVault, the 'maybe plaintext' signal (A15)", label)
		}
		if key != nil {
			t.Fatalf("%s: the gate returned a key", label)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, k.ArchiveID().String()), assure.NoSideEffect(func() bool {
			s := snapshot(out)
			return bytes.Equal(s[0], after[0]) && bytes.Equal(s[1], after[1])
		}))
	}
}

// covers: MA-711, MA-717, R37
func TestTheGateNamesTheFixForEachCause(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(store, k); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		prepare func(t *testing.T) string // returns the folder to gate
		want    ID
		cause   error
		fix     string
	}{
		"drive unplugged": {func(t *testing.T) string { return filepath.Join(t.TempDir(), "unmounted") }, k.ArchiveID(), os.ErrNotExist, "connect the drive"},
		"vault deleted": {func(t *testing.T) string {
			o, _, _ := newArchive(t)
			removeFile(t, o, FileName)
			removeFile(t, o, BackupName)
			return o
		}, k.ArchiveID(), ErrVaultMissing, "restore the archive's key files"},
		"newer MailArchive": {func(t *testing.T) string {
			o, kk, _ := newArchive(t)
			Remember(store, kk)
			editVault(t, o, FileName, func(m map[string]any) { m["format"] = VaultFormat + 1 })
			return o
		}, ID{}, ErrNewerVault, "upgrade MailArchive"},
		"no key stored": {func(t *testing.T) string {
			o, _, _ := newArchive(t)
			return o
		}, ID{}, os.ErrNotExist, "remember its key again"},
		"no recorded id": {func(t *testing.T) string { return out }, ID{}, nil, "no archive id was recorded"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			dir := c.prepare(t)
			want := c.want
			if want == (ID{}) && label != "no recorded id" {
				info, ok, _ := Detect(dir)
				if !ok {
					// a newer vault reads as an error in Detect; take the id from the other copy
					v, err := Parse(readFile(t, dir, BackupName))
					if err != nil {
						t.Fatal(err)
					}
					info.ArchiveID = v.ArchiveID
				}
				want = info.ArchiveID
			}
			_, err := RequireRetrievableKey(dir, want, store)
			if !errors.Is(err, ErrKeyNotRetrievable) {
				t.Fatalf("want ErrKeyNotRetrievable, got %v", err)
			}
			if c.cause != nil && !errors.Is(err, c.cause) {
				t.Fatalf("the refusal must carry its cause %v: %v", c.cause, err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(c.fix))
		})
	}
}

// covers: MA-711, R37
func TestAnUnattendedRunSeesAndHealsADamagedCopy(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(store, k); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, BackupName, []byte("damaged by a sync conflict"))
	o, err := RequireRetrievableKey(out, k.ArchiveID(), store)
	if err != nil {
		t.Fatal(err)
	}
	if !o.NeedsHeal {
		t.Fatal("the gate must report the damaged copy so an unattended run can heal it")
	}
	if err := Heal(out, k.ArchiveID(), KeySecret(o.Key)); err != nil {
		t.Fatalf("an unattended run heals with the remembered key: %v", err)
	}
	if s := snapshot(out); !bytes.Equal(s[0], s[1]) {
		t.Fatal("after the heal both copies must be identical")
	}
}

// covers: MA-725, R37
func TestVerifyKeyProvesAKeyBelongsToTheFolderAndHandsOutNothing(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	_, foreign, _ := newArchive(t)
	before := snapshot(out)
	if err := VerifyKey(out, k.ArchiveID(), k); err != nil {
		t.Fatalf("positive twin: the archive's own key verifies: %v", err)
	}
	for label, c := range map[string]struct {
		dir  string
		want ID
		key  *Key
		err  error
	}{
		"another archive's key":  {out, k.ArchiveID(), foreign, ErrTampered},
		"a recorded id mismatch": {out, foreign.ArchiveID(), k, ErrTampered},
		"a folder with no vault": {t.TempDir(), k.ArchiveID(), k, ErrNoVault},
		"a destroyed key":        {out, k.ArchiveID(), func() *Key { d := testKey(t, k.ArchiveID(), 1); d.Destroy(); return d }(), ErrKeyDestroyed},
	} {
		err := VerifyKey(c.dir, c.want, c.key)
		if err == nil {
			t.Fatalf("%s: VerifyKey must refuse", label)
		}
		if label != "another archive's key" && !errors.Is(err, c.err) {
			t.Fatalf("%s: want %v, got %v", label, c.err, err)
		}
		if label == "another archive's key" && !errors.Is(err, ErrWrongSecret) && !errors.Is(err, ErrTampered) {
			t.Fatalf("%s: want a wrong-key refusal, got %v", label, err)
		}
	}
	if s := snapshot(out); !bytes.Equal(s[0], before[0]) || !bytes.Equal(s[1], before[1]) {
		t.Fatal("VerifyKey must write nothing")
	}
}

// covers: MA-709, MA-711, R37
func TestReadersNeitherLockNorWrite(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	if err := Remember(store, k); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, FileName, []byte("damaged")) // so Open reports NeedsHeal
	before := snapshot(out)
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	o, err := Open(out, ID{}, PasswordSecret(testPassword))
	if err != nil {
		t.Fatalf("Open must not need the archive lock: %v", err)
	}
	if !o.NeedsHeal {
		t.Fatal("fixture: Open must report NeedsHeal for the damaged primary")
	}
	if _, err := RequireRetrievableKey(out, k.ArchiveID(), store); err != nil {
		t.Fatalf("the gate must not need the archive lock: %v", err)
	}
	if _, _, err := Detect(out); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	s := snapshot(out)
	if !bytes.Equal(s[0], before[0]) || !bytes.Equal(s[1], before[1]) {
		t.Fatal("a reader wrote to the vault: Open, Detect and the gate must write nothing, even when a heal is needed")
	}
}
