package desktop

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// The page script's delete functions (docs/design-archive-delete.md §3.1, slice 3b), read as the
// Desktop's other page tests read the page script: the Desktop's tests run no browser, so the
// script's guarantees are checked in its text, and every key it reads is checked against the
// replies the routes really send.

// jsFunc is the body of the page script's function name, failing the test when it has none.
func jsFunc(t *testing.T, name string) string {
	t.Helper()
	body := jsFuncs(dashboardJS)[name]
	if body == "" {
		t.Fatalf("the page script has no %s", name)
	}
	return body
}

// jsHandlerFor is the click handler the page script registers right after start (the
// querySelectorAll or the element it names), as far as its closing parenthesis.
func jsHandlerFor(t *testing.T, start string) string {
	t.Helper()
	i := strings.Index(dashboardJS, start)
	if i < 0 {
		t.Fatalf("the page script has no handler for %s", start)
	}
	k := strings.Index(dashboardJS[i:], "addEventListener('click',")
	if k < 0 {
		t.Fatalf("%s registers no click handler", start)
	}
	open := i + k + len("addEventListener")
	e := jsClose(dashboardJS, open)
	if e < 0 {
		t.Fatalf("%s's handler does not close", start)
	}
	return dashboardJS[i:e]
}

// readKeys are the names a body reads from obj (obj.NAME), sorted.
func readKeys(body, obj string) []string {
	re := regexp.MustCompile(`(?:^|[^\w$.])` + regexp.QuoteMeta(obj) + `\.([A-Za-z_]\w*)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// noBrowserDialogs fails when a body asks through the browser's own dialogs.
func noBrowserDialogs(t *testing.T, what, body string) {
	t.Helper()
	for _, bad := range []string{"confirm(", "prompt(", "alert(", "innerHTML", "outerHTML", "insertAdjacentHTML"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s uses %s: the panel is the page's own, built with textContent (C19, X15)", what, bad)
		}
	}
}

// covers: MA-983, R36, S41
// Delete… opens the plan in a panel beside the row (§3.1; C19): its handler moves the page's
// note beside the row (noteBeside, MA-955) before it says it is working, asks the plan route,
// and draws the panel inside the row — never a browser confirm() or prompt(), and with
// textContent only (X15). Opening a delete panel (or Remove from list's question) closes any
// other one. Every key the panel reads is in the plan route's reply: positive twin first, a
// real plan's reply.
func TestTheDeletePanelSitsBesideTheRowAndReadsThePlansReply(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Panel")
	reply := e.plan(dir)
	assure.Reached(t, reply["ok"] == true && reply["token"] != nil, "a real plan's reply, the panel's source")

	h := jsHandlerFor(t, "document.querySelectorAll('[data-delete-archive]')")
	nb, bz := strings.Index(h, "noteBeside("), strings.Index(h, "delPlan(")
	if nb < 0 || bz < 0 || nb > bz {
		t.Errorf("Delete… does not move the note beside the row before it plans: %s", h)
	}
	plan := jsFunc(t, "delPlan")
	if !strings.Contains(plan, "noteBeside(li)") || !strings.Contains(plan, "busy(") || strings.Index(plan, "noteBeside(") > strings.Index(plan, "busy(") {
		t.Errorf("delPlan does not say it is working beside the row: %s", plan)
	}
	if !strings.Contains(plan, "delPost('api/archive/delete/plan',{path:b.dataset.path})") {
		t.Errorf("delPlan does not ask the plan route for the row's path: %s", plan)
	}
	// One panel at a time: delPlan closes every other panel before it draws its own, in the row.
	cl, ap := strings.Index(plan, "delClose()"), strings.Index(plan, "li.append(delPanel(")
	if cl < 0 || ap < 0 || cl > ap {
		t.Errorf("delPlan does not close the other panels before drawing its own in the row: %s", plan)
	}
	closer := jsFunc(t, "delClose")
	for _, sel := range []string{".delete-panel", ".unlist-ask"} {
		if !strings.Contains(closer, sel) || !strings.Contains(closer, ".remove()") {
			t.Errorf("delClose does not remove every %s: %s", sel, closer)
		}
	}
	for _, f := range []string{"delPlan", "delPanel", "delClose", "delNameMatches", "delSaveFile"} {
		noBrowserDialogs(t, f, jsFunc(t, f))
	}
	noBrowserDialogs(t, "Delete…'s handler", h)

	// Every key the panel reads is in the plan's reply; the optional lines are in planView's
	// reply for a plan that has them (a paused, synced, encrypted archive with a mail copy no
	// record names).
	keys := map[string]bool{}
	for k := range reply {
		keys[k] = true
	}
	full := planView(&archdelete.DeletePlan{Paused: true, Synced: "OneDrive", Kind: archdelete.Encrypted, Unrecorded: []archdelete.Item{{Rel: "store/x.html"}}})
	for k := range full {
		keys[k] = true
	}
	panel := jsFunc(t, "delPanel")
	read := readKeys(panel, "p")
	if len(read) < 15 {
		t.Fatalf("the panel reads only %v from the plan", read)
	}
	for _, k := range read {
		if !keys[k] {
			t.Errorf("the panel reads p.%s, which the plan's reply does not carry", k)
		}
	}
	for _, k := range []string{"folder", "size", "files", "stores", "schedules", "checkYourself", "notes", "pausedLine", "syncedLine", "kept",
		"unrecordedNames", "unrecordedBox", "keyLine", "doesNotReach", "logLine", "threeLine", "typeLine", "name", "token", "planText", "planName"} {
		if !strings.Contains(panel, "p."+k) {
			t.Errorf("the panel does not show the plan's %s (§3.1's list)", k)
		}
	}
}

// covers: MA-983, R36, S41
// The panel asks for the folder's name before anything is deleted (§3.1 step 2, §9 "Names";
// C19): "Type the folder's name, NAME, to delete it", a text box, a live "doesn't match yet",
// Delete permanently (danger style) disabled until the name matches, and Cancel. The match is
// the server's archdelete.NameMatches: not empty, both sides NFC, no case folding, no trimming.
// §4.3's box is unticked; Save this plan saves the reply's exact text under its own name without
// a request; Delete permanently posts the token, the typed name and the box, and a refusal with
// no held result is said in the panel and the button disabled again until the name matches.
func TestThePanelDeletesOnlyOnceTheNameMatches(t *testing.T) {
	// The server's rule, which the page mirrors: positive twin first.
	if !archdelete.NameMatches("Mail Archivé", "Mail Archivé") || archdelete.NameMatches("mail archivé", "Mail Archivé") ||
		archdelete.NameMatches(" Mail", "Mail") || archdelete.NameMatches("", "") {
		t.Fatal("archdelete.NameMatches is not NFC, case-exact, untrimmed and non-empty, which the page mirrors")
	}
	m := jsFunc(t, "delNameMatches")
	if !strings.Contains(m, "typed!==''&&typed.normalize('NFC')===name.normalize('NFC')") {
		t.Errorf("the page's match is not the server's (not empty, both sides NFC): %s", m)
	}
	for _, bad := range []string{"toLowerCase", "toUpperCase", "toLocale", "localeCompare", "trim(", "replace("} {
		if strings.Contains(m, bad) {
			t.Errorf("the page's match uses %s: the server folds no case and trims nothing", bad)
		}
	}

	panel := jsFunc(t, "delPanel")
	for _, want := range []string{
		"p.typeLine", "'input'", "doesn't match yet", "'Delete permanently'", "btn danger-fill", "'Cancel'", "'Save this plan'",
		"go.disabled=true", "const ok=delNameMatches(input.value,p.name);go.disabled=!ok;hint.hidden=ok;",
		"input.addEventListener('input',match)",
		"box.type='checkbox'", "delSaveFile(p.planName,p.planText)",
		"delPost('api/archive/delete',{token:p.token,typed:input.value,unrecorded:!!(box&&box.checked),drawnFor:drawnFor})",
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel lacks %q", want)
		}
	}
	if strings.Contains(panel, "box.checked=true") || strings.Contains(panel, "checked=true") {
		t.Error("§4.3's box is ticked by default")
	}
	// Save this plan sends nothing: the plan's token is neither used nor extended (§8.1, O2-08).
	save := panel[strings.Index(panel, "'Save this plan'"):]
	save = save[:strings.Index(save, "});")]
	if strings.Contains(save, "delPost(") || strings.Contains(save, "fetch(") {
		t.Errorf("Save this plan sends a request: %s", save)
	}
	// A refusal with no held result: said in the panel, the button disabled again until the name matches.
	go_ := panel[strings.Index(panel, "go.addEventListener('click',"):]
	if !strings.Contains(go_, "busy(go,") || !strings.Contains(go_, "if(d.id){") || !strings.Contains(go_, "done(!!d.planUsed);if(!d.planUsed)match();") ||
		!strings.Contains(go_, "done();match();") || !strings.Contains(go_, "msg.textContent=d.error") {
		t.Errorf("Delete permanently does not keep a refusal in the panel with the name check redone: %s", go_)
	}
	f := jsFunc(t, "delSaveFile")
	for _, want := range []string{"new Blob([data]", "URL.createObjectURL(", "a.download=name", "a.click()"} {
		if !strings.Contains(f, want) {
			t.Errorf("delSaveFile does not save through the browser as the fingerprint receipt is saved: %q missing", want)
		}
	}
}
