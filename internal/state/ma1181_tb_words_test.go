package state

import (
	"strings"
	"testing"
	"time"
)

// The completeness words of slice C3 (docs/design-thunderbird-completeness.md §7, §6, §4.4, §2.3,
// §3), built once here and said by every surface. These pin each sentence exactly.

var tbNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)

func tbGmailCaughtUp() TBAccount {
	return TBAccount{Account: "imap.gmail.com", Verdict: "caught_up", Complete: true, Gmail: true, AllMail: "sets_y",
		OnDisk: 238169, Listed: 238169, Tolerance: 476, AsOf: tbNow.Add(-time.Hour)}
}

func tbOtherCaughtUp() TBAccount {
	return TBAccount{Account: "imap.example.com", Verdict: "caught_up", Complete: true, OnDisk: 3402, Listed: 3402, Tolerance: 20,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 3000, OnDisk: 3000}, {Folder: "Sent", Listed: 402, OnDisk: 402}}, AsOf: tbNow.Add(-time.Hour)}
}

func wantLine(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

// covers: MA-1181
// The owner-shaped run, as its record keeps it: Gmail caught up and another account caught up,
// with nothing missing (Local Folders is never in the record, C2): each line says "everything".
// Then each thing that keeps a caught-up account from saying complete gets its own one sentence
// why, with both numbers, and never "everything" (R42 as amended).
func TestCaughtUpWordsSayEverythingOnlyWhenComplete(t *testing.T) {
	say := TBSay{Scheduled: true, Now: tbNow}
	wantLine(t, "Gmail caught up", TBHeadline(tbGmailCaughtUp(), say), "Thunderbird has downloaded everything it lists for imap.gmail.com (238,169 messages).")
	wantLine(t, "the other account caught up", TBHeadline(tbOtherCaughtUp(), say), "Thunderbird has downloaded everything it lists for imap.example.com (3,402 messages).")
	if got := TBLines([]TBAccount{tbGmailCaughtUp(), tbOtherCaughtUp()}, 0, say); len(got) != 2 {
		t.Errorf("the owner-shaped lines: %q", got)
	}

	a := tbOtherCaughtUp()
	a.Complete, a.Missing, a.OnDisk = false, 2, 3400
	wantLine(t, "2 missing, within the tolerance", TBHeadline(a, say),
		"Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for imap.example.com. The rest is likely mail that arrived or was deleted since Thunderbird's count.")
	a.Stale, a.AsOf = true, time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)
	wantLine(t, "2 missing and stale", TBHeadline(a, say),
		"Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for imap.example.com. The rest is likely mail that arrived or was deleted since Thunderbird's count. These numbers are Thunderbird's count from Nov 2. Open Thunderbird and leave it open so it can count again.")

	s := tbOtherCaughtUp()
	s.Complete, s.Stale, s.AsOf = false, true, time.Date(2025, 12, 30, 9, 0, 0, 0, time.Local)
	wantLine(t, "stale with nothing missing (M9)", TBHeadline(s, say),
		"As of Dec 30, 2025, Thunderbird's count matched what is on this computer for imap.example.com (3,402 messages). It has not counted since, so newer mail may not be here yet.")

	d := tbOtherCaughtUp()
	d.Complete, d.DeletedCopies, d.OnDisk = false, true, 3398
	wantLine(t, "deleted copies (M7)", TBHeadline(d, say),
		"Thunderbird lists 3,402 messages for imap.example.com and about 3,398 are on this computer. MailArchive cannot be exact, because Thunderbird still keeps copies of deleted messages until it compacts its folders.")

	n := tbGmailCaughtUp()
	n.Complete, n.NoIdentity = false, 1
	wantLine(t, "a message with no identity", TBHeadline(n, say),
		"Thunderbird has downloaded about 238,169 of the 238,169 messages it lists for imap.gmail.com, but MailArchive could not tell 1 message apart from the others, so it cannot be sure of the count.")

	k := tbOtherCaughtUp()
	k.Complete, k.NotKept = false, []TBFolder{{Folder: "Projects", Listed: 772}}
	wantLine(t, "a not-kept folder (L4)", TBHeadline(k, say),
		"Thunderbird has downloaded the 3,402 messages it lists in the folders it keeps on this computer for imap.example.com.")

	c := tbGmailCaughtUp()
	c.Complete, c.CapNote, c.OnDisk, c.Listed = false, 1000, 1000, 1000
	wantLine(t, "the cap note's headline (TC-22)", TBHeadline(c, say), "Thunderbird has downloaded the 1,000 messages it lists for imap.gmail.com.")
	wantLine(t, "the cap note (TC-22)", TBCapNote(c),
		`Gmail may be showing Thunderbird only the newest 1,000 messages of each folder of imap.gmail.com. In Gmail's settings, find "Folder size limits" and choose not to limit the number of messages.`)

	e := TBAccount{Account: "imap.empty.example", Verdict: "caught_up", Complete: true, ListsNoMail: true}
	wantLine(t, "lists no mail", TBHeadline(e, say), "Thunderbird lists no mail for imap.empty.example, so there is nothing more to download.")

	// A planted record that says complete over its own numbers never gets "everything".
	p := tbOtherCaughtUp()
	p.Missing, p.OnDisk = 5, 3397
	if h := TBHeadline(p, say); strings.Contains(h, "everything") || p.MaySayComplete() {
		t.Errorf("a record saying complete with 5 missing: %q (may say complete %v)", h, p.MaySayComplete())
	}
	p = tbOtherCaughtUp()
	p.Verdict = "behind"
	if p.MaySayComplete() {
		t.Error("a behind record with complete true may say complete")
	}
	// Every other blocker, planted beside complete:true, keeps "everything" out too.
	for what, plant := range map[string]func(*TBAccount){
		"stale":                    func(a *TBAccount) { a.Stale = true },
		"a not-kept folder":        func(a *TBAccount) { a.NotKept = []TBFolder{{Folder: "Projects", Listed: 1}} },
		"not-kept beyond the list": func(a *TBAccount) { a.NotKeptMore = 1 },
		"deleted copies":           func(a *TBAccount) { a.DeletedCopies = true },
		"the cap note":             func(a *TBAccount) { a.CapNote = 1000 },
		"no identity":              func(a *TBAccount) { a.NoIdentity = 1 },
		"split":                    func(a *TBAccount) { a.SplitMailboxes = 1 },
		"glued":                    func(a *TBAccount) { a.GluedMailboxes = 1 },
		"misaligned":               func(a *TBAccount) { a.MisalignedMailboxes = 1 },
	} {
		p := tbOtherCaughtUp()
		plant(&p)
		if h := TBHeadline(p, say); p.MaySayComplete() || strings.Contains(h, "everything") {
			t.Errorf("a record saying complete with %s: may say complete %v, %q", what, p.MaySayComplete(), h)
		}
	}
}

