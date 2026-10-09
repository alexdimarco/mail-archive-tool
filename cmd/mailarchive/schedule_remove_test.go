package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// sandboxScheduler points HOME at a temp dir (launchd plists land under it) and
// puts fake `launchctl` and `crontab` first on PATH, so `schedule -install` and
// `-remove` run end to end through the real binary without touching the
// developer's launchd or crontab. The fakes log their argv and keep the
// crontab in a file. The test process gets the same environment, so
// schedule.Query here sees what the binary installed.
func sandboxScheduler(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake scheduler binaries are POSIX sh scripts; the -remove name resolution under test is OS-independent")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh for the fake scheduler binaries")
	}
	home, bin := t.TempDir(), t.TempDir()
	fakes := map[string]string{
		"launchctl": "#!/bin/sh\necho \"$@\" >> \"$FAKE_SCHED_LOG\"\nexit 0\n",
		"crontab": "#!/bin/sh\ncase \"$1\" in\n" +
			"-l) [ -f \"$FAKE_CRONTAB\" ] || { echo \"no crontab for $USER\" >&2; exit 1; }; cat \"$FAKE_CRONTAB\" ;;\n" +
			"-) cat > \"$FAKE_CRONTAB\" ;;\n*) exit 2 ;;\nesac\n",
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv("FAKE_SCHED_LOG", filepath.Join(home, "launchctl.log"))
	t.Setenv("FAKE_CRONTAB", filepath.Join(home, "crontab"))
	// Never fall through to the real scheduler: both names must resolve to the fakes.
	for name := range fakes {
		if p, err := exec.LookPath(name); err != nil || filepath.Dir(p) != bin {
			t.Fatalf("%s resolves to %q (%v), not the sandbox fake in %s", name, p, err, bin)
		}
	}
}

// covers: MA-454, MA-455, R14, R18, S14, S28
// `-remove` reverses exactly the entry it names and leaves every other one
// alone. An archive with a nightly backup and a weekly verify (two entries; the
// archive's descriptor names the backup): `-remove -- verify -out X` — the
// install command with -install swapped for -remove — removes the VERIFY entry
// and keeps the backup and its descriptor; `-name <verify> -out X -remove` does
// the same; `-name <never installed> -out X -remove` removes nothing, says so,
// and keeps the descriptor; and the plain `-out X -remove` (the positive twin)
// still removes the backup and its descriptor.
func TestScheduleRemoveTargetsTheNamedEntry(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	backup := schedule.DefaultNameFor(out)
	verify := backup + "-verify"

	install := func() {
		t.Helper()
		for _, args := range [][]string{
			{"schedule", "-install", "--", "-out", out, "-auto"},
			{"schedule", "-interval", "weekly", "-at", "05:00", "-install", "--", "verify", "-out", out},
		} {
			if code, _, stderr := runCLIOut(t, args...); code != 0 {
				t.Fatalf("%v exited %d: %s", args, code, stderr)
			}
		}
		if schedule.Query(backup) != schedule.Installed || schedule.Query(verify) != schedule.Installed {
			t.Fatalf("fixture: backup=%v verify=%v, want both installed", schedule.Query(backup), schedule.Query(verify))
		}
	}
	descriptorName := func() string {
		d, err := schedule.ReadDescriptor(out)
		if err != nil {
			return ""
		}
		return d.Name
	}
	expect := func(step string, wantBackup, wantVerify bool, wantDesc string) {
		t.Helper()
		if got := schedule.Query(backup) == schedule.Installed; got != wantBackup {
			t.Errorf("%s: backup entry installed=%v, want %v", step, got, wantBackup)
		}
		if got := schedule.Query(verify) == schedule.Installed; got != wantVerify {
			t.Errorf("%s: verify entry installed=%v, want %v", step, got, wantVerify)
		}
		if got := descriptorName(); got != wantDesc {
			t.Errorf("%s: descriptor names %q, want %q", step, got, wantDesc)
		}
	}

	install()
	assure.Reached(t, descriptorName(), "the backup's descriptor")
	code, stdout, stderr := runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", out)
	if code != 0 || !strings.Contains(stdout, verify) {
		t.Errorf("remove -- verify: exit %d, want it to name %q:\n%s%s", code, verify, stdout, stderr)
	}
	expect("remove -- verify -out X", true, false, backup)

	install()
	code, stdout, stderr = runCLIOut(t, "schedule", "-name", verify, "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, verify) {
		t.Errorf("remove -name verify: exit %d, want it to name %q:\n%s%s", code, verify, stdout, stderr)
	}
	expect("remove -name <verify> -out X", true, false, backup)

	install()
	code, stdout, stderr = runCLIOut(t, "schedule", "-name", "typo-name", "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, "nothing to remove") || !strings.Contains(stdout, "typo-name") || strings.Contains(stdout, "Removed") {
		t.Errorf("remove -name typo: exit %d, want a 'nothing to remove' naming typo-name:\n%s%s", code, stdout, stderr)
	}
	expect("remove -name <never installed> -out X", true, true, backup)

	// Positive twin: the archive's own backup is removed with its descriptor.
	code, stdout, stderr = runCLIOut(t, "schedule", "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, backup) {
		t.Errorf("remove -out: exit %d, want it to name %q:\n%s%s", code, backup, stdout, stderr)
	}
	expect("remove -out X", false, true, "")
}
