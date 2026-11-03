package desktop

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/wizard"
)

// covers: MA-1063, R36
// The scheduled Microsoft 365 capture that finds its archive folder absent (the backup drive not
// plugged in) leaves the count of captures refused for a delete this computer never began as it
// was — at its early check and after the engine's own refusal of the absent folder — as the
// source-loss streak leaves itself on a run that says nothing about the source
// (docs/design-archive-delete.md §15.10 slice 6's "the same rule"; the slice-6 and G3 adversarial
// pass's MED-1). Refused captures 40 days apart with a drive-away capture between them still pause on
// the capture after the drive comes back. Positive twin first: the same 40 days with no absent
// capture between pauses.
//
// A marker that appears during the sign-in (after the early check, before the lock) is refused by
// the engine under its lock, and that refusal still counts (MA-1058's "a delete met under the lock
// counts too", which the gate on the engine's refusal keeps).
func TestMA1063_AnAbsentFolderLeavesTheCapturesDeleteCount(t *testing.T) {
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
	settingsPath := filepath.Join(dir, "desktop-settings.json")
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(captureAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: settingsPath, TokenCachePath: tok, Out: out,
			BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	}
	installed := map[string]bool{}
	var removed []string
	t.Cleanup(wizard.SetSchedulerForTest(func(n string) schedule.State {
		if installed[n] {
			return schedule.Installed
		}
		return schedule.NotInstalled
	}, func(s schedule.Spec) error { removed = append(removed, s.Name); delete(installed, s.Name); return nil }))
	sent := recordSends(t)
	// archive makes a captured archive folder holding a marker no log here knows, its capture's
	// schedule running, and a count whose first refused capture was 40 days ago.
	archive := func() (out, countPath string) {
		out = filepath.Join(t.TempDir(), "archive")
		if err := os.Mkdir(out, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := HeadlessCapture(cfg(out), io.Discard); err != nil {
			t.Fatal(err)
		}
		ma1048Plant(t, out)
		installed[schedule.DefaultNameFor(out)] = true
		countPath = backupalert.DeleteRefusedPath(settingsPath, backupalert.KeyFor(out))
		then := time.Now().UTC().Add(-40 * 24 * time.Hour)
		b, _ := json.Marshal(state.SourceStreak{Version: state.SourceStreakVersion, ConsecutiveFails: 1, FirstUnreachable: then, LastUnreachable: then})
		if err := os.WriteFile(countPath, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return out, countPath
	}
	pausedFor := func(out string) bool {
		for _, ev := range *sent {
			if ev.Result == alert.ResultPaused && ev.Archive == out && ev.Error == wizard.AutoPauseDeleteWhy {
				return true
			}
		}
		return false
	}

	// Positive twin: a refused capture 40 days after the first stops the capture's schedule.
	twin, _ := archive()
	_ = HeadlessCapture(cfg(twin), io.Discard)
	if len(removed) != 1 || removed[0] != schedule.DefaultNameFor(twin) || !pausedFor(twin) {
		t.Fatalf("positive twin: 40 days of refused captures did not pause: removed %v", removed)
	}
	removed = nil

	out, countPath := archive()
	seeded, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	away := out + "-unplugged"
	if err := os.Rename(out, away); err != nil {
		t.Fatal(err)
	}
	cerr := HeadlessCapture(cfg(out), io.Discard)
	rc := 0
	if cerr != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(cerr), assure.Code(1), assure.Names("is not present", out),
		assure.NoSideEffect(func() bool {
			now, rerr := os.ReadFile(countPath)
			return rerr == nil && bytes.Equal(now, seeded) && len(removed) == 0
		}))

	// The drive is back, the marker still there: the refused capture spans 40 days and pauses.
	if err := os.Rename(away, out); err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	_ = HeadlessCapture(cfg(out), &logged)
	if len(removed) != 1 || removed[0] != schedule.DefaultNameFor(out) {
		t.Fatalf("the refused capture after the drive came back did not stop the schedule: removed %v", removed)
	}
	assure.Reached(t, pausedFor(out), "the auto-pause email")
	assure.Reached(t, strings.Contains(logged.String(), "auto-paused the scheduled backup: "+wizard.AutoPauseDeleteWhy), "the pause told in the capture log")

	// A marker that appears during the sign-in: the early check found the folder there with no
	// delete (the old count ends), the engine refuses it under its lock, and that refusal counts.
	raced, racedCount := archive()
	if err := os.Remove(filepath.Join(raced, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	onMe = func() { ma1048Plant(t, raced) }
	cerr = HeadlessCapture(cfg(raced), io.Discard)
	onMe = nil
	if app.FailureKind(cerr) != state.FailDeleteUnfinished {
		t.Fatalf("the marker planted during the sign-in was not refused under the lock: %v", cerr)
	}
	var s state.SourceStreak
	b, rerr := os.ReadFile(racedCount)
	if rerr != nil || json.Unmarshal(b, &s) != nil {
		t.Fatalf("the refusal under the lock was not counted: %v", rerr)
	}
	assure.Reached(t, s.ConsecutiveFails == 1 && time.Since(s.FirstUnreachable) < time.Hour, "the refusal under the lock counted as a new count's first run")
}
