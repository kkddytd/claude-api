@echo off
REM Claude Server 启动脚本 (Windows)
setlocal enabledelayedexpansion

cd /d "%~dp0"

REM 自动检测可执行文件（支持 claude-server.exe 或 claude-server-*.exe 格式）
set "EXE_FILE="

REM 优先检测 claude-server.exe
if exist "claude-server.exe" (
    set "EXE_FILE=claude-server.exe"
) else (
    REM 检测 claude-server-*.exe
    for %%f in (claude-server-*.exe) do (
        set "EXE_FILE=%%f"
    )
)

if "%EXE_FILE%"=="" (
    echo 错误：未找到 claude-server*.exe 可执行文件
    echo 请确保可执行文件与此脚本在同一目录下
    echo.
    pause
    exit /b 1
)

echo 检测到可执行文件: %EXE_FILE%

REM 检查是否已在运行
tasklist /FI "IMAGENAME eq %EXE_FILE%" 2>NUL | find /I "%EXE_FILE%" >NUL
if %ERRORLEVEL% == 0 (
    echo Claude Server 已在运行
    echo.
    pause
    exit /b 1
)

echo 正在启动 Claude Server...
start /B "" "%~dp0%EXE_FILE%" > claude-server.log 2>&1

timeout /t 2 /nobreak > NUL

tasklist /FI "IMAGENAME eq %EXE_FILE%" 2>NUL | find /I "%EXE_FILE%" >NUL
if %ERRORLEVEL% == 0 (
    echo.
    echo ========================================
    echo   Claude Server 启动成功！
    echo ========================================
    echo.
    echo   日志文件: %~dp0claude-server.log
    echo   访问地址: http://localhost:62311
    echo.
    echo   提示：关闭此窗口不会停止服务
    echo   如需停止服务，请运行 stop.bat
    echo.
) else (
    echo.
    echo ========================================
    echo   Claude Server 启动失败！
    echo ========================================
    echo.
    echo   请查看日志: %~dp0claude-server.log
    echo.
    pause
    exit /b 1
)

echo 按任意键关闭此窗口...
pause > NUL
endlocal
