package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-1222, R45
// The status and run summary's Gmail line (docs/design-gmail-dedup.md §8.1, slice B3): counts
// only, of the switched-on Gmail stores. Positive twin first: an archive nobody switched on has
// no line. Switched on: its emails, its copy memberships ("extra folder copies recorded without
// storing them again") and its listed ones ("folder entries taken from Thunderbird's folder
// lists"), digits grouped; another store's records are not counted; a count of zero is left out;
// one of each is said in the singular. No folder name is ever in the line.
func TestTheGmailLineCountsTheSwitchedOnStoresOnly(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rec := func(folders ...Membership) Record {
		return Record{Path: "x/y.html", Folder: "All Mail", ExportedAt: now, Folders: folders}
	}
	m.Add(Key("g", "All Mail", "<1@x>"), rec(Membership{Folder: "INBOX", Kind: MemberCopy, FirstSeen: now}, Membership{Folder: "Sent Mail", Kind: MemberListed, FirstSeen: now}))
	m.Add(Key("g", "All Mail", "<2@x>"), rec(Membership{Folder: "Secret Label", Kind: MemberCopy, FirstSeen: now}))
	m.Add(Key("o", "Inbox", "<3@x>"), rec())
	if l := GmailCountsOf(m).Line(); l != "" {
		t.Fatalf("positive twin: nobody switched on, yet the line %q", l)
	}
	m.SwitchOnGmail("g")
	c := GmailCountsOf(m)
	if c != (GmailCounts{Stores: 1, Messages: 2, Copies: 2, Listed: 1}) {
		t.Fatalf("counts %+v", c)
	}
	want := "Gmail: 2 messages. 2 extra folder copies recorded without storing them again. 1 folder entry taken from Thunderbird's folder lists."
	if c.Line() != want {
		t.Fatalf("line %q, want %q", c.Line(), want)
	}
	for _, f := range []string{"INBOX", "Sent Mail", "Secret Label", "All Mail"} {
		if contains(c.Line(), f) {
			t.Fatalf("the line names a folder: %q", c.Line())
		}
	}
	if l := (GmailCounts{Stores: 1, Messages: 233322, Copies: 79200}).Line(); l != "Gmail: 233,322 messages. 79,200 extra folder copies recorded without storing them again." {
		t.Fatalf("the owner-sized line: %q", l)
	}
	if l := (GmailCounts{Stores: 1, Messages: 1, Copies: 1}).Line(); l != "Gmail: 1 message. 1 extra folder copy recorded without storing it again." {
		t.Fatalf("the singular line: %q", l)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
