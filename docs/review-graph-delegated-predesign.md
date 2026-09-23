# Pre-code design review — Microsoft 365 per-user (delegated) capture

**Design reviewed:** `docs/design-graph-delegated.md` revision 1 (2026-09-23).
**Procedure:** the 10-lens battery of `../assurance-kit/process/design-review.md`, run
finder → skeptic (refutation-default) → rescue, single reviewer. **13 findings raised ·
11 survived · 2 refuted.** Rescue: 6 apparent blockers all DISSOLVED-BY a named change
(no re-design); the rescue pass surfaced one hidden blocker the ten lenses missed.

**Verdict: GO_WITH_CONDITIONS.** Conditions **D-C1..D-C7** are must-fix-before-code and
fold into design revision 2. No lens returned NO_GO. Slices a/b/c each still owe their own
downstream gate (adversarial / friction) as the design states.

## Hidden blocker (rescue pass) — device-flow scheduling couples consent identity to run identity

For app-only, a scheduled `graph` job needs no interactive session: the secret **file**
works for any run identity (that was schedule-v2's "no environment for the secret" fix —
a file, MA-74). Device flow breaks that decoupling. The interactive consent writes a
per-user, `0600`-owned token cache; a scheduled task running as a **different** identity
(Windows SYSTEM/service account, or a non-interactive account with no way to ever complete
a device prompt) cannot read it and can never prime one. So a device scheduled job is only
viable when **the scheduling identity IS the identity that consented, and that identity is
the mailbox owner.** This was invisible at every lens because each looked at one actor;
it only appears when you compose "interactive consent" with "unattended run as X."

**Resolution (condition, not re-design):** (a) the cache lives under `os.UserConfigDir()`
— `%APPDATA%\mailarchive` on Windows, `~/.config/mailarchive` on Unix — so the primed file
and the run resolve to the same per-user path; (b) `schedule` installs the device task with
the **InteractiveToken / same-user principal** it already uses (MA-170) and checks the cache
is present and owned/readable by the installing user; (c) `graph-app-setup.md` states the
coupling plainly: *sign in interactively as the same OS account the schedule runs as, which
is the mailbox owner.* Folded as **D-C3**.

## Findings → conditions

| ID | Lens | Section (or OMISSION) | Claimed failure (verified) | Verdict | Condition |
|---|---|---|---|---|---|
| F1 | 1, 10 | P8 | "No admin consent" over-claims: a user-consent-disabled tenant needs one-time admin consent | REFUTED | P8 already states it conditional, inline |
| F2 | 6 | P3 | UPN taken from a self-asserted id_token could be spoofed | REFUTED | P3 reads UPN from `GET /me` (Graph), not the token |
| F3 | 1, 6 | P5 / OMISSION | Default cache path keys on tenant+client only; two users on one host, or two mailboxes, collide — one token clobbers the other and a run archives whoever last signed in | CONFIRMED (high) | **D-C1** |
| F4 | 3 | §3.5 / OMISSION | Concurrent runs (interactive + cron, or two schedules) race the cache; last-writer persists a SUPERSEDED (Entra-rotated) refresh token → the other is bricked | CONFIRMED (high) | **D-C2** |
| F5 | 4, 5, 8 | P5, P7 / OMISSION | Consent-vs-run identity + prompt sink (see hidden blocker); hard-coded `~/.config` is wrong on Windows | CONFIRMED (high) | **D-C3** |
| F6 | 5 | P5 | Reusing `readSecret`'s 4 KB bound: a Graph access token + refresh token JSON can exceed 4 KB, so a valid cache is refused | CONFIRMED (medium) | **D-C4** |
| F7 | 5, 9 | §3.2 | Device-mode HTTP client must carry the same header/handshake deadlines as app-only or MA-98 regresses for device | CONFIRMED (low) | **D-C5** |
| F8 | 6 | slice a / OMISSION | Device-code phishing (attacker's code entered by the victim) is named as owing the adversarial pass but the mitigation is not stated in the design | CONFIRMED (medium) | **D-C6** |
| F9 | 6, 7 | §4 / OMISSION | Delegated revocation procedure (user "sign out everywhere" / admin revokes the app) is not documented | CONFIRMED (low) | **D-C6** |
| F10 | 10 | P2, §1 | "Strictly less powerful than the app-only secret" over-claims: less DATA reach per credential, but wider credential DISTRIBUTION (a bearer token on N machines vs one central secret) | CONFIRMED (low) | **D-C7** |
| F11 | 3 | P4 / §3.2 | A slow consent lets the device code expire; the run must fail legibly ("code expired, re-run"), not hang | CONFIRMED (low) | folded into P4 test (MA-D4) |
| F12 | 7 | P7 | Unattended-without-cache and expired-refresh must both fail CLOSED with a remedy, not a half-written archive | CONFIRMED (low) | P7 already states it; test MA-D6/D10 |
| F13 | 2 | §5 | Could the token cache be avoided (re-consent every run)? No — scheduled silent reauth is inherent; the cache is Type-I | REFUTED | inherent to P4/OR1 |

## Conditions (fold into revision 2)

- **D-C1 — Cache is mailbox-scoped and signer-checked.** The token cache carries the
  signer's `userPrincipalName`; the default path is derived per mailbox once known. On
  load, if `-mailbox` is set and the cache's stored UPN differs, **refuse** (never silently
  archive a different signer). Two mailboxes get two caches; the file is plaintext at rest
  at `0600` (stated, not "encrypted").
