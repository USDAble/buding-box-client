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
#   make quick-check fast local checks run by the commit hook
#   make gate       everything a change must pass before landing on v1 (see below)
#   make hooks-install enable the repository's commit and push hooks
#   make tidy       go mod tidy
#   make clean      remove build artefacts
#   make brand      regenerate the branding files from branding/brand.json
#   make brand-check fail if a brand literal is typed into source, or a generated copy is stale
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

.PHONY: all build install test test-production cover vet fmt fmt-check tidy clean \
        node-check docs-ref-check quick-check hooks-install gate web-gate \
        brand brand-check datapath-check norms-check agents agents-check \
        sensitive-norm-check \
        reuse-check server-diff-check release-profile-check release-config-check \
        preflight-check \
        eval-build eval-list eval \
        rg-embed rg-embed-clean \
        bundle-tools-windows bundle-tools-macos \
        web-build web-dev dev build-full desktop desktop-dev desktop-app desktop-appimage \
        desktop-portable desktop-portable-all portable-check

all: test

# Build the Vite + Svelte web UI into internal/server/webdist/.
# Requires Node.js and npm installed.
#
# OCTO-FORK: clears webdist first, except the tracked .gitkeep — see
# scripts/webdist-clean.mjs and V-34. Upstream only ever runs vite here, and
# vite is configured not to empty the directory (that config protects the
# .gitkeep), so a deleted asset used to stay on disk and get embedded.
web-build: node-check web-dist-clean
	cd web && npm install && npm run build

# Empty webdist of everything the build does not just produce. Named as its own
# target so it can be run and tested on its own (scripts/webdist-clean.test.mjs
# asserts web-build actually depends on it).
web-dist-clean:
	node scripts/webdist-clean.mjs internal/server/webdist

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
# Fixed at 12.0 (matches cmd/octo-desktop's Info.plist LSMinimumSystemVersion,
# Go 1.25's own linker default, and DARWIN_MIN_MACOS in .goreleaser.yaml for
# the CLI) rather than derived from the build machine's live macOS version —
# deriving it from `sw_vers` bakes whatever OS the builder happens to run into
# the binary's LC_BUILD_VERSION, silently raising the real minimum macOS
# required to launch it. Passed to the link step as -mmacosx-version-min (the
# clang-driver spelling) rather than -Wl,-macos_version_min, so the driver
# does not also derive a host-based version and ld does not warn about two.
#
# OCTO-FORK: CGO_LDFLAGS deliberately omits -Wl,-no_warn_duplicate_libraries,
# which upstream carries here. That flag only silences the benign duplicate
# -lobjc that Go + Wails produce, and not every Apple linker accepts it —
# ld64-530 (Xcode 14.3.1) answers "ld: unknown option:
# -no_warn_duplicate_libraries" and fails the link outright, which is a hard
# failure to trade for one benign warning. Upstream's driver spelling above
# already removes the two-min-versions warning this flag was originally paired
# with (ld: "passed two min versions"), so the omission costs only the
# duplicate-library note.
#
# The 12.0 is upstream's, adopted at the 2026-09-17 merge of f7ba0793: Info.plist
# had already auto-merged to 12.0, Go's own linker default measures 12.0 (otool
# on a locally linked binary), and .goreleaser.yaml pins the CLI at 12.0 — an
# 11.0 here would contradict all three at once.
#
# Measured 2026-09-17 (otool, LC_BUILD_VERSION) so the next reader does not have
# to re-derive it: a pure-Go link records 12.0 on its own, but this target is
# cgo — with no flag the desktop test binary records 11.0 and a bare cgo program
# records 13.0, because cgo's floor comes from clang, which defaults to the build
# host's macOS. The pair below is what makes the shipped floor 12.0 rather than
# whatever the builder happens to run, which is the whole point of pinning it.
# See the desktop compatibility decision
DESKTOP_MACOS_VERSION ?= 12.0
desktop: web-build
	cd cmd/octo-desktop && CGO_ENABLED=1 \
		CGO_CFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
		CGO_LDFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
		go build -ldflags='$(DESKTOP_LDFLAGS)' -o ../../octo-desktop .

