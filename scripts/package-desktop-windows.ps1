#!/usr/bin/env pwsh
<#
.SYNOPSIS
Builds a locally testable Windows installer for the Pudding Box desktop app.

.DESCRIPTION
Builds the web UI, verifies generated branding, embeds ripgrep for the requested
Windows target architecture, creates the desktop and CLI executables, stages uv,
and invokes Inno Setup 6. It does not alter repository or user-level Go settings.

.EXAMPLE
pwsh .\scripts\package-desktop-windows.ps1 -Arch amd64 -Version 0.0.0-local

.EXAMPLE
pwsh .\scripts\package-desktop-windows.ps1 -Arch arm64 -Version 0.0.0-local
#>
[CmdletBinding()]
param(
    # Target architecture of the installer, not necessarily the architecture of this build PC.
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    # Local validation versions may use a prerelease suffix, for example 0.0.0-local.
    [string]$Version = '0.0.0-local',

    # Optional explicit Go executable. When omitted, an official installation is preferred.
    [string]$GoExecutable,

    # Optional explicit Inno Setup 6 compiler executable.
    [string]$IsccExecutable,

    # Reuse a previously built web UI only when it is known to be current.
    [switch]$SkipWebBuild,

    # Preserve this script's temporary uv download directory for troubleshooting.
    [switch]$KeepDownloads,

    # Optional pre-downloaded uv.exe, useful for offline/repeatable local validation.
    [string]$UvExecutable,

    # Optional output directory; defaults to staging or staging-arm64 inside the repository.
    [string]$OutputDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$repositoryRoot = (Resolve-Path -LiteralPath $repositoryRoot).Path
$desktopDirectory = Join-Path $repositoryRoot 'cmd\octo-desktop'
$embedScript = Join-Path $repositoryRoot 'scripts\embed-rg-windows.ps1'
$syncScript = Join-Path $repositoryRoot 'scripts\sync-branding.mjs'
$installerScript = Join-Path $repositoryRoot 'packaging\windows\octo.iss'
$licensePath = Join-Path $repositoryRoot 'LICENSE.txt'

function Restore-EnvironmentVariable {
    param(
        [Parameter(Mandatory)] [string]$Name,
        [AllowNull()] [string]$Value
    )

    if ($null -eq $Value) {
        Remove-Item -LiteralPath "Env:$Name" -ErrorAction SilentlyContinue
    } else {
        Set-Item -LiteralPath "Env:$Name" -Value $Value
    }
}

function Invoke-CheckedCommand {
    param(
        [Parameter(Mandatory)] [string]$Description,
        [Parameter(Mandatory)] [scriptblock]$Command
    )

    Write-Host "==> $Description"
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Description failed with exit code $LASTEXITCODE."
    }
}

function Resolve-CommandPath {
    param(
        [Parameter(Mandatory)] [string]$CommandName,
        [string]$PreferredPath
    )

    if ($PreferredPath) {
        $candidate = [Environment]::ExpandEnvironmentVariables($PreferredPath)
        if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            throw "$CommandName was explicitly set to '$PreferredPath', but that file does not exist."
        }
        return (Resolve-Path -LiteralPath $candidate).Path
    }

    $command = Get-Command -Name $CommandName -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        throw "Could not find $CommandName on PATH. Install it and reopen PowerShell."
    }
    return $command.Source
}

function Resolve-GoExecutable {
    param([string]$PreferredPath)

    if ($PreferredPath) {
        return Resolve-CommandPath -CommandName 'go.exe' -PreferredPath $PreferredPath
    }

    # Prefer the normal official Windows installation over a stale x86 entry earlier on PATH.
    $officialGo = Join-Path ${env:ProgramFiles} 'Go\bin\go.exe'
    if (Test-Path -LiteralPath $officialGo -PathType Leaf) {
        return (Resolve-Path -LiteralPath $officialGo).Path
    }
    return Resolve-CommandPath -CommandName 'go.exe'
}

