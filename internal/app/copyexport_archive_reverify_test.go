package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// The fresh-eyes re-verification of the EW-E4 friction fixes
// (docs/review-extraction-workspace-archive-friction.md, "Re-verification"): what it found, fixed test-first.

// covers: MA-512, MA-420, R12, R29
// RV2: the R-o2 stop tells the owner to make the archive readable again only when the reason it gives is about
// permissions. For the usual reason (the drive was disconnected or the folder moved) the sentence is "Reconnect
// it", with no parenthetical about a reason that is not on the page. The positive twin: a reason that is about
// permissions says so and names the way out.
func TestArchiveStopMentionsPermissionsOnlyWhenThatIsTheReason(t *testing.T) {
	stopAt := func(t *testing.T, away func(a *ceArchive)) *CopyResult {
		t.Helper()
		a := buildCEArchive(t)
		e := newCEEnv(t)
		s := a.open()
		p := a.plan(e, s, allKeys(s))
		e.faults = &copyFaults{hook: func(step string) {
			if step == "W6:2" {
				away(a)
			}
		}}
		res := a.run(e, p, s)
		if res.Stop == nil || res.Stop.RID != "R-o2" {
			t.Fatalf("the run: %+v, stop %+v; want R-o2", res.Counts, res.Stop)
		}
		return res
	}

	t.Run("the folder was moved: Reconnect it, and nothing about permissions", func(t *testing.T) {
		res := stopAt(t, func(a *ceArchive) {
			stash := filepath.Join(tmpDir(t), "away") // the drive stops answering: what it held is gone from the path
			os.MkdirAll(stash, 0o755)
			ents, _ := os.ReadDir(a.dir)
			for _, ent := range ents {
				if err := os.Rename(filepath.Join(a.dir, ent.Name()), filepath.Join(stash, ent.Name())); err != nil {
					t.Fatal(err)
				}
			}
		})
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1),
			assure.Names("no longer available", "it was disconnected or moved", "Reconnect it", "Export the remaining messages", "nothing in the archive is damaged"),
			assure.Forbid("permission", "readable"))
	})

	t.Run("this account may not read it: the stop says to make it readable again", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("an unreadable folder is modelled with permission bits, for a user who is not root")
		}
		res := stopAt(t, func(a *ceArchive) {
			t.Cleanup(func() { os.Chmod(a.dir, 0o755) })
			os.Chmod(a.dir, 0)
		})
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1),
			assure.Names("no longer available", "isn't allowed to read it", "readable", "Export the remaining messages"))
		lacks(t, "the stop", res.Stop.Message, "Reconnect it (or")
	})
}

func lacks(t *testing.T, what, text string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if strings.Contains(text, p) {
			t.Errorf("%s says %q, which is wrong here: %q", what, p, text)
		}
	}
}

// covers: MA-437, MA-420, MA-435, R29, S52
// RV3: the R-h of a retry with nothing to retry fits the folder it is about. An inherited archive folder is
// read-only and its reader has no backup of it: the words say to ask whoever gave them the archive for a
// complete copy (or to copy a folder that holds links into a plain folder), never "restore a file from a
// backup of the archive". The owner's own archive keeps the backup words. The positive twin: the owner's
// words are unchanged.
func TestRetryNothingToRetryFitsAnotherArchiveFolder(t *testing.T) {
	retryRefusal := func(t *testing.T, other bool) string {
		t.Helper()
		a := buildCEArchive(t)
		e := newCEEnv(t)
		s, err := OpenArchive(a.dir, ArchiveOptions{Other: other})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		keys := []string{a.key("hello"), a.key("second")}
		p, _ := a.planWith(e, s, keys, nil)
		linkInPlaceOf(t, a, "hello", "second") // after the preview: a link where the file was, refused for good
		res := a.run(e, p, s)
		if res.Status != CopyStatusPartial || res.Counts.Failed != 1 {
			t.Fatalf("the run: %+v, want partial with one failure", res)
		}
		_, rerr := e.RetryInput(res.RunID, "failed", "")
		cr, ok := rerr.(*CopyRefusal)
		if !ok || cr.ID != "R-h" {
			t.Fatalf("the retry: %v, want R-h", rerr)
		}
		return cr.Message
	}

	owner := retryRefusal(t, false)
	for _, want := range []string{"1 message that failed", "can't be tried again", "backup of the archive", "plain folder"} {
		if !strings.Contains(owner, want) {
			t.Errorf("the owner's R-h lacks %q: %s", want, owner)
		}
	}

	other := retryRefusal(t, true)
	for _, want := range []string{"1 message that failed", "can't be tried again", "Which ones?", "ask whoever gave you the archive", "plain folder"} {
		if !strings.Contains(other, want) {
			t.Errorf("another folder's R-h lacks %q: %s", want, other)
		}
	}
	lacks(t, "another folder's R-h", other, "backup of the archive", "restore")
}
