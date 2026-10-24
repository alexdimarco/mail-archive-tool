package vault

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
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
	got, err := RequireRetrievableKey(out, store)
	if err != nil {
		t.Fatalf("the remembered key must open the archive: %v", err)
	}
	if pt, err := got.OpenUnit(UnitMessage, "msg-1", sealed); err != nil || string(pt) != "archived" {
		t.Fatalf("the retrieved key must open the archive's units: %q %v", pt, err)
	}

	if err := ChangePasswordAt(out, PasswordSecret(testPassword), "the replacement passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireRetrievableKey(out, store); err != nil {
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
	if _, err := RequireRetrievableKey(out, healthy); err != nil {
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
		key, err := RequireRetrievableKey(out, store)
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

	_, err := RequireRetrievableKey(t.TempDir(), healthy)
	if !errors.Is(err, ErrNoVault) {
		t.Fatalf("a plain folder: want ErrNoVault, got %v", err)
	}
}
