package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// The friction re-run's fixes for an archive-sourced export (EW-E4, docs/review-extraction-workspace-archive-friction.md).

// historyCard is the card of run on Export activity, as the page reads it after a restart.
func historyCard(t *testing.T, e *CopyExportEnv, run string) CopyRunInfo {
	t.Helper()
	hist, err := e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range hist {
		if c.Run == run {
			return c
		}
	}
	t.Fatalf("the run %s is not in the history", run)
	return CopyRunInfo{}
}

// linkInPlaceOf puts a symlink where a message's original was: the gate refuses it for good (terminal).
func linkInPlaceOf(t *testing.T, a *ceArchive, slug, target string) {
	t.Helper()
	_, eml, _ := a.files(slug)
	_, other, _ := a.files(target)
	os.Remove(eml)
	if err := os.Symlink(other, eml); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// rewrittenByACapture changes a message's original and its recorded fixity together, as a capture does:
// the run lists it as changed in the archive during export (retryable).
func rewrittenByACapture(a *ceArchive, slug string) {
	_, eml, _ := a.files(slug)
	b := append(a.read(eml), []byte("X-Later: a re-capture\r\n")...)
	os.WriteFile(eml, b, 0o644)
	a.setRec(slug, func(r *state.Record) { r.Fixity.EML = digestOf(b) })
}

// covers: MA-437, MA-420, R29, S52
// F3: "Export failed messages again" is offered when a retryable failure is left, and only then. A partial
// archive export whose failures are all terminal (a link where the original should be, a file with several
// hard links) offers Open folder and Check this export and no retry, and a click that would be refused says
// what happened (the failures cannot be tried again), why (they failed for a reason that trying again does not
// change) and what to do (the reasons are under "Which ones?"; restore the file, then export the folder anew).
// The positive twin first: one retryable failure keeps the offer, and the retry is exactly that message.
func TestArchiveExportOffersRetryOnlyWhileARetryableFailureIsLeft(t *testing.T) {
	run := func(t *testing.T, damage func(a *ceArchive), slugs ...string) (*CopyExportEnv, *ceArchive, *CopyResult) {
		a := buildCEArchive(t)
		e := newCEEnv(t)
		s := a.open()
		var keys []string
		for _, slug := range slugs {
			keys = append(keys, a.key(slug))
		}
		p := a.plan(e, s, keys)
		damage(a) // after the preview, as a capture or bit-rot does
		res := a.run(e, p, s)
		if res.Status != CopyStatusPartial || res.Counts.Failed == 0 {
			t.Fatalf("the run: %+v, want partial with failures", res)
		}
		return e, a, res
	}

	t.Run("a retryable failure keeps the offer", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) { rewrittenByACapture(a, "hello") }, "hello", "second")
		if c := historyCard(t, e, res.RunID); !hasAction(c.Actions, ActExportFailed) {
			t.Fatalf("a retryable failure: the card offers %v, want Export failed messages again", c.Actions)
		}
		in, err := e.RetryInput(res.RunID, "failed", "")
		if err != nil || len(in.Messages) != 1 || in.Messages[0].SourceID != a.key("hello") {
			t.Fatalf("the retry of the retryable failure: %v, %v", in, err)
		}
	})

	t.Run("only terminal failures: no offer, and the click says why", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) { linkInPlaceOf(t, a, "hello", "second") }, "hello", "second")
		if f := failureListed(manifestOf(t, res.Root), a.key("hello")); f == nil || f["class"] != "terminal" {
			t.Fatalf("hello's failure: %v, want terminal", f)
		}
		c := historyCard(t, e, res.RunID)
		if hasAction(c.Actions, ActExportFailed) || !hasAction(c.Actions, ActOpenFolder) || !hasAction(c.Actions, ActCheckThis) {
			t.Errorf("the card of a run whose failures are all terminal offers %v: want Open folder and Check this export, no retry", c.Actions)
		}
		before := treeDigest(t, filepath.Dir(res.Root))
		_, err := e.RetryInput(res.RunID, "failed", "")
		refusedWith(t, err, "R-h", []string{"1 message that failed", "can't be tried again", "Which ones?", "restore"})
		if treeDigest(t, filepath.Dir(res.Root)) != before {
			t.Error("the refused retry wrote into the destination")
		}
	})

	t.Run("a terminal and a retryable failure: the offer stays, for the retryable one only", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) {
			linkInPlaceOf(t, a, "hello", "client-mail")
			rewrittenByACapture(a, "second")
		}, "hello", "second", "client-mail")
		if c := historyCard(t, e, res.RunID); !hasAction(c.Actions, ActExportFailed) {
			t.Fatalf("the card offers %v, want Export failed messages again", c.Actions)
		}
		in, err := e.RetryInput(res.RunID, "failed", "")
		if err != nil || len(in.Messages) != 1 || in.Messages[0].SourceID != a.key("second") {
			t.Fatalf("the retry: %v, %v, want only the retryable message", in, err)
		}
	})
}

