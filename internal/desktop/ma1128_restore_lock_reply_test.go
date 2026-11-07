package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1128, R37, R40, S72
// The Lock reply while a restore of the archive runs (design §6.2 note, Q6; slice R7, as the
// copy's copyGoesOnText): a Lock of the encrypted archive while its restore into Thunderbird
// writes answers "Locked. The restore into Thunderbird already started goes on until it finishes
// — Shut down stops it, and nothing is added to Thunderbird.", and the restore, reading through
// its own read lease, does go on to its end: the folder is in Thunderbird. Its progress in
// Activity is counts only, naming no folder. Positive twin first: a Lock with no restore running
// says nothing of one.
func TestTheLockReplySaysARunningRestoreGoesOn(t *testing.T) {
	const words = "Locked. The restore into Thunderbird already started goes on until it finishes — Shut down stops it, and nothing is added to Thunderbird."
	out := backedUpEncrypted(t)
	e := newRtbEnv(t, out, "", 1)
	e.unlock()
	if l := e.post("/api/encryption/lock", nil); l["ok"] != true || l["note"] != nil {
		t.Fatalf("the positive twin: a Lock with no restore running speaks of one: %v", l)
	}
	e.unlock()
	started, release := holdRun(t)
	tok, _ := e.planFor(0)["token"].(string)
	s := e.post("/api/restore-tb/start", map[string]any{"token": tok, "notEncryptedOK": true})
	assure.Reached(t, s["ok"] == true, "the restore starts")
	<-started
	l := e.post("/api/encryption/lock", nil)
	assure.Reached(t, l["ok"] == true && str(l["note"]) == words, "the Lock reply says the restore goes on: "+str(l["note"]))
	a := e.activity()
	log := strings.Join(toLines(a["log"]), "\n")
	assure.Reached(t, strings.Contains(log, "Restoring: 1 of 1 messages") && !strings.Contains(log, "Inbox") && !strings.Contains(log, "("), "counts only in Activity: "+log)
	close(release)
	a = e.waitDone()
	r, _ := a["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true && len(restoredTops(t, e.profiles[0].local)) == 2 && strings.Contains(str(res["words"]), "Restored 1 message"),
		"locked while it wrote, the restore went on to its end")
}
