# Fold check: Thunderbird completeness design, after the 10-lens review

**Verdict: FAIL (narrow).** Most of the 24 conditions are folded well enough to build from. But the
design's own closing claim (§17: "every path to 'can't tell' … none prints 'complete'") is not yet
true. Six paths can still print "Export complete.", "Backup complete" or "everything":

1. a store with no matched account and no readable folder cache is called `not_applicable`;
2. a Gmail label that carries only the Archive flag can stand in for a hidden All Mail;
3. deleted copies that are smaller than the folder's average size beat the TC-16 estimate;
4. the encrypted Run now note (`encryptedSummary`) always says "Backup complete:";
5. a stale `caught_up` account still says "everything it lists";
6. a folder-cache entry whose `flags` is broken is skipped, so its gap is never counted.

Each is a small change to the design text. None needs a re-design. **C0c may start now**, with one
extra item (M1). **C1a waits** until M2 to M4 are folded, and C1b waits for M5 to M7. The other
items can be folded in the same pass and must land before the slice they name. Re-check only the
must-fix items, not the whole file.

- **Date worked on:** 7 November 2026 (the session's date).
- **What was checked:** `docs/design-thunderbird-completeness.md` at commit 31bd018 (branch
  `tb-completeness`), sha256 `7a59045ff2a8ec9247bf18ee268d7eda6d8993f3eb0884d9a6ecb359256955dc`,
  against `docs/review-thunderbird-completeness-10lens.md` (22330a8).
- **Independence:** this checker did not write the design, the review or the folds. It read the
  review first, then the design end to end. The fold summary in the caller's brief was seen before
  the design was read.
- **Procedure:** read-only. No product code was changed or run, and no test or Thunderbird was run.
  MailArchive code was read in the worktree. Thunderbird source was read from the review's
  downloaded trunk copy (ab60cc5e) in the main checkout's untracked `.claude/tbsrc-review/`, not
  fetched fresh.
- **Register:** a finding report (code and source read), plus the checker's opinions where marked.

## 1. Per-condition results

FOLDED means the text can be built from, and it removes the failure the review named. PARTIAL
means one piece is missing (named). NOT_FOLDED: none.

| Id | Result | Note |
|---|---|---|
| TC-1 | **PARTIAL** | The floor rule is right. But All Mail is found by "AllMail 0x8000 **or** Archive 0x4000". In Thunderbird's source, Gmail's All Mail gets **both** flags (`nsImapMailFolder.cpp:1230-1231`). Archive alone comes from a `\Archive` server folder or from the identity's archive folder (`:1232`; `nsMsgAccountManager.cpp:1541-1542`). So with All Mail hidden, Thunderbird's own "Archives" label can stand in for All Mail. Example: Archives 50,000 (all on disk), INBOX 10,000 (all on disk), Sent 30,000 (nothing on disk). Archives passes the "at least the largest other folder" test, Y = 50,000, X = 60,000, so missing = 0 and the run says "Export complete.". This is TC-1's own failure, reopened. **M5** |
| TC-2 | **PARTIAL** | The missing, string, out-of-range and negative cases are closed. One gap: when `flags` is the field that fails, "kept or not" cannot be told, and "a not-kept entry that fails is skipped" lets a never-downloaded folder with a broken `flags` drop out unseen. Since nothing on disk means the walk never sees it either, the gap is hidden. **M3** |
| TC-3 | **PARTIAL** | The Panorama signals (file checked with `Lstat`, or the pref) are buildable, and the check runs before the stale words. But the key rule contradicts itself. Item 3 says tail keys are used only when no exact key exists. Item 4 says "one exact plus one tail → `unknown`". If item 4 wins, every moved or copied profile is `unknown` **for good**, because old-path keys are removed only when a folder is deleted (review §2). If item 3 wins, a kept folder that so far has only an old-path key is invisible, so its gap is hidden. **M2** |
| TC-4 | FOLDED | The formula is correct at 15 → 1, 100 → 10, 10,000 → 20, 238,169 → 476. X = 0 with Y > 0 can never pass, by the formula alone. Clamping each folder, then applying the tolerance once per account, is stated. "Complete" words need missing = 0. One small ambiguity: see N4 |
| TC-5 | **PARTIAL** | Sorting by `prefs.js` `type` is right, and Exchange (`ews`) → `unknown`. But the §2.2 fold's last row ("no matched account **and** no folder-cache entry → `not_applicable`") is reached by an IMAP store whenever the account cannot be matched and the cache is missing or unreadable. That covers: C0c's own "ambiguous → no account"; a missing or unreadable `prefs.js`; an old `panacea.dat` profile; a cache refused as a link, a pipe or over 16 MB; and an `-input` copy of `ImapMail/<host>` with no `prefs.js` above it. "Has a folder-cache entry" cannot be known when the cache cannot be read. `not_applicable` keeps "Export complete.", which contradicts §4.4's "file missing → `unknown`". **M6** |
| TC-6 | FOLDED | O1 is folded in all places (§5, §7, §9.2, §9.3, R44, MA-1186, MA-1188); C4 is retired; the guard is MA-904. A separate miss on the encrypted screen path is N2 (**M8**) |
| TC-7 | FOLDED | Names come from the key path, are cleaned, capped at 80 characters, the list stops at 10, the Desktop uses `html/template`, and email carries no names. Note: `util.StripControl` (`paths.go:162`) does not remove bidi characters, but `util.StripBidi` (just below it) already does, for U+202A to U+202E and U+2066 to U+2069. Extend that one helper with U+200E, U+200F and U+061C, rather than adding a second. Clean the account name (the store folder name) the same way |
| TC-8 | FOLDED | One note for not-kept Gmail labels, only when All Mail sets Y. A floor case keeps the WARN, which is correct |
| TC-9 | FOLDED | `autosync_offline_stores` → `limited`, decided before the never-checked rule; neutral remedy until MA-1191. Citation holds (`nsAutoSyncManager.cpp:819-825`, `mailnews.js:621`) |
| TC-10 | FOLDED | C0c: match `directory` / `directory-rel` by whole path parts, fall back to `hostname` only when exactly one server has it, and never guess. The current loop (`thunderbird.go:97-104`) confirms the bug, including a `HasSuffix` test on `directory-rel`. See M1 for C0c's one addition, and M6 for what "no account" must lead to |
| TC-11 | FOLDED | The whole-store rule uses the input `path`, and a partial input → `unknown` with no WARN. The copy-first claim is corrected (`run.go:1631-1634` holds) |
| TC-12 | **PARTIAL** | The oldest time, a future time counted as stale, ≤ 0 as never checked, and "Backup finished." when stale are all folded. Missing: the stale `caught_up` words still say "everything". The §2.3 fold says "As of DATE, Thunderbird had downloaded everything it listed", and the §7 fold says "as written, plus 'as of DATE'". The amended R42 and §4.2 fold forbid "everything" when the count is stale. Also, `-json` keeps `verdict: caught_up` with no stale marker. **M9** |
| TC-13 | FOLDED | Words picked by the `compatibility.ini` version, read with the same opener (64 KB cap), with neutral words until the switch-over version is recorded. The ESR target is wrong (see TC-14 / M4) |
| TC-14 | FOLDED, target wrong | There is now an owner, a trigger and a gate (before C1a merges). But it re-reads "ESR 140". This repo's own labs ran **Thunderbird 153.6.0esr** on Windows, Linux and Mac (`thunderbird.go:51`; `docs/lab/presync-attended-start-windows-run1-2026-10-31.md:13`; `docs/lab/restore-thunderbird-macos-2026-11-06.md:81`, which also names Release 159). **M4** |
| TC-15 | FOLDED | `util.ReadRegularFile` at `openregular.go:67` does what the fold says: it uses `Lstat`, refuses a link, opens without blocking, and refuses a file over the cap without reading it whole. Token-by-token decoding catches a duplicated key. On Windows, the safety rests on the check before the open, as the helper's comment says. Acceptable |
| TC-16 | **PARTIAL** | The estimate ⌈expungedBytes ÷ (folderSize ÷ listed)⌉ uses the folder's **average** size. Deleted copies smaller than average beat it. Example: 100 listed, about 1 MB on average (a few large attachments), 50 deleted copies of 10 KB each, so 500 KB `expungedBytes`. The estimate is 1, but the real figure is 50. With 49 messages never downloaded: X = 51 + 50 − 1 = 100, missing = 0, "Export complete.". Also, `expungedBytes` is counted in offline (on-disk) bytes (`nsImapMailFolder.cpp:1388-1395`), while `folderSize` is in server bytes. So the estimate is not an upper bound, and the "fails toward behind" claim in step 5 holds only for the 0x0008 half. **M7** |
| TC-17 | FOLDED | The words depend on whether a schedule exists; the notices only follow scheduled runs anyway |
| TC-18 | FOLDED | C6 and MA-1187 are out; nothing in `internal/presync` changes |
| TC-19 | FOLDED (note) | WARN once, then a note, keyed on a hash. Note: the run writes a fresh "running" record **at its start** (`run.go:1210`, `:1295`), before the walk. So "read the previous record" must happen before that write, and `newRunRecord` should carry the hashes forward, the way it carries `Program` (`run.go:1221-1227`). Otherwise a failed, crashed or `unknown` run in between drops the hash, and the WARN repeats. That is noise, not a false "complete" |
| TC-20 | FOLDED | At most one email of each kind per streak, the stopped email never before the still-downloading one, and the reading flagged to the owner. How it is stored is N3 (**M10**) |
| TC-21 | FOLDED | Read once, after the walk, one read per profile |
| TC-22 | FOLDED | Adds the note and "Backup finished.". The folder's extra step fails safe |
| TC-23 | FOLDED | MA-1190 asserts it, with a fallback key. See N5 |
| TC-24 | FOLDED | All four slips are corrected. Checked: `source.go:151`, `mbox.go:283`, the MA-904 guard, and the status line |

Count: 18 FOLDED (TC-14 with a wrong target, TC-19 with a note), 6 PARTIAL (TC-1, TC-2, TC-3,
TC-5, TC-12, TC-16; TC-12's "everything" is also contradiction X1), 0 NOT_FOLDED.

## 2. The headline rule, traced path by path (§17)

The rule (§4.2 fold): "Export complete." only when every account is `caught_up` with missing = 0,
the count is not stale, and no cap note shows. Otherwise the weakest account sets the lead.

| Path that must not say "complete" | Wizard / Run now | Desktop card, Recent backups | `status` text / `-json` | Email | Result |
|---|---|---|---|---|---|
| missing, wrong-type or negative field | `unknown` → "Backup finished." | note | note / `unknown` + reason | none | holds, **except** a broken `flags` (M3) |
| Panorama | `unknown`, decided first | note | `panorama` | none | holds |
| duplicate keys | `unknown` | note | `duplicate_key` | none | holds for the exact-key case; the moved-profile rule is unclear (M2) |
| partial `-input` | `unknown`, no WARN | note | `partial_input` | none | holds |
| Exchange / unknown `type` | `unknown` | note | `account_type` | none | holds **only when** an account matched or a cache entry was read; otherwise `not_applicable` → "Export complete." (M6) |
| stale figures | lead "Backup finished." | — | `caught_up` with an old `as_of` | none | the lead holds; the **words say "everything"**, and `-json` has no stale marker (M9) |
| All Mail hidden, not kept or unchecked | floor → `behind` or `unknown` | note / WARN | `gmail_floor` | none | holds **unless** an Archive-only label stands in (M5) |
| multiple accounts | weakest sets the lead | per account | per account | per account | holds |
| deleted copies not compacted | estimate subtracted | — | `expunged_estimate` | — | holds on average; fails when sizes are skewed (M7) |
| encrypted archive, any of the above | **`encryptedSummary` says "Backup complete:"** (`internal/wizard/jobrun.go:231-237`) | in memory only | nothing | nothing | **fails** (M8) |

No path prints "caught up" in words: the user-facing sentences never use that phrase. The machine
field `verdict: "caught_up"` does appear in `-json` when 0 < missing ≤ tolerance and when the count
is stale. That is allowed by the design, but a script reading `verdict` alone would take it as
complete (N6).

What was checked outside the design: no alert email in `internal/alert` or `internal/backupalert`
contains "complete". The `status` "Last run" line prints the record's status word ("ok"),
`health.go:2354-2363`. The `status` line "Incomplete: none" (`health.go:2248`) will sit beside a
`thunderbird_behind` WARN. That line is about content cut short, and it is not wrong, but it reads
oddly next to the WARN; the friction review should look at it (C3).

## 3. Do the folds contradict each other?

| Pair asked about | Finding |
|---|---|
| TC-4 tolerance vs TC-16 estimate vs TC-1 floor | **Consistent.** §4.1 step 5 fixes the order: count, subtract, clamp, compute missing, apply the tolerance once. The floor test uses the floor's own tolerance. One ambiguity: TC-1's "All Mail lists at least the largest other folder, less the tolerance" does not say which Y the tolerance is taken from (N4). It fails safe either way, because Y = max(…) after that |
| TC-12 staleness vs TC-19 chosen settings | **Consistent.** Staleness changes the lead and the words, never the WARN-once hash. An account with background download off has no "as of" and is `limited`, decided first |
| TC-20 email order vs Q2 | **Consistent.** "One email of each kind" is the folder's reading of Q2, flagged with a one-line alternative. Not a contradiction |

Contradictions found that were not asked about:

- **X1 (M9).** The stale sentence ("had downloaded everything it listed") against R42 as amended,
  and against §4.2's fold.
- **X2 (M2).** TC-3 item 3 (tail keys only when no exact key) against item 4 ("one exact plus one
  tail → `unknown`").
- **X3 (M6).** §2.2's `not_applicable` row against §4.4's "file missing → `unknown`".
- **X4 (low).** The §5 fold says an encrypted archive's "WARNs and notes appear only on screen while
  it is unlocked". The §7 fold names only three places on screen (end summary, Run now note, the
  card's last result). Neither says whether the Desktop's Status & health page shows them for an
  unlocked encrypted archive. The checker recommends no, because that page is built from records,
  which hold nothing; say so in §7.

## 4. New findings, outside TC-1 to TC-24

- **N1. `prefs.js` is now read on every backup, through `os.ReadFile`** (`thunderbird.go:63-64`,
  `parsePrefs`; `EnableOffline` reads it the same way at `:172`). Today only the wizard and `-enable-offline` read it. With this design, every IMAP
  store's backup reads it (type, `is_gmail`, limits, the Panorama setting). A pipe planted at
  `prefs.js` would hang a scheduled backup, which is the hang TC-15 closed for `folderCache.json`.
  **M1** (C0c already touches this function).
- **N2. The encrypted Run now note says "Backup complete:" every time**
  (`internal/wizard/jobrun.go:231-237`, `encryptedSummary`). The §7 surfaces table names only
  `runNowSummary` (`:102-153`). O1 puts an encrypted archive's completeness on exactly this screen,
  yet the lead it would sit under still says "complete". **M8**
- **N3. The run history is too short for an hourly schedule.** `RunHistoryMax = 50`
  (`internal/state/runhistory.go:26`). On an hourly schedule, 50 runs cover about 50 hours, and the
  notices need 3 days, so neither notice can ever fire. Nothing durable records "this streak's email
  was sent" once the streak's first run has dropped out of the history either. Presync solved the
  same problem with its own small streak file, which keeps a serial number and first and last times
  (`internal/state/syncstreak.go:15-60`). The §5 sentence "it needs no new streak file" needs
  revisiting. **M10** (before C5)
- **N4.** For TC-1, state that the tolerance in "listing at least the largest other kept folder,
  less the tolerance" is taken from that largest folder's listed count.
- **N5.** The owner's Gmail folders show more message separators than Thunderbird lists (+32 in All
  Mail, +6 in INBOX) with `expungedBytes` = 0 (review §4, TC-16). So X can exceed Y for a reason the
  estimate does not see. MA-1190 should record X − Y on the owner's profile, not only whether it is
  within the tolerance. Until it does, "missing = 0" can still be wrong by that unexplained excess.
  Release notes already must not claim the count is exact (TC-16 fold).
- **N6.** In `-json`, add a `complete` true/false field that applies the exact rule used for the
  words. Then a script never has to rebuild the rule from `verdict`, `missing`, `as_of` and the cap
  note.
- **N7.** The `-log` file of a scheduled plaintext run sits beside the archive
  (`schedule.go:1009-1011`). The encrypted root allow-list has no log file (`leak/tree.go:37-44`),
  so encrypted runs must already log somewhere else. This was not traced. §7's "Command line
  (stderr): the headline lines" should say that, for an encrypted archive, the lines go to an
  attended terminal only and never into a written log.

## 5. Gmail detection: `is_gmail` only, or `is_gmail` OR the All Mail flag?

This difference is noted, not failed (gmail-dedup §6 at 9203aae still says "or a folder of the
account carries Thunderbird's All Mail flag", and claims to match this design).

**Pick `is_gmail` only, for both designs, in one shared function.** Why: the whole reason for
per-account math is Thunderbird's single-copy rule, and that rule runs only when
`GetIsGMailServer` is true (`nsImapMailFolder.cpp:8928-8935`, read). A non-Gmail server with a
`\All` folder (some servers offer a virtual "all mail" folder) still gets one copy per folder, so
per-folder math is the true one there. Calling it Gmail would let one full "All" folder hide the gaps
in the other folders. For gmail-dedup, the wider rule adds no benefit that per-folder copies need.
The cost of `is_gmail` only: a profile that has never connected has no `is_gmail` value yet
(`nsImapIncomingServer.cpp:2173` sets it from the server). It then gets the per-folder rule and a
false "behind" for Sent Mail, which fails safe. The shared function should also own "which folder
is All Mail": the single folder carrying 0x8000 (M5).

## 6. Citations sampled (21; all hold)

MailArchive, read in the worktree:

1. `internal/util/openregular.go:67` (`ReadRegularFile`): holds.
2. `internal/util/paths.go:162` (`StripControl`): holds. It does not strip bidi characters;
   `StripBidi` sits just below it.
3. `thunderbird.go:97-104` (the map loop): holds.
4. `thunderbird.go:19-21` (the "fully local" comment): holds.
5. `wizard.go:1594-1598` (the `EnableOffline` call): holds.
6. `cmd/mailarchive/main.go:2133`: holds.
7. `run.go:1631-1634` (stores are read in place): holds.
8. `run.go:1672-1683` (the token from the original `path`): holds.
9. `run.go:1218-1235` (`newRunRecord`): holds; the record is written at `:1210` and `:1295` (TC-19
   note).
10. `wizard.go:1622`, `:1634` ("Download / Sync Now"): holds.
11. `mbox.go:283` (`mozillaReadFlag`): holds.
12. `design-presync-attended.md:945-948` (C-15): holds.
13. gmail-dedup §6 at 9203aae: holds; it says "or … the All Mail flag".

Thunderbird source, trunk ab60cc5e, the review's copy:

14. `nsMsgDBFolder.cpp:1240-1247` (field types; `flags` unsigned 32-bit, `expungedBytes` and
    `folderSize` 64-bit): holds.
15. `nsMsgDBFolder.cpp:238` (`mNumTotalMessages(-1)`): holds.
16. `nsImapMailFolder.cpp:1932-1949` (`serverTotal`, `lastSyncTimeInSec` as a cast to 32 bits): holds.
17. `nsImapMailFolder.cpp:8928-8935` (the `is_gmail` gate), `:8954-8956` (All Mail looked up by
    Archive): holds.
18. `nsMsgDBFolder.cpp:1846-1849` (bug 1726660): holds.
19. `nsImapMailFolder.cpp:1371-1396` (`expungedBytes`): holds; it counts offline sizes (TC-16
    note).
20. `nsAutoSyncManager.cpp:819-825`, `mailnews.js:621`: holds.
21. `nsMsgFolderFlags.idl:74,76,78,96,102` (ImapBox 0x2000, Archive 0x4000, AllMail 0x8000, Noselect,
    Offline): holds.

Lead-in slip (from before the folds, not a fold): §2.3 cites `syncstreak.go:32` for `SyncStaleAfter`,
which is defined at `:33`.

## 7. Must-fix list

Each item is a design-text change, folded beside the text it amends as usual, then re-checked
(only these items).

**Before C0c:**

- **M1 (N1).** In C0c, read `prefs.js` through `util.ReadRegularFile` with a cap (for example
  16 MB). Treat a refusal like an unreadable `prefs.js`, which M6 turns into `unknown`. Add the pipe
  case to MA-1195.

**Before C1a (the reader):**

- **M2 (TC-3, X2).** Replace items 3 and 4 with one rule. Exact mode applies when any key of the
  store matches exactly. In exact mode:
  - an old-path key for a folder that also has an exact key is **ignored**;
  - an old-path key for a folder with **no** exact key → `unknown`, because coverage is incomplete;
  - two exact keys for one folder → `unknown`.

  Tail mode applies only when no key matches exactly; there, two tail keys for one folder →
  `unknown`. Add a fixture for each to MA-1170: a copied profile with old and new keys for every
  folder → checked, not `unknown`; and one folder with an old key only → `unknown`.
- **M3 (TC-2).** An entry whose `flags` fails validation is treated as kept, so the account →
  `unknown`. A failure in any other field of a not-kept entry may still be skipped. Add the case to
  MA-1171.
- **M4 (TC-14, TC-13).** Re-target the source re-read and the lab rows from "ESR 140" to the ESR the
  repo's labs run, **153** (153.6.0esr), and name the current Release seen in the lab (159, per
  `docs/lab/restore-thunderbird-macos-2026-11-06.md:81`). Confirm both on Thunderbird's release
  page at the re-read. This is the gate on C1a.

**Before C1b (the verdict):**

- **M5 (TC-1).** All Mail is the **one** folder of the account carrying AllMail 0x8000. A folder
  carrying only Archive 0x4000 never stands in for it. Source: `nsImapMailFolder.cpp:1230-1232`,
  `nsMsgAccountManager.cpp:1541-1542`. If no folder carries 0x8000, or more than one does, Y is a
  floor. Add the hidden-All-Mail-with-"Archives"-label fixture to MA-1176, never `caught_up`.
- **M6 (TC-5, X3).** `not_applicable` only for a **matched** account whose `type` is `pop3`, `none`
  or `rss`, or when no `prefs.js` is found above the input **and** no path part is `ImapMail`.
  Every other unmatched case → `unknown` ("does not yet know how this kind of account keeps its
  mail"), whether or not the cache could be read. Add to MA-1196 these cases: ambiguous account
  plus no cache; `prefs.js` unreadable plus no cache; and a copied `ImapMail/<host>` with no
  profile above it. Each → `unknown`, never "Export complete.".
- **M7 (TC-16).** "Complete" words (and the missing = 0 claim) also need **`expungedBytes` = 0 on
  every kept folder** of the account, until MA-1199 proves the deleted copies carry 0x0008 and the
  walk excludes them. The verdict may still use the estimate. With `expungedBytes` > 0, the lead is
  "Backup finished." and the words show both numbers. Correct step 5's "fails toward behind" claim:
  it is true only of the 0x0008 half. Add the skewed-size fixture to MA-1173.

**Before C3 (words and screens):**

- **M8 (N2).** Add `encryptedSummary` (`internal/wizard/jobrun.go:231-237`) to §7's surfaces table.
  For an encrypted Thunderbird IMAP archive, its lead follows the same weakest-account rule
  ("Backup complete:" only when every account may say complete). Add it to MA-1181 and MA-1186.
- **M9 (TC-12, X1).** Remove "everything" from the stale `caught_up` sentence, for example: "As of
  DATE, Thunderbird's count matched what is on this computer (N messages). It has not counted since,
  so newer mail may not be here yet." Or amend R42 to allow that dated form, but not both ways. Add
  `stale: true/false` to `-json`, and N6's `complete` field.

**Before C5 (notices and email):**

- **M10 (N3, TC-20).** Name where the behind streak lives so that it survives an hourly schedule's
  50-entry history: a small per-account streak record like `.mailarchive-syncstreak.json`, holding
  the first behind time, X then, and "still-downloading sent" / "stopped sent" flags, plaintext only.
  Or carry the same in the last-run record. Add an hourly 4-day run to MA-1184.

**Can fold with their slice (low):** TC-7's single bidi helper and the cleaned account name (C3);
TC-19's read-before-the-start-write and carry-forward (C2); X4 (C3); N4 (C1b); N5 (MA-1190); N7
(C3).

## 8. Counters

- **Date worked on:** 7 November 2026. Clock from the session, not inferred.
- **Register:** a fold-check finding report. The verdict, the picks in §5 and the suggested wording in
  M9 are the checker's opinion.
- **Drew on, and reached:**
  - the design at 31bd018 (digest above): yes, in full;
  - the review at 22330a8: yes, in full;
  - MailArchive code (§6 items 1 to 11, plus `jobrun.go:215-237`, `runhistory.go:26`,
    `syncstreak.go:1-60`, `health.go:2348-2366`, `schedule.go:995-1011`,
    `desktop/schedule.go:168-182` and `leak/tree.go:25-50`): yes;
  - grep sweeps for "complete", "Backup complete", "Export complete" and "caught up" in
    `internal/` and `cmd/`: yes;
  - Thunderbird source at trunk, the review's downloaded copy: yes, for the lines in §6. Not fetched
    fresh;
  - gmail-dedup at 9203aae: §6 and its MA-1215 row only;
  - `design-presync-attended.md:943-948`: yes;
  - the repo's lab notes, for the Thunderbird version: by grep, line-level only.
- **Not checked, per claim:**
  - Thunderbird ESR 153 or Release 159 source: not read, so M4 rests on the repo's lab notes, not on
    a release page;
  - whether Thunderbird gives a Gmail account's archive folder the Archive flag when All Mail is
    hidden: read in trunk source (identity archive folder → Archive), not seen on a real profile.
    M5 holds either way, because it only narrows the rule;
  - how often deleted messages are smaller than the folder average in practice (M7): not measured;
  - where an encrypted scheduled run's log goes (N7): not traced;
  - the owner's profile: not read by this check.
- **The smallest check that would settle the most:** in a throwaway Gmail profile on the Windows
  VM, hide All Mail from IMAP, archive one message with Thunderbird's archive key, then read the
  `flags` on the new "Archives" folder's cache entry. If it is 0x4000 without 0x8000, M5's failure
  is real on a real install.
- **Borrowed lines:** short quotes from the design, the review and the cited code and source, each
  attributed where it appears.
- **Drift:** none noticed. This check stayed on the design as frozen at 31bd018.
- **Touched:** this file (new) and one commit. No design text and no product code was changed.
- **Persona:** none.
- **Source reached, per source:** design yes; review yes; repo code yes (as listed); Thunderbird
  trunk (local copy) yes; ESR source no; gmail-dedup partly; owner profile no; Mozilla bug tracker
  no.
