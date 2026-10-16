# Design — re-capture integrity: a re-export never makes the archive worse, and never deletes before it is durable

**Revision:** 2 (2026-10-09). **BUILD STATUS:** approved with conditions, not built; owner
rulings folded (`docs/owner-rulings-2026-10-09.md`).
The 10-lens pre-code review is filed as `docs/review-recapture-integrity-predesign.md`:
GO_WITH_CONDITIONS, 97 lens findings (91 confirmed, 6 refuted) plus two from the rescue
pass (H1, H2). Conditions **RC1..RC38** are folded into this revision, and the build must
not start from revision 1. Nothing here is built. No code, catalog row or test changes with
this document.

Two gates stand before code:
- **RC38:** lenses 3, 6 and 7 re-run on this revision, as an addendum to the review, before
  any S4 or S5 code (RR-1). RC1, RC2, RC5, RC9 and RC12 change the verdict, write and
  deletion paths, which is a material change.
- **Owner rulings OR1..OR7** (§11: Q1, Q2, Q5, Q7, Q8 re-premised; Q10 and Q11 new) each
  block the slice they name. **[ruling]** All are answered in
  `docs/owner-rulings-2026-10-09.md` §5 and the joint rulings of its §1, and each change
  they make here is marked `[ruling <id>]`. Two are escalated to the owner: Q1 / OR1 as
  **ESC-3** and Q5 / OR5's amended guards as **ESC-2**. Until the owner answers, S1 lands
  only the additions and the confirmed-open encoders, S2 (without its two held cases) and
  S3 land, and S4..S7 do not start.

Key conditions:
- **RC1** every non-writing outcome stamps the observation (W4 runs W5's record switch).
- **RC2** HEAL: an UNCHANGED re-capture repairs a damaged earlier unit (the review's hidden
  blocker).
- **RC3** MERGE needs an improvement, and an absent item is never carried.
- **RC4** items match by label and content, never by index.
- **RC5** UNCHANGED is decided by content, not page bytes (pending OR1, escalated as ESC-3
  [ruling ESC-3]).
- **RC8** writing beside never consumes the space the manifest save needs.
- **RC9** the earlier-unit gate distinguishes absent from refused and unreadable.
- **RC12** deletion is bound to content and to the superseding copy.
- **RC28 / RC29** the sibling in-place writers and the manifest version are joint rulings.

**Ids are allocated [ruling Q11 = J1].** Revision 2's provisional R22, S42 and S43 are now
**R28**, **S50** and **S51**, which this document uses throughout; the rows keep
MA-350..364. They are proposals for the OPERATOR to accept into `docs/scenario-catalog.md`,
landing with S1's catalog-first PR.

**The owner's decision relayed on 2026-10-09** (the extraction workspace: select the
source, choose the export destination, copy; no drag-and-drop) is honoured. No condition
adds a UI control, a drag-and-drop path or an interaction surface (NG7, RC37).

**Revision 2 condition map** (review → where it lands):

| Condition | Lands in |
|---|---|
| RC1 stamping on non-writing outcomes | P9, §4.6, §4.7 W4, §5.1, §9 MA-356/362 |
| RC2 HEAL | §2.1 R5/R28, P12 (new), §4.1, §4.3, §4.7 W4, §4.10, §4.11 R-H, §5.4, §6, §9 MA-362 |
| RC3 MERGE gate, absent never carried | P3, §4.3 verdict, §4.4, §9 MA-350/356 |
| RC4 label + content matching, parse scheme | §4.1, §4.2, §4.3, §4.13, §9 MA-355 |
| RC5 content-based UNCHANGED | §2.1 R2, P8, §4.2, §4.3, §4.7 W4, §11 Q1, §9 MA-362 |
| RC6 post-verdict eml link | P8, §4.5, §4.7 W2, §9 MA-357/362 |
| RC7 MERGE eml rationale | P4, §4.5 |
| RC8 space safety, adoption | NG5, §4.7 W1, §4.8, §4.11 R-I, §5.4, §9 MA-358/364 |
| RC9 absent / refused / unreadable | §4.1, §4.3, §4.5, §4.7 W1, §4.12, §9 |
| RC10 reindex deletes only manifest-named paths | §4.9, §4.13, §8, §9 MA-361 |
| RC11 Held bound to bytes, validated | §4.2, §4.3, §5.4, §4.12 verify, §9 MA-364 |
| RC12 G6/G7, G1 stem shape | §4.2, §4.8, §5.4, §9 MA-360 |
| RC13 lock re-check before deletion | §4.8, §8, §9 MA-360 |
| RC14 bounded blocked removals | NG11, §4.2, §4.7 W1, §4.8, §4.11 R-J/R-K, §9 |
| RC15 per-cause remedies, `earlier-copy` | §4.11, §7.3, §9 MA-363 |
| RC16 W3 failure path | §4.7 W3, §5.1, §9 MA-359 |
| RC17 legacy adopts | NG4, §4.3, §4.6, §4.7 W3, §4.11 R-F, §5.4, §6, §9 MA-356 |
| RC18 redaction reach, loud stale-row lines | P7, §4.9, §4.13, §7.5, §9 MA-361 |
| RC19 R28 restated | §2.1 |
| RC20 live-path and maintenance housekeeping | §4.8, §4.12, §5.1, §9 MA-358 |
| RC21 migrations, G3 fail-closed | §4.2, §4.8, §9 MA-364 |
| RC22 merge headers, bounds, `os.Root` | §4.4, §4.8 G4, §8, §9 MA-353/354 |
| RC23 typed status fields | §4.2, §4.3, §4.10, §9 MA-355 |
| RC24 two-phase probe | §4.6, §9 MA-351 |
| RC25 one gate, one body reader, one summary | §4.11, §4.12 |
| RC26 cause-specific wording | P2, §4.10, §4.11, §9 MA-354/355/363 |
| RC27 operator walks | P11, §7 |
| RC28 sibling writers (g2, g3) | §2.1 R5, P1, P5, §4.6, §4.7 W1, §4.13, §11 Q10 |
| RC29 version policy | §2.1, §4.2, §6, §11 Q2 |
| RC30 provisional ids | this header, §2.1, §9, §11 Q11 |
| RC31 kill-point harness | P5, P6, §9 MA-358, §12 |
| RC32 slices ship their sinks | §9, §10 |
| RC33 Q1 re-premised | §1.7, §11 Q1 |
| RC34 client placeholders, lab-pending labels | §1.1, §4.3, §7.1, §9 MA-350 |
| RC35 inline conditions, numbers, citations | P1, P10, §4.2, §4.4, §4.5, §8, §9, §12, this header |
| RC36 N2 victims adopted | P6, §4.9, §9 MA-361 |
| RC37 workspace seam, NG7 | NG7, §4.13 |
| RC38 lens 3/6/7 re-run | this header, §10 |

**Findings addressed (verified review findings of 2026-10-08, plan group g4):** F46
(high), N2 (medium), N3 (low), F48 (medium). While reading the code for this design,
three more defects of the same class turned up (§1.5–§1.7). They are stated as
**code-reading results, not yet reproduced by a test**. MA-356, MA-357 and MA-358 pin them.

**Line numbers** are approximate as of 2026-10-09; the named function or statement is
authoritative (RC35). A concurrent in-flight change (Record.BodyFallback,
`html-shown-as-source`) moved `internal/export/exporter.go` by up to six lines, so the
findings' own citations (exporter.go:234, :481) now sit at :235 and :487. Each citation
below also names the function or statement, so it survives further drift.

**Gates this design owes:** the 10-lens pre-code design review now. After the build:
adversarial passes for slices S2, S4 and S5, and a friction review for S6 (§10).
**Coordination** with other in-flight fix groups is in §4.13 (g2 N1 and the supersede
re-render; g3 mime-fidelity's P11 repair and parse scheme; g7 F49/F50/F53; g56
C2-2/C2-3/C1-2; the BodyFallback change; the manifest version; the extraction workspace).

---

## 1. Problem

### 1.1 F46 — a re-capture replaces a good archived copy with a worse one

`Exporter.Export` (exporter.go:145) reads the earlier record only in Incremental mode
(`if e.Mode == Incremental && seen`, exporter.go:235). In Full mode, every
already-archived message is captured from the source as it stands now and committed
without conditions:

- the `.eml` is overwritten first (`writeFileAtomicDigest(… rawName …)`, exporter.go:338);
- the zip is rebuilt by `WriteZip` (exporter.go:358). It is deleted outright when no
  attachment yields bytes (`os.Remove(zipPath)` in WriteZip, attach.go:98-101) or when
  nothing archivable is left (exporter.go:382);
- the html is overwritten (exporter.go:385);
- the record is rebuilt from this capture's gap set (`rec.Missing = gaps`, exporter.go:409).
  This is B2 of design-incremental-completeness: "the record carries the freshly computed
  sets".

A **fill retry** (an Incremental run over a fillable record) is gated by
`probeImproves` (exporter.go:271, defined at :571-590). The probe looks only for
improvements, never for regressions. A retry where one item arrived while another went
bad therefore commits the regression as well. `TestRetryPromotesOnAnyRecoveredItem`
(internal/export/completeness_test.go:133, MA-66) accepts this explicitly. A legacy
record carrying the `unknown` sentinel always probes as improved, so its first
re-examination commits whatever the source holds now.

**Triggers:**
- the GUI's "Full — export everything again" (cmd/mailarchive-gui/main.go:413);
- the README's `-mode full` Thunderbird prep (README.md:56, :950);
- the full re-export the README suggests for gaining fixity (README.md:764, :908);
- a scheduled `-mode full` job;
- a fill retry where another item improved;
- the first incremental pass over a version-1 manifest.

**Realistic degradations** (*conditional, lab-pending*: none is yet confirmed on a real
profile, §9 Lab; RC34):
- Thunderbird "detach attachment": the part keeps its name but its bytes are replaced. The
  client writes a placeholder part (`X-Mozilla-External-Attachment-URL`,
  `X-Mozilla-Altered`) that may be **non-empty**. RC34 treats such a part as "now absent"
  whatever its byte count, so it cannot commit as an NG3 `changed`. Thunderbird "delete
  attachment" renames the part ("Deleted: x", `text/x-moz-deleted`), which changes the
  fingerprint: the record #fp-splits (§1.2) and both copies survive;
- an Outlook offline window shrunk, so older bodies are evicted from the `.ost`. This
  usually removes whole messages, and then no re-capture happens at all;
- IMAP cache eviction in Thunderbird or Evolution (same caveat);
- a torn read. The finder's PoC reproduces this one.

**Result:** the archive deletes its own good copy. It then reports the item as a fillable
"still missing content" gap, blaming the source for a loss the archive caused, and
`verify` attests because the new record carries no zip digest. Once the source no longer
holds the bytes, the loss is permanent.

**Finder's evidence (PoC, deleted):**
- Full mode: `run1 complete=true zip=report.pdf body=true` → `run2 full ok=true missing=[body report.pdf] zip=<no zip>`.
- Torn fill: `run1 missing=[b.pdf] zip=a.pdf` → `run2 missing=[a.pdf] zip=b.pdf filled=1`. A third run with a.pdf readable again self-healed.
- Legacy sentinel over an empty source: `missing=[old.pdf] zip=<no zip>`.

### 1.2 The guard that already exists (verified) and where it stops

`Message.Fingerprint` hashes each attachment's name together with its index
(internal/model/message.go:153-160, the loop at :157). On the **folder-scoped local
path**, removing an attachment entirely changes the fingerprint. The #fp-split loop
(exporter.go:220-232) then files the capture under a qualified key, and the old record
and its zip survive. The finder verified this variant; it is **not** a bug. So on that
path, only two things are destroyed today:
- same-name items that are now **empty or unreadable**;
- a **body** that is now missing (bodies are deliberately outside the fingerprint).

The guard does not hold in two live-path cases:
- immutable-id placement (§1.6);
- a legacy-scheme adopt (exporter.go:220-221, :484), where fingerprints are incomparable
  by design.

### 1.3 N2 — the old files are deleted before the new path is durable

On a re-capture whose file stem changed, `Export` deletes the previous html, zip and eml
(exporter.go:485-490, `os.Remove(old)` at :487) immediately after the in-memory
`Manifest.Add` (exporter.go:470). The manifest is saved only at a checkpoint
(run.go:208-224, graph.go:259-276: every `effectiveEvery` exports, 1,000 to 25,000) or at
a file or mailbox boundary (run.go:243, graph.go:293).

**Effect of a hard kill in that window** (SIGKILL, power loss, OOM, a scheduler
TerminateProcess; SIGINT and SIGTERM are handled gracefully):
- The persisted manifest, and the index if its batch had not committed, point at deleted
  files. The new file sits on disk unrecorded.
- The next incremental run sees a complete record and skips it, so nothing ever repairs it.
- `verify` reports the old path missing and the new one unexpected; `status` goes RED.
- `reindex -rebuild` then prunes the record, so the message vanishes from the manifest,
  search and the folder pages.

Realistic trigger: the README-recommended full re-export of a v0.1.x or v0.2.x archive,
which renames every stem the 68a4096 naming rules changed. One crash can strand up to one
checkpoint interval of records. Finder's PoC: `old file exists=false, new file exists=true`;
persisted path `…_OLDRULE_92544786.html`; the next run reports `exported=0 skipped=1`;
verify reports `missing=1 unexpected=1 attested=false`.

### 1.4 N3 — the zip is replaced or removed before the html is written

On a re-capture, the zip step runs first: WriteZip deletes the old zip (attach.go:101), or
exporter.go:382 does, or the new zip is renamed over the old one. Only then is the html
written (exporter.go:385). If that html write fails (ENOSPC, or on Windows a sharing
violation from an antivirus or indexer handle on the existing html), `Export` returns an
error and the record keeps the old `Fixity.Zip`. Two outcomes:
- the zip was deleted: `verify` reports it `missing` while the old html still links it;
- the zip was replaced: `verify` reports `modified`, a false bit-rot alarm, and
  ARCHIVE-INTEGRITY-ATTENTION.txt tells the user to restore from a backup.

A complete record is skipped by incremental runs, so either state persists until the next
full run. This contradicts the documented order in design-incremental-completeness.md §3.2
step 4: rename the zip, then the html, then remove a stale zip. Finder's PoC:
`zip exists=false, verify missing=1` and `zip exists=true, verify modified=1 (size 153, recorded 159)`.

### 1.5 (new, code reading) A re-capture without `-raw` drops or un-fixes the preserved original

`rawName` is set only under `-raw` when the source has bytes (exporter.go:307-310), and the
record's `Fixity` is rebuilt from scratch (`var fx state.Fixity`, exporter.go:336). So a
full re-export **without** `-raw` of an archive captured **with** `-raw` does one of two
things:
- **unchanged path:** the old `.eml` stays on disk, but the record loses `Fixity.EML`, so
  `verify` reports the eml `unrecorded` (NOT attested, exit 2);
- **changed path:** exporter.go:489 deletes the old `.eml` outright. The preserved original
  that `extract` depends on (R20) is gone.

With `-raw` over a degraded mbox, the eml is overwritten with the degraded original before
anything else is written (exporter.go:338). Not yet reproduced by a test; MA-357 pins it.

### 1.6 (new, code reading) The live path's immutable-id placement bypasses the fingerprint guard

On the live path with a Graph immutable id, `placeByPhysID` (exporter.go:668, called at
:212-217) adopts the sibling that stores the same id and sets `physResolved`. That
**skips** the fingerprint split (`for !physResolved && …`, exporter.go:220). A Full-mode
Graph run (graph.go:502, "Full mode re-downloads everything") can meet a message whose
attachment was removed server-side under the same immutable id: Outlook's "Remove
attachment", or a retention/DLP strip. That message reaches the commit under the same key,
and the removed attachment's zip entry, or the whole zip (exporter.go:382), is destroyed.

*Conditional* on Exchange keeping the immutable id across an in-place attachment removal.
That is expected but needs a real tenant to confirm (lab-pending). MA-356 pins it with the
fake Graph server.

### 1.7 (new, follows from the same code) An unchanged-path re-capture is not crash-consistent with recorded fixity

A re-capture at an unchanged path renames new bytes over the recorded files in place
(`commitTemp`, atomic.go:32-49), while the record changes only in memory (exporter.go:470).
After a hard kill before the next save:
- the files' bytes no longer match the persisted `Fixity`, so `verify` reports `modified`
  and `status` goes RED;
- ARCHIVE-INTEGRITY-ATTENTION.txt tells the user to restore from a backup, which may be an
  older and worse copy;
- incremental runs never re-capture a complete record, so the false alarm persists.

This is the unchanged-path analogue of N2. It is not a listed finding. **RC33:** S1's
encoder for this section, run under the RC31 harness, must show it RED on today's code
before the owner is asked Q1. It is the only defect that needs a new name on an
unchanged-path re-capture (N2 needs only deferred deletion).

### 1.8 F48 — `reindex` deletes a message's current, intact files when its index row is stale

`Reindex` decides that a row is "gone" from the row's own `relPath` (reindex.go:108-116).
It then calls `deleteMessageFiles(rec.Path)` on the **manifest's** path (reindex.go:147-151)
without checking whether that path equals the gone path or is itself missing. The comment
at :127-128 assumes "here the .html is already gone".

How a row goes stale: a message is renamed (as in §1.3), then its index update is lost to
an index Add error that rolls back the whole batch (F49, index.go:226-233) or to a power
loss (F81). The run's own warning (cmd/mailarchive/main.go:1413) then tells the operator
to run `mailarchive reindex`, which deletes the live html, zip and eml and the manifest
record. For Graph mail already gone from the mailbox, the loss is permanent. Finder's PoC:
`reindex: kept=0 pruned=3 err=<nil>; manifest records now=0; message html files on disk=0`.

---

## 2. Properties

### 2.1 Proposed invariant and amendments (catalog edits land in slice S1)

All ids here are provisional (RC30, header): the operator allocates them (OR4, Q11) before
S1, and the amended texts need the owner's acceptance (OR5, Q5). **[ruling J1, J7, ESC-2]**
The ids are allocated (R28, S50, S51). The new R28, S50 and S51 and the additions marked
below land with their slice; the amendments to existing text are escalated to the owner
(ESC-2) and land only if the owner accepts them. Each item is marked.

- **R28 (new, allocated [ruling J1]) — Re-capture is monotone** (restated by RC19). A re-capture
  of an already-archived message never makes an item the archive holds — the body, an
  attachment, an inline image, the preserved original — empty, unreadable or absent. It
  deletes an earlier file only after no persisted manifest record or index row references
  it. Folder pages are a regenerated view, not a durable reference: they follow
  `PagesStale` (§4.8). Exceptions, stated here rather than elsewhere:
  - **NG2:** body kind and length are not ranked; only body presence is compared;
  - **NG3:** same-name attachments whose non-empty size changed are not judged;
  - **NG4 (DEMOTE):** a regressed legacy-scheme adopt commits, and its earlier file is kept
    on disk and owned through the new record's AlsoFiles, rather than recorded as kept
    (RC17);
  - **HEAL (RC2):** a re-capture whose staged bytes equal the recorded fixity may rename
    them over a missing or damaged earlier unit in place.

  A capture that would degrade is not committed (KEEP), or is merged with the held items
  carried forward (MERGE). Either way it is recorded on the record as *kept* and shown by
  the run summary, the report, `status` and `verify`. The scope is re-captures through the
  shared exporter commit; the sibling writers of §4.13 are governed there (RC28).
  **[ruling J7]** New text: lands in S1. **[ruling Q10/OR3 = J6]** The mime-fidelity repair
  is such a re-capture, so R28 governs it too.
- **R1 (amend, add):** "…an item kept from an earlier capture that the source no longer
  delivers is not a gap: it is recorded as kept, never as fillable or terminal."
  **[ruling J7]** An added sentence: lands with S5.
- **R2 (amend; one joint text with the mime-fidelity design, RC28):** "`full` re-examines
  and re-captures all. A re-capture whose content is unchanged rewrites nothing (RC5), one
  that would degrade is kept (R28), and one that finds an archived file missing or damaged
  restores it from identical bytes (RC2)." **[ruling ESC-2]** It changes R2's existing
  text, so it is escalated; until the owner answers, R2 stays as it is.
