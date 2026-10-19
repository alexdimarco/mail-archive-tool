# OP-6 — SE0 id-allocation worksheet (operator sign-off)

**What this is.** OP-6 is the operator's one-pass allocation at SE0 (SC49): fix the final catalog ids for the
single-executable design's block, reserve the Thunderbird pre-sync design's shifted block, and account for
every row, lab item and question the review added — then classify each §10 catalog edit under rule 2 (SC50).
**No catalog-first PR lands before this is signed off.** The ids below are the design's proposal (§9.2, §9.3,
§10); this sheet consolidates them so you can accept or adjust in one pass.

**Catalog of record for SE0.** SE0's catalog-first PR lands on a branch off **`main`**. `main`'s catalog
today holds rows up to **MA-549** plus **MA-645/646/647** (shipped in v0.7.2). So MA-550..MA-644 are free on
`main`. The pre-sync block is on `fix/review-2026-10`, not yet on `main` (its slices are unbuilt), so its ids
are *reserved* here, forward-looking. **Prior high-water marks any design has claimed: R32, S58, X16** — so
R33, S59, X17 are the next free (header).

---

## 1. MA rows — this design's block (MA-610..MA-647)

Rev-1 → this allocation: MA-580..593 → **MA-610..623** in order; MA-603/604/605/606 → **MA-624/625/626/627**.
Rev-1's MA-594..602 (the SE3 "add mail" rows) leave for the SE3 design. No rescue-named id (MA-607, MA-608, …)
stands; all are re-allocated here.

| Range | Tier / slice | What | Allocate? |
|---|---|---|---|
| **MA-610..MA-614** | S/U, SE1 | verbs & dispatch (610,613), legacy-spelling rule 6 (611), no self re-exec (612), build/packaging incl. the tag-bound `ProductVersion` (614) | ☐ |
| **MA-615..MA-618** | U, SE2 | `ProgramForSchedule(wrapped bool)` (615), the identity predicate (616), kind-keeping repair (617), when repair is offered (618) | ☐ |
| **MA-619** | U, SE1 | the narrowed CC22 "our own program" test — **Q10 answered yes (13.44)**: lands for Windows; its Linux half waits on L-L3 | ☐ |
| **MA-620..MA-624** | U/S, SE1 | no-argument rule (620), no-new-state pin (621), X17 sweep (622), rollback reads every file (623), no-dialog-from-CLI (624) | ☐ |
| **MA-625/626/627** | L, pending, SE1 | the Windows / macOS / Linux lab-item rows (cite §9.3) | ☐ |
| **MA-628..MA-641** | U, SE1/SE2 | pair build (628), status `Program:`+`scheduled:` line (629), `ProgramNote` (630), the kind guard (631), flip-converts-nothing (632), per-kind remedies (633), start-failure breadcrumb (634), `exe_missing` install-folder branch (635), shown-refusal (636), handoff (637), ended-instance (638), wizard dialog refusal (639), secure `run-job` read (640), install==repair (641) | ☐ |
| **MA-642** | U, SE1 | terminal refusal invokes no dialog — **Q14 answered yes (13.44)**: lands | ☐ |
| **MA-643** | U, SE1 | migration notification — **Q11 answered (b) (13.44)**: lands (once per affected schedule across the compatibility period; label conditional on L-W13) | ☐ |
| **MA-644** | U, before SE4 | `schedule -repair -out DIR` — **Q12 answered yes (13.44)**: lands before SE4; refuses without mutation | ☐ |
| **MA-645 / MA-646 / MA-647** | U, xfail | OP-1 / OP-2 / OP-3 — **already allocated and live in v0.7.2** (on `main`); no re-allocation | ✓ done |

A reserved (no-id) row — a `Dialogs` interface fake with a golden — is allocated only when a later slice
proposes it (§9.2).

## 2. New R / S / X ids

| Id | What | Allocate? |
|---|---|---|
| **R33** | One program (every function is a verb of one executable) — §10 | ☐ |
| **S59** | the structural claim the design adds at §10 (confirm the exact row text in §10) | ☐ |
| **X17** | the UX-contract sweep clause (the enumerated former-name phrase rule), MA-622 — §10 | ☐ |

## 3. Lab-item ids (§9.3)

| Group | Ids | Allocate? |
|---|---|---|
| Windows | L-W1..L-W14 (L-W6 only on Q10-yes; L-W13 only on Q11-yes — both now yes) | ☐ |
| macOS | L-M1..L-M7 (L-M5..L-M7 gate only the macOS flip slice) | ☐ |
| Linux | L-L1..L-L4 (L-L3 only on Q10-yes for Linux) | ☐ |
| Cost/measure | L-C1, L-C2 | ☐ |

## 4. Questions

Q1..Q21 are **all answered** in ruling 13.44 (SC53 by 13.45) — recorded in §13's owner-answers block. No Q id
is open; they need no allocation beyond being referenced by the rows above.

## 5. Pre-sync design's shifted block — reserved

Reserve **MA-549..MA-579** (31 ids) for the Thunderbird pre-sync design, and **R30..R32, S53..S58, X16** as it
claimed them. The single-executable block starts at **MA-610**, leaving **MA-580..MA-609** as the gap that
absorbs any pre-sync growth and the review's additions. When both lines reach `main`, neither block collides.

## 6. §10 rule-2 classification (SC50)

Each stale catalog-row edit the slices make is in §10's "Catalog edits the slices make" table with its
owner/operator column. The **owner-gated (rule 2)** edits — those that change what an existing covering test or
golden asserts — are **now answered in 13.44**, so the operator's pass is to confirm, not to wait:

- MA-376 (refusal text + shortcut `Arguments`) and MA-546 (the wizard step): **Q18(a) yes** → the renames land in SE1.
- MA-546 (local-only Weekly backup card): **Q18(b) yes** → the card's Repair/Remove render for local-only archives; `TestLocalOriginArchiveInDesktop` is edited to match.
- MA-546 (SE3 add-mail page): **Q18(c)** → deferred to the SE3 design.
- MA-529 (govulncheck target → `./cmd/mailarchive`): **Q17 yes**.
- MA-123 / MA-166 ("the current exe" → `ProgramForSchedule()`): operator classifies; the owner's rule-2 call applies only if it narrows a guarantee (SE2).
- X17's two excluded non-program tokens and the matcher: **operator accepts the X17 definition at SE0** (MA-622).

The remaining §10 additions (R33, S59, the new MA rows, X17's sweep) are **rule-1 additions the operator
accepts**. MA-616's louder `exe_moved` WARN for a fallback-only former-name match is the operator's SE0 call
(SC2).

## 7. Verify before the PR (SC49)

- ☐ No MA id in MA-610..647 (or the reserved 549..579) is used twice or already on `main` (main holds ≤549 + 645/646/647).
- ☐ R33, S59, X17 are free (R32/S58/X16 are the prior high-water marks).
- ☐ Every L-/Q-/MA-id a row cites resolves (the covers-map meta-test will enforce this once the rows land).
- ☐ Each §10 edit is tagged rule-1 (operator-accepted) or rule-2 (owner — all answered in 13.44).

## 8. Sign-off

Reply **"OP-6: allocation accepted"** (noting any id you want changed) and SE0's catalog-first PR can be built:
the rows above added to `docs/scenario-catalog.md` on a branch off `main`, each U/S row with a `t.Skip`/xfail
encoder citing its id and slice, `go test ./...` green with the block skip-encoded. SE1 → SE2 follow.
