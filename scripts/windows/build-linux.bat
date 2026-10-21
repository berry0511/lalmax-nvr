@echo off
setlocal
rem Cross-compile Linux ELF directly in PowerShell, never via Git Bash / .sh.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0nvr.ps1" build -BuildOS linux -BuildArch amd64 %*
exit /b %ERRORLEVEL%
