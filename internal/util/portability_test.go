package util

import (
	"strings"
	"testing"
)

// covers: MA-01, MA-29, R4
// Windows reserved device names are reserved with ANY extension (NUL.txt,
// COM1.log, aux.config.json are all uncreatable), so the check applies to the
// part before the first dot; the "_" prefix neutralizes them.
func TestReservedNamesWithExtension(t *testing.T) {
	cases := map[string]string{
		"NUL.txt":         "_NUL.txt",
		"com1.log":        "_com1.log",
		"aux.config.json": "_aux.config.json",
		"PRN":             "_PRN",
		"console.txt":     "console.txt", // not reserved: longer than the device name
	}
	for in, want := range cases {
		if got := SanitizeSegment(in); got != want {
			t.Errorf("SanitizeSegment(%q) = %q, want %q", in, got, want)
		}
		if got := SanitizeFilename(in, 0); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// covers: MA-03, R4
// A trailing dot or space survives the extension path of SanitizeFilename
// ("report." has extension "."), which Windows silently drops or refuses; the
// assembled name is re-trimmed, and a name that trims to nothing gets the
// index fallback. Interior dots are preserved.
func TestFilenameTrailingDot(t *testing.T) {
	cases := map[string]string{
		"report.":      "report",
		"archive.tar.": "archive.tar",
		"..":           "attachment-4",
		"notes .":      "notes",
		"a.b.c.txt":    "a.b.c.txt",
	}
	for in, want := range cases {
		got := SanitizeFilename(in, 4)
		if got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
		if strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") {
			t.Errorf("SanitizeFilename(%q) = %q ends in a dot/space", in, got)
		}
	}
}

// covers: MA-04, R4, R6
// Slugs keep letters and digits of every script (a Japanese, Cyrillic or Arabic
// subject must not collapse to "untitled"), map everything else to a dash, and
// stay filesystem-safe; bidi override characters are dropped. Names are
// NFC-normalized so a decomposed "é" (e + U+0301) and a composed "é" yield one
// byte sequence — the same name on macOS, Linux and Windows.
func TestSlugUnicodeAndNormalization(t *testing.T) {
	cases := map[string]string{
		"会議の議事録 2026年":      "会議の議事録-2026年",
		"Протокол собрания": "Протокол-собрания",
		"محضر الاجتماع":     "محضر-الاجتماع",
		"Re: Café́ menu":    "Re-Café-menu",  // NFD é → NFC é
		"evil‮name.txt":     "evil-name.txt", // bidi override dropped, not kept
		"Hello, World!":     "Hello-World",
	}
	for in, want := range cases {
		got := Slug(in, 60)
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, `<>:"/\|?*`) {
			t.Errorf("Slug(%q) = %q carries a Windows-illegal character", in, got)
		}
	}
	// Segment names are normalized too.
	if got := SanitizeSegment("Café"); got != "Café" {
		t.Errorf("SanitizeSegment(NFD) = %q, want NFC %q", got, "Café")
	}
}

// covers: MA-89, R6, R4
// Distinct source folders whose names differ only in characters the file
// system cannot carry must not merge into one directory: when sanitization had
// to replace or drop characters (or truncate), the segment gets a short suffix
// derived from the ORIGINAL name — deterministic, sibling-independent — while a
// name that needed only whitespace tidying stays as it was.
func TestAlteredSegmentsStayDistinct(t *testing.T) {
	a := SanitizeSegment("Q1: Reports")
	b := SanitizeSegment("Q1? Reports")
	c := SanitizeSegment("Q1 Reports")
	if a == b {
		t.Errorf("distinct folders merged: %q", a)
	}
	if a == c || b == c {
		t.Errorf("an altered name collided with the clean name %q: %q %q", c, a, b)
	}
	if !strings.HasPrefix(a, "Q1 Reports~") || len(a) != len("Q1 Reports~")+8 {
		t.Errorf("altered segment %q should be the clean form plus ~<8 hex>", a)
	}
	if c != "Q1 Reports" {
		t.Errorf("clean name changed: %q", c)
	}
	if SanitizeSegment("  spaced  name  ") != "spaced name" {
		t.Error("whitespace tidying must not add a suffix")
	}
	if SanitizeSegment("Q1: Reports") != a {
		t.Error("suffix is not deterministic")
	}
	long := strings.Repeat("x", 300)
	if got := SanitizeSegment(long); len([]rune(got)) != 120 || !strings.Contains(got, "~") {
		t.Errorf("a truncated name must carry the suffix within the 120-rune bound: %q", got)
	}
}
