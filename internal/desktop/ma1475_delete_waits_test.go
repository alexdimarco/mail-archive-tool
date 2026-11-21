package desktop

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/wizard"
)

// A delete plan waits until the person is done with it (docs/design-1.7.5-fixes.md §3.1, the
// owner's report of 2026-11-19: "it times out too early making you do the process over"), the
// server's half: a delete plan waits 24 hours, every other kind of held item 10 minutes, each from
// its own field; the discard route ends a delete plan and leaves any other kind's token waiting;
// and a plan held across a backup that saved mail is refused as changed, its reply marked so the
// page asks for the plan again. The page's half is MA-1477.

// waitGone polls until the item under tok is no longer held, at most five seconds.
func waitGone(p *pendingSecrets, tok string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for p.get(tok, anyItem) != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return p.get(tok, anyItem) == nil
}

// covers: MA-1475, MA-972, R36, S41
// A delete plan waits 24 hours and every other kind 10 minutes, each by its own field. Positive
// twin first: a plan held past the other kinds' life (its field shortened) still waits, and with its
// name typed it deletes.
func TestADeletePlanWaitsADayAndEveryOtherKindTenMinutes(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Held")
	e.d.pending.life = 30 * time.Millisecond
	p := e.plan(dir)
	time.Sleep(150 * time.Millisecond)
	e.d.pending.life = pendingLife
	tok, _ := p["token"].(string)
	assure.Reached(t, e.d.pending.get(tok, isDeletePlan) != nil, "a delete plan held past the other kinds' life still waits")
	if done := e.deleteWith(tok, "Held", false); done["ok"] != true || done["status"] != archdelete.StatusComplete {
		t.Fatalf("the plan held past the other kinds' life did not delete: %v", done)
	}

	// The lives themselves, each from its own constant.
	fresh := newPendingSecrets()
	if deletePlanLife != 24*time.Hour || pendingLife != 10*time.Minute || fresh.deleteLife != deletePlanLife || fresh.life != pendingLife {
		t.Fatalf("a delete plan's life is %v (field %v) and every other kind's %v (field %v); want 24h and 10m", deletePlanLife, fresh.deleteLife, pendingLife, fresh.life)
	}

	others := func() []*pendingItem {
		return []*pendingItem{
			{key: "change", rec: &keyring.PendingRecovery{}},
			{key: "restore-key", res: &keyring.PendingRestore{}},
			{key: "forget", plan: &keyring.ForgetPlan{}},
			{key: "new-archive", add: &wizard.PendingAdd{}},
			{key: "copy", copyPlan: &app.DecryptPlan{}},
			{key: restorePlanKey("row"), restorePlan: &heldRestore{}},
		}
	}

	// The delete plans' field ends a delete plan and no other kind.
	s := newPendingSecrets()
	s.deleteLife = 20 * time.Millisecond
	dtok := s.put(&pendingItem{key: deletePlanKey("row"), deletePlan: &heldPlan{row: "row"}})
	var otoks []string
	for _, it := range others() {
		otoks = append(otoks, s.put(it))
	}
	assure.Reached(t, waitGone(s, dtok), "the delete plans' field ends a delete plan")
	for i, tk := range otoks {
		if s.get(tk, anyItem) == nil {
			t.Errorf("the delete plans' field ended another kind of item (%d)", i)
		}
	}

	// The other kinds' field ends every other kind and never a delete plan.
	s = newPendingSecrets()
	s.life = 20 * time.Millisecond
	dtok = s.put(&pendingItem{key: deletePlanKey("row"), deletePlan: &heldPlan{row: "row"}})
	otoks = otoks[:0]
	for _, it := range others() {
		otoks = append(otoks, s.put(it))
	}
	for i, tk := range otoks {
		if !waitGone(s, tk) {
			t.Errorf("the other kinds' field did not end item %d", i)
		}
	}
	assure.Reached(t, s.get(dtok, isDeletePlan) != nil, "the other kinds' field never ends a delete plan")
}

