package tbcomplete

import (
	"testing"
)

// covers: MA-1276
// The C1a pass round 2's R2-02, closed in slice C2: in tail mode a key exact for another store
// of the profile (StoreAmong's others, as given or by goos's comparison) is that store's and is
// never taken. The store /p/ImapMail/h has no key of its own; another account's folder
// /q/ImapMail/h has keys. Positive twin first: without the other stores the reader takes them in
// tail mode (C1A-06's stated limit); with them it has none. Exact mode is unchanged: a store
// with its own keys still reads them, beside the other's.
func TestStoreAmongSkipsAnotherStoresExactKeys(t *testing.T) {
	other := "/q/ImapMail/h"
	data := cacheJSON(t,
		pair{other, root()},
		pair{other + "/INBOX.msf", folder(FlagInbox, 3, 3)})
	c, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.storeAt([]string{"/p/ImapMail/h"}, "linux"); s.Mode != Tail || len(s.Folders) != 1 {
		t.Fatalf("positive twin: without the other stores, tail mode takes the keys: %+v", s)
	}
	if s := c.storeAmong([]string{"/p/ImapMail/h"}, []string{other}, "linux"); s.Mode != NoKeys || len(s.Folders) != 0 || s.Root != nil {
		t.Errorf("with the other store passed, the keys are still taken: %+v", s)
	}
	// Compared as the OS compares names: on macOS a case respelling of the other store's path.
	if s := c.storeAmong([]string{"/p/ImapMail/h"}, []string{"/Q/imapmail/H"}, "darwin"); s.Mode != NoKeys {
		t.Errorf("darwin, the other store spelled in another case: %+v", s)
	}
	if s := c.storeAmong([]string{"/p/ImapMail/h"}, []string{"/Q/imapmail/H"}, "linux"); s.Mode != Tail {
		t.Errorf("linux, another case is another folder: %+v", s)
	}
	// Another store elsewhere changes nothing.
	if s := c.storeAmong([]string{"/p/ImapMail/h"}, []string{"/r/ImapMail/h"}, "linux"); s.Mode != Tail {
		t.Errorf("an unrelated other store: %+v", s)
	}
	// Exact mode: the store's own keys are read; the other's are not its folders.
	both := cacheJSON(t,
		pair{other, root()}, pair{other + "/INBOX.msf", folder(FlagInbox, 3, 3)},
		pair{"/p/ImapMail/h", root()}, pair{"/p/ImapMail/h/INBOX.msf", folder(FlagInbox, 5, 5)})
	c2, err := Parse([]byte(both))
	if err != nil {
		t.Fatal(err)
	}
	s := c2.storeAmong([]string{"/p/ImapMail/h"}, []string{other}, "linux")
	if s.Mode != Exact || len(s.Folders) != 1 || s.Folders[0].TotalMsgs.Value != 5 || s.IgnoredOldKeys != 2 {
		t.Errorf("exact mode with the other store passed (unchanged: the other's two keys ignored as old-path keys): %+v", s)
	}
	if s0 := c2.storeAt([]string{"/p/ImapMail/h"}, "linux"); s0.IgnoredOldKeys != s.IgnoredOldKeys || len(s0.Folders) != len(s.Folders) {
		t.Errorf("exact mode changed by the other stores: %+v vs %+v", s0, s)
	}
}

