# Design: Windows dialogs that fit their words

Status: DRAFT, revision 3 (2026-11-18), cleared for build. Revision 1 (commit
`c909b20`, sha256 `b7b63cc02eb1e797e54ca370deafa26fb8e9484ee48b94cb5a8f831080a4a81d`)
passed the pre-code design review GO_WITH_CONDITIONS
(`docs/review-windows-dialog-fit-predesign.md`: 22 conditions, C1 to C22).
Revision 2, an unpublished local draft, folded them in; its fold check (filed in the
same report) found six conditions only partly folded and sixteen new points. This
revision folds both: each change carries its tag ([C3] for a condition, [N4] for a
fold-check point), and §10 lists them. Owner's go: "yes to widening the windows
dialogue" (2026-11-18, after the MA-1467 Windows walk). Target: 1.7.4. Owes, in
order: the build, in the slices of §4.5; the adversarial pass (supply chain and
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
- Found by the review, not seen in the walk: the wizard's start-up health view,
  shown at every start once a backup is scheduled, passes the whole health report
  as the list's prompt on Windows (`internal/wizard/listtext.go:41-44`,
  `wizard.go:684, 1255`). v0.10.12 shows its first line only, ending in "…".

What was right: every picked row, cut or not, archived the mailbox it names (the
archive log read the older profile's folder for the cut "Older copy:" row), because
the list returns the item by its index, never by the drawn text.

Cause: `github.com/ncruces/zenity` v0.10.12 (pinned in go.mod since 1.0) draws its
Windows list, entry and progress dialogs at fixed sizes (`list_windows.go`: window
281x281, label 241x16, list 241x164, buttons 75x24, scaled by DPI), its labels with
`SS_WORDELLIPSIS` (one line, cut with "…"), and its message boxes with Windows'
`MessageBox`, whose buttons keep the width Windows chose for "OK"/"Cancel" after
zenity's hook renames them. `zenity.Width` is documented "Unix only". The newer
releases (v0.10.13 to v0.10.15, the newest, read 2026-11-18) do not change these
sizes, and v0.10.15's `list_unix.go` turns any list given default items into a
checklist, which would change the Linux picker (MA-1467's Linux lab line, "nothing
preselected": the picker passes `DefaultItems` for the All row), so moving up is
not a fix.

## 2. What must hold (properties)

- **P1, whole words on Windows [C1, C5, C22, N8].** In the library's Windows list,
  entry and progress dialogs and its message boxes, except where a fallback
  (§4.3, Fallbacks) keeps v0.10.12's layout:
  - every list row is drawn whole, or, when it is wider than the widest list the
    screen allows (the cap, §4.3), the list scrolls sideways far enough to show
    its end. Known limit: only a mouse can scroll sideways (in a one-column list
    Left and Right move the selection, as Up and Down do), and while a row's end
    is in view, the rows' starts are not;
  - every line of a list's prompt is drawn (lines wrap; none is cut with "…"),
    or, when the prompt is taller than the room under the height cap, it scrolls
    far enough to show its end. Known limit: it scrolls with the mouse only,
    because the keyboard stays on the list (P2);
  - every line of an entry's or a progress dialog's prompt is drawn up to the
    height cap; past it, the prompt is cut at the bottom. Limit, stated:
    MailArchive gives these dialogs fixed texts of at most about six lines
    (`wizard.go:1716-1745, 1766`; `internal/desktop/dialogs.go:39`);
  - an entry's starting text is shown whole up to the cap; past it, it scrolls
    in its box as before;
  - every button label is drawn whole, unless it is wider than its share of the
    widest dialog the screen allows; then it is cut, and its button stays inside
    the dialog (§4.3, Buttons). MailArchive's labels are short fixed words [B1].

  A message box's own text is Windows' layout and is outside P1: it is drawn
  whole only while the box fits the work area. MA-1470 records whether Windows
  cuts a taller one.
- **P2, the same answers.** Which item a click or Enter returns, the default
  selection, `DisallowEmpty`, the extra button, Cancel, the window's close box and
  `zenity.Context` cancellation behave exactly as in v0.10.12. Only sizes and
  positions change, plus the list's sideways scroll, a too-tall list prompt's
  scroll, and one focus rule: a click in a scrolling prompt hands the focus back
  to the list [C1].
- **P3, nothing changes elsewhere [C16, N1].** The files macOS and Linux build are
  upstream v0.10.12's byte for byte, plus `fit.go`, which they never call: every
  patch file builds for Windows only. The shipped macOS and Linux programs differ
  from 1.7.3's only in build information.
- **P4, a known copy [C12, C14, C16].** The copy is the published v0.10.12 module
  minus the files it leaves out (every `_test.go`, `cmd/`, `.github/`,
  `testdata/`), with the patch files the pins list, plus `fit.go`, `fit_test.go`
  and `UPSTREAM.md`. Every other file's SHA-256 matches the published module's:
  the pins list every file of the module, and their hashes reproduce the module's
  `h1:` hash. The version stays pinned. Moving it turns MA-1468 red until each
  patch file's record names the new upstream file it was re-derived from;
  re-reading the patch is the move procedure's step (§7), which no test can force.

Non-goals: the native file and folder pickers (Windows draws those, unchanged);
the color, date and password dialogs (MailArchive opens none); notices; a message
box's own text layout; any wording change; naming the account in the IMAP question
(the open owner question, friction F12); moving to a newer zenity.

## 3. Options weighed

1. **Patch a copy of zenity v0.10.12** (chosen). Changes at the place that draws;
   DPI changes and every caller covered with no call-site change; the
   `dialogfront` walkers (MA-1111, MA-1117) [C21], keyed on the import path
   `github.com/ncruces/zenity`, keep working because the path does not change.
2. Re-lay out zenity's windows from outside, after they appear (a WinEvent watcher
   like `internal/dialogfront`): no copy, but it depends on zenity's private
   control layout, the dialog would first show narrow and then jump, and zenity's
   own `WM_DPICHANGED` handler would restore the narrow layout behind it.
3. Our own Windows dialogs behind a wrapper package: about 47 call sites outside
   tests move to a new API, and the walkers (MA-1111, MA-1117) would have to
   learn it (a change to a gate) [C21].
4. Move to a newer zenity: does not fix the sizes and changes the Linux list (§1).

The message-box re-flow (§4.3) also leans on a layout we do not own, Windows'
`MessageBox` [C6]. That is acceptable where option 2 was not: zenity's hook
already relies on those buttons' IDs to rename them; the re-flow moves only the
box's own buttons, and only after every read succeeded (§4.3, Fallbacks) [N15], so
its worst case is upstream's look, never a wrong answer; option 2 would be undone
by zenity's own `WM_DPICHANGED` layout every time. MA-1470 is run again after each
Windows feature update (§7).

## 4. Design

### 4.1 Where the copy lives

`third_party/zenity/` holds module `github.com/ncruces/zenity` with the files of
the published v0.10.12 module, minus every `_test.go`, `cmd/` (its command-line
program), `.github/` and `testdata/` (upstream's test data) [C16]. The root
go.mod gains

    replace github.com/ncruces/zenity v0.10.12 => ./third_party/zenity

and keeps `require github.com/ncruces/zenity v0.10.12`. The replace names the
version, so a version move in `require` stops using the copy rather than silently
mixing the two, and MA-1468 turns red. The import path is kept, rather than
rewritten as `internal/source/parsev1`'s copy was, because every call site and
the `dialogfront` walkers key on `github.com/ncruces/zenity` [C12].

The copy keeps upstream's go.mod (go 1.20) and go.sum; they are upstream files and
never patch files, and new code in the copy is written to Go 1.20's rules (no
`min` or `max` built-ins; a loop variable is shared across iterations) [C15].
`go test ./...` at the root does not enter a nested module, so a main-module test
runs the copy's own tests and vet (§5, MA-1469) [C15]. Test programs are never
built inside the copy: a mutation sweep builds them elsewhere with `-o` [N12].

`.gitattributes` gains `third_party/zenity/** -text`, so no checkout rewrites a
byte of the copy, as for parsev1's copy (MA-331) [C13].

**The pins [C12, C14, N5].** `internal/thirdparty/testdata/zenity.pins`, in the
main module, has a provenance header (recorded date, toolchain, module, version,
the module's `h1:` hash, and go.sum's two zenity lines) and one line for each of
the published module's 131 files: its SHA-256 and one of copied, patched or not
copied. For each patched file it also records: the SHA-256 of our version; the
imports it adds to its upstream original's, if any [N2]; and the lab row that
proves it (MA-1470 for every file this design patches) with that run's date,
Windows build, scaling and result, which must read pass. The pins are written only
by a recorder test run with an explicit flag
(`go test ./internal/thirdparty -run TestRecordZenityPins -zenity-record`), which
reads v0.10.12 from the Go module cache (`go mod download -json`) after checking
that its files reproduce the `h1:` hash. Changing the upstream lines is an
operator decision, as for MA-331's pins. A patch file's record is written by the
same recorder, given the lab result to record, in the same change as the patch
file.

`third_party/zenity/UPSTREAM.md` says where the copy came from (module, version,
`h1:`, date), why it is patched, which files the patch changes or adds, how the
stubs in `zsyscall_windows.go` were added [C11], the upkeep of §7 (who moves the
copy, the move procedure, the re-check at each minor release, and the record of
upstream changes taken or left out) [C17], and the way back of §6 [C18].

THIRD-PARTY-NOTICES.md, at the root and in the MSI, gains a line under "Copies in
the source tree": `third_party/zenity` holds a modified copy of zenity v0.10.12
(MIT, its LICENSE kept), changed in the files UPSTREAM.md lists [C19].

### 4.2 What the patch changes

Files changed, all built for Windows only [N1]: `list_windows.go`,
`entry_windows.go`, `progress_windows.go`, `msg_windows.go`, `util_windows.go` (the
label style without the ellipsis, the measuring helper, centring in the work area),
`internal/win/user32.go`, `internal/win/gdi32.go` and
`internal/win/zsyscall_windows.go`. Files added: `fit.go` (pure arithmetic, built
everywhere, no imports, no Windows calls), `fit_test.go` and `UPSTREAM.md`.

Windows calls added [C11]. Each is required unless marked optional; an optional
call is declared with "?" in its `//sys` line, as upstream declares
`GetDpiForWindow`, and a missing one takes its fallback. A required stub for a
function Windows lacks would panic at first use, so every call newer than
Windows 10's first release is optional.

- `DrawTextW` (user32): measuring text.
- `SelectObject` (gdi32): selecting the dialog's font into the measuring DC, and
  back.
- `GetClientRect` (user32): client sizes (the message box; the outer-size
  fallback).
- `MapWindowPoints` (user32): a message box's button rectangles from screen to
  client coordinates.
- `MonitorFromWindow` and `GetMonitorInfoW` (user32): the window's monitor and its
  work area.
- `GetSystemMetricsForDpi` (user32, optional): scroll bar and edge sizes at the
  window's DPI. Fallback: `GetSystemMetrics`, scaled by the window's DPI over the
  system DPI.
- `AdjustWindowRectExForDpi` (user32, optional): the outer size for a client size.
  Fallback in §4.3 (Outer size).

Constants added: `DT_CALCRECT`, `DT_WORDBREAK`, `DT_EDITCONTROL`, `DT_NOPREFIX`,
`DT_EXPANDTABS`, `DT_SINGLELINE`, `LB_SETHORIZONTALEXTENT`, `WM_GETFONT`, `WM_APP`,
`SM_CXVSCROLL`, `SM_CYHSCROLL`, `SM_CXEDGE`, `ES_MULTILINE`, `ES_READONLY`,
`EN_SETFOCUS`, `SW_SHOWNA`, `MONITOR_DEFAULTTOPRIMARY`, `MONITOR_DEFAULTTONEAREST`,
each only if `internal/win` does not already define it. Already present and
reused: `SetFocus`, `GetWindowDC`, `ReleaseDC`, `GetWindowRect`, `SendMessage`,
`SetWindowPos`, `GetDpiForWindow`, `GetDeviceCaps`,
`SWP_NOACTIVATE`, `WS_HSCROLL` [N13].

How the stubs are added [C11]: `zsyscall_windows.go` is generated from the
`//sys` lines. First check whether the generator version upstream used reproduces
the file byte for byte; if it does, add the `//sys` lines and regenerate; if not,
add the stubs by hand in the file's existing `syscall.Syscall` style beside
matching `//sys` lines. UPSTREAM.md records which, and the file's diff against
upstream only adds lines.

The patch adds no `init` function. The only package-level variables it adds are
the generated `NewProc` lines for the new calls in `zsyscall_windows.go`, which
load nothing until first used; a program that opens no dialog runs none of the
patch [C16, N2]. No patch file is expected to need an import its upstream original
lacks; one that does is listed in its pins record [N2].

### 4.3 How the dialogs are laid out

**Units [C3].** `fit.go` works in device pixels at the window's DPI. Text is
measured, and system metrics are read, at that DPI and are never converted to
logical pixels and back. Upstream's fixed sizes (12, 241, 75, 24, 8, 164 and the
rest) and this design's (400, the 12 beside a button label, the 4 between
controls) are logical pixels, scaled to the DPI rounding up. Every control is
created at least as large as what it was measured at.

