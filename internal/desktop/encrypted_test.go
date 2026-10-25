package desktop

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// vaultedArchive is an archive folder holding a committed vault beside a plaintext
// manifest: an encrypted archive whatever its manifest says.
func vaultedArchive(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, manifestName), []byte(`{"version":6,"entries":{},"stores":{"path:/mail":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := vault.PrepareCreate(out, "desktop test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	return out
}

// covers: MA-731, R37
// An encrypted archive is listed and labelled as one: the archive list and the
// dashboard both say "encrypted" (never greyed out as not an archive, never the
// kind its manifest's store ids would give). The positive twin: the same manifest
// without a vault is a local archive.
func TestAnEncryptedArchiveIsListedAndLabelledAsEncrypted(t *testing.T) {
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, manifestName), []byte(`{"version":6,"entries":{},"stores":{"path:/mail":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if archiveKind(plain) != kindLocal || archindex.Kind(plain) != "local" {
		t.Fatalf("the positive twin: a plaintext local archive reads as %q / %q", archiveKind(plain), archindex.Kind(plain))
	}
	sealed := vaultedArchive(t)
	if got := archiveKind(sealed); got != kindEncrypted {
		t.Fatalf("the dashboard labels an encrypted archive %q", got)
	}
	if !archindex.IsArchive(sealed) || archindex.Kind(sealed) != archindex.KindEncrypted {
		t.Fatalf("the archive list reads an encrypted archive as %q (archive: %v)", archindex.Kind(sealed), archindex.IsArchive(sealed))
	}
}

// covers: MA-731, R37
// Without its key the Desktop neither serves an encrypted archive in the reader
// (503, saying it is encrypted, before any index is opened) nor captures into it:
// the scheduled capture refuses and records the refusal as a failed run, and no
// unit or manifest changes.
func TestTheDesktopRefusesAnEncryptedArchiveWithoutItsKey(t *testing.T) {
	sealed := vaultedArchive(t)
	before, _ := os.ReadFile(filepath.Join(sealed, manifestName))

	rm := &readerMux{cfg: Config{Out: sealed}}
	rec := httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || rm.ix != nil {
		t.Fatalf("the reader served an encrypted archive: %d (index open: %v)", rec.Code, rm.ix != nil)
	}
	assure.Refused(t, 1, rec.Body.String(), assure.Code(1), assure.Names(sealed, "encrypted archive", "password or recovery key"))

	dir := t.TempDir()
	cfg := Config{ConfigPath: filepath.Join(dir, "graph-config.json"), SettingsPath: filepath.Join(dir, "settings.json"), TokenCachePath: filepath.Join(dir, "tok.json"), Out: sealed}
	err := HeadlessCapture(cfg, io.Discard)
	if !errors.Is(err, app.ErrEncryptedNeedsKey) {
		t.Fatalf("want ErrEncryptedNeedsKey before the setup check, got %v", err)
	}
	lr, st, rerr := state.ReadLastRun(sealed)
	if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed || !strings.Contains(lr.Error, "encrypted") {
		t.Fatalf("the refused capture is not recorded as a failed run: %+v (%v)", lr, rerr)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed), assure.NoSideEffect(func() bool {
		after, _ := os.ReadFile(filepath.Join(sealed, manifestName))
		_, uerr := os.Stat(filepath.Join(sealed, "units"))
		return string(after) == string(before) && os.IsNotExist(uerr)
	}))
}
