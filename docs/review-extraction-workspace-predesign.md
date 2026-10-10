# Pre-code design review — Extraction workspace

**Design reviewed:** `docs/design-extraction-workspace.md`, revision 1 (2026-10-09).
The conditions below are folded into revision 2 (2026-10-09) of the same file.

**Procedure:** the 10-lens battery of `assurance-kit/process/design-review.md`, run as a
workflow on 2026-10-09: finder → skeptic → rescue. Each lens had its own finder (lenses
1–10). One refutation-default skeptic checked every finding against the design text and
the code. A separate rescue reviewer got every finding the skeptic left at major severity
and was told to save the design with modest mechanism changes, or to say plainly that a
finding survives.

**Lenses run:** all ten. 1 purpose / threat-model fit · 2 focus / proportionality ·
3 durability · 4 usability · 5 integration / seams · 6 security / adversarial ·
7 failure domains / recoverability · 8 migration / coexistence · 9 dependency &
operational cost · 10 honesty of claims.

**Findings: 118 found · 111 confirmed · 7 refuted** (L1-11, L1-15, L2-4, L2-5, L4-5,
L4-8, L5-9). After the skeptic: 0 blocker, 39 major, 72 minor. L5-1, the only finding a
finder rated blocker, was downgraded to major. The rescue handled all 39 majors: 37
DISSOLVED by a named change, 2 PARTIAL (L4-1, L9-4), 0 SURVIVES. The rescue also found the
hidden blocker below, which no lens named.

**Verdict: GO_WITH_CONDITIONS.** Conditions **XC1..XC40** must be fixed before code. They
are folded into revision 2, and the build must not start from revision 1. No lens fails,
because no blocker survives the rescue. Code is gated in two ways:
- **XC40:** lenses 3, 6 and 7 re-run on revision 2 before any E2 code. XC1 moves where the
  app keeps its truth, which is a material change under design-review.md.
- **Owner rulings OR1..OR9:** each blocks the slice it names. They are listed separately
  because each one would amend a decision locked elsewhere, or a product choice the owner
  has not made.

The design is large: 1,209 lines, eight slices, and 47 proposed MA rows. Its slices are
sound once XC28 moves rows to the slices that build them.

## Hidden blocker (rescue pass) — the destination folder is the app's system of record

Revision 1 keeps its plan only in memory (§4.7 step 5) and calls the export directory "the
truth" (P19). But §5.2 concedes that directory is neither trusted nor reliable: it may be
removable, full, shared, cloud-synced or FAT, with "another program writing there". The
design still uses it as the system of record for four things:

- **Recovery.** Retry reads the run's `export-manifest.json` from the destination (§4.13).
- **Integrity.** The spool `.mailexport-entries.tmp` inside the export root feeds the
  manifest, `SHA256SUMS.txt` and the fingerprint (§4.10). Derived HTML is rendered from the
  `.eml` read back off the destination (§4.11).
- **Status.** Reconciliation promotes a start-only run from whatever manifest it finds
  there (§4.16).
- **Identity.** "Open folder" trusts the recorded path (§4.16).

That one choice is the common root of about twenty confirmed findings:
- L1-2, L3-4, L3-5, L3-9, L3-12, L6-3, L6-4, L6-5, L7-2, L7-5 and L7-8;
- the reconcile hang (L1-14, L3-11, L7-6, L9-4);
- the unrecoverable kill (L3-3, L7-3).

Adopted one by one, the lenses' fixes contradict each other. L3-4 puts a plan file inside
the export root. L3-5, L7-2, L6-3, L6-5 and L7-8 need recovery state outside it, and
between them they propose four different side stores.

**Resolution (XC1–XC5).** One per-run 0600 **run record** in the per-user, local,
non-roaming state directory:
- Its header holds the source identity and the export root's identity.
- It then holds the whole plan, then one fsynced line per finished message with that
  message's file hashes.
- Last comes a `sealed` line with the fingerprint, computed from the in-memory
  `SHA256SUMS.txt` bytes and written before the marker is removed.

The manifest, the sums and the fingerprint are built from the run record. Retry and
"Finish as partial" read only the run record. Reconciliation reads only the run record and
the journal, and never parses the destination.

P19 inverts: the run record is the app's truth, and the export directory is the
recipient's. P4 gains a second admission path, and R22 gains "except to finish this app's
own interrupted run" (XC2). This is material, so XC40 re-runs lenses 3, 6 and 7 on
revision 2.

Two concrete defects underneath it:

- **H1a — the journal names a directory before it exists.** §5.1 W2 (journal `start`,
  carrying `dest_dir`) runs before W3 (the exclusive mkdir, which picks `-2…-99` on
  EEXIST). L3-13(c) and L7-7 saw this as an accidental mis-binding (minor). The rescue
  showed it is an attack:
  1. Root names follow the clock to the second, so a co-writer on a shared destination can
     pre-create the next few `MailExport-<timestamp>` names.
  2. The real run then lands in `-2`, while the journal, "Open folder" and reconciliation
     all point at the planted directory.
  3. Reconciling a killed run reads the planted manifest and can promote the run to
     "Complete". That fails toward "looks complete".

  Fix: do the mkdir first, then write the start record with the real path and the root's
  identity (XC3). Add a random suffix to the root name (XC32), and make reconciliation
  downgrade-only (XC4).
- **H1b — R-j is unreachable.** A pick carries a folder id (about 120 characters) and a
  message id (about 70–150), so it is roughly 200–300 bytes of JSON. The 1 MiB body cap
  (§4.2) therefore returns a bare 413 at about 3,500–5,000 picks, well below R-j's 10,000.
  L5-10 raised the same arithmetic. The rescue adds the fix: the server issues short row
  handles, and picks reference those, which also hardens P4 (XC17).

## Conditions (must-fix-before-code; folded into revision 2)

- **XC1 — The run record is the app's system of record (P19 inverted).** Each run gets
  `<state dir>/exports/<runId>.jsonl`, mode 0600, in the per-user, local, non-roaming state
  directory: Windows `%LOCALAPPDATA%\MailArchive`, macOS
  `~/Library/Application Support/MailArchive`, Linux `$XDG_STATE_HOME/mailarchive`.
  - **Header:** format version, run id, source identity (signer UPN or archive path),
    destination, export root path and identity (dev+inode, or volume serial + file ID),
    and options.
  - **Plan:** the whole previewed plan (id, folder path, subject, from, received, size
    hint). The header and plan are fsynced before W4.
  - **Per message:** one fsynced line per finished message (status, class, and each file
    with role, SHA-256 and size).
  - **Seal:** a `sealed` line with the fingerprint, computed from the in-memory
    `SHA256SUMS.txt` bytes, fsynced after W9 and before W10.

  The destination spool is dropped. The manifest, the sums and the fingerprint are built
  from the run record. A derived file is rendered from the read-back `.eml` only after the
  bytes' hash equals the recorded one. On a mismatch no derived file is made, and the
  message is listed as "the original changed on the destination after it was written".

  §8.4 states the residual: a co-writer can still alter files after they are written. The
  recipient's check detects that; nothing prevents it. *Closes:* hidden blocker, L6-3,
  L3-4/L3-5/L7-2 (the retry-input half).
- **XC2 — Retry and "Finish as partial" read only the run record.** P4 gains a second
  admission path: "an id is also accepted if it is in this app's own run record for the run
  being retried, under the same signer (R-l otherwise)".
  - **Which ids:** `failed` = the retryable failures. `rest` = the plan minus the messages
    already done.
  - **How they are resolved:** metadata comes from the record, so there are no per-id
    GETs and no seen-set. Retries sit outside the 10,000 pick cap and inside the 200,000
    run cap. An id that has since vanished is the existing 404 class at run time. The
    destination manifest is never an admission source.
  - **Interrupted runs:** they get "Finish as partial". The app finalizes its own export
    root, which must have the recorded identity, from the record alone. Then "Export the
    remaining messages" is offered. R22 and P6 gain "except to finish this app's own
    interrupted run".
  - **Operator walk:** the §7.2 "laptop closed" cell is re-scored. Re-work counts as time
    spent on the remaining messages only. The Type III "resume from spool" item is retired.
  - **Tests:** MA-437 adds four cases: retry after a restart; retry after Clear sign-in and
    a re-sign-in as the same UPN; a tampered or deleted destination manifest; and Finish as
    partial on a killed run.

  *Closes:* L1-2, L3-4, L3-12, L7-8, L2-2 (retry half).
- **XC3 — The journal is bound, durable and versioned.**
  - (a) The `start` record is written after the exclusive mkdir, with the real directory
    name and the root's identity. The run id goes into the marker and the manifest.
    Reconciliation acts on a directory only if its run id matches the journal's.
  - (b) If a journal or run-record write fails, the start is refused with a named message
    (R-d).
  - (c) Reuse `truncateTornTail` (the `state.OpenHistory` pattern). One mutex covers
    append and compaction.
  - (d) The journal and run records live per machine, in the local non-roaming state
    directory. Every line carries a host id, and reconciliation reads only this host's
    lines.
  - (e) Readers refuse a newer format version with a clear message and tolerate additive
    fields. Compaction copies raw lines verbatim, including lines with an unknown `v`.

  §6's "no reader of exports exists" is corrected. *Closes:* H1a, L3-13, L7-7, L8-7,
  L3-14, L8-6.
- **XC4 — Reconciliation runs off the request path and only ever downgrades.**
  - **When it runs:** once per run per launch, in a background goroutine. Each stat is
    bounded to 5 s and the result is cached. A terminal synthetic record is then appended,
    so no run is probed again.
  - **Endpoints:** `/api/exports/progress` serves only the in-memory run slot.
    `/api/exports/history` serves the cached journal view.
  - **Where status comes from:** only the journal and the run record. The destination is
    probed only for presence, the marker and identity. It is never parsed for status, ids
    or hashes, and no fingerprint is ever computed from disk.
  - **Outcomes:** marker absent, a sealed record and a matching identity → "complete
    (recovered)", shown with the sealed fingerprint. No sealed record → "changed after the
    export — run Check this export".
  - **Docs and tests:** §8.2 gains an "export directory read-back" row. A new MA row
    injects a blocking stat: progress must still return within 1 s, and Cancel must still
    work.

  *Closes:* L1-14, L3-11, L7-6, L9-4(a), L6-5, L7-14, L3-10 (the recomputed-fingerprint
  half).
- **XC5 — Status is honest at every kill point; P14 is restated with its conditions.**
  - **Conditional status.** README's status line and the manifest's status are
    conditional in their own text: "valid only while no `_EXPORT-NOT-FINISHED.txt` is
    present", and `"valid_if_absent"`.
  - **One reconcile rule.** A marker present is never "complete". §5.1 W8 and W9 are
    corrected to match.
  - **Check and finish.** A run with a marker and a sealed record gets "Check and finish":
    the app verifies the sums against the sealed fingerprint, then removes its own marker.
  - **P14 restated.** It now reads "any export directory that contains a message file
    carries the marker or is complete", and W3 and W4 are asserted separately. Its inline
    conditions: it holds across process death; across power loss only on filesystems that
    order metadata; on FAT32/exFAT, the recipient's check is the backstop. MA-429 proves
    ordering, not durability. Removable media get "Eject before unplugging".
  - **Sync cost.** Files are fsynced at write. Directory syncs are batched and done once
    before W9. U tests get a sync seam. MA-443 measures sync cost and includes a USB-stick
    destination.
  - **Tests.** MA-429 asserts the README text and the activity label at every kill point.

  *Closes:* L3-9, L7-5, L1-10, L10-4, L3-15, L9-9.
- **XC6 — The session runs on a process-lifetime context, and shutdown is real.**
  - **Context.** `DashboardHandler` receives main's root context. The Graph session and
    every preview or run are built on it, never on `r.Context()`. Per-call work uses
    derived contexts.
  - **Drain.** On shutdown, `dash.Drain(15 s)` cancels the run, waits for finalization and
    then exits.
  - **Windows.** A hidden top-level window handles `WM_QUERYENDSESSION`/`WM_ENDSESSION`.
    While a run is active it calls `ShutdownBlockReasonCreate`, then drains.
  - **Truthful text.** The §4.13 cancel row says that without the window, a Windows
    logoff, shutdown or MSI upgrade is a kill. §6 states what an MSI upgrade does to an
    in-flight export. Remaining kills recover via XC2.
  - **Tests.** MA rows: a session built under a cancelled handler context, with a 2-s
    fake token, still refreshes; Drain finalizes a running export.

  *Closes:* L3-1, L5-4, L9-3(a), L3-3, L7-3, L5-11 (shutdown half).
- **XC7 — Token write-back checks identity, and sign-in changes stop runs.**
  - **In `internal/graph`.** `putIfNewer` persists only over an existing entry with the
    same UPN and an older expiry. It never re-creates a cleared store. `reload` refuses a
    token stored for another UPN. This ships with capture rows beside MA-258.
  - **Stopping runs.** Clear sign-in, and any sign-in, cancels a running Microsoft 365
    export with a new run-fatal class, "sign-in cleared or changed" (R-z). The run is
    finalized and can be retried from its run record. Every 100 messages the run also
    checks `graph.SignedIn`.
  - **Confirmation.** A workspace sign-in persists only after a confirmation, shown when a
    weekly backup is installed and the UPN differs from the previous signer.
  - **Wording.** §5.2's "adds no new token copy" is corrected.
  - **Tests.** MA rows: a clear during a run leaves the store empty; a sign-in as B during
    A's run leaves the store as B and stops A's run; a deleted entry is not re-created.

  *Closes:* L3-2, L6-1, L7-4, L10-14.
- **XC8 — The sign-in job has its own slot and a guarded status.** `/api/m365/signin` runs
  in its own sign-in slot, under one process-wide mutex shared with capture's device flow.
  The device code comes from a guarded POST `/api/m365/signin-status`, never from GET
  `/api/activity`. *Closes:* L5-6 (slot half), L5-5 (device-code half).
- **XC9 — Graph failures: backoff, a time-based breaker, and named sign-in errors.**
  - **One classifier,** shared with fault-isolation's `classifyFetch` (XC25).
  - **Transport-class errors** (no HTTP status; 502/503/504 after the client's retries;
    idle timeout) back off on the same message, 1 s → 60 s, and never fail it. R-u fires
    only after 5 minutes with no successful request.
  - **Sign-in errors.** `*oauth2.RetrieveError` with invalid_grant, interaction_required,
    invalid_client, unauthorized_client or consent_required → R-c. This applies in the run
    and in the preview and list endpoints.
  - **401 and 403.** A 401 gets one forced refresh and one retry. A 403 is probed with
    `GET /me`.
  - **At a run-fatal stop,** the in-flight message goes to `failures`, never to the head
    of `not_attempted`.
  - **5xx breaker.** 25 consecutive 5xx → R-u2.
  - **Tests.** MA rows cover each case.

  *Closes:* L3-6, L9-1, L9-2, L7-9.
- **XC10 — Destination and archive failure classes are per OS, with a write-side breaker.**
  - **Matching.** `errors.As` on `syscall.Errno`. Unix: ENOSPC, EDQUOT, EROFS, EIO, ENXIO,
    ENODEV, ESTALE, ETIMEDOUT. Windows: 112, 39, 19, 21, 1167, 55, 64, 59, 82.
  - **After any write failure,** the root is probed through the `os.Root`; a failed probe
    is run-fatal.
  - **Breaker.** 5 consecutive write failures are run-fatal.
  - **Rename retries.** Sharing-violation and access-denied renames are retried 5 times,
    100 ms → 2 s.
  - **Wedged runs.** If the run is still blocked 30 s after Cancel, its slot is released
    and the run is marked "Stopped: the destination is not responding". §5.2 states the
    kernel-blocked-write limit.
  - **Archive source.** A missing-file or read error probes the archive root. If the
    archive is gone, the run becomes run-fatal R-o2.
  - **Tests.** MA-427 gains table-driven Windows errno cases, run on every CI OS, plus an
    archive-gone case.

  *Closes:* L7-1, L9-4(b,c), L7-12.
- **XC11 — Free-space policy and a finalization reserve.**
  - **At preview and start.** The policy is stated. Start refuses with R-g2 when the size
    estimate plus a reserve (about 1 KB × messages + 1 MiB) exceeds free space. The preview
    warns when messages of unknown size could exceed it.
  - **During the run.** `util.FreeBytes` is checked every 50 messages, and R-g fires when
    free space drops below the reserve, so finalization always fits. The same floor applies
    to the state-directory volume.
  - **Tests.** MA-427 uses a fake filesystem that stays full, plus a removed-volume case.
    Both still offer "Export the remaining messages".

  *Closes:* L3-5, L7-2.
