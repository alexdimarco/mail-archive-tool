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
	"time"
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

	// Out is the archive directory; Install writes the descriptor of its kind
	// there (the backup's, or a verify's own) and Remove deletes the one that
	// records this entry.
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
	if _, _, err := parseHHMM(s.At); err != nil {
		return err
	}
	// A newline or other control character in any argument would inject a
	// line into a crontab or break a plist/batch file; refuse at the source.
	for _, v := range append([]string{s.Name, s.Exe, s.Log, s.Out, s.WrapperPath}, s.Args...) {
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("schedule argument %q contains a control character and cannot be installed", v)
			}
		}
	}
	if s.Wrapper && !filepath.IsAbs(s.WrapperPath) {
		return fmt.Errorf("wrapper path %q must be absolute (the scheduler runs with an unknown working directory)", s.WrapperPath)
	}
	return nil
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

// TaskRunLength is the length of the direct Task Scheduler run string for this
// spec; a caller that wants no wrapper (the GUI, to avoid a console window)
// must fall back to one when this exceeds SchtasksRunLimit (S12). The install
// itself is defined by XML (SchtasksXML), which has no such length limit; this
// remains the GUI's short-vs-long command heuristic.
func (s Spec) TaskRunLength() int { return len(taskRun(s)) }

// SchtasksRunLimit is Task Scheduler's historical cap on a /TR run string; the
// GUI uses it as the short-vs-long threshold for whether to add a wrapper.
const SchtasksRunLimit = 261

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
// Every token is quoted for cron as well as for sh (cronArg), so a % in a path
// cannot end the command early.
func CronLine(s Spec) (string, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return "", err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return "", err
	}
	quoted := make([]string, 0, 1+len(s.Args))
	for _, a := range s.program() {
		quoted = append(quoted, cronArg(a))
	}
	return cronSchedule(iv, hour, min) + " " + strings.Join(quoted, " "), nil
}

// cronArg quotes one token for a crontab command field. cron reads the field
// before sh does: the first unescaped % ends the command (the rest becomes the
// job's stdin), whatever the shell quoting, and only a backslash-escaped \% is
// a literal %. So a token holding a % is single-quoted piece by piece with
// each % written as "\%" between the pieces: cron turns that into "%", which
// sh reads as a literal %. Escaping a % in place would break on a path whose
// own backslash comes right before the % (cron would pair that backslash with
// the escaping one and still cut the command), whereas here the character
// before every \% is always a double quote. A token without % is shellQuote'd
// unchanged.
func cronArg(tok string) string {
	if !strings.Contains(tok, "%") {
		return shellQuote(tok)
	}
	pieces := strings.Split(tok, "%")
	for i, p := range pieces {
		pieces[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	}
	return strings.Join(pieces, `"\%"`)
}

// cronLineArgv returns the argument vector a managed crontab line was written
// for: the five schedule fields are skipped, cron's \% escape is undone, and the
// command is split into words the way sh splits the quoting CronLine writes
// (single quotes, double quotes, backslash escapes). An unescaped % is read as
// the literal % it was meant to be (a block written before % was escaped), so
// an archive's own older block is still recognised as that archive's and a
// re-install replaces it. It returns nil for a line that has no command.
func cronLineArgv(line string) []string {
	rest := strings.TrimLeft(line, " \t")
	for i := 0; i < 5; i++ {
		j := strings.IndexAny(rest, " \t")
		if j < 0 {
			return nil
		}
		rest = strings.TrimLeft(rest[j:], " \t")
	}
	var cmd strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\\' && i+1 < len(rest) {
			if rest[i+1] == '%' {
				cmd.WriteByte('%')
			} else {
				cmd.WriteByte(c)
				cmd.WriteByte(rest[i+1])
			}
			i++
			continue
		}
		cmd.WriteByte(c)
	}
	var argv []string
	var word strings.Builder
	inWord := false
	s := cmd.String()
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			if inWord {
				argv = append(argv, word.String())
				word.Reset()
				inWord = false
			}
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			word.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\\\"$`", s[i+1]) >= 0 {
					i++
				}
				word.WriteByte(s[i])
			}
		case c == '\\' && i+1 < len(s):
			inWord = true
			i++
			word.WriteByte(s[i])
		default:
			inWord = true
			word.WriteByte(c)
		}
	}
	if inWord {
		argv = append(argv, word.String())
	}
	return argv
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

