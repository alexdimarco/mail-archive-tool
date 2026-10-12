package app

import "testing"

// The cases of the rows FI-S2b lands (design-fault-isolation §4.5, §9): the local
// readers' per-file and per-folder isolation and their containment under R25.

// covers: MA-321, R10, R15, R1, S46
func TestMaildirFileFaultsCostOneFile(t *testing.T) {
	t.Skip("MA-321: a maildir file that cannot be read is a file-unreadable ledger gap and costs that file only; lands in FI-S2b")
}

// covers: MA-322, R25, R10, R4, S47
func TestLocalStoresAreReadUnderContainment(t *testing.T) {
	t.Skip("MA-322: a FIFO, a link out of the store and a directory link are refused and recorded, never read or followed; lands in FI-S2b")
}
