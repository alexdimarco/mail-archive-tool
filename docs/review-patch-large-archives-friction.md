SHIPPABLE_WITH_NOTES

# Friction review: large-archives patch

- **What was reviewed:** commit be3dac0 on the branch patch-large-archives, compared with v1.5.2.
- **Result:** no HIGH or MED friction on Windows or macOS.
- **The notes:** four LOW points, all present in v1.5.2 too (the patch didn't cause or change them), plus the walks that are still owed (listed under "Not walked").

## Platforms walked

Archives used: 20, 2,500 and 20,000 messages, made by the real programs from generated mail. One small archive was also grown past the old 1 MiB record limit (1 MiB = 1,048,576 bytes) while Desktop was open.

| Check | Windows 11 VM (headless Edge, real clicks) | macOS (real Chrome, real clicks) | Linux |
|---|---|---|---|
| Large archive as current shows "Current archive" | works fully | works fully | not walked |
| Large row not current has Switch and Pause | works fully | works fully | not walked |
| Switch, Pause, Resume on a large row | works fully | works fully | not walked |
| "Open an archive folder…" on the 20,000 archive | works fully (real Windows folder dialog) | partly: works, but a stand-in script answered the folder dialog, so the real macOS dialog was never drawn | not walked |
| Archive grows past 1 MiB while Desktop is open | works fully | works fully | not walked |
| No change for a plain folder, a damaged record, or a small archive | works fully | works fully | not walked |
| `mailarchive status -out <folder>` gives the same output in both versions | works fully | works fully | not walked |

## v1.5.2 comparison: was the bug confirmed?

Yes, on both platforms, and it was wider than first described. In v1.5.2:

- **A large archive that isn't current** shows greyed out, with no buttons, and says "Not reachable right now — reconnect the drive or folder and it comes back. Nothing was removed." But the folder is right there.
- **The 2,500 archive made current** stays greyed, and no row says "Current archive". Meanwhile Status & health shows "2500 in manifest", so the page contradicts itself.
- **The 20,000 archive made current** is missing from Your archives altogether.
- **"Open an archive folder…" on a large archive** refuses, saying the record "cannot be read". The record is fine.

With the patch, every one of these shows a normal row. The only warning left is "no schedule is recorded for this archive", which small archives also show.

## Friction findings

All four LOW points are old v1.5.2 behaviour, so none of them blocks this patch.

1. **LOW: notes appear out of sight (Windows and Mac).**
   - What happens: the "folder picker has opened" note and the refusal notes from "Open an archive folder…" appear in the Settings card, about 1,600–1,700 pixels below the button. The person sees nothing happen.
   - Fix: show the note next to the button that was clicked, or scroll to it.
2. **LOW: the switch happens off-screen (Windows).**
   - What happens: after Switch or Open, the page reloads at the same scroll position, so the row turning "Current archive" is above the screen. On the Mac the list happened to be in view.
   - Fix: after a switch, scroll to Your archives, or show "Switched to <folder>".
3. **LOW: slow pages when a 20,000-message archive is current.**
   - What happens: each page load takes about 1.1 seconds on Windows and about 0.7 seconds on the Mac, in both versions. The other cards cause this, not the list.
   - What the patch adds: about 50 ms on the Mac (roughly 8%), and nothing measurable on Windows.
   - Fix: later, keep the current archive's numbers in memory between page loads. Measure again if archives of 100,000 messages or more become common.
4. **LOW: the refusal for a truly damaged record sends the person to Settings, then to Status & health, to repair it.**
   - The wording is unchanged from v1.5.2, and nobody followed that path through.
   - Fix: walk it once, and point the note straight at the repair step.

One cosmetic item, not counted above: on the Mac, opening a folder whose path ends in "/" leaves that "/" in the Archive location box. It doesn't create a duplicate row.

## Not walked

- **Linux:** no Desktop or command-line walk at all.
- **The real macOS folder dialog:** a stand-in answered it on the Mac.
- **The Mail Archive.app bundle on the Mac:** only the command-line program was walked.
- **A visible Edge window on Windows:** Edge ran headless only.
- **Archives not covered:** large encrypted archives, large Microsoft 365 archives, archives of 100,000 messages or more, and archives on a network or removable drive.
- **Real Outlook or Thunderbird mail files:** only generated mail was used.
- **Cold-start timings:** the file cache was warm on both platforms.
- **The three Windows test programs:** they still owe a run.
  - *Correction, the lead, 2026-11-02:* they ran on the lab VM the same day. internal/state passed. internal/archindex failed one new test, a test fault: a bare rename while eight readers looked nonstop. It was fixed in 1fab4e6, then passed 20 of 20 and the whole package passed. internal/desktop failed only on tests that also fail on v1.5.2's own build there: TestTheRecoveryFileIsReadBounded and TestTheForgetPlanShowsThePathAsTheRowNamesIt, both re-run on v1.5.2, plus the known ws_history flake.
- **The existing 2,500 archive in the scratch folder (scratchpad/arch2500):** the Mac walker left it out because a test harness made it and it holds a leftover lock file. The Windows report's Mac status check used it. The Mac walker's own archives gave the same result.

**Smallest check left for the owner:** on a real Windows PC and on the Mac, open the patched Desktop with your largest real archive. Its row in Your archives should read "Current archive", or show "Switch to this archive", and should not be grey. On the Mac, choose the folder through the real "Choose the archive's folder" window.

## Date and basis

- **Date:** 2 November 2026, read from the system clock. The walks ran the same day.
- **Basis:** two walk reports, one for Windows and one for macOS. This record summarises them; I did not repeat any step myself.
