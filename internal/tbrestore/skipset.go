package tbrestore

import (
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
	known      map[string]skipEntry
	unreadable bool
	size       int64
	mod        time.Time
	stated     bool
}

type skipEntry struct {
	profile   string
	committed bool
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
	s.known = map[string]skipEntry{}
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
		s.known[r.Started.ID] = skipEntry{profile: r.Started.Profile, committed: r.Checked && !r.Taken}
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
// marker names, and whether it committed. An id the set does not know makes it re-read
// the log once, unless the log is as it was when the set was built: a restore's started
// line is fsynced before its folder is made, so a re-read always finds a real restore
// that began after the set was built (foldcheck M3).
func (s *SkipSet) Lookup(id, profileKey string) (known, committed bool) {
	if s == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.known[id]
	if !ok && s.changed() {
		s.load()
		e, ok = s.known[id]
	}
	if !ok || e.profile != profileKey {
		return false, false
	}
	return true, e.committed
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
