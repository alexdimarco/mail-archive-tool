package tbcomplete

import (
	"testing"
)

// named is a folder's name under its store ("[Gmail]/All Mail") and its folder-cache value.
type named struct {
	name string
	f    fields
}

// ownerGmailFolders has the shape of the owner's Gmail account (§1, §2.5), invented names, counts
// only: All Mail 238,169 and every folder together 352,176 listed (1.48 times All Mail), Spam not
// kept (Thunderbird's default) and Trash not kept.
func ownerGmailFolders() []named {
	return []named{
		{"INBOX", folder(FlagInbox, 10881, 10881)},
		{"[Gmail]", folder(FlagImapNoselect, 0, 0)},
		{"[Gmail]/All Mail", folder(FlagAllMail|FlagArchive, 238169, 238169)},
		{"[Gmail]/Important", folder(0, 56625, 56625)},
		{"[Gmail]/Sent Mail", folder(FlagSentMail, 33822, 33822)},
		{"[Gmail]/Starred", folder(0, 6803, 6803)},
		{"[Gmail]/Spam", notKept(FlagJunk, 126)},
		{"[Gmail]/Trash", notKept(FlagTrash, 0)},
		{"Label A", folder(0, 33, 33)},
		{"Label B", folder(0, 772, 772)},
		{"Label C", folder(0, 4945, 4945)},
	}
}

// ownerShapedGmail is ownerGmailFolders as the store vStore.
func ownerShapedGmail(t *testing.T, more ...pair) Store {
	t.Helper()
	var pairs []pair
	for _, n := range ownerGmailFolders() {
		pairs = append(pairs, at(n.name, n.f))
	}
	return vstore(t, append(pairs, more...)...)
}

