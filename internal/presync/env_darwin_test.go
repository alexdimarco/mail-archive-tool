//go:build darwin

package presync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// covers: MA-557
// Every macOS helper command uses an absolute path (so exec never does a PATH
// lookup), a fixed argument list (no shell, no -c, no value interpolated into a
// script — the osascript quit is a constant), and the argv catalog equals this
// golden list (design §4.7.1, §5.4). Asserted on the built *exec.Cmd WITHOUT
// running it, so nothing launches, signals or touches Thunderbird.
func TestMacCommandArgvGolden(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		cmd  *exec.Cmd
		want []string
	}{
		{"open", openCmd(ctx, "/Applications/Thunderbird.app", "/Users/x/p"), []string{"/usr/bin/open", "-g", "-j", "/Applications/Thunderbird.app", "--args", "-profile", "/Users/x/p"}},
		{"quit", quitCmd(ctx), []string{"/usr/bin/osascript", "-e", quitScript}},
		{"pgrep", pgrepCmd(ctx, 501), []string{"/usr/bin/pgrep", "-x", "-u", "501", "thunderbird"}},
		{"ps-args", psArgsCmd(ctx, 9), []string{"/bin/ps", "-p", "9", "-o", "args="}},
		{"ps-etime", psEtimeCommCmd(ctx, 9), []string{"/bin/ps", "-p", "9", "-o", "etime=", "-o", "comm="}},
		{"sysctl", sysctlBoottimeCmd(ctx), []string{"/usr/sbin/sysctl", "-n", "kern.boottime"}},
		{"ioreg", ioregIdleCmd(ctx), []string{"/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4"}},
		{"plutil", plutilBundleIDCmd(ctx, "/A.app"), []string{"/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", "/A.app/Contents/Info.plist"}},
		{"caffeinate", caffeinateCmd(ctx, 9), []string{"/usr/bin/caffeinate", "-i", "-w", "9"}},
	} {
		if !strings.HasPrefix(c.cmd.Path, "/") {
			t.Errorf("%s: path %q is not absolute (a non-absolute name triggers a PATH lookup)", c.name, c.cmd.Path)
		}
		if !reflect.DeepEqual(c.cmd.Args, c.want) {
			t.Errorf("%s argv = %v, want %v", c.name, c.cmd.Args, c.want)
		}
		// Belt and braces: no element of the argv is a shell/interpreter — the
		// danger is a `<shell> -c <script>`, never a tool's own `-c` flag (ioreg
		// uses `-c IOHIDSystem` for its class), so flag the interpreter, not `-c`.
		for _, a := range c.cmd.Args {
			switch a {
			case "/bin/sh", "/bin/bash", "/bin/zsh", "/usr/bin/env", "sh", "bash", "zsh", "env":
				t.Errorf("%s argv carries a shell/interpreter token %q", c.name, a)
			}
		}
	}
}

