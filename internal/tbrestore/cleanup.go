package tbrestore

import (
	"errors"
	"io/fs"
	"path"

	"golang.org/x/text/unicode/norm"
)

// The one cleanup (design §8 foldcheck, foldcheck2 and foldcheck3 notes): Cancel, Shut
// down, a failure, W-CHECKFAIL, W-TAKEN and a later Start all use it. Its one idea: if
// Thunderbird could have shown the tree, or anything not MailArchive's is in it, remove
// nothing. It runs in this decision order, and nothing is removed before step 4:
//
//	0b. a "taken" line for the restore → remove nothing (foldcheck3 S3);
//	1.  any entry named "Restored — NAME" beside the container → remove nothing (3a);
//	2.  the made-list, read; unreadable → remove nothing (rule 6);
//	3.  a scan of the whole container: anything that is not the marker, the made-list
//	    or a listed entry of the listed kind → remove nothing (W-LEFT);
//	4.  only now remove: the listed entries in reverse order (each Lstat-ed again for
//	    its kind, absent ones passed over, a folder only when empty), then the
//	    made-list, the marker (only when its ID is this restore's), the container; a
//	    folder not empty at its turn, or a kind that changed, stops removal (W-LEFT's
//	    step-4 form, S4);
//	5.  a removal blocked (a sharing violation past the retry, a permission error)
//	    stops the cleanup with no line, so the next Start tries again.
//
// Rule 7: the parent name is never removed, nor anything outside the container.

// CleanKind is how a cleanup ended.
type CleanKind int

const (
	// CleanRemoved: everything the restore made was removed, then its list, marker
	// and container.
	CleanRemoved CleanKind = iota + 1
	// CleanTaken: the log has a "taken" line (step 0b): nothing removed.
	CleanTaken
	// CleanParent: an entry of the parent name is beside the container (step 1).
	CleanParent
	// CleanListBad: the made-list cannot be read (step 2).
	CleanListBad
	// CleanLeft: the scan found something the restore did not make (step 3).
	CleanLeft
	// CleanLeftPart: removal began, then met something new (step 4).
	CleanLeftPart
	// CleanBlocked: a removal was refused (step 5); nothing is logged.
	CleanBlocked
)

// RemovedNothing reports a cleanup that left the container whole.
func (k CleanKind) RemovedNothing() bool {
	return k == CleanTaken || k == CleanParent || k == CleanListBad || k == CleanLeft
}

// CleanResult is what a cleanup did and found.
type CleanResult struct {
	Kind CleanKind
	// ParentKind is the entry of the parent name, for the words: "file", "folder",
	// "link" or "other"; "" when none.
	ParentKind string
	// Foreign are the first entries found that the restore did not make (at most 3),
	// ForeignN how many the scan found before it stopped.
	Foreign  []string
	ForeignN int
	// Blocked is the entry whose removal was refused.
	Blocked string
	Err     error
}

// cleanupContainer cleans up the restore whose container is top+".sbd" in Local
// Folders, by the decision order above. id is the restore's own ID (its marker must
// name it); taken is whether the log has a "taken" line for it.
func cleanupContainer(f FS, top, id string, taken bool) CleanResult {
	container := top + ".sbd"
	if taken {
		return CleanResult{Kind: CleanTaken, ParentKind: entryKind(f, top)}
	}
	if k := entryKind(f, top); k != "" {
		return CleanResult{Kind: CleanParent, ParentKind: k}
	}
	list, err := readMadeList(f, container)
	if err != nil {
		return CleanResult{Kind: CleanListBad, Err: err}
	}
	if r, clean := scanContainer(f, container, list); !clean {
		return r
	}
	for i := len(list) - 1; i >= 0; i-- {
		e := list[i]
		p := container + "/" + e.P
		fi, err := f.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !kindMatches(e.K, fi) {
			return CleanResult{Kind: CleanLeftPart, Foreign: []string{e.P}, ForeignN: 1, Err: err}
		}
		if r, ok := removeOne(f, p, e.K == "d"); !ok {
			return r
		}
	}
	// The list and the marker go only once the container holds nothing else, so a
	// cleanup stopped by something new keeps them (backups keep leaving it out).
	var extra []string
	if err := f.ReadDir(container, func(b []fs.DirEntry) bool {
		for _, e := range b {
			if e.Name() != MarkerName && e.Name() != MadeListName {
				extra = append(extra, e.Name())
			}
		}
		return len(extra) == 0
	}); err != nil || len(extra) > 0 {
		return CleanResult{Kind: CleanLeftPart, Foreign: extra, ForeignN: len(extra), Err: err}
	}
	if r, ok := removeOne(f, container+"/"+MadeListName, false); !ok {
		return r
	}
	if m, ok := readMarker(f, container); ok && m.ID != id {
		return CleanResult{Kind: CleanLeftPart, Foreign: []string{MarkerName}, ForeignN: 1}
	}
	if r, ok := removeOne(f, container+"/"+MarkerName, false); !ok {
		return r
	}
	if r, ok := removeOne(f, container, true); !ok {
		return r
	}
	_ = f.SyncDir(".")
	return CleanResult{Kind: CleanRemoved}
}

