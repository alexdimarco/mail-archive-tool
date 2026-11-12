# Adversarial pass, round 2: C1a's fix round (a short confirmation)

**Verdict: NOT CLEAN.** 2 LOW are CONFIRMED, each by a red test in the attack clone. No MED or
HIGH. Neither LOW can give a false "complete" in any test here.

- **Round 1's 30 attack tests all pass on 24960a3**: 28 of 28 on macOS, 30 of 30 on the Windows VM.
  The 7 that were red now pass for the right reason (each was read, not only run).
- **R2-01 (LOW, new in part).** The store can now be spelled more than one way: on Windows, long
  and 8.3 short (new in this commit), and anywhere, through a link and through its target (since
  C1a). If Thunderbird has written keys under two of those spellings, every folder becomes a
  duplicate, and stays one for good. This is the same "unknown for good" that C1A-05 fixed for
  case. It fails safe (`unknown`).
- **R2-02 (LOW, the builder's own recorded residual).** A store with no keys of its own takes
  another account's keys when they sit under another folder named `ImapMail/<the same name>`. The
  facts are then wrong, but a false "complete" needs an unlikely mix (see §1).
- **Everything else HELD**: NotCounted over 7,776 field combinations, exact-spelling-wins, the
  tail parent rule and CantTellKeys, the Windows lookup (no key is looked up, no hang added, a
  344-character path works), and Valid on a store that is not IMAP (it fails toward `unknown`).

| Field | Value |
|---|---|
| Reviewed | `git show 24960a3` (internal/tbcomplete/cache.go, spell_windows.go, spell_other.go, the tests, catalog MA-1272 to MA-1278, design §4.1 step 3 and §13 notes). Branch `tb-completeness`, origin tip `24960a3` |
| Date worked | 2026-11-08 |
| Files (sha256) | `cache.go` 1ffb4231…9b7c; `spell_windows.go` 400bf093…fff5 |
| Reviewer | One model instance, acting as both finder and skeptic (the procedure asks for separate ones; that was not done). It did not write C1a or its fixes, and it answers under no name. Every verdict is a self-report backed by a test the lead can re-run |
| Attack clone | `.claude/attack-c1a-r2` in the main checkout, a throwaway clone at `24960a3`, safe to delete. Staged unchanged from `.claude/attack-c1a`: `zz_c1a_attack_test.go`, `zz_c1a_attack_unix_test.go`, `zz_c1a_attack_windows_test.go`. New: `zz_c1a_r2_attack_test.go` (d8f9ef9d…3358), `zz_c1a_r2_attack_windows_test.go` (cc1df059…800b). Windows runner: `.claude/c1a-r2-win.sh`. No product change |
| Thunderbird source | Not re-read this round. The fix round's citations (nsMsgDBFolder.cpp :236, :243; nsImapMailFolder.cpp :353-356, :1107, :1901) were taken as given; round 1 read the same lines |
| Tools | go1.26.0 darwin/arm64; the Windows 11 VM (amd64 build, ARM-emulated). No Linux run. Temp-folder profiles only; no real profile was opened |
| Commands | `go -C .claude/attack-c1a-r2 test -count=1 -v -run AdvC1a ./internal/tbcomplete/`: 28 of 28 pass. `… -run AdvR2 …`: 8 tests, 2 red (R2-01, R2-02). `zsh .claude/c1a-r2-win.sh` (twice): every product test PASS, round 1's 30 PASS, round 2's Windows tests PASS, the same 2 red. `… test -run XXX -fuzz FuzzFolderCache -fuzztime 60s`: 10.6 million runs, no finding |

## 1. Confirmed findings

| ID | Sev | Claim (file:line) | Evidence (test in the clone) | Suggested rule |
|---|---|---|---|---|
| R2-01 | LOW | `storeAt` (`cache.go:470-545`) counts a key as byte-exact if it matches **any** spelling of the store: the path as given, its link target (`:434-436`), or a Windows long or short spelling merged part by part (`:478-491`). Thunderbird writes keys byte for byte from the path it holds and never removes old ones (round 1's C1A-05). So a profile path held one way and later the other leaves two key sets, both byte-exact, and every folder is a duplicate for good (`unknown`). M2 says a moved profile must never be `unknown` for good. The long/short half is new in 24960a3 (before, short keys matched nothing); the link half dates from C1a | `TestAdvR2TwoOfTheStoresOwnSpellingsBothWritten` red on macOS and on the VM, both cases: `duplicates ["" "INBOX"]`, mode Exact | Rank the spellings as C1A-05 ranks case: a key byte-exact in the path **as given** wins; a key exact only in a link target or a long/short variant is then an old-path key (ignored beside an exact key, or OldPathOnly). Only when no key matches the path as given do the other spellings count as exact. Real triggers are rare: Thunderbird started once with `-profile` on a short path, or profiles.ini changed from a link to its target |
| R2-02 | LOW | Tail mode (`cache.go:635-648`) takes the keys of another account whose folder is `<anywhere>/ImapMail/<the store's name>`, when the store has no keys of its own anywhere. The builder recorded this in §13 as a known limit; this pass was asked for its severity | `TestAdvR2TailResidualAnotherImapMail` red: the store reads the other account's `INBOX` and `[Gmail]/All Mail` (`AllMailOne`) | **LOW.** Every real path to it needs a store with no keys at all, and Thunderbird writes a store's keys at its first save after the account exists. So the store is either brand new (nearly empty, so "behind", not "complete") or an orphan with no account (which C2 should not check). A false "complete" also needs this store's local mail to be at least the other account's listed counts, folder by folder. With the store's own old keys present, there are two places, so `CantTellKeys` (HELD in the same test). The fix the builder already wrote down is right: C2 passes the profile's other store paths, and tail mode skips a key that is exact for one of them. It can wait for C2 |

## 2. The six questions

| Q | What was attacked | Verdict | Test |
|---|---|---|---|
| 1 | Round 1's 30 attack tests on 24960a3 | HELD: 28/28 macOS, 30/30 Windows. The 7 formerly red were read: 01 (folderSize -1 Missing, Valid), 03 (depth 300 refused), 14b (UNC root part), 15 (byte-exact wins), 18 (the other store's keys excluded by the parent rule, not by luck), 20 (no serverTotal, not Valid), W02 (short names, run for real, not skipped) | `zz_c1a_attack_*` |
| 2 | NotCounted: every mix of absent, -1, -2, 0, 5 and `"5"` across totalMsgs, serverTotal, expungedBytes, folderSize and pendingMsgs (7,776 entries). Checked: Listed answers exactly when Valid; no Valid entry holds a negative value or lacks a required field; NotCountedYet is true only when totalMsgs -1 is the one failure. Also: -1 named twice (Broken, not "not counted yet"), -1 with spaces round it (NotCounted), a not-counted All Mail, tail mode's root | HELD: 96 Valid, 48 "not counted yet", no entry wrongly either. A not-counted All Mail is still `AllMailOne` with an entry that is not Valid; round 1's note stands (C1b must check `Valid()`) | `TestAdvR2NotCountedEveryCombination`, `TestAdvR2NotCountedEdges` |
| 3 | Exact spelling wins: can an old key win over the current one, or a real duplicate hide | HELD, two notes. (N1) If MailArchive's spelling of the store is the **old** one byte for byte, the old keys win and Thunderbird's current keys are ignored. The counts are then Thunderbird's old ones, with its old `lastSyncTimeInSec`, so the TC-12 "count from DATE" words and the 3-day stale rule apply, as for a moved profile. (N2) One planted byte-exact root key turns every folded key into an old-path key: a folder still in the store becomes "can't tell", one with no files is dropped (counted in IgnoredOldKeys). Only a hand edit does this, and a hand edit can fake any count anyway. A real duplicate is not hidden: two keys for one folder under the same spelling are still Duplicates | `TestAdvR2StaleSpellingWinsWhenItIsMailArchives`, `TestAdvR2PlantedByteExactRootDemotesFoldedKeys` (both record the shape) |
| 4 | CantTellKeys and the ImapMail parent rule: two places, refused keys from a second place, a parent spelled in other case | HELD apart from R2-02. Refused or wrongly shaped keys from a second place do not count as a place, and they land in `Refused` (C1b reads them as `invalid_entry`). A parent in other case is one place on macOS and no parent at all on Linux | `TestAdvR2TailResidualAnotherImapMail`, `TestAdvR2TailParentCase` |
| 5 | Windows short-name lookup. Read: `osSpellings` is called once, on the store's own path (`cache.go:437`); `inStore` builds its checks from the path as given plus a tail already refused for UNC, drive and colon. No key is looked up. Run: a 344-character store (over MAX_PATH); a share that does not exist; `\\.\pipe\…`; `\\?\C:\…`; a folder that does not exist | HELD. The long path is Exact (no CantTellKeys). The missing share took 11 s, and the split test shows the wait is `filepath.EvalSymlinks` (there before this commit); `osSpellings` after it took 0 s. Each odd path answered without a hang and none was Exact. The store path is the archive's source folder that the person chose, which the backup reads anyway, so a lookup on it exposes nothing new | `TestAdvR2W01LongPathStore`, `TestAdvR2W02OddStorePathsNoHang`, `TestAdvR2W03ShareWaitSplit` |
| 6 | Valid() is IMAP-only with no guard | HELD (direction safe). **Nothing enforces it.** `Store` takes no kind, `Valid` does not know the store's kind, and nothing outside `internal/tbcomplete` calls the package yet. Misused on Local Folders, every entry Thunderbird writes there lacks serverTotal, so it is not Valid, not "not counted yet", and gives no count: C1b says `unknown`, never "complete". The only way to Valid is a hand-added serverTotal | `TestAdvR2ValidOnLocalFolders` |

**For C2 (from Q6, not a finding).** Decide "IMAP" by the account's type from `findAccount`, not by
`thunderbird.IsImapStore`, which only checks the path. Add a C2 test where a Local Folders store is
never given a C1 verdict. Either mistake fails safe today, but a guard makes that permanent.

## 3. What this pass did not do

- No Linux run. Linux's rules (NFC only, no case folding) were run as string tests with `goos`
  "linux".
- Thunderbird's source was not re-read; the fix round's citations were taken as given.
- No real Thunderbird and no real profile. R2-01's two triggers (a `-profile` start on a short
  path; a profiles.ini changed from a link to its target) were reasoned about, not reproduced.

**The smallest check that would settle R2-01's likelihood:** on the owner's Windows machine (or the
VM with Thunderbird installed), count how many top-level keys of `folderCache.json` begin with each
spelling of the profile's folder. One spelling means R2-01 is not happening there.

---

Limen footer. Date worked: 2026-11-08 (from the session's clock). Register: a finding (an
adversarial review), with a short report on my own process in the table and §3. Drew on: `git
show 24960a3` (cache.go and spell_windows.go read in full; the new tests read in full); round 1's
review and its attack files; the design's §13 notes and §2.3's TC-12 fold; `thunderbird.go`'s
`IsImapStore` and `FindProfileDir`. Not checked: Thunderbird source lines (taken from the commit),
Linux, any real profile. Hand-back: the key-spelling count above. Counters: Register: finding.
Borrowed lines: none beyond quoted test output. Drift: none noticed. Touched: this file; a
throwaway clone and one runner script under `.claude/`. Persona: none. Source reached:
24960a3 yes; round 1's attack files yes; Thunderbird source no (not needed for the questions
asked); Windows VM yes; real profile no.
