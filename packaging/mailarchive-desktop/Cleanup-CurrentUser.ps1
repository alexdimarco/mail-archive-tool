[CmdletBinding()]
param([switch]$RemoveLocalAppState, [switch]$ForgetEncryptedArchives)
$ErrorActionPreference = 'Stop'

# Credential Manager entries are user scoped. Run this AS the user who signed in.
# ADAPTED from Steve's script: our Windows vault (internal/graphconfig/
# secretstore_windows.go) writes generic credentials whose TargetName is
# "mailarchive:" + account, i.e. "mailarchive:graph-token:..." (the delegated
# sign-in token) and "mailarchive:graph:..." (the client secret, app-only). Match
# that prefix in `cmdkey /list`, strip cmdkey's LegacyGeneric wrapper, and delete.
# Match the real target off the NON-localized 'LegacyGeneric:target=' token + the
# credential name, not the localized 'Target:' field label (which is 'Ziel:' on
# German Windows etc.) — otherwise the sign-in token / secret would be left in the
# vault on non-English Windows (credential residue). This never deletes anything
# but our own 'mailarchive:graph*' entries.
$targets = cmdkey.exe /list |
    Select-String 'LegacyGeneric:target=(mailarchive:graph[^\s]*)' |
    ForEach-Object { $_.Matches.Groups[1].Value } |
    Select-Object -Unique

foreach ($target in $targets) {
    if ($target) { cmdkey.exe /delete:$target | Out-Null }
}

# This user's running MailArchive Desktop (this sign-in session only) holds the
# per-account lock in %LOCALAPPDATA%\MailArchiveDesktop; end it, then remove the lock
# and the instance files — where the instance listens and the key a later launch
# proves itself with — so nothing of a finished deployment is left to be believed
# (design-local-confidentiality CC22). The next start makes new ones.
$session = (Get-Process -Id $PID).SessionId
Get-Process mailarchivew,mailarchive-desktop,mailarchive-gui -ErrorAction SilentlyContinue |
    Where-Object { $_.SessionId -eq $session } |
    Stop-Process -Force -ErrorAction SilentlyContinue
$state = "$env:LOCALAPPDATA\MailArchiveDesktop"
foreach ($pattern in 'desktop*.lock', 'desktop-instance*.json', 'serve-*.json') {
    Remove-Item (Join-Path $state $pattern) -Force -ErrorAction SilentlyContinue
}

# This computer's record of the encrypted archives it made (encrypted-archives.rec,
# its mirror, lock and lost-record list; design-encrypted-archives-phase3 §4.1) is
# KEPT, like the remembered keys of encrypted archives: without it a folder whose key
# files were deleted could read as unencrypted here. Only -ForgetEncryptedArchives
# removes them — the remembered keys first ("mailarchive:archive:..." in Credential
# Manager), then the record, so an interruption never leaves keys the record no
# longer names.
if ($ForgetEncryptedArchives) {
    $keys = cmdkey.exe /list |
        Select-String 'LegacyGeneric:target=(mailarchive:archive:[^\s]*)' |
        ForEach-Object { $_.Matches.Groups[1].Value } |
        Select-Object -Unique
    foreach ($key in $keys) {
        if ($key) { cmdkey.exe /delete:$key | Out-Null }
    }
    Remove-Item (Join-Path $state 'encrypted-archives.*') -Force -ErrorAction SilentlyContinue
}

if ($RemoveLocalAppState) {
    Get-ChildItem $state -Force -ErrorAction SilentlyContinue |
        Where-Object { $ForgetEncryptedArchives -or $_.Name -notlike 'encrypted-archives.*' } |
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item "$env:APPDATA\mailarchive" -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Output 'Current-user MailArchive authorization cleanup completed. Archive data was not deleted.'