**Measuring [C3, C10, N4].** Text is measured with the dialog's own font at that
DPI (the font upstream already sets on every control) by `DrawTextW` with
`DT_CALCRECT|DT_NOPREFIX|DT_EXPANDTABS`, plus `DT_WORDBREAK|DT_EDITCONTROL` at the
label's width for a prompt and `DT_SINGLELINE` for a row or a button label. The
count passed is -1, with the NUL-terminated UTF-16 text, never a Go byte length.
Empty text is never measured: it takes one line. Each measuring pass takes one
`GetWindowDC` with a deferred `ReleaseDC` on the same thread, selects the font with
`old := SelectObject(dc, font)` and puts `old` back before the release, never
deletes a font got from `WM_GETFONT`, and measures only text that changed.

**Prompt labels, in every patched dialog [C3].** A prompt label is as tall as its
wrapped text plus a quarter line of slack, and at least one line. The slack means
a pixel lost between measuring and drawing never drops the last line, which the
label's `SS_EDITCONTROL` style would hide with no sign. Labels are drawn without
`SS_WORDELLIPSIS`. A word that cannot break at a space: the text is measured at
the label's final width, and a measured width wider than the label widens the
content, up to the cap, and is measured again; past the cap, the edit-control
wrap breaks the word. [B8: on Windows the measuring (`DT_EDITCONTROL`) breaks such a word inside
the line, as the label draws it, so the widening does not run: in the lab, a
70-character word kept the progress dialog at 400.]

