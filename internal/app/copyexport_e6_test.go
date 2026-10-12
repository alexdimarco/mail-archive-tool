package app

import "testing"

// The extraction workspace's slice E6 (Extract attachments, design
// docs/design-extraction-workspace.md §4.12, §10) lands catalog-first: each case starts
// here as a skip encoder and is replaced by its real test as the piece it covers is built.
// What stays here at the end is what an owner decision closes, each with the row it cites.

// covers: MA-424, R4, R1, R29, S52
// A Microsoft 365 message with Extract attachments on: every parsed attachment, inline images
// included, is a loose decoded file under <stem>.attachments/, role derived-attachment, hashed as
// written, with sanitized FoldKey-unique names and the parsed part's bytes.
func TestExtractedAttachmentsAreLooseDecodedFilesUnderTheStem(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R4, R29
// Hostile attachment names (.., separators, device names, bidi controls, 300 bytes of CJK) become
// distinct, creatable names inside <stem>.attachments/, and a link planted where the directory is
// is not written through.
func TestExtractedAttachmentNamesAreSanitizedAndContained(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// A zero-byte part and a part whose stream fails are entry notes, never silent, and write no file.
func TestAZeroByteOrFailedPartIsANoteNeverSilent(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// A part the walker kept as .undecoded or .partial is extracted under that name as what it is and
// noted as not the complete decoded attachment.
func TestKeptPartsAreExtractedAsWhatTheyAreAndNoted(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R29
// Each derived attachment is checked against the free space left before it is written; a part
// that does not fit is a note, and the original and the run are unharmed.
func TestDerivedAttachmentsAreCheckedAgainstFreeSpace(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R4, R1, R29, S52
// An archive record without an original: the zip is copied byte-exact and its entries are also
// extracted within the bounds; a bomb entry is skipped with a note and never expanded.
func TestArchiveZipEntriesAreExtractedWithinBounds(t *testing.T) {
	t.Skip("MA-424: zip entry extraction is not built; lands in EW-E6")
}

// covers: MA-424, R29
// An archive record that has an original extracts from its .eml read back from the destination,
// as a Microsoft 365 message does.
func TestAnArchiveRecordWithAnOriginalExtractsFromItsEml(t *testing.T) {
	t.Skip("MA-424: attachment extraction is not built; lands in EW-E6")
}

// covers: MA-424, R1
// README and the manifest say what extracted files are and name the caveats.
func TestReadmeNamesTheExtractedAttachmentsAndTheirCaveats(t *testing.T) {
	t.Skip("MA-424: the README text is not written; lands in EW-E6")
}

// covers: MA-424, R29
// The plan sizes the derived files: the estimate and the reserve grow with the option on.
func TestThePlanSizesExtractedAttachments(t *testing.T) {
	t.Skip("MA-424: the plan does not size extracted attachments; lands in EW-E6")
}

// covers: MA-424, R12
// The plan accepts the option and carries it. Held for the owner: TestCopyExportPlanIsADryRun
// pins the plan's R-h refusal of it until slice E6 (rulings §0, rule 2).
func TestThePlanAcceptsExtractAttachments(t *testing.T) {
	t.Skip("MA-424: PlanCopyExport refuses the option (R-h) and TestCopyExportPlanIsADryRun asserts that; the owner rules on that test before it changes")
}

// covers: MA-425, R1, R29
// A Latin-1 text/csv attachment extracts byte-identical to the MIME part's transfer-decoded bytes.
func TestALatin1CsvAttachmentExtractsByteIdentical(t *testing.T) {
	t.Skip("MA-425: attachment extraction is not built; lands in EW-E6")
}
