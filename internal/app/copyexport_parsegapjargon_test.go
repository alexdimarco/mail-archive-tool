package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-435, R20, R29
// F15 of the MF-S2 friction review (cell OW-8). The copy-export note for a fallback or an
// identity-stub gap gives the row's why and no more: its detail is the walker's own account of
// what it compared with an earlier parser, which a person who receives the export folder cannot
// act on. A part's gap keeps its detail, which names the part (the positive twin).
func TestCopyExportNoteForAFallbackLeavesTheWalkersJargonOut(t *testing.T) {
	label := func(kind, detail string) string { return state.ParseGapLabel(kind, detail) }
	jargon := "this version's reading of the message held less than the earlier parser's (its plain body is empty); the message is kept as an earlier version read it"
	rec := state.Record{Terminal: []string{
		label(state.GapKindFidelityFallback, jargon),
		label(state.GapKindIdentityStub, "an earlier parser could not read this message, so it is filed under the placeholder identity that parser gave such messages"),
		label(state.GapKindUndecodedPart, "x.bin.undecoded — the transfer encoding \"x-uuencode\" is not supported"),
	}}
	joined := strings.Join(archiveGapNotes(rec), "\n")
	if !strings.Contains(joined, "x.bin.undecoded") {
		t.Fatalf("positive twin: a part's note lost the part's name:\n%s", joined)
	}
	for _, bad := range []string{"earlier parser", "this version's reading", "placeholder identity"} {
		if strings.Contains(joined, bad) {
			t.Errorf("the notes carry the walker's phrase %q:\n%s", bad, joined)
		}
	}
	for _, want := range []string{state.RecordRowWhy(state.GapKindFidelityFallback), state.RecordRowWhy(state.GapKindIdentityStub)} {
		if !strings.Contains(joined, want) {
			t.Errorf("the notes do not give the reason %q:\n%s", want, joined)
		}
	}
}
