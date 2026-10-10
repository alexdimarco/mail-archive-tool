# Adversarial review: slice EW-E1 (extraction workspace, Graph additions)

12 found / 3 confirmed / 0 refuted. The other 9 are set aside separately: 3 duplicates (AO3 and AC2 of AI1, AI4 of AO5) and 6 non-goals.

## Scope

- Slice: EW-E1, the extraction workspace's Graph additions (design-extraction-workspace slice E1; rulings §9 row 15): FolderTree, FolderCount, MessagePage, the host/path pin on the workspace loops, CheckRedirect plus body limits (XC26), the F15 timeout fix, identity-safe putIfNewer/reload (XC7).
- Base 6de4792 (fix/review-2026-10); reviewed head 07d23da; fixes on wip/ew-e1.
- Lenses: outside aggressor (the Graph endpoint and redirects); insider (a second signed-in account and mixed identities); integrity and concurrency (timeouts, pins and write-back).
- Procedure: assurance-kit/process/adversarial-review.md, sections 3 and 4.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| AO1 | outside aggressor | WellKnownFolderID decodes its body with no size limit (graph.go:419; FolderTree calls it twice) | CONFIRMED (medium) | Fake served `{"id":"` plus 200 MiB; id returned with err=nil. Reproduced again in the fix tombstone (75 MiB id accepted). | 09a0d5e | TestWellKnownFolderIDBodyIsBounded (MA-528) |
| AO2 | outside aggressor | MessagePage's $expand fallback fires on any non-401/403/404/429 StatusError (workspace.go:262-270) | NON_GOAL | Mechanism real, but it is MA-285's acknowledged best-effort limit (design §4.5 lines 841-844); capture's Messages falls back on any error, so this is stricter than its precedent. No flag was asked for in the design. | - | - |
| AO3 | outside aggressor | putIfNewer check-then-act (delegated.go:312-321) | DUPLICATE of AI1 | Same mechanism; the UPN-only identity half is AI2. | (AI1) | (AI1) |
| AO4 | outside aggressor | graph.Classify reads a refused redirect as ClassTransport (graph.go:1069) | NON_GOAL | Design §4.5 lines 894-895 and guard.go:146-148 state the limit: callers match *RedirectError first (RR2b-C4). Token does not leave. | - | - |
| AO5 | outside aggressor | A followed nextLink or message need not belong to the requested folder (workspace.go:224-240) | NON_GOAL | The pin is the base path by definition (XC25, design lines 875-878). Backstops are row-level parentFolderId, FolderCount reconciliation (§4.7) and E2. Host and token do not leave. | - | - |
| AI1 | insider | putIfNewer is load-then-write with no lock (delegated.go:312-321) | CONFIRMED (low) | Hook TokenStore wrote b@x between load and put; final store held a@x/RA2. Reproduced in the fix tombstone. In-process half fixed; cross-process half is an escalation. | 22007f1 | TestTokenWriteBackIsIdentitySafe, case "a sign-in inside the load-to-write window is not lost" (MA-515); open gap: TestTokenWriteBackCrossProcessSignIn (skip encoder) |
| AI2 | insider | Identity check trusts the cache's own UPN, never Graph's (delegated.go:73, 314) | NON_GOAL | A mislabelled cache needs hand-editing the 0600 file; NewDelegated writes the UPN from Me() and putIfNewer writes only the source's own UPN. Design identity is the UPN (XC7). | - | - |
| AI3 | insider | Folder ids unvalidated; '.', '..', '' change the resource (workspace.go:93,233,344) | NON_GOAL | Requests reproduced but stay on /me (OW3, MA-403 hold). Ids come only from the cached tree and parentFolderId (design line 2566); caller hygiene for E2/E3. | - | - |
| AI4 | insider | nextLink pin is the base path, so a supplied next can reach any GET under /v1.0 (guard.go:101-116) | DUPLICATE of AO5 | Same claim; same NON_GOAL under XC25. | (AO5) | - |
| AC1 | integrity | A shared PageGuard turns a retry of a failed page into a false ListingRepeat (guard.go:78-94) | CONFIRMED (low, downgraded from medium) | 502 on page 2, retry through the same guard gave "did not terminate", no request sent. Low: no E1 loop retries a page, fails closed. Fixed anyway: it is an API contract hazard for E2. | 6106361 | TestPageGuardAllowsRetryOfFailedPage (MA-401) |
| AC2 | integrity | putIfNewer load-then-put not atomic (delegated.go:312-321) | DUPLICATE of AI1 | Same hook-store reproduction. | (AI1) | (AI1) |
| AC3 | integrity | A nil PageOpts.Guard gives a per-call guard (workspace.go:224-227) | NON_GOAL | Documented ("nil = a guard for this one call"); browse is one page per call; FolderTree's own loops use a guard each. The resolution loop is E2. | - | - |

## Escalations

- AI1, cross-process: the fix serialises writes inside one process only. A sign-in in the dashboard process during a scheduled run's load-to-write window can still be overwritten (a forced re-sign-in; no token is ever written under another identity). Closing it needs a cross-process lock on the token file and on each vault backend, on three operating systems: an owner decision. Skip encoder: TestTokenWriteBackCrossProcessSignIn (MA-515, XC7).
