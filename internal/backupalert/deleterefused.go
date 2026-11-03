package backupalert

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The count of scheduled runs refused for a delete this computer never began
// (docs/design-archive-delete.md §15.10 slice 6, the owner's ruling Q5, DM-16; MA-1058): a
// scheduled backup into a folder holding another computer's or account's delete marker is refused
// at every run, so without an end it would fail, and alert, for years. Such refused runs are kept
// as the source-loss streak keeps unreachable runs (state.SourceStreak: the same rule, auto-pause
// once they span state.AutoPauseAfter), but in a small file of this computer's, <key>.delete-refused
// beside the alert streak — never in the folder: nothing is written into a half-deleted folder,
// and the folder's own source-loss record (.mailarchive-sourcestreak.json) is inside it.

// DeleteRefusedSuffix ends the count's file name: <key>.delete-refused, beside the Desktop
// settings file. Not .json, so the job folder's scan never reads one as a job; uninstall removes
// them by this suffix, the delete's step 3 removes the folder's own, and ClearStreak removes the
// key's one.
const DeleteRefusedSuffix = ".delete-refused"

const maxDeleteRefusedBytes = 4 * 1024

// DeleteRefusedPath is where the count for key is kept: beside the settings file at settingsPath.
// "" when either is unknown.
func DeleteRefusedPath(settingsPath, key string) string {
	if strings.TrimSpace(settingsPath) == "" || key == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(settingsPath), key+DeleteRefusedSuffix)
}

// NoteDeleteRefused folds one scheduled run of key into the count kept beside the settings file at
// settingsPath, and returns the count after it. refused: the run was refused for a delete this
// computer never began (archdelete.NeverBegunHere), which extends the count as an unreachable run
// extends the source-loss streak (state.SourceStreak.Observe); any other run ends it, and the file
// is removed. A file that cannot be read (not ours, a link, too big) is an error and is left as it
// is, so it never pauses on a count it cannot trust; a damaged one starts again. With no file to
// keep (no settings path or key) it keeps nothing and returns the zero count, which is never due.
func NoteDeleteRefused(settingsPath, key string, refused bool, now time.Time) (state.SourceStreak, error) {
	path := DeleteRefusedPath(settingsPath, key)
	if path == "" {
		return state.SourceStreak{}, nil
	}
	if !refused {
		return state.SourceStreak{}, clearStreakAt(path)
	}
	prev, err := readDeleteRefused(path)
	if err != nil {
		return state.SourceStreak{}, err
	}
	next := prev.Observe(false, "delete-unfinished", now)
	next.Version = state.SourceStreakVersion
	data, err := json.Marshal(next)
	if err != nil {
		return state.SourceStreak{}, err
	}
	if err := util.WriteFileAtomic0600Temp(path, data, ".delete-refused-*.tmp"); err != nil {
		return state.SourceStreak{}, err
	}
	return next, nil
}

// readDeleteRefused reads the count at path: absent or damaged is the zero count; a file that is
// there and cannot be read is an error.
func readDeleteRefused(path string) (state.SourceStreak, error) {
	data, err := util.ReadSecureFile(path, "count of backups refused for an unfinished delete", maxDeleteRefusedBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return state.SourceStreak{}, nil
		}
		return state.SourceStreak{}, err
	}
	var s state.SourceStreak
	if json.Unmarshal(data, &s) != nil {
		return state.SourceStreak{}, nil
	}
	return s, nil
}
