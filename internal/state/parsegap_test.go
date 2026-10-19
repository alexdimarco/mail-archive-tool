package state

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// covers: MA-334, R1, R26
// A parse gap is a label in the record's own gap lists, in one grammar GapClass owns
// (rulings J5): "\x00kind\x00detail", control-stripped and bounded. The deterministic
// kinds are terminal wherever they sit; the two a complete copy can cure follow the list
// they sit in, so they are fillable on an on-demand source and terminal on Graph (MC20).
// No other label is read as one.
func TestParseGapLabelsAreOneGrammarAndClassed(t *testing.T) {
	label := ParseGapLabel(GapKindUndecodedPart, "r.pdf — encoding \"x-uuencode\"\x1b[31m is not supported")
	kind, detail, ok := ParseGapParts(label)
	if !ok || kind != GapKindUndecodedPart || detail != "r.pdf — encoding \"x-uuencode\"[31m is not supported" {
		t.Errorf("round trip: kind %q detail %q ok %v", kind, detail, ok)
	}
	if long := ParseGapLabel(GapKindCorruptPart, strings.Repeat("é", 1000)); utf8.RuneCountInString(long) != len(parseGapSep+GapKindCorruptPart+parseGapSep)+maxParseGapDetail {
		t.Errorf("a detail is bounded to %d runes, got a label of %d runes", maxParseGapDetail, utf8.RuneCountInString(long))
	}

	for _, kind := range []string{GapKindUndecodedPart, GapKindStructureLimit, GapKindFidelityFallback, GapKindIdentityStub} {
		l := ParseGapLabel(kind, "d")
		if GapClass(l) != ClassTerminal || RecordGapClass(l, false) != ClassTerminal || RecordGapClass(l, true) != ClassTerminal {
			t.Errorf("%s: class %q (Missing %q, Terminal %q), want terminal on every source", kind,
				GapClass(l), RecordGapClass(l, false), RecordGapClass(l, true))
		}
	}
	for _, kind := range []string{GapKindCorruptPart, GapKindUnsplitMultipart} {
		l := ParseGapLabel(kind, "d")
		if GapClass(l) != "" || RecordGapClass(l, false) != ClassFillable || RecordGapClass(l, true) != ClassTerminal {
			t.Errorf("%s: GapClass %q, in Missing %q, in Terminal %q; want the list to decide (fillable, terminal)", kind,
				GapClass(l), RecordGapClass(l, false), RecordGapClass(l, true))
		}
	}

	for _, other := range []string{"body", "report.pdf", "unparsed", "unreadable:body", "\x00", "\x00\x00x", "\x00kind", ""} {
		if IsParseGapLabel(other) {
			t.Errorf("%q is read as a parse gap", other)
		}
	}
}

// covers: MA-348, R1, R18, R26
// Counts() stops counting a record as terminal ("source-empty") when every terminal
// label it has is a parse gap, and UndecodableCount() counts the records with one: the
// status line splits "source-empty" from "cannot decode" on these two numbers. A
// fillable parse gap is fillable like any other (it is re-examined and cured by a
// complete copy) and is also a record with a part this version cannot decode.
func TestParseGapsAreUndecodableNotSourceEmpty(t *testing.T) {
	m, _, tok := gapManifest(t)
	m.Add(Key(tok, "Inbox", "mid:a"), Record{Path: tok + "/a.html", Terminal: []string{ParseGapLabel(GapKindUndecodedPart, "r.pdf")}})
	m.Add(Key(tok, "Inbox", "mid:b"), Record{Path: tok + "/b.html", Missing: []string{ParseGapLabel(GapKindCorruptPart, "cut.pdf")}})
	m.Add(Key(tok, "Inbox", "mid:c"), Record{Path: tok + "/c.html", Terminal: []string{ParseGapLabel(GapKindStructureLimit, "x"), "report.pdf"}})
	m.Add(Key(tok, "Inbox", "mid:d"), Record{Path: tok + "/d.html", Terminal: []string{"report.pdf"}})
	m.Add(Key(tok, "Inbox", "mid:e"), Record{Path: tok + "/e.html", Terminal: []string{LabelUnparsed, ParseGapLabel(GapKindFidelityFallback, "y")}})
	m.Add(Key(tok, "Inbox", "mid:f"), Record{Path: tok + "/f.html", FidelityFallback: 2})
	m.Add(Key(tok, "Inbox", "mid:g"), Record{Path: tok + "/g.html"})

	fillable, terminal, unknown := m.Counts()
	if fillable != 1 || terminal != 3 || unknown != 0 {
		t.Errorf("Counts = fillable %d terminal %d unknown %d; want 1 3 0 (a parse gap alone is not source-empty; report.pdf and unparsed are)", fillable, terminal, unknown)
	}
	if n := m.UndecodableCount(); n != 4 {
		t.Errorf("UndecodableCount = %d, want 4 (records a, b, c and e)", n)
	}
	if n := m.UnparsedTerminalCount(); n != 1 {
		t.Errorf("UnparsedTerminalCount = %d, want 1: record e is terminal only because it is unparsed, a parse gap beside it does not change that", n)
	}
	if n := m.FidelityFallbackCount(); n != 1 {
		t.Errorf("FidelityFallbackCount = %d, want 1 (record f)", n)
	}
}