// covers: MA-1175
// Gmail's Y (§3 with its TC-1 and M5 folds): Y = max(All Mail's listed, the largest other kept
// folder's) plus Spam and Trash only when they are kept, never the sum over labels; X is the
// account's distinct count on disk (C2 supplies it), never one folder's, so Sent Mail with 2 of
// 33,822 on disk while All Mail is full is caught up and no folder is called behind; a folder with
// the All Mail flag on an account that is not Gmail keeps the per-folder rule; a walked mailbox with
// no cache key never adds to X and is counted (L3); and All Mail listing exactly 1,000, 2,000, 5,000
// or 10,000 adds TC-22's cap note, which keeps the complete words away. Positive twin first: the
// owner-shaped account with everything on disk is caught up and may say complete.
func TestGmailYIsAllMailNeverTheSum(t *testing.T) {
	s := ownerShapedGmail(t)
	in := gmail(s, 238169)
	in.OnDisk.Folders = map[string]int64{"[Gmail]/All Mail": 238169, "INBOX": 10881, "[Gmail]/Sent Mail": 2}
	r := Assess(in)
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if !r.Gmail || r.Floor || r.AllMail != AllMailSetsY || r.Listed != 238169 || r.OnDisk != 238169 || r.Missing != 0 || r.Tolerance != 476 || !r.MaySayComplete() {
		t.Fatalf("the positive twin: %+v", r)
	}
	var sum int64
	for _, e := range s.Folders {
		n, _ := e.Listed()
		sum += n
	}
	if sum != 352176 || r.Listed == sum {
		t.Fatalf("the fixture lists %d in all, Y %d: Y must be All Mail, never the sum", sum, r.Listed)
	}
	if len(r.NotKept) != 0 || len(r.DefaultNotKept) != 2 {
		t.Fatalf("Spam and Trash are notes only: not kept %v, default not kept %v", r.NotKept, r.DefaultNotKept)
	}
	sent := false
	for _, f := range r.Folders {
		if f.Path == "[Gmail]/Sent Mail" {
			sent = f.Listed == 33822 && f.OnDisk == 2
		}
	}
	if !sent {
		t.Fatalf("the record's per-folder counts lack Sent Mail's 2 of 33,822: %v", r.Folders)
	}

	t.Run("the tolerance at Gmail's Y", func(t *testing.T) {
		want(t, "476 short", Assess(gmail(s, 238169-476)), CaughtUp, NoReason)
		want(t, "477 short", Assess(gmail(s, 238169-477)), Behind, NoReason)
		want(t, "the owner's first backup, 1,670 of 238,169", Assess(gmail(s, 1670)), Behind, NoReason)
		want(t, "nothing on disk", Assess(gmail(s, 0)), Behind, NoReason)
		// The owner's +32 extra separators in All Mail (N5): missing is clamped at 0.
		r := Assess(gmail(s, 238169+32))
		want(t, "32 more on disk than listed", r, CaughtUp, NoReason)
		if r.Missing != 0 || r.OnDisk != 238169+32 || !r.MaySayComplete() {
			t.Fatalf("32 more on disk than listed: %+v", r)
		}
	})

	t.Run("a label larger than All Mail sets Y", func(t *testing.T) {
		// All Mail stale at 1,000 against a label of 1,010: All Mail is within the label's
		// tolerance (20), so it sets Y, and Y is the larger.
		r := Assess(gmail(gmailStore(t, 1000, 500, 1010), 1010))
		want(t, "label 1,010", r, CaughtUp, NoReason)
		if r.Listed != 1010 || r.Floor {
			t.Fatalf("Y %d, floor %v; want 1,010, not a floor", r.Listed, r.Floor)
		}
	})

	t.Run("Spam and Trash are added only when kept, and are never the largest folder", func(t *testing.T) {
		r := Assess(gmail(gmailStore(t, 1000, 500, 300, at("[Gmail]/Spam", folder(FlagJunk, 40, 40)), at("[Gmail]/Trash", folder(FlagTrash, 2000, 2000))), 3040))
		want(t, "kept Spam and Trash", r, CaughtUp, NoReason)
		if r.Listed != 1000+40+2000 {
			t.Fatalf("Y %d, want All Mail 1,000 + Spam 40 + Trash 2,000", r.Listed)
		}
		r = Assess(gmail(gmailStore(t, 1200, 500, 300, at("[Gmail]/Spam", notKept(FlagJunk, 40)), at("[Gmail]/Trash", notKept(FlagTrash, 2000))), 1200))
		want(t, "Spam and Trash not kept", r, CaughtUp, NoReason)
		if r.Listed != 1200 || !r.MaySayComplete() || len(r.DefaultNotKept) != 2 {
			t.Fatalf("Y %d, may say complete %v, notes %v; want 1,000, true, two notes", r.Listed, r.MaySayComplete(), r.DefaultNotKept)
		}
	})

	t.Run("an All Mail folder on an account that is not Gmail keeps the per-folder rule", func(t *testing.T) {
		in := healthy(gmailStore(t, 1000, 500, 300), map[string]int64{"[Gmail]/All Mail": 1000, "Label": 300})
		in.OnDisk.Distinct = 1000
		r := Assess(in)
		want(t, "not Gmail", r, Behind, NoReason)
		if r.Gmail || r.AllMail != AllMailNotGmail || r.Listed != 1800 || r.Missing != 500 {
			t.Fatalf("not Gmail: %+v", r)
		}
	})

	t.Run("a walked mailbox with no cache key never adds to X (L3)", func(t *testing.T) {
		in := gmail(gmailStore(t, 1000, 500, 300), 900)
		in.OnDisk.UnkeyedMailboxes, in.OnDisk.UnkeyedMessages = 1, 500
		r := Assess(in)
		want(t, "a gap in All Mail with an unkeyed mailbox", r, Behind, NoReason)
		if r.OnDisk != 900 || r.UnkeyedMailboxes != 1 || r.UnkeyedMessages != 500 {
			t.Fatalf("X %d, unkeyed %d/%d; want 900, counted 1/500", r.OnDisk, r.UnkeyedMailboxes, r.UnkeyedMessages)
		}
	})

	t.Run("TC-22: All Mail listing exactly a cap size adds the note and never complete", func(t *testing.T) {
		for _, n := range []int{1000, 2000, 5000, 10000} {
			r := Assess(gmail(gmailStore(t, n, 10, 10), int64(n)))
			want(t, "at the cap", r, CaughtUp, NoReason)
			if r.CapNote != int64(n) || r.MaySayComplete() {
				t.Fatalf("All Mail %d: cap note %d, may say complete %v", n, r.CapNote, r.MaySayComplete())
			}
		}
		for _, n := range []int{999, 1001, 9999, 10001, 20000} {
			if r := Assess(gmail(gmailStore(t, n, 10, 10), int64(n))); r.CapNote != 0 || !r.MaySayComplete() {
				t.Fatalf("All Mail %d: cap note %d, may say complete %v", n, r.CapNote, r.MaySayComplete())
			}
		}
		// Not Gmail: no cap note.
		if r := Assess(healthy(gmailStore(t, 1000, 10, 10), map[string]int64{"[Gmail]/All Mail": 1000, "INBOX": 10, "Label": 10})); r.CapNote != 0 {
			t.Fatalf("not Gmail: cap note %d", r.CapNote)
		}
	})
}
