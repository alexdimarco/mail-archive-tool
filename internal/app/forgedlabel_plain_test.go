package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1
// ESC-MS2-3 (MS2-AI1, the plain-name facet). An attachment whose file name is one of the
// record's reserved labels ("unparsed", "unreadable:body", "body", "unknown", "superseded")
// is a gap of that name and nothing else: status does not call the message unparsed and the
// report row is an empty-attachment row. The labels share one namespace with sender-chosen
// names (state/gapclass.go), which predates MF-S2; separating them changes the labels
// records already carry, so it needs an owner decision.
func TestAnAttachmentNamedLikeAReservedLabelIsOnlyAnAttachment(t *testing.T) {
	t.Skip("ESC-MS2-3: an empty attachment named like a reserved record label (unparsed, unreadable:body) is read as that label; the namespace is shared by design and separating it changes the labels existing records carry (predates MF-S2; invariant R1, MA-348)")
	src := filepath.Join(tmpDir(t), "Inbox")
	forgedNameMessage(t, src, "unparsed")
	out := tmpDir(t)
	r, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	rep, _ := readReport(t, out)
	if r.Unparsed != 0 || strings.Contains(rep, "unparsed-message") {
		t.Errorf("an attachment named \"unparsed\" makes the message an unparsed one: Unparsed %d\n%s", r.Unparsed, rep)
	}
}
