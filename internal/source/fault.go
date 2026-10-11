package source

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Fault is one thing a reader could not read: an item or a folder it skipped, so
// the run can record it in the capture-gap ledger (fault-isolation §4.4, FC29).
// Reason, Scope and Class are the ledger's (state.CaptureGap); Locator is stable for
// the item (pst-nid:<store-uid>/<nid>) or the folder (folder:<path>#<part>), so a
// later run finds the same entry again. Nothing here is trusted: it comes from a
// damaged file, and the ledger strips and bounds it again.
type Fault struct {
	Reason  string
	Scope   string   // state.GapScopeItem or state.GapScopeFolder
	Class   string   // state.ClassFillable, ClassTerminal or ClassInfo
	Folder  []string // the folder the item sat in, or the folder itself (mirrored, sanitized segments)
	Locator string
	Subject string // when the reader could read one (never for an unreadable node)
	Detail  string // the cause, bounded
	Skipped int    // a folder fault: how many items it did not read
}

// FolderPath is the folder's path in the spelling a record's Folder uses.
func (f Fault) FolderPath() string { return strings.Join(f.Folder, "/") }

// maxFaults bounds the faults a Walk keeps for a caller that registered no
// callback; the rest are only counted (FaultsError.Dropped).
const maxFaults = 10000

// FaultsError is what a walk returns to a caller that registered no fault
// callback: the faults it met (at most maxFaults) and how many more it dropped,
// together with the store-level error that ended it, if one did (the outer
// recover of Walk). It is non-nil on purpose: a caller that ignores faults sees a
// loud, fail-closed error, never a quiet success (FC29).
type FaultsError struct {
	Faults  []Fault
	Dropped int
	Err     error // the store-level failure, nil when the walk completed
}

func (e *FaultsError) Error() string {
	n := len(e.Faults) + e.Dropped
	if e.Err != nil {
		return fmt.Sprintf("%v (and %d fault(s) before it)", e.Err, n)
	}
	return fmt.Sprintf("%d item(s) or folder(s) could not be read from the source", n)
}

func (e *FaultsError) Unwrap() error { return e.Err }

// WalkOptions are what a caller passes a FaultWalker.
type WalkOptions struct {
	// Faults, if set, is called with each fault as it happens, during the walk, so
	// the run records it and checkpoints through an hours-long file. A caller that
	// sets none gets a *FaultsError when the walk returns.
	Faults func(Fault)
	// Abort, if set, is asked between rows: a non-nil answer (the run was
	// cancelled, its lock was lost) ends the walk with that error, so a long
	// stretch of unreadable rows, which never reaches the handler, still stops.
	Abort func() error
	// NoStoreUID says the store's identity is not stable across runs (an -outlook
	// scratch PST, rebuilt every run; an .ost Outlook rebuilds in place): its
	// zero-content failures are then counted in one per-folder aggregate instead of
	// being listed by NID (FC8).
	NoStoreUID bool
}

// FaultWalker is a Source that reports what it could not read as it goes. The PST
// reader is one; the mbox, maildir and Evolution readers join in FI-S2b.
type FaultWalker interface {
	Source
	WalkWithFaults(handler MessageHandler, opts WalkOptions) error
}

// faultSink delivers faults to the callback or keeps them for a FaultsError.
type faultSink struct {
	cb      func(Fault)
	kept    []Fault
	dropped int
}

func (s *faultSink) emit(f Fault) {
	f.Detail = boundDetail(f.Detail)
	if s.cb != nil {
		s.cb(f)
		return
	}
	if len(s.kept) < maxFaults {
		s.kept = append(s.kept, f)
		return
	}
	s.dropped++
}

// result is the walk's return: err as it is when a callback took the faults, else
// a *FaultsError holding them (and err).
func (s *faultSink) result(err error) error {
	if s.cb != nil || (len(s.kept) == 0 && s.dropped == 0) {
		return err
	}
	return &FaultsError{Faults: s.kept, Dropped: s.dropped, Err: err}
}

// boundDetail strips control characters from an error text and cuts it to the
// ledger's bound (300 bytes) on a rune boundary.
func boundDetail(s string) string {
	s = util.StripControl(s)
	const max = 300
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 {
		if r, size := utf8.DecodeLastRuneInString(s); r == utf8.RuneError && size <= 1 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// the ledger's classes and scopes, as the readers name them.
const (
	scopeItem   = state.GapScopeItem
	scopeFolder = state.GapScopeFolder
	classTerm   = state.ClassTerminal
)
