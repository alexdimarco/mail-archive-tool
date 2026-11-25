# Friction review: Windows dialogs that fit their words

Date: 2026-11-19. Branch `windows-dialog-fit`. Procedure:
`assurance-kit/process/friction-review.md`. Reviewer: a fresh Claude instance working
under no name, read-only, judging real screenshots from the office test PC (Windows 11,
1920x1080 at 150 %): the released 1.7.3 before the change (`shots-pc`) and the branch
build after it (`shots-fit`, commit 63a70d5), walked as a non-technical Windows owner
making a first Thunderbird backup through ten dialogs.

**Verdict: SHIPPABLE WITH FOLLOW-UPS.** Every list row, prompt and button label is whole,
including every one 1.7.3 cut, and the earlier build's too-wide message-box buttons are
fixed (the IMAP question's from about 340 pixels to 225, the summary's from 250 to 165).
Nothing is worse than before except that each step had its own width (F1).

## Findings and what was done

| ID | Type | What the person meets | Disposition |
|---|---|---|---|
| F1 | II, design choice | Each step had its own width (first menu 715 pixels, kind list 534, date window 405, the yes-or-no questions 636), so the OK button moved by about 160 pixels between steps; 1.7.3 kept every step 400 wide | **Folded** (design note B6): a list or an entry whose words need less is the step width, 400, giving way to the screen cap. The kind list and the date window are now both 658 pixels wide at 150 % (screenshots 32 and 33 of the lab run of 2026-11-19) |
| F2 | III, not yet seen | The start-up health report was not in the screenshots; it may be long enough to scroll, and a scroll box with no border might hide that there is more | **Shot taken:** a typical warning report, as MailArchive's health code words it (a daily backup whose task is gone, last run three days ago), is 14 lines and scrolls at 150 % on a 1080-pixel screen; the scroll bar shows and the last visible line is cut, which says there is more (screenshot 35). No border added |
| F3 | II, the owner's wording call | In that report the reasons (what is wrong, what to do) come after a dozen fact lines, so they are below the fold | **Follow-up**, for the owner: put the reasons right under "Backup health — WARN". The order is the health code's, shared with the command line and Desktop |
| F4 | III, latent | When a message box must grow, its grey band behind the buttons does not follow | **Known limit**, recorded: no wizard question needs a box to grow (all eight that rename their buttons have long texts) |
| F5 | II, not caused by this change | The mailbox list still shows nine rows; a tenth mailbox needs a scroll | **Follow-up**: let the list grow to about fifteen rows |

**The design's two questions.** Should the health view wrap wider first, up to the cap,
before it scrolls? **No**: lines of about 190 characters are hard to follow, and the
longest view scrolls either way; the better cure is F3. Should the list and entry dialogs
share one floor? **Yes, 400**, giving way to the cap on small screens, in this release
(done, F1).

**Could not be judged from the screenshots:** the sideways scroll bar of a row wider than
the screen, the progress dialog growing, Desktop's link entry, other screen sizes and
scalings, high contrast, a second monitor, and how Narrator reads the scrolling report.

## Re-check (2026-11-19, the same lens on the lab run's screenshots 31 to 36)

**F1: SETTLED.** The kind list, the date window and both health views are now 636 pixels
wide, as the yes-or-no questions already were, with OK 254 pixels in from the right edge;
only the menus with long rows are wider, by design. With the walk's own mailboxes OK now
moves about 50 pixels between steps instead of about 160. Nothing is worse than before:
the kind list is the width 1.7.3 gave every step, and the empty rows under short menus
were there before.

**F2: PARTLY.** The scroll bar shows, but in the typical warning report the one line that
says what is wrong is on lines 20 and 21 of the box, the second cut at its edge, with
about 4 to 7 lines out of sight, while the three actions sit in a list nine rows tall.

| ID | Type | What the person meets | Disposition |
|---|---|---|---|
| G1 | II, layout | A report cut mid-sentence above an action list 9 rows tall for 3 actions, about 6 lines of white | **Folded** (design note B11): when a list's prompt is too tall for the room, the list first gives up its empty rows, down to the rows it holds, and the prompt takes that room; it scrolls only if it still does not fit. Ordinary steps keep upstream's list height. In the lab run that followed, the typical warning report shows whole, both of its reasons above a three-row list, nothing scrolling |
| G2 | III, legibility | The report's own words: typed commands in a point-and-click wizard; "Fixity coverage", "manifest", "baseline", "Posture"; "Program: this program" and "scheduled: export"; "not installed … installed 2026-10-02"; padded labels that do not line up in Windows' font | **Follow-up, the owner's wording call**, with F3: for the wizard's start-up view, the reasons first in plain words with what to pick, the commands and internal fields left to the command line's status report |

Could not be judged: whether a row is chosen when the real wizard opens (the lab's shots
passed no default; they now pass the wizard's), the steps not re-shot on this build, and
the hidden lines of the report.
