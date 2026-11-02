package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1045, R36
// A scheduled Microsoft 365 capture that starts while a delete of its folder is still RUNNING (the
// delete holds the archive's lock) skips quietly, as a capture that meets the lock did before
// MA-1044 (the G2 settle pass's LOW-1): no error, no sign-in, nothing written into the folder (not
// its log), and the capture log beside the settings says "This scheduled backup was skipped: a
// delete of this archive started at …", never "click Delete again". Positive twin first: with the
// lock released (the delete stopped part way) the same capture is refused in MA-1044's words.
func TestAScheduledCaptureWhileADeleteRunsSkipsQuietly(t *testing.T) {
	fake := newFakeM365(t)
	target, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	signedIn := false
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me" {
			signedIn = true
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsLog := filepath.Join(dir, "desktop-capture.log")
	logPath := filepath.Join(dir, archdelete.LogName) // this Desktop's (-config) deletion log
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	cfg := Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "desktop-settings.json"), TokenCachePath: tok, Out: out,
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	if err := HeadlessCapture(cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	ma1044Started(t, logPath, out)
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	captureLog := filepath.Join(out, "mailarchive-capture.log")

	// Positive twin: the delete stopped part way.
	before := ma1044Files(t, out)
	signedIn = false
	err = scheduledCapture(cfg, captureLog, settingsLog, io.Discard)
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, out)) && !signedIn }))

	// The delete is running.
	held, lerr := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "delete")
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer held.Release()
	logged := read(settingsLog)
	err = scheduledCapture(cfg, captureLog, settingsLog, io.Discard)
	added := strings.TrimPrefix(read(settingsLog), logged)
	if err != nil || signedIn || !ma1044SameFiles(before, ma1044Files(t, out)) ||
		!strings.Contains(added, "This scheduled backup was skipped: a delete of this archive started at") || strings.Contains(added, "Delete again") {
		t.Fatalf("a scheduled capture while a delete runs must skip quietly: err %v, signed in %v, folder unchanged %v, logged %q",
			err, signedIn, ma1044SameFiles(before, ma1044Files(t, out)), added)
	}
}
