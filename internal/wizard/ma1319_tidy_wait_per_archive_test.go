package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1319, R45, R37
// The wait for another computer's tidy-up is kept for the archive (its id), not for the folder (the
// B3 adversarial pass, round 2's G3, turned from its probe TestB3AdvR2RotationResetsTheTidyWait):
// two drives used in turn at one path, drive 1 stuck at version 8. Drive 2's backups, which get
// past the manifest, end drive 2's wait only; drive 1's wait keeps its start, so once it has lasted
// 3 days drive 1's next waiting backup sends the one alert and the one notice. Positive twin last:
// once drive 1's tidy-up has finished, its own backup ends its wait and sends the recovery.
func TestTheTidyUpWaitIsKeptForTheArchiveNotTheFolder(t *testing.T) {
	g := newLocalRunRig(t)
	okJob := g.jobAt(filepath.Join(g.dir, "ok.json"), g.mbox)
	if RunHeadless(okJob) != 0 || g.results() != "" {
		t.Fatalf("drive 1's first backup: sent %q", g.results())
	}
	d1, d2 := g.out+"-drive1", g.out+"-drive2"
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{g.mbox}, Out: d2, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	id1, id2 := state.ArchiveID(g.out), state.ArchiveID(d2)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("the drives' ids: %q %q", id1, id2)
	}
	stampVersion(t, g.out, "8") // drive 1: another computer's tidy-up, part-way
	if err := os.Rename(g.out, d1); err != nil {
		t.Fatal(err)
	}
	plug := func(from string) func() {
		t.Helper()
		if err := os.Rename(from, g.out); err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.Rename(g.out, from); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Drive 1 waits; its wait began 4 days ago (as kept by its first waiting backup).
	unplug := plug(d1)
	if RunHeadless(okJob) != 0 {
		t.Fatal("drive 1's waiting backup is not a skip")
	}
	if err := gmailanswer.ClearTidyWait(g.out); err != nil {
		t.Fatal(err)
	}
	began := time.Now().Add(-4 * 24 * time.Hour).UTC().Truncate(time.Second)
	if _, err := gmailanswer.NoteTidyWait(g.out, began); err != nil {
		t.Fatal(err)
	}
	unplug()

	// Drive 2's backup goes through: drive 1's wait stays.
	unplug = plug(d2)
	if RunHeadless(okJob) != 0 {
		t.Fatal("drive 2's backup failed")
	}
	if _, ok := gmailanswer.ReadTidyWait(g.out); ok {
		t.Fatal("drive 2, which never waited, reads a wait")
	}
	unplug()
	unplug = plug(d1)
	w, ok := gmailanswer.ReadTidyWait(g.out)
	if !ok || !w.Since.Equal(began) || w.Archive != id1 {
		t.Fatalf("drive 2's backup ended drive 1's wait: %+v %v (began %v)", w, ok, began)
	}
	if g.results() != "" || len(g.notices) != 0 {
		t.Fatalf("before drive 1's next waiting backup: sent %q, notices %v", g.results(), g.notices)
	}
	if RunHeadless(okJob) != 0 {
		t.Fatal("drive 1's waiting backup is not a skip")
	}
	if g.results() != "failed" || len(g.notices) != 1 || !g.sent[0].Tidying {
		t.Fatalf("drive 1, waiting 4 days: sent %q, notices %v; want one alert and one notice", g.results(), g.notices)
	}

	// Drive 1's tidy-up finishes: its own backup ends its wait and sends the recovery.
	stampVersion(t, g.out, "6")
	if RunHeadless(okJob) != 0 || g.results() != "failed,recovered" {
		t.Fatalf("drive 1's backup after its tidy-up: sent %q", g.results())
	}
	if _, ok := gmailanswer.ReadTidyWait(g.out); ok {
		t.Fatal("drive 1's wait is kept after its own backup went through")
	}
	unplug()
}
