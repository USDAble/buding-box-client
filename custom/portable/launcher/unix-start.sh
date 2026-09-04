#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf '启动失败：%s\n' "$*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "需要 curl 进行本地健康检查"

launcher_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
app_root="$(cd "$launcher_dir/.." && pwd -P)"
case "$(uname -s)" in
  Darwin) platform=macos ;;
  Linux) platform=linux ;;
  *) fail "此启动器只支持 macOS 和 Linux" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "不支持的 CPU 架构：$(uname -m)" ;;
esac

manifest="$app_root/manifest.sha256"
[[ -f "$manifest" ]] || fail "缺少完整性清单：$manifest"
if command -v shasum >/dev/null 2>&1; then
  (cd "$app_root" && shasum -a 256 -c manifest.sha256 >/dev/null) || fail "应用文件完整性校验失败"
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$app_root" && sha256sum -c manifest.sha256 >/dev/null) || fail "应用文件完整性校验失败"
else
  fail "系统缺少 shasum/sha256sum，无法校验应用文件"
fi

data_root="$app_root/data/$platform"
portable_home="$data_root/application-home"
logs_dir="$data_root/logs"
temp_dir="$data_root/temp"
locks_dir="$data_root/locks"
workspace="$app_root/workspace"

for dir in "$portable_home" "$logs_dir" "$temp_dir" "$locks_dir" "$workspace"; do
  mkdir -p "$dir" || fail "无法创建 $dir"
  resolved="$(cd "$dir" && pwd -P)"
  case "$resolved/" in
    "$app_root"/*) ;;
    *) fail "数据目录逃逸应用根目录：$resolved" ;;
  esac
  probe="$dir/.portable-write-test-$$"
  : > "$probe" || fail "目录不可写：$dir"
  rm -f "$probe"
done

octo_bin="$app_root/runtime/$platform/$arch/octo"
guard_bin="$app_root/gateway/$platform/$arch/ai-guard"
desktop_root="$app_root/desktop/$platform/$arch"
if [[ "$platform" == macos ]]; then
  desktop_bin="$desktop_root/Buding Box.app/Contents/MacOS/buding-box-desktop"
  desktop_icon="$desktop_root/Buding Box.app/Contents/Resources/icon.png"
else
  desktop_bin="$desktop_root/buding-box-desktop"
  desktop_icon="$desktop_root/icon.png"
fi
[[ -f "$octo_bin" ]] || fail "缺少 $octo_bin"
[[ -f "$guard_bin" ]] || fail "缺少 $guard_bin"
[[ -f "$desktop_bin" ]] || fail "缺少本平台原生桌面壳：$desktop_bin；请在本平台重新执行 current 打包"
[[ -f "$desktop_icon" ]] || fail "缺少本平台桌面图标：$desktop_icon"

config_dir="$app_root/config"
template="$config_dir/octo-config.yml.template"
config_file="$portable_home/.octo/config.yml"
mkdir -p "$(dirname "$config_file")"
if [[ ! -f "$config_file" ]]; then
  escaped_workspace="${workspace//\\/\\\\}"
  escaped_workspace="${escaped_workspace//\"/\\\"}"
  sed "s|__WORKSPACE__|$escaped_workspace|g" "$template" > "$config_file"
  chmod 600 "$config_file"
fi

# 便携版不显示上游首次运行向导。标记位放在 U 盘的 application-home，
# 只影响便携实例，不接触上游源码，也不污染宿主机的 ~/.octo 数据。
onboard_marker="$portable_home/.octo/.onboard_attempted"
if [[ ! -f "$onboard_marker" ]]; then
  printf 'portable\n' > "$onboard_marker"
  chmod 600 "$onboard_marker"
fi

# 临时测试口令与便携前端保持一致；不读取宿主机凭据，也不修改上游鉴权源码。
# 正式发布前必须恢复为随机密钥或接入独立身份服务。
app_access_key=123456

upstream_url="${AI_GUARD_UPSTREAM_URL:-}"
upstream_model="${AI_GUARD_UPSTREAM_MODEL:-portable-approved-model}"
env_file="$config_dir/gateway.env"
if [[ -f "$env_file" ]]; then
  while IFS='=' read -r key value; do
    key="${key#export }"
    case "$key" in
      AI_GUARD_UPSTREAM_URL) [[ -n "$upstream_url" ]] || upstream_url="$value" ;;
      AI_GUARD_UPSTREAM_MODEL) [[ -n "$upstream_model" ]] || upstream_model="$value" ;;
    esac
  done < <(sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$env_file")
fi
upstream_url="${upstream_url:-https://api.openai.com}"
# 启动阶段不读取、不询问上游 API Key；空值会被网关安全地保留为“不可调用上游”。
# 这样桌面可以无交互打开，后续配置凭据时再通过受控环境注入。
upstream_key="${AI_GUARD_UPSTREAM_API_KEY:-}"

if curl -fsS --max-time 1 http://127.0.0.1:18080/healthz >/dev/null 2>&1; then
  fail "端口 18080 已有网关进程"
fi
if curl -fsS --max-time 1 http://127.0.0.1:18082/api/version >/dev/null 2>&1; then
  fail "端口 18082 已有 Octo 进程"
fi

export HOME="$portable_home"
export XDG_CONFIG_HOME="$portable_home/.config"
export XDG_DATA_HOME="$portable_home/.local/share"
export XDG_CACHE_HOME="$portable_home/.cache"
export TMPDIR="$temp_dir"
# CoreFoundation 和 Wails/WebKit 的本地数据根目录均固定到 U 盘。
export CFFIXED_USER_HOME="$portable_home"
export BUDING_BOX_APP_ICON="$desktop_icon"
export OCTO_ACCESS_KEY="$app_access_key"
export AI_GUARD_LISTEN=127.0.0.1:18080
export AI_GUARD_LOCAL_TOKEN=local-gateway-only
export AI_GUARD_UPSTREAM_URL="$upstream_url"
export AI_GUARD_UPSTREAM_MODEL="$upstream_model"
export AI_GUARD_UPSTREAM_API_KEY="$upstream_key"
export AI_GUARD_SYSTEM_PREFIX_FILE="$config_dir/system-prefix.txt"
export AI_GUARD_SYSTEM_SUFFIX_FILE="$config_dir/system-suffix.txt"
export AI_GUARD_ALLOWED_TOOLS_FILE="$config_dir/tool-allowlist.txt"
export AI_GUARD_SENSITIVE_TERMS_FILE="$config_dir/sensitive-terms.txt"
unset upstream_key

guard_pid=''
octo_pid=''
staging_dir=''
cleanup() {
  [[ -n "$octo_pid" ]] && kill "$octo_pid" 2>/dev/null || true
  [[ -n "$guard_pid" ]] && kill "$guard_pid" 2>/dev/null || true
  [[ -n "$octo_pid" ]] && wait "$octo_pid" 2>/dev/null || true
  [[ -n "$guard_pid" ]] && wait "$guard_pid" 2>/dev/null || true
  [[ -n "$staging_dir" && -d "$staging_dir" ]] && rm -rf "$staging_dir"
  rm -f "$locks_dir/portable.lock"
}
trap cleanup EXIT INT TERM

printf '%s\n' "pid=$$ platform=$platform arch=$arch started=$(date -u +%FT%TZ)" > "$locks_dir/portable.lock"

if [[ "$platform" == linux ]]; then
  host_tmp="${XDG_RUNTIME_DIR:-/tmp}"
  staging_dir="$(mktemp -d "$host_tmp/buding-box-runtime.XXXXXX")"
  cp "$octo_bin" "$staging_dir/octo"
  cp "$guard_bin" "$staging_dir/ai-guard"
  cp "$desktop_bin" "$staging_dir/buding-box-desktop"
  chmod 700 "$staging_dir/octo" "$staging_dir/ai-guard" "$staging_dir/buding-box-desktop"
  octo_bin="$staging_dir/octo"
  guard_bin="$staging_dir/ai-guard"
  desktop_bin="$staging_dir/buding-box-desktop"
else
  chmod u+x "$octo_bin" "$guard_bin" "$desktop_bin" 2>/dev/null || true
fi

"$guard_bin" >>"$logs_dir/ai-guard.log" 2>&1 &
guard_pid=$!
for _ in {1..50}; do
  curl -fsS --max-time 1 http://127.0.0.1:18080/healthz >/dev/null 2>&1 && break
  kill -0 "$guard_pid" 2>/dev/null || fail "AI 网关启动失败，请查看 $logs_dir/ai-guard.log"
  sleep 0.1
done
curl -fsS --max-time 1 http://127.0.0.1:18080/healthz >/dev/null || fail "AI 网关健康检查超时"

"$octo_bin" serve --no-supervisor -addr 127.0.0.1:18082 >>"$logs_dir/octo.log" 2>&1 &
octo_pid=$!
for _ in {1..100}; do
  curl -fsS --max-time 1 http://127.0.0.1:18082/api/version >/dev/null 2>&1 && break
  kill -0 "$octo_pid" 2>/dev/null || fail "Octo 启动失败，请查看 $logs_dir/octo.log"
  sleep 0.1
done
curl -fsS --max-time 1 http://127.0.0.1:18082/api/version >/dev/null || fail "Octo 健康检查超时"

printf 'Buding Box 已启动；关闭应用窗口后请等待安全退出提示。\n'
"$desktop_bin" >>"$logs_dir/desktop.log" 2>&1

printf '应用已退出，可以安全弹出 U 盘。\n'
