package desktop

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/dialogfront"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/wizard"
)

// The launcher choice and the wizard inside the running MailArchive (design-menubar
// §6, slice S1). The instance — the process that holds this account's desktop.lock —
// shows a small native list, "Add or back up mail, step by step" or "Open MailArchive
// Desktop in your browser", when it starts from an app icon (`start`), when a later
// launch hands off with `choose`, and on macOS when the app is opened again (reopen).
// It runs the wizard in-process, one at a time per account (the wizard lock), with its
// Desktop item dropped (Q2). Each dialog comes only at the person's request: the start
// they clicked, a hand-off whose proof was verified, or the reopen macOS sent for their
// click (R39, MB-8).

// The choice window's words (Q1, design-menubar §6.1, as the S1 friction review's F1 and
// F16 reword them: every item under about 40 characters and one prompt line, since
// Windows' list window is 241 pixels wide and shows its prompt on one line).
const (
	choiceTitle       = "MailArchive"
	choiceText        = "What would you like to do?"
	choiceWizardItem  = "Add or back up mail, step by step"
	choiceDesktopItem = "Open MailArchive Desktop in your browser"
	// choiceQuitItem is MB-14's Quit, offered only with the newer-installed line.
	choiceQuitItem = "Quit, to use the new version"
	// newerInstalledText is MB-14's line: the running copy is older than the program
	// on disk; it never restarts itself (R33).
	newerInstalledText = "A newer MailArchive is installed."
	// nowPrefix starts the choice's line about the work that runs (MB-15: what runs,
	// never an archive's name).
	nowPrefix = "Now: "
	// otherSessionText is R50's message, in the launcher's own session, when the
	// running copy belongs to another sign-in of this account (the fold's third
	// contradiction; worded by the S1 friction review).
	otherSessionText = "MailArchive is already open for this account on another screen (another sign-in). Use it there, or quit it there to open it here."
	// unknownSessionText is what a launch says when this launch's sign-in or the running
	// copy's cannot be read, so nothing is shown on a screen that may not be this one
	// (fail closed, the S1 adversarial pass A3).
	unknownSessionText = "MailArchive is already open for this account, but it cannot tell whether that is on this screen. If you do not see it here, quit it where it runs, then open it here."
	// wizardOpenDesktopText is what a click on MailArchive asks while the wizard is open
	// (F3): Desktop stays one click away, OK and Cancel.
	wizardOpenDesktopText = "The wizard is open. Open MailArchive Desktop in your browser as well?"
	// closingText is what a launch says when the running copy is still finishing its
	// work and closing after the launch's whole wait (the S1 adversarial pass, A7).
	closingText = "MailArchive is finishing its work and closing. Open it again in a moment."
)

// The notice a launch shows when the choice is already on the screen, in its platform's
// words (the S1 friction review, F6: only Windows has a taskbar).
const (
	choiceOpenTextWindows   = "MailArchive is already asking what you'd like to do. Look for its window, or click MailArchive on the taskbar."
	choiceOpenTextElsewhere = "MailArchive is already asking what you'd like to do. Look for its window on your screen."
)

// choiceOpenText is this platform's form of the already-open notice.
var choiceOpenText = choiceOpenTextOn(runtime.GOOS)

func choiceOpenTextOn(goos string) string {
	if goos == "windows" {
		return choiceOpenTextWindows
	}
	return choiceOpenTextElsewhere
}

// What the person picked in the choice window.
const (
	pickWizard  = "wizard"
	pickDesktop = "desktop"
	pickQuit    = "quit"
	pickNone    = "" // Cancel, or the window closed
)

// choiceView is what the choice window shows: at most one line, which takes the
// question's place (the list itself is the question), which item is the default, and
// whether it offers Quit (MB-14).
type choiceView struct {
	Line        string
	DefaultItem string
	Quit        bool
}

// Text is the window's one prompt line: the view's line, or the question.
func (v choiceView) Text() string {
	if v.Line != "" {
		return v.Line
	}
	return choiceText
}

// The dialog seams, package variables so the dialog walk (MA-1111, MA-1117) links each
// to the declarations that use it, and a test replaces them. The real ones are in
// front_dialogs.go.
var (
	chooseDialog       = zenityChoose
	infoDialog         = zenityInfo
	quitQuestionDialog = zenityQuitQuestion
	// wizardOpenDesktopDialog asks whether to open Desktop while the wizard is open.
	wizardOpenDesktopDialog = zenityAskDesktop
	runWizardIn             = wizard.RunIn
	// canShowDialogs says whether this process can show the native dialogs: macOS and
	// Windows always can; Linux needs a dialog tool and a display (MB-9).
	canShowDialogs = dialogsPossible
)

