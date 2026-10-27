# Encrypted archives, Phase 3 — the user experience

Status: **revision 4, 2026-10-25.**

**The review history.**
- **Review:** `docs/review-encrypted-archives-phase3.md` (GO_WITH_CONDITIONS on revision 1, 44
  conditions).
- **Revision 2** folded those conditions.
- **Revision 3** fixed the fold check's findings and split out two parts that kept failing
  review: encrypting an existing archive (§11) and the program copies (§13.2).
- **The final check of revision 3** (workflow `phase3-final-verify`, run `wf_9f7e021b-51c`) found
  24 confirmed defects and 7 partial resolutions. All were in the record's rollback detection
  (copy heads, §4.7, §12), the recovery-file restore (§9.4) and the key hand-offs (§3). The
  review document's second addendum records them.

**Revision 4.**
- **The record's rollback detection (A25) gets its own design.** That is the copy heads, which
  generation a load passes, and "continue from this older copy". It is
  `docs/design-encrypted-archives-phase3-generations.md`, reviewed before P3-7. Like §11 and §13.2,
  it had now failed three cycles, the design-review gate's sign to split (lens 2).
- **The record P3-0 builds is therefore much smaller:** archive ids and the paths this computer
  used (A15) only.
- **The recovery-file restore drops the sealed digest,** which could not be kept current. It
  states plainly what a restore does and does not withdraw.
- **The remaining findings are fixed in place.**

**Revision 5** (same day). The last check of revision 4 (workflow `phase3-rev4-check`, run
`wf_7e81d863-fd3`) found:
- no blocker;
- 17 confirmed defects in §3, §4 and §9.4, five of them major;
- 4 partial resolutions and 10 checker problems.

Each came with an exact fix, and revision 5 applies those fixes as written (the review's third
addendum maps them). They are rule additions, not a redesign, so no fifth pre-code cycle is run.
The adversarial passes owed by P3-0 and P3-1 check the built code against them.

**P3-0's adversarial pass** (workflow run `wf_8b561d60-f39`, 2026-10-25/26) confirmed 31 of 33
findings and the critic added 5. The rules it changed are marked in place as *Amended after the
P3-0 adversarial pass*, in §4.3, §4.5, §6 (E5) and §8; the findings and what was done with each
are in `docs/review-encrypted-archives-phase3-p30-adversarial.md`.

**P3-1a is built** (2026-10-26; MA-776..MA-789). The operator accepted `internal/keyring` as the
MA-726 allow-list's one package-wide entry, and ruled for a cheap key cost in tests only
(`vault.UseTestCost`, MA-776). Two read-only additions to Phase 1's frozen vault API came with it:
`vault.CostFloor` (K14 asks before deriving above it) and `UseTestCost`, which panics outside a
test binary. What P3-1a decided in building is marked *Amended in P3-1a* in place (§3.7, §4.2,
§16).

**P3-1a's adversarial pass** (workflow run `wf_4c9f8705-682`, 2026-10-26) confirmed 36 of 40
findings and the critic added 10; every one is fixed but three stated residuals, under MA-790.
The findings and what was done are in `docs/review-encrypted-archives-phase3-p31a-adversarial.md`.
The rules it changed are marked *Amended after the P3-1a adversarial pass* in §3.4, §4.6 and
§5.2.

**Gates.**
- **Before code:** none left for P3-0 and P3-1. The three split-out designs are each reviewed
  before their slices.
- **Grounding:** commit `00e5fda` and the read-only code map (workflow `phase3-map`, run
  `wf_c0478152-144`); line references are to that commit.
- **Parent:** `docs/design-encrypted-archives.md`. A1–A25 govern where this document is silent;
  this document adds A26–A35 (§15).

Phases 1, 2a and 2b are built. An encrypted archive can be written and read only by tests:
`app.Options.Key` is set nowhere in production, and the reachability meta-test (MA-726) keeps it
so. **Phase 3 is the first phase a person can reach.** Phase 4 (unattended scheduled backups into
an encrypted archive) follows. Nothing ships before Phase 4 is built and every gate has passed
(D5).

## 0. Scope

In Phase 3:
- making an encrypted archive (the CLI, the setup wizard, Desktop);
- opening one at the computer: the Desktop reader, "Open an archive folder", `serve`, `search`,
  `extract`, the go-back view, keyed `reindex`;
- backing one up by hand: the CLI at a terminal, and Desktop's Run now;
- the keys: change the password, a new recovery key, remember or forget the key, the recovery
  file, rebuilding the key files;
- the keyed check (`verify -full`) and cancellable checks;
- a plaintext copy (the escape hatch);
- encrypting an existing archive (own design, §11);
- the break glass: the note (§13.1) and the program copies (own design, §13.2);
- this computer's record of encrypted archives (A15);
- rollback detection and "continue from this older copy" (A25; own design, §12);
- the friction review and the UX-contract (X1–X17) walk.

Not in Phase 3:
- any backup into an encrypted archive without a person at the computer (O5);
- decrypt-in-place;
- re-keying the master key.

## 1. Owner rulings

**Earlier:**
- R-E1: one solution.
- D1: both key modes, "remember" by default.
- D2: search is kept.
- D5: full build before ship.
- C2: names and structure are encrypted.
- The break glass (parent §1).
- MA-662: the archive list stays folder-only.
- P2a-C14: extended to settings writes.
- A20: uninstall keeps the remembered key.
- The friction review and X walk are deferred to Phase 3.

**2026-10-25:**
- **O1 — the program copies hold all four platforms** (about 134 MB per archive). They are
  downloaded once per version from the official release and checked against its published
  checksums. Offline, this computer's own program is copied, and the note says where to get the
  others.
- **O2 — encrypting an existing archive keeps the old unencrypted copy and asks before deleting
  it.** The question says that deletion cannot be guaranteed on SSDs, cloud folders or backups.
- **O3 — with "remember the key" on, opening the archive unlocks by itself.**
- **O4 — "continue from this older copy" is offered.** The confirmation shows both dates, warns
  about mail newer than the copy, and says the next backup collects again what is still in the
  mailbox.
- **O5 — no scripted backups into an encrypted archive in Phase 3.** A keyed write needs Desktop
  or the command line at a foreground terminal.
- **O6 — the recovery file is offered in the safer form.** It holds only the recovery entry.
  Putting it back needs the recovery key, and sets a new password and a new recovery key.
- **O7 — published releases become unchangeable.** The release process stops replacing a
  published release's files (`gh release upload --clobber`, `release.yml:69-70`).

## 2. The implementer's calls (vetoable)

- **K1 — no password environment variable** (A26).
- **K2 — the recovery key is entered again from where it was saved.**
  - Desktop and the wizard hide the key after it is shown, with "Show again". In the wizard this
    is an extra button on the re-entry dialog.
  - The CLI writes the key, waits for Enter, then erases those lines (or leaves the alternate
    screen) before asking. An empty answer shows the key again.
  - Paste is allowed. This proves the key was copied somewhere, not only seen; nothing stronger
    is claimed. On a terminal that cannot erase lines, the CLI says so.
  - `-recovery-key-out FILE` is an optional extra copy at a terminal. It does not replace the
    re-entry.
- **K3 — attended means a person is there.**
  - A keyed write (backup, conversion, key change) runs only from Desktop, or from a CLI whose
    stdin and stderr are a foreground terminal (O5).
  - Keyed reads may also use a password file or the remembered key without a terminal.
- **K4 — the Desktop's idle lock.**
  - An archive opened with a password locks after 30 idle minutes of dashboard activity, and on
    Lock, an archive switch, or Shut down.
  - Locking ends the reader too (§3.2).
  - One opened from a remembered key re-opens by itself (O3).
- **K5 — a plaintext copy to a cloud-synced or removable folder asks twice.**
- **K6 — encrypting an existing archive is refused** when any listed file fails its check, or
  when an attachment file over the 1 GiB sealing ceiling is the only copy of its attachments.
- **K7 — no decrypt-in-place** (A30). The way back is:
  1. make a plaintext copy;
  2. "forget this encrypted archive here";
  3. remove the folder.
- **K8 — the record survives uninstall,** like the remembered key. The Danger zone's separate,
  confirmed "forget" choice removes both.
- **K9 — downloads of a zip or `.eml` are allowed,** with one line saying the file is not
  encrypted.
- **K10 — `serve` of an encrypted archive refuses a non-loopback address.**
- **K11 — on Linux the standalone wizard does not create an encrypted archive** (zenity passes
  dialog text on the command line). Desktop's "Add another archive" does it in the page, on
  every OS.
- **K12 — the recovery file holds only the recovery entry** (O6).
- **K13 — an unlocked or remembered key may authorise "change the password" and "a new recovery
  key".** A confirmation is asked, and the R-E2 disclosure is shown.
- **K14 — opening an archive this computer has not recorded asks before deriving above the
  floor cost.** Without a terminal it is refused, naming the cost.
- **K15 — a remembered key may rebuild missing key files** (§9.5). **Owner ruling O8,
  2026-10-25: yes.** The remembered key is the
  master key (A10), so rebuilding from it adds no exposure beyond K13. Without this, a person
  who lost both key files and the recovery key would lose an archive this computer can still
  open.

## 3. The shared core: one unlock package

### 3.1 The package and the hand-offs

Every path that turns a secret into a key goes through one new package, `internal/keyring`.
- It is the only production package that calls `vault.PrepareCreate`, `Open`,
  `ChangePasswordAt`, `PrepareNewRecovery`, `PrepareRestore`, `Heal`, `RequireRetrievableKey`,
  `Remember` or `Forget`.
- It is the only one that sets `app.Options.Key`.
- **No key leaves it:** it exports no key accessor. It builds everything that needs a key.

```go
type Unlocked struct{ /* key, id, path source, how opened, refs, locked; unexported */ }

func Create(out string, pw []byte, o CreateOptions) (*PendingCreate, error)
func Open(out string, s Secret, o OpenOptions) (*Unlocked, error)   // o.MayHeal, o.Write
func Remembered(out string, o OpenOptions) (*Unlocked, error)
func (u *Unlocked) Lease() (*Unlocked, error)
func (u *Unlocked) Lock()                                           // §3.2
func (u *Unlocked) Destroy()

// Hand-offs: keyring calls into app and server; neither imports keyring.
func Apply(opts *app.Options, u *Unlocked)                          // a keyed run
func ReaderSource(u *Unlocked) (server.Source, error)               // §7.2
func DecryptCopy(ctx context.Context, u *Unlocked, o app.DecryptCopyOptions) error // §10
func VerifyFull(ctx context.Context, u *Unlocked, o app.VerifyOptions) (app.Report, error) // §8
func Heal(u *Unlocked) error                                        // §3.6; vault.Heal with KeySecret, inside keyring
func Reindex(ctx context.Context, u *Unlocked, o app.ReindexOptions) error // keyed reindex (E7)
func Pause(u *Unlocked, reason string) error                        // the keyed pause (E6)
// The conversion's hand-off is defined by its own design (§11, requirement 11), in this package.
```

