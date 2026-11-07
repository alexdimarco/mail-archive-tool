package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1131, MA-1124, R40
// Desktop's progress line quotes the folder it is writing (settled 2026-11-07, the lead's
// call, owner-delegated words), as the plan's changed-name list does: a folder named
// "100%" reads ("100%"), never as a percentage (100%), and a name holding a line break (as
// the archive's record keeps it) shows the break as \n inside its quotes, in Activity's
// log and its progress line alike. An encrypted archive's progress stays counts only
// (MA-1128's own test).
func TestTheDesktopProgressQuotesTheFolder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Mail")
	rtbWrite(t, filepath.Join(src, "100%"), rtbMbox("pct one"))
	rtbWrite(t, filepath.Join(src, "Linebreak"), rtbMbox("break one"))
	out := rtbPlainArchive(t, src)
	rtbRefolder(t, out, "Linebreak", "Line\nbreak")
	e := newRtbEnv(t, out, "", 1)
	plan := e.planFor(0)
	if s := e.post("/api/restore-tb/start", map[string]any{"token": plan["token"]}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	act := e.waitDone()
	log := strings.Join(toLines(act["log"]), "\n")
	r, _ := act["restore"].(map[string]any)
	res, _ := r["result"].(map[string]any)
	assure.Reached(t, res["done"] == true, "the restore finished: "+str(res["words"]))
	assure.Reached(t, strings.Contains(log, ` messages ("100%")`) && !strings.Contains(log, "(100%)"),
		`Activity names the folder "100%" quoted: `+log)
	assure.Reached(t, strings.Contains(log, ` messages ("Line\nbreak")`) && !strings.Contains(log, "Line\nbreak"),
		"Activity shows the name's line break as \\n, never as a new line: "+log)
}

// rtbRefolder moves the records of folder from to folder to in the archive's own record, as
// an archive made elsewhere (or from Microsoft 365) can keep a name no Windows file holds.
func rtbRefolder(t *testing.T, out, from, to string) {
	t.Helper()
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	n := 0
	for k, r := range man.All() {
		seen = append(seen, r.Folder)
		if r.Folder == from {
			man.MergeFields(k, to, r.LastSeen, r.Present)
			n++
		}
	}
	if n == 0 {
		t.Fatalf("no record in %q: %q", from, seen)
	}
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
}
