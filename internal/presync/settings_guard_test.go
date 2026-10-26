package presync_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/vault"
)

// covers: MA-775, MA-771, R37
// The Thunderbird pre-sync state is a settings write — it names a profile — so it is
// never written inside an encrypted archive: it is refused, naming the file and the
// archive, and the archive is left as it was (phase3 §6 E5; the P3-0 adversarial
// pass, SEC-02). encmark installs the guard here, as every program that links it
// does. The positive twin: in an ordinary folder it is written.
func TestThePreSyncStateIsNeverWrittenInsideAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	plain := filepath.Join(t.TempDir(), "presync-state.json")
	if err := presync.StoreRecordForTest(plain); err != nil {
		t.Fatalf("the positive twin, an ordinary folder: %v", err)
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("the positive twin wrote no state: %v", err)
	}

	enc := t.TempDir()
	if err := os.WriteFile(filepath.Join(enc, vault.FileName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(enc, "settings")
	if err := os.Mkdir(settings, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(settings, "presync-state.json")
	err := presync.StoreRecordForTest(p)
	if !errors.Is(err, encmark.ErrSettingsInEncrypted) {
		t.Fatalf("want ErrSettingsInEncrypted, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(p, enc, "outside every archive"),
		assure.NoSideEffect(func() bool {
			entries, rerr := os.ReadDir(settings)
			return rerr == nil && len(entries) == 0
		}))
}
