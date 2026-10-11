[CmdletBinding()]
param([switch]$RemoveLocalAppState)
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
Get-Process mailarchive-desktop -ErrorAction SilentlyContinue |
    Where-Object { $_.SessionId -eq $session } |
    Stop-Process -Force -ErrorAction SilentlyContinue
$state = "$env:LOCALAPPDATA\MailArchiveDesktop"
foreach ($pattern in 'desktop*.lock', 'desktop-instance*.json', 'serve-*.json') {
    Remove-Item (Join-Path $state $pattern) -Force -ErrorAction SilentlyContinue
}

if ($RemoveLocalAppState) {
    Remove-Item "$env:LOCALAPPDATA\MailArchiveDesktop" -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item "$env:APPDATA\mailarchive" -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Output 'Current-user MailArchive authorization cleanup completed. Archive data was not deleted.'
