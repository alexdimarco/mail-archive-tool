package desktop

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// More of the friction review's fixes, through the dashboard's handlers.

// covers: MA-438, MA-446, R12, S52
// F21: R-k names the other window ("another window started a new preview") besides the age
// and the change; the History card of a running export says it is writing, not "fingerprint
// not recorded at export time" and "N not attempted". F18: after Start succeeds the page does
// not ask "Leave site?". F16: R-a says the command-line tool is a separate download, R-a2
// opens with a plain sentence for someone who does not manage the tenant. The positive twin
// first: Start of a live preview works.
func TestFricSmallWordingAndTheLeaveSitePrompt(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()

	// a preview that another window replaced
	first, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	second, _ := e.preview(e.selection(entry(ids["Inbox"], false)))
	code, out := e.post("/api/exports/start", map[string]any{"previewId": first})
	refusedBody(t, code, out, "R-k", "out of date", "older than 30 minutes", "another window started a new preview", "Preview copy export again")

	// the live one starts; its card says it is writing
	e.slow("F-INBOX", 80*time.Millisecond)
	run := e.ok("/api/exports/start", map[string]any{"previewId": second})["runId"].(string)
	e.waitDone(1)
	c := e.card(run)
	if c["running"] != true || c["fingerprintNote"] != nil {
		t.Errorf("the running card: %v", c)
	}
	if !strings.Contains(exportsJS, "Writing now") || !strings.Contains(exportsJS, "c.running") {
		t.Error("exports.js still shows a running card's counts as 'not attempted'")
	}
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	e.waitRun()

	// F18: Start does not leave the selection armed for the browser's prompt
	i, j := strings.Index(workspaceJS, "st.leaving=true"), strings.Index(workspaceJS, "location.href='exports'")
	if i < 0 || j < 0 || i > j || !strings.Contains(workspaceJS, "if(st.leaving)return;") {
		t.Error("workspace.js navigates to Export activity with the 'Leave site?' prompt still armed")
	}

	// F16
	a := refusalA()
	assure.Refused(t, a.Status, a.Text, assure.Code(statusRefused), assure.Names("isn't set up yet", "tenant and application ID", "mailarchive setup", "separate download"))
	a2 := refusalA2()
	assure.Refused(t, a2.Status, a2.Text, assure.Code(statusRefused),
		assure.Names("This installation can't sign in as you", "app-only access", "device sign-in", "Allow public client flows", "Delegated Mail.Read", "Whoever manages"))
	if !strings.Contains(workspaceJS, "separate download") {
		t.Error("the workspace's own 'not set up' line still sends the Desktop owner to a command they do not have")
	}
}

// covers: MA-431, MA-446, R12, S52
// F11: an export held back because a capture or the weekly backup has the archive says so on
// its Running card (progress carries the reason), says nothing once it moves, and still
// finishes; Cancel works while it waits. The positive twin first: without a capture, no reason.
func TestFricAnExportHeldBackByACaptureSaysWhy(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	var busy atomic.Bool
	e.d.ex.env.SetCaptureBusy(func() bool { return busy.Load() })

	run := e.startExport(e.selection(entry(ids["Sent Items"], false)))
	if p := e.waitRun(); p["waiting"] != nil && p["waiting"] != "" {
		t.Errorf("an export beside no capture says it is waiting: %v", p["waiting"])
	}
	_ = run

	busy.Store(true)
	run = e.startExport(e.selection(entry(ids["Inbox"], false)))
	var p map[string]any
	for i := 0; i < 300; i++ {
		_, p = e.post("/api/exports/progress", nil)
		if w, _ := p["waiting"].(string); w != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w, _ := p["waiting"].(string)
	if p["running"] != true || p["done"] != float64(0) || !strings.Contains(w, "a backup or Archive now is using the archive") || !strings.Contains(w, "carries on by itself") {
		t.Fatalf("the paused export's progress: %v", p)
	}
	byID(t, e.get("/exports").Body.String(), "runWaiting")
	if !strings.Contains(exportsJS, "p.waiting") {
		t.Error("the Running card does not show the reason")
	}
	busy.Store(false)
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(7) {
		t.Errorf("the export after the capture ended: %v", last)
	}
	_, p = e.post("/api/exports/progress", nil)
	if w, _ := p["waiting"].(string); w != "" {
		t.Errorf("still waiting after the capture ended: %v", w)
	}

	// Cancel works while it waits
	busy.Store(true)
	run = e.startExport(e.selection(entry(ids["Sent Items"], false)))
	for i := 0; i < 300; i++ {
		_, p = e.post("/api/exports/progress", nil)
		if w, _ := p["waiting"].(string); w != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "cancelled" {
		t.Errorf("Cancel while waiting: %v", l)
	}
}

// covers: MA-437, MA-446, MA-448, R12, S52
// F7: the card lists the messages that were not exported (subject, folder, reason) under
// "Which ones?"; F8: a stopped run's card keeps what it says about why it stopped and what
// to do after a restart, when the reconciler's label replaces the run's own; F13: the interrupted
// card says what to click next; F12: Check this export of a run that was never finished says so
// and what to do. The positive twin first: a run with no failures lists none.
func TestFricCardsListFailuresAndKeepTheStopsWords(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	clean := e.startExport(e.selection(entry(ids["Sent Items"], false)))
	e.waitRun()
	if c := e.card(clean); c["failures"] != nil || c["next"] != nil {
		t.Fatalf("a clean card: %v", c)
	}

	e.failWith("F-INBOX-0002", 400)
	e.failWith("F-INBOX-0005", 400)
	bad := e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitRun()
	c := e.card(bad)
	fl, _ := c["failures"].([]any)
	if len(fl) != 2 {
		t.Fatalf("the card's failures: %v", c["failures"])
	}
	f0 := fl[0].(map[string]any)
	if f0["subject"] != "subject F-INBOX-0002" || f0["folder"] != "Inbox" || f0["reason"] == "" {
		t.Errorf("the first failure: %v", f0)
	}
	e.failWith("F-INBOX-0002", 0)
	e.failWith("F-INBOX-0005", 0)

	// a wedged run after a restart: the stop's words stay on the card
	run, _, _ := e.wedge()
	e.restart()
	var card map[string]any
	for i := 0; i < 300; i++ {
		card = e.card(run)
		if hasAct(card, "finish-as-partial") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop, _ := card["stop"].(string)
	if !hasAct(card, "finish-as-partial") || !strings.Contains(stop, "destination is full or no longer available") || !strings.Contains(stop, "Export the remaining messages") {
		t.Fatalf("the interrupted card after a restart: %v %q", card["actions"], stop)
	}
	if next, _ := card["next"].(string); !strings.Contains(next, "To continue") || !strings.Contains(next, "Finish as partial") || !strings.Contains(next, "Export the remaining messages") {
		t.Errorf("the interrupted card does not say what to do next: %q", card["next"])
	}
	chk := e.ok("/api/exports/check", map[string]any{"runId": run})
	problems, _ := chk["problems"].([]any)
	if chk["green"] != false || len(problems) == 0 || !strings.Contains(toJSON(problems), "unfinished") || !strings.Contains(chk["remedy"].(string), "Do not hand this folder over") {
		t.Errorf("Check this export of an unfinished export: %v", chk)
	}
	if !strings.Contains(exportsJS, "Which ones?") || !strings.Contains(exportsJS, "c.next") {
		t.Error("exports.js does not draw the failures or the next step")
	}
}
