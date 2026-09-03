// Package schedule generates and installs a recurring-backup entry for the host
// OS's scheduler (cron on Linux, launchd on macOS, Task Scheduler on Windows).
//
// Generation is pure and unit-testable on any OS: the Cron/Launchd/Schtasks
// helpers return the exact text that would be applied, and the crontab-editing
// helpers (UpsertCronBlock/RemoveCronBlock) are string transforms. Only Install
// and Remove touch the machine, and they isolate the side effect behind small
// per-OS functions.
package schedule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DefaultName is the scheduler entry name used when the operator does not choose
// one. It also tags the managed crontab block so install stays idempotent.
const DefaultName = "mailarchive-backup"

// Interval is how often a scheduled backup runs.
type Interval string

const (
	Hourly Interval = "hourly"
	Daily  Interval = "daily"
	Weekly Interval = "weekly"
)

// ParseInterval validates and normalizes an interval string (empty → daily).
func ParseInterval(s string) (Interval, error) {
	switch Interval(strings.ToLower(strings.TrimSpace(s))) {
	case Hourly:
		return Hourly, nil
	case "", Daily:
		return Daily, nil
	case Weekly:
		return Weekly, nil
	default:
		return "", fmt.Errorf("invalid -interval %q (want hourly, daily, or weekly)", s)
	}
}

// Spec describes a scheduled backup: which mailarchive executable to run, the
// export flags to pass it, and when to run it.
type Spec struct {
	Name     string   // scheduler entry name / marker (e.g. "mailarchive-1a2b3c4d")
	Interval Interval // hourly | daily | weekly
	At       string   // "HH:MM"; hourly uses only the minute
	Exe      string   // absolute path to the program to run
	Args     []string // the job's arguments, e.g. ["-out","/data","-mode","incremental","-log","/data/x.log"]
	Log      string   // the job's operator log (the job writes it via -log); its .stderr.log sibling catches crashes

	// Wrapper (Windows): run through a batch wrapper at WrapperPath instead of
	// the bare command, so any path length/characters work and stderr has a
	// sink. The CLI always sets it on Windows.
	Wrapper     bool
	WrapperPath string

	// Out is the archive directory; Install writes the descriptor there and
	// Remove deletes it.
	Out string
}

// Validate checks the fields code generation depends on.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("schedule name is empty")
	}
	if strings.TrimSpace(s.Exe) == "" {
		return errors.New("could not determine the mailarchive executable path")
	}
	if _, err := ParseInterval(string(s.Interval)); err != nil {
		return err
	}
	_, _, err := parseHHMM(s.At)
	return err
}

func parseHHMM(s string) (hour, min int, err error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid -at %q (want HH:MM, e.g. 02:00)", s)
	}
	hour, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid -at %q (hour must be 00-23)", s)
	}
	min, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("invalid -at %q (minute must be 00-59)", s)
	}
	return hour, min, nil
}

// program is the executable followed by its arguments.
func (s Spec) program() []string { return append([]string{s.Exe}, s.Args...) }

// ---- cron (Linux) ---------------------------------------------------------

// CronMarker tags our managed crontab block so a repeated install replaces it
// (idempotent) and a remove finds it precisely. The default name isn't repeated
// (no "# mailarchive-backup mailarchive-backup"); a custom name is appended.
func CronMarker(name string) string {
	if name == DefaultName {
		return "# " + DefaultName
	}
	return "# " + DefaultName + " " + name
}

func cronSchedule(iv Interval, hour, min int) string {
	switch iv {
	case Hourly:
		return fmt.Sprintf("%d * * * *", min)
	case Weekly:
		return fmt.Sprintf("%d %d * * 0", min, hour) // Sunday
	default: // Daily
		return fmt.Sprintf("%d %d * * *", min, hour)
	}
}

// CronLine returns the crontab command line (schedule + command), without the
// marker comment. The job writes its own log (-log); nothing is redirected, so
// anything the program cannot log itself — a crash, a refusal to start —
// reaches cron's mail (MAILTO), the one push channel that needs no setup.
func CronLine(s Spec) (string, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return "", err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return "", err
	}
	return cronSchedule(iv, hour, min) + " " + shellJoin(s.program()), nil
}

// CronBlock is the marker comment plus its command line — the two-line unit that
// UpsertCronBlock inserts and RemoveCronBlock deletes.
func CronBlock(s Spec) (string, error) {
	line, err := CronLine(s)
	if err != nil {
		return "", err
	}
	return CronMarker(s.Name) + "\n" + line, nil
}

// UpsertCronBlock returns existing with block (a marker line + its command line)
// present exactly once: any prior block with the same marker is removed first,
// so applying the same spec repeatedly yields a single block.
func UpsertCronBlock(existing, marker, block string) string {
	stripped := RemoveCronBlock(existing, marker)
	return stripped + block + "\n"
}

