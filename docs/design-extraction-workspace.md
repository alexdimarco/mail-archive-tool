# Design — Extraction workspace: a one-off copy export from Microsoft 365 or the archive, inside MailArchive Desktop

**Revision:** 2 (2026-10-09). **BUILD STATUS:** approved with conditions, not built; owner
rulings folded (`docs/owner-rulings-2026-10-09.md`).
The 10-lens pre-code review is filed as `docs/review-extraction-workspace-predesign.md`:
GO_WITH_CONDITIONS, 118 findings (111 confirmed, 7 refuted). Conditions **XC1..XC40** are
folded into this revision, and the build must not start from revision 1. Nothing in this
document is built.

Gates before code:
- **XC40:** lenses 3, 6 and 7 re-run on this revision, as an addendum to the review,
  before any E2 code (RR-2). XC1 moves where the app keeps its truth, which is a material
  change.
- **RR-3 [ruling W1]:** lenses 3, 6 and 7 re-run on the second archive source's text (Q3),
  before E4 code. It may be folded into RR-2.
- **Owner rulings:** every question in §11 is ruled (`docs/owner-rulings-2026-10-09.md` §2).
  Q13 is escalated (ESC-5) and blocks nothing. Each change a ruling makes to this text is
  marked `[ruling <id>]`.

Key conditions:
- **XC1** a per-run 0600 *run record* in the local state directory is the app's system of
  record. The export directory is the recipient's (§4.19).
- **XC2** retry and "Finish as partial" read only the run record.
- **XC4** reconciliation runs off the request path and only ever downgrades.
- **XC5** status is honest at every kill point.
- **XC6** process-lifetime context and a real shutdown path.
- **XC7** identity-safe token write-back.
- **XC9 / XC10** time-based breakers and per-OS failure classes.
- **XC23** E0 is `docs/design-local-confidentiality.md` slices C1+C2.
- **XC25** the Graph layer is shared with `docs/design-fault-isolation.md`, through its SG
  slice [ruling J4].
- **XC28** each slice ships only what it builds.

**Ids are allocated [ruling Q11 = J1].** Revision 2's provisional R22 / S42 / X12–X14 are now
**R29**, **S52** and **X13–X15**, and this document uses only those. The MA rows keep
MA-400..MA-449; each split row's "a" half keeps the base number and its "b" half takes an
overflow id (MA-505..MA-513); the rows revision 2 numbered MA-450..MA-466 are MA-514..MA-530;
the new W1 row is MA-531; MA-406 is retired. None is in the catalog yet: each lands with its
slice's catalog-first PR, for the OPERATOR to accept into `docs/scenario-catalog.md` and
`docs/ux-contract.md` (`docs/owner-rulings-2026-10-09.md` §8).

**Revision 2 condition map** (review → where it lands):

| Condition | Lands in |
|---|---|
| XC1 run record | §4.19, P19, §4.10, §4.11, §4.13, §5.1, §5.2, §8.4 |
| XC2 retry / Finish as partial | P4, P6, R29, §4.13, §4.19, §7.2, MA-437 |
| XC3 journal binding and versions | §4.9, §4.16, §5.1, §6 |
| XC4 reconciliation | P19, §4.2, §4.16, §8.2, §9 |
| XC5 honest status, P14 | P14, §4.9, §4.10, §4.13, §5.1, §9 |
| XC6 context and shutdown | P13, §4.3, §4.13, §6, §9 |
| XC7 token write-back | §4.3, §4.13, §4.16, §5.2, §7.6, §9 |
| XC8 sign-in slot | §4.2, §4.16 |
| XC9 Graph failure classes | P12, §4.13, §4.15, §7.6, §9 |
| XC10 destination/archive classes | §4.6, §4.13, §5.2, §7.6, §9 |
| XC11 free-space policy | §4.7, §4.13, §7.6, §9 |
| XC12 removable filesystems | §4.7, §4.9, §4.18, §7.6, §8.5, §9 |
| XC13 no-replace placement | P6, P7, §4.9, §9 |
| XC14 verification aids | P9, §4.10, §4.13, §4.16, §7.5, §8.4, §9 |
| XC15 retry chains | §4.10, §4.13, §4.16 |
| XC16 preview counts | §4.7, §9 |
| XC17 row handles, folder ids | P4, §4.2, §4.4, §8.1, §9 |
| XC18 session lifecycle | §4.3, §4.4, §7.6, §9 |
| XC19 archive read correctness | §4.3, §4.6, §9 |
| XC20 archive concurrency, SQLite | P1, P15, §4.3, §4.14, §4.18, §9 |
| XC21 source scope | §3, §4.1, §4.10, §4.16, §7.3 |
| XC22 inheritor guard | §7.4, §11 Q3 |
| XC23 E0 owner, localweb.Mux | P17, §4.2, §8.1, §9, §10 |
| XC24 X13 scope, Q5 premise | §1.4, §2 X13, §4.18, §6, §11 Q5 |
| XC25 Graph layer with S3 | §4.5, §9, §10 |
| XC26 redirect, bodies, F15 | P4, P13, §4.5, §4.18, §8.2, §8.4 |
| XC27 MessagePage fields | §4.5, §4.10, §4.11 |
| XC28 slices and rows | §4.1, §4.17, §9, §10 |
| XC29 provisional ids | this header, §11 Q11 |
| XC30 inline conditions | P5, P15, P16, §4.11, §4.14, §4.15 |
| XC31 picker footprint | P18, §4.8, §5.2, §6, §8.3 |
| XC32 destination posture | §3, §4.7, §4.9, §8.5 |
| XC33 Open folder | §4.2, §4.16, §9 |
| XC34 settings writes | §4.17 |
| XC35 friction cells | §4.13, §6, §7.2, §7.6 |
| XC36 shared helpers | P8, §6 |
| XC37 rollback | §6, §11 Q6 |
| XC38 unowned dependencies | P7, P13, §4.14, §4.18, §10 |
| XC39 fault-isolation's OW4 reading | owner rulings, §11 Q12 |
| XC40 lens 3/6/7 re-run | this header, §10 |

**Owner rulings (already made, not reopened here):**
- **OW1** — the workspace is a new page inside MailArchive Desktop
  (`internal/desktop`, `cmd/mailarchive-desktop`), reusing its sign-in, its loopback
  server and its CSRF/Origin/Host guard (`server.GuardLocalPOST`). Not a separate app.
- **OW2** — the source is EITHER live Microsoft 365 via Graph OR the local MailArchive
  archive; the user chooses per run.
- **OW3** — live Microsoft 365 is the user's OWN mailbox only in v1: no shared
  mailboxes, so no `Mail.Read.Shared`. The mockup's shared-mailbox box is omitted and
  replaced by an explanation.
- **OW4** — drag-and-drop from Outlook was considered and rejected. Selection happens
  in the page ("the simpler tool … where you just select the source and choose the
  export destination to copy it to").
  **The engine (XC39) [ruling Q12].** The "simpler tool" is this workspace's own copy-export
  engine (`internal/app/copyexport*.go`). `docs/design-fault-isolation.md` revision 2 reads
  the same relayed decision the same way: its inputs and FC20 name this workspace as the
  simple flow, with its own engine and `CopyOutcome`, never `app.Run` / `app.RunGraph`. §1.2
  argues why `RunGraph` cannot target a hand-over folder. Where the two overlap, the
  copy-export outcome reuses fault-isolation's gap and outcome vocabulary and its
  `app.GapRemedy`. This reading does not declare desktop P7 superseded: that question is
  escalated (ESC-5, §11 Q13).
- **Q3 (owner-direct, 2026-10-09) [ruling Q3]** — "yes the read only": the archive source
  also accepts another archive folder, read-only, chosen with the folder picker, and nothing
  is ever written there (W1: §4.3, §4.6, §4.7, §7.4, MA-531).

**Claim labels used throughout.** **proven** = a tier-U/S MA row in §9 asserts it on
every commit once its slice is built (nothing is built, so read "proven" as "will be
proven by MA-4xx"); **conditional** = holds only under the condition written next to
the claim; **aspirational** = intended, not mechanically assured. Every property names
the R-invariant or X-rule it serves, or the new one it proposes.

**Vocabulary.** *Copy export* (or *export*): one run of the workspace. *Export
directory*: the new folder one run creates under the chosen *destination*. *Original*:
a message's MIME bytes (`.eml`). *Derived file*: anything this tool produced from the
original (readable `.html`, extracted attachments). `extract` always means the existing
CLI verb (`internal/app/extract.go`), which this design does not change.

## 1. Problem

1. **No way to hand over a subset of mail.** The only path out of the archive is
   `extract` (`internal/app/extract.go:103`). It is whole-archive (every manifest record,
   `extract.go:175`), copies preserved `.eml` only (R20), holds the archive's exclusive
   lock for the whole run (`extract.go:124`), refuses a non-empty `-dest` without
   `--overwrite` (`extract.go:149-157`), and is not in the dashboard: `DashboardHandler`
   registers capture, verify, rebuild, schedule and settings only
   (`internal/desktop/dashboard.go:96-105`). The desktop design's P7 promised "Export a
   copy"; it was never built. A user who wants "Inbox and most of Sent Items, on a USB
   stick, with something the recipient can check" has no tool. Today they drag from
   Outlook (OW4 rejects that) or ask IT for a Purview export.
2. **The desktop's default archive has nothing for `extract` to emit.** `KeepRaw`
   defaults to false (`internal/desktop/settings.go:17`, passed at
   `internal/desktop/capture.go:141`), so a default desktop archive has no `<stem>.eml`,
   and `extract` exits 3 with "NOTHING extractable" (`extract.go:713`, MA-183). The live
   mailbox still holds the originals. But the only Graph pipeline, `app.RunGraph`
   (`internal/app/graph.go:68`), is welded to the archive: it takes the archive lock
   (`:87`), loads and saves the manifest (`:98`, `:323`), writes through the archive
   `Exporter` (`:103`) and sweeps "gone" records (`:660`). It cannot be pointed at a
   hand-over folder.
3. **The Graph client lacks what a selection page needs.** `Folders`
   (`internal/graph/graph.go:175`) walks the whole tree in one call, selects only
   `id,displayName,childFolderCount` (`:205`), so there are no item counts, and returns
   only sanitized path segments (`:226`), never the display name. `Messages` (`:414`)
   streams a whole folder through a callback, with no ordering, no page cursor and no
   size. `MIME` (`:493`) buffers the whole body with `io.ReadAll` under a fixed
   10-minute deadline (`:74`), which is one cause of F9. `statusErr` (`:572`) returns an
   untyped string, so a caller cannot tell 404 (moved/deleted) from 401 (sign-in). The
   server's `@odata.nextLink` is followed without checking where it points (`:234`,
   `:486`), with the bearer token attached.
4. **The dashboard surface is not yet safe for a feature that lists live-mailbox
   subjects or writes to a caller-named folder.** The CSRF token is embedded in the
   unauthenticated GET `/` page (F3, `dashboard.go:90`, template `:287`), so any local
   account can scrape it and POST. GET handlers such as `/api/activity`
   (`capture.go:111`) are unguarded, and the dashboard and reader accept any `Host` on
   GET (F2, `internal/server/server.go:102`). The "already running" path opens whatever
   owns :8097 (F4, `cmd/mailarchive-desktop/main.go:92`). **Re-premised (XC24).** The
   shipped dashboard already lets any local account copy the user's live mailbox to a
   folder that account can read. It can POST `/api/settings` with any `Out`
   (`dashboard.go:110-128`), then POST `/api/capture`, which runs `RunGraph` with the
   victim's token (`capture.go:135-141`). So E0 closes an exposure that exists today, and
   it should ship on its own merits. The workspace inherits that exposure and adds a faster
   selective copy plus a read surface for live-mailbox subjects. A workspace that returned
   subjects over GET would add a CSRF-scrapable read of the live mailbox on top.
5. **The Graph path has no per-message fault isolation** (F9,
   `internal/app/graph.go:621`): one failing `$value` ends the walk. A hand-over of
   thousands of messages must finish around a bad item and name it.

## 2. Properties

Each property: the claim, the invariant it serves or proposes, its label, and the MA
rows that will prove it (§9).

- **P1 — Read-only to the mailbox and to the archive.** A copy export issues only GET
  requests to Graph (R17, R9). It never writes, locks or migrates anything in the
  archive directory: it takes no `.mailarchive.lock`, never calls `Manifest.Save`,
  `index.Add`, the history writer or `pages.Generate`, and refuses a destination inside
  the archive. Proven (MA-403, MA-436, MA-414). **Conditional:** for the archive source,
  opening `search.db` to list messages may create or touch SQLite's own
  `search.db-wal`/`search.db-shm` sidecars, exactly as the reader (`serve`) already does
  (`index.OpenReadonly`, `internal/index/index.go:144`, is not a SQLite read-only open).
  **XC20:** the workspace opens `search.db` with `mode=ro` / `PRAGMA query_only`. Where the
  medium does not allow that, closing the last connection may checkpoint a pending WAL
  into `search.db`. Its bytes then change while its rows stay the same, so MA-436 compares
  `search.db` by rows. No row or message file changes. **Another archive folder [ruling
  W1]:** chosen as a source, it is not even touched that way. Nothing is written there: no
  lock, no manifest save or migration, no `RepairKeys`, no SQLite `-wal`, `-shm` or
  checkpoint, no temp file (§4.3). Proven (MA-531, by bytes). Proposes **R29**.
- **P2 — Own mailbox only, no new permission (OW3).** The workspace builds only the
  delegated device-mode client (`/me` addressing, `internal/graph/graph.go:95`), even
  when the saved config says `auth=app`. It never addresses `/users/{x}` and never adds
  a scope: `delegatedScopes` stays `Mail.Read` + `offline_access`
  (`internal/graph/delegated.go:21`). There is no mailbox-address input on the page.
  Proven (MA-403, MA-408). Serves R17.
- **P3 — A selection is a small description, never a message list.** The page holds and
  sends folder entries (folder id, include-subfolders flag, excluded message ids) and
  individual picks. It never receives or sends every message of a folder. Counts are
  computed server-side from the cached folder tree (§4.4). Proven (MA-409, MA-432).
  Proposes R29.
- **P4 — The server trusts no id or URL the page sends.** Individual picks and
  exclusions are accepted only as short opaque **row handles** that this session issued
  in a listing (XC17). Microsoft 365 folder ids are accepted only if they are in the
  session's cached tree (XC17). List cursors are opaque server-side handles. A Graph
  `nextLink` is never handed to the page, and one that points anywhere other than the
  client's own base URL is refused before any request. A redirect that leaves the base
  scheme, host and port is refused too (XC26). Against a misbehaving or compromised
  server, the pin and the redirect refusal are defence in depth. Against a TLS-intercepting
  proxy they do nothing: that proxy already holds the token, which is today's trust (XC26).
  **Second admission path (XC2):** an id is also accepted if it is in this app's own run
  record (§4.19) for the run being retried, under the same signer (R-l otherwise). The
  destination's `export-manifest.json` is never an admission source. A selection is bound
  to the signer (or archive path) it was made under. Proven (MA-401, MA-410, MA-411,
  MA-437). Serves R17; proposes R29.
- **P5 — Preview is a pure dry run, and the run copies exactly what was previewed.**
  "Preview copy export" resolves the selection, sizes it and names the target paths. It
  writes nothing to the destination or the archive. It writes nothing to the config
  directory either, except the sign-in token's normal refresh: a long preview that crosses
  the access token's expiry persists the rotated token (XC30). MA-412 excludes the token
  entry from its tree hash and asserts that write separately. The run takes
  only a preview id and copies exactly that resolved list. A message that arrives after
  the preview is not copied; one that disappears is a listed failure. Proven (MA-412,
  MA-413, MA-415). Proposes **X14**.
- **P6 — Every run writes a new export directory, contained, never overwriting.** The
  export directory is created with an exclusive `os.Mkdir` under a unique name. Every
  later write goes through an `*os.Root` opened on that new directory (Go 1.26,
  `go.mod:3`), so no write can escape it by `..`, an absolute path or a symlink. No
  existing file is ever opened for writing. **No existing file is ever replaced (XC13):**
  every final placement is a no-replace rename (§4.9). The one exception is finishing
  this app's own interrupted run, whose root identity matches its run record (XC2,
  §4.19). Proven (MA-416, MA-417, MA-418). Serves R4, R5; proposes R29.
- **P7 — Names are portable, distinct and bounded.** Folder and file names come from
  `util.SanitizeSegment` / `util.SanitizeFilename` / `util.Slug`
  (`internal/util/paths.go:47,91,112`). Siblings are de-duplicated on an NFC +
  Unicode-casefold key, widened with the NTFS/exFAT upcase-table equivalents (ı/I, XC13).
  Names that match the 8.3 short-name pattern are disambiguated (XC13). **Relabelled
  (XC13):** names never *silently* merge. A collision the key misses meets the no-replace
  rename and fails that message, and the failure is listed. The subject slug shrinks
  before any path passes 250 UTF-16 units. Proven (MA-417, MA-418, MA-441) for traversal,
  illegal characters, the current reserved list, case collisions and length.
  **[ruling Q15]** The rest (the missing reserved names, F111; bidi controls, F75; the Linux
  255-byte segment limit, F91) is handled for exports by an export-only, stricter naming
  pass that E2 builds over `util.SanitizeSegment` and `util.SanitizeFilename` (layered over
  them, never a copy of them), so MA-417's sub-cases pass with no archive migration. The
  archive writer's own names keep F75/F91/F111: that is an open gap, not accepted, and E2
  lands a skip encoder citing R4 for it when MA-417's sub-cases turn green. On Windows,
  `os.Root` refuses reserved device names, so such a name is never a write to a device.
  Serves R4, R6.
- **P8 — The original is byte-exact or absent, never synthesized.** Microsoft 365
  source: the `.eml` is the exact byte stream Graph's `/$value` returned, hashed as it is
  written. **Conditional:** these are the MIME bytes Exchange Online provides at export
  time. They are not guaranteed to equal the bytes that arrived over SMTP, and two
  downloads of the same message are not guaranteed identical. The hash proves integrity
  from the moment of export onward, nothing earlier (lab row MA-443 measures stability).
  Archive source: a record's preserved `<stem>.eml` is copied only after the gate
  `extract` uses (validated path, no symlink at any level, regular file, nlink 1 (XC19),
  bounded read, `Fixity.EML` match when recorded). "The same gate" is true only once
  `extract`, the `Exporter` and the workspace share one archive-read helper. Until then
  §6 states the fork and its owner (XC36). A record without one gets no `.eml`, and the
  manifest says why. Proven (MA-419, MA-420). Serves R20's posture; proposes R29.
- **P9 — A manifest with hashes that a recipient checks with stock tools.** Every
  export directory carries `export-manifest.json` (run metadata, one entry per selected
  message, every file with role, SHA-256 and size, every failure) and `SHA256SUMS.txt`
  in coreutils format, which `sha256sum -c`, `shasum -a 256 -c` and a one-line
  PowerShell check (printed in `README.txt`) can verify without this tool. The PowerShell
  form is **lab-pending (MA-442)**, not proven (XC14). **Integrity** against accidental
  damage is proven for the coreutils forms (MA-421, MA-422). **Limit, stated here
  (XC14):** the check proves every *listed* file is unchanged, or names the one that
  changed or is missing. It does not detect files *added* to the folder, so README gives a
  stock extra-files check per OS. **Tamper evidence is conditional** on the sender passing
  the *export fingerprint* to the recipient out of band. The fingerprint is the SHA-256
  of `SHA256SUMS.txt`, computed from the in-memory bytes at seal time and never re-read
  from disk (XC1, §4.19), and shown on the Export activity page. It is also conditional on
  the sender still having that fingerprint. It is kept in a fingerprint ledger that
  compaction never prunes, and can be saved as a receipt; losing the ledger loses the
  sender's anchor (XC14). **Authenticity** (who made it) is not claimed: there are no
  signatures (§3). Proposes R29.
- **P10 — Readable HTML comes from the archive's sanitizing renderer and is labelled
  derived.** With "Readable HTML" on, each message also gets `<stem>.html` from
  `export.RenderWith` (`internal/export/html.go:115`; citation refreshed, XC30): the same parse → neutralize →
  re-parse fail-closed path the archive uses (MA-300/MA-301), the same `ArchiveCSP` meta
  first in `<head>`, inert offline. An archived page copied from the archive (archive
  source, no `.eml`) is re-checked with the same live-meta detector. If it carries a
  live refresh (a page rendered before the F1 fix), it is copied as
  `<stem>.archived-page.txt` (inert) with a note, never as `.html`. Proven (MA-423).
  Serves R19, R7.
- **P11 — Extracted attachments are decoded convenience copies, labelled derived.**
  With "Extract attachments" on, each attachment the MIME parser yields is written as a
  loose file under `<stem>.attachments/`, with sanitized, casefold-unique names. The
  `.eml` stays the authoritative copy. **Conditional:** byte-faithful only once the
  parser fixes for C1-1 (non-UTF-8 text attachments re-encoded), F24 (unknown
  charset/encoding drops parts) and F25 (inline-disposition parts unnamed or dropped)
  land. Until C1-1 lands, the option is not shipped (§10, [ruling Q9]: E6, after
  mime-fidelity S2, which carries the C1-1 fix). Proven for names and
  containment (MA-424); MA-425 is the C1-1 encoder (xfail). Serves R4, R1.
- **P12 — A run finishes around bad messages and names them.** Each message's outcome
  is classified (§4.13): per-message *retryable*, per-message *terminal*, or *run-fatal*.
  Only run-fatal classes stop the run (sign-in refused, cleared or changed; destination
  gone or full; archive gone; no progress for 5 minutes; cancel; XC7, XC9, XC10). A
  transport-class error never fails a message, because it backs off on the same message
  (XC9). The message in flight at a run-fatal stop is listed in `failures`, never first in
  `not_attempted` (XC9). Every failure is listed with subject, folder, received time and
  reason. A render-only problem is counted as "exported without a readable copy", not as
  "not exported" (XC35). The outcome uses `extract`'s partition: complete = 0,
  partial = 3, failed/refused = 1. Proven (MA-426, MA-427, MA-440). Serves R1, R10, R12,
  X1.
- **P13 — Cancel is clean and visible.** Cancel stops after the in-flight message
  (aborting its download and removing its temp file). The directory keeps
  `_EXPORT-NOT-FINISHED.txt`, rewritten to say CANCELLED with counts, plus a manifest
  that lists exactly what was written and what was not attempted. Proven (MA-428).
  A stalled token refresh is bounded by the F15 fix, which is now required in E1
  (`http.Client.Timeout`, XC26). An app stop cancels and finalizes through `Drain` (XC6).
  **Conditional:** on Windows that needs the end-session window. Without it, logoff,
  shutdown and an MSI upgrade are a kill, recovered by "Finish as partial" (XC2).
- **P14 — An unfinished export is marked unfinished on disk.** The marker is written
  and fsynced before any message. It is removed only after `SHA256SUMS.txt` is committed
  and the run record is sealed. **Restated (XC5):** any export directory that contains a
  message file carries the marker or is complete. A W2–W4 kill (§5.1) leaves an empty or
  temp-only root, which is reconciled as interrupted. README's and the manifest's status
  are conditional in their own text on the marker being absent, so no kill point leaves a
  document claiming "complete" beside the marker. Proven (MA-429, which proves the
  **ordering** by fault injection). **Conditional, stated here (XC5):** this holds across
  process death. Across power loss or a yanked stick it holds only on filesystems that
  order metadata (NTFS, APFS, ext4 data=ordered). On FAT32/exFAT the recipient's
  `sha256sum -c` is the backstop, and the app says "Eject before unplugging". Serves R5;
  proposes R29.
- **P15 — Exports never block backups, and backups never block exports.** A Microsoft
  365 export does not touch the archive. An archive-source export reads a lock-free
  manifest snapshot (the `status` precedent, `internal/health/health.go:497-499`) and
  verifies every file it copies against the snapshot's fixity, so a scheduled capture
  that fires mid-export runs normally. Proven (MA-431). **Conditional (XC30, XC20):**
  - A concurrent full capture or gap fill can fail individual messages as "changed in the
    archive during export" (listed, retryable).
  - On Windows the snapshot must be read through a share-delete open (the archive
    `os.Root`, §4.3). Until the Windows lab sub-case of MA-442 shows a capture saving its
    manifest during a reload, P15 is unproven on Windows.
  - A Microsoft 365 export runs beside a capture only as far as F17 allows (§4.14). F17 is
    fault-isolation's slice FI-GF17 [ruling Q15].

  Serves R5, R14, R18.
