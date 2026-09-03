package schedule

import (
	"os"
	"path/filepath"
	"strings"
)

// The Windows Task Scheduler limits a task's run string to 261 characters and
// gives the job no stderr sink. Install therefore writes a small batch wrapper
// under the user's profile and schedules THAT: the wrapper carries the full
// command (every token quoted, so any legal NTFS path — spaces, "&", "^" —
// survives cmd.exe) and redirects stderr to a sibling file that catches what
// the program cannot log itself (a crash, a failed start).

// DefaultWrapperPath is %LOCALAPPDATA%\mailarchive\<name>.cmd, falling back to
// the user's config directory when LOCALAPPDATA is unset. It is always
// absolute: a relative wrapper would resolve against the scheduler's working
// directory (System32) and silently never run — Spec.Validate refuses it.
func DefaultWrapperPath(name string) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		}
	}
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".config")
		}
	}
	if !filepath.IsAbs(base) {
		if a, err := filepath.Abs(base); err == nil {
			base = a
		}
	}
	return filepath.Join(base, "mailarchive", name+".cmd")
}

// StderrLogPath is the crash/launch-failure sink beside the operator log:
// <dir>/<name>.stderr.log for <dir>/<name>.log.
func StderrLogPath(log string) string {
	return strings.TrimSuffix(log, ".log") + ".stderr.log"
}

// CmdWrapper returns the batch file that runs the job. Every token is quoted
// unconditionally (inside cmd.exe quotes the separators & | < > ^ ( ) are
// inert), every % is doubled so no environment reference is ever expanded,
// and delayed expansion is never enabled, so ! is literal too.
func CmdWrapper(s Spec) string {
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString("rem mailarchive scheduled backup \"" + strings.ReplaceAll(s.Name, "\"", "'") + "\" - managed by `mailarchive schedule`.\r\n")
	b.WriteString("rem Re-run `mailarchive schedule ... -install` to update it, `-remove` to delete it.\r\n")
	parts := make([]string, 0, 1+len(s.Args))
	for _, tok := range s.program() {
		parts = append(parts, batchQuote(tok))
	}
	line := strings.Join(parts, " ")
	if strings.TrimSpace(s.Log) != "" {
		line += " 2>> " + batchQuote(StderrLogPath(s.Log))
	}
	b.WriteString(line + "\r\n")
	return b.String()
}

// batchQuote wraps a token in double quotes for a batch line. A double quote
// cannot appear in a Windows file name and is not a legal flag value here, so
// it is replaced rather than escaped; % is doubled.
func batchQuote(tok string) string {
	tok = strings.ReplaceAll(tok, `"`, `'`)
	tok = strings.ReplaceAll(tok, "%", "%%")
	return `"` + tok + `"`
}