`app.Reindex` and `wizard.PauseArchive` gain keyed variants that take what `keyring` passes.
Neither imports `keyring`.

The MA-726 allow-list grows by this one package. **Its acceptance is an operator gate at P3-1a**
(A32).

### 3.2 Leases, locking and the reader

- **A lease is an independent key** for one job: Run now, a plaintext copy, a conversion,
  `verify -full`, a keyed reindex.
  - It is copied through `vault.Open(out, want, vault.KeySecret(k))` (`store.go:242-262`), with
    no key derivation and no heal.
  - The job destroys it when it ends.
  - Leasing reads the key files, so it runs **outside** the keyring's lock. The keyring takes a
    reference on the `Unlocked` under the lock, along with its lock epoch, releases the lock,
    opens, then drops the reference. A slow disk never blocks Lock or another archive.
  - **After the open, leasing re-takes the lock.** If the `Unlocked` was locked meanwhile, or its
    epoch changed, the new key is destroyed and the lease fails. A lock that lands during a slow
    lease is therefore never undone.
  - Owed race test: Lock during a lease held open by a fault hook, after which the reader never
    serves decrypted content.
- **Lock, the idle lock, an archive switch and Shut down** mark the dashboard's `Unlocked` as
  locked. The last reference to go destroys its key. Shut down cancels jobs first.
- **The reader is not a job:** its source is ended by a lock.
  - A request holds the source's key **only while it decrypts a unit into its in-memory buffer**
    (units are read whole, A1). It holds it through a counted reference, never during the
    response write.
  - A lock flips the source's "locked" flag, so new requests get the locked page. It drops the
    rendered-page cache, and the lease is destroyed when the last decrypt in progress ends. Lock
    never waits on a client.
  - Each reader response has a write deadline. When it expires, the request's share of the
    unit-read budget is released, so a stalled download cannot hold memory or delay an unlock.
  - With a remembered key the reader re-opens by itself (O3).
- **Owed tests** (run with the race detector):
  - an idle lock during Run now neither fails the run nor writes under a destroyed key;
  - an idle lock during a reader request returns the locked page, never a damage message;
  - a client that stops reading partway through an attachment delays neither Lock nor an unlock.

### 3.3 Which key an open expects

- **A recorded path:** the open passes the recorded id as `want`.
- **An unrecorded path:**
  - With a password or recovery key: `want` is zero, and the header MAC proves the key.
  - With a remembered key: the folder's claimed id only chooses which remembered key to try;
    `RequireRetrievableKey` with that id proves it by the header MAC.
- **Adoption.** The path joins the record only when the open is for a write
  (`OpenOptions.Write`), or when the person chooses to remember the key. A read-only open records
  nothing.
  - A Desktop write job leasing from a dashboard unlock counts as a write: Run now at an
    unrecorded path adopts it, as a blocking write, before the run starts.
- A34: a folder's id never serves as `want` unproven. A swap between two of the person's own
  encrypted archives stays in A5's conceded tier.
- **The Desktop's keyring** maps (opened vault id, path source) → `Unlocked`.

### 3.4 Secret sources

For the CLI, in this order:
1. **`-password-file` or `-recovery-key-file`.** One descriptor-judged reader (the open
   discipline of `util.ReadSecureFile`), at most 4 KiB, stripping one trailing LF or CRLF.
   - Unix: the file's owner must be the effective user, and its mode must give no access to group
     or others.
   - Windows: the owner SID must equal the process token's user SID. A new
     `winsec.PathReadableByOthers` refuses any allow entry granting a read-class right to any SID
     other than the user, SYSTEM or Administrators (deny entries are never counted as safety).
     Unit tests use synthetic SDDL.
   - The path joins `Options.CredentialPaths`.
2. **The remembered key** (O3).
3. **A hidden prompt,** only at a foreground terminal.

Refusals without a terminal:
- A read is refused naming option 1.
- A write is refused naming Phase 4 (O5).

`-remember` is taken by `export -encrypt`, `graph -encrypt`, and any keyed open.

*Amended after the P3-1a adversarial pass:*
- A new password that reads as a recovery key is refused when it is set; at the prompt, what fails
  as a recovery key is tried as a password through a whole open, its own K14 question included
  (SEC-03, CRIT-10).
- A secret file with a UTF-8 byte-order mark reads; a UTF-16 one is refused with the remedy
  (CRIT-09). On Linux an access list beyond the mode is refused (SEC-06); on macOS it is a residual.
- `-recovery-key-file` reads the one line that is not blank and not a `#` comment, so the file
  `-recovery-key-out` writes reads back (SEC-02), and refuses other text without quoting it
  (SEC-07).
- A question asked inside an open (K14, "record it again") passes Ctrl-C on as an interruption
  (CON-08). Finishing an unrecorded archive offers Remember again, on by default (CON-07).

There is no secret-valued flag and no environment variable. A test walks every `FlagSet` against
an explicit allow-list: `-password-file`, `-recovery-key-file`, `-recovery-key-out`,
`-client-secret-file`, `-client-secret-env`, `-token-cache`.

### 3.5 The hidden prompt

`tty.ReadSecret` on `golang.org/x/sys`, with no new dependency.
- The terminal is restored on every exit path and on SIGINT, SIGTERM, SIGHUP, SIGTSTP and
  SIGCONT.
- Tested with `ptytest` on darwin and linux, plus a Windows pseudo-console test or a signed-off
  lab step.

### 3.6 Heal and fork

- **`OpenOptions.MayHeal`.** The CLI's keyed backups (`export`, `graph`) and the key verbs set it,
  healing before the run takes the archive lock. `verify -full`, the reader and a plaintext copy
  leave it off.
- **A heal** uses `KeySecret(key)`, never the password. A failed heal is never fatal.
- **The Desktop.**
  - Its unlock reports `NeedsHeal`.
  - Run now (and every Desktop write job) calls `keyring.Heal(lease)` after leasing and **before**
    the run takes the archive lock, since `vault.Heal` takes that lock itself.
  - While a heal is owed, status shows a WARN: "one copy of this archive's key file is damaged".
- **A fork opens.** Its wording is built from which copy the secret opened.
- **Owed test:** a damaged mirror is rewritten by a Desktop Run now.

### 3.7 Memory, cost and lifetime

