package util

import (
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// upcaseWidening are the spellings NTFS and exFAT treat as one name although
// Unicode case folding keeps them apart: their $UpCase table maps the dotless
// ı (U+0131) to I, so "ı" and "I" and "i" are one name on those volumes. Folding
// uses the lower-case side, so ı widens to i.
var upcaseWidening = strings.NewReplacer("ı", "i")

// FoldKey is the key two sibling names must not share: NFC, Unicode case
// folding (cases.Fold), widened with the NTFS/exFAT upcase-table equivalents
// (XC13). Names on a case-insensitive or normalizing filesystem (NTFS, exFAT,
// APFS, HFS+) that have the same FoldKey are one name there, so an export treats
// them as a collision and disambiguates them instead of letting one overwrite the
// other. It is a comparison key only: never written to disk.
func FoldKey(s string) string {
	s = norm.NFC.String(s)
	s = cases.Fold().String(s)
	s = upcaseWidening.Replace(s)
	return norm.NFC.String(s)
}

// shortNameAlias matches the DOS 8.3 short-name alias Windows may give a long
// name ("PROJEC~1", "REPORT~1.PDF"): up to six characters, a tilde, digits, and
// an optional extension of at most three characters.
var shortNameAlias = regexp.MustCompile(`^[^.~]{1,6}~[0-9]+(\.[^.]{0,3})?$`)

// IsShortNameAlias reports whether name looks like an 8.3 short-name alias. NTFS
// can resolve such a name to a different long name in the same directory, so an
// export that wrote one beside the long name it stands for could land on it
// (XC13). The caller disambiguates a matching name.
func IsShortNameAlias(name string) bool { return shortNameAlias.MatchString(name) }
