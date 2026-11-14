# Adversarial pass, round 5: Gmail de-dup slice B3's round-4 fixes

**Verdict: NOT CLEAN (7 LOW, no MED or higher).** J1 to J5 from round 4 are closed for what they
named: every round-4 probe that was red is green or now shows only the residual the fix states
(J2's probe sees one sealed unit, the message in flight, and B's record "ok"), on macOS and on the
Windows VM. The tail proofs, the capture's post-load check and the encrypted run's lock binding
hold where they were put. Seven LOW findings, four of them in the guard as it would run on any
archive (so they matter for the 1.7.2 back-port too):

- **K1 (LOW).** The walk's 10-second bound lets a re-export **delete** the other archive's files,
  not only add orphans. A message re-exported under a new name removes its previous file by path
  (`exporter.go:789-803`). After a swap that removal lands in the other archive: 5 of B's 6 listed
  message pages were deleted, B's check is "NOT attested", and B's own next backup does not put
  them back (a Full backup of B does). Proved on macOS.
- **K2 (LOW).** Folder pages between proofs are proved on time only, and page writing usually takes
  less than 10 seconds, so in practice nothing is proved between the first folder page and the root
  page. A swap during the pages rewrote all 80 of B's other folder pages with A's (root, README,
  report, record and manifest were protected), and B's next backup with nothing new kept all 80.
  Proved on macOS, 3 of 3.
- **K3 (LOW, a change this round made).** An encrypted run whose **own** lock file is removed
  mid-run (no swap, no other run) now records nothing: its record stays "running", `status` says
  "never finished … crash, kill, or power loss", and the run history has no entry. At `1b84d10` the
  same run recorded "failed, lock_lost". Proved on macOS.
