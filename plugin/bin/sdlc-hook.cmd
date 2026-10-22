@echo off
rem Hand a hook event to the sdlc binary, wherever it happens to live.
rem Mirrors plugin/bin/sdlc-hook; see that file for why this never blocks, why
rem it says nothing outside the session working a story, and why the first
rem SessionStart fetches the plugin's own release.
setlocal DisableDelayedExpansion
set "event=%~1"

rem What a person can do about each reason the fetch stopped; plugin/bin/sdlc-hook
rem says why every one names SDLC_BIN and the local environment editor.
set "network_fix=check that this machine can reach the release, and open a new session, which tries again. Or, in Claude Desktop, open the local environment editor and set SDLC_BIN to the full path of an sdlc you installed; in a terminal, install it with npx @bbsnly/sdlc install. Then type /sdlc:doctor, or run sdlc doctor in a terminal."
set "plugin_fix=update the plugin from the plugin browser and open a new session; if that does not help, report it to the sdlc project. Do not set SDLC_BIN to a binary you did not install yourself; to use one you did, install it in a terminal with npx @bbsnly/sdlc install and set SDLC_BIN to its full path in the local environment editor of Claude Desktop. Then type /sdlc:doctor, or run sdlc doctor in a terminal."
set "mismatch_fix=open a new session to try again. If it fails the same way, what was served is not the release the plugin pins, so do not run it, and report it to the sdlc project. To use an sdlc you installed yourself, with npx @bbsnly/sdlc install in a terminal, set SDLC_BIN to its full path in the local environment editor of Claude Desktop. Then type /sdlc:doctor, or run sdlc doctor in a terminal."
set "other_fix=in Claude Desktop, open the local environment editor and set SDLC_BIN to the full path of an sdlc you installed; in a terminal, install it with npx @bbsnly/sdlc install, then quit the app fully and reopen it so that it sees the new PATH. Then type /sdlc:doctor, or run sdlc doctor in a terminal."
set "platform_fix=build sdlc for this machine from its source, or ask the sdlc project for a release for it; the installers fetch the same releases, so they do not help here. Then, in Claude Desktop, open the local environment editor and set SDLC_BIN to the full path of the sdlc you built, and type /sdlc:doctor, or run sdlc doctor in a terminal. Until then nothing is enforced."

rem The fetch runs once: on a SessionStart with no binary in the plugin's bin
rem directory, unless SDLC_BIN is set.
set "why="
set "fix="
set "stage="
if /i "%event%"=="SessionStart" if not defined SDLC_BIN if defined CLAUDE_PLUGIN_ROOT if not exist "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" if not exist "%CLAUDE_PLUGIN_ROOT%\bin\sdlc" call :fetch

set "found="
rem A trailing backslash exists only for a directory, which is not a program.
if defined SDLC_BIN if exist "%SDLC_BIN%" if not exist "%SDLC_BIN%\" set "found=%SDLC_BIN%"
if not defined found if defined CLAUDE_PLUGIN_ROOT if exist "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" set "found=%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe"
rem PATH only: cmd.exe, and where with it, look in the current directory
rem first, and a hook runs in the project. A bare `sdlc` ran an sdlc.cmd
rem sitting at its root on every tool call, in place of the installed binary.
rem Every installer installs sdlc.exe itself, not a script beside it, so only
rem sdlc.exe is looked up.
if not defined found for /f "delims=" %%B in ('where $PATH:sdlc.exe 2^>nul') do if not defined found set "found=%%B"
rem Where the installers put it, install.ps1 and "npx @bbsnly/sdlc install"
rem alike, found without PATH. The npm installer never adds it to PATH, and a
rem desktop app such as Claude Desktop keeps the PATH it started with, so a binary
rem every new terminal found was not found here. PATH comes first.
if not defined found if defined LOCALAPPDATA if exist "%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe" if not exist "%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe\" set "found=%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe"
if not defined found goto not_found
if defined SDLC_DEBUG_FILE >>"%SDLC_DEBUG_FILE%" echo sdlc-hook %event%: running "%found%"
"%found%" hook %1
exit /b 0

:not_found
if defined SDLC_DEBUG_FILE >>"%SDLC_DEBUG_FILE%" echo sdlc-hook %event%: no sdlc binary was found
if /i "%event%"=="SessionStart" goto session_start
if /i "%event%"=="UserPromptExpansion" goto typed

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
echo {"continue":true,"systemMessage":"sdlc: the sdlc binary was not found, so nothing is being enforced. why: it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH this app started with, or where the installers put it. fix: in Claude Desktop, open the local environment editor and set SDLC_BIN to the full path of an sdlc you installed; in a terminal, install it with npx @bbsnly/sdlc install, then quit the app fully and reopen it so that it sees the new PATH. Then type /sdlc:doctor, or run sdlc doctor in a terminal."}
echo sdlc: the sdlc binary was not found, so nothing is being enforced.>&2
echo.>&2
echo   why  it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH>&2
echo        this app started with, or where the installers put it>&2
echo   fix  in Claude Desktop, open the local environment editor and set SDLC_BIN>&2
echo        to the full path of an sdlc you installed; in a terminal, install it>&2
echo        with "npx @bbsnly/sdlc install", then quit the app fully and reopen it>&2
echo        so that it sees the new PATH. Then type /sdlc:doctor, or run>&2
echo        "sdlc doctor" in a terminal.>&2
exit /b 0

