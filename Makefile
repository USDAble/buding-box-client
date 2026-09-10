# Octo — Go build & test orchestration (Go 1.22+)
#
# Usage:
#   make            same as `make test`
#   make build      build the octo binary at repo root (./octo)
#   make install    install to $GOPATH/bin
#   make test       go test -race ./...
#   make cover      generate coverage.out + coverage.html
#   make vet        go vet ./...
#   make fmt        gofmt -w on all .go files
#   make fmt-check  fail if anything would be reformatted
#   make tidy       go mod tidy
#   make clean      remove build artefacts
#   make brand      regenerate the branding files from branding/brand.json
#   make brand-check validate brand.json + fail if a generated copy is stale
#   make datapath-check  fail if product code reintroduces a ~/.octo path
#
# octo-eval — lightweight eval (manual; needs a model key, NOT in CI):
#   make eval-build           build the ./octo-eval tool
#   make eval-list            list the task suite
#   make eval EVAL_FLAGS=...  run the suite (pass --provider/--model/… via EVAL_FLAGS)
# See dev-docs/octo-eval.md.

GOTAGS ?=
GOFLAGS ?=

# Build tag that enables embedding the ripgrep binary. CI builds without
# this tag so go:embed does not require binaries/rg to be present.
RG_TAGS := embedrg

# Inject version + commit at build time so `octo version` reports a real SHA.
#
# Auto-detection via `git describe` is intentionally avoided because the repo
# still carries Ruby-era tags (v0.11.2, v0.11.2-final-ruby) that would pollute
# the reported version. The single source of truth for the version number is
# internal/version/version.go (what release bumps already edit); dev builds
# derive "<that>-dev" from it, so the two never drift. Release builds set
# VERSION explicitly:
#
#   VERSION=0.12.0 make build
#
BASE_VERSION := $(shell sed -n 's/^var Version = "\(.*\)"/\1/p' internal/version/version.go)
VERSION ?= $(BASE_VERSION)-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/open-octo/octo-agent/internal/version.Version=$(VERSION) \
           -X github.com/open-octo/octo-agent/internal/version.Commit=$(COMMIT) \
           -X github.com/open-octo/octo-agent/internal/tools/rgembed.version=$(RG_VERSION)

# Desktop builds must inject the same Version/Commit as the CLI: the in-app
# update check (internal/upgrade.Eligible) treats an empty Commit as "no release
# metadata" and reports "up to date" forever, so a desktop binary built without
# these never sees a new release. rgembed.version is omitted — the bare
# `desktop` target builds without -tags embedrg.
DESKTOP_LDFLAGS := -X github.com/open-octo/octo-agent/internal/version.Version=$(VERSION) \
                   -X github.com/open-octo/octo-agent/internal/version.Commit=$(COMMIT)

GOFILES := $(shell find . -name '*.go' -not -path './vendor/*' -not -path '*/_vendor/*')

RG_EMBED_DIR := internal/tools/rgembed/binaries
RG_EMBED_BIN := $(RG_EMBED_DIR)/rg

.PHONY: all build install test cover vet fmt fmt-check tidy clean \
        brand brand-check datapath-check release-profile-check \
        server-diff-check reuse-check preflight-check \
        eval-build eval-list eval \
        rg-embed rg-embed-clean \
        bundle-tools-windows bundle-tools-macos \
        web-build web-dev dev build-full desktop desktop-dev desktop-binary desktop-app desktop-appimage \
        desktop-portable desktop-portable-all portable-check \
        brand-assets brand-assets-check

all: test

# Build the Vite + Svelte web UI into internal/server/webdist/.
# Requires Node.js and npm installed.
web-build:
	cd web && npm install && npm run build

# Start both the Go server and Vite dev server simultaneously.
# Ctrl-C stops both. Access the hot-reload UI at http://localhost:5173.
dev:
	@cd web && npm install --prefer-offline --silent
	@trap 'kill 0' INT; \
	  go run ./cmd/octo serve & \
	  cd web && npm run dev & \
	  wait

# Start only the Vite dev server (assumes `octo serve` is running separately).
web-dev:
	cd web && npm run dev

# Download and embed ripgrep for the current GOOS/GOARCH before building.
build: web-build rg-embed
	go build $(GOFLAGS) -tags='$(GOTAGS) $(RG_TAGS)' -ldflags='$(LDFLAGS)' -o octo ./cmd/octo

