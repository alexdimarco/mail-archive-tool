package wizard

import (
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// covers: MA-950, MA-949, R14
// A backup run only when the person clicks Run now (MA-949) is never repaired into a schedule
// they did not choose. Positive twin first: a run-job whose entry is gone is offered a repair in
// the wizard's health view and RepairSpec builds its daily entry. Recorded as Run now only, the
// health view offers no repair, RepairSpec refuses in plain words and builds nothing, and Resume
// after Pause puts no entry back (there was none) and answers ok.
func TestRunNowOnlyIsNeverRepairedIntoASchedule(t *testing.T) {
	out := t.TempDir()
	name := schedule.DefaultNameFor(out)
	desc := schedule.Descriptor{Name: name, Interval: string(schedule.Daily), At: "02:00", Job: []string{"-job", "x"}}
	in := health.Input{HasDescriptor: true, Desc: desc, Out: out,
		KindEv:     schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: "/j/" + name + ".json"},
		SchedState: schedule.NotInstalled, ExeExists: true, ExeIsThis: true}

	// Positive twin: a scheduled run-job whose entry is gone.
	if !repairable(in) {
		t.Fatal("the positive twin: a run-job whose entry is gone is not offered a repair")
	}
	if spec, err := RepairSpec(out, desc, "/opt/mailarchive"); err != nil || spec.Interval != schedule.Daily {
		t.Fatalf("the positive twin: RepairSpec %+v %v", spec, err)
	}

	desc.Interval = string(schedule.Manual)
	in.Desc = desc
	if repairable(in) {
		t.Error("the wizard's health view offers to repair a Run now only backup into a schedule")
	}
	spec, err := RepairSpec(out, desc, "/opt/mailarchive")
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	rc := 0
	if err != nil {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("This archive backs up only when you click Run now, so it has no scheduled backup to repair", "choose How often in MailArchive Desktop's Settings"),
		assure.NoSideEffect(func() bool { return spec.Name == "" && spec.Interval == "" }))

	// Pause, then Resume: no entry is put back, and Resume answers ok.
	if err := schedule.WriteDescriptor(out, schedule.DescribeManual(schedule.Spec{Name: name, Interval: schedule.Daily, At: "03:00", Exe: "/opt/mailarchive", Args: []string{"-job", "x"}, Out: out})); err != nil {
		t.Fatal(err)
	}
	var removed []string
	orig := removeLiveEntry
	removeLiveEntry = func(s schedule.Spec) error { removed = append(removed, s.Name); return nil }
	defer func() { removeLiveEntry = orig }()
	if err := PauseArchive(out, "parked"); err != nil || !seal.IsSealed(out) {
		t.Fatalf("pause: %v", err)
	}
	if err := ResumeArchive(out); err != nil || seal.IsSealed(out) {
		t.Fatalf("resume of a Run now only archive: %v", err)
	}
	if d, err := schedule.ReadDescriptor(out); err != nil || !d.IsManual() {
		t.Fatalf("resume changed the choice: %+v %v", d, err)
	}
}
