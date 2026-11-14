package app

// Proving, before a plaintext run writes its progress, that its folder still holds the archive it
// began with (the B3 adversarial pass, round 3's H1). A drive of a rotation can be swapped at the
// same path or letter, or the folder renamed, at any moment of a run: while it waits for
// Thunderbird, while it walks its sources (minutes for a large store, even when nothing is new), or
// after. The lock file goes with the old folder, so the folder at the run's path then holds another
// archive, or none. An encrypted archive's saves prove the lock at their own commit point
// (container.ErrLockLost); a plaintext save writes by path, so the run proves it here first.

import (
	"fmt"
	"time"

	"mail-archive-tool/internal/state"
)

// archiveRecheckEvery is how often, at most, a plaintext run's walk proves again that its folder
// still holds its archive between saves, so a long walk stops writing message files into another
// archive soon after a swap. A var so a test can make it due at every message.
var archiveRecheckEvery = 10 * time.Second

// archiveChangedFor says the folder opts.Out no longer holds the archive a plaintext run began
// with (began, the id its "running" record carries): its record carries another id, or carries
// none while the run's lock file is gone or replaced too (an archive an older MailArchive wrote).
// Never for an encrypted archive: its record has no id, and its key opens only its own archive.
func archiveChangedFor(opts Options, began string, stillHeld func() error) func() bool {
	return func() bool {
		if opts.Key != nil || began == "" {
			return false
		}
		id := state.ArchiveID(opts.Out)
		if id == began {
			return false
		}
		return id != "" || stillHeld() != nil
	}
}

// archiveGuard proves, before every save of a plaintext run's progress and on time during its
// walk, that its folder still holds its archive and that the lock is still its own.
type archiveGuard struct {
	out       string
	plaintext bool
	stillHeld func() error
	// changed says the folder holds another archive (archiveChangedFor); nil where the run keeps
	// no archive id, when the lock alone is the proof.
	changed func() bool
	proved  time.Time
}

func newArchiveGuard(opts Options, stillHeld func() error, changed func() bool) *archiveGuard {
	return &archiveGuard{out: opts.Out, plaintext: opts.Key == nil, stillHeld: stillHeld, changed: changed, proved: time.Now()}
}

// prove returns why the run must write nothing more into its folder, nil when it may go on: the
// folder holds another archive, or the lock file is no longer the run's. Always nil for an
// encrypted run, whose saves prove the lock themselves.
func (g *archiveGuard) prove() error {
	if !g.plaintext {
		return nil
	}
	g.proved = time.Now()
	if g.changed != nil && g.changed() {
		return archiveChangedDuringError(g.out)
	}
	return g.stillHeld()
}

// due says a plaintext walk should prove again now, between saves.
func (g *archiveGuard) due() bool {
	return g.plaintext && time.Since(g.proved) >= archiveRecheckEvery
}

// archiveChangedDuringError is a run's stop when its folder came to hold another archive after
// it began reading: its manifest, record and README were not written there. Message files it wrote
// before the proof that stopped it stay where they were written, as after any lost lock (R5); the
// proof on time bounds them to about archiveRecheckEvery of the walk.
func archiveChangedDuringError(out string) error {
	return fmt.Errorf("the archive folder %s now holds another archive than the one this backup began with (its drive was changed, or the folder was moved, during the backup): this backup stopped and recorded nothing in it. Run the backup again", out)
}
