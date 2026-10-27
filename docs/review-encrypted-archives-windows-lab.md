# Windows lab — encrypted archives, first release (P3-8 gate)

**Result: the lab found two high-severity security bugs and three medium product bugs. Windows is
not ready to ship.**
- **B1:** a swapped program is kept in the archive as its own program, with no warning.
- **B2:** a readable copy reaches a synced folder through a junction without the second ask.
- **The three medium bugs:** Ctrl-C at the hidden prompt; the secret-file remedy that does not fix
  the file; the case-L wording.

Five of the nine items were only partly covered. Two (OneDrive, and the reader in a browser) were
not covered at all.

B-numbers match the backlog in the friction review:
`/private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/p38/review-encrypted-archives-friction.md`

## Setup

| Item | Value |
|---|---|
| Guest | Parallels "Windows 11", build 10.0.26200, ARM64. All binaries are amd64, running under emulation. |
| Code | `a531899`, cross-compiled `GOOS=windows GOARCH=amd64` from a `git archive` copy. The guest's `\\Mac\Home` did not expose the repository to the user, so the tree was copied into the `\\Mac\winlab` share. |
| Accounts | Mostly the signed-in user, unelevated at medium integrity (`prlctl exec --current-user`). Some runs as SYSTEM, to get the symlink privilege and for comparison. |
| Console | `prlctl exec` has no console, so a ConPTY pseudo-console harness (`conpty.exe`, built in the sandbox) drove every prompt. |
| Date worked | 2026-10-27 |
| Scope | The design lists the P3-8 Windows lab as: the reader, the hidden prompt, the `-recovery-key-out` access list, the secret-file read check, and the P3-5 items (program copies). The items marked pending for this lab are junctions and folder-mounted volumes (SEC-04), OneDrive, and an external disk that reports "fixed". The P3-6 items (encrypting an existing archive) are dropped until P3-6 is built (owner ruling, 2026-10-26). |

## Lab items

| # | Item | Covered? | Result |
|---|---|---|---|
| 1 | Secret-file read check (`-password-file` / `-recovery-key-file`, the SDDL predicate on real files) | **yes** | **The control holds; its remedy text is a bug (B11).** Every unsafe file was refused before the password was used: an explicit grant to Users, an inherited Everyone grant, and a file owned by Administrators. But the printed `icacls … /inheritance:r /grant:r "%USERNAME%:F"` leaves the Users grant in place, so the same refusal repeats. It never changes a foreign owner; only `/setowner` fixes that. In PowerShell, `%USERNAME%` fails with error 1332. |
| 2 | `-recovery-key-out` / `-recovery-file-out` access list | **yes** | **Pass.** Both files are owned by the user, with a protected access list for the user only: `D:PAI(A;;FA;;;<user SID>)`. A key file inside an encrypted archive is refused, including through a junction. Minor (Type II): `RestrictToOwner` sets the access list but not the owner, so run as SYSTEM, MailArchive cannot read back its own key file. |
| 3 | Hidden console prompt (`tty.ReadSecret`), including Ctrl-C | **partial** (ConPTY, not a real window) | **Hiding works; Ctrl-C is a bug (B10).** The password (typed twice) and the retyped key were never echoed. The key showed on the alternate screen and was cleared after. Ctrl-C at "Choose a password", at `search`'s prompt and at a backup's prompt each printed `mailarchive: EOF` and exited 1. The design and X1 say "interrupted; nothing was changed" and exit 130. Nothing was written. A probe showed the console read returns EOF before the interrupt signal arrives. |
| 4 | Junctions and folder-mounted volumes (SEC-04) | **partial** | **K5 bypass, high (B2).** K5 is the rule that a plaintext copy to a synced or removable folder asks twice. `decrypt -dest …\Dropbox\notes` was refused (exit 1). The same folder through `mklink /J plain-name` copied the readable mail into Dropbox (exit 0). Cause: `filepath.EvalSymlinks` has not followed junctions since Go 1.23. A keyless backup through a junction into a recorded archive was correctly refused. **Case L (a recorded archive's parent swapped for a junction) is a wording bug (B12):** it is refused, but worded as "removed", with advice to run `forget-archive`; `status` says "no archive at …". Folder-mounted volumes were not mounted (that would change the guest's disks). By reasoning only: they carry the same reparse tag, so the link check catches them, but the removable check reads the drive letter (B31). |
| 5 | Removable or fixed external disk for the unencrypted copy | **partial** | A network share (`\\Mac\…`) was correctly refused: "is on a network share". There was no USB stick or external disk, and VM settings were not to be changed. The CD drive had no disc. By code reading, an external USB disk that reports DRIVE_FIXED is not asked twice. The design already lists this as pending (B31). |
| 6 | OneDrive placeholders (`util.IsPlaceholder`) | **no** | OneDrive is not installed or signed in in the guest. No Windows test calls the real check; the program-step tests inject a stand-in. |
| 7 | Program copies (P3-5) | **partial** | The self-copy was written (`mailarchive-self-windows-amd64.exe`, 36.9 MB). `mailarchivew.exe` without `mailarchive.exe` beside it wrote no copy and warned; with it beside, it wrote the copy and `verify` passed. **R14 is broken, high (B1).** R14 is the program-copies rule that a self-copy is written only if the file on disk is still the program that started. `cli5.cmd` swapped `bin\mailarchive.exe` for `whoami.exe` at the password prompt. The run exited 0 with no warning, and the archive's "own program" is byte-identical to `whoami.exe`. Cause: on Windows, `os.Stat` loads the file id lazily, so `os.SameFile` compares the new file with itself. The same root defeats R3's same-folder checks (`programstep.go:251`, `:349`) and `vault/restore.go:467` (there a later check still refuses). |
| 8 | The reader (`serve`, and Desktop in a browser) | **no** | Only `search` ran, as a keyed read with `-password-file` (2 matches). `serve`, the reader pages, Desktop, its native folder dialog for the recovery file, and the clipboard were not run on Windows. |
| 9 | The encrypted packages' tests run on Windows | **partial** | Results and classification below. |

