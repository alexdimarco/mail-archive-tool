package schedule

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// emptyCrontab points the crontab seams at an in-memory, empty crontab and
// returns it, so a cron-host branch never reads the runner's real one.
func emptyCrontab(t *testing.T) *string {
	t.Helper()
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	tab := new(string)
	crontabList = func() (string, error) { return *tab, nil }
	crontabInstall = func(c string) error { *tab = c; return nil }
	return tab
}

// fakeTaskScheduler stands a Task Scheduler in for schtasksRun: /Create and
// /Query by task name (names compared as the real one does, case-insensitively),
// /Delete likewise. deleteErr and queryErr, when set, are what /Delete and
// /Query answer instead, with their output.
type fakeTaskScheduler struct {
	tasks               map[string]bool
	deleteOut, queryOut string
	deleteErr, queryErr error
	calls               [][]string
}

func newFakeTaskScheduler(t *testing.T) *fakeTaskScheduler {
	t.Helper()
	f := &fakeTaskScheduler{tasks: map[string]bool{}}
	orig := schtasksRun
	t.Cleanup(func() { schtasksRun = orig })
	key := strings.ToLower
	schtasksRun = func(args ...string) ([]byte, error) {
		f.calls = append(f.calls, append([]string(nil), args...))
		if len(args) < 3 || args[1] != "/TN" {
			return nil, fmt.Errorf("unexpected schtasks call %v", args)
		}
		name := key(args[2])
		switch args[0] {
		case "/Create":
			f.tasks[name] = true
			return []byte("SUCCESS"), nil
		case "/Query":
			if f.queryErr != nil {
				return []byte(f.queryOut), f.queryErr
			}
			if f.tasks[name] {
				return []byte(args[2]), nil
			}
			return []byte("ERROR: The system cannot find the file specified."), &exec.ExitError{}
		case "/Delete":
			if f.deleteErr != nil {
				return []byte(f.deleteOut), f.deleteErr
			}
			if f.tasks[name] {
				delete(f.tasks, name)
				return []byte("SUCCESS"), nil
			}
			return []byte("ERROR: The system cannot find the file specified."), &exec.ExitError{}
		}
		return nil, fmt.Errorf("unexpected schtasks call %v", args)
	}
	return f
}

// covers: MA-457, R14, S28
// G8-AO3 (g8 adversarial pass): task names, the Windows wrapper and XML file
// names and the launchd plist are case-insensitive on Windows and on default
// macOS volumes, so the name guards compare names there without regard to case:
// a verify named "nightly" beside a backup named "Nightly" would silently
// replace the backup's task, wrapper and XML (the clash the cross-kind refusal
// exists to prevent), and `-name NIGHTLY -remove` must find the descriptor that
// records "Nightly". On cron the two are distinct entries and both install. The
// positive twin first (a verify under a name of its own installs on every OS),
// then, for darwin and windows, the refusals in both directions naming the name,
// the archive and the -name remedy with the archive's records unchanged, and
// the removal under another case clearing the descriptor, through a fake Task
// Scheduler (the schtasksRun seam).
func TestNameGuardComparesCaseWhereNamesAreCaseInsensitive(t *testing.T) {
	emptyCrontab(t)
	out, outV := t.TempDir(), t.TempDir() // one archive with a backup, one with a verify
	if err := WriteDescriptor(out, DescribeSpec(backupSpecFor(out, "Nightly"))); err != nil {
		t.Fatal(err)
	}
	if err := WriteVerifyDescriptor(outV, DescribeSpec(verifySpecFor(outV, "Weekly-Verify"))); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		b, _ := os.ReadFile(filepath.Join(out, DescriptorName))
		v, _ := os.ReadFile(filepath.Join(outV, VerifyDescriptorName))
		return string(b) + "\x00" + string(v)
	}
	before := snapshot()

	for _, goos := range []string{"darwin", "windows", "linux"} {
		if err := refuseCollision(verifySpecFor(out, "other-verify"), goos); err != nil {
			t.Errorf("%s: a verify under a name of its own was refused: %v", goos, err)
		}
	}
	// cron entries are case-sensitive: "nightly" is not "Nightly" there.
	if err := refuseCollision(verifySpecFor(out, "nightly"), "linux"); err != nil {
		t.Errorf("linux: a case variant is a distinct cron entry, but was refused: %v", err)
	}
	if err := refuseCollision(backupSpecFor(outV, "weekly-verify"), "linux"); err != nil {
		t.Errorf("linux: a case variant is a distinct cron entry, but was refused: %v", err)
	}
	for _, goos := range []string{"darwin", "windows"} {
		code, msg := refusal(refuseCollision(verifySpecFor(out, "nightly"), goos))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("nightly", out, "backup schedule", "-name"),
			assure.NoSideEffect(func() bool { return snapshot() == before }))
		code, msg = refusal(refuseCollision(backupSpecFor(outV, "weekly-verify"), goos))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("weekly-verify", outV, "verify schedule", "-name"),
			assure.NoSideEffect(func() bool { return snapshot() == before }))
	}

	// -remove under another case finds the recorded entry and clears its record.
	f := newFakeTaskScheduler(t)
	f.tasks["nightly"] = true
	rm := backupSpecFor(out, "NIGHTLY")
	rm.WrapperPath = filepath.Join(t.TempDir(), "NIGHTLY.cmd")
	if err := removeOn(rm, "windows"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, DescriptorName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("-remove -name NIGHTLY left the descriptor that records Nightly (stat: %v)", err)
	}
}

