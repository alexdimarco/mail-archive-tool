package desktop

import (
	"html"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1131, MA-1124, R40
// The changed names in MailArchive Desktop (the lead's call of 2026-11-07, after ADV-N1):
// the plan step's NAMES line carries one "old name → new name" per line, the plan's list
// keeps those line breaks on the page (white-space: pre-line, the words still set by
// textContent only), and Save this record keeps the same lines. A plain archive with a
// folder "a%41b".
func TestTheDesktopPlanListsTheChangedNames(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	rtbWrite(t, filepath.Join(src, "a%41b"), rtbMbox("pct one"))
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 1)
	plan := e.planFor(0)
	lines, _ := plan["lines"].([]any)
	names := ""
	for _, l := range lines {
		if m, _ := l.(map[string]any); str(m["code"]) == "NAMES" {
			names = str(m["words"])
		}
	}
	item := `"a%41b" → "a_41b"`
	assure.Reached(t, strings.HasSuffix(names, "Thunderbird shows the changed names:\n"+item), "the NAMES line lists the changed name on its own line: "+names)

	page := html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, strings.Contains(page, "#rtbPlan li{white-space:pre-line"), "the plan's list keeps each name on its own line")

	if s := e.post("/api/restore-tb/start", map[string]any{"token": plan["token"]}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	act := e.waitDone()
	r, _ := act["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true && strings.Contains(str(res["record"]), "changed names:\n"+item+"\n"), "Save this record keeps the list: "+str(res["record"]))
}
