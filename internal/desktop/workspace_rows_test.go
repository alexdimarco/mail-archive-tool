package desktop

import "testing"

// The extraction workspace's slice E3 (the desktop pages and endpoints; design
// docs/design-extraction-workspace.md §9 and §10) lands catalog-first: each row
// starts here as a skip encoder and is replaced by its real test as the piece it
// covers is built. What stays here at the end is what a later slice or an owner
// decision closes, each with the row it cites.

// covers: MA-437, R29, S52
// Retry builds a new preview from the run record into the next part of the set.
func TestRetryBuildsFromTheRunRecord(t *testing.T) {
	t.Skip("MA-437: the retry endpoint does not exist yet; lands in EW-E3")
}

// covers: MA-438, R12, S52
// Every Microsoft 365 and destination refusal carries its what, why and remedy, and
// no token, Graph URL or crash text.
func TestRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	t.Skip("MA-438: the workspace's refusals do not exist yet; lands in EW-E3")
}

// covers: MA-446, R12, S41, S52
// The Export activity page shows the running export, the history and the actions.
func TestExportActivityPage(t *testing.T) {
	t.Skip("MA-446: the Export activity page does not exist yet; lands in EW-E3")
}

// covers: MA-448, R29, R12, S52
// Reconciliation runs off the request path, once per launch, and only downgrades.
func TestReconciliationIsOffTheRequestPath(t *testing.T) {
	t.Skip("MA-448: the background reconciler does not exist yet; lands in EW-E3")
}
