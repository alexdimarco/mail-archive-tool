//go:build !linux && !darwin && !windows

package procs

import "errors"

// Program cannot be read on this system: a caller that needs to know what a pid runs
// before it acts treats that as "cannot tell" and does not act.
func Program(pid int) (string, error) {
	return "", errors.New("this system does not say which program a process runs")
}
