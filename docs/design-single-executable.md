# Design — one program: `mailarchive` as the single executable behind the command line, the wizard and MailArchive Desktop

**Revision:** 4 (2026-10-17): the owner's Q1..Q21 answers (rulings 13.44) and the SC53 human sign-off (13.45) folded. (Revision 3 (2026-10-16) folded the SC53 re-run dispositions; revision 2 folded the conditions SC1..SC53 of the pre-code review; §15.)
**BUILD STATUS:** reviewed (GO WITH CONDITIONS, bounded), conditions folded. Approved scope: slices
SE0, SE1, SE2 and SE4, once the stale lenses are re-run (§14, SC53). SE3 is not approved; this design is written for SE3 to get its own design and pre-code review, pending the operator's OP-4 (§4.8,
SC39). The macOS flip slice is gated on its own
labs, walk and owner answer (§11, SC19). Not built, not lab-validated. The 10-lens pre-code review
is filed as `docs/review-single-executable-predesign.md` (verdict GO WITH CONDITIONS, bounded,
reviewed against revision 1, sha256
`ee4b8fa41c245734a02fa49e19d3cb82bd0b9d0947671d35ee162f445891df18`). The items still OPEN for the
owner, the operator and the re-review are listed in §15 (SC53).
Nothing here is lab-validated: every Windows, macOS and Linux behaviour this text states from
memory of vendor documentation, and not from this repository, carries the words "lab item" and an
id (L-W*, L-M*, L-L*, L-C*), and is verified on a real machine before the slice that depends on
it ships (§9.3).

Base: branch `fix/review-2026-10` at `b1a16bc`. This document changes no code, no test and no
catalog row. Every id it proposes (R33, S59, X17, MA-610..MA-647, and the lab ids and question
numbers) is provisional until the OPERATOR accepts it into `docs/scenario-catalog.md` and
`docs/ux-contract.md` with the slice's catalog-first PR (`docs/owner-rulings-2026-10-09.md` §8).
**Id allocation (SC49):** this design's MA block starts after the end of the Thunderbird pre-sync
design's provisional block as shifted at its TP0, never at MA-580. That block (MA-549..MA-579 in
its revision 2, 31 ids) collides with MA-549, now in the catalog for the in-app Microsoft 365
configuration, so its shift ends it on MA-580 at least, and this design cannot claim MA-580.
Revision 2 proposes the provisional block MA-610..MA-647; revision 1's MA-580..MA-609 are
withdrawn, and its SE3 rows (MA-594..MA-602) leave this design with SE3 (§4.8, SC39). The
OPERATOR allocates, in one pass at SE0 (OP-6), every id of both designs' blocks and every row, lab
item and question this review adds, with the operator's log entry; until then every id is
provisional and no catalog-first PR lands. The one exception is OP-1..OP-3: the operator allocates
those three rows' ids when accepting them now, ahead of the SE0 pass; MA-645..MA-647 are
placeholders for them, and the SE0 pass allocates the rest of the block around them. The range in
this header equals §9.2's, §10's and §11's. No id a rescue named stands as named (the rescues' MA-607, MA-608 and L-L3 are
re-allocated with the rest). R32, S58 and X16 are the highest ids any design has claimed;
**re-check R33, S59, X17 and the MA, L- and Q ids against the operator's log at SE0** (SC49).

**Owner request (2026-10-15), verbatim:** "Review the code in this repository and recommend ways
to integrate wizard and the management interface into a single executable that can provide all
the diverse functions", then "lets do the design and review".

Gates before code:
- **SG1** the pre-code design review (`docs/review-single-executable-predesign.md`), lenses per
  §14, with the rescue pass. **Met, bounded: filed against revision 1, GO WITH CONDITIONS,
  bounded; conditions SC1..SC53 folded here (§15).** The re-run of lenses 3, 4, 6, 7, 8 and 10 on
  the changed sections, with a second reviewer reading the critic's six re-check items, is owed
  before SE0's catalog-first PR (§14). If revision 2 cannot state SC1 and SC2 as one mechanism
  each, the verdict becomes NO_GO on lenses 3, 4, 7 and 8 (SC53).
- **SG2** owner answers to the questions in §13 that block a slice (each has a recommended
  default and a safe interim that weakens no invariant or gate, so no slice of SE0..SE2 is held
  from being built by an unanswered question; §13 says which slice each blocks), and the operator's
  items OP-1..OP-7 (§13). One question holds a release: under Q18 (a)'s interim SE1's text and
  packaging commits wait, and so release N waits, because MA-376's and MA-546's covering tests pin
  the old text.
- **SG3** lab items, per OS, each before the slice it names ships (§9.3):
  - Windows: L-W1..L-W5, L-W7..L-W10 and L-W14 before SE1+SE2 ship for Windows, with L-W2
    extended to the wrappers running the GUI build and to the console build started directly by
    Task Scheduler, and L-W4 extended to `GetConsoleProcessList` (SC16, SC17, SC22, SC42). L-W1 is
    informational. L-W6 holds that release only if Q10 is answered yes (SC31). L-W11 and L-W12
    before SE4 ships (SC37, SC38). L-W13 only on a Q11 yes. The Windows items run in
    `packaging/mailarchive-desktop/README-RMM.md` §D's hand-validation round on the MSI builder's
    Windows machine, and their results are recorded in a named file (SC29).
  - macOS: L-M1 (first and second open), L-M2 (extended to what `os.Executable` returns through
    the link), L-M3 and L-M4 before SE1+SE2 ship the `.app` (SC19, SC42). L-M5, L-M6 and L-M7
    before the macOS flip slice, and not before SE1 (SC19).
  - Linux: L-L1..L-L2 before the README's Linux text changes; L-L1 in both directions, on the
    architectures it names, before the Linux no-argument row ships (SC16). L-L3 only on a Q10 yes
    for Linux. L-L4 before §7.3's README step ships.
  - L-C1 before a disk figure is printed in the README or the release notes (SC24).
  - **The tag rule (SC29):** no `v*` tag is pushed from a main containing SE1 until §D's round has
    recorded the Windows items. Because release.yml publishes `dist/*` on any `v*` tag, this also
    holds that tag's macOS and Linux release. Shipping SE1 for Windows with any item unrecorded
    relaxes SG3 and is the owner's decision (Q15), never a default.
- **SG4** this design touches other designs only at named seams: the Thunderbird pre-sync
  design's job-file field (§4.2.5 there) rides along unchanged in `internal/job`; and
  design-local-confidentiality §4.C4 step 2 [CC22], which §4.5.2 amends only on the owner's yes to
  Q10, with today's exact-path test as the interim (SC31). The extraction workspace's picker seam
  (`internal/desktop/picker.go`) is not touched by this design; a file variant belongs to the SE3
  design (§4.8, SC39, SC40 R3). The desktop design (`design-mailarchive-desktop.md`) P1, "one
  binary, in-process", is what this design completes: it never asked for three binaries, and it
  forbade a child of the program ("No `mailarchive.exe` child", with desktop Q14/OR8), not every
  child process. The OS tools the program starts (the scheduler, the opener, the clipboard tools,
  `zenity`) are as today, and MA-612 names them (SC52).

**Claim labels used throughout.** **proven** = a tier-U/S row in §10 asserts it on every commit
once its slice is built (nothing is built, so read "proven" as "will be proven by MA-6xx").
**conditional** = holds only under the condition written beside the claim. **aspirational** =
intended, not mechanically assured. **lab item** = depends on how a real OS, installer or desktop
behaves, and is not assumed. **residual** = a gap this design names and does not close; under
the rulings log's rule 3 it is an open condition, not accepted, until the owner accepts it, and §15
lists each one. A friction entry marked **Type II** is a design choice whose cost is stated
(assurance-kit `process/friction-review.md`); where it also leaves a gap, §15 lists it as a
residual.

**Vocabulary.** *The program*: the one executable, `mailarchive` (`mailarchive.exe` on Windows).
*Surface*: one of the three ways the owner meets the program today: the **command line** (the
verbs of `cmd/mailarchive`), the **wizard** (today `mailarchive-gui`: native dialogs, local mail
sources) and **MailArchive Desktop** (today `mailarchive-desktop`: the loopback dashboard with the
embedded reader and the Extraction workspace). *Verb*: the first argument that selects a surface
or an action (`serve`, `status`, and new: `desktop`, `wizard`, `run-job`). *The pair*: on Windows,
the two files built from the same package, `mailarchive.exe` (console subsystem) and
`mailarchivew.exe` (GUI subsystem), identical in code. *Former name*: `mailarchive-gui` and
`mailarchive-desktop`, the file names that stop shipping as programs of their own; until the
floor they ship only as copies of, or a link to, the program (§7.1) (SC37). *Headless run*: a run
a scheduler starts, with no dialog, no console and no browser.
*Kind*: what a schedule runs, named from its job's leading arguments against one fixed set, never
as a path: `verify`; `run-job` (`run-job`, legacy `-job`); `capture` (`desktop -capture`, legacy
`-capture`); `export` (any other non-empty job); `none` when no descriptor is recorded and
`unknown` when one is present but unreadable (§4.6.5) (SC1). *Install names*: `mailarchive.exe`
and `mailarchivew.exe`, the names the MSI payload and the Windows release zip give the pair
(§4.10) (SC11, SC12). *Release N*: the first minor tag (vX.Y.0) that ships SE1 and SE2 together,
with the former-name copies and the `.app` link; patch tags do not count (§7.1) (SC37, SC42). *The
floor*: how long the copies and the link stay, a minimum number of minor versions after release N
and a minimum time after its publication, whichever is later, set by the owner in Q9; until it is
answered they stay in every tag and SE4 does not ship (§7.1) (SC37).

## 1. Problem

1. **Three executables, one module, one engine.** `cmd/mailarchive`, `cmd/mailarchive-gui` and
   `cmd/mailarchive-desktop` all call the same `internal/` packages (`app`, `export`, `health`,
   `schedule`, `state`, `index`, `server`, `desktop`). Each is 23 to 31 MB; each Windows release
   carries all three, and the MSI payload is three copies of one engine (`packaging/build-release.sh`,
   `packaging/mailarchive-desktop/Product.wxs`). Release N makes this four Windows files, about
   one file and 40 MB more than today, until the floor; the saving, about one file and 20 MB less
   than today, arrives with the first release past the floor (SE4), and L-C1 measures each figure
   (§7.1) (SC24, SC37).
2. **The surfaces cannot reach each other's functions.** The dashboard can read, search, check and
   export an archive made from local mail files, but it cannot add mail to it: for a Thunderbird
   archive it renders steps that send the owner to the wizard or the command line (MA-546,
   `internal/desktop/localarchive.go`, `AddMailArgv`). The wizard cannot capture Microsoft 365.
   The command line has every function, and no owner of the double-click kind ever meets it.
   The dashboard has never shipped on macOS or linux/arm64: `packaging/macos/build-app.sh` builds
   only `./cmd/mailarchive-gui` and `./cmd/mailarchive` for darwin, and `build-release.sh` builds
   `./cmd/mailarchive-desktop` only for linux/amd64 and windows/amd64 (and the wizard only for
   those two), so SE1 is the first release to carry the dashboard and its `desktop -capture`
   headless mode there (SC19). This design makes every function a verb of one program (P1); the
   dashboard's adding of local mail is the SE3 design's (§4.8) (SC39).
3. **Three scheduled-run shapes, three repair paths.** Every surface records *its own file* as the
   program a schedule runs: the command line records `<exe> <verb flags> -log FILE`
   (`runSchedule` in `cmd/mailarchive/main.go`), the wizard `<exe> -job FILE` (`offerSchedule`,
   `repairSpec`), the desktop `<exe> -capture -config … -out … -log …` (`backupSpecFor` in
   `internal/desktop/schedule.go`). `health.exeIdentity` compares the recorded file with the
   running one, so the dashboard's `status` view of a wizard-made schedule reads
   `exe_moved` ("not this binary") after any upgrade that replaces the recorded file (the wizard
   then says "this" through `sameFile` while the dashboard says `exe_moved`, because the size and
   mtime fallback compares the recorded file with what was recorded at install, not with the
   running program), or for a descriptor written without `ExeSize`/`ExeMTime` (SC12).
4. **The wizard is the one surface whose logic lives in `cmd/`.** About 1,500 lines of
   `cmd/mailarchive-gui/main.go`, 83 direct dialog calls, 21 tests of its pure decision functions.
   The desktop's launcher is already injectable (`launchEnv` in `cmd/mailarchive-desktop/launch.go`)
   and its substance is in `internal/desktop`.
5. **Three per-user state helpers.** Job files under `os.UserConfigDir()/mailarchive` (`job.Dir`);
   the instance lock under `%LOCALAPPDATA%\MailArchiveDesktop` on Windows and
   `os.UserConfigDir()/mailarchive` elsewhere (`instance.Dir`), so on macOS and Linux it shares
   `job.Dir`'s directory; the workspace's run records under `desktop.StateDir`, which is
   `%LOCALAPPDATA%\MailArchive` on Windows, `~/Library/Application Support/MailArchive` on macOS,
   and `$XDG_STATE_HOME/mailarchive` (else `~/.local/state/mailarchive`) on Linux, per machine and
   non-roaming by XC3. Where new per-user state belongs is OPEN-X1, the operator's (OP-7); this
   design adds no state location and does not depend on the answer (§4.9) (SC51).
6. **The one real constraint is the Windows subsystem.** A Windows executable is a console
   program or a GUI program. The wizard and the desktop are built `-H=windowsgui` so that a
   shortcut click opens no console window (desktop review R1), and so that the wizard's 02:00 run
   opens none when its run string fits `schedule.SchtasksRunLimit` (design-schedule-v2 SC12); the
   desktop's schedule always runs through a `.cmd` wrapper and shows cmd.exe's brief console
   today (SC22). The command line needs a console. Any single-file answer on Windows is one of:
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
  built from one package in one `go build` matrix and differ in linker flags alone (MA-614 parses
  the gated builders; MA-628 builds the pair and asserts Subsystem 3 and 2 and equal build info
  but `-ldflags`) (SC23). Proven (MA-610, MA-611, MA-612 S+U: every process-start site is a named
  OS tool, MA-613) (SC52). Open condition, beside the claim: a program path reaching an
  allowlisted opener through a variable is not traced (§12) (SC52). Proposes **R33**.
- **P2 — Each surface behaves as it does today.** SE1 adds a dispatcher and moves code, and SE2
  changes the schedule mechanisms; together they change no refusal text, no exit code, no route,
  no guard, no file format and no argument of any existing surface, except these, each its own
  commit (after SE1's pure move) with its own row (SC43):
  - (a) `-job FILE` with exactly two arguments, and a `-capture`-first invocation, are refused by
    the export verb today and now run `run-job` and `desktop -capture`, under the program's own
    names only (§4.1, MA-611) (SC15, SC43);
  - (b) the wizard verb's errors and recovered panics also reach stderr, and on Linux and the BSDs
    a missing dialog tool or display is refused before the first dialog (§4.4, MA-639) (SC44);
  - (c) `run-job` reads the job file through `util.ReadSecureFile` (§4.7, MA-640); `job.Write` has
    created every job file `0600` since da05d89, so no job file this program wrote is refused on
    Unix (SC45);
  - (d) the program's name in printed text (P9);
  - (e) only on the owner's yes to Q16, the wizard records `run-job FILE` in place of `-job FILE`,
    and the `-job` assertions in `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` change;
    the interim is `-job FILE` (SC42, SC43);
  - from SE2, (f) `schedule -install`, the wizard's offer and the Weekly backup card's Install
    refuse a cross-kind install under the archive's name (§4.6.5, MA-631); SE1 changes none of
    them (SC1);
  - (g) `instance.Info` gains two optional fields, `version` (SC33) and `ended` (SC32), and the
    `former` field gains the kind `ended`; these are the only additive format changes, and an
    older reader ignores all three (§4.5.3, MA-637, MA-638);
  - (h) `mailarchive.exe`, double-clicked as the only process in its console, prints a pointer to
    the file to double-click and waits for Enter; its refusal text and exit code are unchanged
    (§4.3, MA-620) (SC17);
  - (i) the headless run's rebuild action and the three wizard texts that sent the owner to "this
    program" name the wizard by its click route (§4.4, MA-622) (SC21);
  - (j) only on the owner's yes to Q10, §4.5.2's narrowed "our own program" test, an amendment of
    CC22; the interim is today's test (MA-619) (SC31);
  - from SE2, (k) the other install and repair changes of §4.6: every install and repair refuses a
    translocated program (§4.6.1, D1, MA-630); a capture install or repair refuses with
    `HeadlessCapture`'s not-configured text before `schedule.Install` when Microsoft 365 is not set
    up (§4.6.3, MA-617); a capture or export install deletes the name's job file, and the card's
    Remove of a run-job deletes the job file (§4.6.5 (d), §4.6.3, MA-617, MA-631); and
    `backupSpecFor` records `Args` led by the `desktop` verb (§4.6.4) (SC1, SC8, SC42);
  - from SE2, (l) the repair offer is gated by the schedule's kind, and the wizard's repair is not
    offered, and installs nothing, for a capture descriptor or a run-job descriptor with no job
    file (§4.6.5 (e), MA-617, MA-618); every repair asks the volatile-directory question first
    (§4.6.3, MA-618); `status -json` gains the additive reason codes `start_failed` and
    `exe_version_skew` (§4.6.6, §4.6.2, MA-634, MA-616); and the `Program:` line, the identity
    notes and the per-kind remedy texts change (§6, MA-629, MA-633, MA-635) (SC1, SC2, SC3, SC4,
    SC9, SC10, SC25).

  Under a former name every argument list behaves as at `b1a16bc` (§4.1 rule 6) (SC15). Every MA
  row that proves a surface today keeps proving it, pointed at the moved code.
  **Conditional:** "today" means the behaviour at `b1a16bc`; a surface's own later changes are
  its own. The proof is split (SC43). Refusals, exit codes, routes, guards, formats and the
  wizard's decision functions are proven by the existing rows through the covers map (the
  meta-test fails if a moved test loses its marker), MA-613 (the structural walk of the verbs and
  their flags) and each exception's row. The wizard's dialog sequence, items, defaults, titles and
  texts are unchanged code by the recorded `git diff -M b1a16bc` of SE1's first commit, and are
  lab (MA-95, pending), walked by hand in SE1's friction review.
- **P3 — No console where none is shown today; a console where one is expected.** On Windows a
  shortcut and a double-click start `mailarchivew.exe` and show no console. A scheduled run of a
  schedule installed or repaired after SE2 starts `mailarchivew.exe` (P4; SE1 and SE2 ship
  together, §11) (SC42), and shows no console only when it is installed without a wrapper (§4.2);
  the desktop's and the command line's schedules keep today's `.cmd` wrapper and its brief
  console (non-goal 7) (SC22). `mailarchive.exe` from a terminal prints to that terminal and the
  shell waits for it, as today.
  **Conditional (lab L-W2; L-W1 is informational) (SC23):** that the GUI-subsystem build of this
  package shows no console under Task Scheduler for an interactive task, and that a `.cmd`
  wrapper (which the desktop's `backupSpecFor` and the command line's `schedule` still always
  write on Windows, §3 non-goal 7) shows the brief console it shows today, are facts about
  Windows, stated from the repository's own existing practice, not re-derived here (SC22).
  **Conditional** also on the pair being installed together under its install names (§4.10)
  (SC12). L-W2 also checks that the console-subsystem file started directly by Task Scheduler
  shows a console for the whole run (SC12), and that both wrappers running `mailarchivew.exe`
  wait for it, write its stderr to `<name>.stderr.log` and pass its exit code; if any of the three
  fails, SE2 does not record `mailarchivew.exe` inside a wrapper (§4.6.1) (SC22). Proven (MA-614
  for the gated build lines and the shortcut arguments; MA-628 for Subsystem 3 and 2; the console
  behaviour itself is L-W2) (SC23).
- **P4 — A schedule names one program, and `status` knows its own names.** On Windows every
  schedule any surface installs after SE2 records `mailarchivew.exe` when it is beside the running
  program under that name, with the same owner and the same build (§4.6.0). Otherwise it records
  the running file, the install says so, and `status` names it (SC7, SC8, SC12). For a schedule
  installed with a `.cmd` wrapper (the desktop's and the command line's, always), it records
  `mailarchivew.exe` only if L-W2's three wrapper checks pass; otherwise the wrapper records the
  console build (§4.6.1) (SC22). Elsewhere, the
  running file. `health.exeIdentity` treats a known name in the program's own directory (the pair,
  and until the copies' floor (§7.1) the two former names) as *this program* only when that file
  passes the same test, and a former name then carries the `exe_former_name` note; a file there
  that fails it reads `exe_version_skew` or `exe_moved`, and outside the program's directory the
  size and mtime fallback keeps today's answer under its true meaning, "unchanged since install,
  not compared with this program" (§4.6.2, D4) (SC2, SC7, SC37). `status`, the dashboard's
  health view and the wizard's health check agree whichever of the program's files in one install
  directory the owner started; a second copy in another directory is another install (SC12).
  Each view assesses one archive: `status -out` the archive it names, the dashboard its configured
  archive, and the wizard the archive it used last (SC5). Proven (MA-615, MA-616) for the
  program's own directory. Serves R18 (every run and schedule is legible) and X6, X10.
- **P5 — The upgrade is seamless where a former file can stay, and discoverable, never wrong,
  where it cannot (SC41).** A descriptor that names a former name is detected by the health view
  of the schedule's own archive (SC5). Where the former file still exists (the Windows MSI keeps
  the two former names as copies of `mailarchivew.exe` until the copies' floor, §7.1; the macOS
  `.app` keeps `mailarchive-gui` as a link to `mailarchive` until the floor) the schedule keeps
  running and `status` carries a *note* naming the repair the installing surface offers (SC3,
  SC37). Where it does not exist (Linux, a hand-copied install, and any schedule still on a former
  name after SE4 on any OS) the schedule stops on the first run after the former file goes.
  Nothing the product owns reports this, because the program never starts: no last-run record,
  no `.jobfail.log`, no notification. The stop is discoverable (RED `exe_missing` in `status`,
  the wizard's health check and the dashboard's health view, each for the schedule's own archive;
  cron's mail where the machine delivers it, lab L-L4), not disclosed (design-schedule-v2 §2),
  and the backup stays stopped until it is repaired; the RED's remedy names the repair the
  installing surface offers (SC41, SC3, SC5, SC38). No push tells the owner unless the owner rules
  for a notification (Q11); until then the break is pull-only (SC38). "Never wrong" rests on the
  schedule's kind (§4.6.5): a repair rebuilds the schedule's own kind or refuses, and never
  converts it (SC1). No schedule is rewritten without the owner's action. Proven (MA-617, MA-618,
  MA-633). Lab (L-W3, L-M2, L-L4).
- **P6 — The per-account instance rules are unchanged, except §4.5.2, an amendment of CC22
  pending Q10 (SC31).** The `desktop` verb keeps design-local-confidentiality P8, CC21, CC23 and
  CC22's condition ("ends a same-account old process or tells the user to sign out") unchanged:
  one instance per account per configuration, the lock, the `0600` instance file, the
  challenge-response handoff, the probe of the former address 127.0.0.1:8097, and the visible
  refusals. The one mechanism change, subject to the owner's answer to Q10, is §4.5.2's
  acceptance of the former name `mailarchive-desktop` in the running file's directory, run by the
  same token user, for a listener `probeFormer` classes as `FormerPreUpgrade`, on Windows and
  Linux; it is labelled an amendment of design-local-confidentiality §4.C4 step 2 [CC22], and
  until the SE1 amendment lands (Q10 answered yes, 13.44, owing its adversarial pass) both OSes keep today's exact-path test (SC31). An older instance the
  program ends is named in the new dashboard's header and in `status`, through the instance
  file's optional `ended` field (SC32). The instance file also gains an optional `version`
  field, and a handoff to a holder of another version names that holder (§4.5.3) (SC33). A `desktop` started from `mailarchive.exe` and one from
  `mailarchivew.exe` are the same program to the lock. Proven (the existing MA-376, MA-281 rows
  moved; MA-637, MA-638; MA-619 only on a Q10 yes).
