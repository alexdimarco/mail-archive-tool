package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-908, R32
// The sync streak's rules, pure (design-presync-attended §5.1-§5.3; C-4, C-35): misses
// spanning 3 days with at least two of them are stale, measured between runs and never
// to now, so a computer that was off trips nothing; Serial goes up only when a miss
// follows a reset; a synced session resets the missed streak and the busy count and
// keeps Serial; three profile-busy results in a row count one miss; a miss resets the
// busy count; an implausible time counts the miss without moving a time; a clock that
// went back restarts FirstMissed and keeps Serial. Positive twin first: a miss every 12
// hours for 3 days is stale.
func TestTheSyncStreakSpansRunsNeverNow(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	s := SyncStreak{}
	for h := 0; h <= 72; h += 12 {
		s = s.Miss("owner-active-skipped", t0.Add(time.Duration(h)*time.Hour))
	}
	if !s.Stale() || s.Missed != 7 || s.Serial != 1 || !s.FirstMissed.Equal(t0) || s.LastName != "owner-active-skipped" {
		t.Fatalf("the positive twin: a miss every 12 hours for 3 days: %+v, stale %v", s, s.Stale())
	}

	// One miss alone is never stale, however old; two misses an hour apart are not
	// stale, however long ago they were: the span is between runs, never to now.
	if one := (SyncStreak{}).Miss("x", t0.Add(-400*24*time.Hour)); one.Stale() {
		t.Error("one old miss is stale")
	}
	if one := (SyncStreak{Missed: 1, FirstMissed: t0, LastMissed: t0.Add(4 * 24 * time.Hour)}); one.Stale() {
		t.Error("a record of one miss whose times span 4 days is stale: at least two are needed")
	}
	off := SyncStreak{}.Miss("x", t0).Miss("x", t0.Add(time.Hour))
	if off.Stale() {
		t.Error("two misses an hour apart, the computer off since, read as stale: the span was compared with now")
	}
	if just := (SyncStreak{}).Miss("x", t0).Miss("x", t0.Add(SyncStaleAfter-time.Second)); just.Stale() {
		t.Error("a span just under 3 days is stale")
	}

	// A synced session resets the streak and the busy count and keeps Serial; the next
	// miss starts a new streak (Serial 2).
	r := s.ProfileBusy("profile-busy", t0.Add(80*time.Hour)).Synced(t0.Add(81 * time.Hour))
	if r.Missed != 0 || r.Busy != 0 || !r.FirstMissed.IsZero() || !r.LastMissed.IsZero() || r.Serial != 1 || !r.LastSynced.Equal(t0.Add(81*time.Hour)) || r.Stale() {
		t.Fatalf("after a sync: %+v", r)
	}
	r = r.Miss("leftover-found", t0.Add(90*time.Hour))
	if r.Serial != 2 || r.Missed != 1 || !r.FirstMissed.Equal(t0.Add(90*time.Hour)) {
		t.Fatalf("a miss after a reset: %+v, want Serial 2 and a new first miss", r)
	}
	if r = r.Miss("x", t0.Add(91*time.Hour)); r.Serial != 2 {
		t.Errorf("a second miss in the same streak moved Serial to %d", r.Serial)
	}

	// profile-busy: two in a row move nothing but Busy; the third counts one miss and
	// starts the count again; a miss in between resets the count (C-4, C-86d).
	b := SyncStreak{}.ProfileBusy("profile-busy", t0).ProfileBusy("profile-busy", t0.Add(time.Hour))
	if b.Missed != 0 || b.Busy != 2 || b.Serial != 0 {
		t.Fatalf("two profile-busy: %+v", b)
	}
	if b3 := b.ProfileBusy("profile-busy", t0.Add(2*time.Hour)); b3.Missed != 1 || b3.Busy != 0 || b3.Serial != 1 || b3.LastName != "profile-busy" {
		t.Fatalf("the third profile-busy: %+v, want one miss and the count reset", b3)
	}
	if m := b.Miss("owner-active-skipped", t0.Add(2*time.Hour)); m.Busy != 0 || m.Missed != 1 {
		t.Errorf("a miss after two profile-busy: %+v, want the busy count reset", m)
	}

	// A clock that was not set: the miss counts, no time is kept.
	u := SyncStreak{}.Miss("x", time.Unix(0, 0).UTC())
	if u.Missed != 1 || !u.FirstMissed.IsZero() || !u.LastMissed.IsZero() || u.Serial != 1 {
		t.Fatalf("an implausible time: %+v", u)
	}
	if u = u.Synced(time.Unix(0, 0).UTC()); !u.LastSynced.IsZero() {
		t.Errorf("an implausible sync time was kept: %v", u.LastSynced)
	}

	// A clock that went back restarts FirstMissed, never runs the span negative, and
	// keeps Serial, so it never starts a new email "streak" (C-35).
	ahead := SyncStreak{}.Miss("x", t0.Add(200*24*time.Hour))
	back := ahead.Miss("x", t0)
	if !back.FirstMissed.Equal(t0) || back.Serial != ahead.Serial || back.Missed != 2 || back.Stale() {
		t.Fatalf("after the clock went back: %+v", back)
	}
	for h := 12; h <= 72; h += 12 {
		back = back.Miss("x", t0.Add(time.Duration(h)*time.Hour))
	}
	if !back.Stale() || back.Serial != 1 {
		t.Errorf("3 days after the clock was put right: stale %v, serial %d", back.Stale(), back.Serial)
	}

	// A record written while a clock was wrong spans nothing.
	for _, w := range []SyncStreak{
		{Missed: 5, FirstMissed: time.Unix(0, 0).UTC(), LastMissed: t0},
		{Missed: 5, FirstMissed: t0.Add(60 * 24 * time.Hour), LastMissed: t0},
	} {
		if w.Stale() {
			t.Errorf("a record with an impossible span is stale: %+v", w)
		}
	}
}

