package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// ma1322Stamp rewrites the archive's manifest's version stamp (8: another computer tidying it).
func ma1322Stamp(t *testing.T, out, v string) {
	t.Helper()
	mp := filepath.Join(out, ".mailarchive-manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"version":\s*\d+`)
	if err := os.WriteFile(mp, []byte(re.ReplaceAllString(string(b), `"version": `+v)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1322, R45, R37
// The scheduled Microsoft 365 backup meets another computer's tidy-up as the scheduled local
// backup does (the B3 adversarial pass, round 2's G4): a skip, not a failed run. It returns no
// error, says "Skipped, not failed:" and the tidy-up words in its log, puts the archive's record
// back as it was (no failure, no history entry), sends no email on its first days, and keeps when
// the wait began, so `status` is the tidy-up WARN with "since DATE", never RED. Once the wait has
// lasted 3 days one email goes, worded as a wait, and only one; the first capture after the tidy-up
// ends the wait and sends the recovery. Positive twin first: a capture of a readable archive
// keeps no wait and sends nothing.
func TestTheScheduledCaptureMeetingATidyUpIsASkip(t *testing.T) {
	isolateHome(t)
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
	if err := HeadlessCapture(cfg, io.Discard); err != nil || countHTML(out) == 0 || len(*sent) != 0 {
		t.Fatalf("positive twin: %v, sent %+v", err, *sent)
	}
	if _, ok := gmailanswer.ReadTidyWait(out); ok {
		t.Fatal("positive twin: an ordinary capture kept a tidy-up wait")
	}

	ma1322Stamp(t, out, "8")
	before, _ := os.ReadFile(filepath.Join(out, state.LastRunName))
	hist, _ := state.ReadRunHistory(out)
	var logged bytes.Buffer
	if err := HeadlessCapture(cfg, &logged); err != nil {
		t.Fatalf("a capture meeting the tidy-up: %v, want a skip", err)
	}
	if !strings.Contains(logged.String(), "Skipped, not failed: "+state.TidyUpWords) || strings.Contains(logged.String(), "FAILED") {
		t.Fatalf("the capture's log: %q", logged.String())
	}
	if after, _ := os.ReadFile(filepath.Join(out, state.LastRunName)); string(after) != string(before) {
		t.Fatal("the skipped capture wrote a record into the archive")
	}
	if h, _ := state.ReadRunHistory(out); len(h) != len(hist) {
		t.Fatalf("the skipped capture added a history entry: %d -> %d", len(hist), len(h))
	}
	if len(*sent) != 0 {
		t.Fatalf("the first day of the wait sent %+v", *sent)
	}
	w, ok := gmailanswer.ReadTidyWait(out)
	if !ok {
		t.Fatal("the capture kept no wait")
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	if rep.Posture == "RED" || !slices.Contains(rep.Codes, "manifest_tidying") || !strings.Contains(strings.Join(rep.Reasons, "\n"), state.TidyWaitSinceLine(w.Since)) {
		t.Fatalf("status: %s %v\n%s", rep.Posture, rep.Codes, strings.Join(rep.Reasons, "\n"))
	}

	// Three days on: one email, worded as a wait, however many captures wait.
	if err := gmailanswer.ClearTidyWait(out); err != nil {
		t.Fatal(err)
	}
	if _, err := gmailanswer.NoteTidyWait(out, time.Now().Add(-gmailanswer.TidyWaitAlertAfter-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := HeadlessCapture(cfg, io.Discard); err != nil {
			t.Fatalf("a waiting capture after 3 days: %v", err)
		}
	}
	if len(*sent) != 1 || (*sent)[0].Result != alert.ResultFailed || !(*sent)[0].Tidying {
		t.Fatalf("after 3 days: sent %+v; want one email worded as a wait", *sent)
	}

	// The tidy-up finished: the capture goes through, the wait ends, the recovery is sent.
	ma1322Stamp(t, out, "6")
	if err := HeadlessCapture(cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 2 || (*sent)[1].Result != alert.ResultRecovered {
		t.Fatalf("the capture after the tidy-up: sent %+v", *sent)
	}
	if _, ok := gmailanswer.ReadTidyWait(out); ok {
		t.Fatal("the wait is kept after a capture went through")
	}
}
