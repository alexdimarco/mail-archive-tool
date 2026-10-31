package presync

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// The quiet-rule review of the wait fix (MA-932), 2026-10-31: independent reviewer
// agents checked the first-change gate against Thunderbird's own file layouts and
// against the session's clock. These tests encode the findings that held
// (docs/review-presync-attended-predesign.md, "Quiet-rule review of the wait fix").

// classify lays the given files out under a temp root and reports, for each, whether
// the real walk listed it as mail.
func classify(t *testing.T, files []string) map[string]bool {
	t.Helper()
	root := t.TempDir()
	for _, rel := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "data")
	}
	snap, trunc := digestWatch([]string{root})
	if trunc || len(snap.holes) > 0 {
		t.Fatalf("a small tree came back truncated=%v holes=%q", trunc, snap.holes)
	}
	got := map[string]bool{}
	for key := range snap.store {
		got[strings.ReplaceAll(strings.TrimPrefix(key, walkKey(0, ".")+"\x00"), "\x00", "/")] = true
	}
	return got
}

// covers: MA-933
// The classifier against Thunderbird's own layouts: an mbox is mail only beside its
// .msf index, so the temporary file Thunderbird 128 compacts into ("INBOX-1", no
// index) is not; compaction's nstmp is never mail, even beside its own nstmp.msf
// (Thunderbird 115 local folders) or under a unique name ("nstmp-1"); the system's
// own files inside a maildir cur/new are not mail; and an RSS account (the folder
// holding feeds.json) is not mail. Positive twins first: each real mailbox counts.
func TestClassifierFollowsThunderbirdsLayouts(t *testing.T) {
	mail := []string{ // positive twins
		"ImapMail/h/INBOX", "ImapMail/h/Lists.golang", "ImapMail/h/INBOX.sbd/Work",
		"Mail/Local Folders/Trash", "Mail/Local Folders/nstmpx", "Mail/Local Folders/nstmp.sbd/Sub",
		"Mail/md/INBOX/cur/1698710400.M1P2.eml", "Mail/md/INBOX/new/1698710401.eml",
	}
	notMail := []string{
		"ImapMail/h/INBOX-1",            // Thunderbird 128's compaction temp (layout-1)
		"ImapMail/h/Lists-1.golang",     // the same, for a dotted name
		"ImapMail/h/Orphan",             // no extension and no index
		"Mail/Local Folders/nstmp",      // 115 compaction, beside its own nstmp.msf (layout-2)
		"Mail/Local Folders/nstmp-1",    // a unique compaction name (logic-6)
		"Mail/Local Folders/NSTMP-2",    // any case, beside an index
		"Mail/md/INBOX/cur/.DS_Store",   // the system's own, inside a maildir (layout-3)
		"Mail/md/INBOX/cur/._1.eml",     // a macOS AppleDouble file
		"Mail/md/INBOX/cur/Thumbs.db",   // Windows Explorer
		"Mail/md/INBOX/new/desktop.ini", // Windows Explorer
		"Mail/Feeds/Blog",               // an RSS feed folder (effects-5)
		"Mail/Feeds/Blog.sbd/Sub",       // and its subfolder
	}
	var files []string
	for _, rel := range append(append([]string{}, mail...), notMail...) {
		files = append(files, rel)
		if !strings.Contains(rel, "/cur/") && !strings.Contains(rel, "/new/") && rel != "ImapMail/h/INBOX-1" &&
			rel != "ImapMail/h/Lists-1.golang" && rel != "ImapMail/h/Orphan" && rel != "Mail/Local Folders/nstmp-1" {
			files = append(files, rel+".msf") // the summary index Thunderbird keeps beside each folder
		}
	}
	files = append(files, "Mail/Feeds/feeds.json", "Mail/Feeds/feeditems.json", "Mail/md/INBOX.msf")
	got := classify(t, files)
	for _, rel := range mail {
		if !got[rel] {
			t.Errorf("twin: %s is a mailbox but was not listed as mail", rel)
		}
	}
	for _, rel := range notMail {
		if got[rel] {
			t.Errorf("%s was listed as mail", rel)
		}
	}
}

