#!/usr/bin/env bash
# Package the octo desktop shell into a macOS .app bundle around the Go-built
# binary. The web UI is embedded in the in-process server (go:embed webdist),
# so this only assembles the bundle — run `make web-build` first (the
# `desktop-app` Makefile target does). Needs the macOS toolchain (Xcode
# command-line tools). A .app (with a bundle identifier) is also what native
# notifications require at runtime.
#
# Usage: scripts/package-desktop-macos.sh [version]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MOD_DIR="$ROOT/cmd/octo-desktop"

# Refuse to build a shipped artifact the fork guards reject. The build below
# hardcodes product_production, but this also covers datapath/reuse drift, and
# keeps every packaging path (make target, CI job, direct invocation) aligned.
node "$ROOT/scripts/preflight.mjs"

# OCTO-FORK: 版本改走 internal/version（Makefile:34 明文禁止 `git describe`） — see dev-docs-usdable/需求/20260911/需求基线.md §5.6 V-103
# The version comes from internal/version/version.go, the single source the
# Makefile names (see its comment at line 34). `git describe` was used here and
# is deliberately avoided one directory up for a reason that applies doubly:
# this repo's tags are archive snapshots, so `git describe` yields
# `archive/base-20260911-245-g3b8413c7` — not a version, and its `/` killed the
# sed that fills Info.plist ("bad flag in substitute command: 'b'"), which failed
# every macOS package job at assembly (V-103).
BASE_VERSION="$(sed -n 's/^var Version = "\(.*\)"/\1/p' "$ROOT/internal/version/version.go")"
if [ -z "$BASE_VERSION" ]; then
	echo "cannot read the version out of internal/version/version.go" >&2
	exit 1
fi
# An explicit argument still wins (release builds: `VERSION=0.12.0 ...`), and a
# dev build keeps the Makefile's `-dev` suffix in the version the binary reports
# via `octo version` and the in-app update check.
VERSION="${1:-$BASE_VERSION-dev}"
VERSION="${VERSION#v}"
# Info.plist's two version fields are read by LaunchServices and notarization and
# take period-separated integers only, so they get the bare number: `-dev` is
# legal for the binary's own string but not for a bundle version.
PLIST_VERSION="${VERSION%%-*}"
if ! printf '%s' "$PLIST_VERSION" | grep -qE '^[0-9]+(\.[0-9]+){1,2}$'; then
	echo "refusing to package: '$PLIST_VERSION' (from VERSION='$VERSION') is not a bundle version" >&2
	exit 1
fi
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
APP="$ROOT/Octo.app"
CONTENTS="$APP/Contents"

# Build a universal (x86_64 + arm64) octo-desktop so Octo.app runs natively on
# both Intel and Apple Silicon. Each arch is compiled separately and lipo'd
# together — Wails links WKWebView via CGO, and the macOS SDK ships both arch
# slices, so CC="clang -arch <arch>" cross-compiles the C/ObjC side while GOARCH
# handles the Go side.
#
# ripgrep is go:embed'd per arch (the grep tool shells out to it): each slice
# must embed its OWN arch's rg, or grep breaks on the other arch. rg-embed is a
# file target keyed on GOARCH, so wipe the embedded binary between arches to
# force a re-download for the arch being built.
RG_EMBED="$ROOT/internal/tools/rgembed/binaries/rg"
# Inject Version/Commit so the in-app update check recognizes a real release
# build — without them internal/upgrade.Eligible sees an empty Commit and the
# app reports "up to date" forever.
LDFLAGS="-X github.com/open-octo/octo-agent/internal/version.Version=$VERSION -X github.com/open-octo/octo-agent/internal/version.Commit=$COMMIT"
slices=()
for arch in amd64 arm64; do
	case "$arch" in
		amd64) cc_arch=x86_64 ;;
		arm64) cc_arch=arm64 ;;
	esac
	echo "==> embedding ripgrep (darwin/$arch)"
	rm -f "$RG_EMBED"
	make -C "$ROOT" rg-embed GOOS=darwin GOARCH="$arch"

	echo "==> building octo-desktop (darwin/$arch)"
	out="$ROOT/octo-desktop-$arch"
	# Fixed, not derived from the build machine's own macOS version (a prior
	# version of this script used `sw_vers -productVersion` here, which baked
	# the CI runner's live OS version into the binary's LC_VERSION_MIN load
	# command — on a runner newer than a user's Mac, launching failed with
	# "You can't use this version of the application... with this version of
	# macOS", unrelated to the LSMinimumSystemVersion=12.0 in Info.plist).
	# 12.0 matches that Info.plist value, Go 1.25's own linker default and the
	# CLI's DARWIN_MIN_MACOS in .goreleaser.yaml, so it also fully eliminates
	# the SDK-vs-link-target warning this flag was added for in the first
	# place. The link flag is the clang-driver spelling (-mmacosx-version-min),
	# not -Wl,-macos_version_min, so the driver does not also derive a
	# host-based version and ld does not warn about two min versions.
	macos_ver="12.0"
	# OCTO-FORK: CGO_LDFLAGS drops -Wl,-no_warn_duplicate_libraries, which
	# upstream carries here. The flag is a warning-only suppression, and not
	# every accepted linker knows it — ld64-530 (Xcode 14.3.1) answers
	# "ld: unknown option: -no_warn_duplicate_libraries" and fails the link, so
	# dropping it costs the harmless duplicate-library note and nothing else.
	# The `product_production` tag below is likewise ours, not upstream's: it
	# selects the immutable runtime profile, and release-profile-guard fails
	# any command that ships an artifact without it — taking upstream's
	# `-tags embedrg` here would package a developer-profile .app.
	# See P2-启动与生命周期.md §9.
	( cd "$MOD_DIR" && \
		GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 CC="clang -arch $cc_arch" \
		CGO_CFLAGS="-mmacosx-version-min=$macos_ver" \
		CGO_LDFLAGS="-mmacosx-version-min=$macos_ver" \
		go build -tags 'embedrg product_production' -ldflags "$LDFLAGS" -o "$out" . )
	slices+=("$out")
