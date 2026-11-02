package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1014, R36
// At the terminal, writing the Desktop's row into Your archives (CommandLineDelete.ListRow) takes
// the list's lock (the delete residuals' security pass, CON-07): while another MailArchive program
// holds it past the bound, delete-archive and unlist-archive of a row only the Desktop draws are
// refused, exit 1, "Your archives (FILE) is being changed by another MailArchive window, so nothing
// was deleted; try again" (unlist-archive: "nothing was changed"), the folder, the list, the
// settings and the wizard's note unchanged. Positive twin first: with the lock free, such a row
// is deleted and removed from the list.
func TestTheTerminalWaitsForAnotherProgramsChangeOfYourArchives(t *testing.T) {
	e := newCLIDel(t)
	setOut := func(out string) {
		t.Helper()
		if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = out }); err != nil {
			t.Fatal(err)
		}
	}

	// Positive twin: the Desktop's current archive, not in its list yet, is deleted.
	free := e.plainArchive("Archive F")
	e.unlistForTest(free)
	setOut(free)
	var out bytes.Buffer
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive F"}}, free, "", "", &out); err != nil {
		t.Fatalf("positive twin: delete-archive with the list's lock free: %v\n%s", err, out.String())
	}
	if e.listed(free) {
		t.Fatal("positive twin: the row is still in Your archives")
	}

	held, err := lockfile.Acquire(archindex.LockPath(e.indexPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	// delete-archive: refused before anything changed.
	d := e.plainArchive("Archive D")
	e.unlistForTest(d)
	setOut(d)
	listBefore, _ := os.ReadFile(e.indexPath())
	beforeD := tree(t, d)
	out.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive D"}}, d, "", "", &out)
	afterD := tree(t, d)
	delete(afterD, filepath.Join(d, ".mailarchive.lock"))
	assure.Refused(t, exitCode(err), out.String()+errText(err), assure.Code(1),
		assure.Names("Your archives ("+e.indexPath()+") is being changed by another MailArchive window, so nothing was deleted; try again"),
		assure.NoSideEffect(func() bool {
			l, _ := os.ReadFile(e.indexPath())
			return bytes.Equal(listBefore, l) && sameTree(beforeD, afterD) && desktop.LoadSettings(e.settingsPath()).Out == d
		}))

	// unlist-archive of the wizard's last archive, not in the list yet: refused, nothing changed.
	setOut("")
	w := e.plainArchive("Archive W")
	e.unlistForTest(w)
	lastFile, err := jobfile.LastArchiveFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lastFile, []byte(w+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listBefore, _ = os.ReadFile(e.indexPath())
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, w, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("Your archives ("+e.indexPath()+") is being changed by another MailArchive window, so nothing was changed; try again"),
		assure.NoSideEffect(func() bool {
			l, _ := os.ReadFile(e.indexPath())
			return bytes.Equal(listBefore, l) && jobfile.ReadLastArchive(lastFile) == w
		}))
}
