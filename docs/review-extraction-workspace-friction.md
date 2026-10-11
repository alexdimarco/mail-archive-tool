# Friction review — the extraction workspace, first shippable slice (EW-E3)

> As of 2026-10-11. First walk of `wip/ew-e3-fric` at `51fc3fc` (change set `21d917f..51fc3fc`,
> commits `6a11b72`..`376dd61`, `5adbbf2`, merge `db0ca08`; catalog rows MA-400..MA-449 and
> MA-505..MA-532 as far as E3 built them; rulings §9 row 24, §13.14) on macOS (Darwin 25.6.0),
> Go 1.26.9; the findings were then fixed test-first in `11dc17d`..`36f42fe` and the cells a fix
> touched were **re-walked at `36f42fe`** (the head before this report's own commit). Method: the
> assurance-kit friction review (`friction-review.md`): each cell an actor x scenario walked by
> driving the real handlers and reading what comes back, classified Type I (the ceremony is the
> control), Type II (design choice, reducible without weakening the control), Type III (missing
> legibility or tooling). The Export pages were walked through the dashboard's own router
> (`newDashboard(cfg).router()`, the same handlers `mailarchive-desktop` serves) against the
> repository's test-style fake Microsoft 365 (`ws_testutil_test.go`: device-code, token and `/me`
> endpoints, a folder tree, paged listings, each message's `/$value`), with the HTML rendered to
> text and each history card rendered the way `exportsJS` builds it (the re-walk's transcription
> was brought up to the new `cardEl`). Every run used scratch homes, state folders, destinations
> and an archive folder under the session scratchpad; no real mail, no tenant, no network, no
> credential store. The walk drivers (`fric_*_test.go`) and both sets of transcripts (`ewfric/out`
> at `51fc3fc`, `ewfric/out2` at the head) live in the scratchpad, outside the repository.
> Commands: `go test ./internal/desktop -run 'TestFric...'` through `inhome` (one driver per cell
> group, every one green at the head), `mailarchive` and `mailarchive-desktop` built at the head
> into `ewfric/bin/`; the desktop binary was run with `-h` and started with `-no-browser` and a
> scratch home: it printed its banner and exited 0 on SIGTERM, but its pages sit behind a one-time
> link (a browser or a dialog), so the pages were walked in-process. Baseline: not touched.
> `docs/scenario-catalog.md` and `assureblock/` are unchanged by every commit of the fixes.
>
> **The page scripts could not be run, before or now.** There is no browser, JavaScript engine or
> screen reader here. Every statement about what a page does after a click that rests on
> `workspace.js`, `exports.js` or `signin.js` was read from the script source the server serves
> and is marked *(script)*. Fixing F2 turned up what that can hide: **F27**, below, a defect that
> would have stopped the second script of two pages from running at all, which no handler walk
> could show. The fixes' tests read the scripts for what they must contain; they do not run them.

**Cells walked:** 25 (21 owner, 4 helper) &nbsp;·&nbsp; **Functioning as walked at `51fc3fc`:** 10 yes / 14 partial / 1 no &nbsp;·&nbsp; **Re-walked at the head:** 21 yes / 4 partial / 0 no &nbsp;·&nbsp; **High-friction cells:** 6 (OW-2, OW-5, OW-6, OW-9, OW-10, OW-19) at `51fc3fc`, 0 at the head (OW-19 stays partial: its keyboard and screen-reader walk is owed) &nbsp;·&nbsp; **Findings:** 27 (26 first walk, F27 found while fixing) — Type I: 3 · Type II: 3 (F9 built; F15 an owner question; F23 the owner's ruling, keep) · Type III: 21 (all built or built in part; what was left is listed in the Disposition)

**Verdict: SHIPPABLE** — as built at `36f42fe`, on the evidence of the handlers, the engine's records and the page sources the server serves, and **on one condition that stays open: the click-through of the three page scripts in a real browser, keyboard only and with a screen reader, is owed before a release build.** The first review set its own bar: the verdict becomes SHIPPABLE when the six blockers (F1-F6) are built and their cells re-walked, with F7-F13 in the same pass. All six are built and every one of the six high-friction cells re-walks as a recovery the owner can finish through the page, in named steps (the sign-in recovery is 6 steps, each named by a button on the page, not 8 through an unnamed route); F7-F13 are built; and the Type II finding that could have blocked (F9, the repeated offers of a whole set) is built too. What keeps the condition honest is F27: the walk's blind spot, the scripts, was not harmless, and the fixes for F1, F5, F6, F14, F18, F21 and F27 are verified by reading what the page is made of, not by pressing it.

**Rationale.** What held up in the first walk still holds: the happy path is short and honest (11 steps from a first visit including the Microsoft code, 8 once signed in, and the preview says what, how big, where and who can read it), an interrupted run is recoverable with nothing lost, Check this export names the file that changed, and a helper with no app can verify the folder with the README's own commands. What failed was the road back, and it is rebuilt: a refusal from a card button now stays on its card and in the alert (F1); the sign-in that every "Sign in again" names is a button on Export activity, shown when the saved sign-in is refused, and the workspace no longer calls a refused sign-in "ready" (F2, F3); a finished export whose folder was renamed, moved or deleted gets the Look again, Locate and This folder is gone buttons its refusal tells the owner to click (F4); a retry can be sent to another destination (F5); the folder tree is made of buttons and named controls with live regions and a progressbar (F6). The re-walk then found five things the first fixes did not reach (F19 at the Preview, the "stopped after 7 of 7" count, stale labels on a whole set's earlier parts, the workspace's Sign in after a refusal, a dead tab's frozen card) and they are fixed too. What is not built, and why, is the Disposition: R-s's remedy and the README's first line are pinned by existing tests and the engine's own R-l text by another, and the set-level help for a helper (a note in a sealed earlier part, the seal order in the manifest, "Check the set") is new behaviour of the sealed format, so each waits for the owner.

---

## Cell table

OW = owner, HE = owner's helper (a family member or IT contact with the folder and no app). "Script" = from the served script source. The friction column is what the first walk found at `51fc3fc`; the last column says what was built and what the re-walk at the head showed.

| Cell (actor x scenario) | Functions? (first walk → head) | Friction findings at `51fc3fc` | Type | Fix / backlog item and the re-walk at the head |
|---|---|---|---|---|
| OW-1 · owner x first export of one folder, from a first visit to done (j) | yes → yes | 11 steps signed out (open page, load source, Sign in, enter the Microsoft code, Load folders, open Inbox, Select current folder, type the destination, Preview, Start, Open folder), 8 signed in; the design says 9. Under the "Selected" note the count is the folder's own (7 for an Inbox of 13 with subfolders), the footer says 13. After Start the page navigates with the selection still armed, so the browser's "Leave site?" prompt would appear (script). The preview is clear | III (F14, F18), I (F24) | **F14, F18 built.** The note is the subtree sum (13, checked against the count endpoint); Include subfolders applies to the folders already ticked; the footer says at once when a selection passes 200,000; Start clears the prompt (script). Steps unchanged: 11 / 8. F27 built (without it the workspace page's script did not run) |
| OW-2 · owner x return visit after weeks: the saved sign-in has lapsed (b) | partial → yes | Source says `ready`, Load folders then refuses R-c "Sign in again, then try again" with no way to do so; only route Overview, Clear sign-in, reload, Sign in, code, back. **HIGH** | III (F2, F3) | **F2, F3 built.** A refused saved sign-in is remembered (R-c from a listing, the session or a run): the source answers `needs-sign-in` and the workspace shows Sign in; the refusal itself brings Sign in forward (script) |
| OW-3 · owner x export interrupted after 3 of 7 (app killed, lid closed), app reopened, continue (a) | yes → yes | One button, Finish as partial, no sentence that it is the first of three clicks; confirm text uses manifest/checksums; label "Stopped: ... (finished as partial)". 3 steps, nothing redone | III (F13), I (F24) | **F13 built.** The card says "To continue: click Finish as partial (it keeps everything written so far and lists the rest), then click Export the remaining messages, then Start export."; the README's status line is one sentence. The label words stay (existing tests pin "interrupted" and "finished as partial"). 3 steps, as before |
| OW-4 · owner x killed in the first instant (empty folder) or after the seal but before the marker (a) | yes → yes | Empty folder works (a fingerprint is shown for an empty export); sealed: "Finished writing (Complete) — this folder's marker may still need updating"; "marker" is not an owner word | III (F12, F13) | **F12, F13 built.** "Finished writing (Complete) — click Check and finish to confirm it", with Check this export beside Check and finish and a sentence of what to click. Not changed: an empty export still shows a fingerprint (noted, no owner effect) |
| OW-5 · owner x sign-in revoked mid-run (R-c) and signing in again (b) | partial → yes | "1 failed" for the refused message; retry R-c wiped by reload (F1); 8-step recovery through a route no text names. **HIGH** | III (F1, F2, F8) | **F1, F2, F8 built.** The cut-off message counts as not tried ("2 exported, 0 failed, 5 not attempted"); the stop says "Sign in again (click Sign in on Export activity), then click 'Export the remaining messages' there". Re-walk (b1): click the button, R-c stays on the card and the Sign in card appears, Sign in, code, back, click again, Start: **6 named steps** |
| OW-6 · owner x sign-in changed to bob (R-z) or cleared (R-b), then retry (b) | partial → yes | R-l on a retry says "Load folders again to start a new selection" (wrong remedy); R-b says click Sign in on a page without one; both wiped by reload. **HIGH** | III (F1, F2, F3) | **F1, F2, F3 built.** R-l at retry: both accounts, "Sign in as alice", the button; R-b names the account and the button; both stay on the card; the Sign in card shows for them. The engine's own R-l text stays (pinned by `TestRetryReadsTheRunRecord`): the desktop words it for the page |
| OW-7 · owner x destination refusals before and at Start: typo, relative or `~` path, a file, inside the archive, read-only folder, nearly full (c) | yes → yes | R-e/R-f/R-w/R-g2 are what/why/remedy; the preview passes a read-only or too-small destination; R-g2 can read "80.6 kB free ... needs about 80.6 kB"; "mkdirat"; a file is "doesn't exist"; an empty box reads "The folder  isn't a full path" | III (F17, F19, F8) | **F17, F19, F8 built.** The preview warns with R-g2's and R-w's sentences; R-g2 shows bytes when both round alike (80,631 against 80,632); R-w leads with this computer's advice and says "write-protected"; an empty box says "No destination folder is chosen", ~ and %NAME% are said not to be expanded, a file "is a file, not a folder". The re-walk found F19's words were in a check the Preview does not reach; fixed (`36f42fe`) |
| OW-8 · owner x destination fails mid-run: read-only or vanishes (c) | partial → yes | 4 messages "not exported" with no cause or list; vanished folder: raw "openat", "stopped after 7 of 7" with 0 exported, history card "folder not found" without reason or remedy | III (F7, F8) | **F7, F8 built.** "Which ones?" lists subject, folder and reason; the README names the manifest's failures and the first 10; the stop's why and remedy stay on the card after a restart; "after 0 of 7"; "the folder isn't there any more" |
| OW-9 · owner x the stick is full or dead and the rest should go somewhere else (c) | no → yes | Retry always previews into the same destination; R-g2/R-e say choose another/Browse but the page sends no dest. **HIGH** | III (F5) | **F5 built.** The retry preview shows the Destination card (path, Browse, Create folder) and "Preview again for this destination", which posts the run, the kind and the path (script); the engine path is tested: the next part lands in the other folder, the earlier export byte-unchanged; a missing destination is R-e naming Browse |
| OW-10 · owner x finished export folder renamed/moved/replaced/deleted (d) | partial → yes | The settled card offers no Look again/Locate/This folder is gone though R-r/R-r2 tell the owner to click them; refusals wiped by reload. **HIGH** | III (F4, F1) | **F4, F1 built.** After R-r or R-r2 the card offers the three buttons and keeps the refusal; R-r2 says "nothing is there ... moved, renamed or deleted, or its drive isn't plugged in". Re-walk (d1-d4): rename, delete, replace and an interrupted run all end with the buttons on the card; Locate gives "Complete (recovered)" |
| OW-11 · owner x cancel > remaining (-part2) > failed again (-part3) (e) | partial → yes | Part 1 and 2 still offer retries that duplicate -part4; older cards keep stale labels and repeat the set block | II (F9), III (F10) | **F9 built, F10 in part.** Offers follow what the whole set holds (cached per set); every card and the receipt say "All 7 messages are exported, across the 3 parts of this set."; an earlier part says "the rest was exported in a later part of this set" and no longer lists what a later part exported. Re-walk (e1): after part 3 no earlier card offers a repeat. Left to the owner: a note in sealed earlier parts, seal order in the manifest chain, Check the set |
| OW-12 · owner x Check this export clean/tampered/half-finished; Check and finish; Finish as partial wording (f) | partial → yes | RED names the file, no remedy; R-check points at buttons not on the card then is wiped; interrupted run has no Check button; engine words | III (F12, F1) | **F12, F1 built.** RED ends "Do not hand this folder over as it is. Put back the files named above, or export the messages again"; the finished-writing card offers Check this export; a folder that was never finished says "this export is unfinished ... Finish it first"; the refusal stays on the card |
| OW-13 · owner x selection over 200,000 (R-i) and huge tree (R-s) (g) | partial → partial | No early warning, Preview reads everything then refuses; one folder over 200,000 cannot be split; R-s says export from the archive (not built) or contact support | III, II (F14, F15, F16) | **F14 built; F15 and F16's R-s deferred.** The footer says "more than the 200,000 one export can hold" at once (script). F15 is an owner question (a date range, or word R-i for one folder). R-s's words are pinned by `TestRefusalsCarryWhatWhyAndRemedy` ("Export from the archive instead") and the archive source is E4: owner |
| OW-14 · owner x export while backup holds the archive (MA-431); destination inside the archive (h) | partial → yes | R-f clear; the paused export shows "0 of 7 done, 0 B" with no reason | III (F11) | **F11 built.** Progress carries "Waiting: a backup or Archive now is using the archive. This export carries on by itself when that finishes; you can also cancel it."; it clears when the export moves; Cancel works while it waits |
| OW-15 · owner x second window, reload, restart (R-m, R-k, R-x, R-y) (i) | yes → yes | R-k wording wrong for a replaced preview; running card says "fingerprint not recorded"; dead tab keeps a frozen Running card | III, I (F21, F26) | **F21 built.** R-k adds "or another window started a new preview"; a running card says "Writing now: see Running export above."; a dead tab says MailArchive Desktop could not be reached (script) |
| OW-16 · owner x not set up or app-only (R-a, R-a2); Create folder; Browse (o) | partial → yes | R-a says run `mailarchive setup` (separate download); R-a2 jargon | III (F16) | **F16 built in part.** R-a says the command-line tool is a separate download and to ask whoever installed it; R-a2 opens "This installation can't sign in as you ... Whoever manages your Microsoft 365 app registration needs to ..."; the workspace's own line says the same. Create folder, Browse unchanged and fine |
| OW-17 · owner x Overview card to the two pages (m) | yes → yes | Sidebar Export links on the Overview jump to a card then a second click (owner's open item 1, keep) | II (F23) | none (owner ruled: keep) |
| OW-18 · owner x deployment switch off (l) | yes → yes | No Export group, all routes 404, no dead links; `-no-workspace` in `-h` | none | — (re-checked: the Sign in card and the scripts are behind the same switch, `TestDeploymentSwitchHidesTheWorkspace`) |
| OW-19 · owner x keyboard only / screen reader on the two pages (n) | partial → partial | Click-only span folder names and arrows, unnamed checkboxes, no aria-live/progressbar, history rebuilt every 5 s dropping focus (script). **HIGH** | III (F6) | **F6 built (script, not walked).** Folder names and arrows are buttons (aria-expanded, "Show the subfolders of ..."), checkboxes are named from their rows, the tree and list are named, the selection, preview, mailbox line and each card's result are live regions, the progress bar is a progressbar, a redraw keeps what is open and the focus, and the history is not rebuilt under a focused card. No arrow-key tree (a button list, honestly not marked as a tree). **A keyboard-only and a screen-reader walk in a real browser is owed** |
| OW-20 · owner x error text readable at a glance (n) | partial → yes | Raw "openat"/"mkdirat"; damaged run record text with filename and no remedy; fallback echoes raw errors and paths; engine words | III (F8, F20) | **F8, F20 built.** OS errors read as what happened (the system's text after a colon only where a test pins it); a damaged record is `R-rec` (what, why, what still works) and its card says so, Open folder and Check this export read the folder through the journal; any other error carries no raw text or path (the detail rides in `detail`) |
| OW-21 · owner x laptop sleeps, network blip, throttling (reasoned) | yes → yes | Not walked; stop texts R-u/R-u2/R-u3/R-v2 read and complete | none | — |
| HE-1 · helper x opens a clean finished folder cold (k) | yes → yes | README gives status, counts, per-OS verify commands; first line is a run id, selected folders not listed, long technical README | III (F22) | **F22 built in part.** The README lists the selected folders and puts how to check the export before the technical notes (what the files are, caveats). Its first line stays the run id: the identity check reads it (`TestCopyExportRecipientVerification` pins it) — "title first" waits for the owner |
| HE-2 · helper x checks tampered copy and planted file with README commands (k) | yes → yes | shasum names the FAILED file with exit 1; extra-files command prints the planted file | none | — |
| HE-3 · helper x one part or all parts of a retry set (e) | partial → partial | No README states the set total; part 1 never mentions other parts; set fingerprint only in app/receipt in seal order which no folder records; Check covers one part | III (F10) | **F10 built in part.** Part N's README says "This is part N of a set ... The earlier parts: ..."; the receipt carries "Set total: All 7 messages are exported ..." Not built: a note in part 1 (sealed), seal order in the manifest chain, Check the set: owner |
| HE-4 · helper x stopped, cancelled or partial folder (a, e) | partial → partial | Marker points at the app and says delete the folder; no hint where the rest is; partial README lists a count not which; garbled status line | III (F7, F10, F13) | **F7, F13 built, F10 in part.** The README names the failed messages and the manifest's failures list and has a one-sentence status; the marker still cannot say where the rest is (it is written before the retry exists): owner |

---

## Findings

Ship blockers first (F1-F6), then the rest, most valuable first. All are in this branch's scope. Keys cite the cells above. Each finding is the first walk's, then **Built** (the commit, the test that is the positive twin and the red) or **Not built** (why).

**F1 · [Type III · blocker · OW-3, OW-5, OW-6, OW-10, OW-12] A refusal from a card button is shown and then wiped.** *(script)*
`act()` appended the refusal to the card and the `loadHistory()` it started rebuilt every card. **Built** `11dc17d`: the server keeps each run's last refusal (30 minutes, replaced by the next click, cleared by a success), the history answer carries it as `result`, `exports.js` draws it when it builds a card and mirrors a fresh one into the `role="alert"` banner. Test `TestFricACardButtonsRefusalStaysOnItsCard` (positive twin first; the refusal survives three history answers with the marker kept; a success clears it): red before (the refusal gone from the card, no `c.result.error` in the script), green after.

**F2 · [Type III · blocker · OW-2, OW-5, OW-6] "Sign in again" names a control the owner cannot reach.**
**Built** `21e6b7b`, `36f42fe`: Export activity loads the sign-in script and has a Sign in card, shown when the history answer's `signIn.needed` says (a retry refused for the sign-in, the saved sign-in refused, nobody signed in and something retryable, a stopped card's account not the one signed in); a refused saved sign-in is remembered and `source` answers `needs-sign-in`; a workspace refusal for the sign-in shows Sign in. Test `TestFricARefusedSavedSignInCanBeSignedInAgainFromExportActivity` (red: no `#signInBtn`, no script, R-c naming no account), run through a real sign-in job.

**F3 · [Type III · blocker · OW-2, OW-6] The retry's refusals give the wrong remedy.**
**Built** `21e6b7b`: R-b and R-c at retry name the account and "Click Sign in on this page ... then click '<button>' again", R-l at retry the same; the run stops (R-c, R-z) say "click Sign in on Export activity". The engine's own R-l ("Load folders again", design §7.6) is **not changed**: `TestRetryReadsTheRunRecord` asserts it, so the desktop words the retry's R-l for the page and the engine text is an owner question. Tests `TestFricARetryRefusedForTheSignInNamesTheAccountAndTheButton`, `TestFricSignInStopsAndRetryRefusalNameTheButtons`.

**F4 · [Type III · blocker · OW-10] A finished export's card has no Look again, Locate… or This folder is gone.**
**Built** `11dc17d`: after R-r or R-r2 the card offers the three buttons; R-r2 for a folder that is not there says so. Test `TestFricAFinishedCardOffersTheFolderButtonsAfterTheFolderIsLost` (healthy card offers none; renamed: R-r2 in the new words, nothing revealed, three buttons; Locate removes them): red before.

**F5 · [Type III · blocker · OW-9, OW-8] A retry cannot be pointed at another destination.** *(script)*
**Built** `2602dfa`: see OW-9. Test `TestFricARetryPreviewCanBePointedAtAnotherDestination` (the retry into its own destination, into another folder with the earlier export unchanged, into a missing one: R-e naming Browse): red before.

**F6 · [Type III · blocker · OW-19] The folder tree and the page's announcements are mouse-only.** *(script)*
**Built** `e0c60aa`: see OW-19. Test `TestFricTheTreeAndTheAnnouncementsAreReachableWithoutAMouse` reads the page and the scripts (red: nine missing names or regions). **Owed:** the keyboard-only and screen-reader walk.

**F7 · [Type III · OW-8, OW-11, HE-4] The failed messages are not shown or explained anywhere.** **Built** `56b280b`: "Which ones?" on the card, the README's section and first 10 lines, `failures` named. Tests `TestFricCardsListFailuresAndKeepTheStopsWords`, `TestFricReadmeAndCardListTheMessagesThatWereNotExported`.

**F8 · [Type III · OW-5, OW-7, OW-8, OW-20] A stopped run's reason and remedy are lost, and raw system words get through.** **Built** `56b280b`, `36f42fe`: the stop's words stay on an observation card; OS errors in words; R-w leads with this computer's advice; the count is of messages exported; the cut-off message is "not attempted". Tests `TestFricOwnerTextNamesWhatHappenedNotTheSystemCall`, `TestFricAStopCountsTheMessagesExportedNotAttempted` (red: "after 8 of 12").

**F9 · [Type II · OW-11] Offers are per run, so a completed set keeps offering to export it again.** **Built** `958c611`, `36f42fe` (small, keeps every control, inside the design: the retry API still reads the run record, only the offer is derived from the set). Test `TestFricOffersFollowTheWholeRetrySet` (positive twin first; red with the derivation switched off: three cards still offering a repeat).

**F10 · [Type III · OW-11, HE-3, HE-4] Nothing says a set is complete, and the set cannot be checked without the app.** **Built in part** `958c611`: the set total on every card and in the receipt. **Not built, for the owner:** a "continued in" note in a sealed earlier part (its README, checksums and marker are sealed or gone), the seal order in the manifest chain (a change to the sealed format), "Check the set" (a new feature).

**F11 · [Type III · OW-14] An export paused for a capture says nothing.** **Built** `56b280b`. Test `TestFricAnExportHeldBackByACaptureSaysWhy`.

**F12 · [Type III · OW-4, OW-12] Check this export and Check and finish speak in engine words.** **Built** `56b280b`. Tests `TestFricInterruptedAndFinishedWritingCardsSayWhatToDo` and the desktop twin. Not changed: the reconciler's "changed after the export: its marker is gone" label (rare, still says what to run).

**F13 · [Type III · OW-3, OW-4, HE-4] The interrupted card does not say what to do next.** **Built** `56b280b`: the sentence, the one-sentence README status, a "looking for its folder" note. The label words stay (pinned by tests).

**F14 · [Type III · OW-1, OW-13] The selection note and count mislead, and the 200,000 limit is met late.** *(script)* **Built** `958c611`. Test `TestFricTheSelectionNoteCountsTheSubfolders` (the sum the script takes equals the count endpoint's 13).

**F15 · [Type II · OW-13] One folder over 200,000 messages cannot be exported at all.** **Not built — owner question:** a date range in the selection, or R-i worded for the single-folder case. The recommended answer stands: word R-i for one folder now, a date range with E4.

**F16 · [Type III · OW-13, OW-16] Two remedies point at things the Desktop owner does not have.** **Built in part** `56b280b`: R-a, R-a2 and the workspace's own line. **Not built:** R-s (see OW-13).

**F17 · [Type III · OW-7] The preview does not flag a destination that will not work.** **Built** `56b280b`. Test `TestFricPreviewFlagsADestinationThatWillNotWork` (the read-only half skips for root and on Windows, where Start's R-w is the answer).

**F18 · [Type III · OW-1] A "Leave site?" prompt after a successful Start.** *(script)* **Built** `56b280b`, read from the script.

**F19 · [Type III · OW-7] The destination-typing messages are loose.** **Built** `56b280b`, `36f42fe` (the Preview reached its own copy of the check; the re-walk showed the old words). Tests `TestFricDestinationTypingMessagesSayWhatIsWrong`, `TestFricThePreviewsDestinationMessagesSayWhatIsWrong`. The pinned words ("doesn't exist", "full path", "Browse", "Create folder") stay.

**F20 · [Type III · OW-20] Fallback refusals echo raw errors and use the wrong id.** **Built** `958c611`. Test `TestFricADamagedRunRecordIsWordedAndTheFolderStaysUsable`.

**F21 · [Type III · OW-15] Two small wording slips.** **Built** `56b280b`, `36f42fe` (and the dead tab). Test `TestFricSmallWordingAndTheLeaveSitePrompt`.

**F22 · [Type III · HE-1, HE-4] The README opens with a run id and is long.** **Built in part** `56b280b`: selected folders, verification before the technical notes. **Not built:** "title first" — the first line is the identity line the folder check reads and a test pins it; owner.

**F23 · [Type II · OW-17 · known] The Overview's Export group is a card hop, not a link.** Ruled open item 1 (§13.14), keep. No action.

**F24 · [Type I · OW-1, OW-3] Counted ceremonies.** The Microsoft device code (3 steps), the preview before every Start (X14), finishing an interrupted folder before continuing (the marker, XC2/XC5). No change; F13 now says it aloud.

**F25 · [Type I · OW-7, OW-14] A destination that cannot be proven outside the archive is refused (R-f2).** Not walked (no such mount); words read; backlog (owner: none).

**F26 · [Type I · OW-15] A preview is good for 30 minutes and one at a time.** The control behind R-k; F21 corrected the wording.

**F27 · [Type III · blocker · OW-1, OW-2, OW-5, OW-6, OW-17 · found while fixing F2] Two scripts of one page shared a top-level `const`, and the second never ran.** *(script)*
`workspace.js`, `exports.js` and `signin.js` each began with the shared helpers' top-level `const csrf`, `const epoch`, `const $`, `const RESTART`; the workspace page loads `signin.js` then `workspace.js`, the Overview `dashboard.js` then `signin.js`. Classic scripts share one global lexical scope, so the second of each pair is a SyntaxError that does nothing: the workspace page (Load folders, the tree, Preview, Start) and the Overview's Sign in button would have been dead in a browser, in the build the first review passed on its handlers. **Built** `a4fe9d6`: each workspace script sits inside one function. Test `TestFricNoTwoScriptsOfAPageShareATopLevelName` (a small scanner over each page's scripts; red: six shared names across the two pages). **Owed:** seeing it run.

---

## UX contract (X-series) check

- X2 (what, why, remedy, one voice): holds for the refusals walked at the head, and now in effect: F1 (the text stays), F2/F3 (the remedy names a control that is there, and the account), F4 (R-r/R-r2's buttons are on the card), F8/F20 (no raw system words, no file name). R-s's remedy (F16) is the exception, deferred with its reason.
- X6 (a legibility surface per feature): Export activity is the surface; F7, F8, F10, F11, F12 and F13 are closed in the part the sealed format allows (F10's remainder is the owner's).
- X7 (no traceback): holds. A damaged record, an unreadable journal and a read-only state folder each end in a sentence.
- X11 (plain, action-first labels): the labels keep the words their tests pin ("interrupted", "finished as partial"); "marker" is gone from the finished-writing label and the confirm text keeps "manifest and checksums" (a prompt of the script, unchanged).
- X13, X15: not exercised beyond the walk (every endpoint used was a guarded POST; hostile names are the adversarial pass's). The new fields (`failures`, `next`, `setNote`, `result`) reach the page only as JSON strings drawn with `textContent`, and `TestWorkspaceScriptsAndPagesAreInert` still passes.
- X14 (look before writing): holds; F17 makes the preview raise the sentences Start would.

Compliance is necessary, not sufficient; the verdict stands on the cells above. X14 and X15 stay Partial until the adversarial pass has run.

## What the walk could not cover

- **Page behaviour after a click (all JavaScript), before and after.** No browser, JS engine or screen reader. F1, F5, F6, F14, F18, F21, F27 and the Sign in button's appearance are known from the script source and the answers the server gives; none was clicked. The first walk missed F27 for exactly this reason. A click-through (first export, a refused card button, the Sign in recovery, a retry to another destination, keyboard only, with a screen reader) is owed before a release build, and the F1 test, the F27 scanner and the F6 reads are the guards until then.
- **Windows and Linux.** macOS only: R-w's Windows text order, Explorer's `/select,`, the Windows policy and MSI switch, the long-path caveat, FAT/exFAT warnings, Controlled folder access, Linux `zenity`. The read-only preview warning is not produced on Windows (no permission bits to read without writing). The Windows legs of the engine stay compile- and vet-checked only (§13.12); `vet3` is clean for darwin, linux and windows.
- **Real disk, real unplug.** A full disk (ENOSPC) was modelled by the free-space seam and an injected error; a pulled USB stick (EIO) by removing the folder.
- **Real tenant.** Throttling mid-listing (R-v), a 200,000-message listing's real duration, Graph's own error bodies, a real device-code expiry and the 90-day refresh-token lapse (modelled by a refused refresh and a 401).
- **R-f2** (shares, FUSE, overlay), **R-w2**, **R-u/R-u2/R-u3/R-v2** (5-minute outages, redirects): produced only on conditions this machine cannot create; words read.
- **Sleep and wake** (OW-21): reasoned.
- **The real binary.** Built at the head and started with `-no-browser`; it printed its banner and exited 0 on SIGTERM. The dashboard's pages sit behind the one-time link (a browser or a dialog), so they were reached through the same router in-process.
- **Not part of this review:** the combined adversarial pass (E2 write side plus E3) is still owed (§13.14); Readable HTML, attachments and the archive source (E4 to E6) are not built.
- **The unfiltered suite** at the head: see the Disposition.

## Disposition

Filed at `7a29892` with no fixes; fixed test-first in `11dc17d`..`36f42fe` (eight commits, each with its red and its prove-fail recorded in the message) and re-walked at `36f42fe`. The unfiltered `go test ./...` through `inhome`, `vet3` and `gofmt -l` are recorded in the commit message that files this update. **Left for the owner**, each with its reason:

1. The browser click-through (keyboard-only, a screen reader): the condition on the verdict.
2. F15: a date range in the selection, or R-i worded for one folder (recommended: the wording now, the range with E4).
3. R-s's remedy ("Export from the archive instead, or contact support"): `TestRefusalsCarryWhatWhyAndRemedy` pins it, and the archive source is E4. Changing it needs the assertion changed, which is the owner's.
4. The engine's own R-l text ("Load folders again", §7.6): pinned by `TestRetryReadsTheRunRecord`; the desktop words the retry's R-l meanwhile.
5. F22's "title first": the first line is the identity line that `TestCopyExportRecipientVerification` and the folder check depend on.
6. F10's remainder: a note in a sealed earlier part, the seal order in the manifest chain, "Check the set".
7. F23: the owner's ruling, keep. F24-F26: Type I, counted and left.
