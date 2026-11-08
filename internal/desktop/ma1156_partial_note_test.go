package desktop

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// waitJobEnd reads /api/activity until the job in the slot has ended.
func waitJobEnd(t *testing.T, d *dashboard) map[string]any {
	t.Helper()
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

// covers: MA-1156, R18, S29
// MailArchive Desktop's Run now note after a backup that did not save everything keeps the
// not-downloaded sentence the run said after its words (the 1.6.1 friction review's P6): the job
// ends with all of it as its result, and the note puts the Thunderbird sync's line after the
// first line, then the sentence. Positive twin first: a partial run with nothing left to download
// ends with its words alone.
func TestARunNowNoteAfterAPartialRunKeepsTheNotDownloadedSentence(t *testing.T) {
	d := newDashboard(Config{Out: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	pe := &app.PartialError{Summary: app.GapSummary{FolderGaps: 1, Kinds: state.PartialCounts{ArchiveWrite: state.PartialKind{Folders: 1}}}}
	end := func(err error) string {
		t.Helper()
		if jerr := d.runJob(func(*log.Logger) (string, error) { return "", err }); jerr != nil {
			t.Fatal(jerr)
		}
		m := waitJobEnd(t, d)
		if str(m["error"]) != "" {
			t.Fatalf("a partial run ended as an error: %v", m)
		}
		return str(m["result"])
	}
	if got := end(pe); got != pe.Error() {
		t.Fatalf("the positive twin: %q", got)
	}
	sentence := state.NotDownloadedSentence(2, state.FromThunderbird)
	got := end(fmt.Errorf("%w\n%s", pe, sentence))
	note := runNowEndNote("Thunderbird synced first.", "ok", got, "")
	assure.Reached(t, got == pe.Error()+"\n"+sentence && note == pe.Error()+"\nThunderbird synced first.\n"+sentence,
		"the note keeps the sentence after the sync's line: "+note)
}