// covers: MA-933
// The gate on real files, through the real digest, for the review's sequences: a
// compaction in Thunderbird 128 (INBOX-1 grows beside INBOX, then is renamed over
// it) and in 115 (nstmp and nstmp.msf grow, then are renamed over INBOX and
// INBOX.msf), a Finder .DS_Store in a maildir cur, and a feed folder growing leave it
// shut. Positive twin first: mail appended to INBOX opens it.
func TestGateShutForCompactionTempsAndNonMail(t *testing.T) {
	type step func(dir string)
	appendTo := func(rel, s string) step {
		return func(dir string) {
			f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(rel)), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(s); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	rename := func(from, to string) step {
		return func(dir string) {
			if err := os.Rename(filepath.Join(dir, filepath.FromSlash(from)), filepath.Join(dir, filepath.FromSlash(to))); err != nil {
				t.Fatal(err)
			}
		}
	}
	both := func(a, b step) step { return func(dir string) { a(dir); b(dir) } }
	run := func(steps ...step) bool {
		dir := t.TempDir()
		for rel, body := range map[string]string{
			"h/INBOX": strings.Repeat("m", 4000), "h/INBOX.msf": "idx",
			"md/INBOX/cur/1.eml": "msg", "md/INBOX.msf": "idx",
			"Feeds/feeds.json": "[]", "Feeds/Blog": "item", "Feeds/Blog.msf": "idx",
		} {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, p, body)
		}
		q := newQuietTracker(gateBase, gateCfg)
		s0, _ := digestWatch([]string{dir})
		q.observe(s0, gateBase)
		for i, st := range steps {
			st(dir)
			s, _ := digestWatch([]string{dir})
			q.observe(s, gateBase.Add(time.Duration(10*(i+1))*time.Second))
		}
		return q.changed
	}
	if !run(appendTo("h/INBOX", "new mail")) {
		t.Fatal("positive twin: mail appended to INBOX did not open the gate")
	}
	if run(appendTo("h/INBOX-1", strings.Repeat("m", 1000)), appendTo("h/INBOX-1", strings.Repeat("m", 1000)), rename("h/INBOX-1", "h/INBOX")) {
		t.Error("a Thunderbird 128 compaction (INBOX-1 grown, renamed over INBOX) opened the gate")
	}
	if run(
		both(appendTo("h/nstmp", strings.Repeat("m", 1000)), appendTo("h/nstmp.msf", "i")),
		both(appendTo("h/nstmp", strings.Repeat("m", 1000)), appendTo("h/nstmp.msf", "i")),
		both(rename("h/nstmp", "h/INBOX"), rename("h/nstmp.msf", "h/INBOX.msf")),
	) {
		t.Error("a Thunderbird 115 compaction (nstmp and nstmp.msf grown, renamed over INBOX) opened the gate")
	}
	if run(appendTo("md/INBOX/cur/.DS_Store", "finder")) {
		t.Error("a Finder .DS_Store written into a maildir cur opened the gate")
	}
	if run(appendTo("Feeds/Blog", "a new feed item")) {
		t.Error("a feed folder that grew opened the gate")
	}
}

// covers: MA-933
// A watch root inside another is dropped, so no file is walked twice (logic-2): the
// usual job input <profile>/ImapMail/<account> lies inside <profile>/ImapMail.
// Positive twin first: a root that is not inside another is kept.
func TestWatchRootsDropANestedRoot(t *testing.T) {
	profile := t.TempDir()
	acct := filepath.Join(profile, "ImapMail", "acct")
	local := filepath.Join(profile, "Mail")
	for _, d := range []string{acct, local} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	profile, _ = filepath.EvalSymlinks(profile)
	acct, local = filepath.Join(profile, "ImapMail", "acct"), filepath.Join(profile, "Mail")
	if got := watchRoots(profile, nil); len(got) != 2 || got[1] != local {
		t.Fatalf("twin: the profile's two mail folders gave %q", got)
	}
	got := watchRoots(profile, []string{acct})
	want := []string{filepath.Join(profile, "ImapMail"), local}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("an input inside ImapMail: roots %q, want %q", got, want)
	}
}

