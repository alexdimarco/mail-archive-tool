package graphconfig

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-262, R12, R4, S40
// The non-secret config round-trips through an atomic 0600 file; a missing file is
// a typed not-found (errors.Is ErrNotExist), not a crash; the default path is under
// the OS config dir, never an archive -out; and the secret is never part of it.
func TestConfigRoundTripAndMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "graph-config.json")

	// Missing → typed not-found.
	_, err := Load(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing config should be ErrNotExist, got %v", err)
	}

	in := &Config{Tenant: "contoso.onmicrosoft.com", ClientID: "APPID", Auth: "app"}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %04o, want 0600", fi.Mode().Perm())
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tenant != in.Tenant || got.ClientID != in.ClientID || got.Auth != in.Auth || got.Version != ConfigVersion {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Default path is under the OS config dir, not an archive directory.
	if dp, err := DefaultConfigPath(); err == nil {
		if !strings.Contains(filepath.ToSlash(dp), "mailarchive/graph-config.json") {
			t.Errorf("default config path unexpected: %s", dp)
		}
	}
}

// covers: MA-263, R4, R12, S40
// The file SecretStore round-trips Set→Get and Delete removes it; Get for an unset
// account is a typed not-found; and Get enforces the secure-file discipline (a
// loose-mode secret file is refused).
func TestFileSecretStore(t *testing.T) {
	dir := t.TempDir()
	s := fileStore{dir: dir}
	acct := AccountKey("contoso", "APPID")

	// Unset → not-found.
	_, err := s.Get(acct)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unset account should be ErrNotExist, got %v", err)
	}

	if err := s.Set(acct, "s3cr3t-value"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(acct)
	if err != nil || got != "s3cr3t-value" {
		t.Fatalf("round-trip: got %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(s.path(acct)); fi.Mode().Perm() != 0o600 {
			t.Errorf("secret file mode = %04o, want 0600", fi.Mode().Perm())
		}
		// Loose mode → refused on read (shared ReadSecureFile discipline).
		os.Chmod(s.path(acct), 0o644)
		_, err = s.Get(acct)
		assure.Reached(t, errStr(err), "loose-mode refusal")
		if !strings.Contains(errStr(err), "chmod 600") {
			t.Errorf("loose-mode refusal should name chmod 600: %v", err)
		}
		os.Chmod(s.path(acct), 0o600)
	}

	if err := s.Delete(acct); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(acct); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after Delete, Get should be ErrNotExist, got %v", err)
	}
	// Delete of an absent account is a no-op, not an error.
	if err := s.Delete(acct); err != nil {
		t.Errorf("Delete of absent account should be nil, got %v", err)
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
