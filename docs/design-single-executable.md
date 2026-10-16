# Design — one program: `mailarchive` as the single executable behind the command line, the wizard and MailArchive Desktop

**Revision:** 1 (2026-10-15). **BUILD STATUS:** proposed, not reviewed, not built. The 10-lens
pre-code review is owed before any slice starts and will be filed as
`docs/review-single-executable-predesign.md`; this document's status line is updated when it is.
Nothing here is lab-validated: every Windows, macOS and Linux behaviour this text states from
memory of vendor documentation, and not from this repository, carries the words "lab item" and an
id (L-W*, L-M*, L-L*, L-C*), and is verified on a real machine before the slice that depends on
it ships (§9.3).

Base: branch `fix/review-2026-10` at `b1a16bc`. This document changes no code, no test and no
catalog row. Every id it proposes (R33, S59, X17, MA-580..MA-609) is provisional until the
OPERATOR accepts it into `docs/scenario-catalog.md` and `docs/ux-contract.md` with the slice's
catalog-first PR (`docs/owner-rulings-2026-10-09.md` §8). **Id allocation:** the rulings log names
MA-580 as the next free overflow id after the Thunderbird pre-sync design's provisional block
(MA-549..MA-579, which itself collides with MA-549, now in the catalog for the in-app Microsoft 365
configuration, and must shift at its TP0). R32, S58 and X16 are the highest ids any design has
claimed; **re-check R33, S59 and X17 against the operator's log at SE0.**

**Owner request (2026-10-15), verbatim:** "Review the code in this repository and recommend ways
to integrate wizard and the management interface into a single executable that can provide all
the diverse functions", then "lets do the design and review".

Gates before code:
- **SG1** the pre-code design review (`docs/review-single-executable-predesign.md`), lenses per
  §14, with the rescue pass. **Not run.**
- **SG2** owner answers to the questions in §13 that block a slice (each has a recommended
  default and a safe interim, so no slice is held by an unanswered question).
- **SG3** lab items: L-W1..L-W6 (Windows) before SE1 ships for Windows; L-M1..L-M3 (macOS) before
  SE1 ships the `.app`; L-L1..L-L2 (Linux) before the README's Linux text changes.
- **SG4** this design touches two other unbuilt designs only at named seams: the Thunderbird
  pre-sync design's job-file field (§4.2.5 there) rides along unchanged in `internal/job`; the
  extraction workspace's picker seam (`internal/desktop/picker.go`) gains a file-picker variant
  in SE3 (§4.8.3). The desktop design (`design-mailarchive-desktop.md`) P1, "one binary,
  in-process", is what this design completes: it never asked for three binaries, it asked for
  no child process.

**Claim labels used throughout.** **proven** = a tier-U/S row in §10 asserts it on every commit
once its slice is built (nothing is built, so read "proven" as "will be proven by MA-5xx/6xx").
**conditional** = holds only under the condition written beside the claim. **aspirational** =
intended, not mechanically assured. **lab item** = depends on how a real OS, installer or desktop
behaves, and is not assumed.

**Vocabulary.** *The program*: the one executable, `mailarchive` (`mailarchive.exe` on Windows).
*Surface*: one of the three ways the owner meets the program today: the **command line** (the
verbs of `cmd/mailarchive`), the **wizard** (today `mailarchive-gui`: native dialogs, local mail
sources) and **MailArchive Desktop** (today `mailarchive-desktop`: the loopback dashboard with the
embedded reader and the Extraction workspace). *Verb*: the first argument that selects a surface
or an action (`serve`, `status`, and new: `desktop`, `wizard`, `run-job`). *The pair*: on Windows,
the two files built from the same package, `mailarchive.exe` (console subsystem) and
`mailarchivew.exe` (GUI subsystem), identical in code. *Former name*: `mailarchive-gui` and
`mailarchive-desktop`, the file names that stop shipping. *Headless run*: a run a scheduler
starts, with no dialog, no console and no browser.

## 1. Problem

1. **Three executables, one module, one engine.** `cmd/mailarchive`, `cmd/mailarchive-gui` and
   `cmd/mailarchive-desktop` all call the same `internal/` packages (`app`, `export`, `health`,
   `schedule`, `state`, `index`, `server`, `desktop`). Each is 23 to 31 MB; each Windows release
   carries all three, and the MSI payload is three copies of one engine (`packaging/build-release.sh`,
   `packaging/mailarchive-desktop/Product.wxs`).
2. **The surfaces cannot reach each other's functions.** The dashboard can read, search, check and
   export an archive made from local mail files, but it cannot add mail to it: for a Thunderbird
   archive it renders steps that send the owner to the wizard or the command line (MA-546,
   `internal/desktop/localarchive.go`, `AddMailArgv`). The wizard cannot capture Microsoft 365.
   The command line has every function, and no owner of the double-click kind ever meets it.
3. **Three scheduled-run shapes, three repair paths.** Every surface records *its own file* as the
   program a schedule runs: the command line records `<exe> <verb flags> -log FILE`
   (`runSchedule` in `cmd/mailarchive/main.go`), the wizard `<exe> -job FILE` (`offerSchedule`,
   `repairSpec`), the desktop `<exe> -capture -config … -out … -log …` (`backupSpecFor` in
   `internal/desktop/schedule.go`). `health.exeIdentity` compares the recorded file with the
   running one, so the dashboard's `status` view of a wizard-made schedule reads
   `exe_moved` ("not this binary") on a machine where nothing moved.
4. **The wizard is the one surface whose logic lives in `cmd/`.** About 1,500 lines of
   `cmd/mailarchive-gui/main.go`, 83 direct dialog calls, 21 tests of its pure decision functions.
   The desktop's launcher is already injectable (`launchEnv` in `cmd/mailarchive-desktop/launch.go`)
   and its substance is in `internal/desktop`.
5. **Three per-user state roots.** Job files under `os.UserConfigDir()/mailarchive` (`job.Dir`);
   the instance lock under `%LOCALAPPDATA%\MailArchiveDesktop` on Windows and the config
   directory elsewhere (`instance.Dir`); the workspace's run records under
   `%LOCALAPPDATA%\MailArchive` (`desktop.StateDir`). Already OPEN-X1 in the pre-sync review.