- **D-C2 — Race-safe write-back.** Before persisting a refreshed token, re-read the cache;
  write (atomic temp+fsync+rename) only a token newer than what is on disk. On an
  `invalid_grant` that a concurrent rotation could explain, reload the cache and retry the
  refresh once before failing. Docs: run **one schedule per mailbox**.
- **D-C3 — Consent identity == run identity (hidden blocker).** Cache under
  `os.UserConfigDir()` (Windows `%APPDATA%`, Unix `~/.config`). `schedule` installs the
  device job with the same-user InteractiveToken principal (MA-170) and checks the cache is
  present and readable/owned by the installing user, refusing with the sign-in remedy
  otherwise. The device prompt writes to the **console/stderr always**, never diverted into
  a `-log FILE`. Docs state the identity coupling.
- **D-C4 — Token-appropriate size bound.** The cache reuses the file-**safety** discipline
  (`Lstat` + `O_NOFOLLOW`, no symlink/FIFO/device, Unix `0o077`-clear, non-empty) but a
  64 KB size bound, not the 4 KB secret bound. Factor the shared checks so `readSecret` and
  the cache reader do not drift.
- **D-C5 — Device deadlines.** The device-auth poll, the token exchange and the Graph
  client all run over the deadline-bounded transport; MA-98 is extended to assert a device
  run fails within the configured deadline against a stalling fake.
- **D-C6 — Adversarial surface documented + gated.** The design and `graph-app-setup.md`
  state the device-code phishing mitigation (the tool initiates and displays its **own**
  code; the owner signs into their own tool; verify the consent screen names `mailarchive`)
  and the delegated revocation procedure. Slice a still files
  `docs/review-graph-delegated-adversarial.md` drilling the token-at-rest + phishing paths.
- **D-C7 — Honest credential trade-off.** Replace "strictly less powerful" with the precise
  claim: delegated has **less data reach per credential** (one mailbox, read-only) but
  **wider credential distribution** (a bearer refresh token per operator machine vs one
  central confidential secret); revocation is per user session / per app.

## Mechanical check

`docs/design-graph-delegated.md` BUILD STATUS is `proposed` (not built), and cites this
review file, which exists — the design-doc gate meta-test (if present) is satisfied for the
proposed state. When a slice lands, its design status flips to `built`/`approved` and must
continue to cite this file plus the slice's own gate artifact.
