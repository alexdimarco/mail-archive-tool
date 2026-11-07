package tbengine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// bornFS looks at each folder and file the writer makes the instant its create returns,
// before anything else (FS.Private included) touches it: that instant is the window
// another account raced in R7-01.
type bornFS struct {
	tbrestore.FS
	local string
	look  func(path string)
}

func (b *bornFS) Mkdir(n string) error {
	err := b.FS.Mkdir(n)
	if err == nil {
		b.look(filepath.Join(b.local, filepath.FromSlash(n)))
	}
	return err
}

func (b *bornFS) Create(n string) (tbrestore.WFile, error) {
	w, err := b.FS.Create(n)
	if err == nil {
		b.look(filepath.Join(b.local, filepath.FromSlash(n)))
	}
	return w, err
}

// covers: MA-1128, R37, R40, S72
// R7-01 (settled 2026-11-07, the lead's call): every folder and file a restore makes is
// readable by this account alone FROM ITS CREATION, not only once FS.Private has run.
// Local Folders is given an inheritable "others may read" entry (macOS "everyone",
// Windows BUILTIN\Users, as a second drive has by default), and on macOS and Linux the
// umask is 0, so nothing narrows what the create asks for. Each entry is read with the
// OS's own tool the instant its create returns: none may carry an entry or mode letting
// another account list or read it — the container, the marker, the made-list, each
// folder's file and subfolder, and the parent file made at the commit. Before the fix
// the container was born with Local Folders' entry (macOS and Windows), the window a
// second account won 5 times of 5 on the VM. Both an encrypted and a plain archive.
func TestEverythingARestoreMakesIsPrivateFromItsCreation(t *testing.T) {
	was := setUmask(0)
	defer setUmask(was)
	encOut, u := encryptedArchive(t)
	for _, enc := range []bool{true, false} {
		pr := newProfile(t, "")
		grantOthersRead(t, pr.local)
		var looked, open []string
		fsWas := fsFor
		fsFor = func(r *os.Root) tbrestore.FS {
			return &bornFS{FS: tbrestore.NewRootFS(r), local: pr.local, look: func(p string) {
				looked = append(looked, p)
				if why := othersMayRead(t, p); why != "" {
					open = append(open, p+": "+why)
				}
			}}
		}
		var p *Plan
		if enc {
			p = plan(t, Request{Out: encOut, Keys: u, Profile: pr.choice()})
		} else {
			p = plan(t, Request{Out: plainArchive(t, true), Profile: pr.choice()})
		}
		r := mustRun(t, p, Start{NotEncryptedOK: enc})
		fsFor = fsWas
		assure.Reached(t, r.Done(), fmt.Sprintf("enc=%v: the restore finished: %s", enc, r.Words))
		hasParent := false
		for _, l := range looked {
			hasParent = hasParent || filepath.Base(l) == r.Write.Top
		}
		assure.Reached(t, len(looked) >= 6 && hasParent, fmt.Sprintf("enc=%v: every create was looked at, the parent file too: %v", enc, looked))
		if len(open) > 0 {
			t.Fatalf("enc=%v: born readable by other accounts:\n%s", enc, strings.Join(open, "\n"))
		}
		for _, e := range restoredEntries(t, pr.local, r.Write.Top) {
			if why := othersMayRead(t, e); why != "" {
				t.Fatalf("enc=%v: %s can be read by other accounts after the restore:\n%s", enc, e, why)
			}
		}
	}
}
