package tbrestore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-1123, R40, S68
// Settling an earlier restore at Start, row by row of design §8's foldcheck2 table as
// split by the foldcheck3 note. Each row first runs the read-only prediction (Check and
// -plan): it writes nothing — restores.log's bytes and Local Folders the same before
// and after — and says what Start will do; then Start settles it, removing only what
// that row says, and the new restore runs to "ended" under its " (n)" name (N-g).
func TestRestoreSettleEveryRow(t *testing.T) {
	in, msgs := sampleFolders()
	type row struct {
		crashAt              string            // the step a crash comes just before
		then                 func(fx *fixture) // what happened after the crash
		taken                bool              // the restore wrote a "taken" line before the crash
		wantCase, wantAction string            // how Start settles it
		removes              bool              // Local Folders is as it was afterwards
		words                string            // in the words, when any
	}
	parent := func(kind string) func(fx *fixture) {
		return func(fx *fixture) {
			p := filepath.Join(fx.lf.Dir, "Restored — Work mail")
			switch kind {
			case "file":
				writeFile(t, p, "")
			case "folder":
				_ = os.Mkdir(p, 0o700)
			case "link":
				_ = os.Symlink("elsewhere", p)
			}
		}
	}
	rows := map[string]row{
		"after started, before Mkdir":            {crashAt: "mkdir Restored — Work mail.sbd", wantCase: "1", wantAction: EvAbandoned, removes: true},
		"after the marker, before the made-list": {crashAt: "create Restored — Work mail.sbd/" + MadeListName, wantCase: "3b", wantAction: EvCleaned, removes: true},
		"while writing, clean":                   {crashAt: "create Restored — Work mail.sbd/Sent", wantCase: "3b", wantAction: EvCleaned, removes: true},
		"after checked, before the parent":       {crashAt: "create Restored — Work mail", wantCase: "3b", wantAction: EvCleaned, removes: true},
		"while writing, then a parent file":      {crashAt: "create Restored — Work mail.sbd/Sent", then: parent("file"), wantCase: "3a", wantAction: EvAbandoned, words: "stopped before it finished"},
		"after checked, then a parent folder":    {crashAt: "create Restored — Work mail", then: parent("folder"), wantCase: "3a", wantAction: EvAbandoned, words: "may show"},
		"after checked, then a regular parent":   {crashAt: "create Restored — Work mail", then: parent("file"), wantCase: "2", wantAction: EvEnded},
		"while writing, something unlisted": {crashAt: "create Restored — Work mail.sbd/Sent", then: func(fx *fixture) {
			writeFile(t, filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", "Inbox.msf"), "x")
		}, wantCase: "3b", wantAction: EvAbandoned, words: "did not make"},
		"after taken, the entry there":    {crashAt: "create Restored — Work mail", then: parent("file"), taken: true, wantCase: "0b", wantAction: EvAbandoned, words: "removed nothing"},
		"after taken, the entry gone now": {crashAt: "create Restored — Work mail", taken: true, wantCase: "0b", wantAction: EvAbandoned, words: "removed nothing"},
		"after the parent, before ended":  {crashAt: "syncdir .", wantCase: "2", wantAction: EvEnded},
	}
	if runtime.GOOS != "windows" {
		rows["after checked, then a parent link"] = row{crashAt: "create Restored — Work mail", then: parent("link"), wantCase: "3a", wantAction: EvAbandoned, words: "may show"}
	}
	for name, r := range rows {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, in, msgs)
			before := fx.local()
			id, crashed := fx.writeCrashing(stepOf(t, in, msgs, r.crashAt))
			if !crashed {
				t.Fatal("no crash")
			}
			if r.taken {
				if err := fx.log.Append(LogLine{Event: EvTaken, ID: id}); err != nil {
					t.Fatal(err)
				}
			}
			if r.then != nil {
				r.then(fx)
			}
			// Check / -plan: a read, nothing written.
			logBefore, lfBefore := fx.logBytes(), fx.local()
			p := fx.settle(true)
			if len(p) != 1 || p[0].Case != r.wantCase || fx.logBytes() != logBefore || fx.local() != lfBefore {
				t.Fatalf("the prediction: %+v (it must write nothing)", p)
			}
			s := fx.settle(false)
			if len(s) != 1 || s[0].Case != r.wantCase || s[0].Action != r.wantAction {
				t.Fatalf("settled %+v, want case %s %s", s, r.wantCase, r.wantAction)
			}
			if lastEv(fx.events(id)) != r.wantAction {
				t.Fatalf("the log: %v", fx.events(id))
			}
			switch {
			case r.removes && fx.local() != before:
				t.Fatal("not as it was")
			case !r.removes && fx.local() != lfBefore:
				t.Fatal("this row removes nothing")
			}
			if r.words != "" && !strings.Contains(s[0].Words, r.words) {
				t.Fatalf("words: %q, want %q", s[0].Words, r.words)
			}
			fx.top = fx.freeTop()
			res, _ := fx.write(context.Background(), nil)
			if res.Outcome != OutDone {
				t.Fatalf("the new restore: %s %v", res.Outcome, res.Err)
			}
		})
	}
}

