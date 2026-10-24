package vault

import (
	"bytes"
	"encoding/base64"
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
	if err := ChangePasswordAt(other, ID{}, PasswordSecret(testPassword), testPassword+" again"); err != nil {
		t.Fatal(err) // the other archive's vault is now at generation 2, above ours
	}
	writeFile(t, out, BackupName, readFile(t, other, FileName))
	before := snapshot(out)
	unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }
	store := graphconfig.NewFileSecretStore(t.TempDir())
	Remember(store, k)
	Remember(store, otherKey)

	for label, try := range map[string]func() error{
		"Open":            func() error { _, err := Open(out, ID{}, PasswordSecret(testPassword)); return err },
		"Open (other pw)": func() error { _, err := Open(out, ID{}, PasswordSecret(testPassword+" again")); return err },
		"Heal":            func() error { return Heal(out, ID{}, PasswordSecret(testPassword)) },
		"ChangePassword": func() error {
			return ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), "yet another passphrase")
		},
		"Detect": func() error { _, _, err := Detect(out); return err },
		"gate":   func() error { _, err := RequireRetrievableKey(out, k.ArchiveID(), store); return err },
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
		"Open": func() error { _, err := Open(out, ID{}, PasswordSecret(testPassword)); return err },
		"Heal": func() error { return Heal(out, ID{}, PasswordSecret(testPassword)) },
		"ChangePassword": func() error {
			return ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), "yet another passphrase")
		},
		"Detect": func() error { _, _, err := Detect(out); return err },
		"gate":   func() error { _, err := RequireRetrievableKey(out, k.ArchiveID(), store); return err },
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
	if err := ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), pwA); err != nil {
		t.Fatal(err)
	}
	gen2A := readFile(t, out, FileName)
	writeFile(t, out, FileName, gen1)
	writeFile(t, out, BackupName, gen1)
	if err := ChangePasswordAt(out, ID{}, PasswordSecret(testPassword), pwB); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, FileName, gen2A) // sync left machine A's vault as the primary, B's as the mirror
	before := snapshot(out)

	o, err := Open(out, ID{}, PasswordSecret(pwB))
	if err != nil {
		t.Fatalf("machine B's password must open its own copy: %v", err)
	}
	if !o.Forked || !o.NeedsHeal || o.Vault.Generation != 2 {
		t.Fatalf("want a reported fork at generation 2, got forked=%v heal=%v gen=%d", o.Forked, o.NeedsHeal, o.Vault.Generation)
	}
	if mirror, _ := Parse(before[1]); o.Vault.mac != mirror.mac {
		t.Fatal("on a tie the vault returned must be the copy the secret opened (B's mirror), not the primary")
	}
	err = Heal(out, ID{}, PasswordSecret(pwB))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Heal must not choose between two people's changes: want ErrConflict, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("change the password"), assure.NoSideEffect(func() bool {
		s := snapshot(out)
		return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1])
	}))

	if err := ChangePasswordAt(out, ID{}, PasswordSecret(pwB), pwC); err != nil {
		t.Fatalf("a deliberate change from one side settles the fork: %v", err)
	}
	o, err = Open(out, ID{}, PasswordSecret(pwC))
	if err != nil || o.Forked || o.NeedsHeal || o.Vault.Generation != 3 {
		t.Fatalf("after the change: want one vault at generation 3, got %v", err)
	}
	if _, err := Open(out, ID{}, PasswordSecret(pwA)); !errors.Is(err, ErrWrongSecret) {
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
	o, err := Open(out, ID{}, PasswordSecret(testPassword))
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
	_, err := Open(out, ID{}, PasswordSecret("not the password at all"))
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
	_, err = Open(out, ID{}, PasswordSecret("not the password at all"))
	if !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("a planted copy must not mask a plain wrong password: want ErrWrongSecret, got %v", err)
	}
}

