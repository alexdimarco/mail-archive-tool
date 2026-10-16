# Revision 2 spec (shared by every section drafter)

Revision 2 of `docs/design-single-executable.md` folds the conditions SC1..SC53 of
`docs/review-single-executable-predesign.md`. Each changed passage ends with the condition it folds in
parentheses, e.g. "(SC7)", as `docs/design-thunderbird-presync.md` revision 2 does with "(PC3)". Text that no
condition touches stays word for word. Ids follow the allocation tables below exactly; never invent an id,
a question number or a lab id. A section that SE3 owned says it moved (SC39) and keeps only the seams SC39
names.

## Outline: every heading of revision 2, what changes there, and which conditions land

**Header block.** Revision 2 (2026-10-16). BUILD STATUS: reviewed (GO WITH CONDITIONS, bounded), conditions
folded; approved scope SE0, SE1, SE2 and SE4 once the stale lenses are re-run (SC53); SE3 is not approved and
gets its own design (§4.8); not built, not lab-validated. The review is filed as
`docs/review-single-executable-predesign.md` (against revision 1, sha256 ee4b8fa4...). OPEN items are listed
in §15. Base and id allocation: provisional block MA-610 onward (SC49), with the sentence that the operator
allocates this block and the pre-sync design's shifted block (which ends at MA-580 at least) in one pass at
SE0, and that R33, S59, X17, the MA, L- and Q ids are all re-checked then. Owner request: unchanged. Gates
before code: SG1 the review, filed, and the re-run of SC53 owed before SE0; SG2 owner answers (each has a safe
interim; §13 says which slice each blocks); SG3 lab items per OS, plus the release rule that no `v*` tag is
pushed from a main containing SE1 until README-RMM §D's round has recorded the Windows items (SC29), and the
macOS flip slice's own labs (SC19); SG4 seams: the pre-sync job-file field, design-local-confidentiality §4.C4
step 2 [CC22] (SC31), the reading of desktop P1 as "no child of the program, not every child process"
(SC52); the extraction workspace's picker seam is no longer touched (SC39). Claim labels: unchanged.
Vocabulary adds *kind* (§4.6.5), *install names*, *release N* and *the floor* (§7.1).

**§1 Problem.** 1.1 adds release N's disk clause (SC24). 1.2 adds that the dashboard has never shipped on
macOS or linux/arm64 (SC19). 1.3 names the real trigger of today's false `exe_moved` (SC12). 1.5 states the
three helpers' real values (SC51). 1.6 says the desktop's weekly backup runs through an always-on `.cmd`
wrapper and shows a brief console today (SC22).

**§2 Properties.** P1 unchanged in substance, cites MA-610, MA-612, MA-613. P2 lists its exceptions (SC43):
(a) the two legacy spellings run `run-job` and `desktop -capture`; (b) the wizard verb's stderr and its
preflight refusal (SC44); (c) `run-job`'s secure read (SC45); (d) the program's name in printed text (P9);
(e) only on Q16's yes, the recorded `run-job FILE`; from SE2, (f) the cross-kind refusal (SC1); (g) the
instance file's optional `version` field (SC33). Its proof sentence is split (SC43). P3 conditional on
wrapper-less schedules and the pair installed together under install names (SC22, SC12). P4 records
`mailarchivew.exe` only when it qualifies (SC7, SC12); agreement holds within one install directory; the
wizard's view is its last archive (SC5). P5 becomes "The upgrade is seamless where a former file can stay,
and discoverable, never wrong, where it cannot" with the pull-only second arm (SC41, SC5, SC38). P6: the
instance rules are unchanged except §4.5.2, an amendment of CC22 pending Q10 (SC31). P7: Windows total rule;
macOS bundle gives the wizard (Q2-mac interim); no Linux row until Q4; residuals (SC16, SC19, SC17). P8: "This
design adds no per-user state location; existing files do not move" (SC51). P9: the swept set and the phrase
rule (SC47, SC28, SC21). P10: "Rollback reads every file; on the MSI it is uninstall, then install" (SC36,
SC13). P11: moved to the SE3 design (SC39).

