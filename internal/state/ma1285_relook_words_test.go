package state

import (
	"strings"
	"testing"
)

// The C3 friction re-look's word changes (docs/review-thunderbird-completeness-c3-friction.md,
// "Re-look after the fold rounds (2026-11-08)", R1 to R6, the lead's calls of 2026-11-09). Each
// sentence is pinned exactly; positive twins first.

// covers: MA-1285
// R1: the Gmail floor's "(see below to keep it)" is true only where the not-kept folder's line is
// said below it (the wizard, Run now and the command line say an account's lines in order). Where
// the lines are said apart, ordered by how much they matter (`status`, Status & health), the
// sentence names the line instead: the folder as TBNotKeptLine names it.
func TestTheGmailFloorNamesTheLineWhereItIsNotBelow(t *testing.T) {
	a := TBAccount{Account: "imap.gmail.com", Name: "sam.walker@gmail.com", Verdict: "unknown", Reason: "gmail_floor", Gmail: true, Floor: true,
		AllMail: "not_kept", OnDisk: 50, Listed: 40, NotKept: []TBFolder{{Folder: "[Gmail]/All Mail", Listed: 900}}, Folders: []TBFolder{{Folder: "INBOX", Listed: 50, OnDisk: 50}}}
	const head = "About 50 messages of sam.walker@gmail.com are on this computer. MailArchive cannot tell how many more there are, because Gmail's All Mail folder, which holds all of this account's mail, is not kept on this computer "
	lines := TBAccountLines(a, TBSay{Now: tbNow})
	if len(lines) != 2 || lines[0] != head+"(see below to keep it)." || !strings.Contains(lines[1], "[Gmail]/All Mail (900 messages)") {
		t.Fatalf("positive twin: in order, the line that keeps All Mail is below: %q", lines)
	}
	wantLine(t, "said apart (status, Status & health)", TBHeadline(a, TBSay{Now: tbNow, Apart: true}),
		head+"(the line that names [Gmail]/All Mail says how to keep it).")
	a.NotKept = []TBFolder{{Folder: "Receipts", Listed: 2000}, {Folder: "[Google Mail]/All Mail", Listed: 900}}
	wantLine(t, "Gmail's other name for its folders, as the not-kept line names it", TBHeadline(a, TBSay{Now: tbNow, Apart: true}),
		head+"(the line that names [Google Mail]/All Mail says how to keep it).")
	a.NotKept = nil
	wantLine(t, "no All Mail folder in the record", TBHeadline(a, TBSay{Now: tbNow, Apart: true}),
		head+"(the line that names [Gmail]/All Mail says how to keep it).")
	// No folder of the account kept: the not-kept line is the account's own setting, which names no
	// folder, so the sentence names that line by its words.
	a.NotKept, a.Folders = []TBFolder{{Folder: "[Gmail]/All Mail", Listed: 900}}, nil
	if !strings.HasPrefix(TBNotKeptLine(a), "Thunderbird is not set to keep sam.walker@gmail.com's mail on this computer") {
		t.Fatalf("the account-level not-kept line: %q", TBNotKeptLine(a))
	}
	wantLine(t, "no folder kept", TBHeadline(a, TBSay{Now: tbNow, Apart: true}),
		head+"(the line that says Thunderbird is not set to keep sam.walker@gmail.com's mail says how to keep it).")
}

// covers: MA-1285
// R2: with two or more accounts behind, what to do is said once, after every account's lines (so
// never between one account's folder line and the next account), before the sentence for accounts
// the record left out, and it says whom it is for. Each surface keeps its own last clause (P3).
func TestTheSharedRemedyComesAfterEveryAccount(t *testing.T) {
	b1 := TBAccount{Account: "imap.gmail.com", Name: "sam.walker@gmail.com", Verdict: "behind", Gmail: true, OnDisk: 120, Listed: 900}
	b2 := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "behind", OnDisk: 100, Listed: 500,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 500, OnDisk: 100}}, NotKept: []TBFolder{{Folder: "Projects", Listed: 3}}}
	cu := tbOtherCaughtUp()
	cu.Name = "sam@work.example"
	sched := TBSay{Scheduled: true, Now: tbNow}
	if got := TBLines([]TBAccount{b1, cu}, 0, sched); len(got) != 2 || got[0] != TBHeadline(b1, sched) || got[1] != TBHeadline(cu, sched) {
		t.Fatalf("positive twin: one account behind keeps its own remedy: %q", got)
	}
	h1 := "Thunderbird has downloaded about 120 of 900 messages in sam.walker@gmail.com so far."
	h2 := "Thunderbird has downloaded about 100 of 500 messages in sam@fastmail.com so far. Furthest behind: Inbox 100 of 500."
	const lead = "For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone)"
	for _, c := range []struct {
		what   string
		say    TBSay
		remedy string
	}{
		{"scheduled", sched, lead + "; later backups add the rest."},
		{"no schedule, the wizard", TBSay{Now: tbNow}, lead + ", then run the backup again to add the rest."},
		{"no schedule, Run now", TBSay{Now: tbNow, Surface: TBSurfaceRunNow}, lead + ", then click Run now again to add the rest."},
		{"no schedule, the command line", TBSay{Now: tbNow, Surface: TBSurfaceCommandLine}, lead + ", then run the same command again to add the rest."},
	} {
		got := TBLines([]TBAccount{b1, b2, cu}, 1, c.say)
		want := []string{h1, h2, TBNotKeptLine(b2), TBHeadline(cu, c.say), c.remedy, TBMoreSentence(1)}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s (cell L's order):\n got %q\nwant %q", c.what, got, want)
		}
	}
	three := TBLines([]TBAccount{b1, b2, {Account: "imap.c.example", Verdict: "behind", OnDisk: 1, Listed: 9}}, 0, sched)
	if last := three[len(three)-1]; !strings.HasPrefix(last, "For the 3 accounts still downloading: ") {
		t.Errorf("three behind: %q", three)
	}
}

