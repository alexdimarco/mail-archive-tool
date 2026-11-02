package archdelete

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// writeBackupDescriptor writes the archive's backup descriptor, as an install on this computer
// writes it: its derived name, daily at 02:00, the job given.
func writeBackupDescriptor(t *testing.T, dir string, jobArgs []string) string {
	t.Helper()
	host, _ := os.Hostname()
	name := schedule.DefaultNameFor(dir)
	if err := schedule.WriteDescriptor(dir, schedule.Descriptor{Version: 1, Name: name, Interval: "daily", At: "02:00",
		Exe: "/usr/local/bin/mailarchive", Job: jobArgs, InstalledAt: time.Now(), Host: host}); err != nil {
		t.Fatal(err)
	}
	return name
}

// covers: MA-1025, R36
// The plan lists only the scheduled backups step 2 would stop, by when they run, or says none
// (the 1.5.3 friction review's B1): the scheduler is asked, read-only, by step 2's own absent rule
// (Deps.Scheduler); the saved plan names each with when it runs, or "Schedule to stop: none"; the
// deletion record repeats those lines with what happened (stopped, or already absent when it went
// between the plan and step 2) and the result says "Stopped the scheduled backup that runs daily
// at 02:00." Before, every derived name was listed by its hash whether or not it was installed,
// and the record listed none, so plan and record disagreed. Positive twin first: an installed
// backup is listed, stopped, and said so.
func TestThePlanListsOnlyTheScheduledBackupsStep2WouldStop(t *testing.T) {
	when := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

	// Positive twin: the backup installed, its descriptor saying daily at 02:00.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	name := writeBackupDescriptor(t, dir, []string{"-out", dir, "-mode", "incremental", "-unattended"})
	p := r.plan(t)
	if got, want := p.ScheduleLines(), []string{"Scheduled backups it stops: the backup that runs daily at 02:00."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan lines %q, want %q", got, want)
	}
	plan := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	if !containsLine(plan, "Schedule to stop: "+name+" (runs daily at 02:00)") || strings.Contains(plan, name+"-verify") || containsLine(plan, "Schedule to stop: none") {
		t.Fatalf("saved plan:\n%s", plan)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || !hasLine(res, "Stopped the scheduled backup that runs daily at 02:00.") ||
		!containsLine(string(res.Record), "Schedule stopped: "+name+" (runs daily at 02:00)") {
		t.Fatalf("the twin's delete: %s\n%s", res.Message(), res.Record)
	}

	// Nothing installed: the plan says none, and so do the record and the result.
	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	r.sched.state = map[string]schedule.State{} // nothing installed
	p = r.plan(t)
	if got, want := p.ScheduleLines(), []string{"Scheduled backups it stops: none."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan lines %q, want %q", got, want)
	}
	plan = string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	if !containsLine(plan, "Schedule to stop: none") || strings.Contains(plan, schedule.DefaultNameFor(dir)) {
		t.Fatalf("saved plan names schedules that are not there:\n%s", plan)
	}
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || hasLine(res, "Stopped the scheduled") || !containsLine(string(res.Record), "Schedule stopped: none") ||
		strings.Contains(string(res.Record), schedule.DefaultNameFor(dir)) {
		t.Fatalf("nothing installed: %s\n%s", res.Message(), res.Record)
	}

	// Listed at the plan, gone by step 2: the record says it was already absent.
	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	def := schedule.DefaultNameFor(dir)
	p = r.plan(t)
	r.sched.state[def] = schedule.NotInstalled
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || !containsLine(string(res.Record), "Schedule already absent: "+def) || hasLine(res, "Stopped the scheduled") {
		t.Fatalf("gone by step 2: %s\n%s", res.Message(), res.Record)
	}

	// An encrypted archive, which cannot have a schedule, says nothing of none.
	enc := &DeletePlan{Kind: Encrypted, Schedules: Discovery{Entries: []Entry{{Name: "mailarchive-0000aaaa", State: StateAbsent}}}}
	if got := enc.ScheduleLines(); len(got) != 0 {
		t.Fatalf("an encrypted archive's plan lines: %q", got)
	}
	if text := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: enc})); strings.Contains(text, "Schedule to stop") {
		t.Fatalf("an encrypted archive's saved plan:\n%s", text)
	}
}

