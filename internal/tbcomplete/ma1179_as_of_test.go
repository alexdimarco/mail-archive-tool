package tbcomplete

import (
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1179
// The "as of" (§2.3's TC-12 and M9 folds) is the oldest lastSyncTimeInSec among the kept folders
// that list mail, not the newest (when none lists mail, among the kept folders); it is stale when
// more than 3 days old (state.SyncStaleAfter) or when any of them is later than now (the clock moved
// back). Stale leaves an older-than-3-days verdict as it is, but a stale account never says
// complete; a time in the future also fails caught_up's condition 7 ("none in the future"), so the
// account is not caught up. A not-kept folder's time is not read. Positive twin first.
func TestTheAsOfIsTheOldestCountTime(t *testing.T) {
	if StaleAfter != state.SyncStaleAfter {
		t.Fatalf("StaleAfter %v, state.SyncStaleAfter %v: the design's 3 days are the same", StaleAfter, state.SyncStaleAfter)
	}
	s := vstore(t,
		at("INBOX", folder(FlagInbox, 10, 10)),
		at("Sent", folder(FlagSentMail, 10, 10).with("lastSyncTimeInSec", vSync-1000)),
		at("Empty", folder(0, 0, 0).with("lastSyncTimeInSec", vSync-999999)),
		at("Trash", notKept(FlagTrash, 5).with("lastSyncTimeInSec", vSync-9999999)))
	disk := map[string]int64{"INBOX": 10, "Sent": 10}
	r := Assess(healthy(s, disk))
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if r.AsOf != vSync-1000 || r.Stale || len(r.DefaultNotKept) != 1 || !r.MaySayComplete() {
		t.Fatalf("the positive twin: as of %d (want %d), stale %v, %+v", r.AsOf, vSync-1000, r.Stale, r)
	}

	t.Run("more than 3 days old is stale; exactly 3 days is not", func(t *testing.T) {
		in := healthy(s, disk)
		in.Now = time.Unix(vSync-1000, 0).Add(StaleAfter)
		r := Assess(in)
		want(t, "exactly 3 days", r, CaughtUp, NoReason)
		if r.Stale || !r.MaySayComplete() {
			t.Fatalf("exactly 3 days: stale %v", r.Stale)
		}
		in.Now = in.Now.Add(time.Second)
		r = Assess(in)
		want(t, "3 days and a second", r, CaughtUp, NoReason)
		if !r.Stale || r.MaySayComplete() {
			t.Fatalf("3 days and a second: stale %v, may say complete %v", r.Stale, r.MaySayComplete())
		}
	})

	t.Run("a time later than now is stale, and not caught up", func(t *testing.T) {
		future := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Sent", folder(FlagSentMail, 10, 10).with("lastSyncTimeInSec", vNow.Unix()+1)))
		r := Assess(healthy(future, disk))
		if !r.Stale || r.Verdict == CaughtUp || r.MaySayComplete() {
			t.Fatalf("a time 1 s in the future: stale %v, %v", r.Stale, r.Verdict)
		}
		present := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)), at("Sent", folder(FlagSentMail, 10, 10).with("lastSyncTimeInSec", vNow.Unix())))
		if r := Assess(healthy(present, disk)); r.Stale || r.Verdict != CaughtUp {
			t.Fatalf("a time of exactly now: stale %v, %v", r.Stale, r.Verdict)
		}
	})

	t.Run("lists no mail: the oldest kept folder's time", func(t *testing.T) {
		none := vstore(t, at("INBOX", folder(FlagInbox, 0, 0)), at("Sent", folder(FlagSentMail, 0, 0).with("lastSyncTimeInSec", vSync-50)))
		r := Assess(healthy(none, nil))
		want(t, "lists no mail", r, CaughtUp, NoReason)
		if r.AsOf != vSync-50 {
			t.Fatalf("lists no mail: as of %d, want %d", r.AsOf, vSync-50)
		}
	})

	t.Run("Gmail: the oldest of the kept folders that list mail", func(t *testing.T) {
		g := gmailStore(t, 1000, 500, 300).withSync(t, "Label", vSync-7)
		r := Assess(gmail(g, 1000))
		if r.AsOf != vSync-7 {
			t.Fatalf("Gmail: as of %d, want %d", r.AsOf, vSync-7)
		}
	})
}