// covers: MA-1475, MA-971, R36, S41
// The discard route (/api/archive/delete/discard {token}) ends a delete plan: its token is no longer
// waiting, and a delete with it is refused, nothing removed. It takes only a delete plan's token: a
// token of another kind posted to it is left waiting. It answers ok whether or not the plan was
// still waiting, and changes nothing else. Positive twin first: the plan's token waits until it is
// discarded.
func TestTheDiscardRouteEndsOnlyADeletePlan(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Discarded")
	p := e.plan(dir)
	tok, _ := p["token"].(string)
	assure.Reached(t, e.d.pending.get(tok, isDeletePlan) != nil, "the positive twin: the plan's token waits")

	before := treeState(t, dir)
	settings, _ := os.ReadFile(e.cfg.SettingsPath)
	logBefore := deletionsLog(t, e.cfg)
	unchanged := func() bool {
		s, _ := os.ReadFile(e.cfg.SettingsPath)
		return reflect.DeepEqual(treeState(t, dir), before) && e.listed(dir) && string(s) == string(settings) &&
			deletionsLog(t, e.cfg) == logBefore && len(e.d.deletes.views()) == 0
	}

	_, m := e.post("/api/archive/delete/discard", map[string]any{"token": tok})
	if m["ok"] != true {
		t.Fatalf("the discard: %v", m)
	}
	assure.Reached(t, e.d.pending.get(tok, anyItem) == nil, "the discard ends the delete plan")
	r := e.deleteWith(tok, "Discarded", false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names("no longer waiting", "nothing was deleted"),
		assure.NoSideEffect(unchanged))

	// Another kind's token, posted to the discard route, is left waiting.
	ft := e.d.pending.put(&pendingItem{key: "forget-key", out: dir, plan: &keyring.ForgetPlan{Out: dir}})
	kt := e.d.pending.put(&pendingItem{key: "key-change", out: dir, rec: &keyring.PendingRecovery{}})
	rt := e.d.pending.put(&pendingItem{key: restorePlanKey(dir), out: dir, restorePlan: &heldRestore{}})
	for name, tk := range map[string]string{"a forget's": ft, "a key change's": kt, "a restore's": rt} {
		_, m := e.post("/api/archive/delete/discard", map[string]any{"token": tk})
		if m["ok"] != true || e.d.pending.get(tk, anyItem) == nil {
			t.Errorf("the discard route took %s token, or did not answer ok: %v", name, m)
		}
	}

	// A token no longer waiting, an unknown one and none: ok, nothing changed.
	for _, tk := range []string{tok, "0123456789abcdef0123456789abcdef", ""} {
		_, m := e.post("/api/archive/delete/discard", map[string]any{"token": tk})
		if m["ok"] != true {
			t.Errorf("the discard of %q did not answer ok: %v", tk, m)
		}
	}
	assure.Reached(t, unchanged() && e.d.pending.get(ft, isForgetPlan) != nil, "the discard route changes nothing else")
}

// covers: MA-1475, MA-964, R36, S41
// A plan held across a backup that saved mail is refused when Delete is clicked: "This archive
// changed since you clicked Delete. Click Delete again.", nothing removed (the backup's new mail
// still there), and its reply carries changed: true, so the page asks for the plan again by itself.
// No other refusal carries it. Positive twin first: a plan with no backup in between deletes, its
// reply not marked changed.
func TestAPlanHeldAcrossABackupIsRefusedAsChanged(t *testing.T) {
	e := newDeleteEnv(t)
	twin := e.plainArchive("Quiet")
	if r := e.planAndDelete(twin); r["status"] != archdelete.StatusComplete || r["changed"] != nil {
		t.Fatalf("the positive twin: a plan with no backup in between: status %v, changed %v, error %q", r["status"], r["changed"], replyErr(r))
	}

	// Another refusal after the start, a backup holding the lock: not marked changed.
	busy := e.plainArchive("Busy")
	bp := e.plan(busy)
	l, err := lockfile.Acquire(filepath.Join(busy, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	r := e.deleteWith(bp["token"].(string), "Busy", false)
	l.Release()
	if r["status"] != archdelete.StatusRefusedAfterStart || r["changed"] != nil {
		t.Errorf("a delete refused because a backup holds the lock is marked changed: status %v, changed %v", r["status"], r["changed"])
	}

	dir := filepath.Join(t.TempDir(), "Backed up")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := buildArchiveAt(t, dir)
	e.list(dir)
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	// A backup saves new mail into it while the plan waits.
	alice := filepath.Join(f.src, "alice")
	putMaildirMsg(t, alice, "a9.eml", archMsg("arrived later", "id9", "body nine"))
	opts := app.Options{Inputs: []string{alice}, Out: dir, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	res, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil)
	if err != nil || res.Stats.Exported != 1 {
		t.Fatalf("the backup in between did not save the new message: %+v %v", res, err)
	}
	os.Remove(filepath.Join(dir, ".mailarchive.lock"))
	// The delete's own lock file (taken for the re-check, released) is not the archive's mail.
	after := treeState(t, dir, lockfile.Name)

	r = e.deleteWith(p["token"].(string), "Backed up", false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
		assure.Names("This archive changed since you clicked Delete. Click Delete again."),
		assure.NoSideEffect(func() bool {
			return r["changed"] == true && r["status"] == archdelete.StatusRefusedAfterStart && r["id"] != "" &&
				reflect.DeepEqual(treeState(t, dir, lockfile.Name), after) && archdelete.IsArchive(dir) && e.listed(dir)
		}))
}
