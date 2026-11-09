# Friction review: completeness slice C3's words

Walked 8 November 2026, Windows VM clock 6:34 PM to 6:46 PM, then the Mac (EST). Process:
`../assurance-kit/process/friction-review.md` and `METHODOLOGY.md`. Also read: `docs/ux-contract.md`
(X-series), the repo `CLAUDE.md`, `docs/design-thunderbird-completeness.md` §4.2, §6, §7 and §13's
"Built 2026-11-08 [C3]" note, and every sentence of `internal/state/tbwords.go`, with its callers
(`internal/health/tbcompleteness.go`, `internal/wizard/jobrun.go`, `internal/wizard/wizard.go`
`exportSummaryFor`, `internal/desktop/runhistory.go`, `internal/desktop/archives.go`,
`cmd/mailarchive/main.go` `printSummaryFor`). Windows judged first.

**Verdict: SHIPPABLE_WITH_NOTES.** Every walked cell works: each surface says the same words, the
words are true, "complete" and "everything" appear only when every account may say so, and every
case that cannot claim complete says why in one sentence. No cell is a dead end. But the words are
not yet right for a non-technical Windows person, and MailArchive is about to go public. Ten
findings are medium (M1 to M10). Nine are word changes (Type III, buildable-away), which the kit
says are built before ship. One (M1, how an account is named) touches a design rule and needs the
owner's ruling (Type II). The exact replacement words are in §3. They are proposals for the lead
and the owner; this review does not self-certify them.

## Header

| | |
|---|---|
| Reviewed commit | e85d840, branch `tb-completeness` (worktree pulled, already up to date) |
| Programs | Built from a copy of e85d840 in the main checkout's untracked `.claude/fric-c3/src`: `mailarchive.exe` and `mailarchivew.exe` (`GOOS=windows GOARCH=amd64`, the second with `-H=windowsgui`), the Mac `mailarchive`, and a **lab-only helper** `c3lab` (build tag `c3lab`; never in the worktree). The helper makes the throwaway profiles, prints the wizard's end summary for a real run (it calls the wizard's own `exportSummary` after `app.Run` with the wizard's options), and installs the wizard's run-job (daily, or "Only when I click Run now") without its dialogs |
| Profiles | Made-up Thunderbird-shaped profiles only, under `C:\Users\Public\tb-c3\<cell>` on the VM and `.claude/fric-c3/mac/tb` on the Mac: `prefs.js` and `compatibility.ini` (153.6.0) as Thunderbird writes them, `ImapMail/<host>/` stores with mbox files and `.msf` files, `Mail/Local Folders`, and a `folderCache.json` keyed by the native paths. Never a real profile; Thunderbird itself was not started |
| Own settings | Every program ran with `APPDATA` and `LOCALAPPDATA` (Mac: `HOME`) pointed at a lab folder and the Desktop with `-config C:\Users\Public\c3lab\cfg\config.json`. Cell B's daily Task Scheduler entry (`mailarchive-45a7d635`) and its Mac launchd twin were removed at the end (checked: none left); the walk's Desktops were stopped |
| Browser and input | Windows: Microsoft Edge in the VM user's session, real mouse clicks and keys (`P:\fric-c3\ui.ps1`). Mac: Chrome |
| Evidence | 34 VM screenshots in `.claude/fric-c3/shots/` (private, not committed); each cell's command-line output, `status`, `status -json` and wizard summary under `C:\Users\Public\c3lab\results\` and `.claude/fric-c3/mac/res/` |
| Cells walked | Windows 15 profiles (A to O) × command line, `status`, `status -json`, the wizard's words; MailArchive Desktop in Edge for the archive card (all 15), Recent backups and Status & health (C, G, I), Run now's note (C plaintext, I encrypted). Mac: the 14 plaintext profiles × the same four, and the Desktop in Chrome (card, Recent backups, Status & health for L) |
| Functioning | 15 of 15 fully. High-friction cells: 3 (D, L/O, G), all by words, none blocking |
| Exit codes | Every backup exited 0 (X1: a behind run is a success) |

## 1. The cells and what each said

Account "imap.fastmail.com" is the store folder of a made-up account named `sam@fastmail.com` in
Thunderbird; Gmail's is `sam.walker@gmail.com` (store folder `imap.gmail.com`). The words below are
the command line's (`Thunderbird: LEAD` then the lines); the wizard and `status` said the same
lines (the wizard with "Run the backup again…", as it has no schedule yet).

