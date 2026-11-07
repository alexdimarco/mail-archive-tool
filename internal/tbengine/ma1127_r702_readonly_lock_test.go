package tbengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// backupLockDuring runs the restore of p and, at its first progress, takes the archive's
// lock as a backup takes it, through lockAt (which may be another spelling of the same
// folder); it returns the run's result and that lock's error (nil: the backup got it).
func backupLockDuring(t *testing.T, p *Plan, lockAt string) (*Result, error) {
	t.Helper()
	tried := false
	var got error
	r, err := Run(context.Background(), p, Start{Progress: func(Progress) {
		if tried {
			return
		}
		tried = true
		l, err := lockfile.AcquireAs(lockAt, "backup")
		if err == nil {
			l.Release()
		}
		got = err
	}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assure.Reached(t, tried, "the restore reported progress, so the backup's try was made while it ran")
	return r, got
}

// covers: MA-1127, R40, S67
// R7-02 (settled 2026-11-07, the lead's call): an archive read through a read-only path
// is held under its lock for the whole restore, not probed for an instant at Start. The
// path being read-only does not make the folder so: a bind mount, a container or a share
// can show a writable folder read-only at a second place. Here the read-only check's seam
// stands in that spelling (a real one: TestAReadOnlyBindMountOfTheArchiveHoldsItsLock),
// and a backup's lock taken while the restore runs is refused as held; the plan says the
// lock is held; after the restore the lock is free. Positive twin first: a read-only
// archive with no lock file (no backup has ever run there) restores without one, and the
// plan says so.
func TestAReadOnlyArchiveKeepsItsLockForTheWholeRestore(t *testing.T) {
	was := readOnlyVolume
	readOnlyVolume = func(string) bool { return true }
	defer func() { readOnlyVolume = was }()

	bare := plainArchive(t, true)
	if err := os.Remove(filepath.Join(bare, lockfile.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	pb := plan(t, Request{Out: bare, Profile: newProfile(t, "").choice()})
	assure.Reached(t, pb.ReadOnly && strings.Contains(pb.Text(), "has no lock file"), fmt.Sprintf("no lock file: the plan says it reads without one: %s\n%s", codesOf(pb), pb.Text()))
	assure.Reached(t, mustRun(t, pb, Start{}).Done(), "restored with no lock file to hold")

	out := plainArchive(t, true)
	lp := filepath.Join(out, lockfile.Name)
	if _, err := os.Stat(lp); err != nil {
		write(t, lp, "")
	}
	p := plan(t, Request{Out: out, Profile: newProfile(t, "").choice()})
	assure.Reached(t, p.ReadOnly && strings.Contains(p.Text(), "still holds the archive's lock"), fmt.Sprintf("the plan says the lock is held: %s\n%s", codesOf(p), p.Text()))
	r, lerr := backupLockDuring(t, p, lp)
	assure.Reached(t, r.Done(), "the restore finished: "+r.Words)
	assure.Reached(t, errors.Is(lerr, lockfile.ErrHeld), fmt.Sprintf("a backup's lock taken during the restore is refused as held: %v", lerr))
	l, err := lockfile.AcquireAs(lp, "backup")
	assure.Reached(t, err == nil, fmt.Sprintf("after the restore the lock is free: %v", err))
	l.Release()
}

// covers: MA-1127, R40, S67
// R7-02, the real case (run on Linux in Docker with one folder mounted read-write and
// read-only, MA_RO_BIND="RO|RW"; elsewhere it skips): the plan through the read-only
// spelling is read-only by the real check, and a backup's lock taken through the
// writable spelling while the restore runs is refused, because the restore holds the
// lock through its read-only handle and a bind mount shares the lock.
func TestAReadOnlyBindMountOfTheArchiveHoldsItsLock(t *testing.T) {
	ro, rw, ok := strings.Cut(os.Getenv("MA_RO_BIND"), "|")
	if !ok {
		t.Skip("MA_RO_BIND not set: no second spelling of one folder here")
	}
	name := fmt.Sprintf("ma1127-%d", os.Getpid())
	t.Cleanup(func() { os.RemoveAll(filepath.Join(rw, name)) })
	backup(t, sourceDir(t), filepath.Join(rw, name), true, nil)
	p := plan(t, Request{Out: filepath.Join(ro, name), Profile: newProfile(t, "").choice()})
	assure.Reached(t, p.ReadOnly, "the read-only spelling is read-only by the real check: "+codesOf(p))
	r, lerr := backupLockDuring(t, p, filepath.Join(rw, name, lockfile.Name))
	assure.Reached(t, r.Done(), "the restore finished: "+r.Words)
	assure.Reached(t, errors.Is(lerr, lockfile.ErrHeld), fmt.Sprintf("a backup through the writable spelling is refused while the restore runs: %v", lerr))
}
