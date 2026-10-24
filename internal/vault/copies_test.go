package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// countDerivations records the cost of every Argon2id run. A run above the
// floor is answered with junk instead of being computed, so a test can plant a
// ceiling-cost copy without allocating a gigabyte; the junk never unwraps.
func countDerivations(t *testing.T) *[]Params {
	t.Helper()
	var runs []Params
	orig := argon2Key
	argon2Key = func(pw, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
		runs = append(runs, Params{MemoryKiB: memory, Time: time, Threads: threads})
		if memory > floor.MemoryKiB {
			return make([]byte, keyLen)
		}
		return orig(pw, salt, time, memory, threads, keyLen)
	}
	t.Cleanup(func() { argon2Key = orig })
	return &runs
}

// editVault rewrites name's JSON through f.
func editVault(t *testing.T, out, name string, f func(m map[string]any)) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFile(t, out, name), &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, name, b)
}

// covers: MA-715, R37
func TestCopiesFromTwoArchivesAreRefusedNotHealed(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	other, otherKey, _ := newArchive(t)
	if err := ChangePasswordAt(other, PasswordSecret(testPassword), testPassword+" again"); err != nil {
		t.Fatal(err) // the other archive's vault is now at generation 2, above ours
	}
	writeFile(t, out, BackupName, readFile(t, other, FileName))
	before := snapshot(out)
	unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }
	store := graphconfig.NewFileSecretStore(t.TempDir())
	Remember(store, k)
	Remember(store, otherKey)

	for label, try := range map[string]func() error{
		"Open":            func() error { _, err := Open(out, PasswordSecret(testPassword)); return err },
		"Open (other pw)": func() error { _, err := Open(out, PasswordSecret(testPassword+" again")); return err },
		"Heal":            func() error { return Heal(out, PasswordSecret(testPassword)) },
		"ChangePassword":  func() error { return ChangePasswordAt(out, PasswordSecret(testPassword), "yet another passphrase") },
		"Detect":          func() error { _, _, err := Detect(out); return err },
		"gate":            func() error { _, err := RequireRetrievableKey(out, k.ArchiveID(), store); return err },
	} {
		err := try()
		if !errors.Is(err, ErrTampered) {
			t.Fatalf("%s: want ErrTampered, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names(k.ArchiveID().String(), otherKey.ArchiveID().String(), "different archive"),
			assure.NoSideEffect(unchanged))
	}
}

// covers: MA-715, R37
func TestANewerFormatCopyIsNeverFallenBackPastOrHealedOver(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	editVault(t, out, FileName, func(m map[string]any) { m["format"] = VaultFormat + 1 })
	before := snapshot(out)
	unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }
	store := graphconfig.NewFileSecretStore(t.TempDir())
	Remember(store, k)

	for label, try := range map[string]func() error{
		"Open":           func() error { _, err := Open(out, PasswordSecret(testPassword)); return err },
		"Heal":           func() error { return Heal(out, PasswordSecret(testPassword)) },
		"ChangePassword": func() error { return ChangePasswordAt(out, PasswordSecret(testPassword), "yet another passphrase") },
		"Detect":         func() error { _, _, err := Detect(out); return err },
		"gate":           func() error { _, err := RequireRetrievableKey(out, k.ArchiveID(), store); return err },
	} {
		err := try()
		if !errors.Is(err, ErrNewerVault) {
			t.Fatalf("%s: a newer-format copy beside an older one: want ErrNewerVault, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("upgrade"), assure.NoSideEffect(unchanged))
	}
}

// covers: MA-715, R37
func TestAForkIsNotSettledByHealOnlyByADeliberateChange(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	gen1 := readFile(t, out, FileName)
	const pwA, pwB, pwC = "machine A passphrase", "machine B passphrase", "the settled passphrase"
	if err := ChangePasswordAt(out, PasswordSecret(testPassword), pwA); err != nil {
		t.Fatal(err)
	}
	gen2A := readFile(t, out, FileName)
	writeFile(t, out, FileName, gen1)
	writeFile(t, out, BackupName, gen1)
	if err := ChangePasswordAt(out, PasswordSecret(testPassword), pwB); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, FileName, gen2A) // sync left machine A's vault as the primary, B's as the mirror
	before := snapshot(out)

	o, err := Open(out, PasswordSecret(pwB))
	if err != nil {
		t.Fatalf("machine B's password must open its own copy: %v", err)
	}
	if !o.Forked || !o.NeedsHeal || o.Vault.Generation != 2 {
		t.Fatalf("want a reported fork at generation 2, got forked=%v heal=%v gen=%d", o.Forked, o.NeedsHeal, o.Vault.Generation)
	}
	if mirror, _ := Parse(before[1]); o.Vault.mac != mirror.mac {
		t.Fatal("on a tie the vault returned must be the copy the secret opened (B's mirror), not the primary")
	}
	err = Heal(out, PasswordSecret(pwB))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Heal must not choose between two people's changes: want ErrConflict, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("change the password"), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1])
	}))

	if err := ChangePasswordAt(out, PasswordSecret(pwB), pwC); err != nil {
		t.Fatalf("a deliberate change from one side settles the fork: %v", err)
	}
	o, err = Open(out, PasswordSecret(pwC))
	if err != nil || o.Forked || o.NeedsHeal || o.Vault.Generation != 3 {
		t.Fatalf("after the change: want one vault at generation 3, got %v", err)
	}
	if _, err := Open(out, PasswordSecret(pwA)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the other side's password is superseded, got %v", err)
	}
}

// covers: MA-715, R37
func TestThePlantedExpensiveCopyIsNotPaidFor(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	editVault(t, out, BackupName, func(m map[string]any) {
		m["generation"] = 99 // tried first if copies were tried by generation
		for _, e := range m["entries"].([]any) {
			e.(map[string]any)["memory_kib"] = ceiling.MemoryKiB
			e.(map[string]any)["time"] = ceiling.Time
		}
	})
	runs := countDerivations(t)
	o, err := Open(out, PasswordSecret(testPassword))
	if err != nil {
		t.Fatalf("the authentic primary must open: %v", err)
	}
	if o.Vault.Generation != 1 || !o.NeedsHeal {
		t.Fatalf("want the authentic generation 1 with NeedsHeal, got %d / %v", o.Vault.Generation, o.NeedsHeal)
	}
	for _, p := range assure.Reached(t, *runs, "derivations") {
		if p != floor {
			t.Fatalf("a derivation at %s ran: the planted ceiling-cost copy was paid for although the real one opened", p)
		}
	}
}

// covers: MA-715, R37
func TestAMistypedPasswordCostsOneDerivationAndSaysWrongPassword(t *testing.T) {
	lowCost(t)
	out, _, _ := newArchive(t)
	runs := countDerivations(t)
	_, err := Open(out, PasswordSecret("not the password at all"))
	if !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("want ErrWrongSecret, got %v", err)
	}
	if len(*runs) != 1 {
		t.Fatalf("two identical copies cost %d derivations for one typo, want 1", len(*runs))
	}

	editVault(t, out, BackupName, func(m map[string]any) {
		m["generation"] = 99
		m["entries"].([]any)[0].(map[string]any)["time"] = floor.Time - 1 // a planted copy that reads as "weakened"
	})
	_, err = Open(out, PasswordSecret("not the password at all"))
	if !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("a planted copy must not mask a plain wrong password: want ErrWrongSecret, got %v", err)
	}
}
