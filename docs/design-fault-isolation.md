# Design — fault isolation for capture: one bad item costs exactly that item

**Revision:** 2 (2026-10-09). **BUILD STATUS:** approved with conditions, not built; owner
rulings folded (`docs/owner-rulings-2026-10-09.md`); RR-6 addendum folded and re-verified.
- **The gate.** The 10-lens pre-code review is filed as
  `docs/review-fault-isolation-predesign.md`. Verdict: **GO_WITH_CONDITIONS**. Findings: 114
  found, 111 confirmed, 3 refuted. No blocker survived the rescue.
- **RR-6.** Lenses 3, 6, 7 and 8 re-ran on SG's capture-side changes (ruling J4) before SG
  code. The addendum is filed in the same review file ("Addendum: RR-6"). Verdict:
  **GO_WITH_CONDITIONS**; no blocker survived the rescue. Its conditions **RR6-C1–RR6-C6** are
  folded, each marked `[RR-6 RR6-Cn]` where it lands, with new rows MA-540 and MA-541. RR6-C2
  and RR6-C3 amend ruling J4 (capture's overall cap before S3, and the size cap).
- **RR-6 re-verification.** A fresh-eyes check of that fold failed RR6-C6 and found stale
  text and two gate weakenings. Each fix is marked `[RR-6 RR6-Cn; re-verification]` with the
  condition it completes (a bare `[re-verification]` inside a clause already marked), or
  `[RR-6 re-verification]` where it brings stale text or an id into line. Both weakenings are
  reverted and raised as owner rulings in the addendum: **RR6-OR1** (the size cap on capture
  where no documented figure bounds it) and **RR6-OR2** (the breadth of capture's Graph folder
  walk). Each has a safe interim, and neither blocks SG.
- **This revision.** Revision 2 folds in the review's conditions **FC1–FC34**. Each change is
  marked in the text with its condition number, as `[FCn]`.
- **Ruled.** The ten **owner rulings, OR1–OR10** (§11), are ruled in
  `docs/owner-rulings-2026-10-09.md` §3, and each change they make here is marked
  `[ruling ORn]` (or the joint ruling it cites, J1–J8). None is escalated. The F33 identity
  fold, which this design's S2b would carry, is escalated (ESC-1, [ruling J8]); S2b ships
  without it. Capture's nextLink host pin waits behind the extraction review's XC25 gate
  (FI-SGc, [ruling J4]); lifting that gate is ESC-7.
- **Nothing in this document is built.** Where a claim below says **proven**, it means "proven
  by the named MA row once that row is green". It does not mean built.
- **Catalog ids [ruling OR8 = J1].** Revision 2's provisional R22 and S42–S44 are now **R25**
  and **S46–S48**. The rows keep MA-310..MA-329, with MA-320's and MA-324's Graph halves at
  **MA-501** and **MA-502** and the two lab rows at **MA-503** and **MA-504**.

Revision 1 (2026-10-09) is the text the review examined.

**Inputs.**
- Verified review findings (2026-10-08): **F9**, **F23**, **N1**, **F29**, **F27** and **F33**.
  Each was reproduced by a PoC and is re-cited below with file:line.
- Owner decision, relayed 2026-10-09: the copy/export tool is the **simple flow**. The user
  selects the source, chooses the export destination, and the tool copies. There is no
  drag-and-drop.
- **[FC20]** The simple flow is the extraction workspace (`docs/design-extraction-workspace.md`,
  OW4).
  - It has its own engine (`internal/app/copyexport*.go`) and its own outcome partition
    (`CopyOutcome`, its §4.13).
  - It never calls `app.Run` or `app.RunGraph`, so `ErrPartial` never reaches it.
  - This design touches that flow in two places only: the shared Graph primitive SG (§4.8.1,
    [FC21]) and the shared remedy table `app.GapRemedy` (§4.10).
  - Its result wording belongs to the extraction design.
- This design adds **no interaction surface** and no drag-and-drop anywhere. The outcome it
  defines (§4.9, §4.10) is for the `app.Run` / `app.RunGraph` callers:
  - CLI export and `graph`;
  - the `mailarchive-gui` wizard and `-job`;
  - MailArchive Desktop's Archive now and `-capture`.

**What this design does not decide.** It owns only isolation, recording and reporting. Root
causes that live in sibling finding sets are named as non-goals in §3: path-segment byte
bounds, Graph 5xx retry, positional paging, MIME part fidelity, re-export ordering and
manifest-save surfacing.

## 0. One-line shape

Every per-item and per-folder read, fetch, parse and write fault on every source is
**contained** to that item or folder and **recorded durably**.

- **What gets recorded where.**
  - An item the tool could **partly** read keeps what was read, as a record that names the gap.
  - An item with **nothing** readable, and every folder fault, has no record. These go into a
    new **capture-gap ledger** inside the manifest [FC7].
- **The ledger is inert.** Nothing ever reads it to skip work, so every gap is retried on every
  run that reaches it.
- **Partial runs.** A run with such faults ends **partial**: exit `1` with a `partial` flag
  on the last-run record (status `failed`, `partial: true`), and a typed `app.ErrPartial`
  [ruling OR1, OR2]. X1 is not amended, so no capture verb gains exit `3`.
  - S1a reports a partial run as today's failure (exit 1) [FC33]; S1b adds the flag, the
    summary and the sidecar wording.
- **Where it shows.** The same facts reach the summary, `status`, the report, `verify`, the
  README and the dashboard.
- **Run-level faults still stop the run:**
  - cancel;
  - lock loss;
  - an auth failure, including a failed token refresh [FC11];
  - a full, read-only or vanished archive drive [FC12];
  - a tripped breaker.

## 1. Problem

Today a single damaged or undeliverable item stops everything after it, on every run. In two
cases it hangs or crashes the run. In one case it silently merges distinct items.

| ID | Where | What happens today |
|---|---|---|
| **F9** | `internal/app/graph.go:619-622` | A `/$value` failure is returned from the per-message callback. `graph.Client.Messages` (`internal/graph/graph.go:482-484`) returns it, and `runGraphMailbox` returns before `walked.Mark` (`app/graph.go:647`) and before `SweepGone` (`:660`). The PoC was a fake server: Inbox `[A1,A2,POISON,A3,A4]`, Sent `[B1,B2]`, POISON returning `500 ErrorMimeContentConversionFailed`. Three incremental runs each archived only `[A1 A2]`. A3, A4, B1 and B2 were never fetched. The listing has no `$orderby` (newest first per the finding), so the wedge recurs at the same place and older mail is blocked permanently. The error names only the opaque Graph id. There is no skip. Contributing causes: `get` retries only 429/503 (`graph/graph.go:547`), `statusErr` is an untyped string (`:572-575`), and the MIME deadline is a fixed 10-minute wall clock (`:74`, `:494`) that `buildGraphClient` never sets (`app/graph.go:379-385`), so a large message on a slow link is a permanent poison. |
| **F23** | `internal/source/reader.go:199` | `walkMessages` loops on go-pst `MessageIterator.Next()`. On a failing row, `Next` (go-pst `message.go:94-134`) sets a **sticky** `it.err` and returns false **without advancing `currentIndex`**. The loop ends, and `walk` propagates the error through every ancestor, which stops the whole store. The PoC XORed one byte of one of the 17 message blocks in `testdata/support.pst`. Corrupting #0 yielded 0 messages, #3 yielded 3 and #11 yielded 11. A sweep of 1656 mutations gave 79 error-aborts, 14 panic-aborts and 1563 clean runs. The `(unreadable message)` stub was never produced, because `GetMessage` runs inside `Next()`, outside the `safeConvertMessage` guard (`reader.go:217-224`). Related aborts: a `GetSubFolders` error drops every sibling folder (`reader.go:178-181`), and a `collectAttachments` error fails the message, which then aborts the store (`reader.go:292-295`, `527-563`). The `.pst` remedy text is empty (`ostOpenRemedy`, `app/run.go:463-470`, covers only `.ost`). |
| **N1** | `internal/app/run.go:530`, `internal/export/exporter.go:301` | Any `Export` error is returned from the walk handler and ends the store. That includes `MkdirAll` failing for a folder, for example ENAMETOOLONG for a CJK folder name of 86 or more characters on ext4/btrfs, a deep tree, or an ACL. The Graph path ends the mailbox the same way (`app/graph.go:636-643`). PoC: mbox folders A, B and C, with a regular file planted at `out/<token>/B`. Every run archived `{A:1}` and C never. |
| **F29** | `internal/source/mbox.go:146` | `readMaildirDir` `ReadFile`s every non-directory entry and returns on the first error (`mbox.go:136-147`). Four outcomes. **(a)** A chmod-000 file, or a file renamed `:2,` → `:2,S` by a live client between `ReadDir` and `ReadFile`, loses every later message and folder for that run. A permanently unreadable file loses them on every run. **(b)** A FIFO blocks the run forever. **(c)** A symlinked message file or folder pointing outside the store gets **archived and indexed**. In the PoC, `TOP-SECRET-CONTENT` and `SSN 123-45-6789` were exported. **(d)** A `.sbd` symlinked to the store root re-yielded one message 33 times. `isMaildir` (`mbox.go:106-113`) and `walkSbd` (`:97-103`) use `os.Stat`, which follows links. |
| **F27** | `internal/source/reader.go:183` | `walk` recurses into every sub-folder with no visited set and no depth cap. A hierarchy row whose id points at an ancestor (PoC: child NID 32802 rewritten to the root NID 290) causes `fatal error: stack overflow` after about 72 s. Go stack exhaustion is not a panic, so neither `Walk`'s `recover` (`reader.go:153-157`) nor `runFile`'s can catch it. The process dies, later sources are not archived, the last-run record stays `running`, and every scheduled run repeats the crash. |
| **F33** | `internal/source/reader.go:220` | Every stub is `&model.Message{Subject:"(unreadable message)"}`, so every stub has the same `Identity()` (`sha:` over empty fields, `model/message.go:139-143,202-213`). The first stub in a folder is recorded. Later stubs hit the fillable-retry branch, where `probeImproves` finds nothing and writes nothing. Five stubs gave `exported=1 manifest=1 html=1`. Fields already read are discarded: subject, sender, dates and attachments are lost when only the RTF (`reader.go:286-290`) or attachment step failed. The mbox/maildir/Graph stub drops `Raw` although `data` is in scope (`mbox.go:201-212`). A panic inside `Export` is only logged (`app/run.go:522-527`, `app/graph.go:629-635` set `err=nil`): no `Stats` count, no report row, exit 0. |

All six are loud except F33, which is silent. None corrupts the source or the archive. The
user's harm is completeness: mail that exists in the source never reaches the archive, and
there is nothing the user can do inside the tool about it. These faults sit in the product's
core use (archive before a departure, a retention purge or a licence removal) and the trigger
inputs are normal (old PSTs, live maildirs, large M365 mailboxes), so R1, R10, R17 and R18
are all engaged.

Verification surfaced three **hazards in the obvious fix**. The design must handle each one
explicitly.

- **H1 — sticky iterator.** Wrapping `it.Next()` in a `continue` loops forever on the same row.
  `Next()` neither advances nor clears `it.err`. → §4.4 owns the row loop.
- **H2 — false gone.** Marking a folder `walked` when only some of its fetches failed lets
  `SweepGone` (`internal/state/manifest.go:952-977`) mark still-present messages gone. Two
  paths stamp `LastSeen` only through a successful export: Full mode and the immutable-id
  "comparable → download" path (`app/graph.go:564-575`). A failed re-fetch therefore leaves
  `LastSeen` stale. → §4.8.3 (P7).
- **H3 — loud turned silent.** Recording a Graph download failure as a TERMINAL gap, or as any
  manifest record under the message's identity, makes the Message-ID membership fast path
  (`app/graph.go:504-617`) skip the item forever. → the ledger is a separate, inert structure
  (§4.3, P5).

## 2. Properties

Each property names what it serves: an existing invariant, an X-rule, or a new or amended
invariant proposed in §2.1. Labels: **proven** (the named MA row, once green), **conditional**
(the condition is stated inline) or **aspirational**.

- **P1 — Item isolation.** On every source, a fault in reading, fetching, parsing, rendering or
  writing one message costs exactly that message. The walk continues with the next item and
  the next folder. This covers a Graph `/$value` failure (F9), an unreadable PST row or message
  node (F23), an unreadable or vanished maildir file (F29a), a panicking MIME/RTF/attachment
  parse (F33) and a per-message export error or panic (F33). *Serves R1, R10 (amended), R17.*
  **Proven:** MA-310, MA-315, MA-318, MA-321. **Conditional:**
  - Containment of a go-pst fault requires that the fault surfaces as an error or a recoverable
    panic. A go-pst internal infinite loop, or a runaway allocation, is not contained (§3 N7).
  - On Graph, isolation holds unconditionally for item-class faults. For transport-class faults
    it holds as bounded by the breaker (§4.8.5):
    - a known-poison item from an earlier run never counts toward it;
    - a trip needs a failing control request [FC10].
  - On PST, real media damage never trips the row breaker. Only structural rows count toward
    it [FC9].
- **P2 — Folder isolation.** A folder costs exactly that folder when any of these cannot be
  read: its listing (Graph page), its message table, its sub-folder table, its directory or
  mbox file. The same holds when the folder's output directory cannot be created (N1).
  Siblings, and every other folder still reachable, continue. One bad row in a sub-folder table
  costs only that sub-folder (§4.4). A folder or account that Outlook's own export skipped in
  `-outlook` mode is recorded as a folder gap, never only logged [FC28]. *Serves R6, R10, R1.*
  **Proven:** MA-316, MA-320, MA-501.
- **P3 — No hang, no unbounded walk.** Six guards:
  - FIFOs, sockets and devices are never opened for reading. This covers every file a local
    reader **or discovery** opens, not only maildir entries, so discovery cannot hang before the
    lock is taken [FC2].
  - A PST folder hierarchy is walked with a visited-NID set and a depth cap. **[RR-6 RR6-C6]**
    So is a Graph one: `Folders` keeps one visited folder-id set across the whole call and the
    same depth cap of 300 (§4.8.7). **[RR-6 RR6-C6; re-verification] Conditional:** a
    `childFolders` response that invents fresh folders at every level is bounded in depth, and
    per loop by the page cap, but not in total, so a misbehaving endpoint can keep one walk
    going for as long as it answers. That is an **open gap, not accepted**, encoded by
    MA-541's breadth case as a skip encoder citing R10 (owner ruling RR6-OR2).
  - A Graph download is bounded by an **idle timeout** (no bytes for 2 min), a `$value` header
    timeout, an overall cap and a size cap [FC21]. **[RR-6 RR6-C2; re-verification]** The
    overall cap is 2 h for the copy export from E1, and for capture from S3, where it lands
    with the `Retry-After` clamp and the breakers and replaces today's 10-minute wall clock.
    Until S3, capture's overall cap is that 10-minute wall clock. **[RR-6 RR6-C3]** The size
    cap's figure is confirmed in SG, before SG code, at or above the largest `$value` a stored
    Exchange Online item can produce (§4.8.6). Before S3, a body over the cap, like any other
    download stop, fails the mailbox through today's error path. **[RR-6 RR6-C3;
    re-verification]** The cap reaches capture only at a figure that confirmation bounds.
    Where none does, capture keeps today's uncapped read, so its download is bounded by time
    only: an open gap, not accepted (owner ruling RR6-OR1's safe interim, §4.8.6). A cap that
    reaches capture holds a legal item only while Microsoft's documented limit stays at or
    below it (§4.8.6).
  - `Retry-After` sleeps are clamped (≤60 s each, ≤5 min per item) until F17 lands [FC10]
    (F17 is this design's slice GF17, [ruling workspace Q15]). The clamp lands in S3; until
    then capture's 10-minute overall cap bounds these sleeps [RR-6 RR6-C2]. **[RR-6 RR6-C1;
    re-verification]** The `$value` header timer never runs during these sleeps: it times each
    HTTP attempt, from sending the request to its response headers (§4.8.6).
  - A Graph listing that repeats a `nextLink`, or leaves the client's base host, stops. This
    holds in **every** paging loop, `Folders` included [FC21]. **[ruling J4]** The repeat
    set and the page cap hold in every loop from SG on. The host/path pin holds in the
    extraction workspace's loops from its E1, and in capture's `Folders` and `Messages` from
    step SGc, which waits for MA-443's recorded nextLink shapes (the extraction review's
    XC25 gate; lifting it is ESC-7). Until SGc, a foreign `nextLink` in capture's listings
    is an open gap, not accepted, encoded by MA-314's foreign-host case as a skip encoder.
    **[RR-6 RR6-C5]** Every stop ends its loop with a typed error, never a quiet end of
    listing. On capture's shipped path MA-540 asserts it, the page cap and its per-loop scope
    included.
  - A PST folder table whose rows are **structurally** bad 100 times in a row is closed as one
    folder fault. This covers sub-folder tables too [FC9].

  *Serves R10, R18.*
  - **[FC31] When the run finishes.** It finishes, and so finalizes its last-run record, unless
    one of three things happens:
    - go-pst hangs internally (N7);
    - the OS hangs, for example on a hard-mounted NFS store (N8);
    - the process is killed (power loss, a scheduler time limit).

    In those cases the record stays `running` and `status` reports `run_never_finished`.
  - **[FC10] Wall-clock bound.**
    - Transport faults cost at most 30 minutes per mailbox plus one in-flight item.
      **[RR-6 RR6-C4] Conditional:** this holds only with a failing control request (§4.8.5),
      and only for counted faults.
      - **[RR-6 RR6-C4; re-verification]** Known poison never counts, whatever the control
        request returns. Every known-poison item therefore pays its timer on every run,
        outside the 30 minutes, even when the control request fails.
      - With a healthy control request nothing trips, and the bound is per item only (the
        header timer of at least 2 min, idle 2 min, the overall cap).
      - A mailbox whose `$value` stalls while its listings answer (a conversion incident, or
        items that always stall) therefore costs up to K × the item timer per run. K is the
        number of such items, known poison included, and it can grow over the archive's life.
      - This is an **open condition, not accepted.** A probe-immune time budget would reopen
        §8's hostile-sender wedge, and a ledger-driven limit on retries would narrow P5 and R1's
        added sentence ([ruling J7]).
      - What the operator sees: the run takes longer, and `status` shows `run_stuck` RED once
        a run outlasts its schedule period (`health.go:215`).
      - Before S3 there is no breaker and no known-poison rule, and the first such fault fails
        the mailbox, so a run pays at most one item's timer per mailbox.
    - A download that is still progressing is bounded by the overall cap: 2 h, and for capture
      today's 10 minutes until S3 [RR-6 RR6-C2].
    - Two consecutive mailboxes tripping stop the run.
    - The desktop Archive now has no mid-run cancel. That is stated here, not hidden.

  **Proven:** MA-314, MA-317, MA-322, MA-540, MA-541 [RR-6], except for the conditions below
  [RR-6 RR6-C6; re-verification]. **Conditional:**
  - **Depth cap.** The cap of 300 assumes no genuine store nests deeper. It matches the
    folder-hierarchy depth limit that Exchange Online is understood to publish (300).
    - That figure is an assumption. S2a confirms it against Microsoft's documentation and the
      fixture set.
    - A deeper real store loses only the levels below 300, and that loss is recorded.
    - **[RR-6 RR6-C6]** For a Graph mailbox, SG confirms the figure against Microsoft's
      documentation before SG code, because SG applies the cap to capture's `Folders` first.
      There a deeper level fails the mailbox loudly; no level is dropped silently.
    - **[RR-6 RR6-C6; re-verification]** The confirmation also fixes how Exchange counts
      levels, and MA-541's at-cap chain is built to that count. If it gives a higher figure,
      the cap is that figure, for capture and for S2a. If it gives none, capture's `Folders`
      carries no depth cap until a figure is bounded, as today's walk carries none (the rule
      of owner ruling RR6-OR1's safe interim). The visited set still ships, an endless chain
      of fresh ids is then an open gap, not accepted, and MA-541's over-depth case is a skip
      encoder citing MA-541.
  - **[RR-6 RR6-C6; re-verification] Graph folder breadth.** A `childFolders` response that
    invents fresh folders at every level is not bounded in total: an open gap, not accepted,
    encoded by MA-541's breadth case as a skip encoder citing R10 (owner ruling RR6-OR2).
  - **[RR-6 RR6-C3; re-verification] Size cap.** The cap reaches capture only at a figure SG's
    confirmation bounds, and it holds a legal item only while Microsoft's documented limit
    stays at or below it. Where no figure bounds it, capture's download is bounded by time
    only (owner ruling RR6-OR1, §4.8.6).
  - **Windows.** The special-file guard is proven on Unix only. On Windows it rests on the FC1
    classifier (§4.5).
- **P4 — Every fault is recorded durably, with a reason, a scope, a class and a locator.**
  Classes are fillable, terminal and info (§4.1). Where the source gave them, the human
  handles are kept:
  - subject and received date, from the Graph listing or from a post-parse local fault [FC13];
  - the store's source id (mailbox address or input path) [FC22];
  - file path;
  - PST NID.

  Faults are recorded in one of two places:
  - An item the reader **partly** read becomes a **record** whose content gap names what could
    not be read (`unreadable:body`, `unreadable:attachments`, or `unparsed`).
  - An item with **nothing readable** goes into the manifest's **capture-gap ledger**, and so
    does every folder fault. Nothing readable means a PST node that cannot be read, an envelope
    that panics, or a maildir file that cannot be opened. **No placeholder record, page, fixity
    entry or index row is minted for it** [FC7].

  Both survive the run and appear in the regenerated report and in `status`. *Serves R1
  (amended).* **Proven:** MA-318, MA-323, MA-327. **Conditional:** "durably" requires two
  things:
  - the final manifest save succeeds;
  - a failed save is surfaced as a run failure.

  Today a failed save only logs and exits 0. Finding g7/F53 owns that fix (§3 N6). Until F53
  lands, a failed final save loses this run's ledger entries. It never loses content: under P5
  the items are simply attempted again.
- **P5 — The ledger is inert.** No code path consults a capture gap when deciding whether to
  read, fetch or export an item. A Graph capture failure **never** creates a manifest record or
  an identity-index (`byIdent`) entry. Every gap is therefore re-attempted by every run that
  reaches its item (H3). *Serves R1, R17.* **Proven:** MA-311, MA-324, MA-502.
- **P6 — Unreadable items are distinct, and partial reads keep what was read.**
  - **[FC7] Nothing readable.** An item with nothing readable is one ledger gap of its own,
    keyed by a stable locator [FC8].
    - Five unreadable PST items in one folder are five gaps. They are never one collapsed
      record (F33).
    - No stub record is minted, so no identity changes. `contentHash` gets **no** locator fold.
  - **Raw sources.** A raw-bytes source (mbox, maildir, Graph) whose MIME parse panics keeps
    its bytes.
    - The page and the index carry the headers plus the first 64 KiB of the bytes, as escaped
      text [FC25].
    - The full bytes are kept as `.eml` under `-raw`.
    - The record gets an `unparsed` gap, terminal for this parser generation [FC25].
  - **Partial PST reads.** A PST item whose RTF or attachment step fails keeps every field
    already read, as a record labelled `unreadable:body` or `unreadable:attachments`.
    - It carries its `Locator` [FC6].
    - It is never allowed to replace an archived record (P15, [FC5]).
  - **Recovery.**
    - A gap closes as "recovered" when a later run exports the message at the same locator
      (§4.3 step 2), for example after an in-place PST repair.
    - A partly-read record heals in place when a later read no longer carries its label
      [FC6(2)].
    - If the healthy read lands on a different key, the partial record is marked superseded.
      This is metadata only: no file is written, rendered or deleted [FC6(4)].

  *Serves R1, R3, R7.* **Proven:** MA-318, MA-319. **Conditional:**
  - **In-place repair.** Recovery matching requires the repaired file to keep its store
    identity and the item's NID. A repair that renumbers the NID has a stated outcome:
    - the old gap closes as "no longer in the source" once a completed walk no longer finds it;
    - the real message is archived as new;
    - nothing is mismatched.
  - **Rebuilt PSTs [FC8].** An `-outlook` scratch PST, an `.ost` that Outlook rebuilds, or a
    store whose identity cannot be read gets one per-folder aggregate gap instead of per-NID
    gaps. No per-item recovery is attempted there.
  - **[FC5] No Message-ID.** A PST item without a Message-ID, archived complete earlier and
    partly read later, gets a second, partial record. The complete record is never touched.
- **P7 — Capture faults defer gone-detection; they never make it wrong** (except the residual
  stated below). On Graph [FC4]:
  - **Folder-scope fault.** Any folder-scope fault in a mailbox this run means **no**
    `SweepGone` for that whole mailbox this run. This is today's behaviour. It covers
    `folder-list-failed` and a listing-guard stop.
    - The sweep decides by each record's *last-known* folder. A message that moved into a
      folder whose listing failed would otherwise be swept from its old one.
  - **Known identity.** A fault on an item whose identity is known stamps `LastSeen` on
    **every** same-token record of that identity (`TouchSeen`).
    - The identity is known from the listing's Message-ID, or from the parse when the fault
      came after it.
    - When this run's listing confirmed immutable ids, every same-token record with
      `HasPhysID(ref.PhysID)` is stamped too. The immutable id survives a move.
  - **Unknown identity.** This is a fault with no listing Message-ID, before the parse, that
    matched no record by PhysID.
    - It excludes the token's PhysID-less `sha:`-identity records from this run's sweep.
    - If immutable ids were not confirmed, it excludes **every** `sha:`-identity record of the
      token.
    - It no longer excludes the folder.
  - **Unwritable set.** A ref not attempted because of the unwritable set counts as a fault
    under these rules.

  *Serves R21, S37 (H2).* **Proven:** MA-312. **Conditional, with a stated residual (L10-3):**
  - The known-identity half relies on the listing's `internetMessageId` equalling the parsed
    Message-ID.
  - Before this design, a mismatch cost a redundant download. Under P7 it can cost a **phantom
    `{k,gone}` history event**. Three things must all hold:
    - the record is a pre-v6, PhysID-less `mid:` record;
    - its listing and parsed Message-IDs differ;
    - its fetch fails while it sits in a folder that is marked walked.
  - The failure direction has changed, so this is a new assumption, not the old one. OR10 offers
    the owner a stricter alternative.
  - **[ruling OR10]** The stricter alternative is taken: on any fault whose identity matched
    no record, PhysID-less records are excluded from this run's sweep, as for an unknown
    identity (§4.8.3). The phantom `{k,gone}` cannot arise; the cost is that gone-detection
    for such records is deferred while the fault persists, and `status` names it. Deferred,
    never wrong.
