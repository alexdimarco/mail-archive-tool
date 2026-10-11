package desktop

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-437, MA-448, R12, S52
// F8's remedy after Finish as partial: a run that was cut off because its destination stopped
// answering keeps the stop's why and what to do on its card before Finish (the unsealed
// stop's words) and after it (the sealed stop holds only the short reason). The card after
// Finish as partial says what happened, why, and the button that continues. The positive
// twin first: before Finish the card carries the full words.
func TestFricAFinishedAsPartialCardKeepsTheStopsRemedy(t *testing.T) {
	e := newWsEnv(t)
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
	if stop, _ := card["stop"].(string); !hasAct(card, "finish-as-partial") || !strings.Contains(stop, "Export the remaining messages") {
		t.Fatalf("before Finish: %v %q", card["actions"], card["stop"])
	}
	e.ok("/api/exports/finish", map[string]any{"runId": run})
	card = e.card(run)
	stop, _ := card["stop"].(string)
	for _, want := range []string{"destination", "Export the remaining messages"} {
		if !strings.Contains(stop, want) {
			t.Errorf("after Finish as partial the stop line is %q: it lacks %q", stop, want)
		}
	}
	if !hasAct(card, "export-remaining") {
		t.Errorf("the card after Finish as partial: %v", card["actions"])
	}
}

// covers: MA-438, MA-446, R12, S52
// F15 (the half that needs no owner): the workspace's R-i for one folder, and the footer's note
// beside the count, say that one folder cannot be split yet and what can be done; "split it by
// folder" stays for a selection of several. The refusal is raised while the preview reads the
// folders (200,001 messages are not served here), so its words are read from where they are
// made. The positive twin first: the several-folder words.
func TestFricRIForOneFolderIsWordedForOneFolder(t *testing.T) {
	// through the preview, with the cap lowered: one folder, then two
	e := newWsEnv(t)
	ids := e.folders()
	old := previewMessageCap
	previewMessageCap = 3
	t.Cleanup(func() { previewMessageCap = old })
	if out := e.previewFailure(e.selection(entry(ids["Inbox"], false))); out["id"] != "R-i" ||
		!strings.Contains(out["error"].(string), "This folder has more than 200,000 messages") || !strings.Contains(out["error"].(string), "by hand") {
		t.Errorf("one folder past the cap: %v", out)
	}
	if out := e.previewFailure(e.selection(entry(ids["Inbox"], false), entry(ids["Sent Items"], false))); out["id"] != "R-i" ||
		!strings.Contains(out["error"].(string), "Split it into several exports") {
		t.Errorf("two folders past the cap: %v", out)
	}
	previewMessageCap = old

	several := refusalI(false)
	assure.Refused(t, several.Status, several.Text, assure.Code(statusRefused),
		assure.Names("This selection has more than 200,000 messages", "Split it into several exports", "for example by folder"))
	one := refusalI(true)
	assure.Refused(t, one.Status, one.Text, assure.Code(statusRefused),
		assure.Names("This folder has more than 200,000 messages", "can't split one folder", "by hand", "10,000", "subfolders"),
		assure.Forbid("Split it into several exports", "for example by folder"))
	for _, want := range []string{"const oneFolder=st.entries.length===1", "one folder can\\'t be split across exports yet", "select fewer folders or messages"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not contain %q: the footer's 200,000 note is a dead end for one folder", want)
		}
	}
}

// covers: MA-414, MA-446, R12, S52
// The destination box's own example is a path of this computer's shape (the engine's R-e words
// are checked in the app package): a macOS or Linux owner is not shown C:\Users\you\Downloads.
func TestFricTheDestinationBoxShowsThisComputersExample(t *testing.T) {
	e := newWsEnv(t)
	want := app.DestExampleFor(e.d.cfg.goos())
	page := e.get("/workspace").Body.String()
	if !strings.Contains(page, `placeholder="An existing folder, e.g. `+want+`"`) {
		t.Errorf("the destination box's placeholder is not %q", want)
	}
	if e.d.cfg.goos() != "windows" && strings.Contains(page, `C:\Users`) {
		t.Error("the workspace page shows a Windows path to a computer that is not Windows")
	}
}

// The fresh-eyes check of the friction fixes (EW-E3 re-verification): what it found in the
// fixes themselves.

// covers: MA-437, MA-438, MA-446, R12, S52
// F5's remedy: a retry's preview has no "Preview copy export" button (the footer is for a
// selection; the retry's own button is "Preview again for this destination"). R-k at Start of
// a retry's preview, and when the page asks about one that another window replaced, therefore
// names the button that is on the page. A selection's preview keeps the old words. The
// positive twin first: the live retry preview starts.
func TestFricRKOfARetryPreviewNamesTheRetrysOwnButton(t *testing.T) {
	e := newWsEnv(t)
	run, _ := e.cancelledRun()

	first := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	e.waitPreview(first)
	second := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	e.waitPreview(second)

	// the replaced preview: Start answers R-k in the retry's words
	code, out := e.post("/api/exports/start", map[string]any{"previewId": first})
	refusedBody(t, code, out, "R-k", "out of date", "another window started a new preview", "Preview again for this destination")
	if strings.Contains(out["error"].(string), "Preview copy export") {
		t.Errorf("a retry is told to click a button its page does not show: %v", out["error"])
	}
	// the same for a page that asks about a preview that is gone
	code, out = e.post("/api/workspace/preview-status", map[string]any{"previewId": first})
	refusedBody(t, code, out, "R-k", "out of date", "Preview again for this destination")

	// the positive twin: the live retry preview starts
	if out := e.ok("/api/exports/start", map[string]any{"previewId": second}); out["runId"] == nil || out["runId"] == "" {
		t.Fatalf("the live retry preview did not start: %v", out)
	}
	e.waitRun()

	// a selection's preview keeps "Preview copy export again"
	ids := e.folders()
	one, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	e.preview(e.selection(entry(ids["Inbox"], false)))
	code, out = e.post("/api/exports/start", map[string]any{"previewId": one})
	refusedBody(t, code, out, "R-k", "out of date", "Preview copy export again")
}
