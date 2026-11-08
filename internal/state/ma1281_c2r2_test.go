package state

// The C2 adversarial pass round 2's fixes in the records
// (docs/review-thunderbird-completeness-c2-adversarial-r2.md: R2-06, R2-07, R2-08). The round's
// attack tests are brought in from its clone, each with its positive twin first.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode"
)

// covers: MA-1283
// R2-07 (brought in): name cleaning removes every Default_Ignorable_Code_Point, not only Cf: the
// combining grapheme joiner, the Hangul fillers, U+17B4 and U+17B5, the variation selectors
// (Mongolian, U+FE00 to U+FE0F, U+E0100 to U+E01EF); a name of only Hangul fillers becomes "".
// Positive twin: the Cf characters, U+2028/2029 and the controls go, as before; visible letters,
// a combining accent and an ordinary space stay.
func TestR2_07_InvisibleNonCfCharacters(t *testing.T) {
	for _, r := range []rune{0x200B, 0x200D, 0x2060, 0xFEFF, 0x00AD, 0x180E, 0xFFF9, 0xE0001, 0x2028, 0x2029, 0x0085, 0x202E, 0x2066} {
		if got := CleanFolderName("a" + string(r) + "b"); got != "ab" {
			t.Fatalf("positive twin: U+%04X kept: %q", r, got)
		}
	}
	for _, r := range []rune{0x034F, 0x115F, 0x1160, 0x17B4, 0x17B5, 0x180B, 0x180C, 0x180D, 0x180F, 0x3164, 0xFE00, 0xFE0F, 0xFFA0, 0xE0100, 0xE01EF, 0x2065, 0xFFF0, 0xE0002, 0xE0FFF} {
		if got := CleanFolderName("a" + string(r) + "b"); got != "ab" {
			t.Errorf("U+%04X (%s) kept: %q", r, cat(r), got)
		}
	}
	if blank := CleanFolderName("ㅤㅤᅟ"); blank != "" {
		t.Errorf("a name of only Hangul fillers stays %q (looks empty)", blank)
	}
	for _, keep := range []string{"Inbox", "Boîte de réception", "é", "a b", "受信トレイ", "क्ष"} {
		if got := CleanFolderName(keep); got != keep {
			t.Errorf("%q cleaned to %q", keep, got)
		}
	}
	// Every Default_Ignorable_Code_Point of this Go's Unicode tables goes.
	n := 0
	for _, tab := range []*unicode.RangeTable{unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector} {
		for _, rg := range tab.R16 {
			for r := rune(rg.Lo); r <= rune(rg.Hi); r += rune(rg.Stride) {
				n++
				if CleanFolderName(string(r)) != "" {
					t.Errorf("U+%04X kept", r)
				}
			}
		}
		for _, rg := range tab.R32 {
			for r := rune(rg.Lo); r <= rune(rg.Hi); r += rune(rg.Stride) {
				n++
				if CleanFolderName(string(r)) != "" {
					t.Errorf("U+%04X kept", r)
				}
			}
		}
	}
	if n < 1000 {
		t.Errorf("only %d characters checked", n)
	}
}

func cat(r rune) string {
	for _, c := range []string{"Mn", "Lo", "Cn"} {
		if unicode.Is(unicode.Categories[c], r) {
			return c
		}
	}
	return "?"
}

// covers: MA-1281
// R2-08 (brought in): a full streak record never refuses a new behind store: with 64 entries the
// oldest goes to make room. Positive twin: with room, the new store's streak starts and nothing
// goes.
func TestR2_08_StaleEntriesFillTheStreakCap(t *testing.T) {
	now := time.Now()
	s := TBStreak{}
	for i := 0; i < TBAccountsMax-1; i++ {
		s = s.Behind(fmt.Sprintf("old%02d", i), "imap.old.example", 1, now.Add(-30*24*time.Hour))
	}
	room, _, placed := s.BehindEvicting("new", "imap.new.example", 1, now)
	if _, ok := room.Account("new"); !ok || placed != TBRoomKept || len(room.Accounts) != TBAccountsMax {
		t.Fatal("positive twin: with room, no streak")
	}
	s = s.Behind("old63", "imap.old.example", 1, now.Add(-30*24*time.Hour))
	if _, ok := s.Behind("new", "imap.new.example", 1, now).Account("new"); !ok {
		t.Errorf("64 stale entries: a new behind store gets no streak (its \"still downloading\" email can never come), silently")
	}
}

