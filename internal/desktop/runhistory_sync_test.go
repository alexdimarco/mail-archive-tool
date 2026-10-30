package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// covers: MA-905, MA-925, X10
// The Recent backups card tags each plaintext run with its Thunderbird sync, in the
// closed map's phrase only (design-presync-attended §4.5, slice AS7; C-28, C-37,
// C-16/T12, C-68, OQ-6): positive twin first, the plaintext archive's rows carry the
// phrase for their recorded name — the no-change phrase, quit-no-window, start-blocked
// and wait-ended included — and the page shows it; a run with no recorded sync and a
// cancelled sync get no tag; an unknown or hostile name (control characters, markup)
// reads "Thunderbird not synced" and is never echoed onto the page; and an encrypted
// archive's rows have no tag even when a presync name is planted in its history,
// judged by the archive's kind, not by the entry.
func TestTheRecentBackupsCardTagsTheSync(t *testing.T) {
	out := t.TempDir()
	writeManifest(t, out)
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	names := []string{"", "ok", "ok-noactivity", "watch-truncated-noactivity", "quit-no-window", "start-blocked", "wait-ended", "cancelled", "\x1b[31mrm -rf <b>evil</b>"}
	for i, n := range names {
		if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: at.Add(time.Duration(i) * time.Minute), Status: state.RunOK, Exported: 1, Presync: n}); err != nil {
			t.Fatal(err)
		}
	}
	rows := recentRunRows(out)
	if len(rows) != len(names) {
		t.Fatalf("%d rows, want %d", len(rows), len(names))
	}
	for i, n := range names {
		r := rows[len(names)-1-i] // newest first
		want := presync.TagPhrase(strings.Map(func(c rune) rune {
			if c < 0x20 || c == 0x7f {
				return -1
			}
			return c
		}, n))
		if r.Sync != want {
			t.Errorf("the row for %q has tag %q, want %q", n, r.Sync, want)
		}
	}
	if rows[len(names)-2].Sync != "Thunderbird synced first" {
		t.Fatalf("positive twin: the ok row's tag is %q", rows[len(names)-2].Sync)
	}
	// §4.5's words, row by row, for the names this slice names.
	for i, want := range map[int]string{
		2: "Thunderbird started, but nothing arrived in 20 min", 3: "Thunderbird started, but nothing arrived in 20 min",
		4: "Thunderbird synced, but was left open", 5: "Thunderbird not synced: it could not be started",
		6: "Thunderbird sync stopped early, as you asked",
	} {
		if got := rows[len(names)-1-i].Sync; got != want {
			t.Errorf("the %s row's tag is %q, want %q", names[i], got, want)
		}
	}
	if rows[len(names)-1].Sync != "" || rows[1].Sync != "" {
		t.Errorf("no recorded sync and a cancelled sync must have no tag: %q, %q", rows[len(names)-1].Sync, rows[1].Sync)
	}
	if rows[0].Sync != "Thunderbird not synced" {
		t.Errorf("a hostile name must read as the generic phrase, got %q", rows[0].Sync)
	}

	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	page := cfg.overview("csrf-token")
	for _, want := range []string{"Thunderbird synced first", "Thunderbird started, but nothing arrived in 20 min",
		"Thunderbird synced, but was left open", "Thunderbird not synced: it could not be started",
		"Thunderbird sync stopped early, as you asked", "Thunderbird not synced"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Recent backups card does not show %q", want)
		}
	}
	for _, never := range []string{"evil", "rm -rf", "\x1b", "quit-no-window", "wait-ended"} {
		if strings.Contains(page, never) {
			t.Errorf("the page shows %q", never)
		}
	}

	sealed := vaultedArchive(t)
	planted := `[{"when":"2026-10-30T09:00:00Z","status":"ok","presync":"ok"},{"when":"2026-10-30T10:00:00Z","status":"ok","presync":"owner-active-skipped","encrypted":true}]`
	if err := os.WriteFile(filepath.Join(sealed, state.RunHistoryName), []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	srows := recentRunRows(sealed)
	if len(srows) != 2 {
		t.Fatalf("the planted encrypted history gives %d rows, want 2", len(srows))
	}
	for _, r := range srows {
		if r.Sync != "" {
			t.Errorf("an encrypted archive's row is tagged %q", r.Sync)
		}
	}
}
