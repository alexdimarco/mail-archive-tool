package state

// The C2 adversarial pass round 3's fixes in the records
// (docs/review-thunderbird-completeness-c2-adversarial-r3.md: R3-03, R3-04, R3-05). The round's
// attack tests are brought in from its clone (.claude/attack-c2-r3, internal/state/zz_r3_test.go)
// with their assertions unchanged; only the fold helper follows the new StartRun, Seen and Prune,
// which now take the run's time. Each new rule has its positive twin first.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

// r3Run folds one run the way updateTBStreak does: StartRun, Seen for every store reached,
// existing streaks first, then new ones, then Prune.
func r3Run(s TBStreak, reached map[string]bool, behind []string, now time.Time) TBStreak {
	s, _ = r3RunRooms(s, reached, behind, now)
	return s
}

// r3RunRooms is r3Run, and the stores this run found no room for.
func r3RunRooms(s TBStreak, reached map[string]bool, behind []string, now time.Time) (TBStreak, []string) {
	s = s.StartRun(now)
	for st := range reached {
		s = s.Seen(st, now)
	}
	var full []string
	for _, existing := range []bool{true, false} {
		for _, st := range behind {
			if _, had := s.Account(st); had != existing {
				continue
			}
			var room TBRoom
			s, _, room = s.BehindEvicting(st, st, 10, now)
			if room == TBRoomFull {
				full = append(full, st)
			}
		}
	}
	s, _ = s.Prune(now)
	return s, full
}

// covers: MA-1281
// R3-03 (brought in): a store reached only by a weekly job, beside an hourly job on other inputs
// into the same archive, kept its streak only 72 runs, so it restarted every week and the 3-day
// "still downloading" notice never came. Pruned by time now (30 days), it keeps its streak.
func TestR3_03_WeeklyStoreBesideHourlyNeverKeepsItsStreak(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var s TBStreak
	for h := 0; h <= 2*168; h++ {
		now := t0.Add(time.Duration(h) * time.Hour)
		reached := map[string]bool{"hourly-A": true}
		var behind []string
		if h%168 == 0 {
			reached["weekly-B"] = true
			behind = []string{"weekly-B"}
		}
		s = r3Run(s, reached, behind, now)
	}
	b, ok := s.Account("weekly-B")
	if !ok || !b.FirstBehind.Equal(t0) {
		t.Errorf("weekly-B behind at every one of its 3 runs over 14 days: streak %v first behind %v runs %d (want first behind %v)", ok, b.FirstBehind, b.Runs, t0)
	}
}

// covers: MA-1281
// R3-04 (brought in): with 65 behind stores (more than the old 64), every hourly run evicted the
// first entry, so each streak lived 64 runs and none reached the notice. The safe outcome keeps at
// least TBAccountsMax-1 streaks running past 3 days.
func TestR3_04_SixtyFiveBehindStoresRotateUnderThreeDays(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	stores := make([]string, TBAccountsMax+1)
	reached := map[string]bool{}
	for i := range stores {
		stores[i] = fmt.Sprintf("store-%02d", i)
		reached[stores[i]] = true
	}
	var s TBStreak
	last := t0
	for h := 0; h < 200; h++ {
		last = t0.Add(time.Duration(h) * time.Hour)
		s = r3Run(s, reached, stores, last)
	}
	old := 0
	for _, a := range s.Accounts {
		if last.Sub(a.FirstBehind) >= 72*time.Hour {
			old++
		}
	}
	if old < TBAccountsMax-1 {
		t.Errorf("65 stores behind every hourly run for 200 runs: %d streak(s) older than 3 days (record holds %d)", old, len(s.Accounts))
	}
}

