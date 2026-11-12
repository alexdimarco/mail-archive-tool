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

// prefStmt is one user_pref statement: the setting's name, its value as written (a string with
// its quotes), and its line in the file (from 0).
type prefStmt struct {
	key, value string
	line       int
}

// prefsText is a prefs.js or user.js read line by line, by an allow-list (MA-1264, the C0c pass
// round 4's R4-01 and R4-02, which supersedes MA-1261's "refuse what it cannot parse"): the file
// reads with certainty only when every line is one of the shapes Thunderbird itself writes
// (Preferences.cpp's PreferencesWriter: its "//" header, then one `user_pref(NAME, VALUE);` per
// line, NAME and string VALUEs through StrEscape):
//
//   - an empty line (spaces only);
//   - a // comment, after leading spaces only;
//   - exactly `user_pref("NAME", VALUE);` between optional spaces, NAME a double-quoted string
//     whose only escapes are \\ and \" (MA-1269), and VALUE true, false, a whole number
//     Thunderbird can hold (32 bits, an optional minus) or a double-quoted string whose only
//     escapes are \\, \", \n and \r; and, for the settings MailArchive reads, VALUE of the type
//     Thunderbird uses for that setting (wrongType, MA-1270).
//
// The lines end all in LF or all in CRLF, with no other carriage return (CR) anywhere, no NUL
// byte and no byte-order mark. Inside a quoted NAME or VALUE any other byte stands for itself.
//
// Why each shape is certain, from libpref's parser (lib.rs): a // comment runs to the line's end
// (it ends at LF or CR, and the file holds no lone CR); in a quoted string only the quote, a
// backslash, LF, CR and NUL are special, so with LF, CR and NUL absent and every backslash one of
// the four escapes (each one character after the backslash, never the quote that would close the
// string), the string ends at the same quote for both readers. \n and \r are allowed because
// StrEscape writes them for a value holding a line break (a signature, say). Anything else
// anywhere (a /* */ or # comment, two statements on a line, a statement with a comment after it,
// single quotes, other spacing, a tab, \u or \x escapes, a pref( or sticky_pref( statement) is a
// file changed outside Thunderbird: uncertainLine says where, and nothing is read from it as on,
// nor changed.
type prefsText struct {
	// lines are the file split at LF, each with its CR when the file ends its lines in CRLF.
	lines []string
	stmts []prefStmt // in file order
	// uncertainLine is the first line (from 0) MailArchive cannot read with certainty, or -1;
	// uncertainWhat says what is there, for the command line's words ("holds …" or "has …").
	uncertainLine int
	uncertainWhat string
}

// The command line's naming of what is on a line MailArchive cannot read with certainty, each with
// its verb: "prefs.js line 3 holds a NUL (zero) byte, …".
const (
	whatNUL      = "holds a NUL (zero) byte, where Thunderbird stops reading the file"
	whatBOM      = "holds a byte-order mark (BOM) at the start of the file"
	whatLoneCR   = "holds a carriage return (CR) on its own, which Thunderbird reads as the end of a line"
	whatMixedEOL = "ends in a different way from the file's other lines (LF and CRLF mixed)"
	whatNotWhole = `holds text that is not a whole user_pref("name", value); statement (often a missing ; at its end)`
	whatSpanning = "holds a quoted value that goes on past the end of its line"
	// The fragments below speak of Thunderbird's settings format, never of what Thunderbird
	// "writes": the same fragment follows "user.js is a file people or add-ons write by hand"
	// (the C0c pass round 6's N3, settled in slice C3).
	whatNameBS  = `has a setting name with an escape code that is not part of Thunderbird's settings format (a backslash not followed by \ or ")`
	whatNotBool = "has a value for its setting that is not true or false, the only values Thunderbird uses for that setting"
	whatNotText = "has a value for its setting that is not a quoted text, the only kind of value Thunderbird uses for that setting"
	whatEscape  = `has a quoted value with an escape code that is not part of Thunderbird's settings format (a backslash not followed by \, ", n or r)`
	whatBigInt  = "holds a number too large for Thunderbird, which skips such a line"
	whatComment = "holds a comment that is not part of Thunderbird's settings format (its comments are lines that start with //)"
	whatMore    = "holds more after its user_pref statement (another statement or a comment)"
)

