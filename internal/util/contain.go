package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The containment helpers every "is this path inside that one" question shares:
// extract's -dest overlap check (PC10) and the credential and settings checks of
// design-local-confidentiality P10 [CC16]. Both sides are resolved to their
// deepest existing ancestor first, so a file or folder that does not exist yet is
// still placed through its real, symlink-resolved parent.

// ResolveExisting returns p as an absolute, symlink-resolved path, resolving the
// deepest ancestor that exists (so a not-yet-created path still resolves a
// symlinked parent) and re-appending the non-existent tail.
func ResolveExisting(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	cur := abs
	var tail []string
	for {
		if resolved, rerr := filepath.EvalSymlinks(cur); rerr == nil {
			if final, ok := finalPath(resolved); ok {
				resolved = final // through a Windows junction too (B2)
			}
			parts := append([]string{resolved}, reverseStrings(tail)...)
			return filepath.Join(parts...)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// Within reports whether target is base itself or a path under base, compared
// component-wise on cleaned absolute paths (no string-prefix false positives
// like /a/bc under /a/b) and, on Windows and macOS, whose file systems are
// case-insensitive by default, regardless of case.
//
// A spelling that differs only in a way the file system ignores (Unicode
// normalization on APFS, case on a case-insensitive Linux mount) is still the
// same place, so a path that the string comparison puts outside is also compared
// by file identity: base is within-reach when it is, by os.SameFile, an ancestor
// of target's deepest existing ancestor (or its equal, for a base that does not
// exist yet, with the not-yet-created tails compared as names).
func Within(base, target string) bool {
	return within(runtime.GOOS, base, target) || identityWithin(runtime.GOOS, base, target)
}

// splitExisting splits an absolute path into its deepest existing ancestor (with
// its Stat) and the components below it that do not exist yet. ok is false when
// no ancestor can be stat'ed.
func splitExisting(p string) (anchor string, info os.FileInfo, tail []string, ok bool) {
	cur := filepath.Clean(p)
	for {
		if fi, err := os.Stat(cur); err == nil {
			return cur, fi, reverseStrings(tail), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil, nil, false
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func identityWithin(goos, base, target string) bool {
	_, iB, tB, ok := splitExisting(base)
	if !ok {
		return false
	}
	aT, iT, tT, ok := splitExisting(target)
	if !ok {
		return false
	}
	if len(tB) > 0 {
		// base does not exist: target is under it only if it shares base's existing
		// anchor and carries base's not-yet-created components as its own.
		if !os.SameFile(iB, iT) || len(tT) < len(tB) {
			return false
		}
		for i, c := range tB {
			if tT[i] != c && !((goos == "windows" || goos == "darwin") && strings.EqualFold(tT[i], c)) {
				return false
			}
		}
		return true
	}
	for x := aT; ; {
		if fi, err := os.Stat(x); err == nil && os.SameFile(iB, fi) {
			return true
		}
		parent := filepath.Dir(x)
		if parent == x {
			break
		}
		x = parent
	}
	return false
}

func within(goos, base, target string) bool {
	if goos == "windows" || goos == "darwin" {
		base, target = strings.ToLower(base), strings.ToLower(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Overlap reports whether a equals, sits inside, or contains b (both resolved to
// their deepest existing ancestor), and a phrase naming how for a refusal ("is
// the same directory as", "is inside", "contains").
func Overlap(a, b string) (bool, string) {
	ra := ResolveExisting(a)
	rb := ResolveExisting(b)
	inside, contains := Within(rb, ra), Within(ra, rb)
	switch {
	case inside && contains:
		return true, "is the same directory as"
	case inside:
		return true, "is inside"
	case contains:
		return true, "contains"
	default:
		return false, ""
	}
}

func reverseStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
