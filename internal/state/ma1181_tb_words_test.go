package state

import (
	"strings"
	"testing"
	"time"
)

// The completeness words of slice C3 (docs/design-thunderbird-completeness.md §7, §6, §4.4, §2.3,
// §3), built once here and said by every surface, with the C3 friction review's word changes
// (docs/review-thunderbird-completeness-c3-friction.md P2 to P14). These pin each sentence exactly.
// The fixtures are accounts as a record keeps them (no Name), so the words name each by the folder
// its mail is kept in; MA-1284's tests pin the words for an account with Thunderbird's own name.

var tbNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)

// The folder-named forms of the fixtures' accounts (the owner's fallback wording, 2026-11-08).
const (
	tbFolderGmail   = "the Thunderbird account kept in the folder imap.gmail.com"
	tbFolderExample = "the Thunderbird account kept in the folder imap.example.com"
)

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
// why, with both numbers, and never "everything" (R42 as amended). The stale sentence ends with
// what to do (the friction review's P8).
func TestCaughtUpWordsSayEverythingOnlyWhenComplete(t *testing.T) {
	say := TBSay{Scheduled: true, Now: tbNow}
	wantLine(t, "Gmail caught up", TBHeadline(tbGmailCaughtUp(), say), "Thunderbird has downloaded everything it lists for "+tbFolderGmail+" (238,169 messages).")
	wantLine(t, "the other account caught up", TBHeadline(tbOtherCaughtUp(), say), "Thunderbird has downloaded everything it lists for "+tbFolderExample+" (3,402 messages).")
	if got := TBLines([]TBAccount{tbGmailCaughtUp(), tbOtherCaughtUp()}, 0, say); len(got) != 2 {
		t.Errorf("the owner-shaped lines: %q", got)
	}

	a := tbOtherCaughtUp()
	a.Complete, a.Missing, a.OnDisk = false, 2, 3400
	wantLine(t, "2 missing, within the tolerance", TBHeadline(a, say),
		"Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for "+tbFolderExample+". The rest is likely mail that arrived or was deleted since Thunderbird's count.")
	a.Stale, a.AsOf = true, time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)
	wantLine(t, "2 missing and stale", TBHeadline(a, say),
		"Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for "+tbFolderExample+". The rest is likely mail that arrived or was deleted since Thunderbird's count. These numbers are Thunderbird's count from Nov 2. Open Thunderbird and leave it open so it can count again.")

	s := tbOtherCaughtUp()
	s.Complete, s.Stale, s.AsOf = false, true, time.Date(2025, 12, 30, 9, 0, 0, 0, time.Local)
	wantLine(t, "stale with nothing missing (M9, P8)", TBHeadline(s, say),
		"As of Dec 30, 2025, Thunderbird's count matched what is on this computer for "+tbFolderExample+" (3,402 messages). It has not counted since, so newer mail may not be here yet. Open Thunderbird and leave it open so it can count again.")

	d := tbOtherCaughtUp()
	d.Complete, d.DeletedCopies, d.OnDisk = false, true, 3398
	wantLine(t, "deleted copies (M7)", TBHeadline(d, say),
		"Thunderbird lists 3,402 messages for "+tbFolderExample+" and about 3,398 are on this computer. MailArchive cannot be exact, because Thunderbird still keeps copies of deleted messages until it compacts its folders.")

	n := tbGmailCaughtUp()
	n.Complete, n.NoIdentity = false, 1
	wantLine(t, "a message with no identity", TBHeadline(n, say),
		"Thunderbird has downloaded about 238,169 of the 238,169 messages it lists for "+tbFolderGmail+", but MailArchive could not tell 1 message apart from the others, so it cannot be sure of the count.")

	k := tbOtherCaughtUp()
	k.Complete, k.NotKept = false, []TBFolder{{Folder: "Projects", Listed: 772}}
	wantLine(t, "a not-kept folder (L4)", TBHeadline(k, say),
		"Thunderbird has downloaded the 3,402 messages it lists in the folders it keeps on this computer for "+tbFolderExample+".")

	c := tbGmailCaughtUp()
	c.Complete, c.CapNote, c.OnDisk, c.Listed = false, 1000, 1000, 1000
	wantLine(t, "the cap note's headline (TC-22)", TBHeadline(c, say), "Thunderbird has downloaded the 1,000 messages it lists for "+tbFolderGmail+".")
	wantLine(t, "the cap note (TC-22, P5)", TBCapNote(c),
		`Gmail may be showing Thunderbird only the newest 1,000 messages of each folder of `+tbFolderGmail+`. To change it: in Gmail on the web, click the gear (top right), then See all settings, then the "Forwarding and POP/IMAP" tab; under "Folder size limits", choose "Do not limit the number of messages in an IMAP folder", then click Save Changes.`)

	e := TBAccount{Account: "imap.empty.example", Verdict: "caught_up", Complete: true, ListsNoMail: true}
	wantLine(t, "lists no mail", TBHeadline(e, say), "Thunderbird lists no mail for the Thunderbird account kept in the folder imap.empty.example, so there is nothing more to download.")

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
// furthest behind, Inbox said as Thunderbird shows it (P12); a floor says "at least" (R43); a stale
// count is dated; and "later backups add the rest" is said only when a schedule will make it true
// (TC-17). With no schedule, what to do again is named for where the words are said (P3): the
// wizard and status "Run the backup again", Run now "Click Run now again", the command line "Run
// the same command again". Positive twin first: the scheduled words, the same on every surface.
func TestBehindWordsSayBothNumbersAndWhatToDo(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1670, Listed: 14201, Missing: 12531, Tolerance: 28,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 10799, OnDisk: 1200}, {Folder: "Sent", Listed: 3402, OnDisk: 0}, {Folder: "Drafts", Listed: 0, OnDisk: 470}}}
	head := "Thunderbird has downloaded about 1,670 of 14,201 messages in " + tbFolderExample + " so far. Furthest behind: Inbox 1,200 of 10,799; Sent 0 of 3,402. Leave Thunderbird open (it downloads mostly while the computer is left alone)"
	for _, s := range []TBSurface{TBSurfaceDefault, TBSurfaceRunNow, TBSurfaceCommandLine} {
		wantLine(t, "behind, scheduled", TBHeadline(a, TBSay{Scheduled: true, Now: tbNow, Surface: s}), head+"; later backups add the rest.")
	}
	wantLine(t, "behind, no schedule (TC-17), the wizard and status", TBHeadline(a, TBSay{Now: tbNow}),
		head+". Run the backup again once Thunderbird has downloaded more, to add the rest.")
	wantLine(t, "behind, no schedule, Run now (P3)", TBHeadline(a, TBSay{Now: tbNow, Surface: TBSurfaceRunNow}),
		head+". Click Run now again once Thunderbird has downloaded more, to add the rest.")
	wantLine(t, "behind, no schedule, the command line (P3)", TBHeadline(a, TBSay{Now: tbNow, Surface: TBSurfaceCommandLine}),
		head+". Run the same command again once Thunderbird has downloaded more, to add the rest.")

	g := TBAccount{Account: "imap.gmail.com", Verdict: "behind", Gmail: true, OnDisk: 1670, Listed: 238080,
		Folders: []TBFolder{{Folder: "[Gmail]/Sent Mail", Listed: 33822, OnDisk: 2}}}
	wantLine(t, "Gmail behind: never per folder (§3)", TBHeadline(g, TBSay{Scheduled: true, Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of 238,080 messages in "+tbFolderGmail+" so far. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	g.Floor, g.Listed, g.Stale, g.AsOf = true, 56611, true, time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)
	wantLine(t, "a floor, stale", TBHeadline(g, TBSay{Scheduled: true, Now: tbNow}),
		"Thunderbird has downloaded about 1,670 of at least 56,611 messages in "+tbFolderGmail+" so far (Thunderbird's count from Nov 2). Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	for _, h := range []string{TBHeadline(a, TBSay{Now: tbNow}), TBHeadline(g, TBSay{Now: tbNow})} {
		if strings.Contains(h, "complete") || strings.Contains(h, "everything") {
			t.Errorf("a behind account says complete: %q", h)
		}
	}
}

// covers: MA-1181
// Two or more accounts behind (the friction review's P2): each account's line stops after its
// numbers (and its folders furthest behind), and one line after every account says what to do and whom it is for (the re-look's R2),
// named for the surface with no schedule (P3). Positive twin first: one account behind keeps its
// single line, remedy and all.
func TestTwoAccountsBehindSayTheRemedyOnce(t *testing.T) {
	b1 := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "behind", OnDisk: 300, Listed: 1200,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 1200, OnDisk: 300}}}
	b2 := TBAccount{Account: "imap.gmail.com", Name: "sam.walker@gmail.com", Verdict: "behind", Gmail: true, OnDisk: 60, Listed: 2500,
		NotKept: []TBFolder{{Folder: "Receipts", Listed: 33}}, Folders: []TBFolder{{Folder: "INBOX", Listed: 2500, OnDisk: 60}}}
	cu := tbOtherCaughtUp()
	cu.Name = "sam@example.com"
	sched := TBSay{Scheduled: true, Now: tbNow}
	if got := TBLines([]TBAccount{b1, cu}, 0, sched); len(got) != 2 || got[0] != TBHeadline(b1, sched) ||
		!strings.HasSuffix(got[0], "Furthest behind: Inbox 300 of 1,200. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.") {
		t.Fatalf("positive twin: one account behind keeps its single line: %q", got)
	}
	h1 := "Thunderbird has downloaded about 300 of 1,200 messages in sam@fastmail.com so far. Furthest behind: Inbox 300 of 1,200."
	h2 := "Thunderbird has downloaded about 60 of 2,500 messages in sam.walker@gmail.com so far."
	kept := TBNotKeptLine(b2)
	for _, c := range []struct {
		what   string
		say    TBSay
		remedy string
	}{
		{"scheduled", sched, "For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest."},
		{"no schedule, the wizard", TBSay{Now: tbNow}, "For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone), then run the backup again to add the rest."},
		{"no schedule, Run now", TBSay{Now: tbNow, Surface: TBSurfaceRunNow}, "For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone), then click Run now again to add the rest."},
		{"no schedule, the command line", TBSay{Now: tbNow, Surface: TBSurfaceCommandLine}, "For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone), then run the same command again to add the rest."},
	} {
		got := TBLines([]TBAccount{b1, cu, b2}, 1, c.say)
		want := []string{h1, TBHeadline(cu, c.say), h2, kept, c.remedy, TBMoreSentence(1)}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", c.what, got, want)
		}
		if n := strings.Count(strings.ToLower(strings.Join(got, "\n")), "leave thunderbird open"); n != 1 {
			t.Errorf("%s: the remedy is said %d times", c.what, n)
		}
	}
}