// parsePrefsText reads content (a prefs.js or user.js) as prefsText.
func parsePrefsText(content string) *prefsText {
	t := &prefsText{lines: strings.Split(content, "\n"), uncertainLine: -1}
	unsure := func(i int, what string) {
		if t.uncertainLine < 0 {
			t.uncertainLine, t.uncertainWhat = i, what
		}
	}
	if strings.HasPrefix(content, "\xef\xbb\xbf") {
		unsure(0, whatBOM)
	}
	// The first line ending decides: all LF, or all CRLF.
	crlf := false
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		crlf = true
	}
	for i, line := range t.lines {
		if crlf && i < len(t.lines)-1 {
			if !strings.HasSuffix(line, "\r") {
				unsure(i, whatMixedEOL)
				continue
			}
			line = line[:len(line)-1]
		}
		switch {
		case strings.IndexByte(line, 0) >= 0:
			unsure(i, whatNUL)
			continue
		case strings.IndexByte(line, '\r') >= 0:
			unsure(i, whatLoneCR)
			continue
		}
		s, ok, why := allowedLine(line)
		switch {
		case why != "":
			unsure(i, why)
		case ok:
			s.line = i
			t.stmts = append(t.stmts, s)
		}
	}
	return t
}

// allowedLine reads one line (no LF, CR or NUL in it) by the allow-list: ok when it is a
// statement (s), why "" with ok false when it is empty or a // comment, and otherwise why is what
// is there.
func allowedLine(line string) (s prefStmt, ok bool, why string) {
	code := strings.Trim(line, " ")
	switch {
	case code == "", strings.HasPrefix(code, "//"):
		return s, false, ""
	case strings.HasPrefix(code, "pref(") || strings.HasPrefix(code, "pref ("):
		return s, false, "holds a pref( statement, which Thunderbird does not apply in prefs.js or user.js"
	case strings.HasPrefix(code, "sticky_pref(") || strings.HasPrefix(code, "sticky_pref ("):
		return s, false, "holds a sticky_pref( statement, which Thunderbird does not apply in prefs.js or user.js"
	case strings.HasPrefix(code, "#") || strings.HasPrefix(code, "/*"):
		return s, false, whatComment
	}
	rest, found := strings.CutPrefix(code, `user_pref(`)
	if !found || !strings.HasPrefix(rest, `"`) {
		return s, false, whatNotWhole
	}
	// The name: a double-quoted string whose only escapes are \\ and \" (MA-1269, the C0c pass
	// round 5's R5-02). StrEscape writes a name as it writes a value (Preferences.cpp's
	// pref_savePrefs), so a Windows network printer's settings are saved as
	// print.printer_\\\\SERVER\\Printer.*; lib.rs reads a name with the same get_string_token as a
	// value, so each escape is one character after the backslash, never the closing quote, and the
	// name ends at the same quote for both readers. The name is kept decoded, as Thunderbird keeps
	// it. A \n or \r in a name (StrEscape would write them, but no name holds a line break), a \x
	// or \u, or a backslash before anything else stays refused.
	n, why := quotedLen(rest, `\"`, whatNameBS)
	switch {
	case why == whatSpanning:
		return s, false, whatNotWhole // the name does not end on its line
	case why != "":
		return s, false, why
	case n <= 2:
		return s, false, whatNotWhole // an empty name
	}
	s.key, rest = unquote(rest[:n]), rest[n:]
	if rest, found = strings.CutPrefix(rest, ", "); !found {
		return s, false, whatNotWhole
	}
	if strings.HasPrefix(rest, `"`) {
		n, why := quotedLen(rest, `\"nr`, whatEscape)
		if why != "" {
			return s, false, why
		}
		s.value, rest = rest[:n], rest[n:]
	} else {
		n := 0
		for n < len(rest) && (rest[n] == '-' || rest[n] >= '0' && rest[n] <= '9' || rest[n] >= 'a' && rest[n] <= 'z') {
			n++
		}
		s.value, rest = rest[:n], rest[n:]
		if why := bareValue(s.value); why != "" {
			return s, false, why
		}
	}
	switch {
	case rest == ");":
		if why := wrongType(s); why != "" {
			return s, false, why
		}
		return s, true, ""
	case strings.HasPrefix(rest, ");"):
		return s, false, whatMore
	default:
		return s, false, whatNotWhole
	}
}

// quotedLen is the length of the double-quoted string v starts with, its quotes included, when
// every backslash in it is followed by one of escapes (for a value the four StrEscape writes, \\,
// \", \n and \r; for a name \\ and \" only); why is bad when another escape is there, and
// whatSpanning when the string does not end in v.
func quotedLen(v, escapes, bad string) (int, string) {
	for i := 1; i < len(v); i++ {
		switch v[i] {
		case '"':
			return i + 1, ""
		case '\\':
			if i+1 >= len(v) {
				return 0, whatSpanning
			}
			if strings.IndexByte(escapes, v[i+1]) < 0 {
				return 0, bad
			}
			i++
		}
	}
	return 0, whatSpanning
}

