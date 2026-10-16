package schedule

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-452, R14, S28
// cron ends a job's command at the first unescaped % (the rest becomes the
// job's stdin), before any shell quoting is seen, so every % in an executable,
// archive, input or log path must reach cron escaped. The generated crontab
// line is put through what cron and then sh do with it — cron's command-field
// rule (cronCommand, a port of cronie/Vixie do_command.c) and a real /bin/sh
// word expansion — and must yield exactly the job's argv with nothing left over
// as stdin, for a plain %, a URL-encoded name, a path with a space, a quote, a
// backslash immediately before the %, and a doubled %%. The managed block's
// -out also reads back (cronBlockOut) as the archive path, so the per-name
// collision guard keeps recognising the archive's own block on re-install.
func TestCronLineEscapesPercent(t *testing.T) {
	if runtime.GOOS == "windows" {
		// cron is Unix-only, and an sh.exe a Windows runner may have on PATH
		// (Git for Windows, MSYS) would re-split this quoting under its own
		// command-line rules, not cron's or a Unix sh's.
		t.Skip("cron runs only on Unix; a Windows sh.exe is not the shell cron hands this line to")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX sh on this host; cron runs only where sh exists")
	}
	cases := []struct {
		name string
		exe  string
		out  string
		in   string
	}{
		{"positive control (no %)", "/usr/local/bin/mailarchive", "/home/u/mail", "/home/u/Saved Mail.mbox"},
		{"plain %", "/usr/local/bin/mailarchive", "/home/u/100%mail", "/home/u/Saved%20Mail.mbox"},
		{"space and %", "/opt/mail%archive/bin/mailarchive", "/home/u/My 100%", "/home/u/50% off/inbox"},
		{"quote and %", "/usr/local/bin/mailarchive", "/home/u/it's 50%", "/home/u/%%doubled%%"},
		{"backslash before %", "/usr/local/bin/mailarchive", `/home/u/odd\%dir`, `/home/u/end\`},
	}
	for _, c := range cases {
		s := Spec{
			Name: "mailarchive-1a2b3c4d", Interval: Daily, At: "02:00", Exe: c.exe,
			Args: []string{"-out", c.out, "-mode", "incremental", "-unattended", "-input", c.in, "-log", c.out + "/mailarchive-1a2b3c4d.log"},
			Log:  c.out + "/mailarchive-1a2b3c4d.log", Out: c.out,
		}
		line, err := CronLine(s)
		if err != nil {
			t.Fatal(err)
		}
		f := strings.Fields(line)
		if len(f) < 6 || strings.Join(f[:5], " ") != "0 2 * * *" {
			t.Fatalf("%s: schedule fields wrong: %q", c.name, line)
		}
		field := strings.TrimLeft(line[len("0 2 * * *"):], " ")
		cmd, stdin := cronCommand(field)
		if stdin != "" {
			t.Errorf("%s: cron would cut the command at an unescaped %% and feed %q to stdin\n line: %s", c.name, stdin, line)
		}
		argv, err := shWords(sh, cmd)
		if err != nil {
			t.Errorf("%s: sh cannot parse the command cron runs, %q: %v\n line: %s", c.name, cmd, err, line)
		} else if !equalStrings(assure.Reached(t, argv, c.name+": argv after cron + sh"), s.program()) {
			t.Errorf("%s: cron + sh run\n %q\nwant\n %q\n line: %s", c.name, argv, s.program(), line)
		}
		block, err := CronBlock(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := cronBlockOut(block+"\n", CronMarker(s.Name)); got != c.out {
			t.Errorf("%s: cronBlockOut reads -out back as %q, want %q", c.name, got, c.out)
		}
	}

	// A block written before % was escaped still reads back as its own archive,
	// so re-installing that archive replaces it instead of being refused as a
	// different archive's schedule.
	legacy := CronMarker("mailarchive-1a2b3c4d") + "\n0 2 * * * /usr/local/bin/mailarchive -out /home/u/100%mail -mode incremental -unattended\n"
	if got := cronBlockOut(legacy, CronMarker("mailarchive-1a2b3c4d")); got != "/home/u/100%mail" {
		t.Errorf("a pre-escaping block reads -out back as %q, want /home/u/100%%mail", got)
	}
}

// cronCommand applies cron's rule for a command field (cronie and Vixie cron,
// do_command.c): the field is scanned once; a backslash escapes the next
// character, and an escaped % becomes a literal % with its backslash dropped,
// while any other escaped character keeps its backslash; the first unescaped %
// ends the command, and everything after it is the job's stdin.
func cronCommand(field string) (cmd, stdin string) {
	out := make([]byte, 0, len(field))
	escaped := false
	for i := 0; i < len(field); i++ {
		ch := field[i]
		out = append(out, ch)
		if escaped {
			if ch == '%' {
				out = append(out[:len(out)-2], '%')
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
		}
		if ch == '%' {
			return string(out[:len(out)-1]), field[i+1:]
		}
	}
	return string(out), ""
}

// shWords returns the words a real POSIX sh expands cmd into, the same
// expansion it applies to the words of a simple command.
func shWords(sh, cmd string) ([]string, error) {
	res, err := exec.Command(sh, "-c", "set -- "+cmd+"\nprintf '%s\\000' \"$@\"").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(res), "\x00"), "\x00"), nil
}
