#!/usr/bin/env bash
set -euo pipefail

# 便携桌面测试入口：只使用 custom/portable 的 Vite alias 构建 UI，随后直接
# 编译上游桌面壳；不修改 Makefile 或任何上游源码。普通 make desktop 会重新
# 构建上游 UI，因此不能用于验证便携登录页。
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
web_dir="$repo_root/web"
desktop_dir="$repo_root/cmd/octo-desktop"
desktop_bin="$repo_root/octo-desktop-portable-test"
version="$(sed -n 's/^var Version = "\(.*\)"/\1/p' "$repo_root/internal/version/version.go")"
commit="$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || printf unknown)"
build_cache="$repo_root/dist/portable/.build-cache/go"
force_login="${PORTABLE_FORCE_LOGIN:-1}"
test_home="$repo_root/dist/portable/.desktop-test-home"

if curl -fsS --max-time 1 http://127.0.0.1:8088/api/version >/dev/null 2>&1; then
  printf '端口 8088 已被占用。请从托盘完全退出 Octo 后再运行本脚本。\n' >&2
  exit 1
fi

if [[ ! -d "$web_dir/node_modules" ]]; then
  (cd "$web_dir" && npm ci)
fi

(cd "$web_dir" && PORTABLE_FORCE_LOGIN="$force_login" ./node_modules/.bin/vite build --config ../custom/portable/web/vite.config.mts)
for marker in 'Buding Box，我帮你' '当前测试口令为 123456'; do
  rg -a -q "$marker" "$repo_root/internal/server/webdist/assets" || {
    printf '便携登录页构建校验失败：缺少 %s\n' "$marker" >&2
    exit 1
  }
done

mkdir -p "$build_cache"
mkdir -p "$test_home/.octo"
if [[ ! -f "$test_home/.octo/config.yml" ]]; then
  sed "s|__WORKSPACE__|$repo_root/workspace|g" \
    "$repo_root/custom/portable/config/octo-config.yml.template" > "$test_home/.octo/config.yml"
  chmod 600 "$test_home/.octo/config.yml"
fi
printf 'portable\n' > "$test_home/.octo/.onboard_attempted"
chmod 600 "$test_home/.octo/.onboard_attempted"
ldflags="-X github.com/open-octo/octo-agent/internal/version.Version=${version}-portable-test -X github.com/open-octo/octo-agent/internal/version.Commit=$commit"

case "$(uname -s)" in
  Darwin)
    (cd "$desktop_dir" && env GOCACHE="$build_cache" CGO_ENABLED=1 \
      CGO_CFLAGS='-mmacosx-version-min=11.0' \
      CGO_LDFLAGS='-Wl,-macos_version_min,11.0 -Wl,-no_warn_duplicate_libraries' \
      go build -ldflags="$ldflags" -o "$desktop_bin" .)
    ;;
  Linux)
    (cd "$desktop_dir" && env GOCACHE="$build_cache" CGO_ENABLED=1 \
      go build -ldflags="$ldflags" -o "$desktop_bin" .)
    ;;
  MINGW*|MSYS*|CYGWIN*)
    desktop_bin="$desktop_bin.exe"
    (cd "$desktop_dir" && env GOCACHE="$build_cache" CGO_ENABLED=1 \
      go build -ldflags="$ldflags" -o "$desktop_bin" .)
    ;;
  *)
    printf '当前系统不支持桌面测试构建。\n' >&2
    exit 1
    ;;
esac

printf '便携登录页校验通过，测试口令：123456，强制登录：%s\n' "$force_login"
exec env HOME="$test_home" USERPROFILE="$test_home" \
  XDG_CONFIG_HOME="$test_home/.config" XDG_DATA_HOME="$test_home/.local/share" \
  XDG_CACHE_HOME="$test_home/.cache" OCTO_ACCESS_KEY=123456 "$desktop_bin"
