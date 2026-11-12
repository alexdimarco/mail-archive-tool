# Adversarial pass: C1a's folderCache.json reader, and B1's change to the C0c prefs reader

**Verdict: NOT CLEAN.** 1 MED and 6 LOW are CONFIRMED, each by a red test in the attack clone.
B1 (GmailForStore, the findAccount refactor, IMAP-only) HELD.

- **The MED (C1A-01) comes from the lead's run on the owner's real profile, and the source
  confirms it.** Thunderbird writes `folderSize: -1` for a folder whose size it has not worked
  out yet. The reader calls that broken, so 4 of 35 real folders are not Valid. C1b would then say
  "can't tell" for ordinary accounts. The error is on the safe side (it never says "complete"
  wrongly), but it makes the check useless on real profiles. The rule to change is the design's own
  (§4.1 step 3, "negative values only on the account root"). The builder followed it as written.
- **The six LOWs** are key shapes and file shapes that Thunderbird never writes, or real but rare
  path spellings. Five of them fail safe (`unknown`). Two (C1A-06, C1A-07) can make the reader's
  facts wrong, but no test reached a false "complete" through them.
- **Everything else HELD**: links, pipes, sparse files, nesting, duplicates hidden by escapes,
  number forms, `..` hidden by Unicode, streams, UNC tails, Windows device names, the All Mail
  cases, the Panorama signals, LastVersionMajor, and every B1 case.

