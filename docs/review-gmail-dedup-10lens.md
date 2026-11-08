# Review: Gmail labels, store each message once (design-gmail-dedup, revision 2), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** The capture rule itself is sound: a copy is merged only when its
bytes, Thunderbird's three bookkeeping lines aside, have the same SHA-256 as the record's, and only
after that copy's exact bytes are rebuilt and checked, so a split bug can only miss a merge, never
lose a byte. Four findings are HIGH, all in what surrounds that rule: the merge step for existing
archives checks the wrong bytes against a checksum it has already thrown away (GD-1); an older
MailArchive can run in the middle of a merge (GD-2); the merge step's marker would put folder names
and Message-IDs in plain text beside an encrypted archive (GD-3); and two locked invariants (R20 for
extract, R40 for restore) forbid writing a rebuilt copy, which G6 does (GD-4). The rescue pass
dissolved all of them by named mechanism changes (§3), and in doing so found two more that must be
fixed before code (GD-25 orphan files from manifest-only merges; GD-12's sealed save dropping
`DedupStores`). Its largest simplification is adopted here: in version 1, merge only copies whose
bytes are already identical (79,153 of 79,156 on the owner's data), and drop §4.4's split and
rebuild. That dissolves GD-4 without touching R20 or R40; only if the lead keeps the split does GD-4
need an **owner ruling** (Q-A). 26 conditions in all (GD-1 to GD-26), each to be folded into
`docs/design-gmail-dedup.md` beside the text it changes, marked "Folded from the 10-lens review
[GD-n]", and re-verified. **Build may not start (slice GD1 included) until the HIGH and MED
conditions are folded** (and Q-A answered, if the split is kept). LOW conditions may be folded with
the slice they name. The design is a material change after folding (§4.4 goes), so lenses 3, 6 and
10 re-run on the folded text before GD1.

| Id | Sev | Lens | Section | In one line | Verdict |
|---|---|---|---|---|---|
| GD-1 | HIGH | 3, 7 | §9.1, §9.4, §9.5 | Merge step proves the live mbox copy, deletes the archived one, and checks a fixity it already removed | CONFIRMED; dissolved by rescue (§3) |
| GD-2 | HIGH | 8, 3 | §7.4, §9.5 | v1.6.1 can back up into a half-merged archive: manifest stays version 6 until step 5.3, index changed at 5.2 | CONFIRMED; dissolved by reorder + version 7 first |
| GD-3 | HIGH | 6, 8 | §8.9, §9.5, §9.6 | Merge marker in plain text beside an encrypted archive names folders and Message-IDs | CONFIRMED; dissolved by sealing the group list |
| GD-4 | HIGH | 1, 5, 10 | §3 G6, §4.4, §8.5, §8.6 | R20 and R40 allow only preserved originals; G6 writes rebuilt copies | CONFIRMED; dissolved by raw-identical merging; else **owner ruling Q-A** |
| GD-5 | MED | 6, 3 | §7.1, §8.1 | Alias index is not one-to-one; an alias hit skips the fill, stale-stub and full-run branches | CONFIRMED |
| GD-6 | MED | 6, 10 | §5.3, §5.4 | Listed membership attaches to the one record without checking `X-GM-MSGID`; offsets not recorded today; 1,036 rows need them | CONFIRMED |
| GD-7 | MED | 9, 3 | §7.2, §8.9 | Per-folder `LastSeen` stamped every run re-seals every bucket of an encrypted manifest | CONFIRMED |
| GD-8 | MED | 6, 7, 9 | §5.2 | Summary-file (Mork) reader under-specified: grammar, caps, memory, stability, folder enumeration | CONFIRMED |
| GD-9 | MED | 6, 5 | §6, §2.2 | prefs.js is read with a plain file read, `is_gmail` is not parsed, the account pick is random | CONFIRMED |
| GD-10 | MED | 8, 10 | §3 G7, §7.4 | v1.6.1's `serve`, `search` and `status` open a version-4 index without any version check | CONFIRMED |
| GD-11 | MED | 5 | §8.5, §8.6, OMISSION | Restore counts, extract grouping, Desktop copy export and `/goback` all assume one folder per record | CONFIRMED |
| GD-12 | MED | 8, 1 | §7.4, §9.1, §14 Q4 | One version function over all new fields; the owner's archive turns version 7 at the first backup without asking | CONFIRMED |
| GD-13 | MED | 3, 7 | §5.3, §7.2 | No state table for copy, listed and gone memberships; a partly read folder makes false listed members | CONFIRMED |
| GD-14 | MED | 5, 3 | §8.1, OMISSION | The exporter builds a fresh record on every write; new fields must be carried forward | CONFIRMED |
| GD-15 | MED | 10 | §3, §7.3, §10, §15 | Claims to label: "byte for byte" is the reader's form; manifest size; Problem B "no mail lost" | CONFIRMED |
| GD-16 | MED | 1, 3 | §12 | The merge step's deletion rule is not an invariant; R3, R6, R20, R40 wording | CONFIRMED |
| GD-17 | LOW | 7 | §9, §8.2 | One stored copy now serves every folder; memberships live only in the manifest | CONFIRMED |
| GD-18 | LOW | 4 | §8.3, §9.3, §8.1 | Words: the merge offer, "This folder" leads to All Mail, second computer's refusal | CONFIRMED |
| GD-19 | LOW | 9 | §8.1, §8.2, §8.7, §11 | Summary pass every run, merge-step reading time, verify cost, index query shape | CONFIRMED |
| GD-20 | LOW | 2 | §1, §7.2, §13 | Ship Problem A and Problem B separately; defer per-folder gone | CONFIRMED |
| GD-21 | LOW | 5, 6 | §8.4 | Cross-folder static links: acceptable here, with three limits | CONFIRMED (go-back's objection does not apply) |
| GD-22 | LOW | 10 | §8.8 | Redaction has no tombstone: a deleted Gmail message returns at the next backup | CONFIRMED |
| GD-23 | LOW | 6 | §4.4 | Split rule wording: header-line start, continuation, empty line, case | CONFIRMED (fails safe) |
| GD-24 | LOW | 10 | BUILD STATUS, §2.2, §8.3 | Status line names the wrong review file; three citation errors | CONFIRMED |
| GD-25 | MED | 7, 6 | §9.4 | "Merged in the manifest only, their files kept": files nothing owns, redaction misses them | CONFIRMED (rescue N2) |
| GD-26 | MED | 5, 3 | §9.2, §5.3 | Sent Mail fills in on an unmerged archive, but placement assumes one record per email | CONFIRMED (rescue N3) |

- Date worked on: 7 November 2026 (the session's date).
- Design reviewed: `docs/design-gmail-dedup.md` at commit 9203aae (branch `gmail-dedup`, off main
  0923ffa = v1.6.1), sha256 `b0b06e27e3d4d80ae3f85d08f2ef02203efc552c62ad45f5feb84acb76e1358b`.
- File name: the caller asked for `docs/review-gmail-dedup-10lens.md` (the shape of
  `review-restore-thunderbird-10lens.md`). The design's status line and §13 name
  `docs/review-gmail-dedup-predesign.md` (the kit's convention, `design-review.md:7`). GD-24 fixes the
  design to cite this file.
- Procedure: `../assurance-kit/process/design-review.md`. Read-only; no product code was run, no
  Thunderbird was run, nothing in the design was edited.
  - Lens passes 1 to 10 and the skeptic's code check: the synthesiser (this file's author) read the
    load-bearing code directly (§1).
  - Citation sweep: one read-only agent checked every other file:line in the design and the go-mbox
    v1.0.4 module source; its load-bearing results were re-read by the synthesiser (marked
    "re-read").
  - Data probe: one read-only agent measured counts and structure on the owner's Thunderbird profile
    and archive metadata (§6). No subject, address, body or Message-ID was printed.
  - Rescue pass: one separate agent, given GD-1, GD-2, GD-3, GD-5, GD-6 and GD-7 as blockers and told
    to save the design (§3).
- Register: review synthesis (findings, the synthesiser's decisions, and opinion where marked).
  Statements about Thunderbird's internals that are the reviewers' recollection, not read, are marked
  **(recollection, unverified)**.

## 1. Claims checked against the code (the skeptic's part)

### 1.1 Read by the synthesiser

| Design claim | Where read | Result |
|---|---|---|
| A copy's "raw bytes `B`" are what Thunderbird holds; rebuild gives "exact originals" (§3 G3, G6, §4.4) | `go-mbox@v1.0.4/reader.go:29-75`, `:91-127`; `internal/source/localwalk.go:471-494`; `internal/export/exporter.go:443-515` | **Narrower than stated.** The reader drops the mbox `From ` line, rewrites every line ending to CRLF (`reader.go:59`, `:69`), takes one `>` off a line starting `>From ` (`:62-63`) and drops the blank line before the next separator (`:45-57`). `m.Raw` is that output, and the `.eml` is `m.Raw` (`exporter.go:515`). So "byte for byte" means "as MailArchive's reader gives them", never the mbox file's bytes. The rule is no weaker for it (both copies go through the same transform), but the claim must say so. → GD-15, GD-23 |
| Thunderbird's lines are kept in the raw bytes (§4.3) | `internal/source/mbox.go:132-142`, `reader.go` | Confirmed: nothing filters headers; the parser only reads `X-Mozilla-Status` and `X-Mozilla-Keys`. |
| The capture records "the copy's position in its mbox file (`Offset`)" (§5.3 step 3) | `localwalk.go:471-499`; `reader.go:78-87` | **Does not exist today.** The library wraps its own buffered reader and exposes no position; the walk keeps none; `skipBOM` and `wrapMboxStream` sit underneath. An offset in Thunderbird's convention needs a counting reader below the library. → GD-6 |
| Key placement: plain key, qualified key on a fingerprint mismatch (§2.2, §8.1) | `exporter.go:207-311` | Confirmed, and wider than the design uses: one `(token, folder, identity)` can hold the plain key, `key\x00fp`, and further qualifications (`:298-311`). → GD-5 |
| "alias hit: … stamp `LastSeen` and skip" is today's skip (§8.1) | `exporter.go:313-376`, `:404-408` | **Not today's skip.** A key hit on a seen record also runs the fill retry (`Fillable`), the stale-stub and legacy-placeholder re-examination, `supersedePartial` and `StampParseScheme`, and Full mode re-exports it. On the local path a re-export writes under the *current* folder (`writeFolderPath := folderPath` unless `DedupMailboxWide`, `:404-408`), so a Full run that met a member folder's copy would write the home record's files into the member's directory. → GD-5 |
| `save` always stamps the current version (§7.4) | `internal/state/manifest.go:2148-2157` | Confirmed. |
| Load refuses a newer version (§7.4, G7) | `manifest.go:779-786` | Confirmed for the manifest. **Not for every reader**: `index.OpenReadonly` (`internal/index/index.go:233-256`) has no version check, and `serve`, `search` and `status` use it without loading the manifest (sweep, re-read). → GD-10 |
| The sealed manifest carries the same fields (§7.4, §8.9) | `manifest.go:744-768`, `:2156-2157`; `internal/container/record.go:39-64` | **Partly.** Record fields ride inside `sealedRecord` (`record.go:61-64`), so `Folders`, `NormSHA256` and `MergedFiles` are carried. `DedupStores` is a manifest field: neither `state.View` (`manifest.go:744-750`) nor the sealed header (`record.go:39-51`) has a slot for it. And the sealed record's `LastSeen` uses a "latest walk" marker so a steady run changes no bucket (`record.go:53-82`); per-member `LastSeen` would undo that. → GD-7, GD-12 |
| `Fixity` per file; the merge step checks "recorded fixity" (§9.4, §9.5) | `manifest.go:143-148`, `:273-283` | Confirmed `HTML`, `Zip`, `EML` digests, each optional ("empty on records written before fixity"). `MergedFiles []string` (§7.1) carries none, and the copies' records (which hold them) are removed at step 5.3, before step 5.5 checks. → GD-1 |
| Restore keeps a key in its first node only (§8.6) | `internal/app/restoreread.go:299-318`, `:403-436`; `internal/tbengine/plan.go:355-372` | Confirmed, and wider: `RestorableCounts` also dedupes by key and promises "the counts add up to what a whole-archive plan restores" (`:403-407`); `plan.go:372` mixes per-record counts (`sel.Records - sel.Restorable`) with per-folder `p.Messages`. → GD-11 |
| Extract: one output folder per `Path` directory (§8.5) | `internal/app/extract.go:225-287` | Confirmed: one walk item per record, sorted by the sanitized `Path` directory, one mbox open per folder. A member folder needs one item per (folder, record) before the sort, and the exporter's directory spelling for it. → GD-11 |
| R20 and R40 hold (§8.5, §8.6) | `docs/scenario-catalog.md:148-160`, `:380-395` | **They do not, as worded.** R20: "by copying only each record's PRESERVED original bytes … it never synthesizes or re-serializes a message." R40: "it writes only preserved originals (for an mbox source, the bytes the archive kept)". A copy rebuilt from the home original plus another folder's lines is neither. → GD-4 |
| Redaction deletes `Path` and `AlsoFiles` (§8.8) | `internal/app/reindex.go:152-186` | Confirmed. No tombstone is kept (`manifest.Delete(g.key)`, `:184`), so the source's copy comes back at the next backup. → GD-22 |
| `AlsoFiles` are not owned by verify (§8.7 side note) | `internal/app/verify.go:314-321` | Confirmed: only `Path`'s stem and its `.zip`/`.eml` siblings are owned. |
| prefs.js is read "through the existing contained reader (R25), with `thunderbird.FindAccountForStore`" (§6) | `internal/thunderbird/thunderbird.go:60-110` | **Wrong premise.** `parsePrefs` is `os.ReadFile` (`:63-64`), not the contained reader; nothing parses `is_gmail`; `FindAccountForStore` iterates a map, so with two accounts whose host is `imap.gmail.com` the pick is random (`:86-104`); `strings.HasSuffix(rel, want)` is a loose match. → GD-9 |
| Static folder pages are regenerated, not durable references | `docs/scenario-catalog.md:254-259` (R28), `internal/app/reindex.go:191-195` | Confirmed: "Folder pages are a regenerated view, not a durable reference." This is what makes cross-folder links acceptable here (§2, area 7; GD-21). |

### 1.2 From the citation sweep (load-bearing rows re-read)

- Confirmed as cited: exporter.go:219-225, :227-233, :155-160, :410-433; manifest.go:413-420, :437-448,
  :383-388, :1721-1736, :1645-1659, :422-435, :150-165, :1696-1719; run.go:795-804, :927-929,
  :1881-1901, :1674; graph.go:251, :341-401 (no history event for collapse losers: confirmed);
  localrun.go:415 (re-read), :244-254; source.go:77, :151; mboxreader.go:21; localwalk.go:506, :547;
  wizard.go:1554-1575; message.go:318-327 (re-read), :340-356 (re-read), :376-406; plan.go:664-688,
  :336-339, :603; words.go:73; mbox.go:132-142 (re-read), :280-293 (re-read); html.go:2237-2238,
  :2176-2185 (re-read); rawstub.go:51-61 (re-read); index.go:27-34 (re-read), :41-63 (re-read),
  :162-164 (re-read), :431-458; query.go:141-142, :303; listfolder.go:227-262; reindex.go:396-399,
  :158 (re-read), :176-178 (re-read); sealedindex.go:263; decryptcopy.go:457; copyexport_archive.go:489,
  :530-542, :855-862; server/page.go:57, :123, :135; server.go:61, :79-86; goback.go:137-180;
  pages.go:122, :144-146; pagequery.go:19; keyring/reader.go:560; extract.go:129, :273-287 (re-read),
  :413, :452; restoreread.go:221-231 (re-read), :260 (re-read), :315 (re-read); tbengine/run.go:216-245;
  tbrestore/write.go:387-408; verify.go:194, :316-323 (re-read), :436, :615; archdelete/lister.go:205,
  :882, :833; container/record.go:61-64 (re-read); units.go:23-26, :64-73; scenario-catalog.md:24-33,
  :435.
- Wrong or loose: `internal/app/ws_archive.go:448-499` is `internal/desktop/ws_archive.go:448-499`;
  "only the wizard calls it" — `cmd/mailarchive/main.go:2121` calls `FindAccountForStore` too;
  `exporter.go:207-311` is the key-placement part of `Export`, which runs to `:817`; the `AlsoFiles`
  field is `manifest.go:136-141` (`:399-404` is `CollapseLoss`'s comment); `statusLine` starts at
  `html.go:2104`. → GD-24
- Also found: `ContentHash` is set on every write, local records included (`exporter.go:716`); the
  field's comment at `manifest.go:121-123` ("Empty … on folder-scoped local records") is out of date.
  The exporter carries `AlsoFiles` forward only on the Microsoft 365 path (`exporter.go:763-765`).
  `/goback` lists local records too, grouped by `rec.Folder` (`goback.go:144-152`, `:237-238`).
  Restore's `KeepsTimeline` flips once any record has a non-zero `Record.LastSeen`
  (`restoreread.go:279-283`). The Desktop's archive copy export (`internal/desktop/ws_archive.go`,
  `app.OpenArchive`, `PlanCopyExport`) is a reader of folders that the design does not list.
- Free ids: R42 to R45, S75 to S84 and MA-1151 to MA-1234 are unused on this branch (highest S74,
  highest MA-1150).
- Not checked: the go-back gate digest that states F5 and GB-6 in full (not in the repository; the
  review names them only, `review-goback-predesign.md:49-50`); the Thunderbird source of §2.1.

## 2. The ten attack areas the caller named

### Area 1 — header layouts that break the rebuild or make two emails look the same

The rule fails safe by construction: step 4 of §4.4 rebuilds `B` from `N` and `T` and compares
SHA-256 before anything is merged, so any split bug (a folded line, a duplicate line, a line in an
odd place) turns into "not merged", which is today's behaviour. What remains is the false-merge
direction: two copies whose `N` are equal but which are different emails. They differ, by
definition, only in lines named `X-Mozilla-Status`, `X-Mozilla-Status2` or `X-Mozilla-Keys` in the
top header block, and each folder keeps its own lines, so each folder's copy still rebuilds. The
attacks, one by one:

- **Folded lines, duplicates, odd positions.** Handled by "with its continuation lines, each kept
  verbatim with its position" and the 8-line, 1 KiB bound; the rebuild check catches any error.
  Wording gaps (what starts a header line; continuation of a non-bookkeeping header that happens to
  read `X-Mozilla-Keys:`) are GD-23. Fails safe either way.
- **CRLF and LF.** Cannot arise for an mbox source: the reader rewrites every line ending to CRLF
  before the split ever sees the bytes (§1.1). Two copies differing only in line endings are already
  equal today. Maildir and Evolution keep their bytes, but they are out of scope (§6).
- **A Thunderbird line in the body.** Ignored: "Nothing after the first empty line is looked at".
  The empty line must be found the same way `headerBlock` finds it (`mbox.go:84-101`): the first
  `\r\n\r\n` or `\n\n` within 64 KiB. GD-23.
- **A sender-written `X-Mozilla-Keys` or `X-Mozilla-Status`.** A sender can put these names in a
  message. Two messages identical except in such a line merge; each folder's bytes still rebuild,
  so nothing is lost. The page and the reader show the home copy's "Categories"; a member's own
  categories are only in its `Moz`. Acceptable; noted in GD-23.
- **mbox From-line quoting between folders.** The `From ` separator is never part of `B` (the
  reader consumes it), so its per-folder date never matters. A body line `>From ` loses one `>` in
  every folder alike. A copy quoted differently from another (`From ` versus `>From ` in mboxo's
  ambiguous form) gives different `N` and is not merged, or gives the same `N` and was already
  indistinguishable today. **Refuted as a new risk**, with the honesty condition GD-15.
- **Two different emails, one Message-ID, equal envelope.** Already one record today in the same
  folder (`message.go:340-347`: "Two messages that reuse one Message-ID with an identical envelope
  and differ only in body are treated as one"). The merge rule is stricter than that (whole bytes),
  so it adds no new collapse.

### Area 2 — the merge step's crash order and deletion

The deletion is where the design is weakest (GD-1, GD-2, GD-16). As written:

1. The proof is computed from the wrong copy. §9.1 stamps `NormSHA256`, `CopySHA`, `Offset` and the
   lines "from the copy it reads anyway", that is the live mbox copy. The file §9.5 deletes is the
   copy's archived `.eml`, captured on an earlier day. Thunderbird rewrites `X-Mozilla-Status` in
   place when a copy is read or flagged (§4.3), so the live copy's lines can differ from the archived
   copy's. Deleting the archived `.eml` and keeping the live copy's lines in `Moz` loses the archived
   copy's exact bytes: the very file the archive held.
2. The safety check reads a checksum that is gone. Step 5.5 deletes "only if its bytes still match
   its recorded fixity", but step 5.3 has already removed the copy's record, which held `Fixity`,
   and `MergedFiles` is a list of paths.
3. A record without `Fixity` (written before fixity) cannot "pass" it; the design must say it is then
   manifest-only.
4. The page and the zip of a copy are not proven by the `.eml` comparison. They are deleted as
   derivable, not as byte-identical: the claim must say so (GD-15).

The crash order is otherwise right (anchor save before deletion; marker until done; a backup refuses
meanwhile) apart from the older-binary window (GD-2). The rescue's changes are in §3.

### Area 3 — versions 6 and 7, older MailArchive, a second computer

- Writing 7 only when needed is sound if one function decides it over every new field (GD-12). As
  written the rule names `DedupStores` and `Folders`, not `NormSHA256` or `MergedFiles`, and the
  sealed path has no `DedupStores` slot.
- **v1.6.1 on the same archive refuses its backup, verify and Desktop folder list**, cleanly
  (`manifest.go:784-786`; `listfolder.go:187-189`). **It does not refuse `serve`, `search` or
  `status`**: they open the index read-only without a version check (`index.go:233-256`) and never
  load the manifest. They write nothing, so nothing is corrupted, but they show each email in its
  home folder only (a near-empty Sent Mail again). G7's "refuse by name" over-claims. GD-10.
- **A synced folder used by two computers.** The archive lock is per computer, so two writers on a
  synced folder were already out of scope (README rule, quoted at `manifest.go:781-783`). New here:
  the moment v1.7 writes version 7, the other computer's scheduled backup fails every run (alerts,
  then the auto-pause after 30). Not corruption; the words must say what to do (GD-18).
- **The owner's archive turns version 7 at its first backup**, because §9.1 stamps evidence and Q4
  adds listed memberships "without asking". That is a one-way format change the person did not
  choose. GD-12 and owner question Q-B.
- **Encrypted archives decode strictly** (`record.go:171-181`, `DisallowUnknownFields`, per the
  sweep), so a v7 record mis-stamped 6 would make v1.6.1 call the archive damaged rather than
  newer. Another reason for the single version function.
- **Rollback.** None stated. A v1.7 archive could be written back as version 6 by dropping the
  new fields: v1.6.1 would then see one record per email in its home folder, valid for it, and would
  recapture the other folders' copies as new records on its next backup (more files, nothing lost).
  GD-12 asks the design to state this, and Q-C asks the owner whether to build it.

### Area 4 — the summary-file (Mork) reader as an untrusted-input parser

The worst case the caller set ("an email shown in an extra folder, never a lost or altered byte")
holds by construction: a listed membership "never creates a record, writes a file, or changes a
record's bytes" (§5.3). What is missing is the specification that makes the reader safe and
predictable (GD-8) and a matching rule whose stated bound is true (GD-6):

- Grammar subset and caps with numbers, no recursion, a memory budget (the value dictionaries come
  before the rows that use them, so a one-pass reader holds every value, subjects and addresses
  included, unless it is two-pass or keeps only the atoms the three columns reference).
- Which flag means "deleted" (recollection, unverified: Thunderbird's `Expunged` 0x8 and
  `IMAPDeleted` 0x200000), and that a deleted row adds nothing.
- Read only a stable file (size and modification time equal before and after; Thunderbird running
  writes it), and refuse to mark anything gone from a summary that changed while read.
- The folder of a summary file. Folders with only a `.msf` are never walked (Promotions, Business
  Papers - Corporation: the sweep, `source.go:151`). The pass must list `.msf` files itself, build
  folder paths exactly as `mboxDir` does (`localwalk.go:537-547`, `.sbd` nesting), and apply the
  walk's leave-out rules (R25 containment, dot names, R41 restore leftovers).
- Message-ID spelling: compare exactly the string the identity uses after `mid:`, never case-folded;
  Thunderbird's synthetic ids for a message with none (`md5:` values, one each in All Mail and Important, §6) never match.
- `X-GM-MSGID` collisions: unique per Gmail account (recollection), so a collision is a damaged or
  hostile file; it can only place a message in an extra folder, and must be counted.
- Compaction: Thunderbird rewrites positions when it compacts a folder. Offsets must come from this
  run's walk, with the mbox unchanged between the walk and the summary read (GD-6).

### Area 5 — Gmail detection spoofing

prefs.js and the folder flags are written by the person's own Thunderbird; anyone who can write them
already controls the profile, which is outside the threat model. The real exposure is
over-detection: `is_gmail` follows the server's Gmail extension, and the All Mail flag 0x8000 is set
for any IMAP server that marks a folder as holding all mail (recollection, unverified). On such an
account, Problem A still merges only copies whose bytes match (safe), and Problem B attaches by
Message-ID with no `X-GM-MSGID` to check (bounded to an extra folder). The words "Gmail:" may then be
wrong. GD-9 fixes how prefs.js is read and which account is picked.

### Area 6 — restore into every folder; extract per folder

- Writing the message into every chosen folder recreates in Thunderbird the copies Thunderbird
  itself had, in a new folder of Local Folders (R40's place). That is faithful, not harmful, and R40's
  "only adds" holds. What breaks is R40's other sentence, "it writes only preserved originals": a
  copy rebuilt from the home original and another folder's lines is not a preserved original. Same
  for R20 ("never synthesizes or re-serializes a message"). GD-4.
- The restore's counts and space check assume one folder per record (GD-11).
- A listed member gets the stored original, which carries the *home* copy's bookkeeping lines
  (All Mail's read and tag state). Thunderbird will show those. The plan should say so (GD-18).
- Extract: one item per (folder, record), the exporter's directory spelling for a member folder, and
  a rule for two records whose file stems meet in one member folder (`-format eml`) (GD-11).

### Area 7 — search index, Desktop counts, static cross-folder links

- New table versus older readers: GD-10 (older `serve`/`search` read it silently); redaction's
  `DeleteByID` must also remove `doc_folders` rows (GD-19).
- Desktop folder counts adding above the total: the design already words it ("233,322 messages in 17
  folders", never a sum). Accepted.
- **Static cross-folder links.** The go-back design rejected them because Microsoft 365 folders
  change every run and the static view was to stay "grouped by each message's first-captured folder
  (stable, R13-clean, no cross-directory links)" (`design-goback.md:38-40`, `:168-171`). Here the
  link points at the home file, which never moves (R13), and folder pages are "a regenerated view,
  not a durable reference" (R28, `scenario-catalog.md:258-259`) rebuilt at every run and every
  reindex (`reindex.go:191-195`). Gmail membership changes rarely, and a stale page is corrected at
  the next run. So the objection does not carry over. Limits in GD-21.

### Area 8 — encrypted archives

Members are inside the sealed record, and the summary files are read from the profile, so nothing new
sits in the archive's plain-text files **except** the merge marker (GD-3) and any note or status line
that names a folder (GD-3). `DedupStores` needs a slot in the sealed header (GD-12). Per-member
`LastSeen` defeats the sealed manifest's unchanged-bucket rule (GD-7).

### Area 9 — performance on 312,761 records

Nothing here is out of reach (§6 numbers). The design should state: the summary pass's cost per run
and a skip for an unchanged summary (the hourly schedule exists); the merge plan's reading of every
kept original (tens of GB) with progress and cancel; verify's extra rebuild hashing; the index
query shape so a folder filter stays indexed. GD-19.

### Area 10 — is keeping Thunderbird's lines per folder worth it?

See §3, "Split and rebuild". In short (opinion, after the rescue): **no, not in version 1.** It fails
safe, but on the measured data it buys almost nothing (79,153 of 79,156 live copies are identical
including Thunderbird's lines; none of the other three differs only in them), and it costs a new
rule at the false-merge spot, a per-folder `Moz` on every member, a rebuild in extract, restore and
verify, and a clash with R20 and R40 (GD-4). Merge only byte-identical copies; measure the
lines-only differences before adding the split later (Q-L1).

## 3. Rescue pass

One agent, separate from the finders and the sweep, was given GD-1 (B1), GD-2 (B2), GD-3 (B3), GD-5
(B4), GD-6 (B5) and GD-7 (B6) and told to save the design. It read the design, the procedure's rescue
section and the code. Its outcomes, checked by the synthesiser against the cited lines:

| Blocker | Outcome | The change |
|---|---|---|
| B1 merge evidence (GD-1) | DISSOLVED-BY | `NormSHA256`, `T` and `CopySHA` of a record with a kept original come from that record's own `.eml`, only after it matches `Fixity.EML`; the live copy may group, never prove. `MergedFiles` entries become `{path or unit, size, sha256}` copied from the copy's fixity at step 5.3. Just before each deletion, the survivor's `.eml` is re-checked against its fixity and must rebuild the copy's digest. No recorded EML fixity: never deleted. |
| B2 older binary mid-merge (GD-2) | DISSOLVED-BY, and partly refuted | Partly refuted: the evidence backup (§9.1) already creates `Folders` entries (`CopySHA`, `Offset`, `Moz` exist only on a membership), so the manifest is version 7 before any merge; and v1.6.1's backup refuses a version-4 index (`run.go:393`, when indexing is on, `run.go:840`; re-read). The change: step 5.1 saves the manifest at version 7 with a `MergePending {planDigest, groups}` field before the index is touched; the merge refuses to start unless that save is version 7. |
| B3 encrypted marker (GD-3) | DISSOLVED-BY | The same `MergePending` field, sealed on an encrypted archive; `.mailarchive-merge.json` is dropped altogether (after 5.3 everything needed is in `MergedFiles`). The folder-naming note is refuted *by precedent* if it goes through the capture-gap path, which is sealed (`localwalk.go:114`; `CaptureGaps` in `View`); the Gmail counts go into the sealed `RunDetails`, not the plain status file. |
| B4 alias path (GD-5) | DISSOLVED-BY | Alias maps to a set of keys; keep those whose `Fingerprint` matches; exactly one → `key :=` it and run the existing seen-record path (`exporter.go:264-376`) unchanged; none or several → a miss, go to §4.4. A seen Gmail record is written under its home folder (mirroring the Microsoft 365 `FirstFolder` rule, `:404-408`). Full mode re-exports only from the home folder's copy. §16.4's worry refuted: the alias's fingerprint test has the same within-folder limit as today's plain-key test. |
| B5 listed bound (GD-6) | DISSOLVED-BY (amended by the probe: step 3 kept, see GD-6) | Offsets cannot be had without replacing the reader (go-mbox exposes no position). So: drop §5.3 step 3 and `Offset`; place a listed membership only when the Message-ID maps to exactly one `X-GM-MSGID` across every summary read this run AND exactly one candidate exists (a candidate being a class of records proven byte-equal, N3); otherwise count. §5.4 then reads "needs a reused Message-ID whose other email appears in no summary file that was read". |
| B6 encrypted churn (GD-7) | DISSOLVED-BY | `sealSeen`/`openSeen` (`record.go:75-105`) also marker-encode each membership's `LastSeen` equal to the walk stamp; the local run calls `WalkDone(token, runAt)` (today only Microsoft 365 does, `graph.go:502`) only after a complete walk of the Gmail store. Fallback: per-folder `LastSeen` deferred (GD-20). |

New problems the rescue found (folded as conditions):

- **N1 (must fix)** — the sealed save loses `DedupStores` at every save: `save()` commits only
  `View{Version, Entries, Stores, CaptureGaps, CollapsedTokens}` (`manifest.go:2156-2157`, re-read)
  and `FromSealed` restores only those. → GD-12(b), raised to a named test.
- **N2 (must fix)** — "Groups without that proof are merged in the manifest only, their files kept"
  (§9.4) leaves files nothing names: verify does not own them, and redaction and archive delete reach
  only `Path`, `AlsoFiles` and `MergedFiles`; a redacted message's copy would stay on disk and be
  served. → GD-25.
- **N3** — §9.2 fills Sent Mail at the first backup of an *unmerged* archive, but §5.3 step 2 assumes
  "after Problem A's merging"; an old archive has several records per identity, and without step 3
  they would all be counted and never placed. → GD-26.
- **N4** — §7.3's "the manifest gets smaller" and G6's "byte for byte": already GD-15.

**Split and rebuild (area 10).** The rescue recommends dropping §4.4's split/rebuild in version 1 and
merging a copy only when its raw SHA-256 equals the record's (a `RawSHA256` recorded even without
`-raw`), splitting each identity's records by digest rather than all-or-nothing. Its reasons, checked:
the live copies match All Mail's byte for byte, Thunderbird's lines included, in 79,153 of 79,156
cases (§10.1), and none of the other three differs only in those lines; the 5,285 "some original
differs" groups are an upper bound that counts a whole group as different if any one copy differs
or lacks an `.eml` (the design's own probe script), so a digest split recovers part of it with no
new rule. Dropping the split removes `Moz`, `T`, every rebuild in extract, restore and verify, and
the GD1 adversarial gate; every deletion becomes "byte-identical to a kept, fixity-checked file";
and **GD-4 dissolves**, because the stored original then *is* every member folder's copy, which R20
and R40 already allow. **The synthesiser adopts this recommendation** (opinion, replacing the first
draft's "keep the rule, store `Moz` only when it differs"): it is the smaller mechanism with the same
property on the measured data. A later version may add the split if a counts-only probe shows a
material number of copies that differ only in Thunderbird's three lines (Q-L1).

## 4. Conditions

Each condition: severity, lens, the scenario it closes, and the fix to fold into the design.

**GD-1 (HIGH; lenses 3, 7) — the merge step proves the wrong bytes.**
Scenario: an INBOX copy was archived in March, unread; in May the owner reads it in Thunderbird, which
rewrites its `X-Mozilla-Status` in place; the next backup stamps the May lines and checksum (§9.1);
the merge step deletes the March `.eml` and keeps the May lines; extract and verify now rebuild the
May copy and the March file is gone. Separately, step 5.5's fixity check reads digests the step
removed at 5.3. Fix: (a) at plan time, the proof comes from the copy's own archived `.eml`, after
checking that file against its `Fixity.EML`; never from the live mbox (the live copy may group, never
prove); with raw-identical merging (GD-4) the proof is simply "the copy's `Fixity.EML` digest equals
the survivor's"; (b) a record with no recorded `Fixity.EML` is never offered for deletion and, per
GD-25, not merged either; (c) `MergedFiles` entries carry `{path or unit, role, size, sha256}`
copied from the copy's `Fixity` at step 5.3; (d) immediately before each deletion, re-check the
survivor's `.eml` against its fixity and the file about to be deleted against its own digest, and
require the two `.eml` digests equal (or, if the split is kept, the rebuild to give the copy's
digest); any mismatch keeps the file, owned and reported (GD-16);
(e) the copy's page and zip are deleted only together with a proven `.eml`, and the design says they
are deleted as derivable, not as byte-identical; (f) deletion goes through the contained deleter
(validated relative path, no link followed), as redaction does (`reindex.go:159-166`).

**GD-2 (HIGH; lenses 8, 3) — an older MailArchive inside a half-done merge.**
Scenario: the merge is interrupted after step 5.2 (index rows dropped) and before 5.3; the owner's
other computer, still on v1.6.1, opens the archive: the manifest is version 6, the marker means
nothing to it, and it backs up into an archive whose index no longer matches its manifest. (Partly
refuted by the rescue: the evidence backup already makes the manifest version 7, and v1.6.1's backup
refuses a version-4 index when indexing is on, `run.go:393`, `:840`; but `-no-index` runs and the
version rule's exact trigger leave the gap open.) Fix: the first durable act of the yes is a manifest
save at version 7 carrying `MergePending {planDigest, groups}`; the merge refuses to start unless that
save is version 7; then the index; then the records; then pages; then deletions; then clear
`MergePending`. The `.mailarchive-merge.json` marker is dropped. Every binary that cannot finish the
merge then refuses from the first step on. MA-1229 adds a crash at each step plus "v1.6.1 opens the
archive at each step: refuses".

**GD-3 (HIGH; lenses 6, 8) — plain text beside an encrypted archive.**
Scenario: an encrypted Gmail archive's merge writes `.mailarchive-merge.json` "listing the groups";
group keys are `token\x00folder\x00Message-ID`, so folder names and Message-IDs sit in plain text,
which the encrypted design forbids ("encrypt names and structure"). The same for any run note,
status line or run-history line that names a folder ("Could not read Thunderbird's list for Sent
Mail"). Fix: no marker file at all; the group list is GD-2's `MergePending`, sealed in an encrypted
archive; the summary reader reports through the existing capture-gap path, which is sealed
(`localwalk.go:114`; `CaptureGaps` in `View`); the Gmail counts go into the sealed `RunDetails`;
status and run history in plain text carry counts only, never a folder name. MA-1230 asserts no
folder name or Message-ID appears in any plain file after each step.

**GD-4 (HIGH; lenses 1, 5, 10) — R20 and R40 forbid rebuilt copies. Owner ruling Q-A.**
Scenario: extract `-format eml` or a restore writes Sent Mail's copy rebuilt from All Mail's
original plus Sent Mail's lines. R20 says extract copies "only each record's PRESERVED original
bytes … never synthesizes or re-serializes a message"; R40 says restore "writes only preserved
originals". The design's "R40 holds" (§8.6) is false as R40 is worded. The reviewer cannot amend a
locked invariant. Fix (recommended, from the rescue): version 1 merges only copies whose raw SHA-256
equals the record's `Fixity.EML` (or a new `RawSHA256` recorded on every Gmail record, `-raw` or
not); §4.4's split and `Moz` go; extract and restore write the stored original into every member
folder, which is then byte for byte that folder's copy, so R20 and R40 hold as worded. Only if the
lead keeps the split does this become owner ruling Q-A: amend R20 and R40 to admit "a member
folder's copy rebuilt from a preserved original and its recorded lines, checked against that copy's
recorded SHA-256 before it is written; a mismatch is skipped and listed".

**GD-5 (MED; lenses 6, 3) — the alias path.**
Fix: (a) the alias index maps `(token, folder, identity)` to a set of `{key, fingerprint}`, the shape
`byIdent` already has (`manifest.go:383-397`), and a hit tests every entry with `sameFingerprint`;
(b) on a hit the exporter sets `key` to that record's key and runs the existing seen-record path
(`exporter.go:313-376`) unchanged: fill retry, stale-stub and legacy re-examination,
`supersedePartial`, `StampParseScheme`; (c) Full mode re-writes a merged record only from its home
folder's copy; a member folder's copy only stamps; (d) the KEEP guard (`:227-233`) runs on the alias
key too, so a degraded member copy never displaces a complete home record; (e) MA-1214 gains a
Full-mode re-run and a fillable home record healed by a member's healthy copy (or, if the design
chooses not to heal across folders, says so and counts it).

**GD-6 (MED; lenses 6, 10) — listed-membership matching.**
Scenario: Sent Mail's summary lists a message whose body is on no folder of this computer, with
Message-ID M; the archive holds one different email with Message-ID M; step 2 ("Exactly one
record") attaches it to Sent Mail without looking at `X-GM-MSGID`. §5.4's bound ("needs a broken
`X-GM-MSGID` chain AND a reused Message-ID") is therefore false. Separately, step 3's `Offset` does
not exist: go-mbox exposes no position (`reader.go:78-87`) and the walk keeps none. The rescue
proposed dropping step 3; the probe shows that would leave **1,028 Sent Mail rows and 8 Business
Papers rows unplaced** (their Message-ID matches more than one All Mail copy, §6), and it proved the
convention an offset needs: `storeToken` is the byte offset of the copy's `From ` line (6,803 of
6,803). Fix: (a) place a listed membership directly only when the row's Message-ID has exactly one
candidate (one record, or one class of byte-identical records, GD-26) AND, when the row has an
`X-GM-MSGID`, that id names no other candidate in any summary read this run; (b) otherwise use step
3, with `Offset` defined as the `From ` line's byte offset in the file, taken by the walk's own
ordered scan of separators (not from go-mbox, which hides positions), recorded only for this run, and
used only when the mbox's size and modification time are the same before the walk and after the
summary read; the chain is restricted to the Message-ID's candidates; (c) a row with an empty
`X-GM-MSGID` (INBOX: 10,796 of 10,881, §6) is placed only by (a); (d) anything else adds nothing and
is counted; (e) restate §5.4's bound: "needs a reused Message-ID and a broken or stale offset
chain"; (f) lead's call (Q-L5): if step 3 is cut from version 1 instead, the status line says "1,036
messages Thunderbird lists in Sent Mail and one other folder could not be placed".

**GD-7 (MED; lenses 9, 3) — encrypted bucket churn.**
Scenario: every scheduled run stamps `LastSeen` on 312,000 memberships; the sealed manifest's rule
that a steady run changes no bucket (`record.go:53-57`, P2b-C12) no longer holds; every bucket is
re-sealed and rewritten every run. Fix (rescue B6): `sealSeen`/`openSeen` (`record.go:75-105`) also
marker-encode each membership's `LastSeen` that equals the walk stamp, and the local run calls
`WalkDone(token, runAt)` (today only Microsoft 365 does, `graph.go:502`, re-read) only after a
complete walk of the Gmail store. Or (simpler, with GD-20) keep only `FirstSeen` per membership in
version 1 and add `LastSeen` and `Gone` with the per-folder history design Q5 already defers.

**GD-8 (MED; lenses 6, 7, 9) — the summary-file reader's specification.**
Measured shape to design against (§6): bare-CR line endings; dictionary ids wrapped by a line break
and spaces before `=`; 162,978 groups and 584,041 row updates in All Mail's file; nesting depth 2;
1.8 million values; a 29,016-byte cell; the file changing while Thunderbird runs.
Fix, written into §5.2 before GD5 starts: (a) the grammar subset read (dictionaries, rows, tables,
groups; escapes `$xx` and `\`; aborted groups; row removals and cell updates), and what any other
form does (ignored, or the file refused); (b) an iterative reader, no recursion, nesting depth capped;
(c) caps with numbers: file size, rows, cells per row, value length, dictionary entries, id length
in hex digits, groups, total time; (d) a memory budget, with two passes or only the atoms the three
columns reference kept; (e) which flag bits mean deleted; (f) the file read only when its size and
modification time are equal before and after, once retried, else "not read this run"; (g) opened by
the contained reader (`util.OpenRegularFileIn`, R25), Windows sharing stated, a lab row with
Thunderbird open; (h) `.msf` files enumerated by the walk's own folder-naming and leave-out rules,
including folders that have no mbox; (i) nothing logged but counts and the folder name (GD-3 for
encrypted); (j) fuzzing in GD5's gate names the corpus (the throwaway account's real files, never the
owner's).

**GD-9 (MED; lenses 6, 5) — detection.**
Fix: (a) one detector shared with the completeness design, in `internal/thunderbird`, that reads
prefs.js through the contained reader with a size cap; (b) it parses `is_gmail` and resolves the
account by its directory first, then by host only when unique (today's map pick is random with two
`imap.gmail.com` accounts, `thunderbird.go:86-104`); (c) the All Mail flag's source is named
(folderCache.json, per the completeness design) and read the same way; (d) the design says that over-
detection is bounded (area 5) and words the status line without "Gmail" when only the flag matched,
or says "Gmail" was inferred; (e) the backup now reads prefs.js, which it did not before (§2.2);
state it beside R9 and R25 so no test that pins "the run reads no prefs.js" (presync's MA-562 is
about the presync run, not the backup) is misread.

**GD-10 (MED; lenses 8, 10) — older readers that do not refuse.**
Fix: (a) G7 and §7.4 say exactly what v1.6.1 does: its backup, verify, Desktop folder list and copy
export refuse; its `serve`, `search` and `status` read a version-4 index without a check
(`index.go:233-256`) and show each email in its home folder only, writing nothing; (b) the release
notes say every computer that uses the archive needs v1.7; (c) optional, lead's call: make the
change visible to an old read-only opener (for example by keeping each member as a row an old reader
ignores, which `doc_folders` already is) — no change needed if (a) is accepted.

**GD-11 (MED; lens 5) — everything else that assumes one folder per record.**
Fix, listed in §8 with tests: (a) restore: `SelectForRestore` and `RestorableCounts` count per
(folder, key); the "counts add up" promise (`restoreread.go:403-407`) is restated; `plan.go:372`'s
not-restored count is per folder written; `res.Damaged` counts a record once; the space check sums
per folder written; (b) extract: one walk item per (folder, record) before the sort; the member's
output directory from the same function that spells the home's; a stem collision in a member folder
gets a deterministic suffix, never an overwrite; `Emitted` counted per file written; (c) the
Desktop's archive copy export (`internal/desktop/ws_archive.go`, `PlanCopyExport`): say whether a
message chosen through two folders is copied once or into both, and which bytes `Fetch` returns for
a member; (d) `/goback` (`goback.go:144-152`) groups local records by `rec.Folder`: state whether
it lists members; (e) restore's `KeepsTimeline` (`restoreread.go:279-283`) must not flip because of
member times (stamp `Membership`, never `Record.LastSeen`).

**GD-12 (MED; lenses 8, 1) — the version rule and its rollout.**
Fix: (a) one function decides 6 or 7 from content, over every new field (`Folders`, `NormSHA256`,
`MergedFiles`, `DedupStores`, the merge state), with a reflection test that fails when a new
`Record` or `Manifest` field is added without a decision; the same for the index's 3 or 4, decided
in the same transaction as the first `doc_folders` row; (b) `DedupStores` and `MergePending` gain
slots in `state.View`, `FromSealed`, `save()` and the sealed header (`record.go:39-51`); today the
sealed save silently drops any manifest field outside `View` at every save (rescue N1), so a
round-trip test through an encrypted archive is part of GD2's gate; (c) the design states that the
owner's archive becomes version 7 at its first backup with v1.7 (evidence stamps, Q4) and asks
first (owner question Q-B); (d) the rollback story is stated: none in v1, or a command that writes a
version-6 archive back (Q-C).

**GD-13 (MED; lenses 3, 7) — membership states.**
Fix: a table in §7 for every transition of one membership: copy seen, copy not seen after a complete
walk, copy gone but still listed in the summary, listed, listed no longer, summary unreadable, folder
partly read. Two rules it must contain: a copy membership never loses what reproduces that folder's
copy (`Moz` and `CopySHA`, if the split is kept); the summary pass adds listed members only for folders whose mbox
walk completed this run or that have no mbox, never for a folder read partly
(`folder-partially-read`), where "no copy this run" means "not reached", not "absent".

**GD-14 (MED; lens 5) — carry the new fields forward.**
The exporter builds a fresh record on every write and carries `AlsoFiles` forward only on the
Microsoft 365 path (`exporter.go:763-765`). A Full run, a fill or a heal of a merged record must carry
`Folders`, `NormSHA256` and `MergedFiles` forward, recompute `NormSHA256` from the new bytes, and keep
every member's `Moz`. A test: Full run over MA-1214's fixture leaves four members.

**GD-15 (MED; lens 10) — claims to label.**
Fix in §3, §7.3, §10 and §15: (a) "byte for byte" and "exact originals" mean the reader's form (no
`From ` line, CRLF, one `>` taken from `>From `), proven; (b) §7.3's "the manifest gets smaller" omits
the home membership every Gmail record gains (233,322 of them); with the probe's numbers (§6) it is
false: the manifest grows from 220.7 MB to about 280 MB (§6); store no home membership (the record's
`Folder` and `FirstSeen` already say it) and, with raw-identical merging, no `norm_sha256` or `Moz`,
which brings it to about 210 MB or less; (c) §1's "No mail is lost: all of those messages are archived, from All Mail" is
now measured (§6): true for the four folders except one Sent Mail row whose Message-ID is in no
mbox; label it proven with that exception; (c2) §10.1's "every summary checked has `X-GM-MSGID`"
is false for INBOX (10,796 of 10,881 cells empty); (c3) §10.2's 79,200 extra records is 79,439
counting repeats inside one folder; (c4) the word list's "not part of the email anyone sent" is false
for 4,208 copies whose bookkeeping lines arrived with the message; (d) §5.4's bound (GD-6); (e) the 34.5 GB saving is proven only for the 73,915
byte-identical groups; groups merged in the manifest only free nothing; (f) after the merge one
original serves every folder (GD-17).

**GD-16 (MED; lenses 1, 3) — invariants.**
Fix in §12: (a) a new invariant (or a second clause of R45) for the merge step's deletion: "a file is
deleted only by the person's yes, only after a saved manifest no longer names it, and only when the
survivor's original, rebuilt with the copy's recorded lines, matches the copy's recorded SHA-256 at
the moment of deletion; otherwise it is kept, owned and reported"; (b) R3's sentence as the design
says; (c) R6 ("Output mirrors the source folder tree") gains "a Gmail member folder is mirrored by its
folder page, not by files"; (d) R20 and R40 per Q-A; (e) kept-after-mismatch files are owned (in
`AlsoFiles` or their own list) so verify reports them as kept, not `unexpected`; the existing
`AlsoFiles` ownership gap (`verify.go:314-321`) is fixed at the same time.

**GD-17 (LOW; lens 7) — one copy is now the only copy.**
Today a damaged All Mail `.eml` can still be found in INBOX's copy. After the merge, one original
serves every folder, and folder membership lives only in the manifest (and the regenerated folder
pages). Fix: the design says so plainly, verify's words name every folder a damaged original belongs
to, and `reindex -rebuild` either recovers memberships from the folder pages or says they come back
at the next backup from Thunderbird.

**GD-18 (LOW; lens 4) — words, at design time (Type II, fix now).**
(a) The merge offer: say what happens, not "Merge": for example "Keep one copy of each Gmail email
and free about 34.5 GB? Every folder still shows it." with Review and Not now; (b) a message opened
from Sent Mail has a "This folder" link to All Mail (frozen page, §8.4): the reader's own back link
should return to the folder it came from, and the static page's link wording says "All Mail" by name;
(c) the other computer's refusal and the failing schedule there say "this archive needs MailArchive
1.7 or later on this computer"; (d) restore's plan says a listed folder gets the stored copy with
All Mail's read and tag state; (e) the friction review (GD4, GD8 gates) walks these, Windows first.

**GD-19 (LOW; lens 9) — cost.**
(a) Skip a summary file whose size and modification time match the last read (kept per folder in the
manifest); (b) the merge plan reads every kept original: state the time on the owner's disk, show
progress, allow cancel before step 1; (c) verify's rebuild reads each original once per record, not
once per member; (d) the index: `doc_folders(key, store, folder)` indexed on `(store, folder)` and on
`key`; folder filters as `EXISTS`, facet counts `DISTINCT`; whether the home folder is also a
`doc_folders` row is decided once; (e) redaction's `DeleteByID` (`reindex.go:181`) and every other row
delete also delete the key's `doc_folders` rows.

**GD-20 (LOW; lens 2) — keep the slices separable.**
GD1 to GD4 plus GD8 fix Problem A; GD5 and GD6 fix Problem B. Make each a releasable unit with its own
version-7 trigger, so either can ship if the other stalls. Move per-folder `LastSeen` and `Gone`
(§7.2) to the later history design (it dissolves GD-7 and shrinks GD-13).

**GD-21 (LOW; lenses 5, 6) — cross-folder static links: three limits.**
(a) Built only from the record's validated `Path`, always inside the same store directory (R4), never
from a membership's folder string; (b) the encrypted reader's in-memory pages (`keyring/reader.go:560`)
resolve the same relative link; (c) an unencrypted copy or copy export of a subset of folders brings
the home files with it, or drops the link (GD-11c).

**GD-22 (LOW; lens 10) — redaction.**
§8.8's "Deleting a Gmail message's files removes it from every folder" lasts only until the next
backup while Thunderbird still holds the message, as today for any message (no tombstone,
`reindex.go:184`). Say so.

**GD-23 (LOW; lens 6) — the split rule's words (moot if Q-L1 drops the split).**
If kept, §4.4 step 1 states: a header line starts with a non-space byte and has a name ending at the first
colon; a continuation line starts with space or tab and belongs to the line above it; the empty line
is found as `headerBlock` finds it; names compared ASCII case-insensitively, exact; a continuation of
another header is never split out. MA-1210's cases drop LF/CRLF (cannot arise for mbox) and add a
sender-written `X-Mozilla-Keys` line.

**GD-25 (MED; lenses 7, 6) — no proof, no merge (rescue N2).**
Scenario: an archive made without `-raw` (or a group whose originals lack fixity) is "merged in the
manifest only, their files kept" (§9.4); the copies' records go, so nothing names their files:
verify reports them `unexpected`, and redaction and archive delete (which reach only `Path`,
`AlsoFiles` and `MergedFiles`) leave a redacted message's copy on disk and served. Fix
(recommended): no proof, no merge: such records stay separate and are counted ("N copies kept apart:
the archive has no original to prove them"). Alternative: put the kept files in `AlsoFiles` and make
verify own `AlsoFiles` (GD-16e).

**GD-26 (MED; lenses 5, 3) — listed folders on an unmerged archive (rescue N3).**
Scenario: Q4 fills Sent Mail at the first backup, before the owner merges; the owner's archive has
two to six records per email; §5.3 step 2 finds "more than one" and, with step 3 gone (GD-6), places
nothing: Sent Mail stays at 2. Fix: group the identity's records into byte-identical classes (same
`Fixity.EML` or `RawSHA256`); a single class is one candidate; attach the listed membership to the
class's would-be survivor by §9.4's rule (most complete, earliest, lowest key); step 5.3 then needs
no move. MA-1220 gains an unmerged fixture with three records per email.

**GD-24 (LOW; lens 10) — status and citations.**
The BUILD STATUS line and §13 cite this file (`docs/review-gmail-dedup-10lens.md`). Fix the §1.2
citations listed as wrong or loose.

## 5. Findings rejected (the skeptic)

| Claim | Refuting quote / evidence |
|---|---|
| A folded, duplicated or oddly placed bookkeeping line can make the rebuild wrong and lose a byte | §4.4 step 4: "Put `T` back into `N` at its positions and check the SHA-256 equals `SHA-256(B)`"; step 5 merges only on a match. A wrong split fails the check: not merged. |
| A Thunderbird line in the body merges two different emails | §4.4 step 1: "Nothing after the first empty line is looked at." |
| Mixed CRLF/LF copies in two folders falsely merge | The reader rewrites every line ending to CRLF before any split (`reader.go:59`, `:69`); such copies are already equal today. Not a new risk. |
| The mbox From-line differs per folder and breaks the byte match | The separator is consumed by the reader (`reader.go:94-116`) and never part of `B`. |
| A wrong `.msf` can alter or delete archived mail | §5.3: "A listed membership never creates a record, writes a file, or changes a record's bytes." |
| Restoring into every folder breaks R40's "only adds" | It writes new entries in one new Local Folders folder (R40, `scenario-catalog.md:380-383`); duplicates there mirror Thunderbird's own copies. Only R40's "preserved originals" sentence is broken (GD-4). |
| Desktop folder counts summing above the total mislead | §8.3: "the archive total counts records … never a sum of folder counts." |
| The cross-folder static links repeat the go-back mistake | Folder pages are regenerated (R28, `scenario-catalog.md:258-259`); the link target never moves (R13). See area 7. |
| prefs.js spoofing is an attack | Only the person's own Thunderbird writes it; a writer there already owns the profile (R25's stated scope: "a static hostile store that no other local user can write"). Over-detection is handled in GD-9. |

## 6. The owner's data (read-only, counts only, 7 November 2026)

One read-only agent, scripts in `mail-archive-tool/.claude/gmail-dedup-probe/review/` (not
committed): `mbox_struct.py`, `mork_probe.py`, `mork_diag*.py`, `storetoken_check.py`,
`prefs_flags.py`, `manifest_size.py`. Every mbox copy was read (not sampled). Only counts, sizes,
column names and flag values were printed. Thunderbird was running throughout.

**mbox copies** (All Mail 238,201; Important 56,585; INBOX 10,887; Starred 6,803):

- `X-Mozilla-Status` is the first header line in every copy of every folder.
- **A second bookkeeping pair later in the header: All Mail 3,947, Important 261** (Status and
  Status2 at fields 25 to 28 in 2,850 copies; a pair plus Keys at fields 9 to 12 in 1,048). These
  arrived with the message, not from this Thunderbird: the design's word list ("They are not part
  of the email anyone sent") is false for them. §4.4 would take them out too (the rebuild puts them back, so nothing is
  lost), which is one more reason to drop the split (§3).
- Folded bookkeeping lines: 0. Most bookkeeping lines in one copy: 5, 182 bytes (inside §4.4's bound).
- Line endings: Thunderbird's lines end in LF while the email's headers end in CRLF (inferred from
  byte counts). go-mbox makes them all CRLF (§1.1), so the stored `.eml` is not the file's bytes.
- Other `X-Mozilla-*` names: `X-Mozilla-Draft-Info` 21, `X-Mozilla-News-Host` 5. Header lines with
  no colon: 119 in All Mail. No header block without an empty line within 64 KiB.
- One `From ` separator shape, always `From - <day> <date>`; quoted `>From ` body lines in every
  large folder (All Mail 4,764, plus 309 `>>From `).

**Summary (`.msf`) files:**

- Sizes: All Mail 214.77 MB (it changed while being read), Important 41.98 MB, Sent Mail 18.98 MB.
- Live message rows equal Thunderbird's own counts in every folder (All Mail 238,169; Sent 33,822).
- Line endings are a bare CR; a dictionary entry's id is often followed by a line break and spaces
  before its `=` (453,875 times in All Mail). Groups: 162,978, none aborted, none open. Nesting
  depth at most 2. Dictionaries 572,356, the largest 519,998 entries, 1,809,764 values. Longest
  value 1,083 bytes in a dictionary, 29,016 in a cell. Every All Mail row appears more than once
  (row updates are normal); 584,041 rows opened with `[-`; 98,145 tables cleared with `{-`.
- `message-id` never starts with `<`; one `md5:` placeholder each in All Mail and Important.
- `X-GM-MSGID` is unique within each folder. **But INBOX's summary has an empty `X-GM-MSGID` on
  10,796 of 10,881 rows** (92 values). §10.1's "every summary checked has `X-GM-MSGID` … with about
  as many cells" counted empty cells. → GD-6, GD-15.
- The offset column is `storeToken`; it equals the byte offset of the copy's `From ` line (6,803 of
  6,803 on Starred). → GD-6.

**prefs.js and folderCache.json:** the Gmail account is `server4` (`hostname` imap.gmail.com,
`is_gmail` true); no other account has `is_gmail`. Only All Mail carries the 0x8000 (All Mail) and
0x4000 (Archive) bits, in any account.

**The archive's manifest** (metadata only; the archive lives in a Nextcloud-synced folder, which
makes area 3's second-computer case a real one): 220,708,903 bytes, 312,761 records, 705.7 bytes per
record. Records carry `fixity.{html,eml,zip}.{sha256,size}`. Counting repeats inside one folder too,
there are 79,439 extra records (§10.2's 79,200 counts only spread across folders).
**Size after the design as written: about 280 MB, not smaller** (a membership is about 286 bytes;
+92.4 MB for a membership on every copy including home, +18.9 MB for `norm_sha256`, +4.1 MB for
listed members, −55.9 MB of removed records). About 210 MB if the home membership is not stored.

**The four near-empty folders against the All Mail mbox** (Message-IDs hashed in memory):

| Folder | Rows | Message-ID in All Mail's mbox | In no mbox | Matches more than one All Mail copy |
|---|---:|---:|---:|---:|
| Sent Mail | 33,822 | 33,821 | 1 | 1,028 |
| Promotions | 772 | 772 | 0 | 0 |
| Daily Report | 191 | 191 | 0 | 0 |
| Business Papers - Corporation | 33 | 33 | 0 | 8 |

So §1's "No mail is lost" holds for these folders except one Sent Mail row; and 1,036 rows need a
way to choose between several All Mail copies. Elsewhere, All Mail's own summary lists 272 rows with
no copy in any mbox (a completeness matter), and Spam's 126 rows are in no mbox.

Not measured: the bookkeeping lines' line endings counted directly; Thunderbird's exact Message-ID
parsing (the probe stripped spaces and one pair of angle brackets).

## 7. Questions

**For the owner** (each with the lead's recommendation):

- **Q-A (only if the lead keeps the split; GD-4).** Extract and restore would write a rebuilt copy
  for a folder whose copy MailArchive no longer stores separately. Today's rules say they write only
  the stored original. May the rule say "or a folder's copy rebuilt from the stored original and
  checked against that folder's recorded checksum"? The review recommends not needing to ask: merge
  only copies that are already identical, so every folder gets the stored original, which is then
  exactly that folder's copy.
- **Q-B (GD-12).** With version 1.7, your archive changes to a format older versions cannot back up
  into, at its first backup, because Sent Mail and the other near-empty folders get filled in. Ask
  first, or just do it? Recommended: ask once, in plain words: "Show Sent Mail and your other Gmail
  folders in full? Every computer that uses this archive then needs MailArchive 1.7."
- **Q-C (GD-12).** Do you want a way back to the older format? Recommended: not in version 1; say in
  the release notes that there is none.
- The design's Q1 to Q8 stand; this review adds no change to their recommendations, except Q5 (show
  "no longer in this folder"), which GD-20 suggests moving to the later history design.

**For the lead** (decide and fold; no owner input needed):

- Q-L1: drop §4.4's split for version 1 and merge only byte-identical copies (§3; GD-4).
  Recommended: yes; before adding the split later, run a counts-only probe of copies that differ
  only in Thunderbird's three lines.
- Q-L2: per-member `LastSeen`/`Gone` in v1 or later (GD-7, GD-20). Recommended: later.
- Q-L3: copy export of a message chosen through two folders: once or twice (GD-11c). Recommended: into
  both, like restore, counted per folder.
- Q-L4: whether `reindex -rebuild` recovers memberships from folder pages (GD-17). Recommended: no;
  say they return at the next backup.
- Q-L5: keep §5.3 step 3 (offsets) in version 1 (GD-6). Recommended: yes; without it 1,036 listed
  rows on the owner's account stay unplaced, about 3% of Sent Mail.

---

Written by the review's synthesiser (a model, answering under no name) for Alex DiMarco, under the
Limen method; a fresh instance that has not met the record before.

- Date worked on: 7 November 2026, from the session's clock.
- Drew on: the design at 9203aae (read in full); the code cited in §1.1 (read directly); go-mbox
  v1.0.4's `reader.go` and `mbox.go` (read); the catalog's R3, R6, R20, R28, R40, R41 (read); the
  go-back design's T3 (read). Reached through agents and re-read where marked: the citation sweep
  (§1.2), the data probe (§6) and the rescue pass (§3). Not checked: the go-back gate digest that
  states F5 and GB-6 in full; Thunderbird's source for every "(recollection, unverified)" item.
- Register: review synthesis; opinion where marked (area 10, the recommendations).
- Your smallest checks: read R20 and R40 in `docs/scenario-catalog.md` (lines 148 and 380) to answer
  Q-A; and look at go-mbox's `reader.go` lines 59 to 69 to see why "byte for byte" needs its label.
- Counters: register synthesis · borrowed lines: quotations of code and catalog only, attributed ·
  drift: none noticed · touched: this file only · persona: none · source reached: worktree yes,
  module cache yes, owner's profile and archive metadata through the probe agent (counts only), gate
  digest no.
