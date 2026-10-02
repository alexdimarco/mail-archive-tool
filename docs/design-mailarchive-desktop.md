# Design — `mailarchive-desktop`: an in-repo, in-process dashboard-and-launcher

**Revision:** 2 (2026-10-02). **BUILD STATUS:** approved with conditions, building —
the 10-lens pre-code review is filed as `docs/review-mailarchive-desktop-predesign.md`
(GO_WITH_CONDITIONS, DC1..DC7 folded here). Key conditions: **DC1** scheduled captures
run the engine headless (never the dashboard UI), so a scheduled backup fires whether or
not the dashboard is open; **DC2** the token store is additive (the `0600` file cache
stays the default, Credential-Manager is an opt-in Windows seam, existing device tests
unaffected); **DC3** bounded Activity log + capture-goroutine `recover`; **DC4** the
loopback+CSRF boundary is documented as not defending against a same-user local process;
**DC5** OS-aware security copy; **DC6** clean MSI upgrade over Steve's build; **DC7**
two-port lifecycle under one process. Full scope + owner rulings:
`~/.claude/plans/snazzy-sniffing-seal.md` (approved 2026-10-02). Slice a (S2) carries its
adversarial pass (`docs/review-mailarchive-desktop-adversarial.md`, PASS); S4 its friction
review (`docs/review-mailarchive-desktop-friction.md`, PASS). R1 (windowsgui launch) is
fixed by MA-281 (browser-open on start). All slices S0–S6 + Part B/C built, gate-green.

**Owner rulings:** in-process engine (NOT shell-out), cross-platform core / Windows-first
integration, packaging (MSI/RMM) brought in too, plus a button-label clarity pass and a
feature-gap review (surface verify/reindex/extract/Deleted-Junk/app-only that Steve's
wrapper omits).

## 1. Problem

Steve's **MailArchive Desktop** (a Windows MSI wrapper, out-of-repo) is the product users
see: an always-on local dashboard (`127.0.0.1:8097`) that **shells out** to our
`mailarchive.exe` for every action and drives `schtasks`/Credential Manager/shortcuts. We
own the engine, so shelling out re-implements orchestration we can do natively and couples
us to a child binary's PATH/version/output format. We want the dashboard as a first-class,
owned, gated, cross-platform component — `cmd/mailarchive-desktop` — that calls our internal
packages directly, and clear security language so a user trusts it with their mailbox.

## 2. Properties

- **P1 — One binary, in-process (OR: in-process).** Every dashboard action calls an internal
  package, not a child process: capture `app.RunGraph`; device sign-in `graph.NewDelegated`
  (the `Prompt` callback streams the verification URL + user code to the page); the reader
  `server.New`; status `health.Gather`/`Assess`; scheduling `internal/schedule`;
  verify/reindex/extract via `internal/app`. No `mailarchive.exe` child, no output parsing,
  no two-exe version skew. (new X/MA rows)
- **P2 — Loopback-only control surface (R19-aligned).** The dashboard binds only loopback and
  refuses any other address (reuse `server.IsLoopback`), exactly like `mailarchive setup`. It
  is a *control* surface (it captures, signs in, schedules), so every state-changing POST is
  CSRF-tokened and Origin/Host-guarded (the `setup` pattern, W-C2). The embedded reader keeps
  the archive/reader CSPs unchanged (R19). (new MA rows; X9)
- **P3 — Cross-platform core, Windows-first integration (OR).** The dashboard + in-process
  engine run on any OS (pure Go, no cgo, CI-tested on Linux). OS-specific integration is
  build-tagged: the credential vault (`graphconfig.SecretStore` — Credential Manager on
  Windows, `0600` file elsewhere), Desktop/Startup shortcuts (Windows; a no-op with a legible
  "not applicable on this OS" elsewhere), and the MSI (Windows). Scheduling already spans
  `schtasks`/`launchd`/`crontab` (`internal/schedule`). (new MA rows)
- **P4 — The sign-in lives in the OS vault (honesty linchpin).** The device **refresh token**
  — today a `0600` file (`graph.Config.TokenCachePath`) — is stored via a pluggable token
  store so on Windows it lives in **Credential Manager** (the file remains the cross-platform
  fallback). Only with this is the P8 claim "your sign-in is in Credential Manager" true.
  (new MA rows)
