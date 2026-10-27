package desktop

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-847, R37
// A keyed backup in the job slot has its own cancel and skip (program design R20): the
// skip is offered only while the program step waits for its download and ends only that
// wait — the job goes on, Cancel still offered, a second skip refused; Cancel ends the
// job; with nothing running, both are refused. Any other job offers neither.
func TestTheJobSlotSkipsTheWaitAndCancelsAKeyedBackup(t *testing.T) {
	life := NewLifecycle(context.Background())
	t.Cleanup(life.Stop)
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: t.TempDir()})
	store := graphconfig.NewFileSecretStore(t.TempDir())
	d := newDashboard(Config{SettingsPath: settings, Store: store, Keys: newArchiveKeys(store, readerLockedNote), Lifecycle: life})
	activity := func() map[string]any {
		rec := httptest.NewRecorder()
		d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	until := func(what string, ok func(map[string]any) bool) map[string]any {
		t.Helper()
		for i := 0; i < 200; i++ {
			if m := activity(); ok(m) {
				return m
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("never: " + what)
		return nil
	}

	// Nothing running: both refused.
	c := callAction(t, d, d.cancelJobAction, nil)
	s := callAction(t, d, d.skipProgramAction, nil)
	assure.Refused(t, http.StatusOK, str(c["error"])+" "+str(s["error"]), assure.Code(http.StatusOK), assure.Names("Nothing is running", "No download"),
		assure.NoSideEffect(func() bool { return c["ok"] != true && s["ok"] != true }))

	// Any other job offers neither.
	release := make(chan struct{})
	if err := d.runJob(func(*log.Logger) (string, error) { <-release; return "done", nil }); err != nil {
		t.Fatal(err)
	}
	m := until("the plain job runs", func(m map[string]any) bool { return m["running"] == true })
	if m["cancellable"] == true || m["skippable"] == true {
		t.Fatalf("a plain job offers neither: %v", m)
	}
	close(release)
	until("the plain job ends", func(m map[string]any) bool { return m["running"] != true })

	ended := make(chan error, 1)
	backingUp := make(chan struct{}) // the backup's work before the program step
	if err := d.runKeyedJobOn(t.TempDir(), func(j keyedJob, l *log.Logger) (string, error) {
		<-backingUp
		j.onWait(true)
		select {
		case <-j.skip:
		case <-j.ctx.Done():
		}
		j.onWait(false)
		<-j.ctx.Done()
		ended <- j.ctx.Err()
		return "", j.ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	m = until("the keyed backup runs", func(m map[string]any) bool { return m["cancellable"] == true })
	if early := callAction(t, d, d.skipProgramAction, nil); early["ok"] == true || m["skippable"] == true {
		t.Fatalf("no wait yet: the skip is not offered, and refused: %v %v", early, m)
	}
	close(backingUp)
	m = until("the wait is offered a skip", func(m map[string]any) bool { return m["skippable"] == true })
	if m["cancellable"] != true {
		t.Fatalf("a keyed backup offers Cancel: %v", m)
	}
	if s := callAction(t, d, d.skipProgramAction, nil); s["ok"] != true {
		t.Fatalf("skip: %v", s)
	}
	m = until("the skip ends the wait only", func(m map[string]any) bool { return m["skippable"] != true })
	select {
	case <-ended:
		t.Fatal("the skip ended the job")
	case <-time.After(100 * time.Millisecond):
	}
	if again := callAction(t, d, d.skipProgramAction, nil); again["ok"] == true || m["running"] != true || m["cancellable"] != true {
		t.Fatalf("after the skip: a second skip %v, activity %v", again, m)
	}
	if c := callAction(t, d, d.cancelJobAction, nil); c["ok"] != true {
		t.Fatalf("cancel: %v", c)
	}
	var err error
	select {
	case err = <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("Cancel did not end the job")
	}
	m = until("the cancelled job ends", func(m map[string]any) bool { return m["running"] != true })
	assure.Reached(t, err == context.Canceled && m["cancellable"] != true && m["skippable"] != true, "Cancel ends the keyed backup")
}
