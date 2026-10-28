package keyring

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// Signed in as the built-in Administrator (RID 500), whose entries Windows writes as
// "LA" in an access list, the user's own entry is the user's (the P3-8 fixes' pass,
// WINACL-1): it is not listed as another account that can read the file, and the
// remedy never removes it — before, it was, and running the printed commands twice
// left the file with no entry at all. The access lists are made from text, so this
// runs under any account; the alias is resolved by this computer's own lookup.
// Positive twin: an ordinary account's own entry by its SID; and another account's
// entry beside the Administrator's is still listed and removed.
func TestTheBuiltInAdministratorsOwnEntryIsNotAnotherReader(t *testing.T) {
	la, err := windows.StringToSid("LA")
	if err != nil {
		t.Fatal(err)
	}
	admin := accountOf(la.String())
	sd := func(s string) *windows.SECURITY_DESCRIPTOR {
		t.Helper()
		d, err := windows.SecurityDescriptorFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	const user = "S-1-5-21-1-2-3-1001"
	if owner, readers, readable := accessOf(sd("O:"+user+"D:PAI(A;;FA;;;"+user+")"), account{sid: user}); owner != nil || len(readers) > 0 || readable {
		t.Fatalf("positive twin: an account's own entry by its SID: owner %v, readers %v, readable %v", owner, readers, readable)
	}
	own := sd("O:LAD:PAI(A;;FA;;;LA)")
	t.Logf("the Administrator's own access list reads %s", own.String())
	owner, readers, readable := accessOf(own, admin)
	if owner != nil || len(readers) > 0 || readable {
		t.Fatalf("the Administrator's own entry was taken for another reader: owner %v, readers %v, readable %v", owner, readers, readable)
	}
	_, readers, readable = accessOf(sd("O:LAD:PAI(A;;FA;;;LA)(A;;FR;;;BU)"), admin)
	msg := secretFileRefusal(`C:\pw.txt`, admin, nil, readers, readable)
	users := accountOf("S-1-5-32-545")
	assure.Reached(t, len(readers) == 1 && readers[0].sid == users.sid &&
		strings.Contains(msg, `/remove:g "`+users.arg()+`"`) && !strings.Contains(msg, `/remove:g "`+admin.arg()+`"`) &&
		strings.Count(msg, admin.arg()) == 1, "only Users is listed and removed: "+msg)
}

// covers: MA-854, R37
// A secret file's remedy run in the Command Prompt it names ends the refusal even when
// the file's folder name holds $ and ` (the P3-8 fixes' pass, WINACL-3), where the
// same line typed into PowerShell names another path and fails. Positive twin: the
// file made this account's alone is read and opens the archive.
func TestASecretFileRemedyWorksInTheCommandPromptWithDollarSigns(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	dir := filepath.Join(t.TempDir(), "pw$dir `x")
	os.Mkdir(dir, 0o700)
	p := filepath.Join(dir, "pw.txt")
	if err := os.WriteFile(p, []byte(testPassword+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opens := func() bool {
		s, err := PasswordFile(p)
		if err != nil {
			t.Logf("read %s: %v", p, err)
			return false
		}
		u, err := Open(out, s, OpenOptions{})
		if err != nil {
			return false
		}
		u.Destroy()
		return true
	}
	if b, err := exec.Command("icacls", p, "/grant", "*S-1-5-32-545:R").CombinedOutput(); err != nil {
		t.Fatalf("could not grant Users: %v: %s", err, b)
	}
	s, err := PasswordFile(p)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(p, "Command Prompt (not PowerShell", "$ and `"),
		assure.Forbid("Command Prompt or PowerShell", testPassword), assure.NoSideEffect(func() bool { return s.text == "" }))
	var lines []string
	for _, l := range strings.Split(msg, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "icacls ") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("no command to run in %q", msg)
	}
	// Why not PowerShell: the same line names another path there and fails.
	if _, perr := typedIn("powershell", lines[0]); perr == nil {
		t.Fatalf("PowerShell ran %q without an error: the folder name was not read as something else", lines[0])
	}
	for _, line := range lines {
		runAsTyped(t, "cmd", line)
	}
	assure.Reached(t, opens(), "after the printed command, run in the Command Prompt, the file is read and opens the archive")
}