// sessionsSeparate is true where the instance lock is already per sign-in (Windows'
// logon session): a launch only ever reaches its own session's copy, so the sign-in is
// not compared. Elsewhere one copy serves the account, and a launch whose sign-in, or
// the copy's, cannot be read is answered "unknown" (R50, fail closed; A3).
var sessionsSeparate = runtime.GOOS == "windows"

// front is the instance's side of the choice: who it is, how it opens Desktop, and
// what it is showing now.
type front struct {
	life    *Lifecycle
	dir     string // the account folder (the wizard lock lives there)
	session string // this instance's sign-in session (instance.GUISession)
	version string
	// open opens Desktop in the browser at a fresh one-time link, or shows the link.
	open func()
	// task names the work this process runs, for the choice's "Now:" line, "" when
	// none; question is the Quit question for it, "" when none.
	task, question func() string
	// idle reports that nothing runs, no wizard is open and no Desktop page was opened
	// or asked for in this run (MB-16 as the S1 fix round's F2 extends it).
	idle func() bool
	// hasArchives reports whether an archive is set up (the default item, §6.1).
	hasArchives func() bool
	// bundleVersion is the version of the program on disk (macOS reopen, MB-14); nil
	// or "" when it cannot be read.
	bundleVersion func() string

	mu         sync.Mutex
	choosing   bool
	lastReopen time.Time
	now        func() time.Time
}

// closing reports whether this instance has begun to stop (A7).
func (f *front) closing() bool { return f.life != nil && f.life.Context().Err() != nil }

// gate answers a proven request before anything is opened or shown (instance.Handoff's
// Gate): a closing instance answers "closing" (A7); a request from another sign-in, or
// one whose sign-in or this instance's cannot be read, is answered with that reason so
// nothing shows on a screen the person is not at (R50; A3, fail closed). A first-form
// request (Act "") carries no sign-in, and a quiet open only hands the launch a link to
// show in its own session: only "closing" applies to them.
func (f *front) gate(r instance.ActRequest) string {
	if f.closing() {
		return instance.ReasonClosing
	}
	if r.Act == "" || (r.Act == instance.ActOpen && r.Quiet) || sessionsSeparate {
		return ""
	}
	switch {
	case r.Session == "" || f.session == "":
		return instance.ReasonUnknownSession
	case r.Session != f.session:
		return instance.ReasonOtherSession
	}
	return ""
}

// act answers a proven hand-off's action (instance.Handoff.Act): it returns at once,
// and the window, when shown, belongs to the instance.
func (f *front) act(r instance.ActRequest) (bool, string) {
	if reason := f.gate(r); reason != "" {
		return false, reason
	}
	if !canShowDialogs() {
		return false, instance.ReasonCantShow
	}
	switch r.Act {
	case instance.ActChoose:
		return f.offerChoice(newerVersion(r.Version, f.version), r.Token)
	case instance.ActWizard:
		release, held := wizard.LockWizardIn(f.dir)
		if held {
			return false, instance.ReasonAlreadyOpen
		}
		go func() {
			f.wizardHeld(release)
			f.endIfIdle()
		}()
		return true, ""
	}
	return false, instance.ReasonCantShow
}

// offerChoice shows the choice unless one is already showing. token is the launch's
// Wayland activation token ("" for none, §7.4).
func (f *front) offerChoice(newer bool, token string) (bool, string) {
	f.mu.Lock()
	if f.choosing {
		f.mu.Unlock()
		return false, instance.ReasonAlreadyOpen
	}
	f.choosing = true
	f.mu.Unlock()
	go f.showChoice(newer, token)
	return true, ""
}

// reopen answers macOS's reopen (the app's icon clicked while it runs): the choice,
// at most once a second (MB-17), comparing this program's version with the one on disk
// (MB-14). A copy that is closing shows nothing (A7).
func (f *front) reopen() {
	if f.closing() {
		return
	}
	f.mu.Lock()
	now := f.clock()
	if !f.lastReopen.IsZero() && now.Sub(f.lastReopen) < time.Second {
		f.mu.Unlock()
		return
	}
	f.lastReopen = now
	f.mu.Unlock()
	disk := ""
	if f.bundleVersion != nil {
		disk = f.bundleVersion()
	}
	if !canShowDialogs() {
		f.open()
		return
	}
	f.offerChoice(newerVersion(disk, f.version), "")
}

