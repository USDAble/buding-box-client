#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: custom/portable/build/package.sh [current|all] [--skip-web]

  current   Build the current OS/architecture (default).
  all       Cross-build Go binaries for Windows/macOS/Linux, amd64/arm64.
  --skip-web  Reuse internal/server/webdist from a previous web build.
USAGE
}

mode=current
skip_web=false
for arg in "$@"; do
  case "$arg" in
    current|all) mode="$arg" ;;
    --skip-web) skip_web=true ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
portable_src="$repo_root/custom/portable"
version="$(sed -n 's/^var Version = "\(.*\)"/\1/p' "$repo_root/internal/version/version.go")"
[[ -n "$version" ]] || version=0.0.0
commit="$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || printf unknown)"
release_name="buding-box-portable-$version"
dist_root="$repo_root/dist/portable/$release_name"
app_root="$dist_root/apps/buding-box"
build_cache="$repo_root/dist/portable/.build-cache"
mkdir -p "$build_cache"
export GOCACHE="$build_cache/go"
cd "$repo_root"

if [[ "$skip_web" == false ]]; then
  if [[ ! -d "$repo_root/web/node_modules" ]]; then
    (cd "$repo_root/web" && npm ci)
  fi
  (cd "$repo_root/web" && PORTABLE_FORCE_LOGIN=0 ./node_modules/.bin/vite build --config ../custom/portable/web/vite.config.mts)
fi
[[ -f "$repo_root/internal/server/webdist/index.html" ]] || {
  printf 'missing webdist; run without --skip-web\n' >&2
  exit 1
}
for marker in 'Buding Box，我帮你' '当前测试口令为 123456' 'buding_box_portable_logged_in'; do
  rg -a -q "$marker" "$repo_root/internal/server/webdist/assets" || {
    printf 'webdist is missing portable login marker: %s; run without --skip-web\n' "$marker" >&2
    exit 1
  }
done

go test ./custom/portable/gateway

# dist/ is generated output. Replacing only this versioned directory never
# touches source or user data.
rm -rf "$dist_root"
mkdir -p "$app_root/runtime" "$app_root/gateway" "$app_root/desktop" \
  "$app_root/config" "$app_root/launcher/windows" "$app_root/workspace"

cp "$portable_src/config/octo-config.yml.template" "$app_root/config/"
cp "$portable_src/config/gateway.env.example" "$app_root/config/gateway.env"
cp "$portable_src/config/system-prefix.txt" "$app_root/config/"
cp "$portable_src/config/system-suffix.txt" "$app_root/config/"
cp "$portable_src/config/tool-allowlist.txt" "$app_root/config/"
cp "$portable_src/config/sensitive-terms.txt" "$app_root/config/"
cp "$portable_src/launcher/unix-start.sh" "$app_root/launcher/"
cp "$portable_src/launcher/windows/start.cmd" "$app_root/launcher/windows/"
cp "$portable_src/launcher/windows/start.ps1" "$app_root/launcher/windows/"
cp "$portable_src/launcher/root-start-unix.sh" "$dist_root/Start-macOS.command"
cp "$portable_src/launcher/root-start-unix.sh" "$dist_root/Start-Linux.sh"
cp "$portable_src/launcher/root-start-windows.cmd" "$dist_root/Start-Windows.cmd"
cp "$portable_src/README.md" "$app_root/README.md"
printf '%s-portable (%s)\n' "$version" "$commit" > "$app_root/VERSION"
chmod +x "$dist_root/Start-macOS.command" "$dist_root/Start-Linux.sh" "$app_root/launcher/unix-start.sh"

current_target() {
  case "$(uname -s)" in
    Darwin) goos=darwin ;;
    Linux) goos=linux ;;
    MINGW*|MSYS*|CYGWIN*) goos=windows ;;
    *) printf 'unsupported build host\n' >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) goarch=amd64 ;;
    arm64|aarch64) goarch=arm64 ;;
    *) printf 'unsupported build architecture\n' >&2; exit 1 ;;
  esac
  printf '%s/%s\n' "$goos" "$goarch"
}

if [[ "$mode" == all ]]; then
  targets=(windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64)
