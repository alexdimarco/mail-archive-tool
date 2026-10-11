package app

import "testing"

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
func TestCopyExportReadableHTMLHoldsOneMessage(t *testing.T) {
	t.Skip("MA-419: Readable HTML is slice E5's (the render bound RR2b-C2, MA-509); until it lands the engine refuses the option and runs with it off, which TestCopyExportM365OriginalsAreByteExact covers")
}