// covers: MA-563
// The child environment of every macOS helper is exactly the allowlist (design
// §5.5): with DYLD_INSERT_LIBRARIES, LD_PRELOAD, MOZ_* and a hostile PATH set in
// our own environment, none of the forbidden variables reaches a command, so a
// planted library-injection can never reach Thunderbird. HOME and the rest of
// the allowlist are carried through; ps additionally carries LC_ALL=C.
func TestMacCommandEnvAllowlist(t *testing.T) {
	t.Setenv("DYLD_INSERT_LIBRARIES", "/evil.dylib")
	t.Setenv("LD_PRELOAD", "/evil.so")
	t.Setenv("MOZ_LOG", "all")
	t.Setenv("PATH", "/evil/bin")
	t.Setenv("HOME", "/Users/x")
	t.Setenv("TMPDIR", "/tmp/x")

	ctx := context.Background()
	allowed := map[string]bool{"HOME": true, "USER": true, "TMPDIR": true, "LANG": true, "LC_ALL": true}
	for _, cmd := range []*exec.Cmd{
		openCmd(ctx, "/A.app", "/p"), quitCmd(ctx), pgrepCmd(ctx, 1), psArgsCmd(ctx, 1), psEtimeCommCmd(ctx, 1),
		sysctlBoottimeCmd(ctx), ioregIdleCmd(ctx), plutilBundleIDCmd(ctx, "/A.app"), caffeinateCmd(ctx, 1),
	} {
		for _, kv := range cmd.Env {
			k := kv[:strings.IndexByte(kv, '=')]
			if !allowed[k] {
				t.Errorf("%s carries a disallowed env var %q (a planted %q must never reach the child)", cmd.Path, k, k)
			}
		}
		if !containsEnv(cmd.Env, "HOME=/Users/x") {
			t.Errorf("%s dropped the allowlisted HOME", cmd.Path)
		}
	}
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// psPidArg returns the pid argument of a `ps -p <pid> …` command.
func psPidArg(cmd *exec.Cmd) string {
	for i, a := range cmd.Args {
		if a == "-p" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}

func hasArg(cmd *exec.Cmd, want string) bool {
	for _, a := range cmd.Args {
		if a == want {
			return true
		}
	}
	return false
}

// covers: MA-553, MA-554
// The macOS realization of running-detection (signal B) and the §4.4.3 identity
// fields: pgrep names the pids, and ps gives each a start time (Now minus
// etime, UTC), an executable path (comm, a full path) and a command line (which
// carries -profile). Driven through the fake exec seam on canned output, so no
// system tool runs. A process whose ps reads fail is skipped, never guessed.
func TestMacThunderbirdProcs(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	e := newMacEnv("/Applications/Thunderbird.app")
	e.clock = func() time.Time { return now }
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		switch {
		case cmd.Path == "/usr/bin/pgrep":
			return "4242\n4243\n", "", nil
		case cmd.Path == "/bin/ps" && hasArg(cmd, "etime="):
			switch psPidArg(cmd) {
			case "4242":
				return "  01:00:00 /Applications/Thunderbird.app/Contents/MacOS/thunderbird\n", "", nil
			case "4243":
				return "10-02:03:04 /Applications/Thunderbird.app/Contents/MacOS/thunderbird\n", "", nil
			}
		case cmd.Path == "/bin/ps" && hasArg(cmd, "args="):
			switch psPidArg(cmd) {
			case "4242":
				return "/Applications/Thunderbird.app/Contents/MacOS/thunderbird -profile /Users/x/P\n", "", nil
			case "4243":
				return "/Applications/Thunderbird.app/Contents/MacOS/thunderbird\n", "", nil
			}
		}
		return "", "", fmt.Errorf("unexpected cmd %v", cmd.Args)
	}

	procs := e.thunderbirdProcs()
	if len(procs) != 2 {
		t.Fatalf("thunderbirdProcs = %d procs, want 2: %+v", len(procs), procs)
	}
	if procs[0].pid != 4242 {
		t.Errorf("proc[0].pid = %d, want 4242", procs[0].pid)
	}
	if procs[0].exe != "/Applications/Thunderbird.app/Contents/MacOS/thunderbird" {
		t.Errorf("proc[0].exe = %q", procs[0].exe)
	}
	if want := now.Add(-time.Hour); !procs[0].start.Equal(want) {
		t.Errorf("proc[0].start = %v, want %v (Now minus 01:00:00)", procs[0].start, want)
	}
	if !procs[0].start.Equal(procs[0].start.UTC()) {
		t.Errorf("proc[0].start is not UTC: %v", procs[0].start)
	}
	if !strings.Contains(procs[0].args, "-profile /Users/x/P") {
		t.Errorf("proc[0].args = %q, want it to carry -profile", procs[0].args)
	}
	if want := now.Add(-(10*24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second)); !procs[1].start.Equal(want) {
		t.Errorf("proc[1].start = %v, want %v (Now minus 10-02:03:04)", procs[1].start, want)
	}
}

// covers: MA-553
// A process whose ps etime/comm cannot be parsed is dropped from the list, never
// admitted with guessed fields (nothing is ever quit on a guess).
func TestMacThunderbirdProcsSkipsUnparsable(t *testing.T) {
	e := newMacEnv("/Applications/Thunderbird.app")
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		switch {
		case cmd.Path == "/usr/bin/pgrep":
			return "7\n", "", nil
		case cmd.Path == "/bin/ps":
			return "", "", fmt.Errorf("ps: no such process")
		}
		return "", "", fmt.Errorf("unexpected")
	}
	if procs := e.thunderbirdProcs(); len(procs) != 0 {
		t.Errorf("a process with unreadable ps output was admitted: %+v", procs)
	}
}

