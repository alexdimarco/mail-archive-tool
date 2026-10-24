package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-707, R37
func TestPasswordAndRecoveryKeyBothOpenTheSameArchive(t *testing.T) {
	lowCost(t)
	v, k, rk, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if v.Generation != 1 {
		t.Fatalf("a new vault is generation %d, want 1", v.Generation)
	}
	byPassword, err := v.Unlock(testPassword)
	if err != nil {
		t.Fatalf("the password must open: %v", err)
	}
	byRecovery, err := v.UnlockRecovery(rk.String())
	if err != nil {
		t.Fatalf("the recovery key must open: %v", err)
	}
	if byPassword.master != k.master || byRecovery.master != k.master {
		t.Fatal("both secrets must unlock the same master key")
	}
	if v.Password.Salt == v.Recovery.Salt || bytes.Equal(v.Password.Wrapped, v.Recovery.Wrapped) {
		t.Fatal("the two wraps must have independent salts and ciphertexts")
	}
	v2, _, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if v2.ArchiveID == v.ArchiveID {
		t.Fatal("two archives drew the same id")
	}
	if _, err := v2.Unlock(testPassword); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-707, R37
func TestWrongSecretsAreRefused(t *testing.T) {
	lowCost(t)
	v, _, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	other, err := newRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	for label, try := range map[string]func() (*Key, error){
		"wrong password":             func() (*Key, error) { return v.Unlock(testPassword + "!") },
		"well-formed wrong recovery": func() (*Key, error) { return v.UnlockRecovery(other.String()) },
		"the password as recovery":   func() (*Key, error) { return v.UnlockRecovery(testPassword) },
	} {
		k, err := try()
		if k != nil {
			t.Fatalf("%s: returned a key", label)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1))
		if label != "the password as recovery" && !errors.Is(err, ErrWrongSecret) {
			t.Fatalf("%s: want ErrWrongSecret, got %v", label, err)
		}
	}
}

// covers: MA-707, R37
func TestHeaderMACRefusesAnAlteredVaultEvenWithTheRightPassword(t *testing.T) {
	lowCost(t)
	v, k, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(k); err != nil {
		t.Fatalf("positive twin: the untouched vault verifies: %v", err)
	}
	older := *v
	newer, _, err := v.WithNewRecovery(k) // newer.Recovery is a valid entry of a different generation
	if err != nil {
		t.Fatal(err)
	}
	alter := map[string]func(*Vault){
		"generation raised":            func(x *Vault) { x.Generation++ },
		"older recovery entry swapped": func(x *Vault) { x.Recovery = older.Recovery },
		"other entry's cost changed":   func(x *Vault) { x.Recovery.Params.Time++ },
		"MAC changed":                  func(x *Vault) { x.mac[0] ^= 1 },
	}
	for label, change := range alter {
		bad := *newer
		change(&bad)
		got, err := bad.Unlock(testPassword)
		if !errors.Is(err, ErrTampered) {
			t.Fatalf("%s: want ErrTampered, got %v", label, err)
		}
		if got != nil {
			t.Fatalf("%s: an altered vault handed out a key", label)
		}
	}
	// A swapped archive id cannot even unwrap: the wrap's associated data names it.
	swapped := *v
	swapped.ArchiveID[0] ^= 1
	if _, err := swapped.Unlock(testPassword); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("swapped archive id: the wrap AAD must refuse it at unwrap (ErrWrongSecret), got %v", err)
	}

	// Every field of either entry is under the MAC: alter the entry NOT used to
	// unlock, then unlock with the other secret.
	v2, _, rk2, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	otherEntry := map[string]struct {
		change func(*Vault)
		unlock func(*Vault) (*Key, error)
	}{
		"password salt (unlock by recovery)":    {func(x *Vault) { x.Password.Salt[0] ^= 1 }, func(x *Vault) (*Key, error) { return x.UnlockRecovery(rk2.String()) }},
		"password wrapped (unlock by recovery)": {func(x *Vault) { x.Password.Wrapped = flip(x.Password.Wrapped) }, func(x *Vault) (*Key, error) { return x.UnlockRecovery(rk2.String()) }},
		"password memory (unlock by recovery)":  {func(x *Vault) { x.Password.Params.MemoryKiB *= 2 }, func(x *Vault) (*Key, error) { return x.UnlockRecovery(rk2.String()) }},
		"password threads (unlock by recovery)": {func(x *Vault) { x.Password.Params.Threads++ }, func(x *Vault) (*Key, error) { return x.UnlockRecovery(rk2.String()) }},
		"recovery memory (unlock by password)":  {func(x *Vault) { x.Recovery.Params.MemoryKiB *= 2 }, func(x *Vault) (*Key, error) { return x.Unlock(testPassword) }},
		"recovery threads (unlock by password)": {func(x *Vault) { x.Recovery.Params.Threads++ }, func(x *Vault) (*Key, error) { return x.Unlock(testPassword) }},
		"recovery salt (unlock by password)":    {func(x *Vault) { x.Recovery.Salt[0] ^= 1 }, func(x *Vault) (*Key, error) { return x.Unlock(testPassword) }},
	}
	for label, c := range otherEntry {
		bad := *v2
		bad.Password.Wrapped = bytes.Clone(v2.Password.Wrapped)
		c.change(&bad)
		if k, err := c.unlock(&bad); !errors.Is(err, ErrTampered) || k != nil {
			t.Fatalf("%s: want ErrTampered, got %v", label, err)
		}
	}
}

