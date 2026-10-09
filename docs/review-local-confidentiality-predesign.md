# Pre-code design review — local confidentiality (10 lenses)

**Design reviewed:** `docs/design-local-confidentiality.md`, revision 1 (2026-10-09), BUILD
STATUS "proposed". Code read as of 2026-10-09.

**Procedure:** assurance-kit `process/design-review.md`, run as a workflow on 2026-10-09:
- **Lenses run:** all ten, one finder per lens (1 purpose/threat-model fit, 2 focus/proportionality,
  3 durability, 4 usability, 5 integration/seams, 6 security, 7 failure domains,
  8 migration/coexistence, 9 dependency and operational cost, 10 honesty of claims). L4-1 was
  raised jointly under lenses 4 and 3.
- **Skeptic:** refutation-default, one verdict per finding, with the code in hand. A finding is
  CONFIRMED when revision 1 *as written* permits the failure. It is REFUTED only when a quoted
  section already handles it. The skeptic also re-graded severity (for example L1-5 and L6-4
  major → blocker; L1-7 and L10-5 major → minor).
- **Rescue:** a separate agent was given every finding the skeptic left at major or above and
  told to save the design. It named a dissolving change, or failed, for each one.
- **Filer checks:** the filer re-ran the rescue's SQLite experiment against the vendored
  SQLite 3.50.3 (the CC33 basis: with `PRAGMA secure_delete` and FTS5 `secure-delete` on, a
  delete plus a TRUNCATE checkpoint left 0 of 4 needles in `search.db`+`-wal`, 299 survivors
  still matched, and legacy residue stayed until VACUUM). It read the code the hidden blocker
  cites (`internal/app/reindex.go:300-339`, `internal/desktop/maintenance.go:75-92`,
  `internal/pages/pages.go:43-68`, `internal/util/paths.go:60`). It also cross-checked one
  finding from the sibling fault-isolation gate against this design (F1).

**Findings: 109 found · 101 confirmed · 8 refuted** (L1-8, L1-9, L2-1, L2-5, L2-6, L4-10,
L6-8, L10-9).
- The 109 are 107 lens findings, the rescue's hidden blocker (**H1**) and one filer
  cross-check (**F1**).
- Skeptic severity of the confirmed findings: **7 blocker · 55 major · 39 minor**. The blockers
  are L1-1, L1-5, L4-1, L6-4, L7-1, L7-2 and H1.
- Several lenses found the same defect independently. The R/S/X id collision was found four
  times, the descriptor-named log written outside `-out` four times, and the rename over an
  open log three times. Each keeps its own row and points to one condition.
- **Rescue:** 60 sent (6 blockers, 54 majors). **58 DISSOLVED**, **2 PARTIAL** (L6-2, L6-3;
  their residuals are now stated at the claim), **0 SURVIVE**. The rescue also surfaced the
  hidden blocker H1 and dissolved it with a named change (CC27).

**Verdict: GO_WITH_CONDITIONS.** No blocker survives the rescue, so no lens fails.
- **CC1–CC35** are must-fix-before-code. Each is folded into design revision 2 and marked in its
  text as `[CCn]`.
- **OQ-1 (restated), OQ-6 (restated), OQ-7, OQ-8, OQ-9 and OQ-10** need an **OWNER (or
  OPERATOR) RULING**. Each is a product or operator choice the owner has not made, or it is
  shared with an approved sibling design. They are not imposed here. The slice each one names
  is blocked until it is ruled (design §10, "Blocked on").
- Build may not start from revision 1.

