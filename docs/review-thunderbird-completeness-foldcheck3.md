# Fold check 3: the M6-residual note and the foldcheck2 fold, independently, and C3's code against them

**Verdict: PASS (narrow), no must-fix.** The M6-residual rule (7126111) holds against Thunderbird's
source, and the code applies it. Every error it makes goes toward "can't tell", which is the safe
side. MF1 (4131c9b) is built as written. The encrypted Run now note's lead comes only from the
verdicts (`RunLead`), and no path I traced through `planTB` gives "Backup complete:" over a store
that is not `not_applicable` and may not say complete. There is one LOW contradiction (**N1**): a
store opened by the Evolution reader with no profile above it gets no result at all, even when it
holds `.msf` files. In that corner the residual rule is never applied. It needs a maildir-format
Thunderbird account with folder names that look like Evolution's, copied outside its profile. I
suggest folding it with the existing ADV-C2-16 follow-up, not holding C3 for it. Three test gaps
are LOW (**N2**).

- **Date worked on:** 8 November 2026 (the session's date).
- **What was checked:** worktree `tb-completeness` at **e85d840**. The design
  `docs/design-thunderbird-completeness.md`: §0's two fold notes, §2.2's M6-residual fold, §4.2's
  and §4.4's residual and L4 notes, §7's MF1 note and M8 row, §12's MA-1196 and MA-1181/MA-1186
  folds, §13's L6 note and C3's builder notes, and §17's headline rule with its two closing notes.
  Commits 7126111 and 4131c9b in full. `docs/review-thunderbird-completeness-foldcheck2.md`
  (edf9e0b) in full.
- **Independence:** I wrote none of the design, the M6-residual note, the foldcheck2 fold, fold
  check 2 or C3. **Order of reading:** I read the two commits (the argument) first, then fold check
  2, then the code and Thunderbird's source. So the sources were read after the argument, not
  before.
- **Procedure:** read-only, except for this file. I ran the five covering tests named in §3 with
  `-run` (`go test ./internal/tbcomplete/ ./internal/wizard/ ./internal/app/`): all pass. That is
  not the unfiltered run. No product code, Thunderbird or lab machine was run or changed.
- **Register:** a finding report. The verdict, the severities and the suggested fixes are my
  opinion.

## 1. The M6-residual rule: right and fail-safe? — PASS

**The source.** ESR 153.6.0esr, `.claude/tbsrc-esr153/esr153/mailnews__imap__src__nsImapMailFolder.cpp`:

- `CreateSubFolders` (`:404-508`; the function starts at `:406`) makes an IMAP folder's
  subfolders by listing the directory. Lines `:430-433` skip every entry whose name does not end in
  `.msf`, compared case-blind (`nsCaseInsensitiveStringComparator`). Its caller `GetSubFolders`
  (`:538`) calls it for the server's own directory and for each `.sbd`.
- The Panorama check at `:447` only skips reading the folder cache. The `.msf` listing still runs,
  so at ESR 153 a folder Thunderbird knows from disk is one with a `.msf`. The design's claim
  holds, with the line numbers it gives.

**The code.**

- `Unmatched` (`internal/tbcomplete/verdict.go:150-154`) has the three facts. Its zero value is
  "can't tell".
- `Assess` (`verdict.go:419-423`) gives `not_applicable` to an unmatched store only when all three
  facts are true. Any other unmatched store is `unknown`, `no_match`.
- The facts are filled in at `internal/app/tbcompleteness.go:196`:
  - **no profile:** `findTBProfile`, which is `thunderbird.FindProfileDir` (it walks up six levels);
  - **no `ImapMail` part:** `hasImapMailPart` (`:145-152`), case-blind on Windows and macOS;
  - **no `.msf`:** `noMsf` (`:127-141`).
- For a folder input, `.msf` is noted from the walk's own listings. `noteEntry`
  (`internal/source/mailboxcount.go:115-120`) runs for every entry of every listing, before the
  `.sbd` skip, the restore skip and any check of the entry's kind (`internal/source/localwalk.go:570`).
  So a link or a non-file named `x.msf` counts, and nothing is opened. This matches the design's
  "seen by name only".
- `planTB` (`tbcompleteness.go:107-108`) turns on `.msf` noting for every unmatched folder with no
  profile, even with `Count` off.

**Can a store Thunderbird keeps as IMAP be `not_applicable`?** Only in the cases the design
already accepts:

- an IMAP folder copied **without** its `.msf` files and outside any `ImapMail` folder (the
  design's stated limit, §2.2 fold);
- one more case I could not settle: whether a Panorama profile still writes `.msf` files. MA-1200
  converts a profile to Panorama; it could also record whether the `.msf` files stay. Not checked
  (§5).

Inside a profile, `FindProfileDir` finds `prefs.js`, so the store is either matched (and checked)
or `unknown`. In `planTB` itself I found no other path. **N1** (§4) is the one place this rule is
not reached.

**Can a plain mbox folder with no `.msf` be told "can't tell" wrongly?** Yes, but only toward
"can't tell", never toward "complete". The cases:

- a `prefs.js` within six levels above it, for example mbox exports kept inside a profile folder;
- any path part named `ImapMail`, or `imapmail` on Windows and macOS;
- a single-file input whose folder cannot be listed (`noMsf` returns false at `:132-133`);
- a folder holding `.msf` files from another program that uses them (SeaMonkey or Postbox, for
  example). This is the design's stated cost.

Each of these gives "Backup finished." instead of "Export complete.". The words for `no_match`
(`internal/state/tbwords.go:195-196`) say "…does not yet know how this kind of Thunderbird
account keeps its mail". That reads oddly for a folder that is not Thunderbird's. The design
already gives that wording to C3's friction review (§2.2 fold, "Words and reason").

**One observation, no action.** `Assess` decides `not_applicable` (`verdict.go:419`) before
`WalkFailed` (`:425`). So if the walk of an unmatched, no-profile folder failed before it listed a
`.msf`, the store could still come out `not_applicable`. It cannot reach complete words, though: a
source that fails makes `app.Run` return an error (`internal/app/run.go:1072-1092`). Then the
encrypted Run now note is never built (`internal/wizard/jobrun.go:274-277`), and the failure's own
words win (§13's C2 note).

## 2. MF1: can the encrypted Run now note say "Backup complete:" over a gap? — PASS

- **The lead comes from verdicts only.** `encryptedSummaryFor` (`jobrun.go:297-301`) says
  "Backup complete:" only when `tbcomplete.RunLead(result.Thunderbird)` is `LeadComplete`.
  `RunLeadWith` (`verdict.go:378-392`) gives `LeadComplete` only when every result passes
  `MaySayComplete` (`:307-313`), or when there are no results.
- **"No results" means "every store is `not_applicable`".** `assessThunderbird` drops
  `not_applicable` results (`tbcompleteness.go:230-232`). So "no results" is exactly MF1's "every
  Thunderbird store of the run is `not_applicable`, or the run had none".
- **Every other store has a result, which blocks "Backup complete:".** An `ews` store is
  `account_type`, an unmatched one is `no_match`, and an unmatched one with a `.msf` is `no_match`
  too. Each is `Unknown`, so the lead is `LeadFinished` or weaker.
- **No lead uses the path.** `IsImapStore` is used only by the wizard's prepare offer
  (`internal/wizard/wizard.go:1559`), input discovery and restore, never by a lead.
- **Every store is handed to the check.** All three local readers (`mboxReader`,
  `evolutionReader`, `Reader`) implement `WalkWithFaults`. So every local store goes through the
  branch at `run.go:1752-1767`, which plans it and adds it to `lr.tb`, even when its walk fails.
- **A fatal fault yields no note.** A fatal fault skips `assessThunderbird` (`run.go:1037`), but it
  also makes `app.Run` return an error, so the note is never built.
- **L4 holds.** `MaySayComplete` needs `len(r.NotKept) == 0`. Spam, Trash and Gmail labels not
  kept are notes, as §0's lead's call (C3 note) says.
- **Exception: N1** (§4). In that corner the store has no result, so the encrypted note would say
  "Backup complete:".

## 3. Do the code and tests match the design text? — PARTIAL (tests only; the code matches)

| Design text | Code | Test | Result |
|---|---|---|---|
| §2.2 / §4.2: `not_applicable` only with no profile, no `ImapMail` part and no `.msf` | `verdict.go:419-423`; `tbcompleteness.go:196` | `ma1196_kind_test.go:38-56` (each fact alone keeps it `unknown`); `ma1202_tb_kind_test.go:134-150` (real runs: plain folder, `Inbox.msf`, `Inbox.MSF`) | match |
| §2.2: "holds any `.msf`" = the store folder or any `.sbd` the walk enters, any kind of entry, by name only | `noteEntry` on every listed name (`localwalk.go:570`) | `ma1173_count_test.go:103-222` (`.msf` noted with Count off, `.MSF`) | match |
| §2.2: "the store's path" is the folder the walk treats as one store | `storeDir()`: the input folder, or a single file's folder (`tbcompleteness.go:115-123`) | — | matches in effect (note below) |
| §12 MA-1196: custom folder inside its profile → checked | — | `ma1202_tb_kind_test.go:91-94` | match |
| §12 MA-1196: the same folder outside every profile, **or more than six levels below it**, with `.msf` → `unknown` | as above | the outside case only, through a generic folder (`:143-150`); **no test for more than six levels below** | gap (N2a) |
| §12 MA-1196: a Local Folders copy outside its profile → `unknown`, lead "Backup finished." | as above | the verdict, through the generic `.msf` case; the lead not checked end to end | covered only by combining tests |
| §12 MA-1196: a single mbox file input with no profile (the file branch of `noMsf`, `:131-140`) | `noMsf` file branch | **none** | gap (N2b) |
| §7 / §12 MF1: an encrypted run of an unmatched store and of an `ews` store → never "Backup complete:" | `jobrun.go:297-301` | `ma1181_c3_words_test.go:123-130` uses Results built by hand (`no_match`, `account_type`); `ma1202_tb_kind_test.go:115-125` shows those stores give those Results on a **plaintext** run; `ma1181_c3_run_words_test.go:168-185` shows an encrypted run produces Results. No real encrypted run of either store | covered only by combining tests (N2c) |
| §4.2 L4: a not-kept WARN blocks complete | `MaySayComplete` (`verdict.go:311`), `TBAccount.MaySayComplete` (`tbwords.go:55-59`) | MA-1181/MA-1183 rows (not re-read in full here) | match |

**"The store's path" (no action).** The design reads it as "the folder the walk treats as one
store", noting that a parent `-input` can hold a store whose own path has an `ImapMail` part. The
code checks only the input folder, or a single file's folder. In effect they agree:

- an unmatched walk enters only `.sbd` containers, maildir folders and followed directory links,
  not plain subfolders;
- every listing it makes notes `.msf`.

So a Thunderbird store reached under a parent input is still caught by its `.msf` files. What
remains is the design's stated limit, a copy with no `.msf`.

## 4. Contradictions left

- **N1 (LOW; fold with the ADV-C2-16 follow-up, or before C3's friction review — the lead's call).**
  - **The two rules that meet.** §13's C2-pass note (ADV-C2-16) says "A real Evolution store,
    under no Thunderbird profile, is still told nothing". The code does this: `planTBOther`
    returns nil when there is no profile (`tbcompleteness.go:65-72`). The M6-residual rule says
    the opposite for this corner: an unmatched store with no profile that holds a `.msf` is
    `unknown`.
  - **What reaches the corner.** A Thunderbird store that source detection hands to the Evolution
    reader (`isEvolutionMaildirStore` or `isEvolutionCacheStore`, `internal/source/evolution.go`).
    §13 already names this for maildir-format accounts with folders named like `cur`/`new` and
    one starting with ".". Copied outside its profile, such a store gets **no result**. So
    `RunLead` says "Export complete.", and the encrypted note "Backup complete:", over a store
    that holds `.msf` files.
  - **Why it is LOW.** It needs three rare things at once: maildir storage (not Thunderbird's
    default), Evolution-shaped folder names, and a copy outside the profile.
  - **Suggested fix (one condition).** In `planTBOther`, keep a store with no profile as
    `unknown` (`other_reader`) when its folder holds any `.msf` entry, by name, from one
    `os.ReadDir`. A real Evolution store never has `.msf` files, so it is still told nothing. One
    MA-1196 case covers it.
- **N2 (LOW; tests).** No code change:
  - **a.** Add a case: a store more than six levels below a profile, holding a `.msf`, is
    `unknown`.
  - **b.** Add a case for a single mbox file input with no profile: with a `.msf` beside it, and
    without one (the positive twin).
  - **c.** Add one real encrypted run of an unmatched store holding a `.msf` and of an `ews`
    store, each checked through `encryptedSummaryFor`. MF1's own row names these two runs.
- **Wording (no action needed).**
  - §17's path-1 row still says "no profile above the input and no `ImapMail` path part" without
    "and no `.msf`". The closing note after it corrects this, but the row is not marked
    superseded. One "(see the M6-residual note below)" would stop a reader of the row alone from
    taking the old condition.
  - The closing note's "admits no `not_applicable` for any store Thunderbird keeps as IMAP" is
    true of the rule, with the stated limit, the unchecked Panorama case and N1 set aside.
- **Nothing else.** The superseded MA-1196 custom-folder case is marked as superseded. §7's
  surfaces-table "non-IMAP" is read through the wizard-lead fold, as MF1 says. C3's
  "or the run read none" equals MF1's "every Thunderbird store is `not_applicable`", because those
  stores are dropped before `RunLead`.

**What may proceed.** The L6 gate (§13) is met by this check. N1 and N2 go to the lead; neither
blocks C3's friction review.

## 5. Counters

- **Date worked on:** 8 November 2026, from the session's date.
- **Register:** a fold-check finding report. The verdict, the severities and the fixes are opinion.
- **Drew on, and reached:**
  - commits 7126111 and 4131c9b: yes, in full;
  - fold check 2 (edf9e0b): yes, in full;
  - the design at e85d840: the sections listed above, read by search and excerpt, not the whole
    file;
  - Thunderbird ESR 153.6.0esr `nsImapMailFolder.cpp` `:395-545`: yes. The brief's path
    `.claude/tbsrc-esr153` is under the main checkout, not the worktree;
  - MailArchive code at the lines cited: yes;
  - the five tests named in §3: read, and run with `-run`, all pass.
- **Not checked, per claim:**
  - whether a Panorama profile still writes `.msf` files (§1). Not in the source read;
  - whether a brand-new IMAP account has any `.msf` before its first folder is opened. Not traced;
  - N1's trigger was reasoned from the detection code (`evolution.go:107-154`), not reproduced
    with a real store;
  - the unfiltered `go test ./...` was not run;
  - the MA-1181/MA-1183 L4 tests were not re-read in full;
  - Release 159's copy of `CreateSubFolders` was not re-read (the lab note's `:435` and `:458`).
- **The smallest check that would settle the most:** the lead reads N1's two sentences (§13's
  ADV-C2-16 note and §2.2's residual rule) side by side, then picks one: either "no profile and
  no `.msf` → told nothing", or leave it as it is.
- **Borrowed lines:** short quotes from the design, fold check 2, the code and Thunderbird's
  source, each attributed where it appears.
- **Drift:** none outside the brief. N1 lies where the two checked notes meet an earlier fold.
- **Touched:** this file (new) and one commit. No design text, product code or test was changed.
- **Persona:** none.
- **Source reached, per source:** the commits yes; fold check 2 yes; the design in part (as
  listed); repo code yes (as listed); Thunderbird ESR 153 yes (one file); Release 159 no; lab
  machines no; owner profile no.
