package desktop

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1476, MA-964, R36, S41
// An armed delete whose row was removed from Your archives while its check ran is refused in true
// words (the 1.7.5 adversarial pass's OUT-02): "This archive was removed from Your archives while
// its check ran, so nothing was deleted.", never "asked from another row's panel"; the folder, the
// list and deletions.log unchanged, nothing running, the token still waiting. A request naming
// another row still gets the other-row words (MA-1476's cases unchanged). Positive twin first: the
// plan is armable and its row listed, and an armed delete from its own listed row deletes.
func TestAnArmedDeleteOfARowRemovedFromTheListSaysSo(t *testing.T) {
	e := newDeleteEnv(t)
	y := e.plainArchive("Yarch")
	if py := e.plan(y); py["armable"] != true || !e.listed(y) {
		t.Fatalf("positive twin: armable %v, listed %v", py["armable"], e.listed(y))
	} else if _, r := e.post("/api/archive/delete", map[string]any{"token": py["token"], "typed": "Yarch", "armed": true, "row": y}); r["ok"] != true {
		t.Fatalf("positive twin: an armed delete from its own listed row deletes: %v", r)
	}

	x := e.plainArchive("Xarch")
	other := e.plainArchive("Other")
	p := e.plan(x)
	tok := p["token"].(string)
	_, q := e.post("/api/archive/unlist", map[string]any{"path": x})
	if _, r := e.post("/api/archive/unlist", map[string]any{"path": x, "confirm": true, "named": q["named"]}); r["ok"] != true || e.listed(x) {
		t.Fatalf("Remove from list: %v (listed %v)", r, e.listed(x))
	}
	before, logBefore := treeState(t, x), deletionsLog(t, e.cfg)
	unchanged := func() bool {
		_, err := os.Stat(x)
		return err == nil && reflect.DeepEqual(treeState(t, x), before) && deletionsLog(t, e.cfg) == logBefore &&
			e.d.pending.get(tok, isDeletePlan) != nil && e.d.running.of(x) == nil
	}
	_, r := e.post("/api/archive/delete", map[string]any{"token": tok, "typed": "Xarch", "armed": true, "row": x})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
		assure.Names("removed from Your archives while its check ran", "nothing was deleted"),
		assure.Forbid("another row"), assure.NoSideEffect(unchanged))
	// Another row named: the other-row words, as MA-1476 pins them.
	_, r = e.post("/api/archive/delete", map[string]any{"token": tok, "typed": "Xarch", "armed": true, "row": other})
	if !strings.Contains(replyErr(r), "another row's panel") || !unchanged() {
		t.Errorf("an armed delete naming another row keeps MA-1476's words: %q", replyErr(r))
	}
}
