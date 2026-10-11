package desktop

import "testing"

// The extraction workspace's slice E4, the desktop's half (design
// docs/design-extraction-workspace.md §4.2, §4.3, §7.4, §10): each row starts here as
// a skip encoder (catalog-first) and is replaced by its real test as the endpoint or
// page it covers is built.

// covers: MA-507, R29, R12
func TestArchiveSelectionIsBoundToItsArchive(t *testing.T) {
	t.Skip("MA-507: the archive source's selection and its R-l2 binding are missing; lands in EW-E4")
}

// covers: MA-512, R12, S52
func TestArchiveRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	t.Skip("MA-512: R-o, R-o2, R-o3, R-o4, R-p and R-l2 are missing; lands in EW-E4")
}

// covers: MA-523, R12, S41
func TestSettingsSaveAsksBeforeRetargetingTheWeeklyBackup(t *testing.T) {
	t.Skip("MA-523: the Settings-save guard (XC22) is missing; lands in EW-E4")
}

// covers: MA-531, R29, R9, R20, R12, S52
func TestAnotherArchiveFolderIsReadOnly(t *testing.T) {
	t.Skip("MA-531: the second archive source (W1) is missing; lands in EW-E4")
}
