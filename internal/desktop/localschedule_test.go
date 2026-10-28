package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-581
// reconstructLocalJob rebuilds a schedulable job from a LOCAL archive's own last-run
// record (secure read + strict whitelist mapper), PINS -out to the archive, and
// refuses an undeterminable record or a vanished input. underPath excludes an input
// inside <out>/_outlook-pst (an Outlook-app archive's deleted scratch PSTs).
func TestReconstructLocalJob(t *testing.T) {
	out := tbProfileArchive(t) // writes a last-run with -out <out> -mode incremental -input <store>
	j, ok := reconstructLocalJob(out)
	if !ok {
		t.Fatal("a Thunderbird archive's record should reconstruct")
	}
	if j.Out != out {
		t.Errorf("reconstructLocalJob must pin -out to the archive (%q), got %q", out, j.Out)
	}
	if j.ThunderbirdSync != nil {
		t.Error("reconstruct must not set the toggle; the handler adds it")
	}
	if len(j.Inputs) == 0 {
		t.Fatal("expected the reconstructed job to carry the archived input")
	}

	// A vanished input refuses (never schedule a broken job).
	for _, in := range j.Inputs {
		os.RemoveAll(in)
	}
	if _, ok := reconstructLocalJob(out); ok {
		t.Error("a vanished input must refuse reconstruction")
	}

	// No record at all refuses.
	if _, ok := reconstructLocalJob(filepath.Join(t.TempDir(), "nope")); ok {
		t.Error("no last-run record must refuse")
	}
}

// covers: MA-581
// underPath catches an input inside <out>/_outlook-pst but not a mere name prefix.
func TestUnderPath(t *testing.T) {
	scratch := filepath.Join("/a", "_outlook-pst")
	if !underPath(scratch, filepath.Join(scratch, "x.pst")) {
		t.Error("an input inside _outlook-pst should be caught")
	}
	if !underPath(scratch, scratch) {
		t.Error("the directory itself should be caught")
	}
	if underPath(scratch, filepath.Join("/a", "_outlook-pst-sibling")) {
		t.Error("a name-prefixed sibling must not be caught")
	}
}

// covers: MA-581
// The Desktop local-schedule install creates (and idempotently edits) a RUN-JOB for
// a local Thunderbird archive — never a capture — carrying the pre-sync toggle, and
// refuses (installing nothing) when the archive's record is undeterminable. Positive
// twin first (assure discipline).
func TestScheduleInstallLocal(t *testing.T) {
	out := tbProfileArchive(t) // local TB archive with a reconstructable last-run record
	var installed schedule.Spec
	var wasInstalled bool
	cfg := Config{Out: out, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: filepath.Join(t.TempDir(), "s.json"),
		InstallBackup: func(s schedule.Spec) error { installed, wasInstalled = s, true; return nil }}
	h := DashboardHandler(cfg)
	_, csrf := getPage(t, h)

	// Positive twin: presync on -> a run-job (Args -job <path>), never a capture.
	body := assure.Reached(t, postJSON(h, "/api/schedule-install", csrf, `{"interval":"weekly","time":"03:00","presync":true}`).Body.String(), "local install response")
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("local install did not succeed: %s", body)
	}
	if !wasInstalled || len(installed.Args) < 2 || installed.Args[0] != "-job" {
		t.Fatalf("expected a run-job install (Args -job <path>), got %v", installed.Args)
	}
	if strings.Contains(strings.Join(installed.Args, " "), "-capture") {
		t.Errorf("a local archive must not install a capture: %v", installed.Args)
	}
	j, err := job.Read(installed.Args[1])
	if err != nil {
		t.Fatal(err)
	}
	if j.ThunderbirdSync == nil {
		t.Error("presync:true must write job.ThunderbirdSync")
	}
	if j.Out != out {
		t.Errorf("the scheduled job's -out must be this archive (%q), got %q", out, j.Out)
	}

	// Edit: presync off rewrites the job without the toggle (idempotent re-create).
	if b := postJSON(h, "/api/schedule-install", csrf, `{"interval":"daily","time":"02:00","presync":false}`).Body.String(); !strings.Contains(b, `"ok":true`) {
		t.Fatalf("local edit did not succeed: %s", b)
	}
	if j2, _ := job.Read(installed.Args[1]); j2.ThunderbirdSync != nil {
		t.Error("presync:false must rewrite the job without the toggle")
	}

	// Refusal twin: an undeterminable record (no last-run) -> named refusal, nothing installed.
	if err := os.Remove(filepath.Join(out, state.LastRunName)); err != nil {
		t.Fatal(err)
	}
	var refusedInstall bool
	rcfg := Config{Out: out, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: filepath.Join(t.TempDir(), "r.json"),
		InstallBackup: func(s schedule.Spec) error { refusedInstall = true; return nil }}
	hr := DashboardHandler(rcfg)
	_, csrf2 := getPage(t, hr)
	rbody := postJSON(hr, "/api/schedule-install", csrf2, `{"interval":"weekly","time":"03:00"}`).Body.String()
	rc := 0
	if strings.Contains(rbody, `"ok":false`) {
		rc = 2
	}
	assure.Refused(t, rc, rbody,
		assure.Names("tell how this archive was built", "wizard"),
		assure.NoSideEffect(func() bool { return !refusedInstall }))

	// A3: a REAL install failure (not the nil seam) leaves NO job file, because the
	// install runs BEFORE the write — so a refusal can never strand a decoy job file.
	failOut := tbProfileArchive(t)
	failCfg := Config{Out: failOut, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: filepath.Join(t.TempDir(), "f.json"),
		InstallBackup: func(s schedule.Spec) error { return errors.New("scheduler refused") }}
	hf := DashboardHandler(failCfg)
	_, csrfF := getPage(t, hf)
	if b := postJSON(hf, "/api/schedule-install", csrfF, `{"interval":"weekly","time":"03:00"}`).Body.String(); !strings.Contains(b, `"ok":false`) {
		t.Fatalf("a failed install should report ok:false: %s", b)
	}
	jp, _ := job.PathFor(schedule.DefaultNameFor(failOut))
	if _, err := os.Stat(jp); err == nil {
		t.Error("A3: a failed install must leave NO job file (install runs before the write)")
	}
}