- **P8 — Run-level faults still stop the run.** Each of these ends the mailbox or source walk
  as a failure (exit 1), with no flood of gaps:
  - parent context cancel (the handled interrupt still exits 0, X1);
  - archive lock loss;
  - Graph 401/403 (403 per OR7, ruled mailbox-fatal [ruling OR7]), and a failed token refresh (`*oauth2.RetrieveError`, or the
    delegated source's sign-in-again error) [FC11];
  - ENOSPC, EDQUOT or EROFS (and the Windows disk-full and write-protect codes);
  - an **unavailable archive location**. The archive root or the token directory is gone, or
    the lock file is unreadable. Volume-loss errnos are EIO, ENXIO, ENODEV, ENOTCONN, ESTALE and
    EHOSTDOWN, and on Windows 3 (at the root), 21, 53, 55, 64, 1167 and 1295 [FC12];
  - the `SafeToken` refusal;
  - a tripped breaker: the Graph rate and time breaker of §4.8.5 [FC10], or 50 consecutive
    local write failures, where each folder's first `FolderError` counts [FC12].

  *Serves R5, R12, R18 (H-F9.3).* **Proven:** MA-313, MA-320, MA-501.
- **P9 — Partial is a typed outcome.** A run whose walk completed but observed at least one
  fillable or terminal fault ends **partial**. §4.1 gives the exact definition, which includes
  partly-read records (`unreadable:*` labels). On a partial run:
  - capture verbs exit **`1` with the `partial` flag** [ruling OR2]: the default export verb,
    `graph`, `mailarchive-gui -job` and `mailarchive-desktop -capture`. The locked X1 is not
    amended, so no capture verb gains exit `3`;
  - the last-run status is `failed` with `partial: true` [ruling OR1], so an older `status`
    fails closed (RED) and `JSONVersion` stays 2;
  - in-process callers get `app.ErrPartial`, a typed error, so any caller that has not been
    updated treats partial as failure, never as success. Every in-repo `app.Run` caller is
    updated in S1b, including the `mailarchive-gui` wizard [FC19].

  Precedence is a run-level failure over partial over complete. *Serves X1 (unchanged), R18
  (its `partial` flag added).* **Proven:** MA-325. **Ruled** (`docs/owner-rulings-2026-10-09.md`
  §3):
  - exit `3` is not adopted; X1 stays as it is (OR2);
  - the status is `failed` plus the `partial` flag (OR1);
  - a "vanished" fault seen for the first time makes the run partial (OR4).

  S1a maps a partial run to today's `failed` (exit 1, `run_failed` RED). That is loud and
  fails closed, with no X1 change [FC33]. S1b adds the flag, `gaps_new`, and the partial
  wording of the summary, the sidecar and `status`.
- **P10 — One truth, legible everywhere.**
  - The run summary prints a "Not captured" block (source, folder, reason, remedy) and a
    "Recovered" line.
  - `status` gets reason-specific WARN rules with remedies and additive JSON keys. It also lists
    up to 20 gaps, each with source, folder, subject, date, reason and remedy [FC22].
  - The report gets one row per gap, carrying the store's source id [FC22]. It is written
    whenever the ledger is non-empty [FC24].
  - `verify` prints an informational line; its exit code is unchanged.
  - The README explains placeholders and gaps.
  - The archive root `index.html` shows a script-free banner. Every `pages.Generate` caller
    passes the count, including reindex and rebuild [FC24].
  - `BACKUP-NEEDS-ATTENTION.txt` is written on a partial run. It leads with "new this run: K"
    [FC23].
  - The desktop result and the Status & health view mirror `status` (X10).
    - Subjects appear on the dashboard's GET pages only once the dashboard read guard lands
      (F3/E0).
    - Until then the desktop lines omit the subject [FC22].

  *Serves X6, X10, X11, R7, R18.* **Proven:** MA-326, MA-327, MA-328.
- **P11 — Gaps heal and close.** A gap is removed in either of two cases:
  1. its item is captured (counted as "recovered");
  2. a **completed** walk of its folder no longer finds its locator (counted as "no longer in
     the source"), **and the run's scope covered the gap** [FC13].
     - A gap dated before the run's `-since` is never closed by absence.
     - In a `-since` run, export-stage gaps close only by recovery or by being observed again.

  A gap whose folder was not walked is retained.
  - **[FC14] Attempted or not.** Every gap is either **attempted** (observed or re-tried on its
    token's last run) or **not attempted** (its mailbox, source or folder was not walked).
    - Only attempted gaps drive the WARN and the "retried" wording, and only they can be
      **stuck**.
    - A not-attempted gap is reported as "never captured (last attempted <date>; not in recent
      runs)".
  - **Retired sources.** The gaps of a source that is no longer archived (a departed mailbox, a
    discarded PST) are **permanent** and stay listed. This is Type I: the tool can never observe
    that such an item has left the source.
  - **[FC18] Stuck.** A fillable gap attempted on several runs escalates to **WARN**
    `capture_gaps_stuck`, listed first, and **never to RED**. RED stays reserved for run-level
    failure and corruption. The threshold and its unit (runs or elapsed time) are owner ruling
    OR5. **[ruling OR5]** `Runs ≥ 3` AND `now − FirstRun ≥ max(72 h, two schedule periods)`,
    counting at most one observation per day.

  *Serves R1 ("revisit every fillable gap").* **Proven:** MA-311, MA-323. **Aspirational:**
  "heals" for a fillable Graph failure depends on Microsoft eventually serving the item. The
  tool can only retry and report.
- **P12 — Source-read containment (new invariant R25 [ruling OR8 = J1]).**
  - **The rule.** By default, reading a local store never follows a directory link: a
    symlink, or on Windows a junction or mount point. It never reads a special file, or a file
    whose link target lies outside the store's resolved root.
  - **The opt-in [ruling OR6].** An explicit per-input opt-in (a flag, a GUI option and a
    job-descriptor field, following X4/X5) follows that input's directory links, for that
    input only, guarded by a visited (dev, inode) set, and keeps the input's token. File links
    out of the store and special files stay refused even with the opt-in. R25's text names
    the opt-in, so the invariant stays true for an input that uses it (§2.1).
  - **Every local open.** The rule covers every file a reader or discovery opens [FC2]:
    maildir entries, mbox candidates and files, auto-detection and the Evolution walkers.
  - **Links are judged lexically**, by `Readlink`, before anything is resolved [FC3].
  - **Every refusal is recorded.** A refused entry that might hold mail is never classed info
    [FC1].
  - **Detection.** Detection may still see a linked folder; reading never descends into it.

  *Serves new R25, R9, R15 (F29c/d).* **Proven (Unix):** MA-322. **Conditional:**
  - **Static hostile store.** The threat is a static hostile store, such as one received from
    someone else.
    - A same-user local process racing a symlink swap between the check and the open is out of
      scope, as for DC4 in the desktop design (§8).
    - **[FC3]** R25 also assumes the store is **not writable by another local user**. A
      co-writer can plant a hard link, which is a regular file inside the root, or swap an
      intermediate component. That co-writer is named in §8 as out of scope.
  - **[FC1] Windows.** On Windows, P12 rests on the reparse-point classifier (§4.5), proven by an
    OS-independent test over synthetic file modes plus an unprivileged junction case.
    - The cloud-placeholder case is lab-pending.
    - The Windows open does not refuse links (`secureOpenFlags` is 0), so containment there
      rests on the judgment made before the open.
- **P13 — Bounded growth, over the archive's life** [FC16].
  - **Per token, at any time:**
    - at most 1000 live item-scope entries;
    - at most 1000 folder-scope entries;
    - at most 100 info entries, which never consume the item cap.
  - **Beyond the caps**, the folder's `overflow` entry counts the rest exactly. Its class is the
    most severe class it absorbed, so it still feeds `partial`.
  - **At `Load`**, the total is capped too. Any excess becomes one overflow note.
  - **Strings.** Detail is at most 300 bytes and subject at most 200 runes, both control-stripped.
  - **The bound.** The cap is a lifetime bound, not a per-run one. Gaps of retired sources stay
    within it (P11).
  - **No zero-content records.** No record is minted for an item with nothing readable [FC7].
    A hostile or corrupt table therefore costs ledger entries within the cap, plus aggregate
    per-folder entries (§4.4) [FC8, FC9].

  *Serves the durability lens.* **Proven:** MA-323, MA-315, MA-316.
- **P14 — Healthy inputs are unchanged** [FC31(3)].
  - **What stays the same.** On a store with no faults, the run produces the same records,
    keys, identities, paths and per-message file bytes as today, and an incremental re-run
    exports zero.
    - There is no manifest version bump.
    - No identity anywhere gains a locator fold [FC7].
    - `Record.Locator` exists only on records that carry an `unreadable:*` label [FC6].
  - **Named exceptions.** Three derived or policy-changed outputs differ on purpose:
    - `README.txt` is rewritten each run from a constant that this design changes;
    - the root `index.html` carries the gap-count stamp;
    - a store that captures through directory links changes under OR6 (ruled: it keeps its
      token through the per-input opt-in, P12 [ruling OR6]).

  *Serves R2, R3, R5.* **Proven:** MA-329, with those exceptions excluded from the golden
  comparison.
- **P15 — A degraded capture never replaces an archived record** [FC5].
  - **The guard.** A capture that carries any read-gap label, or a Graph parse stub, is never
    written over an existing same-token record of the same identity (on Graph, the same PhysID)
    that does not already carry those labels. Nothing is written or removed.
    - The record is stamped seen.
    - An info `refresh-failed` gap names the label.
  - **Scope.** It applies on every source and in both modes. It runs ahead of key placement and
    ahead of any write.
  - **g4.** g4's monotone re-capture subsumes this guard when it lands.

  *Serves R1, R3, R5.* **Proven:** MA-318 (Full-mode twins).

### 2.1 Invariant text proposed (catalog edits land catalog-first with their slice)

**[FC34] Ids [ruling OR8 = J1].** The ids below are the operator's allocation: R25 and
S46–S48 (revision 2 called them R22 and S42–S44, which collided with four sibling designs).
Catalog edits across the designs are serialised, one catalog-first PR at a time, in the order
of `docs/owner-rulings-2026-10-09.md` §9 (§10).

**[FC31(1)] Conditions.** Each amended text carries its conditions inline, because the catalog
is law.

**What lands under delegation [ruling J7].** New ids and added sentences land with their
slice. Text that narrows an existing invariant is not adopted; what it would have conceded
stays an open condition, or a skip encoder where a test can show it. Each item below is
marked.

- **R1, amended** — append: *"Every item or folder the source holds that a run could not
  capture (unreadable, undownloadable, unwritable or refused) is recorded in the manifest.
  - It is either a record naming what could not be read (an item read only in part), or an
    entry in the manifest's capture-gap ledger (reason, class fillable/terminal/info, locator,
    source).
  - It is listed in the regenerated report and in `status`.
  - It is never consulted to skip work. Every run whose scope covers it attempts it again,
    until it is captured or a run that covered it no longer finds it in the source.
  - The record is durable once g7/F53 lands. Until then, a failed final save loses that run's
    gap rows, never content.
  - A folder whose corrupted `MessageCount` reads 0 is a trust gap of the PST library (N9)."*

  **[ruling J7]** The ledger sentences (recorded, listed, retried every run, never consulted
  to skip work) land as added sentences. Their durability before g7/F53 lands stays an open
  condition stated at P4, not a sentence in R1. The N9 sentence is not adopted: the
  corrupted-`MessageCount` case becomes a skip encoder citing R1, on a byte-patched copy of
  `support.pst`, in S2a.
- **R10, amended** — replace: *"Malformed, truncated or damaged input costs the smallest unit
  the tool can isolate: the message, else the folder.
  - Damaged input means: bad mbox framing, a non-MIME message, an unparseable or unreadable
    node, a damaged folder table, a cyclic folder hierarchy, a special file.
  - Some faults cost a larger unit, and only that unit:
    - a failed listing of the store root costs that store;
    - a failed enumeration of Graph folders costs that mailbox;
    - an unreadable PST sub-folder table costs that sub-tree.
  - A fault that surfaces as an error or a recoverable panic never crashes the run and never
    hangs it. This excepts go-pst-internal loops and allocations and OS-level hangs (N7/N8)."*

  **[ruling J7]** Not adopted as a replacement: R10's existing "never a fatal crash" stays
  whole. "The smallest unit the tool can isolate" and "never hangs" land as added
  sentences. The exceptions (N7, N8) are not written into R10; they stay open conditions,
  stated at P3 and §3.
- **R17, amended** — "every message in the walked folders" becomes *"every message in the
  walked folders is archived, or recorded as a capture gap of the class given by its reason,
  never as a skip-eligible record, and is retried by every run whose scope covers it"*.

  **[ruling J7]** The replacement wording is not adopted. An added sentence lands instead:
  *"A message a run could not archive is recorded as a capture gap and retried by every run
  whose scope covers it."* A poison item shows as an open R17 gap in `status` until it is
  captured.
- **R18, amended** — the run statuses become `ok / partial / failed / cancelled`, subject to OR1.
  *"partial = the walk completed but at least one fillable or terminal capture gap, or a
  partly-read record, was observed this run."*

  **[ruling OR1, J7]** The statuses stay `ok / failed / cancelled`. A partial run is
  `failed` with an added `partial: true` flag, defined by the sentence above; that flag lands.
- **R25, new — Source-read containment** [ruling OR8 = J1]. The text of P12, with its
  conditions: Unix proven; Windows by the FC1 classifier; a store not writable by another
  local user. **[ruling OR6]** It names the per-input opt-in: *"…never follows a directory
  link, unless that input's directory-link opt-in is set; then its directory links are
  followed under a visited (dev, inode) set, and file links out of the store and special
  files are still refused."*
- **X1, amended** (a locked contract; owner ruling OR2). **[ruling OR2] Not adopted:** X1
  stays as it is, and a partial capture run exits 1 with the `partial` flag. The proposal
  read as follows. The capture verbs get [FC31(4)]:
  - `0` — complete.
  - `3` — partial. The walk completed, but this run observed at least one fillable or terminal
    capture gap, or a partly-read record. Items are named in the report up to the per-token
    cap, with per-folder counts beyond it.
  - `1` — error or refusal.

  A handled interrupt still exits `0` for the capture verbs. The extraction workspace's copy
  export has its own partition (its §4.13), which is not the same.
- **Scenarios, new** (allocated ids [ruling OR8 = J1]; they land with S1a and S2a):
  - **S46** One item or folder in a store or mailbox cannot be read, downloaded, parsed or
    written (R1, R10, R17, R21).
  - **S47** A local store has a hostile or odd layout: links out of the store, FIFOs, a cyclic
    hierarchy (R25, R10).
  - **S48** An operator or inheritor reads the result of a partial capture (R18, R7, X1, X6).

## 3. Non-goals (explicit scope cuts)

- **N1 — Root causes owned elsewhere.**
  - `SanitizeSegment` bounds runes, not bytes (the N1 trigger). That fix belongs to the
    path-portability set (F75/F91/F111). It renames directories and changes local keys, so it
    needs its own migration. This design isolates the failure whatever its cause.
  - ~~Retrying Graph 500/502/504 and budgeting `Retry-After` (F17).~~ **[ruling workspace
    Q15]** F17 is no longer owned elsewhere: it is this design's slice **GF17**, after S3
    (§10).
  - Positional-paging phantom gone (F11).
  - MIME part fidelity (g3: F24/F25/C1-x).
  - Re-export overwrite and delete ordering (g4: F46/N2/N3).
  - Index batch loss (F49).
  - Archive-side symlinks on write (g56/C2-2).
  - Token-fetch timeout (F15).
- **N2 — Repairing sources.** The tool does not repair PSTs. scanpst is external and
  Windows-only. It does not recover content go-pst cannot parse, and it does not resynchronise
  an mbox stream after a mid-file read error. That mbox folder is recorded as
  `folder-partially-read`, with the count read.
- **N3 — No acknowledge or forget verb.** A persistent terminal gap stays reported until the
  item is captured, or until a run that covered it no longer finds it in the source (OR3;
  [ruling OR3]: reported on every run that observes it).
  [FC14] The gaps of a source that is no longer archived persist and stay listed, as "never
  captured … not in recent runs". They do not drive the "retried" wording or stuck.
- **N4 — Directory links are not followed** (symlinks, and on Windows junctions and mount
  points), not even inside the store, by default. **[ruling OR6]** An explicit per-input
  opt-in follows them for that input (P12, §4.5). Out-of-store file links are never
  followed, opt-in or not.
- **N5 — The report keeps its name.** `attachments-report.tsv` is now a misnomer. Renaming it
  would break the README, inheritors' bookmarks and scripts.
- **N6 — Manifest-save failure surfacing** (g7/F53) is a dependency, not part of this design
  (P4 conditional).
- **N7 — go-pst internal hangs or blowups** that are not errors or panics: a cyclic B-tree page
  chain, an allocation of the size declared in a corrupt table. No such input is known. A
  per-store watchdog is a follow-up.
- **N8 — OS-level hangs**, such as a hard-mounted NFS store.
- **N9 — Pre-existing, unchanged:**
  - go-pst's `fmt.Printf` of unmapped message classes to stdout (an X8 deviation).
    [FC30] Walking on past damaged rows multiplies these lines, and cron mails stdout. So
    `Walk` captures go-pst's stdout into the run log while it runs (S2a). If that proves
    infeasible, the amplification is accepted and stated here;
  - TSV formula injection (subjects are already in the report);
  - a corrupted `MessageCount==0` hiding a folder's messages (go-pst trust);
  - the memory bound of whole-message reads.
- **N10 — IMAP live capture** (not built).

## 4. Mechanism

### 4.1 Vocabulary and the gap taxonomy

There are two families of gap.

1. **Content gaps** exist today, on an archived **record**: `Record.Missing` (fillable) and
   `Record.Terminal`. New labels are added (table below). Their meaning is unchanged: record
   Terminal means "never re-fetched" (R17). The one exception is a stale parser generation on
   an `unparsed` stub, re-fetched once per upgrade [FC25].
2. **Capture gaps** are new, in the **ledger**, for an item or folder that has **no record**.
   This includes every item with nothing readable [FC7].

The classes of a capture gap mean something precise:

- **fillable** — expected to heal by re-running: transient, source-side, or fixable by the
  operator outside the source file (permissions, a disk). Feeds `partial`. When stuck, it
  escalates to WARN `capture_gaps_stuck`, **never RED** [FC18].
- **terminal** — will not heal by re-running alone, because the source *as it stands* will not
  deliver the item. Operator action can fix it (repair, policy). Feeds `partial`; status WARN.
- **info** — an anomaly where nothing is missing. Reported only; it does not feed `partial` or
  posture.
  - **[FC1] What counts as nothing missing.** Only **proven non-mail** qualifies: a Unix FIFO,
    socket or device. Any other refused entry that might hold mail is terminal.
  - A Windows reparse file that is not a link, such as a cloud placeholder, is **read**, not
    refused (§4.5).

**Every class is re-examined on every run.** On local sources this is a side effect of the walk,
which re-reads every item. On Graph it happens because the item has no record and is therefore
never skipped (P5). A capture gap's class is a **posture and remedy label**, never a skip
decision. This is how "terminal" coexists with H3.

| Reason | Source | Scope | Class | In-run behaviour | Feeds breaker |
|---|---|---|---|---|---|
| `fetch-failed` | Graph: an HTTP response with a Graph error code, e.g. 500 `ErrorMimeContentConversionFailed`, or another 4xx | item | fillable | next item | no (item-class) |
| `fetch-transport` | Graph: no response, connection error, 429/503 after `get`'s retries, 502/504 | item | fillable | next item | **yes**, unless known poison [FC10] |
| `fetch-timeout` | Graph: SG's typed idle, `$value` header or overall timeout (walk context still live) [RR-6 RR6-C1; re-verification] | item | fillable | next item | **yes**, unless known poison [FC10] |
| `fetch-too-large` [FC21] | Graph: the MIME body exceeds the shared download's size cap | item | terminal | next item; never truncated | no |
| `vanished-at-fetch` | Graph 404 / `ErrorItemNotFound` | item | fillable, self-closing (first sighting makes the run partial, [ruling OR4]) | not retried this run | no |
| `refresh-failed` | any source [FC5]: a degraded capture held back by the KEEP guard (P15), or a Graph Full-mode fault on an item whose Message-ID already has a same-token record | item | info | next item | as its transport class |
| `folder-list-failed` | Graph: a listing page error other than auth, or a listing-guard stop | folder | fillable | next folder; not walked; **no sweep for the mailbox** [FC4] | **yes** if transport-class |
| `export-failed` | any: per-message render or write error | item | fillable | next item | local write breaker |
| `export-panic` | any: a `recover` inside `Export` | item | terminal (tool bug) | next item | no |
| `folder-unwritable` | any: `export.FolderError` (output `MkdirAll`), after the archive-root probe found the root present [FC12] | folder | fillable | remaining items of that folder not attempted (`Skipped` counted per run [FC15]); fast-path skips still stamp; unwritable-set refs are faults for the sweep [FC4] | local write breaker; each folder's first attempt counts [FC12] |
| `folder-unreadable` | local: `ReadDir`, mbox open, `GetMessageTableContext`, sub-folder table | folder | EACCES/EPERM → fillable; structure error → terminal | next folder | no |
| `folder-partially-read` | local: mbox stream error mid-file; a PST row breaker on a message **or** sub-folder table [FC9] | folder | per errno, as above | next folder | no |
| `vanished-during-read` | maildir ENOENT after one re-list | item | fillable, self-closing (first sighting makes the run partial, [ruling OR4]) | next item | no |
| `pst-message-unreadable` [FC7] | PST: a real node whose `GetMessage` fails, or whose envelope read panics (also the `safeConvertMessage` outer guard) | item, locator `pst-nid:<store-uid>/<nid>` [FC8] | terminal | next row; **no record** | no (real damage never counts [FC9]) |
| `pst-messages-unreadable` [FC8] | PST: the same faults in a store whose store-uid cannot be read, an `-outlook` scratch PST, or an `.ost` rebuilt at the same path | folder (aggregate: count plus bounded NID list) | terminal | next row; **no record** | no |
| `pst-row-unreadable` | PST: a row whose NID cannot be read | folder (aggregate per folder [FC8]) | terminal | next row | PST row breaker |
| `pst-row-invalid` | PST: a row NID that is not a normal-message type, a NID already yielded in this store (duplicate), or a NID absent from the node B-tree [FC9] | folder (aggregate per folder [FC8]) | terminal | next row | PST row breaker |
| `file-unreadable` [FC7] | maildir/Evolution: a message file that cannot be read (EACCES/EPERM, EIO) | item, locator `maildir:<folder>/<unique>` | fillable | next item; **no record** | no |
| `folder-cycle` | PST: a sub-folder NID already visited | folder | terminal | edge skipped | no |
| `folder-too-deep` | PST: depth > 300 | folder | terminal | subtree skipped | no |
| `symlink-outside-store` | mbox/maildir/Evolution: a file link resolving outside the root or to a non-regular file, judged lexically first [FC3] | item | terminal | not read | no |
| `symlinked-folder-skipped` | mbox/maildir/Evolution: a directory link (`cur`/`new`/`.sbd`/folder/shard/`subfolders`), including a Windows junction or mount point [FC1] | folder | terminal | not descended | no |
| `not-a-message-file` | any local reader or discovery [FC2]: a FIFO, socket or device (`ModeNamedPipe`/`ModeSocket`/`ModeDevice`/`ModeCharDevice`) [FC1] | item | info | not opened | no |
| `outlook-export-skipped` [FC28] | `-outlook`: a folder whose Outlook `CopyTo` failed, or a failed account | folder, locator `outlook:<account>/<folder>` | fillable | folder absent from the scratch PST | no |
| `outlook-account-skipped` [FC28] | `-outlook`: an account skipped because it is already a `.pst` | folder | info (the detail names the path to archive directly) | — | no |

New **content-gap labels** on records, set by readers through `model.Message.ReadGaps`.
**[FC7]** No label is set for an item with nothing readable. Such an item is a ledger gap
(above). `unreadable:message` and `model.Unreadable` are therefore **not** introduced.

| Label | Set by | Class | Probe (`probeImproves`) |
|---|---|---|---|
| `unreadable:body` | PST: RTF/HTML/plain body read panicked | fillable | improves when the new read no longer carries `unreadable:body` [FC6(2)] |
| `unreadable:attachments` | PST: a failure of the **whole** attachment table (error or panic). Per-part gaps are g3's `Attachment.GapKind` [FC26] | fillable | improves when the new read no longer carries the label, which also covers a real empty attachment set. The `#fp` comparison runs over zero attachments [FC6(1)] |
| `unparsed` | any raw source: MIME parse panic, or the existing `fallbackParse` "(unparseable message)" stub (`mbox.go:329-333`) | **terminal for this parser generation** [FC25]: same bytes, same parser, same result | re-examined once when the running parser generation is newer (§4.8.2, §4.7.4); improves when the new read no longer carries it |

`probeImproves` and `state.GapClass` are the **single owner** of record gap labels. They get
explicit cases for these labels and for g3's labels [FC26]. Today `probeImproves`' `default`
branch would treat an unknown label as an attachment name and never match
(`exporter.go:565-583`).

**`partial`** — a run is partial when it observed either of these:
- at least one ledger gap of class fillable or terminal (its `LastRun == runAt`);
- at least one record written or re-examined that carries an `unreadable:*` label
  (`Stats.Unreadable > 0`).

These do **not** make a run partial:
- `unparsed` and info;
- the pre-existing not-downloaded and source-empty gaps, which keep today's exit-0 behaviour.

Whether a first-sighting self-closing gap counts is owner ruling OR4. **[ruling OR4]** It
counts: a one-shot run before a departure must not claim completeness.

### 4.2 Data and schema

**Manifest (`internal/state/manifest.go`).** Additive. **No `manifestVersion` bump** (stays 6;
rationale in §6):

```go
// CaptureGaps is the capture-gap ledger: items/folders the source holds that no record
// represents. INERT: never consulted by any skip/fetch/export decision (P5); not part of
// Entries, byIdent or byPath.
CaptureGaps map[string]CaptureGap `json:"capture_gaps,omitempty"`

type CaptureGap struct {
    Token    string    `json:"token"`
    Folder   string    `json:"folder"`             // same spelling as Record.Folder; "" = store root. Updated on every observation [FC14]
    Scope    string    `json:"scope"`              // "item" | "folder" | "overflow"
    Locator  string    `json:"loc"`                // graph:<immutable id, else id> | pst-nid:<store-uid>/<nid> | ident:<identity> | maildir:<folder>/<unique> | file:<store-rel path> | folder:<store-rel path> | outlook:<account>/<folder> [FC8, FC28]
    Reason   string    `json:"reason"`             // §4.1 table; an unknown reason is kept, bounded and stripped [FC17]
    Class    string    `json:"class"`              // fillable | terminal | info; overflow = the most severe class absorbed [FC16]
    Subject  string    `json:"subject,omitempty"`  // listing or post-parse subject; ≤200 runes, control-stripped
    Received string    `json:"received,omitempty"` // RFC 3339; Graph listing or post-parse m.Date() [FC13]
    Detail   string    `json:"detail,omitempty"`   // error text; ≤300 bytes, control-stripped
    Skipped  int       `json:"skipped,omitempty"`  // folder/overflow: items not attempted THIS run (reset at the run's first observation [FC15])
    FirstRun time.Time `json:"first"`
    LastRun  time.Time `json:"last"`
    Runs     int       `json:"runs"`               // number of distinct runs that observed it (not calls; not necessarily consecutive) [FC15]
}
```

The key is `token \x00 scope \x00 locator`. This map is separate from `Entries`, so its keys can
never collide with a record key or reach `byIdent`. Accessors:
- `ObserveGap(g, runAt)` inserts the entry, or updates an existing one.
  - On an existing entry it sets `LastRun`, `Reason`, `Class`, `Detail` and `Folder`, plus
    `Subject` and `Received` when given [FC14].
  - It increments `Runs` **only when `LastRun != runAt`**, and resets `Skipped` at a run's first
    observation [FC15].
  - It enforces the P13 lifetime caps [FC16].
- `CloseGap(key) bool`.
- `CloseUnobserved(token, scopeFn, runAt, since) (closed int)`. It closes only entries with
  `LastRun != runAt` that the run's scope covered (§4.3 step 3) [FC13, FC14].
- `CaptureGapCounts(lastRunAt map[token]time.Time) (fillable, terminal, info, stuck,
  notAttempted int, oldest time.Time)`. An entry is *attempted* when its `LastRun` equals its
  token's last run [FC14].
- `CaptureGapsSorted() []CaptureGap`.

`Load` treats the ledger as **untrusted**, like `ReadLastRun` (MA-157). It:
- control-strips and bounds every string;
- keeps a known `Class` even when the `Reason` is unknown, and keeps that `Reason` bounded and
  stripped [FC17];
- maps an unknown `Class` to **terminal**, failing loud, never to info [FC17];
- drops entries whose token is not `SafeToken`, or is not a value in `Stores` [FC14];
- clamps `FirstRun` and `LastRun` to ≤ now [FC14];
- caps the total number of entries, keeping one overflow note for the rest [FC16].

**Record.**
- Gains `Locator string json:"loc,omitempty"`. It is set **only** on records that carry an
  `unreadable:*` label (partly-read PST records) [FC6(3)].
- Gains `RecoveredBy string json:"recovered_by,omitempty"` (§4.7.4).
- Any re-examine of such a record restores a missing `Locator` from `m.Locator`. That heals the
  loss when an older binary saves the manifest (§6) [FC6(3)].
- A lazily built `byLocator map[token+loc]key` covers exactly those records.
- `Load` drops `Locator` from a record without an `unreadable:*` label, and drops a
  `RecoveredBy` that names no existing key [FC6(4)].
- An `unparsed` stub record also carries `ParserGen int json:"parser_gen,omitempty"` and
  `RawDigest string json:"raw_sha256,omitempty"` [FC25].

**Model (`internal/model/message.go`).**
- `Locator string`: set by local readers on every message.
  - PST: `pst-nid:<store-uid>/<nid>` [FC8].
  - maildir: `maildir:<folder>/<unique>`, where unique is the basename before the first `:` or
    `!`.
  - Graph does not set it.
- `ReadGaps []string`: labels.
- `ReadError string`: bounded detail for the record notice.
- **[FC7] Removed from the design.** `Unreadable bool` and the locator fold. No stub record is
  minted for an item with nothing readable, so `contentHash()` is **unchanged** and every
  identity is byte-identical to today (P14). The raw-preserving stub's subject stays exactly
  `(unreadable message)`, the string `internal/source/categories_test.go:124` keys on.

**Stats / Result (`export.Stats`, `app.Result`).**
- `Stats` gains `Unreadable` (partly-read records written or re-examined), `Unparsed`,
  `ExportPanics`, `ExportFaults` and `Kept` (captures held back by the KEEP guard, P15) [FC5].
- `Result` gains `Gaps GapSummary{Observed, New, Recovered, ClosedGone, Fillable, Terminal,
  Info, Stuck, NotAttempted, ItemGaps, FolderGaps int; Lines []string}` and `Partial bool`.
  Items and folders are counted apart [FC23(3)].

**Last-run record (`internal/state/lastrun.go`).**
- **[FC31(5)]** No `Version` bump. No reader gates on it: `WriteLastRun` hard-sets 1
  (`lastrun.go:81`) and `ReadLastRun` never reads it. The fields are additive.
- The status for a partial run is owner ruling **OR1**. It is either a new constant
  `RunPartial = "partial"`, or `"failed"` with `Partial bool`. **[ruling OR1]** It is
  `"failed"` with `Partial bool` (`json:"partial,omitempty"`): an older `status` then fails
  closed (RED) instead of showing GREEN, and no documented value set widens. S1a writes
  `failed` [FC33]; S1b adds `Partial`.
- New fields `GapsObserved`, `GapsNew`, `GapsRecovered`, `GapsFillable`, `GapsTerminal`,
  `GapsStuck`, `Unreadable`.
- On a partial run, `Error` carries a one-line summary ("partial: N item(s) not captured —
  see attachments-report.tsv"), so a JSON consumer that prints `error` sees it.

### 4.3 Ledger lifecycle

1. **Observe.** At the fault, the orchestrator calls `ObserveGap`.
   - The entry is in memory. It reaches disk with the next checkpoint or the end-of-run
     `commit` (history → index → manifest, `app/run.go:337-373`).
   - Faults count as "processed" for the checkpoint gate, on **both** paths: Graph
     (`app/graph.go:266`) and local [FC12]. A run made mostly of faults therefore still
     checkpoints, and still re-checks its lock.
   - Reader faults arrive through a callback during the walk, not after it [FC29].
2. **Recover.** When an item is exported successfully, the orchestrator closes any gap with the
   same `(token, scope=item, locator)`. Each close increments `GapSummary.Recovered`. The
   locator depends on the source [FC8]:
   - Graph: `graph:<immutable id>` when this run's listing confirmed immutable ids, else
     `graph:<ref.ID>`;
   - PST: `pst-nid:<store-uid>/<nid>`. It never matches across a differing store-uid;
   - other local items: the item's `Locator`, or `file:<path>`, or `ident:<identity>` for a
     post-parse fault.
3. **Close as gone.** At the end of a **completed** store walk (local) or mailbox walk (Graph),
   `CloseUnobserved` removes this token's gaps with `LastRun != runAt` [FC14]. It does so only
   when **the run's scope covered the gap**:
   - **Folder fully read.**
     - Graph: the folder's listing was consumed.
     - Local: the walk completed, and no folder-scope gap was observed this run on the gap's
       folder or any ancestor.
   - **Date window [FC13].** An item gap whose `Received` is before this run's `Since` is never
     closed.
   - **Export-stage gaps in a `Since` run [FC13].** In a run with `Since` set, `export-failed`,
     `export-panic` and `folder-unwritable` close only by recovery or by being observed again.
     The report says "outside the -since window — not re-attempted". Reader-stage gaps need no
     such rule, because readers fault before the date filter runs.

   Gaps in folders that were not read are **retained**. A walk aborted by a run-level fault
   closes nothing.
4. **Attempted and stuck.** `status` sees two kinds of gap [FC14]:
   - **Attempted:** `LastRun` equals its token's last run.
   - **Not attempted:** anything else. Its source or folder was not walked.

   Only an attempted fillable gap can be **stuck**. The threshold and its unit are owner ruling
   **OR5**. **[ruling OR5]** A gap is stuck when `Runs ≥ 3` (`Runs` counts distinct runs,
   at most one observation per day [FC15]) AND `now − FirstRun ≥ max(72 h, two schedule
   periods)`. Stuck is WARN, never RED [FC18].
5. **Cap [FC16].** The caps are lifetime caps, not per run:
   - per token, ≤1000 live item-scope entries and ≤1000 folder-scope entries;
   - info entries have their own cap of 100.

   Beyond a cap, the folder's `overflow` entry counts the item instead of adding a row. Its
   class is the most severe class it absorbed.

The ledger is **write-mostly diagnostics**. Its only readers are the report, `status`, `verify`,
the summary, the banner and closure itself. Losing it at any point loses attempt counts and
history, never content (P5).

### 4.4 PST reader (`internal/source/reader.go`): own the loops (F23, F27, F33)

- **Ownership of the copied loops [FC30].** The loops below replace go-pst's
  `MessageIterator.Next` and `Folder.GetSubFolders`, pinned at go-pst **v6.0.2**. The
  `internal/source` reader owns their semantics from now on.
  - A **differential test** shows that, on the healthy fixture, the loops yield the same folder
    set and message set as go-pst's own iterators.
  - Every upstream gate is kept, and each one is listed in the bullets below:
    - `HasSubFolders`;
    - `MessageCount > 0`;
    - the search-folder type;
    - `IdentifierTypeNormalMessage`.
- **Store identity [FC8].** At open, the reader reads the message store's `PidTagRecordKey`
  (0x0FF9) through `file.GetMessageStore()` as `store-uid`. It is used for every `pst-nid:`
  locator.
  - It is not used when the uid cannot be read, or when the store is an `-outlook` scratch PST
    (rebuilt every run) or an `.ost` that Outlook rebuilds at the same path.
  - For those stores, every zero-content failure is recorded in one per-folder aggregate gap,
    `pst-messages-unreadable`, with a count and a bounded NID list. That entry is stable across
    NID churn, so `Runs` counts correctly and nothing false-recovers.
  - S2a verifies that go-pst reads 0x0FF9 on the fixtures. If it cannot, the aggregate applies
    to every PST.
- **Message rows (H1).** `walkMessages` stops using `GetMessageIterator`.
  - It keeps go-pst's two gates:
    - `MessageCount > 0`;
    - not a search folder (`Identifier.GetType() == IdentifierTypeSearchFolder` → nothing).
  - It calls `folder.GetMessageTableContext()`. On failure it records `folder-unreadable`
    (terminal) and returns nil, so sub-folders still walk.
  - It keeps a per-store set of NIDs already yielded [FC9].
  - It iterates `tc.Properties` **by index `i`**. Each row runs under its own `recover`:
    1. Read the NID: `tc.GetPropertyReader(p)` then `GetInteger32()`. On failure →
       `pst-row-unreadable`, counted into the folder's aggregate entry [FC8].
    2. The NID is checked three ways [FC9]. Each failure is a `pst-row-invalid` count in the
       folder's aggregate:
       - its type is not `IdentifierTypeNormalMessage`;
       - it is already in the yielded set (a duplicate);
       - the node B-tree has no entry for it (`GetNodeBTreeNode` fails).
    3. Call `r.file.GetMessage(nid)`. An error or panic → a **`pst-message-unreadable` fault**
       (item, locator `pst-nid:<store-uid>/<nid>`, the bounded error as detail). For a store
       without a uid, the fault is counted into the folder's `pst-messages-unreadable` aggregate
       instead [FC7, FC8].
       - **No message is yielded and no record is minted.**
    4. Otherwise call `convertMessage`, which has its own inward recovers (below).

    The loop **never** calls `Next()`, so the sticky-error hazard cannot arise.
  - **Row breaker [FC9].** Only **structural** failures count toward it: cases 1 and 2.
    - A real node whose data cannot be read (case 3) is real damage. It does not count. It costs
      only its own ledger entry, capped by P13.
    - 100 consecutive structural failures with no success between → stop this folder. Record
      `folder-partially-read` (terminal) with `Skipped = len(rows) - i`.
    - A run of damaged messages in the middle of a folder therefore costs only those messages.
      A bogus or hostile table costs one folder entry.
- **Inward recovers in `convertMessage`.** Each step runs under its own `recover`:
  1. **Envelope** (subject, sender, recipients, ids, dates, state, categories): a panic → a
     `pst-message-unreadable` fault, as in case 3. Nothing reliable was read, so no message is
     yielded [FC7].
  2. **Body** (HTML, plain, RTF): a panic → keep the envelope and add
     `ReadGaps unreadable:body`.
  3. **Attachments** (`collectAttachments`): an error or panic → keep the envelope and body, set
     no attachments, add `ReadGaps unreadable:attachments`. It never fails the message.

  Cases 2 and 3 set `m.Locator`, so the record carries it [FC6(3)]. `safeConvertMessage` stays
  as the outer guard. Its recover now records a `pst-message-unreadable` fault instead of
  yielding a stub [FC7].
- **Sub-folders (F23.3, F27).** `walk` stops using `GetSubFolders()`.
  - It keeps go-pst's gate: `if !folder.HasSubFolders` → no sub-folders (go-pst
    `folder.go:100-105`). Otherwise every leaf folder would read a hierarchy node that does not
    exist [FC30].
  - It calls `GetSubFoldersTableContext()`. On failure it records `folder-unreadable` for the
    parent's sub-tree and continues with the parent's siblings.
  - It iterates the rows by index under a per-row `recover`, reading properties 12289 (name),
    26610 (id), 13826 (count) and 13834 (has-subfolders) exactly as go-pst `folder.go:100-160`
    does.
    - One bad row → `folder-unreadable` for that row only.
    - **[FC9] Breaker.** 100 consecutive bad rows → one `folder-partially-read` with `Skipped`.
      This uses the same breaker as the message rows, so a corrupt hierarchy table costs one
      entry.
  - It threads a `visited map[pst.Identifier]bool` and a `depth`.
    - A child NID already visited → `folder-cycle` (terminal), and the edge is not recursed.
    - `depth > 300` → `folder-too-deep` (terminal), and the subtree is not recursed.

  Recursion depth is now bounded, so stack exhaustion cannot occur.
- **Fault delivery [FC29].** The reader delivers each fault **as it happens**, through a
  `Faults func(Fault)` callback in its walk options. Faults therefore reach `ObserveGap` and the
  checkpoints during an hours-long walk.
  - A caller that registers no callback gets `*source.FaultsError{Faults []Fault, Dropped int}`
    when `Walk` returns. That list holds at most 10 000 faults and counts the rest in `Dropped`.
    The caller sees a non-nil error (loud, fail-closed).
  - `Walk`'s outer `recover` remains the last resort. An error there is a store-level failure,
    as today. It returns the faults already accumulated **together with** the store error, so
    the gaps already observed are kept.
- **`.pst` remedy (F23.5).** `ostOpenRemedy` becomes `dataFileRemedy`. For `.pst` it appends:
  "a damaged .pst can usually be repaired with Outlook's Inbox Repair Tool (scanpst.exe,
  Windows). Repair the file **in place** (it keeps a .bak backup), or put the repaired copy at
  the **same path**, then re-run. Archiving a copy from a different path starts a separate
  archive tree." This extends MA-282.
  - **[FC23(6)] `-outlook` scratch PST.** The remedy is specific: "the item could not be read
    from Outlook's export; open it in Outlook (it is still in your mailbox), or try again".
    It never says "repair the .pst", because that file is rebuilt and reclaimed.