- **K4 (LOW).** An encrypted run has no post-load check (its manifest loads before the pre-sync), so
  a swap during the pre-sync wait (round 2's G1 scenario) is caught only after the first message is
  sealed into the other archive: one unit lands there. A plaintext run writes nothing in the same
  case. Proved on macOS (at `1b84d10`, the same probe put 45 files, the record, the run history and
  the attention notice into B).
- **K5 (LOW, in the guard since round 2).** A healthy archive is called "another archive" when its
  last-run record cannot be read **and** cannot be replaced at the instant the run begins (another
  program holding it open with no sharing, on Windows): `beginRun` gives a new id it cannot write,
  and the post-load check reads the old one. The run stops, records nothing, and `status` still
  shows the previous run. Proved on Windows (exclusive open) and macOS (modes).
- **K6 (LOW).** J4's fix has one ending left: after a real failure whose email could not be sent,
  if only tidy-up waits follow (the other computer never finishes), no email is ever sent, not even
  the wait's 3-day alert, and the success after it sends none either. Proved on macOS and Windows.
- **K7 (LOW).** J5's checks still accept three shapes where the tests run before the guard, and
  `ComputesUserFolder` misses a per-user lookup taken as a value, a constant from another file,
  `os.Environ` and `unix.Getenv`. No package is missed today.

No false "lock lost" was found on a healthy archive from the new per-message proof (the macOS and
Windows local disks and loopback SMB on Windows: 4,000 proofs and 4 healthy runs of 400 messages per
place, no failure); its cost is about 0.6 ms per written message over loopback SMB. K5 is the one
false failure of a healthy archive this round found.

No product code was changed by this pass. The probes stay out of the branch (build tag `b3adv`,
uncommitted, kept in the worktree's `.claude/testdata/`, which the covers scan skips).

## Header

- **Reviewed commit:** `981123c` on `gmail-dedup`; this round's diff `1b84d10..981123c` (MA-1336 to
  MA-1340; `c9facf8..981123c` is docs only). Round 4: `docs/review-gmail-dedup-b3-adversarial-r4.md`.
  Fix notes: design §13 "Folded 2026-11-14 [B3 adversarial round 4 fixes]".
- **Blob ids at `981123c`:** `internal/app/archiveguard.go` 135a97a, `internal/app/run.go` 9c70416,
  `internal/app/graph.go` b12ec88, `internal/pages/pages.go` 5fdf7b9,
  `internal/backupalert/streak.go` db0e822, `internal/testhome/reach/reach.go` 4bdc27e,
  `internal/export/exporter.go` 6de7887, `internal/lockfile/lockfile.go` 697d12e,
  `internal/state/archiveid.go` b0723e1, `internal/health/health.go` fb388e1,
  `internal/index/pagequery.go` da9f6f0. A finding is stale when its cited file changes.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who wrote neither the
  slice nor its fixes, worked the integrity (time and concurrency), insider and outside lenses as
  finder, then as skeptic. Every CONFIRMED row has a probe against the real code; every HELD row
  cites the probe or the code that refutes it. The guard was attacked as it would run on any
  archive (the 1.7.2 back-port takes its lock-only form); each finding says whether it applies there.
- **Worktree:** `.claude/worktrees/gmail-dedup-b3-adv5`, branch `wip/gmail-dedup-b3-adv5` at
  `981123c`. A detached checkout of `1b84d10` (`.claude/base-1b84d10`, removed before the unfiltered
  run) ran the same round-5 probes as a baseline. Probe files (build tag `b3adv`, not committed):
  rounds 1 to 4's thirteen, copied unchanged from `gmail-dedup-b3-adv4/.claude/testdata/probes/` into
  `.claude/testdata/probes-r4/`, and five new in `.claude/testdata/probes-r5/`:
  `internal/app/zz_b3adv_r5_test.go` (runs on both commits), `internal/app/zz_b3adv_r5_tail_test.go`
  (uses round 5's `tailForTest`), `internal/app/zz_b3adv_r5_windows_test.go`,
  `internal/backupalert/zz_b3adv_r5_streak_test.go`, `internal/testhome/reach/zz_b3adv_r5_reach_test.go`.
  Tests `TestB3Adv*` to `TestB3AdvR5*`. They were copied into the package folders for the runs and
  taken out before the unfiltered run.
- **Real primitives only.** Real archives made by real backup runs of the package's
  Thunderbird-shaped profiles and its PST fixture, real encrypted archives (`vault.PrepareCreate`),
  the real lock, last-run record, run history, `health.Gather`/`Assess`, alert streak (sender
  replaced by the package's recorder), and the real `Verify`. Seams: the package's own
  `archiveRecheckEvery`, `BeforeRead` (the pre-sync hook), the progress callback and round 5's
  `tailForTest`; swaps are real folder renames (macOS); K5's hold is a real `CreateFile` with no
  sharing (Windows) or real modes (macOS). Throw-away `HOME`, `XDG_CONFIG_HOME`, `APPDATA` per test.
  No real email, no browser, no Desktop link, no dialog; nothing outside `repositories/` touched; the
  real settings folder never read.
- **Windows:** the Parallels VM "Windows 11", nobody signed in, so the probes ran as SYSTEM through
  this round's own copy of `winrun-at.sh` (`.claude/winrun-b3adv5.sh`: its own `src-b3adv5`,
  `src-b3adv5base`, `wintest-b3adv5`, `wintest-b3adv5base` lab folders, its own `C:\matest-b3adv5`
  and `C:\matest-b3adv5base` program folders and `r5probe*` program names, the share reached as
  `\\Mac\winlab`, `GOTOOLCHAIN=local` with Go 1.26.11 on the VM, test programs cross-built on the
  Mac). SYSTEM cannot write the lab share, so the network form used the VM's own admin share by
  loopback (`\\127.0.0.1\C$\matest-b3adv5\smb`, SMB's semantics at a local disk's latency). The VM
  was not restarted, suspended or signed into; no account was made. `subst` with no arguments on the
  VM afterwards lists no mapping. The other builder's folders and processes were not touched.
- **Commands and results** (Go 1.26.0 darwin/arm64, the repo's pinned toolchain; Windows programs
  cross-built for windows/amd64 with `GOTOOLCHAIN=local`, which on this Mac is go1.27.2, as the
  builder's runner does; 2026-11-14):
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/app/ ./internal/backupalert/ ./internal/testhome/reach/ ./internal/archdelete/ ./internal/gmailanswer/ ./internal/pages/ ./cmd/mailarchive/ ./internal/wizard/ ./internal/desktop/`
    (macOS, rounds 1 to 4): every probe passes but `TestB3AdvR4EncryptedSwapMidWalk`, whose check is
    "nothing at all": it now sees one sealed unit (the message in flight), B's record "ok", B's
    keyless check intact, B's own next backup clean. J4's and J5's round-4 probes now pass.
  - `go test -tags b3adv -run B3AdvR5 -count=1 -v ./internal/app/ ./internal/backupalert/ ./internal/testhome/reach/`
    (macOS, `981123c`): red `TestB3AdvR5SwapMidWalkRemovesTheOthersFiles` (K1),
    `TestB3AdvR5SwapDuringTheFolderPages` (K2, 3 of 3 with `-count=3`),
    `TestB3AdvR5OwnLockRemovedMidWalk/encrypted` (K3), `TestB3AdvR5EncryptedSwapDuringPresync` (K4),
    `TestB3AdvR5RecordHeldAtBeginGivesAFalseAnotherArchive` (K5),
    `TestB3AdvR5OwedEmailNeverGoesWhenOnlyWaitsFollow` (K6); `TestB3AdvR5ReachEdges` and
    `TestB3AdvR5ReachMissedToday` log K7's rows and find no package missed; the cost and healthy-run
    probes pass.
  - The same `internal/app` probes at `1b84d10` (macOS): K3's encrypted form passes (recorded
    "failed", kind `lock_lost`, history 1 to 2); K4's probe puts 45 files, the record, the run history
    and `BACKUP-NEEDS-ATTENTION.txt` into B; K5 red as at `981123c` (the post-load id check is round
    2's); the healthy runs take the same time (encrypted 400 messages 8.8 s at both).
  - `zsh .claude/winrun-b3adv5.sh … b3adv5 … B3Adv internal/app internal/backupalert internal/testhome/reach internal/pages internal/desktop internal/wizard cmd/mailarchive internal/gmailanswer internal/archdelete`
    (Windows, as SYSTEM): reach, pages, wizard, cmd/mailarchive, gmailanswer, archdelete pass;
    desktop passes (first attempt ended in a `prlctl` "PrlJob_GetResult: Invalid argument" with no
    test output; rerun alone: every desktop probe passes); app: the drive-letter probes of rounds 2
    to 4 pass (`TestB3AdvR4DriveLetterSwapAfterTheLastProof` included), the two round-2 rename probes
    stop at "Access is denied" as in rounds 2 to 4, red `TestB3AdvR5RecordHeldExclusivelyAtBegin`
    (K5), the cost and healthy-run probes pass on the local disk and over SMB; backupalert: red K6's
    probe, J4's round-4 probe passes.
  - The cost and healthy-run probes at `1b84d10` on Windows (`… b3adv5base …`) for comparison, and
    K5's Windows probe: red there too.
  - Unfiltered, probes out: `go test -count=1 -p 4 -timeout 60m ./...` (macOS): see "Unfiltered run"
    below.
- **Count:** the five round-4 findings re-run (all closed for what they named), and 24 new
  attempts: 7 confirmed (all LOW), 17 held, refuted or not pursued.

## Round-4 findings: verdicts

| ID | Round 4 | Round 5 verdict | Evidence |
|---|---|---|---|
| J1 | LOW, CONFIRMED | **CLOSED** for what it named (the root `index.html`, README and report, and the cancel paths); K2 is the folder pages between proofs | `TestB3AdvR4SwapAfterTheLastProofWritesIntoAnother` (macOS) and `TestB3AdvR4DriveLetterSwapAfterTheLastProof` (Windows, `subst`) pass: the swap at the folder-cache read is caught by `writePages`' first proof (`archiveguard.go:147-149`), nothing of A's reaches B. Proofs sit before the first page, before the root page (`:151-155`), before the README and before the report (`run.go:376-381`, `:389`, `:417`; a capture whose mailboxes all failed still proves before the report). Cancel paths: `run.go:1097-1107`, `:1116-1122`, `graph.go:571-581`. The capture's tail: `graph.go:633-671`. A page that fails to write for its own reason stays a warning (`archiveguard.go:157-163`) |
| J2 | LOW, CONFIRMED | **CLOSED** for what it named (no record, run history entry, attention notice or sealed manifest written into the other archive; at most the in-flight message's unit); K3 and K4 are what the lock-only binding leaves | `TestB3AdvR4EncryptedSwapMidWalk` (macOS): 1 unit added to B (was 39 to 41 files and the record), B's record "ok", B's keyless check intact, B's own next backup clean. "More than one message": no. The local walk checks its stop before every item (`localrun.go:410-412`, `:485-494`), the capture before every message (`graph.go:888-894`), and a unit is written per message with no buffering (`export/units.go:111-160`); the builder's test bounds it to one with the on-time proof due and not due. A lock taken by another run of the same encrypted archive: the first run stops at its next message and records nothing, the run holding the lock records its own (the fold's stated change). On Windows the lock cannot be removed or overwritten while held (`lockfile.go:137-149`; a refused acquire never writes) |
| J3 | LOW, PLAUSIBLE | **CLOSED** | `graph.go:261-269` (post-load check, before the tidy-up wait ends, the history header and the index) and `:437-439` (proof before the collapse save); the builder's `TestACaptureWhoseFolderIsSwappedOutsideItsWalkWritesNothingIntoTheOther` covers both points. Residual not pursued: L10 |
| J4 | LOW, CONFIRMED | **CLOSED** for what it named; K6 is the ending where only waits follow | `TestB3AdvR4UndeliveredRealFailureIsToldByTheWaitsEmail` passes on macOS and Windows: the wait sends nothing, the next real failure sends the real failure's email (`streak.go:158-164`) |
| J5 | LOW, CONFIRMED | **CLOSED** for what it named; K7 is what the checks still accept | `TestB3AdvR4ReachChecksEdges`: `CallsGuard` false for dead code, an uncalled closure and `Check` after `m.Run`; `ComputesUserFolder` true for a named constant, `ExpandEnv`, `os/user`, `KnownFolderPath`, `syscall.Getenv` |

## New findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Applies to the 1.7.2 back-port | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|---|
| K1 | integrity (time) | A plaintext walk proves on time only (`archiveguard.go:134-140`, `run.go:1058-1060`), and a message re-exported under a new name removes its previous page, zip and kept `.eml` by path (`exporter.go:789-803`; a re-capture with nothing to zip removes the zip, `:632`). After a swap those removals land in the other archive, within the 10-second bound round 3 accepted for "files it wrote" | CONFIRMED, LOW (macOS) | `TestB3AdvR5SwapMidWalkRemovesTheOthersFiles`: A and B, two drives of one mailbox, both named by an earlier rule; A's Full run renames; the swap after its first message. A's run ends "now holds another archive"; B: `missing=5` (its own listed pages), `unexpected=5` (A's renamed copies), "NOT attested". B's own next incremental backup: still `missing=5`, still not attested; B's own Full backup after it: attested | yes (the plaintext walk is on time in either form) | | |
| K2 | integrity (time) | `writePages` proves before the first page, then only when `due()` (10 s) and before the root page (`archiveguard.go:146-156`). Page writing that takes less than 10 s has no proof between its first folder page and the root page; the pages are written last path first (`index/pagequery.go:26-27`) | CONFIRMED, LOW (macOS, 3 of 3) | `TestB3AdvR5SwapDuringTheFolderPages` (default interval): swap once A's first page (INBOX) is written: all 80 of B's other folder pages rewritten by A's run; root `index.html`, README, report, record and manifest untouched; A's run ends "now holds another archive". B's own next backup with nothing new keeps all 80 (`pagesNeeded`, `run.go:1213`) | yes, if it takes `writePages` as is | | |
| K3 | integrity (state) | An encrypted run's "another archive" is its lock alone (`archiveguard.go:47-49`, `:69-71`), and the deferred record writes nothing when it says so (`run.go:834-842`, `graph.go:232-240`). A lock removed from the run's **own** archive (no swap, no other run) now leaves the "running" record unfinalized (R18: "finalizes it … on the way out") | CONFIRMED, LOW (macOS); a change from `1b84d10` | `TestB3AdvR5OwnLockRemovedMidWalk/encrypted`: record `running`, history 1 to 1, `status` `[run_never_finished encrypted]` ("never finished (pid … is gone: crash, kill, or power loss)", `health.go:775`). At `1b84d10`: `failed`, kind `lock_lost`, history 1 to 2, `[run_failed encrypted]`. The plaintext twin records `failed` at both. The run still returns its error (the alert and the CLI see it) | yes, for every archive, if its deferred record skips on a lost lock as the encrypted form here does (a lock-only form cannot tell this from a swap) | | |
| K4 | integrity (time) | An encrypted run's manifest loads before its pre-sync (`run.go:742-748`), so the post-load check (`run.go:897-914`, `manifest == nil` only) never runs for it; after the pre-sync the first proof is the walk's, after its first written message (`archiveguard.go:134-138`) | CONFIRMED, LOW (macOS) | `TestB3AdvR5EncryptedSwapDuringPresync` (swap in `BeforeRead`): A's run ends "lock … was replaced"; B gains one unit, B's record stays "ok". The plaintext form of the same swap writes nothing (round 2's `TestB3AdvR2SwapDuringPresyncSwitchesAnother`). At `1b84d10`: 45 files plus the record, run history and attention notice | yes (its encrypted lock binding has the same order) | | |
| K5 | integrity (false failure) | `beginRun` takes the id with `ArchiveIDOrNew` (`run.go:1437`, `archiveid.go:53-58`) and writes the record best effort (`run.go:1412`). A record unreadable at that instant gives a new id; if the write fails too, the folder keeps the old id, and `archiveChangedFor` (`archiveguard.go:50-59`) reads old ≠ new as another archive at the post-load check (`run.go:905-907`; the capture's `graph.go:264-266`) | CONFIRMED, LOW (Windows, macOS); present since round 2's G1 | `TestB3AdvR5RecordHeldExclusivelyAtBegin` (Windows: the record opened with no sharing until the pre-sync hook): "now holds another archive … while the backup waited: nothing was written into it", the id unchanged, `status` `[no_schedule thunderbird_caught_up]` (the failed run is not in it). Red at `1b84d10` too. macOS form (`chmod`): the same | no (needs the archive id) | | |
| K6 | integrity (alerts) | `streak.go:158-164`: inside a real failure's streak whose email is owed, a wait's alert sends nothing and keeps the record. Only a later **real** failure sends the owed email; a success after it sends no recovery email (`:180-196`, `prev.Failing` false) | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR5OwedEmailNeverGoesWhenOnlyWaitsFollow`: the first send refused; 48 wait alerts with sending working: 0 emails, 1 notice in all (the first failure's); then a success: `Recovered`, 0 emails | no (the tidy-up wait is B3's) | | |
| K7 | integrity (test hygiene) | `reach.CallsGuard` (`reach.go:190-243`) returns at the first statement holding the guard, so a guard as a later argument than `m.Run()` in the same call, or after a `goto`, counts; a branch that runs the tests before it does not stop the scan (by design, `:267-270`). `ComputesUserFolder` (`:113-179`) sees only calls and same-file literals | CONFIRMED, LOW (macOS, syntax probe); no package missed | `TestB3AdvR5ReachEdges`: `CallsGuard` true for `os.Exit(pick(m.Run(), testhome.Run(…)))`, for `goto skip; os.Exit(testhome.Run(…)); skip: os.Exit(m.Run())` and for an `if` that runs the tests first; false for `defer`. `ComputesUserFolder` false for `var homeDir = os.UserHomeDir`, `env{dir: os.UserConfigDir}`, `look(os.Getenv)`, `os.Getenv(homeVar)` with the constant in another file, `os.Environ()`, `unix.Getenv("HOME")`. `TestB3AdvR5ReachMissedToday`: the value forms in the tree today (`presync/env_darwin.go:273`, `instance/instance.go:58`, `cmd/mailarchive/link.go:47`) and the test-file lookups (backupalert, keyring, tbengine, wizard, app, cmd, desktop, schedule) are all in packages that are roots or reach one | no | | |

### K1 (LOW): the walk's bound admits deletions in the other archive

**What happens.** Round 3 bounded a plaintext walk after a swap to about 10 seconds, on the
reasoning that what it writes there is message files that "stay where they were written". But the
exporter also removes: a message re-exported under a new name (a naming rule changed between
releases, a fill whose subject or date was missing before, a Full run) removes its previous page,
zip and kept `.eml` by path. In a rotation of one mailbox, the other drive's file for the same
message has that same path. So inside the bound, the other archive loses the files its manifest
lists, and gains the renamed copies, which no record owns. (In 10 seconds a local Full run handles
roughly a thousand messages on this Mac, several thousand on the Windows VM.)

**Why it matters.** The other archive's check fails ("NOT attested", `missing` for each) and its own
incremental backups do not repair it: the records are there, so the messages are skipped. A Full
backup of it does (the probe's last step: "attested" again). In a one-mailbox rotation the content
survives as the renamed copies, so nothing of the mailbox is lost, but the person is told the other
drive is damaged until they run a Full backup of it.

**Why LOW.** It needs a re-export under new names (uncommon outside a naming change or a Full run)
and a swap inside the window.

**Fix, in plain words.** Prove right before each removal of a previous path (or prove after every
written message for a plaintext walk too, as the encrypted walk now does; the fold left that to the
lead). A removal is the one write here that cannot be undone by the other archive's next backup.

### K2 (LOW): folder pages between proofs

**What happens.** J1's fix proves before the first folder page, before the root page and "on time
between pages". On time means every 10 seconds; writing a few hundred folder pages takes well under
that, so in practice the pages after the first are not proved at all. A swap during them puts every
remaining folder page of the first archive over the other archive's pages of the same folder name.

**Why it matters.** The other archive's root page is kept, but its folder pages now list the first
archive's messages (with links to files it may not hold), and a backup of it with nothing new keeps
them. `reindex` redraws them; so does its next backup that brings something new.

**Why LOW.** A view, not a record; in a one-mailbox rotation the lists mostly agree.

**Fix, in plain words.** Prove before every page (a proof is about 40 to 130 microseconds on a local
disk, 0.6 to 1.8 ms over SMB, small beside a page write with its two fsyncs), or write the pages
into a scratch folder in the archive and move them in after a last proof.

### K3 (LOW): an encrypted run whose own lock is removed is left "running"

**What happens.** J2's fix binds an encrypted run by its lock alone and has the deferred record skip
any keyed run whose lock is no longer its own. That is right when another archive, or another run,
is there now. But when the run's own lock file is simply removed (a person, a cleaning tool, a sync
tool; macOS and Linux allow it while it is open), there is nobody to record anything: the "running"
record stays, the run history gets no entry, and `status` says the run "never finished … crash,
kill, or power loss". Before this round the run recorded "failed, lock_lost".

**Why LOW.** The run still returns its error, so a scheduled run's alert and a manual run's message
say what happened; only the archive's own record and history are wrong, until its next run.

**For the back-port.** A lock-only form for every archive would do this to plaintext archives too
(here a plaintext run still records, because its id says the archive is the same).

**Fix, in plain words.** The lock alone cannot tell this from a swap to another archive that has no
lock file, so "record the failure when the lock file is merely missing" would bring J2 back. Telling
them apart needs something more than the lock: for an encrypted archive the vault's id (the
operator's allow-list decision the fold names); for a lock-only back-port, nothing short of an id.
Short of that, the run could keep its outcome on this computer (beside its job, as a refused run's
is) so the person's view is not "crash, kill, or power loss".

### K4 (LOW): an encrypted run's first message after the pre-sync

**What happens.** A plaintext run checks right after its manifest loads, which is after the
pre-sync wait; an encrypted run loads its manifest before the pre-sync, so it skips that check, and
its first proof after the wait comes after its first written message.

**Why LOW.** One sealed, unreadable unit, outside the other archive's file list; its keyless check
notes it and its next backup removes it (round 4's J2 evidence).

**Fix, in plain words.** Prove once after the pre-sync (or before the walk) for an encrypted run as
well, as the plaintext run does.

### K5 (LOW): a held record makes a healthy archive "another archive"

**What happens.** At a run's start, `beginRun` reads the record's id and, if it cannot read the
record, gives the archive a new id and writes it, best effort. If the same moment's write fails too
(the record held open with no sharing by another program, on Windows), the new id lives only in
memory. A moment later the record is readable again with the old id, and the post-load check sees
two different ids: "another archive … nothing was written into it".

**Why it matters.** A false failure of a healthy backup, worded as a swapped drive. Because the
deferred record also sees "another archive", nothing is recorded: `status` keeps showing the
previous run as the last, so a scheduled run's alert email is the only trace. The next run works.

**Why LOW.** It needs another program to hold the record open with no sharing exactly at the run's
start (which real backup, sync or scanning programs do that was not checked; the probe makes the
hold itself). Not this round's change.

**Fix, in plain words.** Do not mint an id from an unreadable record: when the record is there but
unreadable, read again (or run with no id, where the lock alone guards, as for an older archive).

### K6 (LOW): the owed email, when only waits follow

**What happens.** J4's fix makes a wait inside a real failure's streak, whose email is still owed,
send nothing. Round 4 offered that ("send the real failure's email, or nothing"). Its other ending:
when the real failure stops and only waits follow (the other computer was switched off mid-tidy-up),
no email ever goes: not the owed one, not the wait's 3-day alert, and the success that eventually
follows sends no recovery email (the streak was never "failing").

**Why LOW.** It needs the mail server to be unreachable at the real failure and a tidy-up to start
elsewhere in that streak; `status` names the tidy-up at each skip, and the first failure's notice
was raised.

**Fix, in plain words.** At a wait's alert inside an owed streak, send the wait's own email but keep
the streak's email owed (a separate "wait told" mark), so the wait is told once and the real failure
is still told at its next failure.

### K7 (LOW): what the throw-away-home checks still accept

The checks are against accidental omission, and none exists today. The shapes they still pass are
unusual (a guard as a later argument than `m.Run`, a `goto`); the `if` that runs the tests first is
the design's own allowance for a child helper. The lookups they miss (a per-user function taken as
a value, as `presync`'s seam `homeDir: os.UserHomeDir` is, or a constant from another file) are
ordinary Go; today every such package is a root by another file or reaches one. **Fix, if wanted:**
count a selector of a per-user function whether called or not, and read a package's literals across
its files; a test file that computes a per-user folder could make its package a root too.

## Back-port to 1.7.2 (the lock-only form)

| Finding | Applies | Why |
|---|---|---|
| K1 | yes | The plaintext walk is proved on time in either form; the exporter's removal by path is unchanged |
| K2 | yes | If it takes `writePages` with the on-time proof between pages |
| K3 | yes, wider | A lock-only deferred skip would leave **every** archive's record "running" when its own lock file is removed (here only encrypted ones) |
| K4 | yes | Its encrypted runs load the manifest before the pre-sync the same way; a plaintext run there needs a post-load lock check too |
| K5 | no | Needs the archive id |
| K6 | no | The tidy-up wait is B3's |
| K7 | no | B3's test hygiene |

## Held, refuted and not pursued

| ID | Attempt | Verdict | Evidence |
|---|---|---|---|
| L1 | False "lock lost" from the encrypted walk's per-message proof, on a healthy archive: local disk, network (SMB), Windows | HELD | `TestB3AdvR5ProofCost`, 2,000 proofs each form each place, 0 failed: macOS local 12 µs (encrypted) and 43 µs (plaintext) per proof; Windows local 43 and 107 µs; Windows over loopback SMB 0.57 and 1.76 ms. `TestB3AdvR5HealthyRunTime`, 400 messages, two Full passes each, plaintext and encrypted: all ok on macOS local, Windows local and SMB. Encrypted over SMB 7.7 s and 5.5 s against 6.7 s and 5.3 s at `1b84d10` (single runs, so noisy; the proof cost alone is about 0.23 s per 400 written messages). A real NAS has more latency per round trip, so more cost (not measured), but no new way to fail was found: the proof is the same three small reads (`archiveguard.go:103-112`) |
| L2 | The record briefly unreadable mid-run (torn, gone) | HELD | Round 4's `TestB3AdvR4RecordBrieflyUnreadableIsNoFailure` passes on macOS and Windows: an unreadable record reads as no id and the proof falls to the lock. (K5 is the same at the run's start, where `beginRun` mints an id) |
| L3 | The lock briefly unreadable | HELD (round 4's K2, now asked per written message for an encrypted run) | macOS has no mandatory locks (only `chmod` makes it unreadable); on Windows no program can deny read sharing on the lock while the run's own read-write handle is open (`lockfile.go:59`); SMB proofs: 0 failures |
| L4 | A page, README or report held open on Windows (a browser showing the archive, a scanner) | HELD | A write that fails for its own reason is a warning, as before (`archiveguard.go:157-163`, `run.go:389-390` via `writeArchiveReadme`); only a proof stops the run, and a proof reads only the folder, the record and the lock |
| L5 | Case-insensitive paths, other spellings | HELD | The guard reads the run's own spellings of the folder and the lock (`archiveguard.go:105`, `lockfile.go:142`), so the same files; a case-only rename keeps them valid (round 4's K6) |
| L6 | Very large archives whose pages take minutes | HELD | A proof every 10 s at a few hundred microseconds; no failure mode beyond L1 to L5. (K2 is the opposite case: pages too quick to be proved) |
| L7 | A cloud-synced archive folder whose sync client replaces the lock file with another device's | HELD (true positive) | That is two computers writing one archive; the lock's "replaced" is right, and MailArchive already warns "Keep it local" for a synced folder (MA-1083) |
| L8 | J2: more than one message in the other archive | REFUTED | See J2's row: a stop check before every item and every Graph message, units written per message; 1 unit in round 4's probe and the builder's test |
| L9 | J2: another run of the same encrypted archive takes the lock | HELD (the fold's stated change) | The first run stops at its next message and records nothing; the holder records its own. On Windows the lock cannot be taken from a live holder. K3 is the case with no other run |
| L10 | J3: the collapse reads `.eml` files by path; a stop after its index re-key | NOT PURSUED | After a swap, the merge decisions would read the other archive's files, and the index re-key (by handle, the run's own archive) happens before the proof (`graph.go:418-439`). A stop there is a crash for the idempotent re-run, but a merge decided from absent files could drop an index row the re-run then keeps in the manifest. Needs a pre-v5 archive owing the collapse, `-raw`, a distinct same-envelope reuse pair and a swap inside the collapse's seconds; `reindex` repairs the index |
| L11 | J1: cancel paths write the README or report unproved | REFUTED | `run.go:1097-1107` (`cancelled`), `:1116-1122` (cancel before the first source), `graph.go:571-581` (interrupted capture): all through `finishChecked` with the proof |
| L12 | J1: the search index, attachments, zips | HELD / NOT PURSUED | The index is written through its open SQLite handle, which follows the run's own folder; attachments and zips are written in the walk (its bound; K1 for removals). Round 4's K12 (SQLite removing `-wal`/`-shm` by path at close) is unchanged and not pursued |
| L13 | The pre-sync swap: `SweepOrphans` runs by path before the post-load check (`run.go:879`) | HELD | It removes the other archive's temps older than the run's start and zips without a page: its own crash leftovers by its own rule, which its next run would remove |
| L14 | J4: other orders (wait streak then real failure; real failure delivered then waits; undelivered then real failure again) | HELD | H3's and the builder's `TestAWaitAlertNeverTellsARealFailureWhoseEmailIsOwed`; round 4's J4 probe passes. K6 is the remaining order |
| L15 | The presync test's bounded waits weaken it | REFUTED | `presync/scheduled_fixes_test.go:586-599`, `:632-686`: each `<-ch` became `waitBounded` (2 minutes, `t.Fatalf` by name), the unblock is closed once and also at cleanup; no assertion was removed or changed. Under load the test now fails by name (the session missed its 300 ms budget before S8) instead of hanging; that flake stays a red, not a pass |
| L16 | A capture's post-load check or tail proofs give a false failure | HELD | The same proofs as a local run's; K5 applies to the capture's post-load check as well (`graph.go:264`) |
| L17 | Rounds 1 to 4's attempts come back | HELD | Every round-1 to 4 probe passes on macOS (round 4's encrypted probe sees only the stated residual) and on Windows as in round 4 |

## Unfiltered run

`go test -count=1 -p 4 -timeout 60m ./...` in the worktree at `981123c`, probes and the baseline
checkout out (macOS): 61 of 62 packages pass (the covers scan and the throw-away-home meta-test in
`assureblock` included; `internal/presync` in 25 s, its bounded waits not reached). Red:
`internal/app` on `TestCopyExportTransportOutageAndTheBreaker` alone ("a short outage ended as
Finished with problems", a copy export, which uses no archive guard). The run was load-affected:
`internal/app` took 48 minutes while the 1.7.2 builder's own `internal/app` run and the Windows VM
(about 4 cores) ran beside it. Alone, `go test -run '^TestCopyExportTransportOutageAndTheBreaker$' -count=3 ./internal/app/`
passes 3 of 3. Not a finding of this round (round 4 saw the same copy-export class red under load).

## Probe locations (uncommitted, build tag `b3adv`; kept in `.claude/testdata/`)

- `.claude/testdata/probes-r4/`: rounds 1 to 4's thirteen files, unchanged copies of
  `gmail-dedup-b3-adv4/.claude/testdata/probes/`.
- `.claude/testdata/probes-r5/internal/app/zz_b3adv_r5_test.go`: K1, K3, K4, K5 (macOS form), L1.
- `.claude/testdata/probes-r5/internal/app/zz_b3adv_r5_tail_test.go`: K2.
- `.claude/testdata/probes-r5/internal/app/zz_b3adv_r5_windows_test.go`: K5 (Windows form).
- `.claude/testdata/probes-r5/internal/backupalert/zz_b3adv_r5_streak_test.go`: K6.
- `.claude/testdata/probes-r5/internal/testhome/reach/zz_b3adv_r5_reach_test.go`: K7.
- `.claude/winrun-b3adv5.sh`: the Windows runner.
