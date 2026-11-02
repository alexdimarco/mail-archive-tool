package schedule

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1022, R14
// A computer whose scheduler is cron but which has no crontab program (a container, a
// systemd-timer-only host) has no cron entry, so a removal finds nothing to stop and removes
// nothing, while status's question (Query, MA-78) still says the scheduler is unavailable (the
// 1.5.3 friction review's H1). The real `crontab -l` is run with a PATH that holds no crontab.
// Positive twin first: with a crontab that answers, the entry is found installed and removed,
// the user's own job kept.
func TestNoCrontabProgramMeansNoCronEntryToStop(t *testing.T) {
	name := "mailarchive-1234abcd"
	out := t.TempDir()
	s := backupSpecFor(out, name)

	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	var writes []string
	crontabInstall = func(c string) error { writes = append(writes, c); return nil }

	// Positive twin: a crontab holding the entry and the user's own job.
	block, err := CronBlock(s)
	if err != nil {
		t.Fatal(err)
	}
	crontabList = func() (string, error) { return "0 1 * * * /usr/bin/backup\n" + block + "\n", nil }
	if got := QueryToStopOn(name, "linux"); got != Installed {
		t.Fatalf("an installed entry asked to stop: %v", got)
	}
	if err := RemoveLiveEntryNamedOn(name, "linux"); err != nil || len(writes) != 1 || strings.Contains(writes[0], CronMarker(name)) || !strings.Contains(writes[0], "/usr/bin/backup") {
		t.Fatalf("the removal of an installed entry: %v %q", err, writes)
	}

	// No crontab program: the real `crontab -l` with a PATH that holds none.
	crontabList = origList
	t.Setenv("PATH", t.TempDir())
	if _, err := crontabList(); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("crontab -l with no crontab on PATH: %v", err)
	}
	if got := queryOn(name, "linux"); got != SchedulerUnavailable {
		t.Fatalf("status's question must still say the scheduler is unavailable (MA-78): %v", got)
	}
	if got := QueryToStopOn(name, "linux"); got != NotInstalled {
		t.Fatalf("a removal's question with no crontab program: %v, want not installed", got)
	}
	writes = nil
	if err := RemoveLiveEntryNamedOn(name, "linux"); err != nil || len(writes) != 0 {
		t.Fatalf("removing with no crontab program: %v (writes %q), want nothing to remove", err, writes)
	}
	// An install there cannot work: refused, saying cron is not installed and what to do, the
	// crontab never written.
	code, msg := refusal(installOn(s, "linux"))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("no crontab program", "cron is not installed", "install cron, then try again"),
		assure.NoSideEffect(func() bool {
			_, derr := os.Stat(filepath.Join(out, DescriptorName))
			return len(writes) == 0 && errors.Is(derr, fs.ErrNotExist)
		}))
}

// covers: MA-1022, R14
// A `crontab -l` that fails with no words leaves no stray colon in its error (the friction
// review's item 29 read "…in $PATH:."). Positive twin first: one that fails with words names
// them.
func TestACrontabErrorWithNoWordsEndsCleanly(t *testing.T) {
	origList := crontabList
	t.Cleanup(func() { crontabList = origList })
	crontabList = func() (string, error) { return "crontab: permission denied\n", &exec.ExitError{} }
	if _, err := readCrontabForEdit(); err == nil || !strings.HasSuffix(err.Error(), ": crontab: permission denied") {
		t.Fatalf("with words: %v", err)
	}
	crontabList = func() (string, error) { return "", errFake }
	if _, err := readCrontabForEdit(); err == nil || strings.HasSuffix(err.Error(), ":") || strings.HasSuffix(err.Error(), ": ") {
		t.Fatalf("with no words: %q", err)
	}
}