function Resolve-InnoSetupCompiler {
    param([string]$PreferredPath)

    if ($PreferredPath) {
        return Resolve-CommandPath -CommandName 'ISCC.exe' -PreferredPath $PreferredPath
    }

    $candidates = [System.Collections.Generic.List[string]]::new()
    foreach ($path in @(
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path ${env:ProgramFiles} 'Inno Setup 6\ISCC.exe'),
        (Join-Path ${env:LOCALAPPDATA} 'Programs\Inno Setup 6\ISCC.exe')
    )) {
        if ($path) { $candidates.Add($path) }
    }

    foreach ($registryRoot in @(
        'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall',
        'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall',
        'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'
    )) {
        Get-ChildItem -LiteralPath $registryRoot -ErrorAction SilentlyContinue |
            ForEach-Object { Get-ItemProperty -LiteralPath $_.PSPath -ErrorAction SilentlyContinue } |
            ForEach-Object {
                # StrictMode treats absent registry properties as an error. Many unrelated
                # uninstall entries have neither property, so inspect their property bag first.
                $displayNameProperty = $_.PSObject.Properties['DisplayName']
                $installLocationProperty = $_.PSObject.Properties['InstallLocation']
                if ($null -ne $displayNameProperty -and $null -ne $installLocationProperty -and
                    $displayNameProperty.Value -match '^Inno Setup version 6' -and
                    $installLocationProperty.Value) {
                    $candidates.Add((Join-Path $installLocationProperty.Value 'ISCC.exe'))
                }
            }
    }

    $onPath = Get-Command -Name 'ISCC.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -ne $onPath) { $candidates.Add($onPath.Source) }

    foreach ($candidate in $candidates | Select-Object -Unique) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    throw @"
Inno Setup 6 (ISCC.exe) was not found. Install it, then rerun this script:
  winget install --id JRSoftware.InnoSetup --exact --accept-source-agreements --accept-package-agreements
Or supply -IsccExecutable 'C:\path\to\ISCC.exe'.
"@
}

function Invoke-TargetGo {
    param(
        [Parameter(Mandatory)] [string[]]$GoArguments
    )

    # These values live only for the child go process invocation and are restored even on failure.
    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = $Arch
        $env:CGO_ENABLED = '0'
        & $go @GoArguments
        if ($LASTEXITCODE -ne 0) {
            throw "go $($GoArguments -join ' ') failed with exit code $LASTEXITCODE."
        }
    } finally {
        Restore-EnvironmentVariable -Name 'GOOS' -Value $previousGOOS
        Restore-EnvironmentVariable -Name 'GOARCH' -Value $previousGOARCH
        Restore-EnvironmentVariable -Name 'CGO_ENABLED' -Value $previousCGO
    }
}

if (-not $IsWindows) {
    throw 'This script packages a Windows installer and must run on Windows.'
}
if (-not [Environment]::Is64BitOperatingSystem) {
    throw 'A 64-bit Windows host is required. Desktop installers are produced only for amd64 and arm64.'
}
if ([string]::IsNullOrWhiteSpace($Version) -or $Version -match '[\s"]') {
    throw "Version '$Version' is invalid. Use a non-empty value without spaces or quotation marks, for example 0.0.0-local."
}

$go = Resolve-GoExecutable -PreferredPath $GoExecutable
$node = Resolve-CommandPath -CommandName 'node.exe'
$npm = Resolve-CommandPath -CommandName 'npm.cmd'
$iscc = Resolve-InnoSetupCompiler -PreferredPath $IsccExecutable

$goEnvironment = & $go env GOOS GOHOSTARCH
if ($LASTEXITCODE -ne 0 -or $goEnvironment.Count -lt 2) {
    throw "Could not inspect Go toolchain '$go'."
}
$hostGOOS = $goEnvironment[0].Trim()
$hostGOARCH = $goEnvironment[1].Trim()
if ($hostGOOS -ne 'windows' -or $hostGOARCH -notin @('amd64', 'arm64')) {
    throw "Go toolchain '$go' reports $hostGOOS/$hostGOARCH. Install native official Windows amd64 or arm64 Go; windows/386 is unsupported for desktop packaging."
}