// covers: MA-926, R32
// The no-change count (owner ruling OQ-5, §5.4): two or more scheduled syncs that
// fetched nothing, spanning 3 days, never measured to now, trip the softer warning; a
// session in which mail arrived resets it; MA-891's clock rules hold. Positive twin
// first.
func TestTheNoChangeCountSpansRunsNeverNow(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	s := SyncStreak{}
	for d := 0; d <= 3; d++ {
		s = s.NoChangeSeen(t0.Add(time.Duration(d) * 24 * time.Hour))
	}
	if !s.NothingArrived() || s.NoChange != 4 || !s.FirstNoChange.Equal(t0) {
		t.Fatalf("the positive twin: four nightly no-change syncs: %+v", s)
	}
	if s.Missed != 0 || s.Serial != 0 || s.Stale() {
		t.Errorf("the no-change count moved the missed streak: %+v", s)
	}
	if r := s.MailArrived(); r.NoChange != 0 || !r.FirstNoChange.IsZero() || !r.LastNoChange.IsZero() || r.NothingArrived() {
		t.Errorf("mail arrived, yet: %+v", r)
	}
	if off := (SyncStreak{}).NoChangeSeen(t0).NoChangeSeen(t0.Add(time.Hour)); off.NothingArrived() {
		t.Error("two no-change syncs an hour apart read as 3 days: compared with now")
	}
	if one := (SyncStreak{}).NoChangeSeen(t0.Add(-300 * 24 * time.Hour)); one.NothingArrived() {
		t.Error("one no-change sync alone trips the warning")
	}
	if one := (SyncStreak{NoChange: 1, FirstNoChange: t0, LastNoChange: t0.Add(4 * 24 * time.Hour)}); one.NothingArrived() {
		t.Error("a record of one no-change sync whose times span 4 days trips the warning: at least two are needed")
	}
	u := SyncStreak{}.NoChangeSeen(time.Unix(0, 0).UTC())
	if u.NoChange != 1 || !u.FirstNoChange.IsZero() {
		t.Errorf("an implausible time: %+v", u)
	}
	back := SyncStreak{}.NoChangeSeen(t0.Add(100 * 24 * time.Hour)).NoChangeSeen(t0)
	if !back.FirstNoChange.Equal(t0) || back.NothingArrived() {
		t.Errorf("after the clock went back: %+v", back)
	}
}