// covers: MA-1283
// R3-05 (brought in): characters outside Default_Ignorable_Code_Point that still render as
// nothing or as blank: the Braille blank U+2800 (So), and the space characters U+3000, U+2003,
// U+205F, U+00A0. "Inbox" and "Inbox" + U+2800 must look the same and be the same.
func TestR3_05_BlankRenderingCharactersKept(t *testing.T) {
	for _, r := range []rune{0x2800, 0x3000, 0x2003, 0x205F, 0x00A0} {
		if got := CleanFolderName("Inbox" + string(r)); got != "Inbox" {
			t.Errorf("U+%04X kept: %q", r, got)
		}
	}
}

// covers: MA-1283
// R3-06 (brought in, held): every bidi control and the other invisible format characters go.
func TestR3_06_BidiControlsGone(t *testing.T) {
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x200E, 0x200F, 0x061C, 0x200B, 0x2060, 0xFEFF, 0x180E, 0x2061, 0xFFF0, 0xE0001, 0xE0041, 0x1D173, 0x13430, 0x1BCA0} {
		if got := CleanFolderName("a" + string(r) + "b"); got != "ab" {
			t.Errorf("U+%04X kept: %q", r, got)
		}
	}
}

// covers: MA-1283
// R3-05, the rule (the lead's call): U+2800 is removed; every space separator (category Zs) other
// than U+0020 becomes U+0020; runs of spaces become one; the ends are trimmed; a name of only such
// characters is ""; the cap leaves no space before its "…". Positive twin: ordinary names, one
// ordinary space between words, and letters of other scripts stay as they are.
func TestBlankCharactersAreFoldedAndTrimmed(t *testing.T) {
	for _, keep := range []string{"Inbox", "[Gmail]/Sent Mail", "a b", "受信トレイ", "Boîte de réception", "😀"} {
		if got := CleanFolderName(keep); got != keep {
			t.Fatalf("positive twin: CleanFolderName(%q) = %q", keep, got)
		}
	}
	for in, want := range map[string]string{
		"Inbox⠀":    "Inbox",
		"⠀Inbox":    "Inbox",
		"In⠀box":    "Inbox",
		"Sent Mail": "Sent Mail",
		"受信　トレイ":    "受信 トレイ",
		"a  　  b":   "a b",
		"a  b":      "a b",
		"  Inbox　 ": "Inbox",
		"⠀　 ⠀":      "",
		"⠀ㅤ​ 　":     "",
	} {
		if got := CleanFolderName(in); got != want {
			t.Errorf("CleanFolderName(%+q) = %+q, want %+q", in, got, want)
		}
	}
	// Every Zs character of this Go's Unicode tables becomes one ordinary space between words.
	n := 0
	for _, rg := range unicode.Zs.R16 {
		for r := rune(rg.Lo); r <= rune(rg.Hi); r += rune(rg.Stride) {
			n++
			if got := CleanFolderName("a" + string(r) + "b"); got != "a b" {
				t.Errorf("U+%04X: %+q, want \"a b\"", r, got)
			}
		}
	}
	if n < 17 {
		t.Errorf("only %d space separators checked", n)
	}
	// The cap counts what shows, and leaves no space before "…".
	long := strings.Repeat("x", TBNameMax-1) + " " + strings.Repeat("y", 10)
	if got := CleanFolderName(long); got != strings.Repeat("x", TBNameMax-1)+"…" {
		t.Errorf("a space at the cap: %q", got)
	}
	spaced := strings.Repeat("　", 500) + strings.Repeat("z", TBNameMax)
	if got := CleanFolderName(spaced); got != strings.Repeat("z", TBNameMax) {
		t.Errorf("leading blanks counted toward the cap: %q", got)
	}
}

