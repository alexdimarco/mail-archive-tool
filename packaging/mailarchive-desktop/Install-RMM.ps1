[CmdletBinding()]
param([string]$MsiPath = "$PSScriptRoot\MailArchiveDesktop.msi")
$ErrorActionPreference = 'Stop'
if (-not (Test-Path $MsiPath)) { throw "MSI not found: $MsiPath" }

# End running dashboards BEFORE msiexec, in every signed-in session (as
# Uninstall-RMM.ps1 does). An instance from an earlier version keeps serving its
# dashboard on the old fixed address with no private link until its user signs out;
# stopping it here makes the fix take effect in each session at once (CC22). The
# MSI's own util:CloseApplication ends any that remain.
#
# The former name mailarchive-desktop.exe (a copy this release) is stopped
# unconditionally — its only role now is that pre-upgrade dashboard. A running
# mailarchivew.exe is stopped ONLY when it is a dashboard: no argument after the
# program path, or a first argument `desktop` without `-capture`. A run-job, a
# -job, a -capture, a `desktop -capture` (the weekly backup) or a `wizard` is NEVER
# stopped — a mid-run scheduled backup or an open wizard is left to finish
# (MA-614, SC34). No mailarchive-gui is stopped.
Get-Process mailarchive-desktop -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

Get-CimInstance Win32_Process -Filter "Name = 'mailarchivew.exe'" -ErrorAction SilentlyContinue | ForEach-Object {
    $cmd = $_.CommandLine
    if (-not $cmd) { return }
    # Strip the program path (quoted or bare) to leave just the arguments.
    $cmdArgs = ($cmd -replace '^\s*("[^"]*"|\S+)\s*', '')
    $isDashboard = ($cmdArgs.Trim() -eq '') -or
                   ($cmdArgs -match '^desktop(\s|$)' -and $cmdArgs -notmatch '(^|\s)-capture(\s|$)')
    if ($isDashboard) { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

$p = Start-Process msiexec.exe -ArgumentList @('/i', ('"{0}"' -f $MsiPath), '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
