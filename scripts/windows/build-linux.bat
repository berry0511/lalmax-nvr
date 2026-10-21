@echo off
setlocal
rem Complete Linux/amd64 cross build from a Windows workstation.
set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0nvr.ps1" build %*
exit /b %ERRORLEVEL%
