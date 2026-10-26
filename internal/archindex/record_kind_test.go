package archindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/encrecord"
)

// covers: MA-775, R37
// The archive list labels a folder this computer recorded as encrypted, stripped of
// its key files and replanted with a plaintext manifest, as encrypted — never as
// the plaintext archive it now looks like (the P3-0 adversarial pass, FID-06). The
// positive twin: the same folder, not recorded, is a plaintext archive.
func TestARecordedFolderIsListedAsEncrypted(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
	if k := Kind(out); k == KindEncrypted {
		t.Fatalf("not recorded, the folder is listed as %q", k)
	}
	id := "0123456789abcdef0123456789abcdef"
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if k := Kind(out); k != KindEncrypted {
		t.Fatalf("recorded, the folder is listed as %q", k)
	}
}
