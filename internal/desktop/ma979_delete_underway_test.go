package desktop

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// blockedDelete starts a delete of dir through the routes and stands it inside step 2 (its
// scheduled backup's removal) until release is closed; done carries its reply.
func blockedDelete(t *testing.T, e *delEnv, dir string) (release chan struct{}, done chan map[string]any) {
	t.Helper()
	name := schedule.DefaultNameFor(dir)
	e.sched.installed[name] = true
	entered, release := make(chan struct{}), make(chan struct{})
	var once bool
	e.sched.onRemove = func(n string) {
		if n == name && !once {
			once = true
			close(entered)
			<-release
		}
	}
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	done = make(chan map[string]any, 1)
	go func() {
		_, r := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
		done <- r
	}()
	select {
	case <-entered:
	case <-time.After(20 * time.Second):
		t.Fatal("the delete never reached step 2")
	}
	return release, done
}

// covers: MA-979, R36
// A delete under way is said as what it is (del-s3a-A3-01, A2-03, A3-02): its row says "Being
// deleted" in place of Switch and Pause, offers no Delete and is not "Delete not finished"; a
// second plan of it and Remove from list of it are refused in those words, nothing changed; Shut
// down is refused while it runs, so it is never cut off by a reply that the archives are
// untouched. Positive twins first: the row offers Delete before, and when the delete ends its
// result is complete and nothing is under way.
func TestADeleteUnderWayIsSaidAndRefusesWhatWouldMeetIt(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Running")
	if row := rowFor(t, e.page(), dir); !strings.Contains(row, `data-delete-ok="true"`) || strings.Contains(row, "data-delete-running") {
		t.Fatalf("the positive twin: the row before the delete: %s", row)
	}
	release, done := blockedDelete(t, e, dir)

	row := rowFor(t, e.page(), dir)
	if !strings.Contains(row, "data-delete-running") || !strings.Contains(row, `data-delete-ok="false"`) ||
		!strings.Contains(row, `data-delete-why="`+deletingRowWhy+`"`) || !strings.Contains(row, "Being deleted") ||
		strings.Contains(row, "Delete not finished") || strings.Contains(row, "data-switch-archive") || strings.Contains(row, "data-pause-archive") {
		t.Errorf("the row while its delete runs: %s", row)
	}
	p := e.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.Forbid("A backup of this archive is running"),
		assure.NoSideEffect(func() bool { return p["token"] == nil }))
	_, u := e.post("/api/archive/unlist", map[string]any{"path": dir, "confirm": true})
	assure.Refused(t, http.StatusOK, replyErr(u), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return e.listed(dir) }))
	_, s := e.post("/api/shutdown", nil)
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK), assure.Names("A delete is running", dir, "Nothing was stopped."),
		assure.Forbid("untouched"),
		assure.NoSideEffect(func() bool { return e.d.life.Context().Err() == nil }))

	close(release)
	r := <-done
	if r["ok"] != true || r["status"] != "complete" {
		t.Fatalf("the delete: %v", r)
	}
	if e.d.running.any() != nil {
		t.Error("a delete is still recorded as under way after it ended")
	}
}

// covers: MA-979, R36
// A second delete of a folder whose delete is under way — planned from another row naming the
// same folder — is refused in the under-way words with its token kept, never sent to meet the
// first one's archive lock. Positive twin first: once the first ends, nothing is under way.
func TestASecondDeleteOfAFolderUnderWayIsRefused(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Twice")
	alias := aliasOf(t, dir)
	e.list(alias)
	p2 := e.plan(alias)
	if p2["ok"] != true {
		t.Fatalf("the plan from the second row: %v", p2)
	}
	release, done := blockedDelete(t, e, dir)
	r := e.deleteWith(p2["token"].(string), p2["name"].(string), false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.Forbid("A backup of this archive is running"),
		assure.NoSideEffect(func() bool { return e.d.pending.get(p2["token"].(string), isDeletePlan) != nil }))
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the first delete: %v", r)
	}
	if e.d.running.any() != nil {
		t.Error("a delete is still recorded as under way")
	}
}

// covers: MA-979, R36
// Ctrl-C or a logoff during a delete (the lifecycle's drain) waits for the delete to end, within
// the drain's budget, before MailArchive lets go (del-s3a-A2-03, A3-02): the delete is not cut
// off part way. Positive twin first: Shut down with nothing under way stops MailArchive.
func TestShutDownWaitsForADeleteUnderWay(t *testing.T) {
	twin := newDeleteEnv(t)
	if _, s := twin.post("/api/shutdown", nil); s["ok"] != true {
		t.Fatalf("the positive twin: %v", s)
	}

	e := newDeleteEnv(t)
	dir := e.plainArchive("Draining")
	release, done := blockedDelete(t, e, dir)
	e.d.life.Stop() // Ctrl-C
	select {
	case <-e.d.life.Drained():
		t.Fatal("MailArchive let go while a delete was under way")
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	select {
	case <-e.d.life.Drained():
	case <-time.After(DrainBudget):
		t.Fatal("the drain did not end once the delete did")
	}
	if r := <-done; r["status"] != "complete" {
		t.Errorf("the delete under the drain: %v", r)
	}
}

// covers: MA-979, R36
// A delete that begins once Shut down began is refused, nothing deleted, its plan's token kept
// for nothing (a Shut down began between the Shut down check of a delete under way and this
// delete). Positive twin first: the same plan, MailArchive not stopping, deletes.
func TestADeleteRefusesOnceShutDownBegan(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	e := newDeleteEnv(t, func(w *wsEnv) {
		// A lifecycle whose stop is not drained yet: the moment between Stop and the drain.
		w.cfg.Lifecycle = &Lifecycle{ctx: ctx, cancel: stop, drained: make(chan struct{})}
	})
	twin := e.plainArchive("Twin")
	if r := e.planAndDelete(twin); r["status"] != "complete" {
		t.Fatalf("the positive twin: %v", r)
	}
	dir := e.plainArchive("Stopping")
	p := e.plan(dir)
	before := treeState(t, dir)
	stop()
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(shuttingDownText),
		assure.NoSideEffect(func() bool { return fmt.Sprint(treeState(t, dir)) == fmt.Sprint(before) && e.listed(dir) }))
}

// covers: MA-979, R36
// A workspace preview reading an archive is §7's busy row for its delete (del-s3a-A2-04): the
// plan is refused while it runs. And the other way round, an archive being deleted is neither
// previewed nor exported (del-s3a-A2-04, A4-03): a preview of it fails with its own words, and an
// export started from a preview made before is refused, nothing exported. Positive twins first: a
// preview job on another archive refuses nothing, and the preview and its export start before.
func TestAPreviewOrAnExportOfAnArchiveBeingDeletedIsRefused(t *testing.T) {
	e := newDeleteEnv(t)
	dir, other := e.plainArchive("Previewed"), e.plainArchive("Elsewhere")

	job := &wsPreviewJob{id: "j", cancel: func() {}, arch: true, path: other, state: "running"}
	e.d.ws.mu.Lock()
	e.d.ws.job = job
	e.d.ws.mu.Unlock()
	if p := e.plan(dir); p["ok"] != true {
		t.Fatalf("the positive twin: a preview of another archive: %v", p)
	}
	job.path = dir
	p := e.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("A backup of this archive is running."),
		assure.NoSideEffect(func() bool { return p["token"] == nil }))
	job.set(func() { job.state = "done" })

	arch := archSource("archive-other", dir)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))
	id, _ := e.preview(sel)

	end, ok := e.d.running.tryBegin(dir, dir) // a delete of it under way
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
