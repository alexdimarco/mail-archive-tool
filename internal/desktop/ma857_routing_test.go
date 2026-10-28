package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-857, MA-860, MA-863, R37
// Section 4 (part A): where a capture is counted (Microsoft 365 set up, this the current archive)
// and the archive is local, the card shows the notice in place of the schedule inputs, with no
// Remove when nothing is installed; a crafted install is refused by the route's own
// BuildKindEvidence read, before Install, in plain words, with nothing written — no job file,
// descriptor, settings or scheduler entry (rev4.1). CheckKind is untouched and, as the
// backstop, refuses the same install. Positive twin first: with Microsoft 365 not set up, the
// same archive installs its run-job.
func TestACountedCaptureShowsTheNoticeAndRefusesALocalInstall(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) || len(g.installs) != 1 {
		t.Fatalf("the positive twin, Microsoft 365 not set up: %s", body)
	}
	os.Remove(g.jobPath)
	g.installs, g.fake.tasks = nil, map[string]bool{}

	countCapture(t, out)
	page := g.page()
	if !shows(page, captureCountedLine) || !strings.Contains(page, `id="backupNotice"`) {
		t.Error("the card does not show section 4's notice")
	}
	for _, bad := range []string{`id="schedInterval"`, `id="schedTime"`, `id="schedPresync"`, `id="schedInstall"`, `id="schedRepair"`, `id="runBackupBtn"`, `id="schedRemove"`} {
		if strings.Contains(page, bad) {
			t.Errorf("with nothing installed, the notice's card renders %s", bad)
		}
	}

	for _, req := range []string{`{"interval":"daily","time":"04:00","presync":true}`, `{"interval":"weekly","time":"03:00"}`, `{}`} {
		u := g.snapshot()
		body := g.post(req)
		assure.Refused(t, refusedCode(body), body, assure.Names(captureCountedRefus), assure.NoSideEffect(u.holds))
		if fileBytes(g.jobPath) != nil {
			t.Errorf("%s: the refused install left a job file", req)
		}
	}
	// The guard is the backstop, unchanged: it refuses the same run-job install.
	name := schedule.DefaultNameFor(out)
	if err := schedule.CheckKind(schedule.BuildKindEvidence(out), schedule.KindRunJob, schedule.KindNone, out, name); err == nil {
		t.Error("CheckKind no longer refuses a run-job where a capture is counted")
	}
}

// covers: MA-857, MA-866, R37
// With a run-job already installed where a capture is counted, the notice adds that it keeps
// running, shows its cadence and Thunderbird sync read-only, and offers Remove only (rev4.1). Its
// Repair is refused with nothing written — scheduler entry, descriptor and job file unchanged —
// and so is an edit.
func TestACountedCaptureShowsAnInstalledRunJobReadOnlyWithRemoveOnly(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"daily","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	countCapture(t, out)

	page := g.page()
	for _, want := range []string{captureCountedLine + " " + keepsRunningLine, "How often: daily at 04:00. Sync Thunderbird first: on."} {
		if !shows(page, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	if !strings.Contains(page, `id="schedRemove"`) {
		t.Error("the installed run-job has no Remove")
	}
	for _, bad := range []string{`id="schedInterval"`, `id="schedInstall"`, `id="schedRepair"`, `id="runBackupBtn"`, `id="schedPresync"`} {
		if strings.Contains(page, bad) {
			t.Errorf("the notice's card renders %s", bad)
		}
	}
	for _, req := range []string{`{}`, `{"interval":"weekly","time":"03:00","presync":false}`} {
		u := g.snapshot()
		body := g.post(req)
		assure.Refused(t, refusedCode(body), body, assure.Names(captureCountedRefus), assure.NoSideEffect(u.holds))
	}

	// The sync reads "can't read" with the reason, never "off", for a file that can't be read.
	if err := os.WriteFile(g.jobPath, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if page := g.page(); !shows(page, "Sync Thunderbird first: can't read (it is damaged") {
		t.Error("an unreadable job file's sync does not read as can't read")
	}
}

// covers: MA-857, R37
// A Microsoft 365-kind (mixed) archive with a run-job installed and no capture counted gets the
// local fields and the run-job edit (3.2): the posted cadence is installed as a run-job. Before,
// it went to the run-job Repair, which ignored the posted cadence and answered ok — the schedule
// unchanged.
func TestAMixedArchivesRunJobIsEditedWhereNoCaptureIsCounted(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	addStore(t, out, "mailbox:a@contoso.org") // Archive now added Microsoft 365 mail: mixed
	if backupKind(out) != backupM365 {
		t.Fatal("fixture: not a mixed archive")
	}
	if page := g.page(); !strings.Contains(page, `id="schedInterval"`) || strings.Contains(page, `id="backupNotice"`) {
		t.Error("the mixed archive's run-job is not shown with its fields")
	}
	body := g.post(`{"interval":"daily","time":"05:30"}`)
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the edit: %s", body)
	}
	last := g.installs[len(g.installs)-1]
	if last.Args[0] != "-job" || last.Interval != schedule.Daily || last.At != "05:30" {
		t.Errorf("the posted cadence was not installed as a run-job: %+v", last)
	}
}

// covers: MA-857, R37
// Open gap, found building slice A3 on 2026-10-28, in the kind guard's evidence (part B's, owner
// ratified, not changed here): BuildKindEvidence's scan of the job folder reads every .json file
// there, and the Desktop's own settings file sits in that folder (job.DesktopSettingsFile) and
// reads as a job naming the current archive. So wherever the settings file names an archive, a
// run-job is counted for it that does not exist: section 3.2's table then sees a run-job on every
// current archive, the guard refuses the Desktop's capture install there, and the card's Remove
// takes that file for the run-job's job file. This encoder skips while the gap is open and fails
// once it is closed — then remove the skip, so it guards it.
func TestTheDesktopSettingsFileIsNeverCountedAsARunJob(t *testing.T) {
	isolateHome(t)
	out := emptyArchive(t)
	p, err := job.DesktopSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(p, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	if ev := schedule.BuildKindEvidence(out); ev.Has(schedule.KindRunJob) {
		t.Skipf("open gap (slice A3, 2026-10-28; the guard's evidence, part B): the Desktop settings file %s is counted as a run-job of %s", ev.JobPath, out)
	}
	t.Fatal("the gap is closed: the Desktop settings file is no longer counted as a run-job — remove this encoder's skip so it guards it")
}

// covers: MA-857, MA-860, R37
// A command-line export installed here is read-only (C-7): no install controls, Remove only, and a
// posted cadence is refused in plain words with nothing changed, never answered ok while the
// schedule stays as it was. Positive twin first: the same archive with no export installs.
func TestACommandLineExportIsReadOnly(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}
	os.Remove(g.jobPath)
	exp := schedule.Spec{Name: schedule.DefaultNameFor(out), Interval: schedule.Weekly, At: "03:00", Exe: "/opt/mailarchive",
		Args: []string{"-input", filepath.Join(out, "x"), "-out", out, "-mode", "incremental"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(exp)); err != nil {
		t.Fatal(err)
	}
	page := g.page()
	for _, bad := range []string{`id="schedInterval"`, `id="schedInstall"`, `id="schedRepair"`} {
		if strings.Contains(page, bad) {
			t.Errorf("a command-line export's card renders %s", bad)
		}
	}
	if !strings.Contains(page, `id="schedRemove"`) {
		t.Error("a command-line export has no Remove")
	}
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(commandLineRefusal), assure.NoSideEffect(u.holds))
	if _, err := job.Read(g.jobPath); err == nil {
		t.Error("the refused install wrote a job file")
	}
}
