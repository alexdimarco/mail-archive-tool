# Friction review — Graph setup wizard (slice b)

**Reviewed:** the admin ceremony of `mailarchive setup`, walked as the actor it exists
for — an admin standing up Microsoft 365 capture on an endpoint, who previously got
stuck on which Entra identifier to paste. Classification per
`../assurance-kit/process/friction-review.md`: Type I (inherent, document) vs Type II
(design-choice, fix).

## The walk

1. Run `mailarchive setup`. It prints a loopback URL and the config/secret-store
   locations, and serves the wizard (loopback only). *(Type I — a local page is the
   entry surface; it is never network-reachable, by refusal, A10/MA-265.)*
2. Enter **Directory (tenant) ID** and **Application (client) ID**. Each field names
   where to find it in Entra; the client-id field warns, inline, **"Not an Object ID"**
   and calls out both the app-registration and enterprise-application Object IDs.
   *(Fixes the reported confusion — Type II, fixed.)*
3. Pick **Device** (no secret) or **App-only** (reveals the secret field). *(Matches
   the two deployments; the secret field appears only when it applies.)*
4. (App-only) paste the client secret → **Save**. The secret goes to the OS credential
   manager (Windows) or a `0600` file; the page then shows "✓ already stored", and on
   a later visit the field says "leave blank to keep". *(Type II, fixed — no secret on
   a command line, no re-paste to change unrelated settings.)*
5. Run `mailarchive graph` (or a schedule) with no credential flags — it fills tenant/
   client/auth from the config and resolves the secret from the store (MA-269).

Steps: **0 out-of-band secret transfers** on Windows (the secret never leaves the
machine's vault); **1 local page**; failures legible (every refusal names the cause).

## Findings

| # | Type | Point | Disposition |
|---|---|---|---|
| FR1 | II (FIXED) | Admins pasted an Object ID instead of the Application (client) ID — the reported bug | Fixed at the point of entry: inline help on the client-id field + the `graph-app-setup.md` "Which IDs do I use?" table (MA-268). |
| FR2 | II (FIXED) | The client secret previously had only a plaintext-file path | Fixed: the wizard stores it in Windows Credential Manager (file `0600` elsewhere); never on a command line (MA-266/269). |
| FR3 | I | Device mode still needs the one-time device-code sign-in at first capture | Documented: the Save confirmation says "Run a capture to sign in"; the wizard configures, it does not sign in (that is the separate `graph -auth device` step). |
| FR4 | II (LOW, accepted) | The default config/secret paths are under the OS config dir, not shown unless you read the `setup` banner | Accept: `mailarchive setup` prints both paths on start; `-config` overrides. |
| FR5 | I | On Linux/macOS the secret is a `0600` file (plaintext at rest), not an OS vault | Documented (same posture as the existing `-client-secret-file`); the Windows deployment — the target — uses Credential Manager. |

## Verdict

**PASS (shippable).** The two frictions the feature exists to remove — the Object-ID
confusion (FR1) and the command-line/plaintext secret (FR2) — are fixed and
test-covered, each at the point of entry. The remainder are Type-I (documented). The
reader `serve` is untouched, so no reader friction changes.