### 4.5 mbox, maildir and Evolution readers (`internal/source/mbox.go`, `evolution.go`) (F29, F33)

- **Scope [FC2].** The rules below apply to **every** file or directory that a local reader,
  or input discovery, opens:
  - `readMaildirDir` entries;
  - `walkDir`'s entries, judged **before** `looksLikeMbox` is called;
  - `looksLikeMbox`;
  - `walkFile`;
  - `source.Open`'s mbox sniff;
  - `IsMailStoreDir` / `Detect`;
  - the Evolution walkers (`evolution.go:75-199`).

  All of them open files through one helper, `util.OpenRegularFile`. Discovery therefore
  cannot hang before the lock is taken or before `beginRun`.
- **Root.** At open, the reader resolves its root once (`filepath.EvalSymlinks` of the
  operator-given input path). Every containment check below is made against this resolved root.
  - **[FC3]** The recommended open path is an `os.Root` opened on that resolved root. It never
    resolves outside the root, and it also closes swaps of intermediate path components.
- **Entry judgment.** Each entry is judged by its `DirEntry.Type()`, which is the lstat type:
  - **Plain directory** (`IsDir`, with neither `ModeSymlink` nor `ModeIrregular`) → descend or
    recurse. This includes an Evolution shard [FC1].
  - **Symlink** → `os.Readlink` **first, judged lexically, before anything is resolved** [FC3].
    - Rejected outright: an absolute target, a UNC or drive-qualified target, or a target that
      leaves the root after a lexical `Clean`. The result is `symlink-outside-store` (file) or
      `symlinked-folder-skipped` (directory). The target is never touched.
    - Only a relative in-root target is then resolved and lstat'ed. If it is a regular file,
      read it. A directory or an irregular target → `symlinked-folder-skipped` or
      `symlink-outside-store`.
    - Never descend into a link.
  - **Special file** → `not-a-message-file` (info), never opened [FC1]. This applies **only**
    to the explicit list `ModeNamedPipe`, `ModeSocket`, `ModeDevice`, `ModeCharDevice`.
  - **`ModeIrregular` (Windows reparse points) [FC1].** Under go 1.26 `winsymlink`, a junction,
    a mount point and a cloud or HSM placeholder (OneDrive Files-On-Demand, Dropbox online-only,
    Azure File Sync) are all reported this way. Each is judged with `os.Readlink`:
    - **Readlink succeeds** (junction, mount point) → the directory-link rule:
      `symlinked-folder-skipped`, terminal, recorded.
    - **Readlink fails** (placeholder) → read the entry as a file. On Windows,
      `util.OpenRegularFile`'s descriptor check accepts an irregular handle that is not a
      directory. A placeholder hydrates on demand, as `os.ReadFile` does today.
    - A refused entry that might hold mail is **never** classed info.
  - **Regular file** → read it with `util.OpenRegularFile(path)`.
    - It is a sibling of `util.ReadSecureFile`, without the permission check.
    - On Unix it opens with `secureOpenFlags = O_NOFOLLOW|O_NONBLOCK`
      (`internal/util/securefile_unix.go:8`) and re-checks that the **descriptor** is regular.
      A FIFO swapped in after the lstat therefore neither blocks nor is read.
    - On Windows `secureOpenFlags` is 0, so the open itself does not refuse links. Containment
      there rests on the judgment made before the open.
- **Per-file read errors** (`mbox.go:144-147`):
  - **ENOENT** → re-list the directory **once** and look for the same unique part (a flag
    rename).
    - If found, read it.
    - If not, record `vanished-during-read` (fillable, self-closing).
    - A `new/` → `cur/` move is found later, when `cur/` is read.
  - **EACCES, EPERM, EIO and the rest** → a **`file-unreadable` fault** (item, locator
    `maildir:<folder>/<unique>`, fillable). **No message is yielded and no record is minted**
    [FC7]. The remedy is keyed by errno [FC23(6)]:
    - EACCES/EPERM: "fix the permissions on <path>";
    - EIO: "the disk returned a read error — check the drive".

    The walk continues.
- **Directory detection and descent.** `isMaildir` may still `Stat` for detection. Detection
  is now paired with a descent rule.
  - Before reading, each of these is **lstat**ed and must be a plain directory: `cur`, `new`,
    `.sbd`, the folder itself and Evolution's `subfolders` [FC1].
  - A symlinked one, or on Windows a junctioned one, is recorded as `symlinked-folder-skipped`
    and not read.
  - Detection does not switch to lstat alone, because that would silently drop a linked
    maildir. A folder that is detected but linked is always **recorded**, never silently
    skipped.
  - Not following directory links removes the 33-duplicate loop of F29d.
  - Whether in-store directory links should instead be followed, or opted in, is owner ruling
    **OR6**. **[ruling OR6]** They are followed only under an explicit per-input opt-in (a
    flag, a GUI option and a job-descriptor field, following X4/X5). With it, that input's
    directory links are followed, guarded by a visited (dev, inode) set, and the input keeps
    its token; file links out of the store and special files stay refused. Without it, an
    archive that captures through links today gets a terminal `symlinked-folder-skipped` gap
    whose remedy names the opt-in. S2b builds it.
- **Folder-level errors.**
  - A `ReadDir` failure on a non-root folder → `folder-unreadable`, and the siblings continue.
  - A failure on the store root stays a store-level error.
  - `walkFile` failing to open an mbox → `folder-unreadable`.
  - A stream error mid-file (`NextMessage` or `ReadAll`) → `folder-partially-read` with the
    count already read, and the next folder continues.
- **Raw-preserving stub (F33).** `safeParseMessage`'s recover returns
  `{Subject:"(unreadable message)", PlainBody: <bounded text>, Raw: data, ReadGaps:[unparsed]}`.
  This is the same shape as `fallbackParse`'s stub, which also gains `ReadGaps:[unparsed]`.
  - **[FC25] Bounded text.** The rendered and indexed `PlainBody` is the header block plus the
    first 64 KiB of `data`. It ends with a fixed note that the full bytes are in the `.eml`
    under `-raw`. `Raw` is kept whole.
  - **[FC25] Stamps.** The stub record carries the running `ParserGen` and a `RawDigest`
    (sha256 of `data`).
  - **[FC26] Identity.** The stub's identity is expressed through g3's declared hook, in
    `replayV1`'s panicked branch. `identityTermsOf` folds a digest of the raw bytes, so
    identities differ per message without a locator. Identical bytes are a true duplicate.
    - If g3 has not landed when S2b merges, S2b carries the edit in today's `safeParseMessage`.
      g3's MA-330 golden is then regenerated for panicking inputs only (§10).
    - **[ruling J5, J8 = ESC-1]** The fold moves the identity MA-330's golden pins, so it is
      escalated to the owner (ESC-1). The landing order is fixed (S1a → g3 S0 → g3 S1 → S2b),
      so S2b never edits today's `safeParseMessage` for identity, and the "regenerate the
      golden" route is not taken. S2b ships everything except the fold: the raw-preserving
      content, `Raw` kept, the `unparsed` gap and containment. `stubIdentityTerms` keeps
      today's constant stub, and the collapse of two or more unparseable raw messages in one
      folder stays an open gap, not accepted, encoded as a skip encoder citing R1 in MA-318's
      raw-stub case. The fold itself is slice S2b-id, after the owner answers.

### 4.6 Local orchestration (`internal/app/run.go` `runFile`)

The walk handler classifies each `Export` error before deciding what to do.

**[FC12] Archive-root probe.** Before recording the run's first `FolderError` or `ItemError`,
and again after every 50 such faults, the handler checks that the archive is still there:
- `Lstat` the `-out` root and the token directory;
- call `lock.StillHeld()`.

If either directory is missing, or the lock is lost, the outcome is **run-fatal**. The message
reads "the archive location `<out>` is unavailable — reconnect the drive and re-run" (§7.5), and
the run exits 1. A removed USB drive or a dropped share is therefore never recorded as a folder
of fillable gaps.

| Exporter result | Action |
|---|---|
| `export.IsRunFatal(err)` | return it; the source fails (exit 1) with the refusal text in §7.5. The set is: ENOSPC, EDQUOT, EROFS; **EIO, ENXIO, ENODEV, ENOTCONN, ESTALE, EHOSTDOWN** [FC12]; Windows `ERROR_DISK_FULL` 112, `ERROR_HANDLE_DISK_FULL` 39, `ERROR_WRITE_PROTECT` 19, and **3 (on the root), 21, 53, 55, 64, 1167, 1295** [FC12]; `export.ErrUnsafeToken` |
| `*export.FolderError` (output `MkdirAll`) | after the probe: `ObserveGap(folder-unwritable)`; add the write dir to a per-run `unwritable` set. Later messages whose write dir is in the set are counted into that gap's `Skipped` and not attempted. The folder's **first** attempt counts toward the local breaker [FC12]. Return nil |
| `*export.ItemError` (render, or write of a single file) | after the probe: `ObserveGap(export-failed, item, locator)`; increment the local breaker. Return nil |
| `Stats.Kept` (KEEP guard, P15) | `ObserveGap(refresh-failed, item, ident:<identity>)`, class info [FC5]. Return nil |
| a `recover` in the handler | `Stats.ExportPanics++`. `ObserveGap(export-panic)` with the folder and the subject. Return nil. **No longer log-only** (F33) |

Remedies come from `app.GapRemedy(reason, sourceKind, errnoClass)`, never from the reason alone
[FC12, FC23(6)].

