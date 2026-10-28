package encrecord

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-853, R37
// The Windows refusal of a record copy another account owns (the P3-8 Windows lab,
// B11, carried to the record): it names the owner and this account by name, and its
// icacls commands give the copy back with /setowner — for the other copy too when
// another account owns it as well, so running them never meets a second refusal. It
// never says "%USERNAME%" (PowerShell does not expand it), nor the
// "/inheritance:r /grant:r" that changes no owner. An account with no name is
// given by its SID, marked "*" for icacls. The owner is read through the same seam
// the check uses, so this runs on any computer; the commands are run for real on
// Windows (remedy_windows_test.go).
func TestTheRecordsWindowsRefusalGivesItBackWithSetowner(t *testing.T) {
	const me, users = "S-1-5-21-1-2-3-1001", "S-1-5-32-545"
	names := map[string]string{me: `PC\alex`, users: `BUILTIN\Users`}
	wasName, wasOwner := accountName, instance.OwnerReader
	t.Cleanup(func() { accountName, instance.OwnerReader = wasName, wasOwner })
	accountName = func(sid string) string { return names[sid] }
	owners := map[string]string{}
	instance.OwnerReader = func(p string) (string, []string, error) {
		o, ok := owners[filepath.Base(p)]
		if !ok {
			return "", nil, errors.New("no such file")
		}
		return o, []string{me, "S-1-5-32-544"}, nil
	}

	dir := sandbox(t)
	add(t, idA, t.TempDir())
	p, m := filepath.Join(dir, FileName), filepath.Join(dir, MirrorName)
	before, _ := os.ReadFile(p)

	// Positive twin: both copies this account's, the record loads.
	owners[FileName], owners[MirrorName] = me, me
	if _, err := Load(); err != nil {
		t.Fatalf("a record this account owns: %v", err)
	}

	commands := func(msg string) []string {
		var cmds []string
		for _, l := range strings.Split(msg, "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "icacls ") {
				cmds = append(cmds, l)
			}
		}
		return cmds
	}
	refused := func() (*UnusableError, string) {
		t.Helper()
		_, err := Load()
		var ue *UnusableError
		if !errors.As(err, &ue) || ue.Case != CasePermissions {
			t.Fatalf("want the permissions case, got %v", err)
		}
		return ue, windowsOwnerRefusal(ue.Path, ue.Err)
	}
	unchanged := func() bool { after, _ := os.ReadFile(p); return bytes.Equal(before, after) }

	// Only the record is another account's: one command, for it.
	owners[FileName] = users
	ue, msg := refused()
	assure.Refused(t, code(ue), msg, assure.Code(1),
		assure.Names(p, `BUILTIN\Users`, `PC\alex`, "not yours", "Command Prompt (not PowerShell", "Run as administrator",
			`icacls "`+p+`" /setowner "PC\alex"`),
		assure.Forbid("%USERNAME%", "/grant", "/inheritance", m), assure.NoSideEffect(unchanged))
	if cmds := commands(msg); len(cmds) != 1 {
		t.Fatalf("one copy another account owns: want one command, got %q", cmds)
	}

	// Both copies are: both are given back, one after the other.
	owners[MirrorName] = users
	ue, msg = refused()
	assure.Refused(t, code(ue), msg, assure.Code(1),
		assure.Names(`icacls "`+p+`" /setowner "PC\alex"`, `icacls "`+m+`" /setowner "PC\alex"`, "one after the other", "run them"),
		assure.Forbid("%USERNAME%"), assure.NoSideEffect(unchanged))
	if cmds := commands(msg); len(cmds) != 2 {
		t.Fatalf("both copies another account's: want two commands, got %q", cmds)
	}

	// No name to be had: the SID, marked for icacls.
	names = map[string]string{}
	owners[MirrorName] = me
	ue, msg = refused()
	assure.Refused(t, code(ue), msg, assure.Code(1),
		assure.Names("an account with no name here ("+users+")", `/setowner "*`+me+`"`), assure.NoSideEffect(unchanged))
}
