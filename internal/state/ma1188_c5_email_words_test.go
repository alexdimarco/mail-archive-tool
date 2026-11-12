package state

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-1188
// The email's words (§7's sentences, §9.3, with the C5 friction review's P1 to P4 and lows, the
// lead's call of 2026-11-09): each account on its own line, "The account kept in the folder NAME:"
// (the folder words, never a name or an address), its counts, and how long (still downloading) or
// since when there was no progress (stopped); the stopped lines first; no mail folder named; a
// hostile folder name cleaned. The still remedy is one line; the stopped remedy is two numbered
// steps, for "that account" or "each stopped account", with no separate line for an account still
// downloading. Every email says how to tell which account a folder is. The opening counts the
// stopped accounts in a stopped email, else every account. Positive twin first: one account still
// downloading after 3 days.
func TestTheNoticeEmailWords(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	nov4 := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	const which = `To see which account that is: in Thunderbird, click the menu button (≡, top right), then Account Settings; each account's "Server Settings" page shows the folder its mail is kept in, at the bottom.`
	const step1 = "1. Open Thunderbird and leave it open, with the computer left alone, for an hour or more. If it asks for a password, enter it."
	step2 := func(whose string) string {
		return `2. If Status & health in MailArchive Desktop still shows the same count the next day: in Thunderbird, click the menu button (≡, top right), then Account Settings. On ` + whose + ` "Synchronization & Storage" page, make sure it is set to keep all of the account's mail on this computer.`
	}

	still := TBNotice{Store: "k1", Serial: 1, Account: "imap.gmail.com", OnDisk: 120000, Listed: 238080,
		FirstBehind: now.Add(-3*24*time.Hour - time.Hour), LastBehind: now, LastGrew: now}
	e := TBNoticeEmail([]TBNotice{still}, now)
	want := "The account kept in the folder imap.gmail.com: about 120,000 of 238,080 messages downloaded, still going after 3 days."
	if e.Stopped || e.Accounts != 1 || len(e.Lines) != 1 || e.Lines[0] != want {
		t.Fatalf("positive twin: %+v, want the line %q", e, want)
	}
	if len(e.Remedy) != 1 || e.Remedy[0] != "leave Thunderbird open (it downloads mostly while the computer is left alone). Later backups add the rest by themselves." {
		t.Errorf("the still-downloading remedy: %q", e.Remedy)
	}
	if e.Which != which {
		t.Errorf("the which-account line: %q", e.Which)
	}

	stall := TBNotice{Stopped: true, Store: "k2", Serial: 2, Account: "imap.work.example", OnDisk: 1670, Listed: 56611, Floor: true,
		FirstBehind: time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local), LastBehind: now, LastGrew: nov4}
	e = TBNoticeEmail([]TBNotice{stall}, now)
	if !e.Stopped || e.Accounts != 1 || len(e.Lines) != 1 || e.Lines[0] != "The account kept in the folder imap.work.example: about 1,670 of at least 56,611 messages downloaded, no progress since Nov 4." {
		t.Errorf("the stopped line: %+v", e)
	}
	if len(e.Remedy) != 2 || e.Remedy[0] != step1 || e.Remedy[1] != step2("that account's") || e.Which != which {
		t.Errorf("the stopped remedy:\n%q\nwant\n%q", e.Remedy, []string{step1, step2("that account's")})
	}

	// None downloaded, stopped, a hostile folder name; with an account still downloading (stopped
	// first; the opening counts only the stopped one; no second "leave Thunderbird open").
	none := stall
	none.OnDisk, none.Floor, none.Listed, none.Account = 0, false, 1200, "imap.‮none\u001b.example"
	e = TBNoticeEmail([]TBNotice{still, none}, now)
	if !e.Stopped || e.Accounts != 1 || len(e.Lines) != 2 || e.Lines[0] != "The account kept in the folder imap.none.example: none of its 1,200 messages downloaded yet, no progress since Nov 4." || e.Lines[1] != want {
		t.Errorf("the mixed email: %+v", e)
	}
	if strings.Join(e.Remedy, "|") != step1+"|"+step2("that account's") {
		t.Errorf("the mixed remedy: %q", e.Remedy)
	}
	two := TBNoticeEmail([]TBNotice{stall, none, still, still}, now)
	if two.Accounts != 2 || len(two.Remedy) != 2 || two.Remedy[1] != step2("each stopped account's") {
		t.Errorf("two stopped and two still downloading: %+v", two)
	}
	if n := strings.Count(strings.Join(two.Remedy, " "), "leave"); n != 1 {
		t.Errorf("the stopped remedy says leave %d times: %q", n, two.Remedy)
	}
	if e := TBNoticeEmail([]TBNotice{still, still}, now); e.Accounts != 2 || e.Stopped {
		t.Errorf("two still downloading: %+v", e)
	}

	// The other shapes of a line.
	for _, c := range []struct {
		n    TBNotice
		want string
	}{
		{TBNotice{Account: "imap.gmail.com-1", Listed: 2500, FirstBehind: now.Add(-4 * 24 * time.Hour), LastBehind: now},
			"The account kept in the folder imap.gmail.com-1: none of its 2,500 messages downloaded yet, after 4 days."},
		{TBNotice{Account: "imap.one.example", Listed: 1, FirstBehind: now.Add(-3 * 24 * time.Hour), LastBehind: now},
			"The account kept in the folder imap.one.example: its 1 message is not downloaded yet, after 3 days."},
		{TBNotice{Stopped: true, Account: "imap.fastmail.com", Listed: 3402, LastGrew: nov4, LastBehind: now},
			"The account kept in the folder imap.fastmail.com: none of its 3,402 messages downloaded yet, no progress since Nov 4."},
		{TBNotice{Stopped: true, Account: "imap.fastmail.com", Listed: 1, LastGrew: nov4, LastBehind: now},
			"The account kept in the folder imap.fastmail.com: its 1 message is not downloaded yet, no progress since Nov 4."},
		{TBNotice{Stopped: true, Account: "imap.gmail.com", Listed: 56611, Floor: true, LastGrew: nov4, LastBehind: now},
			"The account kept in the folder imap.gmail.com: none of its messages (at least 56,611) downloaded yet, no progress since Nov 4."},
		{TBNotice{Account: "imap.gmail.com", OnDisk: 400, Listed: 56611, Floor: true, FirstBehind: now.Add(-5 * 24 * time.Hour), LastBehind: now},
			"The account kept in the folder imap.gmail.com: about 400 of at least 56,611 messages downloaded, still going after 5 days."},
	} {
		if got := TBNoticeEmail([]TBNotice{c.n}, now).Lines; len(got) != 1 || got[0] != c.want {
			t.Errorf("got %q\nwant %q", got, c.want)
		}
	}

	// Nothing the words must never say (§9.3, the friction review's L3 and L6).
	all := append(append(append([]string(nil), e.Lines...), two.Lines...), append(two.Remedy, e.Which)...)
	for _, s := range all {
		for _, bad := range []string{"‮", "\u001b", "Inbox", "INBOX", "Furthest behind", "store", "cache", "msf", "none of at least", "Thunderbird account kept", "@"} {
			if strings.Contains(s, bad) {
				t.Errorf("the email says %q: %q", bad, s)
			}
		}
	}
	if e := TBNoticeEmail(nil, now); e.Lines != nil || e.Remedy != nil || e.Stopped {
		t.Errorf("no notice gave %+v", e)
	}
}
