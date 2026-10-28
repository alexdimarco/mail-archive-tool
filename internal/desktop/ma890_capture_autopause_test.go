package desktop

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
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
