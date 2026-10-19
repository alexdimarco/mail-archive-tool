#!/usr/bin/env python3
"""Assemble docs/review-single-executable-predesign.md from the parts.

usage: python3 -I assemble_review.py PARTS_DIR OUT_FILE
"""
import os
import re
import sys

FINDINGS_INTRO = """## Findings

Severity: **blocker** = the design cannot ship as written; **major** = a wrong, unsafe or silent outcome for
the owner, or a claim that is false as written; **minor** = a real defect with a local fix. Lens numbers as in
the header. "Cond." names the conditions below. Rows are in severity order, then by key; SE-C keys were
raised by the critic. The text of each row is the drafters' condensation of the finder's record, both
skeptics' verdicts and the rescue; the verdicts and severities are the agents' own. Section numbers and ids
in the rows (MA-580..MA-606, L-W1..L-W6, Q1..Q9) are revision 1's; revision 2 renumbers its rows from MA-610
and gives the map in its §9.2.
"""

RESCUE_INTRO = """## Rescue pass (per blocker and major)

One rescue agent per confirmed blocker or major, told to save the design. No rescue refuted its finding
outright; each named a change, and the five PARTIAL ones leave an owner ruling, an operator call or a lab fact
open, which the "Owner decisions" and "Lab priorities" sections carry.
"""

ORDER = ["01-header.md", "02-blocker.md", "FINDINGS", "03-findings-table.md", "05-refuted.md", "RESCUE",
         "06-rescue-table.md", "04-conflicts.md", "07-owner.md", "08-conditions.md", "10-lenses.md",
         "12-mechanical.md"]
FILL = {"{{N}}": "53", "{{RERUN}}": "53", "{{N_OWNER}}": "15", "{{N_OPERATOR}}": "7",
        "{{WRAPPER}}": "22", "{{SE3}}": "40", "{{SE4}}": "37"}
FIXES = [
    ("(the operator's MA-546 edit)", "(subject to the owner's MA-546 ruling, Q18)"),
    ("and the operator edits MA-546's row at SE1 and at SE3.",
     "and MA-546's row is edited at SE1 and, with the SE3 design, at SE3, on the owner's ruling (Q18)."),
    ("adds S41, MA-271, MA-278 and MA-546's name as operator edits, each with the slice that makes it true. It sends MA-546's SE3 change to the owner under the rule-2 limit and says which PR carries each edit.",
     "adds S41, MA-271, MA-278 and MA-546's name, each with the slice that makes it true and its class under rule 2 (the operator's where no covering test changes, otherwise the owner's, as for MA-546, Q18), and says which PR carries each edit."),
    ("the card renders for any recorded descriptor (operator's MA-546 edit)",
     "the card renders for any recorded descriptor once the owner rules on MA-546 (Q18; until then not on a local-only archive)"),
    ("and MA-546's SE1 and SE3 edits are listed as operator edits.",
     "and MA-546's SE1 and SE3 edits are listed for the owner's ruling (Q18)."),
    ("MA-600/MA-601 assert both refusals", "the SE3 design's rows assert both refusals"),
    ("the job-file kind in C1's single kind mechanism", "the job-file kind in SC1's single kind mechanism"),
    ("must consume C1's kind guard", "must consume SC1's kind guard"),
    ("only `ProgramForSchedule` resolves (C2)", "only `ProgramForSchedule` resolves (SC8)"),
    ("success-notification owner decision with C5.", "success-notification owner decision (Q11)."),
    ("is handed to C4/C2.", "is handed to SC22 and SC8."),
    ("depends on C2's predicate", "depends on SC2's predicate"),
    ("stays open in C1/C5 (SE-18, SE-33)", "stays open in SC3 and SC38 (SE-18, SE-33)"),
    ("consume C1's single kind guard", "consume SC1's single kind guard"),
    ("is C1's true blocker.", "is the blocker SC1 resolves."),
    ("derived from the tag (C4).", "derived from the tag (SC23)."),
    ("the rescue raised goes to C5.", "the rescue raised goes to SC36."),
    ("The §4.3 terminal condition it needs is C3's.", "The §4.3 terminal condition it needs is SC16's."),
    ("stays with Q4 in C3.", "stays with Q4 in SC16."),
    ("goes to C6 and the re-asked Q2", "goes to SC40 and the re-asked Q2"),
    ("is handed to C6.", "is handed to SC40."),
    ("(C2's draft of SE-14)", "(one drafter's reading of SE-14's rescue)"),
]

