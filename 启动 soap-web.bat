@echo off
setlocal
cd /d "%~dp0"
set "SCRIPT_DIR=%~dp0"
set "EXE=%SCRIPT_DIR%bin\soapweb.exe"
set "ADDR=127.0.0.1:8420"

if not exist "%EXE%" goto :try_go

:run_exe
echo Starting soap-web at http://%ADDR% ...
echo Browser will open automatically. Close this window to stop.
echo.
"%EXE%" -addr "%ADDR%" -data "%SCRIPT_DIR%bin\data" %*
if errorlevel 1 goto :try_go
goto :end

:try_go
echo.
echo [WARN] The prebuilt binary could not start.
echo [WARN] Windows SmartScreen may have blocked it as an unsigned app.
echo [WARN] Trying to run from source with Go...
echo.
where go >nul 2>nul
if errorlevel 1 goto :no_go
go run "%SCRIPT_DIR%cmd\soapweb" -addr "%ADDR%" -data "%SCRIPT_DIR%bin\data" %*
goto :end

:no_go
echo [ERROR] The prebuilt binary is missing or blocked, and Go is not installed.
echo.
echo To run the prebuilt binary:
echo   1. Right-click bin\soapweb.exe, choose Properties.
echo   2. On the General tab, check "Unblock" if available.
echo   3. When SmartScreen appears, choose "More info" then "Run anyway".
echo.
pause
exit /b 1

:end
echo.
echo soap-web stopped.
pause
endlocal
