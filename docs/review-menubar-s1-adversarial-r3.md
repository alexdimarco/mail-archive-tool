# Adversarial re-check, round 3: menu-bar slice S1, fix round 2

Date: 2026-11-14. Reviewed: branch `menubar` at **be4f3e3**; this round's diff is
`git diff 2ed6a5e..be4f3e3` (fix round 2). Read in the worktree
`.claude/worktrees/menubar-s1-adv3` (branch `wip/menubar-s1-adv3`). Earlier passes:
`docs/review-menubar-s1-adversarial.md` (A1–A9) and `docs/review-menubar-s1-adversarial-r2.md`
(B1–B10). Fix notes: `docs/design-menubar.md`, "S1 fix round 2". Procedure:
`assurance-kit/process/adversarial-review.md` (finder lenses outside / insider / concurrency,
then a skeptic step that keeps only what was reproduced or shown in the source). One agent
played both roles in sequence. This pass attacks and reports; it changes no product code.

**Verdict: NOT CLEAN.** B1, B2, B3, B5, B6, B7, B9 and B10 are closed for the attacks they
named; B4's acceptance holds as worded; B8 is narrowed but still open (C2). New: **0 critical,
0 high, 2 medium, 3 low**. 15 suspected attacks refuted (listed at the end). The new `quit`
verb, the hand-off's `can-quit`/`quit` and the rewritten upgrade close step held against every
outside-the-account attack tried; the one way in from outside the account's own full rights is
older than this round (C1).

Header facts:
- Toolchains: the Windows lab builds with the repo's pinned go1.26.0 (`go version` in the
  worktree; the lead's 1.26.11 is not installed on this Mac, as round 2 also found); Linux
  probes in Docker `golang:1.26` = go1.26.9 linux/arm64 with `GOTOOLCHAIN=local`.
- Windows: the lab VM "Windows 11" (ARM64, amd64 emulated). **No user was signed in** (Windows
  Update had restarted it), so everything ran as `nt authority\system` in session 0 through
  plain `prlctl exec`; nobody was signed in, no account was made, the VM was not suspended or
  restarted. Own folder `C:\ProgramData\ma3adv lab\` (a path with a space, on purpose; only
  SYSTEM and Administrators can write it, as Program Files), own programs `ma3advw.exe`
  (`-H=windowsgui`) and `ma3adv.exe` built from be4f3e3, probe `adv3probe.exe` (also copied as
  `adv3wrap.exe`, the lab's stand-in for the installed program: it runs `ma3adv.exe` with the
  copy's own `LOCALAPPDATA`). Each script refuses to run if a process of the lab's program name
  is not under the lab folder; only processes these scripts started were messaged or ended,
  plus one `PING.EXE` the stale-record script started. Final clean-up: no lab copy, no task
  named `MailArchive upgrade *`, the lab folder and the planted file removed (`clean.ps1 -All`:
  "lab folder left: False; planted left: False").
- The real `Install-RMM.ps1` was never run whole (it would touch every `mailarchivew.exe` and
  run msiexec): its close step, cut from the file between its `# --- functions ---` and
  `# --- the upgrade ---` markers, was run against lab copies only, as the builder did.
- Lab adjustment, stated: a copy started by SYSTEM writes its record owned by Administrators,
  and the task's SYSTEM token does not count that owner as its own (the builder's "could not
  ask" in session 0). For the end-to-end runs the lab set each lab record's owner to SYSTEM's
  own SID (`icacls /setowner *S-1-5-18`) first. The user-session twin (record owned by the
  user, task as the user) is the L-W1 lab.
- Mac: nothing that can show a dialog, notification or browser ran here; every Go probe ran in
  Docker or on the VM.
- Blob ids at be4f3e3: `Install-RMM.ps1` 2b26bc5, `quitverb.go` 1b1c870, `apploop_windows.go`
  d78fe1f, `front.go` c5c2377, `handoff.go` 63847f2, `guisession_other.go` 0b311c4,
  `childwatch_linux.go` 79d1ee1.
- Probes (not committed): `.claude/adv3/` in the worktree (`desktop_zz_adv3probe_test.go.txt`,
  `instance_zz_adv3probe_test.go.txt`, build tag `adv3probe`, copy one into its package as
  `zz_adv3probe_test.go` to rerun; `adv3probe_main.go.txt`, the Windows probe, build tags
  `adv3probe && windows`, goes in `cmd/adv3probe/main.go`) and the lab share `.winlab/adv3/`
  (`setup.ps1`, `start.ps1`, `lowil.ps1`, `closestep.ps1`, `b3.ps1`, `stale.ps1`, `clean.ps1`).