- **XC12 — Removable and foreign filesystems.**
  - **Directory size.** No export directory holds more than 1,000 files. Messages go into
    month subdirectories, split `~2` … beyond that.
  - **Filesystem warnings.** The preview reads the filesystem type and warns on FAT32 (the
    4 GiB per-file limit) and on FAT/exFAT/SMB ("access can't be restricted on this
    drive").
  - **Modes.** They are set only at creation, never by a later chmod. README, the C2-1 row
    and §8.5 state that FAT, exFAT and SMB do not keep them.
  - **New refusal R-w** for EACCES/EPERM when creating the root.
  - **Tests.** MA rows: a fault hook enforcing FAT's 65,536-entry cap; the R-w twin.

  *Closes:* L3-7, L7-11(a–c), L1-13, L10-15 (modes half).
- **XC13 — The final placement never replaces a file.**
  - **No-replace rename:** RENAME_NOREPLACE, renamex_np RENAME_EXCL, or MoveFileEx without
    REPLACE_EXISTING. Elsewhere, Lstat inside the root, then link + unlink. EEXIST moves to
    the next unique name or fails that message.
  - **Name collisions.** Names that match the 8.3 alias pattern are disambiguated. Folder
    mkdir is exclusive and never swallows EEXIST. FoldKey gains the upcase-table
    equivalents (ı/I).
  - **P7 relabelled:** "never silently merges; a collision the key misses fails that
    message".
  - **Tests.** MA-418 sub-cases.

  *Closes:* L3-8, L10-5.
- **XC14 — Verification aids, with their limits stated.**
  - (a) P9, README and §8.4 state that the check does not detect added files. README and
    §7.5 add an extra-files check for each OS.
  - (b) README says "open a terminal in this folder". The PowerShell line prints "OK: N
    files checked", or the counts of mismatched and missing files.
  - (c) Export activity gets "Check this export": a re-hash, a comparison with the sealed
    fingerprint, and a list of unlisted files, shown GREEN/RED (X6).
  - (d) Before the sums are written, every recorded file is Lstat-ed. A file that is gone
    (for example quarantined by antivirus) makes the run partial and is named.
  - (e) The fingerprint gets a copy button and "Save fingerprint receipt". Fingerprints go
    to a small ledger that compaction never prunes. P9 and §4.16 say that losing it loses
    the sender's anchor.
  - (f) The PowerShell form is labelled "lab-pending (MA-442)" inline.
  - (g) MA-421 and MA-422 add a planted-file case.

  *Closes:* L1-4, L4-3, L3-10, L3-18, L10-8, L7-11(d).
- **XC15 — A retry chain is one hand-over set.** Retries are named `<root>-part2`, and so
  on. Each manifest carries the whole chain, and README lists the sibling parts. Export
  activity shows one set fingerprint: the SHA-256 over the chain's sums files, in order.
  *Closes:* L4-4.
- **XC16 — Preview counts: "before == after" is the only stability test.**
  - **Re-listing.** When before ≠ after, the folder is re-listed once and the two listings
    are merged. A WARN appears only if the second listing is unstable too.
  - **Info line.** "Listed ≠ reported" becomes a separate, true info line.
  - **Tests.** MA-415 adds a stable folder whose reported count exceeds what is listed, and
    a case with arrivals during the listing.

  *Closes:* L1-3.
- **XC17 — Selection ids: short row handles, a coherent body cap, validated folder ids.**
  - **Row handles.** Picks and exclusions reference short opaque row handles that the
    server issues from the seen-set, so 10,000 of them fit inside 1 MiB. A 413 maps to an
    R-j-style refusal. MA-410 uses ids of realistic length.
  - **Folder ids.** Microsoft 365 folder ids are accepted only if they are in the session's
    cached tree. Each message's folder comes from `parentFolderId`. MA-410 adds an unseen
    id, a well-known name and `..`.
  - **The seen-set** is the gate for handles. Resolution never adds to it, and the handles
    of the live selection are pinned.

  *Closes:* H1b, L5-10, L6-6, L2-2 (gate half).
- **XC18 — The session's lifecycle is visible to the user and bounded.**
  - **Cursors.** A cursor's TTL refreshes on use. An expired cursor continues with a keyset
    query (`receivedDateTime lt <last row>`) or gets a clear refusal.
  - **New refusals:** R-x (list expired) and R-y (dashboard restarted).
  - **Idle release.** The snapshot, tree, seen-set and cursors are released after 30
    minutes idle.
  - **Memory bounds.** The seen-set gets a byte cap. MA-432 asserts the session heap that
    remains after a large browse.

  *Closes:* L4-2, L7-13, L3-16, L9-8.
- **XC19 — Archive reads are correct for redacted, old and hostile archives.**
  - (a) Resolution intersects the selection with what is on disk (R21). Records whose files
    are gone are left out, counted in the preview, and their metadata never enters the
    export.
  - (b) Before listing, the index meta version is read. A newer one gets R-p. An older one,
    or `Rekeyed > 0`, maps keys through `state.MigrateKey`, or disables the list with a
    named message.
  - (c) `UnknownSentinel` is filtered out of notes.
  - (d) A file with nlink > 1 is refused, and "closes the race" becomes "closes escape".
  - (e) MA-435 gets v1 and v2 fixtures.

  *Closes:* L1-6, L8-4, L8-5, L6-7.
- **XC20 — Archive concurrency holds on Windows; SQLite is opened read-only.**
  - **Manifest.** The snapshot is read through the archive `os.Root` (share-delete), never
    `os.ReadFile`. The `health.Gather` precedent is noted. P15 is conditional on Windows
    until a MA-442 sub-case passes.
  - **search.db.** It is opened `mode=ro`/`query_only`. P1's condition covers the WAL
    checkpoint, and MA-436 compares rows, not bytes.
  - **Rebuild.** List requests take a shared lock that Rebuild takes exclusively around its
    rename.

  *Closes:* L1-8, L8-3, L3-17, L10-16, L5-6 (Rebuild half).
- **XC21 — Each source's scope is stated.**
  - **Card copy.** "Microsoft 365: your own mailbox only". "Archive: every mailbox this
    archive holds (…)".
  - **Tree and preview** show each store's mailbox.
  - **Manifest.** `source` is specified for `kind: archive`, and each entry records its
    store's mailbox.
  - **§3 and §7.3** are corrected.

  *Closes:* L1-1.
- **XC22 — The inheritor hazard is guarded at the Settings save.** When a weekly backup is
  installed and the archive location changes, saving asks: "Your weekly backup will now
  archive <UPN> into <folder>". The choices are keep, remove the backup, or cancel. §7.4
  step 1 tells the inheritor to remove it. E4's inheritor cell is gated on Q3 (OR5).
  *Closes:* L4-1 (PARTIAL: the Type II residual stays until Q3).
- **XC23 — E0 is design-local-confidentiality.md slices C1 and C2, merged before E3.**
  - **Ownership.** "Otherwise built here" is deleted.
  - **Routes.** They register through `localweb.Mux`. MA-406 is deleted, because
    MA-372/MA-378 cover it. MA-378 replaces MA-405's private route walk; MA-405 keeps its
    method and guard twins.
  - **URLs.** Every page, fetch and sidebar URL is relative. No text assumes fixed
    :8097/:8099.
  - **Containment** stays on `os.Root`. The ACL posture is OR1. F15 moves to E1 (XC26).

  *Closes:* L2-1, L5-1, L9-7(a).
- **XC24 — X12's scope, Q5's premise and the E0 cutover are stated truthfully.**
  - **X12** is scoped to the workspace and export routes. The existing GETs
    (`/api/activity`, `/api/health`, `/api/schedule-state`) are recorded as an X12
    condition (Partial) and assigned to E0/C2.
  - **Q5's premise.** §1.4, the F3 row and Q5 are re-premised: the shipped dashboard
    already lets any local account copy the user's live mailbox, and E0 ships on its own
    merits.
  - **E0 cutover.** §6 gains an E0 paragraph covering bookmarks, the RMM README step and
    reader links.

  *Closes:* L1-12, L5-5 (X12 half), L1-5, L8-10.
- **XC25 — One Graph layer, coordinated with design-fault-isolation S3.**
  - **From S3,** E1 takes `StatusError{Code, GraphCode, Msg}`, `classifyFetch`, the MIME
    watchdog and cap, and the nextLink repeat guard. MA-402's "Error() keeps today's text"
    is dropped.
  - **The pin** compares after `url.Parse`, with a case-insensitive host, default-port
    normalization and a path prefix on a segment boundary.
  - **Capture's half of the pin** ships as its own slice, E1c, with capture rows and a
    short adversarial note. It is gated on a MA-443 sub-case that records real nextLink
    shapes.
  - **Wording.** "Unchanged" is deleted from §4.5.

  *Closes:* L9-6, L2-3, L8-2.
- **XC26 — The bearer token and response bodies are bounded.**
  - **Redirects.** `CheckRedirect` refuses any redirect that leaves the base scheme, host
    and port. MA-401 adds a 302 sub-case.
  - **Bodies.** Graph JSON is read through `io.LimitReader`.
  - **F15** is fixed in E1 (`http.Client.Timeout`), with a stalled-`/token` row. The
    `$value` path gets a header timeout of at least the idle window.
  - **Honest labels.** The pin is defence in depth against a misbehaving server, not
    against a TLS proxy. R-v's legibility is conditional on F17 or on S3's typed throttle
    error.

  *Closes:* L6-2, L9-3(b), L9-11, L10-11.
- **XC27 — MessagePage carries what the overlay and the manifest need.**
  - **Fields.** `$select` is built from `messageSelectFields` + `parentFolderId`. Size and
    sensitivity are requested by an id-aware filter and decoded by property id.
  - **Immutable ids.** The first page's "honored" flag is kept per cursor and feeds
    `immutable_ids`. `ApplyGraphState` is kept.

  *Closes:* L5-7, L1-7.
- **XC28 — Each slice ships only what it builds, and every row belongs to one slice.**
  - **E7 folds into E3.**
  - **Readable HTML** is hidden until E5, with `exportHTML` defaulting OFF until then.
    MA-445 asserts the default for each slice.
  - **Refusal rows.** MA-438's R-o/R-p clauses move to E4.
  - **Row splits.** MA-403, MA-426, MA-440, MA-411, MA-431, MA-432 and MA-422 are split
    into per-slice rows. MA-422 splits into an in-Go U row, an external `!windows` row and
    the PowerShell line inside MA-442.
  - **Proof tags.** Proofs are tagged "MA-x (slice Ey)".

  *Closes:* L5-2, L10-3, L5-3.
- **XC29 — Ids stay provisional until the operator allocates them.** R22, S42 and X12–X14
  collide with ids proposed by four sibling designs. Here they are provisional labels, and
  no covers marker may cite them. Every Covers cell is rewritten to the allocated ids
  (OR2) before any row is accepted. *Closes:* L10-1.
- **XC30 — Claims carry their conditions inline.**
  - **P5 and MA-412:** the token-refresh exception.
  - **P15:** a concurrent full capture can fail individual messages.
  - **P16 and MA-419:** stated with Readable HTML on and off.
  - **§4.14:** conditional on F17. Back off while a capture runs. State the concurrency
    budget, and serialize previews while a run is active.
  - **§4.15:** drop "observed".
  - **`LiveRefreshIn`:** defined to exclude the tool's own leading metas, with a MA-423
    positive twin.
  - **"Attachments are inside the .eml file"** appears only in the export's own renders,
    with a byte-identical archive-render row.
  - **Citations** refreshed.

  *Closes:* L1-9, L10-7, L10-10, L8-9, L10-13, L10-6, L8-8, L10-12.
- **XC31 — The folder picker's footprint is stated and contained.**
  - **Dependencies.** zenity is new to the `mailarchive-desktop` binary. On Linux the
    picker looks for qarma, zenity or matedialog.
  - **Windows.** The dialog loads shell extensions in-process, so Browse is disabled while
    an export runs.
  - **Failure rows.** §5.2 gets picker crash and hang rows.
  - **Supply chain.** CI runs `GOOS=windows govulncheck ./cmd/mailarchive-desktop`.
  - **Out-of-process picker:** OR8.

  *Closes:* L10-9, L5-11 (zenity half), L7-10, L9-5, L9-10.
- **XC32 — The destination's posture is shown before the run.**
  - **Posture line.** The preview shows owner-only, other accounts can read, or can't be
    restricted.
  - **Cloud-sync detection** gains iCloud Desktop & Documents, `iCloudDrive` and
    "Dropbox (<team>)", and is labelled best-effort.
  - **Root name.** It gets a random suffix.
  - **Creation.** The destination is opened once and its identity re-checked at start. The
    root is created through that handle.
  - **The ACL itself:** OR1.

  *Closes:* L6-8, L10-15 (cloud half).
- **XC33 — "Open folder" opens only the directory this run created.**
  - **Checks first.** Lstat: no symlink or reparse point, a directory, the recorded
    identity, and a marker or manifest present. Otherwise R-r.
  - **Reveal, never launch.** macOS `open -R`, Windows `explorer.exe /select,`, Linux
    `xdg-open` on the verified directory.
  - **Tests.** MA-430 adds a swapped symlink, a swapped `.app` and a different-inode
    directory.

  *Closes:* L6-4.
- **XC34 — Settings writes are serialized.** One mutex covers each settings
  load-modify-save. Each save reloads under the lock and changes only its caller's fields.
  *Closes:* L6-9.
- **XC35 — Friction cells the walk missed.**
  - **Render-only problems** are counted as "N exported without a readable copy" and are
    not offered for retry.
  - **auth=app installs** get a refusal that names the fix (R-a2), and §6 "Existing
    consents work" is corrected.
  - **§7.2 gains six cells:** network blip, FAT directory full, clearing or switching the
    sign-in mid-run, a forced Windows restart, AV quarantine, and retry after a restart.

  *Closes:* L4-6, L4-7, L4-9.
- **XC36 — One archive-read gate and one naming helper, or a stated fork.** Either
  `extract`, the `Exporter` and the workspace share one archive-read helper and one
  exported stem/unique-name helper, or §6 states the fork and its owner. P8's "the same
  gate" is true only then. *Closes:* L5-8.
- **XC37 — The rollback is stated as it really is.** §6 states that the MSI blocks a
  downgrade. Rolling back means uninstalling and reinstalling, and that also removes E0 if
  the two shipped in one release. If the owner answers Q6 yes (OR6), the switch is the
  rollback path: when it is off, no routes are registered and there is no sidebar group,
  with a MA row asserting both. *Closes:* L8-1.
- **XC38 — Dependencies without an owner are labelled unowned.** F17, F75, F91 and F111
  are marked "unowned" next to P7, §4.14 and §4.18 until an owner is named (OR9). P13's F15
  condition goes away instead, because F15 is fixed in E1 (XC26). §10's
  "can land at any time" is corrected, because path portability needs its own migration.
  *Closes:* L9-7(b,c).
- **XC39 — Reconcile with fault-isolation's reading of the same owner decision.** Cite
  `design-fault-isolation.md` (inputs, §4.10, S5) and state where its reading differs. E2
  does not start until OR3 is answered. *Closes:* L10-2.
- **XC40 — Revision 2 re-runs lenses 3, 6 and 7 before E2 code.** These are the
  durability, security and failure-domain lenses. They run as an addendum to this review,
  because XC1–XC5 change where the app keeps its truth. *Closes:* re-verification of the
  hidden blocker.

## Owner rulings (not imposed by this review; each blocks the slice named)

- **OR1 (design Q10) — the export root's ACL posture.**
  - **The question.** `design-local-confidentiality.md` expects workspace writes to get the
    protected owner/SYSTEM/Administrators DACL (its P1/P3, and its "owner-only at rest"
    R22). Revision 1 lets the export inherit the destination's ACL.
  - **Recommendation:** inherit, and show the XC32 posture line. A hand-over copy must be
    readable by the recipient's account. On NTFS removable media, a protected owner-only
    DACL forces a standard-user recipient into an ownership takeover that rewrites every
    file's ACL.
  - **Consequence.** Either answer amends local-confidentiality §6's workspace sentence,
    which this review cannot do.
  - **Blocks:** E2's Windows write path. (L5-1, L6-8, L1-13)
- **OR2 (design Q11) — allocate the R/S/X/MA ids across the five designs in flight**
  (fault-isolation, recapture-integrity, mime-fidelity, local-confidentiality,
  extraction-workspace). **Blocks:** catalog acceptance of any slice's rows. (L10-1)
- **OR3 (design Q12) — which engine is the owner's "simpler tool"?**
  - **The conflict.** This design builds a separate copy-export engine. Fault-isolation
    records the same relayed decision as "that flow drives the same engine (`app.Run` /
    `app.RunGraph`)".
  - **Recommendation:** this workspace's copy engine. The owner's words are "choose the
    export destination to copy it to", and §1.2 shows that `RunGraph` writes an archive,
    not a hand-over folder. Fault-isolation's input line would then be amended.
  - **Blocks:** E2. (L10-2)
- **OR4 (design Q13) — supersede desktop P7.** Does the workspace replace the approved
  desktop design's P7 "Export a copy (extract mbox/eml)", which drops dashboard mbox?
  **Blocks:** E3's sidebar and §3 wording. (L5-8)
- **OR5 (design Q3, now blocking) — a read-only "another archive folder" source.**
  **Blocks:** E4's inheritor cell; XC22 is the interim guard. (L4-1)
- **OR6 (design Q6) — a deployment switch.** Recommended yes, as the rollout's rollback
  path (XC37). (L8-1)
- **OR7 (design Q5, re-premised) — confirm that the workspace does not ship before E0.**
  E0 closes an exposure the shipped dashboard already has, and should ship on its own
  merits. (L1-5)
- **OR8 (design Q14) — an out-of-process folder picker (a self re-exec).** This amends the
  desktop owner ruling P1, "in-process (NOT shell-out)". XC31's minimum applies either way.
  (L7-10, L9-5)
- **OR9 (design Q15) — name owners for F17, F75, F91 and F111,** or take them as slices
  here. An export-only sanitizer variant avoids the archive migration. Until then they stay
  labelled unowned (XC38). (L9-7)

## Rescue outcomes (every finding the skeptic left at major)

| Finding | Outcome | Named change (short) | Condition |
|---|---|---|---|
| L1-2 | DISSOLVED | per-run run record; retry reads only it; P4 second admission path | XC1, XC2 |
| L1-3 | DISSOLVED | before == after is the only stability test; union re-list; info line | XC16 |
| L1-4 | DISSOLVED | limit stated inline; stock extra-files check; Check this export lists unlisted files | XC14 |
| L1-14 | DISSOLVED | background reconcile, 5 s bound, terminal records; progress and history split | XC4 |
| L2-1 | DISSOLVED | E0 = local-confidentiality C1 + C2 merged before E3; MA-406 deleted | XC23 |
| L3-1 | DISSOLVED | session on main's root context; per-call derived contexts | XC6 |
| L3-2 | DISSOLVED | UPN-aware putIfNewer that never re-creates; reload checks UPN; R-z cancels runs; SignedIn poll | XC7 |
| L3-3 | DISSOLVED | truthful row; Drain(15 s); Windows end-session window; run-record recovery | XC6, XC2 |
| L3-4 | DISSOLVED | plan in the run record; Finish as partial; Export the remaining | XC1, XC2 |
| L3-5 | DISSOLVED | retry from the record; finalization reserve; free-space floor; persistently full MA-427 | XC11, XC1 |
| L3-6 | DISSOLVED | same-message backoff; 5-minute no-progress budget | XC9 |
| L3-7 | DISSOLVED | ≤1,000 files per directory; FS-type WARN; write breaker | XC12, XC10 |
| L3-9 | DISSOLVED | conditional README/manifest status; one reconcile rule; sealed line; Check and finish | XC5 |
| L3-11 | DISSOLVED | as L1-14; reconcile never parses a destination manifest | XC4 |
| L3-12 | DISSOLVED | as L1-2; no per-id GETs; outside the pick cap | XC2 |
| L4-1 | **PARTIAL** | confirmation at the Settings save; E4 inheritor walk gated on Q3 — the Type II residual remains until Q3 | XC22, OR5 |
| L5-1 | DISSOLVED | cite the sibling as E0's owner; localweb.Mux; relative URLs; ACL posture to the owner; renumber | XC23, XC29, OR1 |
| L5-2 | DISSOLVED | E7 into E3; Readable HTML hidden/OFF until E5; R-o/R-p to E4 | XC28 |
| L5-3 | DISSOLVED | split cross-slice rows; MA-422 split by platform | XC28 |
| L5-4 | DISSOLVED | as L3-1 | XC6 |
| L6-1 | DISSOLVED | as L3-2 | XC7 |
| L6-3 | DISSOLVED | no destination spool; hashes in the run record; derived files verified before parse | XC1 |
| L6-4 | DISSOLVED | root identity recorded; Lstat + identity + marker before opening; reveal semantics | XC33, XC3 |
| L6-5 | DISSOLVED | reconcile downgrades only; never a fingerprint from disk; read-back row in §8.2 | XC4 |
| L7-1 | DISSOLVED | per-OS errno table; root probe; 5-failure breaker; rename retry | XC10 |
| L7-2 | DISSOLVED | as L3-5, plus the floor on the state-directory volume | XC11 |
| L7-3 | DISSOLVED | as L3-3, plus §6 in-flight MSI upgrade text | XC6 |
| L7-4 | DISSOLVED | as L3-2, plus confirmation before persisting a different UPN | XC7 |
| L7-5 | DISSOLVED | as L3-9 | XC5 |
| L7-6 | DISSOLVED | as L1-14 | XC4 |
| L7-8 | DISSOLVED | as L1-2 | XC2 |
| L7-11 | DISSOLVED | directory split; creation-time modes; R-w; Check this export | XC12, XC14 |
| L9-1 | DISSOLVED | one classifier with fault-isolation; 503/502/504/idle are transport-class; time budget | XC9 |
| L9-2 | DISSOLVED | `*oauth2.RetrieveError` codes → R-c in run, preview and list | XC9 |
| L9-3 | DISSOLVED | root context; F15 required in E1 (`Client.Timeout`) | XC6, XC26 |
| L9-4 | **PARTIAL** | off-path reconcile; network-gone errors fatal; slot released 30 s after Cancel — a kernel-blocked write remains a stated limit | XC4, XC10 |
| L9-6 | DISSOLVED | E1 depends on fault-isolation S3; pin normalization; MA-402 text clause dropped | XC25 |
| L10-1 | DISSOLVED | placeholder ids until the operator allocates | XC29, OR2 |
| L10-3 | DISSOLVED | as L5-2, plus per-slice proof tags | XC28 |

## Findings

