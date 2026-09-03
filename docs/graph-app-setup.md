# Microsoft 365 setup for `mailarchive graph` (admin, one-time)

`mailarchive graph` archives mailboxes **server-side** via Microsoft Graph with
an **app-only** identity — no user sign-in, nothing installed on any client. It
needs a tenant admin to register one app, grant it **read-only** mail access, and
scope it to the mailboxes being archived. ~15 minutes, and it's auditable and
revocable.

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
- **The secret expires.** Entra caps client secrets at 24 months and admins often
  issue 6- or 12-month ones. Put the expiry date in a calendar; when `status`
  reports an authentication failure, create a new secret in Entra and rewrite
  the secret file — nothing else changes.

## Revoking

Remove the client secret, or remove admin consent / the role assignment. Access
stops immediately; the app can do nothing without a valid credential + consent.

## Notes for our two tenants

- **St. Augustine's** (we're Global Admin): do all of the above ourselves.
- **University of Toronto** (we have no admin): steps 1–4 must be done by U of T
  central IT. Frame the request as *single-tenant, read-only `Mail.Read`,
  RBAC-scoped to the named mailboxes* — the minimal, auditable ask. If they
  decline, the fallback is a Purview Content Search → PST export they run and hand
  us (which `mailarchive -input …pst` ingests).
