## Conditions

All are folded into revision 2 unless marked **OPEN**. Each is a tracked change to the design, re-verified by
the re-run of SC53 and by each slice's catalog-first PR. The full text of each change is in the design at the
sections §15 names; the entries below say what it is and what it covers.

**The two that carry the verdict**

- **SC1 One schedule kind.** New §4.6.5: a job's kind is named from its leading arguments against one fixed
  set (`verify`; `run-job` and legacy `-job`; `capture`, that is `desktop -capture` and legacy `-capture`;
  `export`), never as a path, in the same words on every surface. A recorded run-job or capture kind counts
  only when owner-side state corroborates it (the archive's job file; the desktop configuration for that
  archive), because the descriptor's `Job` is archive-held. `schedule.refuseCollision` refuses a cross-kind
  install under the archive's name before any wrapper, job file or scheduler call; a same-kind re-install
  stays idempotent (R14). A repair rebuilds the corroborated kind only, or refuses naming the surface that
  can, and is offered only when exactly one of run-job and capture is corroborated. A capture or export
  install under a name deletes that name's job file, which no entry can run. P2 names the new refusal as its
  SE2 exception. Rows: the guard for each pair of kinds, the kind-keeping repair from each surface, the
  `Job[0]` flip; prove-fail: collapse `jobKind` to two values. Rests on SC2. (SE-01, SE-18, SE-26, SE-39,
  SE-64, SE-C04.)
- **SC2 One identity predicate.** §4.6.2's rule becomes one predicate, written once for the backup's and the
  verify's descriptors, first match wins: missing is RED `exe_missing`; a translocated path is WARN
  `exe_moved`; the same file is this program; a known name in the program's own directory is this program
  only through SC7's sibling test (else `exe_version_skew` or `exe_moved`), and the size and mtime fallback is
  not consulted there; a former name never becomes this program through the fallback; elsewhere the fallback
  keeps today's answer under its true meaning, "unchanged since install, not compared with this program"
  (D4). The directory comparison is specified per OS. `exeIdentity` returns its basis. MA-586 carries the
  composition trace (the MSI N-1 copies with new size and mtime, the macOS link, the CLI tarball copy, a
  Linux former name, a forged descriptor elsewhere, path spellings, a translocated path), and MA-593 and
  §9.4's prove-fail are restated so they can go red. The louder WARN for a former-name file that matched only
  through the fallback is an addition the operator accepts at SE0. (SE-15, SE-16, SE-17, SE-36, SE-37, SE-40,
  SE-47, SE-59, SE-77.)

**Schedules and repair (with SC1)**

- **SC3 Per-kind remedies.** One function decides each schedule's repair route (run-job: the wizard's Repair,
  or the card's where the card is shown; capture: the card's; export: the `scheduleCommand` line; none: the
  explicit installs), and every Repair button and every schedule remedy is rendered from it, so no text names
  a button that is not on its page or a command that refuses. `scheduleCommand` serves the export kind only;
  §4.6.3's "`schedule -install` is the repair" is bounded to command-line schedules; the `Program:` line names
  the installing surface; §7.3 drops its `schedule … -install` parenthesis. Q3 stays no; the scripted repair
  is Q12. (SE-18, SE-62.)
- **SC4 One reader for the start-failure breadcrumb.** In SE2 `health.Gather` reads the breadcrumb `jobFail`
  writes, for this archive's schedule name only, into RED `start_failed` under MA-168's show rule, shown first
  by `status` (an additive JSON code), the dashboard (its empty state included) and the wizard; the wizard
  stops reading the file itself; file, format and toast are unchanged. Residual stated: one slot per account.
  (SE-20.)
- **SC5 The upgrade walk per archive.** §8.2 is rewritten per archive (each health view assesses one archive),
  counts the context switch and names which Repair each click is; §7.1, P5 and §12 say "the health view of
  the schedule's own archive"; P4 and §4.6.2 scope the wizard's view to its last archive; §12 labels "loud at
  N+1" conditional until Q19 is answered. SE1's and SE2's friction reviews measure the rewritten walk.
  (SE-39, SE-62, SE-01.)