- **P16 — Large selections stay bounded.** Hard caps (200,000 messages per run; 10,000
  picks plus exclusions per selection; 100-row list pages; one live preview). Per-message
  state goes to the local run record (XC1), never to an in-memory slice. Originals stream
  to disk. **Conditional (XC30):** originals are streamed when Readable HTML is off. With
  it on, one message's MIME plus its decoded attachments are held, bounded by the 200 MiB
  cap, and MA-419 runs in both modes. The session's caches are byte-capped and released
  after 30 idle minutes (XC18). Proven (MA-432). Proposes R29.
- **P17 — The workspace surface is guarded end to end.** Every workspace endpoint is a
  POST through `GuardLocalPOST`, including the reads (folder trees, message lists,
  progress), because they return mailbox content. The two HTML pages carry no mailbox
  data. Mail-derived text reaches the page only as text (`textContent`), never as
  markup. Proven (MA-405, MA-407, and MA-378 from E0). **Conditional** on E0, which is
  `docs/design-local-confidentiality.md` slices **C1** (Host allow-list on every method,
  `localweb.Mux` route registry) and **C2** (per-launch per-user session under
  `/s/<P>/`, instance file), merged before E3 (XC23). Until E0 lands, any local account
  can scrape the CSRF token (F3), so the workspace is not shipped before it (§10, [ruling
  Q5]).
  Every workspace route registers through `localweb.Mux`, and every page, fetch and
  sidebar URL is relative (XC23). Proposes **X13** (scoped to the workspace and export
  routes, XC24) and **X15**; serves X9, R19.
- **P18 — The destination is chosen with the OS folder picker or typed.** "Browse"
  opens a native folder dialog in the dashboard process (`zenity.SelectFile(
  zenity.Directory())`). zenity is in `go.mod:10` and ships in the MSI through
  `mailarchive-gui`, but it is **new to the `mailarchive-desktop` binary** (XC31). On
  Windows the dialog loads shell extensions into the dashboard process, so Browse is
  disabled while an export runs; a typed path still works (XC31). There is no
  directory-listing API (argument in §4.8). "Create folder" makes exactly one new,
  sanitized directory under an existing destination. Proven (MA-433, MA-434). Serves X9.
- **P19 — One legible activity surface; the run record is the app's truth.** The Export
  activity page shows the running export (counts, bytes, current folder, ETA, Cancel),
  served from the in-memory run slot. It also shows a bounded history (status, counts,
  fingerprint, path, actions) from a 0600 journal. **Inverted (XC1, XC4):** the per-run
  **run record** (§4.19) is the app's system of record, and the export directory is the
  recipient's. Reconciliation reads status only from the journal and the run record. It
  probes the export directory only for presence, the marker and identity, never parses it
  for status, ids or hashes, and only ever downgrades. Proven (MA-430, MA-446, MA-448).
  Serves X6, X10, X11.
- **P20 — What an archive-source export can contain is stated before the run.** The
  preview says how many selected records have an original `.eml` and how many will be
  copied as the archived page plus attachment zip. The panel note changes per source
  (§4.1). Proven (MA-420, MA-435). Serves R20, R1.

**Proposed R29 — Copy export is separate, contained, faithful-or-absent and
self-verifying.** An Extraction-workspace export reads Microsoft 365 (GET-only, the
signer's own mailbox) or an archive, the configured one or another archive folder the user
chooses (read-only, never locked, never written) [ruling W1]. It
writes ONLY inside a newly created export directory under the user-chosen destination,
plus its own 0600 run record in the local state directory (XC1). It never writes into an
existing export, except to finish this app's own interrupted run when the root's identity
matches its run record (XC2). It never writes into the archive. Every name derived from
mail is
sanitized, contained (`os.Root`) and case-insensitively distinct. Each message's
original is copied byte-exact or recorded absent, never synthesized. Derived files are
labelled derived. A manifest lists every selected message and file with its SHA-256,
and `SHA256SUMS.txt` lets a recipient verify with stock tools. A preview that writes
nothing precedes every run and binds it. A run that does not finish keeps its
`_EXPORT-NOT-FINISHED.txt` marker. One message's failure never stops the run and is
listed with its reason.

**Proposed UX rules** (allocated ids [ruling Q11 = J1]; local-confidentiality's
X-CONF-LOCKED is X12). **X13** — mailbox-content reads are guarded like writes. **Scoped (XC24):** every
workspace and export endpoint that returns mailbox or archive content (folder names,
subjects, senders, paths, progress) is a POST with CSRF + loopback Origin/Host (+ the E0
session), never an open GET. **State: Partial.** The shipped GET endpoints
`/api/activity` (log lines and the device code), `/api/health` and
`/api/schedule-state` stay GETs. They are recorded as an X13 condition and assigned to
E0/C2, whose session then guards every method. **X14** — look before writing outside the archive: a
dashboard action that writes to a user-chosen location first shows a dry run (what, how much,
where) that writes nothing, and the write executes exactly the previewed plan. **Scoped
[ruling J7]:** the rule covers the dashboard's actions; the CLI's `extract -dest` writes
without a dry run and is outside it. **X15**
— mail-derived text is inert in the control surface: subjects, senders and folder names
reach the dashboard DOM only via `textContent`; JSON responses carry `nosniff` and
`Cache-Control: no-store`.

**Proposed scenario S52** — a user copies selected folders and messages from Microsoft
365 or the archive to a folder on disk with the Extraction workspace (R29, R4, R5, R17,
R19, R20).

## 3. Non-goals (explicit scope cuts)

- **Live access to other people's mailboxes:** shared mailboxes, delegate access,
  app-only multi-mailbox export (OW3). There is no mailbox-address input. **Scope note
  (XC21):** the *archive* source exports whatever mailboxes the configured archive holds.
  An app-only capture writes one store per mailbox (R17). The workspace therefore says,
  per source, whose mail it reads, and records each store's mailbox in the manifest.
- **Drag-and-drop from Outlook** (OW4) and any Outlook add-in.
- **Other output formats:** PST, `.msg`, mbox (the CLI `extract -format mbox` already
  covers the archive), PDF. **[ruling ESC-5]** This does not declare the desktop design's P7
  ("Export a copy (extract mbox/eml)") superseded. Whether it is, is escalated to the owner;
  until then P7 stays an approved, unbuilt desktop item.
- **Legal-grade chain of custody:** digital signatures, trusted timestamps, legal hold,
  eDiscovery search (KQL), tamper-proof audit logs. SHA-256 hashes give integrity, and
  tamper evidence with the out-of-band fingerprint (P9). Authenticity is not claimed.
- **Search-based or date-range selection** (later; v1 is folders plus picks).
- **Changing the mailbox or the archive in any way**: no move, flag, delete, or feeding
  an export back into the archive.
- **Incremental or scheduled exports.** A copy export is one-off and never schedulable.
- **Cloud destinations.** The destination is a local, mounted or UNC path. A
  cloud-synced folder gets a warning, not a refusal. The warning is **best-effort
  detection** (XC32): OneDrive, Dropbox (including "Dropbox (<team>)" roots), Google
  Drive, iCloud Drive / `iCloudDrive`, macOS iCloud Desktop & Documents, and Box. Other
  providers are not detected.
- **The mockup's in-page destination folder tree** (replaced by the OS picker, §4.8;
  [ruling Q1]).
- **Recoverable Items, the online archive mailbox, public folders, calendar, contacts,
  tasks.**
- **Parallel downloads.** v1 downloads one message at a time, like capture [ruling Q7].
  Parallel downloads wait for MA-443's measured throughput.
- **Deleting or cleaning up exports from the app.** The app never deletes in the
  destination, not even residue of a crashed run (§5). Finishing its *own* interrupted
  run, from its run record, is not cleanup. It only writes README, manifest and sums,
  rewrites its own marker, or removes its own marker after "Check and finish" (XC2, XC5).
- **A CLI verb.** The engine lives in `internal/app` so one can be added later with the
  §4.13 exit partition.
- ~~An archive other than the dashboard's configured archive location~~ — **withdrawn
  [ruling W1]:** the archive source also accepts another archive folder, read-only (§4.3).
- **Re-rendering an archived page that has no `.eml`.** It is copied, or quarantined as
  `.txt` (P10).
- **Fixing the MIME parser limits** (C1-1, F24, F25, F26). Owned elsewhere; this design
  depends on them and states their effect.
- **Localization** of the page or of the export's `README.txt` (English only).

## 4. Mechanism

### 4.1 The owner's mockup, element by element

| Mockup element | Maps to (this design) |
|---|---|
| Sidebar "Extraction workspace" | New route `workspace` (§4.2) in a new sidebar group "Export". Every link is relative, so it works under E0's `/s/<P>/` prefix (XC23). |
| Sidebar "Microsoft 365" | The existing "Microsoft 365 connection" card (`./#sec-m365`, relative; `dashboard.go:358`), linked from every page, extended per §4.16. The label stays "Microsoft 365 connection" (X11, MA-302 continuity). |
| Sidebar "Export activity" | New route `exports` (§4.16). |
| Banner "Selected 1766 messages from Inbox." | The transient status line after "Select current folder": "Selected 1,766 messages from Inbox (and its 3 subfolders)." The count comes from `POST /api/workspace/count` (§4.4). |
| Panel "1 Mailbox source" | Source chooser: "Microsoft 365 — your mailbox (alex@contoso.com)", "MailArchive archive — C:\MailArchive (holds: alex@contoso.com, bob@contoso.com)" (the configured archive location), or **"Another archive folder… (read-only)"**, chosen with Browse or typed [ruling W1]. Each archive source names every mailbox its stores hold, and the tree shows each store's mailbox (XC21). |
| Text box "Blank = my mailbox; or shared mailbox address" | **Omitted (OW3).** In its place, a read-only "Mailbox: alex@contoso.com (your own mailbox)" row and the note: "Shared mailboxes aren't available in this version. MailArchive signs in with read access to your own mailbox only." |
| "Load folders" | `POST /api/workspace/folders` → the whole folder tree with counts, rendered lazily (§4.3, §4.5). If not signed in, it shows the sign-in prompt instead (§4.16). |
| Folder tree with checkbox, expand arrow and count per folder | Each node: checkbox (whole-folder entry), arrow (expands from the cached tree, no request), `(N)` = Graph `totalItemCount` (archive: manifest record count). Deleted Items and Junk Email are listed, labelled and unticked, and included only when ticked [ruling Q2]. |
| "Include subfolders" checkbox | Sets the subtree flag of the entry created when a folder is ticked or "Select current folder" is clicked. Descendants then show as ticked (greyed, inherited). Toggling it later does not rewrite existing entries; the summary names each entry. |
| "Select current folder" | Adds a whole-folder entry for the folder whose messages are listed (with the subtree flag above). |
| Message list (columns Message = subject + sender, Received; checkbox per row; select-all) | `POST api/workspace/messages` → one page of 100, newest first, plus an opaque cursor. Each row carries a short opaque **row handle** (XC17). A row checkbox is an individual pick, or, inside a ticked folder, an exclusion when unticked. Select-all toggles the currently visible loaded rows only. |
| "Filter loaded messages by subject / sender" | Client-side filter over loaded rows only. The placeholder says "loaded" (the MA-120 honesty rule). It never queries the server. |
| "Load more messages" | Next page via the opaque cursor, whose TTL refreshes on use. An expired or evicted cursor continues with a keyset query (`receivedDateTime lt <last loaded row>`, same order), or shows R-x (XC18). It shows "Showing 300 of 1,350" and hides at the end. |
| Panel "2 Export destination", path box, "Browse" | Typed absolute path, or `POST /api/workspace/browse` → native OS folder picker (§4.8). |
| Destination folder tree ("Click an arrow to expand folders…") | **Not built (§4.8, [ruling Q1]).** The OS picker covers it, including its own "New folder". |
| "New subfolder name" + "Create folder" | `POST /api/workspace/mkdir` creates exactly one sanitized directory under the current destination and puts its path in the box (§4.8). |
| "Preserve folder structure" | Option, default ON: `messages/<folder path>/…`. OFF: `messages/` flat, with the folder recorded in the manifest. |
| "Extract attachments" | Option, default OFF: loose decoded files under `<stem>.attachments/` (§4.12). Hidden until C1-1 lands (E6, [ruling Q9]). |
| "Readable HTML" | Option: `<stem>.html` via `export.RenderWith` (§4.11). **Hidden until slice E5 lands, and `exportHTML` defaults OFF until then (XC28).** It defaults ON from E5 on. |
| Note "Always exports original EML and a manifest with hashes. Each run creates a separate export directory." | Microsoft 365 source: shown as written. Archive source: "Exports the original EML where your archive kept one (N of M selected); otherwise the archived page and attachments. Always writes a manifest with hashes. Each run creates a separate export directory." (P20, [ruling Q8]) |
| Footer "2088 messages selected" | Server-computed count (§4.4). After a preview it says "2,088 messages selected (2,086 found — see preview)" if the two differ. |
| "Clear selection" | Clears all entries (asks to confirm when there are any). |
| "Preview copy export" | `POST /api/workspace/preview` → a dry-run job; the preview panel then offers **Start export** and **Back** (§4.7). |

### 4.2 Pages and endpoints

Both pages are served by `DashboardHandler` under `dashboardHeaders` (`dashboard.go:169`:
`dashCSP`, nosniff, no-referrer), share the sidebar partial, and load one static script
each (`workspace.js`, `exports.js`) under `script-src 'self'`, with no inline handlers.
**Every route registers through `localweb.Mux` (E0 slice C1, XC23).** Every page,
fetch and sidebar URL is **relative**, with no leading `/`, so the pages work under C2's
per-launch `/s/<P>/` prefix. No text assumes fixed :8097/:8099 (XC23). The pages are
shells: no mailbox data is in the HTML. All data arrives through these POST endpoints.
Each one checks `r.Method == POST` (else 405), then `GuardLocalPOST` (else 403, with
nothing done), then the E0 session. It caps its body at 1 MiB (`http.MaxBytesReader`).
A 413 maps to an R-j-style refusal, never a bare status. That cap holds 10,000 picks
because picks and exclusions are row handles, not Graph ids (XC17). It replies with JSON
plus `Cache-Control: no-store`. E0's structural MA-378 probes every registered route
unauthenticated and with a hostile Host, which replaces revision 1's private route walk.
MA-405 keeps the method and guard twins, and MA-406 is deleted (XC23).

| Endpoint (POST; paths are relative to the surface root, which E0 puts under `/s/<P>/`, and the page fetches them relatively, XC23) | Does | Side effects |
|---|---|---|
| `/api/m365/signin` | Sign in only, as a job in its **own sign-in slot**, under one process-wide sign-in mutex shared with capture's device flow, so two device flows never run at once (XC8). It runs `graph.NewDelegated` + `Client.Me` against an in-memory store first, then persists, after the XC7 confirmation when a weekly backup is installed and the UPN differs | token stored (vault/file) after the confirmation |
| `/api/m365/signin-status` | Polls the sign-in job: the device code, then the result. A guarded POST, never `GET /api/activity` (XC8) | none |
| `/api/workspace/source` | Describes the sources' availability (signer UPN or "sign-in needed"; archive path or "no archive yet"). **[ruling W1]** With `{otherArchive: path}` (from `browse` or typed) it validates that folder as §4.3 describes and returns its mailboxes, or R-o3 / R-p | none (reads only, in the chosen folder too) |
| `/api/workspace/folders` | Folder tree with counts for the chosen source | Graph GETs; cache |
| `/api/workspace/messages` | One page (100 rows) of a folder, newest first, plus an opaque cursor | Graph GETs or one index read; seen-set |
| `/api/workspace/count` | Count for a selection description | none (cache only) |
| `/api/workspace/browse` | Opens the native folder picker and returns `{path}`, `{canceled}` or `{unavailable}` | an OS dialog |
| `/api/workspace/mkdir` | Creates one directory `{parent, name}` | one `os.Mkdir` |
| `/api/workspace/preview` | Starts the dry-run job and returns `{previewId}` | Graph GETs; memory only |
| `/api/workspace/preview-status` | Polls the job → progress or result | none |
| `/api/exports/start` | `{previewId}` → `{runId}`; starts the run | export directory, files, run record, journal |
| `/api/exports/cancel` | `{runId}` | cancels the run's context |
| `/api/exports/progress` | The running export only, served from the in-memory run slot. It never touches any path (XC4) | none |
| `/api/exports/history` | The cached journal view, kept by the background reconciler (XC4) | none |
| `/api/exports/open-folder` | `{runId}` → reveals the recorded export directory after the identity check, never launching anything (XC33) | `open -R` / `explorer.exe /select,` / `xdg-open` on the verified directory (helper moved from `cmd/mailarchive-gui/main.go:1184`) |
| `/api/exports/retry` | `{runId, which: "failed"\|"rest"}` → a new preview from that run's **run record**, never the destination manifest (XC2) | memory only |
| `/api/exports/finish` | `{runId}` → "Finish as partial" for an interrupted run, or "Check and finish" for a sealed one, from its run record (XC2, XC5) | writes README, manifest, sums and marker in its own root only |
| `/api/exports/check` | `{runId}` → "Check this export": re-hash against `SHA256SUMS.txt`, compare the sealed fingerprint, list unlisted files (XC14) | reads only |

New files: `internal/desktop/workspace.go` (session, selection, count, preview store,
endpoints), `internal/desktop/workspace_page.go` (templates and scripts),
`internal/desktop/exports.go` (run slot, activity, journal, run record, background
reconciler), `internal/desktop/picker.go`
(the zenity seam: `var pickFolder = func(ctx, title string) (string, error)`). Engine:
`internal/app/copyexport.go` (plan, writer, naming, manifest, outcomes),
`internal/app/copyexport_graph.go` and `internal/app/copyexport_archive.go` (source
adapters). Graph: `internal/graph/workspace.go`. Index: one read query in
`internal/index/query.go`. Util: `internal/util/diskfree_{unix,windows}.go` (free bytes
via `x/sys`), `util.FoldKey` (NFC + `x/text/cases.Fold`).

### 4.3 Sessions: the Graph session and the archive snapshot

- **Graph session (one per dashboard process).** It is built on first use through a new
  exported wrapper, `app.OpenGraphSession(ctx, g GraphOptions) (*graph.Client, upn
  string, err error)`, which is `buildGraphClient`'s device branch
  (`internal/app/graph.go:349-377`) forced to `Auth: "device"` with `Unattended: true`
  (no prompt; a missing sign-in returns R-b). Interactive sign-in happens only through
  `/api/m365/signin`.
  - **Context (XC6).** The session, and every preview and run, is built on a
    **process-lifetime context**. `DashboardHandler` receives main's root context, never a
    request's `r.Context()`. `NewDelegated` keeps its context inside the refreshing token
    source (`delegated.go:66,101,187`), and oauth2 reuses it on every refresh. Each request
    and each run passes its own derived context to Graph calls, for cancel and deadlines.
  - **What the session holds.** The client. The signer UPN (read from `GET /me`, MA-253).
    The folder tree (refreshed by each "Load folders"; M365 folder ids from the page must
    be in it, XC17). A **seen-set** of listed message metadata (row handle → id, subject,
    sender, received, size hint, folder id from `parentFolderId`; LRU with an entry cap of
    250,000 and a byte cap, folder ids interned). And **cursors** (opaque random id → the
    Graph `nextLink` plus the "honored" immutable-id flag; cap 64; a 10-minute TTL that
    refreshes on use).
  - **Seen-set role (XC17).** It is the gate for row handles the page sends back.
    Preview resolution never adds to it, and the handles of the live selection are pinned
    against eviction. Retry never uses it (XC2).
  - **Idle release (XC18).** After 30 minutes with no workspace request and no live
    preview or run, the session drops its tree, seen-set and cursors, and the page shows
    "Load folders" again. This replaces revision 1's "lives for the process".
  - **Sign-in changes (XC7).** Clearing the sign-in (`dashboard.go:139`), or a sign-in as
    a different UPN, drops the session. Every outstanding selection and preview is then
    refused (R-l), and a **running** Microsoft 365 export stops with the run-fatal class
    "sign-in cleared or changed" (R-z), finalized and retryable from its run record. The
    run also reads `graph.SignedIn` (local, no network) every 100 messages, which catches
    sign-ins made outside the dashboard (capture, CLI).
  - **Token write-back (XC7).** The capture keeps building its own client inside
    `RunGraph`. Two token sources share one refresh token. In revision 1 that relied on
    `deviceSource`'s reload-on-failure (`internal/graph/delegated.go:190-222`) and
    `putIfNewer` (MA-258), but those resurrect a cleared store and ignore the UPN. Revision
    2 makes write-back identity-safe, in `internal/graph` and therefore in capture too:
    - `putIfNewer` persists only when the stored entry exists, has the source's UPN, and
      has an older expiry. It never re-creates a cleared store. `NewDelegated`'s
      first-sign-in `put` is unchanged.
    - `deviceSource.reload` refuses a stored token whose UPN differs from the source's.
    - This ships with capture rows beside MA-258 (MA-515).
- **Archive snapshot.** For the configured archive location (`Config.effectiveOut`,
  `internal/desktop/settings.go:53`): a load of `.mailarchive-manifest.json` **without**
  the archive lock. Manifest saves are temp-plus-rename, so a reader always sees a whole
  file, and `health.Gather` already relies on this.
  - **Share-delete read (XC20).** The manifest bytes are read through the archive
    `os.Root` (a `FILE_SHARE_DELETE` open on Windows), never `os.ReadFile`. On Windows
    `os.ReadFile` opens without share-delete (`syscall_windows.go:395`), so a concurrent
    capture's `Manifest.Save` rename (`manifest.go:1422`) would fail. The `health.Gather`
    precedent carries the same latent defect, which is noted here and not fixed by this
    design.
  - **Caching and tree.** The snapshot is cached keyed by (path, size, mtime) and released
    on idle (XC18). The tree is store token → folder path, from each record's key prefix
    and current `Folder`, and each store is labelled with its mailbox (XC21). Counts are
    record counts.
  - **Message rows.** They come from a new
    `index.ListFolder(store, folder, offset, limit) ([]ListRow, total, error)` (exact
    folder match, date descending, returns the manifest `key`). The index is opened with
    `mode=ro` / `PRAGMA query_only` (XC20) and **closed at the end of each request**,
    never held.
  - **Index version (XC19).** Before listing, the index meta version is read. A newer
    version is refused with R-p wording. An older version, or a snapshot with
    `Rekeyed > 0` (a pre-v3 manifest re-keyed in memory), maps index keys through
    `state.MigrateKey` in memory. If that is not possible, the list is disabled with "this
    archive's search index predates its format; whole-folder selection works; Rebuild
    search index fixes the list".
  - **Rebuild coordination (XC20).** A list request holds a shared lock that the
    in-process Rebuild takes exclusively around its rename, or listing is refused while a
    Rebuild job runs. So the workspace never holds `search.db` across requests, and never
    inside Rebuild's rename window. This narrows revision 1's "does not add to F5".
  - **Errors.** A load error is shown verbatim (newer format, corrupt: the MA-132/MA-159
    wording).
