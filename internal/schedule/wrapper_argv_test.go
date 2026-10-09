package schedule

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-450, R14, S28
// The Windows wrapper's run line reaches the scheduled program as EXACTLY the
// job's argv, for any host path — including the tokens that end in a backslash
// (a drive root `E:\`, a share root `\\nas\backup\`, a folder typed with its
// trailing separator), which a bare `"E:\"` would turn into an escaped quote
// that swallows every later argument. The line is run through a model of
// cmd.exe (%% → %, the unquoted `2>>` redirect stripped, no other unquoted
// metacharacter) and then split the way the program builds os.Args on Windows
// (goCommandLineToArgv, a verbatim port of Go's os/exec_windows.go), so the
// test proves the round trip rather than a byte pattern.
func TestCmdWrapperArgvRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		args []string
		log  string
	}{
		{"drive root", []string{"-out", `E:\`, "-mode", "incremental", "-unattended", "-auto", "-log", `E:\mailarchive-1a2b3c4d.log`}, `E:\mailarchive-1a2b3c4d.log`},
		{"share root", []string{"-out", `\\nas\backup\`, "-mode", "incremental", "-unattended", "-log", `\\nas\backup\mailarchive-1a2b3c4d.log`}, `\\nas\backup\mailarchive-1a2b3c4d.log`},
		{"trailing separator with a space", []string{"-out", `C:\Mail Archive\`, "-input", `C:\Users\Alex\Outlook Files\`, "-unattended"}, `C:\Mail Archive\mailarchive-1a2b3c4d.log`},
		{"doubled separators", []string{"-out", `C:\odd\\dir\\`, "-unattended"}, `C:\odd\\dir\mailarchive-1a2b3c4d.log`},
		{"metacharacters (positive control)", []string{"-out", `C:\Data\R&D (2026)\Mail!Archive`, "-input", `C:\Users\Alex\%weird%\x.pst`, "-unattended"}, `C:\Data\R&D (2026)\Mail!Archive\mailarchive-1a2b3c4d.log`},
	}
	for _, c := range cases {
		s := Spec{
			Name: "mailarchive-1a2b3c4d", Interval: Daily, At: "02:00",
			Exe:  `C:\Program Files\MailArchive\mailarchive.exe`,
			Args: c.args, Log: c.log,
			Wrapper: true, WrapperPath: `C:\Users\Alex\AppData\Local\mailarchive\mailarchive-1a2b3c4d.cmd`,
		}
		argv, target := cmdRunArgv(t, wrapperRunLine(t, CmdWrapper(s)))
		assure.Reached(t, argv, c.name+": program argv")
		if !equalStrings(argv, s.program()) {
			t.Errorf("%s: the program receives\n %q\nwant\n %q", c.name, argv, s.program())
		}
		if want := StderrLogPath(c.log); target != want {
			t.Errorf("%s: stderr is redirected to %q, want %q", c.name, target, want)
		}
	}
}

// wrapperRunLine returns the wrapper's last (command) line.
func wrapperRunLine(t *testing.T, w string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(w, "\r\n"), "\r\n")
	return assure.Reached(t, lines[len(lines)-1], "wrapper run line")
}

// cmdRunArgv models what cmd.exe does with one batch line and what the program
// it starts then sees: percent expansion first (%% becomes %; every other %
// would be a variable reference, and the wrapper leaves none), then the line is
// scanned with cmd's quote state (toggled by every `"`; a backslash means
// nothing to cmd) for the redirect, which is removed, and the remaining text is
// the program's command line. Any other unquoted metacharacter (& | < > ^)
// would make cmd split or rewrite the line, so it fails the test. The command
// line is then split into os.Args the way a Go program on Windows does.
func cmdRunArgv(t *testing.T, line string) (argv []string, stderrTarget string) {
	t.Helper()
	var exp strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '%' {
			if i+1 < len(line) && line[i+1] == '%' {
				exp.WriteByte('%')
				i++
				continue
			}
			t.Fatalf("an undoubled %% would be expanded by cmd.exe: %q", line)
		}
		exp.WriteByte(line[i])
	}
	l := exp.String()
	cmdEnd := len(l)
	inQuote := false
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == '>' && strings.HasSuffix(l[:i], " 2") && strings.HasPrefix(l[i:], ">> "):
			cmdEnd = i - 2
			stderrTarget = strings.Trim(strings.TrimSpace(l[i+2:]), `"`)
			i = len(l)
		case strings.IndexByte("&|<>^", c) >= 0:
			t.Fatalf("unquoted cmd.exe metacharacter %q at %d would split the line: %q", c, i, l)
		}
	}
	if inQuote && cmdEnd == len(l) {
		t.Fatalf("cmd.exe would see an unterminated quote: %q", l)
	}
	return goCommandLineToArgv(strings.TrimSpace(l[:cmdEnd])), stderrTarget
}

// goCommandLineToArgv is a verbatim port of Go's os/exec_windows.go
// commandLineToArgv/readNextArg/appendBSBytes — how os.Args is built from
// GetCommandLine on Windows (2n backslashes before a quote are n literal
// backslashes and the quote toggles quoting; 2n+1 are n backslashes and a
// literal quote; a doubled quote inside quotes is one literal quote).
func goCommandLineToArgv(cmd string) []string {
	var args []string
	for len(cmd) > 0 {
		if cmd[0] == ' ' || cmd[0] == '\t' {
			cmd = cmd[1:]
			continue
		}
		var arg []byte
		arg, cmd = goReadNextArg(cmd)
		args = append(args, string(arg))
	}
	return args
}

func goReadNextArg(cmd string) (arg []byte, rest string) {
	var b []byte
	var inquote bool
	var nslash int
	for ; len(cmd) > 0; cmd = cmd[1:] {
		c := cmd[0]
		switch c {
		case ' ', '\t':
			if !inquote {
				return goAppendBS(b, nslash), cmd[1:]
			}
		case '"':
			b = goAppendBS(b, nslash/2)
			if nslash%2 == 0 {
				if inquote && len(cmd) > 1 && cmd[1] == '"' {
					b = append(b, c)
					cmd = cmd[1:]
				}
				inquote = !inquote
			} else {
				b = append(b, c)
			}
			nslash = 0
			continue
		case '\\':
			nslash++
			continue
		}
		b = goAppendBS(b, nslash)
		nslash = 0
		b = append(b, c)
	}
	return goAppendBS(b, nslash), ""
}

func goAppendBS(b []byte, n int) []byte {
	for ; n > 0; n-- {
		b = append(b, '\\')
	}
	return b
}
