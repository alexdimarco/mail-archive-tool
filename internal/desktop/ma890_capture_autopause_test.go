package desktop

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/state"
)

// covers: MA-890, R37
// The scheduled capture never sends an auto-pause email for a pause that did not happen (the part
// A pass): with the source streak already due and no schedule record of its own to stop, a
// capture that fails for a reason that says nothing about the source (no saved sign-in) leaves the
// streak as it was and sends its failure email, and no auto-pause email. Before, it reported
// "paused" with nothing removed, and every run sent the auto-pause email again. Positive twin
// first: the failure email goes.
func TestTheCaptureSendsNoAutoPauseForAPauseThatDidNotHappen(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := os.WriteFile(settings, []byte(captureAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	then := time.Now().UTC().Add(-31 * 24 * time.Hour)
	due := state.SourceStreak{ConsecutiveFails: 5, FirstUnreachable: then, LastUnreachable: then.Add(30*24*time.Hour + time.Hour)}
	if err := state.WriteSourceStreak(out, due); err != nil {
		t.Fatal(err)
	}
	sent := recordSends(t)
	cfg := Config{ConfigPath: cfgPath, SettingsPath: settings, TokenCachePath: filepath.Join(dir, "no-sign-in.json"), Out: out}
	if err := HeadlessCapture(cfg, io.Discard); err == nil {
		t.Fatal("fixture: a capture with no saved sign-in did not fail")
	}
	if s, _ := state.ReadSourceStreak(out); s.ConsecutiveFails != due.ConsecutiveFails {
		t.Fatalf("fixture: the failure moved the source streak (%d -> %d), so it is not inconclusive", due.ConsecutiveFails, s.ConsecutiveFails)
	}
	var results []string
	for _, ev := range *sent {
		results = append(results, ev.Result)
	}
	if len(results) == 0 || results[0] != alert.ResultFailed {
		t.Fatalf("the positive twin: the failure email did not go: %v", results)
	}
	if strings.Contains(strings.Join(results, ","), "paused") {
		t.Errorf("an auto-pause email went for a pause that did not happen: %v", results)
	}
}

// newOutageM365 is a Microsoft 365 that lists 30 messages in the Inbox and is down for the rest:
// every download answers 503 with no wait, and so does the breaker's control request, so the
// capture trips the breaker and ends as an outage (the source unreachable).
func newOutageM365(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/me/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		var vs []string
		for i := 1; i <= 30; i++ {
			vs = append(vs, fmt.Sprintf(`{"id":"M%d","internetMessageId":"<m%d@x>","subject":"s","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}`, i, i))
		}
		j(w, `{"value":[`+strings.Join(vs, ",")+`]}`)
	})
	mux.HandleFunc("/me/mailFolders/F_IN", graphtest.Status(503, "0")) // the control request
	mux.HandleFunc("/me/messages/", graphtest.Status(503, "0"))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// covers: MA-890, R37
// The scheduled capture hands auto-pause its own run's verdict (the part A re-verification,
// 2026-10-28): true after a run that found Microsoft 365 unreachable, false after a failure that
// says nothing about the source (no saved sign-in), so an old streak never pauses on the second.
// Positive twin first: the outage run hands true.
func TestTheCaptureHandsAutoPauseItsOwnVerdict(t *testing.T) {
	var got []bool
	orig := autoPauseIfDue
	autoPauseIfDue = func(_ string, unreachable bool) (bool, error) { got = append(got, unreachable); return false, nil }
	t.Cleanup(func() { autoPauseIfDue = orig })

	srv := newOutageM365(t)
	dir, out := t.TempDir(), t.TempDir()
	cfgPath, tok := filepath.Join(dir, "graph-config.json"), filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "desktop-settings.json"), TokenCachePath: tok, Out: out,
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	if err := HeadlessCapture(cfg, io.Discard); err == nil || !app.FoundSourceUnreachable(err) {
		t.Fatalf("fixture: the outage run did not end with the source unreachable: %v", err)
	}
	if len(got) != 1 || !got[0] {
		t.Fatalf("the positive twin: an outage run handed auto-pause %v, want [true]", got)
	}

	cfg.TokenCachePath = filepath.Join(dir, "no-sign-in.json")
	if err := HeadlessCapture(cfg, io.Discard); err == nil || app.FoundSourceUnreachable(err) {
		t.Fatalf("fixture: the run with no saved sign-in: %v", err)
	}
	if len(got) != 2 || got[1] {
		t.Errorf("a run with no saved sign-in handed auto-pause %v, want [true false]", got)
	}
}