**The monitor, the cap and centring [C2].** A dialog belongs to one monitor: at
open, the one upstream centres on (the primary); after that, the window's own
(`MonitorFromWindow`, nearest). The cap is that monitor's work area
(`GetMonitorInfoW`), read in device pixels at every layout, including
`WM_DPICHANGED`: a dialog is at most 80 % of its width and 90 % of its height. A
patched dialog is centred in that work area, not on the whole screen; upstream's
`centerWindow` stays as it is for the dialogs the patch does not touch.

**Outer size [C2].** The window's outer size is computed from the client size its
content needs, for the window's own styles at its DPI
(`AdjustWindowRectExForDpi`). Where Windows lacks that call, the window is sized,
its client area read back with `GetClientRect`, and the size corrected once.

**List dialog [C1, C4].** Content width = the largest of: 241 (upstream's, so a
short menu looks as before); the step width, 400, so the wizard's steps whose words need less share one width; it gives way to the cap, where 241 does not [B6]; the list's width (below); the widest prompt line, up
to 400 (wider lines wrap rather than widen the dialog); the button row (below).
Then capped.

- The list. Its text area is the widest row plus the list box's item margins plus
  one average character of slack. Its width is the text area plus both client
  edges (`SM_CXEDGE`) plus the vertical scroll bar (`SM_CXVSCROLL`), each at the
  DPI. It is created with `WS_HSCROLL`, and its sideways extent
  (`LB_SETHORIZONTALEXTENT`) is always set to the text area, so the sideways bar
  appears only when the list is held at the cap. It keeps upstream's height (164,
  about nine rows, scrolling for more); when the sideways bar appears, the list
  grows by that bar's height (`SM_CYHSCROLL` at the DPI), so as many rows show as
  before.
- The prompt: a prompt label (above), as wide as the content; the list starts 4
  below it.
