// Package autoplace holds where MailArchive's auto-discovery looks for mail (the patterns it
// globs) and the one rule that says a listed input that is gone may be skipped by a run with
// auto-discovery on (docs/design-1.7.5-fixes.md §1.2, MA-1471). The run (app.DiscoverInputs),
// the Desktop's schedule rebuild and status all call it, so they cannot disagree. It imports only
// the standard library and internal/state, so internal/health can call it (health cannot import
// internal/app, whose own tests import health).
package autoplace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/state"
)

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
func GoneSkippable(path string) bool {
	return goneSkippable(path, patterns(), runtime.GOOS)
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
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false // (a): there (a dangling link included), or not known to be gone
	}
	p := filepath.Clean(path)
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
