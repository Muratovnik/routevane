@echo off
rem Start the ready-to-run application. See README.txt beside this file.
setlocal
cd /d "%~dp0"
title Routevane
set "ROUTEVANE_PORT=%~1"
if "%ROUTEVANE_PORT%"=="" set "ROUTEVANE_PORT=8765"
set "ROUTEVANE_OPEN_BROWSER=true"
if "%ROUTEVANE_NO_BROWSER%"=="1" set "ROUTEVANE_OPEN_BROWSER=false"
if not exist "%~dp0routing-agent.exe" (
    echo Missing routing-agent.exe. Extract the whole archive.
    goto :failed
)
echo Routevane: http://127.0.0.1:%ROUTEVANE_PORT%
echo Press Ctrl+C to stop. Your data is in the data folder beside this launcher.
"%~dp0routing-agent.exe" serve --port "%ROUTEVANE_PORT%" --open-browser=%ROUTEVANE_OPEN_BROWSER%
if errorlevel 1 goto :failed
exit /b 0
:failed
echo Routevane did not start. Read README.txt and the error above.
if not "%ROUTEVANE_NO_BROWSER%"=="1" pause
exit /b 1
