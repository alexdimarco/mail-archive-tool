package app

import (
	"context"
	"errors"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-687
// The source-reachability classifier + its wiring into recordRun (archive-manager Slice 3b-i,
// S3-C2/S3-C3): a clean run is a reach (even an empty one, so a quiet source never reads as
// loss), an M365 breaker outage is unreachable, and a cancellation or a non-source failure is
// inconclusive. recordRun folds the verdict into the archive's streak ONLY for scheduled
// (unattended) runs, and only when the verdict is definitive — an interactive run or an
// inconclusive failure never moves it.
func TestMA687_SourceReachClassifyAndStreakWiring(t *testing.T) {
	if r, _ := classifySourceReach(nil); r != sourceReached {
		t.Fatal("a clean run must be a reach")
	}
	if r, _ := classifySourceReach(context.Canceled); r != sourceInconclusive {
		t.Fatal("a cancelled run must be inconclusive")
	}
	if r, _ := classifySourceReach(errors.New("search index busy")); r != sourceInconclusive {
		t.Fatal("a non-source failure must be inconclusive")
	}
	if r, k := classifySourceReach(&outageError{errors.New("graph 503")}); r != sourceUnreachable || k != "m365-unreachable" {
		t.Fatalf("an M365 outage must be unreachable/m365, got %v %q", r, k)
	}

	// Seam the streak store so the test touches no archive file.
	var wrote []state.SourceStreak
	var stored state.SourceStreak
	origR, origW, origLR := readSourceStreak, writeSourceStreak, writeLastRun
	defer func() { readSourceStreak, writeSourceStreak, writeLastRun = origR, origW, origLR }()
	readSourceStreak = func(string) (state.SourceStreak, error) { return stored, nil }
	writeSourceStreak = func(_ string, s state.SourceStreak) error { stored = s; wrote = append(wrote, s); return nil }
	writeLastRun = func(string, state.LastRun) error { return nil }

	call := func(unattended bool, runErr error) {
		e := runErr
		recordRun(Options{Out: "/x", Unattended: unattended}, state.LastRun{}, &Result{}, &e, nil)
	}

	// An unattended outage extends the streak.
	stored = state.SourceStreak{ConsecutiveFails: 2}
	wrote = nil
	call(true, &outageError{errors.New("graph 503")})
	if len(wrote) != 1 || wrote[0].ConsecutiveFails != 3 {
		t.Fatalf("an unattended outage must extend the streak to 3, got %+v", wrote)
	}

	// An unattended reach resets it.
	wrote = nil
	call(true, nil)
	if len(wrote) != 1 || wrote[0].ConsecutiveFails != 0 {
		t.Fatalf("an unattended reach must reset the streak, got %+v", wrote)
	}

	// An interactive run never touches the streak.
	wrote = nil
	call(false, &outageError{errors.New("graph 503")})
	if len(wrote) != 0 {
		t.Fatalf("an interactive run must not move the streak, got %+v", wrote)
	}

	// A non-source failure leaves the streak.
	wrote = nil
	call(true, errors.New("search index busy"))
	if len(wrote) != 0 {
		t.Fatalf("a non-source failure must not move the streak, got %+v", wrote)
	}
}

// covers: MA-688
// S3-C2 OPEN GAP (Slice 3b-ii): a LOST LOCAL source — a Thunderbird/mbox folder that is gone —
// must classify as source-unreachable so the streak climbs. It does not yet: classifySourceReach
// has no local signal, so a gone local folder currently reads as inconclusive. Skipped encoder
// until the local-source-unreachable signal lands — then REMOVE the skip and assert it.
func TestMA688_LostLocalSourceClassifiesUnreachable(t *testing.T) {
	// no-assert: skipped open-gap encoder for S3-C2; un-skip and assert when Slice 3b-ii lands.
	t.Skip("Slice 3b-ii: the local-source-unreachable signal (S3-C2) is not built yet")
}
