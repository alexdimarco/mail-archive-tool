package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-535, MA-412, R12, R29
// F8 (friction review of EW-E6): the preview says what Extract attachments adds to the size, and a destination with
// too little room is told that unticking Extract attachments is a way out, with what it would save, in the
// preview's warning and in the refusal Start gives (the same words, R-g2: what, why, remedy). The positive
// twin first: the same selection without the option adds nothing, and its refusal is the words it always was.
func TestThePreviewAndTheFullDiskRefusalSayWhatExtractAttachmentsAdds(t *testing.T) {
	msgs := ceMsgs(3)
	for i := range msgs {
		msgs[i].SizeHint = 10000
	}
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := func(att, asZip bool) *CopyPlan {
		t.Helper()
		p, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: msgs, Dest: dest, Options: CopyOptions{PreserveFolders: true, Attachments: att, AttachmentsAsZip: asZip}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	off := plan(false, false)
	if off.AttachBytes != 0 {
		t.Errorf("a plan without the option says it adds %d bytes", off.AttachBytes)
	}
	for _, asZip := range []bool{true, false} {
		on := plan(true, asZip)
		if want := int64(3 * (10000 * 4 / 5)); on.AttachBytes != want || on.EstimateBytes != off.EstimateBytes+want {
			t.Errorf("zip %v: the plan says the option adds %d bytes and the estimate is %d (%d without it), want +%d", asZip, on.AttachBytes, on.EstimateBytes, off.EstimateBytes, want)
		}
	}

	e.SetFreeBytes(func(string) (uint64, error) { return 1000, nil }, nil)
	defer e.SetFreeBytes(nil, nil)
	if w := strings.Join(plan(false, false).Warnings, "\n"); !strings.Contains(w, "Free some space or choose another destination.") || strings.Contains(w, "Extract attachments") {
		t.Errorf("without the option the warning is the one it always was: %q", w)
	}
	on := plan(true, true)
	want := "Free some space or choose another destination, or untick Extract attachments (it accounts for about " + humanBytes(on.AttachBytes) + " of that)."
	if w := strings.Join(on.Warnings, "\n"); !strings.Contains(w, want) {
		t.Errorf("the preview's warning lacks %q: %q", want, w)
	}

	// Start refuses with the same words, and writes nothing
	_, err := ceRunPlan(t, e, on, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("Start was not refused: %v", err)
	}
	assure.Refused(t, 1, ref.Message, assure.Code(1), assure.Names(want, "nothing was written", "free, but this export needs"))
	if ref.ID != "R-g2" {
		t.Errorf("the refusal is %s, want R-g2", ref.ID)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("the refused start wrote into the destination: %v", entries)
	}
}
