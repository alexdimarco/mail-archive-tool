package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/encrecord"
)

// covers: MA-775, R37
// Desktop labels a folder this computer recorded as encrypted, stripped of its key
// files and replanted with a plaintext manifest, as encrypted — its source-kind
// features stay off — and never as the plaintext archive it now looks like (the
// P3-0 adversarial pass, FID-06). The positive twin: not recorded, it is not.
func TestDesktopLabelsARecordedFolderEncrypted(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
	if k := archiveKind(out); k == kindEncrypted {
		t.Fatalf("not recorded, the folder is labelled %q", k)
	}
	id := "0123456789abcdef0123456789abcdef"
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if k := archiveKind(out); k != kindEncrypted {
		t.Fatalf("recorded, the folder is labelled %q", k)
	}
}
