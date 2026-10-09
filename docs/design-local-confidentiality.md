# Design — local confidentiality: an archive only its owner can read, local web surfaces only its owner can open, and redaction that really removes

**Revision:** 2 (2026-10-09). **BUILD STATUS:** approved with conditions, not built; owner rulings folded
(`docs/owner-rulings-2026-10-09.md`).
- **The gate.** The 10-lens pre-code review is filed as
  `docs/review-local-confidentiality-predesign.md`. Verdict: **GO_WITH_CONDITIONS**. Findings: 109 found, 101
  confirmed, 8 refuted. Seven blockers (six from the lenses, one hidden blocker from the rescue) were each
  dissolved by a named change; none survived.
- **This revision.** Revision 2 folds in the review's conditions **CC1–CC35**. Each change is marked in the
  text with its condition number, as `[CCn]`. Build may not start from revision 1.
- **Still open.** Owner and operator rulings OQ-1 (restated), OQ-6 (restated), OQ-7, OQ-8, OQ-9 and OQ-10 are
  not decided here (§11). The slice each one names is blocked until it is ruled (§10, "Blocked on"). OQ-2..OQ-5
  stay open as in revision 1. **[ruling]** All ten are now answered in `docs/owner-rulings-2026-10-09.md` §6
  (with J1 and J3 of its §1), and each change they make here is marked `[ruling <id>]`. One is escalated to
  the owner: OQ-8, with R13's classifier and its added deletion cases, as **ESC-4**. Until the owner answers,
  plain `reindex` keeps today's behaviour and slice D does not start. Slice A-W is not built (OQ-1).
- **Nothing in this document is built.** Every "proven" label below means "will be proven by the named MA row
  once built"; no row in MA-365..MA-384 exists in `docs/scenario-catalog.md` yet.
- **Ids are provisional [CC1].** R-CONF-1..3, S-CONF-1..4 and X-CONF-LOCKED are design-local names for the new
  invariants, scenarios and UX rule. Revision 1 called them R22–R24, S42–S45 and X12, which collide with ids four
  sibling designs propose. The OPERATOR allocates the real numbers (OQ-10, jointly with the extraction
  workspace's Q11 and fault-isolation's OR8) when the catalog-first PR is accepted, and every Serves and Covers
  cell is re-checked then. No covers marker may cite a provisional id. The new invariants, the X9 amendment and
  X-CONF-LOCKED are proposals for the OPERATOR to accept (catalog-first), never self-granted.
  **[ruling OQ-10 = J1]** The ids are allocated (owner-rulings log §8.3): R-CONF-1..3 are **R22–R24**,
  S-CONF-1..4 are **S42–S45** and X-CONF-LOCKED is **X12**, which this document now uses throughout. Their
  revision 1 numbers turn out to be the allocated ones, because the sibling designs' proposals moved instead
  (fault-isolation R25, mime-fidelity R26/R27, re-capture R28, workspace R29). The rows keep MA-365..384; MA-368's
  lab part is **MA-500**.
- **Slices.** Six build slices (A, A-W, C1, C2, B, D — §10). Every slice owes an adversarial pass; C2 and D also
  owe a friction review, and A owes one for its status/migration wording. CC27 adds a classifier and an intent
  rule to slice D, so D's conditions are re-verified by re-running lenses 3, 6 and 7 on §4.D before D's
  catalog-first PR (RR-5 in the owner-rulings log). **[ruling OQ-1]** A-W is not built, so five slices remain.

Revision 1 (2026-10-09) is the text the review examined.

**Revision 2 condition map** (review → where it lands):

| Condition | Lands in |
|---|---|
| CC1 provisional ids | header, §2 labels, §6.4, §9, §10, §11 OQ-10 |
| CC2 sibling designs | §3.9, §4.A3, §4.B1, §4.D2, §4.D5, §6.2, §6.3, §10 |
| CC3 hand-over outputs, the workspace bullet | P1, P3, P15, §4.A1, §4.A5, §5, §6.3, §6.4, §7.5, §9, §11 OQ-7 |
| CC4 honest labels | §2 labels, P3, P4, P7, §4.B1, §8.4, §8.5, §9 |
| CC5 macOS argv, link exposure | P7, P15, §4.C2, §4.C3, §8.4, §9, §10 |
| CC6 tool-owned scope | P2, P3, §4.A3, §4.A5, §4.A6, §4.D3, §6.1, §6.4, §7.7, §8.3, §9, §11 OQ-1 |
| CC7 Windows DACL correctness | P3, §4.A3, §4.A5, §4.A6, §5, §6.1, §6.4, §6.5, §7.4, §8.3, §9 |
| CC8 slice A-W | P3, §4.A5, §4.A6, §5, §6.1, §6.4, §6.5, §7.4, §7.6, §8.3, §8.4, §9, §10, §11 OQ-1 |
| CC9 posture reasons | P2, P4, §4.A3, §4.A5, §4.A6, §6.4, §7.5, §7.6, §9 |
| CC10 foreign-owned content | P4, §4.A2, §4.A3, §4.A5, §4.A6, §6.4, §7.6, §8.2, §8.4, §8.6, §9 |
| CC11 walk cost, migration wording, symlinked -out | P2, P4, §4.A3, §4.A6, §5, §6.1, §7.6, §9 |
| CC12 slice A without slice B | P4, §4.A1, §4.A3, §4.A6, §9, §10 |
| CC13 no-follow primitives, reparse points, P5 label | P5, P9, §3.6, §4.A1, §4.A2, §4.B1, §6.3, §8.4, §8.6, §9, §10 |
| CC14 store-level links | §4.B3, §6.1, §7.6, §10 |
| CC15 one route model | P6, §4.C1, §5, §6.3, §9, §10 |
| CC16 credential containment | P10, §4.A6, §4.C1, §6.1, §7.6, §9, §10 |
| CC17 relative URLs | §4.C2, §9, §10 |
| CC18 headless and remote serve | P6, §4.C1, §4.C3, §6.1, §7.2a, §7.6, §8.3, §9, §10 |
| CC19 link-warning precision | §4.C2, §4.C3, §7.6, §8.3, §8.4, §9, §10 |
| CC20 desktop refusals, opener fallback | §4.C3, §4.C4, §5, §6.1, §7.1, §7.6, §8.3, §9, §10 |
| CC21 instance lifecycle | P8, §4.C4, §5, §7.6, §8.3, §9, §10 |
| CC22 upgrade with running instances, packaging | P8, §4.C4, §6.1, §7.4, §7.6, §9, §10 |
| CC23 squatted 8097 | P8, P15, §4.C4, §6.1, §7.1, §7.6, §8.4, §8.6, §9, §10, §11 OQ-9 |
| CC24 session notices | §4.C2, §9, §10 |
| CC25 friction accounting | §6.1, §7.2, §7.5 |
| CC26 membership by record | §4.D2, §4.D5, §6.3, §7.6, §9, §10 |
| CC27 a prune is not a redaction | P13, §4.B2, §4.D2, §4.D3, §4.D4, §4.D5, §4.D6, exit codes, §6.4, §7.1, §7.6, §8.3, §9, §10, §11 OQ-8 |
| CC28 incomplete members | P13, §4.B3, §4.D2, §4.D4, §4.D5, §5, §6.4, §9 |
| CC29 nothing outside -out | P12, P14, §4.A3, §4.A6, §4.D4, §5, §6.4, §7.6, §8.2, §8.3, §8.4, §9 |
| CC30 in-place log rewrite | §4.D4, §5, §8.3, §8.6, §9 |
| CC31 text-file matching and coverage | P12, §4.D2, §4.D3, §4.D4, §8.2, §8.3, §8.4, §9, §10 |
| CC32 redaction claim scope | P11, P14, §4.D6, §6.4, §7.6, §8.4, §9 |
| CC33 scrub at O(deleted) | P11, §4.A2, §4.D1, §4.D5, exit codes, §5, §6.1, §6.2, §7.1, §9, §11 OQ-6 |
| CC34 legacy-scrub retry and space | §4.D1, §4.D5, §5, §6.1, §7.6, §9, §11 OQ-6 |
| CC35 superseded copies | §4.D2, §4.D5, §6.3, §10 |

Evidence base: the verified review findings in the fix-first plan (`g56-confidentiality`:
C2-1/N16, F3, F2, N9, F20, C2-2, C2-3/C1-3, C1-2) plus the related findings F4, F5, F8,
N10 and F16 from the same review; each was re-read against the code at the line cited
here on 2026-10-09. A scratch experiment against the vendored SQLite (modernc.org/sqlite
v1.38.2 = SQLite 3.50.3) on 2026-10-09 fixed the exact scrub sequence (§4.D1); its
results are quoted where they bear on a claim.

## 1. Problem

Four defects, one theme: the archive is a plaintext copy of someone's mail, and today
three different paths hand it to people other than its owner, and the documented way to
take a message out leaves it in.

### 1(a) The full text of every message is readable by every local account (C2-1, N16 — high)

- `search.db` holds subject, sender, recipients and the full body of every message
  (the FTS5 `body` column, `internal/index/index.go:37-59`). It is created by the SQLite
  driver at `index.Open` (`internal/index/index.go:75`, `sql.Open("sqlite", path)`) with
  SQLite's default `0644` masked by umask; nothing ever chmods it. `-wal`/`-shm` copy the
  database file's mode. The rebuild temp `search.db.rebuild` (`internal/app/reindex.go:279-281`)
  is created the same way and renamed over `search.db`.
- The run log (`internal/runlog/runlog.go:32`, `0o644`; also the GUI's
  `<out>/mailarchive.log` and every scheduled job's `<out>/<name>.log`) and the go-back
  history log (`internal/state/history.go:76`, `0o644`) are world-readable. The scheduled
  job's `<name>.stderr.log` is created by the scheduler, not by us (launchd
  `StandardErrorPath`, `internal/schedule/schedule.go:246`; the Windows wrapper's `2>>`,
  `internal/schedule/wrapper.go:61`), with the scheduler's umask.
- Every directory is `MkdirAll(…, 0o755)`: the archive root (`internal/app/run.go:108`,
  `internal/app/graph.go:84`, `internal/state/manifest.go:1393`,
  `cmd/mailarchive-gui/main.go:1316`), every store/folder (`internal/export/exporter.go:301`,
  `internal/pages/pages.go:125`), the Outlook PST staging dir
  (`internal/outlookcom/outlookcom_windows.go:77`), and every `extract -dest` directory
  (`internal/app/extract.go:146,403,442`). Folder and file names carry subject slugs.
- By contrast every file renamed into place from `os.CreateTemp` (pages, zips, `.eml`,
  manifest, reports, last-run, descriptor) is already `0600`, and the lock is `0600`
  (`internal/lockfile/lockfile.go:50`). So the pages are private and the index that
  contains all their text is not.
- Confirmed on a real export of `testdata/support.pst` under umask 022: every page `-rw-------`,
  `search.db -rw-r--r--`, every folder `drwxr-xr-x`; another account copied `search.db` and
  `select … substr(body,1,160) from docs_fts` printed message bodies. On macOS every local
  account's primary group is `staff` and the home folder is `drwxr-x---`, so a second account
  reaches `~/archive`.
