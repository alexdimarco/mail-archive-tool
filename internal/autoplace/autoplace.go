// Package autoplace holds where MailArchive's auto-discovery looks for mail (the patterns it
// globs) and the one rule that says a listed input that is gone may be skipped by a run with
// auto-discovery on (docs/design-1.7.5-fixes.md §1.2, MA-1471). The run (app.DiscoverInputs),
// the Desktop's schedule rebuild and status all call it, so they cannot disagree. It imports only
// the standard library and internal/state, so internal/health can call it (health cannot import
// internal/app, whose own tests import health). Amended by the 1.7.5 adversarial pass (INS-02,
// Q7): it also imports internal/thunderbird, a leaf of its own, to tell a moved account.
package autoplace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
	"mail-archive-tool/internal/thunderbird"
)

// NonLocal reports a path on another computer or one that is not a plain local path: a network
// (UNC) path, a device path, or one that is not absolute. A mail path named so in an archive's
// record, which anyone who can write the archive can change, is skipped before any file check, so
// drawing the Desktop's page, status and a run never reach out to another host (C-17, MA-869).
// The Desktop (its isRemotePath), status (health.GoneMailboxRepairable) and a run
// (app.resolveGoneSince) all ask this one rule (the 1.7.5 adversarial pass's OUT-01).
func NonLocal(p string) bool {
	p = strings.TrimSpace(p)
	return nonLocal(p, runtime.GOOS, filepath.VolumeName(p), filepath.IsAbs(p))
}

// nonLocal is NonLocal's rule as a pure function, fed the path's volume part and whether it is
// absolute (so it is tested on every system). Amended by the 1.7.5 re-verify (RV-01): on Windows a
// path is local only when its volume is a drive letter ("C:"), since Windows reads "\/host/…",
// "/\host\…" and "\??\UNC\host\…" as a network share too.
func nonLocal(p, goos, vol string, abs bool) bool {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") || !abs {
		return true
	}
	if goos == "windows" {
		return !(len(vol) == 2 && vol[1] == ':' && ('a' <= vol[0]|0x20 && vol[0]|0x20 <= 'z'))
	}
	return false
}

// InPlace reports, with no look on disk, whether path lies where auto-discovery looks (rule (b)):
// the Repair helper asks it of every input a record lists before any file check (RV-01).
func InPlace(path string) bool {
	p := filepath.Clean(path)
	for _, pat := range patterns() {
		if match(filepath.Clean(pat), p, runtime.GOOS) {
			return true
		}
	}
	return false
}

// watch, when set, is called with each path before this package looks at it on disk (and before
// health's look through Stat): a test sees every path looked at, and can hold a look to stand for
// stalled storage (MA-869's tombstone for OUT-01). Unset outside tests.
var watch atomic.Pointer[func(path string)]

// WatchLooksForTest calls f with each path before it is looked at on disk by GoneSkippable,
// GoneSourceID or Stat, until restore is called. f may block, to stand for stalled storage.
func WatchLooksForTest(f func(path string)) (restore func()) {
	prev := watch.Swap(&f)
	return func() { watch.Store(prev) }
}

// look tells the watch, if any, that p is about to be looked at.
func look(p string) {
	if f := watch.Load(); f != nil {
		(*f)(p)
	}
}

// Stat is os.Stat of a mail path, seen by the same watch as the rule's own looks: health's check
// that a listed input is there goes through it.
func Stat(p string) (fs.FileInfo, error) {
	look(p)
	return os.Stat(p)
}

// OutlookGlobs are the patterns auto-discovery globs for Outlook's data files.
func OutlookGlobs() []string {
	var globs []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.ost"))
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.pst"))
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		globs = append(globs, filepath.Join(up, "Documents", "Outlook Files", "*.pst"))
	}
	return globs
}

// ThunderbirdGlobs are the patterns auto-discovery globs for Thunderbird's account folders: each
// account under a profile's ImapMail/ and Mail/ (Local Folders included) is a store.
func ThunderbirdGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(home, ".thunderbird"),
		filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
		filepath.Join(home, ".mozilla-thunderbird"),
		filepath.Join(home, "Library", "Thunderbird", "Profiles"),
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		roots = append(roots, filepath.Join(ad, "Thunderbird", "Profiles"))
	}
	var globs []string
	for _, r := range roots {
		globs = append(globs, filepath.Join(r, "*", "ImapMail", "*"))
		globs = append(globs, filepath.Join(r, "*", "Mail", "*"))
	}
	return globs
}

