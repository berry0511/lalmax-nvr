@echo off
setlocal
rem Windows build runs in PowerShell; nvr.ps1 sets temporary GOOS/GOARCH.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0nvr.ps1" build -BuildOS windows -BuildArch amd64 %*
exit /b %ERRORLEVEL%
