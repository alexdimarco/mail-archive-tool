# Continuation note, 2026-10-16 — the single-executable design and its pre-code review

Written so a fresh session (or the owner) can pick this work up cold. Read `CLAUDE.md`, then
`docs/review-single-executable-predesign.md` (the SG1 10-lens pre-code review) and
`docs/design-single-executable.md` (revision 2). Everything below is true at the commit that adds this note
on `fix/review-2026-10`. Nothing in the design is built; no code, catalog, baseline, `assureblock/`, rulings
log, `CLAUDE.md` or permission setting was touched by the review or by revision 2.

## 1. What is filed

- `docs/design-single-executable.md` — **revision 2** (sha256
  `da56956cce822e71eac27bb7672a542ceb76ea01908b076d28675d12dca01a65`), which folds the review's 53
  conditions SC1..SC53. The commit two before this one holds **revision 1** (sha256
  `ee4b8fa41c245734a02fa49e19d3cb82bd0b9d0947671d35ee162f445891df18`), the exact text the review judged,
  committed on its own so the review's recorded digest resolves to a git blob
  (`git log --oneline -- docs/design-single-executable.md` shows both).
- `docs/review-single-executable-predesign.md` — the review (sha256
  `a4c4e150024012fe21a7e2cce5478fd3ed860e71e3a156d1de98892293e79805`). Verdict **GO WITH CONDITIONS,
  bounded**. 88 findings judged, 83 confirmed (1 blocker, 50 major, 32 minor), 5 refuted. Run
  `wf_1bf297de-89d`, 118 agents in the final launch, 0 errors.

## 2. The blocker and the three shipped-code defects

- **Blocker:** a schedule carries no kind, so a Repair from one surface (the wizard) can convert another
  surface's (MailArchive Desktop's) working schedule into one that fails every run while status stays GREEN.
  Fixed on paper by SC1 (one kind mechanism in `refuseCollision`) on top of SC2 (one identity predicate).
  If revision 2 cannot state SC1 and SC2 as one mechanism each, the verdict is NO_GO on lenses 3, 4, 7, 8.
- **OP-1..OP-3** are defects that exist in the shipped code and were confirmed present in the released tag
  `v0.7.1` as well as at branch head `b1a16bc` (2026-10-16):
  - OP-1 `cmd/mailarchive-gui/main.go` `repairable`/`repairSpec` (`Args: ["-job", jobPath]`), both surfaces
    name the schedule `schedule.DefaultNameFor(out)`; the wizard's Repair rebuilds a `-job` schedule over a
    desktop `-capture` schedule. (R14, R18.)
  - OP-2 `internal/desktop/capture.go` `HeadlessCapture` returns on "not configured" / "no archive location
    set" before `app.RunGraph`, the only call that records a run, so `status` stays GREEN until twice the
    period late. (R18.)
  - OP-3 `internal/health/health.go` `Gather` stats the folder only for the write probe; an absent archive
    folder (unplugged drive) reads as a new empty archive: `no_schedule` WARN inviting a second install.
    (R18.)
  Under CLAUDE.md each is an operator-accepted catalog row plus a `t.Skip`/xfail encoder citing its
  invariant, landing now (placeholders MA-645, MA-646, MA-647). This session wrote neither; the operator
  accepts the rows (OP-1..OP-3).

## 3. What is owed before SE0 (SC53)

Re-run lenses **3, 4, 6, 7, 8, 10** on the sections revision 2 changed (§4.3, §4.6, §4.8, §7, and wherever
SC1..SC52 rewrote text), and a **second reviewer** on the critic's six re-check items (listed in the
review's "Residual risk" section: the composed identity predicate case by case, the one kind mechanism, the
composed no-argument table, the Windows kill scope and files-in-use path, the id/question allocation, and
the owner-ruling surface). SC53 stays OPEN until the re-run is filed with the review. This session wrote
revision 2 and does not self-certify the re-run.

## 4. Open decisions (each has a recommended default and a safe interim in the review)

- **Owner:** Q1, Q2 (mac/linux), Q3, Q4, Q9..Q21 (see the review's owner table and §13 of the design).
  Most consequential: Q10 (CC22 amendment), Q13 (Windows signing — needs certificate lead time), Q18(a)
  (holds SE1's text + packaging commits and so release N), Q9 (former-name copy floor).
- **Operator:** OP-1..OP-3 (the rows above), OP-4 (SE3 becomes its own design — recommended yes), OP-5 (the
  Windows release asset is one zip of install names), OP-6 (allocate all ids in one pass at SE0), OP-7
  (OPEN-X1 per-user state root — this design needs no answer), plus classifying each §10 edit under rule 2.

## 5. Build order (design §11)

The open-gap encoders now (OP-1..OP-3); SE0 (catalog-first PR) after the SC53 re-run; SE1 then SE2, released
together as release N (no `v*` tag from a line containing SE1 until SE2 and the Windows lab round are in,
SG3); the SE3 design's slices; the macOS flip slice after SE3; SE4 at the first minor tag past the
former-name floor. Recommendation on record (not an owner ruling): fix OP-1..OP-3 as a v0.7.2 patch from
`main`, and build SE1+SE2 on a branch of their own so the release rule does not freeze other work.

## 6. How to rebuild the deliverables

The scripts beside this note are copies of the session scratchpad (which is temporary):

- `assemble_review.py PARTS_DIR OUT_FILE` rebuilds the review from `review-parts/` (the 10 part files:
  header, blocker, findings table, conflicts, refuted, rescue table, owner, conditions, lenses, mechanical).
  Run `python3 -I assemble_review.py review-parts docs/review-single-executable-predesign.md`.
- `rev2-spec.md` is the outline and id/label allocation revision 2 was drafted from (MA-610..MA-647, the
  rev-1→rev-2 map, labs L-*, Q1..Q21, OP-1..OP-7, D1..D4).
- `assemble_rev2.py`, `fix_rev2.py`, `fix_rev2b.py`, `fix_rev2c.py` are the assembly and the three
  checker-fix passes applied to revision 2 (the three Opus checkers' 64 issues and the regression checker's
  14 issues).

The full agent journal (`journal.jsonl`, ~1 MB, the per-finding evidence behind every row) and the
per-agent transcripts stay in session scratch and are not committed here because of their size; ask if the
evidence trail should be added to the repo before the scratch is lost.

## 7. Tooling facts (unchanged from the 2026-10-13 note unless noted)

- Workflow agents must start their prompt with the absolute-path rule (Read tool, or one plain read-only
  Bash command with absolute paths; never `cd`, `&&`, `||`, `;`, pipes, `$( )`, redirection, unquoted
  globs, relative paths or `echo`) or they trip the project shell-shape guard hook and prompt for approval.
- The review ran its finders, merge and most skeptics on Claude Fable 5.1 and its later skeptics, rescues
  and critic on Claude Opus 5.5; Fable's allowance is now exhausted, so the SC53 re-run and any further
  agents run on Opus.
