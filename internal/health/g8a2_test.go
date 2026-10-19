package health

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-157, MA-158, R18, S29
// G8-AO2 (g8 adversarial pass, fresh-eyes re-check): PowerShell reads the
// typographic double quotes U+201C, U+201D and U+201E as the ASCII one, so a
// token holding one closes the double-quoted string the remedy wraps it in and
// the rest of the token runs as a command. The Windows remedy replaces such a
// token with the placeholder, as it does a token with `$(`; a typographic quote
// is not pasted. Positive twin: a token with a typographic SINGLE quote, which
// is inert inside double quotes, keeps its quoting.
func TestMovedArchiveRemedyNeverPastesATypographicQuote(t *testing.T) {
	if !filepath.IsAbs("/elsewhere") {
		t.Skip("the moved-archive WARN needs POSIX-absolute -out spellings")
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	moved := func(tok string) string {
		in := healthyInput(now)
		in.Out = "/handed/archive"
		in.Desc.Name = "mailarchive-x"
		in.Desc.Job = []string{"-out", "/elsewhere", "-auto", "-input", tok}
		in.GOOS = "windows"
		return joinReasons(Assess(in, now))
	}
	if r := moved("C:\\Mail \u2019Box\\a.pst"); !strings.Contains(r, "\u2019Box") {
		t.Fatalf("a typographic single quote lost its quoting:\n%s", r)
	}
	for _, hostile := range []string{"x\u201d; Start-Process calc; \u201cy", "x\u201c; Start-Process calc; \u201cy", "x\u201e; Start-Process calc; \u201ey"} {
		r := moved(hostile)
		if strings.Contains(r, "Start-Process") {
			t.Errorf("the Windows remedy pastes %q, whose typographic quote PowerShell reads as a closing quote:\n%s", hostile, r)
		}
		if !strings.Contains(r, "<edit-this-value-by-hand>") {
			t.Errorf("the Windows remedy for %q carries no placeholder:\n%s", hostile, r)
		}
	}
}

// covers: MA-455, MA-466, R14, R18, S29
// G8-AO1, G8-AI2 (g8 adversarial pass, fresh-eyes re-check): `status` prints a
// removal command from a descriptor, a file the folder's writer can plant, so
// it names the descriptor's entry only when that entry binds to this archive.
// moved_archive prints `-name N -remove` and verify_not_installed prints the
// same with `-- verify`; a descriptor copied from another archive (its job
// backs up a folder that still exists) must not make either tell the operator
// to remove that archive's entry. Positive twins: the archive's own record keeps
// the command. Through Gather, with real descriptors, for the facts, and through
// Assess for the words.
func TestStatusNeverPrintsARemovalForAnEntryThatIsNotThisArchives(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	x, y := t.TempDir(), t.TempDir()
	t.Setenv("LOCALAPPDATA", t.TempDir()) // isolate the Windows wrapper directory
	xName := "nightly-x-g8a2"
	xSpec := schedule.Spec{Name: xName, Interval: schedule.Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"-out", x, "-auto"}, Out: x}
	xVerify := schedule.Spec{Name: "weekly-x-g8a2", Interval: schedule.Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", x, "-unattended"}, Out: x}

	// Facts: X's own records read in X are this archive's; copied into Y they are not.
	if err := schedule.WriteDescriptor(x, schedule.DescribeSpec(xSpec)); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteVerifyDescriptor(x, schedule.DescribeSpec(xVerify)); err != nil {
		t.Fatal(err)
	}
	// On Windows a custom schedule name binds only when its wrapper .cmd exists; plant
	// X's wrappers (whose run line points at X) so X's own records bind, while Y's
	// copies stay unbound because the wrapper points at X, not Y.
	if runtime.GOOS == "windows" {
		for _, s := range []schedule.Spec{xSpec, xVerify} {
			s.Wrapper, s.WrapperPath = true, schedule.DefaultWrapperPath(s.Name)
			if err := os.MkdirAll(filepath.Dir(s.WrapperPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.WrapperPath, []byte(schedule.CmdWrapper(s)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if in := Gather(x, ""); !in.HasDescriptor || in.DescNameUnbound || !in.HasVerifyDescriptor || in.VerifyNameUnbound {
		t.Fatalf("an archive's own records are judged not to be its own: %+v", in)
	}
	if err := schedule.WriteDescriptor(y, schedule.DescribeSpec(xSpec)); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteVerifyDescriptor(y, schedule.DescribeSpec(xVerify)); err != nil {
		t.Fatal(err)
	}
	in := Gather(y, "")
	if !in.HasDescriptor || !in.DescNameUnbound {
		t.Errorf("a descriptor copied from another archive (its folder still exists) is judged this archive's: %+v", in.Desc)
	}
	if !in.HasVerifyDescriptor && in.VerifyDescErr == nil {
		t.Fatal("fixture: the copied verify descriptor was not gathered")
	}

	// Words.
	if !filepath.IsAbs("/elsewhere") {
		t.Skip("the moved-archive WARN needs POSIX-absolute -out spellings")
	}
	movedWords := func(unbound bool) string {
		in := healthyInput(now)
		in.Out = "/handed/archive"
		in.Desc.Name = "nightly-x-g8a2"
		in.Desc.Job = []string{"-out", "/elsewhere", "-auto"}
		in.DescNameUnbound = unbound
		in.GOOS = "linux"
		return joinReasons(Assess(in, now))
	}
	if r := movedWords(false); !strings.Contains(r, "-name 'nightly-x-g8a2' -remove") && !strings.Contains(r, `-name "nightly-x-g8a2" -remove`) {
		t.Fatalf("the archive's own moved record lost its removal command:\n%s", r)
	}
	if r := movedWords(true); strings.Contains(r, "-remove") {
		t.Errorf("moved_archive tells the operator to remove an entry that is not shown to be this archive's:\n%s", r)
	}

	notInstalled := func(unbound bool) string {
		in := healthyInput(now)
		in.GOOS = "linux"
		in.HasVerifyDescriptorFile, in.HasVerifyDescriptor = true, true
		in.VerifyDesc = schedule.Descriptor{Version: 1, Name: "weekly-x-g8a2", Interval: "weekly", At: "05:00",
			Exe: "/usr/local/bin/mailarchive", Job: []string{"verify", "-out", "/a", "-unattended"}, InstalledAt: now.Add(-90 * 24 * time.Hour), Host: "box"}
		in.VerifySchedState, in.VerifyExeExists, in.VerifyExeIsThis = schedule.NotInstalled, true, true
		in.VerifyNameUnbound = unbound
		return joinReasons(Assess(in, now))
	}
	if r := notInstalled(false); !strings.Contains(r, "-remove") {
		t.Fatalf("the archive's own verify record lost its clear command:\n%s", r)
	}
	if r := notInstalled(true); strings.Contains(r, "-remove") {
		t.Errorf("verify_not_installed tells the operator to remove an entry that is not shown to be this archive's:\n%s", r)
	}
}
