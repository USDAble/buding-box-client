# Restarts only this checkout's development desktop and its Vite service.
# No caller-supplied command, executable, or data directory is accepted.
$ErrorActionPreference = 'Stop'
$clientRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$previewRoot = Join-Path $clientRoot 'tmp\client-preview'
$desktopExe = Join-Path $previewRoot 'PuddingBox.exe'
$nextExe = Join-Path $previewRoot 'PuddingBox-next.exe'
$webRoot = Join-Path $clientRoot 'web'
$viteScript = Join-Path $webRoot 'node_modules\vite\bin\vite.js'

if (-not (Test-Path -LiteralPath $viteScript)) {
    throw 'Frontend dependencies missing. Install web dependencies with npm ci first.'
}
New-Item -ItemType Directory -Path $previewRoot -Force | Out-Null
$priorCGO = $env:CGO_ENABLED
Push-Location (Join-Path $clientRoot 'cmd\octo-desktop')
try {
    $env:CGO_ENABLED = '0'
    & go build '-ldflags=-H windowsgui' -o $nextExe .
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed; current client was not stopped.' }
} finally {
    $env:CGO_ENABLED = $priorCGO
    Pop-Location
}

# Reuse the existing listener only when it is a Vite process from this checkout.
$listeners = @(Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction SilentlyContinue)
foreach ($listener in $listeners) {
    $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener.OwningProcess)"
    $isThisVite = $owner.Name -eq 'node.exe' -and (
        $owner.CommandLine.Contains($viteScript) -or
        $owner.CommandLine -match 'node_modules/vite/bin/vite.js.*--host.*127\.0\.0\.1.*--strictPort'
    )
    if (-not $isThisVite) { throw 'Port 5173 is occupied by another service; refusing to stop it.' }
}
if ($listeners.Count -eq 0) {
    $nodeExe = (Get-Command node.exe -ErrorAction Stop).Source
    Start-Process -FilePath $nodeExe -ArgumentList @('node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--strictPort') -WorkingDirectory $webRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $previewRoot 'vite.stdout.log') -RedirectStandardError (Join-Path $previewRoot 'vite.stderr.log') | Out-Null
}
$viteReady = $false
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    try {
        $response = Invoke-WebRequest 'http://127.0.0.1:5173/@vite/client' -UseBasicParsing -TimeoutSec 2
        if ($response.StatusCode -eq 200) { $viteReady = $true; break }
    } catch {}
    Start-Sleep -Milliseconds 500
}
if (-not $viteReady) { throw 'Vite did not become ready; current client was not stopped.' }

# Match the full executable path, never kill every similarly named process.
Get-Process -Name PuddingBox -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $desktopExe } | ForEach-Object {
    Stop-Process -Id $_.Id -ErrorAction Stop
    Wait-Process -Id $_.Id -Timeout 10 -ErrorAction SilentlyContinue
}
# Windows can retain the executable mapping briefly after process exit.
for ($copyAttempt = 0; $copyAttempt -lt 20; $copyAttempt++) {
    try { Copy-Item -LiteralPath $nextExe -Destination $desktopExe -Force; break }
    catch { if ($copyAttempt -eq 19) { throw }; Start-Sleep -Milliseconds 250 }
}
$priorDataRoot = $env:OCTO_DATA_ROOT
$priorDevURL = $env:OCTO_DESKTOP_DEV_URL
try {
    $env:OCTO_DATA_ROOT = Join-Path $previewRoot 'data'
    $env:OCTO_DESKTOP_DEV_URL = 'http://127.0.0.1:5173'
    $desktop = Start-Process -FilePath $desktopExe -WorkingDirectory $clientRoot -WindowStyle Normal -PassThru
} finally {
    $env:OCTO_DATA_ROOT = $priorDataRoot
    $env:OCTO_DESKTOP_DEV_URL = $priorDevURL
}
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    Start-Sleep -Milliseconds 500
    $desktop.Refresh()
    if ($desktop.HasExited) { throw 'Desktop exited. Inspect tmp/client-preview/data/logs/serve.log.' }
    if ($desktop.MainWindowHandle -ne 0) {
        Write-Output "Desktop window is ready. PID=$($desktop.Id); frontend=http://127.0.0.1:5173"
        exit 0
    }
}
throw 'Desktop process exists, but no window appeared within 15 seconds.'
