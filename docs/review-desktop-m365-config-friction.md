# Friction review — Microsoft 365 configuration in the desktop (MA-549)

**Reviewed:** the in-app Microsoft 365 setup added in `8a2499f` — the Overview's
"Microsoft 365 connection" form and its guarded `POST /api/m365/configure`, which let a
user configure (and update) tenant / application id / auth mode / client secret without
the command line. Classification per `../assurance-kit/process/friction-review.md`:
Type I (security-inherent, document) · Type II (design-choice, drives verdict) ·
Type III (buildable-away, operability backlog — build before ship).

**Environment:** CODE-WALK on macOS (this change set cannot build or install a Windows
MSI, open the dashboard in a browser, or run a real Graph device sign-in here). The
paired LIVE drill — a clean Windows machine walking configure → authenticate → first
archive with no PowerShell — is **pending** the Windows lab (the MA-377 install-drill
family). This review walks the journey through the code, HTML and JS; it does not stand
in for that live drill, and the cells below are flagged accordingly.

**Actors:** owner (non-technical, their own PC) · admin/custodian (IT, managed
deployment) · operator inheriting the box cold. **Cells walked:** 15.

## The walk

| # | Cell (actor × scenario) | Functions? | Friction | Type |
|---|---|---|---|---|
| 1 | owner × first-time **device** config → Save | yes | open dashboard (shortcut opens the browser), type 2 IDs, Save — device is the default, no secret; **0 command-line steps** | II — fixed |
| 2 | owner × first capture after device config ("Connect") | yes | click **Archive now** → a one-time Microsoft device code → sign in | I (device-code is inherent) |
| 3 | admin × first-time **app-only** config (paste secret) → Save | yes | pick App-only, paste the secret, Save; secret → OS credential store, write-only | II — fixed (no CLI, no plaintext file) |
| 4 | admin × update the secret only | yes | Edit → leave the secret blank to keep it (placeholder says so) or paste a new one → Save | II — fixed (no re-type of unrelated settings) |
| 5 | owner × reconfigure to a new tenant/client | yes | Edit → change the IDs → Save; the superseded registration's secret is deleted (MA-270) | II — fixed |
| 6 | owner × app-only but forgets the secret | refuses, legibly | "a client secret is required for app-only auth (or switch to device)" — names the remedy; nothing written | I (legible refusal) |
| 7 | owner × leaves the tenant blank | refuses, legibly | "Directory (tenant) ID is required"; nothing written | I (legible refusal) |
| 8 | operator inheriting a configured box | yes | the card shows tenant / client / auth / sign-in; **Edit** reveals the prefilled form | II — fixed (inheritable, legible) |
| 9 | owner × broken default-browser association | yes | the "Copy MailArchive link" shortcut shows the dashboard link; the form is on that page (CC20) | I (inherent to the local-surface model) |
| 10 | hostile web page / another account × `POST /api/m365/configure` | refused 403, nothing written | none for the legitimate user — the guard IS the control (`d.api`: loopback + CSRF + Origin + Host) | I (security-inherent, X9) |
| 11 | admin × deployment switch OFF, owner still needs M365 | yes | configure is **always-on** (survives the switch), so disabling the *export* workspace does not strand M365 setup that *capture* needs | II — fixed by design (see note) |
| 12 | owner × local-origin (Thunderbird) archive | yes | no M365 form is shown; the card explains the archive is local (MA-546) | I (correct — no Microsoft 365 involved) |
| 13 | owner × clicks **Archive now** BEFORE configuring | refuses, but… | the capture refusal still says "run `mailarchive setup`" — it names the **command line**, not the form now sitting on the same page | **III** (stale remediation — FR7) |
| 14 | owner × un-configured Extraction workspace (source picker / Sign in) | refuses, but… | R-a and the source-picker message still say "saved by the command-line tool, **a separate download** … run `mailarchive setup`" | **III** (stale remediation — FR7) |
| 15 | owner × no writable profile folder (no config path / store) | refuses, legibly | "MailArchive Desktop has nowhere to save the Microsoft 365 settings … open MailArchive from its shortcut … or reinstall it" | I (legible, names remedy) |

