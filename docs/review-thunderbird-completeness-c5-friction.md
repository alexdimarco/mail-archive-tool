# Friction review (light): completeness slice C5's two alert emails

Reviewed 9 November 2026 (Mac clock, EST). Process: `../assurance-kit/process/friction-review.md`,
light form (the words only, no live walk). Also read: `docs/ux-contract.md` (X-series),
`docs/design-thunderbird-completeness.md` §7, §9.3 and §13's C5 note, the C3 friction review
(`docs/review-thunderbird-completeness-c3-friction.md`), the source re-read
(`docs/lab/thunderbird-completeness-source-esr153.md` row 12), `internal/alert/alert.go` (every
alert email's subject and body), `internal/state/tbnotice.go` (the C5 lines and remedy) and
`internal/backupalert/tbbehind.go`.

**Verdict: SHIPPABLE_WITH_NOTES.** Both emails are true, say the backups are working, name what
went wrong and give something to do on the computer. Nothing a person reads is wrong or a dead
end on its own, and no "(s)" appears. But on a phone they read worse than they should: the
preview a phone shows is mostly the archive's path; the account is named by a folder the person
has never seen, with no way given to match it to an account; and the stopped email's second step
says "if nothing changes … check" a page without saying when to look or what to look for. Three
findings are medium (M1 to M3) and seven are low (L1 to L7). All are word changes (Type III,
buildable-away; the kit says these are built before ship). The exact replacement words are in §3.
They are proposals for the lead; this review does not self-certify them.

## Header

| | |
|---|---|
| Reviewed commit | a385f97, branch `tb-completeness` (worktree pulled, already up to date) |
| How rendered | A scratch driver, `.claude/c5friction/main.go` in the worktree (untracked, not committed), builds each case's notices, calls `state.TBNoticeEmail` and then `alert.Config.Message` (the exact message the SMTP path sends) and prints the subject and body. No email was sent; no SMTP connection was opened |
| Cases | 1 one Gmail account still downloading 3 days · 2 two Gmail accounts (`imap.gmail.com`, `imap.gmail.com-1`) in one email · 3 stopped with some downloaded · 4 stopped with none (3,402 messages), 4b with 1 message, 4c with a Gmail "at least" count · 5 one stopped and one still, long Windows path, Windows computer name · 5b two stopped, long Windows path · 6 no computer name |
| Made-up values | Archive `/Volumes/Backup/Mail` and `C:\Users\alexandra.dimarco-wilson\OneDrive - DiMarco Technologies Inc\Documents\Mail Backups\Thunderbird archive 2026`; computers `Alexs-MacBook-Pro.local`, `studio`, `DESKTOP-7Q2K9LM`; run time 9 Nov 2026 03:00 local; stop date Nov 5 |
| Not done | No phone was used: the preview widths below are this reviewer's estimate (about 35 to 45 characters of subject and two lines, about 90 characters, of body in a phone's mail list), not a measurement. No Thunderbird was opened: the menu path and page names are checked against the C3 review and the source re-read only |
| Functioning | 9 of 9 cases render, every one true to its notices |

## 1. What the person reads

Case 1, as sent (line breaks as in the email):

> **Subject (86 characters):** MailArchive: Thunderbird is still downloading mail after 3 days — /Volumes/Backup/Mail
>
> The scheduled backups of /Volumes/Backup/Mail on Alexs-MacBook-Pro.local are working, but
> Thunderbird on that computer is still downloading the mail of the account below, so the backups
> have only what it has so far.
>
> Thunderbird has been downloading the Thunderbird account kept in the folder imap.gmail.com for 3
> days and has about 120,000 of 238,080 messages so far.
>
> Do this on that computer: leave Thunderbird open (it downloads mostly while the computer is left
> alone); later backups add the rest.
>
> You will get this email once while this mail is still downloading, and one more only if
> Thunderbird then stops.
>
> This message was sent by MailArchive's built-in backup alerts.

