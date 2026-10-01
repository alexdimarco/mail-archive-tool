# Design — Graph setup wizard: admin enters tenant / app-registration values in the local UI

**Revision:** 2 (2026-10-01). **BUILD STATUS:** built — the 10-lens pre-code review is
filed as `docs/review-graph-setup-wizard-predesign.md` (GO_WITH_CONDITIONS, W-C1..W-C7
folded here); the slice-a adversarial pass (`docs/review-graph-setup-wizard-adversarial.md`,
PASS, 0 BLOCKER/HIGH) and the slice-b friction review
(`docs/review-graph-setup-wizard-friction.md`, PASS) are filed. Key change from rev 1
(W-C1): the wizard is a **standalone `mailarchive setup` loopback command**, and the
read-only reader `serve` is left untouched — so **R19 is unchanged**. Tests MA-262,263,
265,266,267,268,269,270 (tier U) are green under an unfiltered `go test ./...`; MA-264
(Windows Credential Manager) is lab-pending.

**Owner rulings (2026-10-01):**
- **OR1** — the wizard configures **both** auth modes; the admin picks: **device**
  (tenant + client-id, no secret) or **app-only** (adds a client secret). The secret
  field appears only for app-only.
- **OR2** — the client secret is stored in **Windows Credential Manager** on Windows,
  and in a `chmod 600` file on Linux/macOS (the existing secret-file discipline).
- **OR3** — the wizard lives in our own local web UI (the restyled `serve` design
  system), not in Steve's external `MailArchiveDesktop.exe` wrapper.
- **OR4** — the wizard clarifies which identifier to use (the two-Object-ID trap):
  Application (client) ID + Directory (tenant) ID, never an Object ID.

## 1. Problem

Today the Graph capture credentials reach the tool only as flags/files on the
`graph` command line (`-tenant`, `-client-id`, `-client-secret-file`/env, or the
device `-token-cache`). An admin standing up a mailbox has nowhere guided to enter
the **tenant id** and **application (client) id** from the Entra app registration,
and the client secret has only the plaintext-file path. Steve's external desktop
wrapper added a nice entry surface but bakes the tenant/app ids in as constants and
lives outside this repo. We want a guided, in-repo **setup wizard** that collects the
app-registration values, clarifies the Object-ID trap at the point of entry, and
stores the secret safely (Credential Manager on Windows).

## 2. Properties

- **P1 — Guided entry, two modes (OR1).** A local web wizard collects Directory
  (tenant) ID, Application (client) ID, and the auth mode. For **device** it stops
  there (public client, no secret). For **app-only** it also takes the client secret.
  Each field carries inline help, including the Object-ID clarification (OR4). (new
  MA rows; S-series)
- **P2 — The secret is write-only and never at rest in the clear on Windows (OR2).**
  On POST the secret is held only in memory for the request, written to the secret
  store, then dropped; it is **never** logged, **never** returned by any GET, and
  **never** written to the config file. Store: Windows Credential Manager
  (`CRED_TYPE_GENERIC`, per-user `CRED_PERSIST_LOCAL_MACHINE`→ actually current-user)
  on Windows; a `chmod 600` regular file (the `util.ReadSecureFile`/atomic-write
  discipline) elsewhere. (new MA rows)
- **P3 — Non-secret config is a plain file.** Tenant, client-id, auth mode (and the
  device token-cache path / mailbox) are written to
  `<os.UserConfigDir>/mailarchive/graph-config.json` (`0600`, never inside an
  archive `-out`). It round-trips and is additively versioned. (new MA rows)
- **P4 — The wizard is a standalone loopback command; the reader `serve` is untouched
  (W-C1, R19 unchanged).** The setup surface is `mailarchive setup` — its own
  loopback-only web server exposing ONLY the wizard (the handler lives in the `server`
  package, sharing the restyled design system and `secureHeaders`). It refuses a
  non-loopback bind (reuse `IsLoopback`). The reader `serve` gains NO write route, so
  R19's read-only posture is preserved verbatim. (new MA rows)
- **P5 — The write surface is CSRF-hardened.** The POST is guarded by (a) a
  per-process CSRF token embedded in the wizard page and required on submit, and (b)
  an Origin/Referer check against the loopback origin, so a page in the admin's
  browser on another site cannot drive `127.0.0.1` to write credentials. The wizard
  uses `fetch()` (same-origin, `connect-src 'self'`) under a setup-specific CSP that
  grants `script-src 'self'`/`connect-src 'self'` while the reader keeps its
  form-action-none policy. (new MA rows)
- **P6 — The capture command consumes what the wizard wrote.** `graph` fills missing
  `-tenant`/`-client-id`/`-auth` from the config file, and app mode reads the secret
  from the store (Credential Manager / file) when no `-client-secret-file`/env is
  given — so a wizard-configured endpoint runs `mailarchive graph` (and the schedule)
  with no secret on the command line. (new MA rows)