## Test runs on Windows

| Package | As user | As SYSTEM | Notes |
|---|---|---|---|
| `internal/winsec` | pass | — | |
| `internal/util` | pass (36 pass, 3 skip) | pass | |
| `internal/vault` | 1 fail | 1 fail | `TestARestoreWritesNothingIntoAFolderPutInPlaceOfTheOnePrepared/another_folder` is refused, but by a later check rather than "no longer". Same root as B1. |
| `internal/keyring` | 93 pass / 4 fail / 4 skip | 4 fail | See the classification below. |
| `internal/encrecord` | pass | — | |
| `internal/encmark` | 1 fail | — | Needs `go list` (environmental). |
| `internal/app` (136 encrypted, program and decrypt tests, in two halves) | 27 fail | — | The full package was not run. |
| `internal/desktop` | **does not compile** | — | With a sandbox-only shim: 59 pass, 2 fail, 2 skip. |
| `cmd/mailarchive` | not run | — | Its TestMain builds the CLI with `go`, which the guest does not have. |

## Failures, classified

**Product (real bugs):**
- **B1** (high) — the lazy `SameFile` identity.
  - App: `TestTheStepReadsOnlyWhatItJudged` ("a swap was not judged unreadable").
  - Vault: the restore test above (refused anyway, but by the wrong check).
  - CLI: `cli5.cmd` end to end.
- **B2** (high) — the K5 junction bypass, CLI `cli2` C2 against C3.
- **B10** (medium) — Ctrl-C at a hidden prompt gives "EOF" and exit 1.
- **B11** (medium) — the `icacls` remedy does not fix the file.
- **B12** (medium) — the case-L wording advises `forget-archive` for an archive that still exists.
- **B30** (medium, test code) — `internal/desktop/keys_pass2_test.go` calls `syscall.Mkfifo` with no
  build tag, so no Desktop test compiles for Windows. CI is ubuntu-only, so nothing catches it.
- `verify -full` lists a missing file twice (low): `units/2a/…` and `units\2a\… (a message)`. The
  FID-04 de-duplication compares the two spellings as strings. Keyring:
  `TestVerifyFullReadsWhatTheKeylessCheckCannot`.
- The unencrypted copy's folder inherits its parent's access list on Windows, where Unix gets 0700
  (Type II, low; not measured with a shared parent). App: `TestAWholeCopyIsAPlaintextArchive` (the
  mode-bit half).

