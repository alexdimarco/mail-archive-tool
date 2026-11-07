package tbengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1141, R40, S68
// R6-03 (settled 2026-11-07, the lead's call): a restore that is still writing — its
// restore lock held — is never called unfinished. Read from inside the write, as Desktop's
// page load, `status` and uninstall's plan read it, it is "running now", naming where it
// writes, and never the advice to delete its folder; uninstall's plan does not say nothing
// will clean it up. Positive twin: the same restore once finished is not named at all.
func TestARunningRestoreIsNotCalledUnfinished(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	var mid, midUninstall []tbrestore.Leftover
	withFS(t, atFirstFolder(func() error {
		log, _ := restoresLog()
		mid, _ = tbrestore.Leftovers(log, false)
		midUninstall, _ = tbrestore.Leftovers(log, true)
		return nil
	}))
	r := mustRun(t, p, Start{})
	assure.Reached(t, r.Done(), "the run finishes: "+r.Words)
	for _, set := range [][]tbrestore.Leftover{mid, midUninstall} {
		assure.Reached(t, len(set) == 1 && set[0].Kind == "running", fmt.Sprintf("mid-run, one leftover, running: %+v", set))
		w := set[0].Words
		assure.Reached(t, strings.Contains(w, "running now") && strings.Contains(w, pr.local) && !strings.Contains(w, "delete") && !strings.Contains(w, "did not finish") && !strings.Contains(w, "Nothing will clean it up"),
			"the words say it is running, where, and never to delete it: "+w)
	}
	log, _ := restoresLog()
	after, _ := tbrestore.Leftovers(log, false)
	assure.Reached(t, len(after) == 0, fmt.Sprintf("once finished it is not named: %+v", after))
}

// covers: MA-1122, R40, S68
// R6-10 (settled 2026-11-07): Cancel (Ctrl-C, Shut down) during the post-write check,
// which reads every message back, is a stop with the one cleanup — outcome stopped, the
// stopped words, nothing left in Local Folders — not a restore that finishes anyway; and
// Checking is told when the check begins. Positive twin: uncancelled, Checking is told
// once and it finishes.
func TestCancelDuringThePostWriteCheckStops(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	checks := 0
	assure.Reached(t, mustRun(t, plan(t, Request{Out: out, Profile: pr.choice()}), Start{Checking: func() { checks++ }}).Done() && checks == 1, fmt.Sprintf("positive twin: finished, Checking told %d time(s)", checks))

	pr2 := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr2.choice()})
	before := filesOf(t, pr2.dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	withFS(t, func(op, name string) error {
		if op == "open" && strings.Contains(name, ".sbd/") {
			cancel() // the check's first read-back
		}
		return nil
	})
	r, err := Run(ctx, p, Start{})
	if err != nil {
		t.Fatal(err)
	}
	assure.Refused(t, map[bool]int{true: 1}[r.Stopped()], r.Words, assure.Code(1), assure.Names("The restore was stopped", "nothing was added", "removed"),
		assure.NoSideEffect(func() bool { return filesOf(t, pr2.dir) == before }))
	assure.Reached(t, r.ExitCode() == 1 && !r.Done(), fmt.Sprintf("not counted as restored: exit %d", r.ExitCode()))
}

// covers: MA-1125, R40, S66
// R6-11 (settled 2026-11-07): the store kind is checked again at Start. A plan made while
// Local Folders was one file per folder, then Thunderbird's setting switched to maildir:
// Start refuses W-CHANGED naming it, nothing written. Positive twin: unswitched, it restores.
func TestTheStoreKindIsCheckedAgainAtStart(t *testing.T) {
	out := plainArchive(t, true)
	twin := newProfile(t, "")
	assure.Reached(t, mustRun(t, plan(t, Request{Out: out, Profile: twin.choice()}), Start{}).Done(), "positive twin: restores")

	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	prefs := filepath.Join(pr.dir, "prefs.js")
	b, _ := os.ReadFile(prefs)
	write(t, prefs, strings.Replace(string(b), "@mozilla.org/msgstore/berkeleystore;1", "@mozilla.org/msgstore/maildirstore;1", 1))
	before := snapshot(t, pr.dir)
	_, err := Run(context.Background(), p, Start{})
	refusedAs(t, err, "W-CHANGED", func() bool { return snapshot(t, pr.dir) == before }, "maildir", pr.local)
}

// covers: MA-1120, R40
// R6-16 (settled 2026-11-07): the parent file's name taken after the re-check — at each
// of Thunderbird's lock-take steps — moves the restore to the next " (n)" before anything
// is written, never to W-TAKEN after the whole write; the other program's file is left as
// it was. A name taken at the re-check itself is W-CHANGED, nothing written.
func TestAParentNameTakenAfterTheRecheckIsFoundBeforeTheWrite(t *testing.T) {
	out := plainArchive(t, true)
	for at := 1; at <= 4; at++ {
		t.Run(fmt.Sprintf("at-%d", at), func(t *testing.T) {
			pr := newProfile(t, "")
			p := plan(t, Request{Out: out, Profile: pr.choice()})
			taken := filepath.Join(pr.local, "Restored — Work mail")
			n := 0
			undo := tbrestore.SetProcessProbeForTest(func() (int, bool) {
				if n++; n == at {
					write(t, taken, "another program's file\n")
				}
				return 0, true
			})
			defer undo()
			r, err := Run(context.Background(), p, Start{})
			if at == 1 { // the re-check's own look
				refusedAs(t, err, "W-CHANGED", nil, "is now taken")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(taken)
			assure.Reached(t, r.Done() && r.Write.Top == "Restored — Work mail (2)" && string(got) == "another program's file\n",
				fmt.Sprintf("taken at step %d: restored as (2), the other file kept: %s (%s)", at, r.Write.Outcome, r.Words))
		})
	}
	// Between the writer's own look and its Mkdir (the narrowest window).
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	taken := filepath.Join(pr.local, "Restored — Work mail")
	done := false
	withFS(t, func(op, name string) error {
		if op == "mkdir" && name == "Restored — Work mail.sbd" && !done {
			done = true
			write(t, taken, "planted\n")
		}
		return nil
	})
	r := mustRun(t, p, Start{})
	_, gone := os.Lstat(filepath.Join(pr.local, "Restored — Work mail.sbd"))
	assure.Reached(t, r.Done() && r.Write.Top == "Restored — Work mail (2)" && os.IsNotExist(gone),
		fmt.Sprintf("taken at the Mkdir: the empty folder just made removed, restored as (2): %s %v", r.Words, gone))
}
