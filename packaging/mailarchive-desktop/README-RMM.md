# MailArchive Desktop — in-repo MSI / RMM package

This folder is the WiX/RMM layer that turns our release payload into a deployable
`MailArchiveDesktop.msi`. The binaries come from **our** repo:

- `mailarchive-desktop.exe` — the dashboard (`127.0.0.1:8097`) + archive reader
  (`127.0.0.1:8099`), served **in-process**. It does not shell out to a child
  `mailarchive.exe` the way the earlier out-of-repo wrapper did.
- `mailarchive.exe` — the headless engine / CLI (app-only & multi-mailbox scheduled
  captures, plus every other verb).
- `mailarchive-gui.exe` — the native desktop GUI wizard.

The finished MSI embeds all three EXEs, branding, docs, and notices. **Endpoints
receive only the MSI.**

---

## A. Produce the payload (Alex / CI — Linux, no Windows needed)

`packaging/build-release.sh` already builds the three Windows x64 binaries (pure
cross-compile) and assembles the release asset:

    dist/mailarchive-desktop-windows-msi-payload.zip

Just run `make dist` (or `packaging/build-release.sh`). The zip contains the three
EXEs under their install names, `branding/mailarchive.ico`, `READ ME.txt`, and
`docs/` (LICENSE.txt, release-notes.md, goback.md, graph-app-setup.md).

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

   `-Version` **must stay greater than 1.1.0** (the earlier build) so this MSI cleanly
   upgrades it — see DC6 below. Bump it for each new build.

Build-MSI.ps1 extracts the payload, stages `SOURCE-NOTICE.txt`, `DEPLOYMENT.txt`,
`THIRD-PARTY-NOTICES.md`, `managed-install.marker`, and a regenerated
`PAYLOAD-SHA256.txt`, then runs WiX.

Output:
- `dist\MailArchiveDesktop-1.2.0-x64.msi`
- `dist\MailArchiveDesktop.msi`  (stable name — upload this to the RMM)

The script prints the MSI SHA256.

---

## C. Deploy via RMM

Silent install:
```cmd
msiexec.exe /i MailArchiveDesktop.msi /qn /norestart
```
Detection: `powershell -NoProfile -ExecutionPolicy Bypass -File .\Detect-RMM.ps1` (exit 0 = installed).
Silent uninstall: `powershell -NoProfile -ExecutionPolicy Bypass -File .\Uninstall-RMM.ps1`.

Install is per-machine to `C:\Program Files\MailArchive Desktop\` and creates: an
all-users Desktop shortcut, an all-users Startup shortcut, Start Menu + Uninstall
shortcuts, the Apps & Features entry, and `HKLM\Software\MailArchive Desktop`.

### DC6 — clean replacement of the earlier build
`Product.wxs` keeps the **same UpgradeCode** (`4CE5B43F-…-2D11`) and the **same**
`HKLM\Software\MailArchive Desktop` detection key as the 1.1.0 MSI, so any version
> 1.1.0 performs a `MajorUpgrade`: Windows Installer removes the old build and
installs this one with no conflict and no leftover. **Do not change the UpgradeCode.**
The ProductCode is auto-generated (`Id="*"`) per build — correct for MajorUpgrade.

### What uninstall preserves
Uninstall removes the Program Files payload, shortcuts, and MSI registry data. It
intentionally keeps the user's **mail archive**, `%LOCALAPPDATA%\MailArchiveDesktop`,
and the **Microsoft sign-in in Windows Credential Manager**. For full per-user
deprovisioning run `Cleanup-CurrentUser.ps1` in that user's Windows context.

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

2. **Desktop / Startup shortcuts.** After install, the Desktop and Start Menu
   shortcuts and the all-users **Startup** shortcut (`shell:common startup`) all
   launch `mailarchive-desktop.exe`. The binary is windowsgui (no console) but now
   opens the dashboard in the OS default browser itself once listening (MA-281), so
   a Desktop/Start-menu click should pop `http://127.0.0.1:8097/` straight away —
   verify the page appears. The **Startup** shortcut passes `-no-browser` so the
   at-login launch starts the background server quietly (no browser tab every
   sign-in); verify by then clicking the Desktop shortcut, which should reconnect to
   the running instance and open the browser to it (not error on the bound port).

3. **Weekly scheduled headless capture** (DC1). The weekly backup must run with the
   dashboard closed. Until the dashboard's own schedule action ships (slice S4),
   install the task by hand and verify it:

   ```cmd
   schtasks /Create /TN "MailArchive Desktop Weekly" /SC WEEKLY /D SUN /ST 03:00 ^
     /TR "\"C:\Program Files\MailArchive Desktop\mailarchive-desktop.exe\" -capture" /RL LIMITED /F
   schtasks /Run /TN "MailArchive Desktop Weekly"
   ```

   Confirm it ran from the saved sign-in with **no prompt**: see
   `%APPDATA%\mailarchive\desktop-capture.log` and that the archive grew. This is the
   product's real weekly backup (`mailarchive-desktop.exe -capture`), not a
   `mailarchive.exe` task.

Also smoke-test the DC6 upgrade: install the old 1.1.0 MSI first, then this one — it
should replace it with one Apps & Features entry and the archive/sign-in preserved.

---

## E. Fastest verification — bare `mailarchive-desktop.exe`, no MSI

To validate the dashboard without building or installing anything, run one binary.
It is already produced by `build-release.sh` as
`dist/mailarchive-desktop-windows-amd64.exe`, or build it directly:

```bash
GOOS=windows GOARCH=amd64 go build -o mailarchive-desktop.exe ./cmd/mailarchive-desktop
```

On Windows, **run it from a terminal** (PowerShell/cmd) so its startup line is
visible — it is a windowsgui binary, so a double-click starts it invisibly:

```
MailArchive Desktop — open this page in a browser:
  http://127.0.0.1:8097/
Archive reader: http://127.0.0.1:8099/
```

Open `http://127.0.0.1:8097/`, choose an archive folder, sign in with the device
code, capture. This exercises the **real** dashboard, the in-process engine, the
Credential Manager token path, and the reader — everything except the MSI's
install/shortcut/schedule plumbing. It needs no WiX, no admin, no RMM. Headless
capture can be tried the same way: `mailarchive-desktop.exe -capture`.

---

## Risks / open items

- **R1 — no-console, no browser-open. RESOLVED (MA-281).** `mailarchive-desktop.exe`
  is built `-H=windowsgui` (build-release.sh) with no console, so its stdout URL is
  invisible. The binary now opens the OS default browser to the dashboard itself once
  the server is listening, and if an instance is already running it reconnects and
  opens the browser to that one instead of failing to bind. So a shortcut click shows
  the page, never a silent server. An optional tray/stop UI remains possible future
  polish but is no longer needed for a visible launch. Lab: verify on Windows.
- **R2 — a real startup flag exists now (`-no-browser`). RESOLVED.** The old Startup
  shortcut passed `--startup`, which our binary rejected (exit 1). The binary now has
  a real `-no-browser` flag; the Startup shortcut passes it so the at-login launch
  starts the server quietly (no browser), while the Desktop/Start-menu shortcuts (no
  args) open the browser.
- **R3 — schedule install not yet in the dashboard.** Slice S4 (dashboard "install
  weekly schedule") is not wired yet, so the weekly task is installed by hand
  (section D.3). The engine-headless capture it calls (`-capture`) is implemented.
- **R4 — doc wording.** `README-payload.txt` (committed) says scheduled captures run
  `mailarchive.exe` via schtasks; the product's integrated weekly backup is actually
  `mailarchive-desktop.exe -capture`. Reconcile that wording in a follow-up.
