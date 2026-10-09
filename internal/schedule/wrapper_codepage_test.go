package schedule

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"

	"mail-archive-tool/internal/assure"
)

// oemCodePages are console (OEM) code pages cmd.exe decodes a batch file in by
// default on Western, Cyrillic, Japanese and Simplified-Chinese Windows. cmd
// never auto-detects UTF-8.
var oemCodePages = []struct {
	name string
	enc  encoding.Encoding
}{
	{"CP437", charmap.CodePage437},
	{"CP850", charmap.CodePage850},
	{"CP866", charmap.CodePage866},
	{"CP932", japanese.ShiftJIS},
	{"CP936", simplifiedchinese.GBK},
}

// covers: MA-451, R14, S28
// cmd.exe reads a batch file in the console's OEM code page, not UTF-8, so a
// non-ASCII program, config, archive, input or log path (a local profile
// C:\Users\Jörg, an archive D:\Mail\Eingänge, Анна, 田中) must still reach the
// scheduled program intact. The wrapper's bytes are decoded the way cmd does —
// line by line in the active code page, starting from each OEM page above and
// switching to UTF-8 once a `chcp 65001` line has run — and the run line must
// then split (cmdRunArgv) to exactly the job's argv and .stderr.log target. The
// file carries no BOM (cmd would run it as part of the first command), every
// byte read before the switch is ASCII, a wrapper holding any non-ASCII byte
// switches, and an all-ASCII wrapper never switches the console's code page at
// all. The non-ASCII bytes are placed in every token, and also only where they
// most often are: an ASCII exe under C:\Program Files with an ASCII -config,
// whose only non-ASCII bytes are the archive's -out, its -log and the 2>>
// target; and a job whose only non-ASCII token is its -input. The switch must
// look at the whole run line, not just the program.
func TestCmdWrapperNonASCIIPathsSurviveCodePage(t *testing.T) {
	base := Spec{
		Name: "mailarchive-1a2b3c4d", Interval: Daily, At: "02:00",
		Wrapper: true, WrapperPath: `C:\Users\Jörg\AppData\Local\mailarchive\mailarchive-1a2b3c4d.cmd`,
	}
	fixture := func(exe string, args ...string) Spec {
		s := base
		s.Exe, s.Args = exe, args
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-log" {
				s.Log = args[i+1]
			}
		}
		return s
	}
	const programFiles = `C:\Program Files\MailArchive\mailarchive.exe`
	fixtures := []struct {
		name string
		spec Spec
	}{
		{"all ASCII (no switch)", fixture(programFiles,
			"-out", `D:\Mail\Inbox`, "-unattended", "-log", `D:\Mail\Inbox\mailarchive-1a2b3c4d.log`)},
		{"non-ASCII in every token", fixture(`C:\Users\Jörg\AppData\Local\Programs\MailArchive\mailarchive.exe`,
			"-config", `C:\Users\Jörg\AppData\Roaming\mailarchive\graph-config.json`,
			"-out", `D:\Mail\Eingänge`, "-input", `C:\Users\Анна\田中.pst`, "-unattended",
			"-log", `D:\Mail\Eingänge\mailarchive-1a2b3c4d.log`)},
		{"ASCII exe and -config, non-ASCII -out, -log and 2>> target", fixture(programFiles,
			"-config", `C:\ProgramData\mailarchive\graph-config.json`,
			"-out", `D:\Mail\Eingänge`, "-unattended", "-log", `D:\Mail\Eingänge\mailarchive-1a2b3c4d.log`)},
		{"only -input non-ASCII", fixture(programFiles,
			"-out", `D:\Mail\Inbox`, "-input", `C:\Users\Public\Анна\田中.pst`, "-unattended",
			"-log", `D:\Mail\Inbox\mailarchive-1a2b3c4d.log`)},
	}

	for _, f := range fixtures {
		s := f.spec
		data := []byte(CmdWrapper(s))
		if len(data) >= 3 && string(data[:3]) == "\xef\xbb\xbf" {
			t.Fatalf("%s: the wrapper starts with a UTF-8 BOM, which cmd.exe would run as part of @echo off", f.name)
		}
		for _, cp := range oemCodePages {
			where := f.name + ", " + cp.name
			lines, switched := cmdDecodeBatch(t, where, data, cp.enc)
			assure.Reached(t, lines, where+": decoded wrapper lines")
			argv, target := cmdRunArgv(t, lines[len(lines)-1])
			if !equalStrings(argv, s.program()) {
				t.Errorf("%s: the program receives\n %q\nwant\n %q", where, argv, s.program())
			}
			if want := StderrLogPath(s.Log); target != want {
				t.Errorf("%s: stderr is redirected to %q, want %q", where, target, want)
			}
			if ascii := sevenBit(string(data)); ascii && switched {
				t.Errorf("%s: an all-ASCII wrapper switched the console code page", where)
			} else if !ascii && !switched {
				t.Errorf("%s: a wrapper holding non-ASCII bytes never switched the console to UTF-8", where)
			}
		}
	}
}

// cmdDecodeBatch splits a batch file into CRLF lines and decodes each one in
// cmd.exe's active code page: oem until a `chcp 65001` line has run, UTF-8
// after it. A non-ASCII byte read while the OEM page is active fails the test —
// cmd would garble it differently on every locale. It reports whether the code
// page was switched.
func cmdDecodeBatch(t *testing.T, where string, data []byte, oem encoding.Encoding) (lines []string, switched bool) {
	t.Helper()
	for _, raw := range strings.Split(strings.TrimRight(string(data), "\r\n"), "\r\n") {
		line := raw
		if !switched {
			dec, err := oem.NewDecoder().String(raw)
			if err != nil {
				t.Fatalf("%s: decoding %q in the OEM code page: %v", where, raw, err)
			}
			if !sevenBit(raw) {
				t.Errorf("%s: a non-ASCII byte is read in the OEM code page (cmd sees %q for %q)", where, dec, raw)
			}
			line = dec
		}
		if f := strings.Fields(strings.ToLower(line)); len(f) >= 2 && f[0] == "chcp" && f[1] == "65001" {
			switched = true
			continue
		}
		lines = append(lines, line)
	}
	return lines, switched
}

func sevenBit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
