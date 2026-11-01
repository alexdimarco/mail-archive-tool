package desktop

import (
	"errors"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-975, R36
// A delete is refused while a backup of the archive runs (§7 busy row, C12): a job in the
// Desktop's slot on that archive (a backup, Run now, a verify), or another program's run
// holding its archive lock; the row says so too. Positive twin first: the same job on
// another archive does not refuse it.
func TestADeleteIsRefusedWhileABackupOfItRuns(t *testing.T) {
	e := newDeleteEnv(t)
	dir, other := e.plainArchive("Busy"), e.plainArchive("Elsewhere")
	c := e.d.cap
	c.mu.Lock()
	c.running, c.slotOut = true, other
	c.mu.Unlock()
	if p := e.plan(dir); p["ok"] != true {
		t.Fatalf("the positive twin: a job on another archive: %v", p)
	}
	c.mu.Lock()
	c.slotOut = dir
	c.mu.Unlock()
	before := treeState(t, dir)
	p := e.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("A backup of this archive is running. Try again when it ends."),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir), before) && p["token"] == nil }))
	c.mu.Lock()
	c.running, c.slotOut = false, ""
	c.mu.Unlock()

	// Another program's run: it holds the archive lock.
	l, err := lockfile.Acquire(filepath.Join(dir, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	p = e.plan(dir)
	l.Release()
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("A backup of this archive is running."),
		assure.NoSideEffect(func() bool { return p["token"] == nil }))
}

// rowFor is the Your archives row of path on the page: its data attributes and its text.
func rowFor(t *testing.T, page, path string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<li class="archive-row[^"]*"[^>]*data-path="` + regexp.QuoteMeta(html.EscapeString(path)) + `"[^>]*>.*?</li>`)
	m := re.FindString(page)
	if m == "" {
		t.Fatalf("no row for %s on the page", path)
	}
	return html.UnescapeString(m)
}

// covers: MA-975, R36, S41
// Each row says whether Delete is offered and why not, in §7's words, from §4's archive test
// and not from the row's greyed state (design-archive-delete §10, §7; C16, NEW-isarchive-cap):
// an archive is offered; an unreachable folder, a folder that no longer holds an archive and a
// Desktop started with -out naming the folder are not, each with its reason; the plan route
// refuses the -out folder in the same words, nothing changed. A delete not finished (§5.4)
// marks its row "Delete not finished: click Delete again" in place of Switch and Pause, and
// offers Delete again. Positive twin first: a reachable archive is offered.
func TestEachRowSaysWhetherDeleteIsOffered(t *testing.T) {
	e := newDeleteEnv(t)
	ok := e.plainArchive("Offered")
	gone := filepath.Join(t.TempDir(), "Unplugged")
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, "notes.txt"), []byte("mine"), 0o600)
	e.list(gone, plain)

	page := e.page()
	if row := rowFor(t, page, ok); !strings.Contains(row, `data-delete-ok="true"`) {
		t.Fatalf("an archive's row does not offer Delete: %s", row)
	}
	for _, c := range []struct{ path, why string }{
		{gone, "Reconnect the drive or folder first: nothing can be checked."},
		{plain, "This folder no longer holds an archive. Use Remove from list."},
	} {
		row := rowFor(t, page, c.path)
		if !strings.Contains(row, `data-delete-ok="false"`) || !strings.Contains(row, `data-delete-why="`+c.why+`"`) {
			t.Errorf("the row of %s: %s; want Delete not offered: %q", c.path, row, c.why)
		}
	}

	// A Desktop started with -out naming the folder.
	launched := e.plainArchive("Launched")
	e2 := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Out = launched })
	e2.list(launched, filepath.Join(t.TempDir(), "Another"))
	row := rowFor(t, e2.page(), launched)
	if !strings.Contains(row, `data-delete-ok="false"`) || !strings.Contains(row, "Restart MailArchive without -out, then delete.") {
		t.Errorf("the -out folder's row: %s", row)
	}
	before := treeState(t, launched)
	p := e2.plan(launched)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("Restart MailArchive without -out, then delete."),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, launched), before) && p["token"] == nil }))
	_, u := e2.post("/api/archive/unlist", map[string]any{"path": launched})
	assure.Refused(t, http.StatusOK, replyErr(u), assure.Code(http.StatusOK), assure.Names("Restart MailArchive without -out, then remove it from the list."),
		assure.NoSideEffect(func() bool { return e2.listed(launched) }))

	// A delete stopped at step 2 (the scheduler refused) is not finished: its row says so.
	stopped := e.plainArchive("Stopped")
	name := schedule.DefaultNameFor(stopped)
	e.sched.installed[name] = true
	e.sched.failOn[name] = errors.New("Access is denied")
	r := e.planAndDelete(stopped)
	if r["status"] != "not fully deleted" || r["unfinished"] != true {
		t.Fatalf("the stopped delete: %v", r)
	}
	row = rowFor(t, e.page(), stopped)
	if !strings.Contains(row, "data-delete-unfinished") || !strings.Contains(row, "Delete not finished: click Delete again") ||
		!strings.Contains(row, `data-delete-ok="true"`) || strings.Contains(row, "data-switch-archive") || strings.Contains(row, "data-pause-archive") {
		t.Errorf("the unfinished delete's row: %s", row)
	}
	// Delete again finishes it.
	delete(e.sched.failOn, name)
	if r := e.planAndDelete(stopped); r["status"] != "complete" {
		t.Errorf("Delete again: %v", r)
	}
}
