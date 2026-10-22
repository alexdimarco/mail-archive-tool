//go:build darwin

package presync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/lockfile"
)

// This file is the macOS adapter (design §4.7.1): the command layer, an
// injectable exec seam, the strict parsers for each tool's output, and the
// macEnv realization of the TP1 env interface. Every command names an ABSOLUTE
// path (no PATH lookup, §5.4 rule 7), a fixed argument list (no shell, no -c, no
// value interpolated into a script), the allowlist environment (§5.5), and a
// context whose deadline — when it fires — ends only OUR helper (CommandContext
// does the stop; there is no Kill/Signal in this source, §5.4 rules 1 and 5).

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

func openCmd(ctx context.Context, app, profile string) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/bin/open", "-g", "-j", app, "--args", "-profile", profile)
	c.Env = allowlistEnv()
	return c
}

func quitCmd(ctx context.Context) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", quitScript)
	c.Env = allowlistEnv()
	return c
}

func pgrepCmd(ctx context.Context, uid int) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/bin/pgrep", "-x", "-u", strconv.Itoa(uid), "thunderbird")
	c.Env = allowlistEnv()
	return c
}

func psArgsCmd(ctx context.Context, pid int) *exec.Cmd {
	c := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "args=")
	c.Env = psEnv()
	return c
}

func psEtimeCommCmd(ctx context.Context, pid int) *exec.Cmd {
	c := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "etime=", "-o", "comm=")
	c.Env = psEnv()
	return c
}

func sysctlBoottimeCmd(ctx context.Context) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "kern.boottime")
	c.Env = allowlistEnv()
	return c
}

func ioregIdleCmd(ctx context.Context) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4")
	c.Env = allowlistEnv()
	return c
}

func plutilBundleIDCmd(ctx context.Context, app string) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", app+"/Contents/Info.plist")
	c.Env = allowlistEnv()
	return c
}

func caffeinateCmd(ctx context.Context, pid int) *exec.Cmd {
	c := exec.CommandContext(ctx, "/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(pid))
	c.Env = allowlistEnv()
	return c
}

// probeTimeout bounds every read-only helper (pgrep/ps/sysctl/ioreg), so a stuck
// system tool can never hang the pre-sync (which itself can never gate the
// archive). launchTimeout bounds `open`, which only asks LaunchServices to start
// the app and returns at once; the long wait for the window to appear is the
// state machine's S6, not this call.
const (
	probeTimeout  = 5 * time.Second
	launchTimeout = 15 * time.Second
	quitTimeout   = 20 * time.Second
)

// runCmd runs a built command to completion and returns its stdout and stderr.
// It is the exec seam: production uses realRun; a test replaces it with a fake
// that returns canned output for a given argv, so no system tool runs and
// nothing launches or signals Thunderbird in a test.
type runCmd func(*exec.Cmd) (stdout, stderr string, err error)

// realRun captures stdout/stderr and runs the command. The command already
// carries its context (and thus its deadline); when the deadline fires, the Go
// runtime stops THIS helper — there is no Kill/Signal call here.
func realRun(cmd *exec.Cmd) (string, string, error) {
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// bootSecRe extracts the integer seconds from `sysctl -n kern.boottime` output
// such as "{ sec = 1699564800, usec = 0 } Thu Nov  9 ...". The \b stops it
// matching the "sec" inside "usec".
var bootSecRe = regexp.MustCompile(`\bsec\s*=\s*(\d+)`)

func parseBootTimeSec(out string) (int64, bool) {
	m := bootSecRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// idleNanosRe extracts the HIDIdleTime (nanoseconds since the last keyboard or
// mouse input) from `ioreg -c IOHIDSystem -d 4`; the first entry is the system
// idle time. An output without it is unparseable, which S4/S8 count as active.
var idleNanosRe = regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)

func parseIdleSeconds(out string) (float64, bool) {
	m := idleNanosRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(ns) / 1e9, true
}

// parsePgrepPIDs reads the newline-separated pids pgrep prints.
func parsePgrepPIDs(out string) []int {
	var pids []int
	for _, f := range strings.Fields(out) {
		if p, err := strconv.Atoi(f); err == nil && p > 0 {
			pids = append(pids, p)
		}
	}
	return pids
}

// parsePsEtimeComm splits one `ps -p <pid> -o etime= -o comm=` line into the
// elapsed-time field and the executable path (comm, a full path on macOS). A
// path may contain spaces, so comm is everything after the first field.
func parsePsEtimeComm(out string) (etime, comm string, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			continue
		}
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			return "", "", false
		}
		etime = line[:i]
		comm = strings.TrimSpace(line[i:])
		if etime == "" || comm == "" {
			return "", "", false
		}
		return etime, comm, true
	}
	return "", "", false
}

