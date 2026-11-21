package autoplace

import (
	"runtime"
	"testing"
)

// covers: MA-869, R37
// On Windows a path counts as local only when its volume is a drive letter (the 1.7.5 re-verify's
// RV-01): Windows reads "\/host/…", "/\host\…" and "\??\UNC\host\…" as a network share, so all three
// are non-local, as "\\host" and "//host" are. The rule as a pure function, fed the volume part, runs
// on every system. Positive twin first: a drive-letter path on Windows, and an absolute path on
// macOS and Linux, are local.
func TestOnWindowsOnlyADriveLetterPathIsLocal(t *testing.T) {
	if nonLocal(`C:\Users\qz\AppData`, "windows", "C:", true) || nonLocal("/Users/qz/Library", "darwin", "", true) || nonLocal("/home/qz/.thunderbird", "linux", "", true) {
		t.Fatal("positive twin: a drive-letter path on Windows, and an absolute path elsewhere, are local")
	}
	for _, c := range []struct{ p, vol string }{
		{`\/qz-host/s/x`, `\/qz-host/s`},
		{`/\qz-host\s\x`, `/\qz-host\s`},
		{`\??\UNC\qz-host\s\x`, `\??\UNC\qz-host\s`},
		{`\??\UNC\qz-host\s\x`, `\??\`},
		{`\\qz-host\s\x`, `\\qz-host\s`},
		{"//qz-host/s/x", "//qz-host/s"},
		{`\\?\C:\x`, `\\?\C:`},
	} {
		if !nonLocal(c.p, "windows", c.vol, true) {
			t.Errorf("on Windows %q (volume %q) counts as local", c.p, c.vol)
		}
	}
	if !nonLocal("relative/path", "linux", "", false) {
		t.Error("a path that is not absolute counts as local")
	}
}

// covers: MA-869, R37
// The same three spellings through NonLocal itself, on Windows (Windows CI and the lab run it; on
// macOS and Linux these are other paths, so it skips there). Positive twin first: C:\ is local.
func TestNonLocalRefusesWindowsNetworkSpellings(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path rules: run by Windows CI and the lab")
	}
	if NonLocal(`C:\Users\qz`) {
		t.Fatal(`positive twin: C:\Users\qz is local`)
	}
	for _, p := range []string{`\/qz-host/s/x`, `/\qz-host\s\x`, `\??\UNC\qz-host\s\x`, `\\qz-host\s\x`, "//qz-host/s/x"} {
		if !NonLocal(p) {
			t.Errorf("NonLocal(%q) is false on Windows", p)
		}
	}
}
