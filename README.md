# mailarchive

Archive a local mailbox into a directory of **self-contained HTML files** with
**per-email attachment archives**, mirroring the folder tree — and make it
searchable.

It is **source-agnostic**. Everything downstream of a normalized message type is
shared, so the same export / search / GUI stack works across:

| Source | Format | Reader |
|--------|--------|--------|
| **Outlook** (desktop) | `.pst` / `.ost` | pure-Go [`go-pst`](https://github.com/mooijtech/go-pst) |
| **Thunderbird** (and any mbox) | mbox files / mail directory | [`go-mbox`](https://github.com/emersion/go-mbox) + [`go-message`](https://github.com/emersion/go-message) |
| **Evolution** (GNOME) | local Maildir++ store / IMAP disk cache | maildir reader + [`go-message`](https://github.com/emersion/go-message) |

Adding another source means adding one reader; the exporter, attachment zipping,
incremental manifest, search index, web UI, folder pages and GUI are untouched.

It supports a **full** export and an **incremental** export that only writes
items not seen on previous runs, optionally bounded to a recent date window
(e.g. "the last month"). Everything is **pure Go (no cgo)** — it builds and runs
on any OS, needs neither Outlook nor Thunderbird installed, and can process a
copied mailbox offline.

Beyond exporting, it can **`reindex`** an archive to reconcile it with what's on
disk after files are moved or deleted, and **`schedule`** recurring backups with
the host OS's scheduler (cron / launchd / Task Scheduler).

## Output layout

```
<out>/
  <store name>/
    <mirrored folder path>/
      2026-07-15_1032_subject-slug_a1b2c3d4.html             # one file per email
      2026-07-15_1032_subject-slug_a1b2c3d4-attachments.zip  # only if it has attachments
  search.db                                                  # full-text search index
  index.html                                                 # browsable folder listing
  .mailarchive-manifest.json                                 # export state (incremental)
```

Each HTML file is standalone: a metadata header (From / To / Cc / Date /
Subject) plus the message body. Inline images referenced with `cid:` are
embedded as `data:` URIs so the file renders offline; other attachments go into
the sibling `.zip`.

## Build

```sh
make build            # CLI  -> bin/mailarchive
make build-gui        # GUI  -> bin/mailarchive-gui
make build-windows    # bin/mailarchive.exe (console) + bin/mailarchive-gui.exe (no console)
```

Requires Go 1.23+. Both binaries are pure Go (no cgo); the Windows executables
cross-compile from any OS.

## GUI (native dialog wizard)

`mailarchive-gui` is a small wizard built on native OS dialogs (pure-Go
[`zenity`](https://github.com/ncruces/zenity)) — no browser, and on Windows no
console window. Double-click it and it walks you through:

1. **Source** — **Auto-detect my mailboxes** (finds Outlook, Thunderbird, and
   Evolution stores and lets you pick one or all), or choose a type manually:
   Outlook `.pst`/`.ost` file, a Thunderbird/mbox mail folder, a single mbox file,
   or — on Windows with classic Outlook — **Outlook account (via Outlook app)**,
   which has Outlook export each account to a `.pst` first (for Exchange/`.ost`).
2. **Choose** the file or folder (native picker), or the auto-detected store(s).
3. **IMAP prep** *(when applicable)* — for a Thunderbird IMAP account it offers to
   enable offline download and walk you through *Download/Sync Now* (then exports
   in Full mode); for an Outlook `.ost` it shows the equivalent "Mail to keep
   offline → All" guidance.
4. **Choose** the output folder.
5. **Mode** — Incremental or Full.
6. **Date window** — e.g. `30d`, `4w`, `2026-07-01`, or blank for everything.
7. **Mail app open?** — if yes, it snapshots the file first to avoid a lock.
8. A **progress** dialog (cancellable — progress is saved), then a **summary**.

A run log is written to `mailarchive.log` in the output folder. The GUI drives the
exact same export engine as the CLI, so results are identical.

Build the no-console Windows GUI explicitly with:

```sh
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchive-gui.exe ./cmd/mailarchive-gui
```

## Usage

```sh
# Outlook: incremental export (default). Re-running only writes new messages.
mailarchive -input archive.pst -out ./export

# Thunderbird: point at a mail-store directory (an IMAP/Local Folders account dir).
mailarchive -input ~/.thunderbird/xxxx.default/ImapMail/mail.example.com -out ./export

# A single mbox folder file.
mailarchive -input ~/.thunderbird/xxxx.default/Mail/Local\ Folders/Archive -out ./export

# Evolution: point at the local "On This Computer" store, or an IMAP disk cache.
mailarchive -input ~/.local/share/evolution/mail/local -out ./export

# Auto-discover every mailbox (Outlook on Windows; Thunderbird + Evolution anywhere).
mailarchive -auto -out ./export

# Full export of everything, ignoring the manifest.
mailarchive -input archive.pst -out ./export -mode full

# Only the last month, snapshotting the file first (mail app can stay open).
mailarchive -input "%LOCALAPPDATA%\Microsoft\Outlook\me.ost" -out ./export -since 30d -copy-first
```

An `-input` may be an Outlook `.pst`/`.ost` file, an mbox file, a **mail-store
directory** (a Thunderbird account dir, or an Evolution local/cache store —
walked as one source, including nested `.sbd` subfolders and maildir folders),
or a plain directory of `.pst`/`.ost` files (expanded). `-input` is
**repeatable**, so one command can combine several heterogeneous sources — e.g.
an Outlook `.pst` *and* a Thunderbird account — into a single archive under one
shared search index (each keeps its own top-level `<store>/` directory). `-auto`
does this automatically across every mailbox it finds.

### Flags

| Flag | Description |
|------|-------------|
| `-input` | A `.pst`/`.ost` file, an mbox file, a mail-store directory, or a directory of data files. Repeatable / comma-separated; positional args also count. |
| `-out` | Output directory (required). |
| `-mode` | `incremental` (default) skips items already in the manifest; `full` re-exports everything. |
| `-since` | Only items on/after this: `30d`, `4w`, `12h`, `720h`, or a date like `2026-07-01`. |
| `-manifest` | Manifest path (default `<out>/.mailarchive-manifest.json`). |
| `-copy-first` | Copy each data **file** to a temp snapshot before reading (avoids a lock when the mail app is open; ignored for directories). |
| `-outlook` | **Windows + classic Outlook only.** Have Outlook export each account to a fresh `.pst` under `<out>/_outlook-pst`, then archive those. Use when a live Exchange/`.ost` cache can't be read directly (see below). Refuses with a message elsewhere. |
| `-outlook-sync-wait` | With `-outlook`: run a Send/Receive and wait up to this long (default `5m`) for downloads before creating the PST; `0` skips the sync. Only mail Outlook has downloaded locally is captured — set "Mail to keep offline" to **All** first for a complete archive. |
| `-auto` | Auto-discover mail stores: Outlook files on Windows (`%LOCALAPPDATA%\Microsoft\Outlook\*.ost`, `%USERPROFILE%\Documents\Outlook Files\*.pst`), Thunderbird profiles on any OS (`~/.thunderbird/*/{ImapMail,Mail}/*`, incl. Snap and macOS/Windows), **and** Evolution stores (`~/.local/share/evolution/mail/local` and each `~/.cache/evolution/mail/*` IMAP cache, incl. Flatpak). Orphaned or corrupt Outlook `.ost` stubs (left by removed accounts) are skipped; a file merely locked by a running Outlook is kept. |
| `-index` / `-pages` | Build the search index / folder pages (both default on; set `=false` to skip). |

### Incremental model

Each exported message is recorded in the manifest under a key of
`folder path` + its identity (the RFC 5322 **Message-ID**, or a content hash
when absent). Scoping by folder means the same email filed in two folders is
exported to both, while a re-run still skips each `(folder, message)` pair it
already wrote. Deleting the manifest forces a fresh full export.

Deleting originals is intentionally **not** performed — export only. Remove
messages from your mail app yourself once you've verified the archive.

## Search & discovery

A large export is only useful if you can find things in it. Every export builds a
**full-text search index** (`search.db`, SQLite FTS5) alongside the files and
also writes **browsable folder pages**. Both update incrementally.

**Local search + reader UI** (scales to millions of messages):

```sh
mailarchive serve -out ./export          # then open http://127.0.0.1:8099/
```

Ranked full-text over subject, body, people and attachment names, with filters
for folder, year and has-attachment. Click a result to read the email; grab its
attachments as a zip. The box also understands tokens like
`from:bob after:2025-01 invoice`.

**Terminal search** (no browser):

```sh
mailarchive search -out ./export from:bob invoice
mailarchive search -out ./export -folder Inbox -after 2025-01-01 contract
```

**Browsable pages** (no binary needed): open `./export/index.html` for a folder
listing; each folder has a sortable/filterable `index.html`. For ad-hoc full-text
without the UI, `rg -i "your text" ./export` (ripgrep) or add the folder to
Windows Search.

Indexing and page generation are on by default; disable with `-index=false` /
`-pages=false`. The index is pure-Go SQLite (`modernc.org/sqlite`), so it still
cross-compiles to the Windows binary with no cgo.

## Maintenance & automation

### `reindex` — reconcile the archive with disk

If you delete, move, or rename exported files by hand, the search index, the
manifest, and the folder pages still point at them. `reindex` reconciles the
archive to what is actually on disk: every entry whose file is gone is pruned
from the index and the manifest, the surviving files stay searchable, and the
folder pages are regenerated. Nothing on disk is deleted.

```sh
mailarchive reindex -out ./export
# reindexed: kept=1843 pruned=12
```

### `schedule` — recurring backups

`schedule` writes a recurring-backup entry for the host OS's scheduler — **cron**
on Linux, a **launchd** LaunchAgent on macOS, **Task Scheduler** on Windows. The
scheduled command is `mailarchive` plus the export flags you pass, so it runs an
incremental backup on your cadence. By default it **prints** the exact entry and
applies nothing; add `-install` to apply it (idempotently) and `-remove` to take
it back out.

```sh
# Print the cron/launchd/schtasks entry for a nightly 02:00 incremental backup:
mailarchive schedule -out ./export -auto

# Apply it; re-running -install just updates the single entry.
mailarchive schedule -out ./export -auto -install

# Weekly on Sunday at 03:30, a named job:
mailarchive schedule -out ./export -auto -interval weekly -at 03:30 -name weekly-mail -install

# Remove it later (keyed by name):
mailarchive schedule -name weekly-mail -remove
```

| Flag | Description |
|------|-------------|
| `-interval` | `daily` (default), `weekly`, or `hourly`. |
| `-at` | Time of day `HH:MM` (default `02:00`; `hourly` uses only the minute). |
| `-name` | Scheduler entry name (default `mailarchive-backup`). |
| `-install` / `-remove` | Apply / uninstall (default: just print the entry). |
| export flags | `-out` (required), `-input`, `-auto`, `-mode`, `-copy-first`, `-since` are baked into the scheduled command. |

## Verifying completeness

Every run audits itself for anything **referenced but not fully exported** and
prints a summary; details go to `attachments-report.tsv` (opens in any
spreadsheet) in the output directory:

- **empty / not-downloaded attachment** — a declared attachment produced zero
  bytes. The usual cause is an IMAP account whose message content isn't cached
  locally. Fix: in your mail app download it for offline use (Thunderbird:
  *Account Settings → Synchronization & Storage → Keep messages… on this
  computer*, or right-click the folder → *Download / Sync Now*), then re-run
  with **`-mode full`** — incremental would skip the already-exported message.
- **unresolved inline image** — the HTML references a `cid:` image that isn't
  present in the message. This is common and usually *not* recoverable: it
  happens when a reply/forward quotes an original message but doesn't carry its
  inline images, so the reference dangles in the source mail itself.

The run summary shows `attachments=… inline=… ` and, if any, a `Verification:`
line with the counts.

### IMAP: get everything downloaded first

IMAP accounts (Thunderbird, or Outlook in Cached Exchange Mode) keep mail
locally only **on demand**, so an export can miss not-yet-downloaded content.
For **Thunderbird**, two flags automate the prep:

```sh
# With Thunderbird CLOSED first; it enables offline, then waits while you sync.
mailarchive -enable-offline -sync-wait -mode full \
  -input ~/.thunderbird/xxxx.default/ImapMail/mail.example.com -out ./archive
```

- `-enable-offline` flips the account's "keep messages on this computer" setting
  in `prefs.js` (backs it up first; **refuses if Thunderbird is running**).
- `-sync-wait` then pauses so you can start Thunderbird and **Download/Sync Now**,
  watches the store until it stops growing, and proceeds automatically (or press
  Enter). Use **`-mode full`** so messages exported earlier without their content
  are re-written now that it's local.

For **Outlook**, the tool flags the gap but can't change the setting for you:
set *Account Settings → Change → Mail to keep offline → **All***, then
*Send/Receive → Update Folder*, and re-run with `-mode full`.

## Sources & limitations

**Outlook (`.pst`/`.ost`)**
- **Classic Outlook only.** The *New Outlook* ("Monarch") app has no `.pst`/`.ost`
  files; exporting it would require the Microsoft Graph API instead.
- **OST while Outlook is open.** The file may be locked; use `-copy-first`, or
  close Outlook.
- HTML bodies are read directly from `PidTagHtml` (including the binary form
  modern Outlook uses). Non-mail items (calendar, contacts, tasks) are skipped.
- A corrupt, truncated, or unsupported data file (e.g. an orphaned `.ost` stub a
  removed account left behind) is skipped with an error, never a crash — it is
  also excluded from `-auto` discovery.
- **Live Exchange/IMAP `.ost` caches vary by Outlook build** and some can't be
  parsed directly. On Windows with classic Outlook, `-outlook` (CLI) or the
  **"Outlook account (via Outlook app)"** wizard option sidesteps this: it drives
  Outlook to write a clean, standard `.pst` per account (`AddStore` + `CopyTo`),
  which then archives normally — Outlook does the parsing, so there's no format
  fragility. It first runs a **Send/Receive** and waits (`-outlook-sync-wait`,
  default 5 min) so the offline window is current. Requires classic Outlook with
  a configured profile (not *New Outlook*); Outlook may show a one-time "allow
  programmatic access" prompt.
- **Completeness caveat:** the COM path can only capture what Outlook has
  **downloaded locally**. Cached Exchange/IMAP accounts keep mail outside the
  "Mail to keep offline" window on the server, so for a *complete* archive set
  that slider to **All** and let Send/Receive finish before running. (This can't
  be automated — it's a per-account profile setting that needs a resync.)

**Thunderbird / mbox**
- Reads **mbox** (Thunderbird's default) and **maildir** stores. A mail
  directory is walked as one source — mbox files and maildir folders, nested
  `.sbd` subfolders — while `.msf` Mork indexes are ignored. A single mbox file
  or maildir folder also works.
- Messages are standard MIME, parsed with `go-message` — HTML/plain parts,
  base64/quoted-printable, charsets and `cid:` inline images all handled.
- Messages flagged deleted-but-not-compacted are still exported.

**Evolution (GNOME)**
- Reads the local **"On This Computer"** store (Maildir++, at
  `~/.local/share/evolution/mail/local`) — its dot-encoded folder hierarchy is
  decoded back into a real folder tree — and each IMAP account's **disk cache**
  (`~/.cache/evolution/mail/<account>`), whose per-folder maildirs are walked in
  full (Evolution shards messages into `cur/NN/` buckets). IMAP account caches
  are labelled by their account name.
- Point `-input` at either directory, or use `-auto`. As with any IMAP source,
  only content Evolution has **cached locally** is present; download for offline
  use first for a complete archive.

**Both**
- Deleting originals is intentionally **not** performed — export only. Remove
  messages from your mail app yourself once you've verified the archive.
- Non-UTF-8 legacy text is decoded as Windows-1252 when not valid UTF-8; unusual
  codepages may need refinement.

> **Validate early:** run against one of *your* real `.ost`/`.pst` files first
> and spot-check a few HTML files — some newer OST variants and encodings are
> best confirmed against real data.

## Development

```sh
make test    # unit tests + integration tests against testdata/support.pst
make vet
```

The integration tests use `testdata/support.pst` (a small sample bundled with
`go-pst`). Because the reader is pure Go, the entire pipeline is exercised on
Linux/macOS without Windows or Outlook.

## License

Licensed under the **GNU Affero General Public License v3.0** — see
[`LICENSE`](LICENSE). In short: you may use, modify, and redistribute it, but if
you run a modified version as a network service, you must offer your users its
source. Third-party dependency licenses (all permissive/AGPL-compatible) are
listed in [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).

Contributions are welcome under the CLA in [`CONTRIBUTING.md`](CONTRIBUTING.md),
which keeps a future commercial/dual license possible.

Copyright © 2026 DiMarco Tech.

## Disclaimer

This software is provided **"as is", without warranty of any kind** — see the
*Disclaimer of Warranty* (§15) and *Limitation of Liability* (§16) sections of
the AGPL-3.0 license. **Use at your own risk.**

It reads your mailbox and writes an archive, and — only when you pass
`-enable-offline` — edits a **backed-up** copy of Thunderbird's `prefs.js`. It
does **not** delete or modify your original mail. Before you delete any
originals yourself, verify the archive is complete (check the run summary and
`attachments-report.tsv`).
