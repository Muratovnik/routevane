@echo off
rem Double-click this file to run Routevane from a checkout of this repository.
rem It is the clickable equivalent of "tools/dev.ps1 setup" followed by
rem "tools/dev.ps1 up": dependencies are installed only when they are missing,
rem the control surface is built into the binary, and the browser page opens.
rem
rem A downloaded release archive carries its own launcher and needs none of the
rem toolchains checked below.
setlocal
cd /d "%~dp0"
title Routevane

set "ROUTEVANE_PORT=%~1"
if "%ROUTEVANE_PORT%"=="" set "ROUTEVANE_PORT=8765"

rem PowerShell 7 is what the gates use. Windows PowerShell 5.1 is accepted as a
rem fallback so a first run does not fail on a machine that has only that.
set "ROUTEVANE_SHELL="
where pwsh.exe >nul 2>&1 && set "ROUTEVANE_SHELL=pwsh.exe"
if not defined ROUTEVANE_SHELL (
    where powershell.exe >nul 2>&1 && set "ROUTEVANE_SHELL=powershell.exe"
)
if not defined ROUTEVANE_SHELL (
    echo Routevane: PowerShell was not found.
    echo Install PowerShell 7 from https://aka.ms/powershell and run this file again.
    goto :failed
)

where go.exe >nul 2>&1
if errorlevel 1 (
    echo Routevane: the Go toolchain was not found.
    echo Building from this repository needs Go 1.27. A release archive does not.
    goto :failed
)

rem Reject an unsupported toolchain or missing prerequisite before npm downloads
rem or any other checkout-local setup side effect.
"%ROUTEVANE_SHELL%" -NoLogo -NoProfile -ExecutionPolicy Bypass -File "tools\dev.ps1" doctor
if errorlevel 1 (
    echo Routevane: the source-build prerequisites are not supported.
    goto :failed
)

rem The generated control surface is what makes the browser page exist, so the
rem web dependencies are required even for a run that only serves.
if not exist "web\node_modules" (
    echo Routevane: installing dependencies. This happens once and takes a few minutes.
    "%ROUTEVANE_SHELL%" -NoLogo -NoProfile -ExecutionPolicy Bypass -File "tools\dev.ps1" setup
    if errorlevel 1 (
        echo Routevane: the one-time setup failed. The output above says why.
        goto :failed
    )
)

echo Routevane: starting on http://127.0.0.1:%ROUTEVANE_PORT%
echo Close this window or press Ctrl+C to stop.
"%ROUTEVANE_SHELL%" -NoLogo -NoProfile -ExecutionPolicy Bypass -File "tools\dev.ps1" up -Port %ROUTEVANE_PORT%
if errorlevel 1 goto :failed
endlocal
exit /b 0

:failed
echo.
echo Routevane did not start.
pause
endlocal
exit /b 1