**Owner decision honoured.** The relayed decision for the copy tool is "the simpler tool …
where you just select the source and choose the export destination to copy it to rather than
any drag and drop". Revision 1 recorded it correctly (§6.3), and the extraction workspace
records it as OW4. No condition reopens it or adds a drag, drop or upload surface. What
revision 1 got wrong was its description of that design. It claimed the workspace writes
through `ArchiveFS` with a protected Windows DACL, but the workspace uses its own `os.Root`
and inherits the destination's ACL. CC3 corrects the description. The Windows posture of
hand-over copies becomes a joint owner ruling (OQ-7, the workspace's Q10).

## Conditions (must fix before code; folded into revision 2)

Each line gives the slice it gates and the findings it answers.

- **CC1 — Provisional ids** (all slices; L1-3, L3-12, L5-1, L10-1). Replace the proposed R22–R24,
  S42–S45 and X12 with the design-local ids R-CONF-1..3, S-CONF-1..4 and X-CONF-LOCKED in §2,
  §6.4 and §9. The operator allocates the real numbers (OQ-10, joint with the workspace's Q11 and
  fault-isolation's OR8) when the catalog-first PR is accepted, and every Serves and Covers cell is
  re-checked then. X-CONF-LOCKED is reconciled with the workspace's X12 at that point; the two
  rules are compatible and can take two ids. A provisional id cannot resolve against another
  design's catalog text, so a trace to the wrong property can no longer pass the covers-map gate.
- **CC2 — Sibling designs** (all; L2-2, L3-5, L5-3, L5-4, L10-2). §6.3 gains an entry for each
  sibling:
  - (a) **recapture-integrity.** There is one `internal/archivefs` package: verify's Inspect plus
    `ArchiveFS`'s no-follow write side. Whichever slice lands first creates it, and recapture's
    sweep and gates use it. D2 and D5 amend recapture §4.9's decision table and order; they do
    not replace them.
  - (b) **The extraction workspace.** C1, C2 and C4 are its E0, as its XC23 already says: one
    owner and one session mechanism. Its routes register through `localweb.Mux`, so MA-378
    probes them.
  - (c) **fault-isolation.** B is the write-side complement of its read-side containment.
  - (d) **mime-fidelity.** Its deferral of C2-2 to this design is acknowledged.

  §3.9 and A3 drop the "version above 6" reasoning: the change is an additive field with no
  version bump of its own, valid for manifest v6 and v7. §6.2 is re-derived for both outcomes of
  recapture's Q2. §10 records the cross-design order: recapture S2 (MA-361) and S4 before D, or
  else D carries the repair row itself. C1/C2 come before workspace E3.
- **CC3 — Hand-over outputs and the workspace bullet** (A, C2; L1-2, L4-2, L8-7, L5-4, L10-2).
  - §6.3 states what the workspace actually specifies. Its export root is its own exclusive
    `os.Mkdir` opened as an `*os.Root`, not `ArchiveFS`. A symlinked destination is allowed.
    Unix modes are 0700/0600, set at creation. On Windows the export inherits the destination's
    ACL, pending its Q10.
  - P3's and R-CONF-1's protected DACL covers archive roots (`-out`) and the tool's own state
    directories only.
  - Hand-over outputs (`extract -dest` roots, workspace export directories) are 0700/0600 on
    POSIX. On Windows they follow OQ-7; revision 2 describes inheriting.
  - Before any write, `extract` and the workspace preview (its XC32 posture line) print one
    `Posture(dest)` line when broad SIDs can read the destination.
  - The §5 guard "compromised" row names the workspace's copy-the-mailbox primitive.
  - §7.5 gains recipient rows (NTFS USB drive, UNC share, exFAT).
  - The account banner and MA-384 cover every sign-in surface, including the workspace's
    `/api/m365/signin`.
- **CC4 — Honest labels** (all; L10-4, L10-7, L10-8).
  - "Proven" means a U/S row on the CI matrix (ubuntu, windows, macos; Go 1.26.x; `go.mod` has no
    toolchain line), and B1's toolchain sentence is corrected.
  - The POSIX-mode rows MA-365..367 are GOOS-gated.
  - MA-368 is split into a U part (single-account DACL shape on windows-latest) and the L drill
    (a second account is denied).
  - P7's browser half is labelled lab-pending MA-377 (Chrome, Edge, Firefox, Safari).
  - P7 and §8.5 state inline that loopback is private only while no packet-capture driver grants
    non-admin capture (Npcap without admin-only mode, ChmodBPF, a wireshark group), and that OQ-5
    would close this.
- **CC5 — macOS and link exposure** (C2; L1-10, L10-3, L6-9).
  - P7's condition, C3 and §8.4 add macOS: `/bin/ps` is setuid root, so any account can read the
    argv of `open <url>`, and macOS has no hidepid equivalent.
  - The darwin opener passes the URL to `osascript` on stdin (`open location`), so the token is in
    no argv. The lab confirms no prompt appears; otherwise the residual stays stated.
  - Opener-delivered tokens live about 60 s; printed and copied links live 10 min.
  - The opened link and the printed link are separate tokens. The token is printed only when
    stdout is a terminal; otherwise `serve` points to `serve -link` (CC18).
  - P15's copy is OS-aware for macOS.
- **CC6 — Tighten and prune only what the tool owns** (A, D; L1-5, L4-1, L6-4, L7-1, and the
  drive-root half of L7-2).
  - The tightening set is the fixed root-level names, the store directories named by manifest
    records, the recorded paths with their parent directories, and tool folder pages. "Store
    directory" is manifest-derived only.
  - The walk never descends into a directory that holds its own `.mailarchive-manifest.json` or
    `search.db`.
  - The never-list (home or profile directory, volume or drive root) applies on POSIX and on
    Windows. When `-out` is on it, the tree pass is skipped.
  - A5 never rewrites the `-out` root's DACL when `-out` is on the never-list or holds entries
    outside the tool-owned set. Instead it protects each tool-owned store directory and fixed file
    individually. While anything tool-owned stays open, status shows RED "the archive shares a
    folder with other files — move it into its own folder".
  - D3's prune walk has the same scope.
  - §7 gains a GUI actor, and the GUI's folder picker proposes `<chosen>/MailArchive`.
  - MA-366, MA-368 and MA-381 add a non-archive sibling file, a sibling directory and a nested
    archive, all left unchanged (mode, ACL, pages).
- **CC7 — Windows DACL correctness** (A, A-W; L7-2, L6-10, L4-3, L8-4).
  - The DACL grants full control to the process's TokenUser SID (never "the owner", CREATOR OWNER
    or OWNER RIGHTS), SYSTEM and Administrators.
  - Before any write, the tool computes TokenUser's effective rights under the new DACL. Unless
    full control remains, it does not write and WARNs with a remedy.
  - A new root gets the parent's inheritable ACEs minus the broad SIDs, plus those three. A backup
    account granted at a parent therefore keeps access, and §7.4 is corrected.
  - Posture checks the fixed set, each store directory and the offenders the walk recorded, on
    every lock-holding run.
  - The `access_policy` stamp only suppresses repeated log lines. It is keyed to the root's
    identity: volume serial plus file ID on Windows, device plus inode on POSIX.
  - §5 gains a "wrong DACL" row, and §6.5 states that a protected DACL stops later parent changes
    from propagating. `status -json` lists the principals with access.
- **CC8 — Existing Windows archives become slice A-W** (A-W; L2-3, L8-5).
  - The existing-archive DACL rewrite leaves slice A. Slice A-W is gated on OQ-1 and on a
    completed MA-368 drill.
  - Until A-W ships, an open existing archive is RED `access_open` with an explicit opt-in,
    `mailarchive access -out DIR -restrict` (also a dashboard button). The command logs, first
    saves the root's original SDDL to `<UserConfigDir>/mailarchive/acl-backup/<hash(out)>.sddl`
    (0600, outside the served set), and has `-restore`.
  - README-RMM gets a fleet script, and Detect-RMM reports Posture.
  - OQ-1's built-if-not-ruled default becomes report-only plus the opt-in command.
- **CC9 — Posture reasons that tell the truth** (A; L4-7, L7-3, L7-4, L3-10).
  - One latched flag becomes per-offender reason codes:
    - `access_unenforceable`: a no-op chmod, or a volume without `FILE_PERSISTENT_ACLS`.
    - `access_not_owner`: EPERM. The message names the file and its owner; the remedy is chown or
      take ownership.
    - `access_no_permission`: Windows without `WRITE_DAC`. An administrator runs the printed
      `icacls`.
  - The codes are recomputed on every tightening pass and stored with a timestamp in the last-run
    record. They never latch.
  - On darwin, statfs `MNT_IGNORE_OWNERSHIP` or `MNT_UNKNOWNPERMISSIONS` gives
    `access_unenforceable` with the Get Info remedy. On Linux, vfat, exfat, fuseblk, cifs and nfs
    give WARN "unverified".
  - P2's condition and §7.5's macOS row agree.
  - MA-366/367 add EPERM, missing `WRITE_DAC`, ignore-ownership and a flag that clears.
- **CC10 — Foreign-owned content** (A; L6-2, PARTIAL).
  - POSIX: a tool entry owned by another uid is RED `access_not_owner`, naming the owner. Once the
    root is owner-owned and 0700, the run replaces such a content file before writing to it. It
    makes an owner-owned copy and renames it into place through `ArchiveFS` (for `search.db`,
    before `index.Open`), which breaks any hard link and any path the co-account holds. A
    foreign-owned root makes the run refuse to write: exit 1, with the chown remedy.
  - Windows: the walk records children whose owner is not TokenUser, SYSTEM or Administrators, or
    that carry an explicit broad ACE. Posture reports them RED with the `icacls /setowner` remedy.
  - The residual is stated in §8.6: on Windows the fix needs an administrator. An automatic
    re-home is optional and lab-gated in MA-368.
- **CC11 — Walk cost, migration wording, symlinked `-out`** (A; L3-11, L4-8, L8-9, L10-6).
  - Every verb gates the whole-tree pass on the `access_policy` stamp; the fixed set is tightened
    every run. The stamp is written by a small save that does not depend on the run succeeding.
  - Below the current policy, status shows WARN "will be made private on the next run (<next
    scheduled time>)". RED is reserved for loose modes found after a stamped run.
  - §6.1 says "every lock-holding run", and A3 lists `extract`.
  - A symlinked `-out` is resolved once, for both the walk and the fixed set (EvalSymlinks, or a
    walk through `os.Root`'s FS). A6 accepts it.
  - MA-365..367 add a symlinked-`-out` fixture.
- **CC12 — Slice A does not depend on slice B** (A, B; L5-2). In slice A, `exporter.go:301` and
  `pages.go:125` switch to `util.MkdirAllPrivate`; they move to `ArchiveFS` in slice B. The
  `links_in_archive` status code and its "will not write through it" text ship with B. Until then,
  A's walk only counts links. §10 records the note.
- **CC13 — No-follow primitives, Windows reparse points, an honest P5** (B; L6-5, L10-5, F1, the
  primitive half of L5-8).
  - TightenPath fchmods a descriptor opened `O_NOFOLLOW` (`O_DIRECTORY` for directories), or uses
    `fchmodat(AT_SYMLINK_NOFOLLOW)`.
  - P5's label is split. Operations backed by `os.Root` are race-free against escape. The
    path-based opens (`search.db` and its siblings, the run log) are check-then-use. Until P1
    makes the root private, a writer of the root can redirect them anywhere.
  - On Windows, links are classified as fault-isolation's FC1 classifies them. A symlink, or a
    name-surrogate reparse point (junction, mount point), is a link and is refused. Any other
    `ModeIrregular` entry is judged by its reparse tag (`os.Readlink` fails) and treated as the
    file or directory it represents. P9, A2 and `OpenNoFollow` therefore accept an online-only
    page, history log or `search.db` in a OneDrive folder.
  - MA-369/370 add a junction fixture (windows-latest), and MA-369/370/374 an injected-placeholder
    classifier case.
  - A2 and B1 use the repo's per-OS no-follow constants (`lock_windows.go`,
    `securefile_windows.go`).
- **CC14 — Store-level links** (B; L4-11, L8-3). §6.1 and the release notes state that a store
  directory that is itself a link is refused after upgrade. The P5 refusal text offers the
  supported layout: move the whole archive and point `-out` at it, or make `-out` itself the link.
- **CC15 — One route model** (C1; L5-6). Either the inner muxes (`server.go`, `dashboard.go`,
  `setup.go`) become `localweb.Mux`, or `Wrap` exposes the inner route list through a registration
  hook. A structural check requires every `http.Server`/`Serve` in `cmd/` to be given a
  `localweb.Wrap` handler. MA-378 moves into slice C1 with P6's label.
- **CC16 — Credential containment** (C1; L5-9, L8-6, part 3 of L4-13).
  - P10 reuses extract's `destOverlapsOut`/`resolveExisting`, moved to `util`, which resolve the
    deepest existing ancestor.
  - When `-out` contains the OS config directory (`-out` = home), P10, the reader and the
    tightening walk exempt that directory explicitly.
  - `HeadlessCapture` treats an archive location that contains the config directory as
    `graph -unattended` treats one: it warns, writes a last-run warning, and status WARNs
    `config_in_archive`. Only the Settings POST refuses.
  - MA-375 adds an existing `settings.json` in that position and asserts the capture still runs.
- **CC17 — Relative URLs, completely** (C2; L5-7, L8-11).
  - The C2 deliverable is a grep-complete list: `setup.js`, `/api/setup`, `/app.js`,
    `/dashboard.js`, `href="/"` (dashboard, go-back) and go-back's `template.URL("/files/"…)`.
    Alternatively every surface page emits `<base href="/s/<P>/">`.
  - `Wrap` rewrites or refuses an absolute `Location` header that drops the prefix.
  - A structural assertion requires that no served HTML or JS contains a same-origin absolute URL.
  - Each surface, setup included, gets a functional positive twin that loads its subresources.
