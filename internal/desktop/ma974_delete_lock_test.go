package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-974, R36
// The registry's lock for a delete (design-archive-delete §5.3 step 1 (b), §12 Q3; C13,
// R3/R4:NF-R3-UNLOCK-MIDDELETE, R4:NF-R4-LOCK-NO-WAIT): LockForDelete locks the archive in the
// page whether or not it is open and sets its "being deleted" mark, which every open checks —
// the person's Unlock (on the page and through the route), a remembered key's Unlock, and a
// silent re-open — refused with "This archive is being deleted." until EndDelete clears it.
// Positive twin first: the archive unlocks before the mark, and again after EndDelete.
func TestADeleteLocksTheArchiveAndRefusesEveryOpenUntilItEnds(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}

	// The positive twin: it unlocks, remembering its key.
	if _, m := e.post("/api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true}); m["ok"] != true || k.get(out) == nil {
		t.Fatalf("unlock: %v", m)
	}

	if err := k.LockForDelete(out); err != nil {
		t.Fatal(err)
	}
	if k.get(out) != nil {
		t.Fatal("the archive is still open after LockForDelete")
	}
	_, m := e.post("/api/encryption/unlock", map[string]any{"secret": keysPassword})
	assure.Refused(t, http.StatusOK, toText(m), assure.Code(http.StatusOK), assure.Names("This archive is being deleted."),
		assure.NoSideEffect(func() bool { return k.get(out) == nil }))
	if _, err := k.unlockRemembered(out); err == nil || !strings.Contains(err.Error(), "being deleted") {
		t.Errorf("a remembered key's Unlock during the delete: %v", err)
	}
	if k.remembered(out) != nil {
		t.Error("a silent re-open by the remembered key during the delete")
	}
	if r, _ := k.readerFor(out); r != nil {
		t.Error("the reader re-opened the archive during the delete")
	}

	k.EndDelete(out)
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatalf("after EndDelete the archive does not unlock again: %v", err)
	}
	k.lock(out)
}

// covers: MA-974, MA-973, R36
// An encrypted archive deleted through the Desktop's routes, open on this page: its plan and
// delete complete, the archive is locked in the page first, this computer forgets its record
// (the keyring adapter carries keyring.ForgetPlan.ID into KeyPlan.ID: without it step 8 always
// skips and the row is kept), and its row is gone.
func TestAnEncryptedArchiveOpenHereIsDeletedAndForgotten(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(out)
	if rec, _ := encrecord.Load(); len(rec.Archives) != 1 {
		t.Fatalf("the archive is not recorded on this computer: %v", rec.Archives)
	}
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if r, err := k.readerFor(out); err != nil || r == nil {
		t.Fatalf("the page's reader: %v", err)
	}

	r := e.planAndDelete(out)
	if r["ok"] != true || r["status"] != "complete" {
		t.Fatalf("the delete: %v", r)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
	if k.get(out) != nil {
		t.Error("the archive is still open on the page")
	}
	rec, err := encrecord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := rec.Lookup(out); ok {
		t.Errorf("this computer still records the archive: %v", m)
	}
	if e.listed(out) {
		t.Error("the row is still listed")
	}
	if !strings.Contains(toText(r), "forgot") {
		t.Errorf("the result does not say the key was forgotten: %v", r)
	}
}

// covers: MA-974, R36
// The page's own reader of a plain archive holds its search index open, which Windows cannot
// delete under it (§9 "Open files"): the delete closes it before step 1 and the reader refuses
// that archive ("This archive is being deleted.") until the delete returns.
func TestThePagesReaderIsClosedBeforeTheDelete(t *testing.T) {
	var rm *readerMux
	var during int
	var closed bool
	e := newDeleteEnv(t, func(w *wsEnv) {
		rm = &readerMux{}
		w.cfg.ReaderHold = func(out string) func() {
			release := rm.holdFor(out)
			rm.mu.Lock()
			closed = rm.ix == nil
			rm.mu.Unlock()
			rec := httptest.NewRecorder()
			rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
			during = rec.Code
			if !strings.Contains(rec.Body.String(), "This archive is being deleted.") {
				during = -1
			}
			return release
		}
	})
	rm.cfg = e.cfg
	t.Cleanup(func() { rm.Close() })
	dir := e.plainArchive("Read Here")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
	if rec.Code != http.StatusOK || rm.ix == nil {
		t.Fatalf("the positive twin: the reader opens the archive: %d %s", rec.Code, rec.Body.String())
	}
	r := e.planAndDelete(dir)
	if r["ok"] != true {
		t.Fatalf("the delete: %v", r)
	}
	if !closed || during != http.StatusServiceUnavailable {
		t.Errorf("before step 1: the reader's index closed %v, a request answered %d; want closed and 503 'being deleted'", closed, during)
	}
	if rm.ix != nil {
		t.Error("the reader holds the deleted archive's index")
	}
}