- A prompt too tall for the room [C1]. When the wrapped prompt would push the dialog past the height cap, the list first gives up its empty rows, down to the rows it holds, and the prompt takes that room [B11]; if it would still push the
  dialog past the height cap, it is shown instead in a read-only multi-line EDIT
  control (`ES_MULTILINE|ES_READONLY|WS_VSCROLL`, no border, no `WS_TABSTOP`, the
  label's font) as tall as the room left under the cap, so the dialog stands
  exactly at the cap. Its text has every "\n" written as "\r\n" (an edit control
  breaks lines only there). Both controls are created at setup and every layout
  shows exactly one (shown with `SW_SHOWNA`, hidden with `SW_HIDE`); `fit.go` says
  which. When the EDIT control reports `EN_SETFOCUS`, the dialog sets the focus
  back on the list, so Enter, Escape, Tab and the arrows behave as in v0.10.12; the
  prompt scrolls with the mouse wheel and its scroll bar. In MailArchive this is
  the start-up health view of a scheduled archive with warnings or gaps. If
  MA-1470 shows the EDIT control cannot be made to behave, the fallback is a cut at
  a whole line ending in a visible "…", P1's limit is restated, and the review's
  L1-01 is re-opened.

**Buttons, in every patched dialog.** Each is as wide as the widest label in its
row plus 12 on each side, at least 75 (upstream's), all the same width, 24 high,
8 apart, right-aligned as upstream aligns two; with an extra button the row is
laid out right to left in upstream's OK, extra, Cancel order. When the labels would
need a row wider than the cap, the buttons share the content width equally, so a
label wider than its share is cut and never pushes a button out of the dialog
[B1].

**Entry dialog [C5, N14].** As the list dialog without the list and without the
scrolling prompt. The content width also takes the starting text, measured on one
line plus the edit box's margins, up to the cap. The edit box is as wide as the
content (at least 241, and the step width, 400, the cap permitting [B6]), 24 high, 4 below the prompt.

**Progress dialog [C2, C3, C7].** Its text arrives after it opens (`Text`), so its
content width is fixed at 400 (wider than upstream's 241: about 75 characters a
line at the 150 % screenshots' message font, an estimate), capped as above; it
opens with an empty label, one line high [N4]. `Text` keeps upstream's
`SetWindowText` and then sends the dialog one private message (`WM_APP`+n). The
dialog's window procedure, on the dialog's own locked thread, then measures the
new text, grows the prompt label (with its slack) and the window, and calls
`SetWindowPos`, with the same layout code as at open and on `WM_DPICHANGED`. The
label and the window never shrink while the dialog is open, so the window does not
jump as the wizard's two-second updates change a number. [B8: Nor does the width shrink: the content's width so far is kept with
the prompt's height, and both are re-based to a new DPI rounding up, so a round trip
between two DPIs never shrinks them (adversarial findings CON-03, OUT-05).] That height is kept per
dialog, with no package-level layout state (Desktop can have two dialogs open),
and is re-based on `WM_DPICHANGED`. The window grows downward with its top edge
fixed, moving up only as far as it must to stay inside the work area.

**Message boxes [C2, C6, C9, N15].** Windows lays them out; zenity's existing
`WM_INITDIALOG` hook renames the buttons. When it renamed at least one, the hook
then reads every button of the box's row, renamed or not: its rectangle
(`GetWindowRect`, then `MapWindowPoints` into the box's client coordinates), its
font (`WM_GETFONT`) and its label; and the box's client and window rectangles. If
every read succeeded and a label needs more than its button's width (a need is the
label's measured width plus 12, scaled, on each side), the hook:

1. widens the box to the right by the shortfall, every control keeping its client
   position;
2. widens every button of the row to the widest need;
3. re-flows the row against the new right edge, with the gap Windows used;
4. re-centres the box about its old centre, within its own monitor's work area.

Nothing else moves. The hook runs the same re-flow again after the box handles
`WM_DPICHANGED`. The box's own text is Windows' layout and is not touched.

**DPI changes [C2].** On `WM_DPICHANGED` the list, entry and progress dialogs take
their new position from the rectangle Windows suggests (lParam), measure again at
the new DPI, lay out again, and keep the result inside that monitor's work area. [B7: In this order: the work area and the cap are those of the monitor the
suggested rectangle lies on (`MonitorFromRect`); the controls are placed first, then
the window in one move to the suggested corner at its fitted size, moved only as far
as it must to lie inside that work area. The first build resized the window where it
stood before moving it, so a dialog dragged between screens of different scaling
could be clamped back onto the screen it was leaving (OUT-01, CON-02). A dialog
dragged across the edge now moves wholly onto the new screen when its DPI changes.]

**Never take the keyboard [C8, N14].** Every resize or move of a dialog window
after it is shown uses `SetWindowPos` with `SWP_NOACTIVATE|SWP_NOZORDER` (and
`SWP_NOMOVE` unless the rules above move it), never `ShowWindow`,
`SWP_SHOWWINDOW`, `SWP_HIDEWINDOW` or a hide and show of the dialog window. Child
controls inside it are shown with `SW_SHOWNA` and hidden with `SW_HIDE`. So a
growing progress window never takes the keyboard from what the person is doing
(R39, MA-1114).

**Fallbacks [C9, N4].** A dialog whose work area cannot be read when it is
created, or whose cap is then smaller than upstream's window at that DPI, or whose
first measurement of non-empty text fails or returns 0, is created with v0.10.12's
layout and style, ellipsis label included, and keeps them. After that, a layout
whose reads or measurements fail keeps the dialog's last layout. [B7: At the same DPI it does,
as at opening when the second measurement fails; at another DPI, whose fonts are
already set, the dialog falls back to upstream's layout and label style for good; a
progress text that cannot be measured keeps the last layout and is measured again
with the next `Text` (CON-01, OUT-04).] The floors (241,
75, upstream's heights) always win over the cap. The message-box hook changes
nothing unless every read succeeded, and widens the box before it moves any
button.

`fit.go` holds the arithmetic as pure, total functions [C9]: given the measured
sizes, the DPI, the system metrics and the work area, all in device pixels, it
returns every rectangle, the sideways extent, which prompt control shows, and
whether upstream's layout is kept. A DPI of 0 or less is read as 96; empty text
takes one line and zero rows give upstream's list height, each at the dialog's own
widths (241 at least for a list or an entry, 400 for a progress dialog); nothing
divides by a measured value; nothing indexes a slice that may be empty. The
`_windows.go` files only measure and apply.

### 4.4 What stays as it is

What a dialog returns and when (P2): the list's items, their order and their
mapping back by index; the keyboard (arrows, Home/End, Enter, Escape, Tab), with
the focus kept off a scrolling prompt (§4.3); default items; `DisallowEmpty`'s
greyed OK; the close box; `zenity.Context`; the hook's icon and title work.
`internal/dialogfront` still sees the same windows: they are laid out before
`ShowWindow`; the progress window's later growth and a message box's re-flow are
size changes it does not watch; and a scrolling prompt is a child control, not a
window it acts on.

### 4.5 Build order

Each slice is its own commit, and each fix is proved fail then pass:

1. The untouched copy, the pins and their recorder, the replace, the
   `.gitattributes` line, MA-1468, the main-module runner for the copy's tests and
   vet, the notices line, and the govulncheck steps (§7). The patch then reads as
   a plain git diff against this commit [C12].
2. `fit.go` and MA-1469.
3. The list, entry and progress layouts, the scrolling prompt, and their MA-1470
   cases.
4. The message-box re-flow and its MA-1470 case [C6]. It is a different technique
   from slice 3, sharing only the measuring helper, so it is built and checked on
   its own. It ships in the same release because the clipped "Open the archive"
   is on every backup's summary, and each release costs one adversarial pass, one
   friction review, one Windows walk and one CI round.

## 5. Tests and catalog rows

New rows (numbers as of this draft): **S86** (Windows dialogs show all their
words) and:

- **MA-1468 (U, every OS, main module, `internal/thirdparty`) [C12 to C16, N1, N2,
  N5, N12].**
  - go.mod requires zenity v0.10.12 and replaces exactly that version with
    `./third_party/zenity`;
  - the pins' 131 upstream lines reproduce
    `h1:o4SErDa0kQijlqG6W4OYYzO6kA0fGu34uegvJGcMLBI=`, written in the test and in
    UPSTREAM.md, computed as the go command makes an `h1:` hash: one line
    "sha256  github.com/ncruces/zenity@v0.10.12/path" per file, in file-path
    order, hashed with SHA-256, base64 [C12];
  - every copied file matches its line; every patch file matches its record, the
    record's upstream hash is its original's line, and the record names its lab
    row with a result that reads pass;
  - the files present are exactly the pins' copied and patched files plus
    `fit.go`, `fit_test.go` and `UPSTREAM.md`; the walk ignores only names that
    are never source (OS litter, `util.IsLitter`; Go's `*.test`, `*.test.exe`,
    `*.out`, `*.prof`), so a developer's machine stays green; the left-out files
    are absent; a file that differs from its line only by carriage returns is
    named as such;
  - `.gitattributes` holds `third_party/zenity/** -text`;
  - every patch file builds for Windows only: its build constraints match
    GOOS=windows and none of darwin, linux, freebsd, netbsd, openbsd and
    dragonfly (`go/build`'s `MatchFile`); `fit.go` is the one non-upstream file
    those builds include;
  - each patch file imports exactly its upstream original's packages plus the
    ones its record lists; `fit.go` imports nothing; neither declares an `init`
    function; a patch file adds no package-level variable set by a call except
    `zsyscall_windows.go`'s generated `NewProc` lines, and `fit.go` none.
  - [B9, from the adversarial pass] the eight patchable files are named in MA-1468's
    own table, each with its original's sha256, imports and variables set by a call:
    a record must agree with the table and a patch file may import nothing else; no
    patch file nor `fit.go` carries a compiler directive but build constraints; a
    pins file with two records for one file does not parse (INS-04);
  - [B9] `internal/win/zsyscall_windows.go` is exactly what upstream's generator
    (`mkwinsyscall`, at the copy's x/sys v0.17.0) makes from the `//sys` lines, and
    the exemption for the generator's lines matches their two shapes exactly
    (INS-01);
  - [B9] the go command resolves zenity to the copy (`go list`, so a `go.work` or a
    vendored copy cannot take its place), and nothing is vendored (INS-02);
  - [B9] each of MA-1469's tests reports a pass in the copy's `go test -json` run,
    and `fit_test.go` is there and builds on Windows, macOS and Linux (INS-05);
  - [B9] the pins keep the `lab-copy` hash of the copy's built files (all but
    `fit_test.go` and UPSTREAM.md) that the last passing MA-1470 run was built
    from, written only from that run's receipt, so a patch file or `fit.go` changed
    since turns MA-1468 red (INS-03);
  - [B9] both workflows run the published-module scan on the whole module and, for
    Windows, on `./cmd/mailarchive`, the program Windows ships, and the release
    checks the copy again just before it builds (INS-06, INS-07).

  Positive twin first: a scratch tree made from the pins passes. Then each of
  these turns it red: one byte changed in a copied file; a patch file changed
  without its record; an extra file; the `-text` line removed; an import added to
  `fit.go`; a patch file given a build constraint that admits linux.
- **MA-1469 (U, the copy's own tests in `fit_test.go`) [C1, C3, C4, C9, C15, C21,
  N4].** Run by a main-module test (with MA-1468) that starts
  `go -C third_party/zenity test ./...` with `GOFLAGS=-mod=readonly` and
  `GOWORK=off`, so it never writes into the copy, and fails on a non-zero exit; so
  the unfiltered `go test ./...`, release.yml's gate and the covers map agree. The
  same test runs `go -C third_party/zenity vet ./...` for GOOS windows, darwin and
  linux and fails on any finding except one: upstream's in
  `internal/win/zsyscall_windows.go`, in `SHBrowseForFolder`, a function the patch
  does not change ("possible misuse of unsafe.Pointer", seen 2026-11-18), matched
  by function and message, not line. For the GOOS=windows run only (the file builds
  for nothing else), it also fails if that finding disappears. This is a new check
  with one exception, not a suppression of an existing one: no gate vets zenity
  today; the exception is the operator's to accept (the review's owner notes). The
  nested run uses upstream's own dependency versions (x/sys v0.17.0, x/image
  v0.15.0); the ubuntu job's existing Windows build of the main module compiles
  the patched code against the shipped versions. If the frozen versions ever stop
  building, what to do is the operator's call. `fit_test.go`'s cases:
  - rows that fit: the extent equals the text area, no sideways bar, the list as
    wide as its widest row; a list of 10 or more rows that all fit shows no
    sideways bar;
  - rows past the cap: the list at the cap, the extent at the widest row, the list
    taller by the sideways bar;
  - the 241 and 75 floors;
  - prompt lines up to 400 widen; longer ones wrap; every prompt label's height
    (list, entry and progress) is the wrapped height plus the slack; an
    unbreakable word widens the content up to the cap;
  - an over-tall list prompt: the dialog at the cap, the scrolling prompt's
    rectangle is the room, nothing past the cap; its twin: a prompt that fits stays
    a label at its wrapped height;
  - buttons fit their labels and stay equal and right-aligned;
  - an entry's starting text widens the content up to the cap;
  - DPI 96, 120, 144, 168 and 192, and odd measured sizes (70 px at 144 DPI): every
    rectangle is at least the measured size it holds;
  - the progress dialog opens one line high at 400; its label grows downward,
    never shrinks, and stays inside the work area;
  - nothing exceeds the cap;
  - totality: empty text, zero rows, DPI 0, zero and negative measurements, a 0x0
    and a tiny work area give no panic; empty text and zero rows give the dialog's
    own floors (§4.3); a failed measurement or an unreadable work area gives
    upstream's layout.

  Positive twins first: the list and entry dialogs and their buttons come out at
  upstream's widths, control sizes and button positions for upstream's short words;
  the progress dialog, whose short-text size changes on purpose, comes out 400 wide
  with upstream's heights [C21]. In each, the prompt label is a quarter line taller
  than upstream's 16 (the slack, §4.3), and what is below it moves down by that much
  [B2].
- **MA-1470 (L, Windows lab: the office test PC, one screen at 150 %, env-gated
  like MA-1113) [C1 to C10, C20, N9, N10, N11].** Real dialogs opened through the
  public API in the main module, measured from outside with Win32 by a
  per-monitor-aware thread. [B7, B9: It also checks that a DPI change puts a dialog at the
  corner Windows suggests when that lies inside the work area, that a progress dialog
  shrinks for neither a word wider than 400 nor a shorter text, and a typical warning
  health view as the wizard words it; when every MA-1470 test passes in a program
  built with the copy's `lab-copy` hash, it writes the receipt the pins' recorder
  requires; with `MAILARCHIVE_LAB_SHOTS` set it saves the friction review's
  screenshots, naming the copy that drew them.] Inputs: the walk's rows; a hostile 80-character name;
  right-to-left and zero-width text; a row of 400 characters; a prompt holding a
  120-character path with no spaces, and one holding a word of about 300
  characters with no spaces (past the cap) [N9]; the health view of a scheduled
  archive at its longest (WARN or RED with reasons, 20 gap lines, Windows paths); a
  question with a one-word text and the longest renamed labels MailArchive uses;
  Desktop's "browser did not open" entry with its link [N10]; an end-of-backup
  summary for three or more Thunderbird accounts. Checks:
  - fit: each row fits the list's client width, or the extent covers its measured
    width, and the sideways bar shows exactly then; each prompt's measured width
    fits its label and its height fits with the slack; the scrolling prompt's line
    count (`EM_GETLINECOUNT`) and a scroll to its end reach its last line; each
    button's label fits its button, and for the one-word question the box grew
    [C20]; the link entry's first and last characters lie inside its box
    (`EM_POSFROMCHAR`) [N10]; the patch's measurement of each non-ASCII label
    equals an outside measurement;
  - outside the measuring model: each label and button is captured
    (`PrintWindow`), and its ink reaches its last text line and stops inside its
    right edge (this also checks the 300-character word is broken, not cut);
  - geometry: every control lies inside its dialog's client rectangle, no two
    controls overlap, and the window lies inside the work area and within the cap,
    at open and again after a progress `Text` that grows the window;
  - real answers: Enter on the default item returns it; Down then Enter returns
    the next; OK is greyed under `DisallowEmpty` with nothing chosen; `BM_CLICK` on
    each re-flowed message-box button returns that button's answer; `WM_CLOSE`
    cancels; the arrows still move the selection while the sideways bar shows;
    after a click in a scrolling prompt, Enter still returns the default row;
  - threads and focus: `Text` called from another goroutine leaves the window at
    the fit for its DPI; with another window active, and the test having allowed
    any process the foreground (`AllowSetForegroundWindow(ASFW_ANY)`), a growing
    progress window does not become the active window;
  - DPI: a `WM_DPICHANGED` sent to each open dialog leaves it fitted at the new
    DPI; for an open message box, the test first narrows its buttons, then sends
    the message, and requires them widened again [N11]. A real move between
    monitors is not lab-proven: the PC has one screen;
  - resources: `GetGuiResources(GR_GDIOBJECTS)` comes back to where it started
    after 1,000 `Text` calls, and after each dialog kind opens and closes 10 times
    (after one warm-up);
  - records, not asserts: whether Windows cuts the text of the multi-account
    summary box. If it does, that is from before this patch and goes to the owner
    as a follow-up.

  Plus MA-1467's Windows walk again, including the health view of a scheduled
  archive, with screenshots for the friction review that record which program
  version drew each dialog [C22].

**Questions for the friction review, not before code [C1, C22].** On the Windows
screenshots: should the health view wrap wider first, up to the width cap, before
it scrolls (lines of about 190 characters)? Each wizard step now has its own
width; should the wizard's list and entry dialogs share one floor, for example
400? [B6: Answered by the friction review on 2026-11-19
(docs/review-windows-dialog-fit-friction.md): no, the health view keeps wrapping at
400 and scrolls; yes, one floor, the step width of 400.]

**Mutation sweeps [C21, N12].** MA-1469's sweep reaches `fit.go` only, run on the
Mac and in Linux. The measuring glue is swept through MA-1470 on the test PC,
against these mutants: wrong `DrawTextW` flags; a width other than the label's;
the old DPI's font; the scroll bar at the system DPI; no `WS_HSCROLL`; no
`SWP_NOACTIVATE`; measuring on the caller's goroutine; no focus hand-back. Each
mutant is a test program cross-built outside the copy with `-o`, run on the PC one
at a time while its desktop is free (about ten programs in one session). Not
mutation-proven: a real DPI change between monitors, several monitors, and growth
over hours. The 2026-11-19 sweep of slice 3 (nine mutants) left two alive, now in
this list too [B3]: the scroll bar at the system DPI (on the test PC's one screen the
system DPI is the window's), and no `SWP_NOACTIVATE` (each dialog is already its
own thread's active window, and a background thread's `SetWindowPos` cannot take
the foreground, so the flag changes nothing a test can see; it stays, by review).

## 6. Rollout and the way back

Ships in 1.7.4 for every platform; only Windows' dialogs change. Nothing is stored
or written differently, so there is nothing to migrate. Desktop opens the same
dialogs inside its long-running process, so its dialogs change only when Desktop
restarts: a 1.7.3 Desktop left running keeps the old ones, since the installer's
close step names only the former program name [C22].

The way back keeps every gate green [C18]: copy upstream's files back over every
patched file (the `internal/win` additions too), mark them all copied again in the
pins, keep `fit.go` and its tests, and ship that as the next patch release. People
get upstream's sizes back only through that release: published releases are never
replaced, and the installer refuses a downgrade. Removing the copy altogether is a
later clean-up, with these gate changes: MA-1468, MA-1469 and S86 retired, the
copy's test-and-vet runner and the govulncheck steps of §7 removed, and go.sum's
zenity lines restored. That is a gate reduction, the operator's to accept when it
is proposed.

## 7. What it costs to keep

The patched files' changes (a few hundred lines of Windows layout), `fit.go` and
their tests are ours to keep.

govulncheck cannot match a version for a module replaced by a folder. It runs in
three places today: ci.yml on `./...`, ci.yml with GOOS=windows on the desktop
program (MA-529), and release.yml's gate. Each also runs against a copy of go.mod
without the replace (with `-modfile`, and a matching go.sum made with zenity's two
lines from the pins), the MA-529 one with GOOS=windows, so zenity's advisories are
matched by version again and still block a release [C17, N3].

Upkeep, written in UPSTREAM.md [C17]:

- The operator moves the copy. The move procedure: download the new version and
  check its `h1:` against the Go checksum database; land the untouched new
  version as its own commit first, so the patch stays a plain git diff [N6];
  re-derive each patch file from its new upstream original, reading upstream's
  changes to it; run the recorder for the new pins; run MA-1470 on the test PC and
  record the results; read §1's reasons again (does the new version fix the sizes,
  and does its Linux list still change?).
