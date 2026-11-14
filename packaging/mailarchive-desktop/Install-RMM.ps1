[CmdletBinding()]
param([string]$MsiPath = "$PSScriptRoot\MailArchiveDesktop.msi")
$ErrorActionPreference = 'Stop'

# End running dashboards BEFORE msiexec, in every signed-in session (as
# Uninstall-RMM.ps1 does). An instance from an earlier version keeps serving its
# dashboard on the old fixed address with no private link until its user signs out;
# stopping it here makes the fix take effect in each session at once (CC22). The
# MSI's own util:CloseApplication ends any that remain.
#
# The former name mailarchive-desktop.exe (a copy this release) is stopped
# unconditionally — its only role now is that pre-upgrade dashboard. A running
# mailarchivew.exe is a dashboard when it has no argument after the program path, a
# first argument `desktop` without `-capture`, or a first argument `start` (with or
# without -background: the running MailArchive the shortcuts start since
# design-menubar slice S1, MB-7). A run-job, a -job, a -capture, a `desktop -capture`
# (the weekly backup) or a `wizard` is NEVER stopped — a mid-run scheduled backup or
# an open wizard is left to finish (MA-614, SC34). No mailarchive-gui is stopped.
#
# Since slice S1 a dashboard also hosts the wizard and Run now, so it is asked to close
# the graceful way, by the installed program itself (the S1 fix round 2, B1, B2, B5-B7):
# a one-time task of the copy's own user runs the installed `mailarchivew.exe quit`,
# which asks the copy over its signed hand-off (no window messages, no PowerShell in
# that session: it works under Constrained Language mode and from any sign-in of that
# user). First every copy is only asked whether it would close (`quit -check`); only
# when all say yes is each asked to close, and each closes after its own drain. A copy
# that runs work or an open wizard says no: then nothing is closed or forced, the
# upgrade is not started, the busy process is named, and the script exits 1618 (another
# installation is in progress), so the RMM tool tries again later. A copy that cannot be
# asked is never called busy and never forced: the script names it and why, starts no
# upgrade, and exits 3. Only a copy proven to be from before slice S1 is ended at once,
# as before: by its own record (`quit` answers 6), or, when the installed MailArchive
# itself predates the quit request (no QuitVerb value in its registry key), by running
# that installed program. The task has a random name, a security descriptor that lets
# only SYSTEM and Administrators change it, an execution limit and an expiry after which
# Task Scheduler deletes it, and it is removed as soon as it has answered.

# --- functions ---

# What `mailarchive quit` answers (internal/desktop/quitverb.go).
$QuitDone = 0; $QuitBusy = 3; $QuitNotRunning = 4; $QuitOlder = 6; $QuitStillClosing = 7
# This script's own outcome when a running copy could not be asked (besides msiexec's).
$ExitCouldNotAsk = 3

$taskXml = @'
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>MailArchive upgrade: asks the running MailArchive to close. It removes itself.</Description>
    <SecurityDescriptor>D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;{2})</SecurityDescriptor>
  </RegistrationInfo>
  <Triggers>
    <TimeTrigger>
      <StartBoundary>{3}</StartBoundary>
      <EndBoundary>{4}</EndBoundary>
      <Enabled>true</Enabled>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>{2}</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT2M</ExecutionTimeLimit>
    <DeleteExpiredTaskAfter>PT0S</DeleteExpiredTaskAfter>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{0}</Command>
      <Arguments>{1}</Arguments>
    </Exec>
  </Actions>
</Task>
'@

function ConvertTo-XmlText([string]$s) {
    return $s.Replace('&', '&amp;').Replace('<', '&lt;').Replace('>', '&gt;').Replace('"', '&quot;')
}

# Get-InstallValue reads a value of the MSI's key in the 64-bit registry view (an RMM
# agent may run 32-bit PowerShell), "" when absent.
function Get-InstallValue([string]$name) {
    $ErrorActionPreference = 'SilentlyContinue'
    $out = & reg.exe query 'HKLM\Software\MailArchive Desktop' /v $name /reg:64 2>$null
    foreach ($l in @($out)) {
        if ($l -match ('^\s+' + $name + '\s+REG_\w+\s+(.*)$')) { return $Matches[1].Trim() }
    }
    return ''
}

