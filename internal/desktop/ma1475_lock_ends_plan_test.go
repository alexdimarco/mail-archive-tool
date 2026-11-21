package desktop

import (
	"net/http"
	"os"
	"reflect"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-1475, MA-972, R36, R37
// Lock ends an encrypted archive's waiting delete plan (the 1.7.5 adversarial pass's OUT-05 (b) and
// CON-06; the owner, 2026-11-20, Q10: "Yes, Lock ends it (Recommended)"): a plan made while the
// archive was open holds values read with its key (its message count, mail dates and first run),
// and now waits up to 24 hours, so any lock of that archive discards it, and its delete is then
// refused "no longer waiting" with the folder untouched. Positive twins first: before the Lock the
// plan waits; another archive's waiting plan is not touched by this Lock and still deletes.
func TestLockEndsAnEncryptedArchivesWaitingDeletePlan(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(out)
	x := e.plainArchive("Xarch")
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	pe, px := e.plan(out), e.plan(x)
	if pe["ok"] != true || px["ok"] != true {
		t.Fatalf("the plans: %v %v", pe, px)
	}
	tokE, tokX := pe["token"].(string), px["token"].(string)
	if e.d.pending.get(tokE, isDeletePlan) == nil || e.d.pending.get(tokX, isDeletePlan) == nil {
		t.Fatal("positive twin: both plans wait before the Lock")
	}

	k.lock(out)

	// Positive twin: the other archive's plan is untouched by this Lock and deletes.
	if r := e.deleteWith(tokX, px["name"].(string), false); r["ok"] != true || r["status"] != "complete" {
		t.Fatalf("positive twin: another archive's plan still deletes after this Lock: %v", r)
	}
	before, logBefore := treeState(t, out), deletionsLog(t, e.cfg)
	r := e.deleteWith(tokE, pe["name"].(string), false)
	code := http.StatusOK
	if r["ok"] == true {
		code = 0 // deleted: not a refusal
	}
	assure.Refused(t, code, replyErr(r), assure.Code(http.StatusOK), assure.Names(deleteNotWaiting),
		assure.NoSideEffect(func() bool {
			_, err := os.Stat(out)
			return err == nil && reflect.DeepEqual(treeState(t, out), before) && deletionsLog(t, e.cfg) == logBefore &&
				e.listed(out) && e.d.pending.get(tokE, anyKind) == nil && e.d.running.of(out) == nil
		}))
}