- At each MailArchive minor release, read upstream's changes since v0.10.12 and
  record each Windows change as taken or left out, with the reason. Recorded now:
  - v0.10.15's `MB_SETFOREGROUND`: left out (R39: a dialog the person did not ask
    for never takes their keyboard, and `dialogfront` already brings MailArchive's
    own dialogs forward);
  - v0.10.15's `initFileTypes` end-of-string fix: left out (MailArchive's one
    filter, `*.pst` and `*.ost`, fills 12 of 16 places, so a zero follows the
    string today; that count comes from how Go grows slices, so it is re-checked
    when a filter changes or the Go toolchain moves) [N16];
  - v0.10.15's `initFilters` change (`file_windows.go:502`, the end of the legacy
    dialog's filter string): left out (only the legacy file dialog that zenity
    falls back to reads it; re-checked if MailArchive's picker is ever seen
    falling back) [N7];
  - v0.10.15's multi-select loop fix: left out (MailArchive never selects several
    files);
  - v0.10.15's regenerated `zsyscall_windows.go` (`syscall.Syscall` became
    `SyscallN`): left out (the same calls; taking it would bury the patch's
    additions in a whole-file diff, C11) [N7].
- MA-1470 is run again after each Windows feature update, because the message
  box's layout is Windows' [C6].

