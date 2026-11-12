package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// TBStreakName is the archive-local record of each Thunderbird account's behind-streak: how
// long backups in a row have found Thunderbird still downloading it (the completeness design
// §5's foldcheck M10 fold; slice C2 keeps it, slice C5 sends its notices). The run history keeps
// only RunHistoryMax runs, about two days of an hourly schedule, so a 3-day notice cannot be
// counted from it, and nothing there could say a streak's email was sent. A dotfile in a
// PLAINTEXT archive only: an encrypted archive keeps no streak (O1), and a keyed run removes a
// stray one. It holds no folder name and no Y history.
const TBStreakName = ".mailarchive-tbstreak.json"

// TBStreakVersion is the record format version.
const TBStreakVersion = 1

// TBStreakAccountsMax is how many stores the record keeps a streak for (the C2 pass round 3's
// R3-04, the lead's call: 256). It is apart from TBAccountsMax, the last-run record's cap: a
// streak entry is small, and a store whose streak is evicted never reaches its 3-day notice.
const TBStreakAccountsMax = 256

// maxTBStreakBytes bounds the record a reader takes in: TBStreakAccountsMax entries of at most
// about 1 KiB each (an 80-character name of "<", 6 bytes each in JSON, is the worst; MA-1281's
// bound test measures it).
const maxTBStreakBytes = 512 * 1024

// TBStreak is the cross-run behind-streak state, built like the presync streak (SyncStreak):
// Serial rises only when an account's streak starts, and that streak keeps its number, so the
// once-per-streak emails can key on (RecordID, account, serial); RecordID is chosen at random
// when the record is first written.
//
// Run counts the runs that folded results into the record (StartRun) and LastRunAt is the latest
// one's time; each entry keeps the run that last reached its store (SeenRun) and its time
// (SeenAt), and the run that last found it behind (BehindRun). An entry whose store no run has
// reached for TBStreakUnseenFor is pruned (Prune, by time: the C2 pass round 3's R3-03), and a
// full record makes room only from entries this run did not find behind (BehindEvicting, R3-04),
// never silently.
type TBStreak struct {
	Version   int               `json:"version"`
	Serial    int               `json:"serial"`
	Run       int               `json:"run,omitempty"`
	LastRunAt time.Time         `json:"last_run_at,omitzero"`
	RecordID  string            `json:"record_id,omitempty"`
	Accounts  []TBStreakAccount `json:"accounts,omitempty"`
}

// TBStreakUnseenFor is how long no run may reach a store before its streak entry is pruned: 30
// days, by the record's own times (the C2 pass round 3's R3-03, the lead's call; it replaced a
// count of 72 runs, which pruned a store a weekly job reaches beside an hourly job between its
// runs). A store any schedule reaches (hourly, daily or weekly, internal/schedule) is never
// pruned, with three missed weeks to spare; a store dropped from the job's inputs, a profile that
// moved (a new path is a new key) or an account removed goes after 30 days. A clock that went back
// never prunes (an entry seen "later" than now is kept), and neither does a run whose clock is
// before MinPlausibleTime. Pruning a live entry only restarts its streak: the "still downloading"
// notice comes later, never falsely. Only runs that ended ok with Thunderbird results count.
const TBStreakUnseenFor = 30 * 24 * time.Hour

// TBRoom is how BehindEvicting placed a behind store.
type TBRoom uint8

const (
	// TBRoomKept: the store had a streak, or there was room for a new one.
	TBRoomKept TBRoom = iota
	// TBRoomEvicted: the record was full, and an entry this run did not find behind went.
	TBRoomEvicted
	// TBRoomFull: the record was full of entries this run found behind; none went, and the store
	// got no streak (the caller logs it and leaves the tbstreak_full WARN).
	TBRoomFull
)

// maxTBStreakRun bounds the run counter a record may carry, so a planted value never wraps.
const maxTBStreakRun = 1 << 40