Further rules:
- **Locators [FC8].**
  - A message's item locator is its `Locator` when set.
  - Otherwise it is `file:<store-rel>` for maildir.
  - For a **post-parse** fault on any local source, it is `ident:<m.Identity()>`. This replaces
    rev 1's `mbox:<folder>#<n>` ordinal, which shifted under compaction.
- **Local breaker.** It returns a run-level error for this source (exit 1) after 50 consecutive
  failed attempts, with no success between. Two kinds of failure count:
  - `ItemError`s;
  - each folder's first `FolderError` attempt [FC12].

  Items skipped because of the unwritable set do not count.
- **Checkpoint and lock [FC12].** The local checkpoint, and the `lock.StillHeld` gate inside it,
  are driven by items **processed plus faults**, as §4.3 already does for Graph. They are no
  longer driven by `stats.Exported` alone (`app/run.go:208-217`). A stretch of all-fault items
  still re-checks the lock.
- **Reader faults [FC29].**
  - Faults delivered through the walk callback are recorded as they arrive.
  - A trailing `*source.FaultsError` is recorded fault by fault.
  - In neither case is the source counted as failed.
- **Closure.** `CloseUnobserved` runs only when `Walk` returned nil or only a `FaultsError`.
- **`-outlook` export losses [FC28].** `outlookcom.CreatePSTs` now returns, alongside the stores:
  - the folders whose `CopyTo` failed;
  - the accounts it skipped.

  Each becomes a gap, and the run is then partial:
  - a failed folder or account → `outlook-export-skipped`;
  - an account already present as a `.pst` → `outlook-account-skipped` (info), naming the path
    to archive directly.

  Today `outlookcom_windows.go:222-230,284-285` only logs these.
- **Abort semantics, unchanged.** Context cancel and lock loss (`abort()`) are checked before
  every item, as today (`app/run.go:508-519`).

### 4.7 Exporter (`internal/export/exporter.go`)

1. **Typed errors.** `MkdirAll` failure (`exporter.go:301-303`) → `&FolderError{Dir, Err}`.
   Failed `.eml`, `.html` or render (`:318-329`, `:337-339`, `:384-387`) → `&ItemError{Path,
   Err}`. A `SafeToken` refusal (`:285-287`) → wraps `ErrUnsafeToken`. `IsRunFatal(err)`
   inspects `errors.Is` against the errno set in §4.6.
2. **KEEP guard: a degraded capture never replaces a record (P15) [FC5].**
   - **Where it runs.** On every source and in both modes. It runs **before** key placement
     (the `#fp`-split loop, `exporter.go:220-231`, and `placeByPhysID`, `:688-693`) and before
     any write.
   - **When it fires.** All three conditions hold:
     - the capture carries any `ReadGaps` label, or it is a Graph parse stub;
     - a same-token record exists for `m.Identity()`, or on Graph for `ref.PhysID`;
     - that record's gaps do not already include those labels.
   - **What it does.**
     - Nothing is written, renamed or removed.
     - The live path stamps `MergeFields(key, folder, RunAt, true)` / `TouchSeen`.
     - `Stats.Kept++`.
     - The orchestrator records an info `refresh-failed` gap naming the label.
   - **Never split.** A capture carrying `ReadGaps` is never `#fp`-split.
   - **Healthy input.** The guard cannot affect a healthy input, because `ReadGaps` is empty
     there (P14).
3. **Read gaps.**
   - Each `m.ReadGaps` label is appended to the record's gaps. Its class comes from
     `state.GapClass(label)`, the single owner of record gap labels [FC26]:
     - `unparsed` → Terminal;
     - `unreadable:body` and `unreadable:attachments` → Missing on a local source.
   - The record's `Subject` and `Date` are stored, as for any record with issues (B8).
   - A record carrying an `unreadable:*` label stores its `Locator` [FC6(3)]. A re-examine
     restores a missing one.
   - An `unparsed` record stores its `ParserGen` and `RawDigest` [FC25].
   - `Stats.Unreadable++` whenever an `unreadable:*` record is written or re-examined; likewise
     `Stats.Unparsed`.
4. **Healing partly-read records [FC6].**
   - **(1) `#fp` comparison.** When prev carries `unreadable:attachments`, the loop compares
     prev's `Fingerprint` with m's fingerprint computed **over zero attachments**. That is
     exactly how prev's fingerprint was computed, so a healthy re-read lands on the partial
     record's key instead of minting a "distinct Message-ID reuse".
   - **(2) The probe.** In `probeImproves`, an `unreadable:*` or `unparsed` label improves when
     the new read no longer carries that label.
   - **(3) Supersede, metadata only.** This handles a non-degraded message that is exported
     under a **different** key while a same-token record with the same `Locator` carries an
     `unreadable:*` label. That happens in the no-Message-ID case, where the identity includes
     the body.
     - That record's `unreadable:*` labels are re-classed to Terminal `superseded` (class info).
     - Its `RecoveredBy` is set to the new key, in the same record update.
     - **No file is written, rendered or deleted.** No page is re-rendered, so `Fixity` stays
       true, `Record.Path` is never used as a write target, and there is no crash window.
   - **(4) Unparsed stubs [FC25].** A local `unparsed` stub is superseded the same way, metadata
     only, by the record whose `RawDigest` matches.
   - **(5) Matching.** Matching is `(token, locator)`, and never across differing PST
     store-uids [FC8].
   - **(6) Residual.** A genuine distinct Message-ID reuse in one folder, whose attachment table
     fails, is held as `refresh-failed` for as long as it fails.
   - Rev 1's page-rewriting supersede is **removed** [FC7]: there are no stub records left for
     it to rewrite.
5. **Record notice (R7, R19).**
   - For a record carrying `ReadGaps`, `RenderWith` emits a `<div class="mailarchive-notice">`
     with fixed, escaped text: "This item could only be partly read from the source: <what>.
     Source: <store>, folder <f>. Reason: <ReadError>."
   - An `unparsed` stub's notice adds that the full bytes are in the `.eml` under `-raw`
     [FC25].
   - Every interpolated value is HTML-escaped and control-stripped.
   - The `mailarchive-` namespace is the tool's own, so mail cannot forge it (MA-187).
   - `reindex -rebuild`'s from-HTML reader skips the notice.
6. **Defensive.** `app/graph.go:623-626` (`m == nil` → `return nil`) becomes an `export-failed`
   gap. It is unreachable today, because `parseMessage` never returns nil, but silent if ever
   reached.

### 4.8 Graph (`internal/graph/graph.go`, `internal/app/graph.go`) (F9, H2, H3)

#### 4.8.1 Classification

**[FC21] One shared Graph primitive (slice SG).** The extraction workspace
(`docs/design-extraction-workspace.md` §4.5, E1) needs the same `internal/graph` primitives,
so they are built once, in a slice **SG**. SG lands before S3 and before the extraction
design's E1. Which design owns it is owner ruling **OR9**. **[ruling OR9 = J4]** This design
owns SG, as a standalone slice; the extraction design's E1 depends on SG, not on S3. Lenses
3, 6, 7 and 8 re-ran on SG's capture-side changes before SG code (RR-6). Its addendum is
filed (GO_WITH_CONDITIONS), and its conditions RR6-C1–RR6-C6 are folded and re-verified; the
re-verification raised owner rulings RR6-OR1 and RR6-OR2, whose safe interims block nothing.
The extraction design's E1 keeps its own short adversarial note.
- `statusErr` becomes `*graph.StatusError{Code int, GraphCode string, Text string}`.
  - `GraphCode` is parsed from `error.code`.
  - `Text` is bounded.
  - `Error()` keeps **today's exact text**, as the extraction design's MA-402 requires.
- **One classifier.** `graph.Classify(err, walkCtx)` returns the class and whether the fault is
  transport. Both consumers call it.
- **One streaming download, §4.8.6.** **[ruling J4]** Its `$value` request gets its own
  response-header timeout of at least the idle window, for capture as for the copy export
  (the extraction review's XC26), so a large conversion is not a permanent 60-second poison.
- **One nextLink guard, §4.8.7**, used in every paging loop (its host/path pin reaches
  capture's loops only in step SGc, [ruling J4]).
- **What stays per consumer.** The breaker thresholds and the 404 class, each with a written
  justification:
  - capture retries automatically, through the ledger, on every later run, so a 404 is
    fillable and self-closing here;
  - copy export is a one-off for one selection, so a 404 is terminal there.

`app.classifyFetch(err, walkCtx)` wraps `graph.Classify` and returns
`(reason, class, fatal, transport bool)`:

1. `walkCtx.Err() != nil` → **fatal**: the walk is cancelled. The child deadline is not
   consulted (H-F9.3).
2. `StatusError.Code` 401, or 403 per owner ruling OR7 (**[ruling OR7]**: a 403 on `$value`
   is mailbox-fatal, so a revoked grant never turns into thousands of gaps) → **fatal** for
   the mailbox.
   **[FC11]** A failed token refresh is fatal too, and is classified **before** rule 4. It
   takes either of two forms:
   - `errors.As(err, *oauth2.RetrieveError)`;
   - the delegated source's sign-in-again error.

   Both arrive from `c.hc.Do` with no response (`graph.go:543-545`;
   `internal/graph/delegated.go:190-203`). Either one is mailbox-fatal auth. It uses today's
   auth wording and the existing `auth_remedy` (MA-107), and records no gap.
3. 404, or `GraphCode == ErrorItemNotFound` → `vanished-at-fetch`. Not a transport failure.
4. 429 or 503 (after `get`'s retries), 502, 504, no response, or a network error →
   `fetch-transport`. **Transport.**
5. Idle, header or overall timeout → `fetch-timeout`. **Transport.** **[RR-6 RR6-C1]** These
   arrive as SG's typed `*graph.TimeoutError` (§4.8.6), never as a context error.
6. The body exceeds the size cap → `fetch-too-large` (terminal). Not a transport failure
   [FC21].
7. Any other response, including 500 with any `GraphCode` → `fetch-failed`. Not a transport
   failure.

#### 4.8.2 Per-item handling (`runGraphMailbox`, replacing `app/graph.go:619-622`)

On a fetch error:
- **fatal** → return it. This is today's behaviour: the mailbox fails, no sweep.
- **non-fatal**, in order:
  1. If `opts.Since` is set and `ref.Received` is before it, and the item has no record → it was
     not requested. Count it as `SkippedDate` and record no gap. An **existing** gap for that
     item is retained, not closed, by §4.3 step 3 [FC13].
  2. If Full mode and `ref.InternetMessageID` already has a same-token record →
     `refresh-failed` (info). The archive already holds that capture.
  3. Otherwise `ObserveGap(fetch-*/vanished-at-fetch, item, locator)`, with `ref.Subject`,
     `ref.Received`, the folder and the bounded detail.
     - The locator is `graph:<immutable id>` when this run's listing confirmed immutable ids
       (Preference-Applied), else `graph:<ref.ID>` [FC8(5)].
     - With an immutable-id locator, a moved item, or an item in a renamed folder, re-observes
       its own entry instead of stranding it [FC14].

  **No `manifest.Add`, no `byIdent` entry** (H3/P5). Transport-class faults feed the breaker of
  §4.8.5; any successful download resets its consecutive count. Return nil, so the listing
  continues.
- **Graph stub (F33).** If `ParseRFC822` returns an `unparsed` stub, the stub's
  `InternetMessageID` is set from the listing, but only when **both** hold [FC5]:
  - `ref.InternetMessageID != ""`;
  - **no** same-token record exists for that Message-ID or for `ref.PhysID`. If one exists, the
    KEEP guard holds the stub back, and the archived record is untouched (P15).

  The assignment is expressed through g3's hook: it writes `terms.InternetMessageID` as well
  [FC26].

  This is **stub-only**. Setting it for every no-mid message would change existing identities.

  The stub's identity is then `mid:<id>`. The fast path skips it on later runs, so there is no
  re-download storm. Its raw bytes are preserved: the `.eml` under `-raw`, and bounded escaped
  text on the page [FC25].
- **Parser upgrade [FC25].** An `unparsed` stub whose `ParserGen` is older than the running
  parser is **not** skipped by the fast path. It is re-fetched once in that run.
  - If the new parse succeeds, the record heals in place through `probeImproves` (§4.7.4(2)).
  - If it still fails, the stub's `ParserGen` is updated, and it is not re-fetched again until
    the next upgrade.
- **Graph legacy stubs [FC27].** On the first run with this build, a same-token legacy Graph
  stub is **adopted** instead of being left to the sweep. A legacy stub has Subject
  `(unreadable message)`, `Terminal:["body"]`, and a `sha:` identity over subject and Received.
  - It matches when `PhysID == ref.PhysID`, or when `Date == ref.Received`.
  - It is stamped (`TouchSeen`) and re-classed like the local legacy stubs (§6).
  - It is never marked gone because its identity changed.
- **Export panics and export errors** on the Graph path are classified as in §4.6, archive-root
  probe included [FC12].
  - `FolderError` adds the folder to the unwritable set.
  - A later ref in that folder that would need a download is recorded under the same
    `folder-unwritable` gap without being downloaded. That ref counts as a **fault** for
    §4.8.3 [FC4(d)].
  - The membership fast path still skips **and stamps** refs that are already archived.

#### 4.8.3 Sweep safety (H2, P7)

**[FC4]** `SweepGone` decides by each record's **last-known** `Folder` (`manifest.go:968-970`),
not by the folder where a fault happened. Sweep safety is therefore decided per record:

1. **A folder-scope fault skips the whole mailbox's sweep.** If any folder-scope fault was
   observed in the mailbox this run, `SweepGone` is **not** called for that mailbox this run.
   This is today's behaviour, deferred and never wrong. The faults that count:
   - `folder-list-failed`, including a listing-guard stop;
   - `client.Messages` failing in any folder.

   Without this rule, a message that moved Inbox → Projects would be swept from Inbox when
   Projects' listing fails.
2. **Known identity.** For each item fault with a known identity, every same-token key from
   `manifest.KeysForIdentity(identity)` gets `manifest.TouchSeen(k, runAt)`.
   - The identity is `"mid:"+ref.InternetMessageID`, or `m.Identity()` when the fault followed
     the parse.
   - **[FC4(b)] PhysID.** When this run's listing confirmed immutable ids (Preference-Applied)
     and `ref.PhysID` is set, every same-token record with `HasPhysID(ref.PhysID)` (its
     `PhysID` or `AltPhysIDs`) is stamped too. The immutable id survives a move.
3. **Unknown identity.** A fault has an unknown identity when there was no listing Message-ID,
   the fault came before the parse, and no record matched by PhysID. Its effect is narrower than
   in rev 1, where it excluded the folder:
   - it excludes the token's **PhysID-less `sha:`-identity records** from this run's sweep;
   - if immutable ids were **not** confirmed this run, it excludes **every** `sha:`-identity
     record of the token.

   The folder itself is still marked walked, so the rest of its records are swept normally.
4. **Unwritable set [FC4(d)].** A ref not attempted because of the unwritable set is a fault for
   rules 2 and 3. In Full mode a folder whose write directory became unwritable is therefore
   never swept clean.

`TouchSeen` never flips `Present` (`manifest.go:861-871`), so a failed fetch never resurrects a
gone record. These rules cover:
- Full mode;
- the immutable-id "comparable → download" path;
- post-parse export faults;
- moves.

**Residual (L10-3).** One case is not covered. It needs a pre-v6, PhysID-less `mid:` record whose
listing and parsed Message-IDs differ, and whose fetch fails while it sits in a folder that is
marked walked. That record can receive a phantom `{k,gone}` (P7). OR10 offers the stricter
alternative.

**[ruling OR10]** The stricter rule is taken, closing the residual: a fault whose identity
matched no record (no same-token key for its listing Message-ID, and no PhysID match) is
treated like rule 3's unknown identity, so the token's PhysID-less records are excluded from
this run's sweep. Gone-detection for them is deferred while such a fault persists, and
`status` names it (the `capture_gaps_stuck` line below); it is never wrong. MA-312 gains the
mismatched-Message-ID case.

A persistent unknown-identity suspension is reported by `status` (§4.10). The
`capture_gaps_stuck` line names it, and its remedy says how to end it [FC23(4)].

The history run footer (`app/graph.go:318-322`) is written only when `failures == 0 && !partial`.
Verified: no reader decides anything on the footer. `history.go`'s stats reader skips it
(`history.go:401-403`) and compaction keeps it, so this is conservative and truthful.

#### 4.8.4 Folder isolation

A `client.Messages` error that is not fatal is handled as follows:
- `ObserveGap(folder-list-failed, folder, "folder:"+f.ID)`;
- the folder is **not** marked walked;
- the mailbox's sweep is skipped this run (§4.8.3 rule 1) [FC4];
- the walk continues with the next folder;
- the breaker counts it if it is transport-class.

`client.Folders` failing remains a mailbox failure. Its paging loop now carries the same
nextLink guard (§4.8.7) [FC21]; its host/path pin comes with step SGc [ruling J4]. Its
recursion carries the visited folder-id set and the depth cap (§4.8.7) [RR-6 RR6-C6].

#### 4.8.5 Breaker

**[FC10]** The breaker has a rate bound and a time bound, and it spares known poison.

- **Counted faults.** Transport-class faults count, item or listing (`fetch-transport`,
  `fetch-timeout`, transport-class `folder-list-failed`).
  - **Known poison does not count.** This is a transport-class fault on an item that already
    holds an open gap with the **same reason** from an earlier run. Such a fault is evidence
    about that item, not about the service.
- **Trip conditions.** The breaker trips when any of these holds:
  - 25 consecutive counted faults with no successful download between (the fast path);
  - counted faults are ≥ 50% of the last 50 attempts;
  - cumulative time spent in counted faults in this mailbox exceeds 30 minutes.
- **Control probe.** Before tripping, one control request is made that is unrelated to the
  failing items: the current folder's listing page, or `/mailFolders/{id}?$select=id`. The
  breaker trips only if that request fails too. Otherwise the counters reset and the walk
  continues.
- **On a trip.** The mailbox ends with a run-level error, using the text in §7.5, which prints
  the threshold that fired.
  - Gaps recorded so far are kept.
  - There is no sweep and no `CloseUnobserved` for that mailbox.
  - Other mailboxes in the run continue.
- **Run-level breaker.** If **two consecutive mailboxes** trip, the run stops (exit 1). An
  outage then costs at most about two mailboxes' budget, not 25 timeouts per mailbox across 40
  mailboxes.
- **Item-class faults** (`fetch-failed`, `vanished-at-fetch`, `fetch-too-large`) **never** trip
  it.
- **The §8 claim, conditionally.** A sender cannot stop a mailbox walk by mailing poison
  messages. That holds for item-class poison unconditionally. For poison that stalls or 504s,
  it holds through the known-poison and control-probe rules, until MA-313's three new cases are
  green:
  - 1 success per 10 attempts → trips;
  - 25 known-poison 504s from run 1 → run 2 walks past them;
  - 25 consecutive per-item timeouts with a healthy control → no trip.
- **Thresholds.** Every threshold in this design is fixed by design in this revision: 25, 50%
  of 50, 30 min, 50 local, 100 PST rows, depth 300 (the figure SG confirms), 1000/1000/100
  ledger caps, 2 min idle, the `$value` header window (2 min, never below the idle window),
  2 h overall (capture's 10 minutes until S3), the size cap (the figure SG confirms, 256 MiB or
  higher), the 100 000-page cap, `Retry-After` clamps [RR-6 re-verification].
  Operator-tunable settings are an operability-backlog item. The breaker, timeout and guard
  messages print the value that fired.

#### 4.8.6 Download bounds (F9.5, P3)

- **[FC21] One streaming download.** SG adds `MIMETo(w io.Writer, maxBytes, idle, overall)`.
  `Client.MIME` calls it with a buffer, so capture and copy export share one bounded download.
- **Idle watchdog.** `MIMETo` replaces `context.WithTimeout(ctx, c.mimeTimeout)` with a reader
  wrapper. The wrapper cancels the request context if no byte arrives for `MIMEIdleTimeout`
  (default 2 min).
  - **[RR-6 RR6-C1] Typed stops, never a cancel.** `MIMETo` runs its idle, header and overall
    timers itself, on a child context of the caller's.
    - While the caller's context is live, every stop of its own returns a typed
      `*graph.TimeoutError{Kind: idle|header|overall, Limit, MessageID}`. That includes an
      overall stop that lands inside `get`'s `Retry-After` select (`graph.go:555-557`).
    - **[RR-6 RR6-C1; re-verification] What each timer measures.** The header timer times
      each HTTP attempt, from sending the request to its response headers, and starts again on
      the next attempt. It does not run during `get`'s `Retry-After` sleeps, so a throttled
      item waits as long as the overall cap allows, as it does today. The idle timer runs only
      while a body is read. Only the overall cap and the caller's context bound the whole call,
      sleeps included. MA-540's `Retry-After` twin asserts this.
    - The error names the message and the limit that fired (MA-98, X2). It never unwraps to
      `context.Canceled` or `context.DeadlineExceeded`.
    - Why: a plain cancel surfaces as `context.Canceled`, which shipped capture reads as an
      operator interrupt. `RunGraph` skips every later mailbox (`app/graph.go:294-297`),
      `recordRun` writes `cancelled` (`run.go:427-428`), and `graph` exits 0
      (`main.go:1140-1143`).
    - When the caller's context is done, `MIMETo` returns that context's error.
  - **[RR-6 RR6-C1] Second guard.** `RunGraph` decides "cancelled" from its own `ctx.Err()`,
    not from the error chain. It returns `context.Canceled` only when its context is done.
    While its context is live it never returns an error that matches `context.Canceled`,
    because `recordRun` and the CLI test the chain. MA-540 asserts both guards on the shipped
    path.
- **Overall cap.** The watchdog sits under an overall cap, `MIMEMaxDuration` (default 2 h).
  **[RR-6 RR6-C2; amends ruling J4]** Capture gets the 2 h cap in S3, in the same slice as
  FC10's `Retry-After` clamp, the breaker's time bound and the run-level breaker, so the 2 h
  cap never reaches capture without them.
  - Until S3, `Client.MIME` passes `MIMETo` an overall cap of `Config.MIMETimeout`, whose
    default stays today's 10 minutes (`graph.go:74`). It bounds the whole call, `get`'s
    `Retry-After` sleeps included, as today's `context.WithTimeout` does.
  - Before S3 any fetch fault still fails the mailbox. A mailbox therefore pays at most
    today's 10 minutes per run for throttle sleeps or a trickle. A download that needs longer
    still fails, as it does today, until S3 (F9).
  - The copy export passes its own 2 h from E1.
- **Size cap.** The size cap is no lower than Exchange Online's maximum message size plus MIME
  encoding overhead. ~~It is 200 MiB, as in the extraction design, and S3 confirms that figure
  against Microsoft's documentation.~~ **[RR-6 RR6-C3; amends ruling J4]** SG confirms the
  figure before SG code, because SG ships the cap to capture.
  - **The confirmation.** From Microsoft's documentation, SG establishes the largest `$value`
    an Exchange Online item can produce. It covers stored items (drafts with upload-session
    attachments of up to 150 MB, imported items), not only the transport limit. It counts in
    binary megabytes, with base64's 4/3, CRLF's 78/76 and the headers. The arithmetic is
    written here.
  - **The figure.** 150 MiB of attachment bytes is 150 × 4/3 × 78/76 ≈ 205.3 MiB of MIME
    before headers, which is above 200 MiB. The shared cap is therefore 256 MiB, or higher if
    the confirmation requires it.
  - **Early refusal.** A response whose `Content-Length` exceeds the cap is refused before its
    body is read, so an over-cap item is not downloaded again on every run.
  - **[RR-2b RR2b-C8] A typed size stop.** Like the timers' stops, the size refusal is typed:
    `*graph.TooLargeError{Limit, Declared, MessageID}`. `Limit` is the `maxBytes` the caller
    passed, and `Declared` is the response's `Content-Length` (−1 when none), for the early
    refusal and for a body that streams past the limit alike. The copy export passes a limit
    below the cap when its free-space budget is smaller (its RR2-C5), and `Declared` lets it
    tell an item above the cap from its own budget. `graph.Classify` still classes the stop
    `fetch-too-large`, so nothing changes for capture. The extraction design's MA-402
    asserts the fields.
  - ~~If the documentation cannot bound a stored item, or the limit later rises, the cap is an
    open condition, not accepted, stated at P3 and §12.~~ **[RR-6 RR6-C3; re-verification]**
    That branch shipped the cap to capture where no figure bounds it, so an item that today's
    uncapped `io.ReadAll` (`graph.go:505`) archives could fail its mailbox on every scheduled
    run before S3 and stay a terminal gap after it. It is reverted, and the choice is owner
    ruling **RR6-OR1** (review addendum). Its safe interim:
    - **Only a bounded figure reaches capture.** If the documentation cannot bound a stored
      item, `Client.MIME` passes no size cap, as today, and the copy export alone carries the
      cap (256 MiB) from E1. Capture's download is then bounded by time only (the idle,
      header and overall timers), and its memory by §3 N9's pre-existing bound of
      whole-message reads.
    - That missing cap is an open gap, not accepted, stated at P3, §8 and §12. While capture
      passes no cap, MA-540's over-cap case and MA-314's `fetch-too-large` gap case are skip
      encoders citing their rows.
    - **Conditional.** A cap that reaches capture holds a legal item only while Microsoft's
      documented limit stays at or below it. SG records here the documentation it relied on
      and the date it was checked. A later documented rise past the cap puts capture back on
      the rule above from the release that learns of it. Until that release, an over-cap item
      fails loudly, naming the item and the cap. That window is part of RR6-OR1's question;
      until it is answered it is an open condition, not accepted.
  - An item over the cap is a `fetch-too-large` terminal gap from S3. **[RR-6 RR6-C3]** Before
    S3 it fails the mailbox through today's error path, and the typed error names the item and
    the cap.
  - It is never truncated.
- **Time to first byte.** ~~`ResponseHeaderTimeout` (60 s, `graph.go:137-140`) still bounds
  it.~~ **[ruling J4; RR-6 RR6-C1]** The `$value` request has its own header timer, of at least
  the idle window and inside the overall cap. `MIMETo` runs it as one of its typed stops.
  - A context timer can only shorten the shared transport's 60 s `ResponseHeaderTimeout`
    (`graph.go:137-140`), never lengthen it. The `$value` request therefore goes through its
    own base transport, without that 60 s bound, under the client's token source.
  - JSON requests and the token endpoint keep the shared 60 s.
  - **[RR-6 RR6-C1; re-verification]** The header window is `MIMEHeaderTimeout`, default 2 min
    and never below the idle window. It times each attempt (above).
  - **[RR-6 RR6-C1; re-verification] The redirect seam.** The `$value` client is SG's own, not
    the shared client that the extraction design's E1 hardens. E1's `CheckRedirect` (its
    XC26) must be set on this client too, and E1's `Client.Timeout` (the F15 fix) must not be,
    because it would bound the whole download. The extraction design's §4.5 and MA-528 say
    so, and EW-E1's short adversarial note checks it.
  - **[RR-2b RR2b-C3] How SG builds that client.** At v0.37.0, `oauth2.NewClient` copies
    `CheckRedirect`, `Jar` and `Timeout` from the context's client into the client it returns
    (`oauth2.go:357-366`), whatever its doc comment says. So SG builds the `$value` client
    itself, over its own base transport and the client's token source. It never builds it
    with `NewClient` from a context that carries the shared client, whose copy would bring
    E1's timeout. E1 sets its `CheckRedirect` on this client, and its `Timeout` stays zero.
    The extraction design's MA-527 asserts both halves.
- **[FC10] `Retry-After` clamp.** Until F17 lands, `get`'s `Retry-After` sleeps are clamped to
  ≤ 60 s each and ≤ 5 min per item. Without the clamp, the 2 h cap would replace today's
  10-minute bound on those sleeps (`graph.go:494`, `556-569`). **[RR-6 RR6-C2]** The clamp
  lands in S3 (§10), together with capture's 2 h cap. Until then capture's 10-minute overall
  cap still bounds those sleeps.
- **Deprecated alias.** `Config.MIMETimeout` is kept as a deprecated alias for the overall cap.
  **[RR-6 RR6-C2]** Until S3 it is capture's overall cap, with today's 10-minute default.
- **Test hooks.** They scale all of these.

#### 4.8.7 Listing loop guard

**[FC21]** Every paging loop gets the same guard: `Folders`, `Messages`, and the extraction
design's `FolderTree` and `MessagePage`.
- It keeps a set of `nextLink`s already fetched.
- It pins each `nextLink` to the client's base host and path, so the bearer token is never sent
  elsewhere.
- It caps the loop at 100 000 pages.

**[ruling J4]** SG ships the repeat set and the page cap in every loop, capture's included,
and the host/path pin (with the extraction review's XC25 normalization: case-insensitive
host, default-port normalization, a path prefix on a segment boundary) on the extraction
design's loops. Capture's `Folders` and `Messages` get the pin in step **SGc**, which waits
for MA-443's recorded nextLink shapes (the extraction review's XC25 gate, which protects
scheduled backups from a harmless Microsoft-side variation, L8-2). FC21's capture half
therefore lands behind XC25's gate, and both conditions hold. Lifting the gate is the
owner's decision (ESC-7). Until SGc lands, a foreign `nextLink` in capture's listings is an
open gap, not accepted, encoded by MA-314's foreign-host case as a skip encoder.

A repeat, a foreign host or the page cap ends the loop. **[RR-6 RR6-C5]** It ends it with a
typed error naming the stop, never as a quiet end of listing. A quiet end would let
`runGraphMailbox` mark the folder walked (`app/graph.go:647`), and `SweepGone` (`:660`) would
then mark every record past the stop gone (H2). The outcomes:
- in `Messages`, the folder ends as `folder-list-failed` ("listing did not terminate"); before
  S3 lands, the stop fails the mailbox instead, which is today's error path [ruling J4];
- in `Folders`, the mailbox fails.

**[RR-6 RR6-C5]** On capture's shipped path, MA-540 asserts the pre-S3 `Messages` stop (no
`{k,gone}`), the page cap and its per-loop scope, and the page cap in `Folders` [RR-6
RR6-C5; re-verification]. MA-314's `Messages` gap case lands with S3.

