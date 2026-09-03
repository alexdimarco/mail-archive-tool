# Adversarial review — archive portability (extract, reindex -rebuild, message state)

> As of 2026-09-03, over the tree at `8d8b984` (the portability slices merged).
> Method: the assurance-kit adversarial pass run as a workflow — three finders
> (outside aggressor via mail content; insider with write access to the archive
> or -dest; integrity/concurrency/crash), each finding handed to a
> refutation-default skeptic with the built binary. 6 confirmed (2 high, same
> root cause), 0 refuted. Every finding is dispositioned; the fixes landed in
> commit `91a7b00`.

## Findings and disposition

| # | Lens | Sev. | Verdict | Finding | Disposition |
|---|---|---|---|---|---|
| AGG-1 | outside | high | CONFIRMED | reindex -rebuild: hostile HTML mail shadows its own header/body via <head> markup, forging | Fixed (91a7b00): neutralize strips the reserved namespace; the reader anchors to body's first header div. MA-187. |
| INT-1 | integrity | high | CONFIRMED | reindex -rebuild trusts a mail-forged header: HTML <head> content poisons the reconstructe | Fixed (91a7b00), same two layers as AGG-1. MA-187. |
| INS-EXTRACT-DEST-SYMLINK | insider | medium | CONFIRMED | extract writes preserved mail OUTSIDE -dest through a symlinked directory component (write | Fixed (91a7b00): destDirSafe walks -dest components no-follow, skips-and-reports. MA-188. |
| INT-2 | integrity | low | PARTIAL | Rebuild drops a real record from the manifest on a transient index-write error, and index. | Fixed (91a7b00): index.Add rolls back the batch on an insert error (defense-in-depth). |
| INT-3 | integrity | low | PARTIAL | A crashed reindex -rebuild leaves search.db.rebuild(+ -wal/-shm) that only the next rebuil | Fixed (91a7b00): a routine reindex reclaims a crashed rebuild's search.db.rebuild. MA-189. |
| INT-4 | integrity | low | CONFIRMED | extract and rebuild rename outputs into place without fsyncing the containing directory | Fixed (91a7b00): extract + reindex fsync the parent dir after rename (export.SyncDir). |

## The high-severity root cause

Both high findings (AGG-1, INT-1) were the same defect from two angles. The
from-HTML rebuild reader searched the whole parsed document for the first
element carrying `class="mailarchive-header"` (or a `data-mailarchive-field`
marker), matching any element and visiting `<head>` before `<body>`. The
renderer copies a mail message's `<head>` children verbatim into the exported
page and `neutralize` did not strip the tool's reserved `mailarchive-*`
namespace. So a hostile HTML email carrying
`<head><style class="mailarchive-header">…` or a `<template>` forgery survived
into the page and was read on rebuild as the record's header — erasing or
forging its indexed sender, subject, and date, a silent break of the search
parity R8 and the rebuild fidelity S33 that the predesign review believed
condition PC3 had closed (PC3 considered only later body markup, not head
injection or non-`<div>` elements). The fix is two layers, both landed:
the source strips the reserved namespace from all mail content (protects new
archives), and the reader anchors to the first `<div class="mailarchive-header">`
that is a direct child of `<body>` (protects the installed base whose old pages
were written before the strip).