// covers: MA-1285
// R3: an account Thunderbird has not downloaded anything of yet says so, never "about 0 of N", on
// the headline, Recent backups and the card. "Furthest behind: Inbox 0 of 1,200" stays. Positive
// twin first: one message downloaded says "about 1 of".
func TestNothingDownloadedYetIsNotAbout0(t *testing.T) {
	a := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "behind", OnDisk: 1, Listed: 1200,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 1200, OnDisk: 1}}}
	sched := TBSay{Scheduled: true, Now: tbNow}
	if h := TBHeadline(a, sched); !strings.HasPrefix(h, "Thunderbird has downloaded about 1 of 1,200 messages in sam@fastmail.com so far.") {
		t.Fatalf("positive twin: %q", h)
	}
	a.OnDisk, a.Folders[0].OnDisk = 0, 0
	const leave = " Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest."
	wantLine(t, "the headline", TBHeadline(a, sched),
		"Thunderbird has not downloaded any of the 1,200 messages in sam@fastmail.com yet. Furthest behind: Inbox 0 of 1,200."+leave)
	stale := a
	stale.Stale, stale.AsOf = true, tbNow.AddDate(0, 0, -6)
	wantLine(t, "stale", TBHeadline(stale, sched),
		"Thunderbird has not downloaded any of the 1,200 messages in sam@fastmail.com yet (Thunderbird's count from "+tbDate(stale.AsOf, sched)+"). Furthest behind: Inbox 0 of 1,200."+leave)
	one := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 0, Listed: 1}
	wantLine(t, "one message", TBHeadline(one, sched), "Thunderbird has not downloaded the 1 message in "+tbFolderExample+" yet."+leave)
	floor := TBAccount{Account: "imap.gmail.com", Name: "sam.walker@gmail.com", Verdict: "behind", Gmail: true, Floor: true, OnDisk: 0, Listed: 1200}
	wantLine(t, "a floor", TBHeadline(floor, sched), "Thunderbird has not downloaded any of at least 1,200 messages in sam.walker@gmail.com yet."+leave)

	s, behind := TBCompactSentence(TBCompact{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "behind", X: 0, Y: 1200})
	wantLine(t, "Recent backups", s, "Thunderbird has not downloaded any of the 1,200 messages in sam@fastmail.com yet")
	if !behind {
		t.Errorf("Recent backups: an account with nothing downloaded is still behind")
	}
	wantLine(t, "the card", TBCardBanner([]TBAccount{a}),
		"Thunderbird is still downloading sam@fastmail.com: none of its 1,200 messages are on this computer yet — see Status & health.")
	wantLine(t, "the card, one message", TBCardBanner([]TBAccount{one}),
		"Thunderbird is still downloading "+tbFolderExample+": its 1 message is not on this computer yet — see Status & health.")
	wantLine(t, "the card, a floor", TBCardBanner([]TBAccount{floor}),
		"Thunderbird is still downloading sam.walker@gmail.com: none of at least 1,200 messages are on this computer yet — see Status & health.")
}