// covers: MA-933
// Nested roots and the entry cap cannot open the gate with no mail (logic-2): the walk
// lists a file once, under its first visit's key, so a new index that moves the cap's
// stopping point does not turn a mailbox already seen into a "new" one. Positive twin
// first: a new message, at a place the walk sees, opens the gate in the same setup.
func TestNestedRootsAndTheCapCannotOpenTheGate(t *testing.T) {
	orig := watchMaxEntries
	defer func() { watchMaxEntries = orig }()
	root := t.TempDir()
	acct := filepath.Join(root, "acct")
	cur := filepath.Join(acct, "INBOX", "cur")
	if err := os.MkdirAll(cur, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		writeFile(t, filepath.Join(cur, "m"+string(rune('a'+i))+".eml"), "mail")
	}
	roots := []string{root, acct}
	// root: root, acct, INBOX, cur, 20 messages = 24 nodes; then acct again: acct,
	// INBOX, cur, and the cap stops 7 messages in.
	watchMaxEntries = 24 + 3 + 7
	gate := func(add string) bool {
		q := newQuietTracker(gateBase, gateCfg)
		s0, _ := digestWatch(roots)
		q.observe(s0, gateBase)
		writeFile(t, filepath.Join(acct, filepath.FromSlash(add)), "x")
		defer os.Remove(filepath.Join(acct, filepath.FromSlash(add)))
		s1, trunc := digestWatch(roots)
		if !trunc {
			t.Fatal("the fixture's walk was not capped: the test proves nothing")
		}
		q.observe(s1, gateBase.Add(10*time.Second))
		return q.changed
	}
	if !gate("INBOX/cur/0new.eml") {
		t.Fatal("twin: a new message the walk saw did not open the gate")
	}
	if gate("0.msf") {
		t.Error("a new index moved the cap's stopping point and opened the gate with no mail")
	}
	s, _ := digestWatch([]string{root, acct})
	for key := range s.store {
		if !strings.HasPrefix(key, walkKey(0, ".")) {
			t.Errorf("a file walked twice is listed under its second visit's key %q", key)
		}
	}
}

// covers: MA-934
// The gate's starting point is taken before the launch, while no Thunderbird runs
// (logic-1, effects-1): mail written between the start and the first wait poll (the
// macOS lab saw it at +6 s; Windows can spend 15 s minimizing first) opens the gate,
// so a run where only index touches follow ends ok, not "nothing arrived". Mail that
// was already there before the launch does not. Both modes.
func TestGateStartsFromBeforeTheLaunch(t *testing.T) {
	for _, attended := range []bool{false, true} {
		run := func(mailAt time.Time) sessionResult {
			f := syncingFake()
			f.appearIn = 8 * time.Second
			f.changeAt, f.indexTouchAt = mailAt, as1aBase.Add(40*time.Second)
			d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
			if attended {
				f.idle = 0
				d = attendedDeps(f, d.statePath)
			}
			return runSession(context.Background(), d)
		}
		if r := run(as1aBase.Add(30 * time.Second)); r.outcome != OutcomeOK {
			t.Fatalf("attended=%v twin: mail after the first poll ended %q, want ok", attended, r.outcome.Name)
		}
		if r := run(as1aBase.Add(3 * time.Second)); r.outcome != OutcomeOK {
			t.Errorf("attended=%v: mail written before the first wait poll, then index touches, ended %q; want ok", attended, r.outcome.Name)
		}
		if r := run(as1aBase.Add(-time.Hour)); r.outcome != OutcomeNoActivity {
			t.Errorf("attended=%v: mail already there before the launch ended %q; want ok-noactivity", attended, r.outcome.Name)
		}
	}
}

// laterAfterSleep returns a time later than base by mono on the monotonic clock and
// by wall on the wall clock: what time.Now reads after a Mac has slept between two
// readings (Go's monotonic clock stops in sleep on macOS; design §4.4.2, lab L-C3).
// It writes time.Time's own layout, then checks the result through the public API,
// so a changed layout fails here instead of passing.
func laterAfterSleep(t *testing.T, base time.Time, mono, wall time.Duration) time.Time {
	t.Helper()
	if !strings.Contains(base.String(), " m=") {
		t.Fatal("base has no monotonic reading")
	}
	type layout struct {
		wall uint64
		ext  int64
		loc  *time.Location
	}
	later := base.Add(mono)
	(*layout)(unsafe.Pointer(&later)).wall += uint64((wall-mono)/time.Second) << 30
	if later.Sub(base) != mono || later.Round(0).Sub(base.Round(0)) != wall {
		t.Fatalf("forged time: monotonic %v (want %v), wall %v (want %v)", later.Sub(base), mono, later.Round(0).Sub(base.Round(0)), wall)
	}
	return later
}

