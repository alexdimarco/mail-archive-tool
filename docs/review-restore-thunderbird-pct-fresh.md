# Fresh adversarial review: Restore to Thunderbird, commit bc29f75 ("%" names, NFC/NFD lengths, the sync streak, the lock words, the plan's list of changed names)

Date worked: 2026-11-07 (read from the machine's clock). Register: a finding (adversarial review),
per `../assurance-kit/process/adversarial-review.md`. This reviewer did not write or fix the code
under review; it read the code first, then the earlier review
(`docs/review-restore-thunderbird-adversarial-names.md`) and its settled table, then Thunderbird's
source. Probes ran; nothing in the product was changed.

## Header

- Reviewed commit: `bc29f75` on `restore-thunderbird`. Branch tip when this report was written:
  `d299e2e` (docs only after `bc29f75`: `git diff --stat bc29f75 d299e2e -- internal util cmd` is
  empty), so no finding is stale. The branch worktree also held another agent's uncommitted edits
  (quoted names in `internal/tbengine/plan.go`, `internal/desktop/restoretb.go`, and others); they
  were not read for this review and nothing here depends on them.
- File digests (SHA-256 at `bc29f75`): `internal/tbrestore/names.go` 76acba5d…, `layout.go`
  df84bd0f…, `running.go` 81147c3f…, `tblock.go` f7fae9c8…, `internal/util/foldkey.go` 25c8a98d…,
  `internal/tbengine/plan.go` 106c4f4e…, `internal/app/syncstreak.go` 7633977b….
- Thunderbird source read, whole files, fetched raw on 2026-11-07 from the GitHub mirrors' tips
  (`mozilla/releases-comm-central` master, `d1da2f0b` when checked just after; `mozilla-firefox/firefox`
  main, `ac7b5e4b` when checked just after): `mailnews/local/src/nsMsgBrkMBoxStore.cpp`
  (`DiscoverSubFolders`, `DiscoverChildFolders`), `mailnews/protocols/common/src/FolderPopulation.cpp`
  (`PopulateFolderHierarchy`), `mailnews/base/src/nsMsgDBFolder.cpp` (`parseURI`, `GetChildNamed`,
  `GetChildWithURI`, `AddSubfolder`), `mailnews/base/src/nsMsgUtils.cpp` (`NS_MsgHashIfNecessary`,
  `NS_MsgCreatePathStringFromFolderURI`, `CreateFolderAndCache`, `EncodeFilename`, `DecodeFilename`),
  `mailnews/local/src/nsMsgLocalStoreUtils.cpp` (`nsShouldIgnoreFile`),
  `toolkit/profile/nsProfileLock.cpp` (lock names, `IsSymlinkStaleLock`, `Lock`),
  `xpcom/io/nsLocalFileUnix.cpp` and `xpcom/io/nsNativeCharsetUtils.cpp` (leaf names on macOS and
  Linux), `intl/unicharutil/util/nsUnicharUtils.cpp` and `nsUnicodeProperties.h` (case-blind
  compare). The 153 release branch was not read; the running Thunderbird 153.6.0 was the check.
- Tools: go1.26.0 darwin/arm64 (macOS, APFS case-blind); Docker `golang:1.26` (go1.26.9
  linux/arm64, `--user 1000:1000 --tmpfs /tmp:rw,exec,size=8g,mode=1777`, ext4-like tmpfs,
  case-sensitive); the Windows 11 VM through `.claude/winrun-at.sh` (amd64 test binary,
  ARM-emulated, NTFS); Thunderbird 153.6.0 on the VM in a throwaway profile
  `C:\Users\Public\tb-restore-lab6\p-pct` (a copy of `tb-restore-lab2\fresh-news`, no real account).
- Probes: a throwaway clone at `bc29f75`,
  `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/review-pct` (not deleted: deletion is
  blocked here), files `internal/tbrestore/pct_probe_test.go`, `pct_probe2_test.go`,
  `pct_probe3_test.go`, `pct_probe_unix_test.go`, `internal/app/pct_probe_test.go`,
  `internal/tbengine/pct_probe_unix_test.go`. Lab scripts:
  `/Users/alex.dimarco/repositories/.winlab/pctlab/` (`run.ps1`, `tb.ps1`).
- Commands and exits:
  - macOS: `go test -count=1 -run TestPCTProbe -v ./internal/tbrestore/ ./internal/app/ ./internal/tbengine/`
    exit 1 (the reds listed below); `go test -count=1 ./internal/tbrestore/` (the whole package): the
    only failures are the six red probes; every existing test passed.
  - Linux (Docker, uid 1000): the same probes, and the whole `internal/tbrestore`: the same six red
    probes and nothing else.
  - Windows: `winrun-at.sh <clone> .claude/review-pct-win internal/tbrestore` (whole package, no
    filter): FAIL on five probes (the Unix-only lock probe is not built there); every existing test
    passed.
  - Thunderbird 153.6.0 on the VM: `run.ps1` (files written by hand, Thunderbird started and closed),
    then `tb.ps1 start`, one click to open the folder, a screen capture, `tb.ps1 stop` ("exited
    gracefully"). No Thunderbird was left running.
- Tally: **4 found / 2 confirmed (1 medium, 1 low) / 1 refuted by the running Thunderbird after a
  red probe / 1 lead not probed.** Everything else asked in the brief held (see "What held").

## Verdict

**NOT CLEAN: one medium finding, confirmed in a running Thunderbird.** The commit's own changes hold:
`%` is gone from every name, every name is within 55 units in NFC and NFD and is read by Thunderbird
from its own file, the plan's list cannot be made to pass for a plan line and names nothing for an
encrypted archive, the new lock words never let a restore through, and the streak change does not
raise a false warning for someone who restores often. But Thunderbird has one more step between a
file name and the folder it shows that the restore does not model: it drops a second sibling whose
name equals an earlier one **case-blind by its own rule**, and the restore's sibling key
(`util.FoldKey`) keeps some such pairs apart. For Cherokee letters this was seen in Thunderbird
153.6.0: two restored folders, one shown. The gap is older than `bc29f75` (it is in `FoldKey`), but
it is reached through the very names path this commit settles, so it is reported here.

## Findings, ranked

| ID | Lens | Claim (file:line) | Verdict | Evidence |
|---|---|---|---|---|
| PCT-1 | integrity (untrusted names) | `internal/util/foldkey.go:23-28` (`cases.Fold` swaps Cherokee case), used by `layout.go` `dedupe` (`names.go:265-288`) and `FreeTopName` (`layout.go:295-307`) | CONFIRMED, medium | red on Mac, Linux, Windows; Thunderbird 153.6.0 showed 5 of 6 restored-style folders |
| PCT-2 | lock words / refuse in doubt | `running.go:104-121` (`symlinkLockHeld`) and `:76-91` (`staleParentLink`) read only the pid of a lock link, never its computer | CONFIRMED against Mozilla's source in a fixture, low | red on Mac and Linux |
| PCT-R1 | names | siblings differing only by the dotted capital I (U+0130) are one folder to Thunderbird | REFUTED by Thunderbird 153.6.0 (the probe was red; the running program showed both) | screenshot |
| PCT-S1 | streak | a restore lock read as held while the person's own Thunderbird is open now counts as missed | LEAD only (from reading; not probed) | — |

### PCT-1 (MEDIUM): Cherokee siblings that differ only in case: one is hidden, or the restore fails part way

**Scenario.** An archive folder holds two subfolders whose names are the same Cherokee word in
upper and lower case, for example `ᎠᎡ` (U+13A0 U+13A1) and `ꭰꭱ` (U+AB70 U+AB71). The plan passes.

- **Windows (NTFS) and Linux:** the restore writes both files and says "Restored 2 messages … MailArchive
  checked every message it wrote." Thunderbird shows only one of the two folders. The other folder's
  mail is never seen, with no word of it anywhere.
- **macOS (APFS, case-blind):** the second exclusive create meets the first file. The restore fails
  part way ("openat Restored — Work mail.sbd/ꭰꭱ: file exists"), then its cleanup meets its own first
  file under the other spelling, takes it for something it did not make, stops, and leaves the
  restored mail in Local Folders with "Close Thunderbird and delete it yourself". For an encrypted
  archive that is restored mail left unencrypted after a failure, which the plan's words promise
  MailArchive removes. (The words name no folder for an encrypted archive: checked.)

**Why (source, quoted).** `PopulateFolderHierarchy` (`FolderPopulation.cpp`) asks, for each
discovered child, `parent->GetChildNamed(childFolderName, …)` and adds the folder only `if
(!childFolder)`. `nsMsgDBFolder::GetChildNamed` compares with
`folderName.Equals(aName, nsCaseInsensitiveUTF8StringComparator)` ("case-insensitive compare is
probably LCD across OS filesystems"); that comparator lowercases each character
(`intl::UnicodeProperties::ToLower`). Lowercase U+13A0 is U+AB70, so the two names are one to
Thunderbird. The restore's key, `util.FoldKey`, runs `cases.Fold()` from `golang.org/x/text` v0.43.0,
which on this version maps U+13A0 to U+AB70 **and** U+AB70 to U+13A0 (it swaps them), so the two keys
differ and `dedupe` gives no " (2)".

**Probes and results.**
- `TestPCTProbeFoldKeyCoversThunderbirdsCompare` (the whole code space, each character against its
  lowercase, in a name as written and as Mac OS Extended hands it back decomposed): FoldKey keeps apart
  87 pairs Thunderbird's rule calls equal: the 86 Cherokee pairs, and U+0130 (see PCT-R1). Red on all
  three.
- `TestPCTProbeCherokeeCaseSiblings` (the real `Write`): Linux and Windows `outcome=done restored=2`,
  files `ᎠᎡ` and `ꭰꭱ`; macOS `outcome=failed`, the words quoted above, the file `ᎠᎡ` left behind;
  the encrypted twin: the same on macOS, its words naming no folder.
- **Thunderbird 153.6.0 on the VM** (`run.ps1`, then the screenshot): files `İş`, `iş`, `ᎠᎡ`, `ꭰꭱ`,
  `Plain`, `Other` in `PCTlab.sbd`, each holding one message. Thunderbird's folder pane under PCTlab
  showed five folders, `iş`, `İş`, `Other`, `Plain` and `ᎠᎡ`; **`ꭰꭱ` was not shown**, and
  `folderCache.json` has an entry for each of the five and none for `ꭰꭱ`. (The screenshots are kept
  out of the repo, in `.claude/review-pct-run/`, as `tb-01.png` and `tb-02.png`; they show the VM's
  desktop and taskbar.)

**Weight.** Harm: the same as ADV-N1 and lab BUG-1, a folder that says restored and is not shown;
on macOS also a failed restore that leaves files behind. Likelihood: low; it needs two sibling
folders spelt in Cherokee that differ only in case. Not a regression from `bc29f75`. Not checked:
macOS and Linux Thunderbird (the source path is the same); an HFS+ volume.

**Fix (for the builder; one way, tested only as a probe).** Make the sibling key merge at least what
Thunderbird merges: after NFC, map each character to the smallest member of its simple case-fold
orbit (`unicode.SimpleFold`), map U+0130 and U+0131 to `i` and `i` + U+0307 to `i`, then the full
fold, then the orbit minimum again (the full fold swaps Cherokee back). This candidate,
`altKey` in `pct_probe3_test.go`, keeps together every pair the sweep found, as written and
decomposed (`TestPCTProbeCandidateKeyCoversThunderbird`, green on all three). Merging more than
Thunderbird does only costs an unneeded " (2)". `FoldKey` is shared with the export and extract
paths (XC13), so the same swap applies there; whether it matters there was not checked and is owed
to whoever owns those paths.

### PCT-2 (LOW): a lock link made by another computer

**Scenario.** Mozilla's profile lock writes a link whose target is `ADDRESS:PID` (or `ADDRESS:+PID`).
`IsSymlinkStaleLock` (`nsProfileLock.cpp`) says, for a target from another address, "Remote lock:
give up even if stuck": a Thunderbird on this computer will not open the profile. The restore reads
only the pid and asks whether that pid is alive **here**.

- (a) **Linux, the `lock` link** (Mozilla's fallback name there when fcntl locking fails; source:
  `OLD_LOCKFILE_NAME = u"lock"_ns` and `LockWithSymlink(oldLockFile, false)`): a link naming another
  computer's address, with a pid not alive here, and an ordinary `.parentlock` that no one holds
  here. `CheckRunning` gives `Running() = false` and `TakeProfileLock` succeeds: the restore would go
  ahead. Mozilla would refuse.
- (b) **macOS, the `.parentlock` link** (Mozilla's fallback name there): refused, W-LOCKFAIL at the
  plan and at Start (good). But the words say the link was "left behind when Thunderbird last closed
  without quitting normally" and that opening and quitting Thunderbird once clears it, which it will
  not for another computer's lock. The last sentence ("If this profile is also used from another
  computer, Thunderbird may be open there: close it there first") covers it.

**Probe and result.** `TestPCTProbeRemoteLockLinks` (Mac and Linux): (a) `running=false`,
`Start err=<nil>`, red; (b) refused with the words above; positive twin, a `127.0.0.1` link with a
dead pid, stale for both Mozilla and the restore: green.

**Weight.** For (a) to happen on a real network profile, fcntl locking must have failed on the other
computer but answer "no lock" here (mixed mount options or lock support between the two computers).
Rare. Not a regression from `bc29f75`: `symlinkLockHeld` is older; the commit's new `StaleLink`
covers only `.parentlock`. Nothing seen on two real computers.

**Fix.** Read the address part as Mozilla does: a link whose address is not a loopback address and
not one of this computer's is held, whatever its pid, for both names, worded as possibly open on
another computer.

### PCT-R1 (REFUTED by the running program): the dotted capital I

The port of Thunderbird's compare (`mozLower`, from `GetLowerUTF8Codepoint_inline` and
`nsUnicodeProperties.h`) says lowercase U+0130 is `i`, so `İş` and `iş` would be one folder, and the
probes `TestPCTProbeDottedCapitalISiblingsAreOneFolderToThunderbird`,
`TestPCTProbeDottedCapitalIWrittenAndReportedDone` and `TestPCTProbeFreeTopNameAgainstThunderbirdsCompare`
are red on all three. Thunderbird 153.6.0 showed both `İş` and `iş` (screenshot `tb-02.png`, and both
in `folderCache.json`). So the port is wrong for U+0130 in this build; why was not traced. Those three
probes are not findings. They are kept in the clone as a record of what was predicted and what the
program did.

### PCT-S1 (LEAD, not probed): restore-running wins over "already open"

`presync/running.go` `runningOutcome` names a run restore-running whenever the restore lock reads as
held, even if the person's own Thunderbird is also open, which by itself would count as synced. Since
`bc29f75` restore-running counts toward a miss, where before it counted neither. So a restore lock that
reads held at every backup time **while the person keeps Thunderbird open** would now raise "not
synced" after six such nights. A real restore cannot run while Thunderbird is open (it holds
Thunderbird's lock), and the restore lock lives in MailArchive's own settings folder on the local
disk, where `lockfile.Held` says held only when its lock call fails. So this needs a broken lock
probe there. No probe was made.

## What held

- **Thunderbird's whole read path, beyond the hash** (question 1). From the file to the folder:
  `DiscoverChildFolders` skips folders and `nsShouldIgnoreFile` names (which now also asks
  `IsHidden` and `IsSpecial`: a leading dot on macOS and Linux, the hidden or system attribute on
  Windows; the restore sets neither and every name is plain); `DecodeFilename` (no `%` is left);
  `GetChildNamed` (PCT-1 is the one gap); `AddSubfolder`'s special names (`inbox`, `trash` …) apply
  only at the account's root, where the restore's top name always starts "Restored — "; the folder's
  address escapes the name and `NS_MsgCreatePathStringFromFolderURI` unescapes each piece and hashes it,
  which gives the name back for every name the restore writes (no `%`, `;`, `#`, `?`, `/`, `\`, control
  character, leading dot, or trailing dot, space or `~`). Firefox's macOS and Linux file layer returns
  leaf names as stored, with no Unicode normalisation (`nsLocalFile::GetLeafName` →
  `NS_CopyNativeToUnicode`, a plain UTF-8 to UTF-16 copy), so ADV-N5's counting in the longer of NFC and
  NFD is the right direction.
- **`%` → `_` collisions** (question 2). `a%b`, `a_b`, `A%B`, `a;b`, `a#b`, `a%41b`, `a_41b`, `aAb`,
  `%`, `_`, `100%`, `100_`, `a%b (2)`, `a_b (2)` as siblings: every name distinct by `FoldKey` and by
  Thunderbird's compare, each read from its own file, and the plan lists both `a%b → a_b` and
  `a_b → a_b (2)` (`TestPCTProbePercentUnderscoreSiblings`, green on all three).
- **NFC/NFD cuts** (question 2). Siblings that are NFC and NFD spellings of one 60-`é` name, the same
  with a different last letter, 40 Hangul in both forms, `ǅ`/`ǆ` runs, and 300 fuzzed sibling sets
  (accents, combining marks, Hangul, `%`, `;`, `#`, `.`, `~`, space, `ı`, `i`, `I`): every written name
  read as written in NFC and NFD on both platforms' hash, accepted by `ThunderbirdReads`, and no two
  siblings equal by `FoldKey` or by Thunderbird's compare in either form
  (`TestPCTProbeNormalisationCutsNeverCollide`, green on all three). A cost, not a finding: since
  ADV-N5 counts NFD on every platform, a Hangul name longer than 18 syllables is now cut everywhere.
- **The plan's list** (question 3). Folder names with a line break, U+2028 and U+2029, a right-to-left
  override, a quote and an arrow, a tab, a zero-width space and an escape sequence, and an archive
  folder whose own name holds a line break: every NAMES line after the head is exactly one quoted pair,
  every line of the plan's text is one of the plan's own lines, and the DEST line carries no raw break
  or override (`TestPCTProbePlanListCannotBeInjected`, Mac and Linux; Windows cannot make such file
  names). `strconv.Quote` escapes every non-printing character, separators and format characters
  included. An encrypted archive's NAMES line is the count only (the commit's own test,
  `ma1124_nameslist_test.go`, read); its failure words name no folder (PCT-1's encrypted twin);
  `plan.Text()` reaches only the terminal, `-record-out` and Desktop's saved record.
- **The new lock words** (question 4). No path in the new code says "closed" or lets a restore through
  when it did not before: a dead-pid `.parentlock` link and a folder at the lock name refuse at the
  plan and at Start; a Thunderbird process or a seen hold is W-RUNNING whatever else is found. The one
  way through is PCT-2 (a), which is older.
- **The streak** (question 5). Nightly scheduled syncs that work whenever no restore runs: a restore over
  the backup time on 2 nights of every 3 for 90 nights never trips the warning; at random rates of 30,
  60 and 80 per cent over a year, the warning came only after at least six restore-running nights in a
  row; the shortest run that warns is six (two misses three nights apart), and then the warning is true:
  Thunderbird was not synced (`TestPCTProbeFrequentRestorerNoFalseWarning`, Mac and Linux).

## Owed

- The builder: PCT-1's fix with a tombstone (the Cherokee write probe is a ready shape, on macOS too),
  and a look at `FoldKey`'s other callers; PCT-2's fix if the lead wants it.
- A lab row: the same six-file Local Folders on macOS and Linux Thunderbird, and the APFS failure's
  cleanup.
- Why the port of Thunderbird's compare is wrong for U+0130 (PCT-R1), if the builder ports that
  compare into an oracle.

## Settled 2026-11-07 (the builder, under the lead's calls of 2026-11-07)

Each finding was fixed test-first: the tombstone test was written and seen red on the code of
`9ba2ea9`, then the fix made it green, then each fix was mutated back and the test went red again
(the mutations are listed in the fix commit's message). The fix commit is the one that adds this
table. Design notes: `design-restore-thunderbird.md` §4.4 "Settled 2026-11-07 [PCT-1]", §7.5
"Settled 2026-11-07 [PCT-2]" and the quoting note after it.

| ID | Lead's call | What was done | Tombstone test |
|---|---|---|---|
| PCT-1 (medium) | the restore gets its own sibling key, a superset of `util.FoldKey`, matching every pair Thunderbird's compare joins (read and cited); used for every sibling clash and the plan's names; clashing names get the short-code suffix; an exhaustive test over every code point plus Cherokee; on macOS no half-written restore | Done: `tbrestore.siblingKey` (built on `FoldKey`'s output, so a superset by construction and by test), used in `dedupe` (FinalNames, the Windows re-dedupe) and `FreeTopName`. Thunderbird's compare read again for this fix: `GetChildNamed` → `nsCaseInsensitiveUTF8StringComparator` → `CaseInsensitiveCompare` over `GetLowerUTF8Codepoint_inline`, `u_tolower` per code point (comm-central `d1da2f0b`; firefox main and `esr153` `4bd98d39`, ICU 78.3, Unicode 17). One more gap found while fixing: Go 1.26's tables are Unicode 15, so 57 lowercase pairs of Unicode 16 and 17 (Garay, Beria Erfe, eight Latin and Cyrillic letters) were unknown to both keys; they are in `newerLower`. The second of each pair gets " (2)" (then the hash suffix past 99), so every folder shows; the APFS failure cannot happen, since the two files no longer share a name the volume folds. | `internal/tbrestore/ma1088_pct1_siblingkey_test.go` (`TestTheSiblingKeyJoinsWhatThunderbirdJoins`, `TestTheSiblingKeyJoinsEverythingFoldKeyJoins`, `TestCaseSiblingsThunderbirdJoinsAreEachRestored`) |
| PCT-2 (low) | a lock link naming another host is held (refuse) on Linux and macOS, never judged by the local pid; the Mac words must not say opening and quitting Thunderbird once clears it, but that it may be open on another computer that uses this profile, and to close it there | Done, for both link names, at the plan, at Start and in the race; W-RUNNING with its own words. This computer's addresses are loopback, its interface addresses and its host name; anything else, an address that is not a number included, refuses. Applied on every platform (Mozilla makes neither link on Windows, so it can only refuse more). The old table's `host:` rows now use a loopback address (their pid cases kept). | `internal/tbrestore/ma1127_remotelink_unix_test.go` `TestALockLinkFromAnotherComputerIsHeld` |
| PCT-R1 | refuted by the running program | Nothing to fix. The new key joins `İ` and `i` anyway (Thunderbird's source says it should; merging more costs only a " (2)"). Why 153.6.0 showed both is still not traced. | — |
| PCT-S1 (lead) | not in the calls | Not done; still a lead. | — |
| Quoting (the lead's call) | quote the folder name in W-FAT32 and the entries in W-LEFT as the plan list does; encrypted archives name none; remove the unused "too deep" words | Done (`strconv.Quote`); `wLongPath` removed from `internal/tbrestore/words.go` (nothing referenced it; the plan's `longPathWords` is in use). | `internal/tbengine/ma1124_quotednames_test.go` `TestTheFAT32RefusalQuotesTheFolder`; `internal/tbrestore/ma1122_quotedleft_test.go` |
| Follow-up (owed) | record it | **Export and extract use the weaker `util.FoldKey`; check whether it matters.** The same Cherokee swap and the Unicode 16/17 gap reach the export's and extract's sibling names (XC13) and `thunderbird.ProfileKey`'s path fold on macOS and Windows; there the clash is with the file system's own fold (NTFS's upcase table, APFS), not Thunderbird's, so it may not matter. Not checked. | — |

## Counters

Register: finding. Borrowed lines: the Thunderbird and Firefox source lines quoted above, each named
by file (revisions in the header). Drift: the first draft of PCT-1 was about the dotted capital I; the
running Thunderbird refuted that and confirmed the Cherokee case instead, and the report says so.
Touched: this report in the branch worktree; the throwaway clone; `.claude/review-pct-src`,
`.claude/review-pct-run`, `.claude/review-pct-win` in the repo's untracked `.claude` folder; `.winlab/pctlab`;
on the VM, `C:\Users\Public\tb-restore-lab6` only, and Thunderbird's window on the VM desktop for
about two minutes in all. Persona: none. Source reached, per source: every Thunderbird and Firefox file
named in the header reached in full at the mirrors' tips; the 153 release branch not reached (the
running 153.6.0 checked PCT-1 and PCT-R1); macOS and Linux Thunderbird not run; HFS+ not run.
