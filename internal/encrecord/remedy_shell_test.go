package encrecord

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-854, R37
// The record's Windows remedy is for a Command Prompt only, and says so (the P3-8
// fixes' pass, WINACL-3): PowerShell reads $ and ` inside double quotes, so for an
// account such as PC\dev$ops the printed /setowner "PC\dev$ops" would give the record
// to PC\dev there. The command keeps the name exactly; the advice names the Command
// Prompt and says not PowerShell, and why. The owner is read through the check's own
// seam, so this runs on any computer; the command is run for real in a folder whose
// name holds $ and ` on Windows (remedy_shell_windows_test.go).
func TestTheRecordsRemedyIsForTheCommandPromptOnly(t *testing.T) {
	const me, users = "S-1-5-21-1-2-3-1001", "S-1-5-32-545"
	names := map[string]string{me: `PC\dev$ops`, users: `BUILTIN\Users`}
	wasName, wasOwner := accountName, instance.OwnerReader
	t.Cleanup(func() { accountName, instance.OwnerReader = wasName, wasOwner })
	accountName = func(sid string) string { return names[sid] }
	owners := map[string]string{}
	instance.OwnerReader = func(p string) (string, []string, error) {
		o, ok := owners[filepath.Base(p)]
		if !ok {
			return "", nil, errors.New("no such file")
		}
		return o, []string{me}, nil
	}
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	p := filepath.Join(dir, FileName)
	before, _ := os.ReadFile(p)

	// Positive twin: the record this account owns loads.
	owners[FileName], owners[MirrorName] = me, me
	if _, err := Load(); err != nil {
		t.Fatalf("a record this account owns: %v", err)
	}
	owners[FileName] = users
	_, err := Load()
	var ue *UnusableError
	if !errors.As(err, &ue) || ue.Case != CasePermissions {
		t.Fatalf("want the permissions case, got %v", err)
	}
	msg := windowsOwnerRefusal(ue.Path, ue.Err)
	assure.Refused(t, code(ue), msg, assure.Code(1),
		assure.Names("run this in a Command Prompt (not PowerShell", "$ and `", `/setowner "PC\dev$ops"`),
		assure.Forbid("Command Prompt or PowerShell"),
		assure.NoSideEffect(func() bool { after, _ := os.ReadFile(p); return bytes.Equal(before, after) }))
}
