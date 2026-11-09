package state

import (
	"fmt"
	"time"
)

// The behind streak's two emails (docs/design-thunderbird-completeness.md §5 with its TC-20 and M10
// folds, §7's still-downloading and stopped sentences, §9.3; slice C5): when a scheduled backup of a
// plaintext archive finds an account still behind and its streak has lasted 3 days, one "still
// downloading" email; when X has not grown across behind runs spanning 3 days, one "stopped" email.
// Each at most once per streak, by the record's two flags; a stopped email may follow a
// still-downloading one, never the reverse. The run decides what is due (app); the scheduled run's
// alert layer sends it and only then sets the flags (backupalert). An encrypted archive keeps no
// streak, so it is never due one (O1).

// TBNotice is one Thunderbird account whose behind-streak is due an email at the run that just
// folded it: which email, the streak it is for (the record, the store and the streak's serial, so
// the flag is set on that streak only), and what the words say. It holds the store folder's name
// and counts only: no name Thunderbird shows for the account, and no mail folder (§9.3).
type TBNotice struct {
	Stopped  bool   // the stopped email; else the still-downloading one
	RecordID string // the streak record's RecordID when the run wrote it
	Store    string // the streak's key (TBStreakAccount.Store)
	Serial   int    // the streak's serial
	Account  string // the store folder's name, cleaned
	OnDisk   int64  // X at this run
	Listed   int64  // Y at this run
	Floor    bool   // Y is "at least" (§3's TC-1 fold)
	// FirstBehind, LastBehind and LastGrew are the streak's times: how long it has lasted, and
	// since when X has not grown.
	FirstBehind time.Time
	LastBehind  time.Time
	LastGrew    time.Time
}

// Due reports whether the streak is due an email, and whether it is the stopped one (TC-20): the
// stopped email when X has not grown across behind runs spanning 3 days and it was not sent; else
// the still-downloading one when the behind runs span 3 days and neither was sent. A streak that
// stalls from its start meets both thresholds at once and is due only the stopped email.
func (a TBStreakAccount) Due() (due, stopped bool) {
	switch {
	case a.StoppedSent:
		return false, false
	case a.Stopped():
		return true, true
	case !a.StillSent && a.StillDownloading():
		return true, false
	}
	return false, false
}

// MarkSent sets the streak's flag for the email that went (or that alerts did not want): the
// stopped email sets both flags, so a still-downloading email never follows it. Only the streak of
// store with that serial is marked; ok is false when the record holds no such streak (it ended, or a
// new one started, since the run that found it due).
func (s TBStreak) MarkSent(store string, serial int, stopped bool) (TBStreak, bool) {
	i := s.account(store)
	if i < 0 || s.Accounts[i].Serial != serial {
		return s, false
	}
	s.Accounts = append([]TBStreakAccount(nil), s.Accounts...)
	s.Accounts[i].StillSent = true
	if stopped {
		s.Accounts[i].StoppedSent = true
	}
	return s, true
}

// tbStreakPlausibleMax bounds the run counter, the serial and an entry's behind runs a record may
// carry before it is read as damaged (the C2 pass round 4's R4-01): one backup a minute for 31
// years. Far below maxTBStreakRun, where StartRun stops counting, so a planted counter can never
// freeze "this run".
const tbStreakPlausibleMax = 1 << 24

// Implausible reports a record no run of this archive could have written (R4-01, the lead's call):
// a run counter, serial or behind-run count past tbStreakPlausibleMax, or an entry whose streak
// number is above the record's. The run treats it as damaged: the streak starts fresh and the run
// records a WARN (tbstreak_unreadable), never silent. Fresh is R42's fail-safe reading: a later
// notice, never a false one.
func (s TBStreak) Implausible() bool {
	if s.Run > tbStreakPlausibleMax || s.Serial > tbStreakPlausibleMax {
		return true
	}
	for _, a := range s.Accounts {
		if a.Serial > s.Serial || a.Runs > tbStreakPlausibleMax {
			return true
		}
	}
	return false
}

