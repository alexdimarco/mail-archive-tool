**mail-archive-tool** — a source-agnostic mail archiver (Outlook `.pst`/`.ost`,
Thunderbird mbox/maildir) into self-contained HTML + per-email attachment zips,
with a full-text search index and a native-dialog GUI. Pure Go, no cgo.

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

### Known limitations
- Reads **classic** Outlook `.pst`/`.ost`; *New Outlook* and *Outlook for Mac* (`.olm`) are not supported.
- IMAP mail exports only if downloaded locally; use `-enable-offline -sync-wait` (Thunderbird) or set Outlook's "Mail to keep offline" to All. The run's `attachments-report.tsv` flags anything missing.

Licensed under **AGPL-3.0**. Provided "as is", without warranty — use at your own risk.
