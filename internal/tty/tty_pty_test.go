//go:build linux || darwin

package tty

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tty/ptytest"
)

// covers: MA-373, R23, X8
// A pseudo-terminal is a terminal; a pipe, a file, /dev/null and a nil file are
// not: the link is printed only to a terminal [CC5].
func TestIsTerminal(t *testing.T) {
	_, slave := ptytest.Open(t)
	if !IsTerminal(slave) {
		t.Errorf("a pty slave is not reported as a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	file, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for name, f := range map[string]*os.File{"a pipe": r, "/dev/null": null, "a file": file, "nil": nil} {
		if IsTerminal(f) {
			t.Errorf("%s is reported as a terminal", name)
		}
	}
}

// covers: MA-373, R23
// The two processes of TestForegroundFollowsTheTerminalsGroup, re-executed from
// the test binary. The leader of the session (the pty is its controlling
// terminal, its group the foreground group) prints whether it is in the
// foreground, then starts a child in a process group of its own — a background
// one, as `serve &` is — which prints the same, then takes the terminal back with
// tcsetpgrp, as `fg` does, and prints it once more.
func TestHelperForeground(t *testing.T) {
	// no-assert: re-exec helper; the parent test asserts what it prints
	switch os.Getenv("TTY_TEST_HELPER") {
	case "leader":
		fmt.Printf("fgLeader=%v\n", Foreground(os.Stdin))
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestHelperForeground$")
		cmd.Env = append(os.Environ(), "TTY_TEST_HELPER=child")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Run(); err != nil {
			fmt.Printf("error=%v\n", err)
		}
	case "child":
		fmt.Printf("fgBackground=%v\n", Foreground(os.Stdin))
		signal.Ignore(syscall.SIGTTOU) // a background tcsetpgrp would stop us otherwise
		if err := unix.IoctlSetPointerInt(0, unix.TIOCSPGRP, syscall.Getpgrp()); err != nil {
			fmt.Printf("error=%v\n", err)
			return
		}
		fmt.Printf("fgTakenBack=%v\n", Foreground(os.Stdin))
	}
}

// covers: MA-373, R23
// Foreground tells the terminal's foreground process group from a background one
// against a real pseudo-terminal: true for the session leader, false for a child
// in a group of its own (as under `serve &`), true again once the child takes the
// terminal back (as under `fg`) [CC18]. A stdin that is no terminal is never
// "foreground".
func TestForegroundFollowsTheTerminalsGroup(t *testing.T) {
	master, slave := ptytest.Open(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := ptytest.Command(slave, exe, "-test.run=^TestHelperForeground$")
	cmd.Env = append(os.Environ(), "TTY_TEST_HELPER=leader")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	got := map[string]string{}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(master)
		for sc.Scan() {
			lines <- strings.TrimSpace(sc.Text())
		}
		close(lines)
	}()
	deadline := time.After(20 * time.Second)
	for len(got) < 3 {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("the helper's output ended with %v", got)
			}
			if k, v, found := strings.Cut(l, "="); found && (strings.HasPrefix(k, "fg") || k == "error") {
				if k == "error" {
					t.Fatalf("helper: %s", v)
				}
				got[k] = v
			}
		case <-deadline:
			cmd.Process.Kill()
			t.Fatalf("the helper printed %v before the deadline", got)
		}
	}
	<-done
	assure.Reached(t, got, "foreground samples")
	if got["fgLeader"] != "true" || got["fgBackground"] != "false" || got["fgTakenBack"] != "true" {
		t.Errorf("Foreground for the leader, a background group, and after taking the terminal back = %v, want true, false, true", got)
	}

	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if Foreground(r) || Foreground(nil) {
		t.Errorf("Foreground is true for something that is not a terminal")
	}
}
