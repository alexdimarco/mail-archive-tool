package tbrestore

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain runs this test binary as a child helper when TBR_HELPER is set: another
// process asking about, holding or taking a profile's Thunderbird lock, as a starting
// Thunderbird would (MA-1127). Otherwise it runs the tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TBR_HELPER"); mode != "" {
		fmt.Println(lockHelper(mode, os.Getenv("TBR_PATH")))
		if mode == "hold" {
			time.Sleep(time.Hour) // until killed
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
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
