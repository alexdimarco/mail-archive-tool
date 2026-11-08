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

// FindProfileDir ascends from a store path to the Thunderbird profile directory. A folder
// is the profile only when it holds prefs.js as a regular file (a link or a folder there is
// not one, as the restore's and the pre-sync's profile checks read it) and the path sits
// under its own ImapMail/ or Mail/, or is the profile itself. So a mail store is never taken
// for a profile: a Thunderbird folder named "prefs.js" is the mbox file
// ImapMail/<host>/prefs.js, which the old walk, checking the store first, took for the
// profile's settings and wrote into (the C0c pass's INS-01, MA-1203).
func FindProfileDir(storePath string) (string, bool) {
	d := storePath
	var below []string // the path's components under d, nearest first
	for i := 0; i < 6; i++ {
		if isProfileFor(d, below) {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		below = append([]string{filepath.Base(d)}, below...)
		d = parent
	}
	return "", false
}

// isProfileFor reports whether d is the profile of the path whose components under d are
// below: d holds prefs.js as a regular file, and the path is under d's ImapMail/ or Mail/,
// or is d itself holding an ImapMail/ or Mail/ folder and not inside a store (its own name
// or its parent's is not ImapMail or Mail, and it is no ".sbd" subfolder container).
func isProfileFor(d string, below []string) bool {
	if fi, err := os.Lstat(filepath.Join(d, "prefs.js")); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if len(below) > 0 {
		return isMailRoot(below[0])
	}
	if isMailRoot(filepath.Base(d)) || isMailRoot(filepath.Base(filepath.Dir(d))) || strings.HasSuffix(d, ".sbd") {
		return false
	}
	for _, root := range []string{"ImapMail", "Mail"} {
		if fi, err := os.Lstat(filepath.Join(d, root)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// isMailRoot reports whether name is one of the profile's two mail folders, ImapMail or Mail,
// compared as the OS compares names.
func isMailRoot(name string) bool {
	return sameComponent(name, "ImapMail", runtime.GOOS) || sameComponent(name, "Mail", runtime.GOOS)
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
	// OfflineInUserJS: user.js sets the account's offline_download. Thunderbird applies user.js
	// over prefs.js at every start, so OfflineDownload is user.js's value, and EnableOffline
	// changes nothing (MA-1205).
	OfflineInUserJS bool
}

// rePref is one setting line as Thunderbird writes it (user_pref), or as a hand-written
// user.js may (pref, sticky_pref), once its comments are taken out.
var rePref = regexp.MustCompile(`^(?:user_|sticky_)?pref\("([^"]+)",\s*(.+)\);$`)

// prefsMaxBytes bounds prefs.js and user.js, as tbrestore.PrefsMaxBytes bounds the same
// files: a larger one is refused, never read whole.
const prefsMaxBytes = 16 << 20

// livePref is one setting line Thunderbird applies: its line number in the file (from 0),
// the setting's name and its value as written, and the line's text outside comments.
type livePref struct {
	line       int
	key, value string
	code       string
}

// livePrefs are the setting lines of lines (a prefs.js or user.js split on "\n") that
// Thunderbird applies, in file order. A line inside a /* */ comment, or after // or # on its
// line, never counts (the C0c pass's CON-03); a comment marker inside a quoted value is
// part of the value.
func livePrefs(lines []string) []livePref {
	var out []livePref
	open := false
	for i, raw := range lines {
		var code string
		code, open = codeOf(raw, open)
		code = strings.TrimSpace(code)
		if m := rePref.FindStringSubmatch(code); m != nil {
			out = append(out, livePref{line: i, key: m[1], value: strings.TrimSpace(m[2]), code: code})
		}
	}
	return out
}

// codeOf is line with its comments taken out, and whether a /* comment is still open at its
// end; open says one was open at its start.
func codeOf(line string, open bool) (string, bool) {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		next := byte(0)
		if i+1 < len(line) {
			next = line[i+1]
		}
		switch {
		case open:
			if c == '*' && next == '/' {
				open = false
				i++
			}
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && next != 0 {
				b.WriteByte(next)
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			b.WriteByte(c)
		case c == '#' || (c == '/' && next == '/'):
			return b.String(), false
		case c == '/' && next == '*':
			open = true
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), open
}

// readPrefsFile reads one settings file of the profile as a planted file may be (C0c,
// completeness foldcheck M1): every backup of an IMAP store reads it, unattended when
// scheduled, so a link is refused, not followed; a pipe or device is neither waited on nor
// read; and a file over prefsMaxBytes is refused. It returns the file's bytes and its live
// settings, the last line for a name winning, as Thunderbird applies them.
func readPrefsFile(path string) ([]byte, map[string]string, error) {
	data, err := util.ReadRegularFile(path, prefsMaxBytes)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	for _, p := range livePrefs(strings.Split(string(data), "\n")) {
		out[p.key] = p.value
	}
	return data, out, nil
}

// readUserJS is user.js's live settings, or none when the profile has no user.js.
func readUserJS(profileDir string) (map[string]string, error) {
	_, vals, err := readPrefsFile(filepath.Join(profileDir, "user.js"))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	return vals, err
}

// readPrefs is the profile's settings as Thunderbird applies them: prefs.js, then user.js
// over it (as tbrestore.ReadPrefs reads them; the C0c pass's CON-06), plus user.js's own.
func readPrefs(profileDir string) (prefs, user map[string]string, err error) {
	_, prefs, err = readPrefsFile(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return nil, nil, err
	}
	if user, err = readUserJS(profileDir); err != nil {
		return nil, nil, err
	}
	for k, v := range user {
		prefs[k] = v
	}
	return prefs, user, nil
}

func unquote(v string) string { return strings.Trim(v, `"`) }

// FindAccountForStore locates the account whose own store folder is storeDir (C0c,
// completeness design §4.1 step 1). With two accounts on one server
// (ImapMail/imap.gmail.com and ImapMail/imap.gmail.com-1) the hostname alone cannot tell
// them apart, so:
//
//  0. only a live account's server is a candidate: the accounts listed in
//     mail.accountmanager.accounts, each by its mail.account.<id>.server (a removed
//     account's leftover mail.server lines own nothing; the C0c pass's CON-01);
//  1. the store is matched against each server's directory-rel ([ProfD] is profileDir) and
//     directory, compared by whole path components (samePath);
//  2. only when no server matches that way, the hostname is compared with the store
//     folder's name: only when exactly one server has that hostname (INS-02), and only
//     for a server whose own folder settings, if it has any, name a folder of that name (a
//     moved profile's stale directory still does; a server whose folder is
//     imap.gmail.com-1 does not own a leftover imap.gmail.com);
//  3. no match, or more than one, gives no account and an error: never a guess.
//
// The settings are read as Thunderbird applies them: prefs.js, then user.js over it.
func FindAccountForStore(profileDir, storeDir string) (*Account, error) {
	prefs, user, err := readPrefs(profileDir)
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
	acct, err := matchAccount(prefs, profile, store, runtime.GOOS)
	if acct != nil {
		_, acct.OfflineInUserJS = user["mail.server."+acct.ServerKey+".offline_download"]
	}
	return acct, err
}

// matchAccount is FindAccountForStore's rule on parsed prefs, for goos.
func matchAccount(prefs map[string]string, profile, store, goos string) (*Account, error) {
	parts := components(store)
	name := ""
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	servers := liveServers(prefs)
	if len(servers) == 0 {
		return nil, fmt.Errorf("prefs.js lists no account (mail.accountmanager.accounts), so no account owns the store folder %s", name)
	}
	var byFolder, onHost, byHost []string
	for _, key := range servers {
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
		if name != "" && sameComponent(norm.NFC.String(p("hostname")), name, goos) {
			onHost = append(onHost, key)
			if ownsName {
				byHost = append(byHost, key)
			}
		}
	}
	pick := byFolder
	switch {
	case len(pick) > 0:
	case len(onHost) > 1:
		pick = onHost // the hostname tells nothing when two servers have it
	default:
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

// liveServers are the server keys of the live accounts, sorted, so the match never depends
// on map order: each account listed in mail.accountmanager.accounts (its last line, as
// Thunderbird applies it) gives the server its mail.account.<id>.server names. No list gives
// none.
func liveServers(prefs map[string]string) []string {
	seen := map[string]bool{}
	for _, id := range strings.Split(unquote(prefs["mail.accountmanager.accounts"]), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if key := unquote(prefs["mail.account."+id+".server"]); key != "" && !strings.Contains(key, ".") {
			seen[key] = true
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

// OfflineChange is what EnableOffline did.
type OfflineChange struct {
	// Changed: prefs.js now turns the account's offline download on, where it did not.
	Changed bool
	// Backup is prefs.js.mailarchive-bak's path when Changed: prefs.js as it was before
	// MailArchive first changed it.
	Backup string
	// BackupKept: the backup was already there (an earlier change made it) and was kept as it
	// was, never overwritten (the C0c pass's CON-04), so it holds an older prefs.js than the
	// one just changed.
	BackupKept bool
}

// EnableOffline turns on "keep messages on this computer" (offline_download) for the
// account in prefs.js, after backing the file up. Thunderbird must be closed.
//
//   - It acts on the line Thunderbird applies, the last live one (a commented line never
//     counts), and when that is not true it changes every live line for the setting, so the
//     file says one thing (the C0c pass's CON-02, CON-03); with none it adds one. An account
//     already on is left as it is, with nothing written.
//   - The file keeps its own line ending: CRLF stays CRLF (CON-05).
//   - When user.js sets the setting, Thunderbird applies it over prefs.js at every start, so
//     nothing is changed: on is "already on", anything else a UserJSError (CON-06).
//   - The backup prefs.js.mailarchive-bak is written once and then kept: a later change
//     never overwrites it, so it keeps prefs.js from before MailArchive's first change
//     (CON-04).
//
// prefs.js and user.js are read as readPrefsFile reads them, and neither write (the backup,
// then prefs.js) follows a link or replaces anything but a regular file (C0c, completeness
// foldcheck2 L1): see backupOnce and writeOverRegular.
func EnableOffline(profileDir, serverKey string) (OfflineChange, error) {
	prefsPath := filepath.Join(profileDir, "prefs.js")
	data, err := util.ReadRegularFile(prefsPath, prefsMaxBytes)
	if err != nil {
		return OfflineChange{}, err
	}
	key := "mail.server." + serverKey + ".offline_download"
	user, err := readUserJS(profileDir)
	if err != nil {
		return OfflineChange{}, err
	}
	if v, set := user[key]; set {
		if v == "true" {
			return OfflineChange{}, nil // user.js turns it on already
		}
		return OfflineChange{}, &UserJSError{Path: filepath.Join(profileDir, "user.js"), Key: key}
	}

	content := string(data)
	eol := "\n"
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		eol = "\r\n"
	}
	trueLine := fmt.Sprintf(`user_pref("%s", true);`, key)
	lines := strings.Split(content, "\n")
	var mine []livePref
	for _, p := range livePrefs(lines) {
		if p.key == key {
			mine = append(mine, p)
		}
	}
	switch {
	case len(mine) > 0 && mine[len(mine)-1].value == "true":
		return OfflineChange{}, nil // already enabled
	case len(mine) > 0:
		for _, p := range mine {
			if !strings.Contains(lines[p.line], p.code) {
				// A comment inside the setting's own text: no rewrite keeps the rest of the line.
				return OfflineChange{}, fmt.Errorf("prefs.js line %d sets %s with a comment inside it, which MailArchive does not rewrite, so it changed nothing", p.line+1, key)
			}
			lines[p.line] = strings.Replace(lines[p.line], p.code, trueLine, 1)
		}
		content = strings.Join(lines, "\n")
	default:
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += eol
		}
		content += trueLine + eol
	}

	c := OfflineChange{Backup: prefsPath + ".mailarchive-bak"}
	if c.BackupKept, err = backupOnce(c.Backup, data); err != nil {
		return OfflineChange{}, fmt.Errorf("back up prefs.js: %w", err)
	}
	if err := writeOverRegular(prefsPath, []byte(content)); err != nil {
		return OfflineChange{}, fmt.Errorf("write prefs.js: %w", err)
	}
	c.Changed = true
	return c, nil
}

// backupOnce writes data to path, a backup that is written once and then kept: when path is
// already a regular file it is left as it is and kept is true. Nothing there, it is written
// through the repo's atomic writer. A link, a pipe, a device or a folder there is refused and
// left as it is (the same rule util.ReadRegularFile applies).
func backupOnce(path string, data []byte) (kept bool, err error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, util.WriteFileAtomic0600Temp(path, data, ".mailarchive-prefs-*.tmp")
	case err != nil:
		return false, err
	case fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode()):
		return false, &util.NotRegularError{Path: path, Mode: fi.Mode()}
	}
	return true, nil
}

// writeOverRegular writes data to path through the repo's atomic writer (a temporary file
// beside it, then a rename, which replaces the name and never writes through a link), once
// a fresh Lstat shows path is a regular file. A link, a pipe, a device, a folder or nothing
// there is refused and left as it is (the same rule util.ReadRegularFile applies).
// Something swapped in between the Lstat and the rename is replaced by the rename, never
// written through or waited on.
func writeOverRegular(path string, data []byte) error {
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode()):
		return &util.NotRegularError{Path: path, Mode: fi.Mode()}
	}
	return util.WriteFileAtomic0600Temp(path, data, ".mailarchive-prefs-*.tmp")
}

// UserJSError is EnableOffline's refusal when the profile's user.js sets the account's
// offline setting to something other than on: Thunderbird applies user.js over prefs.js at
// every start, so a change to prefs.js would be undone, and nothing is changed (the C0c
// pass's CON-06, MA-1205). Its words name user.js and say what to do.
type UserJSError struct {
	Path string // the profile's user.js
	Key  string // the setting, mail.server.<key>.offline_download
}

func (e *UserJSError) Error() string {
	return fmt.Sprintf("%s sets this account's offline setting (%s), and Thunderbird applies user.js over prefs.js every time it starts, so MailArchive changed nothing. To keep this account's messages on this computer, close Thunderbird, then change that line in user.js to true or remove it", e.Path, e.Key)
}

// NoAccountError is what the person is told when MailArchive cannot tell which account a
// store belongs to, so it changes nothing (the C0c pass's OUT-01, MA-1204). Why is
// FindAccountForStore's reason, naming prefs.js; Label is the setting to turn on by hand,
// as OfflineSettingLabel names it.
type NoAccountError struct {
	Folder, Label string
	Why           error
}

func (e *NoAccountError) Error() string {
	return fmt.Sprintf("MailArchive could not tell which Thunderbird account the folder %s belongs to, so it changed nothing; in Thunderbird, open that account's settings and turn on %s yourself. Why: %v", e.Folder, e.Label, e.Why)
}

func (e *NoAccountError) Unwrap() error { return e.Why }

// AccountToEnable is the account whose offline download the wizard's Yes, prepare and the
// command line's -enable-offline would turn on for store. When no account can be told, the
// error is a NoAccountError; when user.js keeps the setting off, a UserJSError. Either way
// nothing has been, or may be, changed.
func AccountToEnable(profileDir, store string) (*Account, error) {
	acct, err := FindAccountForStore(profileDir, store)
	if err != nil {
		return nil, &NoAccountError{Folder: StripControl(filepath.Base(store)), Label: OfflineSettingLabel(profileDir), Why: err}
	}
	if acct.OfflineInUserJS && !acct.OfflineDownload {
		return nil, &UserJSError{Path: filepath.Join(profileDir, "user.js"), Key: "mail.server." + acct.ServerKey + ".offline_download"}
	}
	return acct, nil
}

// The account-level offline checkbox's label in Thunderbird, by version (completeness design
// §6, the M4 re-read and foldcheck2 L5: the label changes at 160).
const (
	offlineLabelBefore160 = `"Keep messages in all folders for this account on this computer"`
	offlineLabelFrom160   = `"Save copies of all folders and messages on this computer for offline use"`
)

// OfflineSettingLabel is the offline checkbox's label for the Thunderbird that last opened the
// profile (compatibility.ini's LastVersion): the older label below 160, the newer from 160,
// and both when the version cannot be read (§6 fold L5).
func OfflineSettingLabel(profileDir string) string {
	major, ok := lastVersionMajor(profileDir)
	switch {
	case !ok:
		return offlineLabelBefore160 + " (in Thunderbird 160 and later: " + offlineLabelFrom160 + ")"
	case major < 160:
		return offlineLabelBefore160
	default:
		return offlineLabelFrom160
	}
}

// lastVersionMajor is the major number of compatibility.ini's LastVersion
// ("153.6.0_20251212000000/…"), read as tbrestore.LastVersion reads it; ok is false when it
// cannot be read.
func lastVersionMajor(profileDir string) (int, bool) {
	data, err := util.ReadRegularFile(filepath.Join(profileDir, "compatibility.ini"), 64<<10)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || strings.TrimSpace(k) != "LastVersion" {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.IndexAny(v, "_/"); i >= 0 {
			v = v[:i]
		}
		head, _, _ := strings.Cut(v, ".")
		n, err := strconv.Atoi(head)
		return n, err == nil
	}
	return 0, false
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
