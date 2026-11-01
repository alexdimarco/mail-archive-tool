package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
)

// Slice 3b's adversarial pass and conformance review, the server's half (docs/design-archive-
// delete.md §3.1, §5.4, §8.1, §10; MA-986): the lone current archive's row, what a delete's reply
// tells the page, Dismiss of a result no longer held, a plan spent by a refusal, a delete under
// way told to a page drawn while it ran, the Archive now card of a delete not finished, and the
// panel at a narrow width.

// covers: MA-986, R36, S41
// The most common setup, one archive and it the current one, offers Delete… and Remove from list
// (§10: every row; del-s3b-A2-01, A3-01): Your archives stays hidden for it (MA-666), so its row
// sits on a card of its own, "This archive". A delete of it that stopped at step 2 says "Delete
// not finished: click Delete again" on the Archive now card — and Delete… is there to click
// (A3-02). Positive twin first: with two archives the rows are on Your archives and there is no
// This archive card.
func TestALoneCurrentArchiveOffersDeleteOnItsOwnCard(t *testing.T) {
	e := newDeleteEnv(t)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	only := e.plainArchive("Only")
	other := e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: only}); err != nil {
		t.Fatal(err)
	}
	page := e.page()
	if cardOf(page, "sec-archives") == "" || cardOf(page, "sec-this-archive") != "" {
		t.Fatal("the positive twin: two archives are not listed on Your archives alone")
	}

	// The other archive goes: the current one is alone.
	if r := e.planAndDelete(other); r["rowRemoved"] != true {
		t.Fatalf("the delete of the other archive: %v", r)
	}
	page = e.page()
	if cardOf(page, "sec-archives") != "" {
		t.Error("Your archives is drawn for a lone current archive (MA-666)")
	}
	card := cardOf(page, "sec-this-archive")
	row := rowFor(t, card, only)
	del, unl := rowButton(row, "data-delete-archive"), rowButton(row, "data-unlist-archive")
	if del == "" || strings.Contains(del, "disabled") || unl == "" || !strings.Contains(card, "<h2>This archive</h2>") {
		t.Errorf("the lone current archive's card does not offer Delete… and Remove from list: %s", card)
	}

	// Its delete stops at step 2: the page says to click Delete again, and Delete… is there.
	stopAt2(t, e, only)
	page = e.page()
	if !strings.Contains(page, `id="archiveNowUnfinished">Delete not finished: click Delete again</span>`) {
		t.Fatal("the stopped delete of the current archive is not said on the Archive now card")
	}
	row = rowFor(t, cardOf(page, "sec-this-archive"), only)
	if del = rowButton(row, "data-delete-archive"); del == "" || strings.Contains(del, "disabled") {
		t.Errorf("the page says to click Delete again with no Delete… to click: %s", row)
	}
}

// withoutM365 is a Desktop with no Microsoft 365 set up, so a local archive's Archive now card
// points at its Run now and Archive now never captures (C-7).
func withoutM365(e *wsEnv) { e.cfg.ConfigPath = filepath.Join(e.dir, "no-graph-config.json") }

// deleteDrawn plans path and deletes it with its name typed, from a page drawn for drawnFor.
func (e *delEnv) deleteDrawn(path, drawnFor string) map[string]any {
	e.t.Helper()
	p := e.plan(path)
	if p["ok"] != true {
		e.t.Fatalf("the plan of %s: %v", path, p)
	}
	_, r := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"], "drawnFor": drawnFor})
	return r
}

func strs(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, x := range a {
			s, _ := x.(string)
			out = append(out, s)
		}
	}
	return out
}

