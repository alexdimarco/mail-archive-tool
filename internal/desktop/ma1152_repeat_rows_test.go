package desktop

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1152, R18, S29
// Recent backups tells runs in a row that ended in the same words once, with how often and when
// the last did (the 1.6.1 friction review's P2: two identical 3-line Incomplete rows, and with
// hourly backups a page of them): the row starts at the first of them and says "3 times, last at
// 5:04 PM" (with the date when the last was on another day); a run that ended otherwise between
// them keeps them apart; ten rows are still shown when the history holds more. Each count reads
// in plain words ("1 message", "1,240 messages"). Positive twin first: runs that ended
// differently are a row each.
func TestRecentBackupsTellsRepeatedRunsOnce(t *testing.T) {
	out := t.TempDir()
	writeManifest(t, out)
	at := time.Date(2026, 11, 5, 17, 4, 0, 0, time.Local)
	p := &state.PartialCounts{ArchiveWrite: state.PartialKind{Folders: 1}}
	partial := state.RunHistoryEntry{Status: state.RunFailed, Error: state.PartialText(*p, true), Partial: p}
	add := func(e state.RunHistoryEntry, when time.Time) {
		t.Helper()
		e.When = when
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	add(state.RunHistoryEntry{Status: state.RunOK, Exported: 1}, at.Add(-26*time.Hour))
	add(state.RunHistoryEntry{Status: state.RunOK, Exported: 1240}, at.Add(-25*time.Hour))
	if rows := recentRunRows(out); len(rows) != 2 || rows[0].Repeat != "" || rows[0].Detail != "1,240 messages" || rows[1].Detail != "1 message" {
		t.Fatalf("the positive twin: %+v", rows)
	}
	add(partial, at.Add(-24*time.Hour)) // yesterday
	add(partial, at.Add(-2*time.Hour))
	add(state.RunHistoryEntry{Status: state.RunFailed, Error: "the archive drive is full"}, at.Add(-90*time.Minute))
	add(partial, at.Add(-time.Hour))
	add(partial, at.Add(-30*time.Minute))
	add(partial, at)
	rows := recentRunRows(out)
	if len(rows) != 5 {
		t.Fatalf("%d rows, want 5: %+v", len(rows), rows)
	}
	newest := rows[0]
	assure.Reached(t, newest.Status == "Incomplete" && newest.Repeat == "3 times, last at 5:04 PM" && newest.When == at.Add(-time.Hour).Format("Jan 2, 2006 3:04 PM"),
		"the newest three are one row from the first of them")
	if r := rows[1]; r.Detail != "the archive drive is full" || r.Repeat != "" {
		t.Errorf("the failure between them: %+v", r)
	}
	if r := rows[2]; r.Repeat != "2 times, last at "+at.Add(-2*time.Hour).Format("Jan 2, 2006 3:04 PM") || r.When != at.Add(-24*time.Hour).Format("Jan 2, 2006 3:04 PM") {
		t.Errorf("two in a row across days: %+v", r)
	}

	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	page := cfg.overview("csrf-token")
	if strings.Count(page, "Incomplete</span> (3 times, last at 5:04 PM) — ") != 1 || strings.Count(page, "could not be written to the archive") != 2 {
		t.Errorf("the card does not tell the repeated runs once")
	}

	// Runs that ended in the same minute say only how often: "last at" would repeat the row's
	// own time (the polish-161 walk, N1).
	quick := t.TempDir()
	for _, s := range []int{5, 40} {
		e := partial
		e.When = at.Add(time.Duration(s) * time.Second)
		if err := state.AppendRunHistory(quick, e); err != nil {
			t.Fatal(err)
		}
	}
	if rows := recentRunRows(quick); len(rows) != 1 || rows[0].Repeat != "2 times" {
		t.Errorf("two runs in one minute: %+v", rows)
	}

	// Ten rows still, when the history holds more runs than that.
	many := t.TempDir()
	for i := 0; i < 30; i++ {
		e := state.RunHistoryEntry{Status: state.RunOK, Exported: 1 + i%2*4} // two kinds of row in turn
		e.When = at.Add(time.Duration(i) * time.Minute)
		if err := state.AppendRunHistory(many, e); err != nil {
			t.Fatal(err)
		}
	}
	if rows := recentRunRows(many); len(rows) != 10 {
		t.Errorf("%d rows of alternating runs, want 10", len(rows))
	}
}
