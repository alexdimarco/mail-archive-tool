package desktop

import (
	"os"
	"strings"
	"testing"
	"time"
)

// F2 and F3 of the friction review: a sign-in that is refused, cleared or changed leaves the
// owner on Export activity with a way to sign in again, and every retry refusal names the
// account to sign in as and the button to click next.

// signInNeeded is the history answer's sign-in block.
func (e *wsEnv) signInNeeded() (needed bool, as, why string) {
	e.t.Helper()
	out := e.ok("/api/exports/history", nil)
	s, _ := out["signIn"].(map[string]any)
	if s == nil {
		return false, "", ""
	}
	needed, _ = s["needed"].(bool)
	as, _ = s["as"].(string)
	why, _ = s["why"].(string)
	return needed, as, why
}

// covers: MA-404, MA-437, MA-438, R12, S52
// F2: the saved sign-in is refused (it expired or access was revoked). The workspace no
// longer says "ready": its source answer is needs-sign-in, with the account; Export activity
// loads the sign-in script and carries a Sign in button, shown (by the history answer) when a
// retry is refused for the sign-in; signing in again makes everything ready, and the retry
// works. The positive twin first: with a good sign-in, nothing is asked.
func TestFricARefusedSavedSignInCanBeSignedInAgainFromExportActivity(t *testing.T) {
	e := newWsEnv(t)
	run, _ := e.cancelledRun()

	// the positive twin
	if m := e.ok("/api/workspace/source", nil)["m365"].(map[string]any); m["state"] != "ready" {
		t.Fatalf("a good sign-in: %v", m)
	}
	if needed, _, _ := e.signInNeeded(); needed {
		t.Fatal("Export activity asks for a sign-in with a good one")
	}
	page := e.get("/exports").Body.String()
	_, ids, _ := sidebarNav(t, page)
	for _, id := range []string{"signInBtn", "signInBox", "signInCode", "signInCard"} {
		if !ids[id] {
			t.Errorf("Export activity lacks #%s: 'Sign in again' names a button the owner cannot reach", id)
		}
	}
	if !strings.Contains(page, `src="signin.js"`) {
		t.Error("Export activity does not load the sign-in script")
	}

	// the refresh token is refused: the retry says so, naming the account and the buttons
	e.writeToken("alice@contoso.org", -time.Hour)
	e.fg.mutate(func() { e.fg.tokenErr = "invalid_grant" })
	e.d.ws.dropSession()
	code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
	refusedBody(t, code, out, "R-c", "refused", "expired or access was revoked", "alice@contoso.org", "Click Sign in on this page", "Export the remaining messages")
	if strings.Contains(out["error"].(string), "AADSTS") || strings.Contains(out["error"].(string), "SECRET") {
		t.Errorf("the refusal leaks the identity provider's text: %v", out["error"])
	}

	// the workspace page puts Sign in in front of the owner when it is refused for the sign-in
	if !strings.Contains(workspaceJS, "d.id==='R-c'||d.id==='R-l'))$('signInArea').hidden=false") || !strings.Contains(workspaceJS, "m.state==='needs-sign-in'") {
		t.Error("workspace.js does not show Sign in for a refused sign-in")
	}

	// the workspace stops saying ready, and Export activity shows its Sign in
	m := e.ok("/api/workspace/source", nil)["m365"].(map[string]any)
	if m["state"] != "needs-sign-in" || m["upn"] != "alice@contoso.org" {
		t.Errorf("the workspace's source after a refused sign-in: %v", m)
	}
	needed, as, why := e.signInNeeded()
	if !needed || as != "alice@contoso.org" || !strings.Contains(why, "refused") {
		t.Errorf("Export activity's sign-in block: %v %q %q", needed, as, why)
	}

	// signing in again, through the same job the button starts, makes it all ready again
	e.fg.mutate(func() { e.fg.tokenErr = "" })
	e.ok("/api/m365/signin", nil)
	st := e.waitSignIn("running")
	for st["device"] == nil {
		time.Sleep(10 * time.Millisecond)
		st = e.signInStatus("")
	}
	if done := e.waitSignIn("done"); done["upn"] != "alice@contoso.org" {
		t.Fatalf("the sign-in: %v", done)
	}
	if m := e.ok("/api/workspace/source", nil)["m365"].(map[string]any); m["state"] != "ready" {
		t.Errorf("after signing in again: %v", m)
	}
	if needed, _, _ := e.signInNeeded(); needed {
		t.Error("Export activity still asks for a sign-in after signing in")
	}
	if out := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"}); out["previewId"] == "" {
		t.Errorf("the retry after signing in: %v", out)
	}
}

// covers: MA-437, MA-438, R12, S52
// F3: the retry's refusals give the retry's remedy. Signed out (R-b): the account the export
// was made in, the Sign in button on this page, then the button to click again. Signed in as
// someone else (R-l): both accounts, sign in as the first, then the button again, and never
// "Load folders again", which is the selection's remedy. The stop notes of a run that was
// cut off by the sign-in (R-c, R-z) say the same, and the card shows Sign in for them.
func TestFricARetryRefusedForTheSignInNamesTheAccountAndTheButton(t *testing.T) {
	e := newWsEnv(t)
	run, _ := e.cancelledRun()

	// signed out
	os.Remove(e.tok)
	e.d.signInChanged("")
	code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
	refusedBody(t, code, out, "R-b", "not signed in", "alice@contoso.org", "Click Sign in on this page", "Export the remaining messages")
	if needed, as, _ := e.signInNeeded(); !needed || as != "alice@contoso.org" {
		t.Errorf("signed out: Export activity's sign-in block: %v %q", needed, as)
	}

	// signed in as someone else
	e.writeToken("bob@contoso.org", time.Hour)
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	e.d.signInChanged("bob@contoso.org")
	code, out = e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
	refusedBody(t, code, out, "R-l", "alice@contoso.org", "bob@contoso.org", "Sign in as alice@contoso.org", "Click Sign in on this page", "Export the remaining messages")
	if strings.Contains(out["error"].(string), "Load folders again") {
		t.Errorf("a retry is not told to start a new selection: %v", out["error"])
	}
	if needed, as, _ := e.signInNeeded(); !needed || as != "alice@contoso.org" {
		t.Errorf("signed in as bob: Export activity's sign-in block: %v %q", needed, as)
	}
}
