package desktop

import (
	"errors"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
)

// A folder holding the delete marker, listed so its delete can be finished here
// (docs/design-archive-delete.md §15.4, DM-17; §15.10 slice 4; MA-1052). Open an archive folder…
// and `mailarchive add-archive` accept such a folder, with or without its manifest (they refused one
// without: "isn't a MailArchive archive"): the row is added — never made the archive in use, which
// would be a Switch the marker refuses — so a computer that never listed the folder can finish a
// delete whose manifest is gone. archindex does not change: it still recognises an archive by its
// manifest or encrypted marker, so a folder holding only the marker is a greyed row whose "Delete not
// finished" line comes from the delete's own check.

// markerFolderAt reports a folder that holds the delete marker (a regular file, or a name that is not
// one: its row then says to remove it by hand) and is not a folder inside an encrypted archive.
func markerFolderAt(abs string) bool {
	if dir, _, _ := encmark.AtOrAbove(abs); dir != "" && cleanPath(dir) != cleanPath(abs) {
		return false
	}
	st, err := delmark.Present(abs)
	return err == nil && st != delmark.Absent
}

// listMarkerFolder adds abs, a folder markerFolderAt accepts, to Your archives under the list's lock
// (archindex.UpdateFile, CON-07), as Reconcile adds an archive but without its archive test, which
// such a folder may not pass. row is the row as the list has it; added is false when a row of the
// folder was there already (by its spelling, then by file identity, matchRow's rule).
func (cfg Config) listMarkerFolder(abs string, now time.Time) (row string, added bool, err error) {
	abs = filepath.Clean(abs)
	row = abs
	err = archindex.UpdateFile(cfg.IndexPath, func(idx archindex.Index) (archindex.Index, bool, error) {
		if r, ok := rowIn(idx, abs); ok {
			row = r
			return idx, false, nil
		}
		if len(idx.Entries) >= archindex.MaxEntries {
			return idx, false, errListFull
		}
		idx.Entries = append(idx.Entries, archindex.Entry{Path: abs, AddedAt: now, LastSeenAt: now})
		added = true
		return idx, true, nil
	})
	return row, added, err
}

// errListFull is listMarkerFolder's refusal of a list already holding archindex.MaxEntries rows.
var errListFull = errors.New("Your archives already lists as many archives as MailArchive keeps; remove one from the list first")

// The words for a marker folder listed (§15.4's, lead's words owner-delegated 2026-11-02: what
// happened first, the remedy named; no "again", since this person may have begun no delete).
func markerAddedText(dir string) string {
	return "Added to Your archives: " + dir + ". " + markerFinishText
}

func markerListedText(dir string) string {
	return dir + " is already in Your archives. " + markerFinishText
}

const markerFinishText = "A delete of this archive did not finish: to finish it, click Delete… on its row."
