package desktop

import (
	"errors"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// notRecorded is a scheduler that takes the entry (step 2a) but whose descriptor write fails
// (step 2b), as schedule.Install reports it.
func (g *a3Rig) notRecorded() func(schedule.Spec) error {
	return func(s schedule.Spec) error {
		g.record(s)
		return schedule.NotRecorded(errors.New("the archive folder is read-only"))
	}
}

// failJobWrite makes the next job-file writes fail after the scheduler step (step 3).
func failJobWrite(t *testing.T) {
	t.Helper()
	orig := writeJob
	writeJob = func(string, job.Job) error { return errors.New("the disk is full") }
	t.Cleanup(func() { writeJob = orig })
}

// covers: MA-863, R37
// A step-2b failure on an edit (the entry took the new cadence, the archive could not record it)
// puts the previous entry back from the descriptor, which still holds the old cadence, and says
// the schedule was not changed, with the reason (R3C-13); the job file is untouched and nothing
// advises Remove. Positive twin first: the same edit with a working scheduler changes it.
func TestANotRecordedEditPutsTheOldScheduleBack(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if body := g.post(`{"interval":"daily","time":"06:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, an edit: %s", body)
	}
	g.recordLast() // the descriptor now records daily at 06:00
	jobB := fileBytes(g.jobPath)

	g.next = append(g.next, g.notRecorded())
	n := len(g.installs)
	body := g.post(`{"interval":"weekly","time":"05:00"}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("The backup schedule was not changed: it could not be recorded in the archive (the archive folder is read-only)."),
		assure.NoSideEffect(func() bool {
			last := g.installs[len(g.installs)-1]
			return string(fileBytes(g.jobPath)) == string(jobB) && len(g.installs) == n+2 && last.Interval == schedule.Daily && last.At == "06:00"
		}))
	if strings.Contains(body, "Remove") {
		t.Errorf("the reply advises Remove: %s", body)
	}
}

// covers: MA-863, R37
// A step-2b failure on a create removes the entry it made, when none existed before, and says
// nothing was scheduled, with the reason; no job file is written.
func TestANotRecordedCreateRemovesTheNewEntry(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	g.next = append(g.next, g.notRecorded())
	body := g.post(`{"interval":"weekly","time":"03:00"}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("The backup could not be recorded in the archive (the archive folder is read-only), so nothing was scheduled."),
		assure.NoSideEffect(func() bool {
			return !g.fake.tasks[g.out] && len(g.fake.removed) == 1 && g.fake.removed[0] == g.out && fileBytes(g.jobPath) == nil
		}))
}

// covers: MA-863, R37
// A create over an entry that was already there (a broken run-job's Repair) never removes that
// entry, on a step-2b failure or on a job-file write failure; the reply says it is kept.
func TestACreateOverAnExistingEntryNeverRemovesIt(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	os.Remove(g.jobPath) // broken: the descriptor says run-job, the job file is missing

	// Each refusal through assure.Refused (the part A pass): it names what is kept, and leaves no
	// job file (the row's "a refusal leaves no job file") and the entry that was there.
	kept := func() bool { return fileBytes(g.jobPath) == nil && g.fake.tasks[g.out] && len(g.fake.removed) == 0 }
	g.next = append(g.next, g.notRecorded())
	body := g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("could not be recorded in the archive (the archive folder is read-only)", "the scheduler entry that was there is kept"),
		assure.NoSideEffect(kept))
	failJobWrite(t)
	body = g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("settings file could not be written (the disk is full)", "the scheduler entry that was there is kept"),
		assure.NoSideEffect(kept))
}

// covers: MA-863, R37
// A job file that can't be written after the scheduler step: on a create, what was installed is
// removed and the reply says nothing was scheduled; on an edit, the old job file stays whole and
// the reply says the new cadence is in effect and the sync was not changed — never advising
// Remove (today's "Remove the schedule on this card and try again" is gone).
func TestAJobFileWriteFailureIsAnsweredAsItHappened(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, create: %s", body)
	}
	g.recordLast()
	jobB := fileBytes(g.jobPath)
	failJobWrite(t)
	body := g.post(`{"interval":"daily","time":"05:00","presync":true}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("The new schedule (daily at 05:00) is in effect; Thunderbird sync was not changed: the disk is full."),
		assure.NoSideEffect(func() bool { return string(fileBytes(g.jobPath)) == string(jobB) }))
	if strings.Contains(body, "Remove") {
		t.Errorf("the edit's reply advises Remove: %s", body)
	}

	c := newA3Rig(t, tbProfileArchive(t), "darwin")
	body = c.post(`{"interval":"weekly","time":"03:00"}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("The backup could not be set up (the disk is full), so nothing was scheduled."),
		assure.NoSideEffect(func() bool {
			return !c.fake.tasks[c.out] && len(c.fake.removed) == 1 && fileBytes(c.jobPath) == nil
		}))
}

// covers: MA-863, R37
// A broken run-job (the descriptor records a run-job; its job file is missing) shows plain words
// and a Repair, and that Repair runs the create path: the job file is made again from the
// archive's own record and the reply says so, and that Thunderbird sync is now off. Before, the
// card called it an export to repair from the command line. Run now is not offered.
func TestABrokenRunJobsRepairMakesItsJobFileAgain(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"daily","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	os.Remove(g.jobPath)

	page := g.page()
	if !shows(page, "This archive's scheduled backup can't run: its settings file "+g.jobPath+" is missing. Repair makes it again from this archive's own record.") {
		t.Error("the broken run-job is not told in plain words")
	}
	if !strings.Contains(page, `id="schedRepair"`) || strings.Contains(page, `id="runBackupBtn"`) || strings.Contains(page, "an export backup") {
		t.Error("the broken run-job's card does not offer Repair alone, or calls it an export")
	}
	body := g.post(`{}`)
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, recreatedNote) || !strings.Contains(body, strings.TrimSpace(syncNowOffNote)) {
		t.Fatalf("the Repair: %s", body)
	}
	j, err := job.Read(g.jobPath)
	if err != nil || j.ThunderbirdSync != nil {
		t.Errorf("the Repair did not make the job file again with the sync off: %+v %v", j, err)
	}
	if last := g.installs[len(g.installs)-1]; last.Interval != schedule.Daily || last.At != "04:00" {
		t.Errorf("the Repair did not keep the recorded cadence: %+v", last)
	}
}

// covers: MA-863, R37
// A broken run-job whose job file can't be read shows section 6's unreadable-file message and its
// remedy, ahead of any other label, and no Repair (C-6 would refuse it); a crafted Repair is
// refused with nothing written.
func TestAnUnreadableRunJobShowsNoRepair(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if err := os.WriteFile(g.jobPath, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	page := g.page()
	if !shows(page, "This archive's backup settings file "+g.jobPath+" can't be read (it is damaged") || !shows(page, "Re-create the backup in the MailArchive wizard") {
		t.Error("the unreadable job file is not told with its remedy")
	}
	if i, j := strings.Index(page, `id="backupBroken"`), strings.Index(page, `id="schedKind"`); i < 0 || j < 0 || i > j {
		t.Error("the unreadable-file message is not ahead of the other labels")
	}
	for _, bad := range []string{`id="schedRepair"`, `id="runBackupBtn"`, `id="schedInstall"`} {
		if strings.Contains(page, bad) {
			t.Errorf("the unreadable run-job's card renders %s", bad)
		}
	}
	u := g.snapshot()
	body := g.post(`{}`)
	assure.Refused(t, refusedCode(body), body, assure.Names("can't be read", "Nothing was changed."), assure.NoSideEffect(u.holds))
}
