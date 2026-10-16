package schedule

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

func tmpFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var tmps []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			tmps = append(tmps, e.Name())
		}
	}
	return tmps
}

// covers: MA-457, R14, S14
// G8-AC2 and G8-AC3 (g8 adversarial pass): an install whose descriptor cannot be
// written must not leave an entry nothing records where that can be foreseen,
// and where it cannot must name the entry and how to remove it; and a failed
// descriptor write leaves no temp file in the archive. The positive twin first
// (a verify installs and records itself). Then a non-empty directory planted
// where the verify descriptor goes (by whoever handed the folder over) is
// refused BEFORE the scheduler is touched, naming the path, the crontab
// unchanged and no temp left, however often it is retried; a custom-named
// backup into a folder that does not exist yet (allowed for export jobs) is
// installed, and its error says the entry is installed, names it and gives the
// `-name NAME -remove` that takes it out; and the writer itself leaves no temp
// file when its rename fails.
func TestDescriptorWriteFailureNamesTheEntryAndLeavesNoTemp(t *testing.T) {
	tab := emptyCrontab(t)

	// Positive twin.
	good := t.TempDir()
	if err := installOn(verifySpecFor(good, DefaultNameFor(good)+"-verify"), "linux"); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, *tab, "fixture: the working install wrote its block")
	if _, err := os.Stat(filepath.Join(good, VerifyDescriptorName)); err != nil {
		t.Fatalf("the working install wrote no verify descriptor: %v", err)
	}
	*tab = ""

	// A planted directory at the descriptor path.
	out := t.TempDir()
	planted := filepath.Join(out, VerifyDescriptorName)
	if err := os.MkdirAll(filepath.Join(planted, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		code, msg := refusal(installOn(verifySpecFor(out, DefaultNameFor(out)+"-verify"), "linux"))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(planted),
			assure.NoSideEffect(func() bool { return *tab == "" && len(tmpFiles(t, out)) == 0 }))
	}
	// The writer itself, through the rename that fails, leaves no temp.
	if err := WriteVerifyDescriptor(out, DescribeSpec(verifySpecFor(out, "v"))); err == nil {
		t.Fatalf("fixture: writing over a non-empty directory should fail")
	}
	if tmps := tmpFiles(t, out); len(tmps) != 0 {
		t.Errorf("a failed descriptor write left temp files in the archive: %v", tmps)
	}

	// A folder that does not exist yet: the entry is installed, and the error says so.
	missing := filepath.Join(t.TempDir(), "not-yet")
	err := installOn(backupSpecFor(missing, "nightly-ac2"), "linux")
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("nightly-ac2", "is installed", "-name nightly-ac2 -remove"),
		assure.NoSideEffect(func() bool { return strings.Contains(*tab, CronMarker("nightly-ac2")) }))
}

// covers: MA-452, MA-457, R14, S28
// G8-AC5 (g8 adversarial pass): `crontab -l` that fails for a reason other than
// "no crontab for user" (permission or spool errors) must not be read as an empty
// crontab, or the install writes back only its own block and the removal an
// empty table, destroying the user's unrelated jobs. Through the crontab seams:
// the positive twin first (no crontab yet: the install writes its block, and a
// crontab holding the user's own job keeps it through an install and a
// removal); then a `crontab -l` that exits with "Permission denied" makes both
// the install and the removal fail naming `crontab -l` and its output, with
// nothing written.
func TestCronEditsNeverRewriteACrontabTheyCouldNotRead(t *testing.T) {
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	var listOut string
	var listErr error
	var writes []string
	crontabList = func() (string, error) { return listOut, listErr }
	crontabInstall = func(c string) error { writes = append(writes, c); return nil }

	out := t.TempDir()
	s := backupSpecFor(out, "nightly-ac5")

	// Positive twin: no crontab yet, then the user's own job survives both edits.
	listOut, listErr = "crontab: no crontab for tester\n", &exec.ExitError{}
	if err := installOn(s, "linux"); err != nil || len(writes) != 1 {
		t.Fatalf("an install with no crontab yet failed (%d writes): %v", len(writes), err)
	}
	listOut, listErr = "MAILTO=me\n0 1 * * * /usr/bin/backup\n", nil
	writes = nil
	if err := removeOn(s, "linux"); err != nil || len(writes) != 1 || !strings.Contains(writes[0], "/usr/bin/backup") {
		t.Fatalf("a removal dropped the user's own job: %v %q", err, writes)
	}

	writes = nil
	listOut, listErr = "crontab: permission denied (spool)\n", &exec.ExitError{}
	for _, step := range []struct {
		name string
		do   func() error
	}{
		{"install", func() error { return installOn(s, "linux") }},
		{"remove", func() error { return removeOn(s, "linux") }},
	} {
		code, msg := refusal(step.do())
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("crontab -l", "permission denied"),
			assure.NoSideEffect(func() bool { return len(writes) == 0 }))
	}
}

