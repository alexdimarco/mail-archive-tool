# Pre-code design review — Re-capture integrity

**Design reviewed:** `docs/design-recapture-integrity.md`, revision 1 (2026-10-09).
The conditions below are folded into revision 2 (2026-10-09) of the same file.

**Procedure:** the 10-lens battery of `assurance-kit/process/design-review.md`, run as a
workflow on 2026-10-09: finder → skeptic → rescue. Each lens had its own finder (lenses
1–10). One refutation-default skeptic checked every finding against the design text and
the code. A separate rescue reviewer got every finding the skeptic left at blocker or major
severity and was told to save the design with modest mechanism changes, or to say plainly
that a finding survives.

**Lenses run:** all ten. 1 purpose / threat-model fit · 2 focus / proportionality ·
3 durability · 4 usability · 5 integration / seams · 6 security / adversarial ·
7 failure domains / recoverability · 8 migration / coexistence · 9 dependency &
operational cost · 10 honesty of claims.

**Findings: 97 found · 91 confirmed · 6 refuted** (L1-11, L2-1, L2-4, L3-14, L6-12,
L7-5). The rescue added two more (H1, H2), both confirmed. After the skeptic: 5 blocker,
40 major, 46 minor. The five blockers are two defects:
- **L1-1:** the MERGE verdict has no "something improved" gate, and its construction cannot
  carry an item the new message no longer lists. A v7 record whose attachment disappeared
  loses it (F46 again), and a pure regression MERGEs back and forth on every full run.
- **L1-2 = L3-1 = L5-3 = L7-7:** an UNCHANGED re-capture stamps no observation. A Full-mode
  Graph run over an unchanged mailbox therefore leaves every `LastSeen` behind, and
  `SweepGone` writes the whole mailbox to history as gone.

Three finder blockers were downgraded to major: L1-3 (contingent on the mime-fidelity
design landing as written), L3-2 (no message is lost; recovery is manual) and L5-1
(contingent; resolvable by a joint ruling). The rescue handled all 45 blocker and major
findings: **37 DISSOLVED** by a named change, **8 PARTIAL** (L1-3, L3-4, L3-7, L5-1, L5-10,
L6-1, L8-3, L8-5), **0 SURVIVES**. All five blockers dissolved. The rescue also found the
hidden blocker below (H1), which no lens named.

**Verdict: GO_WITH_CONDITIONS.** Conditions **RC1..RC38** must be fixed before code. They
are folded into revision 2, and the build must not start from revision 1. No lens fails,
because no blocker survives the rescue. Code is gated in two ways:
- **RC38:** lenses 3, 6 and 7 re-run on revision 2, as an addendum to this review, before
  any S4 or S5 code. RC1, RC2, RC5, RC9 and RC12 change the verdict, write and deletion
  paths, which is a material change under design-review.md.
- **Owner rulings OR1..OR7:** each blocks the slice it names. They are listed separately
  because each would amend a decision taken in a sibling design, or a product choice the
  owner has not made. Every PARTIAL rescue leaves its residual either with one of these
  rulings (OR1, OR2, OR3, OR6) or stated inline in the design (L6-1).

**The owner's decision relayed for this run** reads: "go with the simpler tool … where you
just select the source and choose the export destination to copy it to rather than any drag
and drop". It concerns the extraction workspace's interaction model, and this review is
consistent with it. No condition adds a UI control, a drag-and-drop path or any new
interaction surface, and NG7 stands. RC37 stops citing that decision as support for this
design's own scope and states the real seam with the workspace, which is its lock-free
archive reader. RC26(e) changes the GUI only by rewording an existing label.

## Hidden blocker (rescue pass) — an UNCHANGED re-capture cannot heal a damaged archive

W4 and P8 compare the staged bytes with the **recorded** fixity, never with the files on
disk. Take an archived unit that has gone missing or bit-rotted while the source is still
intact:
1. A full re-export finds staged == recorded and classifies the message UNCHANGED.
2. It discards the good temps and writes nothing.
3. `verify` stays `modified` or `missing`, and `status` stays RED for good.

That full re-export is exactly the remedy the tool prints for a RED "not attested":
- `health.go:240`: "restore the affected files from a backup or re-export them from the
  source";
- ARCHIVE-INTEGRITY-ATTENTION.txt (`state/lastrun.go:276-277`): "re-export them from the
  original mail source".

Today's Full run rewrites the file and heals it. Under revision 1, once the source later
degrades, or the Graph mail is gone, the content is unrecoverable even though a run had the
bytes in hand. That is an F46-class loss caused by P8, not by the source. The design
contradicts itself:
- §5.4 says "Earlier zip — missing: holds nothing … modified: COMMIT if no worse (heals
  it)", but on an unchanged source the verdict never reaches COMMIT.
- MA-362 ("writes no file") would pin the regression as correct behaviour.
- W4's `Held` backfill then claims items the disk does not hold. A later MERGE reads a
  missing zip, and R-G misreports it as `modified`.

**Resolution (RC2).** Before discarding the temps, inspect each earlier unit through the
gate. When a unit is absent, its size differs from the recorded size, its `Held` binding
mismatches (RC11), or, in Full mode, its digest differs from the recorded one, the outcome
becomes **HEAL**:
- if the staged digest equals the recorded fixity and the comparison against the on-disk
  content finds nothing the staged unit lacks, the staged unit is renamed into place. This
  is crash-consistent at every instant, because the staged digest IS the persisted fixity:
  a kill leaves the unit "still damaged" or "healed", never a false alarm. It is a named
  W1/P5 exception;
- otherwise the record takes the normal W1 COMMIT path.

HEAL is a Full-mode outcome, which keeps today's parity with a redaction in progress
("delete the html, then reindex"). It is counted as `Healed`, with a summary line.

**Two secondary defects** surfaced by the same pass:
- **H2.** §4.5's MERGE row carries the earlier `.eml`, which lacks the item the merge just
  filled. That contradicts the row's own rationale ("never emit an original known to lack
  content the archive holds"). It is not a regression against today, but the text or the
  rule must change (RC7).
- §4.13's extraction-workspace sentence is wrong, and the workspace proposes the same
  R22/S42 ids. This duplicates L1-8/L5-8/L8-8/L10-8 and L1-7 (RC37, RC30).

## Conditions (must-fix-before-code; folded into revision 2)

- **RC1 — Every non-writing outcome stamps the observation.**
  - **UNCHANGED runs W5's record switch at `T = prev.Path`, minus the file writes.**
    `Manifest.Add` gets the record rebuilt exactly as COMMIT rebuilds it: `Present`,
    `LastSeen = RunAt`, `Folder`; the PhysID + AltPhysIDs union, ContentHash, FpScheme;
    FirstFolder, FirstSeen and AlsoFiles carried; prev's Path, Fixity and ExportedAt kept;
    gaps recomputed from this capture; `Held` backfilled; `Kept` cleared; no Supersede. Then
    `OnExported` runs as in today's Full mode: the index row is replaced and the history
    folder-assertion written.
  - **KEEP and UNVERIFIED** (P9 extended) stamp through `MergeFields` +
    `OnManifestSkip(key, folder, moved || !prev.Present)`, with no `idx.Add` (which would
    index the degraded body).
  - **Tests.** MA-356 and MA-362 gain a live-path UNCHANGED twin, including the first full
    run after an upgrade: `LastSeen == runAt`, `Present`, `SweepGone` returns nothing, and a
    moved message gets its folder-assertion and index folder. Prove-fail: drop W4's `Add`.

  *Closes:* L1-2, L3-1, L5-3, L7-7.
- **RC2 — HEAL: an UNCHANGED re-capture repairs a damaged earlier unit** (hidden blocker
  H1, resolution above).
  - The unit is checked through the gate (RC9): absent, a size mismatch, a `Held` binding
    mismatch (RC11), or (Full mode) a digest mismatch.
  - It is healed in place only when its staged digest equals the recorded fixity and the
    on-disk comparison (RC11) finds nothing the staged unit lacks. Otherwise the record takes
    W1's COMMIT path. A damaged unit that holds something the staged one lacks gives KEEP.
  - Full mode hashes each earlier unit once, like `verify`. The cost is stated in §5.4 and
    §6.
  - New stat `Healed`, summary line R-H, and P12 (new).
  - **Tests.** MA-362 twins: a deleted zip + a full run → the zip is re-created and verify
    attests; a size-preserving flipped byte → healed in Full mode; a restored older zip that
    holds an extra entry → KEEP, not healed.

  *Closes:* H1.
- **RC3 — MERGE requires an improvement, and an absent item is never carried.**
  - The MERGE row reads: "at least one item or the body improved, AND only zip-held items
    regressed, the page did not, and the merge is possible (§4.4)". Otherwise the outcome is
    KEEP.
  - §4.4 gains condition 5: a regressed item that is absent from the new message (no
    (label, occurrence) or content match, RC4) is never carryable, so the outcome is KEEP.
  - **Tests.** MA-350 T1 and T2 twins: two full passes over the same degraded source →
    `Kept=1`, `Merged=0`, no file written, no Superseded entry. MA-356 variant: report.pdf
    absent plus an improved item → KEEP, the earlier trio byte-unchanged, nothing superseded.

  *Closes:* L1-1, L3-5.
- **RC4 — Items match by label and content, never by index; T2 holds only within one parse
  scheme.**
  - `HeldItem` gains `CRC32`, which the zip writer computes anyway. Inline items get one when
    embedded.
  - **T1:** an item is present when its same-(label, occurrence) counterpart is non-empty, or
    when any new item has the same (Size, CRC32). `Ix` is advisory only.
  - **T2** applies only when `prev.ParseScheme` equals the current scheme. That is the
    mime-fidelity design's field; until it lands, both are 0. Otherwise T3 applies.
  - **T3's** "earlier entry absent" test also accepts a (CRC32, size) match read from the
    earlier zip's central directory.
  - §4.1's sentence "attachment names and order are identical" is removed.
  - **Tests.** MA-355 twins: frozen-fingerprint label drift (v1 `[report.pdf]` → fidelity
    `[note.txt, report.pdf now empty]`) → KEEP, never COMMIT; a renamed label whose bytes are
    unchanged (`inline (2)` → `invoice.pdf`) → matched by CRC, not a regression.

  *Closes:* L1-3 (a, b), L3-3. The g3 repair and version halves of L1-3 go to RC28, OR2 and
  OR3.
