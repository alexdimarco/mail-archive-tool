package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-972, R36, S41
// A Delete runs only through its plan's token (design-archive-delete §3.1; C19, C20): the plan
// answers with what Delete removes and the saved plan's exact text, and keeps the plan under a
// one-use token. A name that does not match is refused and the token kept; the right name
// deletes; the same token again, an expired one, a token of another kind and a plan's token
// after a switch of the current archive are refused and nothing is removed; a delete plan's
// token is not a forget's. Positive twin first: a plain archive deleted through its token is
// complete and its row gone.
func TestADeleteRunsOnlyThroughItsPlansToken(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Mail Archivé")

	p := e.plan(dir)
	if p["ok"] != true || p["name"] != "Mail Archivé" || p["folder"] != dir {
		t.Fatalf("the plan: %v", p)
	}
	text, _ := p["planText"].(string)
	if !strings.HasPrefix(text, "Status: planned, nothing deleted\n") || !strings.Contains(text, "Record-SHA256: ") {
		t.Errorf("the saved plan's text: %q", text)
	}
	for _, want := range []string{"What Delete does not reach", "deletions.log"} {
		if !strings.Contains(toText(p), want) {
			t.Errorf("the plan does not say %q: %v", want, p)
		}
	}
	token := p["token"].(string)

	// A name that does not match (another case) is refused, the token kept.
	before := treeState(t, dir)
	m := e.deleteWith(token, "mail archivé", false)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("does not match", "Mail Archivé"),
		assure.NoSideEffect(func() bool {
			return reflect.DeepEqual(treeState(t, dir), before) && e.d.pending.get(token, isDeletePlan) != nil
		}))

	// The positive twin: the name, typed decomposed (NFD), deletes.
	done := e.deleteWith(token, "Mail Archivé", false)
	if done["ok"] != true || done["status"] != "complete" || done["id"] == "" {
		t.Fatalf("the delete: %v", done)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the archive folder is still there: %v", err)
	}
	if e.listed(dir) {
		t.Error("the row is still in Your archives")
	}

	// The same token again.
	dir2 := e.plainArchive("Second")
	before2 := treeState(t, dir2)
	m = e.deleteWith(token, "Mail Archivé", false)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("no longer waiting", "nothing was deleted"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir2), before2) }))

	// Expired.
	e.d.pending.life = 30 * time.Millisecond
	p2 := e.plan(dir2)
	time.Sleep(150 * time.Millisecond)
	e.d.pending.life = pendingLife
	m = e.deleteWith(p2["token"].(string), "Second", false)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir2), before2) && e.listed(dir2) }))

	// Another kind's token: a forget plan's is not a delete's, and a delete's is not a forget's.
	ft := e.d.pending.put(&pendingItem{key: "forget-key", out: dir2, plan: &keyring.ForgetPlan{Out: dir2}})
	m = e.deleteWith(ft, "Second", false)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool {
			return reflect.DeepEqual(treeState(t, dir2), before2) && e.d.pending.get(ft, isForgetPlan) != nil
		}))
	p3 := e.plan(dir2)
	_, fm := e.post("/api/encryption/forget", map[string]any{"token": p3["token"]})
	if fm["ok"] == true || e.d.pending.get(p3["token"].(string), isDeletePlan) == nil {
		t.Errorf("a forget took a delete plan's token: %v", fm)
	}

	// A switch of the current archive ends every delete plan.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: e.out}); err != nil {
		t.Fatal(err)
	}
	if _, sw := e.post("/api/settings", map[string]any{"out": t.TempDir()}); sw["ok"] != true {
		t.Fatalf("the switch: %v", sw)
	}
	m = e.deleteWith(p3["token"].(string), "Second", false)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir2), before2) && e.listed(dir2) }))

	// Shut down ends them too.
	p4 := e.plan(dir2)
	e.d.pending.close()
	if e.d.pending.get(p4["token"].(string), isDeletePlan) != nil {
		t.Error("Shut down kept a delete plan")
	}
}

// covers: MA-972, R36, S41
// Each delete's result and its deletion record's exact bytes are held on the server, under a
// server-made id, until dismissed or Shut down (§3.1 "After a Delete", §8.1; R3:NEW-FINDING-
// record-held-vs-sent, R3:NF-R3-RESULT-RELOAD, R4:NF-R4-HELD-RESULTS-MANY): the delete
// response carries them, every dashboard load carries both of two deletes' results, the bytes
// are the record whose digest deletions.log's finished line holds, a Dismiss of one keeps the
// other, a Dismiss of an id no longer held is refused, and Shut down holds none.
func TestADeletesResultIsHeldUntilDismissedOrShutDown(t *testing.T) {
	e := newDeleteEnv(t)
	a, b := e.plainArchive("First"), e.plainArchive("Second")
	ra, rb := e.planAndDelete(a), e.planAndDelete(b)
	for _, r := range []map[string]any{ra, rb} {
		if r["ok"] != true || r["id"] == "" {
			t.Fatalf("a delete: %v", r)
		}
		rec := recordBytes(t, r)
		if string(rec) != r["record"] {
			t.Error("the record's text and its bytes differ")
		}
		i := bytes.LastIndex(bytes.TrimRight(rec, "\n"), []byte("\n"))
		sum := sha256.Sum256(rec[:i+1])
		digest := hex.EncodeToString(sum[:])
		if !strings.HasSuffix(strings.TrimSpace(string(rec)), "Record-SHA256: "+digest) || !strings.Contains(deletionsLog(t, e.cfg), `"record_sha256":"`+digest+`"`) {
			t.Errorf("the held record's digest %s is not its last line's and deletions.log's", digest)
		}
		name, _ := r["recordName"].(string)
		if !strings.HasPrefix(name, "MailArchive deletion record - ") || !strings.HasSuffix(name, ".txt") {
			t.Errorf("the record's file name: %q", name)
		}
	}

	held := e.heldOnPage()
	if len(held) != 2 {
		t.Fatalf("the page holds %d results, want both: %v", len(held), held)
	}
	for i, r := range []map[string]any{ra, rb} {
		if held[i]["id"] != r["id"] || !bytes.Equal(recordBytes(t, held[i]), recordBytes(t, r)) {
			t.Errorf("held result %d is not the delete's: %v", i, held[i])
		}
	}
	// A page loaded twice still has both (a reload is no dismissal).
	if again := e.heldOnPage(); len(again) != 2 {
		t.Errorf("a second load holds %d", len(again))
	}

	_, m := e.post("/api/archive/delete/dismiss", map[string]any{"id": ra["id"]})
	if m["ok"] != true {
		t.Fatalf("dismiss: %v", m)
	}
	held = e.heldOnPage()
	if len(held) != 1 || held[0]["id"] != rb["id"] {
		t.Fatalf("after dismissing the first: %v", held)
	}
	_, m = e.post("/api/archive/delete/dismiss", map[string]any{"id": ra["id"]})
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("no longer held"),
		assure.NoSideEffect(func() bool { return len(e.heldOnPage()) == 1 }))

	e.stop()
	select {
	case <-e.life.Drained():
	case <-time.After(20 * time.Second):
		t.Fatal("no drain")
	}
	if n := len(e.d.deletes.views()); n != 0 {
		t.Errorf("Shut down kept %d held results", n)
	}
}
