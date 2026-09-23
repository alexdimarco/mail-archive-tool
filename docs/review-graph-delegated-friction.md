# Friction review — delegated (device-code) Graph capture (slice b)

**Reviewed:** the operator ceremony of `-auth device`, walked as the actor it exists
for — a **non-admin mailbox owner** archiving their own M365 mailbox, and the same
person scheduling it. Classification per `../assurance-kit/process/friction-review.md`:
**Type I** (security-inherent — document it) vs **Type II** (design-choice — fix it).

## The walk (interactive first run)

1. Someone registers a public client once (admin, or the user via self-service) —
   delegated `Mail.Read` + `offline_access`, no secret. *(Type I — a one-time app
   registration is inherent to Graph; the same as app-only, minus the secret/RBAC.)*
2. `mailarchive graph -auth device -out ./archive -tenant T -client-id C`.
3. The tool prints a URL + short code to the console. Open it, sign in, enter the
   code, approve. *(Type I — proving you own the mailbox needs one interactive
   sign-in. This is the whole ceremony.)*
4. The archive runs; the sign-in is saved. Later runs need no prompt.

Steps handled: **1 secret at rest** (the refresh token, `0600`, same discipline as
the client secret), **0 out-of-band transfers** (no secret to hand over — the win
over app-only), **1 interactive step** (the sign-in), all failures legible.

## Findings

| # | Type | Point | Disposition |
|---|---|---|---|
| FR1 | II (FIXED pre-code) | A schedule running as a different OS user than the one who signed in cannot read the `0600` cache, and could never prime one | Fixed by D-C3: cache under `os.UserConfigDir()`; `schedule` validates the cache at install time and refuses with the sign-in remedy; docs say "sign in as the account the schedule runs as." (MA-260) |
| FR2 | II (FIXED) | A first-ever unattended run would block forever on a prompt no one answers | Fixed: `Unattended` with no usable cache refuses immediately naming the interactive-sign-in remedy (MA-256); the device prompt is never reached unattended. |
| FR3 | II (FIXED) | A device prompt hidden inside a `-log FILE` would leave an interactive user staring at nothing | Fixed: the prompt writes to `os.Stderr` (the console) directly, never the logger/`-log` (D-C3). |
| FR4 | I | The sign-in expires (refresh-token inactivity ~90 days, or password change / admin revoke) and a scheduled run then fails | Documented: `status` maps the auth failure to a "re-run `-auth device` to sign in again" remedy (MA-256); `graph-app-setup.md` states the expiry. Inherent to delegated tokens. |
| FR5 | I | The one-time browser sign-in itself | Inherent (you must prove mailbox ownership); it is one step, on any device, no redirect port. This is the point of choosing device code (OR1). |
| FR6 | II (LOW, accepted) | The prompt says "open <URL> and enter <code>" but does not name WHICH mailbox is being signed in | Accept for now: device mode archives whoever signs in, and a `-mailbox` mismatch is caught right after (A9). A future nicety could echo the intended address; not a ship blocker. |
| FR7 | II (LOW, accepted) | The default cache path is an opaque hashed filename | Accept: it is derived from tenant/client(/mailbox) so two mailboxes don't collide, is documented, and `-token-cache PATH` overrides it for anyone who wants a legible name. |

## Verdict

**PASS (shippable).** The two frictions that would have bitten — the consent/run
identity coupling (FR1) and the unattended-prompt trap (FR2) — were fixed pre-code
as D-C3/P7 and are structurally enforced (schedule-time check + unattended refusal),
each with a covering test. The remaining frictions are Type-I (documented) or LOW
niceties. The device path is strictly *less* ceremony than app-only for a non-admin:
no secret to mint, transfer, protect, or rotate — one browser sign-in instead.