// TBStreakAccount is one account's streak (M10's list): the first behind run's time and X
// then; the latest behind run's time; the latest time X grew, and X then; the number of behind
// runs; X at the latest behind run and how many behind runs since the latest growth saw none (the
// C5 pass's FALSE-01 and FALSE-02, the lead's calls); and the two once-per-streak flags (set by
// C5).
type TBStreakAccount struct {
	// Store is the account's key: a hash of the store's full path (profile and store), so two
	// stores with one folder name keep a streak each (the C2 pass's ADV-C2-01). An entry with
	// none (written before the key existed, on this unreleased branch only) is dropped when read.
	Store       string    `json:"store"`
	Account     string    `json:"account"` // the store folder's name, for the words
	Serial      int       `json:"serial"`  // the record's Serial when this streak started
	FirstBehind time.Time `json:"first_behind,omitzero"`
	FirstX      int64     `json:"first_x"`
	LastBehind  time.Time `json:"last_behind,omitzero"`
	LastGrew    time.Time `json:"last_grew,omitzero"`
	GrewX       int64     `json:"grew_x"`
	Runs        int       `json:"runs"`
	// LastX is X at the latest behind run: growth ("progress") is X above it, never above the
	// streak's highest (FALSE-02), so X that fell and grows again is progress. An entry without it
	// (written before this field) reads 0, so its next behind run counts as progress: the stopped
	// email comes later, never falsely.
	LastX int64 `json:"last_x"`
	// NoProgressRuns counts the behind runs with a plausible clock since the latest growth whose X
	// did not grow (FALSE-01): "stopped" needs TBStoppedRuns of them, so the days a computer was
	// switched off count for nothing.
	NoProgressRuns int `json:"no_progress_runs,omitempty"`
	// FirstNoProgress is the time of the first of those runs (the lead's call of 2026-11-09):
	// "stopped" also needs them to span TBStoppedSpan, first to latest, so an hourly schedule after
	// a long switch-off waits a day. None until a plausible run without progress sets it, always
	// together with NoProgressRuns: one without the other is damaged (the C5 round-2 pass's
	// PLANT-R2, the lead's call; a record of this unreleased branch written before the field starts
	// fresh once).
	FirstNoProgress time.Time `json:"first_no_progress,omitzero"`
	StillSent       bool      `json:"still_downloading_sent,omitempty"`
	StoppedSent     bool      `json:"stopped_sent,omitempty"`
	// StillTries and StoppedTries count the sends of each email that failed in this streak (the C5
	// round-2 pass's SPAM-R2-A, the lead's call of 2026-11-09): at AlertSendTries, no more are
	// tried. A failure never sets the sent flag beside it.
	StillTries   int `json:"still_downloading_tries,omitempty"`
	StoppedTries int `json:"stopped_tries,omitempty"`
	// TriedSince is the alert layer's delivery id (backupalert's alert-delivered mark) current when
	// those failures were counted, and LastFailedTry the time of the latest (the C5 round-3 pass's
	// CAP-R3-A, the lead's call of 2026-11-09): once any email has been delivered since (a different
	// id) the counts no longer count, and after the 5th failure in a row the email is still tried
	// once a day (AlertTryDue). Neither is a date the streak's rules read.
	TriedSince    string    `json:"tried_since,omitempty"`
	LastFailedTry time.Time `json:"last_failed_try,omitzero"`
	// SeenRun is the record's Run when a run last reached the store (any verdict), and SeenAt
	// that run's time (zero when its clock was before MinPlausibleTime); BehindRun is the Run
	// when a run last found it behind.
	SeenRun   int       `json:"seen_run,omitempty"`
	SeenAt    time.Time `json:"seen_at,omitzero"`
	BehindRun int       `json:"behind_run,omitempty"`
}

// StartRun is the streak at the start of a run's fold at now: Run up by one, and LastRunAt now
// when now is plausible (even when it is before the last one: it is the ceiling a read clamps
// each SeenAt to).
func (s TBStreak) StartRun(now time.Time) TBStreak {
	s.Version = TBStreakVersion
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	if s.Run < maxTBStreakRun {
		s.Run++
	}
	if !now.Before(MinPlausibleTime) {
		s.LastRunAt = now
	}
	return s
}

// Seen notes that this run, at now, reached the store keyed store (whatever its verdict).
func (s TBStreak) Seen(store string, now time.Time) TBStreak {
	if i := s.account(store); i >= 0 {
		s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
		s.Accounts[i].SeenRun = s.Run
		if !now.Before(MinPlausibleTime) {
			s.Accounts[i].SeenAt = now
		}
	}
	return s
}

