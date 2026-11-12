# Adversarial pass: Thunderbird completeness, slice C2

**Verdict: NOT CLEAN.** Two medium findings let mail content make an account read "caught up"
while messages are still missing. That breaks R42, the rule that MailArchive never claims complete
when it is not. Nine low findings follow. No product code was changed by this pass.

## Header

- **Reviewed commit:** `18f0b6c` on branch `tb-completeness` (origin tip at the start of the pass).
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer worked as finder and
  then as skeptic. Every finding below was reproduced against the real code by a test in a
  throwaway clone. No finding rests on reading alone, except where marked "by reading".
- **Lenses:** integrity and concurrency (CON), untrusted mail content (MAIL), planted files in
  the archive folder (INS), privacy (PRIV), and wiring (WIRE).
- **Clone:** `.claude/attack-c2` (a git clone of the worktree at `18f0b6c`). The attack tests are
  in four files there, none committed:
  - `internal/app/zz_c2adv_test.go`
  - `internal/state/zz_c2adv_test.go`
  - `internal/source/zz_c2adv_test.go`
  - `internal/tbcomplete/zz_c2adv_test.go`
- **Commands and results** (Go 1.26.0 on darwin/arm64, macOS):
  - `go test -count=1 -run TestADV_C2_ ./internal/tbcomplete/ ./internal/state/ ./internal/source/ ./internal/app/`
    exits 1. Each red test is a confirmed finding below; each green one is held.
  - The builder's guards were rerun at `18f0b6c`, and all pass (exit 0):
    `TestHealthyInputsMatchTheGolden` (the frozen P14 golden, MA-329),
    `TestTheCheckChangesNothingArchived`, `TestAnEncryptedArchiveKeepsNoCompleteness`,
    `TestTheAccountsTypeDecidesNeverItsPath`, `TestAPartialInputIsUnknownAndNeverCompared`,
    `TestTheM6FactsComeFromTheRun` and `TestAFailedRunRecordsNoCompleteness`.
- **File digests (SHA-256) at `18f0b6c`:**
  - `internal/app/tbcompleteness.go`: `218e3603…a17dd`
  - `internal/state/tbstreak.go`: `9e861612…6cd5c`
  - `internal/state/tbcompleteness.go`: `ca6d5373…37790`
  - `internal/source/mailboxcount.go`: `11e0ca44…483699`
  - `internal/tbcomplete/cache.go`: `290a557d…c863`
  - `internal/tbcomplete/walked.go`: `a33b2908…9209d`
  - `internal/thunderbird/storefacts.go`: `9ce3b396…df89b`