Case 3, stopped with some downloaded (the body's middle and remedy):

> **Subject (76):** MailArchive: Thunderbird has stopped downloading mail — /Volumes/Backup/Mail
>
> Thunderbird has not downloaded any more of the Thunderbird account kept in the folder
> imap.gmail.com since Nov 5 (about 1,670 of 238,080 messages are on that computer).
>
> Do this on that computer: open Thunderbird and leave it open with the computer idle for a while.
> If nothing changes, in Thunderbird, click the menu button (≡, top right), then Account Settings,
> and check the "Synchronization & Storage" page of the Thunderbird account kept in the folder
> imap.gmail.com.

The other cases, in short:

| Case | Subject length | Body length | What stood out |
|---|---|---|---|
| 2 two Gmail accounts | 86 | 839 | "imap.gmail.com" and "imap.gmail.com-1" side by side; nothing says which is which |
| 4 none downloaded | 76 | 811 | "None of the 3,402 messages in … are on that computer yet, and Thunderbird has downloaded none since Nov 5." Clear |
| 4b one message | 76 | 804 | "The 1 message in … is not on that computer yet, and Thunderbird has downloaded nothing since Nov 5." Clear |
| 4c Gmail "at least" | 76 | 811 | "None of at least 56,611 messages in …" reads awkwardly (L3) |
| 5 mixed, Windows path | 173 | 1,208 | the path fills the subject and the body preview; the remedy then says "leave it open" twice (L2) |
| 5b two stopped | 173 | 1,053 | "the "Synchronization & Storage" page of each account named above" |
| 6 no computer name | 86 | 670 | "of /Volumes/Backup/Mail on this computer are working, but Thunderbird on that computer…" (L4) |

## 2. Findings

Grade: **medium** = fix before the public release; **low** = the lead's call.

| Case(s) | Functions? | Finding | Type | Fix |
|---|---|---|---|---|
| all, worst 5 and 5b | Yes | M1: the phone preview is mostly the path | III | P1 |
| all, worst 2 and 5 | Yes | M2: the account's folder name is opaque, with no way given to match it | III | P2 |
| 3, 4, 5, 5b | Yes | M3: the stopped remedy's second step has no "when" and no "what" | III | P3 |
| all | Yes | L1: the subject: a full path, and "after 3 days" may be wrong | III | P4 |
| 5 | Yes | L2: the mixed remedy says "leave Thunderbird open" twice | III | P3 |
| 4c | Yes | L3: "None of at least 56,611 messages" | III | P2 |
| 6 | Yes | L4: "this computer" and "that computer" for one computer | III | P1 |
| 3 to 5b | Yes | L5: "stopped downloading mail of the account" (no "the") | III | P1 |
| all | Yes | L6: "Thunderbird has been downloading the Thunderbird account…" | III | P2 |
| 1, 2 | Yes | L7: the Mac's computer name ends in ".local" (all alert emails) | III | backlog |

### Medium

- **M1. The phone preview is mostly the path.** The body opens "The scheduled backups of PATH on
  COMPUTER are working, but…". A phone's mail list shows about the first 90 characters. In case 1
  that is "The scheduled backups of /Volumes/Backup/Mail on Alexs-MacBook-Pro.local are working,
  but Thunderb…"; in cases 5 and 5b it is "The scheduled backups of
  C:\Users\alexandra.dimarco-wilson\OneDrive - DiMarco Technologies Inc\Documents\Mail…", which says
  nothing. The two things the person needs first, "your backups are fine" and "Thunderbird has not
  got all of one account yet", are past the preview. Fix: lead with those, then give the archive
  and computer on their own lines, as the backup-result emails already do ("Archive: …",
  "Computer: …"). That also removes L4 and L5.
- **M2. "the Thunderbird account kept in the folder imap.gmail.com-1" cannot be matched to an
  account.** The owner ruled that emails keep the folder words for privacy (§9.3), and this review
  keeps them. But Thunderbird never shows the person that folder name: its folder pane and Account
  Settings list accounts by their name (usually the address). With one Gmail account
  "imap.gmail.com" can be guessed. With two (case 2), "imap.gmail.com" and "imap.gmail.com-1"
  cannot, and the stopped email sends the person to "the "Synchronization & Storage" page of the
  Thunderbird account kept in the folder imap.gmail.com", a label that is not on that screen.
  The C0c words already solved this on screen with a pointer to the "Server Settings" page
  (`internal/thunderbird/offline_words.go`, `serverHint`). Fix: say the account shorter on its
  line, and add one sentence on how to tell which account it is.
- **M3. The stopped remedy's second step has no "when" and no "what".** "If nothing changes, …
  check the "Synchronization & Storage" page" does not say how the person would know nothing
  changed (no second email comes: it is sent once), or what to look for on that page. The setting
  that matters there is the one that keeps the account's mail on the computer, whose label changes
  in Thunderbird 160 ("Keep messages in all folders for this account on this computer" up to 159;
  "Save copies of all folders and messages on this computer for offline use" from 160, per the
  source re-read, row 12). The email does not know the version, so naming one label risks the
  wrong one, and naming both is long on a phone. Fix: say where to see whether it moved (Status &
  health shows the count), when to look (the next day), and what to make sure of in words that hold
  for every version. Also say the likely simple cause first: a Thunderbird waiting for a password
  downloads nothing (this reviewer's judgement, not observed in this review).

### Low

- **L1. The subject.** Its key words do show in a phone's preview ("MailArchive: Thunderbird is
  still downlo…", "MailArchive: Thunderbird has stopped dow…"), so this is low. But the full path
  at the end is never seen on a phone, makes a Windows subject 173 characters long, and "after 3
  days" is fixed text: a deferred or retried email (§13's C5 note: one email per run, a failed
  delivery tried again) can carry a line saying "for 5 days". Fix: drop the number, add "(backups
  OK)", and end with the archive's own folder name only (P4). The four older subjects use the full
  path; changing them to match is backlog, the lead's call.
