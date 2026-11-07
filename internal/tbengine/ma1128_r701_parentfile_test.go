package tbengine

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1128, R37, R40, S72
// R7-01 independent review F4 (settled 2026-11-07, the lead's call): the parent file the
// commit makes is read back by FS.Private like every other entry the restore makes. When
// that read-back fails for an encrypted archive whose plan expected the drive to hold
// owner-only (MustBePrivate), the write stops as it does for any other entry: the parent
// file, the very one this restore just made (named by its own open handle), is removed,
// then the one cleanup; the words say the files could not be made readable only by this
// account, and nothing is left. A plain archive's restore asks the parent file too, and
// passes the failure over.
func TestTheParentFileIsReadBackLikeEveryOtherEntry(t *testing.T) {
	failParent := errors.New("the access list could not be set")
	for _, enc := range []bool{true, false} {
		pr := newProfile(t, "")
		var p *Plan
		if enc {
			out, u := encryptedArchive(t)
			p = plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
		} else {
			p = plan(t, Request{Out: plainArchive(t, true), Profile: pr.choice()})
		}
		var asked []string
		fsWas := fsFor
		fsFor = func(r *os.Root) tbrestore.FS {
			return &faultFS{inner: tbrestore.NewRootFS(r), privateFor: func(n string) error {
				asked = append(asked, n)
				if n == p.Top {
					return failParent
				}
				return nil
			}}
		}
		before := filesOf(t, pr.dir)
		r := mustRun(t, p, Start{NotEncryptedOK: enc})
		fsFor = fsWas
		parentAsked := false
		for _, n := range asked {
			parentAsked = parentAsked || n == p.Top
		}
		assure.Reached(t, parentAsked, fmt.Sprintf("enc=%v: FS.Private was asked of the parent file %q: %v", enc, p.Top, asked))
		if enc {
			assure.Refused(t, map[bool]int{true: 1}[!r.Done()], r.Words, assure.Code(1),
				assure.Names("readable only by your account", pr.local, "nothing was added"),
				assure.NoSideEffect(func() bool { return filesOf(t, pr.dir) == before }))
			assure.Reached(t, noFolderNames(r.Words), "the words name no folder of the encrypted archive: "+r.Words)
		} else {
			assure.Reached(t, r.Done(), "a plain archive's restore passes the failure over: "+r.Words)
		}
	}
}
