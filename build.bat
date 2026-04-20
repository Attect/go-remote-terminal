@echo off
REM 跨平台交叉编译脚本 (Windows)
REM 用法: build.bat

setlocal

set APP_NAME=go-remote-terminal
set VERSION=1.0.0
set BUILD_DIR=build
set LDFLAGS=-s -w

echo === Building %APP_NAME% v%VERSION% ===
echo.

REM 创建构建目录
if not exist %BUILD_DIR% mkdir %BUILD_DIR%

REM 清理旧构建
del /Q %BUILD_DIR%\%APP_NAME%-* 2>nul

REM 构建函数
echo Building for windows/amd64...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\%APP_NAME%-windows-amd64.exe .
if %ERRORLEVEL% neq 0 (
    echo   X %APP_NAME%-windows-amd64.exe FAILED
    goto :error
)
echo   OK %APP_NAME%-windows-amd64.exe

echo Building for linux/amd64...
set GOOS=linux
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\%APP_NAME%-linux-amd64 .
if %ERRORLEVEL% neq 0 (
    echo   X %APP_NAME%-linux-amd64 FAILED
    goto :error
)
echo   OK %APP_NAME%-linux-amd64

echo Building for darwin/amd64...
set GOOS=darwin
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\%APP_NAME%-darwin-amd64 .
if %ERRORLEVEL% neq 0 (
    echo   X %APP_NAME%-darwin-amd64 FAILED
    goto :error
)
echo   OK %APP_NAME%-darwin-amd64

echo Building for darwin/arm64...
set GOOS=darwin
set GOARCH=arm64
go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\%APP_NAME%-darwin-arm64 .
if %ERRORLEVEL% neq 0 (
    echo   X %APP_NAME%-darwin-arm64 FAILED
    goto :error
)
echo   OK %APP_NAME%-darwin-arm64

echo.
echo === Build Complete ===
dir /B %BUILD_DIR%\%APP_NAME%-*
goto :end

:error
echo.
echo === Build FAILED ===
exit /b 1

:end
endlocal
