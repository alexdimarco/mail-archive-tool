package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-455, MA-457, R14, R18, S14
// G8-AO1, G8-AI1, G8-AI2 (g8 adversarial pass): an archive's descriptors are
// untrusted input, and the entry name they record decides which scheduler entry
// `-out X -remove` deletes, so it is trusted only as an entry of THIS archive.
// Through the real binary (sandboxed scheduler), the positive twin first: an
// archive's own custom-named backup is removed by `-out Y -remove` through the
// name its descriptor records. Then a descriptor in Y that names another
// archive's live entry — X's descriptor copied over it (its job backs up X), or
// one hand-written for Y that records X's derived entry name — is refused with
// the descriptor, the name and the `-name` remedy named, X's entry still
// installed and Y's descriptor byte-identical; nothing is removed and nothing
// is said to be removed. A verify descriptor planted in Y (recorded on "another
// host", which skips the folder check) that names X's backup entry is not
// offered as the entry to remove: the `-remove -- verify -out Y` hint never
// names X's entry.
func TestScheduleRemoveRefusesADescriptorNameThatIsNotThisArchives(t *testing.T) {
	sandboxScheduler(t)
	x, y := t.TempDir(), t.TempDir()
	xName := schedule.DefaultNameFor(x)
	install := func(args ...string) {
		t.Helper()
		if code, stdout, stderr := runCLIOut(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
	}
	install("schedule", "-install", "--", "-out", x, "-auto")
	assure.Reached(t, schedule.Query(xName) == schedule.Installed, "fixture: X's backup installed")

	// Positive twin: Y's own custom-named backup goes through the name its descriptor records.
	install("schedule", "-name", "nightly-y-g8a", "-install", "--", "-out", y, "-auto")
	code, stdout, stderr := runCLIOut(t, "schedule", "-out", y, "-remove")
	if code != 0 || !strings.Contains(stdout, `Removed scheduled backup "nightly-y-g8a"`) || schedule.Query("nightly-y-g8a") == schedule.Installed {
		t.Fatalf("the archive's own custom-named backup was not removed: exit %d\n%s%s", code, stdout, stderr)
	}

	descPath := filepath.Join(y, schedule.DescriptorName)
	for _, c := range []struct {
		step string
		spec schedule.Spec
	}{
		{"X's descriptor copied into Y", schedule.Spec{Name: xName, Interval: schedule.Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive", Args: []string{"-out", x, "-auto"}, Out: x}},
		{"a descriptor for Y recording X's entry name", schedule.Spec{Name: xName, Interval: schedule.Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive", Args: []string{"-out", y, "-auto"}, Out: y}},
	} {
		if err := schedule.WriteDescriptor(y, schedule.DescribeSpec(c.spec)); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(descPath)
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runCLIOut(t, "schedule", "-out", y, "-remove")
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names(descPath, xName, "-name"), assure.Forbid("Removed scheduled"),
			assure.NoSideEffect(func() bool {
				after, err := os.ReadFile(descPath)
				return err == nil && string(after) == string(before) && schedule.Query(xName) == schedule.Installed
			}))
		if strings.Contains(stdout, "Removed") {
			t.Errorf("%s: -remove claims to have removed an entry:\n%s", c.step, stdout)
		}
	}

	// A verify descriptor planted for Y naming X's backup entry, as another host's.
	planted := schedule.DescribeSpec(schedule.Spec{Name: xName, Interval: schedule.Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", y}, Out: y})
	planted.Host = "elsewhere-g8a"
	if err := schedule.WriteVerifyDescriptor(y, planted); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", y)
	if code != 0 || strings.Contains(stdout+stderr, xName) {
		t.Errorf("the verify removal hint offers another archive's entry %s (exit %d):\n%s%s", xName, code, stdout, stderr)
	}
	if schedule.Query(xName) != schedule.Installed {
		t.Errorf("X's entry was removed")
	}
}
