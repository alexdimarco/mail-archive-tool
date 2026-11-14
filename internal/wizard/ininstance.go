package wizard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/dialogfront"
	"mail-archive-tool/internal/instance"
)

// The wizard inside the running MailArchive (design-menubar §6.3): the instance runs it
// in a goroutine, one at a time per account (the wizard lock, MB-9), from the launcher
// choice or a hand-off. Its export runs on the instance's own life (MB-2): a stop
// cancels it and waits for it, within the stop's bound.

var (
	exportParentMu sync.Mutex
	exportParent   = context.Background()
	exportsRunning atomic.Int32
	exportsIdle    = sync.NewCond(&sync.Mutex{})
	// writesRunning counts the wizard's own writes after its export (the job file and
	// the schedule install), which a stop waits for so it never leaves half a setup
	// (the S1 adversarial pass, A4); wizardsHere counts the wizards this process holds
	// the wizard lock for (an open wizard is work: A4, friction F5 and F9).
	writesRunning atomic.Int32
	wizardsHere   atomic.Int32
)

// writeStarted and writeEnded mark one of the wizard's writes after its export.
func writeStarted() { writesRunning.Add(1) }

func writeEnded() {
	exportsIdle.L.Lock()
	writesRunning.Add(-1)
	exportsIdle.L.Unlock()
	exportsIdle.Broadcast()
}

// OpenHere reports whether this process runs a wizard now (it holds the wizard lock
// through LockWizardIn): the choice's launch, a hand-off, or Add another archive.
func OpenHere() bool { return wizardsHere.Load() > 0 }

// BindExports makes every export this process's wizard runs from now on a child of
// ctx, so a stop of the instance cancels it (MB-2 item 4). A standalone wizard never
// calls it: its exports run on the background context, as before.
func BindExports(ctx context.Context) {
	exportParentMu.Lock()
	exportParent = ctx
	exportParentMu.Unlock()
}

func exportContext() (context.Context, context.CancelFunc) {
	exportParentMu.Lock()
	p := exportParent
	exportParentMu.Unlock()
	return context.WithCancel(p)
}

// exportStarted and exportEnded count the exports running in this process, so a stop
// can wait for them (WaitExports) and the Quit question can name one (ExportsRunning).
func exportStarted() { exportsRunning.Add(1) }

func exportEnded() {
	exportsIdle.L.Lock()
	exportsRunning.Add(-1)
	exportsIdle.L.Unlock()
	exportsIdle.Broadcast()
}

// ExportsRunning reports whether a wizard export runs in this process.
func ExportsRunning() bool { return exportsRunning.Load() > 0 }

// WaitExports waits until no wizard export, and none of the wizard's writes after it,
// runs in this process, or ctx ends (a stop's bound): the drain the instance registers
// for the wizard (MB-2 item 4; the S1 adversarial pass, A4). A wizard that only waits
// for an answer is not waited for.
func WaitExports(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		exportsIdle.L.Lock()
		for (exportsRunning.Load() > 0 || writesRunning.Load() > 0) && ctx.Err() == nil {
			exportsIdle.Wait()
		}
		exportsIdle.L.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		exportsIdle.Broadcast() // let the waiter see ctx's end
		return ctx.Err()
	}
}

// The first list's question: the standalone wizard's (MA-651), and, from the launcher
// choice or a hand-off, one that does not repeat the choice's own "What would you like
// to do?" (the S1 friction review, F10).
const (
	surfacePrompt  = "What would you like to do?"
	launcherPrompt = "What mail do you want to add or back up?"
)

// firstListDialog shows the wizard's first list; a package variable so a test can see
// the question it asks (an operating-system seam: the native list).
var firstListDialog = zenity.List

// launcherChoices is the first list of a wizard reached from the launcher choice or a
// hand-off (Q2, design-menubar §6.2): the "open MailArchive Desktop" item is dropped,
// since the choice already offers Desktop; the two tasks that finish on a Desktop page
// stay. It never has fewer than two items.
func launcherChoices() []string {
	var out []string
	for _, c := range surfaceChoices {
		if c != surfaceDesktopItem {
			out = append(out, c)
		}
	}
	if len(out) < 2 {
		return surfaceChoices
	}
	return out
}

// RunIn runs the wizard inside the running MailArchive, from the launcher choice or a
// hand-off (design-menubar §6.3): Run's body without the process-level parts. The
// first list drops its Desktop item (Q2); its Desktop-page tasks open Desktop through
// openDesktop, the instance's own fresh link, never desktop.Launch (which would try
// to take the lock the instance holds). A panic is shown in a dialog, as Run shows
// one, and the instance goes on. It returns the wizard's exit code. The caller holds
// the wizard lock.
func RunIn(openDesktop func() error) (code int) {
	dialogfront.Arm() // the person picked the wizard (a choice, or a proven hand-off): R39, MB-8
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mailarchive wizard: %v\n", r)
			_ = zenity.Error(fmt.Sprintf("Something went wrong:\n\n%v", r), zenity.Title(appTitle))
			code = 1
		}
	}()
	if refusal, ok := dialogPreflight(runtime.GOOS, dialogToolOnPath(), displayIsSet()); !ok {
		fmt.Fprintln(os.Stderr, refusal)
		return 1
	}
	open := func() error {
		if openDesktop == nil {
			return zenity.Info(openDesktopUnwired, zenity.Title(appTitle))
		}
		return openDesktop()
	}
	switch err := wizardWith(launcherPrompt, launcherChoices(), open); {
	case err == nil, errors.Is(err, zenity.ErrCanceled):
		return 0
	default:
		fmt.Fprintln(os.Stderr, "mailarchive wizard: "+err.Error())
		_ = zenity.Error(err.Error(), zenity.Title(appTitle))
		return 1
	}
}

// HandOff, when set (the cmd layer wires it), is asked by the `wizard` verb before it
// shows anything: it reaches this account's running MailArchive and asks it to run the
// wizard (D5), returning true when that launch is handled — the instance shows it, or
// the person was told why not. false runs the wizard here, under the wizard lock.
var HandOff func() bool

// LockWizard takes the per-account wizard lock (MB-9) beside desktop.lock. It returns
// the release, or held true when another wizard of this account is open, in this
// process or another; a failure to take it for another reason (no account folder)
// runs the wizard unlocked, as before. A variable so a test can point it elsewhere.
var LockWizard = func() (release func(), held bool) {
	dir, err := instance.Dir()
	if err != nil {
		return func() {}, false
	}
	return LockWizardIn(dir)
}

// LockWizardIn is LockWizard for the account folder dir.
func LockWizardIn(dir string) (release func(), held bool) {
	l, err := instance.AcquireWizard(dir)
	if err != nil {
		if instance.IsHeld(err) {
			return nil, true
		}
		return func() {}, false
	}
	wizardsHere.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = l.Release()
			wizardsHere.Add(-1)
		})
	}, false
}