- **Another archive folder (read-only) [ruling W1; Q3, owner-direct].** The source chooser
  also offers "Another archive folder…", picked with Browse (§4.8) or typed. The configured
  archive location stays the default archive source. An inheritor reads an inherited
  archive this way without repointing Settings (§7.4).
  - **Nothing is ever written there.** No `.mailarchive.lock`, no manifest save or load-time
    migration on disk (`state.Load`'s re-keying stays in memory), no `RepairKeys`, no SQLite
    `-wal`, `-shm`, journal or checkpoint, and no temp file. The manifest snapshot is read
    as above, through an `os.Root` on the chosen folder. `search.db` is opened only with the
    URI parameters `mode=ro&immutable=1`, which make SQLite take no lock and create no
    sidecar. If that open fails, or the medium is read-only, the message list is unavailable
    and whole-folder selection works from the manifest, as for a read-only copy (F85). If a
    `-wal` sits beside `search.db`, `immutable=1` does not read it, so the list says "recent
    messages may be missing from this list; whole-folder selection includes them".
  - **It never touches Settings or the weekly backup.** Choosing it changes no setting;
    XC22's Settings-save guard stays for users who do repoint Settings.
  - **The folder is untrusted input.** §4.6's gates apply unchanged: validated relative
    paths, the component-wise Lstat walk, the nlink check, reads through `os.Root`, bounded
    reads and fixity. A folder with no `.mailarchive-manifest.json` is refused (R-o3), and a
    newer manifest or index format is refused (R-p). The selection is bound to the folder's
    path and identity, as R-l binds a signer.
  - **Proven** by MA-531 (by bytes: after preview and run, every file under the folder is
    byte-identical and no file has been added, read-only media included), which is stricter
    than MA-436's row compare.

### 4.4 Selection model and counts

```json
{"source":{"kind":"m365","mailbox":"alex@contoso.com"},
 "folders":[{"id":"AAMk..Inbox","subtree":true,"exclude":[]},
            {"id":"AAMk..Sent","subtree":false,"exclude":["h7Qx2", "..75 more"]}],
 "messages":[{"h":"k3Zp9"}]}
```

- **Entries.** A folder entry is `{id, subtree, exclude[]}`, where `exclude` holds row
  handles. A pick is `{h}`, a **row handle** (XC17): a short opaque id (about 6–10
  characters) that the server issued for a listed row and resolves through the seen-set
  to (folder, Graph id or manifest key). Revision 1 sent `{folder, id}` with full Graph
  ids (about 200–300 bytes per pick), so the 1 MiB cap tripped at about 3,500 picks, well
  below R-j. A Microsoft 365 folder id is accepted only if it is in the session's cached
  tree. That refuses well-known names the page never saw, such as
  `recoverableitemsdeletions`, and `..` (XC17). Each resolved message's folder comes from
  Graph's `parentFolderId`, not from the request. Archive-source folder ids are
  `"<store>/<folder path>"` (JSON-escaped; validated against the snapshot; never used to
  build a path).
- **Resolution** (at preview time, server-side): folder set = each entry's folder, plus
  every descendant in a **freshly fetched** tree if `subtree` (a folder created since
  "Load folders" is included; a vanished one is a preview WARN), de-duplicated across
  entries. Message set = every message in that folder set, minus exclusions, plus picks
  not already inside it, de-duplicated by id. Order: tree order, then newest first.
- **Counts** (`/api/workspace/count`, cache only, no Graph call): Σ over the
  de-duplicated folder set of `totalItemCount` (archive: record count) − exclusions + picks
  outside the set. The page calls it, debounced, after every change, and shows it in the
  footer and the banner. It is labelled as a count of *items Microsoft 365 reports*. The
  preview gives the exact message count and shows both when they differ (a
  `totalItemCount` can include items `/messages` does not return).
- **Bounds.** Picks plus exclusions ≤ 10,000 per selection (R-j), and as row handles they
  fit within the 1 MiB body cap (XC17). A pick or exclusion handle must be in the
  session's seen-set (R-j2), so the page cannot inject ids it was never shown. Retry ids
  are admitted by the run record instead (XC2). A message present in two entries
  (impossible within one Exchange snapshot, possible if it moves between listing and
  preview) is copied once.
- **Where the selection lives.** Only in page memory (lost on reload; a `beforeunload`
  prompt warns). It reaches the server at count and preview time, as the description
  above. After a dashboard restart, or once the session is released on idle, the page's
  handles no longer resolve, and every POST says so with R-y ("MailArchive Desktop
  restarted — reload the page; your selection can't be reused") or R-x. It is never a
  bare 403 (XC18).

### 4.5 Graph client additions (slice E1)

All in `internal/graph`, all GET, all with the existing `Prefer: IdType="ImmutableId"`
header (`graph.go:542`) and throttling retry (`graph.go:531`).

**One Graph layer (XC25) [ruling J4].** E1 depends on `docs/design-fault-isolation.md`
slice **SG** (not S3), which fault-isolation owns and builds once, so there is one owner and
one type:
- `StatusError{Code int, GraphCode string, Text string}` (its §4.8.1): `Text` bounded, and
  `Error()` keeps today's exact text, as MA-402 asserts;
- `graph.Classify`, one classifier for both consumers;
- `MIMETo(w, maxBytes, idle, overall)`, one bounded download (idle 2 min, overall 2 h, size
  cap 200 MiB), whose `$value` request has its own response-header timeout of at least the
  idle window, for both consumers;
- the nextLink guard: a repeat set and a page cap in every paging loop, and the host/path pin
  below.

E1 adds only `FolderTree`, `FolderCount`, `MessagePage` and the use of SG's guard with the
host/path pin on from day one. Revision 1's claim that capture's `Folders`, `Messages` and
`MIME` "keep their behaviour … unchanged" is withdrawn: SG and S3 change them, with their
own capture rows, and capture's host/path pin is fault-isolation's step **FI-SGc** [ruling
J4], still gated as XC25 says (below).

- `FolderTree(ctx, userID string, max int) ([]FolderInfo, error)`. Same walk as
  `Folders` (`$top=100`, `childFolders` recursion) with
  `$select=id,displayName,parentFolderId,childFolderCount,totalItemCount`. **No**
  Deleted Items / Junk exclusion: this is explicit selection, and T7 governs capture
  walks [ruling Q2]. `FolderInfo{ID, ParentID, DisplayName (raw, untrusted), ChildFolderCount,
  TotalItemCount, WellKnown}`. `WellKnown` labels Deleted Items / Junk via
  `WellKnownFolderID` (`graph.go:250`). Refuses above `max` = 10,000 folders (R-s).
- `FolderCount(ctx, userID, folderID) (int, error)` — `GET
  /mailFolders/{id}?$select=totalItemCount`, used for the reconciliation in §4.7.
- `MessagePage(ctx, userID, folderID, next string, o PageOpts) ([]MessageRef, string,
  error)`. The first URL is
  `…/mailFolders/{id}/messages?$select=<messageSelectFields>,parentFolderId&$orderby=receivedDateTime%20desc&$top=<o.Top>`.
  **XC27:** `$select` is built from capture's `messageSelectFields` (importance, isRead,
  categories, to, cc, …) plus `parentFolderId`, so `ApplyGraphState` (§4.11) has its
  inputs and each message's folder comes from Graph. The size (PidTagMessageSize, 0x0E08)
  and sensitivity (PidTagSensitivity) extended properties are requested best-effort in one
  `$expand` with an id-aware `or` filter. They are decoded **by property id**, never
  positionally, because `sensitivityFromExtProps` assumes a single property. The MA-285
  first-page fallback applies if a tenant rejects the `$expand`. The first page's
  `Preference-Applied` "honored" detection (as in `Messages`) is returned and recorded
  per cursor, and it is the source of the manifest's `immutable_ids`. `MessageRef` gains
  `FromName`, `ParentFolderID` and `SizeHint int64` (−1 = unknown). The browse list uses
  `Top=100`; resolution uses `Top=1000`.
- `MIMETo(ctx, userID, messageID string, w io.Writer, maxBytes int64, idle
  time.Duration) (int64, error)` (SG's, with the overall cap as a further argument [ruling
  J4]) streams `/$value` into `w`. It aborts when no byte
  arrives for `idle` (default 120 s; this replaces the fixed 10-minute deadline for this
  path), above `maxBytes` (default 200 MiB, a hair over Exchange Online's 150 MB limit
  after MIME overhead), or on `ctx` cancel. **XC26:** the `$value` request gets its own
  response-header timeout of at least the idle window. The shared transport's 60 s
  `ResponseHeaderTimeout` (`graph.go:136-141`) would otherwise fail a large conversion
  before its first byte, on every retry. MA-443 records time-to-first-byte against size.
- **Typed errors (XC25) [ruling J4].** `StatusError` is SG's `{Code, GraphCode, Text}`
  with a bounded `Text`, and `Error()` keeps today's exact text. Callers use `errors.As` to
  tell 404/401/403/5xx apart. Classification maps SG's `graph.Classify` onto this design's
  own outcome table (§4.13).
