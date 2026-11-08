// Package thunderbird helps prepare a Thunderbird IMAP account for a complete
// export: detecting IMAP (maybe not fully downloaded) accounts, enabling offline
// storage in the profile prefs, and watching a store while the user syncs.
//
// It never contacts the mail server and never touches credentials — it only
// reads/edits local profile files and observes file sizes.
package thunderbird

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/util"
)

// IsImapStore reports whether dir sits under an ImapMail/ folder, where Thunderbird keeps
// its IMAP accounts' stores (it downloads their messages over time, so the local copy may
// be incomplete). It is a path test only: it says nothing about the kind of account. Local
// Folders and POP3 accounts live under Mail/, but so do Thunderbird's native Exchange
// accounts, whose mail is downloaded over time too, and an IMAP account may be given a
// folder outside ImapMail/ (completeness design §2.2, the TC-5 fold).
func IsImapStore(dir string) bool {
	slash := filepath.ToSlash(dir)
	return strings.Contains(slash, "/ImapMail/")
}

// FindProfileDir ascends from a store path to the Thunderbird profile directory
// (the one containing prefs.js).
func FindProfileDir(storePath string) (string, bool) {
	d := storePath
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(d, "prefs.js")); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", false
}

// Account describes one mail account from prefs.js.
type Account struct {
	ServerKey string // e.g. "server3"
	Hostname  string
	Type      string // "imap", "pop3", …
	// OfflineDownload: prefs.js says "keep messages on this computer" is on. An account with
	// no offline_download line uses Thunderbird's default, which is on for a new IMAP account
	// (observed with Thunderbird 153.6.0esr in the Windows lab of 2026-10-31), yet reads false
	// here; EnableOffline then writes the setting out, which changes nothing Thunderbird does.
	OfflineDownload bool
	// OnByDefault: the account has no offline_download line of its own and prefs.js does not
	// turn the default off for every account (mail.server.default.offline_download), so
	// Thunderbird's default applies and it keeps the messages on this computer already.
	OnByDefault bool
}

var rePref = regexp.MustCompile(`^user_pref\("([^"]+)",\s*(.+)\);\s*$`)

// prefsMaxBytes bounds prefs.js, as tbrestore.PrefsMaxBytes bounds the same file: a larger
// one is refused, never read whole.
const prefsMaxBytes = 16 << 20

// parsePrefs reads prefs.js into a key→raw-value map. It is read as a planted file may be
// (C0c, completeness foldcheck M1): every backup of an IMAP store reads it, unattended
// when scheduled, so a link is refused, not followed; a pipe or device is neither waited
// on nor read; and a file over prefsMaxBytes is refused.
func parsePrefs(path string) (map[string]string, error) {
	data, err := util.ReadRegularFile(path, prefsMaxBytes)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := rePref.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return out, nil
}

func unquote(v string) string { return strings.Trim(v, `"`) }

// FindAccountForStore locates the account whose own store folder is storeDir (C0c,
// completeness design §4.1 step 1). With two accounts on one server
// (ImapMail/imap.gmail.com and ImapMail/imap.gmail.com-1) the hostname alone cannot tell
// them apart, so:
//
//  1. the store is matched against each server's directory-rel ([ProfD] is profileDir) and
//     directory, compared by whole path components (samePath);
//  2. only when no server matches that way, the hostname is compared with the store
//     folder's name, and only for a server whose own folder settings, if it has any, name
//     a folder of that name (a moved profile's stale directory still does; a server whose
//     folder is imap.gmail.com-1 does not own a leftover imap.gmail.com);
//  3. no match, or more than one, gives no account and an error: never a guess.
func FindAccountForStore(profileDir, storeDir string) (*Account, error) {
	prefs, err := parsePrefs(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return nil, err
	}
	store, err := filepath.Abs(storeDir)
	if err != nil {
		return nil, err
	}
	profile, err := filepath.Abs(profileDir)
	if err != nil {
		return nil, err
	}
	return matchAccount(prefs, profile, store, runtime.GOOS)
}

