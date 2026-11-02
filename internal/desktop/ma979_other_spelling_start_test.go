package desktop

import (
	"net/http"
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// The start checks against a delete match the folder on disk, not only its spelling (the delete
// residuals' security pass, CON-04: with every one made to compare spellings only, every shipped
// test stayed green). Each case names the folder through a linked parent folder (aliasOf) while the
// delete is of its real path, or the other way round.

// covers: MA-979, R36
// Pause through another spelling of a folder whose delete runs is refused, nothing sealed; and a
// delete that begins while a Pause through another spelling of its folder runs is refused in the
// Pause's words, its plan kept. Positive twins first: a Pause through a link to another archive
// works while the delete runs; the kept plan's delete completes once the Pause ended.
func TestAPauseThroughAnotherSpellingMeetsTheDeleteOfItsFolder(t *testing.T) {
	e := newDeleteEnv(t)
	other := e.plainArchive("Other")
	dir := e.plainArchive("Deleting")
	release, done := blockedDelete(t, e, dir)
	if _, m := e.post("/api/pause-archive", map[string]any{"path": aliasOf(t, other)}); m["ok"] != true || !seal.IsSealed(other) {
		t.Fatalf("positive twin: Pause through a link to another archive while a delete runs: %v", m)
	}
	_, p := e.post("/api/pause-archive", map[string]any{"path": aliasOf(t, dir)})
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return p["ok"] == false && !seal.IsSealed(dir) }))
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the delete of %s: %v", dir, r)
	}

	held := e.plainArchive("Held")
	pl := e.plan(held)
	if pl["ok"] != true {
		t.Fatalf("the plan: %v", pl)
	}
	tok := pl["token"].(string)
	orig := pauseArchiveRun
	t.Cleanup(func() { pauseArchiveRun = orig })
	var during map[string]any
	pauseArchiveRun = func(path, reason, deletionsLog string) error {
		during = e.deleteWith(tok, pl["name"].(string), false)
		return orig(path, reason, deletionsLog)
	}
	if _, m := e.post("/api/pause-archive", map[string]any{"path": aliasOf(t, held)}); m["ok"] != true {
		t.Fatalf("the Pause through a link: %v", m)
	}
	assure.Refused(t, http.StatusOK, replyErr(during), assure.Code(http.StatusOK), assure.Names(pausingRefusal),
		assure.NoSideEffect(func() bool {
			return during["ok"] == false && archdelete.IsArchive(held) && e.listed(held) && e.d.pending.get(tok, isDeletePlan) != nil
		}))
	pauseArchiveRun = orig
	if _, m := e.post("/api/resume-archive", map[string]any{"path": held}); m["ok"] != true {
		t.Fatalf("the Resume: %v", m)
	}
	if r := e.deleteWith(tok, pl["name"].(string), false); r["status"] != "complete" {
		t.Errorf("positive twin: the kept plan's delete once the Pause ended: %v", r)
	}
}

// covers: MA-1009, R36
// A job on the current archive set through another spelling of its folder refuses, as it takes the
// slot, a delete of the folder's real path begun after the route's own check (MA-1009's slot
// check, by file identity). The `statFolder` seam (timing only) begins the delete in the job's own
// ask of the disk. Positive twin first: with no delete, Check integrity starts.
func TestAJobThroughAnotherSpellingRefusesADeleteOfItsFolder(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Current")
	cur := aliasOf(t, dir)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	notRunning := func() bool {
		c := e.d.cap
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.running
	}
	if _, m := e.post("/api/verify", nil); m["ok"] != true {
		t.Fatalf("the positive twin: Check integrity of the current archive: %v", m)
	}
	for deadline := time.Now().Add(30 * time.Second); !notRunning(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the job did not finish")
		}
	}

	orig := statFolder
	t.Cleanup(func() { statFolder = orig })
	armed := true
	var end func()
	statFolder = func(p string) (os.FileInfo, error) {
		if armed && p == cur {
			armed = false
			var ok bool
			if end, ok = e.d.running.tryBegin(dir, dir); !ok {
				t.Error("the delete could not begin")
			}
		}
		return orig(p)
	}
	code, m := e.post("/api/verify", nil)
	statFolder = orig
	if end == nil {
		t.Fatal("the job never asked the disk about the archive before taking the slot")
	}
	defer end()
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(notRunning))
}

// covers: MA-979, R36
// A preview, and an export started from a preview made before, of an archive source named through
// another spelling of a folder whose delete is under way are refused (MA-979's start checks, by
// file identity). Positive twin first: the preview of it is made before the delete begins.
func TestAPreviewOrAnExportThroughAnotherSpellingMeetsTheDeleteOfItsFolder(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Previewed")
	arch := archSource("archive-other", aliasOf(t, dir))
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))
	id, _ := e.preview(sel)
	if id == "" {
		t.Fatal("positive twin: the preview through a link is made")
	}
	end, ok := e.d.running.tryBegin(dir, dir)
	if !ok {
		t.Fatal("no delete could be recorded")
	}
	defer end()
	code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
	assure.Refused(t, code, toText(out), assure.Code(code), assure.Names(deletingExport),
		assure.NoSideEffect(func() bool { _, live := e.d.exportState(); return !live }))
	f := e.previewFailure(sel)
	assure.Refused(t, http.StatusOK, toText(f), assure.Code(http.StatusOK), assure.Names(deletingPreview),
		assure.NoSideEffect(func() bool { _, live := e.d.exportState(); return !live }))
}