- **Tally:** 17 attacked, 11 confirmed (2 MED, 9 LOW), 6 held.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Severity |
|---|---|---|---|---|
| ADV-C2-02 | MAIL | `source/mailboxcount.go:97-99` + `tbcomplete/walked.go:85`: for Gmail, a message with no Message-ID adds 1 to X for each folder it is in | CONFIRMED | MED |
| ADV-C2-03 | MAIL | `source/localwalk.go:487-511` (go-mbox `reader.go:42`): every body line that starts with "From " begins a new counted message | CONFIRMED | MED |
| ADV-C2-16 | WIRE | `app/run.go` runFile `source.IsMboxReader(reader)`: a maildir-format Thunderbird store opened by the Evolution reader gets no verdict at all | CONFIRMED | LOW |
| ADV-C2-12 | MAIL | `source/localwalk.go:387`: every file under a maildir's cur/ and new/ is counted, a `.DS_Store` or an empty file included | CONFIRMED | LOW |
| ADV-C2-14 | MAIL | `source/mailboxcount.go:122-137`: "Message-ID :" (old spelling, space before the colon) is not read, so the message counts as having no ID | CONFIRMED | LOW |
| ADV-C2-01 | CON | `app/tbcompleteness.go:343`, `:151`: the streak and the record name an account by its folder's base name only, so two stores with one name share a streak | CONFIRMED | LOW |
| ADV-C2-05 | INS | `state/tbstreak.go:151` + `app/tbcompleteness.go:337-340`: a streak file other users can read is refused on every run, so the streak stops moving, silently | CONFIRMED | LOW |
| ADV-C2-07 | WIRE | `thunderbird/storefacts.go:127-141`: the account's own old `directory` setting goes into OtherStores and removes its own keys in tail mode | CONFIRMED | LOW |
| ADV-C2-08 | INS | `state/tbcompleteness.go:96-101` vs `state/lastrun.go:640`: the record's own size limits allow 7.4 MB; the reader takes at most 4 MiB | CONFIRMED | LOW |
| ADV-C2-10 | INS | `state/tbstreak.go:150-174`: a version-2 streak file is read as version 1 and saved back as version 1 | CONFIRMED | LOW |
| ADV-C2-11 | PRIV | `state/tbcompleteness.go:107-119`: line and paragraph separators, zero-width characters and the byte-order mark pass through name cleaning | CONFIRMED (outside the design's list) | LOW |
| ADV-C2-04 | INS | the encrypted path's removal of a stray streak file | HELD | — |
| ADV-C2-06 | INS | a planted last-run record changes this run's verdict | HELD | — |
| ADV-C2-09 | INS | planted streak files: link, pipe, folder, huge, garbage, negative numbers | HELD | — |
| ADV-C2-13 | MAIL | huge headers; a sender's own X-Mozilla-Status line | HELD | — |
| ADV-C2-15 | WIRE | a whole profile folder given as the input | HELD (not applicable) | — |
| ADV-C2-17 | WIRE | StoreAmong with other stores spelled oddly, or with this store respelled | HELD | — |

---

### ADV-C2-02 (MED): Gmail messages with no Message-ID count once per folder, so a behind account reads caught up

**What happens.** For Gmail, X is meant to count distinct messages across the account (§3, §2.4
adjustment 2). A message with no Message-ID is counted by `NoID` once for every mailbox that holds a
copy. `OnDiskFor` then adds `mb.NoID` for every kept folder (`walked.go:85`). So one message stored in
both INBOX and All Mail counts 2. Five such messages count 10 and cover five All Mail messages that
were never downloaded. The verdict is `caught_up`, and `MaySayComplete` is true.

The design text says "a message with no Message-ID counts once each". The builder read that as once
per copy. Both the design and the code fail toward "complete" here.

**Test** (`internal/app/zz_c2adv_test.go`). Positive twin first: the same layout with Message-IDs
reads behind, 5 of 10.

```go
_, noID := imapProfile(t, "imap.gmail.com", gm, layout(false)...) // INBOX and All Mail each hold the same 5 ID-less messages; All Mail lists 10
r := onlyResult(t, runTB(t, Options{Inputs: []string{noID}}))
if r.Verdict == tbcomplete.CaughtUp || r.MaySayComplete() { t.Errorf(...) }
```

```
--- FAIL: TestADV_C2_02_GmailNoIDCopiesInflateX
    5 ID-less messages held in two folders, 10 listed: caught_up X 10 Y 10 complete true — a false caught_up (R42)
```

**How common.** Not checked: whether Gmail stores mail with no Message-ID, or adds one on arrival.
ADV-C2-14 widens the gap: an old-style "Message-ID :" header also reads as "no ID".

**Suggested fix (for the builder).** Count a message with no ID at most once across the account. For
example, take NoID from All Mail's mailbox alone when All Mail sets Y. Or key an ID-less message by a
hash of its header block. Either way the error must fail toward "behind".

### ADV-C2-03 (MED): body lines that start with "From " inflate the count, so a behind folder reads caught up

**What happens.** The count is taken for each piece the mbox reader hands back (`localwalk.go`, after
`n++`). The reader, go-mbox v1.0.4, starts a new message at every line beginning "From " that is
not escaped as ">From " (`reader.go:42`). So one stored message whose body has K such lines counts
K+1. That covers K messages missing from the same folder. The count also runs before `parseRaw`, so a
piece that is not a real message still counts.

Anyone who can send the person mail can write such a body. The tolerance is per account and the
shortfall is clamped per folder (`verdict.go:497`), so the extra count covers gaps in that folder
only.

**Test.** Positive twin: the same body with the lines escaped counts 5 of 11 and reads behind.

```go
m := append(tbMsgs("in", 4), tbMsg("crafted", "")+body(""))  // body: 6 lines "From the desk of Bob, note N"
_, raw := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 11, msgs: m})
r := onlyResult(t, runTB(t, Options{Inputs: []string{raw}}))
if r.Verdict == tbcomplete.CaughtUp || r.OnDisk != 5 { t.Errorf(...) }
```

```
--- FAIL: TestADV_C2_03_UnescapedFromLinesInflateX
    5 messages on disk, one with 6 unescaped From lines, 11 listed: caught_up X 11 Y 11 — the count is inflated
```

**Not checked:** whether the Thunderbird versions people run write such lines unescaped in offline
IMAP mbox files. The Thunderbird source copies under `.claude/` do not include the mbox writer. Lab
row MA-1199, or a new lab row, should settle it. Until then, the mechanism is proven in MailArchive's
own code.

**Suggested fix.** For the count only (archiving unchanged, Q7): count a piece as a message only when
it begins at a separator Thunderbird writes. That is a "From " line after a blank line, or a piece
that carries Thunderbird's own `X-Mozilla-Status` as its first header. Or treat any unescaped "From "
line inside a message as "can't tell" for that folder.

### ADV-C2-16 (LOW, would become a false "Export complete." at C3): a Thunderbird store read as Evolution gets no verdict

**What happens.** `runFile` plans the check only when `source.IsMboxReader(reader)` is true.
`source.OpenWith` tries the Evolution reader first. A Thunderbird store in maildir format, whose
server has a folder named `cur` (or `new`) and a folder whose name starts with ".", passes
`isEvolutionMaildirStore`. The store is then walked as Evolution, and no `tbStore` is made. The
account has no result at all. `tbcomplete.RunLead` of an empty list is `LeadComplete`.

**Test.** Positive twin: the same maildir store without those two folders reads behind.

```
--- FAIL: TestADV_C2_16_MaildirStoreReadAsEvolutionGetsNoVerdict
    a behind Thunderbird store with folders named cur and .x got no verdict; the run's lead would be complete (LeadComplete=complete)
```

**Severity.** LOW, because it needs the non-default maildir storage and server folders with those
names. The design's mbox default is not affected: there, `cur` is a file and its subfolders sit in
`cur.sbd`. It fails toward "complete" once C3 shows the lead.

**Suggested fix.** Plan the check from the input path and its account, as `planTB` already does,
whatever reader opened the store. A matched IMAP store whose reader cannot count should be `unknown`,
never missing from the list.

### ADV-C2-12 (LOW): every file in a maildir's cur/ and new/ counts as a message

A maildir folder holding 3 messages, a Finder `.DS_Store` and an empty file counts 5. Each extra file
covers one missing message. These files are archived too, as before. The count adds no check of its
own.

```
--- FAIL: TestADV_C2_12_MaildirJunkFilesCount
    a maildir folder with 3 messages, a .DS_Store and an empty file counts 5 (archived 5)
```

**Suggested fix:** leave out of the count names that start with "." and files with no header block.

### ADV-C2-14 (LOW): an old-style "Message-ID :" header is not read

RFC 5322's obsolete syntax allows white space before the colon. `headerValue` matches only
`message-id:`, so two copies of one such message count as 2 messages with no ID. That feeds
ADV-C2-02.

```
--- FAIL: TestADV_C2_14_ObsoleteFieldSpellingIsNoID
    an obsolete-spelled Message-ID is not read: 0 keys, 1 + 1 with none
```

### ADV-C2-01 (LOW): two stores with the same folder name share one streak

`updateTBStreak` and `tbRecords` name an account by `CleanFolderName(filepath.Base(store))`. Two
stores in one run can share that name: two Thunderbird profiles, each with
`ImapMail/imap.same.example`, or two accounts whose `directory` settings end in the same name. Then
one store's result moves the other's streak:

- **Behind, then caught up:** the streak is started and then ended in the same run, every run.
- **Caught up, then behind:** the streak is ended and restarted every run, the serial goes up each
  time, and the run count never passes 1.

Either way the "still downloading" notice (C5) can never fire for the behind account. The record also
holds two entries with the same name, and TC-19's carried-forward fingerprints mix them up. The
positive twin, with distinct names, reaches 2 runs.

```
--- FAIL: TestADV_C2_01_StreakKeyCollidesAcrossProfiles
    behind then caught-up stores of one name: the behind streak is {…Runs:0} (present false), want 2 runs
    caught-up then behind stores of one name: the behind streak is {…Serial:2 … Runs:1} (present true, serial 2), want 2 runs
```

**Suggested fix:** key the streak and the carry-forward by a fingerprint of the store's full path,
and keep the base name for the words. Alternatively, mark both colliding stores `unknown` for the
streak.

### ADV-C2-05 (LOW): a streak file others can read freezes the streak, silently

`ReadTBStreak` uses `util.ReadSecureFile`, which refuses a file with group or other permission bits.
`updateTBStreak` then returns without writing ("left as it is, never reset"), so the file is never
rewritten at 0600. The streak stays where it was for good, and nothing is logged. In the test, three
behind runs over four days left the streak at 1 run, and "still downloading" stayed false.

This can happen without an attacker. An archive copied with loosened permissions does it. So may a
drive whose file system shows every file as readable by all, such as exFAT on macOS (not checked
here).

The presync streak does the same (`app/syncstreak.go:53-56`). This is not a new kind of fault, but
C2 adds a second copy of it.

```
--- FAIL: TestADV_C2_05_LooseModeFreezesTheStreak
    a 0644 streak record: after 3 behind runs over 4 days the streak holds 1 runs (still downloading: false)
```

**Suggested fix:** for this file, a non-secret record, read through `util.ReadRegularFile` (no
permission check) as the last-run record is read, or log the refusal once. Either way it fails
toward no notice, never toward a false one.

### ADV-C2-07 (LOW): the account's own old `directory` setting removes its own keys in tail mode

`ReadStoreFacts` adds every live account's `directory-rel` and `directory` to `OtherStores`. It skips
only paths equal to the store, and it does not skip the matched account itself. After a profile
moves, Thunderbird's `directory` setting can still name the old place. Tail mode exists for exactly
this case: the keys in `folderCache.json` still name the old place. The account's own old path is now
"another store", so `StoreAmong` drops all of its keys, and the verdict is `unknown` /
`cache_unreadable`. The positive twin, the same profile without the old `directory`, reads caught up
in tail mode.

```
--- FAIL: TestADV_C2_07_OwnStaleDirectoryDropsOwnKeys
    the account's own stale directory setting made its keys another store's: unknown cache_unreadable
```

This fails toward "unknown" and clears once Thunderbird has run and rewritten its keys. But it
disables tail mode in its main use.

**Suggested fix:** leave the matched account's own settings out of `OtherStores` (`key ==
acct.ServerKey`).

### ADV-C2-08 (LOW): the record can grow past the size its own reader accepts

The record's limits allow 64 accounts, 200 kept folders, 3 lists of 10 not-kept folders, and names
of 80 characters. JSON writes `<` as `<`, 6 bytes each. Within those limits the `thunderbird`
field alone reaches 7,427,971 bytes. `ReadLastRun` refuses anything over 4 MiB, so the whole last-run
record would read back as unreadable in `status`. Realistically it needs dozens of IMAP accounts with
hostile folder names. LOW.

```
--- FAIL: TestADV_C2_08_RecordBoundsFitTheReader
    the record's own bounds allow 7427971 bytes of `thunderbird`, over ReadLastRun's 4194304
```

**Suggested fix:** cap the total (for example, folders across all accounts), or check the encoded
size before writing and drop folder detail first.

### ADV-C2-10 (LOW): a later-format streak file is read as this format

A file with `"version":2` is read field by field as version 1. The next behind run saves it back as
version 1 and drops whatever version 2 added. The presync streak does the same. This only matters
when a newer MailArchive and an older one share an archive.

```
--- FAIL: TestADV_C2_10_LaterVersionIsNotTakenAsThis
    a version-2 record is read as this format: {Account:imap.x.example … Runs:40 …} (version 2); the next write saves it as version 1
```

### ADV-C2-11 (LOW, design gap): characters the name cleaning lets through

`CleanFolderName` does exactly what the TC-7 fold lists: C0 and C1 controls, DEL, the listed bidi
controls, and the 80-character cap all hold (tested). Invalid UTF-8 becomes U+FFFD. A planted record
is cleaned again when it is read back.

Characters outside the list pass through unchanged: U+2028 and U+2029 (line and paragraph
separators), U+200B and U+200D (zero-width), and U+FEFF (the byte-order mark). Some displays treat
U+2028 as a line break. The test logs these and does not fail on them. It is recorded here for C3's
surfaces and for MA-1198.

---

## Held (attacked, not reproduced)

- **ADV-C2-04: the encrypted path removes only that one file.** `RemoveTBStreak` is `os.Remove` of
  `<out>/.mailarchive-tbstreak.json` and nothing else. The test checks four cases:
  - a link of that name is removed as a link, and the file it points to is kept;
  - a non-empty folder of that name is kept;
  - a file of that name in the parent folder is kept;
  - the exact regular file is removed (the positive twin).

  One thing does go: an empty folder of exactly that name. That is harmless.
- **ADV-C2-06: a planted record never changes the verdict.** A planted last-run record saying
  `caught_up`, `complete: true` and 40 of 40 left the run's verdict at behind, 3 of 40, and the new
  record says behind. The only thing carried from the old record is TC-19's first-seen time.

  A planted `limit_hash` (it is a hash of known values, so anyone can compute it) can turn the one
  "chosen setting" WARN into a note. That needs write access to the archive folder, which can already
  rewrite every record. Noted for C3.
- **ADV-C2-09: planted streak files.** Results:
  - a link is refused and never followed;
  - a pipe (FIFO) is refused at once, with no hang (5-second guard);
  - a folder of that name is refused;
  - a file over 64 KiB is refused;
  - garbage reads as an empty streak;
  - negative counts are clamped to 0;
  - a malformed `record_id` reads as none;
  - names are cleaned;
  - writes are 0600 and atomic (temp file plus rename, with the swept `.mailarchive-*.tmp` pattern).

  **For C5:** a planted streak with many runs, or with `still_downloading_sent: true`, can trigger or
  suppress one email. Again this needs write access to the archive folder.
- **ADV-C2-13: header shapes stay bounded.** Each lookup reads at most 64 KiB (`headerBlock` and
  `headerSection`). A 4 MiB header with no blank line, plus 2,000 messages with 60 KiB headers, counts
  in 0.3 s. The Message-ID that sits past the bound counts as "no ID", which errs toward "behind"
  except under ADV-C2-02.

  A sender's own `X-Mozilla-Status: 0008` placed after Thunderbird's line is ignored, because the
  first line wins. One with no Thunderbird line before it only lowers X, toward behind.

  Memory: 16 bytes per message for the keys, plus a map for the distinct set. About 64 MB for 1
  million messages (estimated by arithmetic, not measured).
- **ADV-C2-15: a profile folder as the input.** The run refuses it ("no input mail sources found").
  Store discovery hands over each `ImapMail/*` store whole. Nothing to attack.
- **ADV-C2-17: StoreAmong with odd spellings.** Through the public `StoreAmong`, other stores spelled
  with a trailing "/", a "..", a "." or a doubled "/" still own their keys, because `filepath.Abs`
  cleans them. This store respelled as an "other store" never loses its exact keys: exact mode is
  unchanged.

  (The internal `storeAmong` does not clean its inputs, but only `StoreAmong` reaches it, and that
  cleans first.)
- **Wiring, by tests and reading:**
  - The frozen P14 golden and the on/off byte-equality test pass, so the archive output is unchanged.
  - Local Folders and POP3 are never counted and never reach the cache (`TestTheAccountsTypeDecidesNeverItsPath`).
  - A partial input is `unknown`.
  - A failed run writes no completeness (`TestAFailedRunRecordsNoCompleteness`).
  - `recordRun` is deferred after the lock (`run.go:655`, `:759`), so the streak and record writes
    happen under the archive lock. Concurrent runs cannot interleave.
  - The Result's `Thunderbird` field is set by a later defer, so it runs first and `recordRun` sees it.
  - `RunLead` returns "so far" whenever any account is behind or limited, whatever the order, so an
    unknown account never hides a behind one.
- **Privacy, plaintext:**
  - The record holds the store folder name (the server host), counts, kept folder names (Gmail labels
    included, as §3's "X and Y per folder" allows), not-kept folder names, and hashes.
  - The history holds account, verdict, X and Y. The streak holds only the account name.
  - No addresses, subjects or message content are written. Message-ID hashes stay in memory only.
  - The new dotfile is covered by the delete flow's `.mailarchive-` prefix (`archdelete/names.go:53`).
  - **Owed by C5** (already noted by the builder): the README line for the new file.
- **Privacy, encrypted (O1):** `TestAnEncryptedArchiveKeepsNoCompleteness` passes: no new key, no
  count, no server or folder name, no "Thunderbird", and no streak file. `previousThunderbird`,
  `tbRecords` and `updateTBStreak` each check for a key or an encrypted archive before they read or
  write anything.

## What the lead should decide

1. **ADV-C2-02 and ADV-C2-03 (MED)** break R42 by mechanism. Neither is a gate to weaken. Each wants
   a fix with its own test shown failing before the fix and passing after. Use the failing tests
   above as the starting point.
2. **ADV-C2-16** should be fixed before C3 gives the lead words, or C3 must treat "a Thunderbird
   store with no result" as `unknown`.
3. **The LOW items** can fold into C2's follow-up or into C3/C5 as noted. ADV-C2-05 and ADV-C2-10
   share their cause with the presync streak, which is out of C2's scope.

## Rerun after fixes

A fix to `mailboxcount.go`, `localwalk.go`, `walked.go`, `storefacts.go` or `tbstreak.go` makes the
matching lens stale. Rerun the four `zz_c2adv_test.go` files from `.claude/attack-c2` against the
fixed commit.