// covers: MA-1181
// Every way MailArchive cannot tell (§4.4's fold table and the reason codes C1b and C2 left for
// C3) is one sentence that says why, never "complete" or "everything"; the four that gave no next
// step now give one (the friction review's P8: "usually" for the folder list, unverified until a
// lab note sees which event makes Thunderbird write it again).
func TestUnknownWordsSayWhyInOneSentence(t *testing.T) {
	pre := "MailArchive cannot tell whether Thunderbird has downloaded all of the mail of " + tbFolderExample + ", because "
	couldNot := pre + "it could not read Thunderbird's list of folders. Opening Thunderbird and letting it check your mail usually fixes this; then run the backup again."
	for reason, want := range map[string]string{
		"cache_unreadable":  couldNot,
		"duplicate_key":     couldNot,
		"invalid_entry":     couldNot,
		"":                  couldNot,
		"some_future_code":  couldNot,
		"no_match":          pre + "MailArchive does not yet know how this kind of Thunderbird account keeps its mail.",
		"account_type":      pre + "MailArchive does not yet know how this kind of Thunderbird account keeps its mail.",
		"not_counted_yet":   pre + "Thunderbird has not counted all of its folders yet. Open Thunderbird, leave it open for a few minutes, then run the backup again.",
		"expunged_unsized":  pre + "Thunderbird holds copies of deleted messages that MailArchive cannot count.",
		"split_differently": pre + "some of its mail files are laid out in a way MailArchive could not count message by message.",
		"count_uncertain":   pre + "MailArchive could not count some of its messages for certain.",
		"other_reader":      pre + "this backup read its mail the way it reads another mail app's, which does not count it.",
		"walk_failed":       pre + "this backup could not read all of its mail.",
		"old_path_only":     pre + "Thunderbird's list of folders still names 2 folders by where they used to be. Open Thunderbird and leave it open for a minute so it saves that list again, then run the backup again.",
		"panorama":          "This Thunderbird uses its newer storage, which MailArchive cannot read yet, so it cannot tell whether Thunderbird has downloaded all of the mail of " + tbFolderExample + ".",
		"never_checked":     "Thunderbird has not checked the server for " + tbFolderExample + " yet, so MailArchive cannot tell how much is still to come. Open Thunderbird and leave it open.",
		"partial_input":     "Only part of the mail of " + tbFolderExample + " was backed up, so MailArchive did not compare it with Thunderbird's count.",
	} {
		a := TBAccount{Account: "imap.example.com", Verdict: "unknown", Reason: reason, CantTellFolders: 2, OnDisk: 5, Listed: 9}
		got := TBHeadline(a, TBSay{Now: tbNow})
		wantLine(t, "reason "+reason, got, want)
		if strings.Contains(got, "complete") || strings.Contains(got, "everything") {
			t.Errorf("reason %s says complete: %q", reason, got)
		}
	}
	for n, want := range map[int]string{
		1: pre + "Thunderbird's list of folders still names 1 folder by where it used to be. Open Thunderbird and leave it open for a minute so it saves that list again, then run the backup again.",
		0: pre + "Thunderbird's list of folders still names some folders by where they used to be. Open Thunderbird and leave it open for a minute so it saves that list again, then run the backup again.",
	} {
		a := TBAccount{Account: "imap.example.com", Verdict: "unknown", Reason: "old_path_only", CantTellFolders: n}
		wantLine(t, "old path, "+Count(n, "folder", "folders"), TBHeadline(a, TBSay{Now: tbNow}), want)
	}
}

