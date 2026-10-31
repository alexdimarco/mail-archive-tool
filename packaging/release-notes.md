**mail-archive-tool** — a source-agnostic mail archiver (Outlook `.pst`/`.ost`,
Thunderbird mbox/maildir, Evolution, Microsoft 365 via Graph) into self-contained
HTML + per-email attachment zips, with a full-text search index, scheduling,
status, and a native-dialog GUI. Pure Go, no cgo.

### macOS — start here
- **GUI (double-click app):** download **`MailArchive-macos.zip`**, unzip it, then
  because it's unsigned: **right-click "Mail Archive.app" → Open → Open** (once).
  Or in Terminal: `xattr -cr "Mail Archive.app"` then double-click. Universal —
  runs on Apple Silicon and Intel.
- **CLI (optional, Terminal):** download **`mailarchive-cli-macos.tar.gz`** — a
  command-line program, **not** a double-click app (double-clicking a bare
  program opens it in a text editor). In Terminal:
  `tar -xzf mailarchive-cli-macos.tar.gz && cd mailarchive-cli-macos && xattr -cr mailarchive && ./mailarchive -h`.
  Universal — Apple Silicon and Intel.
- macOS ships exactly these two downloads (the app and the CLI tarball); there
  are no bare per-arch macOS binaries to pick wrong.

### Linux / Windows

MailArchive is one program with verbs; there is no separate GUI download.

| Platform | Download | Run |
|---|---|---|
| Linux x86-64 | `mailarchive-linux-amd64` | `mailarchive wizard` or `mailarchive desktop` (the wizard needs `zenity` and a desktop session) |
| Linux arm64 | `mailarchive-linux-arm64` | the same verbs (compiled, not lab-run) |
| Windows x86-64 | `mailarchive-windows-amd64.zip` (or the MSI) | unzip, then double-click `mailarchivew.exe` |

Windows: SmartScreen → *More info → Run anyway*. Verify downloads against `SHA256SUMS`.

**Windows — MailArchive Desktop:** the dashboard is `mailarchivew.exe desktop` — double-click
`mailarchivew.exe` to open it (it opens the dashboard in your browser). For an IT/RMM rollout,
`mailarchive-desktop-windows-msi-payload.zip` builds a self-contained MSI
(`packaging/mailarchive-desktop/`).

### New in 1.5.2 — Run now syncs Thunderbird on a Mac
- On a Mac, Run now syncs Thunderbird first, even while you are at the computer: it starts Thunderbird out of sight, waits for its mail (up to 20 minutes, often about 13; Stop waiting and back up now ends the wait), and leaves it open for you to quit. Scheduled backups still wait until you are away. On Windows, Run now does not start Thunderbird yet; scheduled backups still sync it.
- **Encrypted archives on a Mac** can sync Thunderbird before Run now too: turn on **Sync Thunderbird before each Run now** in Settings. Until that backup starts reading mail, **Lock** cancels it.
- **Skipped syncs are no longer silent.** If Thunderbird could not be synced before an archive's scheduled backups for 3 days, **Status** says so and what to do, and backup alerts send one email about it (when "a failed backup" is ticked).
- **A softer warning** shows in **Status** when Thunderbird was started before each scheduled backup for 3 days but no new mail arrived: it may be waiting for a password or sign-in. No email is sent for this one, and it clears by itself the next time a backup sees new mail arrive in Thunderbird, or a scheduled backup (or, on a Mac, Run now) finds Thunderbird already open.
- **Hourly backups with the Thunderbird sync** still work. In an hour with no new mail, Thunderbird stays open for about 20 minutes while the backup waits for mail; Settings and `mailarchive schedule` say so when you pick Hourly with the sync on.

