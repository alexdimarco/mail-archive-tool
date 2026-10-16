## Lenses: what each found, in one line

1. **Purpose / threat model (16 confirmed):** the claims ran ahead of the mechanisms: "no console at 02:00"
   for wrapped schedules (SE-02), "never silent" on Linux (SE-03), P6's "exactly" against a widened kill test
   (SE-04), "rollback is a file swap" against the MSI's downgrade guard (SE-05), P4 and R33 against a silent
   fallback (SE-14), a remedy and a "one kind per archive" with no mechanism behind them (SE-18, SE-26).
   Held: the forged-descriptor repair rule, no self re-exec, and the size and mtime residual stated as today's.
2. **Focus / proportionality (6):** SE3 is a second design (SE-21); the Linux no-argument row (SE-52) and the
   widened kill test (SE-57) cost more than they buy; SE4 left the former-name code behind (SE-81). The
   skeptics defended the copies (SE-35), the state helper's existence (SE-68) and the mechanism ledger
   (SE-82). Held: two builds of one package is the minimal answer to §1.6 once `AttachConsole` is conceded.
3. **Durability (12):** a same-name install silently changes a schedule's kind (SE-01); identity across copies,
   links, translocation and path spellings (SE-15, SE-16, SE-36, SE-59); exact-name rules against suffixed
   assets (SE-09) and a stdin-only predicate (SE-10, SE-53); a fourth state root (SE-67). Held: atomic job
   files, the bounded activity ring, no format change.
4. **Usability (22):** Repair clicks that convert a schedule (SE-01), remedies that name a refusing command
   (SE-18), the double-clicked console build (SE-08), the wizard with no click launch (SE-28), an upgrade walk
   that hides a context switch (SE-39), texts with no "Do this" (SE-60, SE-61, SE-66), a modal dialog on top
   of stderr in a terminal (SE-51), a second double-click of the `.app` that does nothing (SE-C01).
5. **Integration / seams (18):** SE1 consumed SE2's helper (SE-11); a refactor rode inside the "move" (SE-12);
   former-name copies dropped argument lists into the export verb (SE-06); MA-584 was specified against a CI
   step that does not exist (SE-07); Build-MSI.ps1, payload.py and release.yml were outside every edit list
   (SE-70, SE-71); the catalog edit list was wrong (SE-80); the covers map would go red between slices
   (SE-45). Held: the covers map reads markers textually, so moving packages keeps them.
6. **Security (11):** the archive-held `Job[0]` as the source of a repair's kind (SE-C04); the sibling rule's
   principal where create-file is not write (SE-72); the COM probe in the dashboard (SE-22); "owner-only" for
   a read that checks nothing (SE-19); the no-argument rule as a launch vector for an unnamed server (SE-10).
   Held: a forged `Exe` never runs, SE3's posts sit behind the guard, hostile folder names render as text.
7. **Failure domains (13):** `mailarchivew.exe` as a single point of loss (SE-32); a nightly refusal that
   writes no record, and a breadcrumb with one reader (SE-01, SE-20); a stalled local run holding the slot
   (SE-25); an old engine running under a GREEN status after a half-replaced pair (SE-15). Held: a torn job
   file is refused; a panic releases the lock.
8. **Migration (16):** the upgrade note on every descriptor turns the kind conversion into the prescribed walk
   (SE-01); the MSI downgrade guard (SE-05); the N+1 flag day and the undefined "one release" (SE-33,
   SE-34); RMM kill scope, stale scripts and the stored detection rule (SE-29, SE-41, SE-42); the old
   instance that wins the lock (SE-38); the macOS two-copy layout (SE-40). Held: the legacy spellings
   indefinitely; the repair rewrites the wrapper in place.
9. **Dependency and cost (7):** release N costs a file more, not less (SE-31); the suffixed release names
   (SE-09); the MSI build would throw at N+1 (SE-70); no one owned the lab SG3 waits on (SE-79); `zenity` is an
   undeclared hard dependency of the wizard verb on Linux (SE-13). Held: `zenity` does no work at import time
   (checked upstream at v0.10.12), the CLI already links about 97% of the desktop's bytes, and there is no
   Windows resource to carry into both builds.
10. **Honesty (15):** the size and mtime fallback was misread throughout (SE-47, SE-37, SE-77); "the pair is one
    program" rested on a missing step and an unlabelled toolchain fact (SE-07); "proven" for the wizard's
    dialogs with no row (SE-12); MA-582 could not see the re-exec it claims to forbid (SE-C03); S59 and X17
    over-promised (SE-43, SE-44); the id range collided (SE-46). Held: the "today" claims about `repairable`,
    `job.Read`, the wrappers, MA-546's text and `CloseApplication` matched the code.

## Lab priorities (what decides viability), ranked

1. **L-W3, extended:** the MSI upgrade N-1 to N with a schedule of each kind, a field archive both surfaces
   scheduled, a run in flight through Install-RMM.ps1 and plain `msiexec /qn`, the exit code and Restart
   Manager, the copies' file times and what the fallback reads. Decides whether SE1+SE2 ship for Windows.
2. **L-W4, extended:** `GetConsoleWindow` and `GetConsoleProcessList` under conhost and Windows Terminal, and
   Ctrl-C or closing the console of `mailarchive.exe desktop`. Decides the Windows no-argument rule and SC17.
3. **L-W2, extended:** the wrappers running the GUI-subsystem build (wait, stderr, exit code) and the
   unwrapped console build. Decides whether SE2 records `mailarchivew.exe` inside a wrapper.
4. **L-W9 (new):** deletion and Defender quarantine of `mailarchivew.exe` on an MSI install, and what the
   installer's Repair restores. Decides the `exe_missing` branch and how urgent Q13 is.
5. **L-M1, L-M2 extended, L-M4 (new):** the `.app`'s first and second open, launchd through the link,
   `os.Executable` through links, and app translocation. Decide the macOS rows and §7.2.
6. **L-W7, L-W8 (new):** file ownership on Windows (MSI, zip, FAT) and path spellings (case, 8.3, `\\?\`,
   junctions). Decide SC7's owner bound and SC2's directory comparison.
7. **L-W14 (new):** whether `app.Run`'s Outlook probe from an unattended `run-job` starts Outlook or blocks.
   Decides whether a gap row is owed before SE1 ships for Windows.
8. **L-W10, L-W11 (new):** MSI rollback by uninstall then install, and the skip path to the SE4 build. Decide
   P10's label and SE4's release note.
9. **L-L1, L-L2, extended; L-L4 (new):** Linux launches in both directions on amd64 and arm64, the box with no
   `zenity`, and cron mail with no MTA. Decide Q4, SC44's refusal and P5's wording.
10. **L-W6 and L-L3, only on a Q10 yes; L-M5 and L-M6, only for the macOS flip slice;** L-C1, L-C2, L-W1,
    L-W5, L-W12 and L-W13 are measurements or wording.
