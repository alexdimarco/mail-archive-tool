//go:build darwin

package tbrestore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// pgrepArgs is the macOS process query: Thunderbird processes of this user, by exact
// name (presync's pgrepCmd, env_darwin.go).
func pgrepArgs(uid int) []string {
	return []string{"/usr/bin/pgrep", "-x", "-u", strconv.Itoa(uid), "thunderbird"}
}

// thunderbirdProcessCount counts the pids pgrep lists, before any further read, as
// presync's rawThunderbirdCount does (design §5.1 note: the raw count, not the list
// that drops a process it cannot fully read). pgrep's exit 1 is none; any other
// failure is not known (counted as running).
func thunderbirdProcessCount() (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := pgrepArgs(os.Getuid())
	c := exec.CommandContext(ctx, a[0], a[1:]...)
	c.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	out, err := c.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return 0, true
		}
		return 0, false
	}
	return countPgrep(string(out)), true
}

// countPgrep counts pgrep's pid lines.
func countPgrep(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if _, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			n++
		}
	}
	return n
}
