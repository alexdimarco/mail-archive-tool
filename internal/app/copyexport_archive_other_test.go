package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// covers: MA-512, R12, S52
// R-f2's archive-side wording for another archive folder (design §7.6, RR2-C13) carries what, why and
// remedy: the folder it names, the view it cannot compare (a share on this computer, an sshfs or other
// FUSE mount) and the remedy on the Another archive folder control; and none of it holds a token, a
// Graph URL query, "panic" or "goroutine" (assure's forbid list).
func TestAnotherArchiveFolderRf2WordingCarriesWhatWhyAndRemedy(t *testing.T) {
	_, other, dest := destFixture(t)
	archives := []string{t.TempDir(), other}
	e := newCEEnv(t)
	e.served = func(s string) bool { return s == "thiscomputer" }
	otherReal := util.ResolveExisting(other)
	viewOf := func(v ceView) func(string) ceView {
		return func(p string) ceView {
			if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(otherReal)) {
				return v
			}
			return ceView{Kind: viewLocal}
		}
	}
	forbid := assure.Forbid("panic", "goroutine", "access_token", "refresh_token", "graph.microsoft.com")
	for name, c := range map[string]struct {
		view  ceView
		names []string
	}{
		"an sshfs mount":           {ceView{Kind: viewStacking, Type: "fuse.sshfs"}, []string{"The archive folder " + other, "sshfs or other FUSE mount", "can't compare folder identities", "Another archive folder…", "SMB or NFS client"}},
		"a share on this computer": {ceView{Kind: viewShare, Server: "thiscomputer", Mount: "//thiscomputer/mail"}, []string{"The archive folder " + other, "share on this computer", "can't confirm any folder is outside it", "Another archive folder…", "local path", "Preview copy export again"}},
	} {
		e.view = viewOf(c.view)
		_, err := e.CheckDestinationOther(dest, archives, other)
		var ref *CopyRefusal
		if !errors.As(err, &ref) || ref.ID != "R-f2" {
			t.Fatalf("%s: %v, want R-f2", name, err)
		}
		assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(c.names...), forbid)
	}
}

// covers: MA-531, MA-414, MA-434, R29, R4, R12, S52
// Another archive folder (ruling W1, RR2-C13): the destination stays outside it, by path and by
// identity, and Create folder's parent too. Beside the configured archive, positive twins first (a
// folder with its own identity is accepted); a destination inside the folder, one reached through
// a symlink into it, and one that is an alias of a folder inside it (the identity seam, a bind
// mount's stand-in) are R-f with nothing written under the folder; a view that cannot be compared
// is R-f2 in words that name that folder; a Create folder whose parent is such an alias is R-f and
// makes no directory.
func TestAnotherArchiveFolderDestinationsAreChecked(t *testing.T) {
	_, other, dest := destFixture(t)
	configured := t.TempDir()
	e := newCEEnv(t)
	archives := []string{configured, other}
	own := idOfDir(t, dest)
	e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{own}, nil }
	d0, o0 := lsTree(t, dest), lsTree(t, other)

	if _, err := e.CheckDestinationOther(dest, archives, other); err != nil {
		t.Fatalf("the positive twin, a folder outside both: %v", err)
	}

	inside := filepath.Join(other, "store1", "Inbox")
	_, err := e.CheckDestinationOther(inside, archives, other)
	refusedWith(t, err, "R-f", []string{"inside your archive", other})

	link := filepath.Join(filepath.Dir(dest), "alias")
	if err := os.Symlink(inside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err = e.CheckDestinationOther(link, archives, other)
	refusedWith(t, err, "R-f", []string{"inside your archive", other})

	// an alias by identity: a bind mount of <folder>/store1 stands here as the identity seam
	e.destIDs = func(string) ([]ceIdentity, error) {
		return []ceIdentity{own, idOfDir(t, filepath.Join(other, "store1"))}, nil
	}
	_, err = e.CheckDestinationOther(dest, archives, other)
	refusedWith(t, err, "R-f", []string{"inside your archive", other})
	e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{own}, nil }

	// views that cannot be compared name the folder, with the remedy on the Another archive folder control
	e2 := newCEEnv(t)
	e2.served = func(s string) bool { return s == "thiscomputer" }
	otherReal := util.ResolveExisting(other)
	e2.view = func(p string) ceView {
		if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(otherReal)) {
			return ceView{Kind: viewStacking, Type: "fuse.sshfs"}
		}
		return ceView{Kind: viewLocal}
	}
	_, err = e2.CheckDestinationOther(dest, archives, other)
	refusedWith(t, err, "R-f2", []string{"The archive folder " + other, "sshfs", "Another archive folder…", "SMB or NFS client"})
	e2.view = func(p string) ceView {
		if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(otherReal)) {
			return ceView{Kind: viewShare, Server: "thiscomputer", Mount: "//thiscomputer/mail"}
		}
		return ceView{Kind: viewLocal}
	}
	_, err = e2.CheckDestinationOther(dest, archives, other)
	refusedWith(t, err, "R-f2", []string{"The archive folder " + other, "share on this computer", "local path"})

	// Create folder: the parent is checked the same way, and nothing is made on a refusal
	_, err = e.CreateFolderOther(link, "new", archives, other)
	refusedWith(t, err, "R-f", []string{"inside your archive", other})
	e.destIDs = func(string) ([]ceIdentity, error) {
		return []ceIdentity{own, idOfDir(t, filepath.Join(other, "store1"))}, nil
	}
	_, err = e.CreateFolderOther(dest, "new", archives, other)
	refusedWith(t, err, "R-f", []string{"inside your archive", other})
	if d1, o1 := lsTree(t, dest), lsTree(t, other); !reflect.DeepEqual(d1, d0) || !reflect.DeepEqual(o1, o0) {
		t.Errorf("a refusal wrote something: destination %v -> %v, the folder %v -> %v", d0, d1, o0, o1)
	}
	e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{own}, nil }
	made, err := e.CreateFolderOther(dest, "new", archives, other)
	if err != nil {
		t.Fatalf("the positive twin of Create folder: %v", err)
	}
	if fi, serr := os.Stat(made); serr != nil || !fi.IsDir() {
		t.Errorf("Create folder made nothing at %s", made)
	}
	if o1 := lsTree(t, other); !reflect.DeepEqual(o1, o0) {
		t.Errorf("Create folder added something under the folder: %v", o1)
	}
}