// EvolutionGlobs are the default Evolution store locations: the local Maildir++ store and each
// IMAP account's disk cache (native and Flatpak paths).
func EvolutionGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "evolution", "mail", "local"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "data", "evolution", "mail", "local"),
		filepath.Join(home, ".cache", "evolution", "mail", "*"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "cache", "evolution", "mail", "*"),
	}
}

// The mail programs whose places auto-discovery looks in, as ProgramOf names them.
const (
	Outlook     = "Outlook"
	Thunderbird = "Thunderbird"
	Evolution   = "Evolution"
)

// GoneSkippable reports whether a run with auto-discovery on may skip the listed input path,
// which it did not find, instead of refusing (§1.2 rules (a) to (c)):
//
//   - (a) it is gone, not unreachable: os.Lstat says it does not exist, so a link whose target
//     is gone (a dangling link) is not skipped;
//   - (b) it lies where auto-discovery looks: its path, cleaned, matches one of the patterns
//     auto-discovery globs, cleaned too, by filepath.Match; both sides lower-cased on Windows
//     only (a macOS or Linux volume can be case-sensitive, where a glob never finds another
//     spelling);
//   - (c) its parent folder is still there, as a real folder, in the same place: os.Stat says it
//     is a folder, and the parent with its links resolved matches the pattern's folder part
//     rebuilt on the pattern's root (its leading part with no wildcard) resolved the same way.
//     A link at or above the root resolves alike on both sides; one below it (a profile folder
//     that is a link) takes the parent outside the place.
//
// Whether a mailbox remains after the skips (rule (d)) is the caller's to say.
//
// Added by the 1.7.5 adversarial pass (INS-02; the owner's ruling of 2026-11-20, Q7: "Stop the
// backup (Recommended)"): a Thunderbird account moved with its Local Directory setting, not
// removed (MovedTo), is never skipped, so the run stops as 1.7.4 did.
func GoneSkippable(path string) bool { return GoneSkippableAs(path, "") }

// GoneSkippableAs is GoneSkippable with the account name the archive's previous record gave the
// input's folder ("" when none: RecordedName), which decides a move first (RV2-01).
func GoneSkippableAs(path, recordedName string) bool {
	if !goneSkippable(path, patterns(), runtime.GOOS) {
		return false
	}
	_, moved := MovedToAs(path, recordedName)
	return !moved
}

// MovedTo is MovedToAs with no recorded name (the server-name rule alone).
func MovedTo(path string) (string, bool) { return MovedToAs(path, "") }

// MovedToAs reports, for a gone input in a Thunderbird profile, the folder its account was moved to
// (thunderbird.MovedTo; MA-1481): by the account the previous record named for the folder when it
// names one (recordedName), else by the folder's server name. False for an input in no profile, an
// account removed, and one whose folder setting still names the input (deleted by hand, Q4).
func MovedToAs(path, recordedName string) (string, bool) {
	look(path)
	profile, ok := thunderbird.FindProfileDir(path)
	if !ok {
		return "", false
	}
	return thunderbird.MovedTo(profile, path, recordedName, state.CleanFolderName)
}