build-full: build

# Build the native desktop shell (Wails v3). cmd/octo-desktop is a nested
# module, so its CGO + webview dependencies never touch the CLI's go.mod. Needs
# a platform webview toolchain (macOS: Xcode command-line tools; Windows:
# WebView2). Embeds the web UI first (the in-process server go:embeds webdist).
# Produces a bare binary; use `wails3 build` inside cmd/octo-desktop for a
# packaged .app / installer.
# Fixed at 11.0 (matches cmd/octo-desktop's Info.plist LSMinimumSystemVersion
# and Go's own linker default) rather than derived from the build machine's
# live macOS version — deriving it from `sw_vers` bakes whatever OS the
# builder happens to run into the binary's LC_VERSION_MIN, silently raising
# the real minimum macOS required to launch it.
DESKTOP_MACOS_VERSION ?= 11.0
# OCTO-FORK: CGO_LDFLAGS drops -Wl,-no_warn_duplicate_libraries — that flag is
# a warning-only suppression the Xcode 15 ld_prime linker no longer accepts
# (ld: unknown option). Dropping it only restores the harmless duplicate-library
# warning. See dev-docs-usdable/需求/2260906/技术方案/P2-启动与生命周期.md §9.
desktop: web-build
	cd cmd/octo-desktop && CGO_ENABLED=1 \
		CGO_CFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
		CGO_LDFLAGS="-Wl,-macos_version_min,$(DESKTOP_MACOS_VERSION)" \
		go build -ldflags='$(DESKTOP_LDFLAGS)' -o ../../octo-desktop .

# OCTO-FORK: desktop-binary is an explicit alias for the bare desktop binary
# build (the `desktop` target) — lets docs and scripts ask for "just the
# binary" without implying a packaged .app or an installer.
desktop-binary: desktop

# OCTO-FORK: desktop-dev runs the desktop shell against the Vite dev server so
# web/ edits hot-reload inside the real window instead of the embedded build.
# Start `make web-dev` in a second terminal first (Vite on :5173), then this
# target builds the shell and points the window at :5173. The window's
# in-process hub still owns 8088; Vite proxies /api and /ws back to it (see
# desktopWebviewURL in cmd/octo-desktop/main.go and web/vite.config.ts).
desktop-dev:
	cd cmd/octo-desktop && CGO_ENABLED=1 \
		CGO_CFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
		CGO_LDFLAGS="-Wl,-macos_version_min,$(DESKTOP_MACOS_VERSION)" \
		go build -ldflags='$(DESKTOP_LDFLAGS)' -o ../../octo-desktop-dev .
	OCTO_DESKTOP_DEV_URL="http://localhost:5173" ./octo-desktop-dev

# Package the desktop shell into a double-clickable macOS Octo.app bundle
# (embeds the web UI, ad-hoc signed for local use). Real Developer ID
# notarization is a release step. Windows packaging rides the Inno Setup
# installer track.
desktop-app: web-build
	bash scripts/package-desktop-macos.sh

# Package the desktop shell into a portable Linux AppImage (Linux only; needs
# the GTK4/WebKitGTK 6.0 dev packages + downloads appimagetool). GTK4/WebKitGTK
# are taken from the host — the AppRun launcher preflights them.
desktop-appimage: web-build
	bash scripts/package-desktop-linux.sh

# Portable Windows directory — the product's PRIMARY deliverable (需求 §5.1.1).
# One command builds the GUI exe (VERSIONINFO + icon injected), assembles
# PuddingBox/ (bundled tools + pre-filled data/ + bilingual usage note), runs
# the product self-check, and zips. The installer targets above still build but
# are no longer the main output. See
# dev-docs-usdable/需求/2260906/技术方案/P12-便携打包.md.
desktop-portable: web-build brand-check
	$(MAKE) rg-embed GOOS=windows GOARCH=amd64
	node scripts/package-portable.mjs

# One command for BOTH portable deliverables (P12 §3.7): the Windows PuddingBox/
# directory (cross-compiled — works from any host) and the macOS Octo.app bundle
# (native; needs macOS + Xcode CLT, so this target is for a mac host). CI splits
# these into per-OS jobs in portable.yml rather than running this target.
desktop-portable-all: web-build brand-check desktop-app desktop-portable

