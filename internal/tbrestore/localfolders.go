package tbrestore

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/util"
)

// LocalFolders is the profile's Local Folders account and its folder (design §3.2).
type LocalFolders struct {
	// ServerKey is the account's server key ("server1").
	ServerKey string
	// Dir is the folder, links resolved once: what the plan shows and what is opened.
	Dir string
	// Identity pins Dir between the plan and the start (§3.2 step 4).
	Identity Identity
}

var reServerKey = regexp.MustCompile(`^mail\.server\.(server[0-9]+)\.`)

// FindLocalFolders finds the profile's Local Folders and checks it (design §3.2):
//  1. the server mail.accountmanager.localfoldersserver names, else the one server
//     of type "none"; none or several is W-NOLOCAL. MailArchive never adds the
//     setting itself (lab finding 1).
//  2. its folder: directory-rel when it begins with [ProfD] (the rest joined to the
//     profile as Mozilla does, ".." allowed), else directory (an absolute path).
//  3. the folder exists and is a folder after resolving links once; it neither lies
//     inside nor holds any other account's folder; no part of it below the profile is
//     ImapMail (W-OVERLAP).
//  4. its identity is read (W-NOIDENT on a volume that reports none).
func FindLocalFolders(profileDir string, p *Prefs) (LocalFolders, error) {
	servers := serverKeys(p)
	key, err := localServer(p, servers)
	if err != nil {
		return LocalFolders{}, err
	}
	// Any setting of the Local Folders account that could not be read refuses, even one
	// this lookup would not use: the value is unreadable, never guessed (§3.2 note).
	for k, why := range p.bad {
		if strings.HasPrefix(k, "mail.server."+key+".") {
			return LocalFolders{}, prefsRefusal(why)
		}
	}
	dir, err := serverDir(profileDir, p, key)
	if err != nil {
		return LocalFolders{}, err
	}
	if dir == "" {
		return LocalFolders{}, refuse("W-NOLOCAL", wNoLocal, nil)
	}
	resolved, err := resolveFolder(dir)
	if RefusalCode(err) != "" {
		return LocalFolders{}, err // W-JUNCTION (R6-14)
	}
	if err != nil {
		return LocalFolders{}, refuse("W-LOCALGONE", fill(wLocalGone, "PATH", dir), err)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.IsDir() {
		return LocalFolders{}, refuse("W-LOCALGONE", fill(wLocalGone, "PATH", resolved), err)
	}
	// Settled 2026-11-07 [R6-07]: the folder actually written is held to the profile's
	// own rule (§3.1 step 3) — this user's, neither group- nor world-writable — on macOS
	// and Linux, when prefs point it elsewhere as when it is inside the profile.
	if !profileModeOK(fi) {
		return LocalFolders{}, refuse("W-LOCALMODE", fill(wLocalMode, "PATH", resolved), nil)
	}
	// No part of the folder below the profile is ImapMail (thunderbird.IsImapStore's rule).
	cprof := canonical(profileDir)
	if rel, err := filepath.Rel(cprof, resolved); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if strings.EqualFold(part, "ImapMail") {
				return LocalFolders{}, refuse("W-OVERLAP", fill(wOverlap, "PATH", resolved, "WHAT", "Thunderbird's folder of IMAP accounts (ImapMail)"), nil)
			}
		}
	}
	for _, other := range servers {
		if other == key {
			continue
		}
		od, err := serverDir(profileDir, p, other)
		if err != nil {
			return LocalFolders{}, err
		}
		if od == "" {
			continue
		}
		if hit, _ := util.Overlap(resolved, od); hit {
			acct, _, _ := p.Get("mail.server." + other + ".hostname")
			if acct == "" {
				acct = other
			}
			return LocalFolders{}, refuse("W-OVERLAP", fill(wOverlap, "PATH", resolved, "WHAT", "the folder of the mail account "+shownName(acct)), nil)
		}
	}
	id, err := DirIdentity(resolved)
	if err != nil {
		if errors.Is(err, errNoIdentity) {
			return LocalFolders{}, refuse("W-NOIDENT", fill(wNoIdent, "PATH", resolved), err)
		}
		return LocalFolders{}, refuse("W-LOCALGONE", fill(wLocalGone, "PATH", resolved), err)
	}
	return LocalFolders{ServerKey: key, Dir: resolved, Identity: id}, nil
}

// CheckArchiveOverlap refuses a Local Folders that is, lies inside, or holds the
// archive folder (util.Overlap, as extract checks its destination; §3.2 step 3).
func CheckArchiveOverlap(lf LocalFolders, archiveDir string) error {
	if hit, _ := util.Overlap(lf.Dir, archiveDir); hit {
		return refuse("W-OVERLAP", fill(wOverlap, "PATH", lf.Dir, "WHAT", "the archive folder"), nil)
	}
	return nil
}

// serverKeys are every serverN that has a setting, sorted.
func serverKeys(p *Prefs) []string {
	set := map[string]bool{}
	for _, k := range p.Keys() {
		if m := reServerKey.FindStringSubmatch(k); m != nil {
			set[m[1]] = true
		}
	}
	for k := range p.bad {
		if m := reServerKey.FindStringSubmatch(k); m != nil {
			set[m[1]] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func localServer(p *Prefs, servers []string) (string, error) {
	named, ok, err := p.Get("mail.accountmanager.localfoldersserver")
	if err != nil {
		return "", prefsRefusal(err)
	}
	if ok && named != "" {
		for _, s := range servers {
			if s == named {
				return s, nil
			}
		}
		return "", refuse("W-NOLOCAL", wNoLocal, nil)
	}
	var none []string
	for _, s := range servers {
		t, _, err := p.Get("mail.server." + s + ".type")
		if err != nil {
			return "", prefsRefusal(err)
		}
		if t == "none" {
			none = append(none, s)
		}
	}
	if len(none) != 1 {
		return "", refuse("W-NOLOCAL", wNoLocal, nil)
	}
	return none[0], nil
}

// serverDir is a server's folder by §3.2 step 2: directory-rel when it begins with
// [ProfD], resolved as Mozilla resolves a relative path (the rest joined to the
// profile, ".." allowed: the checks after it are what keep it safe), else directory.
// "" when neither is set.
func serverDir(profileDir string, p *Prefs, key string) (string, error) {
	get := func(f string) (string, error) {
		v, _, err := p.Get("mail.server." + key + "." + f)
		if err != nil {
			return "", prefsRefusal(err)
		}
		return v, nil
	}
	rel, err := get("directory-rel")
	if err != nil {
		return "", err
	}
	if rest, ok := strings.CutPrefix(rel, "[ProfD]"); ok {
		return filepath.Join(canonical(profileDir), filepath.FromSlash(rest)), nil
	}
	abs, err := get("directory")
	if err != nil {
		return "", err
	}
	if abs != "" && filepath.IsAbs(abs) {
		return filepath.Clean(abs), nil
	}
	return "", nil
}