// covers: MA-908, R32
// The file is the archive's own, read securely (§5.1, C-37): written owner-only and
// atomically, read back the same; a file readable by others, a link and one over 4 KiB
// are refused with an error (never read as a fresh streak); a malformed one reads as
// zero; the kept name is control-stripped on read; removing it is idempotent.
func TestTheSyncStreakFileIsReadSecurely(t *testing.T) {
	out := t.TempDir()
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	want := SyncStreak{}.Miss("leftover-found", t0).Miss("leftover-found", t0.Add(time.Hour)).NoChangeSeen(t0)
	if err := WriteSyncStreak(out, want); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(out, SyncStreakName)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(p)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("the file's mode is %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}
	got, err := ReadSyncStreak(out)
	if err != nil || got.Missed != 2 || got.Serial != 1 || got.NoChange != 1 || got.LastName != "leftover-found" || !got.FirstMissed.Equal(t0) {
		t.Fatalf("read back %+v (%v), want %+v", got, err, want)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSyncStreak(out); err == nil {
			t.Error("a sync streak readable by others was read")
		}
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
		link := t.TempDir()
		if err := os.Symlink(p, filepath.Join(link, SyncStreakName)); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSyncStreak(link); err == nil {
			t.Error("a sync streak that is a link was read")
		}
	}
	big := t.TempDir()
	if err := os.WriteFile(filepath.Join(big, SyncStreakName), bytes.Repeat([]byte(" "), 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSyncStreak(big); err == nil {
		t.Error("a sync streak over 4 KiB was read")
	}
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, SyncStreakName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if z, err := ReadSyncStreak(bad); err != nil || z != (SyncStreak{}) {
		t.Errorf("a malformed streak read as %+v (%v), want zero", z, err)
	}
	hostile := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostile, SyncStreakName), []byte(`{"version":1,"missed":2,"last_name":"leftover\u001b[31m-found\u0007"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := ReadSyncStreak(hostile)
	if err != nil || strings.ContainsAny(h.LastName, "\x1b\x07") || h.Missed != 2 {
		t.Errorf("the hostile name read back as %q (%v)", h.LastName, err)
	}

	for i := 0; i < 2; i++ {
		if err := RemoveSyncStreak(out); err != nil {
			t.Fatalf("remove %d: %v", i, err)
		}
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the streak is still there after removal (%v)", err)
	}
}

// covers: MA-910, MA-908, R32
// The record's name (the gate-2 adversarial pass after AS5 and AS6): Named gives a record
// without a RecordID a random one and keeps one it has; two records started apart get
// different ones; the name is written and read back. One Named could not have chosen —
// too long, or with anything but letters and digits — reads as none, so a planted record
// cannot make the told-file too big to read, and the next write names the record again.
// Positive twin first.
func TestTheSyncRecordIsNamed(t *testing.T) {
	out := t.TempDir()
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	a := SyncStreak{}.Miss("owner-active-skipped", t0).Named()
	if a.RecordID == "" || !plausibleRecordID(a.RecordID) {
		t.Fatalf("positive twin: a record was named %q", a.RecordID)
	}
	if err := WriteSyncStreak(out, a); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSyncStreak(out)
	if err != nil || got.RecordID != a.RecordID {
		t.Fatalf("the name read back as %q (%v), want %q", got.RecordID, err, a.RecordID)
	}
	if again := got.Miss("owner-active-skipped", t0.Add(time.Hour)).Named(); again.RecordID != a.RecordID {
		t.Errorf("a named record was named again: %q, was %q", again.RecordID, a.RecordID)
	}
	if b := (SyncStreak{}).Miss("owner-active-skipped", t0).Named(); b.RecordID == a.RecordID {
		t.Errorf("two records started apart share the name %q", b.RecordID)
	}

	for _, bad := range []string{strings.Repeat("A", maxRecordIDLen+1), "abc\u001b[31m", "abc def", "abc/../x"} {
		raw, err := json.Marshal(SyncStreak{Version: 1, Serial: 1, Missed: 2, RecordID: bad})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, SyncStreakName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		h, err := ReadSyncStreak(out)
		if err != nil || h.RecordID != "" || h.Missed != 2 {
			t.Errorf("a name Named could not have chosen (%q) read back as %q (%v)", bad, h.RecordID, err)
		}
	}
	ok := strings.Repeat("Z9", maxRecordIDLen/2)
	raw, _ := json.Marshal(SyncStreak{Version: 1, RecordID: ok})
	if err := os.WriteFile(filepath.Join(out, SyncStreakName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if h, err := ReadSyncStreak(out); err != nil || h.RecordID != ok {
		t.Errorf("a name at the bound read back as %q (%v)", h.RecordID, err)
	}
}

// covers: MA-920, S65
// Migration of the streak (§10, C-39, C-82): an archive from v1.5.1, with no streak
// file, starts clean — no error, the zero streak, and one miss after the upgrade is not
// stale. The downgrade residual (§10 (1)), pinned as stated: v1.5.1 runs never touch
// the streak, so a streak whose first miss was written before a downgrade still counts
// from that date after the upgrade back, and the first miss then raises the warning at
// once — accepted, not handled.
func TestTheSyncStreakMigration(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	out := t.TempDir()
	clean, err := ReadSyncStreak(out)
	if err != nil || clean != (SyncStreak{}) || clean.Stale() || clean.NothingArrived() {
		t.Fatalf("a v1.5.1 archive's missing streak read as %+v (%v), want a clean zero streak", clean, err)
	}
	if one := clean.Miss("owner-active-skipped", t0); one.Stale() || one.Missed != 1 || one.Serial != 1 {
		t.Errorf("the first miss after the upgrade: %+v, stale %v", one, one.Stale())
	}

	// The residual: a miss written before the downgrade, five days of v1.5.1 runs that
	// never touched the file (whatever they synced), then one miss after the upgrade.
	before := SyncStreak{}.Miss("owner-active-skipped", t0)
	if err := WriteSyncStreak(out, before); err != nil {
		t.Fatal(err)
	}
	after, err := ReadSyncStreak(out)
	if err != nil {
		t.Fatal(err)
	}
	after = after.Miss("owner-active-skipped", t0.Add(5*24*time.Hour))
	assure.Reached(t, after.Stale(), "the downgrade residual: the first miss after the upgrade is stale at once")
	if !after.FirstMissed.Equal(t0) || after.Serial != 1 {
		t.Errorf("the residual's streak: %+v, want first miss %v and serial 1", after, t0)
	}
}
