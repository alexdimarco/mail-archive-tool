package vault

import (
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-719, R37
func TestThePreservedOriginalIsItsOwnUnitKind(t *testing.T) {
	id := fixedID()
	const idHex = "000102030405060708090a0b0c0d0e0f"
	aad, err := unitAAD(id, UnitOriginal, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(aad), "4d415531"+"0001"+idHex+"03"+"0002"+"6d31"; got != want {
		t.Fatalf("UnitOriginal AAD:\n got  %s\n want %s", got, want)
	}

	k := testKey(t, id, 0x42)
	const ref = "0123456789abcdef0123456789abcdef.1"
	page, err := k.SealUnit(UnitMessage, ref, []byte("the page"))
	if err != nil {
		t.Fatal(err)
	}
	orig, err := k.SealUnit(UnitOriginal, ref, []byte("the original"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := k.OpenUnit(UnitOriginal, ref, orig); err != nil || string(got) != "the original" {
		t.Fatalf("positive twin: the original opens as an original: %q %v", got, err)
	}
	if _, err := k.OpenUnit(UnitOriginal, ref, page); !errors.Is(err, ErrAuth) {
		t.Fatalf("a page must not open as the original of the same unit, got %v", err)
	}
	if _, err := k.OpenUnit(UnitMessage, ref, orig); !errors.Is(err, ErrAuth) {
		t.Fatalf("an original must not open as the page of the same unit, got %v", err)
	}

	for _, bad := range []UnitKind{0, 4, 255} {
		_, sealErr := k.SealUnit(bad, ref, []byte("x"))
		_, openErr := k.OpenUnit(bad, ref, page)
		_, nameErr := k.UnitName(bad, ref)
		for use, err := range map[string]error{"SealUnit": sealErr, "OpenUnit": openErr, "UnitName": nameErr} {
			if !errors.Is(err, ErrUnitKind) {
				t.Fatalf("%s with kind %d: want ErrUnitKind, got %v", use, bad, err)
			}
		}
	}
}

// covers: MA-720, R37
func TestUnitNamesAreTheFrozenKeyedDerivation(t *testing.T) {
	// Pinned against an independent HKDF + HMAC (Python hashlib/hmac).
	k := testKey(t, fixedID(), 0x42)
	if got := hex.EncodeToString(k.names[:]); got != "b76dafb1016d0e8b1a5e5cf04725b6277788540789c1f99d89d9231cbc27b82b" {
		t.Fatalf("names sub-key (HKDF-SHA256, info \"mailarchive unit names v1\") is %s", got)
	}
	const ref = "0123456789abcdef0123456789abcdef.1"
	for kind, want := range map[UnitKind]string{
		UnitMessage:    "fc25b5ed46130915ca0b9ec8cfa82bf7",
		UnitAttachment: "e4ede67498072064bbe5b193bca08273",
		UnitOriginal:   "2c5d6db7dd0f98e9c078113a9b315994",
	} {
		got, err := k.UnitName(kind, ref)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("UnitName(%d, %q) = %s, want %s", kind, ref, got, want)
		}
	}
}

// covers: MA-720, R37
func TestUnitNamesRevealNothingAndNeverCollideAcrossRolesOrRevisions(t *testing.T) {
	k := testKey(t, fixedID(), 0x42)
	other := testKey(t, fixedID(), 0x43) // another archive's key
	shape := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := map[string]string{}
	for _, ref := range []string{"0123456789abcdef0123456789abcdef.1", "0123456789abcdef0123456789abcdef.2", "fedcba9876543210fedcba9876543210.1"} {
		for _, kind := range []UnitKind{UnitMessage, UnitAttachment, UnitOriginal} {
			name, err := k.UnitName(kind, ref)
			if err != nil {
				t.Fatal(err)
			}
			if !shape.MatchString(name) {
				t.Fatalf("name %q is not 32 lowercase hex characters", name)
			}
			if strings.Contains(name, strings.Split(ref, ".")[0][:8]) {
				t.Fatalf("name %q carries the unit id", name)
			}
			label := ref + "/" + string(rune('0'+kind))
			if prev, dup := seen[name]; dup {
				t.Fatalf("%s and %s share the on-disk name %s", prev, label, name)
			}
			seen[name] = label
			again, _ := k.UnitName(kind, ref)
			if again != name {
				t.Fatal("a unit name must be the same every time it is derived")
			}
			if theirs, _ := other.UnitName(kind, ref); theirs == name {
				t.Fatal("another archive's key must derive different names for the same reference")
			}
		}
	}
	assure.Reached(t, seen, "derived names")

	for _, ref := range []string{"", strings.Repeat("a", maxUnitIDLen+1)} {
		_, err := k.UnitName(UnitMessage, ref)
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("unit reference"))
	}
}
