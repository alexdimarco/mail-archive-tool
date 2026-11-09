package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The account's name in the words (the owner's ruling of 2026-11-08 on the C3 friction review's
// M1): the name Thunderbird shows for the account (TBAccount.Name, usually the email address) when
// the run read one, cleaned and capped as folder names are (TC-7); otherwise "the Thunderbird
// account kept in the folder NAME". The name is for the screen only: the record keeps the store
// folder.

// covers: MA-1284
// An account with Thunderbird's own name is said by that name, in the headline, the possessive
// fix-it words and the card; the same account read back from a record (no name) is said by its
// folder, in words that still read as a sentence. Positive twin first: the folder words.
func TestTheAccountIsSaidByThunderbirdsName(t *testing.T) {
	a := TBAccount{Account: "imap.gmail.com-1", Verdict: "behind", Gmail: true, OnDisk: 60, Listed: 2500}
	say := TBSay{Scheduled: true, Now: tbNow}
	if h := TBHeadline(a, say); !strings.HasPrefix(h, "Thunderbird has downloaded about 60 of 2,500 messages in the Thunderbird account kept in the folder imap.gmail.com-1 so far.") {
		t.Fatalf("positive twin: named by its folder: %q", h)
	}
	a.Name = "sam.work@gmail.com"
	wantLine(t, "the headline", TBHeadline(a, say),
		"Thunderbird has downloaded about 60 of 2,500 messages in sam.work@gmail.com so far. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.")
	wantLine(t, "the card", TBCardBanner([]TBAccount{a}),
		"Thunderbird is still downloading sam.work@gmail.com: about 60 of 2,500 messages are on this computer so far — see Status & health.")
	l := TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "limited", OnDisk: 170, Listed: 2300, Limit: &TBLimit{MaxAgeDays: 30}, ThunderbirdVersion: 153}
	if got := TBLimitLines(l); len(got) != 1 || !strings.Contains(got[0], `then Account Settings; on sam@fastmail.com's "Synchronization & Storage" page, choose`) || strings.Contains(got[0], "imap.fastmail.com") {
		t.Errorf("the fix-it words point at the name Account Settings shows: %q", got)
	}
	// No surface word says the folder once the name is known.
	for _, s := range append(TBAccountLines(l, say), TBCardBanner([]TBAccount{l}), TBHeadline(TBAccount{Account: "imap.x.example", Name: "Work mail", Verdict: "unknown", Reason: "partial_input"}, say)) {
		if strings.Contains(s, "kept in the folder") || strings.Contains(s, "imap.") {
			t.Errorf("a named account said by its folder: %q", s)
		}
	}
	wantLine(t, "a name that is not an address", TBHeadline(TBAccount{Account: "imap.x.example", Name: "Work mail", Verdict: "unknown", Reason: "partial_input"}, say),
		"Only part of Work mail was backed up, so MailArchive did not compare it with Thunderbird's count.")
}

// covers: MA-1284
// The name is text the person, or a server through the account wizard, chose (TC-7): cleaned and
// capped at 80 characters plus "…" as folder names are; a name of nothing visible is no name, so the
// account is said by its folder, and with no folder either, "this account". Positive twin first:
// an ordinary name as it is.
func TestTheNameIsCleanedAndCapped(t *testing.T) {
	a := TBAccount{Account: "imap.example.com", Name: "sam@example.com", Verdict: "caught_up", Complete: true, OnDisk: 2, Listed: 2}
	wantLine(t, "positive twin", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for sam@example.com (2 messages).")
	a.Name = "sam‮@evil\x1b[31m.example​" + strings.Repeat("y", 10*1024)
	h := TBHeadline(a, TBSay{})
	for _, bad := range []string{"‮", "\x1b", "​", strings.Repeat("y", 81)} {
		if strings.Contains(h, bad) {
			t.Errorf("the name holds %q: %q", bad, h)
		}
	}
	if want := "for " + CleanFolderName(a.Name) + " (2 messages)."; !strings.HasSuffix(h, want) || !strings.HasSuffix(CleanFolderName(a.Name), "…") {
		t.Errorf("the cleaned, capped name: %q, want suffix %q", h, want)
	}
	a.Name = "​⁦ \x00"
	wantLine(t, "a name of nothing visible", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for the Thunderbird account kept in the folder imap.example.com (2 messages).")
	a.Account = "​"
	wantLine(t, "no name and no folder", TBHeadline(a, TBSay{}), "Thunderbird has downloaded everything it lists for this account (2 messages).")
	if s := TBNotKeptLine(TBAccount{Account: "​", NotKept: []TBFolder{{Folder: "INBOX", Listed: 1}}}); !strings.HasPrefix(s, "Thunderbird is not set to keep this account's mail on this computer") {
		t.Errorf("the possessive of this account: %q", s)
	}
}

// covers: MA-1284
// The name is never written: the last-run record keeps the store folder and no name (the owner's
// ruling: records, -json and the alert email keep the folder name), so an account read back is
// said by its folder. Positive twin first: the folder is written.
func TestTheNameIsNeverWritten(t *testing.T) {
	out := t.TempDir()
	a := TBAccount{Account: "imap.gmail.com", Name: "qzname@gmail.example", Verdict: "behind", OnDisk: 1, Listed: 9}
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: []TBAccount{a}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"account":"imap.gmail.com"`) && !strings.Contains(string(data), `"account": "imap.gmail.com"`) {
		t.Fatalf("positive twin: the folder is written: %s", data)
	}
	if strings.Contains(string(data), "qzname") {
		t.Errorf("the record holds the account's name: %s", data)
	}
	lr, _, err := ReadLastRun(out)
	if err != nil || len(lr.Thunderbird) != 1 || lr.Thunderbird[0].Name != "" {
		t.Fatalf("read back: %+v %v", lr.Thunderbird, err)
	}
	if h := TBHeadline(lr.Thunderbird[0], TBSay{Scheduled: true}); !strings.Contains(h, "in the Thunderbird account kept in the folder imap.gmail.com so far") {
		t.Errorf("read back, said by its folder: %q", h)
	}
}