// covers: MA-512, MA-420, R12, R29
// F12: an archive that stops being readable is described as one this account cannot READ, never as
// "write-protected" (MailArchive only reads it): the R-o2 stop says what happened (the archive is no longer
// available, with the system's own text after the reason), that nothing in the archive is damaged, and how to
// carry on (reconnect it, or make it readable again, then Export the remaining messages); and one file that
// cannot be read is listed with the read wording. The positive twin: the same export of a readable archive
// completes.
func TestArchiveReadFailuresAreWordedAsReads(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("an unreadable folder is modelled with permission bits, for a user who is not root")
	}
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	if res := a.run(e, a.plan(e, s, allKeys(s)), s); res.Status != CopyStatusComplete {
		t.Fatalf("the positive twin, a readable archive: %+v", res)
	}

	// one file the account may not read, the archive itself fine: a listed failure, in read words
	a = buildCEArchive(t)
	e = newCEEnv(t)
	s = a.open()
	p := a.plan(e, s, allKeys(s))
	_, eml, _ := a.files("hello")
	os.Chmod(eml, 0)
	t.Cleanup(func() { os.Chmod(eml, 0o644) })
	res := a.run(e, p, s)
	f := failureListed(manifestOf(t, res.Root), a.key("hello"))
	if f == nil || f["class"] != "retryable" {
		t.Fatalf("the unreadable file: %v, want a retryable failure", f)
	}
	reason := f["reason"].(string)
	if !strings.Contains(reason, "isn't allowed to read it") || strings.Contains(reason, "write") {
		t.Errorf("the unreadable file's reason: %q, want read words and no write words", reason)
	}
	os.Chmod(eml, 0o644)

	// the whole archive stops answering as unreadable, mid-run
	a = buildCEArchive(t)
	e = newCEEnv(t)
	s = a.open()
	p = a.plan(e, s, allKeys(s))
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W6:2" {
			os.Chmod(a.dir, 0)
		}
	}}
	t.Cleanup(func() { os.Chmod(a.dir, 0o755) })
	res = a.run(e, p, s)
	os.Chmod(a.dir, 0o755)
	if res.Stop == nil || res.Stop.RID != "R-o2" {
		t.Fatalf("the run: %+v, stop %+v; want R-o2", res.Counts, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1),
		assure.Names(a.dir, "no longer available", "isn't allowed to read it", "nothing in the archive is damaged", "Export the remaining messages"),
		assure.Forbid("write-protected", "allowed to write"))
}

// removeFilesOf deletes every file the archive holds for a planned message: its page, original and zip.
func removeFilesOf(m CopyMessage, a *ceArchive) {
	page, eml, zip := archiveSiblings(m.Rel)
	for _, rel := range []string{page, eml, zip} {
		os.Remove(a.abs(rel))
	}
}

