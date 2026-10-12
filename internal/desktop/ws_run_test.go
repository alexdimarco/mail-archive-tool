package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// Settings, the sign-in slot, destination and free-space checks at Start, shutdown and
// sign-in changes during a run, through the dashboard's own handler.

// slow makes every message of the folder take d to download.
func (e *wsEnv) slow(folder string, d time.Duration) {
	e.fg.mutate(func() {
		for _, m := range e.fg.folder(folder).Msgs {
			id := m.ID
			e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) { time.Sleep(d); w.Write(fgBody(id)) }
		}
	})
}

// startExport previews the selection and starts it; it returns the run id.
func (e *wsEnv) startExport(sel map[string]any) string {
	e.t.Helper()
	id, _ := e.preview(sel)
	return e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
}

// waitDone waits until the running export has finished at least n messages.
func (e *wsEnv) waitDone(n int) {
	e.t.Helper()
	for i := 0; i < 600; i++ {
		_, p := e.post("/api/exports/progress", nil)
		if p["running"] == false || p["done"].(float64) >= float64(n) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("the export never finished %d messages", n)
}

// manifestOf reads an export folder's manifest.
func (e *wsEnv) manifestOf(root string) map[string]any {
	e.t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readFileString(e.t, filepath.Join(e.dest, root, "export-manifest.json"))), &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func manifestStatus(m map[string]any) string { return m["run"].(map[string]any)["status"].(string) }

// covers: MA-445, R12
// Settings: the remembered destination and options round-trip in the settings file (0600,
// version 1; Preserve ON, Readable HTML ON from slice E5, attachments OFF when
// absent); an older file without the fields loads with the defaults; an unknown field a
// newer build wrote is tolerated; a start reloads the file under the lock and changes only
// the four export fields.
func TestExportSettingsRoundTrip(t *testing.T) {
	e := newWsEnv(t)
	path := e.cfg.SettingsPath
	if s := LoadSettings(path); !s.Preserve() || !s.ReadableHTML() || s.ExtractAttachments() || s.ExportDest != "" {
		t.Errorf("a missing file: %+v, want the defaults", s)
	}
	old := `{"version":1,"out":"/somewhere/archive","keepRaw":true}`
	os.WriteFile(path, []byte(old), 0o600)
	if s := LoadSettings(path); !s.Preserve() || !s.ReadableHTML() || s.ExtractAttachments() || s.Out != "/somewhere/archive" || !s.KeepRaw {
		t.Errorf("an older file: %+v, want its own fields and the export defaults", s)
	}
	os.WriteFile(path, []byte(`{"version":1,"out":"/somewhere/archive","keepRaw":true,"someFutureField":{"a":[1,2]},"exportDest":"/d"}`), 0o600)
	if s := LoadSettings(path); s.ExportDest != "/d" || s.Out != "/somewhere/archive" {
		t.Errorf("a file with an unknown field from a newer build: %+v", s)
	}
	os.WriteFile(path, []byte(old), 0o600)

	// the source endpoint reports the defaults, Readable HTML ON (slice E5); the page offers
	// it, checked, and still not the option that waits for its slice
	src := e.ok("/api/workspace/source", nil)["settings"].(map[string]any)
	if src["preserve"] != true || src["html"] != true || src["attachments"] != false || src["dest"] != "" {
		t.Errorf("the defaults the page sees: %v", src)
	}
	page := e.get("/workspace").Body.String()
	if !strings.Contains(page, `<input type="checkbox" id="optHTML" checked> Readable HTML`) {
		t.Error("the workspace page does not offer Readable HTML, checked")
	}
	for _, hidden := range []string{"Extract attachments", "optAttach"} {
		if strings.Contains(page, hidden) {
			t.Errorf("the workspace page offers %q before its slice lands", hidden)
		}
	}

	// a start remembers the destination and the options, and nothing else changes; the
	// option that waits for its slice stays off whatever the request says
	ids := e.folders()
	sel := e.selection(entry(ids["Sent Items"], false))
	sel["options"] = map[string]any{"preserve": false, "html": false, "attachments": true}
	e.startExport(sel)
	e.waitRun()
	s := LoadSettings(path)
	if s.Out != "/somewhere/archive" || !s.KeepRaw {
		t.Errorf("a start changed fields that are not its own: %+v", s)
	}
	if s.ExportDest != e.dest || s.Preserve() || s.ExportHTML == nil || *s.ExportHTML || s.ExportAttachments == nil || *s.ExportAttachments {
		t.Errorf("the remembered export fields: %+v", s)
	}
	raw := readFileString(t, path)
	if !strings.Contains(raw, `"version": 1`) || !strings.Contains(raw, `"exportPreserve": false`) {
		t.Errorf("the settings file: %s", raw)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("the settings file's mode: %v %v", fi, err)
		}
	}
	src = e.ok("/api/workspace/source", nil)["settings"].(map[string]any)
	if src["dest"] != e.dest || src["preserve"] != false || src["html"] != false || src["attachments"] != false {
		t.Errorf("the remembered settings the page sees: %v", src)
	}
}

