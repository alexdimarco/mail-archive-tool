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

// The name budgets, in the units the file systems count (§4.4 note step 5): 255 minus 4
// for Thunderbird's ".msf" (or ".sbd") minus 5 for a " (99)" suffix; and the cut-again
// budget when the hash suffix replaces " (n)" (N6): 255 minus 4.
const (
	nameBudget     = 246
	nameBudgetHash = 251
	hashSuffixLen  = 9 // "~" + 8 hex
)

// SafeName is §4.4 note steps 1–4 for one name from the archive (untrusted): file-safe
// on every platform, never hidden, never a device, never a name Thunderbird skips.
// Changed reports whether the name had to change from what the archive spells.
func SafeName(s string) (name string, changed bool) {
	out := util.SanitizeSegment(s)
	// Step 2: what SanitizeSegment keeps: bidirectional controls, U+200E and U+200F,
	// DEL and the C1 controls; then the trims SanitizeSegment did, again, since a
	// removed character may have uncovered a leading dot or a trailing space.
	out = util.StripBidi(out)
	out = strings.Map(func(r rune) rune {
		if r == 0x200e || r == 0x200f || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
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

// cutTo cuts s at a character boundary until it fits max bytes and units.
func cutTo(s string, max int) string {
	for !fits(s, max) {
		_, size := utf8.DecodeLastRuneInString(s)
		if size == 0 {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

// hashOf is the 8-hex suffix of a name's original bytes.
func hashOf(original string) string { return "~" + util.HashHex(original, 8) }

// budgeted is §4.4 note step 5: s within budget, else cut keeping room for the hash
// suffix of the original, so two cut names stay distinct. prefix (the top folder's
// "Restored — ") counts against the budget.
func budgeted(s, original, prefix string, budget int) string {
	if fits(prefix+s, budget) {
		return s
	}
	return strings.TrimPrefix(cutTo(prefix+s, budget-hashSuffixLen), prefix) + hashOf(original)
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
		n = budgeted(n, o, "", nameBudget)
		names[i], changed[i] = n, ch || n != o
	}
	dedupe(names, originals, changed)
	return names, changed
}

// dedupe gives later siblings that spell the same as an earlier one a free " (n)"
// suffix (or the hash suffix past " (99)"), in place.
func dedupe(names, originals []string, changed []bool) {
	seen := map[string]bool{}
	for i, n := range names {
		if !seen[util.FoldKey(n)] {
			seen[util.FoldKey(n)] = true
			continue
		}
		found := false
		for k := 2; k <= 99; k++ {
			c := n + " (" + itoa(k) + ")"
			if !seen[util.FoldKey(c)] {
				names[i], found = c, true
				break
			}
		}
		if !found {
			c := cutTo(n, nameBudgetHash-hashSuffixLen) + hashOf(originals[i]+"\x00"+itoa(i))
			names[i] = c
		}
		changed[i] = true
		seen[util.FoldKey(names[i])] = true
	}
}

// TopName is the new top folder's name for the archive folder's own name, without its
// " (n)": "Restored — " + the name made safe, the prefix counted in the budget.
func TopName(archiveName string) string {
	n, _ := SafeName(archiveName)
	return TopPrefix + budgeted(n, archiveName, TopPrefix, nameBudget)
}

// withNumber is the top name with its " (n)" (n 1: none).
func withNumber(top string, n int) string {
	if n <= 1 {
		return top
	}
	return top + " (" + itoa(n) + ")"
}
