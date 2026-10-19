#!/usr/bin/env python3
"""Third batch: the regression checker's fourteen items (twins left behind by the earlier fixes)."""
import sys

PAIRS = [
    # R1: SameBuild belongs to MA-628, a per-case skip until SE2
    ("| SE0 | MA-610..MA-614 (MA-614's `SameBuild` case skipped until SE2), MA-620..MA-622, MA-624, MA-628, MA-636..MA-640;",
     "| SE0 | MA-610..MA-614, MA-620..MA-622, MA-624, MA-628 (its `SameBuild` call a per-case skip until SE2), MA-636..MA-640;"),
    ("| SE1 (shipped with it, below) | MA-614's `SameBuild` case, MA-615..MA-618,",
     "| SE1 (shipped with it, below) | MA-628's `SameBuild` call, MA-615..MA-618,"),
    ("| MA-628 | — | U | SE1 | the pair build,", "| MA-628 | — | U | SE1 (its `SameBuild` call: SE2) | the pair build,"),
    ("apply to both lines equally (stated residual, §12) | SC23 |",
     "apply to both lines equally (stated residual, §12). From SE2 the comparison is `SameBuild` (§4.6.0), a per-case skip until then | SC23 |"),
    # R2: MA-631 follows §4.6.5 (b)
    ("`Spec.Validate` refuses an install without evidence; the wizard's `offerSchedule` calls",
     "`installOn` refuses a non-verify install without evidence, and `Spec.Validate` is unchanged; the wizard's `offerSchedule` calls"),
    # R3: MA-633 follows §4.6.3's placed exception
    ("no remedy for a run-job or capture descriptor contains `schedule … -install`; the command line's run-job route adds the no-display fallback naming the job file;",
     "no remedy for a run-job or capture descriptor offers `schedule … -install` as the repair of that schedule, the one placed exception being the command line's run-job no-display fallback, which appears only after its `-remove` step and names the job file;"),
    # R4: P2's remaining SE2 changes
    ("    `backupSpecFor` records `Args` led by the `desktop` verb (§4.6.4) (SC1, SC8, SC42).",
     "    `backupSpecFor` records `Args` led by the `desktop` verb (§4.6.4) (SC1, SC8, SC42);\n  - from SE2, (l) the repair offer is gated by the schedule's kind, and the wizard's repair is not\n    offered, and installs nothing, for a capture descriptor or a run-job descriptor with no job\n    file (§4.6.5 (e), MA-617, MA-618); every repair asks the volatile-directory question first\n    (§4.6.3, MA-618); `status -json` gains the additive reason codes `start_failed` and\n    `exe_version_skew` (§4.6.6, §4.6.2, MA-634, MA-616); and the `Program:` line, the identity\n    notes and the per-kind remedy texts change (§6, MA-629, MA-633, MA-635) (SC1, SC2, SC3, SC4,\n    SC9, SC10, SC25)."),
    # R5: Q18 (a) holds the packaging commit too
    ("until it is made, SE1's text commit waits and SE1 is not released (SC21, SC50).",
     "until it is made, SE1's text and packaging commits wait and SE1 is not released (SC21, SC50)."),
    ("owner (Q18 (a)); interim: SE1's text commit waits, and SE1 is not released until it lands |",
     "owner (Q18 (a)); interim: SE1's text and packaging commits wait, and SE1 is not released until both land |"),
    ("| (a) yes: only the program's name changes; (b) not in SE2: the wizard repairs that kind; (c) ask with the SE3 design | (a) SE1's text commit waits, and SE1 is not released until it lands;",
     "| (a) yes: only the program's name and the shortcuts' leading `desktop` verb change; (b) not in SE2: the wizard repairs that kind; (c) ask with the SE3 design | (a) SE1's text and packaging commits wait, and SE1 is not released until both land;"),
    ("SE1 is also not released until the P9\ntext commit that Q18 (a) holds has landed (Q18's interim).",
     "SE1 is also not released until the P9\ntext commit and the packaging commit that Q18 (a) holds have landed (Q18's interim)."),
    # R6
    ("the release-N paragraph of the four packaging\ntexts (SC28)", "the release-N paragraph of the five administrator\ntexts (SC28)"),
    # R7
    ("a `mailarchivew.exe run-job` in flight through Install-RMM.ps1",
     "a `mailarchivew.exe -job …` run (the recorded spelling, Q16; `run-job …` only on a Q16 yes) in flight through Install-RMM.ps1"),
    # R8: MA-616 proves the prefix test
    ("a linux old build under a former name with matching recorded size and mtime; a Windows skewed sibling (`exe_version_skew`);",
     "a linux old build under a former name with matching recorded size and mtime, both `mailarchive-gui` and the former release asset `mailarchive-gui-linux-amd64` (WARN `exe_moved`, never the fallback); a running file that is a renamed copy beside a recorded `mailarchivew.exe` (WARN, never the fallback); a Windows skewed sibling (`exe_version_skew`);"),
    # R9
    ("marked **OPEN** is the owner's and was raised or re-asked by the review.",
     "marked **OPEN** is the owner's and unanswered; each was raised or re-asked by the review except Q1 and Q3, which are unchanged from revision 1."),
    # R10: the gap rows wait on the operator everywhere
    ("When a skip comes off, the test stays as a permanent guard. The operator accepts the\nthree rows (OP-1..OP-3); until then the gaps stand unrecorded.",
     "When a skip comes off, the test stays as a permanent guard. If the operator accepts the\nthree rows (OP-1..OP-3), they land as above; until then the gaps stand unrecorded."),
    ("They land now, in an\noperator-accepted PR of their own, with xfail encoders (§9.1).",
     "If the operator accepts them\n(OP-1..OP-3), they land now, in a PR of their own, with xfail encoders (§9.1)."),
    ("| MA-645 | — | U, xfail now | now (OP-1) |", "| MA-645 | — | U, xfail | now, on the operator's OP-1 |"),
    ("| MA-646 | — | U, xfail now | now (OP-2) |", "| MA-646 | — | U, xfail | now, on the operator's OP-2 |"),
    ("| MA-647 | — | U, xfail now | now (OP-3) |", "| MA-647 | — | U, xfail | now, on the operator's OP-3 |"),
    ("the open gap OP-1 (MA-645, xfail) (SC1, SC6) |", "the open gap OP-1 (MA-645, xfail, once the operator accepts it) (SC1, SC6) |"),
    ("MA-645's skip comes off (SC6) |", "MA-645's skip comes off, if the operator accepted it (SC6) |"),
    # R11: OP-5's interim and its decline point (SE0, where MA-614 lands)
    ("| yes, taken as marked (a distribution choice within the hard limit): the zip is the design's text, and the operator may decline it at SE1's catalog-first PR | if declined: the README names the MSI only for Windows, the zip cases are not built, and §12's bare-download row applies (SC11) |",
     "| yes | until the operator answers, the zip is the design's text and MA-614's zip cases stay skip-encoded; if the operator declines it at SE0's catalog-first PR, the README names the MSI only for Windows, the zip cases are not built, and §12's bare-download row applies (SC11) |"),
    ("answer to OP-5; if the operator declines it at SE1's catalog-first PR,",
     "answer to OP-5, with MA-614's zip cases skip-encoded until the operator answers; if the operator declines it at SE0's catalog-first PR,"),
    # R12: OP-1..OP-3 ids are allocated on acceptance
    ("and every row, lab item and question the review adds in one pass at SE0 (OP-6, SC49).",
     "and every row, lab item and question the review adds in one pass at SE0 (OP-6, SC49), except the ids of OP-1..OP-3, allocated when the operator accepts them (header)."),
    ("| OP-6 | Allocate this design's ids and the pre-sync design's shifted block in one pass at SE0, with every row, lab item and question this review adds, and classify each §10 edit under rule 2 |",
     "| OP-6 | Allocate this design's ids and the pre-sync design's shifted block in one pass at SE0, with every row, lab item and question this review adds (except the ids of OP-1..OP-3, allocated when the operator accepts them, header), and classify each §10 edit under rule 2 |"),
    ("and every row, lab item and question the review adds, in one pass, with its log entry,",
     "and every row, lab item and question the review adds, in one pass (except the ids of OP-1..OP-3, allocated on acceptance), with its log entry,"),
    # R13: the residuals the list missed
    ("  20. After the floor a schedule still on a former name is reported by pull only, on every OS, until\n      Q11 is answered (§7.1, §7.3, §12) (SC38, SC41).\n",
     "  20. After the floor a schedule still on a former name is reported by pull only, on every OS, until\n      Q11 is answered (§7.1, §7.3, §12) (SC38, SC41).\n  21. A forged `export` kind in the descriptor refuses a cross-kind install on the descriptor alone\n      (§4.6.5, §5.1, §5.3) (SC1).\n  22. A second open of the `.app` while it runs starts nothing (P7, §12) (SC19).\n  23. Once a Linux row exists, an unattended graphical start shows the wizard's first dialog, and a\n      box without `zenity` prints the usage where no one sees it (P7, §12) (SC18).\n"),
    # R14
    ("which is L-W10 (SC36) | SC13, SC33, SC36 |", "which is L-W10 (SC36) | SC13, SC32, SC33, SC36 |"),
]


def main():
    path = sys.argv[1]
    t = open(path, encoding="utf-8").read()
    bad = [(t.count(o), o[:90]) for o, _ in PAIRS if t.count(o) != 1]
    if bad:
        for n, o in bad:
            print(f"NOT APPLIED (found {n}x): {o!r}")
        raise SystemExit("left unchanged")
    for o, n in PAIRS:
        t = t.replace(o, n)
    open(path, "w", encoding="utf-8").write(t)
    print("applied", len(PAIRS))


if __name__ == "__main__":
    main()
