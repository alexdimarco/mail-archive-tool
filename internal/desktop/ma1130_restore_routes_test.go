package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbengine"
	"mail-archive-tool/internal/tbrestore"
)

var restoreRoutes = []string{"/api/restore-tb/profiles", "/api/restore-tb/folders", "/api/restore-tb/plan", "/api/restore-tb/start"}

// covers: MA-1130, R23, R40
// The four Restore to Thunderbird routes are guarded POSTs through d.api (design §7.3): GET is
// 405 with Allow: POST; a POST with no token, a wrong token, a foreign Origin or a foreign Host
// is 403 and refused; a body over the cap is 413; each answers JSON with nosniff and no-store.
// A refused request changes nothing: Local Folders, restores.log and the plan's token as they
// were. Positive twin first: the page's own POSTs list the profiles and plan the restore.
func TestTheRestoreRoutesAreGuardedPosts(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 1)
	p := e.planFor(0)
	token, _ := p["token"].(string)
	assure.Reached(t, p["ok"] == true && token != "", "the positive twin: the page's own POST plans the restore")

	before := rtbSnapshot(t, e.profiles[0].dir)
	body, _ := json.Marshal(map[string]any{"token": token, "listID": "x", "position": 0, "whole": true})
	for _, route := range restoreRoutes {
		rec := e.req(http.MethodGet, route, nil, nil)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Errorf("GET %s: %d (Allow %q), want 405 with Allow: POST", route, rec.Code, rec.Header().Get("Allow"))
		}
		for what, mut := range map[string]func(*http.Request){
			"no token":         func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
			"a wrong token":    func(r *http.Request) { r.Header.Set("X-CSRF-Token", "not-the-token") },
			"a foreign Origin": func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
			"a foreign Host":   func(r *http.Request) { r.Host = "evil.example:8097" },
		} {
			rec := e.req(http.MethodPost, route, string(body), mut)
			var out map[string]any
			json.Unmarshal(rec.Body.Bytes(), &out)
			msg, _ := out["error"].(string)
			assure.Refused(t, rec.Code, msg, assure.Code(http.StatusForbidden), assure.Names("refused"))
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s with %s: headers %v, want nosniff and no-store", route, what, rec.Header())
			}
		}
		huge := `{"token":"` + strings.Repeat("x", 70<<10) + `"}`
		rec = e.req(http.MethodPost, route, huge, nil)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		msg, _ := out["error"].(string)
		assure.Refused(t, rec.Code, msg, assure.Code(http.StatusRequestEntityTooLarge), assure.Names("too large"))
	}
	assure.Refused(t, http.StatusForbidden, "refused", assure.Code(http.StatusForbidden), assure.Names("refused"),
		assure.NoSideEffect(func() bool {
			return rtbSnapshot(t, e.profiles[0].dir) == before && e.d.pending.get(token, isRestorePlan) != nil
		}))
}

// covers: MA-1130, R40
// The start takes only its own plan's token, once (design §7.3): a delete plan's token is not a
// restore's (refused, the delete plan left waiting), a restore's is not a delete's (refused at
// Delete permanently, the restore plan left waiting); the restore's token starts it once and the
// same token again is refused with nothing more written. Profile and folder ids are the server's
// own: a list ID the server did not send, a position past the list, and a folder id not in the
// archive's tree are refused with nothing planned. Positive twin first: the token starts the
// restore and its folder is made in Thunderbird.
func TestTheRestoreStartTokenIsOneUseAndOfItsOwnKind(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 2)
	local := e.profiles[1].local

	// Ids the server did not send.
	pr := e.profilesReply()
	for _, c := range []struct {
		body  map[string]any
		names []string
	}{
		{map[string]any{"listID": "0123456789abcdef", "position": 1, "whole": true}, []string{"profiles changed", "again"}},
		{map[string]any{"listID": pr["listID"], "position": 7, "whole": true}, []string{"profiles changed", "again"}},
		{map[string]any{"listID": pr["listID"], "position": 1, "whole": false, "folders": []string{"../../etc"}}, []string{"no longer in this archive", "nothing was planned"}},
	} {
		m := e.post("/api/restore-tb/plan", c.body)
		msg, _ := m["error"].(string)
		assure.Refused(t, 1, msg, assure.Code(1), assure.Names(c.names...), assure.NoSideEffect(func() bool { return m["token"] == nil && len(restoredTops(t, local)) == 0 }))
	}
	// The tree, too, is answered only for the list the server sent (RT-15).
	ft := e.post("/api/restore-tb/folders", map[string]any{"listID": "0123456789abcdef", "position": 1})
	assure.Refused(t, 1, str(ft["error"]), assure.Code(1), assure.Names("profiles changed", "again"), assure.NoSideEffect(func() bool { return ft["folders"] == nil }))

	// Another kind's token is not the restore's, and the restore's is not another kind's.
	delPlan := e.post("/api/archive/delete/plan", map[string]any{"path": e.out})
	delTok, _ := delPlan["token"].(string)
	assure.Reached(t, delTok, "a delete plan's token")
	p := e.planFor(1)
	tok, _ := p["token"].(string)
	assure.Reached(t, tok, "the restore plan's token")
	m := e.post("/api/restore-tb/start", map[string]any{"token": delTok})
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("no longer planned"),
		assure.NoSideEffect(func() bool { return e.d.pending.get(delTok, isDeletePlan) != nil && len(restoredTops(t, local)) == 0 }))
	m = e.post("/api/archive/delete", map[string]any{"token": tok, "typed": filepath.Base(e.out)})
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { _, err := os.Stat(e.out); return err == nil && e.d.pending.get(tok, isRestorePlan) != nil }))

	// Once: the positive twin, then the same token again.
	s := e.post("/api/restore-tb/start", map[string]any{"token": tok})
	assure.Reached(t, s["ok"] == true, "the restore's own token starts it")
	a := e.waitDone()
	r, _ := a["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true && len(restoredTops(t, local)) == 2, "the restore finished and made its folder (the file and its .sbd)")
	before := rtbSnapshot(t, e.profiles[1].dir)
	again := e.post("/api/restore-tb/start", map[string]any{"token": tok})
	assure.Refused(t, 1, str(again["error"]), assure.Code(1), assure.Names("no longer planned", "Check again"),
		assure.NoSideEffect(func() bool { return again["planUsed"] == true && rtbSnapshot(t, e.profiles[1].dir) == before }))
}

