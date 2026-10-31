package presync

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// suspendGap: if two polls fall more than this apart the machine slept (a laptop
// lid, say); the gap is not counted as quiet (design section 4.4.2).
const suspendGap = 30 * time.Second

// Watch-set caps (design sections 4.4.2 / 4.11): the walk stops past these and
// reports truncation, and quiet is then judged on the part walked.
// watchMaxEntries bounds the WHOLE walk — every visited node (directories and
// specials too, not only regular files) counts toward it, so a directory-heavy
// tree cannot walk past the bound. A var so a test can shrink it.
var watchMaxEntries = 200_000

const watchMaxDepth = 16

// quietConfig holds the durations the tracker needs. They are injected so tests
// drive the tracker on a fake clock without real waits.
type quietConfig struct {
	settle  time.Duration // earliest the quiet clock may start counting after `up`
	quiet   time.Duration // no change for this long (after a first change) is quiet
	maxWait time.Duration // longest to wait for quiet, measured from `up`
}

// watchSnap is one poll of the watch set. sum folds every watched regular file
// (indexes included) and moves on any change. store lists the message-store
// files alone (walk key -> size), which is what the first-change gate reads
// (MA-932); other lists every other regular file the same way, so a file that
// becomes mail by its name's rule (its .msf index appears) is not taken for a
// new one. The walk may not have seen everything: capped means it stopped at
// the entry cap after the node whose walk key is frontier; holes are the walk
// keys of entries it could not read (a folder's whole contents, or one file).
type watchSnap struct {
	sum      uint64
	store    map[string]int64
	other    map[string]int64
	capped   bool
	frontier string
	holes    []string
}

// walked reports whether this poll's walk covered the position key — not past
// a capped walk's frontier and not inside a hole — so that a store file missing
// from this poll was really absent then, not merely unseen.
func (s watchSnap) walked(key string) bool {
	if s.capped && key > s.frontier {
		return false
	}
	for _, h := range s.holes {
		if key == h || strings.HasPrefix(key, h+"\x00") {
			return false
		}
	}
	return true
}

// walkKey is a node's position in the walk: the root's index, then the path's
// elements, joined by NUL (which no file name holds). WalkDir visits each
// folder's entries in name order and a folder's contents right after the
// folder, so walk keys sort in exactly the order the walk visits nodes; that is
// what lets a capped walk's frontier say what it did and did not see.
func walkKey(root int, rel string) string {
	k := fmt.Sprintf("%08d", root)
	if rel == "." || rel == "" {
		return k
	}
	return k + "\x00" + strings.ReplaceAll(filepath.ToSlash(rel), "/", "\x00")
}

// quietTracker decides when the watched mail files have gone quiet, per the
// revision-3 rule (design section 4.4.2): quiet is declared only after at least
// one real change has been observed since `up` AND then no change for `quiet`.
// A session in which nothing ever changes waits to `maxWait` and ends
// ok-noactivity — it never concludes at settle+quiet, which is exactly the
// opening silence of Thunderbird's ~10-minute first offline poll (lab L-M5).
//
// The first change must be mail data (MA-932, design-presync-attended L-A9,
// 2026-10-31): Thunderbird touches a folder's .msf index about 2 s before it
// writes mail, and can touch it with no mail following, so an index touch alone
// must not open the gate. Once the gate is open, ANY watched change still resets
// the quiet clock, as before.
type quietTracker struct {
	cfg        quietConfig
	up         time.Time
	lastChange time.Time
	lastDigest uint64
	started    bool // a baseline digest has been taken
	changed    bool // mail data seen to arrive since `up` (the rev-3 gate, MA-932)
	lastPollAt time.Time

	last  watchSnap        // the previous poll
	known map[string]int64 // every regular file seen this session, mail or not, at its last size
}

func newQuietTracker(up time.Time, cfg quietConfig) *quietTracker {
	return &quietTracker{cfg: cfg, up: up, lastChange: up.Add(cfg.settle), known: map[string]int64{}}
}

// observe folds one poll, taken at time now. A digest different from the
// previous poll is a change — growth, an mtime-only touch, a new file, a rename
// or a deletion all move it — and resets the quiet clock; it opens the
// first-change gate only when mail data arrived (mailArrived). A gap longer than
// suspendGap means the machine slept, which resets the quiet clock without
// counting as a change.
func (q *quietTracker) observe(s watchSnap, now time.Time) {
	if !q.lastPollAt.IsZero() && now.Sub(q.lastPollAt) > suspendGap {
		q.lastChange = now
	}
	if q.started && s.sum != q.lastDigest {
		q.lastChange = now
		if !q.changed && q.mailArrived(s) {
			q.changed = true
		}
	}
	for p, sz := range s.other {
		q.known[p] = sz
	}
	for p, sz := range s.store {
		q.known[p] = sz
	}
	q.last = s
	q.lastDigest = s.sum
	q.started = true
	q.lastPollAt = now
}

// mailArrived reports whether this poll's message store shows mail data that
// was not there before (MA-932): a store file seen earlier this session (as mail
// or not) that is now LARGER, or a store file never seen this session that holds
// data. Not
// counted: an index or other non-store file (they are not in the store); a store
// file that shrank or was rewritten at the same size (compaction, a flag
// update); a new file whose size equals one that vanished since the last poll
// (a rename or a move: the same bytes under a new name); a new empty file (a new
// folder; its first growth counts); and a never-seen file at a position the
// previous poll did not walk (past a capped walk's frontier, or inside a folder
// it could not read): it may have been there all along, unseen. Seen files are
// remembered for the whole session, so a file that drops out of one poll (an
// index rebuilt, the cap moving) and comes back, or a dotted mailbox that only
// counts as mail once its .msf is written, is compared, never "new".
func (q *quietTracker) mailArrived(s watchSnap) bool {
	vanished := map[int64]int{} // sizes of store files present last poll and really gone now
	for p, sz := range q.last.store {
		if _, ok := s.store[p]; !ok && s.walked(p) {
			vanished[sz]++
		}
	}
	for p, sz := range s.store {
		if old, seen := q.known[p]; seen {
			if sz > old {
				return true
			}
			continue
		}
		if sz == 0 || !q.last.walked(p) {
			continue
		}
		if vanished[sz] > 0 {
			vanished[sz]--
			continue
		}
		return true
	}
	return false
}

