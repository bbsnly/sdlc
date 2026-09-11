<#
.SYNOPSIS
  Install sdlc, the story-driven delivery loop for Claude Code.

.DESCRIPTION
  Downloads the release archive for this machine, checks it against the
  release's checksums.txt, and puts a single static binary on your PATH.
  Nothing else is installed and nothing outside the install directory is
  touched.

  The usual way to run this is piped, which cannot take parameters, so the
  same settings are read from the environment:

    irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex

    $env:SDLC_VERSION = '0.1.0'        # a version other than the latest
    $env:SDLC_INSTALL_DIR = 'C:\bin'   # somewhere other than the default
    $env:SDLC_NO_PATH = '1'            # install without touching PATH

.PARAMETER Version
  The version to install. Defaults to the latest release.

.PARAMETER Dir
  Where to install. Defaults to %LOCALAPPDATA%\Programs\sdlc\bin.
#>
[CmdletBinding()]
param(
    [string] $Version = $env:SDLC_VERSION,
    [string] $Dir = $env:SDLC_INSTALL_DIR
)

$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 negotiates TLS 1.0 by default, which github.com
# refuses. Without this the download fails with a connection error that says
# nothing about TLS.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'bbsnly/sdlc'

function Stop-WithAdvice {
    param([string] $What, [string[]] $Advice = @())
    Write-Host "sdlc: $What" -ForegroundColor Red
    foreach ($line in $Advice) { Write-Host "  $line" }
    exit 1
}

if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA 'Programs\sdlc\bin' }
# A leading v is what the tag looks like, and it is the natural thing to
# paste; everything downstream wants it without.
if ($Version) { $Version = $Version -replace '^v', '' }

switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default {
        Stop-WithAdvice "no prebuilt binary for $env:PROCESSOR_ARCHITECTURE." @(
            'Build from source instead:',
            "  go install github.com/$repo/cmd/sdlc@latest")
    }
}

if (-not $Version) {
    # Resolve "latest" through the redirect rather than the API: the API is
    # rate limited per IP, and a shared network can exhaust it for everyone.
    try {
        $response = Invoke-WebRequest -Uri "https://github.com/$repo/releases/latest" `
            -MaximumRedirection 0 -ErrorAction SilentlyContinue -UseBasicParsing
        $location = $response.Headers.Location
    } catch {
        $location = $_.Exception.Response.Headers.Location
    }
    if ($location -match '/tag/v(?<v>[0-9][^/]*)$') {
        $Version = $Matches['v']
    } else {
        Stop-WithAdvice 'could not work out the latest version of sdlc.' @(
            "why  https://github.com/$repo/releases/latest did not redirect to a tag;",
            '     the usual cause is no network, or no release yet',
            'fix  set $env:SDLC_VERSION to the version you want')
    }
}

$archive = "sdlc_${Version}_windows_${arch}.zip"
# SDLC_DOWNLOAD_BASE points this at somewhere other than GitHub. It exists so
# that this script can be tested against a local mirror on every commit,
# rather than only by a real release.
$downloadBase = if ($env:SDLC_DOWNLOAD_BASE) { $env:SDLC_DOWNLOAD_BASE } else { "https://github.com/$repo/releases/download" }
$base = "$downloadBase/v$Version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("sdlc-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    Write-Host "sdlc: downloading $archive"
    try {
        Invoke-WebRequest -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
    } catch {
        Stop-WithAdvice "could not download $archive" @(
            "why  $base/$archive did not answer with the file",
            "fix  check that v$Version is released, and that this machine can reach github.com")
    }
    try {
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing
    } catch {
        Stop-WithAdvice "could not download the checksums for v$Version" @(
            "why  $base/checksums.txt did not answer with the file",
            "fix  the release is incomplete; report it at https://github.com/$repo/issues")
    }

    # Verify before anything is moved onto a PATH directory. A release you
    # cannot check is a release you should not install.
    $expected = $null
    foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and ($parts[1].TrimStart('*') -eq $archive)) { $expected = $parts[0] }
    }
    if (-not $expected) {
        Stop-WithAdvice "checksums.txt does not list $archive" @(
            'why  the release is missing the build for this platform',
            "fix  report it at https://github.com/$repo/issues")
    }

    $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
        Stop-WithAdvice 'the download does not match its checksum, so it is not being installed.' @(
            "expected  $expected",
            "got       $actual",
            'This is either a corrupted download or something worse. Try again; if it',
            "happens twice, report it at https://github.com/$repo/issues")
    }

    Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'unpacked') -Force
    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    Copy-Item -Path (Join-Path $tmp 'unpacked\sdlc.exe') -Destination (Join-Path $Dir 'sdlc.exe') -Force
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host "sdlc: installed v$Version in $Dir"

# Windows has no profile file that a package manager can rely on, so the PATH
# entry is written to the user environment. Nothing machine-wide is touched,
# and an entry that is already there is left alone.
#
# SDLC_NO_PATH=1 installs without touching PATH, which is what a Dockerfile or
# a provisioning script wants: there, PATH is set somewhere else and an entry
# written into a user environment that will not exist tomorrow is noise.
if ($env:SDLC_NO_PATH -eq '1') {
    Write-Host ''
    Write-Host "  PATH was left alone (SDLC_NO_PATH=1). Add $Dir to it yourself."
} else {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $Dir) {
        [Environment]::SetEnvironmentVariable('Path', "$Dir;$userPath", 'User')
        Write-Host ''
        Write-Host "  $Dir has been added to your PATH."
        Write-Host '  Open a new terminal for it to take effect.'
    }
}
$env:Path = "$Dir;$env:Path"

Write-Host @'

Next, in Claude Code:

  /plugin marketplace add bbsnly/sdlc
  /plugin install sdlc@sdlc

Then run `sdlc doctor` in a project to check the install.
'@