// covers: MA-1181
// The Gmail floor (O3, the friction review's P9): the number on this computer is said with the
// account, Thunderbird's floor only when it is above it (at or below it, it adds nothing and read
// as "all is well"), and All Mail not kept points at the line below that says how to keep it.
func TestTheGmailFloorNeverReadsAsAllIsWell(t *testing.T) {
	floor := func(use string, x, y int64) TBAccount {
		return TBAccount{Account: "imap.gmail.com", Verdict: "unknown", Reason: "gmail_floor", Gmail: true, Floor: true, AllMail: use, OnDisk: x, Listed: y}
	}
	notKept := "About 50 messages of " + tbFolderGmail + " are on this computer. MailArchive cannot tell how many more there are, because Gmail's All Mail folder, which holds all of this account's mail, is not kept on this computer (see below to keep it)."
	wantLine(t, "All Mail not kept, X above Y", TBHeadline(floor("not_kept", 50, 40), TBSay{Now: tbNow}), notKept)
	wantLine(t, "All Mail not kept, X below Y", TBHeadline(floor("not_kept", 50, 400), TBSay{Now: tbNow}), notKept)
	for use, clause := range map[string]string{"not_shown": "is not shown to Thunderbird",
		"not_checked": "has not been checked by Thunderbird yet", "below_largest": "lists fewer messages than another of its folders", "": "is not shown to Thunderbird"} {
		for _, xy := range [][2]int64{{56700, 56611}, {56611, 56611}} {
			wantLine(t, "O3, All Mail "+use+", X at or above Y", TBHeadline(floor(use, xy[0], xy[1]), TBSay{Now: tbNow}),
				"About "+thousands(int(xy[0]))+" messages of "+tbFolderGmail+" are on this computer. MailArchive cannot tell whether that is all of it, because Gmail's All Mail "+clause+".")
		}
		wantLine(t, "O3, All Mail "+use+", X below Y", TBHeadline(floor(use, 30, 40), TBSay{Now: tbNow}),
			"About 30 messages of "+tbFolderGmail+" are on this computer, and Thunderbird lists at least 40. MailArchive cannot tell whether that is all of it, because Gmail's All Mail "+clause+".")
	}
	wantLine(t, "one message", TBHeadline(floor("not_shown", 1, 1), TBSay{Now: tbNow}),
		"About 1 message of "+tbFolderGmail+" is on this computer. MailArchive cannot tell whether that is all of it, because Gmail's All Mail is not shown to Thunderbird.")
	// At or above the floor, the floor's number is never said (it read as "all is well", M9).
	if h := TBHeadline(floor("not_shown", 56700, 56611), TBSay{Now: tbNow}); strings.Contains(h, "56,611") || strings.Contains(h, "complete") {
		t.Errorf("the floor said at or below X: %q", h)
	}
}

