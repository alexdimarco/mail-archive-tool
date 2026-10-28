package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-877, MA-863, R37
// A step-2b failure on an edit of a run-job whose entry was not running (auto-paused, or lost by
// the scheduler) never starts its old schedule (the part A pass): the entry the edit made is
// taken out, nothing is left running, the job file and the archive's record are unchanged, and
// the reply says the backup was not started again — never "not changed". Positive twin first:
// with the entry running, the same failure puts the old schedule back (MA-863).
func TestANotRecordedEditOfAStoppedBackupLeavesItStopped(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	g.next = append(g.next, g.notRecorded())
	if body := g.post(`{"interval":"daily","time":"05:00"}`); !strings.Contains(body, "The backup schedule was not changed") || !g.fake.tasks[g.out] {
		t.Fatalf("the positive twin: %s (tasks %v)", body, g.fake.tasks)
	}

	delete(g.fake.tasks, g.out) // auto-paused: the entry is out, the record and job file kept
	g.fake.removed = nil
	jobB, descB := fileBytes(g.jobPath), fileBytes(filepath.Join(g.out, schedule.DescriptorName))
	g.next = append(g.next, g.notRecorded())
	body := g.post(`{"interval":"daily","time":"05:00"}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("could not be recorded in the archive (the archive folder is read-only), so it was not started again"),
		assure.Forbid("not changed"),
		assure.NoSideEffect(func() bool {
			return !g.fake.tasks[g.out] && len(g.fake.removed) == 1 &&
				string(fileBytes(g.jobPath)) == string(jobB) && string(fileBytes(filepath.Join(g.out, schedule.DescriptorName))) == string(descB)
		}))
}

// covers: MA-877, MA-863, R37
// A job file that can't be written during a broken run-job's Repair with no entry running (its
// job file missing, its record kept) takes out only the entry the Repair made and writes the
// archive's record back as it was, never deleting it (the part A pass); the reply says nothing
// is running and the record is kept, and the card still shows the broken run-job with its Repair.
// Positive twin first: the same Repair with the job file written succeeds.
func TestABrokenRepairThatFailsKeepsTheArchivesRecord(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"daily","time":"04:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	descPath := filepath.Join(g.out, schedule.DescriptorName)
	descB := fileBytes(descPath)
	broken := func() {
		os.Remove(g.jobPath)
		delete(g.fake.tasks, g.out)
		g.fake.removed = nil
	}
	broken()
	if body := g.post(`{}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, the Repair: %s", body)
	}

	broken()
	// Install rewrites the archive's record as it takes the entry (here with another time, so a
	// record left as Install wrote it shows).
	g.next = append(g.next, func(s schedule.Spec) error {
		g.record(s)
		d := schedule.DescribeSpec(s)
		d.At = "23:59"
		return schedule.WriteDescriptor(g.out, d)
	})
	failJobWrite(t)
	body := g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("The backup could not be set up (the disk is full), so nothing is running; this archive's record of its backup is kept as it was."),
		assure.NoSideEffect(func() bool {
			return !g.fake.tasks[g.out] && len(g.fake.removed) == 1 && string(fileBytes(descPath)) == string(descB) && fileBytes(g.jobPath) == nil
		}))
	if page := g.page(); !strings.Contains(page, `id="schedRepair"`) || !shows(page, "is missing. Repair makes it again") {
		t.Error("after the failed Repair the card no longer shows the broken run-job and its Repair")
	}
}
