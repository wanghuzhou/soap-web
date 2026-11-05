@echo off
REM ============================================================
REM  soap-web 启动脚本 (Windows)
REM  双击运行：启动服务并自动打开浏览器
REM ============================================================
setlocal
chcp 65001 >nul

set "SCRIPT_DIR=%~dp0"
set "EXE=%SCRIPT_DIR%bin\soapweb.exe"
set "ADDR=127.0.0.1:8420"

if not exist "%EXE%" (
  echo [错误] 未找到可执行文件: %EXE%
  echo 请先在项目根目录执行: go build -o bin\soapweb.exe .\cmd\soapweb
  pause
  exit /b 1
)

echo ============================================================
echo   soap-web  SOAP / WSDL 接口调试工具
echo   地址: http://%ADDR%
echo   浏览器将自动打开；关闭本窗口即停止服务。
echo ============================================================
echo.

REM 运行（-open 默认 true，会自动打开浏览器）
"%EXE%" -addr "%ADDR%"

REM 服务退出后暂停，便于查看错误信息
echo.
echo 服务已停止。
pause
endlocal