else
  targets=("$(current_target)")
fi
host_target="$(current_target)"

rg_version=15.1.0
cleanup() {
  make -s -C "$repo_root" rg-embed-clean >/dev/null 2>&1 || true
}
trap cleanup EXIT

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  case "$goos" in
    darwin) platform=macos; extension='' ;;
    windows) platform=windows; extension='.exe' ;;
    linux) platform=linux; extension='' ;;
  esac
  runtime_dir="$app_root/runtime/$platform/$goarch"
  gateway_dir="$app_root/gateway/$platform/$goarch"
  desktop_dir="$app_root/desktop/$platform/$goarch"
  mkdir -p "$runtime_dir" "$gateway_dir" "$desktop_dir" "$app_root/data/$platform"

  printf 'building octo for %s/%s\n' "$goos" "$goarch"
  make -s -C "$repo_root" rg-embed-clean
  if [[ "$goos/$goarch" != windows/arm64 ]]; then
    make -s -C "$repo_root" rg-embed GOOS="$goos" GOARCH="$goarch"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -tags=embedrg \
      -ldflags="-s -w -X github.com/open-octo/octo-agent/internal/version.Version=$version-portable -X github.com/open-octo/octo-agent/internal/version.Commit=$commit -X github.com/open-octo/octo-agent/internal/tools/rgembed.version=$rg_version" \
      -o "$runtime_dir/octo$extension" ./cmd/octo
  else
    printf 'ripgrep is not embedded for windows/arm64 (no upstream binary)\n'
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -ldflags="-s -w -X github.com/open-octo/octo-agent/internal/version.Version=$version-portable -X github.com/open-octo/octo-agent/internal/version.Commit=$commit -X github.com/open-octo/octo-agent/internal/tools/rgembed.version=$rg_version" \
      -o "$runtime_dir/octo$extension" ./cmd/octo
  fi

  printf 'building ai-guard for %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -ldflags='-s -w' \
    -o "$gateway_dir/ai-guard$extension" ./custom/portable/gateway

  # Wails 使用各操作系统的原生 WebView 和 CGO，只能在目标系统或配置完整
  # 交叉工具链的构建机上可靠生成。current 包始终包含当前平台原生壳；all
  # 模式只额外生成当前构建机对应的壳，其他平台应在各自系统执行 current。
  if [[ "$goos/$goarch" == "$host_target" ]]; then
    printf 'building native desktop for %s/%s\n' "$goos" "$goarch"
    desktop_output="$desktop_dir/buding-box-desktop$extension"
    desktop_ldflags="-s -w"
    case "$goos" in
      darwin)
        (cd "$portable_src/desktop" && env CGO_ENABLED=1 \
          CGO_CFLAGS='-mmacosx-version-min=11.0' \
          CGO_LDFLAGS='-Wl,-macos_version_min,11.0 -Wl,-no_warn_duplicate_libraries' \
          go build -ldflags="$desktop_ldflags" -o "$desktop_output" .)
        ;;
      windows)
        (cd "$portable_src/desktop" && env CGO_ENABLED=1 \
          go build -ldflags="$desktop_ldflags -H windowsgui" -o "$desktop_output" .)
        ;;
      linux)
        (cd "$portable_src/desktop" && env CGO_ENABLED=1 \
          go build -ldflags="$desktop_ldflags" -o "$desktop_output" .)
        ;;
    esac
  fi

  if [[ "$goos" != windows ]]; then
    chmod +x "$runtime_dir/octo" "$gateway_dir/ai-guard"
    [[ ! -f "$desktop_dir/buding-box-desktop" ]] || chmod +x "$desktop_dir/buding-box-desktop"
  fi
done

(
  cd "$app_root"
  : > manifest.sha256
  while IFS= read -r file; do
    shasum -a 256 "$file" >> manifest.sha256
  done < <(LC_ALL=C find . -type f \
    ! -path './data/*' \
    ! -path './config/gateway.env' \
    ! -path './config/sensitive-terms.txt' \
    ! -name manifest.sha256 | LC_ALL=C sort)
)

printf '\nPortable package ready:\n  %s\n' "$dist_root"
printf 'Copy that whole directory to the USB drive.\n'