// covers: MA-1181
// One account behind says both numbers (O2) and, for an account that is not Gmail, the folders
// furthest behind; a floor says "at least" (R43); a stale count is dated; and "later backups add
// the rest" is said only when a schedule will make it true (TC-17). Positive twin first: the
// scheduled words.
func TestBehindWordsSayBothNumbersAndWhatToDo(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1670, Listed: 14201, Missing: 12531, Tolerance: 28,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 10799, OnDisk: 1200}, {Folder: "Sent", Listed: 3402, OnDisk: 0}, {Folder: "Drafts", Listed: 0, OnDisk: 470}}}
	wantLine(t, "behind, scheduled", TBHeadline(a, TBSay{Scheduled: true, Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of 14,201 messages in imap.example.com so far. Furthest behind: INBOX 1,200 of 10,799; Sent 0 of 3,402. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	wantLine(t, "behind, no schedule (TC-17)", TBHeadline(a, TBSay{Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of 14,201 messages in imap.example.com so far. Furthest behind: INBOX 1,200 of 10,799; Sent 0 of 3,402. Leave Thunderbird open (it downloads mostly while the computer is left alone). Run the backup again once Thunderbird has downloaded more, to add the rest.")

	g := TBAccount{Account: "imap.gmail.com", Verdict: "behind", Gmail: true, OnDisk: 1670, Listed: 238080,
		Folders: []TBFolder{{Folder: "[Gmail]/Sent Mail", Listed: 33822, OnDisk: 2}}}
	wantLine(t, "Gmail behind: never per folder (§3)", TBHeadline(g, TBSay{Scheduled: true, Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of 238,080 messages in imap.gmail.com so far. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	g.Floor, g.Listed, g.Stale, g.AsOf = true, 56611, true, time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)
	wantLine(t, "a floor, stale", TBHeadline(g, TBSay{Scheduled: true, Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of at least 56,611 messages in imap.gmail.com so far (Thunderbird's count from Nov 2). Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	for _, h := range []string{TBHeadline(a, TBSay{Now: tbNow}), TBHeadline(g, TBSay{Now: tbNow})} {
		if strings.Contains(h, "complete") || strings.Contains(h, "everything") {
			t.Errorf("a behind account says complete: %q", h)
		}
	}
}

// covers: MA-1181
// Every way MailArchive cannot tell (§4.4's fold table and the reason codes C1b and C2 left for
// C3) is one sentence that says why, never "complete" or "everything".
func TestUnknownWordsSayWhyInOneSentence(t *testing.T) {
	pre := "MailArchive cannot tell whether Thunderbird has downloaded all of imap.example.com, because "
	for reason, want := range map[string]string{
		"cache_unreadable":  pre + "it could not read Thunderbird's list of folders.",
		"duplicate_key":     pre + "it could not read Thunderbird's list of folders.",
		"invalid_entry":     pre + "it could not read Thunderbird's list of folders.",
		"":                  pre + "it could not read Thunderbird's list of folders.",
		"some_future_code":  pre + "it could not read Thunderbird's list of folders.",
		"no_match":          pre + "MailArchive does not yet know how this kind of Thunderbird account keeps its mail.",
		"account_type":      pre + "MailArchive does not yet know how this kind of Thunderbird account keeps its mail.",
		"not_counted_yet":   pre + "Thunderbird has not counted all of its folders yet. Open Thunderbird and leave it open.",
		"expunged_unsized":  pre + "Thunderbird holds copies of deleted messages that MailArchive cannot count.",
		"split_differently": pre + "some of its mail files are laid out in a way MailArchive could not count message by message.",
		"count_uncertain":   pre + "MailArchive could not count some of its messages for certain.",
		"other_reader":      pre + "this backup read its mail the way it reads another mail app's, which does not count it.",
		"walk_failed":       pre + "this backup could not read all of its mail.",
		"old_path_only":     pre + "Thunderbird's list of folders still names 2 folders by where they used to be. This clears the next time Thunderbird saves that list (leave it open for a minute).",
		"panorama":          "This Thunderbird uses its newer storage, which MailArchive cannot read yet, so it cannot tell whether Thunderbird has downloaded all of imap.example.com.",
		"never_checked":     "Thunderbird has not checked the server for imap.example.com yet, so MailArchive cannot tell how much is still to come. Open Thunderbird and leave it open.",
		"partial_input":     "Only part of this Thunderbird account (imap.example.com) was backed up, so MailArchive did not compare it with Thunderbird's count.",
	} {
		a := TBAccount{Account: "imap.example.com", Verdict: "unknown", Reason: reason, CantTellFolders: 2, OnDisk: 5, Listed: 9}
		got := TBHeadline(a, TBSay{Now: tbNow})
		wantLine(t, "reason "+reason, got, want)
		if strings.Contains(got, "complete") || strings.Contains(got, "everything") {
			t.Errorf("reason %s says complete: %q", reason, got)
		}
	}
	for use, clause := range map[string]string{"not_kept": "is not kept on this computer", "not_shown": "is not shown to Thunderbird",
		"not_checked": "has not been checked by Thunderbird yet", "below_largest": "lists fewer messages than another of its folders", "": "is not shown to Thunderbird"} {
		a := TBAccount{Account: "imap.gmail.com", Verdict: "unknown", Reason: "gmail_floor", Gmail: true, Floor: true, AllMail: use, OnDisk: 56700, Listed: 56611}
		wantLine(t, "O3, All Mail "+use, TBHeadline(a, TBSay{Now: tbNow}),
			"About 56,700 messages are on this computer. Thunderbird lists at least 56,611 for imap.gmail.com. MailArchive cannot tell whether that is all of it, because Gmail's All Mail "+clause+".")
	}
}

// covers: MA-1185
// Folders not kept on this computer (§6): named with Thunderbird's own words to keep them, the
// first 10 by listed count and "and N more"; when no folder of the account is kept, the account's
// own setting, labelled for the Thunderbird version that last opened the profile (L5: below 160
// the older label, 160 and above the newer one, both when the version cannot be told). Spam and
// Trash, and Gmail's labels while All Mail backs them up, are notes only. Positive twin first:
// an account with every folder kept says nothing of it.
func TestNotKeptWordsNameTheFoldersAndTheFix(t *testing.T) {
	if s := TBNotKeptLine(tbOtherCaughtUp()); s != "" || len(TBNotes(tbOtherCaughtUp())) != 0 {
		t.Fatalf("positive twin: every folder kept: %q %q", s, TBNotes(tbOtherCaughtUp()))
	}
	a := tbOtherCaughtUp()
	a.NotKept = []TBFolder{{Folder: "Projects", Listed: 772}, {Folder: "Receipts", Listed: 33}}
	wantLine(t, "two folders", TBNotKeptLine(a),
		`These folders of imap.example.com are not kept on this computer, so MailArchive cannot back them up: Projects (772 messages), Receipts (33 messages). To keep them: in Thunderbird, right-click each folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`)
	a.NotKept, a.NotKeptMore = a.NotKept[:1], 0
	a.NotKept[0].Listed = 1
	wantLine(t, "one folder", TBNotKeptLine(a),
		`This folder of imap.example.com is not kept on this computer, so MailArchive cannot back it up: Projects (1 message). To keep it: in Thunderbird, right-click the folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`)
	var ten []TBFolder
	for i := 0; i < 10; i++ {
		ten = append(ten, TBFolder{Folder: "F" + string(rune('A'+i)), Listed: 10})
	}
	a.NotKept, a.NotKeptMore = ten, 32
	if s := TBNotKeptLine(a); !strings.Contains(s, "FJ (10 messages) and 32 more. To keep them") || strings.Count(s, "(10 messages)") != 10 {
		t.Errorf("ten named and 32 more: %q", s)
	}

	all := TBAccount{Account: "imap.example.com", Verdict: "unknown", Reason: "cache_unreadable", NotKept: []TBFolder{{Folder: "INBOX", Listed: 9}}}
	for v, want := range map[int]string{
		153: `Thunderbird is not set to keep imap.example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, tick "Keep messages in all folders for this account on this computer".`,
		159: `Thunderbird is not set to keep imap.example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, tick "Keep messages in all folders for this account on this computer".`,
		160: `Thunderbird is not set to keep imap.example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird's Account Settings, under imap.example.com's "Offline message storage", tick "Save copies of all folders and messages on this computer for offline use".`,
		0:   `Thunderbird is not set to keep imap.example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, tick "Keep messages in all folders for this account on this computer" (in Thunderbird 160 and later: under "Offline message storage", tick "Save copies of all folders and messages on this computer for offline use").`,
	} {
		all.ThunderbirdVersion = v
		wantLine(t, "no folder kept, version "+string(rune('0'+v%10)), TBNotKeptLine(all), want)
	}

	notes := TBNotes(TBAccount{Account: "imap.gmail.com", DefaultNotKept: []TBFolder{{Folder: "[Gmail]/Spam", Listed: 3}}, LabelsNotKept: 40})
	if len(notes) != 2 {
		t.Fatalf("the notes: %q", notes)
	}
	wantLine(t, "Spam and Trash", notes[0], "Thunderbird does not keep Spam and Trash of imap.gmail.com on this computer (its usual setting), so they are not backed up.")
	wantLine(t, "Gmail labels (TC-8)", notes[1], "Some of imap.gmail.com's labels are not kept on this computer as folders of their own. Their mail is still backed up through All Mail.")
}

// covers: MA-1185
// A limited account (§6's limited words, TC-9): both numbers, each limit in Thunderbird's own
// words for the version (L5), background download off in neutral words (its label is still owed
// by lab row MA-1191).
func TestLimitedWordsNameTheSetting(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Verdict: "limited", OnDisk: 900, Listed: 5000, Limit: &TBLimit{MaxAgeDays: 30, MaxSizeKB: 50}}
	say := TBSay{Scheduled: true, Now: tbNow}
	wantLine(t, "limited headline", TBHeadline(a, say), "Thunderbird has downloaded about 900 of 5,000 messages in imap.example.com.")
	want := map[int][]string{
		153: {`Thunderbird is set to keep only the most recent 30 days of mail for imap.example.com on this computer, so older mail is not backed up. To back up all of it: in Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".`,
			`Thunderbird is set not to download messages larger than 50 KB for imap.example.com, so those are not backed up. To back them up: in Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, untick "Don't download messages larger than".`},
		160: {`Thunderbird is set to keep only the most recent 30 days of mail for imap.example.com on this computer, so older mail is not backed up. To back up all of it: in Thunderbird's Account Settings for imap.example.com, set it to keep messages of every age on this computer.`,
			`Thunderbird is set not to download messages larger than 50 KB for imap.example.com, so those are not backed up. To back them up: in Thunderbird's Account Settings, under imap.example.com's "Download limits", untick "Skip downloading messages larger than:".`},
		0: {`Thunderbird is set to keep only the most recent 30 days of mail for imap.example.com on this computer, so older mail is not backed up. To back up all of it: in Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".`,
			`Thunderbird is set not to download messages larger than 50 KB for imap.example.com, so those are not backed up. To back them up: in Thunderbird's Account Settings, on imap.example.com's "Synchronization & Storage" page, untick "Don't download messages larger than" (in Thunderbird 160 and later: under "Download limits", untick "Skip downloading messages larger than:").`},
	}
	for v, w := range want {
		a.ThunderbirdVersion = v
		got := TBLimitLines(a)
		if len(got) != 2 {
			t.Fatalf("version %d: %q", v, got)
		}
		wantLine(t, "age limit", got[0], w[0])
		wantLine(t, "size limit", got[1], w[1])
	}
	b := TBAccount{Account: "imap.example.com", Verdict: "limited", Limit: &TBLimit{NoBackgroundDownload: true}}
	if h := TBHeadline(b, say); h != "" {
		t.Errorf("limited with nothing listed: no numbers to say, got %q", h)
	}
	if got := TBAccountLines(b, say); len(got) != 1 || got[0] != "Thunderbird is set not to download imap.example.com's mail in the background, so MailArchive backs up only what you have opened or downloaded by hand. In Thunderbird's Account Settings for imap.example.com, turn on the setting that saves copies of this account's messages on this computer." {
		t.Errorf("background download off (TC-9): %q", got)
	}
}

// covers: MA-1181
// Recent backups keeps only the verdict and the two numbers (§9.1): a behind row's words and its
// flag, a floor as "at least" (R43), and nothing for an account caught up with nothing missing.
// The archive card's one line points at Status & health. Accounts past the record's cap are
// never said complete (R2-06).
func TestTheShortWords(t *testing.T) {
	if s, behind := TBCompactSentence(TBCompact{Account: "imap.gmail.com", Verdict: "caught_up", X: 9, Y: 9}); s != "" || behind {
		t.Fatalf("positive twin: caught up with nothing missing: %q %v", s, behind)
	}
	s, behind := TBCompactSentence(TBCompact{Account: "imap.gmail.com", Verdict: "behind", X: 1670, Y: 56611, Floor: true})
	wantLine(t, "behind, a floor", s, "Thunderbird has downloaded about 1,670 of at least 56,611 messages in imap.gmail.com so far")
	if !behind {
		t.Error("a behind entry is not told behind")
	}
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "caught_up", X: 3400, Y: 3402})
	wantLine(t, "caught up, 2 missing", s, "Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for imap.example.com")
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "limited", X: 900, Y: 5000})
	wantLine(t, "limited", s, "Thunderbird's own settings keep only part of imap.example.com's mail on this computer (about 900 of 5,000 messages)")
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "unknown"})
	wantLine(t, "unknown", s, "MailArchive could not tell how much of imap.example.com Thunderbird has downloaded")

	if b := TBCardBanner([]TBAccount{tbGmailCaughtUp()}); b != "" {
		t.Errorf("positive twin: a caught-up archive's card: %q", b)
	}
	wantLine(t, "the card, one behind", TBCardBanner([]TBAccount{{Account: "imap.gmail.com", Verdict: "behind", OnDisk: 1670, Listed: 238080}}),
		"Thunderbird is still downloading imap.gmail.com: about 1,670 of 238,080 messages are on this computer so far — see Status & health.")
	wantLine(t, "the card, two behind and not-kept folders", TBCardBanner([]TBAccount{{Account: "a", Verdict: "behind"}, {Account: "b", Verdict: "behind", NotKept: []TBFolder{{Folder: "x", Listed: 1}}, NotKeptMore: 2}}),
		"Thunderbird is still downloading 2 accounts; 3 folders are not kept on this computer, so they are not backed up — see Status & health.")

	wantLine(t, "one account left out", TBMoreSentence(1), "MailArchive did not keep the details of 1 more Thunderbird account in this backup's record, so it does not say it is complete.")
	if got := TBLines(nil, 3, TBSay{}); len(got) != 1 || !strings.Contains(got[0], "3 more Thunderbird accounts") {
		t.Errorf("accounts left out: %q", got)
	}
}

// covers: MA-1198
// Folder and account names are text the mail server controls (TC-7): every sentence says them
// cleaned (control characters, a bidi override and invisible characters gone) and capped at 80
// characters plus "…"; a name of nothing visible is said as "(a folder with no name)" or "this
// account". Positive twin first: ordinary names are said as they are.
func TestHostileNamesAreCleanedInEveryWord(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1, Listed: 9,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 9, OnDisk: 1}}, NotKept: []TBFolder{{Folder: "Projects", Listed: 3}}}
	if lines := strings.Join(TBAccountLines(a, TBSay{Now: tbNow}), "\n"); !strings.Contains(lines, "INBOX 1 of 9") || !strings.Contains(lines, "Projects (3 messages)") {
		t.Fatalf("positive twin: %q", lines)
	}
	long := strings.Repeat("x", 10*1024)
	a.Account = "imap.\u202eevil\x1b[31m.com"
	a.Folders[0].Folder = "IN\u202eBOX\x07\u200b"
	a.NotKept = []TBFolder{{Folder: long, Listed: 3}, {Folder: "\u2066\u200b\x00", Listed: 2}}
	lines := strings.Join(append(TBAccountLines(a, TBSay{Now: tbNow}), TBCardBanner([]TBAccount{a})), "\n")
	for _, bad := range []string{"\u202e", "\x1b", "\x07", "\u200b", "\u2066", "\x00", strings.Repeat("x", 81)} {
		if strings.Contains(lines, bad) {
			t.Errorf("the words hold %q:\n%s", bad, lines)
		}
	}
	for _, good := range []string{"imap.evil[31m.com", "INBOX 1 of 9", strings.Repeat("x", 80) + "… (3 messages)", "(a folder with no name) (2 messages)"} {
		if !strings.Contains(lines, good) {
			t.Errorf("the words miss %q:\n%s", good, lines)
		}
	}
	if h := TBHeadline(TBAccount{Account: "\u200b", Verdict: "unknown"}, TBSay{}); !strings.Contains(h, "all of this account,") {
		t.Errorf("an account of nothing visible: %q", h)
	}
}
