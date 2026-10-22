package presync

import (
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

// quietTracker decides when the watched mail files have gone quiet, per the
// revision-3 rule (design section 4.4.2): quiet is declared only after at least
// one real change has been observed since `up` AND then no change for `quiet`.
// A session in which nothing ever changes waits to `maxWait` and ends
// ok-noactivity — it never concludes at settle+quiet, which is exactly the
// opening silence of Thunderbird's ~10-minute first offline poll (lab L-M5).
type quietTracker struct {
	cfg        quietConfig
	up         time.Time
	lastChange time.Time
	lastDigest uint64
	started    bool // a baseline digest has been taken
	changed    bool // at least one change observed since `up` (the rev-3 gate)
	lastPollAt time.Time
}

func newQuietTracker(up time.Time, cfg quietConfig) *quietTracker {
	return &quietTracker{cfg: cfg, up: up, lastChange: up.Add(cfg.settle)}
}

// observe folds one poll's digest, taken at time now. A digest different from
// the previous poll is a change — growth, an mtime-only touch, a new file, a
// rename or a deletion all move it. A gap longer than suspendGap means the
// machine slept, which resets the quiet clock without counting as a change.
func (q *quietTracker) observe(digest uint64, now time.Time) {
	if !q.lastPollAt.IsZero() && now.Sub(q.lastPollAt) > suspendGap {
		q.lastChange = now
	}
	if q.started && digest != q.lastDigest {
		q.lastChange = now
		q.changed = true
	}
	q.lastDigest = digest
	q.started = true
	q.lastPollAt = now
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
// file under the watch roots into one FNV-1a digest. It never opens a file (so a
// content change that keeps the same size and mtime does not move the digest)
// and never descends a symbolic link (WalkDir does not follow them). It stops
// past the entry or depth cap and reports truncation; quiet is then judged on
// the part walked.
func digestWatch(roots []string) (sum uint64, truncated bool) {
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
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // an unreadable entry is skipped, never aborts the walk
			}
			if entries >= watchMaxEntries {
				truncated = true
				return filepath.SkipAll
			}
			entries++ // every visited node counts toward the cap (dirs/specials/files)
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
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
				return nil
			}
			if !info.Mode().IsRegular() { // symlinks and specials are not followed or recorded
				return nil
			}
			writeStr(rel)
			writeU64(uint64(info.Size()))
			writeU64(uint64(info.ModTime().UnixNano()))
			return nil
		})
	}
	return h.Sum64(), truncated
}

func depthOf(rel string) int {
	return len(strings.Split(rel, string(filepath.Separator)))
}
