package source

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	"mail-archive-tool/internal/util"
)

// MailArchive never backs up its own restore leftovers (docs/design-restore-thunderbird.md
// §7.2 as folded by RT-5, foldcheck M3 and foldcheck2 P3; invariant R41). A restore into
// Thunderbird writes one folder "Restored — NAME" whose subfolders sit in
// "Restored — NAME.sbd"; inside that container its marker, a hidden one-line file,
// names the restore. Before the walk enters a container it reads the marker (a regular
// file, at most 4 KiB, through the walk's own root) and leaves the container out when:
//   - the marker is well-formed and the caller's record knows its restore for the
//     marker's profile, and there is no entry of the parent name beside it (an
//     unfinished or abandoned leftover: Thunderbird does not show it); or
//   - the same, and the restore committed (the record has its "checked" line and no
//     "taken" line) and the parent name beside it is a regular file: then the parent
//     file is left out too (the finished restored folder, owner question O1's
//     recommended answer (b));
//   - or the record could not be read, and a well-formed marker has no entry of the
//     parent name beside it.
//
// Any other marked container — an unknown restore, an entry of the parent name that is
// a folder or a link, a restore that did not commit — is walked as an ordinary folder,
// the marker and the made-list inside it never read as mailboxes (they are dot-named).

// RestoreSkip is the caller's record of its restores (tbrestore.SkipSet).
type RestoreSkip interface {
	// Lookup reports whether the record knows the restore id for the profile key the
	// marker names, and whether it committed. An unknown id may make the record re-read
	// its log once (foldcheck M3).
	Lookup(id, profileKey string) (known, committed bool)
	// LogUnreadable reports that the record could not be read.
	LogUnreadable() bool
}

// RestoreMarkerName is the restore's marker inside its container (tbrestore.MarkerName;
// MA-1138's test keeps the two in step by behaviour: a restore tbrestore writes is left
// out).
const RestoreMarkerName = ".mailarchive-restore"

// restoreMarkerMax bounds the marker read.
const restoreMarkerMax = 4 << 10

type restoreMarker struct {
	MailArchive string `json:"mailarchive"`
	ID          string `json:"id"`
	Profile     string `json:"profile"`
}

// leaveOutRestore reports whether the container name+".sbd" in dir is a restore's
// folder the walk leaves out, and whether the parent entry beside it goes with it.
func (w *localWalk) leaveOutRestore(sc *scope, dir, name string) (container, parent bool) {
	rs := w.opts.RestoreSkip
	if rs == nil {
		return false, false
	}
	srel := relJoin(dir, name+".sbd")
	fi, err := sc.lstat(srel)
	if err != nil || !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return false, false
	}
	m, ok := readRestoreMarker(sc, relJoin(srel, RestoreMarkerName))
	if !ok {
		return false, false
	}
	pfi, perr := sc.lstat(relJoin(dir, name))
	noParent := errors.Is(perr, fs.ErrNotExist)
	if rs.LogUnreadable() {
		return noParent, false
	}
	known, committed := rs.Lookup(m.ID, m.Profile)
	switch {
	case !known:
		return false, false
	case noParent:
		return true, false
	case perr == nil && pfi.Mode().IsRegular() && committed:
		return true, true
	}
	return false, false
}

// readRestoreMarker reads a marker: a regular file of at most 4 KiB, never through a
// link, that parses as a restore's marker.
func readRestoreMarker(sc *scope, rel string) (restoreMarker, bool) {
	fi, err := sc.lstat(rel)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > restoreMarkerMax {
		return restoreMarker{}, false
	}
	f, err := util.OpenRegularFileIn(sc.root, filepath.FromSlash(rel))
	if err != nil {
		return restoreMarker{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, restoreMarkerMax+1))
	if err != nil || len(data) > restoreMarkerMax {
		return restoreMarker{}, false
	}
	var m restoreMarker
	if json.Unmarshal(data, &m) != nil || m.MailArchive != "restore" || m.ID == "" {
		return restoreMarker{}, false
	}
	return m, true
}

func (w *localWalk) noteRestoreSkipped(path []string) {
	if w.opts.RestoreSkipped != nil {
		w.opts.RestoreSkipped(path)
	}
}