- **RC5 — UNCHANGED is decided by content, not by page bytes, and the real trigger set is
  stated.** This is the mechanism for OR1's recommended option.
  - `Held.Content = m.ContentDigest()` (equal to `Record.ContentHash` where the live path
    records it). `Held.Render = RenderSchemeCurrent`, an int bumped by any release that
    changes page output and pinned by a golden-render test.
  - **W4 returns UNCHANGED, writing nothing, when all of these hold:** nothing regressed or
    improved (the inventory is equal, including body and eml presence); the zip digest equals
    `Fixity.Zip`, or neither capture has a zip; `Held.Content` and `Held.Render` match; and
    the earlier units are present and intact (RC2).
  - A pre-v7 record without `Held` falls back to revision 1's byte comparison. Its first
    changed full run records `Held`.
  - The page keeps its capture-time state: Unread, Importance, Sensitivity, Categories and
    the transport headers. That matches S35/S36 ("a snapshot … still the same message") and
    design-categories §5 ("capture-time snapshot, shown as captured").
  - **Q1 and P8 restate the triggers:** a content change, a renderer release (once per
    record), `-raw` newly on, a parse-scheme repair.
  - **Tests.** MA-362 twins: a read flag flipped and a category added between two full runs →
    UNCHANGED, no file written.

  *Closes:* L2-3, L3-6, L9-2 (OR1 decides whether the snapshot semantics are accepted).
- **RC6 — The page links the post-verdict `.eml`.**
  - `RenderContext.RawName` names the `.eml` that will exist after the verdict: the new raw,
    or the carried earlier `.eml`. The page is rendered, or re-rendered, after W2 step 4.
  - P8 lists its real inputs inline (render scheme, render options, mailbox state under
    RC5). It is reworded to "no archived file is written, renamed or deleted (temps may be
    staged and discarded)".
  - **Tests.** MA-357 and MA-362 twins: `-raw` on, then off between full runs → UNCHANGED,
    the eml link is present, the eml byte-unchanged.

  *Closes:* L10-6.
- **RC7 — The MERGE eml rule states what it keeps.** §4.5's MERGE row keeps its behaviour:
  carry the earlier `.eml`, never write the new degraded raw, and write none when there is no
  earlier one. Its rationale is restated as: "an original is the faithful bytes of one
  capture, never one known to be degraded relative to the copy it replaces". The residual is
  stated next to P4: on a merged record no original holds every item, and the filled item
  lives only in the zip. MA-357(c) asserts it. *Closes:* H2.
- **RC8 — Writing beside the earlier copy never consumes the space the manifest save
  needs.**
  1. **Space precheck** before staging a moving re-capture: free bytes ≥ prev's Fixity sizes
     + the current manifest size + a margin, via statfs or GetDiskFreeSpaceEx
     (golang.org/x/sys, already in go.mod). If short, commit and run `finishSuperseded`
     first. If still short, the outcome is KEEP with reason `low-disk`, nothing is written,
     and summary line R-I names the space needed. First captures of new mail continue as
     today.
  2. **Pending-bytes trigger:** checkpoint (commit + sweep) when the bytes written beside
     since the last durable save exceed max(1 GiB, 2 × manifest size).
  3. **W1 adoption:** an existing, unowned candidate trio whose three digests equal the
     staged temps is adopted as T, and the temps are discarded. Orphans from a failed save or
     a kill are then re-owned on retry instead of ratcheting to `-v3`, `-v4`.
  4. **§5.4's bound restated:** clamp(n/8, 1000, 25000) messages per store file or mailbox
     (about 12.5% for 8k–200k records, more below 8k) or the pending-bytes cap, whichever
     comes first, conditional on G3–G5 passing. With a gate holding, the bound is per
     message, by RC14's `-v99` cap. The text says which blocks `reindex` clears.
  5. README and the full-mode CLI and GUI text state the free-space precondition (text
     only).
  6. **Tests.** MA-358 twins: ENOSPC injected on staging and on the checkpoint Save → the
     earlier copies are intact, the next run converges (adopts or sweeps), and status is not
     wedged.

  *Closes:* L3-2, L7-6, L9-1, L10-5; the adoption half of L4-1.
- **RC9 — The earlier-unit gate has three outcomes.**
  - `archivefs.Inspect` returns:
    - **absent**: ENOENT or ENOTDIR on a component, parents readable;
    - **refused**: a symlink or an irregular file;
    - **unreadable**: any other Lstat, open or read error, a truncated central directory, or
      a read over the cap.
  - Only **absent** holds nothing. **Refused** and **unreadable** units are presumed held:
    T1 uses `Held`, T2/T3 presume held, and no merge reads from them. A regression then gives
    KEEP or UNVERIFIED with a reason that names the cause and the path, never COMMIT. The
    §4.5 eml carry follows the same rule.
  - In W1, any stat error other than ENOENT counts as "exists".
  - **Tests.** MA-360 twin: a symlinked folder → KEEP. MA-354 twin: injected EACCES on the
    earlier zip → KEEP. MA-357 twin: the same on the eml. MA-355 twin: a truncated earlier
    zip → KEEP.

  *Closes:* L7-1, L3-10.
- **RC10 — `reindex` never deletes on doubt, and never deletes a row path directly.**
  - §4.9 treats `rec.Path` as missing only when Inspect says **absent**. Refused or
    unreadable keeps the row and the record, deletes nothing and logs the path, or aborts as
    today's EachRow does on a non-ENOENT stat error (reindex.go:111-118).
  - Only manifest-named paths are deleted directly: rec.Path's siblings and AlsoFiles, each
    through archivefs (no symlinked component, regular file only, the message-stem shape of
    RC12).
  - The row path is never deleted directly. It is enqueued as
    `SupersededEntry{Path: row path, Key}`, so its siblings must pass G1, G2, G4, G6 and G7.
    §8's sentence "the index is never acted on" then holds.
  - §4.13 sequences S2 with g56's C2-2/P5: both rewrite `deleteMessageFiles`, so the
    archivefs gate lands once.
  - **Tests.** MA-361 variants: a symlinked folder; injected EACCES on `rec.Path`; a
    suffix-less row path naming another record's stem.

  *Closes:* L6-5, L7-2.
- **RC11 — `Held` is bound to the bytes it describes, and validated.**
  - `Held.Zip` = `Fixity.Zip.SHA256` at capture. T1 applies to the zip unit only if the
    on-disk size (from the gate's Lstat) equals `Fixity.Zip.Size` and `Held.Zip` equals the
    current `Fixity.Zip`. Otherwise that unit uses T3 (central-directory names plus CRC).
  - `verify -record` clears `Held`, or refreshes it from the central directory, when it
    re-baselines a zip.
  - §5.4's "COMMIT if no worse (heals it)" becomes "COMMIT only if no worse than the on-disk
    content".
  - `Held` is validated at load or use: Ix in range, Size ≥ 0, unique Ix, bounded label and
    entry lengths. A malformed `Held` is treated as nil (T2/T3), with a warning.
  - **Tests.** MA-364 twin: an older zip with an extra entry is restored, with and without
    `verify -record` → the extra entry is kept or carried, never deleted. A malformed `Held`
    → T2/T3.

  *Closes:* L7-3, the Held half of L7-11.
- **RC12 — Deletion is bound to content and to the superseding copy.**
  - `SupersededEntry` gains the superseded page's digest: `prev.Fixity.HTML`, or computed at
    Supersede time for a pre-fixity record (the page is small).
  - **G6:** delete the siblings only if `entry.Path`'s `.html` is absent (siblings then
    deleted under G1/G2/G4) or its digest equals the entry's. Otherwise drop the entry
    without deleting, and log it.
  - **G7:** if `entry.Key` still has a record, its own current `.html` must be present with
    its recorded size.
  - **G1** is restricted to the message-stem shape of every shipped naming rule
    (`<token>/…/<date>_<time>_<slug>_<hex>[-vN].html`, the pre-68a4096 rule included).
    `index.html` and folder pages never match.
  - §5.4's row "open, toward extra files" is corrected to cite G6/G7.
  - **Tests.** MA-360 twin: an older manifest and search.db are restored after B ↔ B-v2
    alternation → nothing deleted, the entry dropped and logged.

  *Closes:* L6-3, L7-4.
- **RC13 — The lock is re-checked before every deletion.**
  - `finishSuperseded` calls `lock.StillHeld()` before its first deletion and every 100
    deletions, at every call site: start of run, checkpoint, file and mailbox boundary, end
    of run, and reindex.
  - On failure it stops deleting, leaves the entries pending and sets `lockLost`.
  - The boundary and final commits also re-check `StillHeld` before Save, aligned with g7's
    F53.
  - §8 persona 5 is reworded to match.
  - **Tests.** MA-360 twin: the lock file is replaced before the final commit → no file
    deleted.

  *Closes:* L6-4, L7-9.
- **RC14 — Blocked removals are bounded and never permanent by construction.**
  1. A re-capture whose target or earlier directory has a symlinked component is refused
     before writing. This adopts g56 P5's rule early. The outcome is KEEP with reason
     "symlinked folder `<path>`" and the C2-2 remedy (R-J), so W1 never writes beside into a
     directory G4 will refuse to clean.
  2. W1 is capped at `-v99`, which fits the four-rune reservation. When no candidate is free,
     the outcome is KEEP with reason "earlier copies await removal" (R-K), never a longer
     suffix.
  3. Each entry records a cause (`index-stale`, `index-not-open`, `in-use`, `permission`) and
     `FirstBlockedAt`.
  4. A deletion requires `PagesStale` to be persisted true. An entry that becomes deletable
     after a regeneration cleared the flag sets it again and waits for the next save. A
     regeneration clears the flag whenever no deletion has happened since, so blocked
     entries no longer force regeneration on every run.
  5. Deletions are skipped, with the entries left pending, on runs that will not regenerate
     pages (`-pages=false`, no index, `allFailed`).
  6. The text states that redaction removes a pending copy only once its block clears.

  *Closes:* L3-8, L9-3, L7-10 (a, b).
