//go:build linux || darwin

// Package ptytest gives tests a real pseudo-terminal, and a child process that
// has it as its controlling terminal, so terminal behaviour — is stdout a
// terminal, is the process in the foreground group, does a background read stop
// it with SIGTTIN — is checked against the operating system, not a fake [CC18].
package ptytest

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// Open allocates a pseudo-terminal and returns its master and slave ends. Both
// are closed when the test ends.
func Open(t testing.TB) (master, slave *os.File) {
	t.Helper()
	fd, path, err := openMaster()
	if err != nil {
		t.Skipf("no pseudo-terminal on this machine: %v", err)
	}
	master = os.NewFile(uintptr(fd), "ptmx")
	slave, err = os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("open the pty slave %s: %v", path, err)
	}
	t.Cleanup(func() { master.Close(); slave.Close() })
	return master, slave
}

// Command returns cmd wired to the pty: the slave is its stdin, stdout and
// stderr and, as it leads a new session, its controlling terminal; its process
// group starts as the terminal's foreground group.
func Command(slave *os.File, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	return cmd
}

// Describe names the pty for a failure message.
func Describe(master *os.File) string { return fmt.Sprintf("pty master fd %d", master.Fd()) }
