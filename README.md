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

## Just want to download and run it?

No build, no command line. Grab the GUI for your machine from the
[**Releases page**](https://github.com/alexdimarco/mail-archive-tool/releases),
double-click it, follow the wizard, then open `index.html` in the archive folder
it makes — that page needs no software to read.

| Your machine | Download | First run |
|---|---|---|
| **Windows** | `mailarchive-gui-windows-amd64.exe` | double-click (SmartScreen → *More info → Run anyway*) |
| **macOS** | `MailArchive-macos.zip` | unzip, then **right-click "Mail Archive.app" → Open → Open** once (it is unsigned) |
| **Linux** | `mailarchive-gui-linux-amd64` | needs `zenity` installed; mark executable and double-click (or run it) |

On macOS the double-click app is **`MailArchive-macos.zip`** (it unzips to
`Mail Archive.app`). The command-line tool is a separate download,
**`mailarchive-cli-macos.tar.gz`** — a Terminal program, so run it from Terminal
(`tar -xzf …`, then `./mailarchive -h`); do not double-click it, or macOS opens
it in a text editor. Both are universal (Apple Silicon and Intel).

The wizard is the [GUI section](#gui-native-dialog-wizard) below.

## Start here: which path for my mail program?

Every mail program keeps mail a little differently, so the path from "archive
it now" to "keep it archived" differs too. Find your row; each step is
explained in the sections below.

| Mail program | Prepare once | First archive | Keep it current | What to know |
|---|---|---|---|---|
| **Outlook (classic) with a `.pst`** (POP, or an archive file) | nothing | `mailarchive -input x.pst -out ./archive` | `mailarchive schedule -out ./archive -input x.pst -copy-first -install` | `-copy-first` snapshots the file, so Outlook may stay open. |
| **Outlook (classic) with Exchange / IMAP (`.ost`)** | Account Settings → Change → *Mail to keep offline* → **All**, then Send/Receive | Windows: `mailarchive -outlook -out ./archive` (Outlook writes clean `.pst`s first) or the GUI's *Outlook account (via Outlook app)*. Elsewhere: copy the `.ost` and `-input` it. | `mailarchive schedule -out ./archive -outlook -install` — runs while you are logged in. Run `mailarchive -outlook` once by hand first to clear Outlook's one-time "allow programmatic access" prompt; a scheduled run cannot answer it. | Some live `.ost` files cannot be read directly; the Outlook-app path is the reliable one. Outlook's one-time "allow programmatic access" prompt must be answered by hand once. Scheduled runs happen while you are logged in; a night the laptop merely sleeps is caught up when it wakes, but a night it is fully powered off is skipped (`status` shows it). |
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
mailarchive -auto -list -out ./archive                 # preview: which stores would -auto archive? (writes nothing)
mailarchive -auto -out ./archive                       # first archive (incremental by default)
mailarchive status -out ./archive                      # is it complete? did the last run work?
mailarchive schedule -out ./archive -auto              # print the nightly entry (nothing applied)
mailarchive schedule -out ./archive -auto -install     # keep it current, every night at 02:00
mailarchive serve -out ./archive                       # search + read at http://127.0.0.1:8099/
```

`-auto` archives **every** mail store it discovers (Outlook on Windows;
Thunderbird and Evolution on any OS). To see that set before committing to it,
run `-auto -list`: it prints the stores that would be archived, one per line
with a rough size, and exits without exporting or creating the output directory.

## GUI (native dialog wizard)

`mailarchive-gui` is a small wizard built on native OS dialogs (pure-Go
[`zenity`](https://github.com/ncruces/zenity)) — no browser, and on Windows no
console window. Double-click it and it walks you through:

0. **Backup health** — if this wizard has scheduled an archive before, its
   health comes first (last run, completeness, whether the schedule still points
   at this program), with *Continue* and *Remove the scheduled backup* — and,
   when the schedule needs it (not installed, or its program was moved or
   replaced), *Repair the scheduled backup*, which re-installs it from this copy
   of the program.
1. **Source** — **Auto-detect my mailboxes** (Outlook, Thunderbird and Evolution
   stores; pick one, or *All of them (and any added later)*), or choose a type:
   Outlook `.pst`/`.ost` file, Thunderbird/mbox folder, Evolution store (Linux),
   a single mbox file, or — on Windows with classic Outlook — **Outlook account
   (via Outlook app)**, which has Outlook export each account to a `.pst` first
   (offered automatically when a live `.ost` is auto-detected). If nothing is
   found automatically it explains why for your OS — including that New Outlook
   and Outlook for Mac keep no local files and are archived server-side via
   Microsoft 365 — and asks again rather than guessing.
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
8. **Keep the originals too?** *(only for mbox/Thunderbird/Evolution sources,
   which carry raw bytes)* — optionally save each message's original `.eml`
   beside its page so you can re-import it into a mail program later.
9. A **progress** dialog (cancellable — progress is saved), then a **summary** in
   plain words (newly downloaded, not-fully-downloaded-yet, empty-at-the-source,
   index errors) with an *Open the archive* button that opens `index.html`.
10. **Keep this archive current?** — *daily at 02:00* or *weekly, Sunday 03:00*
    installs a schedule that repeats exactly this export using this very
    program (`mailarchive-gui -job FILE`, no dialogs, no console); the wizard
    warns first if the program runs from Downloads or a temp folder. A failed
    scheduled run raises a desktop notification so it never fails silently.

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
  BACKUP-NEEDS-ATTENTION.txt                                 # only after a FAILED run: the reason + how to check (removed by the next success)
  ARCHIVE-INTEGRITY-ATTENTION.txt                            # only after a verify found modified/missing files: the counts + how to restore (removed once verify attests)
  .mailarchive-manifest.json                                 # export state: what is archived, and what is still missing
  .mailarchive-lastrun.json                                  # the last run: started, finished, result, counts
  .mailarchive-lastverify.json                               # the last verify: when it ran, whether every file was intact
  .mailarchive-schedule.json                                 # the schedule feeding this archive (when installed)
  .mailarchive.lock                                          # lock file; may remain after a run — its presence does not mean a run is active
  mailarchive.log                                            # the GUI's run log (interactive and GUI-scheduled runs; rotated at 8 MB, one .1 kept)
  <name>.log                                                 # a CLI-scheduled job's run log (rotated at 8 MB, one .1 kept)
```

`BACKUP-NEEDS-ATTENTION.txt` is written only when a run finalizes as *failed* —
so a desktop user notices a broken nightly backup without opening a JSON file —
and is removed automatically by the next run that succeeds. `.mailarchive.lock`
is a lock file that may remain on disk after a run; its presence does **not**
mean a run is active — the real exclusion is an OS-level lock (`flock`) released
when the run process ends.

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
| `-list` | List the mail stores that would be archived (with a rough size) and exit, without exporting or creating the output directory. Pair with `-auto` to preview auto-discovery, or with `-input`. |
| `-enable-offline` / `-sync-wait` | Thunderbird IMAP one-time prep (interactive; see below). Not schedulable. |

### Incremental model, and what "missing content" means

Each exported message is recorded in the manifest under a key built from its
`store` and its identity (the RFC 5322 **Message-ID**, or a content hash when
absent). A **live** source (Microsoft 365 via Graph) is keyed **per mailbox**:
one physical copy of each message, wherever it is filed, with the folder it lives
in recorded over time — so moving a message between folders updates the record
in place (no second copy, nothing re-downloaded) and the served view follows it
(see [Going back in time](#going-back-in-time-point-in-time-view)). This
one-copy-per-mailbox keying holds for a live archive **created with this version**
AND for a **pre-existing** one on its first upgraded run, which collapses any
per-folder duplicates into one record *before* capturing (see [Upgrading an older
archive](#upgrading-an-older-archive)). A **one-shot
local import** (a `.pst`, mbox, or maildir) additionally scopes the key by
**folder**, so the same email filed in two folders — or the same mailbox
imported twice into one `-out` — is kept in each place; a re-run still skips each
`(store, folder, message)` it already wrote. In either case two *different*
messages that happen to share one Message-ID are both kept (each on its own key),
never silently merged.

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
entry is marked *not yet re-examined*, and the next incremental runs re-examine
each once until none remain. Each run's `Verification:` line reports the current
counts (still missing content, source-empty, not yet re-examined, and how many
gaps it re-examined this run); `status` is the standing surface that shows the
*not yet re-examined* backlog shrinking to zero across runs. Deleting the
manifest forces a fresh full export.

Runs are safe to interrupt: files are written to a temp name and renamed into
place, progress is checkpointed periodically (at least every 1000 messages, and
less often as an archive grows very large so the durability writes never
dominate a long run), and a second run on the same archive is refused while one
is in progress (`.mailarchive.lock`).

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
identical to `-sender bob invoice`; a token overrides the matching flag. A
token's value may contain spaces if you quote it — `folder:"Sent Messages"` or
`from:'a b'` — otherwise the value ends at the first space (a bare
`folder:Sent Messages` would match nothing); the box and the terminal read the
quotes the same way. For
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

## Going back in time (point-in-time view)

A scheduled **live** capture does more than keep the archive current — it records
a **timeline**, so `serve` can show what the mailbox looked like on a past date,
Wayback-style. On the search-and-read page a **Go back in time →** link opens
`/goback`: a date track of the days the archive was captured (newest first) and,
for each, the folders and messages as they stood that day. A full how-to is in
[`docs/goback.md`](docs/goback.md).

Two projections, both rendered by `serve`:

- **Current** (`/goback`) — every message under the folder it lives in **now**,
  with messages that have since left the mailbox hidden. This *follows the live
  mailbox*: a message moved from Inbox to a project folder shows under the
  project folder.
- **As of a date** (`/goback?at=2026-07-15`, or click a date in the track) — the
  timeline folded to the end of that day: each message under the folder it was in
  **then**, including a message that has since been deleted online (shown under
  its last-known folder for any date before it went away, and again after a
  restore).

**This is a `serve` feature only.** The static `index.html` and folder pages you
open straight from disk — with no tool at all — always group each message under
the folder it was **first captured** in, and never change: they follow neither a
move nor the date track. That is deliberate. Those pages carry no script, which
is what keeps them safe to open from a file system (see [Safe to open from
disk](#safe-to-open-from-disk)); the current-follows-the-mailbox and go-back
views are computed by the running server, so they exist only while `serve` runs.
There is no offline time slider.

### Where the timeline comes from

A live capture — `mailarchive graph` today (IMAP later) — writes an append-only
log, `.mailarchive-history.jsonl`, beside the manifest. Each run appends only
what changed since the last: a message newly seen, one that moved folders, one
that is **gone** (present last run, absent now), and one that is **present
again**. A **one-shot local import** (a `.pst`, mbox, or maildir) records no
timeline — its identity is folder-scoped and it has no notion of "the same
mailbox over time" — so go-back is a live-source feature. When there is no
timeline, `serve` says so ("Go-back is unavailable … showing the current mailbox
only") rather than pretending.

### Granularity is your run cadence — so fetch daily

Go-back can only distinguish the days it actually observed. A message that
appeared and was deleted **between** two runs is never seen; a move is dated to
the run that first observed it, not to the moment it happened. **The remedy is to
capture daily** — `schedule` defaults to a daily run for exactly this reason:

```sh
mailarchive schedule -interval daily -at 03:00 -install -- graph -out ./archive \
  -tenant T -client-id ID -mailbox a@org -client-secret-file ~/.config/mailarchive/graph.secret
```

The finer your cadence, the finer the timeline; daily is the recommended floor.

### Two meanings of "gone"

It matters which one you mean:

1. **Deleted from the mailbox** — a *timeline event*. The message's archived file
   is **kept on disk**. It disappears from the *current* view, but any past date
   **before** the deletion still shows it, under the folder it lived in then.
   Nothing is destroyed; the archive is a superset of the live mailbox over time.
   If it comes back later (restored from Deleted Items), a *present-again* event
   is recorded and the timeline reads absent only between the two.
2. **Removed from the archive** — a *redaction*. You delete the exported files
   yourself, then run `reindex`. Now the message shows at **no** date. `serve`
   intersects every date's view with the files actually on disk, so deleting the
   files alone already hides it everywhere; `reindex` then compacts the log,
   dropping that message's events so the redaction is permanent across all dates.
   This is how you take something out of the archive for good.

The archive never deletes a message file on its own (only a normal run keeps
every file, R13) — a redaction is always something *you* do.

### Deleted Items and Junk are excluded by default

So that the everyday "delete a message" — which, in Outlook/Exchange, *moves* it
to Deleted Items — does not fill the archive with trash, a `graph` capture
**skips the Deleted Items and Junk Email folders by default**, by their resolved
well-known-folder ids (so it holds whatever language the mailbox is in). A
message you delete therefore becomes **gone** in the timeline (its earlier copy
kept, per above), not re-captured under Deleted Items. To archive those folders
too, pass `-include-deleted` / `-include-junk`; their messages are then captured
and timelined like any other. The CLI, the GUI and `schedule` all surface this
choice — see [Server-side
archiving](#server-side-archiving-microsoft-365-via-graph).

### Upgrading an older archive

The timeline and one-copy-per-mailbox keying come with manifest **format v5**. The
first run of this version over an older archive migrates the manifest **once**, in
place: it fills the per-message timeline fields (each existing message marked
present, in its current folder, first seen when captured) and — for a **live
(Graph)** archive — it **collapses** any per-folder move-duplicates into one
mailbox-wide record and re-keys the search index to match, *before* the first
walk. So each still-present message is then recognised by its Internet-Message-ID
and is **not** re-downloaded: the archive consolidates to one copy per message on
that first upgraded run, at no re-fetch cost. A collapsed duplicate's file is
**never deleted** (R13) — it stays on disk and is reached only by a later
redaction (`reindex`). Manifest **format v6** adds the per-message Graph immutable
id used to close reused-Message-ID gaps on the live path (below). The forward guard
refuses any archive whose stored version is above 6, so an older binary cannot
silently corrupt a v6 archive; as with every format bump, **do not run an older
`mailarchive` against a v6 archive** (see
[Upgrading an existing archive](#upgrading-an-existing-archive)).

One historical caveat, now **closed on Graph for messages captured under v6**: two
*genuinely different* messages that reuse one Internet-Message-ID are told apart on
the live `graph` path by the mailbox's per-message **immutable id** together with a
body-inclusive **content hash** recorded at capture — the tool downloads a reuse
whose id it has not archived and keeps both when their content differs (even when
their envelopes are identical), or adopts a reissued-id copy of the same content in
place. Existing (pre-v6) archives keep the Message-ID floor for their
already-captured messages (the tool does not re-download to backfill the new
signals — no upgrade storm); messages captured fresh under v6 get the full closure.
Sources without a per-message id (IMAP / local imports) keep the floor too. See
[Limits](docs/goback.md#limits-read-these) for the full, bounded edge
list. If a large existing live archive matters, you can still start a fresh
capture into a new `-out` to get
one-copy-per-message from the first run. (A one-shot local `.pst`/mbox/maildir
import is unaffected: it is folder-scoped by design.)

`mailarchive status` reports the timeline's health on a **History** line (how
many runs and events, GREEN, or a WARN/RED with a remedy if the log is torn or
unreadable); `serve` announces "go-back partial" rather than silently showing
current-only when the log is damaged.

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
permission (issuing only Graph GET requests), walks every folder — **except
Deleted Items and Junk Email, which are excluded by default** (pass
`-include-deleted` / `-include-junk` to archive them; the exclusion is by
resolved well-known-folder id, so it holds whatever the mailbox's display
language is) — and archives each message's raw MIME through the same
HTML/attachment/index pipeline as every other source. Incremental runs skip already-archived messages by Internet-Message-ID
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
Archived range: 2011-03-08 – 2026-09-01 (UTC)
Incomplete: 12 still missing content · 0 source-empty (never fillable) · 0 not yet re-examined — /home/alex/export/attachments-report.tsv
            (still missing content = not downloaded yet; fills on the next run)
Fixity coverage: 1843 of 1843 records recorded (run `mailarchive verify -out "/home/alex/export"` to check the bytes)
Extractable: 1843 of 1843 records have a preserved original (.eml) on disk
Last run:   2026-09-02 02:00 → ok · exported 5 · filled 2 · took 41s
Last verify: 2026-09-01 02:05 → attested
Schedule:   "mailarchive-3fa2b1c0" installed · daily at 02:00 · runs /usr/local/bin/mailarchive · installed 2026-09-01 (UTC) on laptop
Posture:    WARN
  WARN: 12 message(s) still missing content — download for offline use in your mail app, then re-run; incremental fills them (see …)
```

The **Fixity coverage** line is coverage, not integrity: it counts how many
records carry a recorded digest, read from the manifest — it hashes nothing.
Running `mailarchive verify` (below) is what checks the bytes; its verdict shows
up on the **Last verify** line — `attested`, or `NOT attested (modified N,
missing N, unrecorded N)` — and a scheduled verify that finds modified or
missing files turns the posture RED with the restore/re-export remedy.

The **Extractable** line answers "can I migrate this archive out with `extract`?"
It counts how many records have a preserved original (`.eml`) present on disk —
the same file-presence signal `extract` drains and `verify` counts, so the three
never disagree (it Lstats each record, hashing nothing). When some records have
none it names both remedies rather than a blanket "re-archive": a
mbox/maildir/Microsoft 365 source keeps an `.eml` only when captured with `-raw`,
while an Outlook `.pst`/`.ost` never carries one — for those, keep the `.pst`
itself to migrate. It is informational and never changes the posture.

The posture fails closed: a schedule with no run ever recorded, a run that never
finished (crash, kill, power loss), a scheduled program that no longer exists or
is not this binary, a last run older than twice the schedule period, a last
verify that was not attested, a manifest that exists but cannot be read (a newer
format, or corrupt), or a scheduler that cannot be queried all show up, each with
its remedy. It exits 0 whenever it reports; the posture is the answer. For a
machine-readable form see [Machine-readable output](#machine-readable-output).

### Machine-readable output

`status -json` and `verify -json` emit typed, versioned JSON on stdout (stderr
carries any operator conversation) so a monitor or CI gate keys on fields, not
prose.

**`status -json` (version 2).** Top-level keys: `version`, `posture`, `reasons`,
`reason_codes`, `out`, `messages`, `indexed`, `fillable`, `terminal`, `unknown`,
`fixity`, `extractable`, `last_run`, `last_verify`, `schedule`. (`extractable` is
a backward-compatible addition at version 2 — a new optional key, so a consumer
that ignores unknown keys is unaffected and the version is not bumped.)

- `posture` is one of `GREEN` · `WARN` · `RED`.
- `reasons` is the human WARN/RED lines; `reason_codes` is a **parallel** array
  (same length, same order) of stable snake_case codes, so a gate matches a code
  instead of wording that can change. The codes: `manifest_unreadable`,
  `incomplete_content`, `unexamined_entries`, `index_behind`, `cloud_sync`,
  `lastrun_unreadable`, `never_ran`, `run_failed`, `auth_remedy`,
  `run_in_progress`, `run_stuck`, `run_never_finished`, `run_cancelled`,
  `stale`, `not_attested`, `verify_unrecorded`, `verify_stale`,
  `descriptor_unreadable`, `no_schedule`, `moved_archive`, `other_host`,
  `scheduler_unavailable`, `not_installed`, `exe_missing`, `exe_moved`.
- `fixity` is `{records, with_fixity}` (coverage, not integrity), or `null` when
  there is no manifest.
- `extractable` is `{records, records_with_eml}` — how many records have a
  preserved original (`.eml`) present on disk (file presence, the same signal
  `extract` and `verify` use), or `null` when there is no manifest.
- `last_run` is `{status, started, finished, error?, exported, filled}`, where
  `status` is one of `ok` · `failed` · `cancelled` · `running` and `finished` is
  `null` while a run is in progress.
- `last_verify` is `verify`'s own verdict — `{status, started, finished,
  attested, records, with_fixity, checked, ok, modified, missing, unrecorded,
  unexpected, recorded, exit_code}` — or `null` when no verify has run.
- `schedule` is `{name, state, interval, at, exe, host}` (present only when a
  schedule is recorded).

`status` always exits 0: the posture is the answer, so read `posture` /
`reason_codes` rather than the exit code.

**`verify -json` (version 1).** Keys: `version`, `attested` (the verdict as a
bool), `out`, `records`, `with_fixity`, `checked`, `ok`, `modified`, `missing`,
`unrecorded`, `unexpected`, `recorded`, `problems` (`[{path, kind, detail}]`,
`kind` ∈ `modified` · `missing` · `unrecorded` · `unexpected`), and `truncated`
when a per-category list was capped. The **exit code** is the verdict too: `0`
attested · `2` not attested · `1` refusal.

**`search -json`** is a bare JSON array of results, while the running server's
`/api/search` wraps the same results in an envelope, `{total, results, limit,
offset}`. The CLI's default `-limit` is 20; the API's default limit is 50.

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

### Rebuilding the search index

If `search.db` is lost, corrupted, or left out of a copy — but the archive's
manifest (`.mailarchive-manifest.json`) and the exported per-message files are
still there — you can reconstruct the index and the folder pages from the
archive itself, with no access to the original mail source and no network:

```sh
mailarchive reindex -rebuild -out ./export
# rebuilt=1843 (from-eml=1200 re-derived=643 unrecovered-fields=0) pruned=0
```

Rebuild reads only the archive. For each message it prefers the preserved
original bytes: if you archived with `-raw`, the sibling `<stem>.eml` is parsed
back at full fidelity (`from-eml`). Otherwise — every PST/OST archive, and
anything archived without `-raw` — the message's index fields are re-derived
from the archived `<stem>.html` the tool wrote (`re-derived`), which recovers
the **searchable text**, not the original wire bytes. The summary reports the
split, plus the count of fields a page was too old or too damaged to yield.

Rebuild never touches a message file (`.html`/`.zip`/`.eml`) and never contacts
the source; it does regenerate the folder and root `index.html` pages, and a
re-derived record's folder-table columns can be less complete than its intact
message page. The fresh index is built beside the old one and swapped in only on
success, so an interrupted rebuild leaves your existing index untouched. It
requires the manifest — rebuilding the manifest itself is out of scope; if a
copy that skips dotfiles dropped it, restore it from a backup.

For a byte-faithful index rather than re-derived text, archive with `-raw` (so
the `.eml` originals are on disk to parse), or re-export from the original
source. Plain `reindex` on an archive whose index is gone points you here.

### Migrating the archive out (mbox/eml)

To move the archive into another mail system, `extract` writes each message back
out as standard interchange — **mbox** (one mboxrd file per folder) or **eml**
(one byte-exact `.eml` per message, mirroring the folder tree):

```sh
# One mbox file per folder, under ./out:
mailarchive extract -out ./export -format mbox -dest ./out
# emitted=1843 skipped=0
# One .eml per message, mirroring the tree:
mailarchive extract -out ./export -format eml  -dest ./out
```

`extract` is **faithful-only**: it copies each message's *preserved original
bytes* — the `<stem>.eml` kept at capture with `-raw` — and never synthesizes or
re-serializes a message, so nothing it emits is a forgery. A record with no
preserved bytes is counted, named, and skipped, never invented. That means:

- An **Outlook `.pst`/`.ost`** archive has no originals at all (Outlook items
  carry no RFC 822 bytes), so `extract` emits nothing from it.
- An **mbox / maildir / Microsoft 365 (Graph)** archive is extractable only if
  it was captured **with `-raw`**. Without `-raw`, no `.eml` was kept, and both
  the export summary and `mailarchive verify` warn you of this — re-archive with
  `-raw` while you still have the source.

When a record carries recorded fixity, its `.eml` is checked against it before
being emitted (a mismatch is skipped, not written); otherwise the bytes are
emitted *as stored* — run `mailarchive verify` first if you need them attested.
mbox output is **mboxrd**: a reader that unquotes `>From ` lines recovers the
exact original bytes; an mboxo reader does not, so prefer `-format eml` for a
consumer of unknown flavour, which is byte-exact.

`-dest` must be an **empty** directory (or pass `--overwrite`), must **not
overlap `-out`**, and needs room for the full `.eml` volume this copies. Output
is atomic and idempotent — a re-run replaces files, never doubles them.

`extract` holds the archive's exclusive lock for its whole run, so run it
**outside the backup window**; it is an operator-driven migration, not a backup,
and cannot be scheduled. Its exit status is `0` when the whole set was emitted,
`3` when some records had no preserved bytes (partial — including a PST-only
archive, where nothing is extractable), and `1` on refusal or error.

### Upgrading an existing archive

This version scopes each message's identity to its store, so two mailboxes
archived into one `-out` — two profiles that both call their store "Local
Folders", two Outlook files both named "Outlook Data File" — no longer collide
and lose one copy. The first run (or `reindex`) after upgrading re-scopes the
existing manifest and search index once, in place, by content — nothing on
disk is renamed or rewritten. It is logged:

```
re-scoped 4213 manifest entries by store (one-time upgrade; cost scales with archive size)
migrating index keys (4213 rows)
```

The one-time cost is proportional to the archive size; later runs do no such
work. **Once an archive has been written by this version, do not run an older
`mailarchive` against it.** A shared or synced `-out` must be written only by
upgraded copies: an older binary does not understand the newer format and,
lacking a forward guard, will rewrite the manifest in the old shape and write
a second, duplicate copy of each touched message. This version repairs such an
excursion by content on its next run (the old-shape entries are re-scoped and
de-duplicated), but the leftover duplicate files remain on disk — so the safe
rule is to upgrade every machine that writes the same archive.

This version also adds the go-back **timeline** and a manifest **format v5**;
**v6** then adds the per-message Graph immutable id that closes reused-Message-ID
gaps on the live path. The forward guard refuses any archive whose stored version
is above 6, so an older binary cannot silently corrupt a v6 archive. One-copy-per-message live
keying — a message stored once per mailbox with its moves recorded in place — is
the model for a live (`graph`) archive **created with this version** AND for a
*pre-existing* one on its first upgraded run, which **collapses** any per-folder
duplicates into one record and re-keys the index *before* capturing, so each
still-present message is recognised by its Internet-Message-ID and not
re-downloaded (a collapsed duplicate's file stays on disk — R13 — reached only by
a later redaction). See [Upgrading an older
archive](#upgrading-an-older-archive) for the detail (including the rare
reused-Message-ID caveat) and [Going back in
time](#going-back-in-time-point-in-time-view) for what the timeline gives you.

The re-scope re-exports nothing, so it does not backfill fixity: the legacy
bytes are recorded, not re-hashed, so they cannot be attested as pristine. A
freshly-upgraded archive therefore shows `Fixity coverage: 0 of N records recorded`,
and `verify` reports NOT-ATTESTED, until the next full re-export or an explicit
`mailarchive verify -record` baselines the current bytes. This is expected, not
data loss — the files themselves are untouched.

If an older `mailarchive` (or a newer-format archive opened by an older build)
has already touched a shared `-out`, trust the message the export or `verify`
prints — it names the format version and the upgrade remedy ("written by a newer
mailarchive (format version N)…"). Upgrade every binary that writes the archive
before running it again.

### `schedule` — recurring backups

`schedule` writes a recurring-backup entry for the host OS's scheduler — **cron**
on Linux, a **launchd** LaunchAgent on macOS, **Task Scheduler** on Windows. The
scheduled command is `mailarchive` plus your backup job. By default it **prints**
the exact entry and applies nothing; add `-install` to apply it (idempotently)
and `-remove` to take it back out.

Two forms. The flat form puts the export job's flags on the schedule command;
the `--` form takes any backup job — an export, a `graph` job, `reindex`, or `verify`:

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
(they are interactive — do that one-time prep by hand first); `serve`, `search`,
`status` and a nested `schedule` are refused (not backup jobs); `-outlook` is
refused off Windows; a `graph` job must name a `-client-secret-file` that passes
the same checks the job applies.

What it leaves behind: the scheduler entry; `<out>/.mailarchive-schedule.json`
naming the schedule, its cadence, the program it runs and the host; and, after
each run, `<out>/<name>.log` (rotated at 8 MB) plus `.mailarchive-lastrun.json`.
`status` reads all of it.

Per OS:

- **Linux (cron):** cron runs the job whether or not you are logged in, as long
  as the machine is on and `crond` is running — so a missed run means the machine
  was off, not that you were logged out. The job logs itself; anything it cannot
  log (a crash) reaches cron's mail — set `MAILTO` in your crontab if you want
  that pushed to you. On a host with no `crontab` binary (a container, a
  systemd-timer-only box), `status` reports that the scheduler cannot be queried.
- **macOS (launchd):** a LaunchAgent under `~/Library/LaunchAgents`; crash output
  goes to `<out>/<name>.stderr.log`.
- **Windows (Task Scheduler):** the task is defined by an XML file kept next to
  the wrapper — `%LOCALAPPDATA%\mailarchive\<name>.xml` beside `<name>.cmd` — and
  installed with `schtasks /Create /XML`, then confirmed with `schtasks /Query`.
  The wrapper holds the full command with every token quoted and sends crash
  output to `<out>/<name>.stderr.log`; a console window appears briefly during
  the run, which runs while you are logged in (the task is registered with an
  interactive-logon identity, so it fires as you, without a stored password). The task **catches up a missed
  start** when the PC next wakes and **runs on battery**, so a night the machine
  merely slept is not lost — but it **cannot power on an off machine** (a night
  the PC is off is still skipped; `status` shows it). `-remove` deletes the task,
  the wrapper and the XML.

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

Each run prints a single `Verification:` line summarizing the manifest: how many
messages are **still missing content**, how many are **source-empty** (the
source can never deliver them), how many are **not yet re-examined**, and — when
an incremental run re-examined fillable gaps — `re-examined=<count>`. When a
report file exists the line ends with `— details in …/attachments-report.tsv`;
with nothing to report there is no path and no file. (The `Done.` line above it
carries the per-run counts, including `filled=<n>`.) **You no longer need
`-mode full` to fill gaps**: download the content in your mail app and simply
run again.

### Verifying the files themselves (fixity)

Completeness above is about what was *captured*. **Fixity** is about whether the
captured files are still intact on disk. `mailarchive verify -out DIR` re-hashes
every archived file the manifest records and reports each as **ok**, **modified**
(its bytes changed, or a symlink or non-regular file now stands in its place),
**missing** (gone from disk), or **unrecorded** (no digest on record yet); a
stray `.html` / `-attachments.zip` / `.eml` under a store directory that no
record owns is reported as **unexpected**.

Bit-rot, truncation and accidental edits are detected **for files written by
this version or later, or baselined with `verify -record`** — an archive created
by an older mailarchive shows every file as `unrecorded` until it is re-exported
or baselined. `verify -record` hashes the current bytes of every unrecorded file
and stores them as the fixity *from now on*: a baseline of the bytes as they are
today, **not** proof the legacy bytes were ever pristine. Fixity detects
*change*, not authorship — the manifest is not signed, so anyone who can rewrite
a file can rewrite its recorded digest too.

The exit code is the verdict: **0** attested (everything checked and intact,
nothing unrecorded), **2** not attested (something modified, missing, or
unrecorded — each named), **1** a refusal (no archive, or a locked archive). Add
`-json` for a machine-readable report (it carries an explicit `version` and
`attested` field — see [Machine-readable output](#machine-readable-output)).
`mailarchive status` shows only coverage ("Fixity coverage: N of M records
recorded") without hashing anything — `verify` is what checks the bytes.

A **freshly-upgraded archive has no fixity yet**: the one-time re-scope
re-exports nothing, so legacy bytes cannot be attested as pristine. Such an
archive shows `Fixity coverage: 0 of N records recorded` and `verify` reports
every file `unrecorded` (NOT attested) until the next full re-export or an
explicit `mailarchive verify -record`. This is expected, not data loss.

A scheduled verify's verdict **is recorded** — in `.mailarchive-lastverify.json`,
separate from the export last-run record — so `mailarchive status` shows it on
its `Last verify` line: modified or missing files turn the posture RED (restore
or re-export), while an unrecorded-only gap is a WARN. A verify
that finds modified or missing files also writes `ARCHIVE-INTEGRITY-ATTENTION.txt`
at the archive root (removed automatically once a later verify attests), so even
a headless nightly check leaves a plain-language notice on disk.

`verify` reads every byte of every archived file and holds the archive's
exclusive lock for its whole run, so a scheduled backup that fires meanwhile
refuses and records nothing. **Run it outside the backup window** — or schedule
it in its own slot, away from the backup, with an explicit `-name` so it does
not collide with the backup schedule (both derive their name from `-out`):

```sh
mailarchive schedule -interval weekly -at 05:00 -name mailarchive-verify -install -- verify -out ./archive
```

A scheduled verify automatically gets its own entry name (`mailarchive-<hash of
-out>-verify`), distinct from the backup's, so the two coexist and neither
overwrites the other — you don't need to pass `-name`. If you point the verify at
the **same cadence and time** the archive's backup already uses, `schedule`
refuses (the two would hold the lock against each other on every overlap); pick a
different `-at` or `-interval`, as above.

> **Upgrading a shared `-out`:** an archive a newer mailarchive has written must
> not then be written by an older one (a shared or cloud-synced output). Older
> binaries have no forward-version guard; the current format self-heals a single
> such excursion, and `verify` reports the duplicate files an old binary leaves
> behind as `unexpected`.

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
- **OST while Outlook is open.** An exclusively locked `.ost` cannot be read
  *or* copied: **close Outlook**, or use **`-outlook`** (below) to have Outlook
  write a fresh PST. `-copy-first` helps only for a file that can still be opened
  for a *shared* read (many locked files, but not an exclusive lock) — it
  snapshots the whole file onto the archive's own volume before reading; mind the
  disk space for a large `.ost` on an hourly schedule.
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

**Original headers (every source)**
- Each message page shows the transport headers **as stored by the source** in a
  collapsed "Transport headers as stored (unverified)" panel — sender-influenced
  text (the `Received` chain and `Authentication-Results` can be forged), shown
  as-is, not proof of provenance; for PST it is Outlook's decoded copy, often
  absent for items that never crossed the internet.

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
