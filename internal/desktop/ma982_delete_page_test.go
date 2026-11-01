package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
)

// The page's half of Delete and Remove from list (docs/design-archive-delete.md §3.1, §3.2, §8,
// §8.1, §10, slice 3b): the row buttons as the Overview draws them, and what a delete not
// finished takes off the page.

// rowButton is the row's button marked attr (data-delete-archive, data-unlist-archive): its
// opening tag and label, "" when the row has none.
func rowButton(row, attr string) string {
	i := strings.Index(row, attr)
	if i < 0 {
		return ""
	}
	s := strings.LastIndex(row[:i], "<button")
	e := strings.Index(row[i:], "</button>")
	if s < 0 || e < 0 {
		return ""
	}
	return row[s : i+e+len("</button>")]
}

// covers: MA-982, R36, S41
// Every row of Your archives offers Remove from list (secondary) and Delete… (danger) beside
// Switch and Pause, each naming the row's path (design-archive-delete §10). Delete…'s enabled
// state is the row's DeleteOK, never its greyed state: disabled with §7's words as its title
// when the plan would refuse. A row being deleted offers neither; a delete not finished keeps
// both, "Delete not finished: click Delete again" in place of Switch and Pause. The row's state
// sits in one row-state element, which the page replaces when a result marks the row not
// finished. Positive twin first: an archive's row offers both, beside Switch and Pause.
func TestEachRowOffersRemoveFromListAndDelete(t *testing.T) {
	e := newDeleteEnv(t)
	ok := e.plainArchive("Offered")
	gone := filepath.Join(t.TempDir(), "Unplugged")
	e.list(gone)

	page := e.page()
	row := rowFor(t, page, ok)
	del, unl := rowButton(row, "data-delete-archive"), rowButton(row, "data-unlist-archive")
	if del == "" || unl == "" || !strings.Contains(row, "data-switch-archive") || !strings.Contains(row, "data-pause-archive") {
		t.Fatalf("the positive twin: an archive's row does not offer Remove from list and Delete… beside Switch and Pause: %s", row)
	}
	if !strings.Contains(del, `class="btn danger"`) || !strings.HasSuffix(del, ">Delete…</button>") || strings.Contains(del, "disabled") ||
		!strings.Contains(del, `data-path="`+ok+`"`) || !strings.Contains(del, `type="button"`) {
		t.Errorf("Delete… is not an enabled danger button naming the row: %s", del)
	}
	if !strings.Contains(unl, `class="btn secondary"`) || !strings.HasSuffix(unl, ">Remove from list</button>") || strings.Contains(unl, "disabled") ||
		!strings.Contains(unl, `data-path="`+ok+`"`) {
		t.Errorf("Remove from list is not an enabled secondary button naming the row: %s", unl)
	}
	if !strings.Contains(row, `<span class="row-state">`) || !strings.Contains(row[strings.Index(row, `<span class="row-state">`):], "data-switch-archive") {
		t.Errorf("the row's Switch and Pause are not in its row-state element: %s", row)
	}

	// An unplugged row: Delete… disabled, its title §7's words; Remove from list offered.
	row = rowFor(t, page, gone)
	del, unl = rowButton(row, "data-delete-archive"), rowButton(row, "data-unlist-archive")
	if !strings.Contains(del, " disabled") || !strings.Contains(del, `title="Reconnect the drive or folder first: nothing can be checked."`) {
		t.Errorf("an unplugged row's Delete… is not disabled with its reason: %s", del)
	}
	if unl == "" || strings.Contains(unl, "disabled") {
		t.Errorf("an unplugged row does not offer Remove from list: %s", row)
	}

	// A row being deleted: neither.
	running := e.plainArchive("Running")
	release, done := blockedDelete(t, e, running)
	row = rowFor(t, e.page(), running)
	close(release)
	<-done
	if rowButton(row, "data-delete-archive") != "" || rowButton(row, "data-unlist-archive") != "" {
		t.Errorf("a row being deleted offers Delete… or Remove from list: %s", row)
	}

	// A delete not finished: both kept, the tag in place of Switch and Pause.
	e.sched.onRemove = nil
	stopped := e.plainArchive("Stopped")
	stopAt2(t, e, stopped)
	row = rowFor(t, e.page(), stopped)
	del = rowButton(row, "data-delete-archive")
	if del == "" || strings.Contains(del, "disabled") || rowButton(row, "data-unlist-archive") == "" ||
		strings.Contains(row, "data-switch-archive") || strings.Contains(row, "data-pause-archive") ||
		!strings.Contains(row, `<span class="row-state"><span class="archive-tag">Delete not finished: click Delete again</span>`) {
		t.Errorf("a delete not finished does not keep Delete… and Remove from list with its tag in place of Switch and Pause: %s", row)
	}
}

