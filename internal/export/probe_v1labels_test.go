package export

import (
	"io"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-334, R1, R26
// MS2-AI4. A record an earlier build left fillable names its missing attachment by the label
// that build gave it (v1's: "attachment-0" for a part with no usable name); the walker may
// read the same part's Content-Disposition more leniently and call it something else
// ("my report.pdf"). The fill probe matches the old labels against the labels of what v1
// reads now as well (the message's V1Inventory, design MC34), so the record fills when the
// attachment's bytes arrive, whichever parser named it.
func TestProbeImprovesMatchesV1LabelsAsWellAsTheWalkersOwn(t *testing.T) {
	write := func(s string) func(io.Writer) (int64, error) {
		return func(w io.Writer) (int64, error) { n, err := w.Write([]byte(s)); return int64(n), err }
	}
	now := func(size int64) *model.Message {
		return &model.Message{
			PlainBody:   "x",
			Attachments: []model.Attachment{{Filename: "my report.pdf", WriteTo: write("the bytes")}},
			V1Inventory: []model.V1Item{{Index: 0, Label: "attachment-0", Size: size}},
		}
	}
	if !probeImproves(now(9), []string{"attachment-0"}) {
		t.Error("the attachment v1 calls attachment-0 now has bytes, and the record's old label did not improve")
	}
	if probeImproves(now(0), []string{"attachment-0"}) {
		t.Error("v1 still reads attachment-0 with no bytes, and the record improved")
	}
	if probeImproves(now(9), []string{"attachment-7"}) {
		t.Error("a label neither parser has improved the record")
	}
	// The walker's own label still matches, as before.
	if !probeImproves(now(0), []string{"my report.pdf"}) {
		t.Error("the walker's label with bytes did not improve the record")
	}
}