// RemoveCronBlock returns existing with the marker line and the single command
// line that immediately follows it removed. Unrelated lines are preserved.
func RemoveCronBlock(existing, marker string) string {
	if existing == "" {
		return ""
	}
	lines := strings.Split(existing, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			if i+1 < len(lines) {
				i++ // also drop the command line belonging to this marker
			}
			continue
		}
		out = append(out, lines[i])
	}
	res := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if res == "" {
		return ""
	}
	return res + "\n"
}

// ---- launchd (macOS) ------------------------------------------------------

// LaunchdLabel is the reverse-DNS label / plist basename for a job.
func LaunchdLabel(name string) string { return "com.dimarcotech.mailarchive." + name }

// LaunchdPlist returns the LaunchAgent plist XML for the spec.
func LaunchdPlist(s Spec) (string, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return "", err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return "", err
	}

	var args strings.Builder
	for _, a := range s.program() {
		args.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}

	var cal strings.Builder
	cal.WriteString("    <key>Minute</key>\n    <integer>" + strconv.Itoa(min) + "</integer>\n")
	if iv != Hourly {
		cal.WriteString("    <key>Hour</key>\n    <integer>" + strconv.Itoa(hour) + "</integer>\n")
	}
	if iv == Weekly {
		cal.WriteString("    <key>Weekday</key>\n    <integer>0</integer>\n")
	}

	// The job logs itself (-log); launchd only needs a sink for what the
	// program cannot log — a crash or a failed start.
	var logKeys string
	if strings.TrimSpace(s.Log) != "" {
		logKeys = "  <key>StandardErrorPath</key>\n  <string>" + xmlEscape(StderrLogPath(s.Log)) + "</string>\n"
	}

	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + LaunchdLabel(s.Name) + `</string>
  <key>ProgramArguments</key>
  <array>
` + args.String() + `  </array>
  <key>StartCalendarInterval</key>
  <dict>
` + cal.String() + `  </dict>
` + logKeys + `  <key>RunAtLoad</key>
  <false/>
</dict>
</plist>
`, nil
}

func launchdPlistPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel(name)+".plist")
}

// ---- Task Scheduler (Windows) --------------------------------------------

func schtasksSC(iv Interval) string {
	switch iv {
	case Hourly:
		return "HOURLY"
	case Weekly:
		return "WEEKLY"
	default:
		return "DAILY"
	}
}

// SchtasksCreateCmd returns the schtasks command line that Install would run
// (for previewing). The installer builds the argv directly rather than parsing
// this string.
func SchtasksCreateCmd(s Spec) (string, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return "", err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return "", err
	}
	tr := taskRun(s)
	if s.Wrapper {
		tr = winQuote(s.WrapperPath)
	}
	cmd := fmt.Sprintf("schtasks /Create /TN %s /TR %s /SC %s /ST %02d:%02d",
		winQuote(s.Name), winQuote(tr), schtasksSC(iv), hour, min)
	if iv == Weekly {
		cmd += " /D SUN"
	}
	return cmd + " /F", nil
}

// SchtasksDeleteCmd returns the schtasks command that removes the task.
func SchtasksDeleteCmd(name string) string {
	return "schtasks /Delete /TN " + winQuote(name) + " /F"
}

// ---- OS-aware preview / install / remove ----------------------------------

// Preview returns the exact scheduler entry Install would apply on the current
// OS, without applying it.
func Preview(s Spec) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		plist, err := LaunchdPlist(s)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("launchd LaunchAgent — would be written to:\n  %s\n\n%s", launchdPlistPath(s.Name), plist), nil
	case "windows":
		cmd, err := SchtasksCreateCmd(s)
		if err != nil {
			return "", err
		}
		out := "Windows Task Scheduler — would run:\n\n  " + cmd + "\n"
		if s.Wrapper {
			out += "\nwith the wrapper " + s.WrapperPath + " containing:\n\n" + CmdWrapper(s)
		}
		return out, nil
	default:
		block, err := CronBlock(s)
		if err != nil {
			return "", err
		}
		return "cron — would be added to your user crontab:\n\n" + block + "\n", nil
	}
}

// Install applies the schedule to the host OS's scheduler and records the
// archive-local descriptor. On Windows the wrapper is written (and fsynced)
// BEFORE the task exists, so an interrupted install leaves at most an orphan
// wrapper and never a task pointing at nothing.
func Install(s Spec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = installLaunchd(s)
	case "windows":
		if s.Wrapper {
			if werr := writeWrapper(s); werr != nil {
				return werr
			}
		}
		err = installSchtasks(s)
	default:
		err = installCron(s)
	}
	if err != nil {
		return err
	}
	if s.Out != "" {
		if derr := WriteDescriptor(s.Out, DescribeSpec(s)); derr != nil {
			return fmt.Errorf("schedule installed, but its descriptor could not be written: %w", derr)
		}
	}
	return nil
}

