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

// Source-loss thresholds (owner ruling 2026-10-28, design-desktop-settings-card 3.7, R3C-11):
// measured as the time BETWEEN runs that found the source unreachable — LastUnreachable minus
// FirstUnreachable — with at least MinFailedRuns such runs, and NEVER compared with now. So a
// machine that was simply off runs nothing and trips nothing, the banner, status and the
// auto-pause read the same stored record and agree, and an hourly schedule (24 runs a day)
// does not auto-pause after a day and a bit. (Until 2026-10-28 they counted runs: 3 and 30.)
const (
	// SurfaceAfter: show "this archive's source hasn't been reachable" once the unreachable runs
	// span this long.
	SurfaceAfter = 3 * 24 * time.Hour
	// AutoPauseAfter: the optional auto-pause clock (owner policy; the pause only stops the
	// schedule and stays reversible, S3-C5 — it never seals or forgets).
	AutoPauseAfter = 30 * 24 * time.Hour
	// MinFailedRuns: one failed run alone never trips either threshold.
	MinFailedRuns = 2
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
	// LastUnreachable is the latest run that found the source unreachable, written only by such
	// a run (R3C-11). An older MailArchive skips the field (plain json.Unmarshal); a record an
	// older one wrote has none, and trips nothing until the next unreachable run writes it.
	LastUnreachable time.Time `json:"last_unreachable,omitempty"`
	LastSuccess     time.Time `json:"last_success,omitempty"` // the last run that reached the source
	LastKind        string    `json:"last_kind,omitempty"`    // the source-unreachable kind last seen (a classifier label)
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
		s.LastUnreachable = time.Time{}
		s.LastKind = ""
		s.LastSuccess = now
		return s
	}
	s.ConsecutiveFails++
	s.LastKind = util.StripControl(kind)
	// A time the clock can't really have read (before MinPlausibleTime: a computer started with no
	// clock set) counts the run but moves neither stored time (MA-891).
	if now.Before(MinPlausibleTime) {
		return s
	}
	// The clock went backwards past the latest unreachable run (it was ahead, and was put right):
	// the span starts again here, so it never runs negative and never stalls the thresholds. So
	// does a stored first time this record can't trust, before MinPlausibleTime or after now: a
	// v1.5.0 record set it once from whatever the clock read, and kept it for good (MA-891).
	if s.FirstUnreachable.IsZero() || s.FirstUnreachable.Before(MinPlausibleTime) || now.Before(s.FirstUnreachable) ||
		(!s.LastUnreachable.IsZero() && now.Before(s.LastUnreachable)) {
		s.FirstUnreachable = now
	}
	s.LastUnreachable = now
	return s
}

// MinPlausibleTime is the earliest time a source-loss record trusts (MA-891): a run stamped
// before it ran on a clock that was not set, and is never a first or latest unreachable time.
var MinPlausibleTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// Unreachable reports whether the archive's source is currently in an unreachable streak.
func (s SourceStreak) Unreachable() bool { return s.ConsecutiveFails > 0 }

// unreachableFor is how long the unreachable runs span: LastUnreachable minus FirstUnreachable,
// with at least MinFailedRuns of them, else 0. It never reads the clock (R3C-11). A stored time
// before MinPlausibleTime, or a latest time before the first (a record written while a clock was
// wrong), spans nothing (MA-891).
func (s SourceStreak) unreachableFor() time.Duration {
	if s.ConsecutiveFails < MinFailedRuns || s.FirstUnreachable.IsZero() || s.LastUnreachable.IsZero() {
		return 0
	}
	if s.FirstUnreachable.Before(MinPlausibleTime) || s.LastUnreachable.Before(MinPlausibleTime) {
		return 0
	}
	if d := s.LastUnreachable.Sub(s.FirstUnreachable); d > 0 {
		return d
	}
	return 0
}

// SurfaceLoudly reports whether the streak has crossed the "tell the owner" threshold: the
// unreachable runs span SurfaceAfter (3 days), never measured to now, so a machine that was
// off (no runs) never trips it.
func (s SourceStreak) SurfaceLoudly() bool { return s.unreachableFor() >= SurfaceAfter }

// AutoPauseDue reports whether the optional auto-pause clock has elapsed (S3-C5): the
// unreachable runs span AutoPauseAfter (30 days), again between runs. Auto-pause only stops
// the live schedule and stays reversible; it never seals.
func (s SourceStreak) AutoPauseDue() bool { return s.unreachableFor() >= AutoPauseAfter }

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
