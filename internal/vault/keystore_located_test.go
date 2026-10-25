package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-741, R37
// Remember refuses a secret store whose folder is inside an encrypted archive — the
// key's own or any other — before writing (P2a-C14): the master key would travel with
// the archive it opens. Nothing is written there. The positive twin: a store outside
// every archive files the key.
func TestRememberRefusesAStoreInsideAnEncryptedArchive(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	other, _, _ := newArchive(t)
	if err := Remember(graphconfig.NewFileSecretStore(t.TempDir()), k); err != nil {
		t.Fatalf("the positive twin: a store outside every archive files the key: %v", err)
	}
	for _, archive := range []string{out, other} {
		dir := filepath.Join(archive, "Library", "mailarchive", "secrets")
		err := Remember(graphconfig.NewFileSecretStore(dir), k)
		if !errors.Is(err, ErrStoreInsideArchive) {
			t.Fatalf("a store inside %s: want ErrStoreInsideArchive, got %v", archive, err)
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(archive, "outside the archive"),
			assure.NoSideEffect(func() bool {
				_, serr := os.Stat(filepath.Join(archive, "Library"))
				return os.IsNotExist(serr)
			}))
	}
}
