#!/usr/bin/env python3
"""Apply the checkers' remaining fixes to revision 2; every old string must occur exactly once."""
import sys

PAIRS = [
    # P4: the L-W2 wrapper condition beside the claim (limits-19)
    ("  the running file, the install says so, and `status` names it (SC7, SC8, SC12). Elsewhere, the\n  running file.",
     "  the running file, the install says so, and `status` names it (SC7, SC8, SC12). For a schedule\n  installed with a `.cmd` wrapper (the desktop's and the command line's, always), it records\n  `mailarchivew.exe` only if L-W2's three wrapper checks pass; otherwise the wrapper records the\n  console build (§4.6.1) (SC22). Elsewhere, the\n  running file."),
    # P9: the swept set equals MA-622's, and the matcher's exclusions are named (consistency-23, limits-6)
    ("  the administrators' texts `packaging/mailarchive-desktop/README-RMM.md`, `DEPLOYMENT.txt`,\n  `README-payload.txt` and `BUNDLE-CONTENTS.txt` (SC28). They name `mailarchive` and its verbs.",
     "  the administrators' texts `packaging/mailarchive-desktop/README-RMM.md`, `DEPLOYMENT.txt`,\n  `README-payload.txt`, `BUNDLE-CONTENTS.txt` and `SOURCE-NOTICE.txt`, `README-macOS.txt` and\n  `packaging/release-notes.md` (SC28, SC47). They name `mailarchive` and its verbs."),
    ("  MA-614; there is no allowlist and no suppression (SC47).",
     "  MA-614. The phrase rule is the only exemption for text; X17's matcher also excludes two\n  non-program tokens (§9.2) as part of X17's proposed definition, which the operator accepts or\n  rejects at SE0 (OP-6); there is no other skip, allowlist or suppression (SC47)."),
    # MA-617: the command line's case (consistency-29)
    ("from the wizard, the Weekly backup card and the command line, a repair rebuilds its own kind",
     "from the wizard and the Weekly backup card, a repair rebuilds its own kind"),
    ("installs nothing on Cancel (SC9) | SC1, SC3, SC9 |",
     "installs nothing on Cancel (SC9); from the command line, `schedule -install` re-installs an export descriptor and, for a run-job or capture descriptor, is refused by the kind guard (MA-631) with nothing written; a `schedule -repair` case is added only with MA-644 on a Q12 yes | SC1, SC3, SC9 |"),
    # MA-622: the matcher's exclusions are part of X17's definition (limits-6, consistency-23)
    ("with two skips only: the directory `packaging/mailarchive-desktop/` and the MSI payload asset name, unless §4.10 renames that asset. Phrase rule: a former name may appear only on a line that also says \"former name\", and never as the thing to run, double-click, download or build; this is the only exemption, with no allowlist and no suppression. The four packaging texts' release-N paragraph",
     "excluding two non-program tokens, the directory `packaging/mailarchive-desktop/` and the MSI payload asset name (unless §4.10 renames that asset), as part of X17's proposed definition, which the operator accepts or rejects at SE0 (OP-6). Phrase rule: a former name may appear only on a line that also says \"former name\", and never as the thing to run, double-click, download or build; there is no other skip, allowlist or suppression. The five administrator texts' release-N paragraph"),
    # X17 (limits-6)
    ("  scripts sit outside the sweep, pinned by MA-376 and MA-614. The phrase rule is the only\n  exemption: no allowlist and no suppression. Enforcement:",
     "  scripts sit outside the sweep, pinned by MA-376 and MA-614. The phrase rule is the only\n  exemption for text; the matcher's two excluded non-program tokens (§9.2) are part of this\n  definition, which the operator accepts or rejects at SE0 (OP-6); there is no other skip,\n  allowlist or suppression. Enforcement:"),
    # SE4 gates (limits-12)
    ("| the release-note preconditions; L-W11 and L-W12 recorded (SC37, SC38) |",
     "| the release-note preconditions; L-W11 and L-W12 recorded (SC37, SC38); **friction review** of the post-floor walk (the RED with the repair, the skip path); **adversarial pass** on the retirement of the former names from `knownName` in §4.6.2 (trust boundary 2) |"),
    # Q1 and Q3 stay OPEN from revision 1 (limits-8)
    ("| Q1 | Verb names:", "| Q1 **OPEN** (unchanged from revision 1) | Verb names:"),
    ("| as recommended | SE1 (names in texts) |", "| as recommended: SE1's texts use the recommended names | nothing under the interim |"),
    ("| Q3 | Should the command line's", "| Q3 **OPEN** (unchanged from revision 1) | Should the command line's"),
    ("Q2-mac, Q2-linux, Q4, Q9, Q10, Q11, Q12, Q13, Q14, Q15,\n  Q16, Q17, Q18, Q19, Q20 and Q21.",
     "Q1 and Q3 (unchanged from revision 1), Q2-mac, Q2-linux, Q4, Q9, Q10, Q11, Q12, Q13, Q14,\n  Q15, Q16, Q17, Q18, Q19, Q20 and Q21."),
    # bare Q2 (consistency-24)
    ("| folded; **OPEN: Q2, Q4** |", "| folded; **OPEN: Q2-mac, Q2-linux, Q4** |"),
    ("| folded; **OPEN: Q2, Q21** |", "| folded; **OPEN: Q2-mac, Q21** |"),
    ("(**OPEN: Q2, Q4**) (SC16, SC20)", "(**OPEN: Q2-mac, Q2-linux, Q4**) (SC16, SC20)"),
    ("(**OPEN: Q2, Q21**) (SC19, SC20)", "(**OPEN: Q2-mac, Q21**) (SC19, SC20)"),
    # §12 P4 row (limits-19)
    ("the claim also rests on no antivirus verdict against the file (SC7, SC8, SC12, SC25) |",
     "the claim also rests on no antivirus verdict against the file. For a schedule installed with a `.cmd` wrapper (the desktop's and the command line's, always), only if L-W2's three wrapper checks pass; otherwise the wrapper records the console build (§4.6.1) (SC7, SC8, SC12, SC22, SC25) |"),
    # §4.4: where a Q16 yes lands (consistency-19)
    ("  `-job` assertions of `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` change (P2\n  exception (e), the owner's under rule 2). SE2 switches",
     "  `-job` assertions of `TestRepairSpec` and `TestRepairSpecIgnoresHostileJob` change (P2\n  exception (e), the owner's under rule 2); on that yes the change lands in SE2 with `installSpec`\n  and MA-641's `Args` case. SE2 switches"),
    # §9.1 spanning rows (consistency-19)
    ("In this design those rows\nare MA-628 (its `SameBuild` call waits for SE2, §4.6.0) and MA-641 (only if an owner yes to Q16\nputs its `Args` case in SE1).",
     "In this design that row\nis MA-628 (its `SameBuild` call waits for SE2, §4.6.0); on a Q16 yes the recorded-spelling change\nlands in SE2 with MA-641's `Args` case, so MA-641 does not span."),
    # repair offer, precisely (consistency-16, limits-9)
    ("is offered only when exactly one of run-job and capture is corroborated (MA-632) (SC1)",
     "is offered only when the recorded kind is run-job or capture, is corroborated, and the other of the two is not (§4.6.5 (e), MA-632) (SC1)"),
    ("A repair is offered only when exactly one of run-job and capture is corroborated (§4.6.5); on a field archive both surfaces scheduled, Repair is refused naming the surface that can install each kind, and one explicit Install/update follows.",
     "A repair is offered only when the recorded kind is run-job or capture, is corroborated, and the other of the two is not (§4.6.5 (e)); on a field archive both surfaces scheduled, Repair is refused naming the surface that can install each kind, and the owner re-installs the recorded kind from its own surface (the card's Install/update for a capture, the wizard's schedule offer for a run-job); an install of the other kind is refused by §4.6.5's guard until the recorded one is removed."),
    ("Repair is refused naming the surfaces, and the owner makes one explicit Install/update (SC1).",
     "Repair is refused naming the surfaces, and the owner re-installs the recorded kind from its own surface (the card's Install/update for a capture, the wizard's schedule offer for a run-job) (SC1)."),
    # L-W2 (consistency-18, limits-7)
    ("an unwrapped `mailarchivew.exe run-job …`",
     "an unwrapped `mailarchivew.exe -job …` (the recorded spelling, Q16; `run-job …` only on a Q16 yes)"),
    ("R33's wording is narrowed (§10)",
     "R33's wording is narrowed, at SE0 if L-W2 has run by then, otherwise by the owner under rule 2 (§10)"),
    # §7.1 item 1 (consistency-21)
    ("two more plain components, same source file; D3",
     "two more plain components, each a `File` of its own payload source with the same bytes as `mailarchivew.exe`; D3"),
    # SE1 row (consistency-23)
    ("the four packaging texts", "the five administrator texts"),
    # notify sink (consistency-25, limits-18)
    ("to `Notify`, MA-124)", "to the package-level `notify` sink, MA-124)"),
    ("| L-C2 | `Notify` from a scheduler-started", "| L-C2 | the failure notification (the `notify` sink) from a scheduler-started"),
    # Q20 row number (consistency-27)
    ("accept it, stated on §5.3 row 1:", "accept it, stated on §5.3 row 2 (an account that can write a sibling the owner owns):"),
    # S59 proven-by range (consistency-28)
    ("MA-610..MA-641; S59's Linux clause",
     "MA-610..MA-618, MA-620..MA-624, MA-628..MA-641 (MA-619 only on a Q10 yes; the lab rows MA-625..MA-627 pending); S59's Linux clause"),
    # SE2 gates and Q20 (limits-16)
    ("Q20's open condition (its sign-off waits as §13 states)",
     "Q20's open condition (the pass reports on it and cannot accept it; the condition stays open beside §5.1 and §5.3 until the owner answers, and SE2 is not held, §13)"),
    # MA-623 rule numbers (consistency-11)
    ("its rules 3, 6 and 7", "its rules 1, 6 and 7"),
    # MA-625 slice (consistency-20)
    ("| MA-625 | MA-604 | L, pending | SE1, SE2 |", "| MA-625 | MA-604 | L, pending | SE1, SE2 (L-W11, L-W12: SE4) |"),
    # SE0 row (limits-13)
    ("and accepts MA-616's louder WARN (SC2)", "and decides whether to accept MA-616's louder WARN (SC2)"),
    # SE1 commit 9 (consistency-22)
    ("9. the pair in the builders and packaging (MA-614, MA-628);",
     "9. the pair in the builders and packaging (MA-614, MA-628), whose shortcut `Arguments` change waits on Q18 (a) with item 10 (MA-376, §4.10);"),
    # §10 MA-386 decider and the MA-123/MA-166 edit (consistency-26, limits-14, limits-10)
    ("| MA-386 | none: its moved test is strengthened (§9.1) | SE1 | — |",
     "| MA-386 | none: its moved test is strengthened (§9.1) | SE1 | operator (an addition, classified at SE0, OP-6) |\n| MA-123, MA-166 | \"the CURRENT exe\" / \"the current exe\" becomes \"the program `ProgramForSchedule()` chooses (§4.6.1), never the descriptor's\" | SE2, with the `Exe` switch | the operator classifies it at SE0 (OP-6); if it narrows the guarantee or changes what `TestRepairSpec` asserts, the owner (rule 2) |"),
    # §13 lead (consistency-4)
    ("Under the interims no slice of SE0..SE2 is held; the Blocks column names what waits (SG2).",
     "Under the interims no slice of SE0..SE2 is held from being built; the Blocks column names what waits (SG2). One question holds a release: Q18 (a), whose interim holds SE1's text and packaging commits, and so release N."),
]

ANY = [
    # MA-630's leftover SE3 control (consistency-10), with or without "the dashboard's"
    (("the dashboard's Keep current and Repair results", "the Weekly backup card's Install/update and Repair results"),
     ("Keep current and Repair results", "the Weekly backup card's Install/update and Repair results")),
]


def main():
    path = sys.argv[1]
    with open(path, encoding="utf-8") as fh:
        t = fh.read()
    bad = []
    for old, new in PAIRS:
        n = t.count(old)
        if n != 1:
            bad.append((n, old[:90]))
            continue
        t = t.replace(old, new)
    for alts in ANY:
        for old, new in alts:
            if t.count(old) == 1:
                t = t.replace(old, new)
                break
        else:
            bad.append((0, alts[0][0][:90]))
    if bad:
        for n, o in bad:
            print(f"NOT APPLIED (found {n}x): {o!r}")
        raise SystemExit("some fixes did not match exactly once; file left unchanged")
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(t)
    print("applied", len(PAIRS) + len(ANY), "fixes")


if __name__ == "__main__":
    main()
