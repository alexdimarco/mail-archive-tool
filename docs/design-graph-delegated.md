# Design — Microsoft 365 per-user (delegated) capture, alongside app-only

**Revision:** 2 (2026-09-23). **BUILD STATUS:** built (slices a/b/c) — the 10-lens
pre-code review is filed as `docs/review-graph-delegated-predesign.md`
(GO_WITH_CONDITIONS, D-C1..D-C7 folded into this revision; see §7); the slice-a
adversarial pass (`docs/review-graph-delegated-adversarial.md`, PASS, 0 BLOCKER/HIGH)
and the slice-b friction review (`docs/review-graph-delegated-friction.md`, PASS)
are filed. Tests MA-251..MA-260 (tier U) are green under an unfiltered `go test ./...`;
MA-261 (real-tenant device consent) stays lab-pending. MA rows below use the real
catalog numbers MA-251..MA-261 (rev 1 called them MA-D1..D11).

**Owner rulings (2026-09-23), the inputs to this design:**
- **OR1** — the delegated path uses the **OAuth 2.0 device authorization grant**
  (RFC 8628, "device code"), because the tool's primary surface is a headless CLI
  with scheduled runs and device code needs no redirect listener or loopback port
  and works over SSH.
- **OR2** — delegated **coexists with** app-only; it does not replace it. A new
  `-auth app|device` selector; `app` stays the default and today's behaviour.
- **OR3** — "per user" reaches the **signed-in user's own mailbox only** (`/me`),
  delegated `Mail.Read` — never other mailboxes.

## 1. Problem

`mailarchive graph` has one trust model: the **client-credentials (app-only)**
grant with scope `https://graph.microsoft.com/.default`, redeeming the application
`Mail.Read` permission (`internal/graph/graph.go`, R17). Absent RBAC-for-Applications
scoping that grant reads **every** mailbox in the tenant, and even scoped it is a
tenant-wide app registration a Global Admin must consent.

`docs/graph-app-setup.md` already records the cost: for a tenant where we are **not**
admin (University of Toronto) the app registration + admin consent + RBAC scope must
be done by their central IT, and the stated fallback is a hand-run Purview PST export.
The "massive wide permission" is the blocker. A per-user delegated grant reads only
the signed-in user's own mailbox, is **user-consentable** in most tenants (no admin,
no RBAC), and has **less data reach per credential** than the app-only secret (one
mailbox, read-only) — at the cost of **wider credential distribution** (a bearer refresh
token per operator machine vs one central confidential secret) (D-C7).

## 2. Properties

- **P1 — Two auth modes, one archive.** `graph -auth app` is today's app-only path,
  unchanged and default (OR2). `graph -auth device` runs the device-code delegated
  path against the signed-in user's own mailbox. **Both modes issue only GET requests
  and write the identical archive format** — same MIME, same manifest keys, same
  identity/PhysID (immutable-id) mechanics — so an archive may be started in one mode
  and continued in the other (the store token is seeded from the mailbox id, not the
  auth mode). (reword R17; MA-61..64 unchanged; new: MA-251, MA-259)
- **P2 — Read-only is invariant across modes (R17 core).** The delegated app holds
  delegated **`Mail.Read`** (never `Mail.ReadWrite`/`Mail.Send`) and the client is
  GET-only exactly as today. The `Prefer: IdType="ImmutableId"` header and the whole
  rev-6.2 id-set machinery are byte-for-byte reused. (R17; MA-252)
- **P3 — Own mailbox only, proven at the source (OR3).** Delegated requests address
  **`/me`** (`/me/mailFolders`, `/me/mailFolders/{id}/messages`, `/me/messages/{id}/$value`).
  `-mailbox` is **optional** under `-auth device`; when given it MUST equal the
  signed-in user's `userPrincipalName`, read back from `GET /me?$select=userPrincipalName,id`
  (never a self-asserted, unvalidated id_token) — a mismatch is refused before any walk.
  A device run archives exactly one mailbox: the signer's. (MA-253, MA-257)