# Node unit tests for the packaging pipeline (self-check predicates, the
# zero-dependency ZIP writer, the PE reader) — same pattern as brand-check /
# datapath-check. Runs in the Portable workflow (portable.yml).
portable-check:
	node --test scripts/package-portable.test.mjs scripts/pe-info.test.mjs

install: web-build rg-embed
	go install $(GOFLAGS) -tags='$(GOTAGS) $(RG_TAGS)' -ldflags='$(LDFLAGS)' ./cmd/octo

test:
	go test -race $(GOFLAGS) -tags='$(GOTAGS)' ./...

# The production build excludes code behind the `product_production` tag
# (internal/productprofile, internal/provider/local). Untested tagged code
# rots: this target compiles and tests the shipped configuration so the
# production stub cannot silently drift from the developer implementation.
test-production:
	go build -tags product_production ./...
	go vet -tags product_production ./...
	go test -tags product_production $(GOFLAGS) ./...

cover:
	go test $(GOFLAGS) -tags='$(GOTAGS)' -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html in a browser to see line coverage."

vet:
	go vet ./...

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@unformatted=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt found unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

tidy:
	go mod tidy

clean:
	rm -f octo octo.exe octo-eval coverage.out coverage.html
	rm -rf dist/
	rm -f $(RG_EMBED_BIN) $(RG_EMBED_BIN).exe

# ── branding ─────────────────────────────────────────────────────────────────
# branding/brand.json is the single source of truth for product identity (names,
# publisher, identifiers, paths). Consumers that cannot import it — go:embed,
# the nested octo-relay module, Inno Setup — read generated copies committed
# alongside them, so a plain `git clone && make build` needs no Node step.
#
# Edit branding/brand.json, run `make brand`, commit both. `make brand-check` is
# what CI's brand-guard job runs; the scripts have zero npm dependencies, so it
# needs only a Node binary.
#
# `brand` also renders the raster master (branding/source/logo-mark.png — the
# single coloured PNG the designer delivers) into every shipped bitmap via
# cmd/generate-brand-assets — see 品牌升级方案 §5.2 / §5.8. The tray silhouette
# is derived from the master's alpha when no dedicated logo-mono.png exists.
# The tool still validates any .svg source (rejecting embedded <image>/base64
# or live <text>), so a fake-vector regression fails loudly rather than ship a
# degraded icon.
brand:
	node scripts/sync-branding.mjs
	go run ./cmd/generate-brand-assets

brand-check:
	node scripts/brand-schema.mjs
	node scripts/sync-branding.mjs --check
	node --test scripts/brand-schema.test.mjs scripts/sync-branding.test.mjs

# Asset-only steps, separated so the text checks (brand-check) stay independent
# of the design deliverable. Runs after every designer master lands in
# branding/source/logo-mark.png; CI guards drift via brand-assets-check.
brand-assets:
	go run ./cmd/generate-brand-assets

brand-assets-check:
	go run ./cmd/generate-brand-assets --check

# ── portable data root guard ──────────────────────────────────────────────────
# Rejects any reintroduction of the pre-fork ~/.octo data root: a ".octo"
# string literal, or an os.UserHomeDir() call outside scripts/homedir-allowlist.txt.
# CI's datapath-guard job runs the same script.
datapath-check:
	node scripts/datapath-guard.mjs
	node --test scripts/datapath-guard.test.mjs

# ── release profile guard ────────────────────────────────────────────────────
# Every build of cmd/octo-desktop that produces a shipped artifact must carry
# the product_production tag, or the package is a developer build with all the
# production rejections disabled (运行时Profile配置.md §2). CI's
# release-profile-guard job runs the same script.
release-profile-check:
	node scripts/release-profile-guard.mjs
	node --test scripts/release-profile-guard.test.mjs

# ── server diff guard ────────────────────────────────────────────────────────
# internal/server is an upstream tree this fork must keep mergeable. The guard
# is a ratchet: apiProduct (0 upstream, 161 here) and the per-file fork diff
# ceilings may only shrink. CI's server-diff-guard job runs the same script;
# it needs the upstream ref fetched (fetch-depth: 0).
server-diff-check:
	node scripts/server-diff-guard.mjs
	node --test scripts/server-diff-guard.test.mjs