- **RC15 — Each pending-removal cause has its own reason code and remedy, and crash orphans
  are legible.**
  - R-E, the `status` WARN and the verify note give one reason code per cause. Each names the
    first path and the count, with a remedy that works:
    - stale row → "run mailarchive reindex";
    - index not open → "run with -index (the default), or mailarchive reindex";
    - in use → "close the program holding `<path>`";
    - permission → "`<path>` cannot be removed (`<error>`) — check its permissions".
  - The symlink cause no longer arises (RC14).
  - `verify` classifies an unowned file whose stem is in the `-vN` family of a record's Path
    (same directory, the base stem with or without `-vN`) as a new kind, `earlier-copy`. Its
    line names the current path and the cause ("left by an interrupted re-capture; the
    current copy is `<path>`"). The remedy follows OR7.
  - §7.3 walks the power-cut case including the orphans and counts its steps.

  *Closes:* L4-1, L4-2.
- **RC16 — W3's failure path never removes the record's own files.**
  - Once the first rename-over has happened, an error removes only the remaining temps. The
    renamed units stay, because P1 judged them no worse.
  - The record stays unchanged. It has no fixity, so verify reports `unrecorded` as before.
    The error names the path.
  - W2's "remove every file this attempt created" applies only to fresh T names.
  - **Tests.** MA-359 twin: the html rename is refused after the eml and zip renames → the
    eml and zip are present and readable, and the old page still links them by the same
    names.

  *Closes:* L6-6.
- **RC17 — Legacy adopts: identical bytes adopt in place; kept earlier files are owned,
  counted and quantified.**
  1. UNCHANGED by bytes (staged digests equal the recorded fixity, the earlier files intact)
     is evaluated **before** the legacy row. The record is adopted with no write and
     `FpScheme` is re-stamped to current.
  2. Optional: where the rebuild reader parses the earlier page's header rows, an envelope
     match (subject, sender, recipients, date to the second, attachment names) counts as
     verified identity and takes the normal supersede path.
  3. The kept earlier path is recorded in the new record's `AlsoFiles`, and verify learns to
     count AlsoFiles as owned. G2 then never sweeps it, verify no longer calls it
     `unexpected`, and redaction's prune reaches it (reindex.go:150 already deletes
     AlsoFiles).
  4. Every run that leaves one prints one summary line (R-F, extended beyond regressions),
     and `status` counts them.
  5. §5.4 and §6 state the one-time growth: about the pre-v5 Graph share of the archive,
     once, on the first full run.
  6. Legacy adopts never use W3, which resolves W3 against §4.6.

  *Closes:* L3-7, L5-10, L8-3 (each PARTIAL; the residual doubling goes to OR6) and the
  DEMOTE half of L1-6.
- **RC18 — Redaction reaches every copy, and a stale-row redaction is loud.**
  1. The DEMOTE-kept path is in AlsoFiles (RC17).
  2. A redaction prune also deletes the unowned `-vN` family of the redacted stem (same
     directory, the base stem with or without `-vN`, N ≤ 99, owned by no record) through
     archivefs. The stem embeds the key digest, so such a file is by construction a copy of
     the same key.
  3. In §4.9, every repair or keep where the row path differs from `rec.Path` prints both
     paths and a redaction remedy, and deletes nothing:
     - cases A and C: "the archived message is intact at `<rec.Path>`; if you deleted
       `<row path>` to remove this message, also delete `<rec.Path>` and run reindex again";
     - case B: "the current copy `<rec.Path>` is missing; an earlier copy `<row path>` is
       still served — delete it and run reindex to remove the message, or re-export to
       restore it".
  4. P7, §4.13 and §7.5 are qualified "for a fresh index row".
  5. **Tests.** An MA-361 twin of MA-227: a message with a DEMOTE-kept earlier file and a
     `-v2` orphan is redacted → no copy remains under `/files/`. MA-361 twins for cases A, B
     and C assert the line and zero deletions.

  *Closes:* L1-6, L6-1 (PARTIAL: during a stale-row window redaction is loud and one step
  away, not silent and complete).
- **RC19 — R22 states what the mechanism guarantees.** R22 reads: "A re-capture never makes
  an item the archive holds — the body, an attachment, an inline image, the preserved
  original — empty, unreadable or absent. It deletes an earlier file only after no persisted
  manifest record or index row references it." Its exceptions are stated inline:
  - NG2: body kind and length are not ranked;
  - NG3: same-name size changes are not judged;
  - NG4 DEMOTE: the earlier file is kept on disk and owned through AlsoFiles, rather than
    recorded as kept;
  - RC2's HEAL: it writes the recorded bytes in place.

  Folder pages are named as a regenerated view (`PagesStale`), not a durable reference.
  *Closes:* L1-4.
- **RC20 — Live-path and maintenance housekeeping.**
  - `RunGraph` runs the age-gated `SweepOrphans` at its start, next to the start-of-run
    `finishSuperseded`. MA-358 gains a RunGraph kill-at-C1 case.
  - `SweepOrphans`' zip rule runs after Load and skips zips whose stem is a record's Path, an
    AlsoFiles entry or a pending Superseded entry. A record with a missing page but a present
    zip is reported, not deleted. This closes the pre-existing residual of L7-12.
  - `-manifest` outside `-out`: the limitation is stated, or the path is passed through
    reindex, verify and status.
  - `app.Rebuild` (the dashboard's index action) runs `finishSuperseded` and clears
    `PagesStale`. R-E names it as the dashboard remedy.
  - Directory fsyncs are batched: unlink every passing entry, fsync each distinct directory
    once, then drop the entries.

  *Closes:* L3-11, L3-13, L5-6, L7-12, L7-10 (c).
- **RC21 — Superseded entries survive key and path migrations, and G3 fails closed.**
  - Every key remap also maps `Superseded[].Key`, and every path canonicalization maps
    `.Path`. That covers CollapseByIdentity + `idx.Rekey`, Load's re-scope, RepairKeys and
    MigrateKey.
  - G3 resolves the entry through the current record, not the stored key alone.
  - A `PathForKey` error keeps the entry pending.
  - **Tests.** MA-364 twin: a re-key with a pending entry.

  *Closes:* L3-12, L5-7, the G3 half of L7-11.
- **RC22 — The merge copies bytes, never headers, and every read is bounded.**
  - `CreateRaw` gets a fresh FileHeader: the recomputed Name; Method limited to Store or
    Deflate; CRC32, CompressedSize64 and UncompressedSize64 copied; Extra, Comment,
    ExternalAttrs, Flags and Modified reset to what `zw.Create` writes. Encrypted entries are
    refused (KEEP).
  - The digest check and `OpenRaw` use the same open handle.
  - Every decompress is capped at min(`Held.Size + 1`, `rebuildMaxFileBytes`). The
    central-directory entry count is capped at 65,535 through one helper shared with the
    mime-fidelity design's bound; above the cap the outcome is KEEP.
  - G4's removal and the T3 and merge reads go through `os.Root` (`Root.Lstat`,
    `Root.Remove`, `Root.Open`), keeping the no-symlink, regular-file rule as an extra check.
    This is the extraction workspace's containment primitive too.

  *Closes:* L6-8, L6-9, L6-10, L5-5 (c).
- **RC23 — Status and sentinels leave the label namespace.**
  - UNVERIFIED becomes a typed `Record.Unverified bool`, not a `Terminal` label. The kept
    body becomes `KeptBody bool`, beside `Kept []string` (labels only).
  - `UnknownSentinel` stays as is for version-1 records.
  - A gap list containing a label equal to a sentinel (`body`, `unknown`, `unverified`) is
    ambiguous evidence, so the outcome is KEEP.
  - The report and the workspace reader read the typed fields.
  - **Tests.** MA-355 twin: attachments named `body`, `unknown` and `unverified`.

  *Closes:* L6-2, L2-5.
- **RC24 — The fill probe stays cheap.**
  - The probe runs in two phases. First, today's improvement check (oldMissing only, with
    the short-circuit). Only when that finds an improvement are the held items streamed to
    detect regressions.
  - §4.6's "already streams every attachment" is corrected.
  - **Tests.** MA-351 counter assertion: a non-improving probe streams no held attachment.

  *Closes:* L2-7, L9-4.
- **RC25 — One gate, one body reader, one summary.**
  - `archivefs.Inspect` replaces every copy of the gate: verify's `inspect`,
    `extractInspect` (extract.go:532), extract.go:364 and the rebuild walk (reindex.go:236).
    This is coordinated with the workspace's E4 slice.
  - The page-body anchoring and the read cap move into a shared leaf package beside
    archivefs, which htmlheader.go, rebuild and recapture.go all call. A golden test renders
    each page shape (plain, HTML, fallback, no body) and asserts that both readers agree.
  - One app-level `RecaptureSummary(Result) []string` is used by the CLI, the GUI and the
    desktop. MA-363 asserts that the text is identical across them.

  *Closes:* L2-6, L9-5, L5-5 (a, b).
- **RC26 — Owner-facing wording is cause-specific and true.**
  - (a) **KEEP wording by reason and evidence.**
    - `unreadable` → "could not be read this run; kept the archived copy".
    - T3, UNVERIFIED or presumed → "could not confirm the new capture is no worse, so the
      earlier copy was kept".
    - "Only copy" is used only for the empty and absent reasons on T1/T2 evidence.
    - The record carries `KeptHow` (`lost`, `unreadable`, `presumed`), and P2 states the T3
      condition inline. MA-355(b)/(c) assert the presumed wording.
  - (b) **R-B is source-aware.** On-demand sources get the offline remedy. Graph and
    deterministic sources get "no action can recover it from the source; the archive keeps
    the earlier copy".
  - (c) **R-G is split by cause.** A digest or CRC mismatch keeps the restore text. An open
    or read error gets "could not be read now (in use or unreadable); kept unchanged and
    retried next run". A backup restore is never advised without a mismatch. MA-354 twin
    with an injected read error.
  - (d) **R-G reaches status.** Its detections are an additive last-run count, and `status`
    raises a WARN reason code naming `verify` and the restore remedy. Exit codes are
    unchanged (X1).
  - (e) **Unchanged work is visible.** "N unchanged (nothing rewritten)" goes in the summary
    and the Done line. The GUI's Full label is reworded; this is wording only, inside NG7.
  - (f) **Report rows carry subject and date.** On KEEP and UNVERIFIED, Subject and Date are
    set from the new capture whenever HasIssues becomes true.

  *Closes:* L1-10, L10-3, L4-3, L7-8, L5-9, L4-6, L4-5.
