[CmdletBinding()]
param([string]$MsiPath = "$PSScriptRoot\MailArchiveDesktop.msi")
$ErrorActionPreference = 'Stop'
if (-not (Test-Path $MsiPath)) { throw "MSI not found: $MsiPath" }

# End running MailArchive Desktop instances BEFORE msiexec, in every signed-in
# session (as Uninstall-RMM.ps1 already does). An instance started by an earlier
# version keeps serving its dashboard on the old fixed address with no private
# link until its user signs out; stopping it here makes the fix take effect in each
# session at once (design-local-confidentiality CC22). The MSI's own
# util:CloseApplication ends any that remain. Only mailarchive-desktop is stopped: a
# scheduled mailarchive.exe capture that is mid-run is left to finish.
Get-Process mailarchive-desktop -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$p = Start-Process msiexec.exe -ArgumentList @('/i', ('"{0}"' -f $MsiPath), '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
