// Package util holds filesystem-naming and date-parsing helpers shared by the
// exporter.
package util

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// extMax bounds the extension SanitizeFilename keeps (runes, the dot included): a longer
// one is part of the base name and is cut with it, so no name reaches a zip entry (at most
// 65,535 bytes) or a path longer than segmentMax plus extMax runes (MS2-AO9).
const extMax = 24

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
	if len([]rune(ext)) > extMax {
		ext = "" // not an extension: the whole name is a base, and a base is bounded
	}
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

// StripBidi removes the Unicode bidirectional formatting controls (U+202A to
// U+202E, the embeddings and overrides, and U+2066 to U+2069, the isolates): a
// right-to-left override in an untrusted field reverses what follows it in a
// spreadsheet or a page, so "invoice.pdf" can read as "fdp.eciovni". StripControl
// leaves them because they are printable text; fields that are echoed from mail
// into a report or a status line strip them too (friction F16).
func StripBidi(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
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
// Dropbox, Google Drive ("My Drive"/"Google Drive"), iCloud Drive ("iCloudDrive"
// on Windows), Box, Nextcloud, ownCloud, pCloud and Synology Drive — all
// case-insensitive. On macOS everything under Library/CloudStorage (where the
// File Provider clients keep their roots) and Library/Mobile Documents (iCloud
// Drive's own store) is synced too (encrypted archives phase 3, §10). The matched
// service name is returned for a legible remedy; ok is false when the path is not
// synced.
func UnderCloudSync(absPath string) (service string, ok bool) {
	clean := filepath.Clean(absPath)
	for _, env := range []string{"OneDrive", "OneDriveCommercial", "OneDriveConsumer"} {
		if root := os.Getenv(env); root != "" && pathWithin(clean, filepath.Clean(root)) {
			return "OneDrive", true
		}
	}
	segs := strings.FieldsFunc(clean, func(r rune) bool { return r == '/' || r == '\\' })
	for i, seg := range segs {
		if strings.EqualFold(seg, "Library") && i+1 < len(segs) {
			switch {
			case strings.EqualFold(segs[i+1], "Mobile Documents"):
				return "iCloud Drive", true
			case strings.EqualFold(segs[i+1], "CloudStorage"):
				if i+2 < len(segs) {
					if svc := cloudStorageService(segs[i+2]); svc != "" {
						return svc, true
					}
				}
				return "a cloud storage service", true
			}
		}
		if svc := cloudSyncService(seg); svc != "" {
			return svc, true
		}
	}
	return "", false
}

// MaySync reports whether absPath sits in a folder macOS may keep in iCloud
// without the path saying so — the Desktop and Documents folders under home, when
// iCloud's "Desktop & Documents Folders" is on (phase 3 §10: "this folder may be
// synced"). It is false on every other system, where a synced Desktop or Documents
// moves to a path UnderCloudSync recognises.
func MaySync(absPath, home string) bool { return maySync(absPath, home, runtime.GOOS) }

func maySync(absPath, home, goos string) bool {
	if goos != "darwin" || home == "" {
		return false
	}
	clean, h := filepath.Clean(absPath), filepath.Clean(home)
	return pathWithin(clean, filepath.Join(h, "Desktop")) || pathWithin(clean, filepath.Join(h, "Documents"))
}

// cloudStorageService names the service whose root a Library/CloudStorage folder
// is, from the name the File Provider client gives it ("GoogleDrive-<account>",
// "OneDrive-Personal", "Dropbox", "Box-Box"), or "" when it is not one MailArchive
// knows.
func cloudStorageService(seg string) string {
	head, _, _ := strings.Cut(seg, "-")
	if svc := cloudSyncService(head); svc != "" {
		return svc
	}
	return ""
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
	case l == "icloud drive" || l == "iclouddrive":
		return "iCloud Drive"
	case l == "box" || l == "box sync":
		return "Box"
	case l == "nextcloud":
		return "Nextcloud"
	case l == "owncloud":
		return "ownCloud"
	case l == "pcloud drive" || l == "pclouddrive":
		return "pCloud"
	case l == "synologydrive" || l == "synology drive":
		return "Synology Drive"
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

// ValidRecordedPath validates a path a manifest records as a clean, relative,
// forward-slashed in-archive path whose first segment is a store token: no absolute
// path, no backslash or drive colon, no empty, "." or ".." segment, and a first segment
// that is not a dotfile (FC6). It returns the split segments on success. The manifest is
// untrusted (someone else may have written it), so anything that acts on a recorded path
// (verify, the rebuild, the exporter's removal of a record's previous files) asks first.
func ValidRecordedPath(p string) ([]string, bool) {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return nil, false
	}
	if strings.ContainsAny(p, `\:`) {
		return nil, false
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if s == "" || s == "." || s == ".." {
			return nil, false
		}
		if i == 0 && strings.HasPrefix(s, ".") {
			return nil, false
		}
	}
	return parts, true
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
