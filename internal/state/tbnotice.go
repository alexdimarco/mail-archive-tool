package state

import "time"

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

// Sent reports whether the record already marks n's email sent: the same record, store and
// streak, with that email's flag set (the C5 pass's SPAM-01 second guard). The alert layer drops
// such a notice before sending, so a notice handed again, or a run racing the mark, never sends
// the email a second time.
func (s TBStreak) Sent(n TBNotice) bool {
	i := s.account(n.Store)
	if i < 0 || n.RecordID == "" || n.RecordID != s.RecordID || s.Accounts[i].Serial != n.Serial {
		return false
	}
	if n.Stopped {
		return s.Accounts[i].StoppedSent
	}
	return s.Accounts[i].StillSent
}

// tbStreakPlausibleMax bounds the run counter, the serial and an entry's behind runs a record may
// carry before it is read as damaged (the C2 pass round 4's R4-01): one backup a minute for 31
// years. Far below maxTBStreakRun, where StartRun stops counting, so a planted counter can never
// freeze "this run".
const tbStreakPlausibleMax = 1 << 24

// tbStreakEarliest is the earliest date a record may hold (the C5 pass's PLANT-01, the lead's call):
// MailArchive's completeness check did not exist before it, so an earlier date was planted.
var tbStreakEarliest = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// tbStreakAhead is how far after the run's own clock a record's date may be before it is damaged
// (PLANT-01): a day, so a time zone or a small clock correction is never called damage.
const tbStreakAhead = 24 * time.Hour

// ImplausibleAt reports a record no run of this archive could have written, read at now (R4-01 and
// PLANT-01, the lead's calls). Counters: a run counter, serial or behind-run count past
// tbStreakPlausibleMax, an entry whose streak number is above the record's, or more behind runs
// without progress than behind runs. Dates: any date before 2020-01-01; any date more than a day
// after now (only when now itself is plausible, so a run whose clock is wrong never calls a real
// record damaged); a first time after a last one (the streak's first behind after its latest, its
// latest progress before its first or after its latest, its first backup without progress before
// its latest progress or after its latest behind); and one behind run holding two different
// times. The run treats it as damaged: the streak starts fresh and the run records a WARN
// (tbstreak_unreadable), never silent. Fresh is R42's fail-safe reading: a later notice, never a
// false one, and the email and behind_since never show a planted date.
func (s TBStreak) ImplausibleAt(now time.Time) bool {
	if s.Run > tbStreakPlausibleMax || s.Serial > tbStreakPlausibleMax {
		return true
	}
	bad := func(t time.Time) bool {
		return !t.IsZero() && (t.Before(tbStreakEarliest) || !now.Before(MinPlausibleTime) && t.After(now.Add(tbStreakAhead)))
	}
	before := func(a, b time.Time) bool { return !a.IsZero() && !b.IsZero() && b.Before(a) } // a is after b
	if bad(s.LastRunAt) {
		return true
	}
	for _, a := range s.Accounts {
		if a.Serial > s.Serial || a.Runs > tbStreakPlausibleMax || a.NoProgressRuns > a.Runs {
			return true
		}
		if bad(a.FirstBehind) || bad(a.LastBehind) || bad(a.LastGrew) || bad(a.SeenAt) || bad(a.FirstNoProgress) {
			return true
		}
		if before(a.FirstBehind, a.LastBehind) || before(a.FirstBehind, a.LastGrew) || before(a.LastGrew, a.LastBehind) {
			return true
		}
		// The first backup without progress sits between the latest progress and the latest behind
		// backup (the lead's call of 2026-11-09).
		if before(a.LastGrew, a.FirstNoProgress) || before(a.FirstNoProgress, a.LastBehind) {
			return true
		}
		if a.Runs == 1 && !a.FirstBehind.Equal(a.LastBehind) {
			return true
		}
	}
	return false
}

// TBEmail is the Thunderbird download email's words, built from a run's notices (the C5 friction
// review's P1 to P4 and lows, the lead's call of 2026-11-09): which email it is, how many accounts
// its opening counts, one line per account, what to do (one line, or numbered steps) and how to
// tell which account a folder is. The alert layer (alert.TBBehind) adds the subject, the opening,
// the archive and computer lines and the once-only line.
type TBEmail struct {
	Stopped  bool     // any account stopped: the stopped email
	Accounts int      // the accounts the opening counts: the stopped ones in the stopped email, else all
	Lines    []string // one per account, the stopped ones first
	Remedy   []string // one line (begins in lower case after "Do this on that computer: "), or numbered steps
	Which    string   // how to see which account a folder is
}