**Test portability (repo defects that hide real failures, not product bugs):**
- **The program stand-in is not named `mailarchive.exe`**, so the Windows sibling rule finds none:
  - about 10 app tests, for example `TestADevelopmentBuildKeepsItsOwnProgram`;
  - app `TestVerifyFullComparesTheProgramFolder`;
  - desktop `TestTheEncryptionCardSwitchesProgramCopies`;
  - keyring `TestVerifyFullComparesTheKeptCopies` (keyring's TestMain sets no stand-in).
- **The injected `syscall.EROFS` is not mapped on Windows** (4 tests):
  - `TestALockFreeCopyIsComparedWithItsPlan`
  - `TestACopyOffAReadOnlyMediumAndOneWithoutRoom`
  - `TestALockFreeCheckBesideAWriter`
  - `TestAnAmberThatOpenedNoMessageKeepsTheRedStanding`
- **`chmod`-based unreadable files** (4 tests):
  - `TestAnUnreadableArchiveCountsAsEncryptedForItsRecords`
  - `TestAFreshIndexThatCannotBeOpenedStillBacksUp`
  - `TestAGraphCaptureKeepsTheSealedIndexWhole`
  - `TestAScratchIndexThatDoesNotCloseCleanlyIsSaid`
- **Unix mode-bit assertions** (2 tests): `TestAWholeCopyIsAPlaintextArchive` and
  `TestALargeIndexIsWorkedOnInTheScratchFolder`.
- **A mid-copy folder swap:** `TestTheCopyIsMadeOnlyInTheFolderItsPlanChecked`. Probably Windows
  refuses the rename while the copy holds a handle inside the folder. This was inferred, not
  measured; it needs a second look before it is filed as portability rather than product.

**Environmental (this lab's setup):**
- **Symlink privilege**, not held by an unelevated user (4 tests). These pass as SYSTEM: keyring ×2,
  app link tests, and desktop `TestTheRecoveryFileIsReadBounded`.
- **SYSTEM's files are owned by Administrators**, so the keyring secret-file tests fail as SYSTEM
  ("not owned by you"). This is expected for SYSTEM. It does point at the minor `RestrictToOwner`
  owner gap.
- **No `go` in the guest**: the `cmd/mailarchive` TestMain, and encmark's
  `TestEveryProgramLinksTheSettingsGuard`.
- **The FIFO test** (`TestAHungRecoveryFileNeverFreezesTheDesktop`) is Unix-only. It was skipped
  through the shim.
- **amd64 under ARM64 emulation.** No native ARM64 build was run.

**Lab harness friction (not product):**
- `--current-user` was needed to avoid SYSTEM's temp-folder access lists.
- The repository was not visible through `\\Mac\Home`.
- `-test.run` patterns were mangled by quoting through `prlctl` and `cmd` (fixed with per-run `.cmd`
  files).
- The ConPTY child inherited `prlctl`'s pipes until `STARTF_USESTDHANDLES` was set.
- One batch line swallowed a closing quote.

Two small product findings came out of the harness work:
- The usage `mailarchive -encrypt -out NEWDIR [-input ...]` shows `-input` as optional, but without
  it (and without `-auto`) the run stops.
- Commands printed for the person carry doubled backslashes (`"C:\\Users\\…"`), from Go's `%q`.

## What remains for a human at the Windows console

Do these after B1, B2, B10, B11 and B30 are fixed. Use a real, signed-in session, and a native
build if one ships.

1. **The hidden prompt in a real console window**, typed by a person, in both conhost and Windows
   Terminal:
   - the password is not echoed;
   - the key screen is cleared after Enter;
   - pasting the recovery key works;
   - **Ctrl-C prints "interrupted; nothing was changed" and exits 130** (re-check B10).
2. **OneDrive**, which is not installed in the guest:
   - sign in, and make an online-only file and folder;
   - check that the program step treats a placeholder as described (`util.IsPlaceholder`);
   - check that a `decrypt -dest` inside OneDrive is called synced and asks twice;
   - check an archive kept inside OneDrive.
3. **Removable and external drives**:
   - a USB stick (DRIVE_REMOVABLE);
   - an external USB disk that reports DRIVE_FIXED (expected not to be asked twice until B31 is
     built);
   - a removable volume mounted inside a folder on C:.
4. **The secret-file remedy** (after B11): from cmd, PowerShell and an elevated shell, run exactly
   what the refusal prints and confirm the next run succeeds. Also measure an elevated shell's
   default file owner.
5. **The reader and Desktop in a browser on Windows**:
   - `serve` and its one-time link;
   - double-clicking `mailarchivew.exe`;
   - unlock and lock;
   - the native folder dialog for "Save the recovery file…";
   - the clipboard copy of the key;
   - Make an unencrypted copy.
6. **`-remember` with Windows Credential Manager**. It was deliberately avoided in this lab so the
   guest user's vault was not touched.
7. **Kept copies opened years later**: unzip the Windows copy from an archive, and see what
   SmartScreen and "Mark of the Web" say.
8. **Re-run the junction twins (B2, B12) and `cli5.cmd` (B1)** once fixed. Each fix ships with a
   prove-fail, and each owes an adversarial pass, because both are security-sensitive.

## Provenance

- **Register:** a lab report synthesised from the Windows walker's raw results. I re-ran nothing.
- **Not verified here:** the guest transcripts (`cli1`–`cli5`, the probe outputs, the package test
  logs). They are cited by the walker's names under `\\Mac\winlab\wintest\p38\` and the sandbox
  `windows-lab` folder.
- **Checked by me in the design:** R14 and R3 (`docs/design-encrypted-archives-phase3-program.md`,
  lines 274 and 301), and the pending Windows list (design phase 3, lines 1476, 1731-1735 and
  1759-1760).
