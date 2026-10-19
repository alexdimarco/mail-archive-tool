package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-617, R14, R18, S59
// §4.6.3: a repair keeps the schedule's KIND. The Weekly backup card's
// Install/Repair takes the spec from the §4.6.5 CORROBORATED kind, never the
// request: a corroborated run-job is rebuilt through wizard.RepairSpec (Args
// ["-job", <job file>], from the name/out/program, never the descriptor), not
// converted to a capture; a capture install/repair refuses with HeadlessCapture's
// not-configured text BEFORE schedule.Install when Microsoft 365 is not set up;
// and the card's Remove of a run-job also deletes the archive's job file. The
// command line's schedule -install guard for a run-job/capture descriptor is
// MA-631 (internal/schedule), unchanged here.
func TestMA617_RepairKeepsScheduleKind(t *testing.T) {
	// job.Dir() uses os.UserConfigDir(); redirect it (and the default graph-config
	// path CaptureCorroborates reads) into a tempdir, so a planted job file is the
	// only owner-side evidence and no capture is corroborated here.
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

	// --- run-job: the card rebuilds a run-job, never a capture ---
	runjobOut := t.TempDir()
	// the corroborating job file, and a matching run-job descriptor.
	jp := filepath.Join(jobsDir, schedule.DefaultNameFor(runjobOut)+".json")
	jb, _ := json.Marshal(map[string]any{"version": 1, "out": runjobOut})
	if err := os.WriteFile(jp, jb, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(jp, 0o600)
	rjSpec := schedule.Spec{Name: schedule.DefaultNameFor(runjobOut), Interval: schedule.Weekly, At: "03:00",
		Exe: "/usr/local/bin/mailarchive", Args: []string{"-job", jp}, Out: runjobOut}
	if err := schedule.WriteDescriptor(runjobOut, schedule.DescribeSpec(rjSpec)); err != nil {
		t.Fatal(err)
	}

	var installed schedule.Spec
	var wasInstalled bool
	cfg := Config{Out: runjobOut, SettingsPath: filepath.Join(home, "s.json"),
		InstallBackup: func(s schedule.Spec) error { installed, wasInstalled = s, true; return nil }}
	h := DashboardHandler(cfg)
	_, csrf := getPage(t, h)
	r := postAction(h, "/api/schedule-install", csrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	body := assure.Reached(t, r.Body.String(), "run-job repair response")
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("run-job repair did not succeed: %s", body)
	}
	if !wasInstalled || len(installed.Args) == 0 || installed.Args[0] != "-job" {
		t.Errorf("the card converted a run-job instead of keeping its kind: installed Args = %v", installed.Args)
	}
	if strings.Contains(strings.Join(installed.Args, " "), "-capture") {
		t.Errorf("the card installed a capture for a run-job archive: %v", installed.Args)
	}

	// --- capture, Microsoft 365 configured: a capture is installed (positive twin) ---
	capOut := t.TempDir()
	cfgPath := filepath.Join(home, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "app"}); err != nil {
		t.Fatal(err)
	}
	var capSpec schedule.Spec
	var capInstalled bool
	capCfg := Config{Out: capOut, ConfigPath: cfgPath, SettingsPath: filepath.Join(home, "cs.json"),
		InstallBackup: func(s schedule.Spec) error { capSpec, capInstalled = s, true; return nil }}
	hc := DashboardHandler(capCfg)
	_, csrf2 := getPage(t, hc)
	r = postAction(hc, "/api/schedule-install", csrf2, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if b := r.Body.String(); !strings.Contains(b, `"ok":true`) {
		t.Fatalf("capture install (M365 set up) did not succeed: %s", b)
	}
	if !capInstalled || !strings.Contains(strings.Join(capSpec.Args, " "), "-capture") {
		t.Errorf("a capture archive did not install a capture: %v", capSpec.Args)
	}

	// --- capture, Microsoft 365 NOT configured: refused before install ---
	var refusedInstalled bool
	noCfg := Config{Out: t.TempDir(), SettingsPath: filepath.Join(home, "ns.json"),
		InstallBackup: func(s schedule.Spec) error { refusedInstalled = true; return nil }}
	hn := DashboardHandler(noCfg)
	_, csrf3 := getPage(t, hn)
	body = postAction(hn, "/api/schedule-install", csrf3, "127.0.0.1:8097", "http://127.0.0.1:8097").Body.String()
	// Map the JSON response to a typed code (0 ok, 2 refused) so the whole refusal
	// contract — named remediation, no crash text, no install side effect — is one
	// assertion. The positive capture twin above is the "healthy fixture first".
	rc := 0
	if strings.Contains(body, `"ok":false`) {
		rc = 2
	}
	assure.Refused(t, rc, body,
		assure.Names("not configured", "Microsoft 365", "mailarchive setup"),
		assure.NoSideEffect(func() bool { return !refusedInstalled }))

	// --- Remove of a run-job also deletes the archive's job file ---
	rmCfg := Config{Out: runjobOut, SettingsPath: filepath.Join(home, "rs.json"),
		RemoveBackupFor: func(string) error { return nil }, BackupInstalledFor: func(string) bool { return true }}
	hr := DashboardHandler(rmCfg)
	_, csrf4 := getPage(t, hr)
	if _, err := os.Stat(jp); err != nil {
		t.Fatalf("fixture: the run-job's job file should exist before Remove: %v", err)
	}
	r = postAction(hr, "/api/schedule-remove", csrf4, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if b := r.Body.String(); !strings.Contains(b, `"ok":true`) {
		t.Fatalf("run-job remove did not succeed: %s", b)
	}
	if _, err := os.Stat(jp); !os.IsNotExist(err) {
		t.Errorf("Remove of a run-job did not delete its job file %s (err=%v)", jp, err)
	}
}