- **Windows:** mode bits do nothing. The archive inherits its parent's ACL and the tool
  never sets one. An archive created at a drive root (`C:\MailArchive`) inherits `C:\`'s
  "Users: Read & execute" and "Authenticated Users: Modify (subfolders and files)" — every
  account on the PC can read it, and modify it (the precondition C2-2 needs).

### 1(b) The loopback web surfaces admit anyone who can send a request (F3 high, F2 medium, N9 low, F20 low)

- **F3 — a second Windows account lands in the first account's dashboard.** The shipped MSI is
  per-machine (`packaging/mailarchive-desktop/Product.wxs:34`, `InstallScope="perMachine"`) with an
  all-users Startup shortcut (`Product.wxs:57`, `:126-136`, `-no-browser`) and fixed ports
  (`cmd/mailarchive-desktop/main.go:37-38`, `127.0.0.1:8097` / `:8099`). Windows loopback
  TCP is shared by every logon session. The first account to sign in owns both ports; every
  later account's launch hits `isAddrInUse` and **opens the browser to whatever owns 8097**
  (`cmd/mailarchive-desktop/main.go:89-100`) — the first account's dashboard. The dashboard's
  GET `/` embeds the CSRF token for any requester (`internal/desktop/dashboard.go:84-90`,
  template `:287`), and `GuardLocalPOST` accepts a loopback Host, a missing Origin and that
  token (`internal/server/setup.go:143-145`). So account B (or `curl` from any account) reads A's
  whole archive on `:8099`, starts captures with A's sign-in, clears it, retargets "Archive
  location" to a folder B can read, and installs schedules as A. Account B cannot use the
  product at all. The reader has no authentication of any kind (`internal/desktop/reader.go:28-53`).
  The same holds for `mailarchive serve` on any multi-account machine and for
  `mailarchive setup` while it runs.
- **F4 (related) — a squatter gets the user's trust.** Anything already listening on 8097 (a
  hostile co-user, or the benign `mailarchive setup`, whose default is also 8097,
  `cmd/mailarchive/main.go:378`) is opened by the real binary as if it were the dashboard.
- **F2 — DNS rebinding reads the archive.** No surface validates `Host` except the POST guard:
  `secureHeaders` only sets headers (`internal/server/server.go:102-119`); `server.New` and the
  dashboard's GET routes answer any Host. A rebinding page that reaches 127.0.0.1 gets
  `/api/search` (every message), `/files/…` (every page) and the dashboard's identity data and
  CSRF token. Current Chrome/Edge/Firefox prompt (Local Network Access) before a public page
  reaches loopback, so the realistic path is Safari, an outdated or policy-relaxed browser, or a
  user who clicks Allow — hence medium.
- **N9 — the reader serves operator files.** `/files/` (`internal/server/server.go:78-79`) is a
  `containedDir` (`:142-165`) with no name filter: `search.db` in one GET, the manifest, last-run,
  history, schedule descriptor, run logs, the legacy TSV report.
- **F20 — credentials placed inside `-out` are served too.** `parseJob` (`cmd/mailarchive/job.go:293-310`)
  and `runGraph` (`cmd/mailarchive/main.go:1007-`) check a `-token-cache`/`-client-secret-file`'s
  presence and mode, never its containment, although `docs/graph-app-setup.md:172-173` promises
  the cache is "never written inside -out". A file there syncs with the archive and is served
  at `/files/<name>`, bypassing its `0600`.
- **N10 (related)** — `containedDir.Open` resolves symlinks then opens in a second step (TOCTOU).
  **F8 (related)** — `serve`'s default 8099 collides with the desktop reader's.

### 1(c) A symlinked folder inside the archive redirects writes and deletes outside it (C2-2 — medium)

`Exporter.Export` builds `OutDir/store/folders…` and calls `os.MkdirAll` then
`CreateTemp`+rename inside it (`internal/export/exporter.go:299-305`, `atomic.go:24-50`) with no
check for a symlink in any component; `pages.writeFolderPage` does the same for `index.html`
(`internal/pages/pages.go:124-159`); the prior-file prune on re-export follows links
(`exporter.go:478-483`); redaction's `deleteMessageFiles` gates paths only lexically
(`validRelPath`, `internal/app/verify.go:509-526`, string checks) then `os.Remove`s through the
joined path (`internal/app/reindex.go:131-146`), including `AlsoFiles` entries; the history log
and `search.db` opens follow links (`history.go:76`, `index.go:75`). PoC 1: `<out>/support/Drafts`
→ symlink to an outside dir; `-mode full` exited 0 and replaced the outside `index.html` and
wrote six message pages there. PoC 2: a symlinked folder plus one tampered `also_files` entry;
`reindex` exited 0 and deleted three outside files. This breaks R4 ("every path the exporter
writes stays inside the output root") and MA-232 ("a redaction can never delete outside -out").
Every read-side surface is already symlink-strict (verify `inspect`, `verify.go:390-414`; rebuild;
extract `destDirSafe`, `extract.go:348-373`; serve MA-82; lock and log MA-85/MA-93).

### 1(d) Redaction leaves the message in the archive (C2-3/C1-3 — medium, C1-2 — medium)

The README (`README.md:428-433`) calls "delete the exported files, then run `reindex`" "how you
take something out of the archive for good". It is not:

- **C2-3/C1-3 — the text stays in `search.db`.** `reindex` prunes with `idx.DeleteByID`
  (`internal/app/reindex.go:154` → `internal/index/index.go:349`), a plain `DELETE` on `docs` and
  `docs_fts`. The pragmas are only `journal_mode=WAL; synchronous=NORMAL`
  (`internal/index/index.go:32`): no `secure_delete`, no FTS5 merge, no `VACUUM` anywhere.
  SQLite leaves freed cell content in page free space; FTS5 keeps the deleted document's terms
  in its segment b-tree until a merge. After a real redaction `mailarchive search Dubai` returned
  0 matches while `grep -a -o 'ISS Dubai' search.db` printed the full sentence; a second PoC found
  the body ("ZEBRAQUOKKA"), its FTS token and the subject "Your HIV test results" in a 45 KB,
  WAL-checkpointed `search.db`.
- **C1-2 — the folder pages still list it.** `pages.Generate` writes pages only for directories
  that still have index rows and only `index.html`…`index-<pages>.html`
  (`internal/pages/pages.go:123-156`); nothing removes the page of a folder left with no rows, or
  an `index-N.html` above the new page count. After redacting both messages of `Personal/`, its
  `index.html` still listed "Your HIV test results" and "Divorce settlement draft", and `serve`
  serves it at `/files/<store>/Personal/`. Rebuild (`reindex.go:361`) has the same gap.
- Also found while verifying (this design, not in the plan): (1) a crash between the index
  prune and `manifest.Save` leaves manifest records whose files are gone and whose index rows
  are gone, which plain `reindex` (it walks index rows only, `reindex.go:108-124`) never finds
  again — their `Subject` (kept for records with issues, `exporter.go:405-410`) stays in the
  manifest; (2) the tool's own logs name redacted messages by file name (which embeds a subject
  slug: `exporter.go:361`, `atomic.go:130`, `reindex.go:143,302-341`); (3) the legacy report
  `attachments-report-legacy.tsv` (subjects, paths) is never regenerated (`internal/app/report.go:30-41`);
  (4) a crashed `-copy-first` run leaves `.mailarchive-snapshot-*.<ext>` — a full copy of the
  source store — that `SweepOrphans` never removes (it sweeps only `*.tmp`, `atomic.go:114`);
  (5) a failed `-outlook` run keeps `<out>/_outlook-pst/` (`cmd/mailarchive/main.go:185,239-243`),
  Outlook's PST copy of every account; (6) a message still present at the source is re-captured
  by the next run, because the manifest no longer knows it.

## 2. Properties

Labels: **proven** = a U/S MA row on the CI matrix (ubuntu, windows and macos; Go 1.26.x — `go.mod` pins no
toolchain) asserts it once built, and rows that assert POSIX modes are GOOS-gated **[CC4]**; **lab-pending** = a
tier-L row, validated by hand on the named platform; **conditional** = holds only under the condition stated
inline; **aspirational** = not claimed by this build. New invariants (provisional ids **[CC1]**; allocated **[ruling OQ-10 = J1]**): **R22** (owner-only at rest),
**R23** (local web surfaces admit only their launching account), **R24** (redaction removes what
the archive holds) — text in §6.4.

| # | Property | Serves | Label |
|---|---|---|---|
| P1 | **Owner-only creation (POSIX).** Every directory the tool creates under `-out`, under `extract -dest`, and for its own per-user state is created `0700`; every file it creates there is created `0600` — explicitly at the call site, independent of umask. This includes `search.db`, its `-wal`/`-shm`/`-journal`, `search.db.rebuild`, the history log, every run log and its `.1` rotation and `.stderr.log` sibling, pages, zips, `.eml`, manifest, reports, descriptor, lock, PST snapshots. Hand-over outputs (`extract -dest` roots; the extraction workspace's export directories set the same modes themselves) get these POSIX modes; their Windows posture is OQ-7 **[CC3]**. **[ruling OQ-7 = J3]** On Windows they inherit the destination's ACL, and the posture is named before the first write through `Posture(dir)`. | new R22; C2-1, N16 | proven by MA-365 (U, run under umask `000` and `022`) |
| P2 | **Existing archives become owner-only on the next run (POSIX).** A lock-holding run tightens any group/other bits it finds on the entries the tool owns — the fixed root-level set, the store directories the manifest names, the recorded message paths with their parent directories, and tool folder pages; never another entry under `-out`, never a nested archive **[CC6]** (the fixed set every run; the tool-owned tree once per policy version on every verb, gated by the `access_policy` stamp **[CC11]**); when `-out` is the user's home directory or a volume root, the root is not chmod-ed and the tree pass is skipped **[CC6]**; a file system that cannot hold modes is reported, not failed. | R22, R5 | proven by MA-366 (U); **conditional** on a file system that honours POSIX modes (not FAT/exFAT, not SMB/NFS mounts that ignore `chmod`, not a macOS volume mounted with "Ignore ownership" — detected by `statfs` **[CC9]**) — elsewhere P4 says "cannot be restricted on this drive" |
| P3 | **Windows: archive roots the tool creates are owner-only.** When the tool creates `-out` (or its own state directory) it sets a protected DACL: the parent's inheritable entries minus the broad principals (Everyone, Users, Authenticated Users, Interactive, Guests, Anonymous, Network, Domain Users/Guests), plus the running account's TokenUser SID, SYSTEM and Administrators with full control, inherited by everything below — written only after a check that TokenUser keeps full control **[CC7]**. Hand-over roots (`extract -dest`, workspace exports) are not archive roots: their Windows posture is OQ-7 **[CC3]** (**[ruling OQ-7 = J3]**: they inherit the destination's ACL, with the posture named). A `-out` that is a drive/volume root or the profile directory, or that holds entries the tool does not own, never has its root DACL changed; the tool-owned entries are protected one by one **[CC6]**. Existing archives are slice A-W (OQ-1): until it ships, an open existing archive is reported, and the opt-in `mailarchive access -restrict` (with `-restore`) removes the broad principals **[CC8]**. **[ruling OQ-1]** A-W is not built: an existing archive is never rewritten unasked; it is reported, `status` stays RED while it is open, and the opt-in `access -restrict` (with `-restore`) is the remedy. | R22; C2-1 (Windows), precondition of C2-2 | DACL shape proven by the U part of MA-368 on windows-latest **[CC4]**; a second account denied **lab-pending** (MA-500, MA-368's former L part **[ruling J1]**); **conditional** on NTFS/ReFS (FAT/exFAT have no ACLs → P4 reports it) |
| P4 | **Access posture is legible.** `status` (and the dashboard's Status & health view, X10) carries an `Access` line: GREEN owner-only; RED naming each archive file/folder other accounts can read, with the remedy; WARN "this drive cannot restrict access" when modes/ACLs cannot be held; WARN "will be made private on the next run" for an archive below the current policy **[CC11]**; plus `status -json` reason codes `access_open`, `access_unenforceable`, `access_not_owner`, `access_no_permission` **[CC9, CC10]** and, from slice B, `links_in_archive` **[CC12]**. Reasons are recomputed on every tightening pass and never latch **[CC9]**. | X6, R18, R22 | proven by MA-367 (U) for POSIX; Windows posture: U part of MA-368 on windows-latest, second-account part lab-pending **[CC4]** (MA-500, **[ruling J1]**) |
| P5 | **No write or delete follows a symbolic link at or below a store directory.** Export (directory creation, temp files, renames, the prior-file prune), Graph capture, folder-page writes and cleanup, redaction deletes (record `Path` and every `AlsoFiles` entry), the orphan sweep, and the opens of the history log, `search.db` and the run log refuse a link component — a symlink or, on Windows, a junction or other name-surrogate reparse point — or a non-directory component; a cloud placeholder (OneDrive Files-On-Demand) is not a link and is treated as the file or folder it represents **[CC13]**. `-out` itself may still be a symlink. | R4 (restored), MA-232 (strengthened); C2-2, N10 | proven by MA-369, MA-370 (U, including a junction fixture on windows-latest **[CC13]**). **Split label [CC13]:** operations through `os.Root` (directory creation, temp files, renames, removes, the history log) cannot escape `-out` even under a concurrent swap (Go's documented contract on Unix and Windows); the path-based opens — `search.db` and its `-wal`/`-shm` (SQLite opens by path) and the run log — are check-then-use, so someone who can write the archive root can redirect them anywhere until P1/P2 make the root private |
| P6 | **Every local web surface validates `Host` on every request and method** — `serve`, the desktop reader, the dashboard, `setup`: the Host must be one of `127.0.0.1:<port>`, `localhost:<port>`, `[::1]:<port>` with any port, so a forwarded port (`ssh -L 9000:127.0.0.1:8099`) works — the rebinding defence rests on the hostname **[CC18]** (plus, only for a non-loopback `serve`, this machine's interface IP literals and `-allow-host` names); anything else, including an empty Host, is refused with no archive data. | R19 (amended), X9 (amended); F2 | proven by MA-371 (U) and the structural MA-378 (S), both in slice C1 **[CC15]** |
| P7 | **Every local web surface admits only the browser its launching account opened.** Each process mints a per-launch session; the only way in is a one-time launch link (`/_open?t=<token>`) that the process itself opens (or prints); redeeming it sets an `HttpOnly; SameSite=Strict` cookie scoped to a per-launch secret path prefix `/s/<P>/` under which every page and API lives; every other request — any route, any method, GET included — gets a legible locked page (X12) or a JSON 401, and never the CSRF token. | new R23, X9 (amended), new X12; F3, F2 (belt), N9 (belt) | server half (refusals, single-use tokens, cookie attributes) proven by MA-372, MA-373 (U) and MA-378 (S); browser half (real browsers carrying the `Strict`, path-scoped cookie across the meta-refresh hop and to the reader's port) **lab-pending** MA-377 — Chrome, Edge, Firefox, Safari **[CC4]**; **conditional:** the boundary is *other OS accounts and web pages* — not a process running as the same account (DC4 stands), not an administrator/root; loopback traffic is private only while no packet-capture driver grants non-admin capture (Npcap installed without admin-only mode, ChmodBPF, a `wireshark` group) **[CC4]**; on Linux (`/proc/*/cmdline`) and on macOS (setuid `/bin/ps` shows every account's argv; no hidepid equivalent) a co-account that polls process listings can race the browser for a link passed in argv (§8.4) — macOS's opener passes the URL on stdin instead (lab-checked), and opener links live about 60 s **[CC5]**; the race is visible to the user, not silent |
| P8 | **MailArchive Desktop is per-account.** A launch reaches only *this account's* running instance, verified by a challenge–response against a `0600` per-account instance file kept in a non-purgeable per-account directory **[CC21]**; otherwise it starts its own instance (dashboard on 8097 if free, else any free port — never silently: the dashboard and `status` then say another program holds 8097 **[CC23]**; reader always on a free port). **[ruling OQ-9]** The dashboard always takes a free port, as the reader does; "8097 if free" goes. When an unverified listener holds 127.0.0.1:8097, the dashboard header and `status` say "another program is using MailArchive's former address 127.0.0.1:8097 — open MailArchive only from its shortcut" (CC23's notice, adapted). A listener that fails the handshake — another account's instance, `mailarchive setup`, a squatter, a pre-upgrade instance **[CC22]** — is never opened in the browser. A launch with a different `-config`/`-out`, or from another logon session, gets its own instance or a visible refusal, never a silent handoff **[CC21]**. | R23; F3 (lock-out half), F4, F8 (reader half) | proven by MA-376 (U); multi-session and upgrade-while-sessions-run Windows behaviour **lab-pending** MA-377 |
| P9 | **The reader serves exported content only.** `/files/` serves `index.html` and `README.txt` at the archive root and, below it, only `*.html`, `*.eml` and `*-attachments.zip` (and a directory only through its `index.html`); dotfiles, `search.db*`, `*.log`, `*.tsv`, `*.json`, `*.token`, `*.secret`, `*.pst`, `_outlook-pst/`, the lock and anything else are 404 even to a valid session; a link at any component (a symlink, or on Windows a junction) is 404, while a cloud placeholder is served as the file it represents **[CC13]**; the open is race-free (`os.Root`). | R19 (amended); N9, F20 (belt), N10 | proven by MA-374 (U) |
| P10 | **Credentials never live inside the archive.** `graph` refuses (and, with `-unattended`, warns and `status` WARNs) a `-token-cache` or `-client-secret-file` whose resolved path (deepest existing ancestor, with extract's `resolveExisting` moved to `util` **[CC16]**) is inside `-out`; `schedule` refuses it at install; the desktop's Settings POST refuses an archive location that is, contains or is contained in its own config directory, while its scheduled `HeadlessCapture` warns and `status` WARNs `config_in_archive`, so an installed backup keeps running **[CC16]**. When `-out` contains the OS config directory (`-out` = home), that directory counts as outside the archive for this check, and the reader and the tightening walk never touch it **[CC16]**. | graph-delegated P5 (enforced), R12; F20 | proven by MA-375 (U) |
| P11 | **Redaction removes the message's text from `search.db`, logically and inside the file.** After `reindex` redacts a message, neither `search.db` nor its `-wal`/`-shm` contains, as raw bytes, the values unique to it — its subject, body, the sender and recipient values no kept message shares, Message-ID, attachment names and FTS tokens **[CC32]**. A policy-1 index does this at the cost of the deleted rows (`secure_delete` plus FTS5 `secure-delete`); only the one-time legacy scrub rewrites the whole file **[CC33]**. | new R24, R13 (amended); C2-3, C1-3 | proven by MA-379 (U, raw-byte search, with the residue-present positive twin and a twin for the secure-delete path **[CC33]**); "inside the file" only — blocks the file system released are §2 P14's non-coverage; bytes shared with messages you kept (a quoted body, "Re: <subject>") stay in those messages' records (P14) **[CC32]** |
| P12 | **Redaction removes every derived listing the tool keeps.** Folder pages and the root page no longer list it; a folder left empty loses its pages and, if then empty, its directory; a folder that shrank loses its trailing `index-N.html`; the manifest, history log, both reports, the last-run error text and its `BACKUP-NEEDS-ATTENTION.txt`/`ARCHIVE-INTEGRITY-ATTENTION.txt` sidecars **[CC31]**, and lines in the tool's own logs inside `-out` that name it (by stem, bracketed Message-ID or exact subject **[CC31]**) are removed; stale PST snapshots are swept. A log outside `-out` (one an operator pointed the schedule at) is never written; the summary names it as not covered **[CC29]**. | R24, R13 (amended), R21; C1-2, N9 edge | proven by MA-381, MA-382 (U, whole-tree byte search) |
| P13 | **Redaction is crash-safe and fails toward removal, legibly — and only an operator's deletion is a redaction.** Every step is ordered so a crash leaves at most work a re-run finishes (§5); a durable `scrub_pending` marker covers the stem-free tail; a member whose file deletes, page items or log lines did not all complete keeps its manifest record, marked `redaction:"incomplete"`, so the next `reindex` finds and finishes it **[CC28]**; any step that could not complete makes `reindex` exit `1` with an itemized "still present" list; `status` WARNs while a redaction is unfinished. A record is a redaction member only when its own `.html` is absent under a real parent folder; every other reason a repair path prunes (a moved or renamed folder, a link, an unreadable or oversize file, an index error) keeps the record and erases nothing, and a gone set beyond the intent threshold waits for explicit intent (OQ-8) **[CC27]**. **[ruling ESC-4]** The classifier, the intent rule and R13's added deletion cases are escalated to the owner; until the owner answers, plain `reindex` keeps today's behaviour and slice D does not start. | R5, R13, R18, X1, X6 | proven by MA-380 (U, fault injection: kills and returned errors **[CC28]**; the classifier cases **[CC27]**) |
| P14 | **"Removed for good" is stated honestly.** The README, `reindex`'s summary and the archive's `README.txt` say exactly what is and is not covered (§7.6): covered — the message's own records and everything the tool derived from it in the archive folder; not covered — the source mailbox (the next capture re-archives a message still there), other messages you kept that quote, forward, reply to or duplicate it **[CC32]**, backups and copies, file-system/SSD remanence and snapshots, OS indexes and caches, the browser, logs the tool cannot see or that lie outside the archive folder **[CC29]**. | R24; README `:428-433` correction | proven by MA-383 (U, text assertions) |
| P15 | **The security copy is true.** The dashboard's Security & privacy panel, `serve`'s startup text and the README state the new boundary (other accounts on this computer and web pages cannot open your dashboard or archive; a program running as you can) — OS-aware as DC5 requires, with the residuals: on Linux and macOS another account can race a just-opened link (P7) **[CC5]**; MailArchive shows a Microsoft sign-in code only in a window it opened from its shortcut, on every sign-in surface, the extraction workspace's included **[CC3, CC23]**. | DC4, DC5, X11 | proven by MA-384 (U) |

**Aspirational (not claimed by this build):** protection against same-account malware, administrators,
or forensic disk analysis; erasure of freed disk blocks; a redaction that survives re-capture of a message
still in the source (OQ-2; **[ruling OQ-2]**: not built, the warning only); zero-ceremony admission without a
launch link (OQ-5; **[ruling OQ-5]**: reconsidered after C2 ships and its friction review is done).

## 3. Non-goals (explicit scope cuts)

1. **Encryption at rest.** The archive stays plaintext files; the answer to a stolen disk is full-disk
   or volume encryption, which README and P14 recommend.
2. **Same-account processes, administrators/root, backup operators with `SeBackupPrivilege`, physical
   access.** DC4 stands: a program running as you can read your archive directly. Administrators keep
   access on Windows by design (P3 keeps SYSTEM/Administrators so backup agents running as SYSTEM keep
   working).
3. **Secure erasure of disk blocks.** The tool does not overwrite blocks that deleting, renaming over, or
   truncating a file released; it cannot reach SSD wear-levelled copies, APFS/btrfs/ZFS copy-on-write
   blocks, snapshots, Windows Volume Shadow Copies, swap or hibernation files. P14 says so.
4. **Shared-read archives.** No opt-out from owner-only in this revision (OQ-3; **[ruling OQ-3]**: none is
   built). An archive another
   account must read (e.g. a family NAS share read by a second login) is not supported; today it does not
   work either for pages, zips or `.eml` (already `0600`).
5. **Inspecting macOS extended ACLs or Linux default ACLs.** On Linux, `0600`/`0700` mask any named-user/group
   POSIX ACL entry to no access (the group-class bits are the ACL mask); a macOS ACL inherited from a parent
   folder is not masked by mode bits and is not inspected (acknowledged limit, §8.6).
6. **Cloud-synced folders** (OneDrive, iCloud Drive, Dropbox) are copies outside the boundary; their web
   access and version history are not this tool's to control. The tool still works inside such a folder: a
   cloud placeholder is not a link (P5, P9) **[CC13]**.
7. **Sticky redaction across re-capture** (OQ-2, ruled no), **peer-credential admission** (OQ-5, ruled later
   **[ruling OQ-2, OQ-5]**), **TLS for a
   non-loopback `serve`** (the session cookie of a LAN `serve` travels in clear; the existing warning stays),
   **session idle expiry** (a session lasts while its process runs), **hard-link refusal in the reader**
   (it would break archives restored from hard-link backup trees), **denial of service by a co-account**
   (cookie floods, port exhaustion).
8. **Not this design's fixes, but dependencies (§6.3):** F5 (the desktop reader keeps an old `search.db`
   open after a rename), F16 (Windows ACLs on the secret/token files), F1 (already fixed: `go.mod` is on
   `golang.org/x/net v0.60.0`, MA-300/301).
9. **No change to the archive layout, file naming, index key format, or exit-code set,** and no manifest version bump of
   this design's own: its manifest fields (`access_policy`, `redaction`) are additive JSON, valid whether the manifest is
   v6 or recapture-integrity's v7 **[CC2]**. **[ruling J2]** No design bumps the manifest: it stays version 6.

## 4. Mechanism

### 4.A Owner-only at rest (P1–P4; slice A)

**A1 — one creation primitive.** New `internal/util/private.go`: constants `PrivateDir = 0o700`,
`PrivateFile = 0o600`; `MkdirAllPrivate(path)`; `TightenPath(path) (changed bool, err error)`, which opens the
entry `O_NOFOLLOW` (`O_DIRECTORY` for a directory, `O_NONBLOCK` so a FIFO cannot block) and `fchmod`s the
descriptor, or uses `fchmodat(AT_SYMLINK_NOFOLLOW)` where the platform supports it — never a chmod by path, so a
link swapped in after the check is never followed; a link is reported to P5's walk **[CC13]**; and on Windows
(`private_windows.go`, `golang.org/x/sys/windows`, already a dependency at v0.48.0) `RestrictNewRoot(dir)` and
`Posture(dir) (Open|Private|Unenforceable, offenders)`. `Posture` is exported for hand-over destinations too:
`extract` and the extraction workspace's preview posture line use it to name a destination other accounts can read
before the first write **[CC3]**. Every creation site in §1(a) switches to it:
`run.go:108`, `graph.go:84`, `manifest.go:1393`, `exporter.go:301` and `pages.go:125` (`MkdirAllPrivate` in slice A;
they move to P5's `ArchiveFS` in slice B **[CC12]**), `extract.go:146,403,442`, `outlookcom_windows.go:77`,
`cmd/mailarchive-gui/main.go:1316`; `runlog.go:32` and `history.go:76` open with `0o600`.
Outside the archive, files that are not archive content keep their modes (the launchd plist
`schedule.go:637` stays `0644`; wrapper scripts and Task XML are unchanged).

**A2 — `search.db` is born `0600`.** `index.Open` (`index.go:74`): if the path is absent, create it with
`os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY|noFollow, 0o600)` — `noFollow` is the repo's per-OS constant
(`O_NOFOLLOW` on Unix, `0` on Windows as in `lockfile/lock_windows.go`, where B1's reparse-point `Lstat` is the link
check) **[CC13]** — and close it (an empty file is a valid empty SQLite database), then `sql.Open`. A database created
this way is stamped `scrub(pending=0, policy=1)` at schema creation and has FTS5 `secure-delete` on (D1) **[CC33]**.
If present, `Lstat` it and any `-wal`/`-shm`/`-journal` sibling: each must be a regular file — a cloud placeholder
counts as one (B1) **[CC13]**; a loose mode is tightened (A3); once the root is private, a sibling another account owns
is replaced by an owner-owned copy before the open (A3, "foreign-owned") **[CC10]**. SQLite creates `-wal`/`-shm` with the
database file's mode (observed on 2026-10-09: pre-created `0600` → `-wal`/`-shm` `0600`), so the
siblings follow. The same path creates `search.db.rebuild` (Rebuild opens it through `index.Open`). A
process-wide `umask(077)` was considered and rejected: it is inherited by every child (the browser
`xdg-open` starts, `crontab`, `schtasks`), changing the user's own files.

**A3 — migration on the next run (POSIX).** `TightenArchive(out, scope)` runs right after the archive
lock is acquired in every lock-holding run (export, `graph`, desktop capture, `reindex`, `reindex -rebuild`,
`verify -record`, and `extract`, which also holds the lock, `extract.go:121-124` **[CC11]**). A symlinked `-out` is
resolved once (`filepath.EvalSymlinks`), and the resolved root is used for both passes, so the walk reaches the tree
and the root itself is tightened (`WalkDir` does not descend into a symlinked root) **[CC11]**.

**What it touches — only what the tool owns [CC6]:** the fixed set below; the store directories named by manifest
records (a "store directory" is manifest-derived only, never inferred from a directory listing); each record's
recorded `Path`/`AlsoFiles` files and their parent directories; and tool folder pages (D3's identification). It never
changes another entry under `-out`, and it never descends into a directory that holds its own
`.mailarchive-manifest.json` or `search.db` (a nested archive). The `SweepOrphans` walk it rides on is restricted to
the same manifest-named store directories, which also keeps the sweep's existing orphan-zip removal out of folders
that are not the archive's.
- *fixed set, every run:* the `-out` root directory, every manifest-named store directory, `search.db*`,
  `.mailarchive-*` dotfiles at the root, `README.txt`, root `index.html`, both report TSVs, the attention
  notices, and every root-level `*.log`/`*.log.1`/`*.stderr.log`. A log the schedule descriptor names outside
  `-out` is never touched: the descriptor is untrusted input (§8.2) **[CC29]**;
- *tool-owned tree, once per policy version, every verb [CC11]:* when the manifest's new optional
  `access_policy` field is below `1`, the run walks the tool-owned set — export, `reindex` and Rebuild reuse the
  existing `SweepOrphans` walk (`atomic.go:106-135`, `WalkDir`, which does not follow links), `graph`/desktop capture
  and `extract` walk it separately — tightens each entry, collects every link it meets (counted for P4; reported as
  `links_in_archive` once slice B ships **[CC12]**), and then stamps `access_policy: 1`. The stamp is written by a
  small manifest save that does not depend on the run succeeding, and it records the root's identity (device and
  inode); a copied or restored archive whose root identity differs walks again **[CC7, CC11]**. The field is
  additive JSON (`manifest.go:520`, `json.Unmarshal` ignores unknown fields) with no version bump of its own, valid
  for manifest v6 and for recapture-integrity's v7 **[CC2]** (**[ruling J2]**: there is no v7; the manifest stays
  version 6); an older binary that re-saves the manifest drops it,
  which only causes one extra walk.
- *never:* when `-out` is the user's home or profile directory (`os.UserHomeDir`) or a volume/drive root, the root is
  not chmod-ed and the tree pass is skipped; only the fixed files and the manifest-named store directories are
  tightened, which already blocks traversal into the archive. `status` names the location (P4). The same list applies
  on Windows (A5) **[CC6]**.
- *cannot — a reason per offender [CC9]:* a `chmod` that "succeeds" without changing the mode (FAT/exFAT, SMB/NFS
  ignoring modes — detected by re-`Lstat`), a darwin volume whose `statfs` flags carry `MNT_IGNORE_OWNERSHIP` or
  `MNT_UNKNOWNPERMISSIONS` (the default for external drives, which reads back `0600` while every account is treated as
  the owner), or a Linux file-system type (`statfs` `f_type`) of vfat, exfat, fuseblk, cifs/smb2 or nfs is
  `access_unenforceable` (the Linux types read "unverified" even when the mode reads back); a `chmod` that fails with
  EPERM because another account owns the file is `access_not_owner`, naming the file and its owner. Each is logged
  once and recorded with a timestamp in the last-run record — not latched in the manifest: every tightening pass
  recomputes them, so moving the archive to a capable drive or fixing the owner clears the WARN. The run continues
  (capture matters more than posture, and the posture is reported, P4).
- *foreign-owned [CC10]:* once the root is owner-owned and `0700`, a content file in the tightening set that another
  uid owns (`search.db*`, the history log, a log, the manifest, a page) is replaced before the run writes to it: an
  owner-owned `0600` copy is written through `ArchiveFS` and renamed over it (for `search.db*`, before `index.Open`),
  which breaks any hard link and any path the co-account holds open. A foreign-owned `-out` root makes the run refuse
  to write: exit `1`, "the archive folder <out> belongs to <owner>; run `sudo chown -R <you> <out>`".
- One log line on change: `made the archive private: tightened N file(s)/folder(s) other accounts could read`.

**A4 — scheduler-created logs.** `runlog.Open` (`runlog.go:19`) also creates/tightens the `.stderr.log`
sibling at `0600` at every run start (launchd and `cmd.exe` append to an existing file without changing its
mode); `schedule --install` pre-creates both logs `0600`; generated launchd plists gain
`<key>Umask</key><integer>63</integer>` (= `077`) so the job's own process and launchd's stderr file start
private (existing plists get it on the next `--install`; until then `runlog` tightens each run).

**A5 — Windows (P3).** Every DACL the tool writes names its SIDs explicitly: full control for the running
process's **TokenUser** SID — never "the owner", CREATOR OWNER or OWNER RIGHTS (a folder created from an elevated
prompt is owned by BUILTIN\Administrators, which would lock the user's normal token out) — plus SYSTEM and
BUILTIN\Administrators **[CC7]**. Before writing any DACL the tool computes TokenUser's effective rights under it
(`GetEffectiveRightsFromAcl`, a lazy advapi32 call, pure Go); unless full control remains, nothing is written and
`status` WARNs `access_no_permission` with the remedy **[CC7]**.
- **New roots.** `RestrictNewRoot(dir)` runs only when the tool itself created `dir` (the `MkdirAll` returned
  "created") and `dir` is an archive root (`-out`) or the tool's own state directory — never an `extract -dest` root or
  a workspace export directory, whose Windows posture is OQ-7 (revision 2 describes inheriting the destination's ACL,
  with the `Posture` line before the first write) **[CC3]**. **[ruling OQ-7 = J3]** That description is the ruling:
  a hand-over root inherits the destination's ACL, and `Posture(dir)` names it before the first write. With `SetNamedSecurityInfo(dir, SE_FILE_OBJECT,
  DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION, …)` it writes a protected DACL equal to the parent's
  inheritable ACEs minus the broad SIDs (S-1-1-0 Everyone, S-1-5-11 Authenticated Users, S-1-5-32-545 Users, S-1-5-4
  Interactive, S-1-5-32-546 Guests, S-1-5-7 Anonymous, S-1-5-2 Network, the domain's -513/-514), made explicit, plus the
  three full-control entries, all `OBJECT_INHERIT_ACE|CONTAINER_INHERIT_ACE` **[CC7]**. A backup account granted at a
  parent such as `D:\Data` therefore keeps access to archives created later. Everything the tool writes below inherits
  it (SQLite files, `CreateTemp` files, `MkdirAll` folders). A protected DACL stops later changes to the parent's ACL
  from reaching the archive (§6.5).
- **Never-list [CC6].** When `-out` is a drive or volume root or the profile directory, or holds entries outside the
  tool-owned set (A3), the root's DACL is never changed: each tool-owned store directory and fixed file is protected
  individually, and while anything tool-owned stays open `status` is RED "the archive shares a folder with other files
  — move it into its own folder".
- **Existing archives — slice A-W [CC8].** Rewriting an existing archive's DACL is not part of slice A. Until slice A-W
  ships (gated on the OQ-1 ruling and a completed MA-368 drill), an existing archive whose root a broad SID can read is
  RED `access_open`, and the remedy is the explicit, logged opt-in `mailarchive access -out DIR -restrict` (also a
  dashboard button). It first saves the root's original SDDL to `<UserConfigDir>/mailarchive/acl-backup/<hash(out)>.sddl`
  (`0600`, outside the served set), then writes a protected DACL equal to the effective DACL minus the broad-SID entries
  (all others, inherited ones included, kept as explicit ACEs) plus the three full-control entries, subject to the
  never-list and the effective-rights check; Windows propagates the new inheritable ACEs to existing children.
  `mailarchive access -out DIR -restore` re-applies the saved SDDL. README-RMM carries a fleet script for both, and
  Detect-RMM reports `Posture`. Whether A-W later does this automatically on the first run is OQ-1. **[ruling OQ-1]**
  It does not: A-W is not built, and the opt-in `access -restrict` / `-restore` above is the shipped behaviour. The
  tool never rewrites an existing archive's DACL unasked, and `status` stays RED for an open archive until it is
  restricted. MA-500 (MA-368's lab part) still runs, as slice A's Windows drill.
- **Failure** (no `WRITE_DAC`, a network share refusing it) → warning + `access_no_permission`, naming the folder and the
  `icacls` command an administrator can run **[CC9]**.
- **Posture [CC7].** `Posture(dir)` answers P4 on every lock-holding run and in `status`: `Unenforceable` when the
  volume lacks `FILE_PERSISTENT_ACLS` (`GetVolumeInformation`); `Open` when a broad SID can read the root, the fixed set
  or any manifest-named store directory, or when the last walk recorded an offender — a child whose owner is not
  TokenUser, SYSTEM or Administrators, or that carries an explicit broad ACE **[CC10]**. It costs a few dozen
  `GetSecurityInfo` calls and no tree walk. The `access_policy` stamp only suppresses repeated log lines; it is keyed to
  the root's volume serial and file ID, so a copied or restored archive is re-checked where it now lives. `status -json`
  lists the principals that have access, so an MSP can audit them.

**A6 — status (P4).** `health.Gather` adds `Access`: it `Lstat`s the fixed set of A3, resolving a symlinked `-out`
first (a symlinked `-out` is allowed, P5) **[CC11]** (no tree walk; `status` stays fast and read-only), calls `Posture`
on Windows **[CC7]**, and reads the last run's per-offender reasons and link report from the last-run record (new
optional `access` reasons with a timestamp, and a `links` count + first path) **[CC9]**. `health.Assess`:
- RED `access_open` ("search.db is readable by other accounts on this computer (mode 0644) — run any capture or
  `mailarchive reindex -out DIR` to make the archive private") — only once the archive has a current `access_policy`
  stamp. Before its first stamped run an archive reads WARN "will be made private on the next run (<next scheduled
  time>)", so a fleet rollout does not raise a RED that the next run clears by itself **[CC11]**. On Windows, an
  existing archive's RED names `mailarchive access -out DIR -restrict` until slice A-W ships **[CC8]** (**[ruling
  OQ-1]**: A-W is not built, so the RED names it until the archive is restricted);
- WARN `access_unenforceable` ("this drive's file system cannot restrict who reads the archive (exFAT); keep it on an
  encrypted or private drive"; on macOS "this drive ignores ownership — Finder ▸ Get Info ▸ uncheck *Ignore ownership on
  this volume*"; for the Linux types of A3 "access control is unverified on this file system") **[CC9]**;
- RED `access_not_owner` ("<file> belongs to <owner>, so other accounts may read or replace it — run
  `sudo chown -R <you> <out>`"; on Windows "take ownership of <dir>") and WARN `access_no_permission` ("this account
  cannot change who may read <dir> — an administrator can run: icacls …") **[CC9, CC10]**;
- WARN `links_in_archive` ("a symbolic link stands at <path> inside the archive; the tool will not write through it —
  replace it with a real folder"). This code and its text ship with slice B; until then slice A's walk only counts links
  and `status` says nothing about following them **[CC12]**;
- WARN for a home/volume-root `-out`, and RED "the archive shares a folder with other files — move it into its own
  folder" when a Windows root holds non-archive entries and a tool-owned entry is open **[CC6]**;
- WARN `config_in_archive` (P10, desktop capture) **[CC16]**, and the note that the schedule's log lies outside `-out`
  and is not covered by a redaction **[CC29]**.

The dashboard's Status & health view shows the same lines (X10).

### 4.B No link-following writes or deletes (P5; slice B)

**B1 — `ArchiveFS`.** New package `internal/archivefs` — the single no-follow primitive package shared with
`docs/design-recapture-integrity.md`, which moves verify's component-wise `Inspect` there. Whichever slice lands first
creates the package and the other adds to it, and recapture's sweep and gates call the same `ArchiveFS` **[CC2]**.
`OpenArchiveFS(out)` opens `os.OpenRoot(out)` (Go 1.24+; `go.mod` is `go 1.26.0` with no toolchain line, and CI floats
on Go 1.26.x — revision 1 said "toolchain 1.26.9" **[CC4]**). `OpenRoot` follows links in `-out`'s *own* path, so `-out`
may be a symlink. Methods take `/`-separated relative paths and refuse any `..`, absolute or empty segment.

**Link classification [CC13]** (the same rule as `docs/design-fault-isolation.md`'s FC1): a component is a *link* when it
is `ModeSymlink` or, on Windows, a name-surrogate reparse point — a junction or mount point, which Go ≥ 1.23 reports as
`ModeIrregular` without `ModeDir` (`src/os/types_windows.go:193-229`). Any other `ModeIrregular` entry is judged by its
reparse tag (`os.Readlink` fails): a cloud placeholder (OneDrive Files-On-Demand, which Go reports as `ModeIrregular`, with
`ModeDir` added for a folder) or a dedup file is treated as the file or directory it represents. "A real directory"
therefore means `IsDir()` and not a link — never `Type() == ModeDir`, which would refuse every cloud-tagged folder.
- `MkdirAll(rel)`: for each component, `root.Lstat`; an existing component must be a real directory (a
  link or a file → `*LinkInArchiveError{Path}`); a missing one is `root.Mkdir(comp, 0o700)`.
- `CreateTemp(relDir)`: `MkdirAll(relDir)` checks, then `root.OpenFile(relDir/.mailarchive-<rand>.tmp,
  O_CREATE|O_EXCL|O_WRONLY, 0o600)`.
- `Commit(tmp, relDst)`: fsync, close, `root.Rename` (Go 1.25+), fsync the parent directory.
- `Remove(rel)`: `Lstat` every parent (no symlink), `Lstat` the leaf (a symlink leaf is *removed itself*,
  never followed — removing a link cannot touch its target), `root.Remove`.
- `OpenNoFollow(rel, flag, perm)`: parent walk + `O_NOFOLLOW` (Unix) / reparse-point check (Windows) + fstat
  regular-file check (a cloud placeholder counts as regular) **[CC13]**.

`os.Root` guarantees no operation escapes `-out` even if a component is swapped concurrently (its Unix
implementation walks with `openat`+`O_NOFOLLOW`); the `Lstat` walk adds the house rule that an in-archive
link is refused, not followed, with a check-then-use window that only lets someone who can already write the
archive redirect within it. That guarantee covers operations through `os.Root` only. The path-based opens — `search.db`
and its siblings (SQLite opens by path) and the run log — are check-then-use, and a writer of the root can redirect them
anywhere until P1/P2 make the root private; P5's label says so **[CC13]**.

**B2 — call sites.** `Exporter` holds an `ArchiveFS` for the run: `exporter.go:301` (directory),
`writeFileAtomic*`/`commitTemp` (`atomic.go`), the zip writer, the prior-file prune (`exporter.go:478-483`).
`pages.writeFolderPage` and `writeRootPage` use it (pages need an `ArchiveFS` argument from `Generate`'s
callers). `reindex`'s `deleteMessageFiles` uses `ArchiveFS.Remove` after `validRelPath`, and each `AlsoFiles`
entry must additionally pass verify's `inspect` (`verify.go:390`) as a `.html` path before any of its triple is
removed. `SweepOrphans` removes through it. `OpenHistory` opens through `OpenNoFollow` (`0o600`); `index.Open`
does A2's `Lstat` checks (SQLite opens by path, so this check precedes `sql.Open`; the residual swap window
needs write access to the `0700` root); `runlog.Open` already refuses symlinks (MA-93). D2's existence test goes
through `ArchiveFS` and is three-state — present, gone or **blocked** (a link or irregular component on the way) — and a
blocked member is never treated as gone **[CC27]**.

**B3 — classification.** A `LinkInArchiveError` from the exporter is a run-level error for that store: it
propagates exactly like any other `Export` error today (`run.go:529-531`; Graph's equivalent): the store's walk
stops, other stores continue, the run exits `1`, the last-run record is `failed` with the message, `status` is
RED. Nothing is recorded as a gap: the messages not yet written are simply not in the manifest and are
captured by the first run after the link is replaced (no fillable/terminal record is created, so nothing
"never retries"). In `reindex`, an entry refused by B2 is not deleted, is listed under "not removed" in the
summary, and makes `reindex` exit `1` (P13) — today it is a log line with exit `0`. That member keeps its manifest
record, marked `redaction:"incomplete"` (D5 R7), so the next run retries it **[CC28]**.

**Store-level links [CC14].** A store directory that is itself a link is refused like any other component, so that store's
capture exits `1` after the upgrade — a real change for an owner who moved one large mailbox to another disk and linked it
back. §6.1 and the release notes say so, and the refusal text (§7.6) offers the supported layouts: move the whole archive and
point `-out` at it, or make `-out` itself the link (P5 allows it).

### 4.C Local web surfaces (P6–P10, P15; slices C1 and C2)

**C1 — one guard, one route registry.** New package `internal/localweb`:
- `type Surface struct{ Name string; Listener net.Addr; ExtraHosts []string; Session *Session }` and
  `Wrap(s Surface, inner http.Handler) http.Handler` — the only way a production binary serves an HTTP
  handler (`serve`, `setup`, the desktop dashboard and reader). Every route of every surface is registered
  through `localweb.Mux.Handle(pattern, h)`, which records the pattern so MA-378 can probe every route by
  name. **One route model [CC15]:** the inner handlers' bare muxes (`server.go:26`, `dashboard.go:83`, `setup.go:27`)
  become `localweb.Mux` instances — a recording `ServeMux`, so the handlers stay testable unwrapped — and a structural
  check fails if any `http.Server{Handler: …}` or `http.Serve` in `cmd/` is given a handler not built by
  `localweb.Wrap`. MA-378 ships in this slice.
- **Host allow-list (P6, slice C1).** First check on every request: `Host` (lower-cased; no trailing dot, no
  userinfo, port required) must be `127.0.0.1:<p>`, `localhost:<p>` or `[::1]:<p>` for **any** port `<p>`, so a
  forwarded local port (`ssh -L 9000:127.0.0.1:8099`) works; the rebinding defence rests on the hostname (a rebinding
  page's Host carries the attacker's name), not on the port **[CC18]**. For `serve`
  bound to a non-loopback address only: also that IP literal, or, for `0.0.0.0`/`[::]`, the IP literals of
  this machine's interfaces at start-up, plus names given with the new repeatable `serve -allow-host NAME`.
  IP literals are safe against rebinding (a rebinding page's Host carries the attacker's name). Refusal: `403`,
  `text/plain`, body `refused: this page is served only as http://127.0.0.1:<p>/ — open it from MailArchive`,
  no other content.
- **Headers on every response (slice C1):** the existing CSP/nosniff/no-referrer sets are kept and
  `Cache-Control: no-store` (so pages opened in the reader are not kept in the browser's disk cache — this
  matters for P14) and `Cross-Origin-Resource-Policy: same-origin` are added.
- **`/files/` allow-list (P9, slice C1)** lives in `server.New`'s file system, so `serve` and the desktop reader
  share it: `containedDir` becomes an `os.Root`-backed `allowedFiles`. `Open(name)`: split into segments; refuse
  (404) any segment starting with `.`; at depth 1 allow only `index.html`, `README.txt` or a directory; below
  allow a directory (served only via its `index.html`, as today), `*.html`, `*.eml`, `*-attachments.zip`;
  refuse the top-level `_outlook-pst`; `root.Lstat` each prefix (symlink → 404); then `root.Open` and fstat
  (regular file or directory). This replaces `EvalSymlinks`-then-`os.Open` (closing N10).
- **Credentials outside the archive (P10, slice C1).** `graph` (`main.go:1007`) and `parseJob` (`job.go:293-310`)
  resolve `-token-cache`, `-client-secret-file` and `-out` with extract's `destOverlapsOut`/`resolveExisting`
  (`extract.go:618-674`), moved to `util` and shared: the deepest existing ancestor is resolved, so a file whose parent
  does not exist yet is still placed correctly, and no fourth containment helper is written **[CC16]**. Containment is
  tested component-wise (case-insensitive on Windows/macOS). When `-out` contains the OS config directory (`-out` is the
  home directory), that directory is exempt: the default token cache there is not "inside the archive", the reader never
  serves it (P9), and tightening never touches it (A3) **[CC16]**. Interactive `graph`: refuse, exit `1`, nothing written
  (no token cache created, no capture). With `-unattended` (every scheduled job carries it, MA-99): log a warning and set a
  last-run `warning`; `status` WARNs `credential_in_archive` — an installed backup keeps running (refusing at run time would
  silently stop backups of existing installs; the reader no longer serves the file, P9). `schedule --install` refuses
  (operator present). The desktop's Settings POST (the operator is present) refuses an archive location that equals,
  contains, or is inside the OS config directory (`graphconfig.DefaultConfigPath`'s directory, which holds the config,
  settings and the file token cache). Its scheduled `HeadlessCapture` does not refuse: like `graph -unattended` it logs a
  warning, writes a last-run warning, and `status` WARNs `config_in_archive`; then it captures **[CC16]**.

**C2 — sessions (P7, slice C2).** Per process: `P` = 16 random bytes (base32, the path prefix), `S` = 32 random
bytes (the cookie value), both in memory only, discarded at exit. A launch token `T` = 32 random bytes
(base64url), stored as `sha256(T)` in a table of at most 16 entries (oldest evicted), each valid for **one**
redemption — within about **60 seconds** for a token the process hands to the browser opener itself, and **10 minutes**
for a printed or copied link; the opened link and the printed link are separate tokens **[CC5]**.
- `GET /_open?t=T` (any surface; Host-checked first) **[CC19]**: an *unknown* token (not in this process's table) →
  `401` with neutral text ("This link is from an earlier session — get a new one", §7.6); an *expired* token → `401`
  "This link has expired"; a *used* token presented by the browser that redeemed it (it carries that redemption's marker
  cookie M) → the same Continue page again, silently; a used token **without** its M → `401` with the warning text
  ("already used … if you did not open it yourself …", §7.6), the only case that can mean a raced link. Valid → mark used,
  respond **`200`** (not a redirect) with `Set-Cookie: mailarchive_session=S; Path=/s/<P>/; HttpOnly; SameSite=Strict`, a
  second cookie `mailarchive_open=M; Path=/_open; HttpOnly; SameSite=Strict` (M = 16 random bytes stored with `sha256(T)`,
  never derived from T, so an argv watcher who saw T cannot forge it), and a page whose
  `<meta http-equiv="refresh" content="0;url=/s/<P>/">` plus a visible "Continue" link navigate same-origin. A
  `200` page is used because the launch navigation arrives cross-site (opened by the OS, from a terminal, or
  from a file), and a `SameSite=Strict` cookie is withheld from every hop of a cross-site-initiated redirect
  chain; the meta-refresh navigation is initiated by our own page, so the cookie is sent.
- Every request under `/s/<P>/`: at least one `mailarchive_session` cookie (at most 8 are examined, each with a
  constant-time compare) must equal `S`; the prefix is stripped and the inner handler sees `/`, `/api/…`,
  `/files/…`, `/goback` exactly as today. Every request elsewhere, and a failed check, gets the locked page
  (`401`, HTML) — or JSON `{"ok":false,"error":"…"}` with `401` when the path ends in or contains `/api/` — with
  no archive data, no paths, no account names, no CSRF token, never `P`.
- **Why a secret path, not just a cookie:** a cookie for host `127.0.0.1` is sent to *every* port on that host.
  Without the path scope, a co-account who runs a listener on `127.0.0.1:9000` and gets the user to click a link
  to it inside an archived email (rendered by the reader at `127.0.0.1`, so the navigation is same-site and the
  `Strict` cookie is attached) would receive the session. With `Path=/s/<P>/`, the browser sends the cookie only
  to URLs under that prefix, which the attacker cannot form; `Referrer-Policy: no-referrer` (header and the
  archive pages' own meta) keeps `P` out of the request the click makes.
- The reader UI (`internal/server/page.go`: `fetch('/api/search…')`, `'/files/'`, `/goback`), the go-back page's
  links and the dashboard script's `fetch('/api/…')` become **relative** URLs (`api/search`, `files/…`, `goback`)
  so they resolve under `/s/<P>/`. **The complete list [CC17]** (from a grep, not a sample) also covers
  `<script src="/app.js">` (`page.go:61`), `/dashboard.js` (`dashboard.go:436`), `<script src="/setup.js">` and
  `fetch('/api/setup')` (`setup_page.go:109,130`), `href="/"` (`dashboard.go:336`, `goback.go:323`) and go-back's
  `template.URL("/files/"…)` builder (`goback.go:267`) — or every surface page emits `<base href="/s/<P>/">`. `Wrap`
  rewrites an absolute `Location` header from the inner handler (a `ServeMux` clean-path or trailing-slash 301) so it keeps
  the prefix, or refuses it. A structural check fails if any HTML or JS a surface serves contains a same-origin absolute URL
  (`src="/`, `href="/`, `action="/`, `fetch('/`, `template.URL("/`), and each surface — `setup` included — has a functional
  positive twin that loads its page and subresources under the prefix. Archived pages already use relative links.
  `server.New` stays the single reader implementation for `serve` and the desktop.
- **CSRF:** unchanged and kept as defence in depth — every state-changing dashboard/setup POST still passes
  `GuardLocalPOST` (Host loopback, Origin loopback, `X-CSRF-Token`); the token is now only ever delivered inside
  an authenticated page.
- **Session visibility:** each redemption is announced — `serve`/`setup` print to stderr
  `browser session opened at 14:02 (1 this run)`; the dashboard's header (and its Security & privacy panel) shows
  `browser sessions opened this run: N (last at 14:02)`, kept outside the 400-line capture ring so it is visible at rest
  and is not evicted during a capture **[CC24]**. A second notice the user did not cause is the signal of a raced link (§8.4).

**C3 — delivering the link, per surface.**
- **`mailarchive serve`:** opens the browser at its own `/_open?t=…` (an opener token, about 60 s) unless the new
  `-no-browser` is given or no display is detected (Linux: `DISPLAY` and `WAYLAND_DISPLAY` empty, or an SSH session
  without one) **[CC18]** (the argv the desktop already uses, `openBrowserArgv`). When stdout is a terminal it also prints
  a separate, 10-minute link (X8: stdout carries the served URL) — as `if your browser did not open, use:
  http://127.0.0.1:8099/_open?t=…` when the auto-open succeeded **[CC5, CC19]**. When stdout is not a terminal (a
  journal, `nohup.out`, a CI log) it prints the address and `get a link with: mailarchive serve -link -out <folder>`
  instead of a token **[CC5]**. When stdin is a terminal *and the process is in the terminal's foreground process
  group* (`tcgetpgrp == getpgrp`, checked before each read, so a backgrounded `serve &` never takes SIGTTIN and stops),
  pressing Enter prints a fresh link for a second browser or device **[CC18]**.
- **`mailarchive serve -link -out DIR` [CC18]:** `serve` writes a `0600` per-account instance file (keyed by a hash of
  `-out`, in the directory of C4 step 1, holding its address and a key `K`). `serve -link` performs C4's HMAC handoff with
  `quiet:true` and prints the fresh 10-minute link the instance returns (the reply MAC covers the link), so a `serve` run
  under nohup, launchd, systemd or a detached tmux can admit a new browser or device without a restart. A LAN `serve`'s
  cookie may carry a `Max-Age` (one working day), so closing the browser does not end its session. The locked page names
  whichever path applies: the printed link, Enter in the terminal, or `serve -link`.
- **`mailarchive setup`:** the same, and its default address becomes `127.0.0.1:0` (the link it opens or prints carries
  the real port), so it no longer collides with the desktop's 8097 **[CC18]** (**[ruling OQ-9]**: the desktop leaves
  8097 too).
- **MailArchive Desktop:** at start (not `-no-browser`) the process opens the browser at its own `/_open?t=…`.
  The dashboard gains a guarded POST `/api/browser-link` behind a "Use another browser…" button that returns a
  fresh one-time link to copy (the shortcut always opens the *default* browser). A Start-menu shortcut **"Copy MailArchive
  link"** (`mailarchive-desktop -copy-link`) performs the C4 handoff with `quiet:true`, receives a fresh MAC-covered link and
  shows it in a dialog with a Copy button — a way in that needs no OS browser opener; the locked page names it **[CC20]**.
- **macOS opener [CC5]:** the darwin opener runs `osascript` and writes `open location "<url>"` to its stdin, so the token
  is in no process's argv (LaunchServices delivers it by Apple Event). The lab drill (MA-377) confirms that no prompt
  appears; if one does, the argv path stays, with its residual stated (§8.4).
- **Launch-file indirection** (writing a `0600` HTML redirect file and opening its path, as Jupyter does, to keep
  the token out of process argv) was considered and rejected: on Windows and macOS an `.html` file may be
  associated with an editor rather than the browser, and Ubuntu's default snap-confined Firefox cannot read files
  in hidden directories. On Windows a standard account cannot read another account's process command lines. On Linux
  (`/proc/*/cmdline`) and on macOS (setuid `/bin/ps`, no hidepid equivalent) another account can; those residuals are
  §8.4, narrowed by the 60-second opener token and by macOS's stdin delivery **[CC5]**.

**C4 — the per-account desktop instance (P8, slice C2).** In `cmd/mailarchive-desktop/main.go` (server mode):
1. Acquire a per-account lock `desktop.lock` in a **non-purgeable** per-account directory **[CC21]**:
   `%LOCALAPPDATA%\MailArchiveDesktop` on Windows (not roaming; already the product's directory), and the product's config
   directory (`os.UserConfigDir()/mailarchive`) on macOS and Linux — not `os.UserCacheDir`, which cache cleaners purge
   (directory `0700`). Use the existing `internal/lockfile` primitive (no-follow, `0600`, held for the process's life; its
   holder line records the pid). If the directory cannot be determined (`HOME`/`%LOCALAPPDATA%` unset), the launch refuses
   visibly (fail closed). The lock holder re-checks its lock every minute (`lockfile.StillHeld`) and rewrites a missing
   instance file **[CC21]**.
2. **Lock taken** → this is the account's only instance of *this configuration*. First remove any stale instance file
   **[CC21]**. Then bind the dashboard on `-addr` (default `127.0.0.1:0` **[ruling OQ-9]**; revision 2 had
   `127.0.0.1:8097`). **When `-addr` was not given explicitly**, probe 127.0.0.1:8097, MailArchive's former address, and
   check what holds it: a listener that answers `/` but has no `/_handoff` route is a **pre-upgrade instance**
   — if it is this account's (same image path and token user), end it; otherwise tell the user to sign out and back in, and
   warn in the dashboard and `status` **[CC22]**. Any other listener there is unverified and is reported, **never
   silently**: the dashboard header and `status` say "another program is using MailArchive's former address
   127.0.0.1:8097 — open MailArchive only from its shortcut" **[CC23; ruling OQ-9: the notice stays, adapted]** (an
   explicit `-addr` in use is a refusal, §7.6). Bind the reader on `-reader-addr`, whose
   default changes from `127.0.0.1:8099` to `127.0.0.1:0` (the dashboard links to the actual port; this frees 8099 for
   `serve`, F8). Write the instance file `desktop-instance.json` next to the lock (`util.WriteFileAtomic0600`):
   `{"v":1,"pid":…,"exe":…,"started":…,"config":"<config path>","session":<logon-session id>,"dashboard":"127.0.0.1:53210","reader":"127.0.0.1:53211","key":"<64 hex>"}`
   (`key` = `K`, 32 random bytes, distinct from `S` and `P`) **[CC21]**; the dashboard's port is ephemeral **[ruling OQ-9]**. Then serve, and open the browser unless
   `-no-browser`. Remove the file on graceful shutdown (best effort).
3. **Lock held by another process of this account** → an instance exists or is starting. Read the instance file with
   `util.ReadSecureFile` (no-follow, regular file, size bound, the `0600` test on Unix) plus, on Windows, an owner-SID check
   (`GetSecurityInfo`: the owner is this account's TokenUser) **[CC21]**, retrying for up to 15 s while it is absent and the
   lock is held. Cross-check its `pid` with the lock's holder line. If its `config` differs from this launch's
   `-config`/`-out`, or its `session` is another logon session, do **not** hand off: show a visible refusal ("MailArchive
   Desktop is already running for your account with another configuration / in another session …"), or, in another
   session, start a separate instance keyed by that session **[CC21]**. Otherwise **hand off**:
   `POST http://<dashboard>/_handoff` with JSON `{"n":"<32-byte hex nonce N>",
   "p":"<hex HMAC-SHA256(K, "handoff-req|" + N)>"}`. The instance verifies `p` (constant time; body ≤ 1 KiB; at most one
   handoff per second — a second one within that second is answered with a MAC-covered `{"busy":true,…}`, which the client
   treats as success **[CC21]**), mints a launch token, opens the browser at its own `/_open?t=…` (unless the request says
   `"quiet":true`, which a `-no-browser` or `-copy-link` launch sends), waits up to about 15 s for that token to be redeemed,
   and answers `{"opened":true|false,"link":"<a fresh link, when quiet or not opened>","p":"<hex HMAC-SHA256(K,
   "handoff-ok|" + N + "|" + opened + "|" + link)>"}` **[CC20]**. The client checks the answer. On `opened:false` (or when
   the instance's `cmd.Start` failed) it shows the fresh link in a dialog with a Copy button; otherwise it exits `0`.
