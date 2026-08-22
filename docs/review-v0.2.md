# Design + friction review — v0.2 features

Covers the work shipped after v0.1.1 (releases v0.2.0 → v0.2.3). Two parts: a
**design review** (what we targeted vs. what was delivered and verified) and a
**friction walk** (the new surfaces exercised as the actual actor). House format
per `docs/review-friction.md`.

## 1. Design review — goals targeted → delivered

Verification legend: **✔ verified** (automated test or real-data run) ·
**~ partial** (tested in part; some path unexercised) · **⧗ pending** (built,
not yet executed against real infrastructure).

| # | Goal targeted | Delivered | Invariant / specs | Verified? |
|---|---|---|---|---|
| A | `reindex` self-repair — prune index+manifest entries whose file is gone, regenerate pages | Yes | R13 · MA-40..44 | ✔ integration (build archive, delete file, reindex) + real-data CLI run (`kept=16 pruned=1`) |
| B | `schedule` recurring backups — cron/launchd/schtasks, print-by-default, `--install`/`--remove` | Yes | R14 · MA-45..50 | ~ generators + crontab upsert/remove **✔** unit-tested; the real `crontab`/`launchctl`/`schtasks` **apply** is a thin shell-out, **not** exercised (would mutate the host) |
| C | GUI auto-detect — "Auto-detect my mailboxes" reusing `-auto`, pick one/all | Yes | MA-51 (`DiscoverInputs`) | ~ discovery **✔** tested; GUI compiles + cross-compiles, but is **not run** here (no display) |
| D | Evolution support incl. IMAP cache (operator-chosen "full") | Yes | R15 · MA-52..55 | ✔ **real-data**: exported a live cache account (14 msgs, nested + `subfolders/` + `cur/NN` shards). Local Maildir++ decode unit-tested on real folder names; full local run not completed (slow) |
| — | Bug: auto-detect listed unreadable Outlook files; illegible picker | Fixed | R1 · S17 · MA-56 | ✔ unit (drops corrupt/empty stubs; keeps locked/valid) |
| — | Bug: Windows GUI exited silently (go-pst panic on corrupt `.ost`) | Fixed | R10 · S18 · MA-57 | ✔ deterministic fuzz-derived corrupt-PST fixture; prove-fail reproduced the exact panic |
| E | `-outlook` — drive Outlook (COM) to write a clean `.pst` per account, then archive | Yes | R16 · S19 · MA-58/59/60 | **MA-58** (name safety) **✔**, **MA-59** (off-Windows refusal) **✔**; **MA-60 (the COM export itself) ⧗ PENDING — never executed** |
| E+ | `-outlook` Send/Receive-and-wait + completeness warning | Yes | R16 (MA-60) | ⧗ pending (same COM path) |
| — | CI/release: Node-20 action bump, auto-changelog, notes backfill | Yes | — | ✔ observed green in v0.2.1–v0.2.3 runs |

### Honest gaps (not yet verified)

1. **`-outlook` COM path (MA-60) has never run.** `AddStoreEx` / `CopyTo` /
   `RemoveStore`, the store-enumeration, the created-store lookup by file path,
   and `SyncObjects.Start` are coded to the Object Model spec but **unproven**.
   This is the largest risk and is released **behind a lab-pending marker**.
2. **`schedule` real apply** (cron/launchd/schtasks) is unexercised — only the
   pure generation and crontab string-editing are tested.
3. **GUI runtime** — every GUI addition (auto-detect picker, Outlook-app option,
   sync advisory dialog) is compile-verified only, never launched.
4. **Evolution local Maildir++** full run not completed on real data (the cache
   account was; the local store's decode is unit-tested).

## 2. Friction walk — new surfaces (walked as the actor)

**8 new cells walked · 8 functioning · 0 Type-II blockers on verified features ·
1 Type-II→III polish on `-outlook`.** Types per `review-friction.md`
(I inherent · II design-choice · III buildable-away).

| Cell (actor × scenario) | Functions? | Friction finding (observed) | Type | Fix / backlog |
|---|---|---|---|---|
| operator × `reindex` forgot `-out` | yes | `-out is required (the export directory to reconcile)`, exit 1 | I | names the fix |
| operator × `reindex` on a never-exported dir | yes | `no search index at …/search.db (run an export first)`, exit 1 | I | fail-closed, names remedy (matches serve/search) |
| operator × `reindex` after deleting a file | yes | `reindexed: kept=16 pruned=1` — clear; a `Wrote 2 folder index pages…` line precedes it (mild noise) | III | acceptable; could quiet the pages line |
| operator × `schedule` (print, default) | yes | prints the exact cron block + `This was NOT applied…` — opt-in is unmistakable | I | — |
| operator × `schedule` bad `-interval` / missing `-out` / `-install`+`-remove` | yes | each refuses legibly naming the problem, exit 1 | I | — (MA-50) |
| operator × reads the generated cron marker | yes | `# mailarchive-backup mailarchive-backup` — the tag doubles the default name | III | cosmetic; drop the redundant token when name == default |
| operator × `-outlook` on non-Windows | yes | ~~first printed the completeness Note + `Outlook: exporting…` before refusing~~ → **FIXED**: now an immediate clean refusal (`requires Windows…`, exit 1), no note/progress | II→III (resolved) | platform-guarded before any note/progress |
| operator × help for new verbs (`-h`, `reindex -h`, `schedule -h`) | yes | each names its flags incl. `-outlook`, `-outlook-sync-wait` | I | X3 holds |

### UX-contract (X-series) delta for the new surfaces

- **X3 (help total)** — met for `reindex`, `schedule`, and the new `-outlook*`
  flags.
- **X6 (legibility surface)** — `reindex` prints a kept/pruned summary;
  `schedule` prints the entry; `-outlook` logs each step + the completeness note.
- **X7 (no traceback to the operator)** — *strengthened*: the go-pst open-time
  panic (R10/MA-57) no longer reaches the operator, closing a real gap the
  earlier X7 row didn't cover.
- New refusals all route through `return err` → `mailarchive: <msg>` exit 1,
  consistent with X1/X2 (the standing C1/C2 conditions still apply).

### The one actionable friction finding — fixed in this review

**`-outlook` emitted the completeness note and an "exporting…" progress line
before a refusal that is guaranteed off Windows.** It read as "it started, then
failed." **Fixed here:** the CLI now platform-guards up front and refuses
immediately (`requires Windows…`, exit 1) with no note/progress. MA-59 stays
green; `go vet` and the Windows cross-build stay clean.

## 3. Verdict

- **A, C, D, and both bug fixes: SHIPPABLE and verified.**
- **B (`schedule`): SHIPPABLE** — generation/idempotence proven; the host-apply
  wrappers are thin and unexercised (backlog: a fake-`crontab` round-trip test).
- **E (`-outlook`): SHIPPED BEHIND VALIDATION.** Correct by construction and
  cross-compiled, but the COM path is **unverified** (MA-60 pending). Do not
  rely on it until it runs on real Outlook; the release notes and catalog say so.

Backlog from this review: (1) ~~platform-guard the `-outlook` note/progress before
refusal~~ **done**; (2) a fake-`crontab` install/remove round-trip test for
`schedule`; (3) drop the redundant token in the default cron marker
(`# mailarchive-backup mailarchive-backup`); (4) execute MA-60 on a real
Windows+Outlook host (see the testing note below).
