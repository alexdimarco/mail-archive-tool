//go:build linux

package desktop

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// endPreUpgrade ends an older MailArchive Desktop that still holds addr, when it
// is this account's own — the same program file, started by the same user — and
// reports whether the address is free afterwards. Anything else is left alone:
// the dashboard and `status` name it instead [CC22].
func endPreUpgrade(addr string) (int, string) {
	_, ps, err := net.SplitHostPort(addr)
	port, perr := strconv.Atoi(ps)
	if err != nil || perr != nil {
		return 0, ""
	}
	inode := ""
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if b, err := os.ReadFile(f); err == nil {
			if inode = parseProcNetTCP(string(b), port); inode != "" {
				break
			}
		}
	}
	if inode == "" {
		return 0, ""
	}
	pid := ownerOfSocket(inode)
	if pid == 0 || !isOurOwnProgram(pid) {
		return 0, ""
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return 0, ""
	}
	// The ended instance is our own program (isOurOwnProgram), so its path is ours.
	exe, _ := os.Executable()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if probeFormer(addr, 200*time.Millisecond) == "" {
			return pid, exe
		}
	}
	return 0, ""
}

// ownerOfSocket is the process that holds the socket with this inode, or 0.
func ownerOfSocket(inode string) int {
	want := "socket:[" + inode + "]"
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		if l, err := os.Readlink(fd); err == nil && l == want {
			parts := strings.Split(fd, "/")
			if pid, err := strconv.Atoi(parts[2]); err == nil {
				return pid
			}
		}
	}
	return 0
}

// isOurOwnProgram reports whether pid runs this program file as this user.
func isOurOwnProgram(pid int) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return false
	}
	fi, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && exe == self && int(st.Uid) == os.Getuid()
}
