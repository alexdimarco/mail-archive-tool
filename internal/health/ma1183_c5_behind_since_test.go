package health

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1183
// `status -json`'s behind_since (§7's -json row; slice C5): the first behind backup of an account's
// current streak, from the archive's behind-streak record, only for an account the last backup
// found behind and whose streak that record's latest fold extended. Never for another verdict,
// never a time after the record's run or before MinPlausibleTime, and never from a record no run
// could have written (R4-01). Additive: an entry without it has no behind_since key. Positive twin
// first: a behind account says when its streak began.
func TestBehindSinceComesFromTheStreak(t *testing.T) {
	end := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	first := end.Add(-4 * 24 * time.Hour)
	lr := state.LastRun{Status: state.RunOK, Finished: end, Thunderbird: []state.TBAccount{
		{Account: "imap.b.example", StoreID: "b", Verdict: "behind", OnDisk: 3, Listed: 40},
		{Account: "imap.u.example", StoreID: "u", Verdict: "unknown"},
		{Account: "imap.o.example", StoreID: "o", Verdict: "behind", OnDisk: 3, Listed: 40},
		{Account: "imap.f.example", StoreID: "f", Verdict: "behind", OnDisk: 3, Listed: 40},
		{Account: "imap.e.example", StoreID: "e", Verdict: "behind", OnDisk: 3, Listed: 40},
	}}
	s := state.TBStreak{Version: 1, Serial: 5, Run: 9, Accounts: []state.TBStreakAccount{
		{Store: "b", Serial: 1, FirstBehind: first, Runs: 5, BehindRun: 9},
		{Store: "u", Serial: 2, FirstBehind: first, Runs: 5, BehindRun: 9},                                  // planted: unknown at the last run
		{Store: "o", Serial: 3, FirstBehind: first, Runs: 5, BehindRun: 8},                                  // not extended by the latest fold
		{Store: "f", Serial: 4, FirstBehind: end.Add(time.Hour), Runs: 5, BehindRun: 9},                     // after the run
		{Store: "e", Serial: 5, FirstBehind: state.MinPlausibleTime.Add(-time.Hour), Runs: 5, BehindRun: 9}, // implausible
	}}
	in := Input{Out: "/a", LastRun: lr, LastRunState: state.LastRunPresent, TBStreak: s}
	j := JSON(in, Assess(in, end))
	if len(j.ThunderbirdCompleteness) != 5 || !j.ThunderbirdCompleteness[0].BehindSince.Equal(first) {
		t.Fatalf("positive twin: %+v", j.ThunderbirdCompleteness)
	}
	for _, e := range j.ThunderbirdCompleteness[1:] {
		if !e.BehindSince.IsZero() {
			t.Errorf("%s says behind_since %v", e.Account, e.BehindSince)
		}
	}
	data, err := json.Marshal(j.ThunderbirdCompleteness)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"behind_since"`) != 1 || !strings.Contains(string(data), `"account":"imap.u.example"`) {
		t.Errorf("the -json entries: %s", data)
	}

	// From disk through Gather: the same record is read; an implausible one is not.
	out := t.TempDir()
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	g := Gather(out, "")
	if j := JSON(g, Assess(g, end)); !j.ThunderbirdCompleteness[0].BehindSince.Equal(first) {
		t.Fatalf("through Gather: %+v", j.ThunderbirdCompleteness[0])
	}
	bad := s
	bad.Run = 1 << 40
	bad.Accounts = append([]state.TBStreakAccount(nil), s.Accounts...)
	bad.Accounts[0].BehindRun = 1 << 40
	if err := state.WriteTBStreak(out, bad); err != nil {
		t.Fatal(err)
	}
	g = Gather(out, "")
	if j := JSON(g, Assess(g, end)); !j.ThunderbirdCompleteness[0].BehindSince.IsZero() {
		t.Errorf("an implausible record dated it: %+v", j.ThunderbirdCompleteness[0])
	}
}
