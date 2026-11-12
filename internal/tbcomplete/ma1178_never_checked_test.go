package tbcomplete

import (
	"testing"
)

// covers: MA-1178
// A kept folder that lists mail and was never checked (lastSyncTimeInSec ≤ 0 or absent, §4.4; the
// fresh-imap shape) makes the account unknown, reason never_checked, unless the other folders
// already show a certain gap (behind, as M2's rule for a can't-tell folder); so does a kept folder
// the server has not listed (no ImapBox flag, C1A-07's fail-safe), whatever its count and even with
// lastSyncTimeInSec present. The "lists no mail" exception (§4.1 step 3's TC-2 fold): every entry
// valid, every kept folder checked, empty ones included, every folder listing 0 and nothing on disk
// is caught up; messages on disk while every folder lists 0, or no folder at all, is unknown
// (cache_unreadable, "could not read Thunderbird's list of folders"). Positive twin first.
func TestAKeptFolderNeverCheckedIsUnknown(t *testing.T) {
	r := Assess(healthy(vstore(t, at("INBOX", folder(FlagInbox, 0, 0))), nil))
	want(t, "the positive twin: checked, lists no mail", r, CaughtUp, NoReason)
	if !r.ListsNoMail || !r.MaySayComplete() || r.Listed != 0 {
		t.Fatalf("the positive twin: %+v", r)
	}

	t.Run("never checked", func(t *testing.T) {
		for name, sync := range map[string]any{"absent": nil, "0": 0, "negative (the 2038 wrap)": -5} {
			s := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Sent", folder(FlagSentMail, 10, 10).with("lastSyncTimeInSec", sync)))
			r := Assess(healthy(s, map[string]int64{"INBOX": 10, "Sent": 10}))
			want(t, "Sent's lastSyncTimeInSec "+name, r, Unknown, ReasonNeverChecked)
			if r.MaySayComplete() {
				t.Fatalf("%s: may say complete", name)
			}
		}
		fresh := vstore(t, at("INBOX", folder(FlagInbox, 0, 0).with("flags", 0x08001014, "lastSyncTimeInSec", nil)))
		r := Assess(healthy(fresh, nil))
		want(t, "the fresh-imap shape (Windows lab, never connected)", r, Unknown, ReasonNeverChecked)
		if r.AsOf != 0 || r.Stale {
			t.Fatalf("no count time: as of %d, stale %v; want 0, not stale", r.AsOf, r.Stale)
		}
		empty := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Empty", folder(0, 0, 0).with("lastSyncTimeInSec", nil)))
		want(t, "an empty kept folder never checked beside a checked one", Assess(healthy(empty, map[string]int64{"INBOX": 10})), CaughtUp, NoReason)
		listsNone := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)), at("Empty", folder(0, 0, 0).with("lastSyncTimeInSec", nil)))
		want(t, "every folder lists 0, one never checked", Assess(healthy(listsNone, nil)), Unknown, ReasonNeverChecked)
		notKeptOld := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Old", notKept(0, 5).with("lastSyncTimeInSec", nil)))
		if r := Assess(healthy(notKeptOld, map[string]int64{"INBOX": 10})); r.Verdict != CaughtUp {
			t.Fatalf("a not-kept folder never checked: %v (%v)", r.Verdict, r.Reason)
		}
	})

	t.Run("a certain gap elsewhere is behind", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 100, 100)), at("Sent", folder(FlagSentMail, 10, 10).with("lastSyncTimeInSec", nil)))
		r := Assess(healthy(s, map[string]int64{"Sent": 10}))
		want(t, "INBOX empty on disk, Sent never checked", r, Behind, NoReason)
		if r.Listed != 100 || r.Missing != 100 {
			t.Fatalf("the never-checked folder is left out of X and Y: %+v", r)
		}
	})

	t.Run("C1A-07: a kept folder without ImapBox is never caught up, even checked", func(t *testing.T) {
		for _, n := range []int{0, 5} {
			s := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Unlisted", folder(0, n, 0).with("flags", FlagOffline)))
			if e := folderAt(t, s, "Unlisted"); !e.Valid() || e.LastSyncTimeInSec.State != OK {
				t.Fatalf("the fixture: %+v", e)
			}
			r := Assess(healthy(s, map[string]int64{"INBOX": 10, "Unlisted": int64(n)}))
			want(t, "a folder the server has not listed", r, Unknown, ReasonNeverChecked)
		}
		notKeptUnlisted := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Unlisted", folder(0, 0, 0).with("flags", 0)))
		want(t, "a not-kept folder without ImapBox", Assess(healthy(notKeptUnlisted, map[string]int64{"INBOX": 10})), CaughtUp, NoReason)
	})

	t.Run("lists no mail, the exception's edges", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)), at("Sent", folder(FlagSentMail, 0, 0)))
		want(t, "messages on disk while every folder lists 0", Assess(healthy(s, map[string]int64{"INBOX": 3})), Unknown, ReasonCacheUnreadable)
		want(t, "one message on disk while every folder lists 0", Assess(healthy(s, map[string]int64{"INBOX": 0, "Sent": 1})), Unknown, ReasonCacheUnreadable)
		skipped := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)), at("Old", notKept(0, 0).with("totalMsgs", "0")))
		r := Assess(healthy(skipped, nil))
		want(t, "every folder lists 0 but one entry does not validate", r, Unknown, ReasonCacheUnreadable)
		if r.SkippedEntries != 1 {
			t.Fatalf("the entry that does not validate is skipped and counted: %d", r.SkippedEntries)
		}
		in := healthy(s, nil)
		in.OnDisk.UnkeyedMailboxes, in.OnDisk.UnkeyedMessages = 1, 3
		want(t, "an unkeyed mailbox with messages while every folder lists 0", Assess(in), Unknown, ReasonCacheUnreadable)
		want(t, "Gmail: distinct messages while every folder lists 0", Assess(gmail(gmailStore(t, 0, 0, 0), 3)), Unknown, ReasonCacheUnreadable)
		r0 := Assess(gmail(gmailStore(t, 0, 0, 0), 0))
		want(t, "Gmail listing no mail, nothing on disk", r0, CaughtUp, NoReason)
		if !r0.ListsNoMail {
			t.Fatalf("Gmail listing no mail: %+v", r0)
		}
		want(t, "no folder at all, only the root", Assess(healthy(vstore(t), nil)), Unknown, ReasonCacheUnreadable)
		notKeptMail := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)), at("Archive", notKept(0, 40)))
		r = Assess(healthy(notKeptMail, nil))
		want(t, "no countable kept folder, a not-kept folder lists mail", r, Unknown, ReasonCacheUnreadable)
		if len(r.NotKept) != 1 || r.NotKept[0].Path != "Archive" || r.NotKept[0].Listed != 40 {
			t.Fatalf("the not-kept folder is still reported: %v", r.NotKept)
		}
	})
}