| # | Cell | Lead | What the person read (abridged) |
|---|---|---|---|
| A | Caught up: Gmail, another account, Local Folders | Export complete. | "Thunderbird has downloaded everything it lists for imap.gmail.com (60 messages)." and the same for imap.fastmail.com (16). Local Folders: no line. `status`: a note "Thunderbird does not keep Spam and Trash of imap.gmail.com on this computer (its usual setting), so they are not backed up." |
| B | One account behind, daily schedule | Backed up what Thunderbird has so far. | "Thunderbird has downloaded about 390 of 1,690 messages in imap.fastmail.com so far. Furthest behind: INBOX 300 of 1,200; Sent 0 of 400. Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest." |
| C | The same, "Only when I click Run now" | same | the same, ending "…Run the backup again once Thunderbird has downloaded more, to add the rest." Run now's note in Edge: "Backed up what Thunderbird has so far: no new messages." then that line. Recent backups: "OK — Thunderbird still downloading — 390 messages · Thunderbird has downloaded about 390 of 1,690 messages in imap.fastmail.com so far" |
| D | Gmail, All Mail not kept (gmail_floor) | Backup finished. | "About 50 messages are on this computer. Thunderbird lists at least 40 for imap.gmail.com. MailArchive cannot tell whether that is all of it, because Gmail's All Mail is not kept on this computer." Then the WARN: "This folder of imap.gmail.com is not kept on this computer, so MailArchive cannot back it up: [Gmail]/All Mail (3,200 messages). To keep it: in Thunderbird, right-click the folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now." |
| E | A folder not counted yet | Backup finished. | "MailArchive cannot tell whether Thunderbird has downloaded all of imap.fastmail.com, because Thunderbird has not counted all of its folders yet. Open Thunderbird and leave it open." |
| F | A folder known only by an old path | Backup finished. | "…because Thunderbird's list of folders still names 1 folder by where it used to be. This clears the next time Thunderbird saves that list (leave it open for a minute)." |
| G | Kept only 30 days (limited) | Backed up what Thunderbird has so far. | "Thunderbird has downloaded about 170 of 2,300 messages in imap.fastmail.com." then "Thunderbird is set to keep only the most recent 30 days of mail for imap.fastmail.com on this computer, so older mail is not backed up. To back up all of it: in Thunderbird's Account Settings, on imap.fastmail.com's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age"." The archive card: **nothing**. Recent backups: "OK — 170 messages · Thunderbird's own settings keep only part of imap.fastmail.com's mail on this computer (about 170 of 2,300 messages)" |
| H | Two folders not kept (772 and 33 messages) | Backup finished. | "Thunderbird has downloaded the 30 messages it lists in the folders it keeps on this computer for imap.fastmail.com." then the not-kept WARN naming "Projects (772 messages), Receipts (33 messages)" and the right-click steps. Card: "2 folders are not kept on this computer, so they are not backed up — see Status & health." |
| I | Encrypted archive, one account behind, Run now | Backed up what Thunderbird has so far: | Run now in Edge: "Backed up what Thunderbird has so far: **0 message(s)** added or updated. They are sealed in the encrypted archive; read them here with Open archive while it is unlocked." then the behind line. Recent backups: "OK — **backup completed** (its counts are sealed)". `status`: GREEN, nothing about Thunderbird (O1, as designed) |
| J | Caught up, count 6 days old (stale) | Backup finished. | "As of Nov 2, Thunderbird's count matched what is on this computer for imap.fastmail.com (50 messages). It has not counted since, so newer mail may not be here yet." |
| K | 195 of 200 (within the tolerance) | Backup finished. | "Thunderbird has downloaded about 195 of the 200 messages it lists for imap.fastmail.com. The rest is likely mail that arrived or was deleted since Thunderbird's count." |
| L | Two accounts behind, one caught up, one folder not kept | Backed up what Thunderbird has so far. | Two behind lines, **each** ending with the full "Leave Thunderbird open (…). Run the backup again once…" sentence, the not-kept line, and "…everything it lists for outlook.office365.com (8 messages)." Card: "Thunderbird is still downloading 2 accounts; 1 folder is not kept on this computer, so it is not backed up — see Status & health." |
| M | Gmail All Mail lists exactly 1,000 (cap note) | Backup finished. | "Thunderbird has downloaded the 1,000 messages it lists for imap.gmail.com." then "Gmail may be showing Thunderbird only the newest 1,000 messages of each folder of imap.gmail.com. In Gmail's settings, find "Folder size limits" and choose not to limit the number of messages." Card: nothing |
| N | No folder list (folderCache.json missing) | Backup finished. | "MailArchive cannot tell whether Thunderbird has downloaded all of imap.fastmail.com, because it could not read Thunderbird's list of folders." |
| O | Two Gmail accounts, both behind | Backed up what Thunderbird has so far. | "…about 120 of 900 messages in **imap.gmail.com** so far…" and "…about 60 of 2,500 messages in **imap.gmail.com-1** so far…", each with the full remedy. Card: "Thunderbird is still downloading 2 accounts — see Status & health." |

The Mac said every one of these word for word (the Mac's 14 plaintext cells; the Desktop's card,
Recent backups and Status & health for L in Chrome).

## 2. Findings

Grade: **medium** = fix before the public release; **low** = the lead's call.

| Cell(s) | Functions? | Finding | Type | Fix |
|---|---|---|---|---|
| A–O | Yes | M1: accounts are named by store folder | II | P1 (owner ruling) |
| L, O | Yes | M2: the remedy repeats per account | III | P2 |
| C | Yes | M3: Run now's note says "Run the backup again" | III | P3 |
| G | Yes | M4: Account Settings is not said how to open | III | P4 |
| M | Yes | M5: the cap note does not say where the setting is | III | P5 |
| I | Yes | M6: "message(s)" in the encrypted Run now note | III | P6 |
| I | Yes | M7: Recent backups says "backup completed" over a behind run | III | P7 |
| E, F, J, N | Yes | M8: four "cannot tell" or stale sentences give no next step | III | P8 |
| D | Yes | M9: the Gmail floor sentence reads as a contradiction | III | P9 |
| G | Yes | M10: the archive card is silent for a limited account | II (small) | P10 |
| B, C, H | Yes | L1: "Spam and Trash" when only Trash is not kept | III | P11 |
| B, C, L | Yes | L2: "INBOX" in capitals | III | P12 |
| — | Yes | L3: "about 0 of 1 messages" | III | P13 |
| A | Yes | L4: three words for the same complete state | III | P14 |
| G, C | Yes | L5: the Status & health box hides the remedy below its fold | III (layout, pre-existing) | backlog |
| D, H | Yes | L6: the card line's place differs between rows | III (layout) | backlog |

