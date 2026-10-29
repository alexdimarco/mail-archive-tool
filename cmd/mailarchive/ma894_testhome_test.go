package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// underHome reports whether path is inside the throwaway home.
func underHome(path string) bool {
	rel, err := filepath.Rel(cliHome, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// covers: MA-894, R37
// A test that runs the built program with the test's own environment (os.Environ(), as
// MA-612's run-job test does) reaches only the throwaway home: the settings folder the
// test process and its children see is under it, and a refused scheduled run's failure
// record lands there, naming this run's folder — never in the developer's settings, whose
// alert set up there would otherwise send a real backup-failed email. The run's failure
// notification starts the package's do-nothing notifier, never the real one (every
// notifier name resolves into the fakes' folder, and the fake records the call). A `go`
// a test starts keeps the real Go caches and toolchain pin, not ones under the home.
func TestAProgramATestStartsSeesOnlyTheThrowawayHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run-job's failure record on Windows is a lab item (L-W)")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !underHome(cfg) {
		t.Fatalf("the tests' settings folder %s is not under the throwaway home %s", cfg, cliHome)
	}
	for _, v := range []string{"GOCACHE", "GOMODCACHE", "GOTOOLCHAIN"} {
		if got := os.Getenv(v); got == "" || underHome(got) {
			t.Fatalf("%s=%q: a child `go` would not keep the real setting", v, got)
		}
	}
	// Checked before anything runs, so a broken guard never pops a real banner.
	for _, name := range []string{"osascript", "notify-send", "zenity", "qarma", "matedialog"} {
		if p, err := exec.LookPath(name); err != nil || filepath.Dir(p) != fakeNotifierDir {
			t.Fatalf("%s resolves to %q (%v), not the do-nothing fake in %q", name, p, err, fakeNotifierDir)
		}
	}
	before, _ := os.ReadFile(fakeNotifierLog)

	dir := t.TempDir()
	missing := filepath.Join(dir, "not-mounted")
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte(`{"version":1,"out":"`+missing+`","mode":"incremental"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testBin, "run-job", jobPath)
	cmd.Env = os.Environ()
	_ = cmd.Run() // refused: its folder is not there
	rec, err := os.ReadFile(filepath.Join(cfg, "mailarchive", "last-failure.json"))
	assure.Reached(t, err == nil && strings.Contains(string(rec), missing), "the refused run's failure record, naming its folder, in the throwaway home")
	after, _ := os.ReadFile(fakeNotifierLog)
	assure.Reached(t, strings.TrimPrefix(string(after), string(before)), "the refused run's notification, taken by the do-nothing fake")
}
