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

// covers: MA-1506, MA-692, R18
// The Your-archives banner says a Thunderbird account's stop in its reason's words (docs/design-per-
// mailbox-archives.md §11, revision 2.1's F-03), the words status says (health.SourceReasonWordsAt,
// one truth, X10), in place of "reconnect the source" (the template's first branch, before the
// Repair and Run-now forms). Positive twin first: a source-lost archive whose record keeps no reason
// shows today's banner.
func TestTheBannerSaysTheReasonsWords(t *testing.T) {
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
	plain, moved, removed := archive(""), archive(state.SourceReasonMoved), archive(state.SourceReasonRemoved)
	entries := []archindex.Entry{{Path: plain}, {Path: moved}, {Path: removed}}
	rows := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(archindex.Index{Entries: entries})
	byPath := map[string]ArchiveRow{}
	for _, r := range rows {
		byPath[r.Path] = r
	}
	if r := byPath[plain]; r.SourceLostSince == "" || r.SourceLostWords != "" {
		t.Fatalf("positive twin: a record with no reason keeps today's banner: %+v", r)
	}
	for _, dir := range []string{moved, removed} {
		if r := byPath[dir]; r.SourceLostSince == "" || r.SourceLostWords == "" || r.SourceLostWords != health.SourceReasonWordsAt(dir) {
			t.Errorf("%s: the row's words %q, want status's %q", dir, r.SourceLostWords, health.SourceReasonWordsAt(dir))
		}
	}

	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: entries}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idxPath, GOOS: "darwin"}))
	page = html.UnescapeString(page)
	if !strings.Contains(page, "This archive's source hasn't been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups.") {
		t.Fatal("positive twin: the row with no reason shows today's banner")
	}
	for _, want := range []string{
		// Amended 2026-11-25 (the friction review's F12; nothing weakened): "… and choose it again;
		// then click Pause …" before; the run's own second way is now said too.
		"Thunderbird stopped using this archive's mail folder on Oct 12, 2026: its account now keeps its mail in another folder. The mail saved so far stays here. To back that account up, click Make a new archive… and choose it again (or choose One archive for each mailbox and the same folder as before); then click Pause (make read-only) on this archive.",
		"Thunderbird no longer has the account this archive backs up (since Oct 12, 2026). The mail saved so far stays here. To stop its backups, click Pause (make read-only).",
	} {
		if !strings.Contains(page, `<span class="archive-tag">`+want+`</span>`) {
			t.Errorf("the banner does not say, alone in its tag:\n%s", want)
		}
	}
	if n := strings.Count(page, "reconnect the source"); n != 1 {
		t.Errorf("only the row with no reason says reconnect the source: %d times", n)
	}
}
