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
# mailarchivew.exe is a dashboard when it has no argument after the program path, a
# first argument `desktop` without `-capture`, or a first argument `start` (with or
# without -background: the running MailArchive the shortcuts start since
# design-menubar slice S1, MB-7). A run-job, a -job, a -capture, a `desktop -capture`
# (the weekly backup) or a `wizard` is NEVER stopped — a mid-run scheduled backup or
# an open wizard is left to finish (MA-614, SC34). No mailarchive-gui is stopped.
#
# Since slice S1 a dashboard also hosts the wizard and Run now, so it is first asked to
# close the graceful way (the S1 fix round, A2). Its window is in its user's sign-in
# session, which a window message from this script's session cannot reach, and Restart
# Manager cannot ask it (its only window is hidden: "unknown app"); so a one-time task
# of that user, in that session, posts the close request to the copy's own window. An
# idle copy stops and waits for itself; one that runs work or an open wizard refuses
# and keeps running. A refusal (or a task that could not run) is never forced: the
# script then does NOT start the upgrade, says which process is busy and exits 1618
# (another installation is in progress), so the RMM tool tries again later. Only a
# dashboard with no such window (one from before slice S1) is ended by force, as before.
$closeHelper = @'
$ErrorActionPreference = 'SilentlyContinue'
Add-Type @"
using System; using System.Runtime.InteropServices; using System.Text;
public static class MailArchiveClose {
    delegate bool EnumProc(IntPtr h, IntPtr l);
    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc f, IntPtr l);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
    [DllImport("user32.dll")] static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
    // Ask posts WM_CLOSE to the copy's own instance window; false when it has none.
    public static bool Ask(uint target) {
        IntPtr found = IntPtr.Zero;
        EnumWindows((h, l) => { uint p; GetWindowThreadProcessId(h, out p);
            var c = new StringBuilder(64); GetClassName(h, c, 64);
            if (p == target && c.ToString() == "MailArchiveInstanceWindow") { found = h; return false; }
            return true; }, IntPtr.Zero);
        return found != IntPtr.Zero && PostMessage(found, 0x0010, IntPtr.Zero, IntPtr.Zero);
    }
}
"@
if (-not [MailArchiveClose]::Ask(__PID__)) { exit 2 }
for ($i = 0; $i -lt 30; $i++) {
    if (-not (Get-Process -Id __PID__ -ErrorAction SilentlyContinue)) { exit 0 }
    Start-Sleep -Seconds 1
}
exit 1
'@

# Close-InItsSession runs the helper as the copy's own user, in its sign-in session, and
# returns 0 (it ended), 1 (it refused: busy), 2 (no instance window: a dashboard from
# before slice S1) or 3 (the task could not run).
function Close-InItsSession($proc) {
    $owner = Invoke-CimMethod -InputObject $proc -MethodName GetOwner
    if ($owner.ReturnValue -ne 0) { return 3 }
    $script = $closeHelper.Replace('__PID__', [string][int]$proc.ProcessId)
    $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($script))
    $name = "MailArchive upgrade close $($proc.ProcessId)"
    try {
        $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand $encoded"
        $principal = New-ScheduledTaskPrincipal -UserId ("{0}\{1}" -f $owner.Domain, $owner.User) -LogonType Interactive
        Register-ScheduledTask -TaskName $name -Action $action -Principal $principal -Force | Out-Null
        Start-ScheduledTask -TaskName $name
        # The bound: the copy's drain (15 s), its servers' shutdown, and room.
        $code = 3
        for ($i = 0; $i -lt 60; $i++) {
            Start-Sleep -Seconds 1
            $ran = (Get-ScheduledTaskInfo -TaskName $name).LastTaskResult
            # 267011 (0x41303): the task has not run yet; 267009 (0x41301): it is running.
            if ((Get-ScheduledTask -TaskName $name).State -ne 'Running' -and $ran -ne 267011 -and $ran -ne 267009) { $code = $ran; break }
        }
    } catch { $code = 3 } finally {
        Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
    }
    if ($code -in 0, 1, 2) { return [int]$code }
    return 3
}

Get-Process mailarchive-desktop -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$busy = @()
foreach ($proc in @(Get-CimInstance Win32_Process -Filter "Name = 'mailarchivew.exe'" -ErrorAction SilentlyContinue)) {
    $cmd = $proc.CommandLine
    if (-not $cmd) { continue }
    # Strip the program path (quoted or bare) to leave just the arguments.
    $cmdArgs = ($cmd -replace '^\s*("[^"]*"|\S+)\s*', '')
    $isDashboard = ($cmdArgs.Trim() -eq '') -or
                   ($cmdArgs -match '^desktop(\s|$)' -and $cmdArgs -notmatch '(^|\s)-capture(\s|$)') -or
                   ($cmdArgs -match '^start(\s|$)')
    if (-not $isDashboard) { continue }
    $closed = Close-InItsSession $proc
    if ($closed -eq 0) { Write-Output "Closed MailArchive (process $($proc.ProcessId)) after it finished its work." }
    elseif ($closed -eq 2) { Stop-Process -Id $proc.ProcessId -Force -ErrorAction SilentlyContinue }
    else { $busy += $proc.ProcessId }
}
if ($busy.Count -gt 0) {
    Write-Output ("MailArchive is busy in a signed-in session (process {0}): it is running a backup or another task, or its wizard is open. Nothing was stopped and the upgrade was not started. Run this script again later." -f ($busy -join ', '))
    exit 1618
}

$p = Start-Process msiexec.exe -ArgumentList @('/i', ('"{0}"' -f $MsiPath), '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
