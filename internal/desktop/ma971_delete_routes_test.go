package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

var deleteRoutes = []string{"/api/archive/delete/plan", "/api/archive/delete", "/api/archive/delete/dismiss", "/api/archive/unlist"}

// covers: MA-971, R36, S41
// The four delete routes are guarded POSTs (design-archive-delete §3.1, §3.2, §11 "Routes";
// C20): GET is 405 with Allow: POST; a POST with no token, a wrong token, a foreign Origin or a
// foreign Host is 403 and refused; a body over the cap is 413; each with the JSON headers. A
// refused request changes nothing: the archive byte-identical, its row listed, the settings,
// deletions.log and the held results as they were. Positive twin first: the page's own POST
// is answered with the plan.
func TestTheDeleteRoutesAreGuardedPosts(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Mail Archive")

	do := func(method, route string, mut func(*http.Request), payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, route, strings.NewReader(payload))
		req.Host = "127.0.0.1:8097"
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", e.d.csrf)
		req.Header.Set("X-Epoch", e.d.boot)
		if mut != nil {
			mut(req)
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	headers := func(route, what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" ||
			!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s: headers %v, want nosniff, no-store and JSON", route, what, rec.Header())
		}
	}

	// The positive twin: the page's own POST plans the row.
	pathJSON, _ := json.Marshal(map[string]any{"path": dir})
	rec := do(http.MethodPost, "/api/archive/delete/plan", nil, string(pathJSON))
	var twin map[string]any
	json.Unmarshal(rec.Body.Bytes(), &twin)
	if rec.Code != http.StatusOK || twin["ok"] != true || twin["token"] == "" {
		t.Fatalf("the positive twin: %d %s", rec.Code, rec.Body.String())
	}
	token := twin["token"].(string)

	before := treeState(t, dir)
	settings, _ := os.ReadFile(e.cfg.SettingsPath)
	logBefore := deletionsLog(t, e.cfg)
	body, _ := json.Marshal(map[string]any{"path": dir, "token": token, "typed": filepath.Base(dir), "id": "x", "confirm": true})
	for _, route := range deleteRoutes {
		rec := do(http.MethodGet, route, nil, "")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Errorf("GET %s: %d (Allow %q), want 405 with Allow: POST", route, rec.Code, rec.Header().Get("Allow"))
		}
		headers(route, "GET", rec)
		for what, mut := range map[string]func(*http.Request){
			"no token":         func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
			"a wrong token":    func(r *http.Request) { r.Header.Set("X-CSRF-Token", "not-the-token") },
			"a foreign Origin": func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
			"a foreign Host":   func(r *http.Request) { r.Host = "evil.example:8097" },
		} {
			rec := do(http.MethodPost, route, mut, string(body))
			var out map[string]any
			json.Unmarshal(rec.Body.Bytes(), &out)
			msg, _ := out["error"].(string)
			assure.Refused(t, rec.Code, msg, assure.Code(http.StatusForbidden), assure.Names("refused"))
			if out["id"] == "R-y" {
				t.Errorf("POST %s with %s was told it was a restart: %v", route, what, out)
			}
			headers(route, what, rec)
		}
		huge := `{"path":"` + strings.Repeat("x", 70<<10) + `"}`
		rec = do(http.MethodPost, route, nil, huge)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		msg, _ := out["error"].(string)
		assure.Refused(t, rec.Code, msg, assure.Code(http.StatusRequestEntityTooLarge), assure.Names("too large"))
		headers(route, "a body over the cap", rec)
	}
	assure.Refused(t, http.StatusForbidden, "refused", assure.Code(http.StatusForbidden), assure.Names("refused"),
		assure.NoSideEffect(func() bool {
			s, _ := os.ReadFile(e.cfg.SettingsPath)
			return reflect.DeepEqual(treeState(t, dir), before) && e.listed(dir) && string(s) == string(settings) &&
				deletionsLog(t, e.cfg) == logBefore && len(e.d.deletes.views()) == 0
		}))
	// The token the refused delete carried is still the page's: nothing took it.
	if e.d.pending.get(token, isDeletePlan) == nil {
		t.Error("a refused POST used up the plan's token")
	}
}

// covers: MA-971, R36
// The plan and Remove from list act only on a row of Your archives (§3.1 step 1, §3.2; C20):
// a folder that is no row is refused, nothing changed; a row reached by another spelling of the
// same folder is that row (by identity), and the row's own spelling is what is planned; a row
// that cannot be read is matched only by its recorded spelling, exactly — the plan then says to
// reconnect it, and Remove from list removes it — while another spelling of it is no row.
func TestTheDeleteRoutesActOnlyOnARow(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Mail Archive")

	// The positive twin: the row itself.
	if p := e.plan(dir); p["ok"] != true || p["folder"] != dir {
		t.Fatalf("the row's plan: %v", p)
	}

	// Another spelling of the row's folder (a link to it) is the row; the row's spelling is planned.
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if p := e.plan(link); p["ok"] != true || p["folder"] != dir {
			t.Errorf("a link to the row's folder: %v, want the row %s planned", p, dir)
		}
	}

	// A folder that is no row.
	stranger := e.plainArchive("Stranger")
	// Take Stranger off the list again: a real archive, but not a row.
	idx, _ := archindex.ReadFile(e.cfg.IndexPath)
	if err := archindex.WriteFile(e.cfg.IndexPath, archindex.Forget(idx, stranger)); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, stranger)
	for _, route := range []string{"/api/archive/delete/plan", "/api/archive/unlist"} {
		_, m := e.post(route, map[string]any{"path": stranger, "confirm": true})
		msg, _ := m["error"].(string)
		assure.Refused(t, http.StatusOK, msg, assure.Code(http.StatusOK), assure.Names("not in Your archives", "Reload the page"),
			assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, stranger), before) && !e.listed(stranger) && m["token"] == nil }))
	}

	// A row that cannot be read: gone (an unplugged drive).
	gone := filepath.Join(t.TempDir(), "Unplugged")
	e.list(gone)
	p := e.plan(gone)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("Reconnect the drive or folder first"),
		assure.NoSideEffect(func() bool { return e.listed(gone) && p["token"] == nil }))
	// Another spelling of it is no row: only the recorded spelling, exactly.
	other := gone + string(filepath.Separator) + "." + string(filepath.Separator)
	_, m := e.post("/api/archive/unlist", map[string]any{"path": other, "confirm": true})
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("not in Your archives"),
		assure.NoSideEffect(func() bool { return e.listed(gone) }))
	// Its own spelling: Remove from list asks, then removes it.
	_, q := e.post("/api/archive/unlist", map[string]any{"path": gone})
	if q["ok"] != true || q["ask"] != true {
		t.Fatalf("Remove from list of the unplugged row asks first: %v", q)
	}
	_, done := e.post("/api/archive/unlist", map[string]any{"path": gone, "confirm": true, "named": q["named"]})
	if done["ok"] != true || e.listed(gone) {
		t.Fatalf("Remove from list of the unplugged row by its spelling: %v; listed %v", done, e.listed(gone))
	}
}