- **CC18 — Headless and remote `serve`** (C1, C2; L4-5, L8-2, L9-6, L5-10, L5-11).
  - Loopback names (127.0.0.1, localhost, [::1]) are accepted with any port; MA-371's other-port
    case becomes a positive twin. The rebinding defence rests on the hostname.
  - `mailarchive serve -link -out DIR` mints a fresh link through a 0600 per-account instance file
    and C4's HMAC handoff (`quiet:true`; the reply MAC covers the link). The locked page names
    whichever path applies.
  - No auto-open without a display.
  - Stdin is read only while the process is the terminal's foreground process group, so a
    backgrounded `serve` never gets SIGTTIN.
  - `setup` defaults to `127.0.0.1:0`. A LAN `serve`'s cookie may carry a Max-Age.
  - §7 gains tunnelled and detached `serve` actors, with Type counts.
- **CC19 — Warn only on a real race** (C2; L4-6).
  - An unknown token gets neutral text: "this link is from an earlier session; get a new one".
  - Redemption sets a random marker cookie M (`Path=/_open`, HttpOnly, stored server-side with
    `sha256(T)`, never derived from T). A used token presented with its own M continues silently.
    Only a used token without M gets the "another account may have…" warning.
  - After a successful auto-open, `serve` prints "if your browser did not open, use: …".
- **CC20 — Visible desktop refusals and an opener fallback** (C2; L4-4, L7-8, L7-9, L9-1).
  - Every client-side refusal also goes to `zenity.Error`, through a `showRefusal` seam, and to
    stderr. `ncruces/zenity` is already a dependency and uses MessageBox on Windows.
  - The handoff reply waits up to about 15 s for the minted token to be redeemed and returns a
    MAC-covered `opened` (true or false). On false, or when `cmd.Start` fails, the client shows the
    fresh link in a dialog.
  - A Start-menu shortcut, "Copy MailArchive link" (`mailarchive-desktop -copy-link`), shows or
    copies a fresh link. The locked page names it.
  - §5's "no new single point of loss" is corrected.
  - MA-376 asserts the refusal text and an opener that errors.
- **CC21 — Instance lifecycle** (C2; L3-7, L7-11, L9-2, L5-8, L7-7, L2-4, L4-12).
  - The lock and the instance file live in a non-purgeable per-account directory
    (`%LOCALAPPDATA%\MailArchiveDesktop`, or the config directory elsewhere), not in
    `os.UserCacheDir`.
  - The lock holder removes any stale instance file right after taking the lock, before binding.
    It re-checks the lock periodically (`lockfile.StillHeld`) and rewrites the file if it is
    missing.
  - The client cross-checks the file's pid against the lock's holder line, and retries a failed
    handshake for a bounded time while the lock is held. A rate-limited handoff answers "busy — a
    browser is opening", which counts as success.
  - A refusal names the pid from the lock. A missing `UserConfigDir` or `HOME` gives a visible
    refusal: fail closed.
  - The ownership check is `util.ReadSecureFile`, plus an owner-SID check on Windows.
  - The file records the config path and the logon-session id. A launch with a different
    `-config`/`-out`, or from another logon session, gets its own instance or a visible refusal,
    never a silent handoff.
- **CC22 — Upgrade with running instances, and the packaging** (C2; L3-6, L8-1, L5-12, L8-10,
  L9-5).
  - `Install-RMM.ps1` stops mailarchive-desktop processes before msiexec, as
    `Uninstall-RMM.ps1:9` already does. `Product.wxs` adds `util:CloseApplication` with
    TerminateProcess.
  - A listener on 8097 without `/_handoff` is a legacy instance. The new binary ends a
    same-account old process or tells the user to sign out, and status and the dashboard warn.
  - Slice C2's deliverables include `README-RMM.md` §D/§E (verify via the shortcut and the locked
    page), `DEPLOYMENT.txt`, `README-payload.txt`, `BUNDLE-CONTENTS.txt` and
    `Cleanup-CurrentUser.ps1` (which also removes the new state directory).
  - §7.4 is restated, and MA-377 adds an upgrade-while-sessions-run drill.
- **CC23 — A squatted 8097 is never silent** (C2; L6-3, PARTIAL).
  - When an unverified listener holds 8097, the dashboard header and status say "another program
    is using MailArchive's usual address 127.0.0.1:8097 — open MailArchive only from its
    shortcut".
  - The bookmark endorsement leaves §6.1, §7.1 and the locked page.
  - The device-sign-in box and P15 say "MailArchive shows a Microsoft sign-in code only in a window
    it opened from its shortcut".
  - §8.4 adds the persona "a hostile first account squats 8097 and serves a look-alike with its own
    device code", as a stated residual that neither sessions nor OQ-5 close. The fixed-versus-
    ephemeral port is OQ-9.
- **CC24 — Session notices outside the capture ring** (C2; L4-9). The dashboard header (or the
  Security & privacy panel) shows "browser sessions opened this run: N (last at HH:MM)", outside the
  400-line capture ring, and MA-384 asserts it.
- **CC25 — Friction accounting** (C2, A; parts 1–2 of L4-13). §7.2 classes the scripted-API loss as
  Type II and records a same-account `-api-token-file` option in the operability backlog. §7.5 adds
  the non-admin Windows inheritor and their remedy: an administrator takes ownership, or the README
  gives a pre-handover `icacls` step.
- **CC26 — Redaction membership by record** (D; L1-1).
  - Reindex decides every row with recapture-integrity §4.9's table, which becomes the single
    Reindex decision table that both designs amend.
  - A redaction member is a §4.9 "prune" row, or a manifest record with no index row whose own
    `rec.Path` `.html` is missing.
  - A "repair" row (the row's path is missing, and `rec.Path` differs and is present) re-points
    the row. R3, R6, R7 and R9 skip it: nothing is deleted or scrubbed, and the record is kept.
  - A member keeps both stems (row path and `rec.Path`) for R3/R6.
  - If D lands before recapture S2, D carries the repair row itself.
  - MA-380 adds a twin: a stale row with a live record gives 0 files deleted, 0 log lines
    replaced, the record kept, exit 0 and "repaired 1".
- **CC27 — A prune is not a redaction** (D; H1, L7-13, L7-14).
  - (a) **Classifier.** A member is only a record or row whose own `.html` is ENOENT through
    `ArchiveFS` while its parent is a real directory. That directory must hold no
    normalization-equivalent (NFC/NFD) entry and no cloud-eviction placeholder (`.<name>.icloud`).
  - (b) **Every other prune reason** keeps the record, is listed as "not indexed — kept", makes the
    run exit 1, and never triggers R3, D3, D4 or R7. These are: a missing parent folder, a link or
    irregular component (existence is three-state: present, gone or blocked), oversize, unreadable,
    and an `Add` error.
  - (c) **Rebuild** runs D3–D4 only for records classified "missing".
  - (d) **D3** runs only when the gone set is non-empty, only in folders that held gone members,
    and never deletes the page of a directory that still holds a present recorded file.
  - (e) **D4** never rewrites lines written by the current run.
  - (f) **Intent.** A gone set beyond OQ-8's intent threshold is reported, and nothing destructive
    happens until the operator confirms.
  - MA-380 and MA-381 add a renamed folder, an NFD copy, an iCloud placeholder, a refused-link
    folder and a Rebuild with an injected `Add` error. Each must leave records, log lines and
    folder pages intact.
- **CC28 — An incomplete member stays findable** (D; L3-4, L6-6, L7-5).
  - R7 drops a member's record only when every R3 delete for it completed (the record's triple and
    every AlsoFiles triple), along with its R5 page items and R6 lines. Otherwise the record stays,
    with an additive `redaction:"incomplete"` field.
  - R4 still prunes the row, which errs toward removal from search.
  - D2's second set finds the member on the next run and retries the failed step. While such a
    record exists, status WARNs `redaction_unfinished`.
  - MA-380 injects error returns, not kills, at R3 (an `.eml` that cannot be removed), R5 and R6.
    The second reindex must complete the removal and be the only run that exits 0.
- **CC29 — Nothing outside `-out` is written** (D, A; L1-4, L6-1, L7-12, L10-10).
  - The descriptor-named log leaves A3's fixed set, D4's list, P12 and R-CONF-3.
  - The descriptor's `log` value is read only to name the log. When it resolves outside `-out`, the
    not-covered list and status say "your schedule's log <path> is outside this folder and was not
    changed".
  - `.mailarchive-schedule.json` joins §8.2's untrusted inputs and the §5 compromised column.
  - MA-382's outside-log case becomes a NoSideEffect assertion: bytes and mode are unchanged, and
    the summary names the file.
