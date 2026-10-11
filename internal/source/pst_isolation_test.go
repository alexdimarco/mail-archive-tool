package source

import "testing"

// covers: MA-315, R10, R1, S46
func TestPSTMessageRowCorruptionCostsOneItem(t *testing.T) {
	t.Skip("MA-315: the reader still walks go-pst's MessageIterator, whose sticky error ends the store; lands in FI-S2a")
}

// covers: MA-316, R10, R6, R1, S46
func TestPSTFolderTableFaultsCostOneFolder(t *testing.T) {
	t.Skip("MA-316: the reader still ends the store on a folder-table fault; lands in FI-S2a")
}

// covers: MA-317, R10, S47
func TestPSTFolderCycleAndDepthAreBounded(t *testing.T) {
	t.Skip("MA-317: the reader walks sub-folders with no visited set and no depth cap; lands in FI-S2a")
}
