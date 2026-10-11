package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

var scriptSrcRe = regexp.MustCompile(`<script src="([^"]+)"`)

// topLevelDecls lists the names a classic script declares with const, let or class outside
// any function: the names the browser's one shared global lexical scope holds for the whole
// page, so a second script that declares the same name never runs (SyntaxError). It skips
// strings, template literals, comments and regular-expression literals.
func topLevelDecls(src string) []string {
	var out []string
	depth := 0
	prev := byte(';') // the last significant byte, to tell a regex from a division
	atStart := true   // at the start of a statement at depth 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 3
			continue
		case c == '\'' || c == '"' || c == '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
			prev = 'x'
			continue
		case c == '/' && strings.IndexByte("(,=:[!&|?{};", prev) >= 0:
			inClass := false
			for i++; i < len(src); i++ {
				if src[i] == '\\' {
					i++
				} else if src[i] == '[' {
					inClass = true
				} else if src[i] == ']' {
					inClass = false
				} else if src[i] == '/' && !inClass {
					break
				}
			}
			prev = 'x'
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		}
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			continue
		}
		if depth == 0 && atStart {
			for _, kw := range []string{"const ", "let ", "class "} {
				if strings.HasPrefix(src[i:], kw) {
					rest := src[i+len(kw):]
					j := 0
					for j < len(rest) && (rest[j] == '$' || rest[j] == '_' || rest[j] >= '0' && rest[j] <= '9' || rest[j] >= 'a' && rest[j] <= 'z' || rest[j] >= 'A' && rest[j] <= 'Z') {
						j++
					}
					out = append(out, rest[:j])
				}
			}
		}
		atStart = depth == 0 && (c == ';' || c == '}')
		prev = c
	}
	return out
}

// covers: MA-407, R12, S52
// F27 (found fixing F2): two classic scripts on one page share one global lexical scope, so a
// second script that declares the same top-level const never runs at all. The workspace page
// loaded signin.js and workspace.js, the Overview dashboard.js and signin.js, each with
// `const csrf`: the second of each pair would have thrown "Identifier 'csrf' has already been
// declared", leaving the workspace page dead. No two scripts of a page may declare the same
// top-level name (the workspace's scripts keep theirs inside one function).
func TestFricNoTwoScriptsOfAPageShareATopLevelName(t *testing.T) {
	e := newWsEnv(t)
	if got := topLevelDecls("const a=1;function f(){const b=2;}\nlet c=/[/]x/.test('const d');"); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("the scanner reads %v, want [a c]", got)
	}
	for _, page := range []string{"/", "/workspace", "/exports"} {
		body := e.get(page).Body.String()
		srcs := assure.Reached(t, scriptSrcRe.FindAllStringSubmatch(body, -1), page+": its scripts")
		seen := map[string]string{}
		for _, m := range srcs {
			rec := e.get("/" + m[1])
			if rec.Code != 200 {
				t.Fatalf("%s: script %s answers %d", page, m[1], rec.Code)
			}
			for _, name := range topLevelDecls(rec.Body.String()) {
				if other, dup := seen[name]; dup {
					t.Errorf("%s: %s and %s both declare %q at the top level: the second script would not run", page, other, m[1], name)
				}
				seen[name] = m[1]
			}
		}
	}
}

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

// covers: MA-437, MA-444, MA-446, R12, S52
// F5: a retry can be pointed at another destination. The remedies of a full or missing stick
// (R-g2 "choose another destination", R-e "Click Browse") need a destination control on the
// retry's preview: the preview says which retry it is (retryOf, retryWhich) and where it
// goes, the workspace page has a button that previews it again for the path in the box, and
// the engine honours it (the next part lands in the other folder, the earlier export
// untouched); a destination that is not there is R-e naming Browse. The positive twin (the
// same retry into its own destination) first.
func TestFricARetryPreviewCanBePointedAtAnotherDestination(t *testing.T) {
	e := newWsEnv(t)
	run, root := e.cancelledRun()
	before := treeState(t, filepath.Join(e.dest, root))
	exported := e.card(run)["exported"].(float64)

	// the page: a button on the workspace that previews the retry again for the path typed
	page := e.get("/workspace").Body.String()
	if !strings.Contains(page, `id="rePreviewBtn"`) {
		t.Error("the workspace page has no button to preview a retry again with another destination")
	}
	for _, want := range []string{"api('api/exports/retry',{runId:", "retryWhich", "dest:$('destPath').value"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not contain %q: the retry preview cannot send a destination", want)
		}
	}

	// the positive twin: the retry into its own destination
	res := e.waitPreview(e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string))
	if res["retryOf"] != run || res["retryWhich"] != "rest" || res["dest"] != e.dest || !strings.HasSuffix(res["exportName"].(string), "-part2") {
		t.Fatalf("the retry preview: %v", res)
	}

	// another destination (a different stick): the preview goes there, the export lands there
	stick := filepath.Join(e.dir, "stick")
	os.Mkdir(stick, 0o755)
	pid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest", "dest": stick})["previewId"].(string)
	res = e.waitPreview(pid)
	if res["dest"] != stick || res["retryOf"] != run || res["retryWhich"] != "rest" || !strings.HasSuffix(res["exportName"].(string), "-part2") {
		t.Fatalf("the retry preview for another destination: %v", res)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["rootName"] != root+"-part2" {
		t.Fatalf("the retry run: %v", last)
	}
	if got, want := len(e.emlsOfIn(stick, root+"-part2")), 7-int(exported); got != want {
		t.Errorf("the other destination holds %d messages, want the %d that were left", got, want)
	}
	if got := treeState(t, filepath.Join(e.dest, root)); !reflect.DeepEqual(got, before) {
		t.Errorf("the earlier export changed: %v", diffKeys(before, got))
	}

	// a destination that is not there is refused after the preview starts, naming Browse
	pid = e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest", "dest": filepath.Join(e.dir, "unplugged")})["previewId"].(string)
	var failed map[string]any
	for i := 0; i < 300 && failed == nil; i++ {
		if _, out := e.post("/api/workspace/preview-status", map[string]any{"previewId": pid}); out["state"] == "failed" {
			failed = out
		}
		time.Sleep(20 * time.Millisecond)
	}
	if failed == nil || failed["id"] != "R-e" || !strings.Contains(failed["error"].(string), "Browse") || failed["retryOf"] != run || failed["retryWhich"] != "rest" {
		t.Errorf("a retry into a missing destination: %v", failed)
	}
}

// emlsOfIn lists the .eml files of a root inside the given destination.
func (e *wsEnv) emlsOfIn(dest, root string) []string {
	e.t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(dest, root), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".eml") {
			out = append(out, p)
		}
		return nil
	})
	return out
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
