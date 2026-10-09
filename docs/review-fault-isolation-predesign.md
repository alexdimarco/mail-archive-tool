# Pre-code design review — fault isolation for capture (10 lenses)

**Design reviewed:** `docs/design-fault-isolation.md`, revision 1 (2026-10-09), BUILD STATUS
"proposed".

**Procedure:** assurance-kit `process/design-review.md`, run as a workflow on 2026-10-09:
- **Lenses run:** all ten, one finder per lens (1 purpose/threat-model fit, 2 focus/proportionality,
  3 durability, 4 usability, 5 integration/seams, 6 security, 7 failure domains,
  8 migration/coexistence, 9 dependency and operational cost, 10 honesty of claims).
- **Skeptic:** a refutation-default skeptic per finding, with the code in hand. A finding is
  CONFIRMED when revision 1 *as written* permits the failure. It is REFUTED only when a quoted
  section already handles it.
- **Rescue:** a separate agent was given every finding the skeptic left at major or above and
  told to save the design. It named a dissolving change, or failed, for each one.

**Findings: 114 found · 111 confirmed · 3 refuted** (L1-12, L4-11, L5-10).
- Skeptic severity of the confirmed findings: **0 blocker · 46 major · 65 minor**. Two findings
  were raised as blockers and re-graded major by the skeptic: L3-1 (a rolling `-since` closes gaps)
  and L7-1 (a degraded capture replaces an archived record).
- The skeptic run returned no verdict for **L9-5**. The filer checked it against the code (see its
  row). It is the same mechanism as L7-6 and is counted as confirmed (minor).
- **Rescue:** 46 majors sent. **45 DISSOLVED**, **1 PARTIAL** (L10-3: the residual is now stated
  at the claim), **0 SURVIVE**. The rescue also surfaced:
  - one hidden blocker that no lens raised (Windows reparse points);
  - one process blocker (catalog ids proposed by five designs at once);
  - one root-cause simplification, which this review adopts as **FC7**.

**Verdict: GO_WITH_CONDITIONS.** No blocker survives the rescue, so no lens fails.
- **FC1–FC34** are must-fix-before-code. Each is folded into design revision 2 and marked in its
  text with the condition number.
- **OR1–OR10** need an **OWNER RULING**. Each would amend a locked decision (X1, the posture
  partition), or is a product or operator choice the owner has not made. They are not imposed
  here. The slices they gate are blocked until ruled (design §10, "Blocked on").
- Build may not start from revision 1.

**Owner decision honoured.** The relayed decision is a simple copy tool, "select the source and
choose the export destination to copy it to rather than any drag and drop". Revision 1 relayed
that correctly: it adds no interaction surface and no drag-and-drop. What it got wrong was the
engine. That flow is the extraction workspace, and it does not call `app.Run`/`app.RunGraph`.
FC20 corrects this. No condition adds an interaction surface. FC23 points the desktop card at
the existing Archive now action and adds no new control.

## Hidden blocker (rescue pass): Windows reparse points

`go.mod` is `go 1.26.0`, so `winsymlink=1` applies. The rescue checked this in the toolchain's
`os/types_windows.go` `mode()`. Any reparse tag other than SYMLINK, AF_UNIX or DEDUP is reported
as `ModeIrregular`. `DirEntry.Type()` and `File.Stat` on a handle carry the same tag. Two kinds of
entry are affected, and revision 1 handles neither.

- **Cloud and HSM placeholder files** (OneDrive Files-On-Demand, Dropbox online-only, Azure File
  Sync tiered files) are a silent loss. §4.5 and the §4.1 row "FIFO, socket, device or other
  irregular entry" send them to `not-a-message-file`. That class is **info**, so the file is never
  opened, never feeds `partial`, and never changes posture.
  - A maildir copied into a synced folder therefore exports none of those messages. The run exits
    0 and status stays GREEN.
  - Today `os.ReadFile` reads these files, because they hydrate on demand. The fix itself would
    introduce an R1 loss that turns loud into silent, which is the H3 class the design forbids.
  - `util.ReadRegularFile`'s "descriptor is regular" check refuses the same files on Windows,
    where `secureOpenFlags` is 0.
- **NTFS junctions** (`IO_REPARSE_TAG_MOUNT_POINT`) are reported with neither ModeDir nor
  ModeSymlink. "A symlinked one is recorded" keys on ModeSymlink. That leaves two outcomes:
  - An implementer who tests ModeSymlink follows a junctioned `.sbd` or `cur` through the
    `os.Stat`-based `isMaildir`/`walkSbd`. That is an out-of-store read, breaking R22.
  - An implementer who tests lstat IsDir drops the folder with no row.

  MA-322 is "Skipped on Windows", so CI never sees either outcome.

The rescue dissolved it with a modest change, which is now **FC1**: refuse by an explicit
special-file list, judge `ModeIrregular` with `os.Readlink`, never class as info an entry that
might hold mail, and add an OS-independent classifier test plus a Windows junction case.

## Process blocker (rescue pass): catalog ids

Five designs in flight propose R22, and S42 with it, each with a different meaning:
- this design (source-read containment);
- extraction-workspace (copy export);
- recapture-integrity (monotone re-capture);
- local-confidentiality (owner-only at rest);
- mime-fidelity (parser-independent identity).

The catalog ends at R21/S41. The covers-map gate only checks that a cited id exists, so a
`// covers: …, R22` marker would pass against whichever text lands first. The R and S ids
therefore have to be allocated once, by the operator, before any catalog-first edit. That is
**OR8**, and **FC34** blocks this design's catalog edits on it.

## Root-cause simplification adopted (FC7)

Eight findings share one root (L3-3, L3-4, L3-5, L5-4, L6-4, L6-5, L7-4, L7-5), and L2-4 and
L3-9 hang off it too. Revision 1 models an item with **nothing readable** as an archive record:
PST row cases 1–3, an envelope panic, a maildir EACCES. Each such record has a page, fixity, an
index row, a locator folded into identity, and a supersede path that rewrites the page in place.
That record is the source of:
- the stale fixity after supersede (verify RED);
- the trusted `Record.Path` on supersede;
- the kill window;
- no closure when the item leaves the source;
- unbounded stub growth from an interleaved table;
- the locator lost on rollback.

The rescue pointed out that the design's own rule already covers these items: "an item with no
record goes into the ledger". So does the rest of the machinery: the ledger is inert, capped and
closed by a completed walk, and §4.3 step 2 recovers a gap from its locator. **This review
adopts that as FC7.** Lens 2 favours the simpler mechanism that has the same property.

- Records remain only for partial reads (`unreadable:body`, `unreadable:attachments`) and for
  raw-preserving `unparsed` stubs.
- The rescue's per-finding changes for L3-4, L3-5, L5-4, L6-5 and L7-4 were sound. With FC7 they
  have no mechanism left to attach to. Their rows say "rescue DISSOLVED (by FC7)".
- What still applies, in ledger form, is FC8 (stable locators), FC9 (structural breakers) and
  FC16 (caps).
- One healing path survives for partial records. It is metadata-only: no file is written,
  rendered or deleted (FC6(4)).

## Conditions (must-fix-before-code; all folded into revision 2)

- **FC1 — Windows reparse points are judged, not assumed** (hidden blocker; L6-6, L10-6).
  - (a) Refuse only an explicit list, `ModeNamedPipe`/`ModeSocket`/`ModeDevice`/`ModeCharDevice`.
  - (b) On Windows, judge a `ModeIrregular` entry with `os.Readlink`. If it succeeds, apply the
    directory-link rule (`symlinked-folder-skipped`, terminal). If it fails, read the entry as a
    file: the descriptor check accepts an irregular handle that is not a directory.
  - (c) Descend only into a plain lstat directory.
  - (d) `info` only for proven non-mail (a Unix FIFO, socket or device); anything else refused is
    terminal.
  - (e) An OS-independent classifier test over synthetic FileModes, a Windows junction case that
    runs unprivileged, and a pending L row for a real OneDrive placeholder.
  - (f) P12 is labelled "proven on Unix; Windows conditional". The text states that the Windows
    open does not refuse links, so containment there rests on the pre-open judgment.
