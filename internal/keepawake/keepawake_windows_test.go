//go:build windows

package keepawake

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// systemRequests is the SYSTEM section of `powercfg /requests`: the processes that
// are keeping Windows from idle sleep now. It needs an administrator; without one
// the test is skipped.
func systemRequests(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("powercfg", "/requests").CombinedOutput()
	if err != nil {
		t.Skipf("powercfg /requests needs an administrator here: %v: %s", err, out)
	}
	s := string(out)
	start := strings.Index(s, "SYSTEM:")
	if start < 0 {
		t.Fatalf("powercfg /requests has no SYSTEM section:\n%s", s)
	}
	s = s[start+len("SYSTEM:"):]
	if end := strings.Index(s, "AWAYMODE:"); end >= 0 {
		s = s[:end]
	}
	return s
}

// covers: MA-935
// The real request, read back from Windows: while held, this test process is listed
// as keeping the system from idle sleep; once released, it is not.
func TestHoldIsSeenByWindowsAndReleased(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.ToLower(filepath.Base(exe))
	if strings.Contains(strings.ToLower(systemRequests(t)), name) {
		t.Fatalf("%s is already listed before any hold", name)
	}
	release := Hold()
	held := strings.ToLower(systemRequests(t))
	release()
	if !strings.Contains(held, name) {
		t.Errorf("while held, %s is not among the SYSTEM requests:\n%s", name, held)
	}
	if after := strings.ToLower(systemRequests(t)); strings.Contains(after, name) {
		t.Errorf("after release, %s is still among the SYSTEM requests:\n%s", name, after)
	}
}
