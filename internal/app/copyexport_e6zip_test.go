package app

import "testing"

// Extract attachments in zip mode (owner ruling 2026-10-12: one zip per message, the default,
// or loose files; catalog rows MA-533, MA-534, MA-535). Catalog-first: each case starts here as
// a skip encoder citing its row and is replaced by its real test as the piece it covers is built.

// covers: MA-533, R4, R1, R29, S52
func TestZipModeWritesOneZipPerMessageAndNoLooseFiles(t *testing.T) {
	t.Skip("MA-533: zip mode is not built yet")
}

// covers: MA-533, R4, R29
func TestZipModeEntriesAreNamedLikeLooseFilesAndTheZipIsReproducible(t *testing.T) {
	t.Skip("MA-533: zip mode is not built yet")
}

// covers: MA-533, R1
func TestZipModeNotesAnEmptyFailedOrKeptPart(t *testing.T) {
	t.Skip("MA-533: zip mode is not built yet")
}

// covers: MA-533, R1, R29
func TestZipModeChecksFreeSpaceAndObservesCancelAndAbandon(t *testing.T) {
	t.Skip("MA-533: zip mode is not built yet")
}

// covers: MA-533, R7
func TestZipModeReadableCopyLinksTheZip(t *testing.T) {
	t.Skip("MA-533: zip mode is not built yet")
}

// covers: MA-534, R4, R1
func TestZipModeCopiesAnArchivedZipByteExactAndNothingMore(t *testing.T) {
	t.Skip("MA-534: zip mode is not built yet")
}

// covers: MA-534, MA-424
func TestLooseModeIsUnchangedByTheZipModeSwitch(t *testing.T) {
	t.Skip("MA-534: zip mode is not built yet")
}

// covers: MA-535, R12
func TestThePlanSizesAndPlacesAZipModeExport(t *testing.T) {
	t.Skip("MA-535: zip mode is not built yet")
}

// covers: MA-535, R1
func TestReadmeSaysWhatTheAttachmentsZipHolds(t *testing.T) {
	t.Skip("MA-535: zip mode is not built yet")
}
