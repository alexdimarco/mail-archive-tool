package presync

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// The first-change gate counts only mail data (MA-932; design-presync-attended
// L-A9, owner's go-ahead 2026-10-30): the macOS lab saw Thunderbird touch a
// folder's .msf index about 2 s before it wrote mail, and its first fetch can
// wait for the ~10-minute periodic check, so an index touch alone must not start
// the 3-minute quiet clock that ends the wait as "synced".

var gateBase = time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)

var gateCfg = quietConfig{settle: 60 * time.Second, quiet: 180 * time.Second, maxWait: 1200 * time.Second}

type gatePoll struct {
	at   int // seconds after up
	snap watchSnap
}

func snapOf(sum uint64, store map[string]int64) watchSnap {
	return watchSnap{sum: sum, store: store}
}

// cappedAt is s as a walk stopped by the entry cap after the node frontier.
func cappedAt(s watchSnap, frontier string) watchSnap {
	s.capped, s.frontier = true, frontier
	return s
}

// withHoles is s as a walk that could not read the given entries.
func withHoles(s watchSnap, holes ...string) watchSnap {
	s.holes = holes
	return s
}

// withOther is s with these non-mail regular files walked too.
func withOther(s watchSnap, other map[string]int64) watchSnap {
	s.other = other
	return s
}

// k is the walk key of a path (slash-separated) under the first watch root.
func k(rel string) string { return walkKey(0, rel) }

// gateOpened feeds the polls and reports whether quiet was then reached at the
// last poll + quiet, well before maxWait — which only an opened gate allows.
func gateOpened(t *testing.T, polls []gatePoll) bool {
	t.Helper()
	q := newQuietTracker(gateBase, gateCfg)
	last := 0
	for _, p := range polls {
		q.observe(p.snap, gateBase.Add(time.Duration(p.at)*time.Second))
		last = p.at
	}
	at := gateBase.Add(time.Duration(last)*time.Second + gateCfg.quiet)
	if at.Sub(gateBase) >= gateCfg.maxWait {
		t.Fatalf("fixture too long: the check at %v reaches maxWait", at.Sub(gateBase))
	}
	o, done := q.verdict(at)
	if done && o != OutcomeOK {
		t.Fatalf("before maxWait the session ended %q; only ok may end it there", o.Name)
	}
	return done
}

