# Microsoft 365 setup for `mailarchive graph`

`mailarchive graph` archives Microsoft 365 mailboxes via Microsoft Graph,
**read-only**. There are two ways to authenticate; pick one with `-auth`:

- **`-auth app` (app-only, the default)** — a tenant admin registers one app,
  grants it **read-only** mail access, and scopes it to the mailboxes being
  archived. No user sign-in, nothing installed on any client. Best where you are
  (or can reach) the tenant's Global Admin. **Steps 1–6 below.**
- **`-auth device` (per-user, device-code sign-in)** — each person signs the tool
  in to their **own** mailbox once, in a browser, with **no tenant-wide
  permission and no admin RBAC**. Best where you are *not* admin (e.g. a tenant
  whose central IT will not grant an application `Mail.Read`). **See
  "Per-user (device sign-in)" below.**

## App-only (admin, one-time)

The app-only path needs a tenant admin to register one app, grant it **read-only**
mail access, and scope it to the mailboxes being archived. ~15 minutes, and it's
auditable and revocable.

This is safe to hand to the tenant's Global Administrator. Everything below is
**read-only** (`Mail.Read`) and **scoped** to named mailboxes via RBAC for
Applications — the app cannot send, modify, or read anything outside that scope.

## 1. Register the app (Entra admin center → App registrations → New)

- Name: `mailarchive`.
- Supported accounts: **Accounts in this organizational directory only** (single
  tenant).
- No redirect URI needed (app-only, no interactive sign-in).
- Note the **Directory (tenant) ID** and **Application (client) ID**.

## 2. Add a credential (Certificates & secrets)

- **Client secret** (simplest) — create one, copy the value now (shown once).
  Prefer a **certificate** for production if your policy requires it.

## 3. Grant the read-only mail permission (API permissions)

- Add a permission → **Microsoft Graph → Application permissions → `Mail.Read`**.
- Click **Grant admin consent for <org>**. (This is the whole "acceptance" — you,
  the admin, approve it; there is no external Microsoft review for a single-tenant
  app.)

## 4. Scope it to specific mailboxes (RBAC for Applications) — recommended

By default an application `Mail.Read` grant can read *every* mailbox. Limit it to
just the mailboxes you archive, via Exchange Online PowerShell:

```powershell
# One-time: register the app's service principal in Exchange.
New-ServicePrincipal -AppId <APPLICATION_CLIENT_ID> -ServiceId <ENTERPRISE_APP_OBJECT_ID> -DisplayName "mailarchive"

# Scope: a mail-enabled security group whose members are the mailboxes to archive.
New-ManagementScope -Name "mailarchive-scope" -RecipientRestrictionFilter "MemberOfGroup -eq '<GROUP_DN>'"

# Assign the read-only application role, limited to that scope.
New-ManagementRoleAssignment -App <APPLICATION_CLIENT_ID> -Role "Application Mail.Read" -CustomResourceScope "mailarchive-scope"
```