// covers: MA-457, R14, S28
// G8-AI3 (g8 adversarial pass): a backup descriptor that records a verify (an
// older release wrote it so) is the verify's own record, so a backup install
// that finds it never advises `re-install with -name <the verify's name>` —
// following that advice would replace the verify's entry — and a backup under
// the verify's own name is refused as the cross-kind clash it is. The positive
// twin first: a backup descriptor that records a backup still advises its own
// name. The remedy that is safe, removing the entry first, is still named.
func TestBackupInstallNeverAdvisesTheLegacyVerifysName(t *testing.T) {
	emptyCrontab(t)
	legacy, plain := t.TempDir(), t.TempDir()
	verifyName := DefaultNameFor(legacy) + "-verify"
	if err := WriteDescriptor(legacy, DescribeSpec(verifySpecFor(legacy, verifyName))); err != nil {
		t.Fatal(err)
	}
	if err := WriteDescriptor(plain, DescribeSpec(backupSpecFor(plain, "nightly-ai3"))); err != nil {
		t.Fatal(err)
	}

	err := refuseCollision(backupSpecFor(plain, DefaultNameFor(plain)), "linux")
	if err == nil || !strings.Contains(err.Error(), "re-install with -name nightly-ai3") {
		t.Errorf("a backup descriptor that records a backup no longer advises its own name: %v", err)
	}

	for _, s := range []Spec{backupSpecFor(legacy, DefaultNameFor(legacy)), backupSpecFor(legacy, verifyName)} {
		code, msg := refusal(refuseCollision(s, "linux"))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(verifyName, legacy, "remove it first"),
			assure.Forbid("re-install with -name "+verifyName),
			assure.NoSideEffect(func() bool { return true }))
	}
}

// covers: MA-457, R14, S28
// G8-AC1 and K6 (g8 adversarial pass): a /Delete that fails clears the task's
// files and records only when Task Scheduler no longer has the task — never
// because a failing /Query was read as "not installed". Through a fake Task
// Scheduler (the schtasksRun seam), with the wrapper, the XML and both
// descriptors kept for every error: the positive twin first (a task already
// gone: /Delete and /Query both report it absent, the removal succeeds and
// clears the files and the record); then a /Delete that is denied while /Query
// is denied too (access denied, exit status 1), a /Query that fails with a
// status that names nothing missing, and a Task Scheduler that cannot be run
// or asked at all (no schtasks.exe) each return the /Delete error naming
// `schtasks /Delete` and its output, with the task still registered.
func TestWindowsRemovalKeepsTheDeleteErrorWhenTheTaskMayStillBeThere(t *testing.T) {
	out := t.TempDir()
	backup := backupSpecFor(out, DefaultNameFor(out))
	backup.WrapperPath = filepath.Join(out, backup.Name+".cmd")
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	record := func() {
		t.Helper()
		for _, p := range []string{backup.WrapperPath, SchtasksXMLPath(backup.WrapperPath)} {
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := WriteDescriptor(out, DescribeSpec(backup)); err != nil {
			t.Fatal(err)
		}
	}
	kept := func() bool {
		return exists(backup.WrapperPath) && exists(SchtasksXMLPath(backup.WrapperPath)) && exists(filepath.Join(out, DescriptorName))
	}

	f := newFakeTaskScheduler(t)
	// Positive twin: recorded, but already gone from Task Scheduler.
	record()
	if err := removeOn(backup, "windows"); err != nil {
		t.Fatalf("a task already gone is no error: %v", err)
	}
	if kept() || exists(backup.WrapperPath) || exists(filepath.Join(out, DescriptorName)) {
		t.Errorf("a task already gone left its files or record behind")
	}

	denied := []byte("ERROR: Access is denied.")
	for _, c := range []struct {
		step                string
		deleteErr, queryErr error
		deleteOut, queryOut string
	}{
		{"delete and query both denied", &exec.ExitError{}, &exec.ExitError{}, string(denied), string(denied)},
		{"query fails without saying the task is missing", &exec.ExitError{}, &exec.ExitError{}, string(denied), "ERROR: The Task Scheduler service is not running."},
		{"no schtasks.exe to run or ask", &exec.Error{Name: "schtasks", Err: exec.ErrNotFound}, &exec.Error{Name: "schtasks", Err: exec.ErrNotFound}, "", ""},
	} {
		record()
		f.tasks[strings.ToLower(backup.Name)] = true
		f.deleteErr, f.queryErr, f.deleteOut, f.queryOut = c.deleteErr, c.queryErr, c.deleteOut, c.queryOut
		code, msg := refusal(removeOn(backup, "windows"))
		if c.deleteOut == "" {
			c.deleteOut = "schtasks" // the exec error's own text names the program
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("schtasks /Delete"),
			assure.NoSideEffect(func() bool { return kept() && f.tasks[strings.ToLower(backup.Name)] }))
		if c.deleteOut != "schtasks" && !strings.Contains(msg, c.deleteOut) {
			t.Errorf("%s: the error does not carry /Delete's output %q: %s", c.step, c.deleteOut, msg)
		}
	}
}