// covers: MA-553
// lockPID reads the profile lock symlink (signal A): a parseable target gives
// its pid; no lock gives present=false; an unparseable target is present with
// pid 0 (treated as held). pidAlive/procExe go through ps (no signal is sent).
func TestMacRunningProbes(t *testing.T) {
	dir := t.TempDir()
	e := newMacEnv("/Applications/Thunderbird.app")

	// No lock symlink.
	if pid, present := e.lockPID(dir); present || pid != 0 {
		t.Errorf("no-lock lockPID = (%d, %v), want (0, false)", pid, present)
	}
	// A parseable lock.
	if err := os.Symlink("192.168.1.9:+2759488", filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if pid, present := e.lockPID(dir); !present || pid != 2759488 {
		t.Errorf("lockPID = (%d, %v), want (2759488, true)", pid, present)
	}
	// An unparseable target: present, pid 0.
	dir2 := t.TempDir()
	if err := os.Symlink("not-a-pid", filepath.Join(dir2, "lock")); err != nil {
		t.Fatal(err)
	}
	if pid, present := e.lockPID(dir2); !present || pid != 0 {
		t.Errorf("unparseable lockPID = (%d, %v), want (0, true)", pid, present)
	}

	// pidAlive: ps success = alive, ps error = dead, pid<=0 = dead without a run.
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		if psPidArg(cmd) == "100" {
			return "/some/proc\n", "", nil
		}
		return "", "", fmt.Errorf("no such process")
	}
	if !e.pidAlive(100) {
		t.Error("pidAlive(100) = false, want true")
	}
	if e.pidAlive(200) {
		t.Error("pidAlive(200) = true, want false")
	}
	ran := false
	e.run = func(cmd *exec.Cmd) (string, string, error) { ran = true; return "", "", nil }
	if e.pidAlive(0) || ran {
		t.Error("pidAlive(0) must be false without running ps")
	}

	// procExe resolves comm from ps etime/comm.
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		return "  00:05 /usr/bin/python3\n", "", nil
	}
	if exe, ok := e.procExe(55); !ok || exe != "/usr/bin/python3" {
		t.Errorf("procExe = (%q, %v), want (/usr/bin/python3, true)", exe, ok)
	}
}

// covers: MA-558
// bootTime parses the integer seconds from sysctl kern.boottime — and the \b in
// the pattern does not mis-read the "sec" inside "usec".
func TestMacBootTime(t *testing.T) {
	e := newMacEnv("/Applications/Thunderbird.app")
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		return "{ sec = 1699564800, usec = 123456 } Thu Nov  9 12:00:00 2023\n", "", nil
	}
	if got := e.bootTime(); got != 1699564800 {
		t.Errorf("bootTime = %d, want 1699564800", got)
	}
	// A failed read yields 0 (recovery then treats the record as a different boot).
	e.run = func(cmd *exec.Cmd) (string, string, error) { return "", "", fmt.Errorf("boom") }
	if got := e.bootTime(); got != 0 {
		t.Errorf("bootTime on error = %d, want 0", got)
	}
}

// covers: MA-554
// parseEtime turns a ps elapsed-time field into a duration, from which the §4.4.3
// start time (Now minus etime) is derived; a malformed field is rejected so no
// start time is guessed.
func TestMacParseEtime(t *testing.T) {
	ok := []struct {
		in   string
		want time.Duration
	}{
		{"05:23", 5*time.Minute + 23*time.Second},
		{"01:05:23", time.Hour + 5*time.Minute + 23*time.Second},
		{"2-01:05:23", 2*24*time.Hour + time.Hour + 5*time.Minute + 23*time.Second},
		{"  00:00  ", 0},
	}
	for _, c := range ok {
		got, valid := parseEtime(c.in)
		if !valid || got != c.want {
			t.Errorf("parseEtime(%q) = (%v, %v), want (%v, true)", c.in, got, valid, c.want)
		}
	}
	for _, bad := range []string{"", "nope", "1:2:3:4", "01:99", "x-01:00:00"} {
		if _, valid := parseEtime(bad); valid {
			t.Errorf("parseEtime(%q) accepted a malformed field", bad)
		}
	}
}

