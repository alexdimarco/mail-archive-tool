package util

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// covers: MA-418, R4, R6, R29
// FoldKey makes the spellings a case-insensitive or normalizing filesystem treats
// as one name share a key (case, NFC against NFD, and the NTFS/exFAT upcase-table
// equivalents ı/I), and keeps distinct names apart; the 8.3 short-name alias
// pattern matches the aliases Windows invents and not the names this tool writes.
func TestFoldKeyAndShortNameAlias(t *testing.T) {
	if FoldKey("Work") == FoldKey("Report") {
		t.Fatal("two different names share a fold key")
	}
	const school = "école"
	nfc, nfd := norm.NFC.String(school), norm.NFD.String(school)
	if nfc == nfd {
		t.Fatal("the fixture's NFC and NFD spellings are the same bytes")
	}
	same := [][2]string{
		{"Work", "work"},
		{"Report.pdf", "report.PDF"},
		{"I", string(rune(0x131))},                 // dotless i, the upcase-table equivalent of I
		{nfc, nfd},                                 // NFC against NFD
		{"Kelvin", string(rune(0x212a)) + "elvin"}, // the Kelvin sign folds to k
	}
	for _, p := range same {
		if FoldKey(p[0]) != FoldKey(p[1]) {
			t.Errorf("FoldKey(%q) = %q and FoldKey(%q) = %q, want one key", p[0], FoldKey(p[0]), p[1], FoldKey(p[1]))
		}
	}
	for _, alias := range []string{"PROJEC~1", "REPORT~1.PDF", "a~12", "REPORT~1.pdf"} {
		if !IsShortNameAlias(alias) {
			t.Errorf("IsShortNameAlias(%q) = false, want true", alias)
		}
	}
	for _, name := range []string{"report-quarterly-final.pdf", "Q1 Reports~3fa2b1c0", "Inbox", "report~final.pdf", "2026-03"} {
		if IsShortNameAlias(name) {
			t.Errorf("IsShortNameAlias(%q) = true, want false", name)
		}
	}
}
