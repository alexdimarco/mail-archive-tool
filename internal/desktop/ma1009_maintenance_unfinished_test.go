package desktop

import (
	"net/http"
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1009, R36
// Check integrity and Rebuild search index start nothing on a current archive being deleted,
// whose delete did not finish, or whose deletions.log cannot be read, as Archive now and Run now
// (MA-987; del-s3b re-verification, leftover 4): a check would report the files a delete removed
// as damage, and a rebuild would write a new index into a folder the delete left part way. Each is
// refused in MA-987's words, nothing runs. Positive twin first: with no delete, each starts.
func TestCheckIntegrityAndRebuildRefuseAnArchiveWhoseDeleteDidNotFinish(t *testing.T) {
	e := newDeleteEnv(t)
	routes := []string{"/api/verify", "/api/rebuild"}
	notRunning := func() bool {
		c := e.d.cap
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.running
	}
	waitIdle := func() {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); !notRunning(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("the job did not finish")
			}
		}
	}
	current := func(name string) string {
		t.Helper()
		dir := e.plainArchive(name)
		if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	cur := current("Current")
	for _, r := range routes {
		if _, m := e.post(r, nil); m["ok"] != true {
			t.Fatalf("the positive twin: %s of the current archive: %v", r, m)
		}
		waitIdle()
	}

	// Being deleted.
	release, done := blockedDelete(t, e, cur)
	for _, r := range routes {
		code, m := e.post(r, nil)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
			assure.NoSideEffect(notRunning))
	}
	close(release)
	<-done
	e.sched.onRemove = nil

	// Its delete stopped at step 2: not finished.
	keep := current("Stopped")
	stopAt2(t, e, keep)
	for _, r := range routes {
		code, m := e.post(r, nil)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("Delete not finished: click Delete again"),
			assure.NoSideEffect(notRunning))
	}

	// deletions.log cannot be read: a folder where the file goes.
	current("Fine")
	logPath := e.cfg.deletionsLogPath()
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		code, m := e.post(r, nil)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("could not read its deletion log", logPath),
			assure.NoSideEffect(notRunning))
	}
}
