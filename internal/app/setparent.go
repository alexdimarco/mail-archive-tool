package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// A set's parent folder (docs/design-per-mailbox-archives.md §6, revision 2's C9, C14 and C15,
// revision 2.1's F-04; MA-1493): the one folder the wizard makes an archive folder in for each
// mailbox. It is judged once per set, as the unencrypted copy judges "a new folder inside a chosen
// parent" (checkDecryptPaths, then the destination check against every archive found above it and
// every archive this computer knows, each walked once). Each archive folder is then made through a
// handle on the parent, with that checked result re-proved against the folder the handle holds,
// never with a new walk, so a parent swapped for a link after the walk cannot move a new folder into
// an archive.

// SetParent is a parent folder checked for a set of archives: what MakeSetFolder re-proves before
// each folder it makes.
type SetParent struct {
	Parent  string
	outside *CopyOutside
	key     ceDirKey // the folder Parent named when it was checked
}

// CheckSetParent judges parent as the folder a set's archive folders are made in: an absolute,
// clean path to an existing folder (R-e); not an encrypted archive nor inside one (R-enc); not one
// of MailArchive's own folders nor inside one (R-set); and not an archive nor inside one, plaintext
// or encrypted, found by walking up from it or among the archives this computer knows (knownArchives,
// which holds the wizard's made-list), by path (R-f) and by identity, or R-f2 where identity cannot
// decide. The refusals are the copy's *CopyRefusal, by ID: their words speak of the unencrypted
// copy, so a front end words each ID its own way. It writes nothing.
func (e *CopyExportEnv) CheckSetParent(parent string) (*SetParent, error) {
	all, known, err := e.checkDecryptPaths(parent, "", nil, nil)
	if err != nil {
		return nil, err
	}
	outside, err := e.CheckDestinationOther(parent, all, "")
	if err != nil {
		return nil, err
	}
	// The other archives this computer knows, each on its own, as the copy checks them.
	for _, k := range known {
		o, err := e.CheckDestinationOther(parent, []string{k}, "")
		if err != nil {
			return nil, err
		}
		outside.archives = append(outside.archives, o.archives...)
		outside.sets = append(outside.sets, o.sets...)
	}
	key, ok := e.folderKey(parent)
	if !ok {
		return nil, setParentChanged(parent)
	}
	return &SetParent{Parent: parent, outside: outside, key: key}, nil
}

// KnownArchives are the archive folders this computer knows (knownArchives: the Desktop's list, its
// current archive, the wizard's last one, and each folder of the wizard's made-list), each absolute
// and clean, unread. The wizard's set reads each one's job file for the end note's lines about
// existing archives that back up the same mail (docs/design-per-mailbox-archives.md §11, C1).
func KnownArchives() []string { return knownArchives() }

// setParentChanged is R-e for a parent that is not the folder it was when it was checked.
func setParentChanged(parent string) *CopyRefusal {
	return refuse("R-e", fmt.Sprintf("The folder %s changed while MailArchive was checking it — nothing was created. Choose it again.", printable(parent)))
}

// MakeSetFolder makes the archive folder name inside the set's checked parent, at 0700, and returns
// its path. The handle on the parent is opened first; then the set's checked result is re-proved
// against it, never before: the path checks again (checkDecryptPaths, which walks no archive), the
// parent's identity against every archive the check walked (CopyOutside.Recheck), and the folder
// the handle holds must be the one the checks read by path and the one checked for the set. The
// folder is made through that handle. name must be one folder name this computer can use (R-q
// otherwise); a name already there is R-q, "exists", and nothing is made (the caller takes the next
// free number). Nothing is created on a refusal.
func (e *CopyExportEnv) MakeSetFolder(sp *SetParent, name string) (string, error) {
	if sp == nil {
		return "", refuse("R-e", "No folder was checked for the archives — choose one.")
	}
	if why := folderNameWhy(name); why != "" {
		return "", refusalQ(printable(name), why)
	}
	root, err := os.OpenRoot(sp.Parent)
	if err != nil {
		return "", destMissing(sp.Parent)
	}
	defer root.Close()
	e.step("set-open")
	if _, _, err := e.checkDecryptPaths(sp.Parent, "", nil, nil); err != nil {
		return "", err
	}
	if err := sp.outside.Recheck(e, sp.Parent); err != nil {
		return "", err
	}
	hf, herr := root.Open(".")
	if herr != nil {
		return "", destMissing(sp.Parent)
	}
	hid, ierr := readIdentity(hf)
	hf.Close()
	if ierr != nil {
		return "", setParentChanged(sp.Parent)
	}
	if ids, cerr := e.destChain(sp.Parent); cerr != nil || len(ids) == 0 || dirKeyOf(ids[0]) != dirKeyOf(hid) || dirKeyOf(hid) != sp.key {
		return "", setParentChanged(sp.Parent)
	}
	e.step("set-check")
	if err := root.Mkdir(name, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", refusalQ(name, "exists")
		}
		return "", refusalW(sp.Parent, leaveName(err))
	}
	return filepath.Join(sp.Parent, name), nil
}
