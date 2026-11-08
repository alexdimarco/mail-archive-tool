package tbcomplete

import (
	"math"
	"testing"
)

// covers: MA-1173
// Deleted copies not yet compacted (§2.4's TC-16 and M7 folds, owner default Q7), the verdict's
// half (the walk's 0x0008 count is C2's): per kept folder with expungedBytes > 0 the estimate
// ⌈expungedBytes ÷ (folderSize ÷ listed)⌉ is taken off the folder's count on disk (for Gmail, the
// sum off X), each clamped at 0, before missing and the tolerance; with nothing to divide by
// (listed ≤ 0, folderSize ≤ 0 or unknown) the account is unknown, reason expunged_unsized; and any
// expungedBytes > 0 on a kept folder keeps the complete words away (M7), whatever the verdict.
// Positive twin first: no deleted copies may say complete.
func TestDeletedCopiesAreCountedOut(t *testing.T) {
	kept := func(listed int, size, expunged int64) Store {
		return vstore(t, at("INBOX", folder(FlagInbox, listed, listed).with("folderSize", size, "expungedBytes", expunged)))
	}
	r := Assess(healthy(kept(100, 100_000_000, 0), map[string]int64{"INBOX": 100}))
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if r.DeletedCopies || r.ExpungedEstimate != 0 || !r.MaySayComplete() {
		t.Fatalf("the positive twin: %+v", r)
	}

	t.Run("the estimate, rounded up, computed exactly", func(t *testing.T) {
		for _, c := range []struct{ expunged, size, listed, want int64 }{
			{500_000, 100_000_000, 100, 1},   // M7's example: 0.5 rounds up to 1
			{2_000_000, 100_000_000, 100, 2}, // exact: never 3
			{2_000_001, 100_000_000, 100, 3},
			{1, 1 << 62, 1, 1},
			{1 << 62, 1, 1<<31 - 1, math.MaxInt64}, // past int64: capped, never wrapped
			{math.MaxInt64, math.MaxInt64, 1<<31 - 1, 1<<31 - 1},
		} {
			got, ok := ExpungedEstimate(c.expunged, c.size, c.listed)
			if !ok || got != c.want {
				t.Errorf("ExpungedEstimate(%d, %d, %d) = %d, %v; want %d", c.expunged, c.size, c.listed, got, ok, c.want)
			}
		}
		for _, c := range [][3]int64{{1, 0, 10}, {1, -1, 10}, {1, 100, 0}, {1, 100, -1}} {
			if _, ok := ExpungedEstimate(c[0], c[1], c[2]); ok {
				t.Errorf("ExpungedEstimate(%d, %d, %d): nothing to divide by, yet ok", c[0], c[1], c[2])
			}
		}
	})

	t.Run("deleted copies on disk without 0x0008 are taken off: not caught up (TC-16)", func(t *testing.T) {
		// 100 listed, about 1 MB each; 50 real messages and 50 deleted copies on disk.
		r := Assess(healthy(kept(100, 100_000_000, 50_000_000), map[string]int64{"INBOX": 100}))
		want(t, "50 copies of 1 MB", r, Behind, NoReason)
		if r.ExpungedEstimate != 50 || r.OnDisk != 50 || r.Missing != 50 || !r.DeletedCopies {
			t.Fatalf("50 copies of 1 MB: %+v", r)
		}
	})

	t.Run("the skewed-size fixture never says complete (M7)", func(t *testing.T) {
		// 100 listed, 1 MB average; 50 deleted copies of 10 KB (500 KB); 49 never downloaded: on
		// disk 51 + 50 = 101, the estimate 1, X 100, missing 0.
		r := Assess(healthy(kept(100, 100_000_000, 500_000), map[string]int64{"INBOX": 101}))
		want(t, "skewed sizes", r, CaughtUp, NoReason)
		if r.Missing != 0 || r.ExpungedEstimate != 1 || r.OnDisk != 100 || !r.DeletedCopies || r.MaySayComplete() {
			t.Fatalf("skewed sizes: %+v", r)
		}
	})

	t.Run("each folder clamped at 0", func(t *testing.T) {
		r := Assess(healthy(kept(100, 100, 1_000_000), map[string]int64{"INBOX": 3}))
		if r.OnDisk != 0 || r.Missing != 100 {
			t.Fatalf("an estimate above the count: X %d, missing %d; want 0, 100", r.OnDisk, r.Missing)
		}
	})

	t.Run("Gmail: the sum is taken off X", func(t *testing.T) {
		s := gmailStore(t, 1000, 500, 300).change(t, "[Gmail]/All Mail", func(e *Entry) {
			e.FolderSize, e.ExpungedBytes = Field{State: OK, Value: 1_000_000}, Field{State: OK, Value: 30_000}
		}).change(t, "INBOX", func(e *Entry) {
			e.FolderSize, e.ExpungedBytes = Field{State: OK, Value: 500_000}, Field{State: OK, Value: 10_000}
		})
		r := Assess(gmail(s, 1040))
		// All Mail 30 KB of 1 KB average: 30; INBOX 10 KB of 1 KB: 10. X = 1,040 - 40.
		want(t, "Gmail with copies", r, CaughtUp, NoReason)
		if r.ExpungedEstimate != 40 || r.OnDisk != 1000 || r.MaySayComplete() {
			t.Fatalf("Gmail with copies: %+v", r)
		}
		if r := Assess(gmail(s, 10)); r.OnDisk != 0 {
			t.Fatalf("Gmail X clamped: %d", r.OnDisk)
		}
	})

	t.Run("nothing to divide by is unknown", func(t *testing.T) {
		for name, s := range map[string]Store{
			"listed 0":           kept(0, 100, 10),
			"folderSize 0":       kept(10, 0, 10),
			"folderSize unknown": kept(10, -1, 10),
		} {
			want(t, name, Assess(healthy(s, map[string]int64{"INBOX": 10})), Unknown, ReasonExpungedUnsized)
		}
		notKeptCopies := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Old", notKept(0, 0).with("expungedBytes", 10)))
		want(t, "a not-kept folder's copies", Assess(healthy(notKeptCopies, map[string]int64{"INBOX": 10})), CaughtUp, NoReason)
	})
}