$stageName = if ($Arch -eq 'arm64') { 'staging-arm64' } else { 'staging' }
$outputName = if ($Arch -eq 'arm64') { 'octo-setup-arm64' } else { 'octo-setup' }
$archAllowed = if ($Arch -eq 'arm64') { 'arm64' } else { 'x64compatible' }
$stageDirectory = if ($OutputDirectory) {
    $expandedOutputDirectory = [Environment]::ExpandEnvironmentVariables($OutputDirectory)
    if ([IO.Path]::IsPathRooted($expandedOutputDirectory)) {
        $expandedOutputDirectory
    } else {
        Join-Path $repositoryRoot $expandedOutputDirectory
    }
} else {
    Join-Path $repositoryRoot $stageName
}
$stageDirectory = [IO.Path]::GetFullPath($stageDirectory)
$installerPath = Join-Path $stageDirectory "$outputName.exe"
$workDirectory = Join-Path $repositoryRoot (Join-Path 'dl' "package-desktop-windows-$Arch")

# Validate prerequisites before writing build outputs.
foreach ($path in @($desktopDirectory, $embedScript, $syncScript, $installerScript, $licensePath)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Required project file is missing: $path" }
}

Write-Host "Packaging Pudding Box for windows/$Arch using $go (host $hostGOOS/$hostGOARCH)."
Write-Host "Output directory: $stageDirectory"

