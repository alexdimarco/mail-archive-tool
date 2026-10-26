package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-797, MA-759, R36, R37
// Uninstall owns the scratch index an encrypted archive's reader works on above the
// memory budget — index/reader-<pid>-<n>.db, the shape app.ReaderScratch names, the
// two patterns pinned equal — and still refuses the whole uninstall for any other
// name in index/ (design-encrypted-archives-phase3 §7.4).
func TestUninstallOwnsAReadersScratchIndex(t *testing.T) {
	if readerScratch.String() != app.ReaderScratchName.String() {
		t.Fatalf("uninstall's reader scratch pattern %q is not app's %q", readerScratch, app.ReaderScratchName)
	}
	root, files := scratchFixture(t)
	reader := filepath.Join(filepath.Dir(files[2]), "reader-4242-7.db")
	if err := os.WriteFile(reader, []byte("plaintext index"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ScratchTargets(root, nil)
	found := false
	for _, p := range got {
		found = found || p == reader
	}
	assure.Reached(t, err == nil && found, "the reader's scratch index owned")

	for _, name := range []string{"reader-4242.db", "reader-x-1.db", "reader-1-2.db-journal"} {
		bad := filepath.Join(filepath.Dir(files[2]), name)
		os.WriteFile(bad, nil, 0o600)
		_, err := ScratchTargets(root, nil)
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(name))
		if !errors.Is(err, ErrScratchUnexpected) {
			t.Fatalf("%s: want ErrScratchUnexpected, got %v", name, err)
		}
		os.Remove(bad)
	}
}
