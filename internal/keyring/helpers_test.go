package keyring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/vault"
)

// testPassword clears the 12-character floor.
const testPassword = "keyring test passphrase"

// freshRecord gives the test its own empty record of encrypted archives.
func freshRecord(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(encrecord.SetDirForTest(dir))
	return dir
}

// newStore is a file secret store of the test's own.
func newStore(t *testing.T) vault.Store { return graphconfig.NewFileSecretStore(t.TempDir()) }

// made creates and commits an encrypted archive in a new folder, remembering its
// key in s when s is not nil, and returns the folder, the recovery key and the
// archive, still unlocked.
func made(t *testing.T, s vault.Store) (string, string, *Unlocked) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "archive")
	p, err := Create(out, testPassword, CreateOptions{Remember: s != nil, Store: s})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rk := p.RecoveryKey()
	u, err := p.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	t.Cleanup(u.Destroy)
	return out, rk, u
}

// loadRecord reads the record, failing the test if it cannot be used.
func loadRecord(t *testing.T) *encrecord.Record {
	t.Helper()
	rec, err := encrecord.Load()
	if err != nil {
		t.Fatalf("load the record: %v", err)
	}
	return rec
}

// recordedID is the id the record holds for out, or "".
func recordedID(t *testing.T, out string) string {
	t.Helper()
	if m, ok := loadRecord(t).Lookup(out); ok {
		return m.ID
	}
	return ""
}

// change edits the record, failing the test if it cannot.
func change(t *testing.T, fn func(*encrecord.Record) error) {
	t.Helper()
	if _, err := encrecord.Update("test", fn); err != nil {
		t.Fatalf("change the record: %v", err)
	}
}

// damageRecord makes the record unusable (both copies fail their trailer).
func damageRecord(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{encrecord.FileName, encrecord.MirrorName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("MAILARCHIVE-RECORD 1 seq=9\nnot a record\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// encrecordSpelling is the record's comparison form of path.
func encrecordSpelling(path string) string { return encrecord.Spelling(path) }

// exists reports whether path is present.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// refusal maps an error to the (code, message) pair assure.Refused expects.
func refusal(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// countOpens counts the vault opens (derivations, for a password or recovery key)
// for the test's duration.
func countOpens(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := openVault
	openVault = func(out string, want vault.ID, s vault.Secret) (*vault.Opened, error) {
		n++
		return orig(out, want, s)
	}
	t.Cleanup(func() { openVault = orig })
	return &n
}

// countCreates counts the vault creates (derivations) for the test's duration.
func countCreates(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := prepareCreate
	prepareCreate = func(out, pw string) (*vault.PendingCreate, error) {
		n++
		return orig(out, pw)
	}
	t.Cleanup(func() { prepareCreate = orig })
	return &n
}

// raisePasswordCost rewrites both key files' password entry to ask for more memory
// than MailArchive's own (the header no longer authenticates, which only an open
// that derives can find).
func raisePasswordCost(t *testing.T, out string) {
	t.Helper()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		path := filepath.Join(out, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m["entries"].([]any) {
			if e := e.(map[string]any); e["kind"] == "password" {
				e["memory_kib"] = float64(vault.CostFloor().MemoryKiB * 2)
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