### Medium

- **M1. "imap.gmail.com-1" is not something the person has ever seen.** Every line names the
  account by its store folder (the design's rule, §7 and §9.1: "never by its email address").
  Thunderbird shows accounts by their name, which is the email address unless the person changed it,
  in the folder pane and in Account Settings. Three problems seen:
  - With two Gmail accounts (cell O) the lines say "imap.gmail.com" and "imap.gmail.com-1". The
    person cannot tell which is which.
  - A work account reads "outlook.office365.com" (cell L), which a person may not connect to their
    work mail.
  - The fix-it words point into Account Settings by a name that is not there: "on imap.fastmail.com's
    "Synchronization & Storage" page" (cell G). Account Settings lists `sam@fastmail.com`. C0c's
    re-look met the same gap (its R5) and added a server hint.

  This is a design rule, so it is the owner's ruling (P1). The privacy cost is small: a plaintext
  archive's own pages already hold the addresses, and the email notice (§9.3) can keep the store
  folder.
- **M2. The remedy repeats for each account behind** (cells L, O). Two behind accounts give two
  lines of 233 to 271 characters that end with the same two sentences. On a phone this is a wall of text.
  Say each account's numbers, then the remedy once (P2).
- **M3. Run now's note says "Run the backup again"** (cell C, the "Only when I click Run now"
  archive). On that screen the thing to click is called Run now. Say "click Run now again" there
  (P3).
- **M4. "in Thunderbird's Account Settings" does not say how to get there** (cell G, and the
  limited fallback). On Windows Thunderbird hides its menu bar, so there is no Tools menu to see.
  C0c's re-look (R1) fixed this in the offline-setting words with "click the menu button (≡, top
  right), then Account Settings", seen on 153.6.0 on Windows. The completeness words should say it
  the same way (P4).
- **M5. The cap note says "find "Folder size limits"" but not where** (cell M). Gmail's settings
  have about ten tabs. The page is "Forwarding and POP/IMAP" (as the builder noted, §13; not
  checked live, and TC-22 itself is unverified until lab row MA-1201). Name the path; quoting Gmail's
  own labels is fine even with "IMAP" in them, since that is what the person must find (P5).
- **M6. "0 message(s) added or updated"** (cell I, encrypted Run now, `encryptedSummaryFor`,
  `internal/wizard/jobrun.go:305`). C3 rewrote this line's lead, and the brief says "message(s)"
  must not appear. The plaintext note says "no new messages" in the same case (P6).
- **M7. Recent backups says "backup completed (its counts are sealed)" for an encrypted run that
  was behind** (cell I; `internal/desktop/runhistory.go`, the encrypted branches). The design's M8
  fold says no record-based surface says "complete" (§7). The encrypted history keeps nothing about
  completeness (O1), so this row cannot know; it must use the neutral word (P7). This wording is
  older than C3, but C3 is what makes it untrue.
- **M8. Four sentences say why but not what to do.** "…could not read Thunderbird's list of
  folders." (cell N, and `invalid_entry`, `duplicate_key`, unknown reasons) has no next step at all.
  The stale sentence (J) has none, though the stale-with-missing sentence already says "Open
  Thunderbird and leave it open so it can count again." `not_counted_yet` (E) and `old_path_only`
  (F) give one, but not "then run the backup again", and F's "(leave it open for a minute)" leaves
  "it" unclear (P8).
- **M9. The Gmail floor sentence looks like a contradiction** (cell D): "About 50 messages are on
  this computer. Thunderbird lists at least 40…" A person reads "50 here, 40 there, so all is well".
  But the WARN right below says All Mail lists 3,200. When X is at or above the floor, the floor
  number adds nothing; drop it, and when All Mail is not kept, point at the line below (P9).