CLUSTER_SC = {
    "C1-1": "SC1", "C1-2": "SC3", "C1-3": "SC4", "C1-4": "SC5", "C1-5": "SC6",
    "C2-1": "SC7", "C2-2": "SC2", "C2-3": "SC8", "C2-4": "SC9", "C2-5": "SC10", "C2-6": "SC11", "C2-7": "SC12",
    "C2-8": "SC13", "C2-9": "SC14",
    "C3-1": "SC15", "C3-2": "SC16", "C3-3": "SC17", "C3-4": "SC18", "C3-5": "SC11", "C3-6": "SC19", "C3-7": "SC20",
    "C3-8": "SC21",
    "C4-1": "SC22", "C4-2": "SC23", "C4-3": "SC24", "C4-4": "SC25", "C4-5": "SC26", "C4-6": "SC27", "C4-7": "SC28",
    "C4-8": "SC29", "C4-9": "SC30",
    "C5-1": "SC31", "C5-2": "SC32", "C5-3": "SC33", "C5-4": "SC34", "C5-5": "SC35", "C5-6": "SC36", "C5-7": "SC37",
    "C5-8": "SC38",
    "C6-1": "SC39", "C6-2": "SC40 R1", "C6-3": "SC40 R2", "C6-4": "SC40 R3", "C6-5": "SC40 R4", "C6-6": "SC40 R5",
    "C6-7": "SC40 R6", "C6-8": "SC40 R7", "C6-9": "SC40 R8",
    "C7-1": "SC41", "C7-2": "SC42", "C7-3": "SC43", "C7-4": "SC44", "C7-5": "SC45", "C7-6": "SC46", "C7-7": "SC47",
    "C7-8": "SC48", "C7-9": "SC49", "C7-10": "SC50", "C7-11": "SC51", "C7-12": "SC52",
}


def main():
    parts, out = sys.argv[1], sys.argv[2]
    chunks = []
    for name in ORDER:
        if name == "FINDINGS":
            chunks.append(FINDINGS_INTRO)
            continue
        if name == "RESCUE":
            chunks.append(RESCUE_INTRO)
            continue
        with open(os.path.join(parts, name), encoding="utf-8") as fh:
            chunks.append(fh.read().rstrip("\n") + "\n")
    text = "\n".join(chunks)
    for k, v in FILL.items():
        text = text.replace(k, v)
    for a, b in FIXES:
        n = text.count(a)
        text = text.replace(a, b)
        print(f"fix applied {n}x: {a[:70]!r}")
    pat = re.compile(r"(?<![\w§.\-])C([1-7])-(\d+)\b")
    text, n = pat.subn(lambda m: CLUSTER_SC[f"C{m.group(1)}-{m.group(2)}"], text)
    print("numbered cluster labels replaced:", n)
    bare = [(m.start(), text[max(0, m.start() - 40):m.end() + 20]) for m in re.finditer(r"(?<![\w§.\-])C[1-7]\b", text)]
    for _, ctx in bare:
        print("BARE CLUSTER LABEL LEFT:", ctx.replace("\n", " "))
    left = re.findall(r"\{\{[A-Z_0-9]+\}\}", text)
    if left:
        raise SystemExit(f"unfilled placeholders: {sorted(set(left))}")
    with open(out, "w", encoding="utf-8") as fh:
        fh.write(text)
    print("wrote", out, len(text), "bytes", text.count("\n"), "lines")


if __name__ == "__main__":
    main()