// covers: MA-715, R37
func TestAForeignVaultBesideAMissingOrDamagedOwnCopyIsRefused(t *testing.T) {
	lowCost(t)
	other, _, _ := newArchive(t) // same password: people reuse them
	for label, own := range map[string]func(t *testing.T, out string){
		"own mirror missing": func(t *testing.T, out string) { removeFile(t, out, BackupName) },
		"own mirror damaged": func(t *testing.T, out string) {
			editVault(t, out, BackupName, func(m map[string]any) { m["mac"] = "not hex" }) // still names this archive
		},
	} {
		t.Run(label, func(t *testing.T) {
			out, k, _ := newArchive(t)
			own(t, out)
			writeFile(t, out, FileName, readFile(t, other, FileName))
			before := snapshot(out)
			unchanged := func() bool { s := snapshot(out); return bytes.Equal(s[0], before[0]) && bytes.Equal(s[1], before[1]) }
			for op, try := range map[string]func() error{
				"Open": func() error { _, err := Open(out, k.ArchiveID(), PasswordSecret(testPassword)); return err },
				"Heal": func() error { return Heal(out, k.ArchiveID(), PasswordSecret(testPassword)) },
				"ChangePassword": func() error {
					return ChangePasswordAt(out, k.ArchiveID(), PasswordSecret(testPassword), "yet another passphrase")
				},
			} {
				err := try()
				if !errors.Is(err, ErrTampered) {
					t.Fatalf("%s with the recorded id: want ErrTampered, got %v", op, err)
				}
				rc, msg := refusal(err)
				assure.Refused(t, rc, msg, assure.Code(1), assure.Names(k.ArchiveID().String()), assure.NoSideEffect(unchanged))
			}
			if label == "own mirror damaged" {
				// The damaged copy still names this archive, so the mix is caught even
				// with no recorded id.
				if err := Heal(out, ID{}, PasswordSecret(testPassword)); !errors.Is(err, ErrTampered) || !unchanged() {
					t.Fatalf("a damaged own copy beside a foreign one must never be healed over, got %v", err)
				}
			}
		})
	}
}

// covers: MA-715, R37
func TestANewerFormatMirrorWithTrailingDataIsStillNewer(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	var m map[string]any
	if err := json.Unmarshal(readFile(t, out, BackupName), &m); err != nil {
		t.Fatal(err)
	}
	m["format"] = VaultFormat + 1
	first, _ := json.Marshal(m)
	writeFile(t, out, BackupName, append(first, []byte("\n{\"signature\":\"from a future format\"}\n")...))
	before := snapshot(out)
	for op, try := range map[string]func() error{
		"Open":   func() error { _, err := Open(out, k.ArchiveID(), PasswordSecret(testPassword)); return err },
		"Heal":   func() error { return Heal(out, k.ArchiveID(), PasswordSecret(testPassword)) },
		"Detect": func() error { _, _, err := Detect(out); return err },
	} {
		if err := try(); !errors.Is(err, ErrNewerVault) {
			t.Fatalf("%s: a newer mirror with trailing data must read as newer, got %v", op, err)
		}
	}
	if s := snapshot(out); !bytes.Equal(s[0], before[0]) || !bytes.Equal(s[1], before[1]) {
		t.Fatal("the newer copy was healed over")
	}
}

// covers: MA-715, R37
func TestAReformattedCopyIsHealedNotAFork(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	reformatted := bytes.ReplaceAll(readFile(t, out, BackupName), []byte("\n"), []byte("\r\n"))
	reformatted = bytes.ReplaceAll(reformatted, []byte("  "), []byte("\t"))
	writeFile(t, out, BackupName, reformatted)
	o, err := Open(out, k.ArchiveID(), PasswordSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	if o.Forked || !o.NeedsHeal {
		t.Fatalf("the same vault re-spaced is a heal, not a fork: forked=%v heal=%v", o.Forked, o.NeedsHeal)
	}
	if err := Heal(out, k.ArchiveID(), PasswordSecret(testPassword)); err != nil {
		t.Fatalf("Heal must restore a re-spaced copy: %v", err)
	}
	if s := snapshot(out); !bytes.Equal(s[0], s[1]) {
		t.Fatal("after Heal both copies must be identical")
	}
}

// covers: MA-715, R37
func TestANewRecoveryKeySettlesAForkFromTheMirrorSide(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	gen1 := readFile(t, out, FileName)
	const pwA, pwB = "machine A passphrase", "machine B passphrase"
	if err := ChangePasswordAt(out, k.ArchiveID(), PasswordSecret(testPassword), pwA); err != nil {
		t.Fatal(err)
	}
	gen2A := readFile(t, out, FileName)
	writeFile(t, out, FileName, gen1)
	writeFile(t, out, BackupName, gen1)
	if err := ChangePasswordAt(out, k.ArchiveID(), PasswordSecret(testPassword), pwB); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, FileName, gen2A) // A's vault in the primary, B's in the mirror

	p, err := PrepareNewRecovery(out, k.ArchiveID(), PasswordSecret(pwB))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatalf("Heal's advice (make a new recovery key from the side to keep) must work from the mirror side: %v", err)
	}
	o, err := Open(out, k.ArchiveID(), PasswordSecret(pwB))
	if err != nil || o.Forked || o.NeedsHeal || o.Vault.Generation != 3 {
		t.Fatalf("want B's side settled at generation 3, got %v", err)
	}
	if _, err := Open(out, k.ArchiveID(), RecoverySecret(p.RecoveryKey().String())); err != nil {
		t.Fatalf("the new recovery key must open: %v", err)
	}
}