The change will be offered upstream (an issue or pull request to ncruces/zenity
adding Windows sizing that fits its text), posted only with the owner's OK. If
upstream takes it, the copy goes by the clean-up in §6.

## 8. Security

No new process, privilege, file, network use or setting. The new calls are listed
in §4.2: they measure text, read monitor and window geometry, and size and move
this process's own windows. The message-box hook still acts only on dialogs of its
own thread [C11].

The text measured is the text upstream already drew. Mailbox names reach the
picker cleaned and capped at 80 characters (MA-1198, applied through MA-1462);
any other text is bounded in width by the cap, and a too-tall list prompt scrolls,
so long or hostile text cannot push other lines out of reach or make a dialog
larger than the cap [C1, C21]. [B10: Except that a prompt line, row, label or starting text that
measures 0 wide (only invisible characters) gives the dialog upstream's layout, as
§4.3's Fallbacks say; MailArchive's callers never pass one: mailbox names lose
invisible characters (`state.CleanFolderName`), and every other text is MailArchive's
own (OUT-02).] Measuring cannot exhaust GDI objects (§4.3,
MA-1470's count), and the arithmetic cannot panic (§4.3, Fallbacks) [C9, C10].

The copy is a supply-chain change. The pins tie every unpatched file to the
published module's hash, every patch file to a lab-checked record, [B9: and to MA-1468's
own table, the generated file to its generator, the build to the copy, and the lab
record to the files it ran on,] [B12: The receipt shows the files were consistent, not that a lab run took place: someone with commit access can write one, as they could write the lab text before; it stops the honest mistake of recording an old run (adversarial re-verify RV-10).] and the copy's
contents, imports and build targets to a fixed list (MA-1468) [C12 to C16, N1, N5];
govulncheck still matches zenity's advisories, in the three places it runs (§7)
[C17, N3].