// covers: MA-1185
// Folders not kept on this computer (§6): named with Thunderbird's own words to keep them, the
// first 10 by listed count and "and N more"; when no folder of the account is kept, the account's
// own setting, labelled for the Thunderbird version that last opened the profile (L5: below 160
// the older label, 160 and above the newer one, both when the version cannot be told), reached by
// the menu button (P4). Spam and Trash, and Gmail's labels while All Mail backs them up, are notes
// only, naming only the folders not kept (P11). Positive twin first: an account with every folder
// kept says nothing of it.
func TestNotKeptWordsNameTheFoldersAndTheFix(t *testing.T) {
	if s := TBNotKeptLine(tbOtherCaughtUp()); s != "" || len(TBNotes(tbOtherCaughtUp())) != 0 {
		t.Fatalf("positive twin: every folder kept: %q %q", s, TBNotes(tbOtherCaughtUp()))
	}
	a := tbOtherCaughtUp()
	a.NotKept = []TBFolder{{Folder: "Projects", Listed: 772}, {Folder: "Receipts", Listed: 33}}
	wantLine(t, "two folders", TBNotKeptLine(a),
		`These folders of `+tbFolderExample+` are not kept on this computer, so MailArchive cannot back them up: Projects (772 messages), Receipts (33 messages). To keep them: in Thunderbird, right-click each folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`)
	a.NotKept, a.NotKeptMore = a.NotKept[:1], 0
	a.NotKept[0].Listed = 1
	wantLine(t, "one folder", TBNotKeptLine(a),
		`This folder of `+tbFolderExample+` is not kept on this computer, so MailArchive cannot back it up: Projects (1 message). To keep it: in Thunderbird, right-click the folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`)
	a.NotKept = []TBFolder{{Folder: "inbox", Listed: 4}}
	if s := TBNotKeptLine(a); !strings.Contains(s, ": Inbox (4 messages).") {
		t.Errorf("Inbox as Thunderbird shows it (P12): %q", s)
	}
	var ten []TBFolder
	for i := 0; i < 10; i++ {
		ten = append(ten, TBFolder{Folder: "F" + string(rune('A'+i)), Listed: 10})
	}
	a.NotKept, a.NotKeptMore = ten, 32
	if s := TBNotKeptLine(a); !strings.Contains(s, "FJ (10 messages) and 32 more. To keep them") || strings.Count(s, "(10 messages)") != 10 {
		t.Errorf("ten named and 32 more: %q", s)
	}

	all := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "unknown", Reason: "cache_unreadable", NotKept: []TBFolder{{Folder: "INBOX", Listed: 9}}}
	older := `Thunderbird is not set to keep sam@example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird, click the menu button (≡, top right), then Account Settings; on sam@example.com's "Synchronization & Storage" page, tick "Keep messages in all folders for this account on this computer"`
	for v, want := range map[int]string{
		153: older + `.`,
		159: older + `.`,
		160: `Thunderbird is not set to keep sam@example.com's mail on this computer, so MailArchive cannot back it up. In Thunderbird, click the menu button (≡, top right), then Account Settings; under sam@example.com's "Offline message storage", tick "Save copies of all folders and messages on this computer for offline use".`,
		0:   older + ` (in Thunderbird 160 and later: under "Offline message storage", tick "Save copies of all folders and messages on this computer for offline use").`,
	} {
		all.ThunderbirdVersion = v
		wantLine(t, "no folder kept, version "+string(rune('0'+v%10)), TBNotKeptLine(all), want)
	}
	all.Name, all.ThunderbirdVersion = "", 153
	wantLine(t, "no folder kept, named by its folder", TBNotKeptLine(all),
		`Thunderbird is not set to keep the mail of `+tbFolderExample+` on this computer, so MailArchive cannot back it up. In Thunderbird, click the menu button (≡, top right), then Account Settings; on the "Synchronization & Storage" page of `+tbFolderExample+`, tick "Keep messages in all folders for this account on this computer".`)

	notes := TBNotes(TBAccount{Account: "imap.gmail.com", DefaultNotKept: []TBFolder{{Folder: "[Gmail]/Trash", Listed: 30}, {Folder: "[Gmail]/Spam", Listed: 3}}, LabelsNotKept: 40})
	if len(notes) != 2 {
		t.Fatalf("the notes: %q", notes)
	}
	wantLine(t, "Spam and Trash (P11)", notes[0], "Thunderbird does not keep the Spam and Trash folders of "+tbFolderGmail+" on this computer (its usual setting), so they are not backed up.")
	wantLine(t, "Gmail labels (TC-8)", notes[1], "Some of the labels of "+tbFolderGmail+" are not kept on this computer as folders of their own. Their mail is still backed up through All Mail.")
	wantLine(t, "only Trash (P11)", TBDefaultNotKeptNote(TBAccount{Account: "imap.example.com", DefaultNotKept: []TBFolder{{Folder: "Trash", Listed: 5}}}),
		"Thunderbird does not keep the Trash folder of "+tbFolderExample+" on this computer (its usual setting), so it is not backed up.")
	wantLine(t, "Trash and more (P11)", TBDefaultNotKeptNote(TBAccount{Account: "imap.example.com", Name: "sam@example.com", DefaultNotKept: []TBFolder{{Folder: "Trash", Listed: 5}}, DefaultNotKeptMore: 2}),
		"Thunderbird does not keep the Trash folder and 2 more folders of sam@example.com on this computer (its usual setting), so they are not backed up.")
	wantLine(t, "the labels, named", TBLabelsNote(TBAccount{Account: "imap.gmail.com", Name: "sam@gmail.com", LabelsNotKept: 2}),
		"Some of sam@gmail.com's labels are not kept on this computer as folders of their own. Their mail is still backed up through All Mail.")
}