- **R5 (amend, add; scoped by RC28):** "…a re-capture through the shared write path that
  changes a message's bytes writes the new files beside the old ones. At every instant, the
  persisted manifest names a complete set of files that match their recorded fixity, except
  that a HEAL (RC2) writes the recorded bytes in place. Superseded files are removed only
  after the manifest that no longer references them is durable, and an interrupted removal
  is finished by the next run or by `reindex`." The fault-isolation stub re-render is a
  stated exception (§4.13, OR3). **[ruling Q10/OR3]** That exception is dropped:
  fault-isolation revision 2 supersedes metadata only, writing no file (its FC6(4), FC7),
  so there is no stub re-render to except. **[ruling J7, ESC-3]** The write-beside
  sentence lands with S4 only if the owner answers ESC-3 with (a); under (b) or (c) the
  new files are not written beside the old ones, and the sentence is rewritten to match.
- **R13 (amend):** "…no exported message file is deleted, except (a) a pruned record's
  siblings and AlsoFiles when the record's own file is **absent** (ENOENT; a file that
  cannot be inspected is never treated as missing, RC10), (b) superseded earlier copies
  the manifest lists, and (c) on a redaction prune, the unowned `-vN` family of the redacted
  stem (RC18). An index row naming an earlier file of a record whose current file is intact
  is repaired, never pruned, and a row path is never deleted directly (RC10). Nothing the
  manifest's current record names is deleted while it exists." **[ruling ESC-2]** The new
  deletion cases (b) and (c) are escalated, and so is N2 adoption (§4.9, RC36), which keeps
  a record whose recorded file is missing where R13 says it is pruned. Until the owner
  answers, R13 stays as it is; S2 lands only the parts that restore it as written (the
  absent-only rule, archivefs-gated deletion, the row repair).
- **S24 (amend response):** "…a renamed message's old html/zip/eml are removed once the
  manifest naming the new file is durable." **[ruling ESC-2]** Escalated.
- **S39 / MA-201 (amend only if OR2 rules for a bump):** "Load refuses a stored version
  above **7**". The default is no bump (RC29), and then S39 is unchanged. **[ruling Q2/OR2 =
  J2]** No bump: S39 and MA-201 are unchanged.
- **New scenarios (allocated ids [ruling J1]; they land in S1):**
  - **S50**: an archived message is re-captured while its source holds less;
  - **S51**: a re-capture is interrupted, or `reindex` meets a stale row.

### 2.2 Properties

Every claim is labelled. **Proven** means proven by the named MA rows *once built*. Nothing
is built yet.

- **P1 — Never degrade (monotone re-capture).** Serves R28 (new) and R1. On every
  re-capture path through the shared exporter commit (full, fill retry, legacy
  re-examination; local and live) — *conditional* on §4.13's rule for the mime-fidelity
  repair and the fault-isolation stub re-render, which are governed there until OR3
  (RC28); **[ruling Q10/OR3 = J6]** OR3 puts the mime-fidelity repair on this path (it
  takes only UNCHANGED, COMMIT or KEEP, never MERGE), and the fault-isolation stub re-render
  no longer exists — consider each item the archive holds: the body; each attachment or inline image,
  identified by label and occurrence, or by content (RC4); the preserved original. If the
  new capture's counterpart is empty, unreadable or absent, the earlier bytes survive.
  Either the earlier files are kept and the capture is not committed (**KEEP**), or the
  earlier bytes are carried into the new files (**MERGE**, P3).
  - **Proven** (MA-350, 351, 352, 355, 356) where the earlier inventory is recorded and
    bound to the on-disk bytes (tier T1, RC11), or gap-derived with an identical
    fingerprint under the same parse scheme (tier T2, RC4), §4.3. MA-350 and MA-355 carry
    T1/T2 inline-image twins (RC35).
  - **Conditional** for tier T3 (a legacy record with neither a recorded inventory nor a
    comparable fingerprint): it is conservative by construction, and any doubt keeps the
    old copy. That includes an earlier unit that is refused or unreadable (RC9). The stated
    cost is that T3 can refuse a capture that was in fact no worse, which means a missed
    refresh, never a loss.
- **P2 — Kept is recorded, never misreported as missing.** Serves R1 and X6. An item kept
  from an earlier capture is never listed in Missing or Terminal. The record names it in
  `Kept`/`KeptBody` with `KeptAt` and `KeptHow`, the report lists it as kind `source-lost`
  with class `kept`, and `status` and `verify` count it. **Proven** (MA-350, 363).
  *Conditional* for presumed keeps (T3, UNVERIFIED, refused or unreadable units): the
  archive does not know the source holds less, and the wording says so (RC26 (a)).
- **P3 — Merge carries kept attachments forward.** Serves R28 and R1. A re-capture may
  improve one item while regressing only zip-held attachments. The new zip then holds the
  improved items plus the earlier bytes of the regressed ones, so the improvement is not
  starved.
  - **Conditional:** at least one item or the body improved (RC3); every regressed item is
    still listed by the new message (an absent item is never carried, RC3); the earlier
    inventory is T1, or T2 with an unambiguous entry name (§4.4); in T1 the new body digest
    equals `Held.BodyDigest` (RC35 (a)); the earlier zip passes its recorded fixity (or,
    with none recorded, every carried entry passes its CRC-32); and the page unit did not
    regress. Otherwise P1's KEEP applies and the improvement waits (§3 NG8). **Proven**
    under that condition (MA-353, 354).
- **P4 — The preserved original is never degraded or dropped by a re-capture.** Serves
  R20 and R28. Re-capturing without `-raw` keeps the earlier `.eml` and its `Fixity.EML`,
  and the page still links it (RC6). A re-capture that keeps or carries any item never
  writes the new, degraded raw bytes. When a message moves, its `.eml` moves with it. A
  carried pre-fixity `.eml` is digested from the bytes kept or copied, labelled as a
  baseline like `verify -record` (RC35 (f)). **Proven** (MA-357).
  - **Residual (RC7):** on a merged record no original holds every item. The carried
    earlier `.eml` is the faithful bytes of the capture whose items were kept; the filled
    item lives only in the zip.
- **P5 — Crash-consistent re-capture.** Serves R5. At every instant of a re-capture through
  the shared write path that changes bytes, the persisted manifest names a complete set of
  files that exist and match its recorded fixity. New files are written beside the old
  ones under a name no file occupies, and never over a recorded file (*conditional* on
  §4.13's rule for the sibling writers, RC28; **[ruling J6]** the mime-fidelity repair now
  writes through this path; **[ruling ESC-3]** how a changed re-capture names its files is
  escalated, so S4 waits).
  - **Proven** (MA-358, 359) for process death (namespace plus order), *conditional* on the
    kill-point harness of §9 (RC31), which snapshots the archive at Cn before any defer or
    return can commit.
  - **Conditional for power loss:** the filesystem must honour fsync (ext4, APFS and NTFS
    do; FAT and exFAT do not), as in design-incremental-completeness P4.
  - **Stated exception (§4.7 W3):** a record that carries no fixity at all may be rewritten
    in place. A kill then cannot cause a false integrity alarm, and P1 already guarantees
    the bytes are no worse. A failure after the first rename-over removes only temps
    (RC16).
  - **Stated exception (§4.7 W4, HEAL, RC2):** a unit whose staged digest equals its
    recorded fixity may be renamed over a missing or damaged earlier copy in place. A kill
    then leaves the unit still damaged or healed, never a false alarm.
- **P6 — Deferred deletion.** Serves R5, R8 and R13. A superseded file is deleted only
  after:
  - (a) the manifest that no longer references it is durable;
  - (b) no record owns it (Path or AlsoFiles);
  - (c) the search index row of the superseding record no longer names it, or no index
    exists;
  - (d) its path passes the component-wise no-symlink gate;
  - (e) its page still holds the superseded bytes, and the superseding record's own page is
    present (G6, G7, RC12);
  - (f) this process still holds the archive lock, re-checked just before deleting (RC13).

  A deletion interrupted at any point is finished by the next run's start-of-run sweep or
  by `reindex`. **Proven** (MA-358, 360) for process death, *conditional* on the RC31
  harness. *Conditional for power loss:* the directory fsync after the removals must be
  honoured (ext4, APFS and NTFS do; FAT and exFAT do not; RC36). Power-loss rollback of the
  index is a stated residual (§5.2).
- **P7 — `reindex` never deletes a current record's intact files.** Serves R13 and R8. A
  row naming an earlier file is re-pointed to the record's current file, and nothing is
  deleted. A current file that cannot be inspected is never treated as missing (RC10). The
  redaction behaviour (delete the html, then reindex) is unchanged *for a fresh index row*;
  during a stale-row window the repair prints both paths and the redaction remedy (RC18).
  **Proven** (MA-361).
