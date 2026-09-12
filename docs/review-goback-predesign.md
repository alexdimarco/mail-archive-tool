# Pre-code design review — go-back point-in-time archive (10 lenses ×2)

**Design:** `docs/design-goback.md`. **Verdict:** GO_WITH_CONDITIONS. Two gate
runs fed revision 2:

- **Go-back gate** (2026-09-12, over rev 1): 39 findings, all confirmed or
  partial, 0 refuted; rescue GO_WITH_CONDITIONS. Digest:
  `scratchpad/goback-gate-digest.md`.
- **Dedup/trash gate** (2026-09-11, the superseded dedup design whose core
  go-back inherits): 37 findings, 36 confirmed, rescue GO_WITH_CONDITIONS.
  Digest: `scratchpad/dedup-gate-digest.md`.

Both used five finders (two lenses each), a refutation-default skeptic per
finding, and a rescue reviewer; every surviving blocker/high dissolved into a
condition now in `docs/design-goback.md` §3/§5.

## Hidden blocker (rescue, both gates)

Dropping the folder from the key and "adding a migration" to collapse existing
`(token, folder, identity)` records into `(token, identity)` would **silently
drop a genuinely distinct message** that reused a Message-ID in another folder —
an R1 violation. Resolved by making the collapse and the live dedup
**fingerprint-safe**: unify only records whose stored `Fingerprint` matches, and
gate the mailbox-wide skip on an envelope signature (from a widened Graph
`$select`) matching a sibling's fingerprint — otherwise download and let the
existing `#fp` split file it (design §3.1, §3.2).

## Blocker/high conditions folded into rev 2

- **Key format v4, version-gated discrimination** (GB-01/GB-1/F1): bump manifest
  v4 + index v3; the v2→v3 rescue is gated on the version integer, not NUL count,
  so the folder-less key is never misread as legacy. (§3.1)
- **Fingerprint-safe v3→v4 collapse migration** (GB-2/GB-04/hidden): unify only
  same-fingerprint records; distinct ones stay `#fp` siblings; first-captured
  file wins; losers become history events + pruned index rows + `verify`-reported
  orphans; logged; done once. (§3.1)
- **Never dedup on Message-ID alone** (dedup-F1): widen the Graph listing
  `$select`; skip only on an envelope-signature/fingerprint match; else download.
  Identity index = `mid → []{key, fingerprint}`. (§3.2)
- **Full reconciliation for gone/present-again** (GB-03/GB-04/GB-08/GB-3/GB-4/
  F3): every observation stamps `LastSeen`; `gone` computed once after a full
  walk, scoped to folders actually walked; a present-again event; the fold takes
  the latest transition at ≤ D. (§3.3, §3.4)
- **Crash ordering** (GB-02/GB-2/GB-5/F4): history append+fsync → index →
  manifest anchor, at run end and each checkpoint; a crash costs at most a
  duplicate (idempotent) event, never a lost move. (§3.5)
- **Torn-tail write side** (GB-4): fix the trailing newline on open-for-append.
  (§3.3)
- **Static pages first-captured; current + at-D are serve-only** (rescue G;
  removes F5/GB-6 cross-dir-link blockers). (§3.4, §3.6)
- **Move fast-path records without download** (GB-06/GB-3/F2), in-place merge
  preserving fixity (dedup-F4); index folder column follows via a body-free
  update. (§3.2)
- **Scope to the live path** (dedup-F3/E): one-shot local imports keep R3.
  **Folder identity by stable id** (GB-5/dedup-F2). **Both run modes dedup**
  (dedup-F6). **Trash/junk by resolved well-known id, excluded by default,
  config asks** (operator ruling; dedup-F6/F7). (§3.6, §2 T7)
- **History-log recovery/legibility** (F2/H): status/verify coverage + torn-tail
  flag; serve says "go-back partial" on a bad log. (§3.4)
- **New-field load defaults** under the migration (GB-05). **manifest v4 refuses
  an older binary** (dedup-F3/F7). (§3.1)

## Invariants (operator-adopted)

R3 and R17 reworded, R21 added (design §5); S37/S38/S39 and per-slice MA rows to
be registered by the build. Medium/low findings (14 go-back + the dedup set) are
folded as the detailed conditions of §3 or noted as honest limits in §5.