- **M10. The archive card says nothing when Thunderbird keeps only part of an account** (cell G:
  170 of 2,300, so 93% of the mail is not backed up). The card speaks for behind accounts and
  not-kept folders only (design §7's card row), while `status` WARNs `thunderbird_limited`. The
  person who looks only at the card misses the biggest gap of all. A small design change (P10).

### Low

- **L1.** The Spam and Trash note always says "Spam and Trash", even when only Trash is not kept
  (cells B, C, H). It is also awkward: "Spam and Trash of imap.gmail.com" (P11).
- **L2.** "INBOX 300 of 1,200": Thunderbird shows this folder as "Inbox" (P12).
- **L3.** "about 0 of 1 messages": `tbBehind`, the limited headline, `TBCompactSentence` and
  `TBCardBanner` write a literal "messages" after Y. Rare (an account listing one message), but the
  repo's plural helper is right there (P13).
- **L4.** The same complete state is "Export complete." (wizard, command line), "Backup finished:"
  (plaintext Run now, which never says complete) and "Backup complete:" (encrypted Run now). The
  product's word everywhere else is "backup". Proposal P14; the owner's call, since tests pin
  "Export complete." (MA-1181 and others).
- **L5.** In the Desktop, Status & health shows `status`'s text in a monospace box with its own
  scroll bar. The limited WARN's remedy (cell G) and the behind WARN's end (cell C) sit below the
  box's fold on a 1080p-class screen; a person must scroll inside the box. Pre-existing layout, now
  carrying the longest remedies. Backlog.
- **L6.** On Your archives, the card line sits on its own line under some rows (B, C, L) and to the
  right of the folder on others (D, H, O), by its length. Cosmetic. Backlog.
- Noted, no change: "about X of Y" is honest (X is MailArchive's own count, Y Thunderbird's last
  count; both move); "The rest is likely mail that arrived or was deleted since Thunderbird's count"
  (K) is plain and true; the not-kept words and the right-click steps (H) read well and use
  Thunderbird's own labels; "Leave Thunderbird open (it downloads mostly while the computer is left
  alone)" is the clearest sentence in the set.
- Noted for the lead (design, not words): in cell D the lead is "Backup finished." although All
  Mail's 3,200 messages are not here; by the verdict rules a floor is `unknown`, so it is correct as
  designed, and the not-kept WARN carries the gap. If the owner wants the lead to follow the WARN,
  that is a §4.2 change.

### Against the UX contract

- X1 holds: every backup, behind or not, exited 0.
- X6 holds for the new WARNs: `thunderbird_behind`, `thunderbird_limited` and `thunderbird_not_kept`
  each name their remedy. The notes without a remedy (M8) are notes, so X6 does not require one;
  the friction walk does.
- X10: Recent backups, the card and Status & health agree with `status` where they speak. M10 is
  the one place a WARN in `status` has no counterpart on the card. It is not a second posture, but it
  is a silence the person reads as "nothing to see".
- X11 (plain labels): M1, M4 and L2 are the jargon left. "IMAP", "store", "cache" and "msf" appear
  nowhere in these words (checked in every cell's output).
- X17 holds: "MailArchive" throughout.
- X8 was not checked separately (the walk captured stderr and stdout together).
- No other contradiction.

## 3. Proposed words

ACCOUNT is the account's name as these words say it (today the store folder; see P1). Y-words use
the plural helper (`state.Count`).

**P1 (M1; owner ruling).** On screen (wizard, Run now, Desktop, `status` text), name an account by
Thunderbird's own account name, `mail.server.<key>.name` (for most people the email address), with
the store folder only where two accounts share that name or it cannot be read. Records, `-json` and
the email notice keep the store folder (§9.1, §9.3), adding `name` to the plaintext record only if
the owner agrees. Examples:

> Thunderbird has downloaded about 120 of 900 messages in sam.walker@gmail.com so far.
> Thunderbird has downloaded about 60 of 2,500 messages in sam.work@gmail.com so far.

In fix-it words that point into Account Settings, the name is what the person clicks:

> …on the "Synchronization & Storage" page under sam@fastmail.com…

If the owner keeps store folders, the minimum is to say what they are, once per line that names one:

> …messages in the Thunderbird account kept in the folder imap.gmail.com-1 so far…

**P2 (M2).** When two or more accounts are behind, each account's line stops after its numbers (and
"Furthest behind: …"), and one line follows them all:

> Leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.

or, with no schedule:

> Leave Thunderbird open (it downloads mostly while the computer is left alone), then run the backup again to add the rest.

One account behind keeps today's single line.

**P3 (M3).** On Run now's note (plaintext and encrypted), the no-schedule ending:

> …Click Run now again once Thunderbird has downloaded more, to add the rest.

The wizard keeps "Run the backup again…"; the command line says "Run the same command again once
Thunderbird has downloaded more, to add the rest." (This needs `TBSay` to carry the surface.)

**P4 (M4).** The age remedy below 160 (`tbAgeRemedy`):

> To back up all of it: in Thunderbird, click the menu button (≡, top right), then Account Settings; on ACCOUNT's "Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age".

The same opening ("in Thunderbird, click the menu button (≡, top right), then Account Settings;")
for the size remedy, the keep remedy and the TC-9 line. The limited fallback (`TBLimitLines`):

> Thunderbird's own settings keep only part of ACCOUNT's mail on this computer, so the rest is not backed up. To check: in Thunderbird, click the menu button (≡, top right), then Account Settings, and open ACCOUNT's "Synchronization & Storage" page.

**P5 (M5).** `TBCapNote` (recollection of Gmail's labels, unverified like TC-22 itself; lab row
MA-1201 confirms both):

> Gmail may be showing Thunderbird only the newest 1,000 messages of each folder of ACCOUNT. To change it: in Gmail on the web, click the gear (top right), then See all settings, then the "Forwarding and POP/IMAP" tab; under "Folder size limits", choose "Do not limit the number of messages in an IMAP folder", then click Save Changes.

**P6 (M6).** `encryptedSummaryFor`, first sentence:

> Backed up what Thunderbird has so far: no new messages.

when nothing was written, and otherwise:

> Backed up what Thunderbird has so far: 390 messages added or updated. They are sealed in the encrypted archive; …