- **P7 — A double-click goes somewhere visible.** The rule is §4.3's, consulted only under the
  program's own names with an empty argument list (SC16):
  - Windows: a launch with no console and no argument opens MailArchive Desktop (the dashboard
    verb's existing browser-open or link dialog, X11) under any name but a former one, a renamed
    or suffixed copy included (SC16). A former name hands its argument list to its own surface
    (§4.1 rule 6), so `mailarchive-gui.exe` opens the wizard and `mailarchive-desktop.exe` the
    dashboard, as at `b1a16bc` (SC15). `mailarchive.exe` double-clicked, the only process in its
    console, names the file to double-click, prints today's usage refusal and waits for Enter
    before exiting with today's code; it never starts that file (SC17).
  - macOS: `Mail Archive.app` started with no argument and no terminal opens the wizard, as today
    (Q2-mac's interim), until the gated macOS flip slice (§11). The dashboard on macOS is
    `mailarchive desktop` from Terminal, which no release before SE1 carried there and which is
    not lab-run on macOS (SC19).
  - Linux: the row ships once Q4's honour answer (13.44) and L-L1 in both directions are in; until L-L1 is
    recorded an empty argument list gives today's usage refusal. With the row, a launch with no
    terminal on any stream, no controlling terminal and a display opens the wizard. The slice that
    would make the Linux verb `desktop`, owed with the SE3 design, asks "Open MailArchive
    Desktop?" first (SC16, SC18).
  - On every OS, a no-argument start with a terminal on any stream, or on Windows a console it
    shares with a shell, prints today's usage refusal.
  - No launch starts a server the owner did not name or confirm (X11).

  Proven (MA-620). **Conditional (lab L-W4 extended to `GetConsoleProcessList`, L-M1 first and
  second open, L-M2, L-L1 both directions)** on each OS's double-click delivering the environment
  the rule reads; if L-W4 shows the console process count is unreliable, the console build's case
  collapses into today's refusal and §12 states the flash (SC16, SC17). Residuals, stated:
  - a second open of the `.app` while it runs activates it and starts nothing, as today (SC19);
  - once the Linux row exists, an unattended Linux graphical start with no verb (a
    `systemd --user` unit, a crontab exporting `DISPLAY`) shows the wizard's first dialog, or from
    the slice that makes the verb `desktop` the question, and never starts a server (SC18);
  - a Linux box without `zenity` prints the usage where nobody sees it (SC18);
  - a mistyped flag on a shortcut or a by-hand task under a GUI-subsystem file is refused on
    stderr only, as at `b1a16bc` (SC15).

  Open condition (Q14 (b), the owner's): on Linux with neither a dialog tool nor a browser opener,
  what `mailarchive desktop` shows is CC20 as written; whether it instead refuses on stderr naming
  what to install is Q14 (SC26).
- **P8 — SE1 and SE2 add no per-user state location; existing files do not move; Q19's migration inventory is the one new per-user state item, arriving with SE4 (SC51).** SE1
  and SE2 write only where `job.Dir`, `instance.Dir` and `desktop.StateDir` already write (§1.5,
  §4.9) (SC39). Job files, instance files, descriptors and workspace records stay where they are,
  because schedules and running instances name them by absolute path. OPEN-X1 stays the
  operator's (OP-7); this design neither answers it nor depends on the answer. Proven (MA-621, S:
  the set of per-user state call sites is unchanged) (SC51).
- **P9 — No owner-facing text names a program that does not ship.** The swept set is enumerated
  (MA-622, X17): README, the dashboard's add-mail steps (MA-546's `AddMailArgv` text), the health
  remedies, the wizard's summary and its texts, `addMailHelp`, the MSI's shortcuts, `status`, and
  the administrators' texts `packaging/mailarchive-desktop/README-RMM.md`, `DEPLOYMENT.txt`,
  `README-payload.txt`, `BUNDLE-CONTENTS.txt` and `SOURCE-NOTICE.txt`, `README-macOS.txt` and
  `packaging/release-notes.md` (SC28, SC47). They name `mailarchive` and its verbs. A
  former name may appear only on a line that also says "former name", never as the thing to run,
  download or build; the release-N machine scripts (Product.wxs's copy components and
  `CloseApplication`, the RMM scripts' stop lists) are outside the sweep and pinned by MA-376 and
  MA-614. The phrase rule is the only exemption for text; X17's matcher also excludes two
  non-program tokens (§9.2) as part of X17's proposed definition, which the operator accepts or
  rejects at SE0 (OP-6); there is no other skip, allowlist or suppression (SC47). No text says which surface a
  no-argument launch opens ("the window that opens with this program", "open Mail Archive",
  "re-open this program") (SC21). Proven (MA-622: a structural sweep of the enumerated set).
- **P10 — Rollback reads every file; on the MSI it is uninstall, then install (SC36).** The older
  three executables read every file SE1 and SE2 write: they write no new format (the job file
  stays version 1; the descriptor gains no field), except the optional `version` and `ended`
  fields of `instance.Info` and the `former` kind `ended`, which an older launcher still reads
  because every reader since LC-C2 decodes it non-strictly (SC32, SC33, SC39). On Windows the MSI refuses a downgrade, so rollback is `msiexec /x`
  of release N, or Uninstall-RMM.ps1, followed by the N-1 MSI (§7.4) (SC36). A schedule repaired
  to the new name stops running after a rollback that removes the new-name file (the MSI's
  uninstall, then install), and `status` says so (RED `exe_missing`). After a hand swap that leaves
  the new-name file, that file keeps running the schedule, and a pre-SE2 `status` reads it as
  this program: silent while it is present and unchanged, RED `exe_missing` once it is removed,
  WARN `exe_moved` only if it is rewritten in place. The release note's hand-rollback order is:
  delete `mailarchivew.exe` (and `mailarchive` where it replaced nothing), then rebuild the
  schedule with the old program's own repair for its kind (§7.4) (SC13, SC3). "Loud" here means
  RED in every health view, not a push (SC41), and the RED is the cost of rolling back, as
  design-thunderbird-presync P7 states for its own case. Proven (MA-623) for the files;
  conditional on the rollback lab item L-W10 for the installed product (SC36).
- **P11 — Moved to the SE3 design (SC39).** The dashboard's adding of local mail (SE3) is not part
  of this design. §4.8 is an informative stub, not approved and not reviewed, that states the
  intent, the four seams kept here and the requirements R1..R8 that design must meet (SC40). This
  follows the operator's call OP-4; if the operator keeps SE3 here, SC40's requirements become
  §4.8's text and lenses 2, 4, 5, 6 and 7 are re-run on it.

## 3. Non-goals (explicit scope cuts)

1. **No new capture source and no change to any reader.** PST, mbox, maildir, Evolution, Graph:
   unchanged. The Outlook-app export (`outlookcom.CreatePSTs`, COM) is not offered from the
   dashboard; the dashboard's no-COM rule is a requirement of the SE3 design (§4.8, SC40 R2), and
   whether that design offers the export is its own question (SC39).
2. **No rewrite of the wizard as web pages.** The dialog wizard stays, as the `wizard` verb, for
   the owner with no browser, the Outlook-app path and the Thunderbird IMAP preparation
   (`prepareThunderbirdGUI`, which edits `prefs.js` with Thunderbird closed and waits). Whether a
   dashboard page edits `prefs.js` is the SE3 design's question (§4.8) (SC39).
3. **No change to the dashboard's security boundary, and no new dashboard route.** X9, X12, X13,
   X15, the session prefix, the CSRF/Origin/Host guard, the loopback bind: untouched (SC39).
4. **No moving of existing state files** (P8). OPEN-X1 is not answered here; it stays the
   operator's (OP-7) (SC51).
5. **No signing or notarization for macOS and Linux, and no installer for Linux.** The macOS
   `.app` stays unsigned; Linux stays a bare file plus README text. Whether the Windows pair and
   the MSI are Authenticode-signed is the owner's Q13; the interim is unsigned, as today (SC25).
6. **No `AttachConsole`, no `FreeConsole`, no PE-header patching at build time.** The pair is two
   ordinary `go build` invocations (§4.2). A later design may collapse them if a lab shows a
   byte-identical pair is reachable; this one does not depend on it.
7. **No change to what a schedule's wrapper does.** The command line keeps writing its `.cmd`
   wrapper on Windows (always), the wizard only when the run string exceeds
   `schedule.SchtasksRunLimit`, the desktop always; those are three recorded choices of three
   designs and are not re-opened here (Q3 records the one the owner may want to change). The
   desktop's always-on wrapper stays (D2, a delegated-recommended default the owner may
   overrule) and is asked again after L-W2. Any later question to drop it carries SE-C06's
   precondition: `desktop -capture` logs through `io.MultiWriter(stderr, file)` and prints its
   "not configured" refusal before any logger exists, so a capture without the wrapper needs its
   own log writer first (SC22).
8. **No merge of the Microsoft 365 capture into the job file.** `desktop -capture …` keeps its
   argument form; `run-job` runs the wizard's job file only (§4.7). A mixed archive keeps one
   schedule of one kind, and every surface refuses to install the other kind over it (§4.6.5)
   (SC1).
9. **No removal of the former-name copies, or of the `.app` link, before the floor** (§7.1). They
   stay in every tag until the floor the owner sets in Q9 has passed, and go with SE4, by a
   release note, with `status` RED, not pushed, about any descriptor that still names them
   (SC37, SC38).
10. **No new dashboard endpoint in this design, and none without the dashboard's existing
    guard.** The SE3 design's discovery, with its scoped listing claim, is among §4.8's
    requirements (SC40 R6, R7) (SC39).
11. **No dashboard page that adds local mail.** SE3 is its own design, with its own 10-lens
    pre-code review once SE1 and SE2 exist as code (§4.8); no SE1 or SE2 owner text names "Add
    mail from this computer" (SC39).

## 4. Mechanism

### 4.1 The dispatcher (SE1)

`main` in `cmd/mailarchive` keeps its first-argument switch and gains three verbs and two legacy
spellings, and one rule above the switch for a start under a former name (rule 6) (SC15):

| First argument | Runs | Today's equivalent |
|---|---|---|
| any, when the program is started under a former name (`invokedBase` begins `mailarchive-desktop` or `mailarchive-gui`, case-insensitive, without `.exe`) | the whole argument list goes to that name's surface, never to the switch (rule 6) | the file of that name at `b1a16bc` (SC15) |
| `desktop [flags]` | `desktop.Launch(ctx, args, desktop.OSEnv())` (today's `launch`) | `mailarchive-desktop [flags]` |
| `wizard` | `wizard.Run(ctx)` (today's `wizard()`; its stderr output and preflight are §4.4's) | `mailarchive-gui` (SC43, SC44) |
| `run-job FILE` | `wizard.RunHeadless(FILE)` (today's `runHeadless`) | `mailarchive-gui -job FILE` |
| `-job FILE` (legacy, exactly two arguments) | `run-job FILE` | the installed wizard schedules |
| `-capture …` (legacy, first argument) | `desktop -capture …` | the installed desktop schedules |
| no argument | §4.3 | — |
| anything else | today's `runExport(args)` | `mailarchive [flags]` |

*The program's own names*, in this section and in §4.3, are every invoked base name that is not a
former name: `mailarchive` and `mailarchivew`, and also a release's suffixed name or a renamed
copy, which the rules treat as the program (§4.3 pins both cases in MA-620) (SC15, SC16).

Rules:
1. Under the program's own names (`mailarchive`, `mailarchivew`), the two legacy spellings are
   recognised **only** as written above (`-job` with exactly one following argument; `-capture`
   as the first argument). Every other flag-first invocation is `runExport`, as today, so
   `mailarchive -out X -input Y` is unchanged and a typo is refused by the export verb's parser as
   today (X3). Under a former name rule 6 decides instead (SC15; MA-611).
2. The legacy spellings are kept for as long as a descriptor may name them: indefinitely. They
   cost one `switch` case each. From SE2 new installs never write the `-capture`-first spelling
   (§4.6.4); in SE1 the desktop still records what `b1a16bc` records. The wizard's recorded
   `-job FILE` stands unless the owner rules otherwise (Q16) (SC42, SC43).
3. `-h`/`-help` of the root usage lists the three new verbs; each new verb has its own `-h`
   (X3; MA-39's walk extends to them, MA-613).
4. The `wizard` verb recovers a panic into stderr and a dialog, and exits 1: it writes
   `mailarchive wizard: <text>` to stderr before it attempts the dialog, for a recovered panic and
   for its top-level error alike (§4.4) (SC44). The `desktop` verb's refusals go through
   `refuse()`, which writes stderr and shows the dialog, as today (`dialogs.go`). A refusal either
   verb has already shown carries an exported marker (an error type or sentinel in
   `internal/desktop` and in `internal/wizard`; for the desktop, today's `refusedError`); main
   checks it with `errors.As` and exits 1 without the `mailarchive:` line, as both mains do today,
   so a shown refusal reaches each channel once (SC26; MA-636). The command-line verbs keep their
   stderr refusal (X7). A flag or usage refusal from any verb under a GUI-subsystem file (the
   former-name copies, `mailarchivew.exe`) goes to stderr only, as at `b1a16bc`; no dialog is
   added (§5.4 item 6, MA-624), because `mailarchivew.exe` also runs export flags headless at
   02:00. **Residual (stated):** a mistyped or unknown flag on a shortcut or a hand-made task under
   such a file is refused where no one sees it, as both former binaries did at `b1a16bc` (SC15).
   **Correction of revision 1:** `cmd/mailarchive-desktop`'s `main` recovers no top-level panic at
   `b1a16bc` (its capture, sign-in and workspace goroutines recover their own); the `desktop` verb
   keeps that, because a new top-level recovery is a behaviour change P2 does not list.
5. Exit codes: `desktop` and `wizard` keep their 0/1; `run-job` keeps the wizard's headless 1 on
   failure; the command line's X1 map is unchanged (MA-613 asserts the set of literal codes is a
   subset of the declared map, as today). Under rule 6 a `mailarchive-gui*` start keeps the
   wizard's flag-parse exit, the `flag` package's `ExitOnError` code (2 for an unknown flag), as at
   `b1a16bc` (SC15).
6. **A former name hands its whole argument list to its own surface.** When `invokedBase` begins
   `mailarchive-desktop` or `mailarchive-gui` (case-insensitive, without `.exe`), neither the
   first-argument switch, nor rule 1, nor §4.3 is consulted. `mailarchive-desktop*` passes the
   whole argument list to `desktop.Launch` with today's `launch` flag set (its usage names
   `mailarchive desktop`, §4.5.1). `mailarchive-gui*` parses it with the wizard's one-flag (`job`)
   legacy set: any spelling of `-job F` reaches `run-job F`, no `-job` reaches `wizard`, and an
   unknown flag gets today's usage refusal and exit code. Both use Go `flag` semantics, in-process:
   no child and no sibling is started (§5.4 item 1; desktop P1 and desktop Q14/OR8 stand). So
   every argument list either file accepted at `b1a16bc` (`-copy-link`, `-no-browser`,
   `--capture`, `-config X -capture`, `-capture=true`, `-job=F`, `--job F`, and the empty list)
   does the same thing, and every list it refused is refused the same way (P2). `invokedBase` is
   the base name of the path the program was started by, before any symlink is resolved:
   `os.Executable()` unresolved on Windows and macOS (**lab item L-M2** checks macOS through the
   `/usr/local/bin` link and the `.app` link), and the base of `os.Args[0]` on Linux, where
   `os.Executable` resolves `/proc/self/exe`. It selects a surface only; it is never used for
   `ProgramForSchedule` (§4.6.1). MA-611 carries the former-name rows, each asserting that
   `runExport` is never reached, and rows showing the same lists under `mailarchive` and
   `mailarchivew` unchanged by the rule; §9.4 records the prove-fail (drop rule 6 and
   `mailarchive-desktop -copy-link` reaches the export verb's refusal). SE1's adversarial pass
   covers rule 6 with §4.3 and §4.5.2 (§11). Rule 6 retires with the Windows copies at SE4, after
   the floor (§7.1), and for `mailarchive-gui` on macOS with the `.app` link, whose lifetime Q9
   also asks (SC15, SC37).

### 4.2 The Windows pair (SE1)

Two builds of `./cmd/mailarchive` for `GOOS=windows`:

```
go build -o mailarchive.exe  ./cmd/mailarchive
go build -ldflags -H=windowsgui -o mailarchivew.exe ./cmd/mailarchive
```

Same source, same module version, same `debug.ReadBuildInfo()` except the `-ldflags` setting.
**Conditional:** `-ldflags` is recorded in build info only without `-trimpath`; the Subsystem read
is what tells the pair apart (SC23).

The gated builders are `packaging/build-release.sh` and the `Makefile`'s `build-windows`; both
build the pair and stop building the former names. `docs/continuation/build_all.py` (which passes
`-trimpath` and no `-H=windowsgui` today) and `docs/continuation/payload.py` (which builds the two
packages SE1 deletes) are ungated session helpers, not builders: from SE1 they build the Windows
files by running `make build-windows`, and until then the continuation note labels their output
ungated (SC23). The Windows release asset is one zip holding the pair under its install names, with
no suffixed bare `.exe` (§4.10; **OPEN: operator, OP-5**) (SC11). Two rows check what the builders
make (SC23):

- **MA-614** (S, every commit) parses `build-release.sh` and `build-windows`, with
  `GUI_WIN_LDFLAGS` expanded. It asserts that each has exactly two `GOOS=windows go build` lines of
  `./cmd/mailarchive`, whose tokens differ only in the `-o` name and in `-ldflags -H=windowsgui` on
  the `mailarchivew` line; that the payload copies `mailarchive.exe` and `mailarchivew.exe` under
  their install names, plus the two former-name copies of `mailarchivew.exe` for release N only;
  that `Product.wxs`'s shortcuts target `mailarchivew.exe` with the three `desktop` arguments and
  the wizard's (below); that `Build-MSI.ps1`'s `$exes` and `$hashTargets` name exactly the exes the
  payload and the payload README name; and, once the zip of install names lands, that the Windows
  release asset is that zip and no suffixed bare `.exe` is left in `dist/`, which `release.yml`
  publishes whole. MA-614 parses only; the build-info comparison is MA-628's, field by field in SE1
  and through `SameBuild` (§4.6.0) from SE2 (SC7, SC11, SC20, SC23).
- **MA-628** (U) runs inside `go test ./...`, so on every CI leg and in `release.yml`'s Test step
  before `build-release.sh`. It builds the two lines it reads from `build-release.sh` into a
  tempdir, asserts with `debug/pe` that `OptionalHeader64.Subsystem` is 3 for `mailarchive.exe`
  and 2 for `mailarchivew.exe`, and asserts with `debug/buildinfo` that path, main module, deps
  and every setting except `-ldflags` are equal. Its `go build` is a test building the program, as
  `cmd/mailarchive`'s `TestMain` already does, not the program starting itself (desktop P1,
  desktop Q14/OR8). The release environment's `GOFLAGS` and `CGO_ENABLED` apply to both lines
  equally; that is a stated residual (§12) (SC23).

**Lab item L-W1:** confirm on a Windows machine that the two files differ only in the PE optional
header's Subsystem field, the checksum and the build-info bytes; the design does not depend on it
(MA-628 proves the Subsystem), and `exeIdentity` compares the two files' embedded build info
(§4.6.0), not their bytes (§4.6.2). L-W1 stays informational (SC7, SC23).

Which file does what:

| Who starts the program | File | Why |
|---|---|---|
| Desktop, Start menu and Startup shortcuts (MSI) | `mailarchivew.exe desktop`, `… desktop -no-browser`, `… desktop -copy-link` | no console (desktop review R1, X11) |
| MailArchive wizard (MSI, Start menu) | `mailarchivew.exe wizard` | no console; the wizard's click route on Windows, the OS starting a verb (SC20) |
| A schedule installed on Windows after SE2 without a wrapper (the wizard's, when the direct run string fits `schedule.SchtasksRunLimit`) | `mailarchivew.exe -job FILE` (the recorded spelling, Q16) is the task's command, when `mailarchivew.exe` is beside the running program under that name, with the same owner and the same build (§4.6.0); otherwise the running file, with the install's note (§4.6.1) | no console at 02:00 (design-schedule-v2 SC12, conditional on the fit; L-W2) (SC7, SC12, SC22, SC42) |
| A schedule installed with a `.cmd` wrapper (the desktop's Weekly backup and the command line's `schedule`, always; the wizard's when the string does not fit) | the `.cmd` is the task's command and runs `mailarchivew.exe …` under the same condition | cmd.exe's brief console, as today (non-goal 7, D2). If L-W2 shows that a wrapper does not wait for the GUI-subsystem child, capture its stderr into `<name>.stderr.log` or pass its exit code as the task's Last Run Result, SE2 does not record `mailarchivew.exe` inside a wrapper: `ProgramForSchedule(wrapped=true)` returns the console build, whose console is cmd.exe's either way (§4.6.1) (SC22) |
| The owner in a terminal | `mailarchive.exe <verb>` | output and exit code reach the shell (X1, X8) |
| The owner in a terminal who wants the dashboard to stay open | `mailarchivew.exe desktop` | detaches, as today's desktop does. `mailarchive.exe desktop` holds the shell for the dashboard's life: when no instance was running it becomes the account's instance, so Ctrl-C or closing the window ends it, an Archive now in flight leaves RED `run_never_finished`, and later launches that handed off to it start a fresh instance. Type II, stated: non-goal 6 (no `FreeConsole`) and desktop Q14/OR8 (no self re-exec) rule out a code fix; the verb's `-h`, the README's "One program" paragraph and README-RMM §E say the same (L-W4) (SC30) |
| The owner double-clicking `mailarchive.exe` | opens a console that prints the usage refusal, names the file to double-click and waits for Enter (§4.3 row W2) | the refusal and exit code as today (SC17) |

The dialog helpers (`showRefusal`, `showLink`, the wizard's dialogs) work from either file: a
console process may show a `MessageBox`. The channel rule is design-local-confidentiality CC20 as
written: a refusal from `desktop` or `wizard` goes to a dialog and to stderr from either file, each
exactly once (§4.1 rule 4; MA-636). The desktop writes both through `dialogs.go`, as it does today;
the wizard writes stderr first and then the dialog (§4.4) (SC26, SC44). From a terminal the dialog
is a blocking duplicate of the stderr line that holds the shell until it is dismissed, and over a
remote session with no GUI the dialog call fails or waits; this is a stated Type II residual.
Showing stderr alone when the console build's stderr is a terminal amends CC20 and is the owner's
question Q14 (a), answered yes (13.44): the terminal refusal reaches stderr without the extra modal
dialog, and MA-642 is built (SC26). **Lab item L-W5:** a `desktop` or `wizard` refusal from the console build
under Windows Terminal, PowerShell and a remote session; it decides Q14's urgency and this
residual's wording (SC26).

### 4.3 What a launch with no argument does (SE1)

Under a former name §4.1 rule 6 decides every launch, including an empty argument list
(`mailarchive-desktop*` → `desktop`, `mailarchive-gui*` → `wizard`, on every OS, whatever the
console or terminal). Under the program's own names an explicit verb always wins, and this rule
runs only for an empty argument list. First match wins (SC15, SC16).

The rule is a pure function, `defaultVerb(goos, consoleAttached, consoleIsOwn, inBundle,
noTerminal, hasDisplay)`, tested in table form (MA-620). Its inputs (SC16):
- `consoleAttached`: Windows, `GetConsoleWindow() != 0`.
- `consoleIsOwn`: Windows, `GetConsoleProcessList` returns 1.
- `inBundle`: macOS, the unresolved invoked path contains `/Contents/MacOS/`.
- `noTerminal`: macOS and Linux, none of stdin, stdout and stderr is a terminal (checked through
  `internal/tty`, as `cmd/mailarchive/link.go` already does), and opening `/dev/tty` fails (opened
  and closed at once, never read).
- `hasDisplay`: Linux, `DISPLAY` or `WAYLAND_DISPLAY` present.

| Row | OS | Condition | Result | From |
|---|---|---|---|---|
| W1 | Windows | a console is attached and another process shares it (`consoleIsOwn` false: a shell, `start`, a script) | today's usage refusal, text and exit code unchanged | SE1 on |
| W2 | Windows | a console is attached and the program is its only process (`consoleIsOwn` true: Explorer made it for a double-click of `mailarchive.exe`) | today's usage refusal and exit code 1, after the X2 pointer on stderr and the wait (below); never starts the sibling | SE1 on |
| W3 | Windows | no console is attached, any of the program's own names (`mailarchivew`, a suffixed release name, a renamed copy) | `desktop` | SE1 on |
| M1 | macOS | `inBundle` and `noTerminal` (Finder, Dock, `open`) | `wizard` (Q2-mac's interim); `desktop` only from the macOS flip slice, if its gates pass and Q2-mac says the dashboard | SE1 on |
| M2 | macOS | otherwise (a terminal on any stream, including the bundle binary or a PATH symlink to it typed in Terminal, or a path outside a bundle) | today's usage refusal | SE1 on |
| L1 | Linux | `noTerminal` and `hasDisplay`; Q4 is answered **honour** (13.44), so the row ships once L-L1 has passed in both directions | `wizard` in SE1/SE2; from the slice that makes the Linux verb `desktop`, if Q2-linux says the dashboard, the launch question first (below) | L-L1, then per slice |
| L2 | Linux | otherwise (any terminal, no display, or Q4 unanswered or "require the verb") | today's usage refusal | SE1 on |

On Windows no empty-argument launch without a console reaches a stderr-only refusal. The verbs of
M1 and L1 are per-slice constants, changed only by the slice named. TERM is recorded by L-L1 and is
not an input (SC16). **Q4 is answered honour (13.44):** the L1 row ships once L-L1 passes both ways, so a
no-argument Linux launch opens the wizard; **and**, as the explicit, predictable route, the release provides a `.desktop` launcher template with an explicit verb (§7.3) the owner installs. Until L-L1 is recorded the L1 row is dormant and an
empty argument list on Linux gives today's usage refusal; the README names `mailarchive wizard`, `mailarchive
desktop`, the `.desktop` launcher and the honoured double-click (§7.3). A Linux owner who double-clicks a file
still named `mailarchive-gui` gets the wizard through rule 6 either way; the dashboard switch stays deferred to
SE3 (Q2-linux) (SC16, SC18, SC20).

**Row W2, the console build double-clicked.** Before today's refusal line the program writes an
X2 pointer to stderr: "This is the command-line program. To open MailArchive Desktop, double-click
`<sibling>` or the MailArchive Desktop shortcut. For the commands, open a terminal and run `<self>
-h`." `<sibling>` is the GUI file §4.6.1 would return as the sibling (it passes §4.6.0), under the
name it has; when there is none the text names only the shortcut. After the refusal line it writes
"Press Enter to close this window." and reads one line from stdin; EOF ends the wait, so a
redirected stdin never blocks. The refusal text and exit code are unchanged; the pointer and the
wait are the additions P2 names. The program never starts the sibling (§5.4 item 1, desktop
Q14/OR8) and never opens the dashboard from the console build: the console would stay bound to the
server, and `FreeConsole` is excluded by non-goal 6. MA-620 adds the `consoleIsOwn` rows and an
injected-reader test that the pointer and the wait happen only in this case and that the text
names the sibling, or only the shortcut when there is none. If the extended L-W4 shows the process
count is unreliable under Windows Terminal's handoff, W2 collapses into W1 (today's refusal, no
wait) and §12 states the flash as a Type II residual. SE1's friction review walks the console-build
double-click (SC17).

**Row L1, the Linux launch question.** Owed by the slice that makes the Linux no-argument verb
`desktop`, with the SE3 design (§4.8), and only if Q2-linux says the dashboard. When L1 yields
`desktop`, the no-argument path takes no lock, binds nothing and opens no browser until the owner
confirms. It shows one `zenity` Question ("Open MailArchive Desktop?", OK / Cancel) with a bounded
timeout of 60 s. On OK it runs `desktop` in-process, and the instance lock is taken only then
(desktop P1 and desktop Q14/OR8, P8 and CC21 unchanged). On Cancel, on timeout, or when no dialog
program can be shown (L-L2), it prints today's usage refusal to stderr with today's exit code, and
`instance.Acquire` is never reached. The dialog is shown for an empty argument list, not from a
verb, so §5.4 items 3 and 6 and MA-624 stand. In SE1/SE2 L1 yields `wizard`, which starts no server
and takes no lock until the owner acts, so no question is shown there. That slice's MA-620 rows use
a fake `zenity` (an external binary, argv-hygiene): OK runs `desktop`; Cancel or timeout gives the
usage refusal, with the lock file proven absent through `assure.Refused`. **Residuals (stated):** an
unattended Linux graphical start with no verb (a `systemd --user` unit, a crontab exporting
`DISPLAY`) shows the wizard's first dialog in SE1/SE2, or the question from that slice, and never
starts a server; a unit with `Restart=` repeats the question once per timeout; a box without
`zenity` prints the wizard's preflight refusal (§4.4) or the usage where no one sees it (SC18).

**Row M1 and the macOS flip slice.** The `.app`'s double-click is the wizard, and the `.app` is the
wizard's click route while M1's verb is `wizard`. Switching M1 to `desktop` is its own gated slice
after the SE3 design's slice (§11): it ships only when the reopen lab (L-M5), the macOS dashboard
lab (L-M6), a macOS walk and its friction review pass, Q2-mac says the dashboard, and another
wizard click route ships with it (L-M7). If L-M5 shows a second open of a running `.app` only
activates it, Q21 decides whether the flip ships at all. Until then the dashboard on macOS is
`mailarchive desktop` from Terminal (Q2-mac/Q21 answered 13.44: keep the wizard this release) (SC19, SC20).

The inputs are read once, injected, and never consulted again (a test passes them). MA-620 pins
each row; on Windows the suffixed (`mailarchivew-windows-amd64`), renamed (`MailArchive (1)`) and
shell-shared-console cases; on macOS the bundle path, and a PATH symlink to the bundle binary
started from Terminal (usage); on Linux a pipe, `</dev/null` from a terminal and `nohup` (usage);
and the per-slice verb, per slice. §9.4 records the prove-fail: remove `noTerminal` from the macOS
row and MA-620's Terminal row goes red (SC16). **Lab items:** L-W4 (`GetConsoleWindow` and
`GetConsoleProcessList` for each start: an Explorer double-click under conhost and under Windows
Terminal as the default terminal, cmd, PowerShell, Windows Terminal, `start`, `schtasks`), L-M1
(the `.app` whose `CFBundleExecutable` is the program: first and second open, an empty argument
list and a path under `Contents/MacOS`), L-M2 (`os.Executable` through the `/usr/local/bin` link and
the `.app` link before and after `EvalSymlinks`; a Finder launch has no terminal on any stream), L-L1
(GNOME Files and Dolphin "Run" for an ELF, both directions, amd64 and arm64; the streams, `/dev/tty`
and the environment for each launch kind) (SC16, SC17).

### 4.4 The wizard moves to `internal/wizard` (SE1)

- `cmd/mailarchive-gui/main.go` becomes `internal/wizard`: the pure decision functions
  (`keepRawApplies`, `preferOutlookApp`, `nothingFoundMessage`, `repairSpec`, `exportSummary`,
  `jobFor`, `inVolatileDir`, …) and their 21 tests move unchanged; the flow (`wizard`,
  `healthCheck`, `offerSchedule`, `runExport`, `prepareThunderbirdGUI`) moves with its 83 direct
  `zenity` calls, unchanged, as `dialogs.go` moves to `internal/desktop` (§4.5.1). There is no
  `Dialogs` interface in SE1, so §4.1's row reads `wizard.Run(ctx)` and `Notify` is no interface
  method. The move is SE1's first commit, a pure move that `git diff -M b1a16bc` verifies: the
  moved bodies are unchanged apart from the package clause, the exported entry names (`Run`,
  `RunHeadless`) and the import paths, and `cmd/mailarchive-gui` stays as a thin main that calls
  the moved code, so the tree builds and every moved test passes at that commit; the commit
  message records the diff (§11). `lastfailure.go` moves whole in that commit (SC4). The texts do
  not change in the move (P2). The wizard's dialog sequence, items, defaults, titles and texts
  are unchanged code by the recorded move diff and are lab (MA-95, pending), walked by hand in
  SE1's friction review; they are not claimed proven by a row. A later slice may introduce a
  `Dialogs` interface with a scripted fake only together with a U golden row reviewed against
  `b1a16bc`'s `cmd/mailarchive-gui/main.go`, per source type, cancel branches included. (SC43)
- Each later SE1 change to the wizard is its own commit carrying its own row, never folded into
  the move: the wizard verb's stderr output and preflight (below, MA-639), `run-job`'s job-file
  read (§4.7, MA-640), and the texts (P9 and the SC21 texts below, MA-622). (SC43)
- **The wizard verb's stderr and its preflight.** The `wizard` verb writes its top-level error
  and its recovered panic to stderr as `mailarchive wizard: <text>` before it attempts any
  dialog, on every OS and from either Windows file, in the same order as the desktop's
  `showRefusal` (`Fprintln`, then the dialog); a refusal it has shown returns wrapped in the
  exported marker of §4.1 rule 4, so the merged main does not print it again (SC44, SC26). At the
  top of `wizard.Run`, before `healthCheck` and inside no dialog implementation, the verb checks
  two things on Linux and the BSDs: that a tool `zenity` can drive is on PATH (`exec.LookPath`
  over the names the library uses, or its `IsAvailable()`), and that `DISPLAY` or
  `WAYLAND_DISPLAY` is set. If either is missing it refuses on stderr with exit 1 and shows no
  dialog: "mailarchive wizard: the wizard cannot open its windows. Why: it is made of native
  dialogs, which need the zenity tool and a desktop session. Do this: install the zenity package
  and run mailarchive wizard again from your desktop, or use the commands in the README
  (mailarchive -h)." The check looks a file up and an environment variable, and executes
  nothing. The text names no repair it cannot deliver: it does not name MailArchive Desktop,
  which cannot repair a wizard-kind schedule. MA-639 runs the built program (`cmd/mailarchive`
  `TestMain`) with no `zenity`, `qarma` or `matedialog` on PATH and asserts the exact stderr text,
  exit 1, and that a fake dialog tool on a second PATH directory is never invoked
  (`assure.Refused`); its positive twin, a fake `zenity` with `DISPLAY` set, receives the first
  dialog, and a pure table covers `goos`, the lookup result and the display environment. L-L2
  verifies the row on a real box. P2 names this as its exception (b). (SC44)
- The headless run (`runHeadless`, `jobFail`, `notifyFailure`, `logSummary`,
  `cleanupOutlookScratch`) moves to the same package as `RunHeadless(path string) int` and is
  what `run-job` calls. `RunHeadless` keeps the package-level `notify` sink (today
  `var notify = zenity.Notify`), so `TestNotifyFailureHeadless` (MA-124) moves unchanged (SC46).
  The one owner text `RunHeadless` changes beyond the program's name is its rebuild action, today
  `"Rebuild the search index" in the window that opens with this program`, which becomes the
  named constant `headlessRebuildAction`: `Maintenance → Rebuild search index in MailArchive
  Desktop (with that folder as its archive location), or "Rebuild the search index" in the
  wizard (`mailarchive wizard`)`. `IndexOpenError.ForOwner` already names the archive folder.
  The moved MA-386 test asserts that the run-job log names "Maintenance → Rebuild search index",
  `rebuildChoice` and "mailarchive wizard", and no longer contains "the window that opens with
  this program". Because `internal/wizard` never imports `internal/desktop` (below), the constant
  spells the dashboard's control literally, and a test in `internal/desktop`, which may import
  `internal/wizard`, pins that the literal equals `rebuildButton`. The MA-386 row text is
  unchanged; the test gains assertions, an addition the operator classifies at SE0 (OP-6).
  Prove-fail: restore the old rebuild string and the moved MA-386 test goes red. (SC21)
- `health.Input.Surface` keeps its value `"gui"` for the wizard. The text it selects,
  `rebuildHow`'s `choose "Rebuild the search index" below`, is shown only inside the wizard's own
  health list, where that choice is listed, and stays true there. Renaming the value is a text
  change with no owner benefit and is not done. (SC21)
- **The wizard texts that send the owner to "this program".** Three texts send the owner to
  whatever a no-argument launch opens, and each is rewritten to name the wizard by its click
  route and the archive, in the per-OS route words `addMailHelp` uses (§6): on Windows the
  *MailArchive wizard* shortcut in the Start menu; on macOS double-clicking Mail Archive, while
  the `.app`'s verb is the wizard; on Linux `mailarchive wizard`, or the README's `.desktop`
  entry. The three are `notifyFailure`'s "open Mail Archive to see what went wrong", which keeps
  the schedule's name and the word "failed" that MA-124 asserts and names the archive when the
  run knows it; the repair dialog's "Re-open this program any time to check the backup's
  health"; and the schedule confirmation's same sentence. The last two become "To check the
  backup's health later, open the MailArchive wizard (<route>); it shows the health of <archive>,
  the last archive it worked on." These are commits separate from the move, and MA-622 refuses
  the old phrases (SC47). The MA-546 text changes go to the owner (Q18). (SC21)
- **What the wizard records, per slice.** SE1 changes nothing in what `offerSchedule` and
  `repairSpec` record: `Exe` stays `os.Executable()` and `Args` stays `-job FILE`, as at
  `b1a16bc`. Only on Q16's yes does the recorded spelling become `run-job FILE`, and then the
  `-job` assertions of `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` change (P2
  exception (e), the owner's under rule 2); on that yes the change lands in SE2 with `installSpec`
  and MA-641's `Args` case. SE2 switches `Exe` to `ProgramForSchedule()`
  (§4.6.1) at all four call sites (`runSchedule`, `offerSchedule`, `repairSpec`, `backupSpecFor`)
  in one commit, proven by MA-615; no release is cut from SE1 alone (§11). In that same commit
  `offerSchedule`'s inline `schedule.Spec` is extracted as a pure `installSpec` beside
  `repairSpec`, and MA-641 asserts that install and repair produce the same `Args`, the same `Exe`
  rule and the same wrapper decision (`TaskRunLength` against `SchtasksRunLimit`) for the same
  inputs. (SC42, Q16)
- **The order of `offerSchedule` from SE2.** It calls `ProgramForSchedule()` first; its error is
  the X2 refusal of §4.6.1, shown before anything is written. Then the volatile-directory
  question, as today, on the recorded file; then `schedule.CheckKind` (§4.6.5) before its
  `job.Write`; then `job.Write` and `schedule.Install` of `installSpec`'s result. A refusal at
  any of these steps leaves no job file and no scheduler entry. Its confirmation and the repair
  result print `schedule.ProgramNote(exe, reason)` when it is not empty, the repair dialog's "It
  now runs this copy of the program" names the recorded file instead, and the console note keys
  on the recorded file as well as on `spec.Wrapper`. (SC8, SC1)
- `repairSpec`'s rule stands and is stated exactly: the repair takes the job path, the log and
  the program from the archive's own names and the running program (from SE2,
  `ProgramForSchedule()`); from the descriptor it takes the cadence and the time, which name no
  path, and the kind only to choose or refuse, as §4.6.5 corroborates it. It **never** takes a
  path from the descriptor it repairs (`Job`, `Log`, `Exe`). From SE2 the wizard rebuilds a
  corroborated run-job schedule only; for a capture schedule its note names the Weekly backup
  card, and for an export schedule the `scheduleCommand` line (§4.6.3). `repairable` offers the
  repair only under §4.6.3's condition, and every repair asks the volatile-directory question
  first (SC9). Until SE2 the wizard's repair is kind-blind, as at `b1a16bc` (OP-1). If the
  operator accepts OP-1, MA-645 encodes that gap now as an xfail, and its skip comes off when §4.6.5
  lands in SE2; until then the gap stands unrecorded (SC6). (SC1)
- **The start-failure breadcrumb.** `lastfailure.go` moves whole in SE1's pure-move commit. In
  SE2, as its own commit, the record type and its reader (`readLastFailure`, `showLastFailure`,
  `lastFailureLine`) move to `internal/health`, `health.Gather` reads the breadcrumb for this
  archive's schedule name (§4.6.6), and `healthCheck` stops reading the file itself and shows the
  `start_failed` line that `Summary` puts first. The writer (`recordHeadlessFailure`,
  `writeLastFailure`) stays with `jobFail` and writes the type `internal/health` defines, so one
  type defines the format. The file's name (`last-failure.json`), place (`job.Dir()`), version-1
  format and control-stripping are unchanged, and SC4 does not change the toast (its one change
  is SC21's, above). MA-167 and MA-168 move with the code and keep their markers. (SC4)
- `internal/wizard` never imports `internal/desktop`. SE1's move exports nothing for SE3:
  `wizard.Choice` (today's `wizardChoice`), `wizard.OptionsFor` and the exported predicates are
  the SE3 design's change, made as their own commit after the pure move (§4.8). (SC39, SC40)

### 4.5 The desktop launcher moves to `internal/desktop` (SE1)

#### 4.5.1 The move

`launch.go`, `dialogs.go`, `former.go`, `former_windows.go`, `former_linux.go`, `former_other.go`,
`policy_windows.go`, `policy_other.go` and the handler constructors in `main.go` move into
`internal/desktop` (a `launch` file set), with their 21 tests. `launchEnv` and `osEnv` are already
the seam those tests inject; nothing in them names the binary except the refusal texts
(`portBusyText` says "mailarchive-desktop: -addr …"), which become "mailarchive desktop: -addr …"
(P9). The `flag.NewFlagSet("mailarchive-desktop", …)` becomes `"mailarchive desktop"` so `-h`
reads the real invocation (X3). The move is part of SE1's pure-move commit, with
`cmd/mailarchive-desktop` kept as a thin main; the two renames are a later SE1 commit of their
own (SC43).

#### 4.5.2 "Our own program" (Windows and Linux): today's test, and the amendment Q10 decides

**Today, and the interim.** `endPreUpgrade` runs only when `probeFormer` returns
`FormerPreUpgrade`: the old dashboard page answers on 127.0.0.1:8097 and `/_handoff` answers 404.
It ends that listener only when `isOurOwnProgram` holds: on Windows the image path equals
`os.Executable()` and the token user's SID is this process's (`former_windows.go`); on Linux
`/proc/<pid>/exe` equals `os.Executable()` and the uid is this process's (`former_linux.go`,
`exe == self`). On other OSes it ends nothing (`former_other.go`). After SE1 the running file is
`mailarchivew.exe` (Windows) or `mailarchive` (Linux), and a pre-upgrade instance is
`mailarchive-desktop(.exe)`, so the exact-path test never matches it and the owner sees CC22's
PreUpgrade notice until sign-out. That outcome is loud and CC22 allows it. Until the owner
answers Q10, both OSes keep today's exact-path test, unchanged, and MA-619, L-W6 and L-L3 are not
built. No slice is held. (SC31)

**The amendment Q10 asks.** This paragraph amends design-local-confidentiality §4.C4 step 2
[CC22], subject to Q10; it is not decided here. On a yes for an OS, the test on that OS also
accepts the single former name `mailarchive-desktop` (`mailarchive-desktop.exe` on Windows) in
the running file's directory, run by the same token user, and it is still reached only when
`probeFormer` returns `FormerPreUpgrade`. Revision 1's set, "the pair and the two former names",
is cut to that one name: the pair always answer `/_handoff`, so neither is ever
`FormerPreUpgrade`, and `mailarchive-gui` never served a dashboard. The one property the
remaining name buys is that a pre-LC-C2 instance that survived Install-RMM.ps1 and
`CloseApplication`, typically in another session, is ended rather than shown the PreUpgrade
notice. An instance that has the private link (LC-C2 or later) never listens on 8097, so the test
cannot reach it (§7.1). The directory bound keeps the trust where it is today: whoever can write
the program's directory can replace the program. The recommended answer is yes on Windows, and on
Linux only if L-L3 shows that `/proc/<pid>/exe` still names a usable path after the file is
replaced by rename. On a yes, MA-619 accepts the former name in the same directory with the same
SID and refuses the pair's names, `mailarchive-gui`, another directory and another SID; its
negative twin shows that a same-name, same-directory listener that fails `probeFormer`'s
fingerprint is never ended; and L-W6 runs after a real MSI upgrade N-1 to N over an instance
`CloseApplication` did not end, never on a hand-started old process. On a no, MA-619 and L-W6 are
deleted from this design. The edit to design-local-confidentiality §4.C4 step 2 and its [CC22]
fold lands only with the ruling, entered in that design's review record, and the record names
which adversarial pass reviews the amended text: SE1's §11 pass on §4.5.2, or LC-C2's still-owed
pass. (SC31, Q10)

**What ending an older instance interrupts.** `endPreUpgrade` is immediate: `TerminateProcess(h,
1)` on Windows, SIGTERM on Linux, and the design does not count on the older instance draining.
It ends any Archive now, verify or rebuild the older instance is running. R5 keeps the archive
consistent at its last checkpoint, and the interrupted run reads RED `run_never_finished` until
the next run completes. This holds for today's same-path kill as well as for any amendment under
Q10. No probe of the old instance's runs precedes the kill: waiting would leave the
unauthenticated dashboard on 8097 serving, which is the exposure CC22 exists to close. When
`EndOld` succeeds it reports what it ended (the pid and the image path, control-stripped), and
the launcher keeps that record instead of clearing `former`. The new dashboard's header and
`status` then say: "This launch ended an older MailArchive Desktop of this account (process
<pid>, <program>). A run it was doing reads RED until the next run finishes." That is CC22's
"warning in the dashboard and status" applied to the ended case, and no kill rule changes.
`status` reads it from the instance file, as it reads `former` today (`instance.Former()`):
`former` gains the kind `ended`, which an older reader ignores as a kind it does not know, and the
pid and program travel in an optional `ended` field beside `version` (§4.5.3). MA-638 asserts
that after a successful end the header and `status` carry the line naming the pid; its positive
twin shows no line when nothing was ended. (SC32)

#### 4.5.3 The instance file

`instance.Info.Exe` records `os.Executable()` (informational; `status` and the dashboard show it).
`instance.Info` gains two optional fields: `version` (`json:"version,omitempty"`), set from
`buildVersion()` (SC33), and `ended` (§4.5.2) (SC32). Its `former` field also gains the kind
`ended`, which `NoticeFor` in an older reader maps to no notice. `V` stays 1, and every reader since
LC-C2 decodes with a non-strict `json.Unmarshal` and checks only `v` and `dashboard`, so an older
launcher still reads the file. The lock, the session id, the handoff key and the `0600` file:
unchanged. P2's "no file format" and P10's "no new format" name these three additions as their
exceptions (P2 (g)). (SC32, SC33)

After `instance.Reach` returns a verified answer (`Opened`, `Busy` or a `Link`), and never
before, `handoff` checks the holder. If the holder's `version` is absent (a pre-SE1 instance,
whatever its file is called), or differs from this program's and neither is `dev`, the launcher
shows an X2 notice through its dialog seam (`launchEnv`, beside `ShowRefusal` and `ShowLink`)
and on stderr, and still exits 0 after the handoff. The notice has the shape of CC22's
`PreUpgradeNotice` and CC21's pid naming: "Another version of MailArchive Desktop is still
running for this account (<program>, process <pid>), so the dashboard that opened is that
version's. Do this: quit it (on Windows, end process <pid> in Task Manager; elsewhere,
`kill <pid>`), or sign out and back in, then open MailArchive Desktop again." The program path is
control-stripped. The check compares versions, not base names, because a name test misses renamed
files and every same-path in-place upgrade. The launcher does not end a verified lock holder
itself: that would extend CC22's ending power, and it is not proposed. A healthy old holder keeps
the lock until it is quit or the owner signs out (§7.5, Type II). The notice is only as good as
the version stamp: a build whose main-module version reads `(devel)` reports `dev`, and MA-614
asserts that a release build's does not (§4.10). SE1's friction review records that the dashboard
has no Quit action on macOS and Linux. MA-637 asserts that the handoff names a holder of another
or no version, with its pid and program, and that a holder of the same version is not named.
(SC33)

### 4.6 Schedules: one program name, one kind, and the repair the installing surface offers (SE2)

Each mechanism in this section is written once and used by every surface: §4.6.0 qualifies a file
beside the program, §4.6.1 chooses the program a schedule records, §4.6.2 decides whether a recorded
program is this one, §4.6.5 decides which kind a schedule is, and §4.6.3 decides when a repair is
offered and what it rebuilds. §4.6.6 adds the one reader of the start-failure breadcrumb. SE2 owes the
adversarial pass on this section (§11), and SC53's re-run reads it before SE0 (§14). (SC1, SC2, SC53)

#### 4.6.0 A file beside the program: `SameBuild` and `SiblingQualifies` (SE2)

Two functions in `internal/schedule` (SC7):

1. `SameBuild(a, b *debug.BuildInfo) bool` holds when `Path` (the main package), `Main.Path`,
   `Main.Version`, `GoVersion`, `Deps` and every build setting except `-ldflags` are equal,
   `vcs.revision` and `vcs.modified` included. A nil or unreadable side is a different build. `Path`
   is part of it, so no binary of the three-file era (`cmd/mailarchive-gui`, `cmd/mailarchive-desktop`)
   is ever the same build as the new program. It is §4.2's build-info equality as one function. In
   SE1, MA-628 compares the pair's build info field by field (path, main module, dependencies and
   every setting except `-ldflags`); its call to `SameBuild` is a per-case skip encoder until SE2
   builds this section. MA-614 parses only and makes no build-info comparison. (SC7, SC23)
2. `SiblingQualifies(self, p string) (bool, Reason)` holds when all three of these hold, checked in
   this order:
   - `os.Lstat(p)` is a regular file, not a link (an absent `p` gives `sibling_absent`; anything else
     that is not a regular file gives `sibling_not_regular`);
   - `instance.OwnerReader` reports the same owner for `p` as for `self`, compared file against file
     (an unreadable or different owner gives `sibling_other_owner`). It never uses
     `instance.CheckOwner`, which compares with the process token and would refuse every MSI file
     owned by Administrators;
   - `SameBuild(debug.ReadBuildInfo(), buildinfo.ReadFile(p))` holds (else `pair_out_of_step`).

These two functions are the only code that reads a sibling. They read the file's owner and its
embedded build info (`debug/buildinfo`) and never execute it. They run only on a path that the string
and stat bound has already placed in the running program's own directory: §4.6.1's fixed join
`dir(self)/mailarchivew.exe`, and §4.6.2's rule 4 after `inDir` holds. Their callers are
`ProgramForSchedule` (§4.6.1), `exeIdentity`'s name rule (§4.6.2) and MA-628. Whether §4.5.2's narrowed
"our own program" test also takes the owner part, on a Q10 yes, is §4.5.2's. (SC7, SC31)

If `SameBuild` or the owner read cannot be built for a platform, `ProgramForSchedule` returns the
running file with its reason there, and §4.6.2's name rule is false there, so the identity falls to
`exe_moved`: closed, and loud. (SC7)

Conditions stated beside §5.1 boundary 2 and §5.3 (the first is answered by Q20 (13.44): refused with a relocation/reinstallation remedy; the rest not accepted):
- A sibling the owner owns but whose permissions let another account write it (for example, both
  files extracted into a folder that inherits Authenticated Users: Modify) passes the owner test.
  **Q20 answered (13.44): not accepted as a residual.** Executable selection and scheduling require a
  reviewed permissions policy; where the install location cannot be shown to meet it (e.g. it is writable by
  another account), the affected operation is **refused** with a relocation/reinstallation remedy rather than
  run. The permissions-policy definition and the refusal's detailed mechanism and adversarial pass are at SE2
  (a DACL comparison the likely basis). (SC7, Q20)
- A build with no VCS stamp compares on the other fields only. (SC7)
- What owner a standard account, the MSI and a zip extract give a file, and what FAT and exFAT report,
  is **lab item L-W7**, which decides the owner bound. (SC7)

Tests: MA-615 and MA-616 gain an other-owner case (an injected `instance.OwnerReader`), a skewed-build
case (a fixture built with a differing build setting, and a non-Go file), a non-regular case and the
genuine-copy case. §9.4 records the prove-fails for dropping `SameBuild` and for dropping the owner
comparison. (SC7)

#### 4.6.1 `schedule.ProgramForSchedule()`

One helper in `internal/schedule`, used by the command line's `runSchedule`, the wizard's
`offerSchedule`/`repairSpec` and the desktop's `backupSpecFor`, and by every repair
(`repairSchedule`, the Weekly backup card's Repair, §4.6.3). SE2 switches `Exe` at those four call
sites from `os.Executable()` to this helper in one commit, proven by MA-615. Until SE2, SE1's call
sites record `os.Executable()`, as at `b1a16bc`. (SC8, SC42)

`func ProgramForSchedule(wrapped bool) (exe string, reason Reason, err error)` (SC8):

- `exe := os.Executable()`, then `filepath.EvalSymlinks` (absolute), then `filepath.Clean`: this is
  `self`. A non-absolute result is an error. Only this helper and §4.6.2's identity resolve the path;
  §4.1 rule 6 and §4.3 read the invoked path before resolution. (SC8, SC15, SC16)
- On every OS, a `self` whose lower-case slash form contains `/apptranslocation/` returns
  `ErrTranslocated` with no path. Every caller refuses (D1) with an X2 refusal: "Mail Archive is
  running from a temporary copy macOS made of a downloaded app; a schedule recorded from it stops
  working when the app quits. Do this: drag Mail Archive.app to your Applications folder, open it
  from there, and install the schedule (or choose Repair) again." **Lab item L-M4** checks
  translocation on each supported macOS version. (SC8, D1)
- On Windows, when `self`'s base name is `mailarchive.exe` or a former name, the sibling is
  `W = dir(self)/mailarchivew.exe`. When `SiblingQualifies(self, W)` holds (§4.6.0) it returns `W`
  with reason `sibling`; otherwise it returns `self` with one of these reasons: `sibling_absent`,
  `sibling_not_regular`, `sibling_other_owner` or `pair_out_of_step`. The former-name clause retires
  at SE4 (§7.1's floor). When `wrapped` is true and the L-W2 wrapper gate is not known to hold (a build
  constant, default = holds, so this is dormant until L-W2 runs), it returns `self` with reason
  `wrapped_console` even where `SiblingQualifies(self, W)` holds; `ProgramNote` renders `wrapped_console`
  empty, because the console is cmd.exe's either way. (SC8, SC37)
- Elsewhere, and for `mailarchivew.exe` itself, it returns `self` with reason none.

Nothing in the archive, the descriptor or the job file influences the choice (P6 of
design-thunderbird-presync, applied here: settings never come from the archive). `ProgramForSchedule`
refuses a non-absolute path; `Spec.Validate` requires a non-empty `Exe` and checks absoluteness only
for the wrapper path. (SC8)

**One rendering.** `schedule.ProgramNote(exe, reason)` is the only text any surface prints about the
choice (SC8):
- empty for reason none, `sibling` and `wrapped_console`;
- otherwise "This schedule runs `<base(exe)>` because mailarchivew.exe beside it <is missing | is not
  a plain file | belongs to another account | is from a different release>.";
- when `base(exe)` is `mailarchive.exe` it adds "A console window will stay open while it runs.";
- then "Do this: put mailarchivew.exe from the same release back beside it (reinstall from the MSI,
  or copy both files from one release) and choose Repair." For `sibling_absent` the Do-this names the
  installer first: "Do this: restore mailarchivew.exe with the installer's Repair (if your antivirus
  removed it, restore and allow it first), or copy both files from one release, then choose Repair."
  (SC8, SC25)

It is printed by the command line's `schedule -install` (on stdout, after a successful install), by
the wizard's `offerSchedule` and repair result dialog (whose console note now keys on the recorded
file as well as on `spec.Wrapper`), and by the Weekly backup card's Install/update and Repair results.
A notice about a missing `mailarchivew.exe` goes through this same function, never through a separate
stderr write. (SC8, SC25)

**Called first.** Every caller (`runSchedule`, `offerSchedule`, `repairSpec`, `repairSchedule`,
`backupSpecFor`, the card's Repair, and the SE3 design's install, §4.8) calls `ProgramForSchedule`
before `job.Write` and before `schedule.Install`, so a refusal leaves no job file and no scheduler
entry (MA-630: `assure.Refused`, the healthy twin first). Each passes its wrapper decision:
`backupSpecFor` and `runSchedule` pass `true` (their `.cmd` is always on, non-goal 7, D2);
`offerSchedule`/`installSpec` pass the `TaskRunLength`-vs-`SchtasksRunLimit` result, computed from the
console-build name before the call so the one-character name difference cannot change the fit (no
circularity); the repair callers pass the recorded `spec.Wrapper`. (SC8)

**What the owner sees.** On Windows the sibling choice changes what the owner sees in one case only:
a schedule without a wrapper (the wizard's, whose direct run string fits `schedule.SchtasksRunLimit`)
installed from `mailarchive.exe`. For a wrapped schedule (the desktop's Weekly backup and the command
line's `schedule`, whose `.cmd` wrapper is always on: non-goal 7, D2) it changes the program inside
the `.cmd` and nothing on screen; the console is cmd.exe's. **Lab item L-W2** checks, for both
wrappers running `mailarchivew.exe`, that cmd.exe waits for the child, that the child's stderr reaches
`<name>.stderr.log`, and that its exit code becomes the task's Last Run Result. If any of the three
fails, SE2 does not record `mailarchivew.exe` inside a wrapper: `ProgramForSchedule(wrapped=true)` returns
the console build (`self`) instead of the qualifying sibling, and R33's parenthesis gains "a wrapped schedule records the console
build": at SE0 if L-W2 has run by then, as part of the proposed text the operator accepts or not;
after SE0 that is a narrowing of landed text, which goes to the owner under rule 2 (§10). (SC22)

When `mailarchivew.exe` is missing beside the running file, the fallback records the console build,
and `schedule -install` and each surface's repair print the one `sibling_absent` notice above, naming
the installer's Repair. (SC25)

Tests: MA-615 covers each OS, each of the four names (the suffixed-name case included) and each
reason, with real files in a tempdir and an injected `instance.OwnerReader` and `self`. MA-630 proves
the one rendering on every surface. (SC8)

#### 4.6.2 `health.exeIdentity` learns the program's names

Today: `isThis = sameFile(self, d.Exe) || (size and mtime equal)`. After SE2 it is one predicate,
written once and used for the backup's descriptor and the verify's alike (the verify maps to its
`verify_` codes at WARN, as today). (SC2)

Inputs:

```
self       = Clean(EvalSymlinks(os.Executable())), read through a seam tests inject
rec        = d.Exe, as recorded
name(rec)  = base(rec) as written; on Windows, when Lstat(rec) is not a link,
             base(EvalSymlinks(rec)), the long name
inDir      = sameArchivePath(dir(self), dir(rec), GOOS): Clean-equal, else os.SameFile
             of the two directories, else EqualFold on windows and darwin, exact on linux
knownName  ∈ {mailarchive, mailarchivew, mailarchive-desktop, mailarchive-gui} (+ .exe)
formerName = a name that begins mailarchive-desktop or mailarchive-gui, release-asset
             suffixes included (the same test as invokedBase in §4.1 rule 6)
```

`knownName` and `formerName` match with EqualFold on windows and darwin and exactly on linux, with no
trimming of trailing dots or spaces. Because `formerName` is a prefix test, a former release asset
(`mailarchive-gui-linux-amd64`, `mailarchive-desktop-windows-amd64.exe`) is a former name too (SC2). **Lab item L-W8** checks `EvalSymlinks` and `SameFile` on case,
8.3, `\\?\` and junction spellings; **L-M2** checks `os.Executable` through the `/usr/local/bin` link
and the `.app` link before and after `EvalSymlinks`. (SC2)

The rule, first match wins (SC2):

1. `statProgram(rec)` fails: missing (RED `exe_missing`).
2. The lower-case slash form of `rec` contains `/apptranslocation/`: not this program (WARN
   `exe_moved`, with the move-to-Applications remedy).
3. `sameFile(self, rec)`: this program, basis `same_file`.
4. `inDir` and `knownName(name(rec))`:
   - `knownName(base(self))` is false (the running file is a renamed or suffixed copy, which never
     vouches for a sibling): WARN `exe_moved`;
   - `SiblingQualifies(self, rec)` holds (§4.6.0): this program, basis `program_name`;
   - else, if the owner matched and the build was readable but differs: WARN `exe_version_skew`;
   - else: WARN `exe_moved`.
   In this branch the size and mtime fallback is not consulted.
5. `formerName(name(rec))`: WARN `exe_moved`. The fallback never makes a former name this program.
6. `ExeSize` equal, `ExeMTime` non-zero and equal to the stat of `rec`: `isThis` true as today, basis
   `unchanged_since_install`, meaning "the recorded file is unchanged since install; not compared
   with this program" (D4: MA-466 and its fixtures keep their outcomes).
7. Otherwise: WARN `exe_moved`.

`exeIdentity` returns a struct (`exists`, `isThis`, `basis`, `skew`) instead of two booleans. `Gather`
adds, each with a `Verify` twin (SC2):
- `Input.ExeFormerName = isThis && formerName(name(rec))`, whichever of rules 3 and 4 matched;
- `Input.ExeConsoleBuild`: GOOS windows, the file exists and `name(rec)` is `mailarchive.exe`,
  whether or not `isThis`;
- `Input.ExeRepoint` (§4.6.3).

The texts (SC10; the `Program:` line's labels and their order are §6's, proven by MA-629):
- the **note** (not WARN) `exe_former_name`: "The schedule runs `<path>`, a former name of this
  program. Why: this release keeps that name as a copy (or link); a later release removes it. Do this:
  <surface>. Nothing changes until you do." The surface part comes through the existing `in.Surface`
  seam: on the dashboard "click Repair on the Weekly backup card", in the wizard "choose Repair the
  scheduled backup in the health check", on the command line the route §4.6.3's `RepairRoute`
  names. Revision 1's "it works" is dropped. The note keys on the recorded name, `name(rec)`, and
  carries no constant former name, so MA-622's sweep stays green. (SC10, SC37, SC47)
- the **note** `exe_console_build`: "The schedule runs the command-line build mailarchive.exe; a
  console window stays open while it runs. Do this: put mailarchivew.exe beside it and choose
  Repair." (SC10)
- the **WARN** `exe_version_skew`: "the schedule runs `<rec>`, a different build of this program than
  `<self>`: one file was replaced without the other. Do this: reinstall from the MSI (or copy both
  files from one release), then Repair." (SC10)
- `exe_missing` and `exe_moved` are unchanged in meaning. Their remedy names the one repair that
  rebuilds a schedule of the descriptor's kind, from `schedule.RepairRoute` (§4.6.3), in the reading
  surface's words. When `name(rec)` is a former name, `exe_missing` adds "(`<name(rec)>` was a former
  name of this program and was removed by an upgrade)" before the repair. (SC3, SC10)
- On Windows, `exe_missing` for a missing file in the running program's own directory (`inDir`) under
  a name the installed release ships gains the install-folder branch: "the program file is missing
  from the install folder: restore it with the installer's Repair; if your antivirus removed it,
  restore and allow it first; the schedule then runs again unchanged". The exact Repair path and the
  `msiexec /fa` form come from **lab item L-W9**. The schedule re-install stays the remedy in every
  other case. MA-635 pins the branch, with its positive twin. Whether Repair holds against an
  antivirus verdict that still stands depends on Windows signing, the owner's question Q13. (SC25,
  Q13)

The same-directory bound, with §4.6.0's owner and build test, is trust boundary 2 (§5.1). (SC2, SC7)

Each health view assesses one archive: the dashboard its configured archive, the wizard the archive in
`last-archive.txt`, and `status -out` the archive named. The wizard's health check therefore judges
the schedule of its last archive only. (SC5)

**File-system touches.** Every touch of `rec` or its directory goes through the `statProgram` seam
family (the `inDir` comparison included), so the open G8-AI4 encoder
(`TestStatusDoesNotStatAUNCProgramPath`) watches all of them; its call is adapted to the struct and
its assertion is unchanged. Before `inDir` is known, the touches are today's stats of `rec` (rules 1
and 3) and one stat of `dir(rec)`. `Lstat`, the owner read and the build-info read happen only after
`inDir` holds. (SC2)

**Honesty.** The size and mtime fallback stays because descriptors written before SE2 record
`ExeSize`/`ExeMTime` and nothing else identifies a copied file. The fallback says the recorded file
is unchanged since install; it never identifies a copy as this program, and it never makes a former
name, or a program name in the program's directory, this program. (SC12)

A former-name file that matched only through today's fallback moves from silent GREEN to WARN
`exe_moved` (rule 5). That is louder; no existing test pins the silent behaviour, and whether to
accept the change is the operator's call in SE0's catalog-first PR (OP-6). (SC2)

**At SE4** (§7.1's floor): on Windows the former names leave `knownName`, so a hand-made former-name
copy in the program's directory reads as not this program and is never GREEN without a note; they
stay in `formerName`, so rule 5 and `exe_missing`'s former-name hint keep naming them. The
`exe_former_name` note's text is retired. The macOS `knownName` follows the `.app` link's lifetime
(Q9). (SC10, SC37)

Tests: MA-616 carries the cases of the composition trace: the MSI N-1 copy with a new size and mtime;
the darwin real-symlink fixture named `mailarchive-gui`; a darwin text file named `mailarchive-gui`; a
linux old build under a former name with matching recorded size and mtime, both `mailarchive-gui` and the
former release asset `mailarchive-gui-linux-amd64`; a running file that is a renamed copy beside a
recorded `mailarchivew.exe` (WARN, never the fallback); a Windows skewed sibling;
another owner; another directory, with a same-build, same-owner copy named `mailarchivew.exe` and a
zero or mismatched `ExeMTime`; the case, 8.3, `\\?\` and symlinked-parent spellings; a recorded path
under `/AppTranslocation/`; and each note and WARN text. MA-466's existing fixtures (names outside the
name set) keep their outcomes. MA-623 uses this predicate with `self` outside `rec`'s directory.
(SC2, SC10, SC13)

#### 4.6.3 The repair

Which kind a repair rebuilds is §4.6.5's; this section decides when a repair is offered, which route
names it, and which program it records. (SC1, SC9)

- **The route.** One function, `schedule.RepairRoute(desc, out, ev)`, returns the route §4.6.5(e)
  decides: run-job, Repair in the wizard, or on the Weekly backup card of that archive where the card
  is shown; capture, Repair on the card; export, the `scheduleCommand` line; none, the two explicit
  installs. The Repair buttons and every schedule remedy (`exe_missing`, `exe_moved`,
  `exe_former_name`, `not_installed`, `moved_archive`) are rendered from it through a `repairHow(in)`
  beside `rebuildHow`, in the reading surface's words, so no text names a button that is not on its
  page or a command that refuses. `scheduleCommand` serves the export kind only. On the command line,
  the run-job route adds the no-display fallback: remove the schedule with
  `mailarchive schedule -name N -remove`, then schedule the export with
  `mailarchive schedule -out … -install`; the job's settings are in `<job file>`. This fallback is
  the one remedy for a run-job descriptor that names `schedule … -install`: it appears only on the
  command-line surface, only after its `-remove` step, and names the job file (MA-633 asserts that
  placement). (SC3)
- **The re-point term.** `Input.ExeRepoint` holds when the basis is `same_file` or `program_name`
  (never `unchanged_since_install`), `ProgramForSchedule()` returns no error, and its path differs
  from `rec` as a path: not `inDir`, or the names differ (EqualFold on windows and darwin). The
  comparison is never `sameFile`, which would hide the `.app` link. One term covers the former-name
  note, the console-build note once `mailarchivew.exe` is back, and the macOS link. It is not offered
  while `ProgramForSchedule()` would record the same file again, so it neither loops nor appears while
  the sibling is absent. (SC9)
- **The wizard** already repairs (`repairable`, `repairSchedule`). `repairable` keeps its host checks
  and `NotInstalled || !ExeExists || !ExeIsThis`, gains `|| in.ExeRepoint` (and the verify twin), and
  is offered only when the route is the wizard's: the recorded kind is run-job, corroborated, and
  capture is not. For a capture descriptor the wizard's note names the card; for an export it names
  the `scheduleCommand` line. (SC1, SC9)
- **The dashboard's Weekly backup card** gains the same offer for *its* descriptor. Its Repair
  condition changes from "the descriptor's `Exe` is a former name or is missing" to the same terms
  as the wizard's, and it dispatches by the corroborated kind: a run-job is rebuilt through the moved
  pure `wizard.RepairSpec` with the corroborated job path; a capture through `backupSpecFor` from
  `ProgramForSchedule()`; an export is not rebuilt, and the card names the `scheduleCommand` line. The
  button is the existing `schedule-install` endpoint with the existing guard; the server takes the
  spec from the corroborated kind, never from the request (non-goal 10: no new endpoint). The card
  shows the kind in words, Repair per the route, and Remove; Install/update appears only for a
  capture descriptor or no descriptor. The card's Remove of a run-job deletes the corroborated job path
  (§4.6.5(b)) and removes the scheduler entry under the name recovered from that job file's basename, so
  Remove is not a dead-end when the entry was installed under a different path spelling; where no
  corroborated job file is found it falls back to `job.PathFor(schedule.DefaultNameFor(out))`, as the
  wizard's `removeSchedule` does. (SC1, SC3,
  SC9)
- **The card on a local-only archive (Q18(b): yes, narrowly).** The owner ruled (13.44) that the Weekly
  backup card **is** rendered for a local-only archive's recorded schedule, showing the schedule's kind,
  its health, and **Repair and Remove** — but **not** the Microsoft 365 install controls (no Install/update,
  no capture install there). So the owner repairs the archive they are viewing, in place, without the
  `status -out` + command-line detour the earlier interim required. This narrows MA-546's "the Weekly backup
  controls are not rendered" and changes what `TestLocalOriginArchiveInDesktop` asserts — a rule-2 edit the
  owner has now made (Q18(b) yes). The SE3 add-mail page stays Q18(c), deferred to the SE3 design.
  (SC1, SC3, Q18)
- **A capture install**, explicit or a repair, refuses with `HeadlessCapture`'s not-configured text
  before `schedule.Install` when Microsoft 365 is not set up. (SC1)
- **The volatile-directory question first.** Before installing, the wizard's `repairSchedule` and the
  card's Repair show the existing `inVolatileDir` question (the dashboard as its X2-shaped page
  confirmation), exactly as `offerSchedule` does. Cancel installs nothing. (SC9)
- **One program, called first.** Every repair takes its program from `ProgramForSchedule()` (§4.6.1),
  called before `job.Write` and `schedule.Install`, never from the descriptor (§5.4 item 2). The
  wizard's install and repair agree: `installSpec` and `repairSpec` (§4.4) give the same `Args`, the
  same `Exe` rule and the same wrapper decision for the same inputs (MA-641). (SC8, SC42)
- **The command line's `schedule -install` repairs the schedules the command line installs; a wizard
  or desktop schedule is repaired by its own surface (§4.7, Q3).** Q3 stays no. (SC3)
- **`schedule -repair -out DIR` (Q12: yes, before SE4).** The owner ruled (13.44) that `schedule` gains a
  `-repair -out DIR` form (it touches locked schedule-v2 P2 and X4): it re-installs the archive's recorded
  schedule from the archive's own name, §4.6.5's corroborated kind and `ProgramForSchedule()`, never the
  descriptor's `Exe`, after `job.Read` validates the rebuilt job file, with the X1 exit codes (MA-644). It
  preserves the schedule's supported settings and **refuses without mutation** when the corroborated inputs
  are insufficient to reconstruct the schedule with confidence. It **lands before the compatibility files are
  retired (before SE4)**, so headless boxes and fleet administration have a job-preserving repair. The bullet
  above then reads "`schedule -repair -out DIR` is the command-line repair", and Q3 narrows to installing a
  *new* job-file schedule. **Migration notification (Q11: yes (b), broadened).** A successful run under a
  former name raises, through the existing best-effort notify, **one notification per affected schedule for
  the whole compatibility period** (not release N alone), so an install that skipped release N is still
  warned; there is no login notification ((a) is declined); successful backups otherwise stay quiet, and
  delivery is best-effort (MA-643; lab L-W13). Detection otherwise is the per-kind interactive repair (the
  card's or the wizard's Repair, per the route) and `status`'s `Program:` line; a headless Linux box without
  the scripted repair had only the lossy remove-and-reschedule, now superseded by `schedule -repair -out`
  (§7.3, §12). (SC3, SC38, Q11, Q12)
- No repair runs unasked. A scheduled run never rewrites a schedule (P5).
- **At SE4** the Repair trigger on a former-name `Exe` stays: it repairs the RED. (SC37)

Tests: MA-617 asserts, for each of the three kinds and from each surface, that a repair rebuilds its
own kind or refuses with the scheduler entry unchanged and the descriptor byte-identical
(`assure.Refused`, the positive twin first). MA-618 asserts when it is offered: on the former-name
note, on the symlink fixture, and on `exe_console_build` once the sibling returns; not while the
sibling is absent; not on basis `unchanged_since_install`; and that a repair from a Downloads path
asks first and installs nothing on Cancel. MA-633 asserts the per-kind remedy texts, and that no
remedy for a run-job or capture descriptor offers `schedule … -install` as the repair of that
schedule, the one placed exception being the command line's run-job no-display fallback after its
`-remove` step (§4.6.3, The route); its refusal case is the
wizard's repair not offered, and installing nothing, for a capture descriptor and for a run-job
descriptor with no job file, after the healthy run-job twin passes. §9.4: offering on any `isThis`
(the bound dropped) turns MA-618's not-offered cases red. (SC1, SC3, SC9)

#### 4.6.4 The desktop's `-capture` argument form

The argument form is unchanged in SE1/SE2 (non-goal 8). From SE2, `backupSpecFor` records `Exe` from
§4.6.1 and `Args` led by the `desktop` verb (`["desktop", "-capture", …]`); the legacy `-capture`-first
spelling stays accepted (§4.1). Until SE2 it records what `b1a16bc` records. (SC42)

#### 4.6.5 Which kind a schedule is (SE2)

An archive has one schedule under its name, `schedule.DefaultNameFor(out)`, and today two surfaces
install under that name with nothing that tells their kinds apart (OP-1). This section is the one
mechanism that does: one taxonomy, corroborated by owner-side state, guarded in `refuseCollision`,
kept by every repair. Non-goal 8's "one kind per archive" is this section. (SC1)

**(a) Taxonomy.** `internal/schedule` names a job's kind from its leading arguments against one fixed
set, and never treats them as a path:
- `verify`;
- `run-job`: `run-job`, and the legacy `-job`;
- `capture`: `desktop -capture`, and the legacy `-capture`;
- `export`: any other non-empty job.

An absent descriptor is `none`; a present but unreadable one is `unknown`. `jobKind` and
`cronBlockKind` go from two values to these. This one set is the single vocabulary the mechanism
decides, guards (`refuseCollision`) and repairs by, and every surface that *names the kind* to the owner
renders it in these same words: the Weekly backup card (§4.6.3), the wizard, and `status`'s `scheduled:`
kind line (§6). The remedies are not in these words: `schedule.RepairRoute` through `repairHow` (§4.6.3,
§6) renders them in the reading surface's words, as buttons and commands. The kind is a string compared with a fixed set;
no process is started (desktop P1 and desktop Q14/OR8). (SC1, SC3)

**(b) Corroboration.** The descriptor's `Job` is archive-held and untrusted (§5.2). A recorded run-job
or capture kind counts only when owner-side state backs it, and no launchd or Task Scheduler entry is
ever read back (UJ-6, FB-4):
- run-job is corroborated when a job file read through `run-job`'s secure read (`util.ReadSecureFile`,
  §4.7) yields an `Out` equal to `out` under §4.6.2's `sameArchivePath`. `internal/schedule` tries
  `job.PathFor(schedule.DefaultNameFor(out))` first and, on a miss, enumerates `job.Dir()` for an
  owner-owned 0600 job file whose `Out` matches `out` under `sameArchivePath`; the matching file is the
  corroborated job path, and its basename yields the entry name. The descriptor's `Job` path is never
  followed (§5.2);
- capture is corroborated when the desktop configuration at `graphconfig.DefaultConfigPath()` passes
  the check `HeadlessCapture` makes before its not-configured refusal, and the desktop settings'
  archive location or `BackupFor` is `out`;
- on cron hosts, the managed block's kind, read back today (G8-AC6), stands for the recorded kind.

The evidence is one `schedule.KindEvidence` value, whose `Corroborated` field is the set of kinds
owner-side state attests, computed from the job file, the desktop configuration and the cron block above
and never from the descriptor's `Job`. The program builds it once and passes it to
`schedule.Install` (as an argument, not a `Spec` field) and to `health.Gather`. `internal/schedule`
calls `job.Read` itself (`internal/job` imports only the standard library at `b1a16bc`), and receives
the capture predicate from `internal/desktop`, which it never imports. `installOn` refuses a
non-verify install without evidence; `Spec.Validate` is unchanged, so `repairSpec`, its callers and
the covering tests of MA-123 and MA-166 are unchanged. (SC1)

**(c) The guard, in `schedule.refuseCollision`.** A non-verify install of kind K under the archive's
name is refused on two independent triggers, neither of which lets a forged `Job` suppress a refusal:
(1) when any kind in `ev.Corroborated` (b) differs from K — owner-side state attests a different schedule,
so the install would replace it; (2) when the descriptor names `export` and no owner-side state
corroborates any kind — an off-cron export has no owner-side record, so its descriptor stands for it.
Trigger (1) never consults the descriptor-named kind, so forging `Job` cannot suppress it; corroboration
is read from the owner's config directory and crontab, which the archive writer cannot write. Trigger (2)
reads the descriptor's `export` only to add a refusal, never to suppress one. (SC1)
- The refusal comes before the wrapper, the job file or any scheduler call, in today's voice:
  "archive X already has a <corroborated kind (or export) in words> schedule named N: an archive has one
  schedule, and this install would replace it. Remove it first (<that kind's remover>), or archive into a
  separate folder." The remover is the route's (§4.6.3): the wizard's or the card's Remove for a run-job, the card's
  Remove for a capture, `mailarchive schedule -out <archive> -remove` for an export.
- `schedule.CheckKind` exports the same guard so the wizard's `offerSchedule` calls it before its
  `job.Write`; the SE3 design's install calls it first too (§4.8, R1).
- A same-kind re-install passes, so R14's idempotence is unchanged (a same-kind re-install yields one
  entry), and the verify rules are unchanged.
- P2 names this refusal as its SE2 exception (f), beside (k): from SE2, `schedule -install`, the
  wizard's offer and the card's Install refuse a cross-kind install. SE1 changes none of them (§2).

**(d) The job file a kind leaves.** A successful install of a capture or export kind under name N
deletes `job.PathFor(N)`, which no entry can run any longer. Because (c) refuses first on any differing
corroborated kind, this deletion is reached only when no corroborated run-job differs from the installed
kind, so a forged capture or export install can no longer delete a corroborated run-job's job file. (SC1)

**(e) Repair.** A repair rebuilds the recorded corroborated kind only, from trusted inputs and
`ProgramForSchedule()`, never the repairing surface's assumed default. It is offered (a button, or the wizard's `repairable`) only
when R is run-job or capture, R is corroborated, and the other of those two kinds is not. A run-job is
rebuilt through the moved pure `wizard.RepairSpec` with the corroborated job path, from the wizard or
from the card; a capture through `backupSpecFor`, from the card only; an export is never rebuilt by a
GUI. In any other state no Repair is offered, and the text names both explicit installs and, when both
kinds are corroborated, the job file that corroborates the other kind. §4.6.3 renders the routes.
(SC1, SC3)

**(f) Residuals, stated.**
- An install replaces a schedule only where owner-side state corroborates no kind that differs from the
  installed kind. Where no kind is corroborated (an uncorroborated or `unknown` R) the install replaces
  whatever the descriptor names; the schedule it describes cannot run anyway — there is no job file this
  program wrote for this archive, or Microsoft 365 is not set up for it (a drifted-mode job file is
  refused by `run-job` too and goes RED, §4.6.6).
- An `export` descriptor, which an archive writer can forge, can cause a refusal; the refusal names
  the remover, and nothing is replaced. (SC1)
- The tolerant job-file scan (b) makes run-job corroboration spelling-independent, but a kind with no
  job file (capture, corroborated through the desktop configuration) is still installed under the
  schedule *entry* name `schedule.DefaultNameFor(out)` over the raw `out` spelling, because no scheduler
  entry is read back (UJ-6, FB-4). Canonicalizing that name is a one-time pre-SE2 migration that strands
  the names already installed, an operator decision riding labs L-W8 and L-M2 (§15). (SC1)

**(g) Tests.** MA-631 asserts that a cross-kind same-name install is refused at `installOn` for each
pair of kinds, with no job file, wrapper or scheduler entry written, and that a same-kind re-install
yields one entry. MA-632 flips `Job[0]` (wizard to capture on a machine with no Microsoft 365, capture
to run-job with no job file, run-job to export) and asserts no conversion in any case; and a suppression
case: owner-side state corroborates kind T, `Job[0]` is forged to name K (the kind being installed) with
T ≠ K, and `installOn` refuses (`assure.Refused`) with no wrapper or scheduler entry written and the
corroborating job file still present. MA-617 asserts
the kind-keeping repair from each surface (§4.6.3). Prove-fail: collapse `jobKind` to two values and
the kind rows go red; make corroboration always true and MA-632 goes red. MA-645, the xfail encoder of
the shipped gap OP-1 (R14, R18), lands now if the operator accepts it, and its skip comes off in SE2's
commit that builds this guard. (SC1, SC6)

#### 4.6.6 A scheduled run that could not start (SE2)

`jobFail` writes the `last-failure.json` breadcrumb under `job.Dir()` when a headless `run-job` cannot
start (an absent `-out`, a deleted job file), and today only the wizard's `healthCheck` reads it. In
SE2, `health.Gather` becomes its one reader (SC4):

- `Gather` reads the breadcrumb into a new `Input.LastStartFailure` only when its `Name` equals
  `schedule.DefaultNameFor(out)`, the name the wizard's job file uses — computed from the owner-pointed
  `out`, never from the archive-held `Desc.Name`. The match keys on `out` alone, so a forged `Desc.Name`
  cannot hide a start failure, and a deleted job file (a start-failure cause, MA-634) still raises RED.
- `Assess` raises RED `start_failed` under MA-168's existing rule (the archive is unreachable, or the
  failure is newer than the last recorded run). It names the run, the time, the reason and the remedy:
  "fix that (for a backup drive: connect it), and the next scheduled run records a run here".
- `Summary` puts the line first. `status`, `status -json` (an additive reason code; the version is
  unchanged), the dashboard's Status & health and the wizard's health check show the same line (R18,
  X10). With no archive location set there is no `out` to key the match on, so the dashboard's empty
  state shows the breadcrumb line unconditionally; whether to suppress it once an `out` that does not
  match is set is a stated residual for the owner (§15).
- The wizard's `healthCheck` stops reading the file itself.
- The file's name, its place (`job.Dir()`), its version-1 format, its control-stripping on read and
  the toast text are unchanged (P2).

Placement: SE1 moves `lastfailure.go` whole into `internal/wizard` in the pure-move commit, and SE2
moves the reader into `internal/health` as its own commit (§4.4). MA-167 and MA-168 move with the code
and keep their markers. The reader moves and is not added, so the set of per-user state locations is
unchanged (MA-621). (SC4, SC43, SC51)

Residuals, stated: the breadcrumb is one slot per account, so of two archives that both fail to start,
only the later failure shows. The breadcrumb is `run-job`'s alone: a scheduled capture that refuses at
start writes neither a breadcrumb nor a run record, the shipped gap OP-2 (R18), whose xfail encoder is
MA-646 and whose fix is its own slice. Until SE2 the wizard's own read stays the only reader, as at
`b1a16bc`, and §12 states that the dashboard does not show a run that could not start. (SC4, SC6)

Tests: MA-634 runs a real `run-job` against an absent `-out` and against a deleted job file and asserts
the same line on every health surface; no line for another archive's breadcrumb; that a forged
`Desc.Name` over a real breadcrumb still shows RED (the gate keys on `out`); that a later ok run
supersedes it; and the empty-state line. Prove-fail: remove the `Gather` read and MA-634 goes red.
(SC4)

### 4.7 `run-job` (SE1)

The verb is the wizard's headless run under its own name: `mailarchive run-job FILE`. It reads
the job file through `util.ReadSecureFile(path, "job file", 1 MiB)`, the discipline the
repository already applies to the client secret and the token cache: a regular file only, judged
by `Lstat` and again on the opened descriptor; no link; on Unix a no-follow, non-blocking open,
so a FIFO is refused and never waited on; on Unix not group- or other-accessible (the refusal
names `chmod 600`); at most 1 MiB. `job.Read`'s schema, version and `out` checks then run on
those bytes as today (a newer version is refused). `job.Write` has created every job file `0600`
since `da05d89`, so no job file this program wrote is refused on Unix. On Windows the open adds
no flags and there is no mode check: the boundary there is the per-user `%APPDATA%` ACL, as for
the token cache and the secret store, and §5.1 states it as an open condition beside the claim,
not as an accepted residual. (SC45) It refuses when `-out` is absent (the unmounted-drive guard),
logs to `<out>/mailarchive.log`, runs `app.Run` with `Unattended: true`, and exits 1 on failure
with the failure notification (MA-124). (SC46)

Every refusal, whether from the secure read, from `job.Read`'s checks, from an absent `out` or
from a log that cannot be opened, goes through `jobFail`: the `.jobfail.log` sink, the
start-failure breadcrumb (§4.6.6), the notification, and exit 1. So a planted pipe or link can
never hang a scheduled run behind Task Scheduler's `IgnoreNew`. `jobFail`'s
`<name>.jobfail.log` is opened no-follow: `Lstat` must find it regular or absent (the check
`runlog.Open` makes for the run log), and the append open adds the same no-follow, non-blocking
flags as the secure read. A planted symlink sink is refused, its target stays unchanged, and the
breadcrumb and the notification still fire. MA-640 asserts each refusal (a link, a FIFO with a
bounded wait and no hang, a loose mode, an oversize file, a planted symlink sink whose target
stays unchanged), and in each case that no `<out>/mailarchive.log` is written and `app.Run`
never starts, after a positive twin in which a `job.Write` file runs. Prove-fail: revert the read
to `os.ReadFile` and MA-640 goes red (§9.4). P2 names the secure read as its exception (c).
(SC45)

**Its one desktop output.** `run-job` shows no dialog. Its one desktop output is the wizard's
best-effort failure notification (`notifyFailure` to `notify`, MA-124): one for a failed run,
none for a healthy one. On Linux that notification is the one PATH-resolved program a headless
verb starts, as the wizard's `-job` run does at `b1a16bc`. §5.4 item 6 states it as the
exception, and MA-624's Linux probe asserts exactly one fake-`zenity` invocation, with argv
beginning `--notification`, for a failing `run-job`, and none for a healthy one. The probe proves
the claim on Linux only, because macOS notifies through `osascript` and Windows natively: L-C2
checks that the notification from a scheduler-started failing `run-job` returns promptly on macOS
and Windows, and L-L2 that a failing `run-job` on a box with no `zenity`, and under cron with no
display or session bus, still exits 1, logs FAILED, writes the breadcrumb and returns promptly.
Until those lab items answer, whether the notification can delay the run's exit is an open
condition stated beside P3 and §12. (SC46)

**The Outlook probe (lab item L-W14).** When the job has `Auto` set, `app.Run` itself calls
`outlookcom.Detect()` for its live-`.ost` advisory (`internal/app/run.go`), and that creates the
classic Outlook COM object. From an unattended `run-job` this is the wizard's `-job` behaviour at
`b1a16bc`, moved and not changed by this design. Whether that probe, started by Task Scheduler
with no one at the keyboard, starts Outlook or blocks is lab item L-W14; its answer decides
whether a gap row is owed before SE1 ships for Windows. The design does not claim it harmless.

It is one function moved and one `switch` case; the secure read and the no-follow sink are a
later SE1 commit of their own (SC43). `schedule`'s dry-parse (design-schedule-v2 P2) does
not need to learn it: a job file is validated when written and when read, and the schedule the
wizard installs is not built through `schedule`'s flat form. (Q3 asks whether the command line
should be able to install a job-file schedule; the recommended answer is not in this design.)

### 4.8 The dashboard adds local mail (SE3): moved to its own design

**Status: informative, not approved and not reviewed.** This section is not part of this design's approval, and no code is written from it. The operator's call OP-4 decides whether SE3 leaves this design; this design is written for "SE3 leaves". If the operator decides that it stays, the requirements below become this section's design text, and lenses 2, 4, 5, 6 and 7 are re-run on it (OP-4). (SC39)

**Intent.** A dashboard page that discovers or picks a local mail source, runs the wizard's capture (`app.Run`) in-process in the dashboard's one capture slot, and offers the wizard's schedule for the archive. (SC39)

**Where it goes.** SE3 gets its own design and its own 10-lens pre-code review. That review starts once SE1 and SE2 exist as code. Revision 1's §4.8.1..§4.8.5, P11, the picker's file variant, Q5, Q6 and Q7, and the proposed rows MA-594..MA-602 are withdrawn from this design. They serve only as that design's starting draft. So do revision 1's SE3 rows of §5.2 and §5.3 and its no-COM must-never row, which are carried below as the inputs of SC40 R2, R6 and R7. (SC39)

**The four seams this design keeps for it.** (SC39)
1. The job-file kind. Run-job is one named kind in §4.6.5's single schedule-kind mechanism, corroborated by owner-side state. The wizard installs that kind today, so this design needs the kind with or without SE3.
2. `run-job` (§4.7, SE1).
3. `ProgramForSchedule` (§4.6.1, SE2).
4. The dashboard's capture slot is one in-process mutex, and any run the dashboard starts takes it (today's `runJob`, unchanged here).

The import direction is fixed now: `internal/wizard` never imports `internal/desktop` (§4.4), and SE1's move exports nothing for SE3. This design does not touch the extraction workspace's picker seam (`internal/desktop/picker.go`), so its rule is unchanged: Browse is refused while a workspace export runs. No owner-facing text in SE1 or SE2 names "Add mail from this computer". The README, `addMailHelp` and MA-546's steps keep naming the wizard and the commands until SE3 ships (§6). (SC39, SC40 R3)

**Requirements the SE3 design must meet.** These are labelled SC40 R1..R8. They are requirements on a design that is not yet written, not catalog invariants, and that design proposes their test-spec rows. Each one says what holds here without SE3. (SC40)

- **SC40 R1. One kind per archive, through §4.6.5's guard.** The SE3 design adds no kind logic of its own. It uses §4.6.5's guard (`schedule.CheckKind`, the check `schedule.refuseCollision` makes) and its corroboration by owner-side state. The descriptor's `Job` comes from the untrusted archive. It is used only to refuse, never to choose what is installed.
  - "Keep this archive current" calls the guard first, then `job.Write` (atomic), then `schedule.Install`. A failed install leaves the job file, and the next attempt overwrites it.
  - The page and the card refuse in X2 words, with nothing written, in three cases: an archive kept current by the Microsoft 365 backup, one kept current by a local-mail schedule, and one with a command-line schedule (the remedy names `mailarchive schedule -out <archive> -remove`).
  - From SE2 (§4.6.3, MA-617) the card already shows a run-job schedule with its kind, Repair and Remove, no Install/update, and Remove deletes the job file; the SE3 design keeps this and extends it to a local-only archive only under Q18 (c).
  - On a local-only archive the card is governed by MA-546's text. Changing that text is the owner's decision (Q18 (c)), asked with the SE3 design.
  - Without SE3, §4.6.5's guard covers the wizard, desktop and command-line installs. Q5 moves to the SE3 design.
- **SC40 R2. No COM object in the dashboard process.**
  - The live-`.ost` hint is based on the discovered file names alone (`anyOST`). It is worded conditionally: "if classic Outlook is installed on this computer, the MailArchive wizard can have it export this account first: run `mailarchive wizard` and choose Outlook account (via Outlook app)".
  - The page never calls `outlookcom.Detect`, and `preferOutlookApp` stays the wizard's.
  - The `desktop` verb sets one `app.Options` field that is never persisted (it is not in the job file and `jobFor` does not set it). That field turns off `app.Run`'s own `-auto` Outlook probe, which feeds only `ostAdvisory`'s log line. The archive written still equals the wizard's.
  - The Outlook-app refusal reads: "Exporting through the Outlook app is done in the MailArchive wizard: open **MailArchive wizard** from the Start menu (or run `mailarchive wizard`) and choose *Outlook account (via Outlook app)*." (SC20)
  - A must-never row: the process that runs the `desktop` verb creates no COM object (`outlookcom.Detect`, `outlookcom.CreatePSTs`, any `CreateObject`). A counting detector seam proves it, and its positive twin is the wizard's `run-job` path.
  - Without SE3, the dashboard calls neither `outlookcom.Detect` nor `app.Run`. Q6 moves to the SE3 design.
- **SC40 R3. The picker is refused while the capture slot is held, and SE3 has its own picker endpoint.**
  - One shared helper, `pickerRefused()` (`d.exportLive() || d.cap.running`), serves the workspace's Browse and SE3's picker. It reads the in-process slot, not `captureRunning()`'s lockfile probe.
  - The refusal reads "Browse is off while an export or a capture runs: type the path in the box instead".
  - SE3's own guarded picker endpoint is registered whatever `WorkspaceOff` says. Both endpoints share the one-dialog flag (today `d.ws.pickerBusy`), so MA-526 still holds.
  - A dialog opened before Start stays open during the run; that case cites EW-AO5's accepted NON_GOAL.
  - Prove-fail: drop the `d.cap.running` term. MA-529's text is not edited.
  - Without SE3, `picker.go` and today's rule are unchanged.
- **SC40 R4. The page's run uses the wizard's run options, not the schedule's job.**
  - `wizard.Choice` is exported, and `wizard.OptionsFor(c Choice) (app.Options, error)` is extracted from `runExport`'s inline literal. The extraction is its own commit with its own row, after SE1's pure move.
  - `OptionsFor` carries the chosen mode, Full included. It leaves `Unattended` false. It refuses an unparseable date window, and it refuses `OutlookCOM` without the CreatePSTs inputs.
  - `RunHeadless` stays the only place that sets `Unattended: true`. `jobFor` builds only the job file.
  - The page calls predicates exported from `internal/wizard` and never copies them.
  - Without SE3, SE1 moves the wizard unchanged and exports nothing.
- **SC40 R5. The page's run is hosted with the wizard's run guarantees.**
  - The run's context derives from `d.life.Context()`. The slot keeps the cancel func, the run id and the kind `local`.
  - A guarded `/api/local/cancel {runId}` (through `d.api`) cancels only that live local run.
  - A `Lifecycle.OnDrain` hook cancels the run before any wait, so an orderly quit records `cancelled`.
  - The bound is labelled **conditional**: cancel is cooperative at item boundaries (FC31, N8), and the design states the "Stopping" text for a source that does not respond. SE3 adds no slot release for that case.
  - Archive now's lack of a cancel (design-fault-isolation FC10) is left as it is, and the design says so.
  - The run logs to the activity ring and to `newLogger(out)` (`<out>/mailarchive.log`). Prove-fail: drop the drain hook.
  - Without SE3, the slot hosts only today's Archive now, Check integrity and Rebuild.
- **SC40 R6. Owner-named paths pass the engine's recogniser before the capture slot, and the listing claim is scoped.**
  - Every typed or picked path goes through `app.DiscoverInputs([path], false)`. Each result then goes through the engine's recogniser: a directory as `DiscoverInputs` classified it, a `.pst` or `.ost` through `source.DataFileReadable`, and any other file through `source.OpenWith(path, OpenOptions{})`, closed at once and never walked.
  - `/discover` and `/run` both apply this check before the slot.
  - The residual is stated: a file changed after the check, or a locked `.pst` or `.ost`, fails inside `app.Run` as a recorded failed run.
  - A typed folder that is not a mail store returns only the `.pst` and `.ost` files in it, by name, which keeps parity with `-input DIR` (MA-51).
  - Inputs carried from revision 1's §5.2 and §5.3:
    - the typed or picked source path comes from the owner's browser session, is displayed control-stripped, and is the owner's path under the owner's rights, as `-input` is;
    - the discovery results are mail-derived text, rendered through `textContent` (X15);
    - the owner's typed path to a non-mail file is not an escalation, because the engine already reads any file the owner names with `-input`.
  - Without SE3 there is no typed-path endpoint (§3 non-goal 10).
- **SC40 R7. Every SE3 endpoint is registered through `d.api`.**
  - This covers discover, run, cancel, picker and schedule. `d.api` gives POST-only, the CSRF/Origin/Host guard, the body cap, the JSON 405, the restart refusal and DC3's recover, which answers a legible JSON 500 (X11).
  - MA-405's route walk extends to the new prefix. That is an addition, not a lowered gate.
  - The design does not claim the headers as `d.api`'s, because `localweb.Wrap` already stamps `nosniff` and `no-store`.
  - Carried from revision 1's §5.3: a rebinding page or a hostile link that posts to these endpoints meets the same guard (X9, X13), and a request without the session gets the locked page (X12).
  - Without SE3, no endpoint is added (§5.1 item 3).
- **SC40 R8. Reused texts are quoted exactly and completed where the surface differs.**
  - The slot refusal is `runJob`'s existing text ("a task is already running", followed by "wait for it to finish"). When the holder is a local run, the page appends "or click Cancel on Add mail from this computer".
  - The archive-lock refusal keeps the engine's holder line and adds "Why: one run writes an archive at a time. Do this: wait for that run to finish (or stop it), then try again."
  - The volatile-directory warning ends with "and run the wizard again" in the wizard. On the page it ends with "and open MailArchive Desktop from there, then come back to this page".
  - Without SE3, the texts stay as they are at `b1a16bc`.

**What waits on the SE3 design.** Q5, Q6 and Q7, which move to it. Q2-linux's dashboard answer, together with §4.3's "Open MailArchive Desktop?" question before a Linux launch opens the dashboard (SC18). The macOS flip slice, which comes after SE3 (§11; SC19). Q18 (c), MA-546 at SE3. (SC39)

### 4.9 No new state location (SE1, SE2; OPEN-X1 stays the operator's)

SE1 and SE2 add no per-user state location (P8); the one exception is Q19's migration inventory, which arrives with SE4 (below). SE1 and SE2 write only where `job.Dir`, `instance.Dir` and `desktop.StateDir` already write (§1.5 states their values). The three helpers keep their paths and their comments. No `StateRoot` helper is added, and no comment names a root the helpers "will converge on". (SC51, SC39)

OPEN-X1, the root for new per-user state, stays with the operator, as the pre-sync review assigned it (OP-7). This design neither answers it nor depends on the answer in SE1 and SE2 — **with one exception the owner added in Q19 (13.44): the migration inventory** of archives a schedule was installed for (§7.1), the single new per-user state item this work introduces. It is a consumer of OPEN-X1 (OP-7) for its location; its format, rows and lens/adversarial review are designed at its slice (around SE4), not here. SC51's "no new state root" holds for SE1 and SE2; the inventory arrives with SE4's work. (SC51)

MA-621 (S) pins this. The set of non-test files that compute a per-user state directory (those that call `os.UserConfigDir` or read `LOCALAPPDATA` or `XDG_STATE_HOME`) must equal the set at `b1a16bc`, and a fail-closed twin flags a planted new call site. At `b1a16bc` none of those files is in a package SE1 moves (`cmd/mailarchive-gui`, `cmd/mailarchive-desktop`), so the move leaves the set as it is. (SC51)

### 4.10 Packaging

Each item below belongs to SE1 unless it names another slice. "Release N" and "the floor" are defined in §7.1. (SC37)

- **The builders.**
  - The gated builders of the Windows files are `packaging/build-release.sh` and the `Makefile`'s `build-windows`. Each has exactly two `GOOS=windows go build` lines, both of `./cmd/mailarchive`. Their tokens differ only in the `-o` name and in `-ldflags -H=windowsgui` on the `mailarchivew` line (§4.2).
  - MA-614 (S, every commit) parses both builders, with `GUI_WIN_LDFLAGS` expanded, and asserts this.
  - MA-628 (U) builds the two lines it reads from `build-release.sh` inside `go test ./...` and checks Subsystem and build info (§4.2).
  - `docs/continuation/build_all.py` passes `-trimpath` and no `-H=windowsgui` today, and `docs/continuation/payload.py` builds the two packages SE1 removes. Neither is a builder. In SE1 both build the Windows files by running `make build-windows`. Until then the continuation note labels their output ungated.
  (SC23)
- **`build-release.sh`.**
  - Linux: `mailarchive-linux-{amd64,arm64}`. The Linux former-name assets stop shipping (§7.3).
  - Windows: one archive, `mailarchive-windows-amd64.zip`, holding `mailarchive.exe`, `mailarchivew.exe` and `READ ME.txt` under their install names (the MSI payload's files). The suffixed `go build` outputs are intermediates, removed before the script ends, so no suffixed bare Windows `.exe` is left in `dist/`, which `release.yml` publishes whole. Its `READ ME.txt` is the payload's, and its SE1 rewrite addresses both of its readers (below). MA-614 asserts the zip's members and that they are byte-identical to the payload's files. The suffixed names that §4.3 still lists are only a defence for renamed copies. The zip owner's upgrade is §7.6. (SC11)
  - If the operator declines the zip (OP-5), the suffixed bare files keep shipping, the README names the MSI only for Windows, and §12 states the false `exe_moved` on a bare-download install as a residual (OP-5).
  - macOS: `MailArchive-macos.zip` (the `.app`, §7.2) and the CLI tarball (now the same file as the one inside the `.app`).
  - The MSI payload, `mailarchive-desktop-windows-msi-payload.zip`, keeps its asset name. It is the one asset whose name MA-622's matcher skips (§9.2; SC47). It is assembled from the two Windows outputs: `mailarchive.exe`, `mailarchivew.exe`, and, in every tag from release N until the floor, `mailarchive-desktop.exe` and `mailarchive-gui.exe` as copies of `mailarchivew.exe` (§7.1). (SC23, SC37)
  - The payload README says so.
  - The release-N payload and the MSI carry the GUI bytes three times. L-C1 measures it, and whether WiX de-duplicates them; §7.1 states the installed totals. (SC24)
  - A release file that is not stamped (no `vcs.revision`, or `vcs.modified=true`) is refused by `build-release.sh` before it zips. The script reads `go version -m` of both Windows files, and MA-614 finds that check by content. MA-628 compares the build info of the pair it builds (field by field in SE1, through `SameBuild` (§4.6.0) from SE2). It does not require a clean working tree, so it runs on a developer's copy. (SC11, SC23)
  - The dashboard has never shipped on macOS or linux/arm64. At `b1a16bc`, `build-app.sh` builds only `./cmd/mailarchive-gui` and `./cmd/mailarchive` for darwin, and `build-release.sh` builds `./cmd/mailarchive-desktop` only for linux/amd64 and windows/amd64. From SE1:
    - `mailarchive-linux-arm64` gains the wizard and the desktop, compiled but not lab-run on arm64 (L-L1 and L-L2 name the architectures they ran on);
    - macOS carries the dashboard for the first time, reached as `mailarchive desktop` from Terminal, not as the `.app`'s double-click, and labelled "not lab-run on macOS" in the README.
    (SC19)
- **`Product.wxs`.**
  - Shortcuts target `mailarchivew.exe` with `Arguments="desktop"` (Desktop and Start menu), `"desktop -no-browser"` (Startup) and `"desktop -copy-link"` (Copy MailArchive link).
  - One shortcut is added: `WizardShortcut`, Name "MailArchive wizard", Directory `ApplicationProgramsFolder`, Target `[INSTALLFOLDER]mailarchivew.exe`, `Arguments="wizard"`. MA-614 and the moved packaging test pin it the way `CopyLinkShortcut` is pinned (Name, Arguments, Directory). It is the OS starting a verb, not the program starting itself. (SC20, SC23)
  - The covering test of MA-376 asserts `Arguments` of `-copy-link` and `-no-browser` literally. The new arguments change what it asserts, and under rule 2 that is the owner's decision. It is carried with Q18 (a) (§10, §13). Until the owner answers, SE1's packaging commit waits with SE1's text commit. (SC50)
  - `util:CloseApplication` keeps its one target, `mailarchive-desktop.exe`. That is CC22's pre-upgrade instance, the one that serves 8097 with no private link. Because its Target is the image name `mailarchive-desktop.exe`, it also ends an un-repaired former-name weekly-backup capture running as `mailarchive-desktop.exe -capture …` (§7.1 item 2's legacy spelling), exactly as the unfiltered `mailarchive-desktop` stop in Install-RMM.ps1 does and as today; that ended run reads RED `run_never_finished` until its next run completes from its checkpoint (R5). No `CloseApplication` names `mailarchivew.exe`. A running `mailarchivew.exe` (a dashboard of release N or later, a `run-job`, a `desktop -capture`, a `wizard`) is left to Windows Installer's files-in-use handling, as a running `mailarchive.exe` is today. §7.1 says what a run in flight becomes. (SC34)
  - The exe components are plain components: `mailarchive.exe`, `mailarchivew.exe` and, until the floor, the two copies. Each is a `File` of its own payload source, not a `CopyFile` entry (D3). That makes four exe components in release N and two after the floor.
  - The `packaging_test.go` assertions follow (it parses the WiX source).
- **`Build-MSI.ps1`.**
  - `$exes` and `$hashTargets` name `mailarchive.exe` and `mailarchivew.exe`, plus the two copies until the floor. These are exactly the exes that the payload and the payload README name, so the installed `PAYLOAD-SHA256.txt` names `mailarchivew.exe`. MA-614 asserts both lists. (SC23)
  - The MSI's `ProductVersion` is bound to the tag by a built SE1 step, not a hand-typed value: `build-release.sh` writes the tag it is given, without its leading `v`, into the payload (a `PRODUCT-VERSION.txt` beside `PAYLOAD-SHA256.txt`), and `Build-MSI.ps1` sources `ProductVersion` from it and drops the `1.2.0` default; a `-Version` that differs makes it exit non-zero naming both. MA-614 finds both by content (the write line, and the read-and-refuse). So `MajorUpgrade`'s removal of N-1, §7.4's downgrade guard and the floor no longer rest on builder discipline. **Lab item L-W3** still confirms on real Windows that the tag-derived `ProductVersion` drives `MajorUpgrade` to remove N-1 and that upgrade day is never RED; MA-614 proves the scripts' content, not the emitted MSI's runtime. (SC37)
- **The RMM scripts.** All four change. (SC34, SC35)
  - `Install-RMM.ps1` keeps its unfiltered stop of `mailarchive-desktop` before `msiexec` (CC22). It also stops a `mailarchivew` process, in every session, but only when that process's `Win32_Process.CommandLine` (read with `Get-CimInstance`) is a dashboard. A dashboard means no argument after the program path, or a first argument `desktop` without `-capture`.
    - It never stops `run-job`, `-job`, `-capture`, `desktop -capture` or `wizard`. It adds no `mailarchive-gui` stop, which would end the wizard runs today's script spares.
    - Its comment keeps today's sentence that a scheduled capture mid-run is left to finish.
    - A former-name copy's `-capture` run is still ended by the unfiltered `mailarchive-desktop` stop, as a `mailarchive-desktop.exe -capture` run is today. Sparing that run would narrow CC22's wording and need a CC22 ruling, and this design does not ask for one.
  - `Uninstall-RMM.ps1` adds `mailarchivew` to its unfiltered list (`mailarchive-desktop`, `mailarchive`, `mailarchive-gui`), because uninstall removes the files.
  - `Cleanup-CurrentUser.ps1` stops `mailarchive-desktop`, `mailarchive-gui` and `mailarchivew` in the caller's session, unfiltered, because it deletes that account's lock, instance and serve files. The stop stays ahead of `Remove-Item`.
  - Uninstall and Cleanup keep the former names through and after SE4, because an older instance can survive an upgrade.
  - `Detect-RMM.ps1` (release N) tests the DC6 key and both files of the pair (`mailarchive.exe`, `mailarchivew.exe`) and names no former name. README-RMM.md and the release notes tell administrators to replace a stored detection rule before the release that removes the copies. SE4 waits until that text has shipped. (SC35)
  - MA-614 pins all of this:
    - no `CloseApplication` targets `mailarchivew.exe`;
    - Install's filter and the set of verbs it spares;
    - Uninstall's and Cleanup's lists cover every GUI-subsystem executable `Product.wxs` installs, plus the former names;
    - Cleanup's stop comes before its removal, as Install's stop comes before `msiexec`;
    - Detect's rule names the DC6 key and the pair, and no former name.
  - Prove-fail: remove `mailarchivew` from Cleanup's list, and MA-614 goes red (§9.4).
  - MA-376's existing assertions on these scripts hold unchanged and are not replaced: Install still stops `mailarchive-desktop` before `msiexec`, Cleanup's stop still names it, and `CloseApplication`'s target is the same.
- **Where the packaging test lives.** In SE1's move commit, `cmd/mailarchive-desktop/packaging_test.go` (a covering test of MA-376) moves to `cmd/mailarchive/packaging_test.go`. Only its location changes, and its `../../packaging` paths resolve the same from there. MA-614's packaging assertions sit beside it. It does not stay in `cmd/mailarchive-desktop`, which Q17's interim keeps only as a thin, unreleased main. (SC34, SC27)
- **`Info.plist.in` and `build-app.sh`.**
  - `CFBundleExecutable` = `mailarchive`. `Contents/MacOS/mailarchive-gui` is a symbolic link to `mailarchive` until the floor (§7.1, §7.2). (SC37)
  - The program's no-argument verb inside the bundle is the wizard (§4.3's bundle row, the Q2-mac interim). (SC16, SC19)
  - `build-app.sh` builds `./cmd/mailarchive` for both architectures into one universal file (L-M3). It keeps `zip -q -r -y`, so the link survives the zip. MA-614 asserts the `-y`, and that the script makes `Contents/MacOS/mailarchive-gui` a symbolic link to `mailarchive`. (SC11)
  - `README-macOS.txt`, shipped in the zip, says to drag the app to Applications before it is first opened (§6, §7.2). (SC14)
- **CI and release.** (SC27; Q17)
  - `ci.yml` and `release.yml` both run `GOOS=windows govulncheck ./cmd/mailarchive`. `release.yml`'s step comes before "Build release artifacts". The tool is installed for the runner and then pointed at the Windows build with `GOOS`, as in `ci.yml`, so a reachable known vulnerability in the Windows graph blocks the tag.
  - MA-529's text and its covering test (`TestBrowseIsDisabledDuringARunAndCIScansWindows` in `internal/desktop/ws_run_test.go`) name `./cmd/mailarchive-desktop` literally. Changing them is the owner's decision (Q17, rule 2).
  - Until Q17 is answered:
    - `cmd/mailarchive-desktop` stays as a thin, unreleased main that no builder ships;
    - `ci.yml`'s first `GOOS=windows` step keeps its `./cmd/mailarchive-desktop` scan and adds `./cmd/mailarchive` after it, and `release.yml`'s step scans both, so MA-529's existing assertion passes unchanged;
    - MA-529's covering test gains assertions for the two new scans (each found by content, the package argument matched as a whole token). That only adds a guarantee; it is the operator's to accept under the rulings log's rule 1, classified at SE0 (OP-6), and nothing it asserts today changes.
  - On Q17's yes, one commit makes these changes:
    - the thin main and its scan go;
    - the test reads both workflows and finds the govulncheck step by its content, not as the first `GOOS=windows` line;
    - in each workflow it requires a `GOOS=windows` govulncheck step whose package argument is exactly the token `./cmd/mailarchive`, bounded by whitespace or line end; it never uses `strings.Contains`, which the old path would also satisfy;
    - in `release.yml` the step must come before `build-release.sh`;
    - the test's doc comment, its error message and MA-529's text name the new path and both workflows.
  - MA-628 runs inside `go test ./...`, so it runs on every CI leg and in `release.yml`'s Test step before `build-release.sh`. (SC23)
- **`Makefile`.** `build-gui`/`build-desktop` go, and `all` drops them. `build-windows` builds the pair: the two lines above, with `$(GUI_WIN_LDFLAGS)` on the `mailarchivew` line. The continuation scripts call `make build-windows`. (SC23)
- **SE4, the first minor tag past the floor.** The copies leave the payload, `$exes`, `$hashTargets`, `Product.wxs` and the packaging test together. The `.app` link leaves `build-app.sh`. If any one of these edits is missed, MA-614 goes red (§11). (SC23, SC37)
- **The administrator texts.** SE1's text commit rewrites README-RMM.md, DEPLOYMENT.txt, README-payload.txt, BUNDLE-CONTENTS.txt and SOURCE-NOTICE.txt for the pair, and they join §6's, P9's and X17's lists. MA-622 sweeps them under the phrase rule alone. A former name may appear only on a line that also says "former name", and never as the thing to run, download or build. There is no other skip, allowlist or suppression beyond X17's two excluded non-program tokens (§9.2), which the operator accepts or rejects with X17's definition at SE0 (OP-6). (SC28, SC47)
  - README-RMM.md:
    - §A's file list names the pair, and names the copies on lines that say "former name".
    - §C says what each script stops, in place of "stops every running `mailarchive-desktop.exe`". It tells administrators to schedule upgrades outside the 02:00 and Sunday 03:00 backup window and not to force a reboot on exit code 3010 during it (SC34). It tells them to upgrade through a release that ships the copies (SC37), and to replace a stored detection rule before the release that removes them (SC35).
    - §D's checks are rewritten for the pair: the shortcuts launch `mailarchivew.exe desktop`; netstat's pid is `mailarchivew.exe`'s; the Weekly backup task runs the `.cmd` wrapper whose line runs `mailarchivew.exe desktop -capture …`.
    - In §D.3 and the Risks section, "runs `mailarchive-desktop.exe -capture`" becomes `mailarchivew.exe desktop -capture`.
    - §D carries the Windows lab items as its hand-validation round, recorded where SG3 says (SC29).
    - §E builds with `make build-windows` or downloads `mailarchive-windows-amd64.zip`, and runs `mailarchive.exe desktop` from a terminal. It says that `mailarchive.exe desktop` holds the shell and ends with it, and that `mailarchivew.exe desktop` outlives the terminal (SC30).
  - DEPLOYMENT.txt:
    - its "Weekly backup" line names `mailarchivew.exe desktop -capture`;
    - it carries the disk line (SC24);
    - it carries the Repair remedy for a missing program file: restore it with the installer's Repair, and if antivirus removed it, restore and allow it first (SC25);
    - its upgrade line says what each script stops (SC34).
  - README-payload.txt, which is also the zip's `READ ME.txt`, names the pair and names the copies on lines that say "former name". For the zip owner it gives three things: double-click `mailarchivew.exe`; unzip an upgrade into the same folder, then Repair (§7.6); and make a hand-made shortcut to `mailarchivew.exe wizard` for the wizard (SC20). (SC11)
  - BUNDLE-CONTENTS.txt maps the payload to `cmd/mailarchive`. SOURCE-NOTICE.txt names `mailarchive.exe` and `mailarchivew.exe`, built from `cmd/mailarchive`.
  - What these texts say about which processes an upgrade stops follows §7.1's kill scope. (SC34, SC47)

## 5. Security

### 5.1 Trust boundaries

1. **Scheduler → program.** Arguments written by the owner's own install: the wizard's `-job <path>`, which Q16's interim keeps, and the desktop's `-capture …`, led by `desktop` from SE2 (§4.6.4). Trusted like `-out` today. (SC42)
2. **The program's directory → program.**
   - `ProgramForSchedule` and `exeIdentity` look at *sibling files* of the running executable by fixed names. They do it through one test, `SiblingQualifies` (§4.6.0), and only on a path that the string and stat bound has already placed in the running program's own directory.
   - The bound is the owner. A sibling counts only when it is a regular file (not a link), has the running file's owner (compared file to file, never with the process token), and is the same build (`SameBuild`). Its embedded build info is read, and the file is never executed.
   - A file that another account creates there is owned by that account (lab L-W7 states the Windows ownership facts), so it is refused. The schedule then records the running file, and the install says so (§4.6.1).
   - Where `SameBuild` or the owner read cannot be built for a platform, `ProgramForSchedule` returns the running file with its reason, and the name rule is false there. The result is `exe_moved`, which is loud.
   - One condition beside this claim is answered and one stays open. Answered (Q20, 13.44): a sibling the owner owns whose permissions let another account write it (for example, both files extracted into a folder that inherits Authenticated Users: Modify) — executable selection and scheduling require a reviewed permissions policy, and a location that fails it is **refused** with a relocation/reinstallation remedy rather than accepted (the policy and mechanism are at SE2, a DACL comparison the likely basis). Still open: a build with no VCS stamp, which is compared on the other fields only.
   (SC7)
   - Program Files is administrator-writable. A user-directory install (Downloads) gets the wizard's existing `inVolatileDir` warning, and every repair asks that question first, the dashboard's included (§4.6.3). (SC9)
   - A translocated launch (macOS, a downloaded app that was not moved) is refused by `ProgramForSchedule` (§4.6.1), not warned (D1). (SC8)
3. **Browser → dashboard.** Unchanged (X9, X12, X13). This design adds no dashboard route: the Weekly backup card's Repair uses the existing `schedule-install` endpoint under its existing guard (§4.6.3). SE3's routes belong to the SE3 design, which registers each one through `d.api` (§4.8, SC40 R7). (SC39)
4. **Archive directory → program.** The descriptor's `Exe` and `Job` are read for *display and identity comparison only*; nothing executes them; the repair re-installs from the running program.
   - The kind named from `Job` (§4.6.5) chooses a repair, or refuses an install, only when owner-side state corroborates it: the archive's job file for run-job, the desktop configuration for that archive for capture, and on cron hosts the managed block's kind.
   - An `export` kind refuses a cross-kind install on the descriptor alone, because off cron an export has no owner-side record. It never chooses what is installed. §5.3 states this residual.
   (SC1)
   Unchanged from the wizard's `repairSpec` rule, now shared by three surfaces.
5. **Job file → run-job.** The owner's own `0600` file in the config directory.
   - `run-job` reads it through `util.ReadSecureFile`: a regular file, judged by `Lstat` and again on the opened descriptor; no link; a no-follow, non-blocking open on Unix; owner-only mode on Unix; at most 1 MiB. `job.Read` then validates its schema, version and `out` (§4.7).
   - On Unix, a file another account planted is unreadable or is refused for its mode.
   - On Windows the boundary is the per-user `%APPDATA%` ACL, as for the token cache and the secret store. That is an open condition stated beside this claim (rulings log rule 3), not an accepted residual.
   (SC45)
   A same-user attacker who can write it can edit the crontab too (DC4, Type I).

### 5.2 Untrusted inputs and how each is handled

| Input | From | Handling |
|---|---|---|
| First argument | the scheduler, a shortcut, the shell | under the program's own names, an exact-match `switch`: unknown strings fall to the export verb's flag parser, which refuses what it does not know (X3). Under a former name, the whole argv goes to that name's `b1a16bc` flag set (§4.1 rule 6) (SC15) |
| The executable's own path and base name | the OS | the no-argument rule and rule 6 read the invoked path before any symlink is resolved (`os.Executable` on Windows and macOS, the base of `os.Args[0]` on Linux). `os.Args[0]` is set by whoever starts the program, and it selects only a surface that person could select with a verb. `ProgramForSchedule` and identity read the path through `EvalSymlinks` (§4.6.2's `self`). The path is compared to a fixed name set and never interpolated into a command. It is used only for `schedule.Spec.Exe` (quoted by `winQuote`/the plist/crontab writers as today), the instance file's `Exe`, and display (SC15, SC16, SC2, SC52) |
| Sibling file beside the executable | the program's directory (trust boundary 2) | `Lstat`: a regular file (no link), one of the fixed names, with the running file's owner. Its build info is read and compared (§4.6.0). It is never executed (SC7) |
| The sibling's build info and owner | the sibling file (trust boundary 2) | read only after the string and stat bound has placed the path in the program's own directory, through `buildinfo.ReadFile` and `instance.OwnerReader`. Compared with the running file's values (`SameBuild`; owner compared file to file). An unreadable value counts as a different build or another owner: the result is `exe_moved`, and the install records the running file (SC7) |
| Descriptor `Exe`, `ExeSize`, `ExeMTime` | the archive (untrusted) | display (control-stripped, as today) and identity comparison; never executed, never used as the repair's program. Touched only through the `statProgram` seam family; `Lstat`, the owner read and the build-info read happen only inside the program's own directory (§4.6.2) (SC2) |
| Descriptor `Job` and the kind read from it (§4.6.5) | the archive (untrusted) | the kind is named from the leading arguments against a fixed set, never treated as a path, and displayed. It chooses a repair or refuses an install only when owner-side state corroborates it. An `export` kind refuses a cross-kind install on the descriptor alone and never chooses what is installed (SC1) |
| The job file | the config directory (`job.Dir`): the owner's, or another account's where the directory is shared (§5.3) | `util.ReadSecureFile`, then `job.Read`'s schema, version and `out` checks. Every refusal goes through `jobFail` and exits 1. `<name>.jobfail.log` is opened no-follow (§4.7) (SC45) |
| Environment and console (the inputs of §4.3) | the launcher | `DISPLAY`/`WAYLAND_DISPLAY` presence; whether each of the three standard streams is a terminal; whether `/dev/tty` opens (opened and closed, never read); on Windows, the console window and the console's process count. Each is read once, for the no-argument rule only (SC16) |

### 5.3 Attacker personas

| Persona | Attack | Mitigation | Residual / label |
|---|---|---|---|
| **Another account on the same computer** | plant `mailarchivew.exe` beside the program so the next schedule install records it | the owner bound (§4.6.0, trust boundary 2): a sibling counts only as a regular file, not a link, with the running file's owner compared file to file, and of the same build. A file another account creates is owned by that account (L-W7) and is refused (`sibling_other_owner`), so the install records the running file and says so (§4.6.1). The program's directory is Program Files (administrators) or the owner's own folder (owner) (SC7) | Type I, as today for the program file. A sibling the owner owns but another account can write is the refused case in the next row (Q20 answered 13.44: refused with a remedy) |
| **An account that can write a sibling the owner owns** (both files extracted into a folder that inherits Authenticated Users: Modify) | rewrite `mailarchivew.exe` in place so that the owner's next schedule install, or the next scheduled run, runs it | `SameBuild` refuses another build only by its embedded build info, which a writer can copy. The owner bound does not apply, because the file stays the owner's (SC7) | **refused with a remedy** (Q20 answered, 13.44): executable selection and scheduling require a reviewed permissions policy; a location another account can write fails it, so the operation is refused with a relocation/reinstallation remedy. The policy, the mechanism and the adversarial pass are at SE2 (a DACL comparison the likely basis) |
| **Another account that can write the config directory** (a group-writable `~/.config`, a shared `XDG_CONFIG_HOME`, a roaming share) | swap the job file for a link, a FIFO, or a job naming its own `out` | §4.7: `util.ReadSecureFile` refuses a link and a non-regular file without hanging (no-follow, non-blocking open). On Unix it also refuses a group- or other-accessible mode, and a file the owner cannot read fails to open. Every refusal goes through `jobFail` and exits 1, and the sink opens no-follow (SC45) | Unix: refused, and loud (`status` shows RED from the start-failure breadcrumb, §4.6.6). Windows: the per-user `%APPDATA%` ACL is the boundary, an open condition (rulings log rule 3), not accepted |
| **Same-user malware** | edit the job file, the crontab, the descriptor | DC4: a same-user process has the user's rights | Type I |
| **Archive writer** (a NAS, a synced folder) | a descriptor whose `Exe` names an attacker's file so a "repair" installs it; or a size/mtime that makes a planted file read as "this program" | the repair never takes the program from the descriptor (`repairSpec` rule, now shared). Under §4.6.2's predicate, a forged `Exe` is `exe_moved` at best through the name rule. The fallback can still say "unchanged since install", as today (stated), but it never makes a former name, or a program name in the program's directory, this program, and it is never grounds for a re-point offer (§4.6.3). A forged path under `/AppTranslocation/` is never this program. The `Lstat`, owner and build-info reads happen only inside the program's own directory (SC2, SC12) | Type I for the display; the fallback's answer is a stated residual under its true meaning (D4) |
| **Archive writer who flips the kind** | rewrite the descriptor's `Job` (wizard to capture, capture to run-job, run-job to export) so that a Repair or an install converts the archive's schedule | §4.6.5: a recorded run-job or capture kind counts only when owner-side state corroborates it. A repair rebuilds only a corroborated kind, from trusted inputs and the running program, and is offered only when the recorded kind is run-job or capture, is corroborated, and the other of the two is not (§4.6.5 (e), MA-632) (SC1) | stated residual: a forged `export` kind refuses a cross-kind install under the archive's name on the descriptor alone. The owner removes the schedule or archives into a separate folder. The refusal is loud and nothing is converted |
| **Antivirus false positive / owner cleanup** | quarantine or delete `mailarchivew.exe` (the copies share its bytes) | none beyond detection: `status` from `mailarchive.exe` is RED `exe_missing` per archive, and the remedy names the installer's Repair and restore-and-allow (SC25) | Type II: every Windows schedule and shortcut is lost together (the cost of R33). The owner learns it on the next `status` or from a dead shortcut. Durability depends on Windows signing (Q13, §13) |
| **A hostile first argument** (a shortcut edited by malware, a crafted `.lnk`) | `mailarchive <something>` runs an unintended surface | every verb is one the owner could run by hand; no verb elevates; `desktop` binds loopback and needs the session (X9); `wizard` shows dialogs; nothing runs unattended except the two headless forms, which read only owner-written files | no change from today's three files |
| **A squatter at the former address** | as today | unchanged (CC23) | — |

Revision 1's two SE3 personas (a rebinding page against SE3's endpoints, and an owner's typed path to a non-mail file) leave with SE3. §4.8 carries them as inputs of SC40 R6 and R7. (SC39)

### 5.4 What the program must never do (each is a row in §10)

1. Execute, `exec`, or re-launch itself or a sibling to perform a function another surface has, including through the OS scheduler's run-now or an opener given the program's own path (desktop owner ruling P1 and Q14/OR8: in-process, never a child of itself). The pair exists for the subsystem flag alone; neither file starts the other. The program starts only the OS tools MA-612 names: crontab, schtasks, launchctl, open, xdg-open, rundll32, explorer.exe, osascript, the clipboard table and zenity, as today. Desktop P1 forbade a child of the program, not every child process. MA-612 does not trace a program path passed through a variable into an opener's argument; §12 states it as an open condition. (SC52)
2. Take a program path from the archive, the descriptor, the job file or the instance file as
   the thing to schedule or to run.
3. Start a server, a browser or a dialog from a command-line verb the owner did not name. `defaultVerb` applies only to an empty argument list under the program's own names; a former name is §4.1 rule 6. (SC15, SC16)
4. Rewrite a schedule unasked (P5), including from a scheduled run.
5. Serve a new dashboard endpoint outside the session prefix and `d.api`, or add an endpoint that lists directories. (SC39, SC40 R7)
6. Run `zenity` or show any dialog from a command-line verb. MA-624: a fake `zenity` on `PATH` is never invoked by `status`, `verify`, `serve`, `search`, `reindex`, `schedule`, `graph`, `extract`, `setup` or the export verb.
   - `run-job` has one exception. It shows no dialog, and its one desktop output is the wizard's best-effort failure notification (`notifyFailure` to the package-level `notify` sink, MA-124). MA-624's Linux probe asserts exactly one fake-`zenity` invocation, with argv beginning `--notification`, for a failing `run-job`, and none for a healthy one.
   - On Linux this notification is the only PATH-resolved program a headless verb starts, as the wizard's `-job` run does at `b1a16bc`.
   - The probe proves the claim on Linux only. macOS notifies through `osascript` and Windows notifies natively, and both are checked by lab item L-C2.
   (SC46)

## 6. Owner-facing text

What the owner reads changes in the program's name (P9), plus the new verbs, the identity texts and per-kind remedies of §4.6, and the migration lines of §7. Every text below follows the phrase rule at the end of this section (SC3, SC10, SC47).

- **README.**
  - The download table under "Just want to download and run it?" (revision 1 called it the "Quick start" table; at `b1a16bc` it names `mailarchive-gui-windows-amd64.exe` and `mailarchive-gui-linux-amd64`, each a former name) names, per OS (SC11, SC14, SC19, SC20, SC44):
    - Windows: `mailarchive-windows-amd64.zip` (unzip, then double-click `mailarchivew.exe`) or the MSI. No suffixed bare `.exe` is named, because none is published (§4.10). If the operator declines the zip (OP-5), the table names the MSI only for Windows (SC11).
    - macOS: `MailArchive-macos.zip`, which unzips to `Mail Archive.app`. In order: drag `Mail Archive.app` into Applications, then open it (the right-click Open of today, once). Double-clicking it opens the wizard (§4.3; the Q2-mac interim). MailArchive Desktop is `mailarchive desktop` from Terminal, labelled "not lab-run on macOS" (SC14, SC19).
    - Linux: `mailarchive-linux-amd64` (`mailarchive-linux-arm64`: compiled, not lab-run until L-L1 and L-L2 run there), run as `mailarchive wizard` or `mailarchive desktop`. The `.desktop` entry of §7.3 is the click route to the wizard. The wizard needs the `zenity` tool and a desktop session (SC44). A no-argument double-click is named only once Q4 is answered "honour" and L-L1 passes both ways; until then the table names the verbs (SC16, SC19, SC20).
  - The GUI section becomes "The wizard (`mailarchive wizard`)". It names the wizard's click route on each OS: the *MailArchive wizard* shortcut in the Start menu (MSI); for the zip owner without the MSI, one line: make a shortcut to `mailarchivew.exe` with ` wizard` added to its Target; on macOS the `.app` while its no-argument verb is the wizard; on Linux the `.desktop` entry (SC20, SC11).
  - "Adding new mail to a Thunderbird archive" keeps the wizard and the commands. No SE1 or SE2 text names "Add mail from this computer"; the page is named only when the SE3 design ships it (§4.8) (SC39).
  - The macOS Terminal paragraph: on a Mac with the `.app`, Terminal reaches the program inside it through the one `/usr/local/bin` link of §7.2; the CLI tarball is for a Mac without the `.app` (SC14). The "do not double-click the command-line program" text stays.
  - A short "One program" paragraph says what the pair is, why two files exist on Windows, and which to double-click: `mailarchivew.exe`; `mailarchive.exe` is the command-line build, and double-clicked it says which file to double-click (§4.3) (SC17). It says that `mailarchive.exe desktop` holds the terminal and ends with it, and that a terminal user who wants the dashboard to stay open after the terminal closes runs `mailarchivew.exe desktop` (SC30). It gives the disk figures L-C1 measures for release N and for the first release past the floor (§7.1), and never says "saving" for release N (SC24).
  - The upgrade paragraph follows §7: the MSI, then the Repair each scheduled archive's health view offers (§7.1); the zip (§7.6); the `.app` dragged into Applications (§7.2); the Linux step of §7.3. Each line that names a former name says "former name" (SC47).
- `AddMailArgv`/`AddMailAutoArgv` already name `mailarchive`. `addMailHelp` gets one text per slice, rendered per OS from its `goos` argument (SC21):
  - SE1/SE2: "the MailArchive wizard (`mailarchive wizard`; on Windows the *MailArchive wizard* shortcut in the Start menu; on macOS double-click Mail Archive)", followed by the wizard's unchanged dialog strings and the unchanged command-line bullets.
  - From SE3, `addMailHelp`'s text and MA-546's matching edit are the SE3 design's (§4.8; Q18 (c), the owner) (SC39).
  - No slice tells the owner to double-click `mailarchivew.exe` for the wizard, and no text names a former name. MA-546's wizard-step edit is the owner's under the rulings log's rule 2 (Q18 (a)); until it is made, SE1's text and packaging commits wait and SE1 is not released (SC21, SC50).
- **The wizard's texts** (SC21):
  - `RunHeadless`'s rebuild action becomes the named constant `headlessRebuildAction`: "Maintenance → Rebuild search index in MailArchive Desktop (with that folder as its archive location), or "Rebuild the search index" in the wizard (`mailarchive wizard`)". `rebuildHow`'s `gui` text is kept because it is shown only inside the wizard's own health list (§4.4).
  - `notifyFailure`'s "open Mail Archive to see what went wrong", the repair dialog's "Re-open this program any time to check the backup's health" and the schedule confirmation's same sentence each name the wizard by its click route and name the archive.
  - No owner text says which surface a no-argument launch opens ("the window that opens with this program", "open Mail Archive", "re-open this program"); MA-622 refuses those phrases.
- **Health remedies** name the one repair that rebuilds a schedule of the descriptor's kind (§4.6.5): the repair the installing surface offers, rendered from the route of §4.6.3 in the reading surface's words, so no text names a button that is not on its page or a command that refuses (SC3):
  - run-job: "Repair the scheduled backup" in the wizard, or Repair on the Weekly backup card where the card is shown. On the command-line surface it adds the no-display fallback: remove the schedule with `mailarchive schedule -name N -remove`, then schedule the export with `mailarchive schedule -out … -install`; the job's settings are in <job file>.
  - capture: Repair on the Weekly backup card.
  - export: the `mailarchive schedule … -install` line (`scheduleCommand`, used for this kind only).
  - no corroborated kind: the explicit installs (§4.6.3).
  - No remedy for a run-job or capture descriptor contains `schedule … -install` (MA-633). On a local-only archive the dashboard **does** render the Weekly backup card (Q18(b), 13.44), showing the schedule's kind, health, Repair and Remove — not the Microsoft 365 install controls — so the owner repairs it in place; the `status -out` + command-line detour the earlier interim named is no longer needed (SC3, SC50).
  - On Windows, `exe_missing` for a missing known-name file in the running program's own directory gains the install-folder branch: "the program file is missing from the install folder: restore it with the installer's Repair; if your antivirus removed it, restore and allow it first; the schedule then runs again unchanged". The exact Repair path and `msiexec /fa` form come from L-W9. MA-635 pins the branch with its positive twin (SC25).
  - `exe_missing` on a former name adds the hint of label 1 below (SC10).
- The desktop's "The MailArchive wizard (mailarchive-gui) last used …" line names `mailarchive wizard`.
- `status`: a `Program:` line that prints what the descriptor names and one label per state, checked in this order, first match (SC10; MA-629 asserts the order):
  1. missing. When the recorded name is a former name it adds "(<name> was a former name of this program and was removed by an upgrade)" before the repair; the hint interpolates the recorded name and carries no constant former name.
  2. not this program (a temporary copy macOS made of a downloaded app), with the move-to-Applications remedy.
  3. a different build of this program (`exe_version_skew`).
  4. not this program (`exe_moved`).
  5. a former name of this program.
  6. the command-line build of this program (a console shows while it runs).
  7. unchanged since it was installed (not compared with this program).
  8. this program.

  It adds a "scheduled: <kind>" line naming the corroborated kind in the taxonomy's words — verify / run-job / capture / export (§4.6.5(a)) — the same words the Weekly backup card shows (§4.6.3); a `none` descriptor omits the line and an `unknown` one reads "scheduled (kind not readable)" (SC3).
- **The notes and the WARN** (notes are not WARN; the posture is unchanged; nothing here notifies) (SC10):
  - `exe_former_name`: "The schedule runs <path>, a former name of this program. Why: this release keeps that name as a copy (or link); a later release removes it. Do this: <surface>. Nothing changes until you do." The surface part, chosen through `in.Surface`: on the dashboard, "click Repair on the Weekly backup card"; in the wizard, "choose Repair the scheduled backup in the health check"; on the command line, the per-kind remedy above. "It works" is dropped.
  - `exe_console_build`: "The schedule runs the command-line build mailarchive.exe; a console window stays open while it runs. Do this: put mailarchivew.exe beside it and choose Repair."
  - `exe_version_skew` (WARN): "the schedule runs <rec>, a different build of this program than <self>: one file was replaced without the other. Do this: reinstall from the MSI (or copy both files from one release), then Repair."
- **Texts written once where their mechanism lives, and swept here** (SC8, SC17, SC33, SC44): the program note every install and repair surface prints (`schedule.ProgramNote`, §4.6.1) and the translocation refusal (§4.6.1); the console build's pointer and "Press Enter to close this window." (§4.3); the wizard's stderr refusal when it cannot show a dialog (§4.4); the version notice of the handoff (§4.5.3) and the ended-instance line (§4.5.2). The `desktop` verb's `-h` says that `mailarchive.exe desktop` holds the shell and ends with it, and that `mailarchivew.exe desktop` outlives the terminal (SC30).
- **The administrators' texts** (SC28, SC34, SC35, SC37): `packaging/mailarchive-desktop/README-RMM.md`, `DEPLOYMENT.txt`, `README-payload.txt`, `BUNDLE-CONTENTS.txt` and `SOURCE-NOTICE.txt` are renamed with the program and swept (P9, X17).
  - README-RMM §D's verification steps are rewritten for the pair: the shortcuts launch `mailarchivew.exe desktop`; netstat's pid is `mailarchivew.exe`'s; the Weekly backup task runs the `.cmd` wrapper whose line runs `mailarchivew.exe desktop -capture …`.
  - README-RMM §E builds with `make build-windows`, runs `mailarchive.exe desktop` from a terminal and carries SC30's sentence on the instance's lifetime.
  - README-RMM §C ("Detection:") tells the administrator to replace the stored copy of Detect-RMM.ps1 before deploying the release that removes the copies (§7.1). README-RMM's line that `Install-RMM.ps1` "stops every running `mailarchive-desktop.exe`" says what each script stops (§8.6). README-RMM tells administrators to upgrade through a release that ships the copies (the skip path, §7.1), and to schedule upgrades outside the 02:00 and Sunday 03:00 backup window without forcing a reboot on 3010 during it.
  - DEPLOYMENT.txt's "Weekly backup" line names `mailarchivew.exe desktop -capture` and carries the disk line (SC24) and the Repair and restore-and-allow remedy (SC25). BUNDLE-CONTENTS.txt maps the payload to `cmd/mailarchive`.
- **Release notes** (`packaging/release-notes.md`), release N, each line that names a former name saying "former name" (SC47):
  - the former names, the copies (and the `.app` link) kept until the floor (§7.1), and the repair per kind (SC37);
  - the Windows asset is the zip of install names, or the MSI (SC11); on macOS, drag the app into Applications before opening it, and the Terminal link of §7.2 (SC14);
  - the disk figures for this release and for the first release past the floor, measured by L-C1 (SC24);
  - the single point of loss: one file, `mailarchivew.exe`, carries every Windows schedule and shortcut, and the installer's Repair, after restore-and-allow when antivirus removed it, is the remedy (SC25);
  - rollback on the MSI is uninstall, then install, with the remedy that works at N-1 for each kind; the hand rollback is: delete `mailarchivew.exe` (and `mailarchive` where it replaced nothing), then reinstall the schedule from the old program (§7.4) (SC13, SC36);
  - the backup-window guidance and what a run in flight at upgrade becomes (§7.1) (SC34);
  - the stored RMM detection rule must be replaced before the release that removes the copies (SC35); pinned taskbar items, Start pins and owner-made shortcuts to `mailarchive-desktop.exe` (a former name) stop working when the copies go, and the owner pins the Start-menu shortcut again (SC38); the skip path (SC37).

  The last release note before SE4 repeats the detection-rule, pin and skip-path lines as "before you upgrade" lines; SE4's row is held until release N's note and README-RMM have shipped them (SC35, SC38). SE4's note says that a schedule still on a former name is RED and does not run, that nothing pushes, and tells the owner to run `mailarchive status -out <archive>` for every archive that has a schedule; SE4's removal is coupled to the Q19 migration inventory (13.44) (SC5, SC38, SC41). From Q11 (b) (13.44) a best-effort notification per affected schedule across the compatibility period carries the warning, with the release notes as the backstop; the payload README says the same in release N (SC38).
- **The phrase rule** (SC47). Texts name `mailarchive`, `mailarchivew.exe` and `Mail Archive.app`, and each surface by its verb. A former name (`mailarchive-gui`, `mailarchive-desktop`, with or without `.exe` or a release-asset suffix) appears only on a line that also says "former name", and never as the thing to run, double-click, download or build. MA-622 sweeps the enumerated set of §9.2 (the texts above, the README, README-macOS, the Product.wxs shortcut names and descriptions, and the string literals of the owner surfaces); the `status` `Program:` value and the `exe_former_name` path are runtime values proven by MA-616, not swept. The release-N machine scripts sit outside the sweep, pinned by MA-376 and MA-614. The phrase rule is the only exemption for text; X17's two excluded non-program tokens (§9.2) are part of its proposed definition, which the operator accepts or rejects at SE0 (OP-6) (SC47, SC28).

## 7. Migration and coexistence

### 7.1 Windows (MSI)

**Release N and the floor.** *Release N* is the first minor tag (vX.Y.0) that ships the former-name copies and the `.app` link; patch tags do not count. The copies and the link stay in every tag until *the floor*: a minimum number of minor versions after N and a minimum time after N's publication, whichever is later, which the owner sets (Q9; recommended two minor versions and six months). Until Q9 is answered the copies and the link stay in every tag and SE4 does not ship. Every lifetime of the copies in this design ("until the floor") points here. The MSI's `ProductVersion` is bound to the tag by the built step of §4.10 (`build-release.sh` writes it into the payload, `Build-MSI.ps1` reads it and refuses a differing `-Version`, MA-614), so the floor and every Windows claim about release N and the release after it rest on that step, not on the builder typing the version; L-W3 still confirms the emitted MSI's `MajorUpgrade` behaviour on real Windows (§4.10).

Release N (SE1+SE2, which ship as one release, §11) (SC42):

1. The MSI installs `mailarchive.exe`, `mailarchivew.exe`, and `mailarchive-desktop.exe` and `mailarchive-gui.exe` as **copies** of `mailarchivew.exe` (two more plain components, each a `File` of its own payload source with the same bytes as `mailarchivew.exe`; D3). Shortcuts point at `mailarchivew.exe desktop …`, and the *MailArchive wizard* Start-menu shortcut runs `mailarchivew.exe wizard` (SC20). `util:CloseApplication` names `mailarchive-desktop.exe` only, as today: that is CC22's pre-upgrade instance, the one that may serve 127.0.0.1:8097 with no private link. Because its Target is the image name, it also ends an un-repaired former-name weekly-backup capture running as `mailarchive-desktop.exe -capture …` (item 2's legacy spelling), as the unfiltered `mailarchive-desktop` stop does; that run reads RED `run_never_finished` until the next completes from its checkpoint (R5, item 5), conditional on L-W3. A running `mailarchivew.exe` (a dashboard of release N or later, a `run-job`, a `desktop -capture`, a `wizard`) is left to Windows Installer's files-in-use handling, as a running `mailarchive.exe` is today (SC34). **Lab item L-W3:** the upgrade N-1 to N with a schedule of each kind, a field archive both surfaces scheduled, an instance running in two sessions (CC22's existing lab case), a run in flight through Install-RMM.ps1 and through plain `msiexec /qn`, the exit code and Restart Manager, the build of each file on disk until reboot, the installed file times of the copies, and what the size and mtime fallback reads.
2. An installed weekly backup (`mailarchive-desktop.exe -capture …`) keeps running: the copy runs the legacy spelling (§4.1). An installed wizard schedule (`mailarchive-gui.exe -job …`) keeps running the same way. The health view of each schedule's own archive shows the `exe_former_name` **note** with the repair its kind offers: the dashboard assesses its configured archive, the wizard the archive in `last-archive.txt`, and `status -out` the archive named, so a schedule on an archive no view the owner opens assesses is shown only by `status -out` (§8.2) (SC5, SC3). On the MSI, nothing is RED for a schedule on upgrade day, conditional on L-W3, unless the upgrade ended a run (items 4 and 5) (SC12, SC32, SC34).
3. The owner's click on the Repair the schedule's kind offers (§4.6.3), or a `schedule -install` for a command-line schedule, re-records the schedule's own kind under the new name: a wizard job-file schedule as `mailarchivew.exe -job …` (`run-job …` only on Q16's yes), the weekly backup as `mailarchivew.exe desktop -capture …`, a command-line schedule as `mailarchivew.exe` inside its wrapper (the console build instead, if L-W2 fails for a wrapper, §4.6.1) (SC1, SC5, SC22, SC42). A repair is offered only when the recorded kind is run-job or capture, is corroborated, and the other of the two is not (§4.6.5 (e)); on a field archive both surfaces scheduled, Repair is refused naming the surface that can install each kind, and the owner re-installs the recorded kind from its own surface (the card's Install/update for a capture, the wizard's schedule offer for a run-job); an install of the other kind is refused by §4.6.5's guard until the recorded one is removed. Every repair asks the volatile-directory question first, and Cancel installs nothing (SC9).
4. A desktop instance of the old version still running at upgrade: ended by `CloseApplication` when it runs `mailarchive-desktop.exe` (as today). Only a pre-LC-C2 instance listens on 8097 and takes CC22's path: one in another session gets CC22's PreUpgrade notice (as today). §4.5.2's narrowed test lets the new program end it only on Q10's yes (the former name `mailarchive-desktop` in the running file's directory, same token user, reached only on `FormerPreUpgrade`); until the SE1 amendment lands (Q10 answered yes, 13.44), today's exact-path test stands and the PreUpgrade notice is the outcome (SC31). An instance with the private link (LC-C2 or later, release N-1 included) never listens on 8097, so no kill test reaches it: one in another session is left, keeps the lock, and is named by the handoff notice of §4.5.3 (§7.5) (SC31, SC33). When an older instance is ended, any Archive now, verify or rebuild it was running ends with it: R5 keeps the archive at its last checkpoint, the interrupted run reads RED `run_never_finished` until the next run completes, and the new dashboard's header and `status` say that this launch ended an older MailArchive Desktop of this account (MA-638) (SC32).
5. A run in flight at upgrade. A run the upgrade spares — a `run-job`, or a repaired `mailarchivew.exe desktop -capture` whose command line the Install-RMM.ps1 filter spares — is left to finish; an un-repaired former-name `mailarchive-desktop.exe -capture` is NOT spared (the unfiltered `mailarchive-desktop` stop and `util:CloseApplication` end it, and `status` then reads RED `run_never_finished` until the next run completes from its checkpoint). A dashboard's in-process capture (Archive now) ends with its dashboard. Restart Manager, or an RMM reboot after exit code 3010, can still end a spared run. In each case `status` reads RED `run_never_finished` until the next run completes from its checkpoint (L-W3). README-RMM and the release note say to schedule upgrades outside the 02:00 and Sunday 03:00 backup window and not to force a reboot on 3010 during it. Until reboot, files of the pair may be at different builds (the reboot-pending skew window); `exe_version_skew` reads that state and names the reinstall (§4.6.2) (SC34, SC2).
6. **One file carries every Windows schedule and shortcut.** After SE2, `mailarchivew.exe` carries every schedule of every surface and all five of its shortcuts (Desktop, Startup, Start menu, Copy link and the wizard's). The two former-name files are byte-identical copies of it, lost with it to a hash-based antivirus quarantine or a deletion; `mailarchive.exe` is then the only entry point. What the owner sees: every shortcut fails with Windows' missing-target dialog; no scheduled run starts, so no last-run record is written; a wrapped task (the desktop's, the command line's) leaves cmd.exe's "not recognized" line in `<name>.stderr.log` inside the archive, and the wizard's unwrapped task leaves only Task Scheduler's Last Run Result; the dashboard and the wizard cannot be opened; RED `exe_missing` appears for every archive only when `status` runs from `mailarchive.exe`; nothing is pushed (unchanged); a double-click of `mailarchive.exe` gives the console build's pointer and usage refusal (§4.3). The remedy is the installer's Repair, after restoring and allowing the file when antivirus removed it (the install-folder branch of `exe_missing`, §6); after it no schedule needs re-installing. While the pair is unsigned (Q13 interim) Repair re-installs the same bytes, which an antivirus verdict that still stands may quarantine again. Type II, stated; §5.3 carries the persona; **lab item L-W9** (SC25).

**The first minor tag past the floor (SE4).** The two copies go; the `.app` link follows §7.2 (SC37). A descriptor still naming a former name is RED `exe_missing` in every health view, with the hint that it was a former name removed by an upgrade and the repair its kind offers; the schedule does not run and nothing pushes (P5) (SC10, SC41). The owner learns of the break by opening the health view of that schedule's own archive or by running `status`, on every OS, and — from Q11 (b) (13.44) — by one best-effort notification per affected schedule across the compatibility period from a successful run under a former name; §12 labels "loud at N+1" conditional (SC5, SC38). **Q19 answered (13.44):** SE4's removal of the copies is coupled to Q9's floor **and** a per-user inventory of the archives a schedule was installed for — SE4 removes a former name only where that inventory, populated from existing trusted records and walked by both health views, shows the archive migrated (option a); where migration cannot be established the copies stay longer (option c, Q9); the inventory gate (option b) applies only where it can actually be checked. The inventory is new per-user state (§4.9) whose mechanism, rows and review are designed at its slice (around SE4) and whose location needs OPEN-X1 (OP-7). For a fleet, `status -out DIR`'s `Program:` line is the scriptable detection; the repair is per user and interactive (the dashboard's or the wizard's Repair), or the scripted `schedule -repair -out DIR` (Q12, landing before SE4); administrators hold the SE4 release until detection finds no former name (SC38). Release N's note and the last note before SE4 carry, on lines that say "former name" where they name one (SC47):
- pinned taskbar items, Start pins and owner-made shortcuts to `mailarchive-desktop.exe` stop working when the copies go, and the owner pins the Start-menu shortcut again (**lab item L-W12**) (SC38);
- the stored RMM detection rule must be replaced before deploying the release that removes the copies: the older stored rule requires `mailarchive-desktop.exe` and, left in place, reports every endpoint as not installed and reinstalls on every cycle without passing. Release N's Detect-RMM.ps1 names no former name and passes unchanged at N and after SE4 (§8.6). The stored rule is outside every path that turns RED; the release note and README-RMM are its only mitigation (Type II for an administrator who reads neither text, §12). SE4 waits until that text has shipped (SC35);
- the skip path below (SC37).

**The skip path.** An install that reaches a post-SE4 release without having run a release that ships the copies (an RMM fleet skipping the floor, a late download) gets no note phase: every un-repaired schedule is RED `exe_missing` with the repair remedy until the owner repairs it. README-RMM tells administrators to upgrade through a release that ships the copies. **Lab item L-W11:** the MSI from the pre-N build straight to the SE4 build reads RED `exe_missing` with the repair, and the repair restores the schedule (SC37).

**Disk cost.** Today the MSI installs three files of 86.4 MB at 85c4c41 (31,047,680 + 25,177,088 + 30,160,896 B). Release N installs four: the pair plus two copies of `mailarchivew.exe`; at L-C1's estimate of 31 to 33 MB each (the desktop's build already links nearly everything), about 124 to 132 MB, one file and about 40 MB more than today. In release N the payload zip and the MSI carry the `mailarchivew.exe` bytes three times, because build-release.sh zips each entry on its own and Product.wxs gives each copy its own File. The first release past the floor installs the pair, about 62 to 66 MB, one file and about 20 MB less than today; only that release saves disk. Whether the two former-name files become WiX CopyFile (DuplicateFile) entries is the owner's; the default is plain components (D3), which L-W3 tests. (**Lab item L-C1:** the merged file on each OS; the installed Windows total for N and for the release past the floor; the release-N payload zip and MSI against the 85c4c41 build, whose payload zip is 45,438,957 B; whether WiX de-duplicates identical File sources in one cabinet.) The measured figures go to the README's "One program" paragraph, the release notes and DEPLOYMENT.txt (SC24).

### 7.2 macOS (`.app` and the CLI tarball)

- `Mail Archive.app/Contents/MacOS/mailarchive` is the program; `mailarchive-gui` beside it is a symbolic link to it until the floor (§7.1; whether the macOS link stays longer is part of Q9, and SE4 for macOS is held on that answer) (SC37), so a launchd plist that names the old path keeps running, for a bundle at a stable path (SC14). The program's no-argument verb inside the bundle is the wizard, as today (§4.3; the Q2-mac interim) (SC16, SC19). The `mailarchive-gui` link is a former name, so §4.1 rule 6 decides it, reading the unresolved invoked path (L-M2): no argument gives the wizard, `-job F` gives `run-job F` (SC15). **Lab item L-M1/L-M2:** the `.app` with a one-binary executable opens on first and second launch with the same right-click-Open flow as today, and launchd runs the program through the link; `os.Executable` through the `/usr/local/bin` link and the `.app` link, before and after `EvalSymlinks`; the note and the Repair offer (SC14).
- The app is moved before it is opened. The README, README-macOS, the tarball's READ ME and the release notes say, in order: drag `Mail Archive.app` into Applications, then open it. An unmoved, quarantined `.app` runs from a temporary copy macOS makes of it (app translocation); a schedule install or repair from it refuses with the text of §4.6.1 and writes nothing (D1). **Lab item L-M4:** translocation of a quarantined, unmoved `.app` on each supported macOS version; until it runs, the `/AppTranslocation/` path shape is a lab item, not proven (SC14, SC8).
- On a Mac with the `.app`, the Terminal user runs the program inside it, through `sudo mkdir -p /usr/local/bin && sudo ln -s "/Applications/Mail Archive.app/Contents/MacOS/mailarchive" /usr/local/bin/mailarchive`. The CLI tarball is for a Mac without the `.app`. Two copies on one Mac are two installs, and a schedule belongs to the copy that installed it. README-macOS replaces the `cd mailarchive-cli-macos … ./mailarchive` steps for an owner who has the app. The linked program typed bare in Terminal prints the usage refusal, through §4.3's terminal predicate (SC14).
- The CLI tarball ships the same file. The README's "do not double-click the command-line program" text stays: a bare Mach-O still opens in a text editor.
- macOS carries the dashboard for the first time: build-app.sh builds only `./cmd/mailarchive-gui` and `./cmd/mailarchive` for darwin, so SE1 is the first release to put the dashboard and its `desktop -capture` headless mode in front of a Mac owner. It is reached as `mailarchive desktop` from Terminal and is labelled "not lab-run on macOS". Making it the `.app`'s double-click is the macOS flip slice (§11), gated on the second-open lab (L-M5), the macOS dashboard lab (L-M6), §8's macOS walk and its friction review, and Q2-mac answered "the dashboard"; that slice ships its own wizard route ("Open MailArchive wizard.command" in `MailArchive-macos.zip`, **lab item L-M7**), or the `.app` stays the wizard (SC19, SC20).
- The wizard's health check of its last archive (SC5) already offers the repair when the program is missing or moved, and offers it on the link's former-name note (§4.6.3) (SC9); after the floor removes the link, a plist naming the old path is RED `exe_missing` until repaired, and nothing pushes (SC37, SC41).

### 7.3 Linux

Bare files; owners copy them where they like. An owner who replaces a former-name file (`mailarchive-gui-linux-amd64`, `mailarchive-gui-linux-arm64` or `mailarchive-desktop-linux-amd64`, each a former name of `mailarchive`) with `mailarchive` has a crontab naming a file that is gone. The schedule stops on its first run after the file goes. Nothing the product owns reports it, because the program never starts: no last-run record, no `.jobfail.log`, no notification. The stop is discoverable: RED `exe_missing` in `status`, in the wizard's health check of its last archive and in the dashboard's health view of its configured archive, and cron's mail where the machine delivers it (**lab item L-L4**). The backup stays stopped until it is repaired (SC41, SC5). The wizard's health check offers the repair on the next `mailarchive wizard`.

The README says, and a text assertion pins (SC41, SC44, SC3, SC47):

> Copy the new file, then at once run `mailarchive status -out <archive>`. Repair from your desktop with `mailarchive wizard` (it needs the zenity tool and a desktop session), or by the remedy `status` names for the schedule's kind. Until you do, the schedule does not run and nothing tells you.
> `mailarchive-gui-linux-amd64` and `mailarchive-gui-linux-arm64` are former names of `mailarchive`.
> `mailarchive-desktop-linux-amd64` is a former name of `mailarchive`.

`mailarchive schedule … -install` is no longer named as the repair: it repairs only the schedules the command line installs (§4.6.3) (SC3). On a box without `zenity` or a desktop session the wizard refuses on stderr and shows no dialog (§4.4, MA-639, L-L2) (SC44), so the only repair for a wizard schedule there is the lossy remove-and-reschedule of the command-line remedy (§6): remove the schedule by name, then schedule the export, with the job's settings read from the job file. From Q12 (13.44) a scripted `schedule -repair -out DIR` repairs a wizard schedule here too — landing before SE4 and preserving the job, refusing without mutation when it cannot; the lossy remove-and-reschedule is the pre-Q12 fallback (SC3, SC38).

The click route to the wizard is a README `.desktop` entry (`Exec="<where you put it>/mailarchive" wizard`, `Terminal=false`), with the file manager's trust step, whatever Q4's answer is; L-L1 verifies it (SC20). The no-argument double-click is honoured once L-L1 passes both ways (Q4 answered honour, 13.44), and the release provides a `.desktop` launcher template with an explicit verb as the predictable route (SC16). A no-argument launch that would open the dashboard asks first; that question is owed by the slice that makes the Linux no-argument verb `desktop`, with the SE3 design (§4.3) (SC18). arm64: `mailarchive-linux-arm64` gains the wizard and the desktop, compiled, not lab-run until L-L1 and L-L2 run there; the dashboard has never shipped on linux/arm64 (SC19).

No copies, no links: the owner placed the file; the program cannot know where.

### 7.4 Mixed versions and rollback

- Older files read everything the new ones write in SE1/SE2: the job file stays version 1 and the descriptor gains no field. The instance file's optional `version` and `ended` fields and its `former` kind `ended` (§4.5.3) are the only additive changes, and every reader since LC-C2 decodes the file non-strictly (SC32, SC33). MA-623 proves the readers (SC13, SC39).
- **On the MSI, rollback is uninstall, then install.** The MSI refuses a downgrade (Product.wxs `MajorUpgrade` with `DowngradeErrorMessage`, unchanged; DC6), so rollback is `msiexec /x` of N, or Uninstall-RMM.ps1, followed by the N-1 MSI. The archive, `%LOCALAPPDATA%\MailArchiveDesktop`, the Credential Manager sign-in, the job files under `job.Dir`, the wrappers and the scheduled tasks survive (README-RMM "What uninstall preserves"). Schedules not repaired at N keep running, because N-1 installs the real former-name files. **Lab item L-W10:** MSI N to N-1 by uninstall then install, with an instance running in two sessions: that per-user state survives, that unrepaired schedules run, that repaired ones are RED `exe_missing` and the named remedies restore them, and whether `CloseApplication` runs during `msiexec /x`. It decides the rollback release note and P10's label (SC36).
- A schedule repaired to the new names stops running after a rollback that removes the new-name file (the MSI rollback above, or a hand rollback): RED `exe_missing`. The remedy is the old binary's own repair for that kind (SC3, SC36). N-1's `exe_missing` text still says to re-run `mailarchive schedule ... -install`, which fits only a command-line schedule:
  - a wizard job-file schedule repaired to `mailarchivew.exe -job …` (`run-job …` on Q16's yes) is fixed by the old wizard's "Repair the scheduled backup" (`repairable` on `!ExeExists`, `repairSpec` rebuilding `-job <path>`). That remedy applies to wizard job-file schedules only: on a dashboard weekly-backup descriptor it would install a `-job` schedule naming a job file that does not exist;
  - the weekly backup repaired to `mailarchivew.exe desktop -capture …` is fixed by turning the weekly backup on again in the old dashboard (`scheduleInstall` and `backupSpecFor`);
  - a command-line schedule is fixed by the old binary's `schedule -install`, which cannot express `-job`, `run-job` or `-capture`.

  The hand rollback (the zip, or a bare file on macOS or Linux) is: delete `mailarchivew.exe` (and `mailarchive` where it replaced nothing), then reinstall the schedule from the old program. Stated in the release note (P10) (SC13).
- A pre-SE2 `status` reading a post-SE2 descriptor that names `mailarchivew.exe`, run from `mailarchive.exe`, reads "this program" and is silent while that file is present and unchanged (the size and mtime fallback), and the schedule keeps running release N's file; it reads RED `exe_missing` once the file is removed, and WARN `exe_moved` only if the file was rewritten in place. MA-623 proves each outcome through the predicate of §4.6.2 with the running program outside the recorded file's directory (SC13).

### 7.5 Running instances and the lock

The lock is per account and configuration (`instance.Acquire`), not per file name; a `desktop` of the new program and an old `mailarchive-desktop` of the same account contend for the same lock and the old one wins until it exits. A healthy same-configuration holder answers the handoff, so no CC21 refusal fires. After a verified handoff, the launch names a holder of another version, or of no recorded version, with its program path and pid, through the notice of §4.5.3, and still hands off (MA-637). The holder keeps the lock until it is quit or the owner signs out (Type II, stated) (SC33). On a Windows MSI upgrade from a pre-LC-C2 release, `CloseApplication` ends it first; a release-N `mailarchivew.exe` dashboard left running under §7.1 item 1 is named by this notice (SC33, SC34). The lock, the session id, the handoff key and the `0600` file are unchanged; the instance file gains only the optional `version` and `ended` fields and the `former` kind `ended` (§4.5.3).

### 7.6 Windows without the MSI (the zip)

The Windows release asset is `mailarchive-windows-amd64.zip`, holding `mailarchive.exe`, `mailarchivew.exe` and `READ ME.txt` under their install names, the same files as the MSI payload (§4.10; OP-5) (SC11). There are no copies and no shortcuts. The wizard's click route for the zip owner is a hand-made shortcut to `mailarchivew.exe` with ` wizard` added to its Target, named in one README line (a Type II residual, §8) (SC20). The pair from the zip is one program to `ProgramForSchedule` and `status` only when both files are beside each other under their install names, with the running file's owner and the same build (§4.6.0); **lab item L-W7** records a zip extract's owner (SC7, SC12).

A descriptor naming a former release asset (`mailarchive-gui-windows-amd64.exe` or `mailarchive-desktop-windows-amd64.exe`, each a former name) keeps running that older file. Under §4.6.2's predicate a former release asset is a former name (the prefix test), so it reads WARN `exe_moved` with its kind's repair while the old file is present, and RED `exe_missing` with the repair once the owner deletes it; the size and mtime fallback never makes it this program (SC2, SC11). Because the WARN offers the repair (§4.6.3), the README's upgrade line says: unzip into the same folder, choose Repair in the health view of each scheduled archive, then delete the old downloaded `.exe` files (each a former name). §12 scopes "Upgrade day is never RED" to the MSI and macOS (SC11).

## 8. Operator walks

1. **Windows owner, double-click.** Installs the MSI; double-clicks "MailArchive Desktop" (→ `mailarchivew.exe desktop`); the browser opens at the private link (as today). Steps: one double-click after the install. Turning on the Weekly backup records `mailarchivew.exe desktop -capture …` inside the desktop's always-on `.cmd` wrapper, so each Sunday run shows today's brief cmd.exe console (non-goal 7, D2). A wizard schedule whose run string fits `SchtasksRunLimit` runs `mailarchivew.exe` itself and shows no console (L-W2); a longer one gets the wizard's wrapper and its brief console, and the wizard's note says so (SC22). The *MailArchive wizard* Start-menu shortcut opens the wizard (→ `mailarchivew.exe wizard`) (SC20). The add-mail steps are the SE3 design's measured friction walk (SC39).
2. **Windows owner, upgrading from release N-1 with a schedule on each of two archives** (SC5). A desktop capture on D:\M365 (the dashboard's configured archive) and a wizard schedule on D:\Thunderbird (the wizard's last archive, local-only). Runs the MSI; the running dashboard (`mailarchive-desktop.exe`) is closed by `CloseApplication` and replaced; clicks the Desktop shortcut. If an older dashboard of this account survived in another session, the launch names it through the version notice (§4.5.3) and the page that opens is that version's until it is quit or the owner signs out (SC33). The Status & health card of D:\M365 shows one note: "The schedule runs <path>, a former name of this program. … Do this: click Repair on the Weekly backup card. Nothing changes until you do." The owner clicks Repair (answering the volatile-directory question if it is asked, §4.6.3) and the capture kind is re-recorded as `mailarchivew.exe desktop -capture …` (SC1, SC9, SC10). Then the context switch, counted: the D:\Thunderbird schedule is shown only by a health view of that archive. The owner opens the *MailArchive wizard* shortcut (or runs `mailarchive.exe wizard`, or the `mailarchive-gui.exe` copy, a former name, until the floor); the wizard's health check of its last archive shows the note; the owner chooses "Repair the scheduled backup" and the run-job kind is re-recorded under `mailarchivew.exe` (SC1, SC20). Pointing the dashboard at D:\Thunderbird instead now shows the Weekly backup card for that local-only archive — its kind, health, Repair and Remove (Q18(b), 13.44) — so the owner can repair the run-job schedule there too, without opening the wizard (the step count below is the pre-Q18(b) worst case and the friction review re-measures it) (SC3). Steps: two Repairs and one switch of surface. A wizard schedule on an archive the wizard did not use last is shown only by `status -out` (or by pointing the dashboard at it), and is RED `exe_missing` past the floor if it is not repaired. On a field archive both surfaces scheduled (both kinds corroborated, §4.6.5), Repair is refused naming the surfaces, and the owner re-installs the recorded kind from its own surface (the card's Install/update for a capture, the wizard's schedule offer for a run-job) (SC1). Or the owner ignores the notes: both schedules keep running until the floor (§7.1) (SC37). SE1's and SE2's friction reviews measure this walk as written, the refusal-then-Install step included; neither review is self-certified here (SC5).
3. **macOS owner, Terminal.** `mailarchive status -out ~/Archive` (the `.app`'s program, through the link of §7.2) → `Program: … mailarchive-gui (a former name of this program)`, through `sameFile` and `ExeFormerName` (§4.6.2); `mailarchive wizard` → the health check of its last archive offers Repair (§4.6.3) → accepts → `Program: … mailarchive (this program)` (SC14, SC2, SC9). A `mailarchive` from the CLI tarball elsewhere on the same Mac is a second install, and reads that schedule as not this program (§7.2) (SC14).
4. **Linux owner, no browser on the box, schedule from the shell.** `mailarchive schedule -install -- -input ~/.thunderbird/x/Mail/Local\ Folders -out /srv/archive` as today; `mailarchive desktop` is never needed; `mailarchive status` as today; no `zenity` on the machine and none needed (MA-624). `mailarchive wizard` on this box refuses on stderr with exit 1 and shows no dialog, naming the zenity package and the README's commands (§4.4, MA-639); a failing `run-job` under cron returns promptly (**lab item L-L2**) (SC44, SC46). A wizard schedule on such a box is repaired only by the remove-and-reschedule of §7.3 (SC3).
5. **Inheritor, cold, reading `status`.** `mailarchive status -out X` prints what runs the schedule, its label in the fixed order of §6, and which surface installed it, each with the one remedy its kind offers (SC3, SC10); `mailarchive -h` lists every surface as a verb; there is one program; on Windows it has two files, and the console one, double-clicked, says which file to double-click (§4.3) (SC17).
6. **Windows administrator, RMM** (SC34, SC35, SC28, SC38). `Install-RMM.ps1` stops every `mailarchive-desktop` before `msiexec`, unfiltered (CC22, as today), and stops a `mailarchivew` process only when its command line (`Win32_Process.CommandLine`, read with Get-CimInstance across all sessions) is a dashboard: no argument after the program path, or a first argument `desktop` without `-capture`. It never stops `run-job`, `-job`, `-capture`, `desktop -capture` or `wizard`, adds no `mailarchive-gui` stop, and a scheduled capture mid-run is left to finish, as today. `Uninstall-RMM.ps1` adds `mailarchivew` to its unfiltered list. `Cleanup-CurrentUser.ps1` stops `mailarchive-desktop`, `mailarchive-gui` and `mailarchivew` in the caller's session, unfiltered, ahead of its Remove-Item. Both keep the former names through and after SE4. `Detect-RMM.ps1` exits 0 when `HKLM\Software\MailArchive Desktop` (the unchanged DC6 key) has `Installed = 1` and both `mailarchive.exe` and `mailarchivew.exe` exist under `InstallDir`; it names no former name, so it passes unchanged at release N and after SE4. The administrator replaces the copy stored in the RMM before deploying the release that removes the copies (README-RMM §C, the release note). Verification follows README-RMM §D as rewritten: the shortcuts launch `mailarchivew.exe desktop`, the Weekly backup task's wrapper runs `mailarchivew.exe desktop -capture …`, and netstat's pid is `mailarchivew.exe`'s. Upgrades are scheduled outside the 02:00 and Sunday 03:00 backup window, with no forced reboot on 3010 during it. Before the release past the floor, the fleet detects with `mailarchive status -out DIR`'s `Program:` line for each archive, repairs per user and interactively by the dashboard's or the wizard's Repair, and holds that release until detection finds no former name. MA-614 pins the four scripts' stop lists, Install's filter and verb set, and Detect's rule.
7. **macOS or Linux owner, in-place upgrade with a dashboard running** (SC33). The owner replaces the program file (macOS: the new `.app` dragged into Applications over the old; Linux: the new file copied over the old) and runs `mailarchive desktop` while the old dashboard still runs. The old instance holds the lock and answers the handoff, so the browser opens that version's dashboard, and the launch names it with its program and pid and says to quit it (`kill`, with the pid) or sign out and back in. After that, the next launch is the new version's. On macOS this case exists only from release N onward, because no dashboard shipped there before SE1 (SC19). SE1's friction review records that macOS and Linux have no Quit action in the dashboard.

Walks owed by later slices: the macOS flip slice's double-click walk and its friction review (SC19), and the Linux launch-question walk that counts the extra click (SC18), are written with those slices; a walk that adds local mail from the dashboard is the SE3 design's (SC39).

Friction classification (per friction-review.md): the two-file pair on Windows is **Type I** (the subsystem flag is the control: no console on click, a console in the shell); the copies are **Type II** chosen deliberately to avoid a RED upgrade day, and leave at the floor (§7.1) (SC37); the repair being a click rather than automatic is **Type I** (P5: no schedule rewritten unasked). Also:
- the per-archive repair with its counted switch of surface (walk 2): **Type II**, stated (SC5);
- the console build's double-click pointer and wait: **Type II**, chosen so the click is legible (SC17);
- the wizard's click routes: the *MailArchive wizard* Start-menu shortcut (**Type I**); the README `.desktop` entry on Linux (**Type II**, a one-time setup); the zip owner's hand-made shortcut to `mailarchivew.exe wizard` (**Type II** residual); the `.command` file at the macOS flip (**Type II**, Terminal opens), owed by that slice (SC20);
- the ended in-process capture: a dashboard's Archive now ends with its dashboard at upgrade and reads RED `run_never_finished` until the next run (**Type II**, stated) (SC34);
- the console build's dashboard: `mailarchive.exe desktop` holds the shell and ends with it, so Ctrl-C or closing the window ends the account's instance and an Archive now in flight reads RED `run_never_finished`; non-goal 6 and desktop Q14/OR8 rule out a code fix (**Type II**, stated; L-W4) (SC30);
- an older holder of the lock keeps it until quit or sign-out, and macOS and Linux have no Quit action in the dashboard (**Type II**, stated) (SC33);
- the Linux launch question, one extra click (**Type II**), owed by the slice that makes the Linux no-argument verb `desktop` (SC18).

## 9. Test strategy

### 9.1 What moves

Every test of `cmd/mailarchive-gui` and `cmd/mailarchive-desktop` moves with its code into
`internal/wizard` and `internal/desktop`, keeping its `// covers:` marker; the covers-map
meta-test is the proof that none is lost (a U/S row with no covering test fails). The
`cmd/mailarchive` `TestMain` already builds the real program once; the desktop's and wizard's
process-level refusal tests (exit code and stderr) can now run against it where they ran
in-process before, and the new rows below do.

The tests move in SE1's pure-move commit, with their bodies unchanged apart from the package
clause, the exported entry names and import paths, so `git diff -M b1a16bc` shows each of them
as a rename (§11). `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` (MA-123, MA-166) move
unchanged and keep asserting `-job FILE`; their `-job` assertions change only on an owner yes to
Q16 (SC42, SC43). `TestNotifyFailureHeadless` (MA-124) moves unchanged, because
`RunHeadless(path)` keeps the package-level `notify` sink (SC46). The breadcrumb tests of
MA-167 and MA-168 move with `lastfailure.go` into `internal/wizard` in SE1 and with its reader
into `internal/health` in SE2, keeping their markers (SC4). The packaging test
(`TestPackagingFilesMatchTheNewLaunch`, today `cmd/mailarchive-desktop/packaging_test.go`)
moves to `cmd/mailarchive/packaging_test.go` in the pure-move commit; it reads
`../../packaging/…`, which resolves the same from there, so it outlives the deletion of
`cmd/mailarchive-desktop` (SC34). The moved MA-386 test is strengthened in its own later SE1
commit: the run-job log names "Maintenance → Rebuild search index" (pinned equal to `rebuildButton` by a test in `internal/desktop`, §4.4), `rebuildChoice` and "mailarchive wizard", and no
longer contains "the window that opens with this program" (SC21). Strengthening a check is
allowed; no gate moves.

**The covers map stays green between slices (SC48).** Every U/S row of this design's block that
SE0's catalog-first PR lands (§10, §11) lands with a `t.Skip` encoder that names its id and the
slice that turns it green. The encoder sits in the package where the row's test will live;
where that package does not exist yet (`internal/wizard` before SE1), it sits in the package
whose code moves there (`cmd/mailarchive-gui`) and moves with that code in the pure-move
commit. L rows are pending and get no encoder. SE1 and SE2 each remove the skips from their own
rows' encoders, so the encoder becomes the test. A row whose cases span slices lands with its
earlier slice and keeps a per-case skip encoder, citing the row, for its later cases until the
later slice lands (owner rulings 2026-10-09 §3, FC33's reasoning). In this design that row
is MA-628 (its `SameBuild` call waits for SE2, §4.6.0); on a Q16 yes the recorded-spelling change
lands in SE2 with MA-641's `Args` case, so MA-641 does not span. With SE3 moved to its own design, revision 1's spanning rows
MA-591 and MA-593 no longer span: MA-621 is a structural pin that holds from SE1, and MA-623
lands whole in SE2 (SC39, SC51, SC13). The rows that depended on an owner's answer (MA-619,
MA-642, MA-643, MA-644) now land: the owner answered yes to Q10 (Windows; the Linux half waits on L-L3),
Q14 (a), Q11 and Q12 (ruling 13.44), so each is built in its slice's PR with its encoder (§10).
`go test ./...` stays green on main from SE0 through SE2, and release.yml's green-gates step
never blocks on this design's own rows (SC48).

**The shipped gaps, once the operator accepts them (SC6).** MA-645, MA-646 and MA-647 record three gaps of the
code at `b1a16bc` (OP-1..OP-3). Each encoder is a real-primitive test: a real descriptor in a
tempdir, with the scheduler seam faked only for argv. It asserts the invariant's behaviour and
is skipped with the gap named, citing R14 and R18 (MA-645) or R18 (MA-646, MA-647). Each moves
with its code in SE1 and keeps its marker. MA-645's skip comes off when SE2 builds the one
schedule kind (§4.6.5); MA-646's and MA-647's come off in their own slices, outside this
design. When a skip comes off, the test stays as a permanent guard. If the operator accepts the
three rows (OP-1..OP-3), they land as above; until then the gaps stand unrecorded.

### 9.2 Rows (tier U unless marked)

Ids are provisional: the operator allocates this block, the pre-sync design's shifted block
and every row, lab item and question the review adds in one pass at SE0 (OP-6, SC49), except the ids of OP-1..OP-3, allocated when the operator accepts them (header). Every
row's Covers cell is in §10.

| Id | Rev. 1 | Tier | Slice | Asserts | Cond. |
|---|---|---|---|---|---|
| MA-610 | MA-580 | U | SE1 | every surface is a verb: `desktop`, `wizard` and `run-job` reach `desktop.Launch`, `wizard.Run` and `wizard.RunHeadless` in-process; the legacy spellings (`-job FILE` with exactly two arguments, a `-capture`-first list) reach `run-job` and `desktop -capture`; the root usage lists the three verbs | — |
| MA-611 | MA-581 | U | SE1 | the legacy spellings are recognised only in their exact shape and only under the program's own names (`mailarchive`, `mailarchivew`): every other flag-first list reaches `runExport` as today. Under a former name (§4.1 rule 6, the unresolved invoked base name, injected): `mailarchive-desktop` with `-copy-link`, `-no-browser`, `--capture …`, `-config X -capture …`, `-capture=true` or the empty list reaches `desktop` with the same argv; `mailarchive-gui` with `-job F`, `-job=F`, `--job F` or `--job=F` reaches `run-job F`, with no `-job` reaches `wizard`, and with an unknown flag gets today's usage refusal and exit code; none of these reaches `runExport`; the same lists under the program's own names are unchanged by rule 6; `invokedBase` is the unresolved `os.Executable()` on Windows and macOS and the base of `os.Args[0]` on Linux | SC15 |
| MA-612 | MA-582 | S+U | SE1 | no self re-exec. **S:** following MA-378's AST-walk idiom, every process-start site in non-test code under `cmd/` and `internal/` (`exec.Command`/`CommandContext`, an `exec.Cmd` literal or its `Path` field, `os.StartProcess`, `syscall.Exec`/`StartProcess`/`ForkExec`, the Windows `CreateProcess`/`ShellExecute`) names a literal from the closed set of OS tools (crontab, schtasks, launchctl, open, xdg-open, rundll32, explorer.exe, osascript, the clipboard table, zenity) or sits on a pinned allowlist of argv builders whose returned names the walk checks are literals from that set; the walk fails on `os.Executable()`, `schedule.ProgramForSchedule()`, `os.Args`, a sibling `filepath.Join`, the scheduler's run-now words ("/Run" with schtasks, "start"/"kickstart" with launchctl) and those values passed directly into an allowlisted opener; passing the program's path into a `schedule.Spec`, the instance file or a display stays allowed; fail-closed twin: a planted `exec.Command(os.Executable(), "wizard")`, a sibling join and an `exec.Cmd{Path: exe}` are each flagged at their line; positive twin: the healthy tree passes with a non-zero site count. **U:** recording fakes of `mailarchivew` and of the former names, beside a tempdir copy of the built program and on `PATH`, are never invoked by any headless verb's happy path; a fake invoked by hand is recorded (the positive twin). A tool another section adds to the set is a design change, reviewed with the walk's pin | SC52 |
| MA-613 | MA-583 | S | SE1 | the verb and flag walk (each new verb has its own `-h`; MA-39's walk extends to them) and the exit-code set (the literal codes are a subset of the declared X1 map, as today) | — |
| MA-614 | MA-584 | S | SE1 (edits SE2, SE4) | build and packaging structure, parsed. `build-release.sh` and the Makefile's `build-windows` (with `GUI_WIN_LDFLAGS` expanded) each hold exactly two `GOOS=windows go build` lines of `./cmd/mailarchive`, whose tokens differ only in the `-o` name and in `-ldflags -H=windowsgui` on the `mailarchivew` line (SC23). The payload copies `mailarchive.exe` and `mailarchivew.exe` under their install names, plus the two former-name copies of `mailarchivew.exe` from release N until the floor (§7.1). Product.wxs's shortcuts target `mailarchivew.exe` with the three `desktop` arguments, and `WizardShortcut` (Name "MailArchive wizard", `Arguments="wizard"`, Directory `ApplicationProgramsFolder`) is pinned as `CopyLinkShortcut` is (SC20). No `util:CloseApplication` targets `mailarchivew.exe`; its one target is `mailarchive-desktop.exe` (SC34). Build-MSI.ps1's `$exes` and `$hashTargets` name exactly the exes the payload and the payload README name (SC23). `build-release.sh` writes the leading-`v`-stripped tag into `PRODUCT-VERSION.txt` beside `PAYLOAD-SHA256.txt` in the payload, and `Build-MSI.ps1` sources `ProductVersion` from it, drops the `1.2.0` default, and exits non-zero on a `-Version` that differs; this row finds both by content (SC37). The Windows release asset is `mailarchive-windows-amd64.zip`, its members are `mailarchive.exe`, `mailarchivew.exe` and `READ ME.txt`, byte-identical to the MSI payload's files, and no other `-windows-amd64.exe` is left in `dist/` (SC11; if the operator declines the zip, OP-5, these cases are not built). `build-release.sh` refuses, before it zips, a Windows release file that is not stamped (no `vcs.revision`, or `vcs.modified=true`), reading `go version -m` of both files; this row finds that check by content, and MA-628 compares the build info of the pair it builds (through `SameBuild` from SE2), so no case of this row needs a clean working tree (SC11, SC7). A release build's main-module version is not "(devel)" (SC33; open condition: that `build-release.sh` builds from a VCS checkout is unverified). `packaging/macos/build-app.sh` zips with `-y`, and the `.app`'s `Contents/MacOS/mailarchive-gui` is a symbolic link to `mailarchive` (SC11). Install-RMM.ps1 keeps its unfiltered stop of `mailarchive-desktop`, stops a `mailarchivew` process only when its `Win32_Process.CommandLine` is a dashboard (no argument after the program path, or a first argument `desktop` without `-capture`), never stops `run-job`, `-job`, `-capture`, `desktop -capture` or `wizard`, and adds no `mailarchive-gui` stop; Uninstall-RMM.ps1's and Cleanup-CurrentUser.ps1's lists cover every GUI-subsystem executable Product.wxs installs plus the former names, and Cleanup's stop precedes its `Remove-Item` (SC34). Detect-RMM.ps1 exits 0 only when the DC6 key has `Installed = 1` and both files of the pair exist under `InstallDir`, and names no former name (SC35). SE4's edits are listed in §11 | SC11, SC20, SC23, SC34, SC35, SC37 |
| MA-615 | MA-585 | U | SE2 | `ProgramForSchedule(wrapped bool) (exe, reason, error)` on each OS, for each of the four names, a suffixed name and a renamed name, and for each reason: self is `Clean(EvalSymlinks(os.Executable()))`, and a non-absolute self is an error; a self under `/apptranslocation/` (lower-case, slash form) is `ErrTranslocated` with no path on every OS (D1); on Windows, `mailarchive.exe` or a former name with a sibling `mailarchivew.exe` that passes `SiblingQualifies` returns the sibling with reason `sibling`, and otherwise self with exactly one of `sibling_absent`, `sibling_not_regular`, `sibling_other_owner` and `pair_out_of_step` (and `wrapped_console` when `wrapped` is true and the L-W2 gate is open, its case added with the operator's SE0 decision, SC6); a running file whose name is not an install name returns itself and looks up no sibling; `mailarchivew.exe` itself, and every other OS, returns self with no reason. Fixtures are real files in a tempdir with an injected `OwnerReader` and self: the genuine copy, another owner, a skewed build (a fixture built with a differing build setting, or a non-Go file) and a non-regular sibling. The Windows `sibling_absent` case asserts the console-build notice (SC25). Nothing from the archive, the descriptor or the job file enters the choice | SC8, SC7, SC11, SC25 |
| MA-616 | MA-586 | U | SE2 | the composed identity predicate (§4.6.2), first match wins, for the backup's and the verify's descriptors, over the composition trace's cases: the MSI N-1 copy with a new size and mtime; the darwin real-symlink fixture named `mailarchive-gui`; a darwin text file named `mailarchive-gui`; a linux old build under a former name with matching recorded size and mtime, both `mailarchive-gui` and the former release asset `mailarchive-gui-linux-amd64` (WARN `exe_moved`, never the fallback); a running file that is a renamed copy beside a recorded `mailarchivew.exe` (WARN, never the fallback); a Windows skewed sibling (`exe_version_skew`); another owner; another directory holding a same-build, same-owner copy named `mailarchivew.exe` with a zero or mismatched `ExeMTime`; case, 8.3, `\\?\` and symlinked-parent spellings; a recorded path under `/AppTranslocation/`; and a running file whose name is not an install name, which is never "this program" by name (SC11). `exeIdentity` returns its basis; `Input.ExeFormerName`, `Input.ExeConsoleBuild` and their verify twins are set as §4.6.2 states. Before the directory bound holds, the touches of the recorded path are today's stats of it (rules 1 and 3) and one stat of its directory, all through the `statProgram` seam family; the `Lstat`, the owner read and the build-info read happen only after it; `TestStatusDoesNotStatAUNCProgramPath` is adapted to the struct and its assertion is unchanged. The texts: the `exe_former_name` note with each surface's "Do this" and "Nothing changes until you do.", the `exe_console_build` note, the `exe_version_skew` WARN, and the missing-former-name hint, which interpolates the recorded name and carries no constant former name. MA-466's existing fixtures (names outside the name set) keep their outcomes unchanged (D4) | SC2, SC7, SC10, SC11, SC12 |
| MA-617 | MA-587 | U | SE2 | a repair keeps the schedule's kind, from each surface: for a run-job, a capture and an export descriptor, from the wizard and the Weekly backup card, a repair rebuilds its own kind (run-job through the moved `wizard.RepairSpec` with the corroborated job path; capture through `backupSpecFor`, from the card only) from trusted inputs and `ProgramForSchedule()`, never from the descriptor, or refuses with the scheduler entry unchanged and the descriptor byte-identical (`assure.Refused`, positive twin first); a capture install or repair when Microsoft 365 is not set up refuses with `HeadlessCapture`'s not-configured text before `schedule.Install`; the card's Remove of a run-job also deletes the archive's job file; a repair from a Downloads path asks the volatile-directory question first and installs nothing on Cancel (SC9); from the command line, `schedule -install` re-installs an export descriptor and, for a run-job or capture descriptor, is refused by the kind guard (MA-631) with nothing written; a `schedule -repair` case is added only with MA-644 on a Q12 yes | SC1, SC3, SC9 |
| MA-618 | MA-588 | U | SE2 | when the wizard's and the dashboard's repair is offered: `Input.ExeRepoint` offers it on the `exe_former_name` note, on the darwin symlink fixture and on `exe_console_build` once the sibling is back, and not while the sibling is absent or on basis `unchanged_since_install`; a repair is offered only when the recorded kind is run-job or capture, is corroborated, and the other of the two is not; the wizard's repair is not offered and installs nothing for a capture descriptor, or for a run-job descriptor with no job file, after the healthy run-job twin passes (SC3); every repair asks the volatile-directory question first, and Cancel installs nothing | SC9, SC1, SC3 |
| MA-619 | MA-589 | U | SE1, only on a Q10 yes | the narrowed "our own program" test (§4.5.2): it accepts the former name `mailarchive-desktop` (`.exe` on Windows) in the running file's directory with the same SID, reached only when `probeFormer` returns `FormerPreUpgrade`, and refuses the pair's names, `mailarchive-gui`, another directory and another SID; negative twin: a same-name, same-directory listener that fails the `probeFormer` fingerprint is never ended. On a no to Q10 the row is not built and is deleted with L-W6 | SC31 |
| MA-620 | MA-590 | U | SE1 | the no-argument rule `defaultVerb(goos, consoleAttached, consoleIsOwn, inBundle, noTerminal, hasDisplay)`, consulted only under the program's own names with an empty argument list, every row of §4.3 with its inputs injected. Windows: with no console, any name (the suffixed `mailarchivew-windows-amd64` and a renamed `MailArchive (1)` included) gives `desktop`; with a console shared by a shell, today's usage refusal unchanged; when the program is the only process in its console, the pointer naming the GUI file by the name it has (or only the shortcut when there is none), then today's refusal line and exit 1, then "Press Enter to close this window." and one line read, EOF ending the wait; an injected-reader test shows the pointer and the wait happen only in that case, and the sibling is never started (SC17). macOS: the bundle path with no terminal gives `wizard`; a `PATH` symlink to the bundle binary started from Terminal gives the usage refusal. Linux: no row under Q4's interim, so the empty list gives the usage refusal, and a pipe, `</dev/null` from a terminal and `nohup` give the usage refusal. Each per-slice verb is pinned for its slice. The Linux row, once the owner answers Q4 "honour" and L-L1 passes both ways, and the cases of a later slice (SC18's launch question with a fake `zenity`: OK runs `desktop`, Cancel or timeout gives the usage refusal with the lock file absent, `assure.Refused`; the macOS flip slice's bundle row) are added to this row by the slice that owes them | SC16, SC17, SC18, SC19 |
| MA-621 | MA-591 | S | SE1 | no new per-user state location: the set of non-test files that compute a per-user state directory (call `os.UserConfigDir` or read `LOCALAPPDATA`/`XDG_STATE_HOME`) equals the set pinned at `b1a16bc`, restated in the pure-move commit with each moved file's new path and nothing else; fail-closed twin: a planted new call site is flagged | SC51 |
| MA-622 | MA-592 | S | SE1 | X17's enumerated sweep. Swept: README.md; packaging/release-notes.md; README-RMM.md, DEPLOYMENT.txt, README-payload.txt, BUNDLE-CONTENTS.txt and SOURCE-NOTICE.txt; README-macOS.txt; Product.wxs `Shortcut` Name and Description (parsed); and the string literals of the owner surfaces (`addMailHelp`, the dashboard templates including `backupOff`, the health remedies, the wizard's texts, the refusals, the `-h` usage), not Go comments. Matcher: `mailarchive-(gui\|desktop)` as a program or asset token, excluding two non-program tokens, the directory `packaging/mailarchive-desktop/` and the MSI payload asset name (unless §4.10 renames that asset), as part of X17's proposed definition, which the operator accepts or rejects at SE0 (OP-6). Phrase rule: a former name may appear only on a line that also says "former name", and never as the thing to run, double-click, download or build; there is no other skip, allowlist or suppression. The five administrator texts' release-N paragraph, headed as the former names kept as copies for release N, passes by that rule (SC28). The release-N machine scripts (Install-RMM, Uninstall-RMM, Cleanup-CurrentUser, Detect-RMM, Build-MSI, build-release.sh, build-app.sh, and Product.wxs `File` and `CloseApplication`) sit outside the sweep, pinned by MA-376 and MA-614. Also: no owner text says which surface a no-argument launch opens ("the window that opens with this program", "open Mail Archive", "re-open this program") (SC21); every file the README tells the owner to download or double-click is emitted by build-release.sh or is a member of an archive it emits (SC11); the SE1/SE2 add-mail text names the MailArchive wizard and the Start-menu shortcut MA-614 finds, and no SE1 or SE2 text names "Add mail from this computer" (SC21, SC39). Green on the texts SE1 ships | SC47, SC21, SC28, SC11 |
| MA-623 | MA-593 | U | SE2 | rollback reads every file: an older-shaped reader decodes the descriptor (no new field), the version-1 job file and the instance file carrying the optional `version` and `ended` fields and the `former` kind `ended` (non-strict `json.Unmarshal`, SC32, SC33); and a pre-SE2 reader's outcomes for a descriptor naming `mailarchivew.exe`, by calling the composed predicate with self outside the recorded file's directory (its rules 1, 6 and 7): "this program" and silent while the file is present and unchanged, RED `exe_missing` once it is removed, WARN `exe_moved` only if it was rewritten in place. It proves the files, not the installed product's rollback procedure, which is L-W10 (SC36) | SC13, SC32, SC33, SC36 |
| MA-624 | MA-603 | U | SE1 | no dialog from a command-line verb: a fake `zenity` on `PATH` is never invoked by `status`, `verify`, `serve`, `search`, `reindex`, `schedule`, `graph`, `extract`, `setup` or the export verb; `run-job`'s only `zenity` exec is the failure notification: exactly one invocation, with argv beginning `--notification`, for a failing run, and none for a healthy one (MA-124). The probe is Linux-only; macOS notifies through `osascript` and Windows natively, which L-C2 checks | SC46 |
| MA-625 | MA-604 | L, pending | SE1, SE2 (L-W11, L-W12: SE4) | the Windows lab items L-W1..L-W14 (L-W6 only on a Q10 yes, L-W13 only on a Q11 yes), run in README-RMM.md §D's hand-validation round on the MSI builder's machine, which this row cites; results in the lab record (§9.3) | SC29 |
| MA-626 | MA-605 | L, pending | SE1 | the macOS lab items L-M1..L-M4 (L-M1 now first and second open); L-M5..L-M7 are the macOS flip slice's and gate only that slice | SC19 |
| MA-627 | MA-606 | L, pending | SE1 | the Linux lab items L-L1..L-L4 (L-L3 only on a Q10 yes for Linux) | — |
| MA-628 | — | U | SE1 (its `SameBuild` call: SE2) | the pair build, inside `go test ./...` (every CI leg, and release.yml's Test step before build-release.sh): the two `GOOS=windows` lines read from build-release.sh are run into a tempdir; `debug/pe` reads `OptionalHeader64.Subsystem` 3 for `mailarchive.exe` and 2 for `mailarchivew.exe`; `debug/buildinfo` reads equal path, main module, deps and every setting except `-ldflags`. The release environment's `GOFLAGS` and `CGO_ENABLED` apply to both lines equally (stated residual, §12). From SE2 the comparison is `SameBuild` (§4.6.0), a per-case skip until then | SC23 |
| MA-629 | — | U | SE2 | the `Program:` line prints one label per state, checked in this order: missing (with the former-name hint when the recorded name is a former name); not this program (a temporary copy macOS made of a downloaded app); a different build of this program; not this program; a former name of this program; the command-line build of this program (a console shows while it runs); unchanged since it was installed (not compared with this program); this program; and it adds a `scheduled: <kind>` line naming the kind in the taxonomy's words (verify / run-job / capture / export) from the corroborated kind, a `none` descriptor omitting the line and an `unknown` one reading "scheduled (kind not readable)" | SC10, SC3 |
| MA-630 | — | U | SE2 | one rendering of `ProgramForSchedule`'s result: `schedule.ProgramNote(exe, reason)` is empty for no reason, for `sibling` and for `wrapped_console`, and otherwise prints its one note (with the console sentence when the base is `mailarchive.exe`, and the "Do this"), on the CLI's `schedule -install` stdout after a successful install, in the wizard's `offerSchedule` and repair result dialogs (whose console note keys on the recorded file as well as `spec.Wrapper`), and in the Weekly backup card's Install/update and Repair results; a translocation refusal from every caller (`runSchedule`, `offerSchedule`, `repairSpec`, `repairSchedule`, `backupSpecFor`, the dashboard's Repair) writes no job file and no scheduler entry (`assure.Refused`, healthy twin first) | SC8 |
| MA-631 | — | U | SE2 | the kind guard at `installOn`: for each pair of kinds (run-job, capture, export), a non-verify install of kind K under the archive's name is refused when owner-side state corroborates a kind that differs from K (`ev.Corroborated`), or the descriptor names `export` with no owner-side corroboration, before the wrapper, the job file or any scheduler call, in the §4.6.5 text naming the corroborated kind's (or export's) remover, and never on the descriptor-named kind alone; a same-kind re-install yields one entry (R14); an uncorroborated or unknown recorded kind does not refuse (stated residual); a successful capture or export install under a name deletes that name's job file; `installOn` refuses a non-verify install without evidence, and `Spec.Validate` is unchanged; the wizard's `offerSchedule` calls `schedule.CheckKind` before its `job.Write`; the verify rules are unchanged | SC1 |
| MA-632 | — | U | SE2 | a flipped `Job[0]` converts nothing: a wizard descriptor flipped to capture on a machine with no Microsoft 365, a capture descriptor flipped to run-job with no job file, and a run-job descriptor flipped to export each leave the scheduler entry and the descriptor unchanged, and no repair or install converts them (corroboration by owner-side state) | SC1 |
| MA-633 | — | U | SE2 | the per-kind remedies: `schedule.RepairRoute` for each kind and `repairHow` in each reading surface's words, for `exe_missing`, `exe_moved`, `exe_former_name`, `not_installed` and `moved_archive`; `scheduleCommand` appears for the export kind only; no remedy for a run-job or capture descriptor offers `schedule … -install` as the repair of that schedule, the one placed exception being the command line's run-job no-display fallback, which appears only after its `-remove` step and names the job file; for no descriptor, the text names both explicit installs | SC3 |
| MA-634 | — | U | SE2 | the start-failure breadcrumb on every health surface: a real `run-job` against an absent `-out` and against a deleted job file writes the breadcrumb, and `health.Gather` raises RED `start_failed` under MA-168's rule, the same line shown first by `status`, by `status -json` (an additive reason code, version unchanged), by the dashboard's Status & health and by the wizard's health check; no line for another archive's breadcrumb; a reachable descriptor carrying a forged `Desc.Name` (≠ `schedule.DefaultNameFor(out)`) over a real start-failure breadcrumb still shows the RED line on every surface, because the gate keys on `out`, not `Desc.Name`; a later ok run supersedes it; with no archive location set, the dashboard's empty state shows the line; the wizard no longer reads the file itself | SC4 |
| MA-635 | — | U | SE2 | the Windows `exe_missing` remedy's install-folder branch: for a missing known-name file in the running program's own directory, the remedy names restoring it with the installer's Repair (restoring and allowing it first if antivirus removed it); positive twin: every other missing case keeps the schedule re-install remedy | SC25 |
| MA-636 | — | U | SE1 | a shown refusal reaches each channel once, at process level: against the program `cmd/mailarchive`'s `TestMain` builds, `mailarchive desktop -addr <a held port>` exits 1 with the `portBusyText` refusal of §4.5.1 ("mailarchive desktop: -addr …") on stderr exactly once and no "mailarchive: " line; on Linux the dialog seam (a fake `zenity` on `PATH`) receives the text once; a wizard error its dialog showed is not reprinted | SC26 |
| MA-637 | — | U | SE1 | the handoff names a holder of another version: only after `instance.Reach` returns a verified answer (Opened, Busy or Link), a holder whose `version` is absent, or differs from this program's with neither 'dev', gets the notice through the dialog seam naming its program path (control-stripped) and pid, and the launcher still exits 0; positive twin: a same-version holder gets no notice | SC33 |
| MA-638 | — | U | SE1 | an ended older instance is named: after `EndOld` succeeds, the new dashboard's header and `status` carry the line naming the pid and saying a run it held reads RED until the next run; positive twin: no line when nothing was ended | SC32 |
| MA-639 | — | U | SE1 | the wizard verb refuses when it cannot show a dialog: the built program with no `zenity`, `qarma` or `matedialog` on `PATH` exits 1 with the exact §4.4 stderr text, and a fake dialog tool on a second `PATH` directory is never invoked (`assure.Refused`); positive twin: a fake `zenity` with `DISPLAY` set receives the first dialog; a pure table covers the OS, the lookup result and the display environment; errors and recovered panics reach stderr before any dialog | SC44 |
| MA-640 | — | U | SE1 | `run-job` reads its job file securely: a link, a FIFO (bounded, no hang), a group- or other-accessible mode, an oversize file and a planted symlink sink (whose target stays unchanged) are each refused through `jobFail`, with no `<out>/mailarchive.log` written and `app.Run` never started, after a positive twin in which a `job.Write` file runs; the sink is opened no-follow and the notification still fires | SC45 |
| MA-641 | — | U | SE2 | install and repair agree: for the same inputs, the pure `installSpec` and `repairSpec` produce the same `Args`, the same `Exe` rule and the same wrapper decision (`TaskRunLength` against `SchtasksRunLimit`) | SC42 |
| MA-642 | — | U | only on a Q14 (a) yes | a terminal refusal from the console build invokes no dialog | SC26 |
| MA-643 | — | U | Q11 yes (13.44): lands | the migration notification Q11 allows: **one per affected schedule across the whole compatibility period** from a successful run under a former name, none for a healthy or unreachable archive (positive twin first), no login notification, the descriptor byte-identical afterwards; best-effort delivery; its label is conditional on L-W13 | SC38 |
| MA-644 | — | U | Q12 yes (13.44): lands before SE4 | `schedule -repair -out DIR` re-installs the archive's recorded schedule from the archive's own name, the corroborated kind and `ProgramForSchedule()`, never the descriptor's `Exe`, after `job.Read` validates the rebuilt job file; it preserves supported settings and **refuses without mutation** when the inputs are insufficient; with the X1 exit codes | SC38 |
| MA-645 | — | U, xfail | now, on the operator's OP-1 | the wizard's kind-blind repair and offer, and `installOn`'s cross-kind install: `repairable` offers no repair that would rebuild `-job` over a desktop `-capture` descriptor or for a job file that does not exist, `offerSchedule` does not replace a `-capture` entry under the same name, and `installOn` refuses a cross-kind same-name install (R14, R18); skipped with the gap named until SE2 | SC6 |
| MA-646 | — | U, xfail | now, on the operator's OP-2 | a scheduled capture that refuses at start (`HeadlessCapture`'s not-configured or no-location refusal) writes a run record, so `status` does not stay GREEN until `stale` (R18); skipped with the gap named until its own slice | SC6 |
| MA-647 | — | U, xfail | now, on the operator's OP-3 | an absent archive folder (an unmounted drive) is not assessed as a new, empty archive: no `no_schedule` WARN inviting a second install and no "no manifest yet" (R18); skipped with the gap named until its own slice | SC6 |

**Revision 1's map (SC49).** MA-580..MA-593 become MA-610..MA-623 in order; MA-603, MA-604,
MA-605 and MA-606 become MA-624, MA-625, MA-626 and MA-627. Revision 1's MA-594..MA-602 (Add
mail from this computer, SE3) and the paragraph that proposed them leave this design for the
SE3 design, which proposes its own rows (§4.8, SC39). No id a rescue named stands as named
(MA-607, MA-608 and the rest are re-allocated with the block, SC49).

**Reserved, no id.** A later slice may introduce a `Dialogs` interface with a scripted fake
only together with a U golden row, reviewed against `b1a16bc`'s `cmd/mailarchive-gui/main.go`,
per source type, cancel branches included (SC43). That row's id is allocated when the slice is
proposed. Until then the wizard's dialog sequence, items, defaults, titles and texts are proven
unchanged by the recorded move diff, and are lab (MA-95, pending), walked by hand in SE1's
friction review (SC43).

### 9.3 Lab items

This repository has no lab tier (`CLAUDE.md`). The Windows items run in README-RMM.md §D's
hand-validation round on the MSI builder's Windows machine: the L-W items, the extended L-W2
and L-W4 and the loss item L-W9 among them, are appended to §D's list, and MA-625 cites §D.
Results are recorded in a named file: the lab record of
`docs/review-single-executable-predesign.md`, as the pre-sync design records its lab in its
review file. SG3 is unchanged: SE1 does not ship for Windows until the round is recorded, and
no `v*` tag is pushed from a main containing SE1 until then (§11). Shipping SE1 for Windows
lab-pending would relax SG3, so it is the owner's question Q15, not a sentence here (SC29).
The macOS and Linux items are walked by hand on a real Mac and a real Linux desktop and
recorded in the same lab record.

| Id | Question | Decides | Row |
|---|---|---|---|
| L-W1 | the pair differs only in the PE optional header's Subsystem field, the checksum and the build-info bytes (SC23) | nothing (informational; MA-628 proves Subsystem) | MA-625 |
| L-W2 | an unwrapped `mailarchivew.exe -job …` (the recorded spelling, Q16; `run-job …` only on a Q16 yes) started by Task Scheduler as an interactive task shows no console; the desktop's and the command line's always-on `.cmd` wrappers running `mailarchivew.exe` wait for the child, its stderr reaches `<name>.stderr.log`, and its exit code becomes the task's Last Run Result (a forced panic and a forced exit 1); the console build `mailarchive.exe` started directly by Task Scheduler shows a console for the whole run (SC22, SC12) | the Windows release note's wording; whether SE2 records `mailarchivew.exe` inside a wrapper: if any of the three wrapper checks fails, `ProgramForSchedule(wrapped=true)` returns the console build (the console is cmd.exe's either way) and R33's wording is narrowed, at SE0 if L-W2 has run by then, otherwise by the owner under rule 2 (§10) | MA-625 |
| L-W3 | MSI upgrade N-1 to N: a schedule of each kind; a field archive both surfaces scheduled; a `mailarchivew.exe -job …` run (the recorded spelling, Q16; `run-job …` only on a Q16 yes) in flight through Install-RMM.ps1 and through plain `msiexec /qn` (whether it survives, the exit code 0 or 3010, Restart Manager's behaviour under `/qn`); the on-disk build of each file of the pair until reboot and `status` afterwards; an instance running in two sessions (CC22's existing lab case); the copies' file times and what today's size and mtime fallback reads of them (SC34, SC42) | SE1+SE2 ship for Windows | MA-625 |
| L-W4 | `GetConsoleWindow` and `GetConsoleProcessList` for each start: an Explorer double-click of each file under conhost and under Windows Terminal as the default terminal (and whether the tab closes), `cmd`, PowerShell, Windows Terminal, `start mailarchive.exe` and `schtasks`; Ctrl-C and closing the console of `mailarchive.exe desktop` while an Archive now runs (SC16, SC17, SC30) | the Windows no-argument rule; whether the console build's pointer and wait (§4.3) ship, or collapse into today's refusal with the flash stated (SC17); SC30's words | MA-625 |
| L-W5 | a `desktop` or `wizard` refusal from the console build under Windows Terminal, PowerShell and a remote session with no GUI session (SC26) | Q14's urgency; §4.2's residual (the modal duplicate under CC20 as written) | MA-625 |
| L-W6 | only on a Q10 yes: after a real MSI upgrade N-1 to N over an instance `CloseApplication` did not end (never a hand-started old process), what the image path reads and whether the test in force ends it (SC31) | §4.5.2; MA-619 | MA-625 |
| L-W7 | file ownership on Windows: whether a standard account can create a file owned by another SID; the owner of the MSI's installed files; the owner of a zip extract; FAT and exFAT (SC7) | SC7's owner bound (§4.6.0) | MA-625 |
| L-W8 | `EvalSymlinks` and `SameFile` on case, 8.3, `\\?\` and junction spellings (SC2) | SC2's directory comparison (§4.6.2) | MA-625 |
| L-W9 | deletion and Defender quarantine of `mailarchivew.exe` on an MSI install: what the shortcuts show, Last Run Result, `.stderr.log`, `status`, what the installer's Repair restores (its exact path and the `msiexec /fa` form), and whether the file is quarantined again (SC25) | SC25's remedy text; Q13's urgency | MA-625 |
| L-W10 | MSI rollback N to N-1 by `msiexec /x` of N (or Uninstall-RMM.ps1) then the N-1 MSI, with an instance in two sessions: per-user state survives, unrepaired schedules run, repaired ones are RED `exe_missing` and the named per-kind remedies restore them, and whether `CloseApplication` runs during `msiexec /x` (SC36) | P10's label; §7.4; the rollback release note | MA-625 |
| L-W11 | MSI from the pre-N build straight to the SE4 build: RED `exe_missing` with the repair, and Repair restores it (SC37) | SE4's release note; the skip path | MA-625 |
| L-W12 | what a taskbar pin, a Start pin and an owner-made shortcut to a former name show after the copies go, and whether a Start tile follows the rewritten Start-menu entry (SC38) | the release-note line | MA-625 |
| L-W13 | only on a Q11 yes: the chosen notification is shown (after a scheduled run for (b), at login for (a)) (SC38) | the label of the notification (MA-643) | MA-625 |
| L-W14 | whether `app.Run`'s Outlook probe, from an unattended `run-job` of an `Auto` job, starts Outlook or blocks | whether a gap row is owed before SE1 ships for Windows | MA-625 |
| L-M1 | the `.app` with `CFBundleExecutable=mailarchive`: first and second open, an empty argument list, the path under `Contents/MacOS` (SC19) | the macOS rows of §4.3 (MA-620); SE1+SE2 ship the `.app` (SC42) | MA-626 |
| L-M2 | launchd runs the program through the `mailarchive-gui` link; what `os.Executable` returns when Finder, `open` and launchd start the program through the `/usr/local/bin` link, the `.app` link and the bundle path, before and after `EvalSymlinks`; a Finder launch has no terminal on any stream; the note and the Repair offer for a schedule on the link (SC14, SC15, SC16) | §7.2; SC15's `invokedBase`; SC16's macOS predicate; SE1+SE2 ship the `.app` | MA-626 |
| L-M3 | the universal binary (`makefat`) of the merged program | the `.app` build; SE1+SE2 ship the `.app` | MA-626 |
| L-M4 | app translocation of a quarantined, unmoved `.app` on each supported macOS version, and the path shape it runs from (SC14, SC8) | SC8's refusal (D1); §7.2; until it runs, the `/AppTranslocation/` path shape is labelled lab item | MA-626 |
| L-M5 | for the macOS flip slice only: a second Finder or Dock open of a running `.app` reaches `main`; Dock Quit; logout (SC19) | the macOS flip slice; Q21 if a second open only activates the running process | MA-626 |
| L-M6 | for the macOS flip slice only: the dashboard inside the `.app`: its lifetime, device-code sign-in and the `0600` token cache, the picker, and launchd running `desktop -capture` and `run-job` with no UI session against TCC-protected folders, any denial reported in `status` (SC19) | the macOS flip slice | MA-626 |
| L-M7 | for the macOS flip slice only: Gatekeeper's right-click Open of an unsigned `.command` file, and the Terminal window it leaves (SC20) | the macOS wizard click route after the flip | MA-626 |
| L-L1 | GNOME Files and Dolphin, amd64 and arm64, both directions: whether "Run" is still offered for an ELF; the file-manager Run, a `systemd --user` unit with the verb forgotten, a `DISPLAY=:0` crontab, `nohup` and a script's `</dev/null`, each recording the three streams, `/dev/tty`, TERM, JOURNAL_STREAM and the outcome; the README `.desktop` entry with its trust step (SC16, SC20) | Q4; the Linux click route | MA-627 |
| L-L2 | a Linux box with no `zenity`, naming its architecture (amd64 and arm64): every command-line verb runs; `wizard` gives MA-639's refusal; a failing `run-job`, interactively and under cron with no display or session bus, exits 1, logs FAILED, writes the breadcrumb and returns promptly; the library's lookup names; and, once a slice makes the Linux no-argument verb `desktop`, the launch question's fallback (SC44, SC46, SC18) | verifies MA-639 and MA-624 on a real box | MA-627 |
| L-L3 | only on a Q10 yes for Linux: what `/proc/<pid>/exe` reads after the file is replaced by rename (SC31) | Q10's Linux half | MA-627 |
| L-L4 | cron mail on a stock desktop with no MTA (SC41) | P5's and §7.3's wording | MA-627 |
| L-C1 | the merged file's size on each OS; the installed Windows totals for N and for N+1; the release-N payload zip and MSI against the `85c4c41` build (payload zip 45,438,957 B); whether WiX light de-duplicates identical File sources in one cabinet (SC24) | SC24's figures in the README's "One program" note, the release notes' disk line and DEPLOYMENT.txt; D3 | none allocated (OP-6) |
| L-C2 | the failure notification (the `notify` sink) from a scheduler-started failing `run-job` on macOS and on Windows is shown and returns promptly (SC46) | the Linux-only label of MA-624's probe (§12) | none allocated (OP-6) |

### 9.4 Prove-fail list

Each mutation is applied, the named row is watched going red, and the mutation is reverted;
the commit that builds the row records it (`CLAUDE.md`). A row with no mutation named here
records its own in its slice's commit.

- Mutate `main`'s switch so `desktop` falls to `runExport`: MA-610 red (the export verb refuses
  `desktop` as a path).
- Remove the `-job` legacy case: MA-611 red.
- Drop §4.1 rule 6: MA-611 red, because `mailarchive-desktop -copy-link` reaches the export
  verb's refusal (SC15).
- Insert `exec.Command(os.Executable(), "wizard")` into a dashboard handler, and
  `exec.Command(schedule.ProgramForSchedule(), "run-job", p)`: MA-612 red at each line (SC52).
- Drop `-H=windowsgui` from the `mailarchivew` line, then add `-tags` or `-trimpath` to one
  line: MA-628 red on the Subsystem, then on the build info (SC23).
- Remove `mailarchivew` from Cleanup-CurrentUser.ps1's list: MA-614 red (SC34).
- Make `Build-MSI.ps1` accept a `-Version` differing from `PRODUCT-VERSION.txt`, or drop
  `build-release.sh`'s write of the tag into it: MA-614 red (SC37).
- Make `ProgramForSchedule` return `os.Executable()` on Windows: MA-615 red, as revision 1
  states.
- Drop the directory bound from the identity predicate: MA-616's other-directory, same-build
  case red (a forged descriptor in another directory reads "this program") (SC2).
- Drop `SameBuild`: MA-616's darwin text-file, linux old-build and Windows skew cases and
  MA-615's skew case red (SC7).
- Drop the owner comparison: MA-615's and MA-616's other-owner cases red (SC7).
- Consult the size and mtime fallback for former names: MA-616's linux old-build case with
  matching recorded size and mtime red (SC2).
- Key the former-name note on the disjunct instead of the recorded name: MA-616's symlink case
  red (SC2).
- Remove the translocation case: MA-615 and MA-616 red (SC2, SC8).
- Offer the re-point repair on any "this program": MA-618's not-offered cases red (SC9).
- Collapse `jobKind` to two values: MA-631 and MA-617 red (SC1).
- Make corroboration always true: MA-632 red (SC1).
- Drop the predicate's rule 6 (the size and mtime fallback): MA-623's present-and-unchanged case
  red (SC13).
- Remove the breadcrumb read from `health.Gather`: MA-634 red (SC4).
- Remove the shown-refusal marker check from the merged main: MA-636 red (SC26).
- Revert `run-job`'s read to `os.ReadFile`: MA-640 red (SC45).
- Merge the console build's own-console case into the shared-console case: MA-620's
  `consoleIsOwn` case red (SC17).
- Remove `noTerminal` from the macOS bundle row: MA-620's Terminal case red (SC16).
- Make the Linux launch question start `desktop` before OK: MA-620's lock-absent case red, in
  the slice that adds that case (SC18).
- Restore the old rebuild string: the moved MA-386 test red (SC21).

## 10. Catalog rows proposed (nothing is added to the catalog here)

**Invariant proposed:**

- **R33 — One program.** Every function the product offers is a verb of one executable
  (`mailarchive`), reachable from the command line, and every schedule and shortcut the product
  installs names that executable (on Windows, the GUI-subsystem build `mailarchivew.exe` when
  it is installed beside the program under that name, with the same owner and the same build
  (§4.6.0); otherwise the running file, which `status` names) (SC12). The program never starts
  a copy of itself to perform a function, and no surface exists only in a file that is not the
  program. `status` tells a former name of the program from a moved or missing one, naming the
  repair. Proven through MA-610..MA-616 and MA-628; "never starts a copy of itself" is proven by
  MA-612's walk of every process-start site and its recording fakes (SC52). Open condition
  (owner rulings rule 3): a program path passed through a variable into an opener's argument is
  not traced by the walk (§12) (SC52). If L-W2 shows that a wrapper does not wait for the
  GUI-subsystem child, pass its stderr or pass its exit code, the parenthesis gains "a wrapped
  schedule records the console build" (SC22): at SE0 if L-W2 has run by then; after SE0 that is
  a narrowing of landed text, which goes to the owner under rule 2.

**Scenario proposed:**

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S59 The owner meets the product through one program: a shortcut, a schedule and a terminal each start `mailarchive` (on Windows one of its pair); a no-argument launch follows §4.3 (on Linux per Q4); an upgrade from the three-file release keeps every schedule legible, and keeps it running only where the former file still exists (the MSI's copies and the `.app` link, from release N until the floor, §7.1); a schedule whose former file is gone is RED `exe_missing` with the repair its kind offers (SC41, SC37, SC39) | R33, R18, R14, R5, R12 (SC1, SC32, SC39) | a schedule naming a former name keeps running while the former file exists (§7.1, §7.2; never on Linux, §7.3) and carries a note naming the repair; when the file is gone it is RED `exe_missing` with the repair its kind offers, discoverable in `status` and every health view and not pushed; no schedule is rewritten unasked; a no-argument launch always opens a visible surface or prints the usage; no command-line verb shows a dialog, and `run-job` raises only the failure notification (MA-124, MA-624); the command line's export schedule raises none (the schedule-v2 non-goal) (SC41, SC46) | MA-610..MA-618, MA-620..MA-624, MA-628..MA-641 (MA-619 only on a Q10 yes; the lab rows MA-625..MA-627 pending); S59's Linux clause is proven by MA-617, MA-618 and MA-633, the U rows for the RED-with-repair remedy (SC41) |

**UX contract proposed:**

- **X17 — One program, one name in every swept text.** Over MA-622's swept set (§9.2), texts
  name the program as `mailarchive`, `mailarchivew.exe` or `Mail Archive.app`, and each surface
  by its verb. A former name (`mailarchive-gui`, `mailarchive-desktop`, with or without `.exe`
  or a release-asset suffix) may appear only on a line that also says "former name", and never
  as the thing to run, double-click, download or build. The `status` `Program:` value and the
  `exe_former_name` path are runtime values proven by MA-616, not swept. The release-N machine
  scripts sit outside the sweep, pinned by MA-376 and MA-614. The phrase rule is the only
  exemption for text; the matcher's two excluded non-program tokens (§9.2) are part of this
  definition, which the operator accepts or rejects at SE0 (OP-6); there is no other skip,
  allowlist or suppression. Enforcement: MA-622 (S), green on the texts SE1
  ships (SC47).

**Test-spec rows:** MA-610..MA-647 as §9.2 states, tier U unless marked S or L; the three L
rows (MA-625..MA-627) carry "pending" and the lab items they wait on (SC49). Each row's Covers
cell cites S59 and the R rows its text names: R33 for MA-610..MA-616 and MA-628; R18 for
MA-615..MA-618, MA-623 and MA-629..MA-635; R14 for MA-617, MA-631 and MA-632; R5 for MA-638;
R12 for MA-636, MA-639 and MA-640 (SC1, SC32, SC39). MA-645..MA-647 cite R14 and R18 (MA-645)
or R18 (MA-646, MA-647) and not S59, because the gaps exist whatever this design does; the
operator names their scenario when accepting them (OP-1..OP-3) (SC6). If the operator accepts them
(OP-1..OP-3), they land now, in a PR of their own, with xfail encoders (§9.1). SE0's catalog-first PR lands
R33, S59, X17 and MA-610..MA-641 except MA-619, every U/S row with its skip encoder (§9.1)
(SC48). MA-619, MA-642, MA-643 and MA-644 land on the owner's yes to Q10, Q14 (a), Q11 and Q12 — all now
**yes** (ruling 13.44; MA-619's Linux half waits on L-L3 per Q10) — in the PR of the slice that builds them.
No catalog-first PR lands before the operator's one-pass allocation of the ids (OP-6, SC49).

**Catalog edits the slices make (SC50).** Each edit lands in the PR that makes its text true:
SE0's catalog-first PR for a text that is true at SE0, and otherwise the slice PR whose change
makes it true, in the same commit, so no catalog text names a target, a program or a control
before or after it exists. Additions, and renames whose covering tests do not change, are the
operator's (owner rulings 2026-10-09, "How the limit is applied", rule 1); an edit that narrows
text, adds an exception, or changes what an existing covering test asserts goes to the owner
(rule 2). The operator classifies each edit in one pass at SE0 (OP-6). The covers map checks
ids only, so this list is the only guard against catalog drift, and it is reviewed with each
catalog-first PR.

| Row | Edit | Slice and PR | Who decides |
|---|---|---|---|
| X9 | `mailarchive-desktop` becomes `mailarchive desktop` | SE1, with the rename | operator; the owner under Q18 (a) if a covering test pins the name |
| X11 | removed from the rename, because it names no former file; it changes only if the operator wants "the binary" to read `mailarchivew.exe` | — | operator |
| S41 | `mailarchive-desktop` becomes `mailarchive desktop`; "IN-PROCESS (never a child binary)" stays word for word | SE1, with the rename | operator; the owner under Q18 (a) if a covering test pins the name |
| MA-271 | `mailarchive-desktop` becomes `mailarchive desktop` | SE1, with the rename | operator; the owner under Q18 (a) if its test pins the name |
| MA-278 | `mailarchive-desktop --capture` and "this is the command the weekly backup runs" become `mailarchive desktop -capture` | SE2, when `backupSpecFor` records it | operator; the owner under Q18 (a) if its test pins the name |
| MA-376 | the refusal text it cites follows the P9 rename ("mailarchive desktop: -addr …"), and its packaging test's shortcut `Arguments` (`-copy-link`, `-no-browser`) gain the `desktop` verb (`desktop -copy-link`, `desktop -no-browser`) | SE1, with the rename and the packaging commit | owner (Q18 (a)), because its test pins the former name and the old arguments |
| MA-377 | none: it names no file | — | — |
| MA-546 | the "mailarchive-gui" step becomes "the MailArchive wizard (`mailarchive wizard`)" with its click routes, beside the P9 change to `addMailHelp` | SE1, with the text commit | owner (Q18 (a)); interim: SE1's text and packaging commits wait, and SE1 is not released until both land |
| MA-546 | at SE2, the Weekly backup card shown for a local-only archive's recorded schedule | **yes (Q18(b), 13.44)** | the card renders the schedule's kind, health, Repair and Remove for a local-only archive, with no Microsoft 365 install controls; MA-546's text and `TestLocalOriginArchiveInDesktop` are edited to match (rule 2, owner-made) (SC1, SC3) |
| MA-546 | at SE3, the add-mail page replaces the wizard step, and "adding mail is the GUI's and the command's job" | with the SE3 design | owner (Q18 (c)), asked with the SE3 design |
| MA-529 | the govulncheck target `./cmd/mailarchive-desktop` becomes `./cmd/mailarchive`, in both ci.yml and release.yml; `TestBrowseIsDisabledDuringARunAndCIScansWindows` changes in the same commit to find each step by content and require exactly the token `./cmd/mailarchive` (SC27) | SE1, on the owner's yes | owner (Q17), because its test asserts `./cmd/mailarchive-desktop` literally; interim: `cmd/mailarchive-desktop` stays as a thin, unreleased main, CI and release.yml scan it and `./cmd/mailarchive`, and the test keeps its assertion and gains the `./cmd/mailarchive` step in both workflows (a strengthening) |
| MA-79 (L) | its wrapper check extends to the GUI-subsystem child inside the wrapper: cmd.exe waits for it, its stderr reaches `.stderr.log`, its exit code becomes Last Run Result (SC22) | SE2, when a wrapped schedule records `mailarchivew.exe` | operator (an addition) |
| MA-95, MA-123, MA-166 | the `-job` wording becomes `run-job`, and the `-job` assertions of `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` change | only on a Q16 yes | owner (Q16); interim: `-job FILE`, no edit |
| X4 | "`schedule … -- <job>` carries any backup job verbatim" is scoped for `run-job` and `desktop -capture` | only on a Q12 yes | owner (Q12); interim: no edit |
| MA-386 | none: its moved test is strengthened (§9.1) | SE1 | operator (an addition, classified at SE0, OP-6) |
| MA-123, MA-166 | "the CURRENT exe" / "the current exe" becomes "the program `ProgramForSchedule()` chooses (§4.6.1), never the descriptor's" | SE2, with the `Exe` switch | the operator classifies it at SE0 (OP-6); if it narrows the guarantee or changes what `TestRepairSpec` asserts, the owner (rule 2) |
| MA-124, MA-167, MA-168, MA-466 | none: their tests move or stay with their assertions unchanged (D4 for MA-466) | — | — |
| MA-616's louder WARN | a former-name file, former release assets included, that matched only through today's size and mtime fallback reads WARN `exe_moved` instead of silent GREEN (SC2); no existing test pins the old behaviour | SE0's catalog-first PR | operator (an addition) |
| MA-614, MA-615, MA-616, MA-619, MA-620, MA-622 | SE4's edits: the copies leave MA-614's payload, list and wxs cases; the former-name clauses leave MA-615 and the `exe_former_name` note text leaves MA-616; MA-619 and MA-620 follow §11's retire-and-keep list; MA-622's release-N paragraph goes (SC23, SC28, SC37) | SE4, in its PR | classified by the operator in SE4's catalog-first PR; an edit that changes what a landed covering test asserts goes to the owner (rule 2) |

## 11. Build slices (small, in dependency order)

| Slice | What | Depends on | Rows | Gates owed |
|---|---|---|---|---|
| **Now** (independent of this design; **OPEN: operator**) | if the operator accepts the three rows of the gaps in the shipped code at `b1a16bc` (OP-1..OP-3), their xfail encoders land as real-primitive tests (§9.1); until then the gaps stand unrecorded (SC6) | the operator (OP-1..OP-3) | MA-645, MA-646, MA-647 | none |
| **SE0** | the catalog-first PR for R33, S59, X17 and MA-610..MA-641 except MA-619, every U/S row with a `t.Skip` encoder naming its id and its slice (§9.1), the L rows pending (SC48); the operator allocates this design's block, the pre-sync design's shifted block, and every row, lab item and question the review adds, in one pass (except the ids of OP-1..OP-3, allocated on acceptance), with its log entry, and re-checks R33, S59, X17 and the MA, L- and Q ids (OP-6, SC49); the operator classifies each §10 edit under rule 2 (SC50) and decides whether to accept MA-616's louder WARN (SC2); OPEN-X1 stays the operator's and this design needs no answer (OP-7, SC51) | SG1 (the review, filed); the re-run of lenses 3, 4, 6, 7, 8 and 10 on the changed sections, and the second reviewer's reading of the critic's six re-check items, before this PR (SC53, §14) | the block's U/S rows as skip encoders; MA-625..MA-627 pending | the re-run of SC53 (§14) |
| **SE1** | a pure move first, then one commit per named behaviour change with its row (below) (SC43); the dispatcher and the three verbs; §4.1 rule 6 (SC15); `internal/wizard` with no `Dialogs` interface (SC43); the launcher into `internal/desktop`; `run-job` with its secure read (SC45); the no-argument rule with the console build's pointer (SC16, SC17), the macOS and Linux verbs `wizard` and no Linux row until Q4 (SC16, SC19); the shown-refusal rule (SC26); the instance file's `version` and the handoff notice (SC33); the ended-instance line (SC32); the pair in the gated builders, the continuation scripts on `make build-windows` (SC23); packaging: the MSI with copies, `WizardShortcut` (SC20), the single `CloseApplication` target, the four RMM scripts (SC34, SC35), Build-MSI.ps1's two lists (SC23), the Windows zip of install names (SC11, OP-5), the `.app` with its link and build-app.sh's `-y`; the release.yml scan step and the ws_run_test.go edit in the same commit, under Q17's interim (SC27); README and texts (P9), the five administrator texts (SC28), `headlessRebuildAction` and `addMailHelp` per slice and OS (SC21); the dashboard on macOS and linux/arm64 as `mailarchive desktop`, labelled not lab-run (SC19). What the wizard and the dashboard record is unchanged: `os.Executable()` and `-job FILE` (SC42). `cmd/mailarchive-gui` is deleted last; `cmd/mailarchive-desktop` is deleted last only on a Q17 yes (SC43, SC27) | SE0 | MA-610..MA-614, MA-620..MA-622, MA-624, MA-628 (its `SameBuild` call a per-case skip until SE2), MA-636..MA-640; MA-619 only on a Q10 yes; MA-625..MA-627 in part | **friction review**: the §8 walks (the double-click, the per-archive upgrade walk, SC5, and the inheritor), the console-build double-click (SC17), the Start-menu wizard shortcut (SC20), the wizard's dialogs walked by hand against the recorded move diff (SC43, MA-95), and the record that macOS and Linux have no Quit action in the dashboard (SC33); **adversarial pass** on §4.1 rule 6, §4.3 (the `noTerminal` and `consoleIsOwn` inputs and the console build's stdin wait) and §4.5.2 (on a Q10 yes, the amended CC22 text; the record names whether this pass or LC-C2's still-owed pass reviews it, SC31) (SC15, SC16, SC17) |
| **SE2** | §4.6.0's `SameBuild` and `SiblingQualifies` (SC7); `ProgramForSchedule(wrapped bool) (exe, reason, error)` and `ProgramNote` (SC8); the `Exe` switch at all four call sites (`runSchedule`, `offerSchedule`, `repairSpec`, `backupSpecFor`) in one commit, with `offerSchedule`'s spec extracted as the pure `installSpec` in that commit (SC42); `backupSpecFor`'s `Args` led by `desktop`; the schedule kind (§4.6.5): the taxonomy, `schedule.KindEvidence`, the guard in `refuseCollision`, `schedule.CheckKind` (SC1); `schedule.RepairRoute` and `repairHow` (SC3); the composed identity predicate and its texts, `status`'s `Program:` line (SC2, SC10); `Input.ExeRepoint` and the volatile-directory question on every repair (SC9); the Windows `exe_missing` install-folder branch (SC25); the dashboard's Repair; the breadcrumb reader moved into `health.Gather` as its own commit (SC4); MA-645's skip comes off, if the operator accepted it (SC6) | SE1 (shipped with it, below) | MA-628's `SameBuild` call, MA-615..MA-618, MA-623, MA-629..MA-635, MA-641; MA-645 | **adversarial pass** on §4.6: the identity rule against the archive-writer and planted-sibling personas, the kind flip, and Q20's refusal (the owner answered Q20, 13.44: the operation is refused with a relocation/reinstallation remedy, not accepted; the pass reviews the refusal and the permissions policy, §13); **friction review** of the rewritten per-archive upgrade walk (SC5) |
| **SE3** (own design) | needs SE1 and SE2 built; uses `schedule.CheckKind` (R1 of §4.8); owes SC18's Linux launch question if Q2-linux says the dashboard (SC39, SC40, SC18) | SE2 built | its own, proposed by that design | its own 10-lens pre-code review, then adversarial pass and friction review (SC39) |
| **macOS flip** | the one-row change of the `.app`'s no-argument verb to `desktop`, plus the wizard's click route `Open MailArchive wizard.command` in `MailArchive-macos.zip` (SC19, SC20) | SE3; L-M5, L-M6 and L-M7 recorded; a macOS double-click walk (§8) and its friction review; Q2-mac answered "the dashboard"; if L-M5 fails, an owner ruling (Q21) and a design of its own | the flip case added to MA-620; MA-626's L-M5..L-M7 | **friction review** of the macOS double-click walk |
| **SE4** (first minor tag past the floor) | retire the former-name copies and the `.app` link, with the retire-and-keep list below; the release note with the measured N+1 disk figure (SC24) | the copies' floor (§7.1, Q9) has passed, and the last tag before SE4 carried the SC35 and SC38 release-note lines; release N's note and README-RMM have shipped the detection-rule text (SC35, SC37) | SE4's edits to MA-614, MA-615, MA-616, MA-619, MA-620, MA-622 (§10) | the release-note preconditions; L-W11 and L-W12 recorded (SC37, SC38); **friction review** of the post-floor walk (the RED with the repair, the skip path); **adversarial pass** on the retirement of the former names from `knownName` in §4.6.2 (trust boundary 2) |

**SE1's commits (SC43).** The first commit is a pure move: `git diff -M b1a16bc` shows the
moved files' bodies unchanged apart from the package clause, the exported entry names (`Run`,
`RunHeadless`, the launcher's) and import paths. `cmd/mailarchive-gui` and
`cmd/mailarchive-desktop` stay as thin mains that call the moved code, so the tree builds and
every moved test passes at that commit; `lastfailure.go` moves whole into `internal/wizard`
(SC4); the encoders of §9.1 move with their packages. The commit message records the diff. Each
later commit makes one named behaviour change and carries its row:

1. the dispatcher and the three verbs (MA-610, MA-613);
2. the legacy spellings and §4.1 rule 6 (MA-611);
3. the no-argument rule and the console build's pointer (MA-620);
4. the wizard verb's stderr output and preflight (MA-639);
5. `run-job`'s job-file read and its no-follow sink (MA-640);
6. the shown-refusal rule (MA-636);
7. the instance file's `version` and the handoff notice (MA-637), and the ended-instance line
   (MA-638), one commit each;
8. no self re-exec (MA-612) and the pin of per-user state call sites (MA-621);
9. the pair in the builders and packaging (MA-614, MA-628), whose shortcut `Arguments` change waits on Q18 (a) with item 10 (MA-376, §4.10);
10. the texts (P9, SC21, SC28, SC47; MA-622, MA-624 and the moved MA-386 test), which wait on
    Q18 (a) where a covering test pins a former name;
11. the launcher renames (§4.5.1);
12. last, the deletion of the thin mains; `cmd/mailarchive-desktop`'s waits on Q17.

Other sections' SE1 edits are their own commits, never folded into the move. SE1 and SE2 each
remove the skips of their own rows' encoders (SC48).

**The release rule (SC42, SC29).** SE1 and SE2 ship to owners in one release, release N
(§7.1). No release is cut from SE1 alone on any OS, and if SE2 slips, release N slips. The
rule has two reasons. First, under SE1 alone a terminal-started `mailarchive.exe wizard`
records the console build, so a console opens at 02:00. Second, under today's `exeIdentity`
fallback the MSI's former-name copies, whose size and time differ from what the old install
recorded, read `exe_moved` WARN until SE2's name rule exists (§4.6.2). No `v*` tag is pushed
from a main containing SE1 until README-RMM.md §D's round has recorded the Windows lab items
(§9.3, SG3). Because release.yml publishes `dist/*` for every OS on any `v*` tag, this also
holds that tag's macOS and Linux release. Q15 (13.44) is answered no, so SG3 holds as the settled release gate until README-RMM §D's
Windows round is recorded. SE1 is also not released until the P9 text commit and the packaging commit
land (Q18 (a) answered yes, 13.44; the commits still land in SE1).

**SE4: what it retires and what it keeps (SC37, SC10, SC15, SC34, SC35).** Retired on
Windows: the two former names in §4.6.2's `knownName`, so a hand-made former-name copy reads
as not this program and is never GREEN without a note; §4.6.1's former-name clause; the
`exe_former_name` note and its literal; the copies from Build-MSI.ps1's two lists, from
Product.wxs and from `packaging_test.go` (SC23); the release-N paragraph of the five administrator
texts (SC28); and §4.1 rule 6 for `mailarchive-desktop`. Kept: the former-name set the
missing-former-name hint and the predicate's rule 5 read, so a schedule left on a former name
reads RED `exe_missing` with the hint (SC10); §4.5.2's former name, because it serves an
upgrade from a pre-SE1 release, subject to SC31 and Q10; §4.6.3's Repair trigger on a
former-name `Exe`, because it repairs the RED; the former names in Uninstall-RMM.ps1's and
Cleanup-CurrentUser.ps1's stop lists, since an older instance can survive an upgrade (SC34);
Detect-RMM.ps1 as it is, because it names no former name (SC35); and MA-622's phrase rule
(SC47). On macOS, `knownName`'s `mailarchive-gui`, §4.1 rule 6 for `mailarchive-gui` and the
`.app` link follow the macOS link's lifetime (Q9). Also kept: `util:CloseApplication`'s one target,
`mailarchive-desktop.exe`, and Install-RMM.ps1's unfiltered `mailarchive-desktop` stop, because
CC22's pre-upgrade instance can still meet an install that takes the skip path (SC34). SE4's
packaging assertion is that no file under `packaging/` names `mailarchive-desktop.exe` or
`mailarchive-gui.exe`, except `CloseApplication`'s target, the stop lists of Install-RMM.ps1,
Uninstall-RMM.ps1 and Cleanup-CurrentUser.ps1, and lines that say "former name" under MA-622's phrase
rule (SC34, SC35, SC37, SC47). Q19 is asked before SE4; under its interim, SE4's release
note tells the owner to run `status -out` for every scheduled archive, and §12 labels "loud at
N+1" conditional (SC5).

Producers precede sinks: SE2's `ProgramForSchedule` exists before any surface records it, and
until SE2, SE1's call sites record `os.Executable()` (SC42); SE1's `run-job` exists before the
SE3 design schedules it; the four seams the SE3 design builds on (the job-file kind,
`run-job`, `ProgramForSchedule` and the capture slot) exist before SE3 starts (SC39); the copies
(SE1) exist before any descriptor can name a former name that is missing, for installs that
pass through the floor; an install that skips the floor meets the skip path of §7.1 (SC37).

Slice order: the open-gap encoders now; SE0; SE1 then SE2 (which also lands `schedule -repair -out DIR`, MA-644, Q12, before SE4), released together as release N;
the SE3 design's slices; the macOS flip slice after SE3; SE4 at the first minor tag past the
floor (SC42, SC37, SC39).

## 12. Honesty of claims

Revision 2 relabels every claim a condition changed, and each changed row cites its condition. Row ids
follow §9.2's provisional allocation (SC49). Nothing is built and nothing is lab-validated, so "proven"
still reads "will be proven by the row named once its slice is built".

| Claim | Label | Condition / what proves it |
|---|---|---|
| Every surface is a verb of one program | proven (MA-610, MA-613) | — |
| The pair is one program | proven at build (MA-614: the two gated `go build` lines differ only in the `-o` name and `-ldflags -H=windowsgui`; MA-628: Subsystem 3 and 2, equal build info but `-ldflags`), and checked on the machine at every install and `status` (MA-615, MA-616, through `SameBuild`, §4.6.0) | conditional: that `-ldflags` appears in build info only without `-trimpath` is a toolchain fact, labelled conditional (§4.2); the release environment's `GOFLAGS` and `CGO_ENABLED` apply to both lines equally (residual, stated); a build with no VCS stamp is an open condition (§4.6.0); L-W1 is informational (SC7, SC12, SC23) |
| A schedule records `mailarchivew.exe` (P4) | conditional (MA-615, MA-630) | only when it is beside the running program under that name, with the same owner and the same build (§4.6.0), and present; otherwise the schedule records the running file, the install says so, and `status` names it. An owner-owned sibling that another account can write is refused with a relocation/reinstallation remedy (Q20 answered, 13.44), not an accepted residual; the policy and mechanism are at SE2. While the pair is unsigned (Q13's interim), the claim also rests on no antivirus verdict against the file. For a schedule installed with a `.cmd` wrapper (the desktop's and the command line's, always), only if L-W2's three wrapper checks pass; otherwise the wrapper records the console build (§4.6.1) (SC7, SC8, SC12, SC22, SC25) |
| `status` from a second copy | conditional | `status`, the dashboard's health view and the wizard's health check agree within one install directory; a second copy in another directory is another install (P4) (SC12) |
| No console where none is shown today | conditional | for schedules without a wrapper only, with the pair installed together under its install names (L-W2): the GUI-subsystem build started directly by Task Scheduler shows no console, and the console build started directly shows one for the whole run. The desktop's and the command line's always-on `.cmd` wrappers still show today's brief console whatever the program inside (non-goal 7, D2). If L-W2 shows that a wrapper does not wait for the GUI-subsystem child, capture its stderr or pass its exit code, SE2 records the console build inside a wrapper (§4.6.1) (SC12, SC22) |
| Refusals, exit codes, routes, guards, formats and the wizard's decision functions behave as at `b1a16bc`, except P2's listed exceptions | proven through the moved rows and MA-613 | the covers map is the guard that none is lost (SC43) |
| The wizard's dialogs ask what they ask at `b1a16bc` | lab (MA-95, pending) | unchanged code by the recorded move diff (`git diff -M b1a16bc`, SE1's first commit), walked by hand in SE1's friction review (SC43) |
| `status` knows the program's names | proven (MA-616) for the program's own directory | there a known name is this program only through the sibling test (§4.6.0), and the size and mtime fallback is not consulted; a former name never becomes this program through the fallback; outside the program's directory the fallback says the recorded file is unchanged since install, never that it is this program (D4) (SC2, SC12) |
| Upgrade day is never RED | conditional | Windows (MSI) and macOS, for schedules only, as seen in the health view of the schedule's own archive (SC5). The conditions: the copies or the link exist (L-W3, L-M2), and on macOS the bundle sits at a stable path, moved to Applications before it was first opened (L-M4) (SC14); `mailarchivew.exe` is present and, while the pair is unsigned (Q13's interim), no antivirus verdict stands against it (SC25); the install passed through a release that ships the copies (the floor, §7.1), because an install that skips it is RED `exe_missing` with the repair (L-W11) (SC37); no run was ended at upgrade: a run the upgrade spares can still be ended by Restart Manager or by a reboot after exit code 3010, a dashboard's in-process capture ends with its dashboard, and each reads RED `run_never_finished` until the next run (SC34), while an older instance ended under §4.5.2 is named in the header and in `status` (SC32). Linux: not claimed (§7.3: RED with the repair). A Windows owner of the release zip is in Linux's position (§7.6) (SC11) |
| A bare-download Windows install, if the operator declines the zip (OP-5) | residual, stated | the suffixed bare files keep shipping, so a descriptor that names one is outside the name set: after an upgrade into another folder it reads WARN `exe_moved` with the repair, and in the same folder it reads this program only by the same file or as unchanged since install; the README names the MSI only for Windows (SC11) |
| A double-click goes somewhere visible | conditional | L-W4 (extended to `GetConsoleProcessList` and to the console of `mailarchive.exe desktop`), L-M1 (first and second open), L-M4 (translocation), L-L1 (both directions, amd64 and arm64). The Linux row exists only once Q4 is answered "honour"; under Q4's interim a Linux launch with no argument prints the usage refusal. Residuals beside P7: on macOS a second open while the wizard runs activates the running process and starts nothing new, as today (L-M5); a mistyped or unknown flag on a shortcut or a by-hand task under a GUI-subsystem file reaches stderr only, as at `b1a16bc` (§4.1 rule 6); the double-clicked console build prints its pointer and waits for Enter, and if L-W4 shows the console's process count is unreliable the case is today's refusal and the console flashes (SC17). Once a Linux row exists: an unattended Linux graphical start with no verb shows the wizard's first dialog (or, from the slice that makes the Linux verb `desktop`, SC18's question) and never starts a server; a unit with `Restart=` repeats that question once per timeout; a box without `zenity` prints the usage where no one sees it (SC15, SC16, SC18, SC19) |
| An install or repair from a translocated `.app` writes nothing | proven (MA-615, MA-630) | refused rather than "Continue anyway" (D1); the `/AppTranslocation/` path shape is a lab item (L-M4) (SC8, SC14) |
| A repair keeps the schedule's kind | proven (MA-617, MA-631, MA-632) | a recorded kind counts only when owner-side state corroborates it (§4.6.5); an uncorroborated or unknown recorded kind does not refuse a cross-kind install, because the schedule it describes cannot run (residual, stated). Until SE2 builds the mechanism, S59's "keeps it running" and P5's first arm are conditional on no cross-surface Repair being clicked, and the shipped wizard's kind-blind repair is the open gap OP-1 (MA-645, xfail, once the operator accepts it) (SC1, SC6) |
| A start failure is shown on every health surface | proven (MA-634) | the breadcrumb is one slot per account, so of two archives that both fail to start only the later failure shows (residual, stated). Until SE2 the wizard is the only reader, and the dashboard does not show a run that could not start (SC4) |
| The shipped gaps OP-1..OP-3 | open gaps, not accepted | on the operator's acceptance (OP-1..OP-3), each gets a row and an xfail encoder now (MA-645, MA-646, MA-647); until then the gaps stand unrecorded. OP-1's skip comes off with SC1 in SE2; OP-2 and OP-3 each close in their own slice (SC6) |
| One file carries every Windows schedule and shortcut | lab item (L-W9); Type II | after SE2, `mailarchivew.exe` and its two byte-identical copies are lost together to a deletion or a hash-based quarantine, and `mailarchive.exe` is then the only entry point (§7.1). The remedy is the installer's Repair (MA-635); while the pair is unsigned (Q13's interim), Repair re-installs the same bytes and may be quarantined again while the antivirus verdict stands (SC25) |
| A failed or stopped schedule is reported | conditional | pull-only on every OS: `status`, the health views, cron mail on Linux where delivered (lab, L-L4); no push channel (schedule-v2 non-goal). Under Q11's interim no notification is added and MA-124 is unchanged (SC38, SC41) |
| After SE4, a schedule still on a former name is loud | conditional | "loud" means RED `exe_missing` in every health view and in `status`. The owner learns of it by opening that archive's health view or running `status -out`, and — from Q11 (b) (13.44) — by one best-effort notification per affected schedule across the compatibility period (MA-643); SE4's removal of the copies is coupled to the Q19 migration inventory (SC5, SC38, SC41) |
| The RMM detection rule survives the copies' removal | aspirational | release N's Detect-RMM.ps1 tests the DC6 key and both files of the pair and names no former name, so it passes at N and after SE4 (MA-614); an older copy of the rule stored in the RMM is outside every loud path, and the release notes and README-RMM are its only mitigation (Type II for an administrator who reads neither) (SC35) |
| The per-account instance rules (P6) | conditional | P8, CC21, CC23 and CC22's condition are unchanged; the one mechanism change is §4.5.2, an amendment of design-local-confidentiality §4.C4 step 2 [CC22] that waits on Q10. Under Q10's interim today's exact-path test stands on Windows and Linux, and MA-619 and L-W6 are not built (SC31). A healthy older holder keeps the lock until it is quit or the owner signs out, and the handoff names it (MA-637; Type II, stated) (SC33); an older instance the launcher ends is named in the header and in `status` (MA-638) (SC32) |
| Rollback reads every file; on the MSI it is uninstall, then install | proven (MA-623) for the files; conditional on L-W10 for the installed product | the only format changes are the instance file's optional `version` and `ended` fields and its `former` kind `ended`, which older readers ignore (SC32, SC33). A pre-SE2 reader of a descriptor naming `mailarchivew.exe` reads "this program" and is silent while that file is present and unchanged (the schedule keeps running release N's file), reads RED `exe_missing` once it is removed, and reads WARN `exe_moved` only if it was rewritten in place. A repaired schedule is RED after a rollback that removes the new-name file (the MSI's uninstall), with the per-kind remedy that works at N-1 (§7.4) (SC13, SC36) |
| No command-line verb shows a dialog, and `run-job` raises only the failure notification (MA-124, MA-624) | proven (MA-624) on Linux | the fake-`zenity` probe proves the claim on Linux only; macOS notifies through `osascript` and Windows natively (lab, L-C2); L-L2 checks it on a real box without `zenity`. Whether the notification can hold up the run stays an open condition until L-L2 and L-C2 answer (SC46) |
| The wizard refuses visibly when it cannot show a dialog | proven (MA-639) | L-L2 checks it on a real box, amd64 and arm64 (SC44) |
| `run-job` reads its job file securely | proven (MA-640) | a regular file, no link, a no-follow non-blocking open, owner-only mode on Unix, at most 1 MiB; on Windows the per-user ACL is not checked, an open condition (§5.1) (SC45) |
| A shown refusal reaches each channel once | proven (MA-636) | CC20 as written: from `mailarchive.exe` in a terminal, a `desktop` or `wizard` refusal also shows a modal dialog that holds the shell until it is dismissed, and over ssh with no GUI session the dialog call fails or waits (Type II, stated; L-W5). A terminal-only channel amends CC20 and is Q14 (SC26) |
| The program never starts itself or a sibling | proven (MA-612) | it starts only the OS tools §5.4 item 1 names; a program path passed through a variable into an opener's argument is not traced, an open condition beside P1 and R33 (owner rulings rule 3) (SC52) |
| One name in every text (X17) | proven (MA-622) over its enumerated swept set | a former name appears only on a line that also says "former name"; the release-N machine scripts are outside the sweep and pinned by MA-376 and MA-614; the `Program:` value and the `exe_former_name` path are runtime values proven by MA-616 (SC21, SC28, SC47) |
| No new per-user state location (SE1, SE2) | proven (MA-621) | SE1 and SE2 add none; Q19's migration inventory is the one new per-user state item, arriving at SE4 as a consumer of OPEN-X1 (OP-7), designed at that slice (§4.9) (SC51) |
| The dashboard on macOS and linux/arm64 | lab item (not lab-run) | it has never shipped there, and SE1 is its first release on both; the README labels it "not lab-run on macOS". The macOS dashboard lab (L-M6, owed by the macOS flip slice) and L-L1/L-L2 on arm64 would retire the label (SC19) |
| Disk and download cost | lab (L-C1) | release N installs four Windows files, about one file and 40 MB more than today's three; release N+1 installs the pair, about one file and 20 MB less; release N's payload zip and MSI carry the `mailarchivew.exe` bytes three times. The figures are estimates until L-C1 measures them; the copies are plain components (D3) (SC24) |

Revision 1's row "The dashboard adds local mail with the wizard's guarantees" leaves this design with SE3
(§4.8) (SC39).

Status: reviewed (GO WITH CONDITIONS, bounded), conditions folded; approved scope SE0, SE1, SE2 and SE4
once the stale lenses are re-run (§14) (SC53); SE3 is not approved, and this design is written for it to get its own design pending the operator's OP-4 (§4.8) (SC39);
not built, not lab-validated. No `v*` tag is pushed from a main containing SE1 until README-RMM §D's round
has recorded the Windows lab items (SG3) (SC29).

## 13. Owner questions

Each question below was raised or re-asked by the review (except Q1 and Q3, unchanged from revision 1). The
**OPEN** marker in each row's `#` column records that the question was open at review time; **all are now
answered by the owner in ruling 13.44** (and SC53 by 13.45). The table keeps each question's recommended
answer and interim as the record; the **owner's rulings** are listed immediately below, and the six that
deviate from the recommendation (marked ⟐) are folded into the mechanism sections named. Q5, Q6 and Q7 move to
the SE3 design, and revision 1's Q8 becomes the operator's OP-7 (SC39, SC51).

**Owner's answers (ruling 13.44, 2026-10-17):**
- **Q1** yes: `desktop`, `wizard`, `run-job`.
- **Q2-mac** keep the wizard this release; reconsider the dashboard after the macOS lab and a usability review.
- **Q2-linux** wizard in SE1/SE2; defer the dashboard switch to SE3.
- **Q3** defer (no CLI job-file install this phase); decoupled from Q12.
- ⟐ **Q4** yes — honour a no-argument Linux launch once L-L1 passes both ways, **and** ship explicit
  `.desktop` launchers (an explicit verb as the predictable route); the launch opens the wizard (Q2-linux).
  Folded in §4.3, §4.10, §7.3, §6.
- **Q9** floor: two minor versions **and** six months, whichever is later; keep the macOS link while the
  legacy spellings are supported. The floor is the **earliest** removal date, not automatic — removal is
  coupled to Q19 (§7.1).
- **Q10** yes on Windows after its lab and security review; Linux only after L-L3; scoped to the identified
  legacy, unauthenticated dashboard, same account and directory; an active operation may be interrupted
  (accepted); never generalized to killing all old-version processes. Still owes SE2's adversarial pass.
- ⟐ **Q11** approve (b), broadened: once per **affected schedule for the whole compatibility period** (not
  release N alone); no login notification ((a) declined); best-effort; successful backups stay quiet. Folded
  in §4.6.3, §7.1, §6, §12.
- ⟐ **Q12** yes — `schedule -repair -out DIR` lands **before** retiring the compatibility files (before SE4);
  corroborated inputs, preserves supported settings, **refuses without mutation** when reconstruction is
  uncertain. Folded in §4.6.3, §11.
- **Q13** sign only if code signing is available at **no cost**; otherwise ship unsigned (the revision-3
  interim stands; a free open-source-foundation path is worth checking). Not a change.
- **Q14** yes to both CC20 amendments.
- **Q15** no: the Windows lab round stays a release gate (SG3 holds).
- **Q16** no this phase: keep the recorded `-job` spelling; `run-job` is the public command.
- **Q17** yes: move the govulncheck target to `./cmd/mailarchive`; scan both Windows build configs.
- **Q18(a)** yes: the P9 renames and shortcut-argument changes, preserving the guarantees.
- ⟐ **Q18(b)** yes, narrowly: render the schedule's kind, health, **Repair and Remove** on the Weekly backup
  card for local-only archives too; no Microsoft 365 install controls there. Folded in §4.6.3, §6, §9.2
  (MA-546).
- **Q18(c)** defer the dashboard add-mail workflow to the SE3 design.
- ⟐ **Q19** (a) a per-user inventory of archives a schedule was installed for, populated from existing
  trusted records and exposed in health views; (c) keep the copies longer where migration cannot be
  established; (b) gate SE4 on the inventory only where it can be checked. SE4 removal is coupled to the
  inventory **and** Q9's floor. The inventory is new per-user state (§4.9): its detailed mechanism and review
  are at its slice (around SE4), and it needs OPEN-X1 (OP-7) for its location. Folded in §7.1, §4.9, §12.
- ⟐ **Q20** no — do **not** accept it as a residual: require a reviewed permissions policy for executable
  selection and scheduling; if the install location cannot be shown to meet it (e.g. writable by another
  account), **refuse** the affected operation with a relocation/reinstallation remedy. The policy definition
  and the refusal's detailed mechanism and adversarial pass are at SE2. Folded in §4.6.0/§4.6.1, §5.1/§5.3.
- **Q21** no: keep the wizard as the macOS double-click; Terminal-only dashboard access is not the permanent
  answer — return with a reviewed macOS graphical launch/reopen solution.

| # | Question | Recommended | Safe interim | Blocks |
|---|---|---|---|---|
| Q1 **OPEN** (unchanged from revision 1) | Verb names: `desktop`, `wizard`, `run-job`? (X4 says verbs; the catalog already has `graph`, `status`, `setup`.) | yes; `open` was considered for the dashboard and rejected as ambiguous with `serve` | as recommended: SE1's texts use the recommended names | nothing under the interim |
| **Q2-mac** **OPEN** | Should the `.app`'s double-click stay the wizard, or become MailArchive Desktop once a gated macOS flip slice passes? Corrected facts (they apply to Q2-linux too): the dashboard does not have the wizard's functions (the Outlook-app export and the `prefs.js` preparation stay wizard-only); it has never shipped on macOS; a second open of a running `.app` may start nothing; and a dashboard opened by double-click assesses only its configured archive, so a wizard schedule on another archive, and its start failure, are not shown there (SC4, SC5) | the wizard; the flip only after the reopen lab (L-M5), the macOS dashboard lab (L-M6) and a macOS walk with its friction review, and with a wizard click route shipped in the same slice | the wizard | the macOS flip slice (under the interim SE1's `.app` verb is the wizard) (SC16, SC19) |
| **Q2-linux** **OPEN** | From the slice that makes the Linux no-argument verb `desktop` (with the SE3 design), should a Linux file-manager launch open the dashboard behind one "Open MailArchive Desktop?" question, or stay the wizard? | the wizard in SE1 and SE2; the dashboard behind the question from that slice | the wizard (and, under Q4's interim, no Linux row at all) | that slice's Linux change (SC16, SC18) |
| Q3 **OPEN** (unchanged from revision 1) | Should the command line's `schedule` install a job-file schedule (`schedule -install -- run-job FILE`), so Windows CLI installs can drop the `.cmd` wrapper and its console flash? The desktop's Weekly backup keeps its always-on wrapper too (non-goal 7, D2), so both the command line's and the desktop's scheduled runs show today's brief console whatever the program inside (SC22) | not in this design; a later slice on a stated need (L-W2 first). Re-installing an existing schedule from the command line is Q12, not Q3; a yes to Q12 narrows Q3 to installing a new job-file schedule (X4) (SC3, SC38) | no | none |
| **Q4** **OPEN** (re-asked) | On Linux, honour a no-argument file-manager launch at all, or require the verb? Honouring costs the narrowed terminal predicate (none of stdin, stdout and stderr is a terminal, and `/dev/tty` cannot be opened), a two-direction L-L1 lab on amd64 and arm64, and, from the slice that makes the Linux verb `desktop`, one extra click (SC18's question) | honour it once L-L1 passes both ways (today's Linux `mailarchive-gui` users double-click) | require the verb: no Linux row; the README names `mailarchive wizard`, `mailarchive desktop` and a `.desktop` entry | the Linux row of §4.3 (SC16) |
| **Q9** **OPEN** (re-asked as a floor) | How long do the Windows former-name copies stay: a minimum number of minor versions after release N and a minimum time after its publication, whichever is later (release N and the floor, §7.1)? Does the macOS link stay longer? | two minor versions and six months, whichever is later; keep the macOS link as long as the legacy spellings | the copies and the link stay in every tag; SE4 does not ship | SE4 (SC37) |
| **Q10** **OPEN** | May SE1 amend design-local-confidentiality §4.C4 step 2 [CC22] so the "our own program" test also accepts the former name `mailarchive-desktop` in the running file's directory, same token user, only for a listener `probeFormer` classes as `FormerPreUpgrade`, on Windows and on Linux? | yes on Windows; on Linux only if L-L3 shows `/proc/<pid>/exe` still names a usable path; the ruling records which adversarial pass reviews the amended text (SE1's §11 pass on §4.5.2, or LC-C2's still-owed pass) | today's exact-path test on both OSes; a pre-upgrade instance outside the running file's path gets CC22's PreUpgrade notice; MA-619, L-W6 and L-L3 are not built | only the widened test (SC31) |
| **Q11** **OPEN** | May the product raise a notification that is not a failed run: (a) one per login from the Startup launch when a schedule names a former name; (b) one from a successful release-N run under a former name? Both reverse MA-124's "success never notifies" posture | yes to (b), once per schedule and in release N only, through the existing best-effort notify; (a) only if (b) is declined | no push; P5, §7.1 and §12 say the break is pull-only; the release notes carry the warning; MA-643 and L-W13 are not built | the push only (MA-643, L-W13) (SC38, SC41) |
| **Q12** **OPEN** | May `schedule` gain `schedule -repair -out DIR`, which re-installs the archive's recorded schedule from the archive's own name, the corroborated kind (§4.6.5) and the running program (touching locked schedule-v2 P2 and X4)? | yes, after SC1 lands and is re-reviewed; Q3 (installing a new job-file schedule from the command line) stays no | the per-kind interactive remedies (§4.6.3, §6); detection by `status`'s `Program:` line; the headless-Linux residual stated; MA-644 is not built | the scripted fleet repair only (SC3, SC38) |
| **Q13** **OPEN** | Should the Windows pair and the MSI be Authenticode-signed? | sign before release N if a certificate can be obtained (an unchanged antivirus verdict re-quarantines what Repair restores) | unsigned, as today; the single point of loss stated as Type II; P4 and "upgrade day is never RED" conditional | nothing under the interim (SC25) |
| **Q14** **OPEN** | CC20 amendments: (a) from a terminal, a `desktop` or `wizard` refusal from the console build goes to stderr alone; (b) on Linux with neither a dialog tool nor a browser opener, `mailarchive desktop` refuses on stderr (without the link), naming what to install | yes to both | CC20 as written: dialog and stderr, each once; (b) stated as an open condition beside P7; MA-642 is not built | nothing (SC26) |
| **Q15** **OPEN** | If the Windows lab round has not been recorded by the first tag after SE1 merges, may SE1 ship for Windows marked lab-pending? | no: SG3 stands, and the round is scheduled with the release-N candidate | SG3 holds; no `v*` tag from a main containing SE1 until the round is recorded, which also holds that tag's macOS and Linux release | SE1 for Windows, and every tag from a main containing SE1 (SC29) |
| **Q16** **OPEN** | May the wizard's recorded schedule argument change from `-job FILE` to `run-job FILE`? It changes what the covering tests of MA-123 and MA-166 assert (rule 2) | no: keep `-job FILE`; the legacy spelling works indefinitely and the rename has no owner benefit | `-job FILE`; MA-95, MA-123 and MA-166 are not edited | nothing (SC42, SC43) |
| **Q17** **OPEN** | May MA-529's text and covering test change their govulncheck target from `./cmd/mailarchive-desktop` to `./cmd/mailarchive` (rule 2)? | yes: the guarantee is kept and only the path follows the move | `cmd/mailarchive-desktop` stays as a thin, unreleased main; CI scans it and `./cmd/mailarchive` | SE1's deletion of that main (SC27, SC50) |
| **Q18** **OPEN** | Rename and narrowing edits under rule 2: (a) the P9 renames and argument changes whose covering tests pin the old text (MA-376's refusal text and its packaging test's shortcut `Arguments`, MA-546's wizard step, and any of X9, S41, MA-271 and MA-278 whose test pins a former name); (b) MA-546 at SE2, the Weekly backup card shown for a local-only archive's recorded schedule; (c) MA-546 at SE3, the add-mail page. May they be made? | (a) yes: only the program's name and the shortcuts' leading `desktop` verb change; (b) not in SE2: the wizard repairs that kind; (c) ask with the SE3 design | (a) SE1's text and packaging commits wait, and SE1 is not released until both land; (b) and (c) MA-546's text and test stand, and on a local-only archive the dashboard shows the schedule's health line naming the wizard's Repair | (a) SE1's release; (c) SE3 (SC21, SC50) |
| **Q19** **OPEN** | After SE4 removes the copies, how does a schedule on an archive no health view opens become visible? Options: (a) a per-user list of archives a schedule was installed for, walked by both health views; (b) gate SE4 on that list naming no former name; (c) keep the copies longer (Q9) | (a), placed under the root OPEN-X1 decides (OP-7) | §12 labels "loud at N+1" conditional; SE4's release note tells the owner to run `status -out` for every scheduled archive | SE4 (SC5) |
| **Q20** **OPEN** | Accept, as a stated residual, a sibling the owner owns but whose permissions let another account write it (for example, both files extracted into a folder that inherits Authenticated Users: Modify)? Owner ruling 13.10 keeps co-account personas in scope, and rule 3 forbids accepting a residual by delegation | accept it, stated on §5.3 row 2 (an account that can write a sibling the owner owns): closing it needs a DACL comparison, a redesign | an open condition stated beside §5.1's boundary 2 and §5.3, not accepted | nothing is held: the open condition stands until the owner answers, and SE2's adversarial pass reports on it but cannot accept it (rule 3) (SC7) |
| **Q21** **OPEN** | If the reopen lab (L-M5) shows a second open of a running `.app` only activates it, should the `.app` ever be the long-lived dashboard (a Cocoa loop against the no-cgo rule, a launcher against desktop P1 and desktop Q14/OR8, or an idle exit against P8/CC21)? | no: macOS keeps the wizard as the double-click; the dashboard is `mailarchive desktop` from Terminal | the wizard | the macOS flip slice, only if the reopen lab fails (SC19) |

Q5, Q6 and Q7 move to the SE3 design with SE3 (§4.8) (SC39). Two changes travel with them: Q5's
recommended answer names §4.6.5's kind guard as how "the second asks" (SC40 R1), and Q6's rationale gains
"and the page does not probe for Outlook either" (SC40 R2).

**Operator items.** Each is the operator's, never self-granted here.

| # | Item | Recommended | Interim |
|---|---|---|---|
| OP-1..OP-3 | catalog rows and xfail encoders, now, for the three gaps in the shipped code at `b1a16bc`: OP-1 the wizard's kind-blind repair and offer, and `installOn`'s cross-kind install (MA-645; R14, R18); OP-2 a capture that refuses at start writes no record (MA-646; R18); OP-3 an absent archive folder is assessed as a new archive (MA-647; R18) | accept now | none: the gaps stand unrecorded until then (SC6) |
| OP-4 | Does SE3 leave this design for its own design and pre-code review (lens 2, SE-21)? | yes: nine rows, three questions and the hardest case of the kind seam, designable only against SE1 and SE2's code | this design is written for "SE3 leaves"; if not, SC40's requirements become §4.8's text and lenses 2, 4, 5, 6 and 7 are re-run on it (SC39) |
| OP-5 | The Windows release asset becomes one zip of install names | yes | until the operator answers, the zip is the design's text and MA-614's zip cases stay skip-encoded; if the operator declines it at SE0's catalog-first PR, the README names the MSI only for Windows, the zip cases are not built, and §12's bare-download row applies (SC11) |
| OP-6 | Allocate this design's ids and the pre-sync design's shifted block in one pass at SE0, with every row, lab item and question this review adds (except the ids of OP-1..OP-3, allocated when the operator accepts them, header), and classify each §10 edit under rule 2 | as listed in revision 2 | every id provisional; no catalog-first PR before the allocation (SC49, SC50) |
| OP-7 | OPEN-X1 (the root for new per-user state; revision 1's Q8) | decide once for both unbuilt designs, per OS, respecting XC3's per-machine records | this design adds no state location and needs no answer (SC51) |

**Defaults taken as marked** (rulings log, "delegated-recommended"; the owner may overrule any):

- **D1** A schedule install or repair from a translocated macOS app refuses rather than offering
  "Continue anyway" (SC8).
- **D2** The desktop's always-on wrapper stays (non-goal 7). It is asked again after L-W2, with SE-C06's
  log-writer precondition attached (SC22).
- **D3** The release-N copies are plain MSI components, not CopyFile entries (SC24).
- **D4** The size and mtime fallback outside the program's directory keeps today's answer under its true
  label, and MA-466 is unchanged (SC2).

## 14. The re-review owed (SC53)

The pre-code review (SG1) ran all ten lenses on revision 1 and is filed as
`docs/review-single-executable-predesign.md`. Its verdict is GO WITH CONDITIONS, bounded. Revision 2
changes the design's digest in §4.3, §4.6, §4.8 and §7, and wherever SC1..SC52 rewrote text, so lenses 3,
4, 6, 7, 8 and 10 are stale for those sections. They are re-run on the changed sections, and a second
reviewer reads the critic's six re-check items, before SE0's catalog-first PR. Lenses 1, 2, 5 and 9 are not
re-run: their findings are folded below and in §15. If revision 2 cannot state SC1 and SC2 as one mechanism
each, the verdict becomes NO_GO on lenses 3, 4, 7 and 8. The re-run was performed and folded in revision 3
(`docs/continuation/single-exe-review/sc53-rerun-2026-10-16.md`; SC1 and SC2 each held as one mechanism, so
the NO_GO trigger did not fire), and the owner performed the human second-reviewer read of the critic's six
items and confirmed them (ruling 13.45), so **SC53 is discharged.** SE0's remaining gate is the operator's
OP-6 id allocation and this revision's folds. The per-slice gates (the friction reviews and the adversarial
passes of §11) are separate and are not discharged by it; the security-touching Q10 and Q20 edits owe SE2's
adversarial pass (SC53).

**Revision 1's lens questions, as the review answered them.** The questions are revision 1's; each answer
names the condition that carries it.

1. **Purpose / threat-model fit:** does the pair over-claim "one program" (R33's wording)?
   Does P5 over-claim "never silent" for Linux? *Answered:* R33's parenthesis now records
   `mailarchivew.exe` only when it is beside the running program under that name, with the same owner and
   the same build (SC12), and MA-612 can now see a self re-exec (SC52). P5: yes; reworded (SC41).
2. **Focus / proportionality:** is SE3 a separate design? Are the copies (§7.1) and the link
   (§7.2) simpler than a repair-on-first-launch? Is `run-job` a rename worth a verb? *Answered:* SE3 is
   its own design, pending the operator (OP-4) (SC39). The skeptics defended the copies (SE-35), and their
   lifetime is now a floor (SC37, Q9). No finding was confirmed against `run-job` as a verb; it stays (Q1).
3. **Durability:** the same-directory identity rule across years of moves and reinstalls; the
   size+mtime fallback; the copies' lifetime; a descriptor that names a former name forever.
   *Answered:* one identity predicate (§4.6.2) (SC2) with one sibling test (§4.6.0) (SC7); the fallback
   says what it decides (SC12); the copies' lifetime is a floor (SC37); a descriptor that still names a
   former name past the floor is RED with the repair its kind offers (SC38). A finding revision 1 did not
   ask: a same-name install silently changed a schedule's kind (SC1). **Stale: re-run.**
4. **Usability:** the §8 walks; the Windows owner who finds two `.exe` files; the Terminal user
   who runs `mailarchive desktop` and gets a dialog *and* stderr; the inheritor. *Answered:* the upgrade
   walk is rewritten per archive (SC5); the double-clicked console build says which file to double-click
   (SC17), the wizard has a click route (SC20) and the Windows release is a zip of install names (SC11);
   the terminal dialog is CC20 as written, stated, with Q14 (SC26); the inheritor reads the `Program:`
   labels (SC10). **Stale: re-run.**
5. **Integration / seams:** build order (§11); the capture slot shared by two kinds of run; the
   picker variant and the workspace's rules; the pre-sync design's job field; MA-529, X9, X11
   edits; the CLI `TestMain` building a bigger program. *Answered:* SE1 records what `b1a16bc` records and
   SE1 and SE2 ship as one release (SC42); SE1 is a pure move first (SC43); MA-614 and MA-628 check the
   gated builders (SC23); CI scans at PR time and at tag time (SC27); the edit list is classified under
   rule 2 (SC50); the covers map stays green between slices (SC48). The picker variant leaves with SE3, and
   the capture slot stays one of §4.8's four seams (SC39). The pre-sync job field rides along unchanged
   (SG4).
6. **Security:** the planted sibling; the forged descriptor against the name rule; the
   no-argument rule as a launch vector; SE3's endpoints; `isOurOwnProgram`'s widening. *Answered:* the
   owner bound and `SameBuild` (SC7); one predicate (SC2); the composed no-argument rule (SC16); SE3's
   endpoints leave with SE3 (SC39); the widening is narrowed and is the owner's (Q10) (SC31). Findings
   revision 1 did not ask: the archive-held `Job[0]` as the source of a repair's kind (SC1), `run-job`'s
   job-file read (SC45). **Stale: re-run.**
7. **Failure domains:** the pair out of step (one file replaced, not the other); a copy deleted
   by an owner "cleaning up"; the link broken by a copy of the `.app` through a tool that drops
   symlinks (zip). *Answered:* the pair out of step is caught by `SameBuild` and `exe_version_skew` (SC7,
   SC2); the deleted or quarantined file is the single point of loss, with its remedy (SC25); the `.app`
   link is pinned by MA-614 with build-app.sh's `-y`. **Stale: re-run.**
8. **Migration:** the N / N+1 story; rollback; mixed versions in one account; RMM scripts.
   *Answered:* release N, the floor, the skip path and pull-only (SC37, SC38, SC41); rollback reads every
   file and is uninstall, then install, on the MSI (SC13, SC36); the older instance and its version (SC31,
   SC32, SC33); the RMM stops and the detection rule (SC34, SC35). **Stale: re-run.**
9. **Dependency and cost:** two builds per release; four Windows files on disk for one release;
   `zenity` linked into the CLI file (it is already linked into the desktop; the CLI file grows).
   *Answered:* two gated builds and two rows (SC23); release N costs a file more, not less (SC24); `zenity`
   does no work at import time (held), and the wizard verb's dependency on it is refused visibly (SC44).
10. **Honesty:** every "conditional" names its lab item; §12 beside the claims. *Answered:* §12 is
    revised; P2's proof is split (SC43); the id range is allocated once (SC49). **Stale: re-run.**

The rescue pass should be handed, in particular: the Windows console constraint (§1.6) and
whether a third option exists that the design rejected too fast; and the copies versus a
first-launch repair. *Answered:* two builds of one package is the minimal answer to §1.6 once
`AttachConsole` is conceded (lens 2), and the copies were defended against RED-plus-repair (SE-35).

**What the re-run attacks first.** Each stale lens reads the changed sections and starts from the
critic's re-check items it owns:

| Lens | Changed sections it reads first | Re-check items |
|---|---|---|
| 3 Durability | §4.6.0, §4.6.2, §4.6.5, §7.1 (the floor) | 1, 2 |
| 4 Usability | §4.3, §4.1 rule 6, §6, §8 | 2, 3 |
| 6 Security | §4.6.0, §4.6.5, §4.5.2, §4.7, §5 | 1, 2, 4 |
| 7 Failure domains | §4.6.6, §7.1 (the single point of loss, a run in flight) | 1, 4 |
| 8 Migration | §7, §4.10, §11 SE4 | 4 |
| 10 Honesty | §12, §13, §15 | 5, 6 |

Each lens also reads the items §15 lists as found while folding, starting with the ones tagged for it.

**The critic's six re-check items**, read by a second reviewer:

1. The composed identity predicate, traced case by case (§4.6.2, over MA-616's composition trace: the MSI
   N-1 copies with a new size and mtime, the macOS link, the CLI tarball copy, a Linux former name, a
   forged descriptor elsewhere, path spellings, a translocated path).
2. The one schedule-kind mechanism (§4.6.5: one taxonomy, the corroboration by owner-side state, the guard
   in `refuseCollision`, the repair that keeps the kind, and the taxonomy words where a surface names the kind).
3. The composed no-argument table (§4.3, with §4.1 rule 6 for former names).
4. The Windows kill scope and the files-in-use path (§4.5.2, §4.10, §7.1).
5. The id and question allocation (§9.2, §9.3, §10, §13) (SC49).
6. The owner-ruling surface: that no "none needed" hides a locked amendment (§13, §15).

## 15. Review record: conditions folded, items still open

Filed review: `docs/review-single-executable-predesign.md` (verdict GO WITH CONDITIONS, bounded, against
revision 1, sha256 `ee4b8fa41c245734a02fa49e19d3cb82bd0b9d0947671d35ee162f445891df18`). Revision 2 folds
SC1..SC53; a condition that waits on the owner or the operator is folded at its safe interim and marked
**OPEN**. The review's conflicts K1..K20 are composed as its table states, each by the condition it names;
K20's card bullet (the card showing a run-job schedule) is built in SE2 under SC1 and SC3, and R1 requires
the SE3 design to keep it (§4.8). The Windows release zip (SC11) is the design's text as the recommended
answer to OP-5, with MA-614's zip cases skip-encoded until the operator answers; if the operator declines it at SE0's catalog-first PR, the zip cases of §4.2, §4.10, §6,
§7.6, §11 and MA-614 are not built, the README names the MSI only for Windows, and §12's bare-download
row applies.
Revision 2, like revision 1, changes no code, no test and no catalog row. Revision 3 (2026-10-16) folds
the SC53 re-run dispositions filed in `docs/continuation/single-exe-review/sc53-rerun-2026-10-16.md` —
five dissolved findings and the mechanism half of four partials — and, like revisions 1 and 2, changes no
code, no test and no catalog row; the security fixes L6-1 (§4.6.5(c)) and L6-2 (§4.6.6) owe SE2's
adversarial pass.

| Condition | Folded where | Status |
|---|---|---|
| SC1 one schedule kind | P2 (f), non-goal 8, §4.4, §4.6.3, §4.6.5, §5.1 boundary 4, §5.2, §5.3, §7.1, §9.2 (MA-617, MA-631, MA-632), §9.4, §10, §11 SE2, §12 | folded |
| SC2 one identity predicate | §4.6.2, §9.2 (MA-616, MA-623), §9.4, §12, D4 | folded; the louder WARN for a former-name file matched only through the fallback is an addition the operator decides at SE0 (OP-6); **OPEN: operator** |
| SC3 per-kind remedies | §4.6.3, §6, §7.3, §9.2 (MA-633), §13 Q3, Q12 | folded |
| SC4 one reader for the start-failure breadcrumb | §4.4, §4.6.6, §9.2 (MA-634), §11 SE2, §12 | folded |
| SC5 the upgrade walk per archive | P4, P5, §4.6.2, §7.1, §8.2, §12, §13 Q19 | folded |
| SC6 encoders now for the shipped gaps | §9.2 (MA-645..MA-647), §11, §12, §13 OP-1..OP-3 | folded as rows; **OPEN: operator (OP-1..OP-3)** |
| SC7 one test for a file beside the program | P4, §4.6.0, §5.1 boundary 2, §5.2, §5.3, §9.3 (L-W7), §12, §13 Q20 | folded; Q20 answered (13.44): refused with a relocation/reinstallation remedy, not accepted; permissions policy and its adversarial pass owed at SE2 |
| SC8 `ProgramForSchedule(wrapped bool)` returns `(exe, reason, error)` | §4.6.1, §9.2 (MA-615, MA-630), §12, D1 | folded |
| SC9 when the re-point repair is offered | §4.6.3, §9.2 (MA-618) | folded |
| SC10 the identity texts | §4.6.2, §6, §8.5, §9.2 (MA-616, MA-629) | folded |
| SC11 the Windows release ships install names | §4.10, §6, §7.6, §9.2 (MA-614), §12, §13 OP-5 | folded; **OPEN: operator (OP-5)** |
| SC12 the identity claims say what holds | §1.3, P3, P4, §4.2, §4.6.2, §5.3, §10 R33, §12, §14 lens 7 | folded |
| SC13 what a pre-SE2 reader sees | P10, §7.4, §9.2 (MA-623), §12 | folded |
| SC14 macOS: the Terminal user runs the `.app`'s program; move the app first | §6, §7.2, §8.3, §9.3 (L-M2, L-M4), §12 | folded |
| SC15 a former-name copy hands its whole argument list to its surface | §4.1 rules 1 and 6, §9.2 (MA-611), §9.4, §12 | folded |
| SC16 one no-argument rule | P7, §4.3, §5.2, §9.2 (MA-620), §9.4, §12, §13 Q2-mac, Q2-linux, Q4 | folded; Q2-mac/Q2-linux/Q4 answered (13.44): wizard double-click (dashboard deferred to SE3); Q4 honours the no-argument Linux launch after L-L1 and provides a `.desktop` launcher template |
| SC17 the double-clicked console build says which file to double-click | §4.2, §4.3, §8.5, §9.2 (MA-620), §12 | folded |
| SC18 a Linux launch that would open the dashboard asks first | §4.3, §11, §12, §13 Q2-linux | folded; owed by the slice that makes the Linux verb `desktop` |
| SC19 Q2 split; the macOS flip slice gated | header (SG3), §1.2, §4.3, §4.10, §7.2, §9.3 (L-M5..L-M7), §11 (the macOS flip slice), §12, §13 Q2-mac, Q21 | folded; Q2-mac/Q21 answered (13.44): wizard is the macOS double-click this release; a reviewed macOS graphical launch returns later, not Terminal-only permanently |
| SC20 the wizard has a click launch everywhere | §4.2, §4.10, §6, §7.3, §8 friction classification, §9.2 (MA-614) | folded |
| SC21 no text depends on what a no-argument launch opens | P9, §4.4, §6, §9.2 (MA-622) | folded; MA-546's text changes wait on Q18 |
| SC22 the console claim covers wrapper-less schedules only | §1.6, P3, non-goal 7, §4.2, §4.6.1, §8.1, §9.3 (L-W2), §12, §13 Q3, D2 | folded |
| SC23 only gated builders make the Windows files; two rows check them | §4.2, §4.10, §9.2 (MA-614, MA-628), §9.3 (L-W1), §12 | folded |
| SC24 the disk and download cost, stated and measured | §1.1, §6, §7.1, §9.3 (L-C1), §12, D3 | folded; the figures wait on L-C1 |
| SC25 the single point of loss | §4.6.1, §4.6.2, §5.3, §7.1, §9.2 (MA-635), §9.3 (L-W9), §12, §13 Q13 | folded; Q13 answered (13.44): unsigned unless a no-cost signing path exists |
| SC26 a shown refusal reaches each channel once | §4.1 rule 4, §4.2, §9.2 (MA-636, MA-642), §9.3 (L-W5), §12, §13 Q14 | folded; Q14 answered (13.44): yes to both |
| SC27 the Windows scan runs at PR time and at tag time | §4.10, §13 Q17 | folded; Q17 answered (13.44): yes — scan `./cmd/mailarchive` |
| SC28 the administrators' texts renamed and swept | P9, §4.10, §6, §9.2 (MA-622) | folded |
| SC29 the lab that discharges SG3 is named | header (SG3), §9.2 (MA-625), §9.3, §11, §12, §13 Q15 | folded; Q15 answered (13.44): no — SG3 stays a release gate |
| SC30 the console build's dashboard ends with its console | §4.2, §6, §8 friction classification, §9.3 (L-W4) | folded |
| SC31 the kill test narrowed and decided by the owner | header (SG4), P2, P6, §4.5.2, §7.1, §9.2 (MA-619), §9.3 (L-W6, L-L3), §11, §12, §13 Q10, §15 | folded; Q10 answered (13.44): yes on Windows after its lab + security review, Linux after L-L3, scoped; owes SE2's adversarial pass |
| SC32 what ending an older instance interrupts is said | §4.5.2, §7.1, §9.2 (MA-638), §12 | folded |
| SC33 the handoff names a holder of another version | P2 (g), P10, §4.5.3, §7.5, §9.2 (MA-637), §12 | folded |
| SC34 upgrade and RMM stops end dashboards only | §4.10, §7.1, §8.6, §8 friction classification, §9.2 (MA-614), §9.3 (L-W3), §12 | folded |
| SC35 the RMM detection rule survives the copies' removal | §4.10, §6, §7.1, §8.6, §9.2 (MA-614), §11 SE4, §12 | folded |
| SC36 rollback on the MSI is a stated procedure | P10, §6, §7.4, §9.3 (L-W10), §12 | folded |
| SC37 the copies' lifetime is a floor; SE4 lists every former-name site | header (vocabulary), non-goal 9, §4.10, §7.1, §7.2, §9.3 (L-W11), §11 SE4, §12, §13 Q9 | folded; Q9 answered (13.44): floor = two minor versions and six months, earliest-removal, coupled to Q19 |
| SC38 the post-SE4 break described honestly | P5, §4.6.3, §6, §7.1, §8.6, §9.2 (MA-643, MA-644), §9.3 (L-W12, L-W13), §12, §13 Q11, Q12 | folded; Q11/Q12 answered (13.44): one notification per affected schedule across the compatibility period; `schedule -repair -out` before SE4 |
| SC39 §4.8 becomes an unapproved stub | header (scope, SG4), P11, non-goals 3, 10 and 11, §4.4, §4.8, §5, §9.2, §10 S59, §11, §12, §13 (Q5..Q7), §14 lens 2 | folded; **OPEN: operator (OP-4)** |
| SC40 the SE3 design's requirements R1..R8 | §4.8, §13 (Q5, Q6 rationales) | folded as the stub's requirements |
| SC41 the upgrade claims say pull-only where nothing can push | P5, §7.1, §7.3, §10 S59, §12, §14 lens 1 | folded |
| SC42 which file each slice records; SE1 and SE2 ship as one release | §4.4, §4.6.4, §9.2 (MA-641), §11, §13 Q16 | folded; Q16 answered (13.44): no — keep `-job`; `run-job` is the public verb |
| SC43 SE1 is a pure move first | P2, §4.1, §4.4, §11 SE1, §12 | folded |
| SC44 the wizard refuses visibly when it cannot show a dialog | P2 (b), §4.1 rule 4, §4.4, §6, §7.3, §8.4, §9.2 (MA-639), §9.3 (L-L2), §12 | folded |
| SC45 `run-job` reads its job file securely | P2 (c), §4.7, §5.1 boundary 5, §5.2, §5.3, §9.2 (MA-640), §12 | folded |
| SC46 `run-job`'s one desktop output is the failure notification | §4.7, §5.4 item 6, §9.2 (MA-624), §9.3 (L-L2, L-C2), §10 S59, §12 | folded |
| SC47 X17 and MA-622 are one enumerated gate | P9, §6, §9.2 (MA-622), §10 X17, §12 | folded; the operator decides at SE0 whether to accept X17's definition (OP-6); **OPEN: operator** |
| SC48 the covers map stays green between slices | §9.1, §11 SE0 | folded |
| SC49 ids are allocated once | header, §9.2, §9.3, §10, §11 SE0, §13 OP-6 | folded; **OPEN: operator (OP-6)** |
| SC50 the edit list names every stale catalog row, classified under rule 2 | §10, §13 Q17, Q18 | folded; Q17/Q18 answered (13.44): Q17 yes; Q18(a) yes, Q18(b) yes-narrowly, Q18(c) to SE3 |
| SC51 no new state root | P8, §1.5, §4.9, §9.2 (MA-621), §12, §13 OP-7 | folded; OPEN-X1 stays the operator's (OP-7) |
| SC52 MA-612 can see a self re-exec | header (SG4), P1, §5.4 item 1, §9.2 (MA-612), §9.4, §10 R33, §12 | folded |
| SC53 re-run the stale lenses before SE0 | header (SG1), §11 SE0, §14 | re-run folded (revision 3), then checked, and the owner's human second-reviewer read confirmed items 1–6 (13.45): **discharged**; SE0's remaining gate is the operator's OP-6 and this revision's folds |

**OPEN items.** None may be closed by weakening a gate, an invariant or a catalog row's guarantee.
- **The owner's:** all of Q1..Q21 are answered in ruling 13.44 (SC53 in 13.45); see §13's owner-answers block. The genuine owner-side residuals are the per-slice adversarial passes the Q10 and Q20 security edits owe (at SE2) and Q19's inventory, designed at its SE4 slice. The per-question `OPEN` markers in §13's table and in the condition rows above are the review-time snapshot, not live status.
- **The operator's:** OP-1..OP-3 (the shipped gaps, now), OP-4 (SE3 leaves), OP-5 (the Windows zip), OP-6
  (ids and rule-2 classes at SE0), OP-7 (OPEN-X1).
- **Defaults taken as marked** (delegated-recommended; the owner may overrule any): D1..D4.
- **Residuals: open conditions, not accepted** (rulings log rule 3; each stays open until the owner
  accepts it, and none may be closed by weakening a gate):
  1. A mistyped or unknown flag on a GUI-subsystem file is refused on stderr only, as at `b1a16bc`
     (§4.1 rule 6) (SC15).
  2. From a terminal, a `desktop` or `wizard` refusal from the console build also shows a modal dialog
     (CC20 as written; Q14) (§4.2) (SC26).
  3. The release environment's `GOFLAGS` and `CGO_ENABLED` apply to both Windows build lines equally
     (§4.2, §12) (SC23).
  4. `mailarchive.exe desktop` holds the shell and ends with its console (§4.2) (SC30).
  5. If L-W4 shows the console process count is unreliable, the double-clicked console build flashes
     as today (§4.3, §12) (SC17).
  6. A healthy older holder keeps the lock until it is quit or the owner signs out, and macOS and
     Linux have no Quit action in the dashboard (§4.5.3, §7.5) (SC33).
  7. A build with no VCS stamp compares on the other fields only (§4.6.0, §5.1, §5.3). (An owner-owned
     sibling another account can write is **no longer an accepted residual**: Q20 (13.44) refuses the
     operation with a relocation/reinstallation remedy; policy, mechanism and adversarial pass at SE2.) (SC7).
  8. On a headless Linux box, since Q12 (13.44) the job-preserving `schedule -repair -out DIR` repairs a
     wizard schedule (it lands before SE4); the lossy remove-and-reschedule is the pre-Q12 fallback only
     (§4.6.3, §7.3) (SC3).
  9. An uncorroborated or unknown recorded kind does not refuse a cross-kind install (§4.6.5, §12)
     (SC1).
  10. The start-failure breadcrumb is one slot per account (§4.6.6, §12) (SC4).
  11. Outside the program's directory the size and mtime fallback keeps today's answer, "unchanged
      since install, not compared with this program" (§4.6.2, §5.3, D4) (SC2).
  12. On Windows the job file's boundary is the per-user `%APPDATA%` ACL (§4.7, §5.1) (SC45).
  13. The single point of loss: `mailarchivew.exe` and its byte-identical copies are lost together
      to a deletion or a hash-based quarantine (§5.3, §7.1, §12; Q13) (SC25).
  14. The RMM's stored detection rule is outside every loud path (§7.1, §12) (SC35).
  15. A dashboard's in-process capture ends with its dashboard at upgrade, and a spared run can still
      be ended by Restart Manager or a reboot after exit code 3010 (§7.1, §8) (SC34).
  16. The zip owner's wizard click route is a hand-made shortcut (§7.6, §8) (SC20).
  17. A bare-download Windows install, if the operator declines the zip (OP-5) (§12) (SC11).
  18. A program path passed through a variable into an opener's argument is not traced by MA-612's
      walk (§5.4, §12) (SC52).
  19. MA-624's notification probe is Linux-only; macOS and Windows rest on L-C2 (§12) (SC46).
  20. After the floor a schedule still on a former name is reported by pull (the health view and
      `status`) as the reliable channel, plus — from Q11 (b) (13.44) — one best-effort notification per
      affected schedule across the compatibility period (§7.1, §7.3, §12) (SC38, SC41).
  21. A forged `export` kind in the descriptor refuses a cross-kind install on the descriptor alone
      (§4.6.5, §5.1, §5.3) (SC1).
  22. A second open of the `.app` while it runs starts nothing (P7, §12) (SC19).
  23. Once a Linux row exists, an unattended graphical start shows the wizard's first dialog, and a
      box without `zenity` prints the usage where no one sees it (P7, §12) (SC18).
  24. The schedule *entry* name a kind with no job file (capture) is installed under is still over the
      raw `out` spelling; canonicalizing it is a one-time pre-SE2 migration — the operator's, riding labs
      L-W8 and L-M2 (§4.6.5(f), §15) (SC1, L3-F1).
  25. A local-only archive's run-job schedule is repaired in place by the Weekly backup card's Repair
      (Q18(b), 13.44) and headless by `schedule -repair -out DIR` before SE4 (Q12); the earlier
      `status -out` + remove-and-reschedule interim is retired (§4.6.3, §6) (SC3).
  26. With no archive location set the start-failure breadcrumb line shows unconditionally; whether to
      suppress it once a non-matching `out` is set is the owner's (§4.6.6) (SC4).
- **The re-run** of lenses 3, 4, 6, 7, 8 and 10 and the critic's six re-check items (§14): done and folded
  in revision 3 (`docs/continuation/single-exe-review/sc53-rerun-2026-10-16.md`), then checked, and the
  owner's human second-reviewer read confirmed items 1–6 (13.45). **SC53 is discharged;** SE0's remaining
  gate is the operator's OP-6 and this revision's folds (SC53).
- **Lab:** every item of §9.3; SG3's tag rule holds until README-RMM §D's round has recorded the Windows
  items (SC29).
- **Found while folding, for the re-run** (the lens that should read each first):
  1. The zip owner's upgrade (§7.6): an untouched old release asset keeps running the older program until
     the owner repairs it. Because `formerName` is a prefix test (§4.6.2), it reads WARN `exe_moved` with
     the repair, never "unchanged since it was installed"; the re-run confirms the walk (lens 8).
  2. The MSI's `ProductVersion` is bound to the tag by a built SE1 step gated by MA-614 (§4.10, §7.1);
     L-W3 still confirms the emitted MSI's `MajorUpgrade` on real Windows (resolved in revision 3, L8-F1).
  3. `ProgramForSchedule(wrapped bool)` takes the wrapper decision as an argument so the console-build
     branch is written once (§4.6.1); whether the branch fires is lab L-W2, and the R33 narrowing is
     settled at SE0 or by the owner under rule 2 (resolved in revision 3, L7-F2).
  4. The Weekly backup card's Repair keeps the existing `schedule-install` endpoint, and the server takes
     the spec from the corroborated kind, never from the request (§4.6.3); SE2's adversarial pass reads it
     (lens 6).
  5. The Windows `exe_missing` install-folder branch is scoped to a name the installed release ships,
     because the installer cannot restore a copy SE4 removed (§4.6.2) (lens 10).
  6. Under the rulings log's rule 3 every "residual" and "Type II, stated" entry is an open condition until
     the owner accepts it (claim labels); the re-run checks that none is written as accepted (lens 10).

**The CC22 edit.** The edit to design-local-confidentiality §4.C4 step 2 and its [CC22] fold lands only
with the owner's Q10 ruling, entered in that design's review record, which names the adversarial pass that
reviews the amended text (SE1's §11 pass on §4.5.2, or LC-C2's still-owed pass). Until then this design's
§4.5.2 states today's exact-path test, and no other design is edited (SC31).

**Build status after this revision:** reviewed (GO WITH CONDITIONS, bounded); conditions folded (rev 2); the
SC53 re-run folded (rev 3) and **SC53 discharged** — re-run done, checked, and the owner's human
second-reviewer read confirmed (13.45); the owner's Q1..Q21 answers folded (rev 4, ruling 13.44), of which the
six deviations are folded into their mechanism sections and two (Q10, Q20) owe SE2's adversarial pass.
Approved scope SE0, SE1, SE2 and SE4; **SE0's remaining gate is the operator's OP-6 id allocation and §10
rule-2 classification.** SE1 and SE2 ship as one release (SC42); SE3 is not approved, and this design is
written for it to get its own design pending the operator's OP-4 (§4.8) (SC39). Nothing is built and nothing
is lab-validated.