- **P7 — Object-ID clarity at the point of entry (OR4).** The client-id field's help
  states: "Application (client) ID — App registration → Overview. NOT the Object ID
  (neither the app registration's nor the enterprise application's)." Mirrors the
  `graph-app-setup.md` table.

## 3. Mechanism

### 3.1 `internal/graphconfig` (new)
- `Config{Tenant, ClientID, Auth string; TokenCache, Mailbox string; Version int}`
  with `Load(path)`/`Save(path)` (atomic `0600`, `os.UserConfigDir` default path).
- `SecretStore` interface: `Set(account, secret) error`, `Get(account) (string, error)`,
  `Delete(account) error`, `Name() string`. `account` is derived from tenant+client
  (`graph:<tenant8>-<client8>`), so two registrations don't collide.
  - `secretstore_windows.go` (`//go:build windows`): Credential Manager via
    `golang.org/x/sys/windows` `CredWrite`/`CredRead`/`CredDelete` (already a dep;
    pure-Go syscalls, no cgo). Target name `mailarchive:graph:<…>`, `CRED_TYPE_GENERIC`,
    current-user persistence.
  - `secretstore_other.go` (`//go:build !windows`): a `chmod 600` file under the
    config dir, written atomically and read through `util.ReadSecureFile`.

### 3.2 The wizard surface (server package)
- `server.SetupHandler(cfgPath string, store graphconfig.SecretStore) http.Handler`
  with `GET /setup` (the restyled wizard page + a minted CSRF token + `/setup.js`)
  and `POST /setup` (validate → persist config → store/clear secret → respond
  `{ok, configured}`), never echoing the secret.
- Exposed ONLY by a thin `mailarchive setup` command that starts a loopback-only
  server serving ONLY `/setup` + `/setup.js` (no archive/index needed). The reader
  `serve` is not modified (W-C1).
- A setup-specific CSP: `default-src 'none'; script-src 'self'; connect-src 'self';
  style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'; base-uri 'none';
  form-action 'none'` (POST via fetch, not a form submit).

### 3.3 Capture wiring (`cmd/mailarchive` + `internal/graph`)
- `runGraph` loads `graphconfig` and fills unset `-tenant`/`-client-id`/`-auth`; for
  `-auth app` with no `-client-secret-file`/env, reads the secret from the store.
- `schedule` validates the same (a device job's token cache, or that an app job can
  resolve a secret from the store) at install time.

### 3.4 Crash order / durability
- Config write is atomic (temp+fsync+rename). The secret write to the store is a
  distinct step; order is config-first then secret, and a failed secret write surfaces
  as an error (the admin re-enters it) — never a partial that looks complete. No
  archive state is coupled to either.

## 4. Invariants touched
- **R19 unchanged (W-C1).** The reader `serve` gains no write route; the new setup
  surface is a separate `mailarchive setup` command. R19 still describes the read-only
  reader/archive policies as-is. The setup surface's own guarantees (loopback-only,
  CSRF/anti-rebind, write-only secret) are carried by its new MA rows.
- **New scenario (S-series) + MA rows (tier U unless noted):** config round-trip;
  file secret store set/get/delete + its secure-file discipline (U); Credential
  Manager store (**L**, Windows lab); wizard POST writes config + stores secret and
  never returns it (U); CSRF/Origin refusal (U, `assure.Refused`); loopback-only mount
  refusal on a non-loopback bind (U); `graph` fills from config + reads the secret
  from the store (U); the wizard page carries the Object-ID help + mode toggle (U).

## 5. Build slices
- **Slice a — `internal/graphconfig`:** config + secret-store interface + file impl
  (+ Windows CredMan impl, lab row). Tests: config round-trip, file store discipline.
  **Owes the adversarial pass** (secret at rest + store).
- **Slice b — `internal/server` setup surface + `mailarchive setup`:** wizard page
  (restyled), POST handler, CSRF/Origin, loopback-only, setup CSP. Tests: the
  refusals + the write-without-echo. **Owes the friction review** (the admin walk).
- **Slice c — capture wiring + docs + catalog:** `graph`/`schedule` consume config +
  store; `graph-app-setup.md` wizard section; catalog rows; R19 reword.

## 6. Honesty of claims
- **Proven once built (tier U, CI on Linux):** config round-trip, the file secret
  store discipline, the CSRF/Origin + loopback-only refusals, write-without-echo,
  `graph` reading config+file-secret.
- **Lab-pending (Windows):** the Credential Manager store (CredWrite/Read/Delete) and
  the wizard end-to-end on a real Windows endpoint — marked L, like the other Windows
  rows (MA-79/95).
