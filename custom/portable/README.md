# Buding Box Portable v1

This extension packages the existing upstream `octo serve` binary behind a
portable launcher, a local fail-closed AI gateway, and a minimal native Wails
window. It does not modify any upstream source file.

## What v1 includes

- Windows, macOS and Linux launchers.
- Per-platform `HOME`/`USERPROFILE`, WebView data, cache, temp and log paths.
- Native Wails window using WKWebView, WebView2, or WebKitGTK according to the OS.
- A full-page portable login with the temporary test password `123456`.
- A startup loading transition that appears only when auth/config checks exceed
  180 ms, avoiding flashes on fast local starts.
- OpenAI-compatible local gateway on `127.0.0.1:18080`.
- System prompt injection, request-size/model/token limits, lexical injection
  blocking, tool-name allowlisting, tool-argument JSON validation, and output
  redaction for API keys, mainland-China mobile numbers and configured terms.
- Cross-build packaging for amd64 and arm64.
- A startup SHA-256 integrity check for immutable package files.

The upstream ripgrep release has no Windows ARM64 binary, so that one target is
built without embedded ripgrep. The application and gateway run normally, but
features that require the bundled `rg` search binary are unavailable on that
target until a trusted Windows ARM64 build is supplied.

## Current security boundary

This first version validates that tool arguments are valid JSON and that tool
names are allowed. Full validation against every tool's JSON Schema and Shell/
SQL AST analysis are the next security phase; do not enable write, terminal or
database tools in `config/tool-allowlist.txt` until that phase is implemented.

The initial allowlist is intentionally read-only.

## Configure and run

The generated package already contains a fixed portable policy model value, so
startup no longer asks for an approved model name and the desktop UI skips the
upstream first-run model setup page. If the upstream provider uses another
model identifier, edit `apps/buding-box/config/gateway.env` in the generated
package:

```text
AI_GUARD_UPSTREAM_URL=https://api.openai.com
AI_GUARD_UPSTREAM_MODEL=your-approved-model
```

The launcher does not ask for an API key at startup. Without
`AI_GUARD_UPSTREAM_API_KEY`, the desktop opens normally but AI requests fail
closed with a missing-credentials response. To enable model calls, inject
`AI_GUARD_UPSTREAM_API_KEY` from the parent environment; do not store the real
key in the USB package.
`config/gateway.env` and `config/sensitive-terms.txt` are the two mutable
configuration files excluded from the integrity manifest. Changing other
packaged files requires rebuilding the package.

The current development build uses the fixed password `123456`. A successful
login persists a small login-state flag in the USB-resident application data,
so later cold starts reuse the session. Wrong entries
are checked only in the portable page, remain on that page, and have no retry
limit. A successful entry seeds only the same-origin Octo session cookie. UI/API/WebSocket traffic
then goes through the local sidecar and remains protected by Octo's access-key
check without changing upstream authentication code.

The password is intentionally visible in the portable source and web bundle,
so this mode is for development only. Restore a random key or integrate an
identity service before production release.

This protects the normal portable UI entry on port `18080`. Octo's internal
port `18082` remains loopback-only and inherits upstream's same-host trust
model; protecting it from malicious local processes requires OS-level network
isolation or an upstream option to disable the loopback exemption.

- Windows: double-click `Start-Windows.cmd`.
- macOS: run `bash Start-macOS.command` the first time; a signed distribution
  can be double-clicked normally.
- Linux: run `bash Start-Linux.sh`.

The native shell loads only `http://127.0.0.1:18080`, so UI/API/WebSocket
traffic remains behind the portable sidecar. It does not open Chrome or the
system default browser. Windows requires the WebView2 runtime; Linux requires
WebKitGTK; macOS uses the system WKWebView.

## Build

Current platform only:

```bash
bash custom/portable/build/package.sh current
```

All Go target binaries:

```bash
bash custom/portable/build/package.sh all
```

Wails uses each target OS's native WebView and CGO. A complete native release
must run `package.sh current` on Windows, macOS, and Linux respectively. The
`all` mode still cross-builds the portable backend and gateway, but only builds
the native shell for the host OS/architecture.

Reuse an already-built Web UI:

```bash
bash custom/portable/build/package.sh all --skip-web
```

Run the native desktop shell with the portable login UI for development:

```bash
bash custom/portable/build/desktop-test.sh
```

Do not use `make desktop` for this check: its upstream `web-build` target
replaces the portable UI with the ordinary upstream Vite build.
The test build sets `PORTABLE_FORCE_LOGIN=1`, so it always shows the login page
even if the host WebView has a previous test login state.
To test persistence instead, run `PORTABLE_FORCE_LOGIN=0 bash
custom/portable/build/desktop-test.sh` after the first successful login.

The completed folder is written under `dist/portable/`. Copy the whole
versioned directory to the USB drive; do not copy only the executable.

The v1 manifest detects accidental or partial file changes but is not digitally
signed. Production releases should sign each platform binary and the manifest.
