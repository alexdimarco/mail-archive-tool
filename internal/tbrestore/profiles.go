package tbrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

// Profile is one Thunderbird profile the restore can offer (design §3.1).
type Profile struct {
	// Name is profiles.ini's Name=, as shown: bidirectional, C0, DEL and C1 controls
	// removed (§3.1 step 2 notes).
	Name string
	// Dir is the profile folder's canonical path (links resolved).
	Dir string
	// OpensByDefault: an [Install…] Default= names it (the profile Thunderbird opens).
	OpensByDefault bool
	// DefaultFlag: its section says Default=1.
	DefaultFlag bool
	// Usable is false for a profile listed as not usable, with Unusable's words
	// (W-PROFMODE, §3.1 step 3 note): it is shown, never written into.
	Usable   bool
	Unusable string
	// Identity pins Dir between the plan and the start.
	Identity Identity
}

// ProfileList is the list the server made and sent (§3.1 step 6 note): the page
// names a profile by its position and the list's ID, never by a path.
type ProfileList struct {
	ID       string
	Profiles []Profile
	bases    []string
}

// ProfilesININame is Thunderbird's list of profiles.
const ProfilesININame = "profiles.ini"

// ProfileBases are the folders that may hold Thunderbird's profiles.ini on this
// computer, in the order they are read (§3.1 step 1).
func ProfileBases() []string { return profileBases() }

// ListProfiles reads every profiles.ini of this computer (ProfileBases) and lists its
// profiles. No profiles.ini at all is an empty list, not an error (W-NOPROF is the
// caller's when nothing is usable). A profiles.ini over 256 KiB is refused, W-PROFINI,
// never cut short as presync's read is (§3.1 step 2 note).
func ListProfiles() (*ProfileList, error) { return ReadProfiles(ProfileBases()) }

// ReadProfiles is ListProfiles over the given folders.
func ReadProfiles(bases []string) (*ProfileList, error) {
	l := &ProfileList{bases: bases}
	seen := map[string]bool{}
	for _, base := range bases {
		data, err := readProfilesINI(filepath.Join(base, ProfilesININame))
		if err != nil {
			return nil, err
		}
		if data == nil {
			continue
		}
		secs := thunderbird.ParseProfilesINI(data)
		opens := map[string]bool{}
		for _, s := range secs {
			if !s.IsInstall() {
				continue
			}
			for _, d := range s.Defaults {
				opens[canonical(iniPath(base, d, !filepath.IsAbs(d)))] = true
			}
		}
		for _, s := range secs {
			if s.IsInstall() || s.Path == "" || !strings.HasPrefix(s.Header, "Profile") {
				continue
			}
			spelled := iniPath(base, s.Path, s.IsRelative)
			dir := canonical(spelled)
			_, jerr := resolveFolder(spelled)
			if seen[dir] {
				continue
			}
			p, ok := checkProfile(dir)
			if !ok {
				continue // not there, or no prefs.js: not a profile, not offered
			}
			if RefusalCode(jerr) == "W-JUNCTION" {
				p.Usable, p.Unusable = false, jerr.Error() // R6-14: listed, never written into
			}
			seen[dir] = true
			p.Name = shownName(s.Name)
			p.OpensByDefault = opens[dir]
			p.DefaultFlag = s.DefaultFlag()
			l.Profiles = append(l.Profiles, p)
		}
	}
	l.ID = listID(l.Profiles)
	return l, nil
}

// iniPath is a profiles.ini path value as a folder: relative values are joined to the
// folder holding profiles.ini (written with "/" on every platform).
func iniPath(base, v string, rel bool) string {
	if rel {
		return filepath.Join(base, filepath.FromSlash(v))
	}
	return filepath.Clean(v)
}

