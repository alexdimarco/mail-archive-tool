package tbrestore

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1127, R40, S67
// The words a person sees while the lock keeps Thunderbird out (design §5.2 note item
// 5, W-WAIT; lead's call 2026-11-07 from the Mac lab): on Windows they name the
// Cancel of Thunderbird's own dialog (lab B); on macOS and Linux, whose dialog's words
// are not yet seen, they are the lead's platform-neutral words and name no button.
// W-RUNNING adds the Windows sentence about the clock and other signed-in people only
// on Windows.
func TestRestoreWaitAndRunningWords(t *testing.T) {
	const neutral = "While this runs, leave Thunderbird closed. If Thunderbird says it is already open or running, close that message and wait for the restore to finish."
	for _, goos := range []string{"darwin", "linux"} {
		if got := waitWords(goos); got != neutral {
			t.Errorf("%s W-WAIT: %q", goos, got)
		}
		if strings.Contains(runningWords(goos), "icon by the clock") {
			t.Errorf("%s W-RUNNING has no Windows sentence", goos)
		}
	}
	if w := waitWords("windows"); !strings.Contains(w, "choose Cancel") || !strings.HasPrefix(w, "While this runs, leave Thunderbird closed.") {
		t.Errorf("windows W-WAIT names Cancel: %q", w)
	}
	if !strings.Contains(runningWords("windows"), "icon by the clock") {
		t.Error("windows W-RUNNING has its extra sentence")
	}
	// The R7 friction review (2026-11-07, Thunderbird 153.6.0 on Windows): the menu bar with
	// File is hidden by default, so the way to quit named for Windows is the menu button's
	// Exit, which the lab saw (the ≡ menu, "Exit Ctrl+Shift+Q").
	for _, goos := range []string{"windows", "darwin", "linux"} {
		w := runningWords(goos)
		if !strings.Contains(w, "on Windows: the menu button ≡, then Exit") || strings.Contains(w, "File → Exit") ||
			!strings.Contains(w, "on a Mac: Thunderbird → Quit Thunderbird") {
			t.Errorf("%s W-RUNNING names the way to quit that Thunderbird 153 shows: %q", goos, w)
		}
	}
}

// covers: MA-1124, R40
// The version that last opened the profile (design §4.1), read from
// compatibility.ini: below 122 gives W-OLDTB naming it; 122 and later, an unreadable
// line and no file give none.
func TestRestoreLastVersion(t *testing.T) {
	for line, want := range map[string]string{
		"LastVersion=115.18.0_20241104111013/20241104111013": "Thunderbird 115.18.0",
		"LastVersion=153.6.0_20251212000000/20251212000000":  "",
		"LastVersion=122.0_x/x":                              "",
		"LastVersion=garbage":                                "",
	} {
		profile := makeProfile(t, filepath.Join(t.TempDir(), "p"))
		writeFile(t, filepath.Join(profile, "compatibility.ini"), "[Compatibility]\n"+line+"\nLastOSABI=Darwin_aarch64-gcc3\n")
		got := OldThunderbirdWarning(profile)
		if (want == "") != (got == "") || (want != "" && !strings.Contains(got, want)) {
			t.Errorf("%s: got %q", line, got)
		}
	}
	if OldThunderbirdWarning(makeProfile(t, filepath.Join(t.TempDir(), "p"))) != "" {
		t.Error("no compatibility.ini: no warning")
	}
}
