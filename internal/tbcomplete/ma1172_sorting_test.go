package tbcomplete

import (
	"testing"
)

// covers: MA-1172
// Sorting a store's folders (§4.1 step 3 with its TC-2, TC-8 and M3 folds, and §0's lead's call on
// not-kept folders): kept = the Offline flag, and not Virtual and not Noselect; Trash or Junk
// without Offline is "not kept, by default", a note only; for Gmail whose All Mail sets Y, a label
// not kept is one note only (its mail is backed up through All Mail); any other folder without
// Offline that lists mail is "not kept", reported beside any verdict, never folded into behind, and
// it keeps the complete words away (L4); a not-kept folder that lists 0, a virtual folder and a
// Noselect folder are ignored. A not-kept entry that fails validation is skipped and counted, unless
// the walk read it; a kept one is invalid_entry (C1A-01's not_counted_yet when its one failure is
// Thunderbird's -1), and so is one whose flags cannot be read (M3: kept, since nobody can tell).
// Positive twin first.
func TestFoldersAreSortedByTheirFlags(t *testing.T) {
	base := []pair{at("INBOX", folder(FlagInbox, 10, 10))}
	disk := map[string]int64{"INBOX": 10}
	r := Assess(healthy(vstore(t, base...), disk))
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if !r.MaySayComplete() || len(r.NotKept)+len(r.DefaultNotKept)+r.LabelsNotKept+r.SkippedEntries != 0 {
		t.Fatalf("the positive twin: %+v", r)
	}
	with := func(p ...pair) Result {
		return Assess(healthy(vstore(t, append(append([]pair{}, base...), p...)...), disk))
	}

	t.Run("virtual and Noselect are ignored even with Offline", func(t *testing.T) {
		r := with(at("Search", folder(FlagVirtual, 500, 500)), at("Parent", folder(FlagImapNoselect, 500, 500)))
		want(t, "virtual and Noselect", r, CaughtUp, NoReason)
		if r.Listed != 10 || !r.MaySayComplete() {
			t.Fatalf("virtual and Noselect counted: %+v", r)
		}
	})

	t.Run("Trash and Junk not kept are a note only", func(t *testing.T) {
		r := with(at("Trash", notKept(FlagTrash, 50)), at("Junk", notKept(FlagJunk, 234)))
		want(t, "Trash and Junk", r, CaughtUp, NoReason)
		if len(r.DefaultNotKept) != 2 || len(r.NotKept) != 0 || !r.MaySayComplete() {
			t.Fatalf("Trash and Junk: %+v", r)
		}
	})

	t.Run("any other folder not kept is reported, keeps its verdict, and blocks complete", func(t *testing.T) {
		r := with(at("Projects", notKept(0, 772)), at("Receipts", notKept(0, 33)), at("Nothing", notKept(0, 0)))
		want(t, "two not kept", r, CaughtUp, NoReason)
		if len(r.NotKept) != 2 || r.NotKept[0].Path != "Projects" || r.NotKept[0].Listed != 772 || r.NotKept[1].Path != "Receipts" || r.MaySayComplete() || r.Listed != 10 {
			t.Fatalf("two not kept: %+v", r)
		}
	})

	t.Run("a not-kept folder the walk read is still not kept", func(t *testing.T) {
		s := vstore(t, append(append([]pair{}, base...), at("Archive", notKept(0, 40)))...)
		r := Assess(healthy(s, map[string]int64{"INBOX": 10, "Archive": 40}))
		want(t, "a not-kept folder with its mailbox file", r, CaughtUp, NoReason)
		if r.Listed != 10 || len(r.NotKept) != 1 || r.NotKept[0].OnDisk != 40 || len(r.Folders) != 1 {
			t.Fatalf("a not-kept folder with its mailbox file is counted as kept: %+v", r)
		}
	})

	t.Run("Gmail labels not kept are one note while All Mail sets Y; All Mail not kept is reported", func(t *testing.T) {
		var labels []pair
		for i := 0; i < 40; i++ {
			labels = append(labels, at("L"+string(rune('a'+i%26))+string(rune('a'+i/26)), notKept(0, 10)))
		}
		labels = append(labels, at("[Gmail]/Spam", notKept(FlagJunk, 126)))
		r := Assess(gmail(gmailStore(t, 1200, 500, 300, labels...), 1200)) // 1,200: no TC-22 cap note
		want(t, "40 labels not kept", r, CaughtUp, NoReason)
		if r.LabelsNotKept != 40 || len(r.NotKept) != 0 || len(r.DefaultNotKept) != 1 || !r.MaySayComplete() {
			t.Fatalf("40 labels not kept: %+v", r)
		}
		r = Assess(gmail(gmailStore(t, 1000, 500, 300, at("Label X", notKept(0, 10))).withFlags(t, "[Gmail]/All Mail", FlagAllMail|FlagArchive|FlagImapBox), 1000))
		if len(r.NotKept) != 2 || r.LabelsNotKept != 0 || r.Verdict == CaughtUp {
			t.Fatalf("All Mail not kept: All Mail and the label are both not kept: %+v", r)
		}
	})

	t.Run("a not-kept entry that fails validation is skipped and counted, unless walked", func(t *testing.T) {
		bad := at("Old", notKept(0, 5).with("totalMsgs", "5"))
		r := with(bad)
		want(t, "skipped", r, CaughtUp, NoReason)
		if r.SkippedEntries != 1 {
			t.Fatalf("skipped: %d", r.SkippedEntries)
		}
		in := healthy(vstore(t, append(append([]pair{}, base...), bad)...), map[string]int64{"INBOX": 10, "Old": 5})
		want(t, "walked", Assess(in), Unknown, ReasonInvalidEntry)
		virtualBad := at("Search", folder(FlagVirtual, 5, 5).with("totalMsgs", "5"))
		if r := with(virtualBad); r.Verdict != CaughtUp || r.SkippedEntries != 1 {
			t.Fatalf("a virtual entry that fails: %v, skipped %d", r.Verdict, r.SkippedEntries)
		}
		notCounted := at("Fresh", notKept(0, 0).with("totalMsgs", -1))
		in = healthy(vstore(t, append(append([]pair{}, base...), notCounted)...), map[string]int64{"INBOX": 10, "Fresh": 0})
		want(t, "walked, not counted yet", Assess(in), Unknown, ReasonNotCountedYet)
	})

	t.Run("a kept entry that fails is invalid_entry; Thunderbird's -1 alone is not_counted_yet", func(t *testing.T) {
		want(t, "a string totalMsgs", with(at("Sent", folder(FlagSentMail, 5, 5).with("totalMsgs", "5"))), Unknown, ReasonInvalidEntry)
		want(t, "no serverTotal", with(at("Sent", folder(FlagSentMail, 5, 5).with("serverTotal", nil))), Unknown, ReasonInvalidEntry)
		want(t, "totalMsgs -1", with(at("Sent", folder(FlagSentMail, 5, 5).with("totalMsgs", -1))), Unknown, ReasonNotCountedYet)
		want(t, "-1 and a broken entry", with(at("Sent", folder(FlagSentMail, 5, 5).with("totalMsgs", -1)), at("Z", folder(0, 5, 5).with("pendingMsgs", 1.5))), Unknown, ReasonInvalidEntry)
		want(t, "a broken entry and -1 (either order)", with(at("A", folder(0, 5, 5).with("pendingMsgs", 1.5)), at("Sent", folder(FlagSentMail, 5, 5).with("totalMsgs", -1))), Unknown, ReasonInvalidEntry)
	})

	t.Run("M3: flags that cannot be read count as kept, nothing on disk", func(t *testing.T) {
		want(t, "a string flags", with(at("Lost", folder(0, 5, 5).with("flags", "x"))), Unknown, ReasonInvalidEntry)
		want(t, "no flags", with(at("Lost", folder(0, 5, 5).with("flags", nil))), Unknown, ReasonInvalidEntry)
		want(t, "not an object", with(pair{vStore + "/Lost.msf", []int{1}}), Unknown, ReasonInvalidEntry)
	})
}
