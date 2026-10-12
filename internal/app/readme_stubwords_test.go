package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1, R7
// F7 of the MF-S2 friction review, re-verification (cell IN-1). The new README.txt paragraph said
// that a row of kind fidelity-fallback OR identity-stub "means the page shows the message as an
// earlier version of the tool read it". That is true of a fallback and untrue of an identity stub: the
// row's own why says the page shows what the version that wrote it reads, and only the identity (the
// placeholder the message is filed under) is the earlier version's. And the paragraph's opening line
// says a row for a cut part names a part that is in the archive, which a row for a message's TEXT cut
// short (no file, the page shows what could be decoded) is not. The README says each as the row does.
func TestReadmeSaysWhatAStubAndACutTextAre(t *testing.T) {
	text := strings.Join(strings.Fields(archiveReadme), " ")

	if strings.Contains(text, "fidelity-fallback or identity-stub means") {
		t.Error("README.txt still gives one meaning to fidelity-fallback and identity-stub")
	}
	for _, want := range []string{
		"A row of kind fidelity-fallback means the page shows the message as an earlier version of the tool read it",
		"A row of kind identity-stub means an earlier version of the tool could not read the message at all and filed it under a placeholder; the page shows the message as the tool that wrote it read it",
		"A row for a message's text that was cut short or damaged names no file: the page shows what could be decoded of the text",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("README.txt lacks %q", want)
		}
	}

	// The README says of the stub what the row says: the page is this version's reading.
	if why := state.RecordRowWhy(state.GapKindIdentityStub); !strings.Contains(why, "the page shows what this version reads") {
		t.Fatalf("the identity-stub row no longer says the page shows what this version reads: %q", why)
	}
}
