package export

import "testing"

// The extraction workspace's slice E5 lands catalog-first (design
// docs/design-extraction-workspace.md §4.11, §10): the row starts here as a skip
// encoder and is replaced by its real test when RenderContext.AttachDirName is built.

// covers: MA-525, R7, R19, R29
func TestArchiveRendersAreUnchangedByAttachDirName(t *testing.T) {
	t.Skip("MA-525: RenderContext.AttachDirName and the guarded 'attachments are inside the .eml file' sentence are missing; lands in EW-E5")
}
