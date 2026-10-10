package schedule

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-457, R14, S14
// Install writes a descriptor only after the host scheduler has the entry, so
// an install that fails at the scheduler records nothing: no verify descriptor
// claims a verify that never loaded (status would watch it and -remove would
// take it for one), and no backup descriptor either. Each host's install runs
// against a stand-in that fails its scheduler step: crontab refusing the new
// table (the crontab seams), Task Scheduler refusing /Create (the schtasksRun
// seam) and, where a POSIX shell can stand in for it, launchctl failing `load`
// (a fake first on PATH, HOME a temp dir). No real scheduler is touched. The
// positive twin comes first, through the same stand-in in working order: a
// backup and a verify install and each writes its own descriptor. Then a
// backup and a verify, each into a fresh archive, fail with the scheduler's
// step named, and neither descriptor exists there.
func TestInstallRecordsNothingWhenTheSchedulerFails(t *testing.T) {
	failing := false

	// cron: the crontab seams.
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	crontab := ""
	crontabList = func() (string, error) { return crontab, nil }
	crontabInstall = func(c string) error {
		if failing {
			return errors.New("crontab: installing new crontab failed (simulated)")
		}
		crontab = c
		return nil
	}

	// Task Scheduler: the schtasksRun seam.
	origRun := schtasksRun
	t.Cleanup(func() { schtasksRun = origRun })
	tasks := map[string]bool{}
	schtasksRun = func(args ...string) ([]byte, error) {
		if len(args) < 3 || args[1] != "/TN" {
			return nil, fmt.Errorf("unexpected schtasks call %v", args)
		}
		switch args[0] {
		case "/Create":
			if failing {
				return []byte("ERROR: The task XML is malformed."), &exec.ExitError{}
			}
			tasks[args[2]] = true
			return []byte("SUCCESS: The scheduled task has successfully been created."), nil
		case "/Query":
			if tasks[args[2]] {
				return []byte(args[2]), nil
			}
			return []byte("ERROR: The system cannot find the file specified."), &exec.ExitError{}
		}
		return nil, fmt.Errorf("unexpected schtasks call %v", args)
	}
	wrapDir := t.TempDir() // the Windows wrapper and XML definition land here

	legs := []struct{ goos, step string }{{"linux", "crontab"}, {"windows", "schtasks /Create"}}
	// launchd's stand-in is a POSIX sh script, so its leg runs where one can
	// (launchd itself exists only on macOS); the two legs above pin the same
	// ordering, which is the same code after the host's install, on every host.
	failMarker := ""
	if _, err := os.Stat("/bin/sh"); err == nil && runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
		bin := t.TempDir()
		failMarker = filepath.Join(bin, "fail-load")
		fake := "#!/bin/sh\nif [ \"$1\" = load ] && [ -f " + shellQuote(failMarker) + " ]; then echo 'Load failed: 5: Input/output error' >&2; exit 5; fi\nexit 0\n"
		if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
		if p, err := exec.LookPath("launchctl"); err != nil || filepath.Dir(p) != bin {
			t.Fatalf("launchctl resolves to %q (%v), not the sandbox fake", p, err)
		}
		legs = append(legs, struct{ goos, step string }{"darwin", "launchctl load"})
	}
	setFailing := func(v bool) {
		t.Helper()
		failing = v
		if failMarker == "" {
			return
		}
		if v {
			if err := os.WriteFile(failMarker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Remove(failMarker); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	specs := func(goos, out string) []Spec {
		backup := backupSpecFor(out, DefaultNameFor(out))
		verify := verifySpecFor(out, DefaultNameFor(out)+"-verify")
		if goos == "windows" {
			for _, s := range []*Spec{&backup, &verify} {
				s.Wrapper, s.WrapperPath = true, filepath.Join(wrapDir, s.Name+".cmd")
			}
		}
		return []Spec{backup, verify}
	}
	descriptorOf := func(s Spec) string {
		if isVerifyJob(s) {
			return filepath.Join(s.Out, VerifyDescriptorName)
		}
		return filepath.Join(s.Out, DescriptorName)
	}
	absent := func(p string) bool { _, err := os.Lstat(p); return errors.Is(err, os.ErrNotExist) }

	for _, leg := range legs {
		t.Run(leg.goos, func(t *testing.T) {
			// Positive twin: the stand-in in working order; each install records itself.
			setFailing(false)
			for _, s := range specs(leg.goos, t.TempDir()) {
				if err := installOn(s, leg.goos); err != nil {
					t.Fatalf("installing %s through a working %s: %v", s.Name, leg.step, err)
				}
				assure.Reached(t, !absent(descriptorOf(s)), "the descriptor "+s.Name+"'s install writes")
			}

			// The scheduler step fails: the install fails, and records nothing.
			setFailing(true)
			for _, s := range specs(leg.goos, t.TempDir()) {
				code, msg := 0, ""
				if err := installOn(s, leg.goos); err != nil {
					code, msg = 1, err.Error()
				}
				if !absent(descriptorOf(s)) {
					t.Errorf("%s: an install whose %s failed left %s, recording an entry that never loaded", s.Name, leg.step, descriptorOf(s))
				}
				assure.Refused(t, code, msg, assure.Code(1), assure.Names(leg.step),
					assure.NoSideEffect(func() bool {
						return absent(filepath.Join(s.Out, DescriptorName)) && absent(filepath.Join(s.Out, VerifyDescriptorName))
					}))
			}
			setFailing(false)
		})
	}
}