// canonical resolves links when it can, else cleans.
func canonical(p string) string {
	if r, err := resolveFolder(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// resolveFolder is an existing folder's path with its links resolved (R6-14, settled
// 2026-11-07). On Windows filepath.EvalSymlinks fails on a junction in the middle of a
// path (Go 1.23 and later), which refused every profile moved with `mklink /J` in the
// words of a missing profile; so there the repository's own resolver (util.ResolveExisting,
// through junctions and links alike) is used, and a path reached through a link or a
// junction is allowed only when it leads inside the user's own folder — else W-JUNCTION,
// naming where it leads. Elsewhere links are resolved as before.
func resolveFolder(p string) (string, error) {
	if runtime.GOOS != "windows" {
		return filepath.EvalSymlinks(p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	real := util.ResolveExisting(abs)
	if util.ThroughLink(abs) {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" || !util.Within(util.ResolveExisting(home), real) {
			return "", junctionRefusal(abs, real, home)
		}
	}
	return real, nil
}

// readProfilesINI reads profiles.ini bounded: nil when there is none (or it is not an
// ordinary file), W-PROFINI when it is over the cap.
func readProfilesINI(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, thunderbird.ProfilesINIMaxBytes+1))
	if err != nil {
		return nil, nil
	}
	if len(data) > thunderbird.ProfilesINIMaxBytes {
		return nil, refuse("W-PROFINI", wProfINI, nil)
	}
	return data, nil
}

// shownName is a Name= as the page and the terminal show it.
func shownName(s string) string {
	s = util.StripBidi(thunderbird.StripControl(s))
	return strings.Map(func(r rune) rune {
		if r == 0x200e || r == 0x200f {
			return -1
		}
		return r
	}, s)
}

// checkProfile is §3.1 step 3: the folder exists, is a folder and holds prefs.js as
// a regular file (else ok is false: not a profile); on macOS and Linux it must also be
// this user's and not group- or world-writable, else it is listed as not usable with
// W-PROFMODE. Its identity is read for the list's binding.
func checkProfile(dir string) (Profile, bool) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return Profile{}, false
	}
	if pj, err := os.Lstat(filepath.Join(dir, "prefs.js")); err != nil || !pj.Mode().IsRegular() {
		return Profile{}, false
	}
	p := Profile{Dir: dir, Usable: true}
	if !profileModeOK(fi) {
		p.Usable = false
		p.Unusable = fill(wProfMode, "PATH", dir)
	}
	id, err := DirIdentity(dir)
	if err != nil {
		p.Usable = false
		if p.Unusable == "" {
			p.Unusable = fill(wNotProfile, "PATH", dir)
		}
	}
	p.Identity = id
	return p, true
}

// CheckProfile is the check a profile named by the person (the command line's
// -profile FOLDER) must pass: accepted without being listed, as presync accepts a
// named profile, but only through §3.1 step 3.
func CheckProfile(dir string) (Profile, error) {
	cdir, err := resolveFolder(dir)
	if RefusalCode(err) != "" {
		return Profile{}, err
	}
	if err != nil {
		return Profile{}, refuse("W-NOTPROFILE", fill(wNotProfile, "PATH", dir), err)
	}
	p, ok := checkProfile(cdir)
	if !ok {
		return Profile{}, refuse("W-NOTPROFILE", fill(wNotProfile, "PATH", cdir), nil)
	}
	if !p.Usable {
		return Profile{}, refuse("W-PROFMODE", p.Unusable, nil)
	}
	return p, nil
}