// covers: MA-1025, R36
// A Run now backup (a job file of the archive with no scheduler entry, MA-955) is the archive's
// own (the 1.5.3 friction review's B15): its run-job descriptor is confirmed through the job file
// it names, so the plan never lists it as "cannot confirm … check it yourself"; the plan says
// "Its Run now backup is removed too (it has no scheduled times)." and not that a scheduled
// backup is stopped; the delete removes its job file, the result says "Removed its Run now
// backup." and the record "Run now backup removed". Positive twin first: a run-job descriptor
// naming another folder's job file, or a file outside MailArchive's settings folder, stays
// unconfirmed.
func TestARunNowBackupIsTheArchivesOwn(t *testing.T) {
	jobDir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeJob := func(path, out string) {
		t.Helper()
		if err := job.Write(path, job.Job{Name: strings.TrimSuffix(filepath.Base(path), ".json"), Inputs: []string{t.TempDir()}, Out: out, Mode: "incremental"}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(path) })
	}

	// Positive twins: a descriptor naming another folder's job file, and one naming a file outside
	// the settings folder, are not confirmed.
	other := t.TempDir()
	dir := smallArchive(t, t.TempDir())
	otherJob := filepath.Join(jobDir, schedule.DefaultNameFor(other)+".json")
	writeJob(otherJob, other)
	writeBackupDescriptor(t, dir, []string{"-job", otherJob})
	if d, err := (FileDiscovery{}).Discover(dir); err != nil || len(d.Unconfirmed) != 1 {
		t.Fatalf("another folder's job file: %+v %v", d.Unconfirmed, err)
	}
	outside := filepath.Join(t.TempDir(), schedule.DefaultNameFor(dir)+".json")
	writeJob(outside, dir)
	writeBackupDescriptor(t, dir, []string{"-job", outside})
	if d, err := (FileDiscovery{}).Discover(dir); err != nil || len(d.Unconfirmed) != 1 {
		t.Fatalf("a job file outside the settings folder: %+v %v", d.Unconfirmed, err)
	}

	// The archive's own Run now backup.
	dir = smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	r.sched.state = map[string]schedule.State{} // no scheduler entry: Run now only
	def := schedule.DefaultNameFor(dir)
	own := filepath.Join(jobDir, def+".json")
	writeJob(own, dir)
	writeBackupDescriptor(t, dir, []string{"-job", own})
	p := r.plan(t)
	if len(p.Schedules.Unconfirmed) != 0 || len(p.Schedules.notRemoved()) != 0 {
		t.Fatalf("the Run now backup is listed as one to check: %+v", p.Schedules.Unconfirmed)
	}
	want := []string{"Scheduled backups it stops: none.", "Its Run now backup is removed too (it has no scheduled times)."}
	if got := p.ScheduleLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("plan lines %q, want %q", got, want)
	}
	plan := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
	if !containsLine(plan, "Run now backup to remove: "+def+" (runs daily at 02:00)") || strings.Contains(plan, "check it yourself") {
		t.Fatalf("saved plan:\n%s", plan)
	}
	res := Execute(p, p.Name, false, r.x)
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatalf("the Run now backup's job file is still there: %v", err)
	}
	if res.Status != StatusComplete || !hasLine(res, "Removed its Run now backup.") || hasLine(res, "Stopped the scheduled") ||
		!containsLine(string(res.Record), "Run now backup removed: "+def+" (runs daily at 02:00)") || len(r.sched.removed) != 0 {
		t.Fatalf("the delete: %s\n%s", res.Message(), res.Record)
	}
}
