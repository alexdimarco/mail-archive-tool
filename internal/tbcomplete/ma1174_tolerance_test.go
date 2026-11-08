package tbcomplete

import (
	"fmt"
	"testing"
)

// covers: MA-1174
// The tolerance is owner ruling O2's max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), applied once per account,
// never per folder (§4.3's TC-4 fold); each folder's shortfall is clamped at 0 before the sum, so
// a folder holding extra copies cannot hide another's gap; nothing on disk is never caught up; and
// any mail missing keeps the "complete" words away, with both numbers in the result. Positive twin
// first: every message on disk is caught up and may say complete.
func TestToleranceIsO2sFormulaOncePerAccount(t *testing.T) {
	one := func(listed int, disk int64) Result {
		return Assess(healthy(vstore(t, at("INBOX", folder(FlagInbox, listed, listed))), map[string]int64{"INBOX": disk}))
	}
	r := one(100, 100)
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if r.Missing != 0 || r.Listed != 100 || r.OnDisk != 100 || !r.MaySayComplete() {
		t.Fatalf("the positive twin: %+v", r)
	}

	t.Run("the worked values and every boundary", func(t *testing.T) {
		for _, c := range [][2]int64{
			{0, 0}, {1, 0}, {9, 0}, {10, 1}, {15, 1}, {19, 1}, {20, 2}, {100, 10}, {199, 19}, {200, 20},
			{201, 20}, {10000, 20}, {10499, 20}, {10500, 21}, {10999, 21}, {11000, 22}, {238169, 476},
			{1000000, 2000}, {1<<31 - 1, (1<<31 - 1) / 500},
		} {
			if got := Tolerance(c[0]); got != c[1] {
				t.Errorf("Tolerance(%d) = %d, want %d", c[0], got, c[1])
			}
		}
	})

	t.Run("exactly at the tolerance is caught up, with both numbers and no complete; one over is behind", func(t *testing.T) {
		r := one(100, 90)
		want(t, "missing 10 of 100", r, CaughtUp, NoReason)
		if r.Missing != 10 || r.Tolerance != 10 || r.Listed != 100 || r.OnDisk != 90 || r.MaySayComplete() {
			t.Fatalf("missing 10 of 100: %+v", r)
		}
		want(t, "missing 11 of 100", one(100, 89), Behind, NoReason)
		want(t, "missing 1 of 15", one(15, 14), CaughtUp, NoReason)
		want(t, "missing 2 of 15", one(15, 13), Behind, NoReason)
		want(t, "missing 476 of 238,169", one(238169, 238169-476), CaughtUp, NoReason)
		want(t, "missing 477 of 238,169", one(238169, 238169-477), Behind, NoReason)
	})

	t.Run("15 listed and nothing on disk is behind; nothing on disk is never caught up", func(t *testing.T) {
		want(t, "15 and 0", one(15, 0), Behind, NoReason)
		s := vstore(t, at("INBOX", folder(FlagInbox, 1, 1)))
		for y := int64(1); y <= 3000; y++ {
			s.Folders[0].TotalMsgs.Value, s.Folders[0].ServerTotal.Value = y, y
			if r := Assess(healthy(s, nil)); r.Verdict != Behind {
				t.Fatalf("listed %d, nothing on disk: %v", y, r.Verdict)
			}
		}
	})

	t.Run("once per account, never per folder", func(t *testing.T) {
		// Per folder, INBOX's 3 missing of 15 would be over its own tolerance of 1; the account's Y
		// is 200, whose tolerance is 20.
		s := vstore(t, at("INBOX", folder(FlagInbox, 15, 15)), at("Sent", folder(FlagSentMail, 185, 185)))
		r := Assess(healthy(s, map[string]int64{"INBOX": 12, "Sent": 185}))
		want(t, "3 missing of 200", r, CaughtUp, NoReason)
		if r.Listed != 200 || r.Tolerance != 20 || r.Missing != 3 {
			t.Fatalf("3 missing of 200: %+v", r)
		}
		// Ten folders of 1,000, 3 missing in each: within each folder's 20, but 30 against the
		// account's 20.
		var pairs []pair
		disk := map[string]int64{}
		for i := 0; i < 10; i++ {
			name := fmt.Sprintf("F%d", i)
			pairs = append(pairs, at(name, folder(0, 1000, 1000)))
			disk[name] = 997
		}
		r = Assess(healthy(vstore(t, pairs...), disk))
		want(t, "30 missing of 10,000", r, Behind, NoReason)
		if r.Listed != 10000 || r.Tolerance != 20 || r.Missing != 30 {
			t.Fatalf("30 missing of 10,000: %+v", r)
		}
	})

	t.Run("each folder clamped at 0 before the sum: +30 and -30 is behind", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 100, 100)), at("Sent", folder(FlagSentMail, 100, 100)))
		r := Assess(healthy(s, map[string]int64{"INBOX": 130, "Sent": 70}))
		want(t, "+30/-30", r, Behind, NoReason)
		if r.Missing != 30 {
			t.Fatalf("+30/-30: missing %d, want 30", r.Missing)
		}
	})
}
