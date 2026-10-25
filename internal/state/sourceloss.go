package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// SourceStreakName is the archive-local record of how long this archive's SOURCE (its M365
// mailbox, or a local Thunderbird/mbox folder) has been unreachable. A dotfile in the archive
// -out, written each scheduled run.
const SourceStreakName = ".mailarchive-sourcestreak.json"

// SourceStreakVersion is the record format version.
const SourceStreakVersion = 1

const maxSourceStreakBytes = 4 * 1024

// Source-loss thresholds, counted in CONSECUTIVE failed scheduled RUNS, never elapsed time: a
// machine that was simply off runs nothing, so it never trips these — only runs that happened
// and found the source unreachable count (S3-C3). FirstUnreachable is kept ONLY for the
// banner's "since DATE" text, never as a trigger.
const (
	// SurfaceAfterRuns: show "this archive's source hasn't been reachable" after this many
	// consecutive failed runs.
	SurfaceAfterRuns = 3
	// AutoPauseAfterRuns: the optional auto-pause clock (owner policy; the pause only stops
	// the schedule and stays reversible, S3-C5 — it never seals or forgets).
	AutoPauseAfterRuns = 30
)

// SourceStreak is the cross-run source-reachability state §5.7's "surface loudly" threshold
// needs and that the single-slot last-run record (state.LastRun) cannot hold — it is
// overwritten every run, so it carries no streak and no first-failure date. SourceStreak is
// written each scheduled run through a path a headless run may use (unlike the known-archives
// index — a headless run must not write that, SD-6), modelled on the capture-gap ledger's
// persisted FirstRun/LastRun/Runs pattern. The unreachable kind is recorded so the banner and
// a headless signal can say which source and why. The streak is NOT a skip decision — nothing
// consults it to decide whether to run; it only reports how long the source has been gone.
type SourceStreak struct {
	Version          int       `json:"version"`
	ConsecutiveFails int       `json:"consecutive_fails"`           // scheduled runs in a row the source was unreachable
	FirstUnreachable time.Time `json:"first_unreachable,omitempty"` // when the current unreachable streak began ("since DATE")
	LastSuccess      time.Time `json:"last_success,omitempty"`      // the last run that reached the source
	LastKind         string    `json:"last_kind,omitempty"`         // the source-unreachable kind last seen (a classifier label)
}

// Observe folds one run's outcome into the streak and returns the new value (pure — the
// caller writes it). A run that REACHED the source resets the streak (it ends, last-success
// advances); a run that found the source UNREACHABLE extends it (count++, first-unreachable
// anchored once, kind recorded). kind is ignored when reachable.
func (s SourceStreak) Observe(reachable bool, kind string, now time.Time) SourceStreak {
	s.Version = SourceStreakVersion
	if reachable {
		s.ConsecutiveFails = 0
		s.FirstUnreachable = time.Time{}
		s.LastKind = ""
		s.LastSuccess = now
		return s
	}
	s.ConsecutiveFails++
	if s.FirstUnreachable.IsZero() {
		s.FirstUnreachable = now
	}
	s.LastKind = util.StripControl(kind)
	return s
}

// Unreachable reports whether the archive's source is currently in an unreachable streak.
func (s SourceStreak) Unreachable() bool { return s.ConsecutiveFails > 0 }

// SurfaceLoudly reports whether the streak has crossed the "tell the owner" threshold — a run
// count, never elapsed time, so a machine that was off (no runs) never trips it (S3-C3).
func (s SourceStreak) SurfaceLoudly() bool { return s.ConsecutiveFails >= SurfaceAfterRuns }

// AutoPauseDue reports whether the optional auto-pause clock has elapsed (S3-C5): also a run
// count. Auto-pause only stops the live schedule and stays reversible; it never seals.
func (s SourceStreak) AutoPauseDue() bool { return s.ConsecutiveFails >= AutoPauseAfterRuns }

// ReadSourceStreak reads the archive-local streak at out. An ABSENT record is the zero streak
// (reachable, no history) with no error; a present record is read fail-closed via
// util.ReadSecureFile (owner-only, size-bounded, never a symlink). A malformed record reads
// as zero so a damaged file never wedges a run (the next run re-establishes it).
func ReadSourceStreak(out string) (SourceStreak, error) {
	data, err := util.ReadSecureFile(filepath.Join(out, SourceStreakName), "source-loss record", maxSourceStreakBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SourceStreak{}, nil
		}
		return SourceStreak{}, err
	}
	var s SourceStreak
	if json.Unmarshal(data, &s) != nil {
		return SourceStreak{}, nil
	}
	return s, nil
}

// WriteSourceStreak writes the streak atomically and owner-only (0600) beside the archive, so
// a headless scheduled run can record it and a later run reads it back.
func WriteSourceStreak(out string, s SourceStreak) error {
	s.Version = SourceStreakVersion
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(filepath.Join(out, SourceStreakName), data, archiveTempPattern)
}