- **FC2 — Containment covers every local open** (L1-4, L6-2, L10-5).
  - One helper, `util.OpenRegularFile` (`O_NOFOLLOW`+`O_NONBLOCK`, then fstat-regular; FC1 on
    Windows), plus the lstat-type entry judgment. Both are used by `readMaildirDir`, `walkDir`
    (before `looksLikeMbox`), `looksLikeMbox`, `walkFile`, `source.Open`'s sniff,
    `IsMailStoreDir`/`Detect` and the Evolution walkers. Discovery therefore cannot hang before
    the lock is taken.
  - MA-322 gains an mbox-named FIFO at the store root (at discovery and during the walk, under a
    1 s guard). It also gains an `Inbox` symlink to an outside file that begins "From ", with the
    marker ABSENT from pages and `search.db`.
- **FC3 — Links are judged lexically before anything is resolved; the co-user persona is stated**
  (L6-7, L6-3).
  - `Readlink` comes first. Absolute, UNC and drive-qualified targets are rejected before any
    `EvalSymlinks`/`Lstat` of the target, and so is any target that leaves the root after a
    lexical `Clean`.
  - `os.Root` on the resolved store root is the recommended open path.
  - P12 and §8 state inline that R22 holds for a store no other local user can write. The
    co-writer (hard links, component swaps) is named as an out-of-scope persona.
- **FC4 — Sweep safety is decided per record, not per faulting folder** (L1-3, L10-3, L7-15,
  L7-11).
  - (a) Any folder-scope fault in a mailbox skips `SweepGone` for the whole mailbox that run.
  - (b) An item fault with a `ref.PhysID` TouchSeens every same-token record that
    `HasPhysID(ref.PhysID)`, but only when the listing confirmed immutable ids.
  - (c) An unknown-identity fault that matched no record excludes the token's PhysID-less
    `sha:`-identity records from the sweep. If immutable ids were not confirmed, it excludes every
    `sha:` record. It no longer excludes the folder.
  - (d) A ref skipped by the unwritable set counts as a fault.
  - (e) P7 names the L10-3 residual and drops "no new assumption is added".
  - (f) MA-312 gains three cases: a message moved into a folder whose listing then fails; a moved
    no-mid message whose fetch fails; Full-mode refs skipped by the unwritable set.
- **FC5 — A degraded capture never replaces an archived record** (L1-1, L7-1).
  - A gap-monotone KEEP guard in `Export`, on every source and in both modes, ahead of key
    placement (the `#fp` split, `placeByPhysID`) and ahead of any write. It fires on a capture
    that carries `ReadGaps`, or a Graph parse stub, when a same-token record for its identity
    (Graph: its PhysID) exists and does not already hold those labels. Then:
    - nothing is written or removed;
    - `MergeFields`/`TouchSeen`;
    - an info `refresh-failed` gap;
    - `Stats.Kept++`.
  - A capture with `ReadGaps` is never `#fp`-split. A Graph stub gets the listing Message-ID only
    when no such record exists.
  - §10 no longer says "Nothing here depends on g4/N2". It says S1a carries this guard in the
    exporter, so the sink is in place before the producers, and g4 subsumes it.
  - MA-318 gains twins for PST and Graph in Full mode: the record bytes stay equal to Fixity.
  - Stated residual: a no-Message-ID PST item gets a second, partial record, and the complete
    record is untouched.
- **FC6 — Partly-read records heal** (L7-2, L10-10).
  - (1) The `#fp` comparison is done over zero attachments when prev carries
    `unreadable:attachments`.
  - (2) An `unreadable:*` label "improves" when the new read no longer carries it.
  - (3) `Record.Locator` is set on every record that carries an `unreadable:*` label (only
    those), and any re-examine restores a missing one. That heals the rollback loss.
  - (4) Metadata-only supersede, for the no-Message-ID case: the partial record's labels are
    re-classed to Terminal `superseded` (info) and `RecoveredBy` is set. No file is touched.
    `Load` drops a stray `Locator` or `RecoveredBy`.
  - (5) MA-318 and MA-319 cover both orders and the no-Message-ID case.
- **FC7 — An item with nothing readable is a ledger gap, not a record** (rescue root-cause note;
  closes L3-4, L3-5, L5-4, L6-5, L7-4, L2-4; narrows L6-4).
  - These cases become item gaps (`pst-message-unreadable` terminal, `file-unreadable` fillable),
    delivered as faults:
    - a PST `GetMessage` failure;
    - an envelope panic;
    - the `safeConvertMessage` outer guard;
    - a maildir EACCES, EPERM or EIO.
  - No stub record, page, fixity entry or index row is minted for them. `model.Unreadable`, the
    `unreadable:message` label and the `contentHash` locator fold are removed, so no identity
    changes at all.
  - Recovery is §4.3 step 2 and closure is §4.3 step 3.
  - S4 is dissolved: the metadata-only supersede ships with the exporter in S1a, and the legacy
    re-class moves to S2a.
- **FC8 — Locators are stable for the life of what they name** (L3-3, L7-5, L6-9).
  - (1) `pst-nid:<store-uid>/<nid>`, where store-uid is PidTagRecordKey 0x0FF9. Nothing matches
    across differing uids.
  - (2) A store with no readable uid, every `-outlook` scratch PST and a rebuilt `.ost` aggregate
    their zero-content failures into one per-folder `pst-messages-unreadable` gap (a count and a
    bounded NID list), with a scratch-specific remedy.
  - (3) Row cases 1–2 always aggregate per folder.
  - (4) Post-parse local faults use `ident:<m.Identity()>`, not `mbox:<folder>#<n>`.
  - (5) Graph uses the immutable id when the listing confirmed it.
  - (6) S2a verifies that go-pst can read 0x0FF9. If it cannot, (2) applies to every PST.
  - §5's "wrong match impossible" is corrected.
- **FC9 — The PST row breakers count structure, not damage** (L7-7, L6-4, L3-7).
  - A per-store set of NIDs already yielded: a duplicate NID becomes `pst-row-invalid`.
  - A NID absent from the node B-tree becomes `pst-row-invalid`.
  - Only structural rows count toward the 100-row breaker. A real node that cannot be read is an
    FC7 gap (capped by FC16) and does not count.
  - The sub-folder table loop gets the same breaker.
  - MA-316 gains the interleaved 99-bad/1-good table, a 150-row damaged run mid-folder, and a
    corrupt hierarchy table.
- **FC10 — The Graph breaker has a rate and a time bound, and spares known poison** (L9-1, L3-6,
  L6-1, L10-8, L7-17, L3-14, L9-6).
  - It trips when transport faults are ≥50% of the last 50 attempts, or when they take more than
    30 min of a mailbox's time. The 25-consecutive rule stays as a fast path.
  - An item with an open gap of the same reason from an earlier run does not count.
  - A control request must fail before the breaker trips.
  - `Retry-After` is clamped to ≤60 s per sleep and ≤5 min per item until F17.
  - Two consecutive mailbox trips stop the run.
  - P3 and §7.3 state the wall-clock bound, and the text states that desktop Archive now has no
    mid-run cancel.
  - Every threshold is listed as fixed, and its message prints the value that fired.
  - The §8 claim stays conditional until three MA-313 cases are green: 1-in-10 success → trips;
    known-poison 504s → walked past; per-item timeouts with a healthy control → no trip.
- **FC11 — A token-endpoint failure is an auth failure** (L7-6, L9-5). `*oauth2.RetrieveError`,
  or the delegated sign-in-again error, is mailbox-fatal auth with today's wording and
  `auth_remedy`, and records no gap. MA-313 gets a mid-walk `invalid_client` case.
- **FC12 — Losing the archive volume is run-fatal** (L5-3, L7-3, L9-2).
  - On the run's first FolderError or ItemError, and every 50 after it: `Lstat` `-out` and the
    token directory, and call `lock.StillHeld()`. A failure is run-fatal: "the archive location
    `<out>` is unavailable — reconnect the drive and re-run", exit 1.
  - `IsRunFatal` adds EIO, ENXIO, ENODEV, ENOTCONN, ESTALE and EHOSTDOWN, plus Windows 3 (at the
    root), 21, 53, 55, 64, 1167 and 1295.
  - A folder's first FolderError counts toward the local breaker.
  - The local checkpoint and its StillHeld gate are driven by processed plus faults.
  - Remedies are keyed on errno.
  - MA-320 gains a twin that removes `-out` mid-walk and expects exit 1.
- **FC13 — A gap closes only when the run's scope covered it** (L3-1). Each item gap records
  `Received` when the date is known.
  - `CloseUnobserved` never closes a gap dated before `Since`.
  - In a `Since` run, export-stage gaps close only by recovery or re-observation.
  - MA-323 gains a gap that ages out of a rolling window and is retained and reported.
