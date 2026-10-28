package encmark

import (
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// windowsArgv splits a command line the way a Windows program's C runtime does:
// 2n backslashes before a quote are n backslashes and the quote opens or closes a
// quoted part; 2n+1 are n backslashes and a literal quote; other backslashes are
// literal; outside quotes a space or tab ends an argument.
func windowsArgv(line string) []string {
	var args []string
	var cur strings.Builder
	quoted, started := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(line) && line[i] == '\\' {
				n++
				i++
			}
			if i < len(line) && line[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					quoted = !quoted
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			started = true
		case c == '"':
			quoted, started = !quoted, true
		case (c == ' ' || c == '\t') && !quoted:
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started {
		args = append(args, cur.String())
	}
	return args
}

// covers: MA-854, R37
// A folder printed into a command to type reads back on Windows as that folder,
// with the rest of the line kept: a drive root E:\ and a share root \\nas\backup\
// end in a backslash, which would otherwise swallow the closing quote and -from
// FILE after it (the P3-8 pass, COMP-1). Positive twins: a plain folder and a
// network folder without a trailing backslash read back the same.
func TestACommandPathReadsBackOnWindows(t *testing.T) {
	for _, folder := range []string{
		`C:\Users\alex\Mail Archive`, // positive twins
		`\\server\share\Arch`,
		`E:\`,           // the drive root
		`\\nas\backup\`, // the share root
	} {
		line := "mailarchive restore-key-file -out " + commandPath("windows", folder) + " -from FILE"
		got := windowsArgv(line)
		want := []string{"mailarchive", "restore-key-file", "-out", folder, "-from", "FILE"}
		if !slices.Equal(got, want) {
			t.Fatalf("%s is read back as %q from %s", folder, got, line)
		}
	}
	assure.Reached(t, commandPath("windows", `E:\`) == `"E:\\"` && commandPath("linux", "/Volumes/Backup/My Mail") == `"/Volumes/Backup/My Mail"`,
		"the drive root's backslash doubled inside the quotes; elsewhere quoted as before")
}
