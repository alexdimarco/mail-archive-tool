# Research: live IMAP fetch with OAuth2 (and "identify as Thunderbird")

**Question.** Can mailarchive connect to IMAP directly to grab mail, using OAuth2,
"identifying as Thunderbird" so connections to the big providers go smoothly?

**Short verdict.** A native-Go IMAP fetcher with OAuth2 is very feasible and
would make mailarchive a real archiver (no local mail client needed). But two
premises need correcting:

1. **There is no reusable "Thunderbird engine."** Thunderbird's IMAP/OAuth code
   is Mozilla C++ (`libmailnews`), not a library you can link from Go. We would
   build the client on `emersion/go-imap` — which is fine; we don't need their
   engine, only OAuth + XOAUTH2 + a folder walk + FETCH.
2. **"Identify as Thunderbird" means two different things**, and only one is
   still viable (see below).

## The two meanings of "identify as Thunderbird"

### (a) Reuse Thunderbird's OAuth *client_id* — mostly dead, and impersonation

Thunderbird ships hardcoded OAuth credentials (visible in
`mailnews/base/src/OAuth2Providers.sys.mjs`):

- **Gmail:** client_id `406964657835-aq8lmia8j95dhl1a2bvharmfk3t1hgqj.apps.googleusercontent.com`, with a hardcoded "secret" `kSmqreRr0qwBWJgbf5Y-PjSU`, PKCE + external-browser flow.
- **Microsoft:** client_id `9e5f94bc-e8a4-4e73-b8be-63364c29d753`, public client (no secret).

Reusing these to pose as Thunderbird:

- **Microsoft: no longer works.** Microsoft *disabled* the old Thunderbird app
  id (`08162f7c-…`) on 2024-08-01 ("Application 'Thunderbird' is disabled"), and
  the newer id rejects a `http://localhost` redirect that isn't on its allow-list
  ("redirect URI … does not match"). So the loopback flow a third-party tool
  needs is blocked.
- **Gmail: technically possible, but it's impersonation.** The client_id/secret
  are public, so a tool *can* present them — but that is using Mozilla's identity
  and Google's approval, against both parties' terms, fragile (revocable, would
  break Thunderbird too), and a documented gray area. Not something to ship.

**Conclusion:** don't build on Thunderbird's OAuth identity. It's dead for
Microsoft and impermissible for Gmail.

### (b) Send an RFC 2971 `ID` command advertising a client name — fine, and useful

The IMAP `ID` command lets a client announce `name`/`version` (e.g.
`("name" "Thunderbird" "version" "128.0")`). Some servers (notably 163.com, QQ,
other strict hosts) *require* an `ID` before they allow access. This is cheap,
standards-based, and legitimate — `go-imap` supports it. This is the part of
"identify as a normal client" worth doing. TLS/connection fingerprinting beyond
this is unnecessary; the providers gate on OAuth, not on client fingerprint.

## The real gate: OAuth per provider (current, 2026)

### Gmail

- IMAP needs the **restricted** scope `https://mail.google.com/`. A publicly
  distributed app using it must pass Google review **plus a yearly CASA Tier 2**
  security audit (self-serve labs ~$540–$1,000/yr in 2026; the older manual
  assessment was $15k–$75k). Until verified: "unverified app" warning, **max 100
  test users**, and — in *Testing* publishing status — **refresh tokens expire
  after 7 days** (a killer for unattended archiving).
