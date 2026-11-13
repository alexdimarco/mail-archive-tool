# Design: the MailArchive indicator, the launcher choice, and one running copy

Status (2026-11-13, revision 1): DESIGN ONLY, NOT REVIEWED, NOT BUILT. Nothing here exists in the
code yet except the macOS feasibility spike recorded in
`docs/lab/menubar-spike-macos-2026-11-13.md` (its source is in `docs/lab/menubar-spike/`, a
separate Go module so the product build never compiles it).

- Owed before any code: the **10-lens pre-code design review** (this changes how every launch
  starts, adds a long-lived OS integration on three platforms, and extends the hand-off protocol:
  it is architecture- and security-relevant). Not run by the author.
- Owed during the build: an **adversarial pass** on the hand-off and IPC changes (slice S1) and on
  the Linux D-Bus surface (slice S5); a **friction review** of every shippable slice, **Windows
  first** (Edge on the Windows VM); the UX contract rows for the new words.
- Owed from the owner: the answers to §13 (Q1–Q9). Each recommended answer is what gets built if
  the owner agrees; each alternative is named beside its question.
- Order (owner, 2026-11-13): build after the Gmail duplicates work, before the cloud-copy design.

Base: `main` at 1a6d437 (v1.7.1). Branch `menubar`.

---

## 1. The request, and what it means

Owner, 2026-11-13, verbatim:

> "1. When mail archive is running there is no indication of this - maybe a proper indicator in the
> taskbar that has a dropdown menu for the configuration window and the wizard? 2: When clicking the
> start icon it opens up a better choice option 1:wizard (which gives the options minus the
> configuration window) 2:configuration window. If it is running it opens up the existing running
> application (with the same choices) and doesnt start a second instance."

Read plainly, three things:

1. **An indicator.** While MailArchive runs, a small icon sits in the menu bar (Mac), the
   notification area of the taskbar (Windows), or the panel's tray (Linux). Its menu opens the
   configuration window (MailArchive Desktop, the page in the browser) or the wizard, and says what
   is happening now.
2. **A launcher choice.** Clicking the app's own icon (Applications, Start menu, app grid) shows one
   small window: "Run the wizard" or "Open MailArchive Desktop". The wizard then offers its usual
   options *minus* the one that just opens Desktop, since the choice window already offers it.
3. **One running copy.** If MailArchive already runs, clicking its icon reaches that copy, which
   shows the same choice. No second copy starts.

Related owner reports this design also answers:
- 2026-11-12: the Mac app (1.5.9), opened from Applications, "quit at once" while an older Desktop
  was running. Cause found in §2.4.
- 2026-11-12: a Check integrity held the archive with no visible sign anywhere but the activity
  log, and Restore then said "a backup is running" (the wrong task). The state model in §5.3 names
  the real task, and is shared with that polish fix.

## 2. How MailArchive starts today

### 2.1 One program, many front doors
Every function is a verb of one executable (`mailarchive`; on Windows also the no-console build
`mailarchivew.exe`) and is reached in-process, never by starting a copy of itself (R33,
`cmd/mailarchive/dispatch.go`). The GUI verbs are `desktop` (the dashboard and archive reader,
served on loopback behind a one-time private link), `wizard` (native dialogs through
`github.com/ncruces/zenity`) and `run-job` (a scheduled backup, no window).

| Platform | What the person clicks | What runs |
|---|---|---|
| macOS | "Mail Archive" in Applications | `Contents/MacOS/mailarchive-gui` (a symlink to the program; a former name), with no arguments, so the **wizard** (`resolveGUIFormerName`). The wizard's first list can open Desktop **in the same process** (`wizard.OpenDesktop`). |
| Windows | Start menu "MailArchive Desktop", the desktop shortcut | `mailarchivew.exe desktop` |
| Windows | Start menu "MailArchive wizard" | `mailarchivew.exe wizard` |
| Windows | (nothing; at every sign-in) | the MSI's **all-users Startup shortcut**: `mailarchivew.exe desktop -no-browser`, a background Desktop with no window and no sign. |
| Linux | "MailArchive Desktop" in the app grid | `mailarchive desktop` (`packaging/linux/mailarchive.desktop`). A no-argument launch is the usage refusal (row L1 dormant). |

### 2.2 The single-instance machinery that already exists (Desktop only)
`internal/instance`: per account and logon session, the Desktop holds `desktop.lock` for its life
and writes a 0600 instance file `desktop-instance.json` (its loopback address, its version, and a
random key K). A second `desktop` launch that finds the lock held reads the file, calls
`POST /_handoff` on the running Desktop with an HMAC proof made with K, and the running Desktop
opens a browser at a fresh one-time link in its own session (or returns the link to show in a
dialog). Both directions are proven with K; at most one hand-off a second
(`internal/instance/handoff.go`, `internal/desktop/launch.go` `runInstance`/`handoff`).

**The wizard takes no part in it.** Two wizards can run at once; a wizard can run beside a
Desktop; the macOS .app process can *become* the Desktop (wizard → "open MailArchive Desktop").

### 2.3 What the person sees of a running MailArchive today
Nothing outside the browser tab. A Desktop with no tab open (the Windows Startup launch, or a tab
closed) runs invisibly. A scheduled backup (`run-job`, or `desktop -capture`, started by launchd,
Task Scheduler or systemd) is a separate short process with no window. Inside the page, a running
task shows on its button ("Starting…") only until it begins, then only in the activity log.

### 2.4 The 2026-11-12 "app quits at once" report: likely cause
The spike (§9) tested it on this Mac with a plain Go program in a .app bundle that, like
MailArchive today, never starts an AppKit event loop (`docs/lab/menubar-spike/noloop`): opened
once, it runs; **opened again while it runs, macOS starts no second process** — Launch Services
treats the bundle as already running and sends the running process a "reopen" message, which a
process with no event loop never answers. To the person, the second click does nothing visible
(the Dock icon may bounce once), which reads as "it quit at once".