4. **Handshake fails** (connection refused, wrong proof, timeout 3 s), retried for up to 15 s while the lock is held
   **[CC21]**: the client never opens the browser at that address. If the lock is still held by a live process, our own
   instance is wedged: the client shows the refusal of §7.6 ("MailArchive Desktop is running but not responding — quit it
   from Task Manager (pid <pid from the lock>) and start it again") and exits `1`. (With the lock free, step 2 applies and
   any stale file is removed first.)

**Refusals are visible [CC20].** The desktop is a `-H=windowsgui` binary with no console, so every client-side refusal of
this section and of §7.6 goes to `showRefusal`, a seam over `zenity.Error` (the module already depends on
`github.com/ncruces/zenity` v0.10.12, which uses MessageBox on Windows, an `osascript` dialog on macOS and zenity on Linux),
and also to stderr. MA-376 asserts that the seam is called with the exact §7.6 text.

`/_handoff` is a Host-checked, unauthenticated-by-cookie route of the dashboard listener only, and it can do
exactly one thing — make *the instance* open a browser in *its own* logon session, or return a fresh link to a caller that
proves `K` — so a co-account that could somehow call it gains nothing; without `K` it cannot even do that. The handshake
authenticates both directions, so a squatter on the recorded port (F4) cannot make the client open anything.

