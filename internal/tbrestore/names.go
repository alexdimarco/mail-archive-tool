package tbrestore

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"mail-archive-tool/internal/util"
)

// The restore's folder names (design §4.4 as folded, RT-6, foldcheck N6). Every name is
// final in the plan, so no name can fail part way through a restore.

// TopPrefix begins the new top folder's name (owner question Q1's recommended answer).
const TopPrefix = "Restored — "

// The name budgets. The file systems' (§4.4 note step 5): 255 minus 4 for Thunderbird's
// ".msf" (or ".sbd") minus 5 for a " (99)" suffix, in UTF-8 bytes and UTF-16 units.
//
// Settled 2026-11-07 [MA-1095 lab]: Thunderbird's own is far tighter. A folder whose name
// NS_MsgHashIfNecessary would change is kept by Thunderbird under that changed (hashed)
// file name, so it never reads the file the restore wrote and shows the folder empty
// (lab BUG-1). Its rule, read in comm-central mailnews/base/src/nsMsgUtils.cpp
// (releases-comm-central 9ff8c8b16fba, 2026-11-04) with FILE_ILLEGAL_CHARACTERS from
// xpcom/base/nsCRTGlue.h (firefox 6149f492996, 2026-03-16): a name is hashed when it holds
// the path separator, a control character (U+0001–U+001F, U+007F–U+009F), any of
// `/:*?"<>|`, or `;` or `#`; when it starts with "."; when it ends with ".", "~" or a
// space; or when it is longer than MAX_LEN, 55 UTF-16 units. tbNameMax is that limit;
// every name the restore writes is held to it (and so to the byte budget too: 55 units
// are at most 165 UTF-8 bytes).
const (
	nameBudget    = 246
	tbNameMax     = 55
	hashSuffixLen = 9 // "~" + 8 hex
	// topNameMax is the top name's limit without its " (n)": room is kept for " (99)".
	topNameMax = tbNameMax - len(" (99)")
)

// SafeName is §4.4 note steps 1–4 for one name from the archive (untrusted): file-safe
// on every platform, never hidden, never a device, never a name Thunderbird skips.
// Changed reports whether the name had to change from what the archive spells.
func SafeName(s string) (name string, changed bool) {
	out := util.SanitizeSegment(s)
	// Step 2: what SanitizeSegment keeps: bidirectional controls, U+200E and U+200F,
	// DEL and the C1 controls; then the trims SanitizeSegment did, again, since a
	// removed character may have uncovered a leading dot or a trailing space.
	// Settled 2026-11-07 [MA-1095 lab]: ";" and "#" anywhere become "_", since Thunderbird
	// hashes a name that holds either (the rest of its list SanitizeSegment and this step
	// already remove).
	out = util.StripBidi(out)
	out = strings.Map(func(r rune) rune {
		if r == 0x200e || r == 0x200f || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		if r == ';' || r == '#' {
			return '_'
		}
		return r
	}, out)
	out = strings.Trim(out, " .")
	if out == "" {
		out = "untitled"
	}
	// Step 3: the device names SanitizeSegment misses.
	if isDeviceName(out) {
		out = "_" + out
	}
	// Step 4: Thunderbird's skip rules (nsMsgLocalStoreUtils::nsShouldIgnoreFile).
	out = notSkipped(out)
	return out, out != s
}

// deviceNames are the Windows device names, matched case-blind on the part before the
// first dot with trailing spaces ignored (§4.4 note step 3): the ones SanitizeSegment
// knows, and COM0, LPT0, the superscript digits, CONIN$, CONOUT$ and CLOCK$.
var deviceNames = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true, "CLOCK$": true}
	for _, d := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
		m["COM"+d] = true
		m["LPT"+d] = true
	}
	return m
}()

func isDeviceName(s string) bool {
	head := s
	if i := strings.IndexByte(s, '.'); i >= 0 {
		head = s[:i]
	}
	return deviceNames[strings.ToUpper(strings.TrimRight(head, " "))]
}

