# Adversarial review — the exported archive, worked with offline

Per assurance-kit `process/adversarial-review.md`. Question put to the archive:
*is it a robust archive that can be worked with safely offline, with or without
the tool?* Five finders with distinct lenses each built a real archive from the
fixture and from crafted adversarial mbox files, then a refutation-default
skeptic per finding reproduced or refuted it against the real code and output.
Run as a 35-agent workflow on 2026-09-02. **32 found / 29 confirmed or partial /
0 refuted** (3 fell to the verification cap and were re-examined by hand; two
were low-severity duplicates of confirmed items, one — directory listing under
`serve` — was fixed with SRV-2).

Every confirmed finding below is fixed on `main` with a prove-fail record in
its commit; the catalog rows named are its tombstones.

| ID | Lens | Claim (verified) | Verdict | Fix commit | Tombstone |
|---|---|---|---|---|---|
| OFF-1, INH-3, OFF-4, OFF-3 | offline, inheritor | Exported `.html` had no CSP; scripts ran and remote loads fired from `file://` (both render paths); `<base>`/`<form>` live | CONFIRMED (high) | `6f080f5` | R19, MA-80 |
| OFF-2 | offline | `<meta http-equiv=refresh>` navigates on open; CSP cannot restrain it | CONFIRMED (medium) | `6f080f5` (neutralized) | MA-80 |
| SRV-1 | served | Stored XSS: snippets rendered via innerHTML unescaped, UI page with no CSP | CONFIRMED (high) | `6f080f5` | MA-81, MA-83 |
| SRV-2 | served | `serve` followed symlinks out of `-out`; listed directories | CONFIRMED (medium) | `6f080f5` | MA-82 |
| SRV-3 | served | No warning on a non-loopback bind; no server timeouts | PARTIAL (low) | `6f080f5` | MA-84 |
| INT-1 | integrity | Two distinct messages sharing a Message-ID in a folder: the second silently lost | CONFIRMED (high) | `751f021` | R3, MA-86 |
| INT-2 | integrity | Manifest saved only per store; a crash mid-store lost all progress | PARTIAL (medium) | `751f021` | MA-88 |
| INT-4 | integrity | No output-directory lock; overlapping runs clobber the manifest/index | CONFIRMED (medium) | `751f021` | R5, MA-85 |
| INT-5 | integrity | Verification report truncated every run | CONFIRMED (medium) | `49e3386` | R1, MA-68 |
| INT-6 | integrity | `.html`/`.zip` written in place, non-atomically | CONFIRMED (medium) | `679d2bc` | R5, MA-69 |
| INT-7 | integrity | Manifest write had no fsync; a torn manifest was a bare parse error | PARTIAL (low) | `679d2bc` | MA-94 |
| P1 | portability | No total-path bound (Windows MAX_PATH) | PARTIAL (medium) | `68a4096` | MA-89 |
| P2 | portability | Reserved device names with an extension (`NUL.txt`) not neutralized | CONFIRMED (medium) | `68a4096` | MA-01 |
| P3 | portability | Distinct folders differing only in illegal characters merged | CONFIRMED (medium) | `68a4096` | MA-89 |
| P5 | portability | Trailing dot preserved via the extension path | CONFIRMED (medium) | `68a4096` | MA-03 |
| P6 | portability | Non-Latin subjects collapsed to "untitled" | PARTIAL (low) | `68a4096` | MA-04 |
| P7 | portability | 32-bit stem collision overwrote a message | CONFIRMED (medium) | `751f021` | R4, MA-87 |
| P8 | portability | No NFC normalization | CONFIRMED (low) | `68a4096` | MA-04 |
| P4 | portability | Case-only folder collisions on case-insensitive file systems | PARTIAL (low) | — acknowledged limit (catalog §3, README) | — |
| INH-1 | inheritor | Attachments invisible from the message page | PARTIAL (low) | `d6bf79a` | R7, MA-90 |
| INH-2 | inheritor | No raw `.eml`; Reply-To/threading headers dropped | PARTIAL (low) | `d6bf79a` (`-raw`, headers) | MA-96, MA-13 |
| INH-4 | inheritor | Message-ID hidden; Sent vs Received collapsed | PARTIAL (low) | `d6bf79a` | MA-90 |
| INH-5 | inheritor | Times silently converted to UTC, offset discarded, no UTC label | CONFIRMED (medium) | `d6bf79a` | MA-90, MA-91, MA-13 |
| INH-6 | inheritor | No README/layout marker inside the archive | PARTIAL (low) | `d6bf79a` | MA-92 |
| INH-7 | inheritor | Folder pages capped at 5000 rows, oldest hidden | PARTIAL (low) | `d6bf79a` (pagination) | MA-91 |
| INH-8 | inheritor | No navigation from a message page | CONFIRMED (low) | `d6bf79a` | MA-90 |

## Residual, by design

- Case-only and NFC/NFD-only sibling folders can collide on a case-insensitive
  file system (P4): acknowledged in the catalog; the messages remain distinct
  (folder-scoped keys, hashed stems) and are merely co-located.
- The Graph incremental fast-path dedups by Message-ID alone (the price of
  R17's no-re-download guarantee): acknowledged in the catalog.
- The CSP `<meta>` is enforced by every current browser on `file://`; a viewer
  that ignores CSP entirely (some e-mail-client previewers) still shows the
  preserved content with its remote references intact. Stripping would lose
  content; the policy is the control.
