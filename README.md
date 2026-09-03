# mailarchive

Archive a mailbox into a directory of **self-contained HTML files** with
**per-email attachment archives**, mirroring the folder tree — searchable,
browsable with no software, safe to open from disk, and kept current on a
schedule.

It is **source-agnostic**. Everything downstream of a normalized message type is
shared, so the same export / search / GUI stack works across:

| Source | Format | Reader |
|--------|--------|--------|
| **Outlook** (desktop, classic) | `.pst` / `.ost` | pure-Go [`go-pst`](https://github.com/mooijtech/go-pst) |
| **Thunderbird** (and any mbox) | mbox files / mail directory | [`go-mbox`](https://github.com/emersion/go-mbox) + [`go-message`](https://github.com/emersion/go-message) |
| **Evolution** (GNOME) | local Maildir++ store / IMAP disk cache | maildir reader + [`go-message`](https://github.com/emersion/go-message) |
| **Microsoft 365** (server-side) | live mailboxes via Microsoft Graph (app-only, read-only) | `internal/graph` + [`x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) |

Adding another source means adding one reader; the exporter, attachment zipping,
incremental manifest, search index, web UI, folder pages, scheduler and GUI are
untouched. Everything is **pure Go (no cgo)** — it builds and runs on any OS,
needs neither Outlook nor Thunderbird installed, and can process a copied
mailbox offline.

## Start here: which path for my mail program?

Every mail program keeps mail a little differently, so the path from "archive
it now" to "keep it archived" differs too. Find your row; each step is
explained in the sections below.

| Mail program | Prepare once | First archive | Keep it current | What to know |
|---|---|---|---|---|
| **Outlook (classic) with a `.pst`** (POP, or an archive file) | nothing | `mailarchive -input x.pst -out ./archive` | `mailarchive schedule -out ./archive -input x.pst -copy-first -install` | `-copy-first` snapshots the file, so Outlook may stay open. |
| **Outlook (classic) with Exchange / IMAP (`.ost`)** | Account Settings → Change → *Mail to keep offline* → **All**, then Send/Receive | Windows: `mailarchive -outlook -out ./archive` (Outlook writes clean `.pst`s first) or the GUI's *Outlook account (via Outlook app)*. Elsewhere: copy the `.ost` and `-input` it. | `mailarchive schedule -out ./archive -outlook -install` — runs while you are logged in | Some live `.ost` files cannot be read directly; the Outlook-app path is the reliable one. Outlook's one-time "allow programmatic access" prompt must be answered by hand once. |
| **New Outlook / Outlook for Mac** | admin registers an app ([docs/graph-app-setup.md](docs/graph-app-setup.md)) | `mailarchive graph …` | `mailarchive schedule -install -- graph … -client-secret-file FILE` | No local files exist; only the server-side path works. |
| **Thunderbird, IMAP account** | with Thunderbird **closed**: `mailarchive -enable-offline -sync-wait -mode full -input <ImapMail/account> -out ./archive` (it flips "keep messages on this computer" and waits for the sync) | that same command is the first archive | `mailarchive schedule -out ./archive -input <ImapMail/account> -install` | Thunderbird downloads bodies on demand; until it has them a message is archived as *still missing content* and filled by a later incremental run automatically. |
| **Thunderbird, POP / Local Folders** | nothing | `mailarchive -input <Mail/Local Folders> -out ./archive` | `mailarchive schedule -out ./archive -input … -install` | Fully local; nothing to prepare. |
| **Evolution, local ("On This Computer")** | nothing | `mailarchive -input ~/.local/share/evolution/mail/local -out ./archive` | `mailarchive schedule -out ./archive -auto -install` | |
| **Evolution, IMAP account** | in Evolution: Edit → Accounts → account → Receiving Options → *Synchronize remote mail locally*, then let it sync | `mailarchive -input ~/.cache/evolution/mail/<account> -out ./archive` | `mailarchive schedule -out ./archive -auto -install` | Only what Evolution has cached is present; gaps are recorded and filled on later runs. |
| **Microsoft 365 you administer** | one-time app registration ([docs/graph-app-setup.md](docs/graph-app-setup.md)); put the secret in a file readable only by you | `mailarchive graph -out ./archive -tenant T -client-id ID -mailbox a@org -client-secret-file ~/.config/mailarchive/graph.secret` | `mailarchive schedule -install -- graph -out ./archive -tenant T -client-id ID -mailbox a@org -client-secret-file ~/.config/mailarchive/graph.secret` | Nothing on any client; re-runs download nothing already archived. Client secrets expire (≤ 24 months): note the date. |

On any OS, `mailarchive -auto -out ./archive` finds every Outlook (Windows),
Thunderbird and Evolution store, and the GUI does the same with a picker.

After the first run, and any time later:

```sh
mailarchive status -out ./archive      # completeness, last run, schedule — GREEN / WARN / RED with remedies
```

## Quick start (command line)

```sh
mailarchive -auto -out ./archive                       # first archive (incremental by default)
mailarchive status -out ./archive                      # is it complete? did the last run work?
mailarchive schedule -out ./archive -auto              # print the nightly entry (nothing applied)
mailarchive schedule -out ./archive -auto -install     # keep it current, every night at 02:00
mailarchive serve -out ./archive                       # search + read at http://127.0.0.1:8099/
```

## GUI (native dialog wizard)

`mailarchive-gui` is a small wizard built on native OS dialogs (pure-Go
[`zenity`](https://github.com/ncruces/zenity)) — no browser, and on Windows no
console window. Double-click it and it walks you through:

0. **Backup health** — if this wizard has scheduled an archive before, its
   health comes first (last run, completeness, whether the schedule still points
   at this program), with *Remove the scheduled backup* beside *Continue*.
1. **Source** — **Auto-detect my mailboxes** (Outlook, Thunderbird and Evolution
   stores; pick one, or *All of them (and any added later)*), or choose a type:
   Outlook `.pst`/`.ost` file, Thunderbird/mbox folder, Evolution store (Linux),
   a single mbox file, or — on Windows with classic Outlook — **Outlook account
   (via Outlook app)**, which has Outlook export each account to a `.pst` first.
   If nothing is found automatically it asks again rather than guessing.
2. **Choose** the file or folder (native picker), or the auto-detected store(s).
3. **IMAP prep** *(when applicable, auto-detected inputs included)* — for a
   Thunderbird IMAP account it offers to enable offline download and walk you
   through *Download/Sync Now* (then exports everything once); for an Outlook
   `.ost` it shows the equivalent "Mail to keep offline → All" guidance.
4. **Choose** the output folder.
5. **Mode** — Incremental or Full (not asked when the prep just decided it).
6. **Date window** — e.g. `30d`, `4w`, `2026-07-01`, or blank for everything.
7. **Mail app open?** — only for a data *file*; if yes, it snapshots the file
   first to avoid a lock.
8. A **progress** dialog (cancellable — progress is saved), then a **summary**:
   exported, filled, still missing content, source-empty, index errors, and how
   to browse and search the result.
9. **Keep this archive current?** — *daily at 02:00* or *weekly, Sunday 03:00*
   installs a schedule that repeats exactly this export using this very
   program (`mailarchive-gui -job FILE`, no dialogs, no console); the wizard
   warns first if the program runs from Downloads or a temp folder.

A run log is written to `mailarchive.log` in the output folder (rotated at 8 MB).
The GUI drives the exact same export engine as the CLI, so results are identical.

Build the no-console Windows GUI explicitly with:

```sh
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchive-gui.exe ./cmd/mailarchive-gui
```

## Output layout

```
<out>/
  README.txt                                                 # explains all of this to whoever inherits the folder
  index.html                                                 # the list of folders (start here; no software needed)
  <store name>/
    <mirrored folder path>/
      index.html, index-2.html, …                            # browsable, sortable listings (paginated, never truncated)
      2026-07-15_1032_subject-slug_a1b2c3d4.html             # one file per email (UTC time in the name)
      2026-07-15_1032_subject-slug_a1b2c3d4-attachments.zip  # only if it has attachments
      2026-07-15_1032_subject-slug_a1b2c3d4.eml              # the original message, only with -raw
  search.db                                                  # full-text search index (plain SQLite, FTS5)
  attachments-report.tsv                                     # what could not be captured (absent when nothing)
  .mailarchive-manifest.json                                 # export state: what is archived, and what is still missing
  .mailarchive-lastrun.json                                  # the last run: started, finished, result, counts
  .mailarchive-schedule.json                                 # the schedule feeding this archive (when installed)
  .mailarchive.lock                                          # held only while a run is in progress
  <name>.log                                                 # a scheduled job's log (rotated at 8 MB, one .1 kept)
```

Each HTML file stands on its own: links back to its folder page and the archive
root, the metadata header (From / Reply-To / To / Cc / Bcc / Sent / Received
with their original UTC offsets / Message-ID), an *Attachments:* line naming
each archived attachment with a link to the sibling zip, and the message body.
Inline images referenced with `cid:` are embedded as `data:` URIs so the file
renders offline; other attachments go into the sibling `.zip`.

### Safe to open from disk

Every exported page begins with a Content-Security-Policy `<meta>` — the same
policy `serve` sends as a header — so a message opened straight from the file
system runs no script, loads nothing from the network (no tracking pixels,
remote images, fonts or stylesheets), cannot submit forms or rewrite links with
`<base>`, and any mail-supplied `refresh` or policy meta is neutralized. The
content is preserved verbatim; the policy is the control. Browsers enforce the
meta form on `file://`, which is what makes tool-free browsing safe.

## Build

```sh
make build            # CLI  -> bin/mailarchive
make build-gui        # GUI  -> bin/mailarchive-gui
make build-windows    # bin/mailarchive.exe (console) + bin/mailarchive-gui.exe (no console)
```

Requires Go 1.23+. Both binaries are pure Go (no cgo); the Windows executables
cross-compile from any OS.

## Usage

```sh
# Outlook: incremental export (default). Re-running only writes new messages
# and fills in any whose content was missing earlier.
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

# Keep the original messages too (mbox/maildir/Graph sources).
mailarchive -input ~/Mail/inbox.mbox -out ./export -raw
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
| `-mode` | `incremental` (default) skips items already archived and re-examines those still missing content; `full` re-exports everything. |
| `-since` | Only items on/after this: `30d`, `4w`, `12h`, `720h`, or a date like `2026-07-01`. Items already archived but still missing content are re-examined regardless. |
| `-raw` | Also keep each message's original RFC 822 bytes as `<name>.eml` beside the html (mbox/maildir/Graph sources; a `.pst` item has none). |
| `-log` | Write the run log to this file (size-capped, rotated) instead of stderr — what scheduled jobs use. |
| `-manifest` | Manifest path (default `<out>/.mailarchive-manifest.json`). |
| `-copy-first` | Copy each data **file** to a temp snapshot before reading (avoids a lock when the mail app is open; ignored for directories). |
| `-outlook` | **Windows + classic Outlook only.** Have Outlook export each account to a fresh `.pst` under `<out>/_outlook-pst`, then archive those. Use when a live Exchange/`.ost` cache can't be read directly (see below). Refuses with a message elsewhere. |
| `-outlook-sync-wait` | With `-outlook`: run a Send/Receive and wait up to this long (default `5m`) for downloads before creating the PST; `0` skips the sync. Only mail Outlook has downloaded locally is captured — set "Mail to keep offline" to **All** first for a complete archive. |
| `-auto` | Auto-discover mail stores: Outlook files on Windows (`%LOCALAPPDATA%\Microsoft\Outlook\*.ost`, `%USERPROFILE%\Documents\Outlook Files\*.pst`), Thunderbird profiles on any OS (`~/.thunderbird/*/{ImapMail,Mail}/*`, incl. Snap and macOS/Windows), **and** Evolution stores (`~/.local/share/evolution/mail/local` and each `~/.cache/evolution/mail/*` IMAP cache, incl. Flatpak). Orphaned or corrupt Outlook `.ost` stubs (left by removed accounts) are skipped; a file merely locked by a running Outlook is kept. |
| `-index` / `-pages` | Build the search index / folder pages (both default on; set `=false` to skip). |
| `-enable-offline` / `-sync-wait` | Thunderbird IMAP one-time prep (interactive; see below). Not schedulable. |

### Incremental model, and what "missing content" means

Each exported message is recorded in the manifest under a key of
`folder path` + its identity (the RFC 5322 **Message-ID**, or a content hash
when absent). Scoping by folder means the same email filed in two folders is
exported to both, while a re-run still skips each `(folder, message)` pair it
already wrote. Two *different* messages that share one Message-ID in a folder
are both kept.

The manifest also records **what each message is missing**. An IMAP account
(Thunderbird, Evolution) or a windowed Outlook `.ost` holds mail locally only on
demand, so a message can be archived before its body or an attachment has been
downloaded. Such a gap is recorded as **fillable**: every incremental run
re-examines it (cheaply — nothing is rewritten unless the content has arrived)
and fills it once your mail app has downloaded it. A gap the source can never
fill (a Microsoft 365 message that genuinely has no body) is recorded as
**terminal**: reported once, never retried. `attachments-report.tsv` lists both
kinds and is regenerated from the manifest on every run, so nothing found
earlier is ever lost; it disappears when there is nothing to report.

Archives written by earlier versions have no such record: on first use every
entry is marked *not yet re-examined* and the next incremental runs check each
once (the summary and `status` count them down). Deleting the manifest forces
a fresh full export.

Runs are safe to interrupt: files are written to a temp name and renamed into
place, progress is checkpointed every 1000 messages, and a second run on the
same archive is refused while one is in progress (`.mailarchive.lock`).

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
attachments as a zip. The box also understands inline tokens —
`from:bob folder:Inbox after:2025-01 before:2025-07 has:attach invoice` — where a
date can be a whole year (`2025`), a month (`2025-01`) or a day (`2025-01-15`).
`serve` has **no authentication**: it binds to localhost by default and warns
loudly if you bind it elsewhere. Archived pages are served under the same strict
policy they carry, symlinks cannot lead outside the archive, and search snippets
are escaped.

**Terminal search** (no browser):

```sh
mailarchive search -out ./export from:bob invoice
mailarchive search -out ./export -folder Inbox -after 2025-01-01 contract
```

Terminal search understands exactly the same inline tokens as the box
(`from:`, `folder:`, `after:`, `before:`, `has:attach`), so the query above is
identical to `-sender bob invoice`; a token overrides the matching flag. For
scripting, add `-json` (a JSON array of matches on stdout, snippets without the
`<mark>` highlights), or `-paths` (one archive-relative path per match, `-0` to
NUL-separate them for `xargs -0`); in either mode stdout carries only the data
and the "N match(es)" line goes to stderr:

```sh
mailarchive search -out ./export -json from:bob invoice | jq '.[].subject'
mailarchive search -out ./export -paths -0 has:attach | xargs -0 -n1 echo
```

**Browsable pages** (no binary needed): open `./export/index.html` for a folder
listing; each folder has sortable/filterable pages (dates shown in UTC).
`search.db` is an ordinary SQLite database any SQLite tool can query. For ad-hoc
full-text without the UI, `rg -i "your text" ./export` (ripgrep) or add the
folder to Windows Search. `README.txt` in the archive explains all of this to
whoever inherits it.

Indexing and page generation are on by default; disable with `-index=false` /
`-pages=false`. The index is pure-Go SQLite (`modernc.org/sqlite`), so it still
cross-compiles to the Windows binary with no cgo.

## Server-side archiving (Microsoft 365 via Graph)

For a tenant you administer, `mailarchive graph` archives mailboxes **server-side**
— app-only, read-only, **nothing installed on any client**:

```sh
# interactive: secret from the environment
MAILARCHIVE_GRAPH_SECRET='<app secret>' \
mailarchive graph -out ./archive -tenant <TENANT> -client-id <APPID> \
  -mailbox alice@org.example -mailbox bob@org.example

# scheduled (or any time): secret from a file readable only by you
mailarchive graph -out ./archive -tenant <TENANT> -client-id <APPID> \
  -mailbox alice@org.example -client-secret-file ~/.config/mailarchive/graph.secret
```

It authenticates as an Entra app with the read-only **`Mail.Read`** application
permission (issuing only Graph GET requests), walks every folder, and archives
each message's raw MIME through the same HTML/attachment/index pipeline as every
other source. Incremental runs skip already-archived messages by Internet-Message-ID
**without re-downloading** them, so re-runs over huge mailboxes are cheap. To
schedule it, pass the job after `--`:

```sh
mailarchive schedule -interval daily -at 03:00 -install -- graph -out ./archive \
  -tenant <TENANT> -client-id <APPID> -mailbox alice@org.example \
  -client-secret-file ~/.config/mailarchive/graph.secret
```

The secret file must be a regular file (no symlink or pipe), readable only by you
(`chmod 600`), non-empty and small; it is checked the same way when you schedule
and when the job runs. **Client secrets expire** (Entra caps them at 24 months,
admins often issue shorter ones): note the date, and when `status` reports an
authentication failure, rotate the secret in Entra and rewrite the file. One-time
admin setup is in [`docs/graph-app-setup.md`](docs/graph-app-setup.md).

For a mailbox in a tenant you **don't** administer, you can't authorize an app —
use the local paths instead (`-input` a Thunderbird/Evolution store or a `.pst`,
or `-outlook`), or ask that tenant's admin for a Purview PST export.

## Maintenance & automation

### `status` — is the archive complete, and is the backup running?

```sh
mailarchive status -out ./export
```

```
Archive:    /home/alex/export
Messages:   1843 in manifest · 1843 indexed
Incomplete: 12 still fillable · 0 source-empty (terminal) · 0 not yet re-examined — /home/alex/export/attachments-report.tsv
Last run:   2026-09-02 02:00 → ok · exported 5 · filled 2 · took 41s
Schedule:   "mailarchive-3fa2b1c0" installed · daily at 02:00 · runs /usr/local/bin/mailarchive · installed 2026-09-01 on laptop
Posture:    WARN
  WARN: 12 message(s) still missing content — download for offline use in your mail app, then re-run; incremental fills them (see …)
```

The posture fails closed: a schedule with no run ever recorded, a run that never
finished (crash, kill, power loss), a scheduled program that no longer exists or
is not this binary, a last run older than twice the schedule period, or a
scheduler that cannot be queried all show up, each with its remedy. It exits 0
whenever it reports; the posture is the answer.

### `reindex` — reconcile the archive with disk

If you delete, move, or rename exported files by hand, the search index, the
manifest, and the folder pages still point at them. `reindex` reconciles the
archive to what is actually on disk: every entry whose file is gone is pruned
from the index and the manifest, the surviving files stay searchable, orphaned
temp files and zips from an interrupted run are swept, and the folder pages,
the verification report and `README.txt` are regenerated. Nothing else on disk
is deleted.

```sh
mailarchive reindex -out ./export
# reindexed: kept=1843 pruned=12
```

### `schedule` — recurring backups

`schedule` writes a recurring-backup entry for the host OS's scheduler — **cron**
on Linux, a **launchd** LaunchAgent on macOS, **Task Scheduler** on Windows. The
scheduled command is `mailarchive` plus your backup job. By default it **prints**
the exact entry and applies nothing; add `-install` to apply it (idempotently)
and `-remove` to take it back out.

Two forms. The flat form puts the export job's flags on the schedule command;
the `--` form takes any backup job — an export, a `graph` job, or `reindex`:

```sh
# Nightly 02:00 incremental backup of every mailbox found (printed, not applied):
mailarchive schedule -out ./export -auto

# Apply it; re-running -install just updates the single entry.
mailarchive schedule -out ./export -auto -install

# Weekly on Sunday at 03:30, a named job:
mailarchive schedule -out ./export -auto -interval weekly -at 03:30 -name weekly-mail -install

# Windows + classic Outlook: let Outlook write clean PSTs each night (runs while logged in).
mailarchive schedule -out ./export -outlook -install

# Any job after --, validated now exactly as it will run:
mailarchive schedule -install -- graph -out ./export -tenant T -client-id ID -mailbox a@org -client-secret-file ~/.config/mailarchive/graph.secret
mailarchive schedule -interval weekly -install -- reindex -out ./export

# Remove it later (by the archive, or by name):
mailarchive schedule -out ./export -remove
mailarchive schedule -name weekly-mail -remove
```

| Flag | Description |
|------|-------------|
| `-interval` | `daily` (default), `weekly`, or `hourly`. |
| `-at` | Time of day `HH:MM` (default `02:00`; `hourly` uses only the minute). |
| `-name` | Scheduler entry name (default `mailarchive-<hash of the archive path>`: one schedule per archive). Letters, digits, `.`, `_`, `-`; at most 40 characters. |
| `-install` / `-remove` | Apply / uninstall (default: just print the entry). `-remove` finds the entry from `-out` or `-name`. |
| job flags | Either the export flags (`-out`, `-input`, `-auto`, `-mode`, `-copy-first`, `-since`, `-outlook`, `-raw`, …) on the schedule command, **or** the whole job after `--`. Not both. |

What is checked *when you schedule*, not at 02:00: the job's flags parse
exactly as the job will run them; `-enable-offline` / `-sync-wait` are refused
(they are interactive — do that one-time prep by hand first); `serve`, `search`
and `status` are refused (not backup jobs); `-outlook` is refused off Windows; a
`graph` job must name a `-client-secret-file` that passes the same checks the
job applies.

What it leaves behind: the scheduler entry; `<out>/.mailarchive-schedule.json`
naming the schedule, its cadence, the program it runs and the host; and, after
each run, `<out>/<name>.log` (rotated at 8 MB) plus `.mailarchive-lastrun.json`.
`status` reads all of it.

Per OS:

- **Linux (cron):** the job logs itself; anything it cannot log (a crash) reaches
  cron's mail — set `MAILTO` in your crontab if you want that pushed to you.
- **macOS (launchd):** a LaunchAgent under `~/Library/LaunchAgents`; crash output
  goes to `<out>/<name>.stderr.log`.
- **Windows (Task Scheduler):** the task runs a small wrapper,
  `%LOCALAPPDATA%\mailarchive\<name>.cmd`, holding the full command with every
  token quoted (Task Scheduler's own run string is limited to 261 characters and
  has no stderr); a console window appears briefly during the run; the task runs
  only while you are logged in, and a night the PC is off or asleep is skipped —
  `status` shows it. `-remove` deletes the wrapper too.

Upgrading or moving the binary: the entry keeps pointing at the old path. Re-run
`schedule … -install` from the new one (`status` warns "not this binary" until
you do). To roll back to an older release, `schedule … -remove` first: entries
written by this version carry flags older releases do not know.

## Verifying completeness

Every run audits itself for anything **referenced but not fully captured** and
prints a summary; details go to `attachments-report.tsv` (opens in any
spreadsheet) in the output directory, regenerated from the manifest each run:

| kind | class | meaning |
|---|---|---|
| `missing-body` | fillable | the message had no body yet (not downloaded); re-examined by every incremental run and filled when it arrives |
| `empty-attachment` | fillable | a declared attachment produced zero bytes, or its stream failed; likewise re-examined and filled |
| `missing-body` / `empty-attachment` | terminal | the source can never deliver it (a Microsoft 365 message that truly has no body); reported, never retried |
| `unknown` | fillable | archived by an older version before completeness tracking; re-examined once |
| `unresolved-inline-image` | info | the HTML references a `cid:` image that isn't in the message — common in replies/forwards and usually not recoverable |

The run summary shows `filled=… fillable=… terminal=… unknown=…` and a
`Verification:` line pointing at the report. **You no longer need `-mode full`
to fill gaps**: download the content in your mail app and simply run again.

### IMAP: get everything downloaded first

IMAP accounts (Thunderbird, or Outlook in Cached Exchange Mode) keep mail
locally only **on demand**, so an export can miss not-yet-downloaded content.
For **Thunderbird**, two flags automate the one-time prep:

```sh
# With Thunderbird CLOSED first; it enables offline, then waits while you sync.
mailarchive -enable-offline -sync-wait -mode full \
  -input ~/.thunderbird/xxxx.default/ImapMail/mail.example.com -out ./archive
```

- `-enable-offline` flips the account's "keep messages on this computer" setting
  in `prefs.js` (backs it up first; **refuses if Thunderbird is running**).
- `-sync-wait` then pauses so you can start Thunderbird and **Download/Sync Now**,
  watches the store until it stops growing, and proceeds automatically (or press
  Enter).

Do this once, by hand; then schedule the export *without* these flags — from
then on Thunderbird keeps new mail offline and incremental runs fill anything
that was still missing.

For **Outlook**, the tool flags the gap but can't change the setting for you:
set *Account Settings → Change → Mail to keep offline → **All***, then
*Send/Receive → Update Folder*, and run again.

## Sources & limitations

**Outlook (`.pst`/`.ost`)**
- **Classic Outlook only.** The *New Outlook* ("Monarch") app has no `.pst`/`.ost`
  files; use the Microsoft Graph path instead.
- **OST while Outlook is open.** The file may be locked; use `-copy-first`, or
  close Outlook. `-copy-first` copies the whole file to the temp directory on
  every run — mind the disk space for a large `.ost` on an hourly schedule.
- HTML bodies are read directly from `PidTagHtml` (including the binary form
  modern Outlook uses). Non-mail items (calendar, contacts, tasks) are skipped.
- A corrupt, truncated, or unsupported data file (e.g. an orphaned `.ost` stub a
  removed account left behind) is skipped with an error, never a crash — it is
  also excluded from `-auto` discovery.
- **Live Exchange/IMAP `.ost` caches vary by Outlook build** and some can't be
  parsed directly. On Windows with classic Outlook, `-outlook` (CLI) or the
  **"Outlook account (via Outlook app)"** wizard option sidesteps this: it drives
  Outlook to write a clean, standard `.pst` per account (`AddStoreEx` + `CopyTo`)
  which then archives normally — Outlook does the parsing, so there's no format
  fragility. It first runs a **Send/Receive** and waits (`-outlook-sync-wait`,
  default 5 min) so the offline window is current. Requires classic Outlook with
  a configured profile (not *New Outlook*); Outlook may show a one-time "allow
  programmatic access" prompt, which a scheduled run cannot answer — run it once
  by hand first.
- **Completeness caveat:** the COM path can only capture what Outlook has
  **downloaded locally**. Cached Exchange/IMAP accounts keep mail outside the
  "Mail to keep offline" window on the server, so for a *complete* archive set
  that slider to **All** and let Send/Receive finish before running.

**Thunderbird / mbox**
- Reads **mbox** (Thunderbird's default) and **maildir** stores. A mail
  directory is walked as one source — mbox files and maildir folders, nested
  `.sbd` subfolders — while `.msf` Mork indexes are ignored. A single mbox file
  or maildir folder also works.
- Messages are standard MIME, parsed with `go-message` — HTML/plain parts,
  base64/quoted-printable, charsets, `cid:` inline images, Bcc/Reply-To and
  threading headers all handled; `-raw` keeps the original bytes.
- Messages flagged deleted-but-not-compacted are still exported.

**Evolution (GNOME)**
- Reads the local **"On This Computer"** store (Maildir++, at
  `~/.local/share/evolution/mail/local`) — its dot-encoded folder hierarchy is
  decoded back into a real folder tree — and each IMAP account's **disk cache**
  (`~/.cache/evolution/mail/<account>`), whose per-folder maildirs are walked in
  full (Evolution shards messages into `cur/NN/` buckets). IMAP account caches
  are labelled by their account name.
- Point `-input` at either directory, or use `-auto`. As with any IMAP source,
  only content Evolution has **cached locally** is present; enable
  *Synchronize remote mail locally* for the account and let it sync, and later
  runs fill the gaps.

**Names on disk**
- Folder and file names are made safe for every OS (Windows-illegal characters
  and reserved device names, trailing dots, NFC-normalized Unicode). A folder
  whose name had to be altered carries a short `~hash` suffix so two source
  folders never merge; subjects keep their letters in any script; very deep
  trees shorten the subject part of file names. Two folders that differ only in
  letter case, or in NFC/NFD form, can still collide on a case-insensitive file
  system — an acknowledged limit.

**Both**
- Deleting originals is intentionally **not** performed — export only.
- Non-UTF-8 legacy text is decoded as Windows-1252 when not valid UTF-8; unusual
  codepages may need refinement.
- File durability across power loss depends on the archive's file system
  honouring fsync (ext4, APFS and NTFS do; FAT/exFAT sticks do not); the
  namespace guarantee (no partial file ever recorded as done) holds regardless.

> **Validate early:** run against one of *your* real `.ost`/`.pst` files first
> and spot-check a few HTML files — some newer OST variants and encodings are
> best confirmed against real data. Then run `status`.

## Development

```sh
make test    # unit tests + integration tests against testdata/support.pst
make vet
```

The integration tests use `testdata/support.pst` (a small sample bundled with
`go-pst`). Because the reader is pure Go, the entire pipeline is exercised on
Linux/macOS without Windows or Outlook. The invariants and test specs are in
[`docs/scenario-catalog.md`](docs/scenario-catalog.md); the process is in
[`CLAUDE.md`](CLAUDE.md).

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
originals yourself, verify the archive is complete (`mailarchive status`, the run
summary and `attachments-report.tsv`).
