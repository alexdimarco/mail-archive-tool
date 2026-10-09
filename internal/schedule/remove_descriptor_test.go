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
// JSON, as a hand edit leaves it) says nothing about whose it is — Install
// writes it for a backup under any -name — so it is never deleted: it stays,
// byte for byte, when the archive's verify is removed, when its backup is
// removed under the default name (the entry `-out X -remove` falls back to),
// and when a backup installed under a custom -name is removed, and
// UnreadableDescriptor names it each time so the caller can point the
// operator at it. The scheduler is sandboxed: HOME is a temp dir and a fake
// launchctl is first on PATH for launchd, and the crontab seams stand in on
// cron hosts.
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
		for _, leg := range []struct {
			name   string
			backup func(out string) string // the backup's -name
		}{
			{"default -name", DefaultNameFor},
			{"custom -name", func(string) string { return "nightly-mail-ma455" }},
		} {
			t.Run(leg.name, func(t *testing.T) {
				out := t.TempDir()
				exe := "/usr/local/bin/mailarchive"
				backup := Spec{Name: leg.backup(out), Interval: Daily, At: "02:00", Exe: exe, Args: []string{"-out", out}, Out: out}
				verify := Spec{Name: DefaultNameFor(out) + "-verify", Interval: Weekly, At: "05:00", Exe: exe, Args: []string{"verify", "-out", out}, Out: out}
				for _, s := range []Spec{backup, verify} {
					if err := Install(s); err != nil { // the sandboxed launchctl / crontab seams
						t.Fatal(err)
					}
					assure.Reached(t, Query(s.Name) == Installed, "fixture: "+s.Name+" installed")
				}
				// Install recorded the backup whatever its -name.
				if d, err := ReadDescriptor(out); err != nil || d.Name != backup.Name {
					t.Fatalf("fixture: the descriptor should record %s, got %+v (%v)", backup.Name, d, err)
				}
				assure.Reached(t, UnreadableDescriptor(out) == "", "fixture: no unreadable descriptor while it reads")
				path := filepath.Join(out, DescriptorName)
				broken := []byte("{\"version\":1,\"name\":") // a hand edit cut short
				if err := os.WriteFile(path, broken, 0o644); err != nil {
					t.Fatal(err)
				}
				if _, err := ReadDescriptor(out); err == nil || os.IsNotExist(err) {
					t.Fatalf("fixture: the descriptor should exist and be unreadable, got %v", err)
				}
				kept := func(step string) {
					t.Helper()
					if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
						t.Errorf("%s changed or deleted the unreadable descriptor, which records no name -remove can trust (%v)", step, err)
					}
					if got := UnreadableDescriptor(out); got != path {
						t.Errorf("after %s, UnreadableDescriptor = %q, want %s", step, got, path)
					}
				}

				// Another entry of the archive: removed, the unreadable descriptor kept.
				removed, err := RemoveIfInstalled(verify)
				if err != nil {
					t.Fatal(err)
				}
				if !removed || Query(verify.Name) == Installed {
					t.Errorf("the archive's verify entry was not removed (removed=%v, state=%v)", removed, Query(verify.Name))
				}
				kept("removing the verify entry")

				// The archive's backup, under whatever -name it was installed with:
				// removed, and the descriptor still kept.
				removed, err = RemoveIfInstalled(backup)
				if err != nil {
					t.Fatal(err)
				}
				assure.Reached(t, removed, "removal of the archive's backup "+backup.Name)
				if Query(backup.Name) == Installed {
					t.Errorf("the backup %s is still installed", backup.Name)
				}
				kept("removing the backup " + backup.Name)
			})
		}
	})
}