// Remove uninstalls a previously installed schedule by name, then its wrapper
// and descriptor.
func Remove(s Spec) error {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = removeLaunchd(s)
	case "windows":
		err = removeSchtasks(s)
		if err == nil && s.WrapperPath != "" {
			if rerr := os.Remove(s.WrapperPath); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				err = rerr
			}
		}
	default:
		err = removeCron(s)
	}
	if err != nil {
		return err
	}
	if s.Out != "" {
		return RemoveDescriptor(s.Out)
	}
	return nil
}

// writeWrapper writes the batch wrapper atomically and fsynced.
func writeWrapper(s Spec) error {
	dir := filepath.Dir(s.WrapperPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".wrapper-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(CmdWrapper(s)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.WrapperPath)
}

// DefaultLogPath returns where a scheduled run writes its operator log.
func DefaultLogPath(out, name string) string {
	return filepath.Join(out, name+".log")
}

func installCron(s Spec) error {
	block, err := CronBlock(s)
	if err != nil {
		return err
	}
	return writeCrontab(UpsertCronBlock(readCrontab(), CronMarker(s.Name), block))
}

func removeCron(s Spec) error {
	return writeCrontab(RemoveCronBlock(readCrontab(), CronMarker(s.Name)))
}

func readCrontab() string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "" // no crontab yet (or none for this user)
	}
	return string(out)
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab -: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installLaunchd(s Spec) error {
	plist, err := LaunchdPlist(s)
	if err != nil {
		return err
	}
	p := launchdPlistPath(s.Name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", p).Run() // ignore: may not be loaded
	if out, err := exec.Command("launchctl", "load", p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeLaunchd(s Spec) error {
	p := launchdPlistPath(s.Name)
	_ = exec.Command("launchctl", "unload", p).Run()
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SchtasksCreateArgv returns the exact argument vector Install hands to
// schtasks.exe (without the program name itself).
func SchtasksCreateArgv(s Spec) ([]string, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return nil, err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return nil, err
	}
	tr := taskRun(s)
	if s.Wrapper {
		tr = winQuote(s.WrapperPath)
	}
	argv := []string{"/Create", "/TN", s.Name, "/TR", tr, "/SC", schtasksSC(iv), "/ST", fmt.Sprintf("%02d:%02d", hour, min)}
	if iv == Weekly {
		argv = append(argv, "/D", "SUN")
	}
	return append(argv, "/F"), nil
}

func installSchtasks(s Spec) error {
	argv, err := SchtasksCreateArgv(s)
	if err != nil {
		return err
	}
	if out, err := exec.Command("schtasks", argv...).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeSchtasks(s Spec) error {
	if out, err := exec.Command("schtasks", "/Delete", "/TN", s.Name, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---- quoting helpers ------------------------------------------------------

// shellQuote single-quotes s for POSIX sh when it contains anything the shell
// would interpret; a clean token is returned as-is.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`*?[]#&;|<>(){}~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// taskRun is the Task Scheduler "task to run" string: the executable and each
// argument quoted per token, so a path containing spaces ("C:\Program Files\…",
// "OneDrive - Company") is one argument to the scheduled program, not several.
// The program token is always quoted — Task Scheduler takes a leading quoted
// token as the whole program path.
func taskRun(s Spec) string {
	parts := make([]string, 0, 1+len(s.Args))
	parts = append(parts, winQuote(s.Exe))
	for _, a := range s.Args {
		parts = append(parts, winArg(a))
	}
	return strings.Join(parts, " ")
}

// winArg quotes one token for a Windows command line only when it needs it
// (whitespace or a quote); a clean token is returned as-is for readability.
func winArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return winQuote(s)
}

// winQuote always double-quotes s under the Microsoft C-runtime rules the
// receiving program parses its argv with: an embedded quote becomes \", and
// backslashes that precede a quote (or the closing quote) are doubled so they
// stay literal. Ordinary path backslashes are untouched. This is the inverse of
// CommandLineToArgv, so nesting (a quoted run string inside a quoted /TR value)
// round-trips exactly.
func winQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
			b.WriteByte('"')
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			b.WriteByte(c)
		}
		slashes = 0
	}
	b.WriteString(strings.Repeat(`\`, slashes*2)) // trailing backslashes must not escape the closing quote
	b.WriteByte('"')
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
