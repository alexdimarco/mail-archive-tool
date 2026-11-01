//go:build windows

package archdelete

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/nowindow"
)

// covers: MA-959, R36
func TestAJunctionMidPathRefusesTheDeleteOnWindows(t *testing.T) {
	// Positive twin first: a real folder is planned.
	mustPlan(t, linkArchive(t, true), testDeps())

	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: "Mailbox/Inbox/2024-03-01_0900_x_ab12cd34.html"}})
	writeFile(t, filepath.Join(dir, "Docs", "2024-03-01_0900_x_ab12cd34.html"), "the person's page")
	if err := os.MkdirAll(filepath.Join(dir, "Mailbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "Mailbox", "Inbox")
	// A junction needs no privilege; mklink is cmd.exe's.
	out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", junction, filepath.Join(dir, "Docs"))).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a junction (lab): %v %s", err, out)
	}
	before := tree(t, dir)
	rc, msg, c := refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", junction),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) }))
	if c != CaseLink {
		t.Fatalf("case = %s", c)
	}
}