// covers: MA-707, R37
func TestTheArchiveIDIsUnderTheHeaderMAC(t *testing.T) {
	lowCost(t)
	v, k, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	moved := *v
	moved.ArchiveID[15] ^= 1
	if v.computeMAC(k) == moved.computeMAC(k) {
		t.Fatal("changing only the archive id must change the header MAC")
	}
	zero := bytes.ReplaceAll(mustMarshal(t, v), []byte(v.ArchiveID.String()), []byte(strings.Repeat("0", 32)))
	if _, err := Parse(zero); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("an all-zero archive id: want ErrCorrupt, got %v", err)
	}
}

func mustMarshal(t *testing.T, v *Vault) []byte {
	t.Helper()
	b, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func flip(b []byte) []byte {
	c := bytes.Clone(b)
	c[len(c)-1] ^= 1
	return c
}

// covers: MA-703, MA-704, R37
func TestKeyWrapsDrawFreshNoncesAndBindTheirPlace(t *testing.T) {
	lowCost(t)
	rec := recordDraws(t)
	v, _, rk, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	drew := func(b []byte) bool {
		for _, d := range rec.draws {
			if len(d) == nonceSize && bytes.Equal(d, b) {
				return true
			}
		}
		return false
	}
	if !drew(v.Password.Wrapped[:nonceSize]) || !drew(v.Recovery.Wrapped[:nonceSize]) {
		t.Fatal("each wrap's nonce must be its own 24-byte draw from the random source")
	}
	if bytes.Equal(v.Password.Wrapped[:nonceSize], v.Recovery.Wrapped[:nonceSize]) {
		t.Fatal("the two wraps share a nonce")
	}

	kek, err := deriveKEK(passwordBytes(testPassword), v.Password.Salt[:], v.Password.Params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(kek, wrapAAD(v.ArchiveID, entryPassword), v.Password.Wrapped); err != nil {
		t.Fatalf("the password wrap must open under the exact MAW1 associated data: %v", err)
	}
	for label, aad := range map[string][]byte{
		"recovery kind": wrapAAD(v.ArchiveID, entryRecovery),
		"no AAD":        nil,
		"other archive": wrapAAD(ID{1}, entryPassword),
	} {
		if _, err := open(kek, aad, v.Password.Wrapped); !errors.Is(err, ErrAuth) {
			t.Fatalf("the password wrap opened under %s associated data", label)
		}
	}
	rkek, err := deriveKEK(rk[:], v.Recovery.Salt[:], v.Recovery.Params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(rkek, wrapAAD(v.ArchiveID, entryRecovery), v.Recovery.Wrapped); err != nil {
		t.Fatalf("the recovery wrap must open under its MAW1 associated data: %v", err)
	}
}

// covers: MA-707, R37
func TestParseIsStrict(t *testing.T) {
	lowCost(t)
	v, _, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	good, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if back, err := Parse(good); err != nil || back.ArchiveID != v.ArchiveID || back.Generation != v.Generation {
		t.Fatalf("positive twin: a vault's own bytes parse back: %v", err)
	}
	if _, err := Parse(good); err != nil {
		t.Fatal(err)
	}
	edit := func(f func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(good, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	entries := func(m map[string]any) []any { return m["entries"].([]any) }
	corrupt := map[string][]byte{
		"unknown field":     edit(func(m map[string]any) { m["note"] = "hi" }),
		"recovery stripped": edit(func(m map[string]any) { m["entries"] = entries(m)[:1] }),
		"three entries":     edit(func(m map[string]any) { m["entries"] = append(entries(m), entries(m)[0]) }),
		"duplicate kind":    edit(func(m map[string]any) { e := entries(m); m["entries"] = []any{e[0], e[0]} }),
		"short salt":        edit(func(m map[string]any) { entries(m)[0].(map[string]any)["salt"] = "AAAA" }),
		"short wrapped key": edit(func(m map[string]any) { entries(m)[1].(map[string]any)["wrapped"] = "AAAA" }),
		"unknown kdf":       edit(func(m map[string]any) { entries(m)[0].(map[string]any)["kdf"] = "pbkdf2" }),
		"zero generation":   edit(func(m map[string]any) { m["generation"] = 0 }),
		"bad archive id":    edit(func(m map[string]any) { m["archive_id"] = "xyz" }),
		"format zero":       edit(func(m map[string]any) { m["format"] = 0 }),
		"format missing":    edit(func(m map[string]any) { delete(m, "format") }),
		"MAC not hex":       edit(func(m map[string]any) { m["mac"] = "zz" }),
		"MAC short":         edit(func(m map[string]any) { m["mac"] = "abcd" }),
		"unknown entry kind": edit(func(m map[string]any) {
			entries(m)[1].(map[string]any)["kind"] = "escrow"
		}),
		"trailing data": append(bytes.Clone(good), []byte(`{"format":1}`)...),
		"not json":      []byte("not a vault"),
	}
	for label, b := range corrupt {
		_, err := Parse(b)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: want ErrCorrupt, got %v", label, err)
		}
	}
	for label, newerFmt := range map[string][]byte{
		"with a new field":    edit(func(m map[string]any) { m["format"] = VaultFormat + 1; m["future"] = true }),
		"with the same shape": edit(func(m map[string]any) { m["format"] = VaultFormat + 1 }),
	} {
		_, err = Parse(newerFmt)
		if !errors.Is(err, ErrNewerVault) {
			t.Fatalf("newer format %s: want ErrNewerVault, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("upgrade"))
	}
}

// covers: MA-708, R37
func TestChangingThePasswordReWrapsOnly(t *testing.T) {
	lowCost(t)
	v, k, rk, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	sealedBefore, err := k.SealUnit(UnitMessage, "msg-1", []byte("archived before the change"))
	if err != nil {
		t.Fatal(err)
	}
	const newPassword = "a different long passphrase"
	v2, err := v.WithPassword(k, newPassword)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Generation != v.Generation+1 {
		t.Fatalf("generation %d → %d, want +1", v.Generation, v2.Generation)
	}
	if v2.Password.Salt == v.Password.Salt {
		t.Fatal("the new password wrap must use a fresh salt")
	}
	if v2.Recovery.Salt != v.Recovery.Salt || !bytes.Equal(v2.Recovery.Wrapped, v.Recovery.Wrapped) {
		t.Fatal("a password change must leave the recovery entry byte-identical")
	}
	k2, err := v2.Unlock(newPassword)
	if err != nil {
		t.Fatalf("the new password must open: %v", err)
	}
	if _, err := v2.Unlock(testPassword); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old password must be refused, got %v", err)
	}
	if _, err := v2.UnlockRecovery(rk.String()); err != nil {
		t.Fatalf("the recovery key must still open: %v", err)
	}
	if got, err := k2.OpenUnit(UnitMessage, "msg-1", sealedBefore); err != nil || string(got) != "archived before the change" {
		t.Fatalf("a unit sealed before the change must open with the new key (no re-encryption): %q, %v", got, err)
	}

	unchanged, err := v.WithPassword(k, "too short")
	if !errors.Is(err, ErrWeakPassword) || unchanged != nil {
		t.Fatalf("a too-short new password must be refused with no new vault, got %v", err)
	}
	if _, err := v.Unlock(testPassword); err != nil {
		t.Fatalf("a refused change must leave the original working: %v", err)
	}
}

// covers: MA-708, R37
func TestANewRecoveryKeyReplacesTheOldOne(t *testing.T) {
	lowCost(t)
	v, k, oldRK, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	v2, newRK, err := v.WithNewRecovery(k)
	if err != nil {
		t.Fatal(err)
	}
	if newRK == oldRK {
		t.Fatal("the new recovery key must be newly drawn")
	}
	if v2.Generation != v.Generation+1 {
		t.Fatalf("generation %d → %d, want +1", v.Generation, v2.Generation)
	}
	if v2.Password.Salt != v.Password.Salt || !bytes.Equal(v2.Password.Wrapped, v.Password.Wrapped) {
		t.Fatal("a new recovery key must leave the password entry byte-identical")
	}
	if _, err := v2.UnlockRecovery(newRK.String()); err != nil {
		t.Fatalf("the new recovery key must open: %v", err)
	}
	if _, err := v2.UnlockRecovery(oldRK.String()); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("the old recovery key must be refused, got %v", err)
	}
	if _, err := v2.Unlock(testPassword); err != nil {
		t.Fatalf("the password must still open: %v", err)
	}
}

// covers: MA-708, R37
func TestAKeyFromAnotherVaultCannotRewriteThisOne(t *testing.T) {
	lowCost(t)
	v, _, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, foreign, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := v.WithPassword(foreign, "another long passphrase"); !errors.Is(err, ErrTampered) || n != nil {
		t.Fatalf("WithPassword with a foreign key: want ErrTampered, got %v", err)
	}
	if n, _, err := v.WithNewRecovery(foreign); !errors.Is(err, ErrTampered) || n != nil {
		t.Fatalf("WithNewRecovery with a foreign key: want ErrTampered, got %v", err)
	}
	if !strings.Contains(ErrTampered.Error(), "altered") {
		t.Fatalf("the tamper refusal must say the key file was altered: %q", ErrTampered)
	}
}