// StoreID is a Thunderbird store's id in a run's record (state.TBAccount.StoreID): a hash of the
// profile and the store's path, compared as the OS compares names; computable for a folder that is
// gone. The completeness check writes it (app) and RecordedName reads it, by this one function.
func StoreID(profile, store string) string {
	norm := func(p string) string {
		if p == "" {
			return ""
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return tbcomplete.FolderIdentity(filepath.ToSlash(filepath.Clean(p)))
	}
	sum := sha256.Sum256([]byte("tbstore\x00" + norm(profile) + "\x00" + norm(store)))
	return hex.EncodeToString(sum[:16])
}

// RecordedName is the account name the plaintext record prev gave the store at path: its Thunderbird
// entry with the same StoreID, else its gone-mailbox warning for the same input (GoneSourceID; a path
// not plain local or outside the places is passed over first, never looked at). "" for none, and for
// an encrypted record (it keeps no names). The run, the command line and the Repair helper all ask it,
// so they decide a move alike (RV2-01).
func RecordedName(prev state.LastRun, path string) string {
	if prev.Encrypted {
		return ""
	}
	// Its completeness entries' names, else the names a record whose run had none of its own carried
	// (the 1.7.5 re-verify, round 3's RV3-01: a stopped backup's record keeps them).
	if len(prev.Thunderbird) > 0 || len(prev.ThunderbirdNames) > 0 {
		if profile, ok := thunderbird.FindProfileDir(path); ok {
			if n := state.TBNameOf(prev, StoreID(profile, path)); n != "" {
				return n
			}
		}
	}
	read := 0
	for _, w := range prev.Warnings {
		if w.Code != state.MailboxGoneCode {
			continue
		}
		if read++; read > state.MaxGoneWarnings {
			break
		}
		if w.Name != "" && w.Path != "" && !NonLocal(w.Path) && InPlace(w.Path) && GoneSourceID(w.Path) == GoneSourceID(path) {
			return w.Name
		}
	}
	return ""
}

// PstMovedText is the remedy for an Outlook data file (.pst) moved away, in status and the
// summaries alike (the 1.7.5 re-verify's RF-05 words; they were "If you moved this file, put it back
// in FOLDER, or click Make a new archive… in MailArchive Desktop and choose it in its new place.").
func PstMovedText(path string) string {
	return "If you moved this file, put it back in " + filepath.Dir(path) + " and the next backup finds it again. To back it up from its new place instead, click Make a new archive… in MailArchive Desktop and choose the file there: that makes a separate archive."
}

// MovedText is the words of a moved account's refusal (MA-1481): the old folder is gone, where
// Thunderbird now keeps the account's mail, and the two ways to back that mail up. It names folders
// only, never the account's name, since a refusal reaches log files (MA-1288).
func MovedText(path, movedTo string) string {
	// The 1.7.5 re-verify's RF-03 words (they were: "…Thunderbird still lists its account, now kept in
	// NEW (its Local Directory setting was changed), so this backup stopped instead of skipping it. To
	// back that mail up again, set the account's Local Directory back to its folder in the Thunderbird
	// profile, or back up NEW as an archive of its own (Make a new archive…, then Thunderbird / mbox
	// mail folder).").
	// The re-verify, round 2's RF2-01: said when the archive's record named the folder's account and
	// that account now keeps its mail elsewhere. Its name stays out: a refusal reaches log files
	// (MA-1288). Local Folders keeps its setting on its own page.
	return fmt.Sprintf("input %s does not exist, so this backup stopped: Thunderbird still has the account that used it, but now keeps its mail in %s (its Local Directory setting). To back that mail up again, either set the account's Local Directory back to %s (in Thunderbird: Account Settings, then Server Settings, or Local Folders' own page); or back up %s as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder) and stop this archive looking for %s (Make a new archive…, then All of them and this same archive folder).", path, movedTo, path, movedTo, path)
}

// MovedSameServerText is the refusal's words when only the server-name rule found the move (the
// record named no account): the re-verify, round 2's RF2-01 words as the reviewer wrote them.
func MovedSameServerText(path, movedTo string) string {
	// The re-verify, round 3's RF3-03: "or Local Folders", which has no mail server (it read "a
	// Thunderbird account on the same mail server now keeps its mail in NEW …").
	return fmt.Sprintf("input %s does not exist, so this backup stopped: a Thunderbird account on the same mail server, or Local Folders, now keeps its mail in %s (its Local Directory setting). If you moved that account there, set its Local Directory back to %s (in Thunderbird: Account Settings, then Server Settings, or Local Folders' own page), or back up %s as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder). If you removed the account that used %s, or you now back up %s on its own, stop this archive looking for %s (Make a new archive…, then All of them and this same archive folder).", path, movedTo, path, movedTo, path, movedTo, path)
}

// NoLongerUsedText is the words of the stop of an archive of one listed Thunderbird folder, with
// auto-discovery off, whose account now keeps its mail in another folder (docs/design-per-mailbox-
// archives.md §11 "When Thunderbird stops using an archive's folder", C2, and revision 2.1's F-08 for
// the old folder gone; MA-1504). It names folders only, never the account's name, since a refusal
// reaches log files (MA-1288). The design's draft words, for the friction review.
func NoLongerUsedText(old, moved string) string {
	return fmt.Sprintf("Thunderbird no longer uses %s: the account that used it now keeps its mail in %s, so this backup stopped. The mail saved so far stays in this archive. To back up %s, click Make a new archive… and back that account up again (choose it in the list, or choose One archive for each mailbox and the same folder as before); then click Pause (make read-only) on this archive. If Thunderbird should use %s again, set the account's Local Directory back to %s (in Thunderbird: Account Settings, then Server Settings).", old, moved, moved, old, old)
}

// NoLiveOwner reports whether path is a folder of a Thunderbird profile whose settings say, for
// certain, that no live account uses it (thunderbird.NoLiveOwner; C2's second condition, F-01).
func NoLiveOwner(path string) bool {
	look(path)
	profile, ok := thunderbird.FindProfileDir(path)
	if !ok {
		return false
	}
	return thunderbird.NoLiveOwner(profile, path)
}

// MovedHowAs is MovedToAs, also saying whether the recorded name decided (for the words).
func MovedHowAs(path, recordedName string) (string, bool, bool) {
	look(path)
	profile, ok := thunderbird.FindProfileDir(path)
	if !ok {
		return "", false, false
	}
	return thunderbird.MovedHow(profile, path, recordedName, state.CleanFolderName)
}

// ProgramOf names the mail program whose place path lies in, by the patterns auto-discovery
// globs (rule (b), whether or not path exists); "" for a path in none of them.
func ProgramOf(path string) string {
	p := filepath.Clean(path)
	for _, g := range []struct {
		name  string
		globs []string
	}{{Outlook, OutlookGlobs()}, {Thunderbird, ThunderbirdGlobs()}, {Evolution, EvolutionGlobs()}} {
		for _, pat := range g.globs {
			if match(filepath.Clean(pat), p, runtime.GOOS) {
				return g.name
			}
		}
	}
	return ""
}

// GoneSourceID is the store id of a gone input (§1.3): state.PathSourceID of its parent folder
// with links resolved, joined with the input's own last name, so a link above it resolves as it
// did when the store was recorded (state.PathSourceID of a gone path cannot resolve links). A
// parent that cannot be resolved gives state.PathSourceID of the path itself.
func GoneSourceID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	look(filepath.Dir(abs))
	if parent, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return state.PathSourceID(filepath.Join(parent, filepath.Base(abs)))
	}
	return state.PathSourceID(abs)
}

