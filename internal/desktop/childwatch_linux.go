//go:build linux

package desktop

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// newChildStarted closes its channel once a child process of this process appears that
// was not there when it was called: the dialog tool the choice runs has started, so its
// environment (with the activation token) is set (the S1 fix round 2, B10). It reads the
// parent of each process in /proc; it stops when stop closes.
func newChildStarted(stop <-chan struct{}) <-chan struct{} {
	before := childPIDs()
	ch := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			for pid := range childPIDs() {
				if _, old := before[pid]; !old {
					close(ch)
					return
				}
			}
		}
	}()
	return ch
}

// childPIDs is the set of this process's children, from each /proc/<pid>/stat's parent
// field (after the command name's closing parenthesis).
func childPIDs() map[int]struct{} {
	me := os.Getpid()
	out := map[int]struct{}{}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 2 {
			continue
		}
		if ppid, err := strconv.Atoi(f[1]); err == nil && ppid == me {
			out[pid] = struct{}{}
		}
	}
	return out
}
