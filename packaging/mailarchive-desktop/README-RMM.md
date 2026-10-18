# MailArchive Desktop — in-repo MSI / RMM package

This folder is the WiX/RMM layer that turns our release payload into a deployable
`MailArchiveDesktop.msi`. MailArchive is **one program** with verbs; on Windows it
ships as a pair of files built from `./cmd/mailarchive`, plus two former-name copies
kept for this release. The binaries come from **our** repo:

- `mailarchive.exe` — the command-line build (console): the headless engine / CLI
  (app-only & multi-mailbox scheduled captures, plus every other verb).
- `mailarchivew.exe` — the no-console build of the same program. `mailarchivew.exe
  desktop` is the dashboard + archive reader, served **in-process**, each on a free
  loopback port of its own (there is no fixed address to type or bookmark) and each
  opened only through a private one-time link the app makes for its own Windows
  account; `mailarchivew.exe wizard` is the native-dialog wizard. It does not shell
  out to a child the way the earlier out-of-repo wrapper did.
- `mailarchive-desktop.exe` and `mailarchive-gui.exe` — **former names**, shipped as
  byte-identical copies of `mailarchivew.exe` so schedules and shortcuts installed
  under the old names keep running; a later release removes them (§7.1).

The finished MSI embeds the pair and the two former-name copies, branding, docs, and
notices. **Endpoints receive only the MSI.**

---

## A. Produce the payload (Alex / CI — Linux, no Windows needed)

`packaging/build-release.sh` builds the Windows x64 pair (pure cross-compile) and
the two former-name copies of `mailarchivew.exe`, and assembles the release asset:

    dist/mailarchive-desktop-windows-msi-payload.zip

Just run `make dist` (or `packaging/build-release.sh`). The zip contains the pair and
the former-name copies under their install names, `branding/mailarchive.ico`,
`READ ME.txt`, and `docs/` (LICENSE.txt, release-notes.md, goback.md, graph-app-setup.md).

Hand Steve: this `packaging/mailarchive-desktop/` folder **plus** that payload zip.

---

## B. Build the MSI (Steve — Windows, WiX 3)