# OCTO-FORK: desktop-dev runs the desktop shell against the Vite dev server so
# web/ edits hot-reload inside the real window instead of the embedded build.
# Start `make web-dev` in a second terminal first (Vite on :5173), then this
# target builds the shell and points the window at :5173 — it does NOT start
# Vite itself, so skipping that terminal gives a blank window. Deliberately not
# `make dev`: that one starts `go run ./cmd/octo serve`, which fights the
# window's in-process hub for 8088. The window's hub still owns 8088 and Vite
# proxies /api and /ws back to it (see desktopWebviewURL in
# cmd/octo-desktop/main.go and web/vite.config.ts), so shell=octo-desktop and
# the window token ride along unchanged. Only a developer Profile honours
# OCTO_DESKTOP_DEV_URL, so a production build cannot be pointed at a dev
# server. See the local development boundary
desktop-dev:
	cd cmd/octo-desktop && CGO_ENABLED=1 \
		CGO_CFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
		CGO_LDFLAGS="-mmacosx-version-min=$(DESKTOP_MACOS_VERSION)" \
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

# Portable Windows directory — the product's PRIMARY deliverable. One command
# builds the GUI exe (VERSIONINFO + icon injected), assembles PuddingBox/
# (bundled tools + pre-filled data/ + bilingual usage note), runs the product
# self-check, and zips. See 技术方案/P12-便携打包.md.
desktop-portable: web-build brand-check
	$(MAKE) rg-embed GOOS=windows GOARCH=amd64
	node scripts/package-portable.mjs

# One command for BOTH portable deliverables: the Windows PuddingBox/ directory
# (cross-compiled — works from any host) and the macOS .app bundle (native;
# needs macOS + Xcode CLT, so this target is for a mac host). CI splits these
# into per-OS jobs in portable.yml rather than running this target.
desktop-portable-all: web-build brand-check desktop-app desktop-portable

# Node unit tests for the packaging pipeline (self-check predicates, the
# zero-dependency ZIP writer, the PE reader) — same pattern as brand-check.
portable-check: node-check
	node --test scripts/package-portable.test.mjs scripts/pe-info.test.mjs scripts/webdist-clean.test.mjs scripts/preflight.test.mjs

install: web-build rg-embed
	go install $(GOFLAGS) -tags='$(GOTAGS) $(RG_TAGS)' -ldflags='$(LDFLAGS)' ./cmd/octo

test:
	go test -race $(GOFLAGS) -tags='$(GOTAGS)' ./...

# OCTO-FORK: 六条守卫的 Makefile 接线（TODO-02 / V-1） — see
# the guard rollout
#
# Test the build that is actually shipped: everything compiled with the
# product_production tag, which selects the production runtime profile and
# compiles the developer-only paths out. This is the configuration a released
# binary runs, so it is the one whose tests have to pass.
test-production:
	go build -tags product_production ./...
	go vet -tags product_production ./...
	go test -tags product_production $(GOFLAGS) ./...
	# cmd/octo-desktop is a nested module, so the lines above skip it — and that
	# module is where the shipped desktop binary is built from. Its
	# production-only tests are what prove the shipped build installs the host
	# port and stays fail-closed, so they run here too.
	cd cmd/octo-desktop && go test -tags product_production $(GOFLAGS) ./...

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
# needs only a Node binary. It has three legs, and all three are the same rule
# seen from different sides: brand.json is well-formed (brand-schema), every
# generated copy is current (sync-branding --check), and no product file has
# typed the copy in instead of interpolating it (brand-guard — 硬规则 2, see
# V-79 for why the third leg was missing until 2026-09-15).
brand:
	node scripts/sync-branding.mjs