- **P8 — An unchanged re-capture writes nothing.** Serves R2 (amended) and durability. When
  the content is unchanged (RC5: `Held.Content`, `Held.Render`, an equal inventory and an
  equal zip digest) and the earlier units are intact (RC2), no archived file is written,
  renamed or deleted. Temps may be staged and discarded (RC6). **Proven** (MA-362).
  - **Conditional** on its real inputs (RC6): the render scheme, the render options
    (including which `.eml` the page links, decided after the verdict), and, for pre-v7
    records without `Held.Content`, the mailbox state the page renders (read, flag and
    category state, transport headers). It is also conditional on byte-determinism of zip
    for a given build: neither render nor zip has a time or random input (html.go never
    reads the clock, and attach.go:83's `zw.Create` leaves `Modified` zero). A miss only
    costs a rewrite under a new name, never correctness.
  - **Triggers that do move files** (stated for Q1, RC5): a content change, a renderer
    release (once per record), `-raw` newly turned on, a parse-scheme repair.
- **P9 — A non-writing outcome still records the observation** (extended by RC1). Serves
  R21 and R17. On the live path:
  - an **UNCHANGED** re-capture runs W5's record switch at `T = prev.Path` without file
    writes (§4.7 W4), then `OnExported`, exactly as today's Full mode;
  - a **KEEP** or **UNVERIFIED** stamps `LastSeen`, `Present` and `Folder` and writes the
    history folder-assertion on a move or present-again, exactly as an incremental skip
    does.

  Full mode never false-gones an unchanged or refused message. **Proven** (MA-356, 362).
- **P10 — Legacy records end honest.** Serves R1. An `unknown` record whose re-examination
  would degrade keeps its files and is marked `Unverified` (a typed field, RC23). That
  status is terminal: incremental runs never re-capture it, full runs re-check it (by tier
  T3, since its fingerprint was never compared). The record is never claimed "complete" and
  is never fillable forever. **Proven** (MA-355, including a full-run re-check twin, RC35).
- **P11 — Legible outcomes.** Serves X2, X6, X10 and X11. The run summary (CLI, GUI, and
  the desktop Activity log, which runs only Graph incremental and so shows R-E and the
  unchanged count only; RC27), the report, `status` (text and `-json`) and `verify` (text
  and `-json`) show kept, merged, healed, unverified and pending-removal outcomes. One
  `app.RecaptureSummary` produces the text for every surface (RC25). Each line says what
  happened, why, and the remedy, or says no action is needed. **Proven** for the wording
  and counts (MA-363).
  - **Aspirational** until the friction review: that an inheritor understands `-vN` names
    and `source-lost` rows from README.txt alone (§7.5).
- **P12 — A full re-export heals what it can reproduce exactly** (new, RC2). Serves R5 and
  R28. When a Full-mode re-capture's staged unit equals the recorded fixity while the
  archived unit is absent or damaged, the staged unit is renamed into place, and nothing the
  on-disk copy holds is lost: a damaged unit that holds something the staged one lacks gives
  KEEP. The tool's RED remedy ("re-export them from the source") therefore keeps working.
  **Proven** (MA-362's HEAL twins). *Conditional* on Full mode hashing each earlier unit
  once (a stated cost, §6).

---

## 3. Non-goals (explicit scope cuts)

- **NG1 — No flag to force a downgrade.** Removing content from the archive is redaction:
  delete the files, then `reindex`. A re-capture is never a removal path. Per-attachment
  redaction is not added (it never worked reliably before either: a removed attachment
  splits the record, §1.2).
- **NG2 — Body kind and length are not ranked.** Only body *presence* is compared. HTML
  degrading to plain text, a truncated body, or a body shown as escaped source
  (`html-shown-as-source`, the concurrent BodyFallback change) still counts as a present
  body.
- **NG3 — Same-name attachments whose size changed are not judged.** When both the earlier
  and the new bytes are non-empty but differ in length, the capture is not a regression.
  The size change is counted (`changed`) and logged. A parser fix can legitimately change
  bytes (re-encoding fixes, embedded-message extraction). See Q3. **[ruling Q3]** Commit,
  counted as `changed`; empty, absent and `.partial` items are still caught.
- **NG4 — Legacy-scheme adopts are exempt from KEEP** (§4.6): their identity is not
  content-verified, and a KEEP could drop a distinct Message-ID reuse (an R1 loss). A
  legacy record whose staged bytes equal its recorded fixity is adopted in place with no
  write (RC17), since identical bytes cannot be a distinct reuse. Otherwise the capture
  commits under a fresh name and the earlier file is kept on disk, now **owned** through the
  new record's AlsoFiles, counted and reported (RC17). The one-time growth this causes is
  stated in §6, and Q7 (OR6) decides whether to keep it. **[ruling Q7]** It is kept: DEMOTE,
  because KEEP would silently leave a distinct Message-ID reuse uncaptured (R1).
- **NG5 — Orphans left by a crash before the checkpoint are not removed automatically.**
  The new files of a re-capture killed before its record was saved stay on disk. They are
  never deleted by a sweep, because they may hold the newest content (Q8, OR7; **[ruling
  Q8]**: never removed automatically outside redaction, and listed as `earlier-copy`). RC8 makes
  them rare and short-lived: the next re-capture of the same message adopts a candidate
  trio whose digests equal its staged bytes. `verify` names any that remain as kind
  `earlier-copy`, with the current path and the cause (RC15), never as `unexpected`.
- **NG6 — First captures are unchanged.** A message never archived before is written as
  today. The never-overwrite rule (W1) applies to re-captures only.
- **NG7 — No new UI, flag or interaction surface** (restated by RC37). This design adds
  counts and text lines to existing surfaces only, and rewords one existing GUI label
  (RC26 (e)). That is this design's own scope decision: a re-capture's outcomes are
  reported, never chosen by the user, and NG1 rules out a force-downgrade control. It is
  also consistent with the owner's decision relayed on 2026-10-09 for the extraction
  workspace (select a source, choose an export destination, copy; no drag-and-drop), but
  does not rest on it. The workspace never writes the archive; its seam with this design
  is its read side (§4.13).
- **NG8 — Pre-v7 records do not always merge.** Merge needs a known earlier entry name. A
  tier-T2 record with an ambiguous name, or any tier-T3 record, whose re-capture is mixed
  (one item improved, another regressed) is KEPT whole, and the improvement waits until a
  no-worse capture is possible (Q6). **[ruling Q6]** A one-time inventory backfill, slice
  S7 after S5, reads each earlier zip's central directory into `Held` where the zip matches
  its recorded `Fixity.Zip`, never re-baselining fixity. Until it lands the starvation is
  an open gap, not accepted.
- **NG9 — Fault isolation of a failed message is out of scope.** A write failure here still
  returns an error from `Export`. Whether the store walk continues is g2's N1. This design
  only guarantees that the earlier files and record are intact when that happens.
- **NG10 — Index durability is unchanged.** search.db keeps `synchronous=NORMAL`
  (index.go:32). The deletion gates tolerate an index that later reverts (§5.2), and the
  repaired `reindex` heals it.
- **NG11 — The write path still follows symlinked folders inside the archive** for first
  captures. That is g56's C2-2. The new *deletion* path refuses symlinked components
  (stricter than today's exporter.go:487-489), and so does every reindex deletion (RC10).
  A **re-capture** whose target or earlier directory has a symlinked component is refused
  before writing: KEEP with reason "symlinked folder" (RC14), adopting g56 P5's rule early,
  so W1 never writes beside into a directory the sweep would refuse to clean.
- **NG12 — The Graph incremental fast path is untouched** (R17). It never re-captures an
  archived message, so it never reaches this logic.

---

## 4. Mechanism

### 4.1 Vocabulary

- **Units** of a message's archived copy:
  - the **page**: the `.html`, made of the header, the body, and inline images embedded as
    data URIs;
  - the **attachments**: the `-attachments.zip`;
  - the **original**: the `.eml`.
- **Item:** one attachment, identified by **(label, occurrence)**, or by **content**
  (size and CRC-32) when its label changed (RC4). The label is `attachmentLabel(a, i)`
  (attach.go:121-129), the same vocabulary the gap lists use. The occurrence is the
  attachment's rank among same-label attachments in message order. The index `i` is
  advisory only: an equal fingerprint does **not** imply equal labels, order or count once
  the mime-fidelity design freezes the fingerprint on the v1 parser's terms
  (design-mime-fidelity.md P1), so nothing matches by index (RC4).
- **Held:** an item whose bytes the earlier files actually contain, either as a zip entry
  or as an inline data URI: non-empty, and read without a stream error at capture. **Body
  held** means `hasBody` was true at capture.
- **Earlier unit state** (RC9), from `archivefs.Inspect`:
  - **absent** (ENOENT or ENOTDIR on a component, parents readable): the unit holds
    nothing, so it cannot be degraded;
  - **refused** (a symlinked component or an irregular file) and **unreadable** (any other
    Lstat, open or read error, a truncated central directory, a read over the cap): the unit
    is **presumed held**. A regression against it gives KEEP or UNVERIFIED, never COMMIT,
    and nothing is merged from it;
  - **intact** or **damaged** (present, but its size or Full-mode digest differs from the
    recorded fixity): a damaged unit is a HEAL candidate (RC2).
- **Inventory:** body held or not (with the body digest), plus the held items with their
  size, CRC-32 and zip entry name (or "inline"), bound to the zip digest it was read from
  (RC11), with the content digest and render scheme of the capture (RC5).
- **Outcomes:**
  - **COMMIT**: the new capture replaces the earlier one;
  - **UNCHANGED**: unchanged content (RC5) and intact earlier units, nothing written;
  - **HEAL** (RC2): unchanged content, but an earlier unit is absent or damaged and its
    staged bytes equal the recorded fixity, so the staged unit is renamed into place;
  - **MERGE**: a new capture plus carried items;
  - **KEEP**: the earlier copy is kept and nothing is written;
  - **UNVERIFIED**: a KEEP of an `unknown` legacy record;
  - **DEMOTE**: a legacy-adopt COMMIT that regressed, with the earlier file left on disk
    and owned through the new record's AlsoFiles (RC17).

### 4.2 Data and schema — additive fields (version per OR2, RC29; [ruling Q2/OR2 = J2]: version 6)

*Terms:* in this document a **v7 record** is a record captured by this build, so it
carries `Held`, whatever manifest version OR2 settles on (**[ruling J2]**: the manifest
stays version 6, so "v7" names only the record's generation). A **pre-v7 record** has no
`Held`.

New `Record` fields, all additive and `omitempty`:

```go
Held       *Inventory `json:"held,omitempty"`       // what this record's files hold (P1/P3); nil on pre-v7 records
Kept       []string   `json:"kept,omitempty"`       // attachment labels the archive holds that the source no
                                                    // longer delivered at the last re-capture (P2); labels only (RC23)
KeptBody   bool       `json:"kept_body,omitempty"`  // the body is held but was not delivered (RC23)
KeptHow    string     `json:"kept_how,omitempty"`   // "lost" (empty/absent on T1/T2) | "unreadable" | "presumed"
                                                    // (T3, UNVERIFIED, refused/unreadable unit) — drives wording (RC26)
KeptAt     string     `json:"kept_at,omitempty"`    // RFC 3339 UTC of that re-capture (a string, like Record.Date: a
                                                    // zero time.Time is not omitted by encoding/json); informational
Unverified bool       `json:"unverified,omitempty"` // an `unknown` record kept as archived (P10; typed, RC23)

type Inventory struct {
    Body       bool       `json:"b,omitempty"`
    BodyDigest string     `json:"bd,omitempty"` // SHA-256 of the body variants at capture; a T1 MERGE needs equality (RC35)
    Content    string     `json:"c,omitempty"`  // m.ContentDigest() at capture; equals Record.ContentHash where the
                                               // live path records it (RC5)
    Render     int        `json:"r,omitempty"`  // RenderSchemeCurrent at capture; bumped by any release that changes
                                               // page output, pinned by a golden-render test (RC5)
    Zip        string     `json:"z,omitempty"`  // Fixity.Zip.SHA256 the items were read from; "" = no zip (RC11)
    Items      []HeldItem `json:"i,omitempty"`  // held items only, message order
}
type HeldItem struct {
    Ix     int    `json:"x"`            // index at capture; advisory only, never used to match (RC4)
    Label  string `json:"l"`            // attachmentLabel at capture
    Size   int64  `json:"n"`            // bytes held
    CRC    uint32 `json:"crc"`          // CRC-32 of the held bytes; the zip writer computes it, inline items when embedded (RC4)
    Entry  string `json:"e,omitempty"`  // zip entry name, stored only when it differs from Label (RC35)
    Inline bool   `json:"in,omitempty"` // embedded inline in the page, not in the zip
    Kept   bool   `json:"k,omitempty"`  // carried from an earlier capture (P3)
}
```

New top-level `Manifest` fields:

```go
Superseded []SupersededEntry `json:"superseded,omitempty"` // earlier copies awaiting deletion (P6); the user-facing
                                                           // vocabulary is ruled by OR3 (RC28): "pending removal",
                                                           // codes removal_pending_* [ruling Q10(3)]
PagesStale bool              `json:"pages_stale,omitempty"` // folder pages may link a deleted copy (§4.8)

type SupersededEntry struct {
    Path  string `json:"path"`            // the earlier .html (archive-relative); its -attachments.zip and .eml go with it
    Key   string `json:"key"`             // the record that superseded it (G3 resolves it through the current record, RC21)
    HTML  string `json:"h"`               // SHA-256 of the superseded page: prev.Fixity.HTML, or computed at Supersede
                                          // time for a pre-fixity record (G6, RC12)
    At    string `json:"at"`              // RFC 3339 UTC
    Cause string `json:"cause,omitempty"` // last blocking cause: index-stale | index-not-open | in-use | permission (RC14)
    Since string `json:"since,omitempty"` // first time a gate blocked it (RC14)
}
```

- **No new label sentinel** (RC23). UNVERIFIED is the typed `Record.Unverified`, which
  replaces `Missing:["unknown"]` on an UNVERIFIED outcome. It is never retried by
  incremental runs, and full runs re-check it. `UnknownSentinel` stays as is for version-1
  records. A gap list containing a label equal to a sentinel (`body`, `unknown`,
  `unverified`) is ambiguous evidence, so the outcome is KEEP (§4.3).
- `HasIssues()` also returns true when `Kept` is non-empty, `KeptBody` is set or the record
  is `Unverified`, so Subject and Date are carried for report rows. On KEEP and UNVERIFIED,
  Subject and Date are set from the new capture whenever HasIssues becomes true (RC26 (f)).
  Growth is only with such records, as in design-incremental-completeness B8.
- `Counts()` gains `unverified` from the typed field, so an UNVERIFIED record is never
  counted as source-empty. A new `RecaptureCounts()` returns kept records, healed records
  and pending superseded entries by cause.
- **`Held` is validated** at load or use (RC11): Ix in range, Size ≥ 0, unique Ix, bounded
  label and entry lengths. A malformed `Held` is treated as nil (T2/T3), with a warning.
- **Migrations** (RC21): every key remap (CollapseByIdentity + `idx.Rekey`, Load's re-scope,
  RepairKeys, MigrateKey) also maps `Superseded[].Key`, and every path canonicalization maps
  `Superseded[].Path`.
- **Version (RC29, OR2).** The default is **no bump**: the manifest stays version 6, and
  every field above is additive, so an older binary drops them in the safe direction (§6).
  If OR2 rules for a bump, it ships expand/contract: release N's `Load` accepts 7 while its
  `Save` still writes 6, and release N+1, bundling S4 and S5, writes 7 (FC1, MA-132).
  **[ruling Q2/OR2 = J2]** No bump, for every in-flight design: `Load`'s ceiling stays 6,
  and MA-201 and S39 are unchanged. A future change that cannot be additive needs a new
  owner ruling, and would ship with this expand/contract.

**Growth bounds (durability lens):**
- **Inventory:** re-derived from a real fixture manifest before S5 (RC35). The revision 1
  estimate (about 12 bytes per record plus about 45 bytes per held attachment) was about
  half the real size under compact `encoding/json` (manifest.go:1391): an item is about
  32 bytes plus twice the name length, so about 80 bytes for a 25-character name. Storing
  `Entry` only when it differs from the label removes the doubling. It is cleared never; it
  is the record's inventory. MA-364 asserts the size.
- **`Kept`:** bounded by the message's attachment count.
- **`Superseded`:** bounded by one checkpoint interval or the pending-bytes cap (RC8),
  whichever comes first, plus entries blocked by the gates in §4.8. A blocked message is
  bounded by W1's `-v99` cap (RC14). Blocked entries are counted by cause in `status`;
  `reindex` drains the `index-stale` and `index-not-open` causes, and the `in-use` and
  `permission` causes name their own remedy (RC15).
- **`PagesStale`:** one bool.

### 4.3 The never-degrade comparison

**Evidence tiers.** The earlier record's inventory comes from the best evidence available:

| Tier | Applies when | Earlier body held | Earlier items held | Precision |
|---|---|---|---|---|
| **T1 recorded** | `prev.Held != nil` (v7 capture). Per unit: the zip unit uses T1 only when its on-disk size equals `Fixity.Zip.Size` and `Held.Zip` equals the current `Fixity.Zip`; otherwise that unit falls to T3 (RC11) | `Held.Body` | `Held.Items` (label, occurrence, size, CRC-32, entry) | exact for the bytes it is bound to |
| **T2 gap-derived** | no `Held`; `prev.Fingerprint != ""` and `== fp`; `prev.ParseScheme` equals the current parse scheme (both 0 until the mime-fidelity design lands, RC4); record not `unknown` and not `Unverified`; no gap label equal to a sentinel (RC23) | `"body" ∉ Missing ∪ Terminal` | per label L: count(L in message) − count(L in Missing ∪ Terminal) | exact presence; no sizes; zip vs inline unknown |
| **T3 on-disk** | everything else: the `unknown` sentinel; an `Unverified` record (its gap lists were never computed); an empty fingerprint; a fingerprint that differs under immutable-id placement (§1.6); a different parse scheme; an unbound T1 zip unit | not `unknown`: from the gap lists. `unknown`: the earlier page's anchored `.mailarchive-body` div (the direct `<body>` child, read by the shared body reader, RC25) holds anything other than an empty `pre.plain`. An unreadable page or a missing div is **presumed held** | the earlier zip's entry names **and their CRC-32 and size**, read from the central directory through the gate, within a size cap and an entry-count cap of 65,535 (RC22). Inline images: any new-capture gap on an attachment with a Content-ID is **presumed held inline** unless the earlier record lists it as a gap | conservative |

**Earlier units that cannot be inspected** (RC9): a refused or unreadable unit is presumed
to hold every item its tier says it held, and every label the message names when the tier
cannot say. Nothing is merged from it. A refused or unreadable *target or earlier
directory* (a symlinked component) refuses the re-capture before writing: KEEP with reason
`symlinked folder` (RC14).

**New inventory**, computed from this capture:
- body: `hasBody(m)`, and its digest;
- item held: non-empty and read without error, whether it went into the zip or was
  consumed inline, with its size and CRC-32;
- empty: reported **now empty**;
- a client placeholder part (`X-Mozilla-Altered`, `X-Mozilla-External-Attachment-URL`, or
  Content-Type `text/x-moz-deleted`, flagged by the mbox/maildir parser): **now absent**,
  whatever its byte count (RC34);
- stream error: **now unreadable**;
- no counterpart in the new message: **now absent**.

**Comparison for T1 and T2** (RC4). Matching is per label, by occurrence, **never by
index**. In T1 an earlier item is also matched by content: it counts as present when any
new item has the same (Size, CRC-32), whatever its label.
- `regressed(L)`: held now is less than held before. The reason is empty, unreadable or
  absent.
- `improved(L)`: held now is more than held before.
- **Body regressed:** held before and absent now. Body improved is the converse.
- **Changed** (T1 only): held both times, both non-empty, sizes differ. Counted, not a
  regression (NG3). A client placeholder is never "changed" (RC34).
- **Page unit regressed:** the body regressed, or a regressed item was held inline (T1:
  `Inline`; T2: the item has a Content-ID, presumed inline, which is conservative).

**Comparison for T3.** The capture regressed if any of these holds:
- an earlier zip entry is absent from the new zip: neither its name nor its (CRC-32, size)
  appears among the new entries (RC4);
- the earlier body was held and is absent now;
- the inline presumption fires.

**Verdict:**

| Condition (first match wins) | Outcome |
|---|---|
| The target or earlier directory has a symlinked component (RC14), or the free-space precheck fails after a commit and sweep (RC8), or W1 finds no free name up to `-v99` (RC14) | **KEEP** with that reason (R-J, R-I, R-K); nothing written |
| Nothing regressed or improved, content unchanged (`Held.Content` and `Held.Render` equal, the zip digest equals `Fixity.Zip` or neither has a zip; a pre-v7 record without `Held`: every unit's staged bytes equal the recorded fixity), and every earlier unit is intact (RC5, RC2) | **UNCHANGED** (also for a legacy-scheme record, which is then adopted in place and re-stamped, RC17) |
| As above, but an earlier unit is absent or damaged, its staged digest equals the recorded fixity, and the on-disk comparison finds nothing the staged unit lacks (Full mode only) | **HEAL** (RC2) |
| Live path, legacy-scheme adopt (`adoptingLegacy`, exporter.go:484) | exempt from KEEP: COMMIT under a fresh name, the earlier files kept and owned through AlsoFiles, never superseded (DEMOTE when something regressed), §4.6, RC17 |
| Nothing regressed | **COMMIT** |
| At least one item or the body improved, AND only zip-held attachments regressed, every regressed item is still listed by the new message, the page did not regress, and the merge is possible (§4.4) | **MERGE** (RC3) |
| Anything else (a pure regression; an absent item; ambiguous evidence) | **KEEP** (`unknown` record → **UNVERIFIED**) |

"Improved" does not gate a COMMIT in Full mode, but it does gate a MERGE (RC3): a pure
regression is a stable KEEP, so the next full run over the same source writes nothing. In a
fill retry, the probe still requires an improvement before any capture is attempted (R2
unchanged). **A held item never becomes a gap.** Tiers of the named rows (RC35 (d)):
MA-356's main case is T1 (a v7 record); its pre-v7 twin, whose fingerprint differs under
immutable-id placement, is T3; an `Unverified` record re-checked by a full run is T3.

### 4.4 Merge (P3)

The verdict reaches MERGE only when at least one item or the body improved (§4.3, RC3).

**All six conditions must hold:**
1. every regressed item is zip-held;
2. the earlier entry name is known. In T1 that is `HeldItem.Entry` (or the label, when no
   separate entry is stored). In T2 it is known only when the item has no Content-ID and
   `SanitizeFilename(name, i)` (util/paths.go:91) is unique among the message's
   attachments, so `uniqueName` added no ` (n)` suffix, and the earlier zip holds exactly
   that name;
3. the earlier zip is reliable and inspectable (RC9: not refused, not unreadable). It is
   read through the gate (§4.8 G4) from **one open handle**: the whole-file digest is
   checked and the raw entries are read from that same handle (RC22). If `Fixity.Zip` is
   recorded, the digest must match. Otherwise each carried entry must decompress with a
   matching CRC-32 within min(`Size + 1`, `rebuildMaxFileBytes`) in T1, and within the
   512 MiB `rebuildMaxFileBytes` cap in T2 (RC22). The central directory holds at most
   65,535 entries (one helper, shared with the mime-fidelity design's bound);
4. the page did not regress;
5. **every regressed item is still listed by the new message** (RC3). An item with no
   (label, occurrence) or content counterpart in the new message has no slot in the new
   zip, so it is never carryable, and the outcome is KEEP;
6. **in T1, the new body digest equals `Held.BodyDigest`** (RC35 (a)). A same-envelope
   message with a different body is therefore never merged with genuine carried entries; it
   is KEPT. T2 records have no body digest, and the forgery residual is stated in §8.1.

**Construction:**
- Stage a new zip temp, iterating attachments in message order and skipping
  inline-consumed ones.
- An item with non-empty new bytes is written as a new entry.
- A regressed, carryable item has the earlier entry's **raw compressed bytes** copied
  (`zip.File.OpenRaw` → `zip.Writer.CreateRaw`, Go ≥ 1.17; go.mod is 1.26). Nothing is
  recompressed and the CRC is preserved.
- **`CreateRaw` gets a fresh FileHeader** (RC22): the recomputed Name; Method limited to
  Store or Deflate; CRC32, CompressedSize64 and UncompressedSize64 copied; Extra, Comment,
  ExternalAttrs, Flags and Modified reset to what `zw.Create` writes. An encrypted entry is
  refused, and the outcome is KEEP. No header field of the untrusted earlier zip (a
  Unicode-path extra field, symlink mode bits) reaches the new zip.
- The output name is computed by the normal pipeline,
  `uniqueName(SanitizeFilename(Filename, i))`, **never** taken from the stored `Entry`
  string.
- Anything else becomes a gap, as today.

**The new record:**
- carried items have `Kept=true` in `Held.Items`;
- `Kept` lists the carried labels and `KeptAt` is set;
- gaps are the new gaps minus the carried items, classed per `SourceComplete`.

**Failure while merging** (a read error, CRC mismatch or fixity mismatch): the outcome
becomes **KEEP**, with a warning whose wording depends on the cause (§4.11 R-G, RC26 (c)).
It is not an `Export` error. A digest or CRC mismatch is also recorded in the last-run
record, so `status` shows it (RC26 (d)).

### 4.5 The preserved original (P4)

| Outcome | New raw available (`-raw` and the source has bytes) | No new raw |
|---|---|---|
| COMMIT | write the new `.eml` | carry the earlier `.eml` with its `Fixity.EML` (copied to the new name when the message moves, digest re-checked on the copy). If the earlier eml fails its recorded fixity, or is refused or unreadable (RC9), the outcome becomes **KEEP** (never launder a modified original). A pre-fixity earlier `.eml` has no digest to re-check: it is digested from the bytes kept or copied and recorded as a baseline, labelled like `verify -record` (RC35 (f)) |
| UNCHANGED | nothing written: the earlier `.eml` stays even when the new raw differs only in mutable client headers such as `X-Mozilla-Status` (RC5) | the earlier `.eml` and `Fixity.EML` stay |
| HEAL (RC2) | an absent or damaged earlier `.eml` whose staged digest equals `Fixity.EML` is renamed into place | nothing to heal from; a missing earlier `.eml` stays missing and `verify` keeps reporting it |
| MERGE | **do not** write the new raw (it is degraded). Carry the earlier `.eml` if there is one; if there is none, write none | same |
| KEEP / UNVERIFIED | nothing written | nothing written |

**Rationale for the MERGE row (RC7).** An original is the faithful bytes of one capture,
never one known to be degraded relative to the copy it replaces. The earlier `.eml` is the
original of the capture whose items were kept; the new raw lacks them. On a merged record,
therefore, no original holds every item: the filled item lives only in the zip. That is the
stated residual of P4, and MA-357(c) asserts it.

**The page links the post-verdict original (RC6).** `RenderContext.RawName` names the
`.eml` that will exist after the verdict: the new raw, or the carried earlier `.eml`. The
page is rendered, or re-rendered, after W2 step 4. A non-raw full run over a `-raw`
archive therefore renders the eml link, can be UNCHANGED, and the page keeps linking the
preserved original (§7.5).

### 4.6 Where the decision runs

- **Fill retry** (Incremental, fillable record, exporter.go:235-276): the probe runs in
  **two phases** (RC24).
  1. Today's improvement check, unchanged: `probeImproves` streams only the attachments
     whose label is in `oldMissing` and returns at the first improvement (exporter.go:571-590).
     Today it does **not** stream every attachment. A non-improving probe streams no held
     attachment, so the routine incremental run costs what it costs today.
  2. Only when an improvement is found are the held items streamed, to build the new
     presence inventory and tell a stream error apart from zero bytes.

  With T1 or T2 evidence, the verdict is then reached **before rendering**: a KEEP decided
  at the probe writes no temp file (`Retried++`, `Kept++`). MERGE and COMMIT proceed to
  capture.
- **Full mode and `unknown` records:** capture, then decide. The zip build yields the
  inventory, so attachments are not read twice. A KEEP discards the temps. In Full mode each
  earlier unit is also inspected, and hashed once, for HEAL (RC2).
- **The mime-fidelity repair** (design-mime-fidelity.md P11, an incremental re-capture when
  the parse scheme improved) is a re-capture path too. Under §4.13's rule it calls this
  comparison, with its own count precondition kept as an extra gate, and writes through
  W1–W5, unless OR3 declares it an exception (RC28). **[ruling Q10/OR3 = J6]** No exception:
  it writes through W1–W5, takes only UNCHANGED, COMMIT or KEEP (never MERGE), and hands
  this comparator V1C's per-entry v1↔fidelity correspondence as its T1-equivalent
  inventory.
- **First capture:** unchanged (NG6).
- **Stamping of every non-writing outcome (P9, RC1)** on the live path:
  - **UNCHANGED** runs W5's record switch at `T = prev.Path` without file writes (§4.7 W4),
    then `OnExported`, exactly as today's Full mode does through Export's rebuild
    (exporter.go:415-466) and `OnExported` (graph.go:224-235).
  - **KEEP and UNVERIFIED** call `Manifest.MergeFields(key, folder, RunAt, true)` and
    `OnManifestSkip(key, folderPath, moved || !prev.Present)`, exactly as the incremental
    skip does (exporter.go:248-266). There is no `idx.Add`, which would index the degraded
    body.
  - Without this, Full mode stamps the message by nothing (the fast path is
    Incremental-only, graph.go:504), and `SweepGone` (manifest.go:956-971, called at
    graph.go:660) would write it to history as gone.
  - For KEEP and UNVERIFIED the capture-time fields are untouched: Path, Fixity,
    Fingerprint, ContentHash, Held, the gap lists. Only `Kept`, `KeptBody`, `KeptHow`,
    `KeptAt`, `Unverified`, and Subject/Date when HasIssues becomes true (RC26 (f)),
    change.
- **Legacy-adopt exemption (NG4, RC17):** the identity is not content-verified (MA-238). So
  the outcome is never KEEP, which could drop a distinct reuse.
  - **Identical bytes first.** UNCHANGED is evaluated before the legacy row (§4.3). A
    legacy record whose staged bytes equal its recorded fixity, with intact earlier files,
    is adopted with no write, and `FpScheme` is re-stamped to current. Identical bytes cannot
    be a distinct reuse.
  - **Optional envelope verification.** Where the shared body reader (RC25) parses the
    earlier page's header rows, an envelope match (subject, sender, recipients, date to the
    second, attachment names) counts as verified identity, and the normal supersede path is
    taken.
  - Otherwise the capture commits under a fresh name (W1), never in place (W3 never applies
    to a legacy adopt).
  - The earlier files are **never superseded**. They are recorded in the new record's
    `AlsoFiles`, so they are owned: G2 never sweeps them, `verify` counts them as owned, and
    redaction's prune reaches them (reindex.go:150). Because W1 never writes over an
    existing file, this protection also covers the unchanged-path case. Today that case
    overwrites in place and destroys the possibly-distinct original.
  - **Every** run that leaves such an earlier file prints R-F, not only when the comparison
    found a regression (DEMOTE), and `status` counts them.
- **Checkpoint cadence:** KEEP counts as processed work in both gates (run.go:209-210 adds
  `Kept` to `Exported`; graph.go:266 adds `Kept` to `Exported + SkippedManifest`).
  UNCHANGED is counted in `Exported` and in a new `Unchanged` subset, so MA-22's counts and
  `pagesNeeded` are unaffected. The summary reports the subset (RC26 (e)). HEAL counts in
  `Exported` and in `Healed`.

### 4.7 The write path (P5, P8)

- **W1 — Target selection for a re-capture that changes bytes.** This applies to re-captures
  through the shared write path; §4.13 governs the sibling writers (RC28). Let B be
  `stemFor`'s stem under the current naming rule, with the path budget reserving four runes
  for a suffix. Before staging, the **space precheck** runs (RC8): free bytes on the
  archive volume must cover prev's Fixity sizes plus the current manifest size plus a
  margin, measured with statfs or GetDiskFreeSpaceEx (golang.org/x/sys, already in go.mod).
  If short, the run commits and runs `finishSuperseded` first. If still short, the outcome
  is KEEP with reason `low-disk` and nothing is written (R-I). The target T is the first of
  `B`, `B-v2`, … `B-v99` (RC14) for which all of these hold:
  - `T.html` is not `prev.Path`;
  - `PathTaken(T)` is false. That means no record's Path or AlsoFiles names it, and it is
    not pending in `Superseded`;
  - **none** of `T.html`, `T-attachments.zip`, `T.eml` exists on disk. Any stat error other
    than ENOENT counts as "exists" (RC9).

  **Adoption (RC8).** A candidate whose three files exist, are owned by no record, and whose
  digests equal the staged temps is adopted as T: the temps are discarded and the existing
  files are recorded. An orphan left by a kill or a failed save is therefore re-owned on the
  next re-capture instead of pushing the message to `-v3`, `-v4`.

  When no candidate up to `-v99` is free, the outcome is KEEP with reason "earlier copies
  await removal" (R-K), never a longer suffix. So **a re-capture never writes over an
  existing file** (except HEAL, W4, and the pre-fixity exemption, W3). A message alternates
  between B and `B-v2` across changed re-captures. `stemFor`'s collision check (exporter.go,
  `KeyForPath` loop) also consults `PathTaken`, so another key never claims a pending
  superseded path (the 32-bit collision case of MA-87).
- **W2 — Stage, then rename.**
  1. Render the page in memory (this decides which attachments are consumed inline).
  2. Stage the zip temp; this yields the inventory and the digest.
  3. Decide (§4.3, §4.4).
  4. Decide the `.eml` (§4.5) and stage the eml temp (new raw, or a copy of the earlier
     eml).
  5. Re-render the page for T if its stem, or the post-verdict `RawName`, differs from the
     one rendered in step 1. `RawName` names the `.eml` that will exist after the verdict:
     the new raw, or the carried earlier `.eml` (RC6).
  6. Stage the html temp. Each temp gets `commitTemp`'s fsync discipline.
  7. Rename the eml, then the zip, then **the html last**, with a directory fsync after
     the last rename.

  The page is the siblings' commit marker: an orphan zip with no page is swept by
  `SweepOrphans` (atomic.go:106-136), whose zip rule now skips zips that a record, an
  AlsoFiles entry or a pending Superseded entry owns (RC20). On **any** error before the
  record switch, every **fresh T-named** file this attempt created is removed (RC16). These
  are fresh names that nothing references. The earlier files are untouched, and `Export`
  returns the error (NG9).
- **W3 — Pre-fixity in-place exemption.** This applies when the earlier record carries
  **no** `Fixity` at all (a pre-fixity archive), the outcome is COMMIT, `B == prev.Path`,
  and the record is **not** a legacy-scheme adopt (RC17). The units are then staged and
  renamed over the earlier files in place, so names stay stable for the README's "full
  re-export to gain fixity" path. A kill leaves a record with no fixity, so `verify`
  reports `unrecorded` exactly as before the run, never `modified`. P1 already established
  the bytes are no worse. MERGE never uses W3.
  - **Failure path (RC16).** Once the first rename-over has happened, an error removes only
    the remaining **temps**. The renamed units stay, because P1 judged them no worse. The
    record stays unchanged (it has no fixity, so verify reports `unrecorded` as before), and
    the error names the path. W2's cleanup never applies to the record's own paths.
- **W4 — UNCHANGED and HEAL** (RC1, RC2, RC5).
  - **The test** (RC5). Nothing regressed or improved: the inventory is equal, including
    body and eml presence. The staged zip's digest equals `Fixity.Zip`, or neither capture
    has a zip. `Held.Content` and `Held.Render` equal the new capture's. For a pre-v7 record
    without `Held`, the revision 1 test applies instead: the staged zip and eml, and the page
    rendered for `prev.Path`'s own stem, all equal the recorded fixity. When the zip digest
    already differs, the extra render is skipped. The page keeps its capture-time state:
    Unread, Importance, Sensitivity, Categories and the transport headers are not refreshed
    (S35/S36; design-categories §5), pending OR1 (escalated as ESC-3 [ruling ESC-3]; this
    is its option (a)).
  - **The earlier units are inspected** through the gate before the temps are discarded
    (RC2). Absent units, size mismatches against `Fixity` and `Held` binding mismatches are
    always checked; in Full mode each earlier unit is also hashed once, like `verify`.
  - **UNCHANGED** when every earlier unit is intact. The temps are discarded.
  - **HEAL** (Full mode only) when a unit is absent or damaged, its staged digest equals the
    recorded fixity, and the on-disk comparison (§4.3, RC11) finds nothing the staged unit
    lacks. The staged unit is renamed into place, then the directory is fsynced. Because the
    staged digest IS the persisted fixity, a kill leaves the unit "still damaged" or
    "healed", never a false alarm. This is the named exception to W1 and P5. A damaged unit
    whose staged digest differs from the recorded one takes the normal W1 COMMIT path
    instead; a damaged unit that holds something the staged unit lacks gives KEEP. A missing
    page is healed only in Full mode, which keeps today's parity with a redaction in progress
    (delete the html, then reindex).
  - **The record switch (RC1).** Both outcomes then run W5's record switch at
    `T = prev.Path` without moving anything. `Manifest.Add` receives the record rebuilt
    exactly as COMMIT rebuilds it: `Present`, `LastSeen = RunAt`, `Folder`; the PhysID +
    AltPhysIDs union, ContentHash, FpScheme; FirstFolder, FirstSeen and AlsoFiles carried;
    prev's Path, Fixity and ExportedAt kept; gaps recomputed from this capture (identical
    content gives identical gaps, which also retires a stale `unknown`); `Held` backfilled;
    `Kept` cleared; no Supersede. Then `OnExported` replaces the index row and, on the live
    path, writes the history folder-assertion.
- **W5 — Record switch.**
  - `Manifest.Add(K, rec{Path: T, Fixity, Held, gaps, …})`;
  - then, if `prev.Path ≠ T` and the record is not a legacy adopt,
    `Manifest.Supersede(prev.Path, K)`, which also sets `PagesStale`;
  - then `OnExported`, which replaces the index row by key and, on the live path, writes
    the history folder-assertion.

  The kept earlier path of a legacy adopt is appended to the new record's `AlsoFiles`
  (RC17). UNCHANGED and HEAL use this same switch at `T = prev.Path` (W4, RC1). The
  immediate deletions at exporter.go:382, :485-490 and attach.go:101 are **removed**.
  `WriteZip` with `Written == 0` deletes nothing.

### 4.8 Deferred deletion (P6) and the sweep that finishes an interrupted deferral

`app.finishSuperseded(out, manifest, idx, idxOnDisk, lock, logger) (removed, pending int)`
runs:
1. after each `commit` whose manifest Save returned nil: the checkpoints (run.go:208-224,
   graph.go:259-276), the file and mailbox boundaries (run.go:243, graph.go:293), and the
   end of the run (graph.go:323). This needs `commit`/`commitInOrder` (run.go:337-373) to
   **return** the save error; today they log it and continue. g7's F53 touches the same
   function (§4.13). Every boundary and final commit also re-checks `lock.StillHeld()`
   before its Save, not only the cadence-gated checkpoint (RC13);
2. at the **start** of `Run`/`RunGraph`, once the manifest is loaded and the index open,
   before the walk. This finishes a deferral a kill interrupted. `RunGraph` also runs the
   age-gated `SweepOrphans` here, which today only `Run` and `reindex` call (RC20);
3. in `reindex`, after the stale-row repair and the index flush, before the pages (§4.9);
4. in `app.Rebuild`, the dashboard's index action, which then regenerates the pages and
   so clears `PagesStale` under the rule below (RC20). The desktop's remedy for R-E is
   therefore its existing Rebuild.

It never runs in `verify`, `extract`, `serve` or `status`.

**When the sweep runs at all.**
- **The lock (RC13).** `finishSuperseded` calls `lock.StillHeld()` before its first
  deletion and every 100 deletions. On failure it stops deleting, leaves the entries
  pending and sets `lockLost`.
- **Pages (RC14).** Deletions are skipped, and the entries left pending, on a run that will
  not regenerate pages: `-pages=false`, no index open, or an all-failed Graph run
  (graph.go:324). Folder pages therefore never keep linking a copy this run deleted.
- **Pending bytes (RC8).** A checkpoint (commit + sweep) is also forced when the bytes
  written beside since the last durable save exceed max(1 GiB, 2 × manifest size), so the
  transient overhead is bounded whatever the archive's size.

**Gates per entry (all must pass):**
- **G1:** `validRelPath(entry.Path)` holds (verify.go:509), and the path has the
  message-stem shape of a shipped naming rule (`<token>/…/<date>_<time>_<slug>_<hex>[-vN].html`,
  the pre-68a4096 rule included). `index.html` and folder pages never match (RC12).
  Otherwise the entry is **dropped** (tampered) and logged, and nothing is deleted.
- **G2:** no record's Path or AlsoFiles equals the path or one of its siblings. Otherwise
  the entry is dropped without deleting (now owned) and logged.
- **G3 (index):**
  - if a `search.db` exists on disk but is **not open** in this process (an `-index=false`
    run), every entry stays pending (cause `index-not-open`). This mirrors the collapse
    deferral at graph.go:176-185;
  - if it is open, the entry is resolved **through the current record** (RC21): the row of
    the record that now owns `entry.Key`'s identity, via `idx.PathForKey` (new; `docs.key`
    is `UNIQUE`, index.go:40), must not equal `entry.Path`. A stale row, from an F49
    rollback or a power-loss revert, keeps the entry pending (cause `index-stale`) until a
    later run or `reindex` repairs it;
  - a `PathForKey` **error** keeps the entry pending (fail closed, RC21).
- **G4:** each of the three siblings is resolved component-wise without following any
  symlink and removed only if it is a regular file. The removal goes through `os.Root`
  (`Root.Lstat`, `Root.Remove`), so a component swapped for a symlink between the check and
  the removal is refused too (RC22). This is verify's `inspect` (verify.go:390-414) moved to
  `archivefs`, which every gate copy then uses (RC25). An **absent** file counts as done. A
  symlinked component, an irregular file or an unreadable component keeps the entry pending
  and is logged; it is never followed.
- **G5:** a removal error (a Windows sharing violation, EACCES) keeps the entry pending
  (cause `in-use` or `permission`).
- **G6 (content, RC12):** `entry.Path`'s `.html`, if present, must have the entry's
  recorded page digest. A different page means the name was reused by a newer capture (W1's
  B ↔ B-v2 alternation, seen through a restored or conflicted manifest). The entry is then
  dropped without deleting, and logged.
- **G7 (the superseding copy, RC12):** if `entry.Key` still has a record, that record's own
  current `.html` must be present with its recorded size. Otherwise the entry stays pending.

Each blocked entry records its `Cause` and, the first time, `Since` (RC14). When every
passing entry's siblings are removed, each distinct directory is fsynced **once**, and then
the entries are dropped in memory (RC20); the drop becomes durable at the next save.

**The `PagesStale` rule** (RC14). It is set with every `Superseded` entry and persisted by
the same save, so it is durable before any deletion. A deletion requires the flag to be
persisted true: an entry that becomes deletable after a regeneration cleared the flag sets
it again and waits for the next save. A page regeneration clears the flag whenever no
deletion has happened since the flag was last set. `pagesNeeded` (run.go:295) gains
`|| manifest.PagesStale`. `reindex` and `Rebuild` always regenerate the pages.

Result: a run killed after a deletion still regenerates the folder pages next time, even
with `exported == 0`. Blocked entries no longer force a regeneration on every run.
Redaction removes a pending copy only once its block clears (RC14), and R-E names the
block's own remedy (RC15).

### 4.9 `reindex` (P7, F48)

`Reindex` collects the rows as today (reindex.go:108-116) and then decides per row, with
`rec, ok := manifest.Get(key)`. "Missing" means **absent** (ENOENT) as `archivefs.Inspect`
reports it. A path that is refused (symlinked component, irregular file) or unreadable
(any other error) is **never** treated as missing (RC10): the row and the record are kept,
nothing is deleted, and the path is logged, or the command aborts as today's EachRow does
on a non-ENOENT stat error (reindex.go:111-118).

| Row's path on disk | Record | `rec.Path` on disk | Action |
|---|---|---|---|
| present | absent, or `rec.Path == row path` | — | keep (today) |
| present | `rec.Path ≠ row path` | present | **repair**: re-point the row to `rec.Path`; print both paths and the redaction remedy (case A, RC18) |
| present | `rec.Path ≠ row path` | missing | **adopt** (RC36) if an unowned regular file in `rec.Path`'s directory carries the key's stem hash, or the row names it, and matches the record (Message-ID and date from its page header, or its eml); otherwise keep, printing "the current copy `<rec.Path>` is missing; an earlier copy `<row path>` is still served — delete it and run reindex to remove the message, or re-export to restore it" (case B, RC18) |
| missing | absent | — | prune the row (today) |
| missing | `rec.Path == row path` | — | prune: delete `rec.Path`'s siblings and AlsoFiles, the row and the record (today's redaction), plus the unowned `-vN` family of the redacted stem (RC18) |
| missing | `rec.Path ≠ row path` | **present** | **repair**: re-point the row to `rec.Path`; **delete nothing**; keep the record (the F48 fix). Print both paths and the redaction remedy (cases A/C, RC18) |
| missing | `rec.Path ≠ row path` | missing | **adopt** as in row 3 if a matching candidate exists (RC36); otherwise prune: delete the siblings of `rec.Path` plus AlsoFiles and the unowned `-vN` family; **enqueue** the row path as `SupersededEntry{Path: row path, Key}` instead of deleting it directly (RC10); remove the row and the record (redaction of a moved message) |

**[ruling ESC-2] What S2 builds before the owner answers.** Three parts of this table add to
or except from R13's existing text, so they wait for ESC-2: the **adoption** cells (RC36,
which keep a record whose recorded file is missing, where R13 prunes it), the **unowned
`-vN` family** on redaction (deletion case (c)), and the **enqueue** of a row path for the
sweep (which feeds deletion case (b)). Until then S2 prunes such a record as today (deleting
`rec.Path`'s siblings and AlsoFiles, which today's redaction already does, MA-227), leaves
the row path alone, and never deletes it. The repair rows (the F48 fix) and the absent-only,
archivefs-gated deletion land in S2: they restore R13 as written. MA-361's `-vN` and
N2-victim twins are skip encoders citing MA-361 and R13 until the slice after ESC-2.

