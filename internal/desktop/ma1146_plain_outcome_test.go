package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-1146, MA-1149, R18, S48, S29
// Recent backups tells each run as it ended (the owner's report of 2026-11-05: "Failed —
// partial: 4 item(s) not captured — see attachments-report.tsv — Thunderbird was already
// open"): a partial run is "Incomplete", in its kinds' words with the report named only as the
// details; a run that failed outright is still "Failed" with its reason; a run that finished
// says how many messages it added and, when some are not downloaded in full yet, that later
// backups add them. An entry an older version wrote (no kinds) keeps its own text. Positive
// twin first: the outright failure.
func TestRecentBackupsTellsEachRunAsItEnded(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 11, 5, 17, 4, 0, 0, time.Local)
	p := &state.PartialCounts{Mistake: state.PartialKind{Messages: 4}}
	for _, e := range []state.RunHistoryEntry{
		{When: at.Add(-3 * time.Hour), Status: state.RunFailed, Error: "partial: 1 item(s) not captured — see attachments-report.tsv"},
		{When: at.Add(-2 * time.Hour), Status: state.RunOK, Exported: 12, Fillable: 420},
		{When: at.Add(-time.Hour), Status: state.RunFailed, Error: "the archive drive is full"},
		{When: at, Status: state.RunFailed, Error: state.PartialText(*p, true), Partial: p, Presync: "already-running"},
	} {
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	rows := recentRunRows(out)
	if len(rows) != 4 {
		t.Fatalf("rows: %+v", rows)
	}
	if r := rows[1]; r.Status != "Failed" || !r.Failed || r.Detail != "the archive drive is full" {
		t.Fatalf("the positive twin: %+v", r)
	}
	r := rows[0]
	wantDetail := strings.Join(state.PartialSentences(*p), " ") + " " + state.PartialDetails
	if r.Status != "Incomplete" || !r.Failed || r.Detail != wantDetail || r.Sync != "Thunderbird was already open, so the backup left it alone" {
		t.Errorf("the partial run's row: %+v\nwant detail %q", r, wantDetail)
	}
	if ok := rows[2]; ok.Status != "OK" || ok.Detail != "12 messages · "+state.NotDownloadedSentence(420) {
		t.Errorf("the finished run's row: %+v", ok)
	}
	if old := rows[3]; old.Status != "Failed" || old.Detail != "partial: 1 item(s) not captured — see attachments-report.tsv" {
		t.Errorf("an older entry's row: %+v", old)
	}
	for _, row := range rows[:3] {
		assure.Reached(t, !strings.Contains(row.Detail, "partial:") && !strings.Contains(row.Detail, "item(s)"), "no bare code on a new row: "+row.Detail)
	}
}

// covers: MA-1146, R18, S48
// A partial backup in MailArchive Desktop's job slot (Run now, Archive now, a first backup) ends
// with its words as the result — "Not everything was saved. …" — never as an error the page
// prefixes with "Backup failed:" or "It didn't finish:" (owner question Q3, the recommendation
// built). Positive twin first: an outright failure is still the job's error.
func TestADesktopPartialBackupEndsWithItsWords(t *testing.T) {
	d := newDashboard(Config{Out: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	end := func(err error) map[string]any {
		t.Helper()
		if jerr := d.runJob(func(*log.Logger) (string, error) { return "", err }); jerr != nil {
			t.Fatal(jerr)
		}
		for i := 0; i < 500; i++ {
			rec := httptest.NewRecorder()
			d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
			var m map[string]any
			json.Unmarshal(rec.Body.Bytes(), &m)
			if m["running"] != true {
				return m
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("the job never ended")
		return nil
	}
	if m := end(errors.New("the archive drive is full")); str(m["error"]) != "the archive drive is full" || str(m["result"]) != "" {
		t.Fatalf("the positive twin: %v", m)
	}
	pe := &app.PartialError{Summary: app.GapSummary{ItemGaps: 4, Kinds: state.PartialCounts{Mistake: state.PartialKind{Messages: 4}}}}
	m := end(pe)
	assure.Reached(t, str(m["error"]) == "" && str(m["result"]) == pe.Error() && strings.HasPrefix(str(m["result"]), state.PartialLead),
		"the partial backup's words are its result")
}

// covers: MA-1147, R18, S29
// Run now while another backup of the archive is still running (the owner's report of
// 2026-11-05: the Scheduled backup card said "Backup failed: another backup or check is using
// this archive (started 20:00 …) … holding its lock file .mailarchive.lock"): the job ends as a
// skip, not a failure — /api/activity carries its words as "busy", with no error and no
// result, and the card's note is those words, never "Backup failed:" nor "Backup finished.";
// Thunderbird is not started and nothing is recorded. Positive twin first: with the archive
// free, Run now backs up and its note says so.
func TestRunNowMetByARunningBackupIsASkipNotAFailure(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	g.s.alreadyOpen = true // the positive twin needs no wait: Thunderbird is left alone
	g.start()
	if m := g.done(); str(m["error"]) != "" || m["busy"] != nil && str(m["busy"]) != "" || !strings.HasPrefix(str(m["note"]), "Backup finished:") {
		t.Fatalf("the positive twin: %v", m)
	}
	before, _, _ := state.ReadLastRun(g.out)

	held, err := lockfile.AcquireAs(filepath.Join(g.out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	g.start()
	m := g.done()
	held.Release()
	busy := str(m["busy"])
	if !strings.HasPrefix(busy, "Another backup of this archive is already running (started ") || !strings.Contains(busy, "this backup was skipped; nothing is wrong.") {
		t.Fatalf("the skip's words: %v", m)
	}
	if str(m["error"]) != "" || str(m["result"]) != "" || str(m["note"]) != busy {
		t.Errorf("a skip was told as a failure or a finish: error %q result %q note %q", m["error"], m["result"], m["note"])
	}
	after, _, _ := state.ReadLastRun(g.out)
	assure.Reached(t, after.Started.Equal(before.Started) && after.Status == before.Status, "the skip records nothing")
	if w, ok := app.BusySkipped(nil); ok || w != "" {
		t.Error("no error is no skip")
	}
}