// Thunderbird's skip rules, from nsMsgLocalStoreUtils::nsShouldIgnoreFile
// (comm-central mailnews/local/src/nsMsgLocalStoreUtils.cpp, read 2026-11-07 on the
// releases-comm-central mirror, not run): a leading "." or "#", a trailing "~", these
// exact names (case-blind), the endings .snm, .toc, .mozmsgs, .sbd and .msf, and the
// prefix "feeditems_error". Matched case-blind here (the source compares the endings
// as written; case-blind can only change one more name, the safe direction). This
// list replaces §4.4's provisional one (whose .dat, .json, .html and .htm endings the
// source does not have).
var skippedNames = map[string]bool{
	"msgfilterrules.dat": true, "rules.dat": true, "filterlog.html": true, "junklog.html": true,
	"rulesbackup.dat": true, "popstate.dat": true, "sort.dat": true, "mailfilt.log": true,
	"filters.js": true, "feeds.json": true, "feeds.json.tmp": true, "feeds.json.backup": true,
	"feeds.json.corrupt": true, "feeditems.json": true, "feeditems.json.tmp": true,
	"feeditems.json.backup": true, "feeditems.json.corrupt": true, "feeds.rdf": true, "feeditems.rdf": true,
}

var skippedEndings = []string{".snm", ".toc", ".mozmsgs", ".sbd", ".msf"}

// skipped reports whether Thunderbird would pass over a file of this name.
func skipped(s string) bool {
	l := strings.ToLower(s)
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "#") || strings.HasSuffix(s, "~") ||
		skippedNames[l] || strings.HasPrefix(l, "feeditems_error") {
		return true
	}
	for _, e := range skippedEndings {
		if strings.HasSuffix(l, e) {
			return true
		}
	}
	return false
}

// notSkipped changes a name Thunderbird would pass over: a leading "#" (or ".") and the
// "feeditems_error" prefix get "_" put first; anything else gets "_" appended.
func notSkipped(s string) string {
	if !skipped(s) {
		return s
	}
	l := strings.ToLower(s)
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, ".") || strings.HasPrefix(l, "feeditems_error") {
		s = "_" + s
	}
	if skipped(s) {
		s += "_"
	}
	return s
}

// utf16Len is a name's length in UTF-16 units (NTFS's count).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
		if utf16.RuneLen(r) < 0 {
			n += 2 // an invalid rune is written as U+FFFD: one unit; count two, the safe side
		}
	}
	return n
}

// fits reports s within max UTF-8 bytes and max UTF-16 units.
func fits(s string, max int) bool { return len(s) <= max && utf16Len(s) <= max }

