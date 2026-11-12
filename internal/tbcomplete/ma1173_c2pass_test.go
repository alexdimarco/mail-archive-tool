package tbcomplete

// The C2 adversarial pass's fixes in the verdict (docs/review-thunderbird-completeness-c2-
// adversarial.md): a mailbox split differently from Thunderbird's separators (ADV-C2-03), a
// Gmail message with no identity key (ADV-C2-02), a store read by another reader (ADV-C2-16)
// and a walk that failed. Each with its positive twin first.

import (
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1173
// A kept mailbox the reader split into pieces that do not begin at a separator Thunderbird
// writes is "split differently": the account is unknown (split_differently), never caught up and
// never complete; a certain gap still says behind (as M2's can't-tell folders do). OnDiskFor
// counts such a keyed mailbox, never an unkeyed one. Positive twin: the same counts, nothing split.
func TestAMailboxSplitDifferentlyIsNeverComplete(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Sent", folder(FlagSentMail, 5, 5)))
	walked := func(split int64) []Mailbox {
		return []Mailbox{{Rel: "INBOX", Messages: 10, Unseparated: split}, {Rel: "Sent", Messages: 5}, {Rel: "Leftover", Messages: 1, Unseparated: 4}}
	}
	in := healthy(s, nil)
	in.OnDisk = s.onDiskFor(walked(0), false, "linux")
	r := Assess(in)
	want(t, "positive twin", r, CaughtUp, NoReason)
	if !r.MaySayComplete() || in.OnDisk.SplitMailboxes != 0 {
		t.Fatalf("positive twin: complete %v, split %d (an unkeyed mailbox never counts)", r.MaySayComplete(), in.OnDisk.SplitMailboxes)
	}
	in.OnDisk = s.onDiskFor(walked(3), false, "linux")
	r = Assess(in)
	want(t, "INBOX split differently", r, Unknown, ReasonSplitDifferently)
	if r.MaySayComplete() || r.SplitMailboxes != 1 || r.Reason.String() != "split_differently" {
		t.Errorf("split: complete %v, split %d, code %q", r.MaySayComplete(), r.SplitMailboxes, r.Reason)
	}
	// A certain gap still says behind.
	in.OnDisk.Folders["INBOX"] = 2
	want(t, "split with a certain gap", Assess(in), Behind, NoReason)
	// MaySayComplete alone, for a caught-up result that carries a split mailbox.
	if (Result{Verdict: CaughtUp, SplitMailboxes: 1}).MaySayComplete() {
		t.Error("a caught_up result with a split mailbox may say complete")
	}
}

// covers: MA-1175
// ADV-C2-02 in the verdict: a Gmail message with no identity key is left out of X (never counted
// once per copy), and the account may not say complete while any is there. Positive twin: every
// message keyed.
func TestAGmailMessageWithNoKeyBlocksComplete(t *testing.T) {
	s := gmailStore(t, 10, 4, 3)
	mk := func(noKey int64) []Mailbox {
		var keys [][16]byte
		for i := 0; i < 10; i++ {
			keys = append(keys, [16]byte{byte(i + 1)})
		}
		return []Mailbox{{Rel: "INBOX", Messages: 4, Keys: keys[:4]}, {Rel: "[Gmail].sbd/All Mail", Messages: 10 + noKey, Keys: keys, NoKey: noKey}}
	}
	in := gmail(s, 0)
	in.OnDisk = s.onDiskFor(mk(0), true, "linux")
	r := Assess(in)
	want(t, "positive twin", r, CaughtUp, NoReason)
	if !r.MaySayComplete() || r.OnDisk != 10 {
		t.Fatalf("positive twin: complete %v X %d", r.MaySayComplete(), r.OnDisk)
	}
	in.OnDisk = s.onDiskFor(mk(2), true, "linux")
	r = Assess(in)
	if r.OnDisk != 10 || r.NoIdentity != 2 || r.MaySayComplete() {
		t.Errorf("two messages with no key: X %d, no identity %d, complete %v; want 10, 2, false", r.OnDisk, r.NoIdentity, r.MaySayComplete())
	}
	// Not Gmail: NoKey never matters (no identity is kept for the per-folder rule).
	plain := healthy(s, map[string]int64{"INBOX": 4, "[Gmail]/All Mail": 10, "Label": 3})
	plain.OnDisk.NoKey = 7
	if r := Assess(plain); r.NoIdentity != 0 || !r.MaySayComplete() {
		t.Errorf("not Gmail: no identity %d, complete %v", r.NoIdentity, r.MaySayComplete())
	}
}

// covers: MA-1202
// ADV-C2-16 and a failed walk in the verdict: a store read by another reader, or whose walk
// failed, is unknown with its own reason, whatever its counts; a partial input stays
// partial_input (neutral words); a not-applicable account stays not_applicable. Positive twin:
// the same store, counted, is caught up.
func TestAStoreTheWalkDidNotCountIsUnknown(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 3, 3)))
	in := healthy(s, map[string]int64{"INBOX": 3})
	want(t, "positive twin", Assess(in), CaughtUp, NoReason)
	other := in
	other.OtherReader = true
	if r := Assess(other); r.Verdict != Unknown || r.Reason != ReasonOtherReader || r.Reason.String() != "other_reader" {
		t.Errorf("another reader: %v %q", r.Verdict, r.Reason)
	}
	failed := in
	failed.WalkFailed = true
	if r := Assess(failed); r.Verdict != Unknown || r.Reason != ReasonWalkFailed || r.Reason.String() != "walk_failed" {
		t.Errorf("a failed walk: %v %q", r.Verdict, r.Reason)
	}
	part := other
	part.WholeStore = false
	want(t, "a partial input read by another reader", Assess(part), Unknown, ReasonPartialInput)
	pop := other
	pop.Type = "pop3"
	want(t, "a POP3 account", Assess(pop), NotApplicable, NoReason)
	// The store's identity is carried to the result.
	id := in
	id.StoreID = "abc"
	if r := Assess(id); r.StoreID != "abc" {
		t.Errorf("store id %q", r.StoreID)
	}
}

