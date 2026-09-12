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
| Platform | CLI | GUI |
|---|---|---|
| Linux x86-64 | `mailarchive-linux-amd64` | `mailarchive-gui-linux-amd64` (needs `zenity`) |
| Linux arm64 | `mailarchive-linux-arm64` | — |
| Windows x86-64 | `mailarchive-windows-amd64.exe` | `mailarchive-gui-windows-amd64.exe` |

Windows GUI: SmartScreen → *More info → Run anyway*. Verify downloads against `SHA256SUMS`.

### What's new
- **Go back in time:** for a live Microsoft 365 (Graph) archive, `mailarchive serve` now shows the mailbox as it was on any past date, Wayback-style — a scheduled daily capture records an append-only timeline, so a message deleted online stays visible at earlier dates (under its then-folder) while the current view follows moves and hides it. Deleting the files and running `reindex` redacts a message across all dates. Deleted Items and Junk Email are excluded from a Graph capture by default (`-include-deleted` / `-include-junk` opt in). A live capture keeps one copy per message and records folder moves in place (no re-download, no duplicate); the manifest upgrades to format v4 once. The date view is served-only — the offline pages still show each message's first-captured folder. See [`docs/goback.md`](../docs/goback.md).
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
- The Outlook-app export path (`-outlook`) and the real Windows/macOS scheduler installs are validated by hand, not in CI (catalog rows MA-60, MA-79, MA-95).

Licensed under **AGPL-3.0**. Provided "as is", without warranty — use at your own risk.
