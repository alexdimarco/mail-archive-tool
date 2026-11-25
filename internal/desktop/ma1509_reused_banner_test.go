package desktop

import (
	"html"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-1509, MA-692, R18
// The Your-archives banner says the reused reason's words (docs/design-per-mailbox-archives.md
// revision 3, INS-03), the words status says (health.SourceReasonWordsAt, one truth, X10), in place of
// "reconnect the source". Positive twin first: a source-lost archive whose record keeps no reason
// shows today's banner.
func TestTheBannerSaysTheReusedWords(t *testing.T) {
	first := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	archive := func(reason string) string {
		t.Helper()
		dir := t.TempDir()
		writeManifest(t, dir)
		if err := state.WriteSourceStreak(dir, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: first, LastUnreachable: first.Add(state.SurfaceAfter), LastKind: "local-source-unreachable"}); err != nil {
			t.Fatal(err)
		}
		if err := state.WriteLastRun(dir, state.LastRun{Status: state.RunFailed, Started: first, Finished: first, Error: "x", SourceReason: reason}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	plain, reused := archive(""), archive(state.SourceReasonReused)
	entries := []archindex.Entry{{Path: plain}, {Path: reused}}
	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: entries}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idxPath, GOOS: "darwin"}))
	page = html.UnescapeString(page)
	if !strings.Contains(page, "This archive's source hasn't been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups.") {
		t.Fatal("positive twin: the row with no reason shows today's banner")
	}
	want := "Thunderbird now uses this archive's mail folder for a different account (since Oct 12, 2026), so its backups stopped. The mail saved so far stays here. To back up that account, click Make a new archive… and choose it; to stop this archive's backups, click Pause (make read-only)."
	if want != health.SourceReasonWordsAt(reused) || !strings.Contains(page, `<span class="archive-tag">`+want+`</span>`) {
		t.Errorf("the banner does not say, alone in its tag, status's words:\n%s", want)
	}
	if n := strings.Count(page, "reconnect the source"); n != 1 {
		t.Errorf("only the row with no reason says reconnect the source: %d times", n)
	}
}
