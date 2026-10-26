# Encrypted archives, Phase 3 — the user experience

Status: **revision 3, 2026-10-25.**
- **Review:** `docs/review-encrypted-archives-phase3.md` (verdict GO_WITH_CONDITIONS on revision
  1, 44 conditions).
- **Revision 2** folded the conditions and the owner's rulings.
- **Its fold verification and condition 44's re-review** (workflow `phase3-fold-verify`, run
  `wf_1b947358-d46`) found:
  - 17 conditions only partly carried;
  - 27 confirmed defects in the rewritten sections, one of them a blocker (R6-1: a planted
    conversion marker could make MailArchive delete a folder outside the archive);
  - both recorded in the review document's addendum.
- **What revision 3 changes:**
  - It fixes all of those in this document's sections.
  - It **moves encrypting an existing archive (§11) and the program copies (§13.2) into their
    own design documents.** Each is to be reviewed before its slice (P3-6, P3-5), with the
    findings recorded here as their requirements.
  - Both had now been through two review cycles, and the design-review gate names a third as
    the sign to split (lens 2).
- **Still owed before code:** a final verification of revision 3's §3, §4 and §9.4 (the
  sections P3-0 and P3-1 build).
- **Grounding:** written at commit `00e5fda` of branch `encrypted-archives`, from an eight-part
  read-only code map (workflow `phase3-map`, run `wf_c0478152-144`). Line references are to
  `00e5fda`.
- **Parent:** `docs/design-encrypted-archives.md`. Its amendments A1–A25 govern where this
  document is silent; this document adds A26–A34 (§15).

Phases 1, 2a and 2b are built. An encrypted archive can be written and read only by tests:
`app.Options.Key` is set nowhere in production, and the reachability meta-test (MA-726) keeps
it so. **Phase 3 is the first phase a person can reach.** Phase 4 (unattended scheduled
backups into an encrypted archive) follows. Nothing ships before Phase 4 is built and every
gate has passed (D5).

## 0. Scope

In Phase 3:
- making an encrypted archive (the CLI, the setup wizard, Desktop);
- opening one at the computer: the Desktop reader, "Open an archive folder", `serve`, `search`,
  `extract`, the go-back view, keyed `reindex`;
- backing one up by hand: the CLI at a terminal, and Desktop's Run now;
- the keys: change the password, a new recovery key, remember or forget the key, the recovery
  file;
- the keyed check (`verify -full`) and cancellable checks;
- a plaintext copy (the escape hatch);
- encrypting an existing archive (own design, §11);
- the break glass: the note (§13.1) and the program copies (own design, §13.2);
- this computer's record of encrypted archives (A15, A25);
- "continue from this older copy";
- the friction review and the UX-contract (X1–X17) walk.

Not in Phase 3:
- any backup into an encrypted archive without a person at the computer — scheduled,
  `-unattended`, or a script or cron job (O5); that is Phase 4;
- decrypt-in-place;
- re-keying the master key.

## 1. Owner rulings

Earlier:
- **R-E1:** one solution.
- **D1:** both key modes; "remember" is the default.
- **D2:** search is kept.
- **D5:** full build before ship.
- **C2:** names and structure are encrypted.
- **The break glass** (parent §1).
- **MA-662:** the archive list stays folder-only.
- **P2a-C14:** extended to settings writes.
- **A20:** uninstall keeps the remembered key.
- **The friction review and X walk** are deferred to Phase 3.

2026-10-25:
- **O1 — the program copies hold all four platforms** (about 134 MB per archive), downloaded
  once per version from the official release and checked against its published checksums.
  Offline, this computer's own program is copied, and the note says where to get the others.
- **O2 — encrypting an existing archive keeps the old unencrypted copy and asks before deleting
  it,** saying deletion cannot be guaranteed on SSDs, cloud folders or backups.
- **O3 — with "remember the key" on, opening the archive unlocks by itself** (Desktop reader,
  CLI).
- **O4 — "continue from this older copy" is offered.** It shows both dates, warns that mail
  backed up after the older copy is not in it, and says the next backup collects again what is
  still in the mailbox.
- **O5 — no scripted backups into an encrypted archive in Phase 3.** A keyed write needs
  Desktop or the command line at a foreground terminal; anything else is refused, naming
  Phase 4.
- **O6 — the recovery file is offered in the safer form.** It holds only the recovery entry;
  putting it back needs the recovery key and sets a new password and a new recovery key
  (§9.4).
- **O7 — published releases become unchangeable.** The release process stops replacing a
  published release's files (`gh release upload --clobber`, `release.yml:69-70`); a fix is a new
  version.

## 2. The implementer's calls (vetoable)

- **K1 — no password environment variable** (A26).
  - A secret comes from a file or a hidden prompt.
  - Another process of the same user can read a process's environment, and children inherit it.
- **K2 — the recovery key is entered again from where it was saved.**
  - Desktop and the wizard hide the key after it is shown, with a "Show again" button (in the
    wizard, an extra button on the re-entry dialog).
  - The CLI writes the key, waits for Enter, then erases those lines (or leaves the terminal's
    alternate screen) before asking; an empty answer shows the key again.
  - Paste is allowed. This proves the key was copied somewhere, not only seen; nothing stronger
    is claimed.
  - On a terminal that cannot erase lines, the CLI says so and the claim is weaker there.
  - `-recovery-key-out FILE` is an optional extra copy at a terminal. It does not replace the
    re-entry.
- **K3 — attended means a person is there.**
  - In Phase 3 a keyed write (backup, conversion, key change) runs only from Desktop or from a
    CLI whose stdin and stderr are a foreground terminal (O5).
  - Keyed reads may also use a password file or the remembered key without a terminal.
- **K4 — the Desktop's idle lock.**
  - An archive opened with a password locks after 30 idle minutes, and on Lock, an archive
    switch, or Shut down.
  - One opened from a remembered key re-opens by itself (O3).
- **K5 — a plaintext copy to a cloud-synced or removable folder asks twice;** it is not refused.
- **K6 — encrypting an existing archive is refused** when any listed file fails its check, or
  when an attachment file over the 1 GiB sealing ceiling is the only copy of its attachments.
- **K7 — no decrypt-in-place** (A30). The way back is a plaintext copy (§10), then "forget this
  encrypted archive here" (§4.6), then removing the folder.
- **K8 — the record survives uninstall,** like the remembered key. The Danger zone's separate,
  confirmed "forget" choice removes both.
- **K9 — downloads of a zip or `.eml` are allowed,** with one line saying the downloaded file is
  not encrypted.
