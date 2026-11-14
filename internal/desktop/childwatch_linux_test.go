//go:build linux

package desktop

import (
	"os/exec"
	"testing"
	"time"
)

// covers: MA-1427, R50
// The choice's dialog process is seen to start (the S1 fix round 2, B10: the activation
// token leaves the environment then). Positive twin first: with no new child process the
// watch does not fire. A child process started after the watch began makes it fire; a stop
// ends the watch.
func TestMA1427_TheDialogProcessIsSeenToStart(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	seen := newChildStarted(stop)
	select {
	case <-seen:
		t.Fatal("twin: the watch fired with no new child process")
	case <-time.After(150 * time.Millisecond):
	}
	cmd := exec.Command("sleep", "2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	select {
	case <-seen:
	case <-time.After(3 * time.Second):
		t.Error("a child process started and the watch never saw it")
	}
}
