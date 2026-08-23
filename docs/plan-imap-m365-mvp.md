# Plan: live IMAP fetch — Microsoft 365 MVP

Scope: prove the **OAuth2 → IMAP → existing exporter** loop end-to-end against
Microsoft 365, as the first live-network mail source. Gmail and app-password
providers come later (see `docs/research-imap-oauth.md`). M365 is first because
Basic auth is gone, so it's OAuth-only and the sharpest pain.

## 0. Thunderbird ID? No.

The MVP uses a **self-registered Azure app** (bring-your-own client_id), not
Thunderbird's:

- Microsoft disabled Thunderbird's old client_id (2024-08-01); the current one
  rejects third-party `http://localhost` redirects.
- The RFC 2971 IMAP `ID` command (name=Thunderbird) doesn't affect M365 auth —
  M365 gates on OAuth. We may still send a benign `ID` for parity with other
  providers, but it's not what makes M365 work.

## 1. Operator setup (one-time, ~5 min)

In Entra ID (Azure AD) → **App registrations → New registration**:

- Name e.g. `mailarchive`. Supported accounts: **single tenant** (simplest for
  one org) or "any org" for multi-tenant.
- **Authentication → Add a platform → Mobile & desktop applications →** redirect
  URI **`http://localhost`** (loopback; Microsoft allows any port on loopback for
  public clients). Set **"Allow public client flows" = Yes**.
- **API permissions → APIs my organization uses → Office 365 Exchange Online →
  Delegated →** `IMAP.AccessAsUser.All`; **+ Microsoft Graph → Delegated →**
  `offline_access`, `openid`, `email`. **Grant admin consent.**
- No client secret (public client + PKCE).

Operator gives the tool: **tenant id** (or `organizations`), **client id**, and
the **account email**. That's it.

## 2. CLI surface

```
mailarchive imap -out DIR \
  -provider m365 -user alex@dimarcotech.com \
  -tenant <tenant-id> -client-id <app-id> \
  [-folder "INBOX"] [-mode incremental|full] [-login] [-logout]
```

- First run (or `-login`) opens the browser for consent; the token is cached at
  `DIR/.imap-<user>.token.json` (0600). Later runs refresh silently.
- `-logout` deletes the cached token.
- `-provider m365` fills host/port/endpoints/scope; the design leaves room for
  `-provider gmail` and `-provider generic -host … -auth password` later.
- Reuses existing export flags where they apply (`-out`, `-mode`, `-index`,
  `-pages`, `-since`). Everything downstream of a `model.Message` is unchanged.

## 3. Architecture

- **`internal/imapoauth`** (pure Go) — the loopback Authorization-Code+PKCE flow
  and token cache, built on `golang.org/x/oauth2`:
  - endpoints: `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/{authorize,token}`
  - scopes: `https://outlook.office365.com/IMAP.AccessAsUser.All offline_access`
  - spins an `http://localhost:0` server (random port), opens the browser, waits
    for the redirect, exchanges the code, persists the refresh token; exposes an
    `oauth2.TokenSource` that auto-refreshes.
- **`internal/source/imap`** — a `source.Source` implementation on
  `github.com/emersion/go-imap/v2/imapclient`:
  - dial `outlook.office365.com:993` (TLS), `Authenticate` with a small **XOAUTH2**
    `sasl.Client` (`user=<email>\x01auth=Bearer <token>\x01\x01`, base64) — go-sasl
    dropped XOAUTH2, so we vendor the ~10-line mechanism. (Microsoft IMAP requires
    XOAUTH2, not OAUTHBEARER.)
  - optional RFC 2971 `ID`.
  - `LIST "" "*"`, then per mailbox **`EXAMINE`** (read-only) + **`UID FETCH …
    BODY.PEEK[]`** — never `SELECT`/`STORE`/`BODY` — so the mailbox is never
    modified (R9). Each raw message → the existing `parseMessage` →
    `model.Message` → exporter, with folderPath = mailbox name split on the
    server's hierarchy delimiter.
