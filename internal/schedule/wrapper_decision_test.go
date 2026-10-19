package schedule

import (
	"strings"
	"testing"
)

// covers: MA-650
//
// WrapperNeeded is the one wrapper decision every install caller shares (the
// CLI's runSchedule, the wizard's installSpec, the desktop card's
// backupSpecFor), so none can diverge and flash a console window on the common
// path. The common Windows case runs the no-console program directly — no
// cmd.exe, so no console window (the tester-reported friction, §D) — and only an
// over-long run string falls back to the cmd.exe wrapper. This proves the rule
// for each OS and length; the callers all route through the one helper.
func TestWrapperNeededDirectByDefaultOnlyWrapsOverLong(t *testing.T) {
	exe := `C:\Program Files\Mail Archive\mailarchivew.exe`
	normal := Spec{
		Name: "mailarchive-1a2b3c4d", Interval: Daily, At: "02:00", Exe: exe,
		Args: []string{"copy", "-out", `C:\Users\steve\MailArchive`, "-log", `C:\Users\steve\MailArchive\mailarchive.log`},
	}
	if n := normal.TaskRunLength(); n > SchtasksRunLimit {
		t.Fatalf("the normal backup run string is already %d chars, over the %d limit — pick a shorter fixture", n, SchtasksRunLimit)
	}
	overLong := normal
	overLong.Args = []string{"copy", "-out", `C:\Users\steve\` + strings.Repeat("x", SchtasksRunLimit), "-log", "x"}
	if n := overLong.TaskRunLength(); n <= SchtasksRunLimit {
		t.Fatalf("the over-long fixture is only %d chars, not over the %d limit", n, SchtasksRunLimit)
	}

	for _, c := range []struct {
		name string
		s    Spec
		goos string
		want bool
	}{
		{"windows, normal length -> direct launch, no console window", normal, "windows", false},
		{"windows, over-long run string -> cmd.exe wrapper fallback", overLong, "windows", true},
		{"darwin never wraps", overLong, "darwin", false},
		{"linux never wraps", overLong, "linux", false},
	} {
		if got := wrapperNeededOn(c.s, c.goos); got != c.want {
			t.Errorf("%s: wrapperNeededOn = %v, want %v (run length %d, limit %d)", c.name, got, c.want, c.s.TaskRunLength(), SchtasksRunLimit)
		}
	}
}
