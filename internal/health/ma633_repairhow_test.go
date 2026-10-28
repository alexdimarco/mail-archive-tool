package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-633, R18, S59
// The per-kind repair remedies (§4.6.3): repairHow renders schedule.RepairRoute
// in each reading surface's words. A corroborated run-job repairs in the wizard
// or the Scheduled backup card; a corroborated capture on the card; an export
// schedule on the command line; a route with no single repair names both
// explicit installs. scheduleCommand (the `-install` line) appears for the
// export kind only — the one placed exception is the command line's run-job
// no-display fallback, which names `-install` only after its `-remove` step and
// names the job file, so no remedy ever offers a `schedule … -install` the kind
// guard (MA-631) would refuse for a run-job or capture schedule.
func TestMA633_RepairHowPerKindPerSurface(t *testing.T) {
	ev := func(ks ...schedule.Kind) schedule.KindEvidence {
		e := schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{}}
		for _, k := range ks {
			e.Corroborated[k] = true
		}
		return e
	}
	runjobEv := ev(schedule.KindRunJob)
	runjobEv.JobPath = "/home/u/.config/mailarchive/jobs/mailarchive-1234abcd.json"

	base := func(kindEv schedule.KindEvidence, job []string, surface string) Input {
		return Input{Out: "/a", GOOS: "linux", Surface: surface, KindEv: kindEv,
			Desc: schedule.Descriptor{Name: "mailarchive-1234abcd", Job: job}}
	}

	// run-job: the wizard/card words per surface; the CLI adds the no-display
	// fallback, the only run-job remedy that names `schedule … -install`.
	runjob := []string{"run-job", "-job", "x"}
	for _, tc := range []struct {
		surface, want string
		wantInstall   bool
	}{
		{"desktop", "Repair on this archive's Scheduled backup card", false},
		{"gui", "Repair the scheduled backup", false},
		{"", "with no display", true},
	} {
		got := repairHow(base(runjobEv, runjob, tc.surface))
		if !strings.Contains(got, tc.want) {
			t.Errorf("run-job %q surface: %q is missing %q", tc.surface, got, tc.want)
		}
		hasInstall := strings.Contains(got, "schedule -out") && strings.Contains(got, "-install")
		if hasInstall != tc.wantInstall {
			t.Errorf("run-job %q surface: schedule…-install present=%v, want %v:\n%s", tc.surface, hasInstall, tc.wantInstall, got)
		}
		if tc.surface == "" { // the sanctioned fallback: -install after -remove, job file named
			ri, ii := strings.Index(got, "-remove"), strings.Index(got, "-install")
			if ri < 0 || ii < 0 || ii < ri {
				t.Errorf("CLI run-job fallback: `-install` must appear only after `-remove`:\n%s", got)
			}
			if !strings.Contains(got, runjobEv.JobPath) {
				t.Errorf("CLI run-job fallback does not name the job file:\n%s", got)
			}
			if !strings.Contains(got, "-name mailarchive-1234abcd -remove") {
				t.Errorf("CLI run-job fallback: entry name not recovered from the job basename:\n%s", got)
			}
		}
	}

	// capture: the card on every surface; never a `schedule desktop -capture -install`.
	capture := []string{"desktop", "-capture", "-out", "/a"}
	for _, surface := range []string{"", "desktop", "gui"} {
		got := repairHow(base(ev(schedule.KindCapture), capture, surface))
		if !strings.Contains(got, "Scheduled backup card") {
			t.Errorf("capture %q surface: %q does not name the card", surface, got)
		}
		if strings.Contains(got, "-install") {
			t.Errorf("capture %q surface offers a `-install`:\n%s", surface, got)
		}
	}

	// export: the command line on every surface; the one place scheduleCommand appears.
	export := []string{"-out", "/a", "-auto"}
	for _, surface := range []string{"", "desktop", "gui"} {
		got := repairHow(base(ev(), export, surface))
		if !strings.Contains(got, "mailarchive schedule -out /a -auto -install") {
			t.Errorf("export %q surface: %q is not the schedule…-install command", surface, got)
		}
	}

	// no single repair (both kinds corroborated, or an uncorroborated run-job):
	// both explicit installs — the wizard AND a command-line `-install`.
	for _, kindEv := range []schedule.KindEvidence{ev(schedule.KindRunJob, schedule.KindCapture), ev()} {
		got := repairHow(base(kindEv, runjob, ""))
		if !strings.Contains(got, "wizard") || !strings.Contains(got, "mailarchive schedule -out /a -install") {
			t.Errorf("no-single-repair route must name both installs:\n%s", got)
		}
	}
}

// covers: MA-633, R14, R18, S59
// The four backup remedies render the repair through the route: a corroborated
// run-job whose program moved is never told to run a `schedule … -install`
// (MA-631 would refuse it) except inside the sanctioned no-display fallback, and
// a capture schedule whose archive moved never emits a forbidden
// `schedule desktop -capture … -install`.
func TestMA633_RemediesRouteThroughRepairHow(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	// exe_moved for a corroborated run-job (CLI surface).
	in := healthyInput(now)
	in.GOOS = "linux"
	in.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: "/j/mailarchive-1234abcd.json"}
	in.Desc.Job = []string{"run-job", "-job", "x"}
	in.ExeIsThis = false
	r := joinReasons(Assess(in, now))
	if !strings.Contains(r, "not this binary") {
		t.Fatalf("expected the exe_moved WARN:\n%s", r)
	}
	if strings.Contains(r, "-install") {
		ri, ii := strings.Index(r, "-remove"), strings.Index(r, "-install")
		if ri < 0 || ii < ri {
			t.Errorf("run-job exe_moved offers a `-install` that is not the post-`-remove` fallback:\n%s", r)
		}
	}

	// capture moved_archive must route to the card, never a `-capture … -install`.
	if filepath.IsAbs("/elsewhere") {
		mv := healthyInput(now)
		mv.GOOS = "linux"
		mv.Out = "/a"
		mv.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindCapture: true}}
		mv.Desc.Name = "mailarchive-x"
		mv.Desc.Job = []string{"desktop", "-capture", "-out", "/elsewhere"}
		cr := joinReasons(Assess(mv, now))
		if !strings.Contains(cr, "not this archive") {
			t.Fatalf("expected the moved_archive WARN:\n%s", cr)
		}
		if strings.Contains(cr, "-capture") && strings.Contains(cr, "-install") {
			t.Errorf("capture moved_archive emits a forbidden `schedule desktop -capture … -install`:\n%s", cr)
		}
		if !strings.Contains(cr, "Scheduled backup card") {
			t.Errorf("capture moved_archive does not route to the card:\n%s", cr)
		}
	}
}