// covers: MA-524, R12, R5
// Settings serialization: an export start racing a /api/settings save never writes back a
// stale Out, and the save never drops the start's fields, in both orders (the interleaving is
// injected between the file's load and its write).
func TestSettingsWritesAreSerialized(t *testing.T) {
	archive2 := t.TempDir() // outside the settings folder, so the save accepts it
	for _, order := range []string{"the start loads first", "the save loads first"} {
		t.Run(order, func(t *testing.T) {
			e := newWsEnv(t)
			os.WriteFile(e.cfg.SettingsPath, []byte(`{"version":1,"out":"`+strings.ReplaceAll(e.out, `\`, `\\`)+`"}`), 0o600)
			ids := e.folders()
			pid, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))

			var wg sync.WaitGroup
			doSave := func() {
				defer wg.Done()
				body, _ := json.Marshal(map[string]any{"out": archive2, "keepRaw": true})
				req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body)))
				req.Host = "127.0.0.1:8097"
				req.Header.Set("Origin", "http://127.0.0.1:8097")
				req.Header.Set("X-CSRF-Token", e.d.csrf)
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				if !strings.Contains(rec.Body.String(), `"ok":true`) {
					t.Errorf("the save: %d %s", rec.Code, rec.Body.String())
				}
			}
			doStart := func() {
				defer wg.Done()
				body, _ := json.Marshal(map[string]any{"previewId": pid})
				req := httptest.NewRequest(http.MethodPost, "/api/exports/start", strings.NewReader(string(body)))
				req.Host = "127.0.0.1:8097"
				req.Header.Set("Origin", "http://127.0.0.1:8097")
				req.Header.Set("X-CSRF-Token", e.d.csrf)
				req.Header.Set("X-Epoch", e.d.boot)
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				if !strings.Contains(rec.Body.String(), `"ok":true`) {
					t.Errorf("the start: %d %s", rec.Code, rec.Body.String())
				}
			}
			var once sync.Once
			settingsHook = func(who string) {
				once.Do(func() {
					switch {
					case order == "the start loads first" && who == "export":
						wg.Add(1)
						go doSave()
					case order == "the save loads first" && who == "settings":
						wg.Add(1)
						go doStart()
					default:
						return
					}
					time.Sleep(400 * time.Millisecond) // the other writer reaches its own load meanwhile
				})
			}
			t.Cleanup(func() { settingsHook = nil })
			if order == "the start loads first" {
				wg.Add(1)
				doStart()
			} else {
				wg.Add(1)
				doSave()
			}
			wg.Wait()
			e.waitRun()
			s := LoadSettings(e.cfg.SettingsPath)
			if s.Out != archive2 || !s.KeepRaw {
				t.Errorf("%s: the save's fields are lost: Out %q, KeepRaw %v (a stale Out was written back)", order, s.Out, s.KeepRaw)
			}
			if s.ExportDest != e.dest {
				t.Errorf("%s: the start's fields are lost: %+v", order, s)
			}
		})
	}
}

func (e *wsEnv) signInStatus(answer string) map[string]any {
	e.t.Helper()
	body := map[string]any{}
	if answer != "" {
		body["answer"] = answer
	}
	return e.ok("/api/m365/signin-status", body)
}

