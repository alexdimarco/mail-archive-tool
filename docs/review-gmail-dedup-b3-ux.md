# UX contract walk: Gmail de-dup slice B3 (and a re-walk of its friction fixes)

**Verdict: no X rule fails outright. Three are Partial (X6, X8, X10), all for one reason: the
Gmail lines are missing from some of the places that should carry them. Fifteen of the seventeen
friction findings (F1 to F17) are fixed as a person sees them. Two are fixed only in part: F15
(the Run now hint shows only after a reload) and F8 (`status` says nothing after an unanswered
terminal question).** The walk found 2 MED and 9 LOW new findings (U1 to U11). Neither MED blocks
the folder views. U1 is a small change. U2 is a posture call for the lead.

| Header | Value |
|---|---|
| Reviewed | branch `gmail-dedup`, commit `25c5bf0` (B3 and its friction fixes F1 to F17; F18 is open on purpose, the owner's call) |
| Process | `../assurance-kit/process/ux-contract.md`, `../assurance-kit/process/friction-review.md` |
| Contract | `docs/ux-contract.md` (X1 to X17) |
| Friction review re-walked | `docs/review-gmail-dedup-b3-friction.md`; the fix calls are in `docs/design-gmail-dedup.md` §13 "[B3 friction fixes]" |
| Date | 2026-11-13 |
| Windows (first) | Windows 11 VM "Windows 11" (Parallels, ARM, running the amd64 build under emulation). The binaries were cross-built from `25c5bf0` (`GOOS=windows GOARCH=amd64`; `mailarchivew.exe` with `-H=windowsgui`). Microsoft Edge ran as a separate lab instance with its own profile folder and DevTools port, and the Desktop's browser opener was shimmed so it never opened the VM user's own Edge. Every click and read went over that instance's DevTools socket, and only to a page whose address was the lab Desktop's own port. No keys or clicks were sent to any other window. |
| Mac | Native build. The terminal question ran in a real pseudo-terminal (`expect`). The Desktop was read and posted to with `curl` (no browser), with `osascript` shimmed so it keeps the one-time link and answers the folder picker. |
| Lab | A throw-away, Gmail-shaped Thunderbird profile written by a lab-only helper (the friction reviewer's `b3lab`, never committed). Every run used a throw-away `-config`, `HOME`, `APPDATA` and `LOCALAPPDATA` (Windows: `C:\Users\Public\b3ux`, `%LOCALAPPDATA%\b3ux`; Mac: the worktree's `.claude/lab/mac`). No real mail was used, no email was sent, and the owner's settings, archives and Thunderbird profile were not touched. |
| Screenshots | private, not committed: `.claude/lab/shots/` in this worktree (`w01-rows`, `w02-notnow-note`, `w03-settings-on`, `w04-settings-question`, `w05-reader`, `w06-receipts-page`, `w07-message-this-folder`) |
| Not walked live | Linux (the same code on every platform); the encrypted terminal question (U5 comes from reading the code); a scheduled run's backup alert on a version-8 archive (U2's alert half comes from reading the code); the Desktop's Add another archive wizard (its newer-format words were reached through "Open an archive folder…", which shares the check, `folderProblem`). |

## Lab archives

**Windows.** Each archive was made from one profile:
- `Sam Gmail`: backed up before the account counted as Gmail, then backed up with no terminal, so the
  question waits. It has a job file set to Run now only.
- `No settings`: the same, with no job file.
- `Said not now`: the same as `Sam Gmail`.
- `Folders on`: a new archive of the Gmail account, switched on at its first run. It then got
  hand-made listed memberships and `reindex -rebuild`.
- `Tidy-up`: listed, then stamped version 8.
- `Tidy-up 2`: a version-8 copy that is not listed.

**Mac.** `base` waits for the question. `yes`, `notnow`, `wait` and `lone` are copies of it, and
`tidy` is a version-8 copy.

## X rules

