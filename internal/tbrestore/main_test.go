package tbrestore

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/testhome"
)

// TestMain runs this test binary as a child helper when TBR_HELPER is set: another
// process asking about, holding or taking a profile's Thunderbird lock, as a starting
// Thunderbird would (MA-1127). Otherwise it points the home, settings, cache and state
// folders at a throw-away folder for the whole package and runs the tests through
// testhome.Run, so none reaches this computer's real settings folder (the restore lock and
// the restore log live there) or Thunderbird profile, even to read them (MA-1333). A test
// that needs its own sets them again with t.Setenv; the helpers inherit the throw-away
// folders.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TBR_HELPER"); mode != "" {
		fmt.Println(lockHelper(mode, os.Getenv("TBR_PATH")))
		if mode == "hold" {
			time.Sleep(time.Hour) // until killed
		}
		os.Exit(0)
	}
	root, err := os.MkdirTemp("", "mailarchive-tbrestore-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, root)
	}
	code := testhome.Run(root, m)
	os.RemoveAll(root)
	os.Exit(code)
}

// childSays runs the helper in mode against path and returns its first line.
func childSays(t *testing.T, mode, path string) string {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	c.Env = append(os.Environ(), "TBR_HELPER="+mode, "TBR_PATH="+path)
	out, err := c.Output()
	if err != nil {
		t.Fatalf("helper %s: %v", mode, err)
	}
	return strings.TrimSpace(string(out))
}

// childHolds starts a helper that takes the lock at path and keeps it until the
// returned kill is called (a kill -9 / TerminateProcess, not a clean exit).
func childHolds(t *testing.T, path string) func() {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	c.Env = append(os.Environ(), "TBR_HELPER=hold", "TBR_PATH="+path)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "took" {
		_ = c.Process.Kill()
		_ = c.Wait()
		t.Fatalf("the holder did not take the lock: %q", line)
	}
	done := false
	var kill func()
	kill = func() {
		if !done {
			done = true
			_ = c.Process.Kill()
			_ = c.Wait()
		}
	}
	t.Cleanup(kill)
	return kill
}
