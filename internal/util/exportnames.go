package util

import (
	"strings"
	"unicode/utf8"
)

// The export-only, stricter naming pass (owner ruling Q15, extraction design P7
// and §4.18): the copy export writes folder and attachment names through
// ExportSegment and ExportFilename, which layer F75 (bidi controls), F91 (the 255-byte
// segment limit of Linux filesystems) and F111 (the reserved device names
// SanitizeSegment misses) over SanitizeSegment and SanitizeFilename. They call
// those, never a copy of them, and change nothing for the archive writer, whose
// own names keep F75, F91 and F111 as an open gap (no archive migration).

// ExportSegmentMaxBytes is the longest path segment an export writes: 255 bytes,
// the limit of ext4, xfs, btrfs and most Linux filesystems (APFS and NTFS count
// UTF-8 bytes less strictly, so it is never too long for them).
const ExportSegmentMaxBytes = 255

// ExportSegment makes name safe as one folder segment in an export: it strips
// the bidirectional controls (F75), applies SanitizeSegment, prefixes a reserved
// device name SanitizeSegment misses (F111: CONIN$, CONOUT$, COM0, COM¹..COM³,
// LPT0, LPT¹..LPT³, and a name whose base has trailing spaces before its
// extension, "CON .txt") and bounds the result to ExportSegmentMaxBytes at a
// rune boundary (F91), keeping a "~" and a hash of the original name so the cut
// never merges two names. A name changed by the bidi strip carries that suffix
// too.
func ExportSegment(name string) string {
	clean := StripBidi(name)
	out := SanitizeSegment(clean)
	if isReservedStrict(out) {
		out = "_" + out
	}
	if clean != name && !hasHashSuffix(out) {
		out = JoinSuffix(out, "~"+ShortHash(name), ExportSegmentMaxBytes)
	}
	return boundBytes(out, name, ExportSegmentMaxBytes)
}

// ExportFilename is SanitizeFilename with the same pass, for a file name that
// keeps its extension (an extracted attachment): bidi controls stripped, a
// reserved base prefixed, and the whole name bounded to ExportSegmentMaxBytes by
// cutting the base, never the extension (an extension longer than 32 bytes is cut
// too).
func ExportFilename(name string, index int) string {
	clean := StripBidi(name)
	out := SanitizeFilename(clean, index)
	if isReservedStrict(out) {
		out = "_" + out
	}
	if len(out) <= ExportSegmentMaxBytes {
		return out
	}
	base, ext := out, ""
	if i := strings.LastIndexByte(out, '.'); i > 0 {
		base, ext = out[:i], truncateBytes(out[i:], 32)
	}
	suffix := "~" + ShortHash(name)
	base = strings.TrimRight(truncateBytes(base, ExportSegmentMaxBytes-len(ext)-len(suffix)), " .")
	return base + suffix + ext
}

// JoinSuffix appends suffix to name and keeps the result within max bytes by
// cutting name (never the suffix) at a rune boundary. The copy export uses it for
// the "~<hash>" and "~2" disambiguators it adds to a name that is already at the
// bound.
func JoinSuffix(name, suffix string, max int) string {
	if len(name)+len(suffix) <= max {
		return name + suffix
	}
	return strings.TrimRight(truncateBytes(name, max-len(suffix)), " .") + suffix
}

// reservedExtra are the device names beyond reservedNames that Windows refuses
// (F111).
var reservedExtra = map[string]bool{"CONIN$": true, "CONOUT$": true, "CLOCK$": true, "COM0": true, "LPT0": true}

// isReservedStrict is isReserved widened for F111: the part before the first dot,
// without trailing spaces (Windows ignores them, so "CON .txt" is CON), is
// compared upper-cased against the device names, the console names and the
// superscript-digit COM and LPT names.
func isReservedStrict(s string) bool {
	if isReserved(s) {
		return true
	}
	head := s
	if i := strings.IndexByte(s, '.'); i >= 0 {
		head = s[:i]
	}
	up := strings.ToUpper(strings.TrimRight(head, " "))
	if reservedNames[up] || reservedExtra[up] {
		return true
	}
	for _, p := range []string{"COM", "LPT"} {
		if rest, ok := strings.CutPrefix(up, p); ok {
			if r, n := utf8.DecodeRuneInString(rest); n == len(rest) && n > 0 && (r == '¹' || r == '²' || r == '³') {
				return true
			}
		}
	}
	return false
}

// hasHashSuffix reports whether s already ends with SanitizeSegment's "~" and
// eight hex digits.
func hasHashSuffix(s string) bool {
	if len(s) < 9 || s[len(s)-9] != '~' {
		return false
	}
	for _, c := range s[len(s)-8:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// boundBytes cuts s to max bytes at a rune boundary, in place of its tail with
// "~" and a hash of orig, so two long names never become one.
func boundBytes(s, orig string, max int) string {
	if len(s) <= max {
		return s
	}
	base := s
	if hasHashSuffix(base) {
		base = base[:len(base)-9]
	}
	return JoinSuffix(strings.TrimRight(base, " ."), "~"+ShortHash(orig), max)
}

// truncateBytes cuts s to at most max bytes without splitting a UTF-8 sequence.
func truncateBytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
