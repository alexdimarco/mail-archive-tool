package schedule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Reason explains which program ProgramForSchedule chose, and why, so one
// renderer (ProgramNote, §4.6.1) can turn it into the owner's words. The empty
// reason means "no note": the running program itself is the right one.
type Reason string

const (
	ReasonNone              Reason = ""
	ReasonSibling           Reason = "sibling"
	ReasonSiblingAbsent     Reason = "sibling_absent"
	ReasonSiblingNotRegular Reason = "sibling_not_regular"
	ReasonSiblingOtherOwner Reason = "sibling_other_owner"
	ReasonPairOutOfStep     Reason = "pair_out_of_step"
	ReasonWrappedConsole    Reason = "wrapped_console"
)

// ErrTranslocated is returned by ProgramForSchedule when the running program is
// a macOS App Translocation copy (its resolved path lies under an
// /AppTranslocation/ directory): a schedule recorded from it stops working the
// moment the app quits, so every caller refuses (D1, §4.6.1). It carries no path.
var ErrTranslocated = errors.New("the program is running from a temporary translocated copy")

// l2WrapperGateHolds records whether lab item L-W2 has shown that a wrapped
// schedule can run the no-console build mailarchivew.exe — that cmd.exe waits for
// the child, the child's stderr reaches <name>.stderr.log, and its exit code
// becomes the task's Last Run Result. It defaults to true (the gate is assumed to
// hold), so the wrapped_console fallback below is dormant until L-W2 runs and an
// operator flips this with the SE0 decision (SC6, SC22). While it holds, a wrapped
// schedule records the qualifying sibling exactly as an unwrapped one does.
const l2WrapperGateHolds = true

// Seams for self-resolution, overridden in tests so a chosen self can be injected.
var (
	osExecutable = os.Executable
	evalSymlinks = filepath.EvalSymlinks
)

// ProgramForSchedule returns the absolute path of the program a schedule should
// run, and a Reason for any note the caller must show. It is the one place
// (with §4.6.2's identity) that resolves the program path; the four schedule
// call sites — the command line's runSchedule, the wizard's offerSchedule and
// repair, and the desktop's backupSpecFor — use it in place of os.Executable()
// (§4.6.1, MA-615). Nothing from the archive, the descriptor or the job file
// influences the choice (P6).
//
// self is os.Executable() resolved through EvalSymlinks and cleaned; a
// non-absolute result is an error. A translocated self returns ErrTranslocated
// with no path. On Windows, when self is the console build mailarchive.exe or a
// former name, the no-console sibling mailarchivew.exe beside it is preferred when
// SiblingQualifies holds (reason sibling), else self with the qualifying failure's
// reason. Elsewhere, and for mailarchivew.exe itself, it is self with no reason.
// wrapped is the caller's wrapper decision (§4.6.1): the wrapped sibling is only
// held back — as self, reason wrapped_console — once L-W2 is known not to hold.
func ProgramForSchedule(wrapped bool) (exe string, reason Reason, err error) {
	self, err := osExecutable()
	if err != nil {
		return "", ReasonNone, fmt.Errorf("could not determine my own executable path: %w", err)
	}
	if resolved, rerr := evalSymlinks(self); rerr == nil {
		self = resolved
	}
	self = filepath.Clean(self)
	if !filepath.IsAbs(self) {
		return "", ReasonNone, fmt.Errorf("the running program path %q is not absolute", self)
	}
	return programForSchedule(runtime.GOOS, self, wrapped, SiblingQualifies)
}

// programForSchedule is the pure core of ProgramForSchedule: it takes the OS,
// the already-resolved absolute self, the wrapper decision and the sibling test,
// so every OS, name and reason is table-testable from any host.
func programForSchedule(goos, self string, wrapped bool, qualifies func(self, p string) (bool, Reason)) (string, Reason, error) {
	if strings.Contains(strings.ToLower(filepath.ToSlash(self)), "/apptranslocation/") {
		return "", ReasonNone, ErrTranslocated
	}
	if goos != "windows" {
		return self, ReasonNone, nil
	}
	// Windows: only the console build and the former names look up the sibling;
	// the no-console build itself, a suffixed name and a renamed copy do not.
	switch strings.ToLower(filepath.Base(self)) {
	case "mailarchive.exe", "mailarchive-desktop.exe", "mailarchive-gui.exe":
	default:
		return self, ReasonNone, nil
	}
	sibling := filepath.Join(filepath.Dir(self), "mailarchivew.exe")
	ok, r := qualifies(self, sibling)
	if !ok {
		return self, r, nil
	}
	if wrapped && !l2WrapperGateHolds {
		return self, ReasonWrappedConsole, nil
	}
	return sibling, ReasonSibling, nil
}