(The count through the plural helper, as `runNowSummaryFor` already does; the same for the
"Backup complete:" and "Backup finished:" leads.)

**P7 (M7).** `runRowOf`, the encrypted rows:

> backup finished (its counts are sealed)
> backup finished; some messages are not searchable yet — the next backup adds them

**P8 (M8).** The four sentences:

- could not read (`cache_unreadable`, `invalid_entry`, `duplicate_key`, an unknown reason):
  > MailArchive cannot tell whether Thunderbird has downloaded all of ACCOUNT, because it could not read Thunderbird's list of folders. Opening Thunderbird and letting it check your mail usually fixes this; then run the backup again.

  ("Usually": which event makes Thunderbird write `folderCache.json` again was not checked in this
  walk; a lab note should confirm it before "usually" goes.)
- not counted yet:
  > …because Thunderbird has not counted all of its folders yet. Open Thunderbird, leave it open for a few minutes, then run the backup again.
- old path:
  > …because Thunderbird's list of folders still names 1 folder by where it used to be. Open Thunderbird and leave it open for a minute so it saves that list again, then run the backup again.
- stale (the M9 dated sentence):
  > As of Nov 2, Thunderbird's count matched what is on this computer for ACCOUNT (50 messages). It has not counted since, so newer mail may not be here yet. Open Thunderbird and leave it open so it can count again.

**P9 (M9).** The Gmail floor (`gmail_floor`), All Mail not kept:

> About 50 messages of ACCOUNT are on this computer. MailArchive cannot tell how many more there are, because Gmail's All Mail folder, which holds all of this account's mail, is not kept on this computer (see below to keep it).

The other three clauses, when X is at or above Y:

> About 50 messages of ACCOUNT are on this computer. MailArchive cannot tell whether that is all of it, because Gmail's All Mail CLAUSE.

and, when X is below Y, today's sentence with "of ACCOUNT" moved forward:

> About 30 messages of ACCOUNT are on this computer, and Thunderbird lists at least 40. MailArchive cannot tell whether that is all of it, because Gmail's All Mail CLAUSE.

**P10 (M10).** `TBCardBanner` gains the limited accounts, before the not-kept count:

> Thunderbird keeps only part of ACCOUNT's mail on this computer (about 170 of 2,300 messages), so the rest is not backed up — see Status & health.

and, for two or more: "Thunderbird keeps only part of the mail of 2 accounts on this computer".
Joined to the others with "; " as today.

**P11 (L1).** `TBDefaultNotKeptNote`, naming what the record lists (the last part of each name):

> Thunderbird does not keep the Trash folder of ACCOUNT on this computer (its usual setting), so it is not backed up.
> Thunderbird does not keep the Spam and Trash folders of ACCOUNT on this computer (its usual setting), so they are not backed up.

**P12 (L2).** `tbFolderName`: the top-level name `INBOX` (any letter case) is said "Inbox".

**P13 (L3).** "about 0 of 1 message" / "about 1 of 2 messages": the word after Y by Y.

**P14 (L4).** `TBLeadComplete` = "Backup complete." (the wizard and the command line), matching the
encrypted note's "Backup complete:".

### What the changes need (for the lead; nothing changed here)

- Tests pin today's words: MA-1181 (state, app, wizard, cmd), MA-1182 (Desktop rows and card),
  MA-1183 (status), MA-1185 (labels), MA-1186 (encrypted). Each change is test-first under its row,
  with prove-fail on the old words.
- P1 needs the matched account's `name` from prefs.js (C0c's allow-list reader gains one key) and,
  if recorded, a record field; a design fold and probably a light adversarial look, since the name
  is text the person or a server chose (clean and cap it as TC-7 does for folder names).
