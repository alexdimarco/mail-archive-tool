package encrecord

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-853, R37
// On Windows, a record whose copies another account owns is refused naming that
// account and this one by name, and the icacls commands it prints, run exactly as
// printed through the real Command Prompt (the record alone) and PowerShell (both
// copies), give the copies back so the record loads (the P3-8 Windows lab, B11,
// carried to the record). The record is never rewritten while refused. Giving a file
// to another account needs an elevated account or SYSTEM; unelevated it is said and
// the test stops there.
func TestTheRecordsWindowsRefusalEndsWhenItsCommandsAreRun(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	p, m := filepath.Join(dir, FileName), filepath.Join(dir, MirrorName)

	// Positive twin: the record this account wrote loads.
	if _, err := Load(); err != nil {
		t.Fatalf("a record this account wrote: %v", err)
	}
	me := nameOfForTest(t, currentSIDForTest(t))
	users := nameOfForTest(t, "S-1-5-32-545")

	for _, c := range []struct {
		name   string
		copies []string
		shell  string
	}{
		{"the record alone", []string{p}, "cmd"},
		{"both copies", []string{p, m}, "powershell"},
	} {
		for _, f := range c.copies {
			if b, err := exec.Command("icacls", f, "/setowner", "*S-1-5-32-545").CombinedOutput(); err != nil {
				t.Skipf("this account cannot give a file to Users (it is not elevated or SYSTEM): %v: %s", err, b)
			}
		}
		before, _ := os.ReadFile(p)
		_, err := Load()
		var ue *UnusableError
		if !errors.As(err, &ue) || ue.Case != CasePermissions {
			t.Fatalf("%s: want the permissions case, got %v", c.name, err)
		}
		_, uerr := Update("test", func(r *Record) error { return r.Add(idB, t.TempDir(), false, time.Now()) })
		msg := err.Error()
		assure.Refused(t, code(uerr), msg, assure.Code(1),
			assure.Names(append([]string{users, me, "not yours", "/setowner", "Command Prompt or PowerShell"}, c.copies...)...),
			assure.Forbid("%USERNAME%"),
			assure.NoSideEffect(func() bool { after, _ := os.ReadFile(p); return bytes.Equal(before, after) }))
		var cmds []string
		for _, l := range strings.Split(msg, "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "icacls ") {
				cmds = append(cmds, l)
			}
		}
		if len(cmds) != len(c.copies) {
			t.Fatalf("%s: want %d commands, got %q", c.name, len(c.copies), cmds)
		}
		for _, line := range cmds {
			runAsTypedForTest(t, c.shell, line)
		}
		_, err = Load()
		assure.Reached(t, err == nil, c.name+": after the printed commands, the record loads")
	}
}

// runAsTypedForTest runs line as a person would type it into the Command Prompt or
// PowerShell, failing the test when the shell reports an error.
func runAsTypedForTest(t *testing.T, shell, line string) {
	t.Helper()
	var cmd *exec.Cmd
	switch shell {
	case "cmd":
		cmd = exec.Command(`C:\Windows\System32\cmd.exe`)
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + line + `"`}
	default:
		u := utf16.Encode([]rune(line + "\nif ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"))
		b := make([]byte, 2*len(u))
		for i, r := range u {
			b[2*i], b[2*i+1] = byte(r), byte(r>>8)
		}
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %s: %v: %s", shell, line, err, b)
	}
}

// nameOfForTest is the DOMAIN\name of the account sidText names, looked up apart
// from the code under test.
func nameOfForTest(t *testing.T, sidText string) string {
	t.Helper()
	sid, err := windows.StringToSid(sidText)
	if err != nil {
		t.Fatal(err)
	}
	name, domain, _, err := sid.LookupAccount("")
	if err != nil {
		t.Fatal(err)
	}
	if domain != "" {
		name = domain + `\` + name
	}
	return name
}

func currentSIDForTest(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}
