@echo off
rem Developer shortcut: Nuxt HMR and automatic Go rebuilds from this checkout.
rem See CONTRIBUTING.md. Downloaded releases have their own start-routevane.cmd.
setlocal
cd /d "%~dp0"
title Routevane development
set "ROUTEVANE_PORT=%~1"
if "%ROUTEVANE_PORT%"=="" set "ROUTEVANE_PORT=8765"
where pwsh.exe >nul 2>&1
if errorlevel 1 (
    echo Routevane development requires PowerShell 7.4 or newer.
    echo See CONTRIBUTING.md, or use a downloaded release instead.
    goto :failed
)
pwsh -NoLogo -NoProfile -File "tools\dev.ps1" dev -Port "%ROUTEVANE_PORT%"
if errorlevel 1 goto :failed
exit /b 0
:failed
echo Development startup failed. Read the error above and CONTRIBUTING.md.
pause
exit /b 1
