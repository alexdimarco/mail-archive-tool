//go:build darwin

package presync

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// covers: MA-557
// Every macOS helper command uses an absolute path (so exec never does a PATH
// lookup), a fixed argument list (no shell, no -c, no value interpolated into a
// script — the osascript quit is a constant), and the argv catalog equals this
// golden list (design §4.7.1, §5.4). Asserted on the built *exec.Cmd WITHOUT
// running it, so nothing launches, signals or touches Thunderbird.
func TestMacCommandArgvGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  *exec.Cmd
		want []string
	}{
		{"open", openCmd("/Applications/Thunderbird.app", "/Users/x/p"), []string{"/usr/bin/open", "-g", "-j", "/Applications/Thunderbird.app", "--args", "-profile", "/Users/x/p"}},
		{"quit", quitCmd(), []string{"/usr/bin/osascript", "-e", quitScript}},
		{"pgrep", pgrepCmd(501), []string{"/usr/bin/pgrep", "-x", "-u", "501", "thunderbird"}},
		{"ps-args", psArgsCmd(9), []string{"/bin/ps", "-p", "9", "-o", "args="}},
		{"ps-etime", psEtimeCommCmd(9), []string{"/bin/ps", "-p", "9", "-o", "etime=", "-o", "comm="}},
		{"sysctl", sysctlBoottimeCmd(), []string{"/usr/sbin/sysctl", "-n", "kern.boottime"}},
		{"ioreg", ioregIdleCmd(), []string{"/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4"}},
		{"plutil", plutilBundleIDCmd("/A.app"), []string{"/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", "/A.app/Contents/Info.plist"}},
		{"caffeinate", caffeinateCmd(9), []string{"/usr/bin/caffeinate", "-i", "-w", "9"}},
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

	allowed := map[string]bool{"HOME": true, "USER": true, "TMPDIR": true, "LANG": true, "LC_ALL": true}
	for _, cmd := range []*exec.Cmd{
		openCmd("/A.app", "/p"), quitCmd(), pgrepCmd(1), psArgsCmd(1), psEtimeCommCmd(1),
		sysctlBoottimeCmd(), ioregIdleCmd(), plutilBundleIDCmd("/A.app"), caffeinateCmd(1),
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