- **RC27 — The operator walks follow real paths.**
  - §7.2 becomes a GUI/CLI (`app.Run`) Thunderbird incremental fill walk.
  - New walks: the desktop Microsoft 365 user, whose dashboard runs only Graph incremental,
    so it can show R-E and little else; and the scheduled `-mode full` operator, who sees the
    archive only through `status`.
  - P11 scopes the desktop surface accordingly.
  - §7.3 includes the crash orphans (RC15).

  *Closes:* L1-9, L4-4.
- **RC28 — The sibling re-capture writers are named, and the claims carry their condition
  until the joint ruling (OR3).**
  - §4.13 lists the mime-fidelity design's P11 repair (g3) and the fault-isolation design's
    supersede re-render (g2), with the governing rule:
    - g3's repair is a re-capture. It calls §4.3, with its count precondition kept as an
      extra gate, and it writes through W1–W5, recomputing `Held` and clearing `Kept`. OR3
      may instead declare it an exception;
    - g2's stub re-render (a content-free page) is an explicit P5 exception, with the §1.7
      window stated next to the amended R5.
  - The R5 amendment is scoped to "re-captures through the shared write path".
  - P1's "every re-capture path" and W1/P5's "never writes over a recorded file" carry that
    condition inline.
  - g3's repair is added to §4.6's list of re-capture paths.
  - One joint R2 clarification is drafted, to land in a single S1.
  - The "supersede" vocabulary clash is named and left to OR3.

  *Closes:* L3-4, L5-1, L8-5, L10-2 (PARTIAL: the in-place, stable-stem conflict waits on
  OR1–OR3).
- **RC29 — Version policy: one program-level ruling, no bump by default, expand/contract if
  bumped.**
  - §4.13 adds the version seam. Q2 becomes the joint ruling OR2, with this design's default
    flipped to **no bump**.
  - §6 states what an older binary does with each additive field:
    - `Held` dropped → T2/T3 fallback;
    - `Superseded` dropped → pending copies linger, still protected by W1's existing-file
      rule and shown as `earlier-copy` (RC15);
    - `Kept`, `KeptBody`, `KeptHow`, `Unverified` and `PagesStale` dropped → notes lost,
      pages regenerate at the next exporting run.
  - It states the one excursion loss specific to this design: an old binary re-capturing a
    DEMOTEd record back onto B overwrites the kept original, which is the same exposure as
    today's in-place overwrite.
  - **If a bump is ruled:** expand/contract. Release N ships Load accepting 7 while Save
    still writes 6. Release N+1, bundling S4 and S5, starts writing 7. S3's write never
    ships alone.
  - §6 states the restore cost inline: restoring the pre-upgrade backup discards every
    capture since the upgrade (permanently for Graph mail removed since), and the manifest,
    search.db and files must be restored together.
  - It adds the MSI/RMM note (MajorUpgrade with DowngradeErrorMessage, Product.wxs:37-39).
  - The "refuses" claim is scoped to v3+ binaries; pre-guard v2 binaries are a stated
    residual.

  *Closes:* L8-1, L8-2, L8-7.
- **RC30 — R22, S42 and S43 are provisional until the operator allocates ids (OR4).**
  - They are provisional labels, and no covers marker may cite them.
  - The collisions are listed: design-fault-isolation, design-mime-fidelity,
    design-extraction-workspace and design-local-confidentiality.
  - Every Covers cell is rewritten to the allocated ids before S1.

  *Closes:* L1-7, L5-2, L8-4, L10-1.
- **RC31 — The kill-point harness reproduces a hard kill.**
  - §9 specifies the harness. The hook copies `out/` (manifest, search.db with `-wal` and
    `-shm`, the lock file, the history log, every message file) to a snapshot directory at
    Cn, from inside the callback, before any defer or return can commit. This follows
    checkpoint_test.go:36-58's observe-from-the-callback pattern. The next Run and verify
    then run against the snapshot. Alternatively, the test re-execs itself and SIGKILLs the
    child at Cn.
  - MA-358 asserts that the stale lock and the uncommitted index batch are present in the
    snapshot.
  - The "RED on today's code" check uses the same harness before §12 may withdraw §1.7.
  - P5's and P6's "proven" is conditional on that harness.

  *Closes:* L10-4.
- **RC32 — Each slice ships its own sinks.**
  - S4 carries the first new-field producer (`Superseded`) together with R-E, R-H..R-K
    (its HEAL and refusal lines), the pending-removal WARNs, verify's `superseded` and
    `earlier-copy` kinds, and any version change.
  - S3 lands the schema only, with Load accepting the new fields; or S3 is folded into S4.
  - S5 carries its own sinks: report kinds, the Counts and HasIssues changes, R-A..R-D,
    R-F and R-G, and the `merge_source_mismatch` WARN.
  - S6 keeps README.md/README.txt, GUI and desktop parity, and the friction review.
  - MA-364 lands whole in S4.
  - S1 adds `t.Skip` encoders only for the confirmed-open gaps (F46, N2, N3, F48,
    §1.5–§1.7). Feature rows land with their slices.

  *Closes:* L5-4.
- **RC33 — Q1 is re-premised before the owner rules (OR1).**
  - S1's §1.7 encoder runs before Q1 is asked. It is expected to go RED on today's code:
    `commitTemp` renames over the recorded files, and the record is saved only at a later
    checkpoint.
  - Q1 lists option (c): **stable names with a per-record intent journal.**
    - Before a batch's rename-overs, `{old record, new record}` is persisted per key.
    - `verify` accepts either side's per-unit digests.
    - The start-of-run reconcile picks, per unit, the side that matches disk.
    - Cost: one extra manifest save per checkpoint batch, the batch's temps staged first,
      reconcile code, and dual acceptance in verify.
  - Q1 states that a bare "pending dual digest" is not P5-equivalent: after a kill, `Held`
    and the gap lists would describe the other side's files.
  - Q1 states the real trigger set (RC5) and the external costs of a move: bookmarks,
    file:// links, saved reader `/files/` URLs, and rsync-style backups seeing delete +
    create.
  - The last-verify record and `status -json` `last_verify` gain an additive `superseded`
    count. `verify -json` keeps the version 2 bump; the skeptic refuted L8-9's sub-point.

  *Closes:* L2-2, L8-9.
- **RC34 — Client placeholders count as absent, and the headline scenario is labelled
  lab-pending.**
  - The mbox/maildir parser flags as a client placeholder any attachment part carrying
    `X-Mozilla-Altered`, `X-Mozilla-External-Attachment-URL` or Content-Type
    `text/x-moz-deleted`. The comparison treats such a part as "now absent" whatever its byte
    count, so a non-empty placeholder can never commit as NG3 `changed`.
  - S5 builds fixtures from Thunderbird's documented detach and delete part headers.
  - §1.1 and §7.1 are labelled conditional / lab-pending inline. Three facts are stated:
    - "Delete attachment" renames the part ("Deleted: x"), which changes the fingerprint, so
      the record #fp-splits and both copies survive;
    - an offline-window shrink or a cache eviction usually removes whole messages, so no
      re-capture happens;
    - real-profile confirmation stays lab-pending.
  - **Tests.** MA-350 twin: a non-empty placeholder part → KEEP.

  *Closes:* L1-5.
- **RC35 — The remaining claims carry their conditions, and numbers and citations are
  refreshed.**
  - (a) **Same-envelope forgery.** §8.1 states the forgery and forced-COMMIT residuals.
    `Held.BodyDigest` is recorded at T1 capture, and a T1 MERGE carries items only when the
    new body digest equals it. T2 MERGE keeps the residual, stated inline. MA-353 twin: a
    forged same-envelope copy → KEEP.
  - (b) **§8 persona 3** is conditional: it holds on filesystems that enforce POSIX modes or
    ACLs; on FAT/exFAT every writer to the drive is inside the trust boundary.
  - (c) **Inventory size.** The estimate is re-derived from a real fixture manifest, and
    `Entry` is stored only when it differs from the label. MA-364 gets a size assertion.
  - (d) **Proof rows.** MA-350/355 get T1/T2 inline-regression twins, and MA-355(b) a
    full-run re-check twin. §4.3 states the tier for MA-356 and for `Unverified` records.
    §12 lists P6 once, with its scope.
  - (e) **Citations** are refreshed: main.go:1413, identity_test.go:166, schedule.go:294,
    exporter.go:145.
  - (f) **Pre-fixity eml.** A carried, undigested pre-fixity `.eml` is digested from the
    bytes kept or copied, labelled as a baseline like `verify -record`. MA-357 twin.

  *Closes:* L6-7, L6-11, L9-6, L10-7, L10-9, L3-15.
- **RC36 — Existing N2 victims are adopted, not pruned.**
  - In §4.9's odd case and on any run, a record whose `rec.Path` is absent is matched
    against an unowned regular file in the same directory that carries the key's stem hash,
    or that the index row names. The match is checked against the record: Message-ID and
    date from the page header, or its eml.
  - If it matches, the file is adopted as `rec.Path` and logged. Otherwise the record is
    pruned as today, and the log names the candidate.
  - P6's fsync condition is stated next to P6.
  - **Tests.** MA-361 twin built from the N2 PoC state.

  *Closes:* L3-9, L8-6.
