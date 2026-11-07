package tbrestore

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/util"
)

// siblingKey is the key two sibling folder names of a restore must not share: the restore's
// own, stronger than util.FoldKey (settled 2026-11-07 [PCT-1]). Every place the restore asks
// whether two names clash uses it: the " (n)" of FinalNames' siblings and of the Windows
// re-dedupe (dedupe), and the free top name in Local Folders (FreeTopName).
//
// Why. Thunderbird drops a second sibling folder whose name equals an earlier one by its own
// case-blind rule: PopulateFolderHierarchy (comm-central
// mailnews/protocols/common/src/FolderPopulation.cpp) adds a discovered child only when
// parent->GetChildNamed(name) finds none, and nsMsgDBFolder::GetChildNamed
// (mailnews/base/src/nsMsgDBFolder.cpp; both read at releases-comm-central d1da2f0b,
// 2026-11-07) compares with nsCaseInsensitiveUTF8StringComparator. That comparator
// (firefox intl/unicharutil/util/nsUnicharUtils.cpp, CaseInsensitiveCompare and
// GetLowerUTF8Codepoint_inline; the same at main ac7b5e4b and at esr153 4bd98d39) lowers
// each code point on its own — ASCII through a table, anything else through
// intl::UnicodeProperties::ToLower, which is ICU's u_tolower (intl/components/src/
// UnicodeProperties.h), the simple lowercase mapping — and calls two names equal when they
// have as many code points and each pair lowers alike. ICU there is 78.3 (uvernum.h at
// esr153), Unicode 17. util.FoldKey runs x/text's full case folding, which on v0.43.0 SWAPS
// Cherokee case (U+13A0 to U+AB70 and U+AB70 to U+13A0) instead of joining it, so "ᎠᎡ" and
// "ꭰꭱ" had two keys, both were written, and Thunderbird 153.6.0 showed one (the review's
// PCT-1); on a case-blind Mac volume the second create failed part way.
//
// How. The key is FoldKey's, then more merging, so every pair FoldKey joins it joins too
// (nothing that clashed before stops clashing): after FoldKey, "i" + U+0307 (FoldKey's full
// fold of the dotted capital I) becomes "i"; each code point becomes newerLower's (case pairs
// of Unicode 16 and 17 that Go 1.26's Unicode 15 tables do not know), then the smallest
// member of its simple case-fold orbit (unicode.SimpleFold), which joins a character with
// its lowercase, Cherokee included; then NFC and the orbit step once more (NFC can compose a
// new character, "I" + U+0307 to U+0130). Merging more than Thunderbird does only costs an
// unneeded " (2)". A comparison key only: never written to disk. util.FoldKey itself, and
// the export and extract paths that use it, are unchanged (the review's settled table owes
// the check whether the weaker key matters there).
func siblingKey(s string) string {
	k := util.FoldKey(s)
	k = strings.ReplaceAll(k, "i\u0307", "i")
	k = strings.Map(orbitMin, k)
	k = norm.NFC.String(k)
	return strings.Map(orbitMin, k)
}

// orbitMin is r's case representative: newerLower's mapping first, then the smallest code
// point of the simple case-fold orbit (r itself when it has no case partner).
func orbitMin(r rune) rune {
	if l, ok := newerLower[r]; ok {
		r = l
	}
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < m {
			m = f
		}
	}
	return m
}

// newerLower are the simple lowercase mappings of Unicode 17.0.0 (UnicodeData.txt field 13,
// read from unicode.org 2026-11-07) that Go 1.26's tables (Unicode 15.0.0) do not have:
// letters added in Unicode 16 and 17, which Thunderbird's ICU 78 lowers. Each is mapped to
// its lowercase before the orbit step. When Go's tables catch up, an entry only repeats what
// unicode.SimpleFold already joins. A later Thunderbird on a newer Unicode adds pairs this
// list does not know; TestTheSiblingKeyJoinsWhatThunderbirdJoins names the source to
// re-read.
var newerLower = func() map[rune]rune {
	m := map[rune]rune{
		0x1C89: 0x1C8A, // CYRILLIC CAPITAL LETTER TJE
		0xA7CB: 0x0264, // LATIN CAPITAL LETTER RAMS HORN
		0xA7CC: 0xA7CD, // LATIN CAPITAL LETTER S WITH DIAGONAL STROKE
		0xA7CE: 0xA7CF, // LATIN CAPITAL LETTER PHARYNGEAL VOICED FRICATIVE
		0xA7D2: 0xA7D3, // LATIN CAPITAL LETTER DOUBLE THORN
		0xA7D4: 0xA7D5, // LATIN CAPITAL LETTER DOUBLE WYNN
		0xA7DA: 0xA7DB, // LATIN CAPITAL LETTER LAMBDA
		0xA7DC: 0x019B, // LATIN CAPITAL LETTER LAMBDA WITH STROKE
	}
	for r := rune(0x10D50); r <= 0x10D65; r++ { // GARAY CAPITAL LETTER A … OLD NA
		m[r] = r + 0x20
	}
	for r := rune(0x16EA0); r <= 0x16EB8; r++ { // BERIA ERFE CAPITAL LETTER ARKAB … AY
		m[r] = r + 0x1B
	}
	return m
}()
