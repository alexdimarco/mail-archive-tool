[CmdletBinding()]
<#
    Fetch the portable WiX 3.14 build tools (candle.exe, light.exe) into -Destination,
    and use them only if the download is the file we pinned.

    The one place the WiX download happens: Build-MSI.ps1 (when WiX is not installed),
    .github/workflows/release.yml and .github/workflows/windows-msi-lab.yml all call it.

    Order, each step refusing before the next:
      1. read the pinned SHA-256 from wix314-binaries.sha256 beside this script (the
         first line that is not a comment); refuse, before downloading anything, when
         it is not 64 hexadecimal digits (the file ships with a placeholder until a
         person fills it in, so an unfilled pin fails closed);
      2. download wix314-binaries.zip to a temporary file (or, with -ZipPath, use a
         copy of it already on this computer; nothing is downloaded then);
      3. refuse it unless its SHA-256 equals the pinned value;
      4. only then expand it into -Destination, strip the Mark-of-the-Web, and check
         that candle.exe and light.exe are there.

    -ZipPath is held to the same pin as a download. A WiX already installed (on PATH),
    or an earlier download already in Build-MSI.ps1's .tools\wix314, is NOT checked by
    this script: Build-MSI.ps1 uses those as they are (README-RMM.md says so).
#>
param(
    [Parameter(Mandatory = $true)]
    [string]$Destination,
    # A wix314-binaries.zip already on this computer, checked against the pin like a
    # download. The Windows test of this script (MA-1101) uses it, so it needs no network.
    [string]$ZipPath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Url     = 'https://github.com/wixtoolset/wix3/releases/download/wix314rtm/wix314-binaries.zip'
$PinFile = Join-Path $PSScriptRoot 'wix314-binaries.sha256'

# --- 1. The pinned value, checked before any download. ---------------------------
if (-not (Test-Path $PinFile)) {
    throw "Missing $PinFile (the SHA-256 the WiX download must match). Refusing to download WiX unchecked."
}
$pinLine = @(Get-Content $PinFile | Where-Object { $_.Trim() -ne '' -and -not $_.TrimStart().StartsWith('#') }) | Select-Object -First 1
$pinned = ([string]$pinLine).Trim().ToUpperInvariant()
if ($pinned -notmatch '^[0-9A-F]{64}$') {
    throw "wix314-binaries.sha256 does not hold a SHA-256 (64 hexadecimal digits) yet; it holds '$pinned'. Refusing to download and use WiX unchecked. Fill in the checked SHA-256 of wix314-binaries.zip."
}

# --- 2. Download to a temporary file (or use -ZipPath). ---------------------------
$downloaded = -not $ZipPath
if ($downloaded) {
    $zip = Join-Path ([System.IO.Path]::GetTempPath()) ("wix314-binaries-" + [guid]::NewGuid().ToString('N') + '.zip')
} else {
    if (-not (Test-Path -LiteralPath $ZipPath -PathType Leaf)) { throw "-ZipPath $ZipPath is not a file." }
    $zip = (Resolve-Path -LiteralPath $ZipPath).Path
}
try {
    if ($downloaded) {
        # TLS 1.2 so the GitHub download works on older or hardened Windows images.
        try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}
        Write-Host "Downloading $Url"
        Invoke-WebRequest -Uri $Url -OutFile $zip -UseBasicParsing
    } else {
        Write-Host "Using $zip (not downloaded)"
    }

    # --- 3. The download must be the pinned file. ---------------------------------
    $actual = (Get-FileHash -Path $zip -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($actual -ne $pinned) {
        throw "The downloaded wix314-binaries.zip has SHA-256 $actual, but wix314-binaries.sha256 pins $pinned. Refusing to use it."
    }
    Write-Host "wix314-binaries.zip matches the pinned SHA-256 $pinned"

    # --- 4. Only now expand and use it. -------------------------------------------
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Expand-Archive -LiteralPath $zip -DestinationPath $Destination -Force
} finally {
    # Only our own temporary download is deleted, never a -ZipPath the caller gave.
    if ($downloaded) { Remove-Item $zip -Force -ErrorAction SilentlyContinue }
}

# Strip the Mark-of-the-Web: without this, .NET can refuse to load WixUtilExtension.dll
# from an Internet-zone file (HRESULT 0x80131515), failing candle/light.
Get-ChildItem -Path $Destination -Recurse -File | Unblock-File
foreach ($tool in @('candle.exe', 'light.exe')) {
    if (-not (Test-Path (Join-Path $Destination $tool))) {
        throw "The WiX 3.14 download did not contain $tool under $Destination"
    }
}
Write-Host "WiX 3.14 staged at $Destination"
