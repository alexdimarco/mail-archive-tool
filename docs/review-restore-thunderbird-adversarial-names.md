# Adversarial pass: Restore to Thunderbird — names Thunderbird never hashes, and the lock words

Date worked: 2026-11-07. Register: a finding (adversarial review), per
`../assurance-kit/process/adversarial-review.md`. One reviewer did both jobs, finder then
skeptic, in one session: the separation the procedure asks for was not available, so every
finding below is held to the skeptic's standard by a probe that ran against the real code,
not by a second reader. Probes ran; fixes were NOT made (that is the builder's next step).

## Header

- Reviewed commit: `2b229ad` on `restore-thunderbird` ("names Thunderbird never hashes; lock words
  by cause; three wording notes"). Branch tip when this report was written: `b3a68a9`; none of the
  reviewed files changed between the two (`git diff --stat 2b229ad HEAD` over `internal/tbrestore`,
  `internal/presync`, `internal/app/syncstreak.go`, `internal/util`, `internal/lockfile`,
  `internal/tbengine` is empty), so no finding is stale.
- File digests (SHA-256 at `2b229ad`): `names.go` 02d75568…, `layout.go` f38e9212…, `tblock.go`
  09df8c70…, `tblock_unix.go` 0cc9e6ca…, `tblock_windows.go` b4645562…, `running.go` 8d990000…,
  `internal/app/syncstreak.go` 8d470091….
- Thunderbird source read for this pass (GitHub mirror `mozilla/releases-comm-central`, the same
  revision the commit read, `9ff8c8b16fba`, comm-central trunk of 2026-11-04):
  `mailnews/base/src/nsMsgUtils.cpp` (`NS_MsgHashIfNecessary`, `MAX_LEN = 55`, the three
  `ILLEGAL_FOLDER_CHARS` defines, `DecodeFilename`, `EncodeFilename`),
  `mailnews/base/src/nsMsgUtils.h` (their doc comments), `mailnews/local/src/nsMsgBrkMBoxStore.cpp`
  (`DiscoverChildFolders`), `mailnews/protocols/common/src/FolderPopulation.cpp`
  (`PopulateFolderHierarchy`), `mailnews/base/src/nsMsgDBFolder.cpp` (the first half: `parseURI`,
  `InitWithFolderId`), `mailnews/local/src/nsMsgLocalStoreUtils.cpp` (`nsShouldIgnoreFile`); and
  `xpcom/io/nsEscape.h` / `nsEscape.cpp` (`NS_UnescapeURL`, `nsUnescapeCount`) at
  mozilla-firefox/firefox `6149f492996`. Read through a summarising fetch tool in short quotes, not
  whole files; the quotes this report leans on are given in full below.
- Tools: go1.26.0 (darwin/arm64); Docker `golang:1.26` with `--tmpfs /tmp:rw,exec,size=8g,mode=1777`
  (as root); Windows 11 VM through `.claude/winrun-at.sh` (amd64 test binary, ARM-emulated).
- Commands and exit statuses (probes in a throwaway clone at `2b229ad`, deleted after):
  - macOS: `go test -count=1 -run TestADVProbe -v ./internal/tbrestore/ ./internal/app/` — exit 1
    (the four confirming probes red, the rest green, as listed below).
  - Linux: the same in Docker — exit 1, the same reds and greens.
  - Windows: `winrun-at.sh <clone> <out> internal/tbrestore` (the whole package, no filter) — FAIL
    on exactly the probes listed below; every existing tbrestore test passed.
- Tally: **7 found / 4 confirmed / 1 not reproduced (lead only) / 2 refuted.**

## Verdict

**Not ready to ship as is: one high finding.** The commit's own fix holds — every name its steps
make passes Thunderbird's hash rule on both platforms' builds, across 800 fuzzed layouts — but
Thunderbird has a second rule the commit did not model: it **percent-decodes** each file name it
finds before it computes the folder's file from that name. A restored folder whose name holds `%`
followed by two hex digits is read from a different file and shows empty, while the result says it
was restored and checked. The defence in depth cannot see it. The fix is small (one character in
the name step and one clause in the check). The other three confirmed findings are low.

## Findings, ranked

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| ADV-N1 | integrity (untrusted names) | `names.go:51-59` keeps `%`; `names.go:193-204` (`ThunderbirdReads`) does not model Thunderbird's decode | CONFIRMED (against a port of Thunderbird's source; not yet seen in a running Thunderbird) | probe red on Mac, Linux, Windows | — | — |
| ADV-S1 | integrity (warning hidden) | `internal/app/syncstreak.go:74-83`: restore-running counts neither, with no bound | CONFIRMED (low) | probe red on Mac, Linux | — | — |
| ADV-L1 | lock words | `tblock_unix.go:85-87,121` vs `running.go:77-94`: a dead-pid `.parentlock` link | CONFIRMED (low) | probe red on Mac, Linux | — | — |
| ADV-L2 | lock words | `tblock_windows.go:166-168`: a non-regular `parent.lock` is held with no reason | CONFIRMED (low) | probe red on Windows | — | — |
| ADV-N5 | integrity (names) | Mac OS Extended (HFS+) Local Folders: names read back decomposed (NFD) may pass 55 | NOT REPRODUCED (lead only) | no probe possible here | — | — |
| ADV-R1 | names | the 55-unit cut, ` (n)` and tag can end a name in `.`, `~` or space, or collide case-blind | REFUTED | fuzz probe green on all three | — | — |
| ADV-R2 | lock words | a held lock can be worded as "not allowed"/"read-only", letting the person think Thunderbird is closed | REFUTED | reading + probe ADV-L3 green on Windows | — | — |

### ADV-N1 (HIGH): names with `%` and two hex digits are read by Thunderbird from another file

**Scenario.** An archive folder named `Budget%2026` (or `a%41b`, `%7Etilde`, `x%2Fy`), or an archive
whose own folder name is `Mail%20backup` (the top name comes from `filepath.Base(out)`,
`tbengine/plan.go:330-333`). The restore writes `Restored — Work mail.sbd/Budget%2026`. Thunderbird
153 finds that file, names the folder `Budget 26`, and reads the file `Budget 26`, which does not
exist: the folder shows empty. With `Mail%20backup` the whole restore hides behind
`Restored — Mail backup`. The result says every message was restored and checked — the same harm as
lab BUG-1.

**Why (Thunderbird's source, quoted).** Discovery, `nsMsgBrkMBoxStore.cpp` `DiscoverChildFolders`
(since bug 1969363, 2025-07-23, so in 153): `children.AppendElement(DecodeFilename(fileName));`.
`DecodeFilename` (`nsMsgUtils.cpp`): `nsCString out = NS_ConvertUTF16toUTF8(filename);` then
`NS_UnescapeURL(out);` (comment: "does generic percent-decoding, not just for URLs").
`NS_UnescapeURL(nsACString&)` is `aStr.SetLength(nsUnescapeCount(aStr.BeginWriting()));`, and
`nsUnescapeCount` decodes every `%` followed by two characters of `"0123456789ABCDEFabcdef"`
(`%00` too) and copies any other `%`. `PopulateFolderHierarchy` then calls
`parent->AddSubfolder(childFolderName, ...)` with the decoded name, and the folder's file is
`NS_MsgCreatePathStringFromFolderURI` → `path += NS_MsgHashIfNecessary(pathPiece);` over that
decoded name. So the file Thunderbird reads is `NS_MsgHashIfNecessary(DecodeFilename(leaf))`, and
the commit's oracle (`ma1095_tbhash_test.go:63`) models only the second step.

A `%` not followed by two hex digits (`50% off`, `Done 100%`) decodes to itself and is read as
written — those are fine today.

**Probe and result.** `TestADVProbePercentNamesAreReadFromAnotherFile` (a Go port of
`nsUnescapeCount` composed with the commit's own `tbHashIfNecessary`): on Mac, Linux and Windows,
`a%41b` → read as `aAb`; `Budget%2026` → `Budget 26`; `%7Etilde` → `~tilde`; `x%2Fy` → `x/y`, then
hashed to `x3dd3a8f7`; top `Restored — Mail%20backup` (and its ` (2)`) → `Restored — Mail backup`.
`ThunderbirdReads` returned true for every one. End to end,
`TestADVProbePercentNameWrittenAndReportedDone`: the real `Write` on a fixture profile returned
`outcome=done restored=1`, the file `…/Restored — Work mail.sbd/a%41b` exists, and the file
Thunderbird reads, `…/aAb`, does not (all three platforms).

**What is not checked.** No running Thunderbird has shown this yet: the lab's names (r8, both runs)
held no `%`. The source read is comm-central trunk; the 153 release branch was not read line by
line. The smallest check that settles it: on the VM's throwaway profile, restore one folder named
`a%41b` and see whether Thunderbird shows it empty (and whether it makes an `aAb.msf`).

**Fix.** In `SafeName` step 2, turn `%` into `_` alongside `;` and `#` (always, not only before two
hex digits: Thunderbird's newer folder store, the `MOZ_PANORAMA` build path in
`nsMsgDBFolder::InitWithFolderId`, names files with `EncodeFilename`, which escapes every `%`, so
only a name with no `%` at all is read as written by both). In `ThunderbirdReads`, refuse any `%`.
Port `DecodeFilename` into the test oracle (`readByThunderbird`) so the oracle is
`hash(decode(name)) == name`. Also count `%` names among the plan's changed names (they already are,
since `SafeName` reports a change). Note for the design text at §4.4: "Thunderbird finds each local
folder's mbox at the name this function returns" should say "at `NS_MsgHashIfNecessary` of the name
it decoded from the file name".

### ADV-S1 (LOW): a restore lock that stays held hides the 3-day "not synced" warning

**Scenario.** `foldSyncStreak` now puts `restore-running` with the "neither" outcomes
(`syncstreak.go:74-83`). Profile-busy has a bound written for exactly this ("so a stuck profile lock
cannot hide the streak", `state/syncstreak.go:36-38`: three in a row count one miss); restore-running
has none. If the restore lock reads as held night after night — a restore wedged on a network Local
Folders, or a `restore-<key>.lock` on a file system where `lockfile.Held`'s `tryLockFor` fails for a
reason other than a holder (`lockfile.go:236-238` returns held on any error) — the missed streak
freezes and `Stale()` never trips.

**Probe and result.** `TestADVProbeRestoreRunningCanHideTheStaleWarning`: two real misses a day
apart, then 30 nightly scheduled restore-running runs → `missed=2`, span 24 h, `stale=false`. The
same 30 nights as profile-busy → `missed=12`, `stale=true` (Mac and Linux; pure logic, not run on
Windows).

**Weight.** Not a regression: before `2b229ad` restore-running was `already-running`, which counts
as synced (`presync/attended.go:61`) and reset the streak every night, which is worse. A
restore-running night between two misses moves nothing, and the span still covers it (checked).
While a restore holds Thunderbird's lock, Thunderbird itself will not open that profile, so the
person is told something is wrong another way.

**Fix.** Count restore-running like profile-busy (its own counter, or the same `Busy` counter):
`SyncBusyRuns` in a row count one miss.

### ADV-L1 (LOW, macOS and Linux): a stale `.parentlock` link says "Thunderbird is open" forever

**Scenario.** Mozilla falls back to a symlink at `.parentlock` when `fcntl` fails for a reason other
than a conflict (a file system with no record locks). If that Thunderbird crashes, the link stays,
naming a dead pid. The plan's check counts it stale (`symlinkLockHeld`, `running.go:89`: a dead pid
is not held) and finds Thunderbird closed. Start's open uses `O_NOFOLLOW` (`tblock_unix.go:85`), gets
`ELOOP`, and `lockHeldErr` (`tblock_unix.go:121`) words it W-RUNNING, "Thunderbird is open". Check
again passes, Start refuses, every time. This is the loop BUG-2 set out to end, in a corner the
commit did not reach.

**Probe and result.** `TestADVProbeStaleParentlockLinkSaysThunderbirdIsOpen` (a real link to the pid
of an exited `true`): plan `running=false`; Start `W-RUNNING` (Mac and Linux, as root on Linux).

**Weight.** Rare (needs the fallback lock and a crash), and nothing is written. Not a regression
(before, every open failure was W-RUNNING). Opening and quitting Thunderbird clears it; the words do
not say so.

**Fix.** Judge a link at `.parentlock` the same way at Start as at the plan: one naming a live pid,
or one that cannot be read, is W-RUNNING; one naming a dead pid on this computer is W-LOCKFAIL with
words that say Thunderbird left an old lock and that opening and quitting Thunderbird once clears it.

### ADV-L2 (LOW, Windows): a folder or link at `parent.lock` says "Thunderbird is open"

**Scenario.** `windowsLockProbe` returns `true, nil` for an entry that is not an ordinary file
(`tblock_windows.go:166-168`), so `CheckRunning` gives no `LockErr` and `RunningRefusal` words it
W-RUNNING at the plan and at Start. On macOS and Linux the same state is W-LOCKFAIL (the commit's own
folder leg in `TestALockFileThatCannotBeMadeIsNotThunderbirdOpen`). Thunderbird never makes a link on
Windows, so this is never Thunderbird's lock.

**Probe and result.** `TestADVProbeWindowsFolderAtLockNameSaysThunderbirdIsOpen` on the VM: plan
W-RUNNING, Start W-RUNNING.

**Fix.** Return `true, fmt.Errorf("%s is not an ordinary file", p)` there, so the words are
W-LOCKFAIL naming it, as on the other platforms.

### ADV-N5 (lead only, not reproduced): a Local Folders on a Mac OS Extended (HFS+) drive

HFS+ stores names decomposed (NFD) and returns them so; the restore counts its 55 units in NFC. A
name of 55 NFC units with accented letters (`é` is 1 unit NFC, 2 NFD) would be longer than 55 if
Thunderbird reads the decomposed name, and then hashed. None of the Thunderbird files read for this
pass normalise names (`nsMsgUtils.cpp` has none), but Mozilla's macOS file layer was not read, so
whether Thunderbird sees NFD here is unknown. APFS, the default Mac disk format, keeps names as
written, so the common case is unaffected. Cheap fix in the safe direction: count the budget as the
longer of the NFC and NFD forms. Smallest check: a 50-character `é` name restored to a Local Folders
on an HFS+ disk image.

### ADV-R1 (REFUTED): cut, ` (n)`, tag, collisions, reserved names

Attacked: astral characters and emoji counted as 2 units, ZWJ sequences and flags, combining marks
(NFC and NFD spellings), controls, bidi marks, `;`/`#` everywhere, trailing `.`, `~`, space after a
cut, leading `.`, device names (`CON`, `com1`, `LPT¹`), alias shapes (`LONGFO~1`), Thunderbird's
skipped endings (`.msf`, `.sbd`, `.toc`) and names, near-collisions (`a#b`, `a_b`, `A;B`), long
repeats, the top name with every ` (n)` to ` (99)`, and nested layouts on the Windows budget under a
deep Local Folders. `TestADVProbeFuzzedNamesAreReadAsWritten`: 400 rounds, 800 layouts, 2,197
folders pushed to LongPath, 6,642 short-cut parts; every written name unchanged by the hash on both
builds, passed `ThunderbirdReads`, was not skipped, not a device, not an alias, valid UTF-8, never
another sibling's `NAME.msf` or `NAME.sbd`, and siblings never collided case-blind after NFC; no
layout was refused W-TBNAME. `a#b`, `a_b`, `A;B`, `a;b `, `A_B`, `a#b.` came out as `a_b`,
`a_b (2)` … `a_b (6)`. Invalid UTF-8 from the archive comes out valid. Why it holds: every cut name
ends in the 8-hex tag or `)`, the trims run before, and `dedupe` checks every candidate against all
earlier siblings.

### ADV-R2 (REFUTED): a held lock worded as a cause, or a cause worded as held

- A lock Thunderbird holds: on macOS and Linux the probe's `F_GETLK` reports it (W-RUNNING, no error);
  on Windows Thunderbird's share-0 open makes ours fail with `ERROR_SHARING_VIOLATION` (W-RUNNING).
  At the take, `EAGAIN` and `EACCES` are both conflicts (POSIX allows either). `EROFS` is only
  read-only. No path was found where a lock that is held reaches W-LOCKPERM or W-LOCKRO, except a
  Thunderbird of **another account** on macOS or Linux (the process check sees this account only):
  then a profile folder this account cannot enter is W-LOCKPERM, whose words do not say Thunderbird
  is closed and which still refuses.
- On a read-only network share where another computer's Thunderbird holds `parent.lock`, Windows
  checks access before sharing, so the answer is "access denied" → W-LOCKPERM ("choose a profile on
  this computer's own drive"): right advice, still refused.
- Probe `TestADVProbeWindowsHeldAndReadOnlyLock` on the VM (green): a second take while held is
  W-RUNNING; a `parent.lock` with the read-only attribute is not W-RUNNING at the plan or at Start.
- Every refusal path writes nothing; no wording says or implies that Thunderbird is closed.

## What held (beyond R1 and R2)

- **Plan-time against write-time check.** Both are `ThunderbirdReadsAll` over the same `Layout` and
  the same `Restored()` set; the plan checks the ` (99)` form, the write the top actually chosen
  (`FreeTopName` stops at 99 with W-NAMES), so the plan's check is the stricter. Nothing between
  them changes the layout's names. No name passes the plan and fails the write, or the other way.
- **W-TBNAME leaks nothing.** `wTBName` names no folder and the refusal wraps no error
  (`refuse("W-TBNAME", wTBName, nil)`, `layout.go:148`); the write's failure uses those words. An
  encrypted archive's folder names do not reach it.
- **Existing Local Folders entries.** `FreeTopName` compares after NFC and case-blind against both
  `NAME` and `NAME.sbd`, so an existing folder is never reused; this stays true once ADV-N1's fix
  removes `%` from the top name.
- **The sync streak's neither.** A restore-running night between misses neither resets nor shrinks
  the measured span; only an unbroken run of them hides the warning (ADV-S1).

## Owed

- The builder: ADV-N1's fix with a red-then-green tombstone (the end-to-end probe above is a ready
  shape), and the same for S1, L1, L2; then a fresh look at the patch (a fix can bring its own
  finding).
- A lab row on the VM for ADV-N1 (`a%41b`, `Budget%2026`, and an archive folder named
  `Mail%20backup`), since the confirmation here is against Thunderbird's source, not a running
  Thunderbird.
- An independent skeptic, if the operator wants the procedure's separation: this pass had one hand.

## Port used by the probes (for the builder's oracle)

```go
// DecodeFilename: NS_UnescapeURL -> nsUnescapeCount. Every "%" followed by two hex
// digits (either case) becomes that byte; anything else is copied.
func tbDecodeFilename(name string) string {
	isHex := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	unhex := func(c byte) byte {
		switch {
		case c >= '0' && c <= '9':
			return c - '0'
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10
		}
		return c - 'A' + 10
	}
	var b []byte
	for i := 0; i < len(name); i++ {
		if name[i] == '%' && i+2 < len(name) && isHex(name[i+1]) && isHex(name[i+2]) {
			b = append(b, unhex(name[i+1])<<4|unhex(name[i+2]))
			i += 2
			continue
		}
		b = append(b, name[i])
	}
	return string(b)
}

// The file Thunderbird 153 reads for a restored leaf name.
func tbFileFor(leaf string, windows bool) string { return tbHashIfNecessary(tbDecodeFilename(leaf), windows) }
```

## Counters

Register: finding. Borrowed lines: the Thunderbird and Mozilla source lines quoted above (each
named by file and revision). Drift: none noticed. Touched: this report only, in the real worktree;
the probes lived in a throwaway clone that was deleted. Persona: none. Source reached, per source:
`nsMsgUtils.cpp`, `nsMsgUtils.h`, `nsMsgBrkMBoxStore.cpp`, `FolderPopulation.cpp`, `nsEscape.h`,
`nsEscape.cpp` reached (via a summarising fetch, short quotes); `nsMsgDBFolder.cpp` reached for its
first half only (`AddSubfolder` and `GetFilePath` not seen); the 153 release branch not reached
(trunk read instead); Mozilla's macOS file layer not reached (ADV-N5).