// covers: MA-1185
// A limited account (§6's limited words, TC-9): both numbers, each limit in Thunderbird's own
// words for the version (L5), reached by the menu button (P4), background download off in neutral
// words (its label is still owed by lab row MA-1191), and the fallback that says where to look.
func TestLimitedWordsNameTheSetting(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "limited", OnDisk: 900, Listed: 5000, Limit: &TBLimit{MaxAgeDays: 30, MaxSizeKB: 50}}
	say := TBSay{Scheduled: true, Now: tbNow}
	wantLine(t, "limited headline", TBHeadline(a, say), "Thunderbird has downloaded about 900 of 5,000 messages in sam@example.com.")
	menu := "in Thunderbird, click the menu button (≡, top right), then Account Settings; "
	age := `Thunderbird is set to keep only the most recent 30 days of mail for sam@example.com on this computer, so older mail is not backed up. To back up all of it: ` + menu
	size := `Thunderbird is set not to download messages larger than 50 KB for sam@example.com, so those are not backed up. To back them up: ` + menu
	want := map[int][]string{
		153: {age + `on sam@example.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".`,
			size + `on sam@example.com's "Synchronization & Storage" page, untick "Don't download messages larger than".`},
		160: {age + `for sam@example.com, set it to keep messages of every age on this computer.`,
			size + `under sam@example.com's "Download limits", untick "Skip downloading messages larger than:".`},
		0: {age + `on sam@example.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".`,
			size + `on sam@example.com's "Synchronization & Storage" page, untick "Don't download messages larger than" (in Thunderbird 160 and later: under "Download limits", untick "Skip downloading messages larger than:").`},
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
	f := a
	f.Name, f.ThunderbirdVersion = "", 153
	wantLine(t, "the age limit, named by its folder (the headline names it: R4)", TBLimitLines(f)[0],
		`Thunderbird is set to keep only the most recent 30 days of this account's mail on this computer, so older mail is not backed up. To back up all of it: `+menu+`on the "Synchronization & Storage" page of `+tbFolderExample+`, choose "Synchronize all messages locally regardless of age".`)
	b := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "limited", Limit: &TBLimit{NoBackgroundDownload: true}}
	if h := TBHeadline(b, say); h != "" {
		t.Errorf("limited with nothing listed: no numbers to say, got %q", h)
	}
	if got := TBAccountLines(b, say); len(got) != 1 || got[0] != "Thunderbird is set not to download sam@example.com's mail in the background, so MailArchive backs up only what you have opened or downloaded by hand. In Thunderbird, click the menu button (≡, top right), then Account Settings; for sam@example.com, turn on the setting that saves copies of this account's messages on this computer." {
		t.Errorf("background download off (TC-9): %q", got)
	}
	l := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "limited", OnDisk: 170, Listed: 2300}
	wantLine(t, "the limited fallback (P4)", strings.Join(TBLimitLines(l), "\n"),
		`Thunderbird's own settings keep only part of sam@example.com's mail on this computer, so the rest is not backed up. To check: in Thunderbird, click the menu button (≡, top right), then Account Settings, and open sam@example.com's "Synchronization & Storage" page.`)
}