// covers: MA-581
// A Repair (the button posts an empty body: no interval/time/presync, because those
// inputs are not on a scheduled local card — MA-691) must PRESERVE the installed cadence
// and pre-sync, not silently reset them to Weekly/03:00/off (friction HIGH-3). The server
// reads the current descriptor's cadence and the current job's pre-sync and keeps them, so
// the "Nothing in the archive changes" Repair really changes nothing.
func TestScheduleRepairLocalPreservesCadenceAndPresync(t *testing.T) {
	out := tbProfileArchive(t)
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an installed Daily 05:00 run-job that syncs Thunderbird.
	jj, ok := reconstructLocalJob(out)
	if !ok {
		t.Fatal("fixture: expected a reconstructable archive")
	}
	jj.Name = name
	jj.ThunderbirdSync = &job.ThunderbirdSync{}
	if err := job.Write(jp, jj); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{
		Name: name, Interval: "daily", At: "05:00", Job: []string{"run-job", jp},
	}); err != nil {
		t.Fatal(err)
	}

	var installed schedule.Spec
	cfg := Config{Out: out, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: filepath.Join(t.TempDir(), "s.json"),
		InstallBackup: func(s schedule.Spec) error { installed = s; return nil }}
	h := DashboardHandler(cfg)
	_, csrf := getPage(t, h)

	// Repair: the button posts an empty body.
	body := assure.Reached(t, postJSON(h, "/api/schedule-install", csrf, `{}`).Body.String(), "local repair response")
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("local repair did not succeed: %s", body)
	}
	if installed.Interval != schedule.Daily || installed.At != "05:00" {
		t.Errorf("repair reset the cadence: got %q at %q, want daily at 05:00", installed.Interval, installed.At)
	}
	j2, err := job.Read(jp)
	if err != nil {
		t.Fatal(err)
	}
	if j2.ThunderbirdSync == nil {
		t.Error("repair dropped the pre-sync: the re-written job must keep ThunderbirdSync")
	}
}