- Count: **20 suspected / 5 confirmed / 15 refuted**, plus the 10 B re-checks. Of the 5, C2's
  real-world trigger is PLAUSIBLE on top of a mechanism shown in the source.

## Round 2's findings, re-checked

| ID | Verdict | Check |
|---|---|---|
| B1 | **closed** | "Before S1" is now read from the copy's own record (no hand-off actions) or from the MSI's `QuitVerb` value, never from a failed window post; no path of the close step forces a copy that can be asked. VM `closestep.ps1`: the task ran the installed stand-in as the copy's account, `quit -check -end-older -pid 4400` → "would quit now", `-pid 1632` → "busy", nothing forced. A copy at a higher integrity or in another sign-in is reached over loopback (`-pid` → `SessionOf`); one whose record its owner check refuses is "could not ask", exit 3, never forced (the builder's VM run; see C4 for the words). |
| B2 | **closed, residual C4** | No compiled-on-the-fly C# any more; the task is made and read with `schtasks.exe`. VM, the real close step **under Constrained Language mode**: one idle and one busy copy → `code=1618`, both alive, the busy one named; then one idle copy → `code=0`, "Closed MailArchive (process 2372) after it finished its work.", the copy gone, no task left. "Busy", "could not ask" and "closed" are kept apart; but the verb's own reason does not reach the output (C4). |
| B3 | **closed** | VM `b3.ps1`, copy with its wizard open: a posted WM_QUIT → alive, instance window still there (count 1); a lone WM_ENDSESSION(TRUE, CLOSEAPP) → alive; a refused query then WM_ENDSESSION(TRUE, CLOSEAPP) → alive; a second WM_QUIT then a foreign WM_ENDSESSION(TRUE, 0) → alive, window still there. Idle copy: the query allowed (`answer=1`), then its wizard opened, then WM_ENDSESSION(TRUE, CLOSEAPP) → alive (the busy check is made again at the end message). |
| B4 | **accepted LOW, acceptance holds** | MA-1424's amendment and the build note now say `taskkill` without /F closes an open wizard's dialog as Cancel and an idle copy then ends. Not re-run. |
| B5 | **closed** | Name `MailArchive upgrade <GUID>`, made without `/f` (a taken name fails), a protected descriptor `D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;<user>)` inside the definition, its command, arguments and single action read back before it runs. No pre-creation is possible without guessing the GUID. |
| B6 | **closed** | Not re-run: the builder's VM run left a task with `PT2M` and a 15-minute end that Task Scheduler then deleted. Every run here left no task. |
| B7 | **closed** | VM: one idle and one busy copy → the idle one was only asked `-check` and left running, exit 1618 (also under CLM). The race where work starts in one copy after another was closed remains, as the build note says. |
| B8 | **narrowed, open (C2, MA-1439)** | The audit session is gone; logind's own answer from `/proc/self/cgroup` is used, and a process under the user manager gets "" (fail closed). But the environment's `XDG_SESSION_ID` still outranks logind's answer: C2. |
| B9 | **closed** | Docker `TestAdv3Probe_B9FirstForm`: a first-form, not-quiet open at a copy in sign-in 100 → `opened=false link=true`, instance browser opens 0. |
| B10 | **closed (narrowed to the dialog's start)** | Docker `TestAdv3Probe_B10TokenWindow`: with no dialog process the token is set 300 ms in and gone at 2.3 s; an unrelated child started 100 ms in still saw `got=adv3-token-2`, and the token was gone right after it (the first new child clears it, whichever it is). The window is now the dialog's own start, as the build note says. |
| A8 | **open (lab L-W1)** | Not attempted (no signed-in user; a sign-out would end other sessions). |

## New findings

| ID | Sev | Platform | Claim | Check |
|---|---|---|---|---|
| C1 | MED | Windows | A **Low-integrity** process of the account (a browser-style sandbox) reads the instance file's key and uses the signed hand-off: `quit` stops an idle copy, and a quiet `open` hands it a link that opens a full Desktop session. Older than this round; round 1's R4 ("none can read K") does not hold on Windows | PROVEN (VM, as SYSTEM with a token lowered to Low; user-session twin L-W1) |
| C2 | MED | Linux | `XDG_SESSION_ID` from the environment outranks logind's own answer, so an id a sign-in imported into the shared user manager can be carried by launches of every sign-in: B8's "one id for all" leg reopens there | Rule PROVEN (Docker, source); trigger PLAUSIBLE (MA-1439) |
| C3 | LOW | all | `quit -end-older` ends whatever live process a lockless record names: a stale record from an older copy plus a reused pid ends a process that is not MailArchive | PROVEN (VM: a `PING.EXE` ended) |
| C4 | LOW | Windows | The upgrade output loses the verb's own reason ("quit could not ask it (answered 1)"), and a copy that agreed but is still draining after 30 s is reported as "busy: running a backup … or its wizard is open" | PROVEN in the source |
| C5 | LOW | all | An open choice window is not "busy": `quit` (the upgrade) stops a copy while the person is looking at its choice | PROVEN (Docker) |

### C1 (MEDIUM, Windows) — a Low-integrity process reads K and uses the hand-off

**What happens.** The instance file (and its folder) get no mandatory label and no explicit
DACL: `util.WriteFileAtomic0600` sets only the Unix mode, so on Windows the file inherits
`%LOCALAPPDATA%`'s ACL (the account, SYSTEM, Administrators). Windows' default integrity
policy is no-write-up only, so a process of the same account at **Low** integrity can read it.
With K it can send any proven hand-off over loopback (Low integrity does not restrict loopback
TCP): `quit` stops an idle copy (new this round; R52 still holds, a busy copy says no), and an
`open` with `quiet` returns a one-time link whose redemption gives a session for the whole
Desktop page (the archives' mail, and every action the page offers).

**Proof (VM `lowil.ps1`, the lab copy's state folder first given LOCALAPPDATA's kind of ACL:
SYSTEM and Administrators only, inheritance off).**

```
its ACL now: …desktop-instance-s0.json BUILTIN\Administrators:(I)(F) | NT AUTHORITY\SYSTEM:(I)(F)
low child: integrity S-1-16-4096
read ok: pid=5228 dashboard=127.0.0.1:52947 acts=[open choose wizard can-quit quit] keylen=64
act open: shown=false reason="" opened=false busy=false linklen=74 linkprefix="http://127.0.0.1:52947/_open?…" err=<nil>
redeem: status=200 cookies=2 location=""
dashboard GET /s/tw63qo3a5h64p36q63uj3dokpi/ with the session: status=200 first bytes contain MailArchive=true
act quit: shown=true reason="" …
```

The copy (pid 5228) was gone from the process list at the next step. The Low child ran under a
duplicate of SYSTEM's token lowered to `S-1-16-4096`; the access rule is the same for a user's
token lowered to Low, but the user-session twin is not run here (L-W1). Which sandboxes this
reaches: one that runs at Low integrity *without* a restricted token or an AppContainer (those
would also need the account's SID to read the file); the mainstream browsers add one of those,
so the population is small, which is why this is MED and not HIGH.

**Fix, in plain words.** Write the instance file (and the serve file, which holds the same kind
of key) with a Medium mandatory label that forbids reading up, `S:(ML;;NRNWNX;;;ME)`, or put it
in a folder that has that label; then a Low or untrusted process cannot read K. Correct round
1's R4 in the record to say what it was checked against.

### C2 (MEDIUM; trigger PLAUSIBLE, Linux) — the environment's session id outranks logind

**What happens.** `guiSessionFrom` returns `XDG_SESSION_ID` whenever it is a valid field, and
only otherwise asks logind (the cgroup). But a process under the systemd user manager
(`user@<uid>.service`, shared by every sign-in of the account) belongs to no session — logind
itself answers "none" for it, which the new code rightly reads as "" — yet it can still carry an
`XDG_SESSION_ID` in its environment: the user manager's environment is shared by every sign-in,
and a sign-in that imports its whole environment into it (for example `dbus-update-activation-
environment --systemd --all`, which some distributions' X session scripts run, and so an xrdp
sign-in) puts its own id there. Apps the user manager starts afterwards (autostart entries
under systemd, D-Bus-activated apps) then carry that id, whichever sign-in they serve. That is
B8's first leg ("one id for every sign-in", R50 not kept) by another route. The same rule also
lets an env id override a real session scope (a process in `session-4.scope` with
`XDG_SESSION_ID=7` gets "7").

**Proof (Docker `TestAdv3Probe_SessionRule`, the rule only).**

```
a process under the user manager (logind: no session): cgroup rule gives ""
the same process with XDG_SESSION_ID=7 in its environment …: GUISession rule gives "7"
a process in session-4.scope whose environment says XDG_SESSION_ID=7: GUISession rule gives "7" (logind says "4")
```

Whether a real desktop leaks the id this way is MA-1439's lab (pending).

**Fix, in plain words.** Ask logind first: when the process's cgroup is a session scope, use
that id; when it sits under the user manager, answer "" whatever the environment says (logind
says it has no session); use `XDG_SESSION_ID` only where there is no systemd hierarchy at all.

### C3 (LOW) — `quit -end-older` ends whatever a stale record names

**What happens.** For a record with no hand-off actions, the verb checks only that the
record's pid is alive (and, with `-pid`, that it equals the record's), and that the lock's
*holder line*, when readable, names the same pid. It never checks that the lock is **held**
(`lockfile.Release` leaves the line in the file, and its own comment says holders are verified
by locking, never by reading), nor that the process is MailArchive. A copy from before S1 that
crashed or was ended without its clean-up (TerminateProcess — what this very verb and the
script's legacy path do to older copies) leaves such a record; once its pid is reused, `quit
-end-older` ends the new process. MA-1424's promise is "ends it as upgrades always did", and
upgrades ended processes by program name.

**Proof (VM `stale.ps1`).** A record of an older copy's kind (no `acts`, `version v1.7.1`) naming
a live `PING.EXE`, no lock held:

```
ping (not MailArchive) pid=2180 session=0; stale record …\desktop-instance-s0.json
quit -end-older -wait 5s -pid 2180 -> exit 0 : The older MailArchive (process 2180), which cannot be asked to quit, was ended.
ping alive after: False
```

In the upgrade script the pid must also be a `mailarchivew.exe` dashboard, so the reuse there
is improbable; a person running `mailarchive quit -end-older` (it is in the usage line) has no
such filter. The new copy's own start removes a stale record, so the window is from the older
copy's end to the next start.

**Fix, in plain words.** End an older copy only when the lock is held (`LockIsHeld` true) and
the process's program is a MailArchive name (or equals the record's `exe`); otherwise answer
"not running".

### C4 (LOW, Windows) — the upgrade output loses the reason

**What happens.** The task's output is not kept, so when `quit` itself answers 1 the script can
only print "quit could not ask it (answered 1)" (`Get-CouldNotAskReason`). The reasons the verb
knows — an older copy without `-end-older`, its record refused by the owner check (a copy
started as administrator), it did not answer, it is starting — never reach the RMM log, though
B2's fix asked for "could not ask (with the reason)" and README-RMM says "the script prints
which process and why". Also, a copy that agreed and is still draining after `-wait 30s` makes
the verb answer 3, which the script prints as "busy … running a backup or another task, or its
wizard is open … It was left running" — the copy is in fact closing.

**Check.** Source: `Install-RMM.ps1` (`Invoke-AsCopyUser` returns only `LastTaskResult`;
`Get-CouldNotAskReason`; phase two maps `$QuitBusy` to `$busy`). The builder's VM run of a
SYSTEM copy (owner refused) shows the generic form.

**Fix, in plain words.** Have the task write the verb's one line to a file only the copy's
user and SYSTEM can read (or give `quit` a `-out FILE`), and print it; give "still finishing"
its own line in the script.

### C5 (LOW) — an open choice is not "busy"

**What happens.** `quitRequest` refuses only while `busyTask` names work or an open wizard. The
choice window (and the Quit question, and an open Desktop page) are not counted, so the
upgrade's `quit` stops a copy while the person is in the middle of choosing; on Windows the
choice is in the copy's process and vanishes; elsewhere the dialog process goes with the copy's
life. Nothing is written yet, so nothing is lost but the person's click.

**Proof (Docker `TestAdv3Probe_QuitWhileChoiceOpen`).** `can-quit while the choice is open:
shown=true`; `quit … shown=true`; "the copy ended while its choice was on the screen".

**Fix, in plain words.** Count an open choice (`f.choosing`) as busy for `can-quit`/`quit`, or
accept it in MA-1424's words.

## Refuted (the skeptic's record)

| # | Suspected attack | Why it fails |
|---|---|---|
| R1 | Another account sends `can-quit`/`quit` | Needs K: the record is in the account's own folder and refused unless this account owns it. (A Low-integrity process of the *same* account: C1.) |
| R2 | A local user strips a new copy's `acts` so `-end-older` force-ends it while busy | Only the account itself (at Medium or above) can write its record; Low integrity cannot (no-write-up). `MarkClosing` and `Watch` rewrite the remembered record with its `acts`; `launch.go` is the only publisher and always sets them. Whoever can write the record can already end the copy. |
| R3 | Flip or plant `QuitVerb=0x1`/remove it to make the script force new copies | HKLM, administrators only; read in the 64-bit view with `/reg:64`; the MSI writes it as an integer (`0x1`). |
| R4 | `-pid` of another user's process, or a non-MailArchive pid, through the script | The task runs as that pid's own owner (`GetOwnerSid`), which can only end its own processes; a manual run with a foreign pid reads its own folder's file for that session, finds none, "not running". The only way to a non-MailArchive pid is a stale record: C3. |
| R5 | Task Scheduler resolves the installed path unquoted (`C:\Program.exe`) | VM: the stand-in at `C:\ProgramData\ma3adv lab\bin\adv3wrap.exe` ran correctly in every run, and the planted `C:\ProgramData\ma3adv.exe` never ran ("planted … ran: False"). |
| R6 | Pre-create or hijack the task (B5's route) | Random GUID name, no `/f`, protected descriptor, command/arguments/one action read back. |
| R7 | Inject through the SID or arguments into the task XML | The SID must match `^S-1-[0-9-]+$`; the pid is an `[int]`; command and arguments are XML-escaped. |
| R8 | A copy started from a user-writable folder gets its own program run as the user | The task always runs the *installed* program (under Program Files, else exit 3); a copy anywhere is asked over its hand-off; an older one is ended only by its record or, when the install itself predates `quit`, only if it *is* the installed program. |
| R9 | Bypass the Program Files test | `InstallDir` is written by the MSI under HKLM; `..` is refused; the file must exist; a custom install folder fails closed (exit 3), which README-RMM states. |
| R10 | CLM breaks the new close step | VM under Constrained Language mode: 1618 with a busy copy, 0 with an idle one, no task left (B2). |
| R11 | Forge B3's messages in a different order | Refused query then CLOSEAPP; allowed while idle, then busy, then CLOSEAPP: alive both times (VM). |
| R12 | Start work deliberately in the idle-check-then-stop instant to cut a backup | Only a Desktop session (cookie + CSRF) or K can start work, and either can already stop the copy or worse; no outsider can. The accidental race stays the build note's residual. |
| R13 | Spoof the Linux sign-in to make the copy open on its own screen | The launcher's session travels inside a K-signed request: anyone who can send it is the account and could set the variable anyway; a process cannot move itself into another session's root-owned scope. (The honest-mismatch case is C2.) |
| R14 | B9's link reaches the wrong person | The link goes only to a K-proven launcher (the same account) and is shown or opened in that launcher's own session; the copy opens nothing (Docker). |
| R15 | The activation token still leaks to another child | Only to a child started before the dialog's own process (Docker: 100 ms in); it is a one-time token for the same account's compositor, and the dialog normally starts first. Not raised. |

## Notes for the lead

- Highest value first: C1 (a one-line label fix; it also covers the serve file), then C2 (one
  rule swap, and MA-1439 decides the rest), then C3.
- **First proof of the schtasks.exe success path:** the builder's success run predates the
  switch to `schtasks.exe`; this round ran the real close step under CLM through
  `schtasks.exe` end to end (idle copy closed, code 0) and the all-or-none case (1618), as
  SYSTEM with the lab owner adjustment above. The user-session runs stay owed (L-W1).
- Carried, outside this round's diff: the script still ends `mailarchive-desktop.exe`
  unconditionally, and the MSI's `util:CloseApplication` terminates it; a copy or a run started
  under that former name is ended whatever it is doing (the build note says so).
- Catalog effects: C3 and C5 touch MA-1424's quit wording; C4 touches MA-614 and README-RMM's
  exit-3 line; C2 is MA-1439; C1 touches the instance file's promise (CC21) and round 1's R4.
- No probe code is committed.