- **Deletion rules (RC10).** `reindex` deletes directly only manifest-named paths:
  `rec.Path`'s siblings and AlsoFiles, each through archivefs (no symlinked component,
  regular file only, the message-stem shape of G1). A row path, which comes from the
  index, is never deleted directly; it is enqueued, so its siblings must pass G1, G2, G4, G6
  and G7 before the sweep removes them. §8's "the index is never acted on" therefore holds.
- **The `-vN` family on redaction (RC18).** Files in the same directory whose stem is the
  redacted base stem with or without `-v2` … `-v99`, owned by no record, are deleted through
  archivefs. The stem embeds the key digest, so such a file is by construction a copy of
  the same key, and the user asked for that message to be removed.
- **Repair lines (RC18).** Every repair or keep where the row path differs from `rec.Path`
  prints both paths and a redaction remedy, and deletes nothing. Cases A and C: "the
  archived message is intact at `<rec.Path>`; if you deleted `<row path>` to remove this
  message, also delete `<rec.Path>` and run reindex again". A silent and complete redaction
  during a stale-row window is impossible without bringing F48 back, so it is loud and one
  step away instead.
- **Adoption (RC36)** heals archives already damaged by N2 under today's binary: the record
  named a deleted file while the newer copy sat unowned beside it. Without a matching
  candidate, the record is pruned as today and the log names the candidate it rejected.
- **Re-point:** `deriveMessage(out, rec.Path, …)` (reindex.go, the rebuild reader: the
  `.eml` when present, else the page), then `idx.Add(store, strings.Split(rec.Folder, "/"),
  m, rec.Path, key)`. `rec.Folder` is the current folder and is authoritative on the live
  path.
- **Then, in today's order** (reindex.go:160-198): `finishSuperseded` (its gate now sees
  the repaired rows), `pages.Generate`, the report and README, clearing `PagesStale` under §4.8's rule
  (no deletion since the regeneration, RC14), `Save`, `CompactHistory`.
- `Reindex` returns `(kept, pruned, repaired, adopted int, err)`, and the CLI prints the
  repaired and adopted counts (§4.11).
- S2 rewrites `deleteMessageFiles`, as g56's C2-2/P5 does; the archivefs gate lands once
  (§4.13, RC10).

### 4.10 Classification, stats and exit codes

**Gap classes:**
- **KEEP:** the gap lists are left as they were. They still describe the kept files
  truthfully; an improved item that was not captured stays in `Missing` (fillable).
- **MERGE:** gaps are the new gaps minus the carried items, classed per `SourceComplete`.
- **UNVERIFIED:** `Unverified = true` (a typed field, RC23); `Missing:["unknown"]` is
  cleared. No label goes into `Terminal`, so the report and `Counts()` never read it as an
  empty attachment or a source-empty record.
- **UNCHANGED and HEAL:** gaps recomputed from this capture (identical content gives
  identical gaps).
- Kept items are neither fillable nor terminal.

**Error classification:**
- A failed **staging write or rename** (ENOSPC, a rename refusal) is an `Export` error, as
  today. The earlier files and record are intact.
- A **merge-source failure** is a KEEP plus a warning, not an error.
- A **deletion failure** leaves the entry pending, with a warning, not an error.

**Stats** (export.Stats): `Kept` (with a per-reason breakdown: `lost`, `unreadable`,
`presumed`, `low-disk`, `symlinked`, `no-free-name`), `Merged`, `KeptItems`, `Unchanged`
and `Healed` (subsets of `Exported`), `Changed`, `Unverified`, `Demoted`, `LegacyKept`
(earlier files kept beside legacy adopts, RC17), `MergeSourceMismatch` (RC26 (d)).

**Result and the last-run record** (state/lastrun.go): add `Kept`, `Merged`, `Healed`,
`Unverified`, `SupersedeRemoved`, `SupersedePending`, `MergeSourceMismatch` as additive
JSON fields. A non-zero `MergeSourceMismatch` raises a `status` WARN (RC26 (d)).

**Exit codes: no new codes** (X1 unchanged).
- The export exits 0 when re-captures were kept, merged, or left deletions pending. A
  refused downgrade is a correct outcome, not a failure. It exits 1 as today on write or
  read failures.
- `verify` keeps 0, 2 and 1. `superseded`, `earlier-copy` and `kept` never affect
  attestation (like `unexpected`). HEAL changes no exit code; a healed unit simply attests
  at the next verify.
- `reindex` keeps 0 and 1. `status` exits 0 whenever it reports.

### 4.11 Surfaces and wording (UX contract X2: what, why, remedy)

**Run summary.** One app-level `RecaptureSummary(Result) []string` produces the text
(RC25), and the CLI (stderr, via `printSummary`, cmd/mailarchive/main.go:1359-1419), the
GUI summary (cmd/mailarchive-gui/main.go:254, :1154) and the desktop Activity log
(internal/desktop/capture.go) all print it, so the wording cannot drift (X10). The desktop
runs only Graph incremental, so in practice it shows R-E, R-L and little else (RC27).
Counts are filled in. Wording depends on the reason and the evidence (RC26 (a)):

- **R-A (KEEP).** By `KeptHow`:
  - `lost` (an item empty or absent on T1/T2 evidence): `Kept 3 earlier archived copies:
    the mail source now holds less for these messages (2 attachments empty, 1 message body
    missing), so they were not overwritten. No action is needed — the archive now holds the
    only complete copy of these items; keep backing it up. Details:
    <out>/attachments-report.tsv (rows "source-lost").`
  - `unreadable`: `Kept the archived copy of 1 message: 1 attachment could not be read from
    the mail source this run. No action is needed; the next run tries again.`
  - `presumed` (T3, a refused or unreadable earlier unit): `Kept the archived copy of
    2 messages: the run could not confirm the new capture is no worse than the archived
    one, so the earlier copy was kept. Details: <report>.`
- **R-B (KEEP with an improvement not captured), source-aware (RC26 (b)):** `1 message has
  newly available content that was not captured, because capturing it now would discard
  content the archive already holds (its message body).` Then, for an on-demand local
  source: `It is captured automatically once the source delivers the whole message again —
  in your mail app, download the message for offline use, then re-run.` For Graph and
  deterministic sources: `No action can recover the rest from the source; the archive keeps
  the earlier copy.` Both end with `Details: <report>.`
- **R-C (MERGE):** `Filled 4 messages; 1 of them keeps 1 attachment from its earlier copy
  because the source no longer delivers it (see "source-lost" in <report>).` It is printed
  only when something was in fact filled (RC3).
- **R-D (UNVERIFIED):** `2 records archived before completeness tracking were left as
  archived: the run could not confirm the new capture is no worse than the archived copy.
  They are listed as "unverified" in <report>; a later full run re-checks them.`