// covers: MA-1181, MA-1182
// Recent backups keeps only the verdict and the two numbers (§9.1): a behind row's words and its
// flag, a floor as "at least" (R43), and nothing for an account caught up with nothing missing;
// "message" after a Y of 1 (P13). The archive card's one line points at Status & health: behind
// accounts, then accounts Thunderbird keeps only in part (P10), then folders not kept. Accounts
// past the record's cap are never said complete (R2-06).
func TestTheShortWords(t *testing.T) {
	if s, behind := TBCompactSentence(TBCompact{Account: "imap.gmail.com", Verdict: "caught_up", X: 9, Y: 9}); s != "" || behind {
		t.Fatalf("positive twin: caught up with nothing missing: %q %v", s, behind)
	}
	s, behind := TBCompactSentence(TBCompact{Account: "imap.gmail.com", Verdict: "behind", X: 1670, Y: 56611, Floor: true})
	wantLine(t, "behind, a floor", s, "Thunderbird has downloaded about 1,670 of at least 56,611 messages in "+tbFolderGmail+" so far")
	if !behind {
		t.Error("a behind entry is not told behind")
	}
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "behind", X: 0, Y: 1})
	wantLine(t, "behind, one message, none downloaded (P13, R3)", s, "Thunderbird has not downloaded the 1 message in "+tbFolderExample+" yet")
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "caught_up", X: 3400, Y: 3402})
	wantLine(t, "caught up, 2 missing", s, "Thunderbird has downloaded about 3,400 of the 3,402 messages it lists for "+tbFolderExample)
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "limited", X: 900, Y: 5000})
	wantLine(t, "limited", s, "Thunderbird's own settings keep only part of the mail of "+tbFolderExample+" on this computer (about 900 of 5,000 messages)")
	s, _ = TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "unknown"})
	wantLine(t, "unknown", s, "MailArchive could not tell how much of the mail of "+tbFolderExample+" Thunderbird has downloaded")

	if b := TBCardBanner([]TBAccount{tbGmailCaughtUp()}); b != "" {
		t.Errorf("positive twin: a caught-up archive's card: %q", b)
	}
	wantLine(t, "the card, one behind", TBCardBanner([]TBAccount{{Account: "imap.gmail.com", Verdict: "behind", OnDisk: 1670, Listed: 238080}}),
		"Thunderbird is still downloading "+tbFolderGmail+": about 1,670 of 238,080 messages are on this computer so far — see Status & health.")
	wantLine(t, "the card, two behind and not-kept folders", TBCardBanner([]TBAccount{{Account: "a", Verdict: "behind"}, {Account: "b", Verdict: "behind", NotKept: []TBFolder{{Folder: "x", Listed: 1}}, NotKeptMore: 2}}),
		"Thunderbird is still downloading 2 accounts; 3 folders are not kept on this computer, so they are not backed up — see Status & health.")
	wantLine(t, "the card, one limited (P10)", TBCardBanner([]TBAccount{{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "limited", OnDisk: 170, Listed: 2300}}),
		"Thunderbird keeps only part of sam@fastmail.com's mail on this computer (about 170 of 2,300 messages), so the rest is not backed up — see Status & health.")
	wantLine(t, "the card, one limited with nothing listed (P10)", TBCardBanner([]TBAccount{{Account: "imap.fastmail.com", Verdict: "limited"}}),
		"Thunderbird keeps only part of the mail of the Thunderbird account kept in the folder imap.fastmail.com on this computer, so the rest is not backed up — see Status & health.")
	wantLine(t, "the card, behind, two limited and a not-kept folder (P10)", TBCardBanner([]TBAccount{
		{Account: "imap.example.com", Verdict: "behind", OnDisk: 0, Listed: 1},
		{Account: "l1", Verdict: "limited", OnDisk: 1, Listed: 2}, {Account: "l2", Verdict: "limited", NotKept: []TBFolder{{Folder: "x", Listed: 1}}}}),
		"Thunderbird is still downloading "+tbFolderExample+": its 1 message is not on this computer yet; Thunderbird keeps only part of the mail of 2 accounts on this computer, so the rest is not backed up; 1 folder is not kept on this computer, so it is not backed up — see Status & health.")

	wantLine(t, "one account left out", TBMoreSentence(1), "MailArchive did not keep the details of 1 more Thunderbird account in this backup's record, so it does not say it is complete.")
	if got := TBLines(nil, 3, TBSay{}); len(got) != 1 || !strings.Contains(got[0], "3 more Thunderbird accounts") {
		t.Errorf("accounts left out: %q", got)
	}
}

