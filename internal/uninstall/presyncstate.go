package uninstall

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// The Thunderbird sync's state folder (design-presync-attended §5.5, slice AS6; C-40,
// C-63): presyncjob.StateDir, where the sync keeps each profile's write-ahead state record
// and session lock and the first-attended-sync marker. On Windows it is not in the config
// folder but in the local, non-roaming instance folder (instance.Dir), and ConfigTargets
// never descends into a folder, so uninstall names it on its own: the files the sync
// writes, by name, and then the folder — never a recursion, never another name. No new
// state location is computed here (MA-621): the folder is built from the same two pinned
// computations presyncjob.StateDir uses.

// presyncStateDir is this computer's pre-sync state folder ("" where the sync has none); a
// var so a test points it at a throwaway folder.
var presyncStateDir = func() (string, error) {
	return presyncStateDirFor(runtime.GOOS, job.Dir, instance.Dir)
}

// presyncStateDirFor is presyncjob.StateDir for the platform goos, which is passed in with
// the two folders it is built from, so every platform's choice can be checked on any
// machine (C-63; a test keeps it in step with presyncjob.StateDir on the machine running
// it): macOS, the job folder (job.Dir) + presync; Windows, the instance folder
// (instance.Dir, the local app-data MailArchiveDesktop folder) + presync. Any other
// platform has no sync, so no folder ("", nil).
func presyncStateDirFor(goos string, jobDir, instanceDir func() (string, error)) (string, error) {
	var base func() (string, error)
	switch goos {
	case "darwin":
		base = jobDir
	case "windows":
		base = instanceDir
	default:
		return "", nil
	}
	dir, err := base()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}

// presyncProfileFile is a profile's state record or session lock: presync-<fnv64 hex>.json
// or .lock (presync.buildDeps).
var presyncProfileFile = regexp.MustCompile(`^presync-[0-9a-f]{1,16}\.(json|lock)$`)

// presyncMarker is the first-attended-sync marker (presyncjob's attendedMarker).
const presyncMarker = "attended-sync-started"

// PresyncStateTargets is what uninstall removes of the pre-sync state folder at dir: the
// regular files the sync writes there, by name, and then dir itself, last — only when those
// are all it holds, so the folder's removal (os.Remove, never RemoveAll) cannot fail on
// something left. A file of any other name, a folder and a link are kept. A session lock
// another run holds right now (a sync under way) is kept, with its profile's state record.
// An absent dir is nothing to remove; a dir that is, holds or sits in a known archive
// refuses (ErrOverlap, R36). It deletes nothing itself.
func PresyncStateTargets(dir string, knownArchives []string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, nil // not a plain folder (a link, a file): not ours to touch
	}
	if isArchiveDir(dir) {
		return nil, ErrOverlap
	}
	rd := util.ResolveExisting(dir)
	for _, a := range knownArchives {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		ra := util.ResolveExisting(a)
		if util.Within(rd, ra) || util.Within(ra, rd) {
			return nil, ErrOverlap
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{} // profile files kept because their lock is held now
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(n, ".lock") && presyncProfileFile.MatchString(n) && lockfile.Held(filepath.Join(dir, n)) {
			held[strings.TrimSuffix(n, ".lock")] = true
		}
	}
	var targets []string
	all := true
	for _, e := range entries {
		n := e.Name()
		owned := e.Type().IsRegular() && (n == presyncMarker || presyncProfileFile.MatchString(n))
		if owned && presyncProfileFile.MatchString(n) && held[strings.TrimSuffix(strings.TrimSuffix(n, ".json"), ".lock")] {
			owned = false
		}
		if !owned {
			all = false
			continue
		}
		targets = append(targets, filepath.Join(dir, n))
	}
	sort.Strings(targets)
	if all {
		targets = append(targets, dir)
	}
	return targets, nil
}
