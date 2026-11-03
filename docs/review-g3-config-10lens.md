# Review: G3, `-config` for `schedule`, `pause` and `resume` (design-archive-delete §16), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** No finding is HIGH and none needs an owner ruling. 2 MED and 9
LOW findings; 6 conditions (G3-1 to G3-6 below), each folded into §16 of
`docs/design-archive-delete.md` beside the text it changes, marked "Folded from the 10-lens review
[G3-n]". Build may start.

- Date worked on: 3 November 2026 (the session's date).
- Design reviewed: `docs/design-archive-delete.md` §16 as first written, before any fold, on branch
  `switch-keeps-runjob` at commit f284689 (the note itself not yet committed), sha256
  `df0d89b6a817aaf2751a63a2bd10750debd4eceaba3adb5f45c6053235fe7e0f`.
- The ruling it builds: owner, 2 November 2026, "give schedule, pause, resume a -config option
  (and carry it into the 'Delete again' hint); never read both logs" (§15.10 item 7).
- Procedure: `../assurance-kit/process/design-review.md`, all ten lenses. **One reviewer, not
  five:** the builder who wrote the note ran the lenses afterwards as a separate pass, read-only,
  then acted as skeptic by reading the code at f284689 (list below). This is weaker than
  independent reviewers: the same mind wrote and reviewed, so a blind spot the author has is
  likely shared. No rescue pass was run: it is given surviving blockers, and there were none.
- Register: review synthesis (findings, the reviewer's decisions, and opinion where marked).

## Claims checked against the code (the skeptic's part)

Each was read directly; none was run.

| Claim in §16 | Where read | Result |
|---|---|---|
| `wizard.PauseArchive` and `wizard.ResumeArchive` already take a deletion log path | `internal/wizard/pause.go` lines 38, 88 | Confirmed |
| Resume carries the log into its schedule repair, asked before and after the install | `internal/wizard/wizard.go` `repairSchedule` line 1389 (`ActionRefusal(deletionsLog, …)`), line 1409 (`guardedInstall(deletionsLog, …)`) | Confirmed |
| `schedule -install` passes "" today | `cmd/mailarchive/main.go` line 1456 (`archdelete.GuardedInstall("", j.out, …)`) | Confirmed |
| The terminal's pause and resume pass "" today | `cmd/mailarchive/pause.go` lines 28, 49 | Confirmed |
| The marker is asked first, whichever log is read | `internal/archdelete/actionrefusal.go` line 53 → `markerguard.go` `UnfinishedAt` line 44 (`delmark.Present` before the log) | Confirmed |
| With a log path given, no home folder is needed to find the log | `actionrefusal.go` lines 40-52 (`DefaultLogPath` only when `logPath == ""`) | Confirmed |
| An absent log reads as no deletes | `internal/archdelete/dellog.go` `readLogData` lines 258-262 | Confirmed |
| A log that is a link, a folder or too large refuses | `dellog.go` lines 264-270 | Confirmed |
| `delete-archive -config FILE` uses the log beside `abspath(FILE)` | `cmd/mailarchive/archivedelete.go` line 199 → `internal/desktop/clidelete.go` line 45 → `archivedelete.go` (desktop) `deletionsLogPath` line 275 | Confirmed |
| The Desktop's own log is beside its `-config` FILE | `internal/desktop/launch.go` lines 189-200 (`ConfigPath: cfgPath`, not made absolute) | Confirmed, with G3-2's qualification |
| The delete verbs' hints carry `-config` through `terminalHints` | `cmd/mailarchive/archivedelete.go` lines 548-571 | Confirmed |
| No job verb takes `-config`, so one after `--` is refused | `cmd/mailarchive/job.go` lines 192-235; the only `-config` flags are `setup`'s (`serve.go` line 267), the Desktop's and the three Your-archives verbs' | Confirmed |
| The job form refuses any schedule flag before `--` it does not list | `cmd/mailarchive/main.go` lines 1308-1319 (allow-list `interval`, `at`, `name`, `install`, `remove`) | Confirmed: the note did not say this list must grow (finding L5-1) |
| `pause`, `resume` and `schedule` root usage lines are pinned whole | `cmd/mailarchive/help_v152_test.go` lines 27-28 (pause, resume; MA-939) | Confirmed for pause and resume; schedule's root line is not pinned |
| `encmark.CommandPath` escapes control characters off Windows | `internal/encmark/encmark.go` line 283 (`%q`) | Confirmed |

Not checked by anyone (each needs a lab run): how `filepath.Abs` resolves `D:config.json` when
drive D has no current folder in that terminal; how long `os.Stat` on an offline network `-config`
folder waits before it fails on Windows.

## Findings

| ID | Lens | Section (or OMISSION) | Claimed failure | Verdict | Evidence / reason | Condition |
|---|---|---|---|---|---|---|
| L5-1 | 5 integration | §16.1 (OMISSION) | The note says `-config` before `--` is accepted, but does not name the mechanism: the job form's allow-list of schedule flags would refuse it as a stray job flag ("-config belongs after --") | CONFIRMED (MED) | `main.go` lines 1308-1319 | G3-1 |
| L10-1 | 10 honesty, 1 purpose | §16.2 | "This is the file the Desktop started with -config FILE writes" is conditional: the Desktop does not make a relative FILE absolute, so `-config cfg.json` names the same file only when the terminal is in the folder the Desktop was started from | CONFIRMED (MED) | `launch.go` lines 189-200; `deletionsLogPath` joins `filepath.Dir(ConfigPath)` as given | G3-2 |
| L2-1 | 2 proportionality, 5 integration | §16.2 | Moving the path rule into `desktop.DeletionsLogFor` touches the Desktop's own log path; a slip there changes where every Desktop delete writes | CONFIRMED (LOW) | `deletionsLogPath` trims spaces and falls back to `DefaultLogPath`; both must be kept | G3-3 |
| L10-2 | 10 honesty | §16.2 | "A `deletions.log` that is not in an existing FILE folder reads as 'no deletes'" says something else than meant (an absent log in FILE's folder) | CONFIRMED (LOW) | the sentence as written | G3-4 |
| L5-2 | 5 integration | §16.3 (OMISSION) | `deleteRefusalAtTerminal` is shared with the export and `graph` runs (`main.go` lines 188, 462, 954, 1662, 1855); giving it `-config` must not change what they print | CONFIRMED (LOW) | those callers take no `-config` | G3-5 |
| L10-3 | 10 honesty, platforms | §16.5, §16.6 | Windows is the biggest user group; the note says the shared function is tested on Windows but not which test, nor that the real program's Windows walk is owed | CONFIRMED (LOW) | `cmd/mailarchive` cannot run in the lab VM | G3-6 |
| L4-1 | 4 usability | §16.6 | A person who forgets `-config` gets the default log's answer and can pause or resume a folder whose delete (begun from their `-config` Desktop) stopped before step 6 | CONFIRMED (LOW), accepted as written | The ruling's "never read both logs"; the archive is whole before step 6 (owner ruling Q1); the folder's scheduled runs are still refused by the engine's own check (G2) only when the default log or the marker knows; the note states it | none |
| L4-2 | 4 usability | §16.6 | The root usage (`mailarchive -h`) does not name `-config` for pause and resume, unlike delete-archive's line | CONFIRMED (LOW), accepted as written | MA-939 pins those two lines whole; each verb's `-h` and the README name it. Amending a pinned line is a lead's call, not this slice's | none |
| L4-3 | 4 usability | §16.2 | Refusing `schedule -remove` and the preview on a `-config` whose folder is missing stops work that reads no log | REFUTED as a defect | §16.2 gives the reason (a mistyped flag said once, the same way, wherever typed); nothing is lost: the person retypes or drops `-config` | none |
| L6-1 | 6 security | §16.3 | FILE is echoed into the terminal in the hints; a path with control characters or a bidi override could spoof the line | REFUTED | `CommandPath` uses `%q` off Windows (escapes non-printable runes, U+202E included); Windows file names cannot hold control characters; the person typed FILE themselves | none |
| L6-2 | 6 security | §16.2 | Someone who can write FILE's folder can plant a `deletions.log` that refuses pause, resume and installs, or leave one out | CONFIRMED (LOW), accepted | The same as MailArchive's own log today (the person's own settings folder); a planted log cannot hide a marker (asked first); a log readable by others or not a regular file refuses (fail closed, the G2 settle pass's LOW-3) | none |
| L7-1 | 7 failure domains | §16.2 | A `-config` folder on a network share that is offline is refused as "its folder … does not exist", which is not what happened | CONFIRMED (LOW), not fixed here | Existing words of the delete verbs (MA-1024); for the lead with the other wording items | none |

Lenses with no finding: 3 durability (the verbs write no new file; nothing grows; no new crash
point), 8 migration (an older MailArchive given `-config` on these verbs stops with "flag provided
but not defined", loudly; scripts without `-config` are unchanged), 9 operational cost (no new
dependency or service).

Not findings of this design, noted for the lead (both exist today, without `-config`):
- `resume`'s refusal from the install that follows the unseal (a delete that began between
  Resume's two asks) is printed at the terminal as the refusal alone, losing "the archive is
  writable again, but its backup schedule could not be restored": `deleteRefusalAtTerminal` keeps
  only the refusal's words (`pause.go` line 63). It needs a race to reach.
- The marker-only words' "this computer's deletion log has no record of it" can be wrong about
  where when this computer has two logs (§16.6). The ruling rules out reading the other log to
  tell.

## Conditions (each folded into §16, marked "[G3-n]")

1. **G3-1** (L5-1): the job form's allow-list of schedule flags gains `config`, so `schedule
   -config FILE -install -- …` is accepted; MA-1061 proves both forms.
2. **G3-2** (L10-1): §16.2 says "the same file when FILE names the same file": a relative FILE is
   made absolute against the terminal's current folder, the Desktop's against its own; the hints
   always print the absolute path, so a command copied from a hint is unambiguous.
3. **G3-3** (L2-1): `desktop.DeletionsLogFor("")` is "" (MailArchive's own), and
   `Config.deletionsLogPath` keeps its behaviour exactly (spaces trimmed, the default when no
   config path); the Desktop's existing deletion-log tests pass unchanged.
4. **G3-4** (L10-2): the sentence reads "No `deletions.log` in FILE's folder reads as no deletes".
5. **G3-5** (L5-2): `deleteRefusalAtTerminal` takes the verb's `-config`; the export and `graph`
   runs pass "" and print what they print today (their existing tests, MA-1044 and MA-1048,
   unchanged).
6. **G3-6** (L10-3): `desktop.DeletionsLogFor`'s test runs the Windows spellings (a drive path with
   spaces, a network path) on Windows in the lab VM and the macOS and Linux spellings there;
   `cmd/mailarchive` is vetted with GOOS=windows; the Windows terminal walk with a `-config` path
   holding spaces is owed to the lab rows (MA-1055), stated as pending.

## For the lead

- Words: the three new help and README sentences in §16.7 are lead's words, owner-delegated
  2026-11-02; no refusal gets new words.
- No product ruling is asked. The one behaviour a person would notice beyond the ruling,
  refusing a mistyped `-config` on `schedule -remove` and the preview, is a technical consistency
  call (the delete verbs already do it), recorded above as L4-3.