// covers: MA-935
// Sleep is judged on wall time (design §4.4.2, logic-4, effects-4): on macOS the
// monotonic clock stops in sleep, so an hour asleep reads as 10 s on it. After a
// wake the quiet clock restarts, so the wait does not end "synced" before
// Thunderbird has reconnected.
func TestSleepIsJudgedOnWallTime(t *testing.T) {
	base := time.Now()
	in, grown := map[string]int64{"/w/INBOX": 100}, map[string]int64{"/w/INBOX": 900}
	q := newQuietTracker(base, gateCfg)
	q.observe(snapOf(1, in), base)
	q.observe(snapOf(2, grown), base.Add(10*time.Second)) // mail: the gate opens
	woke := laterAfterSleep(t, base, 20*time.Second, 3620*time.Second)
	q.observe(snapOf(2, grown), woke) // nothing changed, but an hour went by asleep
	if o, done := q.verdict(woke.Add(170 * time.Second)); done {
		t.Fatalf("170 s after waking from an hour's sleep the wait ended %q: the sleep was not seen", o.Name)
	}
	if o, done := q.verdict(woke.Add(gateCfg.quiet)); !done || o != OutcomeOK {
		t.Fatalf("quiet after the wake: got (%q, done=%v), want ok", o.Name, done)
	}
}

// covers: MA-935
// The maximum wait counts time awake (effects-2, logic-4): on Windows the clocks run
// on through sleep, so a computer that slept for 8 hours mid-wait must not end "nothing
// arrived" on the first poll after waking, before Thunderbird has had time to fetch.
// It still ends ok-noactivity once 20 minutes awake have passed with no mail.
func TestMaxWaitCountsTimeAwake(t *testing.T) {
	in := map[string]int64{"/w/INBOX": 100}
	q := newQuietTracker(gateBase, gateCfg)
	for s := 0; s <= 100; s += 10 {
		q.observe(snapOf(1, in), gateBase.Add(time.Duration(s)*time.Second))
	}
	asleep := 8 * time.Hour
	for s := 110; ; s += 10 {
		now := gateBase.Add(asleep + time.Duration(s)*time.Second)
		q.observe(snapOf(1, in), now)
		o, done := q.verdict(now)
		if s == 110 && done {
			t.Fatalf("the first poll after 8 h asleep ended the wait %q", o.Name)
		}
		if done {
			if o != OutcomeNoActivity || s < 1170 || s > 1200 {
				t.Fatalf("ended %q at %d s awake; want ok-noactivity at about 1200 s awake", o.Name, s)
			}
			return
		}
		if s > 1300 {
			t.Fatal("the wait never ended after 20 minutes awake")
		}
	}
}

// covers: MA-935
// A slow walk is not a sleep (logic-4): the gap that tells a sleep is from the end of
// one poll's walk to the start of the next, so a profile that takes 25 s to walk still
// reaches quiet. In the tracker, and through the session on the scripted fake.
func TestSlowWalkIsNotASleep(t *testing.T) {
	in, grown := map[string]int64{"/w/INBOX": 100}, map[string]int64{"/w/INBOX": 900}
	q := newQuietTracker(gateBase, gateCfg)
	at := func(s int) time.Time { return gateBase.Add(time.Duration(s) * time.Second) }
	q.observeWalk(snapOf(1, in), at(0), at(25))
	q.observeWalk(snapOf(2, grown), at(35), at(60)) // mail; lastChange = +60
	ended := false
	for start := 70; start < 400; start += 35 {
		q.observeWalk(snapOf(2, grown), at(start), at(start+25))
		if o, done := q.verdict(at(start + 25)); done {
			if o != OutcomeOK {
				t.Fatalf("ended %q, want ok", o.Name)
			}
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("25 s walks were taken for sleeps: the wait never reached quiet")
	}

	f := syncingFake()
	f.digestHook = func() { f.nowT = f.nowT.Add(25 * time.Second) } // each S7 walk takes 25 s
	d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
	if r := runSession(context.Background(), d); r.outcome != OutcomeOK {
		t.Errorf("session with 25 s walks ended %q, want ok", r.outcome.Name)
	}
}

// covers: MA-935
// A poll slower than the default stretches the sleep gap by as much, so a coarse
// poll is not a sleep: 150 s polls with no mail still end ok-noactivity at 20
// minutes, and mail then quiet still ends ok.
func TestCoarsePollIsNotASleep(t *testing.T) {
	cfg := gateCfg
	cfg.poll = 150 * time.Second
	in, grown := map[string]int64{"/w/INBOX": 100}, map[string]int64{"/w/INBOX": 900}
	q := newQuietTracker(gateBase, cfg)
	for s := 0; s < 1200; s += 150 {
		q.observe(snapOf(1, in), gateBase.Add(time.Duration(s)*time.Second))
	}
	if o, done := q.verdict(gateBase.Add(1200 * time.Second)); !done || o != OutcomeNoActivity {
		t.Errorf("150 s polls, no mail, at 20 minutes: got (%q, done=%v), want ok-noactivity", o.Name, done)
	}
	q = newQuietTracker(gateBase, cfg)
	q.observe(snapOf(1, in), gateBase)
	q.observe(snapOf(2, grown), gateBase.Add(150*time.Second))
	q.observe(snapOf(2, grown), gateBase.Add(300*time.Second))
	if o, done := q.verdict(gateBase.Add(330 * time.Second)); !done || o != OutcomeOK {
		t.Errorf("150 s polls, mail then quiet: got (%q, done=%v), want ok", o.Name, done)
	}

	f := syncingFake()
	f.changeAt = as1aBase.Add(24 * time.Hour) // nothing arrives
	d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.cfg = quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait}
	d.pollEvery = 150 * time.Second
	if r := runSession(context.Background(), d); r.outcome != OutcomeNoActivity || r.waited > defMaxWait+d.pollEvery {
		t.Errorf("session with 150 s polls ended %q after %v; want ok-noactivity by %v", r.outcome.Name, r.waited, defMaxWait+d.pollEvery)
	}
}

