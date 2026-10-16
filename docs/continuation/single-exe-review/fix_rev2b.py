#!/usr/bin/env python3
"""Second batch: the exemption wording, the residual definition and §15's residual list."""
import sys

RESIDUALS = """- **Residuals: open conditions, not accepted** (rulings log rule 3; each stays open until the owner
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
  7. An owner-owned sibling that another account can write (Q20), and a build with no VCS stamp,
     which compares on the other fields only (§4.6.0, §5.1, §5.3) (SC7).
  8. On a headless Linux box the only repair of a wizard schedule is remove-and-reschedule (Q12)
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
  20. After the floor a schedule still on a former name is reported by pull only, on every OS, until
      Q11 is answered (§7.1, §7.3, §12) (SC38, SC41).
"""

PAIRS = [
    ("There is no allowlist and no suppression. (SC28, SC47)",
     "There is no other skip, allowlist or suppression beyond X17's two excluded non-program tokens (§9.2), which the operator accepts or rejects with X17's definition at SE0 (OP-6). (SC28, SC47)"),
    ("The phrase rule is the only exemption: no allowlist, no suppression (SC47, SC28).",
     "The phrase rule is the only exemption for text; X17's two excluded non-program tokens (§9.2) are part of its proposed definition, which the operator accepts or rejects at SE0 (OP-6) (SC47, SC28)."),
    ("behaves, and is not assumed. **residual** (also \"Type II, stated\") = a gap this design names and\ndoes not close; under the rulings log's rule 3 it is an open condition, not accepted, until the\nowner accepts it, and it is listed in §15.",
     "behaves, and is not assumed. **residual** = a gap this design names and does not close; under\nthe rulings log's rule 3 it is an open condition, not accepted, until the owner accepts it, and §15\nlists each one. A friction entry marked **Type II** is a design choice whose cost is stated\n(assurance-kit `process/friction-review.md`); where it also leaves a gap, §15 lists it as a\nresidual."),
    ("- **Defaults taken as marked** (delegated-recommended; the owner may overrule any): D1..D4.\n",
     "- **Defaults taken as marked** (delegated-recommended; the owner may overrule any): D1..D4.\n" + RESIDUALS),
]


def main():
    path = sys.argv[1]
    t = open(path, encoding="utf-8").read()
    bad = [(t.count(o), o[:80]) for o, _ in PAIRS if t.count(o) != 1]
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
