# Owner rulings — 2026-10-09

**Status: the final log for the owner, checked.** It was drafted on branch `wip/owner-rulings`
(base `176c4b2`) from two kinds of source:
- the five design documents and their 10-lens pre-code reviews at that commit:
  `docs/design-{extraction-workspace,fault-isolation,mime-fidelity,recapture-integrity,local-confidentiality}.md`
  and the matching `docs/review-*-predesign.md`;
- the reports of four fix branches: `wip/sanitizer-r2-export` (`deb2e9e`),
  `wip/sanitizer-r2-reader` (`7b22df7`), `wip/sanitizer-r2-index` (`1cbf466`) and
  `wip/g8-scheduling` (`4f82d6e`).

A skeptic then checked the draft against the designs, the reviews and the catalog. Every
correction it raised held when its evidence was re-read, so each is applied, and the ruling it
changed says **corrected after check**, with the reason. Its seven smaller notes were applied
the same way. No correction was rejected (§12).

How to read it:
- Nothing here is built. No catalog row or test changes with this file.
- The five designs fold these rulings in the same commit. Each folded change is marked
  `[ruling <id>]` in the design, and each open question there is marked RULED or ESCALATED.
- Every delegated ruling stands until the owner overturns it.
- Every **ESCALATE** waits for the owner. Its safe interim is stated, and nothing that depends
  on it starts (§10).
- The text edits these rulings imply beyond what the designs now carry are listed in §11, for
  each design's author.
- §13 holds the amendments made after the log stood. Each section they change carries a
  pointer to it.

## 0. Authority

**The owner's words** (2026-10-09, verbatim, as relayed to the session that drafted this log):

> yes the read only - also continue without constantly asking me