// covers: MA-437, MA-427, MA-420, R29, R5
// F6: messages whose files vanished in the moments before the archive was seen to be gone (a share that fails
// file by file, a folder being deleted under the run) are not "removed after you selected it" for good: when
// the run stops with R-o2 they become retryable, with the reason that the archive went away, so "Export the
// remaining messages" takes them and, with the archive back, each is resolved against it as it is then (a
// record whose files are really gone is left out by the redaction belt, never exported). The positive twin: a
// file removed while the archive stays reachable remains terminal and is not offered again.
func TestArchiveStopLetsEarlierRemovedFailuresBeRetried(t *testing.T) {
	// the twin: one file removed, the archive reachable
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	p := a.plan(e, s, allKeys(s))
	removeFilesOf(p.Messages[1], a)
	res := a.run(e, p, s)
	if f := failureListed(manifestOf(t, res.Root), p.Messages[1].SourceID); f == nil || f["class"] != "terminal" || !strings.Contains(f["reason"].(string), "removed from the archive after you selected it") {
		t.Fatalf("a file removed with the archive reachable: %v, want terminal", f)
	}
	if rest, err := e.RunRemaining(res.RunID); err != nil || len(rest) != 0 {
		t.Fatalf("the remaining messages of a run whose one failure is a removed file: %d (%v), want none", len(rest), err)
	}

	// the case: two files vanish, then the whole archive does
	a = buildCEArchive(t)
	e = newCEEnv(t)
	s = a.open()
	p = a.plan(e, s, allKeys(s))
	stash := filepath.Join(tmpDir(t), "stash")
	os.MkdirAll(stash, 0o755)
	var moved []string
	e.faults = &copyFaults{hook: func(step string) {
		switch step {
		case "W6:1":
			removeFilesOf(p.Messages[1], a)
			removeFilesOf(p.Messages[2], a)
		case "W6:3":
			if moved != nil {
				return
			}
			ents, _ := os.ReadDir(a.dir)
			for _, ent := range ents {
				os.Rename(filepath.Join(a.dir, ent.Name()), filepath.Join(stash, ent.Name()))
				moved = append(moved, ent.Name())
			}
		}
	}}
	res = a.run(e, p, s)
	if res.Stop == nil || res.Stop.RID != "R-o2" || res.Counts.Exported != 1 || res.Counts.Failed != 3 || res.Counts.NotAttempted != 2 {
		t.Fatalf("the run: %+v, stop %+v; want R-o2 after 1 exported, the 2 vanished files and the message in flight failed, 2 never tried", res.Counts, res.Stop)
	}
	man := manifestOf(t, res.Root)
	for _, i := range []int{1, 2} {
		f := failureListed(man, p.Messages[i].SourceID)
		if f == nil || f["class"] != "retryable" || !strings.Contains(f["reason"].(string), "archive went away") {
			t.Errorf("message %d, which failed before the archive was seen to be gone: %v, want retryable, with the archive going away as the reason", i+1, f)
		}
	}
	rest, err := e.RunRemaining(res.RunID)
	if err != nil || len(rest) != 5 {
		t.Fatalf("Export the remaining messages: %d (%v), want 5 (2 not tried, the 2 that failed before the stop, the one in flight last)", len(rest), err)
	}
	if rest[4].SourceID != p.Messages[3].SourceID {
		t.Errorf("the message in flight is not last: %v", rest[4].SourceID)
	}
	if c := historyCard(t, e, res.RunID); !hasAction(c.Actions, ActExportRemaining) {
		t.Errorf("the card offers %v, want Export the remaining messages", c.Actions)
	}

	// the archive is back, and one of those two really was removed: the belt leaves it out, the rest is exported
	for _, name := range moved {
		os.Rename(filepath.Join(stash, name), filepath.Join(a.dir, name))
	}
	s2 := a.open()
	var keys []string
	for _, m := range rest {
		keys = append(keys, m.SourceID)
	}
	got, rerr := s2.Messages(t.Context(), keys, nil, nil)
	if rerr != nil || got.Skipped != 2 || len(got.Messages) != 3 {
		t.Errorf("the retry resolved against the archive as it is now: %d messages, %d left out (%v), want 3 and the 2 whose files are gone left out", len(got.Messages), got.Skipped, rerr)
	}
}

