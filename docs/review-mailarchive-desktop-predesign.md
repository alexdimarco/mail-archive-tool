# Pre-code design review — `mailarchive-desktop`

**Design reviewed:** `docs/design-mailarchive-desktop.md` revision 1 (2026-10-02) +
the approved plan `~/.claude/plans/snazzy-sniffing-seal.md`. **Procedure:** the 10-lens
battery, finder → skeptic → rescue. **12 findings · 10 confirmed · 2 refuted.** The
rescue pass surfaced the hidden blocker (scheduled captures must not depend on the
dashboard).

**Verdict: GO_WITH_CONDITIONS.** Conditions **DC1..DC7** fold into design rev 2. No
NO_GO lens. The component is large but cleanly sliced; each slice ships independently
and carries its own gate (S2 adversarial, S4 friction, X-series UX contract).

## Hidden blocker (rescue pass) — scheduled captures must be engine-headless

In-process is right for interactive "Archive now", but a **scheduled** capture fires at
(say) 3am when the dashboard process may be closed. If the schedule drove the dashboard,
a closed dashboard = a missed backup. Resolution: the installed scheduled job runs the
**engine's headless capture** (the existing `mailarchive graph …` / a `--capture`
headless entry), independent of the dashboard UI process. The dashboard *installs and
manages* the schedule but is never in the scheduled-capture path. (DC1)

## Findings → conditions

| # | Lens | Claim | Verdict | Condition |
|---|---|---|---|---|
| F1 | 3,9 | A scheduled capture that depends on the open dashboard misses backups when it is closed | CONFIRMED (BLOCKER-class, dissolved) | **DC1** |
| F2 | 5 | Making `graph.NewDelegated`'s token pluggable could break the shipped device tests (MA-254…) | CONFIRMED | **DC2** (additive; file cache stays default) |
| F3 | 3 | The in-memory Activity log grows unbounded; a capture-goroutine panic kills the dashboard | CONFIRMED | **DC3** |
| F4 | 1,6 | Loopback + CSRF does not stop a local process running as the same user from driving the dashboard | CONFIRMED (no new boundary) | **DC4** (document; same user already has full rights) |
| F5 | 10 | The "sign-in in Credential Manager" claim is false off Windows (and before P4 is built) | CONFIRMED | **DC5** (OS-aware copy; ships with DC2) |
| F6 | 8 | Our MSI and Steve's MSI both claim `HKLM\Software\MailArchive Desktop` → upgrade conflict | CONFIRMED | **DC6** (Part C upgrade/product codes) |
| F7 | 2,5 | Two loopback ports (8097/8099) from one process — lifecycle/shutdown | CONFIRMED (low) | **DC7** |
| F8 | 6 | Device-code phishing via the dashboard-shown code | REFUTED | The dashboard initiates its OWN device code (`graph.NewDelegated`) and shows it; identical to the CLI device flow already adversarially cleared (D-C6) |
| F9 | 6 | The embedded reader widens R19 | REFUTED | `server.New` is reused unchanged; the archive/reader CSPs are untouched; the dashboard is a separate control surface (P2) |
| F10 | 4 | First-run with no config strands the user | CONFIRMED (low) | S2 embeds the setup wizard; the dashboard routes a no-config user there (folded into S2) |
| F11 | 6 | A capture writes outside the chosen archive path | REFUTED-ish | Reuses the engine's export path containment (R4); the dashboard only passes the user's `-out` (covered by existing invariants) |
| F12 | 7 | Surfacing `extract`/`reindex -rebuild` from the UI could delete/overwrite data | CONFIRMED (low) | `extract` is additive (writes to a separate `-dest`); `reindex -rebuild` renames into place on success (R13/R8 hold); the UI confirms destructive-sounding actions. Folded into S6 |

## Conditions (fold into rev 2)

- **DC1 — Scheduled capture is engine-headless.** The installed schedule runs the engine's
  headless capture, never the dashboard UI; the dashboard installs/manages the schedule
  only. A scheduled backup works whether or not the dashboard is open.
- **DC2 — Additive token store.** The `0600` file token cache stays the default/fallback;
  the Credential-Manager token store is an opt-in seam on Windows. Existing device tests
  (MA-254 etc.) are unaffected (prove by re-running them unchanged).
- **DC3 — Bounded + crash-safe runtime.** The Activity log is a bounded ring buffer; the
  capture runs in a goroutine with `recover`, so a crafted message or a long run never
  bloats or kills the dashboard (an error surfaces on the capture card).
- **DC4 — Honest trust boundary.** Documented: loopback + CSRF/Origin/Host defends against
  browser/network CSRF and DNS-rebind, NOT a local process running as the same user (which
  already has the user's rights and could run the engine directly). No new boundary claimed.
- **DC5 — OS-aware security copy.** The vault claim reads "Windows Credential Manager" on
  Windows and "a file only you can read" elsewhere; the "sign-in in Credential Manager"
  wording ships only once DC2's Windows token store lands (P4).
- **DC6 — Clean MSI upgrade (Part C).** Our MSI's UpgradeCode/ProductCode cleanly replace
  Steve's build (shared `HKLM\Software\MailArchive Desktop` detection); uninstall preserves
  the archive and the user's credentials (as Steve's does).
- **DC7 — Two-port lifecycle.** Dashboard `:8097` + reader `:8099` under one process; both
  refuse a non-loopback bind; one `http.Server` set with graceful shutdown on Ctrl-C.

## Mechanical check
`docs/design-mailarchive-desktop.md` BUILD STATUS is `proposed` and cites this review,
which exists. Each slice flips its own status and cites its gate artifact (S2 adversarial,
S4 friction) as it lands.
