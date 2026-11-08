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

// maxTBStreakBytes bounds the record a reader takes in: TBAccountsMax accounts of a few hundred
// bytes each.
const maxTBStreakBytes = 64 * 1024

// TBStreak is the cross-run behind-streak state, built like the presync streak (SyncStreak):
// Serial rises only when an account's streak starts, and that streak keeps its number, so the
// once-per-streak emails can key on (RecordID, account, serial); RecordID is chosen at random
// when the record is first written.
//
// Run counts the runs that folded results into the record (StartRun); each entry keeps the run
// that last reached its store (SeenRun) and the run that last found it behind (BehindRun), so an
// entry whose store no run reaches any more is pruned (Prune) and a full record makes room
// (BehindEvicting), never silently (the C2 pass round 2's R2-08).
type TBStreak struct {
	Version  int               `json:"version"`
	Serial   int               `json:"serial"`
	Run      int               `json:"run,omitempty"`
	RecordID string            `json:"record_id,omitempty"`
	Accounts []TBStreakAccount `json:"accounts,omitempty"`
}

// TBStreakUnseenRuns is how many runs in a row may pass without reaching a store before its
// streak entry is pruned: 72, three days of an hourly schedule. A store that any schedule of the
// archive reaches at least once a day (a nightly job beside an hourly one on other inputs) is
// never pruned, with two missed days to spare; a store dropped from the job's inputs, a profile
// that moved (a new path is a new key) or an account removed goes within three days of hourly
// runs. Pruning a live entry only restarts its streak: the "still downloading" notice comes
// later, never falsely. Only runs that ended ok with Thunderbird results count.
const TBStreakUnseenRuns = 72

// maxTBStreakRun bounds the run counter a record may carry, so a planted value never wraps.
const maxTBStreakRun = 1 << 40

// TBStreakAccount is one account's streak (M10's list): the first behind run's time and X
// then; the latest behind run's time; the latest time X grew, and X then; the number of behind
// runs; and the two once-per-streak flags (set by C5).
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
	StillSent   bool      `json:"still_downloading_sent,omitempty"`
	StoppedSent bool      `json:"stopped_sent,omitempty"`
	// SeenRun is the record's Run when a run last reached the store (any verdict); BehindRun
	// when a run last found it behind.
	SeenRun   int `json:"seen_run,omitempty"`
	BehindRun int `json:"behind_run,omitempty"`
}

// StartRun is the streak at the start of a run's fold: Run up by one.
func (s TBStreak) StartRun() TBStreak {
	s.Version = TBStreakVersion
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	if s.Run < maxTBStreakRun {
		s.Run++
	}
	return s
}

// Seen notes that this run reached the store keyed store (whatever its verdict).
func (s TBStreak) Seen(store string) TBStreak {
	if i := s.account(store); i >= 0 {
		s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
		s.Accounts[i].SeenRun = s.Run
	}
	return s
}

// Prune drops the entries whose store no run has reached for TBStreakUnseenRuns runs, and gives
// them, so the caller can say so.
func (s TBStreak) Prune() (TBStreak, []TBStreakAccount) {
	var kept, gone []TBStreakAccount
	for _, a := range s.Accounts {
		if s.Run-a.SeenRun >= TBStreakUnseenRuns {
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
// the serial. X "grew" when it is above the X of the latest growth. A full record makes room
// (BehindEvicting).
func (s TBStreak) Behind(store, name string, x int64, now time.Time) TBStreak {
	s, _, _ = s.BehindEvicting(store, name, x, now)
	return s
}

// BehindEvicting is Behind, and the entry it dropped to make room, if any: a new streak never
// waits for a free place (the C2 pass round 2's R2-08). With TBAccountsMax entries, the one to go
// is the oldest of those this run did not find behind (BehindRun before Run), else the oldest of
// all; oldest is the one a run reached longest ago, then the one found behind longest ago, then
// the first in the record.
func (s TBStreak) BehindEvicting(store, name string, x int64, now time.Time) (TBStreak, TBStreakAccount, bool) {
	s.Version = TBStreakVersion
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	var evicted TBStreakAccount
	dropped := false
	i := s.account(store)
	if i < 0 {
		if len(s.Accounts) >= TBAccountsMax {
			j := s.oldest()
			evicted, dropped = s.Accounts[j], true
			s.Accounts = append(s.Accounts[:j], s.Accounts[j+1:]...)
		}
		s.Serial++
		a := TBStreakAccount{Store: store, Account: name, Serial: s.Serial, FirstX: x, GrewX: x, Runs: 1, SeenRun: s.Run, BehindRun: s.Run}
		if !now.Before(MinPlausibleTime) {
			a.FirstBehind, a.LastBehind, a.LastGrew = now, now, now
		}
		s.Accounts = append(s.Accounts, a)
		return s, evicted, dropped
	}
	a := s.Accounts[i]
	a.Account = name
	a.Runs++
	a.SeenRun, a.BehindRun = s.Run, s.Run
	restarted := a.FirstBehind
	a.FirstBehind, a.LastBehind = spanAt(a.FirstBehind, a.LastBehind, now)
	if !a.FirstBehind.Equal(restarted) && !restarted.IsZero() {
		// The clock went back: the streak's first time restarts, and so does its growth's.
		a.LastGrew, a.GrewX, a.FirstX = a.FirstBehind, x, x
	}
	if x > a.GrewX {
		a.GrewX = x
		if !now.Before(MinPlausibleTime) {
			a.LastGrew = now
		}
	}
	s.Accounts[i] = a
	return s, evicted, dropped
}

// oldest is the index of the entry BehindEvicting drops (s.Accounts is not empty): the least
// (SeenRun, BehindRun), the first on a tie. An entry this run found behind was also reached this
// run (BehindEvicting sets both to Run, and a read clamps both to at most Run), so it sorts after
// every entry this run did not find behind: "the oldest not behind now, else the oldest of all"
// needs no pass of its own.
func (s TBStreak) oldest() int {
	best := 0
	for i, a := range s.Accounts {
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
// spanning SyncStaleAfter, measured from the latest growth to the latest behind run.
func (a TBStreakAccount) Stopped() bool {
	return spanOf(a.Runs, a.LastGrew, a.LastBehind) >= SyncStaleAfter
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
	kept := s.Accounts[:0]
	for _, a := range s.Accounts {
		a.Store = util.StripControl(a.Store)
		if a.Store == "" || len(kept) >= TBAccountsMax {
			continue
		}
		a.Account = CleanFolderName(a.Account)
		a.FirstX, a.GrewX, a.Runs = max(0, a.FirstX), max(0, a.GrewX), max(0, a.Runs)
		// A planted run number is never ahead of the record's (it would never be pruned).
		a.SeenRun, a.BehindRun = min(max(0, a.SeenRun), s.Run), min(max(0, a.BehindRun), s.Run)
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