- **K10 — `serve` of an encrypted archive refuses a non-loopback address.**
- **K11 — on Linux the standalone wizard does not create an encrypted archive.**
  - The pinned zenity passes dialog text on the command line (`entry_unix.go:13`,
    `msg_unix.go:8`).
  - Desktop's "Add another archive" runs the encryption step in the page instead, on every OS.
- **K12 — the recovery file holds only the recovery entry** (O6, §9.4).
- **K13 — an unlocked or remembered key may authorise "change the password" and "a new recovery
  key"** (`vault.KeySecret`, `store.go:234, 600, 622`).
  - It asks for a confirmation.
  - Beside the button it states that anyone using this computer's account could do the same
    (R-E2).
- **K14 — opening an archive this computer has not recorded asks before deriving above the floor
  cost,** under a global memory guard. Without a terminal such an open is refused, naming the
  cost. A hostile key file can ask for up to 1 GiB per attempt (A3's ceiling).

## 3. The shared core: one unlock package

### 3.1 The package and the hand-off

Every path that turns a secret into a key goes through one new package, `internal/keyring`. It is
the only production package that:
- calls `vault.PrepareCreate`, `Open`, `ChangePasswordAt`, `PrepareNewRecovery`,
  `PrepareRestore`, `Heal`, `RequireRetrievableKey`, `Remember` or `Forget`;
- sets `app.Options.Key`.

**No key leaves it.** It exports no accessor for the `*vault.Key`. Instead, `keyring` builds
everything that needs a key and hands over the result:

```go
type Unlocked struct{ /* key, id, path source, how opened; unexported */ }

func Create(out string, pw []byte, o CreateOptions) (*PendingCreate, error)
func Open(out string, s Secret, o OpenOptions) (*Unlocked, error)   // o.MayHeal, o.Write
func Remembered(out string, o OpenOptions) (*Unlocked, error)
func (u *Unlocked) Lease() (*Unlocked, error)  // an independent key copy for one job
func (u *Unlocked) Destroy()

// Hand-offs: keyring calls into app and server; app and server never import keyring.
func Apply(opts *app.Options, u *Unlocked)                   // a keyed run: Key, RecordedID, RecordedGen
func ReaderSource(u *Unlocked) (server.Source, error)        // the sealed reader source (§7.2)
func DecryptCopy(ctx context.Context, u *Unlocked, o app.DecryptCopyOptions) error  // §10
func VerifyFull(ctx context.Context, u *Unlocked, o app.VerifyOptions) (app.Report, error) // §8
```

The MA-726 allow-list grows by this one package. **Its acceptance is an operator gate at P3-1a**
(A32).

### 3.2 The lease rule

**No key is destroyed while another goroutine may use it.**
- Each job gets its own key: Run now, a plaintext copy, a conversion, `verify -full`, a keyed
  reindex, the reader.
  - `Lease` copies the key under the keyring's lock through `vault.Open(out, want,
    vault.KeySecret(k))` (`store.go:242-262`). This needs no key derivation and does not heal.
  - The job destroys its copy when it ends.
- Lock, the idle lock, an archive switch and Shut down destroy only the dashboard's copy.
- Shut down cancels jobs first.
- Owed test: an idle lock during Run now neither fails the run nor writes a pointer under a
  destroyed key.

### 3.3 Which key an open expects

- **A recorded path:** the open passes the recorded id as `want`.
- **An unrecorded path:**
  - With a password or recovery key: `want` is zero, and the opened vault's header MAC proves
    the key.
  - With a remembered key: the folder's claimed id only chooses which remembered key to try;
    `RequireRetrievableKey` with that id proves it by the header MAC.
  - **Adoption:** the path joins the record only when the open is for a write
    (`OpenOptions.Write`), or when the person chooses to remember the key. A read-only open of a
    handed-over copy records nothing.
- Amendment A34 to A15: a folder's id never serves as `want` unproven. A swap between two of the
  person's own encrypted archives stays in A5's conceded tier.
- **The Desktop's keyring** maps (opened vault id, path source) → the dashboard's `Unlocked`.

### 3.4 Secret sources

For the CLI, in this order:
1. **`-password-file` or `-recovery-key-file`.**
   - One reader, at most 4 KiB, stripping only one trailing LF or CRLF.
   - Unix: `job.ReadSecure`'s owner and mode checks.
   - Windows: the owner check, plus a new read-access check (`winsec.PathReadableByOthers`)
     that refuses a file other accounts can read. Today's check covers only write access
     (`job/ownercheck_windows.go:19-22`).
   - The path joins `Options.CredentialPaths`.
2. **The remembered key** (O3).
3. **A hidden prompt.** Only when stdin and stderr are terminals and the process is in the
   foreground.

Refusals:
- Without a terminal, a **read** is refused naming option 1.
- A **write** is refused naming Phase 4 (O5), whatever the source.

The verbs that take `-remember` are `export -encrypt`, `graph -encrypt`, and any keyed open.

There is no secret-valued flag and no environment variable (K1). A test walks every `FlagSet`
and fails on a flag whose name or usage marks a secret, except an explicit allow-list:
`-password-file`, `-recovery-key-file`, `-recovery-key-out`, `-client-secret-file`,
`-client-secret-env`, `-token-cache`.

### 3.5 The hidden prompt

`tty.ReadSecret`, built on `golang.org/x/sys` (termios on Unix, `SetConsoleMode` on Windows).
- No new dependency.
- The terminal is restored on every exit path: a deferred restore; SIGINT, SIGTERM, SIGHUP;
  SIGTSTP and SIGCONT.
- Tests: `ptytest` on darwin and linux; on Windows, a pseudo-console test or a signed-off lab
  step.

### 3.6 Heal and fork

- **`OpenOptions.MayHeal`.** The backup and the key verbs set it; `verify -full`, the reader and a
  plaintext copy leave it off.
  - A heal uses `KeySecret(key)`, never the password.
  - A failed heal is never fatal.
- **A fork opens successfully.** Its wording is built from which copy the secret opened (U12).

### 3.7 Cost and key lifetime

- **Cost.** One unlock is one or two Argon2id derivations of at least 256 MiB (A3).
  - The Desktop unlock is single-flight, and a global memory guard allows one derivation at a
    time.
  - The unlock time on linux-arm64 is measured and stated as a P3-1a deliverable.
- **Lifetime.** `Destroy` zeroes the key (MA-718). Decrypted text in Go's heap is not zeroed
  (R-E2).

## 4. This computer's record of encrypted archives (A15, A25; A27)

### 4.1 Where

- **macOS and Linux:** MailArchive's settings folder.
- **Windows:** the non-roaming per-computer folder (`instance.Dir`, `%LOCALAPPDATA%`).
- **The names:** `encrypted-archives.rec` and its mirror `encrypted-archives.rec.bak`. They are
  not `*.json` names, because uninstall deletes every `*.json` in the settings folder
  (`uninstall/config.go:97`), and a job named `encrypted-archives` would collide (`job.go:127-134`).
- **It is not** the archive list (MA-662), nor the in-archive schedule descriptor.
- **It survives uninstall** (K8). The uninstall plan says so.
- **The Windows cleanup script** (`packaging/mailarchive-desktop/Cleanup-CurrentUser.ps1:39-41`,
  run with `-RemoveLocalAppState`) deletes `%LOCALAPPDATA%\MailArchiveDesktop` and the record
  with it. P3-0 changes the script to keep `encrypted-archives.rec*` and its lock, unless it is
  run with an explicit `-ForgetEncryptedArchives`. If the record is lost anyway, it is the
  "record lost" case (§4.3).

### 4.2 The schema

The file holds:
- A format number, read first, from the file's first bytes, before any size limit applies.
- A sequence number. It rises on every write; the higher one wins between the two copies.
- A SHA-256 trailer over the rest. A copy whose trailer does not match counts as damaged.

Then, per archive id:
- **highest:** the highest generation this computer has seen committed for the id, with its run
  date. It only rises, and is never evicted. It is what "this computer made a newer backup on
  DATE-R" reports (§12).
- **paths:** each path this computer used for the id in a write or a remember (§3.3), cleaned,
  with its `state.PathSourceID` and when it was last seen. A15 enforcement uses them.
- **copy heads:** at most eight. Each holds:
  - a generation;
  - its pointer digest;
  - its run date;
  - the last path it was seen at;
  - when it was last seen.

  A copy is one line of history of the archive. Rotating drives at one mount path are two copies,
  with two heads.

The record does not keep the "remember" or "bundle" choices:
- The secret store governs remember: a key under `archive:<id>` is remembered.
- The archive's sealed header governs bundle (§13.2).

The record holds only what this computer must know without the key.

### 4.3 Reading and writing

- **Writes.** Owner-only, with `util.WriteFileAtomic0600` (it fsyncs the folder).
  - The primary is the commit point; the mirror follows.
  - Both copies valid but different: the higher sequence wins and the other is healed from it.
  - A copy with a newer format number is never healed over, repaired or rewritten. An older build
    keeps fields it does not know.
- **Concurrency.** Changes happen under `encrypted-archives.lock`, with a stated wait of 5
  seconds. A timeout means:
  - a create or an adoption is retried once, then told "MailArchive is busy updating its record
    of encrypted archives — try again";
  - a head update (`OnCommitted`) raises a WARN.
  - **Lock order:** the archive lock before the record lock, never the reverse.
- **Size.** The file is capped at 1 MiB.
  - Heads are evicted least-recently-seen first. The `highest` value is not a head and is never
    evicted.
  - A head's last-seen value is capped at the time of writing, and a future-dated value is
    corrected down.
  - Read-only opens of a matching copy update last-seen.
  - A write that would pass the cap evicts heads first. If still over (more than about 3,000
    archives), it refuses with its own message.
- **Disclosed beside K8:** MailArchive builds from before Phase 3 ignore the record.
- **A record that cannot be used** has three cases, each with its own wording:
  1. **Permissions or owner** (the secure reader refused a file readable by others): the exact
     `chmod 600` or `icacls` command. No repair is offered.
  2. **A newer format**: "this record was written by a newer MailArchive — upgrade".
  3. **Damaged** (both copies fail their trailer or parse): `mailarchive repair-record`, also in
     Desktop.

  In every case the record is never rewritten. Keyed writes are refused, and so are keyless
  writes into folders that hold MailArchive state or encryption residue. That stops plaintext
  backups on this computer too; the refusal is raised through the existing run-failure alerts.
- **Repair.**
  - It moves both copies aside (`encrypted-archives.rec.damaged-<date>`) and never overwrites
    them.
  - It rebuilds from whatever parses in them, plus each folder in the archive list that carries
    encryption markers (its claimed id is proven at its next keyed open).
  - It reports what it could not rebuild: heads reset (§12 asks once per copy again), and paths
    of archives not in the archive list.
- **A lost record.** It is detected per id, not by whether the file exists:
  - a folder in the archive list carries encryption markers but has no entry; or
  - a keyed open finds a remembered key for the folder's claimed id but no entry.

  Each raises a status WARN and is re-adopted at its next keyed open. The secret store cannot list
  its entries, and none is added.

### 4.4 When it is written

- **The id and path, right after `Commit` of a new key file, before the first backup.**
  - If that write fails, nothing more happens. The folder's own key file still makes every
    keyless writer refuse.
  - An adoption write (§3.3) is blocking in the same way.
- **The copy head, after every commit.**
  - `container.LoadOptions` gains `OnCommitted(gen, digest, runDate)`. It is called whenever the
    pointer rename landed, including when `Commit` then returns an inventory-write error
    (`store.go:879, 915-916`).
  - It replaces the head this run opened, or adds one (§4.7), and raises `highest`.
  - A failed write logs and raises a status/health WARN ("this computer's record of the archive
    could not be updated"). The run is not failed.

### 4.5 How it is enforced

- **One predicate.** A small new package, `internal/encmark`, holds `encmark.At(path)`: the
  folder's own markers (`vault.EncryptedMarker`, `EncryptedAtOrAbove`) **or** a recorded path,
  for the path and its parents.
  - app, schedule, health, archindex, wizard, desktop and keyring import it.
  - Every production caller goes through it: `failurekind.go` `encryptedAt`, `wizard/pause.go`,
    `health.go`, archindex, `desktop/localarchive.go`, `RefuseKeyless`, the last-run writer and
    lock verb (`run.go:996, 1058`), and `schedule.refuseEncrypted`.
  - A code-scan meta-test bans direct calls to the vault markers outside `encmark`.
- **The claim, conditioned:** this closes the A15 gap **for the paths this computer recorded**.
- **The refusal has three wordings.** `encmark` reports which case applies:
  1. **Key files missing beside sealed data:** "this folder holds an encrypted archive this
     computer made, but its key files are missing — restore the whole archive folder from a
     backup copy". The words ", or put back the recovery file" appear only once P3-1b has
     shipped.
  2. **Plaintext MailArchive files at a recorded path:** "this folder was an encrypted archive
     this computer made, and now holds unencrypted archive files — it may have been tampered
     with; restore the whole archive folder from a backup copy". **It never offers forget.**
  3. **No MailArchive file at all** (judged by `Lstat` on every reserved name): "this folder used
     to hold an encrypted archive this computer made; if you removed it on purpose, choose
     'forget this encrypted archive here'". The forget offer appears only once P3-1a has shipped.
- **What is live at P3-0:** the record is written, `encmark` refuses recorded paths, and wordings
  1 and 2 are shown without the later actions. Wording 3 shows "use MailArchive to forget it"
  only from P3-1a.
- **Owed test:** a recorded path stripped of its key files, `units/`, `chunks/` and
  `inventory/`, then replanted with a plaintext archive:
  - reads RED in status and Desktop;
  - every keyless writer refuses it;
  - its refusal holds no forget offer.

### 4.6 Forget this encrypted archive here

Per path, by default: `mailarchive forget-archive -out DIR`, or the Desktop row's menu.
- It removes that path from its id's list, after a confirmation that names the path.
- When other paths of the id remain, the confirmation lists them with when each was last seen,
  and says they stay protected.
- When it is the id's last path, a second confirmation offers to remove the whole entry (its
  heads too) and, ticked separately, its remembered key.
- It is offered as K7's last step, and from wording 3 only (§4.5).
- A new archive at a recorded path is refused until that path is forgotten.

### 4.7 Which generation a load passes

Let H be the id's `highest`.

- **A copy whose pointer digest matches a head:** pass that head's generation.
- **A copy above every head, matching none:** forward progress — a failed head write, a commit
  whose inventory then failed, or another computer backing up the same drive.
  - Open it without asking, passing H, which is lower than the copy's generation, so it is never
    refused.
  - On commit, replace the head whose last path is this path, or add one.
  - A notice says the record caught up.
- **A copy below H, matching no head:**
  - **Writes** go once through §12. Confirming adds a head, so a rotated drive asks once, then
    never again (while at most eight copies are live).
  - **Read-only opens** (reader, plaintext copy, `verify -full`) pass `RecordedGen` 0 and show
    §12's two dates as a notice. They are never refused for being older.
- **A matching copy whose head is exactly one generation ahead of its intact pointer** (an
  unfinished save, `store.go:538-542`):
  - Writes are refused until §12 is confirmed.
  - Read-only opens show the notice "this copy is one backup behind what this computer last saw —
    the newest backup's changes, including any removals, are not shown". The notice is raised as
    a status WARN. A plaintext copy made in that state asks first and records it in the copy's
    report.
- **Order.** P3-0 writes heads and `highest`, but loads pass 0 until P3-7 ships §12, so no
  legitimate restore is refused before the way out exists.
- **Owed tests:**
  - two copies rotated at one mount path ask once each, then never;
  - two computers taking turns on one drive never ask;
  - a read-only open of an older copy holding a crashed run's leftover units, on a read-only
    medium, succeeds.

## 5. Making an encrypted archive

### 5.1 The flow

1. **The folder.**
   - It must be new or empty.
   - It must not be at or inside an encrypted archive (`encmark`).
   - It must not be a recorded path.
   - It must not contain MailArchive's settings, scratch or credential folders.
   - A plaintext archive is sent to "encrypt this archive" (§11).
2. **The password,** twice, at least 12 characters, checked before any derivation. The hint:
   "Use at least 12 characters; a longer phrase is stronger. A copy of the archive made before
   you change the password keeps the old one."
3. **Remember the key** (on by default):
   - on: "Backups you start and opening the archive here will not ask for the password. Anyone
     using this computer's account can open it."
   - off: "Every backup and every opening asks for the password."
4. **The program copies** (on by default): "Keep a copy of MailArchive for Mac, Windows and Linux
   in the archive so it can be opened years from now (about 134 MB each time a new version
   updates it)."
5. **Create.** `keyring.Create`, under a progress indicator. The recovery key is shown once with
   the G4 warning in full:

   > Save this recovery key in your password manager now. If you lose both your password and
   > this key, the archive can never be opened — not by you, not by us. There is no reset.

   Once P3-1b has shipped, the recovery file is offered with it (§9.4).
6. **Confirm** (K2). Then, in order:
   1. `Commit`;
   2. the record (§4.4);
   3. Remember, when chosen;
   4. the first backup.

   Every path out before `Commit` calls `Discard`.
7. **"Finish setting up".** A key file committed but no first commit is shown on every front end
   as "this encrypted archive was created but has no backup yet — finish setting up" (unlock,
   then the first backup). For a folder holding a key file but no pointer, whose id already has
   heads here, the wording is instead "this folder holds only a copy of the archive's key file",
   not "finish setting up".

### 5.2 Front ends

- **CLI.**
  - `mailarchive export -encrypt -out DIR …` and `graph -encrypt`, at a foreground terminal (K3).
  - Refused with `-unattended`, `-manifest` and `-list`, and on a folder that is not empty. On a
    folder that only needs finishing, the message says to drop `-encrypt`.
  - `mailarchive encrypt` on an empty folder points to `export -encrypt`.
  - The recovery key is written to stderr (X8), then erased (K2).
  - `-recovery-key-out FILE` (optional, K2):
    - creates a new file only;
    - sets an explicit owner-only access list on Windows;
    - flushes the file and syncs its folder before `Commit`;
    - is refused at or inside any encrypted archive or the target folder;
    - warns when the access list cannot be set.
- **Desktop.**
  - The Encryption card (`sec-encryption`) and "Add another archive". `localArchiveFlow` gains an
    encryption hook, so the encryption step runs in the page between the zenity folder step and
    the backup, on every OS.
  - Secrets travel only in guarded POSTs (`GuardLocalPOST` plus CSRF) through `d.api`, which sets
    `no-store`.
  - The pending create is single-use, discarded after 10 minutes and at Shut down.
  - The recovery key is the only secret the page receives. It is shown with `textContent` and
    never logged.
- **Standalone wizard** (macOS, Windows).
  - After the folder step: "Encrypt this archive?" (default No), masked entry, the key in a
    copyable Entry, then re-entry with "Show again".
  - On Linux, choosing encryption stops before any backup, pointing to Desktop (K11).
  - Its decisions go behind a dialog interface so their order is tested without a display.

## 6. Backing up into an encrypted archive by hand

- **`app.Options` gains `RecordedID`.** The keyed preflight refuses a key whose id is not the
  recorded one (`encrypted.go:226` uses the key's own id today).
- **The CLI.** `export` and `graph` at a foreground terminal unlock (§3) instead of today's
  refusal (`main.go:173, 1216`). Without a terminal they are refused, naming Phase 4 (O5).
- **Desktop Run now** uses a lease (§3.2) through a new attended path.
  - `RunJobInProcess` sets `Unattended=true` today (`jobrun.go:77`).
  - When the archive is locked, Run now asks to unlock first.
- **Scheduled and `-unattended` runs stay refused** (A14).
- **The keyed pause (E6).**
  - Pause on an unlocked encrypted archive writes the reason into the sealed header through a
    keyed commit (P3-3).
  - On a locked one it writes the empty marker (MA-741).
- **Ridden in:**
  - **E2:** the three Outlook-export callers use `app.OutlookPSTDir` with the key (MA-738).
  - **E3:** a keyed run's log goes to the settings folder (MA-740).
  - **E5:** every settings write checks `encmark` (MA-741, P2a-C14).
  - **E7:** keyed `reindex` prunes, re-sealing the history chunks a redaction touches
    (`phase2b.md:295-296, 404`).
  - **E8:** the plaintext last-run record is read through `util.ReadRegularFile`.
  - **U16:** owner-facing text naming files an encrypted archive lacks gets encrypted variants
    (`phase2a.md:192-193`).
- **R37 rows.** Each new write path owes an engine-level test: recorded as encrypted, key files
  deleted, nothing written in plaintext. The paths are CLI create, Desktop create, Run now, the
  wizard, a plaintext copy, and (with its own design) a conversion.

## 7. Reading an encrypted archive

### 7.1 Where the key enters

The Desktop unlocks in the **dashboard**, never in the reader.
- The reader's origin serves untrusted mail, so it stays GET-only, and its route probe (CC15) is
  unchanged.
- The reader receives its source from `keyring.ReaderSource` (§3.1), built on a lease.
- With a remembered key it unlocks silently (O3).
- When locked, it shows a page explaining how to unlock, plus "Put back the key file" from P3-1b
  on (X12; keeps MA-731's 503).

**Open an archive folder…**, in Desktop and on the first-launch front door:
1. pick the folder;
2. unlock in the dashboard;
3. it opens read-only and is added to Your archives. A read-only open is not an adoption (§3.3).

The note spells out these clicks (§13.1).

### 7.2 The source split

`server.New(outDir, ix)` becomes `server.New(src Source, ix)`.
- **The plaintext source** wraps today's code unchanged (MA-81..84, R19, P9).
- **The sealed source:**
  - applies `allowedPath` and `fileAllowed` first;
  - maps `*.html` to the message unit, `*-attachments.zip` to the attachment unit, and `*.eml`
    to the original;
  - serves through `http.ServeContent` under `pinFileTypes` and the archive CSP;
  - never makes `units/`, `chunks/`, `inventory/` or `program/` reachable.
- **The note** is rendered from the program's own text, never read from disk.
- **Pages rendered on the fly.** `RenderFolder` and `RenderRoot` are split out of
  `pages.go:166-212`. Rendered pages are cached in memory per generation. The gap banner gets
  encrypted wording.
- **Go-back** reads the decrypted manifest and `Store.History()`. A damaged chunk stands as a
  barrier.

### 7.3 Loading without the lock

- A read-only `LoadManifest` variant writes nothing, runs no sweep, and never heals.
- **Freshness.** Peek the generation, load, peek again. On a change, load once more. On a second
  change: "a backup is running; try again in a moment". Afterwards, peek at most every two
  seconds and reload on a change.
- **A missing unit or chunk.** Peek again and reload once before reporting anything; never report
  damage for that case. The design claims recovery without false damage, not that such a file is
  never met.
- **A read-only medium** (`EROFS`, or the Windows write-protected error): the reader, a plaintext
  copy and `verify -full` run without the archive lock, peeking the generation before and after.
- **Owed test:** a load interleaved with a commit never refuses falsely.

### 7.4 The index and memory

- **The index loads eagerly,** under the reader's mutex.
- **Above the index budget** (256 MiB, `indexMemoryBudget`), the reader's scratch index lives in
  the per-user cache folder (A33): one file per holder, under an OS file lock, swept at every
  keyed open and at uninstall.
- **Search status.** With `IndexBehind`, a banner warns. A damaged index says search is
  unavailable.
- **Units are read whole** (A1), peaking at about twice the sealed size.
  - **Unit reads are budgeted at 2.25 GiB,** so a message at the 1 GiB attachment ceiling opens.
    Reads beyond the budget wait.
  - A single unit over the budget (possible: preserved originals have no ceiling) is refused in
    plain words: "this message is too large to open on this computer (N MB)".
  - **The whole-process peak** is then about 2.5 GiB (unit budget plus index budget), reached
    only while such a message is open. A key derivation (256 MiB) never runs at the same time,
    because of the global guard (§3.7).

### 7.5 The CLI

`serve`, `search` and `extract` unlock (§3) and use the sealed source.
- `serve` refuses a non-loopback `-addr` (K10).
- `search -paths` stays refused.
- `extract -dest` inside an encrypted archive stays refused.

## 8. Checking with the key

**`verify -full`** is tier 2. It is read-only, under the archive lock, or lock-free on a
read-only medium. It runs through `keyring.VerifyFull`:
1. Run tier 1.
2. Load the record; a refusal is an outcome.
3. Read every unit one at a time within the budget: size, digest, decrypt, opens under its own
   message.
4. Open both copies of every manifest chunk, the history and the index.
5. Cross-check the files the records name against the file list.
6. Name the damaged file and the part it belongs to.

Where tier 1 said `not_attested` for a damaged record copy, tier 2 names the copy and says
whether the other is whole. That answers the 2b-4 residual for record, index and history chunks
**when tier 2 is run** (U10). It writes only the last-verify record and never heals.

**On a read-only medium** that record cannot be written. The output says "the result could not be
saved in the archive (read-only)", the exit code is unchanged, and status keeps its previous
verify record.

**The record and status:**
- The last-verify allow-list gains `tier` (1 or 2), a closed value.
- A tier-1 result never clears or supersedes a tier-2 RED or its attention file; status keeps the
  last tier-2 verdict separately.

**Cancellable** (built in P3-0 for tier 1). Both tiers check a context between files. On Ctrl-C
or Desktop's Cancel:
- the check ends and releases the lock;
- it is recorded as `stopped` (a closed status value, also added in P3-0), carrying the previous
  verdict;
- it exits 130 (SIGINT) or 143 (SIGTERM).

**Exit codes (X1):**
- 0: intact;
- 2: not attested, or damaged;
- 1: refused or failed;
- 130/143: stopped.

**CON-01's keyed side** gets its own review (E9).

## 9. The keys after creation

The CLI's key verbs need a foreground terminal (K3); Desktop's card is attended by nature.

### 9.1 Change the password

`mailarchive change-password -out DIR`, or the card.
- **Authorised by:** the current password, the recovery key, or the unlocked or remembered key
  (K13).
- `ChangePasswordAt` takes the archive lock; while a backup holds it, the message says so.
- `ErrPartiallySaved` reads: "the new password is in effect; the second copy of the key file is
  rewritten at the next opening".

### 9.2 A new recovery key

`mailarchive new-recovery-key -out DIR`, or the card.
- Authorised the same way.
- `PrepareNewRecovery`, then show, hide and re-enter (K2), then `Commit`.
- On `ErrPartiallySaved` the new key is still shown.
- A new recovery file is offered with it.

### 9.3 Remember or forget

- The card's switch, `-remember` at an unlock, or `mailarchive forget-key -out DIR`.
- `vault.Remember` refuses a store inside an encrypted archive.
- Uninstall keeps keys and the record (A20, K8). The Danger zone's separate confirmed choice
  removes both.

### 9.4 The recovery file (O6, K12, A28) — P3-1b

- **What is saved.** Only the recovery entry: the archive id, the recovery salt and settings, and
  the recovery-wrapped master key. It is offered as text, beside the recovery key.
- **A stale file is refused.**
  - The sealed header keeps the digest of the current recovery entry, rewritten at every new
    recovery key.
  - A file from before a new recovery key does not match. It is refused with: "this recovery file
    belongs to an older recovery key; use the one saved with your current recovery key".
  - So an old recovery key with its old file cannot restore. Owed test: an old entry with its old
    key is refused.
- **Putting it back:**
  - `mailarchive restore-key-file -out DIR -from FILE|-`;
  - or "Put back the key file", by pasted text, on the card and the locked reader page.

  It is used only when both key files in the archive are missing or damaged. **It runs in two
  steps, like creation:**
  1. `vault.PrepareRestore(out, want, entry, recoveryKey, newPassword)`:
     - unwraps the master key with the recovery key;
     - proves it against this folder's sealed pointer and the envelope or recorded id;
     - checks the entry's digest against the sealed one;
     - prepares a fresh vault with the new password and a new recovery key.
  2. The new recovery key is shown, hidden and re-entered.
  3. `Commit` writes both copies, under the archive lock.

  This pair is the only change to Phase 1's frozen API.
- **What this guarantees:** every restore sets a new password and a new recovery key, and stale
  entries are refused, so neither a withdrawn password nor a withdrawn recovery key comes back
  through this path.
- **Disclosed:** whoever has both the current saved text and the current recovery key can open
  the archive, which is already true of the recovery key with the archive.

## 10. A plaintext copy (the escape hatch)

`mailarchive decrypt -out DIR -dest PARENT`, or the card's "Make an unencrypted copy". It runs
through `keyring.DecryptCopy`.

**Plan first** (X14): where, how large, the free space.
- `CheckDestination` checks the destination.
- The copy goes to a fresh `<name>-unencrypted-<date>`.

**A synced or removable destination asks twice** (K5).
- Desktop: two confirmations. CLI: the path typed back.
- `util.UnderCloudSync` treats everything under `~/Library/CloudStorage/` and
  `~/Library/Mobile Documents/` as synced, and covers iCloud "Desktop & Documents" or says "this
  folder may be synced". One test per provider.

**How.** A lease; the source's archive lock, or lock-free on a read-only medium (§7.3).
- For each record, one unit at a time within the budget: read the page, attachment and original
  units, check each against `Record.Fixity`, write at `Record.Path`.
- Then:
  - the manifest without `Unit`/`Sealed`, with `Fixity` kept;
  - the history;
  - `search.db` from the index image, or rebuilt;
  - the pages, report and plaintext README, by the plaintext run's code.
- In the one-behind state (§4.7) it asks first and records it in the copy's report.
- **Not carried:** the key file, the id, the remembered key, `program/`.

**Engine B is not the copier.** `copyexport` writes an export folder, not an archive (contrary to
parent C4). Its destination checks and progress pattern are reused.

**Phase 2c closed here.** An encrypted run over MA-329's golden inputs, decrypted to a plaintext
copy, matches the plaintext golden byte for byte under MA-329's own masks.
- Each item of parent §4.3 is mapped to a test.
- The test is proven to fail when `DecryptCopy` is mutated and when sealing is mutated.

## 11. Encrypting an existing archive — its own design (P3-6)

Revisions 1 and 2 designed this inline. Two review cycles found it the least settled part, with
the review's one remaining blocker. **It gets its own design document,
`docs/design-encrypted-archives-phase3-convert.md`, and its own pre-code review before P3-6 is
coded.**

These are settled and carry over:
- O2 — keep the old copy, ask before deleting;
- K6 — the refusals;
- K7 — no decrypt-in-place;
- records sealed from the archive, never re-exported (`export.SealRecord` factored from
  `sealKind`);
- a sibling workspace on the same volume;
- the 1 GiB ceiling;
- an existing schedule is a refusal;
- the plan shows the last plaintext verify and the count of records without fixity;
- encryption authenticates the archive as it stands then;
- Engine B is not the copier.

**Requirements the new design must meet** (from the review and the re-review; IDs in the review's
addendum):
1. **No folder is ever deleted or renamed on the strength of an unauthenticated marker** (R6-1,
   R7-01, a blocker):
   - a sibling is named by base name only, matching a fixed pattern carrying the conversion id,
     resolved only inside the original's parent, not a link, on the same volume, with a
     reciprocal marker;
   - a workspace is deleted only if it holds nothing but the encrypted-archive tree;
   - resume never runs by itself — Desktop start only reports an unfinished conversion, and every
     rollback, roll-forward and abandon is confirmed naming the exact folders;
   - markers are read with the regular-file reader, size-capped and strictly parsed (R6-10).
2. **A roll-forward is proven, not trusted.** The workspace's sealed header carries the
   conversion id, the original's change token, the record count and the fixity digest. Roll-forward
   opens the workspace with its key and matches them before any rename, and the §11 refusals are
   re-run (R6-2's mechanism).
3. **One lock spans the whole conversion, outside both trees.** Resume, cancel and abandon take
   it and the original's archive lock (R6-3, R7-02).
4. **The change token** is stored in both markers and re-checked after rename 1, under the lock
   re-taken at the renamed path, and at every resume. A mismatch rolls back to `copying` or seals
   the change, and O2 is never asked after a mismatch (R3-5, R7-10).
5. **Before rename 1, both markers and the record carry all three fixed names.**
   - The names are the original, the workspace, and the old copy with its date fixed.
   - The furthest stage wins.
   - No step at `verified` or later deletes a tree because the other is missing.
   - A failed rename 2 stays at `swapping` and is retried (R3-3, R7-03).
6. **The `swapped` stage's order:**
   1. the record, holding the old copy's path;
   2. the old marker is replaced by a note under a non-marker name;
   3. the new marker is removed last.

   The O2 question and the "old unencrypted copy" listing come from the record (R3-4, R6-11,
   R7-15).
7. **The pending record entry is defined.** It is a field on the new id's entry, with its
   `encmark` meaning. Every cancel, rollback and abandon removes the new id's entry, workspace
   path and remembered key. Workspace names are unique per conversion (R3-6, R7-04).
8. **Windows.** A failed rename 1 returns to `verified` and keeps the workspace. Renames get a
   folder-sized budget. The re-taken lock is released before O2. The delete is resumable (R3-7,
   R3-15).
9. **The "opens nothing" line** is given on every cancel, rollback and abandon, naming the
   attempt's date and its recovery file (R3-14). `archiveScratch(final, new id)` is deleted on
   each of them (condition 18).
10. **CLI verbs:** `mailarchive encrypt -resume` and `-abandon` (R7-15). The R37 row for the
    conversion path.

## 12. Continue from this older copy (O4; A25)

**When.** A **write** to a copy below H matching no head (§4.7), or to a matching copy whose head
is exactly one generation ahead (`ErrUnfinishedSave`, `store.go:538-542`). A copy below H with a
merge (unnamed files) is refused by the store as `ErrRestoredOlder` (`store.go:544-550`) and goes
through the same step. A copy above every head never comes here (§4.7).

**Steps:**
1. Load the copy with `RecordedGen` 0 to read its generation P and its last backup's date.
2. Show: "This copy is from a backup made on DATE-P. This computer made a newer backup on DATE-R.
   Mail backed up after DATE-P is not in this copy. The next backup collects again whatever is
   still in the mailbox." DATE-R is `highest`'s run date.
3. On confirm, under the archive lock, re-check P and add or replace a copy head at P for this
   copy.

**Notes:**
- Unnamed newer files stay (CRIT-3b).
- In-archive file-list refusals (I > P; FAT's I = P + 1) need a container-level "accept the
  record at P", which gets its own review with E9.
- **P3-7 ships this.** Only then do loads pass recorded generations. The skipped merge-restore
  test (`phase2b.md:238-239`) is written there, and its skip marker is removed when it passes.

## 13. The break glass (parent §1; O1, O7)

### 13.1 The note (P3-5)

`README.txt` at the root, always; an encrypted run already writes it (`readme.go:136-148`,
MA-737).

It says:
- what the folder is;
- that MailArchive opens it with the password or recovery key — for a copy found on its own, in
  Desktop: "Open an archive folder…", then pick this folder;
- that MailArchive can make a normal unencrypted copy;
- the official download page;
- **how to check a bundled copy:**
  1. open the official release page for the version named in the note;
  2. take the expected SHA-256 from the `SHA256SUMS` published there, not from the drive;
  3. compare it with:
     - Linux: `sha256sum <file>`;
     - macOS: `shasum -a 256 <file>`;
     - Windows: `(Get-FileHash <file> -Algorithm SHA256).Hash`.

  The `SHA256SUMS` on the drive catches accidental damage only, never a deliberate swap.
- **how to start each copy:** the Gatekeeper and SmartScreen steps; on Linux, copy the program to
  your home folder, then `chmod +x`;
- that the official download is to be preferred;
- once P3-1b ships, the recovery-file instruction.

MA-737's rule holds: no layout, file or table names. The wording owes the friction review.

**Tamper.** The note's SHA-256 is sealed in the header **whether or not the program copies are
kept**.
- A mismatch raises a WARN before a keyed run rewrites the note.
- The reader renders the note from the program's text (§7.2).

### 13.2 The program copies — its own design (P3-5)

Like §11, this moves to its own document, `docs/design-encrypted-archives-phase3-program.md`,
reviewed before P3-5 is coded. O7's change to the release process is made in P3-5.

These are settled and carry over:
- O1's four platforms, downloaded from the official release and checked against its
  `SHA256SUMS`;
- the `program/` folder;
- byte-identical release assets;
- `SHA256SUMS` written only as the release's own bytes;
- self-copies under their own names when offline;
- a cache under the per-user cache folder (`mailarchive/program-cache/<version>/`), outside
  `job.ScratchDir`;
- the setting changeable on the card;
- the bundle choice governed by the sealed header (§4.2);
- schedule creation from a bundled copy refused;
- the conditioned claim — the published checksums prove the copy is the file the release
  published, not who built the release, and hold because releases are unchangeable (O7).

**Requirements the new design must meet:**
1. **Every file written into `program/` has a sealed entry** (name, SHA-256, origin release or
   this-computer).
   - Self-copies are sealed with their local hash and marked unverified; no unsealed hash file is
     trusted.
   - At each keyed run:
     - a file with no sealed entry is removed with a WARN;
     - a file whose hash differs is rewritten, or removed when it cannot be rewritten;
     - an `incomplete` state never suppresses the check (R6-4).
2. **A refresh replaces release copies only with verified release copies of a higher version.**
   Self-copies are added beside them and never replace them. A development build never removes or
   replaces release copies (R3-8).
3. **All program-copy work runs after the backup's final commit and never fails it.**
   - It uses an intent commit, then the files under temporary names, then a landed commit.
   - Errors are WARNs naming the failure class.
   - It is skipped below a stated free-space margin.
   - It never rewrites from a cache that lacks the expected version (R3-12, R7-08, R7-09).
4. **Forward compatibility.** The keyless check and the tree grammar accept any regular file in
   `program/` matching a bounded generic pattern (never `-attachments.zip`). The keyed run judges
   against the sealed names. Older builds ignore unknown program metadata (R3-13).
5. **Files are opened with the regular-file reader**, with a size ceiling before hashing (R6-10).
6. **When a self-copy is written:** state every trigger (offline, a failed download by class, a
   development build), and list the same triggers in §14's disclosure. On Windows the self-copy is
   the console `mailarchive.exe`, under an `.exe` name (condition 32).
7. **The download:**
   - built-in repository coordinates and fixed release URLs;
   - HTTPS-only redirects to GitHub's hosts, at most 5 hops;
   - a 200 MB per-file ceiling;
   - proxy from the environment;
   - a separate cancellable cache task outside any archive lock, with resume and at most one
     attempt a day per version;
   - a release is only an exact `vMAJOR.MINOR.PATCH` with `vcs.modified=false`;
   - a cache pruning rule that does not need the key (the record keeps no version, so the keep
     rule is stated without one).

## 14. The leak budget and threat model, extended

- **The program copies** reveal the MailArchive version that last refreshed them. A self-copy
  also reveals this computer's operating system and processor type; its triggers are listed in
  the program-copies design.
- **The download** sends this computer's IP address and the version to GitHub, once per version,
  from the cache task.
- **A swapped program copy or note** can mislead someone, or steal a password from someone who
  runs it on another computer.
  - The note says to prefer the official download and how to check against the release page.
  - The next keyed run detects a swap after the fact.
  - The published checksums prove what the release published, not who made the release.
- **This computer's record** maps folder paths to archive ids, on this computer only, owner-only.
- **An unlocked Desktop** holds keys in memory. Another process of the same user is outside the
  dashboard's guard (X9). This is within R-E2.
- **The reader's scratch index** above the budget is plaintext in the cache folder while open, and
  swept after a crash (A33).
- **Downloaded attachments and originals** are plain files (K9).
- **A plaintext copy** is plaintext wherever it goes.
- **After encrypting an existing archive,** the old unencrypted copy stays readable until removed;
  deletion is best effort (O2).
- **Encryption authenticates an archive as it stands when encrypted.**

## 15. Amendments to the parent design

- **A26 — no password environment variable.**
- **A27 — the A15/A25 record.**
  - `encrypted-archives.rec` and its mirror, in the settings folder (macOS/Linux) or
    `%LOCALAPPDATA%` (Windows).
  - It holds a format number, a sequence number and a trailer; per id, `highest`, paths and up to
    eight copy heads (§4.2).
  - It survives uninstall and the Windows cleanup script.
  - It replaces "the archindex entry".
- **A28 — the recovery file is the recovery entry only.**
  - `vault.PrepareRestore` and `Commit` put it back with the recovery key, setting a new password
    and a new recovery key.
  - The sealed header's entry digest refuses stale files.
- **A29 — A12 refined in its own design** (§11), under the requirements listed there.
- **A30 — no decrypt-in-place.**
- **A31 — the break glass:**
  - the note, rendered by the reader from the program's text, its hash always sealed;
  - the program copies under their own design (§13.2);
  - releases unchangeable (O7).
- **A32 — one unlock package** (`internal/keyring`): the single MA-726 allow-list entry (operator
  gate). No key accessor leaves it, and the lease rule governs every key it hands out.
- **A33 — A13 amended.** The reader's above-budget scratch index lives in the per-user cache
  folder, one file per holder, under an OS lock, and is swept.
- **A34 — A15 amended.**
  - A folder's claimed id never serves as `want` unproven; for an unrecorded path it only selects
    which remembered key to try.
  - Adoption happens only for writes and remember.
  - One predicate (`encmark`) carries every "is this encrypted" question.

## 16. Slices, build order and gates

Each slice is built test-first with prove-fail and prove-pass, and owes its own adversarial pass.

| Slice | What |
|---|---|
| **P3-0** groundwork | the record (§4); `encmark` and its meta-test; `OnCommitted`; the Windows cleanup script kept the record; E3, E5, E8, U16; tier-1 cancellation with `stopped` and exit codes 130/143 |
| **P3-1a** keyring + CLI | `internal/keyring` with the lease rule and hand-offs; `tty.ReadSecret`; secret sources and the Windows read-access check; the no-secret-flag test; the linux-arm64 unlock-time measurement; `-encrypt` create; keyed `export`/`graph` at a terminal; `change-password`, `new-recovery-key`, `forget-key`, `forget-archive`, `repair-record`; E2. **Operator gate: the MA-726 allow-list entry.** |
| **P3-1b** recovery file | §9.4, `vault.PrepareRestore` and `Commit`, the sealed entry digest. Its own adversarial pass. |
| **P3-2a** reader | §7; CLI `serve`/`search`/`extract`; keyed `reindex` with E7 |
| **P3-2b** keyed check | `verify -full` (§8) |
| **P3-3** Desktop + wizard | the card, unlock and lock, Open an archive folder, create with the hook, Run now, the keyed pause, the key verbs, Put back the key file, forget, the uninstall plan's scratch rendering (MA-739), the wizard step |
| **P3-4** plaintext copy | §10, `UnderCloudSync`, the Phase 2c closure |
| **P3-5** break glass | the note (§13.1); its own program-copies design and review first, then build; O7 |
| **P3-6** encrypt existing | its own design and review first (§11), then build |
| **P3-7** older copy | §12; loads pass recorded generations; merge-restore un-skipped |
| **P3-8** gates | the friction review and X1–X17 walk; Windows lab: the reader, the hidden prompt, the `-recovery-key-out` access list, the secret-file read check, and the P3-5/P3-6 Windows items |

The X1 exit codes, declared per new verb:
- **`change-password`, `new-recovery-key`, `forget-key`, `forget-archive`, `repair-record`,
  `restore-key-file`, `decrypt`:** 0 done; 1 refused or failed.
- **`decrypt`, stopped by Ctrl-C:** 130.
- **`verify -full`:** as in §8.
- **`encrypt`** is declared in its own design.

**Gates owed by this document:**
- the final verification of revision 3's §3, §4 and §9.4 before P3-0 and P3-1 are coded;
- the two split-out designs, each with its own review before P3-5 and P3-6;
- an adversarial pass per slice;
- the MA-726 operator acceptance at P3-1a;
- the friction review and X walk at P3-8;
- E9's own review.

## 17. Catalog plan

**New MA rows from MA-768:**
- one per slice behaviour;
- an R37 row per new write path;
- `encmark` and its stripped-and-replanted test (with no forget offer);
- the no-secret-flag test;
- the lease rule;
- rotation, and two computers taking turns;
- stale recovery files refused;
- the Phase 2c golden closure.

**Amended rows:** MA-39 (generated from the verb table), MA-726, MA-731, MA-737, MA-743 (with
P3-5), MA-747, MA-765 (`tier`, `stopped`).

Rows are written with their slices.

## 18. Open questions

None blocks the final verification. The note's exact wording goes to the friction review. The
X-series rows for encryption are the operator's call.