### 4.D Redaction that removes (P11–P14; slice D)

**[ruling ESC-4]** OQ-8 is escalated to the owner, together with R13's classifier amendment (only an operator's deletion
is a redaction; a moved, renamed or unreadable file keeps its record) and R13's added deletion cases (the index scrub,
the stale-page and empty-folder removal, the stale-snapshot sweep, and the manifest-driven gone set of D2's "Members").
Until the owner answers, plain `reindex` keeps today's behaviour, slice D does not start, and its catalog-first PR lands
none of these. The text below is the design the owner is asked to accept; RR-5 (lenses 3, 6 and 7 on §4.D) runs before
D's catalog-first PR.

**D1 — the index scrub.** Changes in `internal/index`:
- `pragmas` (`index.go:32`) gains `PRAGMA secure_delete=ON; PRAGMA busy_timeout=5000;` for every read-write
  connection, so every future delete zeroes the freed cell content, and schema creation (and the one-time legacy scrub)
  runs `INSERT INTO docs_fts(docs_fts, rank) VALUES('secure-delete', 1)`, which FTS5 (SQLite ≥ 3.42; the vendored copy is
  3.50.3) persists in its `%_config` table, so a delete also removes the document's entries from the FTS segments.
  Together they are the **mechanism** for every policy-1 index, not a belt: a redaction there costs O(deleted rows), not
  a rewrite of the file **[CC33]**. Their standing cost (extra writes and WAL volume on every capture's replace-by-key and
  FTS merge) is measured and stated in §7.1 when slice D is built; an indicative scratch run showed about +25 % `-wal`
  volume **[CC33]**. `OpenReadonly` gains `busy_timeout` so `serve` waits out a scrub instead of erroring.
- New table `scrub(pending INTEGER NOT NULL, policy INTEGER NOT NULL, at TEXT, attempted_at TEXT, last_error TEXT,
  needed_bytes INTEGER)` (created `IF NOT EXISTS`; `indexVersion` stays 3 — no key-format change; older binaries ignore the
  table) **[CC34]**. `policy` is the scrub-policy version (this design: `1`); a missing row means "never scrubbed by
  policy 1" — but a database this binary creates (A2's create branch, and Rebuild's new `search.db.rebuild`) is stamped
  `pending=0, policy=1` at schema creation, so a fresh or rebuilt index is never mistaken for a legacy one **[CC33]**.
- `(*Index).PruneForRedaction(ids []int64)`: one transaction that deletes each id from `docs` and `docs_fts`
  (as `deleteByIDTx`) **and** upserts `scrub.pending = 1`; commits.
- `(*Index).Scrub() (ScrubReport, error)`, with no open batch: (1) `INSERT INTO docs_fts(docs_fts)
  VALUES('optimize')` — merges every FTS5 segment into one, dropping deleted documents' entries; commit;
  (2) `PRAGMA wal_checkpoint(TRUNCATE)`; (3) `VACUUM` — rebuilds every b-tree into fresh pages and shrinks the
  file, leaving no free page and no freed-cell residue; (4) `PRAGMA wal_checkpoint(TRUNCATE)`, retried up to 5
  times over 10 s while a reader holds a read transaction, then the `-wal` file's size must be 0; (5) a small
  transaction setting `pending = 0, policy = 1, at = now` and a final TRUNCATE checkpoint.
  `ScrubReport{BytesBefore, BytesAfter, Duration}`. Before step 1 it turns both secure options on, so from then on the
  index is a policy-1 index **[CC33]**.
- Why this sequence (scratch experiment, 2026-10-09, SQLite 3.50.3, 300 documents in 3 segments, one deleted,
  case-insensitive raw-byte search of `search.db`+`-wal`): plain `DELETE` → body, subject and FTS tokens all
  still present; `VACUUM` without `optimize` → the FTS tokens still present (the segment blobs are copied
  verbatim); `secure_delete` alone → FTS tokens still present; `optimize`+`VACUUM` → none; before the TRUNCATE
  checkpoint the `-wal` still held earlier frames containing the text. `secure_delete` set *before* the delete
  plus `optimize` was also clean in that fixture, but it cannot clean residue left by earlier non-secure deletes
  (every existing archive, and a delete made by an older binary), so `VACUUM` is part of the scrub. Surviving
  rows keep their ids: `docs.id` and every FTS5 shadow table use `INTEGER PRIMARY KEY`, which `VACUUM` preserves
  (survivors still matched after the experiment's `VACUUM`). The review re-ran the experiment on 2026-10-09 for the
  policy-1 path: with `secure_delete` and FTS5 `secure-delete` both on, a delete plus a TRUNCATE checkpoint left zero
  occurrences of the body, subject and FTS tokens and the survivors still matched, while residue from an earlier plain
  delete stayed until `VACUUM` **[CC33]**.
- **In place, not `VACUUM INTO` + rename:** an in-place `VACUUM` works while `serve` or the desktop reader holds
  an idle connection (observed: the reader saw the scrubbed database immediately), whereas renaming over
  `search.db` fails on Windows while any process holds it open (F5) and, on POSIX, leaves an open reader on the
  old inode. The cost: SQLite builds `VACUUM`'s working copy as an anonymous temp file in the OS temp directory
  (`0600`, unlinked at creation on Unix, delete-on-close on Windows). That copy holds only *surviving* content —
  the redacted rows are deleted and committed before `VACUUM` starts. Transient space: about twice `search.db`'s
  size free on the archive's volume (WAL growth) and its size again in the OS temp directory; a free-space pre-flight runs
  first (below) **[CC34]**.
- **When the full `Scrub` (optimize + `VACUUM`) runs [CC33, CC34]:** only for legacy residue — in `reindex` when
  `policy < 1` (an index created before this policy) or when `scrub.pending = 1` was left by an older-binary delete or an
  unfinished scrub; on an explicit `reindex -compact`; never in `reindex -rebuild` (its database is fresh and stamped); and
  once at the end of the first `export`/`graph`/desktop capture run that opens an index with `policy < 1` (the one-time
  legacy scrub, OQ-6 default; **[ruling OQ-6]**: automatic, as described, after the free-space pre-flight and retried at
  most weekly when unattended), after the capture is committed so a scrub failure cannot starve the capture. A redaction in
  a policy-1 index needs no full scrub: R9 is a `PRAGMA wal_checkpoint(TRUNCATE)` (with step 4's retry) after R4's
  committed `PruneForRedaction`, then `pending = 0`. Revision 1's `freelist_count > 0` trigger is removed, because it fires in
  the steady state (FTS5 automerge and replace-by-key free pages in every capture); §6.2's honest downgrade limit stays. A
  full scrub that fails when this run pruned nothing is a WARN with exit `0`, never a failed `reindex`.
- **Space, retries and legibility [CC34].** Before any `VACUUM`, a free-space pre-flight checks the archive's volume
  (about twice `search.db`'s size, for WAL growth) and SQLite's temp directory (about its size), with statfs /
  `GetDiskFreeSpaceEx` (the extraction workspace's `util.FreeBytes` if it has landed). SQLite's temp directory is pinned to
  the archive's volume where SQLite allows it (`PRAGMA temp_store_directory`, set before the `VACUUM`; an MA-380 case
  confirms the pin), otherwise both volumes are checked. When either is short, the scrub is skipped and `status` WARNs
  `index_unscrubbed`, naming the space needed and the volume. Each attempt records `attempted_at`, `last_error` and
  `needed_bytes`. An unattended capture retries at most once every 7 days and only after the pre-flight passes, while a
  manual `reindex` always tries. A start line names the index size and the expected duration, and says "safe to interrupt;
  it restarts next time". `status` tells "legacy scrub pending" from "failed <date>: needs N GB free on <volume> — run
  `mailarchive reindex`".

**D2 — the reconciled gone set: decided by the record, classified, and only with intent.**
- **One decision table [CC26, CC2].** `Reindex` decides every row with `docs/design-recapture-integrity.md` §4.9's
  table, which becomes the single Reindex decision table both designs amend; this section is an amendment, not a
  replacement. A **repair** row — the row's path is missing but the manifest record's `rec.Path` differs and is present
  (F48: a re-capture renamed the stem and the index batch was lost) — re-points the row and is **never** a member: nothing
  is deleted, scrubbed or dropped, and the summary says "repaired N". If slice D lands before recapture S2 (MA-361), D
  carries the repair row itself.
- **Members.** The redaction set is (§4.9's "prune" rows) ∪ (manifest records that have no index row and whose own
  `rec.Path` `.html` is missing). A crash that removed the index rows but not the manifest records (§1(d) finding 1) is
  therefore repaired by the next `reindex`, and a member kept as `redaction:"incomplete"` (D5 R7) is found again **[CC28]**.
- **Classifier — a prune is not a redaction [CC27].** A row or record is a member only when its own `.html` is absent
  (ENOENT) through `ArchiveFS` while its parent is a real directory, and that directory holds neither a
  Unicode-normalization-equivalent entry (the NFD form of an NFC slug, as an HFS+ copy leaves it) nor a cloud-eviction
  placeholder (`.<name>.icloud`). Existence is three-state — present, gone or **blocked** (a link or irregular component on
  the way, B1) — and a blocked member is never gone. Every other reason a repair path prunes keeps the record, is listed as
  "not indexed — kept: <path> (<reason>)", makes the run exit `1`, and never triggers R3, D3, D4 or R7: a missing parent
  folder (a moved or renamed folder), a link or irregular component, an `.html` over Rebuild's 512 MiB read cap, a file that
  cannot be read (EACCES, a sharing violation), and an `idx.Add` failure.
- **Intent [CC27; OQ-8; ruling ESC-4: escalated, the threshold recommended].** When the gone set exceeds the intent threshold — revision 2 describes: any member whose folder is
  missing entirely, or more than 25 members in one run — plain `reindex` reports "N messages' files are missing from
  <folders> — if you moved or renamed them, put them back; to remove them for good run
  `mailarchive reindex -redact -out …`", does nothing destructive, and exits `1`. `reindex -redact` (and the dashboard's
  confirmation, which lists the folders and counts) accepts every *gone* member, including those whose folder is missing;
  it never accepts a *blocked* one or another prune reason. Below the threshold, plain `reindex` redacts as the README
  documents.
- **Superseded copies [CC35].** When the manifest is recapture-integrity's v7, every `Superseded` entry whose Key belongs to a
  member joins the set, so its pending earlier copy (`<base>.html`/`.zip`/`.eml`) is removed too. **[ruling J2]** There is
  no v7: read "when the manifest carries recapture-integrity's `Superseded` list", an additive v6 field that exists once
  recapture-integrity S4 lands.
- For each member it keeps, in memory only: the relative `.html` path(s) — both the row's path and `rec.Path` when they
  differ **[CC26]** — and every `AlsoFiles` path; the file stem(s) (`<date>_<time>_<slug>_<hash>`, and each superseded base
  stem **[CC35]**); the Message-ID if the key carries one (`mid:` identity); and the subject, read from the member's index
  row before R4 prunes it, or from the record otherwise **[CC31]**.

**D3 — folder pages (P12).** `pages.Generate(out, ix, fs, logger, GenerateOptions{Prune bool, Folders set})` returns
the set of pages it wrote. Every folder page gains `<meta name="generator" content="mailarchive folder-page 1">`. `reindex`
and Rebuild set `Prune` **only when the gone set is non-empty**, with `Folders` the set of folders that held gone members
(capture runs never shrink folders and pass `false`) **[CC27]**. After writing, it walks only those folders, inside A3's
tool-owned scope (manifest-named store directories; never a nested archive, a dot-directory or `_outlook-pst`) **[CC6]**,
through `ArchiveFS` (no link followed). It deletes each file named `index.html` or `index-<n>.html` that Generate did not
just write **and** that is a tool folder page — it carries the generator meta, or (pages written before this revision) the
legacy template fingerprint `<table id="t"><thead><tr>` together with `<th data-i="0">Date (UTC)</th>` within its first
16 KiB — **and** whose directory holds no present file of a manifest record, so an incomplete index never costs a live
folder its pages **[CC27]**. A file that fails the identification test is never touched (a user's own `index.html`
survives). When it sits in a directory the index no longer lists, it is itemized under "still present — check this file"
and the run exits `1`, so a page from an older template cannot pass silently **[CC31]**. Then remove, bottom-up, each
directory left completely empty (`root.Remove` on an empty directory only; never a recursive delete; never `-out` itself).
Message file names never collide with the page names (stems are `<date>_<time>_<slug>_<hash>`). The root page is rewritten
as today.

**D4 — logs, reports, records (P12).** `scrubTextFiles(out, fs, gone)` rewrites every file in this list that exists,
**all inside `-out`**: root-level `*.log`, `*.log.1` and `*.stderr.log`, `attachments-report-legacy.tsv`,
`BACKUP-NEEDS-ATTENTION.txt` and `ARCHIVE-INTEGRITY-ATTENTION.txt` **[CC31]**.
- **Nothing outside `-out` [CC29].** The `-log` path named by `.mailarchive-schedule.json` (`schedule/descriptor.go:32`) is
  read only to name it. The descriptor is untrusted input — anyone who can write the archive could edit it
  (`descriptor.go:91-94`, §8.2) — and a run writes its own argv `-log`, never the descriptor's value. When that path
  resolves outside `-out`, the summary's "not covered" list and `status` say "your schedule's log <path> is outside this
  folder and was not changed". Every default log path is inside `-out` (`schedule.go:583`, `job.go:427`,
  `desktop/schedule.go:47-48`).
- **In place, not rename [CC30].** Each file is filtered into `.mailarchive-logscrub-<name>.tmp` (`0600`, through
  `ArchiveFS`, fsync). Then, under the archive lock, the original is opened through `ArchiveFS`, truncated, and the
  filtered content is written back through that handle; the temp file is removed. A leftover temp file from a crashed run
  is completed by the next run. The inode is kept, so on POSIX an `O_APPEND` writer (the GUI's `<out>/mailarchive.log`,
  the reindex run's own later lines) keeps writing to the live file, and on Windows no `FILE_SHARE_DELETE` is needed (Go
  opens files with read and write sharing, `syscall_windows.go:395`, which permits a second read/write handle and
  `SetEndOfFile`). The run's own `-log` is rewritten through the logger's own `*os.File`. A sharing violation from a foreign
  handle defers that file: it is itemized, and the members it names stay `redaction:"incomplete"` (D5 R7) **[CC28]**.
- **What makes a line match [CC31].** A line is replaced by `[line removed by mailarchive redaction 2026-10-09]` when it
  contains a gone member's exact stem token, its Message-ID in the bracketed `<id>` form the tool logs (at least 8
  characters and an `@`, so a short sender-chosen id cannot wipe unrelated lines), or its exact subject (at least 12
  characters, captured in D2 — this catches `render "<subject>"` errors, `exporter.go:321,328`). Each line's tokens are
  looked up in a hash set (substring needles use Aho-Corasick), so a gone set of 10^5 costs one pass over each file, not
  one pass per member.
- **The current run's own lines [CC27].** A line written by the current run is never rewritten. The run logs a removed
  member by count only, never by stem; only the stems of members it could not finish appear in its log, and those members
  stay incomplete, so a later run scrubs those lines when it finishes them.
