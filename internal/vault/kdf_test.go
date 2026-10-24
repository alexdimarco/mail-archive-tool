package vault

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/assure"
)

// covers: MA-705, R37
func TestShippedKDFFloor(t *testing.T) {
	if want := (Params{MemoryKiB: 256 * 1024, Time: 3, Threads: 1}); floor != want {
		t.Fatalf("the shipped Argon2id floor is %+v, want %+v (A3) — lowering it is an operator decision, not a code change", floor, want)
	}
	if want := (Params{MemoryKiB: 1024 * 1024, Time: 16, Threads: 4}); ceiling != want {
		t.Fatalf("the shipped Argon2id ceiling is %+v, want %+v", ceiling, want)
	}
	if MinPasswordRunes != 12 {
		t.Fatalf("the password floor is %d characters, want 12 (C12)", MinPasswordRunes)
	}
}

// covers: MA-705, R37
func TestFullCostVaultRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 256 MiB per derivation; run without -short")
	}
	v, k, rk, err := Create(testPassword)
	if err != nil {
		t.Fatalf("Create at the shipped cost: %v", err)
	}
	if v.Password.Params != floor || v.Recovery.Params != floor {
		t.Fatalf("a new vault must record the floor cost, got %+v / %+v", v.Password.Params, v.Recovery.Params)
	}
	got, err := v.Unlock(testPassword)
	if err != nil {
		t.Fatalf("Unlock at the shipped cost: %v", err)
	}
	if got.master != k.master {
		t.Fatal("unlock returned a different master key")
	}
	assure.Reached(t, rk.String(), "recovery key")
}

// covers: MA-705, R37
func TestUnlockRefusesACostOutsideTheBoundsBeforeDeriving(t *testing.T) {
	lowCost(t)
	v, _, _, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Unlock(testPassword); err != nil {
		t.Fatalf("positive twin: the untouched vault unlocks: %v", err)
	}
	cases := map[string]struct {
		p    Params
		name string
	}{
		"time below floor":      {Params{floor.MemoryKiB, floor.Time - 1, floor.Threads}, "below the minimum"},
		"memory below floor":    {Params{floor.MemoryKiB - 1, floor.Time, floor.Threads}, "below the minimum"},
		"threads below floor":   {Params{floor.MemoryKiB, floor.Time, 0}, "below the minimum"},
		"memory above ceiling":  {Params{ceiling.MemoryKiB + 1, floor.Time, floor.Threads}, "above the maximum"},
		"time above ceiling":    {Params{floor.MemoryKiB, ceiling.Time + 1, floor.Threads}, "above the maximum"},
		"threads above ceiling": {Params{floor.MemoryKiB, floor.Time, ceiling.Threads + 1}, "above the maximum"},
	}
	runs := countDerivations(t)
	for label, c := range cases {
		bad := *v
		bad.Password.Params = c.p
		*runs = nil
		k, err := bad.Unlock(testPassword)
		if !errors.Is(err, ErrParams) {
			t.Fatalf("%s: want ErrParams, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(c.name))
		if k != nil {
			t.Fatalf("%s: a refused unlock returned a key", label)
		}
		if len(*runs) != 0 {
			t.Fatalf("%s: %d derivations ran before the refusal; the cost must be refused BEFORE any is spent", label, len(*runs))
		}
	}
}

// covers: MA-705, R37
func TestARecoveryUnlockRefusesACostOutsideTheBoundsToo(t *testing.T) {
	lowCost(t)
	v, _, rk, err := Create(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	runs := countDerivations(t)
	for label, p := range map[string]Params{
		"below floor":   {floor.MemoryKiB, floor.Time - 1, floor.Threads},
		"above ceiling": {ceiling.MemoryKiB + 1, floor.Time, floor.Threads},
	} {
		bad := *v
		bad.Recovery.Params = p
		*runs = nil
		if _, err := bad.UnlockRecovery(rk.String()); !errors.Is(err, ErrParams) || len(*runs) != 0 {
			t.Fatalf("%s: want ErrParams with no derivation, got %v after %d", label, err, len(*runs))
		}
	}
}

// covers: MA-705, R37
func TestNewPasswordFloor(t *testing.T) {
	lowCost(t)
	if _, _, _, err := Create(strings.Repeat("x", MinPasswordRunes)); err != nil {
		t.Fatalf("positive twin: a %d-character password is accepted: %v", MinPasswordRunes, err)
	}
	for label, pw := range map[string]string{
		"11 characters":                                       strings.Repeat("x", MinPasswordRunes-1),
		"11 two-byte characters (22 bytes)":                   strings.Repeat("\u00e9", MinPasswordRunes-1),
		"6 decomposed accents (12 code points, 6 characters)": strings.Repeat("e\u0301", 6),
		"empty": "",
	} {
		v, k, _, err := Create(pw)
		if !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("%s: want ErrWeakPassword, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("at least 12"))
		if v != nil || k != nil {
			t.Fatalf("%s: a refused create returned a vault or key", label)
		}
	}
	if _, _, _, err := Create("valid prefix \xff\xfe more text"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("invalid UTF-8: want ErrWeakPassword, got %v", err)
	}
}

// covers: MA-705, R37
func TestPasswordIsUnicodeNormalised(t *testing.T) {
	lowCost(t)
	composed := norm.NFC.String("crème brûlée à la carte")
	decomposed := norm.NFD.String(composed)
	if composed == decomposed {
		t.Fatal("fixture: the two forms must differ byte-wise")
	}
	v, k, _, err := Create(composed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Unlock(decomposed)
	if err != nil {
		t.Fatalf("the decomposed spelling of the same password must unlock: %v", err)
	}
	if got.master != k.master {
		t.Fatal("unlocked a different master key")
	}
}