// covers: MA-420, MA-435, R20, R29, S52
// F2: the preview runs the archive gate on what it counts. A message whose file is a link, or has more than one
// hard link (an archive restored from a hard-link backup, a store folder linked to another disk), is counted as
// one the run will not copy, by the reason the run will list it under, and is not counted among those that "have
// an original"; the run then fails exactly those messages and no others. The positive twin: the plain archive's
// preview counts none.
func TestArchivePreviewRunsTheGateOnWhatItCounts(t *testing.T) {
	// the twin
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	p := a.plan(e, s, allKeys(s))
	if c := p.Archive; c == nil || c.Blocked != 0 || c.WithOriginal != 3 || c.WithoutOriginal != 3 {
		t.Fatalf("the plain archive's counts: %+v, want none blocked, 3 with an original and 3 without", c)
	}

	t.Run("a hard-linked original", func(t *testing.T) {
		a := buildCEArchive(t)
		e := newCEEnv(t)
		_, eml, _ := a.files("hello")
		if err := os.Link(eml, eml+".also"); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		s := a.open()
		p := a.plan(e, s, allKeys(s))
		if c := p.Archive; c.Blocked != 1 || c.BlockedWhy["hardlink"] != 1 || c.WithOriginal != 2 || c.WithoutOriginal != 3 {
			t.Fatalf("the counts: %+v, want 1 blocked for a hard link, 2 with an original, 3 without", c)
		}
		res := a.run(e, p, s)
		if res.Counts.Failed != 1 || failureListed(manifestOf(t, res.Root), a.key("hello")) == nil {
			t.Errorf("the run: %+v, want exactly the message the preview counted as blocked to fail", res.Counts)
		}
	})

	t.Run("a store folder linked to another disk", func(t *testing.T) {
		a := buildCEArchive(t)
		e := newCEEnv(t)
		store := filepath.Join(a.dir, "alice")
		elsewhere := filepath.Join(tmpDir(t), "alice")
		if err := os.Rename(store, elsewhere); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, store); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		s := a.open()
		p := a.plan(e, s, allKeys(s))
		c := p.Archive
		if c.Blocked != 3 || c.BlockedWhy["link"] != 3 || c.WithOriginal != 0 || c.WithoutOriginal != 3 {
			t.Fatalf("the counts: %+v, want alice's 3 blocked for a link and not counted as having an original, bob's 3 without", c)
		}
		res := a.run(e, p, s)
		if res.Counts.Failed != 3 || res.Counts.Exported != 3 {
			t.Errorf("the run: %+v, want the 3 the preview counted as blocked to fail and bob's 3 exported", res.Counts)
		}
	})
}

