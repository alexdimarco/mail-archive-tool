package desktop

import (
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// rtbNode is the folder of the tree reply named name.
func rtbNode(t *testing.T, reply map[string]any, name string) map[string]any {
	t.Helper()
	l, _ := reply["folders"].([]any)
	for _, x := range l {
		if m, _ := x.(map[string]any); m != nil && m["name"] == name {
			return m
		}
	}
	t.Fatalf("no folder %q in the tree: %v", name, reply)
	return nil
}

// covers: MA-1131, R40
// The flow on the server (design §7.3, §6.4, §7.4, §7.5): the profiles by the server's own list
// (its ID, positions, the one pre-selected usable); the tree with each folder's restorable count
// and its subfolders' (§6.5); the plan of the folder chosen, every line in the engine's order —
// W-RECAPTURE first, W-WAIT last —; the run as the Desktop's task, its progress in Activity
// naming the folder (a plain archive), W-WAIT said for the whole run with Cancel offered, the
// post-write check said when it begins; then the result in the engine's words with the record to
// save (the plan and the result). Only the folder chosen, with its subfolder, is in Thunderbird.
func TestTheRestoreFlowRunsAsTheDesktopsTask(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Thunderbird")
	// The archive backs up mail from inside the first profile: it backs that profile up.
	out := rtbPlainArchive(t, filepath.Join(base, "work.default", "ImapMail", "imap.example.org"))
	e := newRtbEnv(t, out, base, 2)

	pr := e.profilesReply()
	ps, _ := pr["profiles"].([]any)
	assure.Reached(t, pr["ok"] == true && str(pr["listID"]) != "" && len(ps) == 2, "the server's list of the two profiles, by its ID")
	first, _ := ps[0].(map[string]any)
	assure.Reached(t, first["name"] == "work" && first["position"] == float64(0) && first["usable"] == true && first["backedUp"] == true,
		"each by its position and name, the one this archive backs up said")
	// §3.1 step 4 as folded (RT-10): the pre-selected one is the profile the archive does not back up.
	assure.Reached(t, pr["default"] == float64(1), "the profile pre-selected is the one the archive does not back up")

	// The person chooses the profile the archive backs up: W-RECAPTURE leads the plan.
	tree := e.post("/api/restore-tb/folders", map[string]any{"listID": pr["listID"], "position": 0})
	a, b, c := rtbNode(t, tree, rtbFolderA), rtbNode(t, tree, rtbFolderB), rtbNode(t, tree, rtbFolderC)
	assure.Reached(t, a["count"] == float64(2) && a["total"] == float64(3) && b["total"] == float64(1) && c["total"] == float64(1) && tree["total"] == float64(4),
		"each folder's restorable count, and with its subfolders")

	plan := e.post("/api/restore-tb/plan", map[string]any{"listID": pr["listID"], "position": 0, "whole": false, "folders": []string{str(a["id"])}})
	lines, _ := plan["lines"].([]any)
	assure.Reached(t, plan["ok"] == true && len(lines) > 3, "the plan of the folder chosen")
	code := func(i int) string { m, _ := lines[i].(map[string]any); return str(m["code"]) }
	words := func(i int) string { m, _ := lines[i].(map[string]any); return str(m["words"]) }
	assure.Reached(t, code(0) == "W-RECAPTURE" && code(len(lines)-1) == "W-WAIT" && words(len(lines)-1) == tbrestore.WaitWords(),
		"W-RECAPTURE first and W-WAIT last, as the engine orders them")

	started, release := holdRun(t)
	s := e.post("/api/restore-tb/start", map[string]any{"token": plan["token"]})
	assure.Reached(t, s["ok"] == true && s["wait"] == tbrestore.WaitWords() && str(s["job"]) != "", "the restore starts as the Desktop's task")
	<-started
	act := e.activity()
	r, _ := act["restore"].(map[string]any)
	assure.Reached(t, act["running"] == true && act["cancellable"] == true && r["running"] == true && r["wait"] == tbrestore.WaitWords() &&
		r["line"] == "Restoring: 1 of 3 messages ("+rtbFolderA+")" && act["job"] == s["job"],
		"while it runs: W-WAIT, the progress line naming the folder, and Cancel")
	close(release)
	act = e.waitDone()
	log := strings.Join(toLines(act["log"]), "\n")
	r, _ = act["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	record := str(res["record"])
	assure.Reached(t, res["done"] == true && strings.HasPrefix(str(res["words"]), "Restored 3 messages") && strings.Contains(log, restoreCheckingText) &&
		strings.Contains(record, "The plan:\n"+words(0)) && strings.Contains(record, "The result:\n"+str(res["words"])) &&
		strings.HasPrefix(str(res["recordName"]), "MailArchive restore record - Work mail - ") && res["recordNote"] == nil && r["wait"] == nil,
		"the result in the engine's words, the check said, the record of the plan and the result")
	sbd := filepath.Join(e.profiles[0].local, "Restored — Work mail.sbd")
	_, errA := os.Stat(filepath.Join(sbd, rtbFolderA))
	_, errB := os.Stat(filepath.Join(sbd, rtbFolderA+".sbd", rtbFolderB))
	_, errC := os.Stat(filepath.Join(sbd, rtbFolderC))
	assure.Reached(t, errA == nil && errB == nil && os.IsNotExist(errC), "only the folder chosen, with its subfolder, is in Thunderbird")
}

// covers: MA-1131, R40
// Cancel the restore (design §7.3 step 4, /api/job/cancel) stops it at its next safe point and
// its one cleanup removes what it wrote: Activity says it was cancelled in a restore's own words,
// the result says it was stopped and nothing was added, and Local Folders is as it was. Shut down
// stops one the same way, and its drain waits for that cleanup. Positive twin first: Cancel with
// nothing running is refused.
func TestCancelAndShutDownStopARestoreAndNothingIsAdded(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 1)
	n := e.post("/api/job/cancel", nil)
	assure.Refused(t, 1, str(n["error"]), assure.Code(1), assure.Names("Nothing is running"))
	before := rtbEntries(t, e.profiles[0].local)

	started, release := holdRun(t)
	tok, _ := e.planFor(0)["token"].(string)
	if s := e.post("/api/restore-tb/start", map[string]any{"token": tok}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	<-started
	cn := e.post("/api/job/cancel", nil)
	close(release)
	act := e.waitDone()
	log := strings.Join(toLines(act["log"]), "\n")
	r, _ := act["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Refused(t, 1, str(res["words"]), assure.Code(1), assure.Names("stopped", "nothing was added to Thunderbird"),
		assure.NoSideEffect(func() bool {
			return cn["ok"] == true && res["stopped"] == true && strings.Contains(log, restoreCancelLine) && !strings.Contains(log, "backup") &&
				rtbEntries(t, e.profiles[0].local) == before
		}))

	// Shut down.
	started2, release2 := holdRun(t)
	tok, _ = e.planFor(0)["token"].(string)
	if s := e.post("/api/restore-tb/start", map[string]any{"token": tok}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	<-started2
	e.stop()
	close(release2)
	act = e.waitDone()
	r, _ = act["restore"].(map[string]any)
	res, _ = r["result"].(map[string]any)
	assure.Refused(t, 1, str(res["words"]), assure.Code(1), assure.Names("stopped", "nothing was added to Thunderbird"),
		assure.NoSideEffect(func() bool { return res["stopped"] == true && rtbEntries(t, e.profiles[0].local) == before }))
}

// covers: MA-1131, X15, X11
// The card is served as the page script and the guide expect (design §7.3): the Restore to
// Thunderbird card on the current archive's page with its button and the panel's five steps,
// each opening with its own status line (role=status) for showStep; the page script brings each
// step in through showStep and puts what the server says on the page by textContent only — never
// a browser confirm() or prompt(), never markup, never the browser's storage. On a locked
// encrypted archive the button is disabled with its reason beside it. Positive twin first: an
// unlocked one's is not.
func TestTheRestoreCardIsServedWithItsSteps(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newRtbEnv(t, out, "", 1)
	e.unlock()
	page := html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	btn := regexp.MustCompile(`<button[^>]*id="rtbBtn"[^>]*>`)
	assure.Reached(t, strings.Contains(page, `id="sec-restore-tb"`) && !strings.Contains(btn.FindString(page), "disabled"), "the positive twin: the card, its button enabled")
	status := `<p class="card-sub" role="status" data-step-status hidden></p>`
	for _, id := range []string{"rtbProfileStep", "rtbWhatStep", "rtbPlanStep", "rtbRunStep", "rtbResultStep"} {
		if !strings.Contains(page, `<div id="`+id+`" hidden>`+status) {
			t.Errorf("%s does not open with its status line", id)
		}
		if !strings.Contains(restoreTBJS, "showStep($('"+id+"'),") {
			t.Errorf("the page script does not bring %s in through showStep", id)
		}
	}
	// Every id of the card the page script binds is on an archive's page (nav_test.go's
	// optionalJSIDs lists them as drawn only with a current archive).
	bound := 0
	for _, id := range jsBoundIDs() {
		if strings.HasPrefix(id, "rtb") {
			bound++
			if !strings.Contains(page, `id="`+id+`"`) {
				t.Errorf("dashboard.js binds #%s, which is not on an archive's page", id)
			}
		}
	}
	assure.Reached(t, bound, "the card's ids the page script binds")
	for _, bad := range []string{"confirm(", "prompt(", "innerHTML", "Storage", "insertAdjacentHTML"} {
		if strings.Contains(restoreTBJS, bad) {
			t.Errorf("the restore's page script uses %s", bad)
		}
	}
	e.cfg.Keys.lock(out)
	page = html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, strings.Contains(btn.FindString(page), "disabled") && strings.Contains(page, `<span class="card-sub" id="rtbWhy">`+restoreLockedWhy+`</span>`),
		"locked: the button disabled, its reason beside it")
}