// covers: MA-1123, R40, S68
// Step 0 (foldcheck2 N-f, foldcheck3 S9): an earlier restore whose recorded Local
// Folders is no longer this profile's, or whose drive is not connected, is left alone —
// nothing removed, no line, still open, named with W-ELSEWHERE ("may not have
// finished") — and the new restore goes ahead; once the person has deleted it from a
// reachable old folder, the record is closed ("abandoned") and not named again.
func TestRestoreSettleElsewhere(t *testing.T) {
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	id, crashed := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
	if !crashed {
		t.Fatal("no crash")
	}
	oldLF := fx.lf
	// The profile's Local Folders is now another folder.
	other := filepath.Join(t.TempDir(), "New Local Folders")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	id2, _ := DirIdentity(other)
	newLF := LocalFolders{Dir: canonical(other), Identity: id2, ServerKey: "server1"}
	root2, err := OpenLocalRoot(newLF)
	if err != nil {
		t.Fatal(err)
	}
	defer root2.Close()
	oldBefore := snapshot(t, oldLF.Dir)
	settle := func() []Settled {
		s, err := Settle(SettleInput{Log: fx.log, Local: newLF, FS: newRootFS(root2), ProfileDir: fx.profile})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := settle()
	if len(s) != 1 || s[0].Case != "0" || s[0].Action != "" || snapshot(t, oldLF.Dir) != oldBefore {
		t.Fatalf("elsewhere: %+v", s)
	}
	mustContain(t, s[0].Words, "may not have finished", "no longer this profile's Local Folders", "delete the folder 'Restored — Work mail.sbd'")
	if len(Open(Restores(mustRead(t, fx.log)), ProfileKeyOf(fx.profile))) != 1 {
		t.Fatal("it stays open")
	}
	// The new restore goes ahead into the new Local Folders.
	fx.root.Close() // Windows cannot move a folder an open root holds
	fx.lf, fx.root = newLF, root2
	fx.top = fx.freeTop()
	if res, _ := fx.write(context.Background(), nil); res.Outcome != OutDone {
		t.Fatalf("the new restore: %s %v", res.Outcome, res.Err)
	}
	// The drive of the old folder not connected: W-ELSEWHERE's other words.
	gone := filepath.Join(t.TempDir(), "unplugged")
	if err := os.Rename(oldLF.Dir, gone); err != nil {
		t.Fatal(err)
	}
	s = settle()
	if len(s) != 1 || s[0].Case != "0" || !strings.Contains(s[0].Words, "isn't connected") {
		t.Fatalf("drive gone: %+v", s)
	}
	// Plugged in again, and the person deleted the leftover: closed, not named again.
	if err := os.Rename(gone, oldLF.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(oldLF.Dir, "Restored — Work mail.sbd")); err != nil {
		t.Fatal(err)
	}
	s = settle()
	if len(s) != 1 || s[0].Action != EvAbandoned || lastEv(fx.events(id)) != EvAbandoned {
		t.Fatalf("deleted by the person: closed: %+v", s)
	}
	if s := settle(); len(s) != 0 {
		t.Fatalf("not named again: %+v", s)
	}
}

// covers: MA-1123, R40, S68
// Prove-fail guard for foldcheck3 S3: a "taken" line settles as remove-nothing whatever
// is beside the container now. A settle that looked only at the parent name now would
// clean this leftover; this one keeps it.
func TestRestoreSettleTakenLineAlwaysKeeps(t *testing.T) {
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	id, _ := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
	if err := fx.log.Append(LogLine{Event: EvTaken, ID: id}); err != nil {
		t.Fatal(err)
	}
	before := fx.local()
	s := fx.settle(false)
	rc := 0
	if len(s) == 1 && s[0].Case == "0b" && s[0].Action == EvAbandoned {
		rc = 1
	}
	refusedAs(t, errFrom(rc, wordsOf(s)), "W-KEPT", func() bool { return fx.local() == before }, "removed nothing", "Thunderbird does not show")
}

// covers: MA-1123, R40, S68
// A removal blocked at Start (rule 5: a sharing violation past the retry, a permission
// error) stops the cleanup with no line, the restore still open and named with "tries
// again", and the new restore goes ahead; a crash during a cleanup's removals leaves a
// leftover the next Start settles again from step 0 (absent entries passed over) until
// Local Folders is as it was.
func TestRestoreSettleBlockedAndInterrupted(t *testing.T) {
	in, msgs := sampleFolders()
	was := retryFor
	retryFor = 0
	defer func() { retryFor = was }()

	fx := newFixture(t, in, msgs)
	before := fx.local()
	id, _ := fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
	f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
		if op == "remove" && strings.HasSuffix(name, "/Sent") {
			return &os.PathError{Op: "remove", Path: name, Err: os.ErrPermission}
		}
		return nil
	}}
	s, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: f, ProfileDir: fx.profile})
	if err != nil || len(s) != 1 || s[0].Action != "" || s[0].Clean.Kind != CleanBlocked {
		t.Fatalf("blocked: %+v %v", s, err)
	}
	mustContain(t, s[0].Words, "would not let MailArchive remove", "tries again at your next restore")
	if lastEv(fx.events(id)) != EvStarted && lastEv(fx.events(id)) != EvChecked {
		t.Fatalf("no line for a blocked removal: %v", fx.events(id))
	}
	fx.top = fx.freeTop()
	res, _ := fx.write(context.Background(), nil)
	if res.Outcome != OutDone || res.Top != "Restored — Work mail (2)" {
		t.Fatalf("the new restore goes ahead under (2): %s %q", res.Outcome, res.Top)
	}
	newOnes := []string{res.Top, res.Top + ".sbd"}
	if s := fx.settle(false); len(s) != 1 || s[0].Action != EvCleaned || snapshot(t, fx.lf.Dir, newOnes...) != before {
		t.Fatalf("the next Start tries again and cleans: %+v", s)
	}

	fx2 := newFixture(t, in, msgs)
	before2 := fx2.local()
	fx2.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
	n := 0
	f2 := &faultFS{inner: newRootFS(fx2.root), hook: func(op, name string) error {
		if op == "remove" {
			if n++; n == 3 {
				panic(crash{})
			}
		}
		return nil
	}}
	func() {
		defer func() { _ = recover() }()
		_, _ = Settle(SettleInput{Log: fx2.log, Local: fx2.lf, FS: f2, ProfileDir: fx2.profile})
	}()
	if s := fx2.settle(false); len(s) != 1 || s[0].Action != EvCleaned || fx2.local() != before2 {
		t.Fatalf("after a crash during the removals, the next Start finishes the cleanup: %+v", s)
	}
}

func wordsOf(s []Settled) string {
	var w []string
	for _, x := range s {
		w = append(w, x.Words)
	}
	return strings.Join(w, " ")
}