// covers: MA-935
// Through the session: a computer that sleeps 8 hours a minute into a wait with no
// mail goes on waiting when it wakes, its progress lines counting minutes awake
// (1..19, each once), and ends ok-noactivity after 20 minutes awake.
func TestSessionWaitCountsTimeAwake(t *testing.T) {
	f := syncingFake()
	f.changeAt = as1aBase.Add(24 * time.Hour * 30) // nothing arrives
	polls, slept := 0, false
	f.digestHook = func() { polls++ }
	f.probeHook = func(name string) {
		if name == "alive" && polls == 6 && !slept { // after the 6th poll's walk, before the 7th
			slept = true
			f.nowT = f.nowT.Add(8 * time.Hour)
		}
	}
	d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
	d.cfg = quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait}
	var log bytes.Buffer
	d.log = &log
	r := runSession(context.Background(), d)
	if r.outcome != OutcomeNoActivity || r.waited < 8*time.Hour {
		t.Fatalf("ended %q after %v; want ok-noactivity after the 8 h sleep and 20 minutes awake", r.outcome.Name, r.waited)
	}
	ns := progressMinutes(t, log.String())
	if len(ns) != 19 {
		t.Errorf("progress minutes %v; want 1..19, minutes awake", ns)
	}
	for i, n := range ns {
		if n != i+1 {
			t.Errorf("progress line %d says %d min, want %d", i, n, i+1)
			break
		}
	}
}

// awakeFake is the scripted fake that also keeps the computer awake on request, the
// way the Windows adapter does (effects-2).
type awakeFake struct {
	*fakeEnv
	held, holds, releases int
}

func (a *awakeFake) holdAwake() func() {
	a.holds++
	a.held++
	return func() { a.held--; a.releases++ }
}

// covers: MA-935
// The session keeps the computer awake from the launch to its end (effects-2), so an
// idle sleep cannot cut the wait; a session that launches nothing asks for nothing.
func TestSessionKeepsTheComputerAwake(t *testing.T) {
	a := &awakeFake{fakeEnv: syncingFake()}
	asleepAt := []int{}
	polls := 0
	a.digestHook = func() {
		polls++
		if a.held != 1 {
			asleepAt = append(asleepAt, polls)
		}
	}
	d := syncingDeps(a, filepath.Join(t.TempDir(), "s.json"))
	if r := runSession(context.Background(), d); r.outcome != OutcomeOK {
		t.Fatalf("the session ended %q, want ok", r.outcome.Name)
	}
	if polls == 0 || len(asleepAt) > 0 || a.holds != 1 || a.releases != 1 || a.held != 0 {
		t.Errorf("polls %d, not held at %v, holds %d, releases %d, still held %d; want held through every poll, once, released",
			polls, asleepAt, a.holds, a.releases, a.held)
	}

	away := &awakeFake{fakeEnv: syncingFake()}
	away.idle = 0 // the owner is at the computer: a scheduled run skips at S4
	d = syncingDeps(away, filepath.Join(t.TempDir(), "s.json"))
	r := runSession(context.Background(), d)
	if r.outcome != OutcomeOwnerActiveSkipped || away.startCalls != 0 || away.holds != 0 {
		t.Errorf("a session that launched nothing (%q, %d starts) held the computer awake %d times", r.outcome.Name, away.startCalls, away.holds)
	}
}
