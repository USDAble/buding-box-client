# Buding Box Portable v1

This extension packages the existing upstream `octo serve` binary behind a
portable launcher and a local fail-closed AI gateway. It does not modify any
upstream source file.

## What v1 includes

- Windows, macOS and Linux launchers.
- Per-platform `HOME`/`USERPROFILE`, cache, temp, log and browser-profile paths.
- Chromium/Chrome/Edge app-mode window with its profile on the USB drive.
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

Edit `apps/buding-box/config/gateway.env` in the generated package:

```text
AI_GUARD_UPSTREAM_URL=https://api.openai.com
AI_GUARD_UPSTREAM_MODEL=your-approved-model
```

Do not store the real API key there. The launcher asks for it each time. You
may alternatively set `AI_GUARD_UPSTREAM_API_KEY` in the parent environment.
`config/gateway.env` and `config/sensitive-terms.txt` are the two mutable
configuration files excluded from the integrity manifest. Changing other
packaged files requires rebuilding the package.

- Windows: double-click `Start-Windows.cmd`.
- macOS: run `bash Start-macOS.command` the first time; a signed distribution
  can be double-clicked normally.
- Linux: run `bash Start-Linux.sh`.

The v1 package uses an installed Chrome, Edge or Chromium. To make the package
self-contained, place a managed Chromium runtime under the generated
`apps/buding-box/browser/<platform>/<arch>/` path expected by the launcher.

## Build

Current platform only:

```bash
bash custom/portable/build/package.sh current
```

All Go target binaries:

```bash
bash custom/portable/build/package.sh all
```

Reuse an already-built Web UI:

```bash
bash custom/portable/build/package.sh all --skip-web
```

The completed folder is written under `dist/portable/`. Copy the whole
versioned directory to the USB drive; do not copy only the executable.

The v1 manifest detects accidental or partial file changes but is not digitally
signed. Production releases should sign each platform binary and the manifest.
