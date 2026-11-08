package tbcomplete

// The C2 adversarial pass round 2's fixes in the verdict
// (docs/review-thunderbird-completeness-c2-adversarial-r2.md: R2-04, R2-05, R2-06). Each with its
// positive twin first.

import "testing"

// covers: MA-1279
// Two counts (the lead's call on R2-04): caught up only by the STRICT count (pieces carrying
// X-Mozilla-Status), behind only by the LENIENT one (every piece at a separator), else unknown,
// count_uncertain; so a piece without the line can neither fake complete nor fake behind. The
// owner-shaped case: 2 of 170 without the line is strict short by 2, within the tolerance (17):
// still caught up, never complete (missing 2). A separator glued to the line before it (R2-05)
// makes the account unknown, split_differently, never behind. A body "From " line of another shape
// (a git patch) still lets a certain gap say behind. Positive twin: all 170 counted for certain.
func TestBehindByTheLenientCountCaughtUpByTheStrict(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 170, 170)))
	in := func(strict, loose int64) Input {
		i := healthy(s, nil)
		i.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX", Messages: strict, Loose: loose}}, false, "linux")
		return i
	}
	r := Assess(in(170, 0))
	want(t, "positive twin", r, CaughtUp, NoReason)
	if !r.MaySayComplete() || r.OnDiskMost != 170 || r.MissingLeast != 0 {
		t.Fatalf("positive twin: %+v", r)
	}
	r = Assess(in(168, 2))
	want(t, "owner-shaped: 2 of 170 without X-Mozilla-Status", r, CaughtUp, NoReason)
	if r.MaySayComplete() || r.Missing != 2 || r.OnDisk != 168 || r.OnDiskMost != 170 || r.NoStatus != 2 || r.Tolerance != 17 {
		t.Errorf("owner-shaped: complete %v missing %d X %d most %d no status %d tolerance %d", r.MaySayComplete(), r.Missing, r.OnDisk, r.OnDiskMost, r.NoStatus, r.Tolerance)
	}
	// Strict short past the tolerance, lenient whole: can't tell.
	r = Assess(in(140, 30))
	want(t, "30 loose pieces cover the strict gap", r, Unknown, ReasonCountUncertain)
	if r.Reason.String() != "count_uncertain" || r.MaySayComplete() {
		t.Errorf("code %q complete %v", r.Reason, r.MaySayComplete())
	}
	// Short by both counts: behind; the edge of the tolerance on the lenient count.
	want(t, "140 and 0 loose", Assess(in(140, 0)), Behind, NoReason)
	want(t, "140 and 12 loose: lenient 18 short", Assess(in(140, 12)), Behind, NoReason)
	want(t, "140 and 13 loose: lenient 17 short, the tolerance", Assess(in(140, 13)), Unknown, ReasonCountUncertain)
	// A glued separator: never behind, however short.
	g := healthy(s, nil)
	g.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX", Messages: 10, Glued: 4}}, false, "linux")
	r = Assess(g)
	want(t, "a glued separator, 160 short", r, Unknown, ReasonSplitDifferently)
	if r.GluedMailboxes != 1 || r.SplitMailboxes != 1 || r.MaySayComplete() {
		t.Errorf("glued: %d glued, %d split, complete %v", r.GluedMailboxes, r.SplitMailboxes, r.MaySayComplete())
	}
	// A body line of another shape (the git patch) with a certain gap: behind.
	b := healthy(s, nil)
	b.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX", Messages: 10, Unseparated: 4}}, false, "linux")
	want(t, "a split mailbox, 160 short", Assess(b), Behind, NoReason)
	// Limits turn a lenient behind into limited, never a count_uncertain.
	lim := in(140, 0)
	lim.Limits.MaxAgeDays = 30
	want(t, "behind with a limit", Assess(lim), Limited, NoReason)
	lim = in(140, 30)
	lim.Limits.MaxAgeDays = 30
	want(t, "uncertain with a limit", Assess(lim), Unknown, ReasonCountUncertain)
}

