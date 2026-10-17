package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-455, R14, R18, S14
// G8-AO1, G8-AI1 (g8 adversarial pass, fresh-eyes re-check): the GUI's Repair
// re-installs a recorded schedule under the name its descriptor records, which a
// folder's writer can plant, so Repair would replace (launchd, cron) or
// overwrite (Task Scheduler /Create /F) ANOTHER archive's entry with this
// archive's job. Repair now installs only a name that binds to this archive
// (schedule.NameBound), the same rule the removal follows. The positive twin
// first: an archive's own record repairs under its name. Then a descriptor in Y
// that records X's entry for X's folder (copied, X still there) is refused with
// the descriptor's name and the remedy, and nothing is installed.
func TestRepairInstallsOnlyAnEntryThatIsThisArchives(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	x, y := t.TempDir(), t.TempDir()
	mk := func(out, name string) schedule.Descriptor {
		return schedule.DescribeSpec(schedule.Spec{Name: name, Interval: schedule.Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive",
			Args: []string{"-out", out, "-auto"}, Out: out})
	}

	own := mk(y, "nightly-y-g8a2gui")
	if spec, err := repairPlan(y, own, "/usr/local/bin/mailarchive"); err != nil || spec.Name != "nightly-y-g8a2gui" {
		t.Fatalf("an archive's own record does not repair under its name: %+v, %v", spec, err)
	}

	planted := mk(x, schedule.DefaultNameFor(x))
	_, err := repairPlan(y, planted, "/usr/local/bin/mailarchive")
	code := 0
	msg := ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(schedule.DefaultNameFor(x), "-name"),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents"))
			return serr != nil || schedule.Query(schedule.DefaultNameFor(x)) == schedule.NotInstalled
		}))
}