// covers: MA-932
func TestFirstChangeGateCountsOnlyMailData(t *testing.T) {
	in, sent, work := k("host/INBOX"), k("host/Sent Items"), k("host/INBOX.sbd/Work")
	cur := func(folder, name string) string { return k("md/" + folder + "/cur/" + name) }
	cases := []struct {
		name  string
		polls []gatePoll
		open  bool
	}{
		// positive twins first: mail data arriving opens the gate
		{"a mailbox grows", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{10, snapOf(2, map[string]int64{in: 900})},
		}, true},
		{"a new mailbox holding mail appears", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{10, snapOf(2, map[string]int64{in: 100, sent: 50})},
		}, true},
		{"a new message file in a maildir cur", []gatePoll{
			{0, snapOf(1, map[string]int64{cur("INBOX", "1"): 10})},
			{10, snapOf(2, map[string]int64{cur("INBOX", "1"): 10, cur("INBOX", "2"): 20})},
		}, true},
		{"a new empty folder, then its first mail", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{10, snapOf(2, map[string]int64{in: 100, sent: 0})},
			{20, snapOf(3, map[string]int64{in: 100, sent: 40})},
		}, true},
		{"a seen mailbox grows while the walk is truncated", []gatePoll{
			{0, cappedAt(snapOf(1, map[string]int64{in: 100}), in)},
			{10, cappedAt(snapOf(2, map[string]int64{in: 200}), in)},
		}, true},
		{"a new mailbox inside the part a capped walk saw", []gatePoll{
			{0, cappedAt(snapOf(1, map[string]int64{in: 100}), k("host/Trash"))},
			{10, cappedAt(snapOf(2, map[string]int64{in: 100, sent: 50}), k("host/Trash"))},
		}, true},
		{"a new mailbox in a capped poll, at a place the last poll walked", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{10, cappedAt(snapOf(2, map[string]int64{in: 100, sent: 50}), sent)},
		}, true},
		{"a new mailbox beside a folder the last poll could not read", []gatePoll{
			{0, withHoles(snapOf(1, map[string]int64{in: 100}), k("host/INBOX.sbd"))},
			{10, snapOf(2, map[string]int64{in: 100, sent: 50})},
		}, true},
		{"a new maildir message while another folder is unreadable", []gatePoll{
			{0, withHoles(snapOf(1, map[string]int64{cur("INBOX", "1"): 10}), k("md/Archive"))},
			{10, withHoles(snapOf(2, map[string]int64{cur("INBOX", "1"): 10, cur("INBOX", "2"): 20}), k("md/Archive"))},
		}, true},
		{"a moved mailbox later grows", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 500})},
			{10, snapOf(2, map[string]int64{sent: 500})},
			{20, snapOf(3, map[string]int64{sent: 600})},
		}, true},

		// what is not mail data leaves the gate shut
		{"an index (.msf) touch alone", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{4, snapOf(2, map[string]int64{in: 100})},
			{14, snapOf(3, map[string]int64{in: 100})},
		}, false},
		{"a compaction shrinks the mailbox", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 900})},
			{10, snapOf(2, map[string]int64{in: 400})},
		}, false},
		{"a rename that keeps the size", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 500})},
			{10, snapOf(2, map[string]int64{sent: 500})},
		}, false},
		{"a maildir message moved to another folder", []gatePoll{
			{0, snapOf(1, map[string]int64{cur("INBOX", "1"): 30})},
			{10, snapOf(2, map[string]int64{cur("Archive", "1"): 30})},
		}, false},
		{"a new empty folder", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100})},
			{10, snapOf(2, map[string]int64{in: 100, sent: 0})},
		}, false},
		{"a mailbox that comes back after its index was rebuilt", []gatePoll{
			{0, snapOf(1, map[string]int64{in: 100, k("host/a.b"): 500})},
			{10, snapOf(2, map[string]int64{in: 100})},
			{20, snapOf(3, map[string]int64{in: 100, k("host/a.b"): 500})},
		}, false},
		{"a dotted mailbox that counts as mail once its index appears", []gatePoll{
			{0, withOther(snapOf(1, map[string]int64{in: 100}), map[string]int64{k("host/a.b"): 500})},
			{10, withOther(snapOf(2, map[string]int64{in: 100, k("host/a.b"): 500}), map[string]int64{k("host/a.b.msf"): 9})},
		}, false},
		{"a mailbox first seen past where the last capped walk stopped", []gatePoll{
			{0, cappedAt(snapOf(1, map[string]int64{in: 100}), in)},
			{10, snapOf(2, map[string]int64{in: 100, sent: 50})},
		}, false},
		{"a mailbox first seen inside a folder the last poll could not read", []gatePoll{
			{0, withHoles(snapOf(1, map[string]int64{in: 100}), k("host/INBOX.sbd"))},
			{10, snapOf(2, map[string]int64{in: 100, work: 50})},
		}, false},
		{"a mailbox first seen where a file could not be read last poll", []gatePoll{
			{0, withHoles(snapOf(1, map[string]int64{in: 100}), sent)},
			{10, snapOf(2, map[string]int64{in: 100, sent: 50})},
		}, false},
	}
	// A store file the cap now hides is not gone, so it cannot explain a new file
	// of its size as a move: positive twin first (really gone: a move).
	moved := []gatePoll{
		{0, snapOf(1, map[string]int64{in: 500, k("host/Old"): 700})},
		{10, snapOf(2, map[string]int64{in: 500, k("host/A"): 700})},
	}
	if gateOpened(t, moved) {
		t.Errorf("twin: Old gone and A of its size appearing (a move) opened the gate")
	}
	hidden := []gatePoll{
		{0, snapOf(1, map[string]int64{in: 500, k("host/Old"): 700})},
		{10, cappedAt(snapOf(2, map[string]int64{in: 500, k("host/A"): 700}), k("host/B"))}, // Old is past the cap, not gone
	}
	cases = append(cases, struct {
		name  string
		polls []gatePoll
		open  bool
	}{"new mail the size of a file the cap now hides", hidden, true})
	for _, c := range cases {
		if got := gateOpened(t, c.polls); got != c.open {
			t.Errorf("%s: gate opened = %v, want %v", c.name, got, c.open)
		}
	}
}