// covers: MA-583
// A local Thunderbird archive WITH a corroborated run-job schedule shows the SAME
// cadence/time/pre-sync form as the create case, PREFILLED from the installed schedule
// (ShowLocalEdit) — so the owner can change the cadence, the time, or the Thunderbird-sync
// toggle in place, not only by removing and re-creating the schedule (friction MED-5 + the
// "editable after setup" ruling). Amended 2026-10-28 (design-desktop-settings-card slice A5,
// OR-3, owner-accepted; nothing weakened): the form is on the Settings card, saved with Save
// settings (Settings is the one editor, ruling 1); the Scheduled backup card keeps its
// empty-posting Repair beside Run now and Remove, and points at Settings. (The non-editable
// DOM shape for a card the Desktop can NOT reconstruct is still pinned by TestMA691...; that
// card shows no inputs.)
func TestLocalScheduleCardEditableAfterInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	jobsDir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	out := tbProfileArchive(t) // reconstructable + Thunderbird-sourced
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	// Plant the corroborating job file (syncs Thunderbird) + a Daily 05:00 run-job descriptor.
	jj, ok := reconstructLocalJob(out)
	if !ok {
		t.Fatal("fixture: expected a reconstructable archive")
	}
	jj.Name = name
	jj.ThunderbirdSync = &job.ThunderbirdSync{}
	if err := job.Write(jp, jj); err != nil {
		t.Fatal(err)
	}
	rjSpec := schedule.Spec{Name: name, Interval: schedule.Daily, At: "05:00",
		Exe: "/usr/local/bin/mailarchive", Args: []string{"-job", jp}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(rjSpec)); err != nil {
		t.Fatal(err)
	}

	page, _ := getPage(t, DashboardHandler(Config{Out: out, GOOS: "darwin", SettingsPath: filepath.Join(home, "s.json")}))

	settings := page[strings.Index(page, `id="sec-settings"`):]
	settings = settings[:strings.Index(settings, `id="saveSettings"`)]
	for _, want := range []string{`id="schedInterval"`, `id="schedTime"`, `id="schedPresync"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("the editable schedule in Settings is missing %q", want)
		}
	}
	for _, want := range []string{`id="schedRemove"`, `id="schedRepair"`, `id="runBackupBtn"`, "Change in Settings"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Scheduled backup card is missing %q", want)
		}
	}
	if strings.Contains(page, `id="schedInstall"`) {
		t.Error("the page still carries the removed Install / update button")
	}
	if !strings.Contains(page, `value="daily" selected`) {
		t.Error("the cadence select is not prefilled to the installed Daily")
	}
	if !strings.Contains(page, `value="05:00"`) {
		t.Error("the time input is not prefilled to the installed 05:00")
	}
	if !strings.Contains(page, `id="schedPresync" checked`) {
		t.Error("the pre-sync checkbox is not checked for a schedule that syncs Thunderbird")
	}
}

// covers: MA-581
// A2: a tampered last-run record cannot aim the scheduled backup at a store that is not
// one of the archive's own recorded sources — an outside -input, or a symlink -input, is
// refused (reconstructLocalJob returns ok=false, so no schedule is offered or installed).
func TestReconstructLocalJobRefusesTamperedInput(t *testing.T) {
	out := tbProfileArchive(t)
	real, ok := reconstructLocalJob(out)
	if !ok || len(real.Inputs) == 0 {
		t.Fatal("fixture: expected a reconstructable input")
	}
	tamper := func(jobJSON string) {
		rec := `{"version":1,"status":"ok","job":` + jobJSON + `}`
		if err := os.WriteFile(filepath.Join(out, state.LastRunName), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// An -input NOT among the archive's manifest sources is refused (the exfiltration attack).
	tamper(fmt.Sprintf(`["-out",%q,"-mode","incremental","-input",%q]`, out, t.TempDir()))
	if _, ok := reconstructLocalJob(out); ok {
		t.Error("A2: an -input outside the archive's recorded sources must be refused")
	}

	// A symlink -input is refused even when it points at the real store (no following).
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real.Inputs[0], link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	tamper(fmt.Sprintf(`["-out",%q,"-mode","incremental","-input",%q]`, out, link))
	if _, ok := reconstructLocalJob(out); ok {
		t.Error("A2: a symlink -input must be refused even pointing at the real store")
	}
}

// covers: MA-583
// The Desktop local-schedule card shows the create/edit form, the pre-sync checkbox
// and a VISIBLE notice (not hover-only, X16) for a local Thunderbird archive the
// Desktop can reconstruct — and NOT the "backup off" message.
func TestLocalScheduleCardUI(t *testing.T) {
	out := tbProfileArchive(t)
	h := DashboardHandler(Config{Out: out, GOOS: "darwin", SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	page, _ := getPage(t, h)
	for _, want := range []string{`id="schedInterval"`, `id="schedPresync"`, "Sync Thunderbird before each backup", "waits for its mail to stop arriving"} {
		if !strings.Contains(page, want) {
			t.Errorf("local-schedule card is missing %q", want)
		}
	}
	if strings.Contains(page, `id="backupOff"`) {
		t.Error("a reconstructable local archive must not show the backup-off message")
	}
}
