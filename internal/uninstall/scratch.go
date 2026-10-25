package uninstall

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// scratchRoot is MailArchive's scratch folder (job.ScratchDir), where an encrypted
// archive's run keeps its plaintext working copies; a var so a test points it elsewhere.
var scratchRoot = job.ScratchDir

// snapshotPrefix starts a -copy-first snapshot's name (export.SnapshotPrefix; the
// scratch test pins that the two agree).
const snapshotPrefix = ".mailarchive-snapshot-"

// ErrScratchUnexpected refuses the whole uninstall because MailArchive's scratch
// folder holds something it never writes: removing what it does not know could
// delete someone's files, so nothing is removed and the owner is told where to look.
var ErrScratchUnexpected = errors.New("MailArchive's working folder holds something MailArchive did not put there")

// ScratchTargets is what uninstall removes of the scratch folder at root, deepest
// first: the files an encrypted archive's run leaves — -copy-first snapshots in
// <archive id>/snap/, the Outlook app's PSTs in <archive id>/_outlook-pst/ and the
// search index a crashed run left in <archive id>/index/search.db — and
// then those folders and root itself. It deletes nothing, and never recurses beyond
// that shape: a symlink, a folder or file of any other name or kind, or a root that
// overlaps a known archive refuses (ErrScratchUnexpected, ErrOverlap), and the folders
// are removed with os.Remove, so one that is not empty stops the removal. An absent
// root is nothing to remove.
func ScratchTargets(root string, knownArchives []string) ([]string, error) {
	fi, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() || isArchiveDir(root) {
		return nil, fmt.Errorf("%w: %s is not a plain folder", ErrScratchUnexpected, root)
	}
	rr := util.ResolveExisting(root)
	for _, a := range knownArchives {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		ra := util.ResolveExisting(a)
		if util.Within(rr, ra) || util.Within(ra, rr) {
			return nil, ErrOverlap
		}
	}
	unexpected := func(p string) error { return fmt.Errorf("%w: %s", ErrScratchUnexpected, p) }
	children, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var files, dirs []string
	for _, c := range children {
		archive := filepath.Join(root, c.Name())
		if !c.Type().IsDir() || !isArchiveID(c.Name()) {
			return nil, unexpected(archive)
		}
		kinds, err := os.ReadDir(archive)
		if err != nil {
			return nil, err
		}
		for _, k := range kinds {
			sub := filepath.Join(archive, k.Name())
			var owned func(string) bool
			switch {
			case k.Type().IsDir() && k.Name() == "snap":
				owned = func(n string) bool { return strings.HasPrefix(n, snapshotPrefix) }
			case k.Type().IsDir() && k.Name() == "_outlook-pst":
				owned = func(n string) bool { return strings.EqualFold(filepath.Ext(n), ".pst") }
			case k.Type().IsDir() && k.Name() == "index":
				owned = func(n string) bool { return n == "search.db" }
			default:
				return nil, unexpected(sub)
			}
			entries, err := os.ReadDir(sub)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.Type().IsRegular() || !owned(e.Name()) {
					return nil, unexpected(filepath.Join(sub, e.Name()))
				}
				files = append(files, filepath.Join(sub, e.Name()))
			}
			dirs = append(dirs, sub)
		}
		dirs = append(dirs, archive)
	}
	return append(append(files, dirs...), root), nil
}

// isArchiveID reports whether name is a scratch folder's archive id: 32 lowercase
// hex characters.
func isArchiveID(name string) bool {
	if len(name) != 32 {
		return false
	}
	for _, r := range name {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
