package container

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-791, R37
// vault.PrepareRestore proves a key against the sealed record by reading the
// pointer's frozen header itself (vault cannot import this package): a pointer
// sealed by container.Seal, under either of its names, proves it — so the vault's
// copy of the names and header cannot drift from this package's.
func TestARestoreIsProvenByARealRecordPointer(t *testing.T) {
	for _, name := range []string{PointerName, MirrorName} {
		out := t.TempDir()
		p, err := vault.PrepareCreate(out, "container restore passphrase")
		if err != nil {
			t.Fatal(err)
		}
		rk := p.RecoveryKey().String()
		file, err := p.RecoveryFile()
		if err != nil {
			t.Fatal(err)
		}
		k, err := p.Commit()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := Seal(k, Pointer{Head: Head{Format: vault.FormatVersion, Gen: 2}})
		k.Destroy()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, kf := range []string{vault.FileName, vault.BackupName} {
			os.Remove(filepath.Join(out, kf))
		}
		e, err := vault.ParseRecoveryFile(file)
		if err != nil {
			t.Fatal(err)
		}
		r, err := vault.PrepareRestore(out, vault.ID{}, vault.RecoveryEntrySource{Entry: e, RecoveryKey: rk}, "a restored passphrase here")
		assure.Reached(t, r, "a restore proven by "+name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r.Discard()
	}
}