- **CC30 — Logs are rewritten in place** (D; L3-9, L5-5, L7-6).
  - R6 streams the filtered copy to `.mailarchive-logscrub-<name>.tmp` (0600, fsync). It then
    truncates the original through a handle opened under the archive lock and writes the filtered
    content back. A leftover temp file is completed next run.
  - The run's own `-log` is rewritten through the logger's own handle. On POSIX, O_APPEND writers
    keep the inode; on Windows no FILE_SHARE_DELETE is needed.
  - A sharing violation from a foreign handle defers that log, and the member stays incomplete
    (CC28).
  - The §5 row is corrected. MA-382 adds `-log` inside `-out` and a second open handle.
- **CC31 — Text-file matching and coverage** (D; L1-7, L6-7, L6-11, L3-8, L3-13, part of L10-10).
  - **Matching.** D4 matches exact stem tokens, and the Message-ID only in the bracketed `<id>`
    form the tool logs (minimum length, an `@` required). It also matches the exact subject, read
    from the index row before R4 (minimum length). The lookup is a hash set (or Aho-Corasick), not
    a scan per member.
  - **Sidecars.** `BACKUP-NEEDS-ATTENTION.txt` and `ARCHIVE-INTEGRITY-ATTENTION.txt` join the
    list. The last-run record is rewritten through `WriteLastRun`, so its sidecar regenerates.
  - **Activity ring.** The desktop clears its activity ring after a dashboard Rebuild.
  - **Unidentified pages.** An `index*.html` in a row-less directory that fails identification is
    itemized "check this file", and the run exits 1.
  - **Log rotation.** The desktop `-capture` log opens through `runlog.Open`, which rotates it and
    no longer duplicates it into stderr. `runlog.Open` also rotates `.stderr.log`. The size
    sentence becomes "rotated at open once past 8 MiB; lines are streamed".
  - MA-382 adds a `render "<subject>"` fixture line and a large-gone-set performance case.
- **CC32 — Scope the redaction claim** (D; L1-6).
  - R-CONF-3 and P11 cover "the redacted message's own records and every file derived from it".
    P11's sender and recipient bytes become "values unique to it".
  - P14, §7.6 and MA-383 add to the not-covered list: other kept messages that quote, forward,
    reply to or duplicate it.
  - An optional aid after R9 prints the stems of KEPT messages whose subject matches a redacted one
    once Re:/Fwd: are stripped. It never prints the removed subject.
- **CC33 — Scrub at O(deleted)** (D; L3-1, L3-3, L8-8, L9-3, L9-7).
  - Every read-write connection sets `PRAGMA secure_delete=ON`, and schema creation persists
    FTS5's `secure-delete` option.
  - A redaction in a policy-1 index then needs only `PruneForRedaction` plus a TRUNCATE
    checkpoint. `optimize`+`VACUUM` remains only for the one-time legacy scrub and for an explicit
    `reindex -compact`.
  - The `freelist_count` trigger is removed; §6.2's honest downgrade limit stays.
  - A fresh database (A2's create branch, Rebuild's new database) is stamped
    `scrub(pending=0, policy=1)`. The legacy scrub turns both options on.
  - A scrub that fails when nothing was pruned is a WARN with exit 0.
  - MA-379 adds a twin for this path, including rows inserted before the options were turned on,
    and a fresh-capture no-VACUUM assertion.
  - §7 states the measured `secure_delete` write and WAL cost.
- **CC34 — Legacy-scrub retry and space** (D; L3-2, L7-10, L9-4).
  - The scrub table records `attempted_at`, `last_error` and `needed_bytes`.
  - Before any VACUUM, a free-space pre-flight checks the archive volume and SQLite's temp
    directory (statfs / GetDiskFreeSpaceEx; reuse the workspace's `util.FreeBytes` if it lands
    first). Where SQLite allows it, the temp directory is pinned to the archive volume; otherwise
    both volumes are checked. When space is short the scrub is skipped with WARN
    `index_unscrubbed`.
  - Unattended runs retry at most once every 7 days. A manual reindex always tries.
  - A start line names the size and expected duration, and says "safe to interrupt; restarts next
    time".
  - Status distinguishes "pending" from "failed <date>: needs N GB free on <volume>".
  - MA-380 adds short-space and repeated-failure cases.
- **CC35 — Superseded copies** (D; L3-5). When recapture-integrity's v7 `Superseded` entries
  exist, every entry whose Key belongs to a gone member joins the gone set, and its stem is captured
  at R2. Recapture's `finishSuperseded` runs between R4 and R5. An entry its gates block is
  itemized, keeps the entry, and makes reindex exit 1. D4 matches the record's stem and every
  superseded stem.

## Owner and operator rulings (not imposed; each blocks the slice it names)

- **OQ-7 (new, from CC3) — Windows posture of hand-over copies.** This is the same question as
  the workspace's Q10, and one ruling answers both. For `extract -dest` roots and workspace export
  directories on Windows, the choice is between:
  - (a) Owner-only: a protected TokenUser/SYSTEM/Administrators DACL. A recipient on another PC
    needs admin rights or takeown. On a file share, "Administrators" means the server's admins.
  - (b) Inherit the destination's ACL. This is easy for the recipient, and the copy is readable by
    whoever can read the destination. A `Posture` line names that before the copy.

  Revision 2 describes (b), which matches the workspace. **Blocks:** the Windows half of
  `extract -dest` in slice A, and the workspace's export slices.
- **OQ-8 (new, from CC27) — Redaction intent.** Should the trace-erasing steps require explicit
  intent? The options are:
  - always: `reindex -redact`, or a dashboard confirmation listing folders and counts;
  - only beyond a threshold: a member whose folder is missing entirely, or more than N messages in
    one run;
  - never: today's "delete the files, then run reindex".

  Revision 2 describes the threshold, with N = 25. Below it, plain `reindex` redacts files deleted
  one by one from folders that still exist, as the README documents. Beyond it, reindex reports
  "N messages' files are missing from <folders> — if you moved or renamed them, put them back; to
  remove them for good run `reindex -redact`" and does nothing destructive. **Blocks:** slice D.
- **OQ-9 (new, from CC23) — The desktop dashboard's address.** The choice is between:
  - (a) Keep `127.0.0.1:8097`. A bookmark lands on the locked page. The owner accepts the stated
    residual: a hostile account that holds 8097 first can phish a user who types the address or
    uses a bookmark.
  - (b) Make the dashboard ephemeral like the reader. Entry is by shortcut only, bookmarks stop
    working, and 8097 leaves the RMM runbook. The failure is less legible for a user who types the
    old address.

  Revision 2 describes (a) with CC23's warnings. **Blocks:** slice C2.
- **OQ-1 (restated, from CC6/CC8) — Existing Windows archives.** CC6 already excludes drive roots,
  profile folders and folders that hold other files. What remains is this: after a completed
  MA-368 drill, should the tool rewrite an existing archive root's DACL on the first run, or only
  through the opt-in `access -restrict`? Built if not ruled: report-only plus the opt-in command.
  **Blocks:** slice A-W only.
- **OQ-6 (restated, from CC33/CC34) — The one-time legacy scrub.** It now applies only to indexes
  created before scrub policy 1, because fresh and rebuilt indexes are stamped. It runs only after a
  free-space pre-flight, with a weekly retry limit. The question is unchanged: automatic at the end
  of the first capture, or only in `reindex` with a status WARN? Built if not ruled: automatic, as
  in revision 1. **Blocks:** nothing beyond slice D's default.
- **OQ-10 (operator, from CC1) — Catalog id allocation.** Allocate R/S/X blocks across the
  in-flight designs, jointly with the workspace's Q11 and fault-isolation's OR8. **Blocks:** each
  slice's catalog-first PR.
- OQ-2, OQ-3, OQ-4 and OQ-5 stay open as written. OQ-4 now asks about the reduced ceremony that §7
  counts after CC18–CC20.

## Slice view

Conditions gate the slice they name. Re-reviewing one slice does not reopen the others. Each
slice's conditions are re-verified against revision 2's text before that slice's catalog-first
PR. CC27 adds a classifier and an intent rule, so slice D's re-verification also re-runs lenses
3, 6 and 7 on §4.D.

