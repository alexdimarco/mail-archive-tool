[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'

# ADAPTED from Steve's Uninstall-RMM.ps1: stop our mailarchive-desktop.exe (was
# MailArchiveDesktop.exe). Normal MSI uninstall PRESERVES the user's mail archive,
# %LOCALAPPDATA%\MailArchiveDesktop, and the Microsoft sign-in token in Credential
# Manager (DC6). Use Cleanup-CurrentUser.ps1 for full per-user deprovisioning.
Get-Process mailarchive-desktop,mailarchive,mailarchive-gui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$roots = @(
  'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'
)
$product = Get-ItemProperty $roots -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -eq 'MailArchive Desktop' } |
    Select-Object -First 1

if (-not $product) {
    Write-Output 'MailArchive Desktop is not installed.'
    exit 0
}

$productCode = $product.PSChildName
$p = Start-Process msiexec.exe -ArgumentList @('/x', $productCode, '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