done

echo "==> lipo -> universal octo-desktop"
lipo -create -output "$ROOT/octo-desktop" "${slices[@]}"
rm -f "${slices[@]}"

echo "==> assembling $APP (version $VERSION, bundle $PLIST_VERSION)"
rm -rf "$APP"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"
mv "$ROOT/octo-desktop" "$CONTENTS/MacOS/octo-desktop"
# `|` as the delimiter: a version cannot contain it, while the `/` in the string
# git describe used to produce here was read as the delimiter and aborted the
# build (V-103).
sed "s|__VERSION__|$PLIST_VERSION|g" "$MOD_DIR/build/darwin/Info.plist" > "$CONTENTS/Info.plist"

# Self-check the output, the way package-portable.mjs verifies its own: the two
# version fields are read by LaunchServices and the notary, so shipping a
# leftover placeholder or a non-version must fail here instead of at install
# time (V-103 is exactly this class of defect, found only in CI logs).
for key in CFBundleShortVersionString CFBundleVersion; do
	got="$(plutil -extract "$key" raw -o - "$CONTENTS/Info.plist" 2>/dev/null || true)"
	if [ "$got" != "$PLIST_VERSION" ]; then
		echo "Info.plist $key = '$got', want '$PLIST_VERSION'" >&2
		exit 1
	fi
done

# Make the bundle self-contained: embed the octo CLI (put on PATH by the
# installer) and uv (seeded into ~/.octo/bin on first launch by the app). Both
# optional — a plain `make desktop-app` without them still produces a runnable
# GUI. The release/installer sets OCTO_CLI and UV_BINARY.
if [ -n "${OCTO_CLI:-}" ] && [ -f "$OCTO_CLI" ]; then
	install -m 0755 "$OCTO_CLI" "$CONTENTS/Resources/octo"
	echo "    embedded octo CLI"
fi
if [ -n "${UV_BINARY:-}" ] && [ -f "$UV_BINARY" ]; then
	install -m 0755 "$UV_BINARY" "$CONTENTS/Resources/uv"
	echo "    embedded uv"
fi
if [ -f "$MOD_DIR/build/darwin/icon.icns" ]; then
	cp "$MOD_DIR/build/darwin/icon.icns" "$CONTENTS/Resources/icon.icns"
else
	echo "    (no build/darwin/icon.icns — bundle uses the default icon)"
fi

# Ad-hoc sign so the local bundle runs without a Developer ID. A real notarized
# signature is a release step, tracked with the .pkg installer effort.
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || \
	echo "    (codesign --sign - failed; the bundle still runs locally)"

echo "==> done: $APP"
