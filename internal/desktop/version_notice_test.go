package desktop

import (
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