// covers: MA-557
// realRun captures stdout and is bounded by its command's context: a command
// that would run far longer than its deadline is stopped (the Go runtime ends
// OUR helper when the context fires — there is no Kill/Signal in the source),
// and realRun returns promptly with an error rather than hanging the pre-sync.
func TestRealRunCapturesAndIsBounded(t *testing.T) {
	out, _, err := realRun(exec.Command("/bin/echo", "hello"))
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Errorf("realRun(echo) = (%q, %v), want (hello, nil)", out, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err = realRun(exec.CommandContext(ctx, "/bin/sleep", "10"))
	if err == nil {
		t.Error("realRun of a 10s sleep with a 50ms deadline returned nil error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("realRun did not honour the deadline: took %v", elapsed)
	}
}

// covers: MA-571
// startHidden opens the resolved .app BUNDLE hidden (open -g -j), not the inner
// executable; a launch failure becomes errNoDesktopSession and open is called
// exactly once (no retry loop).
func TestMacStartHidden(t *testing.T) {
	e := newMacEnv("/Applications/Thunderbird.app")
	const innerExe = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"

	calls := 0
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		if cmd.Path != "/usr/bin/open" {
			return "", "", fmt.Errorf("unexpected cmd %v", cmd.Args)
		}
		calls++
		return "", "", fmt.Errorf("LSOpenURLsWithRole() failed: the window server is unreachable")
	}
	if err := e.startHidden(innerExe, "/Users/x/P"); !errors.Is(err, errNoDesktopSession) {
		t.Errorf("startHidden on a launch failure = %v, want errNoDesktopSession", err)
	}
	if calls != 1 {
		t.Errorf("open called %d times, want exactly 1 (no retry loop)", calls)
	}

	var gotArgs []string
	e.run = func(cmd *exec.Cmd) (string, string, error) { gotArgs = cmd.Args; return "", "", nil }
	if err := e.startHidden(innerExe, "/Users/x/P"); err != nil {
		t.Errorf("startHidden on a working open = %v, want nil", err)
	}
	want := []string{"/usr/bin/open", "-g", "-j", "/Applications/Thunderbird.app", "--args", "-profile", "/Users/x/P"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("startHidden ran %v, want %v (the .app bundle, hidden)", gotArgs, want)
	}
}

// covers: MA-570
// The osascript quit result is classified correctly — success and -600 (already
// gone) are nil; -1743, -1712 and our own deadline are errQuitNotPermitted — and
// quit() makes exactly ONE osascript request.
func TestMacQuitClassify(t *testing.T) {
	for _, c := range []struct {
		name   string
		stderr string
		err    error
		want   error
	}{
		{"success", "", nil, nil},
		{"gone -600", "execution error: ... (-600)", fmt.Errorf("exit status 1"), nil},
		{"not-authorized -1743", "execution error: Not authorized to send Apple events (-1743)", fmt.Errorf("exit status 1"), errQuitNotPermitted},
		{"apple-event-timeout -1712", "execution error: AppleEvent timed out (-1712)", fmt.Errorf("exit status 1"), errQuitNotPermitted},
		{"our deadline", "", fmt.Errorf("signal: killed"), errQuitNotPermitted},
	} {
		if got := classifyQuitErr(c.err, c.stderr); !errors.Is(got, c.want) {
			t.Errorf("%s: classifyQuitErr = %v, want %v", c.name, got, c.want)
		}
	}

	calls := 0
	e := newMacEnv("/Applications/Thunderbird.app")
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		if cmd.Path != "/usr/bin/osascript" {
			return "", "", fmt.Errorf("unexpected cmd %v", cmd.Args)
		}
		calls++
		return "", "execution error: Not authorized to send Apple events (-1743)", fmt.Errorf("exit status 1")
	}
	if err := e.quit(proc{pid: 4242}); !errors.Is(err, errQuitNotPermitted) {
		t.Errorf("quit() = %v, want errQuitNotPermitted", err)
	}
	if calls != 1 {
		t.Errorf("osascript called %d times, want exactly 1", calls)
	}
}

// covers: MA-579
// idleSeconds reads HIDIdleTime (nanoseconds) from ioreg into seconds; output
// without it, and an ioreg error, are both unreadable (ok=false, which S4/S8
// count as the owner being active).
func TestMacIdleSeconds(t *testing.T) {
	e := newMacEnv("/Applications/Thunderbird.app")

	e.run = func(cmd *exec.Cmd) (string, string, error) {
		return "+-o IOHIDSystem  <class IOHIDSystem>\n    {\n      \"HIDIdleTime\" = 5000000000\n      \"HIDPointerAccelerationType\" = 0\n    }\n", "", nil
	}
	if s, ok := e.idleSeconds(); !ok || s != 5.0 {
		t.Errorf("idleSeconds = (%v, %v), want (5, true)", s, ok)
	}

	e.run = func(cmd *exec.Cmd) (string, string, error) { return "no idle field here\n", "", nil }
	if s, ok := e.idleSeconds(); ok {
		t.Errorf("idleSeconds without HIDIdleTime = (%v, %v), want ok=false", s, ok)
	}

	e.run = func(cmd *exec.Cmd) (string, string, error) { return "", "", fmt.Errorf("ioreg failed") }
	if _, ok := e.idleSeconds(); ok {
		t.Error("idleSeconds on an ioreg error returned ok=true")
	}
}