| X | Applies to B3 as | Result | Evidence |
|---|---|---|---|
| X1 Typed exits | the backup's refusal of version 8, `add-archive`'s refusal, `status`, terminal runs | **pass** | Windows: a backup of `Tidy-up` exits `1`; `add-archive "Tidy-up 2"` exits `1` with "Nothing was changed"; `status` and `status -json` exit `0`. Mac: the Yes, Not now and no-answer terminal runs each end with "Done." |
| X2 One refusal voice | the newer-format refusal | **pass** (C2 still open) | Each refusal says what happened, that nothing is lost, and what to do (the tidy-up words). A command-line backup prints the words twice (`FAILED: …` and then `mailarchive: …`), each time with "(manifest …: format version 8; this build understands 7)". That doubling was there before B3; it is the standing condition C2. |
| X3 Help is total | `-gmail-folders` | **pass** | The root `-h` and `schedule -h` both name the flag with F14's help: "Say yes to the Gmail question (\"Gmail: show every folder?\") … This cannot be undone …". |
| X4 One grammar | `-gmail-folders` | **pass** | kebab-case, and it adds to a backup (no new verb). |
| X5 Flags mean the same | `-gmail-folders`; the kept answer | **pass** | `schedule` accepts the flag through the shared builder. The answer is kept in one place on this computer (`gmailanswer`: the job file, or `<key>.gmail-answer`). The Desktop, the terminal and `run-job` all read it, so a terminal Not now (Mac, `notnow`) makes the Desktop's row stop asking and puts Settings' copy in its place ("You said Not now on this computer."). |
| X6 Legibility surface | the waiting question; the tidy-up | **Partial** | Met: `status` shows the waiting line (F8), and version 8 is its own WARN, `manifest_tidying`, with the remedy. Not met: U3 (version 8 also says "not switched on … back up once at a terminal"), U4 (silent after an unanswered terminal question), U7 (no JSON reason code). |
| X7 No traceback | every B3 surface | **pass** | None seen on any surface, Windows or Mac. |
| X8 Output discipline | the terminal question; `search`; `status -json` | **Partial** | Met: the question is asked on stderr (code: `askGmailAnswer(…, os.Stderr, …)`, and MA-1293; in a pseudo-terminal the two streams cannot be told apart, so this was not split live). `search`'s "In: …" goes to stdout and refusals go to stderr (Windows, streams captured apart). Not met: `status`'s text shows the Gmail count line and the waiting line, and `status -json` has neither (U7). |
| X9 Loopback and guarded | `POST api/gmail-answer` | **pass** | Windows Edge, from the lab page: no CSRF header gives `403 refused: this request did not come from this MailArchive Desktop page…`; a GET gives `405 POST only`. |
| X10 One status truth | Desktop health view vs `status` | **Partial** | The posture agrees (both are RED after a refused backup of `Tidy-up`, both WARN `manifest_tidying`). The Desktop's Status & health view has neither Gmail line that `status` prints: the count line and the waiting line (U7). |
| X11 Plain labels | the question box and its buttons | **pass** | The box heading is "Gmail: show every folder?" and the buttons are "Yes, show every folder" and "Not now" (`w01`). The guide "How to use MailArchive" names no Gmail control. That is allowed (it names only controls that are drawn), but see U11. |
| X12 Locked, not blank | `api/gmail-answer` with no session | **pass** | Windows: `401 {"ok":false,"error":"This page needs its private link: …"}`. |
| X13 Content reads guarded | — | **pass** | B3 adds no GET that returns mail content. The answer route is a guarded POST. |
| X14 Look before writing outside | — | n.a. | The answer file is MailArchive's own setting, not a place the person chose. |
| X15 Mail-derived text inert | the answer note; the reader's "In:" line and Folder filter | **pass** | The page script writes the note with `textContent`. The reader runs `shownFolder` and then `esc` on each folder name. Folder names in the folder pages and the message page's link are escaped (`html.EscapeString`). |
| X16 Pre-sync | — | n.a. | |
| X17 One program name | the new texts | **pass** | "MailArchive", "MailArchive Desktop", "MailArchive 1.8". No former names. |

