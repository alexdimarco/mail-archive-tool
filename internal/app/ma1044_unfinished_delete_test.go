package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The engine's unfinished-delete check (MA-1044; the 1.5.3 re-walk's guards pass, G2): a backup
// asks it at its start and again under its archive lock. These tests install a stand-in check
// (archdelete's own, with its words, is proven in internal/archdelete, and through every entry
// point in the wizard, the Desktop and the command line).

// ma1044Check is a stand-in check: it refuses a folder marked pending, and records the log path
// each ask was given.
type ma1044Check struct {
	mu      sync.Mutex
	pending map[string]bool
	asked   []string
}

func (c *ma1044Check) check(logPath, out string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, logPath)
	if c.pending[out] {
		return errors.New("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed.")
	}
	return nil
}

func (c *ma1044Check) mark(out string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[out] = true
}

func (c *ma1044Check) asks() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.asked...)
}

func (c *ma1044Check) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = nil
}

func ma1044Install(t *testing.T) *ma1044Check {
	t.Helper()
	c := &ma1044Check{pending: map[string]bool{}}
	t.Cleanup(SetUnfinishedDeleteCheck(c.check))
	return c
}

// ma1044Snapshot is every file under dir with its bytes, the run's own lock file aside (the
// known leftover a released lock leaves: lockfile Release).
func ma1044Snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() || rel == lockfile.Name {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func ma1044Same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// ma1044Refused checks a refusal by the unfinished-delete check: exit 1, the check's words and
// the folder named, kind delete_unfinished (MA-1045), which RefusedAsNoArchive counts with
// archive_absent (so a scheduled run's callers take back what they wrote before the lock and tell
// it outside the folder), never a source-loss verdict, and the side effect absent.
func ma1044Refused(t *testing.T, err error, out string, absent func() bool) {
	t.Helper()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+out+")"),
		assure.NoSideEffect(absent))
	if k := FailureKind(err); k != state.FailDeleteUnfinished || !RefusedAsNoArchive(err) {
		t.Errorf("the refusal's kind is %s, taken back as no archive, so the callers take back their writes; got %q (taken back %v)", state.FailDeleteUnfinished, k, RefusedAsNoArchive(err))
	}
	if FoundSourceUnreachable(err) {
		t.Error("a refusal for an unfinished delete says nothing about the source: it must not extend the source-loss streak")
	}
}

// covers: MA-1044, R36
// app.Run asks the unfinished-delete check at its start, before any folder, lock or record —
// attended or scheduled — and again under its lock, so a delete that began after the first ask
// and stopped part way before the lock was free is refused before the run's record or any write;
// the lock is released. It passes the run's DeletionsLog. Positive twin first: with the check
// installed and nothing pending, a run asks twice and archives.
func TestMA1044_RunAsksAtItsStartAndUnderItsLock(t *testing.T) {
	c := ma1044Install(t)
	in := ma956Input(t)
	run := func(out string, unattended bool) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: unattended, DeletionsLog: "the-log"}, discard(), nil)
		return err
	}

	// Positive twin.
	ok := filepath.Join(t.TempDir(), "ok")
	if err := run(ok, false); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(ok, ma956Manifest)), "the twin's archive")
	if got := strings.Join(c.asks(), ","); got != "the-log,the-log" {
		t.Fatalf("the run asks at its start and under its lock, with its DeletionsLog: got %q", got)
	}

	// At its start, attended: the folder is not made.
	fresh := filepath.Join(t.TempDir(), "fresh")
	c.mark(fresh)
	ma1044Refused(t, run(fresh, false), fresh, func() bool { return !ma956Exists(fresh) })

	// At its start, scheduled, into an archive: byte-identical (no record written), the lock not
	// taken (the lock file is the earlier run's), and asked once.
	half := filepath.Join(t.TempDir(), "half")
	if err := run(half, false); err != nil {
		t.Fatal(err)
	}
	before := ma1044Snapshot(t, half)
	c.mark(half)
	c.reset()
	ma1044Refused(t, run(half, true), half, func() bool {
		return ma1044Same(before, ma1044Snapshot(t, half)) && !lockfile.Held(filepath.Join(half, lockfile.Name)) && len(c.asks()) == 1
	})

	// Under the lock: a delete of the folder begins after the run's first ask and stops before
	// the lock is free. Nothing recorded (no last run, history or source streak written) and
	// the lock released.
	raced := filepath.Join(t.TempDir(), "raced")
	if err := run(raced, false); err != nil {
		t.Fatal(err)
	}
	before = ma1044Snapshot(t, raced)
	c.reset()
	ma956SeamLock(t, func() { c.mark(raced) })
	ma1044Refused(t, run(raced, true), raced, func() bool {
		return ma1044Same(before, ma1044Snapshot(t, raced)) && !lockfile.Held(filepath.Join(raced, lockfile.Name))
	})
	assure.Reached(t, len(c.asks()) == 2, "the refusal came from the ask under the lock")
}

// covers: MA-1044, R36
// app.RunGraph (a Microsoft 365 capture, scheduled or Archive now) asks the same check at its
// start, before the sign-in, and again under its lock. Positive twin first.
func TestMA1044_RunGraphAsksAtItsStartAndUnderItsLock(t *testing.T) {
	c := ma1044Install(t)
	_, srv := newFakeDelegServer()
	defer srv.Close()
	capture := func(out string, unattended bool) error {
		cache := filepath.Join(t.TempDir(), "primed.json")
		writePrimedCache(t, cache)
		g := delegOpts(srv, cache)
		g.Unattended = unattended
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Unattended: unattended, DeletionsLog: "the-log"}, discardLogger())
		return err
	}

	// Positive twin.
	ok := filepath.Join(t.TempDir(), "ok")
	if err := capture(ok, false); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(ok, ma956Manifest)), "the twin's archive")
	if got := strings.Join(c.asks(), ","); got != "the-log,the-log" {
		t.Fatalf("the capture asks at its start and under its lock, with its DeletionsLog: got %q", got)
	}

	// At its start: no folder made, no sign-in.
	fresh := filepath.Join(t.TempDir(), "fresh")
	c.mark(fresh)
	ma1044Refused(t, capture(fresh, false), fresh, func() bool { return !ma956Exists(fresh) })

	// Under the lock, scheduled.
	raced := filepath.Join(t.TempDir(), "raced")
	if err := capture(raced, false); err != nil {
		t.Fatal(err)
	}
	before := ma1044Snapshot(t, raced)
	ma956SeamLock(t, func() { c.mark(raced) })
	ma1044Refused(t, capture(raced, true), raced, func() bool {
		return ma1044Same(before, ma1044Snapshot(t, raced)) && !lockfile.Held(filepath.Join(raced, lockfile.Name))
	})
}