(If you skip scoping, the app can read all mailboxes — only do that in a tenant
where that's acceptable.)

## 5. Confirm the mailbox

Graph mail read needs no IMAP/POP — the mailbox just has to be a normal Exchange
Online mailbox in this tenant.

## 6. Hand back to the operator

Give the operator: **tenant id**, **client id**, the **client secret**, and the
list of **mailbox UPNs**. They run it interactively with the secret in an
environment variable, or — for scheduled runs, which have no environment — from
a file readable only by them (never on the command line):

```sh
# interactive
MAILARCHIVE_GRAPH_SECRET='<the secret>' \
mailarchive graph -out ./archive \
  -tenant <TENANT_ID> -client-id <CLIENT_ID> \
  -mailbox alice@contoso.org -mailbox bob@contoso.org

# from a file (chmod 600; a regular file, not a symlink or pipe)
mailarchive graph -out ./archive -tenant <TENANT_ID> -client-id <CLIENT_ID> \
  -mailbox alice@contoso.org -client-secret-file ~/.config/mailarchive/graph.secret

# scheduled nightly at 03:00 — the job is validated now, exactly as it will run
mailarchive schedule -interval daily -at 03:00 -install -- graph -out ./archive \
  -tenant <TENANT_ID> -client-id <CLIENT_ID> -mailbox alice@contoso.org \
  -client-secret-file ~/.config/mailarchive/graph.secret
```

- First run captures everything; later runs are incremental (only new mail, no
  re-download of anything already archived). `mailarchive status -out ./archive`
  shows completeness, the last run and the schedule.
- Read-only throughout: the tool issues only Graph GET requests.
- **Deleted Items and Junk Email are excluded by default** — pass
  `-include-deleted` / `-include-junk` to archive them. The exclusion is by
  resolved well-known-folder id (`deletedItems` / `junkemail`), not display name,
  so it holds whatever the mailbox's display language is.
- **The secret expires.** Entra caps client secrets at 24 months and admins often
  issue 6- or 12-month ones. Put the expiry date in a calendar; when `status`
  reports an authentication failure, create a new secret in Entra and rewrite
  the secret file — nothing else changes.

## Per-user (device sign-in) — `-auth device`, no tenant-wide permission

This path reads **only the signed-in user's own mailbox**, with a delegated
`Mail.Read` grant the user consents to themselves. There is **no application
permission, no RBAC, and no admin step** in the common case — the minimal ask for
a tenant where you are not admin.

### 1. Register the app (once, any admin *or* — if the tenant allows it — any user)

- Entra → App registrations → New. Name: `mailarchive`. Single tenant.
- **Authentication → Advanced settings → Allow public client flows → Yes.** (The
  device-code flow is a *public* client — it has **no client secret**.)
- **API permissions → Microsoft Graph → Delegated → `Mail.Read`** and
  **`offline_access`** (the refresh token that lets later scheduled runs skip the
  sign-in). No `Mail.ReadWrite`, no `Mail.Send` — the tool only ever issues GET.
- **Admin consent is conditional.** Most tenants let a user consent to delegated
  `Mail.Read` for themselves at first sign-in — no admin. A tenant that has
  disabled user consent needs a **one-time admin consent** for this app; even
  then there is **no RBAC and no per-mailbox scoping** — it is a single
  tenant-wide "users may read *their own* mail with this app" grant, still far
  narrower than an application `Mail.Read` (which reads *every* mailbox).
- Note the **Directory (tenant) ID** and **Application (client) ID**. There is no
  secret to copy.

### 2. Sign in and archive (each user, on their own machine)

```sh
mailarchive graph -auth device -out ./archive \
  -tenant <TENANT_ID> -client-id <CLIENT_ID>
```

The tool prints a short code and a URL (`https://microsoft.com/devicelogin`).
Open the URL, sign in, and enter the code. **Before you approve, check the
consent screen names the app `mailarchive`** — a code someone *else* sent you
would sign *them* in to your mailbox, so only ever enter a code this tool just
printed to you. After you approve, the run archives your mailbox and saves your
sign-in (a refresh token) so later runs need no sign-in.

- `-mailbox` is optional here and, if given, must be **your own** address — device
  mode archives only the mailbox you signed in as. It refuses any other address.
- The sign-in is cached at a **file readable only by you** under your OS config
  dir (`~/.config/mailarchive/` on Linux/macOS, `%APPDATA%\mailarchive\` on
  Windows). Override with `-token-cache PATH`. It is **never** written inside
  `-out` (which may be a synced OneDrive folder). It is a plaintext bearer token
  at `chmod 600`, not encrypted at rest — treat it like the client secret file.

### 3. Scheduling a device job

A scheduled run cannot prompt, so it runs from the saved sign-in. Two rules:

- **Sign in first, as the same OS account the schedule runs as.** The cache is
  per-user and `0600`; a task running as a different account (or Windows SYSTEM)
  cannot read it. Run step 2 once as that account, then schedule.
- Pass `-token-cache PATH`; `schedule` validates the cache exists and carries a
  refresh token at install time (it refuses otherwise, naming the sign-in step).

```sh
# prime the sign-in (interactive, once, as the scheduling user)
mailarchive graph -auth device -out ./archive -tenant <T> -client-id <C> \
  -token-cache ~/.config/mailarchive/graph.token

# then schedule it — runs unattended from that cache
mailarchive schedule -interval daily -at 03:00 -install -- graph -auth device \
  -out ./archive -tenant <T> -client-id <C> \
  -token-cache ~/.config/mailarchive/graph.token
```

- **The sign-in can expire.** A refresh token lapses after a period of inactivity
  (Entra's default is ~90 days; Conditional Access can shorten it) or when the
  user changes their password / an admin revokes sessions. When `status` reports
  an authentication failure, re-run step 2 to sign in again — nothing else changes.
- One schedule per mailbox (two runs sharing one cache can invalidate each other's
  refresh token).

## Revoking

- **App-only:** remove the client secret, or remove admin consent / the role
  assignment.
- **Device (per-user):** the user revokes it from their own account (Microsoft
  account → sign out everywhere / revoke sessions), or an admin revokes the app's
  consent for the tenant. Deleting the local token-cache file stops *that machine*
  but does not revoke the grant server-side.

Access stops immediately; neither mode can do anything without a valid
credential + consent.

## Notes for our two tenants

- **St. Augustine's** (we're Global Admin): use **`-auth app`** — do the app-only
  steps ourselves; it archives many mailboxes from one scheduled job.
- **University of Toronto** (we have no admin): use **`-auth device`** — each
  person signs the tool in to their own mailbox with no tenant-wide permission
  (register the public client once; user consent usually needs no admin). If even
  that is blocked (user consent disabled *and* no admin will consent the app), the
  last-resort fallback is a Purview Content Search → PST export U of T runs and
  hands us (which `mailarchive -input …pst` ingests).
