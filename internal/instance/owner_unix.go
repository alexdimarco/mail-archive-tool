//go:build !windows

package instance

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// defaultOwnerReader reads the file's uid and reports the process's own.
func defaultOwnerReader(path string) (string, []string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", nil, fmt.Errorf("no owner information for %s on this system", path)
	}
	return strconv.Itoa(int(st.Uid)), []string{strconv.Itoa(os.Getuid())}, nil
}

// LogonSession is the logon-session id of this process: POSIX systems keep one
// per login and this version records none.
func LogonSession() string { return "" }

// SessionOf is the logon session of the process pid: "" here, where the lock and the
// instance file are per account.
func SessionOf(pid int) (string, error) { return "", nil }