The owner's case fits: the old Desktop was running *inside the .app's process* (macOS reaches
Desktop only through the .app's wizard, in-process), so a later click on the app in Applications was
routed to that process and ignored. **Likely, not proven on the owner's build**: lab L-M1 (§12.3)
repeats it with the real 1.7.1 .app, and with two copies of the app at different paths.

The fix is part of this design: the MailArchive process on macOS runs an AppKit event loop and
answers "reopen" by showing the launcher choice (§6, §7.2). The spike proved a pure-Go process
receives that message (`REOPEN: a second launch was handed to this process`).

Caveat for the first upgrade: while an *older* MailArchive (one without the event loop) still runs,
macOS keeps routing clicks to it, so the new version's fix cannot help until the old one is quit
once (Desktop's Shut down button, or signing out). The release notes and the existing
"another version is still running" notice say so (§7.4).

## 3. Decisions in one place

| # | Decision | Section |
|---|---|---|
| D1 | **One MailArchive process per account and sign-in session** ("the instance"): it holds `desktop.lock`, serves Desktop exactly as today, shows the indicator, shows the launcher choice, and runs the wizard. Every GUI launch either becomes it or hands off to it and exits. | §4 |
| D2 | The indicator **lives in the instance only**. A scheduled backup started by the OS scheduler never shows an icon of its own; when the instance runs, it *sees* such runs through the archive locks. With no instance running, nothing shows (as today). | §5.4 |
| D3 | A new launcher verb **`mailarchive start`** (and `start -background` for sign-in) is what every app icon and shortcut runs; the macOS .app's no-argument launch becomes `start`. | §6, §7 |
| D4 | The hand-off gains an **action** — `open` (today's: open Desktop), `choose` (show the launcher choice), `wizard` — covered by the MAC; an older instance that does not know actions is handled by version. | §7.1 |
| D5 | The `wizard` verb **hands off to a running instance** instead of running beside it. One wizard at a time per account session. | §6.3 |
| D6 | State comes from (a) the instance's own job slot, given a **task kind**, (b) a **bounded probe of each known archive's lock** and its verb, (c) the same health facts the "Your archives" card shows. Never from activity-log lines. | §5.3 |
| D7 | The menu and tooltip show **only fixed words, archive names, task kinds and times** — never mail content, folder names, log lines or the private link. Archive names appear inside the menu only, not in the hover tooltip (Q5). | §10.2 |
| D8 | Pure Go, no cgo: Windows through `golang.org/x/sys/windows` (already a dependency); macOS through `github.com/ebitengine/purego` (new, proven by the spike); Linux through `github.com/godbus/dbus/v5` (new) with a no-icon fallback. No helper executable. | §8 |
| D9 | **Quit asks first while a task of this process runs** ("Keep running" is the default). Quitting never stops schedules. | §10.4 |
| D10 | Start at sign-in: Windows keeps the MSI's Startup shortcut (now visible as the icon) with a per-user "don't open at sign-in"; macOS and Linux **opt in**, default off (Q3). | §10.5 |

## 4. The one MailArchive process (the instance)

Today's Desktop process already is a per-account singleton with a lock, a key, a lifecycle with a
graceful stop (`Lifecycle.Stop`, the Shut down button, MA-695) and a hand-off. This design makes it
the *only* long-lived GUI process and gives it three more jobs:

1. **The indicator** (§5), on the OS thread the platform requires (the main thread on macOS; a
   locked thread with its own message loop on Windows; a D-Bus connection on Linux).
2. **The launcher choice** (§6), shown when the instance starts from an app icon, when a later
   launch hands off with `choose`, or when macOS sends "reopen".
3. **The wizard** (§6.3), run in-process in a goroutine, one at a time, as Desktop's "Add another
   archive" already runs `wizard.AddArchive` (MA-696). Its "open Desktop" step opens the instance's
   own one-time link in the browser; it never calls `desktop.Launch` (which would try to take the
   lock the instance already holds).

Nothing about the dashboard changes: same loopback listeners, same private link, same session
cookie, same CSRF guard. The instance serves Desktop from start (as `-no-browser` does today), so
"Open MailArchive Desktop" is instant.

A `-no-tray` flag (and an automatic fallback when no graphical session exists, e.g. over SSH)
keeps a headless Desktop possible; tests run with it.

## 5. The indicator

### 5.1 States
One icon shape (the app's archive box, monochrome so it follows light/dark menu bars), with a
small badge for the state. The state is never shown by colour alone: the badge shape and the words
in the tooltip and menu carry it too.

| State | When | Badge | Tooltip (fixed words) |
|---|---|---|---|
| Idle | nothing running, nothing wrong | none | "MailArchive — all archives up to date" (or "— running" when no archive is set up) |
| Backing up | a backup of any archive (job slot kind `backup`, or a lock held with verb `export`/`graph`) — includes "syncing Thunderbird first" | arrows | "MailArchive — backing up 1 archive" |
| Checking | integrity check (kind `check`, verb `verify`) | magnifier | "MailArchive — checking an archive" |
| Restoring | restore to Thunderbird (kind `restore`, verb `restore-thunderbird`) | arrow out | "MailArchive — restoring to Thunderbird" |
| Rebuilding | search index rebuild (kind `rebuild`, verb `rebuild`/`reindex`) | arrows | "MailArchive — rebuilding a search index" |
| Working | a lock held whose verb is empty or unknown (an encrypted archive's run writes no verb, by design) or a delete | arrows | "MailArchive — working on an archive" |
| Needs attention | any archive has a fact the "Your archives" card already shows as a problem: source not reachable (streak), backups failing (alert streak), auto-paused, a delete that did not finish, an index behind | "!" | "MailArchive — 1 archive needs attention" |

Precedence when several hold: a running task shows its badge, and "needs attention" adds itself to
the tooltip and the menu's first line ("Backing up 1 archive · 1 needs attention"). No animation
(a still icon costs no CPU and does not distract; the badge change is the signal).

### 5.2 The menu
```
MailArchive is backing up “Work mail” — since 14:02        (disabled line, one per task, at most 3, then "and 2 more")
“Home mail” needs attention: its mail hasn’t been reachable since 3 Nov   (one line per archive, opens Desktop on it)
──────────────
Open MailArchive Desktop
Run the wizard…
Status ▸   Work mail — backed up today 14:02
           Home mail — needs attention
           (each line opens Desktop on that archive)
──────────────
Open at sign-in   ✓                                        (Q3)
Quit MailArchive
```
- "Open MailArchive Desktop" opens the browser at a fresh one-time link minted by the instance
  (the existing `sess.Link(origin, OpenerTTL)` path), falling back to the copy-link dialog when no
  browser opens, exactly as the hand-off does.
- "Run the wizard…" starts the in-process wizard (§6.3); while one is open the item reads "The
  wizard is open" and is disabled.
- "Status" is a submenu, not a new window: Desktop already is the status window. (Owner listed
  "Status"; Q1 covers the words.)
- On macOS the left click opens the menu (the platform's norm). On Windows a left click opens the
  same menu and a double click opens Desktop; right click opens the menu. On Linux the panel
  decides; `Activate` (usually left click) opens Desktop.

### 5.3 How the instance learns state
Three sources, merged into one pure value `tray.State` (a list of task lines and attention lines
plus the overall state), recomputed on change and at most every 5 seconds:

**(a) Its own job slot.** `captureState` (internal/desktop/capture.go) knows *whether* a task runs
but not *which kind*. Add a `kind` field set where each job takes the slot (`runJobOnClaiming` and
its callers: capture/Run now → `backup`, verifyAction → `check`, rebuildAction → `rebuild`,
restore into Thunderbird → `restore`, encryption copy → `copy`), plus the archive and start time.
This is exact and costs nothing. The same field fixes the polish item from 2026-11-12: buttons the
running task would refuse read "<task> running…" and refusals name the real task.

**(b) The archive locks.** For every archive in the known-archives index (`internal/archindex`),
probe `<archive>/.mailarchive.lock` with the existing read-only probe (`lockfile.Probe`: opens
without creating or writing, locks for an instant, never follows a link) and, when held, read the
holder's verb and host (`lockfile.HolderVerb`, `HolderHost`). This is what reveals runs the
instance did not start: a scheduled backup from launchd/Task Scheduler/systemd, a command-line
run, a wizard of an older version. A lock held from another computer reads "backing up (from
another computer)".

Bounds, because some archives live on slow or sleeping disks and in cloud-synced folders (the
owner's Nextcloud archive made `status` wait ~40 s on the file provider):
- each probe runs with a 2-second budget in its own goroutine; one that overruns marks that archive
  "can't tell right now" (no badge from it) and is probed again only after 5 minutes;
- an archive whose folder is not present (drive unplugged) is skipped without opening anything;
- archives marked as in a cloud-synced folder (MA-1083's facts) are probed every 60 s, not 5 s;
- the probe's instant hold is the cost the lock package already accepts (`acquireGrace` retries
  cover a run that starts in that instant); at most one probe per archive is in flight.

**(c) Attention facts.** The same reads the Desktop's "Your archives" rows and `status` use
(`ArchiveRow`: source-loss streak, alert streak, auto-pause, unfinished delete, marker; the
run-history's last outcome), recomputed once a minute and right after any task ends. Slice S2
measures the cost on a 50-archive index; the budget is under 50 ms per pass, else it moves to a
5-minute cadence.

**Never read:** the activity log lines (they carry mail folder names and sync messages), mail
files, manifests, or the search index.

### 5.4 Which process owns the icon when no Desktop runs?
None. Decided against the alternatives:
- *A scheduled run shows its own icon* — rejected. On macOS it would need an AppKit event loop in
  `run-job`; on Windows a Task Scheduler run may have no interactive desktop at all; and an icon
  appearing at 3 a.m. and vanishing minutes later is noise the person did not ask for (the spirit
  of R39). Failures of scheduled runs already reach the person through the existing notification
  and email alerts.
- *A scheduled run starts the instance when none runs* — rejected: it is "the program starting a
  copy of itself" (R33), and it would put a window-capable process in a session that may have no
  user at the screen.

So the icon shows exactly when the instance runs. Start at sign-in (§10.5) is how a person who
wants it always visible gets that.

## 6. The launcher choice

### 6.1 The window
A small native list dialog (the same zenity list the wizard's first step uses today, so no new UI
technology; it works on all three platforms and comes to the front through `dialogfront` on
Windows, R39):

```
MailArchive
What would you like to do?

  ( ) Run the wizard — add or back up an archive step by step
  (•) Open MailArchive Desktop — your archives, schedules and settings, in your browser

  [Cancel]  [OK]
```
- When MailArchive is busy, one fixed line above the list: "Now: backing up “Work mail” (since
  14:02)."
- Default item: the wizard when no archive is set up yet; Desktop otherwise.
- Cancel leaves the instance (and its icon) running. A first-ever start, cancelled, also leaves it
  running, and the icon is there to say so.
- On Linux without a dialog tool (no zenity), the chooser cannot show: `start` opens Desktop
  directly and the page says why (the wizard's existing preflight words).

### 6.2 "The wizard minus the configuration window"
Today the wizard's first list (`surfaceChoices`, internal/wizard/wizard.go) is:
1. Archive a mail file on this computer (Outlook, Thunderbird, mbox) — default
2. Microsoft 365, folder export, browsing & tools (open MailArchive Desktop)
3. Open an archive folder I already have, encrypted or not (in MailArchive Desktop)
4. Make a new encrypted archive (in MailArchive Desktop)

Recommended reading (Q2): when the wizard is reached from the launcher choice or the menu, item 2
is dropped (the choice already offers Desktop). Items 3 and 4 stay: they are tasks, which happen to
finish on a Desktop page, and the wizard is where a person looking for "set something up" looks.
Each still opens Desktop at the right page through the instance's own link. With one item left the
list would be pointless, so it never gets shorter than that.

The `wizard` verb typed in a terminal keeps all four items (it may be run where no instance is).

### 6.3 The wizard inside the instance
- Run through a new `wizard.RunIn(ctx, openDesktop func(page string) error) error`: the body of
  today's `Run` (preflight, first list, health check, local flow) without the process-level parts
  (the panic-to-exit-code wrapper becomes a recover that shows the same dialog).
- One wizard at a time: a mutex in the instance shared with Desktop's "Add another archive"; a
  second request while one is open shows nothing new (the menu item is disabled; a hand-off
  `wizard` answers "already open" and the launcher says so in one line).
- The `wizard` verb, when an instance runs for this account session, hands off with `wizard` and
  exits 0; with no instance, it runs as today (a terminal user, or an instance-less platform).

## 7. One running copy, and the hand-off

### 7.1 The protocol change
`POST /_handoff` keeps its shape and gains one field:
`{"n": N, "act": "choose" | "wizard" | "open", "p": HMAC(K, "handoff-req2|" + N + "|" + act)}`.
- A request with no `act` is today's request (label `handoff-req`, meaning `open`), so every older
  launcher still works against a newer instance.
- A new label (`handoff-req2`) for requests with `act`, so an *older* instance refuses a new
  request outright (it cannot verify the proof) instead of misreading it as `open`. The reply's MAC
  covers `act` too (`"handoff-ok2|" + N + "|" + act + "|" + opened + "|" + link`), so a squatter
  cannot turn a `choose` into anything else.
- Version skew: the launcher reads `Version` from the instance file first. An instance older than
  the one that knows actions gets today's request (`open`: the browser opens Desktop) and the
  existing "another version is still running" notice (§4.5.3 of the single-executable design,
  MA-637). Nothing is lost; the person gets Desktop instead of the choice.
- Rate limit, nonce, body limit, constant-time checks, loopback only: unchanged. No new listener,
  no new port, no new file.
- `choose` and `wizard` answer at once (`{"shown": true}`); the dialog then belongs to the
  instance. The launcher exits 0.

### 7.2 macOS
- The .app's no-argument launch becomes `start` (a one-row change to the pure routing:
  `resolveGUIFormerName` under the bundle path, which the `inBundle` input already tells apart).
- The instance runs `[NSApp run]` on the main thread (§8.2) with a delegate whose
  `applicationShouldHandleReopen:hasVisibleWindows:` shows the choice. So a click on the app while
  it runs reaches it through Launch Services, with no hand-off call at all — the path the spike
  proved.
- A launch Launch Services does not route (MailArchive started from Terminal or a LaunchAgent, then
  the app clicked) starts a second process; that process finds `desktop.lock` held and hands off
  with `choose` (§7.1), then exits. Both roads end at the same dialog.
- Activation policy "accessory" (no Dock icon) while only the indicator is up, so the Dock does not
  show a second MailArchive beside the menu bar one. (Owner can ask for a Dock icon; it is one line.)

### 7.3 Windows
- Start menu and desktop shortcuts run `mailarchivew.exe start`; the Startup shortcut runs
  `mailarchivew.exe start -background` (icon, no choice window, no browser). Former shortcuts
  (`desktop`, `wizard`) keep working: `desktop` hands off `open` as today; `wizard` hands off
  `wizard` (D5).
- Windows only lets the program the person just clicked bring a window to the front. The launcher
  calls `AllowSetForegroundWindow(<instance pid>)` before the hand-off, so the choice window the
  instance shows comes up in front and takes the keyboard (R39), not behind Edge.
- The per-session lock already separates fast-user-switching sessions; each has its own instance
  and icon.

### 7.4 Linux
- The desktop entry runs `mailarchive start`. Lock + hand-off as on Windows.
- Wayland compositors refuse focus to a window the person did not just ask for. The launcher passes
  its activation token (`XDG_ACTIVATION_TOKEN`, when the desktop gave it one) in the hand-off
  request; the instance hands it to the zenity process it starts, so the choice can come to the
  front. Where that fails the compositor shows "MailArchive is ready" instead; lab L-L2 records it.
- The no-argument row L1 stays dormant; the desktop entry names `start` explicitly.

### 7.5 What changes in the 2026-11-12 case
macOS: the click reaches the running process through "reopen" and the choice appears. Windows and
Linux: the click hands off through the lock and the choice appears. During the first upgrade from a
version without the event loop, the old process still swallows macOS clicks once (§2.4); the release
notes say "quit MailArchive Desktop once (Shut down) after installing this version".

## 8. Pure Go on each platform

### 8.1 Windows — `golang.org/x/sys/windows` (already a dependency)
- A hidden top-level window (not a message-only window: those do not receive the broadcast
  `TaskbarCreated`, which Explorer sends after it restarts, when the icon must be added again), on a
  goroutine locked to its OS thread, with its own `GetMessage` loop.
- `Shell_NotifyIconW` with `NOTIFYICON_VERSION_4`; identified by `uID`, not `NIF_GUID` (a GUID binds
  the icon to a signed executable's path; the MSI is unsigned today and the program can move).
- Menu: `CreatePopupMenu`/`TrackPopupMenu`, with the documented quirk (`SetForegroundWindow` on the
  hidden window before, a posted `WM_NULL` after) so the menu closes when the person clicks away.
- Keyboard: `NIN_KEYSELECT`/`NIN_SELECT` and `WM_CONTEXTMENU` (Win+B, arrow keys, Enter,
  Shift+F10), so the icon works without a mouse; Narrator reads the tooltip (`szTip`, 128 chars).
- Icons: the app icon is already a resource (MA-1079); the badge variants are drawn at start from
  embedded PNGs with `CreateIconIndirect`, in light and dark and for 100–200% scaling.
- Risks: (1) **Windows 11 hides new icons in the overflow** ("Show hidden icons") until the person
  drags or pins it; a program cannot promote itself. The first choice window says in one line where
  to find the icon and how to pin it. (2) The zenity dialogs and the tray loop each own a locked
  thread; the wizard's dialogs already create their own windows per call — confirmed in slice S3
  on the ARM64 VM. (3) A process without a console started by Task Scheduler in a non-interactive
  session has no notification area: the instance never runs there (§5.4).

### 8.2 macOS — `github.com/ebitengine/purego` (new; Apache-2.0)
- purego calls the Objective-C runtime and AppKit directly from Go with `CGO_ENABLED=0`
  (`objc.GetClass`, `objc.RegisterName`, `ID.Send`, `objc.RegisterClass` for the target/delegate
  class whose methods are Go functions). The spike (§9) built and ran exactly this.
- Threading rule: AppKit is touched only on the main thread. `main` locks the OS thread in `init`
  (harmless for the other verbs). The instance runs `[NSApp run]` there; Desktop's server, the
  probes and the wizard run in goroutines; a state change is posted to the main thread with
  `performSelectorOnMainThread:withObject:waitUntilDone:NO`, and the Go side keeps the state under a
  mutex. One file (`internal/tray/tray_darwin.go`) holds every Objective-C call.
- Images: SF Symbols (`imageWithSystemSymbolName:accessibilityDescription:`, macOS 11+) as template
  images, so light/dark/tinted menu bars and Increase Contrast are handled by the system; the
  accessibility description carries the state words for VoiceOver. On macOS 10.15 (still the
  bundle's minimum) a bundled template PNG instead.
- Risks: (1) purego reaches into Go runtime internals when cgo is off (its internal "fakecgo");
  a Go upgrade can break it. The toolchain is pinned at 1.26.0 on purpose, and the Go-bump checklist
  (go-toolchain note) gains "build and run the menu-bar lab on macOS". (2) A wrong Objective-C
  signature crashes rather than failing politely: every call goes through a small typed wrapper,
  and slice S4 has a smoke test that runs the status item for a second on the macOS CI runner.
  (3) Manual memory rules (retain what we keep: the status item, the menu, the target). (4) On Macs
  with a notch, a crowded menu bar hides items behind it; the app icon (and so the choice) still
  works, and the Help page says so. (5) Binary size: +0.8 MB measured (§9).
- Rejected: a small signed helper app in Swift or Objective-C. It would be a second executable
  carrying a surface of its own (against R33: "no surface exists only in a file that is not the
  program"), need a non-Go toolchain and signing in the release pipeline, and add an IPC channel.

### 8.3 Linux — `github.com/godbus/dbus/v5` (new; BSD-2-Clause, pure Go)
- The tray protocol is StatusNotifierItem over the session D-Bus (KDE Plasma, Xfce, Cinnamon,
  MATE, Budgie, and GNOME *with* the AppIndicator extension, which Ubuntu enables by default and
  Fedora does not) with its menu over `com.canonical.dbusmenu`. About 500 lines of our own, using
  godbus for the bus.
- Fallback when no `org.kde.StatusNotifierWatcher` owns its name on the bus (plain GNOME, a
  headless session): no icon, one log line, and Desktop's header says "MailArchive is running in the
  background — your desktop does not show tray icons (see Help)". The launcher choice and hand-off
  work the same without the icon. (Q8 covers the alternative of a permanent notification.)
- Considered and not chosen: `fyne.io/systray` (pure Go on Windows and Linux, but cgo on macOS,
  and it owns the main loop through global state, which fights the instance's own lifecycle). It is
  a useful reference for the D-Bus details.
- Risks: panel quirks (icon size, menu refresh) differ by desktop; lab L-L1/L-L2 cover KDE, GNOME
  with the extension, GNOME without, and Xfce.

## 9. The macOS feasibility spike (2026-11-13, the owner's Mac, macOS 26.6.2 build 25G83, Apple silicon)

Record: `docs/lab/menubar-spike-macos-2026-11-13.md`. In short:
- **It worked.** A Go program built with `CGO_ENABLED=0`, Go 1.26.0, purego v0.8.4, showed an
  `NSStatusItem` in the menu bar with an SF Symbol image ("archivebox"), a title and an
  accessibility description, and a two-item menu ("Open configuration window", "Run the wizard").
- A menu item's action reached a Go function (`ACTION openConfig: fired (Go callback reached)`).
- The menu opened programmatically (`performClick:`) and closed again.
- Struct-returning calls work (`[[NSScreen mainScreen] frame]` → 1728 × 1117 points).
- Built into a .app and opened a second time, the running process received
  `applicationShouldHandleReopen:hasVisibleWindows:` and **no second process started**.
- A plain Go .app without the event loop, opened twice: **one process only**, the second open was
  swallowed (§2.4).
- Size: 3.3 MB for the spike vs 2.5 MB for the same program without purego: **+0.8 MB**. Links only
  `libSystem` and `libresolv`; AppKit is loaded at run time with `dlopen`.
- **The screenshot with `screencapture -R` failed** ("could not create image from rect", also
  outside the sandbox): the terminal process has no Screen Recording permission on this Mac. In its
  place the spike drew its own status button into a PNG (a process may always draw its own views):
  `docs/lab/menubar-spike/statusitem-render.png`. A real menu-bar screenshot needs the owner to
  grant Screen Recording to the terminal, or a person to take it; lab L-M2.
- Fragile: the menu stayed open only 1–4 s when opened by code (it closes when the app is not
  active — expected for a background app, and not how a person opens it); exiting from a goroutine
  with `os.Exit` was used for the spike only (the product stops through `Lifecycle.Stop` and
  `[NSApp terminate:]` on the main thread).

## 10. Security, privacy, accessibility, sign-in, quitting

### 10.1 Security
- **No new listening socket.** The hand-off stays on the dashboard's existing loopback listener;
  the indicator talks to the rest of the instance in-process. The only change on the wire is the
  `act` field, covered by the MAC in both directions (§7.1).
- **New local entry points, all same-account:** macOS "reopen" Apple Events, Windows messages to
  the hidden window, and on Linux our objects on the session D-Bus. Any process of the same account
  can already do more than these allow (R23 states the same-account limit). Still: each entry point
  can only *show* the choice, the menu, or open Desktop through a fresh link in the instance's own
  session — never act on an archive; Quit while busy still asks (§10.4); the Windows window
  procedure acts only on its own callback message and `TaskbarCreated`. Linux: the D-Bus objects
  expose fixed words and state only (§10.2), so a sandboxed app with session-bus access learns
  "backing up 1 archive" at most.
- **The private-link model is unchanged.** The indicator never shows, copies or logs a link; every
  "Open MailArchive Desktop" mints a fresh one-time link and opens it, as the hand-off does today.
- The instance file, its key and its owner checks are unchanged; the launcher verifies the instance
  exactly as `desktop` does today, and opens nothing at an address that did not prove itself.
- The adversarial pass for S1 should try: a squatter on the recorded port answering `choose`;
  replay of a captured request; a forged `act`; a flood of `choose` (rate limit, one dialog at a
  time); an older instance meeting a newer launcher and the reverse; two launches in the same
  second; a launch during the instance's shutdown drain.

### 10.2 Privacy
- The menu shows archive names (the person's own labels), task kinds, times and counts of
  archives. It never shows subjects, senders, folder names, message counts per folder, log lines
  or paths.
- The hover tooltip (visible to anyone looking at the screen or a screen share) shows **no archive
  names**: "backing up 1 archive" (Q5). Names appear only when the person opens the menu.
- Linux: the same rule, since the tooltip and menu are readable on the session bus.
- An encrypted archive's lock carries no verb by design; the indicator says "working on an
  archive" and never guesses.

### 10.3 Accessibility
- Never colour alone: badge shape + words.
- macOS: native `NSMenu` (VoiceOver reads it), template images, accessibility description with the
  state words.
- Windows: keyboard reach (Win+B, Enter, Shift+F10), Narrator reads the tooltip, light/dark and
  high-contrast icon variants.
- Linux: the panel's own accessibility; `Title` and `ToolTip` carry the words.
- No animation, no sound.

### 10.4 Quitting while a task runs
- "Quit MailArchive" when nothing of *this process* runs: stops at once (`Lifecycle.Stop`, the
  same path as the Shut down button), the icon goes away.
- When this process runs a task (a Run now, a check, a rebuild, a restore, the wizard's export):
  a question — "MailArchive is backing up “Work mail”. If you quit now, the backup stops at its next
  safe point and the next backup finishes what it left." Buttons: **Keep running** (default) /
  **Quit anyway**. For a restore, the restore's own cancel words. (Q4.)
- A run of *another* process (a scheduled backup) is not stopped by Quit, and the question does not
  appear for it; the menu's last line under Quit says "Scheduled backups still run when MailArchive
  is closed."
- The Shut down button in Desktop and Quit in the menu are the same action.

### 10.5 Start at sign-in
- Windows: the MSI's all-users Startup shortcut stays (it is how the background Desktop starts
  today) and becomes `start -background`, so what was invisible is now the icon. A per-user "Open at
  sign-in" tick (in the menu and in Desktop's Settings) is stored in the user's settings; when it is
  off, the Startup launch exits at once and quietly.
- macOS: opt-in, default off (Q3). On macOS 13+ registered with `SMAppService.mainApp` (through
  purego; it appears in System Settings → Login Items with the app's name, where the person can also
  turn it off); on 10.15–12, a LaunchAgent plist that runs `open -a` on the app, so Launch Services
  owns the process and "reopen" routing works.
- Linux: opt-in, default off: an XDG autostart entry (`~/.config/autostart/mailarchive.desktop`,
  `Exec=mailarchive start -background`).
- Uninstall removes the per-user entries it made (the uninstall plan lists them, as it lists
  schedules today); archives are never touched (R36).

## 11. Proposed invariants and catalog ids (land in slice S0, under the covers-map rules)

Free ranges checked 2026-11-13 with `git grep` on `origin/main`, `gmail-dedup` and
`origin/gmail-dedup`: the highest MA id in use anywhere is MA-1291; the highest R is R41 (R44 is
mentioned on the gmail-dedup branch). This design proposes **MA-1400 to MA-1439**, **R50–R52** and
**X30 onward**, leaving room for the Gmail work to grow without a clash. No row is added to
`docs/scenario-catalog.md` by this commit (a U/S row with no covering test would turn the
covers-map gate red); they land with S0, each with its test or marked pending with its lab story.

Proposed invariants (wording for the review to sharpen):
- **R50 — One MailArchive per session.** For one account in one sign-in session, at most one
  MailArchive GUI process runs; every GUI launch either becomes it or reaches it through a proven
  hand-off and exits; a launch never ends silently: it shows the choice, Desktop, or a refusal in
  words.
- **R51 — The indicator tells, and tells only what it may.** While MailArchive runs, its state
  (idle, the running task's kind, needs attention) is visible outside the browser; the indicator
  shows only fixed words, archive names (in the menu only), task kinds and times — never mail
  content, folder names, log lines, paths or links.
- **R52 — Quitting never silently stops work.** Quitting while this process runs a task asks first;
  quitting never stops a scheduled backup or removes a schedule.

Proposed rows (U = unit, S = system/integration, L = lab; ids are placeholders the review may
renumber within the range):

| Id | Tier | What it proves | Slice |
|---|---|---|---|
| MA-1400 | U | `start` routing table: every OS/launch input → become-instance or hand-off; the .app no-arg row → `start` | S1 |
| MA-1401 | U | hand-off `act`: MAC covers act both ways; a forged or missing act is refused/treated as `open`; old label still accepted | S1 |
| MA-1402 | U | version skew: an instance older than actions gets `open` + the other-version notice; a newer instance with an old launcher still opens | S1 |
| MA-1403 | S | second `start` while an instance runs: exactly one process remains, the choice is shown once (fake dialog seam), exit 0 | S1 |
| MA-1404 | S | `wizard` verb with an instance running hands off and runs no dialogs itself | S1 |
| MA-1405 | U | one wizard at a time (chooser, menu, Add another archive, hand-off all share it) | S1 |
| MA-1406 | U | the wizard's first list from the launcher omits only the Desktop item (Q2) | S1 |
| MA-1407 | U | the wizard inside the instance opens Desktop by the instance's own link, never `desktop.Launch` | S1 |
| MA-1408 | S | macOS reopen → choice (fake AppKit seam) | S1 |
| MA-1409 | U | Windows launcher calls AllowSetForegroundWindow with the instance pid before the hand-off (recording fake) | S1 |
| MA-1410 | U | job slot `kind` set by every caller of the slot (walk of `runJobOnClaiming` callers) | S2 |
| MA-1411 | U | lock-verb → state table (export, graph, verify, rebuild, restore-thunderbird, delete, empty, unknown, other host) | S2 |
| MA-1412 | U | probe budget: a probe that overruns marks "can't tell" and backs off; an absent folder is not opened | S2 |
| MA-1413 | U | attention facts → state, precedence with running tasks | S2 |
| MA-1414 | U | R51 sweep: menu/tooltip strings are built only from fixed words, names and times (a planted log line or path never appears) | S2 |
| MA-1415 | U | tooltip carries no archive name (Q5) | S2 |
| MA-1416 | U | refusals and busy buttons name the real task (the 2026-11-12 polish item) | S2 |
| MA-1420 | U | Windows: TaskbarCreated re-adds the icon; menu commands map 1:1 (message-level fake) | S3 |
| MA-1421 | U | Windows: keyboard select opens the menu | S3 |
| MA-1422 | U | quit while busy asks; Keep running is default; quit when idle does not ask (R52) | S3 |
| MA-1423 | U | quit never touches schedules (recording scheduler fake) | S3 |
| MA-1425 | S | macOS: status item created and removed (smoke, macOS runner) | S4 |
| MA-1426 | U | macOS: every AppKit call happens on the main thread (wrapper asserts) | S4 |
| MA-1428 | U | Linux: SNI properties and dbusmenu layout from `tray.State` (in-process bus fake) | S5 |
| MA-1429 | U | Linux: no watcher → no icon, header note, launcher still works | S5 |
| MA-1432 | U | sign-in entries: written only on opt-in, removed on opt-out and by uninstall | S6 |
| MA-1433 | U | Windows Startup launch exits quietly when the per-user tick is off | S6 |
| MA-1436 | L pending | L-M1: the 2026-11-12 case on the real .app (one copy; two copies) | S1 |
| MA-1437 | L pending | L-W1: Windows 11 ARM VM — overflow, Explorer restart, Narrator, high contrast, Edge in front | S3 |
| MA-1438 | L pending | L-M2: macOS 13/14/15+ menu bar, notch, VoiceOver, a real screenshot | S4 |
| MA-1439 | L pending | L-L1/L-L2: KDE, GNOME with and without the extension, Xfce; Wayland focus | S5 |

## 12. Build slices and gates

### 12.1 Slices
- **S0 — catalog and review.** The 10-lens pre-code review of this design; fold; owner answers;
  catalog rows and R50–R52 land with their tests or as pending. *Gate: 10-lens review.*
- **S1 — one running copy, the choice, and the macOS 2026-11-12 fix.** `start` verb; hand-off
  `act`; `wizard` hands off; the wizard runs in the instance; the zenity choice; macOS AppKit event
  loop with the reopen delegate (no status item yet); Windows `AllowSetForegroundWindow`; shortcut
  and .app changes. Shippable alone as a small release (Q6). *Gates: adversarial pass (hand-off and
  IPC); friction review, Windows first; L-M1.*
- **S2 — the state model.** Job-slot kind; bounded lock probes; attention facts; `tray.State` as a
  pure value; the busy-button and refusal words fix from the 2026-11-12 polish item rides here.
  *Gate: friction review of the changed page words.*
- **S3 — Windows indicator.** *Gates: friction review on the Windows VM first (Edge); L-W1.*
- **S4 — macOS indicator** (status item, menu, sign-in through SMAppService/LaunchAgent).
  *Gates: friction review; L-M2.*
- **S5 — Linux indicator** (SNI + dbusmenu, fallback). *Gates: adversarial pass on the D-Bus
  surface; friction review; L-L1/L-L2.*
- **S6 — start at sign-in** across the three, uninstall entries. *Gates: adversarial pass on the
  files it writes in the person's profile (paths, links, permissions); friction review.*
- **S7 — words and help**: Help page section, README, release notes, UX contract rows.

Every slice ships prove-fail → prove-pass (mutate the fix, watch its covering test go red,
revert), recorded in the commit; the unfiltered `go test ./...` runs before each push.

### 12.2 New dependencies (operator decision)
`github.com/ebitengine/purego` (macOS only, behind a build tag) and `github.com/godbus/dbus/v5`
(Linux only, behind a build tag). Both pure Go; both go into THIRD-PARTY-NOTICES. (Q7.)

### 12.3 Labs (this repo has no lab tier; run locally per the tri-platform note)
- L-M1 (macOS): open the real 1.7.1 .app, choose Desktop; open the app again → record what
  happens (expected: nothing, one process). Repeat with a second copy of the .app at another path.
  Then the same with the S1 build → the choice appears.
- L-M2 (macOS): status item on light/dark/tinted menu bars, with a notch and a crowded bar,
  VoiceOver, Increase Contrast; a real screenshot (needs Screen Recording for the terminal, or by
  hand).
- L-W1 (Windows 11 ARM VM, `prlctl exec` cannot drive a signed-in desktop, so by hand): first-run
  overflow, pinning, Explorer restart, Narrator, high contrast, the choice in front of Edge, Startup
  launch with the tick off.
- L-L1/L-L2 (Linux, a desktop VM; Docker has no GUI): KDE, GNOME with and without AppIndicator,
  Xfce; X11 and Wayland focus for the choice.

## 13. Questions for the owner (each with the recommendation that gets built)

- **Q1 — Words.** The menu and the choice say **"Open MailArchive Desktop"** (the name the page,
  the help and the shortcuts already use), with "your archives, schedules and settings" beside it.
  *Alternative:* "Open the configuration window", your phrase. *Recommend:* "Open MailArchive
  Desktop", so one thing has one name everywhere.
- **Q2 — "Wizard minus the configuration window".** *Recommend:* drop only the wizard's "Microsoft
  365, folder export, browsing & tools (open MailArchive Desktop)" item when it is reached from the
  choice or the menu; keep "Open an archive folder I already have" and "Make a new encrypted
  archive". *Alternative:* the wizard goes straight to "Archive a mail file on this computer" with
  no first list.
- **Q3 — Start at sign-in on Mac and Linux.** *Recommend:* off until the person ticks "Open at
  sign-in" (menu and Settings). *Alternative:* on by default, as Windows already is through the
  installer.
- **Q4 — Quit while a task runs.** *Recommend:* ask, with "Keep running" as the default.
  *Alternative:* quit at once; the task stops at its next safe point.
- **Q5 — Archive names in the hover tooltip.** *Recommend:* no — counts only ("backing up 1
  archive"); names only inside the opened menu, because the tooltip shows to anyone watching the
  screen. *Alternative:* show the name in the tooltip.
- **Q6 — Release order.** *Recommend:* ship S1 (one running copy, the choice, the macOS
  "quits at once" fix) as its own small release before the icon slices. *Alternative:* ship it all
  together.
- **Q7 — Two new libraries** (purego for the Mac icon, godbus for the Linux icon; both pure Go,
  permissive licences). *Recommend:* accept. *Alternative:* no Mac/Linux icon (Windows only).
- **Q8 — Linux desktops that show no tray icons (plain GNOME).** *Recommend:* no icon; Desktop's
  header and the Help page explain, and point to the AppIndicator extension. *Alternative:* a
  permanent notification while MailArchive runs.
- **Q9 — Icon when idle.** *Recommend:* always shown while MailArchive runs (it is how you know it
  runs). *Alternative:* only while a task runs or something needs attention.

## 14. Not in this design
- Notifications (the existing failure/recovery notices and email alerts are unchanged).
- A native settings window: the configuration window stays MailArchive Desktop in the browser.
- Progress percentages in the menu (task kind and start time only; Desktop shows progress).
- Any change to archives, schedules, the engine, the reader, or the private-link session model.

## 15. Sources
Read for this design (branch `menubar` at 1a6d437): `cmd/mailarchive/noarg*.go`, `dispatch.go`;
`internal/instance/instance.go`, `handoff.go`; `internal/desktop/launch.go`, `lifecycle.go`,
`jobcontrol.go`, `maintenance.go`, `capture.go` (job slot, `/api/activity`);
`internal/lockfile/lockfile.go`; `internal/app/run.go` (`lockVerb`), the `AcquireAs` call sites;
`internal/wizard/wizard.go` (first step, `Run`, `AddArchive`); `packaging/macos/Info.plist.in`,
`build-app.sh`; `packaging/linux/mailarchive.desktop`; `packaging/mailarchive-desktop/Product.wxs`
(shortcuts, Startup); `docs/scenario-catalog.md` (R23, R33, R36, R39). Not checked: the zenity
library's source (list/question behaviour per platform is as the wizard uses it today; slice S1
confirms the choice window on each platform); Apple's, Microsoft's and freedesktop.org's API
documentation was not opened for this draft — the API names above are from memory except those the
spike exercised (NSStatusBar, NSStatusItem, NSMenu, NSMenuItem target/action, NSImage SF Symbols,
NSApplication delegate reopen, activation policy).
