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
// and delayed expansion is never enabled, so ! is literal too. A program token
// that ends in a backslash has that run doubled (batchArg), so the program's
// own argv parser closes the token where cmd.exe does.
func CmdWrapper(s Spec) string {
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString("rem mailarchive scheduled backup \"" + strings.ReplaceAll(s.Name, "\"", "'") + "\" - managed by `mailarchive schedule`.\r\n")
	b.WriteString("rem Re-run `mailarchive schedule ... -install` to update it, `-remove` to delete it.\r\n")
	parts := make([]string, 0, 1+len(s.Args))
	for _, tok := range s.program() {
		parts = append(parts, batchArg(tok))
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
// it is replaced rather than escaped; % is doubled. The result is read by
// cmd.exe alone — the redirect target, a path cmd opens itself.
func batchQuote(tok string) string {
	tok = strings.ReplaceAll(tok, `"`, `'`)
	tok = strings.ReplaceAll(tok, "%", "%%")
	return `"` + tok + `"`
}

// batchArg quotes a token that cmd.exe hands on to the program, which splits
// its command line under the C-runtime rules (CommandLineToArgvW; Go's
// os.Args): there a backslash run directly before a quote escapes it, so a
// token ending in a separator — a drive root E:\ or a share root
// \\nas\backup\ — written as "E:\" would leave the quote open and fold every
// later argument into it. Doubling the trailing run (2n backslashes and the
// closing quote read as n literal backslashes) closes the token exactly, and
// cmd gives a backslash no meaning, so the doubling is invisible to it. With
// every quote already replaced, the trailing run is the only backslash run that
// can precede a quote.
func batchArg(tok string) string {
	q := batchQuote(tok)
	body := q[1 : len(q)-1]
	trail := len(body) - len(strings.TrimRight(body, `\`))
	return `"` + body + strings.Repeat(`\`, trail) + `"`
}
