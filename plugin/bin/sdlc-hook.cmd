@echo off
rem Hand a hook event to the sdlc binary, wherever it happens to live.
rem Mirrors plugin/bin/sdlc-hook; see that file for why this never blocks.
setlocal

rem A trailing backslash exists only for a directory, which is not a program.
if defined SDLC_BIN if exist "%SDLC_BIN%" if not exist "%SDLC_BIN%\" (
  "%SDLC_BIN%" hook %1
  exit /b 0
)
if defined CLAUDE_PLUGIN_ROOT if exist "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" (
  "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" hook %1
  exit /b 0
)
rem PATH only: cmd.exe, and where with it, look in the current directory
rem first, and a hook runs in the project. A bare `sdlc` ran an sdlc.cmd
rem sitting at its root on every tool call, in place of the installed binary.
rem Every installer installs sdlc.exe itself, not a script beside it, so only
rem sdlc.exe is looked up.
for /f "delims=" %%B in ('where $PATH:sdlc.exe 2^>nul') do (
  "%%B" hook %1
  exit /b 0
)
rem Where the installers put it, install.ps1 and "npx @bbsnly/sdlc install"
rem alike, found without PATH. The npm installer never adds it to PATH, and a
rem desktop app such as Claude Desktop keeps the PATH it started with, so a binary
rem every new terminal found was not found here. PATH comes first.
if defined LOCALAPPDATA if exist "%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe" if not exist "%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe\" (
  "%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe" hook %1
  exit /b 0
)

rem Only a session whose project directory or working directory is in a project
rem that uses sdlc hears that the binary is missing. plugin/bin/sdlc-hook says
rem where that is narrower than the binary's own rule. The directory goes in
rem through a variable: an argument to call is expanded a second time, which
rem doubles a caret and drops a percent sign.
set "dir=%CLAUDE_PROJECT_DIR%"
call :takes_part
if not errorlevel 1 goto missing
set "dir=%CD%"
call :takes_part
if not errorlevel 1 goto missing
exit /b 0

:missing
rem systemMessage is what reaches the session: stderr from a hook that exits 0
rem goes to the debug log only. See plugin/bin/sdlc-hook.
echo {"continue":true,"systemMessage":"sdlc: the sdlc binary was not found, so nothing is being enforced. why: it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH this app started with, or where the installers put it. fix: install it with npx @bbsnly/sdlc install. A desktop app such as Claude Desktop keeps the PATH it started with, so quit it fully and reopen it after installing or changing PATH, or set SDLC_BIN to the full path of the binary. Then run sdlc doctor in your terminal."}
echo sdlc: the sdlc binary was not found, so nothing is being enforced.>&2
echo.>&2
echo   why  it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH>&2
echo        this app started with, or where the installers put it>&2
echo   fix  install it with "npx @bbsnly/sdlc install". A desktop app such as>&2
echo        Claude Desktop keeps the PATH it started with, so quit it fully and>&2
echo        reopen it after installing or changing PATH, or set SDLC_BIN to the>&2
echo        full path of the binary. Then run "sdlc doctor" in your terminal.>&2
exit /b 0

rem takes_part succeeds when %dir%, or a directory above it in the same
rem repository, has .sdlc\config.json. It stops at a repository boundary and
rem at the root of the drive.
:takes_part
if not defined dir exit /b 1
:walk
if exist "%dir%\.sdlc\config.json" exit /b 0
if exist "%dir%\.git" exit /b 1
for %%P in ("%dir%\..") do set "parent=%%~fP"
if /i "%parent%"=="%dir%" exit /b 1
set "dir=%parent%"
goto walk