**[RR-6 RR6-C6] Folder hierarchy.** The guard bounds each paging loop, but `Folders` opens a
new loop at every level, and the client builds each level's first URL (`graph.go:200-205`,
`228-231`). A cyclic or endless `childFolders` response therefore never repeats a `nextLink`,
and no loop reaches the page cap. `Folders` also keeps two guards:
- **One visited folder-id set across the whole call.** A folder id already visited is neither
  listed nor expanded again, so a cycle ends. A revisit is skipped, not fatal: a live mailbox
  can list a folder twice when it moves during the walk, and the guard must not fail a healthy
  walk.
- **A depth cap of 300**, the Exchange hierarchy figure P3 uses, confirmed in SG (P3). A deeper
  level stops the walk with a typed error naming the cap, and the mailbox fails, as a `Folders`
  repeat does. **[RR-6 RR6-C6; re-verification]** A chain exactly at the cap completes,
  counted as the confirmation counts levels, and MA-541's at-cap twin asserts it. If the
  confirmation gives a higher figure, the cap is that figure. If it gives none, `Folders`
  carries no depth cap until one is bounded (P3).

MA-541 asserts both. A response that invents fresh folders at every level is bounded in depth
only, and in breadth only by the per-loop page cap. That is an open condition, not accepted
(§8). **[RR-6 RR6-C6; re-verification]** It is stated at the claim it qualifies (P3, §12),
and MA-541's breadth case encodes it as a skip encoder citing R10: a fake server that invents
two fresh child folders at every level must see `Folders` return under a time guard. Whether
to close it with a total-folder cap is owner ruling RR6-OR2.

### 4.9 Outcome: errors, exit codes, last-run record

- **The error.** `app.Run` and `app.RunGraph` return `app.ErrPartial`, a `*PartialError{Summary
  GapSummary, ReportPath string}` that matches `errors.Is(err, app.ErrPartial)`, exactly when:
  1. no run-level failure occurred,
  2. the run was not cancelled, and
  3. `Result.Partial` is true.

  Precedence: a run-level failure in any source or mailbox returns that error and exits 1,
  even if other sources were partial. A cancellation stays `context.Canceled`.
- **The rulings [ruling OR1, OR2].** X1 is not amended: wherever this section says a
  capture verb exits `3` on a partial run, read **exit 1 with the `partial` flag**. The
  last-run status is `failed` with `partial: true`.
- **Interim mapping until the owner rules [FC33].** Until OR1 and OR2 are ruled, slice S1a
  maps a partial run to today's failure:
  - `ErrPartial` surfaces as a run failure: exit 1, last-run `failed`, `run_failed` RED;
  - the "Not captured" block and the report still print.

  That is loud and fail-closed, with no X1 change. S1b switches to the mapping below once the
  rulings land. **[ruling OR1, OR2]** S1b keeps exit 1 and adds the `partial` flag, so
  `status` shows the partial line rather than `run_failed`.
- **`recordRun`** (`app/run.go:422-436`) maps `ErrPartial` → the status OR1 rules (`"partial"`,
  or `"failed"` plus `Partial:true`), with the gap fields set and `Error` holding the one-line
  summary. **[ruling OR1]** `"failed"` plus `Partial:true`.
- **Attention sidecar.** `updateAttentionSidecar` (`lastrun.go:115-140`) writes
  `BACKUP-NEEDS-ATTENTION.txt` for a partial run too, with its own body. **[FC23(2)]** The body
  leads with what is new:
  - "This email backup finished, but K item(s) could not be archived **for the first time this
    run** (N item(s) and F folder(s) in total). Everything else was saved."
  - "Details: attachments-report.tsv. Run `mailarchive status -out …` for what to do."

  Only an `ok` run removes the sidecar. A cancelled or running record leaves it unchanged.
  Whether a run whose gaps are **all** already known keeps the sidecar and exit 3 is owner
  ruling **OR3**. **[ruling OR3]** It does: every run that observes a fillable or terminal
  gap is partial on every surface (exit 1 under OR2, the sidecar kept, the `-job`
  notification sent), and each surface leads with "new this run: K" and
  `last_run.gaps_new`, so monitoring can tell new loss from known loss.
- **CLI (`cmd/mailarchive/main.go`).** `runExport` and `runGraph` print the summary for nil,
  cancelled **and partial** runs (`main.go:228-236` gains the partial case). They then return
  `&exitError{code: 3}` (**[ruling OR2]**: `code: 1`, with the partial summary). The deferred `FAILED:` lines of both verbs (`main.go:160-164` for
  export, `:1122-1126` for `graph`) skip `ErrPartial`. `reclaimPSTDir` treats partial as success: the `-outlook` scratch PST is rebuilt
  every run, so keeping a mailbox-sized copy for a gap buys nothing.
- **Headless.**
  - `mailarchive-gui -job` (`cmd/mailarchive-gui/main.go:171-178`) returns 3 on partial
    (**[ruling OR2]**: 1).
    - It logs `FINISHED WITH PROBLEMS: …`.
    - It notifies "backup finished; K new item(s) could not be archived (N in total)"
      [FC23(2)].
    - Its scratch reclaim gate (`main.go:181`) becomes
      `runErr == nil || errors.Is(runErr, app.ErrPartial)` [FC19].
  - `mailarchive-desktop -capture` (`desktop.HeadlessCapture` →
    `cmd/mailarchive-desktop/main.go:72`) exits 3 the same way (**[ruling OR2]**: 1).
  - The Windows `.cmd` wrapper propagates the last command's errorlevel unchanged
    (`internal/schedule/wrapper.go`).
- **Interactive GUI wizard [FC19].** `cmd/mailarchive-gui/main.go:1094-1137` calls `app.Run`.
  On `errors.Is(runErr, app.ErrPartial)` it first shows the export summary with a "Not
  captured" section and the report path. It then continues exactly as on success:
  1. it calls `rememberArchive`;
  2. it calls `cleanupOutlookScratch` (both call sites);
  3. it offers "Open the archive";
  4. it runs `offerSchedule`.

  The result → next-step mapping is a pure function, asserted by MA-325, so the wizard is never
  left on "treats partial as failure".

### 4.10 Surfaces

- **Run summary (`printSummary`).** It keeps the `Done.` line and adds, when
  `Gaps.Observed > 0`:

  ```
  Not captured: 3 item(s) (1 new this run), 1 folder — 2 will be retried when you run again, 2 need action — details in <out>/attachments-report.tsv
    ann@contoso.com/Inbox: "Quarterly report" (2026-03-02) — Microsoft 365 returned an error (500 ErrorMimeContentConversionFailed); retried when you run again. If you are about to lose access to this mailbox, save a copy from Outlook on the web now
    ann@contoso.com/Archive/長いフォルダ名…: folder could not be written to E:\archive (file name too long) — rename it in your mail app or archive to an NTFS/APFS drive
  Recovered: 4 item(s) that failed on an earlier run were captured this run.
  ```

  - **Detail lines.** At most 20, then `+N more — see the report`. Each line is prefixed with the
    store's source id (mailbox address or input path) [FC22].
  - **Remedies.** They come from one exported table, `app.GapRemedy(reason, sourceKind,
    errnoClass)`, shared with `status` and with the extraction design's copyexport [FC20,
    FC23(6)].
  - **Destination-side reasons** name the archive location ("could not be written to <out>"),
    never "could not be read" [FC20]. These are `folder-unwritable`, and `export-failed` on a
    write.
  - **Deadline remedy [FC23(1)].** The Outlook-on-the-web save-a-copy remedy is shown at the
    **first** sighting, phrased for the deadline case.
  - **Verification lines.** A `Verification:` line for partly-read and `unparsed` records gets
    its own remedy, never "download for offline use".
- **`status` (`internal/health/health.go`).** `Gather` reads three counts:
  `m.CaptureGapCounts(lastRunAt)` (attempted and not-attempted split by each token's last run
  [FC14]), and the new `m.UnreadableCount()` and `m.UnparsedCount()`.

  New `Assess` rules, each naming its remedy, which is keyed by reason, source kind and errno
  class [FC23(6)]. **RED stays reserved for run-level failure and corruption** (the existing
  partition, `health.go:163-262`) [FC18].
  - **`capture_gaps_stuck` — WARN, listed first [FC18].** "N item(s) have failed on every run
    since <first> — <reason remedy>". Only *attempted* gaps can be stuck. The threshold is owner
    ruling OR5 (**[ruling OR5]**: §4.3 step 4).
    - For a `fetch-failed` gap with a GraphCode, the remedy is: "Microsoft 365 will not export
      this item; save a copy from Outlook on the web. The row stays listed while the item
      remains in the mailbox."
    - When a stuck gap is a no-mid Graph fault, the line adds: "deleted messages in <folder>
      are not being detected while this persists; after saving a copy, move or delete the item
      out of <folder> in the mailbox to resume deletion tracking there" [FC23(4)].
  - **`capture_gaps_fillable` — WARN.** "N item(s)/folder(s) could not be captured on the last
    run; they are retried whenever the archive runs again (see <report>)".
    - The word "automatically" is used **only** when a schedule is installed. `health` already
      knows this (`HasDescriptor`) [FC23(1)].
  - **`capture_gaps_terminal` — WARN.** "N item(s)/folder(s) are NOT in this archive and will
    not be without action — <reason remedy>".
    - For PST, the remedy is scanpst in place.
    - For an `-outlook` scratch PST, it is to open the item in Outlook.
  - **`capture_gaps_not_attempted` — a note (**[ruling OR3]**: not-attempted gaps stay a note) [FC14].** "N
    item(s) from <source> were never captured (last attempted <date>; this source or folder is
    not in recent runs)". These gaps never feed stuck or the "retried" wording.
  - **`unreadable_messages` — WARN.** "N message(s) could only be partly read from the source".
    - For PST, the remedy is: "repair it (scanpst.exe) in place, then re-run".
    - These records are **excluded** from the `incomplete_content` text and count.
    - `fillable` in JSON keeps its existing meaning: all fillable records.
  - **`unparsed_messages` — a note**, with no posture change. It adds "after upgrading, the next
    run re-examines them once" [FC25].
  - **`partial` last-run handling.** A `Last run: … → partial (N not captured, K new)` line. No
    `run_failed` RED; the posture comes from the gap rules above. S1a keeps today's `failed`
    mapping (§4.9); from S1b, `status` reads the record's `partial` flag [ruling OR1].

  **[FC22] Gap lines.** After the reason lines, `health.Summary` lists up to 20 gaps from
  `CaptureGapsSorted()`. Each line gives source id, folder, subject, date, reason and remedy,
  control-stripped. The list ends with `+N more — see the report`.

  **JSON.** These keys are additive:
  - `capture_gaps{fillable,terminal,info,stuck,not_attempted,oldest_first_run,items[]}`;
    `items[]` is bounded to 20;
  - `unreadable`;
  - `last_run` gains the partial counts, including `gaps_new` [FC23(2)].

  **`JSONVersion`.** It depends on OR1. Adding keys is covered by the house rule
  (`health.go:762-768`). Widening the documented values of `last_run.status`
  (README.md:609-611) is not:
  - If OR1 adopts the value `"partial"`, `JSONVersion` becomes 3 and README.md is updated.
  - If OR1 adopts `"failed"` plus `partial:true`, `JSONVersion` stays 2.

  That choice is the owner's [FC31(6)]. **[ruling OR1]** `"failed"` plus `partial:true`:
  `JSONVersion` stays 2, and only additive keys are added.
- **Report (`internal/app/report.go`).** After the record rows, one row per ledger gap:
  - `kind` = reason;
  - `class`;
  - `folder`;
  - `date` = `Received`, else `FirstRun`;
  - `subject`;
  - `detail` = `<locator> · <detail> · <runs> run(s) since <first>`, with "N item(s) in this
    folder not archived" for a folder or overflow row. A not-attempted gap adds "not attempted
    since <LastRun>" [FC14].
  - `path` = the store's **source id** (the mailbox address or input path), taken from the
    reverse of `manifest.Stores` and control-stripped [FC22].

  Every field passes through the existing `tsv()`. Order: source, then folder, then reason, then
  locator.
  - **[FC24] When the report is written.** `writeReport`'s early return that deletes the report
    (`report.go:43-47`) is taken only when `Issues()` **and** the ledger are both empty. A
    ledger-only archive, which is the common F9 case, gets its report.
  - **Labels.** `gapKind` maps the new labels:
    - `unreadable:body|attachments` → `unreadable-body|attachments`;
    - `unparsed` → `unparsed-message`;
    - `superseded` → `superseded` [FC6].
  - `reindex` regenerates the same report.
- **`verify`.** `verify` itself has no new verdict. The summary adds the informational line
  "Not in this archive: N item(s), F folder(s) from the source could not be captured (see
  attachments-report.tsv) — this does not affect attestation". The exit code is unchanged
  (X1 for `verify`).
- **README.txt (`internal/app/readme.go`).**
  - The `attachments-report.tsv` entry becomes "messages, folders, attachments or bodies that
    could not be captured".
  - A new paragraph explains "(unreadable message)" pages (raw-preserving `unparsed` stubs) and
    partly-read pages.
  - **[FC23(5)] The class vocabulary.**
    - "class: fillable = a later run of the tool against the original source may capture it;
      terminal = the source itself would not deliver it as it stands; info = nothing missing."
    - "If no one runs the tool against the source anymore, every fillable or terminal row is
      missing from this archive."
  - The `BACKUP-NEEDS-ATTENTION.txt` entry adds "or finished with items it could not capture".
- **Root `index.html` (`internal/pages/pages.go`).**
  - **The count.** `Generate` takes a count: ledger entries, plus partly-read records, plus
    `unparsed` records.
  - **[FC24] Every caller.** Every `pages.Generate` caller passes the manifest-derived count:
    `run.go:259`, `graph.go:325`, `reindex.go:166` (reindex and redaction) and `reindex.go:361`
    (rebuild). A redaction or `reindex -rebuild` therefore never drops the banner.
  - **The banner.** When the count is non-zero, the root page shows a fixed, escaped,
    script-free line [FC23(7)]: "Some items from the original mail source may not be in this
    archive — see attachments-report.tsv".
    - It shows a count only, never untrusted text.
    - The archive CSP is unchanged (R19).
  - **Re-render.** `pagesNeeded` also returns true when the count changed since the last
    render. The count is stamped in a `data-` attribute that `Generate` compares.
- **Desktop and GUI.**
  - **[FC23(1)(2)(3)] The capture card.** `startCapture`'s result
    (`internal/desktop/capture.go:142-146`) maps `ErrPartial` to: "Finished with problems —
    12,344 archived; 2 messages and 1 folder couldn't be captured (2 new this run). They are
    retried whenever you archive again (**Archive now**). View details".
    - "Archive now" is the **existing** action. No new control is added.
    - "Retried automatically" appears only when a schedule is installed.
    - When a fillable Graph gap is new, the card adds the deadline remedy: "If you are about to
      lose access to this mailbox, save a copy from Outlook on the web now".
    - There is no "of M" total.
    - Items are counted apart from folders, and a folder gap says that its messages were not
      read.
  - **Status & health** mirrors `status` through `health` (X10), gap lines included [FC22].
    - Until the dashboard read guard (F3/E0) lands, its GET page shows each gap line **without
      the subject**: source, folder, date, reason and remedy.
    - The subject appears on the dashboard only after that guard, and stays available in the
      CLI `status` and the report (§8).
  - **The GUI** launch health view and wizard summary read the same (§4.9, [FC19]).
  - **[FC20] The copy flow is out of scope here.** The owner's simple select-source →
    choose-destination copy flow is the extraction workspace's own engine. It never receives
    `ErrPartial`, and its result wording is defined in that design's §4.13. This design shares
    with it only SG (§4.8.1) and `app.GapRemedy`. As a result, the same Microsoft error reads
    the same on the capture card and on the copy page.

### 4.11 Property → mechanism map

**[FC32]** Each "proven" clause names the MA case that asserts it. A clause with no asserting
case is labelled conditional.

