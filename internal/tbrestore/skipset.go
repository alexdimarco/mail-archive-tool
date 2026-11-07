package tbrestore

import (
	"io/fs"
	"os"
	"sync"
	"time"
)

// SkipSet is what a backup needs to leave MailArchive's own restore folders out
// (design §7.2 as folded by RT-5, foldcheck M3 and foldcheck2 P3; invariant R41), built
// by the backup's caller from restores.log so internal/source never reads
// MailArchive's settings. For each restore ID the log knows it keeps its profile's key
// and one flag, committed: a "checked" line and no "taken" line (Start's own rule for
// case 2; the parent file's kind is the walk's to read, never the log's). It is safe
// for concurrent use.
type SkipSet struct {
	mu         sync.Mutex
	path       string
	known      map[string]*skipEntry
	unreadable bool
	size       int64
	mod        time.Time
	stated     bool
}

type skipEntry struct {
	profile   string
	committed bool
	open      bool
	// local and localID are where the restore wrote (its started line): a marker met
	// anywhere else is not this record's restore (ADV-06).
	local   string
	localID *Identity
	// here is that Local Folders as it is now, read once per load when first asked
	// (nil: not read yet; hereOK false: gone, moved or not the recorded folder).
	here   os.FileInfo
	asked  bool
	hereOK bool
}

// at reports whether holder (the folder a walk met the restore's container in) is the
// Local Folders the restore wrote into: the recorded path is still the recorded folder
// by identity, and holder is that same folder. The caller holds the set's mutex.
func (e *skipEntry) at(holder fs.FileInfo) bool {
	if holder == nil || e.local == "" || e.localID == nil {
		return false
	}
	if !e.asked {
		e.asked = true
		if id, err := DirIdentity(e.local); err == nil && id.Same(*e.localID) {
			if fi, err := os.Stat(e.local); err == nil && fi.IsDir() {
				e.here, e.hereOK = fi, true
			}
		}
	}
	return e.hereOK && os.SameFile(e.here, holder)
}

// LoadSkipSet reads restores.log at path into a skip set. It never fails: an absent
// log knows no restore; one that cannot be read marks the set unreadable, and then the
// walk leaves out only marked folders with no entry of their parent name beside them
// (§7.2 RT-5 note, third bullet).
func LoadSkipSet(path string) *SkipSet {
	s := &SkipSet{path: path}
	s.load()
	return s
}

func (s *SkipSet) load() {
	s.known = map[string]*skipEntry{}
	s.unreadable = false
	if fi, err := os.Stat(s.path); err == nil {
		s.size, s.mod, s.stated = fi.Size(), fi.ModTime(), true
	} else {
		s.size, s.mod, s.stated = 0, time.Time{}, false
	}
	lines, err := NewLog(s.path).Read()
	if err != nil {
		s.unreadable = true
		return
	}
	for _, r := range Restores(lines) {
		s.known[r.Started.ID] = &skipEntry{
			profile: r.Started.Profile,
			// ADV-07: an abandoned restore is never committed, whatever lines it has.
			committed: r.Checked && !r.Taken && r.Closed != EvAbandoned,
			open:      r.Closed == "",
			local:     r.Started.Local, localID: r.Started.LocalID,
		}
	}
}

// changed reports whether the log's size or modification time differs from when the
// set was last built (one stat; foldcheck M3's guard against a planted marker forcing
// a re-read per folder).
func (s *SkipSet) changed() bool {
	fi, err := os.Stat(s.path)
	if err != nil {
		return s.stated
	}
	return !s.stated || fi.Size() != s.size || !fi.ModTime().Equal(s.mod)
}

// Lookup reports whether restores.log knows the restore id for the profile whose key a
// marker names and for the folder holder the walk met its container in — by identity
// the Local Folders the restore's started line records (ADV-06: a restore's folder
// copied into another profile, or onto a disk the person backs up, is not this
// record's) — and then whether it is open (no closing line) and whether it committed (a
// "checked" line, no "taken" line, not closed "abandoned": ADV-07). An id the set does
// not know makes it re-read the log once, unless the log is as it was when the set was
// built: a restore's started line is fsynced before its folder is made, so a re-read
// always finds a real restore that began after the set was built (foldcheck M3).
func (s *SkipSet) Lookup(id, profileKey string, holder fs.FileInfo) (known, open, committed bool) {
	if s == nil {
		return false, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.known[id]
	// Settled 2026-11-07 [R6-04]: a restore the set knew as open or not committed may
	// have committed since (a backup that began before or during it), so the log is read
	// again when it has changed, as for an id never seen; a committed one cannot go back.
	if (!ok || e.open || !e.committed) && s.changed() {
		s.load()
		e, ok = s.known[id]
	}
	if !ok || e.profile != profileKey || !e.at(holder) {
		return false, false, false
	}
	return true, e.open, e.committed
}

// LogUnreadable reports that restores.log could not be read when the set was (last)
// built.
func (s *SkipSet) LogUnreadable() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadable
}
