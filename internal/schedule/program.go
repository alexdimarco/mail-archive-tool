package schedule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/encmark"
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

// ErrKeptCopy is returned by ProgramForSchedule when the running program is a copy
// of MailArchive kept in an encrypted archive (design-encrypted-archives-phase3-program
// §6, C15): the next backup may replace or remove it, so a schedule recorded from it
// would stop working. It carries no path.
var ErrKeptCopy = errors.New("Mail Archive is running from a copy kept inside an encrypted archive; a backup may replace or remove that copy, so a schedule cannot run it. Do this: copy the program out of the archive folder (or download it again from the official release page), open it from there, and install the schedule (or choose Repair) again.")

// RefuseKeptCopy refuses a program inside an encrypted archive — in its program
// folder or anywhere in it — or named as a self-copy (mailarchive-self-…), on any
// part of its path.
// It is exported for a caller that already holds the program's path (the Desktop's
// launched Exe), so no path to a schedule skips it (the PG pass, INT-02).
func RefuseKeptCopy(p string) error {
	if res, _ := encmark.At(filepath.Dir(p)); res.Encrypted {
		return ErrKeptCopy
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.HasPrefix(strings.ToLower(seg), "mailarchive-self-") {
			return ErrKeptCopy
		}
	}
	return nil
}

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
	exe, reason, err = programForSchedule(runtime.GOOS, self, wrapped, SiblingQualifies)
	if err != nil {
		return exe, reason, err
	}
	for _, p := range []string{self, exe} {
		if kerr := RefuseKeptCopy(p); kerr != nil {
			return "", ReasonNone, kerr
		}
	}
	// §5.3/Q20 (MA-648): refuse a program an account other than the owner could
	// replace in place, so a scheduled run can never be made to execute someone
	// else's code as the owner. One chokepoint for every scheduling caller.
	if lerr := guardLocation(exe); lerr != nil {
		return "", ReasonNone, lerr
	}
	return exe, reason, nil
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

// SameArchivePath reports whether two -out spellings name the same directory
// (§4.6.2). It prefers os.SameFile when both paths stat (collapsing symlink,
// 8.3/long and mapped-drive spellings); when a stat is unavailable it folds case
// only on the case-insensitive volumes (darwin/windows), keeping an exact
// byte-compare on linux so a genuinely different-cased directory there still
// warns (UJ-A4). The kind corroboration (§4.6.5) and health share this one.
func SameArchivePath(a, b, goos string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	if goos == "darwin" || goos == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return false
}

// KnownName reports whether base is one of the one program's install or former
// names, matched per OS (EqualFold on Windows and macOS, exact on Linux), with no
// trimming of trailing dots or spaces (§4.6.2). The former names stay in the set
// until SE4.
func KnownName(base, goos string) bool {
	for _, n := range []string{"mailarchive", "mailarchivew", "mailarchive-desktop", "mailarchive-gui"} {
		if nameEq(base, n, goos) || nameEq(base, n+".exe", goos) {
			return true
		}
	}
	return false
}

// FormerName reports whether base begins with a former-name prefix
// (mailarchive-desktop or mailarchive-gui), so a former release asset such as
// mailarchive-gui-linux-amd64 is a former name too (§4.1 rule 6's test).
func FormerName(base, goos string) bool {
	return hasNamePrefix(base, "mailarchive-desktop", goos) || hasNamePrefix(base, "mailarchive-gui", goos)
}

func nameEq(a, b, goos string) bool {
	if goos == "linux" {
		return a == b
	}
	return strings.EqualFold(a, b)
}

func hasNamePrefix(s, prefix, goos string) bool {
	if len(s) < len(prefix) {
		return false
	}
	if goos == "linux" {
		return s[:len(prefix)] == prefix
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
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