- P3 needs `TBSay` to know the surface. P10 is a §7 fold.
- P4, P5 and P8 name places in Thunderbird and Gmail: lab row MA-1191 (the ≡ path on the Mac's
  159.0) and MA-1201 (Gmail's page) should see them.

## 4. Not walked

- The wizard's dialog on screen (a zenity question box): its words were taken from the wizard's own
  `exportSummary` over a real run, not seen in the dialog.
- Phone width: Chrome's window would not resize below the screen's width in this session, so phone
  length is judged from the strings (the longest single line is cell L's not-kept line, 278
  characters; cell O's two behind lines are 469 characters before P2, about 290 after).
- Reasons with no cell here: `panorama`, `account_type` (an Exchange account), `partial_input`,
  `never_checked`, `expunged_unsized`, `split_differently`, `count_uncertain`, `other_reader`,
  `walk_failed`, the size limit, background download off, Thunderbird 160's labels, deleted copies,
  messages not told apart, and "accounts the record left out" (`thunderbird_more`). Their words were
  read in `tbwords.go` and are covered by MA-1181 and MA-1185; none has a finding beyond M8's
  pattern.
- A real Thunderbird and real mail, on purpose: made-up profiles only.
- The email notice (no mail server in the lab).

Two mistakes of my own, kept here. Cell E's first profile gave the folder a server count of -1 as
well, which made it `invalid_entry`, not `not_counted_yet`; it was made again with only the folder's
own count at -1 and then read as designed. For cell I, the lab helper first wrote an "Only when I
click Run now" record into the encrypted archive folder; an encrypted archive keeps no such file, so
`status` correctly went RED ("now holds other files"). The record was removed and `status` went back
to GREEN. Neither is a product finding.

## Re-look after the fold rounds (2026-11-08)

Walked 8 November 2026, Windows VM clock 9:32 PM to 9:37 PM, then the Mac at 9:38 PM to 9:40 PM (EST).
A short re-look: the same process (`../assurance-kit/process/friction-review.md`), judged against the
proposals above, `docs/ux-contract.md`, §13's two notes of 2026-11-08 ("C3 friction review; fold
check 3's N1 and N2" and "the account's name on every surface"), and `internal/state/tbwords.go` as
b8f9229 and a2bd246 left it. Windows judged first.

**Verdict: SHIPPABLE_WITH_NOTES.** Every proposal was applied as written, and the owner's two rulings
read well on every surface: the email address names the account everywhere (cell O's two Gmail
accounts are now told apart at a glance), and the fallback is used only where it should be. Two
findings are medium, both small word changes (Type III, buildable-away, so built before the public
release): R1, "see below" points the wrong way on `status` and Status & health; R2, the shared
"Leave Thunderbird open…" line can land between one account's folder line and the next account. Five
are low. Exact words are below. They are proposals for the lead; this re-look does not self-certify
them.

### Header

| | |
|---|---|
| Reviewed commit | a2bd246, branch `tb-completeness` (worktree pulled, already up to date) |
| Programs | Built from `git archive a2bd246` into the main checkout's untracked `.claude/fric-c3r/src`: `mailarchive.exe`, `mailarchivew.exe` (`-H=windowsgui`) and the Mac `mailarchive`, from the tip's code unchanged. Lab-only, never in the worktree: the earlier `c3lab` helper (the wizard's own `exportSummary` after a real run, and the wizard's run-job), now with an "encrypted, Run now only" job that writes no file into the archive folder; and `labenc.exe`, a build of the same copy with a lab tag that answers `-encrypt`'s terminal questions from a script, used only to make the throwaway encrypted archive and later to forget its key |
| Profiles | Made-up Thunderbird-shaped profiles only, under `C:\Users\Public\tb-c3r\<cell>` on the VM and `.claude/fric-c3r/mac/tb` on the Mac (153.6.0). Each account has `mail.server.<key>.name`: `sam.walker@gmail.com`, `sam@fastmail.com`, `sam.work@gmail.com` (the second Gmail), `sam@work.example`; cells P and Q have one account with no name; cell R has two accounts that share the name "Sam Walker". Thunderbird itself was not started |
| Own settings | `APPDATA` and `LOCALAPPDATA` (Mac: `HOME`) pointed at lab folders; Desktop with `-config C:\Users\Public\c3rlab\cfg\config.json`. At the end: cell B's daily Task Scheduler entry and its launchd twin removed (checked: none left on the VM), the encrypted cell's remembered key forgotten from the VM user's Credential Manager, both Desktops stopped, the Chrome tab closed |
| Browser and input | Windows: Microsoft Edge in the VM user's session, real clicks and keys. Mac: Chrome (page text) |
| Evidence | 16 VM screenshots in `.claude/fric-c3r/shots/` (private, not committed); each cell's command-line output, `status`, `status -json` and wizard summary under `C:\Users\Public\c3rlab\results\` and `.claude/fric-c3r/mac/res/` |
| Cells walked | 19 profiles (A to R and I0) on Windows: the command line, `status`, `status -json` and the wizard's words for 17 plaintext cells; Desktop in Edge for every archive card, Run now on the encrypted archive (390 messages, then none) and on cell Q (two behind, one with no name, Run now only), Recent backups (I, Q), Status & health (I, Q). Mac: the same 17 plaintext cells × the same four, and the Desktop's page (every card, Recent backups and Status & health for L) |
| Functioning | All cells fully. Exit code 0 for every backup (X1) |

### Were the proposals applied faithfully?

Yes, each one, seen on a real surface:

| Proposal | Seen | Result |
|---|---|---|
| M1 / P1, the name (owner ruling) | every cell | "sam.walker@gmail.com" and "sam.work@gmail.com" (O); "sam@work.example" (L); folder words only with no name (P, Q) or a shared name (R) |
| P2, one remedy after two behind | L, O, Q on the wizard, command line and Run now | applied; see R2 for where it lands |
| P3, words for each surface | C, Q, I | "Click Run now again…" on Run now; "Run the same command again…" on the command line; "Run the backup again…" in the wizard and `status` |
| P4, the ≡ path | G, P | applied to every Account Settings remedy |
| P5, Gmail's path | M | applied (still unverified until lab row MA-1201) |
| P6, the encrypted count in words | I, Run now twice | "Backed up what Thunderbird has so far: 390 messages added or updated. They are sealed…", then "…: no new messages. Your messages are sealed…". No "(s)" |
| P7, encrypted Recent backups | I | "OK (3 times, last at 9:36 PM) — backup finished (its counts are sealed)" |
| P8, next steps | E, F, J, N | each ends with what to do |
| P9, Gmail floor | D | no more "50 here, at least 40 there"; see R1 |
| P10, card line for a limited account | G, P | "Thunderbird keeps only part of sam@fastmail.com's mail on this computer (about 170 of 2,300 messages), so the rest is not backed up — see Status & health." |
| P11, usual folders | A, B, H | "the Spam and Trash folders of sam.walker@gmail.com … so they are"; "the Trash folder of sam@fastmail.com … so it is" |
| P12, Inbox | B, C, L, Q | "Inbox 300 of 1,200" |
| L4, "Backup complete." (owner ruling) | A | wizard, command line and `status`'s Last run line |

The Mac said every one of these word for word.

### How the names read

- **The email address reads well.** "Thunderbird has downloaded about 120 of 900 messages in
  sam.walker@gmail.com so far" is what a person sees in Thunderbird's folder pane, so they know which
  account it is. In fix-it words it points at what they click: "on sam@fastmail.com's
  "Synchronization & Storage" page". Cell O's two Gmail accounts, the worst case before, now read
  as two different lines.
- **The fallback reads correctly but long.** "the Thunderbird account kept in the folder
  imap.fastmail.com" is clear, and it is rare: Thunderbird writes a name when an account is made, so
  only an unusual profile (P, Q) or two accounts with one name (R) reach it. In the limited WARN
  (cell P) it is said three times in one block of about 480 characters. R4 trims it.
- **A shared name** (R) falls back to folder words for both accounts, on every surface. Correct.

### Findings

Grade: **medium** = fix before the public release; **low** = the lead's call.

| # | Cell(s) | Finding | Grade | Fix |
|---|---|---|---|---|
| R1 | D | "(see below to keep it)" is wrong where the folder's WARN is printed above | medium | P-R1 |
| R2 | L | the shared "Leave Thunderbird open…" line sits between one account's folder line and the next account | medium | P-R2 |
| R3 | I (first backup) | "about 0 of 1,200 messages" | low | P-R3 |
| R4 | P | the fallback said three times in one WARN | low | P-R4 |
| R5 | C, O, Q | Status & health says "Run the backup again" for a Run-now-only archive, while the Run now note just above says "Click Run now again" | low | P-R5 |
| R6 | E, F, N | "all of sam@fastmail.com" reads as "all of a person" | low | P-R6 |

- **R1 (medium). "See below" points up on two surfaces.** The Gmail floor sentence ends "…is not
  kept on this computer (see below to keep it)." In the wizard and on the command line the line that
  says how to keep All Mail is indeed below. But `status` prints every WARN before the notes, so in
  `status` and in Desktop Status & health that line is *above* (cell D, Windows and Mac). The
  person looks down and finds nothing. A direction that is true on one surface and false on another
  should name the line instead.
- **R2 (medium). Where the shared line lands.** P2's line is said "after the last of them", that is,
  after the last behind account's lines, including its not-kept folder line. In cell L (two behind,
  one caught up) the wizard reads:

  > Thunderbird has downloaded about 120 of 900 messages in sam.walker@gmail.com so far.
  > Thunderbird has downloaded about 100 of 500 messages in sam@fastmail.com so far. Furthest behind: Inbox 100 of 500.
  > This folder of sam@fastmail.com is not kept on this computer, … then click Download Now.
  > Leave Thunderbird open (it downloads mostly while the computer is left alone), then run the backup again to add the rest.
  > Thunderbird has downloaded everything it lists for sam@work.example (8 messages).

  The shared line reads as the end of the folder instructions, and another account's line follows
  it. Nothing in it says which accounts it is for. When the behind accounts are the last ones (O, Q,
  Run now in Q) it reads clearly; it is only in the middle that it is unclear. On a phone, where
  these are five wrapped paragraphs, the person cannot tell what "the rest" means.
- **R3 (low).** A brand-new account Thunderbird has not downloaded yet reads "Thunderbird has
  downloaded about 0 of 1,200 messages in sam@fastmail.com so far. Furthest behind: Inbox 0 of
  1,200." (the encrypted archive's first backup in this walk). "About 0" is odd, and this is a
  common first-run case.
- **R4 (low).** Cell P's WARN names "the Thunderbird account kept in the folder imap.fastmail.com"
  three times. After the headline has named it, the limit sentence can say "this account's mail";
  the remedy keeps the full words, since that is how the person finds the account.
- **R5 (low).** `status` already knows the archive is "Only when I click Run now" (its
  `manual_backup` note), but it says its behind remedy with the default surface. On the Desktop the
  Run now note says "Click Run now again…" and Status & health a little further down says "Run the
  backup again…". Not a contradiction, but two words for one action on one page.
- **R6 (low).** "MailArchive cannot tell whether Thunderbird has downloaded all of sam@fastmail.com,
  because…" now that the name is an address. "All of sam@fastmail.com's mail" says what is meant.
- Noted, no change:
  - `status` and Status & health keep a full remedy in each behind WARN (L, O, Q: two WARNs with the
    same two sentences). That is X6 (each WARN names its remedy) and §13 says so; R2 is about the
    wizard, Run now and the command line only.
  - No "(s)", no "store", "cache" or "msf" anywhere walked; "IMAP" only inside Gmail's own label.
  - Status & health's monospace box still hides the remedies below its fold (L5, backlog, seen
    again in cell Q).
