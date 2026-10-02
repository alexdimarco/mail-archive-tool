# RMM detection. Exit 0 = installed (prints version/path), exit 1 = not installed.
# ADAPTED from Steve's Detect-RMM.ps1: checks for our mailarchive-desktop.exe (was
# MailArchiveDesktop.exe) alongside the unchanged HKLM\Software\MailArchive Desktop
# detection key (DC6: same key as Steve's build).
$k = 'HKLM:\Software\MailArchive Desktop'
if (Test-Path $k) {
    $v = Get-ItemProperty $k -ErrorAction SilentlyContinue
    if ($v.Installed -eq 1 -and (Test-Path (Join-Path $v.InstallDir 'mailarchive-desktop.exe')) -and (Test-Path (Join-Path $v.InstallDir 'mailarchive.exe'))) {
        Write-Output "MailArchive Desktop $($v.Version) installed at $($v.InstallDir)"
        exit 0
    }
}
exit 1
