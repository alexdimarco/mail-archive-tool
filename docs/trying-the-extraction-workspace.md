# Trying the Extraction workspace (a first hands-on test)

> This was a one-time test note for the branch `fix/review-2026-10` (October 2026). Some program names and
> cards in it have changed since; for current steps see `README.md`.

For the owner's first real test of the new code on `fix/review-2026-10`. Nothing here is pushed or released.
Everything below was tested against FAKE servers and fixtures only. This is the first time it meets a real
mailbox or a real browser, so expect rough edges and write down what you see.

## What is new, what is proven

| Part | Status |
|---|---|
| `graph` capture of your mailbox (the weekly backup path) | Proven by you earlier. **Changed since:** a failed download or folder is now a recorded gap and the run continues, and the run time limit is 2 hours instead of 10 minutes (FI-S3). Re-check it on a small mailbox first. |
| Extraction workspace: export from Microsoft 365 (new code) | Never run against a real tenant. |
| Extraction workspace: export from your archive, or from another archive folder, read-only (new code) | Never run against a real archive in a browser. |
| The page scripts (Source control, tree, Preview, Start, history cards) | Never run in a real browser. This is the one check the reviews say only you can do. |

## 1. Build and start

From the repository folder (the working copy on branch `fix/review-2026-10`):

    go build -o mailarchive-desktop ./cmd/mailarchive-desktop
    ./mailarchive-desktop -out /path/to/your/archive

Useful flags: `-no-browser` (do not open a browser; use `-copy-link` to get a one-time link in a dialog),
`-no-workspace` (turns the whole workspace off: no Export group, its pages return 404).

Open the one-time link it shows. The address is `http://127.0.0.1:<port>/s/<secret>/...`; if a page says
"Sign-in link needed", open the app from its shortcut or run it again for a fresh link.

## 2. First safe checks (10 minutes, no mailbox needed)

1. Overview shows an **Export** group in the sidebar. Its entry jumps to a card with two links:
   "Extraction workspace" and "Export activity".
2. Open **Extraction workspace**. The **Source** control offers Microsoft 365, "this archive" and
   "another archive folder".
3. Choose **this archive**, press **Load folders**. You should see your archive's stores and folders with
   counts. Tick one small folder. Press **Preview**. Nothing is written yet.
4. Choose a destination folder (Browse, or type an absolute path; **Create folder** makes one). Press
   **Start**. When it ends, open the folder: you should find the messages as `.eml` files, a manifest,
   `SHA256SUMS`, a marker and a `README.txt`.
5. Open **Export activity**: the run shows as a card. Try **Check this export**.

## 3. Then the Microsoft 365 source (small folder first)