- Lines are streamed, so size does not matter (logs are rotated at open once past 8 MiB, `runlog.go:27-31`, and a long run
  can exceed that), and a line longer than 1 MiB is replaced whole if it matches and copied unchanged otherwise **[CC31]**.
  The desktop's `-capture` log opens through `runlog.Open` (rotation; no duplicate copy into stderr), and `runlog.Open`
  rotates the `.stderr.log` sibling too, so no rewritten file grows without bound **[CC31]**.
- `attachments-report.tsv` is regenerated from the manifest as today. The last-run record's `Error` text is replaced by
  `[removed by redaction]` when it matches, through `WriteLastRun`, so `BACKUP-NEEDS-ATTENTION.txt` is regenerated from it
  **[CC31]**. After a dashboard Rebuild the desktop clears its in-memory activity ring **[CC31]**. `SweepOrphans`
  additionally removes stale `.mailarchive-snapshot-*` files (older than this run's start, under the lock, so by
  construction no run is reading them). `_outlook-pst/` is not deleted (a failed `-outlook` run keeps it deliberately); the
  summary names it.

**D5 — the redaction order** (in `Reindex`, as an amendment of recapture-integrity §4.9's order **[CC2]**). Rebuild runs
D3–D4 only for the records it classifies *missing* under D2's classifier — never for a record it pruned for a link, the read
cap, an unreadable file or an `Add` error, which it keeps and lists **[CC27]** — and needs no D1:

| Step | Action |
|---|---|
| R0 | acquire the archive lock; A3 tightening |
| R1 | `SweepOrphans` (incl. stale snapshots) |
| R2 | compute and classify the gone set (D2): repair rows re-pointed, kept reasons listed, the intent threshold checked — beyond it, report and stop before R3 **[CC26, CC27]** |
| R3 | delete each member's `.html`/`-attachments.zip`/`.eml` (both stems when the row and record paths differ) and every `AlsoFiles` triple through `ArchiveFS` (B2); record per member whether every delete completed **[CC28]** |
| R4 | `PruneForRedaction` (rows deleted + `scrub.pending = 1`, one transaction) |
| R4a | recapture-integrity's `finishSuperseded` for the members' `Superseded` entries (v7 manifests; **[ruling J2]**: manifests carrying the additive v6 list); an entry its gates block is itemized and kept **[CC35]** |
| R5 | `pages.Generate(Prune: true, Folders: the members' folders)` (D3) |
| R6 | `scrubTextFiles` (D4) — needs stems, so it runs while the manifest still holds the records |
| R7 | `writeReport`, `writeArchiveReadme`, `manifest.Save`: a member's record is dropped only if all of its R3 deletes, R4a entries, R5 page items and R6 lines completed; otherwise it is kept with the additive field `redaction:"incomplete"`, which D2 finds again on the next run **[CC28]** |
| R8 | `CompactHistory` (as today, `reindex.go:192-198`; driven by the manifest keep-set, stem-free) |
| R9 | policy-1 index: TRUNCATE checkpoint, `pending = 0`; legacy index: `Scrub` after the space pre-flight (D1; stem-free) **[CC33, CC34]** |
| R10 | summary (§7.6); exit `0` only if R3–R9 all completed and no member is incomplete; else exit `1` with the itemized list |

**D6 — honest text.** The README's "Two meanings of gone" item 2 (`README.md:428-433`), the archive's `README.txt`
(`internal/app/readme.go:100`), `reindex -h`, and `reindex`'s summary carry the coverage statement of §7.6. When the
gone set includes records of a live store (a Graph mailbox token) or of a source file that still exists, the summary
adds the re-capture warning. The summary also says that other kept messages which quote, forward, reply to or duplicate a
removed one still hold those bytes, and it may list the stems of kept messages whose subject matches a removed one once
Re:/Fwd: are stripped — never the removed subject itself **[CC32]**. `reindex -h` and the README document `-redact` and the
intent threshold (OQ-8) **[CC27]** (**[ruling ESC-4]**: once the owner has chosen the intent rule).

**Exit codes (X1 unchanged):** no new code. `export`/`graph`: `1` on a P5 link refusal or a P10 refusal.
`reindex`: `1` when any part of the removal could not be completed (P13), when D2 kept a record for a reason other than
an operator's deletion, or when the gone set waits for `-redact` **[CC27]**; a full scrub that fails when nothing was
pruned is a WARN with exit `0` **[CC33]**. `serve`/`setup`/desktop: `1` on a start-up
refusal; the desktop's successful handoff exits `0`. `status`: unchanged (exits `0` whenever it reports; C5 stands).

## 5. Crash ordering and failure domains

Direction words: **toward private / toward removal** = the residue is less exposure than before the step, never more;
**fail closed** = the surface refuses rather than admits.

| Write | Killed at… | Residual state | Direction | What the operator sees |
|---|---|---|---|---|
| A2 pre-create `search.db` | after create, before schema | empty `0600` file — a valid empty database | toward private | nothing; next `Open` creates the schema |
| A3 tightening walk | mid-walk | some entries tightened, `access_policy` not stamped | toward private | next run re-walks (idempotent); the "made private" line appears only on a completed pass |
| A5 Windows DACL set | mid-propagation | root protected; some children still carry old inherited ACEs | toward private | stamp not written → next run re-applies; `Posture` checks the fixed set, the store directories and the recorded offenders, not only the root **[CC7]** |
| A5 / `access -restrict` DACL write | the new DACL would cost the running account its access | not written: the effective-rights check runs before every write **[CC7]** | fail closed (no change) | WARN `access_no_permission` with the remedy; the original SDDL is saved before any rewrite, and `-restore` re-applies it **[CC8]** |
| B1 temp + rename | any point | as today (R5): old file or whole new file; a stray `.mailarchive-*.tmp` (`0600`) swept next run | unchanged | nothing |
| C2 session/tokens (memory) | process exit | all sessions and links void | fail closed | locked page; relaunch |
| C4 instance file | during write | old file or new file (atomic) | fail closed | the lock holder removes a stale file right after taking the lock, before binding; a client that finds the lock held retries the handshake for up to 15 s **[CC21]** |
| C4 per-account lock | process killed | OS releases the lock with the process | fail closed | next launch takes the lock and starts fresh |
| R3 message-file deletes | mid-way | some files removed, rows and records intact | toward removal | message still searchable with a dead page until a re-run; re-run redoes R3 (removing an absent file is a no-op) |
| R3/R5/R6 step **fails** (returns an error, no crash: EACCES, a sharing violation, a blocked link) | — | that member's record kept as `redaction:"incomplete"` at R7; other members finish **[CC28]** | toward removal | exit 1 itemizing it; `status` WARN `redaction_unfinished`; the next `reindex` retries it |
| R4 prune transaction | before commit | nothing pruned | toward removal (R3 done) | re-run redoes R4 |
| R4 | after commit | rows gone, `scrub.pending = 1` durable, residue in file | toward removal | `status` WARN `redaction_unfinished`; re-run continues |
| R5 page rewrite/cleanup | mid-way | some pages rewritten/deleted (each atomic) | toward removal | re-run's R5 is state-based: deletes every tool page it did not write |
| R6 log/report rewrite | mid-file | the filtered temp file is complete, or the original was partly rewritten in place from it | toward removal | the next run completes a leftover `.mailarchive-logscrub-*.tmp` before anything else; records still in the manifest, so R6 is redone **[CC30]** |
| R7 `manifest.Save` | before rename | old manifest (records present) | toward removal | re-run recomputes the gone set from the manifest (D2) and redoes R3–R10 |
| R7 | after rename | records gone; R8–R9 outstanding | toward removal | `pending = 1` still set → `status` WARN; re-run does R8–R9 (stem-free) |
| R8 history compaction | as today | old or compacted log (MA-220 ordering) | toward removal | serve already hides it (MA-211) |
| R9 `optimize` / `VACUUM` | mid-transaction | WAL recovery restores the pre-statement state; residue remains; `pending = 1` | toward removal | `status` WARN; re-run scrubs |
| R9 legacy scrub | the pre-flight finds too little space | not started; `pending`/`policy` unchanged; the attempt recorded **[CC34]** | toward removal (no change) | WARN `index_unscrubbed` naming the space and the volume; an unattended run retries at most weekly |
| R9 `VACUUM` | `SQLITE_FULL` | database intact, unscrubbed; WAL checkpointed back | toward removal | `reindex` exit 1: "free about <2×size> on <volume> and <size> in <tempdir>, then run reindex again" (exit 0 with a WARN when this run pruned nothing **[CC33]**) |
| R9 checkpoint | a reader holds a read transaction past the retries | scrubbed pages in the database, old frames in `-wal` | toward removal | exit 1 naming the reader ("close `mailarchive serve`/the archive reader, then run reindex again") |
| D4 concurrent append | another process appends while R6 rewrites a log in place | its lines land in the same inode after the rewritten content; a line appended between R6's read and its truncate is lost **[CC30]** | bounded loss of diagnostic lines | none |

**Failure domains (lost / compromised):**

| Component | Lost | Compromised |
|---|---|---|
| guard (`localweb.Wrap`) | n/a (code) — a bug fails closed: every request refused, locked page always shown (visible) | a bypass = F3 again, plus — once the extraction workspace mounts — its copy-the-mailbox-to-a-path primitive **[CC3]**; mitigated by MA-378 probing every registered route, workspace routes included, and by the `cmd/` structural check **[CC15]** |
| session `S`, prefix `P`, tokens | process exit → relaunch | same-account process reads process memory → out of scope (DC4) |
| instance file / `K` | the lock holder rewrites a missing file within a minute; a client retries while the lock is held, treats "busy" as success, and refuses (naming the lock's pid) only when the holder stays unresponsive for 15 s **[CC21]** | readable only by the account (`0600`, account-only directory, owner checked by SID on Windows) and administrators — both trusted |
| the OS browser opener | the handoff reply says `opened:false` and the client shows the fresh link in a dialog; "Copy MailArchive link" needs no opener **[CC20]** | n/a |
| `scrub.pending` marker | lost only with `search.db` itself; Rebuild then produces a fresh, clean database | forging `0` needs write access to `search.db` (the owner) |
| `access_policy` stamp | an older binary drops it → one extra walk | forging it suppresses only the tool-owned tree walk on POSIX (the fixed set is tightened every run); on Windows `Posture` runs on every lock-holding run regardless, and the stamp is keyed to the root's identity, so a copied archive is re-checked where it lives **[CC7, CC11]** |
| schedule descriptor (`.mailarchive-schedule.json`) | the schedule's log is not named in the summary | untrusted input: its `log` value is only ever named, never chmod-ed or rewritten (A3, D4) **[CC29]** |
| Windows DACL | an admin resets it → `status` RED `access_open` | administrators are trusted |

No new single point of loss: none of these components holds archive content or the only copy of anything, and admission
does not hang on the OS browser opener alone. Revision 1 claimed this while it did; the opener fallback and "Copy MailArchive
link" make it true **[CC20]**.

## 6. Migration and coexistence

### 6.1 Existing installations

- **Archives (POSIX).** The first lock-holding run after upgrade (export, `graph`, desktop capture, `reindex`, Rebuild,
  `verify -record`, `extract`; read-only verbs never tighten) tightens the fixed set and walks the tool-owned tree once, then
  stamps **[CC11]**. Only what the tool owns changes: other files and folders under `-out`, and nested archives, keep their
  modes **[CC6]**. Files keep their owner; the owner can still read everything (`0600` is owner-read). One log line. An
  archive whose `-out` is the home directory or a volume root is not chmod-ed at the root, its tree pass is skipped, and
  `status` names it **[CC6]**. Until that first run, `status` reads WARN "will be made private on the next run (<time>)",
  not RED **[CC11]**.
- **Archives (Windows).** Existing archives are not rewritten automatically in revision 2: `status` reports an open
  archive RED with the opt-in `mailarchive access -out DIR -restrict` (which saves the original SDDL and has `-restore`), until
  slice A-W ships under OQ-1's ruling and a completed MA-368 drill **[CC8]**. **[ruling OQ-1]** A-W is not built, so the
  opt-in stays the only way an existing archive's DACL changes, and `status` stays RED until it is restricted. A root that is a drive root or the profile folder,
  or that holds non-archive files, is never rewritten; its tool-owned entries are protected one by one **[CC6]**. New archive
  roots created after upgrade are protected from birth, keeping the parent's non-broad grants, so a backup account granted at
  the parent keeps access **[CC7]**.
- **Indexes.** The first capture or `reindex` after upgrade runs the one-time legacy scrub (OQ-6 default; **[ruling OQ-6]**:
  automatic; with slice D, so not before the owner answers ESC-4) on an index
  created before scrub policy 1 — removing the text of messages redacted with earlier versions, which the user believes is
  gone — after the free-space pre-flight, retrying at most weekly when unattended (D1) **[CC34]**. A new or rebuilt index is
  stamped policy 1 and never pays it **[CC33]**. One log line with before/after size.
- **Folder pages** gain the generator meta on their next write; stale pages from earlier redactions are removed by the next
  `reindex` (legacy fingerprint, D3).
- **Schedules.** Installed jobs keep their argv and keep working. The plist `Umask` key and pre-created logs arrive on the
  next `--install`; until then `runlog` tightens the logs each run. A job whose `-token-cache`/`-client-secret-file` is inside
  `-out` keeps running with a WARN (P10); re-installing it is refused until the file is moved. A desktop capture whose archive
  location contains the config directory keeps running with a WARN (`config_in_archive`) **[CC16]**.
- **MailArchive Desktop (MSI upgrade, DC6).** Same shortcuts and arguments, plus a new Start-menu shortcut "Copy MailArchive
  link" **[CC20]**. Instances already running when the MSI upgrades would keep serving unauthenticated on 8097 until sign-out,
  so `Install-RMM.ps1` stops `mailarchive-desktop` processes before `msiexec` (as `Uninstall-RMM.ps1:9` already does),
  `Product.wxs` adds `util:CloseApplication` with TerminateProcess, and a new instance that finds a pre-upgrade one on 8097
  ends it (same account) or tells the user to sign out, warning in the dashboard and `status` **[CC22]**. The reader's port
  becomes ephemeral; a bookmark of `http://127.0.0.1:8099/` stops reaching the desktop reader (it reaches `serve` if that
  runs, which shows its own locked page). The product no longer points users at typed or bookmarked addresses: the locked page
  and the docs say to open MailArchive from its shortcut, because a page reached by a typed address may not be MailArchive
  (§8.4) **[CC23]**; whether the dashboard keeps 8097 at all is OQ-9. **[ruling OQ-9]** It does not: the dashboard takes
  an ephemeral port, as the reader does, so a bookmark of `http://127.0.0.1:8097/` stops reaching it; the shortcut and
  "Copy MailArchive link" are the only ways in; 8097 leaves the RMM runbook; and an unverified listener on 8097 gets the
  adapted notice ("another program is using MailArchive's former address 127.0.0.1:8097 — open MailArchive only from its
  shortcut"). A second account's Startup launch now starts that
  account's own instance instead of exiting "already running". **Packaging deliverables of slice C2 [CC22]:**
  `README-RMM.md` §D/§E (verification by the shortcut and the locked page, the per-session effect of the fix, the
  `access -restrict`/`-restore` fleet script), `DEPLOYMENT.txt`, `README-payload.txt`, `BUNDLE-CONTENTS.txt`, and
  `Cleanup-CurrentUser.ps1`, which also removes the new lock and instance file.
- **Scripts that call `serve`'s HTTP API** (e.g. `curl …/api/search`) stop working: they have no session. The supported
  scripted path is `mailarchive search` (same query grammar, X5/X8). Stated in the README and release notes. A same-account
  `-api-token-file` option is recorded in the operability backlog (§7.2) **[CC25]**.
- **`serve -addr 0.0.0.0`** keeps its warning and now also requires a link per browser (press Enter for each, or run
  `mailarchive serve -link -out DIR` from any terminal of the same account); names other than this machine's IP literals need
  `-allow-host`. A `serve` with no terminal (nohup, launchd, systemd, a detached tmux) gets its links from `serve -link`, and a
  loopback tunnel (`ssh -L 9000:127.0.0.1:8099`) keeps working because any loopback port is accepted **[CC18]**. Both are in
  the release notes.
- **Store directories that are links.** A store directory that is itself a symbolic link (a large mailbox moved to another
  disk) is refused by P5 after the upgrade, so that store's capture exits `1`. The release notes and the refusal text give the
  supported layouts: move the whole archive and point `-out` at it, or make `-out` itself the link **[CC14]**.

### 6.2 Mixed versions

- An **older binary** run against an upgraded archive (e.g. a schedule still pointing at an old `mailarchive.exe`) creates new
  files `0644`/`0755` again; the next new-binary run re-tightens them (fixed set always; tree on export/reindex; the
  dropped stamp makes the next Graph run walk). Between the two runs, those new files are exposed — stated in the release notes.
- An older binary's `reindex` (plain `DELETE`) leaves residue again and sets no `pending`, and the new binary no longer
  guesses from `freelist_count` (D1) **[CC33]**. **Honest limit:** a redaction made with an older binary is only guaranteed
  scrubbed after `reindex -compact` or `reindex -rebuild`; the README says "after a downgrade, redact again with the current
  version, then run `mailarchive reindex -compact`, or run `reindex -rebuild`".
- The older binary ignores the `scrub` table, the `access_policy` field (and drops it on save), the generator meta, the
  instance file and `/s/<P>/` (it serves unauthenticated at `/` again — the regression of a downgrade).
- These hold for a v6 manifest. If recapture-integrity's Q2 bumps the manifest to v7, a v6 binary refuses the v7 manifest
  (`Load` refuses a version above its own), so it does not run against the archive at all: a downgrade past that bump is
  refused, not silent **[CC2]**. **[ruling J2]** Moot: no design bumps the manifest, so it stays version 6 and the points
  above are the whole downgrade story.

### 6.3 Coexistence with parallel work

- **F5 (desktop reader keeps an old `search.db` open).** P11 does not depend on it for plain `reindex` (in-place scrub; open
  readers see the scrubbed file). For `reindex -rebuild` / the dashboard's "Rebuild search index" while a reader is open, the
  claim that the reader stops showing pruned messages is **conditional on F5's fix** (the reader closes or reopens on a change
  of `search.db`'s identity); until then that path's summary says "restart MailArchive Desktop / `serve` to stop showing removed
  messages".
- **F16 (Windows ACLs on secret/token files).** P3's `private_windows.go` is a natural home for F16's primitive; whichever slice
  lands first owns `RestrictNewRoot`/`Posture` and the other reuses it.
- **The extraction workspace (`docs/design-extraction-workspace.md`, revision 2, approved with conditions) [CC2, CC3].**
  The owner has chosen a select-the-source / choose-the-export-destination copy form rather than drag-and-drop (its OW4,
  reaffirmed by the user on 2026-10-09); nothing here reopens that. What revision 1 of this design said about it was wrong,
  and is corrected to what that design specifies:
  - **E0 is this design's C1 + C2 (with C4)**, as its XC23 records: one owner, one session mechanism. The workspace builds
    no session, Host or instance code; every workspace route registers through `localweb.Mux` and uses relative URLs, so
    MA-378 probes it and it works under `/s/<P>/`. Slice C2 is on the workspace's critical path (its E3 depends on E0
    being merged).
  - Having no drag-and-drop, it adds no upload or drop endpoint. It does add a second sign-in surface
    (`/api/m365/signin`), a native folder picker and an "Open folder" action; the account banner and the "sign-in code
    only in a window opened from the shortcut" copy (P15, MA-384) cover its sign-in surface too.
  - **Its writes do not go through `ArchiveFS`.** Its export root is created with an exclusive `os.Mkdir` under the chosen
    destination and opened as its own `*os.Root` (a symlinked destination is allowed; the root is pinned after creation). On
    Unix it sets `0700`/`0600` at creation, which matches P1. **On Windows it inherits the destination's ACL**; whether a
    hand-over root instead gets a protected owner-only DACL is the joint owner ruling OQ-7 (its Q10). P3's protected DACL is
    therefore scoped to archive roots, and `extract -dest` follows the same ruling. **[ruling OQ-7 = J3]** It does not:
    a hand-over root inherits the destination's ACL, P3's protected DACL is scoped to archive roots and the tool's own
    state directories, and R22's text says so (§6.4). Its preview's posture line (its XC32)
    uses this design's `Posture(dir)`, so a destination other accounts can read is named before the copy starts.
  - Once it mounts, a guard bypass also exposes its copy-the-mailbox-to-a-path primitive (§5 failure domains).
- **Recapture integrity (`docs/design-recapture-integrity.md`, proposed) [CC2, CC26, CC35].** It moves verify's
  component-wise `Inspect` into `internal/archivefs`; B1's `ArchiveFS` lives in the same package, so there is one no-follow
  gate. Its §4.9 is the single Reindex decision table: D2 and D5 amend it (the repair row for F48, the classifier, the intent
  rule, and `finishSuperseded` as R4a), and §10 orders slice D after its S2 (MA-361) and S4, or D carries the repair row
  itself. Its v7 manifest (`Superseded` entries, `<stem>-vN.html` names) is handled by D2 and D4 (superseded base stems are
  members' stems), and this design's manifest fields are valid for v6 and v7. **[ruling J2, ESC-2, ESC-3]** There is no
  v7: its `Superseded` entries are additive v6 fields, and its S4 (and with it the `-vN` names) waits for the owner's
  answers to ESC-2 and ESC-3. Both designs change the exporter's prior-file
  prune (`exporter.go:478-490`): recapture removes the immediate delete, and B2's `ArchiveFS` wrapping applies to whatever
  delete remains.
- **Fault isolation (`docs/design-fault-isolation.md`, revision 2, approved with conditions) [CC2, CC13].** B is the
  write-side complement of its read-side source containment. Its FC1 classification of Windows reparse points (a name
  surrogate is a link; a cloud placeholder is the file it represents) is the classification B1 uses, so the two designs
  agree on what a link is.
- **MIME fidelity (`docs/design-mime-fidelity.md`, proposed) [CC2].** It defers C2-2 (the symlinked-folder write) to this
  design; slice B closes it.
- **Existing tests.** `server.New`, `DashboardHandler` and `SetupHandler` stay unwrapped; the guard is applied by the binaries'
  constructors (`localweb.Wrap`), so existing handler tests (MA-81..84, MA-210..213, MA-265..281, MA-302) keep testing the
  inner handlers unchanged. Their muxes become `localweb.Mux` (a recording `ServeMux`), which changes no handler's behaviour
  **[CC15]**. Tests that pin the UI's absolute paths (`/api/search`, `/files/`) are updated to the relative form
  in slice C2 with no assertion removed — a harness change the C2 adversarial pass must confirm is not a weakening.

### 6.4 Catalog and contract changes (operator acceptance required; ids provisional until OQ-10, [CC1])

**[ruling OQ-10 = J1, J7, ESC-4]** The ids below are the allocated ones. Under delegation the new R22–R24, S42–S45 and X12
and their rows are accepted, and so are the R4, R19, X9, X11 and MA-232 amendments and MA-277 and MA-281, each with its
slice. R13's amendment (its classifier and intent rule, and its added deletion cases) is escalated to the owner as ESC-4.

- **R22 — Owner-only at rest.** Every file the tool creates in the archive, in `extract -dest` and in its own state/log
  locations is readable only by the creating account (POSIX `0600` files, `0700` directories; Windows: a protected DACL
  granting the creating account's TokenUser, SYSTEM and Administrators on archive roots and state directories it creates —
  hand-over roots on Windows follow OQ-7 **[CC3, CC7]**); an existing archive's tool-owned entries are tightened on its next
  run (on Windows, existing archives per slice A-W) **[CC6, CC8]**; where the file system cannot restrict access, or another
  account owns an archive file, `status` says so **[CC9, CC10]**. **[ruling OQ-7 = J3, OQ-1]** In the text that lands,
  "hand-over roots on Windows follow OQ-7" reads "hand-over roots (`extract -dest`, workspace exports) on Windows inherit
  the destination's ACL, and the tool names that posture before its first write", and "existing archives per slice A-W"
  reads "an existing archive changes only through the opt-in `mailarchive access -restrict`, and `status` stays RED while it
  is open".
- **R23 — Local web surfaces admit only their launching account.** `serve`, `setup`, the desktop dashboard and reader validate
  `Host` on every request and admit only a browser holding the per-launch session obtained through a one-time link the process
  opened or printed; refused requests get a legible page with no archive data; a desktop launch reaches only its own account's
  verified instance. Not a defence against a process running as the same account.
- **R24 — Redaction removes what the archive holds.** After `reindex` redacts a message whose files the operator deleted
  (an operator's deletion, classified as in D2, never another prune reason **[CC27]**), none of the message's own records and
  none of the files the tool derived from it inside the archive folder contains its text or metadata — the values unique to
  it — as logical content or as raw bytes inside the file **[CC32]**; anything not completed is itemized, stays findable, and
  `reindex` exits `1` **[CC28]**; what lies outside that boundary is stated, including other messages the operator kept that
  quote, forward or duplicate it, and logs outside the archive folder **[CC29, CC32]**.
- **Amended:** R4 (+ "no write or delete follows a symbolic link at or below a store directory"), R13 (+ the scrub, stale-page and
  empty-folder removal, stale-snapshot sweep, the manifest-driven gone set; **[ruling ESC-4]**: escalated with the classifier and
  the intent rule, and not landed until the owner answers), R19 (`serve`: Host + session + `/files/` allow-list),
  MA-232 (exit `1` and "not removed" listing), MA-281 (reconnect only to this account's verified instance), MA-277 (reader on an
  ephemeral port). **X9** amended: the boundary now covers other accounts on the same computer and every request (not only POSTs);
  **X12** new: *locked, not blank* — a refused browser request to a local surface gets a page naming what, why and the
  remediation, and no archive data; API callers get JSON (reconciled at acceptance with the extraction workspace's proposed
  X12, "mailbox-content reads are POST-only": compatible rules, two ids **[CC1]**; **[ruling J1]**: the workspace's rule is
  X13). **X11**: the reconnect sentence becomes "a shortcut click surfaces *this
  account's* dashboard".
- **Scenarios:** S42 (another account on the same computer: files, loopback HTTP, the per-machine desktop's ports), S43 (a rebinding
  page, a hostile email link to a loopback listener, a squatter on the dashboard port), S44 (an operator redacts and the archive is
  later inspected byte-for-byte or handed over), S45 (a symlink or a credential file placed inside the archive).

### 6.5 Rollback of the rollout

Each slice is independently revertible. Reverting a binary leaves tightened modes and DACLs in place (harmless to the owner) and the
`scrub` table (ignored). A protected DACL also stops later changes to the parent's ACL (a backup account an MSP adds at `D:\Data` by
GPO) from reaching the archive; that is inherent to protection and is stated in `README-RMM.md` **[CC7]**. New roots keep the parent's
non-broad grants from birth, so a backup account granted at the parent is not locked out **[CC7]**. Existing Windows archives change
only through the opt-in `mailarchive access -out DIR -restrict`, which first saves the root's original SDDL to
`<UserConfigDir>/mailarchive/acl-backup/<hash(out)>.sddl`; `mailarchive access -out DIR -restore` re-applies it, and README-RMM carries
a fleet script for both. That gives a per-machine and fleet-level rollback of the one DACL change that is not otherwise reversible
**[CC8]**. `status -json` lists the principals that have access, so an MSP can audit them **[CC7]**.

## 7. Operator walk

Step = one action the user takes (a click, a command, a paste). Type per the friction review: I = security-inherent, II =
design-choice, III = buildable-away.

### 7.1 Home user, Windows, MailArchive Desktop, one account

| Task | Today | After | Δ / type |
|---|---|---|---|
| Open the dashboard (shortcut) | 1 | 1 (handoff opens the session) | 0 |
| Open archive / Go back in time | 1 | 1 (cookie covers the reader's port) | 0 |
| Bookmark of `127.0.0.1:8097` after a restart | 1 | 2 (locked page → shortcut); no longer endorsed — the locked page and the docs say to open MailArchive from its shortcut, since a page reached by a typed address may not be MailArchive (§8.4, OQ-9) **[CC23]**. **[ruling OQ-9]** With the ephemeral dashboard port the bookmark reaches no MailArchive page (nothing, or another program, which the dashboard's former-address notice names); the user opens the shortcut: still 2 | +1, II (OQ-5 removes it; **[ruling OQ-5]**: later) |
| The default browser cannot be opened (broken association, blocked opener) | 1 (type the URL) | 2 (the client shows the link in a dialog → paste), or "Copy MailArchive link" from the Start menu **[CC20]** | +1, I |
| A second, non-default browser | 1 | 3 ("Use another browser…" → copy → paste) | +2, II |
| Redact a message | delete the files + `mailarchive reindex -out …` | same; on a policy-1 index no full rewrite of `search.db` **[CC33]**; summary itemizes; a whole folder, or more than 25 messages, also needs `-redact` (OQ-8) **[CC27]**; **[ruling ESC-4]**: as today until the owner answers | 0 (+1 above the intent threshold, II) |

**Standing cost [CC33].** `secure_delete` and FTS5 `secure-delete` add write and WAL volume to every capture (an indicative
scratch run: about +25 % `-wal`); the measured figure is stated here when slice D is built. The one-time legacy scrub costs one
`VACUUM` of the index — minutes on a large index, about twice the index size free on the archive volume and once more in the temp
directory — once per archive created before this policy, and only after its free-space pre-flight passes (D1).

### 7.2 macOS user, `mailarchive serve`

| Task | Today | After | Δ / type |
|---|---|---|---|
| Start and open the reader | 2 (run, click URL) | 1 (run; browser opens) | −1 |
| New tab while running | 1 (type URL) | 1 (address-bar history completes the session URL) or 2 (locked page → Enter in terminal → click) | 0..+1, II |
| Next day | 2 (run, click) | 1 | −1 |
| Script calling the HTTP API | 1 | use `mailarchive search` (no JSON envelope, paging or go-back) | migration, **II** — a same-account `-api-token-file` (a `0600` file the script reads, consistent with DC4) would remove it; recorded in the operability backlog **[CC25]** |

### 7.2a Remote, tunnelled and detached `serve` [CC18]

| Task | Today | After | Δ / type |
|---|---|---|---|
| Read an archive on a home server through `ssh -L 9000:127.0.0.1:8099` | 1 (open the forwarded URL) | 2 (run `mailarchive serve -link -out DIR` in an ssh session; open the link through the forwarded port — any loopback port is accepted) | +1, I |
| `serve` under nohup, launchd, systemd or a detached tmux; a new browser next day | 1 (type the URL) | 1 (`mailarchive serve -link -out DIR`, then open the link); no restart | 0 |
| `serve &` in a terminal | works | works; Enter prints a link only after `fg`, and the server never stops on SIGTTIN | 0 |
| `serve` in a headless SSH session | `xdg-open` may start a text browser | no auto-open without a display; the link is printed | 0 |

### 7.3 A second account on the same computer (family Mac, shift PC, RDS)

| Task | Today | After |
|---|---|---|
| Copy the first account's `search.db` | works (C2-1) | "Permission denied" (POSIX; Windows per P3/OQ-1; **[ruling OQ-1]**: an existing Windows archive once `access -restrict` has run) |
| Open `127.0.0.1:8097` | the first account's dashboard (F3) | the locked page (no data); **[ruling OQ-9]**: no upgraded dashboard listens on 8097 at all, and any account's dashboard port shows the locked page |
| Use MailArchive Desktop at all (Windows) | blocked, "already running" | own instance on its own port, 1 click |

### 7.4 MSP admin (RMM, per-machine MSI, RDS host)

Deploy: the RMM install script stops running `mailarchive-desktop` processes before `msiexec` (shipped in `Install-RMM.ps1`, so 0 new
admin steps), and the fix takes effect in each session at once rather than at the next sign-out **[CC22]**. Verification changes: the
runbook's "open http://127.0.0.1:8097/" step becomes "open MailArchive Desktop from its shortcut; typing the address shows the locked
page" (**[ruling OQ-9]**: 8097 leaves the runbook; README-RMM verifies the install by the shortcut and the locked page), plus 1 lab step: sign in as two test accounts and confirm each sees its own dashboard, including sessions that were running during
the upgrade (MA-377) **[CC22]**. Backup agents running as SYSTEM keep access (P3 keeps SYSTEM). An agent running as a dedicated non-admin
account keeps access if granted explicitly or by inheritance from a non-broad principal at the parent — on new roots too, because a new
root's protected DACL starts from the parent's non-broad entries **[CC7]**. Later changes at the parent no longer reach a protected
archive; `status -json` lists the principals with access for audit **[CC7]**. Existing archives change only by `access -restrict` until
slice A-W (OQ-1) **[CC8]**. **[ruling OQ-1]** A-W is not built, so `access -restrict` (and README-RMM's fleet script for it) stays the
only way.

### 7.5 Inheritor opening the archive cold (copy on a drive, no tool)

| Situation | After |
|---|---|
| macOS external APFS/HFS+ drive | opens directly (macOS ignores ownership on external volumes by default) — 1 step (open `index.html`); for the same reason, while the drive is mounted that way on the owner's Mac every local account can read it, and `status` there says "this drive ignores ownership" (WARN), never GREEN **[CC9]** |
| Windows NTFS drive from another PC, inheritor is an administrator | Explorer: "You don't currently have permission… Continue" (UAC) — +1, I |
| Windows NTFS drive from another PC, inheritor is **not** an administrator | cannot open a protected archive. Remedy: an administrator takes ownership (`takeown /f <dir> /r` then `icacls <dir> /grant <you>:F /t`), or, before handing over, the owner runs the README's `icacls` step to grant the heir — +2, I **[CC25]** |
| Linux ext4 drive, different user id | `sudo chown -R "$(id -un)" /media/…/archive` — +1, I (today the pages are already `0600`, so this step already exists for the mail itself) |
| Wants search | `mailarchive serve -out .` — 1 (browser opens itself) |

**Recipients of a hand-over copy** (`extract -dest`, a workspace export; Windows posture per OQ-7 as revision 2 describes it) **[CC3]**
(**[ruling OQ-7 = J3]**: as described):

| Situation | After |
|---|---|
| NTFS USB drive, recipient on another PC | the copy inherited the drive folder's ACL: it opens directly |
| UNC share `\\server\legal\case` | the copy inherited the share folder's ACL: the share's own permissions decide, as for any file copied there |
| exFAT drive | no ACLs or modes: it opens anywhere; the posture line said "permissions can't be restricted on this drive" before the copy |
| POSIX destination, recipient with another user id | `0700`/`0600`, as today's pages: `sudo chown -R "$(id -un)" <copy>` — +1, I |

### 7.6 Refusal and summary texts (what / why / remediation, X2/X12)

- **Locked page, desktop:** "This MailArchive Desktop page has to be opened from the app. *Why:* each launch protects your dashboard and
  archive with a private link, so other accounts on this computer and web pages cannot open them. *Do this:* open MailArchive Desktop
  from its Start-menu or Desktop shortcut. If your browser does not open, use "Copy MailArchive link" in the Start menu." **[CC20]**
- **Locked page, `serve`/`setup`:** "This archive reader needs its private link. *Why:* … (as above). *Do this:* use the link
  `mailarchive serve` printed in its terminal, press Enter there for a new one, run `mailarchive serve -link -out <folder>` from any
  terminal of your account, or start `mailarchive serve -out <folder>` again." **[CC18]**
- **Link from an earlier session** (unknown token) **[CC19]:** "This link is from an earlier session. *Why:* each link works only while
  the program that made it runs. *Do this:* get a new one (shortcut / press Enter in the `serve` terminal / `mailarchive serve -link`)."
- **Expired link [CC19]:** "This link has expired. *Why:* a link works for a short time after it is made. *Do this:* get a new one (as
  above)."
- **Used link, another browser** (used, and this browser did not redeem it) **[CC19]:** "This link has already been used. *Why:* each link
  opens one browser once. *Do this:* get a new one (as above). If you did not open it yourself, another account on this computer may have:
  stop the reader (Ctrl-C / quit MailArchive Desktop) and start it again — that ends every session — and paste new links into the
  address bar instead of clicking them." A used link presented again by the browser that redeemed it simply continues.
- **Host refusal:** `refused: this page is served only as http://127.0.0.1:<port>/ — open it from MailArchive` (plain text, 403).
- **Desktop, explicit port busy:** "mailarchive-desktop: -addr 127.0.0.1:8097 is in use by another program. *Why:* the dashboard needs
  its own port. *Do this:* leave -addr unset (any free port is used) or choose a free port." (shown in a dialog and on stderr **[CC20]**)
- **Desktop, wedged instance:** "MailArchive Desktop is already running for your account but is not responding. *Do this:* quit it in
  Task Manager (pid <pid from the lock>; or `kill <pid>`), then start it again." (dialog and stderr **[CC20, CC21]**)
- **Desktop, another configuration or session [CC21]:** "MailArchive Desktop is already running for your account with another archive or
  configuration (<path>) / in another sign-in session. *Do this:* use that window, or quit it first."
- **Desktop, the browser did not open [CC20]:** a dialog: "Your browser did not open. Copy this link into your browser: <link> [Copy]".
- **Desktop, 8097 held by another program [CC23; ruling OQ-9]:** in the dashboard header and in `status`: "Another program is using
  MailArchive's former address 127.0.0.1:8097. Open MailArchive only from its shortcut — a page you reach by typing that address or a
  bookmark may not be MailArchive." (Revision 2 said "usual address"; under OQ-9 the dashboard no longer lives there, so the notice
  names it as the former one.)
- **Desktop, pre-upgrade instance [CC22]:** "An older MailArchive Desktop is still running (in this session or another account's). *Do
  this:* sign out and back in, or restart the computer, to finish the upgrade."
- **P5 link:** "mailarchive: refusing to write into <out>/support/Drafts: it is a symbolic link, not a folder. *Why:* a link inside the
  archive could send the backup's writes somewhere outside it. *Do this:* replace the link with a real folder (or remove it) and run
  again. If you moved this folder to another drive for space, move the whole archive there instead and point -out at it, or make -out
  itself the link." (exit 1) **[CC14]**
- **P10:** "mailarchive: the token cache <path> is inside the archive folder <out>. *Why:* the archive is meant to be copied, synced and
  browsed, so a sign-in kept there travels with it. *Do this:* move it outside the archive — omit -token-cache to use the default
  <default path>." (exit 1; with -unattended: a warning and status WARN)
- **Desktop capture, settings inside the archive [CC16]:** a warning in `desktop-capture.log` and the last-run record, and `status` WARN
  `config_in_archive`: "Your MailArchive settings and sign-in are stored inside the archive folder <out>. *Why:* the archive is meant to be
  copied and synced. *Do this:* choose an archive location that does not contain <config dir> (Settings)." The capture still runs.
- **Another account's archive (serve/search):** "mailarchive: search.db is readable only by the account that made this archive
  (<owner>). *Do this:* run `mailarchive` as that account, or take ownership of the folder." (exit 1)
- **`reindex` redaction summary (exit 0):**
  ```
  removed 2 message(s) from the archive:
    files deleted (6), search entries and their text in search.db (scrubbed: 48.1 MB -> 47.9 MB, 12 s),
    folder pages (3 rewritten, 1 removed), empty folders (1), manifest records, go-back history,
    reports, log lines (4 in mailarchive.log)
  not covered — the tool cannot reach these:
    - your mailbox / mail file: if a message is still there, the next capture archives it again —
      delete it at the source first   [shown when the store is a live mailbox or the source file exists]
    - other messages you kept that quote, forward, reply to or duplicate a removed one — search for them
      before you hand the archive over   [CC32]
    - backups and copies (Time Machine, File History, OneDrive/NAS versions, extract output, copies you made)
    - disk blocks released by deleting or shrinking files (SSD, snapshots, shadow copies); use full-disk encryption
    - search indexers, thumbnail caches, swap; your browser's history; logs outside this folder — including
      your schedule's log <path>, which was not changed   [CC29; shown when the schedule's log is outside]
  ```
- **`reindex` repair, kept items and intent [CC26, CC27]:** `repaired 1 search entry (its file had been renamed by a later capture;
  nothing removed)`; `not indexed — kept: <path> (<reason>)` (exit 1); above the intent threshold (OQ-8; **[ruling ESC-4]**: these
  texts ship with slice D, after the owner answers): `12 messages' files are missing
  from support/Personal/ (the whole folder) — if you moved or renamed them, put them back; to remove them for good run mailarchive reindex
  -redact -out <out>` (exit 1, nothing removed).
- **`reindex` incomplete (exit 1):** the same summary headed "redaction INCOMPLETE — still present:" followed by each item not completed
  (a file a link blocked, a stale page that could not be deleted, a log that could not be rewritten, "text may remain in search.db: free
  about 96 MB on /Volumes/Archive and 48 MB in /var/folders/…, then run `mailarchive reindex -out …` again").
- **`status` lines:** `Access: private (only you)` GREEN · `Access: will be made private on the next run (Sun 03:00)` WARN (before the
  first stamped run) **[CC11]** · `Access: OPEN — search.db is readable by other accounts (mode 0644); run any capture or reindex` RED ·
  `Access: OPEN — this archive was created before MailArchive protected new archives; run mailarchive access -out <out> -restrict` RED
  (Windows, until slice A-W) **[CC8]** (**[ruling OQ-1]**: A-W is not built, so until the archive is restricted) · `Access: <file> belongs to <owner>; run sudo chown -R <you> <out>` RED **[CC9, CC10]** · `Access:
  this account cannot change who may read <dir>; an administrator can run: icacls …` WARN **[CC9]** · `Access: this drive cannot restrict
  access (exFAT)` WARN · `Access: this drive ignores ownership (Finder ▸ Get Info ▸ uncheck Ignore ownership)` WARN **[CC9]** · `Redaction:
  unfinished — run mailarchive reindex -out … to finish removing deleted messages' text` WARN · `Index: legacy scrub pending` / `Index:
  legacy scrub failed <date>: needs 41 GB free on D: — run mailarchive reindex` WARN **[CC34]**.

### 7.7 GUI user (`mailarchive-gui`) [CC6]

The GUI uses the folder the OS picker returns directly as `-out` (`cmd/mailarchive-gui/main.go:397-398`), and no export refuses a
non-empty folder, so `-out` is often a drive root (`D:\`), the Documents folder or a shared folder.

| Situation | After |
|---|---|
| `-out` = `D:\` (Windows) | the drive root's ACL is never changed; the archive's own files and store folders are protected one by one; `status` is RED "the archive shares a folder with other files — move it into its own folder" while any of them stays open |
| `-out` = `~/Documents` (POSIX) | only the archive's own files and store folders are tightened; the user's other documents keep their modes |
| Choosing a new location | the picker proposes `<chosen>/MailArchive` (one click to accept), as the extraction workspace does with its export directory — 0 extra steps |

## 8. Security

### 8.1 Trust boundaries

1. **The account boundary on one computer** (new, R22/R23): files (modes/DACL), loopback HTTP (Host + session), the desktop's port and
   handoff (per-account lock, instance file, HMAC). Trusted: the owner's account and its processes, administrators/root.
2. **The browser/web boundary** (strengthened, R19/R23): Host allow-list (rebinding), path-scoped `Strict` session (cross-site and
   co-account navigation), CSRF + Origin (state changes), CSP/no-referrer/no-store/CORP.
3. **The archive-content boundary** (unchanged, R19): archived mail is hostile; pages are inert; the reader serves only allow-listed
   exported files.
4. **The archive-tree boundary** (restored, R4): nothing below a store directory is trusted to be a real directory; writes and deletes
   never follow links.

### 8.2 Untrusted inputs

`Host`, `Cookie` (the session and the `mailarchive_open` marker), `Origin`, the `t` query value, `/_handoff` bodies (≤ 1 KiB JSON), the
`/api/browser-link` request; every path a browser requests under `/files/`; the archive tree's entries (links, names, modes, **owners** — a
foreign-owned file is replaced or reported, never written into **[CC10]**) and the manifest's paths (`Path`, `AlsoFiles` — already
untrusted, MA-232); **the schedule descriptor `.mailarchive-schedule.json`**, which anyone who can write the archive could edit — its `log`
field is only ever named, never chmod-ed or rewritten **[CC29]**; Message-IDs and subjects used as match needles (sender-chosen: matched
only in the bracketed form and above minimum lengths, D4 **[CC31]**); the contents of logs being rewritten (arbitrary bytes, long lines);
the instance files (writable by the account only — parsed defensively anyway: unknown version or malformed → treated as absent); SQLite's
behaviour under `SQLITE_FULL`/`SQLITE_BUSY`.

### 8.3 New surfaces

| Surface | Reachable by | Can do | Guarded by |
|---|---|---|---|
| `GET /_open?t=` | anyone who can reach loopback | turn a valid unused token into a session | Host; 256-bit one-time token, 10 min |
| `POST /_handoff` (the dashboard listener; `serve`'s listener for `serve -link`) | anyone who can reach loopback | make the instance open a browser in its own session, or return a fresh link to a caller that proves `K` | Host; HMAC with `K` over the request and over the reply, including `opened` and the link; 1/s, "busy" beyond **[CC18, CC20]** |
| `POST /api/browser-link` | a session | mint a one-time link | session + CSRF + Origin |
| locked page | anyone | nothing (static text) | Host |
| instance file, per-account lock | the account, administrators | handoff | `0600` file in an account-only, non-purgeable directory; owner checked by SID on Windows **[CC21]** |
| `mailarchive-desktop -copy-link`, `mailarchive serve -link` | the account (it reads the `0600` instance file) | obtain a fresh one-time link | the instance file's `K` **[CC18, CC20]** |
| marker cookie `mailarchive_open` | a browser that redeemed a link | re-enter silently with its own used link | random, stored server-side, never derived from the token, `Path=/_open` **[CC19]** |
| `mailarchive access -restrict` / `-restore` | the operator | rewrite or restore an existing archive root's DACL | explicit command; SDDL saved first; effective-rights check; never-list **[CC7, CC8]** |
| `reindex -redact` | the operator | accept a gone set above the intent threshold | explicit flag; never accepts a blocked member or another prune reason **[CC27]** |
| page-cleanup deletes (D3) | `reindex`/Rebuild | delete tool folder pages, remove empty directories | only folders that held gone members, never one with a present recorded file, never a nested archive; name + generator/fingerprint test, `ArchiveFS`, lock **[CC6, CC27]** |
| log rewrite (D4) | `reindex`/Rebuild | replace matching lines in the tool's own logs inside `-out`, in place | fixed file list inside `-out` only, `ArchiveFS`, lock, bracketed and minimum-length needles **[CC29, CC30, CC31]** |
| `-allow-host` | the operator | widen Host for a non-loopback `serve` | explicit flag; session still required |

### 8.4 Attacker personas

| Persona | Attempt | Outcome |
|---|---|---|
| Co-account (POSIX) | read `search.db`, logs, list folders | denied (P1/P2); unless the drive cannot hold modes (P4 WARN); a file the co-account owned before the upgrade is replaced by an owner-owned copy before the next write, or reported RED `access_not_owner` **[CC10]** |
| Co-account (Windows) | read an archive under `C:\` | denied for archives the tool created (P3); an existing archive is reported RED until `access -restrict` or slice A-W (OQ-1) **[CC8]** (**[ruling OQ-1]**: A-W is not built, so until `access -restrict`); a child folder the co-account owns keeps its implicit `WRITE_DAC` and is reported RED with the `icacls /setowner` remedy, which needs an administrator — **residual** (§8.6) **[CC10]** |
| Co-account | `curl 127.0.0.1:8097/`, `:8099/api/search` | locked page / 401 JSON, no data (P7) (**[ruling OQ-9]**: on the dashboard's and reader's ephemeral ports; no upgraded dashboard listens on 8097) |
| Co-account | squat 8097 before the user's launch | handshake fails; the user's instance starts elsewhere; nothing opened at the squatter (P8); the user's dashboard and `status` say another program holds 8097 **[CC23]**. **[ruling OQ-9]** The instance always starts on an ephemeral port, and the notice names 8097 as MailArchive's former address |
| Co-account (shared PC, first to sign in) | squat 8097 with a look-alike dashboard that requests its own Microsoft device code, and wait for a later account to type the address or use a bookmark | **residual** — the real instance never opens the squatter (P8) and says 8097 is held; the product no longer endorses typed or bookmarked addresses, and the sign-in copy says MailArchive shows a sign-in code only in a window opened from its shortcut. A user who still types the address can be phished, because the look-alike controls its own HTML (the account banner can be forged). Neither sessions nor OQ-5 close it; OQ-9 (an ephemeral dashboard port) narrows it **[CC23]**. **[ruling OQ-9]** The ephemeral port is taken: no MailArchive page lives at 8097 any more, and the former-address notice keeps a squatter there visible. The residual is narrowed, not closed, and stays an open gap, not accepted |
| Co-account | listener on `127.0.0.1:9000` + email link the user clicks in the reader | the click carries no session cookie (path scope) and no Referer (P7) |
| Co-account | login-CSRF: lure the user to `127.0.0.1:<B's port>/_open?t=<B's token>` so the user works inside B's dashboard (e.g. device sign-in into B's instance) | **residual** — possible today in a simpler form (and today the user's own shortcut can open B's squatter, which P8 closes); mitigated by a banner on the dashboard naming the OS account it runs as and the device-sign-in box saying "this sign-in will be saved for <account>"; closed only by OQ-5 (**[ruling OQ-5]**: later, so it stays an open gap, not accepted) |
| Co-account (Linux, macOS) | poll `/proc/*/cmdline` (Linux) or the setuid `ps -axww` (macOS) for `_open?t=` and redeem first | wins a race the user then sees ("already used" in a browser without its marker, a second "session opened" notice); remedy in the page; the opener token lives about 60 s and the printed link is a separate token **[CC5, CC19]**; hardening: mount `/proc` with `hidepid=2` (Linux); on macOS the opener passes the URL on stdin (lab-checked), and the argv path has no OS hardening; Windows standard accounts cannot read other accounts' command lines |
| Co-account with write access to a shared archive folder | plant `support/Drafts -> /etc/…` | the run refuses and names the link (P5); on POSIX P1 removes that write access for archives the tool created |
| Co-account with write access, or the sender of a handed-over archive | point the descriptor's `log` at the owner's `~/.ssh/authorized_keys` and plant a row-less record whose Message-ID matches its lines | nothing: the descriptor's `log` is never chmod-ed or rewritten, and a short Message-ID never matches (D4) **[CC29, CC31]** |
| Co-account with write access to the archive root | swap `search.db` or the run log for a link between the check and the open | redirected anywhere until the root is private — P5's split label states it **[CC13]** |
| Co-account with packet capture | sniff the session cookie and `/s/<P>/` off loopback | possible only where a capture driver grants non-admin capture (Npcap without admin-only mode, ChmodBPF, a `wireshark` group) — **conditional**, stated at P7; OQ-5 would close it **[CC4]** (**[ruling OQ-5]**: later; open, not accepted) |
| Remote web page | DNS rebinding to 127.0.0.1 | 403 on Host (P6); no cookie for the attacker's name either |
| Remote web page | CSRF POST to the dashboard | no session cookie (`Strict`), no CSRF token, Origin refused |
| Hostile sender | content in the archive | inert (R19); links cannot capture the session (P7) |
| Recipient of a redacted archive | `strings search.db`, grep every file | nothing of the redacted message's own records or derived files (P11/P12); kept messages that quote or forward it still hold those bytes, and the summary said so (P14) **[CC32]**; backups/copies made earlier are outside (P14) |
| Same-account malware, administrator, forensic examiner | anything | not defended (§3.2–3.3) |

### 8.5 What an attacker reads off the wire

Loopback traffic is visible only to administrators/root **while no packet-capture driver grants non-admin capture** — Npcap installed
without its "restrict to administrators" option (it supports loopback capture), macOS's ChmodBPF opening `/dev/bpf*` to a group, a Linux
distribution's `wireshark` group for `dumpcap`. Where one does, a co-account can read the session cookie and `/s/<P>/` off loopback and
replay them; OQ-5 would close this **[CC4]** (**[ruling OQ-5]**: reconsidered after C2 ships; until then an open gap, not
accepted). A non-loopback `serve` sends the session cookie and the archive in clear text
over the LAN (unchanged non-goal; the warning stays and now says so).

### 8.6 Acknowledged limits

macOS inherited ACLs are not inspected (§3.5); the legacy scrub's working copy transits SQLite's temp directory unless it can be pinned to
the archive volume (surviving content only, §4.D1); the path-based opens (`search.db` and its siblings, the run log) are check-then-use,
and a writer of the root can redirect them anywhere until the root is private (P5's split label) **[CC13]**; on Windows, a child folder
another account owns keeps that account's implicit `WRITE_DAC` until an administrator changes its owner — the tool reports it RED with the
remedy but cannot fix it without one (an automatic re-home is optional and lab-gated in MA-368; its lab part is MA-500 **[ruling
J1]**) **[CC10]**; diagnostic lines another process appends while a log is rewritten in place can be lost (§5) **[CC30]**; a look-alike
on a squatted 8097 can phish a user who types the address (§8.4) **[CC23]** (**[ruling OQ-9]**: narrowed by the ephemeral dashboard
port and kept visible by the former-address notice; an open gap, not accepted).

## 9. Test plan — proposed rows MA-365..MA-384

Every refusal row proves its positive twin on a healthy fixture first and asserts the side effect is ABSENT with `assure.Refused`; every
row cites its covers ids; prove-fail = mutate the fix, watch the row go red for the right reason, revert, record in the commit.
Revision 2's new cases join the existing rows, so the range stays MA-365..MA-384. Covers cells cite provisional ids, re-checked when the
operator allocates them (OQ-10) **[CC1]**. Rows that assert POSIX modes are GOOS-gated; the CI matrix runs ubuntu, windows and macos
**[CC4]**. MA-368 carries two tiers: at catalog-first time its U part keeps MA-368 and its L part is entered as a separate pending row
whose id the operator allocates with OQ-10 **[CC4]**. **[ruling OQ-10 = J1]** The Covers cells cite the allocated ids, and MA-368's L
part is **MA-500** (tier L, pending), written below as its own row. **[ruling ESC-4]** MA-379..383 are slice D's rows: they land with
D's catalog-first PR, after the owner answers ESC-4, and MA-380's classifier and intent cases and MA-383's `-redact` text follow that
answer.

| ID | Tier | Asserts | Covers | Prove-fail plan |
|---|---|---|---|---|
| MA-365 | U | Under umask `000` and `022`, after each of `export -mode full`, `graph` (fake server), desktop capture, `reindex`, `reindex -rebuild`, `extract -dest`, `schedule --install` (descriptor/log pre-create), every directory under `-out`/`-dest` is `0700` and every file `0600`, including `search.db`, `-wal`, `-shm`, history, run log, `.1`, `.stderr.log`, snapshot; walk is exhaustive (fails on any entry it cannot classify); POSIX only (GOOS-gated) **[CC4]**; repeated with a symlinked `-out` **[CC11]** | R22, S42 | revert A2 (let SQLite create the db) → `search.db 0644` red; revert `runlog` to `0o644` → red; revert one `MkdirAll` to `0o755` → red |
| MA-366 | U | A legacy fixture (`search.db`/`-wal 0644`, logs/history `0644`, dirs `0755`) becomes owner-only after one run of each verb, `extract` included; each verb walks the tool-owned tree once and stamps `access_policy`, a second run does not walk (counted) but still fixes the fixed set, and a Graph run whose capture fails still stamps **[CC11]**; `-out` = a fake home holding a non-archive `0644` file, a non-archive directory and a nested archive: the home root is not chmod-ed, the tree pass is skipped, all three keep their modes, and it is reported **[CC6]**; an injected EPERM records `access_not_owner` naming the file and its owner, a no-op chmod and an injected `statfs` `MNT_IGNORE_OWNERSHIP` record `access_unenforceable`, each with one warning and run exit 0, and a later healthy pass clears them **[CC9]**; a foreign-owned `search.db` (injected owner) is replaced by an owner-owned copy before the open, and a foreign-owned root makes the run refuse to write **[CC10]**; POSIX only (GOOS-gated) | R22, R5, S42 | remove the tightening call → legacy modes survive, red; remove the home-dir guard → home chmod-ed, red; walk all of `-out` → the non-archive file is chmod-ed, red; latch the reason → it never clears, red |
| MA-367 | U | `status` `Access`: GREEN on a private archive; RED `access_open` naming `search.db` and the remedy when it is `0644` after a stamped run, and WARN "will be made private on the next run" before one **[CC11]**; WARN `access_unenforceable`, RED `access_not_owner` and WARN `access_no_permission` from the last-run reasons **[CC9]**; WARN `links_in_archive` naming the first link (slice B) **[CC12]**; a symlinked `-out` reads GREEN when private **[CC11]**; `status -json` reason codes; status writes nothing; POSIX-mode cases GOOS-gated **[CC4]** | R18, R22, X6, S42 | make Gather skip `search.db` → RED case reads GREEN, red |
| MA-368 | U | **U part (windows-latest) [CC4]:** a new `-out` gets a protected DACL of the parent's non-broad inheritable entries plus TokenUser/SYSTEM/Administrators full control — a parent-granted backup account kept, Users/Authenticated Users absent **[CC7]**; the effective-rights check refuses a DACL that would drop TokenUser's full control **[CC7]**; a drive-root-like `-out` and an `-out` holding a non-archive sibling keep their root ACL while the tool-owned entries are protected individually **[CC6]**; `access -restrict` saves the SDDL and `-restore` restores it byte-identically **[CC8]**; `Posture` flags a child with an explicit broad ACE and a child with a foreign owner **[CC7, CC10]**; a copied archive (a new root identity) is re-checked **[CC7]**. The L part is MA-500 **[ruling J1]** | R22, S42 | write the DACL without the parent's entries → backup account missing, red; skip the effective-rights check → the self-lockout fixture loses access, red |
| MA-500 | L | **MA-368's L part — pending [ruling J1]**, validated on Windows 11 by hand: a second account cannot open `search.db`; an existing archive under `C:\` loses its Users/Authenticated Users ACEs after `access -restrict` and keeps an explicit backup-account ACE; an Administrators-owned folder where the user's access comes only through a broad ACE is not rewritten; FAT/exFAT reports unenforceable; the drill gates slice A-W (**[ruling OQ-1]**: A-W is not built, so the drill runs as slice A's Windows drill) | R22, S42 | n/a (lab): the drill records the before/after `icacls` output |
| MA-369 | U | `export -mode full`, incremental, and `graph` against an archive where a folder (and, separately, a store dir) is a symlink to an outside dir: exit 1 naming the link, the outside dir byte-identical (NoSideEffect), other stores still exported; positive twin: `-out` itself a symlink exports normally; an in-root concurrent swap (fault-injected between `Lstat` and `Mkdir`) cannot write outside; a junction (windows-latest) is refused like a symlink, while an injected cloud-placeholder classification (a `ModeIrregular` folder and file whose reparse tag is not a name surrogate) is written through, not refused **[CC13]** | R4, R12, S45 | replace `ArchiveFS.MkdirAll` with `os.MkdirAll` → outside `index.html` replaced, red |
| MA-370 | U | `reindex` with a symlinked folder and a tampered `also_files` through it: the outside `.html/.eml/.zip` survive, the summary lists "not removed", exit 1; the prior-file prune refuses through a link; `pages.Generate` never writes through a link; a symlink planted at `.mailarchive-history.jsonl` and at `search.db` is refused naming the file; positive twin: the same redaction on a link-free archive removes everything, exit 0; the existence check meets the symlinked folder as *blocked*, so its records and rows are untouched — not pruned, not redacted **[CC27]**; the same with a junction on windows-latest **[CC13]** | R4, R13, S45 | revert `deleteMessageFiles` to `os.Remove` → outside files deleted, red (MA-232's PoC 2) |
| MA-371 | U | For `serve`, the desktop reader, the dashboard and `setup`, on every registered route and for GET/POST/HEAD: Host `attacker.example:<p>`, empty, `127.0.0.1.`, userinfo → 403 with none of: subject text, paths, CSRF token; positive twins `127.0.0.1:<p>`, `localhost:<p>`, `[::1]:<p>` and `127.0.0.1:<another port>` (a forwarded port) reach the session check **[CC18]**; a `0.0.0.0` `serve` accepts an interface IP literal and an `-allow-host` name, refuses others | R19, R23, X9, S43 | drop the Host check from `Wrap` → rebinding GET of `/s/<P>/api/search` (with cookie) succeeds, red |
| MA-372 | U | Without a session (no cookie; wrong value; right cookie at a path outside `/s/<P>/`; eight decoy cookies plus none valid): GET `/`, `/api/search`, `/files/x.html`, `/goback`, dashboard `/`, `/api/activity`, `setup /` → HTML 401 locked page (X12 text: what/why/remedy per surface) or JSON 401 for `/api/`, never archive data or the CSRF token; dashboard POSTs → refused, side effect absent; positive twin: redeem a link, cookie attributes are exactly `Path=/s/<P>/; HttpOnly; SameSite=Strict`, the same routes return 200, and the dashboard's reader link works on the reader's port with the same cookie; each surface, `setup` included, has a functional positive twin that loads its page and every subresource under `/s/<P>/`, a structural check finds no same-origin absolute URL in any served HTML or JS, and an absolute `Location` from an inner 301 keeps the prefix **[CC17]** | R23, X9, X12, S42 | accept requests with no cookie → `curl` reads `/api/search`, red |
| MA-373 | U | A link redeems once (second use → "already used" page, 401), expires at 10 min (injected clock), is not accepted as a cookie, the token never appears in any later response; redemption is a 200 page navigating same-origin (not 3xx); the table never exceeds 16; every response carries `Cache-Control: no-store` and `Cross-Origin-Resource-Policy: same-origin`; `serve` prints the link on stdout, a "session opened" notice on stderr per redemption, and a fresh link on Enter (pty fixture); `-no-browser` suppresses the open; an unknown token gets the neutral page, an expired one the expiry page, a used one with its marker cookie continues silently, and only a used one without it gets the warning **[CC19]**; an opener token expires at 60 s and a printed one at 10 min, and the two differ **[CC5]**; the link is printed only when stdout is a terminal **[CC5]**; a backgrounded pty never reads stdin (no SIGTTIN) and reads after `fg` **[CC18]**; `serve -link` returns a fresh link through the instance file and HMAC handoff, and a wrong key gets nothing **[CC18]**; no auto-open without a display (injected environment) **[CC18]** | R23, X8, S43 | make tokens reusable → second redemption 200, red; replace the 200 page with a 303 → asserted status, red |
| MA-374 | U | With a valid session, `/files/` serves root `index.html`, `README.txt`, a folder's `index.html`, a message `.html`, `.eml`, `-attachments.zip`; 404 for `search.db`, `-wal`, `.mailarchive-manifest.json`, `.mailarchive-history.jsonl`, `.mailarchive-lastrun.json`, `.mailarchive-schedule.json`, `.mailarchive.lock`, `mailarchive.log`, `x.stderr.log`, `attachments-report.tsv`, `graph.token`, `x.secret`, `_outlook-pst/a.pst`, `BACKUP-NEEDS-ATTENTION.txt`, a symlink at any depth, a symlink swapped in after the check (fault-injected); an injected cloud-placeholder page (`ModeIrregular`, a non-surrogate tag) is served and a junction is 404 **[CC13]** | R19, S22, S45 | restore the old `containedDir` → `search.db` 200, red |
| MA-375 | U | `graph` with `-token-cache <out>/graph.token` (and via a symlinked parent; and `-client-secret-file` inside) → exit 1 naming both paths and the default, no token cache created, no capture; with `-unattended` → capture runs, warning logged, `status` WARN `credential_in_archive`; `schedule --install` refuses, nothing installed; desktop Settings POST with the config dir as archive location refused, settings unchanged; positive twins outside `-out` succeed; desktop `HeadlessCapture` with an existing `settings.json` whose archive location contains the config dir → the capture runs, a last-run warning is written, `status` WARNs `config_in_archive` **[CC16]**; `-out` = a fake home containing the default config dir → interactive `graph` with defaults proceeds **[CC16]**; a `-token-cache` under `-out` whose parent does not exist yet is still refused **[CC16]** | R12, R17, S45 | delete the containment check → `graph` proceeds, red |
| MA-376 | U | Desktop launches (in-process fixtures with an injected per-account state directory **[CC21]**): first launch takes the lock, removes a stale instance file, binds, writes a `0600` instance file; second launch hands off — the instance's browser opener is called once and the client exits 0, its own opener never called; an opener that errors → `opened:false`, and the client shows the fresh link through the dialog seam **[CC20]**; a second handoff within a second → "busy", the client exits 0 **[CC21]**; a deleted instance file with the lock held → rewritten by the holder, and the client's retry succeeds **[CC21]**; a different `-config` → a visible refusal, no handoff **[CC21]**; a squatter answering at the recorded port with a wrong proof → the client opens nothing and (lock free) starts its own instance on a fallback port; 8097 held by another process → dashboard on an ephemeral port with the "another program" notice in the header and `status` **[CC23]**, reader always ephemeral, both recorded (**[ruling OQ-9]**: the dashboard is on an ephemeral port in every case, and this case keeps the adapted "former address" notice text of §7.6); a listener on 8097 without `/_handoff` → treated as a pre-upgrade instance **[CC22]**; explicit `-addr` busy → exit 1, and the `showRefusal` seam receives the §7.6 text **[CC20]**; wedged instance → exit 1 with its text, naming the lock's pid; the instance-file check compiles and runs under `GOOS=windows` with an injected owner-SID reader **[CC21]** | R23, R12, S42, S43 | revert to the `isAddrInUse → openBrowser(dashURL)` path → squatter opened, red; drop the `showRefusal` call → the seam is never called, red |
| MA-377 | L | Windows 11 with the per-machine MSI, two accounts (fast user switching and an RDS host): each account's Startup and Desktop launches reach its own instance; account B's browser at account A's port sees only the locked page; Edge, Chrome and Firefox carry the session from the dashboard to the reader; Safari on macOS with `serve` likewise; an MSI upgrade while sessions of both accounts are running ends the old instances (or tells the user to sign out), and no unauthenticated 8097 remains **[CC22]** (**[ruling OQ-9]**: nor does any upgraded dashboard listen there); on macOS the opener's stdin delivery opens the browser with no prompt **[CC5]**; a broken default-browser association shows the link dialog, and "Copy MailArchive link" works **[CC20]** — **pending**, lab drill with a friction log | R23, S42 | n/a (lab) |
| MA-378 | S | Structural: every pattern registered through `localweb.Mux` by each production constructor (`serve`, `setup`, dashboard, reader — and any later route, e.g. the extraction workspace) is probed unauthenticated and with a hostile Host and must refuse; a constructor that returns a handler not built by `localweb.Wrap` fails the test; the list of probed patterns is non-empty and equals the registered set (resolve-by-name); every `http.Server`/`http.Serve` in `cmd/` is given a `localweb.Wrap` handler (AST check); ships in slice C1 **[CC15]** | R23, R19, X9 | register one route on a bare `http.ServeMux` → probe finds an unguarded 200, red |
| MA-379 | U | Raw-byte scrub: a fixture message with a unique subject, body phrase (original case), sender, Message-ID and attachment name; positive twin first — before `reindex`, a case-insensitive byte search of `search.db`+`-wal`+`-shm` finds the needles; after deleting its `.html` and `reindex`: zero occurrences of every needle including the lower-cased FTS token, a surviving message still matches FTS, `-wal` size 0, `scrub.pending = 0`; a legacy fixture whose residue was made by a plain `DELETE` is clean after the one-time scrub; **policy-1 twin [CC33]:** on an index with `secure_delete` and FTS5 `secure-delete` on — including rows inserted before the legacy scrub turned the options on — a redaction with no `optimize`/`VACUUM` leaves zero needles and survivors match; a fresh archive's first capture and the first capture after `reindex -rebuild` run no `VACUUM` (counted) | R24, R13, R8, S44 | remove `optimize` → FTS token found, red; remove `VACUUM` → legacy residue found, red; skip the final checkpoint → `-wal` holds the text, red; turn FTS5 `secure-delete` off → the policy-1 twin finds the token, red |
| MA-380 | U | Fault injection at R3, R4, R5, R6, R7, R9 (kill = return early with state persisted): the next `reindex` completes the redaction and every MA-379/381 assertion holds; after an R4–R9 crash `status` WARNs `redaction_unfinished`; an injected `SQLITE_FULL` in `VACUUM` → exit 1, the message names the space needed, manifest/pages/history already reconciled, `pending = 1`; a reader holding a read transaction → exit 1 naming the reader; the manifest-driven gone set repairs records whose index rows were pruned by a crashed run; **errors, not kills [CC28]:** an injected error at R3 (an `.eml` that cannot be removed), R5 and R6 keeps that member's record as `redaction:"incomplete"`, exits 1, `status` WARNs, and the second `reindex` completes the removal and is the only run that exits 0; **repair twin [CC26]:** a stale row whose record points to a present, renamed file → 0 files deleted, 0 log lines replaced, the record kept, exit 0, "repaired 1"; **classifier [CC27]:** a renamed folder, an NFD-named copy of an NFC slug, an iCloud `.<name>.icloud` placeholder, a folder behind a refused link, and a Rebuild with an injected `Add` error each leave records, log lines and folder pages intact, list "not indexed — kept" and exit 1; a gone set above the intent threshold removes nothing without `-redact`; **space [CC34]:** an injected short free space skips the legacy scrub with a WARN and records the attempt; a repeated failure is not retried by an unattended run within 7 days; the temp-directory pin is in effect | R24, R5, R13, R18, S44 | drop `pending` from the R4 transaction → crash after R4 leaves no WARN, red |
| MA-381 | U | Folder pages: redact every message of `Personal/` and one of a multi-page `Inbox/` (pageSize shrunk): `Personal/index.html` removed and the empty `Personal/` directory removed; `Inbox/index-3.html` above the new count removed; a user-written `index.html` without the marker or fingerprint survives (positive twin of the identification); Rebuild does the same; **whole-tree assertion**: no file under `-out` (pages, manifest, history, reports, logs, `search.db*`) contains the redacted subjects; a nested archive and a non-archive folder under `-out` keep their pages and files **[CC6]**; a folder with a present recorded file keeps its pages even when the index lacks its rows **[CC27]**; an unidentifiable `index.html` in a row-less folder is itemized "check this file", exit 1 **[CC31]** | R24, R13, R7, S44 | remove the prune pass → `Personal/index.html` still lists the subject, red (C1-2 PoC) |
| MA-382 | U | Logs and records: lines naming the redacted stem, its bracketed Message-ID or its exact subject (a `render "<subject>"` line) in `mailarchive.log`, `mailarchive.log.1` and `<name>.stderr.log` are replaced by the marker line, other lines byte-identical, and a short Message-ID (`a@b`) wipes nothing **[CC31]**; the descriptor-named log **outside** `-out` is untouched — bytes and mode identical (NoSideEffect) — and named in the summary **[CC29]**; with `-log` inside `-out`, the run's own later lines (R7–R10, the summary) land in the live file, and a second open handle keeps appending to the same inode (POSIX) and does not block the rewrite (Windows) **[CC30]**; `attachments-report-legacy.tsv` loses the row; the last-run error text is replaced through `WriteLastRun`, and `BACKUP-NEEDS-ATTENTION.txt` no longer names it **[CC31]**; a stale `.mailarchive-snapshot-*.pst` is swept, a fresh one (newer than run start) is kept; `_outlook-pst/` is named in the summary and left; a 2 MiB line is handled without truncating the log; a gone set of 10^5 members over a 16 MiB log finishes in one pass within a time bound **[CC31]** | R24, R13, S44 | skip `scrubTextFiles` → the stem survives in `mailarchive.log`, red; rename instead of rewriting in place → the run's own summary line is missing from `-log`, red |
| MA-383 | U | Honesty text: README's redaction section, the archive `README.txt` and `reindex -h` contain the covered/not-covered statement (each of: source re-capture, backups/copies, disk blocks/SSD/snapshots, indexers/caches/browser, logs outside); the summary prints the re-capture line for a Graph store and for a local source file that exists, and omits it otherwise; the not-covered list names kept messages that quote, forward or duplicate a removed one **[CC32]** and an outside schedule log by its path **[CC29]**; `reindex -h` documents `-redact` and the intent threshold **[CC27]** | R24, R12, S44 | delete the re-capture line → assertion red |
| MA-384 | U | Security copy: the dashboard's Security & privacy panel states that other accounts on this computer and web pages cannot open the dashboard or archive and that a program running as you can (OS-aware vault wording kept, MA-272); the dashboard shows the OS account it runs as and the device-sign-in box names it; `serve`'s start-up text no longer says "has no authentication" and names the link; README `serve` section matches; the dashboard header shows "browser sessions opened this run: N (last at HH:MM)" at rest, outside the capture ring **[CC24]**; the account banner and "a sign-in code only in a window opened from the shortcut" appear on every sign-in surface, the extraction workspace's included **[CC3, CC23]**; the macOS copy names the argv residual **[CC5]** | R23, X11, S41 | restore the old "served only on 127.0.0.1" sentence → assertion red |

## 10. Build slices and order

| Slice | Contents | Depends on | Blocked on | Rows | Gates owed after build |
|---|---|---|---|---|---|
| **A** — private at rest | §4.A: `util/private*.go` (no-follow `TightenPath`, exported `Posture`), A2 index pre-create, every creation site (`exporter.go:301` and `pages.go:125` via `MkdirAllPrivate` **[CC12]**), A3 tool-owned tightening with reason codes and foreign-owned replacement, A4 scheduler logs, A5 Windows new roots (TokenUser, the parent's non-broad entries, effective-rights check, never-list) and `Posture` on every run, the opt-in `access -restrict`/`-restore`, A6 status | — (no dependency on B: links are only counted until B **[CC12]**) | OQ-7 for `extract -dest` on Windows; OQ-10 (**[ruling]**: both ruled, J3 and J1; nothing blocks A) | MA-365..367, MA-368 (U part), and MA-500 (MA-368's L part, run as A's Windows drill **[ruling J1, OQ-1]**) | adversarial pass (permissions/ownership edge cases); friction review of the status lines, the migration log line and the inheritor and recipient walks (§7.5) |
| ~~**A-W** — existing Windows archives (new **[CC8]**)~~ **[ruling OQ-1]: not built** | the automatic existing-archive DACL rewrite (SDDL saved first, never-list, effective-rights check), if OQ-1 rules for it; README-RMM fleet script; Detect-RMM posture. **[ruling OQ-1]** OQ-1 rules against it: report-only plus the opt-in `access -restrict`/`-restore`. The README-RMM fleet script for the opt-in and Detect-RMM's posture report move to slice A | A | OQ-1 and a completed MA-368 drill | MA-368 (L part) — now MA-500, in slice A | — |
| **C1** — Host + allow-list + credentials outside | §4.C1: `localweb.Wrap` with Host only (any loopback port **[CC18]**), one route model (`localweb.Mux` for the inner muxes, the `cmd/` structural check **[CC15]**), headers, `/files/` allow-list on `os.Root` (placeholder-aware **[CC13]**), P10 with the shared containment helper and the `HeadlessCapture` warning **[CC16]** | — | OQ-10 (**[ruling J1]**: ruled) | MA-371, 374, 375, 378 **[CC15]** | adversarial pass |
| **C2** — per-account surfaces | §4.C2–C4: sessions, `/s/<P>/`, the complete relative-URL list **[CC17]**, `/_open` with the marker cookie **[CC19]**, link delivery for `serve`/`setup`/desktop (`serve -link`, the foreground check, the macOS stdin opener, `setup` on port 0 **[CC5, CC18]**), instance lock/file/handoff in the non-purgeable directory with the `opened`/`busy` replies **[CC20, CC21]**, visible refusals and "Copy MailArchive link" **[CC20]**, port fallback with the 8097 notice **[CC23]**, pre-upgrade instance detection **[CC22]**, reader ephemeral port, session count **[CC24]**, security copy; packaging: `Install-RMM.ps1`, `Product.wxs` CloseApplication, `README-RMM.md`, `DEPLOYMENT.txt`, `README-payload.txt`, `BUNDLE-CONTENTS.txt`, `Cleanup-CurrentUser.ps1` **[CC22]**. **[ruling OQ-9]** The dashboard takes an ephemeral port like the reader (no 8097 fallback), the 8097 probe stays for pre-upgrade instances (CC22) and for the adapted former-address notice (CC23), and README-RMM verifies the install by the shortcut and the locked page | C1 | OQ-9 (and OQ-4, OQ-5 as before); OQ-10 (**[ruling]**: OQ-9 = (b), OQ-4 = yes, OQ-5 = later, OQ-10 = J1; nothing blocks C2) | MA-372, 373, 376, 377, 384 | adversarial pass **and** friction review (a ceremony change for every reader user — co-equal ship blocker); X9/X12/X11 contract updates |
| **B** — no link-following writes | §4.B: `internal/archivefs` shared with recapture-integrity **[CC2]**, link classification with Windows reparse points **[CC13]**, exporter/pages/sweep/history/index call sites (moving A's `MkdirAllPrivate` sites to `ArchiveFS` **[CC12]**), reindex delete side, the `links_in_archive` status code **[CC12]**, the store-link migration text **[CC14]** | — | OQ-10 (**[ruling J1]**: ruled) | MA-369, 370 | adversarial pass |
| **D** — redaction that removes | §4.D: the secure-delete options and the policy stamp, the legacy scrub with pre-flight and retry policy, the classified gone set amending recapture §4.9 (repair row, classifier, intent, superseded entries), scoped page cleanup, the in-place text-file scrub with CC31's matching, incomplete members, snapshot sweep, order R0–R10, summary and README | A (new files `0600`), B (`ArchiveFS.Remove`, link-safe cleanup, the three-state existence check), and recapture-integrity S2 (MA-361) and S4 when they land first — otherwise D carries the repair row itself **[CC26, CC35]** | OQ-8 (and OQ-6's default); OQ-10. **[ruling ESC-4]** OQ-8 is escalated (with R13's classifier and added deletion cases): D does not start, and its catalog-first PR lands none of them, until the owner answers; RR-5 runs before that PR. OQ-6 = automatic and OQ-10 = J1 are ruled | MA-379..383 | adversarial pass (a residue hunt: raw-byte search of every file after redaction on several fixtures) **and** friction review (reindex duration/disk, summary wording, the intent threshold); before D's catalog-first PR, its conditions are re-verified by re-running lenses 3, 6 and 7 on §4.D **[CC27]** |

**[ruling OQ-1, ESC-4]** A-W is not built, and D waits for ESC-4. The cross-design order is the owner-rulings log's §9: LC-A is row 2,
LC-C1 row 3, LC-C2 row 11, LC-B row 18 (after re-capture S2 creates `internal/archivefs`) and LC-D row 37.

Recommended order **A → C1 → C2 → B → D**, with A-W after A once OQ-1 is ruled and the MA-368 drill has passed: the two high findings
first (A closes C2-1, C2 closes F3), C1 is small and closes F2/N9/F20 with no ceremony change, and D is last because it needs A and B. A
does not wait for B: its two exporter/pages creation sites use `MkdirAllPrivate` and move to `ArchiveFS` in B, and `links_in_archive`
ships with B **[CC12]**. B and C are independent and may be built in parallel. **Cross-design order [CC2]:** recapture-integrity's S2
(MA-361) and S4 before D (or D carries the repair row); this design's C1 and C2 before the extraction workspace's E3, whose E0 they are
(its XC23); the shared `internal/archivefs` package is created by whichever of B and recapture's slice lands first. Each slice is
catalog-first (its rows and R/X text land before code, for the operator to accept, with provisional ids renumbered to the operator's
allocation, OQ-10 **[CC1]**; **[ruling J1]**: done in this document, and catalog-first PRs land one at a time in the log's §9 order),
ships prove-fail → prove-pass, and flips its own status line here citing its gate artifacts. No slice ships
while its gate is open or a ruling it is blocked on is unmade; each stops at its gate and says which one it owes.

## 11. Open questions needing an OWNER RULING

Revision 2 restates OQ-1 and OQ-6 and adds OQ-7..OQ-10 (the review's "Owner and operator rulings"). A question marked **Blocks**
blocks the slice it names until it is ruled.

**All ten are ruled or escalated** (`docs/owner-rulings-2026-10-09.md` §6, with J1 and J3 of its §1 and ESC-4 of its §10). Each
question keeps its text and carries its outcome.

1. **OQ-1 — Windows, existing archives (restated [CC6, CC8]).** Revision 2 never rewrites the DACL of a drive or volume root, a profile
   folder, or a folder that holds files the tool does not own (A5's never-list), and from slice A it offers the opt-in
   `mailarchive access -out DIR -restrict` (logged, original SDDL saved, `-restore`). The remaining question: once the MA-368 drill has
   passed, should slice A-W rewrite an existing archive root's DACL automatically on its first run — removing broad principals (Everyone,
   Users, Authenticated Users, Interactive, Guests, Anonymous, Network, Domain Users/Guests) while keeping every other entry, with the SDDL
   saved first — or stay report-only plus the opt-in command? *Built if not ruled:* report-only plus the opt-in command; new roots are
   protected from birth either way. **Blocks:** slice A-W. **RULED** (`docs/owner-rulings-2026-10-09.md` §6): no. Report-only plus the
   opt-in `access -restrict` / `-restore`; A-W is not built; `status` stays RED for an open archive until it is restricted.
2. **OQ-2 — Redaction versus re-capture.** A redacted message still present at the source is archived again by the next capture. Should
   redaction record a tombstone (a salted hash of the message identity, never its text) that later captures skip and count ("skipped 1
   message you removed from the archive"), with a way to undo it — or is the stated warning ("delete it at the source first") enough?
   *Built if not ruled:* the warning only. **RULED** (`docs/owner-rulings-2026-10-09.md` §6): no tombstone; the warning only, since a
   tombstone would add a skip exception to R1 and R17.
3. **OQ-3 — Shared-read archives.** Is an archive that another OS account must read (a family NAS share, a backup host read by a second
   login) a supported use? If yes, it needs an explicit opt-in (e.g. `-share-group NAME`) that this revision does not design. *Built if not
   ruled:* owner-only, no opt-out. **RULED** (`docs/owner-rulings-2026-10-09.md` §6): no. Owner-only, with no opt-out.
4. **OQ-4 — The `serve`/desktop ceremony.** Is it acceptable that the reader and dashboard open only through a one-time link (the
   browser now opens itself; a second browser or device needs a new link; bookmarks land on the locked page after a restart; scripts must
   use `mailarchive search` instead of the HTTP API)? This is the friction §7.1–7.2 counts. **RULED**
   (`docs/owner-rulings-2026-10-09.md` §6): yes, as §7.1–7.2 count it; C2's friction review is a co-equal ship blocker.
5. **OQ-5 — Peer-credential admission.** On Windows (`GetExtendedTcpTable` owner PID → process token user) and Linux (`/proc/net/tcp` uid),
   the server could verify that the connecting browser runs as the same account, which would admit the user with no link at all and close the
   login-CSRF residual (§8.4) — but it is a second, OS-specific admission path, and macOS has no pure-Go equivalent, so sessions stay needed
   there and for a LAN `serve`. Build it now, later (after C2 ships), or never? *Built if not ruled:* not built. **RULED**
   (`docs/owner-rulings-2026-10-09.md` §6): later, reconsidered after C2 ships and its friction review is done. Until then the
   login-CSRF and packet-capture residuals stay open, not accepted.
6. **OQ-6 — The one-time legacy scrub (restated [CC33, CC34]).** It now applies only to an index created before scrub policy 1 (a new or
   rebuilt index is stamped and never pays it), runs only after a free-space pre-flight on the archive volume and SQLite's temp directory,
   and retries unattended at most once a week. Should the first *capture* after upgrade run it automatically (one `VACUUM`, minutes on a
   large index, transient disk about twice the index size), so text redacted with earlier versions disappears without user action — or
   should only `reindex` run it, with `status` WARNing until it is run? *Built if not ruled:* the automatic one-time scrub at the end of the
   first capture, with `status` WARN only if it was skipped or failed. **RULED** (`docs/owner-rulings-2026-10-09.md` §6): automatically,
   as built, after the free-space pre-flight and retried at most weekly when unattended. It ships with slice D (so after ESC-4).
7. **OQ-7 — Windows posture of hand-over copies (new [CC3]; the same question as the extraction workspace's Q10 — one ruling answers
   both).** For `extract -dest` roots and the workspace's export directories on Windows: (a) owner-only — a protected TokenUser/SYSTEM/
   Administrators DACL; a recipient on another PC needs administrator rights or `takeown`, and on a file share "Administrators" means the
   server's administrators, so the intended recipients are locked out; or (b) inherit the destination's ACL — easy for the recipient, and
   readable by whoever can read the destination, which the `Posture` line names before the copy starts. POSIX modes are `0700`/`0600`
   either way. *Revision 2 describes:* (b), which matches the workspace design. **Blocks:** the Windows half of `extract -dest` in slice A,
   and the workspace's export slices. **RULED = J3** (`docs/owner-rulings-2026-10-09.md` §1): (b), inherit the destination's ACL and
   name the posture before the first write; P3's protected DACL is scoped to archive roots and the tool's own state directories.
8. **OQ-8 — Redaction intent (new [CC27]).** Should the irreversible, trace-erasing steps of a redaction require explicit intent — always
   (`mailarchive reindex -redact`, or a dashboard confirmation listing folders and counts), only beyond a threshold (a member whose folder is
   missing entirely, or more than N members in one run), or never (today's "delete the files, then run `reindex`")? *Revision 2
   describes:* the threshold, with N = 25. Below it, plain `reindex` redacts files deleted one by one from folders that still exist, as the
   README documents; beyond it, `reindex` reports what is missing and how to put it back or confirm, and removes nothing. Whatever the
   ruling, a prune reason that is not an operator's deletion is never a redaction (D2). **Blocks:** slice D. **ESCALATED (ESC-4)**
   (`docs/owner-rulings-2026-10-09.md` §10), together with R13's classifier amendment and R13's added deletion cases (the index scrub,
   the stale-page and empty-folder removal, the stale-snapshot sweep, the manifest-driven gone set). Every option either narrows R13's
   automatic prune or drops the safety net that dissolved the review's hidden blocker. Recommended answer: the threshold, with the R13
   amendment and its deletion cases. Interim: plain `reindex` keeps today's behaviour; slice D does not start.
9. **OQ-9 — The desktop dashboard's address (new [CC23]).** (a) Keep `127.0.0.1:8097`: a bookmark or typed address lands on the locked
   page, which sends the user to the shortcut; the owner accepts the stated residual (§8.4) that a hostile account which holds 8097 first
   can serve a look-alike and phish a user who types the address or uses a bookmark. (b) Make the dashboard ephemeral like the reader: the
   shortcut is the only way in, bookmarks stop working, 8097 leaves the RMM runbook, and a user who types the old address gets a less
   legible failure (nothing answers, or a squatter does). *Revision 2 describes:* (a), with CC23's warnings. **Blocks:** slice C2.
   **RULED** (`docs/owner-rulings-2026-10-09.md` §6): (b), an ephemeral dashboard port; the shortcut and "Copy MailArchive link" are the
   only way in, and 8097 leaves the RMM runbook. CC23's notice stays, adapted: when an unverified listener holds 127.0.0.1:8097, the
   dashboard header and `status` say "another program is using MailArchive's former address 127.0.0.1:8097 — open MailArchive only from
   its shortcut", and MA-376 keeps that case. The squat residual is narrowed, not closed, and stays an open gap, not accepted.
10. **OQ-10 — Catalog id allocation (new, OPERATOR [CC1]; joint with the extraction workspace's Q11 and fault-isolation's OR8).** Allocate
    R/S/X id blocks across the in-flight designs, as the MA ranges already are, so this design's provisional R-CONF-1..3, S-CONF-1..4 and
    X-CONF-LOCKED can be numbered and X-CONF-LOCKED reconciled with the workspace's X12. **Blocks:** each slice's catalog-first PR.
    **RULED = J1** (`docs/owner-rulings-2026-10-09.md` §8): R-CONF-1..3 → R22–R24, S-CONF-1..4 → S42–S45, X-CONF-LOCKED → X12, and the
    workspace's X12 → X13, so the two rules keep two ids; MA-368's L part → MA-500.