// matchAccount is FindAccountForStore's rule on parsed prefs, for goos.
func matchAccount(prefs map[string]string, profile, store, goos string) (*Account, error) {
	parts := components(store)
	name := ""
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	var byFolder, byHost []string
	for _, key := range serverKeys(prefs) {
		p := func(field string) string { return unquote(prefs["mail.server."+key+"."+field]) }
		var folders []string
		if rel, ok := strings.CutPrefix(p("directory-rel"), "[ProfD]"); ok {
			folders = append(folders, profile+"/"+rel)
		}
		if dir := p("directory"); dir != "" {
			folders = append(folders, dir)
		}
		ownsName := true
		for _, f := range folders {
			if samePath(f, store, goos) {
				byFolder = append(byFolder, key)
				break
			}
			if fp := components(f); len(fp) == 0 || !sameComponent(fp[len(fp)-1], name, goos) {
				ownsName = false
			}
		}
		if name != "" && ownsName && sameComponent(norm.NFC.String(p("hostname")), name, goos) {
			byHost = append(byHost, key)
		}
	}
	pick := byFolder
	if len(pick) == 0 {
		pick = byHost
	}
	switch len(pick) {
	case 1:
		key := pick[0]
		own, set := prefs["mail.server."+key+".offline_download"]
		return &Account{
			ServerKey:       key,
			Hostname:        unquote(prefs["mail.server."+key+".hostname"]),
			Type:            unquote(prefs["mail.server."+key+".type"]),
			OfflineDownload: own == "true",
			OnByDefault:     !set && prefs["mail.server.default.offline_download"] != "false",
		}, nil
	case 0:
		return nil, fmt.Errorf("no account in prefs.js matches the store folder %s", name)
	default:
		return nil, fmt.Errorf("more than one account in prefs.js (%s) could own the store folder %s, so MailArchive does not pick one", strings.Join(pick, ", "), name)
	}
}

