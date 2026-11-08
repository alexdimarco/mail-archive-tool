# Review: Thunderbird completeness check (design-thunderbird-completeness), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** The design's core idea holds up: Thunderbird's own folder list
(`folderCache.json`) gives a usable "how much is listed" figure, and judging Gmail per account is
right. The idea was re-read in Thunderbird's source and checked against the owner's real profile.
But seven findings are HIGH. Six of them are paths where "can't tell" or "behind" still ends at a
"complete"-style sentence: the Gmail floor, a missing field, stale or duplicate keys, the tolerance's
low end, Exchange accounts, and deleted-but-not-compacted copies counted as on disk. That last one was
found by the rescue pass. The seventh (TC-6) is a plaintext field beside encrypted archives that
breaks a locked rule. The rescue pass dissolved every HIGH by a named mechanism change; none needs a
re-design (§3). 24 conditions in all (TC-1 to TC-24), each to be folded into
`docs/design-thunderbird-completeness.md` beside the text it changes, marked "Folded from the 10-lens
review [TC-n]", and re-verified.

**Build may not start (slice C1 included) until the HIGH and MED conditions are folded.** Slice C4
(the encrypted field) is also blocked on the owner ruling in TC-6, because it amends rules locked
elsewhere (C-16, A18). The LOW conditions may be folded with the slice they name.

