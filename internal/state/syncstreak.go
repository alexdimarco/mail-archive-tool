package state

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// SyncStreakName is the archive-local record of how long this archive's scheduled
// backups have gone without a Thunderbird sync, and how long its scheduled syncs have
// fetched nothing (design-presync-attended §5.1, slice AS5). A dotfile in a PLAINTEXT
// archive only: an encrypted archive keeps nothing about the sync beside it (§4.4,
// C-16), and a keyed run removes a stray one.
const SyncStreakName = ".mailarchive-syncstreak.json"

// SyncStreakVersion is the record format version.
const SyncStreakVersion = 1

const maxSyncStreakBytes = 4 * 1024

// The sync-streak thresholds (§5.3, §5.4, owner ruling 3 and OQ-5): measured BETWEEN
// runs — the latest counted run minus the first — with at least SyncMinRuns of them,
// and never compared with now, so a computer that was off trips nothing.
const (
	// SyncStaleAfter: "Thunderbird has not been synced … since DATE" once the missed
	// runs span this long; the softer "nothing arrived" warning uses the same span.
	SyncStaleAfter = 3 * 24 * time.Hour
	// SyncMinRuns: one run alone never trips either warning.
	SyncMinRuns = 2
	// SyncBusyRuns: this many scheduled profile-busy results in a row count one miss
	// (C-4), so a stuck profile lock cannot hide the streak.
	SyncBusyRuns = 3
)

// SyncStreak is the cross-run Thunderbird-sync state (§5.1). Missed counts scheduled
// sessions in a row that did not sync; NoChange counts scheduled sessions in a row whose
// wait for mail saw nothing arrive (OQ-5). Serial goes up only when a miss follows a
// reset, so it names one real streak (the once-per-streak email keys on it, C-35);
// a clock that went back restarts FirstMissed but keeps Serial. LastName is the latest
// miss's outcome name, shown only through presync.TagPhrase's closed map (C-37).
type SyncStreak struct {
	Version       int       `json:"version"`
	Serial        int       `json:"serial"`
	Missed        int       `json:"missed"`
	Busy          int       `json:"busy,omitempty"`
	NoChange      int       `json:"no_change,omitempty"`
	FirstNoChange time.Time `json:"first_no_change,omitempty"`
	LastNoChange  time.Time `json:"last_no_change,omitempty"`
	FirstMissed   time.Time `json:"first_missed,omitempty"`
	LastMissed    time.Time `json:"last_missed,omitempty"`
	LastSynced    time.Time `json:"last_synced,omitempty"`
	LastName      string    `json:"last_name,omitempty"`
	// RecordID names this record (the gate-2 adversarial pass after AS5 and AS6): chosen at
	// random when a run first writes a record that has none, and kept by every later write.
	// A record removed (the sync switched off) or damaged starts again at Serial 1 with a
	// new RecordID, so the once-per-streak email, which keys on both, tells the new
	// record's streak and never mistakes it for one told before. Compared, never shown.
	RecordID string `json:"record_id,omitempty"`
}

// maxRecordIDLen bounds a RecordID read back; a longer one, or one with anything but
// letters and digits, reads as none, and the next write names the record again.
const maxRecordIDLen = 64

// Named is the streak with a RecordID: its own when it has one, else a new random one.
func (s SyncStreak) Named() SyncStreak {
	if s.RecordID == "" {
		s.RecordID = rand.Text()
	}
	return s
}

