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
// lid, say); the gap is not counted as quiet (design section 4.4.2), nor, past
// the limit itself, toward the maximum wait (MA-935). It is judged on wall time,
// from the end of one poll's walk to the start of the next, so a slow walk is not
// a sleep (quietConfig.sleepGap stretches it for a poll slower than the default).
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
	maxWait time.Duration // longest to wait for quiet: time awake since `up` (MA-935)
	poll    time.Duration // the session's poll interval; one slower than the default stretches sleepGap
}

// sleepGap is the gap between polls past which the machine is judged to have
// slept: suspendGap at the default 10 s poll, stretched by as much as a slower
// poll adds, so a coarse poll is not taken for a sleep.
func (c quietConfig) sleepGap() time.Duration {
	if c.poll > defPollEvery {
		return suspendGap + c.poll - defPollEvery
	}
	return suspendGap
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
// the quiet clock, as before. The session seeds it with a snapshot taken before
// the launch (baseline, MA-934), so mail written before the first wait poll counts.
type quietTracker struct {
	cfg        quietConfig
	up         time.Time
	lastChange time.Time
	lastDigest uint64
	started    bool // a baseline digest has been taken
	changed    bool // mail data seen to arrive since `up` (the rev-3 gate, MA-932)
	lastPollAt time.Time
	slept      time.Duration // time judged asleep since `up`, not counted toward maxWait (MA-935)

	last  watchSnap        // the previous poll
	known map[string]int64 // every regular file seen this session, mail or not, at its last size
}

func newQuietTracker(up time.Time, cfg quietConfig) *quietTracker {
	return &quietTracker{cfg: cfg, up: up, lastChange: up.Add(cfg.settle), known: map[string]int64{}}
}

// baseline takes s, a snapshot made before the launch while no Thunderbird ran, as
// the starting point the first poll is compared with (MA-934). Mail that
// Thunderbird writes before the first wait poll (the macOS lab saw it at +6 s; on
// Windows the minimize may take 15 s first) then opens the gate, instead of
// hiding in a starting point taken after it.
func (q *quietTracker) baseline(s watchSnap) {
	q.remember(s)
}

// observe folds one poll whose walk ended at now; observeWalk is the same with
// the walk's start given too.
func (q *quietTracker) observe(s watchSnap, now time.Time) { q.observeWalk(s, now, now) }

// observeWalk folds one poll, walked from walkStart to now. A digest different
// from the previous poll is a change — growth, an mtime-only touch, a new file, a
// rename or a deletion all move it — and resets the quiet clock; it opens the
// first-change gate only when mail data arrived (mailArrived). A gap longer than
// sleepGap between the previous walk's end and this walk's start, on wall time
// (the monotonic clock stops in sleep on macOS, design section 4.4.2, lab L-C3),
// means the machine slept: that resets the quiet clock without counting as a
// change, and the time past sleepGap is not counted toward maxWait (MA-935). A
// sleep inside a walk is not seen; a walk is normally well under a second.
func (q *quietTracker) observeWalk(s watchSnap, walkStart, now time.Time) {
	if !q.lastPollAt.IsZero() {
		if gap, limit := walkStart.Round(0).Sub(q.lastPollAt.Round(0)), q.cfg.sleepGap(); gap > limit {
			q.lastChange = now
			q.slept += gap - limit
		}
	}
	if q.started && s.sum != q.lastDigest {
		q.lastChange = now
		if !q.changed && q.mailArrived(s) {
			q.changed = true
		}
	}
	q.remember(s)
	q.lastPollAt = now
}

// remember makes s the previous poll and adds its files to the session's known set.
func (q *quietTracker) remember(s watchSnap) {
	for p, sz := range s.other {
		q.known[p] = sz
	}
	for p, sz := range s.store {
		q.known[p] = sz
	}
	q.last = s
	q.lastDigest = s.sum
	q.started = true
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
// flight at maxWait is not-quiet. maxWait counts time awake: wall time since `up`
// less the time judged asleep (MA-935), so a computer that slept mid-wait does not
// end "nothing arrived" on its first poll after waking.
func (q *quietTracker) verdict(now time.Time) (Outcome, bool) {
	if q.changed && now.Sub(q.lastChange) >= q.cfg.quiet {
		return OutcomeOK, true
	}
	if q.awake(now) >= q.cfg.maxWait {
		if q.changed {
			return OutcomeNotQuiet, true
		}
		return OutcomeNoActivity, true
	}
	return Outcome{}, false
}

// awake is the time the wait has run with the machine awake: wall time since `up`
// less the time judged asleep (MA-935).
func (q *quietTracker) awake(now time.Time) time.Duration {
	return now.Round(0).Sub(q.up.Round(0)) - q.slept
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
	var feedDirs []string            // RSS accounts: the folders holding feeds.json (MA-933)
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
			if _, dup := files[path]; dup {
				// Under a root nested in another (MA-933): a file keeps its first visit's
				// walk key, so the cap's stopping point moving cannot make it look new.
				return nil
			}
			if isFeedsIndex(d.Name()) {
				feedDirs = append(feedDirs, filepath.Dir(path))
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
		if isStoreFile(p, hasIndex) && !inAny(feedDirs, p) {
			snap.store[f.key] = f.size
		} else {
			snap.other[f.key] = f.size
		}
	}
	snap.sum = h.Sum64()
	return snap, truncated
}

// isStoreFile reports whether the regular file at path holds Thunderbird mail
// data (MA-932, MA-933), judged from names alone — never by opening it. What
// Thunderbird keeps under ImapMail/<server>/ and Mail/<account>/:
//   - a folder's mail: an mbox file named after the folder ("INBOX", "Sent
//     Items"), beside its summary index "<folder>.msf"; a folder name may hold a
//     dot ("Lists.golang"); a subfolder's files sit in a "<folder>.sbd"
//     directory, the same way;
//   - in a maildir account, one file per message in the folder directory's "cur"
//     (and, for a store that uses it, "new"); "tmp" holds half-written files;
//   - and files that are not mail: the .msf indexes, msgFilterRules.dat,
//     popstate.dat, filterlog.html, junklog.html, feeds.json and feeditems.json
//     (older: .rdf), search-integration copies (*.mozmsgs/*.wdseml, *.mozeml),
//     the operating system's own (.DS_Store, AppleDouble "._" files, desktop.ini,
//     Thumbs.db), and compaction's temporary files: "nstmp" (Thunderbird 115,
//     which writes an "nstmp.msf" beside it in a local folder; "nstmp-1" when
//     the name is taken) and "<folder>-1" (Thunderbird 128, through a safe
//     output stream's unique name, with no index of its own) — each renamed over
//     the folder when done, and a compaction is not new mail.
//
// So a file is mail when its directory is a maildir "cur" or "new" and it is not
// one of the system's own files, or it has a sibling "<name>.msf" and is not
// compaction's nstmp — but an .msf is never mail, nor is a file in a directory
// named "tmp" (a maildir's half-written message; a folder's own directory is
// "<name>.sbd", never "tmp"). A file with no index is not mail whatever its name
// (MA-933): that is what keeps 128's "INBOX-1" out, and a mailbox whose index
// appears later is compared with its earlier size, never taken for new mail
// (quietTracker.known). The caller also leaves out an RSS account (feeds.json).
// hasIndex says whether the walk saw a regular file "<path>.msf" beside it.
func isStoreFile(path string, hasIndex bool) bool {
	name := filepath.Base(path)
	if strings.EqualFold(filepath.Ext(name), ".msf") {
		return false
	}
	switch filepath.Base(filepath.Dir(path)) {
	case "cur", "new":
		return !isSystemFile(name)
	case "tmp":
		return false // a maildir message still being written; it counts once moved to cur
	}
	return hasIndex && !isCompactionTemp(name)
}

// isSystemFile reports a file the operating system writes into a folder someone
// opened: dot files (.DS_Store, AppleDouble "._x") and Explorer's Thumbs.db and
// desktop.ini. Thunderbird's maildir messages never start with a dot.
func isSystemFile(name string) bool {
	return strings.HasPrefix(name, ".") || strings.EqualFold(name, "Thumbs.db") || strings.EqualFold(name, "desktop.ini")
}

// isCompactionTemp reports compaction's "nstmp" in any case, and the unique
// "nstmp-N" a taken name gets, with or without an extension.
func isCompactionTemp(name string) bool {
	base, _, _ := strings.Cut(strings.ToLower(name), ".")
	n, ok := strings.CutPrefix(base, "nstmp")
	if !ok {
		return false
	}
	if n == "" {
		return true
	}
	digits, ok := strings.CutPrefix(n, "-")
	if !ok || digits == "" {
		return false
	}
	return strings.Trim(digits, "0123456789") == ""
}

// isFeedsIndex reports an RSS account's feed list (feeds.json; feeds.rdf before
// Thunderbird 68): the folder holding it is a feeds account, whose folders hold
// feed items, not mail (MA-933).
func isFeedsIndex(name string) bool {
	return strings.EqualFold(name, "feeds.json") || strings.EqualFold(name, "feeds.rdf")
}

// inAny reports whether path lies inside any of dirs.
func inAny(dirs []string, path string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(path, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func depthOf(rel string) int {
	return len(strings.Split(rel, string(filepath.Separator)))
}