brand-check: node-check
	node scripts/brand-schema.mjs
	node scripts/sync-branding.mjs --check
	node scripts/brand-guard.mjs
	node --test scripts/brand-schema.test.mjs scripts/sync-branding.test.mjs scripts/brand-guard.test.mjs

# ── portable data root guard ──────────────────────────────────────────────────
# Rejects any reintroduction of the pre-fork ~/.octo data root: a ".octo"
# string literal, or an os.UserHomeDir() call outside scripts/homedir-allowlist.txt.
# CI's datapath-guard job runs the same script.
datapath-check:
	node scripts/datapath-guard.mjs
	node --test scripts/datapath-guard.test.mjs

# ── AI-tool entry-point guards ────────────────────────────────────────────────
# Every AI coding tool reads a different file (.cursor/rules, CLAUDE.md,
# .github/copilot-instructions.md, AGENTS.md, .octorules). norms-check asserts
# each one still points at the fork engineering norms; agents-check asserts the
# generated AGENTS.md matches the inlined .octorules. CI runs both (norms-guard /
# agents-guard jobs).
norms-check:
	node scripts/norms-guard.mjs
	node --test scripts/norms-guard.test.mjs

agents:
	node scripts/sync-agents.mjs

agents-check:
	node scripts/sync-agents.mjs --check
	node --test scripts/sync-agents.test.mjs

# ── fork-marker guard ────────────────────────────────────────────────────────
# Hard rule 3 (开发规范) requires `OCTO-FORK:` on every change to an
# upstream file, and that marker list is the inventory an upstream merge is done
# from. The guard anchors on a marker LINE rather than the bare token (prose
# *about* the rule lives in .octorules and CLAUDE.md, which is how a 79%-missing
# tree looked compliant), and it fails when it examined zero files. It needs an
# upstream ref: locally that is origin/main with a fallback to main; CI's
# fork-marker-guard job fetches main explicitly, because the default checkout
# does not. Text formats that cannot hold a comment are named, with reasons, in
# scripts/fork-marker-allowlist.txt.
marker-check:
	node scripts/fork-marker-guard.mjs
	node --test scripts/fork-marker-guard.test.mjs

# Guards requirement D6 / G4 (需求基线.md): the web bundle carries a second copy
# of the normalization symbol table for the dictionary page's pre-check, and
# nothing compared it against the Go owner until D-010 was registered. Order is
# deliberately not asserted (no side observes it) and neither is the rest of the
# algorithm — see the script header for what is and is not covered.
sensitive-norm-check:
	node scripts/sensitive-norm-guard.mjs
	node --test scripts/sensitive-norm-guard.test.mjs

# ── remaining fork guards (TODO-02 / V-1) ────────────────────────────────────
# OCTO-FORK: 四条守卫的 Makefile 接线（此前只有 CI 与本文件的注释声称它们在跑） — see
# the guard rollout
#
# These four scripts already existed on v1 with their unit tests, and
# scripts/preflight.mjs already ran them at packaging time — so packaging was
# protected while committing was not. They are wired here so `make *-check` and
# CI enforce the same rules at the point a change is made, which is where V-1's
# "the rule exists but nothing asserts it" is actually fixed.
#
# reuse-guard exists because the 中台 gateway assembles an app.Sender over
# internal/provider instead of speaking HTTP/SSE itself. The scan is a no-op
# until internal/productclient/gateway exists.
reuse-check:
	node scripts/reuse-guard.mjs
	node --test scripts/reuse-guard.test.mjs

# server-diff-guard is a ratchet: it records today's fork debt (apiProduct call
# sites, per-file diff ceilings, product files parked in the upstream tree) as a
# ceiling that may only shrink. It compares against the upstream-tracking
# branch (origin/main or main), so one must be fetched — locally that means a
# recent `git fetch origin main`.
server-diff-check:
	node scripts/server-diff-guard.mjs
	node --test scripts/server-diff-guard.test.mjs