- **P5 — Status is the same truth as `status` (X6).** The four cards (archive engine / Microsoft
  sign-in / capture / weekly schedule) and the detail come from `health.Gather`/`Assess` +
  config/token presence — the identical GREEN/WARN/RED the `status` verb reports, so the
  dashboard never invents a second posture. (new MA rows; X10)
- **P6 — Plain, action-first labels (button-label pass).** "Archive now", "Sign in, then
  archive", "Open archive", "Go back in time (see the mailbox on a past date)", "Status &
  health", "Activity log", "Recreate shortcuts", "Microsoft 365 connection" — no "reader",
  "deployment", "engine" jargon. (X11)
- **P7 — Surfaces the whole engine (feature-gap review).** Beyond Steve's set: **Check archive
  integrity** (`verify`, R20), **Rebuild search index** (`reindex -rebuild`), **Export a copy**
  (`extract` mbox/eml), a **Deleted Items / Junk** capture toggle (T7), and an **app-only
  (multi-mailbox)** option alongside device (reusing the setup wizard). (new MA rows)
- **P8 — Security & privacy, stated and true.** A dashboard panel (and shorter blurbs in the
  `setup` wizard and the native GUI) states only verifiable claims: local-only + loopback
  (P2), read-only to the mailbox (R17 GET-only), sign-in in the OS vault (P4), no third-party
  servers, own-mailbox-only on device (`/me`), offline-safe pages (R19 `ArchiveCSP`), auditable
  + reversible (revoke / uninstall keeps the archive). (new MA rows; Part B)

## 3. Mechanism (slices — full detail in the plan)

- **S0** this design + gate. **S1** skeleton: loopback server (non-loopback refused), sidebar
  nav, status cards (`health`), connection display, Security panel. **S2** config + device
  sign-in in-process + token→vault (P4); **adversarial pass** filed (review-…-adversarial.md,
  PASS). **S3** archive-now (`app.RunGraph` goroutine) + Activity log + settings
  (location/day/time/raw/Deleted-Junk).
  **S4** schedule install/update/remove (`internal/schedule`); **friction review** filed
  (review-…-friction.md, PASS).
  **S5** embedded reader (`server.New`) + go-back. **S6** maintenance (verify/reindex/extract)
  + Windows shortcuts/startup (build-tagged). **Part C** packaging (MSI/RMM into
  `packaging/mailarchive-desktop/`, single-binary; wired into `build-release.sh`).

Lifecycle: the dashboard and the embedded reader run under one process; a capture runs in a
goroutine with a cancelable context; a single `http.Server` set with graceful shutdown on
Ctrl-C. The restyled design tokens (`internal/server`) are reused for the look.

## 4. Invariants touched
- **R19 unchanged for the reader/archive**; the dashboard is a *new* loopback control surface
  with its own guarantees (P2) carried by new MA rows — not a widening of the reader `serve`.
- **UX contract (new X-series):** **X9** the dashboard binds loopback-only and CSRF/Origin/Host-
  guards every state change; **X10** its status mirrors `status`'s GREEN/WARN/RED (one truth);
  **X11** plain action-first labels, no crash reaches the user (recover → legible card error).
- **New scenario(s) + MA rows per slice** (catalog-first); Windows-only pieces (Credential
  Manager token/secret, shortcuts, MSI) are tier-**L** (lab), consistent with MA-64/79/95/264.

## 5. Honesty of claims
- **Proven on Linux CI (tier U):** loopback-only refusal, CSRF/Origin/Host guard, status cards
  from a seeded `health.Input`, in-process capture against the fake Graph server, the file
  token store, the security-panel content, label/no-crash behavior.
- **Lab-pending (tier L, Windows):** Credential Manager token+secret, Desktop/Startup shortcuts,
  the MSI build/install/uninstall, and a real-tenant device sign-in.
- **Conditional (stated inline):** "sign-in in Credential Manager" holds on Windows once P4 is
  built; elsewhere it is "a file only you can read."