- **SC6 Encoders now for the shipped gaps.** OP-1, OP-2 and OP-3 above get operator-accepted rows and xfail
  encoders now, as real-primitive tests; OP-1's skip comes off with SC1 in SE2. **OPEN: operator.** (SE-01,
  SE-20, SE-26, SE-C04.)

**Program identity (with SC2)**

- **SC7 One test for a file beside the program.** New §4.6.0: `SameBuild` (build info equal on path, main
  module, version, Go version, dependencies and every setting except `-ldflags`, the VCS stamp included) and
  `SiblingQualifies` (a regular file, not a link; the running file's owner, compared file to file; the same
  build). It is the only code that reads a sibling; it reads build info and never executes the file, and runs
  only on a path already in the program's own directory. §5.1.2's "adds no principal" becomes the owner
  bound, with two open conditions stated: an owner-owned sibling another account can write (Q20) and a build
  with no VCS stamp. **OPEN: Q20.** (SE-15, SE-16, SE-72.)
- **SC8 `ProgramForSchedule` returns `(exe, reason, error)`.** Self is `os.Executable()` through
  `EvalSymlinks`; a translocated self is `ErrTranslocated` (refused, D1); on Windows the sibling
  `mailarchivew.exe` is returned only when SC7's test passes, otherwise self with exactly one reason. One
  rendering prints on every install and repair surface, and every caller calls it before `job.Write` and
  `schedule.Install`, so a refusal writes nothing. (SE-14, SE-15, SE-36, SE-72.)
- **SC9 When the re-point repair is offered.** `Input.ExeRepoint` (basis same file or program name, no
  `ProgramForSchedule` error, a recorded path that differs as a path) joins the wizard's `repairable` and the
  dashboard's Repair condition. It covers the former-name note, the console-build note once `mailarchivew.exe`
  is back, and the macOS link, without loops. Every repair asks the existing volatile-directory question
  first; Cancel installs nothing. (SE-14, SE-17, SE-40.)
- **SC10 The identity texts.** The `Program:` line prints one label per state in a fixed order (missing, with
  the former-name hint; translocated; a different build; not this program; a former name; the command-line
  build; unchanged since install; this program). `exe_former_name` gains a "Do this" naming the surface's
  Repair and "Nothing changes until you do"; new note `exe_console_build`; new WARN `exe_version_skew`.
  (SE-14, SE-15, SE-60, SE-76.)
