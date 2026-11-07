package tbengine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/tbrestore"
)

// atFirstFolder runs fn once, when the write creates its first mail folder's file (the
// marker and the made-list come first): the moment a restore is under way.
func atFirstFolder(fn func() error) func(op, name string) error {
	done := false
	return func(op, name string) error {
		if done || op != "create" || strings.Contains(name, ".mailarchive-restore") || !strings.Contains(name, ".sbd/") {
			return nil
		}
		done = true
		return fn()
	}
}

// covers: MA-1127, R40, S67
// The archive's lock is held for the whole run, under the verb restore-thunderbird for an
// unencrypted archive (kept whole by the lock's holder line): while the restore writes, a
// backup of the archive is refused as in use, a scheduled one skips quietly naming "a
// restore of this archive into Thunderbird", and another process can take none of the
// three locks. Afterwards — on success — it takes each of them.
func TestRunHoldsTheArchiveLockForTheWholeRun(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	lp := filepath.Join(out, lockfile.Name)
	var during []string
	var verb, skipLine string
	var quiet, backupHeld bool
	withFS(t, atFirstFolder(func() error {
		during = childTakes(t, lockPaths(t, out, pr.dir))
		verb = lockfile.HolderVerb(lp)
		_, berr := app.Run(context.Background(), app.Options{Inputs: []string{sourceDir(t)}, Out: out, KeepRaw: true}, nil, nil)
		backupHeld = errors.Is(berr, lockfile.ErrHeld)
		skipLine, quiet = app.ScheduledLockSkip(out, berr, time.Now())
		return nil
	}))
	res := mustRun(t, p, Start{})
	assure.Reached(t, res.Done(), fmt.Sprintf("the restore finished: %s", res.Words))
	assure.Reached(t, strings.Join(during, " ") == "refused refused refused", fmt.Sprintf("while it writes, no other process takes the archive lock, the restore lock or Thunderbird's: %v", during))
	assure.Reached(t, verb == "restore-thunderbird", fmt.Sprintf("the archive lock names the verb whole: %q", verb))
	assure.Reached(t, backupHeld, fmt.Sprintf("a backup started meanwhile is refused as in use"))
	assure.Reached(t, quiet && strings.Contains(skipLine, "a restore of this archive into Thunderbird"), fmt.Sprintf("a scheduled backup skips quietly, naming the restore: %q", skipLine))
	allFree(t, out, pr.dir, "after a restore that finished")
}

// covers: MA-1127, R40, S67
// Every lock is released, in reverse, on every way a run can end: a write error (the
// disk full), Cancel (its context), Shut down (the context it was started under, ended)
// and a panic, which the run does not recover but unwinds through its releases (the
// job runner recovers it) — each proven by another process taking all three locks after.
// A refusal at the re-check releases the two it had taken too.
func TestRunReleasesEveryLockOnEveryEnd(t *testing.T) {
	out := plainArchive(t, true)
	cases := []struct {
		name string
		run  func(p *Plan) (string, error)
	}{
		{"an error", func(p *Plan) (string, error) {
			withFS(t, atFirstFolder(func() error { return errors.New("the disk is full") }))
			r, err := Run(context.Background(), p, Start{})
			if err != nil {
				return "", err
			}
			return string(r.Write.Outcome), nil
		}},
		{"Cancel", func(p *Plan) (string, error) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			withFS(t, atFirstFolder(func() error { cancel(); return nil }))
			r, err := Run(ctx, p, Start{})
			if err != nil {
				return "", err
			}
			return string(r.Write.Outcome), nil
		}},
		{"Shut down", func(p *Plan) (string, error) {
			life, shutDown := context.WithCancel(context.Background())
			job, cancel := context.WithCancel(life)
			defer cancel()
			withFS(t, atFirstFolder(func() error { shutDown(); return nil }))
			r, err := Run(job, p, Start{})
			if err != nil {
				return "", err
			}
			return string(r.Write.Outcome), nil
		}},
		{"a panic", func(p *Plan) (out string, err error) {
			withFS(t, atFirstFolder(func() error { panic("boom") }))
			defer func() {
				if r := recover(); r != nil {
					out = "panicked"
				}
			}()
			_, err = Run(context.Background(), p, Start{})
			return "", err
		}},
		{"a refusal at the re-check", func(p *Plan) (string, error) {
			was := freeBytes
			freeBytes = func(string) (uint64, error) { return 1, nil }
			defer func() { freeBytes = was }()
			_, err := Run(context.Background(), p, Start{})
			return tbrestore.RefusalCode(err), nil
		}},
	}
	want := map[string]string{"an error": "failed", "Cancel": "stopped", "Shut down": "stopped", "a panic": "panicked", "a refusal at the re-check": "W-CHANGED"}
	for _, c := range cases {
		pr := newProfile(t, "")
		p := plan(t, Request{Out: out, Profile: pr.choice()})
		got, err := c.run(p)
		withFS(t, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		assure.Reached(t, got == want[c.name], fmt.Sprintf("%s ends as %q (want %q)", c.name, got, want[c.name]))
		allFree(t, out, pr.dir, "after "+c.name)
	}
}

// covers: MA-1127, R37, R40, S67
// An encrypted archive's lock names no verb: "restore-thunderbird" beside it would say
// readable mail was copied out of it (as extract's, §6.2); a scheduled backup meeting it
// reads "another MailArchive task". Its locks are released after too.
func TestAnEncryptedArchivesLockNamesNoVerb(t *testing.T) {
	out, u := encryptedArchive(t)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	verb := "unset"
	withFS(t, atFirstFolder(func() error { verb = lockfile.HolderVerb(filepath.Join(out, lockfile.Name)); return nil }))
	res := mustRun(t, p, Start{NotEncryptedOK: true})
	assure.Reached(t, res.Done(), fmt.Sprintf("the encrypted restore finished: %s", res.Words))
	assure.Reached(t, verb == "", fmt.Sprintf("no verb in an encrypted archive's lock: %q", verb))
	allFree(t, out, pr.dir, "after an encrypted restore")
}
