package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// The friction review's fixes (docs/review-extraction-workspace-friction.md, F1..F22): each
// test asserts what the owner now sees, through the dashboard's own handlers.

// resultOf is the refusal a card carries, or nil.
func resultOf(c map[string]any) map[string]any {
	r, _ := c["result"].(map[string]any)
	return r
}

// cancelledRun starts an export of the Inbox, cancels it after two messages and returns the
// run and its folder name.
func (e *wsEnv) cancelledRun() (run, root string) {
	e.t.Helper()
	ids := e.folders()
	e.slow("F-INBOX", 80*time.Millisecond)
	run = e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitDone(2)
	e.ok("/api/exports/cancel", map[string]any{"runId": run})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "cancelled" {
		e.t.Fatalf("the cancelled run: %v", last)
	}
	return run, last["rootName"].(string)
}

// covers: MA-446, MA-520, R12, S52
// F1: a refusal from a card's button stays on that card. The page rebuilds every card from
// the next history answer, so the refusal is part of that answer (until the next click on
// that card, or 30 minutes) and a reload, or a second window, shows it too. The positive
// twin first: the same button, with nothing wrong, says nothing; a refused one is typed,
// names what happened and what to do, and leaves the marker in place.
func TestFricACardButtonsRefusalStaysOnItsCard(t *testing.T) {
	e := newWsEnv(t)
	// the page script draws the kept refusal when it builds a card, and mirrors a fresh one
	// into the page's alert (the script itself cannot run here: it is read, as the walk read it)
	for _, want := range []string{"c.result.error", "note(t)"} {
		if !strings.Contains(exportsJS, want) {
			t.Errorf("exports.js does not contain %q: a card's refusal would be wiped by the next history rebuild", want)
		}
	}
	run, root := e.cancelledRun()
	markerPath := filepath.Join(e.dest, root, "_EXPORT-NOT-FINISHED.txt")

	// the positive twin: Check and finish on an untouched folder works and the card carries nothing
	e.ok("/api/exports/finish", map[string]any{"runId": run})
	if c := e.card(run); resultOf(c) != nil {
		t.Fatalf("a successful click left a result on the card: %v", resultOf(c))
	}

	// a changed file: the refusal comes back, and is on the card in every later answer
	emls := e.emlsOf(root)
	p := filepath.Join(e.dest, root, filepath.FromSlash(emls[0]))
	orig, _ := os.ReadFile(p)
	os.WriteFile(p, append(append([]byte(nil), orig...), 'x'), 0o600)
	code, out := e.post("/api/exports/finish", map[string]any{"runId": run})
	refusedBody(t, code, out, "R-check", "the marker is kept")
	for i := 0; i < 3; i++ { // the page asks again every few seconds, and after every click
		r := resultOf(e.card(run))
		if r == nil {
			t.Fatalf("history answer %d: the refusal is gone from the card", i+1)
		}
		assure.Refused(t, statusRefused, r["error"].(string), assure.Code(statusRefused), assure.Names("the marker is kept"),
			assure.NoSideEffect(func() bool { _, err := os.Stat(markerPath); return err == nil }))
		if r["id"] != "R-check" {
			t.Errorf("the kept refusal's id: %v", r["id"])
		}
	}
	// a second window sees it too (a fresh dashboard on the same state is a restart: the
	// refusal is the process's, so it is gone, which is the documented lifetime)
	other := e.card(run)
	if resultOf(other) == nil {
		t.Fatal("the refusal is not part of the history answer")
	}

	// the next click on that card replaces it; a success leaves none
	os.WriteFile(p, orig, 0o600)
	e.ok("/api/exports/finish", map[string]any{"runId": run})
	if c := e.card(run); resultOf(c) != nil {
		t.Errorf("the refusal outlived a successful click: %v", resultOf(c))
	}

	// the other card buttons keep theirs too: Check this export of a run that has no folder
	os.RemoveAll(filepath.Join(e.dest, root))
	code, out = e.post("/api/exports/check", map[string]any{"runId": run})
	if out["ok"] != false {
		t.Fatalf("Check this export of a missing folder: %d %v", code, out)
	}
	if r := resultOf(e.card(run)); r == nil || r["error"] != out["error"] {
		t.Errorf("Check this export's refusal is not on the card: %v", r)
	}
}

// covers: MA-446, MA-448, R12, S52
// F4: a finished export's folder is renamed. Open folder refuses (R-r2, in plain words: nothing
// is there), and the card now offers the three buttons the refusal tells the owner to click:
// Look again, Locate... and This folder is gone. Before anything is wrong the card offers
// neither; after Locate accepts the folder the refusal and the buttons are gone.
func TestFricAFinishedCardOffersTheFolderButtonsAfterTheFolderIsLost(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	run := e.startExport(e.selection(entry(ids["Sent Items"], false)))
	last := e.waitRun()["last"].(map[string]any)
	root := last["rootName"].(string)
	var revealed []string
	e.d.reveal = func(name string, args []string) error { revealed = append([]string{name}, args...); return nil }

	// the positive twin: the folder is where it was, Open folder works, no extra buttons
	e.ok("/api/exports/open-folder", map[string]any{"runId": run})
	if len(revealed) == 0 {
		t.Fatal("Open folder did not reveal a healthy folder")
	}
	if c := e.card(run); !reflect.DeepEqual(actionsOf(c), []string{"open-folder", "check-this-export"}) || resultOf(c) != nil {
		t.Fatalf("a healthy finished card: %v %v", actionsOf(c), resultOf(c))
	}

	revealed = nil
	moved := filepath.Join(e.dest, root+" (renamed)")
	if err := os.Rename(filepath.Join(e.dest, root), moved); err != nil {
		t.Fatal(err)
	}
	code, out := e.post("/api/exports/open-folder", map[string]any{"runId": run})
	refusedBody(t, code, out, "R-r2", "nothing is there", "moved, renamed or deleted", "Locate", "Look again", "This folder is gone")
	if strings.Contains(out["error"].(string), "plugged in again") {
		t.Errorf("a renamed folder is not a drive plugged in again: %v", out["error"])
	}
	if len(revealed) != 0 {
		t.Errorf("a refused Open folder revealed %v", revealed)
	}
	c := e.card(run)
	for _, a := range []string{"look-again", "locate", "this-folder-is-gone"} {
		if !hasAct(c, a) {
			t.Errorf("the card after R-r2 does not offer %s: %v", a, actionsOf(c))
		}
	}
	if r := resultOf(c); r == nil || r["id"] != "R-r2" {
		t.Errorf("the card's kept refusal: %v", r)
	}

	// Locate accepts the folder where it is now; the card is a finished one again
	e.ok("/api/exports/locate", map[string]any{"runId": run, "action": "locate", "path": moved})
	c = e.card(run)
	if resultOf(c) != nil || hasAct(c, "look-again") || hasAct(c, "locate") || hasAct(c, "this-folder-is-gone") || !hasAct(c, "open-folder") {
		t.Errorf("the card after Locate: %v %v", actionsOf(c), resultOf(c))
	}
	e.ok("/api/exports/open-folder", map[string]any{"runId": run})
	if len(revealed) == 0 {
		t.Error("Open folder does not reveal the located folder")
	}
}
