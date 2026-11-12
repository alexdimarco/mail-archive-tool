# Thunderbird completeness: the source re-read at ESR 153 and Release 159 (M4)

Source re-read for `docs/design-thunderbird-completeness.md` (fold check item M4, the TC-14
re-read), 7 November 2026. It is the lead's C0 task, done by a delegated agent. It reads source
only: no Thunderbird was run, and no profile was opened. The lab rows (MA-1189, MA-1191, MA-1192,
MA-1199, MA-1200) still owe the real-install half.

**Result in one line:** every load-bearing claim holds at ESR 153.6.0esr and at Release 159.0,
except the account-settings **labels**. The design's "Keep messages for this account on this
computer" is wrong at every version read. The size-limit label is wrong for IMAP. And the new
labels first appear in **Thunderbird 160**, which is not released yet. Three design notes follow
from this (§3).

- **Date worked on:** 7 November 2026 (the session's date).
- **Register:** a source-reading finding. The "design change" column is the reader's opinion,
  which the lead may overrule.

## 1. Which versions, and where the source came from

**Current versions.** Mozilla's product-details feed
(`https://product-details.mozilla.org/1.0/thunderbird_versions.json`, fetched 2026-11-07) says:
`LATEST_THUNDERBIRD_VERSION` 159.0, `THUNDERBIRD_ESR` 153.6.0esr, `THUNDERBIRD_ESR_NEXT` empty,
`LATEST_THUNDERBIRD_DEVEL_VERSION` 160.0b5 and `LATEST_THUNDERBIRD_NIGHTLY_VERSION` 162.0a1. So
153 is the newest ESR and 159 is the current Release, as M4 expected. This feed is the data
behind Thunderbird's release pages. The HTML release page itself was not opened.

**Where the source came from.** The GitHub mirror `mozilla/releases-comm-central` carries only
trunk (`master` = ab60cc5ea7300498991dfaaa84c33204bdb482e4, the review's revision) and branches
from 2009 to 2011. It has no ESR branch and no recent release tag (`git ls-remote`, 2026-11-07).
So the ESR and the Release were read from Mozilla's Mercurial server, by exact revision:

| What | Repository | Tag | Revision |
|---|---|---|---|
| ESR 153.6.0esr (the labs' version) | `hg.mozilla.org/releases/comm-esr153` | `THUNDERBIRD_153_6_0esr_RELEASE` | c3fd7a6092572ef295fd562948356e5cea174bbb |
| Release 159.0 | `hg.mozilla.org/releases/comm-release` | `THUNDERBIRD_159_0_RELEASE` | 1e7f8bb744a4a8e407f17b79d3ff59694ef80f54 |
| 160.0 beta 1 (labels only) | `hg.mozilla.org/releases/comm-beta` | `THUNDERBIRD_160_0b1_RELEASE` | 22dd355df01378a6cc185a95b70f2fa1f29776da |
| 160.0 build 1 (labels only) | `hg.mozilla.org/releases/comm-release` | `THUNDERBIRD_160_0_BUILD1` (tagged 2026-11-06, no RELEASE tag yet) | f8677769eb91b7a150203ad940479e83a8d4c071 |
| trunk, for comparison | GitHub mirror `master` | — | ab60cc5ea7300498991dfaaa84c33204bdb482e4 (the review's downloaded copy) |

Also seen: `THUNDERBIRD_153_7_0esr_BUILD1` (8070f5d975d6) is tagged on comm-esr153, with no
RELEASE tag yet. The next ESR dot release is near; it is a dot release, not a new ESR, so it is
not a re-read trigger by itself.

Each file was fetched as `raw-file/<revision>/<path>` into the main checkout's untracked
`.claude/tbsrc-esr153/` and read as text. Nothing fetched was run.

## 2. Each claim

"Holds" means the same rule, name and type at both versions. The line numbers are the ones read;
they shift between versions, and the design's citations are trunk lines.

| # | Claim (design §) | ESR 153.6.0esr | Release 159.0 | Result | Design change |
|---|---|---|---|---|---|
| 1a | Folder-cache field names and types: `flags` and `userSortOrder` unsigned 32-bit; `totalMsgs`, `totalUnreadMsgs`, `pendingUnreadMsgs` and `pendingMsgs` signed 32-bit; `expungedBytes` and `folderSize` signed 64-bit, written for **every** folder (§2.1 fold) | `nsMsgDBFolder.cpp:1218-1236` (`WriteToFolderCacheElem`, fields at 1226-1233) | `:1232-1250` (fields at 1240-1247, the same lines as trunk) | **holds** | none. §4.1 step 3's "if the ESR does not write `expungedBytes`, decide again" is settled: it is written. Note added there |
| 1b | IMAP fields: `boxFlags`, `hierDelim`, `onlineName` (string), `aclFlags`, `serverTotal`, `serverUnseen`, `serverRecent`, `nextUID`, and `lastSyncTimeInSec` (signed 32-bit, seconds) **only when** the folder has a background-download state object | `nsImapMailFolder.cpp:1890-1917` | `:1963-1991` | **holds** | none. "Absent = never checked" (§4.1 step 3 fold) fits: the field is written only when that object exists |
| 1c | Listed = `totalMsgs` + `pendingMsgs`; the count starts at −1 | `GetTotalMessages` `nsMsgDBFolder.cpp:3899`; `mNumTotalMessages(-1)` `:236`; `ChangeNumPendingTotalMessages` `:3969` | `GetTotalMessages` `:3978` (the sum at `:3995`); `:238`; `:4048` | **holds** | none |
| 1d | `serverTotal` set from the server's count | `m_numServerTotalMessages = numTotal` `nsImapMailFolder.cpp:2753` | the same line located at `:2806` | holds | none |
| 2 | **When the cache is written:** one minute after a change (`kSaveDelayMs`); at close (the destructor calls `Flush`); through a safe temp-file stream; under Panorama the cache is never created | `nsMsgFolderCache.h:42`; `nsMsgFolderCache.cpp:349-361` (`SetModified`), `:243-262` (destructor and `Flush`), `:297-330` (`SaveFolderCache`, safe stream at `:306-327`), `:182-187` (Panorama refusal) | the same file, **byte-identical** to ESR 153 and to trunk | **holds** | none |
| 2b | What gets written, and when: every folder of every server's tree at shutdown, deep; one folder whenever its totals are updated | `nsMsgAccountManager.cpp:226` (`Shutdown` calls `WriteToFolderCache`), `:1795-1806`; `nsMsgIncomingServer.cpp:297-309` (root, deep); `nsMsgDBFolder.cpp:1186-1216` (each folder at its current key, then its subfolders); `:3851` (`UpdateSummaryTotals` calls `FlushToFolderCache`) | the same calls located: `nsMsgAccountManager.cpp:226`, `nsMsgIncomingServer.cpp:308`, `nsMsgDBFolder.cpp:1200`, `:3930` | holds | feeds claim 9 |
| 3a | **Gmail keep-one-copy rule:** `HasMsgOffline` asks `GetOfflineMsgFolder`, which looks in other folders by X-GM-LABELS and X-GM-MSGID **only when** `GetIsGMailServer` is true | `nsImapMailFolder.cpp:8826-8833` (`HasMsgOffline`), `:8835-8925` (`GetOfflineMsgFolder`; the gate at `:8860-8867`; X-GM-MSGID at `:8915`) | `:9016-9023`, `:9025` onward; the gate at `:9054` | **holds** | none |
| 3b | The download queue skips a message `HasMsgOffline` finds; zero-byte messages are skipped | `nsAutoSyncState.cpp:256`, `:334` (`HasMsgOffline`), `:273` (`if (!msgSize) continue`) | `nsAutoSyncState.cpp` byte-identical to ESR 153 | **holds** | none. Trunk adds two include lines, so trunk's numbers are about 2 higher |
| 3c | `is_gmail` is a per-server setting, set from the server's Gmail capability | `nsImapIncomingServer.cpp:302` (`NS_IMPL_SERVERPREF_BOOL(... IsGMailServer, "is_gmail")`), `:2050` (set from `kGmailImapCapability`) | `:399`, `:2163` | **holds** | none. The design's "a never-connected profile has no `is_gmail` yet" fits: it is set only when the server's capabilities arrive |
| 4 | **All Mail is flagged 0x8000 and 0x4000; Archive 0x4000 alone comes from a server `\Archive` folder or the identity's archive folder** (M5) | `nsImapMailFolder.cpp:1161-1164` ("Treat the GMail all mail folder as the archive folder": `kImapAllMail` → Archive \| AllMail; `kImapArchive` → Archive); `nsMsgAccountManager.cpp:1546` (identity archive folder → Archive); Thunderbird's own All Mail lookup by Archive at `nsImapMailFolder.cpp:8885-8887` | `:1237-1239`; `nsMsgAccountManager.cpp:1542`; lookup at `:9075-9077` | **holds** | none. M5 stands |
| 5 | **Flag values:** Offline 0x08000000, AllMail 0x8000, Archive 0x4000, ImapBox 0x2000, ImapNoselect 0x01000000, Virtual 0x20, Trash 0x100, Junk 0x40000000, Inbox 0x1000, SentMail 0x200 | `nsMsgFolderFlags.idl:41,64,66,72,74,76,78,96,103,109` | the same values; it differs from ESR 153 only in renaming unused flag 0x02000000 (`CreatedOffline` → `Unused1`); identical to trunk | **holds** | none. At ESR 153, Offline is line 103 and Junk 109 (trunk: 102, 108) |
| 5b | Message flags: Expunged 0x0008, Offline 0x80 | `nsMsgMessageFlags.idl`, byte-identical to trunk | byte-identical | **holds** | none |
| 5c | Trash and Junk never get the keep-offline flag | `nsImapMailFolder.cpp:385-387`, `:940-942` | the same comments located at `:414`, `:1013` | holds | none |
| 6 | **`autosync_offline_stores`:** default true; when false the account is never downloaded in the background. Background download runs for `imap` and `ews` types only, and skips an IMAP server not logged in this session | default `mailnews.js:613`; check `nsAutoSyncManager.cpp:801-807`; types `:816`; logged-in skip `:824-841`; setting name `nsMsgIncomingServer.cpp:1423` | default `mailnews.js:617` | **holds** | none. Trunk's manager differs from ESR 153 only in how it drops deleted folders from its queues (after line 270), not in any rule used here |
| 6b | Limit defaults: `limit_offline_message_size` false, `max_size` 50 (KB), `autosync_max_age_days` −1 (no age limit), `offline_download` true | `mailnews.js:539-540`, `:614`, `:617`; names `nsMsgIncomingServer.cpp:1410-1412`, `:1426` | `mailnews.js:543-544`, `:618`, `:621`; names `:1413-1415`, `:1429` | **holds** | none |
| 7 | **Panorama:** file `panorama.sqlite` in the profile folder; setting `mail.panorama.enabled`, a boolean, default false, read **once at start-up** (`mirror: once`); with it on, the folder cache is neither created nor written | `DatabaseCore.cpp:136-151`; `mail/app/StaticPrefList.yaml:43-46`; the refusals at `nsMsgFolderCache.cpp:182-187`, `nsMsgDBFolder.cpp:1218-1223`, `nsImapMailFolder.cpp:1890-1895` | `DatabaseCore.cpp:151`; `StaticPrefList.yaml:64-67` | **holds** | none. One nuance for MA-1200: a pref set while Thunderbird runs takes effect only at its next start |
| 8 | **`lastSyncTimeInSec`'s meaning:** set (to now) when Thunderbird finishes a pass over the folder's local headers, queuing bodies to download; not a check of the server | `nsAutoSyncState.cpp:355` (`mLastSyncTime = PR_Now()`), `:748-749` (read back from the cache) | byte-identical | **holds** | none |
| 9 | **(M2) Thunderbird writes a current-path entry for every folder it has loaded, and every folder it knows has a `.msf` in the store** | Entries: see 2b. Every folder in each server's tree is written at its current key **at shutdown** (deep), and a single folder whenever its totals update. Folders: an IMAP account's folders are found at start-up by listing the store's **`.msf` files**, not its mailbox files (`nsImapMailFolder::CreateSubFolders`, `nsImapMailFolder.cpp:404-508`; the `.msf` ending compared case-blind at `:430-432`). The lookup by current key at `:447-451` fails for a moved profile's old keys, which is why old keys linger | `CreateSubFolders` located at `nsImapMailFolder.cpp:435`, its `.msf` test at `:458`; shutdown write as in 2b | **holds, with one limit** | **Limit:** "at the latest when it closes" needs a *clean* close. A crash or a forced quit skips the shutdown write. A folder first created during a session was not traced. Note added beside M2 (§4.1 step 2). The `.msf` finding is also why the M6-residual rule can use "holds any `.msf`" |
| 9b | Old keys are removed only when a folder is deleted (bug 1726660) | removal `nsMsgDBFolder.cpp:3343-3356`, `nsMsgAccountManager.cpp:692`; the bug-1726660 comment `nsMsgDBFolder.cpp:1873-1876` | comment `:1890-1893` | **holds** | none |
| 10 | **(M7) `expungedBytes` counts offline (on-disk) bytes** | `nsImapMailFolder.cpp:1302-1328` (`MarkPendingRemoval`: adds or removes `GetOfflineMessageSize`). A **second path:** deleting a header whose body is on disk adds its on-disk size (`mailnews/db/msgdb/src/nsImapMailDatabase.cpp:49-58`, `AdjustExpungedBytesOnDelete`, ESR 153) | `nsImapMailFolder.cpp:1378-1404` | **holds** | none. Both paths count on-disk bytes, so M7's rule stands. The second path is the usual one for a deleted message |
| 11 | **`nsShouldIgnoreFile`** (which names Thunderbird treats as not a folder) | `mailnews/local/src/nsMsgLocalStoreUtils.cpp:31-87`: a leading `.` or `#`, a trailing `~`, the named filter, feed and pop files, the endings `.snm` and `.toc`, `feeditems_error…`, hidden and "special" files, and the endings `.mozmsgs`, `.sbd` and `.msf` | identical (the file differs by one added include line) | **holds** | none for this design. It matches MailArchive's port in `internal/tbrestore/names.go:101-134`, except that the port does not model Thunderbird's hidden and special-file checks (`:75-81`), which only matter for files the restore does not write. In the mbox store a `.msf` is never a folder name (`nsMsgBrkMBoxStore.cpp:142`, ESR 153); in an IMAP store the `.msf` files *are* how the folders are found (claim 9) |
| 12 | **The account checkbox label, and the version it changed** (§6, TC-13) | `am-offline.xhtml` with `am-offline.dtd`: group "Message Synchronizing" (`syncGroupTitle.label`); checkbox **"Keep messages in all folders for this account on this computer"** (`allFoldersOffline2.label`, `am-offline.xhtml:67`, hidden for pop3, nntp and none); group "Disk Space"; **"Synchronize all messages locally regardless of age"** (`allAutosync.label`); "Synchronize the most recent" N Days/Weeks/Months/Years; for IMAP and Exchange **"Don't download messages larger than" N "KB"** (`autosyncNotDownload.label`, `:189-191`). "Messages larger than" (`offlineNotDownload.label`) is **hidden for imap and ews** (`:182`). Panel name "Synchronization & Storage" (`chrome/messenger/prefs.properties:53`). Folder dialog: "Synchronization", "Select this folder for offline use", "Download Now" (`chrome/messenger/folderProps.dtd:15,19,21`) | `am-offline.xhtml` and `am-offline.dtd` **byte-identical** to ESR 153; `prefs.properties:53` the same; the folder dialog's words are the same, now in `messenger/folderprops.ftl` (`folder-props-synchronization-tab`, `folder-props-select-for-offline`, `folder-props-download-now`) | **differs** from the design | **(a)** The design's "Keep messages for this account on this computer" is not in any version read. Read "Keep messages in all folders for this account on this computer". **(b)** The design's "The size limit names 'Messages larger than … KB'" is wrong for IMAP: read "Don't download messages larger than … KB". **(c)** The new labels arrive in **160**: "Offline message storage", **"Save copies of all folders and messages on this computer for offline use"**, "Download limits" and **"Skip downloading messages larger than:"** (`messenger/accountSettings.ftl:69-88` at 160.0b1 and at 160.0 build 1; absent at 159.0). So the switch-over version is **160**. No 160 age-limit label was found in that file. Notes added beside §6 |
| — | Exchange stores default under `Mail/` (`ExchangeProtocolInfo.cpp:34-36`, TC-5) | not re-read: the file is not at `mailnews/protocols/ews/src/`, and its ESR path was not found | not re-read | **not checked** | none. The design already treats Exchange as `unknown` until MA-1197 |

## 3. What changes in the design

Dated notes, tagged `[M4 re-read]`, were added beside the text they amend. No reviewed text was
rewritten.

1. **§6 (the fix-it words):** the account-level label, the IMAP size-limit label, and the
   switch-over version 160. Version-chosen words still wait for MA-1191, as the TC-13 fold says:
   this re-read is the source half only.
2. **§4.1 step 3:** `expungedBytes` is written for every folder at ESR 153 and Release 159, so
   "absent → `unknown`" stays, with nothing left to decide.
3. **§4.1 step 2 (M2's condition):** relabelled as proven at ESR 153 in source, with the
   clean-close limit.
4. **§16:** the re-read is recorded, with the revisions, and every "[proven at trunk]" Thunderbird
   claim reads "[proven at ESR 153.6.0esr and Release 159.0]", except item 12 (changed) and the
   Exchange layout (not re-read).

## 4. Not checked, per claim

- **ESR 140:** not re-read. The feed lists no ESR but 153, and the labs ran 153.
- **The wizard's "right-click the account → Download / Sync Now"** and Thunderbird's download-now
  command for TC-9: not read (MA-1191).
- **The 160 age-limit wording:** not found in `accountSettings.ftl`, and its other files not read.
- **Whether 160 still names the panel "Synchronization & Storage":** not read.
- **Folders first created during a session** (claim 9): whether their `.msf` exists before the
  first cache write was not traced.
- **Whether a deleted message's leftover offline copy carries the Expunged bit 0x0008 in the
  mbox:** not read. It stays with MA-1199.
- **Claims 1d, 2b, 5c and 9 at Release 159:** read line by line at ESR 153. At 159 the same code
  was located (lines given in §2) but not read line by line.
- **Exchange layout:** not re-read (above).
- **The release page's HTML:** not opened; the product-details feed was used.

**The smallest check that would settle the most:** lab row MA-1191 on the Windows VM, with
153.6.0esr. Open Account Settings → Synchronization & Storage for a throwaway IMAP account, and
check that the three labels in item 12 read as quoted. Then do the same on the Mac with 159.0.

## 5. Counters

- **Register:** a source-reading finding report.
- **Borrowed lines:** Thunderbird's label strings and identifiers, quoted verbatim from the files
  named, by revision.
- **Drift:** none noticed. The work stayed on the claims M4 and the brief named, plus `nsShouldIgnoreFile`.
- **Touched:** this file (new), dated notes in `docs/design-thunderbird-completeness.md`, and
  untracked downloads under the main checkout's `.claude/tbsrc-esr153/` (fetch scripts under
  `.claude/tbsrc-scripts/`). No product code.
- **Persona:** none.
- **Source reached, per source:** comm-esr153 at c3fd7a60: yes, for the files in §2.
  comm-release at 1e7f8bb7: yes, for the same files. comm-beta at 22dd355d and comm-release at
  f8677769: one file each (`accountSettings.ftl`). Trunk: the review's local copy, for diffs
  only. The product-details feed: yes. The GitHub mirror: refs only. The Mozilla bug tracker: no.
  Any real install or profile: no.