**§3 Non-goals.** 1, 2, 4, 6 unchanged. 3 adds "and no new dashboard route" (SC39). 5 says signing is not
done for macOS and Linux, and that Windows signing is Q13. 7 unchanged, plus: the desktop's always-on wrapper
stays (D2), and any later question to drop it carries SE-C06's log-writer precondition. 8 cites §4.6.5 for
"one kind per archive". 9: the copies stay until the floor (SC37). 10: this design adds no endpoint (SC39).
New 11: no dashboard page that adds local mail; SE3 is its own design (§4.8, SC39).

**§4.1 The dispatcher.** The table's `wizard` row runs `wizard.Run(ctx)` (no `Dialogs`, SC43). Rule 1: the
exact legacy shapes apply under the program's own names only (SC15). Rule 4: a recovered panic goes to
stderr and a dialog; a refusal the verb has already shown is not reprinted (SC26, SC44). New rule 6: a
former-name start hands its whole argument list to its own surface (SC15), with the mistyped-flag residual.

**§4.2 The Windows pair.** Builders and the two rows (MA-614 S; MA-628 U, SC23); the `-trimpath` fact
labelled conditional; L-W1 informational. "Which file does what" splits schedules without a wrapper from
wrapped ones (SC22), adds the console build's `desktop` row (SC30) and the Start-menu wizard shortcut (SC20).
The dialog helpers' channel rule: CC20 as written, the terminal duplicate stated, Q14 (SC26). The
double-clicked console build points at §4.3 (SC17).

**§4.3 What a launch with no argument does.** The composed table and its inputs (SC16), the console build's
pointer and wait (SC17), the Linux question owed by the slice that flips the Linux verb (SC18), the per-slice
verbs and the macOS flip slice (SC19). Former names are §4.1 rule 6, not this table.

**§4.4 The wizard moves to `internal/wizard`.** A pure move first, no `Dialogs` interface in SE1 (SC43); what
SE1 and SE2 record (SC42, Q16); `installSpec` (SC42); `repairSpec` takes cadence, time and the corroborated
kind from the descriptor, nothing that names a path (SC1); the `gui` remedy claim dropped and the run-job
rebuild action named (SC21); the wizard verb's stderr and preflight (SC44); `lastfailure.go` moves whole in
SE1 and its reader moves to `internal/health` in SE2 (SC4); `internal/wizard` never imports `internal/desktop`
(SC39).

**§4.5 The desktop launcher moves to `internal/desktop`.** 4.5.1 the move, unchanged. 4.5.2 the narrowed
"our own program" test, conditional on Q10, with what ending an older instance interrupts (SC31, SC32).
4.5.3 the instance file's optional `version` and the handoff notice (SC33).

**§4.6 Schedules.** 4.6.0 (new) a file beside the program: `SameBuild`, `SiblingQualifies` (SC7). 4.6.1
`ProgramForSchedule() (exe, reason, error)` and its one rendering (SC8), the wrapped-spec rule (SC22), the
missing-sibling note (SC25). 4.6.2 the composed identity predicate (SC2), its texts (SC10) and the Windows
install-folder branch of `exe_missing` (SC25). 4.6.3 the repair: when it is offered (SC9), what it rebuilds
(SC1), which route each kind names (SC3), the volatile-directory question, no repair unasked. 4.6.4 the
`-capture` form unchanged in SE1/SE2; from SE2 `Exe` from §4.6.1 and `Args` led by `desktop` (SC42). 4.6.5
(new) which kind a schedule is (SC1). 4.6.6 (new) the start-failure breadcrumb (SC4).

**§4.7 `run-job`.** The secure read and the no-follow sink (SC45); its one desktop output, the failure
notification (SC46); the Outlook probe question, lab L-W14.

**§4.8 The dashboard adds local mail (SE3): moved to its own design.** An informative stub, not approved and
not reviewed (SC39): the intent, the four seams kept here, and the requirements R1..R8 (SC40) with their
rows to be proposed by that design.

**§4.9 No new state location.** `StateRoot` dropped; OPEN-X1 stays the operator's (SC51).

