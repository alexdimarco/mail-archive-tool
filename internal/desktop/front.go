package desktop

import (
	"fmt"
	"os"
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
// shows a small native list, "Run the wizard" or "Open MailArchive Desktop", when it
// starts from an app icon (`start`), when a later launch hands off with `choose`, and
// on macOS when the app is opened again (reopen). It runs the wizard in-process, one
// at a time per account (the wizard lock), with its Desktop item dropped (Q2). Each
// dialog comes only at the person's request: the start they clicked, a hand-off whose
// proof was verified, or the reopen macOS sent for their click (R39, MB-8).

// The choice window's words (Q1, design-menubar §6.1).
const (
	choiceTitle       = "MailArchive"
	choiceText        = "What would you like to do?"
	choiceWizardItem  = "Run the wizard — add or back up an archive step by step"
	choiceDesktopItem = "Open MailArchive Desktop — your archives, schedules and settings, in your browser"
	// choiceQuitItem is MB-14's Quit, offered only with the newer-installed line.
	choiceQuitItem = "Quit MailArchive — then open it again to use the newer version"
	// newerInstalledText is MB-14's line: the running copy is older than the program
	// on disk; it never restarts itself (R33).
	newerInstalledText = "A newer MailArchive is installed. Quit and open MailArchive again to use it."
	// choiceOpenText is what a launch says when the choice is already on the screen.
	choiceOpenText = "MailArchive is already open, and its window is on your screen. Look for it there or in the taskbar."
	// otherSessionText is R50's message, in the launcher's own session, when the
	// running copy belongs to another sign-in of this account (the fold's third
	// contradiction).
	otherSessionText = "MailArchive is already open in another sign-in of this account. Use it there, or quit it there to open it here."
)

// What the person picked in the choice window.
const (
	pickWizard  = "wizard"
	pickDesktop = "desktop"
	pickQuit    = "quit"
	pickNone    = "" // Cancel, or the window closed
)

// choiceView is what the choice window shows: optional lines above the question,
// which item is the default, and whether it offers a Quit button (MB-14).
type choiceView struct {
	Lines       []string
	DefaultItem string
	Quit        bool
}

// Text is the window's text: its lines, then the question.
func (v choiceView) Text() string {
	return strings.Join(append(append([]string(nil), v.Lines...), choiceText), "\n\n")
}

// The dialog seams, package variables so the dialog walk (MA-1111, MA-1117) links each
// to the declarations that use it, and a test replaces them. The real ones are in
// front_dialogs.go.
var (
	chooseDialog       = zenityChoose
	infoDialog         = zenityInfo
	quitQuestionDialog = zenityQuitQuestion
	runWizardIn        = wizard.RunIn
	// canShowDialogs says whether this process can show the native dialogs: macOS and
	// Windows always can; Linux needs a dialog tool and a display (MB-9).
	canShowDialogs = dialogsPossible
)

// front is the instance's side of the choice: who it is, how it opens Desktop, and
// what it is showing now.
type front struct {
	life    *Lifecycle
	dir     string // the account folder (the wizard lock lives there)
	session string // this instance's sign-in session (instance.GUISession)
	version string
	// open opens Desktop in the browser at a fresh one-time link, or shows the link.
	open func()
	// busy is the Quit question for the work this process runs, "" when none.
	busy func() string
	// idle reports that nothing runs and no Desktop page was opened (MB-16).
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

// act answers a proven hand-off's action (instance.Handoff.Act): it returns at once,
// and the window, when shown, belongs to the instance.
func (f *front) act(r instance.ActRequest) (bool, string) {
	if r.Session != "" && f.session != "" && r.Session != f.session {
		return false, instance.ReasonOtherSession
	}
	if !canShowDialogs() {
		return false, instance.ReasonCantShow
	}
	switch r.Act {
	case instance.ActChoose:
		return f.offerChoice(newerVersion(r.Version, f.version), false)
	case instance.ActWizard:
		release, held := wizard.LockWizardIn(f.dir)
		if held {
			return false, instance.ReasonAlreadyOpen
		}
		go f.wizardHeld(release)
		return true, ""
	}
	return false, instance.ReasonCantShow
}

// offerChoice shows the choice unless one is already showing.
func (f *front) offerChoice(newer, first bool) (bool, string) {
	f.mu.Lock()
	if f.choosing {
		f.mu.Unlock()
		return false, instance.ReasonAlreadyOpen
	}
	f.choosing = true
	f.mu.Unlock()
	go f.showChoice(newer, first)
	return true, ""
}

// reopen answers macOS's reopen (the app's icon clicked while it runs): the choice,
// at most once a second (MB-17), comparing this program's version with the one on disk
// (MB-14).
func (f *front) reopen() {
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
	f.offerChoice(newerVersion(disk, f.version), false)
}

func (f *front) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// view is what the choice window shows now.
func (f *front) view(newer bool) choiceView {
	v := choiceView{DefaultItem: choiceDesktopItem, Quit: newer}
	if f.hasArchives != nil && !f.hasArchives() {
		v.DefaultItem = choiceWizardItem
	}
	if newer {
		v.Lines = append(v.Lines, newerInstalledText)
	}
	if f.busy != nil {
		if q := f.busy(); q != "" {
			// MB-15: what runs, never an archive's name.
			v.Lines = append(v.Lines, "Now: "+busyNow(q)+".")
		}
	}
	return v
}

// showChoice shows the choice window and does what was picked. first is a choice
// shown by the launch that started this instance: its Cancel, with nothing running
// and no Desktop page opened, stops the instance, since this version shows no icon to
// say it runs (MB-16, X11).
func (f *front) showChoice(newer, first bool) {
	dialogfront.Arm() // the person clicked the app, or a launch proved itself (R39, MB-8)
	defer func() {
		f.mu.Lock()
		f.choosing = false
		f.mu.Unlock()
	}()
	pick, err := chooseDialog(f.view(newer))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mailarchive: the choice window could not be shown: "+err.Error())
		pick = pickNone
	}
	switch pick {
	case pickDesktop:
		f.open()
	case pickWizard:
		release, held := wizard.LockWizardIn(f.dir)
		if held {
			infoDialog(instance.WizardOpenText)
			return
		}
		f.wizardHeld(release)
	case pickQuit:
		f.personQuits()
	default:
		if first && f.idle != nil && f.idle() {
			f.life.Stop()
		}
	}
}

// wizardHeld runs the wizard in this process under the wizard lock it was given.
func (f *front) wizardHeld(release func()) {
	defer release()
	runWizardIn(func() error { f.open(); return nil })
}

// personQuits is the person's own Quit (MB-14's Quit button): while work runs it asks
// first, Keep running being the default (Q4, MB-2 item 3).
func (f *front) personQuits() {
	dialogfront.Arm() // the person pressed Quit: the question is theirs (R39, MB-8)
	if f.busy != nil {
		if q := f.busy(); q != "" && !quitQuestionDialog(q) {
			return
		}
	}
	f.life.Stop()
}

// busyNow turns the Quit question's "MailArchive is busy (X). …" into X, for the
// choice window's "Now:" line.
func busyNow(q string) string {
	if i, j := strings.Index(q, "("), strings.Index(q, ")"); i >= 0 && j > i {
		return q[i+1 : j]
	}
	return "MailArchive is busy"
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
