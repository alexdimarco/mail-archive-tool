package app

import "testing"

// The extraction workspace's slice E5 (Readable HTML, design
// docs/design-extraction-workspace.md §4.11, §10) lands catalog-first: each row starts
// here as a skip encoder and is replaced by its real test as the piece it covers is
// built. What stays here at the end is what a later slice or an owner decision closes,
// each with the row it cites.

// covers: MA-423, R19, R7, R29, S21, S52
func TestReadableHTMLComesFromTheSanitizingRenderer(t *testing.T) {
	t.Skip("MA-423: the readable copy (the read-back hash check, the render through export.RenderWith) is missing; lands in EW-E5")
}

// covers: MA-509, R29, R10, R1, S52
func TestAReadableCopyThatCannotBeMadeKeepsTheOriginal(t *testing.T) {
	t.Skip("MA-509: the render panic guard and the 32 MiB render bound (RR2b-C2) are missing; lands in EW-E5")
}

// covers: MA-513, R29, R12
func TestAMissingReadableCopyMakesARunPartialNotFailed(t *testing.T) {
	t.Skip("MA-513: the engine writes no derived file yet, so none can be requested and missing; lands in EW-E5")
}