### New in 1.5.1 — one place for backup settings
- **Settings is where you change a backup.** How often, the time, and **Sync Thunderbird before each backup** are all in the Desktop's **Settings** card. The **Scheduled backup** card shows what is set up, with **Run now**, **Repair** and **Remove**.
- **Each archive gets its own schedule.** An archive made from Thunderbird mail gets its own backup that re-reads its mail files, even on a computer where Microsoft 365 is set up.
- **Hourly backups** for archives made from local mail (Thunderbird and other mail files). Microsoft 365 backups stay Daily or Weekly.
- **Every button shows it was pressed.** A button turns grey with a spinner and a "Working…" label until its job is done, and the card then says what happened ("Repaired.", "Removed.").
- **Fewer emails when a backup keeps failing:** one alert per failing streak and one when it recovers. An archive whose mail source can't be reached is flagged after **3 days** and its backups are paused after **30 days**, counted in days, not runs.
- **Fixed:** the Desktop could count its own settings file as a backup job, which could refuse a Microsoft 365 backup or let Remove delete that settings file. Remove now deletes only real backup-job files.
- **Fixed:** a download of the program copies kept in an encrypted archive that stalled could be counted as damaged and started over, instead of resuming where it stopped.

### New in 1.5 — encrypted archives
- **Make an encrypted archive.** Everything in it — messages, attachments, the search index, the folder list and what it was made from — is encrypted on the drive and opens only with its **password** or its **recovery key**. Make one in MailArchive Desktop (**Add another archive → Make a new encrypted archive…**) or at a terminal: `mailarchive -encrypt -out NEWFOLDER -input MAIL`. You choose a password; MailArchive shows a **recovery key once** and asks you to type it back. Keep both safe: if you lose the password and the recovery key, nobody can open the archive — there is no reset.
- **Use it like any archive.** Unlock it on the Desktop's **Encryption** card (or let this computer remember its key), then back it up with **Run now**, search it, read it and go back in time. **Lock** closes it again. Backups into an encrypted archive run while you are there to unlock it; scheduled backups for encrypted archives are not available yet.
- **Change the password, make a new recovery key, put lost key files back** (`change-password`, `new-recovery-key`, `restore-key-file`), and **check it** with the key: `mailarchive verify -full`.
- **The way out.** **Make an unencrypted copy…** (or `mailarchive decrypt`) writes a readable copy into a new folder, showing its plan first and asking twice when the folder may leave this computer (a synced folder, a removable drive).
- **Opening it years from now.** By default the archive keeps copies of MailArchive for Mac, Windows and Linux (about 134 MB each time a new version updates them) in its `program` folder, checked against this release's `SHA256SUMS`, with a `README.txt` saying how to open it. You can switch this off on the Encryption card or with `-program-copies off`. Published releases of MailArchive can no longer be changed, so a copy that matches `SHA256SUMS` is the file that was published.
- **Security update.** The HTML library MailArchive uses to clean and index email (`golang.org/x/net`) moves to v0.62.0 for two published vulnerabilities (GO-2026-6866, GO-2026-6860); its text library moves with it. Mail already archived reads back exactly as before.
- **Limits of this first version.** An existing archive cannot be encrypted in place: make a new encrypted archive beside it and back up into it. And a restored older copy of an encrypted archive is not noticed: MailArchive carries on from that copy, and mail backed up after it is collected again only if it is still in the mailbox (a plain archive does not notice this either).

### New in 1.1–1.3
- **Archive manager.** MailArchive Desktop now manages more than one archive: a **Your archives** card lists every archive this computer knows about, marks one **current**, and switches between them. **Pause (make read-only)** an archive so nothing can change it, and **Resume** it later — nothing is deleted either way.
- **Built-in email alerts.** Have a scheduled backup email you when it **fails** (or is auto-paused, or — if you want — succeeds). Built-in SMTP, no script to install: **Office 365**, **Gmail**, or a **standard SMTP** server; the password is kept in your OS credential vault, and **Send test email** confirms it before you rely on it. Works for both Microsoft 365 and local scheduled backups.
- **Backup run history.** A **Recent backups** card (and an archive-local history record) shows the last several runs — when, result, how many messages — not just the latest.
- **Run now / Shut down / Add another archive.** Run the backup immediately instead of waiting for the schedule; shut the dashboard down cleanly to free its port (do this before an upgrade); and set up an additional archive with the wizard without disturbing the current one.
- **Source-loss surfacing.** When an archive's **source** (its mailbox or mail folder) stops being reachable across scheduled runs, the dashboard shows a banner and `status` reports a WARN (`source_lost`), so a stalled backup is never silent.
- **Uninstall keeps your archives.** A **Danger zone → Uninstall** removes MailArchive's program and schedules from this computer, shows a plan first, and **never deletes an archive**.
- **Upgrading to a newer version.** Stop the program (**Shut down**), replace the files, relaunch — your archives are untouched; if the program's path changed, the app's **Repair** re-points the schedule. See "Upgrading to a newer version" in the README.

