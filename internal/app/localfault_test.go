package app

import "testing"

// covers: MA-318, R1, R3, R7, R10, S46
func TestUnreadablePSTNodesAreLedgerGapsNotRecords(t *testing.T) {
	t.Skip("MA-318: an unreadable PST node still becomes a stub record (or aborts the store); lands in FI-S2a")
}

// covers: MA-319, R1, R6, R13, S46
func TestRecoveryAndClosureOfPSTGaps(t *testing.T) {
	t.Skip("MA-319: nothing records a PST gap yet, so none can recover or close; lands in FI-S2a")
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
func TestFolderUnwritableAndRunFatalOnTheLocalPath(t *testing.T) {
	t.Skip("MA-320: runFile still returns every Export error and ends the source; lands in FI-S2a")
}

// covers: MA-324, R1, R17, S46
func TestLedgerIsInertOnLocalSources(t *testing.T) {
	t.Skip("MA-324: no local walker records or closes a gap yet; lands in FI-S2a")
}
