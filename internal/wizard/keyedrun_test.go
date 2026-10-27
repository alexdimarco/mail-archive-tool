package wizard

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/seal"
)

// covers: MA-806, R37
// The keyed Run now's refusals come before the key is handed over (the P3-3 plan,
// 3d): an Outlook job, a paused archive and an absent archive folder are refused
// with apply never called; what a finished keyed run says names no file of the
// archive (U16). The positive twin: an archive that can run reaches apply.
func TestTheKeyedRunNowRefusesBeforeTheKey(t *testing.T) {
	out := t.TempDir()
	jobAt := func(j job.Job) string {
		p := filepath.Join(t.TempDir(), "job.json")
		if err := job.Write(p, j); err != nil {
			t.Fatal(err)
		}
		return p
	}
	quiet := log.New(io.Discard, "", 0)
	applied := 0
	attended := false
	apply := func(o *app.Options) (func(), error) {
		applied++
		attended = !o.Unattended // a scheduled keyed run stays refused (A14): this one is attended
		return func() {}, context.Canceled
	}

	if _, err := RunJobInProcessKeyed(context.Background(), jobAt(job.Job{Name: "a", Out: out, Mode: "incremental"}), quiet, apply); applied != 1 || err == nil || !attended {
		t.Fatalf("positive twin: an archive that can run reaches apply (%d, %v)", applied, err)
	}
	applied = 0
	for name, c := range map[string]struct {
		j     job.Job
		setup func()
		names string
	}{
		"an Outlook job":    {job.Job{Name: "a", Out: out, Mode: "incremental", Outlook: true}, func() {}, "Outlook"},
		"a paused archive":  {job.Job{Name: "a", Out: out, Mode: "incremental"}, func() { seal.Seal(out, "") }, "paused"},
		"no archive folder": {job.Job{Name: "a", Out: filepath.Join(out, "gone"), Mode: "incremental"}, func() {}, "gone"},
	} {
		c.setup()
		_, err := RunJobInProcessKeyed(context.Background(), jobAt(c.j), quiet, apply)
		if err == nil || applied != 0 || !strings.Contains(err.Error(), c.names) {
			t.Fatalf("%s: %v (apply called %d times)", name, err, applied)
		}
		seal.Unseal(out)
	}
	s := encryptedSummary(app.Result{IndexBehind: true})
	assure.Reached(t, !strings.Contains(s, "index.html") && !strings.Contains(s, "report") && strings.Contains(s, "search index"), "the summary names no file")
}

// covers: MA-848, R37
// A new encrypted archive's choice of program copies is in its run job, and Run now
// carries it as the choice to seal only while none is sealed (R21; the PG4 pass,
// SEC-1): a first backup that could not start or was cancelled does not lose it.
func TestTheRunJobCarriesTheProgramCopiesChoice(t *testing.T) {
	out := t.TempDir()
	quiet := log.New(io.Discard, "", 0)
	got := map[string]app.ProgramChoice{}
	for _, on := range []bool{true, false} {
		p := PendingAddFor(out, nil)
		p.SetProgramCopies(on)
		j := jobFor("a", p.c)
		path := filepath.Join(t.TempDir(), "job.json")
		if err := job.Write(path, j); err != nil {
			t.Fatal(err)
		}
		RunJobInProcessKeyed(context.Background(), path, quiet, func(o *app.Options) (func(), error) {
			got[j.ProgramCopies] = o.ProgramCopiesIfUnchosen
			return func() {}, context.Canceled
		})
	}
	assure.Reached(t, len(got) == 2 && got["on"] == app.ProgramCopiesOn && got["off"] == app.ProgramCopiesOff, "each choice carried as the one to seal while none is")
}
