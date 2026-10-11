package app

import "testing"

// The extraction workspace's slice E4 (the archive source, design
// docs/design-extraction-workspace.md §4.3, §4.6, §10) lands catalog-first: each row
// starts here as a skip encoder and is replaced by its real test as the piece it
// covers is built. What stays here at the end is what a later slice or an owner
// decision closes, each with the row it cites.

// covers: MA-420, R20, R29, R4, S34, S52
func TestArchiveSourceOriginalsAndTheArchivedPageGate(t *testing.T) {
	t.Skip("MA-420: the archive source adapter, its gates and the archived-page gate are missing; lands in EW-E4")
}

// covers: MA-510, R29, R5, R20, S25, S52
func TestArchiveExportTakesNoLockBesideACapture(t *testing.T) {
	t.Skip("MA-510: the archive source adapter is missing, so there is no lock-free archive export to run beside a capture; lands in EW-E4")
}

// covers: MA-435, R20, R29, S52
func TestArchiveSourceTreeCountsAndList(t *testing.T) {
	t.Skip("MA-435: the archive snapshot, its tree and index.ListFolder are missing; lands in EW-E4")
}

// covers: MA-436, R9, R20, R29, S52
func TestArchiveIsNeverWrittenByAnExport(t *testing.T) {
	t.Skip("MA-436: the archive source is missing, so there is no archive export whose bytes could be compared; lands in EW-E4")
}