// Prune drops, at now, the entries whose store no run has reached for TBStreakUnseenFor, and
// gives them, so the caller can say so. Nothing is pruned when now is before MinPlausibleTime,
// nor an entry whose SeenAt is after now (the clock went back). An entry with no SeenAt (its
// runs' clocks were not plausible) starts its 30 days now.
func (s TBStreak) Prune(now time.Time) (TBStreak, []TBStreakAccount) {
	if now.Before(MinPlausibleTime) {
		return s, nil
	}
	var kept, gone []TBStreakAccount
	for _, a := range s.Accounts {
		switch {
		case a.SeenAt.IsZero():
			a.SeenAt = now
		case now.Sub(a.SeenAt) >= TBStreakUnseenFor: // negative when the clock went back: kept
			gone = append(gone, a)
			continue
		}
		kept = append(kept, a)
	}
	s.Accounts = kept
	return s, gone
}

// Named is the streak with a RecordID: its own when it has one, else a new random one.
func (s TBStreak) Named() TBStreak {
	if s.RecordID == "" {
		s.RecordID = SyncStreak{}.Named().RecordID
	}
	return s
}

// account is the index of the streak of the store keyed store in s, or -1.
func (s TBStreak) account(store string) int {
	for i, a := range s.Accounts {
		if a.Store == store {
			return i
		}
	}
	return -1
}

// Account is the streak of the store keyed store and whether it has one.
func (s TBStreak) Account(store string) (TBStreakAccount, bool) {
	if i := s.account(store); i >= 0 {
		return s.Accounts[i], true
	}
	return TBStreakAccount{}, false
}

// Behind folds a behind run of the store keyed store (its folder named name) with x messages on
// disk at now (§5's rules, M10): a streak starts (Serial up) or is extended. The times follow
// MA-891's rules, as the presync streak's: a time before MinPlausibleTime moves neither first nor
// latest, though the run still counts; a clock that went back restarts the first time and keeps
// the serial, and so does a gap of more than TBStreakUnseenFor since the latest behind run
// (CLOCK-R2-A: a streak never counts a firmware date's 610 days). X "grew" when it is above the previous behind run's X (the C5 pass's FALSE-02); a
// plausible run whose X did not grow counts toward "stopped" (FALSE-01), and the first such run
// since the latest growth starts the window TBStoppedSpan measures from. A plausible run that finds
// no progress time it can use, none (the streak's first run had a clock before MinPlausibleTime,
// MISS-01) or one later than now (a wrong clock), starts the progress time at now. A full record
// makes room (BehindEvicting).
func (s TBStreak) Behind(store, name string, x int64, now time.Time) TBStreak {
	s, _, _ = s.BehindEvicting(store, name, x, now)
	return s
}

