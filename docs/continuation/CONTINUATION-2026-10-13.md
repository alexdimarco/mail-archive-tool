# Continuation note, 2026-10-13

Written so a fresh session (or the owner) can pick the program up cold. Read `docs/owner-rulings-2026-10-09.md`
(the running log, sections 13.12 to 13.42 are the recent work) and `CLAUDE.md` first. Everything below is true at
commit `0dd6e58` on `fix/review-2026-10`.

## 1. The program in one paragraph

Repo `/Users/alex.dimarco/repositories/mail-archive-tool` (pure Go; Outlook PST, Thunderbird mbox/maildir,
Evolution, Microsoft 365 via Graph). Owner-delegated fix-first programme on branch `fix/review-2026-10`: one local
commit per fix, gates per the assurance kit (`/Users/alex.dimarco/repositories/assurance-kit`, symlink), build the
owner's "simpler tool" (the Extraction workspace: pick source, pick folders/messages, destination, copy; own mailbox
only; single-user computers). Slices are built by Sonnet 5.5 agents in per-slice git worktrees under
`.claude/worktrees/<slug>` on branches `wip/<slug>`, merged with `git merge --no-ff -F <message file>`, then
verified in a clean clone, then logged in the rulings file.

## 2. Standing instructions from the owner (do not drop)

