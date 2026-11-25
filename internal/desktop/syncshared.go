package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/thunderbird"
)

// The Settings card's sync switch when another archive already syncs the same Thunderbird profile
// (docs/design-per-mailbox-archives.md §11 "The Settings card's sync switch", revision 2's C24;
// MA-1498). Today's sync starts Thunderbird for one archive's profile and takes its profile lock with
// no wait, so a second archive syncing the same profile would start it twice or be left "profile
// busy". A shared sync is later work (§13); until then the switch refuses the second. Archives
// already set up both ways are left as they are: only switching the sync on is refused.

// Its words (§11; the friction review's F13 says where: "switch it off there" and "Repair that
// archive" did not).
const (
	syncSharedWords      = "Thunderbird sync is already on for %s, which backs up the same Thunderbird profile. Only one archive can sync a profile for now: switch it off on that archive's Settings card first (Switch to this archive on its row)."
	syncSharedUnreadable = "Could not read the settings of %s: %s. Try again, or switch to that archive (Switch to this archive on its row) and click Repair on its Scheduled backup card."
)

// findProfile is the one look that finds the Thunderbird profile a mail folder belongs to
// (thunderbird.FindProfileDir); a seam, so a test can see which paths are looked at and model a
// stalled store.
var findProfile = thunderbird.FindProfileDir

// syncSharedRefusal is why the Thunderbird sync can't be switched on for the archive at out, whose
// job is j, in §11's words, or "" when it can. The other archives are the Desktop's known archives
// (its list) whose schedule is installed and not paused (Pause, make read-only). Each one's job file
// is read with job.Read: one that is there and cannot be read stops the switch, naming it; one that
// is not there syncs nothing. One whose job syncs Thunderbird and covers a profile this archive's
// job covers (jobProfiles, compared by identity) stops the switch, naming that archive.
func (d *dashboard) syncSharedRefusal(out string, j job.Job) string {
	mine := jobProfiles(j)
	if len(mine) == 0 || strings.TrimSpace(d.cfg.IndexPath) == "" {
		return ""
	}
	idx, err := archindex.ReadFile(d.cfg.IndexPath)
	if err != nil {
		return ""
	}
	self, _ := os.Stat(out)
	for _, e := range idx.Entries {
		k := cleanPath(e.Path)
		if k == "" || k == cleanPath(out) {
			continue
		}
		if fi, err := os.Stat(k); err == nil && self != nil && os.SameFile(fi, self) {
			continue
		}
		if d.cfg.taskState(k) != schedule.Installed || seal.IsSealed(k) {
			continue
		}
		jp, err := job.PathFor(schedule.DefaultNameFor(k))
		if err != nil {
			continue
		}
		if _, err := os.Lstat(jp); errors.Is(err, fs.ErrNotExist) {
			continue // no job file: nothing of it syncs Thunderbird
		}
		other, err := job.Read(jp)
		if err != nil {
			return fmt.Sprintf(syncSharedUnreadable, k, jp)
		}
		if other.ThunderbirdSync == nil {
			continue
		}
		for _, p := range jobProfiles(other) {
			for _, m := range mine {
				if os.SameFile(p, m) {
					return fmt.Sprintf(syncSharedWords, k)
				}
			}
		}
	}
	return ""
}

// jobProfiles are the Thunderbird profiles a job's sync covers (§11): its ThunderbirdSync.Profile
// when set, else the profile of each of its inputs (findProfile); a job with auto-discovery on also
// covers every profile auto-detect finds (autoProfiles). A path on a network or not a plain local
// path is never looked at (MA-869's rule, isRemotePath). The looks are asked within sourceWait: an
// answer that does not come in time names no profile, as the page's other looks at mail sources do.
func jobProfiles(j job.Job) []os.FileInfo {
	v, _ := withinSourceWait(func() []os.FileInfo {
		var dirs []string
		if j.ThunderbirdSync != nil && strings.TrimSpace(j.ThunderbirdSync.Profile) != "" {
			dirs = append(dirs, j.ThunderbirdSync.Profile)
		} else {
			for _, in := range j.Inputs {
				if isRemotePath(in) {
					continue
				}
				if p, ok := findProfile(in); ok {
					dirs = append(dirs, p)
				}
			}
		}
		if j.Auto {
			dirs = append(dirs, autoProfiles()...)
		}
		var out []os.FileInfo
		for _, d := range dirs {
			if isRemotePath(d) {
				continue
			}
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				out = append(out, fi)
			}
		}
		return out
	})
	return v
}

// autoProfiles are the Thunderbird profiles of the mail folders auto-detect finds (the places
// autoplace.ThunderbirdGlobs names, as the run's own auto-discovery looks), each once.
func autoProfiles() []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range autoplace.ThunderbirdGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if isRemotePath(m) {
				continue
			}
			if p, ok := findProfile(m); ok && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}
