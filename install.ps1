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
[Net.ServicePointManager]::SecurityProtocol =
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# Windows PowerShell 5.1 renders a progress bar for every Invoke-WebRequest,
# and drawing it costs more than downloading the four megabytes it describes.
$ProgressPreference = 'SilentlyContinue'

$repo = 'bbsnly/sdlc'

function Stop-WithAdvice {
    param([string] $What, [string[]] $Advice = @())
    # The error goes to the error stream so that `2>` and CI log capture see
    # it, not only a human watching the console.
    [Console]::Error.WriteLine("sdlc: $What")
    foreach ($line in $Advice) { [Console]::Error.WriteLine("  $line") }
    # `exit` inside `irm ... | iex` -- the invocation this script documents --
    # terminates the user's whole PowerShell session, closing the window on
    # the message it just printed. Throwing stops the script and leaves the
    # session standing. Run as a file, the trailing exit code still applies.
    if ($MyInvocation.ScriptName -or $PSCommandPath) { exit 1 }
    throw "sdlc: $What"
}

if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA 'Programs\sdlc\bin' }
# A leading v is what the tag looks like, and it is the natural thing to
# paste; everything downstream wants it without.
if ($Version) { $Version = $Version -replace '^v', '' }

# PROCESSOR_ARCHITECTURE describes the *process*, not the machine: a 32-bit
# or emulated x64 PowerShell on an ARM64 machine reports x86 or AMD64 and
# would install the wrong binary. OSArchitecture asks the operating system.
$osArch = try {
    [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
} catch {
    if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
}
switch -Regex ($osArch) {
    '^(X64|AMD64)$' { $arch = 'amd64' }
    '^(Arm64|ARM64)$' { $arch = 'arm64' }
    default {
        Stop-WithAdvice "no prebuilt binary for $osArch." @(
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
        $parts = $line.Trim() -split '\s+', 2
        if ($parts.Count -ne 2) { continue }
        # Anchored to 64 hex so that an HTML error page saved under this name
        # cannot supply a "checksum", and so a later duplicate line cannot
        # quietly replace the first one: the first match wins and we stop.
        if ($parts[0] -notmatch '^[0-9a-fA-F]{64}$') { continue }
        if ($parts[1].Trim().TrimStart('*') -eq $archive) { $expected = $parts[0]; break }
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

    Expand-Archive -LiteralPath (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'unpacked') -Force
    $unpacked = Join-Path $tmp 'unpacked\sdlc.exe'
    if (-not (Test-Path -LiteralPath $unpacked)) {
        Stop-WithAdvice "$archive does not contain sdlc.exe" @(
            'why  the archive is not the one this script expects',
            "fix  report it at https://github.com/$repo/issues")
    }
    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    # Copy in under a name PATH cannot resolve, then rename: the copy is the
    # only slow step, and a half-written sdlc.exe on PATH is worse than none.
    # Renaming is also what makes upgrading over a running sdlc.exe fail
    # cleanly instead of leaving a truncated file behind.
    $incoming = Join-Path $Dir ".sdlc.incoming.$PID"
    try {
        Copy-Item -LiteralPath $unpacked -Destination $incoming -Force
        Move-Item -LiteralPath $incoming -Destination (Join-Path $Dir 'sdlc.exe') -Force
    } catch {
        Remove-Item -LiteralPath $incoming -Force -ErrorAction SilentlyContinue
        Stop-WithAdvice "could not install into $Dir" @(
            "why  $($_.Exception.Message)",
            'fix  close any running sdlc.exe and try again, or set',
            '     $env:SDLC_INSTALL_DIR to somewhere you can write')
    }
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
    # Read and write the raw registry value, not the expanded one. The cost of
    # going below [Environment]::SetEnvironmentVariable is that already-open
    # programs are not notified, which is why the message below says to open a
    # new terminal -- it said so before this, and it is still what is true.
    # [Environment]::GetEnvironmentVariable returns %JAVA_HOME%\bin already
    # expanded, and writing that back as a plain string would permanently
    # freeze every such reference in the user's PATH at today's value.
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
        $userPath = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        if (($userPath -split ';') -notcontains $Dir) {
            $kind = if ($userPath -match '%') {
                [Microsoft.Win32.RegistryValueKind]::ExpandString
            } else {
                [Microsoft.Win32.RegistryValueKind]::String
            }
            # A profile with no user PATH yet gets the directory on its own,
            # rather than a value with a trailing separator.
            $updated = if ($userPath) { "$Dir;$userPath" } else { $Dir }
            $key.SetValue('Path', $updated, $kind)
            Write-Host ''
            Write-Host "  $Dir has been added to your PATH."
            Write-Host '  Open a new terminal for it to take effect.'
        }
    } finally {
        if ($key) { $key.Dispose() }
    }
}
$env:Path = "$Dir;$env:Path"

Write-Host @'

Next, in Claude Code:

  /plugin marketplace add bbsnly/sdlc
  /plugin install sdlc@sdlc

Then run `sdlc doctor` in a project to check the install.
'@
