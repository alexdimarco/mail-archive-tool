//go:build linux || darwin

package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"mail-archive-tool/internal/tty/ptytest"
)

// ptyScreen is what a pty's master end shows, line by line.
type ptyScreen struct {
	t     *testing.T
	mu    sync.Mutex
	lines []string
	seen  int
}

func watchPty(t *testing.T, master *os.File) *ptyScreen {
	s := &ptyScreen{t: t}
	go func() {
		sc := bufio.NewScanner(master)
		for sc.Scan() {
			s.mu.Lock()
			s.lines = append(s.lines, strings.TrimRight(sc.Text(), "\r"))
			s.mu.Unlock()
		}
	}()
	return s
}

func (s *ptyScreen) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

// next waits for the first line from the one after the last match that matches re,
// and returns its submatches.
func (s *ptyScreen) next(re *regexp.Regexp, d time.Duration) []string {
	s.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for s.seen < len(s.lines) {
			l := s.lines[s.seen]
			s.seen++
			if m := re.FindStringSubmatch(l); m != nil {
				s.mu.Unlock()
				return m
			}
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("no line matching %v within %v; the screen shows:\n%s", re, d, s.all())
	return nil
}

var (
	reSearchUI = regexp.MustCompile(`^Search UI:\s+(http://127\.0\.0\.1:\d+)/$`)
	reOpenLink = regexp.MustCompile(`^Open this link in your browser: (http://\S+/_open\?t=[A-Za-z0-9_-]+)$`)
)

// isolatedEnv keeps a child's per-account state, config and secrets out of the
// real ones: every place a child could look for them is a temp dir.
func isolatedEnv(t *testing.T, extra ...string) []string {
	dir := t.TempDir()
	return append(os.Environ(), append([]string{"HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "LOCALAPPDATA=" + dir, "APPDATA=" + dir}, extra...)...)
}

// covers: MA-373, R23, X8
// The real `serve` binary on a real pseudo-terminal: stdout is a terminal, so it
// prints the address and a link; the link redeems; each redemption is a notice on
// stderr (a pipe, not the terminal); Enter, typed on the terminal, prints a
// fresh link; and Ctrl-C, typed on it, ends the process cleanly, exit 0 (a handled
// interrupt, X1) [CC5, CC18, CC24].
func TestServeOnAPtyPrintsTheLinkAndEnterGivesAnother(t *testing.T) {
	out := buildSearchFixture(t)
	master, slave := ptytest.Open(t)
	screen := watchPty(t, master)
	cmd := ptytest.Command(slave, testBin, "serve", "-out", out, "-addr", "127.0.0.1:0", "-no-browser")
	cmd.Env = isolatedEnv(t)
	cmd.Stderr = nil // stderr is a pipe, apart from the terminal
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	notices := &syncBuf{}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			notices.Write([]byte(sc.Text() + "\n"))
		}
	}()

	origin := screen.next(reSearchUI, 20*time.Second)[1]
	first := screen.next(reOpenLink, 10*time.Second)[1]
	if !strings.HasPrefix(first, origin+"/_open?t=") {
		t.Errorf("the printed link %q is not on %s", first, origin)
	}
	if code, _, _ := fetch(t, first); code != http.StatusOK {
		t.Errorf("the printed link: status %d, want 200", code)
	}
	waitFor(t, "the session notice on stderr", 5*time.Second, func() bool {
		return strings.Contains(notices.String(), "browser session opened at") && strings.Contains(notices.String(), "(1 this run)")
	})
	if strings.Contains(notices.String(), "_open?t=") {
		t.Errorf("a token reached stderr: %s", notices.String())
	}

	if _, err := master.Write([]byte("\n")); err != nil { // Enter, as typed
		t.Fatal(err)
	}
	second := screen.next(reOpenLink, 10*time.Second)[1]
	if second == first {
		t.Errorf("Enter printed the same link again")
	}
	if code, _, _ := fetch(t, second); code != http.StatusOK {
		t.Errorf("the Enter link: status %d, want 200", code)
	}

	if _, err := master.Write([]byte{3}); err != nil { // Ctrl-C, as typed
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Errorf("serve ended by Ctrl-C with %v, want a clean exit 0", err)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("serve did not end within 10 s of Ctrl-C")
	}
}

// covers: MA-373, R23, X8
// With stdout a pipe (a journal, nohup.out, a CI log) the real binary prints the
// address and `get a link with: mailarchive serve -link -out <folder>` — and no
// token, in stdout or stderr.
func TestServeWithAPipedStdoutPrintsNoToken(t *testing.T) {
	out := buildSearchFixture(t)
	cmd := exec.Command(testBin, "serve", "-out", out, "-addr", "127.0.0.1:0", "-no-browser")
	cmd.Env = isolatedEnv(t)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr syncBuf
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	var lines []string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.HasPrefix(sc.Text(), "Press ") {
			break
		}
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Search UI:  http://127.0.0.1:") || !strings.Contains(text, "get a link with: mailarchive serve -link -out "+out) {
		t.Errorf("stdout lacks the address or the way to get a link:\n%s", text)
	}
	if strings.Contains(text, "_open") || strings.Contains(stderr.String(), "_open") {
		t.Errorf("a token was printed to a pipe:\n%s\n%s", text, stderr.String())
	}
}

// covers: MA-373, R23
// The re-exec'd leader of TestBackgroundedServeNeverReadsStdinAndReadsAfterFg: it
// leads the session whose terminal is the pty, starts `serve` in a process group
// of its own — a background one, as `serve &` is — and, on command over fd 3,
// reports whether serve was stopped (SIGTTIN is what a background read earns) or
// brings serve's group to the foreground, as `fg` does.
func TestHelperLeader(t *testing.T) {
	// no-assert: re-exec helper; the parent test asserts what it prints
	if os.Getenv("MAILARCHIVE_TEST_LEADER") != "1" {
		return
	}
	cmd := exec.Command(os.Getenv("LEADER_BIN"), strings.Split(os.Getenv("LEADER_ARGS"), "\x1f")...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf("LEADER error %v\n", err)
		return
	}
	fmt.Printf("LEADER child=%d\n", cmd.Process.Pid)
	ctl := bufio.NewScanner(os.NewFile(3, "control"))
	for ctl.Scan() {
		switch ctl.Text() {
		case "status":
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(cmd.Process.Pid, &ws, syscall.WUNTRACED|syscall.WNOHANG, nil)
			fmt.Printf("LEADER status stopped=%v\n", err == nil && pid > 0 && ws.Stopped())
		case "fg":
			signal.Ignore(syscall.SIGTTOU)
			err := unix.IoctlSetPointerInt(0, unix.TIOCSPGRP, cmd.Process.Pid)
			fmt.Printf("LEADER fg err=%v\n", err)
		case "quit":
			cmd.Process.Signal(syscall.SIGINT)
			cmd.Wait()
			return
		}
	}
}

// covers: MA-373, R23
// `serve &` on a real terminal: its process group is not the foreground group, so
// it makes no read of stdin — Enter typed meanwhile is not read, no link is
// printed, and the process is not stopped by SIGTTIN; brought to the foreground
// (`fg`) it reads the waiting line and prints the fresh link [CC18].
func TestBackgroundedServeNeverReadsStdinAndReadsAfterFg(t *testing.T) {
	out := buildSearchFixture(t)
	master, slave := ptytest.Open(t)
	screen := watchPty(t, master)
	ctlR, ctlW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctlR.Close(); ctlW.Close() })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := ptytest.Command(slave, exe, "-test.run=^TestHelperLeader$")
	cmd.ExtraFiles = []*os.File{ctlR}
	cmd.Env = isolatedEnv(t, "MAILARCHIVE_TEST_LEADER=1", "LEADER_BIN="+testBin,
		"LEADER_ARGS="+strings.Join([]string{"serve", "-out", out, "-addr", "127.0.0.1:0", "-no-browser"}, "\x1f"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctlW.WriteString("quit\n")
		time.AfterFunc(5*time.Second, func() { cmd.Process.Kill() })
		cmd.Wait()
	})

	screen.next(regexp.MustCompile(`^LEADER child=\d+$`), 30*time.Second)
	screen.next(reSearchUI, 30*time.Second)
	first := screen.next(reOpenLink, 10*time.Second)[1]

	master.Write([]byte("\n")) // Enter, typed while serve is in the background
	time.Sleep(1500 * time.Millisecond)
	ctlW.WriteString("status\n")
	if m := screen.next(regexp.MustCompile(`^LEADER status stopped=(true|false)$`), 10*time.Second); m[1] != "false" {
		t.Fatalf("serve was stopped by the terminal (SIGTTIN): it read stdin from the background")
	}
	for _, l := range strings.Split(screen.all(), "\n") {
		if m := reOpenLink.FindStringSubmatch(l); m != nil && m[1] != first {
			t.Fatalf("a link was printed while serve was in the background: %s", l)
		}
	}

	ctlW.WriteString("fg\n")
	screen.next(regexp.MustCompile(`^LEADER fg err=<nil>$`), 10*time.Second)
	second := screen.next(reOpenLink, 15*time.Second)[1]
	if second == first {
		t.Errorf("the line read after fg printed the same link again")
	}
	if code, _, _ := fetch(t, second); code != http.StatusOK {
		t.Errorf("the link printed after fg: status %d, want 200", code)
	}
}