- **R-E (pending deletion), one line per cause (RC15), naming the first path and the
  count:**
  - `index-stale`: `2 earlier copies replaced this run are kept until the search index
    stops pointing at them (first: <path>). The next run removes them; to finish now, run:
    mailarchive reindex -out "<out>".` On the desktop: `… to finish now, use Rebuild index.`
  - `index-not-open`: `… kept because this run did not open the search index — run with
    -index (the default), or mailarchive reindex -out "<out>".`
  - `in-use`: `… <path> is in use by another program — close it; the next run removes it.`
  - `permission`: `… <path> cannot be removed (<error>) — check its permissions.`
- **R-F (legacy adopt, every run that leaves one, RC17):** `1 message archived by an older
  version was re-captured under a new name; its earlier file was kept at <path> and is
  recorded with the message.` When the comparison found a regression (DEMOTE), it adds:
  `The earlier file holds content the new capture lacks — compare the two before removing
  either.`
- **R-G (merge source unreliable), by cause (RC26 (c)):**
  - digest or CRC mismatch: `Kept the earlier copy of 1 message although the source now
    holds less, and could not carry its attachments forward: its archived zip <path> fails
    its recorded checksum (modified). Restore that file from a backup, then run mailarchive
    verify -out "<out>".`
  - open or read error: `Kept the earlier copy of 1 message: its archived zip <path> could
    not be read now (in use or unreadable). It was left unchanged and is retried next run —
    no action is needed unless this repeats; then run mailarchive verify -out "<out>".`
    A backup restore is never advised without a mismatch.
- **R-H (HEAL, RC2):** `Restored 2 archived files that were missing or damaged, from a mail
  source that still holds them unchanged (first: <path>). Run mailarchive verify -out
  "<out>" to confirm.`
- **R-I (not re-captured: low disk, RC8):** `3 messages were not re-captured: <volume> has
  too little free space to write them beside their archived copies (about <N> MB more is
  needed). Their archived copies are intact. Free space, then re-run.`
- **R-J (not re-captured: symlinked folder, RC14):** `1 message was not re-captured: its
  folder <path> is a symbolic link inside the archive. Its archived copy is intact. Replace
  the link with a real folder (C2-2), then re-run.`
- **R-K (not re-captured: no free name, RC14):** `1 message was not re-captured: 99 earlier
  copies of it still await removal (first: <path>). Its archived copy is intact.` followed
  by that entry's R-E remedy.
- **R-L (unchanged, RC26 (e)):** `9,950 of 10,000 messages were unchanged (nothing
  rewritten).` It also appears in the GUI's Done line.

**Report** (`attachments-report.tsv`, internal/app/report.go): new rows of kind
`source-lost` with class `kept` (the detail is the label or `(message body)`, plus
`presumed` or `unreadable` when `KeptHow` says so), and kind `unverified` with class
`info`, read from the typed fields (RC23). Subject and date are carried (RC26 (f)), and
`tsv()` scrubs the text as today.

**`status`** (internal/health):
- an info line that leaves the posture unchanged: `Kept copies: 3 records hold content
  their mail source no longer delivered at the last re-capture — see
  attachments-report.tsv.` "The archive's copy is the only one" is added only when every
  kept record is `lost` on T1/T2 evidence (RC26 (a));
- `Unverified (legacy) records: 2 — a full run re-checks them.` (info);
- `Earlier files kept beside re-captured records archived before v5: 120 — see verify.`
  (info, RC17);
- a **WARN** per pending-removal cause when entries are still pending after a *completed*
  run (Q4; **[ruling Q4]**: only after a completed run, with kept content GREEN plus an info
  line), each with its own reason code and the R-E remedy, naming the first path: codes
  `removal_pending_index`, `removal_pending_noindex`, `removal_pending_in_use`,
  `removal_pending_permission` (RC15; the vocabulary follows OR3: "pending removal",
  [ruling Q10(3)]);
- a **WARN** `merge_source_mismatch` when the last run found an archived zip or eml failing
  its recorded checksum: `the last run found <N> archived file(s) failing their recorded
  checksum while re-capturing — run mailarchive verify -out "<out>"` (RC26 (d));
- `status -json` (still version 2): an additive object
  `recapture{kept_records, unverified, healed, legacy_kept, removal_pending{…by cause},
  merge_source_mismatch}`.

**`verify`** (internal/app/verify.go):
- files of `Superseded` entries are classified kind **`superseded`** with a count, never
  `unexpected` or `missing`, with this note: `superseded files are earlier copies a newer
  capture replaced; they are removed once <cause> clears — <remedy>.` (RC15);
- an unowned file whose stem is in the `-vN` family of a record's Path (same directory, the
  base stem with or without `-v2` … `-v99`) is kind **`earlier-copy`**: `<path> is an
  earlier copy of a message whose current copy is <current path>, left by an interrupted
  re-capture.` The remedy follows OR7 (Q8) (RC15); **[ruling Q8]** it is never removed
  automatically outside redaction, so the remedy compares the two copies and names
  redaction (delete it, then `reindex`) for one the user does not want;
- a record's `AlsoFiles` are counted as owned, never `unexpected` (RC17);
- a note for kept records: `3 records hold content their source no longer delivered (rows
  "source-lost" in attachments-report.tsv) — keep this archive backed up.`;
- `verify -json` goes to **version 2**, adding the `superseded` and `earlier_copy` counts,
  `kept_records` and the new Problem kinds. A new kind could surprise a consumer that
  enumerates kinds, hence the bump. The last-verify record and `status -json`'s
  `last_verify` gain additive `superseded` and `earlier_copy` counts (RC33).

**`reindex`:** `repaired 3 stale search row(s): the index pointed at an earlier file name;
the archived message is intact and was kept.` Each repair also prints its two paths and the
redaction remedy (§4.9, RC18). Adoptions print `adopted 1 earlier copy of a message whose
recorded file was missing: <path>` (RC36).

**README.md and README.txt** (internal/app/readme.go): a paragraph, "Re-capture never makes
the archive worse", plus these sentences:
- *"A message's files may carry a `-v2` (or `-vN`) suffix: a later capture of the same
  message with changed content was written beside the earlier one and the earlier copy
  removed; the copy linked from the folder page is the current one."*
- *"A message's page shows its read, flagged and category state as it was when the message
  was first captured"* (only if OR1 rules for option (a), RC5; OR1 is escalated as ESC-3
  [ruling ESC-3]).
- *"Rows of kind source-lost in attachments-report.tsv mark content this archive holds that
  the mail source no longer delivered."*
- *"A full re-export needs free space for the messages it re-captures, written beside their
  archived copies before the earlier copies are removed"* (RC8); the same sentence goes in
  the full-mode CLI help and next to the GUI's Full choice.

**GUI label** (RC26 (e), wording only, inside NG7): "Full — export everything again"
(cmd/mailarchive-gui/main.go:413) becomes "Full — re-check everything (unchanged messages
are not rewritten)".

### 4.12 Code map (what changes where)

| File | Change |
|---|---|
| `internal/state/manifest.go` | additive fields (version per OR2, RC29; version 6, [ruling J2]); `Record.Held/Kept/KeptBody/KeptHow/KeptAt/Unverified`; `Manifest.Superseded/PagesStale`; `Supersede` (records the page digest), `SupersededEntries`, `DropSuperseded`, `PathTaken`, `SetPagesStale`; `Held` validation (RC11); key and path migrations map `Superseded` (RC21); `Counts`/`HasIssues`/`RecaptureCounts` |
| `internal/state/lastrun.go` | additive counts, including `Healed` and `MergeSourceMismatch` |
| `internal/export/recapture.go` (new) | inventory, evidence tiers T1–T3 with the Held binding (RC11), label + content matching (RC4), comparison, verdict, HEAL decision (RC2), merge-zip staging with fresh headers and caps (RC22) |
| `internal/export/exporter.go` | `Export` restructured as render → stage → verdict → keep/unchanged/heal/commit/merge → rename → record switch; immediate deletes removed; W1 target selection in `stemFor` with the `-v99` cap, adoption and the space precheck (RC8, RC14); W4 record switch (RC1); KEEP stamping; legacy adopts (RC17); two-phase probe (RC24); `RawName` after the verdict (RC6) |
| `internal/export/attach.go` | `WriteZip` reports per-item inventory (sizes, CRC-32, entry names, empty vs failed) and no longer deletes `zipPath` |
| `internal/source/mbox.go` (mbox and maildir) | flag client placeholder parts (`X-Mozilla-Altered`, `X-Mozilla-External-Attachment-URL`, `text/x-moz-deleted`) on the attachment (RC34) |
| `internal/archivefs` (new, tiny) | `Inspect` with three outcomes (absent / refused / unreadable, RC9) and `os.Root`-based reads and removals (RC22). It **replaces every copy of the gate**: verify's `inspect`, `extractInspect` (extract.go:532), extract.go:364 and the rebuild walk (reindex.go:236), coordinated with the workspace's E4 slice (RC25) |
| `internal/archivefs` (or a sibling leaf package) | the anchored page-body reader and the read and entry-count caps, shared by htmlheader.go, rebuild and recapture.go, with a golden test over every page shape (RC25) |
| `internal/index/index.go` | `PathForKey(key) (string, bool, error)` |
| `internal/app/supersede.go` (new) | `finishSuperseded` with gates G1–G7, the lock re-check, the pages rule and per-entry causes |
| `internal/app/summary.go` (new) | `RecaptureSummary(Result) []string`, the one text for CLI, GUI and desktop (RC25) |
| `internal/app/run.go`, `graph.go` | `commit` returns the save error and re-checks the lock; deletion after a successful save; start-of-run sweep (and `SweepOrphans` in `RunGraph`, RC20); `pagesNeeded` + `PagesStale`; checkpoint gates count KEEP; pending-bytes checkpoint (RC8) |
| `internal/export/atomic.go` | `SweepOrphans`' zip rule runs after Load and skips owned zips (RC20) |
| `internal/app/reindex.go` | §4.9 table; deletions through archivefs; row paths enqueued; adoption; `-vN` family on redaction; `finishSuperseded`; `PagesStale` |
| `internal/app/reindex.go` (`app.Rebuild`) | runs `finishSuperseded` and clears `PagesStale` (RC20) |
| `internal/app/report.go` | `source-lost`/`kept`, `unverified`/`info` rows from typed fields |
| `internal/app/verify.go` | `superseded` and `earlier-copy` kinds; AlsoFiles owned; kept note; json v2; `-record` clears or refreshes `Held` (RC11) |
| `internal/health/health.go` | info lines, per-cause pending-removal WARNs, `merge_source_mismatch` WARN |
| `cmd/mailarchive/main.go`, `cmd/mailarchive-gui/main.go`, `internal/desktop/*` | print `RecaptureSummary` (one truth, X10); GUI Full label reworded |
| `internal/app/readme.go`, `README.md` | text |
| `docs/scenario-catalog.md`, `docs/ux-contract.md` | R28, S50/S51 (allocated ids, [ruling J1]), MA-350..364; X6 row names the new lines (S1); the amendments only once the owner answers ESC-2 [ruling ESC-2] |
| `docs/design-incremental-completeness.md` | B2 annotated "superseded by design-recapture-integrity §4.3" |

**No new dependency.** `archive/zip` raw copy and `os.Root` are in the standard library;
free-space queries use golang.org/x/sys, already in go.mod. **No `search.db` schema
change**: `PathForKey` uses the existing `UNIQUE(key)`.

### 4.13 Seams with other in-flight work

- **g2 N1 (an Export error aborts the store walk):** this design keeps the earlier copy
  intact on every error path. Whether the walk continues is N1's call.
- **g7 F49 (index batch rollback), F81 (power loss):** these are the source of stale rows.
  G3 holds deletions back while a row is stale, and §4.9 repairs the row.
- **g7 F50 (status points at `reindex`, not `-rebuild`):** with this design, plain
  `reindex` is safe and repairs stale rows, but it still never adds a missing row. The
  wording is g7's.
- **g7 F53 (a manifest save failure is only logged):** this design needs `commit` to return
  the save error. Both changes edit `commitInOrder` (run.go:357), so they must be
  sequenced, and either order works. This design makes the deletion conditional on a nil
  error; F53 decides the run's outcome.
- **g56 C2-2 (symlinked folders on the write path):** G4 refuses symlinks on the deletion
  path, and a re-capture into a symlinked folder is refused before writing (RC14). The
  first-capture write path stays C2-2's. **Sequencing (RC10):** S2 and g56's C2-2/P5 both
  rewrite `reindex`'s `deleteMessageFiles`. The archivefs gate lands once, in whichever
  merges first, and the other builds on it.
- **g56 C2-3 and C1-2 (redaction leaves FTS text and page listings):** unchanged.
  Redaction of a message whose earlier copy is still pending removal also removes that
  copy once its block clears: the prune deletes the index row, so G3 passes on the next
  sweep (RC14). Redaction also reaches DEMOTE-kept files (AlsoFiles) and the unowned `-vN`
  family (RC17, RC18). The redaction procedure is unchanged *for a fresh index row*; during
  a stale-row window `reindex` prints the extra step (§4.9, RC18). **[ruling ESC-2]** The
  `-vN` reach is a new R13 deletion case and waits for the owner; the AlsoFiles reach is
  MA-227's existing guarantee.
- **g3, the mime-fidelity design (`docs/design-mime-fidelity.md`)** (RC4, RC28):
  - *Frozen fingerprint.* Its P1 computes the fingerprint from a frozen v1-parser replay,
    while the exported attachment list comes from the fidelity parser, so labels, order and
    count can change under an equal fingerprint. This design therefore never matches by
    index, matches by label and content (RC4), and applies T2 only within one
    `ParseScheme`.
  - *Its P11 repair* is an automatic incremental re-capture that today renames the new zip
    and html over the old ones and keeps the same stem. **Rule:** the repair is a re-capture.
    It calls §4.3's verdict, with its own count precondition ("new ≥ old") kept as an extra
    gate, which closes the "a.pdf now empty plus a new GapKind part" hole. It writes through
    W1–W5, recomputing `Held` and clearing `Kept`. Its stable-stem promise and the §1.7
    window that in-place repair accepts are the subject of **OR3 (Q10)**: either the repair
    accepts `-vN` names, or it is a stated P5 exception with the §1.7 residual written next
    to R5. **[ruling Q10/OR3 = J6]** The repair accepts the names this design's commit
    gives (ESC-3 decides them), with no P5 exception. The mime design's "a decoded subject
    never renames a file" and its rollback claim are amended to match, and its R27 makes
    no file promise for repairs.
  - *Its R2 clarification and its new invariants* (revision 2 cited them as its R22; they
    are now R26 and R27, [ruling J1]). One joint R2 text, escalated together with this
    design's R2 amendment (ESC-2, [ruling J7]); the id collision is gone.
- **g2, the fault-isolation design (`docs/design-fault-isolation.md`)** (RC28):
  - *N1:* this design keeps the earlier copy intact on every error path; whether the walk
    continues is N1's call (above).
  - *Its supersede re-render* rewrites a content-free stub page in place (temp + rename) and
    is **an explicit P5 exception**: a kill can leave the stub page `modified` against its
    recorded fixity, which is the §1.7 window, stated next to the amended R5. OR3 confirms
    it. **[ruling Q10/OR3]** It no longer exists: fault-isolation revision 2 supersedes
    metadata only, writing no file (its FC6(4), FC7), so no P5 exception is needed and the
    R5 amendment drops it. Fault-isolation's heal-in-place goes through the shared exporter
    commit.
  - *Vocabulary.* g2 uses "supersede" for that mechanism, next to this design's
    `Superseded` list and verify kind. OR3 decides which design renames; the recommendation
    is that this design's user-facing terms become "pending removal" (`removal_pending`).
    **[ruling Q10(3)]** Adopted: this design says "pending removal" (`removal_pending`);
    fault-isolation keeps "superseded" for its metadata-only re-class.
- **The manifest version** (RC29, OR2, Q2; **[ruling J2]**: no bump, for all of them): g2, g3 and the local-confidentiality design
  (g56) all keep version 6 so that a rollout can be rolled back by swapping the binary. This
  design's default is now the same: **no bump**, with every field additive and §6 stating
  what an older binary does with each. One program-level ruling covers g2, g3, g4, g56 and
  the BodyFallback change; if it rules for a bump, RC29's expand/contract applies to all of
  them.
- **Catalog ids** (RC30, OR4) **[ruling Q11 = J1]:** allocated once across the five
  designs: this design's R28, S50 and S51; fault-isolation's R25 and S46–S48;
  mime-fidelity's R26, R27 and S49; local-confidentiality's R22–R24 and S42–S45; the
  workspace's R29 and S52.
- **The concurrent BodyFallback change** (Record.BodyFallback, an additive v6 field): body
  presence ignores the fallback (NG2). Its version stance is part of OR2 (additive v6,
  [ruling J2]).
- **The extraction workspace** (`docs/design-extraction-workspace.md`; the owner's decision:
  select a source, choose an export destination, copy; no drag-and-drop) (RC37). It adds no
  surface here, and it **never writes the archive**: its P1 refuses a destination inside
  the archive (MA-414, MA-436), so no R28 obligation flows to it. The seam is its
  **lock-free archive-source reader**:
  - superseded and `-vN` earlier copies are never record paths, so it never enumerates or
    emits them;
  - a `-vN` move or a deferred deletion between its snapshot and its copy surfaces there as
    its existing "changed in the archive during export";
  - `Unverified`, `Kept` and `KeptBody` are typed fields (RC23), so its notes never turn
    them into "attachment … never downloaded";
  - its reader is covered by the same version policy (OR2; version 6, [ruling J2]).

  The workspace design is asked to update its §4.14 and MA-431, which still describe N2's
  immediate deletion, to the W1/W5 semantics. `extract` (archive → mbox/eml) is read-only
  on the archive and unaffected.

---

## 5. Crash ordering and failure domains (R5)

### 5.1 A re-capture that changes bytes and moves P_old → T (COMMIT or MERGE)

The state below is what remains on disk if the process dies **after** the named step
completes. A hard kill is the worst case; SIGINT and SIGTERM stop at a message boundary
and save.

| # | Step | Persisted manifest | Files on disk | Direction | Operator sees / next run does |
|---|---|---|---|---|---|
| C0 | decide (reads only) | earlier record | earlier trio | nothing changed | nothing |
| C1 | stage eml, zip and html temps, each fsynced | earlier | earlier trio + `.mailarchive-*.tmp` | garbage only | next start of `Run`, `RunGraph` (RC20) or `reindex` sweeps the temps (age-gated, atomic.go:106); log "swept N orphaned temp/zip file(s)" |
| C2 | rename eml → `T.eml` | earlier | + `T.eml` (unowned) | extra file | `verify`: `earlier-copy T.eml`, naming the current path (RC15; exit unchanged) |
| C3 | rename zip → `T-attachments.zip` | earlier | + `T` zip | extra file | swept at the next start of `Run`, `RunGraph` or `reindex` (an unowned zip with no page, RC20) |
| C4 | rename html → `T.html`; directory fsync | earlier | earlier trio + T trio | harmless duplicate | `verify`: T files `earlier-copy`, exit 0 if otherwise attested. The earlier record still verifies. The next re-capture of the message adopts the T trio when its digests equal the staged bytes (RC8), rather than moving to `-v3` (NG5) |
| C5 | in memory: `Manifest.Add` + `Supersede` + `PagesStale`; index `Add` (may batch-commit) | earlier | same | duplicate; the index row may name T (exists) | search opens T; `reindex` re-points the row to `P_old` (manifest authority) |
| C6 | commit: history sync → index flush → manifest Save (temp fsync, rename, directory fsync) | **new record → T**; `Superseded ∋ P_old`; `PagesStale` | both trios | consistent; earlier copy pending | `verify`: `P_old` files `superseded`, exit 0; `status` (run never finished) RED as today for the dead run; the next start sweeps |
| C7 | G1–G5 pass → remove `P_old.html`, its zip and its eml, one by one | new; `Superseded ∋ P_old` | T trio + any `P_old` remnant | completes later | remnants classified `superseded`; the next start or `reindex` finishes; pages regenerate (`PagesStale`) |
| C8 | in memory: drop the entry | new; entry still persisted | T trio | no-op later | the next sweep finds the files gone and drops the entry |
| C9 | end of run: final commit → sweep → `pages.Generate` → `PagesStale=false` if the list is empty → Save | clean | T trio | done | summary lines (§4.11) |

