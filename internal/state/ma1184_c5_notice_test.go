package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-1184
// The behind streak is due each email at most once (§5's TC-20 fold, slice C5): a streak whose
// behind runs span 3 days or more (at least 2 runs) is due the still-downloading email; one whose X
// has not grown across behind runs spanning 3 days is due the stopped email; each goes at most once
// per streak (the record's two flags, set only by MarkSent after the email went); a stopped email
// may follow a still-downloading one, never the reverse; a streak that stalls from its start is due
// only the stopped one. MarkSent marks only the streak it names (store and serial). A streak that
// ends and starts again is a new streak with new flags. Positive twin first: a streak that grew over
// 3 days is due the still-downloading email.
func TestTheStreakIsDueEachEmailOnce(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	s := TBStreak{}.Behind("k", "imap.x.example", 100, t0)
	if a, _ := s.Account("k"); func() bool { d, _ := a.Due(); return d }() {
		t.Fatalf("one run is due an email: %+v", a)
	}
	s = s.Behind("k", "imap.x.example", 500, t0.Add(3*day))
	a, _ := s.Account("k")
	if due, stopped := a.Due(); !due || stopped {
		t.Fatalf("positive twin: grown over 3 days is due (%v, stopped %v), want the still-downloading email: %+v", due, stopped, a)
	}
	if _, ok := s.MarkSent("k", a.Serial+1, false); ok {
		t.Error("MarkSent marked a streak of another serial")
	}
	if _, ok := s.MarkSent("other", a.Serial, false); ok {
		t.Error("MarkSent marked a store it does not hold")
	}
	s, ok := s.MarkSent("k", a.Serial, false)
	if a, _ = s.Account("k"); !ok || !a.StillSent || a.StoppedSent {
		t.Fatalf("MarkSent (still): %v %+v", ok, a)
	}
	if due, _ := a.Due(); due {
		t.Errorf("the still-downloading email is due twice: %+v", a)
	}
	// X unchanged for a day: nothing. For 3 days and 3 backups (FALSE-01's rule): the stopped
	// email, once.
	s = s.Behind("k", "imap.x.example", 500, t0.Add(4*day))
	if a, _ = s.Account("k"); func() bool { d, _ := a.Due(); return d }() {
		t.Errorf("a day without growth is due an email: %+v", a)
	}
	s = s.Behind("k", "imap.x.example", 500, t0.Add(5*day))
	s = s.Behind("k", "imap.x.example", 500, t0.Add(6*day+time.Hour))
	a, _ = s.Account("k")
	if due, stopped := a.Due(); !due || !stopped {
		t.Fatalf("3 days without growth: due %v stopped %v, want the stopped email: %+v", due, stopped, a)
	}
	s, _ = s.MarkSent("k", a.Serial, true)
	s = s.Behind("k", "imap.x.example", 500, t0.Add(9*day))
	if a, _ = s.Account("k"); !a.StoppedSent || !a.StillSent || func() bool { d, _ := a.Due(); return d }() {
		t.Errorf("the stopped email is due twice: %+v", a)
	}

	// A streak that stalls from its start: only the stopped email, and never a still-downloading
	// one after it, even once X grows again.
	b := TBStreak{}.Behind("j", "imap.y.example", 7, t0).Behind("j", "imap.y.example", 7, t0.Add(day)).Behind("j", "imap.y.example", 7, t0.Add(2*day)).Behind("j", "imap.y.example", 7, t0.Add(3*day))
	ja, _ := b.Account("j")
	if due, stopped := ja.Due(); !due || !stopped {
		t.Fatalf("a stall from the start: due %v stopped %v, want the stopped email only", due, stopped)
	}
	b, _ = b.MarkSent("j", ja.Serial, true)
	b = b.Behind("j", "imap.y.example", 900, t0.Add(4*day))
	if ja, _ = b.Account("j"); !ja.StillDownloading() || func() bool { d, _ := ja.Due(); return d }() {
		t.Errorf("a still-downloading email is due after the stopped one: %+v", ja)
	}
	// The streak ends; the next one is a new streak, its emails due again in their turn.
	b = b.End("j").Behind("j", "imap.y.example", 10, t0.Add(10*day)).Behind("j", "imap.y.example", 20, t0.Add(13*day))
	if ja, _ = b.Account("j"); ja.Serial != 2 || ja.StillSent || ja.StoppedSent || func() bool { d, s := ja.Due(); return !d || s }() {
		t.Errorf("a new streak after the end: %+v", ja)
	}
}

// covers: MA-1281
// R4-01 (the C2 pass round 4, the lead's call): a record whose run counter or serial no run of this
// archive could have reached, or whose entry claims a streak number above the record's, is
// implausible, so the run treats it as damaged (fresh streak and a WARN, in the app). Positive twin
// first: a record built by real folds and read back is plausible.
func TestAnImplausibleStreakRecordIsSeen(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	s := TBStreak{}.StartRun(t0)
	s = s.Behind("k", "imap.x.example", 1, t0).StartRun(t0.Add(time.Hour)).Behind("k", "imap.x.example", 2, t0.Add(time.Hour))
	out := t.TempDir()
	if err := WriteTBStreak(out, s.Named()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTBStreak(out)
	if err != nil || got.Damaged() {
		t.Fatalf("positive twin: a record of real folds: %v implausible %v", err, got.Damaged())
	}
	edge := got
	edge.Run, edge.Serial = tbStreakPlausibleMax, tbStreakPlausibleMax
	if edge.Damaged() {
		t.Errorf("a counter at the bound itself is implausible")
	}
	for name, plant := range map[string]string{
		"a run counter at the cap":     `{"version":1,"serial":1,"run":1099511627776,"accounts":[{"store":"k","serial":1,"runs":1}]}`,
		"a run counter past the bound": `{"version":1,"serial":1,"run":16777217,"accounts":[{"store":"k","serial":1,"runs":1}]}`,
		"a serial past the bound":      `{"version":1,"serial":16777217,"run":3,"accounts":[{"store":"k","serial":1,"runs":1}]}`,
		"an entry above the record":    `{"version":1,"serial":1,"run":3,"accounts":[{"store":"k","serial":2,"runs":1}]}`,
		"behind runs past the bound":   `{"version":1,"serial":1,"run":3,"accounts":[{"store":"k","serial":1,"runs":16777217}]}`,
	} {
		if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte(plant), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := ReadTBStreak(out)
		if err != nil || !r.Damaged() {
			t.Errorf("%s: read %v, implausible %v", name, err, r.Damaged())
		}
	}
}
