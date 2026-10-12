package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-435, R20, R29
// MS2-AO8. The copy-export notes for an archive record's own gaps (MA-435) say what a parse
// gap is, in the report row's words, and not that an attachment "was never downloaded": a
// parse-gap label is a part kept as received, a multipart shown as text, or a message kept as an
// earlier version read it, none of which is a download that did not happen.
func TestCopyExportNotesDescribeParseGapsAsParseGaps(t *testing.T) {
	label := func(kind, detail string) string { return state.ParseGapLabel(kind, detail) }
	rec := state.Record{
		Terminal: []string{
			label(state.GapKindUndecodedPart, "x.bin.undecoded — the transfer encoding \"x-uuencode\" is not supported"),
			label(state.GapKindFidelityFallback, "this version's reading held less"),
			label(state.GapKindIdentityStub, "an earlier parser could not read this message"),
		},
		Missing: []string{label(state.GapKindUnsplitMultipart, "the multipart has no boundary parameter"), "doc.pdf"},
	}
	notes := archiveGapNotes(rec)
	joined := strings.Join(notes, "\n")
	for _, bad := range []string{"undecoded-part", "fidelity-fallback", "identity-stub", "unsplit-multipart"} {
		if strings.Contains(joined, "attachment '"+bad) {
			t.Errorf("a %s gap is described as an attachment: %q", bad, joined)
		}
	}
	if n := strings.Count(joined, "never downloaded"); n != 1 || !strings.Contains(joined, "the archive records attachment 'doc.pdf' as never downloaded") {
		t.Errorf("notes %q: want only the real attachment gap described as never downloaded", notes)
	}
	for _, want := range []string{
		state.RecordRowWhy(state.GapKindUndecodedPart), state.RecordRowWhy(state.GapKindFidelityFallback),
		state.RecordRowWhy(state.GapKindIdentityStub), state.RecordRowWhy(state.GapKindUnsplitMultipart),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the notes %q do not give the reason %q", notes, want)
		}
	}
	if strings.ContainsRune(joined, 0) {
		t.Errorf("the label separator reached the notes: %q", joined)
	}
}