At no row does the persisted manifest name a missing file or a file that fails its
recorded fixity (P5). Every residual is an extra file, never a missing one. Today's code
fails the other way: N2 at C5, N3 at C3.

**Other paths:**
- **KEEP:** no file writes. Only the kept fields and the timeline stamps change in memory
  (RC1). A kill before the save loses only the note, and the next re-capture recomputes it.
- **UNCHANGED:** temps are discarded, and W5's record switch runs at `prev.Path` in memory
  (RC1). A kill leaves temps (swept), and the stamps and the `Held` backfill are redone by
  the next full run. The persisted history never records a false `gone`: `SweepGone` runs
  only after the walk, and the walk stamped every unchanged record.
- **HEAL (RC2):** a staged unit whose digest equals the recorded fixity is renamed over the
  absent or damaged earlier unit, then the directory is fsynced. A kill leaves the unit
  either still damaged (as before the run) or healed; the persisted fixity matches the
  healed bytes either way, so there is never a false `modified`.
- **W3 (pre-fixity, in place):** stage, then rename over in order (eml, zip, html). A kill
  mid-renames leaves new and old units side by side on a record with **no** fixity, so
  `verify` reports `unrecorded` as before. The gap lists may be stale in the safe direction
  only, because P1 judged the new bytes no worse. A later retry corrects them. A
  **failure** after the first rename-over removes only the remaining temps and never the
  record's own files (RC16).
- **Failure without a kill** (ENOSPC, a rename refused by an antivirus handle): every fresh
  T-named file this attempt created is removed, the earlier trio is untouched, and `Export`
  returns an error naming the path. This is N3 fixed. RC8's space precheck makes ENOSPC
  from this design's own writes a KEEP (`low-disk`) rather than an error.
- **A failed checkpoint Save** (for example ENOSPC on the manifest temp): no deletion
  happens, because deletion follows only a nil Save. The trios written since the last
  durable save are unowned, and the next re-capture of each message adopts them (RC8).
  MA-358 injects this case.

### 5.2 Power loss, beyond process death

- Manifest saves are fsynced (temp + rename + directory fsync, manifest.go:1383-1430).
  They are durable when `Save` returns, *conditional* on the filesystem honouring fsync.
- search.db runs WAL with `synchronous=NORMAL` (index.go:32). A commit may roll back on
  power loss. After C7, the index can therefore revert to a row naming `P_old`, which is
  already deleted. That leaves a **stale row, not data loss**: search returns a broken
  link until the next re-capture or `reindex`, which repairs it (§4.9). G3 cannot prevent
  a revert that happens after its check. This residual is named here and in Q1's trade-off.
- A directory fsync is best effort on Windows and FAT (atomic.go:86-93), as today.

### 5.3 `reindex` (F48 path)

- **I1:** collect rows.
- **I2:** repairs (`idx.Add`, in a transaction), adoptions (RC36) and prunes (deletions of
  manifest-named paths through archivefs, row paths enqueued as Superseded entries,
  `DeleteByID`, `manifest.Delete`; RC10).
- **I3:** flush.
- **I4:** `finishSuperseded`.
- **I5:** pages.
- **I6:** report and README.
- **I7:** Save.
- **I8:** CompactHistory.

What a kill leaves:
- **During I2:** a repair is idempotent, and an unflushed one is redone by the next
  `reindex`; no file is ever deleted for a repair. A prune keeps today's redaction
  semantics.
- **Between I4 and I7:** the entries are still persisted, and so is `PagesStale` (I4's
  deletions required it to be persisted true, and only a later Save clears it, RC14), so
  the next run regenerates the pages.

### 5.4 Failure domains

| Component | Lost | Corrupt or compromised | Fails | Operator sees |
|---|---|---|---|---|
| `Superseded` list (a manifest restored from an older copy, or a sync conflict) | earlier copies linger; an older binary's save drops the list (§6) | stale or tampered entries: G1 (message-stem shape), G2 (unowned) and G4 (no symlinked component, regular file) limit deletion to unowned in-archive message files; **G6** drops an entry whose page no longer holds the superseded bytes (a reused name after B ↔ B-v2), and **G7** holds it while the superseding page is missing (RC12) | open, toward extra files, even under name reuse | `verify` `superseded` or `earlier-copy`; a log line per dropped entry |
| `Held` inventory | records fall back to T2/T3 | malformed: validated and treated as nil (RC11). Stale (an older zip restored, `verify -record`): the `Held.Zip` binding or the on-disk size mismatches, so that unit uses T3 on-disk evidence (RC11). A tampered `Entry` only selects a source entry inside the earlier zip; the output name is recomputed | closed (KEEP on doubt) | nothing, or a KEEP line |
| search.db | deleted: a fresh empty index has no rows, so G3 passes (G6/G7 still hold) | stale rows: G3 holds the deletion; `reindex` repairs. A `PathForKey` error holds it too (RC21) | closed | pending-removal WARN by cause, remedy `reindex` |
| Earlier zip | absent: holds nothing, so it cannot regress; a Full run re-creates it by HEAL when the staged bytes equal the recorded fixity (RC2) | damaged (modified): COMMIT only if no worse than the **on-disk** content (RC11); HEAL when the staged bytes equal the recorded fixity and the disk copy holds nothing more; otherwise KEEP, and no merge from it (R-G). Refused or unreadable: presumed held, KEEP (RC9) | closed | `verify` `modified` until healed; R-G or R-H |
| Earlier eml | absent: nothing to carry | modified, refused or unreadable: KEEP when it would have to be carried (RC9); HEAL when the staged raw equals `Fixity.EML` | closed | R-G-style line naming the eml |
| Disk space | the space precheck (RC8) refuses a moving re-capture before staging: KEEP `low-disk`, nothing written. ENOSPC from another program while staging: fresh temps removed, earlier copy intact. A failed checkpoint Save: no deletion; unowned trios adopted by the next re-capture (RC8) | — | closed | R-I, or a run error as today. Transient extra space: clamp(n/8, 1000, 25000) messages per store file or mailbox (about 12.5% of re-captured messages for archives of 8k–200k records, more below 8k), or the pending-bytes cap of max(1 GiB, 2 × manifest), whichever comes first, *conditional* on G3–G7 passing. With a gate holding, each blocked message keeps at most 98 extra copies (the `-v99` cap, RC14); `reindex` clears the `index-stale` and `index-not-open` causes, not `in-use` or `permission` (RC15) |
| Legacy adopt (pre-v5 live records) | — | — | open, toward extra files | the first full Graph run after the upgrade keeps an earlier file beside every legacy record whose bytes changed: about the pre-v5 share of the archive, once, owned through AlsoFiles and counted (R-F, status; RC17, OR6; [ruling Q7]: paid, DEMOTE kept) |
| Windows handle on an earlier file | deletion G5 holds it pending | — | open, toward extra files | R-E (`in-use`, naming the path); next run |
| Clock | `KeptAt` is informational only; temp sweeping is age-gated as today | — | — | — |

---

## 6. Migration and coexistence

- **Existing v6 archives.** The first load under this build reads every record (no
  `Held`, no `Superseded`). Nothing is rewritten by the upgrade itself. Under the default
  (no bump, RC29) the manifest stays version 6.
  - **Incremental runs** re-capture only fillable records (T2/T3 evidence) and record
    `Held` on each commit.
  - **A full run over an unchanged source with fixity** writes nothing, stamps every
    record (RC1) and backfills `Held` (W4). A record whose page differs only because a
    renderer release or mutable client state changed since its last capture moves once to
    `-v2` and records `Held.Content`/`Held.Render`; later full runs are content-based (RC5).
  - **A full run also heals** (RC2): it hashes each earlier unit once, like `verify`, and
    renames identical staged bytes over any unit that is missing or damaged. The stated cost
    is one read of the archive per full run, on top of today's full re-read of the source.
  - **A full run over a pre-fixity archive** rewrites in place when the name is unchanged
    (W3), gaining fixity with names unchanged (a carried pre-fixity `.eml` is baselined,
    RC35 (f)), and moves through deferred deletion when the naming rule changed (the N2
    population).
  - **Archives already damaged by N2** are healed by `reindex`'s adoption (§4.9, RC36).
  - **Legacy-scheme live records (pre-v5 Graph archives)** (RC17). On the first full Graph
    run, a record whose staged bytes equal its recorded fixity is adopted in place. Every
    other legacy record commits under a fresh name and keeps its earlier file beside it,
    owned through AlsoFiles. The one-time growth is about the pre-v5 Graph share of the
    archive, once; R-F and `status` count it. Whether to pay it is OR6 (Q7). **[ruling Q7]**
    It is paid: DEMOTE stays (NG4).
  - **`unknown` records** go through T3: they COMMIT if no worse, otherwise become
    UNVERIFIED.
- **Mixed versions** (OR2 decides; RC29). **[ruling Q2/OR2 = J2]** No bump: the first case
  below is the one that applies, and the "with a bump" case is kept only as the record of
  what was rejected.
  - **Without a bump (the default):** an older binary loads the manifest and, on its next
    save, drops the additive fields. Field by field, in the safe direction:
    - `Held` dropped → this build falls back to T2/T3 evidence;
    - `Superseded` dropped → pending earlier copies linger unowned. W1's existing-file rule
      still protects them, and `verify` shows them as `earlier-copy` (RC15);
    - `Kept`, `KeptBody`, `KeptHow`, `Unverified` and `PagesStale` dropped → the notes are
      lost, and the pages regenerate at the next exporting run.

    While it runs, the older binary's exposure is today's F46/N2/N3/F48 code. One excursion
    loss is specific to this design: an older binary re-capturing a DEMOTEd record back
    onto B overwrites the kept original, which is the same exposure as today's in-place
    overwrite. Rollback is a binary swap: the `-vN` files are named by the manifest the
    older binary reads.
  - **With a bump (only if OR2 rules for it; [ruling J2]: it did not):** expand/contract. Release N ships a `Load`
    that accepts 7 while its `Save` still writes 6, so rolling back to release N works.
    Release N+1, which bundles S4 and S5, starts writing 7; S3's write never ships alone. A
    binary of v3 or later then refuses a version-7 manifest with "written by a newer
    mailarchive (format version 7…)" (manifest.go:536, MA-132), and a schedule still
    pointing at an old binary fails visibly: the last-run record is `failed`, and `status`
    is RED with the loader's wording and names the upgrade. **Residual:** a pre-guard v2
    binary has no such refusal (manifest.go:531-534, "this only protects v3 and later"); it
    loads, drops the new fields and runs today's code. The README rule stands: upgrade every
    writer of a shared `-out`.
- **Rollback of the rollout.** Without a bump it is a binary swap (above). With a bump there
  is no in-place downgrade of a version-7 manifest, as with every earlier format bump.
  Rolling the binary back past release N then means restoring the archive from the
  pre-upgrade backup, which **discards every capture since the upgrade**. For Graph mail
  removed from the mailbox since, that loss is permanent. The manifest, search.db and the
  message files must be restored together, since files may have moved to `-vN`. The
  restore also reintroduces the defects this design fixes.
  - **Fleet note (MSI/RMM):** the desktop MSI is a MajorUpgrade with a
    DowngradeErrorMessage (Product.wxs:37-39). A rollback is an uninstall plus a reinstall,
    after which, under a bump, every managed machine's scheduled capture fails at Load until
    its archive is restored. This is stated for the owner in Q2. **[ruling J2]** With no
    bump, a rollback stays a binary swap on every machine.
- **Schedules.** No job grammar changes and no new flag. A scheduled `-mode full` job now
  produces KEEP lines instead of destroying content, writes nothing for unchanged content,
  and heals damaged units. The schedule's "-mode full: every scheduled run re-exports
  everything" note (schedule.go:294, and :360, :708) stays true.
