# Going back in time — the point-in-time (Wayback) view

`mailarchive serve` can show you what a mailbox looked like on a **past date** —
not just what is in it now. This is the *go-back* (point-in-time) view: pick a
day, and the archive redraws itself with the folders and messages as they stood
then, including messages that have since been deleted online.

This guide is the operator's how-to. The one-paragraph summary is in the
[README](../README.md#going-back-in-time-point-in-time-view); the mechanism and
its invariants are in [`design-goback.md`](design-goback.md) (§3 mechanisms, §5
invariants R3/R17/R21) and the catalog scenarios S37–S39.

## In short

- Go-back is a **`serve` feature**. Start `mailarchive serve -out ./archive` and
  click **Go back in time →**, or open `/goback` (current) and
  `/goback?at=YYYY-MM-DD` (a past date).
- It works only for a **live** capture (`mailarchive graph`; IMAP later), which
  records a timeline as it runs. A one-shot local import (a `.pst`, mbox, or
  maildir) has no timeline and no go-back.
- The **static** pages you open from disk with no tool never change: they always
  group each message under the folder it was **first captured** in.
- Its resolution is your **run cadence** — so capture **daily**.
- "Gone" has **two meanings**: deleted from the *mailbox* (kept in the archive,
  visible at past dates) vs removed from the *archive* (a redaction — gone at
  every date).

## Turning it on: capture daily on a schedule

The timeline is written by a live capture and grows one entry per run. Nothing
extra to enable — just run `graph`, ideally **daily**, on a schedule:

```sh
mailarchive schedule -interval daily -at 03:00 -install -- graph -out ./archive \
  -tenant T -client-id ID -mailbox alice@org.example \
  -client-secret-file ~/.config/mailarchive/graph.secret
```

`schedule` defaults to a daily run precisely because go-back can only distinguish
the days it actually observed (see [Limits](#limits-read-these) below). A weekly
cadence gives you a weekly timeline; a daily one gives you a daily timeline.
Daily is the recommended floor.

The timeline lives in one append-only file beside the manifest:
`.mailarchive-history.jsonl`. Each run appends only what changed — a message
newly seen, one that moved folders, one that went **gone**, one that came back —
so the file grows with change, not with mailbox size. It is safe to leave alone;
`reindex` is the only thing that ever rewrites it (see
[Redacting](#redacting-remove-something-for-good)).

## Using it in `serve`

```sh
mailarchive serve -out ./archive     # then open http://127.0.0.1:8099/
```

The search-and-read home page carries a **Go back in time →** link to `/goback`.
There are two projections, both rendered entirely on the server:

- **Current** — `/goback`. Every message under the folder it lives in **now**;
  messages that have left the mailbox are hidden. This follows the live mailbox:
  a message moved from Inbox to `Projects/Acme` shows under `Projects/Acme`, not
  where it was first filed.
- **As of a date** — `/goback?at=2026-07-15`, or click any date in the track.
  The timeline is folded to the **end of that day** and each message is shown
  under the folder it was in **then** — including a message deleted online after
  that date, which still appears (under its last-known folder) for any date
  before it went away, and reappears after a restore.

At the top of the page is a **date track**: the days the archive was actually
captured, newest first. Click one to jump to that date's view. A `?at` value that
is not a valid `YYYY-MM-DD` is announced ("that date could not be read") and the
page falls back to the current view — it never echoes the raw value back into the
page.

The go-back page is **fully server-rendered and carries no script** (it is sent
under a strict `default-src 'none'` policy). Every folder name and subject on it —
values a hostile mailbox can shape — is escaped as text, so a folder literally
named `<script>` is inert.

## The static pages never change

Open `index.html` (or any folder page) straight from the archive folder, with no
tool at all, and you get the **first-captured** layout: each message under the
folder it was in when it was first archived. These pages do **not** follow moves
and have **no** date slider — by design. They carry no script, which is exactly
what makes them safe to open from a file system, and go-back is computed live by
the server, so:

- the **static** view = each message's first-captured folder, forever stable;
- the **served current** view = each message's *current* folder, following moves;
- the **served at-date** view = each message's folder on the chosen day.

There is no offline time machine; the slider exists only while `serve` runs.

## What "gone" means

A message can disappear from the *current* view for two very different reasons.
It matters which you mean:

### 1. Deleted from the mailbox — a timeline event

Someone deleted (or the retention policy purged) the message online. The
archive's copy is **kept on disk**. The message drops out of the *current* view,
but every past date **before** the deletion still shows it, under the folder it
lived in then. If it comes back later (restored from Deleted Items), a
*present-again* event is recorded and the folded timeline reads *absent* only
between the deletion and the restore. Nothing in the archive is destroyed — over
time the archive is a **superset** of the live mailbox.

> Everyday deletes usually don't even become "gone": in Outlook/Exchange
> deleting a message *moves* it to Deleted Items, and a `graph` capture skips
> Deleted Items by default (see below). The message is simply no longer in a
> walked folder, so it becomes gone in the timeline (its earlier copy kept), not
> re-captured under a trash folder.

### 2. Removed from the archive — a redaction

You genuinely want something *out* of the archive. That is a deliberate,
operator-driven **redaction**, covered next.

## Redacting: remove something for good

To take a message out of the archive at **every** date:

```sh
# 1. Delete its files — name all three; the shell glob <stem>.* would MISS the
#    dash in -attachments.zip.
stem=./archive/<store>/<folder>/2026-07-15_1032_subject_a1b2c3d4
rm "$stem.html" "$stem-attachments.zip" "$stem.eml"   # .eml only in a -raw archive
# 2. Reconcile the archive to disk (prunes the index, manifest and timeline).
mailarchive reindex -out ./archive
# 3. Confirm nothing remains under that name (any leftover file is NOT yet gone).
mailarchive verify -out ./archive
```

Within the archive, two things make this complete:

- **`serve` intersects every date's view with the files actually on disk.** The
  moment the files are gone, the message vanishes from the current view *and*
  from every `?at=D` view — even before you run `reindex`.
- **`reindex` compacts the timeline.** It drops the removed message's events from
  `.mailarchive-history.jsonl` (keeping run headers, folder renames, and the
  events of messages merely *departed* from the mailbox whose files are still on
  disk), so the redaction spans every date in the archive. The rewrite is atomic
  and a no-op when nothing was removed.

A **normal** run never deletes a message file (invariant R13) — a redaction is
always something you do by hand. That asymmetry is the point: the archive won't
lose anything on its own, and you can still deliberately excise something.

**One precondition — redaction acts on the archive, not the live mailbox.**
Incremental capture skips a message whose Internet-Message-ID is already in the
manifest, and `reindex` removes that manifest entry. So if the message you
redacted still lives in the connected mailbox in a folder you capture, the next
scheduled `graph` run has no record of it and **archives it again**. To remove
it for good, also delete or move it out of the live mailbox (or ensure it lives
only in an excluded folder such as Deleted Items). Closing that loop *inside the
tool* — a redaction list the capture path honours, so a still-live message is
never re-archived — is a planned change under design review; today the operator
closes it in the mailbox.

## Deleted Items and Junk are excluded by default

A `graph` capture **skips the Deleted Items and Junk Email folders by default**,
matched by their resolved well-known-folder ids (so the exclusion holds in any
mailbox display language, never a fragile display-name match). This keeps the
everyday delete-to-trash from filling the archive with junk, and makes an
online delete read as *gone* in the timeline rather than as a re-capture under a
trash folder.

To archive those folders too:

```sh
mailarchive graph -out ./archive … -include-deleted -include-junk
```

Their messages are then captured, deduped and timelined like any other. The CLI
flags, the scheduled-job preview, and the GUI all surface this choice, and a
click-through leaves the default (excluded). See
[`graph-app-setup.md`](graph-app-setup.md) and the README's
[Server-side archiving](../README.md#server-side-archiving-microsoft-365-via-graph)
section.

## Health and recovery

`mailarchive status -out ./archive` reports the timeline on a **History** line:

- `History: N run(s), M event(s) recorded — go-back available` — GREEN.
- `History: none recorded — no go-back timeline (a live capture writes it; a
  one-shot local import has none)` — GREEN (nothing is wrong; there is simply no
  timeline for a local import).
- A **torn tail** (a run crashed mid-line) or an **unreadable line** is a WARN,
  naming the remedy: the next scheduled run repairs a torn tail, and
  `reindex` compacts and heals the log now. A log that cannot be read at all is a
  RED.

`serve` matches this: with no log it announces **go-back unavailable** and shows
the current mailbox only; with a damaged log it announces **go-back partial** —
it never silently pretends a broken timeline is complete. `status -json` carries
a `history {exists, runs, events, bad_lines, torn_tail}` object and a posture
reason code (`history_torn_tail` / `history_corrupt`) for a monitor to key on.

The timeline is **crash-safe**: within each run the message file and its history
events are fsync'd, and the manifest — the "state as of now" projection — is
saved **last**, so a crash costs at most a duplicate event (harmless when the log
is folded), never a lost move.

## Limits (read these)

- **Resolution = run cadence.** Go-back distinguishes only the days it observed.
  A message that appeared and was deleted *between* two runs is never seen, and a
  move is dated to the run that first observed it, not the moment it happened.
  Capture **daily** for a daily timeline.
- **Live sources only.** `graph` writes the timeline (IMAP later). A one-shot
  local import (`.pst`, mbox, maildir) has no timeline and no go-back — its view
  is the static first-captured layout.
- **A pre-existing live archive is consolidated on the first upgraded run.**
  Upgrading a pre-v5 Graph archive collapses any per-folder move-duplicates into
  one mailbox-wide record *before* the first walk and re-keys the search index to
  match, so each still-present message is recognised by its Internet-Message-ID
  and **not** re-downloaded — one copy per message from then on. No file is ever
  deleted by this (R13): a collapsed duplicate's copy stays on disk and is reached
  only by a later redaction.
- **A reused Message-ID: closed on Graph for messages captured under v6.** Some
  senders stamp two genuinely *different* messages with the *same*
  Internet-Message-ID. On the live `graph` path this is resolved by two signals
  recorded when a message is archived under this version: Microsoft Graph gives each
  physical message a distinct **immutable id**, and the archive stores a
  body-inclusive **content hash**. A reuse whose id the archive has not seen is
  downloaded and compared by content hash: a different body → kept as a distinct
  message (a `$100` and a `$250` invoice that share an id *and* an envelope both
  survive — the case the fingerprint alone could not split); the same content with a
  reissued id, or a **copy** of the message filed in another folder → its id is added to the record's content-equal id-set (one record, no duplicate), and every copy is skipped on later runs with no re-download.
  The content hash excludes transport headers, so a migration that only rewrites
  `Received`/etc. is still recognised as the same message. The honest edges, all
  bounded and **none deletes a file**:
    - **Existing (pre-v6) archives keep the Message-ID floor for their
      already-captured messages.** The closure needs the content hash to tell a
      reuse apart, and a message archived before this version has none; the tool
      does **not** re-download the whole mailbox to backfill it (no upgrade storm).
      So a distinct reuse of a *pre-v6* message's Message-ID is skipped just as on
      the floor — the same bounded residual as before, never a new drop. Messages
      captured **fresh** under v6 get the full closure; to close it retroactively,
      re-capture into a new `-out`.
    - **Sources without a per-message id (IMAP / local imports)** keep the floor:
      a distinct reuse with an *identical* envelope may look already-archived and be
      skipped.
    - **A tenant that withholds the immutable id** degrades to the
      Internet-Message-ID floor above.
    - A `-mode full` re-export over a *floor* archive may **overwrite** the
      search/browse record of a copy whose live message is gone — but that copy's
      **file is never deleted** (it stays on disk and `mailarchive verify` flags
      it), so nothing is lost irrecoverably.
- **A message copied into several folders shows one record whose folder may flap.**
  Mailbox-wide dedup keeps ONE record for a message that exists as copies in two or
  more folders (same Message-ID, identical content) — it is never duplicated, and
  with the immutable-id set it is not re-downloaded. But the record has a single
  "current folder", so as each run walks the copies the folder (and the go-back
  timeline) can show it moving between those folders even though it never moved. This
  is a pre-existing property of mailbox-wide dedup (not introduced by the immutable-id
  work) and is cosmetic: no message is lost, duplicated, or re-fetched.
- **Static pages stay first-captured.** The offline pages never follow moves or
  offer a slider; only `serve` does. This is deliberate (no script offline).
- **A move is inferred, not a server event.** The tool notices a message is in a
  different folder than last run and records that as a move; it does not receive a
  push "moved" event, so a message that moves twice between two runs shows only
  its latest folder for that run.
- **Append-only, reindex-compactable.** The log only grows during capture; the
  one thing that shrinks it is `reindex`, which compacts it as part of a
  redaction (above).