## 9. Claims, labelled [C11, C21]

- P1: matches the `DrawTextW` model on the screens MA-1470 runs on (the test PC,
  one screen at 150 %), and words are seen whole in the friction screenshots;
  conditional elsewhere on Windows measuring and drawing text the same way, which
  is what `DrawTextW` is for, and on monitors of one DPI until a test changes DPI
  for real. A message box's own text is outside P1. GitHub's Windows runner builds
  and starts the wizard's tests, which use fake dialogs and open no real one, so
  none of them reaches the patched code beyond compiling it; MA-1470 is lab-only.
  (That runner's job is red today for reasons outside this change: in run
  44355838119 on main, the wizard package panics in
  `TestAddArchiveSaysWhetherTheCopyFinished` and `internal/desktop` times out.)
- P2: diff review plus MA-1470's real answers (§5).
- P3: proven by MA-1468 on every OS, for the source (every patch file builds for
  Windows only); the shipped programs differ only in build information.
- P4: proven by MA-1468: the copy matches the pins, and the pins match the
  published module. A version move is forced to re-pin each patch file; the
  re-read is the move procedure's step.

## 10. Changes from revision 1

The 22 conditions (folded in revision 2, completed here):

| Condition | Where | Change |
|---|---|---|
| C1 | §2 P1, P2; §4.3 List dialog; §5 MA-1469, MA-1470, friction questions; §8 | A too-tall list prompt scrolls in a read-only text box; P1's limit restated; the message box's text outside P1; "wrap wider first" sent to the friction review |
| C2 | §4.3 (monitor, cap, centring, outer size, progress, message boxes, DPI changes); §5 MA-1470 | Window geometry: the window's own monitor and work area, outer size, growth and re-centring rules, the suggested rectangle on a DPI change |
| C3 | §4.3 Units, Measuring, Prompt labels, Progress dialog; §5 MA-1469 | Device pixels, rounding up; a quarter line of slack on every prompt label, the progress one included; unbreakable words; `DrawTextW`'s count |
| C4 | §4.3 List dialog; §5 MA-1469, MA-1470 | One rule for the list's width and sideways extent; the list grows by the sideways bar |
| C5 | §2 P1; §4.3 Entry dialog | The entry's starting text widens the dialog up to the cap |
| C6 | §3; §4.3 Message boxes; §4.5 slice 4; §5 MA-1470; §7 | Re-flow again after a DPI change; a button's need defined; why leaning on `MessageBox`'s layout is acceptable; its own slice |
| C7 | §4.3 Progress dialog; §5 MA-1470 | Measuring and resizing on the dialog's own thread, by a private message |
| C8 | §4.3 Never take the keyboard; §5 MA-1470 | `SWP_NOACTIVATE` on every resize after showing |
| C9 | §4.3 Fallbacks, `fit.go`; §5 MA-1469 | Total arithmetic; upstream's layout kept when a read or measurement fails |
| C10 | §4.3 Measuring; §5 MA-1470 | DC and font release rules; the GDI object count |
| C11 | §4.2; §8; §9 | Every Win32 call and constant listed, with optional calls and fallbacks; how the stubs are added; "monitors of one DPI" |
| C12 | §2 P4; §4.1 The pins; §4.5 slice 1; §5 MA-1468 | Pins of all 131 files tied to the module's `h1:` (computed as the go command does, in file-path order), kept the MA-331 way; the untouched copy committed first |
| C13 | §4.1; §5 MA-1468 | `third_party/zenity/** -text`; the walk's ignored names; the carriage-return message |
| C14 | §2 P4; §4.1 The pins; §5 MA-1468 | Each patch file's record: upstream original, our version, the lab run it passed |
| C15 | §4.1; §5 MA-1469 | The copy's tests and vet run from the main module's tests; Go 1.20 rules; go.mod and go.sum never patched |
| C16 | §2 P3, P4; §4.1; §4.2; §5 MA-1468 | The exact list of added files; imports pinned; no `init`; P3 restated |
| C17 | §7 | govulncheck against a go.mod without the replace; owner, move procedure, re-check at each minor release; upstream changes taken or left out |
| C18 | §6 | A way back that keeps every gate green and restores every patched file; the clean-up as the operator's later call |
| C19 | §4.1 | THIRD-PARTY-NOTICES.md line |
| C20 | §5 MA-1470 | Cases that reach every branch, the box that grew included; a check outside the measuring model; real answers |
| C21 | §3; §5; §8; §9 | Citations (MA-1198, MA-1462; MA-1111, MA-1117); the Windows runner sentence; P1's and P2's labels; MA-1469's twins; the mutation sweeps' reach |
| C22 | §2 P1; §5 MA-1470, friction questions; §6 | Desktop changes on restart; screenshots record the version; the sideways bar's keyboard limit; the one-floor question |

The fold check of revision 2 (its points folded here):