## Findings

| # | Type | Point | Disposition |
|---|---|---|---|
| FR1 | II (FIXED) | Configuring Microsoft 365 required the command line (`mailarchive setup`), unreachable from the desktop — the reported defect | Fixed: the Overview's Microsoft 365 card is an editable, prefilled form; **Save** writes through `server.SaveSetup`. Covered by MA-549 + `TestOverviewContent`. |
| FR2 | II (FIXED) | The client secret had no in-app path and the wizard was a separate download | Fixed: app-only secret → OS credential store, write-only, never echoed; a blank secret on update keeps the stored one (MA-549). |
| FR3 | II (FIXED) | Object-ID confusion (the wizard's FR1) would recur on a second entry surface | Fixed at the point of entry: the form carries the same inline **"Not an Object ID"** help as the wizard. |
| FR4 | II (FIXED, by design) | If configure were gated behind the Extraction-workspace deployment switch, an admin disabling *export* would also strand M365 setup that *capture* needs | Fixed: `/api/m365/configure` is core/always-on; `TestDeploymentSwitchHidesTheWorkspace` asserts it survives the switch while the 19 workspace routes 404. **Operator may veto** this scope call (then it becomes workspace-gated + the form hides when off). |
| FR5 | I | Device mode still needs the one-time device-code sign-in at first capture | Documented in-flow: the Save note says "Click Archive now (or Sign in) to connect with a Microsoft device code". Config ≠ sign-in, same as the wizard (its FR3). |
| FR6 | I | Save and Connect are two steps | Inherent (configure then authenticate); the Save note signposts the next step. |
| FR7 | **III (BACKLOG — owner copy)** | The desktop's "not configured" refusals still point to the command line now that the form exists: `capture.go` capture refusal ("run `mailarchive setup`"), `refusals.go` **R-a** and `pages_scripts.go` source-picker ("saved by the command-line tool, a separate download … run `mailarchive setup`") | **Build before ship.** Proposed: point these at the Overview's Microsoft 365 connection form first, keeping the CLI as the secondary path, e.g. R-a → "Set up Microsoft 365 on the dashboard's **Microsoft 365 connection** card (or run `mailarchive setup` from the command line), then come back." Refusal copy is owner-authored (§7.6 / X2), so the exact wording is the owner's; these strings are test-covered, so the change ships prove-fail → prove-pass. |
| FR8 | III (BACKLOG, low) | No immediate validity check from the form — a wrong tenant/client surfaces only at the next Sign in / Archive now (a Graph error) | Nicety: an inline "Save & connect" or a validity probe would shorten the config → error loop. Low priority. |
| FR9 | I | On Linux/macOS the secret is a `0600` file (plaintext at rest), not an OS vault | Same posture as the CLI wizard (its FR5); the Windows deployment — the target — uses Credential Manager. |

## Verdict

**SHIPPABLE (code-walk)** for the in-app configuration form: the headline friction — a
command-line dependency to configure Microsoft 365 — is removed and test-covered at the
point of entry (FR1–FR3), and the always-on scope call (FR4) keeps setup reachable when
the export workspace is off. The refusals the feature does raise are legible and name
their remedy (cells 6, 7, 15).

Two items stand between this and **shippable without an asterisk**, neither a defect in
the form itself:

1. **FR7 (Type III, build before ship):** three "not configured" refusals still steer the
   user to the command line the feature was meant to retire. They should be reworded to
   point at the form; the copy is the owner's to author.
2. **The live Windows walk is pending** (the MA-377 install-drill family): a clean-machine
   configure → authenticate → first archive with no PowerShell. This review is a
   code-walk and does not certify that drill — flagged, not implied.