- **L2.** Case 5's remedy: "open Thunderbird and leave it open … For the account still downloading:
  leave Thunderbird open…". Step 1 already covers both. Fix in P3.
- **L3.** "None of at least 56,611 messages in …". Fix in P2 ("none of its messages (at least
  56,611)").
- **L4.** With no computer name the lead says "on this computer are working, but Thunderbird on
  that computer…": the same computer called two things. Fix in P1 (the name goes on its own line,
  left out when unknown).
- **L5.** "has stopped downloading mail of the account below": read "the mail of". Fix in P1.
- **L6.** "Thunderbird has been downloading the Thunderbird account kept in the folder…": two
  "Thunderbird"s in seven words, and a person downloads mail, not an account. Fix in P2.
- **L7.** macOS gives its computer name as "Alexs-MacBook-Pro.local". Every alert email uses the
  raw name today, so this is consistent; dropping a trailing ".local" in all of them is backlog.

### What is already right

- **Is the backup broken?** Both emails say the scheduled backups "are working" in their first
  sentence. Kept, and moved to the very start (P1).
- **No "(s)"** in any case; counts say "1 message", "3 days", "2 accounts".
- **No jargon** beyond the folder name the owner ruled for emails. No mail folder name appears
  (§9.3), and no account name or address.
- **The menu path** ("click the menu button (≡, top right), then Account Settings") is the C3
  review's settled words, seen on Windows 153.6.0; the Mac's 159 is owed by lab row MA-1191. The
  page name "Synchronization & Storage" holds to 159 by the source re-read; whether 160 keeps it was
  not read (also MA-1191).
