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
// moment the app quits, so every caller refuses (D1, §4.6.1). It carries no path,
// and its message is the X2 refusal every caller shows.
var ErrTranslocated = errors.New("Mail Archive is running from a temporary copy macOS made of a downloaded app; a schedule recorded from it stops working when the app quits. Do this: drag Mail Archive.app to your Applications folder, open it from there, and install the schedule (or choose Repair) again.")

// l2WrapperGateHolds records whether lab item L-W2 has shown that a wrapped
// schedule can run the no-console build mailarchivew.exe — that cmd.exe waits for
// the child, the child's stderr reaches <name>.stderr.log, and its exit code
// becomes the task's Last Run Result. It defaults to true (the gate is assumed to
// hold), so the wrapped_console fallback below is dormant until L-W2 runs and an
// operator flips this with the SE0 decision (SC6, SC22). While it holds, a wrapped
// schedule records the qualifying sibling exactly as an unwrapped one does.
const l2WrapperGateHolds = true

// SelfResolver returns the running program's path, resolved through EvalSymlinks
// and cleaned. It is a var so a test — in any package — can inject a chosen self,
// e.g. a translocated path to drive a caller's refusal, exactly as
// instance.OwnerReader is injected (§4.6.1, MA-630).
var SelfResolver = func() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine my own executable path: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
		self = resolved
	}
	return filepath.Clean(self), nil
}

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
	self, err := SelfResolver()
	if err != nil {
		return "", ReasonNone, err
	}
	if !filepath.IsAbs(self) {
		return "", ReasonNone, fmt.Errorf("the running program path %q is not absolute", self)
	}
	return programForSchedule(runtime.GOOS, self, wrapped, SiblingQualifies)
}

// ProgramNote is the one rendering of a ProgramForSchedule result that any
// surface shows (§4.6.1): the command line's schedule -install (stdout, after a
// successful install), the wizard's offerSchedule and repair result dialogs, and
// the Weekly backup card's Install/update and Repair results. A notice about a
// missing or unusable mailarchivew.exe goes through here, never a separate write.
// It is empty when there is nothing to say — the running program is the right one
// (none), the qualifying sibling was taken (sibling), or the console build is kept
// under a wrapper (wrapped_console). Otherwise it names the program the schedule
// will run, why the sibling was not used, the console cost when the console build
// is what runs, and the repair to do.
func ProgramNote(exe string, reason Reason) string {
	var why string
	switch reason {
	case ReasonSiblingAbsent:
		why = "is missing"
	case ReasonSiblingNotRegular:
		why = "is not a plain file"
	case ReasonSiblingOtherOwner:
		why = "belongs to another account"
	case ReasonPairOutOfStep:
		why = "is from a different release"
	default: // none, sibling, wrapped_console, or anything unknown: no note
		return ""
	}
	base := filepath.Base(exe)
	note := fmt.Sprintf("This schedule runs %s because mailarchivew.exe beside it %s.", base, why)
	if base == "mailarchive.exe" {
		note += " A console window will stay open while it runs."
	}
	if reason == ReasonSiblingAbsent {
		note += " Do this: restore mailarchivew.exe with the installer's Repair (if your antivirus removed it, restore and allow it first), or copy both files from one release, then choose Repair."
	} else {
		note += " Do this: put mailarchivew.exe from the same release back beside it (reinstall from the MSI, or copy both files from one release) and choose Repair."
	}
	return note
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