Severity is the skeptic's verified severity; the finder's, where different, is in
parentheses. Duplicates are graded once and cite their twin. H-rows are the rescue's.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| H1 | rescue | P19, §4.7 step 5, §4.10, §4.11, §4.13, §4.16 | The untrusted, unreliable destination is the app's system of record for recovery, integrity, status and identity; the lenses' fixes contradict each other | CONFIRMED (blocker, dissolved) | P19 "reconciled against each export directory, which is the truth"; §5.2 "another program writing there"; root of ~20 findings | XC1–XC5, XC40 |
| H1a | rescue | §5.1 W2 before W3, §4.9 | A co-writer pre-creates the next timestamped names; the journal binds the planted directory, and reconcile promotes its planted manifest to "Complete" | CONFIRMED (major) | W2 "Journal start" precedes W3 exclusive mkdir with "-2 … -99"; reconcile "manifest has a status → that status". Overlaps L3-13(c), L7-7 | XC3, XC4, XC32 |
| H1b | rescue | §4.2 1 MiB cap vs §4.4 R-j | R-j's 10,000 is unreachable; a bare 413 at ~3,500–5,000 picks | CONFIRMED (minor) | Pick = folder id + message id ≈ 200–300 B. Same arithmetic as L5-10 | XC17 |
| L1-1 | 1 | §4.16 card, §7.3, §3, §4.10 `source` | "Own mailbox only" copy covers the whole workspace, but the archive source exports every mailbox the archive holds; the manifest does not say whose mail it is | CONFIRMED (minor; finder major) | Card: "The Extraction workspace reads: your own mailbox … (Mail.Read)"; §7.3 "v1 cannot read anyone else's mailbox"; saveSettings stores any Out; `source` shown only for m365 | XC21 |
| L1-2 | 1 | P4, §4.4 R-j2, §4.13 Retry, MA-437 | After a restart the seen-set is empty, so retry is refused by R-j2; or it bypasses the check and P4 is false | CONFIRMED (major) | P4 "accepted only if this session served that id"; §4.13 "build a new preview from that run's export-manifest.json"; no restart case | XC1, XC2 |
| L1-3 | 1 | §4.7 step 2, §4.4, MA-415 | "Listed ≠ after" fires forever on a stable folder whose `totalItemCount` counts non-messages; false "changed" WARN with an impossible remedy | CONFIRMED (major) | §4.7 "or the number listed ≠ after, re-list (at most twice) … raise a WARN"; §4.4 concedes the count "can include items /messages does not return" | XC16 |
| L1-4 | 1 | P9, §4.10 README, §7.5, §8.4 | A file added to the export passes every stock check and the fingerprint still matches | CONFIRMED (major) | SUMS "covers every file in the export except itself and the marker"; checks walk the list; README offers no way to find unlisted files | XC14 |
| L1-5 | 1 | §1.4, §4.18 F3, Q5 | E0's premise is false: the shipped dashboard already lets any local account copy the live mailbox (POST /api/settings Out, then /api/capture) | CONFIRMED (minor) | saveSettings accepts any Out after GuardLocalPOST; startCapture runs RunGraph into effectiveOut. Conclusion (E0 first) still holds | XC24, OR7 |
| L1-6 | 1 | §4.6 step 5, P12 | A deleted-not-reindexed record is listed and fails; its subject and sender enter the export's `failures`, leaking what R21 already hides | CONFIRMED (minor) | §4.6 "fails as 'removed from the archive'"; P12 lists failures with subject; MA-211's disk intersection not applied | XC19 |
| L1-7 | 1 | §4.11 vs §4.5 `$select` | The `applyGraphState` overlay has no inputs and wipes a MIME "Importance: high" | CONFIRMED (minor) | MessagePage selects no importance/isRead/categories; graph.go sets `m.Importance = graphImportance(ref.Importance)` unconditionally | XC27 |
| L1-8 | 1 | P15, §4.3 snapshot, MA-431 | On Windows the snapshot's `os.ReadFile` (no FILE_SHARE_DELETE) makes a concurrent capture's manifest rename fail | CONFIRMED (minor) | manifest.go:514; syscall_windows.go:395 share mode read+write only; Save is a bare `os.Rename` (:1422). Correction: CI does run windows-latest, but this window is not targeted | XC20, XC30 |
| L1-9 | 1 | P5, §4.7, MA-412 | A preview that crosses token expiry rewrites the token cache in the config directory | CONFIRMED (minor) | deviceSource.Token → putIfNewer → WriteFileAtomic0600 under os.UserConfigDir; MA-412's fake never expires | XC30 |
| L1-10 | 1 | P14, MA-429 vs §5.1 W3/W4 | MA-429's literal assertion fails at W3/W4 (empty or temp-only root) | CONFIRMED (minor) | §5.1 W3 "an empty folder with no marker"; W4 "temp residue" | XC5 |
| L1-11 | 1 | R22 vs §4.8 Create folder | R22's "writes ONLY inside a newly created export directory" contradicts Create folder's mkdir | REFUTED | R22's subject is the export run; §4.9: "Nothing is written outside it (except Create folder's single mkdir, §4.8)." | — |
| L1-12 | 1 | proposed X12 | X12 is false on adoption: `/api/activity` and `/api/health` stay open GETs | CONFIRMED (minor) | X12 "every dashboard endpoint that returns mailbox or archive content … is a POST"; no slice converts them | XC24 |
| L1-13 | 1 | §4.18 C2-1, §4.9 Modes, §8.5 | On FAT/exFAT/SMB under macOS/Linux, modes come from the mount, not 0600 | CONFIRMED (minor) | C2-1 row "Avoided for exports: 0700/0600"; §8.5 conditions privacy only on Windows ACLs | XC12, OR1 |
| L1-14 | 1 | §4.16 Reconciliation, §4.13 Progress | Reconcile on every poll stats old runs forever; an offline UNC path freezes progress and Cancel | CONFIRMED (major; finder minor) | "Reconciliation (each activity request)"; synthetic end only in the manifest case; §4.8 "an offline mapped drive or UNC path can stall a listing for minutes" | XC4 |
| L1-15 | 1 | P4, §8.4 proxy row | The nextLink pin is credited against a TLS-inspecting proxy that already sees the token | REFUTED | §8.4 residual: "none beyond today's TLS trust" — a TLS-inspecting proxy is today's TLS trust. The redirect gap is L6-2 | — (row reworded by XC26) |
| L2-1 | 2 | §10 E0, P17, MA-406 | E0 is an undesigned security re-architecture, "otherwise built here as the first slice" | CONFIRMED (major) | §10 E0 "otherwise built here as the first slice … its own rows"; design-review.md: "Build may not start … without this artifact filed" | XC23 |
| L2-2 | 2 | §4.3 seen-set, R-j2, P4 | The seen-set gate defeats no stated persona; its refusal fires after restarts, retries and LRU eviction | CONFIRMED (minor; finder major) | After E0 the page is the trusted user (§8.1 #5); `/me`-scoped PathEscape'd ids; resolution's effect on the LRU unstated. The rescue keeps the gate with a stated role | XC2, XC17 |
| L2-3 | 2 | §4.5 pin + StatusError, §10 E1 | E1 changes shipped capture paging with no gate until E3, which waits on E0 | CONFIRMED (minor) | "It hardens capture too"; E1 "none alone … folded into E3's adversarial pass" | XC25 |
| L2-4 | 2 | §4.4 `/count`, P3 | The count endpoint is unnecessary; the page could compute it | REFUTED | A preference, not a failure: P3 "Counts are computed server-side from the cached folder tree"; §4.2 side effects "none (cache only)" | — |
| L2-5 | 2 | §10 E6, P11 | E6 is approved now and will change when C1-1 lands | REFUTED | Already isolated: E6 depends on "the C1-1 fix" with its own "adversarial addendum"; P11 "Until C1-1 lands, the option is not shipped" | — |
| L3-1 | 3 | §4.3 session (OMISSION: context) | A session built on `r.Context()` fails every refresh after the first expiry ("context canceled") → R-u | CONFIRMED (major) | NewDelegated stores ctx in newDeviceSource and conf.TokenSource; oauth2 v0.37.0 tokenRefresher reuses tf.ctx | XC6 |
| L3-2 | 3 | §4.3 Clear sign-in, §5.2 | A dropped session or a running export re-persists A after Clear, or overwrites B; the next headless capture adopts it | CONFIRMED (major) | putIfNewer writes on a missing cache and ignores UPN; §4.3 drops only "every outstanding selection and preview"; HeadlessCapture adopts the signer | XC7 |
| L3-3 | 3 | §4.13 cancel row (OMISSION: shutdown) | App stop is a kill: no finalization, no manifest, no retry; windowsgui gets no end-session event | CONFIRMED (major) | main.go never passes ctx to DashboardHandler; 5 s Shutdown, no wait; jobs on context.Background() | XC6, XC2 |
| L3-4 | 3 | §4.7 step 5, §5.1 W5/W6, §4.13, §7.2 | The plan lives only in memory; after a kill nothing says what remained; the Type III resume item is unbuildable | CONFIRMED (major) | §4.13 "An interrupted (killed) run has no manifest, so only 'Open folder' is offered" | XC1, XC2 |
| L3-5 | 3 | §4.13, R-g, §7.2 USB full, MA-427 | R-g's remedy needs a manifest a full or removed volume cannot receive; no free-space policy | CONFIRMED (major) | "If finalization itself fails (the disk is full), the marker stays"; MA-427 injects one ENOSPC | XC11, XC1, XC2 |
| L3-6 | 3 | §4.13 breaker, §4.15 | Ten instant dial/DNS errors in a 1–5 s blip stop a multi-hour run | CONFIRMED (major) | Client.get returns `c.hc.Do` errors at once; no wait or time budget designed | XC9 |
| L3-7 | 3 | §4.9 layout, §4.13 (OMISSION: entry limits) | FAT32's 65,536-entry directory cap fails a large folder, on Windows as thousands of "retryable" failures | CONFIRMED (major) | No split rule; flat mode shares one `messages/`; no write breaker | XC12, XC10 |
| L3-8 | 3 | §4.9 temp → rename, P6 | Rename replaces; an 8.3 alias (`REPORT~1.PDF`, `PROJEC~1`) overwrites a sibling | CONFIRMED (minor; finder major) | Rename replaces existing targets; SanitizeFilename keeps `~`; stems are safe (Slug maps `~`) | XC13 |
| L3-9 | 3 | §5.1 W7–W10, §4.10 README, §4.16 | README says complete before sums and marker removal; §5.1 and §4.16 reconcile rules contradict | CONFIRMED (major) | W8 "reconcile may read the status", W9 "recovered status shown, marker kept" vs §4.16 "marker is present → 'interrupted'" | XC5 |
| L3-10 | 3 | §4.10 fingerprint, §4.16 | The only export-time fingerprint record is compacted or lost; a recovered run may show a recomputed one | CONFIRMED (minor; finder major) | "deliberately not written into the export"; "keeps the last 200 runs"; "Losing the journal loses only the history list" | XC14, XC4 |
| L3-11 | 3 | §4.16, §4.13 Progress | As L1-14, plus re-parsing a manifest of up to ~100 MB per poll | CONFIRMED (major) | dashboard.go:488 polls every 1.5 s | XC4 |
| L3-12 | 3 | §4.13 Retry vs R-j2, R-j | Retry refused after a restart; the remaining set can exceed 10,000; the admission path is unstated; the manifest sits on a writable destination | CONFIRMED (major) | No second admission path in P4; R-j "≤ 10,000"; §5.2 "another program writing there" | XC2 |
| L3-13 | 3 | §4.16 journal, §5.1 W2/W11/W12 | A torn tail glues the next record; append and compaction are unlocked; W2 before W3 binds the wrong directory | CONFIRMED (minor) | "skipped on read" only; `truncateTornTail` exists (history.go:87-91) | XC3 |
| L3-14 | 3 | §6 Mixed versions | "No reader of exports exists" is false: retry and reconcile read the manifest | CONFIRMED (minor) | §4.13 Retry and §4.16 read export-manifest.json; no version rule | XC3 |
| L3-15 | 3 | §5.1 conditional, §4.15 | FAT32/exFAT missing from the durability conditional; per-file and directory syncs on flash missing from the ETA | CONFIRMED (minor) | Conditional names only "NTFS, APFS, ext4 data=ordered" | XC5 |
| L3-16 | 3 | §4.3 snapshot + seen-set | Hundreds of MB pinned for weeks in an at-login process | CONFIRMED (minor) | state.Load reads and unmarshals the whole file; LRU 250,000; no idle release | XC18 |
| L3-17 | 3 | P1, MA-436 | The last connection's close checkpoints WAL into `search.db`; MA-436 fails intermittently | CONFIRMED (minor) | index.go:32 WAL; OpenReadonly is a plain `sql.Open` | XC20 |
| L3-18 | 3 | §4.13 Finalization (OMISSION: read-back) | AV quarantine or a fake-capacity stick: "Complete" from write-time hashes, then the recipient's check fails | CONFIRMED (minor; finder major) | No re-read before "Complete"; the recipient's check still catches it | XC14 |
| L4-1 | 4 | §7.4 step 1, Q3 | The prescribed inheritor walk mixes the inheritor's mail into the inherited archive; the warning comes after the save, on another page | CONFIRMED (major) | §7.4 "Hazard, Type II"; HeadlessCapture reads effectiveOut at run time; friction-review.md: Type II drives the verdict | XC22, OR5 (PARTIAL) |
| L4-2 | 4 | §4.3 cursors, §4.7 expiry (OMISSION: behaviour on expiry) | "Load more" fails illegibly after 10 min or 64 listings; a restart loses the selection with bare 403s | CONFIRMED (minor; finder major) | Caps stated, behaviour not; §7.6 has no refusal | XC18 |
| L4-3 | 4 | §7.3, §7.5, §4.10 | No in-app check; README never says to run inside the export root; PowerShell success is silence | CONFIRMED (minor; finder major) | Relative paths in the verify lines; "(no output = every file matches)" | XC14 |
| L4-4 | 4 | §4.13 Retry, §4.15 | Every interruption adds a directory and a fingerprint; no combined index | CONFIRMED (minor; finder major) | "into a new export directory whose manifest names retry_of" | XC15 |
| L4-5 | 4 | §4.1 Include subfolders | Ticking it after selecting silently does nothing | REFUTED | §4.1: "Toggling it later does not rewrite existing entries; the summary names each entry." §7.1 uses the right order | — |
| L4-6 | 4 | §4.13 labels, §4.11 | Render-only problems are labelled "N messages not exported"; retry has nothing to do | CONFIRMED (minor) | "3 partial (any failure, any exported-original-only …)" → "Finished with problems — N messages not exported" | XC35 |
| L4-7 | 4 | P2, §6, §7.6 | An auth=app registration lacks public client flows and Delegated Mail.Read; a raw Entra error | CONFIRMED (minor) | graph-app-setup.md:65 app path adds Application Mail.Read only; device settings at :141-143 | XC35 |
| L4-8 | 4 | §4.10 fingerprint | 64 hex digits read aloud is error-prone, so senders skip it | REFUTED | §4.10 groups of 8 "for reading aloud"; §7.3 "copy the fingerprint … into the hand-over email (1)"; the failure is a behaviour guess | — (XC14 adds a copy button anyway) |
| L4-9 | 4 | §7.2 (OMISSION) | Realistic failure cells are missing from the walk | CONFIRMED (minor) | Seven cells; none for network blip, FAT full, clear or switch sign-in, Windows restart, AV quarantine, retry after a restart | XC35 |
| L5-1 | 5 | §2 R22/S42/X12, §4.2, §4.9 Modes, §10 E0 (OMISSION: local-confidentiality) | Not reconciled with the design that owns E0: different R22/S42/X12, absolute routes vs `/s/<P>/`, a private guard walk vs MA-378, inherited ACL vs a protected root | CONFIRMED (major; finder blocker) | No "local-confidentiality" in the design; local-confidentiality.md:620-624 expects `localweb.Mux`, MA-378, ArchiveFS + protected root; §4.9 "inherits the destination's ACL". Dissolved by named edits + OR1 | XC23, XC29, OR1 |
| L5-2 | 5 | §10 E3 vs §7.6, §4.17, MA-438/445 | E3 ships remedies naming E7's buttons, Readable HTML ON before E5, and archive refusals before E4 | CONFIRMED (major) | R-c/R-g/R-u "click 'Export the remaining messages'" (E7); MA-445 (E3) defaults exportHTML ON; MA-438 (E3) asserts R-o/R-p | XC28 |
| L5-3 | 5 | §9/§10 row assignment | Rows sit in slices that cannot build them; "halves" share one covers marker; MA-422 needs sha256sum on Windows CI | CONFIRMED (major) | MA-403 in E1 and E3; MA-426/440 (E2) need E5; MA-411's archive clause (E3) needs E4 | XC28 |
| L5-4 | 5 | §4.3 session context | As L3-1 | CONFIRMED (major) | NewClient: "The returned client is not valid beyond the lifetime of the context" | XC6 |
| L5-5 | 5 | X12/P17, §4.2 route walk, §4.16 sign-in, MA-405 | No guard wrapper or enumerable registry exists; existing GETs break X12; the device code would ride GET /api/activity | CONFIRMED (minor; finder major) | GuardLocalPOST is inline; bare `http.NewServeMux`; /api/activity, /api/health, /api/schedule-state are open | XC23, XC24, XC8 |
| L5-6 | 5 | §4.2 sign-in job, §4.3, §4.14 | No slot named for sign-in (refused during verify, or two device flows race); the index open overlaps Rebuild's rename | CONFIRMED (minor) | runJob is the shared slot; first sign-in ends in unconditional `be.put` | XC8, XC20 |
| L5-7 | 5 | §4.5 MessagePage, §4.11, §4.10 `immutable_ids` | MessagePage drops capture's fields and the "honored" detection; a combined `$expand` would misdecode sensitivity | CONFIRMED (minor) | messageSelectFields, sensitivityExpand; sensitivityFromExtProps "we ask for exactly that one property" | XC27 |
| L5-8 | 5 | P8, §4.6, §4.9 (OMISSION: desktop P7) | Parallel archive-read and naming code drifts; desktop P7's "Export a copy" is dropped without a ruling | CONFIRMED (minor) | extract's readBounded uses os.Open; baseNameN/stemFor/uniqueName unexported; design-mailarchive-desktop.md P7 | XC36, OR4 |
| L5-9 | 5 | P12, §4.13 Outcome | Cancel → 3 contradicts X1's "handled interrupt = 0" | REFUTED | §3 lists "A CLI verb" as a non-goal; CopyOutcome reuses extract's declared 0/3/1 set, where 3 = not all emitted; X1's interrupt rule is capture's | — |
| L5-10 | 5 | §4.2 1 MiB vs §4.4 R-j | Realistic ids hit 413 at ~3,400 picks; R-j never fires | CONFIRMED (minor) | Pick shape `{"folder":…,"id":…}`; MA-410 expects a bare 413 | XC17 |
| L5-11 | 5 | §6 Rollback, §4.8, §4.13 cancel row | zenity is not linked into the desktop binary today; the shutdown cancel is unreachable | CONFIRMED (minor) | `go list -deps ./cmd/mailarchive-desktop` has no zenity; file_windows.go:195 CoCreateInstance in-process | XC31, XC6 |
| L6-1 | 6 | §4.3, §7.3 | As L3-2, plus §7.3's D-over-A overwrite | CONFIRMED (major) | tokenCfg keyed by tenant and client; headless capture adopts the store's signer | XC7 |
| L6-2 | 6 | P4, §4.5, §8.2, §8.4, MA-401 | Bearer forwarded on cross-host redirects; JSON bodies unbounded | CONFIRMED (minor; finder major) | No CheckRedirect; oauth2 Transport sets the header on every hop; getJSON uses an unbounded decoder | XC26 |
| L6-3 | 6 | §4.10 spool, §4.11 read-back, P9 | A co-writer edits a `.eml` and its spool line; the sums and fingerprint vouch for forged bytes | CONFIRMED (major) | Spool inside the export root; HTML from ".eml bytes (already on disk, read back …)" | XC1 |
| L6-4 | 6 | §4.2 open-folder, §4.16, MA-430 | A symlink or `.app` swapped in at the recorded path is launched by `open` | CONFIRMED (major) | openPathArgv: `open` / `rundll32 url.dll,FileProtocolHandler` / `xdg-open`; only provenance pinned | XC33, XC3 |
| L6-5 | 6 | P19, §4.16, §4.13, §8.2 (OMISSION) | Delete the marker + plant a manifest → "Complete"; retry ingests injected ids | CONFIRMED (major) | "marker is absent and the manifest has a status → that status, recovered"; no read-back row in §8.2 | XC4, XC2 |
| L6-6 | 6 | P4, §4.4, §4.5 | M365 folder ids from the page are unvalidated; well-known names and `..` reach Recoverable Items or the whole mailbox | CONFIRMED (minor) | PathEscape keeps `.`/`..`; Recoverable Items is a §3 non-goal | XC17 |
| L6-7 | 6 | §4.6 step 1, §8.4 insider | Hard links pass the Lstat walk and `os.Root`; the token cache exported as an "original" | CONFIRMED (minor) | extractInspect has no nlink check; "os.Root closes the resolve-then-open race" over-claims | XC19 |
| L6-8 | 6 | §4.7, §4.8, §4.9, §8.5, MA-439/444 | No posture shown; iCloud Desktop & Documents missed; predictable root name; check-by-path then mkdir-by-path | CONFIRMED (minor) | "the export inherits the destination's ACL"; UnderCloudSync matches segments; "MailExport-<local ts>" | XC32, OR1 |
| L6-9 | 6 | §4.17, W13 | Two unlocked load-modify-save writers write back a stale Out | CONFIRMED (minor) | saveSettings has no lock; export start is a second full rewrite | XC34 |
| L7-1 | 7 | §4.13 destination rows, §5.2, MA-427 | Windows disk-full and device-removed codes do not match ENOSPC; the run downloads everything and writes nothing | CONFIRMED (major) | zerrors_windows.go: ENOSPC is an invented APPLICATION_ERROR; Errno.Is maps no disk-full code; no write breaker | XC10 |
| L7-2 | 7 | §4.13 Retry + Finalization, §7.2, R-g | As L3-5, plus filling the volume that holds the journal | CONFIRMED (major) | §4.7 reads free bytes but states no threshold | XC11, XC1 |
| L7-3 | 7 | §4.13 cancel row, P13, §7.2 | As L3-3, plus an RMM MSI upgrade kills in-flight exports | CONFIRMED (major) | main.go:112 ctx unused; Product.wxs MajorUpgrade | XC6 |
| L7-4 | 7 | §4.3, §4.16, §5.2 | As L3-2, plus a wrong-account workspace sign-in retargets the scheduled backup | CONFIRMED (major) | §5.2 "adds no new token copy"; capture passes no Mailboxes; device mode adopts the signer | XC7 |
| L7-5 | 7 | §5.1 W7–W10, P14, §4.16, §7.5 | As L3-9 | CONFIRMED (major) | §5.1 W8/W9 vs §4.16 | XC5 |
| L7-6 | 7 | §4.16, §4.2, P19 | As L1-14 | CONFIRMED (major) | Synchronous stats per start-only run | XC4 |
| L7-7 | 7 | §5.1 W2/W3, §4.9, §4.16 | Path-only binding reports another export as this run; journal-write failure unspecified | CONFIRMED (minor) | W2 precedes W3; the marker carries no run id | XC3 |
| L7-8 | 7 | §4.13 Retry, P4/R-j2, §4.7 | As L1-2, plus no integrity check on the trusted file | CONFIRMED (major) | Neither an exemption nor a check is stated | XC2 |
| L7-9 | 7 | §4.13 401/403, breaker | A transient 401 stops a run; an item-scoped 403 repeats; a 5xx storm grinds | CONFIRMED (minor; finder major) | Single-response run-fatal; breaker counts no-status errors only; in-flight item's fate unstated | XC9 |
| L7-10 | 7 | P18/§4.8, §5.2 picker | The in-process IFileOpenDialog loads shell extensions; a crash kills a running export; a hang wedges Browse | CONFIRMED (minor; finder major) | zenity file_windows.go:195-196 CLSCTX_ALL; §5.2 row covers only "unavailable" | XC31, OR8 |
| L7-11 | 7 | §1, §4.9 Modes, §4.7, §5.2 | FAT entry cap; EACCES at root creation unrefused; a fake-capacity stick reads "Complete"; chmod EPERM on vfat | CONFIRMED (major) | (a), (c), (d) confirmed; (b) speculative — an O_CREATE mode needs no chmod | XC12, XC14 |
| L7-12 | 7 | §4.13 archive row, §5.2 | An archive dropping out mid-run yields thousands of false "damaged" reports | CONFIRMED (minor) | R-o checked at preview only; the only class is "terminal until the archive is repaired" | XC10 |
| L7-13 | 7 | §4.3, §4.4, §7.6 | Cursor expiry, LRU eviction of picks and restart under an open tab have no refusal | CONFIRMED (minor) | Caps stated; no refusal ids; resolution's effect on the seen-set unstated | XC18, XC17 |
| L7-14 | 7 | §4.16 synthetic end, P9, §5.1 W9–W11 | A recovered run's fingerprint could be recomputed from an altered folder | CONFIRMED (minor) | W11 "reconcile recovers it from the folder"; fingerprint source unspecified | XC4 |
| L8-1 | 8 | §6 Rollback, Q6 | The only rollback is a downgrade the MSI blocks, and it removes E0 too | CONFIRMED (minor; finder major) | Product.wxs:37-39 DowngradeErrorMessage; Q6 open | XC37, OR6 |
| L8-2 | 8 | §4.5 pin, §10 E1, MA-443 | Unnormalized pin comparison could fail every scheduled capture on a harmless nextLink variant | CONFIRMED (minor; finder major) | No normalization rule; MA-443 does not record nextLink shape | XC25 |
| L8-3 | 8 | P15, §4.3 | As L1-8; the failed save is only a logged warning | CONFIRMED (minor; finder major) | run.go "could not save manifest"; CI does run windows-latest | XC20 |
| L8-4 | 8 | §6, §4.3, §4.4, §7.4 | On a pre-v3 archive, index keys never match the snapshot's migrated keys; OpenReadonly lacks the newer-version refusal | CONFIRMED (minor; finder major) | manifest.go:563-592 re-keys in memory; RepairKeys only on write paths; fails closed | XC19 |
| L8-5 | 8 | §4.6 step 3 | v1 archives' `UnknownSentinel` becomes false recipient-facing notes | CONFIRMED (minor) | manifest.go:546-553 `Missing=["unknown"]` | XC19 |
| L8-6 | 8 | §6, §4.13, §4.16 | As L3-14, plus compaction by an older binary drops newer lines | CONFIRMED (minor) | No reader or compaction version rule | XC3 |
| L8-7 | 8 | §4.16 journal location | Roaming profiles share one journal across RDS/VDI hosts | CONFIRMED (minor) | os.UserConfigDir → %AppData% Roaming; no host scoping | XC3 |
| L8-8 | 8 | §4.11 AttachDirName | Archive renders gain the false sentence "attachments are inside the .eml file" | CONFIRMED (minor) | Every archive render passes AttachDirName "" (exporter.go:311-327) | XC30 |
| L8-9 | 8 | §4.14 | The run-beside-capture claim depends on unfixed F17; a long export can fail a backup | CONFIRMED (minor) | getJSON wraps the retry loop in a 60 s timeout | XC30, XC38 |
| L8-10 | 8 | §6, §10 E0, Q5 | E0's cutover (bookmarks, RMM verification step, reader links) is undescribed | CONFIRMED (minor) | README-RMM.md:155 "Open http://127.0.0.1:8097/" | XC24 |
| L9-1 | 9 | §4.13, §4.15 | A 503 outage grinds ≥25 s per message for days; 502/504 "finish" with everything failed | CONFIRMED (major) | get retries only 429/503; 502/504 never retried; fault-isolation counts 5xx/idle as transport | XC9 |
| L9-2 | 9 | §4.13 401/403, §7.2, R-c/R-u | A dead refresh token surfaces as a transport error → R-u "check your network" | CONFIRMED (major) | The oauth2 error returns before any Graph request, with no HTTP status | XC9 |
| L9-3 | 9 | §4.3, §4.18 F15, §10 E0 | (a) Context lifetime; (b) a stalled refresh holds deviceSource's mutex and blocks every endpoint and the run | CONFIRMED (major) | delegated.go holds `s.mu` across `s.cur.Token()`; transport bounds only headers; F15 "recommended" | XC6, XC26 |
| L9-4 | 9 | §4.16, §4.7, §4.13, §3 | A dead network destination freezes activity; SMB loss is per-message; a hard NFS mount wedges the slot | CONFIRMED (major) | Per-request reconcile; destination-fatal set is ENOSPC/EROFS/root gone | XC4, XC10 (PARTIAL: kernel-blocked write stated as a limit) |
| L9-5 | 9 | §4.8, §5.2, §6 | As L7-10 | CONFIRMED (minor; finder major) | `go list -deps` shows no zenity today | XC31, OR8 |
| L9-6 | 9 | §4.5 (E1) | The Graph layer is designed twice (fault-isolation S3's `StatusError{Code, GraphCode, Msg}`); MA-402 conflicts; capture "unchanged" is false | CONFIRMED (major) | design-fault-isolation.md:593, :680; S3 at :1033 | XC25 |
| L9-7 | 9 | §10 E0, §4.18, P7, §10 | E0's owner uncited; F15/F17/F75/F91/F111 owned by nobody; "can land at any time" contradicts fault-isolation N1 | CONFIRMED (minor; finder major) | fault-isolation §3 N1 "owned elsewhere"; path portability "needs its own migration" | XC23, XC26, XC38, OR9 |
| L9-8 | 9 | §4.3, P16 | Session caches of ~100–150 MB kept for weeks; no byte bound | CONFIRMED (minor) | LRU 250,000 metadata entries; MA-432 measures run heap only | XC18 |
| L9-9 | 9 | §4.9 temp, §5.1 W5, MA-432 | 120,000+ F_FULLFSYNCs per macOS CI push; directory syncs dominate on USB | CONFIRMED (minor) | MA-432 is tier U on every OS; no sync seam | XC5 |
| L9-10 | 9 | §8.3 (OMISSION: supply chain) | govulncheck analyses GOOS=linux only; newly reachable Windows zenity code is never scanned | CONFIRMED (minor) | ci.yml `if: matrix.os == 'ubuntu-latest'` | XC31 |
| L9-11 | 9 | §4.5 MIMETo | The 60 s ResponseHeaderTimeout still bounds time-to-first-byte; big conversions fail every retry | CONFIRMED (minor) | graph.go:136-141 ResponseHeaderTimeout = reqTimeout | XC26 |
| L10-1 | 10 | header, §2 R22/S42/X12 | The proposed ids are claimed by four sibling designs with other meanings | CONFIRMED (major) | R22 in fault-isolation:228, recapture-integrity:190, mime-fidelity:815, local-confidentiality:153; X12 in local-confidentiality | XC29, OR2 |
| L10-2 | 10 | OW4, §1.2 | Two designs claim one owner decision for incompatible engines | CONFIRMED (minor; partly refuted) | fault-isolation.md:11-15 "That flow drives the same engine (app.Run / app.RunGraph)"; OW4 quotes the owner accurately | XC39, OR3 |
| L10-3 | 10 | §10 E3 vs §7.6, P12, §7.2, MA-445 | As L5-2; proofs carry no slice tag | CONFIRMED (major) | "Proven (MA-x)" without a slice | XC28 |
| L10-4 | 10 | P14 vs §5.1 | P14 has no inline condition; FAT/exFAT omitted; MA-429 proves ordering, not durability | CONFIRMED (minor; finder major) | "There is never a marker-less, incomplete export. Proven (MA-429)." | XC5 |
| L10-5 | 10 | P6, P7, §4.9 | FoldKey ≠ the NTFS/exFAT upcase table (ı/I); 8.3 aliases; rename silently replaces | CONFIRMED (minor; finder major) | at_windows.go:460 FILE_RENAME_REPLACE_IF_EXISTS | XC13 |
| L10-6 | 10 | §4.11 `LiveRefreshIn`, P10, P20 | `hostileSurvives` flags the tool's own charset and CSP metas, so every archived page would be quarantined | CONFIRMED (minor; finder major) | html.go:233-238 hostileEquiv; metaIsCharset; inertMetas at html.go:34-43 | XC30 |
| L10-7 | 10 | P5, §4.7, MA-412 | As L1-9 | CONFIRMED (minor) | MA-412's fake never forces a refresh | XC30 |
| L10-8 | 10 | P9, MA-422, MA-442 | The PowerShell verifier is only lab-pending but sits beside "proven"; MA-422 unspecified without sha256sum | CONFIRMED (minor) | MA-422 covers coreutils only; MA-442 "pending" | XC14, XC28 |
| L10-9 | 10 | §8.3, §6, §4.8 | zenity is not in the desktop binary yet; zenity finds qarma/zenity/matedialog, not kdialog | CONFIRMED (minor) | zenutil/run_unix.go:21 | XC31 |
| L10-10 | 10 | P15, P16, MA-419 | Conditions missing beside the claims: a full capture can fail messages; HTML mode buffers whole messages | CONFIRMED (minor) | §4.14 and §4.15 concede both | XC30 |
| L10-11 | 10 | §4.18 F17 row | R-v legibility depends on the F17 fix the same row calls a dependency | CONFIRMED (minor) | 60 s WithTimeout around the retry loop; untyped exhausted-retries error | XC26 |
| L10-12 | 10 | evidence citations | Several file:line citations point at the wrong code | CONFIRMED (minor) | RenderWith html.go:115; hostileSurvives :250 `(head, body string)`; stemFor exporter.go:515; baseNameN :760; captureState capture.go:56 | XC30 |
| L10-13 | 10 | §4.14, §4.15 | "Observed" throughput is uncited; 4–5 concurrent streams per mailbox will draw 429s | CONFIRMED (minor) | "At an observed ~2–5 messages/s … aspirational until measured" | XC30 |
| L10-14 | 10 | §5.2 Graph session row | The in-memory refresh token re-creates a sign-in removed outside the dashboard | CONFIRMED (minor) | putIfNewer falls through to put on a load error | XC7 |
| L10-15 | 10 | §4.9 Modes, §3, §8.5 | 0600 and the cloud-sync warning are stated without conditions | CONFIRMED (minor) | Mount options; UnderCloudSync misses `iCloudDrive`, "Dropbox (Company)" | XC12, XC32 |
| L10-16 | 10 | P1, MA-436 | As L3-17 | CONFIRMED (minor) | — | XC20 |

## Mechanical check

`docs/design-extraction-workspace.md` revision 2 has BUILD STATUS "approved with
conditions, not built" and cites this review, which exists. Each slice flips its own status
and cites its gate artifact as it lands. Before E2 code: XC40's lens 3/6/7 addendum. After
E3: the adversarial pass (`docs/review-extraction-workspace-adversarial.md`) and the
friction review (`docs/review-extraction-workspace-friction.md`).

## Addendum: RR-2 (2026-10-09), lenses 3, 6 and 7 on revision 2, with ruling J3 and the W1 second archive source (RR-2 and RR-3 folded together)

**Design reviewed:** `docs/design-extraction-workspace.md`, revision 2 (2026-10-09), with
the owner rulings folded at `aed7e5b` (`docs/owner-rulings-2026-10-09.md`). Three parts were
in scope:
- **(a) Revision 2 itself.** XC1 moved where the app keeps its truth (the run record,
  §4.19), which is a material change. With it, the engine E2: the plan, the run record,
  the `os.Root` writer with no-replace placement, naming with at most 1,000 files per
  directory and ruling Q15's export-only stricter naming pass, the marker, README, manifest
  and sums built from the run record, outcomes, failure classes with backoff and the time
  breaker, per-OS destination classes and the write breaker, cancel, and the Microsoft 365
  adapter for originals.
- **(b) Ruling J3.** On Windows, hand-over copies inherit the destination's ACL, and the
  preview names the posture with local-confidentiality's `Posture(dir)` before the first
  write. POSIX modes stay 0700/0600. Archive roots and the tool's own state directories
  keep the protected DACL.
- **(c) Ruling W1, the owner's "yes" to Q3.** The read-only "another archive folder"
  source: every place marked `[ruling W1]` (§3, §4.1, §4.3, §4.7, §7.4, §7.6 R-o3, §9
  MA-531, §10 E4).

This is RR-2 (the design header's XC40) and RR-3 (lenses 3, 6 and 7 on the W1 text before
E4 code), folded into one re-run as the design allows. Not reopened: OW1–OW4, Q3's yes
(owner-direct), and the earlier owner rulings in the rulings log's §0. SG, the shared Graph
primitive, is reviewed separately in RR-6; its definition in ruling J4 was taken as given.

**Procedure:** the battery of `assurance-kit/process/design-review.md`, re-run on three
lenses as a workflow on 2026-10-09: finder → skeptic → rescue, then this filing.
- **Finders:** lens 3; lens 6 (a), ruling J3 and the write side; lens 6 (b), the W1 source;
  lens 7.
- **Skeptic:** one refutation-default reviewer checked every finding against the design
  text and the code.
- **Rescue:** a separate reviewer took every finding the skeptic left at major and was told
  to save the design with modest changes. It also surfaced six findings of its own
  (RR2-H1..RR2-H6).
- **Clerk:** this filing. The rescue's own findings were not yet verified, so the clerk
  re-read the cited text and code for each before making it a condition.

**Lenses run:** 3 durability · 6 security / adversarial · 7 failure domains /
recoverability.

**Findings: 33 found by the finders · 26 confirmed · 7 rejected** (1 refuted, L6a-2;
6 duplicates, L7-1, L7-3, L7-4, L7-9, L7-11 and L7-14). After the skeptic: 0 blocker,
8 major, 18 minor. L3-1, the only finding a finder rated blocker, was re-graded major;
L3-5, L3-7, L6b-1 and L7-5 were re-graded from major to minor. The rescue handled all 8
majors: 8 DISSOLVED by a named change, 0 PARTIAL, 0 SURVIVES. **Its six findings are all
confirmed** by the clerk, and each is dissolved by its named change.

**Verdict: GO_WITH_CONDITIONS.** Conditions **RR2-C1..RR2-C14** must be fixed before code.
They are folded into the design as tracked changes, each marked `[RR-2 RR2-Cn]` where it
lands, with a condition map in its header, for re-verification as step 4 asks. No lens
fails, because no blocker survives the rescue. No condition needs an owner ruling. One
delegated ruling is amended in its mechanism, not in its decision: J3 (RR2-C6). One new
test row: **MA-532**, from the reserve the rulings allocated for RR-2 and RR-3.

The conditions, in short:
- **RR2-C1** — the sealed line carries the run's outcome, and Check and finish acts on it
  (L3-1).
- **RR2-C2** — one seal-and-finish step for every finishing path (L3-6).
- **RR2-C3** — a folder that is absent or slow at launch is looked for again (L3-2,
  L7-2 (b)).
- **RR2-C4** — a root's identity survives a remount (L3-3).
- **RR2-C5** — free space is checked before every message, against a reserve sized from
  the plan (L3-4).
- **RR2-C6** — the posture line names who will be able to read the export, and the run
  checks it on the created root (L6a-1; amends J3's mechanism).
- **RR2-C7** — every unsealed run can be finished, and its remaining messages exported
  (L7-2).
- **RR2-C8** — the no-progress breaker counts awake time only (L7-6).
- **RR2-C9** — an archived page keeps `.html` only with the renderer's exact prefix, from E4
  on (RR2-H1).
- **RR2-C10** — final placement renames relative to the directory handle (RR2-H2).
- **RR2-C11** — another archive folder's manifest is read under a cap, and its `search.db`
  is opened defensively (RR2-H3).
- **RR2-C12** — run records are found without the journal (RR2-H4).
- **RR2-C13** — R-f compares identities as well as paths (RR2-H5).
- **RR2-C14** — an export from another archive folder saves no setting (RR2-H6).

### The rescue's six findings, verified by the clerk

The rescue surfaced six findings that no lens named. The clerk re-read the cited text and
code for each. All six hold: the design as written permits each failure.

- **RR2-H1 — another archive folder's pages would be exported as live `.html`.** §4.6
  step 2 copies a no-original record's `.html` "byte-exact under the same gate and fixity
  check", and for another archive folder that fixity comes from the folder's own manifest.
  The only further check is `LiveRefreshIn`, which §4.11 defines as reporting "any hostile
  meta other than the tool's own leading metas". The renderer's detector looks only at
  `<meta>` elements (`hostileIn`, `internal/export/html.go:260-285`). The renderer relies on
  its leading CSP meta, not on removing scripts: "the policy meta that precedes everything
  makes them inert" (`html.go:153-154`). So a page with a `<script>` and no CSP prefix
  passes, and README calls `.html` "offline-safe" (§7.5), against R19 ("its `<head>` begins
  with a Content-Security-Policy meta"). §10 also builds `LiveRefreshIn` and the quarantine
  path in E5, while E4 already copies pages (MA-420), so between E4 and E5 even a pre-F1
  page with a live refresh is exported as `.html`. Major. → RR2-C9.
- **RR2-H2 — final placement is path-based on macOS and Windows.** §4.9 names
  `renamex_np(RENAME_EXCL)` on macOS, `MoveFileEx` on Windows and "link + unlink" as the
  fallback. Each resolves a full path when it is called, against P6 ("no write can escape
  it by .., an absolute path or a symlink") and §4.9 itself ("every later create, mkdir,
  rename and remove goes through that root"). Under J3 a shared destination's inherited ACL
  can give another account write access inside the root. That account can swap a month
  directory for a junction between the temp file's creation and the rename. MA-417's hook
  plants a symlink before the temp file exists, so it cannot see this. Major. → RR2-C10.
- **RR2-H3 — another archive folder's manifest and `search.db` are read with no bound and
  with SQLite's trusting defaults.** `state.Load` reads the whole file with `os.ReadFile`
  (`internal/state/manifest.go:514`) and unmarshals it (`:524`) before the newer-version
  refusal (`:535`), yet §4.3 claims "bounded reads" for the folder. §4.3 opens `search.db`
  "only with the URI parameters `mode=ro&immutable=1`", with none of the settings SQLite's
  own guidance gives for an untrusted database file. The unbounded load is L6b-1, which the
  skeptic graded minor: a crash of the local dashboard from a folder the user chose, with
  nothing disclosed. The repo registers no SQL functions of its own, so `trusted_schema`
  guards against crafted views and triggers, not against side effects. Minor in impact, but
  W1's "bounded reads" is false as written. → RR2-C11.
- **RR2-H4 — losing the journal strands every unfinished run.** §4.16 reconciles "each run
  whose last event is start or sealed", history is "the cached journal view" (§4.2), and a
  record is deleted only "when its run and its whole retry chain are complete, or when
  journal compaction drops the run" (§4.19). Nothing lists `<state dir>/exports/*.jsonl`.
  A deleted or damaged journal therefore hides every unfinished run's Finish as partial and
  retry, and leaves the records for good. Yet §4.16 says "Exports are unaffected" and §5.2
  says "history empty". This is the general case of L3-9. Major. → RR2-C12.
- **RR2-H5 — R-f for another archive folder compares paths, so an alias lets the export be
  written inside it.** §4.7 step 3 refuses a destination "equal to or inside the other
  archive folder" through `destOverlapsOut`. That function resolves symlinks
  (`resolveExisting`, which calls `filepath.EvalSymlinks`) and then compares with
  `filepath.Rel` (`internal/app/extract.go:618-660`). A mapped drive beside its UNC path, a
  `subst` letter, a `\\localhost\D$` share or a bind mount is not a symlink, so it passes.
  The export root, its temp files and its marker are then written inside the folder where
  §4.3 says "Nothing is ever written there", the owner-direct part of Q3. A NAS archive
  reached both as `Z:\` and as `\\nas\share` is a realistic case. Major. → RR2-C13.
- **RR2-H6 — MA-531's "Settings unchanged" contradicts §4.17.** §4.17 says the four export
  fields "are saved when an export starts", W1 runs included. MA-531 asserts "Settings and
  the installed weekly backup unchanged", and §4.3 says "It never touches Settings or the
  weekly backup". As written, E4 either fails its own row or must narrow it, which the hard
  limit sends to the owner. Minor in impact (nothing is written in the folder, and `Out`
  does not change), but the design cannot pass its own row. → RR2-C14.

All six become conditions, as rescue-surfaced blockers dissolved by a named change. That
includes RR2-H3 and RR2-H6, whose impact is minor, because each makes a stated W1 property
false as written and its fix only strengthens a check.

### Conditions (must-fix-before-code; folded into the design)

- **RR2-C1 — The sealed line carries the run's outcome, and Check and finish acts on it.**
  (L3-1; lands with RR2-C2.)
  - **Sealed line.** The run record's `sealed` line carries the final status (complete |
    partial | cancelled | failed | finished-as-partial), the counts (selected, exported,
    failed, not_attempted, without_original) and the fingerprint (§4.13 step 6, §4.19).
  - **Labels.** A marker-absent sealed run is labelled from that status: "Complete
    (recovered)"; "Finished with problems — N not exported (recovered)", with "Export
    failed messages again"; "Cancelled" or "Stopped: <reason>". A marker-present sealed run
    shows its status and offers Check and finish (§4.16).
  - **Check and finish** verifies as before, then performs that status's marker action:
    remove the marker for complete or partial; rewrite it CANCELLED / STOPPED for
    cancelled, failed or finished-as-partial. It never removes a cancelled or failed run's
    marker.
  - **P6's exception** names the marker rewrite: one atomic replacing rename of the run's
    own marker through the root (a temp file, fsync, `os.Root.Rename` over it), never a
    remove followed by a placement, so the folder is never without a marker. §3 and P14
    say so.
  - **Rows:** MA-429 kills a cancelled run and a partial run at W9a and between W10 and
    W11; MA-520 asserts that Check and finish on a sealed cancelled run rewrites the marker
    and never removes it; MA-430 asserts that a marker-absent sealed partial run is never
    labelled complete.

  *Closes:* L3-1, L3-7, L7-1.
- **RR2-C2 — One seal-and-finish step for every finishing path.** (L3-6.)
  - **One step.** Normal finalization (after SUMS), Finish as partial (after it writes
    README, manifest and SUMS from the record) and Check and finish (after its
    verification) all end with it. Each part is idempotent: (1) the sealed line (RR2-C1),
    fsynced, skipped if present; (2) the fingerprint-ledger line, once per run id; (3) the
    sealed status's marker action, then a directory sync; (4) the journal `end`.
  - **Recovery.** Reconcile appends a missing ledger line from any sealed record, then an
    `end`. A run record is deleted only once its ledger line exists.
  - **Set fingerprint.** It is computed at each part's seal by resuming the SHA-256 state
    that the previous part's ledger line saved, with this part's in-memory SUMS bytes.
    Every ledger line saves its state and the set fingerprint. Nothing is re-read from
    disk, and no earlier run record is needed.
  - **Consistency.** The journal has no `sealed` event: the run record's sealed line is the
    seal, and §4.16's event list, §4.19's seal sentence and §5.1 now match §4.13. §4.2's
    `/api/exports/finish` lists the sealed line, the ledger and the journal among its side
    effects. §5.1 gains W9b (the ledger line), and W11's "killed before" reads "sealed; the
    ledger line or the `end` missing → reconcile appends them".
  - **Rows:** MA-437 (Finish as partial leaves a ledger fingerprint equal to sha256 of the
    SUMS it wrote, and an `end`); MA-447 (a kill between W9a and W9b, and one between W10
    and W11, each leave one ledger line after reconcile); MA-521 (the set fingerprint is
    right after part 1's record has been deleted).

  *Closes:* L3-6, L3-10.
- **RR2-C3 — A folder that is absent or slow at launch is looked for again.** (L3-2; folds
  L7-2 (b).)
  - **Terminal outcomes.** Only the marker-and-seal outcomes are terminal `reconciled`
    records. "Folder not found", "did not answer within 5 s" and "couldn't confirm this is
    the same folder" (RR2-C4) are observations: cached, never journaled.
  - **Looking again.** Each run that has a run record and no terminal outcome is probed once
    per launch (5 s bound, in the background) and by a guarded "Look again" on its card.
  - **The user's answers.** "This folder is gone" makes the outcome terminal and offers
    "Export these N messages again" (RR2-C7). "Locate…" (Browse) accepts the folder at a new
    path, after a drive-letter or mount change, only when the identity check passes.
  - **Wedged runs.** The journal mark is `stopped`, never an `end`. Once the goroutine has
    returned, or after a restart, the run is reconciled like any unsealed run, and Finish as
    partial is offered.
  - **Surface.** A new endpoint, `/api/exports/locate` (§4.2); §8.3 now counts nineteen.
  - **Rows:** MA-448 (absent at the first launch and present at the second → interrupted,
    with Finish as partial offered; a timed-out probe is recorded as a timeout and probed
    again).

  *Closes:* L3-2, L7-2 (b).
- **RR2-C4 — A root's identity survives a remount.** (L3-3.)
  - **The tuple (§4.9).** (1) The content nonce: the run id, which the marker carries and
    README's first line repeats, matched against the record. It is read only to match,
    never to admit. (2) Windows: volume serial + file ID, as before. (3) POSIX: dev+inode
    is compared only on a filesystem type that keeps stable inode numbers (not vfat, exfat
    or msdos), and only while dev is unchanged since the record; then it must match. After
    a dev change (a replug, a remount, a reboot) the nonce decides.
  - **Outcomes.** R-r is kept for a positive mismatch: another run id, a non-directory, a
    symlink or reparse point, a Windows identity mismatch, or a new inode on the same mount
    of a stable-inode filesystem. Anything else is "couldn't confirm — Look again"
    (RR2-C3). A W2–W4 root with no nonce yet, empty or temp-only, reconciles as interrupted
    ("empty export folder", offering Finish as partial; P14, MA-429).
  - **The W1 binding.** The other archive folder is bound by its path and, per message, by
    the fixity in the plan, never by dev+inode. A selection whose folder is no longer at
    that path is refused with the new R-l2, because R-l is worded for a signer (L7-3).
  - **Lens 6, weighed by the rescue.** On POSIX-mode filesystems the nonce sits in a 0600
    file inside a 0700 root, so another account cannot read it. On FAT, exFAT and SMB no
    identity is unforgeable anyway. Integrity never rests on identity, because SUMS come
    from recorded hashes and the fingerprint from memory. Windows keeps its file-ID check
    beside the nonce.
  - **Rows:** MA-430 and MA-448 add a fake filesystem whose dev and inode change between
    launches with the nonce intact (Finish as partial is offered, Open folder opens), and
    one with a different run id (R-r); MA-512 adds R-l2.

  *Closes:* L3-3, L3-11, L7-3.
- **RR2-C5 — Free space is checked before every message, against a reserve sized from the
  plan.** (L3-4.)
  - **Before each message.** Free bytes are read on the destination and on the state volume
    (`statfs` / `GetDiskFreeSpaceEx`, `lpFreeBytesAvailableToCaller`, which respects
    quotas). The destination budget is free − reserve. `MIMETo` gets min(200 MiB cap,
    budget) as `maxBytes`, an archive copy's size is checked against it, and derived files
    are checked against what is left. Passing the budget aborts that message (temp removed),
    lists it as the in-flight failure and stops the run with R-g; passing the cap stays the
    retryable class. The state volume must hold the run record's remaining bound, or the
    run stops with R-d2 (RR2-C7).
  - **The reserve** is computed from the plan at preview: per message, the bound of its
    JSON-escaped manifest entry plus one SUMS line per expected file (two with Readable
    HTML); plus README's bound and one allocation cluster per finalization file and its
    temp. R-g2 and the preview use it.
  - **Restated claims.** §4.13 and §7.2 now say finalization fits against this run's own
    writes. Another program or a quota can still use the reserve; the marker then stays,
    and Finish as partial (RR2-C7) completes the part once space is freed. That is a
    recoverable state with a named remedy, not an accepted gap.
  - **Rows:** MA-518 (150-character ids with Readable HTML on → a reserve above 1 KB per
    message); MA-427 (a volume a few MB above the reserve with a large message → that
    message is aborted at its budget, and finalization fits).

  *Closes:* L3-4, and the floor half of L3-8.
- **RR2-C6 — The posture line names who will be able to read the export, and the run
  checks it on the created root.** (L6a-1; folds L7-13; amends ruling J3's mechanism.)
  - **Windows.** The destination's security descriptor is run through Windows' inheritance
    algorithm (`CreatePrivateObjectSecurityEx`, a lazy advapi32 call like
    local-confidentiality's `GetEffectiveRightsFromAcl`), once for the export root and once
    for a file below it. A hand-over form of `Posture` names every principal other than the
    user, SYSTEM and Administrators that gets read access, not only the broad SIDs.
  - **Start** recomputes the line beside its free-space and identity re-checks, and refuses
    with the new R-w2 if it is broader than the preview's.
  - **The realized check.** After W2 and W4 the realized DACLs of the root and the marker are
    read. If they grant read to anyone the line did not name, the run stops before W5 with
    R-w2: nothing is exported, and the marker is rewritten STOPPED. On an SMB share, where
    the server computes inheritance, the preview says its prediction may differ, and Start
    shows the realized line for confirmation before W5. The realized posture is recorded in
    the run record.
  - **macOS.** Inheritable ACL entries on the destination, and inherited entries on the
    created root, are read and named.
  - **Can't tell (L7-13).** Where the descriptor or ACL cannot be read, the line is a WARN,
    "couldn't check who can read this folder", never "only you can read this folder".
  - **J3 stands:** inherit; POSIX 0700/0600; the protected DACL only for archive roots and
    the tool's own state directories.
  - **Rows:** new **MA-532** (U, Windows and macOS legs; slice E2); MA-438 adds R-w2.

  *Closes:* L6a-1, L7-13.
- **RR2-C7 — Every unsealed run can be finished, and its remaining messages exported.**
  (L7-2.)
  - **Finish as partial** is offered for every unsealed run whose root is found and
    confirmed: killed, finalization failed (a pulled stick plugged back in), panicked, or
    wedged once its goroutine has returned. It re-hashes each present recorded file against
    its recorded hash before listing it. A mismatch is a listed failure ("damaged when the
    computer stopped"), never a SUMS line. It ends with RR2-C2's step, status
    finished-as-partial.
  - **"Export the remaining messages"** is offered from the run record for every unsealed or
    stopped run, interrupted runs included, so §4.13 and §7.2 agree.
  - **A part marked gone** (RR2-C3): `rest` is the plan minus the messages in sealed parts of
    its chain, so its done but never-finalized messages are exported again.
  - **The state directory** is a stated run-fatal class. A run-record or journal append or
    fsync that fails during a run stops it with the new R-d2, and is never matched against
    the destination table. The run is finalized if the state volume takes the sealed line;
    otherwise the marker stays and Finish as partial completes it later.
  - **Rows:** MA-427 (ENOSPC → the stick is removed → finalization fails → it is re-inserted
    → Finish as partial is offered, and the part verifies); MA-437 (`rest` for a part marked
    gone includes its done messages; a changed file is listed damaged, never in SUMS);
    MA-438 adds R-d2.

  *Closes:* L7-2, and the class half of L3-8.
- **RR2-C8 — The no-progress breaker counts awake time only.** (L7-6.)
  - **The breaker.** No progress for 5 minutes of awake time → R-u. Progress is a 2xx
    response or response-body bytes received, so a slow, healthy download of a large message
    never trips it.
  - **Awake time** is measured by a 1-second ticker in the run. A gap above 30 s counts as a
    suspend, is not added, and restarts the window.
  - **Power request.** On Windows an active run holds `ES_SYSTEM_REQUIRED | ES_CONTINUOUS`
    (`SetThreadExecutionState`, in-process, from a goroutine locked to its OS thread). macOS
    and Linux use an in-process equivalent where one is available, and otherwise none.
    Nothing is shelled out, so desktop P1 stands.
  - **The walk.** §7.2 separates "lid closed or sleep: the run resumes on wake" from "app
    killed or crashed: Interrupted", and §4.15 says the same.
  - **Rows:** MA-516 (a clock-injected 60-minute suspend, then 30 s of transport errors,
    then success → zero failures; a 12-minute body download with bytes arriving → no R-u;
    a 5-minute awake outage → R-u).

  *Closes:* L7-6.
- **RR2-C9 — An archived page keeps `.html` only with the renderer's exact prefix, from E4
  on.** (RR2-H1.)
  - **The gate (§4.6 step 2, §4.11, P10).** An archived page (archive source, no `.eml`) is
    exported as `<stem>.html` only if its bytes begin with the renderer's exact prefix
    (doctype, `<html lang="en">`, `<head>`, the charset meta, then `inertMetas`;
    `html.go:34-42`) and `LiveRefreshIn` finds no other hostile meta. Otherwise it is
    copied as `<stem>.archived-page.txt` with a note, under the same fixity check. The
    preview counts these pages.
  - **The slice.** The gate, `LiveRefreshIn` and the quarantine path move from E5 into E4,
    the slice that copies pages. E5 keeps Readable HTML.
  - **Rows:** MA-420 and MA-531 add a page with `<script>` and no CSP prefix (→
    `.archived-page.txt`) and a current-renderer page (stays `.html`). Prove-fail: accept
    any page without a hostile meta → the script page is exported as `.html`.

  *Closes:* RR2-H1.
- **RR2-C10 — Final placement renames relative to the containing directory's handle.**
  (RR2-H2.)
  - **§4.9.** `renameat2(dirfd, tmp, dirfd, name, RENAME_NOREPLACE)` on Linux;
    `renameatx_np(dirfd, tmp, dirfd, name, RENAME_EXCL)` on macOS; on Windows the
    handle-relative rename that Go's `os.Root.Rename` makes (`FILE_RENAME_INFORMATION_EX`
    with `RootDirectory`), without its replace flag. The fallback is `linkat` with the same
    handles (`os.Root.Link`), then removing the temp file. The path-based `renamex_np` and
    `MoveFileEx` are never used.
  - **Rows:** MA-417 adds a fault hook at the rename seam that swaps the parent for a
    symlink or junction: the message fails, and nothing appears outside the root.

  *Closes:* RR2-H2.
- **RR2-C11 — Another archive folder's manifest is read under a cap, and its `search.db` is
  opened defensively.** (RR2-H3.)
  - **The manifest** is read through the root, size-checked and read through
    `io.LimitReader` against 1 GiB before anything is unmarshalled, and refused above it with
    the new R-o4.
  - **`search.db`** is opened with `mode=ro&immutable=1` plus `PRAGMA trusted_schema=OFF`,
    `cell_size_check=ON` and `mmap_size=0`. Before any query, `sqlite_master` must show
    `docs` as a table (not a view) with the columns this build's index versions define.
    Otherwise the list is unavailable, and whole-folder selection works from the manifest.
  - §8.1 #6 and §8.2's archive row say so.
  - **Rows:** MA-531 adds an over-cap manifest (R-o4, side effect absent, no unbounded
    allocation) and a `search.db` whose `docs` is a view (list disabled); MA-512 adds R-o4.

  *Closes:* RR2-H3, L6b-1, L7-14.
- **RR2-C12 — Run records are found without the journal.** (RR2-H4.)
  - **The launch sweep (§4.16).** At launch the reconciler lists
    `<state dir>/exports/*.jsonl`. It adopts any record that no journal line names
    (rebuilding its `start` line from the header, this host only) and reconciles it as
    usual. It deletes any record whose run is sealed and ledgered and whose chain is
    complete.
  - **From L3-9's fix.** R-d removes the run record it had written, so a refused start is
    never adopted. Compaction deletes the dropped runs' records only after the new journal
    is in place.
  - **§5.2's Journal row** states the loss truthfully: the next launch re-adopts every
    surviving record, and only runs whose records are gone too leave the history.
  - **Rows:** MA-430 adds: with the journal deleted, an interrupted run and a sealed run both
    reappear after the next launch, and Finish as partial is offered for the first; a record
    that no journal line names (a W3 kill) is adopted as interrupted.

  *Closes:* RR2-H4, L3-9, L7-11.
- **RR2-C13 — R-f compares identities as well as paths.** (RR2-H5.)
  - **§4.7 step 3 and Start.** The destination directory is opened, and its ancestors are
    walked up to the volume root (`openat(fd, "..")` on POSIX; on Windows each ancestor of
    `GetFinalPathNameByHandle`'s canonical path, opened and its volume serial + file ID
    read). R-f also refuses when any ancestor has the identity of the configured archive or
    of the other archive folder, which is read when the source is validated. `extract`'s
    `destOverlapsOut` is unchanged.
  - **Rows:** MA-414 (E3) adds a destination reached through an alias of the configured
    archive, and MA-531 (E4) one reached through an alias of the fixture folder: `subst` on
    the Windows CI leg, a bind mount on Linux → R-f, with nothing written.

  *Closes:* RR2-H5.
- **RR2-C14 — An export from another archive folder saves no setting.** (RR2-H6.) §4.17:
  export start saves the four export fields, except for an export whose source is another
  archive folder, which saves none. §4.3's "It never touches Settings" is then true, and
  MA-531's "Settings and the installed weekly backup unchanged" holds as written. Rewording
  MA-531 instead would narrow a row and contradict W1's delegated detail, so it is not done.
  *Closes:* RR2-H6.

### Rescue outcomes (every finding the skeptic left at major, and the rescue's own)

| Finding | Outcome | Named change (short) | Condition |
|---|---|---|---|
| L3-1 | DISSOLVED-BY | status and counts in the sealed line; labels and Check and finish act on that status; P6 names the one atomic marker rewrite | RR2-C1 |
| L3-2 | DISSOLVED-BY | only the marker-and-seal outcomes are terminal; absence and timeouts are probed again each launch and by Look again; This folder is gone; Locate… | RR2-C3 |
| L3-3 | DISSOLVED-BY | an identity tuple: the run id as a content nonce, Windows volume serial + file ID, POSIX dev+inode only on the same mount of a stable-inode filesystem; W1 bound by path and fixity | RR2-C4 |
| L3-4 | DISSOLVED-BY | free bytes read before every message; `MIMETo`'s `maxBytes` from the budget; the reserve sized from the plan; the claims restated | RR2-C5 |
| L3-6 | DISSOLVED-BY | one idempotent seal-and-finish step for all three finishing paths; reconcile appends what is missing | RR2-C2 |
| L6a-1 | DISSOLVED-BY | the posture over the inherited descriptor, per principal; recomputed at Start; realized DACLs checked after W2 and W4; macOS ACLs | RR2-C6 |
| L7-2 | DISSOLVED-BY | Finish as partial for every unsealed run, re-hashing first; the remaining messages for interrupted runs; `rest` for a gone part; a state-directory class | RR2-C7 |
| L7-6 | DISSOLVED-BY | an awake-time breaker; body bytes count as progress; a Windows power request | RR2-C8 |
| RR2-H1 | DISSOLVED-BY | the renderer's exact prefix as the archived-page gate, built in E4 | RR2-C9 |
| RR2-H2 | DISSOLVED-BY | handle-relative no-replace renames | RR2-C10 |
| RR2-H3 | DISSOLVED-BY | a 1 GiB manifest cap before unmarshalling; SQLite's settings for an untrusted file and a `docs` table check | RR2-C11 |
| RR2-H4 | DISSOLVED-BY | a launch sweep of the run records | RR2-C12 |
| RR2-H5 | DISSOLVED-BY | an identity walk of the destination's ancestors | RR2-C13 |
| RR2-H6 | DISSOLVED-BY | no settings save for an export from another archive folder | RR2-C14 |

No finding SURVIVES, and none is PARTIAL.

**What the clerk changed in the named changes.** Each change is a strengthening, never a
narrowing:
- **RR2-C2** takes the set fingerprint's source from L3-10's fix (a SHA-256 state carried in
  the never-compacted ledger). The rescue named "rebuilt from their run records", which
  fails once compaction has deleted an earlier part's record while its chain is still open
  (L3-10, confirmed).
- **RR2-C3** probes again every run that has no terminal outcome, not only the unsealed
  ones, so a sealed run whose marker state was never seen is looked for again too.
- **RR2-C4** keeps a new inode on the same mount of a stable-inode filesystem as a positive
  mismatch. The rescue's text would have accepted it on a matching nonce. No remount needs
  that, and it would have narrowed MA-430's different-inode case.
- **RR2-C5** keeps "above the 200 MiB cap" retryable when the cap, not the free-space budget,
  is what a message passes.
- **RR2-C6** names the refusal R-w2, and makes the SMB confirmation take the place of the
  stop on a share.

### Escalations (OWNER RULING items)

None. No condition weakens a gate; narrows an invariant, a catalog row's guarantee or an
X-contract line; edits a baseline; changes what an existing covering test asserts; or
accepts a known gap.
- None of the workspace's ids is in the catalog yet: `docs/scenario-catalog.md` has no R29,
  S52 or workspace MA row, and `docs/ux-contract.md` ends at X11. Every row changed here is
  a proposed row, amended in the design's own test plan.
- `extract`'s `destOverlapsOut`, `state.Load` and the renderer keep their behaviour. RR2-C11
  and RR2-C13 add the workspace's own checks beside them.
- The one restated claim (RR2-C5: finalization fits against this run's own writes) comes
  with a named remedy, Finish as partial, so it accepts no gap.
- Not reopened: OW1–OW4, Q3's yes (owner-direct) and the earlier owner rulings in the
  rulings log's §0. Desktop P1 stands, because RR2-C8 shells out nothing.

### Ruling amendments

- **Amends ruling J3** (delegated-recommended), in its mechanism, not in its decision
  (RR2-C6).
  - **The change.** For the workspace, "the preview's posture line … uses
    local-confidentiality's `Posture(dir)`" becomes: the line is computed by a hand-over
    form of `Posture` over the security descriptor a new child of the destination receives.
    It names each principal that gets read access, says "couldn't check" rather than "only
    you" when it cannot tell, and is checked again on the created root and marker before
    the first message.
  - **Unchanged.** The export inherits the destination's ACL; POSIX modes stay 0700/0600;
    the protected DACL stays scoped to archive roots and the tool's own state directories.
  - **Why.** L6a-1 (confirmed, major): `Posture(dir)` as defined reports Open only "when a
    broad SID can read the root" (`docs/design-local-confidentiality.md` §4.A). An
    inherit-only ACE, or an inheritable grant to one named account, therefore reads Private
    while the new root and its files inherit read access. The line that J3 relies on would
    say "only you" in the unsafe direction.
  - The rulings log itself is not edited here.

### Follow-ups for other owners

- **Local-confidentiality's author.** J3 also has `extract -dest` name its hand-over root's
  posture with `Posture(dir)`. L6a-1's defect reaches that use too, and RR2-C6's hand-over
  evaluation is the fix there (slice LC-A). This addendum does not change that design's
  decisions. Its re-verification corrected the sentences there that described the
  workspace's line, and states the `extract -dest` defect there as an open gap owed by
  slice A (see "Re-verification").

### Findings

Severity is the skeptic's; the finder's, where different, is in parentheses. Duplicates
cite their twin. RR2-H rows are the rescue's, verified by the clerk.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| L3-1 | 3 | §4.19 Seal; §4.13 steps 6–7; §4.16 outcomes; §5.1 W9a/W10 | The sealed line carries no status: Check and finish removes a cancelled or stopped run's marker, and a partial run killed after W10 is recovered as "complete" | CONFIRMED (major; finder blocker) | §4.19 "a sealed line carries the fingerprint"; §4.16 "marker present and a sealed record → … then removes the app's own marker; marker absent and a sealed record → complete (recovered)"; the seal (step 6) comes before the CANCELLED rewrite (step 7) | RR2-C1 (with RR2-C2) |
| L3-2 | 3 | §4.16 How it runs, Outcomes, Recording; §4.13 wedged run | A stick or share offline at login is recorded "folder not found" for good, so a killed export there is never finished; a timed-out probe is unstated; a wedged run can never be finished | CONFIRMED (major) | "Each result is appended once as a terminal reconciled record, so no run is ever probed again"; Finish as partial only from "marker present and no sealed record"; the `-no-browser` Startup launch at login (`cmd/mailarchive-desktop/main.go:43`, `:87`) | RR2-C3 |
| L3-3 | 3 | §4.9 root name; §4.13 Finish as partial; §4.16 outcomes, Open folder; §4.3 W1 binding | dev+inode does not survive a remount (POSIX `st_dev`; Linux vfat and exfat `iunique`), so Finish as partial, Check and finish, Open folder and the W1 binding refuse a replugged stick | CONFIRMED (major) | "its identity (dev+inode / volume serial + file ID) is recorded"; "identity or run id mismatch → moved or replaced", which is terminal; `lockfile.go:123-127` "Compare CONTENT, not inode/file-id" | RR2-C4 |
| L3-4 | 3 | §4.7 step 3; §4.13 free-space row, Finalization; §7.2 "USB stick full" | Writes between the every-50-messages checks eat the reserve, and 1 KB per message is less than one manifest entry plus its SUMS lines, so a full stick still fails finalization | CONFIRMED (major) | "(checked every 50 messages) … so finalization always fits"; "about 1 KB × messages + 1 MiB"; the manifest entry shape (§4.10) | RR2-C5 |
| L3-5 | 3 | §4.3 W1 `immutable=1`; §4.18 C2-3/C1-2 | `immutable=1` ignores a `-wal` that holds redaction deletes, can read a main file torn by an interrupted checkpoint or copy, and is wrong on a folder still being written; a query error after a good open is unhandled | CONFIRMED (minor; finder major) | "If a -wal sits beside search.db, immutable=1 does not read it, so the list says recent messages may be missing" is the only consequence stated; `reindex.go:154-171` commits redaction deletes with no checkpoint | note |
| L3-6 | 3 | §4.2 finish; §4.13 steps 6 and 9, Finish as partial; §4.10; §4.19 Lifetime; §5.1 W11 | Only normal finalization seals and writes the ledger: Finish as partial and Check and finish write neither, a W10–W11 kill loses the ledger line, and no step writes the journal `sealed` event | CONFIRMED (major) | `/api/exports/finish` "writes README, manifest, sums and marker in its own root only"; step 9 writes the ledger line after the marker; §4.19 "The same value goes to the journal and the fingerprint ledger" contradicts §4.13 | RR2-C2 |
| L3-7 | 3 | P6; §4.9; §4.13 step 7; §5.1 W10 | P6 forbids replacing any file, so the CANCELLED/STOPPED rewrite is either a remove-then-place window with no marker or a breach of P6 | CONFIRMED (minor; finder major) | P6 "No existing file is ever replaced … The one exception is finishing this app's own interrupted run"; §4.13 "rewrite it as CANCELLED / STOPPED"; W10 "atomic" | note; closed by RR2-C1 |
| L3-8 | 3 | §4.13 run-record line and table; §4.7 floor; §4.16 R-d | A run-record write failure mid-run has no class: a full system drive is reported as a full destination, or files go unrecorded; a torn line glues onto the next | CONFIRMED (minor) | no run-record row in the table; ERROR_DISK_FULL 112 is destination-fatal; "A failed journal or run-record write refuses the start with R-d" covers the start only; `history.go:75-85` truncates on open only | note; class closed by RR2-C7, floor by RR2-C5 |
| L3-9 | 3 | §5.1 W3, W12; §4.16; §4.19 Lifetime | Records that no journal line names (a W3 kill, R-d after the record, a W12 crash) are never found or deleted | CONFIRMED (minor) | reconcile walks "each run whose last event is start or sealed"; deletion is keyed to the chain or to compaction; nothing lists `exports/*.jsonl` | note; closed by RR2-C12 |
| L3-10 | 3 | §4.16 Set fingerprint; §4.19 Lifetime; MA-521 | The set fingerprint has no durable source once compaction deletes an earlier part's record, and it is never written to the ledger | CONFIRMED (minor) | "SHA-256 over the chain's SHA256SUMS.txt files in part order"; "no fingerprint is ever computed from disk"; the ledger holds per-run values only | note; closed by RR2-C2 |
| L3-11 | 3 | P14; §5.1 W3/W4; §4.16 | A W2–W4 root has no marker, so §4.16 yields "moved or replaced" or "changed after the export", not P14's "interrupted" | CONFIRMED (minor) | P14 "reconciled as interrupted"; §4.16 matches the run id that the marker carries | note; closed by RR2-C4 |
| L3-12 | 3 | §4.16 Lines, Reading; §4.19 Header | "This host" is undefined, and `os.Hostname`, the repo's only host identity, changes with the network on macOS | CONFIRMED (minor) | "only this host's lines are reconciled"; `os.Hostname` at `lockfile.go:62`, `health.go:485`, `descriptor.go:50` | note |
| L3-13 | 3 | §4.3 index version; §4.6 step 5; §4.13 archive row; §4.18 [W1] | For another archive folder, the remedies point at the configured archive's Rebuild and Check integrity, or at writes W1 forbids | CONFIRMED (minor) | "every gate applies unchanged" carries the remedies over; `maintenance.go:44-88` acts on `cfg.effectiveOut()`; `verify.go:125` takes `.mailarchive.lock` | note |
| L3-14 | 3 | §4.19 Version rules; §6 | Nothing makes a newer binary read older run-record or journal formats, yet recovery from an upgrade kill needs it | CONFIRMED (minor) | "A reader refuses a newer format_version … and tolerates additive fields"; the additive-only rule is stated for exports only | note |
| L3-15 | 3 | P13; §4.13 Finalization; §6; §7.2; MA-514 | Drain's fixed 15 s does not cover a finalization that grows with the run | CONFIRMED (minor) | MA-514 "returns within the 15 s budget"; finalization Lstats every file and streams the manifest | note |
| L6a-1 | 6 | §4.9 Modes; §4.7 step 3; §8.5 (J3); local-confidentiality §4.A | `Posture(dir)` judges the destination's own DACL for broad SIDs, so an inherit-only or inheritable named-account ACE reads "only you" while the export inherits read access | CONFIRMED (major) | §4.9 "built on that design's Posture(dir)"; local-confidentiality §4.A "Open when a broad SID can read the root"; Start re-checks only free space, the signer and identity | RR2-C6 (amends J3's mechanism) |
| L6a-2 | 6 | §8.5, §8.4, §4.9 (J3); §4.7 posture line | The posture line discloses read access only, not that a writable shared destination lets other accounts tamper before hand-over | REFUTED | §8.4's co-writer row: "a file altered or added after it was written is detected by the recipient's check, the extra-files check and Check this export, not prevented (stated)"; P9 conditions tamper evidence on the fingerprint; the finding itself says "not a missing integrity control" | — |
| L6b-1 | 6 | §4.3 W1; §8.1 #6; §8.2 (OMISSION: manifest size) | The untrusted folder's manifest is read and unmarshalled whole; a crafted multi-GB file kills the dashboard | CONFIRMED (minor; finder major) | `manifest.go:514` `os.ReadFile`, `:524` `json.Unmarshal`, `:535` the version check after them; §4.6's bound is per record | note; closed by RR2-C11 |
| L6b-2 | 6 | §4.3 W1 index open (OMISSION) | `search.db` is opened by raw path, so a planted symlink or FIFO is followed | CONFIRMED (minor) | `index.go:150` `sql.Open("sqlite", path)`; `extract.go:532`'s Lstat gate guards only the `.eml` | note |
| L7-1 | 7 | §4.19; §4.16; §4.13 steps 6–9 | The sealed line has no status (with Drain's budget expiring mid-finalization as the trigger) | DUPLICATE of L3-1 | the same failure; its trigger is L3-15 | RR2-C1, RR2-C2 |
| L7-2 | 7 | §4.13 Finish as partial, Retry; §4.16; §7.2 | A pulled stick leaves a part that nothing can finish; a killed run whose folder is absent at login is never finished; Retry's offer contradicts §7.2 | CONFIRMED (major) | "An interrupted (killed) run is finished …"; "If finalization itself fails, the marker stays"; Export the remaining only for "cancelled, stopped or finished as partial", against §7.2's "(or Finish as partial first)" | RR2-C7; (b) RR2-C3 |
| L7-3 | 7 | §4.9; §4.13; §4.16; §4.3 W1; R-o2 | dev+inode does not survive a remount; no refusal text exists for an archive-binding mismatch | DUPLICATE of L3-3 | the same failure; its R-l point is taken into RR2-C4 (R-l2) | RR2-C4 |
| L7-4 | 7 | §4.13 table; §4.16; §5.1 W6; §5.2 Run record row | A mid-run run-record failure has no class, and §5.2 overstates what survives losing the record | DUPLICATE of L3-8 | the same failure; the §5.2 sub-point is carried in L3-8's note | as L3-8 |
| L7-5 | 7 | §4.16 Check and finish; §4.13 steps 4–8; §5.1 W9/W9a; MA-520 | A sealed run whose SUMS on disk is missing or wrong can never be finished | CONFIRMED (minor; finder major) | no directory sync between the SUMS rename and the sealed line; Check and finish "refuses otherwise with the marker kept"; Finish as partial needs "no sealed record" | note |
| L7-6 | 7 | §4.13 time breaker; §4.15; §7.2; R-u (OMISSION: power request) | The wall-clock breaker stops any run that spans a sleep of more than 5 minutes | CONFIRMED (major) | "No successful request for 5 minutes (wall clock, XC9)"; §4.15 "a short drop (… wake …) costs only time"; §7.2 "nothing fails" | RR2-C8 |
| L7-7 | 7 | P12; §4.13 breaker, wedged run; §4.6 step 7 | A hung archive-source read trips no breaker, and the wedged-run text blames the destination | CONFIRMED (minor) | the breaker counts Graph requests only; the archive probe runs only "After a missing-file or read error" | note |
| L7-8 | 7 | §4.13 write rows; §4.9 naming; R-g | Five per-message rejections in a row (antivirus, rejected names) stop the run as a destination failure | CONFIRMED (minor) | "5 consecutive write failures of any class … run-fatal: destination"; those errors are classed "retryable" | note |
| L7-9 | 7 | §4.3 W1 `immutable=1` | `immutable=1` on a folder still being written gives wrong rows or SQLITE_CORRUPT | DUPLICATE of L3-5 | the same failure, case (c) | as L3-5 |
| L7-10 | 7 | §7.6 R-o3; §4.3 W1; §5.2 archive row (OMISSION) | Another archive folder that cannot be reached is reported "isn't a MailArchive archive" | CONFIRMED (minor) | R-o3 is the only W1 wording for a missing manifest; `manifest.go:514-516` loads a missing file as an empty manifest | note |
| L7-11 | 7 | §5.1 W3; §4.16; §4.19 | A record written before the journal `start` is never found | DUPLICATE of L3-9 | the same failure | RR2-C12 |
| L7-12 | 7 | §4.13 Finalization step 1 | The presence check reads a vanished volume as every file gone | CONFIRMED (minor) | step 1 has no root probe and does not tell ENOENT from EIO, ENODEV or ERROR_NOT_READY | note |
| L7-13 | 7 | §4.7 posture line; §4.9 Modes; `Posture(dir)` | The posture line has no "couldn't check" outcome, and it reads the destination, not the root the export inherits into | CONFIRMED (minor) | three wordings only; `Posture(dir)` returns Open, Private or Unenforceable with offenders, and no error; part (b) is L6a-1 | note; closed by RR2-C6 |
| L7-14 | 7 | §4.3; P16 (OMISSION) | Another archive folder's manifest is unmarshalled whole inside the process that may be running an export | DUPLICATE of L6b-1 | the same failure | RR2-C11 |
| RR2-H1 | rescue | §4.6 step 2; §4.11; §7.5; §8.1 #6; §10 E4/E5; P10 | Another archive folder's pages are exported as live `.html`: the meta-only detector passes a page with `<script>` and no CSP prefix, and E4 copies pages before E5 builds the detector at all | CONFIRMED (hidden; major) | `hostileIn` checks only `<meta>` (`html.go:260-285`); the renderer relies on its leading CSP (`html.go:153-154`); §4.11 "any hostile meta other than the tool's own leading metas"; E5 builds `LiveRefreshIn` and "the quarantine path" while MA-420 (E4) copies the `.html`; R19 "its `<head>` begins with a Content-Security-Policy meta" | RR2-C9 |
| RR2-H2 | rescue | §4.9 temp files and final placement; P6; MA-417 | `renamex_np`, `MoveFileEx` and link + unlink resolve paths, so a parent swapped for a junction in the rename window redirects the placement outside the root | CONFIRMED (hidden; major) | §4.9 names `renamex_np(RENAME_EXCL)`, `MoveFileEx` and "link + unlink", against P6 "no write can escape it" and §4.9 "every later … rename … goes through that root"; under J3 an inherited ACL can grant another account write access inside the root | RR2-C10 |
| RR2-H3 | rescue | §4.3 W1; §8.1 #6; §8.2 archive row | Another archive folder's manifest is read with no bound, and its `search.db` is opened with SQLite's trusting defaults | CONFIRMED (hidden; minor impact) | `manifest.go:514`, `:524`, `:535`; §4.3 "only with the URI parameters mode=ro&immutable=1" and no defensive setting, while §4.3 claims "bounded reads"; the repo registers no SQL functions of its own | RR2-C11 |
| RR2-H4 | rescue | §4.16; §4.2 history; §4.19 Lifetime; §5.2 Journal row | Losing the journal strands every unfinished run: nothing lists the run records, so none is finished, retried or deleted | CONFIRMED (hidden; major) | "reconciles each run whose last event is start or sealed"; history is "the cached journal view"; "Losing the journal loses the history list … Exports are unaffected" | RR2-C12 |
| RR2-H5 | rescue | §4.7 step 3 [W1]; §4.3; `extract.go:618-660` | R-f compares symlink-resolved paths, so a mapped drive, `subst`, a `\\localhost` share or a bind mount lets the export be written inside the other archive folder | CONFIRMED (hidden; major) | `destOverlapsOut` is `resolveExisting` (`filepath.EvalSymlinks`) then `filepath.Rel`; §4.3 "Nothing is ever written there", the owner-direct part of Q3 | RR2-C13 |
| RR2-H6 | rescue | §9 MA-531; §4.17; §4.3 W1 | §4.17 saves the export fields at every start, so MA-531's "Settings … unchanged" fails, or the row must be narrowed | CONFIRMED (hidden; minor impact) | MA-531 "Settings and the installed weekly backup unchanged"; §4.17 "They are saved when an export starts"; §4.3 "It never touches Settings" | RR2-C14 |

### Notes (minors; not conditions)

None of these is accepted as a gap. Each stays open, owed by the design's author in the
slice named. W1's notes are part of "W1's text completed as RR-3 finds" (owner rulings
§11). Where a condition closes a note, the note says so.
- **L3-5 (E4, W1).** Open `search.db` only when no non-empty `-wal` or `-journal` sits beside
  it and the folder is not the configured archive (by identity). Drop listed rows whose key
  is not in the snapshot or whose files are absent, writing their metadata nowhere. Treat
  any query error as "list unavailable", never as damage. MA-531 adds a WAL that holds a
  deletion. The fix changes W1's delegated `-wal` detail, which the confirmed finding
  allows. L7-9 is its duplicate.
- **L3-7.** Closed by RR2-C1: P6's exception names the one atomic marker rewrite.
- **L3-8 (E2).** The class is closed by RR2-C7 (R-d2) and the floor by RR2-C5. Still owed:
  truncate the run record's torn tail before each append, as the journal does, and correct
  §5.2's Run record row, which says an unsealed run's export "still self-verifies" (L7-4's
  sub-point).
- **L3-9.** Closed by RR2-C12. L7-11 is its duplicate.
- **L3-10.** Closed by RR2-C2: the set fingerprint resumes the SHA-256 state that the
  previous part's ledger line saved.
- **L3-11.** Closed by RR2-C4: an empty or temp-only root reconciles as interrupted.
- **L3-12 (E3).** Define `host` as the OS machine id (Windows `MachineGuid`, macOS
  `IOPlatformUUID`, Linux `/etc/machine-id`), never `os.Hostname`.
- **L3-13 (E4, W1).** For another archive folder, replace "Rebuild search index fixes the
  list" and "run Check integrity" with read-only wording ("the list isn't available for
  this folder; whole-folder selection includes every message", "this file is damaged in
  that folder; the export lists it"), and offer no dashboard maintenance action there.
- **L3-14 (E2).** State that readers read every older run-record and journal format version,
  and that the run-record format changes only additively while any unsealed record exists.
- **L3-15 (E3).** Bound Drain's work, not only its wait. On shutdown, append a cancelled line
  to the run record and stop, and leave README, manifest, sums and the marker text to
  Finish as partial at the next launch. §6, §7.2 and MA-514 name that path.
- **L6b-1.** Closed by RR2-C11. L7-14 is its duplicate.
- **L6b-2 (E4, W1).** Lstat `search.db` in the chosen folder before the read-only open, and
  refuse a symlink or a non-regular file (the list is unavailable, and nothing is written).
  MA-531 adds the case.
- **L7-5 (E2, E3).** Sync the directory after the SUMS rename and before the sealed line.
  When Check and finish finds SUMS missing or mismatched, offer "Rewrite README, manifest
  and checksums from this app's record" through the finish path, re-check every listed
  file, and keep the marker until that check passes.
- **L7-7 (E4).** Make the no-progress breaker source-neutral (no message finished for 5
  minutes of awake time, with R-o2's wording for an archive), and have the wedged-run text
  name the side whose call is blocked.
- **L7-8 (E2).** Count toward the 5-failure breaker only failures whose root probe fails or
  whose errno is unknown. Class antivirus codes (225 and 226, or a temp file gone before the
  rename) and rejected names (ENAMETOOLONG, EINVAL, ERROR_INVALID_NAME 123,
  ERROR_FILENAME_EXCED_RANGE 206) as per-message terminal, each with its own reason, and
  try a rejected name once with the shortest stem.
- **L7-10 (E4, W1).** Tell "can't be reached" (the drive was removed, or the share
  disconnected) from "isn't a MailArchive archive" for both archive sources, and give
  §5.2's archive row the second source (R-o3, unreachable, list unavailable, R-o2).
- **L7-12 (E2).** On the presence check's first Lstat error, probe the root (`Stat(".")`). A
  failed probe is run-fatal for the destination and records nothing as gone. Only ENOENT
  inside a live root counts as "gone since its write".
- **L7-13.** Closed by RR2-C6: the "couldn't check" outcome, and the posture of what the
  root inherits.

### Mechanical check (RR-2)

`docs/design-extraction-workspace.md` keeps BUILD STATUS "approved with conditions, not
built" and cites this review. Its header now says this addendum is filed
(GO_WITH_CONDITIONS) and that RR2-C1..RR2-C14 are folded, each marked `[RR-2 RR2-Cn]`, with
a condition map. Before E2 code: the re-verification of those folded conditions, and again
of the fixes the first re-verification made (below). After E3: the adversarial pass and the
friction review, as before.

### Re-verification (2026-10-09)

**What the check found.** A fresh-eyes check of the folded text at `c2ba9eb` passed RR2-C1,
RR2-C5..RR2-C12 and RR2-C14, the hard limit and the escalations. It found four conditions
not plainly delivered (RR2-C2, RR2-C3, RR2-C4 and RR2-C13) and fourteen stale passages, and
no gate weakening. Each fix below lands in the design under its condition's mark; §9's row
changes say `[re-verification]`.
- **RR2-C2.** A part finished as partial after a later part had sealed (RR2-C7 allows that
  order), and a part marked gone, left the set fingerprint with no state to resume. It now
  covers the chain's **sealed** parts in the order they sealed. It resumes the chain's most
  recent ledger state under the ledger's mutex, each ledger line names the parts it covers,
  and the page and the receipt give that order (§4.10, §4.13 step 7, §4.16; MA-521).
- **RR2-C3.**
  - Reconcile appended `end` unconditionally, so a sealed run whose marker action was never
    done left the re-probe set. It now appends the ledger line at once, and the `end` only
    when a probe sees the marker absent. Otherwise Check and finish appends it, so an `end`
    always means no marker action is left (§4.16, §5.1 W11; MA-447 corrected, MA-448).
  - "Gone" was lost at compaction, and the stated bound was false. "Gone" is now also a
    `gone` line in the run record, and compaction keeps unresolved runs. §4.19's Lifetime
    defines settled and unresolved runs and the three deletion cases (R-d's included), and
    states the real bound (§4.16, §4.19, §5.1 W12; MA-430).
  - A positive mismatch was neither terminal nor an observation. It is now terminal and
    offers Locate… and This folder is gone (§4.2, §4.16; MA-448).
- **RR2-C4.** A folder with no nonce (an empty root, or one whose marker is gone and that has
  no README) is confirmed by its file identity alone, or else it is "couldn't confirm"
  (§4.9, §4.10, §4.13, §4.16, P14, §5.1 W3). Open folder keeps R-r for a positive mismatch
  and refuses an unconfirmed folder with the new R-r2 (§7.6; MA-430, MA-438).
- **RR2-C13.** The walk compared the destination's ancestors only with the archive root's
  identity, so an alias of a folder *inside* the archive passed (a bind mount of
  `<archive>/<store>`, a share defined on a subfolder). Two changes close it:
  - R-f now compares the destination and its ancestors with the identities of every
    directory in the archive, from a bounded no-follow walk.
  - The new **R-f2** refuses, failing closed, every pairing where identity cannot decide: a
    share served by this computer, an archive on one share with the destination on another,
    a FUSE or stacking filesystem, or an incomplete walk. A destination on another
    computer's share beside a local archive, and the reverse, stay allowed.

  Create folder's parent gets the same check, because its mkdir is a write (§4.7, §4.8,
  §7.2, §7.6, §8.1; MA-414, MA-434, MA-438, MA-531). This closes the residual that the check
  said would otherwise need the owner, so no OWNER RULING is raised. Its cost is a stated
  Type I cell in §7.2.
- **Stale passages.**
  - The interrupted and finished-as-partial labels take their cause from a `stopped` line.
    A run appends it when it stops before its seal while the app runs on (§4.13, §4.16,
    §4.19; MA-437).
  - The launch sweep deletes only settled runs' records, so Check and finish keeps its record.
  - MA-423's archived-page cases move to MA-420 (E4), with their prove-fail.
  - §5.2's Journal row names the run-record deletions.
  - §8.1 #5 now claims only what the mechanism refuses.
  - Two precision notes are folded. A non-2xx body is never breaker progress (RR2-C8). On
    Windows, placement falls back to the plain handle-relative `FILE_RENAME_INFORMATION`
    where a volume rejects the Ex class (RR2-C10).
  - In `docs/design-local-confidentiality.md`, the sentences saying the workspace's posture
    line uses `Posture(dir)` now describe the hand-over form. The same defect in
    `extract -dest`'s use is stated there as an open gap owed by its slice A.

**Not changed here.**
- The rulings log's J3 bullet still says the workspace's line uses `Posture(dir)`. The log
  is the owner's record and is not edited. The amendment above is where J3's mechanism
  changes, and the next revision of the log should point J3 at it.
- `extract -dest`'s posture fix is local-confidentiality slice A's (the follow-up above).
- L3-12's host definition, on which the sweep's "this host only" rests, is still a note
  owed in E3.

No folded change is reverted. Each fix adds a check or a refusal, corrects an assertion that
encoded a confirmed defect (MA-447's unconditional `end`), or moves a case between proposed
rows without dropping it (MA-423 to MA-420). These fixes are re-verified again before E2
code.

## Addendum: RR-2b (2026-10-09), the delta design check before EW-E2 code

**Design reviewed:** `docs/design-extraction-workspace.md`, revision 2, with the RR-2
conditions and their first re-verification folded, at `9ba49f8` (branch `wip/rr2b`). Also
read: `docs/design-fault-isolation.md` for SG (§4.8 and its §10 SG row), and
`docs/owner-rulings-2026-10-09.md` (J3, J4, §13.4, §13.5). Where the log and the design
disagree, the log wins.

**Scope** (owner rulings §13.5, "RR-2b, before EW-E2 code"):
- **(a) Re-verification.** A fresh-eyes check of the four conditions that RR-2's fixer
  re-fixed: RR2-C2, RR2-C3, RR2-C4 and RR2-C13.
- **(b) Lenses 6 and 7 on fault-isolation RR-6's edits to this design**, which RR-2 never
  saw (§13.4's follow-up):
  - the six size-cap figures, now 256 MiB or higher;
  - capture's interim 10-minute overall cap;
  - §4.5's redirect bullet about SG's `$value` client;
  - MA-528.

  This includes the question RR-6 left for EW-E1's note: is the copy export's own 2 h
  overall cap bounded by the workspace's no-progress clock?

Not reopened: OW1–OW4, Q3's yes, the log's rulings, J3's amended mechanism (RR2-C6), and
the RR-2 conditions that passed their first re-verification.

**Procedure:** the battery of `assurance-kit/process/design-review.md`, run as one small
design check:
- **Re-verifier:** one fresh-eyes reviewer checked each condition's delivered text against
  the condition and the log.
- **Finders:** lens 6 (security / adversarial) and lens 7 (failure domains /
  recoverability), on RR-6's edits and the text they touch.
- **Skeptic:** one refutation-default reviewer checked every finding against the design
  text, the code and the log.
- **Clerk:** this filing. The run's instructions give each confirmed finding the smallest
  mechanism change that dissolves it, folded as a condition. No separate rescue agent ran.
  The clerk took each finder's named fix as the skeptic endorsed it, and refined it where
  noted below. The clerk did not find these issues, and re-read the cited code before
  folding: `oauth2.go:357-366` at v0.37.0, `net/http/client.go:502-508`, `:719-726` and
  `:833-838`, `internal/export/html.go:640-677`, `internal/graph/graph.go:129-142` and
  `:529-561`, `delegated.go:65-102`, and the runtime's per-OS monotonic clocks.

**Lenses run:** 6 security / adversarial · 7 failure domains / recoverability.

**Result:**
- **Re-verification:** all four conditions FAIL again. RR2-C2 has 6 gaps, RR2-C3 5, RR2-C4
  3 (plus a lens-6 side note), and RR2-C13 4. Each gap is fixed in the design and marked
  `[RR-2b re-verification]`.
- **Findings: 12 found · 10 confirmed · 2 duplicates** (L7-2 of L6-1, L7-5 of L6-3) ·
  0 refuted. After the skeptic: 0 blocker, 3 major (L6-1, L6-2, L7-1) and 7 minor. L6-3
  was confirmed in part: its "no row turns red" claim is refuted by fault-isolation's
  MA-540 twin.
- Every confirmed finding is dissolved by a named change: **RR2b-C1..RR2b-C10**.

**Verdict: GO_WITH_CONDITIONS.** No lens fails: no blocker survives, and each major is
dissolved by a modest mechanism change.
- **Folded.** The re-verification fixes and RR2b-C1..RR2b-C10 are folded into the design as
  tracked changes, marked where they land, with an RR-2b condition map in its header.
- **Re-verification owed.** They are re-verified before EW-E2 code (design-review.md
  step 4). RR2b-C2 belongs to slice E5 and is owed before E5 code.
- **Another design.** RR2b-C3 and RR2b-C8 also land in `docs/design-fault-isolation.md`
  §4.8.6 and its §10 SG row, marked the same way, because SG builds the `$value` client and
  its size stop.
- **No OWNER RULING** is raised (below). **No MA row** is added, and MA-533..MA-539 stay
  reserved to the workspace (§13.5).

### Re-verification (RR2-C2, RR2-C3, RR2-C4, RR2-C13)

| Condition | Result | What was still short (the re-verifier's gaps) | Fix, marked `[RR-2b re-verification]` | Where it landed |
|---|---|---|---|---|
| RR2-C2 seal-and-finish, ledger, set fingerprint | FAIL (6 gaps) | (1) A ledger line that reconcile appends after a W9a–W9b kill or a failed append cannot hold the set state. The state comes from the part's in-memory SUMS bytes, the sealed line held only status, counts and fingerprint, and reconcile may not read the folder. The chain's next part then has no state to resume, or the set skips a sealed part. MA-447 and MA-521 never test it. (2) "A part marked gone never seals and never enters it" is false: a sealed run with no `end` can be marked gone after its SUMS entered the set. (3) A part that seals late cannot find the chain's most recent ledger line, because no ledger line names its chain. (4) §4.2's finish row put the marker before the seal. (5) The log's "A record is deleted only after its ledger line exists" (§13.5) was broken by two §4.19 deletion cases: R-d's refused start, and a run marked gone before it sealed. (6) A failed ledger append had no failure class | (1) The ledger's mutex is taken before the set state is computed and held through W9b. The sealed line stores the new state, the set fingerprint and the parts covered, and the ledger line and reconcile copy them. Before computing, a seal appends any ledger line its chain is missing, from that part's sealed line. The launch sweep indexes records by chain id and appends missing ledger lines before the export slot opens. (2) Stated as it is: a part marked gone after it sealed stays in the set. The page and the receipt name it as gone, say the set cannot be checked without it, and list each other part's own fingerprint. (3) A chain id, the first part's run id, in every header, sealed line and ledger line. (4) The row runs the seal-and-finish step in §4.13's order. (5) The log's rule is kept as written. A run that never seals gets a ledger line of its own, `gone` or `refused`, before its record is deleted. If R-d's append fails too, the record is kept and adopted as an interrupted empty export folder. (6) The state-directory row covers a fingerprint-ledger append. The step stops there with R-d2, the marker stays, and reconcile or Check and finish completes it | §4.2 finish and locate rows; §4.10 `chain` and fingerprint; §4.13 steps 6, 7 and 9, and the state-directory row; §4.16 When `start` is written, Launch sweep, Recording, Set fingerprint; §4.19 Header, Seal, Stop and gone, Lifetime; §5.1 W9a, W9b; §5.2 ledger row; §7.6 R-d2; MA-430, MA-447, MA-521 |
| RR2-C3 absent or slow folders, gone, Locate… | FAIL (5 gaps) | (1) "Export these N messages again" for a gone part that had sealed subtracted that part's own messages, so N = 0 for a complete part whose stick is lost (MA-448's own scenario). (2) §4.16 and §4.19 defined unresolved runs differently, so a sealed, not-ended run marked gone was kept for ever under one and deletable under the other. (3) The first re-fix made a positive mismatch terminal. The log's §13.5 makes only the marker-and-seal outcomes terminal, and the log wins. (4) R-r's remedy ("find the folder yourself, then run Check this export") was stale, because Check this export checks the recorded root. (5) "Whose whole retry chain is complete" was undefined, so a gone single-part run could lose the record that its offered export action reads | (1) For a gone part, `rest` subtracts only the messages exported in the chain's other sealed parts that are not marked gone. (2) One definition, in §4.19, which §4.16 cites. A settled run has its ledger line and is either sealed and ended, or gone; every run that is not settled is unresolved. (3) A positive mismatch is now an observation. It is probed again each launch and by Look again, and still offers Locate… and This folder is gone. MA-448's re-verification case reads "probed again". (4) R-r points at Locate… and This folder is gone. (5) A complete chain is defined: every part is settled, and no part's card still offers an export action. A settled run whose chain is incomplete keeps its record while compaction keeps its lines | §4.2 locate row; §4.9; §4.13 Retry; §4.16 Outcomes, Recording, The bound, Launch sweep; §4.19 Lifetime; §7.6 R-r; MA-430, MA-437, MA-438, MA-448 |
| RR2-C4 identity tuple, no-nonce folders | FAIL (3 gaps, and a side note) | (1) §4.16 still gave "no nonce to read" as a cause of "couldn't confirm", against §4.9, which confirms a no-nonce folder by its file identity where that can decide. (2) The summaries in §4.10 and §4.13 covered only a root with no nonce *yet*. They left out a root whose marker is gone and that has no README. (3) No row asserted that second case. Side note (lens 6, not a delivery failure): on POSIX, dev+inode alone confirms a directory recreated at the same path that reuses the freed inode number | (1) "No nonce, and no file identity that can decide". (2) Both summaries now name both no-nonce cases and cite §4.9. (3) MA-430 adds the marker-deleted, README-less root: on a stable filesystem, Finish as partial is offered; on a fake vfat, "couldn't confirm" and R-r2. The side note is note RR2b-N1 below, stated as an open condition at §4.9 | §4.9, §4.10, §4.13 Finish as partial, §4.16 Outcomes; MA-430 |
| RR2-C13 R-f by identity, R-f2 | FAIL (4 gaps) | (1) The view classifier read only the filesystem type, which cannot see a share or name its server. On Windows, `GetVolumeInformationW` reports NTFS for an SMB share, so a share classified as local and the identity compare decided, which a NAS that gives each share its own volume serial defeats. No row exercised the real classifier. (2) Create folder checked the parent, then re-walked the path with `os.Mkdir(parent/name)`. (3) R-f2 applies on either side, so an archive on a self-served share or on a FUSE or stacking mount refuses every destination, and §7.2's and §7.6's remedies do not work there. (4) No row covered "the reverse" (a local destination beside a share-held archive), or an archive walk that met an unreadable directory | (1) Named inputs per OS: on Windows, the canonical path's UNC prefix and `GetDriveTypeW`; on Linux, `/proc/self/mountinfo`; on macOS, `statfs`'s `f_fstypename` and `f_mntfromname`. "Served by this computer" compares the named server with this computer's names and addresses, and a remote view is never classified local. (2) The mkdir goes through the parent handle the check opened, with no path re-walk. (3) The archive-side causes are stated, including that Microsoft 365 exports are refused too, with remedies that work: the archive's own local path, or the OS's own SMB or NFS client. A §7.2 cell and R-f2 variants. (4) MA-414 adds the reverse twin, the unreadable-directory case and the real classifier on each CI leg. MA-434 adds a hook that swaps the parent | §4.2 mkdir row; §4.7 step 3; §4.8; §5.1 W1; §7.2; §7.6 R-f2; §8.1 #5; MA-414, MA-434, MA-438 |

### Findings

Severity is the skeptic's. Duplicates cite their twin.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| L6-1 | 6 | §4.13 transport and no-progress rows (RR2-C8); §4.5 `MIMETo`; P12 | A download that keeps being cut after its 2xx retries for ever, and the message is never named. Every 2xx is progress, the backoff is at most 60 s, a trickle runs each attempt to the 2 h cap, SG's header and overall stops are unmapped, and nothing caps attempts on one message. The power request stays held | CONFIRMED (major) | §4.13 "Progress is a 2xx response, or body bytes of a 2xx response received"; transport row "back off on the same message … never recorded as a failure; counts only toward the no-progress clock"; P12 "A transport-class error never fails a message"; fault-isolation §4.8.1 rule 5 ("Idle, header or overall timeout → fetch-timeout. Transport."); "Subjects appear only in the failure list". This answers RR-6's question to EW-E1's note: the 2 h cap is not bounded by the no-progress clock | RR2b-C1 |
| L6-2 | 6 | P16; §4.15 run memory; §8.2 MIME row; §8.4 misbehaving server; §4.11 panic recovery; MA-419 | Readable HTML memory is not bounded by the size cap. The renderer writes the whole data URI at every `cid:` reference and re-parses the result. Go's out-of-memory is fatal, so §4.11's recover misses it, and Finish as partial then starts on the same message again: a crash loop | CONFIRMED (major; owed before E5 code, not E2) | `html.go:649-665` (`ref.ReplaceAllString(body, dataURI)`); `drain`, `html.go:671-677`; `sanitizeHTML`, `:164`; `hostileSurvives`, `:250-256`; `parseMessage`'s `io.ReadAll`, `mbox.go:289`, `:334`; P16 "bounded by the shared size cap"; §8.4 "none"; MA-419 "on (one message held)". The flaw predates RR-6, whose edit restated the bound with the new figure | RR2b-C2 |
| L6-3 | 6 | §4.5 Redirects and bodies (XC26) [fault-isolation RR-6]; §4.18 F15; §10 E1 contents and gate cell; MA-402, MA-516, MA-527, MA-528 | §4.5's premise that `oauth2.NewClient` uses the context's client only for token acquisition is false, so E1's `Timeout` reaches every client `NewClient` builds. The `$value` exception and §13.4's fourth question are missing from §4.18 and §10, and no row turns red if the timeout reaches SG's `$value` client | CONFIRMED in part (minor) | `oauth2.go:357-366` at v0.37.0 (`go.mod:12`) copies `CheckRedirect`, `Jar` and `Timeout`; `graph.go:129-130`; `delegated.go:65-66`, `:89`, `:102`; the E1 gate cell omits §13.4's question. **Refuted part:** fault-isolation's MA-540 twin ("headers that arrive after the scaled JSON request timeout but inside the header window → the item is archived", with a 100 ms JSON timeout) turns red if that `Timeout` reaches SG's `$value` client | RR2b-C3 |
| L6-4 | 6 | §4.5 Redirects (XC26); §4.13; §7.6; §8.4; MA-528 | A refused redirect has no typed error, class or wording. net/http returns a `*url.Error`, `graph.Classify` reads it as a network error, the run backs off and stops with R-u ("check your network"), and the foreign host is never named. A custom `CheckRedirect` also drops net/http's 10-hop limit | CONFIRMED (minor) | `client.go:719-726` returns the 3xx response with a `*url.Error`; `graph.go:543-545` (`if err != nil { return nil, err }`); fault-isolation §4.8.1 rule 4; nextLink's "a typed error naming the foreign host", by contrast (§4.5, J4); `client.go:502-508`, `833-838`; MA-528 asserts zero foreign hits only | RR2b-C4 |
| L6-5 | 6 | §4.13 "Before each message" and the transport row; §3; MA-427, MA-516 | The free-space budget is read per message, but a backed-off message downloads again, and nothing says the cut attempt's temp file is removed. Cut temps can therefore use the reserve, and finalization fails with the marker left for the user | CONFIRMED (minor) | the transport row states no temp removal, while the budget abort, a write error, cancel and the `linkat` fallback each state one; temps are `.mailexport-<16 hex>.tmp` with `O_CREATE\|O_EXCL`, one per attempt; §3 "The app never deletes in the destination"; MA-402's "no partial output" is SG's primitive | RR2b-C5 |
| L7-1 | 7 | §4.13 RR2-C5 budget stop, in-flight rule, Retry; §7.6 R-g, R-u, R-u2; §7.2; MA-427, MA-437 | The message in flight at a run-fatal stop is reachable by no remedy. It is in `failures`, "Export the remaining messages" takes the `not_attempted` ids, and "Export failed messages again" is offered only for partial runs. So R-g's own remedy drops the message that needed the space, and R-u2's remedy never retries its 25 failures | CONFIRMED (major) | §4.13 "listed in `failures`, never at the head of `not_attempted`"; Retry's offers; MA-437 "yields its `not_attempted` ids"; MA-517; R-u "none of them was marked failed"; the budget row's "before that message is written" contradicts the prose | RR2b-C6 |
| L7-2 | 7 | §4.13; §4.5 | SG's header and overall stops have no class in §4.13, and RR2-C8's progress rule turns the overall and stall stops into an endless same-message retry | DUPLICATE of L6-1 | the same failure. Its extra detail (the arithmetic for 205.3 MiB at 25 KB/s, the 37 KB/s floor, the missing §5.2 row for the `$value` download, the per-OS timer note) is folded into RR2b-C1 and RR2b-C9 | RR2b-C1 |
| L7-3 | 7 | §4.13 no-progress and 429 rows; §7.6 R-u; §4.5 redirect bullet [fault-isolation RR-6]; MA-528; §4.14 | R-u ("check your network") is reached by causes that are not a lost connection: (a) header stops on a slow conversion, (b) throttle waits, (c) a refused redirect, (d) the capture back-off. A breaker abort of the in-flight call also cannot be told from a user's Cancel | CONFIRMED (minor) | R-u is reached from "No progress for 5 minutes of awake time" with no other test of cause; fault-isolation §4.8.6 `MIMEHeaderTimeout`, 2 min per attempt; `get` sleeps up to 5 × Retry-After (`graph.go:532-559`), unclamped until FI-S3; §4.14 "429s are expected and handled"; fault-isolation's breaker has a control probe (§4.8.5), the copy export none; RR6-C1's second guard covers `RunGraph` only. (c) is L6-4 | RR2b-C7; (c) RR2b-C4 |
| L7-4 | 7 | §4.13 above-cap row and budget bullet [merge of RR-2 with RR-6]; §4.5; P16; the XC25 line; §8.2; rulings §13.4 ESC-8 | Under ESC-8's interim the 256 MiB cap is confirmed against nothing, yet an over-cap item is "retryable (a later export may succeed)". With one `maxBytes` = min(cap, budget), an item above the cap can also be reported as a full disk and stop the whole run | CONFIRMED (minor) | §13.4 ESC-8's interim, "the copy export alone carries the cap (256 MiB) from E1"; fault-isolation §4.8.6, the same; fault-isolation §4.8.5 "Every threshold … is fixed by design"; `MIMETo`'s `(int64, error)` exposes no length and no bound | RR2b-C8 |
| L7-5 | 7 | MA-528; §4.5 | MA-528 guards only the `CheckRedirect` half of SG's `$value` seam; `oauth2.NewClient` copies `Timeout` with it | DUPLICATE of L6-3 | the same failure, with L6-3's partial refutation (MA-540's twin). Its fix (SG's `$value` client never built from a copy of the shared client; an E1 case where a long `$value` completes) is folded into RR2b-C3 | RR2b-C3 |
| L7-6 | 7 | §4.13 no-progress row (RR2-C8); §4.15; §7.2 lid-closed cell; MA-516 | The suspend detector's clock is unstated. Go's monotonic clock stops in suspend on Linux and macOS, so a ticker timed by it never sees the gap; on Windows SG's timers count the sleep. The in-flight download restarts from zero on wake, which §7.2's "the sleep only" understates | CONFIRMED (minor) | runtime `sys_linux_amd64.s:270` (`CLOCK_MONOTONIC`); `sys_darwin_arm64.s:147` (`mach_absolute_time`); Windows reads interrupt time (`time_windows_amd64.s`); §4.13 "a 1-second ticker … a gap above 30 s is a suspend", with no clock named; MA-516's suspend is clock-injected | RR2b-C9; the per-message bound RR2b-C1 |
| L7-7 | 7 | §4.13 step 6 and the `stopped` line; §4.19 Seal; §4.16 journal lines and outcomes | A sealed failed run records no stop reason, so after a restart "Stopped: <reason>" and its reason-specific remedy have no source | CONFIRMED (minor; at the edge of the delta) | step 6: status, counts and fingerprint, no reason; a `stopped` line only for runs that stop before their seal; the journal fields carry no reason; reconciliation never reads the marker or README | RR2b-C10 |

### Conditions (must-fix-before-code; folded into the design)

- **RR2b-C1 — A download that is cut again and again after its body started is listed, never
  retried for ever.** (L6-1; folds L7-2.)
  - **SG's stops by kind.** `header` and `idle` are transport-class: the run backs off on
    the same message. So is an `overall` stop on an attempt that spanned a suspend.
  - **The download bound.** Per message, 5 cuts, or 2 h of awake time since its first
    attempt. A cut is an attempt that delivered body bytes and then ended in a
    transport-class stop before the body did. A cut whose attempt spanned a suspend is not
    counted. An `overall` stop with no suspend reaches the 2 h at once. Past the bound, the
    message is listed as retryable ("kept stopping partway", or "didn't finish downloading
    within 2 hours of download time"), and the run moves on.
  - **Unchanged.** Attempts with no body bytes stay under the no-progress window, with
    RR2b-C7's control request. So MA-516's 30 s outage still completes with zero failures.
  - **P12** restated, and a §5.2 row for the `$value` download.
  - **Rows:** MA-516 adds a stall five times (listed, and the run continues), a trickle to
    the scaled overall cap (listed, never a backoff), and a cut across a seam suspend (not
    counted). Its new prove-fail counts only attempts with no body bytes.
  - **Landed:** P12, §4.5, §4.13, §4.15, §5.2, §7.2, §8.4, §9 (MA-516), §10 (E2).

  *Closes:* L6-1, L7-2. It answers RR-6's question to EW-E1's note: each message's 2 h is now
  bounded.
- **RR2b-C2 — A readable copy has a render bound.** (L6-2; slice E5, owed before E5 code.)
  - **The bound.** Before `RenderWith`, the export predicts the render's size: the selected
    body, plus, for each attachment a `cid:` reference names, its base64 length times the
    references. Above 32 MiB, a fixed design threshold, no readable copy is made: the entry
    is `exported-original-only`, "too large for a readable copy", not offered for retry
    (XC35's outcome).
  - **The text.** §4.11 says why the cap does not bound the render, and that Go's
    out-of-memory is fatal, not a panic. P16, §4.15, §8.2 and §8.4 say the render is bounded
    by the render bound.
  - **Rows:** MA-509 pins the bound: an 8 MiB image referenced 10 times gives the original
    only, with the heap under the bound, plus a prove-fail. MA-419's "on" mode cites it.
    MA-509 is E5's row, so the bound and its row share one slice (XC28).
  - **Landed:** P10, P16, §4.11, §4.15, §8.2, §8.4, §9 (MA-419, MA-509), §10 (E5).

  *Closes:* L6-2. The archive exporter's own expansion is a follow-up.
- **RR2b-C3 — `Client.Timeout` reaches every client `oauth2.NewClient` builds, so SG's
  `$value` client is built without it.** (L6-3; folds L7-5.)
  - **§4.5** states `NewClient`'s real behaviour at v0.37.0 and its reach. §4.18's F15 row
    names the `$value` exception.
  - **SG** builds the `$value` client itself, over its own base transport and the client's
    token source, with E1's `CheckRedirect` and a zero `Timeout`. It never builds it with
    `NewClient` from a context carrying `httpTransport`'s client. Fault-isolation §4.8.6 says
    the same, marked `[RR-2b RR2b-C3]`.
  - **E1's gate cell** carries §13.4's fourth question.
  - **Rows:** MA-527 adds a `$value` longer than `reqTimeout` that completes, and a second
    prove-fail.
  - **Landed:** §4.5, §4.18, §9 (MA-527), §10 (E1); fault-isolation §4.8.6 and its §10 SG
    row.

  *Closes:* L6-3, L7-5.
- **RR2b-C4 — A refused redirect is a typed error naming the host, with the 10-hop limit and
  its own class.** (L6-4; L7-3 (c).)
  - **The error.** `CheckRedirect` returns `*graph.RedirectError{Host, Hops}`, which a caller
    finds with `errors.As` through the `*url.Error`. It keeps net/http's 10-hop limit itself.
  - **The class.** §4.13 matches it before `graph.Classify`. Another host is run-fatal, with
    the new R-u3 naming the host. Past 10 same-host hops is a per-message retryable failure.
  - **Rows:** MA-528 adds the typed error on both clients and the eleventh hop. MA-427 adds a
    foreign redirect → R-u3, never R-u. MA-438 adds R-u3's wording.
  - **Landed:** P4, P12, §4.5, §4.13, §5.2, §7.2, §7.6 (R-u3), §8.2, §8.4, §9 (MA-427,
    MA-438, MA-528), §10 (E1, E2).

  *Closes:* L6-4, L7-3 (c).
- **RR2b-C5 — A cut attempt's temp file is removed before the backoff, and the budget is read
  again before each retry.** (L6-5.)
  - The transport row states the removal. "Before each message" becomes "before each
    message, and again before every retry".
  - **Rows:** MA-427 and MA-516 assert that no `.mailexport-*.tmp` survives a retried
    message. MA-427 adds a volume that fills between two attempts → R-g at the second.
  - **Landed:** §4.13, §4.15, §5.2, §9 (MA-427, MA-516), §10 (E2).

  *Closes:* L6-5.
- **RR2b-C6 — The message in flight at a run-fatal stop is exported by the next part, last.**
  (L7-1.)
  - **The record.** Its progress line records it as retryable, with the stop's R-id and
    class.
  - **The offers.** "Export the remaining messages" on a run that stopped run-fatal takes the
    `not_attempted` ids, then the run's retryable failures, the in-flight message last. For a
    cancelled run it stays the `not_attempted` ids, so MA-437's assertion stands, and
    "Export failed messages again" is offered on a cancelled run with retryable failures.
  - **The text agrees.** A message stopped before its first byte stays first in
    `not_attempted`; one aborted at its budget is the in-flight failure. R-u loses "none of
    them was marked failed". R-g, R-u and R-u2 say that the next part includes the message,
    or the 25.
  - **Rows:** MA-427 (message 813 is in part 2, last); MA-437 (the order, R-u2's 25 and the
    cancelled-run offer); MA-438.
  - **Landed:** P12, §4.13, §7.2, §7.6, §9 (MA-427, MA-437, MA-438), §10 (E2, E3).

  *Closes:* L7-1.
- **RR2b-C7 — R-u only when a control request also goes unanswered; throttling and a stalled
  message get their own outcome.** (L7-3 (a), (b), (d).)
  - **The control request.** At the 5-minute mark the run sends `GET /me?$select=id` once,
    with no retry loop and a 30 s bound (`Client.Probe`, E1), never through `get`.
    - No HTTP answer → R-u.
    - A 429 or 503 → the new R-v2 (run-fatal: throttled).
    - A 2xx → the in-flight message is listed as retryable with its last error ("didn't
      start sending … N times" after header stops), the window restarts, and the run moves
      on.
    - Any other answer takes its own row.
  - **Pauses.** The run's own pauses (§4.14's capture back-off) are not counted toward the
    window.
  - **Cancel.** A breaker stop cancels that message's own child context, never the walk
    context. The run decides "cancelled" only from the walk context's `Err()`, never from an
    error chain.
  - **Rows:** MA-516 adds header stops with healthy JSON (listed, never R-u), a long 429
    (R-v2), a capture pause (no R-u), a sustained outage (R-u), and a breaker stop that is
    never recorded as cancelled. MA-438 adds R-v2 and R-u's wording.
  - **Landed:** P12, §4.13, §4.14, §4.15, §5.2, §7.2, §7.6 (R-u, R-v2), §8.3, §9 (MA-438,
    MA-516), §10 (E1, E2).

  *Closes:* L7-3 (a), (b), (d).
- **RR2b-C8 — Above the size cap is terminal for this version, ESC-8's condition stands next
  to each cap claim, and the cap is decided before the budget.** (L7-4.)
  - **The class.** The 400 / conversion row and the above-cap row are split. Above the cap
    is terminal for this version, listed "larger than this version of MailArchive exports
    (256 MiB)", and not offered for retry by a build with the same cap.
  - **ESC-8's condition** is stated next to the cap claims: the header's XC25 line, P16,
    §4.5, §4.13 and §8.2. It is an open condition, not accepted.
  - **Cap before budget.** `maxBytes` stays min(cap, budget), as the log's §13.5 records
    RR2-C5. SG's size stop is typed, `*graph.TooLargeError{Limit, Declared}` (fault-isolation
    §4.8.6, marked `[RR-2b RR2b-C8]`). A declared length above the cap is the above-cap
    class, whatever limit was passed; a stop at the budget is the free-space stop.
  - **Rows:** MA-402 (the typed fields); MA-426 (above the cap: listed terminal, and the run
    continues); MA-427 (a declared length above the cap with a small budget → above-cap,
    never R-g; one within the cap → R-g).
  - **Landed:** header, P16, §4.5, §4.13, §7.2, §8.2, §9 (MA-402, MA-426, MA-427), §10 (E2);
    fault-isolation §4.8.6 and its §10 SG row.

  *Closes:* L7-4.
- **RR2b-C9 — The suspend detector names its clock, and the in-flight re-download is
  stated.** (L7-6.)
  - **The clock.** The gap is measured on a clock that keeps counting in suspend:
    `CLOCK_BOOTTIME` on Linux, `mach_continuous_time` on macOS, and Go's own monotonic clock
    on Windows, which reads interrupt time.
  - **The re-download.** §4.15 and §7.2 state that the message that was downloading starts
    again from its first byte. The sleep counts toward neither the breaker nor the message's
    2 h (RR2b-C1). On Windows SG's timers count the sleep, so an `overall` stop after a long
    sleep is a cut, not a failure; SG is not changed.
  - **Rows:** MA-516 runs the real ticker with a seam suspend-counting clock: 3 minutes of no
    progress before the jump and 2.5 after give no R-u. Its third prove-fail measures the gap
    with Go's monotonic clock.
  - **Landed:** §4.13, §4.15, §7.2, §9 (MA-516), §10 (E2).

  *Closes:* L7-6.
- **RR2b-C10 — The sealed line and the journal `end` carry the stop's reason.** (L7-7.)
  - The sealed line records the stop's R-id (none for a Cancel), its class and the in-flight
    message's id. A stop between messages records its R-id with no message.
  - The journal's `stopped` and `end` lines gain `reason`: the R-id and class, with no
    subject.
  - **Rows:** MA-430 (after a restart, runs stopped by R-g, R-u, R-c and R-z are labelled
    from the sealed line).
  - **Landed:** §4.13, §4.16, §4.19, §5.1, §9 (MA-430), §10 (E2, E3).

  *Closes:* L7-7.

### Rescue outcomes (the skeptic's majors)

| Finding | Outcome | Named change (short) | Condition |
|---|---|---|---|
| L6-1 | DISSOLVED-BY | a per-message download bound (5 cuts after body bytes, or 2 h of awake time); SG's overall stop listed, never a backoff | RR2b-C1 |
| L6-2 | DISSOLVED-BY | a predicted-render bound before `RenderWith`; the original only above it | RR2b-C2 |
| L7-1 | DISSOLVED-BY | the in-flight message recorded retryable, and exported last by "Export the remaining messages" | RR2b-C6 |

No finding SURVIVES, and none is PARTIAL.

**What the clerk changed in the named fixes.** Each change is a strengthening, or keeps an
existing assertion; none is a narrowing:
- **RR2b-C1** counts only cuts after body bytes, so an outage with no 2xx stays under the
  no-progress window, and MA-516's "a 30 s outage → zero failures" holds. It does not count a
  cut whose attempt spanned a suspend, and its 2 h is awake time. On Windows SG's timers count
  the sleep, so the finder's "overall → listed" alone would list a message after an
  overnight lid close.
- **RR2b-C2** puts the bound's row in MA-509 (E5), not MA-419 (E2), so one slice builds both
  (XC28). It sets the bound at 32 MiB.
- **RR2b-C4** makes a redirect to another host run-fatal, since it would repeat on every
  message, and a same-host loop past 10 hops a per-message failure.
- **RR2b-C6** keeps a cancelled run's `rest` as its `not_attempted` ids, so MA-437's
  assertion stands, and offers "Export failed messages again" there instead.
- **RR2b-C7** sends the control request once, outside `get`. Through `getJSON`, a Retry-After
  longer than its 60 s context ends as a deadline error (F17), which would read as an
  outage. A throttled answer stops the run with R-v2, rather than waiting without a bound.
- **RR2b-C8** keeps `maxBytes` = min(cap, budget), which the log's §13.5 records for RR2-C5.
  It puts the declared length in SG's typed size stop, instead of moving the budget into the
  export's own writer.

**Where the log won.** Three gaps were closed by following the rulings log, not the first
suggestion:
- **RR2-C2 (5).** The re-verifier suggested scoping "A record is deleted only after its
  ledger line exists" to sealed runs. That would narrow the log's rule, so the rule is kept
  as written, and the two kinds of unsealed record get a ledger line of their own.
- **RR2-C3 (3).** A positive mismatch is not terminal, because §13.5 makes only the
  marker-and-seal outcomes terminal. This withdraws the first re-fix's change (3). If the
  owner wants it terminal, the log's next revision can record that (follow-ups).
- **RR2b-C8.** `maxBytes` stays min(cap, budget), as the log records RR2-C5.

### Escalations (OWNER RULING items)

None. No fix or condition narrows an invariant, a catalog row's guarantee or an X line,
accepts a residual, or changes what an existing covering test asserts:
- **Nothing is built, and no workspace row is in the catalog.** `docs/scenario-catalog.md`
  has no R29, S52 or workspace MA row, and `docs/ux-contract.md` has no X13–X15. Every row
  changed here is a proposed row in the design's own test plan.
- **No assertion is dropped.** Two re-verification cells are corrected in place. MA-448's
  encoded a claim the log does not record (a terminal positive mismatch). MA-521's encoded a
  claim the mechanism cannot meet (a sealed part leaving the set). Its case still holds for
  a part marked gone before it sealed.
- **P12's "A transport-class error never fails a message"** is a mechanism sentence, not
  catalog text, and no R-invariant promises it. Restating it makes P12's headline ("finishes
  around bad messages and names them") true.
- **Above the cap**, an item is no longer offered a retry that the same binary cannot
  complete. The legal-item gap under ESC-8's interim stays an open condition, not accepted,
  stated at each claim.
- **The render bound and the download bound** list the message with its reason, as XC35 and
  P12 already do for a render panic and a failure. The original, or the rest of the run, is
  still exported.
- Not reopened: OW1–OW4, Q3's yes, the log's rulings, and J3's amended mechanism.

### Follow-ups for other owners

- **The renderer's owner (`internal/export`).** The archive exporter calls `RenderWith` too
  (`exporter.go:319`, `:327`), so its `cid:` expansion is unbounded there as well. That
  predates these designs (L6-2). RR2b-C2 bounds only the workspace's call.
- **Fault-isolation's author.**
  - Two SG edits are marked `[RR-2b]` in `docs/design-fault-isolation.md` §4.8.6 and its §10
    SG row: the typed size stop (RR2b-C8) and how SG builds the `$value` client (RR2b-C3).
    Its next re-verification, or FI-SG's adversarial pass, should check them.
  - Capture's `app.classifyFetch` (S3) will read E1's refused redirect as a network error,
    through its rule 4, as the copy export would have. Matching `*graph.RedirectError` first
    would name the host on capture's path too (RR2b-C4).
- **The rulings log's next revision** (not edited here):
  - §13.5's RR-2b follow-up now has a result: this addendum, GO_WITH_CONDITIONS, with
    RR2b-C1..RR2b-C10.
  - Should a positive mismatch be terminal? That was the first re-fix's choice, withdrawn
    here because §13.5 does not record it.
  - Is §13.5's "A record is deleted only after its ledger line exists" meant for sealed runs
    only? If so, the `gone` and `refused` ledger lines become optional.
- **EW-E1's short adversarial note.** §13.4's fourth question is now in E1's gate cell
  (RR2b-C3).

### Notes (not conditions)

None of these is accepted as a gap. Each stays open, owed in the slice named.
- **RR2b-N1 (E2; lens 6).** It comes from the re-verifier's side note beside RR2-C4. It was
  not run through the skeptic, so it is recorded as a note and not graded.
  - **The gap.** On POSIX a folder with no nonce is confirmed by dev+inode alone (§4.9).
    ext4 and xfs commonly reuse a freed inode number. So a directory that someone else
    recreated at the same path can confirm, and Finish as partial would then write README,
    manifest, sums and marker into a folder the app did not create. Where the destination
    does not enforce file modes, the manifest's per-message entries (P9) are then readable
    there.
  - **Named fix.** Record the directory's birth time at mkdir (`statx` `stx_btime` on Linux,
    `st_birthtime` on macOS), and require it to match for a folder with no nonce. Where the
    filesystem reports none, the folder is "couldn't confirm".
  - **Windows.** An NTFS file ID carries a sequence number that changes on reuse. A FAT or
    exFAT file ID follows the directory entry's position, and name tunnelling can carry a
    creation time over for a short window, so the fix must be checked there too.
  - §4.9 states the open condition at the claim.

### Mechanical check (RR-2b)

`docs/design-extraction-workspace.md` keeps BUILD STATUS "approved with conditions, not
built" and cites this review. Its header says that RR-2b ran and is filed
(GO_WITH_CONDITIONS), with an RR-2b condition map, and each change is marked
`[RR-2b RR2b-Cn]` or `[RR-2b re-verification]`.
- **Before E2 code:** a re-verification of everything folded here. RR2b-C2's result gates E5
  code, not E2.
- **After E3:** the adversarial pass and the friction review, as before. The friction review
  walks §7.2's new cells.