// covers: MA-1285
// R4: in one warning, an account with no name is said in the full fallback words once (the
// headline); the limit sentence after it says "this account's mail"; the fix-it step keeps the full
// words, since that is how the person finds the account in Account Settings. Positive twin first:
// a named account keeps its short name everywhere.
func TestTheFallbackNameIsSaidOnceInAWarning(t *testing.T) {
	const menu = "in Thunderbird, click the menu button (≡, top right), then Account Settings; "
	named := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "limited", OnDisk: 170, Listed: 2300,
		ThunderbirdVersion: 153, Limit: &TBLimit{MaxAgeDays: 30}}
	if l := TBLimitLines(named); len(l) != 1 || l[0] != `Thunderbird is set to keep only the most recent 30 days of mail for sam@fastmail.com on this computer, so older mail is not backed up. To back up all of it: `+menu+`on sam@fastmail.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".` {
		t.Fatalf("positive twin: %q", l)
	}
	const full = "the Thunderbird account kept in the folder imap.fastmail.com"
	a := named
	a.Name = ""
	wantLine(t, "the headline names it", TBHeadline(a, TBSay{}), "Thunderbird has downloaded about 170 of 2,300 messages in "+full+".")
	wantLine(t, "age (P-R4)", strings.Join(TBLimitLines(a), " "),
		`Thunderbird is set to keep only the most recent 30 days of this account's mail on this computer, so older mail is not backed up. To back up all of it: `+menu+`on the "Synchronization & Storage" page of `+full+`, choose "Synchronize all messages locally regardless of age".`)
	a.Limit = &TBLimit{MaxSizeKB: 500}
	wantLine(t, "size", strings.Join(TBLimitLines(a), " "),
		`Thunderbird is set not to download messages larger than 500 KB for this account, so those are not backed up. To back them up: `+menu+`on the "Synchronization & Storage" page of `+full+`, untick "Don't download messages larger than".`)
	a.Limit = &TBLimit{NoBackgroundDownload: true}
	wantLine(t, "background", strings.Join(TBLimitLines(a), " "),
		"Thunderbird is set not to download this account's mail in the background, so MailArchive backs up only what you have opened or downloaded by hand. In Thunderbird, click the menu button (≡, top right), then Account Settings; for "+full+", turn on the setting that saves copies of this account's messages on this computer.")
	a.Limit = nil
	wantLine(t, "no limit read", strings.Join(TBLimitLines(a), " "),
		`Thunderbird's own settings keep only part of this account's mail on this computer, so the rest is not backed up. To check: in Thunderbird, click the menu button (≡, top right), then Account Settings, and open the "Synchronization & Storage" page of `+full+`.`)
	// The whole limited warning, as status says it, names the fallback twice: once to say which
	// account, once in the step that finds it.
	a.Limit = &TBLimit{MaxAgeDays: 30}
	if warn := TBHeadline(a, TBSay{}) + " " + strings.Join(TBLimitLines(a), " "); strings.Count(warn, full) != 2 {
		t.Errorf("the fallback is said %d times in one warning: %q", strings.Count(warn, full), warn)
	}
	// With no headline (Thunderbird lists nothing), the limit sentence is the first to say which
	// account, so it keeps the full words.
	a.Listed = 0
	if l := TBLimitLines(a); len(l) != 1 || !strings.Contains(l[0], "30 days of mail for "+full+" on this computer") {
		t.Errorf("no headline: %q", l)
	}
}

// covers: MA-1285
// R6: an account's name is said with "mail" where the sentence means the account's mail ("all of
// sam@fastmail.com's mail", never "all of sam@fastmail.com"); with no name, "the mail of the
// Thunderbird account kept in the folder …"; with nothing visible, "this account's mail".
func TestAllOfTheAccountsMail(t *testing.T) {
	a := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "unknown", Reason: "walk_failed"}
	wantLine(t, "a name", TBHeadline(a, TBSay{}), "MailArchive cannot tell whether Thunderbird has downloaded all of sam@fastmail.com's mail, because this backup could not read all of its mail.")
	a.Reason = "panorama"
	wantLine(t, "panorama", TBHeadline(a, TBSay{}), "This Thunderbird uses its newer storage, which MailArchive cannot read yet, so it cannot tell whether Thunderbird has downloaded all of sam@fastmail.com's mail.")
	a.Reason = "partial_input"
	wantLine(t, "partial input", TBHeadline(a, TBSay{}), "Only part of sam@fastmail.com's mail was backed up, so MailArchive did not compare it with Thunderbird's count.")
	a.Name = "AT&T Mail" // a name that already says "mail" is never "AT&T Mail's mail"
	wantLine(t, "a name ending in mail", TBHeadline(a, TBSay{}), "Only part of AT&T Mail was backed up, so MailArchive did not compare it with Thunderbird's count.")
	a.Reason, a.Name = "walk_failed", ""
	wantLine(t, "no name", TBHeadline(a, TBSay{}), "MailArchive cannot tell whether Thunderbird has downloaded all of the mail of the Thunderbird account kept in the folder imap.fastmail.com, because this backup could not read all of its mail.")
	a.Account = "​"
	wantLine(t, "nothing visible", TBHeadline(a, TBSay{}), "MailArchive cannot tell whether Thunderbird has downloaded all of this account's mail, because this backup could not read all of its mail.")
	s, _ := TBCompactSentence(TBCompact{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "unknown"})
	wantLine(t, "Recent backups", s, "MailArchive could not tell how much of sam@fastmail.com's mail Thunderbird has downloaded")
	// No reason's sentence says "all of" a name or "how much of" a name without its mail.
	for _, r := range []string{"panorama", "walk_failed", "cache_unreadable", "no_match", "old_path_only", "not_counted_yet", "count_uncertain"} {
		h := TBHeadline(TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "unknown", Reason: r}, TBSay{})
		if strings.Contains(h, "all of sam@fastmail.com,") || strings.Contains(h, "all of sam@fastmail.com.") {
			t.Errorf("reason %s: %q", r, h)
		}
	}
}
