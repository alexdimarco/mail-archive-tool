# Pre-code design review — one program: `mailarchive` as the single executable

**Design reviewed:** `docs/design-single-executable.md`, revision 1 (2026-10-15), sha256
`ee4b8fa41c245734a02fa49e19d3cb82bd0b9d0947671d35ee162f445891df18`, an untracked file on branch
`fix/review-2026-10` at repository head `b1a16bc`. The conditions below are folded into revision 2 of
the same file.

**Procedure:** the 10-lens battery of `assurance-kit/process/design-review.md`, run 2026-10-15 to
2026-10-16 as one workflow (run `wf_1bf297de-89d`), read-only against the design, the designs it touches
(`design-mailarchive-desktop.md`, `design-local-confidentiality.md`, `design-schedule-v2.md`,
`design-extraction-workspace.md`, `design-thunderbird-presync.md`), the catalog, the UX contract, the
rulings log and the code it cites. Every pass was a separate agent: ten lens finders (one lens each), one
merge agent that removed duplicates across lenses, two skeptics per finding (refutation by default; a
refutation counted only with a verbatim quote of at least 20 characters from the design or the code, and a
split vote stays confirmed and is marked *contested*), one rescue agent per confirmed blocker or major
(told to save the design: refute the finding or name the modest change that dissolves it), and one
completeness critic that also acted as the holistic rescue. The critic's six new findings went through the
same skeptic and rescue passes.

**Deviations from the procedure, stated plainly.**
1. The procedure gives the rescue to one agent for all blockers. Here every blocker and major had its own
   rescue agent, so no rescue saw another's change; the critic's cross-finding pass is the only place the
   changes met, and its list of conflicts is resolved in "Conflicts between the rescues" below.
2. The run was paused and relaunched four times (the owner's machine was suspended; the agents' command
   shapes were changed so the run could go on without approval prompts; the model was switched when the
   Fable allowance ran out). Every result that had finished was replayed from the run's journal and never
   re-run; a result was replayed only when its finding's title matched the merged list. 58 skeptic attempts
   failed in the earlier launches; each was re-run and only the re-run counts.
3. The synthesis owner is the session that wrote revision 1. It judged nothing: every verdict, severity and
   rescue outcome below is an agent's. Seven drafting agents condensed the record per mechanism cluster; the
   owner composed the conditions from their drafts and from the critic. A second reviewer reading revision 2
   would tighten this (see "Residual risk").

**Agents and models.** Finders, merge, 106 of the 164 skeptic verdicts on the merged list and the
blocker's rescue: Claude Fable 5.1. The other 58 skeptic verdicts, the 46 remaining rescues, the critic,
the 12 skeptic verdicts and 4 rescues of the critic's round: Claude Opus 5.5. 118 agents ran in the final
launch with no error.

**Lenses run:** all ten. 1 purpose / threat-model fit · 2 focus / proportionality · 3 durability ·
4 usability · 5 integration / seams · 6 security / adversarial · 7 failure domains / recoverability ·
8 migration / coexistence · 9 dependency and operational cost · 10 honesty of claims. Raw findings per
lens: 16, 9, 12, 20, 18, 11, 13, 15, 7, 14 (135 in all), merged to 82.

**Findings: 88 judged (82 merged plus 6 from the critic) · 83 confirmed · 5 refuted · 1 blocker ·
50 major · 32 minor.** Two confirmed minors are contested. The rescue handled the blocker and all
50 majors: 46 DISSOLVED by a named change, 5 PARTIAL (SE-04, SE-21, SE-28, SE-33, SE-34: each mechanism
is fixed on paper and each leaves an owner ruling, an operator call or a lab fact open), 0 SURVIVES.

**Verdict: GO WITH CONDITIONS, bounded.** Conditions **SC1..SC{{N}}** are folded into revision 2 (§15 of the
design maps each to its section). The bound comes from the critic: the conditions rewrite the same four
mechanisms several times (the schedule's kind, the program's identity, the no-argument rule, the Windows
kill scope), and revision 2 must compose them once. Two consequences are part of the verdict:
- **If revision 2 cannot state one schedule-kind mechanism (SC1) and one identity predicate (SC2), the
  verdict becomes NO_GO on lenses 3, 4, 7 and 8.**
- Revision 2 changes the design's digest in §4.3, §4.6, §4.8 and §7, so lenses 3, 4, 6, 7, 8 and 10 are
  stale for those sections. They are re-run on revision 2 before SE0's catalog-first PR (SC{{RERUN}}).

**Scope of the approval:** slices SE0, SE1, SE2 and SE4, after that re-run. **SE3 (the dashboard adds local
mail) is not approved:** it leaves this design for its own design and pre-code review (SE-21; the
operator's lens-2 call, recommended yes). Revision 2 keeps §4.8 as an informative stub that carries the
SE3 findings as that design's requirements. {{N_OWNER}} questions need the owner (and Q1 and Q3 stay open
from revision 1) and {{N_OPERATOR}} items need the operator; each has a safe interim that weakens no
invariant or gate. One of the operator items is a
defect in the shipped wizard at `b1a16bc`, independent of this design (OP-1 below).
