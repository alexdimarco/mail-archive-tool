package presync

import (
	"errors"
	"sync"
)

// This file holds the attended mode's vocabulary (design-presync-attended §2.1,
// A8, §2.7, §2.8): the Cancel button's cause, what a session left Thunderbird as,
// the phases a session reports, the surfaces its words are for, and the two
// per-platform tables that say where an attended run may start, and may close,
// Thunderbird. Untagged, so the Linux test gate checks every part.

// ErrCancelAndQuit is the cause the Desktop's Cancel button cancels a Run now
// with (§3.1). In an attended session it means "stop the backup; Thunderbird,
// started by this backup, may be closed" (A4(ii)); under never-close (every
// platform until slice AS-Q) it is left open instead, and in S6 the session first
// polls on for the child it started so its record can be cleared (C-74). Any other
// cause — a Lock, Shut down, Ctrl-C, a scheduled stop — quits nothing (A4(i)).
var ErrCancelAndQuit = errors.New("cancelled by the person: Thunderbird may be closed")

// Left is what a session left Thunderbird as (A8, C-23). Synced() never reads it
// (C-72); the end line does.
type Left string

const (
	LeftNone          Left = "none"                // nothing was started, or what was started has gone
	LeftClosed        Left = "closed"              // asked to quit, and it did
	LeftOpen          Left = "left-open"           // left running, with a record the next run announces
	LeftOpenByChoice  Left = "left-open-by-choice" // the person pressed Keep Thunderbird open (slice AS-Q)
	LeftOpenByRule    Left = "left-open-by-rule"   // never-close: this platform's attended close is not allowed (OQ-1), or the wait was stopped (A16)
	LeftMayBeStarting Left = "may-be-starting"     // started, never proven ours: it may still be coming up
)

// The phases a session reports through Config.Phase (§4.6, C-19, C-50).
// `closing` arrives with slice AS-Q.
const (
	PhaseStarting   = "starting"   // S5-S6: Thunderbird is being started
	PhaseWaiting    = "waiting"    // S7: waiting for its mail (since = the start of the wait)
	PhaseCancelling = "cancelling" // A4(ii) under never-close: the S6 child poll after a Cancel
)

// The surfaces OutcomeLine words its remedies for (§4.6, C-38): the Desktop names
// buttons, the command line names no button.
const (
	SurfaceDesktop = "desktop"
	SurfaceCLI     = "cli"
)

// Synced reports whether a session with this outcome counts as having synced
// Thunderbird (§5.2's synced rows), from the outcome alone — never from what
// Thunderbird was left as (C-72). A cancelled run is never synced, and a wait the
// person stopped (wait-ended) counts neither way.
func (o Outcome) Synced() bool {
	switch o {
	case OutcomeOK, OutcomeWatchTruncated, OutcomeNotQuiet,
		OutcomeQuitNotPermitted, OutcomeQuitTimeout, OutcomeQuitSkippedOthers, OutcomeQuitNoWindow,
		OutcomeOwnerActive, OutcomeIdentityUnavailable,
		OutcomeNoActivity, OutcomeWatchTruncatedNoActivity, // synced for the missed streak; the no-change count is AS5's (OQ-5)
		OutcomeAlreadyRunning: // Q1: the person's own Thunderbird was syncing
		return true
	}
	return false
}

// labGate is one platform's entry in an attended table: whether the behaviour is
// allowed there, and the lab record (a path under docs/lab/) that must exist and
// name the platform before the entry may be true (§2.7, §2.8, C-66, MA-927).
type labGate struct {
	allowed bool
	record  string
}

// attendedStartTable says where an attended Run now may start Thunderbird without
// the owner-activity checks (§2.7, C-49). Every entry is false until its lab record
// is in docs/lab/: macOS after labs L-A1 and L-A9 on a test profile (MA-917),
// Windows after MA-918's first-release scope (owner ruling OQ-7: ship the Mac
// first). Where it is false, Run now installs no sync hook at all (§2.2).
var attendedStartTable = map[string]labGate{
	"darwin":  {record: "docs/lab/presync-attended-start-darwin.md"},
	"windows": {record: "docs/lab/presync-attended-start-windows.md"},
}

// attendedQuitTable says where an attended run may ask Thunderbird to close
// (§2.8, OQ-1). Every entry is false until slice AS-Q's commit cites that
// platform's lab L-A5 record; while false, an attended run never closes Thunderbird
// — not at quiet, not on Cancel — and says it was left open (never-close).
var attendedQuitTable = map[string]labGate{
	"darwin":  {record: "docs/lab/presync-attended-close-darwin.md"},
	"windows": {record: "docs/lab/presync-attended-close-windows.md"},
}

// tableMu guards the tables against OverrideTablesForTest racing a reader.
var tableMu sync.RWMutex

// AttendedStartAllowed reports whether an attended Run now may start Thunderbird
// on goos (§2.7). An unknown platform is false.
func AttendedStartAllowed(goos string) bool {
	tableMu.RLock()
	defer tableMu.RUnlock()
	return attendedStartTable[goos].allowed
}

// AttendedQuitAllowed reports whether an attended run may ask Thunderbird to close
// on goos (§2.8); presyncjob sets Config.NeverQuit from it, so no caller can switch
// the close back on (OQ-1). An unknown platform is false.
func AttendedQuitAllowed(goos string) bool {
	tableMu.RLock()
	defer tableMu.RUnlock()
	return attendedQuitTable[goos].allowed
}

// OverrideTablesForTest sets the given platforms' entries of the quit and start
// tables, and returns the function that puts them back (C-79). It is exported only
// because the Desktop and wizard tests that render both values live in other
// packages; MA-927 fails if any non-test file calls it.
func OverrideTablesForTest(quit, start map[string]bool) (restore func()) {
	tableMu.Lock()
	defer tableMu.Unlock()
	oldQuit, oldStart := copyTable(attendedQuitTable), copyTable(attendedStartTable)
	for goos, v := range quit {
		g := attendedQuitTable[goos]
		g.allowed = v
		attendedQuitTable[goos] = g
	}
	for goos, v := range start {
		g := attendedStartTable[goos]
		g.allowed = v
		attendedStartTable[goos] = g
	}
	return func() {
		tableMu.Lock()
		defer tableMu.Unlock()
		attendedQuitTable, attendedStartTable = oldQuit, oldStart
	}
}

func copyTable(t map[string]labGate) map[string]labGate {
	c := make(map[string]labGate, len(t))
	for k, v := range t {
		c[k] = v
	}
	return c
}