Choose **Microsoft 365**, sign in with the device code (the code shows only inside the app's own window),
load folders, pick one small folder, preview, start. Compare the count with what Outlook shows.

## 4. Things to try on purpose (each should explain itself in plain words)

- Cancel a run in the middle, then look at Export activity. You should be offered what to do next.
- Start a run, close the app, reopen it. The old run should say what happened and offer to finish it.
- Pick a destination inside your archive folder. It should be refused, with the reason.
- Another archive folder: pick a folder that is not an archive; it should say so.

## 4b. An archive that came from Thunderbird (or any local mail files)

MailArchive Desktop reads, searches, checks (**Maintenance → Check integrity**, **Rebuild search index**,
**Status & health**) and exports an archive that `mailarchive-gui` or `mailarchive` made from Thunderbird.
**Archive now** is for Microsoft 365, so it is off for such an archive. To keep it current, choose **How often**
in **Settings** and click **Save settings**. The scheduled backup re-reads your Thunderbird mail files, and
**Run now** on the **Scheduled backup** card runs it straight away. The header says what the archive is ("made from local
mail files (for example Thunderbird), not from Microsoft 365").

To open it: if you set no location and started the app without `-out`, the Overview offers the archive the
`mailarchive-gui` wizard used last ("Use this archive?"; nothing changes until you click it). Otherwise type
the archive's folder (the one that holds `.mailarchive-manifest.json`) in **Settings → Archive location** and
click **Save settings**.

### Adding new mail to a Thunderbird archive

New mail comes in through the same archive folder, with the wizard or the command. A run is incremental by
default: it adds only messages that are not already archived.

With `mailarchive-gui` (any OS; the wizard does not pre-fill the archive folder, so choose the same one):
1. Open `mailarchive-gui`. At "What are you exporting?" choose **Thunderbird / mbox mail folder** (or
   **Auto-detect my mailboxes**).
2. Pick your Thunderbird mail folder at "Select the Thunderbird mail folder" (the `Mail/Local Folders` or
   `ImapMail/<account>` folder of your profile; see below).
3. At **Select the output folder** pick this archive's folder, the same one every time.
4. Choose **Incremental** at "Export mode". The summary tells how many messages were added.

With the `mailarchive` command (the same `-out` folder):

macOS / Linux:
```
mailarchive -input "<your Thunderbird mail folder>" -out "<this archive folder>"
mailarchive -auto -out "<this archive folder>"        # every Thunderbird store it finds
```
Thunderbird mail folder, macOS: `~/Library/Thunderbird/Profiles/<profile>/Mail/Local Folders`; Linux:
`~/.thunderbird/<profile>/Mail/Local Folders` (IMAP accounts: `.../ImapMail/<account>`).

Windows (Command Prompt or PowerShell):
```
mailarchive -input "<your Thunderbird mail folder>" -out "<this archive folder>"
mailarchive -auto -out "<this archive folder>"
```
Thunderbird mail folder, Windows: `%APPDATA%\Thunderbird\Profiles\<profile>\Mail\Local Folders`.

`mailarchive -auto -list -out "<folder>"` previews which stores `-auto` would take, without writing. A test
runs this documented command through the command's own flag parser (MA-548), so it stays true.

## 5. The browser check the reviews are waiting for

Please note anything odd on these, in this order:
- Click-through: every button and link on both pages, and the Source control's three choices.
- Keyboard only (Tab, Enter, Space): can you reach and use the tree, Preview and Start?
- Screen reader or zoom, if you use one.
- Anything that says "wasn't listed in this session" or reloads in a loop.

## What to send back

For each problem: what you clicked, what you expected, what it said (copy the text), and a screenshot if
you can. The refusal texts should each say what happened, why and what to do; tell me any that don't.

## Not built yet

A date range in the selection, and an installer option for the deployment switch.

New since the second version: **Extract attachments** is on the workspace page (off by default). With it on,
choose "One zip per message" (the default: one `<name>.attachments.zip` beside each message) or "Loose files"
(a `<name>.attachments` folder). Try it on a small folder with a few attachments, unzip one, and check the
files open and their names look right. Files this tool writes carry no "downloaded from the internet" warning
marker; open them with the usual care.

Also new: a better message parser is switched on for every capture, including the Microsoft 365 `graph` path
you already proved. Messages keep later parts after an unknown character set; parts the tool cannot decode
are kept as `.undecoded` and cut-off attachments as `.partial`, and the run summary and `status` say how many.
Message identities do not change. Please run your usual Microsoft 365 capture once and compare the message
count and a few pages with your earlier archive.

New since the first version of this note: **Readable HTML** is built and ON by default. Each exported
message gets a `.html` page beside its `.eml`, made by the same sanitizing renderer the archive uses.
Try the "Readable HTML" checkbox on the workspace page (untick it to export originals only), open a few of
the `.html` pages in a browser, and check they look right and carry no live content. A message too big for a
readable copy (over 32 MiB rendered) is exported as the original only, and says so. Archives with a damaged PST/OST are handled; the PST fault
fixes are tested on fixtures, not on your real PSTs.