## Friction findings F1 to F17, re-walked

| F | Was | Now, as a person sees it | Outcome |
|---|---|---|---|
| F1 | A Desktop Not now could not be undone | `Said not now`, made current: Settings shows the full question box with "You said Not now on this computer. You can still say yes here." (`w04`). The same happens after a terminal Not now (Mac). | **fixed** (see U6, U10) |
| F2 | No backup settings: both answers refused | `No settings`, Yes: "Yes. From this archive's next backup on…"; after a reload the row says "It has no backup settings on this computer: to back it up from here, click Switch to this archive, then choose How often in Settings and click Save settings." | **fixed** (see U1) |
| F3 | The note was easy to miss | The note takes the question's place inside its box, inside the visible area, with no reload (`w02`). | **fixed** |
| F4 | Text (a) failed the phone test | The new words say what may be missing and why, what Yes does, "Nothing is deleted", that every computer needs 1.8, and "This cannot be undone." They read well on a phone. Two things remain for the owner. First, the text is five sentences long. Second, "Yes lists each email in every folder it is in" is not true until D1 and M1 ship, the release gate the friction review already set. | **fixed, pending the owner's sign-off** |
| F5 | The question was not marked off | It has its own bordered box with a heading, apart from Switch, Pause, Remove and Delete (`w01`). | **fixed** (see U10) |
| F6 | Switching on was silent | Run now on `Sam Gmail` after Yes: "Gmail: switched on for this archive. Each email is stored once and shown in all of its folders. Every computer that backs up into this archive needs MailArchive 1.8 or newer." Settings then shows the setup line and the 1.8 line under Archive location (`w03`). | **fixed on the Desktop**; the command line is U9 |
| F7 | The terminal asked every time and waited forever | Mac: Enter gives "Not now: nothing changed, and MailArchive will not ask again on this computer…", and the next terminal backup asks nothing. No answer gives "No answer, so Not now for this backup…" at 120 s, and that is not remembered. | **fixed** (see U4, U5) |
| F8 | Nothing said the question waits | A run with no terminal (Windows setup) and `status` (Windows `Sam Gmail`, `No settings`) both say "Gmail: not switched on for imap.gmail.com — answer the Gmail question in MailArchive Desktop, back up once at a terminal, or add -gmail-folders…". | **fixed in part**: U4, U3 |
| F9 | Other screens contradicted the tidy-up words | Windows, `Tidy-up`: the Scheduled backup card, Settings, the schedule-install refusal ("… Nothing was changed."), `add-archive`, the command-line backup and `status` (WARN `manifest_tidying`) all use the tidy-up words. The card says "a backup that re-reads this archive's mail files". Mac: "Open an archive folder…" on a version-8 copy uses the same words. | **fixed for the words**; the posture is U2 |
| F10 | "This folder" led to All Mail silently | A message opened from `Receipts` in `Folders on` says "This folder: All Mail" (`w07`). | **fixed** (for pages written after the switch, as the builder recorded) |
| F11 | `[Gmail]/All Mail` was shown | Folder page: "(saved in All Mail)" (`w06`). Reader: the "All Mail" and "Sent Mail" pills, "In: All Mail, Receipts", and the Folder filter's "All Mail (12)" (`w05`). | **fixed** |
| F12 | The count line was jargon | `status`: "Gmail: 19 emails saved; 3 of them are also listed in another folder without a second copy." It still sits beside Thunderbird's "(12 messages)", as the builder recorded (left for D2). | **fixed** |
| F13 | Yes was said twice; the question went to stdout | Mac, typing y: one line, "Yes: from this backup on, …". The question goes to stderr (see X8). | **fixed** |
| F14 | The flag's help | The help leads with "Say yes to the Gmail question …" and says "This cannot be undone". | **fixed** |
| F15 | The yes line did not say how it takes effect | After a reload, the row says "Nothing backs it up by itself: click Run now on the Scheduled backup card…" (for `Sam Gmail`) or how to set it up (for `No settings`). The note shown at the moment of answering says neither (U1). | **fixed in part**: U1 |
| F16 | The tidy-up words were drawn in light grey | Windows Edge: the row's tidy-up words are at full contrast, and the path beside them stays greyed (`w02`, bottom). | **fixed** (seen by eye on Windows) |
| F17 | `search` showed only the home folder | "In: [Gmail]/All Mail, INBOX, Receipts" (Windows `search -folder Receipts`). It keeps the stored path, not F11's shown name, which is the builder's call. | **fixed** |