// covers: MA-1175, MA-1173
// What the walk counted, in the verdict's spelling (slice C2; the C1b note: OnDisk.Folders keyed
// by Entry.Path exactly as the reader gives it). Each walked mailbox is the folder its place
// names, containers named .sbd, compared as the OS compares names; Gmail's Distinct counts each
// Message-ID once across the kept folders' mailboxes, plus each message with none; a mailbox with
// no key, under a container not named .sbd, or the store's own maildir is unkeyed (L3) and never
// in X; two mailboxes naming one folder are both unkeyed; an old-path-only folder (M2) is in
// neither; a not-kept folder's mailbox is walked (its count is there) but never in Distinct.
// Positive twin first.
func TestOnDiskForMapsTheWalkToTheStoresFolders(t *testing.T) {
	s := vstore(t,
		at("INBOX", folder(FlagInbox, 3, 3)),
		at("[Gmail]", folder(FlagImapNoselect, 0, 0)),
		at("[Gmail]/All Mail", folder(FlagAllMail|FlagArchive, 4, 4)),
		at("[Gmail]/Spam", notKept(FlagJunk, 2)))
	k := func(id string) [16]byte { return [16]byte{id[0], id[len(id)-1]} }
	walked := []Mailbox{
		{Rel: "INBOX", Messages: 3, Keys: [][16]byte{k("a1"), k("a2"), k("a3")}},
		{Rel: "[Gmail].sbd/All Mail", Messages: 4, Keys: [][16]byte{k("a1"), k("a2"), k("a4")}, NoKey: 1},
		{Rel: "[Gmail].sbd/Spam", Messages: 2, Keys: [][16]byte{k("s1"), k("s2")}, NoKey: 5},
		{Rel: "Leftover", Messages: 7, Keys: [][16]byte{k("x1")}, NoKey: 3},
		{Rel: "Linked/INBOX", Messages: 2},
		{Rel: "", Messages: 1},
	}
	d := s.onDiskFor(walked, true, "linux")
	if d.Folders["INBOX"] != 3 || d.Folders["[Gmail]/All Mail"] != 4 || d.Folders["[Gmail]/Spam"] != 2 || len(d.Folders) != 3 {
		t.Errorf("folders: %v", d.Folders)
	}
	// Amended 2026-11-08 (the C2 pass, ADV-C2-02): a message with no key is never in X, once
	// per copy or at all; it is counted in NoKey (kept folders only).
	if d.Distinct != 4 || d.NoKey != 1 {
		t.Errorf("distinct %d, no key %d; want 4 (a1, a2, a3, a4; never Spam's or Leftover's) and 1 (All Mail's only)", d.Distinct, d.NoKey)
	}
	if d.UnkeyedMailboxes != 3 || d.UnkeyedMessages != 10 {
		t.Errorf("unkeyed: %d mailboxes, %d messages; want 3, 10", d.UnkeyedMailboxes, d.UnkeyedMessages)
	}
	if nd := s.onDiskFor(walked, false, "linux"); nd.Distinct != 0 || nd.Folders["INBOX"] != 3 {
		t.Errorf("not Gmail: %+v", nd)
	}
	// Names compared as the OS compares them.
	mac := s.onDiskFor([]Mailbox{{Rel: "inbox", Messages: 3}}, false, "darwin")
	lin := s.onDiskFor([]Mailbox{{Rel: "inbox", Messages: 3}}, false, "linux")
	if mac.Folders["INBOX"] != 3 || lin.Folders["INBOX"] != 0 || lin.UnkeyedMailboxes != 1 {
		t.Errorf("case: darwin %+v, linux %+v", mac, lin)
	}
	// A container not named .sbd names no folder of Thunderbird's.
	if bare := s.onDiskFor([]Mailbox{{Rel: "[Gmail]/All Mail", Messages: 4, Keys: [][16]byte{k("a1")}}}, true, "linux"); len(bare.Folders) != 0 || bare.Distinct != 0 || bare.UnkeyedMailboxes != 1 {
		t.Errorf("a container not named .sbd: %+v", bare)
	}
	// Two mailboxes naming one folder: both unkeyed.
	two := s.onDiskFor([]Mailbox{{Rel: "INBOX", Messages: 3}, {Rel: "inbox", Messages: 2}}, false, "darwin")
	if len(two.Folders) != 0 || two.UnkeyedMailboxes != 2 || two.UnkeyedMessages != 5 {
		t.Errorf("two mailboxes for one folder: %+v", two)
	}
	// An old-path-only folder is in neither.
	old := Store{Mode: Exact, OldPathOnly: []Entry{{Path: "Old"}}}
	if o := old.onDiskFor([]Mailbox{{Rel: "Old", Messages: 9, Keys: [][16]byte{k("o1")}, Unseparated: 2}}, true, "linux"); o.SplitMailboxes != 0 || len(o.Folders) != 0 || o.Distinct != 0 || o.UnkeyedMailboxes != 0 {
		t.Errorf("an old-path-only folder: %+v", o)
	}
}
