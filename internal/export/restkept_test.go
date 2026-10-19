package export

import (
	"io"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-348, MA-349, R1, R10
// F5 of the MF-S2 friction review (cell OW-10), the upgrade half. A record an earlier build left
// with a fillable missing-body gap closes on the next re-read when this build finds the text kept
// as received past a structure bound (it no longer records the gap there: no download changes a
// bound). A message with no body and nothing kept unread still does not improve it, so the fill
// probe is not "always true".
func TestAMissingBodyClosesWhenTheTextIsKeptPastAStructureBound(t *testing.T) {
	blob := func(kind string) model.Attachment {
		return model.Attachment{Filename: "remaining-parts.undecoded", GapKind: kind,
			WriteTo: func(w io.Writer) (int64, error) { n, err := w.Write([]byte("rest")); return int64(n), err }}
	}
	old := []string{state.MissingBody}
	if probeImproves(&model.Message{}, old) {
		t.Fatal("positive twin: a message with no body and nothing kept unread improved a missing-body record")
	}
	if !probeImproves(&model.Message{Attachments: []model.Attachment{blob(state.GapKindStructureLimit)}}, old) {
		t.Error("the text kept past a structure bound did not close the old missing-body gap")
	}
	// Only a bound keeps text unread by design: a part cut short is a fillable gap of its own.
	if probeImproves(&model.Message{Attachments: []model.Attachment{blob(state.GapKindCorruptPart)}}, old) {
		t.Error("a corrupt part closed the old missing-body gap")
	}
	if hasBody(&model.Message{}) || !restKeptUnread(&model.Message{Attachments: []model.Attachment{blob(state.GapKindStructureLimit)}}) {
		t.Error("restKeptUnread / hasBody changed meaning")
	}
}