- **Existing guards whose asserted behaviour changes.** These need **operator acceptance**
  (CLAUDE.md: never self-granted; Q5). **[ruling Q5 = ESC-2, J7]** The changes that narrow
  or rewrite a guard are escalated to the owner (ESC-2) and none lands until the owner
  answers. The additions and strengthenings land with their slice. Item by item:
  - **MA-66**'s covering test `TestRetryPromotesOnAnyRecoveredItem`, run 2: from
    "`Missing=[a.txt]`, zip without a.txt" (it explicitly accepts "another item regressed
    meanwhile") to MERGE, complete, a.txt carried, `Kept=[a.txt]` (MA-353). The MA-66 row
    text stays true. **[ruling ESC-2]** Escalated: S5 does not change this test until the
    owner answers.
  - **MA-87**'s covering test `TestStemCollisionAndRename` (identity_test.go:166): (a) the
    earlier html and zip are removed after the durable commit and the sweep, not
    immediately after `Export`; (b) its synthetic step leaves the original B file on disk
    unowned, and under W1 the message moves to `B-v2` rather than overwriting it.
    **[ruling ESC-2]** Escalated: S4 does not change this test until the owner answers.
  - **S24**, **R5** (scoped, RC28) and **R13**: wording (§2.1). **[ruling ESC-2, J7]** S24
    and R13's new cases (including N2 adoption) are escalated (ESC-2). R5's write-beside
    sentence is an addition and lands with S4 only if the owner answers ESC-3 with (a).
  - **R28**: its restated text (RC19). **[ruling J7]** R28 is new text, not an existing
    guard, so it lands with S1.
  - **R2**: wording (§2.1), one joint text with the mime-fidelity design. MA-22's counts
    are unaffected because UNCHANGED and HEAL count in `Exported`. **[ruling ESC-2]**
    Escalated, together with the mime-fidelity clarification.
  - **MA-201 and S39**: version ceiling 6 → 7, only if OR2 rules for a bump. **[ruling
    J2]** No bump: unchanged.
  - **design-incremental-completeness B2**: superseded. **[ruling ESC-2]** Escalated.
  - **MA-238** is *strengthened*, not changed: it now also holds when the path is
    unchanged, and the kept earlier file is owned through AlsoFiles (RC17). **[ruling
    J7]** Accepted.
  - **MA-227** is *strengthened*: no copy of a redacted message stays served, including
    DEMOTE-kept files and the `-vN` family (RC18). **[ruling J7, ESC-2]** Its AlsoFiles
    reach (which covers DEMOTE-kept files) is accepted. Its `-vN`-family clause is R13's
    deletion case (c) and is escalated (ESC-2; corrected after check).
- **The README's upgrade guidance** ("full re-export to gain fixity") stays correct, and is
  now safe on degraded sources.

---

## 7. Operator walk

Step counts are actions the person takes. Friction type: **I** is security- or
integrity-inherent (document it); **II** is a design choice (fix it now or rule on it).

### 7.1 Home user, GUI, Thunderbird IMAP: "Full — export everything again" after detaching attachments

1. Open the GUI.
2. Pick the source.
3. Choose Full.
4. Run.
5. Read the summary.

*Today:* the same 5 steps. The detached attachments are deleted from the archive, the
summary says "still missing content … download for offline use … re-run" (advice that
cannot work), and verify attests.
*After:* the same 5 steps. The summary shows R-A, so the user is told nothing is needed,
and nothing is lost. **0 extra steps.** Type I. *Conditional, lab-pending* (RC34): this
holds for the placeholder shapes the client placeholder rule recognises. Thunderbird's
"delete attachment" renames the part, so the record #fp-splits and both copies survive
without a KEEP. Confirmation on a real profile is pending (§9 Lab).

### 7.2 Home user, GUI or CLI (`app.Run`), Thunderbird IMAP, incremental run with a torn read during a fill (RC27)

1. Run the incremental export (GUI "Run", or a scheduled CLI job).
2. Read the summary.

*After:* R-C (MERGE) when the record was captured under v7 and something improved; R-A
with `unreadable` wording when the torn item was the only change (it heals on the next
run, MA-352); R-B (the improvement waits) for a pre-v7 record with an ambiguous entry name
(NG8). **0 extra steps.** R-B's remedy for this on-demand source is optional and in the
mail app. Type II, ruled by Q6. **[ruling Q6]** After S7's backfill, a pre-v7 record
whose zip matches its recorded fixity has a `Held` inventory and merges; R-B stays only
for records the backfill could not bind.

### 7.2a Home user, desktop dashboard ("Archive now"), Microsoft 365 (RC27)

1. Click "Archive now".
2. Read the Activity line.

The dashboard runs only `app.RunGraph` in Incremental mode (internal/desktop/capture.go).
The Graph fast path never re-captures an archived message (NG12), so this user meets no
KEEP or MERGE. What can reach them: R-E from the start-of-run sweep (with the dashboard's
own remedy, Rebuild index, RC20) and R-L. **0 extra steps.** Type I.

### 7.2b Operator of a scheduled `graph -mode full` job, who sees the archive only through `status` (RC27)

1. Run `mailarchive status -out …` (or read the RMM's copy of `status -json`).
2. Read the posture and the info lines.

*Today:* every full run rewrites the archive, destroys degraded content, and reports
nothing beyond counts. *After:*
- unchanged messages are not rewritten and are still stamped, so the go-back view never
  shows a false `gone` (RC1);
- kept, healed, unverified and legacy-kept counts appear as info lines;
- a pending removal or a merge-source checksum mismatch is a WARN naming its remedy
  (RC15, RC26 (d)).

**0 extra steps**; any action is named by the WARN line. Type I.

### 7.3 Admin: README-recommended full re-export of a v0.2 archive after upgrading; power cut mid-run

1. Install this build.
2. Run `mailarchive -mode full -input … -out …`.
3. *(Power cut.)*
4. On the next scheduled or manual run, the start-of-run sweep finishes any deferral and
   regenerates the pages. Nothing to type.
5. Optionally run `mailarchive verify -out …`. A verify run before step 4 lists `P_old`
   files as `superseded`, not `missing`, and exits 0 if the archive is otherwise attested.
6. Optionally run `mailarchive status`. RED shows only for the dead run (R18, as today);
   GREEN after the next run.
7. **The crash orphans** (RC15). The new trios written after the last durable save sit on
   disk unowned. `verify` lists them as `earlier-copy`, each naming the current copy and
   the cause ("left by an interrupted re-capture"), never as `unexpected` with the
   old-binary note. Re-running the interrupted full re-export **adopts** each orphan whose
   digests equal the staged bytes (RC8), so most of them are re-owned with nothing to type.
   An orphan the re-run did not adopt (its message changed again since) stays listed;
   whether `reindex` removes it is OR7 (Q8). **[ruling Q8]** It does not: the orphan stays
   listed as `earlier-copy`, and the remedy line compares the two copies and names
   redaction for one the user does not want.

*Today:* the same crash leaves records naming deleted files, verify RED with missing and
unexpected entries, and no repair except another full run, or `reindex -rebuild`, which
deletes the records. *After:* **manual repair steps go from 1 or more (with data loss) to
0**, and the remaining orphans, if any, are named with their current copy. Type I for the
deferral; the orphan remedy is Type II, ruled by OR7 (**[ruling Q8]**: no automatic
removal).

### 7.4 Admin: the run warns of index errors (F49), then runs `reindex` as told (main.go:1413)

1. Read the warning.
2. Run `mailarchive reindex -out …`.
3. Read "repaired N stale search row(s) … the archived message is intact and was kept".

*Today:* step 2 deletes the live files and records (F48). Steps are unchanged; the outcome
is safe. Missing rows still need `-rebuild` (g7 F50). Type I.

### 7.5 Inheritor opening the archive cold (no tool)

1. Open `README.txt`, which explains `-vN` names and `source-lost` rows (§4.11).
2. Open the root `index.html`, then a folder page. Pages link only current copies,
   because pages regenerate after deletions (`PagesStale`) and no run deletes without
   regenerating them (RC14). This holds *for a fresh index row*: during a stale-row window
   (F49, a power-loss revert) a page can link an earlier copy until `reindex` repairs the
   row (§5.2, RC18).
3. Open a message, whose page links its zip and its eml, including an eml carried from an
   earlier `-raw` capture (RC6).
4. Optionally open `attachments-report.tsv` in a spreadsheet. `source-lost/kept` rows mark
   the content that exists *only* in this archive.

**4 steps, the same as today.** Earlier copies left by an unfinished deferral are unlinked
files. README.txt's sentence ("the copy linked from the folder page is the current one")
covers them. The `-vN` suffix is Type II, ruled by Q1, and the friction review owes the
cold walk. **[ruling ESC-3]** Q1 is escalated to the owner. Until it is answered,
today's write path stays, and S4, which introduces W1's `-vN` names, does not start.

---

## 8. Security

**Trust boundaries:**
- Mail content from any source is untrusted. That covers names, Content-IDs, bytes,
  stream behaviour, and Graph listings.
- The manifest is untrusted on read (the INS2-1 posture: every path is gated before use).
- Archive files on disk are untrusted on read: the earlier zip, page and eml can be
  replaced, rotted or planted.
- The search index is untrusted only for the paths it names: G3 compares, it never acts
  on a row path. `reindex` never deletes a row path directly either; it enqueues it through
  the sweep's gates (RC10), so this sentence holds for every deletion path.
- The trusted computing base is the running binary, holding the archive lock.

**Attacker personas considered:**
1. **A hostile sender** (crafted names, Content-IDs, zero-length or erroring streams).
   - Worst case on a T1 record: a forced KEEP or a refused merge. The archive keeps its
     earlier copy and the hostile message costs a render per full run, which is no more
     than today.
   - A label equal to a sentinel (`body`, `unknown`, `unverified`) makes the evidence
     ambiguous, so the outcome is KEEP; status never lives in the label namespace (RC23).
   - **Same-envelope forgery (RC35 (a)).** Identity is the Message-ID plus an envelope
     fingerprint, all sender-controlled (model/message.go:146-161). On the folder-scoped
     local path, a forged copy with the same Message-ID and envelope is therefore a
     "re-capture" of the genuine record. A T1 MERGE requires the new body digest to equal
     `Held.BodyDigest`, so a forged body is never merged with genuine carried entries.
     **Residuals, stated:** a T2 (pre-v7) record has no body digest, so a forged body can
     MERGE with genuine carried entries there; and a forgery that regresses nothing
     COMMITs, as it replaces the record today (NG2/NG3).
   - On the live path the sender cannot alter a delivered message server-side.
   - Carried entries get a freshly computed sanitized output name and a fresh FileHeader
     (RC22), so a crafted name or header field cannot introduce zip-slip into the new zip.
2. **A same-user local process or malware with write access to the archive.**
   - It can already delete or alter files and rewrite the manifest and its fixity (README:
     "the manifest is not signed"). No new capability is granted.
   - A tampered `Superseded` entry can at most delete an *unowned* in-archive message file
     (G1: in-archive path with the message-stem shape; G2: unowned; G4: no symlinked
     component, regular file only, removed through `os.Root`; G6: the page still holds the
     superseded bytes; G7: the superseding page is present), which the attacker could delete
     directly. `index.html` and folder pages never match G1 (RC12).
   - A fixity-less earlier zip with valid planted CRCs can be carried into a new zip and
     so gain a fresh digest. That is the same laundering `verify -record` already allows
     the same user. Stated, not a new boundary. Only the bytes are carried: the header is
     rebuilt (RC22).
3. **Another local account.** *Conditional* (RC35 (b)): this holds on filesystems that
   enforce POSIX modes or ACLs. File modes are unchanged (archive files 0600); this design
   creates no new files with wider modes. search.db's mode is g56's C2-1. On FAT or exFAT,
   which keep no modes, every account and every machine the drive is plugged into is inside
   the trust boundary, and personas 2 and 4 become cross-user.
4. **A tampered or conflicted synced manifest** (cloud-sync conflict, an old-binary
   excursion, a manifest restored from a backup as manifest.go:525 advises).
   - Under OR2's default there is no version gate; with a bump, the gate refuses an older
     writer's view. **[ruling J2]** No bump, so there is no version gate; the gates below
     carry the defence.
   - Gates G1/G2/G4/G6/G7 hold regardless. G6 and G7 make deletion content-bound, so a
     stale list fails toward extra files even after B ↔ B-v2 name reuse (RC12).
   - `Held` is validated, and bound to the zip digest it was read from (RC11).
     `Held.Entry` is used only to *look up* a source entry inside the earlier zip, never
     as a filesystem path or an output name.
   - Reads are bounded: min(`Held.Size + 1`, 512 MiB) per entry in T1, the 512 MiB cap in
     T2/T3, and at most 65,535 central-directory entries (RC22), which defuses a zip bomb
     and a central-directory flood.
5. **Integrity and concurrency.**
   - Every writer and reader that mutates holds the exclusive archive lock (R5, MA-85):
     export, Graph, reindex, verify (`-record`), and the desktop dashboard in-process.
   - The sweep runs only under the lock, and **re-checks it** with `lock.StillHeld()`
     before its first deletion and every 100 deletions, at every call site (RC13). Today
     the lock is re-checked only at cadence-gated checkpoints (run.go:216, graph.go:271);
     the boundary and final commits now re-check it too. A lost lock stops deletions and
     leaves the entries pending. The residual is a microsecond-scale window between the
     check and a removal, and the pre-existing manifest-overwrite window, which is g7
     F53's.
   - Removals go through `os.Root`, so a directory component swapped for a symlink between
     G4's check and the removal is refused (RC22).

**New surfaces:**
- No network surface, no flag (NG1, NG7), no new file outside the archive.
- New manifest fields (untrusted input, gated as above).
- New report rows (untrusted text, scrubbed by `tsv()`).
- The new **deletion paths** (the sweep, and reindex's changed prune rules, including the
  `-vN` family on redaction) are security-relevant destructive code driven by manifest
  content, which is why S2 and S4 owe an adversarial pass.
- The merge reads untrusted zips from disk, and HEAL renames over a recorded file, which is
  why S4 and S5 owe one.

---

## 9. Test plan — MA-350..MA-364 (all tier U, CI, infra-free)

**Conventions:**
- **Fault injection** uses a test-only kill-point seam (RC31). A package variable hook is
  called after step Cn and, **from inside the callback**, copies `out/` to a snapshot
  directory: the manifest, search.db with its `-wal` and `-shm`, the lock file, the history
  log and every message file. This happens before any defer or return can commit: a
  returned error still reaches the file-boundary commit (run.go, after `runFile`), a panic
  is recovered per message (run.go:521-527), and the deferred `idx.Close` commits the
  pending batch (index.go:251-256). It follows checkpoint_test.go:36-58's
  observe-from-the-callback pattern. The next Run and `verify` then run against the
  snapshot, which is the residual state a hard kill leaves. Alternatively, the test
  re-execs itself and SIGKILLs the child at Cn. MA-358 asserts that the stale lock and the
  uncommitted index batch are present in the snapshot, so a harness that observes
  post-commit state fails. Mocks are used only for this, for injected I/O errors, and for
  the fake Graph server, as CLAUDE.md allows.
- **Ids are allocated** (RC30; **[ruling Q11 = J1]**): revision 2's provisional R22, S42
  and S43 are R28, S50 and S51, and every Covers cell below cites the allocated ids. A
  covers marker never cites a provisional label.
- **Refusal-shaped outcomes** are asserted with `assure.Refused(t, rc, summary,
  assure.Code(0), assure.Names(<file>, <remedy or report>), assure.NoSideEffect(<earlier
  bytes unchanged ∧ no new file under the store>))`. Every collection under assertion is
  wrapped in `assure.Reached`.
- **Every refusal test runs its positive twin first** on a healthy fixture.
- **Prove-fail plans:** each row names the mutation that must turn it RED, and why.

| ID | Tier | Asserts | Covers |
|---|---|---|---|
| MA-350 | U | Full mode over an on-demand local source (mbox) whose same-name attachment is now zero bytes → **KEEP**: zip, page and eml bytes equal their recorded fixity; the record has no `report.pdf` in Missing; `Kept=[report.pdf]`; `Stats.Kept=1`; the report has a `source-lost/kept` row; `verify` exit 0; summary R-A names the report. Twins: an intact source → UNCHANGED (`Kept=0`); a source that gained a body → COMMIT | R28, R1, S50 |
| MA-351 | U | The body now missing (the Outlook offline-window analogue: same message, empty bodies) in Full mode → KEEP, page byte-unchanged, `Kept=[body]`, "body" not in Missing. In a fill retry where an attachment arrived but the body vanished, the KEEP is decided **at the probe** and no `.mailarchive-*.tmp` is ever created (hook counter). Twin: body present → no KEEP | R28, R1, S50 |
| MA-352 | U | An attachment stream now **errors** in Full mode → KEEP with reason `unreadable`; the next full run with a healthy stream → UNCHANGED, `Kept` cleared (a transient fault heals). Twin: a healthy stream throughout | R28, R1, S50 |
| MA-353 | U | **MERGE** (T1): a v7 record holds a.pdf, b.pdf is fillable. A retry finds b.pdf present and a.pdf empty → the new zip holds a.pdf byte-identical to run 1 (raw-copied, CRC intact) and b.pdf; the record is complete; `Kept=[a.pdf]`; `Held` a.pdf `Kept=true`; `Filled=1`; the earlier trio is superseded and removed after the commit; `verify` exit 0. T2 variant (`Held` stripped, unique names) → MERGE; T2 with a duplicate file name → KEEP (NG8). Replaces MA-66's run-2 expectation (Q5; **[ruling ESC-2]**: escalated, and S5, which builds this row, waits for the owner's answer). Twin: no regression → plain COMMIT | R28, R1, S50 |
| MA-354 | U | **Unreliable merge source:** as MA-353, but the earlier zip has a flipped byte (fails `Fixity.Zip`) → KEEP; the earlier zip is byte-unchanged and still `modified` in verify; b.pdf stays Missing; R-G names the zip path, "restore … from a backup" and `mailarchive verify`. A fixity-less earlier zip with a bad CRC → KEEP. Twin: an intact earlier zip → MERGE | R28, R1, R5, S50 |
| MA-355 | U | **Legacy:** (a) a T2 v6 record (no `Held`, equal fingerprint) whose same-name attachment is now empty in Full mode → KEEP; (b) a version-1 manifest (`unknown`) over a degraded source → **UNVERIFIED**: files byte-unchanged, `Unverified=true` (no label in `Terminal`, RC23), the next incremental run has `Retried==0`, the report row is kind `unverified`, R-D; (c) `unknown` with an inline image embedded in the earlier page and now empty → KEEP (T3 inline presumption). Twin: (d) `unknown` over an intact source → COMMIT with real gaps and `Held` | R28, R1, R5, S50, S6 |
| MA-356 | U | **Live path** (fake Graph server), Full mode, immutable-id match, an attachment **removed entirely** server-side (same PhysID, different fingerprint) → KEEP by label comparison; Path and Fixity unchanged; `LastSeen==runAt`, `Present=true`, `Folder` updated and a history folder-assertion written on a move; `SweepGone` does not mark it gone. A legacy-scheme record → DEMOTE: commits to a fresh name, the earlier file stays byte-unchanged on disk at the **same** path (strengthens MA-238), R-F names it. Twin: attachment intact → UNCHANGED | R28, R21, R17, R1, S50 |
| MA-357 | U | **Preserved original:** a `-raw` archive. (a) A full run *without* `-raw`, unchanged path → `.eml` byte-unchanged and `Fixity.EML` kept (verify exit 0). (b) The same with a forced naming change → the eml moves to the new name byte-identical with the same digest, and the earlier eml is removed only after the commit. (c) A MERGE with `-raw` over a degraded mbox → the earlier eml is carried and the degraded raw never written. (d) An earlier eml with a flipped byte and no new raw → KEEP. Twin: `-raw` over an intact changed source → new eml written | R28, R20, R5, S50, S34 |
| MA-358 | U | **Crash ordering (N2, §1.7):** a moving re-capture is stopped after each of C1..C8 (§5.1). After each stop, with everything reloaded from disk: every record's files exist and match the persisted fixity (verify exit 0); no record names a missing file; after C6 and C7 the earlier files are `superseded` (never `missing` or `unexpected`), and the next incremental run's start sweep removes them and regenerates the folder pages; before C6 the new files are at worst `earlier-copy` (RC15) and nothing is deleted. Same-path variant (§1.7): no stop point yields `modified`. RED on today's code reproduces the finder's numbers | R28, R5, R8, S51, S2 |
| MA-359 | U | **Html write failure (N3):** an injected ENOSPC on the html temp, and a rename refusal on the html → the earlier trio is byte-unchanged and attested (no `missing`, no `modified`); no T-named file remains; `Export` returns an error naming the html path; the next run commits. Twin: no fault → COMMIT | R5, R28, S51 |
| MA-360 | U | **Deletion gates and sweep:** an entry stays pending when (a) the superseding key's index row still names the earlier path (an index failure injected by a SQLite trigger, as in F48's PoC), (b) a search.db exists but the run is `-index=false`, (c) a record owns the path (planted), (d) a directory component is a symlink (skipped on Windows); (e) a tampered entry with `..` is dropped and nothing outside the archive is touched. Each pending entry is shown by `status` (the pending-removal WARN for its cause, with that cause's remedy, RC15) and by R-E. Twin: after `reindex` repairs the row, the next sweep removes the files and status clears | R13, R5, R8, R4, S51 |
| MA-361 | U | **`reindex` (F48):** a 3-message archive whose records name new files while the index rows name earlier, missing ones → reindex deletes **zero** message files, re-points every row (searches hit `rec.Path`), keeps the records, prints repaired=3 pruned=0. A message whose current *and* earlier paths are both missing is pruned. Twin: deleting a record's current html still prunes it and deletes its zip, eml and AlsoFiles (MA-40 and the redaction-reach rows unchanged) | R13, R8, S51, S13 |
| MA-362 | U | **Unchanged re-capture:** a full run over an unchanged source on a v7 archive writes no file (inode, mtime and bytes unchanged; `Unchanged==Exported==N`; no `Superseded` entry). A v6 archive with fixity gets `Held` backfilled without file writes. A pre-fixity archive with an unchanged naming rule is rewritten **in place** (W3), names unchanged, fixity recorded. Twin: a changed body → COMMIT to `B-v2` and the earlier trio removed after the commit | R2, R5, R28, S50 |
| MA-363 | U | **Surfaces:** the CLI summary prints R-A..R-L under their conditions with what, why and remedy and the report path; the report has `source-lost/kept` and `unverified/info` rows with subject and date; `status` prints the kept-copies and unverified info lines with the posture unchanged and the per-cause pending-removal WARNs with their codes and remedies (RC15); `status -json` carries `recapture{…}`; `verify` prints the `superseded` count and the kept note, and `verify -json` is version 2 with `superseded` and `kept_records`; the GUI summary and desktop Activity carry the same counts (X10); `reindex` prints repaired. Twin: a clean archive prints none of these | X2, X6, X10, R18, R1, S50, S51 |
| MA-364 | U | **Format and names:** the additive fields (`Held`, `Kept`, `KeptBody`, `KeptHow`, `Unverified`, `Superseded`, `PagesStale`) round-trip; a manifest without them loads (no `Held`). Under OR2's default the saved version stays 6 and `Load`'s ceiling is unchanged (**[ruling J2]**: this is the case built; the bump case is not); if OR2 rules for a bump, release N loads 7 and saves 6, release N+1 saves 7, and `Load` refuses version 8 naming the file and the remedy, byte-unchanged (RC29). W1 never picks `prev.Path`, a `PathTaken` path (owned, AlsoFiles, pending) or an existing file: a planted unowned file at B is never overwritten, and the message goes to `B-v2`; the path budget holds with the suffix. `PagesStale` forces page regeneration on a later run with `exported==0`, is cleared by a regeneration with no deletion since, and is set again before any later deletion (RC14); `reindex` and `Rebuild` regenerate. Twin: the ordinary path | R5, R4, R12, R28, S51, S39 |

**Revision 2 twins and variants per row.** These are part of each row's assertions. They
stay inside the reserved MA-350..364 range:

| Row | Added by revision 2 | Condition |
|---|---|---|
| 350 | T1 and T2 twins: two full passes over the same degraded source → `Kept=1`, `Merged=0`, no file written, no Superseded entry on the second pass. A non-empty client placeholder part (`X-Mozilla-External-Attachment-URL`) → KEEP, never `changed`. A T1 inline-image regression → KEEP | RC3, RC34, RC35 |
| 351 | A non-improving fill probe streams no held attachment (hook counter) | RC24 |
| 352 | The KEEP prints the `unreadable` wording, not "only copy" | RC26 |
| 353 | A same-envelope copy with a different body (T1) → KEEP, not MERGE. The carried entry's header has the reset Extra, ExternalAttrs, Flags, Comment and Modified; an encrypted earlier entry → KEEP | RC35, RC22 |
| 354 | An injected read error on the earlier zip → KEEP with R-G's read-error wording (no backup advice) and no `merge_source_mismatch` WARN; a checksum mismatch raises the WARN. Injected EACCES on the earlier zip → KEEP (presumed held). A central directory with more than 65,535 entries → KEEP | RC26, RC9, RC22 |
| 355 | Frozen-fingerprint label drift (v1 `[report.pdf]` → fidelity `[note.txt, report.pdf now empty]`) → KEEP, never COMMIT; a relabelled item with unchanged bytes → matched by CRC, not a regression. A truncated earlier zip → KEEP. Attachments named `body`, `unknown` and `unverified` → KEEP (ambiguous), never a sentinel. (b) gains a full-run re-check twin: the source recovers → COMMIT, `Unverified` cleared. (b)/(c) assert the presumed wording. A T2 inline-image regression → KEEP | RC4, RC9, RC23, RC35, RC26 |
| 356 | A live-path UNCHANGED twin: `LastSeen==runAt`, `Present`, `SweepGone` returns nothing, a moved message gets its folder-assertion and index folder. report.pdf absent plus an improved item → KEEP, the earlier trio byte-unchanged, nothing superseded. A byte-identical legacy-scheme record → adopted in place, `FpScheme` re-stamped, no file written; a changed one keeps its earlier file in AlsoFiles and prints R-F | RC1, RC3, RC17 |
| 357 | `-raw` on, then off between full runs → UNCHANGED, the eml link present, the eml byte-unchanged. (c) asserts the merged record's residual: the carried eml lacks the filled item. A refused earlier eml → KEEP. A carried pre-fixity eml is baselined (verify exit 0) | RC6, RC7, RC9, RC35 |
| 358 | Uses the snapshot harness; asserts the stale lock and the uncommitted index batch are present. A RunGraph kill at C1 → its temps are swept by the next RunGraph. ENOSPC injected on staging and on the checkpoint Save → the earlier copies are intact, the next run converges (adopts or sweeps), status is not wedged | RC31, RC20, RC8 |
| 359 | W3 twin: the html rename is refused after the eml and zip renames → the eml and zip are present and readable, and the old page still links them | RC16 |
| 360 | An older manifest and search.db restored after B ↔ B-v2 alternation → nothing deleted, the entry dropped and logged (G6). The lock file replaced before the final commit → no file deleted. A re-key with a pending entry → the entry follows the key. Each cause shows its own WARN code and remedy. A symlinked target folder → KEEP before any write (R-J) | RC12, RC13, RC21, RC15, RC14 |
| 361 | A symlinked folder and injected EACCES on `rec.Path` → nothing deleted, row and record kept. A suffix-less row path naming another record's stem → nothing deleted. Cases A, B and C print both paths and the redaction remedy, with zero deletions. An MA-227 twin: a message with a DEMOTE-kept file and a `-v2` orphan is redacted → no copy remains under `/files/`. An N2 victim built from the finder's PoC state → adopted, verify attests. **[ruling ESC-2]** The `-v2` orphan half of the MA-227 twin and the N2-adoption twin are escalated: S2 lands each as a `t.Skip` encoder citing MA-361 and R13, and the slice that builds it after the owner's answer removes the skip. The DEMOTE-kept half (AlsoFiles, accepted under J7) lands with S5, which builds DEMOTE | RC10, RC18, RC36 |
| 362 | HEAL twins: a deleted zip plus a full run → the zip is re-created and verify attests; a size-preserving flipped byte → healed in Full mode; a restored older zip holding an extra entry → KEEP, not healed. A read flag flipped and a category added between two full runs → UNCHANGED, no file written. The live-path UNCHANGED twin of the first full run after an upgrade → every record stamped, no `gone` event | RC2, RC5, RC1 |
| 363 | `RecaptureSummary` text is identical across CLI, GUI and desktop. R-H..R-L print under their conditions. The per-cause WARN codes and `merge_source_mismatch` WARN | RC25, RC26, RC15 |
| 364 | A malformed `Held` → treated as nil with a warning. W1 stops at `-v99` and KEEPs. W1 adopts an unowned candidate trio whose digests equal the staged temps. `verify -record` clears or refreshes `Held`; a restored zip with an extra entry is never deleted. A re-key maps `Superseded[].Key`. The Inventory size stays within the re-derived bound. Lands whole in S4 (RC32) | RC11, RC14, RC8, RC21, RC35, RC32 |

**Prove-fail plan per row** (mutate the fix, watch it go RED for the stated reason, revert;
recorded in the commit):

| Row | Mutation | Turns RED because |
|---|---|---|
| 350 | delete the regression branch in the verdict; or drop MERGE's improvement gate (RC3) | the zip is removed and the report shows a fillable gap; or the second pass writes a trio and supersedes the first |
| 351 | drop the body term from "page regressed" | the page is overwritten empty; and/or move the probe-time verdict after render, so a temp is created |
| 352 | classify a stream error as not held | the item is lost |
| 353 | skip the raw copy | a.pdf is absent from the new zip |
| 354 | skip the fixity/CRC check | the flipped entry is laundered into a fresh digest |
| 355 | let `unknown` always COMMIT (today's behaviour) | the zip is lost; or make UNVERIFIED fillable, so `Retried≠0` |
| 356 | drop KEEP stamping; or drop W4's record switch (RC1); or drop label-absent regression | `SweepGone` marks the message gone (KEEP or UNCHANGED); or the zip is deleted |
| 357 | rebuild `Fixity` from scratch without carry | `Fixity.EML` is nil and verify exits 2; the moved eml is deleted |
| 358 | move the deletion back before the save | the C5 stop leaves a record naming a deleted file |
| 359 | rename the zip before staging the html | missing or modified |
| 360 | remove G3; or remove G4; or remove G6 (RC12); or skip the lock re-check (RC13) | files are deleted while the row names them; the symlink is followed; the restored-manifest entry deletes the current B; a replaced lock still deletes |
| 361 | restore `deleteMessageFiles(rec.Path)` on every gone row | the F48 numbers `kept=0 pruned=3` |
| 362 | disable the digest short-circuit; or compare page bytes instead of `Held.Content` (RC5); or skip the earlier-unit inspection (RC2) | files are rewritten; a read-flag change moves the trio; a deleted zip stays missing after a full run |
| 363 | remove a line | the needle is missing |
| 364 | drop the existing-file exclusion; or drop the `-v99` cap (RC14); or drop adoption (RC8) | the planted file is overwritten; or clear `PagesStale` at the end of the run regardless, so stale pages survive; or the suffix exceeds the reservation; or an orphan pushes the message to `-v3` |

**Confirmed-open gap encoders (RC32).** Slice S1 lands a `t.Skip` encoder citing its
invariant **only** for the confirmed-open gaps: F46, N2, N3, F48 and §1.5–§1.7. Each runs
under the RC31 harness and must first show RED on today's code. Each slice that makes one
pass **removes the skip**, which turns it into a permanent guard. Rows that specify new
features (HEAL, MERGE, the wording rows) land with the slice that builds them, never as
skip encoders in S1.

**Lab.** None of these rows needs a lab. Real-world confirmations are **pending, with no
lab tier in this repo**: Thunderbird detach and delete and an Outlook offline-window shrink
on a real profile; Exchange keeping the immutable id across "Remove attachment" (§1.6). The
fake fixtures above reproduce the byte-level shapes. S5 builds the Thunderbird fixtures
from the client's documented detach and delete part headers (RC34).

---

## 10. Build slices and order

Each slice ships prove-fail → prove-pass, catalog first.

| Slice | Content | Depends on | Rows | Gate owed after build |
|---|---|---|---|---|
| **S0** | this design + the 10-lens pre-code review, and RC38's lens 3/6/7 addendum on revision 2 | — | — | the review itself (`docs/review-recapture-integrity-predesign.md`) and its addendum |
| **S1** | catalog: R28; R1/R2/R5/R13/S24 amendments (S39 only if OR2 bumps); S50/S51; MA-350..364 rows, all with operator-allocated ids (OR4); `t.Skip` encoders **only** for the confirmed-open gaps F46, N2, N3, F48 and §1.5–§1.7, under the RC31 harness; the §1.7 encoder's result goes to Q1 (RC33); no product code. **[ruling J1, J7, ESC-2] Interim S1:** the ids are allocated; S1 lands R28, S50 and S51 and the rows for the confirmed-open gaps with their encoders, each citing the existing invariant the defect breaks (F46 → R1; N2, N3 and §1.7 → R5; F48 → R13). No existing text or covering test changes: the R2, R13 and S24 amendments wait for ESC-2, R1's added sentence lands with S5, R5's with S4 only on ESC-3 (a), and S39 is unchanged (J2). The feature rows land with the slices that build them (RC32). The §1.7 encoder's result goes with ESC-3 | S0 + OR4 + OR5 (OR4 ruled, J1; OR5 is ESC-2, so the interim scope) | the encoders only | none beyond S0 (the operator accepts the amended guards, OR5; **[ruling ESC-2]**: escalated) |
| **S2** | `reindex` guard (F48, §4.9): the absent-only rule, archivefs-gated deletions, row paths enqueued, loud repair lines, N2 adoption, the `-vN` family on redaction (RC10, RC18, RC36). No format change. The archivefs gate lands once with g56 C2-2/P5 (§4.13). **[ruling ESC-2]** Until the owner answers, S2 builds no N2 adoption, no `-vN` family deletion and no row-path enqueue (§4.9's note): an N2 victim is pruned as today | S1 | MA-361 (its `-vN` and N2-victim twins as skip encoders, [ruling ESC-2]) | **adversarial pass** (destructive path; manifest and index untrusted; redaction semantics) |
| **S3** | the additive schema (state package only; `Load` accepts the new fields; no version write, RC29) + `archivefs.Inspect` with three outcomes, replacing every gate copy (verify and extract unchanged in behaviour, RC25). May be folded into S4 | S1 | none of its own | none of its own (no behaviour change); folded into S4's adversarial pass |
| **S4** | crash-safe write path and deferred deletion (N2, N3, §1.7): W1–W5 (including W4's record switch, RC1, and HEAL, RC2), the space precheck, adoption and `-v99` cap (RC8, RC14), `finishSuperseded` + G1–G7 with the lock re-check (RC12, RC13), `commit` returns the save error, start-of-run sweep (and `SweepOrphans` in `RunGraph`), `PagesStale`, `PathForKey`, Rebuild runs the sweep (RC20); **its sinks**: R-E per cause, R-H (HEAL), R-I (low disk), R-J (symlinked folder), R-K (no free name), the pending-removal WARNs, verify's `superseded` and `earlier-copy` kinds; any version change (OR2) together with this first new-field producer; MA-87 test amendment. **[ruling ESC-3, J2, ESC-2]** S4 does not start until the owner answers ESC-3 (Q1) and ESC-2; there is no version change (J2); the MA-87 test amendment is part of ESC-2 | S2, S3; OR1, OR2 (OR1 is ESC-3; OR2 ruled, J2); sequenced with g7 F53 (§4.13); RR-1 before code | MA-358, 359, 360, 362, 364 (whole) | **adversarial pass** (manifest-driven deletion, crash ordering, symlink gate, HEAL) |
| **S5** | never-degrade (F46, §1.5, §1.6): inventory recording with the Held binding, tiers T1–T3 with label + content matching, verdicts, the MERGE gate, merge with fresh headers and caps, eml rules, two-phase probe, KEEP stamping, legacy adopts, UNVERIFIED as a typed field, client placeholders; **its sinks**: report kinds, the Counts/HasIssues changes, R-A..R-D, R-F, R-G, the `merge_source_mismatch` WARN; MA-66 test amendment. **[ruling ESC-2]** The MA-66 test amendment is part of ESC-2 | S4 (MERGE and the eml carry write a new trio beside the earlier one and need the earlier zip to survive until the commit is durable); OR3, OR6 (both ruled: J6 and Q10, Q7 = DEMOTE); RR-1 before code | MA-350..357 | **adversarial pass** (untrusted earlier zip, page and eml; hostile names and labels) |
| **S6** | `RecaptureSummary` parity across CLI, GUI and desktop, R-L and the GUI label, README.md, README.txt, ux-contract X6 row | S5 | MA-363 | **friction review** (the walks of §7, including the inheritor cold walk and the `-vN` names) |
| **S7** **[ruling Q6]** | the one-time inventory backfill: reads each earlier zip's central directory into `Held` only where the zip matches its recorded `Fixity.Zip`, never re-baselining fixity (unlike `verify -record`); the central-directory cap of RC22 applies | S5 | none yet: its rows take overflow ids at its catalog-first PR (the next free one is MA-535, owner-rulings log §8.4) | **adversarial** addendum (untrusted earlier zips) |

**Order:** S1 → S2 → S3 → S4 → S5 → S6. S2 and S3 may proceed in parallel. S4 and S5 code
waits for RC38's addendum. **[ruling Q6, ESC-2, ESC-3]** S7 follows S5. S4..S7 wait for the
owner's answers to ESC-2 and ESC-3; S1 (interim), S2 (without its held cases) and S3 do
not. The cross-design order is the owner-rulings log's §9 (rows 7, 16, 17 and 33–36). S5's KEEP alone *could* ship before S4, but it would leave
N2/N3 and the §1.7 window open and would need rework for MERGE, so it follows S4. **No
slice merges a producer before its sink** (RC32): S4 carries the sinks of the `Superseded`
list it produces, and S5 carries the sinks of the kept, unverified and merged outcomes it
produces. No release ships a version write without the behaviour behind it (RC29).

---

## 11. Open questions that need an OWNER RULING

**All ruled or escalated** (docs/owner-rulings-2026-10-09.md §5, J1, J2, J6, J7 and §10).
Each question below keeps its text and carries its outcome.

- **Q1 (OR1, re-premised by RC33 and RC5) — How a changed re-capture keeps names and page
  state.** **ESCALATED (ESC-3)**, docs/owner-rulings-2026-10-09.md §10. Recommended answer:
  (a), asked once S1's §1.7 encoder has run. Interim: today's write path stays, S4 does not
  start, mime-fidelity's R27 lands without a file promise for repairs, and R5's
  write-beside sentence lands only on answer (a). Asked only after S1's §1.7 encoder has run under the RC31 harness. N2 needs only
  deferred deletion; a new name is needed only for a re-capture at an unchanged path
  (§1.7).
  - **The real triggers** (RC5): under option (a), files move only on a content change, a
    renderer release (once per record), `-raw` newly turned on, or a parse-scheme repair.
    Under byte comparison (revision 1), they would also move on every read, flag, importance
    or category change, and on every routine fill.
  - **The external costs of a move:** bookmarks and file:// links to a message page, saved
    reader `/files/` URLs and cited paths break, and rsync-style backups see a delete plus a
    create rather than an in-place change.
  - **(a) Move on content change only** (design default). The page keeps its capture-time
    read, flag and category state, so a full run **no longer refreshes that state** on
    pages. That is a product change you must accept: it matches S35/S36 and
    design-categories §5's capture-time snapshot.
  - **(b) Stable names with a per-record intent journal.** Before a batch's rename-overs,
    `{old record, new record}` is persisted per key; `verify` accepts either side's per-unit
    digests; the start-of-run reconcile picks, per unit, the side that matches disk. Cost:
    one extra manifest save per checkpoint batch, the batch's temps staged first (the same
    transient space as W1), reconcile code, and dual acceptance in verify. A bare "pending
    dual digest" is **not** equivalent to P5: after a kill, `Held` and the gap lists would
    describe the other side's files, and a later full run could COMMIT over a filled item.
    State is refreshed on every full run.
  - **(c) Replace in place,** and accept that a hard kill mid-run can raise a false
    "modified" alarm (§1.7) that tells the user to restore from a backup.

  Pre-fixity archives keep their names under every option (W3). **Blocks:** S4.
- **Q2 (OR2, made joint) — Manifest version.** **RULED = J2** (docs/owner-rulings-2026-10-09.md):
  (a), no bump, for every in-flight design; the manifest stays version 6. One program-level ruling covers this design,
  the fault-isolation, mime-fidelity and local-confidentiality designs, and the BodyFallback
  change. Three of them already keep version 6 so that a rollout rolls back by swapping the
  binary.
  - **(a) No bump** (design default, RC29). Every new field is additive. An older binary
    drops them in the safe direction (§6) and runs today's code while it runs.
  - **(b) Bump to 7,** shipped expand/contract (§6): release N reads 7 and writes 6; release
    N+1 writes 7. A rollback past release N then means restoring the pre-upgrade backup,
    which discards every capture since the upgrade, and a fleet MSI rollback fails at Load
    until each archive is restored.

  **Blocks:** S3 and S4.
- **Q3 — Size-changed same-name attachments.** When both the earlier and new bytes are
  non-empty but differ in length, should the new capture commit (counted as `changed`) or
  be kept like a regression? *Design default:* commit (NG3), because parser fixes
  legitimately change bytes. **RULED** (docs/owner-rulings-2026-10-09.md §5): commit,
  counted as `changed`; empty, absent and `.partial` items are still caught.
- **Q4 — Posture.** **RULED** (docs/owner-rulings-2026-10-09.md §5): kept content leaves
  `status` GREEN with an info line; the pending-removal WARNs fire per cause, and only after
  a completed run.
  - Should records holding kept content leave `status` GREEN with an info line (default),
    or raise a WARN even though there is no remedy?
  - Should the per-cause pending-removal WARNs (RC15) fire only after a completed run
    (default), or never?
- **Q5 (OR5, widened) — Accepting changed guards.** Do you accept amending the existing
  guards in §6: MA-66's and MA-87's covering tests, the R2/R5/R13/S24 wording (R5 scoped by
  RC28), R28's restated text (RC19), design-incremental-completeness B2, and, only if Q2
  rules for a bump, the MA-201/S39 version ceiling? The tests encode the current behaviour
  that F46 and N2 identify as the defects. They cannot be changed without your acceptance.
  **Blocks:** S1. **ESCALATED (ESC-2)** for the narrowing items, docs/owner-rulings-2026-10-09.md
  §10: R2 (one joint text with mime-fidelity), R13's new deletion cases and its N2
  adoption exception, MA-227's `-vN` clause, S24, the MA-66 and MA-87 test changes, and B2.
  The additions are accepted under J7 (R28, S50, S51, R1's kept-item sentence, MA-238 and
  MA-227's AlsoFiles reach, and R5's sentence only on ESC-3 (a)); S39 is unchanged (J2).
  Interim: S1 lands only the additions and encoders, S2 lands without its two held cases,
  S3 lands, and no existing text or test changes.
- **Q6 — Starvation on pre-v7 records.** A mixed re-capture (one item arrived, another
  regressed) on a pre-v7 record whose earlier zip entry is ambiguous (T2 duplicate names)
  or unknown (T3) is kept whole, so the newly available item waits (NG8). Is that
  acceptable? Or do you want a one-time inventory backfill (for example a
  `verify -record`-style pass that reads each earlier zip's central directory into `Held`)
  as a further slice? **RULED** (docs/owner-rulings-2026-10-09.md §5): the backfill, as
  slice S7 after S5, only where the zip matches its recorded `Fixity.Zip` and never
  re-baselining fixity. Until it lands the starvation is an open gap, not accepted.
- **Q7 (OR6, now with a quantified residual) — Legacy-scheme adopts.** On the live path, a
  legacy-scheme record whose staged bytes equal its recorded fixity is now adopted in place
  (RC17). For the rest:
  - **DEMOTE** (the default): commit, and keep the earlier file on disk, owned through
    AlsoFiles, counted and reported (R-F, status). It never risks dropping a distinct
    Message-ID reuse. The cost is a one-time duplication of about the pre-v5 Graph share of
    the archive on the first full run (§6).
  - **KEEP** like any other record: no duplication, and the served copy stays the better
    one, but a distinct reuse would then be silently uncaptured.

  **Blocks:** S5. **RULED** (docs/owner-rulings-2026-10-09.md §5): DEMOTE, because KEEP
  would silently leave a distinct Message-ID reuse uncaptured (R1).
- **Q8 (OR7, re-premised) — Orphans from a crash before the checkpoint.** These are the new
  files of a re-capture whose record never reached disk. RC8's adoption re-owns most of them
  on the next re-capture, and RC15 lists the rest as `earlier-copy`, naming the current
  copy. Is it acceptable that the rest are never removed automatically (NG5)? Or should
  `reindex` also remove an unowned `-vN` family sibling of a record's current stem outside
  redaction, at the cost of possibly deleting the newest bytes of a message? **Blocks:**
  RC15's remedy text (S4). **RULED** (docs/owner-rulings-2026-10-09.md §5): no. NG5
  stays; the rest are listed as `earlier-copy`, naming the current copy.
- **Q9 — No forced downgrade.** Confirm there is no flag to force a downgrade, so that
  removing content from the archive stays redaction-only (NG1). **RULED**
  (docs/owner-rulings-2026-10-09.md §5): confirmed.
- **Q10 (OR3, new) — The sibling in-place writers** (§4.13, RC28).
  - Does the mime-fidelity P11 repair route through §4.3 + W1–W5? That accepts `-vN` names
    on repair and amends that design's "a decoded subject never renames a file" and its
    rollback claim. Or is it a stated P5 exception that accepts the §1.7 window on repair?
  - Does the fault-isolation stub re-render become a stated P5 exception? (Recommended.)
  - Which design renames its "supersede" vocabulary? Recommendation: this design's
    user-facing terms become "pending removal" (`removal_pending`).

  These amend sibling designs, so this design cannot decide them. **Blocks:** S4 and S5,
  and the mime-fidelity repair slice. **RULED** (docs/owner-rulings-2026-10-09.md §5, J6):
  (1) the mime-fidelity repair routes through §4.3 and W1–W5 (J6), with the names ESC-3
  decides; (2) the stub re-render no longer exists (fault-isolation revision 2 supersedes
  metadata only), so there is no P5 exception; (3) this design's terms become "pending
  removal" (`removal_pending`).
- **Q11 (OR4, new) — Catalog ids.** Allocate the R and S ids across the in-flight designs
  (fault-isolation, recapture-integrity, mime-fidelity, local-confidentiality,
  extraction-workspace; the same ruling as the workspace review's OR2), and decide whether
  the catalog meta-test gains a duplicate-id check. That is a gate change, so it is the
  operator's. **Blocks:** S1. **RULED = J1** (docs/owner-rulings-2026-10-09.md §8): R28,
  S50 and S51 here; the duplicate-id check is a follow-up for the kit owner (log §11),
  since `assureblock/` is not edited here.

---

## 12. Honesty of claims (summary)

- **Proven once built** (tier U, CI, by the rows named in §2.2 and the revision 2 twins of
  §9):
  - P1 for tiers T1 (bound to the on-disk bytes) and T2 (same parse scheme), on the shared
    exporter commit;
  - P2 for `lost` keeps; P4, P7, P9, P10, P12;
  - P3 under its stated condition;
  - P5 and P6 for process death, under the RC31 snapshot harness (RC35 (d) lists P6 once,
    here);
  - P8 under its stated inputs;
  - P11's wording and counts.
- **Conditional, with the condition stated inline:**
  - P1 for tier T3 and for refused or unreadable earlier units (conservative: may refuse a
    no-worse capture);
  - P1, P5 and W1's "never writes over" for the mime-fidelity repair and the
    fault-isolation stub re-render: governed by §4.13 until OR3 (RC28). **[ruling Q10/OR3
    = J6]** The mime-fidelity repair is on the shared commit, so these hold for it as for any
    re-capture; the stub re-render no longer exists;
  - P2 for presumed keeps (the wording says the run could not confirm, RC26);
  - P3 (merge preconditions, including the improvement gate and the T1 body digest);
  - P5 and P6 for power loss (the filesystem must honour fsync);
  - P8 (render scheme, render options and, for pre-v7 records, mailbox state; zip
    determinism per build);
  - P12 (Full mode hashes each earlier unit once);
  - §1.1's realistic degradations and §7.1 (lab-pending, RC34);
  - §1.6 (Exchange keeping the immutable id across an attachment removal, lab-pending);
  - §5.2 (index rollback after power loss is a repairable residual);
  - §8 persona 3 (filesystems that enforce modes or ACLs, RC35 (b)).
- **Stated residuals:** a merged record has no original holding every item (P4, RC7); a
  T2 MERGE has no body-digest check against a same-envelope forgery (§8, RC35 (a));
  redaction during a stale-row window is loud and one step away, not silent (RC18); legacy
  adopts cost a one-time duplication unless OR6 rules for KEEP (RC17; **[ruling Q7]**:
  DEMOTE, so the duplication is paid once).
- **Aspirational until the S6 friction review:** that an inheritor reads `-vN` names and
  `source-lost` rows correctly from README.txt alone.
- **Not yet reproduced by a test:** §1.5, §1.6 and §1.7 are code-reading results, so S1's
  encoders must first show them RED on today's code, under the RC31 harness. Should one of
  them turn out green on today's code, its claim is withdrawn from §1, and this document is
  revised before S4/S5. §1.7's result is reported with Q1 (RC33), which is now ESC-3's
  question to the owner.
