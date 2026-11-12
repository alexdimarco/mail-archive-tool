# Fold check 2: the M1 to M10 folds of the Thunderbird completeness design

**Verdict: FAIL (narrow), one must-fix.** M1 to M7, M9, M10 and the Gmail-detection fold are
concrete and correct. M8 is right in its rule but adds one escape phrase: the encrypted Run now
note may say "Backup complete:" when "the run had no Thunderbird IMAP store". The design never
defines "IMAP store" by verdict. The only test for it in code is `IsImapStore`, a path check, and
the TC-5 fold retired that test for exactly this decision. Read that way, an encrypted run of a
store with no matched account (path 1 of the six), or of an Exchange store, leads "Backup
complete:" above a "can't tell" line. The fix is one phrase (**MF1**, §3). With MF1 applied,
§17's headline rule admits no path to "complete" for the six formerly-partial paths, nor for the
M6-residual case.

**What may start.** C0c may start now. C1a and C1b may start: M2 to M4 and M5 to M7 pass, and the
M4 source re-read is recorded (437b6c1). **C3 waits for MF1.** C5 is unaffected (M10 passes).

- **Date worked on:** 7 November 2026 (the session's date).
- **What was checked:** `docs/design-thunderbird-completeness.md` at **58e2542** (git file id
  1f94bf8caf4f1efa321312084e2a02d9ffb0a8c5): the M1 to M10 notes, the Gmail-detection note, and
  §17's closing note. The fold list came from `docs/review-thunderbird-completeness-foldcheck.md`
  (92b80f8) §7. The M6-residual and M4 notes added after 58e2542 (7126111, 437b6c1) were read only
  for §2 below.
- **Independence:** this checker did not write the M folds, the design or the first fold check.
  It **did** write the M6-residual note (Job 1, 7126111) and the M4 re-read (Job 2, 437b6c1) in
  this same session. So the residual row in §2, and M4's re-read itself, are checked by their
  author, not independently. The first fold check and the caller's brief were read before the
  design.
- **Procedure:** read-only for the design. MailArchive code was read in the worktree at the lines
  the folds cite. Thunderbird source was read at ESR 153.6.0esr and Release 159.0
  (`docs/lab/thunderbird-completeness-source-esr153.md`). No test, product code or Thunderbird was
  run.
- **Register:** a finding report. The verdict, MF1's wording and the notes' suggested fixes are
  the checker's opinion.

## 1. Per item

PASS means the text is concrete (a builder can write the test from it) and correct (it removes
the failure the first check named, and the cited code says what the fold says).

| Item | Result | Checked |
|---|---|---|
| **M1** `prefs.js` through `util.ReadRegularFile` | **PASS, note A** | `ReadRegularFile(path, max)` at `internal/util/openregular.go:67` uses `Lstat`, refuses a link or a non-regular file, and caps the size. `parsePrefs` reads with `os.ReadFile` at `thunderbird.go:63-64` and is called by `FindAccountForStore` (`:82`), so the scheduled-backup hang is closed and a refusal leads to no account, so `unknown`. MA-1195 gains the pipe and link cases. **Note A:** the fold says "in `parsePrefs` and so for every caller", and itself cites `EnableOffline` at `:172`. But `EnableOffline` does its own `os.ReadFile` (`:172`) and `os.WriteFile` (`:195`, `:198`); it does not call `parsePrefs`. It runs attended only (wizard, `-enable-offline`), so this is no "complete" path. Fold with C0c: move that read onto `ReadRegularFile` too, so a link at `prefs.js` is never read or written through |
| **M2** one key-matching rule | **PASS, notes B and C** | One rule, no contradiction left. Exact mode needs any exact key; an old key beside an exact one is ignored; two exact keys → `unknown`; an old key alone for a folder still in the store makes a "can't tell" folder, which is never `caught_up`; tail mode only with no exact key. The six MA-1170 fixtures cover each branch. The Gmail All-Mail case is a floor. The condition is now proven at ESR 153 in source, with a clean-close limit (437b6c1). **Note B (C1a's adversarial pass):** the `Lstat` path is built from the store folder plus the key's tail, and the key is untrusted. Normalising should refuse a tail with `.` or `..` parts, or an absolute tail, before the `Lstat`. Otherwise an existence check can reach outside the store (nothing is opened, so it is low). **Note C (C1b):** M2 does not say what happens to a mailbox the walk read that has **no** key at all in exact mode (for example a folder left on disk after it was unsubscribed, §11). For a non-Gmail account it changes nothing, because only cache-listed kept folders are summed. For Gmail its messages enter X and can cover a gap, like N5. Suggest: leave such a mailbox out of X, and count it in the record |
| **M3** a broken `flags` counts as kept | **PASS** | Any entry whose `flags` fails validation → `unknown` (`invalid_entry`). A broken other field is skipped only when `flags` is valid and says not kept. MA-1171 gains the case. The root entry is covered too, which is the cautious reading |
| **M4** re-read at ESR 153, naming Release 159 | **PASS** (the re-read is not independent, see above) | The lab citations hold. `presync-attended-start-windows-run1-2026-10-31.md:13` (153.6.0esr, Windows); `restore-thunderbird-macos-2026-11-06.md:23` (159.0, Mac) and `:81` (153.6.0esr, the Linux leg). Every "ESR 140" is marked superseded, in §0, §4.1 step 3, §6, §12, §13 and §16. The re-read itself is now recorded (437b6c1): every claim holds except the account labels, which §6's new note corrects |
| **M5** All Mail is the one folder with 0x8000 | **PASS** | In source, Gmail's All Mail gets AllMail and Archive, and a server `\Archive` folder gets Archive only (`nsImapMailFolder.cpp:1161-1164` at ESR 153; `:1237-1239` at 159). An identity's archive folder gets Archive (`nsMsgAccountManager.cpp:1546` at ESR 153). With none or several 0x8000 folders, Y is a floor, so `behind` or `unknown`. The first check's example (Archives 50,000 on disk, Sent 30,000 not) now gives a floor of 50,000 and X ≥ floor − tolerance, so `unknown` and "Backup finished.". MA-1176 gains the fixture |
| **M6** `not_applicable` narrowed | **PASS** | Only a matched `pop3`/`none`/`rss` account, or no profile and no `ImapMail` part. Every case the first check named lands on `unknown`. The one case it left (a store outside every profile and every `ImapMail` folder) is closed by the M6-residual note (7126111; see §2). One reading made explicit there: "the input's path" means the store's own path, since an `-input` of a parent folder can hold a store whose path has `ImapMail` |
| **M7** "complete" words need `expungedBytes` = 0 | **PASS** | The rule is stated in §2.4, §4.2, §11, §12 and §17, and the false "fails toward behind" claim is marked superseded. The skewed-size example now leads "Backup finished." with both numbers. In source, `expungedBytes` counts on-disk bytes by two paths (`MarkPendingRemoval`, `nsImapMailFolder.cpp:1302-1328`; `AdjustExpungedBytesOnDelete`, `nsImapMailDatabase.cpp:49-58`, both at ESR 153), so the rule's premise holds. N5's excess with `expungedBytes` = 0 is still open, as the first check placed it (MA-1190) |
| **M8** the encrypted Run now note | **FAIL → MF1** | The surface is now named (`encryptedSummary`, `internal/wizard/jobrun.go:229-237`; it still begins "Backup complete:" today). It takes the weakest-account rule from the run's in-memory result, is cleared by Lock, and MA-1181 and MA-1186 gain the rows. The two citations (`:229-237` in the prose, `:231-237` in the table) are both right: the comment, then the function. **But** the row's "or the run had no Thunderbird IMAP store" is an escape the §4.2 rule does not have (MF1, §3) |
| **M9** no "everything" when stale; `stale` and `complete` in `-json` | **PASS** | The stale sentence ("…matched what is on this computer… It has not counted since…") has no "everything", and R42 is not amended, so there is one rule. `complete` uses the exact test (`caught_up`, missing = 0, not stale, no cap note, `expungedBytes` = 0), is true for `not_applicable`, and is false wherever the words may not say complete. MA-1183 gains both fields |
| **M10** the behind streak's own record | **PASS** | `RunHistoryMax = 50` (`internal/state/runhistory.go:26`). The record is modelled on `.mailarchive-syncstreak.json` (`internal/state/syncstreak.go:15-60`), with a serial, a record id, per-account first and last behind times, X then, and two sent flags; no folder names and no Y history. Plaintext only, and a keyed run removes a stray file, as presync's already does (`internal/app/syncstreak.go:38-40`), so MA-904 passes unchanged. MA-1184 gains the hourly 4-day run |
| **Gmail detection** `is_gmail` only, both designs | **PASS** | The single-copy lookup runs only when `GetIsGMailServer` is true (ESR 153 `nsImapMailFolder.cpp:8860-8867`). `is_gmail` is a server setting (`nsImapIncomingServer.cpp:302`), set from the server's Gmail capability (`:2050`), so a never-connected profile has none. That gives the per-folder rule and a false "behind", which fails safe. R43 and MA-1175 agree. The "or the AllMail flag" half is marked superseded in §3 and §17 item 16 |

## 2. The headline rule, path by path (§17, as amended)

The rule: a run says "Export complete." (or, in the encrypted Run now note, "Backup complete:")
only when every account may say complete. That means `caught_up`, no "can't tell" folder,
missing = 0, not stale, no cap note and `expungedBytes` = 0, or the account is `not_applicable` as
M6 (and now the residual note) narrow it. Otherwise the weakest account sets the lead.

| # | Path | Plaintext wizard and Run now | Encrypted Run now note | `status -json` `complete` | Result |
|---|---|---|---|---|---|
| 1 | no matched account, no readable cache | `unknown` → "Backup finished." (M6) | **"Backup complete:" if the store counts as "no Thunderbird IMAP store"** (MF1) | false | holds, **except** on the encrypted note |
| 2 | Archive-only label stands in for All Mail | floor → `behind` or `unknown` (M5) | weakest rule → not "complete" | false | holds |
| 3 | deleted copies smaller than average | "Backup finished.", both numbers (M7) | the same | false | holds |
| 4 | `encryptedSummary` always "Backup complete:" | — | weakest rule (M8), **but** the MF1 escape | — (nothing written, O1) | holds **after MF1** |
| 5 | stale `caught_up` says "everything" | dated words, "Backup finished." (M9) | the same lead | false, `stale` true | holds |
| 6 | broken `flags` drops a folder | `unknown` (`invalid_entry`, M3) | weakest rule | false | holds |
| R | IMAP store outside every profile and every `ImapMail` folder (residual, Job 1) | `.msf` present → `unknown` (`no_match`) → "Backup finished."; no `.msf` → `not_applicable` | weakest rule; **MF1 applies here too** | false with `.msf` | holds after MF1 (self-checked, see Independence) |

Why the residual rule is sound: Thunderbird finds an IMAP store's folders by listing its `.msf`
files (`nsImapMailFolder::CreateSubFolders`, ESR 153 `nsImapMailFolder.cpp:404-508`). So every
store Thunderbird keeps as IMAP holds at least one, and none can reach `not_applicable`. The one
stated limit is an IMAP folder copied without its `.msf` files and outside any `ImapMail` folder,
which nothing on disk ties to Thunderbird.

No user-facing sentence says "caught up". In `-json`, `verdict: "caught_up"` now always sits
beside `complete`, so a script never has to infer it.

## 3. Must-fix

- **MF1 (M8; before C3).** In the M8 table row (§7 fold), replace "or the run had no Thunderbird
  IMAP store" with: "or every Thunderbird store of the run is `not_applicable` (a run with no
  Thunderbird store keeps today's words). This is decided by the accounts' verdicts (§2.2 fold),
  never by `IsImapStore` or the path." Make the same change to MA-1181's original "they do for
  `caught_up` and non-IMAP" (read: "…and for `not_applicable`"), and to the §7 surfaces table's
  "`caught_up` or non-IMAP" (the wizard-lead fold already narrows that one; one line saying so is
  enough). MA-1181 and MA-1186 gain: an encrypted run of an unmatched store and of an `ews` store,
  each → never "Backup complete:".

## 4. Notes (low; fold with the slice named)

- **A (C0c):** `EnableOffline`'s own read and write of `prefs.js` (§1, M1).
- **B (C1a's adversarial pass):** refuse `.`, `..` and absolute tails before M2's `Lstat` (§1, M2).
- **C (C1b):** a walked mailbox with no cache key at all, in exact mode: leave it out of Gmail's X
  (§1, M2).
- **D, seen in passing and not judged here:** a `caught_up` account with not-kept folders still
  leads "Export complete.", beside a not-kept WARN (and, after TC-19, beside a note). R42 says
  never "complete" "while Thunderbird's own counts show mail not yet downloaded", and a not-kept
  folder's mail is not downloaded. The 10-lens review and the first fold check accepted this as
  "reported alongside" (§4.2, §6). It is outside this check's six paths. The lead may want one
  sentence in §4.2 saying whether a not-kept folder with mail keeps "Export complete.".

## 5. Counters

- **Date worked on:** 7 November 2026, from the session clock.
- **Register:** a fold-check finding report; the verdict and the suggested wording are opinion.
- **Drew on, and reached:**
  - the design at 58e2542 (file id above): yes, in full;
  - the first fold check at 92b80f8: yes, in full;
  - MailArchive code: `internal/thunderbird/thunderbird.go:15-60`, `:63-64`, `:81-82`,
    `:170-198`; `internal/util/openregular.go:67-73`; `internal/wizard/jobrun.go:212-237`;
    `internal/state/runhistory.go:26`; `internal/state/syncstreak.go:33`, function list;
    `internal/app/syncstreak.go:25-60`; `internal/source/source.go:60-160`;
    `internal/tbrestore/names.go:95-140`: yes;
  - the lab notes cited by M4: the lines named, yes;
  - Thunderbird source at ESR 153.6.0esr and Release 159.0 (the M4 re-read's downloads), and the
    trunk copy at ab60cc5e: yes, for the lines named.
- **Not checked, per claim:**
  - MF1's failure is a reading of the design's words, not code; no builder has written it yet.
  - Note C's Gmail effect was not measured on a real profile.
  - Whether a folder created during a Thunderbird session has its `.msf` before the first cache
    write (it affects M2's "can't tell" folder): not traced.
  - The gmail-dedup design was not re-read to confirm it adopted `is_gmail` only; the design
    says the lead is telling it.
- **The smallest check that would settle the most:** the lead reads the M8 row's last clause
  against §4.2's run-lead rule and picks one. It is a one-line edit.
- **Borrowed lines:** short quotes from the design, the first fold check, and the cited code and
  source, each attributed where it appears.
- **Drift:** one item outside scope is recorded as note D, marked not judged.
- **Touched:** this file (new) and one commit. No design text and no product code was changed by
  this check (Jobs 1 and 2 changed the design in their own commits).
- **Persona:** none.
- **Source reached, per source:** design yes; first fold check yes; repo code yes (as listed);
  Thunderbird ESR 153 and Release 159 yes (as listed); trunk local copy yes; gmail-dedup no;
  owner profile no; Mozilla bug tracker no.
