package desktop

import (
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
}

// covers: MA-583
// The Desktop local-schedule card shows the create/edit form, the pre-sync checkbox
// and a VISIBLE notice (not hover-only, X16) for a local Thunderbird archive the
// Desktop can reconstruct — and NOT the "backup off" message.
func TestLocalScheduleCardUI(t *testing.T) {
	out := tbProfileArchive(t)
	h := DashboardHandler(Config{Out: out, GOOS: "darwin", SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	page, _ := getPage(t, h)
	for _, want := range []string{`id="schedInterval"`, `id="schedPresync"`, "Sync Thunderbird before each backup", "waits for its mail to finish downloading"} {
		if !strings.Contains(page, want) {
			t.Errorf("local-schedule card is missing %q", want)
		}
	}
	if strings.Contains(page, `id="backupOff"`) {
		t.Error("a reconstructable local archive must not show the backup-off message")
	}
}