- **RC37 — The workspace seam is its reader, and NG7 stands on its own.**
  - NG7 is this design's own scope decision. The owner's workspace ruling (select a source,
    choose an export destination, copy; no drag-and-drop) is honoured, but no longer cited
    as authority for it.
  - §4.13 replaces its workspace sentence with the facts:
    - the workspace never writes the archive;
    - its lock-free archive-source reader never enumerates Superseded or `-vN` earlier
      copies, because they are never record paths;
    - a `-vN` move or a deferred deletion between its snapshot and its copy surfaces there as
      "changed in the archive during export";
    - `Unverified` and `Kept` (typed, RC23) are not turned into "never downloaded" notes;
    - the version policy (OR2) covers its reader.
  - The workspace design is asked to update its §4.14 and MA-431 to the W1/W5 semantics.

  *Closes:* L1-8, L5-8, L8-8, L10-8.
- **RC38 — Lenses 3, 6 and 7 re-run on revision 2 before any S4 or S5 code.** RC1, RC2,
  RC5, RC9 and RC12 change the verdict, write and deletion paths. The addendum is filed in
  this review's file. S2 may proceed on revision 2 once OR4 and OR5 are ruled; it owes its
  own adversarial pass.

## Owner rulings (not imposed by this review; each blocks the slice named)

- **OR1 (design Q1, re-premised by RC33 and RC5) — How a changed re-capture keeps names and
  page state.**
  - (a) **Move to `-vN` only on a content or renderer change.** The page keeps its
    capture-time read, flag and category state; RC5 is the mechanism. This is the
    recommendation.
  - (b) **Stable names with a per-record intent journal** (RC33, option c). State is
    refreshed on each full run, at one extra save per batch plus reconcile code.
  - (c) **Replace in place,** and accept the §1.7 false alarm.

  Choosing (a) means a full run no longer refreshes read, flag or category state on pages.
  That is a product change today's behaviour does not have, which is why this review cannot
  impose it. **Blocks:** S4. (L2-2, L2-3, L3-6, L9-2)
- **OR2 (design Q2, made joint) — One manifest-version decision.** It covers the
  fault-isolation (g2), mime-fidelity (g3), this (g4) and local-confidentiality (g56)
  designs and the BodyFallback change. g2 and g3 already chose "no bump" to keep a
  binary-swap rollback. Recommendation: no bump (RC29's field-by-field degrade). If a bump is
  ruled, RC29's expand/contract applies. **Blocks:** S3 and S4. (L8-1, L8-2, L1-3, L3-4,
  L5-1)
- **OR3 — The sibling in-place writers.**
  - Does the mime-fidelity P11 repair route through §4.3 + W1–W5? That means accepting
    `-vN` names on repair, and amending that design's "a decoded subject never renames a
    file" and its rollback claim. Or is it a stated P5 exception that accepts the §1.7
    window on repair?
  - Does the fault-isolation stub re-render become a stated P5 exception (recommended)?
  - Which design renames its "supersede" vocabulary? Recommendation: this design's
    user-facing terms become "pending removal" (`removal_pending`).

  These amend sibling designs. **Blocks:** S4 and S5, and the mime-fidelity repair slice.
  (L1-3, L3-4, L5-1, L8-5, L10-2)
- **OR4 — Allocate the R/S ids across the in-flight designs.** These are the
  fault-isolation, recapture-integrity, mime-fidelity, local-confidentiality and
  extraction-workspace designs; it is the same ruling as the workspace review's OR2. Also:
  whether the catalog meta-test gains a duplicate-id check, which is a gate change and the
  operator's to make. **Blocks:** S1. (L1-7, L5-2, L8-4, L10-1)
- **OR5 (design Q5, widened) — Accept the amended guards.** These are MA-66's and MA-87's
  covering tests, the R2/R5/R13/S24 wording, R22's restated text (RC19), the scoped R5
  amendment (RC28) and design-incremental-completeness B2. MA-201 and S39 are amended only
  if OR2 rules for a bump. **Blocks:** S1.
- **OR6 (design Q7, now with a quantified residual) — Legacy-scheme adopts.**
  - **DEMOTE** (the default): a one-time duplication of the pre-v5 Graph share on the first
    full run. Under RC17 that duplication is owned through AlsoFiles, counted and reported.
  - **KEEP:** no duplication, but a distinct Message-ID reuse is silently uncaptured.

  **Blocks:** S5. (L3-7, L5-10, L8-3)
- **OR7 (design Q8, re-premised) — Crash orphans.** RC8's adoption re-owns most orphans,
  and RC15 makes them legible. The open question is whether `reindex` should also remove an
  unowned `-vN` family sibling of a record's current stem outside redaction. **Blocks:**
  RC15's remedy text (S4).

