package state

import (
	"strings"
	"testing"
)

// covers: MA-348, R1, R7
// F6 of the MF-S2 friction review (cell CA-3). A corrupt-part row whose detail says the
// TEXT of the message was cut (a body, not an attachment) must not tell the reader that the
// readable start is in the attachments zip under a name ending .partial: a body has neither.
// It says the page shows the readable start. The positive twin: a cut ATTACHMENT's row still
// names the zip and the .partial file, as it did.
func TestACutBodyRowDoesNotPointAtAPartialFileOrZip(t *testing.T) {
	attachment := "contract.pdf.partial — the data is cut short or damaged after 519 bytes: only the readable start is kept"
	body := CorruptBodyDetailPrefix + "1 is cut short or damaged after 24 bytes: only the readable start is shown"

	for _, class := range []string{ClassFillable, ClassTerminal} {
		why, remedy := RecordRowWhyFor(GapKindCorruptPart, attachment), RecordRowRemedyFor(GapKindCorruptPart, class, attachment, OriginalUnstated)
		for _, want := range []string{".partial", "attachments zip"} {
			if !strings.Contains(why+" "+remedy, want) {
				t.Fatalf("positive twin (%s): the row for a cut attachment no longer says %q:\nwhy: %s\nwhat to do: %s", class, want, why, remedy)
			}
		}

		why, remedy = RecordRowWhyFor(GapKindCorruptPart, body), RecordRowRemedyFor(GapKindCorruptPart, class, body, OriginalUnstated)
		for _, bad := range []string{".partial", "zip"} {
			if strings.Contains(why, bad) || strings.Contains(remedy, bad) {
				t.Errorf("%s: a cut body is said to be in a %q file:\nwhy: %s\nwhat to do: %s", class, bad, why, remedy)
			}
		}
		if !strings.Contains(why, "page") || !strings.Contains(remedy, "page") {
			t.Errorf("%s: a cut body's row does not say the page shows its readable start:\nwhy: %s\nwhat to do: %s", class, why, remedy)
		}
		if !strings.Contains(remedy, "the original is kept") {
			t.Errorf("%s: the remedy %q does not say the original is kept", class, remedy)
		}
	}
	// The kind-only words are the attachment's, unchanged.
	if RecordRowWhy(GapKindCorruptPart) != RecordRowWhyFor(GapKindCorruptPart, "") {
		t.Error("RecordRowWhy and RecordRowWhyFor with no detail disagree")
	}
}

// covers: MA-348, R1, R7
// F14 of the MF-S2 friction review (cells IN-1, OW-5, OW-2). Every parse-gap row ends "the
// original is kept", and an inheritor with the folder alone reads that as a file beside the page.
// It says where: in the source when the archive was made without -raw (no .eml was made), and as
// the .eml beside the page when there is one. The words that did not say where (Unstated, what the
// kind-only functions give) are unchanged.
func TestAParseGapRowSaysWhereTheOriginalIs(t *testing.T) {
	for kind, classes := range map[string][]string{
		GapKindUndecodedPart: {ClassTerminal}, GapKindStructureLimit: {ClassTerminal},
		GapKindFidelityFallback: {ClassTerminal}, GapKindIdentityStub: {ClassTerminal},
		GapKindCorruptPart: {ClassFillable, ClassTerminal}, GapKindUnsplitMultipart: {ClassFillable, ClassTerminal},
	} {
		for _, class := range classes {
			unstated := RecordRowRemedyFor(kind, class, "", OriginalUnstated)
			if unstated != RecordRowRemedy(kind, class) || !strings.HasSuffix(unstated, "the original is kept") {
				t.Errorf("%s/%s: the unstated remedy %q is not the kind-only one, ending the original is kept", kind, class, unstated)
			}
			inSource := RecordRowRemedyFor(kind, class, "", OriginalInSource)
			if !strings.Contains(inSource, "the original is kept in the source") || !strings.Contains(inSource, "no .eml") || strings.Contains(inSource, ".eml beside the page") {
				t.Errorf("%s/%s: without an .eml the remedy %q does not say the original is only in the source", kind, class, inSource)
			}
			beside := RecordRowRemedyFor(kind, class, "", OriginalBesidePage)
			if !strings.Contains(beside, "the original is kept as the .eml beside the page") || strings.Contains(beside, "no .eml") {
				t.Errorf("%s/%s: with an .eml the remedy %q does not say it is beside the page", kind, class, beside)
			}
		}
	}
}

// covers: MA-348, R1, R7
// F3 of the MF-S2 friction review (cells OW-3, OW-11, CA-2). The fillable remedy of a cut or
// boundary-less message is not only "download it in your mail app", which is the remedy of an IMAP
// cache: for a damaged mailbox file read in full it is a complete export. The row says both, still
// leading with the download the existing words promise.
func TestAFillableParseGapRowNamesTheCompleteExportToo(t *testing.T) {
	for _, kind := range []string{GapKindCorruptPart, GapKindUnsplitMultipart} {
		r := RecordRowRemedyFor(kind, ClassFillable, "", OriginalInSource)
		if !strings.Contains(r, "download the complete message") || !strings.Contains(r, "mailbox file") || !strings.Contains(r, "complete export") {
			t.Errorf("%s: the fillable remedy %q does not name a complete export for a damaged mailbox file", kind, r)
		}
		if t2 := RecordRowRemedyFor(kind, ClassTerminal, "", OriginalInSource); strings.Contains(t2, "complete export") {
			t.Errorf("%s: a terminal remedy %q promises an export that would not be read", kind, t2)
		}
	}
}
