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
rem Every installer puts sdlc.exe itself on PATH.
for /f "delims=" %%B in ('where $PATH:sdlc.exe 2^>nul') do (
  "%%B" hook %1
  exit /b 0
)

rem systemMessage is what reaches the session: stderr from a hook that exits 0
rem goes to the debug log only. See plugin/bin/sdlc-hook.
echo {"continue":true,"systemMessage":"sdlc: the sdlc binary was not found, so nothing is being enforced. why: the plugin is installed, but the binary it drives is not on PATH and is not in the bin directory of the plugin. fix: run sdlc doctor in your terminal; if that also fails, reinstall with npx @bbsnly/sdlc install"}
echo sdlc: the sdlc binary was not found, so nothing is being enforced.>&2
echo.>&2
echo   why  the plugin is installed but the binary it drives is not on PATH and>&2
echo        is not in the plugin's own bin directory>&2
echo   fix  run "sdlc doctor" in your terminal; if that also fails, reinstall>&2
echo        with "npx @bbsnly/sdlc install">&2
exit /b 0