// launchdPreview renders the macOS install for -install-less previewing: a
// readable command line (identical on every OS, so an operator — and a preview
// test — sees the whole command regardless of how launchd's plist splits each
// argument into its own <string>), the plist that would be written, and the same
// operator notes cron and Windows show (the verify lock warning, how a failed run
// surfaces, and the full-mode caveat). It shows argument PATHS only, never secret
// contents.
func launchdPreview(s Spec) (string, error) {
	plist, err := LaunchdPlist(s)
	if err != nil {
		return "", err
	}
	out := "launchd LaunchAgent — " + cadenceGloss(s) + " — would run:\n\n  " + shellJoin(s.program()) + "\n"
	out += "\nwritten to " + launchdPlistPath(s.Name) + ":\n\n" + plist + "\n"
	if isVerifyJob(s) {
		out += VerifyLockWarning + "\n"
		out += "A failed run surfaces only via `mailarchive status` or launchd's logs (a verify's verdict is shown as Last verify).\n"
	} else {
		out += "A failed run surfaces only via `mailarchive status` or launchd's logs.\n"
	}
	if argHasMode(s.Args, "full") {
		out += "-mode full: every scheduled run re-exports everything.\n"
	}
	return out, nil
}

// ---- Task Scheduler (Windows) --------------------------------------------

// SchtasksCreateCmd returns the schtasks command line that Install would run
// (for previewing). The task is registered from an XML definition, so the
// command names the definition file rather than an inline /TR run string; a
// following /Query confirms the task exists. The installer builds the argv
// directly rather than parsing this string.
func SchtasksCreateCmd(s Spec) (string, error) {
	if _, err := ParseInterval(string(s.Interval)); err != nil {
		return "", err
	}
	if _, _, err := parseHHMM(s.At); err != nil {
		return "", err
	}
	xmlPath := SchtasksXMLPath(s.WrapperPath)
	return fmt.Sprintf("schtasks /Create /TN %s /XML %s /F && schtasks /Query /TN %s",
		winQuote(s.Name), winQuote(xmlPath), winQuote(s.Name)), nil
}

// SchtasksDeleteCmd returns the schtasks command that removes the task.
func SchtasksDeleteCmd(name string) string {
	return "schtasks /Delete /TN " + winQuote(name) + " /F"
}

// ---- OS-aware preview / install / remove ----------------------------------

// Preview returns the exact scheduler entry Install would apply on the current
// OS, without applying it. A scheduled verify at the archive's backup time is
// refused here just as Install refuses it, so a preview never shows an entry the
// install would reject.
func Preview(s Spec) (string, error) {
	if _, err := VerifyScheduleNote(s); err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return launchdPreview(s)
	case "windows":
		return schtasksPreview(s, time.Now())
	default:
		return cronPreview(s)
	}
}

// cronPreview renders the crontab entry with a plain-language gloss above the
// raw schedule line and the notes an operator needs: a failed run is visible
// only via `mailarchive status` or cron's MAILTO, and a full-mode job
// re-exports everything on every run (friction #15).
func cronPreview(s Spec) (string, error) {
	block, err := CronBlock(s)
	if err != nil {
		return "", err
	}
	out := "cron — would be added to your user crontab:\n\n# " + cadenceGloss(s) + "\n" + block + "\n"
	if isVerifyJob(s) {
		out += "\n" + VerifyLockWarning + "\n"
		out += "A failed run surfaces only via `mailarchive status` or cron's MAILTO (a verify's verdict is shown as Last verify).\n"
	} else {
		out += "\nA failed run surfaces only via `mailarchive status` or cron's MAILTO.\n"
	}
	if argHasMode(s.Args, "full") {
		out += "-mode full: every scheduled run re-exports everything.\n"
	}
	return out, nil
}

// cadenceGloss renders the spec's cadence and time in plain language, e.g.
// "daily at 02:00" or "weekly on Sunday at 03:30".
func cadenceGloss(s Spec) string {
	iv, _ := ParseInterval(string(s.Interval))
	hh, mm, _ := parseHHMM(s.At)
	switch iv {
	case Hourly:
		return fmt.Sprintf("hourly at :%02d", mm)
	case Weekly:
		return fmt.Sprintf("weekly on Sunday at %02d:%02d", hh, mm)
	default:
		return fmt.Sprintf("daily at %02d:%02d", hh, mm)
	}
}

// argHasMode reports whether the job args set -mode to value (both `-mode V`
// and `-mode=V` spellings).
func argHasMode(args []string, value string) bool {
	for i := 0; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		if a == "mode" && i+1 < len(args) && args[i+1] == value {
			return true
		}
		if a == "mode="+value {
			return true
		}
	}
	return false
}

