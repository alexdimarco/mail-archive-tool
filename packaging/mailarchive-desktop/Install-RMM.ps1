[CmdletBinding()]
param([string]$MsiPath = "$PSScriptRoot\MailArchiveDesktop.msi")
$ErrorActionPreference = 'Stop'
if (-not (Test-Path $MsiPath)) { throw "MSI not found: $MsiPath" }
$p = Start-Process msiexec.exe -ArgumentList @('/i', ('"{0}"' -f $MsiPath), '/qn', '/norestart') -Wait -PassThru
exit $p.ExitCode