| P (clause) | Mechanism (functions / files) | Asserting MA case |
|---|---|---|
| P1 Graph item | `runGraphMailbox` per-item handling (§4.8.2) | MA-310 |
| P1 PST row/node | `reader.walkMessages`, `convertMessage` (§4.4) | MA-315, MA-318 |
| P1 maildir file | `readMaildirDir` (§4.5) | MA-321 |
| P1 parse/export panic | `safeParseMessage`; `runFile` handler (§4.6) | MA-318 |
| P2 PST sub-folder / message table | `reader.walk` sub-folder rows; `GetMessageTableContext` | MA-316 |
| P2 Graph listing page | `client.Messages` folder isolation (§4.8.4) | MA-316 (new Graph case) [FC32] |
| P2 maildir non-root `ReadDir` | `readMaildirDir` folder errors | MA-316 (new) [FC32] |
| P2 mbox open / mid-stream | `walkFile` → `folder-unreadable` / `folder-partially-read` | MA-316 (new) [FC32] |
| P2 Evolution walkers | `evolution.go` folder errors | MA-316 (new) [FC32] |
| P2 unwritable folder | `export.FolderError`, unwritable set | MA-320 (local), MA-501 (Graph) |
| P2 `-outlook` export skips | `CreatePSTs` skipped folders → `outlook-export-skipped` | MA-320 (unit mapping); MA-504, Windows+Outlook L row pending [FC28] [ruling J1] |
| P3 special files, every open | entry judgment + `util.OpenRegularFile` at every local open (§4.5) | MA-322 (maildir, mbox FIFO at discovery and during the walk) [FC2] |
| P3 Windows reparse | FC1 classifier | MA-322 (synthetic FileMode table; unprivileged junction); MA-503, L row pending (OneDrive placeholder) [ruling J1] |
| P3 cycle / depth | visited and depth guards | MA-317 |
| P3 structural row breakers | message and sub-folder breakers (§4.4) | MA-316 |
| P3 MIME idle, cap, size | `MIMETo` (§4.8.6) | MA-314 (idle, trickle, **overall cap firing**, size cap and its boundary) [FC32] [RR-6 RR6-C3]; MA-540 (typed stops through `RunGraph`, never a cancel) [RR-6 RR6-C1]; capture's 10-minute overall cap until S3: MA-98, unchanged [RR-6 RR6-C2]; the header timer per attempt: MA-540's `Retry-After` twin; the size cap on capture only at a bounded figure: MA-540's over-cap case, a skip encoder while capture passes no cap (RR6-OR1) [RR-6 RR6-C1, RR6-C3; re-verification] |
| P3 nextLink, every loop | §4.8.7 | MA-314 (Messages and **Folders**, repeat and foreign host); MA-540 (capture's pre-S3 `Messages` stop with no `{k,gone}`, the page cap and its per-loop scope) [RR-6 RR6-C5], and the page cap in `Folders` [RR-6 RR6-C5; re-verification] |
| P3 Graph folder hierarchy | `Folders` visited folder-id set and depth cap (§4.8.7) | MA-541 (cycle, moved folder, depth and its at-cap twin) [RR-6 RR6-C6]; breadth: MA-541's skip encoder citing R10, an open gap (RR6-OR2) [RR-6 RR6-C6; re-verification] |
| P3 Retry-After clamp | `get` clamp | MA-313 (new) |
| P4 | `state.CaptureGap`, `ObserveGap`; `model.ReadGaps` → `state.GapClass`; `Load` sanitisation | MA-318, MA-323, MA-327 |
| P5 | ledger separate from `Entries`/`byIdent`; §4.8.2 "no Add on failure" | MA-311, MA-324, MA-502 |
| P6 | ledger gaps for nothing-readable items; raw-preserving stubs; partial records with `Locator`; metadata-only supersede | MA-315, MA-318, MA-319 |
| P7 | §4.8.3 mailbox sweep skip; `TouchSeen` incl. PhysID; narrowed exclusion | MA-312 (a)–(f) |
| P8 401 / 403 / token refresh | `classifyFetch` rule 2 | MA-313 (401, **403**, mid-walk `invalid_client`) [FC11, FC32] |
| P8 disk and volume | `IsRunFatal`; archive-root probe | MA-320 (ENOSPC, **EDQUOT, EROFS** by errno injection; **Windows codes** by classifier table; `-out` removed mid-walk) [FC12, FC32] |
| P8 `SafeToken` | refusal | MA-320 (new refusal case) [FC32] |
| P8 breakers | §4.8.5; local breaker | MA-313, MA-320 |
| P9 | `app.ErrPartial`, `recordRun`, status `failed` + `partial` [ruling OR1]; CLI, GUI wizard, `-job`, desktop exit mapping (exit 1, [ruling OR2]) | MA-325 |
| P10 | `printSummary`, `health.Assess`/`Summary`/`JSON`, `writeReport`, verify summary, readme, `pages.Generate` (every caller), desktop result | MA-326, MA-327, MA-328 |
| P11 | `CloseGap`, `CloseUnobserved` (scope-covered), attempted split, `Runs` | MA-311, MA-323 (incl. the rolling `-since` case) |
| P11 `-since` rule (§4.8.2 step 1) | no gap for a not-requested item; existing gap retained | MA-323 (new) [FC32] |
| P12 | §4.5 root resolution, lexical link judgment, lstat descent rule, containment | MA-322 (Unix; Windows per P3 row) |
| P13 | `ObserveGap` lifetime caps and overflow class; `Load` cap; structural breakers | MA-323, MA-316 |
| P14 | no locator fold; no version bump | MA-329 (named exceptions excluded) |
| P15 | KEEP guard (§4.7.2) | MA-318 (Full-mode PST and Graph twins) |
| footer rule (§4.8.3) | footer only when `failures == 0 && !partial` | MA-325 (new) [FC32] |

## 5. Crash ordering and failure domains

The commit order is unchanged: history append+fsync → index flush → manifest save, the trailing
anchor (`commitInOrder`, `app/run.go:357-373`). This design adds no new file. The ledger travels
inside the manifest's atomic save.

| Write | Killed before / at it | Residual state | Fails toward | Operator sees |
|---|---|---|---|---|
| Partly-read or `unparsed` record export (`.eml` → zip → html → `Manifest.Add` → index) | at any step | the same as any export (R5): orphan temp (swept), orphan zip (swept), an `.html` or `.eml` with no record (overwritten by the same stem when the next run re-exports), or a record not yet saved (re-exported) | redo, never skip | nothing new; `verify` may list an orphan `.eml` as `unexpected` until the item is re-exported (the existing behaviour) |
| KEEP guard (P15) [FC5] | — | nothing is written; only `MergeFields`/`TouchSeen` and an info gap, which travel in the manifest save | — | an info `refresh-failed` row |
| `ObserveGap` (in memory) | before the next save | the entry is lost (local faults now arrive during the walk and reach checkpoints [FC29]) | re-observed next run, because the item still has no record. `Runs` restarts at 1 | stuck escalation delayed by up to one run per crash |
| Gap close on success | — | the record and the gap removal reach disk in the **same** atomic manifest file | never "gap closed but record missing", and never the reverse | — |
| `CloseUnobserved` | before the end-of-walk save | gap retained | over-reports (loud) | a stale row until the next completed walk |
| `TouchSeen` on a Graph fault, and `SweepGone` | the sweep runs once at walk end, after every `TouchSeen` of the walk; both persist in the same commit | before the commit: neither persists | the next run recomputes; no false gone (P7). After a history fsync without the manifest save: the existing duplicate-gone-event case (idempotent fold, MA-205) | — |
| Sweep skipped or narrowed (§4.8.3) [FC4] | run-local; nothing persisted | — | gone-detection is deferred, never wrong (except the L10-3 residual, P7) | `capture_gaps_stuck` names a persistent no-mid suspension |
| Metadata-only supersede [FC6, FC7] | before the save | the partial record still carries its labels | the next run re-applies it (the same locator matches) | the report lists the item for one more run. **No file is rewritten**, so there is no fixity mismatch and no rename→save window. Rev 1's page re-render, which had both, is removed |
| Legacy-placeholder re-class (§6) | before the save | not re-classed | re-classed next run | the old wording for one more run |
| `LastRun` `partial`, then the sidecar | kill before the finalize | record stays `running` | status RED `run_never_finished` (existing) | existing remedy |
| Report, banner, README | derived views written atomically | the old view | regenerated next run / `reindex` | stale view, never torn |

**Failure domains (lens 7).**

| Component | If lost | If compromised or corrupt | Fails | Operator sees |
|---|---|---|---|---|
| Capture-gap ledger | re-derived by the next walk; attempt counts reset | it can only add **false rows**, never suppress capture (P5). `Load` bounds, strips, caps the count, clamps times, maps an unknown class to terminal, and drops a bad or unknown token [FC14, FC16, FC17] | open (loud) | spurious rows. A row is cleared when its folder is next fully listed. A row of a token or folder that is no longer walked shows as "not attempted", never as stuck [FC14] |
| Graph breaker | — | mis-tuned too eager: the mailbox fails loudly (exit 1), which is today's behaviour, and the message prints the threshold that fired. Too lax: more gaps, still capped, and the 30-minute time bound still applies [FC10]. **[RR-6 RR6-C4]** That bound holds only while the control request fails; with a healthy one a run's cost is per item (P3's open condition). **[RR-6 RR6-C4; re-verification]** It never covers known poison, which pays its timer on every run whatever the control request returns | loud | `run_failed` RED or gap rows; `run_stuck` RED once a run outlasts its schedule period |
| MIME idle watchdog / size cap | — | bug that kills progressing downloads: `fetch-timeout` gaps (loud). Never fires: bounded by the overall cap. A cap set too low: `fetch-too-large` terminal gaps (loud), never truncation. **[RR-6 RR6-C1]** Before S3 each of these fails the mailbox through today's error path, with the typed stop named, never read as a cancel. **[RR-6 RR6-C3; re-verification]** The cap reaches capture only at a figure the confirmation bounds; where none does, capture keeps today's uncapped read (RR6-OR1) | loud | gap rows or a slow run |
| Recovery / metadata-only supersede | — | **[FC8]** a wrong match is prevented by the store-uid in every PST locator. A rebuilt store (`-outlook` scratch, `.ost`, or a store whose uid cannot be read) has no per-NID locators, only aggregates, so nothing can match across rebuilds. Within one store generation, NIDs and maildir unique names are unique. A supersede writes no file, so even a wrong match cannot damage a page | — | — |
| Source-read containment | — | bug that refuses a legitimate file: a terminal gap (loud), never a silent drop. **[FC1]** On Windows a reparse file that is not a link is read, never classed info | loud | gap row with path |
| Archive-root probe [FC12] | — | a false "unavailable" fails the run (exit 1, loud). A missed loss is still caught by the next 50-fault probe or by the checkpoint lock check | loud | §7.5 "archive location unavailable" |
| `ErrPartial` | — | a caller that has not been updated treats it as an error | closed (loud) | "failed" wording in that caller until updated |

## 6. Migration and coexistence

- **No manifest version bump.** `capture_gaps`, `Record.Locator` and `Record.RecoveredBy` are
  additive and `omitempty`. A v6 manifest written by this build loads in an older v6 binary,
  which ignores the new keys and **drops** `capture_gaps` the next time it saves. The
  alternative was rejected: bumping to 7 would make every older binary **refuse** the archive
  (`manifest.go:531-533`), so a rollout could not be rolled back without restoring a backup.
  Dropping the ledger loses diagnostics only (P5).
- **Existing archives, first run with this build.** Stores that used to abort now continue past
  their bad items, so the first run may export many **previously unreached** messages. That is
  the recovered loss: expected, and stated in the release note. Healthy archives behave as
  before (P14, MA-329), with P14's named exceptions [FC31(3)].
  - **Linked folders.** An archive that today captures through **directory links** changes
    behaviour (`walkSbd`/`isMaildir` follow them today, `mbox.go:99,108`). Rev 1's remedy,
    "archive the real location as its own `-input`", mints a new token
    (`PathSourceID`, `run.go:504`) and re-exports every message already archived under the old
    token into a second tree.
  - That migration is part of owner ruling **OR6**. The options are: keep following in-store
    links guarded by a visited (dev, inode) set; offer an opt-in on the **same** input that
    keeps its token; or never follow them.
  - **[ruling OR6]** The opt-in on the same input: such an archive's next run records a
    terminal `symlinked-folder-skipped` gap whose remedy names the opt-in. With the opt-in
    set, the input keeps its token and its links are followed under a visited (dev, inode)
    set, so no second tree is minted. S2b is no longer blocked.
- **Legacy collapsed stubs (F33's merged record).** The re-class lands in **S2a**, the slice
  whose changes leave the record orphaned [FC27]. On the first **run**:
  - A record qualifies if its identity component equals the constant legacy-stub identity (the
    `sha:` of the empty-field stub: the same for every local stub, which is why they collapsed)
    **and** its `Subject == "(unreadable message)"`.
  - It is re-classed from `Missing:["body"]` to `Terminal:["legacy-placeholder"]` (report kind
    `unreadable-legacy-placeholder`, class info), and logged once: "re-classed N legacy
    placeholder(s); this version records each unreadable message separately".
  - `status` never writes; the re-class happens only in a run.
  - **[FC27] Graph legacy stubs.** These have distinct identities and `Terminal:["body"]`. They
    are **adopted** on the first upgraded run (§4.8.2): matched by PhysID or Received, stamped,
    and re-classed. They are not left to `SweepGone`, which would otherwise mark each one gone
    once its identity changes.
- **Schedules.** Installed entries are unchanged.
  - **Once OR2 adopts it.** The new exit code `3` is what cron logs, what launchd records and
    what Task Scheduler shows as "Last Run Result 0x3".
    - Monitoring keyed on `!= 0` alerts on partial, which fails closed.
    - Monitoring keyed on `== 1` misses it, as the release note states.
  - **[ruling OR2]** OR2 does not adopt it: a partial run exits 1, so schedulers show
    "Last Run Result 0x1" and every monitor keyed on a non-zero exit alerts, as today.
  - **[FC23(2)] What to monitor.** Monitor `status -json` instead (C5). Key the alert on
    `last_run.gaps_new > 0`, or on posture, so that a known, unfixable gap does not drown a new
    loss.
- **Mixed versions [FC31(6)].**
  - *Older `status` reading a partial record.* Its switch has no case for it, so it shows no
    run-level WARN/RED. The older `status` also ignores `capture_gaps` entirely.
    - For a partial run whose gaps are **all** in the ledger (the F9 class), it shows
      **GREEN**.
    - If OR1 adopts the value `"partial"`, `last_run.status` also widens its documented values.
      A JSON monitor that tests `status == "failed"` then fails open.
    - The new binary's sidecar file and exit code still shout.
    - This is the real trade-off put to the owner as **OR1**. Revision 1 understated it.
    - **[ruling OR1]** OR1 takes `"failed"` plus `partial: true`, so an older `status` reads
      a partial run as a failed one and shows RED, failing closed; no documented value
      widens, and a monitor testing `status == "failed"` keeps alerting.
  - *Older binary capturing a new archive.* Its old abort behaviour returns on bad items
    (loud). It mints its own collapsed legacy stub, which is re-classed on return.
  - *Old and new binaries alternating.* Examples: a schedule pinned to the pre-upgrade exe, or
    the new desktop also writing.
    - The ledger is wiped on every old-binary save, so `Runs` never accumulates.
    - The old report labels `unreadable:*` records "empty-attachment" (`report.go:80-88`).
    - The old report deletes itself when only ledger rows existed.
    - Each new-binary run re-derives the ledger by walking.
  - *New binary on an old archive:* fine.
- **Rollback of the rollout [FC31(6)].** Swap the binary. There is no format refusal and no
  data conversion.
  - **What the old binary drops on its first save.** The old v6 `Record` struct has no
    catch-all field, so it drops:
    - `capture_gaps`, the ledger;
    - `Record.Locator` and `Record.RecoveredBy` on partly-read records;
    - `Record.ParserGen` and `RawDigest` on `unparsed` stubs.
  - **How the new binary recovers on return.**
    - The ledger is re-derived by the next walk.
    - A missing `Locator` is restored on re-examine from `m.Locator` [FC6(3)].
    - A stub with no `ParserGen` is treated as generation 0, so it is re-examined once.
  - **What is lost for good.** Attempt counts and gap history only. Content is never lost (P5).
- **Extraction workspace.** It does not call `app.Run` or `app.RunGraph`, and it never receives
  `ErrPartial` [FC20]. Its seam with this design is SG and `app.GapRemedy` (§4.8.1, §4.10).
- **Other in-process callers.** They receive `ErrPartial`, which is typed. A caller not yet
  updated treats it as failure (§4.9). Every in-repo caller is updated in S1b [FC19].

## 7. Operator walk

Friction is classified per `friction-review.md`. **Type I** is security- or source-inherent and
documented. **Type II** is a design choice, fixed here.

### 7.1 Home user, MailArchive Desktop (M365, device sign-in)

**[FC20]** The owner's simple copy flow (select source → choose destination) is the extraction
workspace. Its walk, including what its user sees when an item fails, is in
`docs/design-extraction-workspace.md` §4.13. It is not walked here, because that flow never
receives this design's outcome.

1. Click **Archive now**.
2. The card reads: "Finished with problems — 12,344 archived; 2 messages couldn't be downloaded
   (2 new this run). They are retried whenever you archive again (**Archive now**). If you are
   about to lose access to this mailbox, save a copy from Outlook on the web now. **View
   details**" [FC23(1)(2)(3)].
   - The card says "automatically" only when a schedule is installed.
3. Click **View details**. Status & health (WARN) lists each item [FC22].
   - Before the dashboard read guard lands, each line gives the mailbox, folder, date, reason
     and remedy.
   - After it lands, the line includes the subject too.
   - The CLI `status` always shows the subject.

**What it costs the user.**
- **2 clicks to know** that something failed and why.
- **0 actions** if the problem is transient and the account stays: the next run shows
  "Recovered: 2".
- **Account about to be removed.** The deadline remedy is on the card at first sight. The user
  saves a copy, or clicks the existing **Archive now** to retry at once (an incremental run
  retries only the gaps).

**If the problem persists.** The posture is **WARN `capture_gaps_stuck`**, listed first and
never RED [FC18]. The threshold is OR5's (§4.3 step 4, [ruling OR5]). The remedy reads: "Microsoft 365 will not export this
item; save a copy from Outlook on the web (*Download*, or forward it to yourself). The row stays
listed while the item remains in the mailbox". For a no-mid item it adds how to resume deletion
tracking [FC23(4)].
- That remedy is **Type I**: the provider will not serve the item.
- Previously the whole mailbox was blocked behind an opaque id. That blockage (**Type II**, F9)
  is removed.

### 7.2 Home user, CLI, an old damaged `.pst` (any OS)

1. `mailarchive -input old.pst -out X` → exit 1 with the partial summary and the `partial`
   flag (**[ruling OR2]**: X1 is not amended, so no exit 3).
   The summary's "Not captured" block names three things, plus the report path:
   - 2 unreadable messages in `Inbox`;
   - 1 partly-read message;
   - 1 unreadable folder `Projects/2009`.
2. Open the report: one row per item or folder, in plain words, each with the `.pst` path.
3. Windows only: close Outlook and run **scanpst.exe on the file in place**. It keeps a `.bak`.
4. Re-run the same command → exit 0.
   - The recovered messages are archived.
   - Their rows close as "recovered" [FC7].
   - The partly-read message's page is re-captured whole [FC6].

   Two other outcomes are possible:
   - If the repair renumbered a message, its old row closes as "no longer in the source" and
     the message is archived as new (P6).
   - If scanpst discarded a message, its row closes the same way.

That is **4 steps**. Off Windows, scanpst is unavailable: **Type I**. The remedy names "a Windows
PC with Outlook", or accepting the listed losses. **Type II fixed:** the remedy text forbids
archiving a repaired copy from a *different path*, which would start a duplicate tree because
the token is seeded from the input path.

### 7.3 Admin, scheduled app-only Graph run, 40 mailboxes

1. The scheduler shows `0x1` for last night (**[ruling OR2]**: a partial run exits 1).
2. `mailarchive status -out X -json` returns:
   - posture WARN;
   - `reason_codes:["capture_gaps_fillable"]`;
   - `capture_gaps{fillable:7,…,items:[…]}`;
   - `last_run.gaps_new:7` [FC23(2)].
3. Filter the report by `kind`. There are seven `fetch-failed` rows, each with a subject and
   date, and each with its **mailbox address** in the `path` column. They fall across 3
   mailboxes [FC22].
4. Usually nothing to do. They heal the next night (exit 0, "Recovered: 7").

That is **3 reads, 0 actions**.
- **Monitoring.** Alert on `last_run.gaps_new > 0` or on posture, not on the bare exit code, so
  a known, unfixable item does not mask a new loss [FC23(2)].
- **Stuck items.** Persistent gaps become WARN `capture_gaps_stuck`, with the per-reason
  remedy [FC18].
- **Network outage.** It trips the breaker (§4.8.5): exit 1, `run_failed` RED, "Microsoft 365
  or the network appears to be unavailable". There are no more than the counted gaps per
  mailbox, and two consecutive tripped mailboxes stop the run [FC10]. **[RR-6 RR6-C4]** A fault
  in `$value` alone, while listings still answer, is not an outage to the breaker. The control
  request succeeds, so nothing trips and each item pays its timer (P3's open condition). The
  night runs long, and `status` shows `run_stuck` RED if the run outlasts the schedule period.
  **[RR-6 RR6-C4; re-verification]** Known-poison items pay theirs every night, whatever the
  control request returns.
- **Mailbox removed from the job.** Its gaps show as "never captured … not in recent runs".
  They stay listed, and they never escalate [FC14].

### 7.4 Inheritor opening the archive cold (no tool)

1. Open **README.txt**. It explains `attachments-report.tsv`, "(unreadable message)" and
   partly-read pages, and **what fillable, terminal and info mean** [FC23(5)].
   - It also says plainly that if no one runs the tool anymore, every fillable or terminal row
     is missing from the archive.
2. Open **index.html**. The banner reads "Some items from the original mail source may not be
   in this archive — see attachments-report.tsv".
3. Open the TSV in a spreadsheet. Every missing message or folder is listed with its source,
   folder, date, subject and the reason in plain words.
4. Optionally open a folder page.
   - Partly-read and unparsed messages appear in place, each explaining what could not be read.
   - Messages with nothing readable have no page; their only record is the report row [FC7].

That is **3 steps, no tool, no secrets**.
- **Retired source.** For a source that is no longer archived, such as a departed mailbox, the
  rows are permanent. The tool can never learn that such an item has left the source. This is
  **Type I** [FC14].

### 7.5 Run-level failure messages (X2: what / why / remediation; exit 1)

| Case | Message |
|---|---|
| Graph breaker [FC10] | `mailbox ann@contoso.com: stopped after <the threshold that fired: 25 downloads in a row / 27 of the last 50 / 30 minutes of failed downloads> failed and a control request also failed (last: graph 503 Service Unavailable) — Microsoft 365 or the network appears to be unavailable. Messages archived so far are kept and nothing was marked deleted. Re-run when the service is reachable; ` + "`mailarchive status -out X`" + ` shows what is missing.` |
| Run-level breaker [FC10] | `stopped the run after two mailboxes in a row could not reach Microsoft 365 (…) — the service or the network appears to be unavailable; re-run when it is reachable.` |
| Archive location unavailable [FC12] | `the archive location E:\archive is unavailable (<errno text>) — reconnect the drive or share and re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.` |
| Token refresh failed [FC11] | today's auth wording plus the existing `auth_remedy` note (MA-107): sign in again, or renew the app secret |
| Disk full | `the archive drive is full (writing E:\archive\…): free space on E: or choose another archive location, then re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.` |
| Read-only archive | `the archive location E:\archive is read-only (writing …): make it writable or choose another location, then re-run.` |
| Local write breaker | `stopped reading old.pst after 50 writes in a row failed (last: …) — check that the archive drive is connected and writable, then re-run.` |
| `.pst` open failure | existing text + the §4.4 `.pst` remedy (in-place repair, same path) |

401/403 keep today's wording plus the existing `auth_remedy` note (MA-107).

## 8. Security

**Trust boundaries and untrusted inputs.**
- Every byte of a local store: PST structures, maildir trees (including symlinks and special
  files), mbox streams.
- Every Graph response field: listing subject and dates, error bodies.
- Filesystem error text.

All of it is now persisted (ledger, record notice) or rendered (summary, status, report, dashboard,
partly-read and unparsed pages).

**New surfaces and their controls.**
1. **Ledger strings**: control-stripped and bounded at write **and** at `Load`, because the
   ledger is untrusted on read (the MA-157 precedent).
2. **Report**: through `tsv()`.
3. **Terminal output**: through `util.StripControl`.
4. **Record notice**: HTML-escaped, inside the existing archive CSP (R19), in the tool's
   namespace.
5. **Banner**: a count only.
6. **Dashboard**: escaped by `html/template`. **[FC22]** Escaping does not settle who can read
   the page. The dashboard's GET pages can be read by a co-user, or by a DNS-rebinding page,
   until the dashboard session/read guard lands (F2/F3, E0). Until then they show gap lines
   **without subjects**. The dependency is stated here, and subjects stay in the CLI and the
   report.
7. **Source-read containment (R25).** It covers every local open [FC2]. Links are judged
   lexically before anything is resolved [FC3], and Windows reparse points are classified
   [FC1].
8. **Raw-stub text**: bounded to the headers plus 64 KiB for the page and the index [FC25].

There is no new network call; Graph stays GET-only (R9, R17). There is no new file, and the
ledger inherits the manifest's mode. There is no new listener.

**Attacker personas.**

| Persona | Goal | Outcome under this design |
|---|---|---|
| Hostile sender (one email via Graph or mbox) | stop the archiving of *other* mail (the F9 class) | defeated by P1. A poison message costs only itself. **Item-class** poison (500s, 4xx) never trips the breaker. **[FC10] Conditional:** poison that stalls, or 504s at the gateway, is spared by two rules (§4.8.5). A known-poison item from an earlier run does not count, and a trip needs a failing control request. This claim holds once MA-313's three new cases are green. A parser-panicking message becomes a raw-preserving stub, its text bounded [FC25] |
| Hostile sender | flood the ledger, inject terminal escapes or formulas, or bloat the index with a huge parser-panicking message | flood bounded over the archive's life (P13, [FC16]). Escapes stripped. Raw-stub page and index text bounded to the headers plus 64 KiB [FC25]. TSV formula injection is pre-existing and unchanged (N9) |
| Hostile store supplier (a PST or maildir handed over: intake, inheritance) | exfiltrate local files into the archive via links (F29c), in **any** layout: maildir, Thunderbird mbox or Evolution | refused and recorded. Links are judged lexically by `Readlink` before anything is resolved, so an out-of-store or UNC target is never touched [FC3]. Every local open, discovery included, uses the one checked helper [FC2]. MA-322 asserts the content is ABSENT from pages and index, in the maildir and mbox layouts |
| Hostile store supplier (Windows) | follow a junction out of the store, or hide mail behind reparse points | a junction or mount point is a directory link: recorded, not followed. A cloud or HSM placeholder is read, never silently dropped [FC1] |
| Hostile store supplier | hang the run (FIFO) or discovery, crash it (cycle), or loop it (`.sbd` → root) | FIFO never opened, at discovery too [FC2]; cycle and depth bounded; directory links not followed (P3, P12) |
| Hostile store supplier | mint unbounded records from a bogus or interleaved table (for example 99 bad rows then 1 good, repeated) | no record is minted for a row with nothing readable [FC7]. Duplicate and absent NIDs are structural and count toward the row breaker [FC9]. Ledger entries are capped over the archive's life (P13) |
| Misbehaving or impersonated Graph endpoint (TLS is assumed; MITM is out of the model) | hide mail by returning 404s or 500s; hang a walk with a looping or foreign `nextLink`; stream without end | every hidden item is a **visible** gap. 500 storms run through, capped and loud. Transport storms trip the rate/time breaker (exit 1). A cyclic, foreign-host or over-long `nextLink` chain is stopped in **every** paging loop, `Folders` included. The bearer token is never sent off the base host. A body over the size cap is a terminal gap [FC21] from S3; before S3 it fails the mailbox through today's error path [RR-6 RR6-C3]. **[RR-6 RR6-C3; re-verification]** Where no documented figure bounds the cap, capture carries none, as today, and its body is bounded by time only: an open gap, not accepted (owner ruling RR6-OR1). **[ruling J4]** In capture's loops the foreign-host stop comes with step SGc, behind the extraction review's XC25 gate; until then that case is an open gap, not accepted. **[RR-6 RR6-C6]** A cyclic or over-deep `childFolders` hierarchy is stopped by the visited folder-id set and the depth cap. One that invents fresh folders at every level is bounded in depth only: an open condition, not accepted. **[RR-6 RR6-C6; re-verification]** It is stated at P3 and encoded by MA-541's skip encoder citing R10 (owner ruling RR6-OR2) |
| Archive-writer (can write `-out`) | plant ledger entries or record fields to suppress capture, raise a permanent alarm, or redirect a write | suppression is impossible: the ledger is inert (P5). At most false rows. `Load` drops rows whose token is not in `Stores`, clamps future times and caps the count. A row whose folder is never walked shows as "not attempted", never stuck [FC14, FC16]. Supersede writes **no file**, so a planted `loc`/`path` cannot redirect a write [FC6, FC7]. `Load` drops a stray `Locator` or `RecoveredBy` |
| Integrity/concurrency: a live mail client renaming or deleting files mid-read | lose mail silently | re-list once; otherwise a self-closing `vanished-during-read` gap. The lock still excludes concurrent archive writers |
| Same-user local process racing a symlink swap between lstat/Readlink and open | read outside the store | **out of scope** (it already holds the user's rights). The final open is `O_NOFOLLOW` + fstat-regular on Unix, so only an intermediate-component swap remains. That swap is closed where the recommended `os.Root` open path is used [FC3] |
| **Co-user who can write the store** (a shared or group-writable maildir, a store on a shared drive) [FC3] | plant a hard link to a file the archiving user can read, or swap an intermediate path component, so the victim's run archives it | **out of scope, stated:** R25 holds for a store **not writable by another local user** (P12). A hard link inside the root is a regular file. Mitigations are `os.Root` and, optionally, refusing a multi-link file owned by another user. The archive's `search.db` is world-readable today (C2-1), which the local-confidentiality design owns |
| Co-user or DNS-rebinding page reading the dashboard | read the subjects of failing items | subjects stay off the dashboard's GET pages until the read guard (F3/E0) lands [FC22] |

The design touches untrusted-input parsing, symlink containment and the integrity of the go-back
sweep. It **owes the adversarial pass** for S2a, S2b, SG and S3 (§10).

## 9. Test plan (rows MA-310..MA-329 and MA-501..MA-504, allocated ids [ruling OR8 = J1]; MA-540 and MA-541, reserved for RR-6 until recorded [RR-6 re-verification])

All rows are tier **U** except MA-315 and MA-329, which walk the real fixtures and are tier
**S**, and MA-503 and MA-504, which are tier **L**. Each row is a covers-mapped spec.

**[RR-6] New rows.** MA-540 (RR6-C5, with RR6-C1's and RR6-C3's shipped-path cases) and MA-541
(RR6-C6) land with SG. They take their ids from the block MA-540..MA-544 given to the RR-6
run. They are rows of their own, not cases of MA-314, because the covers-map counts a row as
covered once any test cites it: a primitive MA-314 test would otherwise stand in for a missing
shipped-path test (FC33's reasoning).

**[RR-6 re-verification] Ids and covers at SG time.**
- The rulings log's §8.4, the allocation of record under J1, does not yet record the block
  MA-540..MA-544. J1 lets a design cite only allocated ids, so until the log's owner records
  that block the two ids are reserved, not allocated, and SG's catalog-first PR waits for the
  record.
- The covers-map refuses a marker that cites an id the catalog does not yet hold
  (`assureblock/covers_map_test.go:59-61`, `139-142`). S46 lands with S1a, after SG, so the
  SG-time markers of MA-314 and MA-540 cite their other ids, and S1a adds S46 to them. Their
  Covers cells read "S46 (from S1a)".

**[FC33] Split rows.** Rev 1's MA-320 and MA-324 each had a local half and a Graph half, and the
covers-map counts a row as covered once any test cites it. They are therefore split into a
local row and a Graph row: MA-320 and MA-501, MA-324 and MA-502 [ruling J1] (revision 2
called them MA-320a/b and MA-324a/b). Each half lands with its own slice, so a slipped slice
cannot leave a row green with half of it untested.

**Rows that span slices [ruling, `docs/owner-rulings-2026-10-09.md` §3].** MA-318 (S2a,
S2b), MA-323 (S1a, S2a), MA-325 and MA-326 (S1a or S1b, and S5), and MA-314 (SG; its
`fetch-timeout`, `fetch-too-large` and `folder-list-failed` gap cases need the S1a/S3
ledger and land with S3; its capture foreign-host case lands with SGc). Each lands with its
earlier slice and carries a skip encoder for its later cases, citing the row, until the
later slice lands (FC33's reasoning: the covers-map would otherwise read an unbuilt half as
covered).

**[FC32] Cases.** The new cases below are the asserting cases named in §4.11.

**Pending L-tier rows.** Two cases need hardware this repo's CI does not have. Each is its
own row [ruling J1], because a row has one tier:
- **MA-503:** a real OneDrive placeholder under a maildir, on a Windows VM with
  Files-On-Demand [FC1] (MA-322's L case; slice S2b);
- **MA-504:** the `-outlook` export-skip mapping against a real Windows Outlook install
  [FC28] (MA-320's L case; slice S2a).

Both are marked pending, with the lab story stated. Fault injection uses a fake Graph server, byte-patched
copies of `testdata/support.pst` (the F23/F27 PoC technique), planted files, chmod and test
hooks. Core logic is never mocked. "Twin" names the healthy-fixture positive twin that must pass
first.

| ID | Tier | Asserts | Covers | Prove-fail (mutate → red) |
|---|---|---|---|---|
| MA-310 | U | Graph: Inbox `[A1,A2,POISON,A3,A4]`, Sent `[B1,B2]`, POISON `$value` → 500 `ErrorMimeContentConversionFailed`. A1, A2, A3, A4, B1 and B2 are all archived; `RunGraph` returns `ErrPartial`; there is **no** manifest record or `KeysForIdentity` hit for POISON; one `fetch-failed` fillable gap carries POISON's subject and received date. Twin: the same mailbox without POISON → nil, no gap | R1, R10, R17, S46 | restore `return fmt.Errorf("fetch message …")` → A3/A4/B* missing |
| MA-311 | U | Ledger inert on Graph: run 2 with POISON healed downloads it once; the gap closes; `Recovered=1`; exit-path nil. A record seeded for POISON as Terminal (the H3 anti-pattern, constructed in the test) is shown never re-fetched, documenting why gaps are not records | R1, R17, R2, S46 | make the fast path also skip ledger locators, or `Add` a Terminal record on failure → POISON never fetched |
| MA-312 | U | Sweep safety (H2, [FC4]), six cases. (a) Full mode: the re-fetch of archived A2 fails → no `{k,gone}`, `Present` true, `LastSeen==runAt`. (b) Immutable-id comparable path: same. (c) A no-mid fetch failure in Inbox, with no PhysID match → the token's PhysID-less `sha:` records are not swept this run (a genuinely deleted no-mid message is **not** marked gone), and they are swept on the next clean run; a known-identity failure in Sent still lets a deleted Sent message be swept. (d) A message moved Inbox → Projects, then the Projects listing fails → no `{k,gone}` (the mailbox is not swept). (e) A moved no-mid message whose fetch fails, with immutable ids confirmed → stamped via PhysID, not gone. (f) Full mode, refs skipped because of the unwritable set → not gone. (g) [ruling OR10] A pre-v6, PhysID-less `mid:` record whose listing and parsed Message-IDs differ, and whose fetch fails in a walked folder → no `{k,gone}`; its sweep is deferred. Twin: a clean walk sweeps a deleted message (MA-207 unchanged) | R21, R1, S37, S46 | drop the `TouchSeen` loop or the PhysID stamp, or always `walked.Mark` → phantom gone in (a)–(f) |
| MA-313 | U | Abort semantics and breaker. Parent ctx cancelled mid-download → no gap for that item, `context.Canceled`, `LastRun` cancelled. `$value` 401 → mailbox failed, exit-path error, ≤1 gap; **403** the same, per OR7 ([ruling OR7]); the token endpoint starting to return 401 `invalid_client` mid-walk → mailbox-fatal auth with `auth_remedy`, **no** gap [FC11]. 25 consecutive 503s plus a failing control request → breaker error naming the threshold that fired and the last status, no sweep, gaps ≤ 25; 24 then a success → no trip (twin). 25 consecutive 500 item-class failures → **no** trip. 404 → `vanished-at-fetch`, fetched once, not counted. **[FC10]:** 1 success per 10 attempts → trips on the rate bound; 25 known-poison 504s that already have gaps from run 1 → run 2 walks past them; 25 consecutive per-item timeouts while the control request succeeds → no trip; two consecutive tripped mailboxes → the run stops; a `Retry-After: 3600` → the sleep is clamped (≤ 60 s scaled) | R5, R12, R18, S46 | classify `context.Canceled` as an item gap → walk continues; count 500 as transport → trips; drop the control probe → the timeout case trips; classify `RetrieveError` as transport → 25 gaps |
| MA-314 | U | MIME and listing bounds (scaled: idle 200 ms, cap 5 s, size cap 1 MiB). A server trickling 1 byte/50 ms for 1.5 s (beyond a 1 s wall clock) completes. A server stalling >200 ms mid-body → `fetch-timeout` gap, and the walk ctx is still live (the next item is fetched). A server that keeps streaming past the **overall cap** → `fetch-timeout` [FC32]. A body over the size cap → `fetch-too-large`, terminal, nothing written [FC21]. **[RR-6 RR6-C3]** A body exactly at the scaled cap completes; one byte more is refused with nothing returned; a response whose `Content-Length` exceeds the cap is refused before its body is read. A repeating `nextLink` ends the folder as `folder-list-failed`. A repeating `nextLink` in **Folders** → the mailbox fails, the walk terminates. A `nextLink` to a foreign host → refused, no bearer token sent [FC21]. **[RR-6 RR6-C2]** These primitive cases pass the scaled overall cap explicitly; capture's own overall cap until S3 is `Config.MIMETimeout`, which MA-98 asserts unchanged. The shipped-path cases are MA-540 and the folder-hierarchy cases MA-541 [RR-6]. **Slices [ruling J4]:** the row lands with SG (the trickle, stall, overall-cap and size-cap primitive cases, the size-cap boundary and `Content-Length` cases [RR-6 RR6-C3], and the repeat stop in `Folders`); its `fetch-timeout`, `fetch-too-large` and `folder-list-failed` gap cases land with S3, and its capture foreign-host case with SGc; until then each is a skip encoder citing MA-314. **[RR-6 RR6-C3; re-verification]** While capture passes no size cap (owner ruling RR6-OR1's safe interim), the `fetch-too-large` gap case stays a skip encoder citing MA-314 | R10, R17, S46 (from S1a) | restore a wall-clock deadline shorter than the trickle → trickle fails; remove the `nextLink` set or the host pin → test timeout / token sent; refuse at the cap itself → the at-cap case fails [RR-6 RR6-C3] |
| MA-540 | U | [RR-6 RR6-C5, with RR6-C1 and RR6-C3] SG on the shipped capture path, through `RunGraph` on the fake Graph server. Two mailboxes; a healthy first run archives every message; then the case run (scaled: idle 200 ms, `$value` header window 600 ms, JSON request timeout 100 ms [RR-6 RR6-C5; re-verification], page cap 5, size cap 1 MiB, capture's overall cap 2 s). In mailbox 1 the case run meets one stop: (1) a repeating `nextLink` in `Messages` after its first page; (2) fresh `nextLink`s past the page cap in `Messages`, and, as a case of its own, in `Folders` [RR-6 RR6-C5; re-verification]; (3) a new item's `$value` that stalls mid-body past the idle window, or sends no headers within the header window, or trickles past the overall cap; (4) a new item's `$value` over the size cap. Each case expects: a typed error naming the stop, and for (3) and (4) the message and the limit, matching neither `context.Canceled` nor `context.DeadlineExceeded`; **zero** `{k,gone}` events and `Present` unchanged for every mailbox-1 record (none was deleted at the source); mailbox 2 walked; last-run `failed` (with `partial: true` from S1b), never `cancelled`; the attention sidecar written; `RunGraph`'s error does not match `context.Canceled`, so `graph` exits 1 (`main.go:1140-1145`). Before S3 the stop fails mailbox 1, which is today's error path; from S3 on a `Messages` or `$value` stop is MA-314's gap case, the typed error is the gap's detail, the mailbox continues, and every other assertion above still holds [RR-6 RR6-C5; re-verification]. A `Folders` stop fails the mailbox before and after S3. (5) Both mailboxes, several folders each, whose total page count exceeds the page cap while every loop stays under it → the run completes with nil (the cap counts per loop). (6) A transport that returns `context.Canceled` for mailbox 1's `$value` while the parent context is live (fault injection, the second guard) → before S3 mailbox 1 fails; from S3 that item is a `fetch-transport` gap (§4.8.1 rule 4) and mailbox 1 continues [RR-6 RR6-C5; re-verification]; in both, mailbox 2 walked, last-run `failed` (with `partial: true` from S1b), never `cancelled`, and `RunGraph`'s error does not match `context.Canceled`, so `graph` exits 1. Twins: headers that arrive after the scaled JSON request timeout but inside the header window → the item is archived; **[RR-6 RR6-C1; re-verification]** a 503 whose `Retry-After` sleep is longer than the header window but ends inside capture's overall cap, then a prompt 200 → the item is archived (the header timer times each attempt and does not run in the sleep); the parent context cancelled during mailbox 1's download → last-run `cancelled`, `graph` exits 0 (X1's handled interrupt), mailbox 2 not walked. **[RR-6 RR6-C5; re-verification] The exit code.** `graph` takes no base-URL setting and cannot reach the fake server, so its exit mapping (`main.go:1140-1145`) is factored so a test can call it, and a test in `cmd/mailarchive` runs `RunGraph` on the same fake server and passes its error through that mapping: exit 1 for every stop case and case (6), exit 0 for the parent-cancel twin. **[RR-6 RR6-C3; re-verification]** While capture passes no size cap (owner ruling RR6-OR1's safe interim), case (4) is a skip encoder citing MA-540 | R10, R17, R18, R21, S37, S46 (from S1a) | end the listing quietly on a stop → phantom `{k,gone}` events appear; return the raw transport error from `MIMETo`'s idle stop → the typed-error assertion fails; drop `RunGraph`'s `ctx.Err()` test → case (6) records `cancelled` and exits 0; keep the page count on the `Client` → case (5) fails; drop the page cap from `Folders` → its page-cap case runs into the time guard; run the header timer across `get`'s sleeps → the `Retry-After` twin fails [RR-6 RR6-C1, RR6-C5; re-verification] |
| MA-541 | U | [RR-6 RR6-C6] Graph folder hierarchy, the Graph twin of MA-317, on the fake Graph server. A `childFolders` response that lists an ancestor (A → B → A) → `Folders` returns under a 1 s guard with A and B each listed once, and no second `childFolders` request is made for A. A chain of fresh folder ids 301 levels deep (one past the cap) → the mailbox fails with the typed depth error naming the cap, under a time guard. A folder listed under two parents, as when it moves during the walk → listed once, and the walk completes. Twins: a healthy three-level tree → every folder listed once, nil; **[RR-6 RR6-C6; re-verification]** a chain exactly at the cap (300 levels, counted as SG's confirmation counts them) → completes, every folder listed once, nil. **[RR-6 RR6-C6; re-verification] Breadth, an open gap (owner ruling RR6-OR2):** a fake server that invents two fresh child folders at every level → `Folders` returns under a time guard; a skip encoder citing R10 until a total-folder cap lands. If SG's confirmation gives no depth figure, the over-depth case is a skip encoder citing MA-541 until one is bounded (P3); a higher figure moves both chains with it | R10, R17 | remove the visited set → the guard times out; remove the depth cap → the over-depth chain does not fail; fail on a revisit → the moved-folder case fails; count one level too many → the at-cap twin fails [RR-6 RR6-C6; re-verification] |
| MA-315 | S | PST per-row isolation (H1, [FC7]): copies of `support.pst` with the message block at #0, #3 or #11 XOR-corrupted each yield **16** real messages plus **one** `pst-message-unreadable` ledger gap whose locator is `pst-nid:<store-uid>/<nid>` (or one `pst-messages-unreadable` aggregate if the store-uid cannot be read) and **no** stub record or page; the walk terminates under a 10 s guard. In the 1656-mutation sweep of block #0 (the F23 PoC), every mutation yields 17 items: 17 real, or 16 real + 1 gap. The F23 emulation gave 1563 at 17 and 93 at 16. No mutation aborts or hangs the walk. A differential check: on the uncorrupted fixture the copied loops yield exactly go-pst's own folder and message sets [FC30]. Twin: the uncorrupted fixture yields 17, no gap | R10, R1, S46 | revert to the `MessageIterator` loop → yields 0/3/11; wrap `it.Next()` in `continue` → guard timeout; mint a stub record again → record count 17 |
| MA-316 | U | Folder isolation: a corrupted PST sub-folder table row costs that sub-folder only (`folder-unreadable`), siblings walk; a `GetMessageTableContext` failure on one folder records `folder-unreadable` and its sub-folders still walk; 100 consecutive **structural** bad rows → one `folder-partially-read` with `Skipped`, no further entries. **[FC9]:** an interleaved table (99 bad rows, 1 duplicate or valid NID, repeated) → bounded ledger entries, no records; a 150-row run of **real** damaged nodes mid-folder → 150 gaps and every later row archived; a corrupt hierarchy table with 100+ bad rows → one `folder-partially-read`. **[FC32]:** a Graph listing page error in one folder → `folder-list-failed`, the next folder walks, no sweep; a maildir non-root `ReadDir` failure → `folder-unreadable`, siblings walk; an mbox open failure → `folder-unreadable`; an mbox mid-stream error → `folder-partially-read` with the count read; the Evolution walkers' folder errors → per-folder gaps | R10, R6, R1, S46 | use `GetSubFolders()` again → siblings lost; count case 3 in the breaker → rows after the damaged run lost; drop the duplicate-NID set → unbounded entries |
| MA-317 | U | PST cycle/depth (F27): the PoC-patched fixture (child NID → root NID) returns well under 1 s with a terminal `folder-cycle` gap and every distinct folder archived once; a synthetic 301-deep chain → `folder-too-deep`. The prove-fail runs the walk in a **child test process** (`os/exec` of the test binary with `-test.run`), because a stack overflow is fatal | R10, S47 | remove the visited set → the child dies with `stack overflow` (the parent asserts failure) |
| MA-318 | U | Placeholder and degraded-capture fidelity (F33, [FC5], [FC6], [FC7], [FC25]). Five unreadable PST nodes in one folder → **5 ledger gaps**, 5 report rows, **0** records or pages; an RTF panic keeps subject/sender/dates/attachments with `unreadable:body` and the record carries `Locator`; an attachment-table error keeps the body with `unreadable:attachments`; a MIME-panicking mbox/maildir/Graph message keeps `Raw` (`.eml` under `-raw`), shows the headers plus the first 64 KiB on the page and in the index, `unparsed` terminal with `ParserGen`/`RawDigest`; an `Export` panic increments `Stats.ExportPanics` and records `export-panic`, never log-only. **KEEP twins (P15):** a `-mode full` run over a PST copy whose RTF now panics, or whose attachment table now fails, on a message archived complete → html and zip bytes equal their recorded Fixity, record unchanged, one info `refresh-failed`; a Graph Full-mode run where an archived message's parse now panics → the record, html and zip are unchanged and no PhysID adoption happens. **Healing:** both orders of an attachment-table failure (partial first, then healthy; healthy first, then failing) → one record, no `#fp` duplicate; a no-Message-ID partial record whose healthy re-read lands on a new key → partial record re-classed `superseded` with `RecoveredBy`, no file written. **[ruling J8 = ESC-1]** Two distinct MIME-panicking raw messages in one folder still share today's constant stub identity until the owner answers ESC-1: that collapse is a skip encoder citing R1 in this row's raw-stub case, removed when slice S2b-id lands | R1, R3, R7, R10, S46 | mint a stub record → 5 records; restore the bare stub → fields lost; restore `err=nil` in the recover → no gap; drop the KEEP guard → html overwritten / zip orphaned; drop the zero-attachment `#fp` comparison → duplicate |
| MA-319 | U | Recovery (P6, [FC6], [FC7], [FC8]): restore the corrupted block in the fixture copy at the **same path** → the next run captures the real message; its `pst-message-unreadable` gap closes as recovered; the report no longer lists it; **no file is written or deleted** by recovery. A repaired file at a different path → new token, the old gap unchanged (the documented duplicate). A repair that removes the item → the gap closes as "no longer in the source" on the next completed walk. A gap whose store-uid differs from the re-read store's → **not** recovered by a message at the same NID (no false recovery). An `-outlook`-style rebuilt store → one per-folder aggregate gap whose `Runs` counts across rebuilds. A partly-read record after an old-binary save stripped `loc` → `Locator` restored on re-examine. `verify` still attests after every case | R1, R6, R13, S46 | match across store-uids → false "recovered"; drop the restore-on-re-examine → `Locator` stays empty |
| MA-320 | U | Folder-unwritable and run-fatal, **local** (N1, [FC12]). mbox A, B, C with a regular file at `out/<tok>/B` → A and C archived, `folder-unwritable` (fillable, `Skipped=n` for this run), `ErrPartial`; remove the file → run 3 archives B, the gap closes, nil. Run-fatal twin pairs (`assure.Refused`, no further writes): an injected ENOSPC, **EDQUOT, EROFS** (errno injection) → exit-path error naming the drive and the remedy; a healthy disk → nil. The **Windows codes** 112/39/19/3/21/53/55/64/1167/1295 map run-fatal in a classifier table test. **`-out` removed mid-walk** → exit 1 "the archive location … is unavailable", no partial outcome, no folder-unwritable rows. A `SafeToken` refusal → exit 1 naming the token. 50 consecutive item write failures, with each folder's first `FolderError` counting → local breaker error. The `-outlook` export-skip mapping (`CreatePSTs` skipped folders → `outlook-export-skipped`) by unit test; the Windows+Outlook case is the L row MA-504, pending [FC28] [ruling J1] | R1, R6, R10, R5, R12, S46 | return `exportErr` as today → C missing; treat ENOSPC or a vanished root as an item → run continues writing / ends partial |
| MA-501 | U | Folder-unwritable, **Graph**: the same fixture through the fake Graph server → the unwritable folder's later refs are recorded under the gap without download; fast-path skips in it still stamp; in Full mode the unwritable-set refs count as faults, so no record in that folder is swept gone [FC4(d)] | R1, R6, R10, R21, S46 | ignore unwritable-set refs in the sweep → phantom gone |
| MA-321 | U | Maildir per-file (F29a, [FC7]): `Inbox/cur{a, b chmod 000, c}`, `Work/cur{d}` → a, c, d archived and b a `file-unreadable` fillable **ledger gap** with locator `maildir:Inbox/<unique>`, **no** record, remedy "fix the permissions" (skipped when running as root or on Windows); an EIO injection → the same gap with the disk remedy; a test hook renaming `b:2,` → `b:2,S` while exporting a → b read once after the re-list; a hook deleting b → `vanished-during-read`, closed on the next run; after `chmod 600` on b → b archived and its gap closes as recovered. Twin: no faults → no rows | R10, R15, R1, S46 | return the read error → c and Work lost |
| MA-322 | U | Containment (F29b/c/d, R25, [FC1], [FC2], [FC3]). **Maildir:** a FIFO in `cur/` → skipped without open (walk completes <1 s), `not-a-message-file` info; a file link to `outside/secret` → `symlink-outside-store`, and the marker string is **ABSENT** from every page and from `search.db` (NoSideEffect); an absolute or `..`-escaping link target is refused **before** any resolution (asserted with a target path that would block if touched); an in-store file link → read once; `Inbox.sbd → root` and `Other/cur → outside` → not followed (n=1, not 33), `symlinked-folder-skipped`. **mbox layout:** a FIFO named `Inbox` at the store root → discovery (`IsMailStoreDir`) and the walk both complete under a 1 s guard; an `Inbox` symlink to an outside file that begins "From " → refused, marker ABSENT. **Windows classifier (OS-independent):** a table of synthetic `FileMode`s → special files refused as info; `ModeIrregular` with `Readlink` success → `symlinked-folder-skipped`; `ModeIrregular` with `Readlink` failure → read; nothing that might hold mail is classed info. **Windows, unprivileged:** a `mklink /J` junction for `cur` → not followed, recorded. A real OneDrive placeholder: the L row MA-503, pending (lab story: a Windows VM with Files-On-Demand) [ruling J1]. **Opt-in [ruling OR6]:** with the input's directory-link opt-in set, `Inbox.sbd → root` is followed once (the visited (dev, inode) set stops the loop, n=1), the input keeps its token, and the file link to `outside/secret` is still refused with the marker ABSENT; without it, the linked folder is a terminal `symlinked-folder-skipped` gap whose remedy names the opt-in. FIFO and symlink cases skipped on Windows, where they need privileges. Twin: the same store without links → every message, no rows | R25, R10, R4, S47 | `os.ReadFile` every entry again → secret archived / FIFO hang (guard timeout); class `ModeIrregular` as info → placeholder dropped |
| MA-323 | U | Ledger lifecycle: Save/Load round-trip; a v6 manifest without `capture_gaps` loads empty; re-observe in a **later** run increments `Runs` and keeps `FirstRun`; two observations in one run increment it once, and `Skipped` resets per run [FC15]; `ObserveGap` updates `Folder` [FC14]; a gap in a fully walked folder that was not re-observed closes; a gap in an unwalked folder is retained and shows as **not attempted**; a gap whose `Received` is before a rolling `-since` is retained and still reported [FC13]; the lifetime caps (1001st item, folder-scope and info caps) → overflow with an exact count and the most severe absorbed class [FC16]; `Load` strips and bounds a hostile `Detail`, drops a non-`SafeToken` entry and an entry whose token is not in `Stores`, clamps a future `LastRun`, keeps a known class with an unknown reason, maps an unknown class to terminal [FC17]; a legacy collapsed stub is re-classed to `legacy-placeholder` once, on a run only; a Graph legacy stub is adopted and **not** swept gone [FC27]; a stale `ParserGen` `unparsed` Graph stub is re-fetched exactly once [FC25] | R1, R5, S46 | remove the cap → >1000 rows; skip the `Load` strip → ESC survives; count calls → `Runs` 2 in one run |
| MA-324 | U | Inertness, **local** (PST, mbox, maildir): a manifest seeded with a gap whose locator matches a healthy item does not stop its capture; the gap closes as recovered | R1, R17, S46 | add a ledger lookup before export in any walker → item skipped |
| MA-502 | U | Inertness, **Graph**: the same through the fake server | R1, R17, S46 | add a ledger lookup to the fast path → item skipped |
| MA-325 | U | Outcome mapping ([ruling OR1, OR2]; S1a asserts the interim `failed`/exit 1 mapping). CLI export and `graph` exit 0 for complete, and 1 for partial (with the partial summary and the `partial` flag) and for run-failure, a run-level failure winning over partial across sources; Ctrl-C → 0. `LastRun.Status` is ok, or failed with `partial: true` on a partial run, or failed, with gap counts and `gaps_new`; `BACKUP-NEEDS-ATTENTION.txt` written for partial, leading with the new count, and removed only by ok. `mailarchive-gui -job` and `mailarchive-desktop -capture` return 1 on partial, with the flag; `-job` reclaims the `-outlook` scratch on partial. **The GUI wizard's** pure result → next-step mapping: partial → summary with Not-captured, rememberArchive, scratch reclaim, open, offerSchedule [FC19]. The history footer is written only when `failures == 0 && !partial` [FC32]. Refusal twin: missing `-out` still exits 1 naming it (MA-33 unchanged) | R18, R12, S48 | map `ErrPartial` to 0 → wrong exit; drop the `partial` flag → the partial line is missing ([ruling OR2]); treat partial as failure in the wizard mapping → no schedule offered |
| MA-326 | U | Status: fillable → WARN `capture_gaps_fillable`, "automatically" only with a schedule; stuck → **WARN** `capture_gaps_stuck` listed first, never RED [FC18], with the no-mid sweep sentence and its move-or-delete remedy when applicable; not-attempted → the note ([ruling OR3]) and never stuck [FC14]; terminal → WARN with the remedy keyed by source kind; partly-read → WARN, absent from `incomplete_content`; a last run with `partial: true` → the partial line, no `run_failed` ([ruling OR1, OR2]). `health.Summary` lists ≤20 gap lines with source, folder, subject, date and remedy; JSON has additive `capture_gaps` (incl. `items[]`, `not_attempted`) and `unreadable`, with `JSONVersion` 2 ([ruling OR1]) [FC22]. The desktop Status & health view shows the same reasons and gap lines **without subjects** until the dashboard read guard lands (X10) | R18, S48 | drop the stuck rule → no reason; escalate stuck to RED → posture assertion fails; render subjects on the GET page → assertion fails |
| MA-327 | U | Report and inheritor: one TSV row per gap (kind, class, folder, date, subject, detail with locator and run count, **source id in `path`**) in a stable order, control-free; a **ledger-only** fixture (no record issues) still writes the report [FC24]; README text present, class vocabulary included; the root `index.html` banner appears with a count, no untrusted text, and the CSP meta intact; after `reindex` and after `reindex -rebuild` the banner is still present [FC24]; `verify` prints the "Not in this archive" line and still exits 0 on an attested archive | R1, R7, R19, S48 | omit ledger rows from `writeReport`, or keep the early return → gap invisible; pass 0 from reindex → banner gone |
| MA-328 | U | Summary: the "Not captured" block (source, folder, reason, remedy; new count), capped at 20 lines with `+N more`; the deadline remedy on a first-seen fillable Graph gap; destination-side reasons name the archive location; the "Recovered" line; partial prints `Done.` and never `FAILED:`; `.pst` open failure appends the in-place-repair remedy (extends MA-282) | R18, R12, S48 | print `FAILED:` on partial → assertion fails |
| MA-329 | S | No regression (P14): on `support.pst` and the mbox, maildir, Evolution and fake-Graph fixtures, records, keys, identities, paths and per-message file bytes equal a pre-change golden manifest (timestamps masked; `README.txt` and the root `index.html` stamp excluded, P14's named exceptions); an incremental re-run exports 0; the ledger is empty | R2, R3, R5 | fold a locator into any message's hash → keys change |
| MA-503 | L | [ruling J1] (MA-322's L case) a real OneDrive Files-On-Demand placeholder under a maildir is read as the file it represents, never refused and never classed info, while a junction beside it is refused and recorded — **pending** (lab: a Windows VM with Files-On-Demand) | R25, S47 | — |
| MA-504 | L | [ruling J1] (MA-320's L case) the `-outlook` export-skip mapping on a real Windows classic-Outlook install: a folder whose `CopyTo` fails and a skipped account each become their ledger gap, and the run is partial — **pending** (lab: Windows with classic Outlook) | R1, R6, R10, S46 | — |

**Lab story.** The Graph error-code classification is proven only against the fake server. The
behaviour of real tenants (which items return `ErrorMimeContentConversionFailed`, and whether a
per-item 403 exists) is not reproduced in CI. It is labelled conditional (§12). It needs no row
in this range; an L row can be added if a tenant repro is wanted.

## 10. Build slices, order and gates

The order is sink before producer: the ledger and every surface land before any source starts
emitting faults. Each slice:
- is catalog-first;
- ships prove-fail → prove-pass;
- runs the covers-map unfiltered.

**[FC33] Rulings.** No slice hard-codes the answer to an open owner ruling. The "Blocked on"
column names what must be ruled before the slice may merge. **[ruling]** OR1–OR10 are ruled
(`docs/owner-rulings-2026-10-09.md` §3); the column now names only the escalations (ESC-1,
ESC-7) and the gates that remain.

**[FC34] Catalog edits.** No catalog edit lands before the operator allocates the R and S ids
(OR8; **[ruling OR8 = J1]** allocated: R25, S46–S48, MA-501..504). Catalog edits across the
five designs in flight are **serialised**, in the order of
`docs/owner-rulings-2026-10-09.md` §9.

| Slice | Content | Rows | Depends on | Blocked on | Gate owed after build |
|---|---|---|---|---|---|
| **S0** | This design → 10-lens pre-code review filed (`review-fault-isolation-predesign.md`, GO_WITH_CONDITIONS); conditions FC1–FC34 folded into rev 2 | — | — | — | **pre-code design review** (done) |
| **S1a** — sinks and surfaces | `state.CaptureGap` plus accessors (lifetime caps, attempted split, `Runs` per run), `Load` sanitisation (fail-closed classes, `Stores` check, clamps, count cap), `GapClass` (single owner of labels); `model.Locator`/`ReadGaps`/`ReadError` (unused by readers yet); exporter read-gap labels, KEEP guard (P15), healing (`#fp` zero-attachment comparison, probe, metadata-only supersede) and record notice; `printSummary` (source ids, new count, deadline remedy); `app.GapRemedy` (reason, source kind, errno class; exported for copyexport); `health` rules (stuck WARN, not-attempted note, ≤20 gap lines) and additive JSON keys; report rows (source id; written when the ledger is non-empty); `verify` line; README (class vocabulary); banner (every `pages.Generate` caller). **A partial run maps to today's `failed`** (exit 1, `run_failed` RED) — loud, fail-closed, no X1 change. The stuck threshold is OR5's [ruling OR5]; S46 and S48 land here [ruling OR8 = J1] | MA-323, MA-326, MA-327, MA-328, MA-329 (golden baseline first) | — | — (OR8 and OR5 ruled) | **friction review** of the surfaces (summary, status, sidecar) |
| **S1b** — the partial outcome | `ErrPartial` with exit 1 and the `partial` flag [ruling OR2], status `failed` + `partial: true` with `JSONVersion` 2 [ruling OR1], sidecar wording and "new this run" on every surface [ruling OR3], first-sighting vanished gaps partial [ruling OR4]; CLI, GUI wizard (FC19), `-job` (reclaim on partial), desktop exit mapping; R1's and R18's added sentences [ruling J7], no X1 change. Every in-repo `LastRun.Status` consumer is updated in this slice (`health.go:203-222,631-632,810-817`), so none ever sees an unknown value | MA-325 | S1a, g7/F53 | — (OR1–OR4 ruled) | **friction review** (exit code, sidecar); no UX-contract X1 amendment is owed [ruling OR2] |
| **S2a** — PST isolation and healing | store-uid locators and per-folder aggregates (FC8); row and sub-folder loops with every upstream gate kept, differential test, go-pst pin (FC30); structural breakers and duplicate/absent-NID checks (FC9); inward recovers; zero-content failures as ledger gaps, no stub records (FC7); visited and depth guards; `.pst` and `-outlook` remedies; fault callback (FC29); `runFile` classification, archive-root probe, run-fatal volume errnos, unwritable set, local breaker, fault-driven checkpoint (FC12); `-outlook` export-skip gaps (FC28); legacy-placeholder re-class (FC27); R10's added sentences [ruling J7]; N9's skip encoder citing R1 [ruling J7] | MA-315, MA-316, MA-317, MA-318, MA-319, MA-320, MA-323 (legacy case), MA-324, MA-504 (L, pending) | S1a | — (OR8 ruled) | **adversarial pass** (untrusted PST parsing, exporter write paths) |
| **S2b** — local containment | mbox/maildir/Evolution per-file and per-folder isolation; containment at **every** local open (FC2); lexical link judgment, recommended `os.Root` (FC3); Windows reparse classifier (FC1); `util.OpenRegularFile`; the per-input directory-link opt-in [ruling OR6]; raw-preserving stubs with bounded text and parser generation (FC25), expressed through g3's hook (FC26), **without the identity fold** [ruling J8]; R25 text, naming the opt-in; S47 | MA-318 (raw stubs, the collapse as a skip encoder citing R1), MA-321, MA-322, MA-503 (L, pending) | S1a, g3 S1 [ruling J5] | — (OR6 and OR8 ruled) | **adversarial pass** (R25 containment, Windows) |
| **S2b-id** — the F33 identity fold [ruling J8] | `stubIdentityTerms` folds a digest of the raw bytes, so distinct unparseable messages stop collapsing; g3's MA-330 golden regenerated for panicking inputs only, with a README note; the skip encoder in MA-318 removed | MA-318 (the fold case) | S2b, g3 S1 | **ESC-1** (the owner accepts the identity drift) | **adversarial** addendum |
| **SG** — shared Graph primitive [ruling OR9 = J4] | `StatusError{Code, GraphCode, Text}` (today's `Error()` text), `graph.Classify`, `MIMETo` (idle, overall and size caps; `$value`'s own header timeout ≥ the idle window, for capture too), nextLink guard: the repeat set and the page cap in every paging loop, and the host/path pin for the extraction design's loops; R29 and S52 land with this slice's catalog-first PR (MA-402 cites them). **[RR-6]** `MIMETo`'s own stops typed (`*graph.TimeoutError`, never a context error while the caller's context is live), with the `$value` header timer on its own base transport, and `RunGraph` deciding "cancelled" from its own `ctx.Err()` (RR6-C1); capture's overall cap kept at today's 10 minutes until S3 (RR6-C2); the size-cap figure confirmed against Microsoft's documentation before SG code, at or above Exchange Online's largest item, with the `Content-Length` refusal (RR6-C3); every guard stop typed, never a quiet end of listing (RR6-C5); `Folders`' visited folder-id set and depth cap, the 300 figure confirmed before SG code (RR6-C6). **[RR-6 re-verification]** The header timer per attempt, never across `get`'s sleeps; the size cap reaching capture only at a figure the confirmation bounds, with capture's uncapped read otherwise (RR6-OR1's safe interim), the documentation and date recorded in §4.8.6; the depth figure's fallback and an at-cap twin; the breadth case an open gap, skip-encoded (RR6-OR2); the `$value` client left for E1's `CheckRedirect`, never its `Client.Timeout`. **[RR-2b]** The size stop typed with its limit and declared length (RR2b-C8); the `$value` client built by SG itself, never through `oauth2.NewClient`'s copy of the shared client (RR2b-C3) | MA-314 (its SG cases; the rest are skip encoders until S3 and SGc); MA-540 and MA-541 [RR-6], with their skip encoders [RR-6 re-verification]; the extraction design's MA-402 | — | — (OR9 ruled); **RR-6** filed: GO_WITH_CONDITIONS, RR6-C1–RR6-C6 folded and re-verified; RR6-OR1 and RR6-OR2 block nothing (their safe interims apply); the catalog-first PR waits until the rulings log records MA-540..MA-544 (J1) [RR-6 re-verification] | **adversarial pass** (network input), shared with the extraction design's E1, which keeps its own short note |
| **SGc** — capture's nextLink pin [ruling J4] (formerly the extraction design's E1c) | the host/path pin with the extraction review's XC25 normalization in capture's `Folders` and `Messages`, a typed error naming the foreign host | MA-505 (the extraction design's MA-401b), MA-314's capture foreign-host case | SG | MA-443's nextLink-shape sub-case recorded (XC25's gate), unless the owner answers **ESC-7** first | short **adversarial** note (shipped capture path) |
| **S3** — Graph isolation | `classifyFetch` (token-refresh auth, FC11; 403 mailbox-fatal [ruling OR7]), per-item gaps with no record, immutable-id locators, `-since` rule, `refresh-failed`, stub listing-mid only without a same-token record, parser-generation re-fetch, Graph legacy-stub adoption (FC27), sweep rules (FC4) with OR10's stricter rule [ruling OR10], folder-list isolation, rate/time breaker with known-poison exemption, control probe and run-level breaker, `Retry-After` clamp (FC10), and in the same slice capture's overall cap raised from today's 10 minutes to `MIMEMaxDuration` (2 h) [RR-6 RR6-C2], footer condition; R17's added sentence [ruling J7] | MA-310, MA-311, MA-312, MA-313, MA-314 (its ledger cases), MA-501, MA-502 | S1a, SG | — (OR7 and OR8 ruled) | **adversarial pass** (network input, go-back sweep integrity) |
| **GF17** — F17 in the shared Graph layer [ruling workspace Q15] | retry Graph 500/502/504 and budget `Retry-After` so throttle waits no longer count against the 60 s request deadline; this lifts the clamp's interim role and the extraction design's R-v condition | rows allocated at its catalog-first PR | S3 | — | **adversarial** addendum (network input) |
| **S5** — in-process callers | desktop result card mapping (existing Archive now action named, schedule-aware wording, new count, items vs folders); Status & health gap lines without subjects until the dashboard read guard (F3/E0) lands; GUI headless notify text | MA-325/MA-326 desktop assertions | S1b, S2a, S2b, S3 | — | **friction review** of the whole feature: every §7 actor walked on the built tool |

rev 1's **S4** (supersede and legacy) is **dissolved** [FC7]:
- no stub records remain to re-render;
- the metadata-only supersede ships with the exporter healing in S1a;
- the legacy re-class moves to S2a, the slice whose changes leave the legacy record orphaned
  [FC27].

**Order.** S2a, S2b and S3 are independent of each other once their dependencies are met.

**Cross-design seams.**
- **The extraction workspace (owner's simple copy flow) [FC20, FC21].** It is a separate
  consumer, with its own engine and `CopyOutcome`, and it never receives `ErrPartial`.
  - The shared surfaces are SG and `app.GapRemedy`.
  - SG lands before S3 and before the extraction design's E1, owned by whichever design lands
    first (OR9). **[ruling OR9 = J4]** This design owns it; E1 depends on SG, not on S3.
  - The breaker thresholds and the 404 class stay per consumer, each with a written
    justification (§4.8.1).
- **g3 / mime-fidelity [FC26].** The overlap is **semantic**, not textual.
  - Both stub-identity changes go through g3's declared hook, in `replayV1`'s panicked branch:
    the raw-bytes digest, and the listing Message-ID written to `terms.InternetMessageID`.
  - `ReadGaps unreadable:attachments` covers a failure of the whole attachment table, and g3's
    `Attachment.GapKind` covers per-part gaps.
  - `GapClass` and `probeImproves` own every record gap label, g3's included.
  - **Merge order.** g3's S1 lands before S2b. If it cannot, S2b carries the edit in today's
    `safeParseMessage`, and g3's MA-330 golden is regenerated for panicking inputs only. g3's
    MA-349 ("never a stub") and this design's MA-318 raw stub are reconciled in that merge.
    **[ruling J5, J8]** The order is fixed: S1a → g3 S0 → g3 S1 → S2b, so the fallback is not
    taken. The golden regeneration is the owner's (ESC-1), in S2b-id.
- **g4 / recapture-integrity [FC5].** Rev 1 said "Nothing here depends on g4/N2". The text now
  reads:
  - S1a carries the KEEP guard (P15) itself.
  - g4's monotone re-capture subsumes it when it lands.
  - Supersede is metadata only, so it has no unchanged-path crash window for g4 to own.
- **g7/F53** (manifest-save surfacing) should land before S1b ships, so that P4's "durably" is
  unconditional.
- **Dashboard session fix (F3/E0, local-confidentiality) [FC22].** Gap subjects appear on the
  dashboard's GET pages only after it lands.
- **The N1 root-cause fix** (`SanitizeSegment` byte bound) can land any time. It reduces
  `folder-unwritable` occurrences.

## 11. Owner rulings (open; each blocks the named slice)

Each open question of rev 1 is now an owner ruling, with the review's added facts. The review
(`docs/review-fault-isolation-predesign.md`) does not impose any of them. **All ten are
RULED** in `docs/owner-rulings-2026-10-09.md` §3 (none escalated); each keeps its question
for context, followed by its ruling.

- **OR1 (was OQ1) — Last-run status for a partial run.** Blocks S1b. There are two options.
  - **The new value `"partial"`.** It is honest. It has two costs:
    - `last_run.status` widens its documented value set (README.md:609-611), so it needs
      `JSONVersion` 3 and a README update;
    - an older `status` binary shows **GREEN** for a partial run whose gaps are all in the
      ledger. It ignores `capture_gaps` entirely, which is wider than rev 1 stated.
  - **`"failed"` plus a `partial` flag.** Older readers fail closed with RED, and the JSON stays
    v2 additive. The cost is that "failed" then has two meanings in the record and in
    `status -json`.

  Until this is ruled, S1a writes `failed`.
  **RULED (delegated-judgement): `"failed"` plus `partial: true`;** `JSONVersion` stays 2.
  An older `status` then fails closed (RED) rather than showing GREEN, and no documented value
  set widens.
- **OR2 (was OQ7) — X1 amendment.** X1 is a **locked** contract in `docs/ux-contract.md`. Should
  `3` become the "partial" exit code for the capture verbs? The text is in §2.1: it is defined by
  the mechanism, cancel stays 0, and the copy export's partition differs. Blocks S1b. Until this
  is ruled, S1a exits 1 on a partial run.
  **RULED (delegated-judgement): no amendment.** A partial capture run keeps exiting 1, and the
  partial state travels in the flag, the summary, the sidecar and `status`. X1 is locked, and
  its amendment is an owner act; exit 3 would also stop monitors keyed on exit 1 from alerting.
  The owner may still amend X1 later. P9 and MA-325 read "exit 1 with the `partial` flag".
- **OR3 (was OQ2) — Persistent known gaps.** Blocks S1b's sidecar and notification behaviour.
  An example is one permanently damaged PST node. There are two options:
  - **Honest-always.** Every run exits `3`, keeps `BACKUP-NEEDS-ATTENTION.txt`, and sends the
    nightly `-job` notification.
  - **Report new loss only.** Exit `3` and the notification only when `GapsNew > 0` or the
    stuck set grows. A known-only state is carried by `status`.

  Either way the sidecar, the notification and the card lead with "new this run: K", and
  `last_run.gaps_new` is exposed [FC23(2)]. This ruling also decides whether not-attempted gaps
  are a note (the default) or a WARN. An acknowledge mechanism remains a separate design.
  **RULED (delegated-judgement): honest-always,** with exit 1 under OR2. Each run that observes
  a fillable or terminal gap is partial on every surface (exit 1, the sidecar kept, the `-job`
  notification sent), and each leads with "new this run: K" and `last_run.gaps_new`.
  Not-attempted gaps stay a note. Reporting known loss as a clean run would accept it.
- **OR4 (was OQ3) — Self-closing "vanished" faults.** A Graph 404, or a maildir file removed
  mid-read, may be seen for the first time on a run. Is that run partial, or report-only unless
  the fault recurs? Recommendation: partial, because a one-shot "archive before departure" run
  must not claim completeness. Blocks S1b.
  **RULED (delegated-recommended): partial.**
- **OR5 (was OQ4) — Stuck threshold: the number and the unit.** Counting runs makes escalation
  depend on cadence and on clicks: hourly schedules exist, and so does a manual Archive now.
  - The review suggests: `Runs ≥ 3` AND `now − FirstRun ≥ max(72 h, two schedule periods)`,
    counting at most one observation per day.
  - [FC18] fixes the posture at **WARN**. Making it RED would amend the existing posture
    partition, which is also the owner's call.
  - Feed into this ruling: 500 `ErrorMimeContentConversionFailed` is deterministic per item.

  Blocks S1a's stuck rule.
  **RULED (delegated-recommended): the review's suggestion,** with the posture fixed at WARN,
  never RED.
- **OR6 (was OQ5) — Directory links in local stores.** Today they are followed. Blocks S2b.
  There are three options:
  - never follow (rev 1);
  - an explicit opt-in on the **same** input, so the token is kept;
  - follow links that resolve inside the store, guarded by a visited (dev, inode) set.

  It also decides the migration for archives that capture through links today. Rev 1's remedy
  ("archive the real location as its own `-input`") mints a new token and re-exports every
  message into a second tree.
  **RULED (delegated-judgement): not by default; an explicit per-input opt-in** (a flag, a GUI
  option and a job-descriptor field, following X4/X5) keeps the input's token and follows its
  directory links, guarded by a visited (dev, inode) set. File links out of the store and
  special files stay refused even with the opt-in. An archive that captures through links today
  gets a terminal gap whose remedy names the opt-in. R25's text names the opt-in.
- **OR7 (was OQ6) — Graph 403 on `$value`.** Mailbox-fatal (recommended: a revoked grant then
  does not become thousands of gaps), or a per-item gap guarded only by the breaker? A failed
  token refresh is auth either way [FC11]. Blocks S3.
  **RULED (delegated-recommended): mailbox-fatal.**
- **OR8 (new) — Catalog id allocation (operator).** Five designs in flight propose an R22 and an
  S42, each with a different meaning: fault-isolation, extraction-workspace,
  recapture-integrity, local-confidentiality and mime-fidelity. The R and S ids must be
  allocated once, before any catalog-first edit. Blocks S1a and every later catalog edit.
  **RULED (delegated-judgement) = J1:** R25, S46–S48, MA-310..329 plus MA-501..504.
- **OR9 (new) — SG ownership (operator).** Which design owns the shared Graph primitive? And
  will the extraction design change its side? Its `StatusError` would gain `GraphCode`, and its
  404 class and breaker thresholds would be reconciled or justified. Blocks SG, and through it
  S3 and the extraction design's E1.
  **RULED (delegated-judgement) = J4:** this design owns SG, a standalone slice before S3 and
  before the extraction design's E1, with `StatusError{Code, GraphCode, Text}` keeping today's
  `Error()` text and one `graph.Classify`; the 404 class and breaker thresholds stay per
  consumer, each justified. Capture's host/path pin is step SGc, behind the extraction review's
  XC25 gate (lifting it is ESC-7). RR-6 re-ran lenses 3, 6, 7 and 8 before SG code. Its
  addendum is filed (GO_WITH_CONDITIONS), and its conditions RR6-C1–RR6-C6 are folded and
  re-verified. The re-verification raised owner rulings RR6-OR1 and RR6-OR2 in the addendum,
  each with a safe interim that blocks nothing [RR-6 re-verification].
- **OR10 (new, optional) — The P7 residual.** Accept the stated residual (the default), or
  exclude PhysID-less records from the sweep on any fault whose identity matched no record? The
  stricter choice defers gone-detection on pre-v6 archives while such a fault persists. Blocks
  nothing; the default applies unless the owner rules otherwise.
  **RULED (delegated-judgement): the stricter rule.** The default would accept a known phantom
  "gone" (R21, S37); gone-detection is deferred instead, and `status` names it.

## 12. Honesty of claims

- **Proven** (once the rows named in §4.11 are green, tier U/S in CI) [FC32]: P1, P2, P5, P8,
  P9, P10, P11 (the close and escalate half), P13, P14 (with its named exceptions) and P15.
  - P3 and P12 are proven on Unix, except for their stated conditions.
  - Any clause in §4.11 still without an asserting case is conditional until it has one.
- **Conditional, with the condition stated at the claim:**
  - P1: containment requires a go-pst fault to surface as an error or a panic. Transport-class
    Graph faults are bounded by the breaker rules (§4.8.5) until MA-313's new cases are green.
  - P3: the run finishes unless go-pst or the OS hangs, or the process is killed (N7/N8). The
    depth cap of 300 assumes no deeper genuine store. The wall-clock bound is per mailbox.
    - **[RR-6 RR6-C4]** Its 30 minutes hold only with a failing control request, and only for
      counted faults. Known poison never counts, so every known-poison item pays its timer on
      every run, whatever the control request returns [re-verification]. With a healthy
      control request the bound is per item. This is an open condition, not accepted.
    - **[RR-6 RR6-C2]** For capture the 2 h overall cap applies from S3. Until then it is
      today's 10 minutes.
    - **[RR-6 RR6-C6; re-verification] Graph folder breadth.** A `childFolders` response that
      invents fresh folders at every level is bounded in depth, and per loop by the page cap,
      but not in total: an open gap, not accepted, encoded by MA-541's skip encoder citing R10
      (owner ruling RR6-OR2).
  - P4 "durably": requires g7/F53.
  - P6 recovery: requires an in-place repair that keeps the store identity and the NID.
    Rebuilt stores (`-outlook`, `.ost`, no readable store-uid) get aggregate gaps and no
    per-item recovery. A no-Message-ID partial read leaves a second, partial record.
  - P7: there is a stated residual. A pre-v6, PhysID-less `mid:` record with mismatched listing
    and parsed Message-IDs can get a phantom gone event (OR10). **[ruling OR10]** Closed by the
    stricter rule: such a record's gone-detection is deferred while the fault persists, never
    wrong.
  - P3's capture-side host pin: holds only from step SGc, behind the extraction review's XC25
    gate; until then a foreign `nextLink` in capture's listings is an open gap, not accepted
    [ruling J4].
  - P12: a static hostile store, not a same-user racer, and **not writable by another local
    user**. On Windows it rests on the FC1 classifier, and the cloud-placeholder case is
    lab-pending (MA-503). With an input's directory-link opt-in set, that input's directory
    links are followed under the visited (dev, inode) set [ruling OR6].
  - F33's collapse of distinct unparseable raw messages into one record stays until the owner
    answers ESC-1, encoded by a skip encoder citing R1 in MA-318 [ruling J8].
  - `unparsed` is terminal **for one parser generation**.
  - The whole Graph error classification is proven against the fake server only. Real-tenant
    codes, and Exchange's maximum message size for the download cap, are lab-unverified.
    - **[RR-6 RR6-C3]** The cap's figure is set in SG from Microsoft's documentation, not from a
      tenant. ~~If that documentation cannot bound a stored item, or the limit later rises, the
      cap is an open condition, not accepted.~~ **[re-verification]** It reaches capture only
      at a figure that documentation bounds. Where none does, capture keeps today's
      uncapped read, bounded by time only: an open gap, not accepted, skip-encoded in MA-540
      (owner ruling RR6-OR1's safe interim). A cap that reaches capture holds a legal item only
      while Microsoft's documented limit stays at or below it; a later rise is part of
      RR6-OR1's question and, until it is answered, an open condition, not accepted.
    - **[RR-6 RR6-C6]** The Graph depth cap of 300 is confirmed the same way, in SG.
      **[re-verification]** A higher documented figure becomes the cap. With no figure,
      capture's `Folders` carries no depth cap, as today, an endless chain of fresh ids is an
      open gap, not accepted, and MA-541's over-depth case is a skip encoder.
  - `PidTagRecordKey` readability through go-pst is verified in S2a. If go-pst cannot read it,
    every PST uses aggregate gaps.
- **Aspirational:** P11's "heals" for fillable Graph gaps. The tool retries and reports; only
  Microsoft can serve the item.
- **Not claimed:**
  - repair of sources;
  - recovery of content go-pst cannot parse;
  - a watchdog for go-pst internal hangs (N7);
  - resynchronisation of mbox streams (N2);
  - a mid-run cancel for desktop Archive now;
  - operator-tunable thresholds. Those are on the operability backlog, and every threshold
    prints in its message.
