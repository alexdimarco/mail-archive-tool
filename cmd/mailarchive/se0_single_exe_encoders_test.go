package main

// SE0 single-executable slice — catalog-first skip encoders.
//
// Each test below encodes one catalog row of the single-executable design
// (docs/scenario-catalog.md MA-610..MA-641, invariants R33/S59, UX clause X17)
// with a `// covers:` marker and a t.Skip naming the slice that turns it green.
// They are placeholders: they keep `go test ./...` green and the covers-map
// meta-test satisfied while the rows exist but the behaviour is unbuilt. When a
// slice builds a row, it REPLACES that row's encoder here with the real covering
// test, so the skip becomes a permanent guard (CLAUDE.md testing discipline).
//
// Out of scope for SE0 (land in their build-slice's PR, per design §10/§11):
// MA-619 (Q10), MA-642 (Q14), MA-643 (Q11), MA-644 (Q12). The lab rows
// MA-625/626/627 are tier L (pending) and carry no encoder.

import "testing"

// MA-616 encoder removed: the §4.6.2 identity predicate is proven by
// internal/health/ma616_identity_test.go (the composition trace) and its
// note/WARN texts (exe_former_name, exe_console_build, exe_version_skew, the
// exe_missing former-name hint) by internal/health/ma616_texts_test.go.

// covers: MA-617, R14, R18, S59
func TestSE0_MA617_repairKeepsScheduleKind(t *testing.T) {
	t.Skip("SE2 encoder: a repair rebuilds its own kind from trusted inputs, never from the descriptor, or refuses")
}

// covers: MA-618, R18, S59
func TestSE0_MA618_whenRepairIsOffered(t *testing.T) {
	t.Skip("SE2 encoder: repair offered only for a corroborated run-job/capture; volatile-directory question first")
}

// MA-633 encoder removed: the per-kind remedies are built. schedule.RepairRoute
// is proven by internal/schedule/repairroute_test.go, and health.repairHow — the
// rendering of the route in each surface's words, with the four backup remedies
// re-routed through it — by internal/health/ma633_repairhow_test.go.

// MA-634 encoder removed: the start-failure breadcrumb reader moved into
// health.Gather (keyed on out, not Desc.Name) and Assess raises RED start_failed
// under MA-168's rule — proven by internal/health/ma634_startfailure_test.go.
// The wizard's writer stays and is exercised through the run-job path (MA-124,
// MA-640).

// covers: MA-635, R18, S59
func TestSE0_MA635_exeMissingInstallFolderBranch(t *testing.T) {
	t.Skip("SE2 encoder: a missing known-name file in the program's own directory names installer Repair; others keep re-install")
}

// covers: MA-641, S59
func TestSE0_MA641_installAndRepairAgree(t *testing.T) {
	t.Skip("SE2 encoder: for the same inputs, installSpec and repairSpec produce the same Args/Exe/wrapper decision")
}