// tbLeave is what to do while Thunderbird is still downloading, for a scheduled backup (the email
// only ever comes from one): it begins in lower case after "Do this on that computer: ".
const tbLeave = "leave Thunderbird open (it downloads mostly while the computer is left alone). Later backups add the rest by themselves."

// tbWhich is how to tell which account a folder is (M2): Thunderbird never shows the folder name
// in its account list, only on each account's "Server Settings" page.
const tbWhich = "To see which account that is: in Thunderbird, " + tbMenu + `; each account's "Server Settings" page shows the folder its mail is kept in, at the bottom.`

// TBNoticeEmail is the email's words for notices. Each account is said by its folder ("The account
// kept in the folder NAME", the owner's fallback wording, §9.3: an email leaves the computer), with
// counts only and no mail folder.
func TBNoticeEmail(notices []TBNotice, now time.Time) TBEmail {
	var halted, going []TBNotice
	for _, n := range notices {
		if n.Stopped {
			halted = append(halted, n)
		} else {
			going = append(going, n)
		}
	}
	if len(halted) == 0 && len(going) == 0 {
		return TBEmail{}
	}
	say := TBSay{Scheduled: true, Now: now}
	e := TBEmail{Stopped: len(halted) > 0, Accounts: len(going), Which: tbWhich}
	for _, n := range halted {
		e.Lines = append(e.Lines, tbStoppedLine(n, say))
	}
	for _, n := range going {
		e.Lines = append(e.Lines, tbStillLine(n))
	}
	if !e.Stopped {
		e.Remedy = []string{tbLeave}
		return e
	}
	e.Accounts = len(halted)
	whose := "that account's"
	if len(halted) > 1 {
		whose = "each stopped account's"
	}
	e.Remedy = []string{
		"1. Open Thunderbird and leave it open, with the computer left alone, for an hour or more. If it asks for a password, enter it.",
		"2. If Status & health in MailArchive Desktop still shows the same count the next day: in Thunderbird, " + tbMenu + ". On " + whose + ` "Synchronization & Storage" page, make sure it is set to keep all of the account's mail on this computer.`,
	}
	return e
}

// tbEmailAccount is how an account's line begins: by its folder, never by a name (§9.3).
func tbEmailAccount(n TBNotice) string {
	if f := CleanFolderName(n.Account); f != "" {
		return "The account kept in the folder " + f
	}
	return "An account"
}

// tbEmailCounts is how much of the account is downloaded: "about X of Y messages downloaded",
// "none of its Y messages downloaded yet" ("none of its messages (at least Y)" for a floor), or
// "its 1 message is not downloaded yet".
func tbEmailCounts(n TBNotice) string {
	switch {
	case n.OnDisk > 0:
		return "about " + tbN(n.OnDisk) + " of " + tbOfMessages(n.Listed, n.Floor) + " downloaded"
	case n.Floor:
		return "none of its messages (at least " + tbN(n.Listed) + ") downloaded yet"
	case n.Listed == 1:
		return "its 1 message is not downloaded yet"
	}
	return "none of its " + tbMessages(n.Listed) + " downloaded yet"
}

// tbStillLine is the still-downloading line: the counts, and how long, in whole days between the
// streak's first and latest behind runs (never to now).
func tbStillLine(n TBNotice) string {
	days := int(max(0, n.LastBehind.Sub(n.FirstBehind)) / (24 * time.Hour))
	after := "after " + Count(days, "day", "days") + "."
	if n.OnDisk > 0 {
		after = "still going " + after
	}
	return tbEmailAccount(n) + ": " + tbEmailCounts(n) + ", " + after
}

// tbStoppedLine is the stopped line: the counts, and since when there was no progress.
func tbStoppedLine(n TBNotice, say TBSay) string {
	return tbEmailAccount(n) + ": " + tbEmailCounts(n) + ", no progress since " + tbDate(n.LastGrew, say) + "."
}
