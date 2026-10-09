package app

import "testing"

// covers: MA-540, R10, R17, R18, R21, S37
// SG's stops on the shipped capture path: a nextLink repeat, the page cap and a
// stalled $value each fail mailbox 1 with a typed error, record no {k,gone},
// walk mailbox 2 and record the run failed, never cancelled.
func TestRunGraphGuardStopsFailTheMailbox(t *testing.T) {
	t.Skip("MA-540: the nextLink guard, MIMETo's typed stops and RunGraph's own ctx.Err() test do not exist yet; lands in FI-SG")
}