// covers: MA-1276
// ADV-C2-17 (held, brought in): StoreAmong's other stores spelled with a trailing separator, a
// "." or ".." part, or doubled separators still own their keys in tail mode; and an other store
// that IS this store, respelled, never strips this store's exact keys. Positive twin: the other
// store as given owns its keys.
func TestADV_C2_17_OtherStoreSpellings(t *testing.T) {
	// Real absolute paths of this OS, as Thunderbird writes its keys (the pass ran it on macOS
	// with Unix spellings; on Windows those are made absolute onto a drive).
	sep := string(filepath.Separator)
	base := t.TempDir()
	this := filepath.Join(base, "p", "ImapMail", "h")
	other := filepath.Join(base, "q", "ImapMail", "h")
	data := cacheJSON(t, pair{other, root()}, pair{filepath.Join(other, "INBOX.msf"), folder(FlagInbox, 3, 3)})
	c, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.storeAmong([]string{this}, []string{other}, runtime.GOOS); s.Mode != NoKeys {
		t.Fatalf("positive twin: %+v", s)
	}
	q := filepath.Join(base, "q")
	for _, o := range []string{other + sep, q + sep + "x" + sep + ".." + sep + "ImapMail" + sep + "h", q + sep + sep + "ImapMail" + sep + "." + sep + "h"} {
		if s, _ := c.StoreAmong(this, []string{o}); s.Mode != NoKeys {
			t.Errorf("other store spelled %q: its keys were taken (%v, %d folders)", o, s.Mode, len(s.Folders))
		}
	}
	own := cacheJSON(t, pair{this, root()}, pair{filepath.Join(this, "INBOX.msf"), folder(FlagInbox, 5, 5)})
	c2, err := Parse([]byte(own))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(base, "p")
	for _, o := range []string{this, this + sep, p + sep + "x" + sep + ".." + sep + "ImapMail" + sep + "h"} {
		if s, _ := c2.StoreAmong(this, []string{o}); s.Mode != Exact || len(s.Folders) != 1 {
			t.Errorf("this store respelled %q as an other store: %v, %d folders", o, s.Mode, len(s.Folders))
		}
	}
}