# ── reuse guard ──────────────────────────────────────────────────────────────
# The 中台 gateway package must assemble an app.Sender, not grow a second
# SSE/HTTP client (开发规范 §3.5). CI's reuse-guard job runs the same script.
reuse-check:
	node scripts/reuse-guard.mjs
	node --test scripts/reuse-guard.test.mjs

# ── packaging preflight ──────────────────────────────────────────────────────
# The same check every packager runs at the start of a build (hard-fails on
# datapath/release-profile/reuse drift; warns on server drift when the upstream
# ref is not fetched). Exposed here so a developer can run it without packaging.
# See scripts/preflight.mjs.
preflight-check:
	node scripts/preflight.mjs

# ── ripgrep embed (build-time only) ──────────────────────────────────────────
# Downloads the matching rg release for GOOS/GOARCH, extracts the binary,
# and places it where go:embed will pick it up. No-op if already present.

RG_VERSION := 15.1.0

# Default to the host platform when GOOS/GOARCH are not set explicitly.
_RG_GOOS   := $(or $(GOOS),$(shell go env GOOS))
_RG_GOARCH := $(or $(GOARCH),$(shell go env GOARCH))

rg-embed: $(RG_EMBED_BIN)

$(RG_EMBED_BIN):
	@echo "Downloading ripgrep $(RG_VERSION) for $(_RG_GOOS)/$(_RG_GOARCH)..."
	@mkdir -p $(RG_EMBED_DIR)
	@bash -c ' \
		GOOS="$(_RG_GOOS)"; GOARCH="$(_RG_GOARCH)"; RG_VERSION="$(RG_VERSION)"; \
		case "$${GOOS}_$${GOARCH}" in \
			darwin_amd64)   asset="ripgrep-$${RG_VERSION}-x86_64-apple-darwin.tar.gz" ;; \
			darwin_arm64)   asset="ripgrep-$${RG_VERSION}-aarch64-apple-darwin.tar.gz" ;; \
			linux_amd64)    asset="ripgrep-$${RG_VERSION}-x86_64-unknown-linux-musl.tar.gz" ;; \
			linux_arm64)    asset="ripgrep-$${RG_VERSION}-aarch64-unknown-linux-gnu.tar.gz" ;; \
			windows_amd64)  asset="ripgrep-$${RG_VERSION}-x86_64-pc-windows-msvc.zip" ;; \
			*) echo "Unsupported platform: $${GOOS}/$${GOARCH} — rg embed skipped"; touch '"$(RG_EMBED_BIN)"'; exit 0 ;; \
		esac; \
		url="https://github.com/BurntSushi/ripgrep/releases/download/$${RG_VERSION}/$${asset}"; \
		if [ "$${GOOS}" = "windows" ]; then \
			curl -sL "$$url" -o /tmp/rg-embed.zip; \
			unzip -q -o /tmp/rg-embed.zip -d /tmp/rg-embed; \
			cp /tmp/rg-embed/ripgrep-$${RG_VERSION}-*/rg.exe $(RG_EMBED_BIN); \
			rm -rf /tmp/rg-embed.zip /tmp/rg-embed; \
		else \
			curl -sL "$$url" | tar -xzf - -C /tmp; \
			cp /tmp/ripgrep-$${RG_VERSION}-*/rg $(RG_EMBED_BIN); \
			rm -rf /tmp/ripgrep-$${RG_VERSION}-*; \
		fi; \
		echo "Embedded rg ready for $${GOOS}/$${GOARCH}" \
	'

rg-embed-clean:
	rm -f $(RG_EMBED_BIN) $(RG_EMBED_BIN).exe