| Point | Where | Change |
|---|---|---|
| N1 | §2 P3; §4.2; §5 MA-1468 | MA-1468 checks every patch file builds for Windows only |
| N2 | §4.1 The pins; §4.2; §5 MA-1468 | Imports: upstream's plus a listed set; the package-variable rule allows only the generated `NewProc` lines |
| N3 | §7; §8 | govulncheck without the replace in all three places it runs, the MA-529 one for Windows |
| N4 | §4.3 Measuring, Progress dialog, Fallbacks, `fit.go`; §5 MA-1469 | Empty text is not measured; a fallback is decided when a dialog is created, and a later failure keeps the last layout; one rule for empty text in both MA-1469 cases |
| N5 | §4.1 The pins; §5 MA-1468; §8 | Each record names the lab row that proves the file and a result that reads pass; "reviewed" dropped |
| N6 | §7 | The move procedure lands the untouched new version first |
| N7 | §7 | v0.10.15's `initFilters` change and regenerated syscall file recorded as left out |
| N8 | §2 P1 | P1 names the fallbacks |
| N9 | §5 MA-1470 | A word of about 300 characters with no spaces, past the cap |
| N10 | §5 MA-1470 | Desktop's link entry: first and last characters inside the box |
| N11 | §5 MA-1470 | The message-box DPI case narrows the buttons first |
| N12 | §4.1; §5 MA-1468, mutation sweeps | `*.test.exe` ignored; test programs built outside the copy |
| N13 | §4.2 | `SWP_NOACTIVATE` is already defined; constants are added only if missing |
| N14 | §4.3 Entry dialog, Never take the keyboard | The keyboard rule is about the dialog window; child controls use `SW_SHOWNA`; the entry has no scrolling prompt |
| N15 | §3; §4.3 Message boxes | The hook reads and re-flows every button of the row |
| N16 | §7 | The "12 of 16" re-check also when the Go toolchain moves |

Build notes (2026-11-19), found while building `fit.go` and recorded here for the
adversarial pass:

| Note | Where | Change |
|---|---|---|
| B1 | §2 P1; §4.3 Buttons | `fit_test.go`'s sweep found a label wider than the cap pushing its button outside the dialog. Buttons now share the content width when the labels need more, and P1 states the limit: such a label is cut |
| B2 | §5 MA-1469 | The positive twins: a prompt label is a quarter line taller than upstream's (C3's slack), so the twins hold upstream's widths, control sizes and button positions, with what is below the prompt moved down by the slack |
| B4 | §5 MA-1470 | Slice 4 on the test PC: the message-box cases pass, and its 2 mutants (no re-flow, no rerun after a DPI change) fail. The lab test now waits until each dialog is shown before measuring and reads a rectangle twice before calling it unreadable (two runs had read a dialog before its layout, once each); the summary-box record measures the text without its trailing blank lines (it first reported a cut that was only those lines). Runs 6 to 8 in a row: all pass |
| B5 | §4.3 Message boxes; §5 MA-1470 | The re-walk of the real wizard found the renamed buttons 1.5 times too wide (the IMAP question's two at 340 pixels; the summary's at 251 for a 167 need), which MA-1470 had not checked. Diagnostics on the test PC showed that inside MessageBox's WM_INITDIALOG the hook read every rectangle at 2/3 of its size while the labels measured true. The re-flow now runs per-monitor DPI aware (restoring the thread's context after), and MA-1470 fails any button wider than the row needs: red on the unfixed build (314 and 251 wide), green on the fix |
| B3 | §5 Mutation sweeps | Slice 3's sweep on the test PC killed 7 of 9 glue mutants; the scroll bar at the system DPI and no `SWP_NOACTIVATE` are listed as not mutation-proven, with the reasons. The lab test's own measuring thread must be per-monitor DPI aware (§5 says so): the first run, which was not, read every size scaled down by 1/1.5 |

Also corrected in revision 2: the progress dialog's 400 is about 75 characters a
line at 150 %, not about 60 (the rescue's measurement of the screenshots, an
estimate).

The adversarial pass and the friction review (2026-11-19; docs/review-windows-dialog-
fit-adversarial.md and docs/review-windows-dialog-fit-friction.md):

| Note | Where | Change |
|---|---|---|
| B6 | §4.3 List dialog, Entry dialog; §5 friction questions, MA-1469 | The friction review's answer: the wizard's list and entry dialogs share one width, the step width of 400, so a step whose words need less no longer has its own width (its F1: OK moved about 160 pixels between steps); it gives way to the cap, and 241 stays the floor that wins over the cap. The health view keeps wrapping at 400 and scrolls. MA-1469's twins now hold upstream's heights and control heights at 400 wide, OK at 254 and Cancel at 337 |
| B7 | §4.3 DPI changes, Fallbacks; §5 MA-1470 | Adversarial findings OUT-01 and CON-02 (one defect, LOW): a DPI change reads the monitor of the suggested rectangle, places the controls, then the window in one move; MA-1470 checks the window goes to the suggested corner (a mutant that ignores it fails seven cases), though which monitor's work area it uses cannot be told apart on a one-screen PC. CON-01 and OUT-04 (INFO): the fallbacks at a failed measurement, by mechanism only (no failure can be caused in the lab) |
| B8 | §4.3 Prompt labels, Progress dialog; §5 MA-1469, MA-1470 | CON-03 and OUT-05 (INFO): the progress dialog's width is kept with its height, re-based rounding up (MA-1469: a round trip never shrinks the floor). The lab showed Windows breaks a word wider than the label inside it, so the width does not change in practice: MA-1470 checks the dialog does not shrink, and the glue mutant that forgets the width survives, as it must |
| B9 | §5 MA-1468, MA-1470; §8; UPSTREAM.md | INS-01 to INS-07: MA-1468's own table of patchable files, the generated file regenerated, the build's resolution checked, MA-1469's tests each required to pass, the lab run's receipt and `lab-copy` hash, the scans wired and pointed at the program Windows ships, and the release's re-check before it builds. Thirteen mutants of the check and one of the runner each turn it red |
| B10 | §8 | OUT-02 (INFO): §8's sentence on hostile text names the zero-width exception, which §4.3's Fallbacks already specify; MA-1469 pins it |
| B11 | §4.3 List dialog (a prompt too tall for the room); §5 MA-1469 | The friction re-check's G1: the typical warning health view scrolled with its reasons out of sight above an action list nine rows tall for three actions. When a list's prompt is too tall for the room, the list first gives up its empty rows, down to the rows it holds (a line each, half a line spare for the list box's rounding to whole rows, its client edges), and the prompt takes that room; it scrolls only if it still does not fit. Ordinary steps keep upstream's list height. MA-1469: 27 of 27 mutants red, three new |
| B12 | §5 MA-1468, MA-1470; §8; UPSTREAM.md; release | The re-verify of the fixes (63a70d5, 8bfe68b; report's last section): go vet's allowed finding is read with either path separator, so Windows CI is not red for it (RV-01); a lab run filtered to some subtests or skipping some writes no receipt, and every lab run first deletes an earlier receipt (RV-02, RV-03); the `lab-copy` hash covers the lab test too (RV-03); one command hashes, builds and re-hashes the lab program (RV-04); `go list`'s check refuses an `-overlay` or `-toolexec` in GOFLAGS and build-release.sh refuses any GOFLAGS (RV-05); the scan and re-check steps may not continue on error nor be followed by `||`, and the release's re-check must name tests that exist (RV-06, RV-07); a message box's buttons are placed before the box (RV-08); a list falling back to upstream's layout drops its sideways extent (RV-09); §8 states what the receipt proves (RV-10) |

