package tbrestore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Leftover is an earlier restore whose folder is still in a Local Folders and that
// Thunderbird does not show, as Desktop start, `status` and uninstall's plan name it
// (design §7.2 foldcheck2 note P2, §7.4 foldcheck2 note, foldcheck3 S7 and S10).
type Leftover struct {
	Restore *Restore
	// Kind: "unfinished" (W-UNFINISHED), "left" (W-LEFT: something not MailArchive's is
	// in it), "listbad" (the made-list cannot be read), "commiterr" (W-COMMITERR),
	// "taken" (its name was taken while it ran), "visible" (an entry of its name was
	// there and has since gone), or, for uninstall only, "elsewhere".
	Kind string
	// Container is the leftover folder's full path.
	Container string
	Words     string
}

// LocalFoldersOf is the profile's Local Folders now: §3.2's own lookup (ReadPrefs, then
// FindLocalFolders), which the display rule reuses to tell whether a recorded Local
// Folders is still the profile's (foldcheck3 S10).
func LocalFoldersOf(profileDir string) (LocalFolders, error) {
	p, err := ReadPrefs(profileDir)
	if err != nil {
		return LocalFolders{}, err
	}
	return FindLocalFolders(profileDir, p)
}

// localFoldersOfSeam is LocalFoldersOf; a test may stand in for it.
var localFoldersOfSeam = LocalFoldersOf

// Leftovers reads restores.log and names, for each restore not closed by "ended" or
// "cleaned", the leftover the rule shows — every one a read, nothing written:
//   - W-UNFINISHED only when the restore has no closing line, its recorded Local
//     Folders is reachable and is still its profile's, the container is there carrying
//     its marker, and no entry of its parent name is beside it (P2);
//   - an "abandoned" restore whose container is still there with its marker and no
//     entry of the parent name, in the words of why it was left (S7: one abandoned
//     because an entry of its name was there, which has since gone, gets W-LEFT's
//     "Thunderbird does not show that folder … delete it yourself" sense too);
//   - nothing when an entry of the parent name is there (the folder is the person's now).
//
// forUninstall adds, for an open restore whose Local Folders cannot be checked (not
// reachable, or no longer its profile's), a line naming its recorded path, since
// nothing will clean it up after MailArchive is removed (S7, third bullet).
func Leftovers(log *Log, forUninstall bool) ([]Leftover, error) {
	lines, err := log.Read()
	if err != nil {
		return nil, err
	}
	var out []Leftover
	for _, r := range Restores(lines) {
		if r.Closed == EvEnded || r.Closed == EvCleaned {
			continue
		}
		if lo, ok := leftoverOf(r, forUninstall); ok {
			if forUninstall {
				lo.Words += " " + wNothingAfterUninstall
			}
			out = append(out, lo)
		}
	}
	return out, nil
}

func leftoverOf(r *Restore, forUninstall bool) (Leftover, bool) {
	date := r.Started.At
	if t, err := time.Parse(time.RFC3339Nano, r.Started.At); err == nil {
		date = t.Local().Format("2 January 2006, 15:04")
	}
	local, top := r.Started.Local, r.Name
	container := filepath.Join(local, r.Container())
	lo := Leftover{Restore: r, Container: container}
	recorded := Identity{}
	if r.Started.LocalID != nil {
		recorded = *r.Started.LocalID
	}
	reach, rerr := DirIdentity(local)
	reachable := rerr == nil && reach.Same(recorded)
	now, nerr := localFoldersOfSeam(r.Started.ProfileDir)
	this := nerr == nil && now.Dir == local && now.Identity.Same(recorded)
	if !reachable || !this {
		if !forUninstall || r.Closed != "" {
			return lo, false
		}
		if reachable {
			if _, err := os.Lstat(container); errors.Is(err, fs.ErrNotExist) {
				return lo, false
			}
		}
		lo.Kind = "elsewhere"
		lo.Words = fill(wElsewhereUninstall, "DATE", date, "PATH", local, "TOP", top)
		return lo, true
	}
	root, err := os.OpenRoot(local)
	if err != nil {
		return lo, false
	}
	defer root.Close()
	f := newRootFS(root)
	if fi, err := f.Lstat(r.Container()); err != nil || !fi.IsDir() {
		return lo, false
	}
	if m, ok := readMarker(f, r.Container()); !ok || m.ID != r.Started.ID {
		return lo, false
	}
	if entryKind(f, top) != "" {
		return lo, false // Thunderbird can show it: the person's folder now
	}
	if r.Closed == "" {
		lo.Kind = "unfinished"
		lo.Words = fill(wUnfinished, "DATE", date, "TOP", top, "PATH", local)
		return lo, true
	}
	// Abandoned, still there, not shown: say why it was left.
	if _, err := readMadeList(f, r.Container()); err != nil {
		lo.Kind, lo.Words = "listbad", fill(wListBad, "TOP", top, "PATH", local)
		return lo, true
	}
	lines := abandonedResult(r)
	switch {
	case lines == string(OutCommitErr):
		lo.Kind, lo.Words = "commiterr", fill(wCommitErrShown, "DATE", date, "TOP", top, "PATH", local)
	case r.Taken:
		lo.Kind, lo.Words = "taken", fill(wTakenEarlierGone, "DATE", date, "TOP", top, "PATH", local)
	default:
		c := predictCleanup(f, top)
		if c.Kind == CleanLeft || c.Kind == CleanLeftPart {
			lo.Kind, lo.Words = "left", fill(wLeftShown, "DATE", date, "TOP", top, "PATH", local)
		} else {
			lo.Kind, lo.Words = "visible", fill(wVisibleGone, "DATE", date, "TOP", top, "PATH", local)
		}
	}
	return lo, true
}

// abandonedResult is the Result of the restore's own closing line, read again from
// the log the Restore was built from (Restores keeps only the event).
func abandonedResult(r *Restore) string { return r.closingResult }

const (
	wNothingAfterUninstall = "Nothing will clean it up after MailArchive is removed."
	wElsewhereUninstall    = "An earlier restore into Thunderbird (DATE) may not have finished. It wrote into PATH, which MailArchive cannot check now. If the folder 'TOP.sbd' is there, close Thunderbird and delete it yourself."
	wLeftShown             = "An earlier restore into Thunderbird (DATE) left the folder 'TOP.sbd' in PATH: it holds something MailArchive did not make, so MailArchive removed nothing there. Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no longer need those files."
	wVisibleGone           = "An earlier restore into Thunderbird (DATE) left the folder 'TOP.sbd' in PATH whole, because a folder of its name that MailArchive did not make was in Local Folders. That folder has gone, so Thunderbird does not show the restored one now. Close Thunderbird and delete 'TOP.sbd' yourself when you no longer need it."
	wCommitErrShown        = "An earlier restore into Thunderbird (DATE) wrote and checked every message, but could not make sure the file that makes the folder show in Thunderbird was saved, so it removed nothing. Thunderbird does not show it now. Close Thunderbird, delete the folder 'TOP.sbd' in PATH, and restore again."
)
