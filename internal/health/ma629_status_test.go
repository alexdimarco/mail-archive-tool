package health

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-629, R18, S59
// status's Program: line prints one label per identity state in a fixed order
// (§6), and the scheduled: line names the kind owner-side state corroborates (here
// nothing is corroborated, so it falls to the recorded descriptor's own kind).
func TestMA629_ProgramLabelAndScheduledKind(t *testing.T) {
	label := func(goos, exe string, exists, isThis, skew, former, console bool, basis string) string {
		return programLabel(Input{
			GOOS: goos, Desc: schedule.Descriptor{Exe: exe},
			ExeExists: exists, ExeIsThis: isThis, ExeSkew: skew,
			ExeFormerName: former, ExeConsoleBuild: console, ExeBasis: basis,
		})
	}
	cases := []struct {
		name, got, want string
	}{
		{"missing", label("linux", "/opt/mailarchive", false, false, false, false, false, ""), "missing"},
		{"missing former name", label("linux", "/opt/mailarchive-gui", false, false, false, false, false, ""), "missing (mailarchive-gui was a former name of this program, removed by an upgrade)"},
		{"translocated", label("darwin", "/private/var/folders/x/AppTranslocation/d/Mail Archive.app/Contents/MacOS/mailarchive", true, false, false, false, false, ""), "not this program (a temporary copy macOS made of a downloaded app)"},
		{"different build", label("windows", "/opt/mailarchivew.exe", true, false, true, false, false, ""), "a different build of this program"},
		{"not this program", label("linux", "/opt/mailarchive", true, false, false, false, false, ""), "not this program"},
		{"former name, is this", label("linux", "/opt/mailarchive-desktop", true, true, false, true, false, "same_file"), "a former name of this program"},
		{"console build", label("windows", "/opt/mailarchive.exe", true, true, false, false, true, "same_file"), "the command-line build of this program (a console shows while it runs)"},
		{"unchanged since install", label("linux", "/opt/mailarchive", true, true, false, false, false, "unchanged_since_install"), "unchanged since it was installed (not compared with this program)"},
		{"this program", label("linux", "/opt/mailarchive", true, true, false, false, false, "program_name"), "this program"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: Program label = %q, want %q", c.name, c.got, c.want)
		}
	}

	// scheduledKind: nothing corroborated (empty config/job dir), so it names the
	// recorded descriptor's own kind.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	out := t.TempDir()
	for _, c := range []struct {
		job  []string
		want schedule.Kind
	}{
		{[]string{"-job", "/x/.mailarchive-job.json"}, schedule.KindRunJob},
		{[]string{"verify", "-out", out}, schedule.KindVerify},
		{[]string{"-out", out, "-mode", "incremental"}, schedule.KindExport},
	} {
		got := scheduledKind(Input{Out: out, Desc: schedule.Descriptor{Job: c.job}})
		if got != c.want {
			t.Errorf("scheduledKind(%v) = %q, want %q", c.job, got, c.want)
		}
	}
}
