//go:build linux || darwin

package tty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tty/ptytest"
)

const typedSecret = "hunter2 correct horse"

// covers: MA-781
// TestHelperReadSecret is the process that asks for the secret, re-executed from
// the test binary. "prompt" asks at its terminal (a session leader, as a terminal's
// first program is). "shell" starts "prompt" in a process group of its own in the
// terminal's foreground, as a shell does, so Ctrl-Z can stop it.
func TestHelperReadSecret(t *testing.T) {
	// no-assert: re-exec helper; the parent test asserts what it prints
	switch os.Getenv("TTY_SECRET_HELPER") {
	case "prompt":
		fmt.Printf("pid=%d\n", os.Getpid())
		b, err := ReadSecret(os.Stdin, os.Stdout, "Password: ")
		var ie *ErrInterrupted
		switch {
		case errors.As(err, &ie):
			fmt.Printf("interrupted=%v\n", ie.Signal)
		case err != nil:
			fmt.Printf("error=%v\n", err)
		default:
			fmt.Printf("match=%v\n", string(b) == typedSecret)
		}
		fmt.Printf("echo=%v\n", echoOn(os.Stdin))
	case "shell":
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestHelperReadSecret$")
		cmd.Env = append(os.Environ(), "TTY_SECRET_HELPER=prompt")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stdout
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: 0}
		if err := cmd.Run(); err != nil {
			fmt.Printf("error=%v\n", err)
		}
	}
}

// echoOn reports whether the terminal f shows what is typed.
func echoOn(f *os.File) bool {
	t, err := unix.IoctlGetTermios(int(f.Fd()), ioctlGetTermios)
	return err == nil && t.Lflag&unix.ECHO != 0
}

// session is a helper run on a fresh pseudo-terminal, with everything it prints.
type session struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	out    bytes.Buffer
}