// covers: MA-932
// Once mail has opened the gate, ANY watched change — an index touch included —
// still resets the quiet clock (as before); and a session that sees only index
// touches waits to maxWait and ends ok-noactivity, never "synced".
func TestFirstChangeGateTimeline(t *testing.T) {
	in := map[string]int64{"/w/INBOX": 100}
	grown := map[string]int64{"/w/INBOX": 900}

	t.Run("an index touch, then mail at the periodic check", func(t *testing.T) {
		q := newQuietTracker(gateBase, gateCfg)
		q.observe(snapOf(1, in), gateBase)
		q.observe(snapOf(2, in), gateBase.Add(4*time.Second)) // INBOX.msf at +4 s
		for s := 10; s < 573; s += 10 {
			q.observe(snapOf(2, in), gateBase.Add(time.Duration(s)*time.Second))
			if o, done := q.verdict(gateBase.Add(time.Duration(s) * time.Second)); done {
				t.Fatalf("at +%ds, before any mail, the wait ended %q: the index touch opened the gate", s, o.Name)
			}
		}
		q.observe(snapOf(3, grown), gateBase.Add(575*time.Second)) // the mail at +575 s
		if _, done := q.verdict(gateBase.Add(575*time.Second + gateCfg.quiet - time.Second)); done {
			t.Fatal("the wait ended before quiet after the mail")
		}
		if o, done := q.verdict(gateBase.Add(575*time.Second + gateCfg.quiet)); !done || o != OutcomeOK {
			t.Fatalf("quiet after the mail: got (%q, done=%v), want ok", o.Name, done)
		}
	})

	t.Run("after the mail, an index touch still resets the clock", func(t *testing.T) {
		q := newQuietTracker(gateBase, gateCfg)
		q.observe(snapOf(1, in), gateBase)
		q.observe(snapOf(2, grown), gateBase.Add(10*time.Second))
		q.observe(snapOf(3, grown), gateBase.Add(100*time.Second)) // an .msf touch
		if _, done := q.verdict(gateBase.Add(10*time.Second + gateCfg.quiet)); done {
			t.Fatal("the index touch after the mail did not reset the quiet clock")
		}
		if o, done := q.verdict(gateBase.Add(100*time.Second + gateCfg.quiet)); !done || o != OutcomeOK {
			t.Fatalf("quiet after the last touch: got (%q, done=%v), want ok", o.Name, done)
		}
	})

	t.Run("only index touches, all the way to maxWait", func(t *testing.T) {
		q := newQuietTracker(gateBase, gateCfg)
		for s := 0; s <= 1200; s += 10 {
			now := gateBase.Add(time.Duration(s) * time.Second)
			var sum uint64
			switch {
			case s == 0:
				sum = 1 // the baseline
			case s < 360:
				sum = 2 // the start-up index touch, seen at +10 s
			default:
				sum = uint64(s) // the periodic touches, every poll from +360 s
			}
			q.observe(snapOf(sum, in), now)
			o, done := q.verdict(now)
			if s < 1200 && done {
				t.Fatalf("at +%ds the wait ended %q on index touches alone", s, o.Name)
			}
			if s == 1200 && (!done || o != OutcomeNoActivity) {
				t.Fatalf("at maxWait: got (%q, done=%v), want ok-noactivity", o.Name, done)
			}
		}
	})
}