### Fixed in 0.7.2
- **The wizard's Repair no longer breaks a weekly backup set up in MailArchive Desktop.** When an archive's schedule had been created by MailArchive Desktop (a weekly Microsoft 365 capture), the launch wizard's health check could offer *Repair* and, on clicking it, convert that backup into a job-file schedule that failed every night while `status` stayed green. The wizard now recognises a MailArchive Desktop schedule, does not offer to repair it, and points you to MailArchive Desktop instead (R14, R18).
- **A scheduled capture that can't start is no longer silently green.** A scheduled Microsoft 365 backup that refused at launch because it was not configured or signed in wrote no run record, so `status` and the health view stayed GREEN until the run was twice its period late. Such a refusal now records a failed run, so status reads RED with the reason straight away (R18).
- **An unplugged drive no longer looks like a brand-new archive.** When an archive's folder was unreachable (a removable or network drive not connected, or a deleted folder), health read it as a new, empty archive and suggested installing a *second* schedule. It now says the location is unreachable and that nothing is wrong with the archive itself, and drops the misleading "no schedule" advice (R18).
### Opening the reader and the dashboard now needs a private link
`serve`, `setup` and MailArchive Desktop are opened only through a **private one-time link** the
program makes each time it starts, so other accounts on the same computer and web pages cannot
open your archive or dashboard (a program running as you still can).
- **`mailarchive serve`** opens your browser itself at its link (not with `-no-browser`, or without
  a display); on a terminal it also prints a second link, and Enter prints another. With no
  terminal (nohup, launchd, systemd, a detached tmux) run **`mailarchive serve -link -out <folder>`**
  from any terminal of your account for a fresh link; an `ssh -L` tunnel keeps working (open the
  link through the forwarded port). `mailarchive setup` now picks a free port.
- **Scripts that called `serve`'s HTTP API with `curl` stop working** (they have no session): use
  `mailarchive search` (same query grammar; `-json` for machine output).
- **`serve -addr 0.0.0.0`** keeps its warning and now says the archive and the session cookie
  cross the network in clear text; each browser still needs its own link.
- **MailArchive Desktop (Windows):** the dashboard and the reader each take a free port, so a
  bookmark of `http://127.0.0.1:8097/` or `:8099/` no longer reaches them — open it from its
  shortcut; the Start Menu has **Copy MailArchive link** for a default browser that cannot be
  opened. Each Windows account (and each sign-in session) has its own instance. An upgrade ends
  running instances so the fix applies in every session at once.
- On Linux and macOS another account that can list running programs could read a link in the
  instant a browser is opened with it; macOS now hands the link to the browser without putting it
  in the program list. A link works once, and "already used" when you did not use it means: stop
  and start the program again.

