# Design: Windows dialogs that fit their words

Status: DRAFT, revision 1 (2026-11-18), for the pre-code design review
(`docs/review-windows-dialog-fit-predesign.md`). Owner's go: "yes to widening the
windows dialogue" (2026-11-18, after the MA-1467 Windows walk). Target: 1.7.4.
Owes, in order: this design review; build; the adversarial pass (supply chain and
hostile text); the friction review (on Windows screenshots); MA-1467's Windows walk
again; CI on macOS, Linux and Windows.

## 1. Why

The 2026-11-18 walk of the 1.7.3 wizard on the office Windows test PC (Windows 11,
1920x1080 at 150 % scaling, the release's `mailarchivew.exe`, an invented
Thunderbird profile; screenshots kept in the owner's lab folder) showed that the
words MailArchive passes to its dialogs are cut on Windows:

- The mailbox list (MA-1462..1466): 6 of 10 rows lost their end at about 36 to 41
  characters: `qzsam.walker@gmail.com (13 GB, last cha`,
  `Older copy: qzsam.walker@gmail.com (1.`, `firstname.lastname@mail.qzuniversity.exa`.
  Sizes, dates and the end of long addresses are not drawn, with no sign.
- Older cuts, there since 1.0: menu rows (`Archive a mail file on this computer
  (Outlook`, `Incremental — only messages new since the`); prompts cut to one
  line with "…" (`Also keep a copy of each original message (…`,
  `Keep this archive current automatically?A sc…`, where the line break is lost);
  button labels clipped on both sides (`pen the archiv` for "Open the archive" on
  every backup's summary, `xport what's c` on the IMAP question); the progress
  dialog's two- and four-line texts (wizard.go's sync wait and Outlook export)
  shown as one cut line.

What was right: every picked row, cut or not, archived the mailbox it names (the
archive log read the older profile's folder for the cut "Older copy:" row), because
the list returns the item by its index, never by the drawn text.

Cause: `github.com/ncruces/zenity` v0.10.12 (pinned in go.mod since 1.0) draws its
Windows list, entry and progress dialogs at fixed sizes (`list_windows.go`: window
281x281, label 241x16, list 241x164, buttons 75x24, scaled by DPI), its labels with
`SS_WORDELLIPSIS` (one line, cut with "…"), and its message boxes with Windows'
`MessageBox`, whose buttons keep the width Windows chose for "OK"/"Cancel" after
zenity's hook renames them. `zenity.Width` is documented "Unix only". The newer
releases (v0.10.13 to v0.10.15, read 2026-11-18) do not change these sizes, and
v0.10.15's `list_unix.go` turns any list given default items into a checklist,
which would change the Linux picker (MA-1467's Linux lab line, "nothing
preselected": the picker passes `DefaultItems` for the All row), so
moving up is not a fix.

## 2. What must hold (properties)

- **P1, whole words on Windows.** In the library's Windows list, entry and progress
  dialogs and in its message boxes, every word MailArchive passes is drawn whole:
  every list row is drawn whole, or, when it is wider than the widest list the
  screen allows (§4.3, the cap), the list scrolls sideways far enough to show its
  end; every line of a prompt is drawn (lines wrap; none is cut with "…"); every
  button label is drawn whole. Limit, stated: a prompt taller than the screen's
  working area minus the rest of the dialog is cut at the bottom (no prompt of ours
  is near that).
- **P2, the same answers.** Which item a click or Enter returns, the default
  selection, `DisallowEmpty`, the extra button, Cancel, the window's close box and
  `zenity.Context` cancellation behave exactly as in v0.10.12. Only sizes and
  positions change, plus the list's sideways scroll.
- **P3, nothing changes elsewhere.** macOS and Linux run the upstream v0.10.12 code
  byte for byte: the patch touches only files built for Windows, plus one new file
  of pure arithmetic built everywhere but called only from Windows files (§4.3).
- **P4, a known copy.** The copy is upstream v0.10.12 file for file except the
  files the patch lists; every other file's SHA-256 matches upstream's; the version
  stays pinned, and moving it forces a re-read of the patch (as MA-315 does for
  go-pst's copied loops).

Non-goals: the native file and folder pickers (Windows draws those, unchanged);
notifications; the color, date and password dialogs (MailArchive opens none);
any wording change; naming the account in the IMAP question (the open owner
question, friction F12); moving to a newer zenity.

## 3. Options weighed

1. **Patch a copy of zenity v0.10.12** (chosen). Changes at the place that draws;
   DPI changes and every caller covered with no call-site change; the
   `dialogfront` walkers (MA-1111, MA-1115, MA-1117), keyed on the import path
   `github.com/ncruces/zenity`, keeps working because the path does not change.
2. Re-lay out zenity's windows from outside, after they appear (a WinEvent watcher
   like `internal/dialogfront`): no copy, but it depends on zenity's private
   control layout, the dialog would first show narrow and then jump, and zenity's
   own `WM_DPICHANGED` handler would restore the narrow layout behind it.
3. Our own Windows dialogs behind a wrapper package: about 60 call sites move to a
   new API, and MA-1113's walker would have to learn it (a change to a gate).
4. Move to a newer zenity: does not fix the sizes and changes the Linux list (§1).

## 4. Design

### 4.1 Where the copy lives

`third_party/zenity/` holds module `github.com/ncruces/zenity` with upstream
v0.10.12's files as the Go module cache extracted them (checked by `go mod verify`
against the `h1:` hash in our go.sum, there since 1.0), minus upstream's `_test.go`
files, `cmd/` (its command-line program) and `.github/`. The root go.mod gains

    replace github.com/ncruces/zenity v0.10.12 => ./third_party/zenity

and keeps `require github.com/ncruces/zenity v0.10.12`. The replace names the
version, so a version move in `require` stops using the copy rather than silently
mixing the two, and the pin test (§5, MA-1468) fails until someone re-reads the
patch. `third_party/zenity` is its own module, so `go test ./...` at the root does
not run inside it; the copy's own tests are ours (§5).

`third_party/zenity/UPSTREAM.md` says where the copy came from (module, version,
the go.sum `h1:` hash, the date), why it is patched, which files the patch changes
or adds, and how to drop the copy (delete the folder and the replace line).
`third_party/zenity/UPSTREAM.sha256` lists the SHA-256 of every copied upstream
file as copied, before the patch.

### 4.2 What the patch changes

Files changed (Windows only): `list_windows.go`, `entry_windows.go`,
`progress_windows.go`, `msg_windows.go`, `util_windows.go` (a label style without
the ellipsis and the measuring helper), `internal/win/user32.go`,
`internal/win/gdi32.go` and `internal/win/zsyscall_windows.go` (two new calls,
`DrawTextW` and `SelectObject`, plus constants: `DT_*`, `LB_SETHORIZONTALEXTENT`,
`WM_GETFONT`, `SPI_GETWORKAREA`, `SM_CXVSCROLL`). File added:
`fit.go` (pure arithmetic, every OS, no Windows calls) and its tests.

### 4.3 How the dialogs are laid out

All sizes below are in logical pixels (at 96 DPI) and scaled by the window's DPI as
upstream scales its fixed sizes. Text is measured with the dialog's own font at
that DPI (`getFont().forDPI`, the font upstream already sets on every control),
using `DrawTextW` with `DT_CALCRECT|DT_NOPREFIX|DT_EXPANDTABS` and, for a
prompt, `DT_WORDBREAK|DT_EDITCONTROL` at the label's width (the flags the label is
drawn with), for a row or a button `DT_SINGLELINE`.

- **The cap.** The widest a dialog may be is 80 % of the primary monitor's working
  area (`SystemParametersInfo(SPI_GETWORKAREA)`), the monitor upstream's
  `centerWindow` centres on; the tallest, 90 % of its height.
- **List dialog.** Content width = the largest of: 241 (upstream's, so a short menu
  looks as before); the widest row plus the vertical scroll bar's width
  (`SM_CXVSCROLL` at this DPI) plus 8 for the list's border and padding; the
  widest prompt line up to 400 (wider prompt lines wrap rather than widen the
  dialog); the button row (below). Then capped. The list keeps upstream's height
  (164, about nine rows, scrolling for more), is created with `WS_HSCROLL`, and
  gets `LB_SETHORIZONTALEXTENT` = the widest row plus 8, so a row wider than the
  capped list can be scrolled into view and the scroll bar appears only then.
  The prompt label is as wide as the content and as tall as its wrapped text (at
  least one line, 16), drawn without `SS_WORDELLIPSIS`; the list starts 4 below it.
- **Buttons, in every patched dialog.** Each is as wide as the widest label in its
  row plus 12 on each side, at least 75 (upstream's), all the same width, 24 high,
  8 apart, right-aligned as upstream aligns two; with an extra button the row is
  laid out right to left as upstream's OK, extra, Cancel order.
- **Entry dialog.** As the list dialog without the list: the edit box is as wide as
  the content (at least 241), 24 high, 4 below the prompt.
- **Progress dialog.** Its text arrives after it opens (`Text`), so its content
  width is fixed at 400 (wider than upstream's 241, about 60 characters a line),
  capped as above. Each `Text` call measures the new text at that width; the label
  and the window grow to fit it and never shrink while the dialog is open, so the
  window does not jump as the wizard's two-second updates change a number.
- **Message boxes.** Windows lays them out; zenity's existing `WM_INITDIALOG` hook
  renames the buttons. After renaming, the hook measures each label with the
  button's own font (`WM_GETFONT`), and when a label needs more than its button's
  width, it widens every button of the row to the widest need, re-flows the row
  right-aligned with the gap Windows used, widens the dialog by any shortfall at the
  left and re-centres it. Nothing else in the box moves.
- **DPI changes.** On `WM_DPICHANGED` the list, entry and progress dialogs measure
  again at the new DPI and lay out again (upstream re-applies its fixed sizes
  there). Windows re-scales a message box itself.

`fit.go` holds the arithmetic as pure functions: given measured widths and heights,
the DPI, the scroll bar's width and the working area, it returns every rectangle and
the sideways extent. The `_windows.go` files only measure and apply.

### 4.4 What stays as it is

What a dialog returns and when (P2): the list's items, their order and their
mapping back by index; the keyboard (arrows, Home/End, Enter, Escape, Tab); default
items; `DisallowEmpty`'s greyed OK; the close box; `zenity.Context`; the hook's
icon and title work; `internal/dialogfront`, which still sees the same windows
(laid out before `ShowWindow`, except the progress dialog's later growth, which is a
size change it does not watch).

## 5. Tests and catalog rows

New rows (numbers as of this draft): **S86** (Windows dialogs show all their words)
and:

- **MA-1468 (U, every OS, main module).** go.mod requires zenity v0.10.12 and
  replaces exactly that version with `./third_party/zenity`; every file under
  `third_party/zenity` is in `UPSTREAM.sha256` with a matching SHA-256 or is one of
  the patch files `UPSTREAM.md` lists; every manifest file exists; no upstream
  `_test.go` is present; the files built for macOS and Linux are all unpatched
  (P3, P4).
- **MA-1469 (U, every OS, the copy's own tests, run by a new CI step
  `go -C third_party/zenity test ./...` on all three runners).** `fit.go`'s
  arithmetic with a stand-in measurer: rows that fit set no sideways extent and
  widen the list to the widest row; rows past the cap leave the list at the cap
  with the extent at the widest row; 241 and 75 floors; prompt lines up to 400
  widen, longer ones wrap and the label's height is the wrapped height; buttons fit
  their labels and stay equal and right-aligned; DPI 96, 120, 144 and 192 scale
  every rectangle; the progress label grows and never shrinks; nothing exceeds the
  cap. Positive twin first: upstream's own sizes come out unchanged for upstream's
  short words.
- **MA-1470 (L, Windows lab: the office test PC, env-gated like MA-1113).** Real
  dialogs opened through the public API (list, entry, progress, a question with
  renamed buttons) with the walk's rows, a hostile 80-character name, right-to-left
  and zero-width text, measured from outside with Win32 (`GetClientRect`,
  `LB_GETHORIZONTALEXTENT`, `DrawTextW` with the control's `WM_GETFONT`): each row
  fits the list's client width or the extent covers it; each prompt's wrapped
  height fits its label; each button's label fits its button; nothing exceeds the
  cap; then closed through `zenity.Context`. Plus MA-1467's Windows walk again,
  with screenshots for the friction review.

Every fix is proved fail then pass with a mutation sweep: `fit.go` and the
measuring glue on the Mac and Linux through MA-1469; the Windows glue through
MA-1470 on the test PC.

## 6. Rollout and the way back

Ships in 1.7.4 for every platform; only Windows' dialogs change. Nothing is stored
or written differently, so there is nothing to migrate. The way back is one line:
delete the replace (and the folder), and the dialogs return to upstream's sizes.

## 7. What it costs to keep

About the size of the patched files' changes (a few hundred lines of Windows
layout) are ours to keep. Moving zenity's version means re-reading the patch
against the new upstream files (MA-1468 fails until the pins move). govulncheck
cannot match a version for a module replaced by a folder; zenity's advisories are
not tracked by it while the copy is in place (not checked: whether zenity has ever
had one). The change will be offered upstream (an issue or pull request to
ncruces/zenity adding Windows sizing that fits its text), posted only with the
owner's OK; if upstream takes it, the copy goes.

## 8. Security

No new process, privilege, file, network use or setting. The new calls measure text
(`DrawTextW`, `SelectObject` on a window DC) and size controls (`SetWindowPos`,
`LB_SETHORIZONTALEXTENT`); the message-box hook still acts only on dialogs of its
own thread. The text measured is the text upstream already drew: names reach the
picker already cleaned and capped at 80 characters (MA-1287), and any long or
hostile text only makes a dialog as wide as the cap. The copy is a supply-chain
change: P4 and MA-1468 make every difference from upstream visible in one listed
set of files.

## 9. Claims, labelled

- P1: proven on the screens MA-1470 runs on (the test PC at 150 %), by measurement
  from outside the dialog; conditional elsewhere on Windows measuring and drawing
  text the same way, which is what `DrawTextW` is for. GitHub's Windows runner does
  not run MA-1470 (it opens real windows) and runs no wizard test today.
- P2: conditional on the patch leaving the message and command handling untouched,
  checked by review of the diff and by MA-1470's answers.
- P3, P4: proven by MA-1468 on every OS.
