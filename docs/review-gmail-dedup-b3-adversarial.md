# Adversarial pass: Gmail de-dup slice B3 and its friction-fix round

**Verdict: NOT CLEAN (1 HIGH, 6 LOW).** The Desktop route's guards, the answer file's own
handling (no path from the request, no link followed, atomic write, owner-only), the terminal's
"scheduled runs never ask", the escaping of every new folder name on a page, and "nothing about an
encrypted archive's Gmail account is written in plain text" all HELD. One finding breaks the rule
this slice exists to keep, that an archive is switched to the 1.8-only format only after someone
said yes for it:

- **A1 (HIGH).** The yes kept on this computer is tied to the folder's path and the account's
  folder name ("imap.gmail.com"), never to the archive itself, and it is never cleared after the
  backup that used it. Deleting the archive in MailArchive Desktop also leaves it behind. So a
  different archive that later sits in the same folder is switched at its next backup, a scheduled
  one included, and nobody said yes for it. The switch cannot be undone. Proved end to end on
  macOS and on the Windows VM.

The six LOW findings: a member folder name that Windows spells another way (A2), the same folder
spelled another way losing its answer (A3), the end of the input remembered as a Not now and Ctrl-C
not stopping the question (A4), control characters in a status line (A5), the Desktop route's
weaker guard against uninstall and Save settings (A6), and the wizard's schedule offer dropping a
kept answer (A7).

No product code was changed by this pass. The probe tests that prove the findings are kept out of
the branch (build tag `b3adv`, uncommitted), because they turn the suite red.

## Header

- **Reviewed commit:** `25c5bf0` on `gmail-dedup` (B3 = `ce06fce`; friction fixes `1be17a4`,
  `7bb200b`, `a5c27b1`), diff `88fddf7..25c5bf0`. Design `docs/design-gmail-dedup.md` §18 to §27
  and the §13 "[B3 friction fixes]" note; catalog rows MA-1222, MA-1224, MA-1236, MA-1243,
  MA-1292 to MA-1301; `docs/review-gmail-dedup-b3-friction.md`.
- **Blob ids at `25c5bf0`:** `internal/gmailanswer/gmailanswer.go` 8684641,
  `internal/desktop/gmailquestion.go` f4bfa80, `cmd/mailarchive/gmail.go` 01b3542,
  `internal/app/gmailswitch.go` eb44463, `internal/pages/pages.go` 5e7b9b0,
  `internal/archdelete/configfiles.go` be139c8, `internal/state/dedup.go` e34d8c5,
  `internal/wizard/jobrun.go` 46937e3. A finding is stale when its cited file changes.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who did not write
  the slice, worked the outside, insider and integrity lenses as finder, then as skeptic. Every
  CONFIRMED row was reproduced by a test against the real code; every HELD row cites the code that
  refutes it.
- **Worktree:** `.claude/worktrees/gmail-dedup-b3-adv`, branch `wip/gmail-dedup-b3-adv` at
  `25c5bf0`. Probe files (build tag `b3adv`, not committed): `internal/app/zz_b3adv_answer_identity_test.go`,
  `internal/archdelete/zz_b3adv_gmail_answer_test.go`, `internal/gmailanswer/zz_b3adv_answer_test.go`,
  `internal/pages/zz_b3adv_member_folder_test.go`, `cmd/mailarchive/zz_b3adv_gmail_test.go`
  (tests `TestB3Adv*`).
