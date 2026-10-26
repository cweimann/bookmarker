@echo off
REM Kill any running Bookmarker and start the current build on :8787.
cd /d "%~dp0"
for /f "tokens=5" %%p in ('netstat -ano ^| findstr /R /C:":8787 .*LISTENING"') do (
  echo Killing PID %%p on :8787
  taskkill /F /PID %%p >nul 2>&1
)
taskkill /F /IM bookmarker.exe >nul 2>&1
set "PATH=C:\Program Files\Go\bin;%PATH%"
go build -o bookmarker.exe .
if errorlevel 1 (
  echo Build failed.
  pause
  exit /b 1
)
start "bookmarker" /MIN bookmarker.exe -addr 127.0.0.1:8787 -data data
echo Started http://127.0.0.1:8787/ — hard-refresh the browser (Ctrl+F5).
timeout /t 2 >nul