# release-profile-guard is what stops a release build that forgot
# `-tags product_production`: the default profile branch is the developer one,
# so a pipeline that forgets the tag still compiles, still passes every test and
# still produces a working binary — it just ships a developer package where
# OCTO_DESKTOP_DEV_URL, environment provider keys and OCTO_DATA_ROOT all apply.
release-profile-check:
	node scripts/release-profile-guard.mjs
	node --test scripts/release-profile-guard.test.mjs

# release-config-guard checks the *content* of the embedded production profile:
# a release that still carries the `.invalid` placeholder hosts validates
# perfectly and then fails every control-plane call at runtime. Advisory —
# packaging an unconfigured build is legitimate while Q1 is open.
release-config-check:
	node scripts/release-config-guard.mjs
	node --test scripts/release-config-guard.test.mjs

# ── local developer entry points ─────────────────────────────────────────────
# Node is checked before npm/Vite so an old system Node produces one actionable
# diagnostic instead of a package-specific syntax error.
node-check:
	node scripts/node-version-guard.mjs
	node --test scripts/node-version-guard.test.mjs

# Keeps design references navigable without scanning vendored/upstream dev-docs.
docs-ref-check: node-check
	node scripts/docs-ref-guard.mjs
	node --test scripts/docs-ref-guard.test.mjs

# Commit-time checks are intentionally quick and deterministic. Full Go race,
# package and web checks run in gate, which the push hook invokes.
quick-check: node-check fmt-check docs-ref-check norms-check agents-check \
	brand-check datapath-check marker-check sensitive-norm-check reuse-check \
	server-diff-check release-profile-check
	@echo "quick-check passed: format + documentation + fork guards."

# Hooks live in the repository so their policy is reviewable. Git does not
# activate a versioned hooks directory automatically; each clone opts in once.
hooks-install:
	git config core.hooksPath .githooks
	@echo "Git hooks installed from .githooks (commit: quick-check; push: gate)."

# gate is the single command a change must pass before it lands on v1. It is
# the current implementation plan's local gate, which until now
# was a list a human had to remember and re-issue by hand:
#
#   make test + make fmt-check + make vet + every *-check
#   + cd web && npm run build && npm test + svelte-check
#
# Why it is a target rather than a paragraph: the list was skipped in practice,
# and the docs record what that cost. V-62 is the headline — until 2026-09-14 CI
# did not run on v1 at all, so the local gate WAS the only gate on the branch
# everything ships from, and V-61 is the defect that landed in that blind spot
# (web tests were green in the worktree while the committed tree did not build).
# A remembered list cannot be invoked by name; a target can, and it can be made
# to match what CI runs.
#
# Scope note (§3.10): this gates the tree you are standing on, including
# uncommitted edits. `V-61` was exactly a worktree-vs-committed-tree divergence,
# so if you are about to land a merge, commit first and run it on the commit.
gate: node-check fmt-check vet test portable-check \
      norms-check agents-check brand-check datapath-check marker-check \
      sensitive-norm-check reuse-check \
      server-diff-check release-profile-check release-config-check \
      web-gate
	@echo ""
	@echo "gate passed: fmt + vet + go test -race + 12 guards + web build/test/check."

# The web half of `gate`. Kept separate so it can be run on its own (it is the
# slowest half and the one that needs Node, not Go).
#
# Run svelte-check from `web/`, never from the repo root: from the root it picks
# up no tsconfig for the Svelte sources and reports a wall of false errors (107
# on 2026-09-15), which reads as "the environment is broken" and gets waved
# away. From web/ the same tree is 0 errors, 0 warnings. That difference is why
# this target exists instead of a remembered command.
web-gate: web-build
	cd web && npm test
	cd web && npx svelte-check --threshold error