6. **The one real constraint is the Windows subsystem.** A Windows executable is a console
   program or a GUI program. The wizard and the desktop are built `-H=windowsgui` so that a
   shortcut click, and a run at 02:00, open no console window (design-schedule-v2 S12; desktop
   review R1). The command line needs a console. Any single-file answer on Windows is one of:
   a console program that hides its window for GUI verbs (a console flashes on every click and
   every scheduled run), a GUI program that attaches to the parent console for CLI verbs (the
   shell's prompt returns before the output; PowerShell does not wait; the `pythonw` problem), or
   two files from one package differing in the subsystem flag alone (`python.exe`/`pythonw.exe`,
   `java`/`javaw`). This design takes the third. On macOS and Linux it is one file.

## 2. Properties

Each property: the claim, the invariant it serves or proposes, its label, and the MA rows that
will prove it (§10).

- **P1 — One program, every surface a verb of it.** Every function of the three executables is
  reachable as a verb of `cmd/mailarchive`: the existing verbs, plus `desktop` (the dashboard),
  `wizard` (the dialog wizard) and `run-job` (the wizard's headless run). No surface is reachable
  only from a file that is not the program. On Windows the pair is one program: the two files are
  built from one package in one `go build` matrix and differ in linker flags alone. Proven
  (MA-580, MA-581, MA-582). Proposes **R33**.
- **P2 — Each surface behaves as it does today.** SE1 adds a dispatcher and moves code; it changes
  no refusal text, no exit code, no route, no guard, no file format and no argument of any
  existing surface except the program's name in printed text (P9). Every MA row that proves a
  surface today keeps proving it, pointed at the moved code. **Conditional:** "today" means the
  behaviour at `b1a16bc`; a surface's own later changes are its own. Proven by the existing rows
  through the covers map (the meta-test fails if a moved test loses its marker) and MA-583 (the
  structural walk of the verbs and their flags).
- **P3 — No console where none is shown today; a console where one is expected.** On Windows a
  shortcut, a double-click and a scheduled run start `mailarchivew.exe` and show no console;
  `mailarchive.exe` from a terminal prints to that terminal and the shell waits for it, as today.
  **Conditional (lab L-W1, L-W2):** that the GUI-subsystem build of this package shows no console
  under Task Scheduler for an interactive task, and that a `.cmd` wrapper (which the command
  line's `schedule` still writes on Windows, §4.6) shows the brief console it shows today, are
  facts about Windows, stated from the repository's own existing practice, not re-derived here.
  Proven (MA-584 for the build matrix and the shortcut arguments; the console behaviour itself is
  L-W2).
- **P4 — A schedule names one program, and `status` knows its own names.** On Windows every
  schedule any surface installs after SE2 records `mailarchivew.exe` beside the running file;
  elsewhere, the running file. `health.exeIdentity` treats the pair, and for one release the two
  former names beside the program, as *this program* (§4.6.2), so `status`, the dashboard's health
  view and the wizard's health check agree, whichever file the owner started. Proven (MA-585,
  MA-586). Serves R18 (every run and schedule is legible) and X6, X10.
- **P5 — The upgrade is loud where it cannot be seamless, and never silent.** A descriptor that
  names a former name is detected by every health view; where the former file still exists
  (the Windows MSI keeps the two former names as copies of `mailarchivew.exe` for one release,
  §7.1; the macOS `.app` keeps `mailarchive-gui` as a link to `mailarchive` for one release) the
  schedule keeps running and `status` carries a *note* naming the one-click repair; where it does
  not exist (Linux, a hand-copied install) the existing RED `exe_missing` stands, with its remedy
  now naming the repair every surface offers. No schedule is rewritten without the owner's
  action. Proven (MA-587, MA-588). Lab (L-W3, L-M2).
- **P6 — The per-account instance rules are unchanged.** The `desktop` verb keeps
  design-local-confidentiality P8 and CC21..CC23 exactly: one instance per account per
  configuration, the lock, the `0600` instance file, the challenge-response handoff, the probe of
  the former address 127.0.0.1:8097, and the visible refusals. A `desktop` started from
  `mailarchive.exe` and one from `mailarchivew.exe` are the same program to the lock and to
  `endPreUpgrade`'s "our own program" test (§4.5.2). Proven (the existing MA-376, MA-281 rows
  moved; MA-589 for the pair).
- **P7 — A double-click goes somewhere visible.** `mailarchivew.exe` started with no argument
  opens MailArchive Desktop (the dashboard verb's existing browser-open or link dialog, X11);
  started under a former name with no argument it opens that name's surface (§4.3). On macOS the
  program inside `Mail Archive.app` started with no argument opens the desktop. `mailarchive`
  with no argument from a terminal prints today's usage refusal. No launch starts a silent,
  invisible server (X11). Proven (MA-590). **Conditional (lab L-W4, L-M1, L-L1)** on each OS's
  double-click delivering the environment the rule reads.
- **P8 — New per-user state has one root; existing files do not move.** Every file this design
  adds lives under one helper's directory (§4.9, OPEN-X1's recommended answer). Job files,
  instance files, descriptors and workspace records stay where they are, because schedules and
  running instances name them by absolute path. Proven (MA-591).
- **P9 — No owner-facing text names a program that does not ship.** README, the dashboard's
  add-mail steps (MA-546's `AddMailArgv` text), the health remedies, the wizard's summary, the
  MSI's shortcuts, and `status` name `mailarchive` and its verbs. Proven (MA-592: a structural
  sweep of the texts the tests already pin).
- **P10 — Rollback is a file swap.** The older three executables read every file this design's
  slices write: SE1 and SE2 write no new format (the job file stays version 1; the descriptor
  gains no field); SE3's job files are the wizard's version-1 files. A schedule repaired to the
  new name stops running after a rollback to the old files and `status` says so (RED
  `exe_missing`): loud, and the cost of rolling back, as design-thunderbird-presync P7 states for
  its own case. Proven (MA-593).
- **P11 — The dashboard adds local mail the way the wizard does, with the wizard's guarantees
  (SE3).** "Add mail from this computer" discovers or picks a local source, runs the same
  `app.Run` with the same options the wizard builds (`jobFor`), under the same archive lock, in
  the dashboard's one capture slot, streams into the bounded activity ring (DC3), and offers the
  same schedule, written as the wizard's job file and run by `run-job`. Every content-bearing
  endpoint it adds is a guarded POST (X13); the picker is the existing seam under its existing
  rule (Browse is refused while an export runs, XC31). It changes nothing the archive invariants
  R1..R8 say, because it adds no capture code: it calls the capture the wizard calls. Proven
  (MA-594..MA-602).

## 3. Non-goals (explicit scope cuts)

1. **No new capture source and no change to any reader.** PST, mbox, maildir, Evolution, Graph:
   unchanged. The Outlook-app export (`outlookcom.CreatePSTs`, COM) is not offered from the
   dashboard in SE3 (§4.8.4, Q6).
2. **No rewrite of the wizard as web pages.** The dialog wizard stays, as the `wizard` verb, for
   the owner with no browser, the Outlook-app path and the Thunderbird IMAP preparation
   (`prepareThunderbirdGUI`, which edits `prefs.js` with Thunderbird closed and waits). SE3's page
   does not edit `prefs.js` (Q7).
3. **No change to the dashboard's security boundary.** X9, X12, X13, X15, the session prefix, the
   CSRF/Origin/Host guard, the loopback bind: untouched. SE3 adds routes under the same guard.
4. **No moving of existing state files** (P8). OPEN-X1 is answered for *new* state only.
5. **No signing, notarization, or installer for Linux.** The macOS `.app` stays unsigned; Linux
   stays a bare file plus README text.
6. **No `AttachConsole`, no `FreeConsole`, no PE-header patching at build time.** The pair is two
   ordinary `go build` invocations (§4.2). A later design may collapse them if a lab shows a
   byte-identical pair is reachable; this one does not depend on it.
7. **No change to what a schedule's wrapper does.** The command line keeps writing its `.cmd`
   wrapper on Windows (always), the wizard only when the run string exceeds
   `schedule.SchtasksRunLimit`, the desktop always; those are three recorded choices of three
   designs and are not re-opened here (Q3 records the one the owner may want to change).
8. **No merge of the Microsoft 365 capture into the job file.** `desktop -capture …` keeps its
   argument form; `run-job` runs the wizard's job file only (§4.7). A mixed archive keeps one
   schedule of one kind (Q5).
9. **No removal of the former-name copies in the release that introduces them** (§7). They go
   one release later, by a release note, with `status` loud about any descriptor that still
   names them.
10. **No new state-changing endpoint without the dashboard's existing guard, and no endpoint
    that lists directories.** SE3's discovery lists *mail stores* the engine already discovers
    (`app.DiscoverInputs`), never arbitrary paths (Q1 of the workspace design, kept).

## 4. Mechanism

### 4.1 The dispatcher (SE1)

`main` in `cmd/mailarchive` keeps its first-argument switch and gains three verbs and two legacy
spellings:

| First argument | Runs | Today's equivalent |
|---|---|---|
| `desktop [flags]` | `desktop.Launch(ctx, args, desktop.OSEnv())` (today's `launch`) | `mailarchive-desktop [flags]` |
| `wizard` | `wizard.Run(ctx, zenityDialogs{})` (today's `wizard()`) | `mailarchive-gui` |
| `run-job FILE` | `wizard.RunHeadless(FILE)` (today's `runHeadless`) | `mailarchive-gui -job FILE` |
| `-job FILE` (legacy, exactly two arguments) | `run-job FILE` | the installed wizard schedules |
| `-capture …` (legacy, first argument) | `desktop -capture …` | the installed desktop schedules |
| no argument | §4.3 | — |
| anything else | today's `runExport(args)` | `mailarchive [flags]` |

Rules:
1. The two legacy spellings are recognised **only** as written above (`-job` with exactly one
   following argument; `-capture` as the first argument). Every other flag-first invocation is
   `runExport`, as today, so `mailarchive -out X -input Y` is unchanged and a typo is refused by
   the export verb's parser as today (X3).
2. The legacy spellings are kept for as long as a descriptor may name them: indefinitely. They
   cost one `switch` case each. New installs never write them (§4.6).
3. `-h`/`-help` of the root usage lists the three new verbs; each new verb has its own `-h`
   (X3; MA-39's walk extends to them, MA-583).
4. The `desktop` and `wizard` verbs recover a panic into a dialog and exit 1, as both binaries do
   today (`main` of each); the command-line verbs keep their stderr refusal (X7).
5. Exit codes: `desktop` and `wizard` keep their 0/1; `run-job` keeps the wizard's headless 1 on
   failure; the command line's X1 map is unchanged (MA-583 asserts the set of literal codes is a
   subset of the declared map, as today).

### 4.2 The Windows pair (SE1)

Two builds of `./cmd/mailarchive` for `GOOS=windows`:

```
go build -o mailarchive.exe  ./cmd/mailarchive
go build -ldflags -H=windowsgui -o mailarchivew.exe ./cmd/mailarchive
```

Same source, same module version, same `debug.ReadBuildInfo()` except the `-ldflags` setting.
`packaging/build-release.sh`, the `Makefile`'s `build-windows`, and
`docs/continuation/build_all.py` build both and stop building the former names. The release
check (MA-584, tier S, runs where a Windows build exists: CI's `GOOS=windows go build` step) asserts
`go version -m` of the two files agrees on path, mod and every build setting but `-ldflags`.
**Lab item L-W1:** confirm on a Windows machine that the two files differ in the PE optional
header's Subsystem field alone (plus any checksum); the design does not depend on it, and
`exeIdentity` does not compare the two files' bytes (§4.6.2).

Which file does what:

| Who starts the program | File | Why |
|---|---|---|
| Desktop, Start menu and Startup shortcuts (MSI) | `mailarchivew.exe desktop`, `… desktop -no-browser`, `… desktop -copy-link` | no console (desktop review R1, X11) |
| Any schedule installed on Windows after SE2 | `mailarchivew.exe …` | no console at 02:00 (S12) |
| The owner in a terminal | `mailarchive.exe <verb>` | output and exit code reach the shell (X1, X8) |
| The owner double-clicking `mailarchive.exe` | opens a console and prints the usage refusal | as today for the CLI file |

The dialog helpers (`showRefusal`, `showLink`, the wizard's dialogs) work from either file: a
console process may show a `MessageBox`; from `mailarchive.exe` the text also reaches stderr, as
the desktop already writes both (`dialogs.go`). **Lab item L-W5:** a `zenity` dialog from the
console build under a terminal, and under `schtasks` with no desktop, behaves as it does from
the GUI build (the latter is already the headless case the wizard handles with `notify`).

### 4.3 What a launch with no argument does (SE1)

The rule is a pure function, `defaultVerb(goos, exeBase, hasConsole, inBundle, hasDisplay, stdinIsTerminal)`,
tested in table form (MA-590):

| OS | Condition | Verb |
|---|---|---|
| Windows | base name is `mailarchivew` and no console is attached (`GetConsoleWindow() == 0`) | `desktop` |
| Windows | base name begins `mailarchive-desktop` and no console | `desktop` (former-name copy, §7.1) |
| Windows | base name begins `mailarchive-gui` and no console | `wizard` (former-name copy) |
| Windows | a console is attached (any name) | today's usage refusal |
| macOS | the executable's path contains `/Contents/MacOS/` | `desktop` (Q2) |
| macOS | base name is `mailarchive-gui` (the `.app`'s link, §7.2) | `wizard` |
| macOS | otherwise | today's usage refusal |
| Linux | stdin is not a terminal, and `DISPLAY` or `WAYLAND_DISPLAY` is set | `desktop` (Q2, Q4) |
| Linux | otherwise | today's usage refusal |

An explicit verb always wins; the rule runs only for an empty argument list. `mailarchive > file`
from a terminal keeps a terminal on stdin and prints the usage refusal to stderr, as today. The
inputs are read once, injected, and never consulted again (a test passes them). **Lab items:**
L-W4 (`GetConsoleWindow()` is 0 for an Explorer double-click of the GUI build and non-zero for a
`cmd.exe` start of the console build, including under Windows Terminal), L-M1 (a double-clicked
`.app` whose `CFBundleExecutable` is the program starts it with an empty argument list and a path
under `Contents/MacOS`), L-L1 (GNOME Files and Dolphin "Run" start an executable with no
terminal and with `DISPLAY`/`WAYLAND_DISPLAY` set).

### 4.4 The wizard moves to `internal/wizard` (SE1)

- `cmd/mailarchive-gui/main.go` becomes `internal/wizard`: the pure decision functions
  (`keepRawApplies`, `preferOutlookApp`, `nothingFoundMessage`, `repairSpec`, `exportSummary`,
  `jobFor`, `inVolatileDir`, …) and their 21 tests move unchanged; the flow (`wizard`,
  `healthCheck`, `offerSchedule`, `runExport`, `prepareThunderbirdGUI`) moves behind a `Dialogs`
  interface with one method per dialog kind the flow uses today (`List`, `Question`, `SelectFile`,
  `Info`, `Warning`, `Error`, `Entry`, `Progress`, `Notify`), implemented by `zenity` in one file
  and by a scripted fake in tests. The 83 call sites become calls on the interface; the texts do
  not change (P2).
- The headless run (`runHeadless`, `jobFail`, `notifyFailure`, `logSummary`,
  `cleanupOutlookScratch`) moves to the same package as `RunHeadless(path string) int` and is
  what `run-job` calls.
- `health.Input.Surface` keeps its value `"gui"` for the wizard (the remedy text it selects,
  "in the window that opens with this program", stays true: the program is now `mailarchive`).
  Renaming the value is a text change with no owner benefit and is not done.
- The schedule the wizard installs (`offerSchedule`) and repairs (`repairSpec`) records
  `Args: []string{"run-job", jobPath}` and `Exe` from §4.6.1 instead of `-job`. `repairSpec`'s
  rule stands: the repair takes the job path, log and program from the archive's own names and
  the running program, **never** from the descriptor it repairs.

### 4.5 The desktop launcher moves to `internal/desktop` (SE1)

#### 4.5.1 The move

`launch.go`, `dialogs.go`, `former.go`, `former_windows.go`, `former_linux.go`, `former_other.go`,
`policy_windows.go`, `policy_other.go` and the handler constructors in `main.go` move into
`internal/desktop` (a `launch` file set), with their 21 tests. `launchEnv` and `osEnv` are already
the seam those tests inject; nothing in them names the binary except the refusal texts
(`portBusyText` says "mailarchive-desktop: -addr …"), which become "mailarchive desktop: -addr …"
(P9). The `flag.NewFlagSet("mailarchive-desktop", …)` becomes `"mailarchive desktop"` so `-h`
reads the real invocation (X3).

#### 4.5.2 The pair and "our own program"

`endPreUpgrade` (Windows) ends an older instance holding the former address only when it "runs
this program file as this user" (`isOurOwnProgram`: the image path equals `os.Executable()`).
After SE1 the older instance is `mailarchive-desktop.exe`, which is *not* `os.Executable()` of the
new program, so the new program would never end it and the owner would see the PreUpgrade notice
until sign-out. That is CC22's existing fallback, not a new failure. SE1 widens the test to
"the image's base name is one of this program's names (the pair and the two former names) in
the same directory as the running file, run by the same user" (MA-589). The directory bound keeps
the trust where it is today: whoever can write the program's directory can replace the program.

#### 4.5.3 The instance file

`instance.Info.Exe` records `os.Executable()` (informational; `status` and the dashboard show it).
Unchanged. The lock, the session id, the handoff key: unchanged.

### 4.6 Schedules: one program name and a repair every surface offers (SE2)

#### 4.6.1 `schedule.ProgramForSchedule()`

One helper in `internal/schedule`, used by the command line's `runSchedule`, the wizard's
`offerSchedule`/`repairSpec` and the desktop's `backupSpecFor`:

- `exe := os.Executable()` (resolved through symlinks, absolute).
- On Windows, if `exe`'s base name is `mailarchive.exe` and `mailarchivew.exe` exists as a
  regular file in the same directory, return that sibling; otherwise return `exe`. A former name
  (`mailarchive-desktop.exe`, `mailarchive-gui.exe`) running from the MSI's copies returns the
  sibling `mailarchivew.exe` when present, else itself.
- Elsewhere return `exe`.

Nothing in the archive, the descriptor or the job file influences the choice (P6 of
design-thunderbird-presync, applied here: settings never come from the archive). `Spec.Validate`
keeps requiring an absolute `Exe`.

#### 4.6.2 `health.exeIdentity` learns the program's names

Today: `isThis = sameFile(self, d.Exe) || (size and mtime equal)`. After SE2:

```
isThis = sameFile(self, d.Exe)
      || (dir(self) == dir(d.Exe) && knownName(base(d.Exe)) && knownName(base(self)))
      || (size and mtime equal, as today)
knownName ∈ {mailarchive, mailarchivew, mailarchive-desktop, mailarchive-gui} (+ .exe)
```

and a new **note** (not WARN), `exe_former_name`, when `isThis` holds through a former name:
"the schedule runs `<path>`, a former name of this program; it works, and the next
Repair/re-install points it at `mailarchive`". The reason codes `exe_missing` (RED) and
`exe_moved` (WARN) are unchanged in meaning; their remedy text names the repair in all three
surfaces ("re-run `mailarchive schedule … -install`, or Repair in MailArchive Desktop or the
wizard"). The same-directory bound is the trust bound of §4.5.2. **Honesty:** the size+mtime
fallback stays because descriptors written before SE2 record `ExeSize`/`ExeMTime` and nothing
else identifies a copied file; it is as weak as it is today.

#### 4.6.3 The repair

- The wizard already repairs (`repairable`, `repairSchedule`): offered when the schedule is not
  installed, the program is missing, or the program is not this one. Unchanged, now also offered
  on the `exe_former_name` note (one line added to `repairable`).
- The dashboard's Weekly backup card gains the same offer for *its* descriptor: when the
  descriptor's `Exe` is a former name or is missing, a "Repair the schedule" button that
  re-installs `backupSpecFor` from `ProgramForSchedule()`; a guarded POST like `schedule-install`
  (it *is* `schedule-install`; the button is the existing endpoint with the existing guard).
- The command line's `schedule -install` is the repair, as the remedy texts say today.
- No repair runs unasked. A scheduled run never rewrites a schedule (P5).

#### 4.6.4 The desktop's `-capture` argument form

Unchanged in SE1/SE2 (non-goal 8). `backupSpecFor` records `Exe` from §4.6.1 and
`Args: ["desktop", "-capture", …]`; the legacy `-capture`-first spelling stays accepted (§4.1).

### 4.7 `run-job` (SE1)

The verb is the wizard's headless run under its own name: `mailarchive run-job FILE`. It reads
the job file with `job.Read` (owner-only file, schema-checked, newer version refused, as today),
refuses when `-out` is absent (the unmounted-drive guard), logs to `<out>/mailarchive.log`,
runs `app.Run` with `Unattended: true`, and exits 1 on failure with the desktop notification. It
is one function moved and one `switch` case. `schedule`'s dry-parse (design-schedule-v2 P2) does
not need to learn it: a job file is validated when written and when read, and the schedule the
wizard installs is not built through `schedule`'s flat form. (Q3 asks whether the command line
should be able to install a job-file schedule; the recommended answer is not in this design.)

### 4.8 The dashboard adds local mail (SE3)

#### 4.8.1 Where it appears

The sidebar's Workspace group gains **Add mail from this computer** (label per X11: the outcome,
not the mechanism). On a local-only archive (MA-546's `kindLocal` with no Microsoft 365 setup) the
page replaces the "Archive now is off" explanation's wizard/command-line steps with a link to
this page *and* keeps the command-line steps (an owner on a headless box still wants them). On
a Microsoft 365 or mixed archive the page is available too (adding a PST to an M365 archive is
what the engine's store-scoped keys were built for, S30).

#### 4.8.2 The steps (the wizard's, in a page)

1. **Source.** "Find my mailboxes" (POST `/api/local/discover`: `app.DiscoverInputs(nil, true)`,
   returns store paths and the wizard's `autoInputLabel` text; the per-OS nothing-found message
   when empty), or "Choose a folder" / "Choose a file" (the picker seam: a directory variant for
   Thunderbird/Evolution/maildir, a file variant with the wizard's `*.pst;*.ost` and mbox
   filters), or a typed path. The Outlook-app export is not offered (Q6); on Windows, when a
   discovered store is a live `.ost` and classic Outlook is detected (`preferOutlookApp`), the page
   says so and names the wizard's option, as the wizard's own question does.
2. **Options.** Mode (incremental default, full), the date window (`util.ParseSince`), copy-first
   for `.pst`/`.ost`, keep-raw where it applies (`keepRawApplies`), follow folder links per input
   where it applies (`followLinksWorthAsking`), Deleted Items/Junk where it applies
   (`deletedJunkApplies`), each through the wizard's pure predicates so the page and the wizard
   cannot disagree on when a question applies. The Thunderbird IMAP preparation is a *notice*
   with the wizard's words ("get everything downloaded first"), not an action (non-goal 2).
3. **Run.** POST `/api/local/run` builds `app.Options` through the wizard's `jobFor` (so the run
   is the one the schedule would repeat), takes the dashboard's one capture slot (the `capture`
   mutex that "Archive now" takes; a second start is refused with the existing "a capture is
   running" text), runs `app.Run` in the recovered goroutine, streams the log into the activity
   ring, and reports the wizard's `exportSummary` counts in the result line, mirroring `status`
   (X10). Browse is refused while it runs (XC31).
4. **Schedule.** The result offers "Keep this archive current": daily 02:00 or weekly Sunday
   03:00 (the wizard's two), writing the wizard's job file (`job.Write`, version 1, `jobFor`) and
   installing `ProgramForSchedule() run-job <path>` with the wizard's wrapper rule, through the
   same guarded POST pattern as `schedule-install`. The Weekly backup card then shows this
   schedule for the archive (its descriptor is the archive's, `schedule.DefaultNameFor(out)`).
   The wizard's volatile-directory warning (`inVolatileDir`) is shown as a card note before the
   install, with the same words.

#### 4.8.3 The picker's file variant

`pickFolder` becomes `pickPath(ctx, title, kind)` with `kind ∈ {dir, file(filters)}`; the seam
keeps its rules (one dialog at a time, refused while an export runs, 10-minute budget, the clean
argv on Linux, MA-433). The destination chooser keeps calling it with `dir`.

#### 4.8.4 What is refused and why (X2 words)

| Case | Refusal |
|---|---|
| A path that is not a mail store the engine recognises | "`<path>` is not an Outlook data file, an mbox file or a mail folder MailArchive can read. Why: only those are archived. Do this: choose the folder that holds your mail files (for Thunderbird, the account's folder under Mail or ImapMail)." (`app.DiscoverInputs`'s own refusal, re-worded by the page as the wizard does) |
| A run already in progress | the existing capture-slot text |
| The archive lock held by another run | the existing `app.Run` lock refusal, shown in the activity ring |
| The Outlook-app path | "Exporting through the Outlook app is done in the MailArchive wizard: run `mailarchive wizard` and choose *Outlook account (via Outlook app)*." |
| `-out` inside the source, or the source inside the archive | the engine's existing refusals |

#### 4.8.5 One capture at a time, one lock

`app.Run` takes the archive lock (R5). The dashboard's "Archive now" (`app.RunGraph`) and this
run share the capture slot, so the dashboard never asks the lock twice. A command-line run of
the same archive at the same time is refused by the lock as today, with the holder's verb in the
message (MA-545's holder line: this run's verb is `export`).

### 4.9 The state root (SE1; OPEN-X1)

One helper, `internal/state/userdir.StateRoot()`: `%LOCALAPPDATA%\MailArchive` on Windows,
`os.UserConfigDir()/mailarchive` elsewhere. **New** files (none in SE1/SE2; SE3 writes none
either: it writes the wizard's job file where `job.Dir` puts it) go under it. `job.Dir`,
`instance.Dir` and `desktop.StateDir` keep their paths (P8) and gain a comment naming the root
they will converge on when a design moves them with a migration. This answers OPEN-X1 with the
operator's recommended default and moves nothing. **Q8** records the alternative.

### 4.10 Packaging

- `build-release.sh`: Linux `mailarchive-linux-{amd64,arm64}`; Windows `mailarchive-windows-amd64.exe`
  and `mailarchivew-windows-amd64.exe`; macOS `MailArchive-macos.zip` (the `.app`, §7.2) and the
  CLI tarball (now the same file as the one inside the `.app`). The MSI payload: `mailarchive.exe`,
  `mailarchivew.exe`, and for one release `mailarchive-desktop.exe` and `mailarchive-gui.exe` as
  copies of `mailarchivew.exe` (§7.1). The payload README says so.
- `Product.wxs`: shortcuts target `mailarchivew.exe` with `Arguments="desktop"`,
  `"desktop -no-browser"`, `"desktop -copy-link"`; `util:CloseApplication` for
  `mailarchive-desktop.exe` **and** `mailarchivew.exe` (an instance of this version holds the lock
  and must be ended before its file is replaced, CC22); the four exe components; the
  `packaging_test.go` assertions follow (it parses the WiX source).
- `Info.plist.in`: `CFBundleExecutable` = `mailarchive`; `Contents/MacOS/mailarchive-gui` is a
  symbolic link to `mailarchive` for one release (§7.2).
- CI: `GOOS=windows govulncheck ./cmd/mailarchive-desktop` becomes `./cmd/mailarchive` (MA-529's
  text names the path; its catalog row is edited in the catalog-first PR, by the operator).
- `Makefile`: `build-gui`/`build-desktop` go; `build-windows` builds the pair.

## 5. Security

### 5.1 Trust boundaries

1. **Scheduler → program.** Arguments written by the owner's own install (`run-job <path>`,
   `desktop -capture …`). Trusted like `-out` today.
2. **The program's directory → program.** `ProgramForSchedule` and `exeIdentity` look at
   *sibling files* of the running executable by fixed names. Whoever can write that directory can
   replace the running file itself; the sibling rule adds no principal. Program Files is
   administrator-writable; a user-directory install (Downloads) is the wizard's existing
   `inVolatileDir` warning, now shown by the dashboard too.
3. **Browser → dashboard.** Unchanged (X9, X12, X13). SE3's endpoints sit behind the session
   prefix and the CSRF/Origin/Host guard like every workspace endpoint.
4. **Archive directory → program.** The descriptor's `Exe` is read for *display and identity
   comparison only*; nothing executes it; the repair re-installs from the running program.
   Unchanged from the wizard's `repairSpec` rule, now shared by three surfaces.
5. **Job file → run-job.** The owner's own `0600` file in the config directory; `job.Read`
   validates; a same-user attacker who can write it can edit the crontab too (DC4, Type I).

### 5.2 Untrusted inputs and how each is handled

| Input | From | Handling |
|---|---|---|
| First argument | the scheduler, a shortcut, the shell | an exact-match `switch`; unknown strings fall to the export verb's flag parser, which refuses what it does not know (X3) |
| The executable's own path and base name | the OS | read once through `os.Executable` + `EvalSymlinks`; compared to a fixed name set; never interpolated into a command; the only thing done with it is `schedule.Spec.Exe` (quoted by `winQuote`/the plist/crontab writers as today) |
| Sibling file beside the executable | the program's directory (trust boundary 2) | `Lstat`: must be a regular file (no link), one of the fixed names; never read, never executed by the program itself |
| Descriptor `Exe`, `ExeSize`, `ExeMTime` | the archive (untrusted) | display (control-stripped, as today) and identity comparison; never executed, never used as the repair's program |
| Typed or picked source path (SE3) | the owner's browser session | the engine's own discovery/refusal (`app.DiscoverInputs`); displayed control-stripped; it is the owner's path, under the owner's rights, exactly as `-input` is |
| Discovery results (SE3) | the file system | mail store paths and labels: mail-derived text, rendered through `textContent` (X15), `nosniff`, `no-store` |
| Environment (`DISPLAY`, `WAYLAND_DISPLAY`) | the launcher | presence only, read once, for the no-argument rule |

### 5.3 Attacker personas

| Persona | Attack | Mitigation | Residual / label |
|---|---|---|---|
| **Another account on the same computer** | plant `mailarchivew.exe` beside the program so the next schedule install records it | the program's directory is Program Files (administrators) or the owner's own folder (owner); an attacker who can write there can replace `mailarchive.exe` itself; no new principal (trust boundary 2) | Type I, as today for the program file |
| **Same-user malware** | edit the job file, the crontab, the descriptor | DC4: a same-user process has the user's rights | Type I |
| **Archive writer** (a NAS, a synced folder) | a descriptor whose `Exe` names an attacker's file so a "repair" installs it; or a size/mtime that makes a planted file read as "this program" | the repair never takes the program from the descriptor (`repairSpec` rule, now shared); `exeIdentity`'s same-directory bound means a forged `Exe` outside the program's directory is `exe_moved` at best and never "this program" through the name rule; the size+mtime fallback is today's and is as weak as today (stated) | Type I for the display; the fallback's weakness is a stated residual |
| **A hostile first argument** (a shortcut edited by malware, a crafted `.lnk`) | `mailarchive <something>` runs an unintended surface | every verb is one the owner could run by hand; no verb elevates; `desktop` binds loopback and needs the session (X9); `wizard` shows dialogs; nothing runs unattended except the two headless forms, which read only owner-written files | no change from today's three files |
| **A squatter at the former address** | as today | unchanged (CC23) | — |
| **A rebinding page, a hostile link against SE3's endpoints** | POST to `/api/local/run` or `/discover` | the same guard as every workspace endpoint (X9, X13); a request without the session gets the locked page (X12); a run needs the capture slot and the archive lock | as the workspace |
| **An owner's typed path to a non-mail file** (SE3) | `/etc/passwd` as "an mbox" | `app.DiscoverInputs` recognises stores by structure; a plain file is refused unless it parses as mbox; the engine already reads any file the owner names with `-input` under the owner's rights | not an escalation; the engine's existing behaviour |

### 5.4 What the program must never do (each is a row in §10)

1. Execute, `exec`, or re-launch itself or a sibling to perform a function another surface has
   (desktop owner ruling P1 and Q14/OR8: in-process, never a child of itself). The pair exists
   for the subsystem flag alone; neither file starts the other.
2. Take a program path from the archive, the descriptor, the job file or the instance file as
   the thing to schedule or to run.
3. Start a server, a browser or a dialog from a command-line verb the owner did not name
   (`defaultVerb` applies to an empty argument list only).
4. Rewrite a schedule unasked (P5), including from a scheduled run.
5. Serve any SE3 endpoint outside the session prefix and the guard, or add an endpoint that lists
   directories.
6. Run `zenity` or show any dialog from a command-line verb (MA-603: a fake `zenity` on `PATH`
   is never invoked by `status`, `verify`, `serve`, `search`, `reindex`, `schedule`, `graph`,
   `extract`, `setup`, the export verb or `run-job`).

## 6. Owner-facing text

What the owner reads changes in name only (P9), plus the new verbs:

- README: the "Quick start" table names `mailarchivew.exe` (Windows), `Mail Archive.app` (macOS)
  and `mailarchive desktop` / `mailarchive wizard` (Linux); the GUI section becomes "The wizard
  (`mailarchive wizard`)"; "Adding new mail to a Thunderbird archive" gains the dashboard's page
  as the first way and keeps the wizard and the commands; a short "One program" paragraph says
  what the pair is and why two files exist on Windows.
- `AddMailArgv`/`AddMailAutoArgv` already name `mailarchive`. The wizard step in `addMailHelp`
  reads "run `mailarchive wizard` (on Windows, double-click `mailarchivew.exe` or the MailArchive
  Desktop shortcut and choose *Add mail from this computer*)".
- Health remedies: "re-run `mailarchive schedule … -install` from the current program, or Repair
  in MailArchive Desktop or the wizard".
- The desktop's "The MailArchive wizard (mailarchive-gui) last used …" line names
  `mailarchive wizard`.
- `status`: a `Program:` line (what the descriptor names, and "this program" / "a former name of
  this program" / "not this program" / "missing").
- Release notes: the former names, the one-release copies, the repair, the rollback consequence.

## 7. Migration and coexistence

### 7.1 Windows (MSI)

Release N (SE1+SE2):

1. The MSI installs `mailarchive.exe`, `mailarchivew.exe`, and `mailarchive-desktop.exe` and
   `mailarchive-gui.exe` as **copies** of `mailarchivew.exe` (two more components, same source
   file). Shortcuts point at `mailarchivew.exe desktop …`. `util:CloseApplication` names both
   `mailarchive-desktop.exe` and `mailarchivew.exe`. **Lab item L-W3:** the upgrade over the
   release before, with an instance running in two sessions (CC22's existing lab case), and the
   installed file times of the copies.
2. An installed weekly backup (`mailarchive-desktop.exe -capture …`) keeps running: the copy runs
   the legacy spelling (§4.1). An installed wizard schedule (`mailarchive-gui.exe -job …`) keeps
   running the same way. `status` and both health views show the `exe_former_name` **note** with
   the repair. Nothing is RED on upgrade day.
3. The owner's first click on Repair (dashboard or wizard), or any `schedule -install`, re-records
   `mailarchivew.exe run-job …` / `mailarchivew.exe desktop -capture …`.
4. A desktop instance of the old version still running at upgrade: ended by `CloseApplication`
   (as today); one in another session: CC22's PreUpgrade notice (as today), and §4.5.2's widened
   test lets the new program end it when it is this account's.

Release N+1: the two copies go. A descriptor still naming a former name is RED `exe_missing` with
the repair remedy: loud (P5, P10). The release note says so one release ahead.

Disk cost of release N: two more ~31 MB files, against today's three distinct ~30 MB files; a
net saving of about one file. (**Lab item L-C1:** measure the merged file; the desktop's build
already links nearly everything, so the estimate is 31 to 33 MB.)

### 7.2 macOS (`.app` and the CLI tarball)

- `Mail Archive.app/Contents/MacOS/mailarchive` is the program; `mailarchive-gui` beside it is a
  symbolic link to it for one release, so a launchd plist that names the old path keeps running
  (`defaultVerb`'s base-name rule gives it the wizard when started with no argument, and the
  plist passes `-job`, the legacy spelling). **Lab item L-M1/L-M2:** the `.app` with a one-binary
  executable opens on first launch with the same right-click-Open flow as today, and launchd runs
  the program through the link.
- The CLI tarball ships the same file. The README's "do not double-click the command-line
  program" text stays: a bare Mach-O still opens in a text editor.
- The wizard's health check already offers the repair when the program is missing or moved;
  after N+1 removes the link, a plist naming the old path is RED `exe_missing` until repaired.

### 7.3 Linux

Bare files; owners copy them where they like. An owner who replaces `mailarchive-gui` with
`mailarchive` has a crontab naming a file that is gone: RED `exe_missing`, the wizard's health
check offers the repair on the next `mailarchive wizard`. The README says: copy the new file,
then run `mailarchive wizard` once and accept the repair (or `mailarchive schedule … -install`).
No copies, no links: the owner placed the file; the program cannot know where.

### 7.4 Mixed versions and rollback

- Older files read everything the new ones write in SE1/SE2 (no format change) and SE3 (version-1
  job files). An older `mailarchive-gui` run against a job file SE3 wrote runs it.
- A schedule repaired to the new names stops running after a rollback to the old files: RED
  `exe_missing`, the remedy names the old binary's own `schedule -install`. Stated in the
  release note (P10).
- A pre-SE2 `status` reading a post-SE2 descriptor sees `mailarchivew.exe` as "not this binary"
  (`exe_moved` WARN) when run from `mailarchive.exe`: a rollback-only, loud-not-wrong state.

### 7.5 Running instances and the lock

The lock is per account and configuration (`instance.Acquire`), not per file name; a `desktop`
of the new program and an old `mailarchive-desktop` of the same account contend for the same
lock and the old one wins until it exits (CC21's "wedged or other configuration" refusals name
it). Unchanged.

## 8. Operator walks

1. **Windows owner, double-click.** Installs the MSI; double-clicks "MailArchive Desktop" (→
   `mailarchivew.exe desktop`); the browser opens at the private link (as today). Sidebar: Add
   mail from this computer → Find my mailboxes → one Thunderbird store found → Incremental →
   Run → the activity ring streams → "Added 1,204 messages" → Keep current: weekly → the job
   file is written, the task installed, the Weekly backup card shows it. Steps: 7 clicks. No
   console at any point (L-W2).
2. **Windows owner, upgrading from release N-1 with both schedules.** Runs the MSI; the running
   dashboard is closed and replaced; clicks the Desktop shortcut; the Status & health card shows
   one note per schedule, "runs a former name of this program; works; Repair to point it at
   `mailarchive`"; clicks Repair twice. Or ignores it: both schedules keep running until N+1.
3. **macOS owner, Terminal.** `mailarchive status -out ~/Archive` → `Program: … mailarchive-gui
   (a former name of this program)`; `mailarchive wizard` → the health check offers Repair →
   accepts → `Program: … mailarchive (this program)`.
4. **Linux owner, no browser on the box, schedule from the shell.** `mailarchive schedule -install
   -- -input ~/.thunderbird/x/Mail/Local\ Folders -out /srv/archive` as today; `mailarchive
   desktop` is never needed; `mailarchive status` as today; no `zenity` on the machine and none
   needed (MA-603).
5. **Inheritor, cold, reading `status`.** `mailarchive status -out X` prints what runs the
   schedule and whether it is this program, a former name, moved or missing, each with the one
   remedy; `mailarchive -h` lists every surface as a verb; there is one file to find.
6. **Windows administrator, RMM.** `Install-RMM.ps1` stops `mailarchive-desktop` and now also
   `mailarchivew` before `msiexec`; `Detect-RMM.ps1` detects `mailarchivew.exe`;
   `Cleanup-CurrentUser.ps1` unchanged. `packaging_test.go` pins all three.

Friction classification (per friction-review.md): the two-file pair on Windows is **Type I**
(the subsystem flag is the control: no console on click, a console in the shell); the one-release
copies are **Type II** chosen deliberately to avoid a RED upgrade day, and leave in N+1; the
repair being a click rather than automatic is **Type I** (P5: no schedule rewritten unasked).

## 9. Test strategy

### 9.1 What moves

Every test of `cmd/mailarchive-gui` and `cmd/mailarchive-desktop` moves with its code into
`internal/wizard` and `internal/desktop`, keeping its `// covers:` marker; the covers-map
meta-test is the proof that none is lost (a U/S row with no covering test fails). The
`cmd/mailarchive` `TestMain` already builds the real program once; the desktop's and wizard's
process-level refusal tests (exit code and stderr) can now run against it where they ran
in-process before, and the new rows below do.

### 9.2 New rows (tier U unless marked)

Dispatch and the pair (SE1): MA-580 (every surface is a verb; the former spellings), MA-581
(the legacy spellings are recognised only in their exact shape), MA-582 (neither file ever
starts the other: argv-hygiene fake of the program on `PATH`), MA-583 (S: the verb/flag walk and
the exit-code set), MA-584 (S: the Windows build matrix and shortcut arguments, parsed from
`build-release.sh`, the `Makefile` and `Product.wxs`), MA-590 (the no-argument table), MA-592 (S:
no owner-facing text names a former name), MA-603 (no dialog from a command-line verb).

Schedules (SE2): MA-585 (`ProgramForSchedule` on each OS, with and without a sibling, from each
of the four names), MA-586 (`exeIdentity`: the pair and the former names in the same directory
are this program; the same names in another directory are not; the note's text), MA-587 (the
dashboard's repair re-installs from the running program, never from the descriptor), MA-588
(the wizard's repair offered on the former-name note), MA-589 (`isOurOwnProgram` for the pair
and the former names; another directory refused), MA-591 (the state root helper and that
SE1..SE3 write nothing new outside it), MA-593 (rollback: an older reader reads SE3's job file;
a descriptor naming `mailarchivew.exe` read by a pre-SE2 assessment is `exe_moved`, never
"this program" and never silent).

Add mail from this computer (SE3): MA-594 (the page is served under the session prefix only;
every endpoint refuses without the guard; nothing starts), MA-595 (discover returns the engine's
stores and the nothing-found text; lists no directory), MA-596 (the file picker variant: filters,
cancel, unavailable, busy, refused during an export), MA-597 (the options the page offers equal
the wizard's predicates for the same inputs: table over the source types), MA-598 (run: a real
Thunderbird fixture archived by the real engine from the page; the archive equals the wizard's
run of the same `jobFor`; the activity ring is bounded), MA-599 (one capture at a time: a run
while "Archive now" is live is refused with the existing text and starts nothing; the lock holder
line names `export`), MA-600 (schedule: the job file equals the wizard's for the same choices;
the task names `ProgramForSchedule() run-job <path>`; the Weekly backup card shows it; the
volatile-directory note), MA-601 (refusals of §4.8.4, each X2-shaped, each with the side effect
absent), MA-602 (a local-only archive's page offers the run; a Microsoft 365 archive's page
offers it too; the add-mail steps name the page). Lab, pending: MA-604 (L: Windows double-click,
console behaviour, MSI upgrade with copies: L-W1..L-W6), MA-605 (L: the macOS `.app` and the
launchd link: L-M1..L-M3), MA-606 (L: Linux file-manager launch: L-L1..L-L2).

### 9.3 Lab items

| Id | Question | Decides |
|---|---|---|
| L-W1 | the pair differs in the Subsystem field (and checksum) alone | nothing (informational; §4.2) |
| L-W2 | `schtasks` runs `mailarchivew.exe run-job …` as an interactive task with no console flash; the CLI's `.cmd` wrapper still flashes as today | the Windows release note's wording; whether Q3 is worth asking |
| L-W3 | MSI upgrade N-1 → N with a running instance in two sessions; copies' file times; `CloseApplication` on both names | SE1 ships for Windows |
| L-W4 | `GetConsoleWindow()` for an Explorer double-click of the GUI build, a `cmd.exe` start, a Windows Terminal start, a `schtasks` start of each file | the no-argument rule on Windows |
| L-W5 | a dialog from the console build under a terminal and under `schtasks` with no desktop | the refusal path of `desktop`/`wizard` from `mailarchive.exe` |
| L-W6 | `isOurOwnProgram`'s widened test ends an old `mailarchive-desktop.exe` of this account at the former address | §4.5.2 |
| L-M1 | `.app` with `CFBundleExecutable=mailarchive`: first-open flow, empty argv, path under `Contents/MacOS` | the no-argument rule on macOS |
| L-M2 | launchd runs the program through the `mailarchive-gui` link; the repair rewrites the plist | §7.2 |
| L-M3 | the universal binary (`makefat`) of the merged program | the `.app` build |
| L-L1 | GNOME Files / Dolphin "Run" of the program: no terminal, `DISPLAY` set | the no-argument rule on Linux (Q4) |
| L-L2 | a Linux box with no `zenity`: every command-line verb runs; `wizard` refuses naming the package | MA-603's lab twin |
| L-C1 | the merged file's size on each OS | the README's note; nothing else |

### 9.4 Prove-fail for the dispatcher

Mutate `main`'s switch so `desktop` falls to `runExport`: MA-580 red (the export verb refuses
`desktop` as a path). Remove the `-job` legacy case: MA-581 red. Make `ProgramForSchedule`
return `os.Executable()` on Windows: MA-585 red. Drop the same-directory bound from
`exeIdentity`: MA-586 red (a forged descriptor in another directory reads "this program").

## 10. Catalog rows proposed (nothing is added to the catalog here)

**Invariant proposed:**

- **R33 — One program.** Every function the product offers is a verb of one executable
  (`mailarchive`), reachable from the command line, and every schedule and shortcut the product
  installs names that executable (on Windows, the GUI-subsystem build of it, `mailarchivew.exe`).
  The program never starts a copy of itself to perform a function, and no surface exists only in
  a file that is not the program. `status` tells a former name of the program from a moved or
  missing one, naming the repair.

**Scenario proposed:**

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S59 The owner meets the product through one program: a double-click, a shortcut, a schedule and a terminal each start `mailarchive` (on Windows one of its pair), the dashboard adds local mail, and an upgrade from the three-file release keeps every schedule running and legible | R33, R18, R5, R19, R12 | a schedule naming a former name keeps running while the former name exists and is a note with a one-click repair; when it is gone it is RED `exe_missing` with the same repair; no schedule is rewritten unasked; a no-argument launch always opens a visible surface or prints the usage; no command-line verb shows a dialog | MA-580..MA-606 |

**UX contract proposed:**

- **X17 — One program, one name in every text.** Every owner-facing text (README, refusals,
  remedies, dashboard steps, release notes, shortcuts) names the program as `mailarchive` and a
  surface as its verb; no text names a file that does not ship. Enforcement: MA-592 (S).

**Test-spec rows:** MA-580..MA-606 as §9.2 states, tier U unless marked S or L; the three L rows
carry "pending" and the lab items they wait on. Each row's Covers cell cites S59 and the R rows
its text names (R33; R18 for MA-585..MA-588; R5 for MA-599; R19, R12 for MA-594, MA-601).
MA-529's existing text (`govulncheck` on `./cmd/mailarchive-desktop`) is edited to `./cmd/mailarchive`
in the SE1 catalog-first PR (operator's edit). X9's and X11's texts, which name
`mailarchive-desktop`, are edited to `mailarchive desktop` in the same PR.

## 11. Build slices (small, in dependency order)

| Slice | What | Depends on | Gates owed |
|---|---|---|---|
| **SE0** | this design through its pre-code review; catalog-first PR for R33, S59, X17, MA-580..MA-606 (ids re-checked); OPEN-X1 answered as §4.9 | — | design review (this document) |
| **SE1** | the dispatcher; `internal/wizard`; the launcher into `internal/desktop`; `run-job`; the no-argument rule; the pair in the build scripts; packaging (MSI with copies, `.app` with link); README and texts (P9); `cmd/mailarchive-gui` and `cmd/mailarchive-desktop` deleted | SE0 | **friction review** (the double-click, upgrade and inheritor walks of §8; the surfaces are shippable); **adversarial pass** limited to §4.3 and §4.5.2 (the no-argument rule and the widened "our own program" test) |
| **SE2** | `ProgramForSchedule`; `exeIdentity` and the `exe_former_name` note; `status`'s `Program:` line; the dashboard's Repair; the wizard's repair on the note | SE1 | **adversarial pass** (the identity rule against the archive-writer and planted-sibling personas) |
| **SE3** | Add mail from this computer: discover, picker file variant, options, run, schedule; the add-mail steps name it | SE2 | **adversarial pass** (new content-bearing POSTs, X13) and **friction review** (the §8.1 walk, measured) |
| **SE4** (release N+1) | remove the former-name copies and the `.app` link; release note; `exe_former_name` becomes unreachable and its text is retired | SE2 shipped one release earlier | none beyond the release note |

Producers precede sinks: SE2's `ProgramForSchedule` exists before SE3 writes a schedule with it;
SE1's `run-job` exists before SE3 schedules it; the copies (SE1) exist before any descriptor can
name a former name that is missing.

## 12. Honesty of claims

| Claim | Label | Condition / what proves it |
|---|---|---|
| Every surface is a verb of one program | proven (MA-580, MA-583) | — |
| The pair is one program | proven (MA-584: same module, same settings but `-ldflags`) | L-W1 is informational |
| No console where none is shown today | conditional | the GUI-subsystem build under Task Scheduler (L-W2); the CLI's `.cmd` wrapper still flashes, as today |
| Each surface behaves as at `b1a16bc` | proven through the moved rows | the covers map is the guard that none is lost |
| `status` knows the program's names | proven (MA-586) | the size+mtime fallback is as weak as today |
| Upgrade day is never RED | conditional | Windows and macOS: the copies/link exist (L-W3, L-M2); Linux: not claimed (§7.3: RED with the repair) |
| A double-click goes somewhere visible | conditional | L-W4, L-M1, L-L1 |
| Rollback is a file swap | proven (MA-593) | a repaired schedule is loud-broken after rollback, stated |
| The dashboard adds local mail with the wizard's guarantees | proven (MA-598: equal archives) | the Outlook-app path and the `prefs.js` edit are not offered (Q6, Q7) |
| No command-line verb shows a dialog | proven (MA-603) | L-L2 on a real box without `zenity` |
| Disk: one file, ~31 MB, plus two copies for one release on Windows | lab (L-C1) | — |

## 13. Owner questions

Each has a recommended default and a safe interim; no slice is held by an unanswered question
unless marked.

| # | Question | Recommended | Safe interim | Blocks |
|---|---|---|---|---|
| Q1 | Verb names: `desktop`, `wizard`, `run-job`? (X4 says verbs; the catalog already has `graph`, `status`, `setup`.) | yes; `open` was considered for the dashboard and rejected as ambiguous with `serve` | as recommended | SE1 (names in texts) |
| Q2 | No-argument default on macOS (inside the `.app`) and Linux (no terminal, a display): the dashboard, or the wizard? Today a double-click gives the wizard. | the dashboard: it has the wizard's functions after SE3 and the archive's health on its first page; the wizard stays one verb away | the dashboard | SE1 |
| Q3 | Should the command line's `schedule` install a job-file schedule (`schedule -install -- run-job FILE`), so Windows CLI installs can drop the `.cmd` wrapper and its console flash? | not in this design; a later slice on a stated need (L-W2 first) | no | none |
| Q4 | Linux: honour a file-manager double-click at all (the `DISPLAY` rule), or require the verb? | honour it (today's Linux `mailarchive-gui` users double-click); L-L1 first | require the verb (README) | SE1 Linux text |
| Q5 | A mixed archive (Microsoft 365 and local stores) wants both kinds of schedule under one name. One run that does both (a `m365: true` job field), or one kind per archive? | one kind per archive in SE3, the second asks for a separate archive folder or the command line; a combined job is a later design | one kind | SE3 |
| Q6 | Offer the Outlook-app export (COM) from the dashboard page? | no in SE3: COM in the process that serves the dashboard and holds the token is XC31's class of risk; the page names the wizard | no | SE3 |
| Q7 | Offer the Thunderbird `prefs.js` offline-download edit from the dashboard? | no: it needs Thunderbird closed and an owner at the keyboard; the page shows the wizard's notice | no | SE3 |
| Q8 | OPEN-X1: the state root for new files is `%LOCALAPPDATA%\MailArchive` / `UserConfigDir/mailarchive` (§4.9), existing files unmoved. Or move the job files and instance files under it with a migration? | as §4.9: no move | as §4.9 | none |
| Q9 | Keep the former-name copies for one release (N) or two? | one; `status` is loud from N+1 | one | SE4 timing |

## 14. Pre-code review readiness

Lenses to run (all ten), and what each should attack first:

1. **Purpose / threat-model fit:** does the pair over-claim "one program" (R33's wording)?
   Does P5 over-claim "never silent" for Linux?
2. **Focus / proportionality:** is SE3 a separate design? Are the copies (§7.1) and the link
   (§7.2) simpler than a repair-on-first-launch? Is `run-job` a rename worth a verb?
3. **Durability:** the same-directory identity rule across years of moves and reinstalls; the
   size+mtime fallback; the copies' lifetime; a descriptor that names a former name forever.
4. **Usability:** the §8 walks; the Windows owner who finds two `.exe` files; the Terminal user
   who runs `mailarchive desktop` and gets a dialog *and* stderr; the inheritor.
5. **Integration / seams:** build order (§11); the capture slot shared by two kinds of run; the
   picker variant and the workspace's rules; the pre-sync design's job field; MA-529, X9, X11
   edits; the CLI `TestMain` building a bigger program.
6. **Security:** the planted sibling; the forged descriptor against the name rule; the
   no-argument rule as a launch vector; SE3's endpoints; `isOurOwnProgram`'s widening.
7. **Failure domains:** the pair out of step (one file replaced, not the other); a copy deleted
   by an owner "cleaning up"; the link broken by a copy of the `.app` through a tool that drops
   symlinks (zip).
8. **Migration:** the N / N+1 story; rollback; mixed versions in one account; RMM scripts.
9. **Dependency and cost:** two builds per release; four Windows files on disk for one release;
   `zenity` linked into the CLI file (it is already linked into the desktop; the CLI file grows).
10. **Honesty:** every "conditional" names its lab item; §12 beside the claims.

The rescue pass should be handed, in particular: the Windows console constraint (§1.6) and
whether a third option exists that the design rejected too fast; and the copies versus a
first-launch repair.

## 15. Review record

Empty until `docs/review-single-executable-predesign.md` is filed; the conditions it imposes are
folded here as revision 2 and listed in this section.
