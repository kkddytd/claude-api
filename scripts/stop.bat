@echo off
REM Claude Server 停止脚本 (Windows)
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

tasklist /FI "IMAGENAME eq %EXE_FILE%" 2>NUL | find /I "%EXE_FILE%" >NUL
if %ERRORLEVEL% == 1 (
    echo.
    echo Claude Server 未在运行
    echo.
    pause
    exit /b 0
)

echo 正在停止 Claude Server...
taskkill /IM %EXE_FILE% /F >NUL 2>&1

timeout /t 1 /nobreak > NUL

tasklist /FI "IMAGENAME eq %EXE_FILE%" 2>NUL | find /I "%EXE_FILE%" >NUL
if %ERRORLEVEL% == 1 (
    echo.
    echo ========================================
    echo   Claude Server 已停止
    echo ========================================
    echo.
) else (
    echo.
    echo ========================================
    echo   停止 Claude Server 失败
    echo ========================================
    echo   请尝试手动在任务管理器中结束进程
    echo.
    pause
    exit /b 1
)

echo 按任意键关闭此窗口...
pause > NUL
endlocal
