@echo off
rem Hand a hook event to the sdlc binary, wherever it happens to live.
rem Mirrors plugin/bin/sdlc-hook; see that file for why this never blocks.
setlocal

if defined SDLC_BIN if exist "%SDLC_BIN%" (
  "%SDLC_BIN%" hook %1
  exit /b 0
)
if defined CLAUDE_PLUGIN_ROOT if exist "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" (
  "%CLAUDE_PLUGIN_ROOT%\bin\sdlc.exe" hook %1
  exit /b 0
)
where sdlc >nul 2>&1
if %ERRORLEVEL% equ 0 (
  sdlc hook %1
  exit /b 0
)

echo {"continue":true}
echo sdlc: the sdlc binary was not found, so nothing is being enforced.>&2
echo.>&2
echo   why  the plugin is installed but the binary it drives is not on PATH and>&2
echo        is not in the plugin's own bin directory>&2
echo   fix  run "sdlc doctor" in your terminal; if that also fails, reinstall>&2
echo        with "npx @bbsnly/sdlc install">&2
exit /b 0