### Fixed in 0.7.1
- **Windows: rebuilding the search index no longer fails while the archive is open.** A leaked database handle kept `search.db` open, so `reindex -rebuild` (and the dashboard's *Rebuild index*) could fail with "Access is denied" on Windows while a viewer held the index. The handle is now released; rebuild and reindex work with the archive open.
- **Windows: a lost or overwritten archive lock is now detected mid-run.** The lock-loss check relied on POSIX file identity, which Windows cannot provide, so in rare cases two runs on the same archive could overlap on Windows. The lock now carries a content marker re-checked during the run, so a run whose lock is taken over stops instead of racing (R5) — on every OS.
- **Clearer schedule preview on macOS and Windows.** `schedule` now shows the full command on one readable line (as on Linux) plus the same operator notes, not only the raw launchd/schtasks definition.

### What's new
- **MailArchive Desktop (Windows) — a local control panel for Microsoft 365.** A new single
  app opens a private dashboard on `127.0.0.1` (this computer only) that signs you in to M365,
  archives your mailbox, browses it (with go-back-in-time), schedules a weekly backup, and
  checks the archive's health — all **in-process** (no separate engine to install), with a
  plain-language Security & privacy panel. Double-click it and it opens the page in your
  browser; the weekly backup keeps running with the app closed. Ships with an MSI/RMM kit for
  IT. The dashboard and its reader bind loopback only and refuse any network address; the
  sign-in is read-only (`Mail.Read`) and stays in the OS vault. Cross-platform core (Linux/
  macOS run the same dashboard); Windows integration (Credential Manager, shortcuts, MSI) is
  Windows-first and validated by hand, not in CI.
- **Guided Microsoft 365 setup (`mailarchive setup`).** A local wizard walks an admin through
  the Entra app registration — tenant ID, application (client) ID, and (app-only) the client
  secret — and clarifies the two-Object-ID trap: you need the **Application (client) ID**, not
  an Object ID. The secret is stored in your OS credential vault, never on the command line.
- **Your Microsoft sign-in lives in the OS vault.** On Windows the delegated sign-in (and any
  app secret) are kept in **Windows Credential Manager**, not a plaintext file; elsewhere, a
  file only you can read. Only the refresh token is stored (the short-lived access token is
  regenerated as needed), and nothing is uploaded. Sign out any time (*Clear sign-in*);
  uninstalling keeps your archive.
- **Microsoft 365 without admin — sign in as yourself (`-auth device`):** you no longer need a tenant-wide app permission to archive an M365 mailbox. `mailarchive graph -auth device -out ./archive -tenant T -client-id C` prints a code and a URL; you sign in once in a browser and approve, and the tool archives **your own** mailbox with a read-only **delegated** `Mail.Read` grant — no admin, no RBAC, no client secret. The sign-in is saved (a refresh token, `chmod 600`, under your OS config dir) so scheduled runs need no re-sign-in; run the schedule as the same account you signed in as. The original **`-auth app`** (app-only, admin-consented, many mailboxes from one job) is unchanged and still the default. Read-only throughout, either way. See [`docs/graph-app-setup.md`](../docs/graph-app-setup.md).
- **Reused Message-IDs, told apart (Microsoft 365 / Graph):** some senders stamp two genuinely *different* messages with the *same* `Message-ID`. Earlier versions kept one copy per Message-ID, so a distinct reuse could be skipped. A live Graph archive now uses each message's Microsoft **immutable id** plus a body-inclusive **content hash** recorded at capture: a reuse whose id it has not archived is downloaded and, when its content differs, kept as its own message — even when the two share an identical envelope (a `$100` and a `$250` invoice that reuse one id both survive). A copy filed into several folders, or a message whose id is reissued by a mailbox restore/migration, is recognised as the same content and kept as **one** record (no duplicate, no re-download). The manifest format bumps to **v6**; this closure applies to messages captured **fresh** under v6 — an existing archive keeps the Message-ID behaviour for what it already holds (no re-download to upgrade), and sources without a per-message id (IMAP / local imports) keep it too. See [`docs/goback.md`](../docs/goback.md).
- **Go back in time:** for a live Microsoft 365 (Graph) archive, `mailarchive serve` now shows the mailbox as it was on any past date, Wayback-style — a scheduled daily capture records an append-only timeline, so a message deleted online stays visible at earlier dates (under its then-folder) while the current view follows moves and hides it. Deleting the files and running `reindex` redacts a message across all dates. Deleted Items and Junk Email are excluded from a Graph capture by default (`-include-deleted` / `-include-junk` opt in). A live archive keeps one copy per message and records folder moves in place (no re-download, no duplicate); the manifest format bumps to v5 (v6 adds the reused-Message-ID closure above), and the first run over a pre-existing live archive **collapses** any per-folder duplicates into one record (recognised by Message-ID, not re-downloaded — see Known limitations). The date view is served-only — the offline pages still show each message's first-captured folder. See [`docs/goback.md`](../docs/goback.md).
- **Keep it current:** `mailarchive schedule` takes any backup job (export, Graph, reindex, verify), validates it when you schedule it, works with any Windows path, logs to a rotating file, and the GUI offers *Keep this archive current?* after a run. One schedule per archive.
- **Know where you stand:** `mailarchive status -out DIR` — completeness, last run, schedule, GREEN/WARN/RED with remedies; the GUI shows the same on launch.
- **Incremental fills gaps:** mail archived before its content was downloaded is re-examined by every incremental run and filled once it is there; `-mode full` is no longer the remedy.
- **Verify the files:** `mailarchive verify -out DIR` re-hashes every archived file against the checksum recorded when it was written (exit 0 attested, 2 not attested), and `verify -record` baselines an older archive; `status` shows a *Fixity coverage* and *Last verify* line, and a scheduled verify that finds modified or missing files goes RED.
- **Scriptable:** `mailarchive search -json`/`-paths`/`-0` and `mailarchive status -json` (versioned, with `reason_codes`) make both surfaces machine-readable.
- **Safe offline:** every exported page carries its own policy — nothing runs or loads from the network when opened from disk; `serve` is hardened too.
- **Legible archive:** README.txt inside the archive, navigation and attachment links on every message page, full headers with original time offsets, paginated folder pages, optional `-raw` originals.

### Known limitations
- Reads **classic** Outlook `.pst`/`.ost`; *New Outlook* and *Outlook for Mac* (`.olm`) are not supported locally — use the Microsoft 365 (Graph) path.
- IMAP mail exports only if downloaded locally; do the one-time prep (`-enable-offline -sync-wait` for Thunderbird, "Mail to keep offline → All" for Outlook, *Synchronize remote mail locally* for Evolution). The archive records what is still missing and later runs fill it; `attachments-report.tsv` and `status` show the count.
- Windows scheduled runs happen only while you are logged in; a console window appears briefly for CLI-scheduled jobs.
- Go-back (the point-in-time view) is a `serve` feature and needs a live Graph capture that records the timeline; a one-shot local import (`.pst`/mbox/maildir) has none. Its resolution is your run cadence — capture daily for a daily timeline.
- Upgrading a **pre-existing** live (Graph) archive **consolidates** it on the first run: per-folder move-duplicates collapse into one mailbox-wide record and the search index is re-keyed, so each still-present message is recognised by its Internet-Message-ID and **not** re-downloaded (one copy per message, no re-fetch). A collapsed duplicate's file is never deleted. The reused-Message-ID closure (above) applies to messages captured **fresh** under format v6: an existing archive keeps the Internet-Message-ID behaviour for its already-captured messages (a distinct reuse of one of those is skipped, as before — the tool never re-downloads a whole mailbox to upgrade), and IMAP / local imports (no per-message id) keep it too — in all cases **no file is ever deleted** (`verify` flags any orphan). A message copied into several folders is kept as one record but its shown "current folder" and the date-view timeline may flap between those folders each run (cosmetic; the message is never lost, duplicated, or re-fetched).
- The Outlook-app export path (`-outlook`) and the real Windows/macOS scheduler installs are validated by hand, not in CI (catalog rows MA-60, MA-79, MA-95). MailArchive Desktop's Windows-specific integration — the Credential Manager sign-in/secret, Desktop/Startup shortcuts, and the MSI build/install/upgrade — is likewise validated on Windows by hand (the dashboard, in-process capture, and reader are CI-tested cross-platform on Linux). Reading a mailbox's own `sensitivity` from Microsoft 365 is best-effort: a tenant that doesn't expose the underlying property is archived without it, not failed.

Licensed under **AGPL-3.0**. Provided "as is", without warranty — use at your own risk.