- **FC14 — Gaps carry an "attempted" state; nothing is stranded or falsely "retried"** (L1-10,
  L3-2, L7-8, L1-15, L6-11, L7-13).
  - `ObserveGap` updates Folder, Subject and Received.
  - `status` splits gaps by whether gap.LastRun equals the token's last runAt. Only attempted gaps
    drive the WARN and the "retried" wording, and only they can become stuck.
  - Not-attempted gaps read "never captured (last attempted `<date>`; not in recent runs)": a
    note by default, WARN if OR3 so rules. They stay in the report, the banner and the JSON.
  - `Load` clamps times to ≤ now and drops tokens that are absent from Stores.
  - `CloseUnobserved` tests `LastRun != runAt`.
  - P11, N3 and §7.4 state that a retired source's gaps are permanent and listed (Type I).
- **FC15 — Runs counts runs** (L3-10, L7-12). `Runs++` happens only when `LastRun != runAt`.
  `Skipped` resets at the first observation in a run. MA-323 gets cases.
- **FC16 — Lifetime bounds on every scope** (L3-7, L3-8, L10-11, L6-11).
  - Per token, over the archive's life: ≤1000 live item entries and ≤1000 folder-scope entries.
    The overflow entry counts the rest, and its class is the most severe it absorbed.
  - Info entries have their own cap of 100.
  - `Load` caps the total.
  - P13 states the lifetime bound.
- **FC17 — Unknown ledger values fail closed** (L3-12). A known Class with an unknown Reason is
  kept. An unknown Class becomes terminal. The Reason string is kept, bounded and stripped.
- **FC18 — Stuck is WARN, never RED** (L1-9).
  - `capture_gaps_stuck` is WARN with its own code, listed first. RED stays reserved for run
    failure and corruption (`health.go:163-262`).
  - The remedy text for a GraphCode gap is honest: "Microsoft 365 will not export this item; save
    a copy from Outlook on the web. The row stays listed while the item remains in the mailbox."
  - The threshold and its unit are **OR5**.
- **FC19 — Every `app.Run` caller maps partial** (L1-6, L4-5, L5-5).
  - On `ErrPartial` the `mailarchive-gui` wizard shows the summary with the Not-captured block,
    then calls rememberArchive and cleanupOutlookScratch, opens the archive and offers a schedule.
  - The `-job` reclaim gate (`main.go:181`) also accepts `ErrPartial`.
  - The mapping is a pure function, asserted in MA-325.
- **FC20 — The owner's copy flow is a separate consumer** (L1-5, L4-1, L5-2, L7-9, L10-1).
  - The Inputs bullet is rewritten: the flow is the extraction workspace (OW4). It has its own
    engine and `CopyOutcome` and never calls `app.Run`/`RunGraph`.
  - The copy-flow bullet in §4.10 and the copy-flow branch of §7.1 are removed.
  - `app.GapRemedy` is exported for copyexport.
  - Destination-side reasons name the archive location, never "could not be read".
  - S5's hand-off becomes the SG coordination (FC21).
  - No interaction surface is added.
- **FC21 — One shared Graph primitive, SG** (L2-1, L5-1, L9-3, L6-8). SG lands before S3 and
  before the extraction design's E1. It provides:
  - `StatusError{Code, GraphCode, Text}`, keeping today's `Error()` text (MA-402);
  - one classifier;
  - one streaming `MIMETo(w, maxBytes, idle, overall)` that capture also calls, with a size cap
    no lower than Exchange's maximum message size plus MIME overhead (200 MiB, to be confirmed
    in S3; over the cap → a `fetch-too-large` terminal gap, never truncated);
  - one nextLink guard (host/path pin, repeat set, page cap) in Folders, Messages, FolderTree and
    MessagePage.

  The breaker thresholds and the 404 class stay per consumer, each with a written justification.
  §10 names the seam. Ownership is **OR9**.
- **FC22 — `status` lists the gaps, with their source** (L4-3, L10-9, L4-4, L6-12).
  - `health.Summary` lists up to 20 gaps (source, folder, subject, date, reason, remedy), then
    "+N more", and JSON gets a bounded `capture_gaps.items[]`.
  - Every gap row carries the store's source id (mailbox address or input path), taken from the
    reverse of `manifest.Stores`. That covers the report, the summary, status and the card.
  - Dashboard GET pages show subjects only once the dashboard read guard (F3/E0) lands. Until
    then the line is the same without the subject, and §8 states the dependency.
- **FC23 — Wording that is true for every actor** (L4-2, L4-6, L4-9, L4-12, L4-13, L4-8, L1-2).
  - (1) The card says "retried whenever you archive again (Archive now)", naming the existing
    action. It says "automatically" only when a schedule is installed. The Outlook-on-the-web
    remedy shows at first sight, for the deadline case.
  - (2) The sidecar, the notification and the card lead with "new this run: K". `status -json`
    exposes `last_run.gaps_new`, and the monitoring advice keys on it.
  - (3) Items are counted apart from folders, and "of M" is dropped.
  - (4) The no-mid stuck remedy explains how to resume deletion tracking.
  - (5) A README line gives the class vocabulary.
  - (6) Remedies are keyed by reason, source kind and errno class (EACCES, EIO, PST, the
    `-outlook` scratch).
  - (7) A local item gap says "could not be read from the source this run (a copy archived by an
    earlier run is kept)". This is the stated L1-2 limitation.
- **FC24 — Derived views see the ledger** (L4-10, L7-14, L3-13).
  - `writeReport` returns early only when there are no record issues **and** the ledger is empty.
  - Every `pages.Generate` caller (run, graph, reindex, rebuild) passes the count.
  - MA-327 gains a ledger-only fixture and a reindex/rebuild case.
- **FC25 — `unparsed` is terminal for one parser generation, and bounded** (L1-14, L3-11, L8-6,
  L10-11, L6-10).
  - Each stub carries a parser generation and a raw-bytes digest.
  - After an upgrade, a Graph stub is re-fetched once and heals in place through FC6(2). A local
    stub is superseded metadata-only by the record whose digest matches.
  - The page and the index carry the headers plus the first 64 KiB of the raw message. `Raw` is
    kept whole.
- **FC26 — Stub identity goes through g3's hook; one gap vocabulary** (L8-3, L5-12).
  - The raw-bytes stub identity and the Graph listing-mid are expressed in g3's `replayV1`
    panicked branch.
  - `ReadGaps unreadable:attachments` means the whole attachment table failed.
    `Attachment.GapKind` covers per-part gaps.
  - `GapClass`/`probeImproves` are the one owner of the labels, and they list g3's labels.
  - The merge order is recorded in §10.
- **FC27 — Legacy stubs** (L8-2, L5-7). The local legacy re-class moves into S2a. A Graph legacy
  stub (PhysID or Received match) is adopted on the first upgraded run: TouchSeen and re-class,
  so it is never swept gone. MA-323 gets cases.
- **FC28 — Outlook export losses are recorded** (L7-10).
  - `CreatePSTs` returns the folders and accounts it skipped. Each becomes an
    `outlook-export-skipped` folder gap (fillable).
  - An account skipped as "already a .pst" is recorded as info.
  - The mapping gets a unit test, and a Windows+Outlook L row is pending.
- **FC29 — Reader faults are delivered as they happen** (L7-16). A callback during the walk
  delivers each fault. A Walk that ends in its outer recover returns the accumulated faults with
  the store error. Any list bound is stated, and dropped faults are counted.
- **FC30 — The copied go-pst loops are owned** (L9-4).
  - §4.4 lists every upstream gate: HasSubFolders, `MessageCount > 0`, search-folder type and
    `IdentifierTypeNormalMessage`.
  - go-pst is pinned at v6.0.2, and the reader package owns the copied loops.
  - A differential test runs against go-pst's own iterators on the healthy fixture.
  - N9 records the Printf amplification.
- **FC31 — Claims carry their conditions inline** (L1-7, L10-7, L1-11, L8-5, L1-13, L5-11, L2-5,
  L3-9, L8-1, L10-12, L8-7).
  - (1) The amended R1, R10 and R17 texts carry their conditions, as the rescue worded them.
  - (2) P3 reads "unless go-pst or the OS hangs (N7/N8) or the process is killed".
  - (3) P14 is limited to records, keys, identities and per-message files. README, the root stamp
    and linked folders are named exceptions.
  - (4) X1 is reworded by mechanism: items are named up to the cap, cancel stays 0, and the copy
    export's partition differs.
  - (5) The last-run version bump is dropped.
  - (6) §6 states the full rollback loss and the corrected OQ1 residual.
