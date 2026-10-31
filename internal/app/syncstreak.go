package app

import (
	"log"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// Seams so a test sees the sync streak's reads, writes and removals (the archive's own
// file is still the real one: these are the state package's functions).
var (
	readSyncStreak   = state.ReadSyncStreak
	writeSyncStreak  = state.WriteSyncStreak
	removeSyncStreak = state.RemoveSyncStreak
)

// updateSyncStreak folds a finished run's Thunderbird sync into the archive's sync
// streak (design-presync-attended §5.1-§5.3, slice AS5). Called from recordRun beside
// the source streak; best effort — a read or write problem never fails the run and
// never falsely moves the streak.
//
//   - Plaintext only, by code (C-16): a keyed run, or a run into an encrypted archive,
//     writes nothing and removes a stray file.
//   - A scheduled run (Unattended and not attended, C-33) whose job has no sync removes
//     the file (C-34); a manual run never does.
//   - A synced session from ANY run resets the missed streak; only a scheduled run whose
//     job asks for the sync extends it (§5.3). profile-busy counts apart (C-4); a
//     cancelled run, a stopped wait and no session at all move nothing (C-72, OQ-6).
//   - The no-change count reads the session's Verdict (OQ-5): a scheduled no-change
//     verdict extends it whatever the outcome became; a verdict that mail arrived resets
//     it from any run; an attended no-change verdict never moves it.
func updateSyncStreak(opts Options, out *PresyncOutcome, encrypted bool, now time.Time, logger *log.Logger) {
	if opts.Key != nil || encrypted || ArchiveEncrypted(opts.Out) {
		_ = removeSyncStreak(opts.Out) // nothing about the sync beside an encrypted archive (§4.4)
		return
	}
	scheduled := opts.Unattended && !opts.PresyncAttended
	if scheduled && !opts.PresyncWanted {
		if err := removeSyncStreak(opts.Out); err != nil && logger != nil {
			logger.Printf("warning: could not remove the Thunderbird sync record %s (%v)", filepath.Join(opts.Out, state.SyncStreakName), err)
		}
		return
	}
	if out == nil {
		return // no session: the sync is off here, the run ended before it, or Run now does not sync on this platform
	}
	extend := scheduled && opts.PresyncWanted
	prev, err := readSyncStreak(opts.Out)
	if err != nil {
		return // an unreadable record is left as it is, never reset
	}
	next, moved := foldSyncStreak(prev, *out, extend, now)
	if !moved {
		return
	}
	if werr := writeSyncStreak(opts.Out, next); werr != nil && logger != nil {
		logger.Printf("warning: could not record the Thunderbird sync streak in %s (%v)", filepath.Join(opts.Out, state.SyncStreakName), werr)
	}
}

// foldSyncStreak is updateSyncStreak's pure part: the streak after one session, and
// whether anything moved (§5.2's table). extend is "a scheduled run whose job asks for
// the sync".
func foldSyncStreak(s state.SyncStreak, o PresyncOutcome, extend bool, now time.Time) (state.SyncStreak, bool) {
	moved := false
	switch o.Name {
	case presync.OutcomeCancelled.Name, presync.OutcomeWaitEnded.Name, presync.OutcomeStateUnreadable.Name, "":
		// neither (C-72, OQ-6, C-45): nothing is known about whether Thunderbird could sync
	case presync.OutcomeProfileBusy.Name:
		if extend {
			s, moved = s.ProfileBusy(o.Name, now), true
		}
	default:
		switch {
		case o.Synced:
			s, moved = s.Synced(now), true
		case extend:
			s, moved = s.Miss(o.Name, now), true
		}
	}
	switch o.Verdict {
	case presync.OutcomeNoActivity.Name, presync.OutcomeWatchTruncatedNoActivity.Name:
		if extend {
			s, moved = s.NoChangeSeen(now), true
		}
	case presync.OutcomeOK.Name, presync.OutcomeWatchTruncated.Name, presync.OutcomeNotQuiet.Name:
		s, moved = s.MailArrived(), true
	}
	return s, moved
}
