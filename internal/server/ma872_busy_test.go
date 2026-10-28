package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-872, R37
// The archive reader's Prev and Next (design-desktop-settings-card §11, slice A1) go through
// busy: the button is disabled, marked aria-busy, its label becomes "Loading…" and the results
// line says what loads; search gives it back when the reply arrives, success or error, before it
// draws the results (which set whether Prev and Next can be pressed). The reader's style carries
// a pressed and a busy rule. The page and script are served by Serve, which both the Desktop's
// reader and mailarchive serve use, so the one check covers both. The separate Microsoft 365
// setup surface is untouched. The script is read, not run: a live check in Chrome is the
// friction note's.
func TestReaderPrevNextGoThroughBusy(t *testing.T) {
	ts, _ := serveArchive(t, "<p>hi</p>")
	_, js := getBody(t, ts.URL+"/app.js")
	_, page := getBody(t, ts.URL+"/")
	assure.Reached(t, js == appJS, "the served app.js is the reader's script")

	i := strings.Index(js, "function busy(b,label,text){")
	if i < 0 {
		t.Fatal("the reader's script has no busy helper")
	}
	helper := js[i : i+strings.Index(js[i:], "\n}\n")]
	for _, want := range []string{"b.disabled=true", "b.setAttribute('aria-busy','true')", "b.textContent=label", "$('#meta').textContent=text", "b.removeAttribute('aria-busy')", "b.textContent=was", "b.disabled=false"} {
		if !strings.Contains(helper, want) {
			t.Errorf("the reader's busy does not %q:\n%s", want, helper)
		}
	}
	for _, want := range []string{
		"$('#prev').onclick=()=>{if(offset>0){offset-=limit;search(busy($('#prev'),'Loading…',",
		"$('#next').onclick=()=>{if(offset+limit<lastTotal){offset+=limit;search(busy($('#next'),'Loading…',",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("Prev or Next does not go through busy: missing %q", want)
		}
	}
	if n := strings.Count(js, ".onclick="); n != 2 {
		t.Errorf("the reader binds %d click handlers; this test reads two (Prev and Next)", n)
	}
	s := strings.Index(js, "async function search(done){")
	if s < 0 {
		t.Fatal("search does not take the busy's done")
	}
	body := js[s : s+strings.Index(js[s:], "\n}\n")]
	gave, drew := strings.Index(body, "finally{if(done)done();}"), strings.Index(body, "render(d)")
	if gave < 0 || drew < 0 || gave > drew || !strings.Contains(body, "catch(e){d={error:") {
		t.Errorf("search does not give the button back on every reply, error included, before drawing:\n%s", body)
	}

	for _, want := range []string{
		"button:disabled{opacity:.45;cursor:default}",
		"button:active:not(:disabled){background:var(--accent-soft);transform:translateY(1px)}",
		"button[aria-busy=true]{cursor:progress;opacity:.75}",
		"button[aria-busy=true]::before{",
		"@keyframes busy-spin",
		"@media (prefers-reduced-motion:reduce){button[aria-busy=true]::before{animation:none}}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the reader's style does not carry %q", want)
		}
	}
	if d, b := strings.Index(page, "button:disabled{"), strings.Index(page, "button[aria-busy=true]{"); b < d {
		t.Errorf("the busy rule does not follow button:disabled (at %d, %d)", d, b)
	}

	// the setup surface is not in scope: no busy, no pressed or busy rule
	h, _, _ := newSetup(t)
	_, setup := getCSRF(t, h)
	rec, req := httptest.NewRecorder(), httptest.NewRequest("GET", "/setup.js", nil)
	req.Host = "127.0.0.1:8097"
	h.ServeHTTP(rec, req)
	for _, bad := range []string{"busy(", "aria-busy", ":active"} {
		if strings.Contains(setup, bad) || strings.Contains(rec.Body.String(), bad) || strings.Contains(setupJS, bad) {
			t.Errorf("the Microsoft 365 setup surface carries %q; it is out of this slice's scope", bad)
		}
	}
	assure.Reached(t, rec.Body.String(), "the setup surface's script was served")
}