**§4.10 Packaging.** The gated builders (SC23); the Windows zip of install names (SC11); the MSI payload,
the shortcuts with the wizard's (SC20), the single `CloseApplication` target (SC34), plain components (D3),
Build-MSI.ps1's lists (SC23); the RMM scripts (SC34, SC35); `Info.plist.in` and the link, kept to the floor
(SC37); CI and release.yml (SC27, with the thin-main interim of Q17); the Makefile and the continuation
scripts (SC23); the dashboard has never shipped on macOS or linux/arm64 (SC19); the administrator texts
(SC28).

**§5 Security.** 5.1: boundary 2 the owner bound (SC7); 3 no new route (SC39); 4 the descriptor's `Exe` and
`Job` are untrusted, and a kind counts only when owner-side state corroborates it (SC1); 5 the job file per OS
(SC45). 5.2 adds rows: the descriptor's `Job` and the kind, the job file, the sibling's build info and owner,
the §4.3 inputs (SC1, SC7, SC16, SC45). 5.3 adds personas: antivirus or owner cleanup (SC25), the archive
writer who flips the kind (SC1), another account that can write the config directory (SC45), and the
owner-owned sibling another account can write as an open condition (SC7, Q20). 5.4: item 1 names the allowed
OS tools and the scheduler's run-now words (SC52); item 5 moved with SE3; item 6 states `run-job`'s
notification exception (SC46).

**§6 Owner-facing text.** Per-kind remedies (SC3); the `Program:` line's labels (SC10); the notes and the
WARN (SC10); `addMailHelp` per slice and OS and the wizard's texts (SC21); README: the zip, move the app
before opening it, the Terminal link (SC14), the wizard's click routes (SC20), the "One program" paragraph
with measured disk figures (SC24) and the console build's dashboard (SC30), Linux needs `zenity` for the
wizard (SC44); release-note lines (SC35, SC37, SC38); the administrator texts (SC28); the phrase rule
(SC47).

**§7 Migration and coexistence.** 7.1 Windows (MSI): release N and the floor defined first (SC37); items for
the install set (SC34), schedules on upgrade day with the per-archive view (SC5), the repair keeping the kind
(SC1), the older instance (SC31), a run in flight and the reboot-skew window (SC34), the single point of loss
(SC25), and the release after the floor (SE4): the skip path, pins, the detection rule, pull-only (SC37,
SC35, SC38, SC41); the disk paragraph (SC24). 7.2 macOS (SC14, SC19, link to the floor). 7.3 Linux (SC41,
SC44, SC3, SC20). 7.4 rollback (SC36, SC13). 7.5 instances and the lock (SC33). 7.6 (new) Windows without the
MSI (SC11).

**§8 Operator walks.** 8.1 without SE3's steps, the console note per wrapper (SC22); 8.2 per archive (SC5);
8.3 the Terminal link (SC14); 8.4 the wizard's refusal on a box without `zenity` (SC44); 8.5 the `Program:`
labels and the console build's pointer (SC10, SC17); 8.6 RMM (SC34, SC35). Friction classification adds the
wizard's click routes (SC20), the ended in-process capture (SC34) and the console build's dashboard (SC30).

**§9 Test strategy.** 9.1 what moves, the SE0 skip encoders and the rows that span slices (SC48). 9.2 the rows
of the allocation table, with the revision 1 map. 9.3 the lab table. 9.4 the prove-fail list.

**§10 Catalog rows proposed.** R33 with SC12's condition and SC52's proof; S59 rewritten (SC41, SC46; SE3's
clause gone, SC39); X17 (SC47); the test-spec rows; the edit list with each edit's slice, PR and rule-2 class
(SC50).

**§11 Build slices.** SE0 after SC53's re-run, with the encoders (SC48) and the id allocation (SC49). SE1 as
a pure move then one commit per behaviour change (SC43), gates owed: the friction review and the adversarial
pass on §4.1 rule 6, §4.3 and §4.5.2. SE2: the `Exe` switch, the kind, the identity predicate, the
breadcrumb; gate owed: the adversarial pass on §4.6. The release rule: SE1 and SE2 ship together (SC42), and
SG3's tag rule (SC29). SE4 after the floor, with its retire-and-keep list and its release-note preconditions
(SC37, SC35). The macOS flip slice, gated (SC19). SE3 moved (SC39).