- Noted, outside these words (pre-existing, for the backlog):
  - After Run now, the Recent backups card on the same page does not change until the page is
    reloaded (I, Q). After a reload, the encrypted runs were grouped as "OK (3 times, last at
    9:36 PM)".
  - The encrypted archive's Scheduled backup card still says "Not installed." under a Run now note
    that just reported a backup.
  - The wizard's summary prints "Newly downloaded this run: 0" a few lines below "Thunderbird has
    downloaded about 390 of 1,690…". The first counter is MailArchive's (Microsoft 365) and the
    second is Thunderbird's, but both say "downloaded".

### The ≡ steps on Windows

"in Thunderbird, click the menu button (≡, top right), then Account Settings; on sam@fastmail.com's
"Synchronization & Storage" page, choose "Synchronize all messages locally regardless of age"" is
right for Thunderbird on Windows as far as this re-look can tell: the menu bar is hidden by default
there, the ≡ menu at the right end of the toolbar holds Account Settings, and the left side of
Account Settings lists each account by its name with "Synchronization & Storage" under it. This
re-look did **not** see it in a running Thunderbird (none on the VM, on purpose). It rests on C0c's
re-look (R1), which saw the ≡ path on 153.6.0 on Windows. Lab row MA-1191 still owes the Mac's 159.
The right-click steps for a folder ("right-click the folder → Properties → Synchronization → tick
"Select this folder for offline use", then click Download Now") are unchanged and were not
re-checked.

### Length on a phone

The wizard and Run now lines are each one short paragraph. The longest blocks are in `status` and
Status & health: cell P's limited WARN at about 480 characters (about 400 with a name, cell G, and
about 400 after R4), and Gmail's cap note at about 330. Cell O's two behind lines and the shared line
are about 290 characters together in the wizard. Phone width was not rendered (as before, the
window would not shrink); this is judged from the strings.

