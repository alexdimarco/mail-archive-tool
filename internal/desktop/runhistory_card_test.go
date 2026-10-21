package desktop

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-701
// The Recent backups card shows the archive's run history (newest first) with each
// run's result and detail, and offers the Diagnostics nav entry.
func TestMA701_RecentBackupsCard(t *testing.T) {
	out := t.TempDir()
	writeManifest(t, out)
	if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: time.Now(), Status: state.RunOK, Exported: 1240}); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: time.Now(), Status: state.RunFailed, Error: "the backup drive was not mounted"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	page := cfg.overview("csrf-token")

	for _, want := range []string{`id="sec-history"`, `href="#sec-history"`, "Recent backups", "1240 messages", "the backup drive was not mounted", "Failed"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Recent backups card is missing %q", want)
		}
	}
	// The failed run is the most recent, so it renders before the ok one.
	if strings.Index(page, "the backup drive was not mounted") > strings.Index(page, "1240 messages") {
		t.Fatal("recent runs should render newest-first (the failure before the earlier success)")
	}
}