// bareValue is "" when v is an unquoted value as Thunderbird writes one: true, false, or a whole
// number with an optional minus that a 32-bit setting holds (lib.rs drops a larger one and skips
// to the next ";"); otherwise what is there.
func bareValue(v string) string {
	if v == "true" || v == "false" {
		return ""
	}
	digits := strings.TrimPrefix(v, "-")
	if digits == "" {
		return whatNotWhole
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return whatNotWhole
		}
	}
	if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < -1<<31 || n > 1<<31-1 {
		return whatBigInt
	}
	return ""
}

// wrongType is "" unless s is one of the settings MailArchive reads and its value is not of the
// type Thunderbird uses for that setting (MA-1270, the C0c pass round 5's R5-01): then what is
// there. Thunderbird drops a user value whose type differs from the setting's built-in default
// (Preferences.cpp's Pref::SetUserValue, "Rejected attempt to change type of pref", copy
// .claude/mozsrc-r5, sha256 196973de…), so for such a setting the last statement does not win, and
// MailArchive does not model which one does: the file is one it cannot read with certainty.
// Thunderbird never writes another type for these settings, so the rule refuses no file it wrote.
// The settings, with where each type comes from:
//
//   - mail.server.default.offline_download: true or false. mailnews.js line 622,
//     pref("mail.server.default.offline_download",true) (comm-central tip, copy
//     .claude/mozsrc-r5b, sha256 41ebf6b1…).
//   - mail.server.<key>.offline_download (each server's own): true or false. It has no default of
//     its own in mailnews.js; Thunderbird reads it with mail.server.default.offline_download as its
//     fallback, the same setting by another name, so it takes that default's type (mailnews.js line
//     622 again; the fallback rule is the builder's reading, not checked in source this round).
//   - mail.server.default.is_gmail and mail.server.<key>.is_gmail: true or false (slice B1, MA-1235;
//     GmailForStore reads it). mailnews.js has pref("mail.server.default.is_gmail", false) (ESR 153
//     line 578, Release 159 line 582, comm-central tip line 586; copies .claude/tbsrc-esr153 and
//     .claude/mozsrc-r5b), and a server's own is read with that default as its fallback
//     (nsMsgIncomingServer::GetBoolValue, ESR 153 lines 449-459; IsGMailServer is a server bool
//     pref, nsImapIncomingServer.cpp ESR 153 line 302).
//   - mail.accountmanager.accounts: a quoted string. mailnews.js line 535,
//     pref("mail.accountmanager.accounts", "").
//   - mail.account.<id>.server, and mail.server.<key>.hostname, .type, .directory and
//     .directory-rel: a quoted string. None has a default in mailnews.js; each holds a name or a
//     path, which Thunderbird writes as a string (the builder's judgement, as the round-5 review
//     suggested; not checked in Thunderbird's account code this round).
//
// <key> is a name with no dot in it (as liveServers takes server keys), so a setting such as
// mail.server.server1.spamActionTargetAccount.type is not one of these; <id> is any name, as
// liveServers takes account ids from the list.
func wrongType(s prefStmt) string {
	isBool := s.value == "true" || s.value == "false"
	isText := strings.HasPrefix(s.value, `"`)
	switch k := s.key; {
	case k == "mail.accountmanager.accounts":
	case strings.HasPrefix(k, "mail.account.") && strings.HasSuffix(k, ".server") && len(k) > len("mail.account..server"):
	case strings.HasPrefix(k, "mail.server."):
		key, field, ok := strings.Cut(strings.TrimPrefix(k, "mail.server."), ".")
		switch {
		case !ok || key == "" || strings.Contains(field, "."):
			return ""
		case field == "offline_download", field == "is_gmail":
			if !isBool {
				return whatNotBool
			}
			return ""
		case field == "hostname", field == "type", field == "directory", field == "directory-rel":
		default:
			return ""
		}
	default:
		return ""
	}
	if !isText {
		return whatNotText
	}
	return ""
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

// prefSettings are a profile's settings as Thunderbird applies them, and the two files as read.
type prefSettings struct {
	vals        map[string]string
	prefs, user *prefsText // the two files as read; nil for settings given as a map
}

// uncertain is the first place MailArchive cannot read with certainty (MA-1261): its file
// ("prefs.js", then "user.js"), its line (from 1) and what is there, or "" when both files read.
func (s prefSettings) uncertain() (string, int, string) {
	return firstUncertain(s.prefs, s.user)
}

// firstUncertain is the first place in prefs.js (p), then user.js (u), that MailArchive cannot
// read with certainty, or "" when both read (a nil file reads).
func firstUncertain(p, u *prefsText) (file string, line int, what string) {
	for _, f := range []struct {
		name string
		t    *prefsText
	}{{"prefs.js", p}, {"user.js", u}} {
		if f.t != nil && f.t.uncertainLine >= 0 {
			return f.name, f.t.uncertainLine + 1, f.t.uncertainWhat
		}
	}
	return "", 0, ""
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

// unquote is a string value without its quotes, its escapes decoded as Thunderbird decodes them:
// \\, \", \n and \r, the only ones a file that reads with certainty holds (MA-1264). So a Windows
// path written C:\\Users is C:\Users, and on macOS and Linux a backslash in a value is the
// backslash it stands for (MA-1265).
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	} else {
		v = strings.Trim(v, `"`)
	}
	if strings.IndexByte(v, '\\') < 0 {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			if d, ok := map[byte]byte{'\\': '\\', '"': '"', 'n': '\n', 'r': '\r'}[v[i+1]]; ok {
				b.WriteByte(d)
				i++
				continue
			}
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// strEscape is s as Thunderbird's StrEscape writes it inside quotes (Preferences.cpp): \n, \r, \\
// and \" escaped, every other byte as it is.
func strEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(s)
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
//  2. only when no server matches that way, and only for a store directly in
//     <profile>/ImapMail/ (MA-1262), the hostname is compared with the store folder's name:
//     only when exactly one server has that hostname (INS-02), and only for a server whose
//     own folder settings, if it has any, name a folder of that name (a moved profile's
//     stale directory still does; a server whose folder is imap.gmail.com-1 does not own a
//     leftover imap.gmail.com);
//  3. no match, or more than one, gives no account and an error: never a guess.
//
// The settings are read as Thunderbird applies them: prefs.js, then user.js over it. When either
// file cannot be read with certainty (MA-1261: a line Thunderbird never writes, anywhere in it),
// it gives no account and a HandEditedError, so nothing is ever reported on from such a file.
func FindAccountForStore(profileDir, storeDir string) (*Account, error) {
	acct, _, err := findAccount(profileDir, storeDir)
	return acct, err
}

// findAccount is FindAccountForStore, also giving the settings it matched on, so a caller that
// reads another of the account's settings (GmailForStore) reads the files once, and reads them
// as the match did.
func findAccount(profileDir, storeDir string) (*Account, prefSettings, error) {
	s, user, err := readPrefs(profileDir)
	if err != nil {
		return nil, s, err
	}
	store, err := filepath.Abs(storeDir)
	if err != nil {
		return nil, s, err
	}
	profile, err := filepath.Abs(profileDir)
	if err != nil {
		return nil, s, err
	}
	acct, err := matchAccountIn(s, profile, store, runtime.GOOS)
	var h *HandEditedError
	if errors.As(err, &h) {
		h.Host, h.Place = StripControl(filepath.Base(store)), placeFor(profileDir)
	}
	if acct != nil {
		_, acct.OfflineInUserJS = user["mail.server."+acct.ServerKey+".offline_download"]
	}
	return acct, s, err
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

// matchAccountIn is matchAccount on s. When prefs.js or user.js cannot be read with certainty
// (MA-1261), it gives no account, whatever the match found, and a HandEditedError naming the
// first such place (and the offline setting of the account the match found, if any): it never
// reports an account as on, or as one to change, from a file it cannot be sure of.
func matchAccountIn(s prefSettings, profile, store, goos string) (*Account, error) {
	get := func(key string) (string, bool) { v, ok := s.vals[key]; return v, ok }
	val := func(key string) string { return s.vals[key] }
	acct, err := matchWith(get, val, profile, store, goos)
	if file, line, what := s.uncertain(); file != "" {
		key := ""
		if acct != nil {
			key = "mail.server." + acct.ServerKey + ".offline_download"
		}
		return nil, &HandEditedError{File: file, Line: line, Key: key, What: what}
	}
	return acct, err
}

// matchWith is the match itself, reading every setting through get (or val, its value alone).
func matchWith(get func(string) (string, bool), val func(string) string, profile, store, goos string) (*Account, error) {
	parts := components(store, goos)
	name, direct := "", false
	if len(parts) > 0 {
		name = parts[len(parts)-1]
		// The hostname tells an account only for a store directly in <profile>/ImapMail/, where
		// Thunderbird makes one (MA-1262, the C0c pass round 3's R3-05): a deeper store, such as a
		// copied profile's under Mail/ or ImapMail/, is matched by its own folder settings only.
		// "Directly" is by goos's path rules: on macOS and Linux a folder named
		// "ImapMail\imap.gmail.com" in the profile folder is not in ImapMail/ (MA-1265, R4-03).
		direct = samePath(strings.Join(parts[:len(parts)-1], "/"), profile+"/ImapMail", goos)
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
			if fp := components(f, goos); len(fp) == 0 || !sameComponent(fp[len(fp)-1], name, goos) {
				ownsName = false
			}
		}
		if direct && name != "" && sameComponent(norm.NFC.String(p("hostname")), name, goos) {
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
// after one normalising (completeness design §4.1 step 2's fold): Unicode NFC; on Windows "\"
// and "/" alike (a doubled "\\" is one separator), while on macOS and Linux "\" is part of a name
// (MA-1265, the C0c pass round 4's R4-03); "." and ".." resolved; and case-folded on Windows and
// macOS only. An empty path matches nothing.
func samePath(a, b, goos string) bool {
	ca, cb := components(a, goos), components(b, goos)
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

// components splits p as samePath compares it on goos: at "/", and on Windows at "\" too. A
// leading "" stands for the root, so an absolute path never equals a relative one.
func components(p, goos string) []string {
	if p == "" {
		return nil
	}
	p = norm.NFC.String(p)
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	parts := strings.Split(path.Clean(p), "/")
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
//   - It changes nothing, and reports nothing as already on, unless prefs.js and user.js both
//     read with certainty (MA-1261, by the allow-list of MA-1264): a line Thunderbird never
//     writes, anywhere in either file, is a HandEditedError.
//   - So it rewrites only a line that holds that one user_pref statement and nothing else but
//     spaces and the line ending (MA-1207, round 2's R2-01 and R2-02): the allow-list reads no
//     other line as a statement.
//   - The file keeps its own line ending: CRLF stays CRLF (CON-05).
//   - A failed write of the backup or of prefs.js is a WriteError.
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
		return OfflineChange{}, &WriteError{Profile: profileDir, Err: fmt.Errorf("back up prefs.js: %w", err)}
	}
	if err := writeOverRegular(plan.prefsPath, []byte(plan.content)); err != nil {
		return OfflineChange{}, &WriteError{Profile: profileDir, Err: fmt.Errorf("write prefs.js: %w", err)}
	}
	c.Changed = true
	return c, nil
}

// WriteError is EnableOffline's failed write of prefs.js or its backup, after every check passed
// (a read-only folder, a full disk, a file swapped in). Its Error is the raw error ("back up
// prefs.js: …" or "write prefs.js: …"); the command line words it (the friction final look's Q4).
type WriteError struct {
	Profile string
	Err     error
}

func (e *WriteError) Error() string { return e.Err.Error() }
func (e *WriteError) Unwrap() error { return e.Err }

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
	ut, err := readUserJS(profileDir)
	if err != nil {
		return nil, err
	}
	// Strict reading (MA-1261, MA-1264): nothing is read as on, and nothing changed, unless both
	// files read with certainty.
	if file, line, what := firstUncertain(pt, ut); file != "" {
		return nil, &HandEditedError{File: file, Line: line, Key: key, What: what,
			Host: unquote(pt.values()["mail.server."+serverKey+".hostname"]), Place: placeFor(profileDir)}
	}
	if v, set := ut.values()[key]; set {
		if v == "true" {
			return nil, nil // user.js turns it on already
		}
		return nil, &UserJSError{Path: filepath.Join(profileDir, "user.js"), Key: key,
			Host: unquote(pt.values()["mail.server."+serverKey+".hostname"])}
	}
	content := string(data)
	eol := "\n"
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		eol = "\r\n"
	}
	// The name is written as Thunderbird writes it (StrEscape), since names are read decoded
	// (MA-1269): a server key holding a quote or a backslash still gives a line that reads back as
	// this setting.
	trueLine := fmt.Sprintf(`user_pref("%s", true);`, strEscape(key))
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
			// The allow-list reads a statement only on a line that holds it alone, between
			// spaces (MA-1264): its text is replaced, the spaces and a CRLF's CR kept.
			lines[s.line] = strings.Replace(lines[s.line], strings.Trim(lines[s.line], " \r"), trueLine, 1)
		}
		content = strings.Join(lines, "\n")
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