// covers: MA-1279
// Gmail's two counts (R2-04, with the builder's reading for messages with no identity): the
// lenient X adds the loose pieces' identities not already counted and every message with no
// identity key (each may be one more email), so a gap by it is certain. Positive twin: every
// message keyed and carrying the line.
func TestGmailBehindOnlyByTheLenientX(t *testing.T) {
	s := gmailStore(t, 10, 4, 3)
	k := func(i int) [16]byte { return [16]byte{byte(i)} }
	keys := func(from, to int) [][16]byte {
		var out [][16]byte
		for i := from; i <= to; i++ {
			out = append(out, k(i))
		}
		return out
	}
	assess := func(mb ...Mailbox) Result {
		in := gmail(s, 0)
		in.OnDisk = s.onDiskFor(mb, true, "linux")
		return Assess(in)
	}
	want(t, "positive twin", assess(Mailbox{Rel: "[Gmail].sbd/All Mail", Messages: 10, Keys: keys(1, 10)}), CaughtUp, NoReason)
	// 8 for certain, 2 loose new identities: can't tell.
	r := assess(Mailbox{Rel: "[Gmail].sbd/All Mail", Messages: 8, Keys: keys(1, 8), Loose: 2, LooseKeys: keys(9, 10)})
	want(t, "8 strict, 2 loose new", r, Unknown, ReasonCountUncertain)
	if r.OnDisk != 8 || r.OnDiskMost != 10 {
		t.Errorf("X %d, most %d; want 8, 10", r.OnDisk, r.OnDiskMost)
	}
	// Loose copies of counted emails add nothing (in another folder too): behind.
	r = assess(Mailbox{Rel: "[Gmail].sbd/All Mail", Messages: 8, Keys: keys(1, 8)}, Mailbox{Rel: "INBOX", Messages: 0, Loose: 2, LooseKeys: keys(1, 2)})
	want(t, "8 strict, 2 loose copies", r, Behind, NoReason)
	if r.OnDiskMost != 8 {
		t.Errorf("most %d, want 8", r.OnDiskMost)
	}
	// Messages with no identity, strict or loose, may each be one more: can't tell.
	want(t, "8 keyed and 2 with no key", assess(Mailbox{Rel: "[Gmail].sbd/All Mail", Messages: 10, Keys: keys(1, 8), NoKey: 2}), Unknown, ReasonCountUncertain)
	want(t, "8 keyed and 2 loose with no key", assess(Mailbox{Rel: "[Gmail].sbd/All Mail", Messages: 8, Keys: keys(1, 8), Loose: 2, LooseNoKey: 2}), Unknown, ReasonCountUncertain)
	// A loose piece in a folder not kept never adds to the lenient X either.
	sp := gmailStore(t, 10, 4, 3, at("[Gmail]/Spam", notKept(FlagJunk, 5)))
	in := gmail(sp, 0)
	in.OnDisk = sp.onDiskFor([]Mailbox{{Rel: "[Gmail].sbd/All Mail", Messages: 8, Keys: keys(1, 8)}, {Rel: "[Gmail].sbd/Spam", Loose: 2, LooseKeys: keys(20, 21)}}, true, "linux")
	if r := Assess(in); r.Verdict != Behind || r.OnDiskMost != 8 {
		t.Errorf("loose pieces of a folder not kept: %v most %d; want behind, 8", r.Verdict, r.OnDiskMost)
	}
	// The floor (All Mail not checked) is told behind only by the lenient X too.
	fl := gmailStore(t, 10, 4, 3)
	for i := range fl.Folders {
		if fl.Folders[i].Path == "[Gmail]/All Mail" {
			fl.Folders[i].LastSyncTimeInSec = Field{State: OK, Value: 0}
		}
	}
	in = gmail(fl, 0)
	in.OnDisk = fl.onDiskFor([]Mailbox{{Rel: "[Gmail].sbd/All Mail", Messages: 1, Keys: keys(1, 1), Loose: 3, LooseKeys: keys(2, 4)}}, true, "linux")
	r = Assess(in)
	if r.Verdict == Behind || !r.Floor {
		t.Errorf("a floor of 4 with X 1 for certain and 4 at most: %v floor %v", r.Verdict, r.Floor)
	}
	in.OnDisk = fl.onDiskFor([]Mailbox{{Rel: "[Gmail].sbd/All Mail", Messages: 1, Keys: keys(1, 1)}}, true, "linux")
	want(t, "a floor of 4 with X 1 at most", Assess(in), Behind, NoReason)
}

// covers: MA-1279
// "Lists no mail" (TC-2's exception) needs nothing on disk, loose pieces included: a store
// that lists 0 everywhere while a mailbox holds a loose piece is not caught up by it. Positive
// twin: nothing on disk.
func TestLooseMailOnDiskIsNotNoMail(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)))
	in := healthy(s, nil)
	in.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX"}}, false, "linux")
	r := Assess(in)
	if r.Verdict != CaughtUp || !r.ListsNoMail {
		t.Fatalf("positive twin: %+v", r)
	}
	in.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX", Loose: 1}}, false, "linux")
	want(t, "a loose piece while every folder lists 0", Assess(in), Unknown, ReasonCacheUnreadable)
	in.OnDisk = s.onDiskFor([]Mailbox{{Rel: "INBOX"}, {Rel: "Leftover", Loose: 1}}, false, "linux")
	if r := Assess(in); r.Verdict != Unknown || r.UnkeyedMessages != 1 {
		t.Errorf("a loose piece in a mailbox with no cache key while every folder lists 0: %v unkeyed %d", r.Verdict, r.UnkeyedMessages)
	}
	g := gmail(gmailStore(t, 0, 0, 0), 0)
	g.OnDisk.LooseDistinct = 1
	want(t, "Gmail, a loose identity while every folder lists 0", Assess(g), Unknown, ReasonCacheUnreadable)
}

// covers: MA-1282
// R2-06: a lead built from a record that left accounts out takes each one left out as unknown:
// never LeadComplete, and a behind account still leads so_far. Positive twin: none left out.
func TestALeadWithAccountsLeftOutIsNeverComplete(t *testing.T) {
	complete := Result{Verdict: CaughtUp, Listed: 5, OnDisk: 5}
	if RunLeadWith([]Result{complete}, 0) != LeadComplete || RunLead([]Result{complete}) != LeadComplete {
		t.Fatal("positive twin: a complete account does not lead complete")
	}
	if got := RunLeadWith([]Result{complete}, 1); got != LeadFinished {
		t.Errorf("one account left out: %v, want finished", got)
	}
	if got := RunLeadWith(nil, 3); got != LeadFinished {
		t.Errorf("only accounts left out: %v, want finished", got)
	}
	if got := RunLeadWith([]Result{complete, {Verdict: Behind}}, 1); got != LeadSoFar {
		t.Errorf("a behind account and one left out: %v, want so_far", got)
	}
}