func (f *front) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// view is what the choice window shows now: one line at most, the newer-installed line
// before the "Now:" line (the Quit item it comes with asks about the work anyway).
func (f *front) view(newer bool) choiceView {
	v := choiceView{DefaultItem: choiceDesktopItem, Quit: newer}
	if f.hasArchives != nil && !f.hasArchives() {
		v.DefaultItem = choiceWizardItem
	}
	switch {
	case newer:
		v.Line = newerInstalledText
	case f.task != nil:
		if what := f.task(); what != "" {
			v.Line = nowPrefix + what + "."
		}
	}
	return v
}

// showChoice shows the choice window and does what was picked. The "choice is showing"
// state ends before the picked step runs, so a later click reaches the choice again
// while the wizard runs (the S1 friction review, F3). When the choice is dismissed, or
// the wizard it started ends, with nothing running, no wizard open and no Desktop page
// opened in this run, the instance stops: this version shows no icon to say it runs
// (MB-16, X11; extended by the lead's call on F2).
func (f *front) showChoice(newer bool, token string) {
	dialogfront.Arm() // the person clicked the app, or a launch proved itself (R39, MB-8)
	done := func() {
		f.mu.Lock()
		f.choosing = false
		f.mu.Unlock()
	}
	var pick string
	var err error
	if wizard.OpenHere() && !newer {
		// The wizard of this process is open: the click is answered with Desktop, one
		// question away, never a second list — Windows' dialog library cannot open a
		// second list window while the wizard's list is open in the same process (the S1
		// fix round's VM walk), and the list's wizard item would only say it is open (F3).
		dialogfront.Arm()
		pick = pickNone
		if wizardOpenDesktopDialog(wizardOpenDesktopText) {
			pick = pickDesktop
		}
	} else {
		pick, err = f.choose(f.view(newer), token)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mailarchive: the choice window could not be shown: "+err.Error())
		pick = pickNone
	}
	done()
	switch pick {
	case pickDesktop:
		f.open()
	case pickWizard:
		release, held := wizard.LockWizardIn(f.dir)
		if held {
			infoDialog(instance.WizardOpenText)
			f.endIfIdle()
			return
		}
		f.wizardHeld(release)
		f.endIfIdle()
	case pickQuit:
		f.personQuits()
	default:
		f.endIfIdle()
	}
}

// choose shows the choice; a launch's Wayland activation token is in the dialog
// process's environment while it starts, so the window may come to the front (§7.4;
// the S1 adversarial pass, A9).
func (f *front) choose(v choiceView, token string) (string, error) {
	dialogfront.Arm() // only showChoice calls it: the person's click or a proven launch (R39, MB-8)
	if token != "" {
		activationMu.Lock()
		os.Setenv(activationEnv, token)
		defer func() {
			os.Unsetenv(activationEnv)
			activationMu.Unlock()
		}()
	}
	return chooseDialog(f.life.Context(), v)
}

// activationEnv is the variable a Wayland app reads its activation token from.
const activationEnv = "XDG_ACTIVATION_TOKEN"

var activationMu sync.Mutex

// endIfIdle stops the instance when nothing runs, no wizard is open and no Desktop
// page was opened or asked for in this run (MB-16; F2).
func (f *front) endIfIdle() {
	if f.idle != nil && f.idle() {
		f.life.Stop()
	}
}

// wizardHeld runs the wizard in this process under the wizard lock it was given.
func (f *front) wizardHeld(release func()) {
	defer release()
	runWizardIn(func() error { f.open(); return nil })
}

// personQuits is the person's own Quit (MB-14's Quit item): while work runs it asks
// first, Keep running being the default (Q4, MB-2 item 3).
func (f *front) personQuits() {
	dialogfront.Arm() // the person pressed Quit: the question is theirs (R39, MB-8)
	if f.question != nil {
		if q := f.question(); q != "" && !quitQuestionDialog(q) {
			return
		}
	}
	f.life.Stop()
}

// newerVersion reports whether version a (the launcher's, or the program on disk) is
// newer than b (this instance's): both must read as release versions "vX.Y.Z" (the
// "v" optional, a "-suffix" ignored); a dev build or anything else is never newer.
// Capability never rests on it (MB-3); it only words MB-14's line.
func newerVersion(a, b string) bool {
	pa, oka := releaseVersion(a)
	pb, okb := releaseVersion(b)
	if !oka || !okb {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func releaseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
