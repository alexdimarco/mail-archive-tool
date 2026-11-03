//go:build unix

package archdelete

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/state"
)

// covers: MA-1049, R36
// A pipe at the name is never opened (the test fails on a timeout, not a hang): every backup is
// refused in the not-regular words and Delete's plan in MsgPlanNotRegular's; a marker of mode
// 0000 is a marker all the same (presence needs no read permission), in the marker-only words;
// a folder that cannot be searched (0000) refuses with the "could not check" words, kind
// archive_unwritable. Positive twin first: the same archive without them is backed up.
func TestMA1049_APipeAnUnreadableMarkerAndAFolderThatCannotBeSearched(t *testing.T) {
	half, _, mbox := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	os.Remove(filepath.Join(half, delmark.Name))
	dir := smallArchive(t, t.TempDir())

	// Positive twin.
	if err := ma1048Backup(half, logPath, mbox); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	// A pipe.
	for _, d := range []string{half, dir} {
		if err := syscall.Mkfifo(filepath.Join(d, delmark.Name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	ma1049Within(t, "the backup", func() { err = ma1048Backup(half, logPath, mbox) })
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgNotRegular+" (Folder: "+half+")"))
	before := statTree(t, dir)
	var perr error
	ma1049Within(t, "the plan", func() { _, perr = Plan(dir, testDeps()) })
	r := AsRefusal(perr)
	assure.Refused(t, refusalCode(perr), errText(perr), assure.Code(1), assure.Names(delmark.MsgPlanNotRegular),
		assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) }))
	if r == nil || r.Case != CaseMarkerNotRegular {
		t.Fatalf("the plan's case: %+v", r)
	}
	os.Remove(filepath.Join(half, delmark.Name))

	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind the superuser")
	}
	// Mode 0000.
	if err := os.WriteFile(filepath.Join(half, delmark.Name), []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	err = ma1048Backup(half, logPath, mbox)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"))
	os.Remove(filepath.Join(half, delmark.Name))

	// A folder that cannot be searched.
	if err := os.Chmod(half, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(half, 0o755) })
	err = app.RefuseUnfinishedDelete(half, logPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not check this archive's folder for a delete that did not finish (permission denied), so it was not backed up; nothing was changed. Check that the drive or folder can be read, then try again. (Folder: "+half+")"))
	if k := app.FailureKind(err); k != state.FailArchiveUnwritable || !app.RefusedAsNoArchive(err) {
		t.Errorf("kind %q, taken back %v", k, app.RefusedAsNoArchive(err))
	}
}
