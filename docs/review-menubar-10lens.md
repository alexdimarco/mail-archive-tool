# Review: the MailArchive indicator, launcher choice and one running copy (design-menubar), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** The design's shape holds up. Making the existing Desktop process
the one long-lived MailArchive (the "instance") reuses the lock, key and hand-off that already
exist. Adding an action to the hand-off keeps both directions proven by the key. The no-cgo route
on all three platforms has a working macOS spike. Leaving scheduled backups icon-less is right.

Two findings are HIGH. Both were dissolved by a named change, so neither needs a re-design (§3):

- **MB-1.** The "needs attention" reads in §5.3(c) point at code that writes into every archive.
  `health.Gather` takes the archive lock with `Acquire`, which creates the lock file and rewrites
  its holder line, and it writes a 64 KiB probe file. The Your-archives row builder can rewrite
  the archives list and asks the OS scheduler about each row. Run once a minute, that breaks "no
  write" and churns cloud-synced folders.
- **MB-2.** Quitting does not do what its words promise. Desktop's shutdown today does not wait
  for a backup, check or rebuild running in its job slot. The wizard's export would run on
  `context.Background()`. On macOS, an AppKit app that is told to quit calls `exit()` unless it
  defers, which skips the Go shutdown entirely. So "stops at its next safe point" is not
  guaranteed on any path, and logout or an installer can stop the process mid-write.

24 conditions in all (MB-1 to MB-24). Fold each into `docs/design-menubar.md` beside the text it
changes, mark it "Folded from the 10-lens review [MB-n]", and re-verify it.

**Build may not start on slice S1 until MB-1 to MB-14 are folded.** MB-1, MB-5 and MB-6 may be
folded with S2, the slice they change, but before S2's code. The LOW conditions may be folded
with the slice they name. **L-M1 (MB-4) runs before any S1 code**: it needs no code, and its result
decides whether the macOS part of S1 is the fix the design says it is.

