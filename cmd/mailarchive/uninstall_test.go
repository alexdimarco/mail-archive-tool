package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// isolatedConfigEnv points every per-user-config env var at a fresh empty dir, so the
// uninstall CLI reads/plans against nothing real on the test machine.
func isolatedConfigEnv(t *testing.T) []string {
	t.Helper()
	d := t.TempDir()
	return append(os.Environ(),
		"HOME="+d, "USERPROFILE="+d, "XDG_CONFIG_HOME="+d, "AppData="+d, "LOCALAPPDATA="+d)
}

// covers: MA-679
// The `uninstall` CLI verb previews what it will remove and the archives it keeps, stops on
// -dry-run, and refuses a real run without -yes — so nothing is removed by accident. (The
// destructive path is covered in isolation by the orchestrator test, MA-678; here we only
// exercise the safe preview + the confirmation gate, against an empty config.)
func TestMA679_UninstallCLIPreviewAndGate(t *testing.T) {
	env := isolatedConfigEnv(t)

	dr := exec.Command(testBin, "uninstall", "-dry-run")
	dr.Env = env
	out, err := dr.CombinedOutput()
	if err != nil {
		t.Fatalf("`uninstall -dry-run` must succeed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "KEEP your") || !strings.Contains(string(out), "will REMOVE") {
		t.Fatalf("the dry run must show what is removed and kept, got:\n%s", out)
	}

	// A real run without -yes must refuse (exit non-zero), so nothing is removed by accident.
	ng := exec.Command(testBin, "uninstall")
	ng.Env = env
	if out, err := ng.CombinedOutput(); err == nil {
		t.Fatalf("`uninstall` without -yes must refuse, but it exited 0:\n%s", out)
	}
}
