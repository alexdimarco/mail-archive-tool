//go:build !windows

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-508, R29, S52
// Recipient verification with the stock tool, run as an external binary on the host:
// `sha256sum -c` (Linux) or `shasum -a 256 -c` (macOS) passes on a fresh export and
// fails, naming the file, after a byte flip. Split from MA-422; the PowerShell form is
// MA-442's (a lab row).
func TestCopyExportExternalSha256sum(t *testing.T) {
	argv := []string{"sha256sum", "-c", copySumsName}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		if _, err := exec.LookPath("shasum"); err != nil {
			t.Skip("MA-508: neither sha256sum nor shasum is on this host; the CI hosts have one")
		}
		argv = []string{"shasum", "-a", "256", "-c", copySumsName}
	}
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	run := func() (string, error) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = res.Root
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(); err != nil {
		t.Fatalf("%s failed on a fresh export: %v\n%s", argv[0], err, out)
	}
	var victim string
	for p := range treeFiles(t, res.Root) {
		if strings.HasPrefix(p, "messages/") {
			victim = p
			break
		}
	}
	full := filepath.Join(res.Root, filepath.FromSlash(victim))
	orig, _ := os.ReadFile(full)
	os.WriteFile(full, append([]byte("X"), orig[1:]...), 0o600)
	out, err := run()
	if err == nil || !strings.Contains(out, victim) {
		t.Errorf("%s did not fail naming %s after a byte flip: %v\n%s", argv[0], victim, err, out)
	}
}