// covers: MA-1281
// R3-03, the rule (the lead's call): an entry is pruned only when no run has reached its store for
// 30 days, by the record's own times, never by how many runs passed. Positive twin first: 1,000
// hourly runs that never reach a store reached by a run 29 days ago keep it. Then: 30 days unseen
// prunes it; a clock that went back never prunes (an entry seen "later" than now is kept, at any
// distance); a run whose clock is before MinPlausibleTime prunes nothing; an entry with no time
// starts its 30 days at the next run; a planted time is never trusted past the record's own latest
// run time, and with none it is none.
func TestTheStreakIsPrunedByTimeNotByRuns(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	s := TBStreak{}.StartRun(t0).Behind("b", "imap.b.example", 1, t0)
	for i := 1; i <= 1000; i++ {
		at := t0.Add(29*24*time.Hour + time.Duration(i)*time.Second)
		var gone []TBStreakAccount
		if s, gone = s.StartRun(at).Seen("other", at).Prune(at); len(gone) != 0 {
			t.Fatalf("positive twin: run %d pruned %+v 29 days after it was reached", i, gone)
		}
	}
	if _, ok := s.Account("b"); !ok || s.Run != 1001 {
		t.Fatalf("positive twin: %+v", s)
	}
	if _, gone := s.StartRun(t0.Add(TBStreakUnseenFor)).Prune(t0.Add(TBStreakUnseenFor)); len(gone) != 1 || gone[0].Store != "b" {
		t.Errorf("30 days unseen: pruned %+v", gone)
	}

	// The clock went back: by a year, then forward again to 29 days past the backward time.
	future := t0.Add(365 * 24 * time.Hour)
	f := TBStreak{}.StartRun(future).Behind("f", "imap.f.example", 1, future)
	for _, at := range []time.Time{t0, t0.Add(29 * 24 * time.Hour), future.Add(-time.Hour)} {
		if kept, gone := f.StartRun(at).Prune(at); len(gone) != 0 || len(kept.Accounts) != 1 {
			t.Errorf("clock at %v, entry seen at %v: pruned %+v", at, future, gone)
		}
	}
	// A clock before MinPlausibleTime prunes nothing, even an entry seen long ago.
	old := TBStreak{}.StartRun(t0).Behind("o", "imap.o.example", 1, t0)
	if _, gone := old.StartRun(time.Unix(0, 0)).Prune(time.Unix(0, 0)); len(gone) != 0 {
		t.Errorf("a 1970 clock pruned %+v", gone)
	}
	// An entry made under an implausible clock has no time; the next plausible run starts its 30
	// days, so it is neither kept for ever nor pruned at once.
	nt := TBStreak{}.StartRun(time.Unix(0, 0)).Behind("n", "imap.n.example", 1, time.Unix(0, 0))
	if a, _ := nt.Account("n"); !a.SeenAt.IsZero() {
		t.Fatalf("an implausible clock gave a time: %v", a.SeenAt)
	}
	// Another implausible run gives it no time either (else a 1970 time would be pruned at once).
	nt, gone := nt.StartRun(time.Unix(0, 0)).Prune(time.Unix(0, 0))
	if a, _ := nt.Account("n"); len(gone) != 0 || !a.SeenAt.IsZero() {
		t.Fatalf("an implausible run: pruned %+v, time %v", gone, a.SeenAt)
	}
	nt, gone = nt.StartRun(t0).Prune(t0)
	if a, ok := nt.Account("n"); len(gone) != 0 || !ok || !a.SeenAt.Equal(t0) {
		t.Errorf("no time: %+v %v, pruned %+v", a, ok, gone)
	}
	if _, gone := nt.StartRun(t0.Add(TBStreakUnseenFor)).Prune(t0.Add(TBStreakUnseenFor)); len(gone) != 1 {
		t.Errorf("no time, then 30 days: pruned %+v", gone)
	}

	// Planted times: a seen time after the record's last run is clamped to it; with no last run
	// time, or an implausible one, it is none.
	out := t.TempDir()
	write := func(rec map[string]any) TBStreak {
		t.Helper()
		data, _ := json.Marshal(rec)
		if err := os.WriteFile(filepath.Join(out, TBStreakName), data, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadTBStreak(out)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	acct := []map[string]any{{"store": "p", "account": "a", "runs": 1, "seen_run": 3, "seen_at": t0.Add(10 * 365 * 24 * time.Hour)}}
	p := write(map[string]any{"version": 1, "run": 3, "last_run_at": t0, "accounts": acct})
	if !p.LastRunAt.Equal(t0) || !p.Accounts[0].SeenAt.Equal(t0) {
		t.Errorf("a planted future seen time: last run %v, seen %v (want both %v)", p.LastRunAt, p.Accounts[0].SeenAt, t0)
	}
	if _, gone := p.StartRun(t0.Add(TBStreakUnseenFor)).Prune(t0.Add(TBStreakUnseenFor)); len(gone) != 1 {
		t.Errorf("the clamped entry, 30 days on: pruned %+v", gone)
	}
	for _, last := range []any{nil, time.Unix(0, 0)} {
		rec := map[string]any{"version": 1, "run": 3, "accounts": acct}
		if last != nil {
			rec["last_run_at"] = last
		}
		if q := write(rec); !q.LastRunAt.IsZero() || !q.Accounts[0].SeenAt.IsZero() {
			t.Errorf("last run %v: read %v and seen %v, want none", last, q.LastRunAt, q.Accounts[0].SeenAt)
		}
	}
}

// covers: MA-1281
// R3-04, the rule (the lead's call): the record keeps 256 stores' streaks, and a full record never
// evicts an entry this run found behind. Positive twin first: the 65 stores of R3-04, behind every
// hourly run for 200 runs, all keep their streaks from the first run. Then 257 stores behind every
// run: the 256 that got a streak keep it from the first run to the last (each reaches 3 days), and
// the 257th finds no room at every run (TBRoomFull: the caller logs it and leaves the
// tbstreak_full WARN), never taking a running streak's place.
func TestAFullRecordNeverEvictsAStreakFoundBehind(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	hourly := func(n int) (TBStreak, int, time.Time) {
		stores := make([]string, n)
		reached := map[string]bool{}
		for i := range stores {
			stores[i] = fmt.Sprintf("store-%03d", i)
			reached[stores[i]] = true
		}
		var s TBStreak
		fullRuns := 0
		last := t0
		for h := 0; h < 200; h++ {
			last = t0.Add(time.Duration(h) * time.Hour)
			var full []string
			s, full = r3RunRooms(s, reached, stores, last)
			if len(full) > 0 {
				if len(full) != 1 || full[0] != stores[n-1] {
					t.Fatalf("run %d: no room for %v", h, full)
				}
				fullRuns++
			}
		}
		return s, fullRuns, last
	}
	s, fullRuns, _ := hourly(TBAccountsMax + 1)
	if len(s.Accounts) != TBAccountsMax+1 || fullRuns != 0 {
		t.Fatalf("positive twin: 65 stores, %d streaks, %d runs with no room", len(s.Accounts), fullRuns)
	}
	for _, a := range s.Accounts {
		if !a.FirstBehind.Equal(t0) || a.Runs != 200 {
			t.Errorf("positive twin: %s first behind %v runs %d (want %v, 200)", a.Store, a.FirstBehind, a.Runs, t0)
		}
	}

	s, fullRuns, last := hourly(TBStreakAccountsMax + 1)
	if len(s.Accounts) != TBStreakAccountsMax || fullRuns != 200 {
		t.Errorf("257 stores: %d streaks, %d of 200 runs with no room", len(s.Accounts), fullRuns)
	}
	for _, a := range s.Accounts {
		if !a.FirstBehind.Equal(t0) || a.Runs != 200 || last.Sub(a.FirstBehind) < 72*time.Hour {
			t.Errorf("257 stores: %s first behind %v runs %d", a.Store, a.FirstBehind, a.Runs)
		}
	}
	if _, ok := s.Account(fmt.Sprintf("store-%03d", TBStreakAccountsMax)); ok {
		t.Error("the 257th store took a running streak's place")
	}
}

// covers: MA-1279
// The last-run record keeps misaligned_mailboxes (R3-01), never negative when read back.
// Positive twin: a real count round-trips.
func TestTheRecordKeepsMisalignedMailboxes(t *testing.T) {
	out := t.TempDir()
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: []TBAccount{{Account: "b", MisalignedMailboxes: 2}}}); err != nil {
		t.Fatal(err)
	}
	if r, _, err := ReadLastRun(out); err != nil || r.Thunderbird[0].MisalignedMailboxes != 2 {
		t.Fatalf("positive twin: %v %+v", err, r.Thunderbird)
	}
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: []TBAccount{{Account: "a", MisalignedMailboxes: -3}}}); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := ReadLastRun(out); r.Thunderbird[0].MisalignedMailboxes != 0 {
		t.Errorf("a planted -3: %d", r.Thunderbird[0].MisalignedMailboxes)
	}
}