// BehindEvicting is Behind, the entry it dropped to make room, if any, and how the store was
// placed. A full record (TBStreakAccountsMax entries) makes room only from the entries this run
// did not find behind (BehindRun before Run; the C2 pass round 3's R3-04, the lead's call): the
// oldest of them goes (TBRoomEvicted), oldest being the one a run reached longest ago, then the
// one found behind longest ago, then the first in the record. An entry this run found behind is
// never evicted: when every entry was, the record is unchanged and the store gets no streak
// (TBRoomFull), so a running streak always reaches its notice and the caller says who waited.
func (s TBStreak) BehindEvicting(store, name string, x int64, now time.Time) (TBStreak, TBStreakAccount, TBRoom) {
	var evicted TBStreakAccount
	room := TBRoomKept
	i := s.account(store)
	if i < 0 && len(s.Accounts) >= TBStreakAccountsMax {
		j := s.oldest()
		if j < 0 {
			return s, evicted, TBRoomFull
		}
		s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
		evicted, room = s.Accounts[j], TBRoomEvicted
		s.Accounts = append(s.Accounts[:j], s.Accounts[j+1:]...)
	}
	s.Version = TBStreakVersion
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	seenAt := time.Time{}
	if !now.Before(MinPlausibleTime) {
		seenAt = now
	}
	if i < 0 {
		s.Serial++
		a := TBStreakAccount{Store: store, Account: name, Serial: s.Serial, FirstX: x, GrewX: x, LastX: x, Runs: 1, SeenRun: s.Run, SeenAt: seenAt, BehindRun: s.Run}
		if !now.Before(MinPlausibleTime) {
			a.FirstBehind, a.LastBehind, a.LastGrew = now, now, now
		}
		s.Accounts = append(s.Accounts, a)
		return s, evicted, room
	}
	a := s.Accounts[i]
	a.Account = name
	a.Runs++
	a.SeenRun, a.BehindRun = s.Run, s.Run
	if !seenAt.IsZero() {
		a.SeenAt = seenAt
	}
	restarted := a.FirstBehind
	plausible := !now.Before(MinPlausibleTime)
	// A gap of more than TBStreakUnseenFor since the latest behind backup restarts the streak's
	// times here, as a clock that went back does (the C5 round-2 pass's CLOCK-R2-A, the lead's call
	// of 2026-11-09): either the earlier clock was wrong (a dead clock battery's firmware date at a
	// streak's first backup) or no backup ran for a month; neither is a month of downloading.
	gap := plausible && !a.LastBehind.IsZero() && now.Sub(a.LastBehind) > TBStreakUnseenFor
	// A clock before the latest behind backup went back, even when it reads exactly the streak's
	// first time, which spanAt then keeps (the C5 round-3 pass's TIE-R3, the lead's call of
	// 2026-11-09): a clock equal to a stored time is not "later".
	back := plausible && !a.LastBehind.IsZero() && now.Before(a.LastBehind)
	a.FirstBehind, a.LastBehind = spanAt(a.FirstBehind, a.LastBehind, now)
	if gap {
		a.FirstBehind = now
	}
	grew := x > a.LastX
	a.LastX = x
	switch {
	case (back || !a.FirstBehind.Equal(restarted)) && !restarted.IsZero():
		// The clock went back, or a gap of more than a month: the streak's first time restarts, and
		// so does its growth's. The serial and the sent flags are kept.
		a.LastGrew, a.GrewX, a.FirstX, a.NoProgressRuns, a.FirstNoProgress = a.FirstBehind, x, x, 0, time.Time{}
	case plausible && (a.LastGrew.IsZero() || a.LastGrew.After(now)):
		// No progress time this run can measure from (MISS-01): it starts here.
		a.LastGrew, a.GrewX, a.NoProgressRuns, a.FirstNoProgress = now, x, 0, time.Time{}
	case grew:
		a.GrewX, a.NoProgressRuns, a.FirstNoProgress = x, 0, time.Time{}
		if plausible {
			a.LastGrew = now
		}
	case plausible:
		if a.NoProgressRuns < tbStreakPlausibleMax {
			a.NoProgressRuns++
		}
		if a.FirstNoProgress.IsZero() {
			a.FirstNoProgress = now
		}
	}
	s.Accounts[i] = a
	return s, evicted, room
}

// TBStoppedRuns is how many behind runs since the latest growth must have seen none before the
// stopped email is due (the C5 pass's FALSE-01, the lead's call of 2026-11-09), beside the 3 days.
const TBStoppedRuns = 3

// TBStoppedSpan is how long those runs must span, the first of them to the latest (the lead's call
// of 2026-11-09, beside FALSE-01): a day, so an hourly schedule after a long switch-off cannot call
// an account stopped a few hours after the computer is back on. A daily schedule's third such run
// already spans two days.
const TBStoppedSpan = 24 * time.Hour

// oldest is the index of the entry BehindEvicting may drop: of the entries this run did not find
// behind (BehindRun before Run; a read clamps BehindRun to at most Run), the least (SeenRun,
// BehindRun), the first on a tie; -1 when this run found every entry behind.
func (s TBStreak) oldest() int {
	best := -1
	for i, a := range s.Accounts {
		if a.BehindRun >= s.Run {
			continue // found behind this run: never evicted
		}
		if best < 0 {
			best = i
			continue
		}
		b := s.Accounts[best]
		if a.SeenRun < b.SeenRun || a.SeenRun == b.SeenRun && a.BehindRun < b.BehindRun {
			best = i
		}
	}
	return best
}

// End ends the streak of the store keyed store (a caught_up run, with no email; or a limited
// run, silently): its entry goes. Serial is kept, so the next streak gets a new number.
func (s TBStreak) End(store string) TBStreak {
	s.Version = TBStreakVersion
	i := s.account(store)
	if i < 0 {
		return s
	}
	s.Accounts = append(append([]TBStreakAccount(nil), s.Accounts[:i]...), s.Accounts[i+1:]...)
	return s
}

// StillDownloading reports §5's "still downloading" threshold: behind runs spanning
// SyncStaleAfter or more, at least SyncMinRuns of them, measured between runs, never to now.
func (a TBStreakAccount) StillDownloading() bool {
	return spanOf(a.Runs, a.FirstBehind, a.LastBehind) >= SyncStaleAfter
}

