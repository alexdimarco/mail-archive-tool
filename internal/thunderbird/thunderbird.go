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

// FindProfileDir ascends from a store path to the Thunderbird profile directory (MA-1206). A
// folder is a profile by its own content, never by the names of the folders around it: it holds
// prefs.js as a regular file and a compatibility.ini that reads as Thunderbird's (hasProfileFiles).
// The nearest folder at or above the path that holds both decides: it is the path's profile when
// the path is that folder itself or sits under its own ImapMail/ or Mail/, and otherwise the path
// has no profile. So:
//
//   - a mail store is never taken for a profile. A Thunderbird mail folder may be named
//     "prefs.js" (the mbox file ImapMail/<host>/prefs.js, the C0c pass's INS-01) or even
//     "compatibility.ini", but an mbox file starts with "From ", never with [Compatibility]
//     (round 2's R2-03);
//   - a profile kept in a folder named Mail or ImapMail is found (R2-06);
//   - a copy of another profile kept under this profile's Mail/ that carries its own two files
//     is its own profile, so its store is never given this profile's account (R2-04).
func FindProfileDir(storePath string) (string, bool) {
	d := storePath
	var below []string // the path's components under d, nearest first
	for i := 0; i < 6; i++ {
		if hasProfileFiles(d) {
			if len(below) == 0 || isMailRoot(below[0]) {
				return d, true
			}
			return "", false // inside a profile, but not in its mail: no store of it
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

// hasProfileFiles reports whether d holds the two files Thunderbird leaves in every profile it
// has opened: prefs.js as a regular file (a link or a folder there is not one), and
// compatibility.ini, read through the bounded safe reader, as Thunderbird writes it.
func hasProfileFiles(d string) bool {
	if fi, err := os.Lstat(filepath.Join(d, "prefs.js")); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	_, ok := compatibilityLastVersion(d)
	return ok
}

// compatibilityLastVersion is the LastVersion= value of d's compatibility.ini, read through
// util.ReadRegularFile (64 KB: a link, a pipe or a larger file is refused). ok is true only for
// the file as Thunderbird writes it at every start: its first line that is not blank is
// [Compatibility], and that section holds a LastVersion= line (whatever its value). A mail folder
// named compatibility.ini starts with "From ", so it never reads as one.
func compatibilityLastVersion(d string) (string, bool) {
	data, err := util.ReadRegularFile(filepath.Join(d, "compatibility.ini"), 64<<10)
	if err != nil {
		return "", false
	}
	first, inSection := true, false
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\xef\xbb\xbf"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case first:
			if line != "[Compatibility]" {
				return "", false
			}
			first, inSection = false, true
		case strings.HasPrefix(line, "["):
			inSection = false
		case inSection:
			if k, v, found := strings.Cut(line, "="); found && strings.TrimSpace(k) == "LastVersion" {
				return strings.TrimSpace(v), true
			}
		}
	}
	return "", false
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

// prefsMaxBytes bounds prefs.js and user.js, as tbrestore.PrefsMaxBytes bounds the same
// files: a larger one is refused, never read whole.
const prefsMaxBytes = 16 << 20

// prefStmt is one setting statement Thunderbird applies: the call (user_pref, as Thunderbird
// writes it; pref or sticky_pref, as a hand-written user.js may), the setting's name, its value
// as written (a string with its quotes), and its line in the file (from 0).
type prefStmt struct {
	fn, key, value string
	line           int
}

// prefsText is a prefs.js or user.js read statement by statement, as Thunderbird's parser reads
// it (MA-1207): a line inside a /* */ comment, or after // or # on its line, never counts (the
// C0c pass's CON-03), a comment marker inside a quoted value is part of the value, and a line may
// hold several statements, each of which counts (round 2's R2-02).
type prefsText struct {
	lines []string
	stmts []prefStmt // in file order
	// unread are the lines (by number) whose text outside comments is not a run of whole
	// statements: MailArchive cannot be sure what Thunderbird reads there, so a setting they
	// name is one it cannot tell (see named).
	unread map[int]string
	// alone are the lines that hold exactly one user_pref statement and nothing else but spaces
	// and the line ending, with no comment on them: the only lines EnableOffline rewrites (R2-01).
	alone map[int]bool
	// openAtEnd: a /* comment is still open at the end, so a line added there would not count.
	openAtEnd bool
}

// parsePrefsText reads content (a prefs.js or user.js) as prefsText.
func parsePrefsText(content string) *prefsText {
	t := &prefsText{lines: strings.Split(content, "\n"), unread: map[int]string{}, alone: map[int]bool{}}
	open := false
	for i, raw := range t.lines {
		var code string
		code, open = codeOf(raw, open)
		stmts, ok := parseStatements(code)
		if !ok {
			t.unread[i] = code
			continue
		}
		for _, s := range stmts {
			s.line = i
			t.stmts = append(t.stmts, s)
		}
		// code == raw: nothing on the line was a comment (a comment open from an earlier line
		// takes at least its closing "*/" out of the code).
		t.alone[i] = len(stmts) == 1 && stmts[0].fn == "user_pref" && code == raw
	}
	t.openAtEnd = open
	return t
}

// named is the first line (from 0) that MailArchive cannot read as settings and that names key,
// or -1. Such a line may set key in a way it cannot tell.
func (t *prefsText) named(key string) int {
	first := -1
	for i, code := range t.unread {
		if strings.Contains(code, key) && (first < 0 || i < first) {
			first = i
		}
	}
	return first
}

// values are the settings as Thunderbird applies them: for a name set more than once, the last
// statement wins.
func (t *prefsText) values() map[string]string {
	out := map[string]string{}
	for _, s := range t.stmts {
		out[s.key] = s.value
	}
	return out
}

// parseStatements reads code (one line, its comments taken out) as a run of whole statements,
// each `fn("name", value);` with spaces allowed between the parts, as Thunderbird's parser
// accepts them: fn is user_pref, pref or sticky_pref; the name a quoted string; the value a
// quoted string, true, false or a whole number. ok is false when anything else is there.
func parseStatements(code string) (out []prefStmt, ok bool) {
	i := 0
	space := func() {
		for i < len(code) && strings.IndexByte(" \t\r\f\v", code[i]) >= 0 {
			i++
		}
	}
	expect := func(c byte) bool {
		space()
		if i < len(code) && code[i] == c {
			i++
			return true
		}
		return false
	}
	quoted := func() (string, bool) {
		space()
		if i >= len(code) || (code[i] != '"' && code[i] != '\'') {
			return "", false
		}
		q, start := code[i], i
		for i++; i < len(code); i++ {
			switch code[i] {
			case '\\':
				i++
			case q:
				i++
				return code[start:i], true
			}
		}
		return "", false
	}
	for {
		space()
		if i == len(code) {
			return out, true
		}
		start := i
		for i < len(code) && (code[i] == '_' || code[i] >= 'a' && code[i] <= 'z') {
			i++
		}
		fn := code[start:i]
		if fn != "user_pref" && fn != "pref" && fn != "sticky_pref" || !expect('(') {
			return nil, false
		}
		key, kok := quoted()
		if !kok || len(key) < 3 || !expect(',') {
			return nil, false
		}
		space()
		var value string
		if i < len(code) && (code[i] == '"' || code[i] == '\'') {
			v, vok := quoted()
			if !vok {
				return nil, false
			}
			value = v
		} else {
			vs := i
			for i < len(code) && (code[i] == '+' || code[i] == '-' || code[i] >= '0' && code[i] <= '9' || code[i] >= 'a' && code[i] <= 'z') {
				i++
			}
			value = code[vs:i]
			if !bareValue(value) {
				return nil, false
			}
		}
		if !expect(')') || !expect(';') {
			return nil, false
		}
		out = append(out, prefStmt{fn: fn, key: key[1 : len(key)-1], value: value})
	}
}

// bareValue reports whether v is an unquoted value Thunderbird's parser accepts: true, false, or
// a whole number with an optional sign.
func bareValue(v string) bool {
	if v == "true" || v == "false" {
		return true
	}
	digits := strings.TrimLeft(v, "+-")
	if digits == "" || len(v)-len(digits) > 1 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
// read; and a file over prefsMaxBytes is refused. It returns the file's bytes and the file read
// statement by statement.
func readPrefsFile(path string) ([]byte, *prefsText, error) {
	data, err := util.ReadRegularFile(path, prefsMaxBytes)
	if err != nil {
		return nil, nil, err
	}
	return data, parsePrefsText(string(data)), nil
}

// readUserJS is user.js read statement by statement, or an empty one when the profile has no
// user.js.
func readUserJS(profileDir string) (*prefsText, error) {
	_, t, err := readPrefsFile(filepath.Join(profileDir, "user.js"))
	if errors.Is(err, fs.ErrNotExist) {
		return parsePrefsText(""), nil
	}
	return t, err
}

// prefSettings are a profile's settings as Thunderbird applies them, and which names it cannot
// be sure of.
type prefSettings struct {
	vals        map[string]string
	prefs, user *prefsText // the two files as read; nil for settings given as a map
}

// unsure is where a line MailArchive cannot read as settings names key ("prefs.js", line from
// 1), or "" when none does, in prefs.js first, then user.js.
func (s prefSettings) unsure(key string) (string, int) {
	for _, f := range []struct {
		name string
		t    *prefsText
	}{{"prefs.js", s.prefs}, {"user.js", s.user}} {
		if f.t == nil {
			continue
		}
		if n := f.t.named(key); n >= 0 {
			return f.name, n + 1
		}
	}
	return "", 0
}

// readPrefs is the profile's settings as Thunderbird applies them: prefs.js, then user.js
// over it (as tbrestore.ReadPrefs reads them; the C0c pass's CON-06), plus user.js's own.
func readPrefs(profileDir string) (prefSettings, map[string]string, error) {
	_, pt, err := readPrefsFile(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return prefSettings{}, nil, err
	}
	ut, err := readUserJS(profileDir)
	if err != nil {
		return prefSettings{}, nil, err
	}
	vals, user := pt.values(), ut.values()
	for k, v := range user {
		vals[k] = v
	}
	return prefSettings{vals: vals, prefs: pt, user: ut}, user, nil
}

// unquote is a string value without its quotes.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return strings.Trim(v, `"`)
}

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
// The settings are read as Thunderbird applies them: prefs.js, then user.js over it. When a
// setting the match reads is named on a line MailArchive cannot read as settings, it cannot be
// sure what Thunderbird reads, so it gives no account and a HandEditedError (MA-1207).
func FindAccountForStore(profileDir, storeDir string) (*Account, error) {
	s, user, err := readPrefs(profileDir)
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
	acct, err := matchAccountIn(s, profile, store, runtime.GOOS)
	var h *HandEditedError
	if errors.As(err, &h) {
		h.Host, h.Place = StripControl(filepath.Base(store)), placeFor(profileDir)
	}
	if acct != nil {
		_, acct.OfflineInUserJS = user["mail.server."+acct.ServerKey+".offline_download"]
	}
	return acct, err
}

// matchAccount is FindAccountForStore's rule on parsed prefs, for goos.
func matchAccount(prefs map[string]string, profile, store, goos string) (*Account, error) {
	return matchAccountIn(prefSettings{vals: prefs}, profile, store, goos)
}

// matchError is why matchAccount gave no account; its words name prefs.js and the store folder.
type matchError struct {
	kind    NoAccountKind
	servers []string // the live servers that could own the store, for NoAccountAmbiguous
	msg     string
}

func (e *matchError) Error() string { return e.msg }

// matchAccountIn is matchAccount on s, which also says which names it cannot be sure of: the
// first name the match reads that a line MailArchive cannot read names gives a HandEditedError,
// whatever the match found.
func matchAccountIn(s prefSettings, profile, store, goos string) (*Account, error) {
	var unsureKey, unsureFile string
	var unsureLine int
	get := func(key string) (string, bool) {
		if unsureKey == "" {
			if f, n := s.unsure(key); f != "" {
				unsureKey, unsureFile, unsureLine = key, f, n
			}
		}
		v, ok := s.vals[key]
		return v, ok
	}
	val := func(key string) string { v, _ := get(key); return v }
	acct, err := matchWith(get, val, profile, store, goos)
	if unsureKey != "" {
		return nil, &HandEditedError{File: unsureFile, Line: unsureLine, Key: unsureKey, Why: whyUnread}
	}
	return acct, err
}

// matchWith is the match itself, reading every setting through get (or val, its value alone).
func matchWith(get func(string) (string, bool), val func(string) string, profile, store, goos string) (*Account, error) {
	parts := components(store)
	name := ""
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	servers := liveServers(val)
	if len(servers) == 0 {
		return nil, &matchError{kind: NoAccountNoList, msg: fmt.Sprintf("prefs.js lists no account (mail.accountmanager.accounts), so no account owns the store folder %s", name)}
	}
	var byFolder, onHost, byHost []string
	for _, key := range servers {
		p := func(field string) string { return unquote(val("mail.server." + key + "." + field)) }
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
		own, set := get("mail.server." + key + ".offline_download")
		return &Account{
			ServerKey:       key,
			Hostname:        unquote(val("mail.server." + key + ".hostname")),
			Type:            unquote(val("mail.server." + key + ".type")),
			OfflineDownload: own == "true",
			OnByDefault:     !set && val("mail.server.default.offline_download") != "false",
		}, nil
	case 0:
		return nil, &matchError{kind: NoAccountNone, msg: fmt.Sprintf("no account in prefs.js matches the store folder %s", name)}
	default:
		return nil, &matchError{kind: NoAccountAmbiguous, servers: pick, msg: fmt.Sprintf("more than one account in prefs.js (%s) could own the store folder %s, so MailArchive does not pick one", strings.Join(pick, ", "), name)}
	}
}

// liveServers are the server keys of the live accounts, sorted, so the match never depends
// on map order: each account listed in mail.accountmanager.accounts (its last line, as
// Thunderbird applies it) gives the server its mail.account.<id>.server names. No list gives
// none.
func liveServers(val func(string) string) []string {
	seen := map[string]bool{}
	for _, id := range strings.Split(unquote(val("mail.accountmanager.accounts")), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if key := unquote(val("mail.account." + id + ".server")); key != "" && !strings.Contains(key, ".") {
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
// account in prefs.js, after backing the file up. Thunderbird must be closed. It first works
// out the whole change with nothing written (CheckOffline's checks), then writes.
//
//   - It acts on the statement Thunderbird applies, the last one (a commented line never
//     counts), and when that is not true it changes every statement for the setting, so the
//     file says one thing (the C0c pass's CON-02, CON-03); with none it adds one. An account
//     already on is left as it is, with nothing written.
//   - It rewrites only a line that holds that one user_pref statement and nothing else but
//     spaces and the line ending (MA-1207, round 2's R2-01 and R2-02). When a statement for the
//     setting shares its line with anything else (another statement, a comment, a commented
//     copy), when a line it cannot read as settings names the setting, or when a comment left
//     open at the end would hide an added line, it changes nothing: a HandEditedError.
//   - The file keeps its own line ending: CRLF stays CRLF (CON-05).
//   - When user.js sets the setting, Thunderbird applies it over prefs.js at every start, so
//     nothing is changed: on is "already on", anything else a UserJSError (CON-06).
//   - The backup prefs.js.mailarchive-bak is written once and then kept: a later change
//     never overwrites it, so it keeps prefs.js from before MailArchive's first change
//     (CON-04). Something there that is not a regular file is refused before any write.
//
// prefs.js and user.js are read as readPrefsFile reads them, and neither write (the backup,
// then prefs.js) follows a link or replaces anything but a regular file (C0c, completeness
// foldcheck2 L1): see backupOnce and writeOverRegular.
func EnableOffline(profileDir, serverKey string) (OfflineChange, error) {
	plan, err := planOffline(profileDir, serverKey)
	if err != nil || plan == nil {
		return OfflineChange{}, err
	}
	c := OfflineChange{Backup: plan.backup}
	if c.BackupKept, err = backupOnce(c.Backup, plan.data); err != nil {
		return OfflineChange{}, fmt.Errorf("back up prefs.js: %w", err)
	}
	if err := writeOverRegular(plan.prefsPath, []byte(plan.content)); err != nil {
		return OfflineChange{}, fmt.Errorf("write prefs.js: %w", err)
	}
	c.Changed = true
	return c, nil
}

// CheckOffline runs every check EnableOffline makes before it writes, and writes nothing: its
// error is the refusal EnableOffline would give (a UserJSError, a HandEditedError, or prefs.js
// or its backup path refused). The command line runs it for every store before it changes any
// (MA-1208), and the wizard before it asks the person to quit Thunderbird.
func CheckOffline(profileDir, serverKey string) error {
	_, err := planOffline(profileDir, serverKey)
	return err
}

// offlinePlan is EnableOffline's change, worked out with nothing written.
type offlinePlan struct {
	prefsPath, backup string
	data              []byte // prefs.js as read: what the backup holds
	content           string // prefs.js to write
}

// planOffline works out EnableOffline's change. A nil plan and no error: nothing to change.
func planOffline(profileDir, serverKey string) (*offlinePlan, error) {
	prefsPath := filepath.Join(profileDir, "prefs.js")
	data, pt, err := readPrefsFile(prefsPath)
	if err != nil {
		return nil, err
	}
	key := "mail.server." + serverKey + ".offline_download"
	hand := func(file string, line int, why handWhy) error {
		return &HandEditedError{File: file, Line: line, Key: key, Why: why,
			Host: unquote(pt.values()["mail.server."+serverKey+".hostname"]), Place: placeFor(profileDir)}
	}
	ut, err := readUserJS(profileDir)
	if err != nil {
		return nil, err
	}
	if n := ut.named(key); n >= 0 {
		return nil, hand("user.js", n+1, whyUnread)
	}
	if v, set := ut.values()[key]; set {
		if v == "true" {
			return nil, nil // user.js turns it on already
		}
		return nil, &UserJSError{Path: filepath.Join(profileDir, "user.js"), Key: key,
			Host: unquote(pt.values()["mail.server."+serverKey+".hostname"])}
	}
	if n := pt.named(key); n >= 0 {
		return nil, hand("prefs.js", n+1, whyUnread)
	}

	content := string(data)
	eol := "\n"
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		eol = "\r\n"
	}
	trueLine := fmt.Sprintf(`user_pref("%s", true);`, key)
	var mine []prefStmt
	for _, s := range pt.stmts {
		if s.key == key {
			mine = append(mine, s)
		}
	}
	switch {
	case len(mine) > 0 && mine[len(mine)-1].value == "true":
		return nil, nil // already enabled
	case len(mine) > 0:
		lines := append([]string(nil), pt.lines...)
		for _, s := range mine {
			if !pt.alone[s.line] {
				return nil, hand("prefs.js", s.line+1, whyShared)
			}
			// The line is that one statement between spaces: its whole text is replaced.
			lines[s.line] = strings.Replace(lines[s.line], strings.TrimSpace(lines[s.line]), trueLine, 1)
		}
		content = strings.Join(lines, "\n")
	case pt.openAtEnd:
		return nil, hand("prefs.js", 0, whyOpenAtEnd)
	default:
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += eol
		}
		content += trueLine + eol
	}

	backup := prefsPath + ".mailarchive-bak"
	if fi, err := os.Lstat(backup); err == nil && (fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode())) {
		return nil, fmt.Errorf("back up prefs.js: %w", &util.NotRegularError{Path: backup, Mode: fi.Mode()})
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("back up prefs.js: %w", err)
	}
	return &offlinePlan{prefsPath: prefsPath, backup: backup, data: data, content: content}, nil
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

// The refusals EnableOffline, CheckOffline and AccountToEnable give, with their words for the
// command line and the wizard, are in offline_words.go.

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