// covers: MA-1281
// The record at its new cap fits its reader's bound: 256 entries with the worst names (80
// characters of "<", 6 bytes each in JSON, then "…"), long store keys and the largest counters
// and times are written and read back whole; a record with more entries than the cap keeps 256.
// Positive twin: one entry round-trips.
func TestTheStreakRecordAtItsCapFitsItsBound(t *testing.T) {
	out := t.TempDir()
	now := time.Date(2026, 11, 8, 12, 0, 0, 123456789, time.FixedZone("x", -9*3600-30*60))
	one := TBStreak{}.StartRun(now).Behind("k", "imap.k.example", 1, now).Named()
	if err := WriteTBStreak(out, one); err != nil {
		t.Fatal(err)
	}
	if back, err := ReadTBStreak(out); err != nil || len(back.Accounts) != 1 {
		t.Fatalf("positive twin: %v %+v", err, back)
	}
	s := TBStreak{Serial: 1 << 40, Run: maxTBStreakRun, LastRunAt: now}
	for i := 0; i < TBStreakAccountsMax+20; i++ {
		s.Accounts = append(s.Accounts, TBStreakAccount{
			Store: strings.Repeat("f", 64) + fmt.Sprintf("%04d", i), Account: strings.Repeat("<", TBNameMax) + "…",
			Serial: 1 << 40, FirstBehind: now, FirstX: 1 << 62, LastBehind: now, LastGrew: now, GrewX: 1 << 62, Runs: 1 << 40,
			StillSent: true, StoppedSent: true, SeenRun: maxTBStreakRun, SeenAt: now, BehindRun: maxTBStreakRun,
		})
	}
	data, _ := json.Marshal(TBStreak{Version: TBStreakVersion, Serial: s.Serial, Run: s.Run, LastRunAt: now, RecordID: one.RecordID, Accounts: s.Accounts[:TBStreakAccountsMax]})
	if len(data) > maxTBStreakBytes {
		t.Fatalf("the worst record at the cap is %d bytes, over the bound %d", len(data), maxTBStreakBytes)
	}
	t.Logf("the worst record at the cap: %d bytes of %d", len(data), maxTBStreakBytes)
	if err := os.WriteFile(filepath.Join(out, TBStreakName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if back, err := ReadTBStreak(out); err != nil || len(back.Accounts) != TBStreakAccountsMax {
		t.Errorf("the worst record at the cap read back: %v, %d entries", err, len(back.Accounts))
	}
	over := s
	over.Accounts = s.Accounts[:TBStreakAccountsMax+3]
	over.Accounts = append([]TBStreakAccount(nil), over.Accounts...)
	for i := range over.Accounts {
		over.Accounts[i].Account = "a"
	}
	if err := WriteTBStreak(out, over.Named()); err != nil {
		t.Fatal(err)
	}
	if back, err := ReadTBStreak(out); err != nil || len(back.Accounts) != TBStreakAccountsMax {
		t.Errorf("259 entries read back: %v, %d (want %d)", err, len(back.Accounts), TBStreakAccountsMax)
	}
}
