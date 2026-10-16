package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
)

// The fresh-eyes re-verification of the EW-E4 friction fixes
// (docs/review-extraction-workspace-archive-friction.md, "Re-verification"): what it found, fixed test-first.

// removeSlug deletes the files the archive holds for a message: its page, its original and its zip.
func (f *archFixture) removeSlug(slug string) {
	f.t.Helper()
	page := f.path(f.rec(slug).Path)
	stem := strings.TrimSuffix(page, ".html")
	for _, p := range []string{page, stem + ".eml", stem + ".zip"} {
		os.Remove(p)
	}
}

// covers: MA-437, MA-420, MA-512, R12, R29
// RV1: a file really removed from the archive, and then the drive going away in the same run (R-o2), leaves
// that message retryable (friction F6: the run cannot tell the two apart). When the drive is back and the retry
// has exported what is there, the first export's card still offers "Export the remaining messages" (only the
// archive itself can say the removed files are gone), and a click on it is refused: the refusal says what
// happened (those messages are no longer in the archive, nothing was previewed), why (their files were
// removed) and what to do (restore them, or ask whoever gave you the archive; or leave it, nothing else is
// owed), in the words of the kind of folder, and writes nothing. The positive twin: the first retry, with the
// messages that are there, previews and exports.
func TestRetryOfRemovedMessagesAfterTheDriveWentAwayTellsWhatToDo(t *testing.T) {
	for _, kind := range []string{"archive", "archive-other"} {
		t.Run(kind, func(t *testing.T) {
			e := newWsEnv(t)
			dir := e.out
			if kind == "archive-other" {
				dir = filepath.Join(e.dir, "inherited")
			}
			f := buildArchiveAt(t, dir)
			src := archSource(kind, dir)
			os.Remove(e.tok)
			e.archFolders(src)
			id, _ := e.preview(e.archSel(src, entry("alice", true), entry("bob", false)))
			stash := filepath.Join(e.dir, "stash")
			os.MkdirAll(stash, 0o755)
			ents, _ := os.ReadDir(dir)
			var once sync.Once
			e.d.ex.env.SetStepHook(func(step string) {
				switch step {
				case "W6:1": // two files are removed from the archive, one by one ...
					f.removeSlug("second")
					f.removeSlug("client-mail")
				case "W6:5": // ... and then the drive goes
					once.Do(func() {
						for _, ent := range ents {
							os.Rename(filepath.Join(dir, ent.Name()), filepath.Join(stash, ent.Name()))
						}
					})
				}
			})
			run := e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
			if last := e.waitRun()["last"].(map[string]any); last["stopId"] != "R-o2" {
				t.Fatalf("the export did not stop with R-o2: %v", last)
			}
			e.d.ex.env.SetStepHook(nil)
			for _, ent := range ents { // the drive is back
				os.Rename(filepath.Join(stash, ent.Name()), filepath.Join(dir, ent.Name()))
			}

			// the twin: the retry previews what is there (the two removed messages are carried into the plan
			// and listed as removed from the archive, E4-AC10) and exports it
			pid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
			res := e.waitPreview(pid)
			if w := strings.Join(toStrings(res["warnings"]), " | "); !strings.Contains(w, "2 of these messages are no longer in the archive: they will be listed as removed from it and not exported") {
				t.Fatalf("the retry's preview: %q, want the two removed messages named as removed", w)
			}
			e.ok("/api/exports/start", map[string]any{"previewId": pid})
			last := e.waitRun()["last"].(map[string]any)
			if last["stopId"] != nil && last["stopId"] != "" {
				t.Fatalf("the retry stopped: %v", last)
			}
			// the retry's own list says what happened (removed from the archive), and what to do, in the words of the kind of folder
			fix := []string{"restore", "backup of the archive"}
			if kind == "archive-other" {
				fix = []string{"ask whoever gave you the archive"}
			}
			fails, _ := e.manifestOf(last["rootName"].(string))["failures"].([]any)
			if len(fails) != 2 {
				t.Fatalf("the retry's failures: %v, want the two removed messages", fails)
			}
			for _, fl := range fails {
				reason, _ := fl.(map[string]any)["reason"].(string)
				assure.Refused(t, 1, reason, assure.Code(1), assure.Names(append([]string{"removed from the archive after you selected it", "if that was not on purpose"}, fix...)...))
			}
			// the removed messages are settled by that retry: the first export's card does not offer them again
			if card := e.card(run); hasStr(card["actions"], "export-remaining") {
				t.Errorf("the first export's card still offers Export the remaining messages: %v", card["actions"])
			}

			// a click that arrives anyway (a stale page) is refused and says there is nothing left, and writes nothing
			before, _ := os.ReadDir(e.dest)
			code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
			msg, _ := out["error"].(string)
			if out["id"] != "R-h" {
				t.Fatalf("the click on the first export's card: %d %v, want R-h", code, out)
			}
			assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names("no messages left to export", "nothing left to do"),
				assure.NoSideEffect(func() bool { after, _ := os.ReadDir(e.dest); return len(after) == len(before) }))
			if kind == "archive-other" {
				for _, text := range []string{msg, fails[0].(map[string]any)["reason"].(string)} {
					lacks(t, "another folder's words", text, "backup of the archive", "Settings")
				}
			}
		})
	}
}

// covers: MA-435, MA-512, R12, R20
// RV4: the words for an archive that went away between the preview and Start name the way back that is true for
// the folder: the owner's own archive is pointed at its new place in Settings, another archive folder is chosen
// again as the Source and has nothing to do with Settings.
func TestAnArchiveThatWentAwayBeforeStartNamesTheWayBackForItsKind(t *testing.T) {
	owner, other := previewAgainAfterGone(false), previewAgainAfterGone(true)
	if !strings.Contains(owner, "Preview copy export again") || !strings.Contains(owner, "Archive location in Settings") {
		t.Errorf("the owner's archive: %q", owner)
	}
	if !strings.Contains(other, "Preview copy export again") || !strings.Contains(other, "Another archive folder") {
		t.Errorf("another archive folder: %q", other)
	}
	lacks(t, "another archive folder's words", other, "Settings", "Archive location")
}