// cutUnits cuts s at a character boundary until it is at most max UTF-16 units: the
// unit Thunderbird's MAX_LEN counts. (A character is never split, so no surrogate half
// is left, as Thunderbird's own cut also avoids.)
func cutUnits(s string, max int) string {
	for utf16Len(s) > max {
		_, size := utf8.DecodeLastRuneInString(s)
		if size == 0 {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

// fitsTB reports s within units UTF-16 units (at most tbNameMax) and the byte budget.
func fitsTB(s string, units int) bool { return utf16Len(s) <= units && len(s) <= nameBudget }

// hashOf is the 8-hex suffix of a name's original bytes.
func hashOf(original string) string { return "~" + util.HashHex(original, 8) }

// budgeted is §4.4 note step 5 as settled 2026-11-07 [MA-1095 lab]: s within units
// (Thunderbird's 55, or the top name's 50), else cut keeping room for the hash suffix of
// the original, so two cut names stay distinct. prefix (the top folder's "Restored — ")
// counts against the budget.
func budgeted(s, original, prefix string, units int) string {
	if fitsTB(prefix+s, units) {
		return s
	}
	return strings.TrimPrefix(cutUnits(prefix+s, units-hashSuffixLen), prefix) + hashOf(original)
}

// ThunderbirdReads reports whether Thunderbird keeps a folder of this name under this
// very file name: NS_MsgHashIfNecessary would return it unchanged (the rule above, with
// both platforms' separators, since a profile can be copied between them). A name it
// would hash, Thunderbird shows empty. The plan and the write hold every name to it
// (defence in depth: the name steps already make every name so).
func ThunderbirdReads(name string) bool {
	if name == "" || utf16Len(name) > tbNameMax {
		return false
	}
	for _, r := range name {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || strings.ContainsRune(`/\:*?"<>|;#`, r) {
			return false
		}
	}
	last, _ := utf8.DecodeLastRuneInString(name)
	return name[0] != '.' && last != '.' && last != '~' && last != ' '
}

// FinalNames is §4.4 note steps 1–6 for one folder's children, in archive order:
// each made safe and within budget, then, LAST, siblings that now spell the same —
// after NFC and case-blind on every platform, since a profile can be copied to Windows
// — get " (2)", " (3)" … up to " (99)", and past that the hash suffix, cut again so the
// name fits 251 bytes and units (N6). taken are names already used among these
// siblings (none, for a new folder). changed reports, per name, that it is not the
// archive's own.
func FinalNames(originals []string) (names []string, changed []bool) {
	names = make([]string, len(originals))
	changed = make([]bool, len(originals))
	for i, o := range originals {
		n, ch := SafeName(o)
		n = notShortAlias(budgeted(n, o, "", tbNameMax), o)
		names[i], changed[i] = n, ch || n != o
	}
	dedupe(names, originals, changed)
	return names, changed
}

// notShortAlias is a name that does not look like a Windows 8.3 short-name alias
// ("LONGFO~1", "REPORT~2.PDF"): Windows may give that alias to a long-named sibling, so
// the exclusive create of a folder named like one could find the sibling and fail at
// Start. Such a name gets the hash suffix of its original, as extract's names do (XC13,
// util.IsShortNameAlias), on every platform since a profile can be copied to Windows
// (ADV-05, settled 2026-11-07). The result has two tildes, so it is never an alias.
func notShortAlias(n, original string) string {
	if !util.IsShortNameAlias(n) {
		return n
	}
	return cutUnits(n, tbNameMax-hashSuffixLen) + hashOf(original)
}

// dedupe gives later siblings that spell the same as an earlier one a free " (n)"
// suffix (or the hash suffix past " (99)"), in place. Settled 2026-11-07 [MA-1095 lab]:
// the name is cut first where the suffix would take it past Thunderbird's 55 units, so a
// name kept whole up to 55 still gets a " (n)" Thunderbird reads.
func dedupe(names, originals []string, changed []bool) {
	seen := map[string]bool{}
	for i, n := range names {
		if !seen[util.FoldKey(n)] {
			seen[util.FoldKey(n)] = true
			continue
		}
		found := false
		for k := 2; k <= 99; k++ {
			sfx := " (" + itoa(k) + ")"
			c := cutUnits(n, tbNameMax-len(sfx)) + sfx
			if !seen[util.FoldKey(c)] {
				names[i], found = c, true
				break
			}
		}
		if !found {
			c := cutUnits(n, tbNameMax-hashSuffixLen) + hashOf(originals[i]+"\x00"+itoa(i))
			names[i] = notShortAlias(c, originals[i]+"\x00"+itoa(i)+"\x00alias")
		}
		changed[i] = true
		seen[util.FoldKey(names[i])] = true
	}
}

// TopName is the new top folder's name for the archive folder's own name, without its
// " (n)": "Restored — " + the name made safe, the prefix counted in the budget. Settled
// 2026-11-07 [MA-1095 lab]: with " (99)" it is at most Thunderbird's 55 units, so NAME
// is at most 39 and a longer one is cut to 30 plus the hash suffix of the archive's name
// (lab BUG-1: a NAME over 44 hid the whole restore).
func TopName(archiveName string) string {
	top, _ := topName(archiveName)
	return top
}

// topName is TopName and whether NAME is not the archive's own spelling.
func topName(archiveName string) (string, bool) {
	n, _ := SafeName(archiveName)
	n = budgeted(n, archiveName, TopPrefix, topNameMax)
	return TopPrefix + n, n != archiveName
}

// withNumber is the top name with its " (n)" (n 1: none).
func withNumber(top string, n int) string {
	if n <= 1 {
		return top
	}
	return top + " (" + itoa(n) + ")"
}