// removeOne removes one entry (absent: passed over). A folder that is not empty, or one
// that changed kind, is CleanLeftPart; a removal refused otherwise is CleanBlocked.
func removeOne(f FS, p string, dir bool) (CleanResult, bool) {
	err := withRetry(func() error {
		err := f.Remove(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
	if err == nil {
		return CleanResult{}, true
	}
	if dir {
		if fi, lerr := f.Lstat(p); lerr == nil && fi.IsDir() {
			// still there: not empty (something was added after the scan)
			return CleanResult{Kind: CleanLeftPart, Foreign: []string{path.Base(p)}, ForeignN: 1, Err: err}, false
		}
	}
	return CleanResult{Kind: CleanBlocked, Blocked: p, Err: err}, false
}

// scanContainer is step 3: the whole container, following no link, going down only
// into listed folders, before anything is removed. Names are matched as the file system
// matches them (foldcheck3 S8): NFC-normalised on both sides, so a name an HFS+ volume
// stored decomposed is still the list's; but each listed name is matched once, so a
// second entry that spells the same (a decomposed twin on a volume that keeps both) is
// not the list's. The scan reads each folder in batches and stops at the batch that
// holds the first entry not on the list, so it does no more work than the list allows.
func scanContainer(f FS, container string, list []madeEntry) (CleanResult, bool) {
	listed := map[string]string{}
	for _, e := range list {
		listed[norm.NFC.String(e.P)] = e.K
	}
	matched := map[string]bool{}
	var foreign []string
	n := 0
	dirs := []string{""}
	for len(dirs) > 0 && n == 0 {
		dir := dirs[0]
		dirs = dirs[1:]
		full := container
		if dir != "" {
			full += "/" + dir
		}
		err := f.ReadDir(full, func(batch []fs.DirEntry) bool {
			for _, e := range batch {
				rel := e.Name()
				if dir != "" {
					rel = dir + "/" + rel
				}
				fi, err := f.Lstat(full + "/" + e.Name())
				if dir == "" && (e.Name() == MarkerName || e.Name() == MadeListName) {
					if err == nil && fi.Mode().IsRegular() {
						continue
					}
				} else if k, ok := listed[norm.NFC.String(rel)]; ok && err == nil && kindMatches(k, fi) && !matched[norm.NFC.String(rel)] {
					matched[norm.NFC.String(rel)] = true
					if k == "d" {
						dirs = append(dirs, rel)
					}
					continue
				}
				n++
				if len(foreign) < 3 {
					foreign = append(foreign, rel)
				}
			}
			return n == 0
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return CleanResult{Kind: CleanLeft, Foreign: []string{dir}, ForeignN: 1, Err: err}, false
		}
	}
	if n > 0 {
		return CleanResult{Kind: CleanLeft, Foreign: foreign, ForeignN: n}, false
	}
	return CleanResult{}, true
}

// kindMatches: "f" a regular file, "d" a folder; never a link.
func kindMatches(k string, fi fs.FileInfo) bool {
	switch k {
	case "f":
		return fi.Mode().IsRegular()
	case "d":
		return fi.IsDir() && fi.Mode()&fs.ModeSymlink == 0
	}
	return false
}

// entryKind is what is at name in Local Folders: "" for nothing.
func entryKind(f FS, name string) string {
	fi, err := f.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		return "other" // cannot be told: treated as there (remove nothing)
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return "link"
	case fi.IsDir():
		return "folder"
	case fi.Mode().IsRegular():
		return "file"
	}
	return "other"
}