// GoneText is the words of a gone input's refusal, as the run says them
// (app.sourceUnreachableError): whatever compares a record's error with an input's refusal
// compares this one text.
func GoneText(path string) string {
	return fmt.Sprintf("input %s does not exist (check the path, or that the drive holding it is mounted)", path)
}

// patterns is every pattern auto-discovery globs, Outlook's first.
func patterns() []string {
	all := OutlookGlobs()
	all = append(all, ThunderbirdGlobs()...)
	return append(all, EvolutionGlobs()...)
}

func goneSkippable(path string, pats []string, goos string) bool {
	if path == "" {
		return false
	}
	look(path)
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false // (a): there (a dangling link included), or not known to be gone
	}
	p := filepath.Clean(path)
	// (a) again on the cleaned path: a trailing separator, "/." or "link/.." make the system follow
	// the last link, so a dangling link spelled so reads as gone (the 1.7.5 adversarial pass's
	// INS-03). Skipped only when both spellings say it does not exist.
	if p != path {
		look(p)
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			return false
		}
	}
	for _, pat := range pats {
		pat = filepath.Clean(pat)
		if match(pat, p, goos) && parentInPlace(p, pat, goos) {
			return true
		}
	}
	return false
}

// parentInPlace is rule (c) for the gone input p, which matches the pattern pat: its parent is a
// folder, and resolved it matches pat's folder part rebuilt on pat's root resolved.
func parentInPlace(p, pat, goos string) bool {
	parent := filepath.Dir(p)
	look(parent)
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return false
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return false
	}
	root, rest := splitRoot(filepath.Dir(pat), goos)
	if root == "" {
		return false
	}
	look(root)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return match(filepath.Join(resolvedRoot, rest), resolved, goos)
}

// splitRoot splits the folder pattern folderPat at its root: the leading part with no wildcard,
// and the rest (which starts with a wildcard element, or is "" for a pattern with none).
func splitRoot(folderPat, goos string) (root, rest string) {
	magic := `*?[`
	if goos != "windows" {
		magic += `\`
	}
	i := strings.IndexAny(folderPat, magic)
	if i < 0 {
		return folderPat, ""
	}
	j := strings.LastIndexByte(folderPat[:i], filepath.Separator)
	if j <= 0 {
		return "", "" // no folder with no wildcard to resolve: never a pattern auto-discovery globs
	}
	return folderPat[:j], folderPat[j+1:]
}

// match is filepath.Match with both sides lower-cased on Windows only; a pattern that does not
// parse matches nothing.
func match(pat, p, goos string) bool {
	if goos == "windows" {
		pat, p = strings.ToLower(pat), strings.ToLower(p)
	}
	ok, err := filepath.Match(pat, p)
	return err == nil && ok
}
