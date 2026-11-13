# Lab: macOS menu-bar item in pure Go (feasibility spike for design-menubar)

Date: 2026-11-13. Machine: the owner's Mac, macOS 26.6.2 (build 25G83), Apple silicon.
Toolchain: Go 1.26.0 (darwin/arm64), `CGO_ENABLED=0`. Library: `github.com/ebitengine/purego`
v0.8.4 (fetched through the Go module proxy; the only module fetched).
Source: `docs/lab/menubar-spike/` (a separate Go module; the binaries were not committed).
Run by the design agent on branch `menubar`. Report on design-menubar §9.

## What was built
1. `spike` (`main.go`): loads AppKit with `purego.Dlopen`, makes `NSApplication` an accessory app
   (no Dock icon), registers an Objective-C class `MASpikeTarget` whose methods are Go functions
   (`objc.RegisterClass`), and shows an `NSStatusItem` with the SF Symbol "archivebox" (template),
   the title " Backing up", a tooltip, an accessibility description, and a menu of two items:
   "Open configuration window", "Run the wizard". Timed steps: at 1 s it fires the first menu item
   by code (`performActionForItemAtIndex:0`); at 1.5 s it draws its own status button into a PNG;
   at 2 s it opens the menu (`performClick:` on the button); it exits after 12 s (25 s with `stay`).
   It also implements `applicationShouldHandleReopen:hasVisibleWindows:` and logs it.
2. `Spike.app`: the same binary in a bundle (`Spike-Info.plist`, `LSUIElement` true), opened twice
   with `open`.
3. `NoLoop.app` (`noloop/main.go`, `NoLoop-Info.plist`): a plain Go program with **no** AppKit
   event loop (like `mailarchive` today) that logs its start and sleeps 15 s; opened twice with
   `open`.

## Results (from the spike's own logs)
```
13:41:06.945 start pid=18482 exe=…/.claude/spike-menubar/spike
13:41:07.018 status item shown (img=true)
13:41:07.026 applicationDidFinishLaunching
13:41:08.019 tick1: performActionForItemAtIndex:0
13:41:08.021 ACTION openConfig: fired (Go callback reached)
13:41:08.527 tick3: button 109x22 pt rendered to …/statusitem-render.png (ok=true)
13:41:09.019 tick2: opening the menu (performClick:)
13:41:13.240 tick2: menu tracking ended
13:41:19.019 exit after 12s

(Spike.app, opened, then opened again)
13:41:43.221 start pid=18558 exe=…/Spike.app/Contents/MacOS/spike
13:41:43.279 status item shown (img=true)
13:41:44.281 ACTION openConfig: fired (Go callback reached)
13:41:44.815 REOPEN: a second launch was handed to this process (hasVisibleWindows=false)

(NoLoop.app, opened, then opened again a few seconds later)
13:42:02.505 start pid=18622
13:42:17.506 exit pid=18622
```
- The status item showed; the menu's action reached Go; the menu opened and closed.
- A struct-returning call worked: `[[NSScreen mainScreen] frame]` gave 1728 × 1117 points.
- Second open of `Spike.app`: no second process; the running one got "reopen".
- Second open of `NoLoop.app`: **no second process either**, and nothing answered — the likely
  mechanism of the owner's 2026-11-12 "the app quits at once" report (design-menubar §2.4).
- Size: `spike` 3,305,026 bytes; the same program without purego (`hello/`, not committed)
  2,509,426 bytes: purego and the Objective-C glue add about 0.8 MB. `otool -L`: only
  `/usr/lib/libSystem.B.dylib` and `/usr/lib/libresolv.9.dylib`; AppKit is opened at run time.

## What did not work
- `screencapture -x -R1028,0,700,160 …` (and a small `-R0,0,200,30`) failed with "could not create
  image from rect", inside and outside the tool sandbox: the terminal has no Screen Recording
  permission on this Mac. Instead, the spike drew its own status button (a process may always draw
  its own views): `menubar-spike/statusitem-render.png` — the archive-box symbol and "Backing up",
  109 × 22 points. A real screenshot of the menu bar with the menu open is owed (lab L-M2): grant
  Screen Recording to the terminal, or take it by hand.

## Fragile or worth knowing
- Opened by code, the menu stayed open only 1–4 s: an accessory app that is not active loses its
  menu when focus moves. A person's click activates the menu normally; not a product concern.
- The spike exits with `os.Exit` from a goroutine; the product must stop through its lifecycle and
  `[NSApp terminate:]` on the main thread.
- purego's no-cgo mode depends on Go runtime internals; this run proves Go 1.26.0 with purego
  v0.8.4 only. A Go upgrade must re-run this lab.
- Objective-C signatures are unchecked at compile time: a wrong argument type crashes. The product
  wraps each call once, in one file.
