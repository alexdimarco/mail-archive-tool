# L-A10 — does Thunderbird on Windows honour "start minimized, do not activate"? — 2026-10-31

**Answer: yes.** Started with `STARTF_USESHOWWINDOW` and `SW_SHOWMINNOACTIVE` (7),
Thunderbird's window was minimized from the first 100 ms sample in every trial and never
took the keyboard, even after more than 200 s without input, when today's start took the
foreground every time. Run by the lead (an agent) on its own, with the owner away; the owner
had approved the Windows lab ("lets try windows - I want it shipped in this version").

## Set-up

Same VM, Thunderbird and profile as `presync-attended-start-windows-run1-2026-10-31.md`
(Windows 11 on ARM, amd64 programs emulated — **timings are emulated**; Thunderbird
153.6.0esr; profile `default-esr`, one Gmail IMAP account). `ForegroundLockTimeout` on the
VM: 200000 ms (Windows' default).

- `spike.exe` (lab kit, `~/repositories/.winlab/ma918/tools/spike/main.go`, not product
  code), started from Windows Terminal (`wt.exe -- spike.exe …`) so it holds whatever right a
  terminal-started Desktop holds. After a wait it starts Thunderbird one of two ways and never
  touches its window:
  - `plain`: today's product start — `exec` with `CREATE_BREAKAWAY_FROM_JOB`, as
    `buildLaunchCmd`;
  - `minnoactive`: `CreateProcess` with `STARTF_USESHOWWINDOW` + `SW_SHOWMINNOACTIVE`, same
    breakaway flag.
- The separate observer (100 ms) and, in the typing half, the typer (brings Notepad to the
  front with an Alt tap, then types a middle dot every 200 ms while Notepad has the keyboard).

## Results

| Trial | Start | Input before the start | Thunderbird's window | Keyboard (foreground) |
|---|---|---|---|---|
| 1 | plain | typing | **shown** 1100×642, stayed shown | Notepad throughout (143 typed) |
| 2 | minnoactive | typing | **minimized** from the first sample | Notepad throughout |
| 3 | plain | typing | shown, stayed shown | Notepad throughout |
| 4 | minnoactive | typing | minimized from the first sample | Notepad throughout |
| 5 | plain | typing | shown, stayed shown | Notepad throughout |
| 6 | minnoactive | typing | minimized from the first sample | Notepad throughout |
| idle1 | plain | **none for 230 s** | shown | **Thunderbird took it** (11:40:49.6) |
| idle2 | minnoactive | none for 230 s | minimized from the first sample | stayed with the terminal |
| idle3 | plain | none for 230 s | shown | **Thunderbird took it** (11:49:25.0) |
| idle4 | minnoactive | none for 230 s | minimized from the first sample | stayed with the terminal |

## What it explains

The keyboard steals in MA-918 run 1 (runs 5 and 5b) happened in scripted runs where nobody
had typed for hours: past `ForegroundLockTimeout`, Windows lets a new window take the
foreground, and today's start shows one. While a person is typing, neither start took the
keyboard. The `SW_SHOWMINNOACTIVE` start shows no window and activates nothing in either
case, so it also removes the flash (0.1–0.4 s in run 1).

## Slow start: a Primary Password (trial pp1, 12:47, the owner present)

The owner set a Primary Password in Thunderbird, then the `minnoactive` start ran (the typer's
bring-forward did not take this time, so no one was typing). At 12:47:51.3 a 361×175 window
"Password Required - Mozilla Thunderbird" was shown — the show state applies to the main
window, not to this prompt — and at 12:47:52.8 **it took the foreground**. The owner typed the
password; at 12:47:57.9 the prompt closed and the main window came up minimized, not active.
Thunderbird asks for the Primary Password at every start, so every sync start meets it.

**Owner ruling, 2026-10-31 (verbatim: "accept the password window"):** with a Primary Password
set, the password prompt coming to the front and taking the keyboard is accepted — the person
has just clicked Run now and must answer it, and keys typed into a password box cannot act on
mail (the main window stays minimized and inactive). It does not block the Windows entry. The
guide says Thunderbird asks for it each time a backup starts Thunderbird.

## Next

The product start becomes the `SW_SHOWMINNOACTIVE` start (MA-923); then MA-918 is re-run on
that build.