rem A session that has just started is in a project or not: one that takes
rem part hears what happened, any other hears nothing. The payload is read to
rem the end either way, so the caller never writes into a closed pipe.
:session_start
"%SystemRoot%\System32\findstr.exe" "^" >nul 2>nul
set "dir=%CLAUDE_PROJECT_DIR%"
call :project_of && goto session_told
set "dir=%CD%"
call :project_of && goto session_told
echo {"continue":true}
exit /b 0
:session_told
if not defined why set "fix=%other_fix%"
if not defined why set "why=it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH this app started with, or where the installers put it"
echo {"continue":true,"systemMessage":"sdlc: the sdlc binary was not found, so nothing is being enforced. why: %why%. fix: %fix%"}
exit /b 0

rem A person who typed one of the plugin's commands is told it did not run, in
rem any project. The name is matched, never echoed back.
:typed
"%SystemRoot%\System32\findstr.exe" /l /c:"\"command_name\":\"sdlc:" /c:"\"command_name\": \"sdlc:" >nul 2>nul
if errorlevel 1 exit /b 0
echo {"continue":true,"decision":"block","reason":"sdlc: the command you typed did not run, because the sdlc binary was not found, so nothing is being enforced. why: it is not at SDLC_BIN, in the bin directory of the plugin, on the PATH this app started with, or where the installers put it. fix: %other_fix% Then type your command again."}
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

rem project_of sets dir to the project %dir% is in: the nearest directory, going
rem up, with .sdlc\config.json. The walk stops at a repository boundary and at
rem the root of the drive, and fails there.
:project_of
if not defined dir exit /b 1
:climb
if exist "%dir%\.sdlc\config.json" exit /b 0
if exist "%dir%\.git" exit /b 1
for %%P in ("%dir%\..") do set "parent=%%~fP"
if /i "%parent%"=="%dir%" exit /b 1
set "dir=%parent%"
goto climb

rem working_session sets the variable its argument names to the Claude Code
rem session recorded as working the story under way in the project %dir% is
rem in, and leaves it unset when there is no such project, no story under way
rem there, or no session recorded.
:working_session
call :project_of || exit /b 1
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

rem fetch puts the plugin's own release of sdlc in the plugin's bin directory,
rem or sets why to what stopped it and fix to what a person can do about it.
rem Every program it runs is named by its full path, because cmd.exe looks in
rem the current directory first, and a hook runs in the project.
:fetch
set "fix=%other_fix%"
set "bin=%CLAUDE_PLUGIN_ROOT%\bin"
set "ver="
if not exist "%CLAUDE_PLUGIN_ROOT%\.claude-plugin\plugin.json" goto no_version
for /f "usebackq tokens=1* delims=:" %%A in ("%CLAUDE_PLUGIN_ROOT%\.claude-plugin\plugin.json") do (
  set "key=%%A"
  set "ver=%%B"
  call :is_version_key && goto version_found
)
goto no_version
:version_found
if not defined ver goto no_version
set "ver=%ver:"=%"
set "ver=%ver: =%"
set "ver=%ver:,=%"
rem It goes into a URL, a file name and the JSON written below, so it holds
rem nothing but digits, letters, dots and hyphens, and starts with a digit.
setlocal EnableDelayedExpansion
set "rest=#!ver!"
for %%C in (a b c d e f g h i j k l m n o p q r s t u v w x y z 0 1 2 3 4 5 6 7 8 9 . -) do set "rest=!rest:%%C=!"
set "lead=!ver:~0,1!"
set "digit="
for %%D in (0 1 2 3 4 5 6 7 8 9) do if "!lead!"=="%%D" set "digit=1"
if not "!rest!"=="#" (
  endlocal
  goto no_version
)
if not defined digit (
  endlocal
  goto no_version
)
if not "!ver:~64!"=="" (
  endlocal
  goto no_version
)
endlocal

set "arch=%PROCESSOR_ARCHITEW6432%"
if not defined arch set "arch=%PROCESSOR_ARCHITECTURE%"
if /i "%arch%"=="AMD64" set "arch=amd64"
if /i "%arch%"=="ARM64" set "arch=arm64"
if not "%arch%"=="amd64" if not "%arch%"=="arm64" goto no_platform
set "archive=sdlc_%ver%_windows_%arch%.zip"

