package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-408, MA-446, R12, S52
// F14: the note under Selected counts the folder with its subfolders (13 for an Inbox of 7 with
// two levels of subfolders, the number the count endpoint gives for it), the Include subfolders
// box applies to the folders already selected, and the footer says at once when a selection is
// more than one export holds. The page script cannot run here: the note's number is the sum the
// script takes over the folders answer (checked here against the count endpoint), and the script
// is read for the rest.
func TestFricTheSelectionNoteCountsTheSubfolders(t *testing.T) {
	e := newWsEnv(t)
	tree := e.ok("/api/workspace/folders", nil)["folders"].([]any)
	count := map[string]float64{}
	parent := map[string]string{}
	var inbox string
	for _, f := range tree {
		m := f.(map[string]any)
		id := m["id"].(string)
		count[id], parent[id] = m["count"].(float64), m["parent"].(string)
		if m["name"] == "Inbox" {
			inbox = id
		}
	}
	var sub func(id string) float64
	sub = func(id string) float64 {
		n := count[id]
		for k, p := range parent {
			if p == id {
				n += sub(k)
			}
		}
		return n
	}
	own := count[inbox]
	got := e.ok("/api/workspace/count", e.selection(entry(inbox, true)))["count"].(float64)
	if sub(inbox) != got || own == got {
		t.Fatalf("the subtree sum %v of the folders answer is not the count %v (own %v): the note would under-count", sub(inbox), got, own)
	}
	for _, want := range []string{"function subtreeCount(", "withSub?subtreeCount(id):f.count", "$('inclSub').addEventListener('change'", "e.subtree=on", "d.count>200000"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not contain %q", want)
		}
	}
	if strings.Contains(workspaceJS, "fmt(f.count)+' messages from '+f.name+(($('inclSub')") {
		t.Error("workspace.js still counts the folder's own messages beside 'and its subfolders'")
	}
}

// covers: MA-437, MA-446, MA-448, R12, S52
// F20: a run record that cannot be read gives a typed refusal with a remedy and no file name,
// not "couldn't do that: the run record <id>.jsonl has no header" under the id of a failed
// progress write; the card says its record is damaged and what still works (Open folder and
// Check this export, which then read the folder through the journal); and an error nothing
// else classifies reaches the owner without its raw text. The positive twin first: the same
// button on an undamaged record works.
func TestFricADamagedRunRecordIsWordedAndTheFolderStaysUsable(t *testing.T) {
	e := newWsEnv(t)
	run, root := e.cancelledRun()
	e.ok("/api/exports/finish", map[string]any{"runId": run}) // the positive twin

	var rec string
	filepath.WalkDir(e.state, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, run+".jsonl") {
			rec = p
		}
		return nil
	})
	if rec == "" {
		t.Fatal("no run record for the run")
	}
	if err := os.WriteFile(rec, []byte("not a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.restart()

	code, out := e.post("/api/exports/finish", map[string]any{"runId": run})
	refusedBody(t, code, out, "R-rec", "can't read the record", "damaged", "untouched", "Check this export")
	for _, bad := range []string{".jsonl", "has no header", run} {
		if strings.Contains(out["error"].(string), bad) {
			t.Errorf("the refusal still says %q: %v", bad, out["error"])
		}
	}
	c := e.card(run)
	if next, _ := c["next"].(string); !strings.Contains(next, "can't read the record") || !hasAct(c, "check-this-export") {
		t.Errorf("the card of an export with a damaged record: %v %v", c["next"], c["actions"])
	}
	if chk := e.ok("/api/exports/check", map[string]any{"runId": run}); chk["green"] != true {
		t.Errorf("Check this export through the journal: %v", chk)
	}
	if _, err := os.Stat(filepath.Join(e.dest, root)); err != nil {
		t.Errorf("the folder is not untouched: %v", err)
	}

	// anything else: no raw text
	ref := engineRefusal(errors.New("boom: open /Users/someone/secret/path.jsonl: odd"))
	assure.Refused(t, ref.Status, ref.Text, assure.Code(statusRefused), assure.Names("couldn't do that", "nothing was changed", "try again"),
		assure.Forbid("boom", "/Users/", ".jsonl"))
	if ref.ID == "R-d2" || ref.Extra["detail"] == nil {
		t.Errorf("the fallback refusal: %v %v", ref.ID, ref.Extra)
	}
}