// covers: MA-456, MA-457, R14, S28
// G8-AC6 (g8 adversarial pass): on cron hosts the one-archive-per-name guard
// reads the managed block back, and the block says what kind of entry holds the
// name: a verify under the name a backup block holds (or the reverse) is refused
// even when the archive's descriptors are gone (deleted by hand, or never
// written because an earlier install failed), as it is when they are there. The
// positive twin first: the backup re-installs under its own name, and the
// verify under a name of its own beside it. Then, with the backup's descriptor
// removed, the verify under the backup's name is refused naming the name, the
// archive and the -name remedy with the crontab unchanged; likewise a backup
// under a name a verify block holds.
func TestCronNameGuardReadsTheBlocksKindWhenDescriptorsAreGone(t *testing.T) {
	tab := emptyCrontab(t)
	out := t.TempDir()
	backup, verify := backupSpecFor(out, "nightly-ac6"), verifySpecFor(out, "weekly-ac6")
	for _, s := range []Spec{backup, verify, backup} { // the positive twin: side by side, re-install
		if err := installOn(s, "linux"); err != nil {
			t.Fatalf("installing %s: %v", s.Name, err)
		}
	}
	for _, name := range []string{DescriptorName, VerifyDescriptorName} {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	was := *tab
	for _, c := range []struct {
		s    Spec
		want string
	}{
		{verifySpecFor(out, "nightly-ac6"), "backup"},
		{backupSpecFor(out, "weekly-ac6"), "verify"},
	} {
		code, msg := refusal(installOn(c.s, "linux"))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(c.s.Name, out, c.want, "-name"),
			assure.NoSideEffect(func() bool { return *tab == was }))
	}
}

// covers: MA-457, R14, S14
// K2 (g8 completion run's fresh-eyes check): the install's note of an earlier
// verify reads the verify descriptor and, without a readable one, the backup
// descriptor an older release wrote for the verify itself. The legacy branch
// was untested: with only that backup descriptor recording a verify entry that
// is still installed, an install of the verify under a new -name names the old
// entry; the same name, or an entry already gone from the scheduler, names
// nothing (sandboxed scheduler: temp HOME, fake launchctl, crontab seams).
func TestSupersededVerifyReadsAnOlderReleasesBackupDescriptor(t *testing.T) {
	sandboxHostScheduler(t)
	out := t.TempDir()
	const old = "mailarchive-verify" // the name the old README passed
	if err := Install(verifySpecFor(out, old)); err != nil {
		t.Fatal(err)
	}
	// An older release recorded the verify in the backup's descriptor, and no verify descriptor.
	if err := RemoveVerifyDescriptor(out); err != nil {
		t.Fatal(err)
	}
	if err := WriteDescriptor(out, DescribeSpec(verifySpecFor(out, old))); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, Query(old) == Installed, "fixture: the old verify entry is installed")

	if got := SupersededVerify(verifySpecFor(out, DefaultNameFor(out)+"-verify")); got != old {
		t.Errorf("an install under a new name does not name the older release's verify: got %q, want %q", got, old)
	}
	if got := SupersededVerify(verifySpecFor(out, old)); got != "" {
		t.Errorf("a re-install under the same name names %q", got)
	}
	if err := Remove(Spec{Name: old, Out: out, Interval: Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive", WrapperPath: DefaultWrapperPath(old)}); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := WriteDescriptor(out, DescribeSpec(verifySpecFor(out, old))); err != nil {
		t.Fatal(err)
	}
	if got := SupersededVerify(verifySpecFor(out, DefaultNameFor(out)+"-verify")); got != "" {
		t.Errorf("an entry already gone from the scheduler is named: %q", got)
	}
}
