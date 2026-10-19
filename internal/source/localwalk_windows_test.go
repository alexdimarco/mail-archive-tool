//go:build windows

package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-322, R25, R10, S47
// Windows, unprivileged (FC1): a junction made with `mklink /J` for a maildir's cur/ is a
// directory link: it is not followed (the mail behind it is not read) and it is recorded as
// a terminal symlinked-folder-skipped gap. A junction needs no privilege to make; the real
// OneDrive placeholder next to it is the lab row MA-503. This file compiles and vets on
// every OS; it runs only on Windows.
func TestWindowsJunctionIsADirectoryLinkAndIsRecorded(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "elsewhere", "cur")
	writeLocal(t, filepath.Join(target, "m:2,"), localMail("behind-the-junction"))
	store := filepath.Join(parent, "store")
	writeLocal(t, filepath.Join(store, "Inbox", "cur", "a:2,"), localMail("a"))
	writeLocal(t, filepath.Join(store, "Other", "new", "n:2,"), localMail("n"))
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(store, "Other", "cur"), target).CombinedOutput(); err != nil {
		t.Skipf("no junction here: %v: %s", err, out)
	}
	if _, err := os.Lstat(filepath.Join(store, "Other", "cur")); err != nil {
		t.Fatal(err)
	}
	got := walkLocal(t, store, OpenOptions{})
	for _, m := range got.msgs {
		if m.Subject == "behind-the-junction" {
			t.Error("the walk followed a junction")
		}
	}
	f, ok := got.fault("symlinked-folder-skipped")
	if !ok || f.Class != state.ClassTerminal || f.Locator != "folder:Other/cur" {
		t.Errorf("junction fault %+v, %v; want a terminal symlinked-folder-skipped at folder:Other/cur", f, ok)
	}
}
