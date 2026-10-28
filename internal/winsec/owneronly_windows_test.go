//go:build windows

package winsec

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// checkPath is CheckSecretFile on the file at path.
func checkPath(t *testing.T, path string) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return CheckSecretFile(windows.Handle(f.Fd()))
}

// covers: MA-855, R37
// A secret file MailArchive writes is this user's alone and owned by them (P3-8's CI
// run on windows-latest; T2-16): an administrator running elevated — or SYSTEM, as the
// lab runs — makes files Administrators own, which CheckSecretFile refuses as not
// theirs (design §3.4: the owner is the user), so MailArchive's own saved recovery key
// did not read back. RestrictToOwner now sets the owner too, and the file reads back.
// Where new files are already the user's (an unelevated user), the refusal before is
// not there to see, and the test says so.
func TestASecretFileMailArchiveWritesIsTheUsersOwn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "recovery-key.txt")
	if err := os.WriteFile(p, []byte("a saved recovery key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if checkPath(t, p) == nil {
		t.Log("new files are already this user's here (not elevated): the refusal before RestrictToOwner is not seen")
	}
	if err := RestrictToOwner(p); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, checkPath(t, p) == nil, "the file MailArchive restricted is this user's alone, and reads back")
}