// covers: MA-1130, MA-1128, R37, R40, S72
// The tick and the second confirmation are checked on the server (design §6.2 notes, RT-8): an
// encrypted plan started without notEncryptedOK is refused in W-TICK's words, and one that asks
// twice (Local Folders in a synced folder) without confirmAgain in W-NOTAGAIN's, each with
// nothing written and the plan still waiting for the box the reply names; with both, it runs.
// The page's ticks are only how the person says it.
func TestTheRestoreTickAndSecondYesAreCheckedOnTheServer(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newRtbEnv(t, out, filepath.Join(t.TempDir(), "Dropbox", "Thunderbird"), 1)
	e.unlock()
	p := e.planFor(0)
	tok, _ := p["token"].(string)
	assure.Reached(t, p["ok"] == true && p["encrypted"] == true && p["askTwice"] == true && strings.Contains(str(p["againLine"]), "Dropbox"),
		"an encrypted plan into a synced Local Folders asks for the tick and a second yes")
	before := rtbSnapshot(t, e.profiles[0].dir)
	unchanged := func() bool {
		return rtbSnapshot(t, e.profiles[0].dir) == before && e.d.pending.get(tok, isRestorePlan) != nil
	}

	m := e.post("/api/restore-tb/start", map[string]any{"token": tok, "confirmAgain": true})
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("will not be encrypted", "not ticked", "Tick it"), assure.NoSideEffect(func() bool { return m["need"] == "tick" && unchanged() }))
	m = e.post("/api/restore-tb/start", map[string]any{"token": tok, "notEncryptedOK": true})
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("Dropbox", "not confirmed a second time"), assure.NoSideEffect(func() bool { return m["need"] == "again" && unchanged() }))

	s := e.post("/api/restore-tb/start", map[string]any{"token": tok, "notEncryptedOK": true, "confirmAgain": true})
	assure.Reached(t, s["ok"] == true, "with both, the restore starts")
	a := e.waitDone()
	r, _ := a["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true && strings.HasPrefix(str(res["words"]), "Restored 1 message"), "the encrypted restore finished")
}