## New findings

Severity is HIGH (blocks ship), MED (fix before release) or LOW (polish).

**U1 · MED. The note shown when someone answers Yes does not say what starts the backup.**
- *Where:* Windows Edge, Yes on `Sam Gmail` (Run now only) and on `No settings` (no backup
  settings).
- *What happens:* Both notes say only "Yes. From this archive's next backup on, each Gmail email
  is stored once and shown in all of its folders." F15's words, "click Run now…" and "choose How
  often in Settings…", appear only after a reload. F3 removed the automatic reload, so the person
  who has just answered never sees them. For `No settings`, nothing will ever start that backup.
- *Fix:* Have `api/gmail-answer` return `gmailYesWaitsFor(path, current)` as the Yes note. It is
  the same line the row shows after a reload.

**U2 · MED. A backup refused because of version 8 is still a failed backup: RED, "FAILED", and a
backup alert.**
- *Where:* Windows `Tidy-up`, after one refused command-line backup: the Desktop's Status & health
  view, and `status`.
- *What happens:* The posture is RED: "the last run … FAILED: Another computer started tidying …
  Nothing is lost. …". The view shows the long tidy-up words three times: in Last run, after RED,
  and after WARN. A scheduled backup on this computer would be refused at every run while the other
  computer tidies. By the code, each refused run is recorded as failed, so the backup alerts
  (failure, once per streak) would be sent. That sends the "something is wrong" message F9 set
  out to remove. The builder recorded this as their call ("a refused scheduled backup's last-run
  line is still the run's failure").
- *Fix (the lead's call):* Treat a newer-format refusal the way a busy archive is treated: a skip,
  recorded with its own outcome, not `run_failed`. Then the only posture is WARN
  `manifest_tidying`, and no failure alert is sent.

**U3 · LOW. A version-8 archive's `status` also says "Gmail: not switched on … back up once at a
terminal".**
- *Where:* Windows, `status` of `Tidy-up`.
- *What happens:* The line comes from this computer's last run, before the other computer switched
  the archive on and began tidying. It advises a backup that the WARN directly above it says this
  computer cannot do.
- *Fix:* Give no waiting line when the manifest is unreadable or newer. More generally, give none
  when the loaded manifest already has the store switched on.

**U4 · LOW. After an unanswered terminal question, `status` says nothing about it.**
- *Where:* Mac, `wait` (no answer for 2 minutes).
- *What happens:* The run records the store as held (`gmail_held`, as a Not now would be), and
  `status` prints the waiting line only for stores still off (`gmail_off`). The Desktop still
  asks (it reads both lists), and the next terminal backup asks again, so this is LOW. But no
  answer is not an answer, and the command-line view says nothing at all.
- *Fix:* Record an unanswered question as `gmail_off`, or have `status` read the held stores
  unless a Not now is kept on this computer.

**U5 · LOW (by reading the code). For an encrypted archive, the terminal's Not now words are untrue.**
- *What happens:* The terminal always says "MailArchive will not ask again on this computer". For
  an encrypted archive the Not now is never kept (O1: nothing beside it may name a Gmail account),
  so it asks at every terminal backup.
- *Fix:* Use an encrypted variant: "Not now: nothing changed. MailArchive asks again at the next
  backup you start at a terminal."

**U6 · LOW. Settings' Not now note sends the person to the place they are already in.**
- *Where:* Windows Edge, Not now in Settings' copy for `Said not now`.
- *What happens:* The note says "To say yes later, make this the current archive and answer the
  same question in Settings."
- *Fix:* In Settings, say "Nothing changed. You can say yes here any time."

**U7 · LOW. The Gmail lines are in `status`'s text only.**
- *What happens:* The waiting line (F8) and the count line are not in `status -json` (no field and
  no reason code), and not in the Desktop's Status & health view. This is why X6, X8 and X10 are
  Partial.
- *Fix:* Add them to `health.Assess` as plain reasons with codes (for example `gmail_waiting`,
  `gmail_counts`), so text, JSON and the Desktop all carry them from one source.

**U8 · LOW (words, the owner's text (b)). "To finish: finish it on that computer" reads as a
stutter.**
- *What happens:* That is the phrase as it reads now. Also, when the words refuse an *open* or an
  *add* (Mac "Open an archive folder…", Windows `add-archive`), "this computer does not back up
  into it" understates what happens: the archive cannot be opened or listed here either.
- *Fix:* Change the first to "To finish: resume it on that computer, or install the newest
  MailArchive here and click Resume tidy-up on this archive's card." Change the second to "Until
  it finishes, this computer cannot open it or back up into it." Both changes are for the owner's
  sign-off.

**U9 · LOW. A new Gmail archive made at a terminal never says it needs 1.8.**
- *Where:* Windows, the first backup of `Folders on`.
- *What happens:* The run says "Gmail: a new archive — from now on each email is stored once and
  shown in all of its folders". `status` then says only the count line. F6's 1.8 line is on the
  Desktop only.
- *Fix:* Add `state.GmailNeedsLine` to that log line, and to `status`'s Gmail count line.

**U10 · LOW. After a Not now, Settings shows the full question box for as long as the archive
stays as it is.**
- *Where:* Windows Edge, `w04`.
- *What happens:* The box keeps the full five-sentence question and a blue primary "Yes, show
  every folder", which asks the person again each time they open Settings. F1 asked for a short
  line ("Gmail folders: not switched on. [Turn on…]"). Yes, a step that cannot be undone, is the
  filled primary button wherever the box appears, with no second confirmation.
- *Fix (the lead's call):* After a Not now, show the one line, with its button opening the box.
  Draw "Yes, show every folder" as a secondary button, like "Not now".

**U11 · LOW. The guide "How to use MailArchive" does not mention the Gmail question.**
- *What happens:* The guide does not say what the question is, that it is asked once, or that
  Settings keeps it after a Not now (the friction review's F1 noted this too).
- *Fix:* Add one paragraph to its Thunderbird part.

**Seen and not a finding:**
- The folder page looked garbled ("·" and "📎" printed as other characters) only in the PowerShell
  console that relayed it. The page is UTF-8, and Edge shows it correctly (`w06`).
- INBOX lists "Lunch on Friday?" twice. This is the lab helper's doing, as the friction review
  noted.
- Duplicates in the reader before the tidy-up are expected until D2.

## For the lead

- Fix U1 before release: it is one line, and it completes F15.
- U2 is a posture decision: a skip, or keep RED. As built, a person whose other computer is
  tidying gets RED and failure alerts on this computer for as long as the tidy-up lasts.
- The release gate stands: B3 must not ship ahead of D1 and M1. The Run now result now says
  "Gmail: switched on … shown in all of its folders" while nothing visible changes in this build.
- The words in `internal/state/dedup.go` still need the owner's sign-off. They pass the phone-reader
  test, with U5, U6 and U8 as the changes to put to the owner.
