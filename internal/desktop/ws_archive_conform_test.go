package desktop

import (
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
)

// covers: MA-435, R20, R29, S52
// The workspace never reads search.db inside Rebuild's rename window (design §4.3, XC20), through the
// Maintenance card's own Rebuild job and not a seam of the test's: while the job runs, the archive's
// message list says the index is being rebuilt and the preview says its names carry no index data,
// whole-folder selection still resolves from the manifest; when the job is done the list is back.
func TestTheRebuildJobHoldsTheArchiveListOffUntilItIsDone(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	if rows, _ := e.archRows(arch, "alice"); len(rows) != 2 { // the positive twin: the list works before the job
		t.Fatalf("alice's list before the rebuild: %d rows, want 2", len(rows))
	}

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(release) }) }
	t.Cleanup(let)
	e.d.rebuild = func(out string, l *log.Logger) (app.RebuildReport, error) {
		close(entered)
		<-release
		return app.RebuildReport{}, nil
	}
	e.ok("/api/rebuild", nil)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the Rebuild job never started")
	}

	_, held := e.archRows(arch, "alice")
	if n, _ := held["listUnavailable"].(string); !strings.Contains(n, "being rebuilt") || len(held["rows"].([]any)) != 0 {
		t.Errorf("the list while the Rebuild job runs: %v, want it held off", held)
	}
	_, res := e.preview(e.archSel(arch, entry("alice", true)))
	if res["count"] != float64(3) || !strings.Contains(strings.Join(toStrings(res["warnings"]), "|"), "being rebuilt") {
		t.Errorf("whole-folder selection during the rebuild: %v, want 3 messages and a warning that the index is being rebuilt", res)
	}

	let()
	deadline := time.Now().Add(10 * time.Second)
	for e.d.rebuilding.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the Rebuild job never let the list back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rows, out := e.archRows(arch, "alice"); len(rows) != 2 || out["listUnavailable"] != nil {
		t.Errorf("the list after the rebuild: %d rows, %v", len(rows), out["listUnavailable"])
	}
}
