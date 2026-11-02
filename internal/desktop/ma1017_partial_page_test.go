package desktop

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-1017, R36, S41
// A delete of the page's own archive that stops after its step 4 cleared it as the archive in use
// — at step 6, a file it could not remove — is not finished, yet the page's archive is no longer
// in use: the reply's page is "partial", so the page script greys the archive's cards (the 1.5.3
// friction fixes' adversarial pass, H1; before, any delete not finished said "unfinished" and the
// cards stayed live for an archive no longer in use). Positive twin first: a stop at step 2 leaves
// it the archive in use, and the reply's page is still "unfinished". The file is made one step 6
// cannot remove on every platform, as any user: while the delete stops the scheduled backup (step
// 2, after the plan was checked again under the lock), a recorded file is replaced by a folder
// that holds a file.
func TestADeleteOfTheCurrentArchiveStoppedAfterStep4SaysPartial(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}

	// The positive twin: stopped at step 2, still the archive in use.
	name := schedule.DefaultNameFor(cur)
	e.sched.installed[name] = true
	e.sched.failOn[name] = os.ErrPermission
	r := e.deleteDrawn(cur, cur)
	if r["unfinished"] != true || r["page"] != "unfinished" || LoadSettings(e.cfg.SettingsPath).Out != cur {
		t.Fatalf("the positive twin: a stop at step 2 of the current archive: page %v, unfinished %v, archive in use %q",
			r["page"], r["unfinished"], LoadSettings(e.cfg.SettingsPath).Out)
	}

	// Stopped at step 6, after step 4 cleared it.
	delete(e.sched.failOn, name)
	victim := recordedFileIn(t, cur)
	e.sched.onRemove = func(string) {
		if err := os.Remove(victim); err != nil {
			t.Error(err)
		}
		if err := os.MkdirAll(victim, 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(filepath.Join(victim, "held.txt"), []byte("held"), 0o600); err != nil {
			t.Error(err)
		}
	}
	r = e.deleteDrawn(cur, cur)
	e.sched.onRemove = nil
	if r["status"] != "not fully deleted" || r["unfinished"] != true {
		t.Fatalf("the delete stopped at step 6: %v", r)
	}
	if out := LoadSettings(e.cfg.SettingsPath).Out; out != "" {
		t.Fatalf("step 4 did not clear the archive in use: %q", out)
	}
	if r["page"] != "partial" {
		t.Errorf("a delete not finished after step 4 cleared the page's archive says page %q; want \"partial\", so the page greys its cards", r["page"])
	}
}

// covers: MA-1017, R36, S41
// The page script greys the archive's cards for a reply whose page is "partial" as for "deleted",
// in "not fully deleted" words, and marks its row not finished (H1 of the 1.5.3 friction fixes'
// adversarial pass: the "partial" path was never run). Positive twin first (in the scenario):
// "unfinished" greys nothing. Run on the page script under Node.
func TestThePageGreysTheArchiveForAPartialDelete(t *testing.T) {
	if passed := runPageScenario(t, "partialCurrent"); len(passed) != 4 {
		t.Errorf("the partialCurrent scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}

// recordedFileIn is a file the archive in dir recorded, in a folder below it (never its manifest).
func recordedFileIn(t *testing.T, dir string) string {
	t.Helper()
	var found string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && found == "" && d.Type().IsRegular() && filepath.Dir(p) != dir {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no file below a folder in %s", dir)
	}
	return found
}