func listID(ps []Profile) string {
	h := sha256.New()
	for _, p := range ps {
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%s\x00%t\x00%t\x00%t\n", p.Dir, p.Identity.Volume, p.Identity.FileID, p.Name, p.Usable, p.OpensByDefault, p.DefaultFlag)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Resolve gives the profile at pos in the list the page was sent, after reading
// profiles.ini again (§3.1 step 6 note): refused when the list the page names is not
// the one there now (another ID), when the entry at pos is not the same folder with
// the same identity, or when it is not usable.
func (l *ProfileList) Resolve(listIDSent string, pos int) (Profile, error) {
	now, err := ReadProfiles(l.bases)
	if err != nil {
		return Profile{}, err
	}
	if listIDSent != l.ID || now.ID != l.ID || pos < 0 || pos >= len(now.Profiles) || pos >= len(l.Profiles) {
		return Profile{}, refuse("W-PROFCHANGED", wProfChanged, nil)
	}
	was, is := l.Profiles[pos], now.Profiles[pos]
	if was.Dir != is.Dir || !was.Identity.Same(is.Identity) {
		return Profile{}, refuse("W-PROFCHANGED", wProfChanged, nil)
	}
	if !is.Usable {
		return Profile{}, refuse("W-PROFMODE", is.Unusable, nil)
	}
	return is, nil
}

// Usable are the positions of the usable profiles.
func (l *ProfileList) Usable() []int {
	var out []int
	for i, p := range l.Profiles {
		if p.Usable {
			out = append(out, i)
		}
	}
	return out
}

// Default picks the profile pre-selected for the person (§3.1 step 4 as folded,
// RT-10). backsUp reports whether the archive backs up a profile folder (an input of
// one of its jobs lies inside it); source is the profile the archive's own Thunderbird
// source came from ("" when none). With several usable profiles, the first, in the
// order opens-by-default → Default=1 → listed order, that the archive does not back
// up; otherwise (one profile, or every one backed up) the source's profile, then the
// one Thunderbird opens, then Default=1, then — when there is only one usable — that
// one. ok is false when the person must choose (or none is usable).
func (l *ProfileList) Default(backsUp func(dir string) bool, source string) (pos int, ok bool) {
	usable := l.Usable()
	if len(usable) == 0 {
		return -1, false
	}
	order := func() []int {
		var out []int
		add := func(pred func(Profile) bool) {
			for _, i := range usable {
				if pred(l.Profiles[i]) && !contains(out, i) {
					out = append(out, i)
				}
			}
		}
		add(func(p Profile) bool { return p.OpensByDefault })
		add(func(p Profile) bool { return p.DefaultFlag })
		add(func(Profile) bool { return true })
		return out
	}()
	if len(usable) > 1 && backsUp != nil {
		for _, i := range order {
			if !backsUp(l.Profiles[i].Dir) {
				return i, true
			}
		}
	}
	if source != "" {
		cs := canonical(source)
		for _, i := range usable {
			if l.Profiles[i].Dir == cs {
				return i, true
			}
		}
	}
	for _, i := range usable {
		if l.Profiles[i].OpensByDefault {
			return i, true
		}
	}
	for _, i := range usable {
		if l.Profiles[i].DefaultFlag {
			return i, true
		}
	}
	if len(usable) == 1 {
		return usable[0], true
	}
	return -1, false
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// BacksUp reports whether any of inputs (an archive's job inputs) lies inside the
// profile folder dir, links resolved: the archive backs this profile up, so W-RECAPTURE
// leads the plan (§6.4 note). It is the same test on every platform (the Linux leg was
// new code, §2 note).
func BacksUp(inputs []string, dir string) bool {
	cdir := canonical(dir)
	for _, in := range inputs {
		if in == "" {
			continue
		}
		if util.Within(cdir, canonical(in)) {
			return true
		}
	}
	return false
}

// unixHomeBases are the profiles.ini folders under a home folder on macOS (goos
// "darwin") and on Linux and the other Unixes (design §3.1 step 1).
func unixHomeBases(home, goos string) []string {
	if goos == "darwin" {
		return []string{filepath.Join(home, "Library", "Thunderbird")}
	}
	return []string{
		filepath.Join(home, ".thunderbird"),
		filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
		filepath.Join(home, ".var", "app", "org.mozilla.Thunderbird", ".thunderbird"),
	}
}

// itoa is strconv.Itoa, kept short for the words.
func itoa(n int) string { return strconv.Itoa(n) }
