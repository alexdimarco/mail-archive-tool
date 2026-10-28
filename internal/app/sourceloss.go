package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/state"
)

// sourceReach is a run's verdict about whether it REACHED the archive's source (§5.7, S3-C2).
type sourceReach int

const (
	// sourceInconclusive: the run failed for a reason that says nothing about the source (a
	// search-index refusal, a config error) or was interrupted — leave the streak unchanged,
	// never falsely reset or extend it.
	sourceInconclusive sourceReach = iota
	// sourceReached: the run read the source (even an empty/quiet one is a reach) — reset.
	sourceReached
	// sourceUnreachable: the source could not be reached — extend the streak.
	sourceUnreachable
)

// sourceUnreachableError marks a run failure caused by the archive's SOURCE being unreachable
// at input discovery — a local mail-store folder that is gone or on an unmounted drive. The
// message stays the operator-facing one; the type lets classifySourceReach recognise it
// (S3-C2) so the streak climbs, and the streak's run count then sorts a transient absence (a
// drive briefly unplugged) from a permanent loss (S3-C3).
type sourceUnreachableError struct{ path string }

func (e *sourceUnreachableError) Error() string {
	return fmt.Sprintf("input %s does not exist (check the path, or that the drive holding it is mounted)", e.path)
}

// classifySourceReach decides, from a finished run's error, whether the run reached the
// archive's SOURCE. A clean run reached it (even an empty mailbox is a reach, so a healthy
// quiet source never reads as source-loss). An M365 breaker outage is unreachable. A
// cancellation, a search-index refusal, or any other failure is inconclusive about the source.
//
// A gone LOCAL source (a Thunderbird/mbox folder that is missing or on an unmounted drive) is
// unreachable too, via the typed sourceUnreachableError that DiscoverInputs returns (S3-C2);
// the streak's run count then sorts a transient absence from a permanent loss (S3-C3). A
// present-but-unreadable source (a lock or permission error) is NOT "gone", so it stays
// inconclusive and never false-positives as a lost source.
func classifySourceReach(err error) (sourceReach, string) {
	switch {
	case err == nil:
		return sourceReached, ""
	case errors.Is(err, context.Canceled):
		return sourceInconclusive, ""
	default:
		var outage *outageError
		if errors.As(err, &outage) {
			return sourceUnreachable, "m365-unreachable"
		}
		var sue *sourceUnreachableError
		if errors.As(err, &sue) {
			return sourceUnreachable, "local-source-unreachable"
		}
		return sourceInconclusive, ""
	}
}

// FoundSourceUnreachable reports whether a scheduled run that ended with err found the archive's
// source unreachable — the verdict that extends the source streak. Auto-pause acts only after
// such a run (MA-890): a failure that says nothing about the source never pauses on an old streak.
func FoundSourceUnreachable(err error) bool {
	reach, _ := classifySourceReach(err)
	return reach == sourceUnreachable
}

// Seams so a test moves the streak without touching a real archive-local file.
var (
	readSourceStreak  = state.ReadSourceStreak
	writeSourceStreak = state.WriteSourceStreak
)

// updateSourceStreak folds a finished SCHEDULED run's outcome into the archive's source-loss
// streak (§5.7, Slice 3b): a definitive reach resets it, an unreachable verdict extends it, an
// inconclusive outcome leaves it. Best effort — a read or write problem never fails the run
// and never falsely moves the streak.
func updateSourceStreak(out string, runErr error, now time.Time, encrypted bool, logger *log.Logger) {
	reach, kind := classifySourceReach(runErr)
	if reach == sourceInconclusive {
		return
	}
	if encrypted {
		kind = "" // an encrypted archive's streak does not say what kind of source it has (A6)
	}
	prev, err := readSourceStreak(out)
	if err != nil {
		return // a read error leaves the streak unchanged rather than resetting it
	}
	next := prev.Observe(reach == sourceReached, kind, now)
	if werr := writeSourceStreak(out, next); werr != nil && logger != nil {
		logger.Printf("warning: could not record the source-reachability streak in %s (%v)", filepath.Join(out, state.SourceStreakName), werr)
	}
}