// covers: MA-932
// The classifier on a real profile tree: digestWatch lists exactly the
// message-store files — mboxes (with or without an extension of their own, by
// their .msf sibling), subfolder mboxes, maildir messages in cur and new — and
// not indexes, filter rules, logs, feeds, compaction's nstmp, a maildir's tmp,
// search-integration copies or the system's own files.
func TestDigestWatchListsOnlyTheMessageStore(t *testing.T) {
	root := t.TempDir()
	imap := filepath.Join(root, "ImapMail", "imap.example.com")
	local := filepath.Join(root, "Mail", "Local Folders")
	md := filepath.Join(root, "Mail", "maildir.example.com")
	files := map[string]bool{ // relative path -> is mail
		"ImapMail/imap.example.com/INBOX":                     true,
		"ImapMail/imap.example.com/INBOX.msf":                 false,
		"ImapMail/imap.example.com/Sent Items":                true,
		"ImapMail/imap.example.com/Sent Items.msf":            false,
		"ImapMail/imap.example.com/Lists.golang":              true, // a dotted folder name
		"ImapMail/imap.example.com/Lists.golang.msf":          false,
		"ImapMail/imap.example.com/INBOX.sbd/Work":            true,
		"ImapMail/imap.example.com/INBOX.sbd/Work.msf":        false,
		"ImapMail/imap.example.com/msgFilterRules.dat":        false,
		"ImapMail/imap.example.com/filterlog.html":            false,
		"ImapMail/imap.example.com/nstmp":                     false,
		"ImapMail/imap.example.com/INBOX.mozmsgs/1.wdseml":    false,
		"ImapMail/imap.example.com/.DS_Store":                 false,
		"Mail/Local Folders/Trash":                            true,
		"Mail/Local Folders/Trash.msf":                        false,
		"Mail/Local Folders/Unsent Messages":                  true,
		"Mail/Local Folders/popstate.dat":                     false,
		"Mail/Local Folders/feeds.json":                       false,
		"Mail/maildir.example.com/INBOX.msf":                  false,
		"Mail/maildir.example.com/INBOX/cur/1698710400.M1P2":  true,
		"Mail/maildir.example.com/INBOX/cur/1698710401.eml":   true,
		"Mail/maildir.example.com/INBOX/new/1698710402.M3P4":  true,
		"Mail/maildir.example.com/INBOX/tmp/1698710403.M5P6":  false,
		"Mail/maildir.example.com/INBOX/tmp/1698710404":       false,
		"Mail/maildir.example.com/INBOX/cur/1698710400.M1P2x": true,
	}
	for _, d := range []string{imap, local, md} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var want []string
	for rel, mail := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, strings.Repeat("x", len(rel)))
		if mail {
			want = append(want, rel)
		}
	}
	snap, trunc := digestWatch([]string{root})
	if trunc || snap.capped || len(snap.holes) > 0 {
		t.Fatalf("a small tree came back truncated=%v capped=%v holes=%q", trunc, snap.capped, snap.holes)
	}
	var got []string
	for key, sz := range snap.store {
		rel := strings.ReplaceAll(strings.TrimPrefix(key, walkKey(0, ".")+"\x00"), "\x00", "/")
		got = append(got, rel)
		if sz != int64(len(rel)) {
			t.Errorf("%s: size %d, want %d", rel, sz, len(rel))
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("message store\n got: %v\nwant: %v", got, want)
	}
}