// covers: MA-1181
// "about X of Y" says "message" after a Y of 1 everywhere it is said (P13): the behind and limited
// headlines and the card. Positive twin first: a Y of 2 says "messages".
func TestOneMessageIsSingular(t *testing.T) {
	two := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1, Listed: 2}
	if h := TBHeadline(two, TBSay{Scheduled: true}); !strings.HasPrefix(h, "Thunderbird has downloaded about 1 of 2 messages in ") {
		t.Fatalf("positive twin: %q", h)
	}
	one := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 0, Listed: 1}
	wantLine(t, "behind, 0 of 1 (R3)", TBHeadline(one, TBSay{Scheduled: true}),
		"Thunderbird has not downloaded the 1 message in "+tbFolderExample+" yet. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	one.Verdict = "limited"
	wantLine(t, "limited, 0 of 1", TBHeadline(one, TBSay{}), "Thunderbird has downloaded about 0 of 1 message in "+tbFolderExample+".")
	one.Floor = true
	wantLine(t, "limited, a floor of 1", TBHeadline(one, TBSay{}), "Thunderbird has downloaded about 0 of at least 1 message in "+tbFolderExample+".")
	c := TBAccount{Account: "imap.example.com", Verdict: "caught_up", OnDisk: 0, Listed: 1, Missing: 1}
	wantLine(t, "caught up within the tolerance, 0 of 1", TBHeadline(c, TBSay{}),
		"Thunderbird has downloaded about 0 of the 1 message it lists for "+tbFolderExample+". The rest is likely mail that arrived or was deleted since Thunderbird's count.")
	if s, _ := TBCompactSentence(TBCompact{Account: "imap.example.com", Verdict: "limited", X: 0, Y: 1}); !strings.HasSuffix(s, "(about 0 of 1 message)") {
		t.Errorf("Recent backups, limited 0 of 1: %q", s)
	}
}

