package util

import (
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
func Within(base, target string) bool { return within(runtime.GOOS, base, target) }

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