func startSession(t *testing.T, mode string) *session {
	t.Helper()
	master, slave := ptytest.Open(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, master: master, slave: slave}
	s.cmd = ptytest.Command(slave, exe, "-test.run=^TestHelperReadSecret$")
	s.cmd.Env = append(os.Environ(), "TTY_SECRET_HELPER="+mode)
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	t.Cleanup(func() { s.cmd.Process.Kill(); s.cmd.Wait() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := master.Read(buf)
			s.mu.Lock()
			s.out.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return s
}

func (s *session) printed() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// waitFor waits until cond holds, failing the test after 20 seconds.
func (s *session) waitFor(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			s.t.Fatalf("waited for %s; the terminal shows %q", what, s.printed())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// prompts waits until the prompt has been printed n times.
func (s *session) prompts(n int) {
	s.t.Helper()
	s.waitFor(fmt.Sprintf("prompt %d", n), func() bool { return strings.Count(s.printed(), "Password: ") >= n })
}

// value is what the helper printed after key=.
func (s *session) value(key string) string {
	s.t.Helper()
	s.waitFor(key, func() bool { return strings.Contains(s.printed(), key+"=") })
	rest := s.printed()[strings.Index(s.printed(), key+"=")+len(key)+1:]
	return strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
}

func (s *session) typeIn(text string) {
	s.t.Helper()
	if _, err := s.master.Write([]byte(text)); err != nil {
		s.t.Fatal(err)
	}
}

// covers: MA-781
func TestASecretIsReadWithoutShowingIt(t *testing.T) {
	s := startSession(t, "prompt")
	s.prompts(1)
	s.waitFor("typing hidden", func() bool { return !echoOn(s.slave) })
	s.typeIn(typedSecret + "\n")
	assure.Reached(t, s.value("match") == "true", "the secret read whole")
	if s.value("echo") != "true" {
		t.Fatal("typing is still hidden after the prompt returned")
	}
	if strings.Contains(s.printed(), typedSecret) {
		t.Fatalf("the secret was shown on the terminal: %q", s.printed())
	}
}

// covers: MA-781
func TestAHiddenPromptPutsTheTerminalBackWhenInterrupted(t *testing.T) {
	cases := map[string]struct {
		send func(s *session)
		want string
	}{
		"Ctrl-C":  {func(s *session) { s.typeIn("\x03") }, "interrupt"},
		"SIGTERM": {func(s *session) { s.cmd.Process.Signal(syscall.SIGTERM) }, "terminated"},
		"SIGHUP":  {func(s *session) { s.cmd.Process.Signal(syscall.SIGHUP) }, "hangup"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := startSession(t, "prompt")
			s.prompts(1)
			s.waitFor("typing hidden", func() bool { return !echoOn(s.slave) })
			c.send(s)
			got := s.value("interrupted")
			assure.Reached(t, got, "the interruption")
			if got != c.want {
				t.Fatalf("interrupted by %q, want %q", got, c.want)
			}
			// The prompt's own process reports the terminal after the prompt returned:
			// once it exits, the terminal it led is hung up and cannot be asked.
			if s.value("echo") != "true" {
				t.Fatal("typing is still hidden after the interrupted prompt returned")
			}
		})
	}
}

// covers: MA-781
func TestCtrlZShowsTypingWhileStoppedAndHidesItWhenContinued(t *testing.T) {
	s := startSession(t, "shell")
	s.prompts(1)
	pid, err := strconv.Atoi(s.value("pid"))
	if err != nil {
		t.Fatal(err)
	}
	s.waitFor("typing hidden", func() bool { return !echoOn(s.slave) })
	s.typeIn("\x1a")
	s.waitFor("the prompt stopped", func() bool { return stopped(pid) })
	if !echoOn(s.slave) {
		t.Fatal("typing is hidden while the prompt is stopped")
	}
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	s.prompts(2)
	s.waitFor("typing hidden again", func() bool { return !echoOn(s.slave) })
	s.typeIn(typedSecret + "\n")
	assure.Reached(t, s.value("match") == "true", "the secret read after Ctrl-Z and fg")
	if strings.Contains(s.printed(), typedSecret) {
		t.Fatalf("the secret was shown on the terminal: %q", s.printed())
	}
	// Asked once, and once more after fg: never in a loop.
	if n := strings.Count(s.printed(), "Password: "); n != 2 {
		t.Fatalf("the prompt was printed %d times, want 2: %q", n, s.printed())
	}
}

// stopped reports whether process pid is stopped (ps state T), as Ctrl-Z leaves it.
func stopped(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "T")
}

// covers: MA-781
// Ctrl-Z at a prompt that no shell could continue (the terminal's first program, as
// after exec or ssh -t) is ignored, as the system ignores it there: the prompt does
// not stop for good, typing stays hidden, and the secret is still read.
func TestCtrlZIsIgnoredWhenNothingCouldContinueThePrompt(t *testing.T) {
	s := startSession(t, "prompt")
	s.prompts(1)
	pid, err := strconv.Atoi(s.value("pid"))
	if err != nil {
		t.Fatal(err)
	}
	s.waitFor("typing hidden", func() bool { return !echoOn(s.slave) })
	s.typeIn("\x1a")
	time.Sleep(300 * time.Millisecond)
	if stopped(pid) {
		syscall.Kill(pid, syscall.SIGCONT)
		t.Fatal("Ctrl-Z stopped a prompt nothing could continue")
	}
	if echoOn(s.slave) {
		t.Fatal("Ctrl-Z showed typing at a prompt it could not stop")
	}
	s.typeIn(typedSecret + "\n")
	assure.Reached(t, s.value("match") == "true", "the secret read after an ignored Ctrl-Z")
	if strings.Contains(s.printed(), typedSecret) {
		t.Fatalf("the secret was shown on the terminal: %q", s.printed())
	}
}

// covers: MA-781
func TestAHiddenPromptNeedsATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var out bytes.Buffer
	_, err = ReadSecret(r, &out, "Password: ")
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("terminal"), assure.NoSideEffect(func() bool { return out.Len() == 0 }))
}

// covers: MA-781
func TestALineIsReadToItsEndAndNoFurther(t *testing.T) {
	got, err := readLine(strings.NewReader("secret\r\nnext"))
	if err != nil || string(got) != "secret" {
		t.Fatalf("readLine = %q, %v", got, err)
	}
	r := strings.NewReader("one\ntwo\n")
	readLine(r)
	rest, _ := readLine(r)
	assure.Reached(t, string(rest) == "two", "the next line left for the next read")
	if _, err := readLine(strings.NewReader(strings.Repeat("x", MaxSecret+1) + "\n")); !errors.Is(err, ErrTooLong) {
		t.Fatalf("an over-long line: %v", err)
	}
}
