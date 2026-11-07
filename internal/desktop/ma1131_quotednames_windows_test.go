package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1131, MA-1124, R40
// On Windows, Desktop's plan into a Local Folders 192 characters deep: the W-LONGPATH line
// names the folders below "Quarterly reports and plans" each quoted — "100%" as a name, and
// a name holding a line break with the break shown as \n — and Save this record keeps the
// same line.
func TestTheDesktopPlanQuotesTheFoldersTooDeep(t *testing.T) {
	const deep = "Quarterly reports and plans"
	src := filepath.Join(t.TempDir(), "Mail")
	rtbWrite(t, filepath.Join(src, deep), rtbMbox("deep one"))
	rtbWrite(t, filepath.Join(src, deep+".sbd", "100%"), rtbMbox("pct one"))
	rtbWrite(t, filepath.Join(src, deep+".sbd", "Linebreak"), rtbMbox("break one"))
	out := rtbPlainArchive(t, src)
	rtbRefolder(t, out, deep+"/Linebreak", deep+"/Line\nbreak")

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pad := 192 - len(`\work.default\Mail\Local Folders`) - len(root) - 1
	if pad < 1 {
		t.Fatalf("the temp folder %s is too long to pad", root)
	}
	e := newRtbEnv(t, out, filepath.Join(root, strings.Repeat("d", pad)), 1)
	if len(e.profiles[0].local) != 192 {
		t.Fatalf("the fixture's Local Folders is %d characters, want 192: %s", len(e.profiles[0].local), e.profiles[0].local)
	}
	plan := e.planFor(0)
	lines, _ := plan["lines"].([]any)
	long := ""
	for _, l := range lines {
		if m, _ := l.(map[string]any); str(m["code"]) == "W-LONGPATH" {
			long = str(m["words"])
		}
	}
	for _, w := range []string{"3 folders are nested too deep", `"` + deep + `"`, `"` + deep + `/100%"`, `"` + deep + `/Line\nbreak"`} {
		assure.Reached(t, strings.Contains(long, w), "the plan's W-LONGPATH holds "+w+": "+long)
	}
	assure.Reached(t, !strings.Contains(long, "\n"), "no line break from a name: "+long)

	if s := e.post("/api/restore-tb/start", map[string]any{"token": plan["token"]}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	act := e.waitDone()
	r, _ := act["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true && strings.Contains(str(res["record"]), "\n"+long+"\n"), "Save this record keeps the quoted line: "+str(res["record"]))
}
