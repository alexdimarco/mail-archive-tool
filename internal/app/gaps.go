package app

import (
	"mail-archive-tool/internal/state"
)

// GapRemedy is what to do about a capture gap, keyed by its reason, the kind of
// source it came from (state.SourceGraph, SourcePST, SourceOutlookScratch,
// SourceLocal) and the errno class of its cause (state.ErrnoPerm, ErrnoIO,
// ErrnoSpace, ErrnoTooLong; "" for any other). It is the ONE table the run
// summary, `status` and the report share, exported so the extraction workspace's
// copy export words the same Microsoft error the same way (FC20, FC23(6)); a
// reason that is the archive's destination ("could not be written") names the
// archive location, never "read". The table itself lives in internal/state,
// below both this package and health, which must not import app.
func GapRemedy(reason, sourceKind, errnoClass string) string {
	return state.GapRemedy(reason, sourceKind, errnoClass)
}

// gapBannerCount is the count the root index.html is generated with and stamped
// with (§4.10, FC24): what the manifest says the source held that this archive
// may not — ledger gaps that are missing (an overflow entry for the items it
// counts), partly-read records and unparsed records. Every pages.Generate caller
// passes it, so a run, a Graph run, reindex and rebuild agree.
func gapBannerCount(m *state.Manifest) int {
	return m.MissingGapCount() + m.UnreadableCount() + m.UnparsedCount()
}
