# Friction re-walk: the 1.7.2 patch after its fix round

Walked 14 November 2026, 12:21 to 12:45 Mac time (EST), on the Mac (native), in throw-away Linux
containers (`golang:1.26`) and on the Parallels VM "Windows 11" as SYSTEM (nobody is signed in to
it). Process: `../assurance-kit/process/friction-review.md`. Scope: branch `patch-1.7.2` at
`ff29ee2`, this round being `git diff c71467d..ff29ee2` (the fix round `c8d8657` and its notes
`ff29ee2`); the first walk is `docs/review-patch-1.7.2-friction.md` (F1 to F11) and its follow-up
table; the UX contract's X1, X2 and X8; the owner's rule that every backup outcome says why and how
to fix it in plain words for a phone reader. The adversarial re-check ran on this Mac at the same
time; no cell here depended on tight timing, and the one timing question (C6) was settled by a
separate timed run.

**Verdict: SHIPPABLE-WITH-EDITS.** The fix round did what the first walk asked. Every lost-lock
ending now leads with plain words on every surface (F1), the right drive put back says the backup
"did not finish and could not record how it ended" and names a swapped drive (F3), the stray
folder of F5 is gone on Mac, Linux and Windows, and a short outage while a backup only reads ends
OK (P1). The other archive kept its records, pages, README and index in every cell. Two things must
change before the tag, both in the release notes: they quote only one of the two endings a swap
gives, and the common one for a person swapping USB drives is the other (G3); and "a brief hiccup
is not a failure" is not true for a backup that is writing when the hiccup comes (G4). Two more
are text-only and in one place (`internal/app/archiveguard.go`), and the owner's "polish ships"
rule says fold them now: every surface reads "Backup failed: Backup stopped: …" (G1), and the
"Details:" tail repeats the full lock path in the places with least room (G2). Nothing here needs a
gate, a baseline or a behaviour changed.

## Header

| | |
|---|---|
| Reviewed commit | `ff29ee2` (worktree `wip/patch-1.7.2-fric2`, clean but for its untracked `.claude/`) |
| Programs | `mailarchive` built from `ff29ee2` for macOS arm64, Linux arm64 (container) and Windows amd64 (VM, emulated). One lab-only helper, never committed: `fric2lab` (writes an archive's run-job file and its "Only when I click Run now" record with the wizard's own `RunJobSpec` and `schedule.DescribeManual`, no scheduler entry), built from a build-tagged file deleted from the worktree right after the build. Copied from the first walk's lab: `genmbox.py`, `swapdirs` (macOS `renamex_np RENAME_SWAP`), the loopback SMTP sink `fakesmtp` (127.0.0.1:2525, no relay), the prepared mbox files |
| Own settings | Mac: `HOME` and `-config` under the worktree's `.claude/testdata/fric2/mac/`; the Desktop ran with PATH shims for `osascript` and `open` that kept the one-time link, so no browser, dialog or notice opened; it was driven with `curl` only and shut down from its own API (`api/shutdown`). Linux: everything inside each container's `/work`. Windows: `C:\fric2lab` and drive letter `V:` (a `subst`, removed at the end: "V: there at the end: False") |
| Email | Linux containers only (a scheduled failure raises a desktop notice on a Mac). The sink kept 5 messages (4 in the first container, 1 in the second); no real mail was sent. The lab's run-job carries a "Run now only" record, as in the first walk; the email path is run-job's, the same for a daily schedule |
| Mail | Made-up mbox files: Mac source A grown from 3,000 to 30,000 messages, Linux 2,000 to 20,000, Windows 2,000 to 14,000; source B 30 messages |
| How the folder was changed | Mac terminal: `mv` A away, `mv` B in (a gap of milliseconds: an unplug and a plug), and `swapdirs` (no gap). Mac Desktop: `swapdirs`. Linux: `mv`, `mv` (gap); `mv --exchange` was refused by the container's file system, so the no-gap cell made the archive path a link and replaced it in one rename. Windows: `subst V: /d`, then `subst V:` to B's folder |
| Outages | Mac only: `chmod 000` on the archive's parent folder, or on its lock file, then back |
| Baseline | `docs/scenario-catalog.md` unchanged by this review. No catalog row, gate, covers marker, test or product file was changed |
| Cells walked | 21 (Mac terminal 10, Mac Desktop 4, Linux 5, Windows 2), and one the lab could not make (E1b) |
| Functioning | 21 of 21: in every cell the other archive's records, pages, README and index stayed as they were, and the stopped run's own archive got its failure recorded exactly when it was still there and still its own |

