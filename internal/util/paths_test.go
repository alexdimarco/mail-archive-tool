package util

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-29, R4
// Containment tombstone: names derived from untrusted mail cannot become path
// separators, traversal tokens, or absolute paths that escape the output root.
func TestContainment(t *testing.T) {
	// Normalize to the host separator so the prefix check is valid on Windows
	// (filepath.Join yields \out\root there, not /out/root).
	root := filepath.Clean("/out/root")
	adversarial := []string{
		"../../etc/passwd", "..", ".", "/abs/path", `..\..\win`,
		"a/b/c", "....//....//", "con", "nul", "\x00\x01evil", "  ..  ",
	}
	for _, in := range adversarial {
		seg := SanitizeSegment(in)
		if strings.ContainsAny(seg, `/\`) {
			t.Errorf("SanitizeSegment(%q)=%q contains a path separator", in, seg)
		}
		if seg == ".." || seg == "." || seg == "" {
			t.Errorf("SanitizeSegment(%q)=%q is a traversal/empty token", in, seg)
		}
		joined := filepath.Clean(filepath.Join(root, seg))
		if joined != root && !strings.HasPrefix(joined, root+string(filepath.Separator)) {
			t.Errorf("segment from %q escaped the root: %q", in, joined)
		}

		fn := SanitizeFilename(in, 0)
		if strings.ContainsAny(fn, `/\`) {
			t.Errorf("SanitizeFilename(%q)=%q contains a path separator", in, fn)
		}
	}
}

// covers: MA-01
func TestSanitizeSegment(t *testing.T) {
	cases := map[string]string{
		`Inbox`:            "Inbox",
		`a/b\c:d*e?`:       "a b c d e",
		`  spaced  name  `: "spaced name",
		`trailing.`:        "trailing",
		``:                 "untitled",
		`   `:              "untitled",
		"con":              "_con", // reserved device name
		"NUL":              "_NUL",
	}
	for in, want := range cases {
		if got := SanitizeSegment(in); got != want {
			t.Errorf("SanitizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// covers: MA-02
func TestSanitizeSegmentLength(t *testing.T) {
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	if got := SanitizeSegment(long); len([]rune(got)) != 120 {
		t.Errorf("expected length 120, got %d", len([]rune(got)))
	}
}

// covers: MA-03
func TestSanitizeFilename(t *testing.T) {
	if got := SanitizeFilename("report:final.PDF", 0); got != "report final.PDF" {
		t.Errorf("got %q", got)
	}
	if got := SanitizeFilename("", 3); got != "attachment-3" {
		t.Errorf("empty name: got %q", got)
	}
	if got := SanitizeFilename("a/b/c.txt", 0); got != "a b c.txt" {
		t.Errorf("path-ish name: got %q", got)
	}
}

// covers: MA-04
func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Hello, World!":    "Hello-World",
		"   ":              "untitled",
		"Re: [ticket] #42": "Re-ticket-42",
		"already-a-slug":   "already-a-slug",
	}
	for in, want := range cases {
		if got := Slug(in, 60); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// covers: MA-05
func TestShortHash(t *testing.T) {
	a := ShortHash("key")
	b := ShortHash("key")
	c := ShortHash("other")
	if len(a) != 8 {
		t.Errorf("expected 8-char hash, got %d", len(a))
	}
	if a != b {
		t.Errorf("hash not deterministic: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("distinct inputs collided: %q", a)
	}
}