// covers: MA-986, R36, S41
// A delete's reply names every row of Your archives that is its folder (rows: its other
// spellings, as step 9 removes them and deletions.log marks them; del-s3b-A1-02, conformance gap
// 1) and what it means for the archive the page was drawn for (page): "unfinished" for a stop
// after step 2 began, "deleted" for a complete delete, and "" for another archive's delete and
// for one refused after its start, which removed nothing (A2-02). The page compares no paths.
// Positive twin first: another archive's delete leaves the page's archive alone.
func TestADeletesReplyNamesTheFoldersRowsAndThePagesArchive(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Twice")
	alias := cur + string(filepath.Separator) // the same folder, spelled with a trailing separator
	e.list(alias)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: alias}); err != nil {
		t.Fatal(err)
	}
	away := e.plainArchive("Away")
	r := e.deleteDrawn(away, alias)
	if r["status"] != archdelete.StatusComplete || r["page"] != "" || strings.Join(strs(r["rows"]), "|") != away {
		t.Fatalf("the positive twin: another archive's delete: page %v rows %v (%v)", r["page"], r["rows"], r["status"])
	}

	// A refusal after the start (a backup took the lock between the plan and the delete).
	p := e.plan(cur)
	l, err := lockfile.Acquire(filepath.Join(cur, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	_, r = e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"], "drawnFor": alias})
	l.Release()
	if r["status"] != archdelete.StatusRefusedAfterStart || r["id"] == "" || r["page"] != "" {
		t.Errorf("a delete of the page's archive refused after its start tells the page it reached it: page %q, status %v, id %v", r["page"], r["status"], r["id"])
	}

	// A stop at step 2: both spellings, and the page's archive not finished.
	name := schedule.DefaultNameFor(cur)
	e.sched.installed[name] = true
	e.sched.failOn[name] = errors.New("Access is denied")
	r = e.deleteDrawn(cur, alias)
	rows := strs(r["rows"])
	if r["unfinished"] != true || r["page"] != "unfinished" || len(rows) != 2 || rows[0] != cur || rows[1] != alias {
		t.Errorf("a stop at step 2 of the page's archive, listed twice: page %q rows %v unfinished %v", r["page"], rows, r["unfinished"])
	}

	// Delete again, complete: deleted.
	delete(e.sched.failOn, name)
	r = e.deleteDrawn(cur, alias)
	if r["status"] != archdelete.StatusComplete || r["page"] != "deleted" || r["rowRemoved"] != true {
		t.Errorf("the complete delete of the page's archive: page %q status %v", r["page"], r["status"])
	}
	if e.listed(cur) || e.listed(alias) {
		t.Errorf("a spelling of the deleted folder is still listed: %v", e.rows())
	}
}

// covers: MA-986, R36, S41
// Dismiss of a result no longer held (dismissed in another tab) says so with notHeld, so the page
// takes it away instead of holding its reloads for ever (del-s3b-A2-05); a refusal that spent the
// plan's one-use token says planUsed and how to go on, so the page does not offer a Delete
// permanently that can only fail (conformance gap 3), and one that kept it (a name that does not
// match) does not. Positive twins first: the first Dismiss closes it; a name that does not match
// keeps the plan.
func TestARepliesSayWhatThePageCanStillDo(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Dismissed")
	r := e.planAndDelete(dir)
	id, _ := r["id"].(string)
	if _, d := e.post("/api/archive/delete/dismiss", map[string]any{"id": id}); d["ok"] != true {
		t.Fatalf("the positive twin: the first Dismiss: %v", d)
	}
	_, d := e.post("/api/archive/delete/dismiss", map[string]any{"id": id})
	if d["ok"] != false || d["notHeld"] != true || replyErr(d) != notHeldText {
		t.Errorf("a second Dismiss is not told the result is no longer held: %v", d)
	}

	kept := e.plainArchive("Kept")
	p := e.plan(kept)
	_, m := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": "kept"})
	if m["ok"] != false || m["planUsed"] != nil {
		t.Fatalf("the positive twin: a name that does not match spends the plan: %v", m)
	}
	// deletions.log cannot be written: refused before the start, the token spent.
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(logPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(logPath, 0o400); err != nil {
		t.Fatal(err)
	}
	_, m = e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	if err := os.Chmod(logPath, 0o600); err != nil {
		t.Fatal(err)
	}
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("deletion log", "Click Delete… again"),
		assure.NoSideEffect(func() bool {
			return m["planUsed"] == true && m["id"] == "" && e.listed(kept) && archdelete.IsArchive(kept)
		}))
	_, m = e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	if m["planUsed"] != true || replyErr(m) != deleteNotWaiting {
		t.Errorf("a spent plan's token is not said to be spent: %v", m)
	}
}

// covers: MA-986, R36, S41
// A page drawn while a delete ran shows its row "Being deleted — its result shows on this page
// when the delete ends." and asks /api/activity until no delete is under way (deleting), then is
// drawn again with its result (del-s3b-A2-03). Positive twin first: with no delete under way,
// deleting is false.
func TestTheActivityRouteSaysWhileADeleteIsUnderWay(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Long")
	activity := func() map[string]any {
		var m map[string]any
		if err := json.Unmarshal(e.get("/api/activity").Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if a := activity(); a["deleting"] != false {
		t.Fatalf("the positive twin: no delete under way, deleting is %v", a["deleting"])
	}
	release, done := blockedDelete(t, e, dir)
	during, row := activity(), rowFor(t, e.page(), dir)
	close(release)
	<-done
	if during["deleting"] != true {
		t.Errorf("a delete under way is not said to /api/activity: %v", during["deleting"])
	}
	if !strings.Contains(row, "data-delete-running") || !strings.Contains(dashboardJS, "if(document.querySelector('[data-delete-running]'))setTimeout(delWatch,") {
		t.Errorf("a row being deleted is not watched by the page: %s", row)
	}
	if a := activity(); a["deleting"] != false {
		t.Errorf("the delete ended, yet deleting is %v", a["deleting"])
	}
}

// covers: MA-986, R36, S41
// A current local archive whose delete did not finish: the Archive now card says "Delete not
// finished: click Delete again" and no longer points at a Run now that is not there, nor at other
// ways to add mail into the half-deleted folder (§3.1, §5.4; del-s3b-A2-08). The delete panel and
// a result wrap a long folder path inside the row instead of widening the page at a narrow width
// (del-s3b-A3-05). Positive twin first: before the delete the card points at Run now.
func TestTheArchiveNowCardOfADeleteNotFinishedAndANarrowPage(t *testing.T) {
	e := newDeleteEnv(t, withoutM365)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	cur := e.plainArchive("Local")
	manualRunJob(t, cur)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	way := "To back it up now, click <b>Run now</b>"
	if page := e.page(); !strings.Contains(page, `id="archiveNowWay"`) || !strings.Contains(page, way) {
		t.Fatal("the positive twin: the local archive's Archive now card does not point at Run now")
	}
	stopAt2(t, e, cur)
	page := e.page()
	card := cardOf(page, "sec-archive")
	if !strings.Contains(card, `id="archiveNowUnfinished"`) {
		t.Fatalf("the Archive now card does not say the delete is not finished: %s", card)
	}
	for _, bad := range []string{`id="archiveNowWay"`, way, `id="archiveNowHand"`} {
		if strings.Contains(card, bad) {
			t.Errorf("the Archive now card of a delete not finished still shows %q", bad)
		}
	}

	for _, rule := range []string{".archive-row>.note{min-width:0}", ".delete-panel,.held-result,.unlist-ask{overflow-wrap:anywhere}"} {
		if !strings.Contains(page, rule) {
			t.Errorf("the page's style lacks %s: a long folder path in the panel widens the page", rule)
		}
	}
}
