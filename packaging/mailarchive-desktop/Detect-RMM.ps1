# RMM detection. Exit 0 = installed (prints version/path), exit 1 = not installed.
# Checks both files of the one-program PAIR — mailarchive.exe and mailarchivew.exe —
# under the unchanged HKLM\Software\MailArchive Desktop detection key (DC6: same key
# as the earlier build). It NAMES NO FORMER NAME, so it passes unchanged at this
# release and after the former-name copies are removed (§7.1, SC35). Replace any
# older stored rule that requires mailarchive-desktop.exe before deploying the
# release that removes the copies.
$k = 'HKLM:\Software\MailArchive Desktop'
if (Test-Path $k) {
    $v = Get-ItemProperty $k -ErrorAction SilentlyContinue
    if ($v.Installed -eq 1 -and (Test-Path (Join-Path $v.InstallDir 'mailarchive.exe')) -and (Test-Path (Join-Path $v.InstallDir 'mailarchivew.exe'))) {
        Write-Output "MailArchive Desktop $($v.Version) installed at $($v.InstallDir)"
        exit 0
    }
}
exit 1