// covers: MA-932
// A capped walk's frontier says exactly what it saw: walk keys sort in the order
// WalkDir visits nodes (names that sort around the path separator included), so
// at every cap, a mailbox is in the poll's store if and only if its key is at or
// before the frontier. And past the cap nothing is reported walked.
func TestWalkFrontierMatchesWhatTheWalkSaw(t *testing.T) {
	orig := watchMaxEntries
	defer func() { watchMaxEntries = orig }()

	root := t.TempDir()
	rels := []string{ // all mailboxes (no extension), in folders whose names sort around "/"
		"a/b/c", "a/b0", "a b", "a-b", "a.sbd/x", "a0", "ab", "C", "Z", "_", "b/a", "b/b/a", "b-", "b0", // no two differ only by case (macOS)
	}
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "mail")
	}
	watchMaxEntries = 1 << 20
	full, _ := digestWatch([]string{root})
	if len(full.store) != len(rels) {
		t.Fatalf("uncapped walk listed %d mailboxes, want %d", len(full.store), len(rels))
	}
	sawCap := false
	for limit := 1; limit <= 40; limit++ {
		watchMaxEntries = limit
		s, trunc := digestWatch([]string{root})
		if trunc != s.capped {
			t.Fatalf("cap %d: truncated=%v but capped=%v", limit, trunc, s.capped)
		}
		sawCap = sawCap || s.capped
		for key := range full.store {
			_, in := s.store[key]
			if in != s.walked(key) {
				t.Errorf("cap %d: %q in the store=%v but walked=%v (frontier %q)", limit, key, in, s.walked(key), s.frontier)
			}
		}
	}
	if !sawCap {
		t.Fatal("no cap was ever hit: the test proves nothing")
	}
}

// covers: MA-932
// A folder the walk cannot read is a hole: nothing under it is reported walked,
// so a mailbox first seen there next poll is not new mail; the rest of the tree
// is still walked. Positive twin first: the readable tree has no hole.
func TestWalkHoleForAnUnreadableFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a POSIX mode that denies the owner (not Windows, not root)")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "host", "INBOX.sbd")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, "Work"), "mail")
	writeFile(t, filepath.Join(root, "host", "INBOX"), "mail")
	if s, _ := digestWatch([]string{root}); len(s.holes) != 0 || len(s.store) != 2 {
		t.Fatalf("twin: readable tree gave holes %q and %d mailboxes, want none and 2", s.holes, len(s.store))
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(locked, 0o700) }()
	s, _ := digestWatch([]string{root})
	if s.walked(k("host/INBOX.sbd/Work")) {
		t.Errorf("a mailbox inside an unreadable folder is reported walked (holes %q)", s.holes)
	}
	if _, ok := s.store[k("host/INBOX")]; !ok || !s.walked(k("host/INBOX")) {
		t.Errorf("the readable mailbox beside the hole was not walked: %v", s.store)
	}
}

