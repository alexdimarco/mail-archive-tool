// Package util holds filesystem-naming and date-parsing helpers shared by the
// exporter.
package util

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Characters illegal in Windows path segments (plus control chars).
	illegalChars = regexp.MustCompile(`[<>:"/\\|?*` + "\x00-\x1f" + `]`)
	multiSpace   = regexp.MustCompile(`\s+`)
	nonSlug      = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	multiDash    = regexp.MustCompile(`-{2,}`)
)

// reservedNames are Windows device names that cannot be used as a bare file or
// directory name regardless of extension.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeSegment makes s safe to use as a single path segment on Windows and
// POSIX filesystems: it strips illegal characters, collapses whitespace, trims
// trailing dots/spaces (which Windows silently drops) and bounds the length.
func SanitizeSegment(s string) string {
	s = illegalChars.ReplaceAllString(s, " ")
	s = multiSpace.ReplaceAllString(s, " ")
	s = strings.Trim(s, " .")
	if s == "" {
		return "untitled"
	}
	if reservedNames[strings.ToUpper(s)] {
		s = "_" + s
	}
	return truncateRunes(s, 120)
}

// SanitizeFilename sanitizes a full file name while preserving its extension.
// index provides a fallback base for nameless attachments.
func SanitizeFilename(name string, index int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "attachment-" + itoa(index)
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base = SanitizeSegment(base)
	ext = illegalChars.ReplaceAllString(ext, "")
	return base + ext
}

// Slug produces a compact, filesystem-friendly slug from an arbitrary string
// (typically an email subject), bounded to maxRunes.
func Slug(s string, maxRunes int) string {
	s = nonSlug.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-._ ")
	if s == "" {
		return "untitled"
	}
	return strings.Trim(truncateRunes(s, maxRunes), "-._ ")
}

// ShortHash returns a short, stable hex digest of s, used to guarantee unique
// output filenames without blowing the Windows 260-character path limit.
func ShortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func itoa(i int) string {
	// Small, allocation-free integer to string for non-negative indexes.
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