// tbLeave is what to do while Thunderbird is still downloading, as the screen says it (tbBehind),
// for a scheduled backup: the email only ever comes from one.
const tbLeave = "leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest."

// TBNoticeEmail is the email's words for notices (the friction review of the email owes its
// look): whether it is the stopped email (any account stopped), one line per account (the stopped
// ones first), and what to do, beginning in lower case after the email's "Do this on that
// computer: ". Each account is said by its folder ("the Thunderbird account kept in the folder
// NAME", the owner's fallback wording, §9.3: an email leaves the computer), with counts only.
func TBNoticeEmail(notices []TBNotice, now time.Time) (stopped bool, lines []string, remedy string) {
	var halted, going []TBNotice
	for _, n := range notices {
		if n.Stopped {
			halted = append(halted, n)
		} else {
			going = append(going, n)
		}
	}
	say := TBSay{Scheduled: true, Now: now}
	for _, n := range halted {
		lines = append(lines, tbStoppedLine(n, say))
	}
	for _, n := range going {
		lines = append(lines, tbStillLine(n))
	}
	switch {
	case len(halted) == 0 && len(going) == 0:
		return false, nil, ""
	case len(halted) == 0:
		return false, lines, tbLeave
	}
	whose := "each account named above"
	if len(halted) == 1 {
		whose = tbNoticeName(halted[0])
	}
	remedy = "open Thunderbird and leave it open with the computer idle for a while. If nothing changes, in Thunderbird, " + tbMenu + `, and check the "Synchronization & Storage" page of ` + whose + "."
	if len(going) > 0 {
		remedy += " For the " + pick(len(going), "account", thousands(len(going))+" accounts") + " still downloading: " + tbLeave
	}
	return true, lines, remedy
}

// tbNoticeName is the account as the email says it: by its folder, never by a name (§9.3).
func tbNoticeName(n TBNotice) string { return tbName(TBAccount{Account: n.Account}) }

// tbStillLine is §7's still-downloading sentence: how long, measured between the streak's behind
// runs (never to now), and both numbers.
func tbStillLine(n TBNotice) string {
	days := int(max(0, n.LastBehind.Sub(n.FirstBehind)) / (24 * time.Hour))
	s := fmt.Sprintf("Thunderbird has been downloading %s for %s and ", tbNoticeName(n), Count(days, "day", "days"))
	if n.OnDisk <= 0 {
		return s + "has not downloaded " + tbNoneOf(n.Listed, n.Floor) + " yet."
	}
	return s + "has about " + tbN(n.OnDisk) + " of " + tbOfMessages(n.Listed, n.Floor) + " so far."
}

// tbStoppedLine is §7's stopped sentence: since when X has not grown, and both numbers; with
// nothing on that computer, that none is there yet.
func tbStoppedLine(n TBNotice, say TBSay) string {
	since := tbDate(n.LastGrew, say)
	if n.OnDisk <= 0 {
		if n.Listed == 1 && !n.Floor {
			return "The 1 message in " + tbNoticeName(n) + " is not on that computer yet, and Thunderbird has downloaded nothing since " + since + "."
		}
		return "None of " + tbOfMessagesThe(n.Listed, n.Floor) + " in " + tbNoticeName(n) + " are on that computer yet, and Thunderbird has downloaded none since " + since + "."
	}
	return "Thunderbird has not downloaded any more of " + tbNoticeName(n) + " since " + since + " (about " + tbN(n.OnDisk) + " of " + tbOfMessages(n.Listed, n.Floor) + " are on that computer)."
}

// tbOfMessagesThe is "the Y messages", or, for a floor, "at least Y messages".
func tbOfMessagesThe(y int64, floor bool) string {
	if floor {
		return "at least " + tbMessages(y)
	}
	return "the " + tbMessages(y)
}