func (e *wsEnv) waitSignIn(state string) map[string]any {
	e.t.Helper()
	for i := 0; i < 600; i++ {
		out := e.signInStatus("")
		if out["state"] == state {
			return out
		}
		if out["state"] == "failed" && state != "failed" {
			e.t.Fatalf("the sign-in failed: %v", out)
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatalf("the sign-in never reached %q", state)
	return nil
}

// covers: MA-404, R17, R19, S41
// The sign-in-only POST runs graph.NewDelegated and Me in-process against an in-memory token
// store and saves the token afterwards; it is a job in its own slot under one process-wide
// sign-in mutex shared with capture's device flow (a concurrent capture sign-in is refused,
// and a sign-in while a capture signs in); the device code is served only by the guarded
// signin-status POST, never by GET /api/activity; with a weekly backup installed, a
// sign-in as another account than the previous signer is not saved until the user answers
// Keep or Sign out; an untokened POST starts no device flow.
func TestSignInRunsInItsOwnSlot(t *testing.T) {
	e := newWsEnv(t)
	os.Remove(e.tok) // not signed in
	if out := e.ok("/api/workspace/source", nil)["m365"].(map[string]any); out["state"] != "not-signed-in" {
		t.Fatalf("not signed in: %v", out)
	}
	flows := e.fg.count("/devicecode")

	// an untokened start starts no device flow
	req := httptest.NewRequest(http.MethodPost, "/api/m365/signin", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || e.fg.count("/devicecode") != flows {
		t.Errorf("an untokened sign-in: %d, device flows %d -> %d", rec.Code, flows, e.fg.count("/devicecode"))
	}

	// a capture holding the device flow refuses a sign-in here
	e.d.flow.hold("capture")
	code, out := e.post("/api/m365/signin", nil)
	refusedBody(t, code, out, "R-m", "already in progress", "Archive now")
	e.d.flow.release("capture")

	// the sign-in: a job whose code only the guarded status POST serves
	e.ok("/api/m365/signin", nil)
	st := e.waitSignIn("running")
	for st["device"] == nil {
		time.Sleep(10 * time.Millisecond)
		st = e.signInStatus("")
	}
	dev := st["device"].(map[string]any)
	if dev["code"] != "WXYZ-1234" || !strings.HasPrefix(dev["url"].(string), "https://") {
		t.Errorf("the device code: %v", dev)
	}
	act := httptest.NewRecorder()
	e.h.ServeHTTP(act, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	if strings.Contains(act.Body.String(), "WXYZ-1234") {
		t.Errorf("GET /api/activity served the workspace's device code: %s", act.Body.String())
	}
	bare := httptest.NewRequest(http.MethodPost, "/api/m365/signin-status", strings.NewReader("{}"))
	bare.Host = "127.0.0.1:8097"
	bare.Header.Set("Origin", "http://evil.example")
	bareRec := httptest.NewRecorder()
	e.h.ServeHTTP(bareRec, bare)
	if bareRec.Code != http.StatusForbidden || strings.Contains(bareRec.Body.String(), "WXYZ") {
		t.Errorf("an unguarded status POST: %d %s", bareRec.Code, bareRec.Body.String())
	}
	if _, err := os.Stat(e.tok); !os.IsNotExist(err) {
		t.Errorf("the token was saved before the sign-in finished: %v", err)
	}
	if out := e.ok("/api/m365/signin", nil); out["already"] != true {
		t.Errorf("a second sign-in while one runs: %v", out)
	}
	// a capture started now is refused: the sign-in holds the process-wide mutex
	capRec := e.postRaw("/api/capture", "{}")
	if !strings.Contains(capRec.Body.String(), "already in progress") {
		t.Errorf("a capture during the sign-in: %s", capRec.Body.String())
	}
	done := e.waitSignIn("done")
	if done["upn"] != "alice@contoso.org" || done["account"] != `DESKTOP-7\alex` || !strings.Contains(done["notice"].(string), "only in a window it opened from its shortcut") {
		t.Errorf("the finished sign-in: %v", done)
	}
	if upn, ok := e.d.signedInNow(); !ok || upn != "alice@contoso.org" {
		t.Errorf("after the sign-in: %q %v", upn, ok)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(e.tok); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("the saved sign-in: %v %v", fi, err)
		}
	}
	if e.d.flow.busy() != "" {
		t.Errorf("the device-flow mutex is still held by %q", e.d.flow.busy())
	}

	// with a weekly backup installed, a sign-in as another account waits for the answer
	e.cfg.BackupInstalled = func() bool { return true }
	e.d.cfg.BackupInstalled = e.cfg.BackupInstalled
	e.fg.mutate(func() { e.fg.upn = "bob@contoso.org" })
	for _, answer := range []string{"signout", "keep"} {
		before := readFileString(t, e.tok)
		e.ok("/api/m365/signin", nil)
		c := e.waitSignIn("confirm")
		text := c["confirm"].(map[string]any)["text"].(string)
		if !strings.Contains(text, "bob@contoso.org") || !strings.Contains(text, e.out) {
			t.Errorf("the confirmation: %q", text)
		}
		if readFileString(t, e.tok) != before {
			t.Fatalf("the new sign-in was saved before the answer")
		}
		e.signInStatus(answer)
		fin := e.waitSignIn("done")
		switch answer {
		case "signout":
			if fin["signedOut"] != true || readFileString(t, e.tok) != before {
				t.Errorf("Sign out: %v, token changed %v", fin, readFileString(t, e.tok) != before)
			}
		case "keep":
			if upn, _ := e.d.signedInNow(); upn != "bob@contoso.org" {
				t.Errorf("Keep: signed in as %q", upn)
			}
		}
	}
}

// covers: MA-384, R23
// The extraction workspace's own sign-in surface carries the account banner and the "a
// sign-in code only in a window opened from the shortcut" copy [CC3, CC23]: on the workspace
// page, on the Overview's Microsoft 365 card when signed out, and in the status POST's own
// answer.
func TestWorkspaceSignInSurfaceCarriesTheAccountBanner(t *testing.T) {
	e := newWsEnv(t)
	os.Remove(e.tok)
	status := e.signInStatus("")
	if status["account"] != `DESKTOP-7\alex` || !strings.Contains(status["notice"].(string), `running as DESKTOP-7\alex`) ||
		!strings.Contains(status["notice"].(string), "only in a window it opened from its shortcut") {
		t.Errorf("the status POST's banner and notice: %v", status)
	}
	for _, p := range []string{"/workspace", "/"} {
		page := e.get(p).Body.String()
		_, ids, _ := sidebarNav(t, page)
		for _, id := range []string{"signInBox", "signInBanner", "signInNotice", "signInCode"} {
			if !ids[id] {
				t.Errorf("%s: the sign-in surface lacks #%s", p, id)
			}
		}
	}
	if page := e.get("/workspace").Body.String(); !strings.Contains(page, `DESKTOP-7\alex`) || !strings.Contains(page, "running as") {
		t.Error("the workspace page does not name the account the dashboard runs as")
	}
	if !strings.Contains(signinJS, "only") && !strings.Contains(signinJS, "d.notice") {
		t.Error("the sign-in script does not show the notice")
	}
}

// covers: MA-444, R29, R4, R12
// The destination is validated again at start: a destination deleted, or turned into a
// symlink into the archive, between preview and start is refused with the side effect
// absent (the archive untouched, no export folder); the positive twin starts.
func TestDestinationIsCheckedAgainAtStart(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	preview := func(dest string) string {
		sel := e.selection(entry(ids["Sent Items"], false))
		sel["dest"] = dest
		id, _ := e.preview(sel)
		return id
	}
	dest := filepath.Join(e.dir, "dest2")
	os.Mkdir(dest, 0o755)

	// deleted between preview and start
	id := preview(dest)
	os.Remove(dest)
	code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
	refusedBody(t, code, out, "R-e", "doesn't exist", "Create folder")

	// turned into a symlink into the archive
	os.WriteFile(filepath.Join(e.out, "index.html"), []byte("archive"), 0o600)
	os.Mkdir(dest, 0o755)
	id = preview(dest)
	archive := treeState(t, e.out)
	os.Remove(dest)
	if err := os.Symlink(e.out, dest); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	code, out = e.post("/api/exports/start", map[string]any{"previewId": id})
	if out["ok"] != false || (out["id"] != "R-f" && out["id"] != "R-e" && out["id"] != "R-k") {
		t.Fatalf("a destination swapped for a link into the archive: %d %v", code, out)
	}
	if code != statusRefused {
		t.Errorf("the refusal's status: %d", code)
	}
	if got := treeState(t, e.out); !reflect.DeepEqual(got, archive) {
		t.Errorf("a refused start wrote into the archive: %v", diffKeys(archive, got))
	}
	if e.d.exportLive() {
		t.Error("a refused start left an export running")
	}

	// the positive twin
	os.Remove(dest)
	os.Mkdir(dest, 0o755)
	id = preview(dest)
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if last := e.waitRun()["last"].(map[string]any); last["status"] != "complete" {
		t.Errorf("the positive twin: %v", last)
	}
}

// covers: MA-518, R29, R12
// Free-space policy: Start refuses with R-g2 when the estimate plus the reserve exceeds the
// free bytes, with the side effect absent; the preview reports the reserve, sized from the
// plan (150-character ids give more than 1 KB per message), and R-g2 compares against it; the
// floor is also checked on the state directory's volume against the run record's bound.
func TestFreeSpacePolicyRefusesAtStart(t *testing.T) {
	e := newWsEnv(t)
	// a folder of 20 messages with ~150-character ids
	e.fg.addFolder("F-LONGID", "Long ids", "", 0)
	e.fg.mutate(func() {
		f := e.fg.folder("F-LONGID")
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("AAMkAGI2NzM3YTM1LTU1ZWMtNDlmOS04YzQxLTFhNWNmZDU3N2I4OQBGAAAAAAB%s%05d", strings.Repeat("Q", 80), i)
			f.Msgs = append(f.Msgs, fgMsg{ID: id, Subject: "s", From: "a@example.com", Received: base.Add(-time.Duration(i) * time.Minute), Size: 100})
		}
	})
	ids := e.folders()
	sel := e.selection(entry(ids["Long ids"], false))
	sel["options"] = map[string]any{"preserve": true, "html": true} // hidden before E5: the reserve is the same
	pid, res := e.preview(sel)
	reserve, estimate := int64(res["reserve"].(float64)), int64(res["estimateBytes"].(float64))
	if reserve <= 20*1024 {
		t.Errorf("the reserve is %d bytes for 20 messages with 150-character ids, want more than 1 KB each", reserve)
	}
	need := uint64(estimate + reserve)

	// warm the state directory with one good run first, so the refusals below are compared
	// against a state directory that already exists
	e.startExport(e.selection(entry(ids["Sent Items"], false)))
	e.waitRun()
	pid, _ = e.preview(sel)
	destBefore, stateBefore := treeState(t, e.dest), treeState(t, e.state)
	env := e.d.ex.env

	env.SetFreeBytes(func(string) (uint64, error) { return need - 1, nil }, nil)
	code, out := e.post("/api/exports/start", map[string]any{"previewId": pid})
	refusedBody(t, code, out, "R-g2", "free", "Free some space")
	if got := treeState(t, e.dest); !reflect.DeepEqual(got, destBefore) {
		t.Errorf("a refused start wrote into the destination: %v", diffKeys(destBefore, got))
	}
	if got := treeState(t, e.state); !reflect.DeepEqual(got, stateBefore) {
		t.Errorf("a refused start wrote into the state directory: %v", diffKeys(stateBefore, got))
	}

	// the floor on the state volume
	pid, _ = e.preview(sel)
	bound := uint64(res["recordBound"].(float64))
	env.SetFreeBytes(nil, func(string) (uint64, error) { return bound - 1, nil })
	code, out = e.post("/api/exports/start", map[string]any{"previewId": pid})
	refusedBody(t, code, out, "R-d", e.state)
	if got := treeState(t, e.dest); !reflect.DeepEqual(got, destBefore) {
		t.Errorf("a refused start wrote into the destination: %v", diffKeys(destBefore, got))
	}

	// exactly enough is enough (the positive twin)
	pid, _ = e.preview(sel)
	env.SetFreeBytes(func(string) (uint64, error) { return need, nil }, func(string) (uint64, error) { return bound, nil })
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	if last := e.waitRun()["last"].(map[string]any); last["status"] != "complete" {
		t.Errorf("with exactly the free space needed: %v", last)
	}
}

// covers: MA-529, R12, S52
// Structural: Browse is disabled while an export runs (the endpoint refuses and the picker
// seam is not invoked), and CI runs govulncheck for GOOS=windows on ./cmd/mailarchive-desktop.
func TestBrowseIsDisabledDuringARunAndCIScansWindows(t *testing.T) {
	e := newWsEnv(t)
	dialogs := 0
	e.d.pick = func(context.Context, string) (string, error) { dialogs++; return e.dest, nil }
	if out := e.ok("/api/workspace/browse", nil); out["path"] != e.dest || dialogs != 1 {
		t.Fatalf("the positive twin: %v, %d dialogs", out, dialogs)
	}
	ids := e.folders()
	e.slow("F-SENT", 150*time.Millisecond)
	e.startExport(e.selection(entry(ids["Sent Items"], false)))
	_, out := e.post("/api/workspace/browse", nil)
	if out["ok"] != false || out["disabled"] != true || !strings.Contains(out["error"].(string), "Browse is off while an export runs") || !strings.Contains(out["error"].(string), "type the folder's path") {
		t.Errorf("Browse during a run: %v", out)
	}
	if dialogs != 1 {
		t.Errorf("Browse during a run opened a dialog (%d)", dialogs)
	}
	e.waitRun()
	if out := e.ok("/api/workspace/browse", nil); out["path"] != e.dest || dialogs != 2 {
		t.Errorf("Browse after the run: %v, %d dialogs", out, dialogs)
	}

	ci := readFileString(t, filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	i := strings.Index(ci, "GOOS=windows")
	if i < 0 {
		t.Fatal("CI never sets GOOS=windows")
	}
	step := ci[i:]
	if j := strings.Index(step, "\n      - "); j >= 0 {
		step = step[:j]
	}
	if !strings.Contains(step, "govulncheck") || !strings.Contains(step, "./cmd/mailarchive-desktop") {
		t.Errorf("CI's Windows step does not run govulncheck on ./cmd/mailarchive-desktop: %q", step)
	}
}

// covers: MA-514, R29, R5
// Shutdown: cancelling the process's root context drains the lifecycle, which cancels the
// run, waits for its finalization (manifest status cancelled, marker CANCELLED) and finishes
// within the 15 s budget.
func TestDrainCancelsAndFinalizesTheRun(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.slow("F-INBOX", 100*time.Millisecond)
	e.d.ex.env.SetStepHook(func(step string) {
		if step == "W9a" { // finalization is slow: Drained must wait for it
			time.Sleep(500 * time.Millisecond)
		}
	})
	e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitDone(2)
	began := time.Now()
	e.stop()
	select {
	case <-e.life.Drained():
	case <-time.After(DrainBudget + 5*time.Second):
		t.Fatal("the lifecycle never drained")
	}
	if took := time.Since(began); took > DrainBudget {
		t.Errorf("the drain took %v, over the %v budget", took, DrainBudget)
	}
	_, p := e.post("/api/exports/progress", nil)
	last, _ := p["last"].(map[string]any)
	if p["running"] != false || last == nil || last["status"] != "cancelled" {
		t.Fatalf("when Drained closed the run was %v (last %v): it must be finalized", p["running"], last)
	}
	m := e.manifestOf(last["rootName"].(string))
	if manifestStatus(m) != "cancelled" {
		t.Errorf("the manifest's status is %q, want cancelled", manifestStatus(m))
	}
	marker := readFileString(t, filepath.Join(e.dest, last["rootName"].(string), "_EXPORT-NOT-FINISHED.txt"))
	if !strings.Contains(marker, "CANCELLED") {
		t.Errorf("the marker: %q", marker)
	}
	if n := last["exported"].(float64); n < 2 || n >= 7 {
		t.Errorf("the cancelled run exported %v of 7", n)
	}
}

// covers: MA-530, R17, R29, S41, S52
// Sign-in changes at the desktop surface: Clear sign-in during a run stops it with R-z,
// finalized (manifest failed, marker STOPPED) and retryable from the run record; a sign-in
// as B during A's run is R-z naming both; an out-of-process change seen by the
// every-100-messages poll is R-z; a refresh token the token endpoint refuses shows R-b or R-c
// in the list and preview endpoints, never raw text.
func TestSignInChangesStopTheRun(t *testing.T) {
	t.Run("clear sign-in", func(t *testing.T) {
		e := newWsEnv(t)
		ids := e.folders()
		e.slow("F-INBOX", 80*time.Millisecond)
		e.startExport(e.selection(entry(ids["Inbox"], false)))
		e.waitDone(2)
		if rec := e.postRaw("/api/clear-signin", "{}"); rec.Code != http.StatusOK {
			t.Fatalf("clear sign-in: %d %s", rec.Code, rec.Body.String())
		}
		last := e.waitRun()["last"].(map[string]any)
		if last["status"] != "failed" || last["stopId"] != "R-z" || !strings.Contains(last["stop"].(string), "alice@contoso.org") {
			t.Fatalf("the run after Clear sign-in: %v", last)
		}
		if manifestStatus(e.manifestOf(last["rootName"].(string))) != "failed" {
			t.Error("the manifest does not say failed")
		}
		if marker := readFileString(t, filepath.Join(e.dest, last["rootName"].(string), "_EXPORT-NOT-FINISHED.txt")); !strings.Contains(marker, "STOPPED") {
			t.Errorf("the marker: %q", marker)
		}
		// retryable from the run record after signing in again as the same account
		e.writeToken("alice@contoso.org", time.Hour)
		code, h := e.post("/api/exports/history", nil)
		if code != http.StatusOK || !hasAction(h, "export-remaining") {
			t.Errorf("the stopped run's offers: %v", h)
		}
	})
	t.Run("another account", func(t *testing.T) {
		e := newWsEnv(t)
		ids := e.folders()
		e.slow("F-INBOX", 80*time.Millisecond)
		e.startExport(e.selection(entry(ids["Inbox"], false)))
		e.waitDone(2)
		e.writeToken("bob@contoso.org", time.Hour)
		e.d.signInChanged("bob@contoso.org")
		last := e.waitRun()["last"].(map[string]any)
		stop, _ := last["stop"].(string)
		if last["stopId"] != "R-z" || !strings.Contains(stop, "alice@contoso.org") || !strings.Contains(stop, "bob@contoso.org") {
			t.Errorf("the run after a sign-in as B: %v", last)
		}
	})
	t.Run("an out-of-process change", func(t *testing.T) {
		e := newWsEnv(t)
		e.fg.addFolder("F-MANY", "Many", "", 150)
		e.fg.mutate(func() {
			for _, m := range e.fg.folder("F-MANY").Msgs {
				id := m.ID
				e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) { time.Sleep(8 * time.Millisecond); w.Write(fgBody(id)) }
			}
		})
		ids := e.folders()
		e.startExport(e.selection(entry(ids["Many"], false)))
		e.waitDone(3)
		e.writeToken("bob@contoso.org", time.Hour) // another process signed in as B; no one told the dashboard
		last := e.waitRun()["last"].(map[string]any)
		if last["stopId"] != "R-z" || last["exported"].(float64) < 90 || last["exported"].(float64) > 101 {
			t.Errorf("the run after an out-of-process change: %v (want R-z at the 100-message poll)", last)
		}
	})
	t.Run("a refused refresh token", func(t *testing.T) {
		e := newWsEnv(t)
		e.fg.mutate(func() { e.fg.ttl = 2 }) // every use refreshes
		e.writeToken("alice@contoso.org", -time.Hour)
		ids := e.folders()
		sel := e.selection(entry(ids["Inbox"], false))
		clean := func(out map[string]any, id string) {
			t.Helper()
			msg, _ := out["error"].(string)
			if out["id"] != id {
				t.Errorf("want %s, got %v", id, out)
			}
			assure.Refused(t, statusRefused, msg, assure.Code(statusRefused), assure.Forbid("invalid_grant", "SECRET-RT-VALUE", "AADSTS", "oauth2", e.fg.srv.URL, "panic"))
		}
		e.fg.mutate(func() { e.fg.tokenErr = "invalid_grant" })
		code, out := e.post("/api/workspace/messages", map[string]any{"folder": ids["Inbox"]})
		if code != statusRefused {
			t.Errorf("a list with a refused refresh token: %d", code)
		}
		clean(out, "R-c")
		code, out = e.post("/api/workspace/preview", sel) // the dropped session is rebuilt, and refused at once
		if code != statusRefused {
			t.Errorf("a preview with a refused refresh token: %d", code)
		}
		clean(out, "R-c")
		e.fg.mutate(func() { e.fg.tokenErr = "" })
		ids = e.folders() // a live session again, whose next refresh is refused mid-preview
		e.fg.mutate(func() { e.fg.tokenErr = "invalid_grant" })
		clean(e.previewFailure(sel), "R-c")
		e.fg.mutate(func() { e.fg.tokenErr = "" })

		os.Remove(e.tok) // cleared: R-b
		e.d.signInChanged("")
		code, out = e.post("/api/workspace/folders", nil)
		if code != statusRefused {
			t.Errorf("folders with no sign-in: %d", code)
		}
		clean(out, "R-b")
	})
}

// hasAction reports whether any card of a history answer offers the action.
func hasAction(h map[string]any, action string) bool {
	runs, _ := h["runs"].([]any)
	for _, r := range runs {
		for _, a := range r.(map[string]any)["actions"].([]any) {
			if a == action {
				return true
			}
		}
	}
	return false
}
