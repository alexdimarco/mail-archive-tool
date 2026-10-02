[CmdletBinding()]
<#
    Build the self-contained MailArchive Desktop MSI from the release payload that
    packaging/build-release.sh produces (mailarchive-desktop-windows-msi-payload.zip).

    Go is NOT needed on the build PC — the three EXEs are already in the payload.
    WiX 3 is used if installed, else the portable WiX 3.14 tools are fetched once.

    Payload resolution order (first that exists wins), unless -PayloadDir is given:
      1. <script>\mailarchive-desktop-windows-msi-payload\            (unzipped beside script)
      2. <script>\mailarchive-desktop-windows-msi-payload.zip         (zip beside script)
      3. <repo>\dist\mailarchive-desktop-windows-msi-payload[.zip]    (a local build-release.sh run)
    A zip is extracted into <OutputDirectory>\payload.

    This script then STAGES these into the payload before building, then runs WiX:
      SOURCE-NOTICE.txt, DEPLOYMENT.txt, THIRD-PARTY-NOTICES.md  (checked in beside this script)
      managed-install.marker                                     (written here)
      PAYLOAD-SHA256.txt                                         (generated here)
#>
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '1.2.0',           # MUST stay > 1.1.0 (Steve's build) for the DC6 upgrade
    [string]$PayloadDir = '',
    [string]$OutputDirectory = "$PSScriptRoot\dist",
    [string]$SourceCommit = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Root       = $PSScriptRoot
$ProductWxs = Join-Path $Root 'Product.wxs'
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

# --- 1. Resolve (and if needed extract) the payload. -----------------------------
function Expand-PayloadZip([string]$zip) {
    $dest = Join-Path $OutputDirectory 'payload'
    if (Test-Path $dest) { Remove-Item $dest -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    Expand-Archive -Path $zip -DestinationPath $dest -Force
    # The zip contains a single top folder; descend into it.
    $inner = Join-Path $dest 'mailarchive-desktop-windows-msi-payload'
    if (Test-Path $inner) { return $inner }
    return $dest
}

if (-not $PayloadDir) {
    $repo = Resolve-Path (Join-Path $Root '..\..') -ErrorAction SilentlyContinue
    $candidates = @(
        (Join-Path $Root 'mailarchive-desktop-windows-msi-payload'),
        (Join-Path $Root 'mailarchive-desktop-windows-msi-payload.zip')
    )
    if ($repo) {
        $candidates += (Join-Path $repo 'dist\mailarchive-desktop-windows-msi-payload')
        $candidates += (Join-Path $repo 'dist\mailarchive-desktop-windows-msi-payload.zip')
    }
    foreach ($c in $candidates) {
        if (Test-Path $c) { $PayloadDir = $c; break }
    }
}
if (-not $PayloadDir) {
    throw "No payload found. Run packaging/build-release.sh to produce mailarchive-desktop-windows-msi-payload.zip, or pass -PayloadDir."
}
if ($PayloadDir -like '*.zip') { $PayloadDir = Expand-PayloadZip $PayloadDir }
$Payload = (Resolve-Path $PayloadDir).Path
Write-Host "Using payload: $Payload"

# --- 2. Verify the three EXEs are present and real PE files. ----------------------
$exes = @('mailarchive-desktop.exe','mailarchive.exe','mailarchive-gui.exe')
foreach ($exeName in $exes) {
    $exePath = Join-Path $Payload $exeName
    if (-not (Test-Path $exePath)) { throw "Payload is missing $exeName" }
    $stream = [System.IO.File]::OpenRead($exePath)
    try { $b0 = $stream.ReadByte(); $b1 = $stream.ReadByte() } finally { $stream.Dispose() }
    if ($b0 -ne 0x4D -or $b1 -ne 0x5A) { throw "$exePath is not a valid Windows PE executable (bad MZ header)." }
}
if (-not (Test-Path (Join-Path $Payload 'branding\mailarchive.ico'))) {
    throw "Payload is missing branding\mailarchive.ico (needed for the installer icon)."
}

# --- 3. Stage the extra files (notices, deployment metadata, marker). -------------
foreach ($f in @('SOURCE-NOTICE.txt','DEPLOYMENT.txt','THIRD-PARTY-NOTICES.md')) {
    $src = Join-Path $Root $f
    if (-not (Test-Path $src)) { throw "Missing checked-in file beside this script: $f" }
    Copy-Item $src (Join-Path $Payload $f) -Force
}
@'
MailArchive Desktop is installed and managed by Windows Installer (MSI).
Shortcut and startup integration are owned by the MSI package.
'@ | Set-Content (Join-Path $Payload 'managed-install.marker') -Encoding ASCII

# Stamp the build commit into the staged SOURCE-NOTICE (AGPL source pointer).
if (-not $SourceCommit) {
    try { $SourceCommit = (& git -C $Root rev-parse HEAD 2>$null).Trim() } catch { $SourceCommit = '' }
}
if ($SourceCommit) {
    Add-Content (Join-Path $Payload 'SOURCE-NOTICE.txt') "`r`nBuilt from commit: $SourceCommit"
}

# --- 4. Regenerate the payload hash manifest. -------------------------------------
$hashTargets = @(
    'mailarchive-desktop.exe','mailarchive.exe','mailarchive-gui.exe',
    'branding\mailarchive.ico','READ ME.txt',
    'docs\LICENSE.txt','docs\release-notes.md','docs\goback.md','docs\graph-app-setup.md',
    'DEPLOYMENT.txt','SOURCE-NOTICE.txt','THIRD-PARTY-NOTICES.md','managed-install.marker'
)
$hashLines = foreach ($name in $hashTargets) {
    $p = Join-Path $Payload $name
    if (Test-Path $p) { $h = Get-FileHash $p -Algorithm SHA256; "$($h.Hash)  $name" }
}
$hashLines | Set-Content (Join-Path $Payload 'PAYLOAD-SHA256.txt') -Encoding ASCII

# --- 5. Locate WiX 3 (installed, else fetch the portable tools privately). --------
$candle = (Get-Command candle.exe -ErrorAction SilentlyContinue).Source
$light  = (Get-Command light.exe  -ErrorAction SilentlyContinue).Source
if (-not $candle -or -not $light) {
    $toolDir = Join-Path $Root '.tools\wix314'
    $candle = Join-Path $toolDir 'candle.exe'
    $light  = Join-Path $toolDir 'light.exe'
    if (-not (Test-Path $candle) -or -not (Test-Path $light)) {
        New-Item -ItemType Directory -Force -Path $toolDir | Out-Null
        $zipPath = Join-Path $Root '.tools\wix314-binaries.zip'
        $url = 'https://github.com/wixtoolset/wix3/releases/download/wix314rtm/wix314-binaries.zip'
        Write-Host 'Downloading portable WiX 3.14 build tools...'
        # Force TLS 1.2 so the GitHub download works on older/hardened Windows images
        # (Server 2016/2012R2 without SchUseStrongCrypto default to SSL3/TLS1.0, which
        # GitHub rejects with "Could not create SSL/TLS secure channel").
        try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}
        Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
        Expand-Archive -Path $zipPath -DestinationPath $toolDir -Force
    }
}
if (-not (Test-Path $candle) -or -not (Test-Path $light)) { throw 'WiX candle.exe/light.exe could not be located.' }

