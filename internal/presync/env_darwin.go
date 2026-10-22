//go:build darwin

package presync

import (
	"os"
	"os/exec"
	"strconv"
)

// This file is the macOS adapter's command layer (design §4.7.1): one builder
// per helper, each naming an ABSOLUTE path (no PATH lookup, §5.4 rule 7), a
// fixed argument list (no shell, no -c, no value interpolated into a script),
// and the allowlist environment (§5.5). Tests assert every builder's argv and
// env WITHOUT running it, so nothing launches or signals Thunderbird in a test.

// allowlistEnvKeys are the only variables copied from our own environment into a
// macOS helper's child (design §5.5). DYLD_*, LD_*, MOZ_*, GTK_*, PATH and every
// other variable are dropped, so a planted DYLD_INSERT_LIBRARIES or MOZ_* can
// never reach Thunderbird.
var allowlistEnvKeys = []string{"HOME", "USER", "TMPDIR", "LANG"}

func allowlistEnv() []string {
	env := make([]string, 0, len(allowlistEnvKeys))
	for _, k := range allowlistEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// psEnv is the allowlist plus LC_ALL=C, for the ps helpers only, so etime and
// args parse in a known locale (design §4.7.1, §5.5).
func psEnv() []string { return append(allowlistEnv(), "LC_ALL=C") }

// quitScript is the CONSTANT AppleScript that asks Thunderbird to quit through
// its own quit request (design §4.7.1). Nothing is interpolated into it, and the
// "is running" guard means the request can never launch Thunderbird.
const quitScript = `if application id "org.mozilla.thunderbird" is running then tell application id "org.mozilla.thunderbird" to quit`

func openCmd(app, profile string) *exec.Cmd {
	c := exec.Command("/usr/bin/open", "-g", "-j", app, "--args", "-profile", profile)
	c.Env = allowlistEnv()
	return c
}

func quitCmd() *exec.Cmd {
	c := exec.Command("/usr/bin/osascript", "-e", quitScript)
	c.Env = allowlistEnv()
	return c
}

func pgrepCmd(uid int) *exec.Cmd {
	c := exec.Command("/usr/bin/pgrep", "-x", "-u", strconv.Itoa(uid), "thunderbird")
	c.Env = allowlistEnv()
	return c
}

func psArgsCmd(pid int) *exec.Cmd {
	c := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "args=")
	c.Env = psEnv()
	return c
}

func psEtimeCommCmd(pid int) *exec.Cmd {
	c := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "etime=", "-o", "comm=")
	c.Env = psEnv()
	return c
}

func sysctlBoottimeCmd() *exec.Cmd {
	c := exec.Command("/usr/sbin/sysctl", "-n", "kern.boottime")
	c.Env = allowlistEnv()
	return c
}

func ioregIdleCmd() *exec.Cmd {
	c := exec.Command("/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4")
	c.Env = allowlistEnv()
	return c
}

func plutilBundleIDCmd(app string) *exec.Cmd {
	c := exec.Command("/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", app+"/Contents/Info.plist")
	c.Env = allowlistEnv()
	return c
}

func caffeinateCmd(pid int) *exec.Cmd {
	c := exec.Command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(pid))
	c.Env = allowlistEnv()
	return c
}