// VerifyLockWarning cautions that a scheduled verify holds the archive's
// exclusive lock for its whole run, so it must be scheduled away from the backup:
// a backup whose trigger fires during a verify refuses and records nothing.
const VerifyLockWarning = "Note: verify holds the archive lock for its whole run; a backup whose trigger fires meanwhile refuses and records nothing — schedule it away from the backup time."

// isVerifyJob reports whether the spec runs a verify job (its first argument is
// the verify verb).
func isVerifyJob(s Spec) bool { return len(s.Args) > 0 && s.Args[0] == "verify" }

// descriptorIsVerify reports whether a recorded descriptor is itself a verify
// schedule rather than a backup (its job's first argument is the verify verb).
func descriptorIsVerify(d Descriptor) bool { return len(d.Job) > 0 && d.Job[0] == "verify" }

// VerifyScheduleNote returns the caution to print for a scheduled verify job, or
// "" for any non-verify job. It refuses — with a typed error naming the recorded
// backup and the remedy — when the verify would run at the SAME cadence and time
// as the archive's already-recorded backup schedule: a verify holds the archive's
// exclusive lock for its whole run, so scheduling it at the backup's own time
// guarantees the two clash on every overlap (friction #4, R12/R14). Any other
// time returns the warning, so the operator is told about the lock either way.
func VerifyScheduleNote(s Spec) (string, error) {
	if !isVerifyJob(s) {
		return "", nil
	}
	if d, err := ReadDescriptor(s.Out); err == nil && !descriptorIsVerify(d) {
		if d.Interval == string(s.Interval) && d.At == s.At {
			return "", fmt.Errorf("this verify would run %s — the same time as the backup schedule %q recorded for %s; a verify holds the archive lock for its whole run, so the two would clash on every overlap: give the verify a different -at (or -interval) from the backup", cadenceGloss(s), d.Name, s.Out)
		}
	}
	return VerifyLockWarning, nil
}

// Install applies the schedule to the host OS's scheduler and records the
// archive-local descriptor of its kind. On Windows the wrapper is written (and
// fsynced) BEFORE the task exists, so an interrupted install leaves at most an
// orphan wrapper and never a task pointing at nothing. The descriptor is
// written only AFTER the scheduler has the entry, so an install that fails at
// the scheduler records nothing: no descriptor claims an entry that never
// loaded.
func Install(s Spec) error { return installOn(s, runtime.GOOS) }