// covers: MA-1198
// Folder and account names are text the mail server controls (TC-7): every sentence says them
// cleaned (control characters, a bidi override and invisible characters gone) and capped at 80
// characters plus "…"; a name of nothing visible is said as "(a folder with no name)" or "this
// account". Positive twin first: ordinary names are said as they are (Inbox as Thunderbird shows
// it, P12).
func TestHostileNamesAreCleanedInEveryWord(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1, Listed: 9,
		Folders: []TBFolder{{Folder: "INBOX", Listed: 9, OnDisk: 1}}, NotKept: []TBFolder{{Folder: "Projects", Listed: 3}}}
	if lines := strings.Join(TBAccountLines(a, TBSay{Now: tbNow}), "\n"); !strings.Contains(lines, "Inbox 1 of 9") || !strings.Contains(lines, "Projects (3 messages)") {
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
	for _, good := range []string{"the Thunderbird account kept in the folder imap.evil[31m.com", "Inbox 1 of 9", strings.Repeat("x", 80) + "… (3 messages)", "(a folder with no name) (2 messages)"} {
		if !strings.Contains(lines, good) {
			t.Errorf("the words miss %q:\n%s", good, lines)
		}
	}
	if h := TBHeadline(TBAccount{Account: "\u200b", Verdict: "unknown"}, TBSay{}); !strings.Contains(h, "all of this account's mail,") {
		t.Errorf("an account of nothing visible: %q", h)
	}
}
