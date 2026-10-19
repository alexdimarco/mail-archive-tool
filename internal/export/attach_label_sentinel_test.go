package export

import (
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-355, MA-435, R28, R20
// E4-AI10, an encoder. A gap label is the attachment's file name, and the state layer's own gap labels share
// the label space: an attachment literally named "unknown" (state.UnknownSentinel), "body"
// (state.MissingBody) or "unparsed" (state.LabelUnparsed) is stored as the sentinel, so the archive source's
// export words a real, current gap as "completeness was not tracked by the version that captured this message"
// (or as the body never downloaded), which is false for a current capture. The archive source's note
// builder inherits the ambiguity and cannot resolve it: MA-355 already treats these names as "ambiguous" on
// the capture side. The remedy is a label for a real attachment that no sentinel can equal (for example
// "attachment: unknown"), which changes what a manifest records for such a file and how the capture side
// matches labels (MA-355's KEEP rules), so it is the owner's: this is the body the fix must satisfy.
func TestAnAttachmentNamedLikeASentinelHasALabelThatIsNotOne(t *testing.T) {
	t.Skip("OWNER DECISION PENDING (E4-AI10, MA-355 'attachments named body, unknown and unverified are ambiguous'): the label of an attachment named like a state-layer sentinel must not equal it; the remedy changes the labels a manifest records and the capture side's matching, not the archive source's note builder")
	for _, name := range []string{state.UnknownSentinel, state.MissingBody, state.LabelUnparsed} {
		if got := attachmentLabel(model.Attachment{Filename: name}, 0); got == state.UnknownSentinel || got == state.MissingBody || got == state.LabelUnparsed {
			t.Errorf("an attachment named %q is labelled %q, the state layer's own gap label", name, got)
		}
	}
}
