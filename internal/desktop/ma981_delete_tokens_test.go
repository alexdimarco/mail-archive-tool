package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
)

// covers: MA-981, R36
// Forget this encrypted archive here ends a delete planned of that archive (del-s3a-A2-01): the
// plan read this computer's record as it was, so its token is no longer waiting and nothing is
// deleted. Positive twin first: the token is waiting before the forget.
func TestAForgetEndsADeletePlannedOfItsArchive(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	t.Cleanup(func() { e.cfg.Keys.lockAll(true) })
	e.list(out)
	p := e.plan(out)
	if p["ok"] != true || e.d.pending.get(p["token"].(string), isDeletePlan) == nil {
		t.Fatalf("the positive twin: the plan is waiting: %v", p)
	}
	_, fp := e.post("/api/encryption/forget/plan", map[string]any{"path": out})
	if _, f := e.post("/api/encryption/forget", map[string]any{"token": fp["token"], "whole": true, "keys": true}); f["ok"] != true {
		t.Fatalf("the forget: %v %v", fp, f)
	}
	before := treeState(t, out)
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(deleteNotWaiting),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, out), before) && e.listed(out) }))
}

// covers: MA-981, R36
// A delete's settings are written only while they still hold what the delete read (del-s3a-A2-06):
// a switch of the current archive (or a new last-archive note) made in another tab between the
// delete's step 4 read and its write is kept. Positive twin first: unchanged since the read,
// they are cleared.
func TestADeletesSettingsWriteKeepsASwitchMadeMeanwhile(t *testing.T) {
	e := newDeleteEnv(t)
	x, y := t.TempDir(), t.TempDir()
	lf, err := job.LastArchiveFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lf), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(lf) })
	set := func(out, last string) {
		t.Helper()
		if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lf, []byte(last+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	set(x, x)
	p := &desktopPointers{settings: e.cfg.SettingsPath}
	if got, err := p.Load(); err != nil || got.Out != x || got.LastArchive != x {
		t.Fatalf("the read: %v %v", got, err)
	}
	if err := p.Save(archdelete.Pointers{}); err != nil {
		t.Fatal(err)
	}
	if s := e.cfg.settings(); s.Out != "" || job.ReadLastArchive(lf) != "" {
		t.Fatalf("the positive twin: unchanged since the read, they are cleared: %q %q", s.Out, job.ReadLastArchive(lf))
	}

	set(x, x)
	p = &desktopPointers{settings: e.cfg.SettingsPath}
	if _, err := p.Load(); err != nil {
		t.Fatal(err)
	}
	set(y, y) // a switch in another tab
	if err := p.Save(archdelete.Pointers{}); err != nil {
		t.Fatal(err)
	}
	if s := e.cfg.settings(); s.Out != y {
		t.Errorf("the switch to %s made meanwhile was undone: Out is %q", y, s.Out)
	}
	if got := job.ReadLastArchive(lf); got != y {
		t.Errorf("the last-archive note made meanwhile was undone: %q", got)
	}
}

// covers: MA-981, R36
// Only a switch of the current archive ends a delete planned (del-s3a-A3-06): a Settings save that
// leaves the location as it is (a box ticked) keeps it, and its delete goes on. Positive twin
// first: a save that switches ends it.
func TestOnlyASwitchEndsADeletePlanned(t *testing.T) {
	e := newDeleteEnv(t)
	cur, other := e.plainArchive("Current"), e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	a := e.plainArchive("Planned")
	p := e.plan(a)
	if _, s := e.post("/api/settings", map[string]any{"Out": other}); s["ok"] != true {
		t.Fatalf("the switch: %v", s)
	}
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	if replyErr(r) != deleteNotWaiting {
		t.Fatalf("the positive twin: a switch ends the plan: %v", r)
	}

	p = e.plan(a)
	tick := true
	if _, s := e.post("/api/settings", map[string]any{"Out": other, "IncludeDeleted": tick}); s["ok"] != true {
		t.Fatalf("the save: %v", s)
	}
	if r := e.deleteWith(p["token"].(string), p["name"].(string), false); r["ok"] != true || r["status"] != "complete" {
		t.Errorf("a save that kept the location ended the delete planned: %v", r)
	}
}

// covers: MA-981, R36
// The new recovery key's Cancel takes only a key change's token (del-s3a-A4-02): a delete plan's
// token sent there is left where it is, and its delete goes on. Positive twin first: the plan is
// waiting before.
func TestTheRecoveryKeysCancelLeavesADeletePlan(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Kept Plan")
	p := e.plan(dir)
	tok := p["token"].(string)
	if e.d.pending.get(tok, isDeletePlan) == nil {
		t.Fatalf("the positive twin: %v", p)
	}
	e.post("/api/encryption/new-recovery-key/cancel", map[string]any{"token": tok})
	if e.d.pending.get(tok, isDeletePlan) == nil {
		t.Fatal("the recovery key's Cancel ended a delete plan")
	}
	if r := e.deleteWith(tok, p["name"].(string), false); r["status"] != "complete" {
		t.Errorf("the delete: %v", r)
	}
}

// covers: MA-981, R36
// A delete of the current archive, whose step 4 clears it, applies a switch's rules (del-s3a
// re-verification, R3): a delete planned of another row while it was current ends ("That delete
// is no longer waiting"), nothing of that row deleted. Positive twin first: a delete of an
// archive that is not current leaves that plan waiting.
func TestADeleteOfTheCurrentArchiveIsASwitch(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	planned := e.plainArchive("Planned")
	p := e.plan(planned)
	if p["ok"] != true {
		t.Fatalf("the plan of %s: %v", planned, p)
	}
	tok := p["token"].(string)
	if r := e.planAndDelete(e.plainArchive("NotCurrent")); r["status"] != "complete" {
		t.Fatalf("the positive twin: a delete of an archive that is not current: %v", r)
	}
	if e.d.pending.get(tok, isDeletePlan) == nil {
		t.Fatal("the positive twin: a delete of an archive that is not current ended another row's plan")
	}

	if r := e.planAndDelete(cur); r["status"] != "complete" || e.cfg.settings().Out != "" {
		t.Fatalf("the delete of the current archive: %v (current now %q)", r, e.cfg.settings().Out)
	}
	r := e.deleteWith(tok, p["name"].(string), false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(deleteNotWaiting),
		assure.NoSideEffect(func() bool { return r["ok"] == false && archdelete.IsArchive(planned) && e.listed(planned) }))
}

// covers: MA-981, R36
// Remove from list that clears the current archive (it is unreachable, §5.2) applies a switch's
// rules (del-s3a-A4-04): the encrypted archive open here locks, and a delete planned ends. Positive
// twin first: Remove from list of another row leaves the current archive open.
func TestRemoveFromListOfTheCurrentArchiveIsASwitch(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	e.list(out)
	if _, m := e.post("/api/encryption/unlock", map[string]any{"secret": keysPassword}); m["ok"] != true || k.get(out) == nil {
		t.Fatalf("unlock: %v", m)
	}
	other := e.plainArchive("Other")
	if _, u := e.post("/api/archive/unlist", map[string]any{"path": other, "confirm": true, "named": []string{}}); u["ok"] != true {
		t.Fatalf("the positive twin: %v", u)
	}
	if k.get(out) == nil {
		t.Fatal("the positive twin: Remove from list of another row locked the current archive")
	}

	planned := e.plainArchive("Planned")
	p := e.plan(planned)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(out, moved); err != nil { // its drive unplugged
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Rename(moved, out) })
	_, q := e.post("/api/archive/unlist", map[string]any{"path": out})
	_, u := e.post("/api/archive/unlist", map[string]any{"path": out, "confirm": true, "named": q["named"]})
	if u["ok"] != true || e.cfg.settings().Out != "" {
		t.Fatalf("Remove from list of the unreachable current archive: %v %v", q, u)
	}
	if k.get(out) != nil {
		t.Error("the archive that is no longer current is still open here")
	}
	if r := e.deleteWith(p["token"].(string), p["name"].(string), false); replyErr(r) != deleteNotWaiting {
		t.Errorf("a delete planned before the switch: %v", r)
	}
}
