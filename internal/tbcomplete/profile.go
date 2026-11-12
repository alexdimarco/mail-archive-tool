package tbcomplete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/thunderbird"
)

// Tri is a yes, a no, or "can't tell". Its zero value is CantTell, so an answer nobody set is
// never read as a no.
type Tri int

const (
	CantTell Tri = iota
	No
	Yes
)

// Profile is what the profile itself says around folderCache.json (slice C1a's Panorama signals
// and version read; design §2.3's TC-3 fold and §6's L5 fold). Each signal is reported on its own;
// C1b decides what they mean (either Panorama signal Yes, or CantTell, is `unknown`).
type Profile struct {
	// PanoramaFile: a directory entry named panorama.sqlite is in the profile folder, seen by Lstat
	// and never opened, whatever kind of entry it is. CantTell when Lstat fails for another reason
	// than "not there".
	PanoramaFile Tri
	// PanoramaPref: mail.panorama.enabled as Thunderbird applies it (thunderbird.PanoramaEnabled).
	PanoramaPref Tri
	// VersionMajor is the major number of the Thunderbird that last opened the profile
	// (compatibility.ini's LastVersion), when VersionKnown.
	VersionMajor int
	VersionKnown bool
}

// panoramaFile is the file Panorama keeps its database in (DatabaseCore.cpp, ESR 153 lines
// 136-151).
const panoramaFile = "panorama.sqlite"

// ReadProfile reads profileDir's Panorama signals and Thunderbird version. It writes nothing and
// opens no file but prefs.js, user.js and compatibility.ini, each through util.ReadRegularFile.
func ReadProfile(profileDir string) Profile {
	var p Profile
	switch _, err := os.Lstat(filepath.Join(profileDir, panoramaFile)); {
	case err == nil:
		p.PanoramaFile = Yes
	case errors.Is(err, fs.ErrNotExist):
		p.PanoramaFile = No
	}
	switch on, err := thunderbird.PanoramaEnabled(profileDir); {
	case err != nil:
	case on:
		p.PanoramaPref = Yes
	default:
		p.PanoramaPref = No
	}
	p.VersionMajor, p.VersionKnown = thunderbird.LastVersionMajor(profileDir)
	return p
}
