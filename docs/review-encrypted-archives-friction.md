# Friction review — encrypted archives, first release (P3-8 gate)

**Proposed verdict (a proposal for the owner, not a sign-off): SHIPPABLE as a design, but not
ready to ship.** None of the design-choice findings (Type II) is high-friction. The ceremony
(password, recovery key shown once and typed back, terminal-only keyed writes) is legible and
works. But the kit's rule is that the buildable-away backlog (Type III) is built before ship, and
that backlog holds **7 high-severity bugs**. Two of them are security bugs found on Windows. Until
Tier A below is fixed and re-walked, the release must not ship.

The kit is the assurance kit's friction-review process (`../assurance-kit/process/friction-review.md`):
- **Type I** means the ceremony is the control. It is counted, not faulted.
- **Type II** means it can be reduced by redesign without weakening the control. These findings
  drive the verdict.
- **Type III** means missing tooling or wording. It is excluded from the design verdict but must be
  built before ship.

## Header

| Item | Value |
|---|---|
| Feature | Encrypted archives, first release: phase-3 slices P3-0 to P3-5, then the P3-8 gates (owner ruling 2026-10-26) |
| Reviewed SHA | `a531899` (branch `encrypted-archives`), "PG4's adversarial pass — the fixes (MA-848)" |
| Binary | prebuilt `p38/bin/mailarchive`, a darwin-arm64 development build of 34,593,522 bytes. The walkers did not check its embedded version. |
| Baseline digest | Not taken by the walkers. No covers-baseline digest was recorded for this walk. |
| Environment | macOS host (Darwin 25.6.0, arm64), with every walker in its own sandboxed HOME, config and cache under the session scratchpad. Windows: a Parallels "Windows 11" guest (build 10.0.26200, ARM64) running amd64 binaries under emulation, cross-compiled from a `git archive` of `a531899`. |
| Date worked | 2026-10-27 |
| Actors | owner (CLI, non-technical, following `-h` and the program's own words); owner who forgot the password; owner who lost the recovery key; owner whose key files were deleted; owner on a second computer; owner forgetting the key or the archive; a new person inheriting the archive cold; owner/admin under failures (wrong secret, busy, drive missing, interrupts, tampering, the way out); owner in MailArchive Desktop (browser); the X-series walker (UX contract rows X1–X17); the Windows lab |
| Walkers | w1 owner CLI · w2 lost and inherited · w3 failures and exit · w4 Desktop · x-walk (X1–X17) · windows-lab |
| Commands and statuses | Recorded per cell in the walkers' raw results. Exit codes are quoted in the table and backlog below. |
| Cells walked | **26** friction cells (actor × scenario), plus **16** X rows and **9** Windows lab items, reported separately |
| Functioning | **15** fully · **9** partially · **1** not (K9's download line) · **1** not scored (a design question: a lock state on each row of "Your archives") |
| High-friction cells | **4** of the 26 carry a high-severity finding: w1 cell 4, w3 cell 4, w3 cell 5, w4 cell 4. Two X rows (X2, X6) and two Windows items (4, 7) carry the others. That makes **7** distinct high-severity defects. |
| Type II findings | 18, of which 5 are medium and none high (section below) |
| Type III backlog | 31 ranked items (B1–B31) plus a low-severity list |

## Report table

Backlog ids (B-numbers) point to the ranked backlog below. "w1 c2" means walker 1, cell 2.

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| w1 c1 · owner CLI · make a new encrypted archive with `-encrypt` | yes (6 steps) | Refused cleanly with no terminal. The show-once key screen, typed confirmation and 12-character minimum are clear. Program copies are never asked about or mentioned: 34.6 MB was added silently, against design §5.1 step 4. The wrong-key message shows "(vault: …)". The recovery key is typed back blind. The key file names its archive only by a hex id, and nothing says it was saved. Every mbox first run ends with the general `-raw` WARNING. | I · III · III · II · III · III | B9, B14, T2-7, B24; the `-raw` warning goes to the general backlog |
| w1 c2 · owner CLI · back up again, `status`, `verify`, `verify -full`, one damaged unit | yes (5) | A "swept 0 unit and N record file(s) nothing names" line on every run. Damage is named only by sealed file name, and the remedy is printed twice. The cheaper repair (a `-mode full` backup) is never offered. An incremental backup into a damaged archive says nothing. `status` ignores the remembered key. Mixed time zones. The keyless `verify` gives no next step. | III · III · III · II · III | B18, B19, T2-4; low list |
| w1 c3 · owner CLI · search, serve on loopback | yes (3) | `search` prints a `-> inbox/….html` path that does not exist for an encrypted archive. `serve` prints a bare URL that answers 401 before the working link. `serve -h` still says "contains search.db". The private link, loopback and lenient key entry work. Sealed files, the program and the key file all return 404 through the reader. | III · III · III · I | low list |
| w1 c4 · owner CLI · program copies, following the note as a future person | partial (6) | **A tampered self-copy is replaced silently and reported "updated" (bug, high).** Turning copies on or off makes no output line. "It cannot be checked" is not explained for a self-copy. The years-from-now steps assume terminal skill and have no Mac or Gatekeeper line. The README tamper warning carries the wrong prefix. The offline warning is truthful. | III (bug) · III · III · III · III · I | **B3**, B23, B22; low list |
| w2 setup · owner · make, back up, save recovery key and recovery file | yes (6) | The key is still shown and must be typed back even with `-recovery-key-out` (the control). Killed runs leave only an empty folder. The first-run hints do not say a password will be asked. Development build: only one self-copy. | I · III · III | low list |
| w2 c1 · forgot the password · recovery key, then `change-password` | yes (5) | One prompt takes either secret, and the key is accepted in any case. Nothing confirms the key was accepted. The third wrong try exits with "vault:" and no next step. An empty Enter counts as a try. | I · III · III · II | B14, T2-8 |
| w2 c2 · lost the recovery key · `new-recovery-key` | yes (5) | Proven: the superseded key plus its old recovery file still open a copy. This is inherent (the archive key never changes), and the program says so. The output-file clash is checked before any prompt, which is good. | I · (positive) | none |
| w2 c3 · key files deleted · `restore-key-file` | yes (8) | The `search` refusal names the path three times and ends "open it … with its password", which cannot work. `status` prints the RED reason twice and adds "copies … were updated" under RED. Prompt order is odd: where to save the recovery file comes before the new password. Forcing a new password and a new key is the control. | III · III · II · I | B12, T2-9; low list |
| w2 c4 · second computer · fresh HOME, record damage, `repair-record` | yes (9) | The "record damaged" refusal does not name `repair-record` (design §4.3 case 3), and it asks for the password first. Recording is silent and there is no verb to list records. A damaged main record copy is not healed. The sweep line appears. `repair-record` itself is clear. | III · II · III · III · III · (positive) | B13, T2-2, B18; low list |
| w2 c5 · forget the key and the archive | yes (12) | `forget-key` with no terminal is refused for the wrong reason. `-remember` gives no confirmation. The forgotten path is shown case-folded. "Record it again?" is unexplained, and answering n has no next step. Two-step confirmations are the control. Re-recording is silent. | III · III · III · III · I · III | B17, B27; low list |
| w2 c6 · inheriting cold · only the folder | partial (11) | With no secret, nothing can be read (the control). README does not say where a secret is likely kept. The Mac self-copy is filed under "a Linux or bare program" with no Gatekeeper steps. README's first route is Desktop (which the inheritor lacks), then a download (needs a network). The offline route needs terminal skill. With the password, it works in about 5 steps with 2 dead ends. | I · III · III · II · II | B22, T2-5 |
| w3 c1 · wrong password three ways (backup, `verify -full`, `decrypt`) | yes (7) | The final refusal has "vault:" and no way forward, and does not name the `-password-file` it read. The secret-file mode check is clear and gives the exact command (control). `decrypt` asks its confirmation before the password, so a wrong password wastes it. | III · III · I · II | B14, T2-2 |
| w3 c2 · archive busy | partial (6) | A keyed backup and `decrypt` ask for the password (and the confirmation) before refusing busy, while `verify -full` refuses first (design §8). The busy text is raw diagnostics (pid, host, nonce, lock path) printed twice. Nothing is written (safe). | II · III · I | T2-1, B15 |
| w3 c3 · drive missing (folder renamed) | partial (7) | Never says "is the drive plugged in?". It offers only `forget-archive` and "open it with its password", although the right key was given. The path is printed three times. `status` says "run an export into it first". No decoy archive is created. | III · III · III · I | B12; low list |
| w3 c4 · interrupts at prompts, `verify -full`, `reindex -rebuild`, backup | partial (14) | **`reindex -rebuild` says "nothing was saved" and exits 130 when the index was saved (bug, high, reproduced twice).** `reindex` notes are alarming. `decrypt` and `reindex` are absent from X1. An interrupted backup prints "Done." first. "Nothing was changed" after Ctrl-C at Remember is not quite true. Prompt and `verify -full` interrupts are clean. Mixed time zones. | III (bug) · III · III · III · III · I · III | **B4**; X1 amendment; low list |
| w3 c5 · tampering (README, planted file, self-copy byte, unit byte) | partial (14) | **Self-copy replaced silently (same bug as w1 c4).** The keyless `verify` says "intact" with no word that program/ and README need the key to check. The damage remedy is printed twice, and the `-mode full` repair goes unmentioned. Wrong warning prefix. An incremental backup into a RED archive is silent. README and planted-file warnings are proportionate. | III (bug) · II · III · III · III · I | **B3**, T2-3, B19 |
| w3 c6 · the way out: `decrypt`, synced destination, stopped halfway | yes (9) | CRIT-03 option B (the owner's ruling that nothing tracks a stopped unencrypted copy) is told before and after. The terminal message does not repeat "readable". A re-run picks "-2" and does not mention the unfinished sibling. The full path, about 190 characters, must be typed back with no retry. The size estimate is inflated. No progress for 38 s. The copy's README does not say it is an unencrypted copy. The plan, the warnings and the typed path are the control. | III · II · II · III · III · III · I | T2-10, T2-11; low list |
| w4 c0 · setup · CLI-made archive, opened in Desktop | partial (6) | `-encrypt` needs a terminal (harness friction). No "saved to" line for the key file. A CLI-made archive has no Run now in Desktop and no exact command is given. | I · III · III | B24, B26 |
| w4 c1 · Desktop · unlock, read the card, lock, unlock again | yes (8) | No sidebar link to the Encryption card, though the locked reader sends people there. Switching archives silently locks. The program-copies checkbox promises "Mac, Windows and Linux, about 134 MB" in a development build. | III · II · III | T2-12; low list |
| w4 c2 · Desktop · Run now | yes (4) | The result says "open it in MailArchive Desktop" to someone already in it. The sweep line appears in the activity log. Recent-backups rows are all alike (sealed counts) even while unlocked. No progress count. | III · III · II · III | B18, T2-4; low list |
| w4 c3 · Desktop · program-copies switch | yes (8) | Run now's result never says the 34 MB copy was removed or written. A cancelled run is recorded as "program_updated" though the file was not rewritten (unsure). | III · III | B23; low list (verify) |
| w4 c4 · Desktop · cancel a running backup; Skip download | partial (5) | **Run now never shows a Cancel button (high; found by reading the page script, not by a real click).** Skip is correctly absent in a development build. Health says a cancel "failed" and points to details that do not exist. | III (bug) · I · III | **B7**, B20 |
| w4 c5 · Desktop · status and health | partial (3) | Health is byte-identical locked and unlocked, and tells someone in Desktop to open Desktop. A local encrypted archive is shown as "Your personal Microsoft 365 archive", with Archive now leading to "not configured". The health line names `.mailarchive-vault.json`. | II · III · III | T2-4, B25; low list |
| w4 6a · deferred · a lock state on each "Your archives" row | not scored (4) | Not needed for the first release (see "Deferred decisions"). | II | later: a "key remembered" badge |
| w4 6b · deferred · K9's "this file is not encrypted" line on reader downloads | **no** (5) | Absent. The zip and the `.eml` download as plain files with no line. K9 (the design rule that a downloaded zip or `.eml` is allowed, with one line saying it is not encrypted) requires it. | III | **B8** |
| w4 6c · deferred · a plaintext golden test of the overview and health reply | yes (3) | No mail-derived text was found. `/api/activity` carries source paths (the X13 standing condition). | II | add the golden (see "Deferred decisions") |

## Type III operability backlog — build before ship, ranked

### Tier A: high-severity bugs (block the release on their own)

- **B1 — Windows: a program swapped after start becomes the archive's "own program", with no
  warning.**
  - What breaks: R14 from the program-copies review, which says a self-copy is written only if the
    program file on disk is still the one that started.
  - Measured: a decoy (`whoami.exe`) was written into `program\` and the run exited 0.
  - Root cause: on Windows, Go's `os.Stat` loads the file id only when `os.SameFile` is called, so
    the check compares the new file with itself.
  - The same root breaks R3's "program/ is still the same folder" checks
    (`internal/app/programstep.go:251`, `:349`) and `internal/vault/restore.go:467` (a later check
    catches that one).
  - Fix: take identities from an open handle, or force the id to load right after the Stat.
  - Gate: security-sensitive, so **an adversarial pass is owed on the fix.**
- **B2 — Windows: `decrypt` writes a readable copy into a synced folder reached through a junction,
  without asking twice.**
  - What breaks: K5, the rule that a plaintext copy to a synced or removable folder asks twice.
  - Measured: the direct Dropbox path is refused (exit 1). The same folder through `mklink /J`
    copied (exit 0).
  - Root cause: `util.ResolveExisting` relies on `filepath.EvalSymlinks`, which since Go 1.23 does
    not follow junctions.
  - Fix: resolve junctions and mount points on Windows, and add a junction twin to the K5 tests.
  - Gate: security-sensitive, so **an adversarial pass is owed.**
- **B3 — A changed self-copy of MailArchive is replaced by the next keyed backup silently, and
  reported "updated".**
  - Code: `internal/app/programdecide.go:220-223`, against the warning in the release-copy branch
    at `:144-150`.
  - This breaks the program design §4.2 and §6 ("replaces a changed copy and warns").
  - It hides the exact swap the README warns about, and the person who may have run the swapped
    copy is never told.
  - Fix: warn, record a distinct outcome (for example `program_repaired_changed`), and show it in
    `status`.
  - Gate: the adversarial pass re-checks it.
- **B4 — `reindex -rebuild` reports "nothing was saved" and exits 130 when the rebuild was saved.**
  - `cmd/mailarchive/main.go:805-808` checks whether a signal arrived before it checks the result.
  - Fix: check the result first. A saved rebuild reports saved.
- **B5 — `status` shows GREEN for an encrypted archive whose two key files are missing, when this
  computer has not recorded it** (a copied or inherited archive). Nobody can open it, yet nothing
  warns. Make it RED and name `restore-key-file -from FILE`.
- **B6 — `restore-key-file -remembered` claims "This computer remembers the archive's key" without
  checking.**
  - It then asks four questions and runs the key derivation.
  - It ends with "vault: … no stored secret for archive:…: file does not exist", and shows archive
    `000…0` on a plain folder.
  - Fix: check for a remembered key, and that the folder is encrypted, before the first question.
- **B7 — Desktop's Run now never shows a Cancel button.**
  - `#jobCancel` lives in the Microsoft 365 "Archive now" card, and Run now's `pollBackup()`
    (`internal/desktop/dashboard.go:1822-1830`) never reveals it.
  - It was found by reading the script, so **confirm with a real click** when fixing.

### Tier B: medium

- **B8** — K9's line is missing on reader downloads (`internal/server/page.go:92`). Add: "A
  downloaded file is not encrypted: it is an ordinary copy on this computer."
- **B9** — `-encrypt` at the CLI never asks about program copies and prints nothing about them.
  - Design §5.1 step 4 calls for the question, on by default.
  - The copies are 34 MB here, about 134 MB on a release.
  - Moved from the walker's Type II: the design already settled this, and the code does not match.
- **B10** — Windows: Ctrl-C at any hidden prompt prints "mailarchive: EOF" and exits 1, instead of
  "interrupted; nothing was changed" and exit 130 (`internal/tty/secret_windows.go`).
- **B11** — Windows: the secret-file refusal's `icacls` remedy loops.
  - It leaves an explicit grant to another account in place, and never changes a foreign owner.
  - Its `%USERNAME%` fails in PowerShell with error 1332.
  - Fix: name the accounts found, give the resolved account name, and use `/setowner` for the owner
    case.
- **B12** — Refusals give advice that cannot be followed.
  - Key files missing: they say "open it with its password".
  - Folder missing: no "is the drive plugged in?", only `forget-archive`.
  - The path is repeated three times.
  - On Windows, a recorded parent swapped for a junction (case L) is worded as "removed", with
    advice to forget it. Following that advice drops the record of an archive that still exists.
- **B13** — The "record damaged" refusal does not name `mailarchive repair-record`, as design §4.3
  case 3 requires now that P3-1a has shipped.
- **B14** — The wrong-secret refusal starts with "vault:", gives no next step, and does not name the
  `-password-file` it read. Desktop words it differently (two voices). The key-confirmation error
  also shows "(vault: …)".
- **B15** — The busy-archive message is raw lock diagnostics printed twice. Use `verify -full`'s
  wording: what holds it, and "try again when it finishes".
- **B16** — `change-password` and `new-recovery-key` on a plain or missing folder prompt for a
  password, then fail with "vault: no vault found". Judge the folder first.
- **B17** — One no-terminal refusal is reused where it does not fit.
  - `forget-key` needs no password, and the sentence about scheduled backups is off-topic.
  - `change-password` with `-password-file` is not told that the new password must be typed.
- **B18** — "swept 0 unit and N record file(s) nothing names: …" is printed on every keyed backup,
  CLI and Desktop (`internal/app/encrypted.go:70`). Stay silent for routine leftovers.
- **B19** — `verify -full` damage handling.
  - It names a damaged message only by its sealed file name, though it holds the key and could give
    the date, sender and subject.
  - The remedy is printed twice.
  - It never offers the cheaper repair. A `-mode full` backup from the original mail was proven to
    clear it.
  - An incremental backup into a RED archive says "Done" with no reminder.
- **B20** — A cancelled run reads "the backup failed — open … for the details" in `status` and in
  Desktop health (`internal/health/health.go:1011`), while Desktop's result says "cancelled … safe".
  Two truths for one run (X10).
- **B21** — `status -json` on a sealed archive reports `messages 0`, `indexed 0`,
  `indexed_known true`. A monitor sees an empty archive.
- **B22** — The program section of README.txt (`internal/app/programnote.go`).
  - It files the Mac self-copy under "a Linux or bare program", with no Gatekeeper steps (design
    §13.1).
  - It does not explain "it cannot be checked".
  - It tells Windows users to run `mailarchive.exe`, where `mailarchivew.exe` opens Desktop.
  - It assumes terminal skill: the full quoted path, "it asks for the password, then prints a link".
  - It does not tell an inheritor where a secret is likely kept.
- **B23** — Adding, removing or repairing program copies (34–134 MB) prints nothing in the backup
  output or in Run now's result.
- **B24** — The saved recovery-key file names its archive only by a 32-hex id that appears nowhere
  the owner looks. No "Recovery key saved to <path>" line is printed. Put the folder name and date
  in the key file, and the id in README and `status`.
- **B25** — Desktop shows an encrypted archive made from local mail as a Microsoft 365 archive (the
  heading, Archive now, the M365 settings). `localOnly()` is false for the encrypted kind
  (`internal/desktop/localarchive.go:87-93`).
- **B26** — An encrypted archive made at the CLI has no Run now in Desktop and no exact command.
  Desktop answers "no run-job backup to run here".
- **B27** — `forget-archive` and Desktop's forget plan show the recorded path case-folded, yet the
  person is told to type it "exactly as shown".
- **B28** — Help gaps.
  - MA-39 (the help-totality test) does not walk `decrypt -h`, the new verbs in root `-h`, or
    `-full` in `verify -h`.
  - README.md has no encryption section.
  - `encrypt -h` prints a refusal, not usage.
- **B29** — The X17 sweep (MA-622) does not reach `internal/app/readme.go`,
  `internal/app/programnote.go` or `internal/keyring`. They are clean today but unguarded.
- **B30** — Windows test coverage.
  - The `internal/desktop` tests do not compile for Windows (`syscall.Mkfifo` in
    `keys_pass2_test.go`).
  - There is no ConPTY-backed prompt test.
  - The program-copy stand-in is not named `mailarchive.exe`.
  - About 20 Unix-only test seams fail on Windows.
  - CI is ubuntu-only. Add a `GOOS=windows` compile check.
- **B31** — Windows: an external USB disk that reports "fixed" is not asked twice, and a removable
  volume mounted inside a folder on C: reads as C: (`internal/app/removable_windows.go`). The design
  already lists this as pending.

### Tier C: low (one line each)

- Wording and ordering:
  - `search` prints a `->` file path that does not exist for an encrypted archive.
  - `serve` prints a bare URL that answers 401 before the working link.
  - `serve -h` and `reindex -h` describe plaintext archives.
  - `status` mixes local time and UTC.
  - The keyless `verify` gives no next step.
  - The README tamper warning carries the "copies of MailArchive:" prefix.
  - `status` RED text is doubled, with "copies … updated" under RED.
  - After the recovery key is accepted, nothing confirms it.
- The key record:
  - `-remember` gives no confirmation, and a later open does not say it used a remembered key.
  - "Record it again?" is unexplained, and answering n has no next step.
  - Recording is silent on a new computer, and re-recording is silent too.
  - A damaged main record copy is not healed on a successful run.
  - `forget-key` reports success when nothing was remembered.
- Interrupts and `reindex`:
  - An interrupted backup prints "Done." before "interrupted".
  - "Nothing was changed" after Ctrl-C at Remember, when the record had been written.
  - `reindex -rebuild` prints "index is missing messages" on every run.
- `decrypt`:
  - The stop message does not repeat "readable", and says "stopped" twice.
  - The size estimate is inflated (a 32 MiB reserve).
  - No progress output.
  - The copy's README does not say it is an unencrypted copy.
  - It prints its plan on stdout and its count twice (X8).
- Desktop:
  - No "Encryption" sidebar link.
  - The development-build card promises three platforms.
  - The Run now result says "open it in MailArchive Desktop".
  - No running count.
  - A cancelled run is recorded as `program_updated` (unsure, verify).
  - The health line names `.mailarchive-vault.json`.
  - A key-change question is carried in the `error` field.
  - "no run-job" jargon.
  - The Backup alerts card is shown for an archive that cannot be scheduled.
  - "Make its key" and "1 messages were left out".
- CLI and docs:
  - `-remember` and `-remembered` are near-identical names.
  - `forget-key` has no usage line of its own in root `-h`, and `decrypt` is missing from the `-out
    is required` list.
  - The key verbs' help has no "Exit:" line.
  - `-encrypt` usage shows `-input` as optional.
  - Windows commands are printed with doubled backslashes.
  - `status` "Schedule: none recorded" does not say schedules are unavailable for encrypted
    archives.
  - CSRF refusal jargon.
  - `util.IsPlaceholder` (OneDrive) has no Windows test.
  - `verify -full` lists a missing file twice on Windows (slash against backslash, which defeats
    the FID-04 de-duplication).
- General (not encryption): the first mbox run's `-raw` WARNING, which every new owner meets on
  their first encrypted run.

## Type II findings — these drive the verdict

None is high. Five are medium (T2-1, T2-3, T2-4, T2-5, T2-6). Each can be reduced without weakening the control.

| # | Finding | Severity | Proposed change |
|---|---|---|---|
| T2-1 | A busy archive is refused only after the password (and `decrypt`'s confirmation) is asked. `verify -full` refuses first, as design §8 says. | medium | Check the lock before any prompt, on every keyed verb. |
| T2-2 | The same pattern elsewhere: a damaged record is refused after the password, and `decrypt`'s confirmation comes before the password, so a wrong password wastes it. | low | Judge everything that can refuse before asking for a secret. Keep the confirmation across a retry. |
| T2-3 | The keyless `verify` says "intact" beside a planted `.exe` or an edited README, with no word that program/ and the note can only be checked with the key. Program design §3 keeps program/ informational; the outcome need not change. | medium | One line: "program/ and README.txt are checked only by `verify -full`." |
| T2-4 | The key held or remembered is ignored where it would help. `status` says "open it with its password" though the key is remembered. Desktop health is identical locked and unlocked. Recent-backups rows stay sealed while unlocked. | medium | While a key is held (remembered, or unlocked in Desktop), show the sealed counts read-only. Otherwise say exactly where to unlock. |
| T2-5 | An inheritor's offline route needs terminal skill. README offers Desktop first (which they lack), then a download (needs a network). | medium | Reorder for the offline case and make the terminal steps literal. Building this is B22. |
| T2-6 | Desktop's "Save the recovery file…" writes into the picked folder with no preview. Design §16 records that X14's preview is owed. | medium | A one-line preview and confirm, or an operator ruling that single small files from a native dialog are out of X14. |
| T2-7 | The recovery key is typed back blind, though it was just shown openly. | low | Echo it, or show which group differs. |
| T2-8 | An empty Enter counts as one of the three tries. | low | Re-ask without counting. |
| T2-9 | `restore-key-file` asks where to save the new recovery file before the new password, and does not offer to save the new key to a file. | low | Reorder, and offer it. |
| T2-10 | A re-run of `decrypt` picks "-2" and does not mention the unfinished readable copy beside it. | low | Owner call within CRIT-03 option B. Checking the same parent for a `_COPY-NOT-FINISHED.txt` sibling needs no tracker. |
| T2-11 | The synced-destination confirmation needs the full path, about 190 characters, with no retry. | low | Type the new folder's name, and allow one retry. |
| T2-12 | Switching archives silently locks the encrypted one. | low | "Switching locks <archive>." |
| T2-13 | `repair-record` on a healthy record exits 1. A declined confirmation exits 1 and this is undeclared. | low | Decide in X1 (see the amendments). |
| T2-14 | A stub `encrypt` verb exists beside the working `-encrypt` flag. | low | Drop it until P3-6, or reserve it in X4. |
| T2-15 | `-password-file` lifts the terminal need on read verbs but not on write verbs. This is deliberate under O5 (keyed writes are attended), but it is not said. | low | Say it in each write verb's help line. |
| T2-16 | Windows: `RestrictToOwner` sets the access list but not the owner. The unencrypted copy's folder inherits its parent's access list instead of the Unix 0700 (not measured with a shared parent). | low | Set the owner too, and restrict the copy folder on Windows. |
| T2-17 | `GET /api/activity` carries source and destination paths (the X13 standing condition). | low | No change now. |
| T2-18 | The texts say "MailArchive Desktop" about 20 times, and the create dialogs are titled "Mail Archive Export" (X17). | low | Operator ruling. |

Already low-friction (noted, no action): the `-recovery-key-out` clash is checked before any
prompt; `repair-record`'s output is clear; the cold path with the password takes about 5 steps.

Reclassified by this synthesis: the silent self-copy repair (w1 called it Type II) is B3, Type III,
a bug against the design. The missing program-copies question (w1, Type II) is B9, Type III,
because the design already settled it.

## Deferred decisions

The phase-3 design deferred three items to this review (§16, line 1719).

1. **A lock state on each row of "Your archives": not needed for the first release.**
   - Walker w4 unlocked an archive, switched away and back, and found it locked. A row other than
     the current one is therefore always locked, and the current one shows its state on its
     Encryption card.
   - The one thing a badge would add is "opens by itself (key remembered)". The design's O3 says a
     remembered archive re-opens on switch-back, and remembered keys were not walked in Desktop.
   - Recommendation: no badge now. Build T2-12's "Switching locks <archive>" line. Add a "key
     remembered" badge later.
2. **K9's "this file is not encrypted" line on reader downloads: needed, and absent. Build before
   ship (B8).**
   - Unlocked, the attachment zip downloaded as HTTP 200 `application/zip` in plaintext, and the
     `.eml` as `message/rfc822` in full, with no line on the message page or the search result's
     zip link.
   - K9 (design line 153) requires the line. Design line 1596 had placed it in P3-3.
3. **A plaintext golden test of the overview and health reply: not a blocker on the evidence, but
   cheap. Add it with the fix batch.**
   - A search for planted markers (subject, sender, filename) found nothing in 6 saved dashboard
     pages (locked and unlocked) or in the health JSON.
   - The plaintext `.mailarchive-lastrun.json`, `runhistory.json` and `manifest.json` hold only
     status, times, "encrypted" and the archive id.
   - The golden guards against a regression. It does not cover `/api/activity`, which carries
     source paths by design (X13).

## X1–X17 walk (encrypted surfaces)

| Row | State | One line |
|---|---|---|
| X1 typed exits | **partial** | The new verbs exit consistently (0, 1, 2, 3, 130, 143), but `decrypt`'s set and the 130/143 of keyed `serve`, `search`, `extract`, `reindex` and `decrypt` are undeclared. B4's false 130 and Windows' B10 break it. |
| X2 one refusal voice | **gap** | "vault:" prefixes, refusals that cannot be followed (B12, B16, B17), B6, two voices for the wrong secret (B14), the remedy doubled. |
| X3 help is total | **partial** | Every new verb prints usage except `encrypt -h`. MA-39 does not walk the new verbs. README.md has no encryption section. The row's "Met" is stale. |
| X4 one grammar | **partial** | A stub `encrypt` verb sits beside the `-encrypt` flag. `-remember` and `-remembered` are near-identical. |
| X5 flags mean the same | **partial** | `-password-file` reaches differently on read and write verbs (deliberate, unstated). `reindex -h` text is stale. |
| X6 legibility surface | **gap** | GREEN for an archive with no key files (B5). `status -json` shows zeros (B21). A cancelled run worded as failed (B20). "Schedule: none recorded". |
| X7 no traceback | **met** | No panic text in about 70 runs. All encryption and job routes recover through `d.api`. |
| X8 output discipline | **partial** | `decrypt` puts its plan on stdout and its count twice. The sweep line is noise on every keyed run. |
| X9 loopback and guarded | **met** | Encryption and job endpoints refuse with 401, 403 or 405 as they should. `serve -addr 0.0.0.0` is refused for an encrypted archive. |
| X10 one status truth | **partial** | Desktop "cancelled … safe" against `status` "failed". Health says "open in Desktop" while unlocked in Desktop. |
| X11 plain labels | **partial** | An encrypted local archive is shown as Microsoft 365 (B25). Case-folded paths to type back (B27). Mechanism jargon ("no run-job", "Make its key"). The alerts card is shown. |
| X12 locked, not blank | **met** | The locked reader page (503 when locked, 401 without a link) and the JSON 401 carry no data, path or token. |
| X13 content reads guarded | **met (standing condition)** | All 19 encryption and job routes are guarded POSTs. `/api/activity` and `/api/health` stay GETs, as recorded. |
| X14 look before writing | **partial** | Desktop's copy plan and start are exact (a stale plan is refused). "Save the recovery file…" has no preview (T2-6). CLI `decrypt` and the create are outside the row's wording. |
| X15 mail text inert | **met (Desktop); reader not walked live** | Desktop writes through `textContent` with `nosniff` and `no-store`. The keyed reader's rendering of a hostile subject was checked by code only. |
| X16 Thunderbird pre-sync | **not applicable** | Not an encrypted surface. |
| X17 one name | **partial** | README.txt names `mailarchive.exe` for Windows. The sweep does not reach the encrypted texts (B29). "MailArchive Desktop" needs a ruling. |

**Contract amendments the operator must make** (operator decisions, not self-granted):
1. **X1:**
   - Declare `decrypt`'s set: 0 done · 3 partial · 1 refused · 130 / 143 stopped.
   - Widen the 130/143 clause to any keyed prompt (`serve`, `search`, `extract`, `reindex`,
     `decrypt`) and to a stopped `reindex -rebuild`.
   - State that a declined confirmation is a refusal (1).
   - Rule whether `repair-record` on a healthy record is 0 or 1.
   - Design §10 and the §16 per-verb list also need the same correction. That is a design edit, not
     a contract edit.
2. **X3:**
   - Add `decrypt`, `change-password`, `new-recovery-key`, `forget-key`, `forget-archive`,
     `repair-record`, `restore-key-file` and `verify -full` to the walked list.
   - Mark the row Partial until MA-39 walks them.
3. **X4:** reserve or drop the `encrypt` verb.
4. **X8:** record `decrypt`'s stdout/stderr split, or require the fix.
5. **X14:**
   - Bring CLI `decrypt` into scope: the plan is always shown, it is confirmed at a terminal, and a
     folder that may leave this computer needs its path typed back.
   - State the Desktop create's position.
   - Rule on single small files saved from a native dialog (the recovery file).
6. **X17:**
   - Add `internal/app/readme.go`, `internal/app/programnote.go` and `internal/keyring` to MA-622's
     swept set.
   - Rule whether "MailArchive Desktop" is an accepted surface name.
7. **X10 and X11** (wording only): name the Encryption card and its labels (Unlock, Lock, Make an
   unencrypted copy…) in the rows they extend.

## Not covered (honest)

- **Release program copies**: the Mac app zip, the Windows zip, the Linux programs and the
  `SHA256SUMS` check. Only the development self-copy existed, and network use was forbidden. "Skip
  the program download" was seen only in its idle refusal. The 134 MB figure was not checked.
- **Gatekeeper / quarantine on a kept Mac copy, and SmartScreen on a kept Windows copy**: the
  sandbox copies carried no quarantine attribute.
- **Desktop native dialogs**: "Make a new encrypted archive…", "Add another archive…" and "Open an
  archive folder…" were driven only through an osascript stand-in (x-walk). The create needed 7
  dialogs before the password, which is itself a friction point to re-walk by hand.
- **Real browser clicks**: Desktop was driven through its guarded API and read as HTML. B7 (Cancel)
  comes from reading the script.
- **Desktop actions not exercised**: "Put back the key file", forget carried to completion, Repair
  the record, the idle lock, the Remember toggle, change password and new recovery key carried
  through Desktop, Check integrity and Rebuild search index.
- **The remembered-key path in Desktop**, and a stale or wrong remembered key.
- **Untested CLI paths**: `restore-key-file -remembered` with a real remembered key;
  `restore-key-file` on a computer that never recorded the archive; a wrong secret at
  `change-password`, `new-recovery-key`, `restore-key-file`, `serve`, `search` and `extract`; a
  second Ctrl-C ("ends it at once"); a Ctrl-C during `decrypt`'s final writes; SIGKILL and
  stale-lock recovery; a busy archive during a running `verify -full`.
- **Drives**: a real unplugged `/Volumes` drive (simulated by renaming a folder), and the removable
  "asks twice" branch on macOS.
- **Other accounts**: an inheritor on another user account or uid (the archive files are 0600 and
  owned by the maker); an ext4 drive.
- **Size**: large archives and the memory budget (the largest was 20,000 messages in Desktop and
  6,000 at the CLI).
- **Other front ends**: keyed `graph` (Microsoft 365, no network) and keyed `extract`.
- **Linux**: not walked at all. Windows: see the separate lab report.
- **Test side effects to disclose**: two walkers' first `serve` runs (w1, w2) did not pass
  `-no-browser`, and one of them opened the real macOS browser on a loopback one-time link. No real
  home, config, keychain or LaunchAgent file was touched; the walkers checked. A stale `serve-*.json`
  was left in the w2 sandbox after a forced kill.

## Provenance of this synthesis

- **Register:** a synthesis report. It merges six walkers' raw results. I re-ran nothing.
- **Checked in the design by me:** design §5.1 step 4 (lines 649-651, the program-copies question
  on by default); K9 (lines 153, 1596, 1719); the §16 X1 list (lines 1762-1767); and R3 and R14 in
  `docs/design-encrypted-archives-phase3-program.md` (lines 274, 301).
- **Not checked by me:** every line number, exit code and quoted output is the walkers' report,
  unverified here. "Unsure" marks are kept where a walker gave one.
- **Smallest check before acting:** re-run B4's and B3's repro steps on `a531899` before fixing, so
  each fix ships with a prove-fail.