- **P4 — Interactive once, then unattended.** First device run prints the
  verification URL + user code to stderr and blocks (bounded by the code's own expiry)
  until the owner consents. On success the refresh token (from `offline_access`) is
  cached; later runs redeem it silently. "Unattended from run 1" is **false** and the
  docs say so; the honest claim is "interactive once, then unattended" (P8, lens 10).
  (MA-254, MA-256)
- **P5 — The token cache is a secret at rest, handled like the client secret.** The
  cache is a JSON `{upn, token}` (**D-C1**: bound to one signer) written to a **regular
  file readable only by the operator** under **`os.UserConfigDir()`** (**D-C3**:
  `%APPDATA%\mailarchive` on Windows, `~/.config/mailarchive` on Unix; default name
  `graph-token-<tenant8>-<client8>[-<mailbox8>].json`; overridable with `-token-cache
  PATH`), **never inside `-out`** (the archive may be a synced OneDrive folder). Reads
  reuse the **shared** file-safety discipline `util.ReadSecureFile` (**D-C4**: `Lstat` +
  `O_NOFOLLOW`, no symlink/FIFO/device, Unix mode `0o077` clear else refused naming
  `chmod 600`) with a **64 KB** bound (a Graph token JSON exceeds `readSecret`'s 4 KB),
  the plaintext-at-rest posture stated (not "encrypted"). Writes are **race-safe**
  (**D-C2**): atomic temp-file + fsync + rename, and only when the new token is newer
  than what is on disk (Entra rotates the refresh token on each use); an `invalid_grant`
  a concurrent rotation could explain reloads the cache and retries the refresh once. A
  crash fails toward re-consent, never toward an accepted-but-corrupt token. On load, a
  cache whose stored `upn` differs from `-mailbox` is refused (never silently archives a
  different signer). (MA-255, MA-258)
- **P6 — A device run takes no client secret.** `-auth device` is a **public client**
  (Azure "Allow public client flows" = yes); it has no secret. Passing
  `-client-secret-file`/`-client-secret-env` with `-auth device` is **refused** naming
  why (so a secret is never fed to a flow that ignores it, and the operator is not
  misled). `-tenant` and `-client-id` are still required. (MA-257)
- **P7 — Scheduled device jobs need a primed cache, checked at schedule time.** A
  device prompt can never run unattended, so a scheduled `graph -auth device` job
  requires `-token-cache PATH`, and `schedule` validates at install time that the cache
  exists, passes the P5 file check, parses, and carries a refresh token — mirroring the
  secret-file check (SC10). A first-ever unattended device run with no usable cache is
  refused naming the remedy: "run `mailarchive graph -auth device …` once interactively
  to sign in." `status` maps an `invalid_grant`/expired-refresh failure to a re-consent
  remedy line, exactly as it maps an expired app secret. (MA-256, MA-260)
- **P8 — Honest, conditional consent claim.** "No admin consent" is **conditional**:
  most tenants let a user self-consent to delegated `Mail.Read`, but a tenant with user
  consent disabled still needs a **one-time admin consent** for the delegated
  permission — with **no RBAC and no per-mailbox scoping**, a single tenant-wide "users
  may read their own mail with this app" grant, still far narrower than application
  `Mail.Read`. This condition is stated inline in `graph-app-setup.md`, next to the
  claim. (lens 10)

## 3. Mechanism

### 3.1 Addressing (`/me` vs `/users/{upn}`)
`graph.Client` gains one addressing helper; every `"/users/" + url.PathEscape(userID)`
becomes `c.userSeg(userID)`:

```go
func (c *Client) userSeg(userID string) string {
    if c.meMode { return "/me" }               // delegated (OR3)
    return "/users/" + url.PathEscape(userID)  // app-only, unchanged
}
```

`meMode` is set by the constructor for the device path. Method signatures keep their
`userID` parameter (app-only iterates several mailboxes on one client); in device mode
`userID` is the signer's own UPN and `userSeg` ignores it. This is the whole endpoint
change — Folders/Messages/MIME/WellKnownFolderID bodies are otherwise untouched, so
the walk, exclusion, paging, throttling and immutable-id logic are shared verbatim.

### 3.2 Device flow + token source (no new dependency)
`golang.org/x/oauth2 v0.24.0` already provides `Config.DeviceAuth` /
`Config.DeviceAccessToken` (`deviceauth.go`). The delegated config:

```go
conf := &oauth2.Config{
    ClientID: cfg.ClientID,
    Endpoint: oauth2.Endpoint{
        AuthURL:       ".../{tenant}/oauth2/v2.0/authorize", // unused by device flow
        TokenURL:      ".../{tenant}/oauth2/v2.0/token",
        DeviceAuthURL: ".../{tenant}/oauth2/v2.0/devicecode",
    },
    Scopes: []string{"https://graph.microsoft.com/Mail.Read", "offline_access"},
}
```

- **First run, no cache:** `da, _ := conf.DeviceAuth(ctx)`; print `da.VerificationURI`
  + `da.UserCode` to stderr; `tok, _ := conf.DeviceAccessToken(ctx, da)` (polls at
  `da.Interval`, bounded by `da.Expiry`); persist `tok` (P5).
- **Later runs:** load `tok`; `ts := oauth2.ReuseTokenSource(tok, conf.TokenSource(ctx, tok))`.
  The source auto-refreshes with the refresh token; a **write-back wrapper** persists
  the token whenever `ts.Token()` returns one whose refresh/access/expiry differ from
  what is on disk (Entra rotates the refresh token on each use), atomically (P5). The
  HTTP client is `oauth2.NewClient(ctx, ts)` over the same deadline-bounded transport
  the app-only path builds today (MA-98 deadlines unchanged).
- **Refresh failure** (`invalid_grant`: revoked / expired-past-inactivity / CA policy)
  surfaces as the run's error with a re-consent remedy; `status` maps it (P7).

The test endpoints (device-auth, token, Graph) are overridable exactly as `TokenURL`
is today, so the whole flow is CI-tested against an in-process fake with no network.

### 3.3 CLI surface
`graphFlags` adds `-auth` (`app` default) and `-token-cache`. Validation:
- `-auth app` — unchanged (tenant, client-id, secret required).
- `-auth device` — tenant + client-id required; secret flags refused (P6); `-mailbox`
  optional and, if present, exactly one, matched to the signer (P3); `-token-cache`
  defaulted (P5) or explicit.
- Anything else for `-auth` → refuse naming the two valid values.

`RunGraph` selects the client constructor by mode; the mailbox loop runs once for the
signer under `-auth device` (the UPN read back from `/me`). Everything downstream —
exporter, manifest, index, history, gone-sweep — is identical.

### 3.4 Schedule + status
- `schedule` (job.go) learns that a `graph` job may carry `-auth device -token-cache
  PATH`; at install time it runs the P5 cache check + refresh-token presence (the
  device analogue of the secret-file check), refusing with the remedy if unprimed.
  A device job carries no secret file.
- `status` adds the re-consent remedy mapping (P7) beside the existing expired-secret
  remedy; no new posture, same GREEN/WARN/RED partition (R18 untouched).

### 3.5 Crash order (durability)
The only new durable write is the token cache. Order: obtain token → temp write →
fsync → rename over the cache. A crash before rename keeps the prior valid token (run
re-refreshes next time); a crash after rename has the new token. There is no archive
state coupled to the token, so token-cache failure never corrupts the manifest/index —
it fails the run closed at auth time, before the lock-holding walk writes anything.

## 4. Invariants touched

- **R17 reworded (broadened, not weakened).** Today: "the app holds the read-only
  `Mail.Read` **application** permission." Proposed: "the Graph source — **app-only OR
  delegated per-user** — issues only GET requests (app-only application `Mail.Read`, or
  delegated `Mail.Read` for the **signed-in user's own mailbox**), so a mailbox is never
  modified," with the complete/incremental clauses unchanged. The read-only GET-only
  core and the whole incremental/id story are preserved; MA-61..64 and MA-241..250 stay
  green under `-auth app`.
- **New MA rows (proposed, tier U unless noted; folded into `docs/scenario-catalog.md`
  under S20/S38 at review time):**
  - **MA-251** device-mode client addresses `/me` (folders/messages/MIME/well-known) and
    issues only GET — proven on the fake Graph; app-mode still addresses `/users/{upn}`.
  - **MA-252** the device path sends `Prefer: IdType="ImmutableId"` and honors
    Preference-Applied identically (PhysID set only when honored).
  - **MA-253** `-mailbox` under `-auth device` must equal `/me` `userPrincipalName`; a
    mismatch is refused (assure.Refused) with no walk; absent `-mailbox` archives the signer.
  - **MA-254** device first-run prints the verification URI + user code and blocks on
    `DeviceAccessToken`; a fake device+token endpoint drives consent → a cached token.
  - **MA-255** the token cache read enforces the `readSecret` discipline (symlink/FIFO/
    device refused; Unix `0o077`-clear else `chmod 600` refusal; size-bound; non-empty).
  - **MA-256** an unattended device run with no usable cache is refused naming the
    interactive-sign-in remedy; a primed cache runs silently (positive twin).
  - **MA-257** `-auth device` with a secret flag is refused (P6); `-auth` with an unknown
    value is refused naming `app|device`.
  - **MA-258** token write-back is atomic and round-trips (a rotated refresh token is
    persisted; a torn temp file never replaces a valid cache).
  - **MA-259** an archive captured `-auth app` continues `-auth device` (same mailbox)
    with zero re-download (token-independent store key; incremental fast-path holds).
  - **MA-260** `schedule` validates a device job's token cache at install time (unprimed
    → refused with remedy); `status` maps `invalid_grant` to a re-consent remedy line.
  - **MA-261** (L, **pending**, real tenant) end-to-end device-code consent + delegated
    `Mail.Read` walk against a live M365 mailbox; validates the consent screen names
    `mailarchive` and user-consent-disabled tenants fall back to one-time admin consent.

## 5. Build slices (proposed) + tests

- **Slice a — `internal/graph`:** `userSeg`/`meMode`; device token source with the
  test-overridable device-auth/token endpoints; token-cache load/store with the secret
  discipline and atomic write-back; `/me` UPN read-back. Tests MA-251, 252, 254, 255, 258.
  **Owes the adversarial pass** (`docs/review-graph-delegated-adversarial.md`): a new
  bearer secret at rest + the device-code phishing surface.
- **Slice b — `internal/app` + `cmd/mailarchive`:** `-auth`/`-token-cache` flags,
  mode-selecting `RunGraph`, the P3/P6 refusals, the interactive prompt. Tests MA-253,
  256, 257, 259. **Owes the friction review** (`docs/review-graph-delegated-friction.md`):
  the first-run consent ceremony as the owner walks it.
- **Slice c — schedule/status + docs:** MA-260; `graph-app-setup.md` delegated section
  (the P8 conditional-consent note, the app-registration steps: public client, delegated
  `Mail.Read` + `offline_access`, no secret) and README. Lab row MA-261 stays pending.

Each slice is a separate commit naming its gate and shipping prove-fail → prove-pass.

## 6. Honesty of claims

- **Proven once built:** read-only (GET-only), `/me`-only addressing, the P3/P6 refusals,
  the token-cache secret discipline, atomic write-back, incremental cross-mode continuity
  — all tier-U against the in-process fake (MA-251..MA-260).

## 7. Conditions folded (rev 2, from `docs/review-graph-delegated-predesign.md`)

- **D-C1 — cache is mailbox-scoped + signer-checked.** Cache JSON carries `upn`; default
  path adds `-<mailbox8>` when `-mailbox` is given; on load a cache whose `upn` ≠ `-mailbox`
  is refused. Plaintext at rest at `0600` (stated, not "encrypted"). (P5; MA-255)
- **D-C2 — race-safe write-back.** Re-read before write; persist only a token newer
  (later `Expiry`) than on disk; on an `invalid_grant` a concurrent rotation could
  explain, reload the cache and retry the refresh once. Docs: one schedule per mailbox.
  (P5; MA-258)
- **D-C3 — consent identity == run identity (hidden blocker).** Cache under
  `os.UserConfigDir()`; `schedule` installs the device task with the same-user
  InteractiveToken principal (MA-170) and checks the cache is readable by the installing
  user; the device prompt writes to the console/stderr **always**, never a `-log` file.
  (P5, P7; MA-260)
- **D-C4 — token-appropriate size bound.** Shared `util.ReadSecureFile` carries the
  file-safety checks (Lstat/O_NOFOLLOW/mode/regular) so `readSecret` and the cache reader
  cannot drift; the cache uses a 64 KB bound, `readSecret` keeps 4 KB. (P5; MA-255)
- **D-C5 — device deadlines.** The device-auth poll, token exchange and Graph client all
  run over the deadline-bounded transport; MA-98 style deadline coverage extends to a
  device run against a stalling fake (folded into MA-254).
- **D-C6 — adversarial surface documented + gated.** `graph-app-setup.md` states the
  device-code phishing mitigation (the tool shows its **own** code; verify the consent
  screen names `mailarchive`) and the delegated revocation procedure; slice a files the
  adversarial pass.
- **D-C7 — honest credential trade-off.** §1/P2 say delegated has **less data reach per
  credential** (one mailbox, read-only) but **wider credential distribution** (a bearer
  refresh token per operator machine vs one central confidential secret).
- **Conditional (condition stated inline, P8):** "no admin consent needed" holds only
  where the tenant permits user consent; otherwise one-time admin consent, still no RBAC
  and still own-mailbox-only.
- **Lab-validation-pending:** real-tenant device consent, the consent-screen app name, and
  the user-consent-disabled fallback (MA-261) — never labelled proven until run on a live
  tenant. First-run is interactive; the docs never claim unattended-from-zero.
