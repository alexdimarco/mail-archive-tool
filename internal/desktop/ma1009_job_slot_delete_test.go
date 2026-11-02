package desktop

import (
	"net/http"
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1009, R36
// A delete of the current archive that begins after Check integrity's or Rebuild's own check, and
// before the job takes the slot, is refused by the job as it takes the slot, in MA-979's words,
// nothing run (the residual fixes' adversarial pass, finding 4): the delete's busy check reads the
// slot, so without this neither side would see the other. The `statFolder` seam (timing only)
// begins the delete in the job's own ask of the disk, made just before the slot's lock. Positive
// twin first: with no delete, each starts.
func TestAJobRefusesADeleteBegunAfterItsRoutesCheck(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
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
	for _, r := range routes {
		if _, m := e.post(r, nil); m["ok"] != true {
			t.Fatalf("the positive twin: %s of the current archive: %v", r, m)
		}
		waitIdle()
	}

	orig := statFolder
	t.Cleanup(func() { statFolder = orig })
	for _, r := range routes {
		armed := true
		var end func()
		statFolder = func(p string) (os.FileInfo, error) {
			if armed && samePath(p, cur) {
				armed = false // tryBegin asks the disk too
				var ok bool
				if end, ok = e.d.running.tryBegin(cur, cur); !ok {
					t.Error("the delete could not begin")
				}
			}
			return orig(p)
		}
		code, m := e.post(r, nil)
		statFolder = orig
		if end == nil {
			t.Fatalf("%s never asked the disk about the archive before taking the slot", r)
		}
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
			assure.NoSideEffect(notRunning))
		end()
	}
}