- **"yes the read only"** answers extraction-workspace **Q3** (its review's **OR5**) as it
  was asked. The archive source gains a read-only "another archive folder" option, chosen
  with the folder picker, where nothing is ever written. This is the only **owner-direct**
  ruling in this log. How it is built (W1's details) is delegated.
- **"continue without constantly asking me"** delegates everything else, within the hard
  limit below.
- **Earlier rulings stand and are not reopened:**
  - the workspace's owner rulings:
    - OW1: a new page inside MailArchive Desktop;
    - OW2: the source is live Microsoft 365 or the local archive, chosen per run;
    - OW3: own mailbox only, no `Mail.Read.Shared`;
    - OW4: no drag-and-drop;
  - the desktop design's owner rulings (`docs/design-mailarchive-desktop.md`, "Owner
    rulings"):
    - P1: in-process, not shell-out;
    - the feature-gap review: the dashboard surfaces verify, reindex, extract, the
      Deleted/Junk toggle and app-only. Its P7, "Export a copy (extract mbox/eml)", carries
      it out and is approved but unbuilt. **Corrected after check:** the draft left this
      ruling out of the list, which let a delegated ruling supersede P7 (see ESC-5);
  - operator ruling T7 (capture walks skip Deleted Items and Junk Email by default);
  - decision UJ-6 (launchd and Task Scheduler entries are never read back).

**Authority labels used in every table.**

| Label | Meaning |
|---|---|
| owner-direct | decided by the owner's own words |
| delegated-recommended | the design or its review marks the option *Recommended* (or *proposed*, or *the review suggests*) and it passes the hard limit, so it is taken as marked |
| delegated-judgement | no option is marked. The option is chosen by these criteria, in order: (1) never weakens a gate, an invariant, a catalog row's guarantee or a baseline; (2) simplest and closest to the owner's stated intent (a simpler tool: select the source, choose the destination, copy); (3) reversible; (4) keeps behaviour honest to the user |
| ESCALATE | every option needs something only the owner can grant. The question goes back to the owner with a recommended answer and a safe interim |

**The hard limit** (CLAUDE.md: "Intentional reductions are accepted by the OPERATOR, never
self-granted"):
- A delegated ruling never self-grants a gate reduction. That covers accepting a weaker
  invariant, editing a baseline, removing or loosening a catalog row's guarantee, changing
  the vendored kit meta-tests under `assureblock/`, and accepting a known gap as "fine".
- Strengthening a check is allowed.
- A change to `assureblock/` is recorded as a follow-up for the kit owner (§11). It is never
  made here.

**How the limit is applied, the same way in every section:**
1. **Additions are accepted.** New R, S and X ids, new MA rows, and amendments that only add a
   guarantee, land catalog-first with their slice under the ids of §8.
2. **Narrowings are not.** An amendment goes to the owner (ESCALATE) if it does any of these:
   - narrows existing catalog or contract text;
   - adds an exception or a deletion case to it;
   - changes what an existing covering test or golden asserts.

   Where the design works without it, it is simply not adopted.
3. **No residual is accepted.** Where an option reads "accept the residual", the ruling takes
   the option that does not need it. Whatever still remains is an **open gap, not accepted**:
   a skip encoder citing its invariant where a test can show it, and otherwise an open
   condition stated next to the claim.

## 1. Joint rulings (one answer, cited by every design)

### J1 — Catalog ids
- **Ids:** workspace Q11 / OR2 · fault-isolation OR8 · re-capture Q11 / OR4 · mime-fidelity
  OR5 · local-confidentiality OQ-10.
- **Question:** Who allocates the R, S, X and MA ids that all five designs claim, and should
  the meta-test gain a duplicate-id check?
- **Ruling:**
  - The ids are allocated in §8: contiguous R, S and X blocks in build order, the assigned MA
    ranges kept, and overflow from MA-500.
  - The five designs cite only allocated ids, and no covers marker cites a provisional
    label. Each design rewrites its Covers cells before its first catalog-first PR.
  - Catalog-first PRs land one at a time, in §9's order.
  - The duplicate-id check is wanted, together with two further meta-test strengthenings. All
    three go to the kit owner as follow-ups (§11), and `assureblock/` is not edited here.
- **Authority:** delegated-judgement.
- **Why:** two properties of the covers-map make provisional ids unsafe:
  - It treats any R id mentioned anywhere in the catalog as valid (`invIDRe`,
    `assureblock/covers_map_test.go:24`). Five different "R22"s would therefore all pass.
  - It parses a spec row only as an integer `MA-N` id followed by a tier letter (line 23). A
    split row named `MA-401a` would escape the coverage rule, and a marker citing it is read
    as `MA-401` (line 28).
- **Unblocks:** the first catalog-first PR of every design.

### J2 — Manifest version
- **Ids:** re-capture Q2 / OR2 (made joint) · fault-isolation §6 · mime-fidelity §6 and the
  v7 clause of its OR3 · local-confidentiality §3.9 and §6.2 · the `BodyFallback` change.
- **Question:** Does any in-flight change bump the manifest past version 6?
- **Ruling:** No bump. The manifest stays version 6 (`internal/state/manifest.go:35`).
  - Every new field is additive and `omitempty`. That covers the five designs, the
    `body_fallback` field that has already landed (`manifest.go:87`), and FB-1's writer
    signal.
  - `Load`'s ceiling stays 6, so MA-201 and S39 are unchanged.
  - Each design keeps the list in its §6 of what an older binary drops, field by field.
  - A future change that cannot be additive needs a new owner ruling. It would ship with
    re-capture RC29's expand/contract.
- **Authority:** delegated-recommended. Re-capture OR2 recommends no bump, and
  fault-isolation, mime-fidelity and local-confidentiality had already chosen it.
- **Why:** rollback stays a binary swap. A bump would make every older binary refuse the
  archive, and rolling back past it would mean restoring a backup, which discards every
  capture made since the upgrade.
- **Unblocks:**
  - re-capture S3 and the version part of S4;
  - it removes mime OR3's v7 clause and local-confidentiality §6.2's v7 paragraph.

### J3 — Windows posture of hand-over copies
- **Ids:** workspace Q10 / OR1 · local-confidentiality OQ-7.
- **Question:** On Windows, should a hand-over copy (a workspace export directory, or an
  `extract -dest` root) get an owner-only protected DACL, or inherit the destination's ACL?
- **Ruling:** Inherit, and name the posture before the first write.
  - The workspace preview's posture line (its XC32) and `extract` both use
    local-confidentiality's `Posture(dir)`. **[Mechanism amended by RR-2, §13.5:** the
    workspace uses a hand-over form of `Posture`, computed over the security descriptor a new
    child of the destination would inherit; `extract -dest`'s use carries the same defect, an
    open gap owed by LC-A.**]**
  - POSIX modes stay 0700/0600 either way.
  - Local-confidentiality's protected DACL (its P3) is scoped to archive roots and to the
    tool's own state directories. The workspace sentence in its §6 and the text of R22
    (R-CONF-1, owner-only at rest) both say so.
- **Authority:** delegated-recommended (workspace Q10 marks it Recommended, and
  local-confidentiality revision 2 describes it).
- **Why:** a hand-over has to open for its recipient. An owner-only DACL forces a
  standard-user recipient to have an administrator take ownership, and on a file share
  "Administrators" means the server's administrators.
- **Unblocks:**
  - the Windows write path of workspace E2;
  - the Windows `extract -dest` half of local-confidentiality slice A.

### J4 — The shared Graph primitive SG
- **Ids:** fault-isolation OR9 (FC21) · workspace XC25 (with its E1c slice, MA-401, MA-402
  and MA-443).
- **Question:** Who owns the shared Graph primitive, and what is its single definition where
  the two designs disagree?
- **Ruling:** Fault-isolation owns SG. It is a standalone slice that lands before
  fault-isolation S3 and before workspace E1, and E1 depends on SG, not on S3. Where the two
  designs differ, SG's definition is:
  - **Type.** `StatusError{Code int, GraphCode string, Text string}`, with `Text` bounded.
    `Error()` keeps today's text (fault-isolation FC21), and callers branch with
    `errors.As`.
  - **Classifier.** There is one, `graph.Classify`.
    - `app.classifyFetch` stays capture's wrapper, in S3.
    - The copy export maps `graph.Classify` onto its own outcome table (workspace §4.13).
    - The 404 class, the 403 handling and the breaker thresholds stay per consumer, each with
      a one-line justification. Fault-isolation §4.8.1 has its line; the workspace adds its
      own to §4.13.
  - **Download.** There is one bounded `MIMETo(w, maxBytes, idle, overall)`: idle 2 min,
    overall 2 h, size cap 200 MiB. For both consumers, `$value` gets its own
    response-header timeout of at least the idle window (workspace XC26), so a large
    conversion never becomes a permanent 60-second poison. **[Amended by RR-6, §13.4:**
    capture's overall cap stays at today's 10 minutes until S3; the 200 MiB figure is replaced
    by one confirmed from Microsoft's documentation, at least 256 MiB, that reaches capture
    only where the documentation bounds it; the header timer times each attempt.**]**
  - **nextLink guard.** One guard, built once in SG, with three parts: a repeat set, a page
    cap, and a host/path pin with XC25's normalization.
    - The repeat set and the page cap run in every paging loop from SG on, capture's
      `Folders` and `Messages` included.
    - The host/path pin runs on the workspace loops (`FolderTree`, `MessagePage`) from E1 on.
    - Capture's `Folders` and `Messages` get the pin in a separate step, **FI-SGc**, which
      switches it on once MA-443's nextLink-shape sub-case is recorded. That keeps XC25's
      gate, which protects scheduled backups from a harmless Microsoft-side variation (L8-2).
      Removing the gate is the owner's call (ESC-7). Fault-isolation's FC21 asks for the pin
      in every loop; its capture half waits behind XC25's gate, so both conditions hold.
    - A stop names the foreign host, and a bearer token is never sent there. From
      fault-isolation S3 on, a stop in `Messages` is a loud `folder-list-failed` gap. Before
      S3 lands it fails the mailbox, which is today's error path (`runGraphMailbox` returns
      before `walked.Mark`). A stop in `Folders` fails the mailbox.
  - **Rows.** SG carries MA-314 (its SG cases; its ledger cases land with S3 and its capture
    foreign-host case with FI-SGc, each a skip encoder citing MA-314 until then, §3) and
    MA-402. FI-SGc carries MA-505 (MA-401b). E1 keeps MA-400, MA-401 (the "a" half), MA-403
    (the "a" half), MA-515, MA-527 and MA-528 (the allocated ids are in §8). **[Added by RR-6,
    §13.4:** SG also carries MA-540 and MA-541, with `Folders`' visited folder-id set and depth
    cap.**]**
- **Authority:** delegated-judgement.
- **Why:**
  - One type and one guard live in one package.
  - The repeat set and page cap only stop loops that would never end, so they fail loud and
    closed and cannot fail a healthy walk.
  - Workspace E1 no longer waits on fault-isolation S1a.
- **Corrected after check:** the draft dropped XC25's gate on capture's pin ("MA-443 informs,
  no longer gates"). That gate is a review condition from lens 8 (L8-2), so lifting it is a
  gate reduction, which only the owner can grant (ESC-7). The draft also relied on RR-2 to
  re-check the change, but RR-2 runs only before EW-E2 code, after FI-SG and EW-E1 have
  shipped, and it covers lenses 3, 6 and 7, not 8. The capture-side changes SG makes are now
  re-checked by RR-6 (lenses 3, 6, 7 and 8) before FI-SG code, and EW-E1 always owes its short
  adversarial note (L2-3).
- **Unblocks:** SG, fault-isolation S3 and workspace E1. XC25's gate stands for FI-SGc.

### J5 — One gap channel
- **Ids:** mime-fidelity OR2, with Q5 restated there · fault-isolation FC26 and its §10 seams.
- **Question:** Five things need one answer: the gap primitive, who owns stub identity, where
  the `unparsed` label lives, who owns the `Counts()`/status rewrite, and the landing order.
- **Ruling:**
  - **Gap primitive.** Fault-isolation's `ReadGaps` + `GapClass`, together with
    `probeImproves`, are the single owner of record gap labels.
    - Mime-fidelity's parse-gap kinds register there as informational, so they never make a
      run partial.
    - Per-part gaps stay `Attachment.GapKind`.
    - `unreadable:attachments` means the whole attachment table failed.
  - **Stub identity.** Fault-isolation owns `stubIdentityTerms`. Mime-fidelity S1 creates it,
    reproducing today's constant stub exactly, so S1 changes no identity.
  - **The `unparsed` label** on v1's `fallbackParse` stub is added outside `parsev1.go`:
    in `safeParseMessage`'s routing until the fidelity walker exists, then in the walker's
    header-fallback branch (mime S2).
  - **Counts.** Fault-isolation S1a owns the `Counts()`/status rewrite. Mime S2 and
    re-capture S5 add their kinds to it as new cases; there is never a second rewrite.
  - **Landing order.** Fault-isolation S1a → mime S0 → mime S1 → fault-isolation S2b. The other
    order (S2b first, then "regenerate MA-330's golden for panicking inputs") is not taken,
    because it edits a baseline. The identity fold itself is J8.
- **Authority:**
  - the gap primitive: delegated-recommended (mime OR2 proposes it and fault-isolation FC26
    adopts it);
  - the rest: delegated-judgement.
- **Why:** each label and each status line gets one owner, the frozen file is never edited,
  and sinks land before producers.
- **Unblocks:** mime S1 (together with OR4), and the merge order of fault-isolation S2b.

### J6 — How a repair commits
- **Ids:** mime-fidelity OR3 · re-capture Q10 / OR3 (its first sub-question).
- **Question:** How does mime-fidelity's P11 repair commit the files it rewrites?
- **Ruling:** A repair has no commit path of its own. It is a re-capture through re-capture
  integrity's verdict (its §4.3) and write path (W1–W5).
  - **Outcomes.** It takes only UNCHANGED, COMMIT or KEEP, never MERGE.
  - **Inventory.** V1C's per-entry v1↔fidelity correspondence goes to re-capture's comparator
    as the T1-equivalent inventory.
  - **No exemption.** No in-place exemption is added to re-capture's R5 amendment.
  - **File names** are whatever re-capture's commit gives (re-capture Q1, ESC-3). MA-346 reads
    "the stem re-capture's commit gives it".
  - **"Never to less"** belongs to re-capture's monotone invariant, R28. Mime's R27 (frozen
    identity, guarded repair) cites it and keeps V1C's extra preconditions: the archive files
    are as recorded, and it is the same message.
  - **R27 makes no file promise for a repair.** It lands in MF-S0 saying that no parser change
    moves an archived message's key or fingerprint, and that a repair's files are those R28's
    commit gives it: its stem, or that stem's `-vN` successor, as ESC-3 rules. It drops "or
    files" and "re-captured in place". **Corrected after check:** the draft accepted R27 as
    written, but under this ruling a parse-scheme repair goes through W1–W5, and ESC-3's
    recommended answer (a) moves its files to `-vN` (re-capture Q1 lists a parse-scheme repair
    among the triggers). R27 as written would then be false.
  - **The manifest-v7 clause** is moot under J2.
- **Authority:** delegated-judgement.
- **Why:** an in-place exemption would accept the §1.7 window: a kill can leave a new zip, the
  old html and the old fixity, which `verify` reports as "modified". That is a known gap a
  delegated ruling cannot accept. One commit primitive also means one crash story.
- **Unblocks:** it removes OR3 from the blockers of mime S7 and re-capture S4/S5. Mime S7
  still waits on re-capture S4 and S5 (ESC-2, ESC-3) and on RR-4.

### J7 — Which proposed catalog texts land under delegation
- **Ids:** re-capture Q5 / OR5 (in part) · fault-isolation §2.1 · mime-fidelity §9 and its R2
  clarification · local-confidentiality §6.4 · workspace §2.
- **Question:** Which proposed catalog and contract texts may land without the owner?
- **Ruling:** rules 1 and 2 of §0, design by design:

| Design | Accepted under delegation (each with its slice) | Not accepted under delegation |
|---|---|---|
| Fault isolation | new R25, S46–S48 and their rows. R25's text carries OR6's per-input opt-in: by default no directory link is followed, and with the opt-in an input's directory links are followed under its visited (dev, inode) set, never a file link out of the store or a special file. R1's ledger sentences (recorded, listed, retried every run, never consulted to skip work). R10's "the smallest unit" and "never hangs" as added sentences. R17's "a message a run could not archive is recorded as a capture gap and retried" as an added sentence. R18's `partial` flag (fault-isolation OR1) | not adopted, residual open: R10's exceptions (N7, N8) and its narrowing of "never a fatal crash". R17's replacement wording ("archived, or recorded as a capture gap"). R1's N9 sentence, which becomes a skip encoder citing R1 in S2a. X1's exit 3 (fault-isolation OR2) |
| MIME fidelity | new R26, S49 and MA-330..349. New R27 without a file promise for repairs: no parser change moves a key or fingerprint, and a repair's files are those R28's commit gives it (J6) | the R2 clarification, which lands with re-capture's R2 text as one paragraph (ESC-2). Option (a) of Q6 / OR1, which amends R2's "`full` re-exports all" (ESC-6) |
| Re-capture integrity | new R28, S50, S51, their rows and the confirmed-open gap encoders. R1's "a kept item is not a gap" (with S5). MA-238, strengthened (it also holds at an unchanged path, and the kept file is owned through AlsoFiles). MA-227's existing reach (AlsoFiles, which also owns DEMOTE-kept files). R5's write-beside sentence, with S4, **only if ESC-3 is answered (a)**: under (b) or (c) the new files are not written beside the old ones | ESC-2: R2; R13's new deletion cases (superseded copies, the redacted stem's `-vN` family) and its N2 adoption exception; MA-227's `-vN`-family clause; S24; the MA-66 and MA-87 covering-test changes; design-incremental-completeness B2 |
| Local confidentiality | new R22–R24, S42–S45, X12 and their rows. The R4, R19, X9, X11 and MA-232 amendments. MA-277 (the reader on its own ephemeral loopback port) and MA-281 (reconnect only to this account's verified instance): the shortcut still always opens the user's own dashboard, no assertion is removed without a stronger replacement, and the C2 adversarial pass confirms it | ESC-4: R13's classifier and intent rule (CC27, OQ-8), and R13's added deletion cases (the scrub, stale-page and empty-folder removal, the stale-snapshot sweep, the manifest-driven gone set) |
| Extraction workspace | new R29, S52, X13–X15 and their rows. X14 ("look before writing outside the archive") is scoped to the dashboard's actions, because `extract -dest` writes without a dry run | — |

- **Authority:** delegated-judgement. This is the hard limit, applied.
- **Why:** additions and strengthenings cannot lower a gate. Narrowing catalog text, or
  changing what a covering test asserts, is the owner's decision.
- **Corrected after check:**
  - Re-capture: the draft accepted MA-227's strengthening whole. That strengthening includes
    deleting the redacted stem's `-vN` family (`design-recapture-integrity.md` §6), which is
    R13 deletion case (c). The draft escalated that same case in ESC-2, and the design lists
    MA-227 among the guards that need operator acceptance. The `-vN` clause therefore moves to
    ESC-2.
  - Re-capture: R5's write-beside sentence is true only under ESC-3's answer (a).
  - MIME: R27 as written promised that no parser change moves a message's files and that a
    repair is in place. Under J6 that holds only if ESC-3 is answered (b) or (c), so R27 lands
    without that promise (J6).
  - Local confidentiality: its R13 amendment also adds deletion cases (§6.4 of that design),
    which widen R13's "only … are swept". The draft neither accepted nor escalated them. They
    join ESC-4.
  - From the skeptic's notes: R25 needs OR6's opt-in in its own text, or it is false for an
    input that uses the opt-in; X14 as worded is false for `extract -dest`; and N2 adoption
    (re-capture RC36) keeps a record whose recorded file is missing, an exception to R13's
    "deleted, moved or renamed … is pruned", so it joins ESC-2 like local-confidentiality's
    similar change in ESC-4.
- **Unblocks:** the first catalog-first PR of every design.

### J8 — F33 stub identity against the MA-330 golden — **ESCALATE (ESC-1)**
- **Ids:** fault-isolation §4.5 (FC26) and S2b · mime-fidelity MA-330, its §5 drift policy and
  OR2.
- **Question:** May fault-isolation's raw-preserving stub fold a digest of the raw bytes into
  its identity, so that two unparseable messages stop collapsing into one record (F33)?
- **Why it is escalated:** the fold moves the identity of every message the v1 parser cannot
  parse. MA-330's identity golden pins that identity, and mime's drift policy sends any
  MA-330 move to the owner. Both options are owner-only:
  - (a) regenerate MA-330's golden for panicking inputs only, which edits a baseline;
  - (b) keep the constant stub identity, which leaves F33's collapse in place for raw
    sources, a known R1 loss.
- **Safe interim:**
  - S2b ships everything except the fold: the raw-preserving content, `Raw` kept, the
    `unparsed` gap, and containment.
  - `stubIdentityTerms` keeps today's constant stub.
  - The collapse of two or more unparseable raw messages in one folder stays an open gap,
    pinned by a skip encoder citing R1 in MA-318's raw-stub case.
- **Recommended answer:** (a), with a README note naming the shape. Fault-isolation's
  legacy-stub migration (FC27) already handles existing records.
- **Blocks:** the fold only (FI-S2b-id, §9).

## 2. Extraction workspace (`docs/design-extraction-workspace.md`, revision 2)

The workspace's owner rulings OW1–OW4 stand. Two gates sit in front of code: RR-2 (XC40)
before E2, and RR-3 (the W1 text for Q3, now folded into the design) before E4.

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| Q1 | Use the OS folder picker plus a typed path instead of the mockup's in-page destination tree? | Yes. | delegated-recommended (§4.8's decision) | no directory-listing API on the loopback surface, so no new read surface and no listing that stalls on a dead UNC path; the picker has its own "New folder" | E3 |
| Q2 | T7 does not apply to the workspace, so the tree lists Deleted Items and Junk Email, unticked — confirm? | Confirmed. Both are listed, labelled and unticked, and are included only when the user ticks them. T7 still governs capture | delegated-judgement | matches the owner's mockup; nothing is added silently | E1, E3 |
| Q3 / OR5 | Should the archive source also accept "another archive folder", read-only and chosen with the picker? | **Yes**, see W1 | owner-direct | the owner's words | E4's inheritor cell, after RR-3 |
| Q4 | Include records that have left the mailbox in a folder selection by default? | Yes, counted separately in the preview | delegated-recommended ("Proposed: yes") | the archive's value is mail the mailbox no longer has, and the preview says how many | E4 |
| Q5 / OR7 | Confirm the workspace never ships before E0 (local-confidentiality C1 + C2), even on single-user installs? | Confirmed. E0 is scheduled first, on its own merits (§9) | delegated-judgement | shipping without E0 would add a mailbox-read surface that any local account can already drive (F3), a known exposure a delegated ruling cannot accept | E3 (order) |
| Q6 / OR6 | Should an administrator be able to turn the workspace off for a deployment? | Yes, and MA-462 (MA-526) applies | delegated-recommended | the only rollback short of an uninstall, which would also remove E0 | E3 |
| Q7 | Download one message at a time in v1, or 2–4 in parallel? | One at a time. Parallel downloads wait for MA-443's measured throughput | delegated-judgement | simplest; the export shares the 4-concurrent-per-mailbox budget with capture while F17 is open | E2 |
| Q8 | Accept the reworded panel note for the archive source? | Yes | delegated-judgement | "Always exports original EML" is false for a default desktop archive | E4 |
| Q9 | Should "Extract attachments" wait for the C1-1 parser fix? | Yes. It ships in E6, after mime-fidelity S2 | delegated-judgement | shipping it earlier ships a known re-encoding defect | E6 |
| Q10 / OR1 | Windows ACL posture of an export root | = J3: inherit, and show the posture line | delegated-recommended | J3 | E2's Windows write path |
| Q11 / OR2 | Id allocation | = J1 (§8) | delegated-judgement | J1 | catalog acceptance of every slice |
| Q12 / OR3 | Which engine is the owner's "simpler tool"? | This workspace's own copy engine (`internal/app/copyexport*.go`). Fault-isolation revision 2 already reads it this way (its FC20), so the OW4 "contested reading" paragraph and XC39 are updated to cite that (§11) | delegated-recommended | "copy it to" names a hand-over folder, while `RunGraph` writes an archive (§1.2) | E2 |
| Q13 / OR4 | Does the workspace supersede desktop P7 "Export a copy (extract mbox/eml)"? | **ESCALATE (ESC-5).** Interim: E3 ships the workspace and its Export sidebar group without declaring P7 superseded; P7 stays an approved, unbuilt desktop item, and the workspace's §3, OW4 and XC39 do not call it superseded. **Corrected after check:** the draft ruled "yes" under delegation, but P7 carries out a direct owner ruling (the desktop feature-gap review), and superseding it drops a dashboard mbox export. A delegated ruling may not reopen an owner ruling (the draft's own Q14 reasoning) | ESCALATE | it changes what an owner ruling delivers | nothing: E1–E6 proceed on the interim |
| Q14 / OR8 | Run the native picker in a child process (a self re-exec)? | No. Desktop owner ruling P1 (in-process, not shell-out) stands, and XC31's minimum applies: Browse is disabled while an export runs, and §5.2 keeps the crash and hang rows | delegated-judgement | a delegated ruling does not reopen an owner ruling; with Browse disabled during a run, a picker crash cannot cost an export | E3 |
| Q15 / OR9 | Who owns F17, F75, F91 and F111? | For exports, F75, F91 and F111 are handled by an export-only, stricter naming pass layered over `util.SanitizeSegment` and `SanitizeFilename` (never a copy of them), built in E2. MA-417's sub-cases then pass with no archive migration. The archive-side fix, which needs its own migration, stays open outside these five designs, as an open gap, not accepted: when MA-417's sub-cases turn green in E2, E2 also lands a skip encoder citing R4 for the archive writer's names, so the gap keeps an encoder (from the skeptic's notes). F17 goes to the owner of the shared Graph layer (fault-isolation), as slice FI-GF17 after S3; fault-isolation's §3 N1 stops listing F17 as owned elsewhere | delegated-judgement | no archive migration; one owner for the Graph layer | E2 (P7 holds for exports); §4.14 and R-v once FI-GF17 lands |

**W1 — the second archive source (Q3).** The owner-direct part is the answer to Q3 as asked:
yes, a read-only "another archive folder" source, chosen with the folder picker, where nothing
is ever written. Everything else below is delegated-judgement detail that serves that answer
(corrected after check: the draft filed all of it under the owner-direct label).
- The source chooser gains **Another archive folder…**, picked with the OS folder picker
  (Browse) or typed, as the destination already allows. The configured archive location stays
  the default archive source.
- **Nothing is ever written there.** That rules out a lock, a manifest save or migration, an
  index repair (`RepairKeys`), a SQLite `-wal`, `-shm` or checkpoint, and any temp file.
  - The design picks the mechanism: for example SQLite's `immutable=1`, or listing from the
    manifest alone. If a `-wal` sits beside `search.db`, the list says recent rows may be
    missing, and whole-folder selection still works.
  - A new row, MA-531, asserts it by bytes. After preview and run, every file under the
    folder is byte-identical and no file has been added, read-only media included. That is
    stricter than MA-436's row compare.
- **It never touches Settings or the weekly backup.** The inheritor walk (§7.4) therefore
  loses its Settings step. XC22's save guard stays for users who do repoint Settings.
- **The folder is untrusted input.**
  - The §4.6 gates apply unchanged: validated paths, the Lstat walk, the nlink check,
    `os.Root`, bounded reads and fixity.
  - A newer format is refused (R-p).
  - A destination inside the folder is refused, as one inside the configured archive is
    (R-f).
- §3's non-goal "an archive other than the dashboard's configured archive location" is
  withdrawn. This commit folds W1 into the workspace design (§3, §4.1, §4.3, §4.7, §7.4,
  §7.6, §9's MA-531 and §10's E4, each marked `[ruling W1]`), so RR-3 has text to review
  before E4.

## 3. Fault isolation (`docs/design-fault-isolation.md`, revision 2)

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| OR1 / OQ1 | What last-run status does a partial run get? | `"failed"` plus `partial: true`. `JSONVersion` stays 2, because the field is additive | delegated-judgement | an older `status` then fails closed (RED) instead of showing GREEN for a partial run whose gaps are all in the ledger; no documented value set widens | S1b |
| OR2 / OQ7 | Amend the locked X1 so that the capture verbs exit 3 on a partial run? | No amendment under delegation. A partial capture run keeps exiting 1 (S1a's mapping), and the partial state travels in the flag, the summary, the sidecar and `status`. The owner may still amend X1 later | delegated-judgement | X1 is locked, and design-review step 4 makes its amendment an owner act; exit 3 would also stop monitors that key on exit code 1 from alerting on cases that fail today | S1b |
| OR3 / OQ2 | Persistent known gaps: report on every run, or report only new loss? | Report on every run. Each run that observes a fillable or terminal gap is partial on every surface (exit 1, the sidecar kept, the `-job` notification sent), and each surface leads with "new this run: K" and `last_run.gaps_new`. Not-attempted gaps stay a note | delegated-judgement | reporting known loss as a clean run would accept it; `gaps_new` lets monitoring separate new loss | S1b |
| OR4 / OQ3 | Does a "vanished" fault seen for the first time make the run partial? | Yes | delegated-recommended | a one-shot run before a departure must not claim completeness | S1b |
| OR5 / OQ4 | What is the stuck threshold, and in what unit? | `Runs ≥ 3` AND `now − FirstRun ≥ max(72 h, two schedule periods)`, counting at most one observation per day. The posture stays WARN (FC18) and never becomes RED | delegated-recommended (the review's suggestion) | escalation no longer depends on the schedule's cadence or on extra clicks | S1a |
| OR6 / OQ5 | Should local stores follow directory links? | Not by default. An explicit per-input opt-in (a flag, a GUI option and a job-descriptor field, following X4/X5) keeps the input's token and follows directory links for that input only, guarded by a visited (dev, inode) set. File links outside the store and special files stay refused even with the opt-in. An archive that captures through links today gets a terminal gap whose remedy names the opt-in. R25's own text names the opt-in, so the invariant stays true for an input that uses it (J7; from the skeptic's notes) | delegated-judgement | the only option that keeps an affected user on the same token, with no second tree, while R25 holds by default | S2b |
| OR7 / OQ6 | Is a Graph 403 on `$value` mailbox-fatal? | Yes | delegated-recommended | a revoked grant must not turn into thousands of gaps | S3 |
| OR8 | Catalog ids | = J1 | delegated-judgement | J1 | S1a and every catalog edit |
| OR9 | Who owns SG? | = J4 | delegated-judgement | J4 | SG, S3, workspace E1 |
| OR10 | The P7 residual (L10-3) | Take the stricter rule: exclude PhysID-less records from the sweep on any fault whose identity matched no record. Gone-detection is then deferred (and `status` names it), never wrong | delegated-judgement | the default would accept a known phantom "gone" (R21, S37) | S3 |

Notes:
- **§2.1's amended texts follow J7.** The added sentences land. R10's exceptions and R17's
  replacement wording are not adopted:
  - N7 and N8 stay open conditions;
  - a poison item shows as an open R17 gap in `status` until it is captured;
  - N9 becomes a skip encoder citing R1, on a byte-patched copy of `support.pst`, in S2a.
- **Rows that still span two slices:** MA-318 (S2a and S2b), MA-323 (S1a and S2a), MA-325
  and MA-326 (S1a or S1b, and S5), and MA-314 (SG, then S3 for its `fetch-timeout`,
  `fetch-too-large` and `folder-list-failed` gap cases, which need the S1a/S3 ledger, and
  FI-SGc for its capture foreign-host case, J4).
  - Each lands with its earlier slice and carries a skip encoder for its later cases, citing
    the row, until the later slice lands.
  - Without that, the covers-map would read an unbuilt half as covered. That is FC33's
    reasoning.
  - **Corrected after check:** the draft's list left out MA-314 (and, in §5, MA-361).
- **F17** is now this design's slice FI-GF17 (workspace Q15). Its §3 N1 no longer lists F17
  as owned elsewhere (from the skeptic's notes).
- **Under OR2,** P9's "capture verbs exit 3" and MA-325's exit-3 cases read "exit 1 with the
  `partial` flag". The UX-contract X1 amendment that S1b owed is no longer needed.

## 4. MIME fidelity (`docs/design-mime-fidelity.md`, revision 2)

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| Q1 | Repair automatically, or only when the operator asks? | Automatically, on the next ordinary run. `state.RevokedSchemes` is the rollback lever | delegated-recommended ("proposed") | it mirrors MA-70's re-examination, and V1C (never to less) and MC4 bound the blast radius | S7 (with J6, ESC-2 and RR-4) |
| Q2 | May we commit an Outlook-written PST fixture with method-5 attached items? | Yes, if it is synthetic. Either it is generated on a Windows Outlook host from made-up messages (no real mail), or it is a third-party sample whose licence is recorded in THIRD-PARTY-NOTICES. Until it exists, MA-343 stays L and pending, and S6 does not merge | delegated-judgement | the only way P9 becomes proven; no personal data enters the repo | S6 |
| Q3 | A follow-up design for repair from the preserved `.eml`? | Yes, as its own design with its own 10-lens review. It does not block S0–S7 | delegated-judgement | "`-mode full` is enough" would accept that a record whose source is gone can never be repaired | — |
| Q4 | Keep uuencode parts still encoded, or add a decoder now? | Keep them still encoded, with a terminal `undecoded-part` gap and the bytes kept (R26). A bounded decoder can be a later slice | delegated-recommended ("proposed") | the gap is visible and the bytes survive | S2 |
| Q5 (part of OR2) | Make the identity freeze a house rule? | Confirmed. Identity comes from the frozen v1 replay for every future parser change, and the frozen surface changes only through §5's drift policy, which takes any MA-330 move to the owner. Fault-isolation's stub change is an edit to `stubIdentityTerms`, never to `parsev1.go` (J5, J8) | delegated-judgement | it protects R3 and reserves identity moves to the owner | S1 |
| Q6 / OR1 | How does with-Message-ID Graph mail get repaired on MailArchive Desktop? | **ESCALATE (ESC-6).** Interim: MC16/MC37's disclosure stands, the gap stays open, not accepted, and MF-SD does not start. **Corrected after check:** the draft took option (a) under delegation, but (a)'s full walk "writes only records where the repair fires", which the design and its review both say "amends full mode's 're-export everything' for that walk". That adds an exception to R2's "`full` re-exports all", a narrowing §0 rule 2 sends to the owner. (b) cannot reach a default Desktop archive, which keeps no `.eml`, and (c) accepts the gap | ESCALATE | every option either amends R2 or accepts the gap | MF-SD, once answered |
| OR2 | The joint ruling with fault-isolation | = J5. The identity fold is J8 (ESCALATE) | see J5 | J5 | S1 |
| OR3 | The joint ruling with re-capture integrity | = J6 | delegated-judgement | J6 | removes OR3 from S7 |
| OR4 | Pin the Go toolchain? | Keep floating; the CI policy is unchanged. P1's toolchain residual stays an open condition, not accepted. MA-330 and MA-331 run on every CI build and catch drift on corpus shapes. A strengthening is recommended for S1: vendor `encoding/base64` into `parsev1`, and have MA-331 assert `unicode.Version`, so that the named residual becomes a tripwire | delegated-judgement | pinning would miss toolchain security fixes, which weakens the CI policy (the track-the-current-patch rule in `ci.yml` that OR4 cites); accepting the residual is not delegable | S1 |
| OR5 | Catalog ids | = J1 | delegated-judgement | J1 | S0 |

Notes:
- **S0's precondition is met.** S0 needs `BodyFallback` to have landed, and it has on this
  branch (`internal/state/manifest.go:87`).
- **J6 changes §4.10.** Its staged rename-over is replaced by re-capture's commit. That
  change goes in before RR-4.
- **S7 waits for S6, unconditionally.** The design and MC43 require S2–S6 to ship before S7,
  or in the same release, so that users see a single repair burst: S6's embedded-message
  notes would otherwise fire a second burst. While Q2's fixture does not exist, S7 waits.
  Shipping S7 before S6 relaxes MC43, which is the owner's decision; it is not asked now,
  because S7 already waits on ESC-2 and ESC-3. **Corrected after check:** the draft's build
  order made S6 a dependency only "if its fixture exists".

## 5. Re-capture integrity (`docs/design-recapture-integrity.md`, revision 2)

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| Q1 / OR1 | How does a changed re-capture keep names and page state? | **ESCALATE (ESC-3).** Ask after S1's §1.7 encoder has run (RC33) | ESCALATE | every option either narrows R2, loosens what `verify` accepts, or accepts the §1.7 false alarm | S4 |
| Q2 / OR2 | Manifest version | = J2: no bump | delegated-recommended | J2 | S3, S4 |
| Q3 | Same-name attachments whose non-empty size changed: commit or keep? | Commit, counted as `changed` (NG3, the design default) | delegated-judgement | parser fixes legitimately change bytes; empty, absent and `.partial` items are still caught (KEEP, mime P4) | S5 |
| Q4 | What posture do kept content and pending removals get? | Kept content: GREEN with an info line. Pending-removal WARNs: per cause, and only after a completed run | delegated-judgement | a WARN must name a remedy (X6), and kept content has none; "never" would hide a real pending state | S4, S6 |
| Q5 / OR5 | Accept the amended guards? | **ESCALATE (ESC-2)** for the narrowing items. The additions are accepted (J7) | ESCALATE | they change existing catalog text and the assertions of existing covering tests | S1 (wording), S4, S5 |
| Q6 | Starvation on pre-v7 records | A one-time inventory backfill as slice RI-S7, after S5. It reads each earlier zip's central directory into `Held` only where the zip matches its recorded `Fixity.Zip`, and it never re-baselines fixity, unlike `verify -record`. Until then the starvation is an open gap, not accepted | delegated-judgement | it closes the gap instead of accepting it | RI-S7 |
| Q7 / OR6 | Legacy-scheme adopts: DEMOTE or KEEP? | DEMOTE (the default): commit, and keep the earlier file, owned through AlsoFiles, counted and reported | delegated-judgement | KEEP would silently leave a distinct Message-ID reuse uncaptured (R1) | S5 |
| Q8 / OR7 | Should `reindex` remove crash orphans outside redaction? | No. NG5 stays: they are never removed automatically outside redaction, and they are listed as `earlier-copy`, naming the current copy | delegated-judgement | automatic removal could delete a message's newest bytes (R1, R13) | S4 (RC15's remedy text) |
| Q9 | No forced downgrade? | Confirmed. No flag forces a downgrade, and removing content stays redaction-only (NG1) | delegated-judgement | a forced downgrade would lose content | S5 |
| Q10 / OR3 | The sibling in-place writers | (1) The mime repair = J6. (2) The fault-isolation stub re-render no longer exists: fault-isolation revision 2 supersedes metadata only, writing no file (FC6(4), FC7). So no P5 exception is needed; drop that sentence from the R5 amendment and from §4.13. Fault-isolation's heal-in-place goes through the shared exporter commit. (3) This design's user-facing terms and codes become "pending removal" (`removal_pending`). Fault-isolation keeps "superseded" for its metadata-only re-class | (1), (2): delegated-judgement. (3): delegated-recommended | one commit primitive; no exception for code that is gone; one meaning per word | S4, S5 |
| Q11 / OR4 | Catalog ids, and a duplicate-id meta-test check | = J1. The check is a kit-owner follow-up (§11) | delegated-judgement | J1 | S1 |

Notes:
- **The interim S1, under ESC-2.** S1 lands the new R28, S50 and S51, plus the rows RC32
  names for the confirmed-open gaps (F46, N2, N3, F48 and §1.5–§1.7).
  - Their skip encoders cite the existing invariant each defect breaks, for example F46 → R1,
    N2, N3 and §1.7 → R5, and F48 → R13. They run under the RC31 harness.
  - No existing text or covering test changes.
  - The feature rows (HEAL, MERGE, wording) land with the slice that builds them (RC32).
  - The result of the §1.7 encoder is what ESC-3 is asked with (RC33).
- **RI-S2 lands its F48 core:** the absent-only rule, archivefs-gated deletion and row repair.
  That restores R13 as written, because an index row naming an earlier file of a message
  whose current file is intact is not a message whose file was deleted.
  - Deleting the redacted stem's `-vN` family is a new deletion case, so it waits for ESC-2.
  - **N2 adoption (RC36) waits for ESC-2 too.** It keeps a record whose recorded file is
    missing by re-pointing it to an unowned newer copy, where R13 says such a record is
    pruned. That is an exception to existing catalog text, the same kind of change ESC-4 asks
    about for local confidentiality. Until the owner answers, RI-S2 prunes an N2 victim as
    today. **Corrected after check** (from the skeptic's notes): the draft put adoption in the
    delegated core and said the core restores R13 as written.
- **MA-361 spans two slices** (corrected after check): it lands with RI-S2 without its
  `-vN` redaction twin and its N2-victim adoption twin. Each of those is a skip encoder citing
  MA-361 and R13 until the slice after ESC-2 builds it.

## 6. Local confidentiality (`docs/design-local-confidentiality.md`, revision 2)

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| OQ-1 | Should slice A-W rewrite existing Windows archive roots' DACLs automatically? | No. Report only, plus the opt-in `mailarchive access -restrict` / `-restore` (the built default), and A-W is not built. `status` stays RED for an open archive until it is restricted | delegated-judgement | the tool never rewrites an ACL on a user's machine unasked, so a backup account that relies on a broad principal keeps working; the exposure is never hidden | A (A-W dropped) |
| OQ-2 | Record tombstones so a redaction survives re-capture? | No. The warning only ("delete it at the source first") | delegated-judgement | a tombstone adds a skip exception to R1 and R17's "every message is archived" | D |
| OQ-3 | Support shared-read archives? | No. Owner-only, with no opt-out | delegated-judgement | an opt-out would weaken R22 (R-CONF-1), and none is designed | A |
| OQ-4 | Accept the one-time-link ceremony for the reader and the dashboard? | Yes, as §7.1–7.2 count it. C2's friction review is a co-equal ship blocker | delegated-judgement | refusing it leaves F3 (high) open | C2 |
| OQ-5 | Peer-credential admission: now, later or never? | Later. It is reconsidered after C2 ships and its friction review is done. Until then the login-CSRF and packet-capture residuals stay open, not accepted | delegated-judgement | a second, OS-specific admission path adds surface now, and macOS has no equivalent | — |
| OQ-6 | Run the one-time legacy scrub automatically at the end of the first capture, or only in `reindex`? | Automatically (the built default), after the free-space pre-flight, and retried at most weekly when unattended. `status` WARNs only if it was skipped or failed | delegated-judgement | it removes text the user believes is already gone, with no action needed | D |
| OQ-7 | Windows posture of hand-over copies | = J3 | delegated-recommended | J3 | A (Windows `extract -dest`), workspace E2 |
| OQ-8 | Should redaction need explicit intent? | **ESCALATE (ESC-4)**, together with R13's classifier amendment and R13's added deletion cases (corrected after check: the draft escalated only the classifier and the intent rule). LC-D's catalog-first PR lands none of them until the owner rules | ESCALATE | every option either narrows R13's automatic prune or drops the safety net that dissolved the review's hidden blocker | D |
| OQ-9 | Where does the dashboard live? | Option (b): an ephemeral port, like the reader. The shortcut, and "Copy MailArchive link", are the only way in; 8097 leaves the RMM runbook. CC23's notice stays, adapted: when an unverified listener holds 127.0.0.1:8097, the dashboard header and `status` say "another program is using MailArchive's former address 127.0.0.1:8097 — open MailArchive only from its shortcut", and MA-376 keeps that case. The typed-address and bookmark squat residual is narrowed, not closed, and stays an open gap, not accepted. **Corrected after check:** the draft dropped the notice, which is part of a must-fix review condition (CC23, "A squatted 8097 is never silent"); without it the residual that stays open would be silent. Option (b) does not need the notice dropped: C2 already probes 8097 for pre-upgrade instances (CC22) | delegated-judgement | (a) needs the owner to accept the squat-and-phish residual; (b) removes the fixed address the residual depends on, and the notice keeps what remains visible | C2 |
| OQ-10 | Catalog ids | = J1 | delegated-judgement | J1 | every slice's catalog-first PR |

Notes:
- **Under OQ-9, C2's text changes.** The dashboard takes a free port, as the reader does.
  P8's "8097 if free" goes, the CC23 notice names 8097 as MailArchive's former address, and
  README-RMM verifies the install by the shortcut and the locked page.
- **Under OQ-1, A-W is not built.** MA-368's L part (MA-500) still runs as slice A's
  Windows drill.

## 7. Fix-branch questions

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| FB-1 (MA-187; reader branch `7b22df7`) | On a page whose old-header bytes are not live, should the rebuild erase the fields or risk a forgery? | Option (2), an out-of-band writer signal, see the FB-1 note | delegated-judgement | option (1) is a gate reduction; option (3) reopens the RV-1 forgery and weakens an existing assertion | the reader branch's merge (with its encoder); FX-187 |
| FB-2 (RV-2; export branch `deb2e9e`) | Keep disarming `<meta http-equiv=Link>`? | Keep it | delegated-judgement | strictly safer and costs nothing; MA-470's "head Link pragma" case pins it, so dropping it would delete an assertion that passes | the export branch's merge |
| FB-3 (DIFF-FP-1 coupling; export and index branches) | May the scripting-off retry move head-noscript content into the body? | No. The retry keeps a head `<noscript>`'s content out of the rendered body; the integration pass is fixing it. MA-492's `TestLiveAndRebuiltIndexAgreeOnHeadNoscriptMarkup` must pass, unchanged, on the merged tree. The export branch's product question ("is that acceptable?") is answered no | delegated-judgement | keeps live and rebuilt search in parity (R8) with no export/app API change, and matches what a reader of the original sees with JavaScript on | the integration pass |
| FB-4 (N11 remainder; `wip/g8-scheduling` `4f82d6e`) | How is a verify schedule guarded and checked for staleness? | See the FB-4 note. New rows MA-457 and MA-458 | delegated-judgement | | FX-G8 |
| FB-5 (index branch `1cbf466`) | Confirm the extractor's noscript policy, and that its cost bound belongs to the export side | Confirmed. Noscript content is indexed as a scripting-off reader parses it, appended after the main text, and a noscript inside an explicit mail head is skipped (R8 parity). The cost bound belongs to the export-side too-large cap (MA-472), so the extractor gets no guard of its own. RV-8's main-parse memory residual stays open with the export owner | delegated-judgement | a guard would send live and rebuilt input through different extractors: an R8 seam, and a threshold a sender can trigger | the index branch's merge |

**FB-1 — the writer signal.**
- **The field.** A new per-record manifest field, written whenever this build writes the page.
  It is never set on a restamp, merge or adopt that keeps an older page.
- **The rule.** `reindex -rebuild` reads a page that carries the legacy span as a legacy page
  only when its record lacks the field.
- **The field is additive v6 (J2).** An older binary that drops it makes the page ambiguous
  again, which falls back to "erase, never forge".
- **Old pages stay ambiguous.** Pages already on disk from v0.3.0 to `aa1c98d` cannot be
  told apart by their bytes. On them the reader keeps "erase, never forge", and that residual
  is an encoded open gap, not accepted: a skip encoder citing MA-187 and R8. The reader
  branch did not add that skip; this ruling asks for it.
- **MA-187's catalog text does not change.** Its guarantee is that a crafted message "cannot
  forge or erase" its indexed fields. Rewording it to concede the legacy-page erase residual
  would narrow that guarantee, which is the owner's decision. FX-SAN adds only the skip
  encoder. **Corrected after check:** the draft's build order had FX-SAN reword MA-187 "as an
  open gap".
- **New row:** MA-484.

**FB-4 — the verify descriptor.**
- **The descriptor.** When a verify schedule is installed, write
  `.mailarchive-verify-schedule.json` beside `.mailarchive-schedule.json`, and remove it
  with the schedule.
- **The name guard.** On every OS the guard reads only the archive's own descriptors (backup
  and verify). There is no read-back of launchd or Task Scheduler entries, so UJ-6 stands.
- **Staleness.** `health`'s `verify_stale` reads the verify descriptor.
- **The residual.** On a launchd or Task Scheduler host, two archives that share one explicit
  `-name` are still not detected at install. The archive that lost its verify now shows
  `verify_stale`. That is an open residual, not accepted; closing it would reopen UJ-6.

**Also open in those reports, not ruled here:**
- the CLI Verification wording at `cmd/mailarchive/main.go:1385`;
- widening the index's `blockElements` set to the flattener's;
- whether the re-check should fail closed on any live mail `<link href>`, not only on
  resource hints;
- the implied-head noscript/noframes drift, and which owner takes it;
- the flattener's SVG/MathML container and `<td>` separator fixes (MA-492 is not to be
  weakened);
- whether `noframes`, `noembed` and `iframe` text should still be indexed;
- `-token-cache` still required on Windows (the MA-260 contract);
- the Windows lab pass for `chcp` (MA-79);
- the g8 review's six minor issues;
- the adversarial round 3 the sanitizer branches owe (§9, FX-SAN).

## 8. Catalog id allocation

### 8.1 What the catalog holds today

Found with `git grep` on this branch at `176c4b2`:

| Series | Where | Highest id |
|---|---|---|
| R | `docs/scenario-catalog.md` §2 | R21 (no R22 or higher anywhere) |
| S | `docs/scenario-catalog.md` §3 | S41 (no S42 or higher) |
| X | `docs/ux-contract.md` | X11 (no X12 or higher) |
| MA | `docs/scenario-catalog.md` §4 | MA-304 (MA-305..309 free) |

- **Held on unmerged fix branches** (from their reports): MA-450..456 (`wip/g8-scheduling`),
  MA-470..477 (export), MA-480..483 (reader) and MA-488..493 (index).
- **Ranges kept as assigned:**

| Owner | Range |
|---|---|
| sanitizer fixes | MA-300..309, MA-470..499 |
| fault isolation | MA-310..329 |
| MIME fidelity | MA-330..349 |
| re-capture integrity | MA-350..364 |
| local confidentiality | MA-365..384 |
| g7 | MA-385..399 |
| extraction workspace | MA-400..449 |
| g8 | MA-450..469 |

### 8.2 Rules
1. **The five designs cite only these allocated ids.** A provisional label is never cited by
   a covers marker. That includes R22, S42 and X12 as the designs use them, R-PARSE,
   R-CONF-1, S-CONF-1, X-CONF-LOCKED and MA-401a. Each design rewrites its Covers cells and
   cross-references before its first catalog-first PR.
2. **Every MA row id is an integer.** A split row's "a" half keeps the base number, and its
   "b" half takes an id from the design's overflow block.
3. **A row has one tier.** A U row's lab half becomes its own L row.
4. **Catalog-first PRs land one at a time,** in §9's order (fault-isolation FC34).
5. **Two ids are retired:** workspace MA-406 (deleted by XC23) never enters the catalog, and
   the workspace's provisional reserve MA-467..469 is released, because it sits in g8's
   range.

### 8.3 R, S and X blocks

The blocks are contiguous and follow the build order of each design's first catalog-first
slice: local-confidentiality A, then fault-isolation SG and S1a, then mime S0, then
re-capture S1, then workspace E1.

| Design | Provisional label | Allocated | Lands with |
|---|---|---|---|
| Local confidentiality | R-CONF-1 owner-only at rest | **R22** | A |
| | R-CONF-2 local web surfaces admit only their launching account | **R23** | C1 |
| | R-CONF-3 redaction removes what the archive holds | **R24** | D |
| Fault isolation | R22 source-read containment | **R25** | S2b |
| MIME fidelity | R-PARSE parse fidelity | **R26** | S0 |
| | R-FREEZE frozen identity, guarded repair | **R27** | S0 |
| Re-capture integrity | R22 re-capture is monotone | **R28** | S1 |
| Extraction workspace | R22 copy export is separate, contained, faithful-or-absent and self-verifying | **R29** | FI-SG, which carries the workspace's MA-402 (J4) |
| Local confidentiality | S-CONF-1 another account on the same computer | **S42** | A |
| | S-CONF-2 a rebinding page, a hostile link, a squatter | **S43** | C1 |
| | S-CONF-3 a redaction inspected byte for byte, or handed over | **S44** | D |
| | S-CONF-4 a symlink or a credential file inside the archive | **S45** | C1 |
| Fault isolation | S42 one item or folder cannot be read, downloaded, parsed or written | **S46** | S1a |
| | S43 a hostile or odd local store layout | **S47** | S2a |
| | S44 an operator or inheritor reads the result of a partial capture | **S48** | S1a |
| MIME fidelity | S-PARSE a legacy, Apple Mail, list or Outlook message is archived, and an older archive is upgraded | **S49** | S0 |
| Re-capture integrity | S42 a message is re-captured while its source holds less | **S50** | S1 |
| | S43 a re-capture is interrupted, or `reindex` meets a stale row | **S51** | S1 |
| Extraction workspace | S42 a user copies selected folders and messages to a folder on disk | **S52** | FI-SG (as R29) |
| Local confidentiality | X-CONF-LOCKED locked, not blank | **X12** | C2 |
| Extraction workspace | X12 mailbox-content reads are guarded like writes (scoped, Partial) | **X13** | E3 |
| | X13 look before writing outside the archive | **X14** | E3 |
| | X14 mail-derived text is inert in the control surface | **X15** | E3 |

### 8.4 MA blocks

**Unchanged:** every row keeps its proposed number except those in the tables below.
- **MIME fidelity: MA-330..349** (20 rows, no overflow). MA-343 enters as tier L (pending) and
  becomes U when Q2's fixture lands, in S6.
- **Re-capture integrity: MA-350..364** (15 rows, no overflow).

**Fault isolation (MA-310..329; overflow MA-501..504):**

| Provisional | Allocated | Slice |
|---|---|---|
| MA-320a (local half) | MA-320 | S2a |
| MA-320b (Graph half) | **MA-501** | S3 |
| MA-324a (local half) | MA-324 | S2a |
| MA-324b (Graph half) | **MA-502** | S3 |
| MA-322's L case: a real OneDrive Files-On-Demand placeholder under a maildir | **MA-503**, tier L, pending (lab: a Windows VM with Files-On-Demand) | S2b |
| MA-320a's L case: the `-outlook` export-skip mapping on a real Windows Outlook install | **MA-504**, tier L, pending (lab: Windows with classic Outlook) | S2a |

**Local confidentiality (MA-365..384; overflow MA-500):**

| Provisional | Allocated | Slice |
|---|---|---|
| MA-368, U part | MA-368 | A |
| MA-368, L part | **MA-500**, tier L, pending | A |

**Extraction workspace (MA-400..449; overflow MA-505..534):** MA-400..449 keep their numbers.
Each split row's "a" half keeps the base number, MA-406 is retired, and MA-402 is built in
FI-SG (J4).

| Provisional | Allocated | Slice | | Provisional | Allocated | Slice |
|---|---|---|---|---|---|---|
| MA-401b | **MA-505** | FI-SGc (J4) | | MA-457 | **MA-521** | E3 |
| MA-403b | **MA-506** | E3 | | MA-458 | **MA-522** | E3 |
| MA-411b | **MA-507** | E4 | | MA-459 | **MA-523** | E4 |
| MA-422b | **MA-508** | E2 | | MA-460 | **MA-524** | E3 |
| MA-426b | **MA-509** | E5 | | MA-461 | **MA-525** | E5 |
| MA-431b | **MA-510** | E4 | | MA-462 | **MA-526** | E3 (Q6 = yes) |
| MA-432b | **MA-511** | E3 | | MA-463 | **MA-527** | E1 |
| MA-438b | **MA-512** | E4 | | MA-464 | **MA-528** | E1 |
| MA-440b | **MA-513** | E5 | | MA-465 | **MA-529** | E3 |
| MA-450 | **MA-514** | E3 | | MA-466 | **MA-530** | E3 |
| MA-451 | **MA-515** | E1 | | new: Q3's second archive source (W1) | **MA-531** | E4 |
| MA-452 | **MA-516** | E2 | | reserve (RR-2, RR-3) | **MA-532..534** | — |
| MA-453 | **MA-517** | E2 | | | | |
| MA-454 | **MA-518** | E3 | | | | |
| MA-455 | **MA-519** | E2 | | | | |
| MA-456 | **MA-520** | E3 | | | | |

**Fix branches, inside their own ranges:**
- MA-484, FB-1's writer signal (sanitizer range);
- MA-457 and MA-458, FB-4's verify descriptor and its `verify_stale` (g8 range).

The workspace's provisional MA-457 and MA-458 are now MA-521 and MA-522, so nothing collides
once the workspace text is rewritten.

**Overflow, in build order:** MA-500 (local confidentiality), MA-501..504 (fault isolation),
MA-505..534 (extraction workspace). The next free overflow id is **MA-535**. *(Superseded by
§13.1, which gives out MA-535..MA-545. The next free overflow id is now MA-546.)*

## 9. Cross-design build order

**How to read the table:**
- The numbers are merge order.
- Slices with no dependency between them may be built in parallel. Each rebases on what has
  merged before it.
- "Blocked by" names an ESCALATE (§10) or another block that must clear first. A blocked
  slice does not start until the owner answers or the block clears. Rows 39–41 merge when
  their blocks clear, whatever their number.

**These can start as soon as this log stands:**
- the lens re-runs RR-1, RR-2, RR-3 and RR-6 (RR-3 reviews the W1 text this commit folds
  into the workspace design, so it has text to review now; corrected after check, from the
  skeptic's notes);
- FX-SAN (merge after its adversarial round 3);
- LC-A and LC-C1;
- FI-SG (after RR-6, which has run: §13.4) and FI-S1a;
- MF-S0;
- RI-S1 (interim);
- FX-G8.

**Lens re-runs that must come first:**

| Id | Re-run | Before | Required by |
|---|---|---|---|
| RR-1 | lenses 3, 6 and 7 on re-capture revision 2 | RI-S4 and RI-S5 code | RC38 |
| RR-2 | lenses 3, 6 and 7 on workspace revision 2, including J3's change | EW-E2 code | XC40 |
| RR-3 | lenses 3, 6 and 7 on the workspace's second-archive-source text (W1, folded into the design and marked `[ruling W1]`); may be folded into RR-2 | EW-E4 | Q3 is a material change |
| RR-4 | delta re-review of §4.10 (lenses 3, 6, 7, 8), on the text that writes J6 in | MF-S7 | MC43 |
| RR-5 | lenses 3, 6 and 7 on §4.D | LC-D's catalog-first PR | CC27 |
| RR-6 | lenses 3, 6, 7 and 8 on SG as J4 defines it: every capture-side change (the shared `StatusError`, `graph.Classify`, `MIMETo` with its own `$value` header timeout, the repeat set and page cap in capture's loops) | FI-SG code | J4, corrected after check (XC25, L8-2, L2-3): RR-2 runs too late for SG and lacks lens 8 |

**Build order.** Slice prefixes: FX = fix branch, LC = local confidentiality, FI = fault
isolation, MF = MIME fidelity, RI = re-capture integrity, EW = extraction workspace, G7 = g7.

| # | Slice | Depends on | Re-run first | Gate owed after build | Blocked by |
|---|---|---|---|---|---|
| 1 | FX-SAN: integrate `wip/sanitizer-r2-{export,reader,index}`, with FB-2, FB-3 and FB-5, the flattener's SVG and `<td>` fixes, and FB-1's skip encoder citing MA-187 and R8 for legacy ambiguous pages (MA-187's text unchanged; corrected after check) | — | — | **adversarial pass**, round 3, over the three branches' new commits (owed by their reports) | — |
| 2 | LC-A: private at rest | — | — | **adversarial pass**; **friction review** (status lines, migration line, inheritor and recipient walks) | — |
| 3 | LC-C1: Host check, allow-list, credentials outside the archive, `localweb.Mux` and MA-378 | — | — | **adversarial pass** | — |
| 4 | FI-SG: the shared Graph primitive (J4): `StatusError`, `graph.Classify`, `MIMETo`, and the nextLink guard's repeat set and page cap in every loop (the host/path pin for the workspace loops; capture's pin is row 41) | — | **RR-6** | **adversarial pass** (network input), shared with EW-E1's note | — |
| 5 | FI-S1a: sinks and surfaces, `GapClass`, the `Counts()`/status rewrite (J5), `app.GapRemedy` | — | — | **friction review** (summary, status, sidecar) | — |
| 6 | MF-S0: the corpus, goldens from the release tag, MA-330..349 as skip encoders | — (`BodyFallback` has landed) | — | none (tests and catalog) | — |
| 7 | RI-S1 (interim): R28, S50 and S51, with the confirmed-open gap rows and encoders | — | — | none | the amended wording waits for **ESC-2** |
| 8 | FX-G8: `wip/g8-scheduling` after its review fixes, plus FB-4 (MA-457, MA-458) | — | — | re-review of the fixes; friction check of the `verify_stale` line | — |
| 9 | G7-F53: a failed manifest save is surfaced (g7's slice; it edits `commitInOrder`) | — | — | g7's own gate | — |
| 10 | FX-187: FB-1's writer signal (MA-484) | 1; `state.Record` edits serialised with 5, 12 and 17 | — | **adversarial pass** (rebuild forgery) | — |
| 11 | LC-C2: per-account surfaces, with OQ-9's ephemeral dashboard and CC23's adapted 8097 notice (workspace E0 = 3 + 11) | 3 | — | **adversarial pass** and **friction review** (a co-equal ship blocker); X9, X11 and X12 contract updates | — |
| 12 | MF-S1: identity routing, `stubIdentityTerms`, the vendored replay | 6 (with J5 and OR4) | — | **adversarial pass** (identity drift) | — |
| 13 | FI-S2a: PST isolation, plus N9's encoder | 5 | — | **adversarial pass** | — |
| 14 | FI-S3: Graph isolation | 4, 5 | — | **adversarial pass** | — |
| 15 | EW-E1: Graph additions, F15, `CheckRedirect`, identity-safe token write-back | 4 | — | short **adversarial** note, always: redirect refusal and token write-back touch capture (L2-3; corrected after check, the draft let it fold into EW-E3's pass) | — |
| 16 | RI-S2: the `reindex` guard (F48); creates the `internal/archivefs` gate | 7 | — | **adversarial pass** | the `-vN` redaction case and N2 adoption wait for **ESC-2** |
| 17 | RI-S3: additive schema and `archivefs.Inspect` | 7 | — | none of its own | — |
| 18 | LC-B: no link-following writes (`ArchiveFS`, in the same package) | 16 (the package) | — | **adversarial pass** | — |
| 19 | FI-S2b: local containment, with OR6's opt-in and raw stubs without the identity fold | 5, 12 | — | **adversarial pass** | the fold is **ESC-1** (row 40) |
| 20 | MF-S2: the fidelity walker, with C1-1 | 12 | — | **adversarial pass**; **friction review** and X-series check | — |
| 21 | MF-S5: PST interim | 12 | — | **adversarial pass** | — |
| 22 | FI-S1b: the partial outcome (OR1–OR4) | 5, 9 | — | **friction review** | — |
| 23 | EW-E2: the engine, the run record, the export-only naming pass (Q15) | 15, 5 (with J3 and Q12) | **RR-2** | **adversarial pass** on the write side (combined with EW-E3's) | — |
| 24 | EW-E3: the first shippable workspace (Microsoft 365 → original EML + manifest, retry, the switch); its Export sidebar group ships without declaring desktop P7 superseded (ESC-5's interim) | 23, 3, 11 | — | **adversarial pass** and **friction review** | — |
| 25 | MF-S3: body assembly and naming | 20 | — | **friction review** | — |
| 26 | MF-S4: the renderer | 25, 1 | — | **adversarial pass**; **friction review** and X-series check | — |
| 27 | FI-S5: the in-process callers | 22, 13, 19, 14 | — | **friction review** of the whole feature | — |
| 28 | FI-GF17: F17 in the shared Graph layer (Q15) | 14 | — | **adversarial** addendum (network input) | — |
| 29 | EW-E4: the archive source, including Q3's second archive folder (W1, MA-531) | 23, 24 | **RR-3** | **adversarial** addendum; **friction** re-run (the §7.4 inheritor cell) | — |
| 30 | EW-E5: Readable HTML | 23 (29 for the quarantine), 1 | — | none new (noted in EW-E4's addendum) | — |
| 31 | MF-S6: full PST embedded extraction | 21, plus Q2's fixture committed | — | **adversarial pass** | waits for the fixture |
| 32 | EW-E6: Extract attachments | 23, 30, 20 | — | **adversarial** addendum | — |
| 33 | RI-S4: the crash-safe write path and deferred deletion | 16, 17, 9 (sequenced on `commitInOrder`) | **RR-1** | **adversarial pass** | **ESC-2, ESC-3** |
| 34 | RI-S5: never degrade | 33 | **RR-1** | **adversarial pass** | **ESC-2**, via 33 |
| 35 | RI-S6: summary parity and README | 34 | — | **friction review** | via 33 and 34 |
| 36 | RI-S7: the inventory backfill (Q6) | 34 | — | **adversarial** addendum (untrusted earlier zips) | via 34 |
| 37 | LC-D: redaction that removes | 2, 18, 16 (and 33 if it lands first) | **RR-5** | **adversarial pass** (a residue hunt) and **friction review** | **ESC-4** |
| 38 | MF-S7: repair (J6) | 20, 21, 25, 26, 31, 33, 34 (31 unconditionally, MC43; corrected after check) | **RR-4** | **friction review** and **adversarial pass** | **ESC-2** (the joint R2 text), via 33 and 34; waits for 31's fixture |
| 39 | MF-SD: the Desktop repair action (Q6 / OR1) | 38 | — | **friction review** | **ESC-6**, and via 38 |
| 40 | FI-S2b-id: the F33 identity fold | 19, 12 | — | **adversarial** addendum | **ESC-1** |
| 41 | FI-SGc: capture's nextLink host/path pin in `Folders` and `Messages` (formerly EW-E1c; MA-505 and MA-314's capture foreign-host case) | 4 | — | short **adversarial** note (shipped capture path) | MA-443's nextLink-shape sub-case recorded (XC25), unless the owner answers **ESC-7** first |

**Not built:** LC-A-W (OQ-1). EW-E1c's content is fault-isolation's FI-SGc (row 41, J4).

## 10. Escalations (these go back to the owner)

| Id | The question for the owner | Recommended answer | Safe interim | Blocks |
|---|---|---|---|---|
| **ESC-1** (J8) | Accept the named identity drift for messages the v1 parser cannot parse, by regenerating MA-330's golden for those inputs only, with a README note? | Yes | S2b ships without the fold; the constant stub stays; the collapse is a skip encoder citing R1 | FI-S2b-id |
| **ESC-2** (re-capture Q5 / OR5, plus the joint R2 text) | Accept these amended guards? R2 as one joint text with mime-fidelity's repair clarification. R13's new deletion cases: superseded copies, and the redacted stem's `-vN` family, which is also MA-227's `-vN`-family clause. R13's N2 adoption exception: a record whose recorded file is missing is re-pointed to its unowned newer copy instead of pruned. S24's removal once the new manifest is durable. The MA-66 and MA-87 covering-test changes. Design-incremental-completeness B2, superseded | Accept. Those tests encode F46 and N2 as today's behaviour, and adoption heals archives N2 already damaged | RI-S1 lands only the additions and encoders; RI-S2 lands without the `-vN` case and without N2 adoption (an N2 victim is pruned as today; MA-361's two twins are skip encoders); RI-S3 lands; no existing text or test changes | RI-S4..S7, MF-S7, MF-SD, and RI-S2's two held cases |
| **ESC-3** (re-capture Q1 / OR1) | How should a changed re-capture keep names and page state? (a) move to `-vN` only on a content or renderer change, so pages keep their capture-time read, flag and category state; (b) stable names with a per-record intent journal, where `verify` accepts either side's digest; or (c) replace in place and accept the §1.7 false alarm | (a), the review's recommendation, asked once RI-S1's §1.7 encoder has run (RC33) | today's write path stays; RI-S4 does not start; R27 lands without a file promise for repairs (J6), and R5's write-beside sentence lands only on answer (a) (J7) | RI-S4..S7, MF-S7, MF-SD |
| **ESC-4** (local-confidentiality OQ-8, plus R13's classifier and its added deletion cases) | Accept R13's amendment, that only an operator's deletion is a redaction and that a moved, renamed or unreadable file keeps its record? Accept R13's added deletion cases: the index scrub, stale-page and empty-folder removal, the stale-snapshot sweep, and the manifest-driven gone set (records with no index row whose page is missing)? And which intent rule: always, beyond a threshold (N = 25, as revision 2 describes) or never? | The threshold, with the R13 amendment and its deletion cases | plain `reindex` keeps today's behaviour; LC-D does not start, and its catalog-first PR lands none of these | LC-D |
| **ESC-5** (workspace Q13 / OR4; new, corrected after check) | Does the extraction workspace supersede the approved desktop design's P7, "Export a copy (extract mbox/eml)"? P7 carries out your feature-gap ruling for the desktop, and superseding it drops a dashboard mbox export | Yes: one export surface in the dashboard, with `extract -format mbox` kept as the CLI path for mbox | EW-E3 ships the workspace and its Export sidebar group without declaring P7 superseded; P7 stays an approved, unbuilt desktop item; the workspace's §3, OW4 and XC39 do not call it superseded | nothing: EW-E1..E6 proceed on the interim |
| **ESC-6** (mime-fidelity Q6 / OR1; new, corrected after check) | How does with-Message-ID Graph mail get repaired on MailArchive Desktop? (a) a Desktop action, "Repair messages archived by an earlier version (re-downloads your mailbox)", whose full walk writes only records where the repair fires, which amends R2's "`full` re-exports all" for that walk; (b) rely on Q3's `.eml` repair, which a default Desktop archive (no `.eml`) cannot use; or (c) accept the gap and disclose it | (a). If you would rather leave R2's full-mode clause untouched, MF-SD can instead be designed as a new, separately named repair walk, which is then a pure addition | MC16/MC37's disclosure stands; the gap stays open, not accepted; MF-SD does not start | MF-SD |
| **ESC-7** (J4, workspace XC25; new, corrected after check) | May capture's nextLink host/path pin ship before MA-443's real-tenant sub-case has recorded Graph's nextLink shapes? XC25 gates it so that a harmless Microsoft-side variation cannot fail every scheduled backup (L8-2). This repo has no lab tier, so the gate may never clear, and until it does a foreign nextLink in capture's listings would receive the bearer token | Yes, in part: ship a host-only pin for capture now (scheme, host and port, after XC25's normalization), which keeps the bearer token on the base host; the path-prefix part waits for the recorded shapes | FI-SG ships the repeat set and page cap in every loop, and the full pin on the workspace loops only; capture's loops keep today's host behaviour; that exposure stays an open gap, not accepted, encoded by MA-314's capture foreign-host case as a skip encoder | FI-SGc (row 41), until MA-443's sub-case is recorded |

None of the seven blocks the owner's chosen tool. EW-E1..E6 need none of them: ESC-5's
interim lets EW-E3 ship, and ESC-7 holds back only capture's pin.

**Added after the log:** ESC-8 and ESC-9, raised by RR-6, are in §13.4. Neither blocks
anything.

## 11. Follow-ups for other owners

**The kit owner** (`assureblock/`, which is not edited here). Three strengthenings of the
covers-map gate:
1. **A duplicate-id check:** an R or S id defined twice, or an MA row id that appears twice.
2. **A definition check:** every R id the catalog mentions has exactly one `- **Rn —`
   definition line. Today any mention makes an id valid.
3. **A row-shape check:** any `| MA-` table line that the spec-row regex cannot parse fails,
   instead of being skipped. Examples are `MA-401a`, or a tier written `U + L`.

**The design authors.** This commit folds every ruling into its design with a minimal edit
marked `[ruling <id>]`, marks each open question RULED or ESCALATED with a pointer here,
rewrites the provisional R, S and X ids to §8's ids (property text, Covers cells, MA rows'
covers lists), renames the split and overflow MA rows, and keeps each BUILD STATUS line as
"approved with conditions, not built", adding "owner rulings folded". What it folded, and what
each author still owes:
- **Extraction workspace.**
  - Folded: Q1–Q15 marked; the W1 source (§3, §4.1, §4.3, §4.7, §7.4, §7.6 R-o3, MA-531, E4);
    OW4 and XC39 cite fault-isolation FC20 (Q12) without calling P7 superseded (ESC-5); E1
    depends on SG, and E1c's content is fault-isolation's FI-SGc (J4); §4.5 and MA-402 read
    `StatusError{Code, GraphCode, Text}` with `Error()` keeping today's text (J4); §4.13's
    classifier is `graph.Classify` with the copy export's own 404 justification; J3's posture;
    X14 scoped to the dashboard (J7); Q14 closed; F17 to FI-GF17 and the archive-side R4 skip
    encoder (Q15); the deployment switch (Q6).
  - Owed: W1's text completed as RR-3 finds; each split row's two halves written as two
    catalog rows at the catalog-first PR.
- **Fault isolation.**
  - Folded: OR1–OR10 marked; ids; P9, MA-325 and §4.9 read "exit 1 with the `partial` flag"
    (OR2); §2.1 follows J7; R25 names OR6's opt-in, and §4.5 and §6 describe it; the SG row,
    P3 and §4.8.7 follow J4 (capture's pin is FI-SGc); FI-GF17 is a slice and §3 N1 no longer
    lists F17 (Q15); OR10's stricter sweep rule; the F33 fold is ESC-1; MA-503 and MA-504 rows;
    the rows that span slices (§3).
  - Owed: the remaining exit-3 wording in §4.9, §6 and §7 rewritten when S1b is built.
- **MIME fidelity.**
  - Folded: Q1–Q5 and OR2–OR5 marked; Q6 / OR1 is ESC-6; ids; R27 without a file promise
    for repairs (J6); notes on P1, P11, §4.10, §5 and §6 where J6 changes "in place", "never
    renames" and the rollback text; the R2 clarification waits for ESC-2; MA-343's tier;
    MA-344 and MA-346 read the stem re-capture's commit gives.
  - Owed: §4.10's commit, "In place" and write-discipline text rewritten through re-capture's
    W1–W5 (J6), before RR-4; the strengthening recommended for S1 (OR4).
- **Re-capture integrity.**
  - Folded: Q1–Q11 marked (Q1 and Q5 ESCALATED); ids; the R5 amendment loses the
    fault-isolation stub-re-render exception and lands only on ESC-3 (a); "pending removal"
    (`removal_pending`); the interim S1 scope; S2 without the `-vN` case and N2 adoption;
    MA-227's `-vN` clause with ESC-2; MA-361's two held twins; slice S7 (Q6).
  - Owed: nothing beyond the ESC-2 and ESC-3 answers.
- **Local confidentiality.**
  - Folded: OQ-1..OQ-10 marked (OQ-8 ESCALATED); ids; MA-368's L part is MA-500; §6's
    workspace sentence follows J3; C2's dashboard port follows OQ-9 with CC23's adapted notice;
    A-W is not built (OQ-1); R13's added deletion cases wait for ESC-4.
  - Owed: nothing beyond the ESC-4 answer.

## 12. The check (what the skeptic raised, and what was done)

Every correction's evidence was re-read at the cited lines before it was applied.

| # | Raised against | Finding | Outcome |
|---|---|---|---|
| 1 | Workspace Q13 / OR4 | Superseding desktop P7 reopens an owner ruling (the feature-gap review) | applied: ESC-5; §0 lists the feature-gap ruling |
| 2 | Mime Q6 / OR1 | Option (a) amends R2's "`full` re-exports all" | applied: ESC-6 |
| 3 | J7, re-capture row | MA-227's strengthening includes the `-vN` deletion case that ESC-2 holds | applied: the `-vN` clause joins ESC-2 |
| 4 | J6, J7 (R27, R5) | R27's "files" and "in place" and R5's write-beside sentence depend on ESC-3 | applied: R27 without a file promise; R5 only on (a) |
| 5 | §9 row 38 | MF-S7 must wait for MF-S6 (MC43) | applied: 31 is an unconditional dependency |
| 6 | J4, §9 rows 4 and 15, RR-2 | XC25's gate on capture's pin was dropped; RR-2 too late and without lens 8; EW-E1's note made optional | applied: FI-SGc (row 41), ESC-7, RR-6, EW-E1's note always; MA-402's rewrite folded |
| 7 | §3 and §5 notes | MA-314 and MA-361 span two slices | applied: both listed, with skip encoders |
| 8 | J7 LC row, ESC-4 | LC's R13 amendment adds deletion cases | applied: they join ESC-4 |
| 9 | §9 row 1, FB-1 | Rewording MA-187 narrows its guarantee | applied: MA-187 unchanged; skip encoder only |
| 10 | LC OQ-9 | Dropping CC23's notice makes the remaining residual silent | applied: the notice stays, adapted |
| n1 | §5 RI-S2 | N2 adoption is an exception to R13's prune | applied: adoption joins ESC-2 |
| n2 | J7 FI row | R25 must name OR6's opt-in | applied |
| n3 | Workspace Q15 | F17 still "owned elsewhere" in fault-isolation N1; no FI-GF17 edit listed; the archive-side R4 gap would lose its encoder | applied |
| n4 | W1 | Delegated detail filed under the owner-direct label | applied: split |
| n5 | §9 | RR-3 listed as startable with no text to review | applied: W1 folded into the design |
| n6 | J7 workspace row | X14 is false for `extract -dest` | applied: scoped to the dashboard |

The check found no question missing from §1–§7: every Q, OR and OQ of the five designs and
their reviews appears there.

## 13. Amendments after the log

Added on 2026-10-09, after the log stood. Every item is delegated-judgement (§0). None weakens
a gate, edits a baseline or narrows a catalog row's guarantee.

### 13.1 MA ids (amends §8.4)

| Owner | Block | Status |
|---|---|---|
| RR-2 and RR-3 (extraction workspace) | MA-532..MA-534 (§8.4's reserve), then MA-535..MA-539 | reserved; the ids the run uses are recorded with its result (§13.5) |
| RR-6 (fault isolation, SG) | MA-540..MA-544 | **allocated:** MA-540 (SG's stops on the shipped capture path, through `RunGraph`) and MA-541 (the Graph folder hierarchy: visited set and depth cap), both in FI-SG. MA-542..MA-544 stay reserved to fault isolation |
| g7-Q1 (§13.3) | MA-545 | **allocated:** an export or graph `run_in_progress` is judged by the lock holder's verb (G7) |

The next free overflow id is **MA-546**. An unused id in a reserved block stays with that
block's owner. This is the record that the fault-isolation design's §9 and its §10 SG row wait
for (J1), so SG's catalog-first PR may land MA-540 and MA-541.

### 13.2 Merge order for slices that are already built (amends §9's "How to read the table")

§9's numbers are a valid topological order, not a queue for code that already exists. FX-G8
(row 8) and the g7 branch (row 9: G7-F53 and the rest of `wip/g7-index-status`) merge as soon
as their owed gates pass, after FX-SAN, or before it while FX-SAN's round 3 is still running.
Neither depends on rows 2–7, and catalog changes still land one at a time.
- **Why:** finished, reviewed fixes should not wait on six unbuilt slices. Later slices rebase
  onto a base that already holds them.

### 13.3 Questions raised by the fix-branch reviews (adds to §7)

| Id | Question | Ruling |
|---|---|---|
| g7-Q1 | How is a running export or graph record's `run_in_progress` judged? | By the lock holder's verb, as `2ff682f` did for verify. It is live only while the holder's verb is export or graph, or while the holder names no verb (status's own probe, or a binary older than `c19f429`). Counting those as live keeps a mixed-version install free of a false RED. New row MA-545 (§13.1) |
| g7-Q2 | Does the Windows sharing-violation retry cover the other atomic renames? | Yes. The last-run record, the last-verify record, the history log and the sidecars go through `state`'s `replaceFile`, which gets the same bounded, Windows-only retry. Best-effort writes stay best-effort. MA-399 is extended |
| g8-R2 | What happens to an unreadable descriptor left by a custom-named backup's removal? | It is never deleted, because the tool cannot tell whose it is. The removal prints its path and the remedy, and status keeps naming it. MA-455 is extended. A default-named removal keeps 568c992's rule, restored in `5090706`: there the file goes with the default-named entry. Extending "never deleted" to that case would narrow MA-455's guarantee, so it is ESC-10 (§13.6) |
| g8-Q6 | `55e449c` reads FB-4's name guard as two cross-kind refusals on every OS (a verify under the backup's name, a backup under the verify's name), with no one-verify-per-archive refusal. Is that the right reading? | Yes. A one-verify-per-archive refusal would block the residual's recovery (re-installing under the archive's own derived name) |
| g8-Q7 | Keep `1f26d5c`'s skip encoder for FB-4's residual (a verify `-name` shared across archives on launchd and Task Scheduler) under MA-456? | Keep it. §0 rule 3 asks for an encoder wherever a test can show the open gap |
| g8-Q8 | Should a verify schedule installed after the archive's last verify be exempt from `verify_stale` until two of its periods have passed? | Not adopted. It would flip MA-466's covering scenario (§0, rule 2). `b755a43` stands: the WARN still fires, says the schedule was installed after the last verify and has not run yet, and names a remedy that clears it (wait for the next run, or run `verify` now) |
| g8-Q4 | Should MA-260's `-token-cache` requirement on Windows be relaxed? | No, keep requiring it. Relaxing it would change what an existing covering test asserts (§0, rule 2). It goes on the owner's low-priority list |
| g8-Q5 | Should a legacy verify entry be removed automatically? | No, keep the hint. The name may by now hold another archive's verify |
| g8-R3 | X6 was marked Met while MA-466's encoder was still open | Resolved by building FB-4 (MA-457, MA-458), which turns MA-466's encoder green. X6's text is not edited |
| FB-3 note | Does round 3's medium finding (a `<plaintext>` or script escape inside a noscript ends the page early) reopen FB-3? | No. Option (b), the in-place noscript retry, stands. The finding is fixed inside it by failing closed on page structure: the assembled page is re-parsed in both scripting readings, its header and body must be live children of `<body>`, and otherwise the page falls back |
| san-Q1 | `c3b26a6` goes past the review's ask in two ways, both found by probing. The in-place retry also runs when the first pass's page check fails, so a harmless head noscript that leaves a `<b>` or `<div>` open for readers with scripting off renders instead of falling back. Stray noscript end tags under SVG and MathML integration points are refused. Keep both? | Keep both. The first renders more harmless mail while every page still passes the structure check (header and body live in both readings). The second only refuses more. Round 3 attacks both by name |
| san-Q2 | Who owns the two R8 shapes left after `c3b26a6` and encoded at `829f51d`: a `<plaintext>` at the end of a mail swallowing the page's closing tags, and the flattener dropping a noscript's tags past depth 480? | FX-SAN. Both go into round 3's scope and are fixed there if the fix needs no design change, which turns the encoder green. Otherwise they stay skip encoders (open gaps, not accepted) and go on the owner's list |

### 13.4 RR-6: the SG re-review (amends J4 and §10, and completes §9's RR-6 row)

- **Run.** Lenses 3, 6, 7 and 8 ran on SG as J4 defines it, before FI-SG code, on
  `wip/rr6-sg` (base `aed7e5b`). The addendum ("Addendum: RR-6") is filed in
  `docs/review-fault-isolation-predesign.md`. Its conditions are folded into
  `docs/design-fault-isolation.md`, each marked `[RR-6 RR6-Cn]`. The commits are `5f060db` and
  `5cc54bc`.
- **Verdict: GO_WITH_CONDITIONS.** There were 29 findings. The skeptic confirmed 15 (1 blocker,
  3 major, 11 minor) and refuted or merged 14. The rescue pass dissolved the blocker and two of
  the majors with mechanism changes and narrowed the third (L3-2). It also raised three hidden
  blockers, and each is now a condition.
- **Conditions folded:**
  - RR6-C1: typed stops. `MIMETo`'s stops return `*graph.TimeoutError`, and `RunGraph` decides
    "cancelled" from its own context.
  - RR6-C2: capture's interim 10-minute overall cap.
  - RR6-C3: the size-cap figure is confirmed from Microsoft's documentation, at least 256 MiB.
    A response whose `Content-Length` exceeds it is refused before the body is read.
  - RR6-C4: FC10's 30-minute bound is stated with its condition.
  - RR6-C5: every guard stop is typed, never a quiet end of listing (MA-540).
  - RR6-C6: `Folders` gets a visited set and a depth cap of 300 (MA-541).

  A fresh-eyes check failed RR6-C6 and found two gate weakenings in the fold. The fixer
  reverted both and raised them as owner rulings RR6-OR1 and RR6-OR2 (ESC-8 and ESC-9, below).
- **J4 amended.** Its Download and Rows bullets carry a pointer here.
  1. **Overall cap.** Until FI-S3, capture's `Client.MIME` passes `MIMETo` an overall cap of
     `Config.MIMETimeout`, today's 10 minutes. S3 raises it to the 2 h `MIMEMaxDuration` in the
     same slice as FC10's `Retry-After` clamp, the breaker's time bound and the run-level
     breaker. The copy export keeps J4's 2 h from E1 (RR6-C2).
     - **Why:** between SG and S3 the 2 h cap would reach capture without the clamp and the
       breakers. A throttled or trickling mailbox could then hold the archive lock for up to
       2 h instead of 10 minutes, about 80 h for a 40-mailbox job, and later scheduled runs
       would be refused.
  2. **Size cap.** The 200 MiB figure is replaced. Before SG code, SG confirms from Microsoft's
     documentation the largest `$value` an Exchange Online item can produce, stored items
     included. The shared cap is set at or above that figure: 256 MiB, or higher if the
     confirmation requires it. A response whose `Content-Length` exceeds the cap is refused
     before its body is read (RR6-C3). The cap reaches capture only at a figure the
     confirmation bounds; otherwise ESC-8's safe interim applies.
     - **Why:** 150 MiB of attachments comes to about 205.3 MiB once base64 and CRLF are
       added. A 200 MiB cap would fail an item that today's code archives, on every run before
       S3, and leave it a permanent gap after S3.
  3. **Header timer.** J4's "own response-header timeout of at least the idle window" is
     `MIMEHeaderTimeout`, default 2 minutes. It times each HTTP attempt from send to headers,
     never during `get`'s `Retry-After` sleeps. `MIMETo`'s own stops are typed, and none is
     read as a cancel while the caller's context is live (RR6-C1).
  4. **Added to SG.** Capture's `Folders` keeps one visited folder-id set across the call and a
     depth cap of 300, confirmed against Microsoft's documentation before SG code (RR6-C6).
     - A higher documented figure becomes the cap. With no figure, `Folders` gets no depth cap
       and the over-depth case is a skip encoder.
     - A revisited folder is skipped. A level past the cap fails the mailbox with a typed error.
- **Open conditions, not accepted.** Each is stated in the design next to its claim, and
  nothing waits on it:
  - with a healthy control request, FC10's bound is per item only, and known-poison items pay
    their timers on every run (RR6-C4);
  - the breadth of capture's folder walk (ESC-9's interim);
  - the window between a documented rise of Microsoft's limit and the release that answers it
    (ESC-8's interim).
- **Follow-ups:**
  - RR-6 edited the extraction design: the six 200 MiB figures, capture's interim 10 minutes,
    §4.5's redirect bullet and MA-528, each marked `[fault-isolation RR-6]`. RR-2 had already
    started on `aed7e5b` and does not see those edits. A delta check of them (lenses 6 and 7)
    therefore runs before EW-E2 code, after RR-2 and RR-3 merge.
  - EW-E1's short adversarial note gains a fourth question: does `CheckRedirect` cover SG's
    `$value` client, and does `Client.Timeout` stay off it?
- **Unblocks:** FI-SG (§9 row 4). The gate it owes after build is still the adversarial pass
  (network input).

| Id | The question for the owner | Recommended answer | Safe interim | Blocks |
|---|---|---|---|---|
| **ESC-8** (RR6-OR1) | Where Microsoft's documentation gives no figure that bounds a stored item's `$value`, or Microsoft later raises its limit past a shipped cap, may capture carry the size cap anyway? If it does, an item above the cap, which today's uncapped read archives, fails its mailbox on every scheduled run before S3 and is a terminal gap after it. If it does not, capture's download stays bounded by time only, as today | No. Capture keeps no lower cap than today's until a documented figure bounds it, and the copy export alone carries the cap from E1. After a documented rise, the next release raises capture's cap, or drops it under the same rule. You accept the window between the rise and that release, in which an over-cap item fails loudly, naming the item and the cap | The recommended rule, without accepting the window, which stays an open condition. While capture passes no cap, the missing cap is an open gap, not accepted, and MA-540's over-cap case and MA-314's `fetch-too-large` gap case are skip encoders. The same rule holds for the depth cap | nothing |
| **ESC-9** (RR6-OR2) | Should capture's Graph folder walk carry a total-folder cap? A `childFolders` response that invents fresh folders at every level is bounded per loop but not in total, so a misbehaving endpoint can keep one walk going for as long as it answers. Only a cap at or above Exchange Online's real per-mailbox folder limit cannot fail a healthy mailbox | Yes, if Microsoft documents a per-mailbox folder limit: a cap at or above it, confirmed like the size cap, with an at-cap twin. If none is documented, no cap, and the case stays open | No total-folder cap. The case is an open gap, not accepted, encoded by MA-541's breadth case as a skip encoder citing R10 | nothing |

### 13.5 RR-2 and RR-3: the extraction workspace re-review (amends J3's mechanism, and completes §9's RR-2 and RR-3 rows)

- **Run.** Lenses 3, 6 and 7 ran on revision 2 with ruling J3 and the W1 second archive
  source, with RR-2 and RR-3 folded into one run as the design allows. Lens 6 had two finders,
  one for E2's write path with J3 and one for W1. The run was on `wip/rr2-rr3-workspace`
  (base `aed7e5b`). The addendum is filed in `docs/review-extraction-workspace-predesign.md`,
  and its conditions are folded into `docs/design-extraction-workspace.md`, each marked
  `[RR-2 RR2-Cn]`. The commits are `c2ba9eb` and `2a82bdb`, merged as `1f588de`.
- **Verdict: GO_WITH_CONDITIONS.** There were 33 findings. 26 were confirmed (8 major,
  18 minor) and 7 refuted or merged. The rescue pass dissolved the 8 majors with mechanism
  changes and raised 6 hidden blockers, each now a condition. A fresh-eyes check failed
  RR2-C2, RR2-C3, RR2-C4 and RR2-C13, and the fixer re-fixed them. No gate weakening was
  found, and no owner question was raised.
- **Conditions folded:**
  - RR2-C1 and RR2-C2: the run record's sealed line carries the final status and counts.
    One idempotent seal-and-finish step ends normal finalization, Finish as partial and
    Check and finish. A record is deleted only after its ledger line exists.
  - RR2-C3: only marker-and-seal outcomes are terminal. "Folder not found", a probe timeout
    and "couldn't confirm" are re-probed, and "This folder is gone" is a terminal choice.
  - RR2-C4: the root's identity is a tuple (the run id as a content nonce, then the
    volume's file identity). The W1 source is bound by its path plus per-message fixity.
  - RR2-C5: free bytes are read before every message, and `MIMETo`'s `maxBytes` is the
    smaller of the size cap and the free-space budget.
  - RR2-C6: the hand-over posture line (J3, below), with new row MA-532.
  - RR2-C7: Finish as partial is offered for every unsealed run whose root is confirmed.
    A run-record or journal write failure stops the run.
  - RR2-C8: the time breaker counts awake time only, and the run holds off system sleep
    in-process.
  - RR2-C9: an archived page (with no `.eml`) keeps `.html` only if it begins with the
    renderer's exact prefix and carries no other hostile meta. Otherwise it is quarantined
    as text. This moves from E5 to E4.
  - RR2-C10: final placement is a no-replace rename relative to a directory handle opened
    through the root, never a path-based rename.
  - RR2-C11: another archive folder's manifest is read through a 1 GiB limit, and its
    `search.db` is opened read-only and immutable, with SQLite's defensive settings.
  - RR2-C12: a launch sweep adopts run records that no journal line names.
  - RR2-C13: the destination-inside-archive refusal compares file identities as well as
    paths, and fails closed where identity cannot decide.
  - RR2-C14: an export from another archive folder saves no setting, so MA-531 holds as
    written.
- **MA ids (amends §13.1).** MA-532 is allocated (the realized posture check, RR2-C6, slice
  E2). MA-533..MA-539 stay reserved to the workspace. The next free overflow id stays MA-546.
- **J3 amended (mechanism only).** Its Ruling bullet carries a pointer here.
  - **Change:** the workspace preview's posture line uses a hand-over form of `Posture`,
    computed over the security descriptor a new child of the destination would inherit, for
    the root and for a file.
    - It names each principal with read access.
    - It reads "couldn't check who can read this folder" when the descriptor cannot be read.
    - It is recomputed at Start, which refuses with R-w2 if the result is broader.
    - It is checked again on the realized root and marker before the first message.
  - J3's decision is unchanged: inherit the destination's ACL; POSIX 0700/0600; the
    protected DACL only for archive roots and the tool's own state directories.
  - **Why:** L6a-1 (major). `Posture(dir)` reports Open only when a broad SID can read the
    root, so an inheritable grant to one named account would read "only you" while the
    export inherits read access.
- **Follow-ups:**
  - `extract -dest`'s use of `Posture(dir)` has the same defect. The local-confidentiality
    design now states it as an open gap, not accepted, owed by LC-A (§9 row 2).
  - L3-12 (what "this host" means; `os.Hostname` is not stable on macOS) is an open note owed
    in E3.
  - **RR-2b, before EW-E2 code:** a fresh-eyes re-verification of the four re-fixed
    conditions (RR2-C2, RR2-C3, RR2-C4 and RR2-C13), together with §13.4's delta check of
    RR-6's edits to the extraction design (lenses 6 and 7). They run as one small design
    check.
  - The merge reconciled one sentence the two re-runs wrote separately. RR2-C5's
    "min(200 MiB cap, budget)" now names the shared size cap that SG confirms.
- **Unblocks:** EW-E2 once EW-E1 is built and RR-2b passes; EW-E4 (RR-3) once EW-E2 and
  EW-E3 are built.

### 13.8 RV-10 built (2026-10-10), delegated-judgement

RV-10 (status, desktop and GUI visibility of messages shown as source) is built on the
sanitizer's free ids MA-494, MA-495 and MA-497..MA-499. Two delegated rulings:
- **Where the count comes from.** From the manifest's per-message `body_fallback` reason, not from
  new last-run fields: it persists across incremental runs (the gap the review named: run 2
  printed nothing), and it adds no record field. It is a note with code `shown_as_source`, never a
  WARN or RED, so a healthy backup stays GREEN. `status -json` gains an optional `shown_as_source`
  key (version unchanged). The desktop and GUI reuse `health.Summary`.
- **Wording.** The plain-words line ("N message(s) are shown as plain text instead of formatted
  pages … Nothing is lost … Nothing to do.") follows the what / why / remedy rule; X6 and X10 only
  require that the apps mirror `status`. Not done, and left to a later slice: the GUI export
  summary and the desktop capture result message do not mention the fallback count.

### 13.9 EW-E1 built and merged (2026-10-10)

EW-E1's short adversarial note (rulings §9 row 15) ran: 12 found / 3 confirmed, fixed, report in
`docs/review-ew-e1-adversarial.md`. All four of the note's questions are answered yes by tests:
CheckRedirect refuses a cross-host redirect on the workspace clients and on SG's `$value` client;
`Client.Timeout` stays off `$value`; the host/path pin refuses only a foreign host or path (not a
healthy tenant's variation, with XC25's normalization); the identity-safe write-back never writes
one account's token under another's identity. Delegated detail rulings: CheckRedirect compares each
hop with the origin its request was first sent to; the workspace read paths refuse a client that is
not the delegated `/me` one; the "honored" flag travels as `PhysID = ID` on each ref, so **E2 must
record it from the first page's refs** (an empty first page cannot carry it).

| Id | The question for the owner | Recommended answer | Safe interim |
|---|---|---|---|
| **ESC-15** (EW-E1 AI1, XC7) | The token write-back is serialised within one process only. A sign-in in the dashboard process during a scheduled run's load-to-write window can be overwritten, forcing a re-sign-in (never a token under another identity). Add a cross-process lock on the token file and each vault backend? | Yes, in the slice that owns sign-in (LC-C2 or E3), because it needs a lock per vault backend | Left as is; `TestTokenWriteBackCrossProcessSignIn` is a skip encoder (MA-515, XC7) |

### 13.10 Product scope: single-user computers only (owner-direct, 2026-10-10)

The owner stated that the software is for single-user computers only. Consequences, delegated:
- **LC-A (private at rest, §9 row 2) is deferred.** Its exposure, other local accounts reading the
  archive's files, does not arise on a single-user computer. It stays designed and approved with
  conditions, not built, and is not dropped: the owner can reopen it.
- **Not touched by this:** LC-C1 is merged (its Host check and `/files/` allow-list defend against
  web pages, not other accounts). LC-C2 stays in the build order because the extraction workspace
  depends on it (§9 row 24: E0 = rows 3 + 11); whether its per-account session machinery can be
  slimmed for a single-user scope is a design question for its own slice, not decided here.
- **No gate is reduced:** no catalog row, test or baseline changes with this ruling. The co-account
  personas in the designs become out of scope only if the owner says so for a specific row.

### 13.11 FI-S1a built and merged (2026-10-10)

FI-S1a's friction review (rulings §9 row 5) ran: 18 cells, SHIPPABLE after 18 fixes with prove-fail,
`docs/review-fi-s1a-friction.md`. No capture records a ledger gap yet (S2a and S3 produce them), so
the walk seeded archives and ran the real binary. Delegated rulings: the stuck counter rises at most
once per UTC day; the remedy table lives in `internal/state` with `app.GapRemedy` as the exported
entry; the no-Message-ID stuck sentence (FC23(4)) needs a field the design does not name, so **S3
adds an additive ledger field for it** (additions are accepted, §0 rule 1); its test stays a skip
encoder. The remaining friction backlog items are owned by S1b (F13, F15, OR1..OR3), S2a (F19) or
documented (F17, F18).

| Id | The question for the owner | Recommended answer | Safe interim |
|---|---|---|---|
| **ESC-16 RULED: ALLOWED by the owner, 2026-10-10 ("I allow the change")** (FI-S1a friction R5) | Rows in the verification report for an unreadable or unparsed record carry only a bare code ("unparsed", "unreadable:attachments") with no why or what-to-do. Giving them one means loosening `TestReportKindsForReadGapLabels` (MA-327), which pins that detail to the bare code. Allow that change? | Yes: a bare code leaves the inheritor with no remedy, and the test would be tightened to the new wording rather than dropped | The banner and README.txt promise a why and a remedy only for ledger rows; MA-327 unchanged |

### 13.12 EW-E2 built and merged (2026-10-10), delegated-judgement

The copy engine (`internal/app/copyexport*.go`, `util.FreeBytes`/`FoldKey`/export naming pass,
`graph.Client.ForceRefresh`, `graph.TooLargeError.Declared`) is built catalog-first and merged. Its
conformance check found four gaps, all fixed with tests: a 403 on a message body was listed per
message instead of stopping the run with R-c (MA-517); the manifest hash and size were not checked
against the file (MA-419); the size cap was a second literal copy of the shared cap (MA-426); the
Windows junction test was missing (MA-417). The adversarial pass is COMBINED with EW-E3's (§9 row
23) and runs after E3. Open skip encoders, each citing its row: MA-419 Readable HTML (E5), MA-427
archive-root-gone (E4), MA-427 retry preview and offered labels (E3), and R4 for the archive
writer's F75/F91/F111 names. R-f/R-f2 (MA-414/434) are E3's per design §10, though the E2 task text
listed them. The Windows legs (placement, DACL, power, MA-532, MA-417 junction) are compile- and
vet-checked only and need the Windows CI host. The 60,000-message scale test takes about 55 s under
`-race`.

### 13.13 LC-C2 built and merged (2026-10-11), delegated-judgement

Per-account surfaces built whole (nothing slimmed, §13.10) and merged: sessions, `/s/<P>/`, relative
URLs, `/_open`, link delivery, the instance lock, file and handoff, the ephemeral dashboard port
with the former-address notice, the security copy and packaging. Conform fixed two gaps: `serve`
printed a one-time link to a redirected log (CC5), and two covers markers (R12, S41) lost in a test
replacement were restored. **Owed before ship, co-equal blockers: the adversarial pass and the
friction review** (neither has run; X9/X11/X12 stay "Partial"). **J7:** two desktop tests
(`TestDesktopReconnectsWhenAlreadyRunning`, `TestDesktopNoBrowserSuppressesOpen`) were replaced by
stronger verified-handoff tests; the adversarial pass must confirm that is no weakening. MA-377
stays lab-pending; MA-384's workspace sign-in half is a skip encoder closed by E3.

The build asks, as one owner decision (single-user scope): would you simplify C2? **Q1** the
lock/instance file/HMAC to a plain "already running" check. **Q2** the marker cookie and three link
pages to one "already used" page. **Q3** the secret `/s/<P>/` prefix and path-scoped cookie to a
host-wide cookie plus Host check and CSRF. **Q4** the two tokens and the macOS stdin opener to one
token and plain `open`. **Q5** `serve -link`, its instance file and the Enter loop. **Q6** the
session count and account banner. **Q7** the 8097 former-address notice (CC22's upgrade part
stays). **Q8** the "other accounts" wording to "web pages". Recommended: **keep as built** until the
adversarial pass and E3 are done, then simplify as a deliberate change with rows amended. Any
simplification amends catalog rows, so it needs your word (§0 rule 2). E3 builds on C2 as built.

### 13.14 EW-E3 built and merged (2026-10-11), delegated-judgement

The first shippable workspace is built and merged: the Extraction workspace and Export activity
pages, selection with row handles, preview, start and cancel, progress and history with the
background reconciler, retry and retry chains, Finish as partial, Check this export and Check and
finish, the Microsoft 365 card and sign-in slot, the picker, Create folder, serialized settings, the
deployment switch (`-no-workspace`, Windows `DisableExtractionWorkspace` policy) and the Export
sidebar group. Source is Microsoft 365 only, output original EML plus manifest, Readable HTML hidden.
Conform fixed seven gaps (journal text not stripped of control characters, Start's destination
re-check unproven, Open folder argv, a damaged file on Finish as partial, R-i wording, the FAT
warning, the moved-or-replaced re-probe), all with tests. Closed here: E2's skip encoders for the
retry preview, MA-430, 437, 438, 448 and 521 halves, and MA-384's workspace sign-in banner.
**Owed before ship: the combined adversarial pass (E2 write side plus E3) and the friction review.**
X13 stays Partial; X14 and X15 are Partial, not Met, until both gates close.

Owner decisions (2026-10-11): (1) **RULED: keep the card.** The Overview sidebar's Export group
jumps to a card with the two links; MA-302 is unchanged. (2) **RULED: defer.** The MSI property
for the deployment switch is not built; only the `-no-workspace` flag and the Windows
`DisableExtractionWorkspace` policy are. It stays on the list and is built if MSI fleet deployment
is ever needed (it would also need a Windows lab check). Skip encoders remaining:
ESC-15's token lock, E4's archive-root-gone, E5's Readable HTML, R4's archive-writer names. The
Linux mocked-zenity argv test and all Windows legs have not run here.

### 13.15 The extraction workspace's adversarial pass and friction review (merged 2026-10-11)

**Adversarial pass** (E2 write side plus E3, `docs/review-extraction-workspace-adversarial.md`): 30
found, 21 confirmed, 19 fixed with tombstone tests. Notable: file names from 4-byte letters
exceeding 255 bytes; a planted FIFO wedging the seal; a folder-parent cycle overflowing the stack;
Open folder and Check judging a folder other than the one probed; sign-in saved out of order with
a live write-back; a retry repeating another part's messages; History rescanning every poll. **J7
confirmed**: LC-C2's two replaced desktop tests lose nothing the originals asserted, and the old
"any bare listener is the instance" behaviour is deliberately reversed. Two owner decisions, each
a skip encoder, **recommended: defer** (single-user): **EW-AI4** (Windows second logon session: a
second engine reconciles the first's LIVE export as interrupted; the fix is a per-run liveness
lock and new wording) and **EW-AI7** (Linux Open folder: `xdg-open` could launch a file swapped in
within milliseconds; a trailing separator on the argv closes it but changes MA-430's pinned argv).

**Friction review** (`docs/review-extraction-workspace-friction.md`): 25 cells, 31 findings,
high-friction cells 6 to 1. Verdict **HIGH_FRICTION_NOT_SHIPPABLE**: the one cell left (OW-19) is
the browser click-through of the three page scripts, keyboard-only and with a screen reader, which
needs a real browser and assistive technology. It is the owner's or a lab's check and gates ship.
F15 (no date range in the selection; one folder over 200,000 messages cannot be exported) is an
owner question, recommended with E4.

The two branches both changed retry-chain handling and merged textually but not in behaviour;
three tests failed after the merge and were reconciled in `8cffbcf` (failure lists now share the
set's one source; the card failure list is cached; one fixture step reordered with no assertion
loosened). Clean-clone suite, vet on three OSes and gofmt are green.

### 13.16 MF-S0 built and merged (2026-10-11), delegated-judgement

Catalog first (R26, R27 with the J6 text, S49), the fidelity corpus (`testdata/mime`, 110 messages)
and the identity and content goldens, recorded at the **v0.7.1** release tag (066f86d; the design
names no tag). MA-330..349 are skip encoders, each citing R26 or R27 and naming the slice that turns
it green; MA-343 is tier L, pending. No production code changed; the R2 clarification is NOT added
(J7, ESC-2). Conform: CONFORMS. Delegated: goldens were built with go1.26.9 and the tag's own
`go.mod` (the branch's newer x/text reproduces them byte for byte); `.gitattributes` marks the
corpus `-text` so CRLF bytes survive on Windows CI. **Open:** (1) MC7 asks for one corpus item
that makes the v1 parser panic, and none was found (two fuzz runs, about 14 million executions),
so MA-330's panic case stays a skip; the owner may rule the constructed stub enough. (2) R27 cites
R28, which does not exist until RI-S1 lands it. (3) MF-S1 must point `corpusV1Parse` and
`corpusV1Panics` at the frozen v1 parser so the goldens keep pinning v1. (4) OR4 residual: the
goldens used the go1.26.9 stdlib, not the release's toolchain; MF-S1's MA-331 closes it.

### 13.17 FI-S2a built and merged (2026-10-11), delegated-judgement

PST (and `-outlook`) isolation and healing: per-row and per-sub-folder loops with go-pst's gates kept
(differential test, v6.0.2 pinned, `0x0FF9` read on the fixtures), structural breakers, visited and
depth guards (S47), zero-content failures as ledger gaps, the local run's classification (run-fatal
volume errors, unwritable folders, a 50-write breaker, fault-driven checkpoint), `-outlook` export
skips as gaps, and the legacy-placeholder re-class. Every new gap kind has its why and remedy
(MA-327). Conform: CONFORMS. **Owed: the adversarial pass** (untrusted PST parsing, exporter write
paths). MA-504 stays tier L, pending. Skip encoders remain for the Graph and FI-S2b halves (MA-316,
MA-318, MA-323), and R1 for N9's corrupted `MessageCount` and the raw-stub identity (ESC-1).

Owner decisions raised by the conform check, none blocking, recommendations in brackets:
1. **Interim hole between S2a and S2b.** `mbox.go` still mints the constant "(unreadable message)"
   stub for a MIME-panicking mbox, maildir or Graph message; the re-class then hides that loss from
   the partial and Not-captured surfaces on the next run, and no ledger gap exists until FI-S2b.
   [Land FI-S2b before any release, or restrict the re-class to records older than the build. The
   branch is local and unreleased, so nothing is exposed meanwhile.]
2. **F19 and N4/R7** (the `.pst` open error repeating its path; one failing file's record): §13.11
   assigns them to S2a but the design's S2a row does not list them, and the folder-link remedy needs
   S2b. [Treat as FI-S2b's.]
3. **N9 stdout capture** was not built: a process-global stdout swap would swallow other writers
   (the device-code prompt). [Accept and state it in the design: go-pst's stray prints are visible.]
4. **MA-316 row wording:** the mechanism emits an aggregate plus one folder-partially-read (two
   entries) where the prose says one. [Tighten the prose.]
5. If mbox, maildir and Evolution walks should close unobserved reader-stage gaps before FI-S2b,
   FI-S1a's `TestRootBannerAppearsFromEveryCaller` seeds a gap that would be deleted: the owner's call.

Load-sensitive tests: `TestCopyExportTransportOutageAndTheBreaker`, `...TwentyFiveServerErrorsAreRU2`,
`...ASuspendRestartsTheWindow` and `...ScalesToSixtyThousandMessages` fail on a busy machine (several
workflows running) and pass alone three of three. The 60,000-message heap bound (40 MiB) sits at its
limit under load. [Keep as is; revisit if CI flakes.]

### 13.18 EW-E4 built and merged (2026-10-11), delegated-judgement

The archive source is built: the Source control offers Microsoft 365, this archive, and **another
archive folder, read-only** (W1). Includes the archive adapter with the redaction belt, the component-wise
no-symlink walk and one-hard-link check, the archived-page gate and quarantine (moved from E5, RR2-C9),
`index.ListFolder` read-only with the Rebuild lock, the tree from the manifest with per-store mailbox
labels, the Settings-save guard (MA-523), R-o, R-o2, R-o3, R-o4 and R-l2, no archive lock beside a capture
(MA-510), and a retry that re-resolves against the archive as it is now. E3's skip encoder for MA-427's
archive-root-gone half is replaced by a real test. Conform fixed nine gaps, each with a test (a page too
large for the gate, the Rebuild lock through the real job, the M365 export leaving an archive
byte-identical, the manifest cap checked before any allocation, among them). **Owed: the adversarial
addendum and the friction re-run (§7.4 inheritor cell).** Delegated: records whose recorded file is
missing are skipped, not served from the archived page; an archived page over 32 MiB is quarantined unread;
a typed another-archive path must be absolute; the archive snapshot is released after 30 idle minutes.
Windows legs (fileLinks, subst) are vet-checked only.

### 13.19 RI-S1 built and merged (2026-10-11), delegated-judgement

Interim re-capture-integrity slice, catalog and encoders only: R28, S50 and S51 land (R27's reference to
R28 now resolves), and nine skip encoders for the confirmed-open gaps: MA-350, 351, 352, 355 (F46),
356 (1.6), 357 (1.5), 358 (N2 and 1.7), 359 (N3), 361 (F48). No product code; the R2, R13 and S24
amendments, R1's added sentence and R5's write-beside sentence are not made (ESC-2, ESC-3). **The
encoders are red without their skip, which confirms these defects exist in today's code:** a full run
over a source whose attachment is now empty deletes the archived attachments zip and rewrites the page
(F46, loses a held attachment); `reindex` deletes message files whose index rows are stale, in the
reproduction three current files and an emptied manifest (F48); a kill right after a changed message is
re-captured makes `verify` report two files modified (1.7, a false integrity alarm, the result ESC-3
asks for); a failed page write leaves the zip and eml no longer matching their recorded bytes (N3). The
fixes are RI-S2 (F48), RI-S4 and RI-S5, which wait on ESC-2 and ESC-3. **Owner attention:** F46 and F48
are data-loss paths in released behaviour, not only in this branch; a note for the release plan.

### 13.20 FI-S2a's adversarial pass (merged 2026-10-11)

Report `docs/review-fault-isolation-s2a-adversarial.md`: 24 found, 12 confirmed, none refuted, 12
fixed with tombstone tests, each reproduced from byte-patched PST fixtures. The serious ones: a store
display name with one DEL byte permanently killed every run and silently dropped gaps (high); a 2-byte
row-size lie in a table header made a store, folder or part of one vanish and erased old gaps; the
lifetime ledger cap was bypassed by per-folder overflow entries; a copy in another folder closed a
failed copy's gap; an attachment-stream panic lost the whole message and leaked a temp file; the local
breaker let a source's own too-deep paths end every later source. `TestCorruptedMessageCountHidesAFolder`
(the R1 gap, formerly a skip) is now a permanent guard. **Open, owner decisions, each a skip encoder:**
(1) **ESC-S2A-1:** a sub-folder flag flipped true to false hides a whole subtree with no trace; go-pst's
gate (kept by FC30) believes the flag, and the healthy fixture itself holds hierarchy tables under
flagged leaves, so reading every leaf's table changes what is archived. [Recommended: accept for now,
state it as a known limit; revisit with a real PST corpus.] (2) **K2**, the interim stub hole
(§13.17 item 1), unchanged: land FI-S2b before any release. (3) **ESC-S2A-2:** the row-size check
follows the MS-PST layout; a real producer that pads rows would get a folder-partially-read gap (no
capture lost); to be confirmed on real Outlook PSTs with MA-504 in the Windows lab. FI-S3 (Graph
isolation) may now start: its dependency, S2a, has its review.

### 13.21 EW-E4's friction re-run and adversarial addendum (merged 2026-10-11)

**Friction re-run** (`docs/review-extraction-workspace-archive-friction.md`): 25 cells, 22 findings plus
four from re-verification; 15 of 20 in-scope findings built, 3 in part, 2 deferred (F14, F20); high-friction
cells 4 to 2. Verdict **HIGH_FRICTION_NOT_SHIPPABLE**, for two reasons only: **OW-9** (a stopped archive
export cannot be finished from a copy of its archive, because retry is bound to the archive path; the
re-binding changes a design rule, so it is the owner's decision; recommended: leave as is, the case is
rare) and **OW-20** (the browser click-through, now also covering the Source control). **Adversarial
addendum** (`docs/review-extraction-workspace-archive-adversarial.md`): 29 found, 20 confirmed, 2 refuted,
18 fixed with tombstone tests. Serious: a manifest store id was resolved on the local filesystem (a UNC
path would leak an NTLM hash on Windows); a 40 KB manifest with deep folder paths took 56 s and 98 GB of
allocation to build the tree; a forged fixity size stopped the run with a false "destination is full";
the Settings guard went blind after "Save and keep the backup"; the archive session held an open root
between requests (blocks ejecting the drive). **Open, owner decisions, each a skip encoder:**
(1) **E4-AO5:** the 1 GiB manifest cap bounds bytes, not memory (a 9 MiB manifest held 208 MiB); the
remedy is a record-count or per-record limit with its own R-o4 wording, or a lower cap [suggested start:
refuse above 2,000,000 records, read the version before the full unmarshal]. (2) **E4-AI10:** a real
attachment named like a legacy sentinel word is worded as an old-version caveat; it needs a
non-colliding label. (3) OW-9, above.

**Merge reconciliation.** The two branches changed the same archive code; the merge (`393f6f0`) kept both
sides and updated four superseded test expectations to the other branch's deliberate behaviour, none
loosened: the Settings guard names the launch location when the field is cleared (truth over the
friction wording); the owner's Rebuild label is the page's own "Maintenance → Rebuild search index";
a count of one reads singular; and removed messages are listed as removed and settled in a retry rather
than offered again. Another archive folder's index advice no longer names the Maintenance button.

### 13.22 FI-S3 built and merged (2026-10-11), delegated-judgement

Graph isolation: `classifyFetch` (token-refresh auth, 403 mailbox-fatal per OR7), per-item gaps with no
record, per-folder listing gaps, immutable-id locators, the `-since` rule, the sweep rules with OR10's
stricter rule (a fault never sweeps a present message gone), legacy-stub adoption, the stub hold-back
(a stub is not written over an archived record), the rate/time breaker with the known-poison exemption,
the control probe and run-level breaker, the `Retry-After` clamp, and **capture's overall cap raised from
10 minutes to 2 hours**. New additive ledger field `CaptureGap.DefersGone` (§13.11) for the no-Message-ID
stuck sentence. Conform: GAPS_FIXED (one assertion gap, MA-312 case c). **Owed: the adversarial pass**
(network input, go-back sweep integrity). **Owner attention:** this slice changes the behaviour of the
`graph` capture path you proved earlier: faults that used to fail the mailbox are now gaps and the
mailbox continues, and the time cap is 2 h. **Existing-test change for the owner to confirm or reject
(rulings 0, rule 2):** MA-540's stop cases and transport-cancel case asserted "before S3 the mailbox fails";
the row's own text says that from S3 a Messages or `$value` stop is MA-314's gap case, so those tests now
assert the gap (reason, scope, class, stop named, no gone events, all Present, mailbox 2 walked, run
recorded failed not cancelled) and keep every other assertion; one subtest's fault moved to the folder
list. Skip encoders remain: MA-314 `fetch-too-large` and MA-540 case 4 (capture passes no size cap,
RR6-OR1/ESC-8), MA-314's foreign-host case (ESC-7), MA-323's parser-generation re-fetch and MA-318's
raw-preserving stub (FI-S2b), and two no-Message-ID stubs with the same Received time still collapse
(ESC-1). Real-tenant behaviour is proven only against the fake server, as the design says.

### 13.23 FI-S3's adversarial pass (merged 2026-10-11)

`docs/review-fault-isolation-s3-adversarial.md`: 25 found, 15 confirmed, 1 refuted, 14 of 16 confirmed
fixed with tombstone tests (the report counts the id-confirmed variant of S3-AI1 separately). Serious:
a close-delimited truncated `$value` was accepted as a whole message permanently; a 200 listing with no
`value` array was read as an empty folder and swept its records gone; a `Retry-After` that overflows
`Duration` bypassed the clamp; a Full run with `-since` downloaded archived messages older than the
window and then swept them gone; a long item id collided in the ledger. **K6 confirmed:** the MA-540
tests adapted by the S3 build keep every original assertion except the sanctioned typed-error to
gap-detail change (one gap closed with a new test), which answers the §13.22 question for the owner;
your confirmation is still wanted since §0 rule 2 makes it yours. **Open, owner decisions, each a skip
encoder:** (1) **ESC-S3-1:** a message restored under a new immutable id whose download fails has its
archived record swept gone (reversible); sparing every record on an unmatched fault would let each new
poison item defer the whole mailbox's gone-detection, and contradicts OR10. [Accept: the sweep is
reversible and rare.] (2) **ESC-S3-2:** a breaker trip's own gaps become known poison in the next run, so
each outage run fetches 25 more items before it trips; the fix changes FC10's known-poison rule (RR6-C4).
[Accept for now.] (3) **ESC-S3-3:** a parse-failing item that reuses an archived Message-ID but has an
unmatched immutable id is held back under an info gap that says nothing is lost, when it may be a
different message; the fix changes P15's hold-back and the §4.1 reason table. [Owner to rule; the
conservative fix is an `#fp` split or a fillable gap with its own reason.]

### 13.24 MF-S1 built and merged (2026-10-11), owner-direct baseline decision

Identity routing: `safeParseMessage` routes identity through a frozen replay of the v0.7.1 parser
(`internal/source/parsev1.go` with its vendored dependencies, licences and a pins file; MA-331 pins every
vendored file, the charset table and `unicode.Version`), `stubIdentityTerms` reproduces today's constant
stub, identity-view stems and the preserved original are carried forward on every write, and
`ContentHash` and the `parse_scheme` stamp (=1) are written on every local write and restored on re-read.
Content still comes from v1: nothing a user sees changes. MA-330, MA-331, MA-347 (identity half) and
MA-346 (a, c) are real tests; MA-346 (b, d) and MA-347's repair half stay skips for MF-S7. **Owner
decision (2026-10-11, "yes - apply the patch"):** the MA-329 baseline `internal/app/testdata/p14-golden.json`
was regenerated with exactly the two additive fields `chash` and `parse_scheme` (46 records; `chash` added
on 34, the other 12 already had one; nothing else moved; the MIME goldens are byte-unchanged). **Owed: the
adversarial pass** (identity drift, mixed versions, stems). **Note:** `TestCopyExportScalesToSixtyThousandMessages`
(a 40 MiB heap bound) measures 25 to 42 MiB and fails now and then under load, with or without S1; the two
extra record fields use a little more memory. It is the owner's call whether to leave the bound, so it is
left unchanged.

### 13.25 EW-E5 built, merge held for an owner decision (2026-10-11)

Readable HTML is built and tested at engine level on branch `wip/ew-e5`: rendering from the existing
sanitizing renderer for both sources (`RenderContext.AttachDirName` and the guarded attachments sentence,
the read-back hash check, MA-525's twelve archive-render goldens unchanged), the 32 MiB predicted-render
bound with original-only fallback (MA-509), and the rule that a missing or damaged readable copy never
makes an original look failed (MA-513: presence, Finish as partial, Check this export, retry). The page
toggle, the default flip to ON, the plan accepting the option and the preview line are NOT built: they
change what two existing tests assert (`TestExportSettingsRoundTrip`, MA-445, asserts OFF and hidden;
`TestCopyExportPlanIsADryRun` asserts the plan refuses the option with a placeholder R-h), which is the
owner's call (§0 rule 2). Both were staged for this flip (MA-445's row says OFF before E5, ON after; R-h
means only an empty selection in design §7.6). The prepared change is the patch
`scratchpad/ew-e5-gate-flip-v2.patch`. [Recommended: confirm.] Same question outstanding for the MA-540
test edits in §13.22 and §13.23.

### 13.26 Owner confirmations of 2026-10-11: E5's test flip and S3's MA-540 edits (merged)

The owner answered "yes to both" to the two open questions of §13.22, §13.23 and §13.25. **(1) EW-E5 merged
(`a74e656`):** Readable HTML is ON by default, the toggle is shown and checked, the plan accepts the option,
the preview carries a "Readable HTML" line, and TestReadableHTMLToggleDecidesTheExport is a real test. The
two staged tests were edited by owner authorisation, exactly as prepared: `TestExportSettingsRoundTrip`
now asserts ON, shown, checked and html=false remembered (it asserted OFF and hidden); and
`TestCopyExportPlanIsADryRun` no longer asserts the plan refuses the option with R-h and gains a positive
twin. Prove-fail recorded for the default, the plan, the page checkbox and the preview `html` key. MA-445's
catalog row now says ON after E5. A readable copy remains a convenience; its absence or damage never makes
an original look failed. **(2) S3's MA-540 edits:** the adapted stop and transport-cancel tests, which now
assert the gap behaviour the row specifies from S3 (the S3 adversarial pass confirmed they keep every
original assertion except that sanctioned change), are confirmed by the owner. Open and unchanged: the
"Which ones?" list for messages exported without a readable copy (they are in the README's first ten and
the manifest; the card shows only a count), a usability residual for a friction pass.

### 13.27 MF-S1's adversarial pass (merged 2026-10-11)

`docs/review-mime-fidelity-s1-adversarial.md`: 11 found, 4 confirmed, 1 refuted, 3 duplicates, 3 non-goals.
Fixed with tombstone tests: the re-read stamp rule (a record is stamped current on re-read only when no
standing note is unexamined, MA-347); two unpinned guards (the still-incomplete case and the carried-forward
original at another stem); the `examinedBy` boundary (a mutant `>=` survived, now pinned); the nested-multipart
cost is encoded. **Two findings need owner rulings under MC28, because any fix changes the frozen v1 replay
that pins identity (J5, OR4); each is a skip encoder and not a regression of S1:**
(1) **MS1-AO1 (medium):** the identity, fingerprint, content digest and file stem of a message whose `Date`
header ends in an alphabetic zone abbreviation (`EST`, `PST`, `CET`) depend on the host's time zone: the same
bytes give fingerprint `376f4140d094bcbd` under UTC and `2c977d1a7e6cc44b` under America/New_York, so one
message can become two records when the same archive is captured from two computers or after a time-zone
change. **This is in v1, i.e. in released behaviour.** [Recommended: rule a fixed table for common
abbreviations as a deliberate, versioned identity migration, with the existing records kept as aliases;
defer to a design slice; until then the single-user, single-computer case is not exposed.] (2) **MS1-AO2
(medium):** the frozen replay is quadratic on an unclosed Message-ID comment (a 200 KB header took over a
second) and on deeply nested multipart (a 6,000-deep, 401 KB message over a second), with no deadline; S1
runs it beside the content parser, so the cost doubles. [Recommended: a deadline or depth cap that turns a
slow parse into the stub identity, ruled as a deliberate drift.] Both encoders cite R10 as well as R27 and
MA-331. Merged with the S1 build's `TestCopyExportScalesToSixtyThousandMessages` heap bound untouched (§13.24).

### 13.28 FI-S2b built and merged (2026-10-11), delegated-judgement

Local containment: mbox, maildir, Thunderbird and Evolution are read through one checked open
(`util.OpenRegularFile`, `ReadDirIn`, `os.Root`, a lexical link judgment and the Windows reparse
classifier). A pipe or device where a mailbox or folder should be is a visible gap and never a hang; one
fault costs one file or one folder; a message whose parse panics keeps its bytes as a raw-preserving stub
(first 64 KiB, valid UTF-8, with `Record.ParserGen`, no identity fold, J8) that a later build re-examines
once; the per-input directory-link opt-in (OR6) is carried by the CLI, the GUI wizard and the job; **K2, the
interim hole of §13.17, is closed:** the legacy re-class can no longer hide a stub this build minted
(`TestAReclassDoesNotHideAStubThisBuildMinted` is a real test), and the Graph raw-stub and parser-generation
re-fetch skips of §13.22 are real tests. Conform: GAPS_FIXED (a silent drop in the Evolution Maildir++ root, a
vacuous discovery guard, the missing EIO injection, a Windows-junction maildir). **Owed: the adversarial pass**
(R25 containment, Windows). **Open:** ESC-1 (two distinct MIME-panicking raw messages with no Message-ID in
one folder still collapse into one record; skip encoder citing R1; FI-S2b-id); a fallback "(unparseable
message)" stub that is neither labelled `unparsed` nor bounded (skip encoder citing MA-318); MA-503 (tier L,
pending: a real OneDrive Files-On-Demand placeholder); the Windows junction test is compiled and vetted, not run.
Delegated choices: maildir messages carry a new `ReadLocator` (the S2a test pins an `ident:` locator for
export panics); `ParserGenCurrent` follows `ParseSchemeCurrent`; a mailbox-shaped file in a Thunderbird folder
that cannot be opened at the sniff is a fillable `folder-unreadable` gap (it used to be silently ignored).

### 13.29 MF-S2 built and merged, switched OFF, awaiting an owner decision (2026-10-11)

The fidelity walker is built, tested and merged **but not switched on** (`fidelityContent` is false;
`ParseSchemeCurrent` stays 1), so a build still writes exactly what the build before S2 wrote. Built:
the walker (later parts after an unknown charset kept and decoded; unknown transfer encodings kept as
`.undecoded` with a terminal `undecoded-part` gap; lenient parameters; text attachments keep their source
bytes; truncated base64 saved as `.partial` with a fillable `corrupt-part` gap; legacy 8-bit text becomes
valid UTF-8), the bounds (depth 33, 10,001 leaves, 64+ parameters, `filename*N`, header size, encoded-word
bomb, 32 parse-gap labels plus a summary), the "fidelity contains v1" check with the visible
`fidelity-fallback` gap, the shared `internal/util` primitives (MC35), parse-gap classes and the "cannot
decode" counts on the CLI, GUI, desktop and `status` surfaces, and `BenchmarkParseCorpus` (the replay alone
about 770 MB/s, replay plus walker about 315 MB/s, peak about 7.7 times a 16 MiB message). Conform:
GAPS_FIXED (the check's verbatim-body rule was untested). **Owed: the adversarial pass (hostile MIME) and the
friction review with the X-series check.** MF-S1's merge conflict in `internal/state/manifest.go` (FI-S2b's
`ParserGen` fields and S2's `FidelityFallback`) was resolved by keeping both.

**Owner decision needed to switch it on** (recommended: yes): (1) narrow four assertions of MF-S1 tests that
say "no content moves": `TestFidelityParserKeepsTheIdentityGoldenOnTheCorpus` (subtests "content from v1",
"v1 panics: the stub and its identity", "the content parser panics: v1's content") and
`TestMixedVersionsKeepIdentityAndStems` (a record whose message carries a note newer than its scheme is not
stamped on re-read, per MS1-AI1); the identity goldens themselves do not change. (2) Bump
`ParseSchemeCurrent` to 2, which regenerates `p14-golden.json` (`parse_scheme` 1 to 2 on 46 records, nothing
else). **What switching on changes for you:** every capture path, including the proven `graph` path, then
decodes bodies and attachments with the new walker (better text, kept attachments, new visible gaps).
Identity does not change. Also open: the README.txt in an archive root does not yet explain `.undecoded` and
`.partial` for an inheritor (the design puts it with S7); MS1-AO1 and MS1-AO2 (§13.27) stay owner decisions,
the replay cost now doubled by the walker; `internal/desktop/workspace.go` is one space off gofmt at the base.

### 13.30 FI-S2b adversarial pass merged (2026-10-12), delegated-judgement

Branch `wip/fi-s2b-adv` (report `docs/review-fault-isolation-s2b-adversarial.md`). Found 28, confirmed 18,
refuted 1, duplicates 7, non-goals 2. Sixteen fixed, each with a tombstone test: the depth cap on the local walk
(S2B-AO1), a healthy sibling no longer closes an unreadable file's gap (AO2), pipes named like data files are
never opened (AO4), placeholder folders are folders (AO5), Thunderbird parents are no longer sniffed away
(AO6), directory-link opt-in fixes (AO7, AO8, AO9, AI8, AC2), a mailbox whose first read fails is recorded
(AI7), a message moved from `new/` to `cur/` mid-walk is no partial run (AI4), a planted manifest path deletes
nothing outside the archive (AI9), the visited set and the vanished-file relist are no longer quadratic
(AI10, AC5), two stubs of the same bytes are each settled by their own copy (AC7). The merge to the fix
branch was clean; the full suite and `vet3` pass in the clean clone.

**Open for the owner (batched, each has a skip encoder that goes red when un-skipped; nothing blocks):**
- **ESC-S2B-1** (S2B-AI2): an older binary's save drops a stub's parser generation and raw digest, so a later
  healthy read never supersedes it. Recommended: recover the digest from the `.eml` when one exists.
- **ESC-S2B-2** (S2B-AC4): a last mbox message cut mid-append by a live client is archived as complete.
  Recommended: a size/mtime stability check that leaves a fillable gap on the folder, not a partial run.
- **ESC-S2B-3** (S2B-AO6 residual): a non-empty file named like a mailbox that sniffs as no mailbox.
  Encoder `TestANonMboxFileNamedLikeAMailboxIsRecorded`.

Known and unchanged: ESC-1 (K1), K2, MA-503 (now also needs placeholder-folder rows in the lab), the
Windows tests are compiled and vetted, not run.

### 13.7 Escalations from LC-C1's adversarial pass (merged 2026-10-10)

None blocks anything: each has a skip encoder or a safe interim in force.

| Id | The question for the owner | Recommended answer | Safe interim |
|---|---|---|---|
| **ESC-11** (AC8) | Answers that net/http writes before any handler (400, 408, 431 on a malformed request) cannot carry the policy headers, so MA-378's "every response" holds only for handler answers. Build a listener-level wrapper, or narrow the wording to handler answers? | Narrow the wording: these answers carry no archive data, and a listener wrapper is a large change for no data protection | Wording unchanged; `TestParseErrorAnswersCarryThePolicyHeaders` is a skip encoder (MA-378, R19, R23) |
| **ESC-12** (AI2) | `status` echoes the recorded text of a known warning code from a record any archive writer can write. Rebuild the text from the code alone (dropping the file path P10's text names), or leave it to slice A's private root? | Rebuild from the code and show the path from a separate, validated field | Echo stays; `TestStatusDoesNotEchoAPlantedWarningText` is a skip encoder (MA-375, R12) |
| **ESC-13** (AC4) | An interactive Archive run records no `config_in_archive` warning, so `status` stops warning while the settings stay inside the archive. MA-375 and P10 name only the Settings POST and the headless capture. Cover the interactive run too (refuse or warn)? | Warn, as the headless capture does | Not covered; encoded by a skip encoder |
| **ESC-14** (AO5) | A loopback `serve` on `127.0.0.2` now accepts its own literal, because the printed URL otherwise failed its own Host check. Design P6, S43 and MA-371 say the loopback set is exactly `127.0.0.1`, `localhost` and `[::1]`. Amend those three to match, or refuse such a bind at start? | Refuse the bind at start (keeps the exact set, narrower exposure) | The fix stays; `TestLoopbackBindOtherThan127_0_0_1KeepsTheDesignsExactSet` is a skip encoder that goes red when unskipped |

### 13.6 Escalation from the g8 branch

**ESC-10 RULED by the owner on 2026-10-10 ("esc-10 go"): the recommended answer.** One rule for
every removal: never delete a descriptor the tool cannot read; print its path and the remedy, and
status keeps naming it. This is owner-accepted, so it narrows MA-455's old guarantee on purpose:
`5090706`'s default-named deletion is reversed, and MA-455's covering assertion for the
default-named case changes from "the file goes with the entry" to "the file stays and is named".
It is built on the g8 branch after its adversarial pass finishes (the pass is reading that code),
as its own commit with prove-fail, and MA-455's row text is updated in the same commit. The table
below is kept as the record of the question.

| Id | The question for the owner | Recommended answer | Safe interim | Blocks |
|---|---|---|---|---|
| **ESC-10** (g8 review, issue 1; §13.3 g8-R2) | Should `schedule -out X -remove`, and the dashboard's remove, keep and name an unreadable backup descriptor, as a custom-named removal now does, instead of deleting it with the archive's default-named schedule? Adopting this narrows MA-455's existing guarantee and reverses 568c992's covering assertion (§0, rule 2) | Yes. One rule for every removal: never delete a descriptor the tool cannot read, because it cannot tell whose it is. Status names the file with the remedy | 568c992's rule, restored in `5090706`: the file goes with the default-named entry and stays, named, with any other entry. The residual: a damaged descriptor that was rewritten for a custom-named backup is deleted when a default-named entry for the same archive is removed | nothing |