### Proposed words

ACCOUNT is the account's name as these words say it today (`tbName`).

**P-R1.** `tbGmailFloor`, All Mail not kept:

> About 50 messages of ACCOUNT are on this computer. MailArchive cannot tell how many more there are, because Gmail's All Mail folder, which holds all of this account's mail, is not kept on this computer (the line that names [Gmail]/All Mail says how to keep it).

(The folder as `TBNotKeptLine` names it, so the two lines match whatever order a surface prints them in.)

**P-R2.** `TBLines`: with two or more accounts behind, say the shared line once **after every
account's lines** (before `TBMoreSentence`), and say whom it is for:

> For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone), then run the backup again to add the rest.

with the surface's own words as today ("…then click Run now again to add the rest.", "…then run the
same command again to add the rest."), and with a schedule:

> For the 2 accounts still downloading: leave Thunderbird open (it downloads mostly while the computer is left alone); later backups add the rest.

**P-R3.** When X is 0 (`tbBehind`, `TBCompactSentence`, `TBCardBanner`):

> Thunderbird has not downloaded any of the 1,200 messages in ACCOUNT yet.

and on the card: "Thunderbird is still downloading ACCOUNT: none of its 1,200 messages are on this
computer yet". "Furthest behind: Inbox 0 of 1,200" can stay.

**P-R4.** `TBLimitLines` and its remedies, when the account has no name: the limit sentence says
"this account's mail" after the headline has named it; the remedy keeps the folder words:

> Thunderbird is set to keep only the most recent 30 days of this account's mail on this computer, so older mail is not backed up. To back up all of it: in Thunderbird, click the menu button (≡, top right), then Account Settings; on the "Synchronization & Storage" page of the Thunderbird account kept in the folder imap.fastmail.com, choose "Synchronize all messages locally regardless of age".

(With a name, today's words are fine: the name is short.)

**P-R5.** `status` (and so Status & health) says its behind remedy with `TBSurfaceRunNow` when the
archive's How often is "Only when I click Run now", as its `manual_backup` note already knows:

> …Leave Thunderbird open (it downloads mostly while the computer is left alone). Click Run now again once Thunderbird has downloaded more, to add the rest.

**P-R6.** `tbUnknown`'s opening, with the possessive the words already use (`tbOwn`):

> MailArchive cannot tell whether Thunderbird has downloaded all of sam@fastmail.com's mail, because …

(with no name: "…all of the mail of the Thunderbird account kept in the folder imap.fastmail.com,
because …").

### What the changes need (for the lead; nothing changed here)

- Tests pin today's words: MA-1181 (state, app, wizard, cmd), MA-1182 (Desktop), MA-1183 (status),
  MA-1284 (names). Each change test-first under its row, prove-fail on the old words.
- P-R2 changes where a line goes, not only its words: the MA-1181 test for P2's placement changes
  with it. P-R5 needs `status` to pass the surface it already knows.

### Not walked in this re-look

- A real Thunderbird (on purpose), so the ≡ path, "Synchronization & Storage" and Download Now
  were not seen on screen here.
- The wizard's dialog itself (its words were taken from the wizard's own `exportSummary` over a
  real run, as before) and phone width.
- The alert email (unchanged, §9.3).
- Reasons with no cell (as listed in §4 above); their words were read in `tbwords.go`.

Two mistakes of my own, kept here. The Mac link shim was first written without its execute bit, so
the first Mac Desktop start handed its one-time link to the real `osascript`, which may have
opened it in the Mac's default browser. That Desktop was stopped at once (so the link no longer
works), and started again with the shim fixed. On the VM, `forget-key` refused to run without a terminal (correct: it is the person's
own action), so the lab-only build answered it; the key is forgotten.
