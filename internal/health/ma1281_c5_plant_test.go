package health

import (
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1183, MA-1281
// PLANT-01 (the C5 adversarial pass, the lead's call): `status -json`'s behind_since never shows a
// date from a damaged record: a date more than a day after now, a date before 2020-01-01, or a
// first time after a last one makes the whole record untrusted, as a counter past its bound does.
// Positive twin first: a plausible record, read through Gather, says when the streak began.
func TestBehindSinceNeverShowsADamagedDate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	first := now.Add(-4 * 24 * time.Hour)
	lr := state.LastRun{Status: state.RunOK, Finished: now.Add(-time.Minute), Thunderbird: []state.TBAccount{
		{Account: "imap.b.example", StoreID: "b", Verdict: "behind", OnDisk: 3, Listed: 40}}}
	good := state.TBStreak{Version: 1, Serial: 1, Run: 9, LastRunAt: now.Add(-time.Minute), Accounts: []state.TBStreakAccount{
		{Store: "b", Serial: 1, FirstBehind: first, LastBehind: now.Add(-time.Minute), LastGrew: first, Runs: 5, BehindRun: 9, SeenRun: 9}}}
	out := t.TempDir()
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	since := func(s state.TBStreak) time.Time {
		t.Helper()
		if err := state.WriteTBStreak(out, s); err != nil {
			t.Fatal(err)
		}
		g := Gather(out, "")
		j := JSON(g, Assess(g, now))
		if len(j.ThunderbirdCompleteness) != 1 {
			t.Fatalf("entries: %+v", j.ThunderbirdCompleteness)
		}
		return j.ThunderbirdCompleteness[0].BehindSince
	}
	if got := since(good); !got.Equal(first) {
		t.Fatalf("positive twin: behind_since %v, want %v", got, first)
	}
	for name, plant := range map[string]func(*state.TBStreakAccount){
		"progress 3 days ahead": func(a *state.TBStreakAccount) { a.LastGrew = now.Add(3 * 24 * time.Hour) },
		"the latest backup 3 days ahead, in order": func(a *state.TBStreakAccount) {
			a.LastBehind = now.Add(3 * 24 * time.Hour)
		},
		"a first time before 2020": func(a *state.TBStreakAccount) {
			a.FirstBehind, a.LastGrew = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
		},
		"the first after the latest": func(a *state.TBStreakAccount) {
			a.LastBehind, a.LastGrew = first.Add(-time.Hour), time.Time{}
		},
	} {
		bad := good
		bad.Accounts = append([]state.TBStreakAccount(nil), good.Accounts...)
		plant(&bad.Accounts[0])
		if got := since(bad); !got.IsZero() {
			t.Errorf("%s: behind_since %v", name, got)
		}
	}
}