- **FC32 — Every "proven" clause names the case that asserts it** (L10-4, L5-9). §4.11 becomes a
  property → mechanism → MA case matrix, and the missing cases are added:
  - MA-313: 403, EDQUOT/EROFS, the Windows codes, SafeToken;
  - MA-314: the cap firing;
  - Graph listing-page isolation, maildir non-root ReadDir, mbox open and mid-stream, the
    Evolution walkers;
  - the `-since` rule and the footer rule.

  A clause still without a case is relabelled conditional.
- **FC33 — Slices ship independently of open rulings** (L2-2, L2-3, L5-8, L5-9).
  - S1a covers the sinks and surfaces. A partial run maps to today's failure (exit 1) until
    OR1/OR2 are ruled. S1b covers exit 3 and `RunPartial` after the rulings.
  - S2a is the PST work plus the legacy re-class; S2b is mbox/maildir/Evolution containment,
    after OR6.
  - S4 is dissolved.
  - The build table gets a "Blocked on" column.
  - MA-320 and MA-324 are split into a (local) and b (Graph).
- **FC34 — Catalog ids are allocated once** (L1-8, L3-15, L8-4, L10-2). No catalog-first edit
  happens before OR8. R22, S42, S43 and S44 are marked "proposed, id pending OR8", and §10
  serialises catalog edits.

## Owner rulings (not imposed; each blocks the named slice)

- **OR1 — Last-run status for a partial run (OQ1).** The choice is between two options:
  - `"partial"`: `last_run.status` widens a documented value set (README.md:609-611), so it needs
    `JSONVersion` 3 and a README update (L5-6). An older `status` shows **GREEN** for a
    ledger-only partial, which is wider than revision 1 said (L8-7).
  - `"failed"` plus a `partial` flag: older readers fail closed (RED), and JSON stays v2
    additive.

  Blocks S1b.
- **OR2 — The X1 amendment (OQ7).** X1 in `docs/ux-contract.md` is locked. Adopting capture-verb
  exit `3` amends it, using the FC31(4) text. Blocks S1b. Until it is ruled, S1a reports a
  partial run as exit 1.
- **OR3 — Persistent known gaps (OQ2, with L4-6 and L1-10).** Choose one:
  - honest-always: every run exits 3, the sidecar is kept, the nightly notification goes out;
  - exit 3 and notification only when `GapsNew > 0` or the stuck set grows, with a known-only
    state carried by `status`.

  Also decide whether not-attempted gaps are a note or a WARN. Blocks S1b's sidecar and notify
  behaviour.
- **OR4 — Self-closing "vanished" faults (OQ3).** A first sighting is either partial or
  report-only. Blocks S1b's `partial` definition.
- **OR5 — Stuck threshold (OQ4, with L4-7).** Rule on both the number and the unit: runs, or
  elapsed time. The review suggests Runs ≥ 3 AND now − FirstRun ≥ max(72 h, two schedule
  periods), with at most one observation per day. FC18 sets the posture at WARN; making it RED
  would amend the posture partition, which is also the owner's call. Blocks S1a's stuck rule.
- **OR6 — Directory links in local stores (OQ5, with L8-5).** There are three options:
  - never follow (revision 1);
  - an opt-in;
  - follow in-store links, guarded by a visited (dev, inode) set.

  Also decide the migration for archives that capture through links today. Revision 1's remedy
  mints a new token and re-exports into a second tree. Blocks S2b.
- **OR7 — Graph 403 on `$value` (OQ6).** It is either mailbox-fatal or a per-item gap. Blocks S3.
- **OR8 — Catalog id allocation across the five designs (operator).** Blocks every catalog-first
  edit, from S1a on.
- **OR9 — Who owns SG, and what the extraction design changes on its side (operator).** On its
  side: its `StatusError` gains `GraphCode`, and its 404 class and breaker thresholds are
  reconciled or justified. Blocks S3 and the extraction design's E1.
- **OR10 — The L10-3 residual.** Either accept the residual as stated in P7 (the default), or
  exclude PhysID-less records from the sweep on any unmatched-identity fault. The second option
  defers gone-detection on pre-v6 archives while such a fault persists. Blocks nothing; the
  default applies unless the owner rules otherwise.

## Operability backlog (Type III; ships with the feature)

- Operator-tunable breaker and timeout thresholds, as bounded job/GraphOptions settings (L9-6).
  FC10 makes the values visible in messages first.
- A per-store watchdog for go-pst internal hangs (N7).
- A mid-run cancel for desktop Archive now (L9-1). FC10 states that none exists.
- An acknowledge verb for known terminal gaps (N3, OQ2/OR3).

## Findings

