package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// The PG pass's follow-up on the P3-4 fix round (docs/review-encrypted-archives-phase3-pg-adversarial.md).

// covers: MA-846, R37
// A name the copy keeps for itself is kept under full case folding (FIX-04): "ſearch.db"
// is search.db on a volume that folds case, so its message is left out and listed —
// the copy is not failed.
func TestReservedNamesAreKeptUnderUnicodeFolding(t *testing.T) {
	good := copyMsg("good")
	long := fakeMsg{path: "ſearch.db/m.html", files: map[string][]byte{".html": []byte("x")}}
	f := newFakeCopy(t, good, long)
	rep, p, err := copyOf(t, f, nil, nil)
	problems, _ := os.ReadFile(filepath.Join(p.Dest, CopyProblemsName))
	assure.Reached(t, err == nil && rep.Skipped == 1 && rep.Copied == 1 && strings.Contains(string(problems), "keeps for its own files"),
		fmt.Sprintf("left out, not failed: %+v %v", rep, err))
}

// covers: MA-846, R37
// A rebuilt index is judged small by what it indexes, never by the image it replaces
// (FIX-05): an empty image marked behind, rebuilt over messages larger than the memory
// budget, is opened as any index.
func TestARebuiltIndexIsJudgedByWhatItIndexes(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"))
	f.behind = true
	ob, oo := copyIndexMemory, copyIndexOpen
	copyIndexMemory = 16 << 20 // above the empty image, below what the rebuild indexes (its reserve alone is 32 MiB)
	var smalls []bool
	copyIndexOpen = func(path string, small bool) (*index.Index, error) {
		smalls = append(smalls, small)
		return oo(path, small)
	}
	defer func() { copyIndexMemory, copyIndexOpen = ob, oo }()
	rep, _, err := copyOf(t, f, nil, nil)
	assure.Reached(t, err == nil && rep.Index == "rebuilt" && len(smalls) == 1 && !smalls[0], fmt.Sprintf("judged %v", smalls))
}

// covers: MA-846, R37
// A mark that cannot be removed leaves the copy unfinished like any failure (FIX-06):
// no manifest, the folder named.
func TestAMarkThatCannotBeRemovedLeavesNoManifest(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"))
	e := planEnv()
	p, err := e.PlanDecryptCopy(f.out, tmpDir(t), nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	e.faults = &copyFaults{hook: func(step string) {
		if step == "decrypt-unmark" {
			mark := filepath.Join(p.Dest, CopyMarkerName)
			os.Remove(mark)
			os.MkdirAll(filepath.Join(mark, "x"), 0o700) // a folder with something in it
		}
	}}
	rep, err := e.DecryptSealed(context.Background(), p, nil, nil, f, nil, nil)
	_, man := os.Stat(filepath.Join(p.Dest, plainManifestName))
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names(p.Dest, "unfinished"),
		assure.NoSideEffect(func() bool { return errors.Is(err, ErrCopyFailed) && rep.Unfinished && errors.Is(man, fs.ErrNotExist) }))
}

// covers: MA-846, R37
// Another archive this computer knows, that the copy cannot be shown to be outside of,
// refuses it naming that archive and how to stop it blocking (FIX-02, CRIT-07).
func TestAKnownArchiveThatCannotBeCheckedIsNamed(t *testing.T) {
	out := sealedArchive(t)
	parent := tmpDir(t)
	known := tmpDir(t)
	e := planEnv()
	e.view = func(p string) ceView {
		if filepath.Base(p) == filepath.Base(known) {
			return ceView{Kind: viewStacking} // a FUSE mount: identities cannot be compared
		}
		return ceView{Kind: viewLocal}
	}
	knownArchives = func() []string { return []string{known} }
	defer func() { knownArchives = func() []string { return nil } }()
	_, err := e.PlanDecryptCopy(out, parent, nil, nil, planDay)
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names(known, "remove it from Your archives"),
		assure.NoSideEffect(func() bool { entries, _ := os.ReadDir(parent); return len(entries) == 0 }))
}
