//go:build darwin

// Spike (design-menubar §9): a pure-Go (CGO_ENABLED=0) macOS menu-bar item with a
// two-item menu, through the Objective-C runtime via github.com/ebitengine/purego.
// Not product code. It proves, on this Mac:
//   - an NSStatusItem shows, with a template SF Symbol image, a title and an
//     accessibility description;
//   - a menu item's target/action reaches a Go function (performActionForItemAtIndex:);
//   - the menu opens (the status button's performClick:) for the screenshot;
//   - when built into a .app and opened a second time, LaunchServices re-opens the
//     running process (applicationShouldHandleReopen:hasVisibleWindows:) instead of
//     starting a second one — the hand-off the design relies on.
//
// Every line goes to spike.log beside the binary (or beside the .app), since a
// process started by `open` has no terminal.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func init() { runtime.LockOSThread() } // AppKit lives on the main thread

var (
	logf   *os.File
	menu   objc.ID
	button objc.ID
	app    objc.ID
	logDir string
)

func sel(s string) objc.SEL { return objc.RegisterName(s) }

// nsstr makes an autoreleased NSString from a Go string (a NUL-terminated copy).
func nsstr(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)
	return id
}

func logln(format string, a ...any) {
	line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, a...)
	fmt.Println(line)
	if logf != nil {
		fmt.Fprintln(logf, line)
	}
}

type cgRect struct{ X, Y, W, H float64 }

func main() {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if i := strings.Index(dir, ".app/Contents/MacOS"); i >= 0 {
		dir = filepath.Dir(dir[:i+len(".app")])
	}
	logDir = dir
	logf, _ = os.OpenFile(filepath.Join(dir, "spike.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	logln("start pid=%d exe=%s", os.Getpid(), exe)

	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		logln("dlopen AppKit: %v", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "screen" {
		scr := objc.ID(objc.GetClass("NSScreen")).Send(sel("mainScreen"))
		r := objc.Send[cgRect](scr, sel("frame"))
		fmt.Printf("%.0f %.0f\n", r.W, r.H)
		return
	}

	app = objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), 1) // NSApplicationActivationPolicyAccessory: no Dock icon

	cls, err := objc.RegisterClass("MASpikeTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("openConfig:"), Fn: func(self objc.ID, _ objc.SEL, sender objc.ID) { logln("ACTION openConfig: fired (Go callback reached)") }},
		{Cmd: sel("runWizard:"), Fn: func(self objc.ID, _ objc.SEL, sender objc.ID) { logln("ACTION runWizard: fired") }},
		{Cmd: sel("tick1:"), Fn: func(self objc.ID, _ objc.SEL, arg objc.ID) {
			logln("tick1: performActionForItemAtIndex:0")
			menu.Send(sel("performActionForItemAtIndex:"), 0)
		}},
		{Cmd: sel("tick2:"), Fn: func(self objc.ID, _ objc.SEL, arg objc.ID) {
			logln("tick2: opening the menu (performClick:)")
			button.Send(sel("performClick:"), objc.ID(0))
			logln("tick2: menu tracking ended")
		}},
		{Cmd: sel("tick3:"), Fn: func(self objc.ID, _ objc.SEL, arg objc.ID) {
			// Render the status button into a PNG ourselves: a process may draw its own
			// views without the Screen Recording permission screencapture needs.
			b := objc.Send[cgRect](button, sel("bounds"))
			rep := button.Send(sel("bitmapImageRepForCachingDisplayInRect:"), b)
			if rep == 0 {
				logln("tick3: no bitmap rep")
				return
			}
			button.Send(sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
			dict := objc.ID(objc.GetClass("NSDictionary")).Send(sel("dictionary"))
			data := rep.Send(sel("representationUsingType:properties:"), 4, dict) // NSBitmapImageFileTypePNG
			p := filepath.Join(logDir, "statusitem-render.png")
			ok := data.Send(sel("writeToFile:atomically:"), nsstr(p), true)
			logln("tick3: button %.0fx%.0f pt rendered to %s (ok=%v)", b.W, b.H, p, ok != 0)
		}},
		{Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"), Fn: func(self objc.ID, _ objc.SEL, a objc.ID, visible bool) bool {
			logln("REOPEN: a second launch was handed to this process (hasVisibleWindows=%v)", visible)
			return false
		}},
		{Cmd: sel("applicationDidFinishLaunching:"), Fn: func(self objc.ID, _ objc.SEL, n objc.ID) { logln("applicationDidFinishLaunching") }},
	})
	if err != nil {
		logln("RegisterClass: %v", err)
		os.Exit(1)
	}
	target := objc.ID(cls).Send(sel("new"))
	app.Send(sel("setDelegate:"), target)

	statusBar := objc.ID(objc.GetClass("NSStatusBar")).Send(sel("systemStatusBar"))
	item := statusBar.Send(sel("statusItemWithLength:"), float64(-1)) // NSVariableStatusItemLength
	item.Send(sel("retain"))
	button = item.Send(sel("button"))
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), nsstr("archivebox"), nsstr("MailArchive: backing up"))
	if img != 0 {
		img.Send(sel("setTemplate:"), true)
		button.Send(sel("setImage:"), img)
	}
	button.Send(sel("setTitle:"), nsstr(" Backing up"))
	button.Send(sel("setToolTip:"), nsstr("MailArchive — backing up “Work mail” (spike)"))

	menu = objc.ID(objc.GetClass("NSMenu")).Send(sel("new"))
	add := func(title, action string) objc.ID {
		var s objc.SEL
		if action != "" {
			s = sel(action)
		}
		mi := objc.ID(objc.GetClass("NSMenuItem")).Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"), nsstr(title), s, nsstr(""))
		if action != "" {
			mi.Send(sel("setTarget:"), target)
		}
		menu.Send(sel("addItem:"), mi)
		return mi
	}
	add("Open configuration window", "openConfig:")
	add("Run the wizard", "runWizard:")
	menu.Send(sel("setAutoenablesItems:"), false)
	item.Send(sel("setMenu:"), menu)
	logln("status item shown (img=%v)", img != 0)

	stay := 12 * time.Second
	if len(os.Args) > 1 && os.Args[1] == "stay" {
		stay = 25 * time.Second
	}
	if len(os.Args) <= 1 || os.Args[1] != "noclick" {
		target.Send(sel("performSelector:withObject:afterDelay:"), sel("tick1:"), objc.ID(0), float64(1.0))
		target.Send(sel("performSelector:withObject:afterDelay:"), sel("tick3:"), objc.ID(0), float64(1.5))
		target.Send(sel("performSelector:withObject:afterDelay:"), sel("tick2:"), objc.ID(0), float64(2.0))
	}
	go func() {
		time.Sleep(stay)
		logln("exit after %s", stay)
		os.Exit(0)
	}()
	app.Send(sel("run"))
}