The Verdict column gives the skeptic's verdict and severity, and the rescue outcome for majors.
The Condition column names what closes the finding. A refuted finding carries its refuting
quote.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| L1-1 | 1 | §4.4 inward recovers; §10 "Nothing here depends on g4/N2" | A PST body or attachment read failing on a later Full run keeps the envelope on the archived record's key and overwrites the only copy of the body | CONFIRMED (major) · rescue DISSOLVED | Fingerprint excludes bodies (`model/message.go:154`); Full mode skips the incremental guard (`exporter.go:235`); same stem, so `writeFileAtomicDigest` overwrites the html (PST has no `.eml`). Correction: the zip is not deleted, the `#fp` split re-keys a no-attachment read (`exporter.go:220-231`; that is L7-2) | FC5 |
| L1-2 | 1 | §4.2 Locator stub-only; banner. OMISSION: local refresh-failed | A local item archived intact and later unreadable gets a second placeholder, a false "not in this archive" banner, WARN and exit 3 every night | CONFIRMED (minor) | `Locator`/`byLocator` stub-only (§4.2); refresh-failed is Graph-only (§4.8.2 step 2). Loud, no content lost; only the banner and the duplicate are false | FC7, FC23(7) |
| L1-3 | 1 | P7; §4.8.3; §4.8.4 | A message moved Inbox→Projects whose new folder's listing then fails is swept gone from its stale Folder | CONFIRMED (major) · rescue DISSOLVED | `SweepGone` tests `walked.Has(r.Folder)`, the last-known folder (`internal/state/manifest.go:956-977`); today `runGraphMailbox` returns before `walked.Mark` on any Messages error (`app/graph.go:647`); MA-312 has no move case | FC4 |
| L1-4 | 1 | P3, P12; §4.5; MA-322 | The mbox layout and discovery open with plain `os.Open`: a FIFO hangs the run, an out-of-store link is archived | CONFIRMED (major) · rescue DISSOLVED | `walkDir` → `looksLikeMbox` → `os.Open` (`mbox.go:57-93`, `source.go:120-129`); `walkFile` (`mbox.go:162-166`); `IsMailStoreDir` (`source.go:84`); §4.5 scoped to `readMaildirDir`; MA-322 maildir-only | FC2 |
| L1-5 | 1 | Inputs; §4.10 last bullet; §7.1; S5 | The owner's copy flow is a separate engine; `ErrPartial` never reaches it and "retried next time" is false there | CONFIRMED (major) · rescue DISSOLVED | extraction-workspace OW4 (`:18-20`); §1.2 RunGraph "is welded to the archive … cannot be pointed at a hand-over folder" (`:50-56`); own CopyOutcome and classes (`:697-746`). The owner decision itself (no drag-and-drop, no new surface) is relayed correctly | FC20 |
| L1-6 | 1 | OMISSION: GUI wizard; P9; §4.9; S1/S5 | The GUI wizard shows a partial run as a failure dialog and skips remember, scratch reclaim, open and schedule | CONFIRMED (major) · rescue DISSOLVED | `cmd/mailarchive-gui/main.go:1110-1117` returns before `rememberArchive` (`:1118`), `cleanupOutlookScratch` (`:1121-1123`), the summary (`:1127`) and `offerSchedule`; `-job` reclaims only when `runErr == nil` (`:181`) | FC19 |
| L1-7 | 1 | §2.1 R1/R10/R17; P3 | The amended invariant texts state unconditionally what the design concedes is conditional | CONFIRMED (major) · rescue DISSOLVED | R10 "never hangs" vs N7/N8; "the message, else the folder" vs §4.5 store root and §4.8.4 Folders; R1 "durably" vs P4's F53 condition; R17 "fillable" vs `export-panic` terminal | FC31(1)(2) |
| L1-8 | 1 | §2.1 R22, S42–S44 | Sibling designs propose the same R22/S42 with different meanings | CONFIRMED (minor) | extraction-workspace `:221/:244`, mime-fidelity `:136/:815/:827`, recapture-integrity `:190/:217-218`, local-confidentiality `:7/:153`; catalog ends at R21/S41 | FC34, OR8 |
| L1-9 | 1 | P11; §4.3 step 4; §4.10 stuck RED | One email Exchange cannot convert holds the archive RED forever, with a remedy that does not clear it, ranked above terminal gaps | CONFIRMED (major) · rescue DISSOLVED | RED reserved for run failure and corruption (`health.go:163-262`); 500 ErrorMimeContentConversionFailed is fillable (§4.1) and deterministic per item; §7.1 calls the remedy Type I | FC18, OR5 |
| L1-10 | 1 | P11; N3; P13 | Gaps of a departed mailbox or discarded PST never close; "retried every run" stays false; P13's bound fails | CONFIRMED (major) · rescue DISSOLVED | §4.3 step 3 closes only after a completed walk of the gap's folder; unwalked gaps "are retained"; the cap is per store per run (§4.3 step 5) | FC14, FC16 |
| L1-11 | 1 | P14; MA-329 | "Same file bytes as today" is false for README.txt, the root page stamp and linked folders | CONFIRMED (minor) | README.txt rewritten from a constant each run (`readme.go:100-101`) and §4.10 changes it; §4.10 stamps a `data-` count; OQ5 drops linked folders | FC31(3) |
| L1-12 | 1 | §0; P8 | P8 files cancel (exit 0) under "a failure (exit 1)", and 401/403 stop only the mailbox | REFUTED | P8: "Each of these ends the **mailbox or source walk** as a failure (exit 1)" and "parent context cancel (the handled interrupt still exits 0, X1)"; §4.8.5 "Other mailboxes in the run continue"; §4.9 precedence `1` > `3` > `0` | — |
| L1-13 | 1 | §2.1 X1 vs §4.1 partial | "Each missing item is named in the report" over-claims | CONFIRMED (minor) | §4.1 excludes not-downloaded and source-empty gaps (exit 0); P13 overflow items are counted, not named | FC31(4) |
| L1-14 | 1 | §4.1 `unparsed`; §4.8.2 Graph stub | An unparsed stub never benefits from a parser fix; a local one is left beside a duplicate | CONFIRMED (minor) | §4.1 "same bytes, same result"; the Graph stub keyed `mid:<listing id>` is skipped forever; mime-fidelity changes the parser | FC25 |
| L1-15 | 1 | §5 ledger row; §8 archive-writer | A planted row with a made-up folder or foreign token is never cleared and can show stuck | CONFIRMED (minor) | closure needs a walk of the gap's folder and token (§4.3 step 3); `Load` drops only non-`SafeToken` tokens | FC14 |
| L2-1 | 2 | §4.8.1, §4.8.5–§4.8.7; §10 seams | The extraction design builds the same Graph primitives in the same files, with different semantics | CONFIRMED (major) · rescue DISSOLVED | extraction §4.5 (`:432-443`): MIMETo 120 s idle/200 MiB, `StatusError{Code,Text}` keeping today's `Error()` (MA-402), nextLink pin in all loops; §4.13 (`:705-716`): 404 terminal, 10-error breaker; two StatusError types cannot coexist in package graph | FC21, OR9 |
| L2-2 | 2 | §10 S1; S2/S3 "Depends on S1" | The data-loss fixes wait on the exit-code rulings bundled into S1 | CONFIRMED (minor) | S1 bundles `ErrPartial`, exit 3, `RunPartial` and the X1 text with the sinks while OQ1/OQ7 are open | FC33 |
| L2-3 | 2 | §10 S2; OQ5 | The PST crash fixes wait on the symlink policy and its ruling | CONFIRMED (minor) | S2 bundles the PST loops with the symlink policy, `util.ReadRegularFile` and R22 while OQ5 is open | FC33 |
| L2-4 | 2 | S4; P6; MA-319 | The supersede machinery ships on an unverified NID-keeping precondition | CONFIRMED (minor) | P6 states the condition; MA-319 restores bytes rather than running scanpst; NIDs are not stable for `-outlook` (L3-3) | FC7, FC6(4) |
| L2-5 | 2 | §4.2 last-run Version 1 → 2 | The bump implies a compatibility gate that does not exist | CONFIRMED (minor) | `WriteLastRun` hard-sets `Version = 1` (`lastrun.go:81`); `ReadLastRun` never reads it (`lastrun.go:166-188`) | FC31(5) |
| L3-1 | 3 | §4.3 step 3; §4.8.2 step 1; R1 amended | A rolling `-since` window closes unhealed gaps as "no longer in the source" | CONFIRMED (major; raised as blocker, re-graded) · rescue DISSOLVED | Export returns `SkippedDate` before any write for a record-less item (`exporter.go:277-279`); §4.8.2 step 1 records no gap; `util.ParseSince` is relative (`dates.go:41-52`) and re-evaluated each run (`mailarchive-gui main.go:160-166`). Re-graded: rolling-since jobs only; loud for the whole window | FC13 |
| L3-2 | 3 | §4.3 step 3; `ObserveGap`; P13; §4.10 | Renamed folders, toggled-off folders and dropped mailboxes strand gaps forever under false wording | CONFIRMED (major) · rescue DISSOLVED | `ObserveGap` never updates Folder (§4.2); folderKey is the display path (`app/graph.go:483`); IncludeDeleted/IncludeJunk toggles (`capture.go:137`); `CaptureGapCounts` has no LastRun filter | FC14, FC8(5) |
| L3-3 | 3 | §4.2 Locator; §4.7.3; §5 "wrong match impossible"; P13 | `-outlook` rebuilds the PST at a fixed path every run: NIDs move, supersede false-matches, stubs grow without bound | CONFIRMED (major) · rescue DISSOLVED | `outlookcom_windows.go:220-223` removes and rebuilds at a constant path; the token stays, NIDs are reallocated; the stub identity folds the NID | FC8, FC7 |
| L3-4 | 3 | §4.7.3 (the only exit for a stub) | A stub whose item leaves the source never closes; its WARN persists after the remedy is carried out | CONFIRMED (major) · rescue DISSOLVED (by FC7) | P11 closure covers the ledger only; `unreadable_messages` counts records; a deleted or moved maildir stub file never re-yields its locator | FC7 |
| L3-5 | 3 | §4.7.3 supersede | Supersede rewrites the stub html without refreshing Fixity, so verify reports it modified (RED) forever | CONFIRMED (major) · rescue DISSOLVED (by FC7) | Fixity is recorded only inside Export for the record written (`exporter.go:333-336`); verify compares Fixity.HTML; Assess RED `not_attested` (`health.go:233-234`) | FC7, FC6(4) |
| L3-6 | 3 | §4.8.1 rules 4–5; §4.8.5 | 25 adjacent, deterministically stalling items wedge the mailbox at the same place every run | CONFIRMED (minor) | 502/504/timeouts count as transport; the listing order is fixed. Re-graded: speculative trigger, loud, §5 concedes "too eager" | FC10 |
| L3-7 | 3 | §4.4 sub-folders; P13 (item-scope cap only) | A corrupt hierarchy table yields one uncapped folder-scope entry per bad row, re-observed every run | CONFIRMED (major) · rescue DISSOLVED | §4.4 "One bad row → `folder-unreadable` for that row only", no breaker; the §4.3 step 5 cap counts item scope only | FC9, FC16 |
| L3-8 | 3 | §4.3 step 5; §4.1 `partial` | Info entries consume the cap; the overflow entry has no class, so real faults past the cap may not make the run partial | CONFIRMED (minor) | `not-a-message-file` is item-scope info; the `overflow` scope has no class | FC16 |
| L3-9 | 3 | §6 rollback | An older binary also drops `Record.Locator`/`RecoveredBy`, so supersede never fires after a rollback round trip | CONFIRMED (minor) | Record (`manifest.go:68`) has no `loc`/`recovered_by`; a still-broken re-yield ends StillIncomplete with no rewrite (`exporter.go:271-274`) | FC6(3), FC31(6) |
| L3-10 | 3 | §4.2 `Runs` | Runs counts calls, not runs; "consecutive" is not enforced | CONFIRMED (minor) | `ObserveGap` "sets `Runs++` … on an existing one" on every call | FC15 |
| L3-11 | 3 | §4.1 `unparsed`; §4.8.2 | Parser fixes in flight never heal existing unparsed stubs | CONFIRMED (minor) | as L1-14; a local raw stub keys on the raw text, so a later parse mints a second record | FC25 |
| L3-12 | 3 | §4.2 Load sanitisation | Unknown Reason/Class map to info: a newer build's fillable gaps are downgraded and stay so after a save | CONFIRMED (minor) | §4.2 "maps an unknown `Reason` or `Class` to `info`" | FC17 |
| L3-13 | 3 | §4.10 banner | reindex and rebuild regenerate the root page without the count and drop the banner | CONFIRMED (minor) | `pages.Generate` callers: `run.go:259`, `graph.go:325`, `reindex.go:166`, `reindex.go:361`; §4.10 specifies only the run | FC24 |
| L3-14 | 3 | P3; §4.8.6 | A trickling endpoint can hold one mailbox about 50 h | CONFIRMED (minor) | 2 min idle under a 2 h cap per item; 25 consecutive faults to trip | FC10, FC31(2) |
| L3-15 | 3 | §2.1 | R22/S42 collide with the extraction design | CONFIRMED (minor) | as L1-8 | FC34, OR8 |
| L4-1 | 4 | Inputs; §4.10; §7.1; S5 | Copy-flow users are promised an automatic retry they never get; the two designs word and class the same fault differently | CONFIRMED (major) · rescue DISSOLVED | as L1-5; extraction `:741-746` (retry only via "Export failed messages again"); 404 class and wording differ | FC20, FC23 |
| L4-2 | 4 | §7.1; §4.10 card; §1 core use; OQ3 | "They'll be retried next time" tells a user archiving before account removal to do nothing | CONFIRMED (major) · rescue DISSOLVED | §7.1 "0 actions if transient"; the OWA remedy appears only at stuck; OQ3 says a one-shot run must not claim completeness; Type II | FC23(1) |
| L4-3 | 4 | §7.1 step 3; §4.10 status | The per-item list behind "2 clicks to know" is never specified | CONFIRMED (major) · rescue DISSOLVED | §4.10 status emits counts; the desktop view renders `health.Summary` (`maintenance.go:103-113`); subjects live only in the TSV | FC22 |
| L4-4 | 4 | §4.10 report (path empty); summary lines | Gap rows do not say which mailbox or file they belong to | CONFIRMED (major) · rescue DISSOLVED | report columns (`report.go:51-55`); gap `path` empty; `graph:<id>`/`pst-nid:<n>` carry no mailbox | FC22 |
| L4-5 | 4 | OMISSION: GUI wizard result on `ErrPartial` | The main home-user path shows a partial run as failure | CONFIRMED (major) · rescue DISSOLVED | as L1-6 (`main.go:1110-1137`; §4.9 maps only `-job`) | FC19 |
| L4-6 | 4 | OQ2; §4.9; §6; `GapsNew` unused | Under honest-always, a known loss and a new loss look the same every night | CONFIRMED (minor) | `GapsNew` recorded (§4.2) and used nowhere; OQ2 raises the trade-off | FC23(2), OR3 |
| L4-7 | 4 | P11; §4.3 step 4; OQ4 | Stuck counts runs, so escalation depends on cadence and clicks | CONFIRMED (minor) | `Runs ≥ 3` (§4.3 step 4); hourly schedules exist (`schedule.go:32`); manual Archive now | OR5 |
| L4-8 | 4 | §4.10 `unreadable_messages` remedy | Maildir and `-outlook` placeholders get the scanpst remedy | CONFIRMED (minor) | §4.1 maildir EACCES/EIO share the label; the `-outlook` scratch is rebuilt and reclaimed (§4.9) | FC23(6), FC8(2) |
| L4-9 | 4 | §4.10 card counts; GapSummary | A folder gap counts as 1; "of M" has no source | CONFIRMED (minor) | GapSummary counts entries; `Skipped` is set only for folder-unwritable and the row breaker | FC23(3) |
| L4-10 | 4 | §4.10 report | A ledger-only archive gets no report: `writeReport` deletes it when no record has issues | CONFIRMED (minor) | `report.go:43-47` early return; MA-327 has no ledger-only fixture | FC24 |
| L4-11 | 4 | §4.2 `Error`; §4.10 status line | `health.Summary` prints "partial: partial: …" | REFUTED | §4.10: "`partial` last-run handling: a `Last run: … → partial (N not captured)` line", which replaces the generic `": "+Error` path (`health.go:631-636`). Only an older binary doubles it, cosmetically (input to OR1) | — |
| L4-12 | 4 | §4.8.3; `capture_gaps_stuck` text | The no-mid stuck remedy never says how to resume deletion tracking | CONFIRMED (minor) | the OWA copy leaves the item in place, so the §4.8.3 suspension persists | FC23(4) |
| L4-13 | 4 | §7.4; README bullets | A cold inheritor gets class words with no meaning | CONFIRMED (minor) | the README additions omit fillable/terminal/info | FC23(5) |
| L5-1 | 5 | §4.8.1, §4.8.5–§4.8.7; §2.1; §10 | Parallel Graph primitives and colliding ids with the extraction design | CONFIRMED (major) · rescue DISSOLVED | as L2-1, plus the R22/S42 collision (the rescue counts five designs, not two) | FC21, FC34, OR8, OR9 |
| L5-2 | 5 | Inputs; §4.10 bullet 3; §7.1; S5 | The `ErrPartial` rendering obligation targets an interface the copy flow never receives | CONFIRMED (major) · rescue DISSOLVED | as L1-5 | FC20 |
| L5-3 | 5 | P8; §4.6; §4.8.2 | A removed archive drive ends a run of fewer than 50 folders as exit 3 "partial" | CONFIRMED (major) · rescue DISSOLVED | the §4.6 `IsRunFatal` set lacks volume-loss errnos; skipped-by-set is not counted; checkpoint and `StillHeld` gated on Exported (`run.go:208-217`, `531-533`); extraction classes "export root gone" run-fatal (`:714`) | FC12 |
| L5-4 | 5 | §4.7.3; §5 supersede row; MA-319 | Supersede leaves Fixity stale and has no persisted input for "same bytes" | CONFIRMED (major) · rescue DISSOLVED (by FC7) | as L3-5; `ReadError` lives on `model.Message`, not `state.Record` (§4.2) | FC7, FC6(4) |
| L5-5 | 5 | §4.9; §7; §10 S1/S5 | The GUI wizard and the `-job` scratch reclaim are missing from the caller inventory | CONFIRMED (major) · rescue DISSOLVED | as L1-6 (`main.go:1094-1118`, `:181`) | FC19 |
| L5-6 | 5 | §4.10 "JSONVersion stays 2" | Adding "partial" widens a documented field's values; the additive-key rule does not cover that | CONFIRMED (minor) | README.md:609-611 documents `ok · failed · cancelled · running`; `health.go:762-768` covers new keys only | OR1, FC31(6) |
| L5-7 | 5 | §10 S2 vs S4; §6 legacy stubs | Legacy collapsed stubs sit orphaned between S2 and S4 | CONFIRMED (minor) | S2 changes stub identities; the re-class lands in S4 | FC27 |
| L5-8 | 5 | §10 build table vs §11 | Slices hard-code answers to open owner questions | CONFIRMED (minor) | no gating column; `design-review.md` step 4 makes amending the locked X1 an owner ruling | FC33, OR1, OR2, OR6, OR7 |
| L5-9 | 5 | §10 MA-320/MA-324 split | The covers-map counts MA-320/324 covered by their local halves while the Graph halves are untested | CONFIRMED (minor) | the covers-map counts any citing test (CLAUDE.md) | FC33 |
| L5-10 | 5 | §4.8.1 case 4 | Exhausted throttle retries return an untyped error that falls through to item class | REFUTED | §4.8.1 case 4 classes "429 or 503 (after `get`'s retries)" as `fetch-transport`, and MA-313 asserts "25 consecutive 503s → breaker error"; a 503 run exhausts `get`'s loop (`graph.go:546-554`), so an untyped fall-through fails MA-313 | — |
| L5-11 | 5 | §2.1 X1 amended | The X1 text overstates: overflow items are not named, and cancel differs across verbs | CONFIRMED (minor) | P13 overflow is counted only; extract's `3` is partial per X1, but the copy export is not `extract` | FC31(4) |
| L5-12 | 5 | §10 g3 seam "textual only" | The two designs' gap labels and stub semantics conflict semantically | CONFIRMED (minor) | mime-fidelity §4.11 (`:636-639`) puts F33's stub identity in `replayV1`'s panic branch; its MA-349 "never a stub" vs MA-318 | FC26 |
| L6-1 | 6 | §4.8.5; §8 hostile-sender row | 25 adjacent per-item 504s or timeouts trip the breaker every run | CONFIRMED (minor) | as L3-6 | FC10 |
| L6-2 | 6 | §4.5; P3; P12; §8; MA-322 | The Thunderbird mbox layout and discovery are outside containment; discovery can hang before the lock | CONFIRMED (major) · rescue DISSOLVED | as L1-4; `IsMailStoreDir` runs at discovery (`source.go:60-87`) before `beginRun` (`run.go:100-121`) | FC2 |
| L6-3 | 6 | P12; §8 racer row | A co-user who can write the store plants hard links or swaps components; resolve-then-open protects only the last component | CONFIRMED (minor) | a hard link inside the root is a regular file whose target is itself; macOS has no protected_hardlinks. Re-graded: needs a co-writable store | FC3 |
| L6-4 | 6 | §4.4 row breaker; P13; §8 | An interleaved 99-bad/1-good table mints unbounded stub records | CONFIRMED (major) · rescue DISSOLVED | the breaker needs 100 consecutive failures; stubs are records outside the ledger cap | FC7, FC9, FC16 |
| L6-5 | 6 | §4.7.3; §4.2 Load | A planted `loc`+`path` redirects supersede's re-render outside `-out` | CONFIRMED (major) · rescue DISSOLVED (by FC7) | supersede rewrites at the stored `Record.Path`; the INS2-1 guard (`exporter.go:282-287`) covers computed paths only; verify and reindex already treat Path as untrusted | FC7, FC6(4) |
| L6-6 | 6 | §4.5 lstat rules; MA-322 skipped on Windows | NTFS junctions are `ModeIrregular`, not `ModeSymlink`, and `os.Stat` descent follows them | CONFIRMED (minor) | go 1.26 `winsymlink` (`go.mod:3`); `walkSbd`/`isMaildir` use `os.Stat` (`mbox.go:99,108`) | FC1 |
| L6-7 | 6 | §4.5 symlink rule | Resolving before checking touches attacker-chosen targets (UNC → NTLM leak; autofs) | CONFIRMED (minor) | §4.5 "Symlink → `EvalSymlinks`. If the target lies inside the resolved root…" | FC3 |
| L6-8 | 6 | §4.8.7; P3; §8 endpoint row | `Client.Folders` follows nextLink with no guard and no host pin | CONFIRMED (minor) | `graph.go:205-236` | FC21 |
| L6-9 | 6 | §4.3; §4.6 `mbox:<folder>#<n>`; §4.8.2 `graph:<ref.ID>` | Ordinal and move-dependent locators give false recoveries and reset Runs | CONFIRMED (minor) | recovery and closure match on the locator (§4.3 step 2); the ordinal shifts on compaction | FC8(4)(5) |
| L6-10 | 6 | §4.5 raw-preserving stub | The whole raw message (base64 included) is rendered and full-text indexed | CONFIRMED (minor) | §4.5 `PlainBody: string(data)` | FC25 |
| L6-11 | 6 | §4.3 step 3; §4.2 Load; N3 | Planted or retired-token gaps stay stuck; the entry count at Load is unbounded | CONFIRMED (minor) | `CloseUnobserved` runs only for walked tokens; Load bounds strings, not count | FC14, FC16 |
| L6-12 | 6 | §4.10 desktop; §8 | Gap subjects on dashboard GET pages are readable by a co-user until the F3 session fix | CONFIRMED (minor) | Status & health is a GET (`maintenance.go:103-113`); `/api/activity` GET (`capture.go:111`). Correction: `printSummary` is CLI-only (`cmd/mailarchive/main.go:1359`) | FC22 |
| L7-1 | 7 | §4.4; §4.8.2 listing mid; §10 g4 | A degraded capture is routed onto an archived record's key and replaces it (PST Full mode; Graph Full mode via placeByPhysID) | CONFIRMED (major; raised as blocker, re-graded) · rescue DISSOLVED | PST as L1-1; Graph: listing mid on the stub + PhysID (`app/graph.go:420`) + `placeByPhysID` adopt (`exporter.go:688-693`). Correction: the stem differs, so the old html/zip are orphaned rather than overwritten. Re-graded: needs Full mode plus a new failure | FC5 |
| L7-2 | 7 | §4.1 `unreadable:attachments`; P6; §4.7.3 | An attachment-table failure never heals and mints a phantom duplicate | CONFIRMED (major) · rescue DISSOLVED | Fingerprint folds attachment names (`model/message.go:154-160`); the `#fp` split (`exporter.go:220-231`) runs before the probe; Locator stub-only | FC6 |
| L7-3 | 7 | §4.6; §4.1 folder-unwritable; P8; §7.5 | Losing the archive volume is not run-fatal | CONFIRMED (major) · rescue DISSOLVED | as L5-3 | FC12 |
| L7-4 | 7 | §4.7.3; §5 crash and domain rows; §10 g4 | Supersede's fixity and kill window depend on g4 | CONFIRMED (major) · rescue DISSOLVED (by FC7) | as L3-5; the rename→save window is recapture-integrity's §1.7 class | FC7, FC5 |
| L7-5 | 7 | P6; §4.7.3; §5; P13 | `pst-nid` locators are not stable across `-outlook` and `.ost` rebuilds | CONFIRMED (major) · rescue DISSOLVED | as L3-3; positional `pst-row` locators churn too | FC8 |
| L7-6 | 7 | §4.8.1 rules 2/4; P8; §7.5 | A mid-run token refresh failure is classed transport and blamed on the network | CONFIRMED (minor) | oauth2 wraps the transport (`graph.go:129-130`); the error comes from `c.hc.Do` with no response (`graph.go:543-545`). Re-graded: uncommon mid-run; "(last: …)" still carries AADSTS | FC11 |
| L7-7 | 7 | §4.4 row breaker; P1; P13 | A run of real damage trips the breaker and abandons the healthy rest of the folder on every run | CONFIRMED (major) · rescue DISSOLVED | the breaker counts case 3, a valid NID whose `GetMessage` fails | FC9 |
| L7-8 | 7 | §4.3 step 3; P11; §4.10; §7.1/§7.3 | No lifecycle state for gaps whose mailbox, source or folder has left the run set | CONFIRMED (major) · rescue DISSOLVED | as L1-10/L3-2; an ok run removes the sidecar (`lastrun.go:142-143`) while status stays RED | FC14 |
| L7-9 | 7 | Inputs; §4.10; §7.1 | The copy flow does not drive `app.Run`, and two divergent Graph primitive sets would land | CONFIRMED (major) · rescue DISSOLVED | as L1-5/L2-1 | FC20, FC21 |
| L7-10 | 7 | P2; X1 "0 complete"; §3 | Folder and account losses in the Outlook COM export are only logged; the run exits 0, which now means "complete" | CONFIRMED (major) · rescue DISSOLVED | `outlookcom_windows.go:284-285` logs "skipped folder"; `:222-230` logs a failed account | FC28 |
| L7-11 | 7 | §4.8.2 FolderError; §4.8.3; MA-320 | In Full mode, refs skipped by the unwritable set let `SweepGone` mark that folder's records gone | CONFIRMED (minor) | §4.8.3 is silent on unwritable-set refs; Full mode has no fast-path stamp | FC4(d) |
| L7-12 | 7 | §4.2 `ObserveGap`; §4.3 | Runs reaches 3 within one run; Skipped accumulates across runs | CONFIRMED (minor) | as L3-10; `Skipped` is never reset | FC15 |
| L7-13 | 7 | §4.2 Load; §4.3 step 3; §5 | Future-dated or foreign-token rows never close | CONFIRMED (minor) | `CloseUnobserved` closes only `LastRun < runAt`; Load neither clamps nor checks Stores | FC14 |
| L7-14 | 7 | §4.10 report | as L4-10 | CONFIRMED (minor) | `report.go:43-47` | FC24 |
| L7-15 | 7 | P7; §4.8.3 | One persistent no-mid poison suspends its folder's gone-detection although PhysID would identify it | CONFIRMED (minor) | `HasPhysID`/`AltPhysIDs` exist and are used (`exporter.go:691`) | FC4(b) |
| L7-16 | 7 | §4.4 fault delivery | Faults reach the ledger only when Walk finishes normally; the list bound is unstated | CONFIRMED (minor) | §4.4 "after it finishes walking"; no size stated | FC29 |
| L7-17 | 7 | §4.8.5 (per mailbox); §7.3 | An outage costs about 25 timeouts per mailbox across every mailbox | CONFIRMED (minor) | `ResponseHeaderTimeout` 60 s (`graph.go:137-140`); the breaker is per mailbox | FC10 |
| L8-1 | 8 | §6 rollback, mixed versions | Old binaries drop Locator/RecoveredBy, and alternating binaries wipe the ledger | CONFIRMED (minor) | as L3-9; the old report labels `unreadable:*` "empty-attachment" (`report.go:80-88`) | FC6(3), FC31(6) |
| L8-2 | 8 | §6 "Graph legacy stubs … left as they are"; P7 | Graph legacy stubs are swept gone on the first upgraded run | CONFIRMED (minor) | today's Graph stub identity is sha over subject + Received (`mbox.go:201-206`), stamped by the skip (`exporter.go:248-266`); after §4.8.2 the identity changes | FC27 |
| L8-3 | 8 | §10 g3 "textual only"; §4.5; §4.8.2; §4.2 | The two designs define stub identity in incompatible places | CONFIRMED (major) · rescue DISSOLVED | mime-fidelity §4.2 (`:311-321`) freezes identity terms; §4.11 (`:636-639`) requires F33's fix in `replayV1`'s panic branch | FC26 |
| L8-4 | 8 | §2.1 | Five designs propose R22 | CONFIRMED (minor) | as L1-8 | FC34, OR8 |
| L8-5 | 8 | P14; §6; OQ5 | Archives that capture through directory links change on upgrade; the remedy duplicates mail under a new token | CONFIRMED (minor) | `walkSbd`/`isMaildir` follow links today (`mbox.go:99,108`); `PathSourceID` (`run.go:504`) mints a new token | OR6, FC31(3) |
| L8-6 | 8 | §4.1 `unparsed` | as L1-14 | CONFIRMED (minor) | mime-fidelity (`:311-319`) assumes a later build repairs | FC25 |
| L8-7 | 8 | §6 mixed versions; OQ1 | An older status shows GREEN for a ledger-only partial; the JSON status values widen at v2 | CONFIRMED (minor) | the older Assess (`health.go:203-222`) has no partial case and ignores `capture_gaps` | FC31(6), OR1 |
| L9-1 | 9 | §4.8.5; §4.8.6; P3; §7.3 | A degraded Graph has no wall-clock bound; the 2 h cap replaces the 10-min bound on Retry-After sleeps | CONFIRMED (major) · rescue DISSOLVED | breaker consecutive-only; `retryAfter` accepts any integer (`graph.go:563-569`); today the 10-min ctx bounds the sleeps (`graph.go:494`, `556-560`); desktop runs on `context.Background()` (`capture.go:44,142`); no ExecutionTimeLimit | FC10 |
| L9-2 | 9 | P8; §4.6; §7.5 | A dead archive volume ends as partial with a "rename it" remedy | CONFIRMED (major) · rescue DISSOLVED | as L5-3; `gapRemedy` keyed by reason only | FC12, FC23(6) |
| L9-3 | 9 | §4.8.1, §4.8.5–§4.8.7; §10 | The shared graph-client primitive has no owner | CONFIRMED (major) · rescue DISSOLVED | as L2-1 | FC21, OR9 |
| L9-4 | 9 | §4.4 copied go-pst loops | The copied loops have no owner or pin, and §4.4 omits the HasSubFolders gate | CONFIRMED (minor) | go-pst v6.0.2 `GetSubFolders` opens with the HasSubFolders gate (`pkg/folder.go:100-105`); MA-329 would catch the omission | FC30 |
| L9-5 | 9 | §4.8.1 rules 2/4; OQ6 | A token-endpoint failure becomes 25 transport gaps and a network remedy | CONFIRMED (minor) — **filer-verified**: the skeptic run returned no verdict for this id | `deviceSource.Token` returns the oauth2 error unchanged (`internal/graph/delegated.go:190-203`); `get` returns `c.hc.Do`'s error with no response (`graph.go:543-545`); same mechanism as L7-6 | FC11 |
| L9-6 | 9 | thresholds throughout | Fixed constants, with no operator setting and no telemetry | CONFIRMED (minor) | §4.8.6 "The test hooks scale all three"; no operator setting is stated | FC10; operability backlog |
| L10-1 | 10 | Inputs; §4.10; §7.1; S5 | as L1-5 | CONFIRMED (major) · rescue DISSOLVED | extraction `:50-56`, `:343-345`, `:697-746` | FC20 |
| L10-2 | 10 | §2.1; §9 Covers | as L1-8 | CONFIRMED (minor) | parallel catalog-first inserts after R21 conflict at merge | FC34, OR8 |
| L10-3 | 10 | P7; §4.8.3 | "No false gone" fails for moved no-mid messages; "no new assumption" hides that the failure direction changed | CONFIRMED (major) · rescue **PARTIAL** | `SweepGone` decides by the stored Folder (`manifest.go:968-970`); a no-mid move is recorded only after a successful download; a Message-ID mismatch used to cost a download and now costs a phantom gone. Residual: a pre-v6, PhysID-less `mid:` record with mismatched ids whose fetch fails in a marked folder | FC4(e), OR10 |
| L10-4 | 10 | §2 "Proven" labels; §9; §4.8.2; §4.8.3 | Several mechanisms labelled proven have no asserting MA case | CONFIRMED (major) · rescue DISSOLVED | MA-313 lacks 403/EDQUOT/EROFS/Windows/SafeToken; MA-320 injects only ENOSPC; MA-314 never fires the cap; no row for §4.8.4, maildir ReadDir, mbox open/mid-stream, Evolution, the `-since` rule or the footer rule | FC32 |
| L10-5 | 10 | P3; P12; §4.5 | as L1-4 | CONFIRMED (major) · rescue DISSOLVED | as L1-4 | FC2 |
| L10-6 | 10 | P12 "Proven: MA-322"; §8 | P12 has no proof on Windows, where junctions and cloud placeholders behave differently | CONFIRMED (minor) | MA-322 skipped on Windows; `secureOpenFlags` is 0 there (`util/securefile_windows.go`); a junction is `ModeIrregular` | FC1 |
| L10-7 | 10 | P3 | "A run always finishes" is unconditional | CONFIRMED (minor) | N7, N8 and the §5 kill row concede hangs and kills | FC31(2) |
| L10-8 | 10 | §4.8.5; §8 | as L6-1 | CONFIRMED (minor) | as L3-6 | FC10 |
| L10-9 | 10 | §7.1 step 3 | as L4-3 | CONFIRMED (major) · rescue DISSOLVED | as L4-3 | FC22 |
| L10-10 | 10 | P6; §4.1 probes | A no-Message-ID partial PST record never heals; its healthy re-read becomes a second record | CONFIRMED (minor) | `contentHash` folds bodies and `len(Attachments)` (`model/message.go:202-213`); Locator stub-only | FC6(3)(4) |
| L10-11 | 10 | P13; §4.1 `unparsed` | No steady-state ledger bound; "terminal" holds only within one parser version | CONFIRMED (minor) | per-run cap plus retained gaps; as L1-14 | FC16, FC25 |
| L10-12 | 10 | §6 rollback | as L3-9 | CONFIRMED (minor) | Record (`manifest.go:68`) | FC6(3), FC31(6) |

## Mechanical check

`docs/design-fault-isolation.md` revision 2 has BUILD STATUS "approved with conditions, not
built" and cites this review, which exists on disk. The repo has no meta-test for this rule yet
(`assureblock/` holds only the block and covers-map tests), so the check is by convention, as for
the earlier gates. Each slice flips its own status when it lands and cites its gate artifacts: a
friction review for S1a/S1b, an adversarial pass for S2a, S2b and S3.