// covers: MA-1281
// R2-08's rules as round 3 amended them (R3-03 and R3-04, the lead's calls): each run's fold starts
// with StartRun (Run up by one, LastRunAt its time); a store the run reaches is Seen whatever its
// verdict; an entry whose store no run reached for TBStreakUnseenFor (30 days) is pruned and given
// back, one seen 30 days less a second ago is kept; a full record makes room for a new behind store
// by dropping the oldest entry this run did not find behind (the one reached longest ago), and
// never one it found behind; the counters are kept in the record, a planted one clamped (never
// ahead of the record's run, never negative, never wrapping). Positive twin: a streak seen every
// run is never pruned.
func TestTheStreakRecordPrunesAndMakesRoom(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s := TBStreak{}.StartRun(now).Behind("live", "imap.live.example", 1, now).Behind("gone", "imap.gone.example", 1, now)
	for h := 1; h <= 31*24; h++ {
		at := now.Add(time.Duration(h) * time.Hour)
		s = s.StartRun(at).Seen("live", at)
		var p []TBStreakAccount
		s, p = s.Prune(at)
		if at.Sub(now) < TBStreakUnseenFor && len(p) != 0 {
			t.Fatalf("hour %d: pruned %+v before 30 days", h, p)
		}
		if at.Sub(now) == TBStreakUnseenFor && (len(p) != 1 || p[0].Store != "gone") {
			t.Fatalf("hour %d: %+v, want the unseen entry pruned at 30 days", h, p)
		}
	}
	if _, ok := s.Account("live"); !ok || len(s.Accounts) != 1 || s.Run != 31*24+1 {
		t.Fatalf("positive twin: the entry seen every run: %+v", s)
	}
	// A second short of 30 days is kept.
	edge := TBStreak{}.StartRun(now).Behind("e", "imap.e.example", 1, now)
	if kept, p := edge.Prune(now.Add(TBStreakUnseenFor - time.Second)); len(p) != 0 || len(kept.Accounts) != 1 {
		t.Errorf("30 days less a second: pruned %+v", p)
	}
	// A streak extended every run is reached every run, Seen or not.
	ext := TBStreak{}.StartRun(now).Behind("ext", "imap.ext.example", 1, now)
	for i := 0; i < 100; i++ {
		at := now.Add(time.Duration(i+1) * 24 * time.Hour)
		ext, _ = ext.StartRun(at).Behind("ext", "imap.ext.example", int64(i), at).Prune(at)
	}
	if a, ok := ext.Account("ext"); !ok || a.Runs != 101 || a.SeenRun != ext.Run || a.BehindRun != ext.Run || !a.SeenAt.Equal(now.Add(100*24*time.Hour)) {
		t.Errorf("a streak extended every run: %+v %v", a, ok)
	}

	// A full record, this run: entries 0..253 not reached for a while (reached at runs 1..254),
	// 254 and 255 found behind in this run. The new store drops entry 0, the one reached longest
	// ago.
	full := TBStreak{}
	for i := 0; i < TBStreakAccountsMax; i++ {
		full = full.StartRun(now).Behind(fmt.Sprintf("s%03d", i), "imap.example", 1, now)
	}
	full = full.StartRun(now).Behind("s254", "imap.example", 2, now).Behind("s255", "imap.example", 2, now)
	got, gone, placed := full.BehindEvicting("new", "imap.new.example", 1, now)
	if placed != TBRoomEvicted || gone.Store != "s000" || len(got.Accounts) != TBStreakAccountsMax {
		t.Errorf("full: %v %q (%d entries); want s000 evicted", placed, gone.Store, len(got.Accounts))
	}
	if a, ok := got.Account("new"); !ok || a.SeenRun != got.Run || a.BehindRun != got.Run || a.Runs != 1 || !a.SeenAt.Equal(now) {
		t.Errorf("the new streak: %+v", a)
	}
	// Reaching another entry this run changes nothing; reaching s000 this run spares it, and the
	// next reached longest ago (s001) goes.
	reach := full.Seen("s005", now)
	if _, gone, _ := reach.BehindEvicting("new", "imap.new.example", 1, now); gone.Store != "s000" {
		t.Errorf("dropped %q, want s000", gone.Store)
	}
	reach = full.Seen("s000", now)
	if _, gone, _ := reach.BehindEvicting("new", "imap.new.example", 1, now); gone.Store != "s001" {
		t.Errorf("s000 reached this run: dropped %q, want s001", gone.Store)
	}
	// Every entry found behind this run: none goes, the record is unchanged, and the new store
	// gets no streak (TBRoomFull: the caller says so).
	all := TBStreak{}.StartRun(now)
	for i := 0; i < TBStreakAccountsMax; i++ {
		all = all.Behind(fmt.Sprintf("b%03d", i), "imap.example", 1, now)
	}
	all = all.Seen("b000", now)
	if after, _, placed := all.BehindEvicting("new", "imap.new.example", 1, now); placed != TBRoomFull || len(after.Accounts) != TBStreakAccountsMax || after.Serial != all.Serial {
		t.Errorf("all behind now: %v, %d entries, serial %d (was %d)", placed, len(after.Accounts), after.Serial, all.Serial)
	} else if _, ok := after.Account("new"); ok {
		t.Error("all behind now: the new store took a running streak's place")
	} else if _, ok := after.Account("b000"); !ok {
		t.Error("all behind now: b000 was evicted")
	}

	// The counters round-trip; a planted one is clamped.
	out := t.TempDir()
	if err := WriteTBStreak(out, got.Named()); err != nil {
		t.Fatal(err)
	}
	back, err := ReadTBStreak(out)
	if err != nil || back.Run != got.Run || back.Accounts[len(back.Accounts)-1].SeenRun != got.Run || !back.LastRunAt.Equal(now) || !back.Accounts[len(back.Accounts)-1].SeenAt.Equal(now) {
		t.Fatalf("round trip: %v run %d", err, back.Run)
	}
	planted := map[string]any{"version": 1, "serial": 3, "run": 5, "record_id": back.RecordID, "accounts": []map[string]any{
		{"store": "ahead", "account": "a", "seen_run": 1 << 50, "behind_run": -4, "runs": 1},
	}}
	data, _ := json.Marshal(planted)
	if err := os.WriteFile(filepath.Join(out, TBStreakName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := ReadTBStreak(out)
	if err != nil || p.Run != 5 || p.Accounts[0].SeenRun != 5 || p.Accounts[0].BehindRun != 0 {
		t.Errorf("planted: %v %+v", err, p)
	}
	planted["run"] = 1 << 62
	data, _ = json.Marshal(planted)
	_ = os.WriteFile(filepath.Join(out, TBStreakName), data, 0o600)
	if p, _ := ReadTBStreak(out); p.Run != maxTBStreakRun || p.StartRun(now).Run != maxTBStreakRun {
		t.Errorf("a planted huge run: %d", p.Run)
	}
	if p, _ := ReadTBStreak(out); func() bool { q, _ := p.StartRun(now).Prune(now); return len(q.Accounts) == 0 }() {
		t.Error("the planted entry, with no time of its own, was pruned at once")
	}
}

// covers: MA-1282
// R2-06: a record read back with more accounts than TBAccountsMax keeps 64 and adds the rest to
// thunderbird_more, as does a record that already says some were left out; a planted
// thunderbird_more is never negative and never wraps. Positive twin: 64 accounts, none left out.
func TestARecordSaysHowManyAccountsItLeftOut(t *testing.T) {
	out := t.TempDir()
	accts := func(n int) []TBAccount {
		var a []TBAccount
		for i := 0; i < n; i++ {
			a = append(a, TBAccount{Account: fmt.Sprintf("imap.%02d.example", i), Verdict: "caught_up", Complete: true})
		}
		return a
	}
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: accts(TBAccountsMax)}); err != nil {
		t.Fatal(err)
	}
	if r, _, err := ReadLastRun(out); err != nil || len(r.Thunderbird) != TBAccountsMax || r.ThunderbirdMore != 0 {
		t.Fatalf("positive twin: %v %d %d", err, len(r.Thunderbird), r.ThunderbirdMore)
	}
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: accts(TBAccountsMax + 3), ThunderbirdMore: 2}); err != nil {
		t.Fatal(err)
	}
	if r, _, err := ReadLastRun(out); err != nil || len(r.Thunderbird) != TBAccountsMax || r.ThunderbirdMore != 5 {
		t.Errorf("67 accounts and 2 left out: %v %d %d, want 64 and 5", err, len(r.Thunderbird), r.ThunderbirdMore)
	}
	for _, planted := range []int{-7, 1 << 62} {
		if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: accts(1), ThunderbirdMore: planted}); err != nil {
			t.Fatal(err)
		}
		if r, _, _ := ReadLastRun(out); r.ThunderbirdMore < 0 || r.ThunderbirdMore > 1<<20 {
			t.Errorf("planted %d: read %d", planted, r.ThunderbirdMore)
		}
	}
	// Round 2's new counts are never negative either.
	if err := WriteLastRun(out, LastRun{Status: RunOK, Thunderbird: []TBAccount{{Account: "a", GluedMailboxes: -3, NoStatus: -9}, {Account: "b", GluedMailboxes: 2, NoStatus: 7}}}); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := ReadLastRun(out); r.Thunderbird[0].GluedMailboxes != 0 || r.Thunderbird[0].NoStatus != 0 || r.Thunderbird[1].GluedMailboxes != 2 || r.Thunderbird[1].NoStatus != 7 {
		t.Errorf("planted counts: %+v", r.Thunderbird)
	}
}