// stopAt2 deletes dir through the routes with its scheduled backup's removal refused, so the
// delete stops at step 2 (§5.3) and is not finished (§5.4).
func stopAt2(t *testing.T, e *delEnv, dir string) {
	t.Helper()
	name := schedule.DefaultNameFor(dir)
	e.sched.installed[name] = true
	e.sched.failOn[name] = errors.New("Access is denied")
	if r := e.planAndDelete(dir); r["unfinished"] != true || r["id"] == "" {
		t.Fatalf("the stopped delete of %s: %v", dir, r)
	}
}

// manualRunJob makes out's backup "Only when I click Run now" (MA-949) on this computer: its job
// file and the archive's record, so the Scheduled backup card offers Run now.
func manualRunJob(t *testing.T, out string) {
	t.Helper()
	jp := plantRunJob(t, out)
	host, _ := os.Hostname()
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{Version: 1, Name: schedule.DefaultNameFor(out), Interval: string(schedule.Manual),
		Job: []string{"-job", jp}, InstalledAt: time.Now(), Host: host}); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-982, R36, S41
// A delete of the current archive that stopped at step 2 (before step 4 cleared the archive in
// use) is a delete not finished (§5.4): the page offers neither Run now nor Archive now for it —
// the cards say "Delete not finished: click Delete again" in their place and the sidebar links
// go (§3.1 "Where the result sits", R4:NF-R4-ROW-KEPT-BUTTONS). Positive twins first: the same
// current archive before its delete shows Run now and its sidebar link, and another current
// archive with no Run now shows the sidebar's Archive now.
func TestACurrentArchivesUnfinishedDeleteOffersNoRunNowOrArchiveNow(t *testing.T) {
	e := newDeleteEnv(t)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	stopAtStep2 := func(dir string) {
		stopAt2(t, e, dir)
		if s := LoadSettings(e.cfg.SettingsPath); s.Out != dir {
			t.Fatalf("a step-2 stop changed the archive in use: %q", s.Out)
		}
	}

	runNow := e.plainArchive("With Run now")
	manualRunJob(t, runNow)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: runNow}); err != nil {
		t.Fatal(err)
	}
	page := e.page()
	if !strings.Contains(page, `id="runBackupBtn"`) || !strings.Contains(page, `id="navRunNow"`) || strings.Contains(page, `id="runNowUnfinished"`) {
		t.Fatal("the positive twin: the current archive before its delete does not offer Run now")
	}
	stopAtStep2(runNow)
	page = e.page()
	if strings.Contains(page, `id="runBackupBtn"`) || strings.Contains(page, `id="navRunNow"`) || strings.Contains(page, `id="navArchive"`) {
		t.Error("the current archive's delete is not finished, yet Run now or Archive now is offered")
	}
	for _, id := range []string{"runNowUnfinished", "archiveNowUnfinished"} {
		if !strings.Contains(page, `id="`+id+`">Delete not finished: click Delete again</span>`) {
			t.Errorf("the page does not say %s's \"Delete not finished: click Delete again\" in its place", id)
		}
	}

	plain := e.plainArchive("Without Run now")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: plain}); err != nil {
		t.Fatal(err)
	}
	if page = e.page(); !strings.Contains(page, `id="navArchive"`) || strings.Contains(page, `id="archiveNowUnfinished"`) {
		t.Fatal("the positive twin: a current archive with no Run now does not offer the sidebar's Archive now")
	}
	stopAtStep2(plain)
	if page = e.page(); strings.Contains(page, `id="navArchive"`) || strings.Contains(page, `id="archiveBtn"`) ||
		!strings.Contains(page, `id="archiveNowUnfinished">Delete not finished: click Delete again</span>`) {
		t.Error("the current archive's delete is not finished, yet Archive now is offered")
	}
}