| Field | Value |
|---|---|
| Reviewed | `git show b32947e` (internal/tbcomplete/, internal/thunderbird/panorama.go, catalog MA-1170/1171/1235, design §13 "Built 2026-11-08 [C1a]"); `git show 5f92b89 -- internal/thunderbird/thunderbird.go`; `e8228fd`. Branch `tb-completeness`, origin tip `b32947e` |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/tbcomplete/cache.go` a12f7381…1b8e; `internal/tbcomplete/profile.go` 4532784e…8c71; `internal/thunderbird/panorama.go` f36054e9…b697; `internal/thunderbird/gmail.go` 09f42cdb…be99; `internal/thunderbird/thunderbird.go` 74e3bd6a…3c53 |
| Reviewer | One model instance, acting as both finder and skeptic (the procedure asks for separate ones; that was not done). It did not write C1a or B1, and it answers under no name. Every verdict below is a self-report backed by a test the lead can re-run |
| Attack clone | `.claude/attack-c1a` in the main checkout, a throwaway clone at `b32947e`, safe to delete. New files: `internal/tbcomplete/zz_c1a_attack_test.go` (e09617f6…539e), `zz_c1a_attack_unix_test.go` (9e3a29c6…d847), `zz_c1a_attack_windows_test.go` (36b75274…08eb). Staged unchanged from `.claude/attack-c0c-r6` by `.claude/c1a-adv-stage.sh`: every C0c `zz_r*` and `r6_*` attack file. No product change |
| Thunderbird source | ESR 153 copies in `.claude/tbsrc-esr153/esr153/` (`nsMsgDBFolder.cpp`, `nsImapMailFolder.cpp`, `nsMsgFolderCache.cpp`, `nsImapIncomingServer.cpp`). Fetched on 2026-11-08 from the comm-central and mozilla-central tips into `.claude/tmp/`: `MailNewsTypes.h`, `nsIMsgFolder.idl`, `nsMsgDBFolder.h`, and Mozilla's bundled jsoncpp `json_reader.cpp`. The ESR 153 copies of those four files were not reached |
| Tools | go1.26.0 darwin/arm64 (macOS); the Windows 11 VM (amd64 build, ARM-emulated) via `.claude/c1a-adv-win.sh`. **No Linux run.** Only temp-folder profiles were used. No real profile was opened by this pass; the real-profile figures in C1A-01 are the lead's |
| Commands | `go -C .claude/attack-c1a test -count=1 -v -run AdvC1a ./internal/tbcomplete/`: exit 1, 28 attack tests, 6 red (C1A-01, 03, 04, 05, 06, 07). `zsh .claude/c1a-adv-win.sh`: all 16 product tests and the fuzz seeds PASS; the attack tests red on Windows are the same 6 plus C1A-08. `go -C .claude/attack-c1a test -count=1 -p 4 ./internal/thunderbird/ ./internal/wizard/ ./cmd/mailarchive/` with the C0c attack files staged: wizard ok, cmd ok, thunderbird red only on the 4 known reds of round 6's baseline (§3) |
| Count | 30 attack tests; 7 red = 7 CONFIRMED (1 MED, 6 LOW); 23 HELD |

## 1. Confirmed findings

| ID | Sev | Claim (file:line) | Evidence (test in the clone) | Suggested rule |
|---|---|---|---|---|
| C1A-01 | MED | `cache.go:708` reads any negative value off the root as Broken. Thunderbird writes `folderSize: -1` on folders whose size it never worked out, so a real, unedited profile is not Valid | `TestAdvC1a01FolderSizeUnknownIsNotBroken` red: `FolderSize={State:Broken Why:"negative (allowed only on the account root)"}`. Source in §2. The lead's real profile: 4 of 35 folders (3 in the Gmail store, 1 in another IMAP store), every other field ok | `folderSize` exactly -1 → **Missing** ("Thunderbird does not know the size"); any other negative → Broken. Fix the design's §4.1 step 3 "Negative values" bullet with it, and give C1a's fixtures a non-root `folderSize: -1` (today `helpers_test.go`'s `folder()` and `testdata/*.json` carry -1 on roots only, so no test met the real shape) |
| C1A-03 | LOW | `Parse` (`cache.go:287-321`) accepts any nesting up to Go's limit (10,000). Thunderbird's jsoncpp stops at 256 levels and then **discards the whole file** (`nsMsgFolderCache.cpp:281-287`): it starts an empty cache and later rewrites the file. So the reader trusts a file Thunderbird itself threw away | `TestAdvC1a03DeepNestingThunderbirdDiscards` red: 300 levels under an unrelated key parse. Mozilla's `json_reader.cpp` `setDefaults`: `stackLimit` 256 | Refuse nesting deeper than 2 (Thunderbird writes the top-level object, entry objects, and numbers or strings inside them), so the file is `CacheMalformed` and C1b says `unknown`. Only a hand-made file reaches this |
| C1A-04 | LOW | On Windows, `storeParts` (`cache.go:513-524`) and `exactTail` (`:539-554`) collapse the leading empty parts. So the UNC store `\\srv\share\h` and the key `\srv\share\h\A.msf` (the root of the current drive, which is another place) compare equal: the key is read as exact | `TestAdvC1a14bDriveRootKeyIsNotTheUNCStore` red | Keep the UNC `\\` prefix as its own root part on Windows. Thunderbird never writes a drive-root key, so only a planted file reaches this |
| C1A-05 | LOW | Case folding (macOS, Windows) and NFC (every OS) make a respelling of the profile's path that changes only case, or only the Unicode form, count as "the same path". Thunderbird looks a key up byte for byte (`nsMsgFolderCache.cpp:335`, `isMember`). After such a respelling it writes new keys and keeps the old ones, which it removes only when a folder is deleted (bug 1726660). So every folder gets two exact keys and stays a duplicate (`unknown`) for good. M2 said a moved profile must never be `unknown` for good | `TestAdvC1a15CaseOnlyRespellingIsNotForever` red: `Duplicates ["" "INBOX"]` | The lead's call. One option: when some key matches the store's spelling byte for byte, treat keys that match only after folding as old-path keys (ignored beside an exact one, M2); fold only when no key matches byte for byte. Real triggers are rare (a profile path whose case changes in profiles.ini, a renamed Windows user folder). It fails safe |
| C1A-06 | LOW | In tail mode, `nameTail` (`cache.go:560-568`) takes any key that has a part named like the store, so the **current** keys of another store with the same folder name become this store's folders. For example, a moved profile's `ImapMail/imap.example.com` and a second account whose local folder is `/Volumes/Data/imap.example.com`. In the test the moved store's All Mail came out as the other store's folder (`AllMailOne`) | `TestAdvC1a18TailModeTakesAnotherStoresKeys` red | Follow the design's own wording, "the key's path under `ImapMail/<host>/`": in tail mode require the parent part too (`ImapMail`, or `Mail` for a Mail/ store), or skip a key that is exact for another store the profile names. This mostly ends in duplicates (`unknown`) or extra listed folders (`behind`). No false "complete" was shown |
| C1A-07 | LOW | `Valid` (`cache.go:118`) asks for `serverTotal` only when the entry's own flags carry ImapBox 0x2000. An entry in an IMAP store with that bit cleared and no `serverTotal` is Valid, and `Listed` falls back to `totalMsgs + pendingMsgs`, which may be below the server's count. Thunderbird writes `serverTotal` for every IMAP folder (`nsImapMailFolder.cpp:1901`) | `TestAdvC1a20ImapStoreEntryWithoutServerTotal` red: Valid, Listed 3 | It follows §4.1 step 3 as written, but the flag comes from the same untrusted entry. Since C1 runs for IMAP stores only, require `serverTotal` on every non-root entry (in C1a, or in C1b using the store's kind). Only a hand edit reaches this, and a hand edit can fake any count anyway |
| C1A-08 | LOW | On Windows, an 8.3 short-name spelling on either side (keys long and store short, or the reverse) finds **no keys**, because even the leaf differs (`IMAPEX~1.COM` against `imap.example.com`). `filepath.EvalSymlinks` did not supply the long spelling here. Windows itself handed the test a short user folder (`C:\Users\ALEX~1.DIM`) | `TestAdvC1aW02ShortNameStore` red on the Windows VM: mode NoKeys both ways | Add the long-name spelling (`GetLongPathNameW`) as a further spelling of the store on Windows. It fails safe (`unknown`) |

## 2. The lead's three questions on C1A-01, from source

**(1) What Thunderbird writes for `folderSize`.** It writes the folder's `mFolderSize` as it is:
`element->SetCachedInt64("folderSize", mFolderSize)` (`nsMsgDBFolder.cpp` ESR 153 :1233). The
value starts at `kSizeUnknown` (constructor, :243). `kSizeUnknown` is
`const int64_t kSizeUnknown = -1;` (`mailnews/base/public/MailNewsTypes.h:18` at the comm-central
tip, fetched 2026-11-08; the ESR 153 copy was not reached, though ESR's `nsMsgDBFolder.cpp` includes
that header). `nsIMsgFolder.idl` (tip) says of `sizeOnDisk`: "-1 when the size isn't known". Every
change to it in ESR 153's IMAP code keeps it at -1 or above:

- the Trash's `SetSizeOnDisk(0)` (`nsImapMailFolder.cpp:1505`);
- the root's 0 (:1687);
- `mFolderSize += messageSize`, where the size is an unsigned 32-bit number (:2920);
- a fresh sum of unsigned sizes (:4644);
- `SetSizeOnDisk(mFolderSize)` (:5503).

So **-1 exactly** is the only negative Thunderbird's IMAP code writes. A side note: :2920 adds to a
-1 start, so a size can come out one byte low. That is harmless here. The local-folder store's size
code was not in the copies reached.

**(2) Other fields that may be -1 off the root in a real file.**

- **`totalMsgs`: yes, possibly.** It starts at -1 for every folder (`mNumTotalMessages(-1)`, :236;
  the source re-read's claim 1c). It is replaced only when the folder's summary is read (:667) or
  when the folder is read from the cache. Not traced: whether a folder can be written before
  either happens. Not seen in the owner's data. The reader's answer (not Valid, so "can't tell") is
  the right outcome, because Thunderbird itself has no count then. But C1b would give the reason
  `invalid_entry`, which says the file was changed outside Thunderbird. A reason of its own ("not
  counted by Thunderbird yet") would be truer. Test `TestAdvC1a02…` HELD.
- **`pendingMsgs`: no.** It starts at 0 (:242). It changes only by positive amounts
  (`UpdatePendingCounts`, `nsImapMailFolder.cpp:5297-5308`) and by resets to 0 (:2459, :5198).
- **`serverTotal`: no.** It starts at 0 (:222) and is set from the server's count (:2656, :2753).
  The protocol parser was not traced.
- **`expungedBytes`: not settled.** It starts at 0 (:238) and is taken from the folder's database
  (:669). The database's arithmetic (`nsDBFolderInfo`) was not reached, so a negative value from
  its accounting is not ruled out.
- **`lastSyncTimeInSec`**: 0 when never synced, negative only past 2038. The reader already
  accepts both.

**(3) Proposed rule.**

- `folderSize` -1 off the root → Missing (it is optional, so the entry stays Valid).
- `folderSize` < -1 → Broken.
- `totalMsgs` -1 off the root → still not Valid, with its own reason for C1b's words.
- Every other negative off the root → Broken, as today.

Record the source lines above beside the design's §4.1 step 3. Its "Thunderbird starts the count
at -1" is true of every folder, not just the root.

## 3. Held

| Attack | Verdict | Test |
|---|---|---|
| A link, a link to a pipe, a pipe, and a 1 GB sparse file at `folderCache.json`: no hang (5 s bound), and the sparse file refused by the size cap in under 3 s | HELD | `TestAdvC1aU01HostileFileAtTheName` |
| A file swapped during the read: `util.ReadRegularFile` (`openregular.go:67-88`) does an Lstat, then `OpenRegularFile`, and reads at most cap + 1 bytes from that one open file. `OpenRegularFile`'s own checks were reviewed in C0c's rounds and were not re-read or raced here | HELD (by reading only) | — |
| 16 MB of `[`, and 200,000 nested objects: an error, no crash | HELD | `TestAdvC1a03bHugeNestingNoCrash` |
| A duplicate key hidden by an escape (`\u0049NBOX`), and a field named twice through `fl\u0061gs` | HELD: both seen | `TestAdvC1a04…`, `TestAdvC1a05…` |
| Shapes jsoncpp reads but this reader refuses: a BOM, `//` and `/* */` comments, trailing commas, trailing data. Also NaN, hex, a leading zero, `+1`, single quotes, a missing comma | HELD: each refused, which is the safe direction (the file is `unknown`) | `TestAdvC1a06…` |
| Number forms per kind: `-0`; the edges of uint32, int32 and int64; 400 digits; `1E3`, `1e0`, `5.0`, `-0.0`, a quoted number, true, null, a list, an object. jsoncpp would read `1e3`, `5.0`, true and null as numbers; the reader calls them Broken, the safe direction | HELD | `TestAdvC1a07NumberForms` |
| A valid JSON that is not an object (`[]`, `"x"`, `1`, null, true, empty) | HELD | `TestAdvC1a08…` |
| `..` hidden by Unicode (fullwidth stops, dot leaders, a zero-width space, a combining accent): every Lstat stays inside the store, and none is read as still there | HELD | `TestAdvC1a09…` |
| A linked `.sbd` (also two levels deep), and a linked `.msf` pointing outside: never followed, each read as "still there" (can't tell). An unreadable `.sbd`: still there | HELD | `TestAdvC1aU02…`, `TestAdvC1aU03…` |
| Windows streams (`x.msf:$DATA`, `x.sbd:s\y.msf`), UNC and `\\?\` and `\\.\pipe` tails, and backslash tails on macOS | HELD: refused | `TestAdvC1a12…`, `TestAdvC1a14…` |
| Windows device names (CON, NUL, COM1, LPT1, AUX.sbd, CONIN$, `PRN `, nul.txt) as old-path keys, on the VM | HELD: no hang. Only NUL read as still there; the rest read as gone. They are not refused (`TestAdvC1a13` records that), but nothing is opened | `TestAdvC1aW01…` |
| NFC and NFD on Linux (two real files, one identity) | HELD: a duplicate, the safe direction | `TestAdvC1a16…` |
| All Mail: one; two (several); another store's (none); 0x4000 only (none); flags missing or a string on another folder (can't tell); a value that is not an object (can't tell) | HELD | `TestAdvC1a19AllMail` |
| Panorama setting: absent, true, a string, a number, user.js over prefs.js both ways, a `pref(` line, a hand-edited line, last line wins. Panorama file: a pipe, a folder, a dangling link (each "yes", never opened); a profile folder that cannot be searched (each signal "can't tell", version unknown) | HELD | `TestAdvC1a21…`, `TestAdvC1aU04…` |
| LastVersionMajor: letters, empty, a 23-digit major are "not known". `-5` and `+160` are read as -5 and 160; Thunderbird never writes either, and the number only picks which words are shown | HELD (note) | `TestAdvC1a22…` |
| B1 GmailForStore, 16 cases: own true or false; none; the default true; own false over the default true; user.js yes and no; a dead server's true (no) and its string (can't tell, by the widened type rule); a string or a number (can't tell); type `IMAP` in capitals and `pop3` (no); the type set in user.js (yes); a dotted key (ignored); a comment after the line (can't tell); two live accounts matched only by host name (can't tell). Each matches what Thunderbird applies (`NS_IMPL_SERVERPREF_BOOL(…IsGMailServer, "is_gmail")`, `nsImapIncomingServer.cpp:302`; set from the server's capability at :2050) | HELD | `TestAdvC1a23GmailForStore` |
| B1's findAccount refactor: C0c's attack files from rounds 1 to 6, rerun on `b32947e` | HELD: the same 4 reds as round 6's baseline (`.claude/c0c-r6-attack-rerun.txt`): `TestR2BlockCommentSpanningLines`, `TestR2TwoSettingsOnOneLine`, `TestR3ReaderHeld` and `TestR4StrictReadingHeld`, at the same lines. Those are earlier rounds' expectations that the allow-list reader superseded on purpose. Every `r6_*` test green; wizard ok; cmd/mailarchive ok | staged files |

## 4. Notes for C1b (not findings)

- **Keep L2's cautious reading. Do not narrow "refused" to "ignored and counted".** Two things
  rest on C1b reading every `Store.Refused` as `invalid_entry` (`unknown`):
  - A refused exact key does not put the store in exact mode. With an old-path key beside it, the
    store goes to tail mode and the stale key is read as the folder's entry
    (`TestAdvC1a24RefusedExactKeyLeavesTailMode`).
  - `allMail` never looks at a refused key's flags (`TestAdvC1a19`, "refused key").
- **`AllMailOne` can name an entry that is not Valid** (`TestAdvC1a19`, "not valid"). C1b must
  check `Valid()` before it uses that folder's count.
- **The Panorama setting is read from prefs.js and user.js only.** A setting forced by
  `policies.json` or an admin's autoconfig is not seen. `panorama.sqlite` is the second signal for
  that case. This was not tested on a real install.

## 5. What this pass did not do

- There was no Linux run; the Linux-only rules (NFC, no folding) were exercised as string tests
  under `goos` "linux".
- There was no fuzzing beyond the committed seeds, and no real Thunderbird run.
- The ESR 153 copies of `MailNewsTypes.h` and of jsoncpp were not reached; the tip copies were
  read instead.

**The smallest checks that would settle the rest:**

1. Open one of the owner's 4 real `folderSize: -1` folders in Thunderbird's folder properties,
   and confirm it shows no size. That confirms -1 means "not known" there.
2. `grep -c '"totalMsgs":-1' folderCache.json` on that profile shows whether any non-root folder
   carries `totalMsgs` -1 in real life.
