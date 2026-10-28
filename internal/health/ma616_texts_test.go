package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-616, R18, S59
// §4.6.2's note/WARN texts for a recorded schedule's program, rendered from the
// identity flags exeIdentity sets (the predicate itself is
// TestMA616_ComposedIdentity; this pins the words): the exe_former_name NOTE
// (the former name, the why, the surface's "Do this" via repairHow, and
// "Nothing changes until you do."), the exe_console_build NOTE, the
// exe_version_skew WARN naming both builds, and exe_missing's former-name hint.
// A note leaves the posture unchanged — a working former-name schedule is GREEN.
// Paths are POSIX so filepath.Base splits them on this test host (on the real
// Windows host the recorded path and the running program share the host's
// separator, so the same code splits correctly there).
func TestMA616_IdentityNoteTexts(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	assess := func(mut func(in *Input)) Report {
		in := healthyInput(now)
		in.Surface = "desktop" // a Repair-button surface, so "Do this" names the card
		mut(&in)
		return Assess(in, now)
	}
	reasonFor := func(r Report, code string) string {
		for i, c := range r.Codes {
			if c == code {
				return r.Reasons[i]
			}
		}
		return ""
	}

	// exe_former_name: a NOTE (no WARN/RED prefix, posture unchanged), naming the
	// former name, the why, the card (the "Do this" the route names for a
	// corroborated run-job on the dashboard), and the reassurance.
	r := assess(func(in *Input) {
		in.Desc.Exe = "/opt/mailarchive/mailarchive-gui"
		in.Desc.Job = []string{"run-job", "-job", "x"}
		in.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: "/j/mailarchive-1234abcd.json"}
		in.ExeIsThis, in.ExeFormerName = true, true
	})
	msg := reasonFor(r, "exe_former_name")
	if msg == "" {
		t.Fatalf("no exe_former_name note:\n%v", r.Reasons)
	}
	for _, want := range []string{"former name of this program", "Why:", "Do this:", "Scheduled backup card", "Nothing changes until you do."} {
		if !strings.Contains(msg, want) {
			t.Errorf("exe_former_name note is missing %q:\n%s", want, msg)
		}
	}
	if strings.HasPrefix(msg, "WARN:") || strings.HasPrefix(msg, "RED:") {
		t.Errorf("exe_former_name must be a note, not a WARN/RED:\n%s", msg)
	}
	if r.Posture != "GREEN" {
		t.Errorf("a working former-name schedule must stay GREEN, got %s:\n%v", r.Posture, r.Reasons)
	}
	// It carries no constant former name of its own (MA-622): the name in the text
	// is the recorded one the fixture supplied.
	if !strings.Contains(msg, "/opt/mailarchive/mailarchive-gui") {
		t.Errorf("exe_former_name should interpolate the recorded name:\n%s", msg)
	}

	// exe_console_build: a NOTE naming mailarchivew.exe as the fix.
	r = assess(func(in *Input) {
		in.Desc.Exe = "/opt/mailarchive/mailarchive.exe"
		in.ExeIsThis, in.ExeConsoleBuild = true, true
	})
	msg = reasonFor(r, "exe_console_build")
	if msg == "" {
		t.Fatalf("no exe_console_build note:\n%v", r.Reasons)
	}
	for _, want := range []string{"command-line build mailarchive.exe", "console window", "mailarchivew.exe", "Repair"} {
		if !strings.Contains(msg, want) {
			t.Errorf("exe_console_build note is missing %q:\n%s", want, msg)
		}
	}
	if r.Posture != "GREEN" {
		t.Errorf("a working console-build schedule must stay GREEN, got %s", r.Posture)
	}

	// exe_version_skew: a WARN naming both builds and the reinstall-then-repair fix.
	const self = "/opt/mailarchive/mailarchivew.exe"
	const rec = "/opt/mailarchive/mailarchive.exe"
	r = assess(func(in *Input) {
		in.Desc.Exe = rec
		in.ExeIsThis, in.ExeSkew, in.ExeSelf = false, true, self
	})
	msg = reasonFor(r, "exe_version_skew")
	if msg == "" {
		t.Fatalf("no exe_version_skew WARN:\n%v", r.Reasons)
	}
	if !strings.HasPrefix(msg, "WARN:") {
		t.Errorf("exe_version_skew must be a WARN:\n%s", msg)
	}
	for _, want := range []string{"different build of this program", rec, self, "reinstall from the MSI", "Do this:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("exe_version_skew WARN is missing %q:\n%s", want, msg)
		}
	}

	// exe_missing with a former name: the RED carries the former-name hint before
	// the repair.
	r = assess(func(in *Input) {
		in.Desc.Exe = "/opt/mailarchive/mailarchive-desktop.exe"
		in.ExeExists = false
	})
	msg = reasonFor(r, "exe_missing")
	if msg == "" {
		t.Fatalf("no exe_missing RED:\n%v", r.Reasons)
	}
	if !strings.Contains(msg, "was a former name of this program and was removed by an upgrade") {
		t.Errorf("exe_missing is missing the former-name hint:\n%s", msg)
	}
}
