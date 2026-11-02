package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/nowindow"
)

// cliLinkFolder makes link a link to the folder target: a junction on Windows, a symbolic link
// elsewhere (as internal/desktop's linkFolder).
func cliLinkFolder(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", link, target)).CombinedOutput()
		if err != nil {
			t.Fatalf("could not make a junction %s -> %s: %v %s", link, target, err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatalf("could not make a link %s -> %s: %v", link, target, err)
	}
}

// covers: MA-1027, R36, R18
// The command `status` gives to finish a delete names the row the delete was begun from, the
// spelling `delete-archive -out` finds it by, not the spelling status was given (the 1.5.3 friction
// fixes' adversarial pass, finding 10): asked through another path to the same folder, it still
// names the row. Positive twin first: asked by the row's own spelling, it names that.
func TestStatusNamesTheRowToFinishADelete(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Half Deleted")
	release := func() {}
	beforeDeleteStep = func(step int) {
		if step == 4 {
			release = e.desktopRunning("2")
		}
	}
	var out bytes.Buffer
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Half Deleted"}}, dir, "", "", &out)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 {
		t.Fatalf("the delete should stop part way: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
	release()
	want := "run mailarchive delete-archive -out " + encmark.CommandPath(dir) + " again"

	// Positive twin: the row's own spelling.
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, want) {
		t.Fatalf("the positive twin:\n%s", got)
	}

	// Another path to the same folder.
	alias := filepath.Join(t.TempDir(), "alias")
	cliLinkFolder(t, filepath.Dir(dir), alias)
	other := filepath.Join(alias, filepath.Base(dir))
	got, code := statusOf(t, other)
	if code != 0 || !strings.Contains(got, want) || strings.Contains(got, "delete-archive -out "+encmark.CommandPath(other)) {
		t.Fatalf("status through %s:\n%s", other, got)
	}
}
