package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-866, R37
// A run-job's Repair goes to repairRunJob whatever the kind (C-5): it rebuilds the entry from the
// descriptor's cadence and never rewrites the job file, so it keeps the sync and works with a
// source missing — the job file's bytes unchanged. Before, a local archive's Repair rebuilt the
// job from the archive's record, which a missing source refused. With Microsoft 365 set up (a
// capture counted) the same Repair is refused and nothing is written.
func TestARunJobRepairNeverRewritesItsJobFile(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"daily","time":"05:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	jobB := fileBytes(g.jobPath)

	store := archivedStore(t, out)
	if err := os.Rename(store, store+"-away"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Rename(store+"-away", store) })

	n := len(g.installs)
	body := g.post(`{}`)
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("Repair with the source missing: %s", body)
	}
	if len(g.installs) != n+1 {
		t.Fatalf("Repair installed %d entries", len(g.installs)-n)
	}
	last := g.installs[len(g.installs)-1]
	if last.Args[0] != "-job" || last.Interval != schedule.Daily || last.At != "05:00" {
		t.Errorf("Repair did not rebuild the run-job at its recorded cadence: %+v", last)
	}
	if string(fileBytes(g.jobPath)) != string(jobB) {
		t.Error("Repair rewrote the job file")
	}

	countCapture(t, out)
	u := g.snapshot()
	body = g.post(`{}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(captureCountedRefus), assure.NoSideEffect(u.holds))
	if err := schedule.CheckKind(schedule.BuildKindEvidence(out), schedule.KindRunJob, schedule.KindRunJob, out, filepath.Base(g.jobPath)); err == nil {
		t.Error("the guard, the backstop, no longer refuses this run-job where a capture is counted")
	}
}