// verdict returns the outcome and true once the session should end watching, or
// a zero Outcome and false to keep polling. ok needs a first change then quiet;
// with no change it waits to maxWait and ends ok-noactivity; a change still in
// flight at maxWait is not-quiet.
func (q *quietTracker) verdict(now time.Time) (Outcome, bool) {
	if q.changed && now.Sub(q.lastChange) >= q.cfg.quiet {
		return OutcomeOK, true
	}
	if now.Sub(q.up) >= q.cfg.maxWait {
		if q.changed {
			return OutcomeNotQuiet, true
		}
		return OutcomeNoActivity, true
	}
	return Outcome{}, false
}

// digestWatch folds (relative path, size, mtime in nanoseconds) of every regular
// file under the watch roots into one FNV-1a digest, and lists the
// message-store files among them (isStoreFile). It never opens a file (so a
// content change that keeps the same size and mtime does not move the digest)
// and never descends a symbolic link (WalkDir does not follow them). It stops
// past the entry or depth cap and reports truncation; quiet is then judged on
// the part walked.
func digestWatch(roots []string) (snap watchSnap, truncated bool) {
	h := fnv.New64a()
	var num [8]byte
	writeStr := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	writeU64 := func(v uint64) {
		for i := 0; i < 8; i++ {
			num[i] = byte(v >> (8 * i))
		}
		h.Write(num[:])
	}
	entries := 0
	type walkedFile struct {
		key  string
		size int64
	}
	files := map[string]walkedFile{} // every regular file walked, by full path
	for ri, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// What lies under it went unseen this poll: a hole (MA-932). An
				// unreadable entry is skipped, never aborts the walk.
				rel, rerr := filepath.Rel(root, path)
				if rerr != nil {
					rel = path
				}
				snap.holes = append(snap.holes, walkKey(ri, rel))
				return nil
			}
			if entries >= watchMaxEntries {
				truncated, snap.capped = true, true
				return filepath.SkipAll
			}
			entries++ // every visited node counts toward the cap (dirs/specials/files)
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			snap.frontier = walkKey(ri, rel) // the last node visited
			if rel != "." && depthOf(rel) > watchMaxDepth {
				truncated = true
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				snap.holes = append(snap.holes, snap.frontier) // gone between listing and reading
				return nil
			}
			if !info.Mode().IsRegular() { // symlinks and specials are not followed or recorded
				return nil
			}
			writeStr(rel)
			writeU64(uint64(info.Size()))
			writeU64(uint64(info.ModTime().UnixNano()))
			files[path] = walkedFile{key: snap.frontier, size: info.Size()}
			return nil
		})
	}
	snap.store, snap.other = map[string]int64{}, map[string]int64{}
	for p, f := range files {
		_, hasIndex := files[p+".msf"]
		if isStoreFile(p, hasIndex) {
			snap.store[f.key] = f.size
		} else {
			snap.other[f.key] = f.size
		}
	}
	snap.sum = h.Sum64()
	return snap, truncated
}

// isStoreFile reports whether the regular file at path holds Thunderbird mail
// data (MA-932), judged from names alone — never by opening it. What Thunderbird
// keeps under ImapMail/<server>/ and Mail/<account>/:
//   - a folder's mail: an mbox file named after the folder, no extension of its
//     own ("INBOX", "Sent Items"), beside its summary index "<folder>.msf"; a
//     folder name may hold a dot ("Lists.golang"), so a file with a sibling
//     "<name>.msf" is mail whatever its name; a subfolder's files sit in a
//     "<folder>.sbd" directory, the same way;
//   - in a maildir account, one file per message in the folder directory's "cur"
//     (and, for a store that uses it, "new"); "tmp" holds half-written files;
//   - and files that are not mail: the .msf indexes, msgFilterRules.dat,
//     popstate.dat, filterlog.html, junklog.html, feeds.json and feeditems.json
//     (older: .rdf), search-integration copies (*.mozmsgs/*.wdseml, *.mozeml),
//     the operating system's own (.DS_Store, desktop.ini, Thumbs.db), and
//     "nstmp", the temporary file folder compaction writes and renames over a
//     folder (a compaction is not new mail).
//
// So a file is mail when its directory is a maildir "cur" or "new", or it has a
// sibling "<name>.msf", or it has no extension and is not "nstmp" — but an .msf
// is never mail, nor is a file in a directory named "tmp" (a maildir's
// half-written message; a folder's own directory is "<name>.sbd", never "tmp").
// hasIndex says whether the walk saw a regular file "<path>.msf" beside it.
func isStoreFile(path string, hasIndex bool) bool {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".msf") {
		return false
	}
	switch filepath.Base(filepath.Dir(path)) {
	case "cur", "new":
		return true
	case "tmp":
		return false // a maildir message still being written; it counts once moved to cur
	}
	if hasIndex {
		return true
	}
	return ext == "" && !strings.EqualFold(name, "nstmp")
}

func depthOf(rel string) int {
	return len(strings.Split(rel, string(filepath.Separator)))
}
