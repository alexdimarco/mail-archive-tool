package tbcomplete

import (
	"testing"
)

// covers: MA-1177
// limited (§4.1 step 4 with its TC-9 fold, §4.2's fold): an account that is behind while one of
// Thunderbird's own limits is on (autosync_max_age_days > 0, the message size limit, or
// autosync_offline_stores false) is limited, naming the limits in the result; with
// autosync_offline_stores false it is limited even when never checked, decided before the
// never-checked rule. A limit changes nothing for a caught-up account, and limited never says
// complete. Positive twin first: behind with no limit is behind.
func TestALimitMakesBehindLimited(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 100, 100)))
	behindIn := func(l Limits) Input {
		in := healthy(s, map[string]int64{"INBOX": 50})
		in.Limits = l
		return in
	}
	want(t, "the positive twin", Assess(behindIn(Limits{})), Behind, NoReason)
	for name, l := range map[string]Limits{
		"recent mail only":         {MaxAgeDays: 30},
		"the size limit":           {SizeLimit: true, MaxSizeKB: 50},
		"no background download":   {NoBackgroundDownload: true},
		"the size limit with 0 KB": {SizeLimit: true},
	} {
		r := Assess(behindIn(l))
		want(t, name, r, Limited, NoReason)
		if r.Limits != l || r.MaySayComplete() || r.Missing != 50 {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	if r := Assess(behindIn(Limits{MaxAgeDays: -1})); r.Verdict != Behind {
		t.Fatalf("a negative age is no limit: %v", r.Verdict)
	}

	t.Run("a caught-up account keeps caught up", func(t *testing.T) {
		in := healthy(s, map[string]int64{"INBOX": 100})
		in.Limits = Limits{MaxAgeDays: 30, SizeLimit: true, NoBackgroundDownload: true}
		r := Assess(in)
		want(t, "caught up with every limit", r, CaughtUp, NoReason)
		if !r.MaySayComplete() {
			t.Fatalf("caught up with a limit, missing 0: may say complete %v", r.MaySayComplete())
		}
	})

	t.Run("no background download is limited even when never checked (TC-9)", func(t *testing.T) {
		never := vstore(t, at("INBOX", folder(FlagInbox, 100, 100).with("lastSyncTimeInSec", nil)))
		in := healthy(never, map[string]int64{"INBOX": 100})
		want(t, "never checked, no limit", Assess(in), Unknown, ReasonNeverChecked)
		in.Limits = Limits{NoBackgroundDownload: true}
		want(t, "never checked, no background download", Assess(in), Limited, NoReason)
		in.Limits = Limits{MaxAgeDays: 30, SizeLimit: true}
		want(t, "never checked, the other limits", Assess(in), Unknown, ReasonNeverChecked)
	})

	t.Run("Gmail", func(t *testing.T) {
		g := gmail(gmailStore(t, 1000, 500, 300), 500)
		g.Limits = Limits{MaxAgeDays: 30}
		want(t, "behind on All Mail with an age limit", Assess(g), Limited, NoReason)
		hidden := gmail(vstore(t, at("INBOX", folder(FlagInbox, 500, 500))), 100)
		hidden.Limits = Limits{SizeLimit: true}
		r := Assess(hidden)
		want(t, "behind on a floor with the size limit", r, Limited, NoReason)
		if !r.Floor {
			t.Fatalf("a floor's limited lost its floor: %+v", r)
		}
		unchecked := gmail(gmailStore(t, 1000, 500, 300).withSync(t, "[Gmail]/All Mail", 0), 1000)
		want(t, "All Mail never checked", Assess(unchecked), Unknown, ReasonGmailFloor)
		unchecked.Limits = Limits{NoBackgroundDownload: true}
		want(t, "All Mail never checked, no background download", Assess(unchecked), Limited, NoReason)
		shown := gmail(vstore(t, at("INBOX", folder(FlagInbox, 500, 500))), 500)
		shown.Limits = Limits{NoBackgroundDownload: true}
		want(t, "All Mail hidden, no background download, X at the floor", Assess(shown), Unknown, ReasonGmailFloor)
	})
}