| Id | Sev | Lens | Section | In one line | Verdict |
|---|---|---|---|---|---|
| TC-1 | HIGH | 1, 10 | §3, §4.2 | Gmail with All Mail hidden, not kept, or smaller than a label: the "at least" floor can still reach "caught up" | CONFIRMED; dissolved |
| TC-2 | HIGH | 7, 9 | §4.1 step 3, §11 | A missing or renamed field reads as 0, every folder is "ignored", "caught up" is vacuously true | CONFIRMED; dissolved |
| TC-3 | HIGH | 3, 7, 9 | §2.3, §4.1 step 2, §11 | Panorama leaves the old file frozen; a moved profile keeps old-path keys that tail-match | CONFIRMED; dissolved |
| TC-4 | HIGH | 1, 10 | §4.2, §4.3 | Tolerance: 20-message floor calls a small account with nothing on disk "caught up"; per-folder vs per-account unstated | CONFIRMED; dissolved |
| TC-5 | HIGH | 5, 7 | §2.2, §4.2 | Exchange (EWS) stores live under `Mail/`, so "not under ImapMail/" makes them not_applicable, which keeps "Export complete." | CONFIRMED; dissolved |
| TC-6 | HIGH | 6, 8 | §9.2, Q3b | The `tb_behind` field beside an encrypted archive says "made from Thunderbird", which the locked rule C-16 forbids; the cited guard is a file-name list | CONFIRMED; **owner ruling** |
| TC-7 | MED | 6, 4 | §6, §7, §9.1 | Folder names are server-controlled text shown in the terminal and Desktop: no stripping, escaping or cap stated | CONFIRMED |
| TC-8 | MED | 4, 1 | §6 | A Gmail label not kept on this computer is no gap when All Mail is kept, yet it gets a WARN and "tick each folder" | CONFIRMED |
| TC-9 | MED | 7, 4 | §4.1 step 4 | `autosync_offline_stores` off: Thunderbird never downloads in the background; the words say "leave it open" forever | CONFIRMED |
| TC-10 | MED | 5, 4 | §4.1 step 1, §7 | Two accounts on one server: `FindAccountForStore` can return the wrong one; both are named "imap.gmail.com" | CONFIRMED |
| TC-11 | MED | 5 | §2.4, §4.1 | `-copy-first` does not copy store folders; a single-mbox or subfolder `-input` is a partial account | CONFIRMED |
| TC-12 | MED | 1, 3, 10 | §2.3, §7 | "As of" uses the newest check, calls it a server check, and keeps "Export complete." when weeks stale | CONFIRMED |
| TC-13 | MED | 10, 4, 9 | §6 | Fix-it words differ by Thunderbird version: trunk says "Save copies of all folders and messages…" | CONFIRMED |
| TC-14 | MED | 9, 10 | §16 | Every source claim is proven at trunk, not at the ESR users run | CONFIRMED |
| TC-15 | MED | 6, 7 | §4.1 step 2 | Reading the file: no regular-file check (a pipe hangs the backup), no number range rules | CONFIRMED |
| TC-16 | HIGH | 10, 1, 7 | §2.4 | Deleted or moved copies stay in the offline mbox until compaction (`expungedBytes`; 18.7 MB in the owner's Outlook INBOX); the 0x0008 bit that would exclude them is unproven, so they can hide a gap | CONFIRMED (rescue); lab |
| TC-17 | MED | 4, 10 | §7 | "Later backups add the rest" is untrue after a one-off wizard export with no schedule | CONFIRMED |
| TC-18 | LOW | 2 | §8, §13 C6 | The presync change is separable, touches a reviewed state machine, and buys at most 20 minutes | CONFIRMED |
| TC-19 | LOW | 4 | §5 | `limited` and `not_kept` WARN forever for a setting the person chose on purpose | CONFIRMED |
| TC-20 | LOW | 4 | §5 | "Same email slot": a stall after the still-downloading email may never be told | CONFIRMED |
| TC-21 | LOW | 5, 7 | §2.4 | When the cache is read relative to the walk is unstated; reading first is the optimistic order | CONFIRMED |
| TC-22 | LOW | 1 | §3 | Gmail's "limit IMAP folders to N messages" setting caps every count; "everything it lists" then hides the rest | PLAUSIBLE (lab) |
| TC-23 | LOW | 10 | §2.4, §3 | Distinct-by-Message-ID may undercount Gmail (two messages, one ID): a permanent false "behind" | PLAUSIBLE (lab) |
| TC-24 | LOW | 10 | §1, §9.2, header | Citation slips: `source.go:151`, `mbox.go:280`, `leak/tree.go`, the review file's name | CONFIRMED |

- Date worked on: 7 November 2026 (the session's date).
- Design reviewed: `docs/design-thunderbird-completeness.md` at commit 552fc98 (branch
  `tb-completeness`, off main 0923ffa = v1.6.1), sha256
  `63f9d6d1b8c49af57379bf2556221e3712bf57d5dc9f27789b34343ab91be28c`.
- File name: the caller asked for `docs/review-thunderbird-completeness-10lens.md`. The design's status
  line names `docs/review-thunderbird-completeness-predesign.md` (the kit's convention,
  `design-review.md:7`). TC-24 fixes the status line to cite this file.
- Procedure: `../assurance-kit/process/design-review.md`. Read-only. No product code was run, no
  Thunderbird was run, no test was run.
  - Lens passes 1–10 and the skeptic's code check: the synthesiser (this file's author) read every
    cited MailArchive file:line directly (§1).
  - Thunderbird's source: fetched fresh by the synthesiser from the GitHub mirror at the design's
    revision ab60cc5ea7300498991dfaaa84c33204bdb482e4 (not the designer's cached copies), plus
    `nsMailDirProvider.cpp`, `ExchangeProtocolInfo.cpp`, `panorama/src/DatabaseCore.cpp`,
    `accountSettings.ftl` and GitHub code search of the default branch (§2).
  - The owner's Mac profile: `folderCache.json` only, read-only, through a script that printed counts,
    flags and standard folder names, with personal labels replaced by numbers (`label#1`…). Nothing was
    copied into the repo. `prefs.js` and the mbox files were **not** read by this review.
  - Rescue pass: one separate agent, given TC-1 to TC-6 and three MED candidates, told to save the
    design. Its outcomes are recorded per blocker (§3); it found TC-16's HIGH form.
- Register: review synthesis (findings, the synthesiser's decisions, and opinion where marked).
  Statements about Gmail's or Thunderbird's behaviour that were not read in source or a profile are
  marked **(recollection, unverified)**.

## 1. Claims checked against the code (the skeptic's part)

All read by the synthesiser in the worktree at 552fc98.

| Design claim | Result |
|---|---|
| mbox reader `localwalk.go:457-500`, folder walk `:506-609`, `n := 0` `:472`, partial text `:474`, `n++` `:493` | **Holds.** The counter at `:493` runs before `w.handler` (`:495`), and the handler (`run.go:1686-1694`, `src.handle`) is where the export's "already done" skip lives |
| the date filter is downstream | **Holds.** `Since` is passed to the exporter (`run.go:799`), not the walk |
| `-copy-first` "counts its copy, but maps it back" (§2.4) | **Wrong premise.** `runFile` snapshots single files only: "mail-store directories are read in place" (`run.go:1631-1634`). A store is never copied. The identity seed already uses the original `path` (`run.go:1672-1683`). TC-11 |
| `.msf` "only checked for existence" at `source.go:77`, `:151` | `:77` is an existence glob; `:151` is an extension **exclusion** (`isMailboxCandidate`). TC-24 |
| `presync/quiet.go:321`, `:362` | Holds (index beside a store file; `.msf` excluded) |
| `mozillaReadFlag` `mbox.go:280` | The comment is `:280`; the function is `:283`. TC-24 |
| one total recorded, `run.go:1421` | Holds |
| `NotDownloadedSentence` `outcomewords.go:176-179` | Holds |
| wizard 45 s stable `wizard.go:1617`, "Download complete" `:1637`; "Export complete." `:1894`; `EnableOffline` call `:1594-1598` | Holds. The wizard watches the whole store's size (`:1619`) |
| `runNowSummary` `jobrun.go:102-153` | Holds |
| `defQuiet` `api.go:99`, `defMaxWait` `:100`; rule `quiet.go:221-232`; `Synced()` `attended.go:55-65`; `runSession` `session.go:131` | Holds |
| `SyncStaleAfter`/`SyncMinRuns` `syncstreak.go:30-38` | Holds (`:33`, `:35`) |
| `backupalert.Report` `streak.go:132-187` | Holds (start `:132`) |
| `partial()` `gaps.go:225`, `partialOutcome` `:276-285` | Holds |
| `warn`/`note` `health.go:366-382`; `Assess` `:415`; Last run `:2348-2383`; `JSONReport` `:2514-2573` | Holds |
| `encryptedLastRun` `encrypted_record.go:141-149`; `NoteIndexBehind` `:45-48` | Holds |
| `RunHistoryEntry` `runhistory.go:35-58`; `minimal()` `:61-63` | Holds |
| presync dropped for encrypted `run.go:1379-1385`, `app/syncstreak.go:38-41` | Holds |
| the leak test `internal/assure/leak/tree.go:31-44` "gains this field" | **Misattributed.** `tree.go:37-44` is the list of **file names** allowed at an encrypted archive's root. It cannot see a field inside a record. The field guard is MA-904: record key sets equal the A18 allow-list, and a scan of the whole folder for "Thunderbird", "presync" and outcome names (`scenario-catalog.md`, row MA-904). TC-6 |
| `IsImapStore` `thunderbird.go:22-25`; `FindProfileDir` `:29-42`; `FindAccountForStore` `:81-116`; `EnableOffline` `:170`; `main.go:2133` | Holds. But `FindAccountForStore` loops over a Go map and returns the first server whose `hostname == want` (`:97-104`): with two accounts on one server, the result is random. TC-10, side finding A |
| store discovery `run.go:1881-1902` (no Flatpak); restore's list `tbrestore/profiles.go:428-437`; `profiles_windows.go:14-19` | Holds. Each `ImapMail/<host>` and `Mail/<name>` is one store (`:1896-1898`) |
| `storekind.go:26-28` | Holds |
| `ArchiveRow` `archives.go:31-76`, `sourceLostSince` `:241-250`; `recentRunRows` `runhistory.go:26-85` | Holds |
| `alert.go:164-165` | Holds (the subject switch) |
| a new run record carries no old completeness | **Holds** (checked for the crash lens): `newRunRecord` builds a fresh record (`run.go:1218-1235`); an encrypted one carries only `Program` |
| older MailArchive reading newer records | **Holds**: no strict decoding (`DisallowUnknownFields` appears nowhere in `internal/state`), so an added field is ignored by an older reader |
| text helpers to reuse | `util.StripControl` (`internal/util/paths.go:162`); the Desktop renders through `html/template` (`internal/desktop/pages.go`, `dashboard.go`) |

## 2. Thunderbird claims: what was read, what is lab-required, and which would break the design

Read by the synthesiser at trunk revision ab60cc5e, fetched fresh. "Trunk" matters: users run the ESR
(140) or Release builds, not trunk (TC-14).

| Claim (design §) | Status | If wrong, does the design break? |
|---|---|---|
| keys are the `.msf` path from `GetFolderCacheKey` (`nsMsgDBFolder.cpp:1170-1186`), written as the persistent descriptor (`:1210-1213`) | **Proven** in source; the owner's 38 keys all start with the profile's current absolute path, and roots have no `.msf` | — |
| value fields (`nsMsgDBFolder.cpp:1232-1250`, `nsImapMailFolder.cpp:1925-1953`); `totalMsgs` `:1242`, read from the summary `:681`; `pendingMsgs` `:1245`; `ChangeNumPendingTotalMessages` `:4062-4078`; `serverTotal` `:1936`, set `:2691-2699`; `onlineName` `:1934`; `lastSyncTimeInSec` `:1943-1950`; `GetTotalMessages` `:4009` | **Proven** | — |
| flag values `nsMsgFolderFlags.idl:41,64,66,72,78,96,102,108`; message flags Expunged 0x0008 `:29`, Offline 0x80 `:47` | **Proven**. The owner's All Mail carries both AllMail 0x8000 **and** Archive 0x4000; Thunderbird's own Gmail label lookup finds All Mail by **Archive** (`nsImapMailFolder.cpp:8954-8956`) | If a Gmail All Mail lacks 0x8000, the design falls back to the floor. Safe once TC-1 is folded |
| save one minute after a change (`nsMsgFolderCache.h:42`, `.cpp:349-361`), at close (`:243-262`), atomic temp-and-move (`:306-327`) | **Proven** in source. The owner's file was 2.1 h old with Thunderbird running, while `lastSyncTimeInSec` was 4.5–7 h old: the time is written only when a folder is next flushed | Not load-bearing at the 3-day threshold |
| Panorama refuses to create the cache (`nsMsgFolderCache.cpp:184-187`, `nsMsgDBFolder.cpp:1152-1155`) | **Proven**, but **incomplete**: Panorama writes `panorama.sqlite` (`DatabaseCore.cpp:136-151`), and nothing there removes `folderCache.json`. A code search of the default branch found no other deleter. Whether a converted profile keeps the old file is **lab-required** | **Yes**: a frozen file read as valid gives wrong numbers (TC-3) |
| autosync idle and 60 KB rules (`nsAutoSyncManager.h:29-41`, `:127-128`; `.cpp:1264-1278`) | **Proven** | — |
| single copy per Gmail message (`nsImapMailFolder.cpp:8906-9004`; `nsAutoSyncState.cpp:254-259`, `:335-339`) | **Proven** in source; it matches the owner's profile counts (designer's read) | — |
| `lastSyncTimeInSec` meaning (§2.2 table) | **Proven**: set when the pass over the folder's **local** headers finishes (`nsAutoSyncState.cpp:353-358`). It is not a server check (TC-12) | Words only |
| zero-byte messages skipped (`nsAutoSyncState.cpp:271-275`) | **Proven** | — |
| Trash and Junk not flagged offline at creation (`nsImapMailFolder.cpp:1005-1018`) | **Proven**, at folder creation only; a person can tick them later (handled) | — |
| prefs defaults (`mailnews.js:547-548,625`); `max_age` strategy (`nsAutoSyncManager.cpp:107-125`); pref names (`nsMsgIncomingServer.cpp:1413-1415,1429`) | **Proven**. **Missed**: `autosync_offline_stores` (`mailnews.js:621`), which turns background download off per server (`nsAutoSyncManager.cpp:819-825`), and the "not logged in this session" skip (`:844-859`) | TC-9 |
| `is_gmail` (`nsImapIncomingServer.cpp:399`) | **Proven**; set from the server's capability (`:2173`) | — |
| EWS accounts "unknown" (§2.2) | **Contradicted by the layout**: EWS stores default to `NS_APP_MAIL_50_DIR` (`ExchangeProtocolInfo.cpp:34-36`) = `Mail` (`nsMailDirProvider.cpp:17,43-44`), the same root as Local Folders; EWS autosyncs (`nsAutoSyncManager.cpp:832-835`). The field set in an EWS cache entry is **lab-required** | **Yes**: as written they are not_applicable (TC-5) |
| folder-properties labels (`folderprops.ftl:63-71`) | **Proven** at trunk | — |
| `am-offline.dtd` labels | `allAutosync.label` **proven** and still used by trunk's `am-offline.xhtml`. But trunk's checkbox is `account-settings-save-all-messages` = "Save copies of all folders and messages on this computer for offline use" (`accountSettings.ftl:72-73`), under "Offline message storage"; the size limit is now "Skip downloading messages larger than:". The design's "Keep messages for this account on this computer" appears in comm-central only in SeaMonkey's help | Words only, but wrong on new builds (TC-13) |
| deleted-not-compacted messages carry 0x0008 in an IMAP offline mbox (§2.4) | **Lab-required**. The INBOX "+6 separators" reading is an interpretation, and this review did not read the mbox | **Yes**: if absent, deleted copies inflate the on-disk count and can hide a gap. `expungedBytes` shows such copies exist (18.7 MB in the owner's Outlook INBOX) (TC-16) |
| distinct Message-IDs ≈ All Mail's listed count when caught up (§3) | **Lab-required** | Fails toward "behind" (safe, but a permanent false WARN): TC-23 |
| Gmail throttling (§1) | Unverified, as the design says; not load-bearing | — |
| a moved profile's keys (§4.1 step 2) | **Proven risk**: Thunderbird's own comment cites bug 1726660, "folderCache.json is removed or becomes invalid after moving a profile" (`nsMsgDBFolder.cpp:1846-1849`). Old-path entries are only removed when a folder is deleted (`RemoveElement`, `nsMsgFolderCache.cpp:344-347`) | **Yes**, with tail matching (TC-3) |

The owner's profile (7 November 2026, read-only, counts only) also showed:

- every kept folder with mail has `lastSyncTimeInSec` under 0.3 days old;
- kept label folders with 0 messages also carry it;
- Spam (Junk flag, not kept) and Trash (not kept) have none;
- account roots carry `totalMsgs` = -1;
- the Outlook account's Trash has `totalMsgs` 5, `serverTotal` 0 and flag 0x10000000 (OfflineEvents);
- Templates is 0x00400000 with no ImapBox flag.

So the "never checked → unknown" rule does **not** strand this profile at `unknown`, which was a risk.

## 3. Rescue pass outcomes

One separate agent, given TC-1 to TC-6 (as B1–B6) and three MED candidates, was told to save the design
and to look for a blocker underneath. Its claims that change a condition were re-read by the
synthesiser (marked "re-read").

| Blocker | Outcome | The change (adopted into the condition) |
|---|---|---|
| B1 Gmail floor (TC-1) | DISSOLVED-BY | All Mail sets Y only when kept, checked (`lastSyncTimeInSec` > 0) and listed at least the largest other kept folder less the tolerance; otherwise Y is a floor and the best verdict is `unknown`. Also: detect Gmail by `is_gmail` **only**. Thunderbird's single-copy lookup runs only when `GetIsGMailServer` is true (`nsImapMailFolder.cpp:8928-8935`, re-read), so a non-Gmail server with an AllMail-flagged folder keeps the per-folder rule |
| B2 stale keys (TC-3) | DISSOLVED-BY | the Panorama marker → `unknown`; exact-path match first, tail match only when the file holds a single path prefix; two keys for one folder → `unknown` |
| B3 missing field (TC-2) | DISSOLVED-BY | fields read as present-or-absent; `caught_up` needs at least one kept folder actually compared; a walk that found messages where every folder lists 0 → `unknown`. Thunderbird starts `mNumTotalMessages` at -1 (`nsMsgDBFolder.cpp:238`, re-read) |
| B4 tolerance (TC-4) | DISSOLVED-BY | tolerance = max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), once per account; per-folder deficits clamped at 0 before summing. 15 listed → 1; 100 → 10; 238,169 → 476 |
| B5 Exchange (TC-5) | DISSOLVED-BY | sort by the account's `prefs.js` `type`. The rescue added: an IMAP account given a custom local directory outside `ImapMail/` also fails `IsImapStore` (`thunderbird.go:22-25`) and so also reaches today's "complete" words; type-based sorting covers it |
| B6 encrypted `tb_behind` (TC-6) | SURVIVES as proposed; DISSOLVED-BY dropping it | the sealed-record escape is **not available**: the sealed header's run-details place (A24) is reserved but unbuilt, an open question (`design-presync-attended.md:947-948`, re-read: "The sealed header's reserved run-details place (A24) is Q4, not this design"). So: completeness on screen only for encrypted archives (end summary, Run now note, the card while unlocked, cleared by Lock, the C-15 pattern); nothing persisted; no notices or email for encrypted archives. R44 becomes "nothing beside an encrypted archive", and slice C4 and its pass go away. Losing the notices is a product limit for the owner |
| **N1 (new): stale offline copies inflate X** | DISSOLVABLE; **raised to HIGH as TC-16** | Thunderbird keeps the bodies of deleted or moved messages in the offline file until compaction, and counts them in `expungedBytes` (`nsImapMailFolder.cpp:1371-1396`, re-read). Compaction waits for a threshold and may ask the person first (`nsMsgDBFolder.cpp:1641-1646`, re-read). Nothing read shows those copies carry 0x0008. The owner's file (re-read for this): the Outlook INBOX has `expungedBytes` 18,718,461 against 910 messages listed. So stale copies are real on the owner's own machine. The change: read `expungedBytes` and subtract an estimate before the test (TC-16) |

What the rescue tried that held:

- `lastSyncTimeInSec` marks headers queued for download, not bodies downloaded, so the design's
  table wording fits.
- The walk reads every message on every run, so incremental runs count the same as first runs.
- Thunderbird's file-safe folder names match the walk's file names.

Overall, from the rescue: GO WITH CONDITIONS is reachable; no re-design.

## 4. Conditions

Each condition: severity, lens, the scenario it closes, and the fix. "Words" means the one shared
sentence built in `internal/state` (§7).

### TC-1 — HIGH — lenses 1, 10 — §3, §4.2, MA-1176

**Scenario.** A Gmail account with All Mail hidden over IMAP (or not kept on this computer). Y falls to
the largest single kept folder, say Important at 56,625. X counts distinct messages across every label
file, so INBOX + Important + Starred + labels on disk can pass 56,625 while Sent (33,822) has nothing on
disk. §4.2's only test is "missing within tolerance", so the verdict is `caught_up`: "Thunderbird has
downloaded everything it lists". The same holds when All Mail's listed count is smaller than one
label's: the design takes the max, but an All Mail smaller than a label is itself a sign the figure is
not the union.

**Fix.** Y is a **floor** whenever All Mail is not kept, has no `lastSyncTimeInSec`, is absent, or
is listed below the largest other kept folder. With a floor:

- X below the floor minus the tolerance → `behind`. That is sound, because X under a floor is
  certainly short.
- Otherwise → `unknown` with its own words: "about X messages on this computer; Thunderbird lists at
  least Y; MailArchive cannot tell whether that is all of it, because Gmail's All Mail is not kept on
  this computer". Never `caught_up`. The lead is "Backup finished."

Detect Gmail by the `is_gmail` pref **only**, not also by the AllMail flag. Thunderbird's single-copy
rule depends on that pref alone (`nsImapMailFolder.cpp:8928-8935`). A non-Gmail server with an
AllMail-flagged folder keeps the per-folder rule. If prefs cannot be read, the account is `unknown`
(TC-5).

MA-1176 gains the fixture: X above the floor, a large label empty, never `caught_up`.

### TC-2 — HIGH — lenses 7, 9 — §4.1 step 3, §11 "a field renamed", MA-1171

**Scenario.** A future Thunderbird renames `totalMsgs`, or writes it as a string. The reader takes 0,
"listed 0" puts every folder in **ignored**, the account has no folder left to be behind, and
`caught_up` is vacuously true. §11's "a field renamed → unknown" is not what §4.1 builds.

**Fix.** Validate before sorting:

- On every non-root entry of the store, `flags` and `totalMsgs` must be present JSON integers within
  int32. On an entry with the ImapBox flag (0x2000), `serverTotal` must be too. Any kept folder
  failing this → the account is `unknown`.
- A negative value is allowed only on the account root (the owner's roots carry -1). Elsewhere it
  makes the account `unknown`.
- An account with no countable kept folder → `unknown`, never `caught_up`. A store whose entries all
  validate and list 0 is the one exception: it may say "Thunderbird lists no mail for this account".

MA-1171 gains: a missing `totalMsgs`, a string `totalMsgs` and an out-of-range number, each → `unknown`.

### TC-3 — HIGH — lenses 3, 7, 9 — §2.3, §4.1 step 2, §11 rows 2 and 6

**Scenario A (Panorama).** A profile converted to Panorama keeps its last `folderCache.json`. Thunderbird
stops writing it (`nsMsgFolderCache.cpp:184-187`) but nothing removes it (§2). MailArchive reads frozen
counts as valid for months:

- new mail is never "listed", so the account can read `caught_up`;
- the "as of" words tell the person to "open Thunderbird so it can check again" while it is open
  every day.

**Scenario B (moved profile).** After a move or restore, old-path keys stay (bug 1726660) beside new
ones. Tail matching can give one folder two entries, and the stale one can win.

**Fix.**

1. **Panorama → unknown.** If `panorama.sqlite` exists in the profile, or `prefs.js` sets
   `mail.panorama.enabled` true, the account is `unknown`, with words naming "Thunderbird's newer
   storage". Add a lab row that converts a throwaway profile and records whether the file stays.
2. **Exact keys first.** Match a key exactly against the store's current absolute path, after native
   normalisation (case-folded on Windows and macOS, `\` and `/` alike). Use tail matching only when no
   key of the store matches exactly.
3. **Duplicates → unknown.** If two keys match one folder by tail, the account is `unknown`.
4. **Normalise names.** Apply Unicode NFC to both sides before comparing.

MA-1170 gains a fixture holding both an old-path and a current-path key for the same folder.

### TC-4 — HIGH — lenses 1, 10 — §4.2, §4.3, MA-1174, Q1

**Scenario, low end.** A new account with 15 messages listed and none on disk: missing 15 ≤ 20, so
`caught_up`: "downloaded everything it lists (15 messages)". If the tolerance is applied per folder
(§4.2 does not say), 40 small folders each missing 19 hide 760 messages.

**Scenario, high end.** At 0.2%, a 1,000,000-message account can miss 2,000 and still read "everything".

**Fix.**

- Non-Gmail: missing = the sum over kept folders of max(0, listed − on disk). Clamp each folder before
  summing, so a folder holding extra copies cannot hide another folder's gap.
- Gmail: missing = max(0, Y − X).
- Apply the tolerance once per account, never per folder.
- Low end (the rescue's formula): tolerance = max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋). 15 listed allows
  1 missing; 100 allows 10; 238,169 allows 476. X = 0 with Y > 0 is never `caught_up`.
- When 0 < missing ≤ tolerance, the words show both numbers ("about 237,900 of 238,169; the rest is
  likely mail that arrived or was deleted since Thunderbird's count"). "Everything" is said only when
  missing = 0.
- Q1 keeps the numbers.

MA-1174 gains: listed 15 with 0 on disk → `behind`; two folders, one +30 and one −30 → `behind`.

### TC-5 — HIGH — lenses 5, 7 — §2.2 EWS bullet, §4.2 `not_applicable` row, §11

**Scenario.** A Thunderbird native Exchange account stores mail under `Mail/<host>` (§2). Discovery
picks it up (`run.go:1898`). `IsImapStore` says no, so `not_applicable` applies, and "the wording used
today" is "Export complete." for an account that downloads over time. This is a can't-tell path that
ends at "complete". It also contradicts §2.2's own "EWS → unknown".

**Fix.**

- Sort a store by its `prefs.js` server `type`, through the matched account (TC-10), not by the
  `ImapMail/` path:
  - `imap` → checked;
  - `ews`, any other or unknown type, or prefs that cannot be read for a store with a folder cache
    entry → `unknown` (neutral words);
  - only `pop3`, `none` (Local Folders) and `rss` → `not_applicable`.
- This also covers an IMAP account given a custom local directory outside `ImapMail/`, which
  `IsImapStore` misses today (found by the rescue).
- Add a U row (an EWS-typed store under `Mail/` → `unknown`, never "complete"; an IMAP account with a
  custom directory → checked) and a pending lab row that reads one real EWS profile's cache fields.

### TC-6 — HIGH — lenses 6, 8 — §9.2, R44, Q3b, MA-1186 — **owner ruling, blocks slice C4**

**Scenario.** `tb_behind`, written beside an encrypted archive, is present only when true. Its
presence says "this archive is made from Thunderbird IMAP on this computer". That is exactly the source
kind that rule C-16 keeps out (`design-presync-attended.md` §4.4: "Any pre-sync code or name says
**this archive is made from Thunderbird on this computer**, a source kind. So nothing is written"). A6
moves the source kind into the sealed record, and A18 fixes the allow-lists that MA-904 pins by exact
key set. The history's run of `tb_behind` values over time also shows when a new account was added
(behind again). §9.2's "it tells nothing about the mail" is true, but that is not the rule. The guard
it cites (`leak/tree.go`) cannot see fields (§1).

**Fix (recommended; the rescue's escape).** For an encrypted archive, completeness is **on screen
only**: the end summary, the Run now note, and the card's last result while the archive is unlocked
here, cleared by Lock or a switch. That is the pattern presync already uses (C-15,
`design-presync-attended.md:945-948`). Nothing is written beside the archive. No still-downloading or
stopped notice, and no email, for encrypted archives. R44 becomes "nothing about completeness beside
an encrypted archive", and slice C4 and its adversarial pass go away.

The sealed record is **not** a ready alternative: its run-details place (A24) is reserved but unbuilt
(§3). It can carry completeness later, when A24 is designed. If the owner prefers the boolean instead,
that amends C-16 and A18: rule it explicitly, update MA-904's allow-list in the same change, and run
the adversarial pass. Either way §9.2 must cite MA-904 (the key-set and folder scan) as the guard, not
`tree.go`.

### TC-7 — MED — lenses 6, 4 — §6, §7, §9.1

**Scenario.** Folder names come from the IMAP server. A shared or hostile mailbox can name a folder
with terminal escape codes, bidirectional-override characters or 10 KB of text. §6 prints them in
`status` (a terminal), in the wizard and in the Desktop.

**Fix.**

- Every folder name taken from the cache or the walk passes `util.StripControl`, with bidi controls
  removed too.
- Each name is capped (for example 80 characters plus "…").
- The not-kept list is capped (the first 10 by size, then "and N more").
- The Desktop renders names only through `html/template`.
- Name which field supplies the human name. `onlineName` is the server's raw name (modified UTF-7 for
  non-ASCII on servers without UTF8=ACCEPT; **recollection, unverified**), so prefer the walk's folder
  path.
- A U row plants control characters, a bidi override and an over-long name, and checks all four
  surfaces.

### TC-8 — MED — lenses 4, 1 — §6 first block, S76

**Scenario.** A Gmail user with 40 labels, most not kept on this computer, and All Mail kept. Every
label's mail is in All Mail, so it is backed up. Yet §6 raises WARN `thunderbird_not_kept` and asks
for "right-click each folder…" 40 times.

**Fix.** For a Gmail account whose All Mail is kept and checked, a not-kept label is at most a
note: "their mail is backed up through All Mail". Only a not-kept All Mail itself (and Spam or Trash,
as today's note) is reported. MA-1185 gains the Gmail case.

### TC-9 — MED — lenses 7, 4 — §4.1 step 4

**Scenario.** `mail.server.serverN.autosync_offline_stores` is false (default true, `mailnews.js:621`).
Thunderbird never downloads in the background (`nsAutoSyncManager.cpp:819-825`) and never sets
`lastSyncTimeInSec`. The account reads `unknown` with "Open Thunderbird and leave it open", which can
never help.

**Fix.** Read `autosync_offline_stores` beside the other limits.

- False → `limited`, with words: "Thunderbird is set not to download this account's mail in the
  background, so MailArchive backs up only what you have opened or downloaded by hand."
- The remedy names Thunderbird's download-now command. That label is **lab-required** (MA-1191).

The "not logged in this session" skip (`:844-859`) needs no new verdict; the stopped notice's remedy
covers it.

### TC-10 — MED — lenses 5, 4 — §4.1 step 1, §7 "named by its server"

**Scenario.** Two Gmail accounts live in `ImapMail/imap.gmail.com` and `ImapMail/imap.gmail.com-1`.

- `FindAccountForStore` returns whichever server Go's map order gives first when `hostname` equals
  the store's base name (`thunderbird.go:97-104`), so the limits and `is_gmail` can come from the
  wrong account.
- Both accounts would read "imap.gmail.com" in the words.
- A prefix test of keys would mix the two stores.

**Fix.**

- Match by `directory` / `directory-rel` first, component-wise, and by `hostname` only when exactly
  one server has it.
- Name an account by its store folder name (`imap.gmail.com-1`), which is no more personal than the
  host.
- Compare key paths by whole path components.

A U row covers two same-host accounts.

### TC-11 — MED — lens 5 — §2.4 last paragraph, §4.1

**Scenario.** §2.4's `-copy-first` sentence describes a mapping that cannot occur: stores are read in
place (§1). Meanwhile an `-input` that is one mbox file, or a subfolder under `ImapMail/<host>`, makes
the walk see part of an account. For Gmail X is then tiny ("behind" forever). For other IMAP the
unread folders read 0 of N.

**Fix.**

- Completeness is computed only when the walk covered a whole `ImapMail/<host>` store, using the
  original input `path` (as the token does, `run.go:1672-1683`), never `openPath`.
- A partial `-input` under `ImapMail/` → `unknown` (neutral words, no WARN).
- Replace the copy-first sentence with this rule.

### TC-12 — MED — lenses 1, 3, 10 — §2.3 "What MailArchive uses instead", §7 "stale"

**Scenario.** Thunderbird has not run for three weeks. The "as of" is the **newest**
`lastSyncTimeInSec`, so one fresh INBOX hides weeks-stale folders. The words call it "Thunderbird's last
check of the server", but the field is the end of a pass over Thunderbird's own local list
(`nsAutoSyncState.cpp:353-358`). A `caught_up` verdict keeps the lead "Export complete." on figures
three weeks old.

**Fix.**

- Use the **oldest** `lastSyncTimeInSec` among kept folders with mail as the "as of".
- Word it "Thunderbird's count from DATE", not "check of the server".
- When it is more than 3 days old: the lead is "Backup finished.", and `caught_up` reads "as of DATE,
  Thunderbird had downloaded everything it listed".
- A time in the future (the clock moved back) counts as stale.
- A value ≤ 0 counts as never checked. That includes the year-2038 wrap of Thunderbird's int32
  seconds, `nsImapMailFolder.cpp:1948`, which fails safe.

MA-1179 gains these.

### TC-13 — MED — lenses 10, 4, 9 — §6, MA-1191

**Scenario.** On a current build the account-level checkbox is "Save copies of all folders and
messages on this computer for offline use" (§2), not the design's wording, and the size limit is "Skip
downloading messages larger than:". ESR 140 likely still shows the older page **(recollection,
unverified)**. One fixed sentence is wrong for one group or the other. The wizard's existing "right-click
the account → Download / Sync Now" (`wizard.go:1622`, `:1634`) is unverified too.

**Fix.** Choose the fix words by Thunderbird version, read from the profile's `compatibility.ini`
`LastVersion`, with version-neutral words when it is unknown. MA-1191 covers ESR 140 and the current
Release, and adds the wizard's instruction.

### TC-14 — MED — lenses 9, 10 — §16, §11 last line, §12 L rows

**Scenario.** Every Thunderbird claim is proven at trunk ab60cc5e (committed the day of the design).
Users run ESR 140 or Release. A field, a default or the Panorama gate may differ there.

**Fix.**

- Re-read the load-bearing claims at the ESR branch in use (the cache fields, the save rules, the
  Gmail single-copy lookup, the Panorama gate, the `autosync_offline_stores` default). Label each
  "[proven at trunk]" or "[proven at ESR 140]".
- The release notes name the Thunderbird versions the lab rows ran on.
- §11's "re-checked at each Thunderbird ESR" gets an owner: who runs it, and what triggers it (an ESR
  release date, like the Go toolchain revisit).

### TC-15 — MED — lenses 6, 7 — §4.1 step 2, MA-1171

**Scenario.** `folderCache.json` is a pipe, a link to `/dev/zero`, or a link out of the profile. A
plain open blocks the backup or reads outside the profile. JSON numbers like `1e308` or `-0`, a
non-object value per key, and duplicate keys are unstated.

**Fix.**

- Open with the repo's regular-file opener: refuse anything not a regular file, never wait on it (the
  MA-767 precedent for verify records). Read through a 16 MB limit.
- Decode with `json.Number` and the TC-2 rules; skip a non-object value per key (counted), and if it
  is a kept folder the account is `unknown`.
- Keep the file in memory only; no write, ever.

MA-1171 gains a pipe and a link.

### TC-16 — HIGH — lenses 10, 1, 7 — §2.4 adjustment 1, Q7 (found by the rescue pass, N1)

**Scenario.** Thunderbird keeps the bodies of deleted or moved IMAP messages in the offline mbox until
it compacts the folder. It tracks their size in `expungedBytes`, and compaction waits for a threshold
and may ask first (§3). The design assumes these copies carry the Expunged bit 0x0008 in
`X-Mozilla-Status`, but that is unverified (§2). If they do not, they count as on disk, and in one
folder they can cover the same number of messages never downloaded. The result is a false `caught_up`,
breaking R42.

This is not hypothetical: the owner's Outlook INBOX holds 18.7 MB of such copies against 910
messages listed. The owner's Gmail folders show `expungedBytes` 0 even though All Mail and INBOX have
more separators than listed (+32, +6, the designer's figures). So the Gmail extras are something else.
A different split of "From " lines between the Go mbox reader and Thunderbird would inflate X the same
way.

**Fix.**

- Read `expungedBytes` per folder.
- When it is above 0, subtract an estimate of the stale copies before the test:
  `expungedBytes ÷ (folderSize ÷ listed)`, rounded up. For Gmail, subtract the sum across the
  account from X.
- The subtraction is skipped only once a lab row proves those copies carry 0x0008 and the walk
  excludes them.
- Label the 0x0008 adjustment [unverified].
- Add a pending lab row (Windows first): delete and move messages in a throwaway IMAP profile, then
  record the separators, how many carry 0x0008, `expungedBytes`, and the walk's count, before and
  after compaction.
- Until it runs, release notes do not claim the count is exact.

MA-1173 gains a fixture with `expungedBytes` > 0 and extra unflagged copies → not `caught_up`.

### TC-17 — MED — lenses 4, 10 — §7 headline sentences

**Scenario.** A first-time user runs the wizard once with no schedule. The end summary promises "later
backups add the rest", which nothing will do.

**Fix.** The second half depends on whether a schedule exists for the archive. With none: "Run the
backup again once Thunderbird has downloaded more to add the rest." MA-1181 gains the no-schedule case.

### TC-18 — LOW — lens 2 — §8, §13 C6, Q4

**Scenario.** C6 makes presync read the folder cache and changes the wait rule of a state machine that
has its own reviews. It buys at most the existing 20-minute cap against a download that takes days.

**Fix.** Move C6 out of this bundle into its own short note, after C3 has shipped and the lab rows give
real download rates. Q4's answer stands either way.

### TC-19 — LOW — lens 4 — §5 posture bullets, Q3

**Scenario.** A person who keeps 30 days on purpose sees WARN `thunderbird_limited` on every `status`,
forever. A person who unticked folders on purpose sees `thunderbird_not_kept` forever (outside the
Gmail case, TC-8).

**Fix.** WARN on the first run that shows the setting, then a note while the setting is unchanged.
A fingerprint of the setting goes in the record. Lead question L3.

### TC-20 — LOW — lens 4 — §5 "stopped downloading"

**Scenario.** "It takes the same email slot" can be read as one email per streak in total. Then a
download that stalls after the still-downloading email is never told by email.

**Fix.** At most one still-downloading and one stopped email per streak. A stopped notice may follow a
still-downloading one, but never the reverse. MA-1184 gains the sequence.

### TC-21 — LOW — lenses 5, 7 — §2.4, §4.1

**Scenario.** Nothing says whether the cache is read before or after the walk. A 238,000-message walk
takes long enough for Thunderbird to list new mail. Reading first is the optimistic order (Y smaller,
X maybe larger).

**Fix.** Read it once, after the walk, and say so.

### TC-22 — LOW — lens 1 — §3

**Scenario.** Gmail has an IMAP setting that limits each folder to the newest 1,000 / 2,000 / 5,000 /
10,000 messages **(recollection, unverified)**. Every count, All Mail's included, is then capped, and
"downloaded everything it lists" is true of the cap only.

**Fix.** When All Mail's listed count equals one of those values exactly, add a note that Gmail may be
showing only the newest N messages over IMAP. Add a lab row (or drop the note if the setting is gone).

### TC-23 — LOW — lens 10 — §3 X rule, MA-1190

**Scenario.** If Gmail holds two messages with one Message-ID often enough, the distinct count stays
below All Mail's listed count by more than the tolerance. The result is a permanent false "behind"
and a "stopped" email.

**Fix.** MA-1190 must assert, on the owner's caught-up profile, that X is within tolerance of All
Mail's listed count. If it is not, fall back to a key of Message-ID plus Date plus size.

### TC-24 — LOW — lens 10 — §1, §9.2, BUILD STATUS line

**Fix.**

- `source.go:151` is an extension exclusion, not an existence check.
- `mozillaReadFlag` is at `mbox.go:283`.
- §9.2's guard is MA-904, not `leak/tree.go` (TC-6).
- The status line cites this file, `docs/review-thunderbird-completeness-10lens.md`.

## 5. Questions

### 5.1 For the owner (product choices)

- **O1 (TC-6, Q3b). Encrypted archives: where does "still downloading" live?** Recommend: **on screen
  only**, while the archive is unlocked, the way the Thunderbird sync result already shows. Nothing
  new is written beside the archive, and encrypted archives get no "still downloading" email. The
  alternative, the plaintext yes/no, means amending the rule that nothing about Thunderbird is written
  beside an encrypted archive (C-16). Keeping it inside the encrypted record waits for a part of the
  encrypted format that is not built yet (A24).
- **O2 (TC-4, Q1). The tolerance.** Recommend: within 20 messages or 0.2%, whichever is larger, but
  never more than 10% of the total; nothing on disk is never "caught up"; and the words show both
  numbers whenever any are missing.
- **O3 (TC-1). Gmail with All Mail hidden.** Recommend: say "Thunderbird lists at least Y; MailArchive
  cannot tell whether that is all of it" and never "complete". The other way would ask the person to
  turn All Mail on.
- Q2 (success with a note), Q3 (WARN from the first behind run), Q5, Q6, Q7 and Q8: this review has no
  objection to the design's recommendations.

### 5.2 For the lead (technical; the owner has delegated these)

- **L1 (TC-18).** Take C6 out of this bundle. Recommend yes.
- **L2.** C5 (the notices) ships with C3 or right after it. Recommend right after C3's friction review,
  so the email words are walked once the screen words exist.
- **L3 (TC-19).** WARN once, then a note, for a deliberate limit. Recommend yes.
- **L4 (TC-3).** The Panorama signal: `panorama.sqlite`'s presence, the pref, or both. Recommend
  both, either one → `unknown`.
- **L5 (TC-13).** Version-chosen fix words from `compatibility.ini`, or version-neutral words only.
  Recommend version-chosen with a neutral fallback.

## 6. Side findings (existing code, outside this design)

- **A. `FindAccountForStore` can pick the wrong account** when two servers share a hostname
  (`thunderbird.go:97-104`, map order). Today the wizard and `-enable-offline` pass that account to
  `EnableOffline` (`wizard.go:1594-1598`, `main.go:2133`), so they can switch on offline download for
  the wrong account and report the right one as done. Worth its own small fix and test, whatever this
  design does.
- **B. `IsImapStore`'s comment** says everything under `Mail/` is "always fully local"
  (`thunderbird.go:19-21`). Thunderbird's Exchange accounts break that (§2). Any other caller relying
  on it inherits the gap.
- **C.** The missing Flatpak path in backup discovery (the design's own §10 finding) is confirmed
  (`run.go:1886-1893` against `tbrestore/profiles.go:432-436`).

## 7. Counters

- Date worked on: 7 November 2026. Clock read from the session; not inferred.
- Register: review synthesis. It holds findings (code and Thunderbird source read), the synthesiser's
  decisions, and opinion where marked. Statements marked "recollection, unverified" are opinion.
- Drew on, and reached:
  - the design at 552fc98 (digest above), and `../assurance-kit/process/design-review.md`;
  - the MailArchive code listed in §1, read directly;
  - Thunderbird source at ab60cc5e, fetched fresh: `nsMsgFolderCache.{cpp,h}`, `nsMsgDBFolder.cpp`,
    `nsImapMailFolder.cpp`, `nsImapIncomingServer.cpp`, `nsAutoSyncManager.{cpp,h}`,
    `nsAutoSyncState.cpp`, `nsMsgIncomingServer.cpp`, `nsMsgFolderFlags.idl`, `nsMsgMessageFlags.idl`,
    `mailnews.js`, `folderprops.ftl`, `am-offline.dtd`, `accountSettings.ftl`,
    `nsMailDirProvider.cpp`, `ExchangeProtocolInfo.cpp`, `panorama/src/DatabaseCore.cpp`; GitHub code
    search (default branch) for `folderCache.json`, the offline labels and their use;
  - the owner's `folderCache.json` (counts and flags only, read-only);
  - `docs/design-presync-attended.md` §4.4 and `docs/scenario-catalog.md` row MA-904;
  - the rescue agent's report (§3).
- Not reached or not checked, per claim:
  - the owner's `prefs.js` and mbox files: not read, so the INBOX "+6" and All Mail "+32" separator
    figures and the `is_gmail` flag are the designer's;
  - the ESR 140 branch: not read; every source claim here is trunk (TC-14);
  - the full Panorama migration path: only `DatabaseCore.cpp` and a default-branch code search
    (TC-3's lab row settles it);
  - Gmail's IMAP folder-size setting and Message-ID behaviour: recollection (TC-22, TC-23);
  - no Thunderbird, test or lab row was run.
- The smallest check that would settle the most: on the Windows VM, in a throwaway IMAP profile,
  delete a few downloaded messages and see whether their copies in the mbox carry 0x0008 before
  compaction (TC-16). Then convert or move the profile and see whether `folderCache.json` stays and
  with which keys (TC-3).
- Borrowed lines: short quotes from the files cited and from design-presync-attended §4.4; the rescue
  escapes are the rescue agent's, adopted with attribution.
- Drift: none noticed; the review stayed on the design as frozen.
- Touched: this file only (new), and one commit. The design text and product code are unchanged.
  Review helper scripts and downloaded Thunderbird source sit in the main checkout's untracked
  `.claude/` folder, outside the worktree.
- Persona: none.
- Source reached, per source:
  - design: yes;
  - repo code: yes (as listed);
  - Thunderbird source at trunk: yes (as listed);
  - ESR source: no;
  - owner profile: `folderCache.json` only;
  - Mozilla bug tracker: no (bug 1726660 is cited from Thunderbird's own comment);
  - assurance-kit process: yes.