- **Deps:** `emersion/go-imap/v2`, `golang.org/x/oauth2` — both pure Go, cgo-free,
  cross-platform. No change to the no-cgo guarantee.

## 4. Incremental (network-aware)

IMAP fetch is expensive, so incremental must avoid re-downloading:

- Extend the manifest/state with a per-`(account, folder)` **UIDVALIDITY + highest
  UID** watermark.
- Re-run: if `UIDVALIDITY` unchanged, `UID FETCH <lastUID+1>:* BODY.PEEK[]` — only
  new messages. If it changed (server renumbered), re-fetch the folder and rebuild
  the watermark.
- The existing Message-ID/content-hash identity still backs cross-source dedup and
  the manifest key; the UID watermark is the fast-path skip on top.

## 5. Assurance fit — mostly CI-testable (unlike the COM path)

New invariant **R17 — Live IMAP is read-only, faithful, incremental, least-priv:**
EXAMINE + BODY.PEEK never modify the mailbox; every fetched message is exported
once; the token is cached 0600 and only refreshed; incremental uses
UIDVALIDITY/UID and re-fetches on validity change.

Crucially, `emersion/go-imap` ships a server (`imapserver`), so most of this runs
in CI against a **real in-process IMAP server** — not lab-tier:

- **U** XOAUTH2 SASL string format (pure).
- **U/A** OAuth loopback: fake authorize/token via `httptest` → assert code
  exchange, refresh, and 0600 token-cache round-trip.
- **S** fetch loop vs a scripted in-process `imapserver`: folder walk, message
  mapping, and **read-only proof** — the fake server records zero STORE/flag
  changes and no `\Seen` set (EXAMINE + BODY.PEEK).
- **S** incremental: second run against the same fake server fetches only new
  UIDs; a bumped UIDVALIDITY forces a full re-fetch.
- **L (pending)** real M365 OAuth consent + server quirks — validated against an
  actual account, like MA-60. But the protocol/auth logic is CI-covered.

MA rows: XOAUTH2 format, OAuth exchange/refresh, token-cache perms, read-only
fetch, folder mapping, UID incremental, UIDVALIDITY reset — plus the lab row.

## 6. Phases

1. `internal/imapoauth`: loopback + PKCE + token cache; httptest-backed tests.
2. `internal/source/imap`: dial + XOAUTH2 + EXAMINE + BODY.PEEK + folder walk;
   `imapserver`-backed tests (incl. read-only proof).
3. Incremental: UIDVALIDITY/UID watermark in state; wire to the manifest.
4. CLI `imap` subcommand (+ `-login`/`-logout`); catalog R17 + MA rows; README;
   an Entra-app setup doc.
5. Lab validation against real M365 (flip the L rows); then generalize to Gmail /
   app-password providers.

## 7. Risks / caveats

- **Admin consent** for `IMAP.AccessAsUser.All` may be required in the tenant
  (one-time; the operator here is likely admin).
- **Conditional Access / MFA** policies can block automated sign-in; the loopback
  browser flow handles interactive MFA at login time.
- **Throttling** on large mailboxes (Microsoft may send BYE / slow down) — MVP
  keeps fetching simple; backoff/resume is backlog.
- **Token at rest** is a bearer credential — 0600, in the output dir; document it.
- **Scope longevity** — confirm `IMAP.AccessAsUser.All` is still offered at build
  time (Microsoft has been narrowing legacy-protocol access).
- **Read-only discipline is load-bearing** — any accidental `SELECT`/`BODY`
  (vs EXAMINE/BODY.PEEK) would set `\Seen` and violate R9; the fake-server test
  guards exactly this.

## 8. Out of scope (MVP)

Gmail (own restricted-scope story), app-password/generic providers, SMTP, the
GUI surface (CLI first), and unattended/scheduled OAuth refresh beyond the cached
refresh token.
