@echo off
setlocal
cd /d "%~dp0"
rem Expects mailarchive-desktop-windows-msi-payload.zip (or the unzipped folder)
rem beside this script or under the repo's dist\. See README-RMM.md.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Build-MSI.ps1" -Version 1.2.0
if errorlevel 1 (
  echo.
  echo MSI build FAILED.
  pause
  exit /b 1
)
echo.
echo MSI build complete. See the dist folder.
pause
