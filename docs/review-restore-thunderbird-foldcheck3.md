# Fold re-check 3: Restore to Thunderbird, the second fold check's items (commit 06b18c7)

**Verdict: PASS_WITH_NOTES.** P1, P2, P3, the two residual sentences and N-a..N-g are all folded,
and concretely enough to build from. At every one of the 12 crash points in §8's new table, the
next Start removes nothing unless no entry named `Restored — NAME` is beside the container and
everything inside is on the made-list. What it then removes is only what this restore made. No
point removes anything Thunderbird is showing *at that moment*. Nothing found is a safety or
correctness defect that has to be fixed before slice R0. The notes below are for the build slices.
Two of them (S1, S2) correct catalog or test-row text that R0 writes, so fold them at R0. One (S3)
is a cheap hardening that the lead may want before R4.

- Date worked on: 6 November 2026 (the session's date).
- Checked: `docs/design-restore-thunderbird.md` at 06b18c7. That is the full diff against 547b00f
  (450 lines added, 4 changed, design only). I read it beside §5.4 (both earlier sequences), §7.2,
  §8 (the RT-4 states, the foldcheck note's cases 1–3 and rules 1–7) and §11's R40 and R41. I
  read the second fold check (547b00f) first, as the brief said.
- Register: a verification finding. Lines marked "recommend" are opinion.
- Read-only: no code or Thunderbird was run, and the design was not edited.

## 1. Per item

| Item | Result | Where, and what was checked |
|---|---|---|
| P1 nothing removed under a parent name | FOLDED | §8 foldcheck2 note: case 3 is split into 3a (any entry of the parent name, made by any program → remove nothing, "abandoned", W-VISIBLE) and 3b (no entry → the cleanup). Every cleanup, the run's own included, now runs in a fixed decision order: parent name, then list, then a scan of the whole container, and only then any removal. The live W-TAKEN case leaves the checked tree visible (§5.4 foldcheck2 note, decision 12). The scan runs before any removal, as the check asked. |
| P2 words and trigger | FOLDED | §7.2 foldcheck2 note: W-UNFINISHED shows only when all four hold: no closing line, a reachable Local Folders that is still this profile's, the marker present, and no entry of the parent name. An "abandoned" leftover is never promised removal. W-LEFT's and W-TAKEN's false sentences are superseded. Uninstall uses the same rule (§7.4 foldcheck2 note). |
| P3 skip = committed | FOLDED | §7.2 foldcheck2 note, R41 as reworded (§11) and MA-1138 all say the same thing. Skip when no entry of the parent name is there. Under O1(b), skip the container and its parent only when the parent is a regular file (`Lstat` by the walk) and the log has "checked" and no "taken". Otherwise back both up as ordinary folders. With an unreadable log, skip only containers with no entry of the parent name. The three agree with each other and with Start's case 2. |
| Residual case, two sentences | FOLDED | §8 foldcheck2 note: the O1(b) cost is named, and case 2 now needs a regular file, so a folder of that name goes to 3a. MA-1138 pins the cost. |
| N-a order | FOLDED | Cleanup steps 1–4; nothing is removed before step 4. |
| N-b uninstall pattern | FOLDED | `restores.log` goes into `fixedConfigNames`. The lock files get a pattern case in `deletableConfigName` (`restore-` + 16 lowercase hex + `.lock`), kept in step with `tbrestore` by behaviour (MA-1140). |
| N-c fsync after create | FOLDED | One `Lstat` re-check. A regular, empty file → committed, finished, error named, exit `0`/`3`. Anything else → "abandoned", nothing removed, W-COMMITERR, exit `1`. |
| N-d two old lines | FOLDED | §8 line 1707 and §13 R5 are both marked superseded. |
| N-e labels | FOLDED | Relabelled in the header note, §7.4 and §7.5. |
| N-f Local Folders must match | FOLDED | Step 0 ("elsewhere"): no line written, nothing removed, W-ELSEWHERE. Case 1 is reached only after step 0. |
| N-g Start goes on | FOLDED | The new restore always runs. Earlier restores that were not cleaned up are named after its result, and its exit code is its own (§8, §10). |
| P1 premise as a lab step | FOLDED | Added to MA-1091's Windows leg; it runs before R7 (§12.3, §13). See S3: one more of its readings is load-bearing. |

## 2. The 12 crash points (§8 foldcheck2 table), checked against the rules

Rules used: step 0, cases 1, 2, 3a and 3b, cleanup steps 1–6, and rule 7. "Own" means made by this
restore: an entry on its made-list, or the made-list, the marker (matching ID) or the container,
all made by exclusive create before or with the list.

| # | Crash at | Rule result | Removes | Thunderbird could show it now? | OK |
|---|---|---|---|---|---|
| 1 | after "started", before `Mkdir` | step 0, then case 1 | nothing | n/a | yes |
| 2 | after `Mkdir`, before the marker | case 1, left | nothing | no (empty, no parent) | yes |
| 3 | after the marker, list absent or empty | 3b, the scan finds only the marker (and list) | the marker, the list if any, the emptied container: all own | no (no parent; lab A3) | yes |
| 4 | writing, or after "checked"; clean | 3b | listed entries, then the list, marker and container: all own | no | yes |
| 5 | as 4, then another program makes the parent name | 3a, **except** after "checked" with a regular file: case 2, "ended" (S2) | nothing | yes | yes (safe either way) |
| 6 | as 4, something unlisted inside | 3b, step 3 | nothing | no | yes |
| 7 | residual (create failed, before "taken") | case 2 if regular file, else 3a | nothing | yes | yes |
| 8 | after "taken" | 3a while the entry is there; **3b if it has since gone** (S3) | nothing / own listed entries | yes / no | see S3 |
| 9 | after the parent file, before "ended" | case 2 | nothing | yes | yes |
| 10 | parent fsync failed, then crash | regular file → case 2; none → 3b | nothing / own listed entries | — / no | yes |
| 11 | during a cleanup's removals | the decision order again from step 0 (3b normally; 3a or W-LEFT if something has appeared) | only own entries still there | no | yes |
| 12 | Local Folders moved or drive missing | step 0, elsewhere | nothing | n/a | yes |

So nothing on any row removes an entry that is not the restore's own, and nothing removes while an
entry of the parent name is present. The cleanup never removes the parent name (rule 7). The only
path to removing files that Thunderbird *could earlier* have shown is S3. It needs a program other
than Thunderbird to remove only the parent file, and Thunderbird's own `.msf` files still stop it.

## 3. The new words, each in its state

| Row | True in exactly the state that shows it? |
|---|---|
| W-UNFINISHED | Yes for "Thunderbird does not show it" (no parent entry). Its promise "removes what it wrote … if anything else has been put in that folder, it leaves the whole folder" does not cover a parent name appearing (3a) or an unreadable list. Those outcomes are reported when they happen, so this is a soft promise, not a false fact. |
| W-LEFT | Yes for a step-3 stop. **Not** for a step-4 stop (something added between the scan and the removals): some listed entries were already removed, so "so it removed nothing there" is false (S4). |
| W-TAKEN | Yes in a live W-TAKEN where the entry is still there at cleanup step 1. If the entry has gone by step 1, the tree is removed and "removed nothing" is false (S5; Thunderbird is held out then, so this is safe). |
| W-VISIBLE | The checked form is true: with "taken", or a folder or link entry, MailArchive did not make that name. The unchecked form is true, but "stopped before it finished writing" also covers a crash during the post-write check (S6). "Backs them up" matches P3 in both forms. "Shows" rests on the MA-1091 step, as the text says. |
| W-COMMITERR | True. Its Desktop-start condition says "no *file* of the parent name", where §7.2 says "no *entry*" (S7). |
| W-ELSEWHERE | True when the restore had not committed. "Did not finish" can be false for a restore that committed but crashed before its "ended" line and whose Local Folders then moved (S6). |

## 4. Citations (new in 06b18c7; all checked, all correct)

- `internal/uninstall/config.go:27-38`: `fixedConfigNames` and its comment, "the exact config-dir
  files uninstall owns".
- `:35-37`: the `deletions.log` entry.
- `:114-130`: `deletableConfigName`, with the `.jobfail.log` and `.alert-streak` suffix cases (and
  `.sync-alert` and `.delete-refused`).
- MA-965 (catalog line 1320): its test keeps `deletions.log` in step with `archdelete.LogName`.
- MA-910 (line 1268): keeps `*.sync-alert` in step "by behaviour".
- Lab rows MA-1091 and MA-1133, and slice R8, exist where §12.3 and §13 say.

## 5. Notes for the slices (not blocking)

- **S1 (R0, catalog wording).** R40's new clause and the table's closing sentence say the cleanup
  "removes only entries this restore's made-list names". But steps 3–4 also remove the made-list,
  the marker and the emptied container. None of these is on the list. So the invariant as worded
  is false about the design's own cleanup. Recommend, when R0 writes it: "removes only entries its
  made-list names, then the made-list, its marker and the emptied folder".
- **S2 (R0, table row 5 and MA-1123).** The crash is after "checked" and before the create, and
  then another program makes a *regular file* of the parent name. That is case 2: "ended", not
  3a. It removes nothing (safe). But it is a second instance of the residual case's cost: it is
  reported as finished, and under O1(b) the other folder's own mail is skipped by backups. It is
  just as rare (a crash in the commit instant, plus that exact name). Recommend splitting the row
  and naming it in the residual bullet. MA-1123 tests "every row of §8's foldcheck2 table", so the
  builder needs the table to agree with case 2.
- **S3 (R4, hardening; lead's call).** Row 8 says "after 'taken' → 3a", but the rule checks whether
  the entry is there *now*. Here is how that goes wrong. The crash comes after "taken". Thunderbird
  is no longer held out, so it shows the tree under the other entry, and the person moves mail
  into a restored subfolder. Then a program other than Thunderbird removes only that parent entry
  (Thunderbird's own delete or rename takes the `.sbd` with it, which leads to case 1). The next
  Start goes to 3b. Only the scan's `.msf` check stands between that and removing the person's
  mail.
  - Recommend: a "taken" line means 3a whatever is beside the container now. The log already
    proves a parent name existed, and it costs one condition.
  - Also correct §8's "which step 1 catches" sentence ("the parent name was there earlier" is not
    caught by step 1).
  - Also correct §12.3's "nothing in the cleanup rule rests on the answer". Reading (3) of the
    MA-1091 step (which `.msf` files Thunderbird makes inside the container) is what this backstop
    rests on.
  - Row 5's version of the same path has no log evidence. It stays with the `.msf` scan and the
    R3–R5 adversarial pass.
- **S4 (R4, words).** W-LEFT needs a form for the step-4 stop: "removed part of what it wrote, then
  found …". The same goes for 3b's "'abandoned' when the cleanup removes nothing", which is also
  used for a partial step-4 stop. MA-1122's fault-seam case should assert those words.
- **S5 (R4, words).** A live Cancel, failure or W-CHECKFAIL whose cleanup meets the parent name
  has no exact row. W-TAKEN's "had already written and checked every message" and W-VISIBLE's "An
  earlier restore" are both untrue there, yet MA-1122 cites "W-VISIBLE or W-TAKEN". Three more
  states also need words:
  - a live W-TAKEN whose entry has gone by step 1 (the tree removed: the old W-TAKEN words fit);
  - a parent create that fails for a reason other than "exists" but leaves an entry (step 1
    catches it, with no "taken" line);
  - the case-variant entry that lab C showed fails the create (on a case-insensitive volume,
    `Lstat` finds the variant, so 3a).
- **S6 (R4, words).** For W-VISIBLE's unchecked form, say "stopped before it finished". For
  W-ELSEWHERE, say "may not have finished" (it cannot tell committed from unfinished with the
  drive gone).
- **S7 (R5, display rule).**
  - Use §7.2's "no entry" for W-COMMITERR at Desktop start.
  - A 3a-abandoned leftover whose parent entry later disappears matches none of §7.2's three rows
    (W-LEFT, made-list unreadable, W-COMMITERR). Pick one: W-LEFT's "Thunderbird does not show
    that folder … delete it yourself" fits.
  - Uninstall names no "elsewhere" leftover, although nothing will clean those up either. Consider
    naming them by their recorded path.
- **S8 (R4, the scan's name match).** The scan compares the names the directory read returns with
  the names on the made-list. On a macOS HFS+ volume (which stores names decomposed, a form called
  NFD) or with any normalisation difference, an accented folder name would read as "not on the
  list". The cleanup would then remove nothing and say W-LEFT for a clean leftover. That is the
  safe direction, but it would happen on every such cleanup. Recommend matching names as the file
  system does (or `Lstat`-ing each listed name and counting directory entries), with an NFD test
  in MA-1122.
- **S9 (R4).**
  - An "elsewhere" restore stays open, so W-ELSEWHERE is named at every Start for as long as it
    stays open. Consider closing it once the person has deleted it (when the old path is
    reachable), or naming it once.
  - Case 2 requires a regular file, while N-c's live re-check requires a regular *empty* file.
    Both are safe; say which applies where.
- **S10 (R5).** Desktop start's "reachable and this profile's Local Folders" needs each recorded
  profile's current Local Folders, which means a read of Thunderbird's prefs. Name the function it
  reuses (§3.2) so R5 does not invent one.

---

- Drew on, and reached: the design at 06b18c7 (the full diff against 547b00f, and §5.4, §7.2, §8
  and §11 as they stand); the second fold check at 547b00f; `internal/uninstall/config.go` and the
  catalog rows MA-910 and MA-965 in this worktree.
- Not checked, per claim:
  - S3's path rests on two things not run here: that Thunderbird shows a tree under another
    program's parent file, and that it writes `.msf` files inside the container. That is the
    MA-1091 step.
  - S8 rests on HFS+ storing names decomposed (general knowledge, not run here).
  - Every lab row is still pending. No test or program was run.
- Smallest check for the reader: §8's foldcheck2 table, row "after 'taken'", against step 1 of the
  cleanup a few lines above it (design lines 1824–1825 and 1901). Row 8 is unconditional; step 1
  is not.
- Counters. Register: verification finding. Borrowed lines: short quotes from the design and the
  code. Drift: none noticed. Touched: this file (new), one commit, one push; the design and product
  code are unchanged. Persona: none. Source reached: design, second fold check, code and catalog,
  yes; Thunderbird and the VM, no.
