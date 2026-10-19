package schedule

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// refusal turns an error into assure.Refused's (code, message) pair.
func refusal(err error) (int, string) {
	if err != nil {
		return 1, err.Error()
	}
	return 0, ""
}

// covers: MA-455, MA-457, R14, R18, S14
// G8-AO1, G8-AI1, G8-AI2 (g8 adversarial pass): the entry name a descriptor
// records is untrusted input — a folder handed over can name any entry — so an
// operation that acts on "the archive's entry" (`-out X -remove`, the GUI's
// remove) takes the recorded name only when it binds to THIS archive. The
// positive twins first: an archive's own entry reads under its default name and
// under a custom -name, on cron (the block's -out is the archive), and on
// Windows (the archive's own task side-file exists). Then, sandboxed scheduler (temp
// HOME, fake launchctl, crontab seams): X's descriptor copied into Y, a
// descriptor for Y recording X's derived entry name, a custom name that another
// archive's cron block holds, and a name that is no entry of ours on Windows (a
// root-folder task such as GoogleUpdateTaskMachineCore: no side-file of ours)
// are each refused with the descriptor, the name and the remedy named, and X's
// entry stays installed. A verify descriptor planted with X's backup entry name
// (recorded on "another host", which skips the folder check) neither reads nor
// becomes the "earlier verify" an install names for removal. An archive moved
// on this host keeps its verify visible: a verify recorded for the folder's old
// path, which no longer exists, is the moved archive's own, so the install
// names the entry still scheduled against the old path (K1).
func TestSchedulerEntryNameIsBoundToItsArchive(t *testing.T) {
	sandboxHostScheduler(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	x, y := t.TempDir(), t.TempDir()
	xName := DefaultNameFor(x)
	if err := Install(backupSpecFor(x, xName)); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, Query(xName) == Installed, "fixture: X's backup installed")

	// Positive twins: the archive's own names bind.
	if got, err := RecordedBackupName(x); err != nil || got != xName {
		t.Fatalf("an archive's own default-named backup does not bind: %q, %v", got, err)
	}
	if err := Install(backupSpecFor(y, "nightly-y-g8a")); err != nil {
		t.Fatal(err)
	}
	if got, err := RecordedBackupName(y); err != nil || got != "nightly-y-g8a" {
		t.Fatalf("an archive's own custom-named backup does not bind: %q, %v", got, err)
	}

	// Planted names in Y's backup descriptor.
	yPath := filepath.Join(y, DescriptorName)
	for _, c := range []struct {
		step string
		spec Spec
	}{
		{"X's descriptor copied into Y", backupSpecFor(x, xName)},
		{"a descriptor for Y recording X's derived name", backupSpecFor(y, xName)},
	} {
		if err := WriteDescriptor(y, DescribeSpec(c.spec)); err != nil {
			t.Fatal(err)
		}
		_, err := RecordedBackupName(y)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(yPath, xName, "-name"),
			assure.NoSideEffect(func() bool { return Query(xName) == Installed }))
		_ = c.step
	}

	// Custom names, on the hosts whose state can say whose they are.
	t.Run("cron: another archive's block", func(t *testing.T) {
		crontab := ""
		origList, origInstall := crontabList, crontabInstall
		t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
		crontabList = func() (string, error) { return crontab, nil }
		crontabInstall = func(c string) error { crontab = c; return nil }
		b := t.TempDir()
		if err := installOn(backupSpecFor(b, "nightlyB-g8a"), "linux"); err != nil {
			t.Fatal(err)
		}
		own := t.TempDir()
		if err := installOn(backupSpecFor(own, "nightlyOwn-g8a"), "linux"); err != nil {
			t.Fatal(err)
		}
		if d, err := ReadDescriptor(own); err != nil || nameBoundOn(own, d, "linux") != nil {
			t.Fatalf("an archive's own custom block does not bind: %v", err)
		}
		planted := DescribeSpec(backupSpecFor(own, "nightlyB-g8a"))
		code, msg := refusal(nameBoundOn(own, planted, "linux"))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("nightlyB-g8a", b),
			assure.NoSideEffect(func() bool { return cronBlockOut(crontab, CronMarker("nightlyB-g8a")) == b }))
	})
	t.Run("windows: a task that is not ours", func(t *testing.T) {
		const foreign = "GoogleUpdateTaskMachineCore"
		own := t.TempDir()
		d := DescribeSpec(backupSpecFor(own, foreign))
		code, msg := refusal(nameBoundOn(own, d, "windows"))
		// No side-file (neither XML nor wrapper) for a foreign task, so it is not
		// one this tool installed; the refusal names it and the -name remedy (MA-650).
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(foreign, "-name"))
		// Positive twin: the archive's own custom-named task has its wrapper.
		ownSpec := backupSpecFor(own, "nightly-win-g8a")
		ownSpec.Wrapper, ownSpec.WrapperPath = true, DefaultWrapperPath(ownSpec.Name)
		if err := writeWrapper(ownSpec); err != nil {
			t.Fatal(err)
		}
		if err := nameBoundOn(own, DescribeSpec(ownSpec), "windows"); err != nil {
			t.Errorf("an archive's own custom-named task, wrapper present, does not bind: %v", err)
		}
	})

	// A planted verify descriptor naming X's backup entry.
	t.Run("a planted verify descriptor", func(t *testing.T) {
		if err := RemoveDescriptor(y); err != nil { // the verify descriptor stands alone
			t.Fatal(err)
		}
		planted := DescribeSpec(verifySpecFor(y, xName))
		planted.Host = "elsewhere-g8a"
		if err := WriteVerifyDescriptor(y, planted); err != nil {
			t.Fatal(err)
		}
		if got := SupersededVerify(verifySpecFor(y, "weekly-verify-g8a")); got != "" {
			t.Errorf("an install names another archive's entry %s as this archive's earlier verify", got)
		}
		if got, ok := RecordedVerifyName(y); ok {
			t.Errorf("a verify descriptor naming another archive's backup entry is offered as this archive's verify: %q", got)
		}
		if Query(xName) != Installed {
			t.Errorf("X's entry is gone")
		}
	})

	// K1: the archive is renamed on this host; its verify entry is still scheduled
	// against the old path.
	t.Run("a moved archive's verify", func(t *testing.T) {
		parent := t.TempDir()
		oldDir, newDir := filepath.Join(parent, "old"), filepath.Join(parent, "new")
		if err := os.Mkdir(oldDir, 0o755); err != nil {
			t.Fatal(err)
		}
		oldVerify := DefaultNameFor(oldDir) + "-verify"
		if err := Install(verifySpecFor(oldDir, oldVerify)); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			t.Fatal(err)
		}
		assure.Reached(t, Query(oldVerify) == Installed, "fixture: the verify entry still scheduled against the old path")
		d, err := ReadVerifyDescriptor(newDir)
		if err != nil || d.Name != oldVerify {
			t.Fatalf("the moved archive's verify is hidden: %+v, %v", d, err)
		}
		if got := SupersededVerify(verifySpecFor(newDir, "weekly-verify-moved-g8a")); got != oldVerify {
			t.Errorf("an install in the moved archive does not name the verify still scheduled against the old path: got %q, want %q", got, oldVerify)
		}
	})
}
