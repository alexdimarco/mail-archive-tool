package desktop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// The scheduled capture's fixes from slice 2's adversarial pass (design-archive-delete §7;
// findings del-s2-A3-01, A2-03).

func ma956DesktopNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range entries {
		if e.Name() != ".mailarchive.lock" { // the run's own lock file: the known leftover (lockfile Release)
			n = append(n, e.Name())
		}
	}
	slices.Sort(n)
	return n
}

// covers: MA-956, R36
// A scheduled capture the emptied-folder guard refuses leaves a trace without email alerts
// (del-s2-A3-01): its log, when the schedule keeps it outside the refused folder (after
// "Save and keep" moved the archive), says why; when the log is in the refused folder,
// nothing is written there and the refusal goes to desktop-capture.log beside the Desktop's
// settings. Positive twin first: a log in a folder that is not refused is opened as given.
func TestMA956_RefusedScheduledCaptureLeavesATrace(t *testing.T) {
	env := &launchEnv{Stderr: io.Discard}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsLog := filepath.Join(dir, "desktop-capture.log")
	kept := t.TempDir()
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	// Twin: an empty folder is not refused; its log is opened where the schedule names it.
	twin := t.TempDir()
	_ = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", twin, "-log", filepath.Join(twin, "c.log")}, env)
	assure.Reached(t, slices.Contains(ma956DesktopNames(t, twin), "c.log"), "the twin's log in its folder")

	// The log outside the refused folder: it says why.
	elsewhere := filepath.Join(t.TempDir(), "mailarchive-capture.log")
	err := launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", kept, "-log", elsewhere}, env)
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, read(elsewhere), assure.Code(1), assure.Names("FAILED:", kept, "holds no MailArchive archive"),
		assure.NoSideEffect(func() bool { return slices.Equal(ma956DesktopNames(t, kept), []string{"notes.txt"}) }))

	// The log in the refused folder: nothing written there, the refusal beside the settings.
	err = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", kept, "-log", filepath.Join(kept, "mailarchive-capture.log")}, env)
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, read(settingsLog), assure.Code(1), assure.Names("FAILED:", kept, "holds no MailArchive archive"),
		assure.Forbid("notes.txt"),
		assure.NoSideEffect(func() bool { return slices.Equal(ma956DesktopNames(t, kept), []string{"notes.txt"}) }))

	// The same with the archive's earlier capture log still there (a delete could not remove
	// it): it is not written to.
	earlier := filepath.Join(kept, "mailarchive-capture.log")
	if err := os.WriteFile(earlier, []byte("an earlier capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = launch(context.Background(), []string{"-capture", "-config", cfgPath, "-out", kept, "-log", earlier}, env)
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names(kept, "holds no MailArchive archive"),
		assure.NoSideEffect(func() bool { return read(earlier) == "an earlier capture\n" }))
}

// covers: MA-956, R36
// A signed-in scheduled capture with its log in the archive folder, whose archive is deleted
// during its sign-in (the delete leaves only the log the capture holds open, Windows): it is
// refused under its lock (RunGraph saw the archive before it signed in), the log it made is
// closed and removed, and the refusal goes to desktop-capture.log beside the settings
// (del-s2-A2-03). Positive twin first: a capture left alone keeps its log there.
func TestMA956_ScheduledCaptureTakesBackItsLog(t *testing.T) {
	fake := newFakeM365(t)
	target, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	var onMe func()
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me" && onMe != nil {
			onMe()
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsLog := filepath.Join(dir, "desktop-capture.log")
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "desktop-settings.json"), TokenCachePath: tok, Out: out,
			BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	}

	twin := t.TempDir()
	if err := scheduledCapture(cfg(twin), filepath.Join(twin, "mailarchive-capture.log"), settingsLog, io.Discard); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	assure.Reached(t, slices.Contains(ma956DesktopNames(t, twin), "mailarchive-capture.log"), "the twin's log kept")

	kept := t.TempDir()
	if err := HeadlessCapture(cfg(kept), io.Discard); err != nil {
		t.Fatal(err)
	}
	onMe = func() {
		for _, n := range ma956DesktopNames(t, kept) {
			if n != "mailarchive-capture.log" {
				_ = os.RemoveAll(filepath.Join(kept, n))
			}
		}
	}
	err = scheduledCapture(cfg(kept), filepath.Join(kept, "mailarchive-capture.log"), settingsLog, io.Discard)
	rc := 0
	if err != nil {
		rc = 1
	}
	b, _ := os.ReadFile(settingsLog)
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names(kept, "held a MailArchive archive when this backup started"),
		assure.NoSideEffect(func() bool { return len(ma956DesktopNames(t, kept)) == 0 }))
	if !strings.Contains(string(b), "FAILED:") || !strings.Contains(string(b), "held a MailArchive archive") {
		t.Errorf("the refusal must be logged beside the settings, got %q", b)
	}

	// The log outside the folder (after "Save and keep"): the refusal under the lock is in it.
	moved := t.TempDir()
	if err := HeadlessCapture(cfg(moved), io.Discard); err != nil {
		t.Fatal(err)
	}
	onMe = func() {
		for _, n := range ma956DesktopNames(t, moved) {
			_ = os.RemoveAll(filepath.Join(moved, n))
		}
	}
	elsewhere := filepath.Join(t.TempDir(), "mailarchive-capture.log")
	err = scheduledCapture(cfg(moved), elsewhere, settingsLog, io.Discard)
	rc = 0
	if err != nil {
		rc = 1
	}
	got, _ := os.ReadFile(elsewhere)
	assure.Refused(t, rc, string(got), assure.Code(1), assure.Names("FAILED:", moved, "held a MailArchive archive when this backup started"),
		assure.NoSideEffect(func() bool { return len(ma956DesktopNames(t, moved)) == 0 }))

	// Deleted between the log's opening and the capture's start, leaving only the log (the
	// capture holds it open, Windows): the capture's own checks find no archive then, but
	// scheduledCapture saw one before it opened the log, so it is refused under the lock
	// (slice 2's re-verification, item 1).
	onMe = nil
	between := t.TempDir()
	if err := HeadlessCapture(cfg(between), io.Discard); err != nil {
		t.Fatal(err)
	}
	real := startCapture
	t.Cleanup(func() { startCapture = real })
	startCapture = func(c Config, w io.Writer) error {
		for _, n := range ma956DesktopNames(t, between) {
			if n != "mailarchive-capture.log" {
				_ = os.RemoveAll(filepath.Join(between, n))
			}
		}
		return real(c, w)
	}
	err = scheduledCapture(cfg(between), filepath.Join(between, "mailarchive-capture.log"), settingsLog, io.Discard)
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names(between, "held a MailArchive archive when this backup started"),
		assure.NoSideEffect(func() bool { return len(ma956DesktopNames(t, between)) == 0 }))
}