- **`nextLink` pinning (XC25).** A followed link is compared after `url.Parse`: scheme
  equal; host equal case-insensitively; port equal after default-port normalization (443
  for https); path under the base path on a segment boundary. Otherwise it is a typed
  error naming the foreign host, and no request is made. The workspace paths
  (`FolderTree`, `MessagePage`) are pinned from day one. Capture's `Folders` and
  `Messages` get the pin in their own small capture-hardening step, now fault-isolation's
  **FI-SGc** [ruling J4] (formerly this design's E1c). It has capture rows (MA-505) and a
  short adversarial note, and is gated on an MA-443 sub-case that records real nextLink
  shapes, so a harmless Microsoft-side variation cannot fail every scheduled backup.
  Lifting that gate is the owner's decision (ESC-7).
- **Redirects and bodies (XC26).** `httpTransport`'s client sets `CheckRedirect` to refuse
  any redirect that leaves the base scheme, host and port. oauth2's Transport attaches
  `Authorization` on every hop, so a cross-host 302 would otherwise carry the bearer
  token. Graph JSON bodies are read through `io.LimitReader`. The client also sets
  `Timeout: reqTimeout`. That is the **F15 fix, required in E1**: `oauth2.NewClient` uses
  the context's client only for token acquisition, so the timeout bounds token fetches
  and the `deviceSource` mutex hold, and a stalled `/token` body can no longer block every
  workspace endpoint and the run.

### 4.6 Archive source adapter (slice E4)

The same adapter serves both archive sources. For another archive folder [ruling W1], "the
archive directory" below is the chosen folder, and every gate applies unchanged.

For each resolved record (key, `state.Record`):

1. **Original.** `emlRel = strings.TrimSuffix(rec.Path, ".html") + ".eml"`, then
   `validRelPath` (`internal/app/verify.go:509`), `extractInspect`
   (`internal/app/extract.go:532`: component-wise `Lstat`, no symlink at any level,
   regular file), and a read through an `*os.Root` opened on the archive directory,
   bounded to the recorded `Fixity.EML.Size+1` (`readBounded`, `extract.go:560`). With a
   recorded digest, a length or hash mismatch is a per-message failure. Without one, the
   file is copied "as stored, unattested" (a manifest note). **XC19:** a regular file
   with nlink > 1 is refused as a per-message failure, because a hard link defeats the
   Lstat walk and `os.Root` alike. `os.Root` closes *escape* from the archive. Within the
   root, the window between the check and the use remains, as in sibling designs: a
   co-writer can swap a file between Lstat and open, which the fixity check then catches
   when a digest is recorded.
2. **No original.** The record's `.html` (always) and `-attachments.zip` (if
   `Fixity.Zip`, or present) are copied byte-exact under the same gate and fixity check,
   with role `archived-page` / `archived-attachments-zip`, status
   `exported-without-original`, and the reason ("the archive kept no original: captured
   without 'Keep each message's original .eml'" for Graph and mbox stores; "Outlook .pst
   items never have one" for PST stores). These files are copied regardless of the
   Readable HTML toggle, because they are the only content.
3. **The archive's own gaps.** `rec.Missing`/`rec.Terminal`/`rec.Unresolved` are copied
   into the entry's `notes` ("the archive records attachment 'x.pdf' as never
   downloaded"). **XC19:** `state.UnknownSentinel`, which `state.Load` puts on every
   issue-free v1 record, is filtered out. It is replaced by "completeness was not tracked
   by the version that captured this message".
4. **Gone-from-mailbox records** (`Present == false`) are included by default and
   counted separately in the preview [ruling Q4].
5. **Redaction belt (XC19).** At resolution the selection is intersected with what is on
   disk: the record's page and `.eml` are Lstat-ed, as R21's reader does. A record whose
   files were deleted but not yet reindexed is left out of the plan. The preview reports
   only a count ("N records whose files were removed are skipped — run Rebuild search
   index"). Its subject, sender and time are **never** written into the export, `failures`
   included. A redacted record (files deleted, then `reindex`) is absent from the
   manifest and cannot be selected (R13, R21).
6. **Each entry records its store's mailbox** (XC21).
7. **Archive gone mid-run (XC10).** After a missing-file or read error, the archive root
   and manifest are probed. If either is gone, the run stops run-fatal with R-o2
   ("archive no longer available"), and "Export the remaining messages" is offered. It
   does not list thousands of false "damaged" messages.

### 4.7 Preview (the dry run) and the plan

`/api/workspace/preview` starts a recovered goroutine (one at a time; a new preview
cancels the old one). It:

1. Re-validates the source binding (signer UPN or archive path) and the selection.
2. Resolves the message set (§4.4). Microsoft 365 folders are listed with
   `MessagePage(Top=1000)`. **Reconciliation (F11 mitigation; revised by XC16):** read
   `FolderCount` before and after each folder's listing. **before == after is the only
   stability test.** If before ≠ after, re-list once and take the **union** of the two
   listings. With newest-first `$skip` paging an arrival causes a duplicate, which is
   de-duplicated, and only a removal ahead of the cursor causes a skip. An id that is gone
   by run time is the existing 404 class. The WARN is raised only if the second listing's
   own before ≠ after: "Inbox changed while it was being read twice in a row … Preview
   again for an exact copy." **Listed ≠ reported** is a separate, stable fact, not a
   change. It gets an info line with true wording: "Microsoft 365 reports 1,350 items in
   Inbox; 1,348 are messages and will be copied." That line never re-lists and never
   WARNs. Residual: a balanced add-and-remove during the listing is invisible (F11's
   delta-query fix would close it).
3. Validates the destination: absolute and clean, no NUL; exists and is a directory
   (else R-e); not equal to or inside the archive location after symlink resolution
   (`destOverlapsOut`, `extract.go:621`, using only its "same"/"inside" cases; else R-f),
   nor equal to or inside the other archive folder when that is the source [ruling W1].
   It also reports:
   - **Free bytes** via `util.FreeBytes` (statfs / `GetDiskFreeSpaceEx`, read-only), with
     the **free-space policy (XC11):** the estimate plus a finalization reserve of about
     1 KB × messages + 1 MiB must fit. If it would not, Start refuses with R-g2. The
     preview WARNs when unknown-size messages could exceed it, and the same floor is
     checked on the volume that holds the state directory.
   - **Filesystem type (XC12)** via statfs `f_type` / `GetVolumeInformationW`. FAT32 gets
     a WARN for its 4 GiB per-file limit (a larger message is a listed failure).
     FAT/exFAT/SMB get "access can't be restricted on this drive".
   - **Posture line (XC32):** "only you can read this folder", "other accounts on this
     computer can read this folder" (C:\Users\Public, shared folders, inherited ACLs), or
     "permissions can't be restricted on this drive".
   - **Cloud sync** via `util.UnderCloudSync`, extended to iCloud Desktop & Documents,
     `iCloudDrive` and "Dropbox (<team>)" roots, and labelled best-effort (XC32). It is a
     WARN naming the service: "files you export here will be uploaded to OneDrive".
4. Computes: exact count; per-folder counts (reported vs found); a size estimate
   (Σ known `SizeHint` × (1 + 1.0 if Readable HTML + 0.8 if attachments), with "N
   messages of unknown size" alongside; archive source: exact sizes from `Fixity`, plus
   the same factors for derived files); the export directory name it would use if
   started now (the final name is fixed at start, §4.9); the first 10 target paths and
   the longest one (UTF-16 units) with the count over 259; with-original / without-
   original counts (archive source); the gone count (archive source); an ETA band from
   the message count (§4.15).
5. Stores the **plan** under a random `previewId`, bound to (source identity, a
   selection hash, the destination and its directory identity, the options), and keeps
   only the latest. It expires after 30 minutes. At start the plan is written into the
   run record (§4.19) before the marker (XC1).

While an export runs, previews and listings are serialized behind the run (XC30), so
the workspace stays within the 4-concurrent-per-mailbox budget (§4.14).

The preview writes nothing to the destination or the archive. Apart from the sign-in
token's normal refresh, it writes nothing to the config directory either (XC30). MA-412
hashes all three trees before and after, with the token entry excluded and asserted
separately. Start (`api/exports/start`) re-checks only what can change cheaply: free
space and the reserve (XC11), and the signer unchanged. It also **opens the destination
once as a directory handle**, checks that its identity equals the previewed one and that
it is still outside the archive, and then creates the export root **through that handle**
(XC32). There is no path re-walk between the check and the mkdir. Start refuses on a
stale or superseded id (R-k).

### 4.8 Destination chooser: the native OS picker, not an in-page directory tree

**Decision:** "Browse" opens the OS folder picker in the dashboard process. The path
box always accepts a typed path. No endpoint lists directories.

**The argument.** The two options differ in what new HTTP surface they add.

- *In-page tree* (the mockup). It needs `listDir(path) → child names`, callable over
  loopback HTTP with any path. Even directories-only, that is filesystem reconnaissance
  for anything that can call the dashboard: any local account while F3 is open (scrape
  the CSRF token, then POST), and a DNS-rebinding page if a GET is ever added (F2). It
  is a new code path with its own traversal, symlink, special-file and hang hazards: an
  offline mapped drive or UNC path can stall a listing for minutes. And it duplicates a
  chooser the OS already provides, with favourites, network locations and "New folder".
  With E0 in place, the remaining exposure is only to the same user (DC4, out of scope),
  so the security cost after E0 is mostly the bug surface and the hang hazard, not a
  confidentiality hole. That is why this was a recommendation and an owner question (Q1),
  not a security necessity. **[ruling Q1]** The picker plus the typed path is accepted.
- *Native picker.* No new read surface: the path comes from an OS dialog the user
  drives, on the machine that owns the loopback (always the user's own, given
  loopback-only plus E0's per-user instance). Costs, which are friction rather than
  security:
  - The dialog may open behind the browser window. The page says "A folder picker has
    opened — it may be behind this window".
  - On Linux it needs a `qarma`, `zenity` or `matedialog` binary: zenity v0.10.12 looks
    for those, not `kdialog` (XC31). Otherwise the reply is `{unavailable}` and the page
    asks for a typed path (R-n).
  - One dialog at a time. A second request is refused: "a folder picker is already
    open".
  - The request waits up to 10 minutes, with `zenity.Context(ctx)` dismissing it.

  **Footprint (XC31).** zenity is new to the `mailarchive-desktop` binary; today only
  `mailarchive-gui` links it. On Windows, `IFileOpenDialog` is created in-process, which
  loads shell namespace, overlay and preview extensions (OneDrive, Dropbox, AV, NAS
  clients) into the process that holds the token and runs the export. So **Browse is
  disabled while an export runs**; a typed path still works. A crash in an extension
  kills the process, and the running export is then recovered by "Finish as partial"
  (XC2). A hang inside an extension cannot be dismissed by `zenity.Context`. Browse then
  stays refused ("a folder picker is already open") until restart. Both rows are in
  §5.2. Running the dialog in a child process would contain both, but it would reopen the
  desktop owner ruling P1 (in-process, not shell-out). **[ruling Q14]** It is not done: P1
  stands, and this minimum (Browse disabled during a run, the §5.2 rows) is the design.

The picker is not a security boundary. The page can submit any typed path anyway, and
that path is validated the same way (§4.7 step 3). The boundary is "only the signed-in
OS user can drive the dashboard" (E0).

**Create folder.** `{parent, name}`: `parent` must be an existing directory outside the
archive. `name` must equal `util.SanitizeSegment(name)` (no separator, `..`, reserved
name or empty; otherwise R-q with the sanitized suggestion). Then one `os.Mkdir(parent/name,
0o700)`; an existing name is R-q ("already exists — pick it with Browse, or use another
name"). It returns the new path for the box.

### 4.9 Output layout, naming and sanitization

```
<destination>/MailExport-2026-10-09_142233-7f3a/     export root (exclusive mkdir via the destination handle, 0700)
  _EXPORT-NOT-FINISHED.txt                           written first (carries the run id); removed last (kept if cancelled/killed)
  README.txt                                         plain text: what, conditional status, layout, how to verify, caveats
  export-manifest.json                               run metadata + one entry per selected message (built from the run record)
  SHA256SUMS.txt                                     coreutils format; every file except itself and the marker
  messages/
    Inbox/Clients/Acme/                              Preserve folder structure ON (archive source: messages/<store>/…)
      2026-03/                                       month subdirectory; ≤1,000 files each, then 2026-03~2 (XC12)
        2026-03-03_0900_quarterly-review_8e54306f.eml
        2026-03-03_0900_quarterly-review_8e54306f.html            Readable HTML
        2026-03-03_0900_quarterly-review_8e54306f.attachments/    Extract attachments
          report.pdf
          report (2).pdf
```

- **Export root name:** `MailExport-<local YYYY-MM-DD_HHMMSS>-<4 random hex>` (XC32: a
  co-writer cannot pre-create the next names). On `EEXIST`, try `-2` … `-99`, then fail
  with R-t. It is created exclusively (never `MkdirAll`) **through the destination
  directory handle** opened at start (XC32), then opened as an `*os.Root`, and its
  identity (dev+inode / volume serial + file ID) is recorded. **Only then** is the journal
  `start` record written, with the real name and that identity (XC3). Every later create,
  mkdir, rename and remove goes through that root. Nothing else is written outside it,
  except the local run record and journal (XC1) and Create folder's single mkdir (§4.8).
  EACCES/EPERM at creation (Controlled Folder Access, macOS privacy settings, ACLs) is
  refused with R-w (XC12).
- **Directory size (XC12):** no export directory holds more than 1,000 files. Messages go
  under a month subdirectory (`messages/<folder>/2026-03/…`, or `messages/2026-03/…` with
  Preserve OFF), split as `2026-03~2`, `~3` … past 1,000. That is about 8,000 FAT
  directory entries with long names, far below FAT32's 65,536. The stem starts with the
  date, so order within a month is kept.
- **Folder segments:** `util.SanitizeSegment(displayName)` per level. Among siblings
  already used in this export, an `util.FoldKey` collision appends `~` +
  `util.HashHex(folderID, 8)` (F55's lesson, applied at write time). FoldKey is NFC +
  `cases.Fold`, widened with the NTFS/exFAT upcase-table equivalents (ı/I, XC13). A
  segment that matches the 8.3 short-name alias pattern (`~` + digits before an optional
  extension, e.g. `PROJEC~1`) is disambiguated the same way (XC13). Each folder mkdir is
  exclusive, and its `EEXIST` is never swallowed (XC13).
- **Message stem:** `<received UTC 2006-01-02_1504>_<util.Slug(subject, 60)>_<HashHex(sourceID,
  n)>`, the archive's own scheme (`baseNameN`, `internal/export/exporter.go:760`) so an
  inheritor sees one convention. The naming helpers are shared with the `Exporter` once
  exported (XC36). `sourceID` is the Graph id (immutable when honoured) or the archive
  key. `n` lengthens 8→12→16→24→40 on a FoldKey collision within the directory.
- **Path budget:** if the absolute path passes 250 UTF-16 units, the slug shrinks
  60→24→0 (the `stemFor` rule, `exporter.go:515`). Paths still over 259 are written
  (Go's `os` handles `\\?\` long paths on Windows) and **reported** in the preview and
  README ("Windows Explorer and some zip tools cannot open paths over 260 characters;
  choose a shorter destination or turn off Preserve folder structure"). Folder names are
  never truncated beyond `SanitizeSegment`'s own rule.
- **Attachment names:** `util.SanitizeFilename(name, i)`; a nameless part gets
  `attachment-<i><ext from MIME type>`. FoldKey-unique within the message's directory
  via a casefold variant of `uniqueName` (`internal/export/attach.go:133` is
  case-sensitive, which is fine for zip entries and wrong on disk). Names matching the
  8.3 alias pattern (e.g. `REPORT~1.PDF`) are disambiguated (XC13).
- **Modes (XC12):** directories 0700, files 0600, set **only at creation**
  (`O_CREATE` 0600 / `Mkdir` 0700, whatever the umask; MA-439; the archive's C2-1
  lesson), never by a later chmod, so vfat and exFAT mounts cannot fail the write.
  **Conditional:** the modes hold on filesystems with POSIX modes. On FAT, exFAT,
  NTFS-3g and SMB/NFS mounts, access follows the mount or share options, and README
  says so. Windows: the export inherits the destination's ACL (conditional, stated in
  README; lab row MA-442). **[ruling Q10 = J3]** It does not get a protected owner-only
  DACL: a hand-over copy must open for its recipient. `docs/design-local-confidentiality.md`
  scopes its protected DACL to archive roots and the tool's own state directories. The
  preview's posture line (§4.7), built on that design's `Posture(dir)`, names before the
  first write who can read the destination.
- **Temp files and final placement:** `.mailexport-<16 hex>.tmp` (`O_CREATE|O_EXCL`,
  0600) in the final directory: write, `Sync`, then a **no-replace rename** to the final
  name (XC13). That is `renameat2(RENAME_NOREPLACE)` on Linux, `renamex_np(RENAME_EXCL)`
  on macOS and `MoveFileEx` without `MOVEFILE_REPLACE_EXISTING` on Windows. Where none of
  those is available, it is an Lstat in the root followed by link + unlink. Go's
  `os.Root.Rename` replaces (Windows passes `FILE_RENAME_REPLACE_IF_EXISTS`), so it is not
  used for final placement. On `EEXIST` the name moves to the next unique variant, or the
  message fails, and the failure is listed. A rename that fails with a sharing violation
  or access-denied (OneDrive, AV scanning a new file) is retried 5 times with 100 ms → 2 s
  backoff before the message fails (XC10). **Directory syncs are batched (XC5):** each
  file is fsynced as it is written, and directories are synced once before
  `SHA256SUMS.txt` (W9) rather than per file. Tests use a sync seam, so MA-432 does not
  pay 120,000 `F_FULLFSYNC`s per macOS run.

### 4.10 The manifest, SHA256SUMS, README and fingerprint; how a recipient verifies

`export-manifest.json` (format version 1; UTF-8). It is streamed at finalization from
the local **run record** (§4.19, XC1), one entry per message, so memory stays bounded.
There is no destination spool: revision 1's `.mailexport-entries.tmp` inside the export
root is dropped, because a co-writer on the destination could edit it, and the sums and
fingerprint would then vouch for forged bytes (XC1).

```json
{"format":"mailarchive-copy-export","format_version":1,
 "tool":{"name":"MailArchive Desktop","version":"v0.x.y"},
 "run":{"id":"9c1f…","started_utc":"2026-10-09T18:22:33Z","finished_utc":"…","status":"complete|partial|cancelled|failed",
        "valid_if_absent":"_EXPORT-NOT-FINISHED.txt","retry_of":"","chain":[{"part":1,"run_id":"…","root":"MailExport-…","fingerprint":"…"}]},
 "source":{"kind":"m365","mailbox":"alex@contoso.com","tenant":"<tenant id>","immutable_ids":true,
           "original_means":"MIME bytes returned by Microsoft Graph /$value at export time"},
 "selection":[{"folder":"Inbox","subfolders":3,"excluded":0,"messages":1766},
              {"folder":"Sent Items","subfolders":0,"excluded":76,"messages":322}],
 "options":{"preserve_folder_structure":true,"readable_html":true,"extract_attachments":false},
 "counts":{"selected":2088,"exported":2086,"failed":2,"not_attempted":0,"without_original":0},
 "hash":"SHA-256",
 "messages":[{"n":1,"source_id":"AAMk…","internet_message_id":"<…>","subject":"…","from":"…",
   "received_utc":"…","folder":"Inbox/Clients/Acme","status":"exported",
   "files":[{"path":"messages/Inbox/Clients/Acme/…8e54306f.eml","role":"original","sha256":"…","size":48211},
            {"path":"…8e54306f.html","role":"derived-html","sha256":"…","size":51002}],
   "notes":[]}],
 "failures":[{"n":17,"source_id":"…","subject":"…","folder":"Inbox","received_utc":"…","class":"retryable","reason":"Microsoft 365 could not convert this message to MIME (ErrorMimeContentConversionFailed)"}],
 "not_attempted":[]}
```

Notes on the fields:
- **`status`** is valid only while `_EXPORT-NOT-FINISHED.txt` is absent, as
  `valid_if_absent` says in the manifest's own text (XC5). The marker carries the same
  `run.id`, and reconciliation acts on a directory only when the ids match (XC3).
- **`immutable_ids`** comes from the first page's "honored" flag recorded per cursor
  (XC27).
- **`chain`** lists every part of a retry set (XC15).
- **Archive source (XC21):** `source` is
  `{"kind":"archive","archive":"C:\\MailArchive","stores":[{"store":"…","mailbox":"alex@contoso.com"},…]}`,
  and each message entry carries `"mailbox"`.

Folder names, subjects and senders are stored as Graph reports them (JSON-escaped,
control characters stripped), not sanitized, so the recipient sees the real names.
Roles: `original`, `archived-page`, `archived-attachments-zip`, `derived-html`,
`derived-attachment`, `quarantined-archived-page`.

`SHA256SUMS.txt` holds lines `<64 lowercase hex>␠␠<path relative to the export root,
forward slashes>`, sorted by path. It covers every file in the export except itself
and the marker, including `README.txt` and `export-manifest.json`. Names cannot contain
newlines or backslashes, because the sanitizers replace control and illegal characters,
so the plain coreutils form is unambiguous. UTF-8, no BOM.

`README.txt` states:
- **Status, conditional in its own text (XC5):** "Status: complete — valid only while no
  _EXPORT-NOT-FINISHED.txt is in this folder and the check below passes." If not
  complete, it says why and how many.
- **Source and run:** mailbox or archive (and, for an archive, every mailbox it holds,
  XC21); run times (UTC); counts; layout.
- **What the files are:** what "original" means for this source (the P8 caveat); that
  derived files are conveniences and the `.eml` is authoritative (P10/P11); the
  parser-limit caveat when derived files exist.
- **Caveats:** long-path warnings. On FAT/exFAT/SMB, file permissions are not kept
  (XC12). On removable media: "Eject before unplugging" (XC5). Originals are real mail
  and should be opened with the same caution (links, attachments). For a retry set, the
  sibling parts (XC15).
- **Verification**, which README says to run **"in a terminal opened in this folder"**,
  with the per-OS way to open one (Explorer address bar → `powershell`; Finder → Services
  → New Terminal at Folder; a file manager's "Open in Terminal") (XC14):
  - Linux: `sha256sum -c SHA256SUMS.txt` · macOS: `shasum -a 256 -c SHA256SUMS.txt`
  - Windows PowerShell 5.1+ (**lab-pending, MA-442**; XC14):
    `$n=0;$bad=0;$gone=0; Get-Content -Encoding UTF8 SHA256SUMS.txt | ForEach-Object { $h,$p = $_ -split '  ',2; $n++; if (-not (Test-Path -LiteralPath $p)) { $gone++; "MISSING $p" } elseif ((Get-FileHash -Algorithm SHA256 -LiteralPath $p).Hash -ne $h.ToUpper()) { $bad++; "MISMATCH $p" } }; if ($bad+$gone -eq 0) { "OK: $n files checked" } else { "$bad changed, $gone missing of $n" }`
    The line ends with an explicit "OK: N files checked" or the counts, never silence.
  - **What the check does not cover (XC14):** "The check proves every listed file is
    unchanged, or names the one that changed or is missing. It does not detect files
    added to this folder. Files not listed in SHA256SUMS.txt are not part of the export."
  - **Extra-files check (XC14).** POSIX:
    `find . -type f ! -name SHA256SUMS.txt ! -name _EXPORT-NOT-FINISHED.txt | sed 's|^\./||' | sort | comm -23 - <(cut -c67- SHA256SUMS.txt | sort)`
    prints any unlisted file. PowerShell: `Compare-Object` of the relative names from
    `Get-ChildItem -Recurse -File` against the SUMS paths.
  - **Fingerprint:** `sha256sum SHA256SUMS.txt` / `shasum -a 256 SHA256SUMS.txt` /
    `Get-FileHash SHA256SUMS.txt` must equal the fingerprint the sender gave you
    separately (email, phone). Matching hashes prove the listed files are unchanged since
    export only if that fingerprint also matches.

**The fingerprint (XC1, XC14).** It is computed from the **in-memory** `SHA256SUMS.txt`
bytes at seal time, never by re-reading the file. It is recorded in the run record's
`sealed` line and in a small **fingerprint ledger** (`<state dir>/export-fingerprints.jsonl`,
0600, never compacted). It is shown on the Export activity page in groups of 8 hex digits
for reading aloud, with a **copy** button and **"Save fingerprint receipt"** (a small text
file the page offers as a download, for the user to keep outside the export). It is
deliberately not written into the export, where it would prove nothing. **Losing the
ledger loses the sender's tamper anchor** for past exports, and P9 says so. A
fingerprint is never recomputed from disk for a run that was not sealed. Such a run shows
"fingerprint not recorded at export time" (XC4).

### 4.11 Readable HTML

Slice E5; hidden, with `exportHTML` OFF, until E5 lands (XC28). Microsoft 365 source,
and archive source with an original:
1. The `.eml` bytes are read back bounded by the MIME cap into the parse buffer.
   **Their SHA-256 must equal the hash recorded in the run record when the original was
   written (XC1).** On a mismatch no derived file is made, and the message is listed as
   "the original changed on the destination after it was written".
2. `source.ParseRFC822`.
3. Graph listing state is overlaid with `applyGraphState` (`internal/app/graph.go:398`;
   exported as `app.ApplyGraphState`) so the Status and Categories rows match the
   archive's. Its inputs (importance, isRead, categories, sensitivity) come from
   MessagePage's widened `$select` (XC27).
4. `export.RenderWith(m, RenderContext{RawName: "<stem>.eml", AttachDirName:
   "<stem>.attachments" or ""})`.

`RenderContext` gains one field, `AttachDirName`, so `renderHeader` links extracted
files by relative name. **Guarded (XC30):** the sentence "attachments are inside the .eml
file" appears only when `RawName != ""`, `ZipName == ""` and `AttachDirName == ""`, that
is, only in the export's own renders. Archive renders stay byte-identical before and after
the change, which MA-525 asserts. With no `RootRel`/`FolderIndexRel`, the header renders
no navigation (`html.go:68-78`). The
CSP and no-referrer metas come first (R19). The parse → serialize → re-parse fail-closed
check (`sanitizeHTML`, MA-301) applies unchanged.

Archive source without an original: the archived page is copied byte-exact. Its
back-links to the archive's folder and root pages do not resolve in the export (README
says so). Before copying, the bytes go through a live-meta detector built on the
renderer's own (`hostileSurvives(head, body string)`, `html.go:250`), exported as
`export.LiveRefreshIn(page []byte) bool`. **Defined (XC30):** it reports any hostile meta
**other than the tool's own leading metas**, that is, an exact match of the charset
meta and `inertMetas` (`html.go:34-43`) at the start of `<head>`. Every page the
renderer writes starts with those, and `hostileSurvives` alone would flag every page.
If a live refresh or a foreign CSP meta is found (a page rendered before the F1 fix), it
is copied as `<stem>.archived-page.txt` with a note instead (P10). Its hash still equals
the archive's recorded fixity. MA-423's positive twin: a page rendered by the current
renderer is copied byte-exact as `.html`.

A panic in parse or render for one message is recovered: the original stays, the
`.html` is absent, and the entry gets `notes:["readable copy could not be made: …"]`
with status `exported-original-only`. It counts as partial (§4.13), but it is labelled
"exported without a readable copy", never "not exported", and is not offered for retry
(XC35).

### 4.12 Extract attachments

From the parsed message: every `model.Attachment`, inline images included (an eDiscovery
recipient expects all parts), each written via `WriteTo` into a root-contained temp file
and renamed, hashed as written, role `derived-attachment`. A zero-byte or failed stream
is an entry note, never silent (R1). Archive source without an original: the
`-attachments.zip` is copied byte-exact (role `archived-attachments-zip`), and its
entries are also extracted, under bounds: at most 1,000 entries; per-entry uncompressed
≤ 1 GiB; per-message total ≤ 4 GiB; compression ratio ≤ 200:1. Entry names are
re-sanitized and flattened (zip-slip `../` is impossible because only the base name is
used, then `SanitizeFilename`). A bound breach skips that entry with a note. Caveats in
README: C1-1 (non-UTF-8 text attachments re-encoded; zips captured before the fix
already hold re-encoded bytes), F24/F25 (parts dropped or unnamed), F26 (PST
forwarded-as-attachment items are a few-byte blob in the archive zip, with no gap
recorded, so the export cannot detect them).

### 4.13 The run: states, progress, cancel, failure classes, outcomes

**One export at a time** in its own slot (`exportState`), independent of the capture
slot (`captureState`, `internal/desktop/capture.go:56`; the shared job runner is `runJob`, `maintenance.go:19`; citations refreshed, XC30). A second start is R-m.
States: `preparing` → `running` → `finalizing` → `complete` | `partial` | `cancelled` |
`failed`, plus `interrupted`, which is derived on reload (§4.16). The goroutine has the
DC3 `recover`: a panic becomes `failed`, and the marker stays.

**Per message (Microsoft 365):** `MIMETo` → temp → no-replace rename `<stem>.eml`
(XC13); then derived files; then one fsynced line in the **run record** (status, class,
files with role, SHA-256 and size; XC1). The archive adapter is §4.6.

**Classification (XC9, XC10).** One classifier, SG's `graph.Classify` (XC25, [ruling J4]),
sorts every Graph error, and this design maps its classes onto the table below. Capture's
wrapper, `app.classifyFetch`, is fault-isolation's and is not used here. The 404 class, the
403 handling and the breaker thresholds are this design's own, with this justification: a
copy export is a one-off for one selection, so a message that is gone is terminal for that
selection, whereas capture retries through its ledger on every later run. Destination write errors are matched with `errors.As`
on `syscall.Errno` against a per-OS table, never with `errors.Is(err, syscall.ENOSPC)`.
On Windows, `syscall.ENOSPC` is an invented constant that no disk-full code maps to.

| Condition | Class | Effect |
|---|---|---|
| `/$value` 404 | terminal (for this selection) | listed: "no longer in the mailbox (moved or deleted after you selected it)"; never retried in-run (F9 hazard 4) |
| 400 / `ErrorMimeContentConversionFailed` / above the 200 MiB cap | retryable (a later export may succeed) | listed with the reason; offered by "Export failed messages again" |
| **Transport-class**: no HTTP status (dial, DNS, reset, token-endpoint network failure); 502/503/504 after the client's retries; idle timeout (XC9) | **back off on the same message** (1 s → 60 s, exponential) | never recorded as a failure; counts only toward the no-progress clock |
| No successful request for **5 minutes** (wall clock, XC9) | **run-fatal: connection lost** | stop → R-u (names the 5 minutes) |
| 25 consecutive per-message server-class failures (5xx with a body) (XC9) | **run-fatal: Microsoft 365 having problems** | stop → R-u2 |
| 429 | handled in `get` with Retry-After (`graph.go:547`) | none unless retries run out → transport-class |
| Derived-file failure (parse or render panic, attachment stream error, read-back hash ≠ recorded, XC1) | partial content | original kept; entry `exported-original-only` ("exported without a readable copy", XC35) or a note |
| Write error on one file, not in the destination-fatal table | retryable | temp removed; message listed; then the root is probed through the `os.Root` (`Stat(".")`), and a failed probe is run-fatal (XC10) |
| Rename fails with `ERROR_SHARING_VIOLATION` / `ERROR_ACCESS_DENIED` (OneDrive, AV) (XC10) | retried in place | 5 tries, 100 ms → 2 s, before the message fails |
| **5 consecutive write failures** of any class (XC10) | **run-fatal: destination** | stop → R-g naming the last error |
| Archive: missing file / fixity mismatch / gate failure | terminal until the archive is repaired | "changed or damaged in the archive — run Check integrity" |
| Archive: root or manifest gone on the probe after a read error (XC10) | **run-fatal: archive no longer available** | stop → R-o2 |
| Archive: no `.eml` kept | not a failure | `exported-without-original` (disclosed in the preview) |
| 401 (XC9) | one forced token refresh and one retry | the run becomes fatal only if the refresh fails → R-c |
| 403 (XC9) | probe `GET /me` | probe fails → **run-fatal: sign-in** (R-c); probe succeeds → per-message terminal for that item |
| `*oauth2.RetrieveError` with `invalid_grant` / `interaction_required` / `invalid_client` / `unauthorized_client` / `consent_required` (XC9) | **run-fatal: sign-in** | stop → R-c (also in preview and list endpoints, as R-b/R-c) |
| Clear sign-in, any sign-in, or `graph.SignedIn` UPN changed or vanished (checked every 100 messages) (XC7) | **run-fatal: sign-in cleared or changed** | stop → R-z; finalized; retryable from the run record |
| Destination-fatal errno: Unix ENOSPC, EDQUOT, EROFS, EIO, ENXIO, ENODEV, ESTALE, ETIMEDOUT; Windows ERROR_DISK_FULL 112, ERROR_HANDLE_DISK_FULL 39, ERROR_WRITE_PROTECT 19, ERROR_NOT_READY 21, ERROR_DEVICE_NOT_CONNECTED 1167, ERROR_DEV_NOT_EXIST 55, ERROR_NETNAME_DELETED 64, ERROR_UNEXP_NET_ERR 59, ERROR_CANNOT_MAKE 82; export root gone (XC10) | **run-fatal: destination** | stop → R-g |
| Free space below the finalization reserve (checked every 50 messages) (XC11) | **run-fatal: destination** | stop → R-g, so finalization always fits |
| Cancel; app stop via `Drain` (XC6) | **cancel** | stop after the in-flight message; its temp removed |
| Windows logoff / shutdown / MSI upgrade **without** the end-session window; any kill (XC6) | **kill** | no finalization; recovered by "Finish as partial" from the run record (XC2) |

The message in flight at any run-fatal stop is listed in `failures`, never at the head of
`not_attempted`. So "Export the remaining messages" never starts on the message that
stopped the run (XC9). The cancel and the breakers test the walk context, not the
per-download deadline (F9 hazard 3). The export never writes the archive manifest, so
F9's hazards 1 and 2 (recording a failure under the message's identity; false "gone"
sweeps) cannot arise.

**A wedged run (XC10).** If the run goroutine is still blocked 30 s after Cancel (a write
stuck in the kernel on a hard NFS mount or a hung SMB session), the export slot is
released. The run is then marked "Stopped: the destination is not responding" in the
journal and its run record. The wedged goroutine's context is cancelled, so it makes no
further writes when its syscall returns. It holds one OS thread until then or until the
app restarts (§5.2).

**Finalization** (every stop except a kill):
1. **Presence check (XC14).** Every recorded file is Lstat-ed through the root. A file
   gone since its write (AV quarantine, for example) makes the run partial and is named
   in `failures`.
2. Write `README.txt`, whose status is conditional in its own text (XC5).
3. Write `export-manifest.json` (streamed from the run record, XC1).
4. Sync the directories (batched, XC5).
5. Write `SHA256SUMS.txt`, built from the hashes in the run record.
6. Append the run record's `sealed` line, with the fingerprint computed from the
   in-memory SUMS bytes, and fsync it (XC1, XC5).
7. **Complete/partial:** remove `_EXPORT-NOT-FINISHED.txt`. **Cancelled/failed:** rewrite
   it as "CANCELLED / STOPPED at <time>: <exported> of <selected> exported, reason …; do
   not hand this folder over as complete".
8. Sync the directory.
9. Write the journal `end` record and the fingerprint ledger line.

If finalization itself fails, the marker stays, and the activity page says so. With the
free-space reserve (XC11), a full disk can no longer cause this.

**Outcome** (engine `CopyOutcome`, reserved for a future CLI verb, consistent with X1's
`extract` partition): `0` complete (every selected message exported with every
requested file); `3` partial (any failure, any `exported-original-only`, any not
attempted: so a cancel is 3 too); `1` failed or refused (a run-fatal stop, or a
refusal before any write). The page labels them "Complete", "Finished with problems —
N messages not exported", "Cancelled", "Stopped: <reason>". **XC35:** render-only
problems are reported as a separate count, "N exported without a readable copy". When
they are the only problems, the label is "Finished with problems — N exported without a
readable copy", and retry is not offered for them.

**Progress** (`api/exports/progress`; XC4): selected / done / failed counts, bytes
written, current folder, rate (moving average) and ETA, and the last 400 log lines (the
`logRingMax` bound, `capture.go:50`). It is served from the in-memory run slot only and
never waits on history reconciliation. Subjects appear only in the failure list.

**Retry (XC2; in slice E3 per XC28)** (`api/exports/retry`). "Export failed messages
again" (partial) and "Export the remaining messages" (cancelled, stopped or finished as
partial) build a new **preview** from that run's **run record** (§4.19), never from the
destination's `export-manifest.json`:
- `failed` takes the retryable failures. `rest` takes the plan minus the done messages.
- Signer must match (R-l). Admission is P4's second path. There is no seen-set and no
  per-id GET: names and metadata come from the record. Retries sit outside the 10,000
  pick cap and inside the 200,000 run cap.
- A retry goes through the normal preview → start flow into a **new** export directory,
  named as the next **part** of the set: `<first root>-part2`, `-part3` … (XC15). Its
  manifest names `retry_of` and carries the whole `chain`.
- The earlier export is never modified.
- The destination's manifest being missing (the stick was handed over), edited or
  planted changes nothing.

**Finish as partial (XC2).** An interrupted (killed) run is finished by the app from its
run record alone, after the root's identity matches the recorded one (XC3). README,
manifest and sums list exactly the recorded files that are present under the root
(Lstat), the remaining ids are `not_attempted`, and the marker is rewritten STOPPED.
"Export the remaining messages" is then offered. This retires revision 1's Type III
"resume from the spool" item.

### 4.14 Concurrency and the archive lock

- **Microsoft 365 source:** no archive involvement at all, and no archive location is
  needed. It runs alongside a capture, **conditional on F17 (XC30, XC38)**.
  - **Why the condition.** Until F17 is fixed (fault-isolation's slice FI-GF17, after its
    S3 [ruling Q15]), a Retry-After of about 60 s
    or more counts against the capture's 60-second request deadline. A long export beside
    the scheduled backup can then make that backup fail ("context deadline exceeded"),
    not merely slow it down.
  - **Until then,** the export backs off while a capture runs (in-process, or signalled by
    the archive lock being held) and after any 429.
  - **Concurrency budget (XC30).** The run, a capture, a preview and browsing would be up
    to five request streams against Exchange's 4-concurrent-per-app-per-mailbox limit,
    beside 10,000 requests per 10 minutes. So previews and listings are serialized while
    an export runs (§4.7), and 429s are expected and handled, not exceptional. At about 3
    messages/s an export uses about 1,800 requests per 10 minutes.
- **Archive source:** no lock. Manifest snapshot plus per-file fixity verification
  (§4.3, §4.6). What a concurrent writer can do, and what the export sees:
  - An incremental Graph capture adds new records and does not rewrite existing files
    (R13). The snapshot does not include the new records; nothing else is affected.
  - A full-mode capture or gap fill re-writes a record (temp + rename, R5), and may
    delete the old stem (N2). The export sees a fixity mismatch or a missing file, and
    reports "changed in the archive during export". Nothing half-old/half-new is ever
    emitted.
  - `reindex` redaction deletes files → "removed from the archive".
  - `Rebuild` renames `search.db`. The workspace never holds it across requests, and a
    list request's shared lock keeps it out of Rebuild's rename window (§4.3, XC20). The
    run does not read it.
  - A concurrent full capture can fail individual messages (listed, retryable). P15 says
    so inline (XC30).

  The alternative, holding the exclusive lock as `extract` does (R20/PC14), was
  rejected: it would make a scheduled backup that fires during a multi-hour export
  refuse. That refusal is quiet: lock refusals write no last-run record, so it shows up
  only as `stale` two periods later (R18). A per-message, listed "changed during export"
  fails louder and is retryable.
- **In-process:** the export slot and the capture/verify/rebuild slot are independent.
  `Verify` hashing the archive while an archive-source export reads it only costs I/O.

### 4.15 Large selections (tens of thousands of messages)

- The page never holds more than the loaded list pages (100 rows each) plus the
  selection description. Whole folders are never enumerated client-side.
- Resolution is streamed at 1,000 per page. Resolved items are about 300 bytes each in
  memory (50,000 ≈ 15 MB). Cap: **200,000 per run** (R-i, "split into several exports,
  for example by folder").
- Run memory: one message at a time. Originals stream to disk. Parsing for derived files
  reads one `.eml` back (bounded by the 200 MiB cap; large attachments are buffered
  whole in memory, the catalog's existing acknowledged limit). So with Readable HTML on,
  one message's MIME plus decoded attachments is held (XC30). Per-message state is
  written to the local run record, not held (XC1).
- Throughput: sequential. An estimated ~2–5 messages/s means 50,000 messages take about
  3–7 hours. This is **aspirational** and unsourced until measured on a real tenant and on
  a USB-stick destination, sync cost included (MA-443; XC30, XC5). The preview gives this
  band, and the activity page computes a live ETA. Parallelism is Q7.
- Long runs and laptop sleep: a short drop (Wi-Fi roam, wake, VPN reconnect) costs only
  time. The run backs off on the same message and continues (XC9). Only 5 minutes with
  no successful request stops it (R-u), and then "Export the remaining messages"
  continues into the next part of the set (XC15).

### 4.16 Export activity, the journal, and the Microsoft 365 entry

- **Journal** `desktop-exports.jsonl`. **Location (XC3):** the per-user, **local,
  non-roaming** state directory, next to the run records (§4.19): Windows
  `%LOCALAPPDATA%\MailArchive`, macOS `~/Library/Application Support/MailArchive`, Linux
  `$XDG_STATE_HOME/mailarchive`. It is not the roaming `%AppData%` config directory,
  which folder-redirected RDS/VDI profiles share across hosts.
  - **Lines.** 0600, append-only lines of at most 4 KB: `{v:1, host, run,
    event:"start"|"sealed"|"end"|"reconciled", at, source_kind, mailbox|archive, dest_dir,
    root_id, planned, exported, failed, status, fingerprint}`. No subjects (privacy).
  - **When `start` is written (XC3):** **after** the exclusive mkdir of the export root,
    with the real directory name and the root's identity. A failed journal or run-record
    write refuses the start with R-d, naming the state directory. The empty root is then
    left in place and reported.
  - **Torn tails and locking (XC3).** Before each append the journal's torn tail is
    truncated (`state.OpenHistory`'s `truncateTornTail`, `internal/state/history.go`). One
    mutex covers append and compaction.
  - **Compaction.** Temp + rename once the journal passes 1,000 lines; it keeps the last
    200 runs and copies raw lines verbatim, including lines with an unknown `v`, which it
    never reinterprets (XC3). Fingerprints survive compaction in the separate fingerprint
    ledger (§4.10, XC14).
  - **Reading.** Strings are control-stripped (`util.StripControl`), malformed lines are
    skipped, and only this host's lines are reconciled. The journal is an index. The run
    record is the app's truth (§4.19).
- **Reconciliation (XC4): once per run per launch, in the background, downgrade-only.**
  - **How it runs.** A background goroutine reconciles each run whose last event is
    `start` or `sealed` **once per launch**. Each probe is bounded to 5 s, and a hung
    probe leaks only that goroutine. Results are cached for `/api/exports/history`.
    Progress (`/api/exports/progress`) never waits on it.
  - **What it reads.** Status comes **only** from the journal and the run record. The
    export directory is probed only for presence, for the marker, and for an identity
    and run id that match the record (XC3). It is never parsed for status, ids or hashes,
    and **no fingerprint is ever computed from disk**.
  - **Outcomes:**
    - directory missing → "folder not found (at <time>)";
    - identity or run id mismatch → "this folder was moved or replaced since the export";
    - marker present and no `sealed` record → "interrupted (the app stopped while this
      export was running)", offering **Finish as partial** (XC2);
    - marker present and a `sealed` record → "Finished writing but not marked complete",
      offering **Check and finish**, which verifies sha256(SUMS on disk) = the sealed
      fingerprint and every listed file, then removes the app's own marker (XC5);
    - marker absent and a `sealed` record → "complete (recovered)", with the sealed
      fingerprint;
    - marker absent and no `sealed` record → "changed after the export — run Check this
      export", with "fingerprint not recorded at export time".
  - **Recording.** Each result is appended once as a terminal `reconciled` record, so no
    run is ever probed again. Losing the journal loses the history list. Losing the
    fingerprint ledger loses the sender's tamper anchor (P9). Exports are unaffected.
- **Export activity page** (`exports`):
  - **Running export** (§4.13 progress, Cancel).
  - **History:** time, source, destination, status label, counts, fingerprint (copy
    button, "Save fingerprint receipt"; XC14), and the set fingerprint for a retry chain
    (XC15).
  - **Actions:** "Open folder"; "Check this export" (XC14: re-hash every listed file
    against `SHA256SUMS.txt`, compare sha256(SUMS) with the sealed fingerprint, list
    unlisted files; GREEN/RED per X6, and recommended on the finished card for removable
    media); "Export failed messages again"; "Export the remaining messages"; "Finish as
    partial" and "Check and finish" (XC2, XC5).
  - **Set fingerprint (XC15):** SHA-256 over the chain's `SHA256SUMS.txt` files in part
    order.
- **"Open folder" (XC33)** never takes a path from the request. Before opening the
  recorded export root it requires all of these:
  - Lstat shows no symlink or reparse point;
  - the path is a directory;
  - its identity equals the root identity recorded at creation;
  - the marker or the manifest is present.

  Otherwise it refuses with R-r ("this folder was moved or replaced since the export").
  It opens with reveal semantics that never launch anything: macOS `open -R
  <root>/README.txt`, Windows `explorer.exe /select,<root>\README.txt`, Linux `xdg-open`
  on the verified directory. On macOS an `.app` bundle is a directory, so the Lstat check
  alone would not be enough.
- **Microsoft 365 entry** (`./#sec-m365`, relative, extended): tenant, client id,
  sign-in mode, "Signed in as alex@contoso.com" with "Clear sign-in" (existing). It adds a
  **Sign in** button when not signed in. The button runs `api/m365/signin` in its own
  slot (XC8) and shows the device code from the guarded `api/m365/signin-status`, never
  from `GET /api/activity`. **XC7:** when a weekly backup is installed and the new UPN
  differs from the previous signer, nothing is persisted until the user answers "Signed
  in as bob@ — your weekly backup will now archive bob@ into C:\MailArchive" with **Keep**
  or **Sign out**. **Scope lines, per source (XC21):** "Microsoft 365: your own mailbox
  only (alex@contoso.com), read-only (Mail.Read)." "Archive: every mailbox this archive
  holds (alex@contoso.com, bob@contoso.com)." "Shared mailboxes and other people's live
  mailboxes aren't available in this version. They would need a different Microsoft
  permission (Mail.Read.Shared) that this app does not request." It reflects the session:
  a workspace sign-in shows here and vice versa (one token store, with identity-safe
  write-back, XC7).

### 4.17 Settings

`desktop-settings.json` (version stays 1) gains optional fields `exportDest string`,
`exportPreserve *bool`, `exportHTML *bool`, `exportAttachments *bool`. `nil` means the
default: Preserve ON; Readable HTML OFF until slice E5 lands, then ON (XC28); attachments
OFF. They are saved when an export starts. **Serialized (XC34):** one mutex covers every
settings load-modify-save, `/api/settings` and export start alike. Each save reloads the
file under the lock and changes only the fields its caller owns: export start changes
only the four export fields. So two tabs, or a settings save racing an export start, can
never write back a stale `Out` that the scheduled backup would then read. Go's JSON
decoder ignores unknown fields, so an older binary reads the file. An older binary that
saves settings drops these fields, which is harmless (§6).

### 4.18 Inherited findings and their disposition

| Finding | Disposition |
|---|---|
| F1 meta refresh smuggled via SVG/MathML (`go.mod:11`) | Fixed upstream (x/net v0.60.0; MA-300/MA-301). Export pages reuse `RenderWith` and MA-423 re-runs the corpus. Archived pages rendered before the fix are detected and quarantined as `.txt` (P10). |
| F2 any Host on GET (`server.go:102`) | **E0 prerequisite** = `docs/design-local-confidentiality.md` slice C1 (XC23). Until then the workspace exposes only POST data endpoints (Host-checked by `GuardLocalPOST`, D2) and data-free page shells. |
| F3 CSRF token to anyone; cross-user dashboard (`dashboard.go:90`) | **E0 hard prerequisite** ([ruling Q5]) = local-confidentiality slice C2 (XC23). **Re-premised (XC24):** the shipped dashboard already lets another local account copy the live mailbox to a path (POST `/api/settings` with any `Out`, then `/api/capture` with the victim's token). E0 closes that exposure on its own merits. The workspace inherits it and adds a faster selective copy and a subject-read surface, so it must not ship while another local account can POST. |
| F4 port squatter (`main.go:92`) | **E0.** A look-alike dashboard could phish a workspace sign-in. |
| N9 / N10 / F20 reader `/files` exposure and race | Not used. A destination inside the archive is refused, so the reader never serves an export. Archive reads use `os.Root` plus the Lstat gate. |
| C2-1 world-readable `search.db` / 0755 directories | Avoided for exports: 0700/0600 at creation (MA-439). **Conditional (XC12):** only on filesystems with POSIX modes. On FAT/exFAT/SMB/NFS the mount or share decides, and the preview's posture line says so. Windows ACL posture: inherited from the destination and named by the posture line [ruling Q10 = J3]. The archive's own modes are not this design's. |
| C2-2 symlinked folder inside the archive | Archive reads are symlink-strict (`extractInspect` + `os.Root`). |
| C2-3 / C1-2 redaction residue in `search.db` FTS / folder pages | Selection resolves from the manifest (redacted records are absent). The list reads `docs` rows, which redaction deletes. |
| F5 reader holds `search.db` | The workspace opens it per request, read-only, and closes it. A list request's shared lock keeps it out of the in-process Rebuild's rename window (XC20). It narrows F5's window to the request; it does not remove F5. |
| F9 one bad `$value` halts the walk (`graph.go:621`) | Avoided: per-message classes (§4.13). Hazards 1 and 2 cannot arise (no manifest writes); hazards 3–5 adopted. |
| F11 position paging | Mitigated by count reconciliation (§4.7); residual stated. |
| F15 token fetch without overall timeout | **Fixed in E1 (XC26):** `httpTransport`'s client gets `Timeout: reqTimeout`, which bounds token fetches and the `deviceSource` mutex hold. That fixes F15 for capture and the CLI too. |
| F17 throttle waits count against the 60 s deadline; 5xx not retried | Dependency, owned by fault-isolation as its slice FI-GF17, after its S3 [ruling Q15]. **Conditional (XC26):** a listing under a long Retry-After shows R-v only once FI-GF17 or S3's typed throttle error lands. Until then it surfaces as a deadline error that the preview reports as "Microsoft 365 didn't answer in time — try again in a few minutes", not as R-v. 502/503/504 after retries are transport-class (backoff, XC9). F17 also conditions §4.14. |
| F55 case-only folder collisions | Avoided by sibling FoldKey de-duplication (MA-418). |
| F75 bidi / F91 byte cap / F111 reserved names (`util/paths.go`) | **[ruling Q15]** For exports: an export-only, stricter naming pass layered over the util sanitizers, built in E2, so MA-417's sub-cases pass with no archive migration. The archive-side fix needs its own migration and stays open outside this design, as an open gap, not accepted: E2 lands a skip encoder citing R4 for the archive writer's names. `os.Root` is the Windows backstop. |
| C1-1 / F24 / F25 parser limits | Original unaffected (M365). Derived files carry caveats. "Extract attachments" is not shipped before C1-1 (E6, [ruling Q9]). |
| F26 PST embedded items | Archive source only. Cannot be detected; README caveat for PST stores. |
| F49 / F50 / F85 index gaps, read-only medium | Folder selection and counts come from the manifest. The list may miss rows ("K messages in this folder are not in the search index — Rebuild search index fixes the list"). On a read-only copy the list is unavailable, and whole-folder selection still works. |
| F8 invisible startup errors | Inherited, unrelated to the workspace. |

### 4.19 The run record: the app's system of record (XC1, XC2; review hidden blocker)

Revision 1 kept the plan only in memory and treated the export directory as the truth,
for recovery, integrity, status and identity. That directory may be removable, full,
shared, cloud-synced or FAT, with another program writing there (§5.2). Revision 2 moves
the app's truth into one local file per run. The export directory becomes the
recipient's.

- **Where.** `<state dir>/exports/<runId>.jsonl`, mode 0600, in the per-user, local,
  non-roaming state directory (Windows `%LOCALAPPDATA%\MailArchive`, macOS
  `~/Library/Application Support/MailArchive`, Linux `$XDG_STATE_HOME/mailarchive`). That
  is the same directory as the journal and the fingerprint ledger (§4.16). The free-space
  floor applies to its volume too (XC11).
- **Header** (line 1): `format_version`, run id, host, source identity (signer UPN or
  archive path, plus per-store mailboxes), destination, export root path and **identity**
  (dev+inode, or Windows volume serial + file ID, read after the exclusive mkdir), and
  options.
- **Plan:** the whole previewed plan, one line per message: source id, folder path,
  subject, from, received, size hint. The header and the plan are fsynced **before** the
  marker is written (W4). From then on, "what was planned" survives any kill.
- **Progress:** one fsynced line per finished message (status, class, reason, and each
  file with path, role, SHA-256 and size), after that message's files are placed. The
  hashes are computed as the bytes stream. On local disk this costs about 1 ms per
  message.
- **Seal:** after `SHA256SUMS.txt` is written (W9) and before the marker is removed
  (W10), a `sealed` line carries the fingerprint, computed from the in-memory SUMS bytes,
  and is fsynced. The same value goes to the journal and the fingerprint ledger.
- **What is built from it.** `export-manifest.json` and `SHA256SUMS.txt` are streamed from
  the record, and their per-file hashes are the recorded ones. Derived HTML is rendered
  only from read-back bytes whose hash equals the recorded one. There is no destination
  spool.
- **What reads it.**
  - Retry and "Export the remaining messages" (P4's second admission path).
  - "Finish as partial", which finalizes the app's own root from the record. Only
    recorded files that are present under the root (Lstat) are listed. Directory syncs
    are batched, so a file recorded just before a power loss may be gone, and it is then
    `not_attempted`.
  - "Check and finish".
  - Reconciliation (§4.16).

  None of them parses the destination's `export-manifest.json` or derives a status, id or
  fingerprint from destination content.
- **Version rules (XC3).** A reader refuses a newer `format_version` with "made by a newer
  MailArchive Desktop — update to retry", and tolerates additive fields.
- **Lifetime.** A record is deleted when its run and its whole retry chain are complete,
  or when journal compaction drops the run. Its fingerprint survives in the ledger.
- **Residual (stated, §8.4).** A co-writer on the destination can still alter or add files
  *after* they are written. The recipient's check, the extra-files check and "Check this
  export" detect that. Nothing in the run vouches for it.

## 5. Crash ordering and failure domains

### 5.1 Every write, killed at each step

Revision 2 order (XC1, XC3, XC5): the exclusive mkdir now comes **before** the journal
`start`, and the run record replaces the destination spool.

| # | Write (order) | Killed before | Killed during | Killed after | Fails toward | Operator sees |
|---|---|---|---|---|---|---|
| W0 | Preview (writes nothing) | — | — | — | nothing | preview lost; preview again |
| W1 | Create folder: one `os.Mkdir` | nothing | atomic | an empty folder | harmless | the folder exists, empty |
| W2 | Export root: exclusive mkdir through the destination handle; random-suffixed name; identity read (XC3, XC32) | no folder | atomic | an empty folder with no marker and no journal record (sub-second window) | an empty folder is plainly not an export, and no record points at it | an empty `MailExport-…` folder; README-less, so nothing claims it |
| W3 | Run record header + plan (append, fsync), then journal `start` with the real name and root identity (append after `truncateTornTail`, fsync) (XC1, XC3) | empty root, no record | torn line, truncated before the next append | record | a record for an empty, marker-less root | "interrupted"; **Finish as partial** writes a 0-message STOPPED export, or the folder can be deleted |
| W4 | `_EXPORT-NOT-FINISHED.txt` carrying the run id (temp → fsync → rename → dir sync) | root + record, no marker | temp residue | marker | unfinished | "interrupted"; marker explains; Finish as partial |
| W5 | Per message: temp → `Sync` → no-replace rename, for each file (directory syncs batched, XC5) | previous files only | `.mailexport-*.tmp` residue, never a partial file under a final name (R5 analog) | file complete | unfinished (marker present) | "interrupted"; marker; Finish as partial |
| W6 | Run-record line for the message (append, fsync, after its files) (XC1) | the files exist, unrecorded (not listed by Finish as partial) | torn last line, truncated | recorded | unfinished (marker present) | same |
| W7 | Presence check, then `README.txt` (atomic; status conditional in its own text, XC5) | — | temp residue | README, which says "complete — valid only while no _EXPORT-NOT-FINISHED.txt is in this folder" | unfinished (marker present); README's own text says not to trust its status | "interrupted"; Finish as partial |
| W8 | `export-manifest.json` (temp, streamed from the run record → fsync → rename; `valid_if_absent`) | — | temp residue | manifest | unfinished (marker present); **reconcile never reads it** (XC4) | "interrupted"; Finish as partial |
| W9 | Directory syncs, then `SHA256SUMS.txt` (atomic, built from recorded hashes) | — | temp residue | sums | unfinished (marker present) | "interrupted"; Finish as partial |
| W9a | Run record `sealed` line with the in-memory fingerprint (fsync) (XC1, XC5) | sums on disk, not sealed | torn line | sealed | unfinished (marker present) but provably written | "Finished writing but not marked complete" → **Check and finish** |
| W10 | Remove the marker (complete/partial) or rewrite it (cancelled/failed); dir sync | marker present: claims less than the truth (safe) | atomic | done | complete | complete |
| W11 | Journal `end` + fingerprint ledger line | journal says sealed | torn line | record | reconcile reads the **sealed** line, never the folder (XC4) | "complete (recovered)" with the sealed fingerprint |
| W12 | Journal compaction (temp → rename, under the journal mutex, unknown-`v` lines kept verbatim) (XC3) | old journal | temp residue | new journal | at worst, history lost; fingerprints kept in the ledger | shorter history |
| W13 | Settings (existing `util.WriteFileAtomic0600`, under the settings mutex, XC34) | old | — | new | old defaults | previous defaults |

**The invariant this ordering buys (P14, restated by XC5).** Any export directory that
contains a message file carries the marker or is complete. A W2–W4 kill leaves an
empty or temp-only root, which is reconciled as interrupted. One rule covers every kill
point: a marker present is **never** shown as "complete". The W7–W9a states show
"interrupted" or "Finished writing but not marked complete", never a recovered
"complete". README and the manifest say in their own text that their status is valid
only without the marker, so no file in the folder claims completion beside the marker.
Every crash fails toward "says unfinished". It never fails toward "looks complete".
**Conditional, stated inline in P14:**
- "Durable" depends on the filesystem ordering metadata operations (NTFS, APFS, ext4
  data=ordered). On Windows a directory fsync is a no-op, and the claim rests on NTFS's
  journaled metadata.
- **FAT32 and exFAT** (the USB-stick case) are not journaled. A yank or power loss can
  persist the marker's removal before `SHA256SUMS.txt` or file data. On them the
  recipient's `sha256sum -c` is the backstop, and the app and README say "Eject before
  unplugging".

MA-429 proves the **ordering** by fault injection at every step, because a process kill
still lets the OS flush. It does not prove power-loss durability. Apart from finishing
its own run (XC2, XC5), the app never cleans residue (§3). README and the activity page
tell the user the folder can be deleted.

### 5.2 Failure domains

| Component | Lost | Compromised / misbehaving | Fails | Operator sees |
|---|---|---|---|---|
| Graph session | rebuilt from the token store; R-b if the token is gone; released after 30 idle minutes (XC18) | a token leak is the existing capture posture (D3/D4). **XC7:** the session holds the refresh token in memory for its lifetime. It writes back only same-UPN rotations to an *existing* entry, so a sign-in cleared in the dashboard, in Credential Manager or by deleting the token file is never re-created | closed | sign-in prompt; a running export stops with R-z |
| Preview store | lost on restart, expiry or a new preview | ids validated against the session (P4) | closed (R-k, R-y) | "preview again" |
| Run goroutine | killed with the process (§5.1); recovered by Finish as partial from the run record (XC2) | panic recovered → `failed`, marker kept; wedged in the kernel 30 s after Cancel → slot released, "Stopped: the destination is not responding", one OS thread held until the syscall returns or the app restarts (XC10) | closed | "Stopped: an internal error" / "not responding" |
| **Run record** (XC1) | its run can no longer be retried or finished from the app; the export on disk is unaffected and still self-verifies | same-user file, 0600, local; a reader refuses a newer format legibly | closed (no retry offered) | "this export's run record is missing — Check this export still works" |
| Destination volume | removed or full → run-fatal; the per-OS errno table, root probe, 5-failure breaker and free-space reserve make that hold on Windows, SMB and FAT too (XC10, XC11) | another program writing there: only the export root is ours, under `os.Root`. The app's truth is the run record, never this volume (XC1, XC4) | closed, marker kept; finalization fits the reserve | R-g / R-g2; "Export the remaining messages" from the run record |
| Archive (source) | missing → R-o; gone mid-run → run-fatal R-o2 (XC10) | damaged or swapped files → fixity mismatch per message; hard links refused (XC19) | closed per message | listed failures; "run Check integrity" |
| Journal | history empty; fingerprints survive in the ledger (XC14) | malformed lines skipped; torn tail truncated (XC3); paths are never trusted for deletion (there is no deletion) | open (exports unaffected) | shorter history |
| Fingerprint ledger (XC14) | the sender's tamper anchor for past exports is lost, unless a receipt was saved | same-user file, 0600 | open | "fingerprint not recorded" |
| Native picker | unavailable → typed path | **crash** in a shell extension (Windows, in-process) kills the dashboard process: the run is interrupted and recovered by Finish as partial. **Hang**: `zenity.Context` cannot dismiss it, and Browse stays refused until restart. Browse is disabled while an export runs (XC31) | open to the typed fallback | R-n; "a folder picker is already open" |
| An export after hand-over | — | tampered → `sha256sum -c` mismatch; re-hashed throughout → caught only by the fingerprint; files **added** → caught only by the extra-files check or Check this export (XC14) | detectable | the recipient's check fails |

No new single point of loss: the export is a copy, and its sources are untouched. The
run record is the one new component whose loss costs anything: it costs retry and finish
for that run, never the export itself.

## 6. Migration / coexistence

- **Existing archives** are read with today's loaders (`state.Load` handles v1–v6 in
  memory; a newer format is refused with its own wording). Nothing is migrated, written
  or locked. The archive and index formats are unchanged (one new read-only query).
- **Existing manifests:** an archive-source export reads `Fixity` when present. A
  pre-fixity record is copied "as stored, unattested" (a note), like `extract`'s
  `EmittedNoFixity`.
- **Schedules** are unaffected. The scheduled headless capture (DC1) never runs the
  workspace, exports are not schedulable, and neither source takes the archive lock.
- **Settings:** optional fields, version unchanged. An older binary ignores them
  (reads) or drops them (writes), and the user loses only the remembered destination
  and options.
- **New files** (the journal, run records and fingerprint ledger, in the local state
  directory) are ignored by older binaries.
- **Graph:** no new permission, scope or app-registration change. The
  `totalItemCount`/`parentFolderId` selects and the PidTagMessageSize / PidTagSensitivity
  `$expand` are v1.0, with the MA-285 fallback. **Corrected (XC35):** existing *device*
  consents work. An install configured `auth=app` has an app-only registration with the
  Application Mail.Read permission and a secret. It has neither "Allow public client
  flows" nor the Delegated Mail.Read permission that the workspace's forced device
  sign-in needs. That sign-in is refused with R-a2, which names the fix, instead of a raw
  Entra error.
- **Mixed versions (corrected, XC3).** Revision 1 said "no reader of exports exists".
  That was false: retry and reconciliation read the app's own formats. In revision 2 the
  readers are the run record (§4.19) and the journal. The destination manifest is no
  longer read by the app at all (XC4), which removes the cross-version exposure revision
  1 had. The rules:
  - A reader refuses a newer `format_version` with "made by a newer MailArchive Desktop —
    update to retry", and tolerates additive fields.
  - Journal compaction copies raw lines verbatim and keeps lines whose `v` it does not
    know.
  - Exports stay self-describing (`format_version: 1`): additive changes keep version 1,
    anything else bumps it.
- **Coexistence with `extract`:** unchanged and still the whole-archive migration path.
  The workspace is selection-based hand-over. Their exit partitions agree (X1). **Shared
  code or a stated fork (XC36):** `extract`, the `Exporter` and the workspace use one
  shared archive-read helper (`validRelPath` + Lstat walk + `os.Root` read + fixity) and
  one exported stem / unique-name helper (`baseNameN`, `stemFor`, a casefold
  `uniqueName`). Otherwise this section names the fork and its owner before E4, and P8's
  "the same gate" is withdrawn. Whether the workspace supersedes the approved desktop
  design's P7 "Export a copy (extract mbox/eml)" is escalated to the owner [ruling ESC-5].
  Until the owner answers, nothing here declares P7 superseded: it stays an approved,
  unbuilt desktop item, and E3 ships the workspace and its Export sidebar group beside it.
- **E0 cutover (XC24).** E0 is `docs/design-local-confidentiality.md` slices C1 + C2, and
  its own migration section owns the cutover: the landing page for old bookmarks, the
  RMM verification step (`packaging/mailarchive-desktop/README-RMM.md:152-156`, "Open
  http://127.0.0.1:8097/") and the dashboard-to-reader links. The workspace adds nothing
  to it beyond using relative URLs (XC23).
- **In-flight exports during an upgrade (XC6).** With the Windows end-session window,
  Restart Manager's close request during an RMM-pushed MSI upgrade drains the run: it is
  cancelled and finalized, and "Export the remaining messages" is offered after the
  upgrade. Without the window, the run is killed and recovered with "Finish as partial"
  and then "Export the remaining messages", both from the run record.
- **Rollback (stated as it really is, XC37).** The MSI blocks a downgrade
  (`Product.wxs:37-39`, MajorUpgrade with `DowngradeErrorMessage`). Rolling back the
  workspace by binary therefore means uninstalling and reinstalling the older MSI. That
  also removes every other fix in the release, including E0 if E0 shipped in the same
  release. **[ruling Q6]** The deployment switch is therefore the rollback path: when it
  is off, the workspace and export routes are never registered (404) and the sidebar
  group is hidden (MA-526). Either way, exports on disk stay self-contained, the journal
  and run records are orphaned harmlessly, and there is no archive state to roll back.
- **Desktop binary footprint (corrected, XC31).** The MSI gains no component, but zenity
  is **new to the `mailarchive-desktop` binary**: `go list -deps ./cmd/mailarchive-desktop`
  shows no zenity today, and only `mailarchive-gui` links it. On Windows this brings an
  in-process `IFileOpenDialog`, which loads shell extensions into the dashboard process.
  On macOS it means `osascript`. On Linux it means a `qarma`, `zenity` or `matedialog`
  binary, which is optional (typed path otherwise).

## 7. Operator walk

Step = one deliberate user action (a click on a control or a dialog choice; ticking N
rows counts as one "pick rows" step). Friction types follow `friction-review.md`.

### 7.1 Home user — Inbox (with subfolders) plus most of Sent Items to Downloads (signed in)

1. Sidebar → **Extraction workspace**.
2. Source shows "Microsoft 365 — your mailbox (alex@contoso.com)" → **Load folders**.
3. Click **Inbox** (its messages list), tick **Include subfolders**, click **Select
   current folder** → "Selected 1,766 messages from Inbox (and its 3 subfolders)."
4. Click **Sent Items** → **Select current folder** → "Load more messages" ×3 → untick
   76 rows (one "pick rows" step). The footer reads "2,088 messages selected".
5. **Browse** → the OS picker → choose Downloads, create "Archive Folder" in the picker
   (or type the name in New subfolder name → **Create folder**).
6. Options: defaults (Preserve ON; Readable HTML ON from slice E5, hidden and OFF before it, XC28); optional.
7. **Preview copy export** → about 30 s → "2,088 messages · about 1.4 GB (12 of unknown
   size) · into C:\Users\alex\Downloads\Archive Folder\MailExport-2026-10-09_142233 ·
   about 10–20 minutes".
8. **Start export**.
9. Export activity → "Complete — 2,088 exported. Fingerprint 3f2a91c0 …" → **Open
   folder**.

**9 steps**, no secrets handled, no out-of-band transfer (one more if they send the
fingerprint). Not signed in: +3 (**Sign in**, open the Microsoft page, enter the code).
Type I.

### 7.2 Home user — the ugly cells

Each cell is scored for whether it functions, the step count, and the re-work time
(XC35). Retry ships in E3 (XC28), so every remedy here exists in the first shippable
slice.

| Cell | What happens | Steps to recover · re-work | Type |
|---|---|---|---|
| One message cannot convert | "Finished with problems — 1 message not exported" (subject, folder, reason) | **Export failed messages again** → **Start export** (2) · that message only | I (Microsoft's conversion) |
| Sign-in expires or is revoked mid-run | R-c (also when the refresh token itself is refused, XC9); the run stops; the marker says STOPPED | **Sign in** (3) → **Export the remaining messages** → **Start** (2) · the remaining messages | I |
| USB stick full | R-g at the free-space reserve, so finalization still fits (XC11); marker says STOPPED | free space or choose another destination; **Export the remaining messages** → **Start** (2), from the run record even if the stick was removed | I |
| Changed their mind | **Cancel** (1); marker says CANCELLED | delete the folder by hand (1) | I |
| Laptop closed / app killed (XC2) | next launch: "Interrupted"; folder has the marker | **Finish as partial** (1) → **Export the remaining messages** → **Start** (2) · re-work is the remaining messages only, not a redo | I (revision 1 was Type II; resume-from-spool retired) |
| Network blip mid-run (Wi-Fi roam, wake, VPN) (XC9) | the run backs off on the same message and continues; nothing fails | none (0) · seconds | I |
| Network down > 5 minutes (XC9) | R-u, outcome 1 | **Export the remaining messages** → **Start** (2) · the remaining messages | I |
| FAT32 stick, 10,000-message Inbox (XC12) | the preview WARNs (FAT32, 4 GiB per file, permissions not kept); ≤1,000 files per directory, so no directory-full failure | none (0) | I |
| Clears the sign-in, or signs in as someone else, mid-run (XC7) | R-z; the run stops cleanly; the token is not re-persisted | **Sign in** as the intended account (3) → **Export the remaining messages** → **Start** (2) | I |
| Forced Windows restart overnight (XC6) | with the end-session window: drained → "Cancelled"; without it: "Interrupted" | **Export the remaining messages** (or **Finish as partial** first) → **Start** (2–3) · the remaining messages | I |
| AV quarantines a written `.eml` (XC14) | the presence check before the sums makes the run partial and names the file | **Export failed messages again** → **Start** (2), or restore it from quarantine and **Check this export** (1) | I |
| Retry after a restart (XC2) | retry reads the run record, so it works after any restart | **Export the remaining messages** → **Start** (2) | I |
| Linux, no zenity | R-n | type the path (1) | III (bundling a picker is backlog) |
| Picks 12,000 rows by hand | R-j (reachable now that picks are row handles, XC17) | **Select current folder** + untick (2) | I (bound) |

### 7.3 Admin / IT handing over a departing user's mail

v1 cannot read anyone else's **live** mailbox (OW3), and the Microsoft 365 card says so
in its own words. **Corrected (XC21):** if the configured archive already holds the
departing user's mail, for example from an app-only CLI capture with one store per
mailbox, the archive source can export it. The card and the tree name every mailbox the
archive holds, and the manifest records it. Otherwise the admin either runs the workspace
signed in as that user (with the user's cooperation, on their machine) or uses Purview.
**Type I** (the permission model). To check an export before handing it over, there are
two routes:
- In the app: **Check this export** on Export activity (1, XC14).
- By hand: open a terminal in the folder, read `README.txt` and run the verify line plus
  the extra-files check (2).

Then copy the fingerprint from Export activity into the hand-over email with its copy
button, or save the receipt (1).

### 7.4 Inheritor opening the archive cold (has the archive folder and MailArchive Desktop)

**[ruling W1; Q3, owner-direct]** The inheritor no longer repoints Settings. The walk:

1. **Extraction workspace** → Source **Another archive folder…** → **Browse** → the
   inherited folder (or type its path) → **Load folders** (stores → folders, counts from
   its manifest). Nothing is written in that folder, and Settings and the weekly backup are
   untouched, so the inheritor's own backup keeps archiving the inheritor's mailbox into the
   inheritor's own archive.
2. Select folders and messages (as 7.1, steps 3–4).
3. **Browse** → destination.
4. **Preview copy export** → "Of 2,088 selected, 312 have an original .eml; 1,776 will
   be copied as the archived page and attachment zip (no original was kept). 41 are no
   longer in the mailbox (kept in the archive)."
5. **Start export** → **Open folder**.

**About 9 steps**, one fewer than revision 2's Settings route, with no hazard. From a
read-only copy (DVD), the message list is unavailable (F85); whole-folder selection still
works. The revision 2 residual (Type II, PARTIAL in the review: to read an inherited archive
the inheritor had to give up the backup or retarget it) is closed by this source.

**A user who still repoints Settings** (Settings → Archive location → the inherited folder →
**Save settings**) keeps XC22's guard. If a weekly backup is installed, the next scheduled
capture would write the user's own mailbox into that archive, because settings are read at
run time (MA-279). So when a weekly backup is installed and Archive location changes, **Save
asks first**: "Your weekly backup will now archive <signed-in UPN> into <new folder>". The
choices are **Save and keep the backup**, **Save and remove the weekly backup**, and
**Cancel**. The harmful save is an informed one, at the moment it happens (MA-523).

### 7.5 Recipient opening the export cold (no tool)

1. Open `README.txt`: status (valid only while no `_EXPORT-NOT-FINISHED.txt` is present,
   XC5), mailbox, dates, counts, layout, caveats, verify commands.
2. Open a terminal **in the export folder** (README says how, per OS), then verify:
   Linux `sha256sum -c SHA256SUMS.txt`; macOS `shasum -a 256 -c SHA256SUMS.txt`;
   Windows: paste the PowerShell line ("OK: N files checked"). Run README's extra-files
   check too, which lists any file not in `SHA256SUMS.txt` (XC14).
3. Compute the fingerprint of `SHA256SUMS.txt` and compare it with the one the sender
   sent separately.
4. Open messages: `.html` in a browser (offline-safe), `.eml` in a mail client (README:
   "these are the original messages, open them with the same caution as email").

**3–4 steps**, one out-of-band value. Type I.

### 7.6 Refusal catalogue (what / why / remediation; X2 voice)

| Id | Message |
|---|---|
| R-a | "Microsoft 365 isn't set up yet — the workspace needs your tenant and application ID to read your mailbox. Run `mailarchive setup`, then come back." |
| R-b | "You're not signed in to Microsoft 365 — the workspace reads your mailbox with your own sign-in. Click Sign in and enter the code shown." |
| R-a2 | (XC35) "This installation is set up for app-only access, which can't sign in as you — the workspace reads your own mailbox with a device sign-in. In the app registration, turn on 'Allow public client flows' and add the Delegated Mail.Read permission, or register a separate device app (docs/graph-app-setup.md)." |
| R-c | "Microsoft 365 refused the request (your sign-in expired or access was revoked) — the export stopped after 1,203 of 2,088 messages. Sign in again, then click 'Export the remaining messages' on Export activity." Also raised when the refresh token itself is refused (`invalid_grant`, `interaction_required` …), never shown as "check your network" (XC9). |
| R-d | (XC3) "MailArchive Desktop couldn't record this export in C:\Users\alex\AppData\Local\MailArchive (disk full or access denied) — nothing was exported. Free some space there, then click Start export again." |
| R-e | "The folder C:\Users\alex\Downloads\Archive Folder doesn't exist — an export is written into an existing folder. Click Browse to choose one, or Create folder." |
| R-f | "That folder is inside your archive (C:\MailArchive) — an export written there would mix into the archive. Choose a folder outside C:\MailArchive." |
| R-g | "The destination is full or no longer available (E:\ — no space left) — the export stopped after 812 of 2,088 messages and the folder is marked unfinished. Free some space or choose another destination, then click 'Export the remaining messages'." The remedy works even when the destination is gone, because it reads the run record (XC2, XC11). It names the real cause from the per-OS table, e.g. "the drive was removed" or "the network share is gone" (XC10). |
| R-g2 | (XC11) "E:\ has 1.1 GB free, but this export needs about 1.4 GB plus room to finish (the manifest and checksums) — nothing was written. Free some space or choose another destination." |
| R-h | (Preview disabled) "Select at least one folder or message to preview." |
| R-i | "This selection has 240,000 messages — one export copies up to 200,000. Split it into several exports, for example by folder." |
| R-j | "You've picked 10,000 individual messages — the most one selection can hold. Select the whole folder (Select current folder) and untick the ones you don't want." |
| R-j2 | "A message in this selection wasn't listed in this session — the page may be out of date. Reload the folder and select again." It never applies to retries, which are admitted by the run record (XC2). |
| R-k | "This preview is out of date (older than 30 minutes, or the selection or destination changed) — click Preview copy export again." |
| R-l | "This selection was made in alex@contoso.com's mailbox, but you're now signed in as bob@contoso.com. Load folders again to start a new selection." |
| R-m | "An export is already running (started 14:02, 812 of 2,088 done) — only one runs at a time. Wait for it or cancel it on Export activity." |
| R-n | "The folder picker isn't available on this computer — the OS folder dialog needs the `zenity` program on Linux. Type the folder path in the box instead." |
| R-o | "There's no archive at C:\MailArchive yet — run Archive now first, or choose Microsoft 365 as the source." |
| R-o2 | (XC10) "The archive at \\\\nas\\MailArchive is no longer available (it was disconnected) — the export stopped after 1,500 of 2,088 messages; nothing in the archive is damaged. Reconnect it, then click 'Export the remaining messages'." |
| R-o3 | [ruling W1] "D:\Inherited\Mail isn't a MailArchive archive (it has no .mailarchive-manifest.json) — choose the archive's top folder, the one that holds index.html and README.txt." Nothing is read beyond that check, and nothing is written. |
| R-p | (the loader's own wording) e.g. "The archive's manifest was written by a newer MailArchive (format 7) — update MailArchive Desktop to export from it." |
| R-q | "A folder named 'Archive Folder' already exists here — choose it with Browse, or use another name." / "'a/b' can't be a folder name (it contains '/') — try 'a b'." |
| R-r | (XC33) "This export's folder was moved or replaced since the export (E:\…\MailExport-2026-10-09_142233-7f3a) — MailArchive won't open it. Find the folder yourself, then run Check this export." |
| R-s | "This mailbox has more than 10,000 folders — more than the workspace can show. Export from the archive instead, or contact support." |
| R-t | "Couldn't create a new export folder in C:\…\Archive Folder (MailExport-2026-10-09_142233 through -99 already exist) — choose another destination." |
| R-u | (XC9) "The connection to Microsoft 365 has been down for 5 minutes — the export stopped after 1,500 of 2,088 messages; none of them was marked failed. Check your network, then click 'Export the remaining messages'." |
| R-u2 | (XC9) "Microsoft 365 is having problems (25 messages in a row returned a server error) — the export stopped after 1,500 of 2,088 messages. Try again later with 'Export the remaining messages'." |
| R-v | "Microsoft 365 is limiting requests right now (throttled) — the preview couldn't list Inbox. Try again in a few minutes." **Conditional (XC26):** shown only once F17 (fault-isolation's FI-GF17, [ruling Q15]) or S3's typed throttle error lands; until then a long Retry-After surfaces as "Microsoft 365 didn't answer in time — try again in a few minutes." |
| R-w | (XC12) "MailArchive couldn't create a folder in E:\Exports (access denied) — choose another folder, or allow MailArchive Desktop in Windows Security → Controlled folder access (macOS: System Settings → Privacy & Security → Files and Folders)." Nothing is written. |
| R-x | (XC18) "This list expired — reloading Inbox from the newest message." (shown when a keyset continuation is impossible; the selection is kept) |
| R-y | (XC18) "MailArchive Desktop restarted — reload the page; your selection can't be reused." |
| R-z | (XC7) "The Microsoft 365 sign-in was cleared or changed (now: bob@contoso.com) — the export of alex@contoso.com's mailbox stopped after 900 of 2,088 messages, so nothing is read with the wrong account. Sign in as alex@contoso.com, then click 'Export the remaining messages'." |

No refusal echoes a token, a Graph URL with its query, or a stack trace (MA-438; X7).

## 8. Security

### 8.1 Trust boundaries

1. **Browser page ↔ dashboard process** (loopback HTTP). Guarded by `GuardLocalPOST`
   (CSRF + loopback Origin + loopback Host) on every data endpoint, reads included
   (X13), plus E0's per-launch, per-user session and Host allow-list on every method.
   E0 is `docs/design-local-confidentiality.md` C1 + C2, with routes registered through
   `localweb.Mux` and probed by MA-378 (XC23). The page shells carry no mailbox data.
2. **Dashboard process ↔ Microsoft Graph.** A delegated `Mail.Read` bearer token. GET
   only. Requests go only to the configured base:
   - `nextLink` is pinned after normalization (XC25);
   - redirects off the base are refused (XC26);
   - JSON bodies are bounded (XC26);
   - ids are `url.PathEscape`d;
   - page-supplied picks are row handles checked against the seen-set, and folder ids
     must be in the cached tree (XC17).
3. **Mail content → the dashboard page.** Untrusted. JSON (Go's encoder escapes `<`,
   `>`, `&`) with `nosniff`, rendered by `textContent` only (X15). The page CSP is the
   existing `dashCSP` (`script-src 'self'`).
4. **Mail content → disk.** Untrusted names → sanitized, FoldKey-unique, `os.Root`-
   contained. HTML → the R19 renderer. The original `.eml` is written as bytes and never
   interpreted by this tool.
5. **User → destination path.** A trusted user (after E0), but validated: an absolute,
   existing directory, not in the archive. A symlinked destination is allowed (the
   user's choice). At start the destination is opened once as a directory handle, its
   identity is re-checked, and the export root is created through that handle and then
   pinned with `os.Root` (XC32).
6. **Archive directory → export.** Semi-trusted: anyone who can write the archive.
   Every path is gated by `validRelPath` + `extractInspect` + `os.Root`, plus the nlink
   check (XC19); sizes are bounded, and fixity is checked. **Another archive folder [ruling
   W1]** is untrusted input: anyone may have written it. The same gates apply, the
   newer-format refusal applies, and nothing is ever written there (§4.3).
7. **OS dialog → process.** The user's choice. Not a boundary (§4.8). It does load
   third-party shell code into the process on Windows (XC31).
8. **Export directory → app (read-back; XC4).** Untrusted: other programs and accounts
   may write the destination. The app probes it only for presence, the marker, identity
   and run id. It never takes status, ids, hashes or a fingerprint from it. Its truth is
   the local run record (§4.19).

### 8.2 Untrusted inputs

| Input | From | Handling |
|---|---|---|
| Folder display names, subjects, sender names/addresses | mail senders; the mailbox owner | JSON-escaped, control-stripped, `textContent`; sanitized and FoldKey-unique on disk |
| Attachment file names | mail senders | `SanitizeFilename`, FoldKey-unique, root-contained; bidi conditional on F75 |
| MIME bytes | mail senders via Graph | written verbatim (bounded 200 MiB); parsed only for derived files, under the existing panic guard |
| Message HTML | mail senders | `RenderWith` + re-parse fail-closed (MA-300/301) |
| Graph JSON, `nextLink`, redirects | Microsoft, or a misbehaving or compromised server | bodies read through `io.LimitReader` (XC26); `nextLink` pinned to the base after normalization (XC25); redirects off the base refused (XC26) |
| Archive manifest, paths, zips | whoever can write the archive, or whoever wrote another archive folder chosen as a source [ruling W1] | path gates, no symlinks, fixity, zip-bomb bounds; for another archive folder also R-o3, R-p and the read-only open of §4.3 (MA-531) |
| Archived `.html` (no original) | the archive | live-meta detector; quarantined as `.txt` if live |
| Selection JSON, row handles, folder ids, cursors | the page | 1 MiB cap (413 → R-j-style refusal); row handles resolved through the seen-set; M365 folder ids only from the cached tree; message folders from `parentFolderId`; opaque cursors; caps (XC17) |
| Journal lines, run records, fingerprint ledger | the local state directory (same user) | control-stripped; torn tail truncated; newer formats refused legibly; never used to delete or write outside the app's own export root (XC1, XC3) |
| **Export directory read-back** (XC4) | the destination, which other programs or accounts may write | presence, marker, identity and run id only; never trusted for status, ids, hashes or a fingerprint; "Open folder" re-checks identity first (XC33) |

### 8.3 New surfaces

Eighteen POST endpoints (§4.2) and two page shells. A native dialog in-process. A
filesystem write primitive: create one new directory tree under a user-named path, one
mkdir, and finishing the app's own interrupted root (XC2). New Graph read methods
(folder tree with counts, page listing, streaming `$value`). One 0600 journal, per-run
0600 run records and a fingerprint ledger in the local state directory (XC1). On
Windows, a hidden end-session window (XC6). No new listener, port, permission or scope.
**Corrected (XC31):** no new *module* dependency, but ncruces/zenity is new to the
`mailarchive-desktop` binary, and its Windows COM dialog code becomes reachable in the
at-login process that holds the token. CI's govulncheck analyses GOOS=linux only, so CI
adds `GOOS=windows govulncheck ./cmd/mailarchive-desktop`, and the E3 adversarial pass
records zenity's new reachability.

### 8.4 Attacker personas

| Persona | Attack | Mitigation | Residual / label |
|---|---|---|---|
| Outside web page (CSRF) | POST `/api/exports/start` with a crafted plan | CSRF token + custom header (no CORS preflight granted) + Origin check; the start takes only a server-held preview id | none (proven, MA-405) |
| DNS rebinding | read folder lists or subjects; scrape the token from `/` | data endpoints are POST + Host-checked (D2); E0 Host allow-list on GET; the session cookie is not sent to the attacker's hostname | conditional on E0 for the page shells |
| Another local account (shared PC, RDS) | scrape the CSRF token, export the victim's mailbox to `C:\Users\Public` (and, today, already via `/api/settings` + `/api/capture`, XC24) | **E0** = local-confidentiality C2's per-user session and instance (F3/F4) (XC23) | **not shippable before E0** ([ruling Q5]) |
| Malicious sender | traversal or reserved names, bidi spoofing, long names, script or refresh in HTML, MIME/zip bombs, markup in a subject aimed at the dashboard | sanitizers + `os.Root`; R19 renderer; bounds; `textContent` | bidi/reserved/byte-length conditional on the util fixes (P7); the `.eml` is the original (README caution) |
| Archive-writer insider | symlink, hard link or swapped files to read outside the archive or poison the export | `extractInspect` + `os.Root` + nlink 1 (XC19) + fixity | a pre-fixity record is "unattested" (stated); the in-root check-then-use window is caught by fixity only when recorded |
| Misbehaving or compromised server | redirect paging or a 302 to an attacker host to harvest the bearer token; huge JSON or `$value` | `nextLink` pinning after normalization (XC25); `CheckRedirect` refusing off-base redirects (XC26); `io.LimitReader` on JSON; size cap on `$value` | none (defence in depth) |
| TLS-intercepting proxy | read the token | none: it already sees `Authorization` on every request (XC26) | today's TLS trust; not mitigated by the pin |
| **Destination co-writer during the run** (shared drive, USB, SMB, inherited ACL; XC1) | replace a written `.eml` (and, in revision 1, its spool line) so the sums vouch for forged bytes; swap the `.eml` before the HTML read-back; pre-create the next export names; plant a manifest or delete the marker to promote a killed run; swap the root for a symlink or `.app` before "Open folder" | no destination spool: hashes live in the local run record, and the sums and fingerprint are built from it; read-back verified against the recorded hash before parsing; random-suffixed root created through a re-checked handle, identity recorded after the mkdir (XC3, XC32); reconciliation downgrade-only, never reading destination content (XC4); "Open folder" re-checks identity and only reveals (XC33) | a file altered or added **after** it was written is detected by the recipient's check, the extra-files check and Check this export, not prevented (stated) |
| Recipient-side tamperer | alter files, re-hash everything, or **add** files | `SHA256SUMS.txt`; out-of-band fingerprint; README's extra-files check (XC14) | without the fingerprint, only accidental damage is detected (P9, conditional); added files are detected only by the extra-files check or Check this export, never by `sha256sum -c` alone (XC14) |
| Same-user local process | drive the dashboard | DC4: out of scope (it has the user's rights) | Type I, documented |
| Dashboard XSS (hypothetical) | turn the page against the user | CSP `script-src 'self'`, no inline handlers, `textContent` only (MA-407) | an XSS would reach no more than the existing capture/settings surface already allows |

### 8.5 Explicit non-claims

Not DLP: the same OS user can always copy their mail (DC4), and the workspace makes that
easier on purpose. An administrator can turn the workspace off for a deployment [ruling
Q6] (MA-526). No authenticity or signatures (P9). On Windows, the export's privacy depends on the
destination's ACL: the root inherits it and gets no protected DACL [ruling Q10 = J3]. **Conditional
(XC12):** the 0700/0600 modes hold only on filesystems with POSIX modes. On FAT, exFAT
and most SMB/NAS shares, the mount or share decides who can read the export, and the
preview's posture line says which applies (XC32). A cloud-synced destination uploads the
mail. It is warned, not refused, and detection is best-effort: OneDrive, Dropbox
(including team roots), Google Drive, iCloud Drive / `iCloudDrive`, macOS iCloud Desktop
& Documents, and Box (XC32).

## 9. Test plan (MA-400..MA-449 and MA-505..MA-531; allocated ids [ruling Q11 = J1])

The ids are the operator's allocation (`docs/owner-rulings-2026-10-09.md` §8). Each row
lands with its slice's catalog-first PR, and no covers marker cites a row before then. Each
row belongs to exactly one slice (XC28). A split row's first half keeps its number and its
second half takes an overflow id (MA-505..MA-513); both are listed in "Revision 2 row
changes" below, and each becomes its own catalog row. Tier U unless
marked. Each refusal row proves the positive twin first and asserts with
`assure.Refused` (typed result, message names the what/why/remedy, side effect absent).
The fake Graph server pattern is `internal/app/graph_test.go:31`. The prove-fail column
names the mutation that must turn the row red before revert.

| ID | Tier | Asserts | Covers | Prove-fail |
|---|---|---|---|---|
| MA-400 | U | `FolderTree` on a delegated client addresses only `/me/mailFolders` and `…/childFolders`, includes Deleted Items and Junk Email (labelled via the well-known ids), returns the raw `displayName`, `parentFolderId`, `childFolderCount` and `totalItemCount` per folder, pages `$top=100`, refuses above the folder cap, and issues only GETs | R17, R29, S52 | drop `totalItemCount` from `$select` → count assertion fails |
| MA-401 | U | `MessagePage` returns one page ordered `receivedDateTime desc` with subject, sender name and address, received, `hasAttachments` and the size hint (best-effort `$expand`, with the first-page fallback when the fake 400s it); a `nextLink` whose scheme, host, port or path prefix differs from the base is refused with no request made (the fake records zero hits on the foreign host) — in `MessagePage`, `FolderTree` and capture's `Messages`/`Folders` alike. Capture's half is MA-505, built in fault-isolation's FI-SGc [ruling J4] | R17, R29, S52, S20 | remove the pin → the foreign host receives a request carrying `Authorization` |
| MA-402 | U | `MIMETo` streams `/$value` byte-exact into the writer; aborts on idle beyond the configured timeout, refuses above the size cap and stops on ctx cancel, leaving no partial output; `$value` gets a header timeout ≥ the idle window (XC26); `StatusError` is SG's `{Code, GraphCode, Text}`, its bounded `Text`, and `Error()` keeping today's exact text, and it exposes 404/401/403/500 through `errors.As` [ruling J4]. Built in fault-isolation's SG slice | R17, R29 | restore `io.ReadAll` with a fixed deadline → the idle/cap cases fail; change `Error()`'s text → the text assertion fails |
| MA-403 | U | the workspace's Graph session is the device (`/me`) client even with `auth=app` in the config; no request ever addresses `/users/`; `delegatedScopes` equals exactly `Mail.Read` + `offline_access`; a whole M365 preview and export issues only GET | R17, R9, R29, S52 | build the session with `graph.New` (app) → a `/users/` request appears |
| MA-404 | U | the sign-in-only POST runs `NewDelegated` + `Me` in-process, surfaces the device code, persists the token and reports "Signed in as"; an untokened, cross-Origin or foreign-Host POST is 403 with no device flow started | R17, R19, S41 | skip the guard → the refusal twin starts a flow |
| MA-405 | U | every `/api/workspace/*`, `/api/exports/*` and `/api/m365/signin` route: GET → 405; a POST missing the token, with a foreign Origin or with a non-loopback Host → 403 with the side effect absent (no Graph hit, no file, no dialog, no journal line); JSON replies carry `nosniff` and `no-store`. **XC23:** the route-table walk is E0's MA-378, which probes every pattern registered through `localweb.Mux`; this row keeps only the method and guard twins | R19, R12, S41, S52 | skip `GuardLocalPOST` on one route → that route's twin fails |
| MA-406 | — | **Deleted (XC23).** E0's MA-372/MA-378 (local-confidentiality C2) cover the per-launch session and Host checks on every workspace route, because the routes register through `localweb.Mux` | — | — |
| MA-407 | S | structural: `workspace.js` and `exports.js` contain no `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `eval` or `Function(`, and the pages carry no inline `on*` handler; every element id each script binds exists in its page; a folder or subject containing `<img src=x onerror=…>` from the fake Graph comes back as a JSON-escaped string | R19, S52 | add one `innerHTML` assignment → the walk fails |
| MA-408 | U | every dashboard page (Overview, workspace, Export activity) renders the sidebar, and every entry resolves (same-page anchors exist; cross-page links hit a registered route whose page has the anchor), extending MA-302; the Microsoft 365 card states own-mailbox-only and that shared mailboxes aren't available; the workspace page has no mailbox-address input | R12, S41, S52 | link "Export activity" to a missing anchor → fails |
| MA-409 | U | selection resolution and counts: a subtree entry resolves to the folder plus every descendant in a fresh tree (including one created after the load); a non-subtree entry to the folder alone; exclusions removed; picks inside a selected subtree counted once; overlapping entries de-duplicated; `/count` equals Σ `totalItemCount` − exclusions + outside picks, with no Graph request made; the request carries no per-message list for whole folders | R29, S52 | count subtree picks twice → the count assertion fails |
| MA-410 | U | a pick or exclusion id that the session never listed is refused (R-j2) naming it; more than 10,000 picks plus exclusions → R-j; a body over 1 MiB → 413; positive twin with listed ids | R29, R12 | accept unseen ids → the refusal twin fails |
| MA-411 | U | source binding: a selection or preview made under signer A is refused after a sign-in as B (R-l, both named); an archive-source selection is refused after the archive location changes | R29, R12 | drop the UPN from the binding → the B-run proceeds |
| MA-412 | U | the preview writes nothing: tree hashes (names, sizes, bytes) of the destination, the archive and the config directory are identical before and after; the result carries the exact count, per-folder reported vs found, the size estimate with the unknown split, free bytes, the would-be directory name, 10 sample paths, the longest path and the over-259 count, and the cloud-sync warning when applicable | R29, S52 | `MkdirAll` the export root during preview → the destination hash differs |
| MA-413 | U | the run copies exactly the previewed ids: a message added to the fake folder after the preview is absent from the export; one deleted after the preview is a listed "no longer in the mailbox" failure; an expired (clock-injected) or superseded preview id → R-k | R29, S52 | re-resolve at start → the added message appears |
| MA-414 | U | destination refusals: missing → R-e; a file → R-e; relative → refused; equal to or inside the archive (including via a symlinked parent) → R-f; each with the side effect absent; positive twin; a cloud-synced path yields a warning naming the service, not a refusal | R29, R4, R12, S52 | drop the overlap check → an export lands inside `-out` |
| MA-415 | U | reconciliation: when the fake's `totalItemCount` changes between the before and after reads, the folder is re-listed; a persistent mismatch is a preview WARN naming the folder and both numbers; the run exports the listed set | R29, R1, S52 | skip the after-read → no WARN on the changing fake |
| MA-416 | U | each run creates a new export directory via exclusive mkdir; two runs in the same second get `…` and `…-2`; a planted pre-existing directory of the would-be name is never written into; an earlier export's bytes are unchanged after a later run (hash compare) | R29, R5, S52 | `MkdirAll` the root → the second run writes into the first |
| MA-417 | U | containment and names: a symlink planted inside the export root (via a fault hook mid-run) pointing outside is not written through (the message fails and nothing appears outside); hostile names (`..`, `/`, `\`, absolute, NUL, `CON`, `COM1.txt`, trailing dot or space, 300-byte CJK, RLO bidi, `COM¹`, `CONIN$`, `CON .txt`) produce distinct, creatable, in-root names — the last four sub-cases are xfail encoders citing R4 until F75/F91/F111 land in util | R4, R6, R29, S52 | write via `os.Create(filepath.Join(...))` → the symlink case writes outside |
| MA-418 | U | FoldKey de-duplication: sibling folders `Work`/`work` (archive source) and attachments `Report.pdf`/`report.PDF`/`report.pdf` in one message get distinct names, deterministic across two runs; NFC and NFD spellings of one name collide and are disambiguated | R4, R6, R29 | compare names case-sensitively → overwrite detected |
| MA-419 | U | M365 originals: each `.eml` equals the fake's `$value` bytes, its manifest SHA-256 and size equal the file's, and the in-run heap stays under a bound while a 50 MiB message streams | R29, R17, S52 | buffer then transform line endings → the byte compare fails |
| MA-420 | U | archive originals: a `-raw` record's `.eml` is copied byte-exact after the gate; a fixity mismatch, a symlinked `.eml`, one over the cap, and a missing file are each a listed failure, never emitted; a record without `.eml` exports its `.html` and zip byte-exact (hash = archive fixity) with status `exported-without-original` and the source-specific reason; nothing is synthesized (no `.eml` appears for it) | R20, R29, R4, S34, S52 | emit `.eml` re-serialized from the parsed message → the no-synthesis assertion fails |
| MA-421 | U | manifest format: the required fields (format and version, tool, run, source, selection, options, counts, `hash`, entries with files/roles/sha256/size, failures, `not_attempted`); every file except `SHA256SUMS.txt` and the marker is listed in `SHA256SUMS.txt`, sorted, coreutils format; an in-Go `sha256sum -c` mirror passes, and a flipped byte in any file makes it report that path | R29, S52 | omit README from the sums → the "every file listed" walk fails |
| MA-422 | U | recipient verification without the tool: `sha256sum -c` (or `shasum -a 256 -c`), run as an external binary on the CI host, passes on a fresh export and fails naming the file after a byte flip; README names all three verify forms and the fingerprint step; the journal fingerprint = sha256(`SHA256SUMS.txt`) | R29, S52 | write `SHA256SUMS.txt` with one space → the external check fails |
| MA-423 | U | readable HTML: the export `.html` comes from `RenderWith`, its head starts with the `ArchiveCSP` meta then no-referrer before any mail markup, and the MA-300/MA-301 hostile corpus re-parses with no live refresh or CSP meta; it links the sibling `.eml` (and the attachments directory when extracted) by relative name; role `derived-html`; an archived page carrying a live refresh is copied as `.archived-page.txt` with a note, its hash equal to the archive fixity | R19, R7, R29, S21, S52 | copy the archived page as `.html` unconditionally → the quarantine case fails |
| MA-424 | U | extract attachments: every parsed attachment is written under `<stem>.attachments/` with sanitized, FoldKey-unique names, bytes equal to the parsed part's bytes, role `derived-attachment`; a zero-byte or failed part is a note; archive zip entries are extracted within the bounds (entry count, per-entry and total uncompressed, ratio) with `../` names flattened, a bomb entry skipped with a note, and the zip itself copied byte-exact | R4, R1, R29, S52 | drop the ratio bound → the bomb fixture is expanded |
| MA-425 | U | a Latin-1 `text/csv` attachment extracts byte-identical to the MIME part's transfer-decoded bytes — **xfail encoder citing R1/R29 until the C1-1 parser fix lands**, then a permanent guard | R1, R29 | (the open gap is itself the red state; on the fix, remove xfail) |
| MA-426 | U | isolation: fake Inbox `[A1, A2, POISON(500 ErrorMimeContentConversionFailed), GONE(404), A3]` → A1/A2/A3 exported; POISON listed as `retryable` with the reason; GONE listed as "no longer in the mailbox", attempted once; outcome 3; a render panic on one message keeps its `.eml` with status `exported-original-only`; the archive is untouched | R29, R10, R1, S52 | return on the first fetch error (today's `graph.go:621` behaviour) → A3 is missing |
| MA-427 | U | run-fatal classes: a 401 whose forced refresh fails stops with R-c and outcome 1 (XC9; the time breaker is MA-516); ENOSPC (fault-injected) stops with R-g, temps removed, marker kept as STOPPED; each finalizes whatever it can (manifest status `failed`) | R29, R5, R12 | treat 401 as per-message → the run continues through every message |
| MA-428 | U | cancel: a guarded cancel stops after the in-flight message (its temp removed); the marker is rewritten CANCELLED with counts; the manifest has status `cancelled` and lists exported and `not_attempted` ids; the sums cover what was written; no partial file under a final name | R29, R5 | remove the marker on cancel → the CANCELLED assertion fails |
| MA-429 | U | crash ordering (**proves ordering, not power-loss durability**; XC5): a fault hook kills the run at each of W2…W10, including W9a (§5.1). At every point, any directory that contains a message file carries the marker or is complete; a W2–W4 kill leaves an empty or temp-only root, reconciled as interrupted; README's text at each point says complete only conditionally on the marker's absence; the activity label at each point is never "complete" while the marker is present (W9a → "Finished writing but not marked complete"); temps carry the `.mailexport-` prefix | R29, R5 | remove the marker before writing the sums → the W9 kill point fails |
| MA-430 | U | journal: 0600; compaction (temp + rename) keeps the last 200 runs; start-without-end with the marker shows interrupted; a missing directory shows "folder not found"; marker absent plus a **sealed run record** reconciles to "complete (recovered)", while marker absent plus a manifest status but no sealed record never does (XC4); a torn tail is truncated before the next append, and unknown-`v` lines survive compaction (XC3); control characters stripped on read; a deleted journal leaves exports intact; "Open folder" opens only the journal path (a mocked opener asserts argv), refusing a run id it does not know | R29, R12, S52 | open the request's path → the argv assertion fails |
| MA-431 | U | concurrency: an M365 export completes while a capture holds the archive lock (the export never acquires it; archive bytes unchanged); an archive-source export takes no lock (a capture started mid-export succeeds and writes its last-run record); a record re-exported by a concurrent full capture mid-export is "changed in the archive during export", never emitted mixed; a second start → R-m | R29, R5, R20, S25, S52 | take `lockfile.AcquireAs` in the archive adapter → the concurrent capture refuses |
| MA-432 | U | scale: a 60,000-message fake folder resolves and exports with the heap delta under the stated bound, per-message state written to the run record (no entries slice held; XC1), list endpoints never return more than 100 rows, progress counters advance and an ETA is present; a 200,001-message selection → R-i | R29, S52 | accumulate entries in memory → the heap bound fails |
| MA-433 | U | picker: browse calls the picker seam with the directory option and returns the path; cancel → `{canceled}`; an unavailable picker → R-n's remedy; a second concurrent browse is refused; an unguarded browse never invokes the seam; the mocked zenity binary on Linux gets a clean argv | R29, R12, S52 | drop the one-at-a-time mutex → two dialogs |
| MA-434 | U | Create folder: creates exactly one directory with a sanitized single-segment name under an existing destination; refuses an existing name, a separator, `..`, a reserved name, empty, and a parent inside the archive, with the side effect absent; positive twin | R4, R29, R12 | use `MkdirAll(parent/name)` with an unsanitized name → `a/b` creates two levels |
| MA-435 | U | archive source: the tree and counts come from the manifest (stores → folders by current `Folder`); the message list comes from `index.ListFolder`, which is closed after each request (a following `Rebuild` rename succeeds on the test's injected "handle held" check); gone records are counted separately and included by default; the preview reports with-original vs without-original; archive gaps are carried into notes; a newer-format manifest → R-p | R20, R29, S52 | hold the index open → the rename check fails |
| MA-436 | U | the archive is never written: for both sources, every archive file except `search.db-wal`/`-shm` is byte-identical after preview and run, `search.db`'s rows are unchanged, and no `.mailarchive.lock` is created when absent; an M365 export works with no archive location set at all | R9, R20, R29, S52 | call `index.Open` (read-write, runs `RepairKeys`) → a change is detected |
| MA-437 | U | retry: "Export failed messages again" on a partial run yields a preview of exactly its failed ids; "Export the remaining messages" on a cancelled run yields its `not_attempted` ids; the ids come from the run record, never the destination manifest (XC2); each goes to a new directory whose manifest names `retry_of`; the earlier export is byte-unchanged; a signer mismatch → R-l | R29, S52 | write retries into the old directory → the unchanged assertion fails |
| MA-438 | U | refusal wording: each R-a…R-v (§7.6) carries its what, why and remedy substrings; none contains a token, a Graph URL query, `panic` or `goroutine` (assure's forbid list) | R12, S52 | replace one message with `err.Error()` → the substrings are missing |
| MA-439 | U (unix) | export directories are 0700 and files 0600 under umask 022 and umask 0 | R29, R4 | create with 0o755 → the mode assertion fails |
| MA-440 | U | outcome mapping: complete → 0, partial and cancelled → 3, failed or refused → 1; the activity labels match; one requested-but-missing derived file makes a run partial | R29, R12 | map cancelled → 0 → fails |
| MA-441 | U | path budget: a long destination makes the slug shrink 60 → 24 → 0 before any path is reported over budget; the preview reports the longest path in UTF-16 units and the over-259 count; folder segments are never truncated beyond `SanitizeSegment` | R4, R29 | skip the slug shrink → the over-budget count is too high |
| MA-442 | L | Windows end to end: the real IFileOpenDialog picker; an export to a destination giving paths over 260 characters opens in Explorer with LongPathsEnabled on and is flagged when off; the README PowerShell line passes on Windows PowerShell 5.1 with a non-ASCII path; the destination's ACL is inherited — **pending** (Windows lab) | R29, S52 | — |
| MA-443 | L | real tenant: `totalItemCount` vs `/messages` agreement per folder type; immutable ids honoured; `$value` byte stability across two downloads (informs P8's caveat); PidTagMessageSize availability; measured messages/s for the ETA band — **pending** (live tenant) | R17, R29, S52 | — |
| MA-444 | U | the destination validation is repeated at start: a destination deleted, or turned into a symlink into the archive, between preview and start is refused (R-e/R-f) with the side effect absent | R29, R4, R12 | validate at preview only → start writes into the archive |
| MA-445 | U | settings: the remembered destination and options round-trip in `desktop-settings.json` (0600, version 1, `*bool` defaults ON/ON/OFF when absent); an older file without the fields loads with the defaults; an unknown field written by a newer build is tolerated | R12 | default `exportPreserve` to false when absent → fails |
| MA-446 | U | Export activity page: a running export shows counts, bytes, current folder, ETA and Cancel; finished runs show status label, counts, fingerprint, path and the actions their status allows; a panic in the run surfaces as "Stopped: an internal error" on the card, never a traceback (X11/DC3) | R12, S41, S52 | let the panic escape → the test process dies |

**Revision 2 row changes (existing rows amended by the review's conditions).** Each
change is part of its row's assertion; the prove-fail column is unchanged unless noted.

| Row | Change | Condition |
|---|---|---|
| MA-401 | adds: a 302 from the fake base to a foreign fake records zero hits there; the pin accepts host case and an explicit `:443` and refuses a path prefix that is not on a segment boundary; split: **MA-401** (E1: `MessagePage`/`FolderTree` pin and redirect) and **MA-505** (fault-isolation's FI-SGc, formerly E1c: capture's `Folders`/`Messages` pin; covers as MA-401) [ruling J4] | XC25, XC26, XC28 |
| MA-403 | split: **MA-403** (E1: the `/me` device client, scopes exactly `Mail.Read` + `offline_access`), **MA-506** (E3: a whole M365 preview and export issues only GET, never `/users/`) | XC28 |
| MA-404 | adds: the sign-in runs in its own slot; a concurrent capture device flow is refused by the sign-in mutex; the device code is served only by the guarded `signin-status` POST; with a weekly backup installed, a different UPN is not persisted until confirmed | XC8, XC7 |
| MA-409 | picks and exclusions are row handles | XC17 |
| MA-410 | realistic-length (≈150-character) Graph ids; 10,000 handles fit in 1 MiB; a 413 is an R-j-style refusal, not a bare status; an unseen folder id, `recoverableitemsdeletions` and `..` are refused with no Graph request | XC17 |
| MA-411 | split: **MA-411** (E3: signer binding), **MA-507** (E4: archive-location binding) | XC28 |
| MA-412 | the token entry is excluded from the config-directory hash and its refresh asserted separately; the posture line and filesystem-type warnings are in the result | XC30, XC12, XC32 |
| MA-415 | adds: a stable folder with reported > listed gets no re-list and no "changed" WARN, only the info line; arrivals during a newest-first listing produce no WARN after the union | XC16 |
| MA-416 | the root name carries a random suffix; the journal `start` names the directory actually created (a planted `MailExport-<ts>` is never recorded or reconciled as this run) | XC3, XC32 |
| MA-418 | adds: `ı`/`I` siblings; an 8.3 alias (`REPORT~1.PDF` beside `report-quarterly-final.pdf`) on the Windows CI leg; a planted file under a final name is never replaced (no-replace rename) | XC13 |
| MA-419 | runs with Readable HTML off (streamed bound) and on (one message held) | XC30 |
| MA-421 | adds: a planted, unlisted file is reported by the in-Go mirror's extra-files check | XC14 |
| MA-422 | split: **MA-422** (U, every OS: in-Go coreutils-format check incl. a planted file), **MA-508** (U, `//go:build !windows`: external `sha256sum`/`shasum`), the PowerShell line inside MA-442 (L) | XC28, XC14 |
| MA-423 | adds the positive twin: a page rendered by the current renderer is copied byte-exact as `.html` (its own leading metas are not "hostile"); adds: a `.eml` replaced on the destination before the read-back yields no derived file and the reason "the original changed on the destination after it was written" | XC30, XC1 |
| MA-426 | split: **MA-426** (E2: engine isolation, POISON/GONE), **MA-509** (E5: render panic → `exported-original-only`, labelled "exported without a readable copy") | XC28, XC35 |
| MA-427 | adds: table-driven Windows errno sub-cases (112, 39, 19, 1167, 82) on every CI OS; a fake filesystem that stays full after N writes → R-g, finalization fits the reserve, "Export the remaining messages" offered; a removed volume still offers it from the run record; 5 consecutive write failures → run-fatal; archive root gone mid-run → R-o2. Replaced: "10 consecutive transport errors → R-u" becomes MA-516's time breaker, and "a 401 mid-run → R-c" becomes "a 401 whose forced refresh fails → R-c" (MA-517) | XC10, XC11, XC9 |
| MA-430 | adds: "Open folder" refuses a swapped symlink, a swapped `.app` bundle and a different-inode directory with the opener not invoked, and opens with reveal argv; reconciliation is MA-448's | XC33, XC4 |
| MA-431 | split: **MA-431** (E3: M365 export beside a locked archive), **MA-510** (E4: lock-free archive export beside a capture) | XC28 |
| MA-432 | split: **MA-432** (E2: engine heap and the run-record bound, with the sync seam), **MA-511** (E3: list endpoints ≤ 100 rows, progress, ETA); adds retained session heap after a large browse and the byte cap | XC28, XC5, XC18 |
| MA-435 | adds: a v2-era fixture (manifest v2 + pre-meta index) lists via the migrated keys or shows the named message; a newer index version → R-p; a v1 fixture's `UnknownSentinel` never reaches notes; records whose files are gone are skipped and counted, their metadata absent from the export | XC19 |
| MA-436 | `search.db` compared by rows (opened `mode=ro`) | XC20 |
| MA-437 | adds: retry after a process restart; after Clear sign-in then re-sign-in as the same UPN; with the destination manifest deleted or tampered (what is retried is unchanged); Finish as partial on a killed run; parts named `-part2` with the chain in the manifest | XC2, XC15 |
| MA-438 | split: **MA-438** (E3: every Microsoft 365 and destination refusal, adding R-a2, R-d, R-g2, R-r, R-u2, R-w, R-x, R-y, R-z) and **MA-512** (E4: R-o, R-o2, R-p) | XC28 |
| MA-440 | split: **MA-440** (E2: complete/partial/cancelled/failed mapping), **MA-513** (E5: a requested-but-missing derived file makes a run partial) | XC28 |
| MA-442 | adds: a capture saving its manifest while the workspace reloads the snapshot (share-delete read; P15 on Windows); the README PowerShell line ("OK: N files checked"); the end-session window drains a run on logoff | XC20, XC14, XC6 |
| MA-443 | adds: real nextLink scheme/host/path shapes for `/me` folder and message paging (gates fault-isolation's FI-SGc, formerly E1c [ruling J4]; lifting that gate is ESC-7); `$value` time-to-first-byte vs size; throughput on a USB-stick destination including sync cost | XC25, XC26, XC5 |
| MA-445 | asserts the per-slice default: `exportHTML` OFF (and hidden) before E5, ON after; the settings mutex reloads under the lock and changes only export fields | XC28, XC34 |

**New rows (revision 2; allocated ids [ruling Q11 = J1]; revision 2 numbered them
MA-447..MA-466, of which MA-450..MA-466 are now MA-514..MA-530).**

| ID | Tier | Asserts | Covers | Prove-fail | Slice |
|---|---|---|---|---|---|
| MA-447 | U | run record: header + plan are fsynced before the marker; one line per finished message with every file's role, SHA-256 and size; `export-manifest.json` and `SHA256SUMS.txt` are built from it (no `.mailexport-entries.tmp` exists in the root at any point); the `sealed` fingerprint equals sha256 of the in-memory SUMS bytes; a `.eml` replaced on the destination after its run-record line still fails `sha256sum -c` against the SUMS built from the record; the presence check before SUMS makes a run partial when a recorded file vanished (fault hook) | R29, R5, S52 | build SUMS by re-reading files from the destination → the replaced-file case vouches for the forged bytes | E2 |
| MA-448 | U | reconciliation: a fault-injected blocking stat on an old run's `dest_dir` leaves `/api/exports/progress` answering within 1 s and Cancel working; each run is probed once per launch and gets a terminal `reconciled` record; a planted manifest with status complete plus a deleted marker never yields "complete" without a sealed record; no fingerprint is ever computed from disk; an identity mismatch shows "moved or replaced" | R29, R12, S52 | reconcile synchronously on the progress request → the 1 s bound fails | E3 |
| MA-449 | U | process-lifetime context: a session built inside a handler whose context is then cancelled, with a fake token expiring in 2 s, still refreshes for a later list and a 3-minute run | R17, R29 | build the session on `r.Context()` → the later list fails "context canceled" | E3 |
| MA-514 | U | shutdown: cancelling main's root context calls `Drain`, which cancels the run, waits for finalization (manifest status `cancelled`, marker CANCELLED) and returns within the 15 s budget | R29, R5 | return from main after `Shutdown` without draining → no manifest | E3 |
| MA-515 | U | identity-safe token write-back (in `internal/graph`, so capture too): a live source whose store was cleared does not re-create it on the next refresh; a live source for A does not overwrite a stored entry for B; `reload` refuses a stored token for another UPN; a deleted vault entry or token file is not re-created; MA-258's same-UPN rotation still persists | R17, R5, S20 | restore `putIfNewer`'s write-on-missing → the cleared store is re-created | E1 |
| MA-516 | U | transport backoff and the time breaker (clock-injected): a 30 s outage then recovery → the run completes with zero failures; a sustained outage → R-u once 5 minutes pass with no success, outcome 1; 25 consecutive 503s → R-u2, outcome 1 | R29, R10, S52 | count transport errors per request (revision 1's 10-in-a-row) → the 30 s outage fails the run | E2 |
| MA-517 | U | sign-in classification (engine): a fake token endpoint returning 400 `invalid_grant` after N messages → R-c, not R-u, with at most one message affected; a transient 401 whose forced refresh succeeds → the run continues; the in-flight message at a run-fatal stop is in `failures`, never first in `not_attempted` | R17, R29, S52 | treat `*oauth2.RetrieveError` as transport → R-u appears | E2 |
| MA-518 | U | free-space policy: start refuses with R-g2 when the estimate plus the reserve exceeds free bytes (side effect absent); the preview reports the reserve; the floor is also checked on the state-directory volume | R29, R12 | drop the reserve → start proceeds and finalization fails on the fake full disk | E3 |
| MA-519 | U | removable filesystems: a fault hook enforcing FAT's 65,536-entry directory cap is never reached by a 10,000-message folder (≤1,000 files per directory, month split); modes are set only at creation (a chmod-rejecting fake filesystem still accepts every write); EACCES at root creation → R-w with nothing written | R29, R4, R12 | put every message in one directory → the entry cap is hit | E2 |
| MA-520 | U | Check this export and Check and finish: a flipped byte, a missing file and a planted unlisted file are each reported (RED); a clean export is GREEN; Check and finish removes the app's own marker only when sha256(SUMS) equals the sealed fingerprint and every listed file matches, and refuses otherwise with the marker kept | R29, S52 | remove the marker without comparing the sealed fingerprint → a re-hashed SUMS is accepted | E3 |
| MA-521 | U | retry chain: a retry of a retry is named `-part3`, every part's manifest carries the full `chain`, README lists the siblings, and Export activity's set fingerprint equals SHA-256 over the parts' SUMS files in order | R29, S52 | omit an earlier part from `chain` → the set fingerprint differs | E3 |
| MA-522 | U | session lifecycle: "Load more" after the cursor TTL continues by keyset with no duplicate or skipped row on a static folder; an evicted cursor shows R-x; a POST after a dashboard restart shows R-y, never a bare 403; after 30 idle minutes (clock-injected) the tree, seen-set and cursors are released; resolution never adds to the seen-set and live-selection handles survive LRU pressure | R29, R12, S52 | let resolution feed the LRU → a pick is evicted and R-j2 fires | E3 |
| MA-523 | U | Settings save with a weekly backup installed and a changed archive location asks first, naming the signed-in UPN and the new folder; "Save and remove the weekly backup" removes it; "Cancel" leaves settings unchanged (side effect absent) | R12, S41 | save without the confirmation → the schedule silently retargets | E4 |
| MA-524 | U | settings serialization: an export start racing a `/api/settings` save never writes back a stale `Out` (both orders, injected interleaving) | R12, R5 | drop the mutex → the stale `Out` is written | E3 |
| MA-525 | U | archive renders stay byte-identical after `RenderContext.AttachDirName` is added (exporter contexts: KeepRaw on and off, with and without a zip); the "attachments are inside the .eml file" sentence appears only in export renders | R7, R19, R29 | print the sentence whenever `AttachDirName == ""` → an archive render changes | E5 |
| MA-526 | U | (Q6 ruled yes [ruling Q6]) with the deployment switch off, every workspace and export route returns 404 and the sidebar has no "Export" group | R12 | register the routes regardless → a 200 appears | E3 |
| MA-527 | U | F15: a stalled `/token` response body is released within `reqTimeout`; an unrelated workspace list waits no longer than that; the run stops with a typed error, never hangs | R17, R29 | remove `Client.Timeout` → the list blocks behind the mutex | E1 |
| MA-528 | U | redirects and bodies: a Graph 302 to a foreign host carries no `Authorization` there (zero hits); a Graph JSON body over the limit is refused, not decoded | R17, R29 | leave `CheckRedirect` unset → the foreign fake receives the token | E1 |
| MA-529 | S | structural: Browse is disabled while an export runs (the browse endpoint refuses with the picker seam not invoked); CI runs `govulncheck` for GOOS=windows on `./cmd/mailarchive-desktop` | R12, S52 | allow browse during a run → the seam is invoked | E3 |
| MA-530 | U | sign-in changes at the desktop surface: Clear sign-in during a run → the running export stops with R-z, finalized (manifest `failed`, marker STOPPED) and retryable from the run record; a sign-in as B during A's run → R-z naming both; an out-of-process change seen by the every-100-messages `SignedIn` poll → R-z; a refused refresh token in the preview and list endpoints shows R-b/R-c, never raw text | R17, R29, S41, S52 | drop the session-drop → run cancel link → A's run continues after the clear | E3 |
| MA-531 | U | [ruling W1] another archive folder, read-only: with a writable fixture folder and a read-only one, preview and run leave every file under the folder byte-identical and add no file (no `.mailarchive.lock`, no manifest save, no `search.db-wal`/`-shm`/journal, no temp), Settings and the installed weekly backup unchanged; a `search.db-wal` beside the index → the list says recent messages may be missing and whole-folder selection still exports them; a folder with no manifest → R-o3; a newer manifest or index → R-p; a destination inside the folder → R-f; each refusal with the side effect absent; positive twin first | R29, R9, R20, R12, S52 | open the index read-write (`index.Open`, which runs `RepairKeys`) → a byte change or an added `-wal` is detected | E4 |

MA-532..MA-534 are held in reserve (for RR-2 and RR-3). Revision 2's reserve MA-467..MA-469
is released, because it lies in g8's range [ruling Q11 = J1].

## 10. Build slices and order

Each slice adds its MA rows to the catalog first (OPERATOR-accepted, under the allocated
ids, XC29, [ruling Q11 = J1]), ships prove-fail → prove-pass, and leaves the covers map
green. Catalog-first PRs land one at a time, in the cross-design order of
`docs/owner-rulings-2026-10-09.md` §9. Producers merge before their sinks. **Each slice
lists only the rows it builds, and each row belongs to exactly one slice (XC28).** Every
"Proven (MA-x)" claim in this document is read as "MA-x (slice Ey)", taking the slice from
this table. **No E2 code starts** until lenses 3, 6 and 7 have re-run on revision 2 as an
addendum to the review (**XC40**, RR-2). Q12 (which engine) and Q10 (export-root ACL
posture) are ruled [ruling Q12, Q10 = J3]. **No E4 code starts** until RR-3 has re-run lenses
3, 6 and 7 on the W1 text [ruling W1].

| Slice | Contents | Depends on | Rows | Gate owed after build |
|---|---|---|---|---|
| **E0** (prerequisite; XC23) | **= `docs/design-local-confidentiality.md` slices C1** (Host allow-list on every method and route, `localweb.Mux` route registry; its MA-371/374/375) **and C2** (per-launch per-user session under `/s/<P>/`, relative UI URLs, instance file and handoff; its MA-372/373/376/378), both merged before E3. Not built here: revision 1's "otherwise built here as the first slice" is withdrawn | — | local-confidentiality's rows | local-confidentiality's own gates (adversarial pass; C2 friction review) |
| **E1** | Graph additions (§4.5): `FolderTree`, `FolderCount`, `MessagePage` (widened `$select`, id-aware extended properties, "honored" flag; XC27), the use of SG's `MIMETo` and nextLink guard, the host/path pin on the workspace paths (XC25), `CheckRedirect` + `io.LimitReader` (XC26), **the F15 fix** (`Client.Timeout`; XC26), identity-safe `putIfNewer`/`reload` (XC7) | **`docs/design-fault-isolation.md` slice SG** [ruling J4] (`StatusError{Code, GraphCode, Text}` with today's `Error()` text, `graph.Classify`, `MIMETo`, the nextLink guard; XC25), not S3 | MA-400, MA-401, MA-403, MA-515, MA-527, MA-528 (MA-402 is built in SG [ruling J4]) | short adversarial note, always: redirect refusal and token write-back touch capture (L2-3) [ruling J4] |
| ~~E1c~~ | **Moved [ruling J4]:** capture's nextLink pin is fault-isolation's step FI-SGc, still gated on MA-443's nextLink-shape sub-case (XC25); lifting that gate is ESC-7 | — | MA-505 (built in FI-SGc) | FI-SGc's short adversarial note |
| **E2** | Engine (`internal/app/copyexport*.go`): plan, **run record** (§4.19, XC1), `os.Root` writer with no-replace placement (XC13), naming with ≤1,000 files per directory (XC12) and the export-only stricter naming pass for F75/F91/F111 [ruling Q15], marker, README, manifest and sums built from the run record, outcomes, failure classes with backoff and time breaker (XC9), per-OS destination classes and write breaker (XC10), cancel, M365 adapter (originals only), `util.FreeBytes`/`FoldKey`; R29 and S52 land with SG's catalog-first PR (MA-402 cites them, [ruling J4]); the archive writer's F75/F91/F111 gap gets a skip encoder citing R4 [ruling Q15] | E1; **XC40 addendum filed (RR-2)**; Q10 and Q12 are ruled | MA-416..419, MA-421, MA-422, MA-508, MA-426, MA-427..429, MA-432, MA-439, MA-440, MA-441, MA-447, MA-516, MA-517, MA-519 | adversarial pass on the write side (combined with E3's) |
| **E3** — first shippable | Desktop: `workspace`, `exports`, endpoints through `localweb.Mux` with relative URLs (XC23), Graph session on the process-lifetime context with `Drain` (XC6), selection with row handles (XC17) and count, preview, start/cancel, progress/history split with the background reconciler (XC4), journal (XC3), the Microsoft 365 card and the sign-in slot (XC8), picker (disabled during a run, XC31), Create folder, settings (serialized, XC34), **retry, Finish as partial, Check this export and retry chains (revision 1's E7 folded in; XC2, XC14, XC15, XC28)**, the deployment switch [ruling Q6], and the Export sidebar group, which does not declare desktop P7 superseded [ruling ESC-5]. Source: Microsoft 365. Output: original EML + manifest. Readable HTML **hidden, `exportHTML` OFF** (XC28). X13 (scoped, Partial; XC24), X14 (scoped to the dashboard, [ruling J7]) and X15 land under their allocated ids | E2, **E0 merged** | MA-506, MA-404, MA-405, MA-407..410, MA-411, MA-412..415, MA-430, MA-431, MA-511, MA-433, MA-434, MA-437, MA-438, MA-444..446, MA-448, MA-449, MA-514, MA-518, MA-520..522, MA-524, MA-526, MA-529, MA-530 | **adversarial pass** (`docs/review-extraction-workspace-adversarial.md`: new loopback read surface returning mailbox content, a filesystem write primitive, a native dialog newly linked into the desktop binary, the run record) and **friction review** (`docs/review-extraction-workspace-friction.md`, every §7.2 cell, retry and Finish as partial included) |
| **E4** | Archive source: adapter (§4.6) with the redaction belt, nlink check, `UnknownSentinel` filter and archive-gone class (XC19, XC10); `index.ListFolder` read-only with the version check and Rebuild lock (XC19, XC20); tree from the share-delete snapshot (XC20) with per-store mailboxes (XC21); content disclosure; the Settings-save guard (XC22); **the second archive source, another archive folder, read-only (§4.3, R-o3) [ruling W1]** | E2, E3; Q3 is ruled yes (owner-direct) [ruling Q3]; **RR-3 before code** | MA-507, MA-420, MA-510, MA-435, MA-436, MA-512, MA-523, MA-531 | adversarial addendum (archive read gates, the read-only open of another archive folder, concurrency, Windows share mode) + friction re-run (the inheritor walk §7.4) |
| **E5** | Readable HTML (`RenderContext.AttachDirName` with the guarded sentence, `export.LiveRefreshIn` as defined in §4.11, read-back hash check, the quarantine path); `exportHTML` default flips to ON | E2 (E4 for quarantine) | MA-423, MA-509, MA-513, MA-525 | none new (renderer reused; R19 rows); noted in the E4 addendum |
| **E6** | Extract attachments, including archive zip extraction with bounds | E2, E5, **mime-fidelity S2, which carries the C1-1 fix** [ruling Q9] | MA-424, MA-425 | adversarial addendum (untrusted names → loose files; zip bounds) |

E1 → E2 → E3 is the smallest independently reviewable shippable unit (M365 → original
EML + manifest, with retry). E4–E6 each add one capability behind it. Revision 1's E7 is
folded into E3, so that E3's refusals never name a control that does not exist (XC28).

**Dependencies (XC38) [ruling Q15].** F75, F91 and F111 are handled for exports by an
export-only, stricter naming pass built in E2, so MA-417's sub-cases pass with no archive
migration. The shared util fix "renames directories and changes local keys, so it needs its
own migration" (`docs/design-fault-isolation.md` §3 N1), and it stays open outside this
design, as an open gap with a skip encoder citing R4 (E2). F17 conditions §4.14 and R-v; it
is fault-isolation's slice FI-GF17, after its S3. Revision 1's "can land at any time" stays
withdrawn.

## 11. Open questions needing an OWNER RULING — all RULED or ESCALATED

Every question below is answered in `docs/owner-rulings-2026-10-09.md` §2 (the joint
rulings it cites are in its §1). Each keeps its question for context, followed by its ruling.

- **Q1** — Do you accept the OS folder picker (Browse) plus the typed path in place of
  the mockup's in-page destination folder tree ("Click an arrow to expand folders…")?
  The tree needs a directory-listing API on the loopback dashboard (§4.8).
  **RULED (delegated-recommended): yes.** No directory-listing API on the loopback surface.
- **Q2** — Operator ruling T7 excludes Deleted Items and Junk Email from capture walks
  by default. Do you confirm that T7 does not apply to the workspace, so the tree lists
  Deleted Items (89) and Junk Email (1), unticked, as the mockup shows?
  **RULED (delegated-judgement): confirmed.** Both are listed, labelled and unticked, and
  are included only when ticked; T7 still governs capture.
- **Q3** — Should the archive source also accept "another archive folder" (read-only,
  chosen with the picker), so an inheritor need not repoint the archive location, which
  would also retarget the weekly backup (§7.4)? Ruling OW2 says "the local MailArchive
  archive". **Now blocking (review OR5):** E4's inheritor cell (§7.4) is gated on this
  ruling. XC22's Settings-save confirmation is the interim guard, and the Type II residual
  stays until Q3 is answered.
  **RULED (owner-direct, 2026-10-09: "yes the read only"): yes.** Built as W1 (§4.3, §4.6,
  §4.7, §7.4, R-o3, MA-531, E4), whose details are delegated; RR-3 re-runs lenses 3, 6 and
  7 on that text before E4 code. The Type II residual is closed.
- **Q4** — For the archive source, should messages that have since been deleted from the
  live mailbox, but are kept in the archive, be included in a folder selection by
  default? (Proposed: yes, counted separately in the preview.)
  **RULED (delegated-recommended): yes,** counted separately in the preview.
- **Q5** (re-premised, XC24; review OR7) — Do you confirm that the workspace must not
  ship before E0 (`docs/design-local-confidentiality.md` C1 + C2) lands, even for
  single-user installs? The premise is corrected: without E0, any local account can
  *already* copy the user's live mailbox through the shipped dashboard (POST
  `/api/settings` with any `Out`, then `/api/capture`). The workspace inherits that
  exposure and adds a selective copy and a subject-read surface. So E0 should ship on its
  own merits, whatever happens to the workspace.
  **RULED (delegated-judgement): confirmed.** E0 is scheduled first, on its own merits.
- **Q6** (review OR6) — Should an administrator (MSI or RMM property, or an HKLM policy
  value) be able to turn the Extraction workspace off for a deployment? **Recommended:
  yes,** because that switch is the rollout's only rollback path short of an
  uninstall/reinstall, which also removes E0 (XC37, §6). If yes, MA-526 applies.
  **RULED (delegated-recommended): yes.** MA-526 applies, in E3.
- **Q7** — Is sequential download acceptable for v1 (about 3–7 hours per 50,000
  messages), or must v1 download 2–4 messages in parallel (within Exchange's
  4-concurrent-per-mailbox limit)?
  **RULED (delegated-judgement): one at a time.** Parallel downloads wait for MA-443's
  measured throughput.
- **Q8** — For the archive source, do you accept the reworded panel note ("Exports the
  original EML where your archive kept one … otherwise the archived page and
  attachments"), since "Always exports original EML" is false for a default desktop
  archive?
  **RULED (delegated-judgement): yes.**
- **Q9** — Should "Extract attachments" wait for the C1-1 parser fix (as planned), or
  ship earlier with a warning that text attachments in non-UTF-8 encodings may be
  re-encoded?
  **RULED (delegated-judgement): it waits.** E6 ships after mime-fidelity S2.
- **Q10** (review OR1; XC23, XC32) — **Export-root ACL posture on Windows.**
  `docs/design-local-confidentiality.md` §6 expects workspace writes to get its protected
  owner/SYSTEM/Administrators DACL (its P1/P3, and its "owner-only at rest" R22). This
  design lets the export inherit the destination's ACL. **Recommended: inherit, and show
  the posture line** (§4.7). A hand-over copy must be readable by the recipient's account.
  On NTFS removable media, a protected owner-only DACL forces a standard-user recipient
  into an administrator ownership takeover that rewrites every file's ACL. Either answer
  amends local-confidentiality §6's workspace sentence. **Blocks:** E2's Windows write
  path.
  **RULED (delegated-recommended) = J3: inherit, and show the posture line.**
  Local-confidentiality's protected DACL is scoped to archive roots and the tool's own state
  directories; its §6 says so.
- **Q11** (review OR2; XC29) — **Id allocation.** Will the operator allocate the R, S, X
  and MA id ranges across the five in-flight designs (fault-isolation,
  recapture-integrity, mime-fidelity, local-confidentiality, extraction-workspace)? Revision
  2's provisional R22, S42 and X12 were claimed by several of them with different meanings.
  **Blocks:** catalog acceptance of any slice's rows.
  **RULED (delegated-judgement) = J1.** R29, S52, X13–X15; MA-400..449 plus MA-505..531;
  MA-406 retired; the reserve MA-467..469 released (header, §9). The duplicate-id check goes
  to the kit owner as a follow-up.
- **Q12** (review OR3; XC39) — **Which engine is the "simpler tool"?** Is the owner's
  "simpler tool … where you just select the source and choose the export destination to
  copy it to" this workspace's separate copy-export engine? Or is it a capture into a
  chosen folder through `app.Run` / `app.RunGraph`, as `docs/design-fault-isolation.md`
  records the same relayed decision? **Recommended: this workspace's copy engine.** The
  owner's words are "copy it to", and §1.2 shows `RunGraph` writes an archive (lock,
  manifest, `Exporter`, gone-sweeps), not a hand-over folder. Fault-isolation's input
  line would then be amended. **Blocks:** E2.
  **RULED (delegated-recommended): this workspace's copy engine.** Fault-isolation revision
  2 already reads it this way (its FC20), so the OW4 paragraph and XC39 cite that.
- **Q13** (review OR4; XC36) — **Desktop P7.** Does the workspace supersede the approved
  desktop design's P7 "Export a copy (extract mbox/eml)" (`docs/design-mailarchive-desktop.md`
  rev 2), which drops a dashboard mbox export? The CLI's `extract -format mbox` remains.
  **Blocks:** E3's sidebar and the §3 wording.
  **ESCALATED (ESC-5).** P7 carries out a direct owner ruling (the desktop feature-gap
  review), which a delegated ruling may not reopen. Recommended answer: yes, one export
  surface, with `extract -format mbox` as the CLI path. Interim: E3 ships the workspace and
  its Export sidebar group without declaring P7 superseded; P7 stays an approved, unbuilt
  desktop item, and §3, OW4 and XC39 do not call it superseded. It blocks nothing.
- **Q14** (review OR8; XC31) — **Out-of-process folder picker.** Should the native picker
  run in a child process (a self re-exec, `mailarchive-desktop -pick-folder`, killed after
  the 10-minute bound), so that a crashing or hanging shell extension cannot take down the
  dashboard and a running export? That amends the desktop owner ruling P1 "in-process
  (NOT shell-out)". XC31's minimum (Browse disabled during a run, §5.2 rows) applies
  either way.
  **RULED (delegated-judgement): no.** P1 stands; XC31's minimum is the design.
- **Q15** (review OR9; XC38) — **Unowned dependencies.** Who owns F17 (throttle waits
  against the 60 s deadline; 5xx not retried) and F75 / F91 / F111 (bidi controls, the
  255-byte segment limit, reserved names)? Alternatively, should this design take them as
  explicit slices (an export-only sanitizer variant avoids the archive migration)? Until
  answered, P7, §4.14 and §4.18 label them unowned. (P13's old F15 condition is gone, because F15 is fixed in E1, XC26.)
  **RULED (delegated-judgement).** F75, F91 and F111: an export-only, stricter naming pass
  layered over the util sanitizers, built in E2, so MA-417's sub-cases pass with no archive
  migration; the archive-side fix stays open outside these designs, with a skip encoder
  citing R4. F17: fault-isolation's slice FI-GF17, after its S3.
