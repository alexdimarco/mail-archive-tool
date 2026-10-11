package desktop

import (
	"strings"
	"testing"
)

// covers: MA-437, MA-521, MA-520, MA-427, R29, S52
// F9, F10: offers follow what the whole retry set holds. Run 1 exports 5 of 7 and fails 2;
// part 2 exports one of them and fails the other; part 3 completes the set. While a message is
// still missing the earlier cards still offer to export it (the positive twin); once the set is
// whole none of the three cards offers "Export failed messages again" (it would make a
// duplicate part and move the set fingerprint), and every card, and the receipt, says how much
// of the set is exported.
func TestFricOffersFollowTheWholeRetrySet(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.failWith("F-INBOX-0002", 400)
	e.failWith("F-INBOX-0005", 400)
	run1 := e.startExport(e.selection(entry(ids["Inbox"], false)))
	e.waitRun()
	if c := e.card(run1); !hasAct(c, "export-failed-again") || c["setNote"] != nil {
		t.Fatalf("the first run's card: %v %v", actionsOf(c), c["setNote"])
	}

	// part 2: one of the two exports, the other fails again
	e.failWith("F-INBOX-0002", 0)
	pid := e.ok("/api/exports/retry", map[string]any{"runId": run1, "which": "failed"})["previewId"].(string)
	e.waitPreview(pid)
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last2 := e.waitRun()["last"].(map[string]any)
	run2 := e.manifestOf(last2["rootName"].(string))["run"].(map[string]any)["id"].(string)
	c1, c2 := e.card(run1), e.card(run2)
	if !hasAct(c1, "export-failed-again") || !hasAct(c2, "export-failed-again") {
		t.Fatalf("a message is still missing, so both cards still offer it: %v %v", actionsOf(c1), actionsOf(c2))
	}
	if note, _ := c2["setNote"].(string); !strings.Contains(note, "6 of 7 messages are exported so far") || !strings.Contains(note, "2 parts") {
		t.Errorf("part 2's set note: %q", c2["setNote"])
	}

	// part 3 completes the set
	e.failWith("F-INBOX-0005", 0)
	pid = e.ok("/api/exports/retry", map[string]any{"runId": run2, "which": "failed"})["previewId"].(string)
	e.waitPreview(pid)
	e.ok("/api/exports/start", map[string]any{"previewId": pid})
	last3 := e.waitRun()["last"].(map[string]any)
	run3 := e.manifestOf(last3["rootName"].(string))["run"].(map[string]any)["id"].(string)
	for i, run := range []string{run1, run2, run3} {
		c := e.card(run)
		if hasAct(c, "export-failed-again") || hasAct(c, "export-remaining") {
			t.Errorf("part %d still offers to export what the set already holds: %v", i+1, actionsOf(c))
		}
		if note, _ := c["setNote"].(string); !strings.Contains(note, "All 7 messages are exported") || !strings.Contains(note, "3 parts") {
			t.Errorf("part %d's set note: %q", i+1, c["setNote"])
		}
		if r, _ := c["receipt"].(string); !strings.Contains(r, "Set total: All 7 messages are exported") {
			t.Errorf("part %d's receipt does not carry the set total: %s", i+1, r)
		}
		if !hasAct(c, "open-folder") || !hasAct(c, "check-this-export") {
			t.Errorf("part %d lost Open folder or Check this export: %v", i+1, actionsOf(c))
		}
		if i < 2 { // an earlier part no longer says its messages were not exported, or lists them
			if l, _ := c["label"].(string); !strings.Contains(l, "the rest was exported in a later part of this set") {
				t.Errorf("part %d's label is stale: %q", i+1, l)
			}
			if c["failures"] != nil {
				t.Errorf("part %d lists as not exported what part 3 exported: %v", i+1, c["failures"])
			}
		}
	}
}
