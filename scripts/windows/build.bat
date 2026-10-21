@echo off
setlocal
rem Always build Windows/amd64 even if GOOS=linux was previously exported.
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0nvr.ps1" build %*
exit /b %ERRORLEVEL%
