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

$dataRoot = Join-Path $AppRoot 'data\windows'
$portableHome = Join-Path $dataRoot 'application-home'
$browserProfile = Join-Path $dataRoot 'browser-profile'
$logsDir = Join-Path $dataRoot 'logs'
$tempDir = Join-Path $dataRoot 'temp'
$locksDir = Join-Path $dataRoot 'locks'
$workspace = Join-Path $AppRoot 'workspace'

foreach ($dir in @($portableHome, $browserProfile, $logsDir, $tempDir, $locksDir, $workspace)) {
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
$desktopBin = Join-Path $AppRoot "desktop\windows\$arch\buding-box-desktop.exe"
$desktopIcon = Join-Path $AppRoot "desktop\windows\$arch\icon.png"
if (-not (Test-Path $octoBin)) { throw "缺少 $octoBin" }
if (-not (Test-Path $guardBin)) { throw "缺少 $guardBin" }
if (-not (Test-Path $desktopBin)) { throw "缺少本平台原生桌面壳：$desktopBin；请在 Windows 本机重新执行 current 打包" }
if (-not (Test-Path $desktopIcon)) { throw "缺少本平台桌面图标：$desktopIcon" }

$configDir = Join-Path $AppRoot 'config'
$configFile = Join-Path $portableHome '.octo\config.yml'
New-Item -ItemType Directory -Force -Path (Split-Path $configFile) | Out-Null
if (-not (Test-Path $configFile)) {
    $template = [IO.File]::ReadAllText((Join-Path $configDir 'octo-config.yml.template'))
    $workspaceYaml = $workspace.Replace('\', '/')
    $content = $template.Replace('__WORKSPACE__', $workspaceYaml)
    [IO.File]::WriteAllText($configFile, $content, [Text.UTF8Encoding]::new($false))
}

# 便携版不显示上游首次运行向导。标记位放在 U 盘的 application-home，
# 只影响便携实例，不接触上游源码，也不污染宿主机的用户配置目录。
$onboardMarker = Join-Path (Split-Path $configFile) '.onboard_attempted'
if (-not (Test-Path $onboardMarker)) {
    [IO.File]::WriteAllText($onboardMarker, "portable`n", [Text.UTF8Encoding]::new($false))
}

# 临时测试口令与便携前端保持一致；不读取宿主机凭据，也不修改上游鉴权源码。
# 正式发布前必须恢复为随机密钥或接入独立身份服务。
$appAccessKey = '123456'

$upstreamUrl = $env:AI_GUARD_UPSTREAM_URL
$upstreamModel = if ($env:AI_GUARD_UPSTREAM_MODEL) { $env:AI_GUARD_UPSTREAM_MODEL } else { 'portable-approved-model' }
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
$upstreamKey = $env:AI_GUARD_UPSTREAM_API_KEY
# 启动阶段不读取、不询问上游 API Key；空值会被网关安全地保留为“不可调用上游”。
# 这样桌面可以无交互打开，后续配置凭据时再通过受控环境注入。
if (-not $upstreamKey) { $upstreamKey = '' }

$env:USERPROFILE = $portableHome
$env:HOME = $portableHome
$env:APPDATA = Join-Path $portableHome 'AppData\Roaming'
$env:LOCALAPPDATA = Join-Path $portableHome 'AppData\Local'
$env:TEMP = $tempDir
$env:TMP = $tempDir
$env:BUDING_BOX_WEBVIEW_DATA = $browserProfile
$env:BUDING_BOX_APP_ICON = $desktopIcon
$env:OCTO_ACCESS_KEY = $appAccessKey
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
if (Test-Health 'http://127.0.0.1:18082/api/version') { throw '端口 18082 已有 Octo 进程' }

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

    $octo = Start-Process -FilePath $octoBin -ArgumentList @('serve', '--no-supervisor', '-addr', '127.0.0.1:18082') -PassThru -RedirectStandardOutput (Join-Path $logsDir 'octo.log') -RedirectStandardError (Join-Path $logsDir 'octo-error.log')
    for ($i = 0; $i -lt 100 -and -not (Test-Health 'http://127.0.0.1:18082/api/version'); $i++) {
        if ($octo.HasExited) { throw "Octo 启动失败，请查看 $logsDir" }
        Start-Sleep -Milliseconds 100
    }
    if (-not (Test-Health 'http://127.0.0.1:18082/api/version')) { throw 'Octo 健康检查超时' }

    Write-Host 'Buding Box 已启动；关闭应用窗口后请等待安全退出提示。'
    Start-Process -FilePath $desktopBin -Wait
    Write-Host '应用已退出，可以安全弹出 U 盘。'
}
finally {
    if ($octo -and -not $octo.HasExited) { Stop-Process -Id $octo.Id -Force -ErrorAction SilentlyContinue }
    if ($guard -and -not $guard.HasExited) { Stop-Process -Id $guard.Id -Force -ErrorAction SilentlyContinue }
    Remove-Item -Force $lockFile -ErrorAction SilentlyContinue
}
