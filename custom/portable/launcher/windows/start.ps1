param([Parameter(Mandatory = $true)][string]$AppRoot)

$ErrorActionPreference = 'Stop'
$AppRoot = (Resolve-Path $AppRoot).Path.TrimEnd('\')
$architecture = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($architecture.ToUpperInvariant()) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "不支持的 CPU 架构：$architecture" }
}

$manifest = Join-Path $AppRoot 'manifest.sha256'
if (-not (Test-Path $manifest)) { throw "缺少完整性清单：$manifest" }
foreach ($line in [IO.File]::ReadAllLines($manifest)) {
    if (-not $line) { continue }
    if ($line -notmatch '^([0-9a-fA-F]{64})  (.+)$') { throw "完整性清单格式无效：$line" }
    $expectedHash = $Matches[1]
    $relativePath = $Matches[2].Replace('/', '\')
    if ($relativePath.StartsWith('.\')) { $relativePath = $relativePath.Substring(2) }
    $targetPath = [IO.Path]::GetFullPath((Join-Path $AppRoot $relativePath))
    if (-not $targetPath.StartsWith($AppRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "完整性清单路径逃逸应用根目录：$relativePath"
    }
    if (-not (Test-Path -LiteralPath $targetPath -PathType Leaf)) { throw "应用文件缺失：$relativePath" }
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $targetPath).Hash
    if (-not $actualHash.Equals($expectedHash, [StringComparison]::OrdinalIgnoreCase)) {
        throw "应用文件完整性校验失败：$relativePath"
    }
}

$originalLocalAppData = $env:LOCALAPPDATA
$originalProgramFiles = $env:ProgramFiles
$originalProgramFilesX86 = ${env:ProgramFiles(x86)}
$dataRoot = Join-Path $AppRoot 'data\windows'
$portableHome = Join-Path $dataRoot 'application-home'
$browserProfile = Join-Path $dataRoot 'browser-profile'
$browserCache = Join-Path $dataRoot 'browser-cache'
$gatewayData = Join-Path $dataRoot 'gateway'
$logsDir = Join-Path $dataRoot 'logs'
$tempDir = Join-Path $dataRoot 'temp'
$locksDir = Join-Path $dataRoot 'locks'
$workspace = Join-Path $AppRoot 'workspace'

foreach ($dir in @($portableHome, $browserProfile, $browserCache, $gatewayData, $logsDir, $tempDir, $locksDir, $workspace)) {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $resolved = (Resolve-Path $dir).Path
    if (-not $resolved.StartsWith($AppRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "数据目录逃逸应用根目录：$resolved"
    }
    $probe = Join-Path $dir ".portable-write-test-$PID"
    [IO.File]::WriteAllText($probe, 'ok')
    Remove-Item -Force $probe
}

$octoBin = Join-Path $AppRoot "runtime\windows\$arch\octo.exe"
$guardBin = Join-Path $AppRoot "gateway\windows\$arch\ai-guard.exe"
if (-not (Test-Path $octoBin)) { throw "缺少 $octoBin" }
if (-not (Test-Path $guardBin)) { throw "缺少 $guardBin" }

$configDir = Join-Path $AppRoot 'config'
$configFile = Join-Path $portableHome '.octo\config.yml'
New-Item -ItemType Directory -Force -Path (Split-Path $configFile) | Out-Null
if (-not (Test-Path $configFile)) {
    $template = [IO.File]::ReadAllText((Join-Path $configDir 'octo-config.yml.template'))
    $workspaceYaml = $workspace.Replace('\', '/')
    $content = $template.Replace('__WORKSPACE__', $workspaceYaml)
    [IO.File]::WriteAllText($configFile, $content, [Text.UTF8Encoding]::new($false))
}

$upstreamUrl = $env:AI_GUARD_UPSTREAM_URL
$upstreamModel = $env:AI_GUARD_UPSTREAM_MODEL
$gatewayEnv = Join-Path $configDir 'gateway.env'
if (Test-Path $gatewayEnv) {
    foreach ($line in [IO.File]::ReadAllLines($gatewayEnv)) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#') -or -not $trimmed.Contains('=')) { continue }
        $pair = $trimmed.Split('=', 2)
        if ($pair[0] -eq 'AI_GUARD_UPSTREAM_URL' -and -not $upstreamUrl) { $upstreamUrl = $pair[1].Trim() }
        if ($pair[0] -eq 'AI_GUARD_UPSTREAM_MODEL' -and -not $upstreamModel) { $upstreamModel = $pair[1].Trim() }
    }
}
if (-not $upstreamUrl) { $upstreamUrl = 'https://api.openai.com' }
if (-not $upstreamModel) { $upstreamModel = Read-Host '上游模型名称' }

$upstreamKey = $env:AI_GUARD_UPSTREAM_API_KEY
if (-not $upstreamKey) {
    $secure = Read-Host '上游 API Key（不会写入磁盘）' -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { $upstreamKey = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}
if (-not $upstreamModel -or -not $upstreamKey) { throw '模型和 API Key 不能为空' }

$env:USERPROFILE = $portableHome
$env:HOME = $portableHome
$env:APPDATA = Join-Path $portableHome 'AppData\Roaming'
$env:LOCALAPPDATA = Join-Path $portableHome 'AppData\Local'
$env:TEMP = $tempDir
$env:TMP = $tempDir
$env:WEBVIEW2_USER_DATA_FOLDER = $browserProfile
$env:OCTO_PORTABLE_ROOT = $AppRoot
$env:OCTO_PORTABLE = '1'
$env:AI_GUARD_LISTEN = '127.0.0.1:18080'
$env:AI_GUARD_LOCAL_TOKEN = 'local-gateway-only'
$env:AI_GUARD_UPSTREAM_URL = $upstreamUrl
$env:AI_GUARD_UPSTREAM_MODEL = $upstreamModel
$env:AI_GUARD_UPSTREAM_API_KEY = $upstreamKey
$env:AI_GUARD_SYSTEM_PREFIX_FILE = Join-Path $configDir 'system-prefix.txt'
$env:AI_GUARD_SYSTEM_SUFFIX_FILE = Join-Path $configDir 'system-suffix.txt'
$env:AI_GUARD_ALLOWED_TOOLS_FILE = Join-Path $configDir 'tool-allowlist.txt'
$env:AI_GUARD_SENSITIVE_TERMS_FILE = Join-Path $configDir 'sensitive-terms.txt'
$upstreamKey = $null

function Test-Health([string]$Url) {
    try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 1 -Uri $Url | Out-Null; return $true }
    catch { return $false }
}
if (Test-Health 'http://127.0.0.1:18080/healthz') { throw '端口 18080 已有网关进程' }
if (Test-Health 'http://127.0.0.1:8088/api/version') { throw '端口 8088 已有 Octo 进程' }

$guard = $null
$octo = $null
$lockFile = Join-Path $locksDir 'portable.lock'
try {
    [IO.File]::WriteAllText($lockFile, "pid=$PID platform=windows arch=$arch started=$([DateTime]::UtcNow.ToString('o'))")
    $guard = Start-Process -FilePath $guardBin -PassThru -RedirectStandardOutput (Join-Path $logsDir 'ai-guard.log') -RedirectStandardError (Join-Path $logsDir 'ai-guard-error.log')
    for ($i = 0; $i -lt 50 -and -not (Test-Health 'http://127.0.0.1:18080/healthz'); $i++) {
        if ($guard.HasExited) { throw "AI 网关启动失败，请查看 $logsDir" }
        Start-Sleep -Milliseconds 100
    }
    if (-not (Test-Health 'http://127.0.0.1:18080/healthz')) { throw 'AI 网关健康检查超时' }

    $octo = Start-Process -FilePath $octoBin -ArgumentList @('serve', '--no-supervisor', '-addr', '127.0.0.1:8088') -PassThru -RedirectStandardOutput (Join-Path $logsDir 'octo.log') -RedirectStandardError (Join-Path $logsDir 'octo-error.log')
    for ($i = 0; $i -lt 100 -and -not (Test-Health 'http://127.0.0.1:8088/api/version'); $i++) {
        if ($octo.HasExited) { throw "Octo 启动失败，请查看 $logsDir" }
        Start-Sleep -Milliseconds 100
    }
    if (-not (Test-Health 'http://127.0.0.1:8088/api/version')) { throw 'Octo 健康检查超时' }

    $candidatePaths = [Collections.Generic.List[string]]::new()
    $candidatePaths.Add((Join-Path $AppRoot "browser\windows\$arch\chrome.exe"))
    if ($originalProgramFiles) {
        $candidatePaths.Add((Join-Path $originalProgramFiles 'Google\Chrome\Application\chrome.exe'))
        $candidatePaths.Add((Join-Path $originalProgramFiles 'Microsoft\Edge\Application\msedge.exe'))
    }
    if ($originalProgramFilesX86) {
        $candidatePaths.Add((Join-Path $originalProgramFilesX86 'Microsoft\Edge\Application\msedge.exe'))
    }
    if ($originalLocalAppData) {
        $candidatePaths.Add((Join-Path $originalLocalAppData 'Google\Chrome\Application\chrome.exe'))
    }
    $browserCandidates = @($candidatePaths | Where-Object { Test-Path $_ })
    if (-not $browserCandidates) { throw "未找到 Chrome/Edge；请安装浏览器或放入 $AppRoot\browser\windows\$arch" }
    $browser = $browserCandidates[0]
    $browserArgs = @(
        '--app=http://127.0.0.1:8088',
        "--user-data-dir=$browserProfile",
        "--disk-cache-dir=$browserCache",
        '--no-first-run', '--disable-sync', '--disable-background-networking',
        '--disable-component-update', '--disable-crash-reporter'
    )
    Write-Host 'Buding Box 已启动；关闭应用窗口后请等待安全退出提示。'
    Start-Process -FilePath $browser -ArgumentList $browserArgs -Wait
    Write-Host '应用已退出，可以安全弹出 U 盘。'
}
finally {
    if ($octo -and -not $octo.HasExited) { Stop-Process -Id $octo.Id -Force -ErrorAction SilentlyContinue }
    if ($guard -and -not $guard.HasExited) { Stop-Process -Id $guard.Id -Force -ErrorAction SilentlyContinue }
    Remove-Item -Force $lockFile -ErrorAction SilentlyContinue
}
