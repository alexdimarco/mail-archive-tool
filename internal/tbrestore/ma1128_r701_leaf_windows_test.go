//go:build windows

package tbrestore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1128, R40
// R7-01 independent review F2 (settled 2026-11-07, the lead's call): FS's own promise — no
// name, link or ".." reaches outside Local Folders — holds for the last part of a name too.
// createAt hands that part to NtCreateFile as a name relative to its checked parent, and
// NT reads '\' as a folder separator and ':' as a file's stream, so such a part is refused
// before anything is made: through a junction standing in the restore's own folder
// ("C.sbd/J\escaped", J leading outside Local Folders) nothing is made outside; a stream
// on a file this restore did not make ("Trash:stream") is not written. Every name the
// writer passes is SafeName's today, so this is the FS layer's own guard. Positive twin
// first: an ordinary name in the same folder is made.
func TestAWindowsNamePartThatIsAPathOrAStreamIsRefused(t *testing.T) {
	_, local := localProfile(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(local, "Trash"), "From - x\nSubject: old\n\nkept\n")
	if err := os.Mkdir(filepath.Join(local, "C.sbd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := makeLink("junction", filepath.Join(local, "C.sbd", "J"), outside, ""); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(local)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := newRootFS(root)

	w, err := f.Create("C.sbd/ok")
	assure.Reached(t, err == nil, "an ordinary name is made")
	if err == nil {
		w.Close()
	}
	assure.Reached(t, f.Mkdir("C.sbd/okdir") == nil, "an ordinary folder name is made")

	beforeOut := snapshot(t, outside)
	beforeTrash := snapshot(t, local, "C.sbd")
	for _, tc := range []struct {
		name string
		dir  bool
	}{
		{`C.sbd/J\escaped`, false},
		{`C.sbd/J\escdir`, true},
		{"Trash:stream", false},
		{"C.sbd/ok:stream", false},
		{"C.sbd/okdir:stream", true},
	} {
		var err error
		if tc.dir {
			err = f.Mkdir(tc.name)
		} else {
			var w wfile
			if w, err = f.Create(tc.name); err == nil {
				w.Close()
			}
		}
		rc, msg := 0, ""
		if err != nil {
			msg = err.Error()
			if errors.Is(err, windows.ERROR_INVALID_NAME) {
				rc = 1
			}
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(tc.name),
			assure.NoSideEffect(func() bool {
				_, serr := os.Lstat(filepath.Join(local, filepath.FromSlash(tc.name)))
				return snapshot(t, outside) == beforeOut && snapshot(t, local, "C.sbd") == beforeTrash && errors.Is(serr, os.ErrNotExist)
			}))
	}
}
