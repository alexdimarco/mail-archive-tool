# Owner rulings — 2026-10-09

**Status: a draft log for the owner.** It was drafted on branch `wip/owner-rulings` (base
`176c4b2`) from two kinds of source:
- the five design documents and their 10-lens pre-code reviews at that commit:
  `docs/design-{extraction-workspace,fault-isolation,mime-fidelity,recapture-integrity,local-confidentiality}.md`
  and the matching `docs/review-*-predesign.md`;
- the reports of four fix branches: `wip/sanitizer-r2-export` (`deb2e9e`),
  `wip/sanitizer-r2-reader` (`7b22df7`), `wip/sanitizer-r2-index` (`1cbf466`) and
  `wip/g8-scheduling` (`4f82d6e`).

How to read it:
- Nothing here is built. No catalog row, design document or test changes with this file.
- Every delegated ruling stands until the owner overturns it.
- Every **ESCALATE** waits for the owner. Its safe interim is stated, and nothing that depends
  on it starts (§10).
- The text edits these rulings imply for each design are listed in §11, for that design's
  author.

## 0. Authority

**The owner's words** (2026-10-09, verbatim, as relayed to the session that drafted this log):

> yes the read only - also continue without constantly asking me

- **"yes the read only"** answers extraction-workspace **Q3** (its review's **OR5**). The
  archive source gains a read-only "another archive folder" option, chosen with the folder
  picker, where nothing is ever written. This is the only **owner-direct** ruling in this
  log.
- **"continue without constantly asking me"** delegates everything else, within the hard
  limit below.
- **Earlier rulings stand and are not reopened:**
  - the workspace's owner rulings:
    - OW1: a new page inside MailArchive Desktop;
    - OW2: the source is live Microsoft 365 or the local archive, chosen per run;
    - OW3: own mailbox only, no `Mail.Read.Shared`;
    - OW4: no drag-and-drop;
  - the desktop design's owner ruling P1 (in-process, not shell-out);
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
    local-confidentiality's `Posture(dir)`.
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
    conversion never becomes a permanent 60-second poison.
  - **nextLink guard.** One guard (a repeat set, a host/path pin with XC25's normalization,
    and a page cap) runs in every paging loop from SG on, including capture's `Folders` and
    `Messages`.
    - Slice E1c is folded into SG.
    - MA-443's nextLink-shape sub-case stays a pending lab row. It now informs the work and no
      longer gates it.
    - A refused link names the foreign host. In `Messages` it is a loud `folder-list-failed`
      gap and in `Folders` a mailbox failure. A bearer token is never sent.
  - **Rows.** SG carries MA-314 (the primitive cases), MA-402 and MA-401b (MA-505). E1 keeps
    MA-400, MA-401a, MA-403a, MA-451, MA-463 and MA-464 (the allocated ids are in §8).
- **Authority:** delegated-judgement.
- **Why:**
  - One type and one guard live in one package.
  - Pinning capture's nextLink is a strengthening that fails loud and closed.
  - Gating it on a lab row this repo cannot run would leave the bearer-token exposure open
    with no end date.
  - Workspace E1 no longer waits on fault-isolation S1a.
- **Unblocks:** SG, fault-isolation S3 and workspace E1. This supersedes the E1c gate in the
  workspace's XC25. The XC40 addendum (RR-2) re-checks the change.

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
| Fault isolation | new R25, S46–S48 and their rows. R1's ledger sentences (recorded, listed, retried every run, never consulted to skip work). R10's "the smallest unit" and "never hangs" as added sentences. R17's "a message a run could not archive is recorded as a capture gap and retried" as an added sentence. R18's `partial` flag (fault-isolation OR1) | not adopted, residual open: R10's exceptions (N7, N8) and its narrowing of "never a fatal crash". R17's replacement wording ("archived, or recorded as a capture gap"). R1's N9 sentence, which becomes a skip encoder citing R1 in S2a. X1's exit 3 (fault-isolation OR2) |
| MIME fidelity | new R26, R27, S49 and MA-330..349 | the R2 clarification, which lands with re-capture's R2 text as one paragraph (ESC-2) |
| Re-capture integrity | new R28, S50, S51, their rows and the confirmed-open gap encoders. R5's write-beside sentence (with S4). R1's "a kept item is not a gap" (with S5). MA-238 and MA-227, strengthened | ESC-2: R2, R13 (new deletion cases), S24, the MA-66 and MA-87 covering-test changes, and design-incremental-completeness B2 |
| Local confidentiality | new R22–R24, S42–S45, X12 and their rows. The R4, R19, X9, X11 and MA-232 amendments. MA-277 (the reader on its own ephemeral loopback port) and MA-281 (reconnect only to this account's verified instance): the shortcut still always opens the user's own dashboard, no assertion is removed without a stronger replacement, and the C2 adversarial pass confirms it | ESC-4: R13's classifier and intent rule (CC27, OQ-8) |
| Extraction workspace | new R29, S52, X13–X15 and their rows | — |

- **Authority:** delegated-judgement. This is the hard limit, applied.
- **Why:** additions and strengthenings cannot lower a gate. Narrowing catalog text, or
  changing what a covering test asserts, is the owner's decision.
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
before E2, and RR-3 (the revision-3 text for Q3) before E4.

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
| Q13 / OR4 | Does the workspace supersede desktop P7 "Export a copy (extract mbox/eml)"? | Yes. The dashboard has one export surface, and `extract -format mbox` stays the CLI path for mbox | delegated-judgement | one tool for select, choose, copy; P7 was never built | E3's sidebar, §3 |
| Q14 / OR8 | Run the native picker in a child process (a self re-exec)? | No. Desktop owner ruling P1 (in-process, not shell-out) stands, and XC31's minimum applies: Browse is disabled while an export runs, and §5.2 keeps the crash and hang rows | delegated-judgement | a delegated ruling does not reopen an owner ruling; with Browse disabled during a run, a picker crash cannot cost an export | E3 |
| Q15 / OR9 | Who owns F17, F75, F91 and F111? | For exports, F75, F91 and F111 are handled by an export-only, stricter naming pass layered over `util.SanitizeSegment` and `SanitizeFilename` (never a copy of them), built in E2. MA-417's sub-cases then pass with no archive migration. The archive-side fix, which needs its own migration, stays open outside these five designs. F17 goes to the owner of the shared Graph layer (fault-isolation), as slice FI-GF17 after S3 | delegated-judgement | no archive migration; one owner for the Graph layer | E2 (P7 holds for exports); §4.14 and R-v once FI-GF17 lands |

**W1 — the second archive source (Q3, owner-direct).**
- The source chooser gains **Another archive folder…**, picked with the OS folder picker
  (Browse) or typed. The configured archive location stays the default archive source.
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
  withdrawn. Revision 3 writes §4.3, §4.6, §7.4 and MA-531 before E4 (RR-3).

## 3. Fault isolation (`docs/design-fault-isolation.md`, revision 2)

| Ids | Question | Ruling | Authority | Why | Unblocks |
|---|---|---|---|---|---|
| OR1 / OQ1 | What last-run status does a partial run get? | `"failed"` plus `partial: true`. `JSONVersion` stays 2, because the field is additive | delegated-judgement | an older `status` then fails closed (RED) instead of showing GREEN for a partial run whose gaps are all in the ledger; no documented value set widens | S1b |
| OR2 / OQ7 | Amend the locked X1 so that the capture verbs exit 3 on a partial run? | No amendment under delegation. A partial capture run keeps exiting 1 (S1a's mapping), and the partial state travels in the flag, the summary, the sidecar and `status`. The owner may still amend X1 later | delegated-judgement | X1 is locked, and design-review step 4 makes its amendment an owner act; exit 3 would also stop monitors that key on exit code 1 from alerting on cases that fail today | S1b |
| OR3 / OQ2 | Persistent known gaps: report on every run, or report only new loss? | Report on every run. Each run that observes a fillable or terminal gap is partial on every surface (exit 1, the sidecar kept, the `-job` notification sent), and each surface leads with "new this run: K" and `last_run.gaps_new`. Not-attempted gaps stay a note | delegated-judgement | reporting known loss as a clean run would accept it; `gaps_new` lets monitoring separate new loss | S1b |
| OR4 / OQ3 | Does a "vanished" fault seen for the first time make the run partial? | Yes | delegated-recommended | a one-shot run before a departure must not claim completeness | S1b |
| OR5 / OQ4 | What is the stuck threshold, and in what unit? | `Runs ≥ 3` AND `now − FirstRun ≥ max(72 h, two schedule periods)`, counting at most one observation per day. The posture stays WARN (FC18) and never becomes RED | delegated-recommended (the review's suggestion) | escalation no longer depends on the schedule's cadence or on extra clicks | S1a |
| OR6 / OQ5 | Should local stores follow directory links? | Not by default. An explicit per-input opt-in (a flag, a GUI option and a job-descriptor field, following X4/X5) keeps the input's token and follows directory links for that input only, guarded by a visited (dev, inode) set. File links outside the store and special files stay refused even with the opt-in. An archive that captures through links today gets a terminal gap whose remedy names the opt-in | delegated-judgement | the only option that keeps an affected user on the same token, with no second tree, while R25 holds by default | S2b |
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
- **Rows that still span two slices:** MA-318 (S2a and S2b), MA-323 (S1a and S2a), and
  MA-325 and MA-326 (S1a or S1b, and S5).
  - Each lands with its earlier slice and carries a skip encoder for its later cases, citing
    the row, until the later slice lands.
  - Without that, the covers-map would read an unbuilt half as covered. That is FC33's
    reasoning.
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
| Q6 / OR1 | How does with-Message-ID Graph mail get repaired on MailArchive Desktop? | Option (a): a Desktop action, "Repair messages archived by an earlier version (re-downloads your mailbox)", with a size and time estimate, as slice MF-SD after S7 with its own friction review. Until then MC16/MC37's disclosure stands, and the gap stays open, not accepted | delegated-judgement | (c) accepts the gap, and (b) cannot reach a default Desktop archive, which keeps no `.eml` | MF-SD |
| OR2 | The joint ruling with fault-isolation | = J5. The identity fold is J8 (ESCALATE) | see J5 | J5 | S1 |
| OR3 | The joint ruling with re-capture integrity | = J6 | delegated-judgement | J6 | removes OR3 from S7 |
| OR4 | Pin the Go toolchain? | Keep floating; the CI policy is unchanged. P1's toolchain residual stays an open condition, not accepted. MA-330 and MA-331 run on every CI build and catch drift on corpus shapes. A strengthening is recommended for S1: vendor `encoding/base64` into `parsev1`, and have MA-331 assert `unicode.Version`, so that the named residual becomes a tripwire | delegated-judgement | pinning would miss toolchain security fixes, which weakens the CI policy (the track-the-current-patch rule in `ci.yml` that OR4 cites); accepting the residual is not delegable | S1 |
| OR5 | Catalog ids | = J1 | delegated-judgement | J1 | S0 |

Notes:
- **S0's precondition is met.** S0 needs `BodyFallback` to have landed, and it has on this
  branch (`internal/state/manifest.go:87`).
- **J6 changes §4.10.** Its staged rename-over is replaced by re-capture's commit. That
  change goes in before RR-4.

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
- **RI-S2 lands its F48 core:** the absent-only rule, archivefs-gated deletion, row repair and
  N2 adoption. That restores R13 as written. Deleting the redacted stem's `-vN` family is a
  new deletion case, so it waits for ESC-2.

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
| OQ-8 | Should redaction need explicit intent? | **ESCALATE (ESC-4)**, together with R13's classifier amendment | ESCALATE | every option either narrows R13's automatic prune or drops the safety net that dissolved the review's hidden blocker | D |
| OQ-9 | Where does the dashboard live? | Option (b): an ephemeral port, like the reader. The shortcut, and "Copy MailArchive link", are the only way in; 8097 leaves the RMM runbook; CC23's "another program holds 8097" notice is dropped. The typed-address squat residual is narrowed, not closed, and stays an open gap, not accepted | delegated-judgement | (a) needs the owner to accept the squat-and-phish residual; (b) removes the fixed address the residual depends on | C2 |
| OQ-10 | Catalog ids | = J1 | delegated-judgement | J1 | every slice's catalog-first PR |

Notes:
- **Under OQ-9, C2's text changes.** The dashboard takes a free port, as the reader does.
  P8's "8097 if free" and the CC23 notice go, and README-RMM verifies the install by the
  shortcut and the locked page.
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
| Extraction workspace | R22 copy export is separate, contained, faithful-or-absent and self-verifying | **R29** | FI-SG, which carries the workspace's MA-402 and MA-505 (J4) |
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
| MA-401b | **MA-505** | FI-SG (J4) | | MA-457 | **MA-521** | E3 |
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
MA-505..534 (extraction workspace). The next free overflow id is **MA-535**.

## 9. Cross-design build order

**How to read the table:**
- The numbers are merge order.
- Slices with no dependency between them may be built in parallel. Each rebases on what has
  merged before it.
- "Blocked by" names an ESCALATE (§10). A blocked slice does not start until the owner
  answers.

**These can start as soon as this log stands:**
- the lens re-runs RR-1, RR-2 and RR-3;
- FX-SAN (merge after its adversarial round 3);
- LC-A and LC-C1;
- FI-SG and FI-S1a;
- MF-S0;
- RI-S1 (interim);
- FX-G8.

**Lens re-runs that must come first:**

| Id | Re-run | Before | Required by |
|---|---|---|---|
| RR-1 | lenses 3, 6 and 7 on re-capture revision 2 | RI-S4 and RI-S5 code | RC38 |
| RR-2 | lenses 3, 6 and 7 on workspace revision 2, including J3's and J4's changes | EW-E2 code | XC40 |
| RR-3 | lenses 3, 6 and 7 on workspace revision 3's second-archive-source text (W1); may be folded into RR-2 | EW-E4 | Q3 is a material change |
| RR-4 | delta re-review of §4.10 (lenses 3, 6, 7, 8), on the text that writes J6 in | MF-S7 | MC43 |
| RR-5 | lenses 3, 6 and 7 on §4.D | LC-D's catalog-first PR | CC27 |

**Build order.** Slice prefixes: FX = fix branch, LC = local confidentiality, FI = fault
isolation, MF = MIME fidelity, RI = re-capture integrity, EW = extraction workspace, G7 = g7.

| # | Slice | Depends on | Re-run first | Gate owed after build | Blocked by |
|---|---|---|---|---|---|
| 1 | FX-SAN: integrate `wip/sanitizer-r2-{export,reader,index}`, with FB-2, FB-3 and FB-5, the flattener's SVG and `<td>` fixes, and MA-187 reworded as an open gap with its encoder (FB-1) | — | — | **adversarial pass**, round 3, over the three branches' new commits (owed by their reports) | — |
| 2 | LC-A: private at rest | — | — | **adversarial pass**; **friction review** (status lines, migration line, inheritor and recipient walks) | — |
| 3 | LC-C1: Host check, allow-list, credentials outside the archive, `localweb.Mux` and MA-378 | — | — | **adversarial pass** | — |
| 4 | FI-SG: the shared Graph primitive (J4, with E1c folded in) | — | — | **adversarial pass** (network input), shared with EW-E1's note | — |
| 5 | FI-S1a: sinks and surfaces, `GapClass`, the `Counts()`/status rewrite (J5), `app.GapRemedy` | — | — | **friction review** (summary, status, sidecar) | — |
| 6 | MF-S0: the corpus, goldens from the release tag, MA-330..349 as skip encoders | — (`BodyFallback` has landed) | — | none (tests and catalog) | — |
| 7 | RI-S1 (interim): R28, S50 and S51, with the confirmed-open gap rows and encoders | — | — | none | the amended wording waits for **ESC-2** |
| 8 | FX-G8: `wip/g8-scheduling` after its review fixes, plus FB-4 (MA-457, MA-458) | — | — | re-review of the fixes; friction check of the `verify_stale` line | — |
| 9 | G7-F53: a failed manifest save is surfaced (g7's slice; it edits `commitInOrder`) | — | — | g7's own gate | — |
| 10 | FX-187: FB-1's writer signal (MA-484) | 1; `state.Record` edits serialised with 5, 12 and 17 | — | **adversarial pass** (rebuild forgery) | — |
| 11 | LC-C2: per-account surfaces, with OQ-9's ephemeral dashboard (workspace E0 = 3 + 11) | 3 | — | **adversarial pass** and **friction review** (a co-equal ship blocker); X9, X11 and X12 contract updates | — |
| 12 | MF-S1: identity routing, `stubIdentityTerms`, the vendored replay | 6 (with J5 and OR4) | — | **adversarial pass** (identity drift) | — |
| 13 | FI-S2a: PST isolation, plus N9's encoder | 5 | — | **adversarial pass** | — |
| 14 | FI-S3: Graph isolation | 4, 5 | — | **adversarial pass** | — |
| 15 | EW-E1: Graph additions, F15, `CheckRedirect`, identity-safe token write-back | 4 | — | short **adversarial** note (or folded into EW-E3's pass) | — |
| 16 | RI-S2: the `reindex` guard (F48); creates the `internal/archivefs` gate | 7 | — | **adversarial pass** | the `-vN` redaction case waits for **ESC-2** |
| 17 | RI-S3: additive schema and `archivefs.Inspect` | 7 | — | none of its own | — |
| 18 | LC-B: no link-following writes (`ArchiveFS`, in the same package) | 16 (the package) | — | **adversarial pass** | — |
| 19 | FI-S2b: local containment, with OR6's opt-in and raw stubs without the identity fold | 5, 12 | — | **adversarial pass** | the fold is **ESC-1** (row 40) |
| 20 | MF-S2: the fidelity walker, with C1-1 | 12 | — | **adversarial pass**; **friction review** and X-series check | — |
| 21 | MF-S5: PST interim | 12 | — | **adversarial pass** | — |
| 22 | FI-S1b: the partial outcome (OR1–OR4) | 5, 9 | — | **friction review** | — |
| 23 | EW-E2: the engine, the run record, the export-only naming pass (Q15) | 15, 5 (with J3 and Q12) | **RR-2** | **adversarial pass** on the write side (combined with EW-E3's) | — |
| 24 | EW-E3: the first shippable workspace (Microsoft 365 → original EML + manifest, retry, the switch) | 23, 3, 11 | — | **adversarial pass** and **friction review** | — |
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
| 38 | MF-S7: repair (J6) | 20, 21, 25, 26 (31 if its fixture exists), 33, 34 | **RR-4** | **friction review** and **adversarial pass** | **ESC-2** (the joint R2 text), via 33 and 34 |
| 39 | MF-SD: the Desktop repair action (Q6 / OR1) | 38 | — | **friction review** | via 38 |
| 40 | FI-S2b-id: the F33 identity fold | 19, 12 | — | **adversarial** addendum | **ESC-1** |

**Not built:** LC-A-W (OQ-1). EW-E1c is folded into FI-SG (J4).

## 10. Escalations (these go back to the owner)

| Id | The question for the owner | Recommended answer | Safe interim | Blocks |
|---|---|---|---|---|
| **ESC-1** (J8) | Accept the named identity drift for messages the v1 parser cannot parse, by regenerating MA-330's golden for those inputs only, with a README note? | Yes | S2b ships without the fold; the constant stub stays; the collapse is a skip encoder citing R1 | FI-S2b-id |
| **ESC-2** (re-capture Q5 / OR5, plus the joint R2 text) | Accept these amended guards? R2 as one joint text with mime-fidelity's repair clarification. R13's new deletion cases: superseded copies, and the redacted stem's `-vN` family. S24's removal once the new manifest is durable. The MA-66 and MA-87 covering-test changes. Design-incremental-completeness B2, superseded | Accept. Those tests encode F46 and N2 as today's behaviour | RI-S1 lands only the additions and encoders; RI-S2 lands without the `-vN` case; RI-S3 lands; no existing text or test changes | RI-S4..S7, MF-S7, MF-SD |
| **ESC-3** (re-capture Q1 / OR1) | How should a changed re-capture keep names and page state? (a) move to `-vN` only on a content or renderer change, so pages keep their capture-time read, flag and category state; (b) stable names with a per-record intent journal, where `verify` accepts either side's digest; or (c) replace in place and accept the §1.7 false alarm | (a), the review's recommendation, asked once RI-S1's §1.7 encoder has run (RC33) | today's write path stays; RI-S4 does not start | RI-S4..S7, MF-S7, MF-SD |
| **ESC-4** (local-confidentiality OQ-8, plus R13's classifier) | Accept R13's amendment, that only an operator's deletion is a redaction and that a moved, renamed or unreadable file keeps its record? And which intent rule: always, beyond a threshold (N = 25, as revision 2 describes) or never? | The threshold, with the R13 amendment | plain `reindex` keeps today's behaviour; LC-D does not start | LC-D |

None of the four blocks the owner's chosen tool. EW-E1..E6 need none of them.

## 11. Follow-ups for other owners

**The kit owner** (`assureblock/`, which is not edited here). Three strengthenings of the
covers-map gate:
1. **A duplicate-id check:** an R or S id defined twice, or an MA row id that appears twice.
2. **A definition check:** every R id the catalog mentions has exactly one `- **Rn —`
   definition line. Today any mention makes an id valid.
3. **A row-shape check:** any `| MA-` table line that the spec-row regex cannot parse fails,
   instead of being skipped. Examples are `MA-401a`, or a tier written `U + L`.

**The design authors.** These text edits follow from the rulings above:
- **Extraction workspace:**
  - rewrite the Covers cells to §8's ids;
  - OW4 and XC39 cite fault-isolation FC20 (Q12);
  - E1c is removed, and E1 depends on SG (J4);
  - §4.13's classification row cites `graph.Classify`, with the copy export's one-line
    justification;
  - §3's Q3 non-goal is withdrawn, and revision 3 adds the W1 text and MA-531;
  - Q14 is closed;
  - F17 goes to FI-GF17.
- **Fault isolation:**
  - rewrite the Covers cells;
  - the SG row list follows J4;
  - P9 and MA-325 read "exit 1 with the `partial` flag" (OR2);
  - §2.1 follows J7;
  - §4.5 and §6 describe OR6's opt-in;
  - the skip encoders for rows that span slices (§3).
- **MIME fidelity:**
  - rewrite the Covers cells;
  - §4.10 commits through re-capture's write path (J6), before RR-4;
  - the OR4 note, and the strengthening recommended for S1.
- **Re-capture integrity:**
  - rewrite the Covers cells;
  - the R5 amendment loses the fault-isolation stub-re-render exception;
  - "pending removal" (`removal_pending`);
  - the interim S1 scope (§5).
- **Local confidentiality:**
  - rewrite the Covers cells;
  - §6's workspace sentence follows J3;
  - C2's dashboard port follows OQ-9;
  - A-W is marked not built (OQ-1).
