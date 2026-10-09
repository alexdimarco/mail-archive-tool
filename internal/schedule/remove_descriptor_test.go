package schedule

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-455, R14, R18, S14
// The archive's descriptor is evidence that ITS schedule is installed, not that
// any schedule is: RemoveIfInstalled for a name the descriptor does not record,
// and that the host scheduler does not have either, removes nothing, reports
// removed=false, never touches the scheduler, and leaves the descriptor —
// which still names the archive's real, running schedule for `status` — exactly
// as it was. The positive twin is the descriptor's own name, which is removed
// together with the descriptor. The scheduler is sandboxed: HOME is a temp dir
// and a fake launchctl is first on PATH for launchd, and the crontab seams
// stand in on cron hosts.
func TestRemoveIfInstalledKeepsAnotherSchedulesDescriptor(t *testing.T) {
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd":
	default:
		t.Skip("the scheduler is sandboxed for launchd (HOME + a fake launchctl) and cron (the crontab seams) only")
	}
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	if p, err := exec.LookPath("launchctl"); err != nil || filepath.Dir(p) != bin {
		t.Fatalf("launchctl resolves to %q (%v), not the sandbox fake", p, err)
	}
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	crontab, wrote := "", false
	crontabList = func() (string, error) { return crontab, nil }
	crontabInstall = func(c string) error { crontab, wrote = c, true; return nil }

	out := t.TempDir()
	real := Spec{Name: "mailarchive-real0001", Interval: Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive", Args: []string{"-out", out}, Out: out}
	if err := WriteDescriptor(out, DescribeSpec(real)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(out, DescriptorName))
	if err != nil {
		t.Fatal(err)
	}

	typo := real
	typo.Name = "typo-name"
	removed, err := RemoveIfInstalled(typo)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("reported removing a schedule name that was never installed")
	}
	if wrote {
		t.Error("a no-op remove rewrote the crontab")
	}
	after, err := os.ReadFile(filepath.Join(out, DescriptorName))
	if err != nil || string(after) != string(before) {
		t.Errorf("the descriptor of the archive's real schedule was changed or deleted (%v)", err)
	}

	// Positive twin: the descriptor's own schedule is removed with it.
	removed, err = RemoveIfInstalled(real)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, removed, "removal of the recorded schedule")
	if _, err := ReadDescriptor(out); !os.IsNotExist(err) {
		t.Errorf("the recorded schedule's descriptor survived its removal: %v", err)
	}
}
