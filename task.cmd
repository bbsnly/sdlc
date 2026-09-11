@echo off
rem Bootstrap wrapper for `task` on Windows. See ./task for the POSIX twin.
setlocal enabledelayedexpansion

set "HERE=%~dp0"
set "TASKFILE=%HERE%Taskfile.yml"

rem %~dp0 ends in a backslash, and a trailing backslash in front of a closing
rem quote escapes that quote: --dir "C:\repo\" reaches the program as
rem C:\repo" and takes the next argument with it. Strip it before anything is
rem passed on a command line. TASKFILE above is built by concatenation, not
rem passed as an argument, so it wants the separator and keeps it.
if "%HERE:~-1%"=="\" set "HERE=%HERE:~0,-1%"

if not exist "%TASKFILE%" (
  echo task: Taskfile.yml not found beside this script 1>&2
  exit /b 1
)

set "VERSION="
for /f "tokens=2 delims=: " %%v in ('findstr /r /c:"^ *TASK_VERSION:" "%TASKFILE%"') do (
  if not defined VERSION set "VERSION=%%v"
)
if not defined VERSION (
  echo task: could not read TASK_VERSION from Taskfile.yml 1>&2
  exit /b 1
)

where go >nul 2>&1
if errorlevel 1 (
  echo task: Go is not installed, and this wrapper runs task through it. 1>&2
  echo        Install Go: https://go.dev/dl/ 1>&2
  exit /b 1
)

go run "github.com/go-task/task/v3/cmd/task@v%VERSION%" --dir "%HERE%" %*