// covers: MA-1130, R40
// The restore plan lives under a key of its own (design §7.3 foldcheck RT-13): a pending copy
// plan of the same archive survives a restore plan; a second restore plan into the same profile
// replaces the first; a settings save that keeps the location keeps the plan, and a save that
// switches the current archive ends it, as the switch's afterSwitch does; a Lock leaves it, and
// Start then refuses it in W-LOCKED's words with nothing written.
func TestTheRestorePlanIsKeptUnderItsOwnKey(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newRtbEnv(t, out, "", 1)
	e.unlock()
	parent := t.TempDir()
	e.d.pick = func(context.Context, string) (string, error) { return parent, nil }
	cp := e.post("/api/encryption/copy/plan", nil)
	copyTok, _ := cp["token"].(string)
	assure.Reached(t, copyTok, "a copy plan's token")

	first, _ := e.planFor(0)["token"].(string)
	assure.Reached(t, first, "a restore plan's token")
	assure.Reached(t, e.d.pending.get(copyTok, isCopyPlan) != nil, "the pending copy plan survives a restore plan")
	second, _ := e.planFor(0)["token"].(string)
	assure.Reached(t, second != first && e.d.pending.get(first, isRestorePlan) == nil && e.d.pending.get(second, isRestorePlan) != nil,
		"a second plan into the same profile replaces the first")

	// A save that keeps the location.
	sv := e.post("/api/settings", map[string]any{"Out": out, "DrawnFor": out})
	assure.Reached(t, sv["ok"] == true && e.d.pending.get(second, isRestorePlan) != nil, "a settings save that keeps the location keeps the plan")

	// A Lock leaves it; Start then refuses it, nothing written.
	before := rtbSnapshot(t, e.profiles[0].dir)
	l := e.post("/api/encryption/lock", nil)
	assure.Reached(t, l["ok"] == true && e.d.pending.get(second, isRestorePlan) != nil, "a Lock leaves the restore plan")
	s := e.post("/api/restore-tb/start", map[string]any{"token": second, "notEncryptedOK": true})
	assure.Reached(t, s["ok"] == true, "the start is taken; the engine refuses it under its locks")
	a := e.waitDone()
	r, _ := a["restore"].(map[string]any)
	assure.Refused(t, 1, str(r["error"]), assure.Code(1), assure.Names("locked", "Unlock it", "Check again"),
		assure.NoSideEffect(func() bool { return rtbSnapshot(t, e.profiles[0].dir) == before }))

	// A switch ends it: by a save, and by afterSwitch.
	e.unlock()
	third, _ := e.planFor(0)["token"].(string)
	other := filepath.Join(t.TempDir(), "Other")
	os.MkdirAll(other, 0o700)
	sw := e.post("/api/settings", map[string]any{"Out": other, "DrawnFor": out})
	assure.Reached(t, sw["ok"] == true && e.d.pending.get(third, isRestorePlan) == nil, "a save that switches the current archive ends the plan")
	SaveSettings(e.cfg.SettingsPath, Settings{Out: out})
	e.unlock()
	fourth, _ := e.planFor(0)["token"].(string)
	assure.Reached(t, fourth, "a plan after the switch back")
	e.d.afterSwitch()
	assure.Reached(t, e.d.pending.get(fourth, isRestorePlan) == nil, "afterSwitch ends the plan, beside delete plans")
}

// covers: MA-1130, R40
// INS-07 (the R6 adversarial pass, carried into slice R7): Desktop never shows, serialises or
// logs an error's own text or the raw result. An error the engine gives that is not one of its
// refusals — at the plan (a damaged archive record), at the start, a panic inside the run — is
// said in fixed words; the reply, Activity's lines and its restore view never hold the error's
// text. Positive twin first: a refusal's own words are said as they are (W-RUNNING).
func TestNoRawErrorTextReachesTheRestorePage(t *testing.T) {
	const canary = "CANARY-raw-error-/private/Zebrafolder/secret.eml"
	src := filepath.Join(t.TempDir(), "Mail")
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 1)

	// The positive twin: a refusal's words.
	restoreRunning := tbrestore.SetProcessProbeForTest(func() (int, bool) { return 1, true })
	m := e.planFor(0)
	restoreRunning()
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names(tbrestore.RunningWords()))

	// A plan error that is not a refusal: the archive's record damaged.
	man := filepath.Join(e.out, ".mailarchive-manifest.json")
	good, _ := os.ReadFile(man)
	os.WriteFile(man, []byte("{not json "+canary), 0o600)
	m = e.planFor(0)
	os.WriteFile(man, good, 0o600)
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("could not check this restore", "-plan"), assure.Forbid(canary, "invalid character"))

	// A start error that is not a refusal, then a panic in the run.
	was := restoreTBRun
	t.Cleanup(func() { restoreTBRun = was })
	for _, fail := range []func() (*tbengine.Result, error){
		func() (*tbengine.Result, error) { return nil, errors.New(canary) },
		func() (*tbengine.Result, error) { panic(canary) },
	} {
		restoreTBRun = func(context.Context, *tbengine.Plan, tbengine.Start) (*tbengine.Result, error) { return fail() }
		tok, _ := e.planFor(0)["token"].(string)
		s := e.post("/api/restore-tb/start", map[string]any{"token": tok})
		assure.Reached(t, s["ok"] == true, "the start is taken")
		a := e.waitDone()
		raw, _ := json.Marshal(a)
		r, _ := a["restore"].(map[string]any)
		assure.Refused(t, 1, str(r["error"])+" "+str(a["error"]), assure.Code(1), assure.Names("restore"), assure.Forbid(canary),
			assure.NoSideEffect(func() bool {
				return !strings.Contains(string(raw), canary) && !strings.Contains(string(raw), "Outcome")
			}))
	}
}
