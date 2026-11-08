package state

import (
	"encoding/json"
	"errors"
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
type TBStreak struct {
	Version  int               `json:"version"`
	Serial   int               `json:"serial"`
	RecordID string            `json:"record_id,omitempty"`
	Accounts []TBStreakAccount `json:"accounts,omitempty"`
}

// TBStreakAccount is one account's streak (M10's list): the first behind run's time and X
// then; the latest behind run's time; the latest time X grew, and X then; the number of behind
// runs; and the two once-per-streak flags (set by C5).
type TBStreakAccount struct {
	Account     string    `json:"account"` // the store folder's name
	Serial      int       `json:"serial"`  // the record's Serial when this streak started
	FirstBehind time.Time `json:"first_behind,omitzero"`
	FirstX      int64     `json:"first_x"`
	LastBehind  time.Time `json:"last_behind,omitzero"`
	LastGrew    time.Time `json:"last_grew,omitzero"`
	GrewX       int64     `json:"grew_x"`
	Runs        int       `json:"runs"`
	StillSent   bool      `json:"still_downloading_sent,omitempty"`
	StoppedSent bool      `json:"stopped_sent,omitempty"`
}

// Named is the streak with a RecordID: its own when it has one, else a new random one.
func (s TBStreak) Named() TBStreak {
	if s.RecordID == "" {
		s.RecordID = SyncStreak{}.Named().RecordID
	}
	return s
}

// account is the index of name's streak in s, or -1.
func (s TBStreak) account(name string) int {
	for i, a := range s.Accounts {
		if a.Account == name {
			return i
		}
	}
	return -1
}

// Account is name's streak and whether it has one.
func (s TBStreak) Account(name string) (TBStreakAccount, bool) {
	if i := s.account(name); i >= 0 {
		return s.Accounts[i], true
	}
	return TBStreakAccount{}, false
}

// Behind folds a behind run of account name with x messages on disk at now (§5's rules, M10):
// a streak starts (Serial up) or is extended. The times follow MA-891's rules, as the presync
// streak's: a time before MinPlausibleTime moves neither first nor latest, though the run still
// counts; a clock that went back restarts the first time and keeps the serial. X "grew" when it
// is above the X of the latest growth.
func (s TBStreak) Behind(name string, x int64, now time.Time) TBStreak {
	s.Version = TBStreakVersion
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	i := s.account(name)
	if i < 0 {
		if len(s.Accounts) >= TBAccountsMax {
			return s
		}
		s.Serial++
		a := TBStreakAccount{Account: name, Serial: s.Serial, FirstX: x, GrewX: x, Runs: 1}
		if !now.Before(MinPlausibleTime) {
			a.FirstBehind, a.LastBehind, a.LastGrew = now, now, now
		}
		s.Accounts = append(s.Accounts, a)
		return s
	}
	a := s.Accounts[i]
	a.Runs++
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
	return s
}

// End ends name's streak (a caught_up run, with no email; or a limited run, silently): its
// entry goes. Serial is kept, so the next streak gets a new number.
func (s TBStreak) End(name string) TBStreak {
	s.Version = TBStreakVersion
	i := s.account(name)
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

// ReadTBStreak reads the archive's behind-streak record. An ABSENT record is the zero streak
// with no error. A present one is read fail-closed through util.ReadSecureFile (owner-only,
// bounded, never a link); a malformed one reads as zero, so a damaged file never wedges a run.
// Names are cleaned; a RecordID Named could not have chosen reads as none.
func ReadTBStreak(out string) (TBStreak, error) {
	data, err := util.ReadSecureFile(filepath.Join(out, TBStreakName), "Thunderbird download record", maxTBStreakBytes)
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
	if !plausibleRecordID(s.RecordID) {
		s.RecordID = ""
	}
	if len(s.Accounts) > TBAccountsMax {
		s.Accounts = s.Accounts[:TBAccountsMax]
	}
	for i := range s.Accounts {
		a := &s.Accounts[i]
		a.Account = CleanFolderName(a.Account)
		a.FirstX, a.GrewX, a.Runs = max(0, a.FirstX), max(0, a.GrewX), max(0, a.Runs)
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
