# Adversarial pass — MailArchive Desktop sign-in + token custody + loopback control surface (slice S2)

**Reviewed:** the built code — `internal/desktop/dashboard.go` (the sign-in card,
`clearSignin`, `dashCSP`/`dashboardHeaders`), `internal/server` `GuardLocalPOST`
(`IsLoopback` / `originIsLoopback` / `tokenOK` / `NewCSRFToken`), the token-store seam
`internal/graph/delegated.go` (`tokenBackend` load/put/clear, `SignedIn`, `ClearSignIn`),
and `internal/graphconfig/secretstore_windows.go` (`CredWriteW`/`CredReadW`/`CredDeleteW`,
target `mailarchive:graph-token:<hash>`) — against the surface S2 adds: a **bearer refresh
token persisted in the OS vault** and a **loopback web control surface that writes** (sign-in,
clear-sign-in). Personas: outside aggressor (browser CSRF / DNS-rebind), malicious/curious
local user, integrity/concurrency, device-code phishing. Builds on the delegated-auth
adversarial pass (`review-graph-delegated-adversarial.md`) — token-at-rest findings there
(A2/A4/A6/A7/A8) carry; this pass covers the **dashboard** surface and the **vault** backing.
**9 findings raised · 9 verified · 0 BLOCKER · 0 HIGH.**

| # | Persona | Claim | Verdict | Evidence / disposition |
|---|---|---|---|---|
| D1 | outside | A web page the signed-in user visits POSTs to `http://127.0.0.1:8097/api/clear-signin` (or `/api/capture`, `/api/schedule-install`) and drives the dashboard | REFUTED (mitigated) | Every state-changing POST passes `GuardLocalPOST`: it requires `X-CSRF-Token` to equal the per-process token (`tokenOK`, `subtle.ConstantTimeCompare`, empty rejected). A cross-site page cannot read that token — the page is served no-store under `dashCSP` (`script-src 'self'`, `connect-src 'self'`), and a cross-origin `fetch` cannot set a custom header without a CORS preflight the dashboard never grants; a form POST can't set `X-CSRF-Token` at all. A bad/untokened request → **403, side effect absent** (MA-274/276/279). |
| D2 | outside | DNS-rebind: the attacker points a hostname they control at `127.0.0.1` so the browser treats their origin as same-site and reaches the dashboard | REFUTED | `GuardLocalPOST` requires `IsLoopback(r.Host)` — the rebind carries the attacker's hostname in the `Host` header, which is not `localhost`/`127.0.0.1`/`::1`, so the POST is refused before any handler logic. A present `Origin` must also be a loopback origin (`originIsLoopback`). |
| D3 | local | Another user (or process) on the machine reads the refresh token at rest | REFUTED for a peer user; CONFIRMED Type-I for admin/SYSTEM (documented) | Windows: `CredWriteW` writes a **generic** credential in the signed-in user's own Credential Manager vault (`credPersistLocalMachine`, per-user, DPAPI-protected); a different non-admin user cannot read it. Elsewhere: `util.WriteFileAtomic0600` + `ReadSecureFile` (refuses a group/world-readable file). Admin/SYSTEM (Windows) or root (elsewhere) can read it — same posture as the client-secret file, stated plaintext-at-rest (DC4 / graph-delegated A2). |
| D4 | local/outside | The refresh/access token leaks across the HTTP boundary — a response body, the Activity-log ring, or an error | REFUTED | The vault/file blob never crosses HTTP: `SignedIn` returns only the UPN **label** + a bool; `clearSignin` returns `{ok}`/`{error}`; the Activity ring streams the run logger, which prints `Signed in as <upn>` and the device **user code** (short-lived, single-use, meant to be shown) — never the refresh/access token (graph-delegated A6). `d.cfg.load()` surfaces config, not the token. |
| D5 | phishing | The device code shown on the dashboard is an attacker's code, signing the attacker into the victim's mailbox | REFUTED (Type-I residual documented) | The dashboard initiates its **own** device code via `graph.NewDelegated` and only ever redeems the code from that same `DeviceAuth`; it never accepts an externally supplied code (predesign F8; graph-delegated A1). The copy tells the user to approve only the code the page just showed, for the `mailarchive` app. The inherent RFC-8628 social-engineering residual is Type-I. |
| D6 | local same-user | A local process running **as the signed-in user** drives the dashboard POSTs or reads the vault entry | CONFIRMED (Type-I, DC4 — no new boundary) | The per-process CSRF token lives in that user's process memory and the vault entry is in that user's vault; a process with the user's rights can already run the engine directly and read the user's own credentials. `GuardLocalPOST`'s doc comment and ux-contract X9 / design DC4 state this boundary explicitly; no stronger claim is made. |
| D7 | integrity/concurrency | A crafted or racing clear-sign-in corrupts or wrongly removes the token mid-capture | REFUTED | `clearSignin` is `GuardLocalPOST`-gated — a bad request is 403 with the sign-in **untouched** (MA-274). `ClearSignIn` deletes only our own entry (`CredDeleteW` on our target / `os.Remove` of our path) and tolerates not-exist. A capture in flight holds its own in-memory token source; worst case the **next** capture re-consents. Fails closed, no corruption. |
| D8 | outside | The second loopback port (the reader, `:8099`) widens the write surface | REFUTED | `ReaderHandler` embeds `server.New` **unchanged** (R19 GET-only, `ArchiveCSP`), binds loopback only, and exposes no control action; it is a separate read surface (predesign F9). Both ports refuse a non-loopback bind (MA-271, DC7). |
| D9 | outside | The CSRF token is guessable or compared in variable time | REFUTED | `NewCSRFToken` = `randomToken()` (crypto/rand); `tokenOK` compares with `subtle.ConstantTimeCompare` and rejects the empty token. The token is minted per process, not persisted. |

**Verdict: PASS (ship).** S2 adds no BLOCKER/HIGH. The write surface is loopback + CSRF +
Origin + Host guarded (anti-DNS-rebind), failing closed with the side effect absent on any
bad request. The vault move makes the "sign-in stays in Windows Credential Manager" claim
**true** (per-user DPAPI vault; `0600` file elsewhere), with the two residuals — admin/root
read (D3) and same-user local process (D6) — Type-I and documented under DC4/X9, not data
loss. Token-at-rest hardening (atomic write, `O_NOFOLLOW`, size bound, no-log) is inherited
from the delegated pass and unchanged. Covering tests: MA-273 (token-store seam, cross-platform
in-memory store), MA-274 (sign-in state + guarded clear), MA-276 (guarded settings). prove-fail
→ prove-pass records are in the S2 commit.

**Addendum (post-ship fix, MA-283):** the vault blob now persists a **slim token** — refresh
token + type + expiry, **no access token** (the access token is a ~2 KB Graph JWT that
overflowed Credential Manager's 2560-byte `CRED_MAX_CREDENTIAL_BLOB_SIZE` and surfaced as
"The stub received bad data" / RPC_X_BAD_STUB_DATA). This strengthens D3/D4: less bearer
material at rest (the short-lived access token is never persisted, only regenerated from the
refresh token), with no weakening — the long-lived refresh token is protected exactly as
before. The `0600` file cache is unchanged (DC2).
