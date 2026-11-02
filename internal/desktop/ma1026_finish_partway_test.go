package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// covers: MA-1026, R36
// The Desktop's plan of a row whose delete did not finish begins with the date that delete began
// (the 1.5.3 friction review's D1): the reply's finishesLine is "This finishes the delete begun
// DATE.", which the panel draws first, and the saved plan says which run it finishes. Positive twin
// first: an ordinary plan has none.
func TestThePlanOfAPartWayDeleteSaysWhenItBegan(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Part Way")
	if p := e.plan(dir); p["ok"] != true || p["finishesLine"] != "" {
		t.Fatalf("the positive twin: %v", p["finishesLine"])
	}
	// A delete of it that stopped with no finished line (a crash, a power cut).
	began := time.Date(2026, 11, 1, 22, 5, 0, 0, time.UTC)
	line, _ := json.Marshal(map[string]any{"at": began.Format(time.RFC3339Nano), "event": "started", "run": "firstrun0001", "folder": dir, "row": dir, "kind": "plain"})
	f, err := os.OpenFile(e.cfg.deletionsLogPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
	p := e.plan(dir)
	want := "This finishes the delete begun " + began.Local().Format("2006-01-02 15:04 -07:00") + "."
	if p["ok"] != true || p["finishesLine"] != want {
		t.Fatalf("finishesLine %q, want %q (%v)", p["finishesLine"], want, p["error"])
	}
	if text, _ := p["planText"].(string); !strings.Contains("\n"+text, "\nFinishes: the delete begun "+began.Local().Format("2006-01-02 15:04 -07:00")+" (run firstrun0001)\n") {
		t.Fatalf("the saved plan:\n%s", text)
	}
}
