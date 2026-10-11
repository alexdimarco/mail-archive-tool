package app

import "testing"

// The rows FI-S3 lands (fault-isolation §9, §10): Graph isolation. Each is a skip
// encoder here, catalog-first; the slice's commits turn them into real tests.

// covers: MA-310, R1, R10, R17, S46
func TestGraphPoisonItemCostsOnlyThatItem(t *testing.T) {
	t.Skip("MA-310: a Graph $value failure (500 ErrorMimeContentConversionFailed) on one message is a fetch-failed gap that leaves every other message archived and no record for the poison; lands in FI-S3")
}

// covers: MA-311, R1, R17, R2, S46
func TestGraphLedgerIsInertAndHeals(t *testing.T) {
	t.Skip("MA-311: a Graph gap is retried by the next run, closes as recovered when the item is captured, and a Terminal record seeded for the poison shows why a gap is never a record; lands in FI-S3")
}

// covers: MA-312, R21, R1, S37, S46
func TestGraphFaultsNeverSweepAPresentMessageGone(t *testing.T) {
	t.Skip("MA-312: a Graph fetch or listing fault defers gone-detection for exactly the records it could hide (FC4, OR10) and never marks a present message gone; lands in FI-S3")
}

// covers: MA-313, R5, R12, R18, S46
func TestGraphAbortSemanticsAndBreaker(t *testing.T) {
	t.Skip("MA-313: auth faults and a cancel stop the mailbox without a gap; the rate and time breaker, its control probe, the known-poison exemption, the run-level breaker and the Retry-After clamp (FC10, FC11); lands in FI-S3")
}

// covers: MA-501, R1, R6, R10, R21, S46
func TestGraphUnwritableFolderCostsOneFolder(t *testing.T) {
	t.Skip("MA-501: a Graph folder whose output directory cannot be created is a folder-unwritable gap, its later refs are not downloaded, and in Full mode they are faults for the sweep; lands in FI-S3")
}

// covers: MA-502, R1, R17, S46
func TestGraphLedgerNeverStopsACapture(t *testing.T) {
	t.Skip("MA-502: a manifest seeded with a gap whose graph: locator matches a healthy item does not stop its capture, and the gap closes as recovered; lands in FI-S3")
}
