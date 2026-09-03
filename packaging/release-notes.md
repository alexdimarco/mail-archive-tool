**mail-archive-tool** — a source-agnostic mail archiver (Outlook `.pst`/`.ost`,
Thunderbird mbox/maildir, Evolution, Microsoft 365 via Graph) into self-contained
HTML + per-email attachment zips, with a full-text search index, scheduling,
status, and a native-dialog GUI. Pure Go, no cgo.

### macOS — start here
- **GUI (double-click app):** download **`MailArchive-macos.zip`**, unzip it, then
  because it's unsigned: **right-click "Mail Archive.app" → Open → Open** (once).
  Or in Terminal: `xattr -cr "Mail Archive.app"` then double-click. Universal —
  runs on Apple Silicon and Intel.
- **CLI:** `mailarchive-macos-universal` —
  `xattr -d com.apple.quarantine mailarchive-macos-universal && chmod +x mailarchive-macos-universal && ./mailarchive-macos-universal -h`
- The raw `mailarchive-darwin-*` files are per-arch CLI binaries for scripting;
  most Mac users want the two universal downloads above.

### Linux / Windows
| Platform | CLI | GUI |
|---|---|---|
| Linux x86-64 | `mailarchive-linux-amd64` | `mailarchive-gui-linux-amd64` (needs `zenity`) |
| Linux arm64 | `mailarchive-linux-arm64` | — |
| Windows x86-64 | `mailarchive-windows-amd64.exe` | `mailarchive-gui-windows-amd64.exe` |

Windows GUI: SmartScreen → *More info → Run anyway*. Verify downloads against `SHA256SUMS`.

### What's new
- **Keep it current:** `mailarchive schedule` takes any backup job (export, Graph, reindex), validates it when you schedule it, works with any Windows path, logs to a rotating file, and the GUI offers *Keep this archive current?* after a run. One schedule per archive.
- **Know where you stand:** `mailarchive status -out DIR` — completeness, last run, schedule, GREEN/WARN/RED with remedies; the GUI shows the same on launch.
- **Incremental fills gaps:** mail archived before its content was downloaded is re-examined by every incremental run and filled once it is there; `-mode full` is no longer the remedy.
- **Safe offline:** every exported page carries its own policy — nothing runs or loads from the network when opened from disk; `serve` is hardened too.
- **Legible archive:** README.txt inside the archive, navigation and attachment links on every message page, full headers with original time offsets, paginated folder pages, optional `-raw` originals.

### Known limitations
- Reads **classic** Outlook `.pst`/`.ost`; *New Outlook* and *Outlook for Mac* (`.olm`) are not supported locally — use the Microsoft 365 (Graph) path.
- IMAP mail exports only if downloaded locally; do the one-time prep (`-enable-offline -sync-wait` for Thunderbird, "Mail to keep offline → All" for Outlook, *Synchronize remote mail locally* for Evolution). The archive records what is still missing and later runs fill it; `attachments-report.tsv` and `status` show the count.
- Windows scheduled runs happen only while you are logged in; a console window appears briefly for CLI-scheduled jobs.
- The Outlook-app export path (`-outlook`) and the real Windows/macOS scheduler installs are validated by hand, not in CI (catalog rows MA-60, MA-79, MA-95).

Licensed under **AGPL-3.0**. Provided "as is", without warranty — use at your own risk.
