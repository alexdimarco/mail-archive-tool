// Package util holds filesystem-naming and date-parsing helpers shared by the
// exporter.
package util

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// Characters illegal in Windows path segments (plus control chars).
	illegalChars = regexp.MustCompile(`[<>:"/\\|?*` + "\x00-\x1f" + `]`)
	multiSpace   = regexp.MustCompile(`\s+`)
	multiDash    = regexp.MustCompile(`-{2,}`)
)

// segmentMax bounds a path segment (runes); NTFS/ext4/APFS allow 255 bytes or
// UTF-16 units, and deep trees need headroom under Windows' 260-char paths.
const segmentMax = 120

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
// POSIX filesystems: it NFC-normalizes (one byte sequence per name on every
// OS), strips illegal characters, collapses whitespace, trims trailing
// dots/spaces (which Windows silently drops), neutralizes reserved device
// names (with any extension) and bounds the length. When characters had to be
// replaced or the name truncated, a short suffix derived from the ORIGINAL
// name is appended ("Q1 Reports~3fa2b1c0") so two distinct source folders that
// differ only in such characters never merge into one directory (R6); the
// suffix depends on nothing but the original name, so it is stable across runs.
func SanitizeSegment(s string) string {
	out, changed := sanitizeSegment(s)
	if !changed {
		return out
	}
	suffix := "~" + ShortHash(s)
	return truncateRunes(out, segmentMax-len(suffix)) + suffix
}

// sanitizeSegment is SanitizeSegment without the disambiguating suffix; it
// reports whether any character was replaced/dropped or the name truncated
// (whitespace tidying and reserved-name prefixing do not count).
func sanitizeSegment(s string) (string, bool) {
	s = norm.NFC.String(s)
	cleaned := illegalChars.ReplaceAllString(s, " ")
	changed := cleaned != s
	s = multiSpace.ReplaceAllString(cleaned, " ")
	s = strings.Trim(s, " .")
	if s == "" {
		return "untitled", false
	}
	if isReserved(s) {
		s = "_" + s
	}
	if len([]rune(s)) > segmentMax {
		return truncateRunes(s, segmentMax), true
	}
	return s, changed
}

// isReserved reports whether the part before the first dot is a Windows
// device name: NUL.txt and COM1.log are as uncreatable as NUL and COM1.
func isReserved(s string) bool {
	head := s
	if i := strings.IndexByte(s, '.'); i >= 0 {
		head = s[:i]
	}
	return reservedNames[strings.ToUpper(head)]
}

// SanitizeFilename sanitizes a full file name while preserving its extension.
// index provides a fallback base for nameless attachments. Attachment names are
// disambiguated per archive by the zip writer, so no suffix is added here; the
// result never ends in a dot or space.
func SanitizeFilename(name string, index int) string {
	name = strings.TrimSpace(norm.NFC.String(name))
	if name == "" {
		return "attachment-" + itoa(index)
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base, _ = sanitizeSegment(base)
	ext = illegalChars.ReplaceAllString(ext, "")
	result := strings.TrimRight(base+ext, " .")
	if result == "" || result == "untitled" || strings.TrimLeft(result, ".") == "" {
		return "attachment-" + itoa(index)
	}
	return result
}

// Slug produces a compact, filesystem-friendly slug from an arbitrary string
// (typically an email subject), bounded to maxRunes. Letters and digits of
// every script are kept (a Japanese or Cyrillic subject stays readable);
// everything else — punctuation, symbols, separators, controls and bidi
// overrides — becomes a single dash. NFC-normalized.
func Slug(s string, maxRunes int) string {
	var b strings.Builder
	for _, r := range norm.NFC.String(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := multiDash.ReplaceAllString(b.String(), "-")
	out = strings.Trim(out, "-._ ")
	if out == "" {
		return "untitled"
	}
	return strings.Trim(truncateRunes(out, maxRunes), "-._ ")
}

// ShortHash returns a short, stable hex digest of s, used to guarantee unique
// output filenames without blowing the Windows 260-character path limit.
func ShortHash(s string) string { return HashHex(s, 8) }

// HashHex returns the first n hex characters (n ≤ 40) of the SHA-1 of s: the
// exporter lengthens a file stem's digest deterministically when two keys
// collide at 8 characters.
func HashHex(s string, n int) string {
	sum := sha1.Sum([]byte(s))
	h := hex.EncodeToString(sum[:])
	if n < len(h) {
		return h[:n]
	}
	return h
}

// StripControl removes control characters from an untrusted string: C0 controls
// and DEL (below 0x20, and 0x7f) and the C1 range (0x80–0x9f). Records read from
// an archive directory (the schedule descriptor, the last-run and last-verify
// records) are writable by anyone who can write the directory, and their
// strings are echoed into terminals, dialogs and pasteable remedies; stripping
// here at the read boundary neutralizes ANSI escapes and embedded newlines that
// could forge an output line before they reach any consumer.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// UnderCloudSync reports whether absPath sits inside a consumer cloud-sync
// folder that re-uploads on every change (P3). It recognises OneDrive by the
// OneDrive* environment variables the client sets to its sync roots, and every
// supported provider by a path segment naming its local root: OneDrive
// (a segment beginning with "OneDrive", so "OneDrive - Company" matches),
// Dropbox, Google Drive ("My Drive"/"Google Drive"), iCloud Drive and Box — all
// case-insensitive. The matched service name is returned for a legible remedy;
// ok is false when the path is not synced.
func UnderCloudSync(absPath string) (service string, ok bool) {
	clean := filepath.Clean(absPath)
	for _, env := range []string{"OneDrive", "OneDriveCommercial", "OneDriveConsumer"} {
		if root := os.Getenv(env); root != "" && pathWithin(clean, filepath.Clean(root)) {
			return "OneDrive", true
		}
	}
	for _, seg := range strings.FieldsFunc(clean, func(r rune) bool { return r == '/' || r == '\\' }) {
		if svc := cloudSyncService(seg); svc != "" {
			return svc, true
		}
	}
	return "", false
}

// cloudSyncService maps one path segment to the consumer cloud-sync service
// whose local root it names (case-insensitive), or "" if it names none.
func cloudSyncService(seg string) string {
	l := strings.ToLower(seg)
	switch {
	case strings.HasPrefix(l, "onedrive"):
		return "OneDrive"
	case l == "dropbox":
		return "Dropbox"
	case l == "google drive" || l == "googledrive" || l == "my drive":
		return "Google Drive"
	case l == "icloud drive":
		return "iCloud Drive"
	case l == "box" || l == "box sync":
		return "Box"
	}
	return ""
}

// pathWithin reports whether path is root or a descendant of root, comparing
// case-insensitively (Windows paths are case-insensitive and this is a
// Windows-facing check).
func pathWithin(path, root string) bool {
	if root == "" {
		return false
	}
	p, r := strings.ToLower(path), strings.ToLower(root)
	if p == r {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(p, strings.TrimRight(r, sep)+sep)
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
