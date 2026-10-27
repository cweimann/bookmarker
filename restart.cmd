@echo off
REM Kill any running Bookmarker and start the current build.
REM Listen addresses, client allowlist, and data dir come from bookmarker.yaml.
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
if not exist bookmarker.yaml echo No bookmarker.yaml: using defaults (127.0.0.1:8787 only). See bookmarker.example.yaml.
start "bookmarker" /MIN bookmarker.exe
echo Started. The minimized "bookmarker" window logs listeners and the allowlist.
echo Hard-refresh the browser (Ctrl+F5) if the UI looks stale.
timeout /t 2 >nul
