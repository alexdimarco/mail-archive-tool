//go:build darwin

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"mail-archive-tool/internal/util"
)

// The macOS event loop of the instance (design-menubar §7.2, §8.2; slice S1). A Go
// program in a .app bundle that never runs AppKit's event loop cannot answer Launch
// Services: opened again while it runs, macOS starts no second process and sends the
// running one a "reopen" it never reads (the owner's 2026-11-12 report; error -600
// from `open -a`, confirmed on the owner's Mac, §2.4 MB-4). So the instance runs
// [NSApp run] on the main thread, with a delegate that answers:
//   - applicationShouldHandleReopen:hasVisibleWindows: — the launcher choice (§6.1);
//   - applicationShouldTerminate: — sign-out and installers quit an NSApplication
//     through an Apple Event, and AppKit's own path ends in exit(), skipping the Go
//     drain: the delegate answers NSTerminateLater, runs the one stop (no question
//     asked), and replies YES on the main thread when the serving has drained, or when
//     the bound runs out (MB-2).
// No status item yet (slice S4). Activation policy "accessory": no Dock icon. Every
// Objective-C call is here, on the main thread; purego calls AppKit with no cgo.

// AppKit's values the loop uses.
const (
	nsActivationPolicyAccessory = 1
	nsTerminateNow              = 1
	nsTerminateLater            = 2
	nsEventTypeApplicationDef   = 15
)

// terminateBound is how long a terminate request waits for the serving to drain.
const terminateBound = DrainBudget + 2*time.Second

func init() { appLoop = darwinLoop }

// allowForeground: macOS has no such rule; Launch Services activates the app.
var allowForeground func(pid int)

func sel(s string) objc.SEL { return objc.RegisterName(s) }

type nsPoint struct{ X, Y float64 }

var darwinClassOnce sync.Once

// darwinLoop runs serve in a goroutine and AppKit's event loop on the calling thread,
// which must be the process's main thread (cmd/mailarchive locks it in init on macOS).
// When AppKit cannot be loaded it serves directly.
func darwinLoop(serve func() error, ev LoopEvents) error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return serve()
	}
	app := objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), nsActivationPolicyAccessory)

	var (
		mu     sync.Mutex
		served bool
	)
	done := make(chan struct{})
	var serr error

	stopLoop := func() {
		// [NSApp stop:] takes effect after the next event: post an empty one.
		app.Send(sel("stop:"), objc.ID(0))
		e := objc.ID(objc.GetClass("NSEvent")).Send(sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			nsEventTypeApplicationDef, nsPoint{}, uint64(0), float64(0), int64(0), objc.ID(0), int16(0), int64(0), int64(0))
		app.Send(sel("postEvent:atStart:"), e, true)
	}

	var cls objc.Class
	var err error
	darwinClassOnce.Do(func() {
		cls, err = objc.RegisterClass("MailArchiveAppDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"), Fn: func(self objc.ID, _ objc.SEL, a objc.ID, visible bool) bool {
				fmt.Fprintln(os.Stderr, "mailarchive: macOS opened the app again; showing the choice")
				if ev.Reopen != nil {
					go ev.Reopen()
				}
				return false
			}},
			{Cmd: sel("applicationShouldTerminate:"), Fn: func(self objc.ID, _ objc.SEL, a objc.ID) uint64 {
				mu.Lock()
				over := served
				mu.Unlock()
				if over {
					return nsTerminateNow
				}
				go func() {
					if ev.EndSession != nil {
						ev.EndSession(terminateBound)
					}
					self.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("maReplyTerminate:"), objc.ID(0), false)
				}()
				return nsTerminateLater
			}},
			{Cmd: sel("maReplyTerminate:"), Fn: func(self objc.ID, _ objc.SEL, a objc.ID) {
				app.Send(sel("replyToApplicationShouldTerminate:"), true)
			}},
			{Cmd: sel("maStop:"), Fn: func(self objc.ID, _ objc.SEL, a objc.ID) { stopLoop() }},
		})
	})
	if err != nil || cls == 0 {
		fmt.Fprintln(os.Stderr, "mailarchive: the macOS event loop could not start; serving without it")
		return serve()
	}
	delegate := objc.ID(cls).Send(sel("new"))
	app.Send(sel("setDelegate:"), delegate)

	go func() {
		serr = serve()
		mu.Lock()
		served = true
		mu.Unlock()
		close(done)
		// The serving ended (Shut down, Ctrl-C): end the event loop on the main thread so
		// Launch returns and the process exits through Go.
		delegate.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("maStop:"), objc.ID(0), false)
	}()
	fmt.Fprintln(os.Stderr, "mailarchive: the macOS event loop runs (the app's later opens reach this process)")
	app.Send(sel("run"))
	<-done
	return serr
}

// shortVersionRe finds CFBundleShortVersionString's value in an Info.plist.
var shortVersionRe = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([0-9A-Za-z.\-+]{1,40})</string>`)

// bundleVersionOnDisk is the version of the .app on disk that this program runs from,
// read from its Info.plist (bounded, regular file, never through a link), "" when the
// program is not in a bundle or the version cannot be read (MB-14).
func bundleVersionOnDisk() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return ""
	}
	plist := filepath.Join(exe[:i+len(".app")], "Contents", "Info.plist")
	b, err := util.ReadRegularFile(plist, 64<<10)
	if err != nil {
		return ""
	}
	m := shortVersionRe.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return "v" + string(m[1])
}
