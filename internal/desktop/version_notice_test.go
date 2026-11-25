package desktop

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// covers: MA-637, S59
// The pure rule: a handoff names the holder when its version is absent (a pre-SE1
// instance), or differs from this program's with neither a 'dev' build (§4.5.3).
func TestMA637_OtherVersion(t *testing.T) {
	cases := []struct {
		holder, mine string
		want         bool
	}{
		{"", "1.0.0", true},       // absent (pre-SE1) → name it
		{"1.0.0", "2.0.0", true},  // differs, neither dev → name it
		{"1.0.0", "1.0.0", false}, // same → no notice
		{"dev", "1.0.0", false},   // holder is a dev build → not comparable
		{"1.0.0", "dev", false},   // this program is a dev build → not comparable
		{"dev", "dev", false},
		{"", "dev", true}, // absent is still named, even from a dev build
	}
	for _, c := range cases {
		if got := otherVersion(c.holder, c.mine); got != c.want {
			t.Errorf("otherVersion(%q, %q) = %v, want %v", c.holder, c.mine, got, c.want)
		}
	}
}

// covers: MA-637, S59
// The notice says what to do in the launcher's own system's words (the Windows walk of S1,
// 2026-11-25: it said "MailArchive Desktop" though the Start menu now says MailArchive, told a
// Windows user `kill`, and never named the Shut down button on the page that had just opened,
// which every 1.8.0 page has). Positive twin first, on every system: it names the holder's
// program and pid and says to click Shut down, then open MailArchive again. Then the fallback is
// the system's own (Task Manager, Activity Monitor, `kill`) and never another system's.
func TestMA637_TheNoticeSaysWhatToDoOnEachSystem(t *testing.T) {
	exe, pid := `C:\Program Files\MailArchive\mailarchivew.exe`, 9616
	for _, c := range []struct {
		goos, fallback string
		never          []string
	}{
		{"windows", "end process 9616 in Task Manager", []string{"kill", "Activity Monitor"}},
		{"darwin", "quit process 9616 in Activity Monitor", []string{"kill", "Task Manager"}},
		{"linux", "run `kill 9616` in a terminal", []string{"Task Manager", "Activity Monitor"}},
	} {
		got := otherVersionTextOn(c.goos, exe, pid)
		for _, want := range []string{"Another version of MailArchive is still running", exe, "process 9616", "click Shut down", "then open MailArchive again"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: positive twin: the notice lacks %q: %q", c.goos, want, got)
			}
		}
		if !strings.Contains(got, c.fallback) {
			t.Errorf("%s: the notice lacks this system's way %q: %q", c.goos, c.fallback, got)
		}
		for _, bad := range append(c.never, "MailArchive Desktop") {
			if strings.Contains(got, bad) {
				t.Errorf("%s: the notice says %q: %q", c.goos, bad, got)
			}
		}
	}
	if otherVersionText(exe, pid) != otherVersionTextOn(runtime.GOOS, exe, pid) {
		t.Errorf("the launcher's notice is not this system's")
	}
}

// covers: MA-637, S59
// At the launch level: a second launch that hands off to a holder of another or
// no version is told through the dialog seam — a notice naming the holder's
// program and pid — and still succeeds (exit 0); a same-version holder is not
// named (§4.5.3). The notice fires only after a verified handoff.
func TestMA637_HandoffNamesAnotherVersion(t *testing.T) {
	t.Run("another version", func(t *testing.T) {
		g := newDRig(t)
		g.version = "1.0.0"
		g.start()
		g.version = "2.0.0"
		rec, err := g.second()
		if err != nil {
			t.Fatalf("the handoff must succeed (exit 0): %v", err)
		}
		notices := rec.noticeList()
		if len(notices) != 1 {
			t.Fatalf("got %d notice(s), want exactly one naming the other-version holder: %v", len(notices), notices)
		}
		if !strings.Contains(notices[0], strconv.Itoa(g.info.PID)) || !strings.Contains(notices[0], "Another version") {
			t.Errorf("the notice should name the holder's pid %d and say another version: %q", g.info.PID, notices[0])
		}
		if g.info.Exe != "" && !strings.Contains(notices[0], g.info.Exe) {
			t.Errorf("the notice should name the holder's program %q: %q", g.info.Exe, notices[0])
		}
	})

	t.Run("absent version", func(t *testing.T) {
		g := newDRig(t)
		g.version = "" // a pre-SE1 holder stamps no version
		g.start()
		g.version = "2.0.0"
		rec, err := g.second()
		if err != nil {
			t.Fatalf("the handoff must succeed: %v", err)
		}
		if n := rec.noticeList(); len(n) != 1 {
			t.Errorf("a holder with no version must be named once: %v", n)
		}
	})

	t.Run("same version (positive twin)", func(t *testing.T) {
		g := newDRig(t)
		g.version = "1.0.0"
		g.start()
		// The launcher keeps 1.0.0.
		rec, err := g.second()
		if err != nil {
			t.Fatalf("the handoff must succeed: %v", err)
		}
		if n := rec.noticeList(); len(n) != 0 {
			t.Errorf("a same-version holder must not be named: %v", n)
		}
	})
}
