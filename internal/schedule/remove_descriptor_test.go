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
// together with the descriptor. A descriptor that cannot be read at all (bad
// JSON, as a hand edit leaves it) records no one's schedule, so it goes with the
// archive's DEFAULT-named schedule — the entry `-out X -remove` falls back to
// when it cannot read the descriptor, and the only one that writes it — and
// stays, byte for byte, when another entry of the archive (its verify) is
// removed. The scheduler is sandboxed: HOME is a temp dir and a fake launchctl
// is first on PATH for launchd, and the crontab seams stand in on cron hosts.
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

	t.Run("unreadable descriptor", func(t *testing.T) {
		out := t.TempDir()
		exe := "/usr/local/bin/mailarchive"
		backup := Spec{Name: DefaultNameFor(out), Interval: Daily, At: "02:00", Exe: exe, Args: []string{"-out", out}, Out: out}
		verify := Spec{Name: DefaultNameFor(out) + "-verify", Interval: Weekly, At: "05:00", Exe: exe, Args: []string{"verify", "-out", out}, Out: out}
		for _, s := range []Spec{backup, verify} {
			if err := Install(s); err != nil { // the sandboxed launchctl / crontab seams
				t.Fatal(err)
			}
			assure.Reached(t, Query(s.Name) == Installed, "fixture: "+s.Name+" installed")
		}
		path := filepath.Join(out, DescriptorName)
		broken := []byte("{\"version\":1,\"name\":") // a hand edit cut short
		if err := os.WriteFile(path, broken, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadDescriptor(out); err == nil || os.IsNotExist(err) {
			t.Fatalf("fixture: the descriptor should exist and be unreadable, got %v", err)
		}

		// Another entry of the archive: removed, the unreadable descriptor kept.
		removed, err := RemoveIfInstalled(verify)
		if err != nil {
			t.Fatal(err)
		}
		if !removed || Query(verify.Name) == Installed {
			t.Errorf("the archive's verify entry was not removed (removed=%v, state=%v)", removed, Query(verify.Name))
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
			t.Errorf("removing the verify entry changed or deleted the archive's descriptor (%v)", err)
		}

		// The archive's default-named schedule: removed together with it.
		removed, err = RemoveIfInstalled(backup)
		if err != nil {
			t.Fatal(err)
		}
		assure.Reached(t, removed, "removal of the archive's default-named schedule")
		if Query(backup.Name) == Installed {
			t.Errorf("the default-named schedule %s is still installed", backup.Name)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("an unreadable descriptor outlived the archive's default-named schedule, so status and -install still point at a schedule that is gone (stat: %v)", err)
		}
	})
}
