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

// covers: MA-894, R37
// A test that runs the built program with the test's own environment (os.Environ(), as
// MA-612's run-job test does) reaches only the throwaway home: the settings folder the
// test process and its children see is under it, and a refused scheduled run's failure
// record lands there, naming this run's folder — never in the developer's settings, whose
// alert set up there would otherwise send a real backup-failed email.
func TestAProgramATestStartsSeesOnlyTheThrowawayHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run-job's failure record on Windows is a lab item (L-W)")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(cliHome, cfg); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("the tests' settings folder %s is not under the throwaway home %s", cfg, cliHome)
	}
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
}
