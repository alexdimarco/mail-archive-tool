# Note for gmail-dedup: slice B1 was built split (2026-11-08)

Written on branch `tb-completeness` by the builder of B1, for whoever folds
`docs/design-gmail-dedup.md` when the two branches meet. The dedup design was not changed on
this branch. Lead's call, 2026-11-08 [B1 split].

## What B1 is

One exported function in `internal/thunderbird`:

```go
func GmailForStore(profileDir, storeDir string) (Gmail, error)
```

It answers `GmailYes`, `GmailNo` or `GmailCantTell` (the zero value) for one Thunderbird store.

- **Decided only by** the matched account's `mail.server.<key>.is_gmail`, as Thunderbird applies
  it: prefs.js, then user.js over it. With no line of its own, the account takes
  `mail.server.default.is_gmail`; with neither, Thunderbird's built-in false (mailnews.js; a
  profile that never connected is a no).
- **The account** comes from `FindAccountForStore`, matched by the store's own folder setting
  (C0c, MA-1195). No account, two possible accounts, a removed account's leftover folder, or no
  account list → can't tell.
- **The read** goes through C0c's reader: `util.ReadRegularFile` (16 MB), the allow-list of lines
  Thunderbird writes, and the typed reading (MA-1270, which now also checks `is_gmail` is true or
  false). A link, pipe, over-cap or missing file, a line Thunderbird never writes, or a
  wrong-typed `is_gmail` → can't tell. The files are read once per call.
- **Never used:** the host name, folder names, folder flags.
- **The error** for can't tell names prefs.js or user.js and the store folder. Its words are a
  log's, not a surface's. It unwraps to `FindAccountForStore`'s error (a `*HandEditedError` when a
  file cannot be read with certainty).

Can't tell is the dedup design's "not Gmail this run, per folder, with one note" (§6's
[GD-9] fold). The sticky `DedupStores` rule is the caller's, not this function's.

## What B1 is not: the All Mail flag

§6's "[dedup foldcheck S7]" note says the All Mail flag (0x8000) is read from
`folderCache.json` "by the same shared function". **That part was not built in B1.**

- `folderCache.json` is read by the completeness design's slice C1a, in the leaf package
  `internal/tbcomplete`, with its own bounds (`util.ReadRegularFile`, 16 MB) and its own
  adversarial pass. A second reader of the same untrusted file inside `internal/thunderbird`
  would be a second parser to review.
- "Which folder is All Mail" (the one folder with 0x8000; none or more than one → none) comes
  from that reader.
- MA-1235's 0x8000 cases ("All Mail found by 0x8000 only, never 0x4000, and absent when two
  folders carry 0x8000"; S7's "from a `folderCache.json` fixture") are proven with C1a. Until
  then the MA-1235 row says they are left for C1a.

## What the dedup design should fold

1. §6's S7 note: All Mail comes from C1a's `folderCache.json` reader, not from
   `GmailForStore`. The design only names All Mail in words (§8.4's "This folder: All Mail",
   restore's All Mail note), so it needs C1a's answer for its words only.
2. §13's B1 row: built as `GmailForStore`; the 0x8000 part moved to C1a.
3. §12: MA-1235 is in the catalog with what B1 proves. MA-1215 was **not** added: its parts
   (a non-Gmail account's All-Mail-flagged folder archived per folder; sticky `DedupStores`)
   are about the capture and the manifest, which do not exist yet. MA-1215's detection clause
   is MA-1235's.
4. §6 says "the run reads prefs.js once and hands the matched account to both features".
   `GmailForStore` reads once per call. If the run also calls `FindAccountForStore` for the same
   store, that is a second read. A slice that wires both can add the answer to `Account` instead.

## Open question for the lead (low confidence)

`is_gmail` is an IMAP server setting in Thunderbird (`nsImapIncomingServer`). B1 does not look at
the account's type. So a hand-written `mail.server.default.is_gmail` true would make a POP
account's store a yes. Thunderbird never writes that line. The completeness check sorts by type
before it asks, so it is unaffected. For dedup, a wrong yes only merges byte-identical copies.
If the lead wants "not IMAP → no", it is one line and one test.