- **Real primitives only.** Real temporary archives made by real backup runs of a real
  Thunderbird-shaped Gmail profile (the package's own fixtures), the real answer file, the real
  delete step 3, the real page builder and index. Settings folders are throw-away (`HOME`,
  `XDG_CONFIG_HOME`, `APPDATA` set per test). No real email, no browser, no Desktop link, nothing
  outside `repositories/` touched.
- **Windows:** the Parallels VM "Windows 11" through this pass's own copy of `winrun-at.sh`
  (`.claude/winrun-b3adv.sh` in the worktree: its own `src-b3adv` and `wintest-b3adv` folders and
  program names, so the other builder's files were not touched; only `-test.run B3Adv` runs).
- **Commands and results** (Go 1.26.0 darwin/arm64 and windows/amd64 on the VM, 2026-11-13):
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/app/ ./internal/archdelete/ ./internal/gmailanswer/ ./cmd/mailarchive/`
    (macOS): every probe marked CONFIRMED below is red; `TestB3AdvInterruptDoesNotEndTheWait`
    passes and logs that only the time limit ended the wait.
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/pages/` (macOS): both pass (macOS keeps
    "INBOX." and ".. " as literal folders).
  - `zsh .claude/winrun-b3adv.sh … internal/pages internal/gmailanswer internal/archdelete` and
    `… internal/app cmd/mailarchive` (Windows): A1, A2, A3, A4, A5 red as on macOS, and
    `TestB3AdvTrailingDotMemberFolderAliasesAnotherPage` red on Windows only.
- **Count:** 27 attacks; 7 confirmed (1 HIGH, 6 LOW); 20 held or refuted.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| A1 | integrity / insider (time) | `internal/gmailanswer/gmailanswer.go:53-62` (the answer's name is the folder's path only), `:91-106` (read with no check of which archive is there); `internal/wizard/jobrun.go:386-393` and `cmd/mailarchive/gmail.go:58-60` (every run carries the kept yes; nothing clears it); `internal/archdelete/configfiles.go:79` (step 3 removes the alert files, not `.gmail-answer`) | CONFIRMED, HIGH (macOS, Windows) | `TestB3AdvAYesKeptForOneArchiveSwitchesAnother`: archive B at version 7, `imap.gmail.com` switched on, by A's yes. `TestB3AdvDeleteStep3KeepsTheGmailAnswer`, `TestB3AdvAnswerHasNoArchiveIdentity` | (owed) | (owed) |
| A2 | outside (crafted data), Windows | `internal/state/dedup.go:319-329` and `internal/pages/pages.go:266` accept a segment ending in a dot or a space | CONFIRMED, LOW (Windows only) | `TestB3AdvTrailingDotMemberFolderAliasesAnotherPage`: INBOX's own page lost its own email. `TestB3AdvDotDotSpaceMemberFolder`: ".. " made the page build fail | (owed) | (owed) |
| A3 | integrity | `internal/gmailanswer/gmailanswer.go:61` uses `schedule.DefaultNameFor`, which neither case-folds nor resolves links | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvAnswerKeyFollowsSpellingNotFolder`: same folder, the other spelling reads no Not now | (owed) | (owed) |
| A4 | integrity (terminal) | `cmd/mailarchive/gmail.go:111` ignores the read error, so the end of the input is an answered Not now and `:69-73` keeps it; `:107-129` takes no context, and `main.go:425` has already caught Ctrl-C | CONFIRMED, LOW (macOS, Windows); the Windows Ctrl-C path PLAUSIBLE | `TestB3AdvEndOfInputIsARememberedNotNow`; `TestB3AdvInterruptDoesNotEndTheWait` | (owed) | (owed) |
| A5 | outside (crafted data) | `cmd/mailarchive/gmail.go:90-93` and `main.go:2431-2437` print a store name passed by `state.SafeToken` alone (`internal/state/manifest.go:496-512`), which lets C1 controls and bidi overrides through | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvStatusWaitingLinePrintsControls`: U+009B and U+202E printed raw | (owed) | (owed) |
| A6 | integrity / concurrency | `internal/desktop/gmailquestion.go:207` takes `hold` (`internal/desktop/deleting.go:44`), not `holdSettings` (`:57`); no `unfinishedActionRefusal`; `gmailanswer.Keep` (`gmailanswer.go:111-125`) rewrites the job file with no lock shared with Save settings (`internal/desktop/localschedule.go:158`, `:238`, `:284`) | PLAUSIBLE, LOW | Mechanism in source (a race; not reproduced) | (owed) | (owed) |
| A7 | integrity | `internal/wizard/wizard.go:2298-2315` builds the job file afresh (no Gmail fields) for the schedule offer and Add another archive (`:2250-2258`); `gmailanswer.go:117-121` had removed the answer file when it moved the answer into the job file | PLAUSIBLE, LOW | Mechanism in source | (owed) | (owed) |

### A1 (HIGH): a yes given for one archive switches another archive in the same folder

**What happens.** The person clicks Yes on archive A's card. The yes is kept as a list of
account-folder names ("imap.gmail.com") under a file named after A's folder path, or in that
folder's job file. Nothing records that it was A. Nothing removes it once a backup has used it
(the design's §13 note says it "waits in this computer's settings until the next backup"; it
actually stays for good). Delete in MailArchive Desktop removes the job file but not
`<key>.gmail-answer`.

Any archive that is later in that folder is switched at its next backup if one of its Gmail
account folders has the same name. A person's first Gmail account folder in Thunderbird is
"imap.gmail.com", so the names usually match. Realistic ways this happens:

- two backup drives used in turn under the same drive letter (`E:\MailArchive` on each);
- an older copy of an archive restored into the same folder;
- an archive deleted, and another existing archive added at the same path.

The probe follows the person's real steps. A real existing archive A is backed up by a scheduled
run, and the question waits. The yes is kept with `gmailanswer.Keep`, as the card does. A is
removed, and a different existing archive B, from another profile, is moved into A's folder. Then
B's scheduled run is given the answers this computer keeps for the folder, as `optsForJob` gives
them. Result on macOS and Windows: B is version 7, with `imap.gmail.com` switched on. Every computer
that backs B up now needs 1.8, and the switch cannot be undone.

A Not now has the same flaw the other way. It silences the card for whatever archive is in the
folder later. That is lower harm: Settings still asks.

**Fix, in plain words.**

1. Keep, with the yes, which archive it was given for: an id the archive itself holds. Apply the
   yes only to that archive.
2. Clear the yes once the backup that used it has switched those stores on.
3. Have Delete and Remove from list remove `<key>.gmail-answer`, as they remove the alert files.

### A2 (LOW): a member folder Windows spells another way

A Gmail membership's folder "INBOX." passes the manifest check (`validMemberFolder`) and the page
check (`cleanSegments`). Windows drops the trailing dot, so the member-only page for "INBOX." is
written over INBOX's own page. On the VM, INBOX's page lost its own email. A segment ".. " (dot,
dot, space) does not escape the archive on Windows (refuted), but it makes `pages.Generate` fail. The run only logs
"warning: folder pages", so the later pages are not refreshed.

Today a membership can come only from a hand-edited manifest, because no B3 code writes one. The
D1 and M1 slices will write them from Thunderbird's folders. The archive's mail is not touched.

**Fix:** accept a member folder only when each segment is what `util.SanitizeSegment` would have
written. That is the exporter's own rule for trailing dots, spaces and reserved names. Apply it in
both checks.

### A3 (LOW): the same folder spelled another way has no answer

The answer's file name comes from the folder path exactly as typed. It does not ignore letter case
the way Windows and macOS do, and it does not resolve links. On both platforms, a Not now kept for
`…\Archive` is not found for `…\ARCHIVE`, the same folder. So a person who said Not now in the
Desktop is asked again by `mailarchive export -out e:\mail`, and a Desktop yes is not applied by a
terminal run typed with another spelling (it asks instead). The yes never goes to the wrong
archive this way. A fix for A1 that names the archive also fixes this.

### A4 (LOW): the end of the input is remembered as Not now, and Ctrl-C does not stop the question

`askGmailAnswer` throws away the read error. An empty read at the end of the input (Ctrl-D, or on
Windows a Ctrl-C that ends a console read) is taken as an answered Not now and kept for good. The
terminal then says "MailArchive will not ask again on this computer". The person who pressed
Ctrl-C on Unix gets no response: the run has already caught the interrupt, and the question takes
no context. The question waits until the person presses Enter, which is again a kept Not now, or
until 2 minutes pass. The probes prove the end-of-input part on both platforms, and that only a
line or the time limit ends the wait. The Windows console's Ctrl-C behaviour is PLAUSIBLE: an
interactive console cannot be driven through `prlctl`.

**Fix:**

- Count only a typed line as an answer. The end of the input or a read error is no answer, so it
  is not remembered.
- Let the run's interrupt end the wait, as no answer.

### A5 (LOW): control characters in the waiting line

`status`'s "Gmail: not switched on for STORE …" and the unattended run's summary line both print
the store name after `state.SafeToken`. That function rejects only C0 controls and DEL. U+009B (a
one-character terminal escape) and U+202E (right-to-left override) reach the terminal and the log.
The name comes from the archive's own last-run record or its manifest's store list, so a person who
can write the archive folder can set it. Everywhere else the command line strips both: `scrubTTY`,
and `util.StripControl` with `util.StripBidi`.

**Fix:** pass the name through the same scrub before printing it.

### A6 (LOW): the Desktop answer route's guards

The route takes the delete hold, not the settings hold that Remove from list takes for the same
reason (CON-06). An uninstall that starts while the route runs does not wait for it. So the answer
file, or a whole job file (`Keep` reads the job file, then writes it back), can be written after
uninstall's cleanup and left behind.

The route also does not make the unfinished-delete check that Pause and Resume make. It keeps an
answer for a half-deleted folder, and A1 then keeps that answer for good.

`Keep`'s read-modify-write of the job file shares no lock with Save settings, which reads the job
file at `localschedule.go:158` and writes it at `:238` and `:284`. A Yes or Not now that races Save
settings can be lost: the card asks again. This race never invents a yes.

A crash between the temp file and the rename leaves a `.gmail-answer-*.tmp` file. Uninstall does
not recognise it, so it stays in the settings folder.

**Fix:** take `holdSettings` and the unfinished-delete check, as Remove from list and Pause do.
Write the job file under the same lock Save settings uses.

### A7 (LOW): the wizard's schedule offer forgets a kept answer

When an archive has a job file, a Desktop answer is moved into it, and the answer file is removed.
The wizard's "Yes, every day" offer, which Add another archive also uses, writes a new job file
from the wizard's answers. Those answers carry no Gmail fields. A Not now is then forgotten (the
card asks again, though it said it would not), or a yes no longer waits (the card asks again).
Resume does not do this: its repair passes no job writer. This path never invents a yes.

**Fix:** carry `GmailNotNow` and `GmailYes` over when a job file is rewritten. Or always keep the
answer in the answer file only, and have the job file never hold it.

## Held and refuted attempts

| ID | Attempt | Verdict | Evidence |
|---|---|---|---|
| H1 | A cross-site page or another local page posts an answer (CSRF) | HELD | `gmailquestion.go:193` `server.GuardLocalPOST`: loopback Host, a loopback or empty Origin, and the per-process token in the `X-CSRF-Token` header compared in constant time (`internal/server/setup.go:174-183`). A JSON body with a custom header needs a preflight that the server never grants. The router's private-link cookie applies as for Pause |
| H2 | Answer for an archive other than the one shown, or one not in Your archives | HELD (by authority) | The route accepts any folder that holds a plaintext archive (`archindex.Kind` reads that folder's manifest), but only with the token, which is the person's own authority. The stores are read again from that folder's last-run record (`gmailquestion.go:212-217`), never taken from the request |
| H3 | Answer for an encrypted archive (the "key" binding) | HELD | `gmailStoresAt` (`gmailquestion.go:77-96`) returns nothing for a non-plaintext kind, for `encmark.MustMinimise`, and for an encrypted last-run record. The route then refuses with "no Gmail question". `Path` takes no key from the request |
| H4 | Traversal through the answer file's name | HELD | `gmailanswer.go:61`: "mailarchive-" plus a fixed-length digest plus the suffix, in `job.Dir()`. No request text reaches the name |
| H5 | The answer file is a link, a pipe or a device | HELD | `util.ReadSecureFile` (`internal/util/securefile.go:128-169`): Lstat, then an open that does not follow links and does not block, judged on the open descriptor. The write's rename replaces a link and never follows it. MA-1292's test covers the link |
| H6 | A torn answer file after a crash | HELD | `WriteFileAtomic0600Temp`: temp file, fsync, chmod 0600, rename, then fsync of the folder. A damaged file reads as no answer, which is never a yes |
| H7 | Windows permissions on the answer file | HELD (posture as before) | It inherits the per-user `%APPDATA%\mailarchive` permissions, like every job file; `ReadSecureFile` skips the mode check on Windows for every settings file alike |
| H8 | Another local account plants a yes | HELD (out of scope) | The settings folder is owner-only (0700 on Unix; the user's profile on Windows). Whoever can write there already controls the job files, including what a scheduled run reads |
| H9 | A planted `gmail_off` in the last-run record widens the yes | HELD | The yes covers the tokens listed, but a token switches only a store that the run itself detects as Gmail (`run.go:1813`, `:1821` call `gmail.store` only on `thunderbird.GmailYes`). The question is asked about the whole archive |
| H10 | A scheduled or non-terminal run asks, or waits on stdin | HELD | Every schedule's arguments carry `-unattended`; `gmailAtTerminal` needs stdin, stdout and stderr to be terminals and the process in the foreground; the run-job path never sets `AskGmail` (MA-1236's job-options test) |
| H11 | Stdin tricks (a pasted answer, "yes" hidden in a longer line) | HELD | Only the first line is read, and only an exact "y" or "yes" (any case, after trimming) is Yes. Anything else is Not now, never a yes |
| H12 | The 2-minute wait lets another process take the lock | HELD | The lock is an operating-system file lock with no time-out (`internal/lockfile`), so nothing can take it while it is held. A scheduled run is kept out for at most 2 minutes and records that as its own failure (accepted in the F7 note) |
| H13 | A remembered Not now blocks `-gmail-folders` | HELD | `gmailswitch.go:95`: the flag's yes is checked before the Not now |
| H14 | HTML or attribute injection through a folder name | HELD | "This folder: NAME" goes through `html.EscapeString` (`internal/export/html.go:2191-2195`); "(saved in …)" through `html/template`; the reader's `inFolders` and `esc` write text nodes, and the folder filter uses `textContent` |
| H15 | A member row's link leaves the store's folder | HELD | `memberRows` (`pages.go:226-262`) needs the email's path inside the same store, clean segments and ".html". The ".. " page build on Windows fails, it does not escape (A2) |
| H16 | A crafted version 8 hides a damaged manifest from `status` | HELD | `parseManifest` decodes the whole file first, so a damaged file is still RED "cannot be read". Only a well-formed manifest at version 8 (or one with `merge_pending`) gets the WARN, and a refused scheduled backup keeps its own failure posture (F9 note). Version 9 is RED. Note: the WARN's "Nothing is lost" rests on the stamp alone |
| H17 | Leaking a plaintext folder name or answer for an encrypted archive | HELD | `recordRun` writes `gmail_off/held/on` only when the run is not encrypted (`run.go:1471-1480`); `Gmail.Counts` only when `Key == nil`; the "you said yes" log line and the waiting lines only when not encrypted; the terminal never keeps a Not now when `opts.Key` is set (`keyring.Apply`, `main.go:397`, runs before `setGmailQuestion`, `:423`); the question and its answer go to stderr, never to the `-log` file; Run now's switched line is in the plaintext summary only |
| H18 | Replay of an answer | HELD | The answer is idempotent, and the token belongs to the process |
| H19 | An answer given while a backup of that archive runs | HELD | The run read its options when it started. Its last-run record keeps the question waiting, the card shows "yes waits", and the next backup applies the yes |
| H20 | `jobFor` writes the yes into another archive's job file (it never checks `j.Out`) | HELD (not reachable) | Only through a collision of the 8-hex-digit path digest between two of one person's archive folders. Cheap hardening: check `j.Out` |

Conceded by design, noted only: an existing archive with no records at all counts as new and is
switched by any run, a scheduled one included (B2's `GmailDecision`, design §7.4). That covers an
archive a 1.7 computer made from an empty Gmail account.