// serverKeys are prefs' mail.server.serverN keys, sorted, so the match never depends on
// map order.
func serverKeys(prefs map[string]string) []string {
	seen := map[string]bool{}
	for k := range prefs {
		if rest, ok := strings.CutPrefix(k, "mail.server.server"); ok && rest != "" {
			seen["server"+strings.SplitN(rest, ".", 2)[0]] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// samePath reports whether a and b name the same folder, compared by whole path components
// after one normalising (completeness design §4.1 step 2's fold): Unicode NFC; "\" and "/"
// alike, so a prefs.js value with its backslashes doubled matches the native path; "." and
// ".." resolved; and case-folded on Windows and macOS only. An empty path matches nothing.
func samePath(a, b, goos string) bool {
	ca, cb := components(a), components(b)
	if len(ca) == 0 || len(ca) != len(cb) {
		return false
	}
	for i := range ca {
		if !sameComponent(ca[i], cb[i], goos) {
			return false
		}
	}
	return true
}

// components splits p as samePath compares it. A leading "" stands for the root, so an
// absolute path never equals a relative one.
func components(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(path.Clean(strings.ReplaceAll(norm.NFC.String(p), `\`, "/")), "/")
	out := parts[:0]
	for i, s := range parts {
		if s != "" || i == 0 {
			out = append(out, s)
		}
	}
	return out
}

// sameComponent compares one path component: case-blind on Windows and macOS, whose usual
// file systems are, and exact elsewhere.
func sameComponent(a, b, goos string) bool {
	if goos == "windows" || goos == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Running reports whether Thunderbird is currently running for this profile.
// prefs.js must not be edited while it runs.
//
// Detection is by liveness, not file existence: on Unix/macOS Mozilla writes a
// `lock` symlink → "<ip>:+<pid>" that a *cleanly* quit process removes, but a
// crash/kill leaves stale (and `.parentlock` is a regular file that ALWAYS
// persists). So we read the symlink's pid and check whether that process is
// alive. On Windows there is no such symlink; `parent.lock` is held open while
// running, so we probe it. A stale lock therefore reads as "not running".
func Running(profileDir string) bool {
	if target, err := os.Readlink(filepath.Join(profileDir, "lock")); err == nil {
		if pid := parseLockPID(target); pid > 0 {
			return pidAlive(pid)
		}
		return true // symlink present but unparseable target — be conservative
	}
	return windowsLockHeld(profileDir)
}

// parseLockPID extracts the pid from a Mozilla lock target such as
// "127.0.1.1:+2759488", "hostname:12345", or a bare "+2759488".
func parseLockPID(target string) int {
	s := target
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimPrefix(s, "+")
	pid, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return pid
}

// windowsLockHeld probes Windows' parent.lock, which is held open while
// Thunderbird runs (its existence persists, so we must try to open it).
func windowsLockHeld(profileDir string) bool {
	p := filepath.Join(profileDir, "parent.lock")
	if _, err := os.Stat(p); err != nil {
		return false
	}
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return true // locked by a running Thunderbird
	}
	f.Close()
	return false
}

// EnableOffline turns on "keep messages on this computer" (offline_download) for
// the account in prefs.js, after backing the file up. It returns whether a
// change was made and the backup path. Thunderbird must be closed.
//
// prefs.js is read as parsePrefs reads it, and neither write (the backup, then prefs.js)
// follows a link or replaces anything but a regular file (C0c, completeness foldcheck2
// L1): see writeOverRegular.
func EnableOffline(profileDir, serverKey string) (changed bool, backup string, err error) {
	prefsPath := filepath.Join(profileDir, "prefs.js")
	data, err := util.ReadRegularFile(prefsPath, prefsMaxBytes)
	if err != nil {
		return false, "", err
	}
	content := string(data)
	key := "mail.server." + serverKey + ".offline_download"

	trueLine := fmt.Sprintf(`user_pref("%s", true);`, key)
	falseLine := fmt.Sprintf(`user_pref("%s", false);`, key)

	switch {
	case strings.Contains(content, trueLine):
		return false, "", nil // already enabled
	case strings.Contains(content, falseLine):
		content = strings.Replace(content, falseLine, trueLine, 1)
	default:
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += trueLine + "\n"
	}

	backup = prefsPath + ".mailarchive-bak"
	if err := writeOverRegular(backup, data, true); err != nil {
		return false, "", fmt.Errorf("back up prefs.js: %w", err)
	}
	if err := writeOverRegular(prefsPath, []byte(content), false); err != nil {
		return false, backup, fmt.Errorf("write prefs.js: %w", err)
	}
	return true, backup, nil
}

// writeOverRegular writes data to path through the repo's atomic writer (a temporary file
// beside it, then a rename, which replaces the name and never writes through a link), once
// a fresh Lstat shows path is a regular file, or nothing at all when mayBeAbsent. A link, a
// pipe, a device or a folder there is refused and left as it is (the same rule
// util.ReadRegularFile applies). Something swapped in between the Lstat and the rename is
// replaced by the rename, never written through or waited on.
func writeOverRegular(path string, data []byte, mayBeAbsent bool) error {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && mayBeAbsent:
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode()):
		return &util.NotRegularError{Path: path, Mode: fi.Mode()}
	}
	return util.WriteFileAtomic0600Temp(path, data, ".mailarchive-prefs-*.tmp")
}

// MboxFiles returns the mbox files under a store directory (the things that grow
// as messages download), recursing into ".sbd" subfolder containers.
func MboxFiles(storeDir string) []string {
	var files []string
	var walk func(string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			full := filepath.Join(dir, name)
			if e.IsDir() {
				if strings.HasSuffix(name, ".sbd") {
					walk(full)
				}
				continue
			}
			switch strings.ToLower(filepath.Ext(name)) {
			case ".msf", ".dat":
				continue
			}
			files = append(files, full)
		}
	}
	walk(storeDir)
	return files
}

// HumanBytes formats a byte count as a short human-readable string.
func HumanBytes(n int64) string {
	const unit = 1024
	neg := ""
	if n < 0 {
		neg, n = "-", -n
	}
	if n < unit {
		return fmt.Sprintf("%s%d B", neg, n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %ciB", neg, float64(n)/float64(div), "KMGTPE"[exp])
}

// TotalSize sums the sizes of the given files (missing files count as 0).
func TotalSize(files []string) int64 {
	var total int64
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// StableWaiter tracks a store's total size and reports when it has stopped
// growing for the configured duration.
type StableWaiter struct {
	files      []string
	stableFor  time.Duration
	lastSize   int64
	lastChange time.Time
}

// NewStableWaiter starts watching storeDir, treating "no growth for stableFor"
// as sync-complete. now is the current time (injected for testability).
func NewStableWaiter(storeDir string, stableFor time.Duration, now time.Time) *StableWaiter {
	files := MboxFiles(storeDir)
	return &StableWaiter{
		files:      files,
		stableFor:  stableFor,
		lastSize:   TotalSize(files),
		lastChange: now,
	}
}

// Size returns the current total size.
func (w *StableWaiter) Size() int64 { return TotalSize(w.files) }

// Poll updates the tracker at time now and reports the current size and whether
// the store has been stable (no growth) for at least stableFor.
func (w *StableWaiter) Poll(now time.Time) (size int64, stable bool) {
	size = TotalSize(w.files)
	if size != w.lastSize {
		w.lastSize = size
		w.lastChange = now
	}
	return size, now.Sub(w.lastChange) >= w.stableFor
}
