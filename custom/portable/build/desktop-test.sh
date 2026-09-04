#!/usr/bin/env bash
set -euo pipefail

# 便携桌面测试必须复用正式打包入口，避免测试上游完整桌面壳、发布自定义最小壳
# 所造成的窗口、图标和系统适配差异。所有产物与测试数据仍只写入 dist/portable。
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
version="$(sed -n 's/^var Version = "\(.*\)"/\1/p' "$repo_root/internal/version/version.go")"
[[ -n "$version" ]] || version=0.0.0
dist_root="$repo_root/dist/portable/buding-box-portable-$version"
force_login="${PORTABLE_FORCE_LOGIN:-1}"

PORTABLE_FORCE_LOGIN="$force_login" PORTABLE_PRESERVE_DATA=1 \
  bash "$repo_root/custom/portable/build/package.sh" current
printf '便携正式桌面壳构建完成，强制登录：%s\n' "$force_login"

case "$(uname -s)" in
  Darwin) exec bash "$dist_root/Start-macOS.command" ;;
  Linux) exec bash "$dist_root/Start-Linux.sh" ;;
  MINGW*|MSYS*|CYGWIN*) exec cmd.exe /c "$(cygpath -w "$dist_root/Start-Windows.cmd")" ;;
  *) printf '当前系统不支持桌面测试。\n' >&2; exit 1 ;;
esac