- **Consistency with the other alert emails.** The sign-off ("This message was sent by
  MailArchive's built-in backup alerts.") is the same in all six emails. The sender is the alert
  settings' From in all of them. The "MailArchive: " subject prefix and "Do this on that computer:"
  match the Thunderbird-not-synced email. The once-only sentence matches that email's. P1 adds the
  "Archive:" and "Computer:" lines of the backup-result emails; the Thunderbird-not-synced email
  could take the same shape later (backlog).

## 3. Proposed words

Notes for the builder:
- These change only the email (`state.TBNoticeEmail`, `tbStillLine`, `tbStoppedLine`, `tbLeave`
  and `alert.tbBehindSubjectAndBody`); they are used nowhere else.
- The numbered steps need line breaks, which `alert.header` strips from a single remedy string, so
  the remedy becomes a list of lines, each stripped.
- NAME in the subject is the last part of the archive's path.
- In the bodies below, square-bracketed parts are filled in; everything else is literal.

### P4. Subjects

- Still downloading: `MailArchive: Thunderbird still downloading (backups OK) — NAME`
- Stopped (any account stopped): `MailArchive: Thunderbird stopped downloading (backups OK) — NAME`

Example: "MailArchive: Thunderbird stopped downloading (backups OK) — Thunderbird archive 2026" (82
characters, against 173 today).

### P1. The opening and the archive lines

Still downloading:

> Your backups are working. Thunderbird has not finished downloading [one account's / 2
> accounts'] mail yet, so the backups have only part of it so far.
>
> Archive: [path]
> Computer: [computer name; the line is left out when unknown]

Stopped:

> Your backups are working, but Thunderbird has stopped downloading [one account's / 2
> accounts'] mail, so the backups cannot get the rest until it starts again.
>
> Archive: [path]
> Computer: [computer name; left out when unknown]

In the mixed case (case 5) the stopped opening counts only the stopped accounts; the still
downloading account has its own line below.

### P2. One line per account, and which account it is

Each account, stopped ones first:

- still, some downloaded: `The account kept in the folder imap.gmail.com: about 120,000 of 238,080 messages downloaded, still going after 3 days.`
- still, none downloaded: `The account kept in the folder imap.gmail.com-1: none of its 2,500 messages downloaded yet, after 4 days.`
- stopped, some downloaded: `The account kept in the folder imap.gmail.com: about 1,670 of 238,080 messages downloaded, no progress since Nov 5.`
- stopped, none: `The account kept in the folder imap.fastmail.com: none of its 3,402 messages downloaded yet, no progress since Nov 5.`
- stopped, one message: `The account kept in the folder imap.fastmail.com: its 1 message is not downloaded yet, no progress since Nov 5.`
- "at least" counts: `about 1,670 of at least 56,611 messages downloaded` and `none of its messages (at least 56,611) downloaded yet`.

Then, after the remedy, always:

> To see which account that is: in Thunderbird, click the menu button (≡, top right), then
> Account Settings; each account's "Server Settings" page shows the folder its mail is kept in, at
> the bottom.

(The "Server Settings" page is named by the C0c words already shipped. That it shows the folder
"at the bottom", under "Local Directory", is this reviewer's memory of Thunderbird and was not
checked here: add it to lab row MA-1191.)

### P3. What to do

Still downloading (no account stopped):

> Do this on that computer: leave Thunderbird open (it downloads mostly while the computer is
> left alone). Later backups add the rest by themselves.

Stopped (one or more accounts stopped, with or without others still downloading):

> Do this on that computer:
> 1. Open Thunderbird and leave it open, with the computer left alone, for an hour or more. If it
>    asks for a password, enter it.
> 2. If Status & health in MailArchive Desktop still shows the same count the next day: in
>    Thunderbird, click the menu button (≡, top right), then Account Settings. On [that
>    account's / each stopped account's] "Synchronization & Storage" page, make sure it is set
>    to keep all of the account's mail on this computer.

There is no separate sentence for an account still downloading: step 1 is what it needs.

### The once-only lines

- Still downloading: `You will get this email once. If the download then stops, you will get one more.`
- Stopped: `You will get this email once for each account that stops downloading, not after every backup.`

The sign-off is unchanged.

### Case 5 with every proposal applied

> **Subject:** MailArchive: Thunderbird stopped downloading (backups OK) — Thunderbird archive 2026
>
> Your backups are working, but Thunderbird has stopped downloading one account's mail, so the
> backups cannot get the rest until it starts again.
>
> Archive: C:\Users\alexandra.dimarco-wilson\OneDrive - DiMarco Technologies Inc\Documents\Mail Backups\Thunderbird archive 2026
> Computer: DESKTOP-7Q2K9LM
>
> The account kept in the folder imap.gmail.com: about 1,670 of 238,080 messages downloaded, no
> progress since Nov 5.
> The account kept in the folder imap.gmail.com-1: about 400 of 2,500 messages downloaded, still
> going after 5 days.
>
> Do this on that computer:
> 1. Open Thunderbird and leave it open, with the computer left alone, for an hour or more. If it
>    asks for a password, enter it.
> 2. If Status & health in MailArchive Desktop still shows the same count the next day: in
>    Thunderbird, click the menu button (≡, top right), then Account Settings. On that account's
>    "Synchronization & Storage" page, make sure it is set to keep all of the account's mail on
>    this computer.
>
> To see which account that is: in Thunderbird, click the menu button (≡, top right), then
> Account Settings; each account's "Server Settings" page shows the folder its mail is kept in, at
> the bottom.
>
> You will get this email once for each account that stops downloading, not after every backup.
>
> This message was sent by MailArchive's built-in backup alerts.

The phone preview now reads "Your backups are working, but Thunderbird has stopped downloading
one account's mail…" instead of the path.

## 4. What this review did not check, and the smallest check for each

- **The phone preview widths** are an estimate. Check: on the owner's phone, look at an existing
  MailArchive alert in the mail list and count how much of its subject and body shows.
- **"Server Settings" shows the folder at the bottom ("Local Directory")** and **"Synchronization
  & Storage" in Thunderbird 160**: from memory and the source re-read only. Check: lab row MA-1191,
  one look at each page on the Mac's Thunderbird.
- **"If it asks for a password" as a likely cause** of a stopped download is a judgement, not an
  observation.
- **"an hour or more"** rests on the earlier live finding that Thunderbird's offline download waits
  for its periodic check (about 10 minutes); it was not timed for a stopped account.

## Provenance

- Date worked on: 9 November 2026 (the Mac's clock).
- Drew on: the files listed at the top, read at a385f97, and the nine rendered emails. The rendered
  words are the code's; everything graded or proposed here is this reviewer's opinion.
- Register: a review (finding plus proposals).