// installOn is Install for the host scheduler goos selects; the cron and Task
// Scheduler branches run against their seams (crontabList/crontabInstall,
// schtasksRun), so a test can drive any branch's failure on any OS.
func installOn(s Spec, goos string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	// A scheduled verify at the backup's own time is refused; at any other time it
	// is a distinct entry that coexists with the archive's backup schedule, so it
	// bypasses the one-schedule-per-archive descriptor guard and never overwrites
	// the backup's descriptor (friction #4) — but, like any entry, it may not take
	// over a name the archive's backup or another archive's entry already holds.
	if _, err := VerifyScheduleNote(s); err != nil {
		return err
	}
	if err := refuseCollision(s, goos); err != nil {
		return err
	}
	// A descriptor path the write could never take (a directory planted by whoever
	// handed the folder over) is refused before the scheduler has the entry, so the
	// install does not leave an entry nothing records (and nothing, on a retry, a
	// second temp file).
	if s.Out != "" {
		if err := descriptorPathUsable(filepath.Join(s.Out, descriptorFileFor(s))); err != nil {
			return err
		}
	}
	var err error
	switch goos {
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
	// The archive-local descriptors record what is installed, one per kind: the
	// BACKUP's (DescriptorName) for any non-verify job, and the verify's own
	// (VerifyDescriptorName, FB-4) for a verify. Neither kind ever writes the
	// other's.
	if s.Out != "" {
		if isVerifyJob(s) {
			if derr := WriteVerifyDescriptor(s.Out, DescribeSpec(s)); derr != nil {
				return fmt.Errorf("verify schedule %q is installed, but its descriptor could not be written: %w — `mailarchive schedule -name %s -remove -- verify -out %q` takes the entry out; fix the folder and re-run the install to record it", s.Name, derr, s.Name, s.Out)
			}
		} else if derr := WriteDescriptor(s.Out, DescribeSpec(s)); derr != nil {
			return fmt.Errorf("schedule %q is installed, but its descriptor could not be written: %w — `mailarchive schedule -name %s -remove` takes the entry out; fix the folder and re-run the install to record it", s.Name, derr, s.Name)
		}
	}
	return nil
}

// refuseCollision keeps one schedule per archive and one archive per schedule
// name: an archive that already records a schedule under another name, or a
// name that already backs up a different archive (cron: read from the existing
// block's -out), is refused with the remedy named — never silently replaced.
// The descriptor guard is for the archive's backup only (a verify is a second,
// distinctly named entry with a descriptor of its own); the name guard applies
// to every entry, a verify included. On every OS the name guard reads the
// archive's own two descriptors, so neither of the archive's entries takes the
// name the other holds: a verify under the backup's name would replace the
// backup's entry, and a backup under the verify's name the verify's (FB-4).
// Only on cron hosts does it also read the scheduler back (the managed block's
// -out). launchd and Task Scheduler entries are never read back (UJ-6), so two
// archives sharing one explicit -name there are not detected here — an open
// residual (owner ruling FB-4). goos selects the host scheduler.
func refuseCollision(s Spec, goos string) error {
	if s.Out != "" {
		if isVerifyJob(s) {
			// An older release's descriptor may record a verify itself; that is this
			// verify's own record, not the backup's name.
			if d, err := ReadDescriptor(s.Out); err == nil && !descriptorIsVerify(d) && sameName(d.Name, s.Name, goos) {
				return fmt.Errorf("%q is the backup schedule of archive %s: a verify under the same -name would replace it — %s", s.Name, s.Out, otherNameRemedy("verify", s.Name, DefaultNameFor(s.Out)+"-verify"))
			}
		} else {
			if d, err := ReadDescriptor(s.Out); err == nil && descriptorIsVerify(d) {
				// An older release recorded the archive's verify here. It is not a backup
				// to re-install under (G8-AI3): the entry is the verify's, and a backup
				// installed under its name would replace it, so that name is never
				// advised and a backup under it is refused as the clash it is.
				if sameName(d.Name, s.Name, goos) {
					return fmt.Errorf("%q is the verify schedule of archive %s (an older release recorded it in the backup's descriptor): a backup under the same -name would replace it — %s, or remove it first (`mailarchive schedule -name %s -remove -- verify -out %q`)", s.Name, s.Out, otherNameRemedy("backup", s.Name, DefaultNameFor(s.Out)), d.Name, s.Out)
				}
				return fmt.Errorf("archive %s already has a schedule named %q, a verify an older release recorded in the backup's descriptor: remove it first (`mailarchive schedule -out %q -remove`), then install the backup", s.Out, d.Name, s.Out)
			} else if err == nil && !sameName(d.Name, s.Name, goos) {
				return fmt.Errorf("archive %s already has a schedule named %q: remove it first (`mailarchive schedule -out %q -remove`) or re-install with -name %s", s.Out, d.Name, s.Out, d.Name)
			}
			if d, err := ReadVerifyDescriptor(s.Out); err == nil && sameName(d.Name, s.Name, goos) {
				return fmt.Errorf("%q is the verify schedule of archive %s: a backup under the same -name would replace it — %s", s.Name, s.Out, otherNameRemedy("backup", s.Name, DefaultNameFor(s.Out)))
			}
		}
	}
	if goos == "linux" || goos == "freebsd" || goos == "openbsd" || goos == "netbsd" {
		tab := readCrontab()
		if other := cronBlockOut(tab, CronMarker(s.Name)); other != "" && s.Out != "" && other != s.Out {
			return fmt.Errorf("a schedule named %q already backs up %s: choose another -name, or remove that schedule first", s.Name, other)
		}
		// The block says what kind of entry holds the name, so the cross-kind guard
		// holds when the archive's descriptors are gone or were never written (G8-AC6).
		if kind := cronBlockKind(tab, CronMarker(s.Name)); kind != "" && s.Out != "" && cronBlockOut(tab, CronMarker(s.Name)) == s.Out {
			if mine := jobKind(s); kind != mine {
				alt := DefaultNameFor(s.Out)
				if mine == "verify" {
					alt += "-verify"
				}
				return fmt.Errorf("%q is the %s schedule of archive %s (its crontab block): a %s under the same -name would replace it — %s", s.Name, kind, s.Out, mine, otherNameRemedy(mine, s.Name, alt))
			}
		}
	}
	return nil
}

// jobKind is "verify" for a verify job and "backup" for any other.
func jobKind(s Spec) string {
	if isVerifyJob(s) {
		return "verify"
	}
	return "backup"
}

// cronBlockKind returns "verify" or "backup" for the managed block with marker
// (what its command line runs), or "" when absent or unparseable.
func cronBlockKind(crontab, marker string) string {
	lines := strings.Split(crontab, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker && i+1 < len(lines) {
			argv := cronLineArgv(lines[i+1])
			switch {
			case len(argv) > 1 && argv[1] == "verify":
				return "verify"
			case len(argv) > 0:
				return "backup"
			}
		}
	}
	return ""
}

// otherNameRemedy is the way out of a name the archive's other entry already
// holds: another -name, or none at all when the kind's derived default (alt)
// is not the refused name itself.
func otherNameRemedy(kind, name, alt string) string {
	if alt == name {
		return "give the " + kind + " another -name"
	}
	return fmt.Sprintf("give the %s another -name, or leave -name out (it then gets %q)", kind, alt)
}

// cronBlockOut returns the -out argument of the managed block with marker, or
// "" when absent or unparseable. The command line is read back the way cron
// and sh run it (cronLineArgv), so a quoted or %-escaped path reads back as
// the path itself.
func cronBlockOut(crontab, marker string) string {
	lines := strings.Split(crontab, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker && i+1 < len(lines) {
			argv := cronLineArgv(lines[i+1])
			for j, a := range argv {
				if a == "-out" && j+1 < len(argv) {
					return argv[j+1]
				}
			}
		}
	}
	return ""
}

// Remove uninstalls a previously installed schedule by name, then its wrapper,
// its XML definition and each of the archive's descriptors that goes with THIS
// schedule: the backup's descriptor with the backup's entry (descriptorGoesWith),
// the verify descriptor with the verify's, never one with the other's. A
// descriptor naming another schedule (the archive's backup, when a verify entry
// is removed) still describes a running schedule that `status` watches, so it
// is kept. A backup descriptor that exists but cannot be read stays with every
// entry (MA-455, ESC-10), and so does a verify descriptor that cannot be read. The
// caller names a file left so (UnreadableDescriptor, UnreadableVerifyDescriptor
// and their remedies) and `status` keeps reporting it. An entry already gone
// from the host scheduler (taken out by hand) is no error on any OS, so a
// recorded-but-gone entry's removal still clears its record.
func Remove(s Spec) error { return removeOn(s, runtime.GOOS) }

// removeOn is Remove for the host scheduler goos selects; the Windows branch
// runs against the schtasksRun seam, so a test can drive it on any OS.
func removeOn(s Spec, goos string) error {
	var err error
	switch goos {
	case "darwin":
		err = removeLaunchd(s)
	case "windows":
		err = removeSchtasks(s)
		if err == nil {
			err = removeSchtasksSideFiles(s)
		}
	default:
		err = removeCron(s)
	}
	if err != nil {
		return err
	}
	// Both decisions are taken before either file goes: whether the verify
	// descriptor is trusted can depend on the backup's (ReadVerifyDescriptor).
	backupGoes, verifyGoes := descriptorGoesWith(s.Out, s.Name, goos), verifyRecordedOn(s.Out, s.Name, goos)
	if backupGoes {
		if err := RemoveDescriptor(s.Out); err != nil {
			return err
		}
	}
	if verifyGoes {
		return RemoveVerifyDescriptor(s.Out)
	}
	return nil
}

// descriptorGoesWith reports whether removing the schedule called name also
// removes the archive's (backup) descriptor: only when the descriptor is
// readable and records that name. A descriptor that exists but cannot be read (a
// hand edit, bad JSON, an invalid name) is never deleted, with the archive's
// default-named entry or any other: nothing in it says whose it was (a backup
// under any -name writes it), so the caller names it with
// UnreadableDescriptorRemedy instead (MA-455; owner ruling §13.6, ESC-10).
func descriptorGoesWith(out, name, goos string) bool {
	if out == "" {
		return false
	}
	d, err := ReadDescriptor(out)
	switch {
	case err == nil:
		return sameName(d.Name, name, goos)
	case errors.Is(err, fs.ErrNotExist):
		return false
	default:
		// Unreadable: never deleted, whatever the entry's name (rulings §13.6, ESC-10).
		return false
	}
}

// UnreadableDescriptorRemedy is what the operator does with the archive's
// descriptor when it exists but cannot be read and -remove left it in place:
// re-installing the backup schedule rewrites it, and once the archive has no
// backup schedule left it is deleted by hand. A backup schedule is any
// non-verify job: only those write this descriptor.
const UnreadableDescriptorRemedy = "delete it if no backup schedule for this archive is installed, or re-run that schedule's `mailarchive schedule ... -install` to rewrite it"

// UnreadableDescriptor returns the path of the archive's descriptor when the
// file exists but cannot be read, and "" otherwise. Remove never deletes such a
// file (descriptorGoesWith), so a caller that has just removed a backup under
// any name names what is left with UnreadableDescriptorRemedy: only the
// operator can tell whose it was.
func UnreadableDescriptor(out string) string {
	if out == "" {
		return ""
	}
	path := filepath.Join(out, DescriptorName)
	if _, err := os.Lstat(path); err != nil {
		return ""
	}
	if _, err := ReadDescriptor(out); err == nil {
		return ""
	}
	return path
}

// UnreadableVerifyDescriptorRemedy is what the operator does with the archive's
// verify descriptor when it exists but cannot be read: re-installing the
// archive's verify rewrites it, and once no verify for the archive is installed
// (-remove leaves the file in place) it is deleted by hand.
const UnreadableVerifyDescriptorRemedy = "delete it if no verify schedule for this archive is installed, or re-run that verify's `mailarchive schedule ... -install -- verify ...` to rewrite it"

// UnreadableVerifyDescriptor returns the path of the archive's verify
// descriptor when the file exists but cannot be read, and "" otherwise. Remove
// never deletes such a file (it goes with neither entry), so a caller that has
// just removed a verify schedule names it with UnreadableVerifyDescriptorRemedy.
func UnreadableVerifyDescriptor(out string) string {
	if out == "" {
		return ""
	}
	path := filepath.Join(out, VerifyDescriptorName)
	if _, err := os.Lstat(path); err != nil {
		return ""
	}
	if _, err := ReadVerifyDescriptor(out); err == nil {
		return ""
	}
	return path
}

// descriptorRecords reports whether the archive at out has a readable
// descriptor that records the schedule called name.
func descriptorRecords(out, name string) bool {
	if out == "" {
		return false
	}
	d, err := ReadDescriptor(out)
	return err == nil && sameName(d.Name, name, runtime.GOOS)
}

// sameName reports whether two schedule names are the same entry on the
// scheduler goos selects: Task Scheduler task names, the Windows wrapper and XML
// file names and the launchd plist file are case-insensitive on Windows and on
// default macOS volumes, so two names that differ only in case are one entry
// there (G8-AO3); cron marker lines are compared exactly.
func sameName(a, b, goos string) bool {
	if goos == "darwin" || goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// VerifyRecorded reports whether the archive at out has a readable verify
// descriptor that records the schedule called name: that entry is the
// archive's scheduled verify, whatever -name it was installed under.
func VerifyRecorded(out, name string) bool { return verifyRecordedOn(out, name, runtime.GOOS) }

func verifyRecordedOn(out, name, goos string) bool {
	if out == "" {
		return false
	}
	d, err := ReadVerifyDescriptor(out)
	return err == nil && sameName(d.Name, name, goos)
}

// SupersededVerify returns the name of the archive's earlier scheduled verify
// that installing s — a verify under a -name of its own — leaves running: the
// name the archive's verify descriptor records (or, without a readable one, a
// backup descriptor an older release wrote for the verify itself), when that
// is not s.Name and the host scheduler still has it. Install never removes that
// entry: on launchd and Task Scheduler nothing can say the old name is still
// this archive's (their entries are never read back, UJ-6, and another
// archive's verify may have taken a shared -name over). And once Install has
// rewritten the verify descriptor for the new entry, nothing records the old
// one. So the caller reads it before installing and names it, with the command
// that removes it. "" otherwise: not a verify, the same name, or nothing left
// running under the old one.
func SupersededVerify(s Spec) string {
	if !isVerifyJob(s) || s.Out == "" {
		return ""
	}
	// The recorded name is untrusted (a handed-over folder can record any entry
	// name): it is named for removal only when it binds to this archive.
	old, _ := RecordedVerifyName(s.Out)
	if old == "" || old == s.Name || Query(old) != Installed {
		return ""
	}
	return old
}

// RemoveIfInstalled removes a schedule only when it is actually present — one
// of the archive's descriptors records this name, or the named entry is in the
// host scheduler — and returns removed=false without touching the scheduler
// otherwise. So a `-remove` for an archive that never had a schedule neither
// claims to have removed one nor rewrites (materializing an empty) crontab
// (friction #9), and a name that was never installed (a typo, another
// archive's entry) does not count the archive's own descriptors as its
// presence.
func RemoveIfInstalled(s Spec) (removed bool, err error) {
	if !descriptorRecords(s.Out, s.Name) && !VerifyRecorded(s.Out, s.Name) && Query(s.Name) != Installed {
		return false, nil
	}
	return true, Remove(s)
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
	tab, err := readCrontabForEdit()
	if err != nil {
		return err
	}
	return writeCrontab(UpsertCronBlock(tab, CronMarker(s.Name), block))
}

func removeCron(s Spec) error {
	tab, err := readCrontabForEdit()
	if err != nil {
		return err
	}
	return writeCrontab(RemoveCronBlock(tab, CronMarker(s.Name)))
}

// crontabList and crontabInstall are the two crontab side effects, indirected
// through package variables so a test can sandbox them — proving a no-op
// `-remove` writes nothing — without touching the developer's real crontab.
// crontabList returns `crontab -l`'s combined output and run error;
// crontabInstall pipes new content to `crontab -`.
var (
	crontabList = func() (string, error) {
		out, err := exec.Command("crontab", "-l").CombinedOutput()
		return string(out), err
	}
	crontabInstall = func(content string) error {
		cmd := exec.Command("crontab", "-")
		cmd.Stdin = strings.NewReader(content)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("crontab -: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

// schtasksRun runs schtasks.exe with args and returns its combined output and
// run error. Like crontabList and crontabInstall it is a package variable, so a
// test can stand a fake Task Scheduler in, on any OS, without touching the
// runner's real one.
var schtasksRun = func(args ...string) ([]byte, error) {
	return exec.Command("schtasks", args...).CombinedOutput()
}

// readCrontab is the crontab for a read-only look (the name guard): an
// unreadable one reads as empty there, and the edit that follows is refused by
// readCrontabForEdit.
func readCrontab() string {
	out, _ := readCrontabForEdit()
	return out
}

// readCrontabForEdit returns the user's crontab for an edit that writes the whole
// table back. "No crontab yet" is an empty one; any other `crontab -l` failure
// (a permission or spool error) is an error, never an empty table — writing back
// only our block would destroy the user's other jobs (G8-AC5).
func readCrontabForEdit() (string, error) {
	out, err := crontabList()
	if err == nil {
		return out, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if t := strings.ToLower(out); strings.Contains(t, "no crontab") || strings.TrimSpace(t) == "" {
			return "", nil // no crontab yet (or none for this user)
		}
	}
	return "", fmt.Errorf("crontab -l failed, so the crontab was left alone rather than overwritten: %w: %s", err, strings.TrimSpace(out))
}

func writeCrontab(content string) error { return crontabInstall(content) }

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
// schtasks.exe (without the program name itself) to register the task from its
// XML definition. Validate/generation errors surface here so Install fails
// before touching the scheduler.
func SchtasksCreateArgv(s Spec) ([]string, error) {
	if _, err := ParseInterval(string(s.Interval)); err != nil {
		return nil, err
	}
	if _, _, err := parseHHMM(s.At); err != nil {
		return nil, err
	}
	return []string{"/Create", "/TN", s.Name, "/XML", SchtasksXMLPath(s.WrapperPath), "/F"}, nil
}

// SchtasksQueryArgv is the argv Install runs after /Create to confirm the task
// was actually registered (a /Create that "succeeds" but registers nothing must
// not pass silently).
func SchtasksQueryArgv(name string) []string {
	return []string{"/Query", "/TN", name}
}

// schtasksPreview renders the Windows install for -install-less previewing: the
// schtasks command, the XML definition it would write (decoded to UTF-8 for the
// terminal), and the wrapper body when one is used. now fixes the trigger's
// StartBoundary.
func schtasksPreview(s Spec, now time.Time) (string, error) {
	cmd, err := SchtasksCreateCmd(s)
	if err != nil {
		return "", err
	}
	doc, err := SchtasksXML(s, now)
	if err != nil {
		return "", err
	}
	body, err := decodeUTF16LE(doc)
	if err != nil {
		return "", err
	}
	xmlPath := SchtasksXMLPath(s.WrapperPath)
	out := "Windows Task Scheduler — " + cadenceGloss(s) + " (StartBoundary is local time) — would run:\n\n  " + shellJoin(s.program()) + "\n"
	out += "\nregistered with:\n\n  " + cmd + "\n"
	out += "\nfrom the definition " + xmlPath + " (written UTF-16LE):\n\n" + body + "\n"
	// The resilience gloss the cron preview's persona also needs: the XML sets
	// StartWhenAvailable=true, batteries allowed, WakeToRun=false (see MA-146).
	out += "\nA run missed while the PC slept is caught up on wake; runs on battery; a night the PC is off is skipped.\n"
	if isVerifyJob(s) {
		out += VerifyLockWarning + "\n"
		out += "A failed run surfaces only via `mailarchive status` (a verify's verdict is shown as Last verify).\n"
	} else {
		out += "A failed run surfaces only via `mailarchive status`.\n"
	}
	if argHasMode(s.Args, "full") {
		out += "-mode full: every scheduled run re-exports everything.\n"
	}
	if s.Wrapper {
		out += "\nwith the wrapper " + s.WrapperPath + " containing:\n\n" + CmdWrapper(s)
	}
	return out, nil
}

func installSchtasks(s Spec) error {
	xmlPath := SchtasksXMLPath(s.WrapperPath)
	doc, err := SchtasksXML(s, time.Now())
	if err != nil {
		return err
	}
	if err := writeFileAtomic(xmlPath, doc); err != nil {
		return fmt.Errorf("could not write the Task Scheduler definition %s: %w", xmlPath, err)
	}
	argv, err := SchtasksCreateArgv(s)
	if err != nil {
		return err
	}
	if out, err := schtasksRun(argv...); err != nil {
		return fmt.Errorf("schtasks /Create from %s: %w: %s", xmlPath, err, strings.TrimSpace(string(out)))
	}
	// Confirm the task exists: a /Create that reported success but registered
	// nothing (a definition schtasks silently rejected) must not pass as a
	// working schedule.
	if out, err := schtasksRun(SchtasksQueryArgv(s.Name)...); err != nil {
		return fmt.Errorf("schtasks /Create reported success but the task %q is absent — its definition is %s: %w: %s", s.Name, xmlPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeSchtasks deletes the task. A task already gone from Task Scheduler
// (deleted there by hand) leaves nothing to delete, as an absent launchd plist
// or cron block does: when /Delete fails and /Query no longer finds the task,
// the removal goes on to the task's files and the descriptor that records it,
// so a recorded-but-gone entry's stale record is cleared on Windows too. A task
// Task Scheduler still has — or that it cannot be asked about, or refuses to
// answer about (access denied, a stopped Task Scheduler service: schtasks exits
// 1 for those as for a missing task) — keeps the /Delete error (G8-AC1): only
// /Query saying the task is not there clears it.
func removeSchtasks(s Spec) error {
	out, err := schtasksRun("/Delete", "/TN", s.Name, "/F")
	if err == nil {
		return nil
	}
	if qout, qerr := schtasksRun(SchtasksQueryArgv(s.Name)...); schtasksState(qerr) == NotInstalled && schtasksSaysMissing(qout) {
		return nil
	}
	return fmt.Errorf("schtasks /Delete: %w: %s — the task may still be registered; delete it by hand with `schtasks /Delete /TN %s /F` or in Task Scheduler", err, strings.TrimSpace(string(out)), s.Name)
}

// schtasksSaysMissing reports whether a failed `schtasks /Query /TN name` said
// the task does not exist ("ERROR: The system cannot find the file specified." /
// "...the specified task name ... does not exist in the system"), as opposed to
// failing for another reason with the same exit status. The wording is the
// system's own; on a localised Windows nothing matches, and the removal keeps the
// /Delete error and names the by-hand delete rather than claim a removal it
// could not confirm.
func schtasksSaysMissing(out []byte) bool {
	t := strings.ToLower(string(out))
	return strings.Contains(t, "cannot find") || strings.Contains(t, "does not exist")
}

// removeSchtasksSideFiles deletes the wrapper and the XML definition Install
// wrote beside it, tolerating either being already gone. It is the file half of
// the Windows Remove and is unit-testable off Windows.
func removeSchtasksSideFiles(s Spec) error {
	if s.WrapperPath == "" {
		return nil
	}
	for _, p := range []string{s.WrapperPath, SchtasksXMLPath(s.WrapperPath)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeFileAtomic writes data to path via a temp file that is fsynced and
// renamed into place, so an interrupted write never leaves a half-written
// definition where schtasks would read it.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".xml-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmp.Name(), path)
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