- **One process-wide memory guard** covers both key derivations and the reader's unit reads
  (§7.4).
  - A derivation (up to 1 GiB under A3's ceiling) waits while unit reads hold their budget, and
    the reverse. Only one derivation runs at a time.
  - The process peak is therefore the larger of the two plus the index budget: about 2.5 GiB
    (§7.4).
  - The Desktop unlock is single-flight.
- **The linux-arm64 unlock time** is measured and stated in P3-1a. *Measured in P3-1a
  (2026-10-26):* in Docker's `golang:1.26` linux/arm64 on an Apple-silicon Mac, a new key file
  and one unlock at the shipped floor (256 MiB, time 3) took 1.33 s for three derivations, about
  0.44 s each. A small ARM board is slower; this is a fast linux-arm64, not a floor.
- **Key lifetime.** `Destroy` zeroes the key (MA-718). Decrypted text in Go's heap is not zeroed
  (R-E2).

## 4. This computer's record of encrypted archives (A15; A27)

### 4.1 Where

- **macOS and Linux:** MailArchive's settings folder.
- **Windows:** `%LOCALAPPDATA%` (`instance.Dir`, non-roaming).
- **The files:** `encrypted-archives.rec` and the mirror `encrypted-archives.rec.bak`. They are not
  `*.json` names: uninstall deletes `*.json` (`uninstall/config.go:97`), and a job name could
  collide (`job.go:127-134`).
- **It is not** the archive list (MA-662), nor anything inside the archive.
- **It survives uninstall** (K8), and the uninstall plan says so.
- **The Windows cleanup script** (`Cleanup-CurrentUser.ps1:39-41`) changes in P3-0 to keep the
  record and its lock unless run with `-ForgetEncryptedArchives`.

### 4.2 The schema

- **The header:**
  - a format number, read first from the file's first bytes, before any size limit;
  - a sequence number, rising on every write;
  - a SHA-256 trailer, so a copy whose trailer fails counts as damaged.
- **Per archive id:**
  - **paths:** each path this computer used for a write or a remember (§3.3). Each path holds:
    - its cleaned spelling, folded to one letter case only where `state.PathSourceID` folds
      (Windows, macOS);
    - its `PathSourceID`;
    - when it was recorded;
    - `unproven` when it was rebuilt by repair (§4.3).

    **A recorded path is never itself a link.** At create and adoption, an output folder that is
    a symbolic link, junction or mount point is recorded by its resolved target. A later
    refusal for "now a link" (§4.5) therefore always means a change.

    *Amended in P3-1a (the P3-0 pass's OUT-04, OUT-05, SEC-04):* every link on the way to the
    folder is resolved when it is recorded — a parent's as well as its own (`util.ResolveExisting`)
    — so no recorded path is reached through a link. A recorded spelling that later resolves
    elsewhere had a link put on its way, the folder's or a parent's, and is case L. An archive
    made through a linked output folder is recorded where it really is, so re-pointing the link
    takes no protection away. Windows junctions and folder-mounted volumes resolve the same way;
    that they read intact stays a Windows-lab item (MA-788).
  - **forgotten:** a tombstone, set when the person forgets the whole id (§4.6).
    - It only stops "lost record" warnings for that id's remembered key.
    - It never blocks adoption. A deliberate write or remember on a tombstoned id asks once ("you
      told MailArchive to forget this archive; record it again?"), then clears the tombstone and
      records the path.
  - **An id with zero paths is kept** as an entry, never dropped silently.
- **Nothing else.**
  - The generation and copy history this computer has seen (A25) is added by the generations
    design (§12), under a new format number.
  - "Remember" is governed by the secret store (a key under `archive:<id>`).
  - "Bundle" is governed by the archive's sealed header (§13.2).

**Forward compatibility.** Any field whose meaning can refuse or protect raises the format number,
so an older build reads the record as "newer — upgrade" and fails closed. Only purely
informational fields may be added under the same number; an older build keeps them unread. The
split-out designs (§11, §12) are bound by this rule.

### 4.3 Reading and writing

- **Writes.**
  - Owner-only, with `util.WriteFileAtomic0600`.
  - The primary is the commit point, and the mirror follows.
  - Two valid copies that differ: the higher sequence wins, and the other is healed from it,
    under the record lock after a re-read.
  - A copy that fails its trailer is moved aside (`.damaged-<date>-<n>`) before anything is written
    over it. If its readable header shows a higher sequence than the surviving copy, the heal says
    "a recent change to this record may have been lost".
  - A copy with a newer format number, even beside an older one, makes the whole record "newer"
    (case 2 below). It is never healed over, repaired or rewritten.
- **Concurrency.** Changes happen under `encrypted-archives.lock`, through a new
  `lockfile.AcquireWait(path, verb, wait)` with a 5-second wait.
  - A timeout during create or adoption is told: "MailArchive is busy updating its record of
    encrypted archives — finish setting up when it is free" (§5.1 step 7).
  - **Lock order:** the archive lock before the record lock, never the reverse.
- **Size.** Capped at 1 MiB, which is about 5,000 archives. A write that would pass the cap
  refuses with its own message.
- **Disclosed beside K8:** MailArchive builds from before Phase 3 ignore the record.
- **A record that cannot be used.** Three cases, each with its own wording:
  1. **Permissions or owner:** the exact `chmod 600` or `icacls` command. No repair is offered.
  2. **A newer format:** "this record was written by a newer MailArchive — upgrade".
  3. **Damaged:** `mailarchive repair-record`, also in Desktop. Until P3-1a ships, the wording
     names the file and says the record needs repair.

  **In every case** the record is never rewritten, and callers behave the same way:

  | Caller | While the record cannot be used |
  |---|---|
  | keyless write into a folder holding MailArchive state or encryption residue | refused, and raised through the existing run-failure alerts |
  | keyed read (reader, plaintext copy, `verify -full`) | proceeds with `want` zero, with a WARN — the escape hatch survives |
  | keyed write, create, adoption | refused, before `PrepareCreate` |

  Disclosed beside K8: plaintext backups on this computer stop until the record is usable. That
  includes running a MailArchive older than the record's format, for example after a downgrade
  across the generations design's format change.

  *Amended after the P3-0 adversarial pass:*
  - **A fourth case, unreadable** (REG-03): the settings folder cannot be found, or the record
    cannot be looked for (permission denied on the folder, a file where a folder belongs). It
    names the folder and the system's error, never "damaged". It refuses like the other three.
  - **Permissions includes the owner** (SEC-03): a copy not owned by this account is case 1, on
    every system, using the instance files' owner check.
  - **"Newer" is read from the magic word and the format number alone** (CRIT-01), whatever
    follows on the header line and however long it is, so a later format can grow its header.
  - **A damaged copy is moved aside only by a write that succeeds** (CON-03): the change, the
    size cap and the lock are checked first, so a failed write leaves the copy, and the "may have
    been lost" notice, for the next write.
  - **"Busy" only when another holder has the lock** (CON-05); any other lock failure names the
    folder and the error.
- **Repair.**
  - It scans first, without the record lock.
  - Under the lock, it re-checks that the record is still unusable and unchanged since the scan,
    and refuses if not.
  - It moves both copies aside (`encrypted-archives.rec.damaged-<date>-<n>`) and never overwrites
    them.
  - It takes paths and tombstones from whatever still parses. Every rebuilt path is marked
    **unproven**:
    - It counts **only** toward refusing keyless writes, with its own wording (§4.5 case 0).
    - It never serves as `want` or `RecordedID`, never decides case 1 or 2, and never satisfies
      §9.4's no-pointer exception.
    - It becomes proven at a write or remember open whose opened id equals its id. Otherwise the
      unproven entry is dropped and the path is adopted under the opened id.
  - It reports what it could not rebuild.
- **A lost record** (it lacks an id that should be there).
  - **Detected** at a keyed open that finds a remembered key for the folder's claimed id, with no
    entry and no tombstone.
  - **Recorded durably:** the open writes the id into `encrypted-archives.lost` beside the record
    (owner-only, a closed list of ids). `health.Assess` reads it and shows a status WARN.
  - **Cleared** for an id when that id is re-adopted, which happens only on a write or remember
    open (§3.3), or when the person acknowledges it in Desktop.
  - The secret store cannot list its entries, and no listing is added.
- **The lock call.** `lockfile.AcquireWait` takes a caller-chosen wait. The record lock uses 5
  seconds.

### 4.4 When it is written

- **The id and path, right after `Commit` of a new key file, before the first backup.** If that
  write fails, nothing more happens; the folder's own key file still makes every keyless writer
  refuse.
- **An adoption** (§3.3) is blocking in the same way.
- **Nothing in P3-0 writes after a backup's commit.** Generation recording belongs to the
  generations design (§12).

### 4.5 How it is enforced

**One predicate.** A small new package, `internal/encmark`, holds `encmark.At(path)`: the folder's
own markers (`vault.EncryptedMarker`, `EncryptedAtOrAbove`), **or** a recorded or unproven path.
- **Matching.** For the path and each parent, it compares both the cleaned spelling as given and
  the resolved `PathSourceID` against both stored forms. Letter case is folded only where
  `PathSourceID` folds it (§4.2).
- **A link where a folder belongs.** It `Lstat`s each recorded path the query touches.
  - A recorded path that is now a symbolic link, a junction or a mount point is refused (case L
    below).
  - On Windows this is judged by reparse tag: tags with the name-surrogate bit are refused, and
    cloud-file tags (OneDrive placeholders) are not. Go's mode bits alone cannot tell these
    apart.
- **Who uses it.**
  - app, schedule, health, archindex, wizard, desktop and keyring import it.
  - Every production caller goes through it: `failurekind.go` `encryptedAt`, `wizard/pause.go`,
    `health.go`, archindex, `desktop/localarchive.go`, `RefuseKeyless`, the last-run writer and
    lock verb (`run.go:996, 1058`), and `schedule.refuseEncrypted`.
  - A code-scan meta-test bans direct calls to the vault markers outside `encmark`.
- **The claim, conditioned:** this closes the A15 gap **for the paths this computer recorded**.

**Two closed name lists, applied first.**
- **Reserved names.** Every file and folder an encrypted archive itself writes:
  - the key files and envelope;
  - the pointer pair, `inventory/`, `units/`, `chunks/`;
  - `README.txt` and `program/`;
  - `.mailarchive.lock`;
  - the plaintext last-run, last-verify, run-history, source-streak and paused records;
  - the attention files;
  - the conversion marker;
  - the temp-name patterns.

  The list lives in `encmark` and is checked against the tree grammar (MA-743) by a test.
- **Operating-system litter,** a closed list per OS, ignored by every case:
  - macOS: `.DS_Store`, `._*`, `.Spotlight-V100`, `.fseventsd`, `.Trashes`, `.TemporaryItems`;
  - Windows: `Thumbs.db`, `desktop.ini`, `$RECYCLE.BIN`, `System Volume Information`;
  - Linux: `lost+found`.

  *Amended after the P3-0 adversarial pass (CRIT-04):* the list above missed names systems drop
  at drive roots, so a healthy archive there read as tampered. Added, matched without regard to
  letter case: macOS `.DocumentRevisions-V100`, `.VolumeIcon.icns`, `.apdisk`, `.localized`,
  `Icon\r`, `.com.apple.timemachine.donotpresent`, `.PKInstallSandboxManager*`; Linux
  `.Trash-*`; Windows `found.NNN` (three digits).

**The refusal: six cases, decided in this order.**
- **L — the recorded path is now a link or mount point:** "this folder was an encrypted archive
  this computer made, and is now a link to somewhere else — it may have been tampered with". Its
  forget offer comes with the strong confirmation (§4.6).
- **0 — an unproven path** (rebuilt by repair): "this computer once recorded an encrypted archive
  here, and its record was repaired; open it with its password to confirm". No forget offer until
  it is proven or dropped.
- **2 — any entry that is neither reserved nor litter, at a recorded path** (a plaintext
  MailArchive file, or anything else), whatever sealed data or key files remain: "this folder was
  an encrypted archive this computer made, and now holds other files — it may have been tampered
  with, or another drive may now use this path; restore the whole archive folder from a backup
  copy". Its forget offer comes with the strong confirmation (§4.6).
- **1 — key files present that name another id, at a recorded path:** "this folder holds files
  naming a different encrypted archive than the one this computer recorded here — it may have
  been tampered with". The id is only a claim, so forget gets the same strong confirmation as
  case 2.
- **3 — key files missing or damaged** (absent, or present but not opening with any secret this
  computer holds) **beside sealed data:**
  - at a recorded path: "this folder holds an encrypted archive this computer made, but its key
    files are missing or damaged — restore the whole archive folder from a backup copy". From
    P3-1b it adds "or put back the recovery file", and "or rebuild them from this computer's
    remembered key" when one exists;
  - at an unrecorded path: "this folder holds an encrypted archive whose key files are missing or
    damaged".
- **4 — at a recorded path, nothing but reserved non-data names and litter, or the folder
  absent:** "this folder used to hold an encrypted archive this computer made; if you removed it
  on purpose, choose 'forget this encrypted archive here'".

Before P3-1a ships `forget-archive`, every forget offer reads "MailArchive cannot write here".
P3-0 ships the cases with that interim wording.

*Amended after the P3-0 adversarial pass:*
- **Matching is by every spelling of the folder** (OUT-01, OUT-02, SEC-01). Lookup walks the
  parents of the path as given and of its resolved form. Where `PathSourceID` folds, the spelling
  is put in Unicode composed form (NFC) and fully case-folded, not only lower-cased. A recorded
  path that still exists also matches by file identity (`os.SameFile`).
- **Case L judges the recorded spelling, not the query** (FID-01). A folder reached by its
  recorded target or source id is not "now a link", so an archive made through a linked output
  folder stays usable.
- **A plaintext manifest at a recorded path is case 2** (OUT-06), even though its name is
  reserved. **Newer key files naming the recorded id are not case 3** (FID-02): a newer vault is
  newer, never damaged (A7).
- **Verify refuses a recorded folder in any case but intact** (CRIT-02), in that case's words,
  before any lock or write.
- **The label, the minimising and the refusal are three questions** (CON-01, REG-02, CRIT-03).
  While the record cannot be used: a keyless write or check into a folder holding MailArchive
  files is refused with the record's problem and remedy (`encmark.ErrRecordUnusable`, failure
  kind `encrypted_record_unusable`), never run as an encrypted check; the user-facing label
  (status, Desktop, the archive list) answers from the folder's own markers only; and whether a
  refused run's record is minimised stays fail-closed (`encmark.MustMinimise`), so no source path
  is written into a folder that may be an encrypted archive.
- **Deferred to P3-1a, when recording starts** (nothing records a path in P3-0): a Windows
  junction or folder-mounted volume must be recorded by its final path, so an intact archive there
  is not case L (SEC-04); a link recorded under its own spelling and re-pointed later, and a
  recorded path whose parent is swapped for a link, must read case L, not plaintext or case 4
  (OUT-04, OUT-05). The second needs §4.2's "a recorded path is never itself a link" revisited.
  The Windows rows are pending in MA-775, to run on the Windows lab VM.

**Owed tests:**
- A recorded path stripped of its key files, `units/`, `chunks/` and `inventory/`, then replanted
  with a plaintext archive, reads RED in status and Desktop, every keyless writer refuses it, and
  it gets case 2. The same with a planted foreign key file added still gets case 2.
- A recorded path replaced by a link to an empty sibling, and by a Windows junction: every keyless
  writer refuses, with case L. A recorded path inside a cloud-tagged (OneDrive) folder is not
  refused.
- Key files deleted beside sealed data, plus `.DS_Store`, `._units`, `Thumbs.db` or
  `System Volume Information`: case 3. A folder holding only `.DS_Store`: case 4.
- An archive created through a symlinked output folder stays usable.
- On Windows and macOS, a recorded path reached through another letter case matches. On Linux,
  `/data/arch` does not match a recorded `/data/Arch`. Through a symlinked parent it matches on
  every OS.

### 4.6 Forget this encrypted archive here

`mailarchive forget-archive -out DIR`, or the Desktop row's menu.
- **Order.** Every confirmation is asked first. Then it takes the archive lock (when the folder is
  present), then the record lock, re-reads the state, refuses if anything changed, and writes. No
  lock is held across a prompt.
- **Exact entry.** It removes only the exact stored entry, and the confirmation shows the stored
  spelling.
- **Keys first.** When keys are forgotten too, they are deleted first, then the tombstone or
  record is written. A crash in between leaves the keys still findable.

- **Per path, by default.** It removes that path from its id's list, after a confirmation that:
  - names the path;
  - lists the id's other paths with when each was recorded, saying they stay protected;
  - says other copies of this archive that used this same folder path, such as another drive
    mounted at the same place, lose this computer's protection at this path too.
- **From cases L, 1 and 2** (the strong confirmation), it first says the folder may have been
  tampered with, then asks for the path to be typed back.
- **The last path.** When it is the id's last path, a second confirmation offers to forget the
  whole id. That sets the tombstone and drops the paths, and, ticked separately, forgets its
  remembered key.
- **The Danger zone's forget** removes the remembered keys of every id in the record, tombstones
  included. A key whose id was never in the record cannot be found (the store cannot list), and
  the plan says so.
- **It is offered** as K7's last step, and from refusal cases L, 1, 2 and 4.
- **The Windows cleanup script's `-ForgetEncryptedArchives`** deletes every
  `mailarchive:archive:*` credential first, using the script's existing `cmdkey` parse, and then
  the record. Credential Manager can list entries; the cross-platform store cannot.
- **A new archive at a recorded path** is refused until that path is forgotten.

*Amended after the P3-1a adversarial pass:*
- **Order:** under the locks the folder's case and the record are re-read; only then is a
  remembered key forgotten, then the record written (CON-05, FID-01). A folder whose archive lock
  another run holds is refused, "try again when it is free" (FID-02).
- **An unproven path whose key files are gone** (CRIT-02; kept by the owner, 2026-10-26) can be
  forgotten, with the strong confirmation: nothing could ever prove it, and §4.5's case 0 now says
  how to forget it instead of "open it with its password".
- A removed archive typed through a linked parent folder is its own folder (CRIT-06).
- A recorded folder replaced by a link to another recorded archive matches as itself, in case L
  (OUT-01).
- **A backup at a terminal may unlock an unproven path whose archive is whole**, which proves it
  (§4.3); `verify` still refuses it (REG-02, CRIT-03).

## 5. Making an encrypted archive

### 5.1 The flow

1. **The folder** must be:
   - new or empty;
   - not at or inside an encrypted archive (`encmark`);
   - not a recorded path;
   - without MailArchive's settings, scratch or credential folders.

   A plaintext archive is sent to "encrypt this archive" (§11).
2. **The password,** typed twice, at least 12 characters, checked before any derivation, with the
   hint: "Use at least 12 characters; a longer phrase is stronger. A copy of the archive made
   before you change the password keeps the old one."
3. **Remember the key** (on by default):
   - on: "Backups you start and opening the archive here will not ask for the password. Anyone
     using this computer's account can open it."
   - off: "Every backup and every opening asks for the password."
4. **The program copies** (on by default): "Keep a copy of MailArchive for Mac, Windows and Linux
   in the archive so it can be opened years from now (about 134 MB each time a new version
   updates it)."
5. **Create.** `keyring.Create`, under a progress indicator. The recovery key is shown once, with
   the G4 warning in full:

   > Save this recovery key in your password manager now. If you lose both your password and
   > this key, the archive can never be opened — not by you, not by us. There is no reset.

   From P3-1b, the recovery file is offered with it.
6. **Confirm** (K2), then:
   1. `Commit`;
   2. the record (§4.4);
   3. Remember, when chosen;
   4. the first backup.

   Every path out before `Commit` calls `Discard`.
7. **"Finish setting up".** A key file committed but no first commit is shown everywhere as "this
   encrypted archive was created but has no backup yet — finish setting up". Finishing:
   1. unlock;
   2. offer Remember again (on by default);
   3. record the path as an adoption;
   4. run the first backup.

   A folder holding only a key file, for an id this computer already recorded at another path,
   says "this folder holds only a copy of the archive's key file" instead.

### 5.2 Front ends

- **CLI.**
  - `mailarchive export -encrypt -out DIR …` and `graph -encrypt`, at a foreground terminal.
  - Refused with `-unattended`, `-manifest` and `-list`, and on a folder that is not empty. On a
    folder that only needs finishing, the refusal says to drop `-encrypt`.
  - `mailarchive encrypt` on an empty folder points to `export -encrypt`.
  - The recovery key goes to stderr, then is erased (K2).
  - `-recovery-key-out FILE`:
    - creates a new file only;
    - sets an owner-only access list on Windows;
    - is flushed, with its folder synced, before `Commit`;
    - is refused at or inside any encrypted archive or the target folder;
    - warns when the access list cannot be set.

    *Amended after the P3-1a adversarial pass (SEC-01, SEC-09, CON-03, CRIT-04):* it is judged
    before anything is asked, and written only after the key was entered again, just before
    `Commit`: made empty, restricted to the user, then filled. If it cannot be restricted it is
    removed and the run refused, not warned: a recovery key is never left where others may read
    it. When the key does not take effect, it is overwritten and removed, and the person is told
    the key is NOT in effect and the earlier one still opens. When the archive was created but
    not recorded (`keyring.ErrCreatedNotRecorded`), it is kept and named.
  - `-encrypt` judges the folder before asking for a password (REG-03).
- **Desktop.**
  - The Encryption card (`sec-encryption`), and "Add another archive" through an encryption hook
    in `localArchiveFlow`, on every OS.
  - Secrets travel only in guarded POSTs (`GuardLocalPOST` plus CSRF) through `d.api`
    (`no-store`).
  - The pending create is single-use, discarded after 10 minutes and at Shut down.
  - The recovery key is the only secret the page receives. It is shown with `textContent` and
    never logged.
- **Standalone wizard** (macOS, Windows).
  - "Encrypt this archive?" (default No), masked entry, the key in a copyable Entry, then
    re-entry with "Show again".
  - On Linux, choosing encryption stops before any backup and points to Desktop.
  - The decisions go behind a dialog interface, so they are tested without a display.

## 6. Backing up into an encrypted archive by hand

- **`app.Options` gains `RecordedID`.** The keyed preflight refuses a key whose id is not the
  recorded one (`encrypted.go:226` today uses the key's own id).
- **The CLI.** `export` and `graph` at a foreground terminal unlock (§3) in place of today's
  refusal (`main.go:173, 1216`). Without a terminal they are refused, naming Phase 4.
- **Desktop Run now** uses a lease through a new attended path (`RunJobInProcess` sets
  `Unattended=true` today, `jobrun.go:77`), after the heal of §3.6.
- **Scheduled and `-unattended` runs stay refused** (A14).
- **The keyed pause (E6, P3-3).** Pause on an unlocked encrypted archive writes the reason into
  the sealed header through a keyed commit. On a locked one it writes the empty marker.
- **Ridden in:**
  - **E2:** the Outlook-export callers use `app.OutlookPSTDir` with the key.
  - **E3:** a keyed run's log goes to the settings folder.
  - **E5:** every settings write checks `encmark`. *Amended after the P3-0 adversarial pass
    (OUT-03, SEC-02):* "every" names, beside Desktop settings, the Microsoft 365 settings, stored
    secrets, the sign-in cache and the archive list, also: the job files, the Desktop instance
    file, the wizard's last-archive breadcrumb, the pre-sync record, the Desktop settings
    `.unreadable` copy and this computer's record itself. Packages `encmark` cannot be imported
    into get an init-installed hook.
  - **E7:** keyed `reindex` prunes, re-sealing the history chunks a redaction touches.
    *Owner ruling, 2026-10-26 ("Rebuild only"):* on an encrypted archive `reindex` only rebuilds
    the search index; it never removes a message. A sealed unit that is missing is damage, never
    a deletion: an encrypted archive has no per-message file a person deletes to redact, so
    pruning on a missing unit would turn a disk or sync fault into a silent, permanent deletion.
    Removing a message for good gets its own explicit command and its own design, later; the
    history re-sealing E7 describes moves there.
  - **E8:** the plaintext last-run record is read with `util.ReadRegularFile`.
  - **U16:** encrypted variants of owner-facing text that names files an encrypted archive lacks.
- **R37 rows.** Each new write path owes an engine-level test: recorded as encrypted, key files
  deleted, nothing written in plaintext. The paths are CLI create, Desktop create, Run now, the
  wizard, and a plaintext copy; the conversion's row comes with its own design.

## 7. Reading an encrypted archive

### 7.1 Where the key enters

The Desktop unlocks in the **dashboard**, never in the reader.
- The reader's origin serves untrusted mail, so it stays GET-only. Its route probe (CC15) asserts
  that it has no POST route.
- The reader gets its source from `keyring.ReaderSource` (§3.1).
- With a remembered key it unlocks silently (O3).
- When locked, it shows a page that says how to unlock, with a link that opens the dashboard's
  Encryption card. Every form, "Put back the key file" included, lives only on the dashboard
  (X12; keeps MA-731's 503).

**Open an archive folder…**, in Desktop and on the first-launch front door:
1. pick the folder;
2. unlock in the dashboard;
3. it opens read-only and joins Your archives.

A read-only open is not an adoption (§3.3). The note spells out these clicks.

### 7.2 The source split

`server.New(outDir, ix)` becomes `server.New(src Source, ix)`.
- **The plaintext source** wraps today's code unchanged (MA-81..84, R19, P9).
- **The sealed source:**
  - applies `allowedPath` and `fileAllowed` first;
  - maps `*.html` to the message unit, `*-attachments.zip` to the attachment unit and `*.eml` to
    the original;
  - serves through `http.ServeContent` under `pinFileTypes` and the archive CSP;
  - never makes `units/`, `chunks/`, `inventory/` or `program/` reachable.
- **The note** is rendered from the program's own text.
- **Rendered pages.** `RenderFolder` and `RenderRoot` come out of `pages.go:166-212`. They are
  cached in memory per generation and dropped at a lock. The gap banner gets encrypted wording.
- **Go-back** reads the decrypted manifest and `Store.History()`.

### 7.3 Loading without the lock

- A read-only `LoadManifest` variant writes nothing, runs no sweep, and never heals.
- **Freshness.** Peek, load, then peek again. On a change, load once more; on a second change, "a
  backup is running; try again in a moment". After that, peek at most every two seconds and
  reload on a change.
- **A missing unit or chunk.** Peek again and reload once before reporting. Never report damage
  for it.
- **A read-only medium** (`EROFS`, or the Windows write-protected error): the reader, a plaintext
  copy and `verify -full` run without the archive lock, peeking before and after.
- **Which generation a read passes.** In Phase 3 before P3-7, every load passes `RecordedGen` 0.
  That turns off only the recorded-generation rules (`store.go:538-553`). The in-archive
  file-list rules (`store.go:526-536`) still run on every load, including read-only ones, so a
  file list one generation ahead is still `ErrUnfinishedSave`, and the tests expect that. The
  generations design decides the value from P3-7 on.
- **Owed test:** a load interleaved with a commit never refuses falsely.

### 7.4 The index and memory

- **The index loads eagerly** under the reader's mutex.
- **Above the index budget** (256 MiB, `indexMemoryBudget`), the reader's scratch index lives in
  the per-user cache folder (A33): one file per holder, under an OS lock, swept at every keyed
  open and at uninstall.
- **With `IndexBehind`,** a banner says search may be missing messages. A damaged index says
  search is unavailable.
- **Units are read whole** (A1), peaking at about twice the sealed size.
  - **Unit reads share a 2.25 GiB budget** under the memory guard (§3.7), so a message at the
    1 GiB attachment ceiling opens. Reads beyond the budget wait.
  - A single unit over the budget is refused in plain words: "this message is too large to open
    on this computer (N MB)".
  - **Process peak:** about 2.5 GiB, while such a message is open.

### 7.5 The CLI

`serve`, `search` and `extract` unlock and use the sealed source.
- `serve` refuses a non-loopback `-addr`.
- `search -paths` stays refused.
- `extract -dest` inside an encrypted archive stays refused.

*Built in P3-2a (2026-10-26; MA-794..797).* What the build decided, beyond the text above:
- **The source.** `server.Serve(src)` takes a `server.Source` (`UseIndex`, `Files`, `GoBack`,
  `LockedNote`); `server.New(dir, ix)` is the plaintext folder's, unchanged. The sealed source is
  `keyring.Reader`, so no key leaves keyring; `server.SealedFiles` keeps the HTTP rules (the name
  rules, type pins, `ServeContent`, the write deadline) and asks keyring only for a path's bytes.
- **GET-only** is a wrapper that answers any other method with 405, for every reader, plaintext
  included.
- **The reader's key.** It reads through the `Unlocked` it is given, taking a counted reference
  only to load, to name a unit and to open it; the ciphertext is read without it
  (`Units.Locate`, `export.ReadSealedAt`, `Units.Open`). Desktop's lease and its own lock flag
  come with P3-3.
- **Freshness.** A peek of the primary pointer (`container.PeekFile`) at most every two seconds; a
  unit or chunk missed loads the record again once, then a unit still gone is 404, never damage.
- **Present** for go-back is a record with a page unit: an encrypted archive's messages are never
  removed by deleting a file (the owner's ruling on E7).
- **Pages** are `pages.Render`, rendered once per loaded generation; the root names no README and
  no report, and the gap banner says to run `status`. `README.txt` is 404 until the note (P3-5).
- **The memory guard** is `internal/memguard`: the keyring's derivation turn and the unit reads'
  2.25 GiB budget; a unit read reserves twice its sealed size.
- **The index** loads eagerly; above 256 MiB into the reader's own scratch file,
  `index/reader-<pid>-<n>.db`, one per load, removed when its view closes, a gone reader's swept by
  the next keyed open; uninstall owns the shape (MA-759 amended). An index behind is a note on the
  search page and on `search`'s stderr; none, or damaged, is "search is unavailable" naming
  `reindex -rebuild`.
- **The CLI's read unlock** (`unlockForRead`) takes a secret file, the remembered key, or the
  prompt; with none and no terminal it refuses before anything else is judged. `extract` keeps the
  archive's `extract` lock. Keyed `reindex` is `-rebuild` only, at a terminal (a write).
- **Not built here:** the Desktop's unlock (P3-3); read-only media without the archive lock for
  extract (it takes the lock, as a plaintext extract does).

*Amended after the P3-2a adversarial pass (2026-10-26; MA-798,
review-encrypted-archives-phase3-p32a-adversarial):*
- **Ending a reader.** `Reader.Close`, which `serve` and `search` call as they end, waits (within
  ten seconds) for a load in progress and the requests using the view, so a scratch index's file
  is gone before the process ends. `Lock` stays immediate. Locking the open a reader reads through
  locks the reader too. A load that finishes after a lock is closed, never served.
- **The scratch index's lock.** A reader holds an OS lock on a lock file beside its index,
  `reader-<pid>-<n>.db.lock` (the index's own file carries SQLite's locks), taken before the index
  is made. The sweep removes an index whose lock file it can hold, or that has none, whatever pid
  its name carries. Uninstall owns both names.
- **The write deadline renews with each write**, fifteen seconds by default: a download that
  makes progress runs as long as it needs; one whose client stops reading ends within the
  deadline and gives back its memory, so it holds back neither an unlock nor Lock.
- **What is missing is never damage.** An index chunk gone while the record holds still makes
  search "unavailable — part of the search index is missing"; the reader stays open. A history
  chunk gone is a partial timeline. A unit gone is loaded for again only when the record moved
  (a peek), and a unit whose bytes are not its record's never. The record pointer's peek covers
  both copies, so one planted copy never stops a read the other serves.
- **No usable index.** The folder pages are built from the index; without one they, and search,
  say so and name `reindex -rebuild`. Each message still opens by its own link.
- **The API** builds its answer while the index is in use and writes it after.
- **Keyed `reindex -rebuild`** stops between messages at the first Ctrl-C or SIGTERM, saving
  nothing (exit 130 or 143); a second signal ends it at once.

## 8. Checking with the key

**`verify -full`** (tier 2, through `keyring.VerifyFull`) is read-only. It runs under the archive
lock, or lock-free on a read-only medium:
1. tier 1;
2. load the record — a refusal is an outcome;
3. every unit, one at a time within the budget;
4. both copies of every manifest chunk, the history and the index;
5. cross-check the files the records name against the file list;
6. name the damaged file and its part.

It answers the 2b-4 residual for record, index and history chunks **when tier 2 is run** (U10).
It writes only the last-verify record and never heals.

**On a read-only medium** that record cannot be written. The output says "the result could not be
saved in the archive (read-only)", the exit code is unchanged, and status keeps its previous
record.

**The record and status:**
- The last-verify allow-list gains `tier` (1 or 2), a closed value.
- A tier-1 result never clears or supersedes a tier-2 RED or its attention file.

**Cancellable** (tier 1 in P3-0): a context is checked between files. On Ctrl-C or Cancel:
- the lock is released;
- the check is recorded as `stopped` (a closed status value, added in P3-0), carrying the previous
  verdict;
- it exits 130 (SIGINT) or 143 (SIGTERM).

*Amended after the P3-0 adversarial pass:* a stopped check walks no folder after the stop
(CON-06). For staleness, status judges a stopped check by the last finished verdict it carries, or
as no check at all when it carries none, never by when the stop started (CON-02). Status's summary
shows it as stopped, with that verdict beside it (CON-04).

**Exit codes (X1):**
- 0: intact;
- 2: not attested, or damaged;
- 1: refused or failed;
- 130/143: stopped.

**CON-01's keyed side** gets its own review (E9).

## 9. The keys after creation

The CLI's key verbs need a foreground terminal; Desktop's card is attended by nature.

### 9.1 Change the password

`mailarchive change-password -out DIR`, or the card.
- **Authorised by:** the current password, the recovery key, or the unlocked or remembered key
  (K13).
- `ChangePasswordAt` takes the archive lock.
- `ErrPartiallySaved` reads: "the new password is in effect; the second copy of the key file is
  rewritten at the next opening".

### 9.2 A new recovery key

`mailarchive new-recovery-key -out DIR`, or the card.
- Authorised the same way.
- `PrepareNewRecovery`, then show, hide and re-enter (K2), then `Commit`.
- On `ErrPartiallySaved` the key is still shown.
- From P3-1b, a new recovery file is offered with it, with: "destroy recovery files saved with
  your earlier recovery key — together with that key they still open this archive" (§9.4).
  *Amended after the P3-1b pass (CRIT-01):* that sentence is said only when the new recovery file
  was saved. Otherwise the person is told to keep the earlier pair, which is then the only way to
  put back lost key files.

### 9.3 Remember or forget

The card's switch, `-remember` at an unlock, or `mailarchive forget-key -out DIR`.
- `vault.Remember` refuses a store inside an encrypted archive.
- Uninstall keeps keys and the record (A20, K8).

### 9.4 The recovery file (O6, K12, A28) — P3-1b

**What is saved:** only the recovery entry — the archive id, the recovery salt and settings, and
the recovery-wrapped master key. It is offered as text beside the recovery key.

**Putting it back.** The person may use it only when **no copy of the key file opens with the
secret presented**: each copy is absent, fails to parse, or parses but does not authenticate under
the master key the restore proves. It is refused instead when a copy:
- is from a newer MailArchive (`ErrNewerVault`: "upgrade MailArchive");
- names another archive;
- cannot be read because of an I/O error ("the key file could not be read — check the drive",
  never treated as damage).

There are two ways to put it back:
- `mailarchive restore-key-file -out DIR -from FILE`;
- "Put back the key file" on the dashboard's Encryption card, by pasted text.

At the CLI the file must be a regular file. Stdin is the terminal, used for the prompts.

It runs in two steps, like creation.

1. **`vault.PrepareRestore(out, want, src RestoreSource, newPassword)`.** The source is one of:
   - `RecoveryEntrySource{entry, recoveryKey}` (this section);
   - `RememberedSource{store, claimed}` (§9.5). Vault loads the remembered key from the store by
     the folder's claimed id, inside vault, so no key leaves before the proof.

   It then:
   - **proves the master key against this folder's sealed pointer.** Vault reads the pointer's
     frozen header itself and opens it with the key (`Key.OpenPointer`), because `container`
     imports `vault`, not the reverse. When the folder has no authentic pointer, it is refused
     unless the path is recorded (proven, not unproven) and the id equals the recorded id. The
     envelope's claim alone is never accepted (A34).
   - **prepares a fresh key file** with the new password and a new recovery key, at generation
     `max(now in Unix seconds, 1 + the highest generation any copy on disk claims)`:
     - it is refused when the clock reads earlier than this build's release date ("this computer's
       clock is wrong");
     - the generation is capped below 2^62;
     - `WithPassword` and `WithNewRecovery` refuse to overflow it.

     A key file put back from an old backup therefore never outranks the restored one, and a heal
     rewrites it from the new one.
2. **The new recovery key** is shown, hidden and re-entered.
3. **`Commit`**, under the archive lock:
   - re-reads both copies and re-runs this section's preconditions;
   - re-proves the key against the current pointer, then writes both copies and re-parses what it
     wrote;
   - refuses with `ErrConflict` (a new prepare is needed) if anything changed since the prepare.

**This pair, with its two sources, is the only change to Phase 1's frozen API.**

*Built in P3-1b (2026-10-26; MA-791, MA-792):* the pair, `vault.ParseRecoveryFile` and
`Vault.RecoveryFile`, and — with the owner's go-ahead — `vault.OpenWithin`, which closes the P3-1a
pass's OUT-04: K14's cost limit is applied by the vault to the bytes it derives from. The vault reads
the record pointer's frozen header itself; a container test pins it against `container.Seal`.
The build date the clock is checked against is a constant set when the code was written
(2026-10-26). Commit's "re-run the preconditions" is a byte-for-byte comparison of both key-file
copies and both record pointers with what was prepared. The CLI is `restore-key-file -out DIR
-from FILE | -remembered`, with `-recovery-file-out` on it, on `-encrypt` and on
`new-recovery-key`; Desktop's "Put back the key file" stays with P3-3. P3-1b owes its adversarial
pass.

*Amended after the P3-1b adversarial pass (2026-10-26; MA-793,
review-encrypted-archives-phase3-p31b-adversarial):*
- **A damaged pointer blocks.** When a pointer file is present but neither copy opens with the
  key, the restore is refused, at a recorded path too: the code cannot tell damage from another
  archive's record. The refusal names both, and says to restore the record files from a backup of
  the folder first (CRY-06). "No authentic pointer" above means no pointer file at all.
- **A folder with no pointer needs its key files to vouch.** There the only evidence is the key
  file: a copy naming the archive that does not authenticate under the key is accepted only when
  its recovery entry is the recovery file's own, byte for byte (merely damaged); otherwise the
  restore is refused and told to move that copy aside (CRY-02, CRIT-03). The remembered key is
  this computer's own and vouches for any copy.
- **The generation keeps headroom.** A copy claiming a generation within 2^32 of the cap is refused
  by its name ("move it aside"), and so is a clock that far ahead; every restore leaves 2^32
  rewrites below the cap (CRY-03, OUT-03). A claim far ahead of the clock but nowhere near the cap
  is still outranked: it is the only floor after a restore on a fast clock (CRIT-02).
- **The outranking claim has a stated limit (accepted by the owner, 2026-10-26: "accept").** With
  no copy of the key file left on disk, the generation is the clock alone. A second restore on a clock behind the first's is then
  outranked if the first's key file comes back, and the password it withdrew opens again (FID-01,
  CRY-04). The sentence "a key file put back from an old backup therefore never outranks the
  restored one" holds while a copy claiming the earlier generation survives or the clocks were
  right. A skipped test encodes the gap. Closing it needs a floor that survives losing every copy.
- **Commit, step 3:** it refuses a folder that is no longer the directory prepared (renamed and
  replaced, or a link put there) before taking the lock and again before writing (OUT-02). It
  reads back only the primary it wrote, so a mirror planted afterwards cannot turn an effective
  restore into "not in effect". Once the primary is written every error says the restore is in
  effect: `ErrPartiallySaved`, `vault.ErrUnconfirmed` (it did not read back as written), or
  `keyring.ErrRestoredNotRecorded`. The CLI then keeps the saved copies of the new recovery key
  (OUT-01, OUT-04, CON-02).
- **K14 reaches recovery files.** At a folder this computer has not recorded, a recovery file whose
  entry asks more than MailArchive's own cost is asked about before it is derived
  (`RecoveryEntrySource.Limit`, `RestoreOptions.ConfirmCost`; CRY-05). `OpenWithin` judges every
  copy before any derivation, the re-derivation on a newer copy included (CRY-01).
- **The keyring's Commit** re-reads the record (an archive forgotten meanwhile is asked about
  again) and judges the folder's case again (CON-01, CRIT-05).
- **The CLI** checks the recovery file and the recovery key's typing before the new password. It
  asks again for a key that does not open the file, keeping the password (CRIT-06). Without
  `-recovery-file-out` it offers to save the new recovery file at the terminal (§5.1 step 5,
  §9.2). Without one saved, it does not say to destroy the earlier recovery files. It says the
  earlier recovery file, with the earlier recovery key, is now the only way to put back lost key
  files (CRIT-01).
- **Not done (design gap, FID-05 b):** a recorded folder whose first backup never ran and whose key
  files are gone is §4.5 case 4, offered only forget-archive, though the vault would accept a
  restore there. Damaged key files beside sealed data (case 3) are now offered the restore at
  every open.

**What it does and does not withdraw.**
- Every restore sets a new password, so a withdrawn password never comes back through it.
- The master key is never re-keyed (§0). So an earlier recovery key, together with a recovery file
  or key file saved with it, still opens the archive.
- §9.2 and the card say to destroy those. Nothing stronger is claimed.

**Owed tests:**
- one per refused state:
  - a newer key file;
  - a foreign key file;
  - an I/O error;
  - a copy that opens;
  - no authentic pointer at an unrecorded or unproven path;
  - a clock before the build date;
- an old key file put back after a restore is outranked and healed over, including after a
  second restore on a clock behind the first;
- each refused state appearing between prepare and `Commit` makes `Commit` refuse.

### 9.5 Rebuild the key files from the remembered key (K15) — P3-1b

Same preconditions as §9.4. It is offered from refusal case 3 and the card when this computer
holds a remembered key for the folder's claimed id.
- `vault.PrepareRestore` with `RememberedSource` (§9.4). It proves the key the same way, under
  the same rule for a folder without a pointer, and sets a new password and a new recovery key,
  under K13's confirmation and disclosure.
- Then show, hide, re-enter, and `Commit`.
- It also covers both key files present but corrupted, since the preconditions are "no copy
  opens".

## 10. A plaintext copy (the escape hatch)

`mailarchive decrypt -out DIR -dest PARENT`, or the card's "Make an unencrypted copy". It runs
through `keyring.DecryptCopy`.

**Plan first** (X14): where, how large, the free space.
- `CheckDestination` checks the destination.
- The copy is made in a fresh folder, `<name>-unencrypted-<date>`.

**A synced or removable destination asks twice** (K5).
- Desktop asks twice; the CLI has the path typed back.
- `util.UnderCloudSync` treats everything under `~/Library/CloudStorage/` and
  `~/Library/Mobile Documents/` as synced, and covers iCloud "Desktop & Documents" or says "this
  folder may be synced". One test per provider.

**How.** It takes a lease, then the source's archive lock (lock-free on a read-only medium).
- For each record, one unit at a time within the budget, it reads the page, attachment and
  original units, checks each against `Record.Fixity`, and writes it at `Record.Path`.
- Then it writes:
  - the manifest without `Unit`/`Sealed`, with `Fixity` kept;
  - the history;
  - `search.db`, from the index image or rebuilt;
  - the pages, the report and the plaintext README, by the plaintext run's code.

**Not carried:** the key file, the id, the remembered key, `program/`.

**Engine B is not the copier.** `copyexport` writes an export folder, not an archive (contrary to
parent C4). Its destination checks and progress pattern are reused.

**Exit codes:**
- 0 done;
- 1 refused or failed;
- 130 (SIGINT) or 143 (SIGTERM) when stopped, leaving a partial copy marked unfinished.

**Phase 2c closed here.** An encrypted run over MA-329's golden inputs, decrypted to a plaintext
copy, matches the plaintext golden byte for byte under MA-329's own masks.
- Each item of parent §4.3 is mapped to a test.
- The test is proven to fail when `DecryptCopy` is mutated and when sealing is mutated.

## 11. Encrypting an existing archive — its own design (P3-6)

**Its own design document,** `docs/design-encrypted-archives-phase3-convert.md`, with its own
pre-code review before P3-6 is coded.

**Settled, carried over:**
- O2 (keep the old copy, ask before deleting);
- K6;
- K7;
- records sealed from the archive, never re-exported (`export.SealRecord` factored from
  `sealKind`);
- a sibling workspace on the same volume;
- the 1 GiB ceiling;
- an existing schedule is a refusal;
- the plan shows the last plaintext verify and the count of records without fixity;
- encryption authenticates the archive as it stands then;
- Engine B is not the copier.

**Requirements** (IDs in the review's addenda):
1. **No folder is deleted or renamed on the strength of an unauthenticated marker** (R6-1, R7-01;
   the blocker):
   - a sibling is named by base name only, matching a fixed pattern carrying the conversion id,
     resolved only inside the original's parent, not a link, on the same volume, with a
     reciprocal marker;
   - a workspace is deleted only if it holds nothing but the encrypted-archive tree;
   - resume never runs by itself — Desktop start only reports an unfinished conversion, and every
     rollback, resume and abandon is confirmed, naming the exact folders;
   - markers are read with the regular-file reader, size-capped and strictly parsed (R6-10).
2. **A resume toward swapping is proven, not trusted.** The workspace's sealed header carries the
   conversion id, the original's change token, the record count and the fixity digest. Resume
   opens the workspace with its key (the password or the remembered key) and matches them before
   any rename, and the refusals are re-run (R6-2, condition 29).
3. **One lock spans the whole conversion, outside both trees.** Resume, cancel and abandon take it
   and the original's archive lock. The original's lock is also held from the plan through
   `verified`, and the plan refuses with "a backup is running" when it is held (R6-3, R7-02,
   condition 27).
4. **The change token** is stored in both markers and re-checked after rename 1, under the lock
   re-taken at the renamed path, and at every resume. A mismatch goes back to `copying` (renaming
   the original back while that lock is held) or seals the change. O2 is never asked after a
   mismatch (R3-5, R7-10).
5. **Terms.** "Resume" means moving toward the swap. "Past rename 1" means never going back except
   on a token mismatch, and the design states both.
   - Before rename 1, both markers and the record carry all three fixed names: the original, the
     workspace, and the old copy with its date fixed.
   - The furthest stage wins.
   - No step at `verified` or later deletes a tree because the other is missing.
   - A failed rename 2 stays at `swapping` and is retried (R3-3, R7-03).
6. **The order of `swapped`:**
   1. the record, holding the old copy's path;
   2. the old marker replaced by a note under a non-marker name;
   3. the new marker removed last.

   The O2 question and the "old unencrypted copy" listing come from the record (R3-4, R6-11,
   R7-15).
7. **The record fields this needs** (the pending entry, the old copy's path) are defined under §4.2's
   forward-compatibility rule, with their `encmark` meaning. The old copy's path is never a
   `paths` entry, so the legitimate old copy is never refused as case 2. Every cancel, rollback
   and abandon removes the new id's entry, workspace path and remembered key. Workspace names are
   unique per conversion (R3-6, R7-04).
8. **Windows.**
   - A failed rename 1 returns to `verified` and keeps the workspace.
   - Renames get a folder-sized budget.
   - The re-taken lock is released before O2.
   - The delete can be resumed (R3-7, R3-15).
9. **The "opens nothing" line** is shown on every cancel, rollback and abandon, naming the
   attempt's date and its recovery file. `archiveScratch(final, new id)` is deleted on each
   (R3-14, condition 18).
10. **CLI verbs:** `mailarchive encrypt -resume` and `-abandon`, with their X1 exit codes, plus the
    R37 row (R7-15).
11. **The conversion's key hand-off** is a `keyring` export, so no key leaves `keyring` (A32;
    condition 3).
12. **Uninstall** refuses while a conversion is pending (condition 27).
13. **Before swapping,** the reader and every in-process handle on both trees are closed, and the
    reader's cached handler is invalidated after the swap. When something stands at `<name>`,
    the person is told exactly what to move, and is offered "end the conversion, keep both
    folders" (condition 28, R7-11).
14. **After a conversion,** status and the card say that this archive's backup schedule is off
    until Phase 4 (condition 30).

## 12. Rollback detection and "continue from this older copy" — its own design (P3-7)

**Its own design document,** `docs/design-encrypted-archives-phase3-generations.md`, with its own
pre-code review before P3-7 is coded. It owns A25 and O4. Until P3-7 ships, every keyed load
passes `RecordedGen` 0, so no legitimate restore is ever refused (§7.3), and nothing records
generations.

**Settled, carried over:**
- O4's confirmation and its wording (both dates; mail backed up after the older copy is not in
  it; the next backup collects again what is still in the mailbox);
- the confirmation is needed only before a write — reads get a notice;
- a merge-restore (an older record beside newer files) is refused as A25 intends.

**Requirements** (from the second addendum's F-series and the first's R3 series):
1. **Classification happens inside the keyed load, under the archive lock,** against the digest
   of the pointer the load actually authenticated and selected. It never uses `Peek`.
   `LoadOptions` carries this computer's history for the id, not a single `RecordedGen`
   (F6-02).
2. **Forward progress is strictly "an authenticated generation above the highest this computer
   saw".** Any unmatched copy at or below it takes the older-copy rule (F6-01, F7-05).
3. **A commit replaces only the history entry whose digest the run loaded,** never another copy's
   entry chosen by a shared path (F6-01, F7-04).
4. **Each entry keeps the previous pointer's digest,** so "one backup behind" (an unfinished save)
   is recognised (F7-03).
5. **The store's `ErrUnfinishedSave` and `ErrRestoredOlder` stay reachable.** A merge-restore is
   refused before any O4 offer.
6. **Owed tests:**
   - two drives rotated at one mount path, one of them advanced on a second computer, and neither
     asks twice;
   - an evicted or replaced top entry with an older genuine state replanted below it asks O4;
   - a swap between unlock and Run now asks O4;
   - a read-only open of an older copy holding a crashed run's leftovers, on a read-only medium,
     succeeds.
7. **The record fields it adds** follow §4.2's forward-compatibility rule (a new format number).
   Repair never takes numeric fields from a copy whose trailer failed (F7-10).
8. **"A head-write failed" WARN.** Its durable home is a closed field in the plaintext last-run
   record, cleared at the next success (F7-14). `forget-archive` takes the archive lock, so a run
   cannot re-create a forgotten entry (F7-12).
9. **The one-behind notice** ("this copy is one backup behind what this computer last saw — the
   newest backup's changes, including any removals, are not shown") is a status WARN. A plaintext
   copy made in that state asks first (R6-9).

## 13. The break glass (parent §1; O1, O7)

### 13.1 The note (P3-5)

`README.txt` at the root, always; an encrypted run already writes it (`readme.go:136-148`,
MA-737).

It says:
- what the folder is;
- that MailArchive opens it with the password or recovery key. For a copy found on its own, in
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

  The `SHA256SUMS` on the drive catches accidental damage only.
- **how to start each copy:** the Gatekeeper and SmartScreen steps; on Linux, copy the program to
  your home folder, then `chmod +x`;
- that the official download is to be preferred;
- from P3-1b, the recovery-file instruction.

MA-737's rule holds: no layout, file or table names. The wording owes the friction review.

**Tamper.** The note's SHA-256 is sealed in the header whether or not the program copies are
kept. A mismatch raises a WARN before a keyed run rewrites the note. The reader renders the note
from the program's text.

### 13.2 The program copies — its own design (P3-5)

**Its own design document,** `docs/design-encrypted-archives-phase3-program.md`, with its own
review before P3-5 is coded. O7's change to the release process is made in P3-5.

**Settled, carried over:**
- O1's four platforms, downloaded and checked;
- the `program/` folder;
- byte-identical release assets;
- `SHA256SUMS` written only as the release's own bytes;
- self-copies under their own names;
- a cache in `mailarchive/program-cache/<version>/`, outside `job.ScratchDir`;
- the setting changeable on the card;
- the bundle choice governed by the sealed header;
- schedule creation from a bundled copy refused;
- the conditioned claim — the published checksums prove the copy is the file the release
  published, not who built it, and they hold because releases are unchangeable (O7).

**Requirements:**
1. **Every file written into `program/` has a sealed entry** (name, SHA-256, origin).
   - Self-copies are sealed with their local hash and marked unverified.
   - At each keyed run:
     - a file with no sealed entry is removed with a WARN;
     - a changed file is rewritten, or removed when it cannot be rewritten;
     - an `incomplete` state never suppresses the check (R6-4).
2. **Release copies are replaced only by verified release copies of a higher version.**
   Self-copies sit beside them and never replace them. A development build never removes or
   replaces release copies (R3-8).
3. **All program-copy work runs after the backup's final commit and never fails it.**
   - It uses an intent commit, then the files under temporary names, then a landed commit.
   - Its errors are WARNs naming the failure class.
   - It is skipped below a stated free-space margin.
   - It never rewrites from a cache that lacks the expected version (R3-12, R7-08, R7-09).
4. **Forward compatibility.** The keyless check and the tree grammar accept any regular file in
   `program/` matching a bounded generic pattern (never `-attachments.zip`). The keyed run judges
   against the sealed names (R3-13).
5. **Files are opened with the regular-file reader,** with a size ceiling before hashing (R6-10).
6. **Every trigger for a self-copy is stated** (offline, each failed-download class, a development
   build) and listed in §14. On Windows the self-copy is the console `mailarchive.exe`, under an
   `.exe` name (condition 32).
7. **The download:**
   - built-in repository coordinates and fixed URLs;
   - redirects over HTTPS only, to GitHub's hosts, at most 5 hops;
   - a 200 MB ceiling per file;
   - the proxy from the environment;
   - a separate cancellable cache task outside any archive lock, with resume and at most one
     attempt a day per version;
   - only an exact `vMAJOR.MINOR.PATCH` with `vcs.modified=false` counts as a release;
   - a cache pruning rule that does not need the key.
8. **An asset missing from the release** reads "not published" (condition 33).
9. **Cache files are re-hashed** against the verified `SHA256SUMS` at each copy (condition 34).
10. **The cache is listed in the uninstall plan** (condition 36).
11. **Replacing a self-copy** with the verified release copy of the same platform is an upgrade,
    not a swap: no WARN (condition 32).
12. **The cache keeps at least the current version,** plus the newest version that any archive
    opened on this computer expects. That version is learned at each keyed run and kept in the
    cache folder, not in the record (condition 36).

## 14. The leak budget and threat model, extended

- **The program copies** reveal the MailArchive version that last refreshed them. A self-copy also
  reveals this computer's operating system and processor type; its triggers are stated in the
  program design.
- **The download** sends this computer's IP address and the version to GitHub, once per version.
- **A swapped program copy or note** can mislead someone, or steal a password from someone who
  runs it elsewhere. The note says to prefer the official download and how to check it. The next
  keyed run detects a swap, after the fact. The checksums prove what the release published, not
  who made it.
- **This computer's record** maps folder paths to archive ids, on this computer only, owner-only.
- **An unlocked Desktop** holds keys in memory. Another process of the same user is outside the
  dashboard's guard (within R-E2).
- **The reader's scratch index** above the budget is plaintext in the cache folder while open,
  and swept after a crash.
- **Downloaded attachments and originals** are plain files (K9).
- **A plaintext copy** is plaintext wherever it goes.
- **The old unencrypted copy** after encrypting stays readable until removed (O2).
- **Encryption authenticates an archive as it stands when encrypted.**
- **The master key is never re-keyed.** An earlier recovery key with a file saved alongside it
  still opens the archive (§9.4).
- **A restored key file's generation** reveals roughly when the restore was done, to anyone
  without the key (§9.4).

## 15. Amendments to the parent design

- **A26 — no password environment variable.**
- **A27 — this computer's record.**
  - `encrypted-archives.rec` and its mirror, in the settings folder, or `%LOCALAPPDATA%` on Windows.
  - A format number, a sequence number and a trailer; per id, the paths and a tombstone.
  - It survives uninstall and the Windows cleanup script.
  - It replaces "the archindex entry".
  - Generations are added by the generations design.
- **A28 — the recovery file is the recovery entry only.**
  - `vault.PrepareRestore` and `Commit` put it back, setting a new password and a new recovery key
    at a time-based generation.
  - The remembered key may do the same (K15).
- **A29 — A12 refined** in its own design (§11).
- **A30 — no decrypt-in-place.**
- **A31 — the break glass:** the note (its hash always sealed); the program copies in their own
  design; releases unchangeable (O7).
- **A32 — one unlock package** (`internal/keyring`): the single MA-726 allow-list entry (operator
  gate); no key leaves it; leases.
- **A33 — A13 amended.** The reader's above-budget scratch index goes in the per-user cache folder.
- **A34 — A15 amended.**
  - A folder's claimed id never serves as `want` unproven.
  - Adoption happens only for writes and remember.
  - One predicate (`encmark`) carries every "is this encrypted" question, matching both spellings
    and refusing a link.
- **A35 — A25 is designed separately (§12).** Until it ships, loads pass `RecordedGen` 0.

## 16. Slices, build order and gates

Each slice is built test-first, with prove-fail and prove-pass, and owes its own adversarial pass.

| Slice | What |
|---|---|
| **P3-0** groundwork | the record (§4); `encmark` and its tests; `lockfile.AcquireWait`; the cleanup script; E5, E8; tier-1 cancellation, `stopped`, exit codes 130/143. (E3 and U16 moved to P3-3 when it was built: both change only what the wizard and Desktop show for an encrypted archive, which no one can reach before P3-3.) |
| **P3-1a** keyring + CLI | `internal/keyring` (leases, hand-offs, heal ordering); `tty.ReadSecret`; secret sources and the Windows read check; the no-secret-flag test; the linux-arm64 measurement; `-encrypt` create; keyed `export`/`graph`; `change-password`, `new-recovery-key`, `forget-key`, `forget-archive`, `repair-record`; E2. **Operator gate: the MA-726 allow-list entry.** |
| **P3-1b** recovery | §9.4 and §9.5, `vault.PrepareRestore`/`Commit`; its own adversarial pass |
| **P3-2a** reader | §7; CLI `serve`/`search`/`extract`; keyed `reindex` with E7 (*rebuild only*, owner ruling 2026-10-26, §6) |
| **P3-2b** keyed check | `verify -full` (§8) |
| **P3-3** Desktop + wizard | the card, unlock and lock, Open an archive folder, create with the hook, Run now, the heal, the keyed pause, the key verbs, Put back the key file, forget, the uninstall plan's scratch rendering (MA-739), the wizard step; E3 (the wizard's log for an encrypted archive in the settings folder) and U16 (encrypted wording where text names `index.html`, the report or `search.db`: `capture.go:259`, `wizard.go:1517`, `refusals.go:142-144`, `health.go:1389`) |
| **P3-4** plaintext copy | §10, `UnderCloudSync`, the Phase 2c closure |
| **P3-5** break glass | the note; the program design and review first; O7 |
| **P3-6** encrypt existing | its design and review first (§11) |
| **P3-7** generations | its design and review first (§12) |
| **P3-8** gates | the friction review and X1–X17 walk; Windows lab (reader, hidden prompt, `-recovery-key-out` access list, secret-file read check, the P3-5/P3-6 items) |

*Planned for P3-2a (2026-10-26), from a map of the code it touches.* It is built in four parts,
each test-first; the adversarial pass covers the whole slice:
- **P3-2a-1, the read path's groundwork.** A read-only store handle (`LoadOptions.ReadOnly`: Commit,
  Init and the sweep refuse); a pointer peek that reads the file; `History` that reports a missing
  chunk or a locked key instead of folding them into barriers, and the history gap; index errors
  that keep "missing" and "locked" (`%w`); one memory guard (a leaf package) holding the
  derivation turn and the 2.25 GiB unit-read budget; unit reads split so the key is held only
  while a unit is named and opened.
- **P3-2a-2, the reader.** `server.New(src Source, ix)`, the plaintext source wrapping today's code
  unchanged; the sealed source, built inside `keyring` behind the interface so no key leaves it
  (`keyring.ReaderSource`); `pages.RenderFolder`/`RenderRoot` to a buffer, cached per generation;
  go-back from the decrypted manifest and history; the locked page; GET and HEAD only; a write
  deadline per response; peek, load, peek again.
- **P3-2a-3, the command line.** A read unlock (`-password-file`, `-recovery-key-file`, the
  remembered key, then the prompt; no adoption, no heal; refused without any of them, naming
  `-password-file`); keyed `serve` (a non-loopback `-addr` refused), `search` (`-paths` refused
  before any unlock), `extract`; the scratch index one per holder under an OS lock, with
  uninstall's allow-list amended.
- **P3-2a-4, keyed `reindex`.** A write unlock, then `RebuildEncrypted` with the pause check and
  cancellation; no prune (the owner's ruling above).

Implementer's calls, vetoable:
- **Desktop stays locked in P3-2a.** The dashboard unlock is P3-3, so the Desktop reader keeps
  MA-731's 503 and gains only the source split.
- **`README.txt` is 404 on an encrypted archive** until the note exists (P3-5); the sealed root
  page does not link it.
- **The CLI `serve` has no idle lock;** it locks when it exits, and its locked page says to start
  `serve` again.
- **`search` prints logical paths** (`-json` and the human form): they name the message in the
  reader, not a file on disk; `-paths` stays refused.
- **Keyed `extract` takes the archive's `extract` lock,** as a plaintext extract does.

*Amended in P3-1a — what the slice built, and what stays pending:*
- Built: `internal/keyring` (open by password, recovery key or remembered key; the recorded id
  as `want`; adoption only for a write or a remember, a tombstone asked about; unproven paths
  proven or replaced; K14; the lost-record note and its status WARN; heal; leases and locks;
  create; the key verbs' cores; forget and forget-key; `Apply` with `app.Options.RecordedID`; E2's
  PST folder); `tty.ReadSecret` and `tty.ReadLine`; secret files with the Unix owner and mode
  check and the Windows SDDL predicate; `encrecord.Repair` and the lost list; `-encrypt`, keyed
  `export` and `graph`, and the five verbs at the CLI; the no-secret-flag scan; the
  linux-arm64 measurement (§3.7); recording by the resolved location (§4.2).
- A keyed backup or `-encrypt` without a terminal is refused before discovery, as a keyless one
  is (MA-733's order).
- A prompt ended by Ctrl-C or SIGTERM exits 130 or 143, as a stopped `verify` does. The UX
  contract's X1 map (`docs/ux-contract.md`) lists 0–3 only; adding 130 and 143 there is the
  operator's call (§18).
- Pending, for the Windows lab (P3-8): the console prompt, the secret file's handle check,
  `-recovery-key-out`'s access list, junctions and folder-mounted volumes (SEC-04), and OneDrive.

**X1 exit codes per new verb:**
- `change-password`, `new-recovery-key`, `forget-key`, `forget-archive`, `repair-record`,
  `restore-key-file`: 0 done, 1 refused or failed.
- `decrypt`: as §10.
- `verify -full`: as §8.
- `encrypt`: its own design.

**Gates owed by this document:**
- none further before P3-0 and P3-1. Revision 5 applies the last check's fixes, and the slices'
  adversarial passes check the built code against them;
- the three split-out designs, each reviewed before its slice;
- an adversarial pass per slice;
- the MA-726 operator acceptance at P3-1a;
- the friction review and X walk at P3-8;
- E9's own review.

## 17. Catalog plan

**New MA rows from MA-768:**
- one per slice behaviour;
- an R37 row per new write path;
- `encmark` with its stripped-and-replanted, link and spelling tests;
- the no-secret-flag test;
- leases and the reader lock;
- the restore's refused states and the outranking test;
- the Phase 2c closure.

**Amended rows:** MA-39, MA-726, MA-731, MA-737, MA-743 (with P3-5), MA-747 (with P3-7), MA-765.

## 18. Open questions

None blocks the last check. The note's wording goes to the friction review. The X-series rows
for encryption are the operator's call. *Settled 2026-10-26:* X1's map gains 130 and 143 (a run
or prompt stopped by Ctrl-C or SIGTERM with nothing saved by it), and CI runs the tests with a
20-minute limit per package; the owner also kept the P3-1a pass's CRIT-02 fix (an unproven path
whose key files are gone can be forgotten).
