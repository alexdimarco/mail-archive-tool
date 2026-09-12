# Lab test — does Graph close the #8 (distinct Message-ID reuse) drop?

**Why.** The go-back rev-4 design defers the "#8 closed on Graph" closure behind a
lab check (gate condition EC6, `docs/review-goback-dedup-rev4-gate.md`). The
closure relies on Microsoft Graph's **immutable message id** as a per-physical
identity. It cannot be verified from the build machine — it needs YOUR live M365
tenant and the app-only app you already registered (`docs/graph-app-setup.md`,
`Mail.Read`). This runbook answers four questions; if all four pass, the closure
is buildable, if any fails it stays a logged residual (the floor).

You need: the app's **tenant id**, **client id**, **client secret**; a **test
mailbox** UPN (`USER`); and a way to send mail with a **custom Message-ID** — the
`swaks` CLI is easiest. `curl` + `jq` for the calls.

```sh
export TENANT=...            # Directory (tenant) ID
export CLIENT_ID=...         # Application (client) ID
export CLIENT_SECRET=...     # a client secret value
export USER=test@yourdomain  # the test mailbox (must be in the RBAC scope)
```

## 0. Get an app-only token (same grant the tool uses)

```sh
TOKEN=$(curl -s -X POST \
  "https://login.microsoftonline.com/$TENANT/oauth2/v2.0/token" \
  -d "client_id=$CLIENT_ID" -d "client_secret=$CLIENT_SECRET" \
  -d "scope=https://graph.microsoft.com/.default" \
  -d "grant_type=client_credentials" | jq -r .access_token)
test -n "$TOKEN" && echo "got token" || echo "TOKEN FAILED — check creds/consent"
```

## Q1 (EC6) — are immutable ids returned under APP-ONLY?

List the Inbox WITH the immutable-id preference and check Graph honored it:

```sh
curl -s -D /tmp/hdrs.txt \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Prefer: IdType="ImmutableId"' \
  "https://graph.microsoft.com/v1.0/users/$USER/mailFolders/inbox/messages?\$select=id,internetMessageId,subject&\$top=5" \
  | jq '.value[] | {id, internetMessageId, subject}'
grep -i '^Preference-Applied' /tmp/hdrs.txt
```

- **PASS** if the response header `Preference-Applied: IdType="ImmutableId"`
  is present (and the `id` values look immutable — a longer base64url string,
  different in shape from the default handle). This is the whole EC6 question:
  the preference works with an app-only token.
- **FAIL** if the header is absent (Graph ignored the preference / app-only
  doesn't get immutable ids). Then #8 on Graph stays the logged floor — stop here
  and tell me; the floor build needs no change.

> Note: whichever result, the tool's steady-state dedup (Message-ID membership
> skip) is unaffected — this test only gates the ADDED #8 closure.

## Q2 (R17 across a move) — is the immutable id stable when a message moves?

1. Pick one message from Q1; note its `id` (call it `$IMM`) and folder.
2. In Outlook/OWA, **move that message to another folder** (e.g. Inbox → Archive).
3. Re-list the destination folder with the same `Prefer` header and find the
   same message (match on `internetMessageId`); compare its `id` to `$IMM`.

- **PASS** if the `id` is UNCHANGED after the move (this is what lets the tool
  skip re-download on a move without any content signature).
- **FAIL** if the `id` changed → it is not move-stable; the closure loses its R17
  benefit. Report the result.

## Q3 (the #8 core) — do two distinct messages that REUSE a Message-ID get DIFFERENT immutable ids?

This is the decisive one: the closure works only if a reused Internet-Message-ID
maps to two DISTINCT physical ids.

1. Send two different-body messages to `$USER` that carry the SAME `Message-ID`:

```sh
swaks --to "$USER" --server <your-smtp> --header "Message-ID: <dup-test-8@yourdomain>" \
      --body "First message, body ALPHA."
swaks --to "$USER" --server <your-smtp> --header "Message-ID: <dup-test-8@yourdomain>" \
      --body "Second message, body BRAVO — a distinct message reusing the id."
```

   (No SMTP relay handy? Any method that lands two mails with an identical
   `Message-ID:` header works — e.g. two `.eml` imports. The point is one
   Internet-Message-ID, two physically distinct messages.)

2. After both arrive, list the Inbox with the `Prefer` header and filter to that
   Message-ID:

```sh
curl -s -H "Authorization: Bearer $TOKEN" -H 'Prefer: IdType="ImmutableId"' \
  "https://graph.microsoft.com/v1.0/users/$USER/mailFolders/inbox/messages?\$select=id,internetMessageId,bodyPreview&\$top=50" \
  | jq '[.value[] | select(.internetMessageId=="<dup-test-8@yourdomain>")] | {count: length, ids: [.[].id], previews: [.[].bodyPreview]}'
```

- **PASS** if `count` is 2 and the two `ids` are **different** (and the two
  previews show ALPHA vs BRAVO). This proves Graph gives each physical message its
  own immutable id, so the tool can download the distinct one and keep both — #8
  closed on Graph.
- **FAIL / inconclusive** if only one message appears (the service deduped them),
  or the two ids are equal → the closure cannot distinguish them; #8 stays the
  logged floor.

## Q4 (EC6, the fetch) — does the MIME download resolve with an immutable id?

Take one `$IMM` from Q1/Q3 and fetch its raw MIME the way the tool would:

```sh
curl -s -o /tmp/msg.eml -w "%{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" -H 'Prefer: IdType="ImmutableId"' \
  "https://graph.microsoft.com/v1.0/users/$USER/messages/$IMM/\$value"
head -5 /tmp/msg.eml
```

- **PASS** if the status is `200` and `/tmp/msg.eml` begins with real RFC-822
  headers. (Confirms `/$value` accepts the immutable id under the header — the tool
  must send the `Prefer` header on the FETCH too, not only the listing.)
- **FAIL** if `404`/`400` → the fetch path needs the plain id, complicating the
  closure. Report it.

## What to send me

Just the four PASS/FAIL results (and any surprising header/id shapes). If Q1, Q3,
and Q4 pass, I'll build the ImmutableId closure through its own gate; if any of
Q1/Q3/Q4 fails, "#8 closed on Graph" is not deliverable and we keep the bounded,
logged residual on Graph too — the floor build already handles that and needs no
change.
