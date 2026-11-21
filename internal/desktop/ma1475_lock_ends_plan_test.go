package desktop

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-1475, R36, R37
// Any Lock ends every encrypted archive's waiting delete plan (the 1.7.5 re-verify's RV-06): the
// archive opened under one spelling of its folder (macOS's /private/var for /var) and its row listed
// under the other, the plan made from the row, which holds values read with the key, does not
// outlive the Lock. Positive twin first: the plan waits before the Lock, and an unencrypted
// archive's plan survives it.
func TestAnyLockEndsEveryEncryptedDeletePlan(t *testing.T) {
	out := backedUpEncrypted(t)
	alt := "/private" + out
	if !strings.HasPrefix(out, "/var/") {
		alt = out // no second spelling here: the same spelling still proves the rule
	} else if _, err := os.Stat(alt); err != nil {
		alt = out
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(out)
	x := e.plainArchive("Xplain")
	if _, err := k.unlock(alt, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	pe, px := e.plan(out), e.plan(x)
	tokE, tokX := pe["token"].(string), px["token"].(string)
	if e.d.pending.get(tokE, isDeletePlan) == nil || e.d.pending.get(tokX, isDeletePlan) == nil {
		t.Fatal("positive twin: both plans wait before the Lock")
	}
	k.lock(alt)
	if e.d.pending.get(tokX, isDeletePlan) == nil {
		t.Error("positive twin: an unencrypted archive's plan survives a Lock")
	}
	if e.d.pending.get(tokE, isDeletePlan) != nil {
		t.Errorf("the archive was locked as %s, but the delete plan made from its row %s, holding values read with its key, still waits", alt, out)
	}
}

// covers: MA-1475, R36, R37
// A Lock ends only an encrypted plan that read values with its key open (the re-verify, round 2's
// RV2-02): the idle lock of another archive (30 minutes unused) leaves a delete plan made while its
// own archive was locked, which holds nothing read with a key, so a person who steps away during a
// long check is not sent back to it. Positive twins first: the plan waits, and still waits after
// another archive is unlocked.
func TestTheIdleLockOfAnotherArchiveKeepsALockedArchivesPlan(t *testing.T) {
	enc := backedUpEncrypted(t) // the archive to delete, never opened here
	cur := backedUpEncrypted(t) // the archive in use, opened
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(enc, cur)
	p := e.plan(enc)
	tok, _ := p["token"].(string)
	if tok == "" || e.d.pending.get(tok, isDeletePlan) == nil {
		t.Fatalf("positive twin: the plan of the locked archive waits: %v", p)
	}
	if _, err := k.unlock(cur, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if e.d.pending.get(tok, isDeletePlan) == nil {
		t.Fatal("positive twin: unlocking another archive leaves the plan waiting")
	}
	k.reap(time.Now().Add(31 * time.Minute))
	if e.d.pending.get(tok, isDeletePlan) == nil {
		t.Error("the idle lock of another archive ended a delete plan made while its own archive was locked")
	}
}

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
