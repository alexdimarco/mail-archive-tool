package desktop

import (
	"context"
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

// covers: MA-740, R37
// The scheduled Desktop capture never opens a log inside an encrypted archive (a
// schedule the Desktop installed points its log into the archive): for an encrypted
// archive the capture is refused as one without its key and recorded as the minimal
// failed run, with no log file created; a plaintext archive's capture whose log would
// be inside some encrypted archive is refused before the log opens. The positive twin:
// a plaintext archive's capture opens its log where it is told.
func TestTheScheduledCaptureNeverOpensALogInsideAnEncryptedArchive(t *testing.T) {
	env := &launchEnv{Stderr: io.Discard}
	cfgPath := filepath.Join(t.TempDir(), "graph-config.json")
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, manifestName), []byte(`{"version":6,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(t.TempDir(), "capture.log")
	_ = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", plain, "-log", ok}, env)
	if _, err := os.Stat(ok); err != nil {
		t.Fatalf("the positive twin: a plaintext archive's capture opens its log: %v", err)
	}

	sealed := vaultedArchive(t)
	inside := filepath.Join(sealed, "mailarchive-backup.log")
	err := launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", sealed, "-log", inside}, env)
	if !errors.Is(err, app.ErrEncryptedNeedsKey) {
		t.Fatalf("an encrypted archive's capture: want ErrEncryptedNeedsKey, got %v", err)
	}
	if lr, _, _ := state.ReadLastRun(sealed); lr.ErrorKind != state.FailEncryptedNeedsKey || lr.Error != "" {
		t.Fatalf("the refused capture is not the minimal record: %+v", lr)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed), assure.NoSideEffect(func() bool {
		_, serr := os.Stat(inside)
		return os.IsNotExist(serr)
	}))

	elsewhere := filepath.Join(sealed, "other.log")
	err = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", plain, "-log", elsewhere}, env)
	if !errors.Is(err, app.ErrLogInEncryptedArchive) {
		t.Fatalf("a log inside another encrypted archive: want ErrLogInEncryptedArchive, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed, "-log"), assure.NoSideEffect(func() bool {
		_, serr := os.Stat(elsewhere)
		return os.IsNotExist(serr)
	}))
}

// covers: MA-736, MA-748, R37
// The Desktop says an encrypted archive's failures in the kind's fixed words: the
// Recent-backups card shows a history entry's kind, and a failed task's message
// names the archive and its kind, never the error's text. The positive twins: a
// plaintext entry and task keep their own text.
func TestTheDesktopSaysAnEncryptedArchivesFailuresByKind(t *testing.T) {
	plain := t.TempDir()
	if err := state.AppendRunHistory(plain, state.RunHistoryEntry{Status: state.RunFailed, Error: "the backup drive was not mounted"}); err != nil {
		t.Fatal(err)
	}
	if rows := recentRunRows(plain); len(rows) != 1 || rows[0].Detail != "the backup drive was not mounted" {
		t.Fatalf("the positive twin: a plaintext failure keeps its text: %+v", rows)
	}
	secret := errors.New("mailbox someone@example.org: SECRET server text")
	if got := jobErrorText(plain, secret); got != secret.Error() {
		t.Fatalf("the positive twin: a plaintext task keeps its text: %q", got)
	}

	sealed := vaultedArchive(t)
	if err := state.AppendRunHistory(sealed, state.RunHistoryEntry{Status: state.RunFailed, ErrorKind: state.FailMailboxFailed}); err != nil {
		t.Fatal(err)
	}
	if rows := recentRunRows(sealed); len(rows) != 1 || rows[0].Detail != state.FailureText(state.FailMailboxFailed) {
		t.Fatalf("an encrypted archive's failure must show its kind's words: %+v", rows)
	}
	// covers MA-748 too (F15): an encrypted archive's ok run is not "no new mail" —
	// its counts are sealed.
	if err := state.AppendRunHistory(sealed, state.RunHistoryEntry{Status: state.RunOK, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	if rows := recentRunRows(sealed); len(rows) != 2 || strings.Contains(rows[0].Detail+rows[1].Detail, "no new mail") {
		t.Fatalf("an encrypted archive's ok run must not read as no new mail: %+v", rows)
	}
	got := jobErrorText(sealed, secret)
	if !strings.Contains(got, sealed) || strings.Contains(got, "SECRET") || strings.Contains(got, "someone@") {
		t.Fatalf("an encrypted archive's task message carries the error's text: %q", got)
	}
}

// covers: MA-763, R37
// The Recent-backups card says an encrypted archive's backup that left its search
// index behind completed with some messages not yet searchable — a success, never a
// failure — and an ordinary success still says its counts are sealed.
func TestTheDesktopSaysABackupLeftTheIndexBehind(t *testing.T) {
	sealed := vaultedArchive(t)
	if err := state.AppendRunHistory(sealed, state.RunHistoryEntry{Status: state.RunOK, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendRunHistory(sealed, state.RunHistoryEntry{Status: state.RunOK, ErrorKind: state.NoteIndexBehind, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	rows := recentRunRows(sealed)
	if len(rows) != 2 || rows[0].Failed || rows[0].Status != "OK" || !strings.Contains(rows[0].Detail, "not searchable yet") {
		t.Fatalf("a backup that left its index behind must read as a success that says so: %+v", rows)
	}
	if !strings.Contains(rows[1].Detail, "sealed") {
		t.Fatalf("the positive twin: an ordinary success says its counts are sealed: %+v", rows[1])
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
	if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed || lr.ErrorKind != state.FailEncryptedNeedsKey || lr.Error != "" {
		t.Fatalf("the refused capture is not recorded as the minimal failed run: %+v (%v)", lr, rerr)
	}
	if h, herr := state.ReadRunHistory(sealed); herr != nil || len(h) != 1 || h[0].ErrorKind != state.FailEncryptedNeedsKey {
		t.Fatalf("the refused capture is not one history entry with its kind: %+v (%v)", h, herr)
	}
	if raw, _ := os.ReadFile(filepath.Join(sealed, state.LastRunName)); strings.Contains(string(raw), sealed) {
		t.Fatalf("the last-run record names the archive: %s", raw)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed), assure.NoSideEffect(func() bool {
		after, _ := os.ReadFile(filepath.Join(sealed, manifestName))
		_, uerr := os.Stat(filepath.Join(sealed, "units"))
		return string(after) == string(before) && os.IsNotExist(uerr)
	}))
}
