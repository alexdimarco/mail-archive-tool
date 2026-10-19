package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The extraction workspace's slice E2 (the copy-export engine, design
// docs/design-extraction-workspace.md §9 and §10) lands catalog-first: each row
// starts here as a skip encoder and is replaced by its real test as the engine
// piece it covers is built. What stays here at the end is what a later slice or an
// owner decision closes, each with the row it cites.

// covers: R4
// The archive writer's own names keep F75 (bidi controls), F91 (the 255-byte segment
// limit) and F111 (the reserved device names SanitizeSegment misses: COM¹, CONIN$,
// "CON .txt"): export.Exporter still passes an RLO control, a 300-byte CJK segment
// and those names through util.SanitizeSegment unchanged. The copy export has its own,
// stricter pass (util.ExportSegment, ruling Q15, MA-417), so it needs no archive
// migration; fixing the archive's names renames directories and changes local keys,
// so it needs a migration of its own (fault-isolation §3 N1). An open gap, not
// accepted, which this encoder keeps open until that migration lands.
func TestArchiveWriterNamesKeepF75F91F111(t *testing.T) {
	t.Skip("R4: the archive writer's names keep F75/F91/F111 (ruling Q15, extraction design §4.18); the fix needs its own migration, owned outside the extraction workspace; the copy export's naming pass (util.ExportSegment) closes MA-417 meanwhile")
}

// covers: MA-419, R29, R17, S52
// With Readable HTML on, one message's MIME and its decoded attachments are held, and
// its readable copy is made only within the render bound (32 MiB of predicted rendered
// HTML, which MA-509 pins): the on half of MA-419's two modes.
//
// Six messages of 12 MiB each (a 9 MiB attachment, base64-encoded) run with Readable HTML
// on: every .eml is the served bytes and carries its readable copy, and the heap grows by
// no more than a few messages' worth while the run lasts, because the one message being
// rendered is the only one held (its .eml read back into one buffer, its parse, its page);
// a run that kept each message's bytes would hold all six, and the bound sits between them.
func TestCopyExportReadableHTMLHoldsOneMessage(t *testing.T) {
	const n = 6
	e := newCEEnv(t)
	msgs := ceMsgs(n)
	body := map[string][]byte{}
	for i := range msgs {
		id := msgs[i].SourceID
		pdf := e5Image(9 << 20)
		pdf[0] = byte(i) // the messages differ
		body[id] = e5Related(id, "held "+id, "<p>marker-text</p>", nil, string(pdf))
	}
	if len(body[msgs[0].SourceID]) < 12<<20 || len(body[msgs[0].SourceID]) > 13<<20 {
		t.Fatalf("a message is %d bytes, want about 12 MiB", len(body[msgs[0].SourceID]))
	}
	var res *CopyResult
	if peak := peakHeap(func() { res, _ = e5Run(t, e, msgs, func(id string) []byte { return body[id] }) }); peak > 80<<20 {
		t.Errorf("the heap grew by %d MiB over %d messages of 12 MiB: more than the one being rendered is held", peak>>20, n)
	}
	if res.Counts.Exported != n || res.Counts.OriginalOnly != 0 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	for i, line := range e5Files(t, e, res) {
		if len(line.Files) != 2 || line.Files[1].Role != roleDerivedHTML {
			t.Fatalf("message %d: %+v", i, line)
		}
		orig, err := os.ReadFile(filepath.Join(res.Root, filepath.FromSlash(line.Files[0].Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(orig, body[msgs[i-1].SourceID]) {
			t.Errorf("message %d: the .eml is not the served bytes", i)
		}
		if line.Files[1].Size > 64<<10 {
			t.Errorf("message %d: its readable copy is %d bytes: the attachment is not in the page", i, line.Files[1].Size)
		}
	}
}