Requirements: 64-bit Windows + PowerShell. **Go is not required** — the EXEs are in
the payload. WiX 3 is used if installed; otherwise Build-MSI.ps1 downloads the
portable WiX 3.14 tools into a private `.tools\` folder (needs internet once).

1. Put `mailarchive-desktop-windows-msi-payload.zip` beside `Build-MSI.ps1` (or let
   it find the unzipped folder, or a sibling repo `dist\`; or pass `-PayloadDir`).
2. Double-click **`BUILD-MSI.cmd`**, or run:

   ```powershell
   .\Build-MSI.ps1 -Version 1.2.0
   ```

   The version comes from the release tag, baked into the payload; `Build-MSI.ps1`
   reads it from there, so you need not pass `-Version` at all. If you do pass it, it
   must MATCH the payload or the build is refused (MA-614). It must be greater than
   1.1.0 (the earlier build) so this MSI cleanly upgrades it — see DC6 below.

Build-MSI.ps1 extracts the payload, stages `SOURCE-NOTICE.txt`, `DEPLOYMENT.txt`,
`THIRD-PARTY-NOTICES.md`, `managed-install.marker`, and a regenerated
`PAYLOAD-SHA256.txt`, then runs WiX.

Output:
- `dist\MailArchiveDesktop-1.2.0-x64.msi`
- `dist\MailArchiveDesktop.msi`  (stable name — upload this to the RMM)

The script prints the MSI SHA256.

---

## C. Deploy via RMM

Silent install (use the script: for an upgrade it ends older running dashboards
first, as the MSI's own CloseApplication does):
```cmd
powershell -NoProfile -ExecutionPolicy Bypass -File .\Install-RMM.ps1
```
or `msiexec.exe /i MailArchiveDesktop.msi /qn /norestart` directly.

**An upgrade takes effect at once, in every signed-in session.** Instances that were
already running before the upgrade served their dashboard with no private link until
their user signed out. `Install-RMM.ps1` stops every running `mailarchivew.exe`
dashboard and, for an upgrade, every running copy of the former name `mailarchive-desktop.exe`
(kept as a copy this release) before `msiexec`, and the MSI
ends any that remain (`util:CloseApplication`); the new version's own instance, started from a shortcut,
finds an older one still holding the former address `127.0.0.1:8097`, ends it when it
is that account's, and otherwise names it in the dashboard header and in `status` ("An
older MailArchive Desktop is still running … sign out and back in"). Typing the old
dashboard address `127.0.0.1:8097` no longer reaches MailArchive at all.

Detection: `powershell -NoProfile -ExecutionPolicy Bypass -File .\Detect-RMM.ps1` (exit 0 = installed).
Detect-RMM.ps1 names no former name, so it passes unchanged at this release and after
the copies are removed; **replace any older stored copy of the detection rule that
requires the former name `mailarchive-desktop.exe` before deploying the release that
removes the copies** (an older rule left in place reports every endpoint as not installed).
Silent uninstall: `powershell -NoProfile -ExecutionPolicy Bypass -File .\Uninstall-RMM.ps1`.

Install is per-machine to `C:\Program Files\MailArchive Desktop\` and creates: an
all-users Desktop shortcut, an all-users Startup shortcut, Start Menu + Uninstall
shortcuts, a Start Menu **MailArchive wizard** shortcut (`wizard`), a Start Menu
**Copy MailArchive link** shortcut (`-copy-link`: shows a fresh private link in a
dialog with a Copy button, for a default browser that cannot be opened), the Apps &
Features entry, and `HKLM\Software\MailArchive Desktop`.

Each Windows account runs its own instance, and each Terminal Services session of an
account (an RDS host, fast user switching) its own: the lock and instance file live
in `%LOCALAPPDATA%\MailArchiveDesktop` (not roaming, not a cache folder a cleaner
purges), so a second account never reaches the first account's dashboard, and a
second session's shortcut opens a browser in that session.

### DC6 — clean replacement of the earlier build
`Product.wxs` keeps the **same UpgradeCode** (`4CE5B43F-…-2D11`) and the **same**
`HKLM\Software\MailArchive Desktop` detection key as the 1.1.0 MSI, so any version
> 1.1.0 performs a `MajorUpgrade`: Windows Installer removes the old build and
installs this one with no conflict and no leftover. **Do not change the UpgradeCode.**
The ProductCode is auto-generated (`Id="*"`) per build — correct for MajorUpgrade.

### What uninstall preserves
Uninstall removes the Program Files payload, shortcuts, and MSI registry data. It
intentionally keeps the user's **mail archive**, `%LOCALAPPDATA%\MailArchiveDesktop`
(the per-account lock and instance files), and the **Microsoft sign-in in Windows
Credential Manager**. For full per-user deprovisioning run `Cleanup-CurrentUser.ps1`
in that user's Windows context; it also removes the lock and instance files.

---

## D. What Steve is validating (NOT testable on our Linux CI)

Our CI proves the loopback refusal, CSRF/Origin/Host guard, status cards, in-process
capture against a fake Graph server, and the file token store. The Windows-only
integration below is **lab-pending** and is exactly what this round covers:

1. **Credential Manager token path** (MA-264, lab). Sign in from the dashboard, then:
   - `cmdkey /list` shows a `mailarchive:graph-token:…` generic credential.
   - There is **no** token file under `%APPDATA%\mailarchive\`.
   - Reopen the dashboard — Overview shows **Signed in** with no new prompt (read
     back from the vault via `CredReadW`).
   - "Clear sign-in" removes the `cmdkey` entry.

2. **Desktop / Startup shortcuts and the install check.** After install, the Desktop
   and Start Menu shortcuts and the all-users **Startup** shortcut (`shell:common
   startup`) all launch `mailarchivew.exe desktop`. The binary is windowsgui (no
   console) but opens the dashboard in the OS default browser itself once listening
   (MA-281), at a private one-time link, so a Desktop/Start-menu click should pop the
   dashboard straight away — **verify the install this way: open MailArchive Desktop
   from its shortcut and see the page.** The **Startup** shortcut passes
   `-no-browser` so the at-login launch starts the background server quietly (no
   browser tab every sign-in); verify by then clicking the Desktop shortcut, which
   should reconnect to the running instance — after it proves itself — and open the
   browser to it (not error, and not start a second one). Then verify the lock: in a
   browser, type `http://127.0.0.1:<port>/` for a port the app is listening on (`netstat
   -ano` shows it beside the pid of `mailarchivew.exe` in Task Manager) — it
   must show the **locked page** ("This MailArchive Desktop page has to be opened from the app"), never the
   dashboard. Typing the old address `127.0.0.1:8097` shows nothing of MailArchive.

   A broken default-browser association: the launch shows the link in a dialog with a
   Copy button; the Start Menu **Copy MailArchive link** shortcut does the same on
   demand. Check both.

   Multi-account (fast user switching, an RDS host): sign in as two test accounts and
   confirm each sees its own dashboard, that account B's browser at account A's port
   sees only the locked page, and that the upgrade ending running instances (above)
   holds for sessions that were running during it. This is the lab drill MA-377
   (pending, with a friction log).

3. **Weekly scheduled headless capture** (DC1, slice S4 shipped — MA-279). The
   dashboard installs/updates/removes the weekly backup itself (the **Weekly backup**
   control), writing a Task Scheduler entry that runs `mailarchivew.exe desktop
   -capture` so the backup runs with the dashboard closed. The task name is derived
   per archive — `mailarchive-<hash>` (`schedule.DefaultNameFor`) — not a fixed
   string. Verify end-to-end from the UI:

   - Set a weekly day/time in the dashboard and save; Overview's **weekly backup**
     card flips to *Installed*.
   - `schtasks /Query /FO LIST | findstr mailarchive-` shows the task.
   - Run it (`schtasks /Run /TN mailarchive-<hash>`) and confirm it ran from the
     saved sign-in with **no prompt** — see `%APPDATA%\mailarchive\desktop-capture.log`
     and that the archive grew.

   A by-hand `schtasks /Create … /TR "\"…\mailarchivew.exe\" desktop -capture"` is only
   a fallback if the dashboard reports the scheduler unavailable. Either way the task
   runs `mailarchivew.exe desktop -capture` (the product's real weekly backup), never a
   bare command-line task.

Also smoke-test the DC6 upgrade: install the old 1.1.0 MSI first, then this one — it
should replace it with one Apps & Features entry and the archive/sign-in preserved.

---

## E. Fastest verification — the bare no-console build (`mailarchivew.exe`), no MSI

To validate the dashboard without building or installing anything, run one file. It is
already produced by `build-release.sh` as `mailarchivew.exe` in the payload, or build
it directly (`make build-windows`, or by hand):

```bash
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchivew.exe ./cmd/mailarchive
```

On Windows, **run `mailarchive.exe desktop` from a terminal** (PowerShell/cmd) so its
startup lines are visible — `mailarchivew.exe` is a windowsgui build, so a double-click
starts it invisibly. A terminal's `mailarchive.exe desktop` holds that terminal and
ends with it; `mailarchivew.exe desktop` outlives it.

```
MailArchive Desktop is running: dashboard on 127.0.0.1:<port> and archive reader on 127.0.0.1:<port>, each opened only through a private link.
If your browser does not open, run: mailarchive desktop -copy-link
```

It opens your browser at a private one-time link; if that fails a dialog shows the
link, and `mailarchivew.exe desktop -copy-link` shows a fresh one. Choose an archive
folder, sign in with the device code, capture. This exercises the **real** dashboard, the in-process engine, the
Credential Manager token path, and the reader — everything except the MSI's
install/shortcut/schedule plumbing. It needs no WiX, no admin, no RMM. Headless
capture can be tried the same way: `mailarchivew.exe desktop -capture`.

---

## Risks / open items

- **R1 — no-console, no browser-open. RESOLVED (MA-281).** `mailarchivew.exe`
  is built `-H=windowsgui` (build-release.sh) with no console, so its stdout URL is
  invisible. The binary now opens the OS default browser to the dashboard itself once
  the server is listening, and if an instance of this account is already running it
  reconnects to it — after a challenge-response proves it is that account's own — and
  has it open the browser, instead of failing to bind. So a shortcut click shows the
  page, never a silent server; and every refusal (another configuration, an instance
  that does not respond, a port already in use) is shown in a dialog, since there is
  no console. An optional tray/stop UI remains possible future
  polish but is no longer needed for a visible launch. Lab: verify on Windows.
- **R2 — a real startup flag exists now (`-no-browser`). RESOLVED.** The old Startup
  shortcut passed `--startup`, which our binary rejected (exit 1). The binary now has
  a real `-no-browser` flag; the Startup shortcut passes it so the at-login launch
  starts the server quietly (no browser), while the Desktop/Start-menu shortcuts (no
  args) open the browser.
- **R3 — schedule install in the dashboard. RESOLVED (S4 / MA-279).** The dashboard's
  Weekly backup control installs/updates/removes the task in-process via
  `internal/schedule` (cross-platform schtasks/cron/launchd), running
  `mailarchivew.exe desktop -capture`. The by-hand schtasks of section D.3 is now only
  a scheduler-unavailable fallback.
- **R4 — doc wording. RESOLVED (Part C).** `README-payload.txt` now states the
  integrated weekly backup runs `mailarchivew.exe desktop -capture` in-process (DC1),
  not a bare command-line schtasks job.