// covers: MA-932
// The gate on real files, through the real digest: an index touch, a filter-file
// write, a compaction (nstmp written, then renamed over the smaller mailbox), a
// folder rename and a half-written maildir message leave it shut; mail appended to the mailbox, and
// a maildir message moved from tmp into cur, open it. Positive twin first.
func TestFirstChangeGateOnRealFiles(t *testing.T) {
	type step func(t *testing.T, dir string)
	appendTo := func(rel, s string) step {
		return func(t *testing.T, dir string) {
			t.Helper()
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
		return func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Rename(filepath.Join(dir, filepath.FromSlash(from)), filepath.Join(dir, filepath.FromSlash(to))); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(steps ...step) bool {
		dir := t.TempDir()
		for rel, body := range map[string]string{
			"h/INBOX": strings.Repeat("m", 400), "h/INBOX.msf": "idx", "h/msgFilterRules.dat": "r",
		} {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, p, body)
		}
		if err := os.MkdirAll(filepath.Join(dir, "md", "INBOX", "cur"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "md", "INBOX", "tmp"), 0o700); err != nil {
			t.Fatal(err)
		}
		q := newQuietTracker(gateBase, gateCfg)
		s0, _ := digestWatch([]string{dir})
		q.observe(s0, gateBase)
		for i, st := range steps {
			st(t, dir)
			s, _ := digestWatch([]string{dir})
			q.observe(s, gateBase.Add(time.Duration(10*(i+1))*time.Second))
		}
		return q.changed
	}
	if !run(appendTo("h/INBOX", "new mail")) {
		t.Fatal("positive twin: mail appended to the mailbox did not open the gate")
	}
	if !run(appendTo("md/INBOX/tmp/1.M1", "msg"), rename("md/INBOX/tmp/1.M1", "md/INBOX/cur/1.M1")) {
		t.Fatal("positive twin: a maildir message moved into cur did not open the gate")
	}
	if run(appendTo("h/INBOX.msf", "touch")) {
		t.Error("an index (.msf) write opened the gate")
	}
	if run(appendTo("h/msgFilterRules.dat", "x")) {
		t.Error("a filter-rules write opened the gate")
	}
	if run(appendTo("h/nstmp", strings.Repeat("m", 200)), rename("h/nstmp", "h/INBOX")) {
		t.Error("a compaction (nstmp written, renamed over the mailbox) opened the gate")
	}
	if run(func(t *testing.T, dir string) {
		rename("h/INBOX", "h/Old")(t, dir)
		rename("h/INBOX.msf", "h/Old.msf")(t, dir)
	}) {
		t.Error("a folder renamed (its mailbox and index moved, same size) opened the gate")
	}
	if run(appendTo("h/Lists.golang", "old mail"), appendTo("h/Lists.golang.msf", "rebuilt index")) {
		t.Error("a dotted mailbox whose index was recreated (no mail arriving) opened the gate")
	}
	if !run(appendTo("h/Lists.golang", "old mail"), func(t *testing.T, dir string) {
		appendTo("h/Lists.golang.msf", "index")(t, dir)
		appendTo("h/Lists.golang", "new mail")(t, dir)
	}) {
		t.Error("twin: a dotted mailbox that grew as its index came back did not open the gate")
	}
	if run(appendTo("md/INBOX/tmp/2", "half")) {
		t.Error("a half-written maildir message in tmp opened the gate")
	}
}

// covers: MA-932
// Through the session, on the scripted fake, both modes: an index touch soon
// after the start no longer ends the wait as synced before the mail comes; the
// session waits for the mail and ends ok after it, and a session that sees only
// the index touch waits to the maximum and ends ok-noactivity. Positive twin
// first: mail with no index touch ends ok, as before.
func TestIndexTouchDoesNotEndTheWait(t *testing.T) {
	for _, attended := range []bool{false, true} {
		run := func(indexAt, mailAt time.Time) sessionResult {
			f := syncingFake()
			f.indexTouchAt, f.changeAt = indexAt, mailAt
			d := syncingDeps(f, filepath.Join(t.TempDir(), "s.json"))
			if attended {
				f.idle = 0
				d = attendedDeps(f, d.statePath)
			}
			return runSession(context.Background(), d)
		}
		if r := run(time.Time{}, as1aBase.Add(24*time.Second)); r.outcome != OutcomeOK {
			t.Fatalf("attended=%v twin: mail with no index touch ended %q, want ok", attended, r.outcome.Name)
		}
		r := run(as1aBase.Add(30*time.Second), as1aBase.Add(300*time.Second))
		if r.outcome != OutcomeOK || r.waited < 290*time.Second {
			t.Errorf("attended=%v: an index touch at +30s, mail at +300s ended %q after %v; want ok after the mail (>= 290s)", attended, r.outcome.Name, r.waited)
		}
		if r := run(as1aBase.Add(30*time.Second), as1aBase.Add(24*time.Hour)); r.outcome != OutcomeNoActivity {
			t.Errorf("attended=%v: an index touch and no mail ended %q, want ok-noactivity", attended, r.outcome.Name)
		}
	}
}
