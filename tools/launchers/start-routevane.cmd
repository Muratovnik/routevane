@echo off
rem Double-click this file to run Routevane. Nothing else is needed: the control
rem surface is inside the binary next to this file, and the catalog it reads is
rem the directory beside it.
rem
rem Pass a port as the first argument to use one other than 8765.
setlocal
rem The binary is named by an absolute path rather than found on the search
rem path: a machine hardened with NoDefaultCurrentDirectoryInExePath does not
rem look in the current directory, and this file must work there too.
cd /d "%~dp0"
title Routevane

set "ROUTEVANE_PORT=%~1"
if "%ROUTEVANE_PORT%"=="" set "ROUTEVANE_PORT=8765"

set "ROUTEVANE_BINARY=%~dp0routing-agent.exe"
if not exist "%ROUTEVANE_BINARY%" (
    echo Routevane: routing-agent.exe is not next to this file.
    echo Unpack the whole archive and run the copy inside it.
    goto :failed
)

rem The page is opened a moment later, in a separate window, so it is not
rem requested before the listener answers and so a browser failure can never
rem stop the service.
start "" /min cmd /c "timeout /t 2 /nobreak >nul & start "" http://127.0.0.1:%ROUTEVANE_PORT%"

echo Routevane: http://127.0.0.1:%ROUTEVANE_PORT%
echo Close this window or press Ctrl+C to stop.
echo.
"%ROUTEVANE_BINARY%" serve --port %ROUTEVANE_PORT%
if errorlevel 1 goto :failed
endlocal
exit /b 0

:failed
echo.
echo Routevane did not start.
pause
endlocal
exit /b 1
