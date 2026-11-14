package state

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-1300, R45
// The Gmail count line in plain words (the B3 friction review's F12): "Gmail: 19 emails saved; 4 of
// them are also listed in another folder without a second copy." — emails, and how many of them
// are also listed elsewhere, never "folder entries". Positive twin first: an archive with nothing
// switched on has no line; one with no email listed elsewhere says only how many are saved.
func TestTheGmailCountLineSaysEmailsSavedAndHowManyAreListedElsewhere(t *testing.T) {
	m := newManifest("")
	if l := GmailCountsOf(m).Line(); l != "" {
		t.Fatalf("positive twin: nothing switched on, yet %q", l)
	}
	now := time.Now().UTC()
	for i, folders := range [][]Membership{
		nil,
		{{Folder: "INBOX", Kind: MemberCopy, FirstSeen: now}, {Folder: "Receipts", Kind: MemberListed, FirstSeen: now}},
		{{Folder: "Sent", Kind: MemberListed, FirstSeen: now}},
	} {
		m.Add(Key("g", "[Gmail]/All Mail", "mid:"+string(rune('a'+i))), Record{Folder: "[Gmail]/All Mail", Folders: folders})
	}
	m.SwitchOnGmail("g")
	c := GmailCountsOf(m)
	if want := "Gmail: 3 emails saved; 2 of them are also listed in another folder without a second copy."; c.Line() != want {
		t.Fatalf("line %q, want %q", c.Line(), want)
	}
	for _, jargon := range []string{"folder entr", "message", "Receipts", "INBOX"} {
		if strings.Contains(c.Line(), jargon) {
			t.Fatalf("the line says %q: %q", jargon, c.Line())
		}
	}
	if l := (GmailCounts{Stores: 1, Messages: 1}).Line(); l != "Gmail: 1 email saved." {
		t.Fatalf("one email, listed nowhere else: %q", l)
	}
	if l := (GmailCounts{Stores: 1, Messages: 233322, InOther: 1}).Line(); l != "Gmail: 233,322 emails saved; 1 of them is also listed in another folder without a second copy." {
		t.Fatalf("one listed elsewhere: %q", l)
	}
}