# preflight-check is the packaging-time tier: a shipped artifact cannot be
# produced from a tree the guards would reject. CI already runs them on a
# commit; packaging is a later, different act — it can start from a dirty tree,
# a stale checkout, or a machine that skipped CI. HARD checks fail the build;
# the release-config and server-diff checks only warn.
preflight-check:
	node scripts/preflight.mjs

# ── ripgrep embed (build-time only) ──────────────────────────────────────────
# Downloads the matching rg release for GOOS/GOARCH, extracts the binary, and
# places it where go:embed will pick it up.
#
# OCTO-FORK: the target platform joins the up-to-date judgement, and a staged
# payload is verified before it is trusted — see V-108 in
# the product baseline. Upstream's `rg-embed: $(RG_EMBED_BIN)`
# was a pure file target: the payload path is the same for every platform, so
# the *second* build for a different platform found the file present, skipped
# the download, and embedded the first platform's binary. `make build` on a mac
# host stages a Mach-O; the `make desktop-portable` that follows cross-compiles
# a windows/amd64 exe around that macOS rg. Which platform you got depended only
# on which build ran first in the checkout, and the exe failed at runtime.
#
# Three changes fix it. The first is the bug; the other two are what keep it
# from coming back through a door the first one does not cover:
#
#   1. `.rg-stamp` records `<RG_VERSION> <GOOS>/<GOARCH>`, so a cached payload is
#      reused only for the target it was staged for. This is also why the
#      download is keyed on the version — bumping RG_VERSION invalidates it.
#      File presence is deliberately *not* part of the judgement any more; it is
#      the whole cause here.
#
#      The cache-hit path re-runs the platform check on the bytes it is about to
#      vouch for. A stamp is a claim, and a claim is only as good as what it
#      claims about: this way the up-to-date verdict always ends at bytes that
#      were read, never at a note that says they were fine. (Cheap enough not to
#      matter — `make build` already pays for a Vite build.)
#
#   2. `set -euo pipefail`, `curl -f`, and no `touch` on the failure paths:
#      upstream ran `curl -sL` and printed "Embedded rg ready" unconditionally,
#      so a 404 or a failed `cp` left an empty or absent payload and still
#      reported success. `pipefail` is not decoration: `curl … | tar -xzf -` is
#      exit-code-blind on its left half, and bsdtar accepts an empty stream as a
#      valid archive, so without it a connection that died before the first byte
#      looks like a successful extraction. The stamp is written only after the
#      bytes are verified, so a failure cannot be mistaken for a valid cache on
#      the next run.
#
#   3. `node scripts/package-portable.mjs --check-payload-platform` asks the
#      self-check's rule instead of restating it as magic numbers in shell
#      (§3.8: one definition of "these bytes can run there"). It runs right
#      after a download, which turns a wrong asset mapping below into a failed
#      build rather than an `rg` the target cannot exec. `make build` and
#      `make install` already reach node through `web-build`, so this adds no
#      dependency to either; a bare `make rg-embed` does gain one, and fails
#      loudly when node is absent rather than staging unverified bytes.
#
# A platform ripgrep does not publish for stages an empty payload — `go:embed`
# needs the file to exist — and the stamp records that platform too, so the
# empty file is never mistaken for a real binary by a later supported build.
#
# `windows_arm64` is the sixth mapping and is not in upstream's case: upstream
# let it fall through to the empty-payload branch, so `make rg-embed
# GOOS=windows GOARCH=arm64` produced a bundle whose grep tool had no `rg` while
# the CI leg for the same target (desktop.yml's `windows-arm64`, which runs this
# fork's PowerShell equivalent) got a real one. The asset name follows the
# convention that script already asserts for arm64.

RG_VERSION := 15.1.0

# Default to the host platform when GOOS/GOARCH are not set explicitly.
_RG_GOOS   := $(or $(GOOS),$(shell go env GOOS))
_RG_GOARCH := $(or $(GOARCH),$(shell go env GOARCH))

