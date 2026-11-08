package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1153, R18, R5, S25
// A holder that may be stuck (it started a day or more ago) on another computer is named by that
// computer, which the lock's holder line records, instead of "restart the computer" (the 1.6.1
// friction review's P3: an archive on a shared drive may be held from another computer, and
// restarting this one would not end it). The name is the line's own, kept to a computer name's
// characters, so a planted line forges nothing; a recent holder, wherever it runs, keeps the
// skip's words with no computer named. Positive twin first: a stuck holder on this computer
// (its full DNS name, another case) still says "restart the computer".
func TestAStuckHolderOnAnotherComputerNamesIt(t *testing.T) {
	prev := thisHost
	thisHost = func() string { return "ALEX-MAC" }
	t.Cleanup(func() { thisHost = prev })
	lock := filepath.Join(t.TempDir(), lockfile.Name)
	now := time.Date(2026, 11, 7, 17, 30, 0, 0, time.Local)
	words := func(host string, age time.Duration) string {
		t.Helper()
		line := "pid=1 started=" + now.Add(-age).UTC().Format(time.RFC3339) + " host=" + host + " nonce=n verb=export\n"
		if err := os.WriteFile(lock, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		return busyWords(lock, now)
	}
	here := words("alex-mac.home", 30*time.Hour)
	if !strings.Contains(here, "restart the computer to end it") || strings.Contains(here, "another computer") {
		t.Fatalf("the positive twin: a stuck holder on this computer: %q", here)
	}
	other := words("OFFICE-PC", 30*time.Hour)
	assure.Reached(t, strings.Contains(other, "It started more than a day ago on another computer, OFFICE-PC, and still keeps this archive in use (its lock file, "+lockfile.Name+")") &&
		strings.Contains(other, "restart the computer OFFICE-PC to end it (restarting this one does not)") && strings.Contains(other, "so it may be stuck") &&
		!strings.Contains(other, "nothing is wrong") && !strings.Contains(other, "host="), "a stuck holder on another computer is named by it: "+other)

	planted := words("<b>evil</b>`rm`$(x)\x1b[31m", 30*time.Hour)
	for _, never := range []string{"<", ">", "`", "$", "(x)", "\x1b"} {
		if strings.Contains(strings.SplitN(planted, " — ", 2)[1], never) {
			t.Errorf("a planted host name is echoed (%q): %q", never, planted)
		}
	}
	if !strings.Contains(planted, "restart the computer bevilbrmx31m to end it") {
		t.Errorf("a planted host name keeps only a computer name's characters: %q", planted)
	}
	if recent := words("OFFICE-PC", time.Hour); strings.Contains(recent, "OFFICE-PC") || !strings.Contains(recent, "nothing is wrong") {
		t.Errorf("a recent holder on another computer: %q", recent)
	}
	if none := words("", 30*time.Hour); !strings.Contains(none, "restart the computer to end it") {
		t.Errorf("a line that names no computer: %q", none)
	}
}
