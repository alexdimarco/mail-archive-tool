# SC53 — human second-reviewer checklist

**Why this exists.** The single-executable pre-code review was run by the same session that wrote the design
and composed the review's conditions, so in part it graded its own work. The review's *Residual risk* section
(`docs/review-single-executable-predesign.md`) asks for an **independent human** to read the six places where
that self-composition could hide a mistake, before SE0 (the first catalog-first PR). This is the last open
piece of condition **SC53**.

**What's already been done.** A fresh-agent pass ran all six items as part of the SC53 re-run
(`docs/continuation/single-exe-review/sc53-rerun-2026-10-16.md`). Five held; item **4 (Windows kill scope)**
was found wrong and fixed in revision 3. So the content is already checked once — your read is the
*independent sign-off* the process wants, not a fresh full review. Budget ~30–60 minutes.

**How to use this.** Open `docs/design-single-executable.md` (revision 3) and work through the six items
below. For each, read the named section(s) and confirm the "Check" holds. Record the outcome at the bottom.
If anything is off, note it instead of signing off — a finding here is exactly what this step is for.

---

## The six items

**1. The composed identity predicate — traced case by case.** Read **§4.6.2** (and MA-616 in §9.2 for the
case list). *Check:* each case in the composition trace reaches the right verdict and none wrongly falls
through to the size+mtime fallback — the MSI N-1 copies (new size+mtime), the macOS link, the CLI-tarball
copy, a Linux former name, a forged descriptor in another directory, odd path spellings (case / 8.3 / `\\?\` /
symlinked parent), and a translocated app. *(Fresh-agent pass: held.)*

**2. The one schedule-kind mechanism.** Read **§4.6.5** (a)–(g). *Check:* a schedule's kind is one taxonomy
(verify / run-job / capture / export), corroborated by owner-side state (not the descriptor's forgeable
`Job`), guarded once in `refuseCollision` before any wrapper/job-file/scheduler write, repaired to the
corroborated kind only, and named in those same words where a surface shows the kind to the owner. *(Fresh
pass: held; this is the mechanism the whole verdict hinges on.)*

**3. The composed no-argument launch table.** Read **§4.3** (with **§4.1 rule 6** for former names). *Check:*
every cell of `defaultVerb(goos, consoleAttached, consoleIsOwn, inBundle)` is defined and internally
consistent, and matches the claims in §4.2 and §12. *(Fresh pass: held.)*

**4. The Windows kill scope and the files-in-use path.** Read **§4.5.2, §4.10, §7.1 (items 1 and 5).**
*Check:* an upgrade ends exactly the pre-upgrade instance — plus, stated honestly, an un-repaired former-name
`mailarchive-desktop.exe -capture` run — and **never** generalizes to killing all old-version processes; the
files-in-use wording is consistent between the MSI (`util:CloseApplication`) and the RMM scripts. *(Fresh
pass: FOUND WRONG — the scope was under-stated; fixed in revision 3. **Give this one the closest read.**)*

**5. The id and question allocation.** Read **§9.2, §9.3, §10, §13** (SC49). *Check:* the header and body
MA / L / Q / R / S / X ranges agree, no id is used twice, and nothing collides with the Thunderbird pre-sync
block. *(Fresh pass: held.)*

**6. The owner-ruling surface.** Read **§13 and §15.** *Check:* no gate marked "none needed" actually hides a
*locked amendment* — a change to catalog or contract (X-series) text, or to what an existing covering test or
golden asserts — that should ESCALATE to you under the rulings log's rule 2. *(Fresh pass: held.)*

---

## Sign-off

- **To approve:** reply (or note here) "SC53 second review: items 1–6 confirmed" — that discharges the last
  piece of SC53, and SE0 can proceed once the operator's OP-6 id allocation is done.
- **To flag:** name the item and what's wrong; it folds like any other review finding before SE0.

Supporting material, all on branch `fix/review-2026-10`:
- the design: `docs/design-single-executable.md` (revision 3);
- the review + its *Residual risk*: `docs/review-single-executable-predesign.md`;
- what the fresh-agent pass found per item: `docs/continuation/single-exe-review/sc53-rerun-2026-10-16.md`.
