package archdelete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"mail-archive-tool/internal/util"
)

// RowRefusal is what a row of Your archives says about Delete before any plan is made
// (docs/design-archive-delete.md §10: Delete's enabled state comes from §4's archive test
// and §7, not from the row's greyed state, and when it is disabled its title says why in
// §7's words; MA-975): a Desktop started with -out naming the folder, a folder that is or
// holds a link, a protected folder (§7's protected row, another known archive among them),
// the folder not reachable, the capture of the current archive registered under its name,
// the archive test (not an archive, a mixed folder, a manifest it cannot read or a newer
// one), a backup running on it. An unfinished delete of the folder in deps.Log (§5.4) is
// never refused here but for a link or a protected folder, which the plan refuses as well:
// Delete again finishes it, and the plan decides. knownArchive skips the archive test when
// the caller's own read already found an archive (archindex's bounded read). nil: Delete is
// offered. A page of rows asks RowsCheck instead, which reads deletions.log once.
func RowRefusal(folder string, deps Deps, knownArchive bool) error {
	_, err := NewRowsCheck(deps).State(folder, knownArchive)
	return err
}

// RowsCheck answers RowRefusal and the row's unfinished delete for a page of rows at once
// (del-s3a-A3-04, A4-05): deletions.log is read once, and each path the checks ask of the
// file system — a row, a logged folder, a protected folder and its ancestors — is asked once
// for its life, so a folder on a network share that is offline costs one wait per page, not
// one per row and line.
type RowsCheck struct {
	deps Deps
	stat *statMemo
	log  *DeleteLog
}

// NewRowsCheck reads deps.Log once (one that cannot be read reports no unfinished delete,
// and the plan says why).
func NewRowsCheck(deps Deps) *RowsCheck {
	r := &RowsCheck{deps: deps, stat: newStatMemo()}
	if deps.Log != "" {
		if lines, err := readLog(deps.Log); err == nil {
			r.log = newDeleteLog(lines, r.stat)
		}
	}
	return r
}

// State is the row's unfinished delete of folder (§5.4; nil when none) and RowRefusal's
// answer for it.
func (r *RowsCheck) State(folder string, knownArchive bool) (*Unfinished, error) {
	abs := cleanAbs(folder)
	deps := r.deps
	goos := deps.goos()
	stat := r.stat.stat
	var u *Unfinished
	if r.log != nil {
		u = r.log.Unfinished(abs, goos)
	}
	// A link, or a protected folder, the plan refuses whatever else holds, so the row does not
	// offer Delete for it (del-s3a-A2-02); and in planArchive's order — the folder a link, then
	// protected, then started with -out, then the manifest itself a link — so a folder that is
	// more than one says the same on the row and in the plan (del-s3a re-verification, R4).
	fi, lerr := os.Lstat(abs)
	if lerr == nil {
		if link, err := util.IsLinkLike(abs); err == nil && link {
			return u, linkRefusal(abs)
		}
		if fi.IsDir() {
			if err := checkProtectedStat(stat, abs, deps.Protected); err != nil {
				return u, err
			}
		}
	}
	if deps.Settings.LaunchOut != "" && sameFolderStat(stat, deps.Settings.LaunchOut, abs, goos) {
		return u, refuse(CaseLaunchOut, msgLaunchOut)
	}
	if lerr == nil && fi.IsDir() {
		// The plan's archive test refuses a manifest that is a link, a known archive's too
		// (classifyWith), which the row's knownArchive skips.
		mp := filepath.Join(abs, manifestName)
		if mfi, err := os.Lstat(mp); err == nil {
			if link, err := util.IsLinkLike(mp); mfi.Mode()&fs.ModeSymlink != 0 || (err == nil && link) {
				return u, linkRefusal(mp)
			}
		}
	}
	if u != nil {
		return u, nil
	}
	fi, err := stat(abs)
	if err != nil || !fi.IsDir() {
		if err != nil {
			return nil, refuse(CaseUnreachable, msgUnreachable)
		}
		return nil, refuse(CaseNotArchive, msgNotArchive)
	}
	if err := checkBackupFor(abs, deps); err != nil {
		return nil, err
	}
	if !knownArchive {
		k, err := rowArchiveTest(abs)
		if err != nil {
			return nil, err
		}
		if k == 0 {
			return nil, refuse(CaseNotArchive, msgNotArchive)
		}
	}
	if deps.Busy != nil && deps.Busy(abs) {
		return nil, refuse(CaseBusy, msgBusy)
	}
	return nil, nil
}

// rowArchiveTest is §4's archive test for a row, its manifest parse kept by the manifest's
// identity, size and modification time (del-s3a-A3-03): archindex's 1 MiB read passes over an
// archive of a few thousand messages, and the row then asked the whole manifest — megabytes,
// over a network drive seconds — on every page load. A manifest that changed is parsed again;
// the plan never uses what is kept here, and reads it whole.
func rowArchiveTest(folder string) (Kind, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return 0, refuse(CaseUnreachable, msgUnreachable)
	}
	defer root.Close()
	return classifyWith(root, folder, rowManifests.parse)
}

// manifestParses keeps rowArchiveTest's parses.
type manifestParses struct {
	mu sync.Mutex
	m  map[string]manifestParse
}

type manifestParse struct {
	fi  os.FileInfo
	err error
}

// manifestParsesMax bounds what is kept: far more rows than a page has.
const manifestParsesMax = 512

var rowManifests = &manifestParses{m: map[string]manifestParse{}}

// parse is readManifest's verdict for the manifest fi (an Lstat of it) in the folder abs.
func (c *manifestParses) parse(root *os.Root, abs string, fi os.FileInfo) error {
	c.mu.Lock()
	k, ok := c.m[abs]
	c.mu.Unlock()
	if ok && os.SameFile(k.fi, fi) && k.fi.Size() == fi.Size() && k.fi.ModTime().Equal(fi.ModTime()) {
		return k.err
	}
	_, _, err := readManifest(root)
	var re *manifestReadError
	if errors.As(err, &re) {
		return err // a read that failed (a sharing violation, an I/O error) may pass next time
	}
	c.mu.Lock()
	if len(c.m) >= manifestParsesMax {
		c.m = map[string]manifestParse{}
	}
	c.m[abs] = manifestParse{fi: fi, err: err}
	c.mu.Unlock()
	return err
}