rem Both of these come before any request; see plugin/bin/sdlc-hook.
if not exist "%CLAUDE_PLUGIN_ROOT%\checksums.txt" goto no_pins
set "want="
for /f "usebackq tokens=1,2" %%A in ("%CLAUDE_PLUGIN_ROOT%\checksums.txt") do (
  if "%%B"=="%archive%" set "want=%%A"
  if "%%B"=="*%archive%" set "want=%%A"
)
if not defined want goto no_pin
setlocal EnableDelayedExpansion
set "rest=#!want!"
for %%C in (a b c d e f 0 1 2 3 4 5 6 7 8 9) do set "rest=!rest:%%C=!"
if not "!rest!"=="#" (
  endlocal
  goto no_pin
)
if "!want:~63,1!"=="" (
  endlocal
  goto no_pin
)
if not "!want:~64!"=="" (
  endlocal
  goto no_pin
)
endlocal

rem A staging directory a killed launcher left behind a day ago is nobody's.
"%SystemRoot%\System32\forfiles.exe" /p "%bin%" /m ".sdlc-fetch.*" /d -1 /c "%ComSpec% /c if @isdir==TRUE rd /s /q @path" <nul >nul 2>nul
rem %RANDOM% is seeded from the clock when cmd.exe starts, so two launchers
rem started together can pick the same name; a name taken is a reason to pick
rem another, never to share it.
set "tries=0"
:stage_again
set /a tries+=1
set "stage=%bin%\.sdlc-fetch.%RANDOM%%RANDOM%%RANDOM%"
mkdir "%stage%" >nul 2>nul && goto staged
set "stage="
if %tries% lss 8 goto stage_again
goto no_write
:staged

set "base=https://github.com/bbsnly/sdlc/releases/download"
if defined SDLC_DOWNLOAD_BASE set "base=%SDLC_DOWNLOAD_BASE%"
if not exist "%SystemRoot%\System32\curl.exe" goto no_fetch
"%SystemRoot%\System32\curl.exe" -fsSL --retry 2 --connect-timeout 10 --max-time 50 --retry-max-time 60 -o "%stage%\%archive%" "%base%/v%ver%/%archive%" <nul >nul 2>nul
if errorlevel 1 goto no_fetch

if not exist "%SystemRoot%\System32\certutil.exe" goto no_check
"%SystemRoot%\System32\certutil.exe" -hashfile "%stage%\%archive%" SHA256 <nul >"%stage%\sum" 2>nul
set "got="
for /f "usebackq skip=1 delims=" %%H in ("%stage%\sum") do if not defined got set "got=%%H"
if not defined got goto mismatch
rem Older certutil puts a space between each byte.
set "got=%got: =%"
if /i not "%got%"=="%want%" goto mismatch

rem Only the binary comes out of the archive.
if exist "%SystemRoot%\System32\tar.exe" "%SystemRoot%\System32\tar.exe" -xf "%stage%\%archive%" -C "%stage%" sdlc.exe <nul >nul 2>nul
if not exist "%stage%\sdlc.exe" goto no_unpack
rem A running sdlc.exe cannot be replaced, and is the same release.
move /y "%stage%\sdlc.exe" "%bin%\sdlc.exe" >nul 2>nul
if not exist "%bin%\sdlc.exe" goto no_write
goto fetch_done

:no_version
set "why=the version of the plugin could not be read from its plugin.json"
set "fix=%plugin_fix%"
goto fetch_done
:no_platform
set "why=no release of sdlc is built for this operating system and processor"
set "fix=%platform_fix%"
goto fetch_done
:no_pins
set "why=the plugin carries no checksums.txt, so no download can be checked against a pinned checksum"
set "fix=%plugin_fix%"
goto fetch_done
:no_pin
set "why=the plugin's checksums.txt holds no checksum for sdlc %ver% on windows_%arch%"
set "fix=%plugin_fix%"
goto fetch_done
:no_write
set "why=the bin directory of the plugin could not be written to"
goto fetch_done
:no_fetch
set "why=could not fetch sdlc %ver% (%archive%) from the release"
set "fix=%network_fix%"
goto fetch_done
:no_check
set "why=nothing here can check a download against its pinned checksum (certutil)"
goto fetch_done
:mismatch
set "why=the download does not match the checksum the plugin pins for it, so it was neither installed nor run"
set "fix=%mismatch_fix%"
goto fetch_done
:no_unpack
set "why=the download could not be unpacked"
goto fetch_done

rem Every way out of the fetch comes through here, so the staging directory, and
rem the archive in it, never outlive it.
:fetch_done
if defined stage rd /s /q "%stage%" >nul 2>nul
set "stage="
if not defined SDLC_DEBUG_FILE exit /b 0
if defined why >>"%SDLC_DEBUG_FILE%" echo sdlc-hook %event%: the fetch stopped: %why%
if not defined why >>"%SDLC_DEBUG_FILE%" echo sdlc-hook %event%: fetched sdlc %ver% into the plugin's bin directory
exit /b 0

rem is_version_key reports whether %key%, the part of a plugin.json line before
rem its first colon, is the "version" key.
:is_version_key
set "key=%key:"=%"
set "key=%key: =%"
if "%key%"=="version" exit /b 0
exit /b 1