Q3, Q4 (now covering RC15's per-cause codes), Q6 and Q9 stay open as in revision 1. This
review does not change them.

## Rescue outcomes (every finding the skeptic left at blocker or major)

| Finding | Outcome | Named change (short) | Condition |
|---|---|---|---|
| L1-1 | DISSOLVED | MERGE needs an improvement; absent items never carried; two-pass MA-350 twins | RC3 |
| L1-2 | DISSOLVED | W4 = W5 record switch at prev.Path minus file writes; P9 covers KEEP/UNVERIFIED | RC1 |
| L1-3 | **PARTIAL** | content matching (CRC32), T2 per parse scheme, g3 in §4.13 — g3's in-place repair and v6 stance remain cross-design | RC4, RC28, OR2, OR3 |
| L1-4 | DISSOLVED | R22 restated presence-monotone with inline exceptions | RC19 |
| L1-5 | DISSOLVED | client-placeholder rule; lab-pending labels; renamed-part and whole-message facts | RC34 |
| L1-6 | DISSOLVED | DEMOTE path in AlsoFiles; redaction removes the `-vN` family; adoption | RC17, RC18, RC8 |
| L2-2 | DISSOLVED | §1.7 encoder first; option (c) journaled stable names in Q1 | RC33, OR1 |
| L2-3 | DISSOLVED | content-based UNCHANGED; capture-time state snapshot; triggers restated | RC5, OR1 |
| L3-1 | DISSOLVED | as L1-2, plus MA-362's first-full-run-after-upgrade twin | RC1 |
| L3-2 | DISSOLVED | space precheck; pending-bytes trigger; digest-equal adoption; honest bound | RC8 |
| L3-3 | DISSOLVED | as L1-3 (a, b); frozen-fingerprint label-drift row | RC4 |
| L3-4 | **PARTIAL** | g2/g3 named with a rule; R5 scoped — needs the sibling designs and the owner | RC28, OR2, OR3 |
| L3-5 | DISSOLVED | as L1-1 | RC3 |
| L3-6 | DISSOLVED | as L2-3 | RC5, OR1 |
| L3-7 | **PARTIAL** | UNCHANGED before the legacy row; owned, counted, quantified — the doubling remains NG4's price | RC17, OR6 |
| L3-8 | DISSOLVED | symlinked targets refused; `-v99` cap; per-entry cause; PagesStale only after a deletion | RC14 |
| L4-1 | DISSOLVED | adoption; `earlier-copy` verify kind; §7.3 walk | RC8, RC15 |
| L4-2 | DISSOLVED | one reason code and remedy per cause, naming the path | RC15 |
| L5-1 | **PARTIAL** | g3 repair through §4.3; one R2 text — the stable-stem conflict waits on OR1–OR3 | RC28, OR3 |
| L5-3 | DISSOLVED | as L1-2, including the PhysID union and ContentHash refresh | RC1 |
| L5-4 | DISSOLVED | sinks move into their producers' slices; encoders only for open gaps | RC32 |
| L5-10 | **PARTIAL** | as L3-7 | RC17, OR6 |
| L6-1 | **PARTIAL** | loud two-path repair lines; MA-361 A/B/C twins — silent complete redaction is impossible during a stale-row window | RC18 |
| L6-3 | DISSOLVED | page digest in the entry (G6); superseding page present (G7); G1 stem shape | RC12 |
| L6-4 | DISSOLVED | StillHeld before deletions at every call site | RC13 |
| L6-5 | DISSOLVED | only manifest-named paths deleted directly; row path enqueued through the gates | RC10 |
| L6-6 | DISSOLVED | W3 failure removes only temps | RC16 |
| L7-1 | DISSOLVED | absent / refused / unreadable; only absent holds nothing | RC9 |
| L7-2 | DISSOLVED | prune only on absent; reindex deletions through archivefs | RC10, RC9 |
| L7-3 | DISSOLVED | Held bound to Fixity.Zip and size; verify -record clears Held | RC11 |
| L7-4 | DISSOLVED | as L6-3 | RC12 |
| L7-6 | DISSOLVED | as L3-2 | RC8 |
| L7-7 | DISSOLVED | as L1-2 | RC1 |
| L7-9 | DISSOLVED | as L6-4 | RC13 |
| L8-1 | DISSOLVED | Q2 joint, default no bump; field-by-field degrade stated | RC29, OR2 |
| L8-2 | DISSOLVED | expand/contract if bumped; restore cost and MSI note inline | RC29 |
| L8-3 | **PARTIAL** | as L3-7, plus legacy adopts never use W3 | RC17, OR6 |
| L8-5 | **PARTIAL** | as L3-4, plus the vocabulary ruling | RC28, OR3 |
| L9-1 | DISSOLVED | as L3-2, plus README/full-mode free-space text | RC8 |
| L9-2 | DISSOLVED | as L2-3 | RC5, OR1 |
| L9-3 | DISSOLVED | as L3-8 and L4-2 | RC14, RC15 |
| L10-2 | DISSOLVED | inline condition on P1, W1 and P5; §4.13 seams | RC28 |
| L10-4 | DISSOLVED | snapshot-at-Cn harness (or re-exec + SIGKILL); P5/P6 conditional on it | RC31 |
| L10-5 | DISSOLVED | true worst-case bound with its condition; byte cap | RC8, RC14 |
| L10-6 | DISSOLVED | RawName from the post-verdict eml; render after step 4; P8 reworded | RC6 |

## Findings

Severity is the skeptic's verified severity; the finder's, where different, is in
parentheses. Duplicates are graded once and cite their twin. H-rows are the rescue's.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| H1 | rescue | §4.7 W4, P8, §5.4 "Earlier zip" | UNCHANGED compares staged bytes with the recorded fixity, never with the files on disk, so a full re-export cannot heal a missing or bit-rotted unit; the tool's own RED remedy stops working | CONFIRMED (blocker, dissolved) | W4 "The digests of the staged zip and eml … all equal the recorded fixity. The temps are then discarded"; health.go:240 "re-export them from the source"; lastrun.go:276-277; §5.4 "modified: COMMIT if no worse (heals it)" is unreachable on an unchanged source | RC2 |
| H2 | rescue | §4.5 MERGE row | MERGE carries the earlier `.eml`, which lacks the item the merge just filled, against the row's own rationale | CONFIRMED (minor) | §4.5 "Carry the earlier .eml if there is one; if there is none, write none (R20 faithful-or-absent: never emit an original known to lack content the archive holds)". Not a regression against today | RC7 |
| L1-1 | 1 | §4.3 verdict (MERGE row), §4.4 Construction | No improvement gate before KEEP; construction iterates the new message, so an item now absent is never carried (F46 loss on v7, MA-356); a pure regression MERGEs and oscillates B ↔ B-v2 every full run | CONFIRMED (blocker, dissolved) | "Only zip-held attachments regressed, the page did not, and the merge is possible (§4.4) → MERGE" precedes "Anything else → KEEP"; "'Improved' does not gate a COMMIT in Full mode"; §4.4 "iterating attachments in message order"; contradicts MA-350/MA-356 | RC3, RC4 |
| L1-2 | 1 | §4.7 W4, P9 (OMISSION: stamping for UNCHANGED) | A Full run over an unchanged mailbox stamps no LastSeen; SweepGone marks the whole mailbox gone; a move is never folder-asserted | CONFIRMED (blocker, dissolved) | W4 "refreshed in memory only: Held is backfilled, Kept cleared, and Path, Fixity and ExportedAt are unchanged"; P9 "A KEEP on the live path"; graph.go:504 fast path Incremental-only; exporter.go:425-427; SweepGone manifest.go:956-971 called at graph.go:660 | RC1 |
| L1-3 | 1 | §4.1 Item, §4.3 T1 "by Ix exactly", §4.13 (OMISSION: g3) | Under g3's frozen v1 fingerprint, labels and order change under an equal fingerprint: Ix matching masks a loss or yields a false KEEP; g3's in-place repair breaks P5; v7 voids g3's rollback | CONFIRMED (major, partial; finder blocker) | §4.1 "When the earlier and new fingerprints are equal, attachment names and order are identical"; mime-fidelity.md:136-150, :625-626, :641-645, :697-712; g3 absent from §4.13. Contingent on g3 landing as written | RC4, RC28, OR2, OR3 |
| L1-4 | 1 | §2.1 R22 text | R22 is broader than the mechanism: NG2/NG3 and DEMOTE leave the archive holding less; C7 deletes while folder pages still link | CONFIRMED (major) | R22 "Either way it is recorded on the record as kept"; §4.1 DEMOTE "earlier file left on disk, unowned"; §5.1 C7 precedes C9 `pages.Generate` | RC19 |
| L1-5 | 1 | §1.1 realistic degradations, §7.1, MA-350 | The headline Thunderbird detach case is unmodelled; a non-empty placeholder commits as NG3 `changed`; "nothing is lost" lacks its condition | CONFIRMED (major) | §1.1 "Thunderbird 'detach/delete attachment' (same name, the bytes are gone)" stated as fact; no `text/x-moz-deleted` / `X-Mozilla-External-Attachment-URL` handling or fixture in the repo; only §9 says lab-pending | RC34 |
| L1-6 | 1 | §4.6 legacy adopt, NG5, §4.13 redaction (OMISSION: redaction reach) | DEMOTE-kept and NG5 copies are outside AlsoFiles and Superseded, so redaction leaves them served at `/files/` | CONFIRMED (major) | §4.6 "The earlier files are never superseded; they stay unowned … now also covers the unchanged-path case"; §4.13 "unchanged"; server.go:78-79; MA-227 (scenario-catalog.md:475) | RC17, RC18 |
| L1-7 | 1 | §2.1 R22, S42, S43 | Five rev-1 designs propose R22/S42 with different meanings; the covers-map checks only that an id exists | CONFIRMED (minor; finder major) | design-extraction-workspace.md:5/221/244; design-fault-isolation.md:188/228/233-237; design-mime-fidelity.md:136/207/794; design-local-confidentiality.md:7/153. No runtime effect | RC30, OR4 |
| L1-8 | 1 | NG7, §4.13 "The extraction workspace" | NG7 borrows the owner's workspace ruling; §4.13 describes a workspace write the workspace forbids; the real seam (its lock-free reader turning Terminal into "never downloaded") is missed | CONFIRMED (minor) | workspace P1 (:84-92), MA-414, MA-436; notes at :463-466; its §4.14 (:757-765) still assumes N2. The owner's chosen tool is not faulted | RC37, RC23 |
| L1-9 | 1 | §7.2, P11 | The walked desktop + Thunderbird path does not exist | CONFIRMED (minor) | internal/desktop/capture.go:43-44, :141-142 run only `app.RunGraph` in Incremental | RC27 |
| L1-10 | 1 | §4.11 R-A, R-D, status, verify note | "The only complete copy" is false for `unreadable` (heals next run) and for presumed T3/UNVERIFIED keeps | CONFIRMED (minor) | R-A "the archive now holds the only complete copy"; MA-352 heals; T3 "presumed held" | RC26 |
| L1-11 | 1 | §4.2 KeptAt, §5.4 Clock | KeptAt is overwritten on every KEEP and read by nothing | REFUTED | §4.2 "RFC 3339 UTC of that re-capture"; §5.4 "KeptAt is informational only". No claim says it marks the first loss | — |
| L2-1 | 2 | Whole document; §10 S1/S2; Q5 | Bundling makes F48 wait on the whole gate and an unrelated Q5; un-bundle | REFUTED | Q5 includes "the R2/R13/S24 wording", and today's R13 (scenario-catalog.md:71-77) requires pruning the record of a moved file, which §4.9 row 6 contradicts, so S2 needs the R13 amendment however packaged; §10 S2 is already "Standalone, smallest … no format change" | — |
| L2-2 | 2 | §4.7 W1, Q1 | Q1 offers an incomplete option set; N2 needs only deferred deletion; §1.7 is unreproduced; no stable-name option is offered | CONFIRMED (major) | Q1 "the price of the persisted manifest always naming intact files (P5)"; its only alternative is in-place "plus a post-crash reconcile pass to be designed"; §12 may withdraw §1.7 | RC33, OR1 |
| L2-3 | 2 | P8, W4, Q1 | Page and `-raw` eml bytes follow read, flag and category state, so every full run moves every touched message | CONFIRMED (major) | html.go:418-431, :543, :551-553; graph.go:403-416; mbox.go:249, :274-279; Q1 "a fill, or a refresh by a full run after a renderer change" | RC5, RC33, OR1 |
| L2-4 | 2 | §4.4 MERGE (T2), P3 | MERGE is disproportionate; ship T1-only or KEEP-only first | REFUTED | A scope preference, not a permitted failure: P3 "so the improvement is not starved"; NG8 "is KEPT whole" for ambiguous pre-v7 cases. The real MERGE defect is L1-1 | — |
| L2-5 | 2 | P10, `UnverifiedSentinel` | The sentinel sits in a label list that consumers read as gap labels; its population is unsized | CONFIRMED (minor) | attach.go:121-129; report.go:80-89 maps it to `empty-attachment`; `unknown` arises only from version-1 manifests (manifest.go:541-552) | RC23 |
| L2-6 | 2 | §4.12 archivefs "one gate" | Other copies of the gate stay | CONFIRMED (minor) | extract.go:532 `extractInspect`, extract.go:364, reindex.go:236; §4.12 moves only verify's | RC25 |
| L2-7 | 2 | §4.6 Fill retry | The probe does not stream every attachment today; a full-inventory probe adds reads on every run | CONFIRMED (minor) | exporter.go:571-590 loops `oldMissing` only and short-circuits; §4.6 "It already streams every attachment" | RC24 |
| L3-1 | 3 | W4, §4.6 KEEP stamping, P9 | As L1-2, including the first full run after an upgrade (MA-362) | CONFIRMED (blocker, dissolved) | As L1-2 | RC1 |
| L3-2 | 3 | W1/W2, §4.8, NG5, §5.4 Disk | The 2.5% bound is wrong (12.5% up to 200k); on a near-full disk the Save fails, nothing is freed, orphans ratchet to -v3, -v4, and the archive wedges | CONFIRMED (major; finder blocker) | run.go:279-289 clamp(n/8, 1000, 25000); manifest.go:1383-1423 writes the full temp before the rename; W1 "none … exists on disk"; NG5 never removes. No message is lost | RC8 |
| L3-3 | 3 | §4.1 Item, §4.3 T1/T2 (OMISSION: g3) | Ix matching under g3's frozen fingerprint pairs different items; an absent Held item cannot be carried; T2 over-credits | CONFIRMED (major) | As L1-3 | RC4 |
| L3-4 | 3 | §4.13 (OMISSION: g2, g3), R5 amendment, Q2, ids | g3's repair and g2's re-render rewrite in place and keep v6; the amended R5 makes them violations; Held goes stale | CONFIRMED (major, partial) | mime-fidelity.md:590-600, :668-672; fault-isolation.md:818, :836-841 | RC28, RC30, OR2, OR3 |
| L3-5 | 3 | §4.3 MERGE row vs MA-350, R-A, P3 | A pure regression MERGEs and oscillates every full run; R-C claims "Filled N" | CONFIRMED (major) | The pure-regression half of L1-1 | RC3 |
| L3-6 | 3 | P8, W1, Q1 | As L2-3; a Graph message copied into two folders flips names twice per full run | CONFIRMED (major) | model/message.go contentHash comment "mutable state"; html.go:418-431 | RC5, OR1 |
| L3-7 | 3 | §4.3 row 1 before UNCHANGED, §4.6, §5.4 | The first full Graph run duplicates every pre-v5 record permanently | CONFIRMED (major, partial) | exporter.go:484 `adoptingLegacy`; manifest.go:36-52; graph.go:504; row 1 "COMMIT under a fresh name, earlier files never superseded" | RC17, OR6 |
| L3-8 | 3 | §4.8 G3–G5, §4.2, W1, PagesStale | Permanently blocked entries: unbounded growth, suffixes past -v99, pages regenerated on every run forever; "drained by reindex" is false | CONFIRMED (major) | §4.2 "drained by reindex"; reindex runs the same gates (§4.8 point 3); W1 "four runes" with no cap | RC14 |
| L3-9 | 3 | §4.9 row 3, §6, §5.4 | Existing N2 victims stay RED or are pruned; no heal | CONFIRMED (minor; finder major) | §4.9 "keep (today; the odd case is left alone)". Not a regression | RC36 |
| L3-10 | 3 | §4.3 T3, §4.1 Held, W1 | Unreadable-earlier-zip semantics undefined; W1's "exists" undefined for non-ENOENT stat errors | CONFIRMED (minor) | The corrupt-zip half is covered by P1 "any doubt keeps the old copy"; the W1 stat semantics remain | RC9 |
| L3-11 | 3 | §5.1 C1/C3 | RunGraph never runs SweepOrphans; temps and orphan zips accumulate on Graph and desktop archives | CONFIRMED (minor) | SweepOrphans callers: run.go:135 and reindex.go:91 only; verify.go:451 skips dotfiles | RC20 |
| L3-12 | 3 | §4.2 SupersededEntry (OMISSION: migrations) | A re-key leaves a dead key; G3 passes while the re-keyed row names the path | CONFIRMED (minor) | graph.go:176-207 CollapseByIdentity + `idx.Rekey` | RC21 |
| L3-13 | 3 | §4.8 per-entry directory fsync | Up to 25,000 directory fsyncs per checkpoint on HDD/NAS | CONFIRMED (minor) | §4.8 "fsync the directory, then drop the entry" | RC20 |
| L3-14 | 3 | §5.2, NG10 | A row reverted by power loss is never detected | REFUTED | §5.2 "That leaves a stale row, not data loss … This residual is named here"; NG10 "Index durability is unchanged" | — |
| L3-15 | 3 | §4.5 COMMIT without new raw, W3, §6 | A carried pre-fixity eml stays unrecorded; verify exits 2 | CONFIRMED (minor) | §4.5 "carry the earlier .eml with its Fixity.EML … digest re-checked"; §6 "gaining fixity with names unchanged" | RC35 |
| L4-1 | 4 | §7.3, NG5, verify wording | Crash orphans are illegible; verify blames an old binary; a re-run makes them permanent duplicates | CONFIRMED (major) | verify.go:601 "a leftover from an old-binary excursion … compare bytes before deleting anything"; §7.3 "Manual repair steps go from 1 or more … to 0" | RC8, RC15 |
| L4-2 | 4 | §4.11 R-E, `superseded_pending`, verify note | One cause and one remedy for four causes; reindex cannot clear G4/G5; no file named | CONFIRMED (major) | §4.8 point 3; ux-contract.md:16 (X6 "each WARN/RED naming its remedy") | RC15, RC14 |
| L4-3 | 4 | §4.11 R-B | One remedy for every source; it cannot work for Graph or deterministic sources | CONFIRMED (minor) | R-B "in your mail app, download the message for offline use, then re-run" | RC26 |
| L4-4 | 4 | §7.2 | As L1-9; the desktop Graph user and the scheduled full-run operator are never walked | CONFIRMED (minor) | capture.go:43-44, :140-142 | RC27 |
| L4-5 | 4 | §4.6 KEEP, §4.2 HasIssues, MA-363 | Source-lost rows for complete-at-capture records have a blank subject and date | CONFIRMED (minor) | exporter.go:411-416 stores Subject/Date only `if rec.HasIssues()`; §4.6 "Only Kept and KeptAt change" | RC26 |
| L4-6 | 4 | §4.11 summary, R2, GUI label | UNCHANGED is counted in Exported with no line of its own; the GUI label contradicts | CONFIRMED (minor) | §4.6 "UNCHANGED is counted in Exported"; cmd/mailarchive-gui/main.go:413 "Full — export everything again" | RC26 |
| L5-1 | 5 | §4.6, §4.13 (OMISSION: g3 repair) | g3 adds a fourth, in-place re-capture path with a weaker count check; R5/P5 are false on it; v7 falsifies g3's P12; two R2 texts | CONFIRMED (major, partial; finder blocker) | mime-fidelity.md:577-597, :641-643, :670, :711-713. Contingent on both designs landing | RC28, OR2, OR3 |
| L5-2 | 5 | §2.1 ids, §10 S1 | As L1-7 | CONFIRMED (minor) | As L1-7 | RC30, OR4 |
| L5-3 | 5 | W4 vs P9, §4.6 | As L1-2; PhysID/AltPhysIDs and ContentHash are never refreshed either | CONFIRMED (blocker, dissolved) | exporter.go:415-465, :470-472; manifest.go:956-985 | RC1 |
| L5-4 | 5 | §10 build slices | Producers before their sinks (S5 → S6; S4's entries → R-E in S6); S3 bumps v7 with no behaviour; MA-364 split; S1 skip-encodes unbuilt features | CONFIRMED (major) | report.go:80-89 maps `unverified` to `empty-attachment`; Counts manifest.go:752-764; S3 "none of its own (no behaviour change)" | RC32 |
| L5-5 | 5 | §4.11 run summary, §4.12 | Three summary formatters; a ported copy of the body anchoring; G4 as a new primitive beside `os.Root` | CONFIRMED (minor) | run.go:135 (app imports export); design-extraction-workspace.md:224, :837 | RC25, RC22 |
| L5-6 | 5 | §5.1 C1/C3, §4.8, R-E | RunGraph never sweeps; a `-manifest` outside `-out` is invisible to reindex, verify and status; the dashboard's Rebuild does not drain | CONFIRMED (minor) | job.go:47; reindex.go:48; verify.go:131; health.go:496; maintenance.go:88 | RC20 |
| L5-7 | 5 | §4.2 Key, §4.8 G3 | Key remaps skip Superseded; a PathForKey error is unspecified, so fail-open is permitted | CONFIRMED (minor) | §4.12 "PathForKey(key) (string, bool, error)"; G3 text | RC21 |
| L5-8 | 5 | §4.13 workspace, NG7 | As L1-8 | CONFIRMED (minor) | Workspace P1, MA-414, MA-436; its §4.14 (:757-765) | RC37 |
| L5-9 | 5 | §4.10, §4.11 R-G | Detected bit-rot exits 0 and status stays GREEN | CONFIRMED (minor) | §4.10 "A merge-source failure is a KEEP plus a warning, not an error"; no reason code | RC26 |
| L5-10 | 5 | §4.3 row 1, §4.6 | As L3-7: digest-identical legacy records duplicate | CONFIRMED (major, partial; finder minor) | As L3-7 | RC17, OR6 |
| L6-1 | 6 | §4.9 rows 3 and 6, P7, §4.13, §7.5 | With a stale row, the user deletes the copy they can see, and reindex repairs instead of redacting, or keeps a served earlier copy | CONFIRMED (major, partial) | pages.go:43-64 builds pages from rows; row 6 "re-point … delete nothing; keep the record"; server.go:78 | RC18 |
| L6-2 | 6 | §4.3 T2, §4.2 Kept, sentinel | Labels share the sentinel namespace: a file named `body`, `unknown` or `unverified` breaks T2, `Unknown()` and UNVERIFIED | CONFIRMED (minor; finder major) | attach.go:121-129; manifest.go:57-60, :200-202. Needs exact names and a coincident degradation | RC23 |
| L6-3 | 6 | W1 alternation, §4.8, §5.4 Superseded row, persona 4 | A restored older manifest's stale entry names the current B after alternation; every gate passes; the only copy is deleted | CONFIRMED (major) | W1 "A message alternates between B and B-v2"; SupersededEntry holds only Path/Key/At; manifest.go:525 "restore it from a backup" | RC12 |
| L6-4 | 6 | §8 persona 5, §4.8 run points | StillHeld runs only at cadence checkpoints; the boundary and final commits sweep without it | CONFIRMED (major) | run.go:216 and graph.go:271 only; graph.go:293, :323 | RC13 |
| L6-5 | 6 | §4.9 last row, §8, NG11 | reindex deletes by an index-supplied row path with only `validRelPath`; a suffix-less path deletes another stem; symlinks are followed | CONFIRMED (major) | §8 "G3 compares, it never acts on a row path" vs §4.9 "delete the siblings of rec.Path and of the row path"; reindex.go:131-145 | RC10 |
| L6-6 | 6 | W3 with W2's cleanup, §5.1 | W2's cleanup applied to W3 removes the record's own just-renamed eml and zip; silent on pre-fixity records | CONFIRMED (major) | W2 "every file this attempt created is removed. These are fresh names"; W3 renames over the record's files and states no failure path | RC16 |
| L6-7 | 6 | §4.4 MERGE, §8.1 | A same-envelope forgery MERGEs: a forged page with genuine carried entries; §8.1 understates | CONFIRMED (minor; finder major) | model/message.go:146-161; §8.1 "Worst case: a forced KEEP or a refused merge" | RC35 |
| L6-8 | 6 | §4.4 Construction, §8.1 zip-slip | CreateRaw copies untrusted header fields (Unicode-path extra, symlink attributes); the digest check and the raw read are separate opens | CONFIRMED (minor) | §4.4 specifies only the recomputed Name. C2-2 persona only | RC22 |
| L6-9 | 6 | §8 persona 4 bounds, T3 | The T1 cap comes from untrusted `Held.Size`; no central-directory entry cap | CONFIRMED (minor) | §4.4 "the recorded Size + 1"; T3 "within a size cap" only | RC22 |
| L6-10 | 6 | §4.8 G4 | Lstat-then-Remove races under the C2-2 persona | CONFIRMED (minor) | G4 is verify.go:390-414 followed by a removal | RC22 |
| L6-11 | 6 | §8 persona 3 | The 0600 claim is unconditional; FAT/exFAT has no modes | CONFIRMED (minor) | P5 states its FAT condition inline; persona 3 does not | RC35 |
| L6-12 | 6 | §8 personas (OMISSION: mailbox-write attacker) | An attacker with mailbox write edits a message; a full run COMMITs and deletes the genuine copy | REFUTED | NG2 "Only body presence is compared"; NG3 "the capture is not a regression"; today's Full run overwrites identically, so no new failure | — |
| L7-1 | 7 | §4.1 Held, §4.3 tiers, §5.4 | An earlier unit that cannot be inspected "holds nothing", so EACCES, EIO or a symlink lets a degraded capture COMMIT | CONFIRMED (major) | §4.1 "missing on disk, or fails the no-symlink gate, holds nothing"; verify.go:394-397 maps any Lstat error to `missing` | RC9 |
| L7-2 | 7 | §4.9 last row, "A file that fails the gate is treated as missing" | A symlinked or EACCES `rec.Path` is pruned and its live trio deleted (F48 again) | CONFIRMED (major) | §4.9 "A file that fails the gate is treated as missing"; reindex.go:139-144 plain `os.Remove` | RC10, RC9 |
| L7-3 | 7 | §4.3 T1 "exact", §5.4 "heals it" | Held is not bound to the bytes it describes; a restored backup zip holding more is superseded and deleted | CONFIRMED (major) | T1 "exact"; verify.go:376-379 `saveFixity` updates only Fixity | RC11 |
| L7-4 | 7 | §5.4 Superseded row, W1, G2/G3, persona 4 | As L6-3 | CONFIRMED (major) | As L6-3 | RC12 |
| L7-5 | 7 | OMISSION: a manifest row in §5.4; NG6 | Without a manifest every message is a first capture and R22 does not apply | REFUTED | NG6 "First captures are unchanged … The never-overwrite rule (W1) applies to re-captures only"; identical to today | — |
| L7-6 | 7 | §5.4 Disk, NG5, Q8, §4.8 | As L3-2: self-feeding ENOSPC; up to 100% at n ≤ 1000 | CONFIRMED (major) | As L3-2 | RC8 |
| L7-7 | 7 | W4, P9 | As L1-2; history `gone` events are durable and irreversible | CONFIRMED (blocker, dissolved) | manifest.go:965 "LastSeen.Before(thisRun)"; graph.go:660 | RC1 |
| L7-8 | 7 | §4.4 merge failure, R-G | One message for three causes; a read error is told to restore from a backup | CONFIRMED (minor) | R-G "fails its recorded checksum (modified). Restore that file from a backup" | RC26 |
| L7-9 | 7 | §8 persona 5, §4.8 call sites | As L6-4 (NFS/SMB, a replaced lock file) | CONFIRMED (major; finder minor) | As L6-4 | RC13 |
| L7-10 | 7 | PagesStale, R-E remedy, §7.5 | Deletions on `-pages=false` or all-failed runs leave stale page links; reindex cannot clear G4/G5; the desktop cannot drain | CONFIRMED (minor) | run.go:258; graph.go:324; job.go:54, :88; maintenance.go:88 | RC14, RC20 |
| L7-11 | 7 | §4.2 Held, §5.4, G3 | A malformed Held is unvalidated; a PathForKey error is unspecified | CONFIRMED (minor) | The panic claim is refuted (per-message recover, run.go:521-527, graph.go:629-634); validation and fail-closed G3 remain | RC11, RC21 |
| L7-12 | 7 | W2 (SweepOrphans as the commit marker) | SweepOrphans deletes any zip without a page, including a current record's zip whose page an antivirus quarantined | CONFIRMED (minor) | atomic.go:119-123, run before Load (run.go:131-139). Pre-existing | RC20 |
| L8-1 | 8 | §6, Q2, §4.13 | The v7 bump contradicts three sibling designs' rollback stance | CONFIRMED (major) | fault-isolation.md:836-841, :866; mime-fidelity.md:711-713; local-confidentiality.md:245 | RC29, OR2 |
| L8-2 | 8 | §6 rollback, §10 S3 | Rollback is a data-losing restore whose cost is unstated; S3 can ship the one-way bump with no fix; MSI fleet rollback | CONFIRMED (major) | §6 "restoring the archive … from the pre-upgrade backup"; S3 "no behaviour change"; Product.wxs:37-39 | RC29, RC32, OR2 |
| L8-3 | 8 | §4.3 row 1, §4.6, W3 | As L3-7; W3 also contradicts §4.6 for pre-fixity legacy records; R-F is silent unless something regressed | CONFIRMED (major, partial) | W3 "no Fixity at all … renamed over the earlier files in place"; §4.6 "commits under a fresh name"; R-F "When the comparison finds a regression" | RC17, OR6 |
| L8-4 | 8 | §2.1 ids | As L1-7 | CONFIRMED (minor; finder major) | As L1-7 | RC30, OR4 |
| L8-5 | 8 | §4.13 (OMISSION: g3, g2) | As L3-4, plus the "supersede" vocabulary clash | CONFIRMED (major, partial) | mime-fidelity.md:588-596, :670; fault-isolation.md:818 | RC28, OR3 |
| L8-6 | 8 | §6, §4.9 row 3 | As L3-9 | CONFIRMED (minor) | As L3-9 | RC36 |
| L8-7 | 8 | §6 "It can never apply …" | Pre-guard v2 binaries do not refuse a newer manifest | CONFIRMED (minor) | manifest.go:531-534 "this only protects v3 and later" | RC29 |
| L8-8 | 8 | NG7, §4.13 | As L1-8 | CONFIRMED (minor) | design-extraction-workspace.md:221-225 | RC37 |
| L8-9 | 8 | Q1, verify `-json` v2 | The external costs of `-vN` names are unstated; the last-verify fields are unspecified | CONFIRMED (minor) | The `-json` v2 sub-point is refuted: README.md:588-592 covers "a new optional key", while the bump's reason is a new kind value | RC33 |
| L9-1 | 9 | OMISSION: disk-full / save-failure drill | As L3-2 | CONFIRMED (major) | run.go:357-373 only logs a failed Save | RC8 |
| L9-2 | 9 | Q1, W1, P8 | As L2-3; WriteZip always rebuilds the zip | CONFIRMED (major) | exporter.go:358; html.go:418-431, :551 | RC5, OR1 |
| L9-3 | 9 | §4.8 G4/G5, §4.2, R-E | As L3-8 and L4-2 | CONFIRMED (major; finder minor) | §4.2 "drained by reindex" | RC14, RC15 |
| L9-4 | 9 | §4.6 Fill retry | As L2-7 | CONFIRMED (minor) | exporter.go:571-588 | RC24 |
| L9-5 | 9 | §4.12 recapture.go "ported" | export cannot import app, so the port is a second page parser that drifts | CONFIRMED (minor) | run.go:135; htmlheader.go:43-70 | RC25 |
| L9-6 | 9 | §4.2 growth bounds | The Inventory estimate is about half the real size | CONFIRMED (minor) | manifest.go:1391 compact encoding; about 80 B per 25-character item against "about 45 bytes" | RC35 |
| L10-1 | 10 | §2.1 ids | As L1-7 | CONFIRMED (minor; finder major) | As L1-7 | RC30, OR4 |
| L10-2 | 10 | §4.13, P1, W1/P5, Q2 | "Every re-capture path" and "never writes over" are unconditional claims a sibling falsifies | CONFIRMED (major) | mime-fidelity.md:221-233, :566-592, :670-680, :697-712; fault-isolation.md:567, :818 | RC28 |
| L10-3 | 10 | §4.11 R-A/R-D, P2, T3 | Presumed KEEPs are stated as facts about the source | CONFIRMED (minor; finder major) | T3 "presumed held"; P2 Proven with no tier condition; §4.10 KEEP leaves the gaps "as they were" | RC26 |
| L10-4 | 10 | P5/P6 "Proven", §9 harness | An in-process kill hook observes post-commit state; MA-358 could pass vacuously and wrongly withdraw §1.7 | CONFIRMED (major) | The file-boundary commit runs regardless of runErr; deferred `idx.Close` commits (index.go:251-256); per-message recover (run.go:521-527); checkpoint_test.go:36-58 pattern not specified | RC31 |
| L10-5 | 10 | §5.4 Disk, §4.2 "drained by reindex" | 2.5% is the best case; the overhead is unbounded while a gate holds | CONFIRMED (major) | run.go:279-289 | RC8, RC14 |
| L10-6 | 10 | P8, W2 order, W4, §4.5, MA-357(a), §7.5 | A non-raw full run over a `-raw` archive can never be UNCHANGED; "writes nothing" is false (temps are staged) | CONFIRMED (major) | exporter.go:307-315; html.go:611-613; settings.go:17 (desktop KeepRaw off) | RC6 |
| L10-7 | 10 | P1, P10, §12, §4.3 | The named rows do not exercise T1/T2 inline items, the full-run re-check or the unverified tier; P6 is listed twice | CONFIRMED (minor) | MA-355(c) is T3 only; MA-355(b) asserts only "Retried==0" | RC35 |
| L10-8 | 10 | NG7, §4.13 | As L1-8 | CONFIRMED (minor) | The workspace ruling concerns its selection UI | RC37 |
| L10-9 | 10 | Header "Line numbers are as of 2026-10-09" | Four citations have drifted | CONFIRMED (minor) | main.go:1413 (not :1409); identity_test.go:166; schedule.go:294; exporter.go:145 | RC35 |

## Mechanical check

`docs/design-recapture-integrity.md` revision 2 has BUILD STATUS "approved with
conditions, not built" and cites this review, which exists. Each slice flips its own status
and cites its gate artifact as it lands. Before S4 or S5 code: RC38's lens 3/6/7 addendum
(appended to this file). After the build: the adversarial passes for S2, S4 and S5
(`docs/review-recapture-integrity-adversarial.md`) and the friction review for S6
(`docs/review-recapture-integrity-friction.md`).