| Id | Sev | Lens | Section | In one line | Verdict |
|---|---|---|---|---|---|
| MB-1 | HIGH | 1, 3, 6, 9 | §5.3(c), §10.2 | The attention reads the design points at write into every archive (lock holder line, a 64 KiB probe file, the archives list) and query the OS scheduler per row | CONFIRMED; dissolved |
| MB-2 | HIGH | 7, 3, 10 | §10.4, §8.2, §8.1, S1 | No stop path waits for a running task. Desktop's drain skips the job slot, the wizard export is unbound, and AppKit's terminate calls `exit()`. Logout, an installer or Quit can stop mid-write | CONFIRMED; dissolved |
| MB-3 | MED | 8, 6 | §7.1 | Choosing request form by version string: dev builds can't be compared, and an old instance answers 403, which the launcher retries for 15 s, then says "not responding" | CONFIRMED |
| MB-4 | MED | 10, 8 | §2.4, §7.5, §9, Q6 | The "quits at once" fix is not proven. The spike bundle had `LSUIElement` and a real executable; the product has neither (no `LSUIElement`, a symlinked executable) | CONFIRMED |
| MB-5 | MED | 1, 5 | §5.1, §5.3(a), §10.4 | Tasks outside the job slot (workspace export, deletes, restore, wizard export) appear in neither the state nor the Quit question | CONFIRMED |
| MB-6 | MED | 1, 7, 2 | §5.3(b) | The lock probe can show false "busy": its own locks, short holds with no verb (status's own probe), and an open handle during an in-instance delete on Windows | CONFIRMED |
| MB-7 | MED | 5, 8 | §7.3, §11 | MA-614 and the RMM scripts recognise the dashboard by `desktop`; a `start` instance would not be stopped by an RMM upgrade, and the shortcut list is not per shortcut | CONFIRMED |
| MB-8 | MED | 5, 4 | §6, §7.3, §8.1 | New dialog entry points (choice via hand-off, menu, Quit question) sit outside dialogfront's Arm rule and its walk (MA-1111, MA-1117) | CONFIRMED |
| MB-9 | MED | 1, 7 | §2.2, §6.3, D1, D5, R50 | On macOS and Linux the lock is per account, not per sign-in. A no-icon or SSH-started instance cannot show the choice. A standalone `wizard` plus an instance means two wizards | CONFIRMED |
| MB-10 | MED | 3, 9 | §8.2 | An AppKit accessory app with no windows can be App-Napped, which today's Desktop is not. Weeks-long life needs autorelease discipline and a soak | PLAUSIBLE (lab) |
| MB-11 | MED | 8, 9, 10 | §10.5, §8.2 | `SMAppService` on an unsigned bundle is unverified; App Translocation is not handled. Go 1.26 stamps a macOS 12 minimum, so the 10.15 branches are dead | CONFIRMED (min-OS); PLAUSIBLE (SMAppService) |
| MB-12 | MED | 6, 7 | §8.3, §10.1, §10.2 | Linux: the menu's layout gives archive names to any session-bus client at all times; a tray watcher that appears late is never re-checked | CONFIRMED |
| MB-13 | MED | 5 | §11 | Existing rows that pin today's routing and shortcuts (MA-611, MA-614, MA-620, MA-1080, MA-1111/1117, X11, S59) are not named as amended | CONFIRMED |
| MB-14 | MED | 8 | §7.1, §7.5 | After S1, every in-place upgrade leaves the old instance answering clicks with its own choice and wizard until it is quit | CONFIRMED |
| MB-15 | LOW | 6, 4 | §6.1, Q5 | The choice window shows an archive name, which is as visible as the tooltip Q5 keeps names out of | CONFIRMED |
| MB-16 | LOW | 4 | §6.1 | Cancel where no icon can show leaves an invisible background server (X11) | CONFIRMED |
| MB-17 | LOW | 6 | §10.1, §7.3 | Same-session entry points: forged menu command messages; foreground right given to a pid named only in the file; no rate limit on reopen, Activate or tray clicks | CONFIRMED |
| MB-18 | LOW | 10, 5 | §11, §12 | Which new rows gate CI is unstated; core rows should use real processes, a real lock and a real session bus | CONFIRMED |
| MB-19 | LOW | 9 | §8.2, §9 | purego's runtime shim is linked into every macOS verb; only arm64 and a toy program were measured | CONFIRMED |
| MB-20 | LOW | 9 | OMISSION | Operability: nothing tells a person, or `status`, whether the instance runs and why no icon shows | CONFIRMED |
| MB-21 | LOW | 4 | §10.5, Q3 | Windows' all-users Startup now shows an icon to every user who signs in, including ones with no archive | CONFIRMED |
| MB-22 | LOW | 4 | OMISSION | The delete and unlist refusals say "click Shut down at the top of MailArchive Desktop"; they will be met far more often and should name Quit | CONFIRMED |
| MB-23 | LOW | 1 | §11 R50, R52 | R50's wording ("at most one GUI process") is broken by launchers, old versions and the standalone wizard | CONFIRMED |
| MB-24 | LOW | 10 | §7.2, §15, header | Citation slips: `resolveGUIFormerName` takes no `inBundle`; the no-argument row is `defaultVerb` M1; review file name | CONFIRMED |

## 1. Provenance

- Date worked on: 13 November 2026 (the session's date).
- Design reviewed: `docs/design-menubar.md` at commit 74a6633 (branch `menubar`), sha256
  `a76a07a8aa4a0b937871256c02a54df9d8a2f25da38284674a476c2030698751`. Code read at the same commit.
- File name: the caller asked for `docs/review-menubar-10lens.md`. The kit's convention is
  `docs/review-<design>-predesign.md` (`design-review.md:7`). MB-24 asks the design's status line
  to cite this file.
- Procedure: `../assurance-kit/process/design-review.md`. Read-only: no product code was built or
  run, and no test was run (another agent was building; the caller asked for light CPU use). The
  only commands were reads, `git grep`, `shasum`, and one read of the Go 1.26 linker source.
- **Deviation, stated:** the kit asks for independent lens reviewers, a separate skeptic and a
  separate rescue agent. One reviewer ran all three roles here, in sequence. The skeptic's
  refutations (§4) and the rescue (§3) are recorded, but they are not independent of the finder,
  which weakens both. An independent skeptic over MB-1 to MB-14 would be the cheapest way to
  strengthen this review.
- Read: the design in full; the spike record and `docs/lab/menubar-spike/main.go` and both plists;
  repo `CLAUDE.md`; `docs/ux-contract.md`; the catalog rows R23, R33, R39, S59, MA-610 to MA-628;
  `internal/lockfile/{lockfile,lock_unix,lock_windows}.go`; `internal/instance/handoff.go`,
  `holder.go` (signatures), `owner_unix.go`; `internal/desktop/{launch,lifecycle,jobcontrol,maintenance,archives}.go`,
  parts of `dashboard.go`, `exports.go`, `archivedelete.go`; `internal/health/health.go`
  (`Gather`, `probeWrite`); `internal/wizard/wizard.go` (`Run`, first list, export context);
  `internal/dialogfront/dialogfront.go`; `cmd/mailarchive/{noarg,noarg_darwin,dispatch}.go`
  (routing); `cmd/mailarchive/archivedelete.go` (refusal words);
  `internal/schedule/program.go` (translocation rule); `packaging/macos/{build-app.sh,Info.plist.in}`;
  `packaging/mailarchive-desktop/Product.wxs` (shortcuts, CloseApplication); Go 1.26.0
  `src/cmd/link/internal/ld/macho.go:440`.
- Not read: the RMM PowerShell scripts themselves (their rules were read from MA-614's text); the
  zenity library; Apple, Microsoft and freedesktop documentation. Every API behaviour below that
  comes from memory is labelled "from memory".

## 2. Findings and conditions

### MB-1 (HIGH) — the attention reads must be read-only and bounded. Amends §5.3(c), §10.2, R51.

§5.3(c) says the attention facts come from "the same reads the Desktop's 'Your archives' rows and
`status` use (`ArchiveRow` …; the run-history's last outcome)", once a minute, and lists "an index
behind" among them. The code behind those reads writes:

- `health.Gather` probes the lock with `lockfile.Acquire` (`internal/health/health.go:1936`), and
  `AcquireWait` opens with `O_CREATE`, truncates and writes a new holder line (`lockfile.go:59`,
  `:88-89`). It also calls `probeWrite` (`health.go:1064`), which creates, fills (64 KiB, synced)
  and removes a temporary file in the archive folder. "Index behind" comes from `Gather` opening
  `search.db`.
- `Config.archiveRows` (`internal/desktop/archives.go:168`) runs per row: `archindex.Kind` (a
  streaming walk of the manifest); `deleteRowFacts` with `archdelete.HostDiscovery` (the OS
  scheduler is asked about the row's schedules); encrypted-marker reads. Its caller
  `reconcileArchives` can rewrite the archives list (`archives.go:152`).

Run every minute for every archive, from sign-in, this would:
- rewrite every archive's lock file;
- write and delete a 64 KiB file in every archive folder (a Nextcloud or OneDrive folder uploads
  each change; a sleeping drive is woken);
- run scheduler queries once a minute.

All of that contradicts §5.3's "Never read … mail files, manifests" and the review question "no
write". A row builder that the next developer "reuses" because the design names it is the likely
path.

**Condition.**
1. §5.3(c) names a new, narrow reader (for example `tray.Attention(path)`). It reads only named
   small records: the source-loss streak, the alert streak, the auto-pause/seal state, the
   unfinished-delete marker, and the last run's outcome. Each read is bounded, regular file only,
   never through a link, as `ReadSecureFile` and `state.Read*` already do.
2. It never calls `health.Gather`, `archiveRows`, `reconcileArchives` or anything that asks the
   scheduler, opens the index or takes a lock.
3. "An index behind" leaves the indicator unless it can be read from an existing record without
   opening the index.
4. The same skip rules as (b) apply: drive absent, cloud-synced cadence, 2-second budget,
   back-off.
5. X10 ("one status truth"): the Your-archives row and the indicator call **one** shared
   classifier over these facts. A row tests that the two agree on a fixture set.
6. A new MA row proves "no write" with real files. Before and after a full pass over tempdir
   archives (one with no lock file, one read-only, one encrypted), the tree is unchanged in names,
   sizes, modification times and bytes, and no file is created.

### MB-2 (HIGH) — every stop goes through the lifecycle and waits; system-initiated stops never ask. Amends §10.4, §8.1, §8.2, S1.

What the code does today:
- `newDashboard` registers drains for pending secrets, deletes under way, the Thunderbird restore,
  and the workspace export (`dashboard.go:303-317`, `exports.go:912`). **Nothing waits for the job
  slot**: Run now, Check integrity, Rebuild, the encryption copy. `runJobOnClaiming` starts a
  goroutine and returns (`maintenance.go:70-95`).
- `serveInstance` returns after the drain (`launch.go:538-541`), and the process exits.
- A cancellable job's context is cancelled by `Stop` (`jobcontrol.go:50`). The non-cancellable
  ones (check, rebuild) are not cancelled at all; they are cut off by the exit.
- The wizard's export inside the instance would run on `context.Background()` (`wizard.go:1892`).

So "If you quit now, the backup stops at its next safe point" (§10.4) is not what happens. The
archive is left as a crash leaves it. The next run's intent recovers it, so no mail is lost, but
R52's words and the dialog's words promise more.

The design also adds termination paths it does not route:
- **macOS.** Once the process is an `NSApplication`, logout and installers quit it through an
  Apple Event. AppKit's default terminate path ends with `exit()` (from memory) after
  `applicationWillTerminate:`. That skips the Go drain unless the delegate answers
  `applicationShouldTerminate:` with `NSTerminateLater` and replies only after the drain. §9's
  note covers only the menu's own Quit.
- **Windows.** A hidden top-level window now receives `WM_QUERYENDSESSION` and `WM_ENDSESSION`
  at logoff, and Restart Manager's close request during an MSI upgrade (`ENDSESSION_CLOSEAPP`,
  from memory). After `WM_ENDSESSION` returns, the process is ended.

**Condition.**
1. One stop function. Menu Quit, Shut down, SIGTERM or Ctrl-C, the macOS terminate request, and
   Windows end-session all call `Lifecycle.Stop` and wait for `Drained` within the budget.
   - macOS: `applicationShouldTerminate:` returns `NSTerminateLater`, then calls
     `replyToApplicationShouldTerminate:YES` from the main thread when drained or out of budget.
   - Windows: answer `WM_QUERYENDSESSION` (with `ShutdownBlockReasonCreate` while a task runs),
     drain in `WM_ENDSESSION` within about 5 s, then let go.
2. **System-initiated stops never show the Quit question.** Only the menu's Quit and the Shut
   down button ask.
3. Register a drain that cancels and waits for the job slot (cancelling cancellable jobs; for
   check and rebuild, state what is left) and for the in-instance wizard export (bind it to the
   lifecycle context).
4. Make the Quit dialog's words true for each task kind, or change them to what does happen
   ("…stops now; the next backup finishes what it left").
5. Tests with real primitives: a real `Lifecycle`, a job held in the slot by fault injection, and
   an assertion that stop waits for it and that the end-session path shows no dialog. The macOS
   delegate path is a smoke row plus lab.
6. Because S1 already brings the AppKit loop in, these land in S1, not S3.

### MB-3 (MED) — choose the request form by capability, not version. Amends §7.1.

§7.1 has the launcher read `Version` and send the old form to "an instance older than the one that
knows actions". The current comparison (`otherVersion`, `launch.go:126`) only tells equal from
different, and treats `dev` as incomparable.

If a new request reaches an old instance:
- it fails its key check (403);
- `Call` returns `ErrBadProof`;
- `Reach` retries for the whole 15-second window (`handoff.go:215-234`);
- the launch then shows the wedged refusal, "already running … but is not responding"
  (`launch.go:110`), which is wrong.

**Condition.**
1. The instance file gains an `acts` list. An absent field means an older instance, which gets
   the old `open` request. Nothing parses versions.
2. A 403 to a new request from an instance that does advertise actions falls back once to the old
   `open` request; it is not retried as "not responding".
3. The server refuses an unknown `act` (400); it never defaults to `open`.
4. The immediate `{"shown":…}` reply for `choose` and `wizard` is MAC-covered, for example
   `"handoff-shown2|N|act|shown|reason"`, where `reason` covers "already open" and "can't show
   here" (MB-9).
5. On Linux, the activation token (§7.4) is inside the request MAC, bounded (for example ≤ 256
   printable ASCII characters), and passed to the dialog tool only as an environment value.

### MB-4 (MED) — the 2026-11-12 fix is a likely fix until L-M1, and the spike's bundle is not the product's. Amends §2.4, §7.5, §9, Q6.

The proof that "reopen" reaches a pure-Go process came from `Spike.app`, whose `Spike-Info.plist`
sets `LSUIElement` true and whose `CFBundleExecutable` is a real file. The product's
`Info.plist.in` has no `LSUIElement`, and its `CFBundleExecutable` is the `mailarchive-gui`
**symlink** (`build-app.sh:41`). Neither difference is known to matter (from memory, the reopen
event should still be delivered), but neither is proven. Also, §2.4 says "likely, not proven",
while §7.5 and Q6 call S1 "the macOS 'quits at once' fix" with no condition.

**Condition.**
1. Run L-M1 against the released 1.7.1 `.app` **before S1's code**. It needs no code. If it does
   not reproduce, the macOS half of S1 loses its stated reason, and Q6's ordering is re-asked.
2. L-M1 also runs a spike built with the product's bundle shape (symlinked executable, with and
   without `LSUIElement`). Alternatively, add `LSUIElement` to `Info.plist.in` (which also stops a
   Dock icon flashing before the accessory policy is set) and amend any row that pins the plist.
3. §7.5 and Q6 say "likely fixes, pending L-M1".

### MB-5 (MED) — name every task the instance can run. Amends §5.1, §5.3(a), §10.4.

The `kind` field goes on the capture job slot only. The instance also runs:
- the Extraction workspace export, in its own slot (`d.ex.slot`, `exports.go:919`);
- a delete under way (`d.running`);
- the restore to Thunderbird (`d.rtb`, which does use the job slot as well);
- the in-instance wizard export (§6.3);
- the encryption copy (`copy`, listed in §5.3(a)), which has no row in §5.1's state table.

**Condition.**
1. §5.1 has one row per task kind: backup, check, rebuild, restore, copy, delete, workspace export,
   wizard export.
2. The state model and the Quit question read every holder, not only the job slot.
3. MA-1410's walk covers every place that starts one of them, not only `runJobOnClaiming`'s
   callers.

### MB-6 (MED) — the lock probe must not show false "busy". Amends §5.3(b).

`lockfile.Probe` writes nothing and never creates the file (`lockfile.go:189-223`; refutation R-1
below). Contention with a starting backup is covered by the 100 ms acquire grace
(`lockfile.go:152-161`). But the state the probe feeds can still be wrong:

- **The instance's own locks.** flock is per open file (`lock_unix.go:13-15`), so the instance
  probing an archive it is itself backing up reads "held". Without de-duplication that is "backing
  up 2 archives", and the attribution is wrong.
- **Short holders with no verb.** `health.Gather` (the `status` verb, and Desktop's health view,
  `maintenance.go:259`), `keyring/forget.go:186`, `vault/store.go:452` and `decryptcopy`'s copy
  lock all hold the lock briefly with an empty verb. §5.1 maps an empty verb to "Working on an
  archive", so these flash the icon.
- **Windows deletes.** A probe has the lock file open for a moment. A delete in the instance that
  removes that file or its folder at that moment can fail with a sharing violation (from memory:
  Go opens files on Windows without `FILE_SHARE_DELETE`). A multi-step delete then stops part way.
- **Write-mode opens.** `Probe` opens read-write first. On cloud placeholders a write-mode open can
  have side effects that a read-only open does not; a read-only handle is enough to take flock or
  `LockFileEx`.
- **Blocking checks.** "Skipped without opening anything" still needs a `stat`, and a `stat` on a
  sleeping network volume can block. It needs the same budget.

**Condition.**
1. The instance's own tasks win; probes skip archives the instance holds or is deleting or moving.
2. A lock-only task is shown when it is held on two probes in a row (at least one interval
   apart), or when the holder line's start time is more than a few seconds old.
3. The probe uses a read-only handle.
4. The existence check runs inside the 2-second budget.
5. "From another computer" is labelled best effort (the holder line is untrusted, and network-file
   locking varies).
6. New U rows cover the de-duplication, the debounce, and a delete that meets a concurrent probe
   (real files, fault injection for timing).

### MB-7 (MED) — the Windows installer and RMM scripts. Amends §7.3, §11, S1.

MA-614 pins:
- "Product.wxs's shortcuts target `mailarchivew.exe` with the three `desktop` arguments";
- the `WizardShortcut`'s `wizard` argument;
- a single `util:CloseApplication` target, `mailarchive-desktop.exe` (`Product.wxs:52-58`);
- Install-RMM.ps1 "stops a `mailarchivew` process only when its CommandLine is a dashboard (no
  argument … or a first argument `desktop` without `-capture`)".

An instance started by `start` or `start -background` matches none of these. An RMM upgrade would
leave it running with its files in use, in every session of a shared machine. §7.3 says "Start
menu and desktop shortcuts run `start`" but does not say what happens to the "MailArchive wizard"
and "copy link" shortcuts.

**Condition.**
1. §7.3 gives one row per shortcut (Desktop, Start menu, Wizard, Copy link, Startup) with its new
   arguments.
2. Install-RMM.ps1, Uninstall-RMM.ps1 and Cleanup-CurrentUser.ps1 learn `start` (and never stop
   `run-job` or `-capture`).
3. §7.3 states how an MSI upgrade meets a running instance in every session (Restart Manager's
   close request goes to MB-2's end-session path).
4. MA-614 is amended in S0 (MB-13).

### MB-8 (MED) — Windows first: the new dialogs must follow R39's Arm rule. Amends §6, §7.3, §8.1.

`dialogfront` lets a dialog take the keyboard only when the person just asked (Arm) or one of our
windows is active, and a walk test (MA-1111, MA-1117) pins that dialogs are reached only through
`desktop.Launch` and `wizard.Run`, and inside Desktop only after an Arm (`dialogfront.go:39-41`).
The design adds four new entry points: the choice shown on a hand-off, the menu's "Run the
wizard…", the Quit question, and the choice shown on reopen.

`AllowSetForegroundWindow` (§7.3) gives the right to come forward, but dialogfront still decides.
Two more interactions:
- The tray's `SetForegroundWindow`-on-the-hidden-window quirk makes "the active window is one of
  ours" true after the menu closes, so a later dialog the person did not ask for (for example a
  task's end) could take the keyboard.
- The hidden window must never count as a dialog (`isDialogWindow` requires a caption).

**Condition.**
1. Each new entry point calls `Arm` at the person's request. A hand-off counts only after its proof
   is verified.
2. The hidden window is created without `WS_CAPTION` and never shown.
3. The after-menu foreground is excluded from "ours" for dialogs that were not armed.
4. MA-1111 and MA-1117's walk is widened to the new entry points (amended, not a parallel test).
5. L-W1 checks the choice in front of Edge through both the hand-off and the menu.

### MB-9 (MED) — one instance per what, and what a no-display instance does. Amends §2.2, §4, §6.3, D1, D5, R50.

`LogonSession()` returns `""` on POSIX (`owner_unix.go:27`), so on macOS and Linux the instance is
one per **account**, not per sign-in session as D1 says. Three consequences:

1. **A second graphical session of the same account** (Linux: a local session plus xrdp) hands off
   to the first session's display. The choice and the browser appear where the person is not.
2. **An instance that started with no display** (§4's automatic `-no-tray` fallback over SSH)
   holds the lock. A later click hands off `choose` to a process that cannot show a dialog, so the
   launch ends silently, against R50 and X11.
3. **D5 is per process.** The `wizard` verb with no instance runs standalone (§6.3), and that
   includes the existing "MailArchive wizard" Start-menu shortcut. If the person then clicks the
   app, a new instance starts, and its in-process mutex cannot see the standalone wizard: two
   wizards.

**Condition.**
1. R50 and D1 say "per account (per logon session on Windows)".
2. An instance that cannot show dialogs answers `choose` and `wizard` with "can't show here"
   (MB-3), and the launcher then does what `desktop` does today: opens the browser or shows the
   link dialog.
3. "One wizard" holds across processes. Either the `wizard` verb in a graphical session becomes
   the instance, or both share a per-account wizard lock file (the existing lockfile pattern).
4. A row covers it.

### MB-10 (MED) — App Nap and a weeks-long life. Amends §8.2, labs.

From memory: macOS can App Nap an app that has no visible windows, which coalesces its timers and
throttles its I/O. Today's Desktop never checks in with AppKit. The instance will.

It also becomes always-on (at sign-in on Windows; opt-in elsewhere), so leaks that did not matter
for a Desktop opened for minutes now matter. Autoreleased objects from Objective-C calls made
outside the run loop's pool leak; a tray state change every 5 s on a goroutine adds up.

**Condition.**
1. While any task runs or a Desktop session is open, the instance holds an `NSProcessInfo`
   activity (user-initiated, allowing idle system sleep), and lets go after.
2. Every Objective-C call that creates objects runs inside the run loop or an explicit
   autorelease pool. MA-1426's main-thread assert extends to "inside a pool".
3. L-M2 measures a Run now with the browser in front, with and without the activity.
4. Before S3, S4 or S5 ships, a 24-hour soak on each platform (memory, goroutines, handles flat)
   is recorded.

### MB-11 (MED) — start at sign-in on macOS. Amends §10.5, §8.2.

- `SMAppService.mainApp` is cited from memory (§15). Whether it registers and stays registered for
  an **unsigned**, linker-ad-hoc-signed bundle whose code identity changes every release is not
  shown. From memory: Background Task Management and privacy permissions key on code identity, so
  each upgrade may re-prompt or leave an orphaned entry.
- A bundle run from Gatekeeper's App Translocation (a random read-only path) would register that
  path. The repo already refuses that for schedules (`internal/schedule/program.go:227`, MA-615).
- **Proven:** Go 1.26.0's linker stamps a macOS **12.0** minimum
  (`src/cmd/link/internal/ld/macho.go:440`). The product cannot run on 10.15 or 11, so §8.2's
  "On macOS 10.15 … a bundled template PNG" and §10.5's "10.15–12" branch for 10.15 and 11 are
  dead code. The bundle's `LSMinimumSystemVersion` of 10.15 is already untrue; that is a separate
  fix for the operator.

**Condition.**
1. One mechanism on every supported macOS: a per-user LaunchAgent that runs `/usr/bin/open -a
   <bundle path>` (open is already on MA-612's tool list), so Launch Services owns the process and
   reopen routing works. `SMAppService` waits until a lab shows it holding across two unsigned
   upgrades.
2. Turning it on refuses while translocated, reusing `ProgramForSchedule`'s rule and words.
3. The 10.15 branches are removed; §8.2 and §10.5 say "macOS 12+ (Go 1.26's minimum)".
4. A lab row on two upgrades in a row.

### MB-12 (MED) — Linux bus exposure and a late tray. Amends §8.3, §10.1, §10.2.

1. **Names on the bus.** `com.canonical.dbusmenu`'s `GetLayout` can be called by any session-bus
   client at any time, not only while the menu is open. Its layout would carry the archive names
   from §5.2, so §10.1's "a sandboxed app with session-bus access learns 'backing up 1 archive' at
   most" is untrue as written. Either fill the archive lines only on `AboutToShow` and clear them
   when the menu closes, or keep names out of the Linux menu. §10.1 then says which.
2. **A late tray.** At sign-in the autostart entry can run before the panel's
   `org.kde.StatusNotifierWatcher` exists, and a panel restart replaces it. §8.3 checks once and
   falls back for good. Watch `NameOwnerChanged` for the watcher and register again: the Linux
   twin of `TaskbarCreated`.
3. `Activate` and menu events are rate-limited like the hand-off (MB-17).

### MB-13 (MED) — name the existing rows this design amends. Amends §11, S0.

These rows pin what the design changes:
- MA-611: `mailarchive-gui` with no `-job` reaches `wizard`;
- MA-620 and `defaultVerb`'s M1 row (`noarg.go:62-64`);
- MA-614: shortcuts, CloseApplication, RMM scripts;
- MA-1080: the Linux desktop entry;
- MA-1111 and MA-1117: dialogfront's walk;
- X11: "a shortcut click surfaces this account's dashboard … never a silent, invisible background
  server";
- S59's no-argument clause.

Under the repo's rule, these are amended in the open, never edited to pass.

**Condition.** §11 lists each row with its old and new text. S0 lands each amendment as "amended,
not weakened" with the operator's acceptance before S1's code.

### MB-14 (MED) — the old version keeps answering after every upgrade. Amends §7.1, §7.5.

Once S1 ships, an upgrade in place (dragging a new `.app` over the old one, or an MSI whose
end-session close was refused) leaves the running instance answering reopen and hand-offs with
**its own** version's choice and wizard until it is quit. §7.5 covers only the first upgrade.

The instance may not restart itself (R33; MA-612 forbids passing the program's path to `open`).

**Condition.**
1. The hand-off carries the launcher's version. On reopen, the macOS instance compares its own
   version with the bundle's version on disk.
2. When newer, the choice window says one line, "A newer MailArchive is installed. Quit and open
   MailArchive again to use it.", with a Quit button. There is no self-restart.
3. A row covers it.

### MB-15 to MB-24 (LOW)

- **MB-15 (§6.1, Q5).** The choice window's "Now: backing up "Work mail"" puts a name in a dialog
  as visible as the tooltip. Use the count form there too.
- **MB-16 (§6.1).** Cancelling the choice keeps the instance running "and the icon is there to say
  so". Where no icon can show (GNOME without the extension, the `-no-tray` fallback), a Cancel on
  a launch that started the instance, with nothing running, stops it. Otherwise it is the silent
  background server X11 rules out.
- **MB-17 (§10.1, §7.3).**
  - Windows: take menu commands from `TrackPopupMenu` with `TPM_RETURNCMD` and ignore `WM_COMMAND`
    (otherwise any same-session process can post "Quit").
  - Call `AllowSetForegroundWindow` only for the pid the lock's holder record confirms (the
    `HolderPID` check in `launch.go:299`).
  - Limit reopen, D-Bus `Activate` and tray-click "open Desktop" to one a second, as the hand-off
    is.
- **MB-18 (§11, §12).**
  - Say which new rows run in the gating CI. The project memory notes the test gate is Ubuntu-only
    (not verified in this review). Windows and macOS rows are then lab or non-gating.
  - Use real primitives for core logic: MA-1403 with two real processes of the built program and a
    real lock; MA-1428 on a real private `dbus-daemon` session bus rather than an in-process fake.
  - AppKit and Win32 fakes are acceptable as operating-system seams, and should be named as such.
- **MB-19 (§8.2, §9).** purego's no-cgo runtime shim is linked into the whole macOS program, so
  every verb runs on it, not just the indicator. L-M1 runs a backup and a `run-job` with the
  purego build on both macOS slices: arm64, and amd64 under Rosetta. The "+0.8 MB" is arm64 only.
- **MB-20 (omission; operability).** The instance file records the icon state and reason ("shown",
  "no tray on this desktop", "turned off"). `status` and Desktop's header say whether MailArchive
  is running, its version, and why no icon shows. Name where a windows-GUI instance's log lines
  go.
- **MB-21 (§10.5, Q3).** The MSI's all-users Startup entry now puts an icon in front of every user
  who signs in, including those with no archive (before, an invisible Desktop ran for them).
  Recommend: the background start exits quietly for a user whose archives list is empty. This is
  an owner ruling.
- **MB-22 (omission).** `desktopRunningText` (`cmd/mailarchive/archivedelete.go:93`) says "click
  Shut down at the top of MailArchive Desktop". With the instance running from sign-in this
  refusal becomes common. Add "or Quit MailArchive from its icon".
- **MB-23 (§11).** Sharpen the invariants:
  - R50: "at most one instance (the holder of the account's Desktop lock) per account, per logon
    session on Windows; every GUI launch becomes it or reaches it and exits".
  - R52: "every stop" (MB-2), not only "quitting".
- **MB-24 (citations).**
  - `resolveGUIFormerName` takes only `args` (`dispatch.go:108`); the `inBundle` input belongs to
    `defaultVerb` (`noarg.go:50`). Both the `mailarchive-gui` row and the M1 row change.
  - The design's status line should cite this review file.

## 3. Rescue pass (one reviewer; see the deviation in §1)

| Blocker | Escape tried | Outcome |
|---|---|---|
| MB-1 | Drop the attention line entirely; or read only named records | **DISSOLVED-BY** the narrow reader in MB-1. Removing the feature is unnecessary: every fact the menu needs except "index behind" lives in a small record that can be read without the lock. While doing this, the rescue found that "index behind" cannot be told without opening the index, so it leaves the indicator. |
| MB-2 | Keep quit-as-is and change the words; or drain properly | **DISSOLVED-BY** MB-2's single stop path plus a job-slot drain. While doing this, the rescue found the true root: **today's Shut down button already does not wait for the job slot**, so the gap is not new. The design inherits it and makes it more common (sign-in instance, logout, installers), and its Quit words would state it as solved. That is why it is HIGH and why it lands in S1. |

No blocker survives, so the verdict is GO WITH CONDITIONS, not NO GO.

## 4. Skeptic pass: claims checked and refuted

| Id | Claimed failure | Refuting evidence |
|---|---|---|
| R-1 | The lock probe writes into archives | `Probe` never creates or writes: "Like Held it never creates the file or writes to it" (`lockfile.go:189-196`). Only the attention path (MB-1) writes. |
| R-2 | Probing its own lock releases the instance's own hold (the fcntl trap) | flock, not fcntl: "flock is per open file description" (`lock_unix.go:13-15`); Windows locks per handle. |
| R-3 | A probe makes a scheduled backup refuse | Acquire retries for `acquireGrace` (100 ms) against a 20 ms probe (`lockfile.go:152-161`). `HoldFile` (no grace) is not used on the archive lock. |
| R-4 | A squatter can turn `choose` into another action | §7.1: the MAC covers `act` in both directions, with a new label so an old instance refuses. |
| R-5 | The design adds a listening socket | §10.1: "No new listening socket." |
| R-6 | Quit stops scheduled backups | §10.4: "A run of *another* process … is not stopped by Quit." |
| R-7 | The tooltip leaks archive names | Q5 and §10.2 keep names out of the tooltip (but see MB-15 for the choice window and MB-12 for Linux). |
| R-8 | AppKit is touched off the main thread | §8.2's threading rule and MA-1426's assert. |

## 5. The owner's questions: recommendations

- **Q1 (words).** Agree: "Open MailArchive Desktop".
- **Q2 (the wizard without the Desktop item).** Agree.
- **Q3 (sign-in on Mac and Linux).** Agree: off by default. On Windows, see MB-21 for a quiet exit
  when the user has no archives.
- **Q4 (Quit while a task runs).** Agree: ask, with "Keep running" as the default. Never ask on a
  logout, shutdown or installer stop (MB-2), and fix the words to match what happens.
- **Q5 (names in the tooltip).** Agree: counts only. Extend the rule to the choice window (MB-15)
  and the Linux bus (MB-12).
- **Q6 (release order).** Agree: S1 ships alone, **but only after L-M1 reproduces the report**
  (MB-4). S1 must carry MB-2's stop paths, because it brings the AppKit loop in.
- **Q7 (two new libraries).** Accept godbus. Accept purego with conditions: pin its version, add
  the menu-bar lab to the Go-bump checklist (as §8.2 says), and run MB-19's whole-program lab.
- **Q8 (Linux desktops with no tray).** Agree: no icon. Add the late-watcher re-check (MB-12) and
  MB-16's Cancel rule.
- **Q9 (icon when idle).** Agree: always shown while MailArchive runs.

## 6. What this review did not do

- No independent finder, skeptic or rescue agents (§1).
- No code was run. The App Nap, `SMAppService`, Windows end-session, sharing-violation and
  dbusmenu points are from memory and are marked so. Each has a lab or test named.
- The RMM scripts were judged from MA-614's description of them, not read.
