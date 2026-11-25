package desktop

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// rowOf is the HTML of the Your-archives row for path on page, "" when there is none.
func rowOf(page, path string) string {
	for _, li := range strings.Split(page, `<li class="archive-row`)[1:] {
		if strings.Contains(li, `data-path="`+html.EscapeString(path)+`"`) {
			if i := strings.Index(li, "</li>"); i >= 0 {
				return li[:i]
			}
			return li
		}
	}
	return ""
}

// covers: MA-1505, S90
// How a set's archives are doing (docs/design-per-mailbox-archives.md §10, R1; revision 2.1's
// F-10b): a set makes none of its archives current, and the health view covers only the current
// archive, so each row in Your archives says, from the last-run record it already reads for its
// Thunderbird line, "Last backup did not finish (DATE): switch to it to see why." when its last
// backup did not end ok. Positive twin first: a row whose last backup ended ok says nothing new. A
// failed and a cancelled backup say the line with the day it ended ("Nov 24"; with its year when not
// this year); a record that says a backup is running says nothing new (one may be in progress); the
// current archive's row says nothing new (its health view says why). A planted record's status and
// error text never reach the page, only its date, and through the escaping template (X15).
func TestARowWhoseLastBackupDidNotFinishSaysSo(t *testing.T) {
	now := time.Now()
	nov24 := time.Date(now.Year(), time.November, 24, 9, 30, 0, 0, time.Local)
	dir := t.TempDir()
	record := func(name string, lr state.LastRun) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, p)
		if err := state.WriteLastRun(p, lr); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := record("ok", state.LastRun{Status: state.RunOK, Started: nov24.Add(-time.Minute), Finished: nov24})
	failed := record("failed", state.LastRun{Status: state.RunFailed, Started: nov24.Add(-time.Minute), Finished: nov24, Error: "disk full"})
	cancelled := record("cancelled", state.LastRun{Status: state.RunCancelled, Started: nov24.Add(-time.Minute), Finished: nov24})
	running := record("running", state.LastRun{Status: state.RunRunning, Started: nov24})
	lastYear := record("last year", state.LastRun{Status: state.RunFailed, Started: nov24.AddDate(-1, 0, 0), Finished: nov24.AddDate(-1, 0, 0)})
	current := record("current", state.LastRun{Status: state.RunFailed, Started: nov24.Add(-time.Minute), Finished: nov24})
	planted := record("planted", state.LastRun{Status: `<img src=x onerror=alert(1)>`, Started: nov24.Add(-time.Minute), Finished: nov24, Error: `<script>alert(2)</script>`})

	idxPath := filepath.Join(dir, archindex.Name)
	var entries []archindex.Entry
	for _, p := range []string{ok, failed, cancelled, running, lastYear, current, planted} {
		entries = append(entries, archindex.Entry{Path: p, AddedAt: at0, LastSeenAt: at0})
	}
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: entries}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "s.json")
	if err := SaveSettings(settings, Settings{Out: current}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}
	page := assure.Reached(t, getOverview(t, cfg), "the page")
	const line = "Last backup did not finish"

	// Positive twin: a backup that ended ok says nothing new.
	if r := assure.Reached(t, rowOf(page, ok), "the ok archive's row"); strings.Contains(r, line) {
		t.Fatalf("positive twin: a row whose last backup ended ok says %q:\n%s", line, r)
	}
	want := "Last backup did not finish (Nov 24): switch to it to see why."
	for _, p := range []string{failed, cancelled, planted} {
		if r := rowOf(page, p); !strings.Contains(r, want) {
			t.Errorf("%s: the row does not say %q:\n%s", filepath.Base(p), want, r)
		}
	}
	if want := fmt.Sprintf("Last backup did not finish (Nov 24, %d): switch to it to see why.", nov24.Year()-1); !strings.Contains(rowOf(page, lastYear), want) {
		t.Errorf("a backup last year: want %q in\n%s", want, rowOf(page, lastYear))
	}
	if r := assure.Reached(t, rowOf(page, running), "the running archive's row"); strings.Contains(r, line) {
		t.Errorf("a record that says a backup is running says %q:\n%s", line, r)
	}
	if r := assure.Reached(t, rowOf(page, current), "the current archive's row"); strings.Contains(r, line) {
		t.Errorf("the current archive's row says %q (its health view says why):\n%s", line, r)
	}
	for _, bad := range []string{"<img", "onerror", "<script>alert", "alert(", "disk full"} {
		if strings.Contains(page, bad) {
			t.Errorf("a record's own text reached the page: %q", bad)
		}
	}
	if n := strings.Count(page, line); n != 4 {
		t.Errorf("%d rows say the line, want 4 (failed, cancelled, last year, planted)", n)
	}
}