# Records what the staged payload is, so the next run can tell a cache that
# matches the target from one staged for another platform.
RG_EMBED_STAMP := $(RG_EMBED_DIR)/.rg-stamp

rg-embed:
	@bash -c 'set -euo pipefail; \
		GOOS="$(_RG_GOOS)"; GOARCH="$(_RG_GOARCH)"; RG_VERSION="$(RG_VERSION)"; \
		want="$${RG_VERSION} $${GOOS}/$${GOARCH}"; \
		if [ -s "$(RG_EMBED_BIN)" ] \
		   && [ -f "$(RG_EMBED_STAMP)" ] \
		   && [ "$$(cat "$(RG_EMBED_STAMP)")" = "$$want" ] \
		   && node scripts/package-portable.mjs --check-payload-platform "$(RG_EMBED_BIN)" "$${GOOS}" >/dev/null 2>&1; then \
			echo "rg $${RG_VERSION} for $${GOOS}/$${GOARCH} already staged"; \
			exit 0; \
		fi; \
		mkdir -p "$(RG_EMBED_DIR)"; \
		case "$${GOOS}_$${GOARCH}" in \
			darwin_amd64)   asset="ripgrep-$${RG_VERSION}-x86_64-apple-darwin.tar.gz" ;; \
			darwin_arm64)   asset="ripgrep-$${RG_VERSION}-aarch64-apple-darwin.tar.gz" ;; \
			linux_amd64)    asset="ripgrep-$${RG_VERSION}-x86_64-unknown-linux-musl.tar.gz" ;; \
			linux_arm64)    asset="ripgrep-$${RG_VERSION}-aarch64-unknown-linux-gnu.tar.gz" ;; \
			windows_amd64)  asset="ripgrep-$${RG_VERSION}-x86_64-pc-windows-msvc.zip" ;; \
			windows_arm64)  asset="ripgrep-$${RG_VERSION}-aarch64-pc-windows-msvc.zip" ;; \
			*) \
				echo "rg-embed: no ripgrep release for $${GOOS}/$${GOARCH}; staging an empty payload" >&2; \
				: > "$(RG_EMBED_BIN)"; \
				printf "%s\n" "$$want" > "$(RG_EMBED_STAMP)"; \
				exit 0 ;; \
		esac; \
		echo "Staging ripgrep $${RG_VERSION} for $${GOOS}/$${GOARCH}..."; \
		url="https://github.com/BurntSushi/ripgrep/releases/download/$${RG_VERSION}/$${asset}"; \
		work="$$(mktemp -d)"; trap "rm -rf \"$$work\"" EXIT; \
		case "$${asset##*.}" in \
			zip) curl -fsSL "$$url" -o "$$work/rg.zip"; \
			     unzip -q "$$work/rg.zip" -d "$$work/x"; \
			     cp "$$work"/x/ripgrep-$${RG_VERSION}-*/rg.exe "$(RG_EMBED_BIN)" ;; \
			gz)  curl -fsSL "$$url" | tar -xzf - -C "$$work"; \
			     cp "$$work"/ripgrep-$${RG_VERSION}-*/rg "$(RG_EMBED_BIN)" ;; \
		esac; \
		chmod +x "$(RG_EMBED_BIN)"; \
		node scripts/package-portable.mjs --check-payload-platform "$(RG_EMBED_BIN)" "$${GOOS}"; \
		printf "%s\n" "$$want" > "$(RG_EMBED_STAMP)"; \
		echo "Embedded rg $${RG_VERSION} for $${GOOS}/$${GOARCH}" \
	'

# OCTO-FORK: also drops the stamp, so a clean is a clean — see V-108.
rg-embed-clean:
	rm -f $(RG_EMBED_BIN) $(RG_EMBED_BIN).exe $(RG_EMBED_STAMP)

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
