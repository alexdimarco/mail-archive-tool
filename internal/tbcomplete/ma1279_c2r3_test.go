package tbcomplete

// The C2 adversarial pass round 3's fix in the verdict
// (docs/review-thunderbird-completeness-c2-adversarial-r3.md: R3-01). Positive twin first.

import "testing"

// covers: MA-1279
// R3-01, the lead's call: a mailbox whose reader's pieces and the separator scan's notes differ in
// number (Misaligned) had none of its pieces counted, so it is split differently and its account
// is unknown, split_differently: never behind however short it looks, never caught up, never
// complete. A misaligned mailbox no folder key names still counts its pieces as unkeyed messages,
// so the account never "lists no mail" over them. Positive twin: the same mailbox aligned and short
// is behind; an empty store with no stray mailbox lists no mail.
func TestAMisalignedMailboxIsNeverBehind(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 170, 170)))
	in := func(mb Mailbox) Input {
		i := healthy(s, nil)
		i.OnDisk = s.onDiskFor([]Mailbox{mb}, false, "linux")
		return i
	}
	want(t, "positive twin: aligned, 160 short", Assess(in(Mailbox{Rel: "INBOX", Messages: 10})), Behind, NoReason)
	r := Assess(in(Mailbox{Rel: "INBOX", Misaligned: true, Uncounted: 13}))
	want(t, "misaligned, nothing counted", r, Unknown, ReasonSplitDifferently)
	if r.MisalignedMailboxes != 1 || r.SplitMailboxes != 1 || r.GluedMailboxes != 0 || r.MaySayComplete() || r.Reason.String() != "split_differently" {
		t.Errorf("misaligned: %d misaligned, %d split, %d glued, complete %v", r.MisalignedMailboxes, r.SplitMailboxes, r.GluedMailboxes, r.MaySayComplete())
	}
	// Even with a limit on, which would make a behind account "limited".
	lim := in(Mailbox{Rel: "INBOX", Misaligned: true, Uncounted: 13})
	lim.Limits.MaxAgeDays = 30
	want(t, "misaligned with a limit", Assess(lim), Unknown, ReasonSplitDifferently)

	empty := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)))
	ein := healthy(empty, nil)
	ein.OnDisk = empty.onDiskFor([]Mailbox{{Rel: "INBOX"}}, false, "linux")
	if r := Assess(ein); r.Verdict != CaughtUp || !r.ListsNoMail {
		t.Fatalf("positive twin: an empty store: %v %v lists no mail %v", r.Verdict, r.Reason, r.ListsNoMail)
	}
	stray := healthy(empty, nil)
	stray.OnDisk = empty.onDiskFor([]Mailbox{{Rel: "INBOX"}, {Rel: "Stray.d/x", Misaligned: true, Uncounted: 4}}, false, "linux")
	if r := Assess(stray); r.ListsNoMail || stray.OnDisk.UnkeyedMessages != 4 {
		t.Errorf("an unkeyed misaligned mailbox of 4 pieces: %v lists no mail %v, unkeyed messages %d", r.Verdict, r.ListsNoMail, stray.OnDisk.UnkeyedMessages)
	}
}
