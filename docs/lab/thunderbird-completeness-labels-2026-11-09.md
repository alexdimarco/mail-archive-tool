# Lab: the labels MailArchive's words point to (2026-11-09)

Lab rows MA-1191 (Thunderbird labels and the ≡ path) and MA-1201 (Gmail's folder-size setting),
run by the lead with the owner's go-ahead ("do the checks", 2026-11-09). Read-only throughout: no
Thunderbird profile was opened, Thunderbird was not started, and nothing in Gmail was clicked or
saved.

## Thunderbird: the installed app's own text (omni.ja), en-US

| Label MailArchive names | 153.6.0 (Windows VM) | 159.0 (owner's Mac) |
|---|---|---|
| ≡ menu item "Account Settings" (`appmenu_accountmgr`, `accountManagerCmd2.label`) | yes | yes |
| Account page "Synchronization & Storage" (`prefPanel-synchronization`) | yes | yes |
| Account page "Server Settings" (`prefPanel-server`) | yes | yes |
| "Keep messages in all folders for this account on this computer" (`allFoldersOffline2.label`) | yes | yes |
| "Don't download messages larger than" (`autosyncNotDownload.label`) | yes | yes |
| "Synchronize all messages locally regardless of age" (`allAutosync.label`) | yes | yes |
| Server Settings' last section "Message Storage" with "Local Directory:" (the store folder) | yes | yes |

How: on the Mac, `unzip -p /Applications/Thunderbird.app/Contents/Resources/omni.ja <entry>`; on
the VM, a read-only PowerShell script (`/Users/alex.dimarco/repositories/.winlab/tblabels.ps1`,
not committed) opening `C:\Program Files\Mozilla Thunderbird\omni.ja` as a zip. Not checked: 160
(not installed on either machine; the 160 labels rest on the earlier source re-read,
`docs/lab/thunderbird-completeness-source-esr153.md`), other languages, and how the pages look on
screen.

Follow-up (polish, not a defect): the "which account" line says Server Settings "shows the folder
its mail is kept in, at the bottom"; the field is labelled "Local Directory:" under "Message
Storage", which the line could name.

## Gmail: Settings → "Forwarding and POP/IMAP"

On the owner's Gmail (Google Workspace) in Chrome, the Forwarding and POP/IMAP settings tab has
"Folder size limits" with the radio options "Do not limit the number of messages in an IMAP folder
(default)" and "Limit IMAP folders to contain no more than this many messages", and a "Save
Changes" button. This matches the Gmail cap note's steps. Not checked: the gear → "See all
settings" path (standard Gmail; not clicked), the chosen option on this account (not read), and
other languages.
