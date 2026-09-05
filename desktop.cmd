@echo off
rem Run the desktop application from a source checkout. See CONTRIBUTING.md.
setlocal
cd /d "%~dp0"
pwsh -NoLogo -NoProfile -File "tools\dev.ps1" desktop
exit /b %errorlevel%