**§12 Honesty of claims.** The revised table (every SC that changes a claim's label).

**§13 Owner questions.** The questions table, the operator items and the defaults, as allocated below.

**§14 The re-review owed (SC53).** Lenses 3, 4, 6, 7, 8 and 10 on the changed sections, and what each attacks
first: the critic's six re-check items.

**§15 Review record: conditions folded, items still open.** SC1..SC53, where each is folded, and its status.

## Allocation: test-spec rows (provisional; the operator allocates at SE0)

| Rev. 2 | Rev. 1 | Tier | Slice | Proves | Cond. |
|---|---|---|---|---|---|
| MA-610 | MA-580 | U | SE1 | every surface is a verb; the legacy spellings | — |
| MA-611 | MA-581 | U | SE1 | the legacy spellings only in their exact shape under the program's own names; a former name hands its whole argument list to its surface (rule 6) | SC15 |
| MA-612 | MA-582 | S+U | SE1 | no self re-exec: the walk of every process start, and recording fakes on PATH | SC52 |
| MA-613 | MA-583 | S | SE1 | the verb and flag walk and the exit-code set | — |
| MA-614 | MA-584 | S | SE1 (edits SE2, SE4) | build and packaging structure: the builders, the payload, the shortcuts with the wizard's, Build-MSI.ps1's lists, the release zip's members, Detect-RMM.ps1's rule, the RMM stop lists, the single `CloseApplication` target, a release build that is not "(devel)", build-app.sh's `-y` and the `.app` link | SC11, SC20, SC23, SC34, SC35 |
| MA-615 | MA-585 | U | SE2 | `ProgramForSchedule` on each OS, for each name and each reason; translocation refused | SC8 |
| MA-616 | MA-586 | U | SE2 | the composed identity predicate over the composition trace's cases, and the note and WARN texts | SC2, SC10 |
| MA-617 | MA-587 | U | SE2 | a repair keeps the schedule's kind, from each surface | SC1 |
| MA-618 | MA-588 | U | SE2 | when the wizard's and the dashboard's repair is offered; the volatile-directory question first | SC9 |
| MA-619 | MA-589 | U | SE1, only on a Q10 yes | the narrowed "our own program" test | SC31 |
| MA-620 | MA-590 | U | SE1 | the no-argument table, every case, per slice; the console build's pointer | SC16, SC17 |
| MA-621 | MA-591 | S | SE1 | the set of per-user state call sites is unchanged | SC51 |
| MA-622 | MA-592 | S | SE1 | X17: the enumerated sweep and the phrase rule | SC47, SC21, SC28 |
| MA-623 | MA-593 | U | SE2 | rollback: older readers read every file; a pre-SE2 reader's outcomes | SC13 |
| MA-624 | MA-603 | U | SE1 | no dialog from a command-line verb; `run-job` raises only the failure notification | SC46 |
| MA-625 | MA-604 | L, pending | SE1, SE2 | the Windows lab items L-W1..L-W14, in README-RMM §D's round | SC29 |
| MA-626 | MA-605 | L, pending | SE1 | the macOS lab items L-M1..L-M4 (L-M5..L-M7 for the flip slice) | SC19 |
| MA-627 | MA-606 | L, pending | SE1 | the Linux lab items L-L1..L-L4 | — |
| MA-628 | — | U | SE1 | the pair build: Subsystem 3 and 2, equal build info but `-ldflags` | SC23 |
| MA-629 | — | U | SE2 | the `Program:` line's labels and their order | SC10 |
| MA-630 | — | U | SE2 | one rendering of `ProgramForSchedule`'s result on every surface; a translocation refusal writes nothing | SC8 |
| MA-631 | — | U | SE2 | the kind guard at `installOn` for each pair of kinds; a same-kind re-install yields one entry | SC1 |
| MA-632 | — | U | SE2 | a flipped `Job[0]` converts nothing (corroboration) | SC1 |
| MA-633 | — | U | SE2 | the per-kind remedy texts; none for run-job or capture says `schedule … -install` | SC3 |
| MA-634 | — | U | SE2 | the start-failure breadcrumb on every health surface | SC4 |
| MA-635 | — | U | SE2 | the Windows `exe_missing` install-folder branch | SC25 |
| MA-636 | — | U | SE1 | a shown refusal reaches each channel once (process level) | SC26 |
| MA-637 | — | U | SE1 | the handoff names a holder of another version | SC33 |
| MA-638 | — | U | SE1 | an ended older instance is named in the header and in `status` | SC32 |
| MA-639 | — | U | SE1 | the wizard verb refuses when it cannot show a dialog | SC44 |
| MA-640 | — | U | SE1 | `run-job` reads its job file securely; the failure sink follows no link | SC45 |
| MA-641 | — | U | SE2 | install and repair agree (`installSpec` and `repairSpec`) | SC42 |
| MA-642 | — | U | only on Q14 (a) yes | a terminal refusal from the console build invokes no dialog | SC26 |
| MA-643 | — | U | only on Q11 yes | the notification Q11 allows | SC38 |
| MA-644 | — | U | only on Q12 yes | `schedule -repair -out DIR` | SC38 |
| MA-645 | — | U, xfail now | now (OP-1) | the wizard's kind-blind repair and offer, and `installOn`'s cross-kind install (R14, R18) | SC6 |
| MA-646 | — | U, xfail now | now (OP-2) | a capture that refuses at start writes no record (R18) | SC6 |
| MA-647 | — | U, xfail now | now (OP-3) | an absent archive folder is assessed as a new archive (R18) | SC6 |

Revision 1's MA-594..MA-602 (SE3) leave this design for the SE3 design (SC39). Existing rows edited:
MA-79 (L) extends its wrapper check to the GUI-subsystem child (an addition, operator); MA-376's refusal text
and MA-546's wizard step follow the P9 rename (owner, Q18); MA-529's target (owner, Q17); MA-95, MA-123 and
MA-166 only on Q16's yes (owner); X9, S41, MA-271 and MA-278 follow the rename (operator, unless a covering
test pins the name, then owner under Q18); X4 is scoped only on Q12's yes (owner).

