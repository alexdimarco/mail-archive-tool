package desktop

import (
	"net/http"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-987, R36, S41
// Archive now (api/capture) and Run now (api/run-backup) start nothing on an archive the page was
// not drawn for, nor on a current archive being deleted or whose delete did not finish
// (docs/design-archive-delete.md §3.1 "Where the result sits", §5.4; del-s3b-A1-03, A3-03 and
// conformance gap 2): while a delete's result is held the page is not drawn again after a switch,
// so its cards can name the archive it was drawn for while the server's current archive is
// another; and the emptied-folder guard lets a run into a folder a delete left part way, since
// its manifest goes last (§14, MA-956). The refusal names the cause; nothing runs. A post naming
// no folder (a client older than this page) is not refused as stale. Positive twin first: a post
// from a page drawn for the current archive reaches each route's own answer.
func TestArchiveNowAndRunNowRefuseAStalePageAndADeleteNotFinished(t *testing.T) {
	e := newDeleteEnv(t, withoutM365)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	cur := e.plainArchive("Current")
	other := e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	routes := []string{"/api/capture", "/api/run-backup"}
	notRunning := func() bool {
		c := e.d.cap
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.running
	}
	guard := map[string]bool{staleRefusal: true, runUnfinishedText: true, deletingRefusal: true}
	for _, r := range routes {
		_, m := e.post(r, map[string]any{"drawnFor": cur})
		if guard[replyErr(m)] {
			t.Fatalf("the positive twin: %s from a page drawn for the current archive is refused: %v", r, m)
		}
		_, m = e.post(r, nil)
		if guard[replyErr(m)] {
			t.Fatalf("the positive twin: %s from a page naming no folder is refused: %v", r, m)
		}
	}

	// A page drawn for another archive than the current one.
	for _, r := range routes {
		code, m := e.post(r, map[string]any{"drawnFor": other})
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("out of date", "reload"),
			assure.NoSideEffect(notRunning))
		// The same folder spelled with a trailing separator is the archive the page was drawn for.
		if _, m := e.post(r, map[string]any{"drawnFor": cur + string(filepath.Separator)}); replyErr(m) == staleRefusal {
			t.Errorf("%s calls a page drawn for the current archive, spelled with a trailing separator, out of date", r)
		}
	}

	// The current archive being deleted.
	release, done := blockedDelete(t, e, cur)
	for _, r := range routes {
		code, m := e.post(r, map[string]any{"drawnFor": cur})
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("being deleted"),
			assure.NoSideEffect(notRunning))
	}
	close(release)
	<-done
	e.sched.onRemove = nil

	// The current archive's delete stopped at step 2: not finished.
	keep := e.plainArchive("Stopped")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: keep}); err != nil {
		t.Fatal(err)
	}
	stopAt2(t, e, keep)
	for _, r := range routes {
		for _, body := range []any{map[string]any{"drawnFor": keep}, nil} {
			code, m := e.post(r, body)
			assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("Delete not finished: click Delete again", "not backed up"),
				assure.NoSideEffect(notRunning))
		}
	}
}
