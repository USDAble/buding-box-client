#!/usr/bin/env pwsh
<#
.SYNOPSIS
Regenerates every Pudding Box logo asset from one canonical 1024x1024 PNG.

.DESCRIPTION
This is the single convenience entrypoint for replacing the logo. It invokes the
cross-platform Go generator, synchronizes generated brand targets, checks for
configuration drift, and runs the branding tests. It never sets GOARCH and does
not require ImageMagick, iconutil, Python, or manually editing platform assets.

.EXAMPLE
pwsh .\scripts\update-brand-assets.ps1 -InputPath 'C:\path\to\logo-1024.png'
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InputPath,

    # Optional explicit Go executable. If omitted, the native official install is preferred on Windows.
    [string]$GoExecutable,

    # Optional explicit Node executable.
    [string]$NodeExecutable
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$generatorDirectory = Join-Path $repositoryRoot 'cmd\generate-brand-assets'
$syncScript = Join-Path $repositoryRoot 'scripts\sync-branding.mjs'
$brandConfig = Join-Path $repositoryRoot 'branding\brand.json'

function Resolve-Tool {
    param(
        [Parameter(Mandatory)] [string]$CommandName,
        [string]$PreferredPath,
        [string]$WindowsOfficialPath
    )

    if ($PreferredPath) {
        $candidate = [Environment]::ExpandEnvironmentVariables($PreferredPath)
        if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            throw "$CommandName was explicitly set to '$PreferredPath', but that file does not exist."
        }
        return (Resolve-Path -LiteralPath $candidate).Path
    }

    if ($IsWindows -and $WindowsOfficialPath -and (Test-Path -LiteralPath $WindowsOfficialPath -PathType Leaf)) {
        return (Resolve-Path -LiteralPath $WindowsOfficialPath).Path
    }

    $command = Get-Command -Name $CommandName -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        throw "Could not find $CommandName. Install it or supply the corresponding explicit path."
    }
    return $command.Source
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

$inputFile = (Resolve-Path -LiteralPath $InputPath -ErrorAction Stop).Path
if ([IO.Path]::GetExtension($inputFile) -ine '.png') {
    throw "Logo input must be a PNG file: $inputFile"
}
foreach ($requiredPath in @($generatorDirectory, $syncScript, $brandConfig)) {
    if (-not (Test-Path -LiteralPath $requiredPath)) {
        throw "Required project file is missing: $requiredPath"
    }
}

$officialGo = if ($IsWindows) { Join-Path ${env:ProgramFiles} 'Go\bin\go.exe' } else { $null }
$go = Resolve-Tool -CommandName ($(if ($IsWindows) { 'go.exe' } else { 'go' })) -PreferredPath $GoExecutable -WindowsOfficialPath $officialGo
$node = Resolve-Tool -CommandName ($(if ($IsWindows) { 'node.exe' } else { 'node' })) -PreferredPath $NodeExecutable

$generatedAssets = @(
    'branding\assets\pudding-box-icon-1024.png',
    'branding\assets\pudding-box-icon-512.png',
    'branding\assets\pudding-box-icon-256.png',
    'branding\assets\pudding-box-icon-192.png',
    'branding\assets\pudding-box-icon-180.png',
    'branding\assets\pudding-box-icon-152.png',
    'branding\assets\pudding-box-icon-144.png',
    'branding\assets\pudding-box-icon-128.png',
    'branding\assets\pudding-box-icon-120.png',
    'branding\assets\pudding-box-icon-96.png',
    'branding\assets\pudding-box-icon-72.png',
    'branding\assets\pudding-box-icon-64.png',
    'branding\assets\pudding-box-icon-48.png',
    'branding\assets\pudding-box-icon-32.png',
    'branding\assets\pudding-box-icon-16.png',
    'branding\assets\pudding-box-mark.svg',
    'cmd\octo-desktop\build\windows\icon.ico',
    'cmd\octo-desktop\build\linux\icon.png',
    'cmd\octo-desktop\build\darwin\tray-icon.png',
    'cmd\octo-desktop\build\darwin\icon.icns'
)

Write-Host "Updating all Pudding Box logo assets from: $inputFile"
Write-Host "Generator tool: $go"
Write-Host "Node tool:      $node"

Push-Location $repositoryRoot
try {
    Invoke-CheckedCommand -Description 'generating PNG, SVG, ICO, and ICNS logo assets' -Command {
        & $go run ./cmd/generate-brand-assets --input $inputFile
    }
    Invoke-CheckedCommand -Description 'synchronizing generated brand targets' -Command {
        & $node $syncScript
    }
    Invoke-CheckedCommand -Description 'checking generated brand targets' -Command {
        & $node $syncScript --check
    }
    Invoke-CheckedCommand -Description 'running branding tests' -Command {
        & $node --test scripts/sync-branding.test.mjs scripts/brand-ci.test.mjs scripts/package-desktop-windows.test.mjs scripts/update-brand-assets.test.mjs
    }

    Write-Host ''
    Write-Host 'Logo assets updated successfully:' -ForegroundColor Green
    foreach ($relativePath in $generatedAssets) {
        $absolutePath = Join-Path $repositoryRoot $relativePath
        if (-not (Test-Path -LiteralPath $absolutePath -PathType Leaf)) {
            throw "Generator did not produce expected asset: $absolutePath"
        }
        $file = Get-Item -LiteralPath $absolutePath
        $hash = (Get-FileHash -LiteralPath $absolutePath -Algorithm SHA256).Hash
        Write-Host ("  {0} ({1} bytes, SHA256 {2})" -f $relativePath, $file.Length, $hash)
    }
    Write-Host ''
    Write-Host 'Next: review git diff, update branding\brand.json if the temporary-logo flag is changing, then rebuild each target package.'
} finally {
    Pop-Location
}