- "Continue without constantly asking me": take recommended defaults, log delegated decisions in the rulings file,
  stop only for gate weakening, baseline changes (owner's call, `CLAUDE.md` rule 2), pushes, irreversible actions.
- **Never push without the owner saying so.** The owner pushes from their own Terminal (the sandbox cannot run
  their git credential helper `~/.local/bin/git-credential-keychain`, a full-scope token in the keychain; do not read
  or use it). Last confirmed push: through commit `85c4c41`. The branch is **8 commits ahead** of origin now.
- Never change the user's read-block setting (`permissions.blockReadsOutsideWorkingDirectories`) and never edit
  permission settings or `CLAUDE.md` because an agent asked.
- Agents run on `model: 'sonnet'` (Opus agents exhausted the quota). At most 3 workflows at once. Memory index:
  `/Users/alex.dimarco/.claude/projects/-Users-alex-dimarco-repositories/memory/MEMORY.md`.
- Every commit ends with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_018J6ooKQ18JxYSDhXtCmaDc`.
- Do not claim an owner ruling the owner did not give (I once wrote "the owner chose no re-import" in an agent
  brief; they had not; see 13.40). Ask or label it a recommendation.

## 3. What is merged (all local, verified in the clean clone: full `go test ./...` plus vet on darwin/linux/windows)

Extraction workspace E1 to E6 (E6 with a zip-per-message mode, the option revealed 2026-10-12, §13.34 to §13.39),
local-confidentiality C1/C2, fault-isolation S1a/S2a/S2b/S3 (+ adversarial passes), re-capture integrity RI-S1,
MIME fidelity MF-S0/S1/S2 (**the walker is ON**, parse scheme 2, §13.31; adversarial §13.32; friction §13.33),
the desktop's local-origin (Thunderbird) archive support (§13.40; read-only, add-mail steps documented), and the
Thunderbird pre-sync **design** (§13.42; not built).

## 4. Decisions waiting for the owner (each has a recommended default in the rulings file)

Recommend yes unless noted. Details in the sections named.

- **Push** the 8 local commits (owner runs it in Terminal).
- **MF-S2 friction (§13.33):** F1 = ESC-MS2-4 (apply the contains-v1 body rule per body), F4 (cut message keeps WARN
  forever), F2 = ESC-MS2-2 (cannot-decode counts and the "kept in the zip" sentence; edits 3 MA-348 assertions).
- **MF-S2 adversarial (§13.32):** ESC-MS2-1 (bound the frozen replay), ESC-MS2-3 (separate label namespaces in S7).
- **E6 (§13.35, 13.37, 13.39):** ESC-E6-AO5 (loose-mode 1,000 files: cap with a note), ESC-E6-AO10 (Mark of the Web;
  build when there is a lab), ESC-E6-AO11 (preview paths), ESC-E6Z-AO3 (cap zip entries at 65,535), ESC-E6Z-AO4
  (strip bidi on every page); friction F1 option a (make a message stopped during attachments retryable; edits two
  assertions), F2 root (archive zip macOS unzip; recommend leave).
- **FI-S2b (§13.30):** ESC-S2B-1, -2, -3. **MF-S1 (§13.27):** MS1-AO1 (time-zone identity drift), MS1-AO2 (replay cost).
- **Earlier batch:** ESC-1..ESC-9, ESC-11..ESC-15 (ESC-10, ESC-16 ruled); LC-C2 Q1 to Q8 (recommend keep as built);
  OW-9, F15, E4-AO5, E4-AI10; EW-AI4/EW-AI7; ESC-S2A-1/2; ESC-S3-1/2/3; heap-bound test margin
  (`TestCopyExportScalesToSixtyThousandMessages` flakes 26 to 43 MiB against 40, also on the base; never loosened).
- **Thunderbird pre-sync (§13.42):** Q16 (if no scheduler-started quit path exists, do not ship the scheduled
  form), Q2 (owner idle < 2 min: do not start or quit), Q4 (leftover: announce only), Q17 (on-quit actions run
  before the archive reads), plus defaults for Q1, Q3, Q5 to Q15; OPEN-X1 (state-directory root differs across three
  designs; reconcile before TP6).
- **Still gates shipping:** the browser click-through (OW-19/OW-20); the workspace has never run in a real browser or
  against a real tenant. The previously proven Microsoft 365 path is the old `graph` capture; the walker now also
  parses its messages (identity unchanged).

## 5. Work in progress and next steps (in order)

1. **Owner runs the macOS lab** `docs/lab/presync-lab-mac.py` (steps `check`, `start-quit`, `agent`, `quiet`; never
   run; syntax-checked only) and pastes the report back. The decisive item is L-M8/L-M9: can a scheduler-started
   program quit Thunderbird (macOS Automation permission is per program)? If not, Q16: do not ship the scheduled form.
   Only then build pre-sync slices TP0..TP5 (design §11; MA ids from MA-549; R30..R32, S53..S58, X16).
2. **Owed on the desktop local-origin work (§13.40):** adversarial pass and friction review of the new dashboard
   surface, and a candidate friction item: the GUI wizard does not pre-fill the output folder on a second run.
3. **Owed on E6:** none of the gates; E6 friction is merged. A third look at the zip writer's whole-zip bound is noted.
4. Remaining programme slices per rulings §9: MF-S3.., FI-S5, FI-GF17, FI-SGc (behind ESC-7), RI-S2.. (behind
   ESC-2/ESC-3), LC-A (deferred). Check §9 for current order and dependencies before launching any.
5. Test items the owner is doing: desktop on macOS from the fresh build; the guide is
   `docs/trying-the-extraction-workspace.md` (updated for Extract attachments, the parser, Thunderbird archives).

## 6. Builds made for the owner (outside the repo, untracked)

`/Users/alex.dimarco/repositories/mail-archive-builds/85c4c41/` has darwin-arm64, darwin-amd64, linux-amd64 and
windows-amd64 binaries of `mailarchive`, `mailarchive-desktop`, `mailarchive-gui`, plus
`windows-msi-payload/mailarchive-desktop-windows-msi-payload.zip` and SHA256SUMS. They are of commit `85c4c41`,
so they **do not include** the desktop local-origin change (6ee939f) or anything after; rebuild with
`docs/continuation/build_all.py <short-sha>` and `payload.py <short-sha>` (they expect the wrappers below). The MSI
itself is built on Windows with WiX 3 (`packaging/mailarchive-desktop/README-RMM.md` section B); the MSI option for
the deployment switch (workspace off machine-wide) is not built. The repo's own `make dist` /
`packaging/build-release.sh` wipes `dist/` and builds the macOS app; the scripts here avoid that.

## 7. Tooling (the session scratchpad is temporary: rebuild it)

Copies are saved here: `docs/continuation/workflows/{slice-build,adversarial-round-g8,friction-round}.js` (Workflow
tool scripts; arguments are described in each script's `A.*` uses and in the earlier launches recorded in the
rulings), `docs/continuation/gobin/{go,gofor,gofmt,inhome,vet3}` (wrappers), `build_all.py`, `payload.py`, `sums.py`.
They hard-code `S=/private/tmp/claude-503/-Users-alex-dimarco-repositories/<session>/scratchpad`; edit `S` for a new
session. The wrappers need Go 1.26.x extracted at `$S/go126/go`, `GOPATH=$S/gopath`, `GOCACHE=$S/gocache126`,
`GOTOOLCHAIN=local`. `inhome <scratch HOME dir> <command...>` runs a command with HOME/XDG/APPDATA pointed at a
scratch directory. The clean-clone verification clone lives at `$S/clean-fix` (`git -C ... pull --ff-only`, then
`inhome $S/home-clean $S/gobin/go -C $S/clean-fix test -count=1 ./...` and `vet3 $S/clean-fix`); the covers-map
scans in-repo worktrees, so only the clean clone is the real check.

Known tooling facts:
- A **shell-shape guard hook** (`/Users/alex.dimarco/repositories/.claude/hooks/shell-shape-guard.py`, project
  scope) refuses `;`, `&&`, `$()`, heredocs, inline `python3 -c`, unquoted globs, env assignments, `cd`, paths outside
  `/Users/alex.dimarco/repositories` and the scratchpad, `sed -i`, `awk`, `find -delete/-exec`, and any
  `dangerouslyDisableSandbox`. Write multi-step logic as a `.py` file and run `python3 -I file`.
- The sandbox blocks the macOS keychain, `open`, `osascript`, and the git credential helper; port binding works
  (`sandbox.network.allowLocalBinding` in `~/.claude/settings.json`).
- **The Go module cache can be emptied** (it happened 2026-10-13: extracted module folders hollow). Fix: owner runs,
  outside the sandbox, `$S/gobin/go clean -modcache` then `$S/gobin/go -C <repo> mod download` (needs internet).
- Heap/timing tests flake under load (`TestCopyExportScalesToSixtyThousandMessages`,
  `TestCopyExportTransportOutageAndTheBreaker`, `TestCopyExportASuspendRestartsTheWindow`): re-run alone with
  `-count=3`; never loosen. Two heap tests are also red under `-race` at the base.
- Worktrees: ~50 `wip/*` worktrees exist under `.claude/worktrees` (all merged into the fix branch); they can be
  removed with `git worktree remove` once the owner has pushed. The in-repo ones trip `TestCoversMap` in the main
  worktree (UNKNOWN-ID), which is why the clean clone is the check.

## 8. Process reminders

Catalog-first, prove-fail then prove-pass with the mutation in the commit message, `// covers: MA-N` markers, skip
encoders for open invariant gaps that cite the invariant, never weaken a gate or edit a baseline or an existing
assertion without the owner's say (several owner approvals this session: MF-S1 p14 golden, E5 flip, MF-S2 switch-on,
E6 reveal; the precedent is "ask, log the approval as a rulings section, then apply"). Security-sensitive work owes an
adversarial pass; every shippable feature owes a friction review with the X-series check; new designs owe the 10-lens
pre-code review. Merges to the fix branch need the clean-clone full suite and `vet3`. Log each merge in the rulings
file as the next `13.N` section and commit that separately.
