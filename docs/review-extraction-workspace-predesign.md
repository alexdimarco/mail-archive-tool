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