// plausibleRecordID reports whether id is one Named could have chosen: letters and
// digits only, at most maxRecordIDLen of them.
func plausibleRecordID(id string) bool {
	if len(id) > maxRecordIDLen {
		return false
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Synced folds a session that synced, from any run (§5.3 "Reset"): the missed streak
// and the profile-busy count end. Serial is kept: the next miss starts a new streak.
// A time before MinPlausibleTime is not kept as LastSynced (MA-891).
func (s SyncStreak) Synced(now time.Time) SyncStreak {
	s.Version = SyncStreakVersion
	s.Missed, s.Busy = 0, 0
	s.FirstMissed, s.LastMissed = time.Time{}, time.Time{}
	if !now.Before(MinPlausibleTime) {
		s.LastSynced = now
	}
	return s
}

// Miss folds a scheduled session that did not sync (§5.3 "Extend"): Missed counts up
// (and Serial with it when a streak begins), the profile-busy count ends, and name is
// kept, control-stripped, as the latest miss's. The times follow MA-891's rules: a time
// before MinPlausibleTime moves neither, though the run still counts; a clock that went
// back past the latest miss (or a first time this record cannot trust) restarts
// FirstMissed — and Serial does not change, so it never starts a new email "streak".
func (s SyncStreak) Miss(name string, now time.Time) SyncStreak {
	s.Version = SyncStreakVersion
	if s.Missed == 0 {
		s.Serial++
	}
	s.Missed++
	s.Busy = 0
	s.LastName = util.StripControl(name)
	s.FirstMissed, s.LastMissed = spanAt(s.FirstMissed, s.LastMissed, now)
	return s
}

// ProfileBusy folds a scheduled profile-busy session (§5.2, C-4): counted apart, and
// the SyncBusyRuns-th in a row counts one miss and starts the count again.
func (s SyncStreak) ProfileBusy(name string, now time.Time) SyncStreak {
	s.Version = SyncStreakVersion
	s.Busy++
	if s.Busy >= SyncBusyRuns {
		return s.Miss(name, now) // Miss resets Busy
	}
	return s
}

// NoChangeSeen folds a scheduled session whose wait for mail saw nothing arrive (OQ-5),
// under the same clock rules as Miss.
func (s SyncStreak) NoChangeSeen(now time.Time) SyncStreak {
	s.Version = SyncStreakVersion
	s.NoChange++
	s.FirstNoChange, s.LastNoChange = spanAt(s.FirstNoChange, s.LastNoChange, now)
	return s
}

// MailArrived folds a session, from any run, whose wait for mail saw mail arrive, or a
// scheduled session that found the person's own Thunderbird already open, or an attended
// Run now that found it open (owner ruling 3 of 2026-10-31; only where Run now starts
// Thunderbird, since elsewhere a Run now runs no session): the no-change run ends (§5.2,
// the second and third cases amended 2026-10-31).
func (s SyncStreak) MailArrived() SyncStreak {
	s.Version = SyncStreakVersion
	s.NoChange = 0
	s.FirstNoChange, s.LastNoChange = time.Time{}, time.Time{}
	return s
}

// spanAt moves a (first, last) pair to include a run at now under MA-891's rules.
func spanAt(first, last, now time.Time) (time.Time, time.Time) {
	if now.Before(MinPlausibleTime) {
		return first, last
	}
	if first.IsZero() || first.Before(MinPlausibleTime) || now.Before(first) || (!last.IsZero() && now.Before(last)) {
		first = now
	}
	return first, now
}

// spanOf is how long n counted runs span, last minus first, with at least SyncMinRuns
// of them and both times plausible, else 0. It never reads the clock.
func spanOf(n int, first, last time.Time) time.Duration {
	if n < SyncMinRuns || first.IsZero() || last.IsZero() || first.Before(MinPlausibleTime) || last.Before(MinPlausibleTime) {
		return 0
	}
	if d := last.Sub(first); d > 0 {
		return d
	}
	return 0
}

// Stale reports the 3-day "not synced" threshold (§5.3): at least two missed scheduled
// runs spanning three days, never measured to now.
func (s SyncStreak) Stale() bool {
	return spanOf(s.Missed, s.FirstMissed, s.LastMissed) >= SyncStaleAfter
}

// NothingArrived reports the softer threshold (§5.4, OQ-5): at least two scheduled
// syncs in a row that fetched nothing, spanning three days, never measured to now.
func (s SyncStreak) NothingArrived() bool {
	return spanOf(s.NoChange, s.FirstNoChange, s.LastNoChange) >= SyncStaleAfter
}

// ReadSyncStreak reads the archive-local sync streak at out. An ABSENT record is the
// zero streak with no error: an archive from before this record starts clean (MA-920).
// A present record is read fail-closed through util.ReadSecureFile (owner-only, 4 KiB,
// never a link); a malformed one reads as zero, so a damaged file never wedges a run.
// The name it keeps is control-stripped (C-37); a RecordID Named could not have chosen
// reads as none.
func ReadSyncStreak(out string) (SyncStreak, error) {
	data, err := util.ReadSecureFile(filepath.Join(out, SyncStreakName), "Thunderbird sync record", maxSyncStreakBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SyncStreak{}, nil
		}
		return SyncStreak{}, err
	}
	var s SyncStreak
	if json.Unmarshal(data, &s) != nil {
		return SyncStreak{}, nil
	}
	s.LastName = util.StripControl(s.LastName)
	if !plausibleRecordID(s.RecordID) {
		s.RecordID = ""
	}
	return s, nil
}

// WriteSyncStreak writes the streak atomically and owner-only (0600) beside the archive.
func WriteSyncStreak(out string, s SyncStreak) error {
	s.Version = SyncStreakVersion
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(filepath.Join(out, SyncStreakName), data, archiveTempPattern)
}

// RemoveSyncStreak removes the archive's sync streak, if any (§5.3 "Removing the file"):
// where the sync is switched off, and beside an encrypted archive. An absent file is
// not an error.
func RemoveSyncStreak(out string) error {
	if err := os.Remove(filepath.Join(out, SyncStreakName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