// Stopped reports §5's "stopped downloading" threshold: X has not grown across behind runs
// spanning SyncStaleAfter, measured from the latest growth to the latest behind run, AND at least
// TBStoppedRuns behind runs in that span saw no growth (FALSE-01), AND those runs span
// TBStoppedSpan, the first to the latest: a computer switched off for days, then one backup, or a
// few hours of hourly ones, is never "stopped".
func (a TBStreakAccount) Stopped() bool {
	return a.NoProgressRuns >= TBStoppedRuns && spanOf(a.Runs, a.LastGrew, a.LastBehind) >= SyncStaleAfter &&
		spanOf(a.NoProgressRuns, a.FirstNoProgress, a.LastBehind) >= TBStoppedSpan
}

// ErrTBStreakNewer is ReadTBStreak's refusal of a record a later MailArchive wrote (a format
// version above TBStreakVersion): it is never read as this format and never written over, so a
// later version's fields are never lost (the C2 pass's ADV-C2-10).
var ErrTBStreakNewer = errors.New("written by a later MailArchive")

// ReadTBStreak reads the archive's behind-streak record. An ABSENT record is the zero streak
// with no error. A present one is read fail-closed through util.ReadSecureFile (owner-only,
// bounded, never a link: a refusal wraps util.ErrReadableByOthers when only its permissions are
// wrong); a malformed one reads as zero, so a damaged file never wedges a run; one of a later
// format is refused with ErrTBStreakNewer. Names are cleaned; a RecordID Named could not have
// chosen reads as none; an entry with no store key is dropped.
func ReadTBStreak(out string) (TBStreak, error) {
	path := filepath.Join(out, TBStreakName)
	data, err := util.ReadSecureFile(path, "Thunderbird download record", maxTBStreakBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return TBStreak{}, nil
		}
		return TBStreak{}, err
	}
	var s TBStreak
	if json.Unmarshal(data, &s) != nil {
		return TBStreak{}, nil
	}
	if s.Version > TBStreakVersion {
		return TBStreak{}, fmt.Errorf("Thunderbird download record %s is format %d, %w (this one reads format %d)", path, s.Version, ErrTBStreakNewer, TBStreakVersion)
	}
	if !plausibleRecordID(s.RecordID) {
		s.RecordID = ""
	}
	s.Run = min(max(0, s.Run), maxTBStreakRun)
	if s.LastRunAt.Before(MinPlausibleTime) {
		s.LastRunAt = time.Time{}
	}
	kept := s.Accounts[:0]
	for _, a := range s.Accounts {
		a.Store = util.StripControl(a.Store)
		if a.Store == "" || len(kept) >= TBStreakAccountsMax {
			continue
		}
		a.Account = CleanFolderName(a.Account)
		a.FirstX, a.GrewX, a.LastX, a.Runs, a.NoProgressRuns = max(0, a.FirstX), max(0, a.GrewX), max(0, a.LastX), max(0, a.Runs), max(0, a.NoProgressRuns)
		a.StillTries, a.StoppedTries = max(0, a.StillTries), max(0, a.StoppedTries)
		// A planted run number is never ahead of the record's (it would never be pruned).
		a.SeenRun, a.BehindRun = min(max(0, a.SeenRun), s.Run), min(max(0, a.BehindRun), s.Run)
		// Nor is a planted time: SeenAt is never after the latest run's time, nor implausible,
		// and with no run time it is none (Prune then starts the entry's 30 days at the next run).
		if a.SeenAt.After(s.LastRunAt) || a.SeenAt.Before(MinPlausibleTime) {
			a.SeenAt = s.LastRunAt // zero, or plausible
		}
		kept = append(kept, a)
	}
	s.Accounts = kept
	if len(s.Accounts) == 0 {
		s.Accounts = nil
	}
	return s, nil
}

// WriteTBStreak writes the record atomically and owner-only (0600) beside the archive.
func WriteTBStreak(out string, s TBStreak) error {
	s.Version = TBStreakVersion
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(filepath.Join(out, TBStreakName), data, archiveTempPattern)
}

// RemoveTBStreak removes the archive's behind-streak record, if any: beside an encrypted
// archive. An absent file is not an error.
func RemoveTBStreak(out string) error {
	if err := os.Remove(filepath.Join(out, TBStreakName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
