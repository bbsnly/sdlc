@echo off
rem Hand a hook event to the sdlc binary, wherever it happens to live.
rem Mirrors plugin/bin/sdlc-hook; see that file for why this never blocks, and
rem why it says nothing outside the session working a story.
setlocal DisableDelayedExpansion

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

rem Only the Claude Code session `sdlc start` recorded, in the project its
rem project directory or working directory is in, hears that the binary is
rem missing. plugin/bin/sdlc-hook says where finding the project is narrower
rem than the binary's own rule. The directory goes in through a variable: an
rem argument to call is expanded a second time, which doubles a caret and drops
rem a percent sign.
set "from_project="
set "from_cwd="
set "dir=%CLAUDE_PROJECT_DIR%"
call :working_session from_project
set "dir=%CD%"
call :working_session from_cwd
if not defined from_project if not defined from_cwd exit /b 0
if not defined from_project set "from_project=%from_cwd%"
if not defined from_cwd set "from_cwd=%from_project%"

rem That session's id is in the payload, which is read only here: in a project
rem with a story under way and a session recorded as working it. Both ids hold
rem nothing but letters, digits, dots, underscores and hyphens, so they are safe
rem on these lines. Claude Code writes the payload on one line with session_id
rem first, and a large edit makes that line long. set /p reads its first 1023
rem bytes, which is where the id is looked for first; findstr, which ships with
rem every Windows, looks through what is left, and never matches a line of 8191
rem bytes or more when it reads a pipe. An id written after both is not found,
rem and the session working the story misses the warning on that call. Every
rem other session hears nothing either way.
set "heard="
set "first="
set /p first=
if defined first call :names_session
if not defined heard (
  "%SystemRoot%\System32\findstr.exe" /l /i /c:"\"session_id\":\"%from_project%\"" /c:"\"session_id\": \"%from_project%\"" /c:"\"session_id\":\"%from_cwd%\"" /c:"\"session_id\": \"%from_cwd%\"" >nul 2>nul
  if not errorlevel 1 set "heard=1"
)
if not defined heard exit /b 0

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

rem names_session sets heard when the payload's first line, in %first%, is from
rem the session either id names. The line is only ever expanded late, which
rem never runs what it expands, and comparing it with itself less the id tells
rem whether the id is in it.
:names_session
setlocal EnableDelayedExpansion
set "found="
for %%I in ("%from_project%" "%from_cwd%") do (
  set "probe=!first:"session_id":"%%~I"=!"
  if not "!probe!"=="!first!" set "found=1"
  set "probe=!first:"session_id": "%%~I"=!"
  if not "!probe!"=="!first!" set "found=1"
)
endlocal & set "heard=%found%"
exit /b 0

rem working_session sets the variable its argument names to the Claude Code
rem session recorded as working the story under way in the project %dir% is
rem in, and leaves it unset when there is no such project, no story under way
rem there, or no session recorded. The project is the nearest directory, going
rem up, with .sdlc\config.json; the walk stops at a repository boundary and at
rem the root of the drive.
:working_session
if not defined dir exit /b 1
:climb
if exist "%dir%\.sdlc\config.json" goto recorded
if exist "%dir%\.git" exit /b 1
for %%P in ("%dir%\..") do set "parent=%%~fP"
if /i "%parent%"=="%dir%" exit /b 1
set "dir=%parent%"
goto climb
:recorded
set "state=%dir%\.sdlc\state"
if not exist "%state%\active" exit /b 1
if exist "%state%\active\" exit /b 1
if not exist "%state%\session" exit /b 1
if exist "%state%\session\" exit /b 1
rem Each file holds one line that says anything, as the binary reads it: a
rem line of nothing but spaces and tabs is passed over, the spaces and tabs
rem round a value are not part of it, and a second line with something on it
rem means the file names nothing.
set "lines=0"
for /f "usebackq tokens=1" %%A in ("%state%\active") do set /a lines+=1
if not "%lines%"=="1" exit /b 1
set "id="
set "more="
set "lines=0"
for /f "usebackq tokens=1*" %%S in ("%state%\session") do (
  set "id=%%S"
  set "more=%%T"
  set /a lines+=1
)
if not "%lines%"=="1" exit /b 1
if defined more exit /b 1
rem What `sdlc start` records, and nothing else: the id is read with delayed
rem expansion, which never runs what it expands, and whatever is left once
rem every character an id may hold is taken out means it is not one. Taking
rem a letter out takes out both its cases.
setlocal EnableDelayedExpansion
set "rest=#!id!"
for %%C in (a b c d e f g h i j k l m n o p q r s t u v w x y z 0 1 2 3 4 5 6 7 8 9 . _ -) do set "rest=!rest:%%C=!"
if not "!rest!"=="#" (
  endlocal
  exit /b 1
)
if not "!id:~200!"=="" (
  endlocal
  exit /b 1
)
for %%V in ("!id!") do (
  endlocal
  set "%~1=%%~V"
)
exit /b 0
