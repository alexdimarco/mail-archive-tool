package tbengine

import (
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbrestore"
)

// Folder is one node of an archive's folder tree as MailArchive Desktop's "Choose folders"
// shows it (design §7.3 step 2, slice R7): a store or a folder, its id (checked against
// this tree again at the plan, never used to build a path), and its restorable messages.
type Folder struct {
	ID, Parent, Name string
	Store            bool
	// Count is the restorable messages filed directly in it; Total adds its subfolders',
	// since choosing a folder restores its subfolders too (§6.5).
	Count, Total int
}

// Folders reads the archive's folder tree, writing nothing: a plain archive's manifest, or
// an encrypted one's through a read lease of its own, destroyed before it returns (as the
// plan reads it). With counts, each node says how many of its messages a restore would
// put back (app's RestorableCounts, SelectForRestore's own judgement); without, the tree
// only, for checking the ids a page sends. A locked encrypted archive is W-LOCKED.
func Folders(out string, keys *keyring.Unlocked, counts bool) ([]Folder, error) {
	abs, err := filepath.Abs(out)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, refusal("W-NOARCHIVE", fill(wNoArchive, "OUT", out, "WHY", "no folder was named"), err)
	}
	abs = filepath.Clean(abs)
	if keys == nil {
		if kerr := app.RefuseKeyless("restore from it", abs); kerr != nil {
			if app.UnlockableAt(abs) {
				return nil, refusal("W-LOCKED", wLocked, kerr)
			}
			return nil, kerr
		}
	}
	man, err := loadManifest(abs, keys)
	if err != nil {
		return nil, err
	}
	tree, err := app.NewArchiveTree(man)
	if err != nil {
		return nil, err
	}
	var n map[string]int
	if counts {
		n = tree.RestorableCounts(abs, keys != nil)
	}
	nodes := tree.Tree()
	fs := make([]Folder, len(nodes))
	at := make(map[string]int, len(nodes))
	for i, nd := range nodes {
		fs[i] = Folder{ID: nd.ID, Parent: nd.Parent, Name: nd.Name, Store: nd.Store, Count: n[nd.ID], Total: n[nd.ID]}
		at[nd.ID] = i
	}
	// The tree is in depth-first order, every child after its parent: walked backwards, a
	// node's total is whole before it is added to its parent's.
	for i := len(fs) - 1; i >= 0; i-- {
		if p, ok := at[fs[i].Parent]; ok && fs[i].Parent != "" {
			fs[p].Total += fs[i].Total
		}
	}
	return fs, nil
}

// SetOSProbesForTest makes the engine's probes of this computer answer "none" — not
// running as administrator or root, no Microsoft Store Thunderbird, Local Folders on no
// network drive — for another package's tests (MailArchive Desktop's, slice R7), as this
// package's own TestMain does, so a test's fake profile in a temp folder is planned the
// same on every computer. It returns the function that puts them back.
func SetOSProbesForTest() (restore func()) {
	e, s, n := elevatedProbe, storeWarning, networkWarning
	elevatedProbe = func() bool { return false }
	storeWarning = func() string { return "" }
	networkWarning = func(tbrestore.LocalFolders) string { return "" }
	return func() { elevatedProbe, storeWarning, networkWarning = e, s, n }
}
