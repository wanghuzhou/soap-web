@echo off
setlocal
set "SCRIPT_DIR=%~dp0"
set "EXE=%SCRIPT_DIR%bin\soapweb.exe"
set "ADDR=127.0.0.1:8420"

if not exist "%EXE%" (
  echo [ERROR] binary not found: %EXE%
  echo Please build first: go build -o bin\soapweb.exe .\cmd\soapweb
  pause
  exit /b 1
)

echo Starting soap-web at http://%ADDR% ...
echo Browser will open automatically. Close this window to stop.
echo.
"%EXE%" -addr "%ADDR%"
echo.
echo soap-web stopped.
pause
endlocal