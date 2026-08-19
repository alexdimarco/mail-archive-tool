// Package thunderbird helps prepare a Thunderbird IMAP account for a complete
// export: detecting on-demand (not-fully-downloaded) accounts, enabling offline
// storage in the profile prefs, and watching a store while the user syncs.
//
// It never contacts the mail server and never touches credentials — it only
// reads/edits local profile files and observes file sizes.
package thunderbird

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// IsImapStore reports whether dir is a Thunderbird IMAP account store (its
// messages are downloaded on demand, so the local copy may be incomplete).
// Local Folders (under Mail/) are excluded — they are always fully local.
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
	ServerKey       string // e.g. "server3"
	Hostname        string
	Type            string // "imap", "pop3", …
	OfflineDownload bool   // whether "keep messages on this computer" is on
}

var rePref = regexp.MustCompile(`^user_pref\("([^"]+)",\s*(.+)\);\s*$`)

// parsePrefs reads prefs.js into a key→raw-value map.
func parsePrefs(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
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

// FindAccountForStore locates the account whose store directory matches
// storeDir (by hostname or configured directory).
func FindAccountForStore(profileDir, storeDir string) (*Account, error) {
	prefs, err := parsePrefs(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return nil, err
	}
	want := filepath.Base(strings.TrimRight(storeDir, string(os.PathSeparator)))

	// Collect server keys.
	servers := map[string]bool{}
	for k := range prefs {
		if strings.HasPrefix(k, "mail.server.server") {
			parts := strings.SplitN(strings.TrimPrefix(k, "mail.server."), ".", 2)
			servers[parts[0]] = true
		}
	}

	for key := range servers {
		p := func(field string) string { return unquote(prefs["mail.server."+key+"."+field]) }
		host := p("hostname")
		dir := p("directory")
		rel := p("directory-rel")
		if host == want ||
			(dir != "" && filepath.Base(dir) == want) ||
			(rel != "" && strings.HasSuffix(rel, want)) {
			return &Account{
				ServerKey:       key,
				Hostname:        host,
				Type:            p("type"),
				OfflineDownload: prefs["mail.server."+key+".offline_download"] == "true",
			}, nil
		}
	}
	return nil, fmt.Errorf("no account in prefs.js matches store %q", want)
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
func EnableOffline(profileDir, serverKey string) (changed bool, backup string, err error) {
	prefsPath := filepath.Join(profileDir, "prefs.js")
	data, err := os.ReadFile(prefsPath)
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
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return false, "", fmt.Errorf("back up prefs.js: %w", err)
	}
	if err := os.WriteFile(prefsPath, []byte(content), 0o600); err != nil {
		return false, backup, fmt.Errorf("write prefs.js: %w", err)
	}
	return true, backup, nil
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