# --- 6. Build the MSI. ------------------------------------------------------------
$obj          = Join-Path $OutputDirectory 'Product.wixobj'
$msiVersioned = Join-Path $OutputDirectory "MailArchiveDesktop-$Version-x64.msi"
$msiStable    = Join-Path $OutputDirectory 'MailArchiveDesktop.msi'

Write-Host "Building self-contained MailArchive Desktop MSI v$Version..."
& $candle -nologo -arch x64 -dProductVersion=$Version "-dSourceDir=$Payload" -out $obj $ProductWxs
if ($LASTEXITCODE -ne 0) { throw "candle.exe failed with exit code $LASTEXITCODE" }

& $light -nologo -sval -out $msiVersioned $obj
if ($LASTEXITCODE -ne 0) { throw "light.exe failed with exit code $LASTEXITCODE" }

Copy-Item $msiVersioned $msiStable -Force
Remove-Item $obj -ErrorAction SilentlyContinue

$hash = Get-FileHash $msiVersioned -Algorithm SHA256
Write-Host ''
Write-Host 'Build complete. THIS MSI CONTAINS THE EXECUTABLES; deploy only the MSI:'
Write-Host "  $msiVersioned"
Write-Host "  $msiStable"
Write-Host "  SHA256: $($hash.Hash)"
Write-Host ''
Write-Host 'RMM silent install:'
Write-Host "  msiexec.exe /i `"$msiStable`" /qn /norestart"
Write-Host 'RMM uninstall: use Uninstall-RMM.ps1 or Apps & Features.'