// wrapAt wraps k's master key for kind under secret at cost p — a cost this
// build never writes, to model a future writer that raised it.
func wrapAt(t *testing.T, k *Key, kind entryKind, secret []byte, p Params) Entry {
	t.Helper()
	e := Entry{Params: p}
	e.Salt[0] = byte(kind) + 7 // any salt; distinct from the original
	kek, err := deriveKEK(secret, e.Salt[:], p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Wrapped, err = seal(kek, wrapAAD(k.id, kind), k.master[:]); err != nil {
		t.Fatal(err)
	}
	return e
}

// covers: MA-715, R37
func TestACurrentSecretIsNotSupersededByACheaperOlderCopy(t *testing.T) {
	lowCost(t)
	out, k, rk := newArchive(t)
	gen1 := readFile(t, out, FileName)
	v1, err := Parse(gen1)
	if err != nil {
		t.Fatal(err)
	}
	// Generation 2: the same recovery key re-wrapped at a higher time cost, so
	// the stale generation-1 mirror is the cheaper copy to try first.
	v2 := *v1
	v2.Generation = 2
	v2.Recovery = wrapAt(t, k, entryRecovery, rk[:], Params{MemoryKiB: floor.MemoryKiB, Time: floor.Time + 2, Threads: 1})
	v2.mac = v2.computeMAC(k)
	b2, err := v2.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, FileName, b2)
	writeFile(t, out, BackupName, gen1)

	o, err := Open(out, k.ArchiveID(), RecoverySecret(rk.String()))
	if err != nil {
		t.Fatalf("a recovery key that still opens generation 2 must not be called superseded: %v", err)
	}
	if o.Vault.Generation != 2 || !o.NeedsHeal {
		t.Fatalf("want generation 2 with NeedsHeal, got %d / %v", o.Vault.Generation, o.NeedsHeal)
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	Remember(store, k)
	g, err := RequireRetrievableKey(out, k.ArchiveID(), store)
	if err != nil || g.Vault.Generation != 2 {
		t.Fatalf("the remembered key must open generation 2, got %v", err)
	}
}

// covers: MA-715, R37
func TestTamperingFoundByTheRightPasswordIsReportedAsTampering(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	editVault(t, out, FileName, func(m map[string]any) { m["generation"] = 7 }) // header altered; wrap intact
	editVault(t, out, BackupName, func(m map[string]any) {
		e := m["entries"].([]any)[0].(map[string]any)
		w, err := base64.StdEncoding.DecodeString(e["wrapped"].(string))
		if err != nil {
			t.Fatal(err)
		}
		w[len(w)-1] ^= 1
		e["wrapped"] = base64.StdEncoding.EncodeToString(w) // well-formed junk that no password unwraps
	})
	if _, err := Parse(readFile(t, out, BackupName)); err != nil {
		t.Fatalf("fixture: the junk mirror must still parse, so both reasons are in play: %v", err)
	}
	_, err := Open(out, k.ArchiveID(), PasswordSecret(testPassword))
	if !errors.Is(err, ErrTampered) {
		t.Fatalf("the right password against an altered header beside junk: want ErrTampered, got %v", err)
	}
}