## Allocation: lab items

| Id | Question | Decides |
|---|---|---|
| L-W1 | the pair differs only in Subsystem, checksum and build-info bytes | nothing (informational; MA-628 proves Subsystem) |
| L-W2 | an unwrapped `mailarchivew.exe run-job` shows no console; the wrappers running the GUI build wait, capture stderr and pass the exit code; the console build started directly shows a console for the whole run | the release note; whether SE2 records `mailarchivew.exe` inside a wrapper (SC22) |
| L-W3 | MSI upgrade N-1 to N: a schedule of each kind, a field archive both surfaces scheduled, a run in flight through Install-RMM.ps1 and plain `msiexec /qn`, the exit code and Restart Manager, the build on disk until reboot, the copies' file times and what the fallback reads | SE1+SE2 ship for Windows |
| L-W4 | `GetConsoleWindow` and `GetConsoleProcessList` for each start (Explorer under conhost and Windows Terminal, cmd, PowerShell, Windows Terminal, `start`, `schtasks`); Ctrl-C and closing the console of `mailarchive.exe desktop` during an Archive now | the Windows no-argument rule; SC17; SC30's words |
| L-W5 | a `desktop` or `wizard` refusal from the console build under Windows Terminal, PowerShell and a remote session | Q14's urgency; §4.2's residual |
| L-W6 | only on a Q10 yes: after a real MSI upgrade over an instance `CloseApplication` did not end, what the image path reads and whether the test in force ends it | §4.5.2; MA-619 |
| L-W7 | file ownership: a standard account creating a file owned by another SID; the MSI's files' owner; a zip extract's owner; FAT and exFAT | SC7's owner bound |
| L-W8 | `EvalSymlinks` and `SameFile` on case, 8.3, `\\?\` and junction spellings | SC2's directory comparison |
| L-W9 | deletion and Defender quarantine of `mailarchivew.exe`: shortcuts, Last Run Result, `.stderr.log`, `status`, what the installer's Repair restores, re-quarantine | SC25's remedy; Q13's urgency |
| L-W10 | MSI rollback N to N-1 by uninstall then install, an instance in two sessions | P10's label; §7.4 |
| L-W11 | MSI from the pre-N build straight to the SE4 build | SE4's release note; the skip path |
| L-W12 | taskbar and Start pins and owner-made shortcuts after the copies go | the release-note line |
| L-W13 | only on a Q11 yes: the notification is shown at login | the label of the notification |
| L-W14 | `app.Run`'s Outlook probe from an unattended `run-job` of an `Auto` job | whether a gap row is owed before SE1 ships for Windows |
| L-M1 | the `.app` with `CFBundleExecutable=mailarchive`: first and second open, empty argv, the path under `Contents/MacOS` | the macOS rows |
| L-M2 | launchd through the `mailarchive-gui` link; `os.Executable` through the `/usr/local/bin` link and the `.app` link before and after `EvalSymlinks`; a Finder launch has no terminal on any stream; the note and Repair offer | §7.2; SC15's `invokedBase`; SC16's macOS predicate |
| L-M3 | the universal binary of the merged program | the `.app` build |
| L-M4 | app translocation of a quarantined, unmoved `.app` on each supported macOS version | SC8's refusal; §7.2 |
| L-M5 | a second Finder or Dock open of a running `.app`; Dock Quit; logout | the macOS flip slice; Q21 |
| L-M6 | the dashboard inside the `.app`: lifetime, sign-in and token cache, the picker, launchd `desktop -capture` and `run-job` under TCC | the macOS flip slice |
| L-M7 | only for the flip slice: Gatekeeper's right-click Open of an unsigned `.command` | the macOS wizard route after the flip |
| L-L1 | GNOME Files and Dolphin "Run" for an ELF, both directions, amd64 and arm64; the streams, `/dev/tty` and environment for each launch kind; the `.desktop` entry with its trust step | Q4; the Linux click route |
| L-L2 | a box without `zenity`, amd64 and arm64: every command-line verb runs; `wizard` refuses (MA-639); a failing `run-job` under cron returns promptly | SC44; SC46 |
| L-L3 | only on a Q10 yes for Linux: `/proc/<pid>/exe` after the file is replaced by rename | Q10's Linux half |
| L-L4 | cron mail on a stock desktop with no MTA | P5's and §7.3's wording |
| L-C1 | the merged file's size; installed totals for N and N+1; the release-N payload and MSI; WiX de-duplication | SC24's figures; D3 |
| L-C2 | `Notify` from a scheduler-started failing `run-job` on macOS and Windows returns promptly | SC46's Linux-only label |

## Allocation: questions, operator items, defaults

Q1 unchanged (verb names; recommended yes). Q2 re-asked as Q2-mac and Q2-linux. Q3 unchanged (no command-line
job-file install; recommended no). Q4 re-asked. Q5, Q6 and Q7 move to the SE3 design. Q8 becomes OP-7. Q9
re-asked as a floor. New: Q10 (CC22 kill test), Q11 (a notification that is not a failed run), Q12
(`schedule -repair` and X4), Q13 (Windows signing), Q14 (CC20 amendments), Q15 (SG3 lab-pending ship), Q16
(the recorded spelling), Q17 (MA-529's target), Q18 (rename and narrowing edits under rule 2), Q19 (a schedule
inventory after SE4), Q20 (the owner-owned sibling another account can write), Q21 (a long-lived macOS
dashboard). Operator: OP-1..OP-3 (the shipped gaps), OP-4 (SE3 leaves), OP-5 (the Windows zip), OP-6 (ids and
rule-2 classes at SE0), OP-7 (OPEN-X1). Defaults taken as marked: D1 (translocation refuses), D2 (the
desktop's wrapper stays), D3 (plain components), D4 (the fallback outside the program directory keeps
today's answer; MA-466 unchanged). Refer to the desktop design's ruling as "desktop Q14/OR8", never as Q14.