- **Clean path for an org (e.g. Crescendum's Workspace):** register the OAuth
  client as **Internal** (user type = Internal). Internal apps in a Workspace org
  need **no verification, no CASA**, and their refresh tokens **don't** expire on
  the 7-day Testing clock. This is the pragmatic route for archiving accounts in
  a domain you control.

### Microsoft 365 / Outlook.com

- Basic auth for IMAP has been **off since 2022-10-01**; OAuth2 is mandatory.
- Register an **Azure app** (single-tenant is simplest for one org), add the
  delegated permission **`IMAP.AccessAsUser.All`** + **`offline_access`**,
  configure it as a **public client** with a `http://localhost` loopback redirect,
  grant admin consent once. (App-only `IMAP.AccessAsApp` also exists for
  unattended mailbox access without per-user consent, granted per-mailbox.)

### Others

- **Yahoo / AOL / Fastmail / generic:** app-specific passwords + plain IMAP
  `LOGIN` still work in many cases — far simpler than OAuth. Worth supporting as a
  first-class, low-friction option alongside OAuth.

## What we'd build (native Go, additive source)

- **`internal/source/imap`** using `github.com/emersion/go-imap/v2`
  (`imapclient`) — dial TLS, SASL auth, send `ID`, `LIST` folders, `FETCH BODY[]`,
  hand each raw RFC 5322 message to the existing `parseMessage` → `model.Message`
  → exporter. The parsing/HTML/attachment/index pipeline is entirely reused.
- **Auth:** `golang.org/x/oauth2` for the loopback authorization-code + PKCE flow
  and refresh; a token cache on disk (per account). SASL **XOAUTH2** (Microsoft
  requires it; Gmail accepts it — note `go-sasl` deprecated XOAUTH2, so implement
  the ~10-line mechanism ourselves or vendor a helper) and **OAUTHBEARER**
  (Gmail's preferred) both.
- **Config:** provider presets (Gmail, Microsoft, Yahoo, generic host/port) plus
  **bring-your-own client_id/secret** — the model used by `isync`/`mbsync`,
  `aerc`, `himalaya`, `mutt_oauth2`: the operator registers their own OAuth app
  (or, for Crescendum, an Internal/single-tenant one) and supplies the id. No
  Thunderbird impersonation.
- **Incremental:** IMAP `UIDVALIDITY` + `UID` map cleanly onto the existing
  manifest dedup, so incremental re-fetch is natural.

## Risks / honest caveats

- **Distribution vs. verification.** A broadly-distributed build can't legitimately
  ship a working Gmail full-mail OAuth client without CASA. The viable model is
  **bring-your-own-OAuth-app** (each operator/org registers one). For Crescendum's
  own domains this is easy (Internal Google app + single-tenant Azure app).
- **Not the Thunderbird identity.** Riding on Thunderbird's client_id is dead
  (MS) / impermissible (Gmail); we register our own. The only "be like
  Thunderbird" we keep is the harmless RFC 2971 `ID` string.
- **Scope creep.** This turns mailarchive from a local-store archiver into a live
  network mail client (tokens, refresh, secrets at rest, server quirks, throttling,
  large-mailbox pacing). It's a real subsystem, not a flag.
- **Maintenance.** Provider OAuth policies shift (this doc will age); the
  loopback-redirect and scope details in particular.

## Recommendation

1. Build the **native Go IMAP source** (`emersion/go-imap`) with **XOAUTH2/
   OAUTHBEARER + loopback OAuth + bring-your-own client_id**, and send an RFC 2971
   `ID`. Ship **Gmail** and **Microsoft 365** presets plus a **generic
   host/app-password** path.
2. **Do not** embed or impersonate Thunderbird's OAuth credentials.
3. For Crescendum specifically, document the **Internal Google OAuth app** and
   **single-tenant Azure app** setup — that sidesteps CASA/verification entirely
   for the domains we own.

An MVP could target one provider end-to-end first (Microsoft 365, since Basic
auth is gone and it's the sharper pain) to prove the OAuth + go-imap + exporter
loop, then add Gmail and app-password providers.

## Sources

- go-imap v2 client + SASL: <https://github.com/emersion/go-imap>,
  <https://pkg.go.dev/github.com/emersion/go-imap/v2/imapclient>,
  <https://github.com/emersion/go-sasl/issues/18> (XOAUTH2 deprecation)
- Thunderbird OAuth providers/credentials:
  <https://searchfox.org/comm-central/source/mailnews/base/src/OAuth2Providers.sys.mjs>
- Thunderbird client_id reuse blocked:
  <https://github.com/simonrob/email-oauth2-proxy/issues/267>,
  <https://github.com/pimalaya/himalaya/issues/555>
- Gmail restricted scope + CASA: <https://developers.google.com/workspace/gmail/imap/xoauth2-protocol>,
  <https://www.unipile.com/integrating-google-oauth-2-0-user-authentication-into-your-app/>
- Microsoft Basic-auth deprecation + IMAP OAuth:
  <https://learn.microsoft.com/en-us/exchange/clients-and-mobile-in-exchange-online/deprecation-of-basic-authentication-exchange-online>
