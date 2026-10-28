package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

const captureAlerts = `{"alertEnabled":true,"alertOnFailure":true,"alertOnPause":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`

// recordSends installs a recording sender for the test and returns what it delivered.
func recordSends(t *testing.T) *[]alert.Event {
	t.Helper()
	var sent []alert.Event
	restore := backupalert.SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil })
	t.Cleanup(restore)
	return &sent
}

// failStreak marks the archive's alert streak failing, as an alerted failure leaves it.
func failStreak(t *testing.T, settings, out string) string {
	t.Helper()
	p := backupalert.StreakPath(settings, backupalert.KeyFor(out))
	if err := os.WriteFile(p, []byte(`{"version":1,"failing":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// covers: MA-870, R37
// The scheduled capture keeps the alert streak beside the Desktop's settings file, keyed by the
// archive folder it backs up into: its early failure exit (Microsoft 365 not set up) goes through
// it, so two failed runs in a row send one email. Positive twin first: the first one sends.
func TestTheCaptureEmailsOncePerFailingStreak(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := os.WriteFile(settings, []byte(captureAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	sent := recordSends(t)
	cfg := Config{ConfigPath: filepath.Join(dir, "graph.json"), SettingsPath: settings, Out: out}
	if err := HeadlessCapture(cfg, io.Discard); err == nil || len(*sent) != 1 || (*sent)[0].Result != alert.ResultFailed {
		t.Fatalf("the first failure: err=%v sent %+v", err, *sent)
	}
	if _, err := os.Stat(backupalert.StreakPath(settings, backupalert.KeyFor(out))); err != nil {
		t.Fatalf("the capture's streak is not keyed by its archive folder: %v", err)
	}
	if err := HeadlessCapture(cfg, io.Discard); err == nil || len(*sent) != 1 {
		t.Fatalf("the second failure in a row: sent %d emails, want 1", len(*sent))
	}
}

// covers: MA-859, R37
// The scheduled capture, like the local job, skips quietly while another holder has the archive
// lock and started under a day ago: no error, the line names the holder, and no email, alert
// streak or run history entry. Positive twin first: with the lock free it captures.
func TestTheCaptureSkipsQuietlyWhileAnotherHolderRuns(t *testing.T) {
	srv := newFakeM365(t)
	dir, out := t.TempDir(), t.TempDir()
	cfgPath, tok := filepath.Join(dir, "graph-config.json"), filepath.Join(dir, "tok.json")
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := os.WriteFile(settings, []byte(captureAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ConfigPath: cfgPath, SettingsPath: settings, TokenCachePath: tok, Out: out,
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	sent := recordSends(t)
	if err := HeadlessCapture(cfg, io.Discard); err != nil || countHTML(out) == 0 {
		t.Fatalf("the positive twin: %v", err)
	}
	hist, _ := state.ReadRunHistory(out)
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	var logged bytes.Buffer
	if err := HeadlessCapture(cfg, &logged); err != nil {
		t.Fatalf("a capture while a backup holds the archive: %v, want a quiet skip", err)
	}
	if !strings.Contains(logged.String(), "skipped: an earlier backup of this archive started at") {
		t.Errorf("the skip's line does not name the holder: %q", logged.String())
	}
	if len(*sent) != 0 {
		t.Errorf("the quiet skip sent %+v", *sent)
	}
	if _, err := os.Lstat(backupalert.StreakPath(settings, backupalert.KeyFor(out))); !os.IsNotExist(err) {
		t.Error("the quiet skip wrote an alert streak")
	}
	if h, _ := state.ReadRunHistory(out); len(h) != len(hist) {
		t.Errorf("the quiet skip added a run history entry: %d -> %d", len(hist), len(h))
	}
}

// covers: MA-870, R37
// A schedule's create and its Remove clear the archive's alert streak, so remove, re-install, then
// fail sends: the capture's create, the local backup's create, and Remove each leave no failing
// streak behind. Positive twin first: an edit of an installed capture keeps the streak.
func TestCreateAndRemoveClearTheAlertStreak(t *testing.T) {
	e := newWsEnv(t)
	fake := &backupFake{tasks: map[string]bool{}}
	useBackupFake(e, fake)
	seamCaptureInstall(e, fake)
	if _, body := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "04:00"}); body["ok"] != true {
		t.Fatalf("create: %v", body)
	}
	p := failStreak(t, e.cfg.SettingsPath, e.out)
	if _, body := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "04:00"}); body["ok"] != true {
		t.Fatalf("edit: %v", body)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("the positive twin: an edit cleared the failing streak")
	}
	if _, body := e.post("/api/schedule-remove", map[string]any{}); body["ok"] != true {
		t.Fatalf("remove: %v", body)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatal("Remove left the failing streak behind")
	}
	failStreak(t, e.cfg.SettingsPath, e.out)
	if _, body := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "04:00"}); body["ok"] != true {
		t.Fatalf("re-install: %v", body)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatal("the capture's create left the failing streak behind")
	}

	// The local backup's create.
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	lp := failStreak(t, g.settings, out)
	if body := g.post(`{"interval":"daily","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("local create: %s", body)
	}
	if _, err := os.Lstat(lp); !os.IsNotExist(err) {
		t.Fatal("the local backup's create left the failing streak behind")
	}
}
