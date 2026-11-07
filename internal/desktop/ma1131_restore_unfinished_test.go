package desktop

import (
	"context"
	"html"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbengine"
)

// covers: MA-1131, R40, R41
// A restore that stopped unexpectedly part way (a crash inside the run: a panic while it writes)
// is said in fixed words, never the panic's text (INS-07); the page drawn next names the
// unfinished restore at Desktop start in W-UNFINISHED's words; the next plan into that profile
// says it will remove the partial folder first; and that restore does so: Thunderbird
// is left with the new restore's folder only, and no restore is left unfinished. Positive twin
// first: a page with no unfinished restore names none.
func TestAnUnfinishedRestoreIsSaidAndRemovedByTheNext(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	e := newRtbEnv(t, rtbPlainArchive(t, src), "", 1)
	page := html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	if strings.Contains(page, "data-restore-leftover") {
		t.Fatal("the positive twin: a page with no unfinished restore names one")
	}

	was := restoreTBRun
	restoreTBRun = func(ctx context.Context, p *tbengine.Plan, s tbengine.Start) (*tbengine.Result, error) {
		inner := s.Progress
		s.Progress = func(g tbengine.Progress) { inner(g); panic("CANARY-crash-text") }
		return tbengine.Run(ctx, p, s)
	}
	tok, _ := e.planFor(0)["token"].(string)
	if s := e.post("/api/restore-tb/start", map[string]any{"token": tok}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	a := e.waitDone()
	restoreTBRun = was
	r, _ := a["restore"].(map[string]any)
	assure.Refused(t, 1, str(r["error"]), assure.Code(1), assure.Names("stopped unexpectedly", "removes it at the start of your next restore"), assure.Forbid("CANARY"))

	page = html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, strings.Contains(page, "data-restore-leftover") && strings.Contains(page, "An earlier restore into Thunderbird did not finish"),
		"the page drawn next names the unfinished restore (W-UNFINISHED)")
	p := e.planFor(0)
	lines, _ := p["lines"].([]any)
	earlier := ""
	for _, x := range lines {
		if m, _ := x.(map[string]any); m != nil && m["code"] == "EARLIER" {
			earlier = str(m["words"])
		}
	}
	assure.Reached(t, strings.Contains(earlier, "did not finish") && strings.Contains(earlier, "will be removed first"),
		"the next plan says it removes the partial folder first: "+earlier)
	if s := e.post("/api/restore-tb/start", map[string]any{"token": p["token"]}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	a = e.waitDone()
	r, _ = a["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	page = html.UnescapeString(e.req(http.MethodGet, "/", nil, nil).Body.String())
	assure.Reached(t, res["done"] == true && len(restoredTops(t, e.profiles[0].local)) == 2 && !strings.Contains(page, "data-restore-leftover"),
		"the next restore removed the partial folder and went ahead; nothing is left unfinished")
}