## The walk

Actor: a person with two USB drives used in turn, not technical, often reading on a phone. Grade is
how well the words serve that person: **high** (clear), **med** (works, with a note), **low**
(misleads or leaves them stuck). Paths are shortened to `…/drive/Archive`; the lab's paths are long
(a worktree), so every sentence below is about 100 characters longer than it would be on
`/Volumes/Backup/Archive`.

| # | Cell | Functions? | What the person sees | Grade | Findings |
|---|---|---|---|---|---|
| C1 | Mac terminal: A's backup writing, A moved away, B moved in (gap) | yes: B's 7 top files byte-for-byte unchanged, 38 files before and after, no folder of A's in B; exit 1 at 6 s | twice (log line, then `mailarchive:`): `the archive location …/drive/Archive is unavailable (create output dir …/drive/Archive/A/Inbox: stat …/drive/Archive: no such file or directory) — reconnect the drive or share and re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.` | med | G3 |
| C1' | the same, A put back: `status` and `status -json` | yes | `Last run: … → never finished` and `RED: the backup started 2026-11-14 12:21 EST did not finish and could not record how it ended: MailArchive was stopped, the computer shut down or restarted, or this archive's drive was disconnected or swapped during the backup. Run the backup again.`; `-json` `run_never_finished`, `last_run.status` `running`. Next backup `Done.`, exit 0 | high (F3), one note | G5 |
| C2 | Mac terminal with `-log`: A and B exchanged in one step (`swapdirs`) | yes: B unchanged (41 files before and after; the 3 extra since C1 are `verify`'s and `status`'s, F10); none of A's files landed in B; exit 1 at 6 s | terminal: one paragraph, `mailarchive: Backup stopped: the archive folder changed during the backup. Another drive or folder is now at …/drive/Archive (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: lock …/drive/Archive/.mailarchive.lock was replaced during the run.` Run log: the warning `the folder at … changed during this backup (…), so this backup's result was not written into it.`, then `FAILED: Backup stopped: …` | med | G1, G2, G6 |
| C3 | Mac terminal: A's own lock file deleted 6 s in | yes: recorded failed in A (last-run record, run history, BACKUP-NEEDS-ATTENTION.txt); exit 1 at 10 s | terminal (twice), `status` (`Last run: … → failed: …` and `RED: the last run (…) FAILED: …`), the notice file's `Reason:`: `Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was deleted during the backup, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder. Details: lock …/drive/Archive/.mailarchive.lock was removed during the run.` | med | G1, G2 |
| C4 | Mac terminal: A's own lock file written over 6 s in | yes: recorded failed in A; exit 1 at 10 s | the same places: `Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was changed during the backup: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished. Details: lock …/.mailarchive.lock was replaced during the run.` | med | G1, G2 |
| C5 | Mac terminal: A's folder moved away, nothing in its place (F5) | yes: **nothing made at the old path** (`drive/` empty after the run); exit 1 at 6 s | the `unavailable (create output dir …: stat …: no such file or directory)` sentence of C1; with A back, C1's F3 words | med | G3 |
| C6 | Mac terminal: nothing new to save; the archive's parent folder unreadable from 1 s to 6 s (P1) | yes: exit 0, `Done. exported=0`, status ok | nothing but the normal run. Timed: a nothing-new run takes 0.74 s here, so this one (6 s) waited out the outage at its end-of-run writes | high | G4 (the note's wording) |
| C6b | the same, the lock file unreadable from 1 s to 6 s | yes: exit 0 at 6 s | nothing but the normal run | high | — |
| C6c | the same, the lock file unreadable from 0.5 s to 8.5 s | yes: exit 0 at 12.5 s (the waits grow to 4 s) | nothing but the normal run | high | — |
| C7 | Mac terminal: the archive's parent folder unreadable for 5 s **while the backup writes** | no failure of the guard, but the backup fails: exit 1 at 11 s; nothing recorded (the folder was still unreadable at the end) | twice: `the archive location … is unavailable (lstat …: permission denied) — reconnect the drive or share and re-run. …`; then `status`: C1's "did not finish" RED. Next backup: `swept orphan: …/A/Inbox/.mailarchive-….tmp`, `Done.` | med | G4 |
| D1 | Mac Desktop, Run now; A and B exchanged in one step mid-run | yes: B's file list (41) the same before and after; none of A's landed | Run now's note: `Backup failed: Backup stopped: the archive folder changed during the backup. Another drive … Details: lock …/.mailarchive.lock was replaced during the run.`; its live log ends on the F4 warning. With B at the path: Status & health and Recent backups show B's own good backup. With A back: Status & health `Last run: … → never finished` and the F3 RED; Recent backups has no row for the stopped run | med | G1, G2, G5, G6 |
| D2 | Mac Desktop, Run now; A's own lock file deleted | yes: recorded in A | note `Backup failed: Backup stopped: MailArchive's lock file … was deleted …`; Recent backups `Nov 14, 2026 12:30 PM — Failed — Backup stopped: MailArchive's lock file … Details: lock …/.mailarchive.lock was removed during the run.`; Status & health `Last run: … → failed: Backup stopped: …` and `RED: the last run (…) FAILED: Backup stopped: …` | med | G1, G2 |
| D3 | Mac Desktop, Run now; A's own lock file written over | yes: recorded in A | as D2 with the "changed" words | med | G1, G2 |
| D4 | Mac Desktop, Run now; A's folder moved away, nothing in its place | yes: nothing made at the old path | note `Backup failed: the archive location … is unavailable (lstat …: no such file or directory) — reconnect the drive or share and re-run. …`. Status & health while A is away: `RED: the scheduled program  no longer exists — the job cannot run; re-run \`mailarchive schedule -out … -install\`` and `WARN: the schedule "" is recorded but not present in this host's scheduler …` beside the right `WARN: the archive location … is not reachable — … connect it and check again`. With A back: the F3 RED | med; low while the drive is out | G3, G10 |
| E1 | Linux scheduled job (`run-job`), alerts on; A moved away, B moved in (gap), 1 s in | yes: B's 38 files unchanged; exit 1 at 1 s | stderr `mailarchive run-job: the archive location /work/drive/Archive is unavailable (create output dir /work/drive/Archive/A/Inbox: stat /work/drive/Archive: no such file or directory) — reconnect the drive or share and re-run. …`; the email: subject `MailArchive backup FAILED: /work/drive/Archive`, `Reason:` the same sentence. A put back: the F3 RED | med | G3 |
| E2 | Linux scheduled job; A's own lock file deleted, new alert streak | yes: recorded in A; `status -json` `run_failed`, `last_run.error` the plain words | email `Reason:` `Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was deleted during the backup, … Details: lock /work/drive/Archive/.mailarchive.lock was removed during the run.` | med | G1, G2 |
| E3 | Linux scheduled job; A's own lock file written over, new streak | yes: recorded in A | email `Reason:` the "changed" lock words | med | G1, G2 |
| E4 | Linux scheduled job; A's folder moved away, nothing in its place, new streak | yes: **nothing made at the old path** | email `Reason:` the `unavailable (create output dir …)` sentence; A back: the F3 RED | med | G3 |
| E5 | Linux scheduled job; the archive path switched from A's folder to B's in one rename, 1 s in (twice) | yes: B's records unchanged; **541 and 288** of A's new message files landed in B's folder in under a second before the next save's proof stopped the run | email `Reason:` `Backup stopped: the archive folder changed during the backup. Another drive or folder is now at /work/drive/Archive (…). Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: lock /work/drive/Archive/.mailarchive.lock was replaced during the run.` `verify` on B: `attested`, `unexpected=288`, every file listed, then the F8 note. A back: the F3 RED | med | G1, G2, G11 |
| E1b | Linux: A and B exchanged with `mv --exchange` | not walked: the container's file system refused the exchange (`Unknown error -1`); the run ended ok in A | — | — | — |
| W1 | Windows terminal (SYSTEM): `V:` moved from A's folder to B's 5 s in | yes: B's 38 files unchanged; exit 1 at 5.1 s | twice: `the archive location V:\Archive is unavailable (GetFileAttributesEx V:\Archive: The system cannot find the path specified.) - reconnect the drive or share and re-run. …`. A's real folder and A back at `V:`: the F3 RED (`run_never_finished`) | med | G3 |
| W2 | Windows terminal: `V:` removed 5 s in, nothing in its place | yes: `V:` not made again; exit 1 at 5.1 s | the W1 sentence; A back: the F3 RED | med | G3 |

All three swaps with a gap (C1, E1, W1) ended on "unavailable"; all four with no gap (C2, D1, E5
twice) ended on "Backup stopped: the archive folder changed". A person who unplugs one USB drive and
plugs in another always leaves a gap, and a backup that is writing at that moment meets it.

## The first walk's findings, re-walked

| F | Then | Now | Where seen |
|---|---|---|---|
| F1 lock's internal sentence on every surface | HIGH | **fixed**: the plain words lead on every surface; kind `lock_lost`, exit 1 and every machine field unchanged. Two wording faults left, G1 and G2 | C2 to C4, D1 to D3, E2, E3, E5 |
| F2 release notes | MEDIUM | **partly**: the overstatements of the first draft are gone; two new ones, G3 (quotes one ending of two) and G4 (hiccup) | — |
| F3 "never finished (crash, kill, power loss)" on the right drive | MEDIUM | **fixed**: "did not finish and could not record how it ended: …, or this archive's drive was disconnected or swapped during the backup. Run the backup again." on Mac, Linux, Windows, in `status`, `-json` reasons and Status & health. The line above it still says "never finished" (G5). Recent backups still has no row for a stopped swap (the Type II breadcrumb was not taken, as recorded) | C1', C5, C7, D1, D4, E1, E4, E5, W1, W2 |
| F4 jargon warning | MEDIUM | **fixed** as words; it now repeats the error's first sentence just above it (G6) | C2 run log, D1 live log, E5 run log |
| F5 vanished folder made again | MEDIUM | **fixed**: nothing at the old path on all three systems; the run ends "unavailable" at once | C5, D4, E4, W2 |
| F6 system error in the lock sentence | LOW | **fixed** for the lock sentences; the "unavailable" sentence's system text got longer with F5 (G3) | — |
| F7 encrypted sentence | LOW | **fixed** as words (code-read); one note (G7) | not walked |
| F8 `verify`'s note | LOW | **fixed**: the note names the swap | E5 |
| F9 failure printed twice | LOW, pre-existing | unchanged; with -log off a "changed" stop now prints three near-identical paragraphs (G6) | C3, C4 |
| F10 `status` writes the lock and index side files | INFO, pre-existing | unchanged (B's lock and `search.db-shm`/`-wal` changed between C1 and C2 by `status`, not by a backup) | C2 |
| F11 Windows start failure words | LOW | words adopted, but they reach stderr only, which the Start menu's program has none of (G8) | code-read |
| P1 a short outage while a backup only reads | (adversarial) | **holds**: ends OK, waiting at its end-of-run writes | C6, C6b, C6c |

## New findings

Severity is for the person, not the code. Type is the process's: III is buildable-away wording, II
a design choice. "Must" means before the tag; "should" means text-only and cheap, fold now under
the owner's "polish ships" rule; "optional" is the lead's call.

### G1 — MEDIUM (III, should): every frame doubles the lead, "Backup failed: Backup stopped: …"

**Seen:** Run now's note (D1 to D3), the terminal's `FAILED: Backup stopped: …` (C3, C4), `status`
(`→ failed: Backup stopped: …`, `RED: … FAILED: Backup stopped: …`), Status & health, Recent backups
(`Failed — Backup stopped: …`). Every other failure reason in MailArchive is a lower-case clause
that these frames lead into ("Backup failed: the disk is full"); the first walk's F1 proposal did
not allow for the frames. The first words a phone reader sees on Run now are a stutter.

**Current** (`archiveguard.go`, `stoppedChangedWords`, `stoppedDeletedWords`, `stoppedRewrittenWords`):
each begins `Backup stopped: `.

**Proposed:** drop `Backup stopped: ` and start each in lower case (full text under G2). Then Run
now reads `Backup failed: the archive folder changed during the backup. Another drive …` and the
email `Reason: the archive folder changed during the backup. …`. The release notes then say "it
says the archive folder changed during the backup" (no quotation of a capitalised lead).

### G2 — MEDIUM (III, should): "Details:" repeats the full lock path where there is least room

**Seen:** every surface of C2 to C4, D1 to D3, E2, E3, E5. In the "changed" sentence the folder's
path appears twice; in the lock sentences the "Details:" tail is the only path, and it is the
hidden lock file's. On a short real path the deleted-lock sentence is about 375 characters, about
ten lines of a Recent backups row on a phone, and the last of them is `Details: lock
/Volumes/Backup/Archive/.mailarchive.lock was removed during the run.` The archive's path is
already beside it on every surface (the email's `Archive:` line, the card's archive, `status`'s
`Archive:`, the `-out` the person typed).

**Current:** `Details: lock <folder>/.mailarchive.lock was replaced during the run.` and `Details:
lock <folder>/.mailarchive.lock was removed during the run.`

**Proposed** (G1 and G2 together; the phrases "was replaced during the run" and "was removed during
the run" stay for any test that looks for them; the run log can keep the full `LostError` text):

- folder changed:
  > the archive folder changed during the backup. Another drive or folder is now at `<folder>` (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: the lock file was replaced during the run.
- own lock file deleted:
  > MailArchive's lock file in the archive folder (.mailarchive.lock) was deleted during the backup, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder. Details: the lock file was removed during the run.
- own lock file changed:
  > MailArchive's lock file in the archive folder (.mailarchive.lock) was changed during the backup: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished. Details: the lock file was replaced during the run.

### G3 — MEDIUM (III; must for the notes, should for the sentence): a USB swap usually ends on "unavailable", which the notes do not mention and which does not mention a swap

**Seen:** C1, E1, W1 (every swap with a gap), C5, D4, E4, W2. Since F5's fix the run never makes
the vanished folder again, so a write that lands in the gap between unplugging one drive and
plugging the other ends at once on the existing run-fatal sentence. The release notes say only
`It says "Backup stopped: the archive folder changed during the backup."`; a person who swapped
drives and got the email searches the notes for words they did not get. The sentence itself says
"reconnect the drive", while a drive is connected (the wrong one), and its system text grew with F5:
`(create output dir …/drive/Archive/A/Inbox: stat …/drive/Archive: no such file or directory)`.
Not a regression in what happens: 1.7.1 wrote on into whatever was at the path.

**Current** (`internal/app/localrun.go`, `unavailable`):
`the archive location <folder> is unavailable (<system's words>) — reconnect the drive or share and re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.`

**Proposed, must:** the release notes name both endings (text at the end).

**Proposed, should** (keeps "is unavailable" and the kept-messages note; the system's words move
to "Details:", as F1 did for the lock):
> the archive location `<folder>` is unavailable — connect the drive or network share that holds this archive (if you swapped drives, put this archive's drive back), then run the backup again. Messages written before this point are kept; nothing was recorded as archived that was not fully written. Details: `<system's words>`.

### G4 — MEDIUM (III, must): "A brief hiccup is not a failure" is not true while a backup writes

**Seen:** C7. A 5-second outage of the archive's folder while the backup wrote new mail failed it
at once (`the archive location … is unavailable (lstat …: permission denied) — reconnect the drive
or share and re-run`), and Status then said "did not finish". The write that meets the outage asks
the older check (the run's probe after a write fault, FC12), which does not wait. 1.7.1 does the
same, so this is not a regression; the bullet's heading says otherwise. Its second sentence is
also not quite right: "A backup that only reads mail it already has does not check the folder" —
it checks before its end-of-run writes, and waits there (C6: a 0.74-second run took 6 seconds and
ended OK; C6c: 12.5 seconds, OK). The new check is what waits out a hiccup.

**Current:** `**A brief hiccup is not a failure.** A backup that only reads mail it already has does
not check the folder. When it does check and the folder can't be read (a network folder
reconnecting, for example), it keeps trying for about 30 seconds, writing nothing, before it stops.`

**Proposed:**
> **The new check waits out a brief hiccup.** If it cannot read the archive folder for a moment (a network folder reconnecting, for example), it tries again for about 30 seconds, writing nothing, before it stops the backup.

### G5 — LOW (III, optional): "never finished" and "did not finish" on one screen

**Seen:** `status` and Status & health after every stopped swap (C1', D1, D4, E1, E4, E5, W1, W2):
`Last run: 2026-11-14 12:21 EST → never finished`, and two lines below it the F3 RED "did not finish
and could not record how it ended". The release notes quote "did not finish".

**Current** (`internal/health/health.go`, the `Last run:` summary, two places): `never finished`.
**Proposed:** `did not finish`.

### G6 — LOW (III, optional): the F4 warning repeats the error's first sentence just above it

**Seen:** C2's run log, D1's live log (directly under the note that says the same), E5's run log;
by code, a plain terminal run (no `-log`) prints the warning, then `FAILED: …`, then `mailarchive:
…`: three paragraphs of the same news on a phone. It still marks which branch ran in a log file.

**Current:** `warning: the folder at <folder> changed during this backup (another drive or folder is
there now, or another backup took it over), so this backup's result was not written into it.`
**Proposed** (shorter, it no longer restates the cause):
> warning: this backup's result was not written into `<folder>`: the folder there is no longer this archive's.

### G7 — LOW (III, code-read, optional): the encrypted sentence tells a person whose lock file was deleted to connect the drive

An encrypted archive's record, Recent backups row and email use the kind's fixed sentence. It is
right for a swap; for its own lock file deleted (recorded in its own archive) "connect this
archive's drive" sends the person looking for a drive problem they do not have.

**Current** (`internal/state/encrypted_record.go`, `FailLockLost`): `the backup stopped because the
archive folder changed during the backup (its drive was swapped or disconnected, the folder was
moved, or another program changed MailArchive's lock file there) — connect this archive's drive and
run the backup again`
**Proposed:**
> the backup stopped because the archive folder changed during the backup (its drive was swapped or disconnected, or the folder was moved), or because another program changed or deleted MailArchive's lock file there — if you swapped drives, put this archive's drive back, then run the backup again

### G8 — LOW (III with a small II, code-read, optional): the Windows start-failure words are printed where nobody sees them

`instanceStartError` returns a plain error, not a refusal (`refuse`), so its words go to stderr
only (`cmd/mailarchive/dispatch.go`, `noarg.go`, `cmd/mailarchive-desktop/main.go`). MailArchive
Desktop from the Start menu, the desktop shortcut and at sign-in is `mailarchivew.exe`, built
`-H=windowsgui`: it has no console, so the person sees nothing and the Desktop simply does not
open. The words also give no action, and the second cause the code names (a process below Medium
integrity, for example one started from a sandbox) is not a network drive.

**Current:** `MailArchive Desktop could not start: Windows would not protect its private start file
in <folder> (<Windows error>). That folder must be on this computer's own drive.`
**Proposed:** show it as the other start refusals are shown (`refuse(env, …)`: a dialog and
stderr), in their voice:
> MailArchive Desktop cannot start: Windows would not protect its private start file in `<folder>` (`<Windows error>`). Do this: start MailArchive Desktop from the Start menu. If this message comes back, that folder is on a drive that cannot protect it, such as a network drive; it must be on this computer's own drive.

### G9 — LOW (III, in the notes): "quit MailArchive Desktop" is not the page's word

The Windows bullet says "After updating, quit MailArchive Desktop (or sign out and back in)". The
page's button, and the notes' own Windows section, say **Shut down**. Folded into the proposed
notes.

### G10 — INFO (pre-existing, not in this diff): Status & health with the drive out tells the person to reinstall a schedule

**Seen:** D4, while A's folder was away (the state a drive-swapper is in between swaps): `RED: the
scheduled program  no longer exists — the job cannot run; re-run \`mailarchive schedule -out … -install\``
(an empty program name, two spaces) and `WARN: the schedule "" is recorded but not present in this
host's scheduler — …`, beside the right `WARN: the archive location … is not reachable — if it is on
a removable or network drive, connect it and check again; nothing is wrong with the archive itself`.
The archive's schedule record lives in the folder that is not there. `health.go` changed only the F3
words in this patch, so this predates 1.7.2. Follow-up: with the archive unreachable, say only that.

### G11 — INFO (pre-existing): `verify` lists every unexpected file

**Seen:** E5: 288 lines of `unexpected: A/Inbox/…html` before the F8 note, on a phone. The note
itself is right. Its "a leftover from an old-binary excursion" is old jargon. Follow-up.

## Where each new sentence appears

| Sentence | Terminal | Run log | Run now note | Recent backups | Status & health, `status` | `status -json` | Email | Run history, BACKUP-NEEDS-ATTENTION.txt |
|---|---|---|---|---|---|---|---|---|
| "Backup stopped: the archive folder changed …" (folder no longer the run's) | yes, once with `-log`, twice (plus G6) without | yes | yes, after "Backup failed: " | no: nothing is recorded in either folder | no: the right drive shows the F3 line, the other drive its own last backup | no | yes (E5) | no |
| "… lock file … was deleted …" (own archive) | yes, twice | yes | yes | yes, in full | yes, in `Last run:` and the RED | `last_run.error`, code `run_failed` | yes (E2) | yes |
| "… lock file … was changed …" (own archive) | yes, twice | yes | yes | yes | yes | yes | yes (E3) | yes |
| the warning (F4) | yes without `-log` | yes | in the live log under the note | no | no | no | no | no |
| "did not finish and could not record how it ended …" (F3) | — | — | — | — (no row) | yes, after `Last run: … never finished` (G5) | in `reasons`; code `run_never_finished` unchanged | no | no |
| "the archive location … is unavailable …" (now reached by a swap with a gap, F5) | yes, twice | yes | yes | no | no (F3 line once back) | no | yes (E1, E4) | no |
| encrypted kind sentence (F7) | code-read: the record's surfaces only | | | | | | | |
| `verify`'s note (F8) | yes (E5) | | | | | | | |
| Windows start failure (F11) | stderr only; nothing from the Start menu (G8) | | | | | | | |

The email subject (`MailArchive backup FAILED: <folder>`) and the desktop notice after a scheduled
failure (`MailArchive: the scheduled backup of <folder> failed. Open MailArchive Desktop to see
why.`) carry no reason, so neither got longer. After a swap the Desktop the notice points to can
only show the F3 line, which now names the swap.

## Release notes: proposed text

Replaces lines 80 to 85 of `packaging/release-notes.md`. 336 words with the heading, about 320
without, where the current text has 367 with it. It keeps every true fact: what went wrong, who is
affected, what the person sees, what to do, the hiccup, the Windows fix and what to do after
updating, and that nothing else changed. It names both endings (G3), says what waits out a hiccup
(G4) and uses the page's **Shut down** (G9). Going lower would drop a fact (the platforms, or the
"possibly hundreds" stray files). If G1 is not folded, quote the capitalised lead instead: `… and
fails, saying "Backup stopped: the archive folder changed during the backup" or that the archive
location is unavailable; …`.

```markdown
### Fixed in 1.7.2 — a swapped drive stops a backup; a sandboxed program can't read MailArchive Desktop's key
- **If you swap backup drives, a backup no longer writes its records into the other drive's archive.** Before, a drive changed during a backup (another drive at the same letter or folder, or the folder moved or renamed) could leave the backup's records in the other drive's archive, ending "OK". Now a backup checks its folder before every save, page and change to a file already there, and at least every 10 seconds while it writes. If the folder has changed or is gone, the backup stops, writes nothing more there and fails, saying the archive folder changed or the archive location is unavailable; a scheduled backup emails as usual. This covers Mac, Windows, Linux, encrypted archives and Microsoft 365 backups.
- **What to do:** put this archive's drive back and run the backup again. Until then, Status & health may say the last backup "did not finish". The other archive keeps its records; only message files written in the seconds before the backup noticed (up to 10 seconds' worth, possibly hundreds) stay in its folder, unlisted.
- **The new check waits out a brief hiccup:** if the folder can't be read for a moment (a network folder reconnecting, say), it retries for about 30 seconds, writing nothing, before it stops the backup.
- **Windows: a sandboxed program can no longer read MailArchive Desktop's key file.** A program in a restricted sandbox under your account (as a browser runs web pages) could read the file holding MailArchive Desktop's private key and use it to open a Desktop page. Now only programs running normally under your account can read it; shortcuts work as before. After updating, click **Shut down** in MailArchive Desktop and open it again (or sign out and back in) to replace the copy already running. Mac and Linux are unaffected.
- **Nothing else changed.** Your archives, settings and scheduled backups stay as they are.
```

Each claim, against the code and the walk: the checks (`archiveguard.go`: saves, pages, README,
`BeforeReplace`, `BeforeMessageWrite` at 10 s, the sweep); "changed or is gone … fails"
(C1 to C5, D1 to D4, E1 to E5, W1, W2: exit 1 every time, the email in E1 to E5); "did not finish"
(every stopped swap); "up to 10 seconds' worth, possibly hundreds" (E5: 541 and 288 files in
under a second; 0 in the Mac and Windows swaps); "about 30 seconds, writing nothing"
(`archiveUnreadableFor`, C6c); the Windows bullet (MA-1427, the adversarial review's P2 and P4; not
walked here); Microsoft 365 (`RunGraph` shares the guard; not walked).

## Not walked

- **Microsoft 365 capture** (`RunGraph`): needs a tenant. Same guard and same words.
- **Encrypted archives** (F7, G7): making one needs a typed password; the sentence was code-read.
- **The Windows sandbox fix and its start failure** (MA-1427, F11, G8): needs a signed-in session,
  a Low-integrity process, or a profile folder without labels; code-read.
- **A Windows swap with no gap**: a drive letter cannot be moved without one, so W1 met the gap.
- **The macOS desktop notice**: never raised on this Mac by rule; its words read from
  `internal/wizard/wizard.go` (`failureNotice`).
- **Edge and the Windows Desktop**: nobody is signed in to the VM.
- **A Linux exchange with no gap** with `mv --exchange`: refused by the container's file system;
  E5's link switch stands in for it.

## Lab notes

Everything is under the worktree's `.claude/testdata/fric2/` (untracked, not committed):
`mac/setup.sh`, `mac/walk-mac.sh` (C1 to C7), `mac/time-noop.sh` (the timing and C6c),
`mac/desk-prep.sh`, `mac/desk-run.sh`, `mac/desk-drive.sh` (D1 to D4), results in `mac/res/`;
`linux/walk-linux.sh` (E1 to E4) and `linux/walk-linux2.sh` (E5), results and the kept emails in
`linux/res/` and `linux/res2/`. The first Linux pass ran its changes 6 s in and every run had
already finished (the container writes about 750 messages a second); its results were replaced by
the second pass at 1 s. The Windows script and the program are in
`/Users/alex.dimarco/repositories/.winlab/fric2/` (`walk-win.ps1`, `dump-res.ps1`); the results
stay in the VM at `C:\fric2lab\res`. The `V:` mapping was removed by the script's `finally`. The
Mac Desktop was shut down from its own API; no browser, dialog or notice opened.

## Final round (2026-11-14, the patch builder; added beside the re-walk, nothing above changed)

The lead's calls, one final round on `patch-1.7.2`:

| Finding | Outcome |
|---|---|
| G1 | Adopted: the three stop sentences no longer open with "Backup stopped: "; each is a clause the surfaces lead into (MA-1447's amendment). |
| G2 | Adopted: "Details: the lock file was replaced during the run." (or "removed"), no path (`lockfile.LostError.Short`). |
| G3 | Adopted both parts: the notes name both endings; the sentence is the proposed one, the system's words after "Details:" (MA-1457, amends MA-320). |
| G4 | Adopted as proposed, plus one sentence the adversarial re-check's Q1 asks for: "A dropout that meets a backup while it writes new mail, between checks, still stops it at once, as before." |
| G5 | Adopted: the `Last run:` line says "→ did not finish"; the run's own "could not record this run" warning says the same (MA-1448's amendment). The `Last verify:` line, which pairs with its own "never finished" WARN, is unchanged. |
| G6 | Adopted: "warning: this backup's result was not written into FOLDER: the folder there is no longer this archive's." |
| G7 | Adopted (MA-1449's amendment). |
| G8 | Adopted as a refusal (dialog and stderr) in the proposed voice; since the research note's L0, these words are shown only when no usable start file could be written: a label Windows will not set now falls back to the file 1.7.1 wrote (MA-1453). |
| G9 | Adopted in the notes: "click **Shut down** in MailArchive Desktop and open it again". |
| G10 | Pre-existing, not in this patch. Follow-up: with the archive unreachable, Status & health should say only that, not "re-install the schedule". |
| G11 | Pre-existing, not in this patch. Follow-up: `verify` could group unexpected files by folder, and the note's "old-binary excursion" wants plain words. |

The notes' Windows bullet also says, since L0's safety net, that the key file is protected "where Windows allows (otherwise the file is kept as before)". The section is about 350 words, over the target, to keep Q1's and L0's sentences true.
