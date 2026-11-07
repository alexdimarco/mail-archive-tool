package tbrestore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Settling the earlier restores into a profile (design §8 foldcheck, foldcheck2 and
// foldcheck3 notes). At Start, under all three locks, before the new restore, each
// started line for the profile with no closing line is settled by the first case that
// applies; Check and -plan run the same cases read-only and say what Start will do.
//
//	0.  its recorded Local Folders is not this profile's now (another path or
//	    identity), or cannot be reached → elsewhere: nothing removed, no line (S9: when
//	    the recorded folder is reachable and the container is gone from it, the person
//	    has deleted it, so the record is closed "abandoned" and not named again);
//	0b. a "taken" line → 3a: "abandoned", nothing removed (S3);
//	1.  the container absent, or without this ID's marker → "abandoned", nothing
//	    removed (a container with no marker cannot be proven MailArchive's: N1);
//	2.  `Restored — NAME` beside it is a regular file (Lstat; any size: Thunderbird may
//	    since have filed mail in it), the log has "checked" and no "taken" → it had
//	    committed: "ended", nothing removed. (The live run's own re-check after a failed
//	    fsync, N-c, wants a regular *empty* file, since right after its create nothing
//	    else can have written into it: S9.)
//	3a. any entry of that name (a file of an unchecked restore, a folder, a link) →
//	    "abandoned", nothing removed, W-VISIBLE;
//	3b. otherwise → the one cleanup: "cleaned" when it removed everything, "abandoned"
//	    when it removed nothing or stopped part way (W-LEFT, the made-list's words), no
//	    line when a removal was blocked.

// Settled is what Start did (or, read-only, would do) with one earlier restore.
type Settled struct {
	Restore *Restore
	// Action: "ended", "cleaned", "abandoned", or "" when no line was written (an
	// elsewhere restore, a blocked removal); Case names the rule ("0", "0b", "1", "2",
	// "3a", "3b").
	Action string
	Case   string
	Clean  *CleanResult
	// Words are said after the new restore's own result ("" for one that needs none).
	Words string
}

// SettleInput is what settling needs.
type SettleInput struct {
	Log *Log
	// Local is the profile's Local Folders now, and FS its writer root (nil for a
	// read-only prediction too: a root is opened only to read).
	Local LocalFolders
	FS    FS
	// ProfileDir names the profile whose restores are settled.
	ProfileDir string
	// ReadOnly: Check and -plan, which write nothing (foldcheck M2).
	ReadOnly bool
}

// Settle settles every open restore into the profile, in the order they started.
func Settle(in SettleInput) ([]Settled, error) {
	lines, err := in.Log.Read()
	if err != nil {
		return nil, err
	}
	var out []Settled
	for _, r := range Open(Restores(lines), ProfileKeyOf(in.ProfileDir)) {
		s := settleOne(in, r)
		out = append(out, s)
		if in.ReadOnly || s.Action == "" {
			continue
		}
		res := s.Case
		if err := in.Log.Append(LogLine{Event: s.Action, ID: r.Started.ID, Result: "settled (case " + res + ")"}); err != nil {
			return out, err
		}
	}
	return out, nil
}

func settleOne(in SettleInput, r *Restore) Settled {
	s := Settled{Restore: r}
	date := r.Started.At
	if t, err := time.Parse(time.RFC3339Nano, r.Started.At); err == nil {
		date = t.Local().Format("2 January 2006, 15:04")
	}
	top := r.Name
	local := r.Started.Local
	// 0. Is its Local Folders this one?
	recorded := Identity{}
	if r.Started.LocalID != nil {
		recorded = *r.Started.LocalID
	}
	if local != in.Local.Dir || !recorded.Same(in.Local.Identity) {
		s.Case = "0"
		reach, err := DirIdentity(local)
		if err != nil || !reach.Same(recorded) {
			s.Words = fill(wElsewhereGone, "DATE", date, "PATH", local)
			return s
		}
		if _, err := os.Lstat(filepath.Join(local, r.Container())); errors.Is(err, fs.ErrNotExist) {
			s.Action = EvAbandoned // S9: the person deleted it; close the record
			return s
		}
		s.Words = fill(wElsewhereMoved, "DATE", date, "PATH", local, "TOP", top)
		return s
	}
	f := in.FS
	parent := entryKind(f, top)
	// 0b. A "taken" line always means remove nothing (foldcheck3 S3).
	if r.Taken {
		s.Case, s.Action = "0b", EvAbandoned
		if parent == "" {
			s.Words = fill(wTakenEarlierGone, "DATE", date, "TOP", top, "PATH", local)
		} else {
			s.Words = fill(wVisibleChecked, "DATE", date, "TOP", top, "SHOWS", showsWord(parent, "shows"))
		}
		return s
	}
	// 1. The container absent, or not carrying this ID's marker.
	if fi, err := f.Lstat(r.Container()); err != nil || !fi.IsDir() {
		s.Case, s.Action = "1", EvAbandoned
		return s
	}
	if m, ok := readMarker(f, r.Container()); !ok || m.ID != r.Started.ID {
		s.Case, s.Action = "1", EvAbandoned
		return s
	}
	// 2. Committed.
	if parent == "file" && r.Checked {
		s.Case, s.Action = "2", EvEnded
		return s
	}
	// 3a. An entry of the parent name, the restore not committed.
	if parent != "" {
		s.Case, s.Action = "3a", EvAbandoned
		if r.Checked {
			s.Words = fill(wVisibleChecked, "DATE", date, "TOP", top, "SHOWS", showsWord(parent, "shows"))
		} else {
			s.Words = fill(wVisibleUnchecked, "DATE", date, "TOP", top, "SHOWS", showsWord(parent, "shows"))
		}
		return s
	}
	// 3b. The one cleanup (read-only: steps 1 to 3 only, a prediction).
	s.Case = "3b"
	if in.ReadOnly {
		c := predictCleanup(f, top)
		s.Clean = &c
		if c.Kind == CleanRemoved {
			s.Words = fill(wWillRemove, "DATE", date)
		} else {
			s.Words = leftoverWords(c, top, local, r.Started.Encrypted)
		}
		return s
	}
	c := cleanupContainer(f, top, r.Started.ID, false, nil)
	s.Clean = &c
	switch c.Kind {
	case CleanRemoved:
		s.Action = EvCleaned
	case CleanBlocked:
		if r.Started.Encrypted {
			s.Words = fill(wBlockedEnc, "TOP", top) + wTriesAgain
		} else {
			s.Words = fill(wBlocked, "FILE", c.Blocked, "TOP", top) + wTriesAgain
		}
	case CleanParent:
		// the parent name appeared between the checks above and the cleanup's own
		s.Action = EvAbandoned
		s.Words = fill(wVisibleUnchecked, "DATE", date, "TOP", top, "SHOWS", showsWord(c.ParentKind, "shows"))
	default:
		s.Action = EvAbandoned
		s.Words = leftoverWords(c, top, local, r.Started.Encrypted)
	}
	return s
}

// predictCleanup is the cleanup's steps 1 to 3, writing nothing: what Start would do.
func predictCleanup(f FS, top string) CleanResult {
	container := top + ".sbd"
	if k := entryKind(f, top); k != "" {
		return CleanResult{Kind: CleanParent, ParentKind: k}
	}
	list, err := readMadeList(f, container)
	if err != nil {
		return CleanResult{Kind: CleanListBad, Err: err}
	}
	if _, r, clean := scanContainer(f, container, list, nil); !clean {
		return r
	}
	return CleanResult{Kind: CleanRemoved}
}

const (
	wWillRemove       = "An earlier restore into this profile did not finish (DATE). Its partial folder, which Thunderbird does not show, will be removed first."
	wTakenEarlierGone = "An earlier restore into Thunderbird (DATE) wrote and checked every message, but its folder's name was taken by something else while it ran, so MailArchive removed nothing: the restored folders are in 'TOP.sbd' in PATH, which Thunderbird does not show. Close Thunderbird and delete that folder yourself when you no longer need it."
)
