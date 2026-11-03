//go:build windows

package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// heldMarker opens the marker in dir as another program would, at step 6's first removal: without
// share-delete (most programs), or with it (an indexer, an antivirus). The handle is closed when the
// test ends, or earlier by the returned func.
func heldMarker(t *testing.T, dir string, shareDelete bool) (closeIt func()) {
	t.Helper()
	h := windows.InvalidHandle
	atFirstRemoval(t, func() {
		p, err := windows.UTF16PtrFromString(filepath.Join(dir, delmark.Name))
		if err != nil {
			t.Fatal(err)
		}
		share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
		if shareDelete {
			share |= windows.FILE_SHARE_DELETE
		}
		h, err = windows.CreateFile(p, windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatalf("open the marker: %v", err)
		}
	})
	closeIt = func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
			h = windows.InvalidHandle
		}
	}
	t.Cleanup(closeIt)
	return closeIt
}

// covers: MA-1047, R36
// A marker another program holds open when the delete removes it (§15.2; the slice-3 pass's probe,
// run on the VM, kept as a test; it corrects the builder's "reasoned, not run" note, LOW-4):
//   - held without share-delete: a name the archive owns that could not be removed — "Not fully
//     deleted. 1 file could not be removed: .mailarchive-deleting.json." — the row "Delete not
//     finished", another computer's backup refused in the marker-only words; once the program lets
//     go, Delete again ends complete, the marker gone, the person's files kept;
//   - held with share-delete: the delete is complete and the person's files kept (NTFS removes the
//     name at once; a volume that leaves it "delete pending" keeps the folder and still ends
//     complete), and once the program lets go no marker is left.
//
// Positive twin first: the same delete with nothing holding it ends complete.
func TestMA1047_AMarkerHeldOpenOnWindows(t *testing.T) {
	oldPause := retryPause
	t.Cleanup(func() { retryPause = oldPause })
	retryPause = 5 * time.Millisecond
	if res := newRig(t, plainBesidePersonsFiles(t)).run(t, false); res.Status != StatusComplete {
		t.Fatalf("positive twin: %s", res.Message())
	}

	t.Run("without share-delete", func(t *testing.T) {
		dir := plainBesidePersonsFiles(t)
		r := newRig(t, dir)
		closeIt := heldMarker(t, dir, false)
		res := r.run(t, false)
		if res.Status != StatusPartial || !res.Unfinished || !strings.HasPrefix(res.Lines[0], "Not fully deleted. 1 file could not be removed: "+delmark.Name+".") {
			t.Fatalf("%s", res.Message())
		}
		before := ma1048Names(dir)
		err := ma1048Backup(dir, filepath.Join(t.TempDir(), LogName), ma1046Mbox(t))
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+dir+")"),
			assure.NoSideEffect(func() bool { return strings.Join(before, "\x00") == strings.Join(ma1048Names(dir), "\x00") }))
		closeIt()
		if res := r.run(t, false); res.Status != StatusComplete {
			t.Fatalf("Delete again: %s", res.Message())
		}
		if exists(filepath.Join(dir, delmark.Name)) || !exists(filepath.Join(dir, "taxes.pdf")) {
			t.Fatal("the marker gone, the person's files kept")
		}
	})
	t.Run("with share-delete", func(t *testing.T) {
		dir := plainBesidePersonsFiles(t)
		r := newRig(t, dir)
		closeIt := heldMarker(t, dir, true)
		res := r.run(t, false)
		if res.Status != StatusComplete || !exists(filepath.Join(dir, "taxes.pdf")) {
			t.Fatalf("%s", res.Message())
		}
		closeIt()
		if exists(filepath.Join(dir, delmark.Name)) {
			t.Fatal("no marker once the program lets go")
		}
		if _, err := os.Lstat(filepath.Join(dir, "taxes.pdf")); err != nil {
			t.Fatal(err)
		}
	})
}
