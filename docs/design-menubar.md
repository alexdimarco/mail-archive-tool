# Design: the MailArchive indicator, the launcher choice, and one running copy

Status (2026-11-13, revision 1): DESIGN ONLY, NOT REVIEWED, NOT BUILT. Nothing here exists in the
code yet except the macOS feasibility spike recorded in
`docs/lab/menubar-spike-macos-2026-11-13.md` (its source is in `docs/lab/menubar-spike/`, a
separate Go module so the product build never compiles it). *[Superseded 2026-11-13 in its
"NOT REVIEWED" words by the note below.]*

> **Folded 2026-11-13 [menubar 10-lens MB-24] (lead's call).** Status, revision 2: REVIEWED, NOT
> BUILT. The 10-lens pre-code review is `docs/review-menubar-10lens.md` (verdict GO WITH
> CONDITIONS, conditions MB-1 to MB-24, commit cf20831). Every condition, MB-1 to MB-24, is folded
> below beside the text it changes, each marked "Folded 2026-11-13 [menubar 10-lens MB-n]". The
> lead accepted every condition as the review proposes; where the review offered two ways, the
> note names which one was taken. Superseded sentences stay in place, marked. The review was run
> by one reviewer in all three roles (its §1 deviation); an independent skeptic over MB-1 to MB-14
> is still the cheapest way to strengthen it, and is not run here.

- Owed before any code: the **10-lens pre-code design review** (this changes how every launch
  starts, adds a long-lived OS integration on three platforms, and extends the hand-off protocol:
  it is architecture- and security-relevant). Not run by the author. *[Done 2026-11-13: see the
  MB-24 note above.]*
- Owed during the build: an **adversarial pass** on the hand-off and IPC changes (slice S1) and on
  the Linux D-Bus surface (slice S5); a **friction review** of every shippable slice, **Windows
  first** (Edge on the Windows VM); the UX contract rows for the new words.
- Owed from the owner: the answers to §13 (Q1–Q9). Each recommended answer is what gets built if
  the owner agrees; each alternative is named beside its question. *(2026-11-13: the
  recommendations are now the defaults, pending the owner's "ok to your picks"; see the note in
  §13.)* *[Answered 2026-11-13: the owner's "ok to your picks" — Q1–Q9 are rulings as
  recommended; the dated note is in §13.]*
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

> **Folded 2026-11-13 [menubar 10-lens MB-9] (lead's call).** "Per account and logon session" holds
> on Windows only. On macOS and Linux `LogonSession()` returns an empty string
> (`internal/instance/owner_unix.go:27`), so the lock and the instance are **one per account**,
> whatever the number of sign-ins. The design takes that as it is (no new per-session key on
> POSIX); D1, R50 and §6.3 are amended to match.

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
*[Superseded 2026-11-13 in its "likely, not proven" words by MB-4 below: confirmed on the owner's
Mac.]*

> **Folded 2026-11-13 [menubar 10-lens MB-4] (lead's call).** **Evidence now in hand** (the lead,
> 2026-11-13, on the owner's own Mac, with a 1.7.0 MailArchive Desktop process running): `open -a
> "Mail Archive"` while that Desktop runs fails with `_LSOpenURLsWithCompletionHandler() failed …
> error -600`, and no second process starts. macOS routes the open to the running app's process,
> which has no Apple-event loop to answer it. This is recorded as **the confirmed cause of the
> 2026-11-12 report**; the first part of L-M1 (the one-copy case) is done.
> - The error differs from the spike's `noloop` case, which was silently swallowed; both end the
>   same way for the person: no second process and nothing visible.
> - The check ran against 1.7.0, not 1.7.1, and through `open -a`, not a Finder click. Both
>   versions lack the event loop and both routes go through Launch Services, so neither difference
>   is expected to matter. (These two points are the folding agent's, not the lead's.)
> - Still owed from L-M1, now as S1 lab work rather than a gate before S1's code: the two-copies
>   case, and proof that "reopen" reaches the **product's** bundle shape (the symlinked
>   `mailarchive-gui` executable, with and without `LSUIElement`), run against the first S1 build
>   before S1 ships. S1 may proceed as its own small release (Q6) once this folded design is
>   re-checked.

The fix is part of this design: the MailArchive process on macOS runs an AppKit event loop and
answers "reopen" by showing the launcher choice (§6, §7.2). The spike proved a pure-Go process
receives that message (`REOPEN: a second launch was handed to this process`).

Caveat for the first upgrade: while an *older* MailArchive (one without the event loop) still runs,
macOS keeps routing clicks to it, so the new version's fix cannot help until the old one is quit
once (Desktop's Shut down button, or signing out). The release notes and the existing
"another version is still running" notice say so (§7.4).

> **Folded 2026-11-13 [menubar 10-lens MB-14] (lead's call).** This is not only the first upgrade.
> After S1 ships, **every** in-place upgrade (a new `.app` dragged over the old one, or an MSI whose
> end-session close was refused) leaves the running instance answering clicks with its own
> version's choice and wizard until it is quit. The fix is in §6.1 and §7.1: the hand-off carries
> the launcher's version, the macOS instance compares its own version with the bundle's on disk on
> reopen, and the choice window then says "A newer MailArchive is installed. Quit and open
> MailArchive again to use it." with a Quit button. The instance never restarts itself (R33;
> MA-612 forbids passing the program's path to `open`).

## 3. Decisions in one place

| # | Decision | Section |
|---|---|---|
| D1 | **One MailArchive process per account and sign-in session** ("the instance"): it holds `desktop.lock`, serves Desktop exactly as today, shows the indicator, shows the launcher choice, and runs the wizard. Every GUI launch either becomes it or hands off to it and exits. *[Amended 2026-11-13, MB-9: per account; per logon session on Windows only.]* | §4 |
| D2 | The indicator **lives in the instance only**. A scheduled backup started by the OS scheduler never shows an icon of its own; when the instance runs, it *sees* such runs through the archive locks. With no instance running, nothing shows (as today). | §5.4 |
| D3 | A new launcher verb **`mailarchive start`** (and `start -background` for sign-in) is what every app icon and shortcut runs; the macOS .app's no-argument launch becomes `start`. | §6, §7 |
| D4 | The hand-off gains an **action** — `open` (today's: open Desktop), `choose` (show the launcher choice), `wizard` — covered by the MAC; an older instance that does not know actions is handled by version. *[Superseded 2026-11-13, MB-3: by capability.]* | §7.1 |
| D5 | The `wizard` verb **hands off to a running instance** instead of running beside it. One wizard at a time per account session. *[Amended 2026-11-13, MB-9: across processes, by a wizard lock.]* | §6.3 |
| D6 | State comes from (a) the instance's own job slot, given a **task kind**, (b) a **bounded probe of each known archive's lock** and its verb, (c) the same health facts the "Your archives" card shows. Never from activity-log lines. *[(c) superseded 2026-11-13, MB-1: a read-only reader of named records.]* | §5.3 |
| D7 | The menu and tooltip show **only fixed words, archive names, task kinds and times** — never mail content, folder names, log lines or the private link. Archive names appear inside the menu only, not in the hover tooltip (Q5). *[Amended 2026-11-13, MB-12, MB-15: never on Linux; not in the choice window.]* | §10.2 |
| D8 | Pure Go, no cgo: Windows through `golang.org/x/sys/windows` (already a dependency); macOS through `github.com/ebitengine/purego` (new, proven by the spike); Linux through `github.com/godbus/dbus/v5` (new) with a no-icon fallback. No helper executable. | §8 |
| D9 | **Quit asks first while a task of this process runs** ("Keep running" is the default). Quitting never stops schedules. *[Amended 2026-11-13, MB-2: every stop waits up to a bound and records "interrupted"; only the person's own Quit asks.]* | §10.4 |
| D10 | Start at sign-in: Windows keeps the MSI's Startup shortcut (now visible as the icon) with a per-user "don't open at sign-in"; macOS and Linux **opt in**, default off (Q3). *[Amended 2026-11-13, MB-11, MB-21: LaunchAgent on macOS; quiet exit with no archives on Windows.]* | §10.5 |

> **Folded 2026-11-13 [menubar 10-lens MB-1, MB-2, MB-3, MB-9, MB-11, MB-12, MB-15, MB-21] (lead's
> call).** The table above stays as reviewed; these amend it:
> - **D1** (MB-9): "per account and sign-in session" is superseded by "per account (per logon
>   session on Windows)". A second graphical sign-in of the same account on macOS or Linux reaches
>   the first one's instance.
> - **D4** (MB-3): "handled by version" is superseded. The launcher chooses the request form by the
>   instance's advertised capability (an `acts` list in the instance file), never by comparing
>   versions (§7.1).
> - **D5** (MB-9): "one wizard at a time" holds across processes, not only inside the instance
>   (§6.3).
> - **D6 (c)** (MB-1): superseded. The attention facts come from a new read-only reader of named
>   records, never from the "Your archives" row builder or `health.Gather` (§5.3).
> - **D7** (MB-12, MB-15): on Linux the menu shows no archive names at all, only generic words; the
>   choice window shows counts, not names.
> - **D9** (MB-2): every stop path (Quit, Shut down, sign-out, installer, OS termination) waits for
>   running work up to a bound, then records the run as interrupted. Only a person's own Quit asks
>   first (§10.4).
> - **D10** (MB-11, MB-21): macOS uses one mechanism, a per-user LaunchAgent that runs `open -a`;
>   `SMAppService` waits for a lab. On Windows, the background start also exits quietly for a user
>   with no archives (default pending the owner, §10.5).

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

> **Folded 2026-11-13 [menubar 10-lens MB-9, MB-16, MB-20] (lead's call).**
> - **MB-9.** An instance that cannot show dialogs (the `-no-tray` fallback with no display, or a
>   Linux session with no dialog tool) answers a `choose` or `wizard` hand-off with "can't show here"
>   (§7.1). The launcher then does what `desktop` does today: opens the browser at Desktop, or shows
>   the link dialog. A launch never ends silently (R50, X11).
> - **MB-16.** Where no icon can show (plain GNOME without the AppIndicator extension, the
>   `-no-tray` fallback), a Cancel on the choice of a launch that started the instance, with nothing
>   running, stops the instance. Otherwise it would be the silent background server X11 rules out.
> - **MB-20.** The instance file records the icon's state and its reason: "shown", "no tray on this
>   desktop", or "turned off". `status` and Desktop's header say whether MailArchive is running, its
>   version, and why no icon shows. A Windows no-console instance has no console for its log lines;
>   S1 names where they go (not checked by this fold), and the Help page (S7) says so.

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
| Working | a lock held whose verb is empty or unknown (an encrypted archive's run writes no verb, by design) or a delete *[the delete part superseded 2026-11-13, MB-5: a delete in this process has its own row]* | arrows | "MailArchive — working on an archive" |
| Needs attention | any archive has a fact the "Your archives" card already shows as a problem: source not reachable (streak), backups failing (alert streak), auto-paused, a delete that did not finish, an index behind *[superseded 2026-11-13, MB-1]* | "!" | "MailArchive — 1 archive needs attention" |

Precedence when several hold: a running task shows its badge, and "needs attention" adds itself to
the tooltip and the menu's first line ("Backing up 1 archive · 1 needs attention"). No animation
(a still icon costs no CPU and does not distract; the badge change is the signal).

> **Folded 2026-11-13 [menubar 10-lens MB-1, MB-5, MB-6] (lead's call).**
> - **MB-5.** The table gains one row per task kind the instance can run, each read from the place
>   that holds it, not only the capture job slot: **backup** (Run now; job slot), **check** (job
>   slot), **rebuild** (job slot), **restore** to Thunderbird (`d.rtb` and the job slot), **copy**
>   (the encryption copy; job slot; tooltip "MailArchive — copying an archive", badge arrows),
>   **delete** (`d.running`; tooltip "MailArchive — deleting an archive"), **workspace export**
>   (the Extraction workspace export, its own slot `d.ex.slot`; tooltip "MailArchive — exporting
>   mail"), and **wizard export** (the in-instance wizard's export; tooltip as backup). The state
>   model and the Quit question (§10.4) read every one of these holders.
> - **MB-1.** In the "Needs attention" row, "an index behind" is superseded: it leaves the
>   indicator, because it cannot be told without opening the search index. The "Your archives" card
>   keeps showing it.
> - **MB-6.** The "Working" row's "a lock held whose verb is empty" shows only when the hold is
>   seen on two probes in a row at least one interval apart, or when the holder line's start time is
>   more than a few seconds old, so `status`'s own short probe and other brief holds with no verb do
>   not flash the icon.

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

> **Folded 2026-11-13 [menubar 10-lens MB-12, MB-17] (lead's call).**
> - **MB-12, Linux.** The Linux tray menu **never shows archive names**, at any time: its layout
>   (`com.canonical.dbusmenu` `GetLayout`) can be read by any session-bus client whether or not the
>   menu is open. On Linux the task lines read "Backing up 1 archive — since 14:02", the attention
>   line reads "1 archive needs attention", and "Status ▸" has one generic line per archive state
>   count ("2 up to date", "1 needs attention"), each opening Desktop's "Your archives". The review's
>   other way (fill names only on `AboutToShow`, clear on close) is not taken. macOS and Windows keep
>   the menu as drawn above.
> - **MB-17.** "Open MailArchive Desktop" from a tray click (Windows double click, Linux
>   `Activate`) and the macOS reopen are limited to one a second, as the hand-off is.

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
- an archive whose folder is not present (drive unplugged) is skipped without opening anything
  *[amended 2026-11-13, MB-6: after a time-boxed `stat`]*;
- archives marked as in a cloud-synced folder (MA-1083's facts) are probed every 60 s, not 5 s;
- the probe's instant hold is the cost the lock package already accepts (`acquireGrace` retries
  cover a run that starts in that instant); at most one probe per archive is in flight.

> **Folded 2026-11-13 [menubar 10-lens MB-6] (lead's call).** The probe must not show a false
> "busy":
> 1. The instance's own tasks win. Probes skip any archive the instance itself holds, is deleting or
>    is moving (flock is per open file, so the instance probing its own backup reads "held" and
>    would count it twice).
> 2. A lock held with an empty verb is debounced as in the §5.1 MB-6 note.
> 3. The probe opens the lock file **read-only** (enough for flock and `LockFileEx`; a write-mode
>    open can have side effects on cloud placeholders).
> 4. The "folder not present" check (`stat`) runs **inside** the 2-second budget, since a `stat` on a
>    sleeping network volume can block. "Skipped without opening anything" above is amended to
>    "skipped after a time-boxed `stat`".
> 5. "From another computer" is labelled best effort: the holder line is untrusted and network-file
>    locking varies.
> 6. New U rows (§11, MA-1419) cover the de-duplication, the debounce, and a delete in the instance
>    that meets a concurrent probe on Windows (real files, fault injection for timing).

**(c) Attention facts.** The same reads the Desktop's "Your archives" rows and `status` use
(`ArchiveRow`: source-loss streak, alert streak, auto-pause, unfinished delete, marker; the
run-history's last outcome), recomputed once a minute and right after any task ends. Slice S2
measures the cost on a 50-archive index; the budget is under 50 ms per pass, else it moves to a
5-minute cadence. *[Superseded 2026-11-13 in its source ("the same reads … rows and `status`
use") by MB-1 below; the cadence and cost budget stay, as stated there.]*

**Never read:** the activity log lines (they carry mail folder names and sync messages), mail
files, manifests, or the search index.

> **Folded 2026-11-13 [menubar 10-lens MB-1] (lead's call).** The reads named above write into
> every archive: `health.Gather` takes the archive lock with `Acquire` (which creates the lock file
> and rewrites its holder line) and writes a 64 KiB probe file; the "Your archives" row builder
> (`archiveRows`, `reconcileArchives`) can rewrite the archives list and asks the OS scheduler about
> each row. The attention facts therefore come from **a new read-only reader** (for example
> `tray.Attention(path)`), with these rules:
> 1. **Named records only.** It reads only small named records in the archive's state: the last
>    run (`state.ReadLastRun`), the last verify (`state.ReadLastVerify`), the source-loss streak,
>    the alert streak, the auto-pause/seal state, and the unfinished-delete marker and notice files.
>    Each through the bounded safe reader (`util.ReadSecureFile` or the existing `state.Read*`):
>    size-capped, regular file only, never through a link.
> 2. **Never** takes a lock, **never** writes, **never** probes or asks the scheduler, never opens
>    the search index, and never calls `health.Gather`, `archiveRows`, `reconcileArchives` or
>    anything that does.
> 3. **"Search index behind" is dropped from the icon** (it cannot be told without opening the
>    index). The "Your archives" card keeps it.
> 4. **Poll interval:** once every 60 seconds, and right after any task of the instance ends.
>    Archives in a cloud-synced folder (MA-1083's facts) are read at most every 5 minutes (the
>    folder's pick, so a file provider is not woken each minute). The S2 cost budget above (under
>    50 ms per pass on 50 archives, else every 5 minutes) still applies.
> 5. **Nothing is read on an archive whose drive is not mounted.** The reader first `stat`s the
>    archive folder inside a 2-second budget; a folder that is absent, or a `stat` that overruns,
>    means nothing in that archive is opened on this pass, and an overrun backs off 5 minutes, as
>    the lock probe does.
> 6. **One status truth (X10).** The "Your archives" row and the indicator call one shared
>    classifier over these facts; a row (MA-1418) tests that the two agree on a fixture set.
> 7. **Proven "no write"** (MA-1417): before and after a full pass over tempdir archives (one with no
>    lock file, one read-only, one encrypted), the tree is unchanged in names, sizes, modification
>    times and bytes, and no file is created.
>
> This lands with S2 (the slice it changes), before S2's code.

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
  14:02)." *[Superseded 2026-11-13 by MB-15 below: counts, not names.]*
- Default item: the wizard when no archive is set up yet; Desktop otherwise.
- Cancel leaves the instance (and its icon) running. A first-ever start, cancelled, also leaves it
  running, and the icon is there to say so. *[Amended 2026-11-13 by MB-16 below where no icon can
  show.]*
- On Linux without a dialog tool (no zenity), the chooser cannot show: `start` opens Desktop
  directly and the page says why (the wizard's existing preflight words).

> **Folded 2026-11-13 [menubar 10-lens MB-8, MB-14, MB-15, MB-16] (lead's call).**
> - **MB-15.** The busy line uses the count form, as the tooltip does: "Now: backing up 1 archive
>   (since 14:02)." The choice window is as visible as the tooltip (Q5).
> - **MB-16.** Where no icon can show (plain GNOME without the extension, the `-no-tray` fallback),
>   Cancel on a launch that started the instance, with nothing running, stops the instance (§4
>   note).
> - **MB-8 (dialogs in front, R39).** The choice is a dialog with a new way in, so it follows
>   `dialogfront`'s Arm rule: it is shown only after `dialogfront.Arm()` at the person's request. A
>   hand-off counts as the person's request **only after its proof is verified**. The same holds for
>   the menu's "Run the wizard…", the Quit question (§10.4) and the choice shown on macOS reopen.
>   MA-1111 and MA-1117's walk is widened to these entry points (amended, not a parallel test).
> - **MB-14.** When the instance is older than the program on disk (the launcher's version in the
>   hand-off, or on macOS the bundle's version read on reopen), the window says one line above the
>   list, "A newer MailArchive is installed. Quit and open MailArchive again to use it.", and offers
>   a Quit button. No self-restart.

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

> **Folded 2026-11-13 [menubar 10-lens MB-9, MB-2] (lead's call).**
> - **MB-9, one wizard across processes.** The in-process mutex cannot see a standalone wizard (for
>   example the "MailArchive wizard" Start-menu shortcut run with no instance, then the app clicked).
>   Of the review's two ways, the folder takes **a per-account wizard lock file** (`wizard.lock`
>   beside `desktop.lock`, the existing `lockfile` pattern), held by whichever process runs a
>   wizard, the instance or a standalone `wizard`. A second wizard anywhere finds it held and says
>   "The wizard is already open." in one line (a hand-off answers "already open"). The other way
>   ("the `wizard` verb in a graphical session becomes the instance") is not taken, because it
>   would not cover a terminal `wizard`. A row (MA-1430) covers it.
> - **MB-2.** The in-instance wizard's export runs on the instance's lifecycle context, not
>   `context.Background()` (`wizard.go:1892` today), and is a holder the stop paths wait for
>   (§10.4).

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
  MA-637). Nothing is lost; the person gets Desktop instead of the choice. *[Superseded
  2026-11-13 by MB-3 below: by capability, not version.]*
- Rate limit, nonce, body limit, constant-time checks, loopback only: unchanged. No new listener,
  no new port, no new file.
- `choose` and `wizard` answer at once (`{"shown": true}`); the dialog then belongs to the
  instance. The launcher exits 0.

> **Folded 2026-11-13 [menubar 10-lens MB-3, MB-14] (lead's call).** The "Version skew" bullet
> above is superseded: choosing the form by version fails for `dev` builds (incomparable) and sends
> a new request to an old instance, which answers 403; `Reach` then retries for 15 seconds and the
> launch wrongly says "not responding". Instead:
> 1. The instance file gains an **`acts` list** (the actions the instance knows). An absent field
>    means an older instance, which gets the old `open` request. Nothing parses versions.
> 2. A 403 to a new request from an instance that does advertise `acts` falls back **once** to the
>    old `open` request; it is never retried as "not responding".
> 3. The server refuses an unknown `act` with 400; it never defaults to `open`.
> 4. The immediate reply to `choose` and `wizard` is MAC-covered, for example
>    `"handoff-shown2|" + N + "|" + act + "|" + shown + "|" + reason`, where `reason` covers "already
>    open" and "can't show here" (§4 note, MB-9).
> 5. On Linux the activation token (§7.4) is inside the request MAC, bounded (at most 256 printable
>    ASCII characters), and passed to the dialog tool only as an environment value.
> 6. **MB-14:** the request also carries the launcher's version (inside the MAC), so the instance
>    can show the "newer MailArchive is installed" line (§6.1).

### 7.2 macOS
- The .app's no-argument launch becomes `start` (a one-row change to the pure routing:
  `resolveGUIFormerName` under the bundle path, which the `inBundle` input already tells apart).
  *[Citation superseded 2026-11-13 by MB-24 below.]*
- The instance runs `[NSApp run]` on the main thread (§8.2) with a delegate whose
  `applicationShouldHandleReopen:hasVisibleWindows:` shows the choice. So a click on the app while
  it runs reaches it through Launch Services, with no hand-off call at all — the path the spike
  proved.
- A launch Launch Services does not route (MailArchive started from Terminal or a LaunchAgent, then
  the app clicked) starts a second process; that process finds `desktop.lock` held and hands off
  with `choose` (§7.1), then exits. Both roads end at the same dialog. *[Clarified 2026-11-13,
  MB-11: MailArchive's own sign-in LaunchAgent runs `open -a`, so Launch Services does route it;
  "a LaunchAgent" here means one that runs the program directly.]*
- Activation policy "accessory" (no Dock icon) while only the indicator is up, so the Dock does not
  show a second MailArchive beside the menu bar one. (Owner can ask for a Dock icon; it is one line.)

> **Folded 2026-11-13 [menubar 10-lens MB-24, MB-4, MB-2] (lead's call).**
> - **MB-24.** `resolveGUIFormerName` takes only `args` (`cmd/mailarchive/dispatch.go:108`); the
>   `inBundle` input belongs to `defaultVerb` (`cmd/mailarchive/noarg.go:50`). Both change in S1:
>   the `mailarchive-gui` former-name row (with no `-job` it reaches `start`, not `wizard`) and
>   `defaultVerb`'s macOS bundle row (M1: it gives `start`). MA-611 and MA-620 are amended (§11).
> - **MB-4.** S1 adds `LSUIElement` to `packaging/macos/Info.plist.in`, which also stops a Dock icon
>   flashing before the accessory policy is set; any row that pins the plist is amended. L-M1's S1
>   run proves "reopen" reaches the product's symlinked executable before S1 ships. (The review
>   offered either; the folder takes both, since the S1 build is the product's bundle shape.)
> - **MB-2.** The delegate also answers `applicationShouldTerminate:` (§10.4): sign-out and
>   installers quit an `NSApplication` through an Apple Event, and AppKit's default path ends in
>   `exit()`, skipping the Go drain.

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

> **Folded 2026-11-13 [menubar 10-lens MB-7, MB-8, MB-17] (lead's call).**
> - **MB-7, one row per shortcut** (`packaging/mailarchive-desktop/Product.wxs`). "Start menu and
>   desktop shortcuts run `start`" above is made exact by this table:
>
>   | Shortcut (Id, name) | Today | After S1 |
>   |---|---|---|
>   | `DesktopShortcut`, "MailArchive Desktop" (desktop) | `desktop` | `start` |
>   | `StartMenuShortcut`, "MailArchive Desktop" (Start menu) | `desktop` | `start` |
>   | `WizardShortcut`, "MailArchive wizard" | `wizard` | `wizard` (unchanged; hands off `wizard` when an instance runs, D5; the wizard lock of §6.3 covers it when none does) |
>   | `CopyLinkShortcut`, "Copy MailArchive link" | `desktop -copy-link` | `desktop -copy-link` (unchanged; it hands off `open` and shows the link, as today) |
>   | `StartupShortcut`, "MailArchive Desktop" (all-users Startup) | `desktop -no-browser` | `start -background` |
>   | `UninstallShortcut` | `msiexec /x` | unchanged |
>
> - **MB-7, RMM scripts.** Install-RMM.ps1, Uninstall-RMM.ps1 and Cleanup-CurrentUser.ps1 learn to
>   recognise a `mailarchivew` process whose first argument is `start` (with or without
>   `-background`) as the instance and stop it; they still never stop `run-job`, `-job`, `-capture`,
>   `desktop -capture` or `wizard`.
> - **MB-7, MSI upgrade.** The one `util:CloseApplication` target stays `mailarchive-desktop.exe`
>   (MA-614). An instance running in any session when an MSI upgrade runs meets Restart Manager's
>   close request (`WM_QUERYENDSESSION` with `ENDSESSION_CLOSEAPP`, from memory), which goes to the
>   end-session stop path of §10.4 (MB-2): it waits for running work up to the bound and asks
>   nothing. If an instance refuses or outlives it, MB-14's "newer MailArchive is installed" line
>   tells the person.
> - **MB-8.** `AllowSetForegroundWindow` gives the instance the right to come forward, but
>   `dialogfront` still decides: the choice shows only after `Arm` (§6.1 note). The after-menu
>   foreground (the hidden window made foreground for the menu quirk, §8.1) is excluded from "one of
>   our windows is active" for any dialog that was not armed. L-W1 checks the choice in front of
>   Edge through both the hand-off and the menu.
> - **MB-17.** The launcher calls `AllowSetForegroundWindow` only for the pid that the lock's
>   holder record confirms (the `HolderPID` check in `internal/desktop/launch.go:299`), never for a
>   pid named only in the instance file.

### 7.4 Linux
- The desktop entry runs `mailarchive start`. Lock + hand-off as on Windows.
- Wayland compositors refuse focus to a window the person did not just ask for. The launcher passes
  its activation token (`XDG_ACTIVATION_TOKEN`, when the desktop gave it one) in the hand-off
  request; the instance hands it to the zenity process it starts, so the choice can come to the
  front. Where that fails the compositor shows "MailArchive is ready" instead; lab L-L2 records it.
- The no-argument row L1 stays dormant; the desktop entry names `start` explicitly.

> **Folded 2026-11-13 [menubar 10-lens MB-3, MB-9, MB-13] (lead's call).** The activation token is
> carried inside the request MAC, at most 256 printable ASCII characters, and reaches zenity only as
> an environment value (§7.1 note, item 5). Because the lock is per account on Linux (§2.2 note), a
> second graphical session of the same account (a local session plus xrdp) hands off to the first
> session's instance, and the choice appears on that display; the design accepts this and R50 says
> so. The desktop entry's change (`Exec=mailarchive desktop` to `Exec=mailarchive start`, and the
> READ ME's `~/.local/bin/mailarchive desktop` line) amends MA-1080 (§11).

### 7.5 What changes in the 2026-11-12 case
macOS: the click reaches the running process through "reopen" and the choice appears. Windows and
Linux: the click hands off through the lock and the choice appears. During the first upgrade from a
version without the event loop, the old process still swallows macOS clicks once (§2.4); the release
notes say "quit MailArchive Desktop once (Shut down) after installing this version".

> **Folded 2026-11-13 [menubar 10-lens MB-4, MB-14] (lead's call).** The macOS cause is confirmed
> (§2.4 MB-4 note: `open -a` against a running 1.7.0 Desktop fails with error -600 and starts no
> second process). That S1's event loop and reopen delegate fix it **on the product's bundle** is
> still to be shown by L-M1's S1 run before S1 ships; until then, "fixes" here reads "fixes, pending
> L-M1's S1 run". For every later upgrade, not only the first, see the §2.4 MB-14 note.

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

> **Folded 2026-11-13 [menubar 10-lens MB-8, MB-17, MB-2] (lead's call).**
> - **MB-8.** The hidden window is created **without `WS_CAPTION`** and is never shown, so it never
>   counts as a dialog (`isDialogWindow` requires a caption).
> - **MB-17.** Menu commands are taken from `TrackPopupMenu` with `TPM_RETURNCMD`; the window
>   procedure ignores `WM_COMMAND`, so another process in the session cannot post "Quit".
> - **MB-2.** The window procedure also answers `WM_QUERYENDSESSION` (calling
>   `ShutdownBlockReasonCreate` while a task runs) and drains in `WM_ENDSESSION` within about 5
>   seconds, then lets go (§10.4). The same path takes Restart Manager's close request during an MSI
>   upgrade (§7.3 note). The Windows end-session behaviour is from memory; L-W1 checks it.

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
  bundle's minimum) a bundled template PNG instead. *[Superseded 2026-11-13 by MB-11 below: the
  product cannot run below macOS 12.]*
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

> **Folded 2026-11-13 [menubar 10-lens MB-2, MB-10, MB-11, MB-19] (lead's call).**
> - **MB-2 (lands in S1).** The app delegate answers `applicationShouldTerminate:` with
>   `NSTerminateLater`, runs the one stop path (§10.4), and calls
>   `replyToApplicationShouldTerminate:YES` on the main thread when drained or when the bound runs
>   out. The terminate request from sign-out or an installer never shows the Quit question.
> - **MB-10.** While any task runs or a Desktop session is open, the instance holds an
>   `NSProcessInfo` activity (user-initiated, allowing idle system sleep) so App Nap does not
>   throttle it, and lets go after. Every Objective-C call that creates objects runs inside the run
>   loop or an explicit autorelease pool; MA-1426's main-thread assert extends to "inside a pool".
>   L-M2 measures a Run now with the browser in front, with and without the activity. Before S3, S4
>   or S5 ships, a 24-hour soak on each platform (memory, goroutines and handles flat) is recorded.
> - **MB-11.** Go 1.26.0's linker stamps a macOS **12.0** minimum
>   (`src/cmd/link/internal/ld/macho.go:440`), so the product cannot run on 10.15 or 11. Every
>   "macOS 10.15" branch in this design is dropped; the indicator targets **macOS 12+ (Go 1.26's
>   minimum)**, and SF Symbols (macOS 11+) are always available. The bundle's
>   `LSMinimumSystemVersion` of 10.15 is already untrue; correcting it is a separate operator fix.
> - **MB-19.** purego's no-cgo runtime shim is linked into the whole macOS program, so every verb
>   runs on it. L-M1 also runs a backup and a `run-job` with the purego build on both macOS slices:
>   arm64, and amd64 under Rosetta. The "+0.8 MB" (§9) is arm64 only.

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

> **Folded 2026-11-13 [menubar 10-lens MB-12] (lead's call).**
> 1. **Names on the bus.** The dbusmenu layout carries **no archive names** at any time; the Linux
>    menu uses generic words only (§5.2 note). The SNI `Title` and `ToolTip` already carry none.
> 2. **A late tray.** The fallback above is no longer final: at sign-in the autostart
>    entry can run before the panel's `org.kde.StatusNotifierWatcher` exists, and a panel restart
>    replaces it. The instance watches `NameOwnerChanged` for the watcher and registers again when
>    one appears (the Linux twin of Windows' `TaskbarCreated`); the header note and the instance
>    file's icon state (§4 MB-20 note) follow.
> 3. `Activate` and menu events are rate-limited like the hand-off (one a second, MB-17).

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

> **Folded 2026-11-13 [menubar 10-lens MB-4, MB-19] (lead's call).** The spike's bundle is not the
> product's: `Spike-Info.plist` sets `LSUIElement` and its `CFBundleExecutable` is a real file,
> while the product's `Info.plist.in` has no `LSUIElement` and its executable is the
> `mailarchive-gui` symlink. The "reopen reaches a pure-Go process" result therefore holds for the
> spike's shape only, until L-M1's S1 run (§7.2 note). The "+0.8 MB" was measured on arm64 only,
> on a toy program (MB-19).

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
  "backing up 1 archive" at most. *[Made true 2026-11-13 by MB-12: as first written, the dbusmenu
  layout would have carried archive names; see the §5.2 and §8.3 notes.]*

> **Folded 2026-11-13 [menubar 10-lens MB-17] (lead's call).** The same-session entry points are
> tightened: Windows menu commands only through `TrackPopupMenu`'s `TPM_RETURNCMD`, never
> `WM_COMMAND` (§8.1 note); `AllowSetForegroundWindow` only for the pid the lock's holder record
> confirms (§7.3 note); reopen, D-Bus `Activate` and tray-click "open Desktop" limited to one a
> second. The adversarial pass for S1 adds: a forged menu command message, a foreground grant to a
> pid named only in the instance file, and a flood of reopen events.
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
- Linux: the same rule, since the tooltip and menu are readable on the session bus. *[Superseded
  2026-11-13 by MB-12: on Linux the menu, too, shows no archive names, only generic words.]*

> **Folded 2026-11-13 [menubar 10-lens MB-12, MB-15] (lead's call).** Where archive names may
> appear: the opened menu on macOS and Windows only. Not in the tooltip (Q5), not in the choice
> window (MB-15, count form), and nowhere on Linux (MB-12).
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
  **Quit anyway**. For a restore, the restore's own cancel words. (Q4.) *[The words "stops at its
  next safe point" are superseded 2026-11-13 by MB-2 below: no stop path waits today.]*
- A run of *another* process (a scheduled backup) is not stopped by Quit, and the question does not
  appear for it; the menu's last line under Quit says "Scheduled backups still run when MailArchive
  is closed."
- The Shut down button in Desktop and Quit in the menu are the same action.

> **Folded 2026-11-13 [menubar 10-lens MB-2, MB-5, MB-22] (lead's call).** What the code does today:
> Desktop's shutdown drains pending secrets, deletes, the Thunderbird restore and the workspace
> export, but **nothing waits for the job slot** (Run now, Check integrity, Rebuild, the encryption
> copy), and the wizard's export would run on `context.Background()`. So the words above promise
> more than happens. The rule, which **lands in S1** (S1 already brings the AppKit loop in):
> 1. **One stop function, every stop path.** The menu's Quit, Desktop's Shut down button, sign-out
>    or logoff, an installer or upgrade, and OS termination (SIGTERM, Ctrl-C) all call
>    `Lifecycle.Stop` and wait for `Drained`.
>    - macOS: `applicationShouldTerminate:` answers `NSTerminateLater`, and
>      `replyToApplicationShouldTerminate:YES` is sent on the main thread when drained or when the
>      bound runs out (through purego, §8.2 note).
>    - Windows: `WM_QUERYENDSESSION` is answered (with `ShutdownBlockReasonCreate` while a task
>      runs), and the drain runs in `WM_ENDSESSION` (§8.1 note).
> 2. **Wait up to a bound, then record the run as interrupted.** Each stop waits for running work up
>    to its bound: the existing `DrainBudget` (15 seconds, `internal/desktop/lifecycle.go:10`) for
>    Quit, Shut down and SIGTERM; about 5 seconds inside Windows' `WM_ENDSESSION`; and on macOS
>    what the system allows after `NSTerminateLater`. A task still running at the bound is
>    cancelled (cancellable jobs through their context; check and rebuild, which are not
>    cancellable, are stated per kind in S1), and its run is recorded as **interrupted**. That record
>    is written whole (write-then-rename, as the state records are) or not at all: **never a
>    half-written record**. The next run's intent recovers the archive, as after a crash.
> 3. **Only a person's own Quit asks first.** The menu's Quit and Desktop's Shut down button (the
>    same action, above) ask while a task runs, with **Keep running** as the default. Sign-out, an
>    installer, Restart Manager and OS termination **never** show the question.
> 4. **A drain for every holder (MB-5).** A drain is registered that cancels and waits for the job
>    slot and for the in-instance wizard export (bound to the lifecycle context), alongside the
>    existing ones. The question reads every holder of §5.1's MB-5 note, not only the job slot.
> 5. **True words.** The dialog says what does happen, per task kind, for example "If you quit now,
>    the backup stops now; the next backup finishes what it left."
> 6. **Tests** with real primitives (MA-1424): a real `Lifecycle`, a job held in the slot by fault
>    injection, asserting that stop waits for it up to the bound, records it as interrupted, and
>    that the end-session path shows no dialog. The macOS delegate path is a smoke row plus lab.
>
> **MB-22.** The delete and unlist refusal (`desktopRunningText`,
> `cmd/mailarchive/archivedelete.go:93`, "click Shut down at the top of MailArchive Desktop") gains
> "or Quit MailArchive from its icon", since with the instance running from sign-in this refusal
> will be met far more often.

### 10.5 Start at sign-in
- Windows: the MSI's all-users Startup shortcut stays (it is how the background Desktop starts
  today) and becomes `start -background`, so what was invisible is now the icon. A per-user "Open at
  sign-in" tick (in the menu and in Desktop's Settings) is stored in the user's settings; when it is
  off, the Startup launch exits at once and quietly.
- macOS: opt-in, default off (Q3). On macOS 13+ registered with `SMAppService.mainApp` (through
  purego; it appears in System Settings → Login Items with the app's name, where the person can also
  turn it off); on 10.15–12, a LaunchAgent plist that runs `open -a` on the app, so Launch Services
  owns the process and "reopen" routing works. *[Superseded 2026-11-13 by MB-11 below: one
  mechanism, the LaunchAgent, on every supported macOS.]*
- Linux: opt-in, default off: an XDG autostart entry (`~/.config/autostart/mailarchive.desktop`,
  `Exec=mailarchive start -background`).
- Uninstall removes the per-user entries it made (the uninstall plan lists them, as it lists
  schedules today); archives are never touched (R36).

> **Folded 2026-11-13 [menubar 10-lens MB-11, MB-21] (lead's call).**
> - **MB-11, macOS.** One mechanism on every supported macOS (12 and later; §8.2 note): a per-user
>   LaunchAgent that runs `/usr/bin/open -a <bundle path>` (`open` is already on MA-612's tool
>   list), so Launch Services owns the process and reopen routing works. `SMAppService.mainApp`
>   waits until a lab shows it registering and staying registered across two unsigned upgrades in a
>   row (its behaviour with an ad-hoc-signed bundle whose code identity changes every release is
>   unverified). Turning "Open at sign-in" on **refuses while the app runs translocated** (from
>   Gatekeeper's App Translocation, a random read-only path), reusing `ProgramForSchedule`'s rule
>   and words (`internal/schedule/program.go:227`, MA-615). A lab row covers two upgrades in a row
>   with the LaunchAgent on.
> - **MB-21, Windows (default pending the owner).** The MSI's all-users Startup entry will now put
>   an icon in front of every user who signs in, including those with no archive. Default: the
>   `start -background` launch **exits quietly for a user whose archives list is empty**, as it does
>   when the per-user tick is off. This is an owner ruling, recorded as the default until the owner
>   says otherwise (§13 note); MA-1433 is widened to cover it.

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
  words. *[Superseded 2026-11-13 by MB-23 below.]*
- **R51 — The indicator tells, and tells only what it may.** While MailArchive runs, its state
  (idle, the running task's kind, needs attention) is visible outside the browser; the indicator
  shows only fixed words, archive names (in the menu only), task kinds and times — never mail
  content, folder names, log lines, paths or links.
- **R52 — Quitting never silently stops work.** Quitting while this process runs a task asks first;
  quitting never stops a scheduled backup or removes a schedule. *[Superseded 2026-11-13 by MB-23
  below.]*

> **Folded 2026-11-13 [menubar 10-lens MB-23, MB-1, MB-12] (lead's call).** Sharpened invariants:
> - **R50 — One MailArchive per account.** At most one instance (the holder of the account's
>   `desktop.lock`) runs per account (per logon session on Windows). Every GUI launch either
>   becomes it or reaches it through a proven hand-off and exits. A launch never ends silently: it
>   shows the choice, Desktop, or a refusal in words. (Launchers, a standalone `wizard` and an older
>   version's process are not "the instance"; the wizard lock, §6.3, keeps one wizard per account.)
> - **R51** stands, with: the attention facts are read without a lock or a write (MB-1); on Linux
>   the indicator shows no archive names at all (MB-12).
> - **R52 — No stop silently cuts work short.** Every stop (Quit, Shut down, sign-out, installer,
>   OS termination) waits for this process's running work up to a bound, then records it as
>   interrupted, never as a half-written record. Only the person's own Quit asks first. No stop ends
>   a scheduled backup run by another process or removes a schedule.

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

> **Folded 2026-11-13 [menubar 10-lens MB-1 to MB-21, MB-13, MB-18] (lead's call).** Changes to the
> table above (its rows stay as drafted; these amend them):
> - **MA-1401** (MB-3): "a forged or missing act is refused/treated as `open`" reads: a request
>   under the old label with no `act` is `open`; an unknown `act` under the new label is refused
>   with 400, never treated as `open`.
> - **MA-1402** (MB-3): superseded. It proves the capability rule instead: no `acts` in the
>   instance file gets the old `open` request; a 403 from an instance that advertises `acts` falls
>   back once, not to "not responding".
> - **MA-1403** (MB-18): two real processes of the built program and a real lock, not only a fake
>   dialog seam.
> - **MA-1408** (MB-18): the fake AppKit seam is named as an operating-system seam.
> - **MA-1413** (MB-1): "index behind" is not an attention fact.
> - **MA-1422** (MB-2): moves to S1 for Desktop's Shut down button; the menu's Quit joins it in
>   S3–S5.
> - **MA-1428** (MB-18): tier S, on a real private `dbus-daemon` session bus, not an in-process
>   fake; it also proves the layout carries no archive name (MB-12).
> - **MA-1433** (MB-21): also covers "exits quietly when the archives list is empty".
> - **MA-1436** (MB-4): the one-copy case is recorded (the lead's `open -a` run, §2.4 note); the
>   two-copies case, the product bundle shape and MB-19's backup and `run-job` on arm64 and on amd64
>   under Rosetta remain.
>
> New rows, in this design's free ids (placeholders the S0 commit may renumber within the range):
>
> | Id | Tier | What it proves | Slice |
> |---|---|---|---|
> | MA-1417 | U | MB-1: the attention reader writes nothing: a full pass over tempdir archives (no lock file, read-only, encrypted) leaves names, sizes, mtimes and bytes unchanged and creates no file; an archive whose folder is absent is not opened | S2 |
> | MA-1418 | U | MB-1, X10: the "Your archives" row and the indicator use one classifier and agree on a fixture set | S2 |
> | MA-1419 | U | MB-6: own-task de-duplication, the empty-verb debounce, and a Windows delete meeting a concurrent probe (real files, fault injection for timing) | S2 |
> | MA-1424 | S | MB-2, R52: every stop path waits for a job held in the slot (fault injection) and for the wizard export up to the bound, records the run as interrupted (whole record or none), and the end-session path shows no dialog; real `Lifecycle` | S1 |
> | MA-1427 | U | MB-3: `acts` capability, the one-time 403 fallback, 400 on unknown `act`, the MAC-covered `shown` reply with its reason, the bounded activation token, the launcher version in the MAC | S1 |
> | MA-1430 | U | MB-9: one wizard per account across processes (wizard lock); an instance that cannot show dialogs answers "can't show here" and the launcher opens Desktop or shows the link | S1 |
> | MA-1431 | U | MB-14: an older instance meeting a newer launcher or bundle shows the "newer MailArchive is installed" line and never restarts itself | S1 |
> | MA-1434 | U | MB-16, MB-20: Cancel where no icon can show stops an instance this launch started with nothing running; the instance file's icon state and `status`'s running/version/why-no-icon words | S1 |
> | MA-1435 | U | MB-8, MB-17: each new dialog entry arms first and a hand-off arms only after its proof; the hidden window has no caption; menu commands only through `TPM_RETURNCMD`; foreground granted only to the holder-confirmed pid; reopen/`Activate`/tray-click rate limit | S1 |
>
> If more ids are needed, the S0 commit re-checks for clashes before using any id past MA-1439.
>
> **MB-13, existing rows this design amends** (each lands in S0 as "amended, not weakened", with
> the operator's acceptance, before S1's code; never edited to make a test pass):
> - **MA-611.** Old: "`mailarchive-gui` … with no `-job` reaches `wizard`". New: "… with no `-job`
>   reaches `start`".
> - **MA-620** (`defaultVerb`'s M1 row). Old: "macOS: the bundle path with no terminal gives
>   `wizard`". New: "… gives `start`".
> - **MA-614.** Old: "Product.wxs's shortcuts target `mailarchivew.exe` with the three `desktop`
>   arguments"; "Install-RMM.ps1 … stops a `mailarchivew` process only when its CommandLine is a
>   dashboard (no argument … or a first argument `desktop` without `-capture`)". New: the shortcuts'
>   arguments are §7.3's table (`DesktopShortcut` and `StartMenuShortcut` `start`, `StartupShortcut`
>   `start -background`, `CopyLinkShortcut` `desktop -copy-link`, `WizardShortcut` `wizard`); a
>   dashboard is also a first argument `start`; Uninstall-RMM.ps1 and Cleanup-CurrentUser.ps1
>   likewise; still never `run-job`, `-job`, `-capture`, `desktop -capture` or `wizard`. The single
>   `util:CloseApplication` target is unchanged.
> - **MA-1080.** Old: "Exec=mailarchive desktop", and the READ ME's "`~/.local/bin/mailarchive
>   desktop`". New: `Exec=mailarchive start` and `~/.local/bin/mailarchive start`; the installed
>   entry's full-path rewrite is unchanged.
> - **MA-1111 and MA-1117.** Old: dialogs are reached only through `desktop.Launch`, `wizard.Run`
>   (and the listed asking places). New: the instance's entry points are added, the choice on start,
>   on hand-off and on reopen, the menu's "Run the wizard…", and the Quit question, each installing
>   and arming first (MB-8). The walk is widened, not duplicated.
> - **X11.** Old: "if this account's instance is already running it reconnects to that one … and
>   has it open the browser … never a silent, invisible background server". New: "… it reaches that
>   one, which shows the choice (or, where it cannot show dialogs, opens the browser or shows the
>   link) … never a silent, invisible background server: the instance shows its icon, or where no
>   icon can show, a Cancel with nothing running stops it (MB-16)."
> - **S59.** Old: "a no-argument launch follows §4.3 … always opens a visible surface or prints the
>   usage". New: the words stand; its macOS bundle case now reaches `start`, which shows the choice.
>
> **MB-18, which rows gate CI.** `ci.yml` runs `go test ./...` on Ubuntu, Windows and macOS. Per the
> project note (not re-checked here), only the Ubuntu run is treated as the gate, because the
> Windows run has a known flaky pool. So: every U row, MA-1403, MA-1424 and MA-1428 (a private
> `dbus-daemon` is available on Ubuntu) must pass on Ubuntu; the Windows-only rows (MA-1409,
> MA-1420, MA-1421, the Windows parts of MA-1435) and the macOS rows (MA-1408, MA-1425, MA-1426)
> run on their CI runners as non-gating and are backed by the labs. AppKit and Win32 fakes are
> operating-system seams, named as such; core logic uses real processes, a real lock and a real
> session bus.

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
  *[Sign-in superseded 2026-11-13, MB-11: LaunchAgent only.]*
  *Gates: friction review; L-M2.*
- **S5 — Linux indicator** (SNI + dbusmenu, fallback). *Gates: adversarial pass on the D-Bus
  surface; friction review; L-L1/L-L2.*
- **S6 — start at sign-in** across the three, uninstall entries. *Gates: adversarial pass on the
  files it writes in the person's profile (paths, links, permissions); friction review.*
- **S7 — words and help**: Help page section, README, release notes, UX contract rows.

Every slice ships prove-fail → prove-pass (mutate the fix, watch its covering test go red,
revert), recorded in the commit; the unfiltered `go test ./...` runs before each push.

> **Folded 2026-11-13 [menubar 10-lens, all conditions] (lead's call).** Where each condition lands:
> - **S0** gains: the MB-13 amendments of MA-611, MA-614, MA-620, MA-1080, MA-1111, MA-1117, X11 and
>   S59 (operator's acceptance before S1's code); R50 and R52 as sharpened (MB-23); the new rows
>   MA-1417 to MA-1435 (§11 note).
> - **S1** gains: MB-2's one stop path on every stop (Quit, Shut down, sign-out, installer, OS
>   termination), with the macOS `applicationShouldTerminate:` and Windows
>   `WM_QUERYENDSESSION`/`WM_ENDSESSION` hooks, the job-slot and wizard-export drains, the
>   "interrupted" record and true Quit words; MB-3's capability hand-off; MB-7's shortcut table and
>   RMM scripts; MB-8's Arm rule for the new dialogs and the caption-less hidden window; MB-9's
>   wizard lock and "can't show here"; MB-14's newer-version line; MB-16's Cancel rule; MB-17's
>   entry-point limits; MB-20's icon state and `status` words; MB-22's refusal words; MB-4's
>   `LSUIElement`; MB-24's routing citations. S1's gates add **L-M1's S1 run** (two copies, product
>   bundle shape, MB-19's backup and `run-job` on arm64 and amd64 under Rosetta) before it ships.
>   Because MB-2's Windows hooks need a window procedure, S1 creates the hidden window (no icon
>   yet) on Windows too.
> - **S2** gains: MB-1's read-only attention reader, MB-5's every-holder state, MB-6's probe rules.
>   MB-1, MB-5 and MB-6 are folded here and must be re-verified before S2's code.
> - **S3, S4, S5** gain: MB-10's 24-hour soak on each platform before each ships; MB-12's Linux
>   rules in S5; MB-11's LaunchAgent (not `SMAppService`) in S4 and S6. *The S4 line's "sign-in
>   through SMAppService/LaunchAgent" is superseded: LaunchAgent only.*
> - **S6** gains: MB-11's translocation refusal and two-upgrade lab; MB-21's quiet exit for a user
>   with no archives (default pending the owner).
> - **Labs (§12.3)** gain: L-M1's S1 run as above; L-M2 measures App Nap with and without the
>   activity (MB-10); L-W1 checks the choice in front of Edge through both the hand-off and the menu
>   (MB-8), and logoff and MSI-upgrade end-session behaviour (MB-2, MB-7); a macOS lab of the
>   LaunchAgent across two unsigned upgrades (MB-11).

> **Built 2026-11-13 (slice S1; the builder's calls, for the lead to check).**
> - **MB-22 is held back to the first icon slice.** "or Quit MailArchive from its icon" would be
>   untrue while no icon exists; the refusal keeps naming Shut down until S3–S5 add the icon.
> - **MB-7's RMM scripts:** only Install-RMM.ps1 changes (a first argument `start` is a
>   dashboard). Uninstall-RMM.ps1 and Cleanup-CurrentUser.ps1 already stop every
>   `mailarchivew` process, a `start` instance among them, so they are unchanged.
> - **Task words before S2's job-slot kind:** the Quit question and the choice's "Now:" line name
>   a backup (Run now), a restore to Thunderbird, a delete, a workspace export and the wizard's
>   copy; a check, a rebuild or the encryption copy reads "a task is running" until S2 adds the
>   kind (MA-1410).
> - **Linux choice text:** the choice's extra lines (the newer-version line, the "Now:" line)
>   follow MA-1286's rule: on Linux they show first in a message dialog, then the list with its
>   fixed question; macOS and Windows show them above the list.
> - **A hand-off that showed nothing does not count against the one-a-second limit**, so the
>   launcher's fallback open after "can't show here" is not answered "busy" and lost.
> - **The `wizard` verb hands off before its dialog preflight**, so a launch with no dialog tool
>   still reaches a running copy that has one.
> - **The two-process proof of MA-1403** is the Mac smoke lab (MA-1436); the unit run is
>   in-process over the real lock and listeners.
> - **MB-14's Quit is a third list item**, "Quit MailArchive — then open it again to use the
>   newer version", not a button: on macOS the dialog library's list has no extra button
>   (found in the S1 Mac lab, `docs/lab/menubar-s1-mac-2026-11-13.md`).

> **S1 fix round 2026-11-13 (the adversarial pass A1–A9 and the friction review F1–F16;
> the lead's calls and the builder's, for the lead to check).**
> - **Lead's call, F2:** when the choice is dismissed, or the wizard (from the choice or a
>   hand-off) ends, with nothing running, no wizard open and no Desktop page opened or asked
>   for in this run, the program ends. This extends MB-16's first-start Cancel to every
>   choice; a sign-in copy (`start -background`) whose later choice is dismissed ends too. The
>   icon slices (S3–S5) revisit this.
> - **Lead's call, F3:** the "choice is showing" state ends before the picked step runs, so a
>   click during the wizard is answered in the copy. *Changed by the VM walk:* the dialog
>   library on Windows registers one window class per dialog kind, so a second list cannot
>   open while the wizard's list is open in the same process (RegisterClassEx fails and the
>   choice never shows). So while a wizard of this process is open, a click asks "The wizard
>   is open. Open MailArchive Desktop in your browser as well?" (a message box, OK/Cancel)
>   instead of the list; while another process runs the wizard, the list shows as before.
>   Residual: the newer-version choice (with its Quit item) during an open wizard would still
>   fail on Windows (rare).
> - **Lead's call, A4/F5/F9:** an open wizard of this process (it holds the wizard lock) is
>   work: the Quit question and Shut down ask ("The wizard is open. Quit anyway? The wizard
>   closes, and its unanswered questions are dropped."), the first choice's dismissal never
>   ends the program under it, Windows' installer close request is refused. A stop waits for
>   the wizard's own writes after its export (job file and schedule install), not for a wizard
>   that only waits for an answer. *Builder's note:* the wizard's own dialogs are not closed
>   by a stop; on macOS and Linux a dialog left open when the process ends stays on the
>   screen with nothing behind it (residual, LOW; the choice's own list now closes with the
>   instance through `zenity.Context`).
> - **Lead's call, A2:** Install-RMM.ps1 asks each dashboard to close the graceful way and
>   never forces a copy that refuses (work or a wizard): the script then exits **1618**
>   before msiexec (builder's pick of code: RMM tools retry it; the lead may prefer another).
>   *Found on the VM:* Restart Manager classifies the copy as an unknown app (its only window
>   is hidden), and RmShutdown without force returns 350, so neither the script nor the MSI's
>   own Restart Manager can ask it. The script therefore runs a one-time interactive task as
>   the copy's user, in its session, that posts WM_CLOSE to the copy's
>   `MailArchiveInstanceWindow` and waits up to 30 s. A dashboard with no such window (a copy
>   from before S1) is still forced, as CC22 needs. *Consequence for MB-7's note:* an MSI run
>   directly (not through the script) meets a running copy as files in use (a restart to
>   finish), never a close; L-W1 should confirm.
> - **Builder's call, A1:** a WM_CLOSE from outside never destroys the hidden window: an idle
>   copy takes it as the one stop (asking nothing, waiting); a copy running work or an open
>   wizard refuses it (as it refuses an installer). The loop's own end uses a private message
>   honoured only after the serving ended; a loop that ends early (a posted WM_QUIT) is a stop
>   request. `taskkill` without /F also sends WM_CLOSE to the wizard's own dialog windows,
>   which close as a person's Cancel would (seen on the VM).
> - **A3:** every launch, `desktop` included, uses the second form's `open` with its sign-in
>   when the copy lists it; the copy's gate answers before anything shows. Where a sign-in
>   cannot be read (macOS and Linux; Windows' lock is per session) it fails closed with
>   "unknown-session" and the launch says it cannot tell. Linux falls back to the kernel's
>   audit session (`/proc/self/sessionid`) when XDG_SESSION_ID is absent. Whether GNOME-started
>   apps carry a readable sign-in is the Linux lab (MA-1439, pending): if not, a second click
>   there gets the "cannot tell" notice instead of the choice.
> - **A5:** WM_ENDSESSION stops the copy only with SM_SHUTTINGDOWN or ENDSESSION_CLOSEAPP (the
>   latter can be forged: it narrows, not closes, the gap; that stop still waits). §10.1's
>   "acts only on its own callback message and TaskbarCreated" now reads: and on the end of
>   the session as above, and on a close request as a waiting stop.
> - **A6:** a bounded record of 1024 used nonces; a repeat is 403. The v2 MAC also covers
>   `quiet`.
> - **A7:** the copy marks its instance file `closing` when a stop begins and answers
>   "closing"; a launch waits up to the drain's bound plus 10 s, then becomes the next copy or
>   says "MailArchive is finishing its work and closing. Open it again in a moment."
> - **A8:** not provable on the shared VM (a real sign-out would end other jobs' sessions):
>   left as lab L-W1, pending, named in MA-1424 and R52.
> - **A9:** the activation token is set in the choice's dialog process environment while it
>   starts, then removed.
> - **Words:** the friction review's (F1, F6, F16, R50, the Quit question). The backup's "the
>   next backup picks up where it left off" was checked in the engine: a cancel commits the
>   manifest and index before it returns (`internal/app/run.go`, the cancel path; R5, R2), the
>   run record says "cancelled" (Recent backups: "Cancelled — stopped before it finished"), and
>   the next run copies only what is missing. The prompt is one line: the newer-installed line,
>   else "Now: …", else the question. F10's wizard question "What mail do you want to add or
>   back up?". F14's terminal words. F11: the list needs a pick for OK.
> - **Not done here:** F7 (renaming the "MailArchive Desktop" shortcuts to "MailArchive") is a
>   packaging and Help-guide change the friction review sent to the lead or owner; F12's arrow
>   keys: the dialog library's list is a standard Windows LISTBOX with the keyboard set on it,
>   which handles the arrows itself (nothing to change); F13 (the keyboard after the launch's
>   notice) is backlog; F15 is covered by A7. F12 on the VM: no key could be sent, since the
>   keyboard belonged to the Parallels console window, not the lab's dialog; a real-keyboard
>   pass stays owed.

> **S1 fix round 2, 2026-11-13 (the re-check `docs/review-menubar-s1-adversarial-r2.md`, B1–B10
> and A5; the lead's direction, and where the builder deviated, why).**
> - **B1, B2, B5, B7 — the upgrade asks through the program, never by window messages.** A new
>   verb, **`mailarchive quit [-check] [-pid N] [-wait 30s]`**, asks this account's running copy
>   over the signed hand-off (two new actions, `can-quit` and `quit`, advertised in the instance
>   file): an idle copy quits by the one stop and waits for its drain; a copy running work or with
>   its wizard open answers "busy" and keeps running. Exit 0 quit (or, with -check, would), 1 could
>   not ask (the reason said; usage errors too), 2 not running, 3 busy or still finishing after
>   -wait. *Gate found while building:* the lead's "a distinct code per outcome" first got codes
>   3–7, but X1 (docs/ux-contract.md) and MA-613 pin every exit code the program emits to
>   {0, 1, 2, 3, 130, 143}; MA-613's scan did not see codes defined in `internal/desktop`, so it
>   stayed green — routing around the gate, and widening it is the operator's call. So the four
>   outcomes the lead named (closed, busy, none, could not ask) take 0, 3, 2 and 1, and an older
>   copy is not a fifth code: with `-end-older` (only the script passes it) `quit` ends a copy
>   whose own record lists no actions, as upgrades did, and never a copy that can be asked. A test
>   now pins quit's codes inside MA-613's set. If the operator prefers separate codes for "older"
>   and "still finishing", that is a widening of X1 for them to accept. `-pid` finds that process's own sign-in's instance file, so a user signed in
>   twice, or a copy at a higher integrity, is reached all the same (the hand-off is loopback, not a
>   window). Install-RMM.ps1 asks every dashboard `quit -check` first and asks them to `quit` only
>   when none said no (B7); busy → nothing closed, exit 1618; could not ask → never "busy", never
>   forced, exit **3** with the process and the reason. *Builder's calls:* the verb is `quit` (the
>   CLI's verbs are single words; a person at a terminal can use it too), not `instance close`; the
>   task runs the installed **`mailarchivew.exe`** (no console window flashes in the user's
>   session), not `mailarchive.exe`; the script's codes for the verb's answers are pinned to the
>   verb's own constants by MA-614's test.
> - **"Before S1" is decided by version, never by unreachability.** Our Windows programs carry no
>   file-version resource (the .syso holds only the icon), so: (a) the copy's own instance file —
>   no hand-off actions listed is a version from before S1, which `quit -end-older` ends as before;
>   (b) for an installed MailArchive that predates `quit`, the MSI's key now gets a
>   `QuitVerb` value (Product.wxs): when it is absent, a copy running that installed program is from
>   before S1 and is ended as before; a copy running from anywhere else cannot be asked → exit 3.
> - **B5, B6 — the task.** Random name (`MailArchive upgrade <GUID>`), created without /f (a name
>   taken fails), a protected security descriptor in its own XML (SYSTEM and Administrators full,
>   the user read), its action checked after creation, `ExecutionTimeLimit` 2 minutes, a trigger
>   that never fires with an `EndBoundary` 15 minutes on and `DeleteExpiredTaskAfter` PT0S, deleted
>   in a `finally`; it may start on battery (the round-1 task inherited "do not start on battery",
>   which would have made an idle laptop's copy look busy). *Deviation:* a task **folder** that only
>   SYSTEM and Administrators can write needs the Task Scheduler COM object to set its security,
>   which Constrained Language mode blocks; the task's own protected descriptor gives the same
>   protection in the root folder. *Found on the VM:* the ScheduledTasks module (CDXML) does not load
>   in a session switched to Constrained Language mode, so the script makes and runs the task with
>   **schtasks.exe**; it reads the last result from the verbose CSV row by position (the labels are
>   localised, the column order is not). The installed program is run only from under
>   `%ProgramW6432%` (Program Files): it runs as another user, so it must sit where only
>   administrators write (else exit 3, "not installed under Program Files").
> - **B3 (and A5):** a posted WM_QUIT is a close request like WM_CLOSE — refused while busy, the
>   message loop going on (the window still answers sign-out), the one waiting stop when idle. A
>   WM_ENDSESSION with ENDSESSION_CLOSEAPP stops the copy only after the copy allowed that request's
>   WM_QUERYENDSESSION (once) and while still idle; a lone one is ignored.
> - **B4, accepted LOW (builder's call):** the dialog library answers a WM_CLOSE to the wizard's own
>   dialog exactly as its Cancel (both are its "canceled"), so telling them apart would mean
>   changing the library. `taskkill` without /F therefore closes an open wizard like Cancel, and an
>   idle copy then ends; the wizard's Cancel is a safe point (its copy stops; "run the wizard again
>   to finish it"). MA-1424 and MA-1435 say so.
> - **B8:** Linux's sign-in is XDG_SESSION_ID, else logind's own answer for the process — the
>   session scope its control group sits in (`session-<id>.scope`, the rule of systemd's
>   `sd_pid_get_session`), read from `/proc/self/cgroup`, with no new dependency (godbus stays S5's)
>   — never the kernel's audit session. A process the user manager started (GNOME on Wayland starts
>   apps under `user@<uid>.service`) has none: "unknown", the "cannot tell" notice, fail closed. If
>   GNOME-started copies all land there, a second click on GNOME never shows the choice; MA-1439
>   (pending, now in the catalog) settles that before the Linux release.
> - **B9 (deviation: a link, not a refusal):** a first-form open carries no sign-in; where one copy
>   serves every sign-in (macOS, Linux) the copy answers it as a quiet open — a link the older
>   launcher shows in its own session — never its own browser. A refusal (503) would make an older
>   launcher retry for 15 s and then say the copy is "not responding … quit it in Task Manager",
>   which is false and asks the person to kill a copy that may be working; a link is what the
>   unknown-sign-in rule already gives a quiet open, and opens nothing on the copy's screen.
> - **B10:** the token is set, the dialog started, and the token taken out of the environment as
>   soon as a new child process of the copy appears (the dialog tool; Linux reads /proc), at most
>   2 s after, or when the dialog returns.
> - **Residuals, for the lead:** the quit request's idle check and its stop are not one step (as
>   WM_CLOSE's are not): work started in that instant is cancelled by the stop, waited for and
>   recorded as interrupted. Install-RMM.ps1 still ends the former name `mailarchive-desktop.exe`
>   unconditionally (MA-614, SC34), and this release's former-name file is the S1 program: a copy
>   started under that name is ended whatever it is doing — outside B1–B10, not changed here. No
>   admin override for a copy that stays busy for days (the review's suggestion): the runbook tells
>   the admin to have the user click Shut down. A8 stays the L-W1 lab.
> - **The Windows VM lab of this round (2026-11-13, Windows 11 ARM VM, lab builds `ma2fixw.exe`
>   and `ma2fix.exe` from this branch, the real close step cut from Install-RMM.ps1 between its
>   `# --- functions ---` and `# --- the upgrade ---` markers and run as SYSTEM against lab copies
>   only, with a lab wrapper standing in for the installed program that points the verb at the
>   lab copy's own state folder; files under `.winlab/fix2/`):**
>   - *In a real user session (before the switch to schtasks.exe):* an idle copy, full language:
>     "Closed MailArchive (process 13584) after it finished its work.", code 0 after 7 s, the
>     copy gone, no task left.
>   - *Under Constrained Language mode (the session switched, as the re-check did):* the first
>     try found the ScheduledTasks module cannot load; the script said "could not be made",
>     exit 3, forced nothing (the B2 rule held even then) — hence schtasks.exe. Then Windows
>     Update restarted the VM (22:14, MoUsoCoreWorker, "Service pack (Planned)"), signing the
>     user out; no one signed in again, and this round signs no one in, so the rest ran against
>     copies started as SYSTEM in session 0: under CLM the whole close step ran with
>     schtasks.exe (no CLM error), the task ran, the verb answered "could not ask" — a
>     SYSTEM-started copy's record is owned by Administrators while the task's token differs,
>     exactly what a copy started "as administrator" meets — and the script printed it, exit 3,
>     the copy alive, no task left. The legacy path under CLM: a copy not running the installed
>     program is exit 3; one running it is ended, code 0.
>   - *B6:* the close step killed 2 s in left `\MailArchive upgrade <GUID>` with
>     `D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;…)`, limit PT2M, delete-after PT0S, end 22:41:54;
>     at 22:42 Task Scheduler had deleted it.
>   - *B3:* MA-1435's tests and their two mutations ran on the VM (as SYSTEM, no user signed in).
>   - *Owed (L-W1 above):* the success path and a busy refusal under CLM in a real user session,
>     two users or two copies (B7) end to end, a copy started as administrator.
> - **Owner's ruling on F7, 2026-11-13 ("rename it", relayed by the lead):** what opens the
>   choice is named **MailArchive**. Done here: the MSI's Desktop, Start-menu and Startup
>   shortcuts (the three that run `start`) are named "MailArchive" with the description "Add or
>   back up mail, or open MailArchive Desktop in your browser"; the Linux menu entry
>   (`Exec=mailarchive start`) likewise, with its READ ME; the guide's way back on Windows, the
>   README and the release notes' first steps and README-RMM name the MailArchive shortcut.
>   Kept: the product name, its Program Files and Start-menu folders, the Uninstall entry and the
>   DC6 key stay "MailArchive Desktop" (renaming the product is not what F7 asked, and DC6 pins
>   the key). *Builder's finding, macOS:* the app is "Mail Archive" (`Mail Archive.app`,
>   CFBundleName) and opens the choice, but never said "Desktop", so it is not F7's mismatch; it
>   is left alone (a renamed .app would sit beside the old one after a drag-install upgrade). The
>   guide's macOS way back still named the old wizard item and now says "choose Open MailArchive
>   Desktop in your browser". *Upgrades:* the shortcuts are plain shortcuts of the product's
>   component and the MajorUpgrade removes the earlier product, with its shortcuts, before the
>   new files go in (afterInstallInitialize), so no "MailArchive Desktop" shortcut is left; a
>   pin a person made to the old shortcut is theirs to re-pin. Not proven on the VM (installing
>   over the shared VM's own MailArchive would change it): L-W1 below. Linux: the entry keeps its
>   file name, so the install line replaces it in place. MA-1431 and MA-1080 are amended.

> **S1 fix round 3, 2026-11-14 (the re-check `docs/review-menubar-s1-adversarial-r3.md`, C1–C5;
> the lead's direction, and the builder's calls, for the lead to check).**
> - **C1 — the instance file on Windows gets a Medium no-read-up label.** Every instance file
>   (the desktop's per session, and a `serve`'s; the only files that hold K — the settings file,
>   the logs, the locks and the job files hold no key, and Desktop's session secrets live only in
>   memory and the browser's cookie) is written by a new `util.WriteSecretFileAtomic0600`: on
>   Windows its temporary file is created by `CreateFile` with the security descriptor
>   `S:(ML;;NRNWNX;;;ME)` (only a SACL label; owner and DACL come from the folder and token as
>   before), and the rename keeps it. A system that will not set the label makes the write fail
>   (the copy then says it could not record itself), never a readable key. *VM, as SYSTEM (no one
>   signed in), a lab copy started at Medium integrity with a LOCALAPPDATA-like ACL:* the file
>   shows `Mandatory Label\Medium Mandatory Level:(NW,NR,NX)`; at Medium the probe and
>   `quit -check` reach the copy; at Low the probe's read and `quit -check` are "Access is
>   denied", the copy untouched; `quit` at Medium then quits it. The same lab with the **shipped
>   v1.7.1** (built from the tag): a Low process reads K, the quiet hand-off returns a link, and
>   its redemption gives a Desktop session (status 200) — the exposure ships today (lead: patch
>   decision). Owed: the same with a real user's token (L-W1).
> - **C2 — logind first on Linux.** Where systemd runs (`/run/systemd/system` exists, sd_booted's
>   test, or the process's own group path names a systemd unit), the sign-in is logind's answer
>   only: the `session-<id>.scope` the process sits in, else "" (unknown, the "cannot tell"
>   notice) — even when `XDG_SESSION_ID` is set, since the shared user manager's environment can
>   carry one sign-in's id to apps of every sign-in. Where systemd does not run (elogind, a
>   container, the BSDs), `XDG_SESSION_ID` as before. *Builder's call:* "logind unavailable" is
>   decided by systemd's presence, not by a failed read: with systemd running and
>   `/proc/self/cgroup` unreadable the answer is "" (fail closed). Consequence for MA-1439: a copy
>   started by the user manager never shows the choice on a second click (the "cannot tell"
>   notice) even if its environment has an id; the lab still decides whether that is acceptable.
> - **C3 — `quit -end-older` ends only a live, proven MailArchive.** For a record with no hand-off
>   actions the verb now first checks that its lock is held (a record nobody's lock backs is
>   stale: "not running", exit 2, whatever flag), and, before ending or saying it would, reads
>   the process's program from the system (`procs.Program`: QueryFullProcessImageName on
>   Windows, `/proc/<pid>/exe` on Linux, `kern.procargs2` on macOS, "cannot tell" elsewhere) and
>   requires one of `schedule.KnownName`'s names; otherwise "could not ask", exit 1, with the
>   program named. *Builder's call:* the record's own `exe` is not trusted for this (a later
>   `run-job` of the same program could hold a reused pid; the lock check is what rules that out,
>   and the name check what rules out a foreign program).

### 12.2 New dependencies (operator decision)
`github.com/ebitengine/purego` (macOS only, behind a build tag) and `github.com/godbus/dbus/v5`
(Linux only, behind a build tag). Both pure Go; both go into THIRD-PARTY-NOTICES. (Q7.)

> **Folded 2026-11-13 [menubar 10-lens MB-19, MB-2] (lead's call).** purego's version is pinned,
> and the Go-bump checklist gains the menu-bar lab (as §8.2 says) and MB-19's whole-program lab
> (a backup and a `run-job` on arm64 and on amd64 under Rosetta). purego enters with **S1**, not
> S4: S1's AppKit event loop, reopen delegate and `applicationShouldTerminate:` (MB-2) are made
> through it. So Q7's answer is needed before S1's macOS code.

### 12.3 Labs (this repo has no lab tier; run locally per the tri-platform note)
- L-M1 (macOS): open the real 1.7.1 .app, choose Desktop; open the app again → record what
  happens (expected: nothing, one process). Repeat with a second copy of the .app at another path.
  Then the same with the S1 build → the choice appears.
- L-M2 (macOS): status item on light/dark/tinted menu bars, with a notch and a crowded bar,
  VoiceOver, Increase Contrast; a real screenshot (needs Screen Recording for the terminal, or by
  hand).
- L-W1 (Windows 11 ARM VM, `prlctl exec` cannot drive a signed-in desktop, so by hand): first-run
  overflow, pinning, Explorer restart, Narrator, high contrast, the choice in front of Edge, Startup
  launch with the tick off. *Added 2026-11-13 (F7):* install an MSI whose shortcuts are named
  "MailArchive Desktop", then upgrade to one naming them "MailArchive": no "MailArchive Desktop"
  shortcut is left on the all-users Desktop, in Startup or in the Start menu. *Added 2026-11-13
  (fix round 2):* Install-RMM.ps1's close step in a real user session — an idle copy, a busy one
  (exit 1618, nothing closed), two users or two copies (all or none), under AppLocker or WDAC
  Constrained Language mode, and a copy started "as administrator" (exit 3, never forced).
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

> **Folded 2026-11-13 [menubar 10-lens §5, MB-4, MB-12, MB-15, MB-16, MB-21] (lead's call).** Each
> recommendation above is now **the default that gets built, pending the owner's "ok to your
> picks"** (asked 2026-11-13; not yet answered). The review agreed with all nine, adding:
> - **Q3:** on Windows, the background start exits quietly for a user with no archives (MB-21; an
>   owner ruling, default as recommended).
> - **Q4:** ask only on the person's own Quit (the menu's Quit or Shut down); never on sign-out,
>   shutdown or an installer; the words say what does happen (MB-2).
> - **Q5:** counts only, extended to the choice window (MB-15) and to the whole Linux menu (MB-12).
> - **Q6:** S1 ships alone. The review asked for L-M1 to reproduce the report first; the lead's
>   2026-11-13 `open -a` run did (§2.4 MB-4 note). S1 carries MB-2's stop paths.
> - **Q7:** accept godbus; accept purego pinned, with the menu-bar lab and MB-19's whole-program
>   lab on the Go-bump checklist. Needed before S1's macOS code (§12.2 note).
> - **Q8:** no icon, plus the late-watcher re-check (MB-12) and MB-16's Cancel rule.

> **Owner ruling 2026-11-13 ("ok to your picks").** The owner accepted every recommendation
> above: Q1–Q9 are now rulings as recommended, with the review's additions. In words: the menu
> and the choice say "Open MailArchive Desktop" (Q1); the wizard reached from the choice drops
> only its Desktop item (Q2); start at sign-in on Mac and Linux is off until the person ticks it
> (Q3); Quit while busy asks, Keep running the default (Q4); no archive names in the tooltip
> (Q5); S1 ships first as its own small release (Q6); purego and godbus are accepted (Q7); plain
> GNOME gets no icon and the Help page explains (Q8); the icon shows whenever MailArchive runs
> (Q9). This also answers MB-21's Windows default (a quiet background start for a user with no
> archives, S6). Slice S1 is built on this ruling (branch `menubar`, 2026-11-13).

## 14. Not in this design
- Notifications (the existing failure/recovery notices and email alerts are unchanged).
- A native settings window: the configuration window stays MailArchive Desktop in the browser.
- Progress percentages in the menu (task kind and start time only; Desktop shows progress).
- Any change to archives, schedules, the engine, the reader, or the private-link session model.

> **Folded 2026-11-13 [menubar fold contradictions] (lead's call).** The four contradictions the
> 10-lens fold left open are settled:
> 1. **The Quit question names no archive**, like the choice window: "MailArchive is busy (a backup
>    is running). Quit anyway? The backup stops and is recorded as interrupted." Buttons: Keep
>    running (default), Quit.
> 2. **Shut down is a person's own stop**, so it asks the same question when work is running (MB-2).
>    MA-695's test is added to the list of tests S1 amends; its assertion that Shut down releases
>    the lock stays, now after the wait.
> 3. **A second sign-in to the same Mac or Linux account never ends silently (R50):** when the
>    running copy belongs to another session, it answers the hand-off with "open in another
>    session", and the launcher shows its own message in this session: "MailArchive is already
>    open in another sign-in of this account. Use it there, or quit it there to open it here." The
>    choice window never appears on the other screen for this click.
> 4. **purego comes with S1 on macOS**, so owner Q7 must be answered before S1 is built (asked
>    2026-11-13 as part of "ok to your picks"). S1's labs gain a Mac smoke test on the real app
>    bundle: Desktop running, open the app again, the choice window appears in front, no second
>    process, no −600.

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
