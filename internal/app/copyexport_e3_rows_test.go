package app

import "testing"

// The extraction workspace's slice E3 rows whose test lives with the engine seams
// E3 adds beside the desktop's pages (design docs/design-extraction-workspace.md §9
// and §10). Each starts here as a skip encoder and is replaced by its real test as
// the seam it covers is built.

// covers: MA-414, R29, R4, R12, S52
// A destination missing, a file, relative, equal to or inside the configured archive
// (also through a symlinked parent), or an alias of the archive or of a folder inside
// it by identity, is refused R-e or R-f; a view whose identity cannot be compared is
// R-f2; each with nothing written.
func TestDestinationRefusals(t *testing.T) {
	t.Skip("MA-414: the destination check (R-f by identity, R-f2) does not exist yet; lands in EW-E3")
}

// covers: MA-434, R4, R29, R12
// Create folder makes exactly one sanitized directory through the handle its checks
// opened, and refuses an unsafe name or a parent inside the archive.
func TestCreateFolderMakesExactlyOneChecked(t *testing.T) {
	t.Skip("MA-434: Create folder does not exist yet; lands in EW-E3")
}

// covers: MA-520, R29, S52
// Check this export reports a flipped byte, a missing file and a planted file, and
// compares the sealed fingerprint; Check and finish refuses with the marker kept.
func TestCheckThisExportReportsWhatChanged(t *testing.T) {
	t.Skip("MA-520: Check this export does not exist yet; lands in EW-E3")
}

// covers: MA-521, R29, S52
// A retry of a retry is the next part, every manifest carries the chain, and the set
// fingerprint is over the parts' SUMS in seal order.
func TestRetryChainIsNamedAndFingerprinted(t *testing.T) {
	t.Skip("MA-521: the retry part's naming and the page's set fingerprint do not exist yet; lands in EW-E3")
}

// covers: MA-430, R29, R12, S52
// The journal, the activity labels and Open folder follow the sealed line, the marker
// and the identity tuple.
func TestActivityLabelsFollowTheSealedLine(t *testing.T) {
	t.Skip("MA-430: the activity history and its labels do not exist yet; lands in EW-E3")
}