Push-Location $repositoryRoot
try {
    Invoke-CheckedCommand -Description 'checking generated branding' -Command {
        & $node $syncScript --check
    }

    if (-not $SkipWebBuild) {
        Invoke-CheckedCommand -Description 'installing locked web dependencies' -Command {
            & $npm --prefix (Join-Path $repositoryRoot 'web') ci
        }
        Invoke-CheckedCommand -Description 'building web UI' -Command {
            & $npm --prefix (Join-Path $repositoryRoot 'web') run build
        }
    } elseif (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot 'internal\server\webdist\index.html') -PathType Leaf)) {
        throw '-SkipWebBuild was supplied, but internal\server\webdist\index.html is missing. Build the web UI first or omit -SkipWebBuild.'
    }

    Invoke-CheckedCommand -Description "embedding ripgrep for windows/$Arch" -Command {
        & $embedScript -Arch $Arch
    }

    New-Item -ItemType Directory -Force -Path $stageDirectory | Out-Null
    $desktopExecutable = Join-Path $stageDirectory 'octo-desktop.exe'
    $cliExecutable = Join-Path $stageDirectory 'octo.exe'

    Invoke-CheckedCommand -Description "generating Windows icon and manifest resource for $Arch" -Command {
        Invoke-TargetGo -GoArguments @('-C', $desktopDirectory, 'run', './build/windows/generate-syso', $Arch, 'build/windows/icon.ico', 'build/windows/wails.exe.manifest', $Version)
    }

    $commit = (& git -C $repositoryRoot rev-parse --short HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) { $commit = 'local' }
    $versionPackage = 'github.com/open-octo/octo-agent/internal/version'
    $desktopLdFlags = "-H windowsgui -X $versionPackage.Version=$Version -X $versionPackage.Commit=$commit"
    $cliLdFlags = "-X $versionPackage.Version=$Version -X $versionPackage.Commit=$commit"

    Invoke-CheckedCommand -Description "building Pudding Box desktop executable for windows/$Arch" -Command {
        Invoke-TargetGo -GoArguments @('-C', $desktopDirectory, 'build', '-v', '-tags', 'embedrg', '-ldflags', $desktopLdFlags, '-o', $desktopExecutable, '.')
    }
    Invoke-CheckedCommand -Description "building Pudding Box CLI for windows/$Arch" -Command {
        Invoke-TargetGo -GoArguments @('build', '-v', '-ldflags', $cliLdFlags, '-o', $cliExecutable, './cmd/octo')
    }
    Copy-Item -LiteralPath $licensePath -Destination (Join-Path $stageDirectory 'LICENSE.txt') -Force

    $makefile = Get-Content -LiteralPath (Join-Path $repositoryRoot 'Makefile') -Raw
    if ($makefile -notmatch '(?m)^UV_VERSION\s*:=\s*(?<uvVersion>[0-9]+(?:\.[0-9]+)+)\s*$') {
        throw 'Could not find a pinned UV_VERSION in Makefile.'
    }
    $uvVersion = $Matches.uvVersion
    $uvArch = if ($Arch -eq 'arm64') { 'aarch64' } else { 'x86_64' }
    $uvZip = Join-Path $workDirectory 'uv.zip'
    $uvExtractDirectory = Join-Path $workDirectory 'uv'

    New-Item -ItemType Directory -Force -Path $workDirectory | Out-Null
    try {
        if ($UvExecutable) {
            Invoke-CheckedCommand -Description "staging supplied uv.exe for windows/$Arch" -Command {
                $uvPath = [Environment]::ExpandEnvironmentVariables($UvExecutable)
                if (-not (Test-Path -LiteralPath $uvPath -PathType Leaf)) {
                    throw "The supplied uv executable does not exist: $UvExecutable"
                }
                $resolvedUvPath = (Resolve-Path -LiteralPath $uvPath).Path
                $destinationUvPath = Join-Path (Resolve-Path -LiteralPath $stageDirectory).Path 'uv.exe'
                if ($resolvedUvPath -ne $destinationUvPath) {
                    Copy-Item -LiteralPath $resolvedUvPath -Destination $destinationUvPath -Force
                }
            }
        } else {
            Invoke-CheckedCommand -Description "downloading bundled uv $uvVersion for windows/$Arch" -Command {
                $uvUrl = "https://github.com/astral-sh/uv/releases/download/$uvVersion/uv-$uvArch-pc-windows-msvc.zip"
                Invoke-WebRequest -Uri $uvUrl -OutFile $uvZip -TimeoutSec 120
                Expand-Archive -LiteralPath $uvZip -DestinationPath $uvExtractDirectory -Force
                $uv = Get-ChildItem -LiteralPath $uvExtractDirectory -Recurse -Filter 'uv.exe' | Select-Object -First 1
                if ($null -eq $uv) { throw "uv.exe was not found in $uvUrl." }
                Copy-Item -LiteralPath $uv.FullName -Destination (Join-Path $stageDirectory 'uv.exe') -Force
            }
        }
    } finally {
        if (-not $KeepDownloads -and (Test-Path -LiteralPath $workDirectory)) {
            Remove-Item -LiteralPath $workDirectory -Recurse -Force
        }
    }

    # Do not let a pre-existing installer make a failed compiler invocation look successful.
    if (Test-Path -LiteralPath $installerPath -PathType Leaf) {
        Remove-Item -LiteralPath $installerPath -Force
    }
    Invoke-CheckedCommand -Description "compiling Inno Setup installer for windows/$Arch" -Command {
        & $iscc "/DAppVersion=$Version" "/DSourceDir=$stageDirectory" "/DArchAllowed=$archAllowed" "/DOutputName=$outputName" "/O$stageDirectory" $installerScript
    }

    if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
        throw "Inno Setup reported success but did not produce $installerPath."
    }
    $installer = Get-Item -LiteralPath $installerPath
    $hash = (Get-FileHash -LiteralPath $installerPath -Algorithm SHA256).Hash
    Write-Host ''
    Write-Host 'Windows installer created successfully:' -ForegroundColor Green
    Write-Host "  Path:   $($installer.FullName)"
    Write-Host "  Size:   $($installer.Length) bytes"
    Write-Host "  SHA256: $hash"
    Write-Host "  Target: windows/$Arch"
} finally {
    Pop-Location
}
