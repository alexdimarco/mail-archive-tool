package keyring

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-853, R37
// A secret file Windows lets another account read, or that another account owns, is
// refused naming those accounts and this account by name, with icacls commands that
// end the refusal when run exactly as printed (the P3-8 Windows lab, B11): an
// explicit grant to Users is removed, a grant to Everyone passed down from the
// folder is dropped, and a file owned by Administrators is given back with
// /setowner. Never "%USERNAME%", which PowerShell does not expand. The commands are
// run as printed through the real Command Prompt and PowerShell, and the next read
// opens the archive. The owner case needs an account that may give a file to
// Administrators (elevated, or SYSTEM); unelevated it is said and left out.
func TestASecretFileRefusalOnWindowsGivesCommandsThatEndIt(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	me := lookupAccountForTest(t, currentSIDForTest(t))
	icacls := func(args ...string) error {
		b, err := exec.Command("icacls", args...).CombinedOutput()
		if err != nil {
			t.Logf("icacls %v: %v: %s", args, err, b)
		}
		return err
	}
	opens := func(path string) bool {
		s, err := PasswordFile(path)
		if err != nil {
			t.Logf("read %s: %v", path, err)
			return false
		}
		u, err := Open(out, s, OpenOptions{})
		if err != nil {
			return false
		}
		u.Destroy()
		return true
	}
	newFile := func(dir string) string {
		p := filepath.Join(dir, "pw.txt")
		if err := os.WriteFile(p, []byte(testPassword+"\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Positive twin: a file made this account's alone (by SID, apart from the
	// refusal's own text) is read and opens the archive.
	good := newFile(t.TempDir())
	if icacls(good, "/setowner", "*"+me.sid) != nil || icacls(good, "/inheritance:r", "/grant:r", "*"+me.sid+":F") != nil {
		t.Fatal("could not make a file this account's alone")
	}
	assure.Reached(t, opens(good), "a file only this account can read opens the archive")

	users := lookupForTest(t, "S-1-5-32-545")
	everyone := lookupForTest(t, "S-1-1-0")
	admins := lookupForTest(t, "S-1-5-32-544")

	type tcase struct {
		name  string
		path  string
		names []string
		shell string
	}
	var cases []tcase

	granted := newFile(t.TempDir())
	if icacls(granted, "/grant", "*S-1-5-32-545:R") != nil {
		t.Fatal("could not grant Users")
	}
	cases = append(cases, tcase{"an explicit grant to Users", granted, []string{users}, "cmd"})

	shared := t.TempDir()
	if icacls(shared, "/grant", "*S-1-1-0:(OI)(CI)R") != nil {
		t.Fatal("could not grant Everyone on the folder")
	}
	cases = append(cases, tcase{"a grant to Everyone passed down from the folder", newFile(shared), []string{everyone}, "powershell"})

	foreign := newFile(t.TempDir())
	if icacls(foreign, "/setowner", "*S-1-5-32-544") == nil {
		cases = append(cases, tcase{"a file owned by Administrators", foreign, []string{admins, "/setowner", "Run as administrator"}, "powershell"})
	} else {
		t.Log("this account cannot give a file to Administrators (it is not elevated): the owner case runs elevated or as SYSTEM")
	}

	for _, c := range cases {
		s, err := PasswordFile(c.path)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names(append([]string{c.path, me.name, "icacls", "Command Prompt or PowerShell"}, c.names...)...),
			assure.Forbid("%USERNAME%", testPassword), assure.NoSideEffect(func() bool { return s.text == "" }))
		var cmds []string
		for _, l := range strings.Split(msg, "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "icacls ") {
				cmds = append(cmds, l)
			}
		}
		if len(cmds) == 0 {
			t.Fatalf("%s: no command to run in %q", c.name, msg)
		}
		for _, line := range cmds {
			runAsTyped(t, c.shell, line)
		}
		assure.Reached(t, opens(c.path), c.name+": after the printed commands, the file is read and opens the archive")
	}
}

// runAsTyped runs line as a person would type it into the Command Prompt or
// PowerShell, failing the test when the shell reports an error.
func runAsTyped(t *testing.T, shell, line string) {
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

// accountForTest is an account's SID and DOMAIN\name, looked up apart from the code
// under test.
type accountForTest struct{ sid, name string }

func lookupForTest(t *testing.T, sidText string) string {
	t.Helper()
	return lookupAccountForTest(t, sidText).name
}

func lookupAccountForTest(t *testing.T, sidText string) accountForTest {
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
	return accountForTest{sid: sid.String(), name: name}
}

func currentSIDForTest(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}