// parseEtime turns a ps etime field ("MM:SS", "HH:MM:SS" or "DD-HH:MM:SS") into
// a duration. The process start time is Now() minus this (design §4.7.1; lstart
// is not used, it is zoneless and ambiguous across a DST change).
func parseEtime(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	days := 0
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, err := strconv.Atoi(s[:i])
		if err != nil || d < 0 {
			return 0, false
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	var h, m, sec int
	var err error
	switch len(parts) {
	case 3:
		if h, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if m, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
		if sec, err = strconv.Atoi(parts[2]); err != nil {
			return 0, false
		}
	case 2:
		if m, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if sec, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if h < 0 || m < 0 || m >= 60 || sec < 0 || sec >= 60 {
		return 0, false
	}
	return time.Duration(days)*24*time.Hour + time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
}

// parseMozLockPID extracts the pid from a Mozilla profile lock target such as
// "127.0.1.1:+2759488", "hostname:12345" or a bare "+2759488" (the same format
// internal/thunderbird reads; kept local so this security-reviewed package is
// self-contained). Returns 0 when the target names no parseable pid.
func parseMozLockPID(target string) int {
	s := target
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return pid
}

// macEnv is the real macOS env (design §4.7.1). It holds only the current uid
// (for pgrep), the resolved .app bundle (for the launcher), the exec seam and
// the clock; the per-profile session lock is held between acquireLock and
// releaseLock. It never reads a setting, path or pid from the archive directory.
type macEnv struct {
	uid    int
	bundle string // the resolved /Applications/Thunderbird.app, for startHidden
	run    runCmd
	clock  func() time.Time
	lock   *lockfile.Lock
}

func newMacEnv(bundle string) *macEnv {
	return &macEnv{uid: os.Getuid(), bundle: bundle, run: realRun, clock: time.Now}
}

// macEnv is the real macOS realization of the env seam.
var _ env = (*macEnv)(nil)

func (e *macEnv) now() time.Time        { return e.clock() }
func (e *macEnv) sleep(d time.Duration) { time.Sleep(d) }

// digest folds the watch set without opening a file (design §4.4.2).
func (e *macEnv) digest(roots []string) (uint64, bool) { return digestWatch(roots) }

// bootTime reads the machine's boot time in unix seconds (a reboot clears the
// record, design §4.6). 0 on any failure — recovery then treats the record as
// from a different boot and clears it, which is the safe reading.
func (e *macEnv) bootTime() int64 {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _, err := e.run(sysctlBoottimeCmd(ctx))
	if err != nil {
		return 0
	}
	sec, _ := parseBootTimeSec(out)
	return sec
}

// lockPID reads the profile lock symlink: present is false when there is no
// lock; pid is 0 when the lock names no parseable pid (design §4.4.1 signal A).
func (e *macEnv) lockPID(profile string) (int, bool) {
	target, err := os.Readlink(filepath.Join(profile, "lock"))
	if err != nil {
		return 0, false
	}
	return parseMozLockPID(target), true
}

// pidAlive reports whether pid is a live process — via `ps -p`, which sends no
// signal (so §5.4 rule 1 holds even for a liveness probe).
func (e *macEnv) pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_, _, err := e.run(psArgsCmd(ctx, pid))
	return err == nil
}

// procExe resolves a live pid's executable path (comm), to tell a stale lock
// whose pid was reused by a non-Thunderbird process (design §4.4.1).
func (e *macEnv) procExe(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _, err := e.run(psEtimeCommCmd(ctx, pid))
	if err != nil {
		return "", false
	}
	_, comm, ok := parsePsEtimeComm(out)
	return comm, ok
}

// thunderbirdProcs lists Thunderbird-named processes run by this user (design
// §4.4.1 signal B / the §4.4.3 ownership candidates): pgrep names the pids, then
// ps gives each one's start time (Now minus etime, UTC), executable path and
// command line (which carries the -profile argument). A process that cannot be
// fully read is skipped, never guessed.
func (e *macEnv) thunderbirdProcs() []proc {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _, err := e.run(pgrepCmd(ctx, e.uid))
	if err != nil { // pgrep exits non-zero when nothing matches
		return nil
	}
	var procs []proc
	for _, pid := range parsePgrepPIDs(out) {
		if p, ok := e.procInfo(ctx, pid); ok {
			procs = append(procs, p)
		}
	}
	return procs
}

func (e *macEnv) procInfo(ctx context.Context, pid int) (proc, bool) {
	etOut, _, err := e.run(psEtimeCommCmd(ctx, pid))
	if err != nil {
		return proc{}, false
	}
	etime, comm, ok := parsePsEtimeComm(etOut)
	if !ok {
		return proc{}, false
	}
	dur, ok := parseEtime(etime)
	if !ok {
		return proc{}, false
	}
	argsOut, _, err := e.run(psArgsCmd(ctx, pid))
	if err != nil {
		return proc{}, false
	}
	return proc{
		pid:   pid,
		exe:   comm,
		start: e.now().Add(-dur).UTC(),
		args:  strings.TrimSpace(argsOut),
	}, true
}

// acquireLock takes the per-profile session lock, one try (a brief grace, no
// long wait): ok is false when another run holds it (busy, not an error).
func (e *macEnv) acquireLock(path string) (bool, error) {
	l, err := lockfile.Acquire(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return false, nil
		}
		return false, err
	}
	e.lock = l
	return true, nil
}