# Test-Dashboard: a mailarchivew command line that is a dashboard (see the top).
function Test-Dashboard([string]$cmd) {
    if (-not $cmd) { return $false }
    # Strip the program path (quoted or bare) to leave just the arguments.
    $cmdArgs = ($cmd -replace '^\s*("[^"]*"|\S+)\s*', '')
    return ($cmdArgs.Trim() -eq '') -or
           ($cmdArgs -match '^desktop(\s|$)' -and $cmdArgs -notmatch '(^|\s)-capture(\s|$)') -or
           ($cmdArgs -match '^start(\s|$)')
}

# Invoke-AsCopyUser runs the installed program with $arguments as the copy's own user,
# in a sign-in of that user, through a one-time task, and returns its exit status, or -1
# when the task could not be made. A status above 255 is Task Scheduler's own result
# (the program never ran to its end). It uses schtasks.exe, not the ScheduledTasks
# module, which Constrained Language mode can keep from loading.
function Invoke-AsCopyUser($proc, [string]$exe, [string]$arguments, [int]$bound) {
    $ErrorActionPreference = 'SilentlyContinue'
    $owner = Invoke-CimMethod -InputObject $proc -MethodName GetOwnerSid
    if ($owner.ReturnValue -ne 0 -or -not ($owner.Sid -match '^S-1-[0-9-]+$')) { return -1 }
    $sid = [string]$owner.Sid
    $name = 'MailArchive upgrade ' + (New-Guid).Guid
    $now = Get-Date
    $xml = $taskXml -f (ConvertTo-XmlText $exe), (ConvertTo-XmlText $arguments), $sid, $now.AddDays(-1).ToString('s'), $now.AddMinutes(15).ToString('s')
    # The definition goes through a file of SYSTEM's own temporary folder, under the
    # task's random name.
    $file = Join-Path $env:windir ('Temp\' + $name + '.xml')
    $code = -1
    try {
        Set-Content -LiteralPath $file -Value $xml -Encoding Unicode
        # No /f: a task of that name made by anyone else first makes this fail.
        & schtasks.exe /create /tn "\$name" /xml $file 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { return -1 }
        $made = (& schtasks.exe /query /tn "\$name" /xml 2>$null) -join "`n"
        # The task made is this one: one action, the installed program, these arguments.
        if (-not $made.Contains('<Command>' + (ConvertTo-XmlText $exe) + '</Command>') -or -not $made.Contains('<Arguments>' + (ConvertTo-XmlText $arguments) + '</Arguments>') -or ($made -split '<Exec>').Count -ne 2) { return -1 }
        & schtasks.exe /run /tn "\$name" 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { return -1 }
        $code = -2  # until it answers within the bound
        for ($i = 0; $i -lt $bound; $i++) {
            Start-Sleep -Seconds 1
            # The verbose CSV row's seventh column is the last result, whatever the
            # display language: 267011 (0x41303) not run yet, 267009 (0x41301) running.
            $row = @(& schtasks.exe /query /tn "\$name" /fo csv /v /nh 2>$null) | Select-Object -First 1
            $cols = ([string]$row).Trim().Trim('"') -split '","'
            if ($cols.Count -lt 7 -or $cols[6] -notmatch '^-?\d+$') { continue }
            $ran = [long]$cols[6]
            if ($ran -lt 0) { $ran += 4294967296 }  # an HRESULT, shown signed
            if ($ran -ne 267011 -and $ran -ne 267009) { $code = $ran; break }
        }
    } finally {
        & schtasks.exe /delete /tn "\$name" /f 2>$null | Out-Null
        Remove-Item -LiteralPath $file -Force -ErrorAction SilentlyContinue
    }
    return $code
}

# The words for a copy that could not be asked, from what the task returned.
function Get-CouldNotAskReason([long]$r) {
    if ($r -eq -2) { return 'the one-time task in its user''s sign-in did not finish in time' }
    if ($r -lt 0) { return 'the one-time task in its user''s sign-in could not be made' }
    if ($r -gt 255) { return ('the one-time task in its user''s sign-in could not run (result 0x{0:X})' -f $r) }
    return "MailArchive could not reach it (quit answered $r)"
}

# Close-RunningMailArchive asks every running dashboard of $ProcessName to close, all or
# none, and returns @{ Code = 0 | 1618 | 3; Lines = what to print }. $Exe is the installed
# mailarchivew.exe, $QuitVerb whether it has the `quit` verb, $TrustedRoot the folder only
# administrators can write that it must be in (it is run as each copy's user).
function Close-RunningMailArchive([string]$ProcessName, [string]$Exe, [bool]$QuitVerb, [string]$TrustedRoot) {
    $lines = @(); $ask = @(); $force = @(); $busy = @(); $cannot = @()
    $root = $TrustedRoot.TrimEnd('\') + '\'
    $trusted = [bool]$Exe -and ($Exe -notmatch '\.\.') -and $Exe.ToLowerInvariant().StartsWith($root.ToLowerInvariant()) -and (Test-Path -LiteralPath $Exe -PathType Leaf)
    foreach ($proc in @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName'" -ErrorAction SilentlyContinue)) {
        if (-not (Test-Dashboard $proc.CommandLine)) { continue }
        $id = [int]$proc.ProcessId
        if (-not $QuitVerb) {
            # The installed MailArchive predates the quit request, so a copy running it is
            # from before slice S1 (it has nothing to ask with): ended at once, as before.
            if ($Exe -and $proc.ExecutablePath -and $proc.ExecutablePath.ToLowerInvariant() -eq $Exe.ToLowerInvariant()) { $force += $proc }
            else { $cannot += "process ${id}: it runs from $($proc.ExecutablePath), not from the installed MailArchive, which cannot ask it" }
            continue
        }
        if (-not $trusted) { $cannot += "process ${id}: MailArchive is not installed under $root, so its program is not run as another user to ask"; continue }
        $r = Invoke-AsCopyUser $proc $Exe "quit -check -pid $id" 45
        if ($r -eq $QuitDone) { $ask += $proc }
        elseif ($r -eq $QuitBusy) { $busy += $id }
        elseif ($r -eq $QuitNotRunning) { }  # not (or no longer) its account's running copy: nothing to close
        elseif ($r -eq $QuitOlder) { $force += $proc }  # its own record: from before slice S1
        else { $cannot += "process ${id}: " + (Get-CouldNotAskReason $r) }
    }
    if (-not $busy -and -not $cannot) {
        foreach ($proc in $ask) {
            $id = [int]$proc.ProcessId
            $r = Invoke-AsCopyUser $proc $Exe "quit -pid $id -wait 30s" 75
            if ($r -eq $QuitDone) { $lines += "Closed MailArchive (process $id) after it finished its work." }
            elseif ($r -eq $QuitNotRunning) { }
            elseif ($r -eq $QuitBusy -or $r -eq $QuitStillClosing) { $busy += $id }  # work began since it was asked
            else { $cannot += "process ${id}: " + (Get-CouldNotAskReason $r) }
        }
    }
    if ($busy -or $cannot) {
        foreach ($id in $busy) {
            $lines += "MailArchive is busy in a signed-in session (process $id): it is running a backup or another task, or its wizard is open. It was left running and the upgrade was not started. Run this script again later."
        }
        foreach ($c in $cannot) {
            $lines += "MailArchive could not be asked to close in a signed-in session ($c). It was left running and the upgrade was not started. See README-RMM.md: A running MailArchive that is busy is never cut off."
        }
        $code = 1618
        if ($cannot) { $code = $ExitCouldNotAsk }
        return @{ Code = $code; Lines = $lines }
    }
    foreach ($proc in $force) { Stop-Process -Id $proc.ProcessId -Force -ErrorAction SilentlyContinue }
    return @{ Code = 0; Lines = $lines }
}

# --- the upgrade ---

if (-not (Test-Path $MsiPath)) { throw "MSI not found: $MsiPath" }

Get-Process mailarchive-desktop -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$installDir = Get-InstallValue 'InstallDir'
$installedExe = ''
if ($installDir) { $installedExe = Join-Path $installDir 'mailarchivew.exe' }
$programFiles = $env:ProgramW6432
if (-not $programFiles) { $programFiles = $env:ProgramFiles }
$closing = Close-RunningMailArchive -ProcessName 'mailarchivew.exe' -Exe $installedExe -QuitVerb ((Get-InstallValue 'QuitVerb') -eq '0x1') -TrustedRoot $programFiles
foreach ($l in $closing.Lines) { Write-Output $l }
if ($closing.Code -ne 0) { exit $closing.Code }

$p = Start-Process msiexec.exe -ArgumentList @('/i', ('"{0}"' -f $MsiPath), '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
