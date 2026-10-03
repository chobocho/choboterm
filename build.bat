@echo off
setlocal

rem ============================================================
rem  choboterm 빌드 스크립트
rem  wails build 로 실행 파일을 만들고 release 폴더에 복사합니다.
rem ============================================================

cd /d "%~dp0"

where wails >nul 2>nul
if errorlevel 1 (
    echo [오류] wails 명령을 찾을 수 없습니다.
    echo        go install github.com/wailsapp/wails/v2/cmd/wails@latest
    goto :fail
)

echo [1/3] 테스트 실행 중...
go test ./...
if errorlevel 1 (
    echo [오류] 테스트가 실패했습니다.
    goto :fail
)

echo [2/3] 빌드 중...
call wails build -clean
if errorlevel 1 (
    echo [오류] 빌드가 실패했습니다.
    goto :fail
)

echo [3/3] release 폴더에 복사 중...
if not exist release mkdir release
copy /y "build\bin\choboterm.exe" "release\choboterm.exe" >nul
if errorlevel 1 (
    echo [오류] 복사에 실패했습니다. choboterm.exe 가 실행 중이면 종료하고 다시 시도하세요.
    goto :fail
)

echo.
echo 완료: %~dp0release\choboterm.exe
endlocal
exit /b 0

:fail
echo.
echo 빌드를 중단했습니다.
endlocal
exit /b 1
