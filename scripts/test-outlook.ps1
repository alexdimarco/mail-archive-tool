<#
.SYNOPSIS
  Lab validation for mailarchive's -outlook (Outlook COM -> PST) path — the
  MA-60 scenario in docs/scenario-catalog.md.

.DESCRIPTION
  Run on a Windows box with CLASSIC Outlook installed and a mail profile. Both
  checks are secret-free (no Exchange account required):

    1. COM plumbing probe — replicates the EXACT Object Model sequence
       mailarchive uses (AddStoreEx -> GetRootFolder -> Folders -> CopyTo ->
       RemoveStore) against a throwaway source PST, and verifies the copy. This
       proves the API sequence works on THIS Outlook build — the part of the Go
       code that has never executed.

    2. End-to-end — runs `mailarchive.exe -outlook` and captures its output, so
       you see how the real tool enumerates stores and behaves.

  IMPORTANT: mailarchive intentionally SKIPS accounts already stored as a .pst
  (archive those directly with `-input`), so a local-PST-only profile exercises
  enumeration + the skip path in step 2, while step 1 covers the AddStoreEx/
  CopyTo plumbing. A full end-to-end export needs a NON-PST (Exchange/IMAP)
  account in the profile.

.PARAMETER Exe
  Path to the mailarchive executable. Default: .\mailarchive.exe

.PARAMETER SyncWait
  Value for -outlook-sync-wait in step 2 (default 20s to keep the test quick;
  raise it for a real account).

.NOTES
  Prerequisites:
  - Classic Outlook installed (New Outlook does NOT expose the COM model).
  - An Outlook profile exists. A profile with only a local PST and no email
    account is enough for the probe: Control Panel -> Mail -> Show Profiles ->
    Add -> Manual setup -> Outlook Data File (.pst).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File test-outlook.ps1 -Exe .\mailarchive-windows-amd64.exe
#>
param(
  [string]$Exe = ".\mailarchive.exe",
  [string]$SyncWait = "20s"
)

$ErrorActionPreference = "Continue"
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$work  = Join-Path $env:TEMP ("mailarchive-outlook-test-" + $stamp)
New-Item -ItemType Directory -Force -Path $work | Out-Null
$log = Join-Path $work "test-outlook.log"

function Log($msg) {
  ("{0}  {1}" -f (Get-Date -Format "HH:mm:ss"), $msg) | Tee-Object -FilePath $log -Append
}

Log "=== mailarchive -outlook lab test ==="
Log "work dir: $work"
Log ""

# --- 0. Outlook present? --------------------------------------------------
try {
  $ol = New-Object -ComObject Outlook.Application
  $ns = $ol.GetNamespace("MAPI")
  Log ("Outlook detected: version {0}" -f $ol.Version)
} catch {
  Log "FAIL: could not start Outlook COM: $($_.Exception.Message)"
  Log "Classic Outlook must be installed with a configured profile (see -PREREQUISITES)."
  exit 1
}

# --- 1. COM plumbing probe: AddStoreEx / CopyTo / RemoveStore -------------
Log ""
Log "--- Probe 1: AddStoreEx + CopyTo + RemoveStore ---"
$olStoreUnicode = 3   # OlStoreType.olStoreUnicode
$olMailItem     = 0   # OlItemType.olMailItem
$srcPst  = Join-Path $work "probe-source.pst"
$destPst = Join-Path $work "probe-dest.pst"
$probePass = $false
try {
  # A source store with one folder holding one message.
  $ns.AddStoreEx($srcPst, $olStoreUnicode)
  $srcRoot = $ns.Folders.Item($ns.Folders.Count)   # the just-added store's root
  $srcFolder = $srcRoot.Folders.Add("ProbeFolder")
  $mail = $ol.CreateItem($olMailItem)
  $mail.Subject = "probe message"
  $mail.Body = "hello from the probe"
  $mail.Move($srcFolder) | Out-Null
  Log ("source PST ready: {0} folder(s), {1} item(s) in ProbeFolder" -f $srcRoot.Folders.Count, $srcFolder.Items.Count)

  # The destination store mailarchive would create, then copy every top folder in.
  $ns.AddStoreEx($destPst, $olStoreUnicode)
  $destRoot = $ns.Folders.Item($ns.Folders.Count)
  $copied = 0
  foreach ($f in $srcRoot.Folders) { [void]$f.CopyTo($destRoot); $copied++ }
  Log ("CopyTo: {0} folder(s) copied into the destination PST" -f $copied)

  # Verify the copy landed before detaching.
  $found = $null
  foreach ($f in $destRoot.Folders) { if ($f.Name -eq "ProbeFolder") { $found = $f } }
  if ($null -ne $found -and $found.Items.Count -ge 1) {
    Log ("PROBE PASS: destination PST has ProbeFolder with {0} item(s)" -f $found.Items.Count)
    $probePass = $true
  } else {
    Log "PROBE FAIL: destination PST is missing the copied folder/item"
  }

  # Detach both stores (leaves the .pst files on disk), as mailarchive does.
  $ns.RemoveStore($srcRoot)
  $ns.RemoveStore($destRoot)
  if (Test-Path $destPst) { Log ("destination PST size: {0} bytes" -f (Get-Item $destPst).Length) }
} catch {
  Log "PROBE ERROR: $($_.Exception.Message)"
}

# --- 2. End-to-end: mailarchive.exe -outlook ------------------------------
Log ""
Log "--- Probe 2: mailarchive.exe -outlook ---"
if (-not (Test-Path $Exe)) {
  Log "SKIP: executable not found at '$Exe' (pass -Exe <path>)"
} else {
  $exportDir = Join-Path $work "export"
  Log ("running: $Exe -outlook -out $exportDir -outlook-sync-wait $SyncWait")
  Log ""
  & $Exe -outlook -out $exportDir -outlook-sync-wait $SyncWait *>&1 | Tee-Object -FilePath $log -Append
  $code = $LASTEXITCODE
  Log ""
  Log ("mailarchive exit code: {0}" -f $code)
  $psts = @(Get-ChildItem -Path (Join-Path $exportDir "_outlook-pst") -Filter *.pst -ErrorAction SilentlyContinue)
  Log ("PST(s) created by mailarchive: {0}" -f $psts.Count)
  foreach ($p in $psts) { Log ("  {0}  ({1} bytes)" -f $p.Name, $p.Length) }
  $html = @(Get-ChildItem -Path $exportDir -Recurse -Filter *.html -ErrorAction SilentlyContinue | Where-Object { $_.Name -ne "index.html" })
  Log ("exported .html messages: {0}" -f $html.Count)
}

# --- Summary --------------------------------------------------------------
Log ""
Log "=== SUMMARY ==="
Log ("Probe 1 (COM plumbing): {0}" -f ($(if ($probePass) { "PASS" } else { "FAIL / see errors above" })))
Log "Probe 2 (mailarchive -outlook): see exit code + PST count above."
Log ""
Log "Interpreting results:"
Log " - Probe 1 PASS = AddStoreEx/CopyTo/RemoveStore work on this Outlook build."
Log " - Probe 2 with a NON-PST (Exchange/IMAP) account present = full MA-60."
Log " - Probe 2 on a local-PST-only profile will report accounts skipped and"
Log "   'no Outlook accounts could be exported' — that is expected."
Log ""
Log "Full log: $log"
Log "Send this log back to close out MA-60."
