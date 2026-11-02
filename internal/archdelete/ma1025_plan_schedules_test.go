package archdelete

import (
	"errors"
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
// On Windows the plan asks step 2's whole absent rule (the 1.5.3 friction fixes' adversarial
// pass, finding 7): a backup Task Scheduler does not list, but whose side files are there while
// the scheduler does not say it is missing, is one step 2 stops (it asks RemoveLiveEntry), so the
// plan lists it and it is never taken for a Run now backup, even with a job file. Positive twin
// first: when the scheduler says it is missing, it is absent — not listed, and with a job file the
// archive's Run now backup.
func TestTheWindowsPlanAsksTheSideFileRule(t *testing.T) {
	jobDir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	setup := func(saysMissing bool) (*rig, *DeletePlan, string) {
		t.Helper()
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		r.x.Deps.GOOS = "windows"
		r.x.Deps.Scheduler = r.sched
		def := schedule.DefaultNameFor(dir)
		r.sched.state = map[string]schedule.State{} // Task Scheduler does not list it
		r.sched.sideFiles[def] = true
		r.sched.saysMissing = saysMissing
		own := filepath.Join(jobDir, def+".json")
		if err := job.Write(own, job.Job{Name: def, Inputs: []string{t.TempDir()}, Out: dir, Mode: "incremental"}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(own) })
		writeBackupDescriptor(t, dir, []string{"-job", own})
		return r, r.plan(t), def
	}
	listed := func(p *DeletePlan, name string) bool {
		for _, e := range p.Schedules.Listed() {
			if e.Name == name {
				return true
			}
		}
		return false
	}
	runNow := func(p *DeletePlan, name string) bool {
		for _, e := range p.Schedules.RunNowBackups() {
			if e.Name == name {
				return true
			}
		}
		return false
	}

	// Positive twin: the scheduler says it is missing.
	_, p, def := setup(true)
	if listed(p, def) || !runNow(p, def) {
		t.Fatalf("said missing: listed %v, Run now %v (%+v)", listed(p, def), runNow(p, def), p.Schedules.Entries)
	}

	// Not said missing, its side files there: step 2 stops it, so the plan lists it.
	r, p, def := setup(false)
	if !listed(p, def) || runNow(p, def) {
		t.Fatalf("side files there: listed %v, Run now %v (%+v)", listed(p, def), runNow(p, def), p.Schedules.Entries)
	}
	if got := p.ScheduleLines(); len(got) == 0 || got[0] != "Scheduled backups it stops: the backup that runs daily at 02:00." {
		t.Fatalf("plan lines %q", got)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || !contains(r.sched.removed, def) || !containsLine(string(res.Record), "Schedule stopped: "+def+" (runs daily at 02:00)") ||
		strings.Contains(string(res.Record), "Run now backup") {
		t.Fatalf("the delete (removed %q): %s\n%s", r.sched.removed, res.Message(), res.Record)
	}
}

// covers: MA-1025, R36
// The record of a delete that stopped at step 2 answers every scheduled backup its plan listed (the
// 1.5.3 friction fixes' adversarial pass, finding 5): one step 2 did not stop is "Schedule not
// stopped: NAME (runs …)", beside "Schedule stopped" for one it did. Before, such a record said
// "Schedule stopped: none" and nothing of the backups its plan said it would stop. Positive twin
// first: a delete that completes has no "Schedule not stopped".
func TestARecordStoppedAtStep2NamesTheBackupsNotStopped(t *testing.T) {
	when := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	name := writeBackupDescriptor(t, dir, []string{"-out", dir, "-mode", "incremental", "-unattended"})
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || strings.Contains(string(res.Record), "Schedule not stopped") {
		t.Fatalf("the positive twin:\n%s", res.Record)
	}

	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	name = writeBackupDescriptor(t, dir, []string{"-out", dir, "-mode", "incremental", "-unattended"})
	verify := name + "-verify"
	r.sched.state[verify] = schedule.Installed
	p = r.plan(t)
	if got := len(p.Schedules.Listed()); got != 2 {
		t.Fatalf("the plan should list both: %d", got)
	}
	plan := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	if !containsLine(plan, "Schedule to stop: "+name+" (runs daily at 02:00)") || !containsLine(plan, "Schedule to stop: "+verify) {
		t.Fatalf("saved plan:\n%s", plan)
	}
	r.sched.fail[verify] = errors.New("the scheduler refused") // the backup stops, its check does not
	res = Execute(p, p.Name, false, r.x)
	rec := string(res.Record)
	if res.Status != StatusPartial || !containsLine(rec, "Schedule stopped: "+name+" (runs daily at 02:00)") ||
		!containsLine(rec, "Schedule not stopped: "+verify) || strings.Contains(rec, "Schedule not stopped: "+name+" ") || containsLine(rec, "Schedule stopped: none") {
		t.Fatalf("a delete stopped at step 2 (%s):\n%s", res.Status, rec)
	}

	// Stopped before any was stopped: each listed one is not stopped, and none is said stopped.
	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	r.x.Deps.Scheduler = r.sched
	name = writeBackupDescriptor(t, dir, []string{"-out", dir, "-mode", "incremental", "-unattended"})
	p = r.plan(t)
	r.sched.fail[name] = errors.New("the scheduler refused")
	res = Execute(p, p.Name, false, r.x)
	rec = string(res.Record)
	if res.Status != StatusPartial || !containsLine(rec, "Schedule not stopped: "+name+" (runs daily at 02:00)") || !containsLine(rec, "Schedule stopped: none") {
		t.Fatalf("a delete stopped at its first backup:\n%s", rec)
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
	// No "(runs daily at 02:00)" beside "it has no scheduled times" (adversarial pass, finding 8).
	if !containsLine(plan, "Run now backup to remove: "+def+" (no scheduled times)") || strings.Contains(plan, "check it yourself") || strings.Contains(plan, "(runs ") {
		t.Fatalf("saved plan:\n%s", plan)
	}
	res := Execute(p, p.Name, false, r.x)
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatalf("the Run now backup's job file is still there: %v", err)
	}
	if res.Status != StatusComplete || !hasLine(res, "Removed its Run now backup.") || hasLine(res, "Stopped the scheduled") ||
		!containsLine(string(res.Record), "Run now backup removed: "+def+" (no scheduled times)") || strings.Contains(string(res.Record), "(runs ") || len(r.sched.removed) != 0 {
		t.Fatalf("the delete: %s\n%s", res.Message(), res.Record)
	}
}