- **SC11 The Windows release ships install names.** The Windows release asset becomes one zip holding
  `mailarchive.exe`, `mailarchivew.exe` and a READ ME under their install names (the MSI payload's files);
  no suffixed bare `.exe` is published; the README names the zip or the MSI; a new §7.6 covers the zip
  owner's upgrade; the suffixed-name rows stay only as defence for renamed copies; SE-28's README line for a
  suffixed file is dropped. MA-584 asserts the members and their byte-identity with the payload. **OPEN:
  operator (OP-5).** (SE-09, SE-14, SE-15, SE-16.)
- **SC12 The identity claims say what holds.** P4, R33's parenthesis and §4.2's row record `mailarchivew.exe`
  only "when it is beside the running program under that name, with the same owner and the same build"; P3
  and §12 add "the pair installed together under its install names"; §1.3 names the real trigger of today's
  false `exe_moved`; §4.6.2's honesty sentence says what the fallback decides; §12 gains a row for a second
  copy in another directory. (SE-14, SE-15, SE-16, SE-40, SE-47, SE-77.)
- **SC13 What a pre-SE2 reader sees.** §7.4, P10, MA-593 and §12 state it: "this program" and silent while the
  file is present and unchanged, RED once removed, WARN only if rewritten in place. "Never this program and
  never silent" is deleted. (SE-37.)
- **SC14 macOS: the Terminal user runs the `.app`'s program, and the app is moved before it is opened.**
  §7.2: on a Mac with the `.app`, Terminal reaches the program inside it through one `/usr/local/bin` link;
  two copies are two installs. The README, README-macOS and the release notes say to drag the app to
  Applications first. "Keeps running" holds "for a bundle at a stable path". (SE-17, SE-36, SE-40.)

**Dispatch and launch**

- **SC15 A former-name copy hands its whole argument list to its own surface.** §4.1 rule 6: started under a
  former name (the unresolved invoked base name), the program does not consult the first-argument switch;
  `mailarchive-desktop*` passes every argument to the launcher's flag set and `mailarchive-gui*` to the
  wizard's legacy one-flag set, so every list either file accepted at `b1a16bc` behaves the same and every
  list it refused is refused the same way. The residual (a mistyped flag on a GUI-subsystem file is refused
  on stderr only, as today) is stated. Rule 6 retires with the copies. (SE-06, SE-54.)
- **SC16 One no-argument rule.** §4.3 becomes `defaultVerb(goos, consoleAttached, consoleIsOwn, inBundle,
  noTerminal, hasDisplay)`, consulted only under the program's own names with an empty argument list. Windows
  is total and keyed on the subsystem (no console, any name: `desktop`; a console: the usage refusal, SC17).
  macOS and Linux share one terminal predicate: none of stdin, stdout and stderr is a terminal and `/dev/tty`
  cannot be opened. The macOS bundle row gives `wizard`; the Linux row exists only after Q4 is answered
  "honour" and L-L1 passes both ways, and gives `wizard`; each verb is a per-slice constant. MA-590 pins the
  rows and the suffixed, renamed, shared-console, PATH-symlink, pipe, `</dev/null` and `nohup` cases.
  **OPEN: Q2, Q4.** (SE-08, SE-09, SE-10, SE-52, SE-53, SE-54, SE-73, SE-C01, SE-C02.)
- **SC17 The console build, double-clicked, says which file to double-click.** When `mailarchive.exe` is the
  only process in its console, it writes an X2 pointer naming the GUI file by the name it has (or only the
  shortcut), then today's refusal line and exit code, and waits for Enter (EOF ends the wait). It never starts
  the sibling. If the extended L-W4 shows the process count is unreliable, the case collapses into today's
  refusal and §12 states the flash. (SE-08.)
- **SC18 A Linux launch that would open the dashboard asks first.** Owed by the slice that makes the Linux
  no-argument verb `desktop` (with the SE3 design): a bounded "Open MailArchive Desktop?" question before any
  lock or bind; Cancel, timeout or no dialog tool gives the usage refusal. (SE-10.)
- **SC19 Q2 split; the macOS dashboard double-click is its own gated slice.** §1, §4.10 and §7.2 state that the
  dashboard has never shipped on macOS or linux/arm64; SE1 ships it there as `mailarchive desktop`, labelled
  "not lab-run". Switching the `.app`'s double-click to the dashboard is a separate slice after SE3, shipped
  only when the reopen lab, the macOS dashboard lab, a macOS walk and its friction review pass, Q2-mac says
  "the dashboard", and a wizard click route ships with it. **OPEN: Q2, Q21.** (SE-C01, SE-C02, SE-28,
  SE-56.)
- **SC20 The wizard has a click launch everywhere.** An MSI Start-menu shortcut "MailArchive wizard"
  (`mailarchivew.exe wizard`), pinned by MA-584; the Outlook-app refusal names it; a README `.desktop` entry
  on Linux; on macOS the `.app` is the wizard while its verb is the wizard, and the flip slice must ship
  another route. §8 lists each route's friction type. (SE-28, SE-73.)
- **SC21 No text depends on what a no-argument launch opens.** §4.4 drops the claim that the `gui` remedy
  text stays true; the run-job rebuild action becomes a named constant naming both surfaces; the wizard texts
  that send the owner to "this program" name the wizard by its click route; `addMailHelp` gets one text per
  slice and OS; MA-592 refuses the phrases. The MA-546 text changes go to the owner (Q18). (SE-56, SE-73.)

**Windows pair, build and channels**

- **SC22 The console claim covers wrapper-less schedules only.** A scheduled run shows no console only when
  the task's command is `mailarchivew.exe` itself; the desktop's and the command line's always-on wrappers
  keep today's brief console (non-goal 7, D2). §4.2, P3, §1.6, §4.6.1, §8.1, §12 and Q3 say so. L-W2 checks
  that both wrappers wait for the GUI-subsystem child, capture its stderr and pass its exit code; if any
  fails, `ProgramForSchedule` returns the console build for a wrapped spec. (SE-02, SE-78.)
- **SC23 Only gated builders make the Windows files, and two rows check them.** `build-release.sh` and the
  Makefile's `build-windows` are the builders; the continuation scripts call `make build-windows`. MA-584 (S)
  parses the builders, the payload, the shortcuts, Build-MSI.ps1's two lists and the release asset; a new
  tier-U row builds the pair inside `go test ./...` and asserts Subsystem 3 and 2 (`debug/pe`) and equal build
  info but `-ldflags` (`debug/buildinfo`). The `-trimpath` fact is labelled conditional. (SE-07, SE-70.)
- **SC24 The disk and download cost, stated and measured.** Release N installs four Windows files, about one
  file and 40 MB more than today; N+1 about one file and 20 MB less; the release-N payload and MSI carry the
  GUI bytes three times. L-C1 measures each figure (D3). (SE-31.)
- **SC25 The single point of loss.** §7.1 states that after SE2 one file, `mailarchivew.exe`, carries every
  Windows schedule and shortcut, lists what the owner sees when it is deleted or quarantined, and §5.3 gains
  that persona; the Windows `exe_missing` remedy gains an install-folder branch (restore with the installer's
  Repair, restoring and allowing it first if antivirus removed it). **OPEN: Q13.** (SE-32.)
- **SC26 A shown refusal reaches each channel once.** The merged main does not reprint a refusal the
  `desktop` or `wizard` verb has shown (an exported marker, `errors.As`); a process-level row proves it.
  §4.2 states the channel rule: CC20 as written, with the terminal duplicate stated. **OPEN: Q14.** (SE-50,
  SE-51.)
- **SC27 The Windows scan runs at PR time and at tag time.** ci.yml scans `./cmd/mailarchive` for
  GOOS=windows and release.yml gains the step before building; the test finds each step by content. The
  target change of MA-529 is the owner's; until then `cmd/mailarchive-desktop` stays as a thin, unreleased
  main and CI scans both. **OPEN: Q17.** (SE-69, SE-71.)
- **SC28 The administrators' texts are renamed and swept.** README-RMM.md, DEPLOYMENT.txt, README-payload.txt
  and BUNDLE-CONTENTS.txt join §6's, P9's and X17's lists, with README-RMM's checks rewritten for the pair.
  (SE-74, SE-C05.)
- **SC29 The lab that discharges SG3 is named.** The Windows items run in README-RMM.md §D's hand-validation
  round on the MSI builder's machine and are recorded in a named file; no `v*` tag is pushed from a main
  containing SE1 until then, which also holds that tag's macOS and Linux release (stated). **OPEN: Q15.**
  (SE-79.)
- **SC30 The console build's dashboard ends with its console.** §4.2, the verb's `-h`, the README and
  README-RMM state that `mailarchive.exe desktop` holds the shell and ends with it, and that `mailarchivew.exe
  desktop` outlives the terminal. (SE-C05.)

**Upgrade, rollback and the lock**

- **SC31 The kill test is narrowed and decided by the owner.** P6 stops claiming the instance rules are kept
  exactly; §4.5.2's widening shrinks to the former name `mailarchive-desktop` in the running file's
  directory, same token user, reached only on `FormerPreUpgrade`, on Windows and Linux, labelled an amendment
  of CC22. Until Q10 is answered both OSes keep today's exact-path test, and MA-589 and L-W6 are not built.
  **OPEN: Q10.** (SE-04, SE-38, SE-57.)
- **SC32 What ending an older instance interrupts is said.** `endPreUpgrade` ends any run the older instance
  holds (R5 keeps the archive at its last checkpoint; RED `run_never_finished` until the next run); the end
  is recorded and named in the new dashboard's header and in `status`. (SE-58.)
- **SC33 The handoff names a holder of another version.** `instance.Info` gains an optional `version`; after a
  verified handoff the launcher names a holder of another or no version, with its pid and program, and still
  hands off; §7.5 states that a healthy old holder keeps the lock until quit or sign-out. P2 and P10 name the
  field as their one format exception. (SE-38.)
- **SC34 Upgrade and RMM stops end dashboards only.** `util:CloseApplication` keeps its one target,
  `mailarchive-desktop.exe`; a running `mailarchivew.exe` is left to files-in-use handling; Install-RMM.ps1
  stops a `mailarchivew` process only when its command line is a dashboard; Uninstall-RMM.ps1 and
  Cleanup-CurrentUser.ps1 add `mailarchivew` and keep the former names; §7.1 states what a run in flight
  becomes, the reboot-skew window and the backup-window guidance. (SE-05, SE-29, SE-41.)
- **SC35 The RMM detection rule survives the copies' removal.** Release N's Detect-RMM.ps1 tests the DC6 key
  and both files of the pair and names no former name; README-RMM and the release notes tell administrators
  to replace the stored rule before the release that removes the copies, and SE4 waits until that text has
  shipped. (SE-42.)
- **SC36 Rollback on the MSI is a stated procedure.** P10 becomes "rollback reads every file; on the MSI it is
  uninstall, then install", conditional on a rollback lab item; §7.4 states the procedure and the per-kind
  remedy that works at N-1. (SE-05.)
- **SC37 The copies' lifetime is a floor, and SE4 lists every former-name site.** §7.1 defines release N and
  keeps the copies and the `.app` link until the owner's floor; every "one release" points at it; the skip
  path is stated; SE4's cell lists each former-name site to retire or keep, with the matching row edits.
  **OPEN: Q9.** (SE-34, SE-81.)
- **SC38 The post-SE4 break is described honestly.** After SE4 a schedule still on a former name is RED in
  every surface and nothing is pushed; the fleet path is `status -out DIR` plus a per-user interactive repair,
  and administrators hold SE4 until detection finds none; the release notes name dead pins, the stored
  detection rule and the skip path. **OPEN: Q11, Q12.** (SE-33, SE-75.)

**SE3 leaves this design**

- **SC39 §4.8 becomes an unapproved stub.** P11, §4.8's design text, the picker file variant, Q5..Q7, the SE3
  rows, gates and walk steps, its rows in §5, its §12 claim and S59's clause leave this design. Four seams
  stay: the job-file kind (SC1), `run-job`, `ProgramForSchedule`, and the capture slot as one in-process mutex.
  No SE1 or SE2 text names "Add mail from this computer". **OPEN: operator (OP-4).** (SE-21 and the SE3
  findings.)
- **SC40 The SE3 design's requirements.** The stub lists them: R1 one kind per archive through SC1's guard;
  R2 no COM object in the dashboard process; R3 the picker refused while the capture slot is held, with its
  own endpoint; R4 the run's options from an extracted `wizard.OptionsFor`, not `jobFor`; R5 cancel, drain, a
  stated bound and a log file for the page's run; R6 owner-named paths through the engine's recogniser before
  the slot, with the listing claim scoped; R7 every endpoint through `d.api`; R8 reused texts quoted exactly
  and completed for the surface. (SE-21..SE-27, SE-49, SE-61, SE-63, SE-65, SE-66.)

**Claims, slices, tests and ids**

- **SC41 The upgrade claims say pull-only where nothing can push.** P5 becomes "seamless where a former file
  can stay, and discoverable, never wrong, where it cannot"; "loud" means RED in every health view; §7.3's
  README step says to run `status` at once; §12 gains the pull-only row; S59's cells are rewritten without
  SE3's clause. (SE-03, SE-43.)
- **SC42 Which file each slice records; SE1 and SE2 ship as one release.** SE1 records what `b1a16bc` records
  (`os.Executable()`, `-job FILE`); SE2 switches all four call sites to `ProgramForSchedule()` in one commit;
  no release is cut from SE1 alone; `offerSchedule`'s spec is extracted as a pure `installSpec` and a row
  proves install and repair agree. **OPEN: Q16** (the recorded spelling). (SE-11, SE-12.)
- **SC43 SE1 is a pure move first.** The first commit is verifiable with `git diff -M b1a16bc`, with thin mains
  kept so every moved test passes; no `Dialogs` interface in SE1; each later commit makes one named
  behaviour change with its row. P2 lists its exceptions and splits its proof: rows for refusals, codes,
  routes, guards, formats and decision functions; the recorded move diff, MA-95 (lab) and SE1's friction walk
  for the wizard's dialogs. (SE-11, SE-12, SE-48.)
- **SC44 The wizard refuses visibly when it cannot show a dialog.** It writes errors and recovered panics to
  stderr before any dialog, and on Linux and the BSDs refuses before the first dialog when no dialog tool is
  on PATH or no display is set, in X2 words naming no repair it cannot deliver. (SE-13.)
- **SC45 `run-job` reads its job file securely.** Through `util.ReadSecureFile` (a regular file, no link, a
  no-follow non-blocking open, owner-only mode on Unix, 1 MiB), every refusal through `jobFail`, the failure
  sink opened no-follow; §5.1 states the boundary per OS, with Windows's per-user ACL an open condition.
  (SE-19.)
- **SC46 `run-job`'s one desktop output is the failure notification.** §5.4 item 6 states the exception;
  MA-603's Linux probe asserts one notification for a failing run and none for a healthy one; the probe is
  Linux-only, with a macOS and Windows lab twin. (SE-30.)
- **SC47 X17 and MA-592 are one enumerated gate.** X17 claims one name over MA-592's swept set; a former name
  may appear only on a line that also says "former name", never as the thing to run, download or build; the
  release-N machine scripts are outside the sweep and pinned by MA-376 and MA-584; no allowlist and no
  suppression. (SE-44.)
- **SC48 The covers map stays green between slices.** Every U/S row of the block lands at SE0 with a skip
  encoder naming its id and the slice that turns it green; L rows get none; rows spanning slices keep
  per-case skips (FC33). (SE-45.)
- **SC49 Ids are allocated once.** Revision 2 proposes MA-610 onward (MA-580 belongs to the pre-sync block
  after its shift); the operator allocates both designs' blocks and every id this review adds in one pass at
  SE0; no rescue-named id stands. **OPEN: operator (OP-6).** (SE-46.)
- **SC50 The edit list names every stale catalog row, classified under rule 2.** §10 lists each row the slices
  make stale with its slice and PR; additions, and renames whose covering tests do not change, are the
  operator's; edits that narrow text or change what a covering test asserts go to the owner (MA-529, Q17;
  the renames whose tests pin a former name, MA-376 among them, and MA-546, Q18; MA-95, MA-123 and MA-166
  only on Q16's yes). **OPEN: Q17, Q18.** (SE-80, SE-44, SE-45.)
- **SC51 No new state root.** §4.9 drops `StateRoot()`; this design adds no per-user state location; OPEN-X1
  stays the operator's (OP-7); MA-591 becomes an S row pinning the set of per-user state call sites.
  (SE-67.)
- **SC52 MA-582 can see a self re-exec.** MA-582 becomes S and U: an AST walk lists every process-start site
  under `cmd/` and `internal/`, requires a literal from the closed set of OS tools or a pinned argv builder,
  and fails on `os.Executable()`, `ProgramForSchedule()`, `os.Args`, a sibling join and the scheduler's
  run-now words; recording fakes on PATH back it. (SE-C03.)

**The re-review**

- **SC53 Re-run the stale lenses before SE0.** Revision 2 changes the digest in §4.3, §4.6, §4.8 and §7 and
  wherever SC1..SC52 rewrote text. Lenses 3, 4, 6, 7, 8 and 10 are re-run on the changed sections, and a
  second reviewer reads the critic's six re-check items, before SE0's catalog-first PR. If revision 2 cannot
  state SC1 and SC2 as one mechanism each, the verdict becomes NO_GO on lenses 3, 4, 7 and 8. **OPEN until
  run.**