func (e *macEnv) releaseLock(string) {
	if e.lock != nil {
		_ = e.lock.Release()
		e.lock = nil
	}
}

// quit asks Thunderbird to quit through its own quit request (design §4.7.1,
// S8): one osascript call of the constant script, bounded by quitTimeout. It
// never signals or kills Thunderbird — osascript delivers the app's own quit
// request. The result is classified so the machine can tell consent-denied from
// success.
func (e *macEnv) quit() error {
	ctx, cancel := context.WithTimeout(context.Background(), quitTimeout)
	defer cancel()
	_, stderr, err := e.run(quitCmd(ctx))
	return classifyQuitErr(err, stderr)
}

// classifyQuitErr maps the osascript outcome (design §4.7.1): no error is
// success (including the `is running` guard no-op); error code -600 means
// Thunderbird was already gone (success); -1743 (not authorized), -1712 (Apple
// event timed out) and our own deadline all mean the quit did not take, reported
// as errQuitNotPermitted so the owner is pointed at the Automation grant and
// Thunderbird is left running.
func classifyQuitErr(err error, stderr string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(stderr, "-600") {
		return nil
	}
	return errQuitNotPermitted
}

// idleSeconds is how long the owner has been away from keyboard and mouse, and
// whether it could be read (design §4.4.3 / §4.7.1). An unreadable reading
// (ioreg failed, or no HIDIdleTime in the output) is (0, false); S4 and S8 count
// that as the owner being active, so an unreadable idle never starts or quits.
func (e *macEnv) idleSeconds() (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _, err := e.run(ioregIdleCmd(ctx))
	if err != nil {
		return 0, false
	}
	return parseIdleSeconds(out)
}

// startHidden launches Thunderbird out of sight on the profile (design §4.7.1,
// S5): `open -g -j` starts the resolved .app bundle behind every other window
// and does not bring it forward. The `app` argument is the matched inner
// executable the caller uses for identity (§4.4.3); the launcher opens the
// bundle resolved at setup (e.bundle). A launch failure is reported as
// errNoDesktopSession — for a bundle already validated at resolution, `open`
// failing means there is no usable GUI session — and the machine does not retry.
func (e *macEnv) startHidden(app, profile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
	defer cancel()
	if _, _, err := e.run(openCmd(ctx, e.bundle, profile)); err != nil {
		return errNoDesktopSession
	}
	return nil
}