| Slice | Conditions | Blocked on |
|---|---|---|
| all (before any catalog-first PR) | CC1, CC2, CC4 | OQ-10 |
| A — private at rest | CC3 (POSIX, Posture line), CC6, CC7 (new roots), CC9, CC10, CC11, CC12, CC25 (§7.5), CC29 (A3 fixed set) | OQ-7 (Windows `extract -dest` only) |
| A-W — existing Windows archives (new) | CC7, CC8 | OQ-1 and a completed MA-368 drill |
| B — no link-following writes | CC12, CC13, CC14 | — |
| C1 — Host, allow-list, credentials | CC15, CC16, CC18 (Host rule) | — |
| C2 — per-account surfaces | CC3 (sign-in banner), CC5, CC17, CC18, CC19, CC20, CC21, CC22, CC23, CC24, CC25 (§7.2) | OQ-9 (and OQ-4, OQ-5 as before) |
| D — redaction that removes | CC6 (prune scope), CC26–CC35 | OQ-8 (and OQ-6's default) |

## Hidden blocker (rescue pass): "pruned" is not "redacted" (H1)

Slice D attaches its irreversible, trace-erasing steps to the tool's two existing **repair**
paths: R3 deletes, D3 page and folder removal, D4 line replacement and the R7 record drop. Those
paths prune for many reasons that are not redactions, and D adds no intent signal. The lenses found
only the row-versus-record case (F48, L1-1).

1. **Rebuild** is the dashboard's "Rebuild search index" button (`maintenance.go:75-92`), and the
   remedy the tool prints for a lost or corrupt index. It drops a manifest record in several
   cases besides a missing `.html` (`reindex.go:300-339`, read for this review):
   - a symlinked or irregular component;
   - an `.html` over 512 MiB;
   - a file that cannot be read (EACCES, or a Windows sharing violation from AV or an indexer);
   - an `idx.Add` failure, such as SQLITE_FULL on a nearly full archive volume.

   D5 says "Rebuild runs D3–D4 for the records it prunes", so every such live message would be
   treated as redacted:
   - Its log lines are replaced with the marker, including the run's own "rebuild: skipped … pruned"
     and "could not index" lines, which are the only evidence of what failed.
   - Its legacy-report row is removed.
   - Its folder page is deleted. Generate writes only folders that still have index rows
     (`pages.go:43-68`), and D3 deletes every tool page Generate did not write.
   - Its files stay on disk as manifest-less orphans that no later run can re-adopt.

   Today those records are already dropped. D would also remove the traces that let anyone find
   and recover them, label the result a redaction, and exit 0. It also contradicts B3, where the
   same symlink condition is a refusal that is "not recorded as a gap".
2. **reindex** is the remedy that status's `index_behind` (`health.go:179-181`) and the post-error
   summary (`main.go:1413`) prescribe. It takes every `.html` absent at its recorded path as an
   operator deletion. That also happens without any deletion:
   - when a user renames or moves a folder inside the archive;
   - when an archive that passed through HFS+ is copied to ext4 or a NAS (slugs keep NFC-normalized
     non-ASCII letters, `util/paths.go:60`, so a stat of the NFC name fails on the NFD copy);
   - when a sync client evicts files.

   This contradicts `README.md:435-436`: "The archive never deletes a message file on its own … a
   redaction is always something *you* do."

**Dissolved by CC27:** an explicit classifier (only ENOENT under a real parent directory, with no
normalization-equivalent or eviction-placeholder sibling); every other prune reason keeps the
record, exits 1 and never scrubs; Rebuild scrubs only "missing" records; D3 never deletes a page of
a directory that still holds a present recorded file; D4 never rewrites the current run's lines;
and a gone set beyond the intent threshold waits for explicit intent (OQ-8). The fix is a
classifier plus an intent rule, not a re-design. Steps R3–R10 are sound once membership means what
the README says it means.

## Findings

Severity is the skeptic's. Each rescue outcome (DISSOLVED / PARTIAL) is listed for the
findings the rescue was sent, which were every blocker and major.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| L1-1 | 1 | §4.D D2; D5 R3/R6/R7/R9; R24 | A stale row (its path gone) whose record points to a live file is treated as redacted: files deleted, traces scrubbed, record dropped, exit 0 (F48) | CONFIRMED (blocker) → DISSOLVED | D2 has no repair branch; `reindex.go:108-116` stats the row path, `:147-151` deletes `rec.Path`; `main.go:1413` advises reindex; recapture §1.8 PoC kept=0 pruned=3 | CC26 |
| L1-2 | 1 | §6.3 workspace bullet; R22 scope; §5 guard row | §6.3 says workspace writes use ArchiveFS and the protected DACL; the workspace says its own os.Root and "inherits the destination's ACL" — whichever is built second breaks the other | CONFIRMED (major) → DISSOLVED | §6.3 l.623-624 against workspace §4.9 and §8.5; ArchiveFS is `os.OpenRoot(out)` and cannot reach a destination | CC3 (OQ-7) |
| L1-3 | 1 | §2 header; §6.4; §9 Covers | R22/S42/S43/S44/X12 are already proposed with other meanings by four sibling designs; covers-map resolves by name, so a wrong-property trace passes | CONFIRMED (major) → DISSOLVED | fault-isolation :188, mime-fidelity :136, recapture :190, workspace :5/221/234; the catalog ends at R21/S41/X11 | CC1 |
| L1-4 | 1 | §4.A A3 fixed set; §4.D D4; §8.2; P5/R4 | The descriptor's `log` field (an in-archive, untrusted file) is chmod-ed every run and rewritten by reindex even outside `-out` | CONFIRMED (major) → DISSOLVED | `descriptor.go:91-92` "The file is untrusted"; §8.2 omits it; "through ArchiveFS" cannot reach outside `-out` (B1) | CC29 |
| L1-5 | 1 | P2; A3 whole tree and never; A5; §6.1 | Tightening walks all of `-out`; the exclusion covers only the root and only on POSIX; A5 has none → non-archive data in ~/Documents, a NAS or D:\ altered, irreversibly | CONFIRMED (blocker) → DISSOLVED | `atomic.go:106-108` WalkDir(out) with no skip list; A3 l.247-248; A5 l.265-271 "Windows propagates…"; §6.5 a revert leaves the DACL | CC6 |
| L1-6 | 1 | P11, P12, R24, P14, §7.6 | Kept replies, forwards and duplicates still hold the subject and quoted body, while R24 says no kept file holds its text | CONFIRMED (major) → DISSOLVED | R24 l.640-642 and P11 are not scoped to the message's own records; P14 and §7.6 are silent | CC32 |
| L1-7 | 1 | §4.D D4; D2; P12 | `render "<subject>"` errors reach the logs, the last-run record and BACKUP-NEEDS-ATTENTION.txt; D4 matches only stems and Message-IDs | CONFIRMED (minor) | `exporter.go:321,328`; `run.go:431`; `lastrun.go:116-138`; the sidecar is not in D4's list | CC31 |
| L1-8 | 1 | R23; P7 | R23 does not say a LAN serve sends its session in clear | REFUTED | §3.7: "TLS for a non-loopback `serve` (the session cookie of a LAN `serve` travels in clear; the existing warning stays)"; §8.5 repeats it — a conceded non-goal | — |
| L1-9 | 1 | P10 | P10's title overclaims: with `-unattended` it only warns | REFUTED | P10's cell: "`graph` refuses (and, with `-unattended`, warns and `status` WARNs)…"; C1 l.351-353 gives the reason | — |
| L1-10 | 1 | P7 conditional; C3; §8.4 | macOS is missing from the argv-race analysis | CONFIRMED (major) → DISSOLVED | `/bin/ps` is setuid root and `ps -axww -o user,args` lists other accounts' argv (verified, macOS 26.6.2); `openBrowserArgv` passes the URL to `open` (`main.go:165-167`) | CC5 |
| L2-1 | 2 | Header; §10; §11 | One gate bundles four separable slices, so a blocker on one holds all | REFUTED | §10: "B and C are independent and may be built in parallel", "No slice ships while its gate is open; each stops at its gate"; §6.5: "Each slice is independently revertible". This review records conditions per slice (Slice view) | — |
| L2-2 | 2 | §6.3; B1/B2; D2/D5; C1/C2 | Sibling designs build the same no-follow delete gate, Reindex table and session guard; whichever merges second regresses the other | CONFIRMED (major) → DISSOLVED | recapture :638-642 and :790 (`internal/archivefs`); workspace E0 (:1166, now its XC23); mime-fidelity :776 | CC2 |
| L2-3 | 2 | P3/A5; OQ-1; §10 slice A | The fleet-wide DACL rewrite of existing archives rides with the trivial POSIX fix and is validated only by a pending lab row | CONFIRMED (major) → DISSOLVED | §10 slice A contents; MA-368 tier L "pending"; §6.5: a revert leaves the DACL | CC8 (OQ-1) |
| L2-4 | 2 | §4.C4 steps 3–4 | The handoff opens the browser in the instance's own logon session, so a second RDS session sees nothing | CONFIRMED (minor) | C4 l.430-432 "in *its own* logon session" | CC21 |
| L2-5 | 2 | §4.C2 `/_open` | Strict plus a meta-refresh page adds a browser dependency; Lax plus 303 is simpler | REFUTED | C2 l.366-368 states why; MA-373 pins "a 200 page navigating same-origin (not 3xx)"; MA-377 validates the browsers. A preference between two working choices | — |
| L2-6 | 2 | D1 legacy scrub; OQ-6 | Every install pays a VACUUM for residue it may not have | REFUTED | OQ-6 l.876-879 puts the cost, the benefit and the alternative to the owner. The SQLITE_FULL and retry harms are L3-2/L7-10/L9-4 | — (see CC33, CC34) |
| L4-1 | 4 / 3 | A5; A3 whole tree and never; OQ-1; OMISSION: GUI actor in §7 | The GUI uses a picked D:\ or general folder as `-out`; the first upgraded run rewrites the drive's DACL or strips the modes of unrelated files | CONFIRMED (blocker) → DISSOLVED | `cmd/mailarchive-gui/main.go:397-398` uses the picked folder directly; `run.go:108` refuses no non-empty folder; A5 has no exclusion | CC6 |
| L3-1 | 3 | D1 "When Scrub runs"; §5 R9; §7.1 | Every reindex with freelist > 0, the steady state, runs optimize plus a full VACUUM: O(index) time, about 3× space, the lock held, exit 1 when space is short | CONFIRMED (major) → DISSOLVED | D1 l.469-474; no pre-flight. Filer re-run: with both secure options on, delete + TRUNCATE leaves 0 needles and survivors match | CC33 |
| L3-2 | 3 | D1 legacy scrub; OQ-6; §7.6 | A failed legacy scrub leaves policy < 1, so every scheduled capture retries a doomed VACUUM | CONFIRMED (major) → DISSOLVED | step 5 sets policy=1 only on success; no attempt record or backoff | CC34 |
| L3-3 | 3 | D1 scrub table; Rebuild | Fresh and rebuilt databases read as legacy and get a needless VACUUM | CONFIRMED (minor) | "a missing row means 'never scrubbed by policy 1'"; the table is created without a row | CC33 |
| L3-4 | 3 / 4 | D5 R3–R10; P13; B3 | A member whose R3/R6 failed is still pruned (R4) and dropped (R7); the next run finds nothing and exits 0 with the `.eml` left | CONFIRMED (major) → DISSOLVED | B3 l.319-320; R10; `reindex.go:131-160`; SweepOrphans never removes `.eml` (`atomic.go:113-125`) | CC28 |
| L3-5 | 3 | §3.9; A3; §6.2; §6.3; D2/R3 | recapture-integrity's v7 Superseded copies of a redacted message survive and stay served; P12 is false | CONFIRMED (major) → DISSOLVED | recapture :406-411, :425; §3.9 "No change to … manifest version" | CC35, CC2 |
| L3-6 | 3 | §6.1 MSI; C4 step 2 | The MajorUpgrade leaves old unauthenticated instances on 8097, and a new launch starts a second instance | CONFIRMED (major) → DISSOLVED | `Product.wxs:37` has no CloseApplication; `Install-RMM.ps1` has no Stop-Process; `build-release.sh:31` windowsgui | CC22 |
| L3-7 | 3 | C4 steps 2–4; §5 instance row | A stale instance file at every Windows login gets the healthy instance called wedged; a macOS cache purge allows two instances | CONFIRMED (minor) | `main.go:113` handles only Interrupt/SIGTERM; both files live in UserCacheDir | CC21 |
| L3-8 | 3 | D4 size claim | The desktop `-capture` log and `.stderr.log` are never rotated; naive matching is O(lines × members) | CONFIRMED (minor) | `cmd/mailarchive-desktop/main.go:66-68`: plain OpenFile and MultiWriter(os.Stderr, f) | CC31 |
| L3-9 | 3 | D4 temp+rename; §5 D4 row | A rename over an open log fails on Windows; on POSIX later lines go to an unlinked inode | CONFIRMED (major) → DISSOLVED | reindex holds `-log` (`main.go:325-336`, `:592-598`); `syscall_windows.go:395` has no FILE_SHARE_DELETE | CC30 |
| L3-10 | 3 | A3 "cannot"; A6 | `access_enforceable:false` is never cleared | CONFIRMED (minor) | no sentence in A3/A5/A6 clears or re-derives it | CC9 |
| L3-11 | 3 | A3 whole tree | A per-entry lstat on every export, forever | CONFIRMED (minor) | `atomic.go:113-117` calls Info() only for temp files today | CC11 |
| L3-12 | 3 | §2; §6.4; §9 | The same id collision as L1-3 | CONFIRMED (major) → DISSOLVED | as L1-3; fault-isolation :235-237, recapture :218 | CC1 |
| L3-13 | 3 | D3 legacy fingerprint | A page from an older template is never touched and not itemized; exit 0 | CONFIRMED (minor) | the fingerprint matches only `templates.go:53-54` | CC31 |
| L4-2 | 4 | P3; A5; §6.3; OMISSION: recipient walk in §7 | A protected DACL on hand-over roots locks the recipient out (NTFS USB drive, UNC share) | CONFIRMED (major) → DISSOLVED | the workspace says "the export inherits the destination's ACL"; §7.5 has no recipient row | CC3 (OQ-7) |
| L4-3 | 4 / 3 | §7.4; A5 RestrictNewRoot; §6.5 | New protected roots block a backup account's inherited ACE, so §7.4's claim is false | CONFIRMED (major) → DISSOLVED | A5: a protected DACL of "owner/SYSTEM/BUILTIN\Administrators" only | CC7 |
| L4-4 | 4 | C4 step 4; §7.6 desktop texts | A windowsgui binary has no surface for its refusals, so clicks do nothing | CONFIRMED (major) → DISSOLVED | `build-release.sh:31`; `main.go:28-31`, `:186-189` "a failure is silent" | CC20 |
| L4-5 | 4 | C1 Host port; C2 cookie; C3 Enter; §7.2 | An ssh -L tunnel is refused; a detached serve cannot mint links; xdg-open runs on a headless SSH session | CONFIRMED (major) → DISSOLVED | MA-371 refuses "127.0.0.1:<other port>"; C3 "When stdin is a terminal" | CC18 |
| L4-6 | 4 | §7.6 used link; C3; P7 | One attack-suggesting page answers used, expired and unknown tokens → alarm fatigue on the signal P7 relies on | CONFIRMED (major) → DISSOLVED | C2: "unknown/used/expired → 401"; serve opens and prints the same token | CC19 |
| L4-7 | 4 | A3 "cannot"; A6 | EPERM and a no-op chmod share one flag and an exFAT message; the RED remedy cannot succeed | CONFIRMED (major) → DISSOLVED | the A3 "cannot" text; A6's WARN and RED texts | CC9 |
| L4-8 | 4 | A6 RED; §6.1 | A fleet-wide RED for up to a week after rollout | CONFIRMED (minor) | `desktop/schedule.go:36-46`: weekly at 03:00 | CC11 |
| L4-9 | 4 | C2 session visibility | The Activity-log notice is invisible at rest and evicted during captures | CONFIRMED (minor) | `capture.go:48-79` ring; `dashboard.go:487-499` polls only while running | CC24 |
| L4-10 | 4 | A6; §7.6 remedies | CLI-only remedies are shown on the GUI and the desktop | REFUTED | "run any capture" works on both (GUI export, desktop "Archive now"); `redaction_unfinished` arises only after `mailarchive reindex`; `index_unscrubbed`'s remedy is L3-2/L7-10 | — |
| L4-11 | 4 | B3; P5/MA-369; OMISSION in §6.1 | A deliberately symlinked store stops backing up after the upgrade | CONFIRMED (minor) | B3 makes it a run-level error; §6.1 is silent; P5 allows `-out` itself to be a link | CC14 |
| L4-12 | 4 | C4 steps 1–3 | The per-account lock ignores `-config`/`-out`, so a second tenant's instance hands off to the first | CONFIRMED (minor) | C4 step 1: one lock per account | CC21 |
| L4-13 | 4 | §7.2; §7.5; C1 P10 | The scripted-API loss is Type II; the non-admin Windows inheritor is omitted; HeadlessCapture refuses while `graph -unattended` warns | CONFIRMED (minor) | §7.2 rates it I; §7.5 has only an admin row; C1 l.354-356 | CC25, CC16 |
| L5-1 | 5 | Header; §2; §6.4 | The same id collision as L1-3 | CONFIRMED (major) → DISSOLVED | header l.7-8; the workspace's X12 has a different meaning | CC1 |
| L5-2 | 5 | §10 order A→C1→C2→B→D; A1, A2, A6 | Slice A is written against slice B's ArchiveFS; meanwhile status says links are not followed while `exporter.go:302` still follows them | CONFIRMED (major) → DISSOLVED | A1 "via P5's `ArchiveFS`"; §10 slice A "Depends on —" | CC12 |
| L5-3 | 5 | §6.3 OMISSION (recapture); B1/B2; D2/D5; A3 | Two archivefs packages, two Reindex orders, and a version claim tied to v6 | CONFIRMED (major) → DISSOLVED | recapture :785-793, :919 | CC2, CC35 |
| L5-4 | 5 | §6.3 workspace bullet | Every §6.3 claim about the workspace contradicts its text: E0 may be built twice, it has its own route test and its own os.Root, it inherits the ACL, and it adds a second sign-in surface | CONFIRMED (major) → DISSOLVED | workspace :312-320, :551-560, :588, :1166, :299-336 (revision 1) | CC2, CC3 |
| L5-5 | 5 | D4; §5 D4 row | The rename runs over reindex's own open `-log` | CONFIRMED (major) → DISSOLVED | `runlog.go:32` O_APPEND; `main.go:578-598` | CC30 |
| L5-6 | 5 | C1 registry; §6.3 tests; MA-378 | Inner bare muxes escape the registry; MA-378 ships after the guard it certifies | CONFIRMED (minor) | `server.go:26`, `dashboard.go:83`, `setup.go:27`; §10 puts MA-378 in C2 | CC15 |
| L5-7 | 5 | C2 relative URLs; MA-372 | The absolute-URL list is incomplete, so the setup wizard dies under C2; a 301 Location drops the prefix | CONFIRMED (minor) | `setup_page.go:109,130`; `page.go:61`; `dashboard.go:336,436`; `goback.go:267,323` | CC17 |
| L5-8 | 5 | C4 step 3; A2 | The instance-file check and O_NOFOLLOW are POSIX-only | CONFIRMED (minor) | `securefile.go:93` skips the mode test on Windows; `lock_windows.go:12` noFollow = 0. CI does run windows-latest | CC21, CC13 |
| L5-9 | 5 | C1 P10; P2/A3/A6 | A fourth containment helper; an undefined result when the parent is missing; `-out` = $HOME refuses the default `graph` | CONFIRMED (minor) | `extract.go:621-674`; `graphconfig/config.go:51-52` | CC16 |
| L5-10 | 5 | C3 "press Enter" | A backgrounded `serve` is stopped by SIGTTIN | CONFIRMED (major) → DISSOLVED | Go leaves SIGTTIN at its default (stop) unless notified; no foreground check | CC18 |
| L5-11 | 5 | C4 port plan; OMISSION: setup/serve ports | `setup` stays on 8097 and collides with the desktop | CONFIRMED (minor) | `main.go:378` | CC18 |
| L5-12 | 5 | §6.1 MSI bullet | The packaging docs give fixed URLs, the cleanup misses the new state, and old instances keep serving | CONFIRMED (major) → DISSOLVED | `DEPLOYMENT.txt:2-3`; `README-RMM.md:107,150-156`; `Cleanup-CurrentUser.ps1:25-26` | CC22 |
| L6-1 | 6 | A3 fixed set; D4; §8.2 | A co-writer or a hand-over sender points the descriptor's log at ~/.ssh/authorized_keys; lines are replaced and the file chmod-ed | CONFIRMED (major) → DISSOLVED | `descriptor.go:91-94`; D2 accepts row-less records the attacker plants | CC29 |
| L6-2 | 6 | A3 "cannot"; A2; A5/A6 Posture | A foreign-owned `search.db` or history keeps streaming text to a co-account under a "drive cannot restrict" WARN; a Windows child keeps WRITE_DAC; Posture checks only the root | CONFIRMED (major) → PARTIAL | A2 checks only "a regular file"; A3 EPERM: "the run continues"; §5 "status checks the root" | CC10 |
| L6-3 | 6 | C4 port fallback; §6.1/§7.1 bookmark; §8.4 | A hostile first account squats 8097; later accounts' bookmarks reach a look-alike that phishes a device code | CONFIRMED (major) → PARTIAL | C4 step 2 falls back silently; §6.1 "A bookmark of http://127.0.0.1:8097/ reaches the locked page"; §8.4 lists only the `/_open` lure | CC23 (OQ-9) |
| L6-4 | 6 | A3 whole tree and never; MA-366 | With `-out` = $HOME or a shared volume, every file below it is tightened on every export | CONFIRMED (blocker) → DISSOLVED | A3 l.247-248 exempts the root only; `atomic.go:106-108` | CC6 |
| L6-5 | 6 | A1 TightenPath | Lstat-then-chmod by path follows a link swapped in after the check | CONFIRMED (minor) | A1's text; os.Chmod follows links | CC13 |
| L6-6 | 6 | D2; D5 R3/R4/R7; B3; P13 | An `.eml` that could not be deleted loses its row and record and is never found again | CONFIRMED (major) → DISSOLVED | as L3-4 | CC28 |
| L6-7 | 6 | D4 line matching | A short, sender-chosen Message-ID wipes unrelated log lines | CONFIRMED (minor) | D4 justifies the rule for stems only | CC31 |
| L6-8 | 6 | C2 "Why a secret path" | P is compared without constant time, and P is printed | REFUTED | C2: P and S are "in memory only", and refused responses carry "never `P`"; a wrong P gets the same locked page; a timing oracle on a 128-bit path over loopback HTTP is not practical; nothing prints `/s/<P>/` | — |
| L6-9 | 6 | C3 link delivery | The same token is opened and printed; stdout is not a private channel | CONFIRMED (minor) | C3's serve text | CC5 |
| L6-10 | 6 | A5 "owner"; Posture | Under elevation "the owner" is Administrators; children's broad ACEs are never checked | CONFIRMED (major) → DISSOLVED | A5 names no SID; Posture checks only the root | CC7 |
| L6-11 | 6 | D4 / P12 list OMISSION | BACKUP-NEEDS-ATTENTION.txt and the desktop activity ring keep stems | CONFIRMED (minor) | `lastrun.go:116-138`; the ring is outside R24's file scope, so that half carries no weight | CC31 |
| L7-1 | 7 | A3 whole tree; D3 prune walk | The unbounded walk chmods home and volume trees; D3 deletes a nested archive's pages | CONFIRMED (blocker) → DISSOLVED | `atomic.go:106-135`; D3: "every directory below the archive root" | CC6 |
| L7-2 | 7 | A5; P3; OQ-1 | A drive-root DACL rewrite; an undefined owner SID; self-lockout | CONFIRMED (blocker) → DISSOLVED | A5 has no never-list, names no SID and checks no effective rights | CC7, CC6 |
| L7-3 | 7 | A3 "cannot"; A6; P2/P4; §7.5 | macOS volumes that ignore ownership read GREEN while every account can read them | CONFIRMED (major) → DISSOLVED | §7.5: "macOS ignores ownership on external volumes by default" | CC9 |
| L7-4 | 7 | A3 "cannot"; A5 failure; A6 | One latched flag, the wrong cause and remedy; an RMM-created root stays RED forever | CONFIRMED (major) → DISSOLVED | as L4-7 | CC9 |
| L7-5 | 7 | D2 "in memory only"; D5; B3; P13 | Step failures (not crashes) lose the stems; the next run exits 0 with residue left | CONFIRMED (major) → DISSOLVED | §5 covers kills only | CC28 |
| L7-6 | 7 | D4; §5 D4 row | The run's own log and foreign handles; a Windows sharing violation on every run | CONFIRMED (major) → DISSOLVED | `main.go:325-336`; `syscall_windows.go:395` | CC30 |
| L7-7 | 7 | C4 step 3 | Checked literally, the Windows instance file is always rejected | CONFIRMED (minor) | `securefile.go:93`; CI runs windows-latest, so tests would catch it | CC21 |
| L7-8 | 7 | C4 step 4; §7.6 | Refusals are invisible on Windows | CONFIRMED (major) → DISSOLVED | as L4-4 | CC20 |
| L7-9 | 7 | §5 "No new single point of loss"; C3; §7.6 | A failed browser opener locks the user out of their dashboard | CONFIRMED (major) → DISSOLVED | `main.go:186-196`: the opener fails silently; the client exits 0 | CC20 |
| L7-10 | 7 | D1; §5 R9; OQ-6 | The legacy scrub's retry policy and blast radius are unstated | CONFIRMED (major) → DISSOLVED | as L3-2 | CC34 |
| L7-11 | 7 | C4 steps 1/3; §5 | A purged file, a double click or a slow start gets a healthy instance called wedged | CONFIRMED (minor) | C4: at most one handoff per second; a 5 s wait | CC21 |
| L7-12 | 7 | D4 outside log; §5; P5 | An untrusted descriptor drives a rename outside `-out` | CONFIRMED (major) → DISSOLVED | as L1-4; `lastfailure.go:67` treats the descriptor as untrusted | CC29 |
| L7-13 | 7 | D2/B2 existence check | A refused link classed as "gone" redacts a whole folder | CONFIRMED (minor) | `reindex.go:111-117`: only ErrNotExist is gone today; D2 leaves it undefined | CC27 |
| L7-14 | 7 | D3 Prune on every reindex/Rebuild | Prune deletes the pages of present messages when the index is incomplete | CONFIRMED (minor) | `pages.go:43-72`; `run.go:184-188` | CC27 |
| L8-1 | 8 | §6.1; C4; MA-377 | Old instances survive the MSI upgrade, and a duplicate starts | CONFIRMED (major) → DISSOLVED | `Install-RMM.ps1:5` against `Uninstall-RMM.ps1:9` | CC22 |
| L8-2 | 8 | C3; C1 Host rule; §6.1 | A detached serve and ssh tunnels regress silently | CONFIRMED (major) → DISSOLVED | as L4-5 | CC18 |
| L8-3 | 8 | B3; §6.1 | A store-level link stops that store's capture permanently | CONFIRMED (minor) | as L4-11 | CC14 |
| L8-4 | 8 | A5 stamp; §5 stamp row; A6 | A copied or restored Windows archive carries the stamp, so A5 never runs and the RED remedy loops | CONFIRMED (major) → DISSOLVED | the stamp lives in the manifest; §5's row is false on Windows | CC7 |
| L8-5 | 8 | §6.5; A5/OQ-1 | The DACL rewrite cannot be undone; inheritance is frozen; there is no fleet undo | CONFIRMED (major) → DISSOLVED | §6.5: "re-grant that principal explicitly" | CC8 |
| L8-6 | 8 | C1 P10 HeadlessCapture; §6.1 | A home-folder archive location silently stops the weekly backup | CONFIRMED (major) → DISSOLVED | `capture.go:25-46` refuses before RunGraph; `health.go:223` | CC16 |
| L8-7 | 8 | §6.3; P1/P3 | A protected DACL on hand-over destinations is a silent regression | CONFIRMED (major) → DISSOLVED | as L4-2 | CC3 (OQ-7) |
| L8-8 | 8 | D1 scrub table; A2; Rebuild | A fresh or rebuilt database gets the legacy VACUUM | CONFIRMED (minor) | as L3-3 | CC33 |
| L8-9 | 8 | §6.1 "any verb" against A3's list | §6.1 says any verb; A3 lists six and omits extract | CONFIRMED (minor) | `extract.go:121-124` holds the archive lock | CC11 |
| L8-10 | 8 | §6.1 OMISSION: packaging and operator docs | The cleanup script and the runbook are not updated | CONFIRMED (minor) | `Cleanup-CurrentUser.ps1:25-26`; `DEPLOYMENT.txt:2-3` | CC22 |
| L8-11 | 8 | C2 relative URLs | The same as L5-7 | CONFIRMED (minor) | the grep of `page.go:61` and the rest | CC17 |
| L9-1 | 9 | C3/C4; §7.6 locked page | A failed opener is a silent lockout | CONFIRMED (major) → DISSOLVED | as L7-9 | CC20 |
| L9-2 | 9 | C4; §5 instance row | Load-bearing state sits in a purgeable cache directory, and the cleanup misses it | CONFIRMED (minor) | os.UserCacheDir; `Cleanup-CurrentUser.ps1` | CC21 |
| L9-3 | 9 | D1 freelist trigger; §6.2 | freelist > 0 is the steady state; a fresh database reads as legacy | CONFIRMED (major) → DISSOLVED | Add deletes and re-inserts by key (`index.go:192`); scheduled reindex jobs exist (`job.go:194`) | CC33 |
| L9-4 | 9 | D1 legacy scrub in a 3am capture; §5 R9 | No pre-flight; the temp copy lands on the system volume; the retry is unspecified | CONFIRMED (major) → DISSOLVED | as L3-2; Hourly schedules exist (`schedule.go:32`) | CC34 |
| L9-5 | 9 | §6.1; §7.4; §10 slice C2 | The shipped runbook breaks, so §7.4's "0 new steps" is untrue | CONFIRMED (minor) | `Product.wxs:91` installs DEPLOYMENT.txt | CC22 |
| L9-6 | 9 | C3 "press Enter"; §6.1 LAN serve | A serve with no TTY has no way to mint a link | CONFIRMED (major) → DISSOLVED | as L4-5 | CC18 |
| L9-7 | 9 | D1 `secure_delete` "(belt)" | The permanent write and WAL cost is unquantified | CONFIRMED (minor) | D1 calls it a belt; §7 states no cost | CC33 |
| L10-1 | 10 | Header; §2; §6.4 | The id collision | CONFIRMED (major) → DISSOLVED | as L1-3 | CC1 |
| L10-2 | 10 | §6.3 workspace bullet | §6.3 misstates the workspace, and the E0 dependency is unrecorded | CONFIRMED (major) → DISSOLVED | workspace :587-589, :1050-1052, :834-836, :1166 (revision 1) | CC2, CC3 |
| L10-3 | 10 | P7 condition; C3; §8.4 | macOS is missing from the argv condition | CONFIRMED (major) → DISSOLVED | as L1-10 | CC5 |
| L10-4 | 10 | §8.5; P7 | "Loopback traffic is visible only to administrators/root" is stated unconditionally; capture drivers can grant non-admin capture | CONFIRMED (minor) | the §8.5 sentence; Npcap, ChmodBPF and wireshark-group install options | CC4 |
| L10-5 | 10 | P5 label; B2; §8.6 | Path-based opens (search.db, the run log, chmod) can be redirected outside `-out`; junctions | CONFIRMED (minor) | B2 concedes "SQLite opens by path"; `runlog.go:22-32` | CC13 |
| L10-6 | 10 | A3 whole tree; P2 | WalkDir does not descend into a symlinked `-out` | CONFIRMED (minor) | verified on go1.26.9: 1 entry against 3; `atomic.go:108` | CC11 |
| L10-7 | 10 | §2 label definition; B1 toolchain | CI is a three-OS matrix and the toolchain floats; the POSIX rows are not gated | CONFIRMED (minor) | `ci.yml:19`; `go.mod` has no toolchain line | CC4 |
| L10-8 | 10 | P7 label | The browser half is labelled proven, but only MA-377 (lab) exercises it | CONFIRMED (minor) | P8 carries "lab-pending MA-377"; P7 does not | CC4 |
| L10-9 | 10 | P1; A4 | The `.stderr.log` creation claim rests on launchd and cmd.exe behaviour | REFUTED | A4: "`schedule --install` pre-creates both logs `0600` … existing plists get it on the next `--install`; until then `runlog` tightens each run"; MA-365 asserts the pre-create | — |
| L10-10 | 10 | D4 outside log; size cap | The outside-log primitive is undesigned, and "capped" is inaccurate | CONFIRMED (major) → DISSOLVED | B1 is rooted at `-out`; `runlog.go:27-31` rotates only at open | CC29, CC31 |
| H1 | rescue | D5 "Rebuild runs D3–D4 for the records it prunes"; D2; D3; D4 | Every prune is treated as a redaction: Rebuild's non-missing prune reasons (link, > 512 MiB, unreadable, Add error) and reindex's moved, renamed, NFD-copied or evicted files get the irreversible trace scrub | CONFIRMED (blocker) → DISSOLVED | `reindex.go:300-339` prune reasons (read for this review); `maintenance.go:75-92` dashboard Rebuild; `pages.go:43-68`; `util/paths.go:60` NFC slugs; `README.md:435-436` "a redaction is always something *you* do" | CC27 (OQ-8) |
| F1 | filer (cross-check of fault-isolation's hidden blocker) | B1 MkdirAll "real directory"; B1 OpenNoFollow and P9 "regular file" fstat checks; A2 | On Windows, cloud placeholders (OneDrive Files-On-Demand) are ModeIrregular, so in a synced archive the reader 404s online-only pages and the history log or search.db is refused; L10-5's suggested `Type() != ModeDir` rule would also refuse cloud-tagged folders (ModeDir plus ModeIrregular) | CONFIRMED (major; filer) | go1.26.9 `src/os/types_windows.go:193-229`: only SYMLINK, AF_UNIX and DEDUP tags escape ModeIrregular, and name surrogates lose ModeDir; status supports archives under sync folders with a WARN (`health.go`, OutUnderSync) | CC13 |

## Mechanical check

`docs/design-local-confidentiality.md`'s BUILD STATUS reads "approved with conditions, not
built". It cites this review, which exists. Revision 2 folds CC1–CC35 into the design text, each
marked `[CCn]`, and lists the open rulings in §11. Each slice flips its own status line and cites
its gate artifacts as it lands. Slices A, B and C1 owe an adversarial pass; A-W, C2 and D owe
an adversarial pass and a friction review. A owes a friction review of its status and migration
wording.
