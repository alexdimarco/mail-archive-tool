package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-535, MA-424, R12
// Re-verification of EW-E6's friction fix F8, the premise of the page's Extract attachments line (desktop
// TestThePreviewLineGivesTheAttachmentsShareOnlyWhenThereIsOne): a selection whose sizes are all unknown has
// Extract attachments on and adds no bytes to the estimate, so the plan's figure for the option is 0 and the
// page must not print it as "about 0 B of it".
func TestAPlanOfMessagesOfUnknownSizeAddsNoBytesForExtractAttachments(t *testing.T) {
	msgs := ceMsgs(2)
	for i := range msgs {
		msgs[i].SizeHint = -1
	}
	e, src := newCEEnv(t), newFakeSrc()
	p, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: msgs, Dest: t.TempDir(), Options: CopyOptions{Attachments: true, AttachmentsAsZip: true}})
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownSize != 2 || p.AttachBytes != 0 || !p.Options.Attachments {
		t.Errorf("unknown sizes %d, the option adds %d bytes, attachments %v: want 2, 0, true", p.UnknownSize, p.AttachBytes, p.Options.Attachments)
	}
}

// covers: MA-535, MA-412, MA-424, R12, R29
// Re-verification of EW-E6's friction fix F8 (X2: a remedy with no way to do it): a destination with too little
// room is told it can untick Extract attachments, but a retry keeps the choices of the export it continues and
// the workspace page locks the box (F10), so the preview of a retry and Start's refusal for it offered a remedy
// the page does not allow. A retry's warning and refusal say only what can be done there: free some space or
// choose another destination. The positive twin first: a new export of the same messages, with the same room,
// is still told it can untick the option, with what that saves.
func TestARetryWithTooLittleRoomIsNotToldToUntickExtractAttachments(t *testing.T) {
	msgs := ceMsgs(3)
	for i := range msgs {
		msgs[i].SizeHint = 10000
	}
	e, src := newCEEnv(t), newFakeSrc()
	failIDs(src, map[string]bool{"M00002": true})
	p1 := cePlan(t, e, src, msgs)
	p1.Options.PreserveFolders, p1.Options.Attachments, p1.Options.AttachmentsAsZip = true, true, true
	first := mustRun(t, e, p1, src)
	if first.Outcome != CopyOutcomePartial {
		t.Fatalf("the first export: %s, %+v", first.Label, first.Counts)
	}
	failIDs(src, nil)

	e.SetFreeBytes(func(string) (uint64, error) { return 1000, nil }, nil)
	defer e.SetFreeBytes(nil, nil)

	// the twin: a new export of the same messages is told it can untick the option
	fresh, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: msgs, Dest: p1.Dest,
		Options: CopyOptions{PreserveFolders: true, Attachments: true, AttachmentsAsZip: true}})
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(fresh.Warnings, "\n"); !strings.Contains(w, "or untick Extract attachments (it accounts for about "+humanBytes(fresh.AttachBytes)+" of that)") {
		t.Fatalf("the new export's warning lost the remedy it had: %q", w)
	}

	// the retry of the failed message keeps the option, and cannot untick it
	in, err := e.RetryInput(first.RunID, "failed", "alice@contoso.org")
	if err != nil {
		t.Fatal(err)
	}
	in.Dest = p1.Dest
	retry, err := e.PlanCopyExport(*in)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Options.Attachments || retry.Chain.RetryOf == "" || retry.AttachBytes == 0 {
		t.Fatalf("the retry's plan: attachments %v, retry of %q, option adds %d", retry.Options.Attachments, retry.Chain.RetryOf, retry.AttachBytes)
	}
	w := assure.Reached(t, strings.Join(retry.Warnings, "\n"), "the retry preview's warning for too little room")
	if !strings.Contains(w, "Free some space or choose another destination.") || strings.Contains(w, "untick") {
		t.Errorf("the retry's warning offers a remedy the page locks, or lost its own: %q", w)
	}

	// Start refuses with the same words, and writes nothing
	before, _ := os.ReadDir(p1.Dest)
	_, err = ceRunPlan(t, e, retry, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("the retry was not refused: %v", err)
	}
	assure.Refused(t, 1, ref.Message, assure.Code(1), assure.Names("Free some space or choose another destination.", "nothing was written", "free, but this export needs"))
	if ref.ID != "R-g2" || strings.Contains(ref.Message, "untick") {
		t.Errorf("the retry's refusal is %s %q", ref.ID, ref.Message)
	}
	if after, _ := os.ReadDir(p1.Dest); len(after) != len(before) {
		t.Errorf("the refused retry wrote into the destination: %d entries became %d", len(before), len(after))
	}
}