# ── bundled tools (installer-only): uv ───────────────────────────────────────
# Fetches upstream release binaries for uv (astral-sh/uv) and stages them
# under dist/bundled-tools/<platform>/ so the Windows/macOS installers
# (packaging/windows/octo.iss, packaging/macos/build.sh) can bundle it — a
# fresh install then has `uv run` working with zero manual download, for the
# office-xlsx skill (issue #1054's P1 priority: the non-technical-user path).
#
# The generic bundled-dir mechanism (bundledBinDir/withBundledBinPath in
# internal/tools/sandbox.go, and the toolchain.go presence probe) is generic
# — it picks up whatever's in ~/.octo/bin, so a future bundle-tools-* target
# needs no code change, only a Makefile/packaging addition.
#
# Deliberately NOT go:embed'd into the octo binary itself: that would bloat
# every build on every platform (CLI-only Linux users included) with a binary
# they'll never use. Instead this target runs once per release. A plain
# `git clone && make build` / `go install` never runs it and never needs
# dist/bundled-tools to exist — that's why it isn't in git and isn't a
# dependency of the `build` target.
#
# release.yml's macos-installer job runs bundle-tools-macos directly (macOS
# runners have `curl`/`unzip`/`lipo` out of the box). The windows-installer
# job does NOT run bundle-tools-windows — windows-latest isn't guaranteed to
# have GNU Make, so that job fetches the same asset with native PowerShell
# instead (see release.yml) and its version pin must be kept in sync with
# UV_VERSION below by hand. bundle-tools-windows is still here for local
# testing (works from any host with curl/unzip, Windows included via
# WSL/Git-Bash) and to keep both platforms documented in one place.
#
# Version is pinned (like RG_VERSION above) rather than tracking "latest", so
# a release build is reproducible; bump deliberately.
UV_VERSION := 0.11.26

BUNDLE_TOOLS_DIR := dist/bundled-tools

# octo.iss only ever produces a windows/amd64 installer (ArchitecturesAllowed
# x64compatible), so only that one asset is needed.
bundle-tools-windows:
	@echo "Fetching uv $(UV_VERSION) for windows/amd64..."
	@mkdir -p $(BUNDLE_TOOLS_DIR)/windows-amd64
	@set -eu; work=$$(mktemp -d); trap 'rm -rf "$$work"' EXIT; \
		curl -sL -o "$$work/uv.zip" "https://github.com/astral-sh/uv/releases/download/$(UV_VERSION)/uv-x86_64-pc-windows-msvc.zip"; \
		unzip -q -o "$$work/uv.zip" -d "$$work/uv"; \
		cp "$$work/uv/uv.exe" $(BUNDLE_TOOLS_DIR)/windows-amd64/uv.exe; \
		chmod +x $(BUNDLE_TOOLS_DIR)/windows-amd64/uv.exe
	@echo "Staged $(BUNDLE_TOOLS_DIR)/windows-amd64/uv.exe"

# octo-setup.pkg ships one universal (amd64+arm64) binary via lipo, same as
# the octo binary itself (universal_binaries in .goreleaser.yaml) — so uv gets
# lipo'd into a universal binary here too, rather than shipping two separate
# per-arch copies in a single pkg.
bundle-tools-macos:
	@echo "Fetching uv $(UV_VERSION) for darwin (universal)..."
	@mkdir -p $(BUNDLE_TOOLS_DIR)/darwin-universal
	@set -eu; work=$$(mktemp -d); trap 'rm -rf "$$work"' EXIT; \
		curl -sL "https://github.com/astral-sh/uv/releases/download/$(UV_VERSION)/uv-aarch64-apple-darwin.tar.gz" | tar -xzf - -C "$$work"; \
		curl -sL "https://github.com/astral-sh/uv/releases/download/$(UV_VERSION)/uv-x86_64-apple-darwin.tar.gz" | tar -xzf - -C "$$work"; \
		lipo -create -output $(BUNDLE_TOOLS_DIR)/darwin-universal/uv \
			"$$work/uv-aarch64-apple-darwin/uv" "$$work/uv-x86_64-apple-darwin/uv"; \
		chmod +x $(BUNDLE_TOOLS_DIR)/darwin-universal/uv
	@echo "Staged $(BUNDLE_TOOLS_DIR)/darwin-universal/uv"

# ── octo-eval — lightweight eval (manual; see dev-docs/octo-eval.md) ─────────
# Each task under evals/tasks/ is a local fixture; no Docker, no clone. Calls a
# real model, so never run in CI. Pass provider/model/etc through EVAL_FLAGS,
# e.g. make eval EVAL_FLAGS='--provider openai --model deepseek-v4-flash --allow-net'.
EVAL_FLAGS ?=

eval-build:
	go build $(GOFLAGS) -o octo-eval ./cmd/octo-eval

eval-list: eval-build
	./octo-eval list

eval: eval-build build
	./octo-eval run --octo ./octo $(EVAL_FLAGS)
