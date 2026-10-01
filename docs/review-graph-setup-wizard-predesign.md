# Pre-code design review — Graph setup wizard

**Design reviewed:** `docs/design-graph-setup-wizard.md` revision 1 (2026-10-01).
**Procedure:** the 10-lens battery of `../assurance-kit/process/design-review.md`, run
finder → skeptic → rescue, single reviewer. **10 findings raised · 8 confirmed · 2
refuted.** Rescue surfaced the key simplification that dissolves the design's biggest
risk (the R19 write-surface reword).

**Verdict: GO_WITH_CONDITIONS.** Conditions **W-C1..W-C7** fold into design rev 2. No
lens returned NO_GO.

## Rescue pass — the simplification that removes the R19 risk

Rev 1 proposed mounting the wizard into the read-only reader `serve` behind `-admin`.
That is what forced an R19 reword and dragged in serve's archive/index coupling. The
rescue move: **make the wizard a standalone loopback `mailarchive setup` command**
(the handler still lives in the `server` package, sharing the restyled design system
and `secureHeaders`), and **leave the reader `serve` completely untouched**. The
reader keeps its form-action-none, read-only posture; R19 needs **no change**; serve's
"needs an archive" coupling is irrelevant to a pre-archive setup step. One surface,
smaller blast radius. (W-C1)

## Findings → conditions

| # | Lens | Claim | Verdict | Condition |
|---|---|---|---|---|
| F1 | 2, 6 | Adding a config-write route to the read-only reader `serve` widens a hardened surface and forces an R19 reword | CONFIRMED | **W-C1** (dissolve: standalone `mailarchive setup`; reader serve untouched; R19 unchanged) |
| F2 | 6 | A browser page on another site can POST to `127.0.0.1` and write credentials (CSRF) | CONFIRMED | **W-C2** |
| F3 | 6 | DNS-rebinding bypasses an Origin-only check | CONFIRMED | **W-C2** (also require a loopback Host header) |
| F4 | 6, 10 | The secret could leak via request logging or a GET echo | CONFIRMED | **W-C3** |
| F5 | 3 | Reconfiguring to a new tenant/client orphans the old Credential Manager entry | CONFIRMED (low) | **W-C4** |
| F6 | 10 | "Never at rest in the clear" over-reads the non-Windows file store (it is plaintext `0600`) | CONFIRMED | **W-C4** (state plaintext-0600, same as today's secret file) |
| F7 | 5, 6 | The reader's `uiCSP` has `form-action 'none'`, so a wizard form would be blocked | CONFIRMED | **W-C3** (setup CSP + fetch, not a form submit) |
| F8 | 8 | The wizard must not break existing `-client-secret-file`/env callers | CONFIRMED | **W-C5/W-C6** (flags/env take precedence; config/store are fallback) |
| F9 | 1 | The wizard "signs in" — scope creep | REFUTED | It only configures values; device sign-in stays the separate `graph -auth device` step (stated P6) |
| F10 | 9 | Credential Manager needs a new dependency | REFUTED | `golang.org/x/sys v0.34.0` is already a direct dep; CredMan is pure-Go syscalls |

## Conditions (fold into rev 2)

- **W-C1 — Standalone setup surface.** The wizard is `mailarchive setup` — a
  loopback-only web server exposing ONLY the setup page/API (handler in the `server`
  package). The reader `serve` is NOT modified and gains no write route; **R19 is
  unchanged** (no reword). `setup` refuses a non-loopback bind (reuse `IsLoopback`).
- **W-C2 — CSRF + anti-rebind.** POST requires a per-process CSRF token embedded in
  the page; the handler also checks the `Origin` (when present) is the loopback origin
  AND the `Host` header is a loopback host. Any mismatch is refused.
- **W-C3 — Secret is write-only.** `/setup` never logs the request body; no GET ever
  returns the secret (it reports "configured: yes/no" only); the secret is held in
  memory for the request then dropped; the config file never contains it. The setup
  page runs under its own CSP (`script-src 'self'; connect-src 'self'`) and POSTs via
  `fetch`, not a form submit.
- **W-C4 — Secret store.** Windows Credential Manager (generic credential, the user's
  own vault) on Windows — a lab row. Elsewhere, an atomic `chmod 600` file via the
  `util.ReadSecureFile` discipline, documented as **plaintext at rest** (same posture
  as today's `-client-secret-file`). Reconfiguring to a different tenant/client
  `Delete`s the superseded entry.
- **W-C5 — Config file.** `graph-config.json` (`0600`, atomic, versioned) under
  `os.UserConfigDir()`, never inside `-out`. Explicit `-tenant`/`-client-id`/`-auth`
  flags and `-client-secret-file`/env always take precedence over it (coexistence).
- **W-C6 — Capture consumes it.** `graph` fills unset tenant/client/auth from the
  config and, for app mode with no file/env secret, reads the secret from the store;
  `schedule` validates the resolved result at install time. The device path is
  unchanged.
- **W-C7 — Honesty.** The Windows Credential Manager store and the wizard end-to-end
  are **lab-pending** (Windows, like MA-79/95); the file store, the CSRF/anti-rebind
  and loopback refusals, write-without-echo, and config/capture wiring are tier-U on
  Linux CI.

## Mechanical check
`docs/design-graph-setup-wizard.md` BUILD STATUS is `proposed` and cites this review,
which exists. On build it flips to `built` and continues to cite this file plus the
slice-a adversarial pass and slice-b friction review.