// covers: MA-420, MA-510, R12, R20, R29
// F17: the failure reasons of an archive export name the button the owner has ("Maintenance → Check integrity"),
// never the command-line verb ("run Verify"), and each reason that cannot change on its own gives a next step: a
// damaged file (restore it, or ask for a fresh copy of a read-only folder, which cannot be checked from here), a
// removed one (restore it if that was not on purpose), a link or a file with several hard links (copy the archive
// into a plain folder). The words the existing assertions pin stay. The positive twin: a retryable reason keeps its
// own sentence.
func TestArchiveFailureReasonsSayWhatToDoNext(t *testing.T) {
	type damage struct {
		name   string
		mutate func(a *ceArchive)
		reason string   // what is pinned already, and what the reason adds
		self   []string // for the owner's own archive
		other  []string // for another archive folder, which is read-only
	}
	cases := []damage{
		{"a damaged file", func(a *ceArchive) {
			_, eml, _ := a.files("hello")
			b := a.read(eml)
			b[len(b)-3] ^= 0x20
			os.WriteFile(eml, b, 0o644)
		}, "doesn't match the checksum the archive recorded", []string{"restore the file from a backup of the archive", "Maintenance → Check integrity"},
			[]string{"this folder is read-only", "ask whoever gave you the archive for a fresh copy"}},
		{"a removed file", func(a *ceArchive) {
			_, eml, _ := a.files("hello")
			os.Remove(eml)
		}, "removed from the archive after you selected it", []string{"if that was not on purpose", "restore the file from a backup of the archive"},
			[]string{"if that was not on purpose", "ask whoever gave you the archive for a complete copy"}},
		{"a link", func(a *ceArchive) { linkInPlaceOf(t, a, "hello", "second") }, "a link stands in the archive",
			[]string{"copy the archive into a plain folder that holds real files, not links"}, []string{"copy the archive into a plain folder that holds real files, not links"}},
		{"a hard link", func(a *ceArchive) {
			_, eml, _ := a.files("hello")
			if err := os.Link(eml, eml+".also"); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}
		}, "more than one hard link", []string{"copy the archive into a plain folder that holds real files, not links"}, []string{"copy the archive into a plain folder that holds real files, not links"}},
	}
	for _, other := range []bool{false, true} {
		for _, c := range cases {
			name := c.name + ", the owner's archive"
			if other {
				name = c.name + ", another archive folder"
			}
			t.Run(name, func(t *testing.T) {
				a := buildCEArchive(t)
				e := newCEEnv(t)
				s, err := OpenArchive(a.dir, ArchiveOptions{Other: other})
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				hello := a.key("hello")
				p := a.plan(e, s, []string{hello, a.key("second")})
				c.mutate(a) // after the preview, as a capture, a redaction or bit-rot does
				res := a.run(e, p, s)
				f := failureListed(manifestOf(t, res.Root), hello)
				if f == nil {
					t.Fatalf("hello is not listed as failed: %v", manifestOf(t, res.Root)["failures"])
				}
				reason := f["reason"].(string)
				want := append([]string{c.reason}, c.self...)
				if other {
					want = append([]string{c.reason}, c.other...)
				}
				for _, w := range want {
					if !strings.Contains(reason, w) {
						t.Errorf("the reason %q lacks %q", reason, w)
					}
				}
				if strings.Contains(reason, "Verify") {
					t.Errorf("the reason names the command-line verb, not the button: %q", reason)
				}
				if other && strings.Contains(reason, "Check integrity") {
					t.Errorf("a read-only folder is told to run a check that cannot run on it: %q", reason)
				}
			})
		}
	}

	// the twin: a reason a retry can change keeps its own sentence, with no next step to add
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	p := a.plan(e, s, []string{a.key("hello"), a.key("second")})
	rewrittenByACapture(a, "hello")
	res := a.run(e, p, s)
	if f := failureListed(manifestOf(t, res.Root), a.key("hello")); f == nil || !strings.Contains(f["reason"].(string), "preview again for the new copy") {
		t.Errorf("the retryable reason: %v", f)
	}
	// the manifest's note for an original with no checksum names the button too
	a = buildCEArchive(t)
	e = newCEEnv(t)
	a.setRec("hello", func(r *state.Record) { r.Fixity.EML = nil })
	s = a.open()
	res = a.run(e, a.plan(e, s, []string{a.key("hello")}), s)
	if n := notesOf(entryOf(t, manifestOf(t, res.Root), a.key("hello"))); !strings.Contains(n, "Check integrity") || strings.Contains(n, "run Verify") {
		t.Errorf("the note for an original with no recorded checksum: %q", n)
	}
}
