package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeSecret is a graphconfig.SecretStore stand-in that records Delete calls and can be
// made to fail, so the test needs no real OS vault.
type fakeSecret struct {
	deleted []string
	err     error
}

func (f *fakeSecret) Set(account, secret string) error { return nil }
func (f *fakeSecret) Get(account string) (string, error) {
	return "", nil
}
func (f *fakeSecret) Delete(account string) error {
	f.deleted = append(f.deleted, account)
	return f.err
}
func (f *fakeSecret) Name() string { return "fake" }

// covers: MA-672
// Uninstall clears BOTH credentials — the sign-in token AND the client secret — and is
// fail-closed: a real clear error is surfaced, not swallowed; an already-absent
// credential is fine (U-C4).
func TestMA672_ClearCredentialsBothAndFailClosed(t *testing.T) {
	// A token-cache file present + a secret store: both are cleared.
	tok := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(tok, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSecret{}
	if err := ClearCredentials("contoso.onmicrosoft.com", "client-123", nil, tok, fs); err != nil {
		t.Fatalf("clear must succeed: %v", err)
	}
	if _, err := os.Stat(tok); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the token cache file must be deleted")
	}
	if len(fs.deleted) != 1 {
		t.Fatalf("the client secret must be deleted exactly once, got %v", fs.deleted)
	}

	// Fail-closed: a real secret-store error is surfaced, not swallowed.
	fs2 := &fakeSecret{err: errors.New("vault is locked")}
	if err := ClearCredentials("t", "c", nil, filepath.Join(t.TempDir(), "absent.json"), fs2); err == nil {
		t.Fatal("a failing secret clear must be surfaced (fail-closed), not swallowed")
	}

	// An already-absent token + a clean secret store: not an error.
	if err := ClearCredentials("t", "c", nil, filepath.Join(t.TempDir(), "absent.json"), &fakeSecret{}); err != nil {
		t.Fatalf("an already-absent credential must not be an error: %v", err)
	}
}
