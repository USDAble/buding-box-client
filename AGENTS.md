Generated from .octorules by scripts/sync-agents.mjs. Do not edit directly.

# AGENTS.md

**This file is an entry point, not the spec.** Every AI coding tool that reads or edits this repository MUST read and obey, in this order:

1. **`dev-docs-usdable/开发规范.md`** — this fork's binding engineering norms (branching, the three hard rules, review, testing, DoD).
2. **`.octorules`** and **`CLAUDE.md`** — the upstream normative rules, of **equal standing** with the fork spec (开发规范 §3.6). `CLAUDE.md` is the fuller write-up, `.octorules` the short index.

The full `.octorules` text is **inlined below verbatim**, so a tool that reads only this file still receives the upstream rules — including the three hard rules under "Fork rules". `dev-docs-usdable/开发规范.md` and `CLAUDE.md` stay pointers: both exceed the per-file instruction budget Codex imposes (`project_doc_max_bytes`, 32 KiB by default), so they cannot be inlined. Nothing here replaces them.

`scripts/norms-guard.mjs` and `scripts/sync-agents.mjs --check` (CI `norms-guard` / `agents-guard` jobs, `make norms-check` / `make agents-check`, and the packaging preflight) fail the build if this file is missing, stops pointing at the fork spec, or drifts from `.octorules`.

## Where things live

| Content | Location |
|---|---|
| This fork's norms and upstream-merge policy | `dev-docs-usdable/开发规范.md`, `dev-docs-usdable/上游合并策略.md` |
| This fork's requirements, plans, per-PR design docs | `dev-docs-usdable/需求/<批次>/` |
| Upstream architecture decisions | `dev-docs/` — **upstream directory, do not add downstream docs here** |

---

<!-- BEGIN inlined .octorules — edit .octorules and run `make agents` -->

# octo-agent Project Rules

The canonical project guidance for contributors and AI coding agents. Keep this short — substantive design context belongs in `dev-docs/`.

## Project

`octo-agent` is a Go AI agent CLI (Go 1.25+, single binary). Module path `github.com/open-octo/octo-agent`. Ships as CLI + embedded Web UI + IM bridges (see `octo serve`).

**This repository is a downstream fork** shipping the 布丁盒子 / Pudding Box portable desktop product. **The fork-specific rules in `dev-docs-usdable/开发规范.md` are binding, not reference material**, and `.octorules` + `CLAUDE.md` are the binding upstream norms of equal standing (开发规范 §3.6). Upstream merge policy: `dev-docs-usdable/上游合并策略.md`. `scripts/norms-guard.mjs` enforces that every AI-tool entry point (`AGENTS.md`, `.cursor/rules/dev-norms.mdc`, `.github/copilot-instructions.md`, `CLAUDE.md`, this file) points at that spec. The three hard rules under "Fork rules" below override anything here that conflicts.

## Commands

```bash
make build                                                  # ./octo (or set VERSION=0.x.y for releases)
make test                                                   # go test -race ./...
make vet                                                    # go vet ./...
make fmt-check                                              # gofmt -l . must print nothing
make fmt                                                    # gofmt -w .
make tidy                                                   # go mod tidy

go test ./internal/agent/                                   # single package
go test ./internal/provider/anthropic/ -run TestSendStream  # single test
go test -race -v ./internal/tools/                          # verbose race
```

## Layering

```
cmd/octo/          CLI entry, flag parsing, REPL, sessions
internal/app/      Provider-client construction + session bootstrap
internal/agent/    Agent loop, history, content blocks, Sender interfaces
internal/provider/ Provider interface + per-vendor implementations
internal/tools/    Concrete ToolExecutor implementations
internal/version/  Version constants overridable via -ldflags
```

Dependency direction is one-way: `provider → agent`, `tools → agent`, never the other way. `internal/app` is the single place that constructs provider clients and adapts them to `agent.Sender`; every entry point (`cmd/octo`, `internal/server`) reaches the LLM through it rather than importing `provider` directly.

## Adding capability

- **New provider** — implement `provider.Provider` (required) and optionally `provider.StreamingProvider`, `provider.ToolProvider`, `provider.ToolStreamingProvider`. Put it under `internal/provider/<name>/`. Each protocol's wire-format quirks are isolated inside the package; the agent layer must not learn about them.
- **New tool** — implement `agent.ToolExecutor` and `Definition() agent.ToolDefinition` returning the JSON Schema the LLM sees. Place it under `internal/tools/<name>.go`. Register it in `tools.DefaultRegistry` and add it to `tools.DefaultTools()` if it belongs in the default set.
- **New skill** — `<data root>/skills/<name>/SKILL.md` with the same frontmatter format Claude Code uses. The skill loader composes existing tools — adding a skill should not require new tool code.

## Fork rules

Enforced by CI. Full text and rationale in `dev-docs-usdable/开发规范.md` §3.

1. **Never resolve a data path yourself.** No `os.UserHomeDir()`, no `".octo"` literal — everything goes through `internal/datapath`. The data root is `<exe dir>/data`, overridable only by `$OCTO_DATA_ROOT`. `~/.octo` is gone from this fork **including the CLI**, which is what lets `datapath-guard` reject the `".octo"` literal with zero exceptions. Genuine host-home access goes in `scripts/homedir-allowlist.txt` with a written reason.
2. **Never hardcode a brand string.** Interpolate `{brand}` / `{brandShort}` from `branding/brand.json` (`brand-guard`). Annotate the two exceptions: "Octo" in the port-conflict message names the *upstream* product; `BUDING-DEMO-0001` and `buding-*` model ids are fixed ASCII data keys that do not follow the English brand name.
3. **Mark every change to an upstream file** with `// OCTO-FORK: <why> — see <design doc>`. Prefer making an upstream feature unreachable over deleting it; deletions cause delete-vs-modify conflicts git cannot auto-resolve.

Beyond those three, the fork spec adds five discipline rules (`开发规范.md` §3.4–§3.10) that every plan and PR is judged against. They are binding, not advice:

4. **Reuse first (§3.5).** Before creating a package, client, or HTTP path, run a capability search and write it into the plan: what you checked, why it cannot be reused (name the function/field), what the reused shape looks like. ~80% coverage means reuse and add the missing 20%. Reusing-or-not is a human-confirm item.
5. **Single sources of truth (§3.8).** Contracts, error codes, and DTO field names live in `中台交付包` §3.2 and the P0-01 §合同骨架. **Model display names come from the signed catalog `displayName`** and the frontend keeps no id→name table (debug/test fallback only). Data paths, brand strings, permission, and mode grouping each have exactly one owner. A derivative doc quotes a one-line conclusion + a link — never a copy of the reasoning or the table.
6. **Bounded degradation (§3.9).** Every fallback answers: fall back to what, is the user told, when does it recover. Never degrade silently to an unauthorized source, never treat degradation as authorization, fail closed on signature/version/audience/clock checks. **Never write a user-editable file at startup** (an idempotent first-run seed is the only exception — §3.9.1).
7. **Stop and ask (§3.7).** Reuse-avoidance, breaking an architectural convention, touching auth/credential/data-root/brand/billing/permission/routing, unconventional logic (an extra layer to dodge a rule, one concept stored twice, silent fallback), upstream core files, and using a capability outside its design intent all need explicit human confirmation recorded in the PR: which row, the facts, the choice, who confirmed.
8. **Write the scope (§3.10).** Every rule says which domain it governs — production vs developer, build-time vs run-time, local vs remote. A rule without a scope gets cited in both directions.

These are ratcheted by `norms-guard`, `datapath-guard`, `brand-guard`, `reuse-guard`, `server-diff-guard`, and `release-profile-guard` — all wired into `make *-check`, CI, and the packaging preflight.

Upstream merges use `merge`, never `rebase`, and land as their own PR with no functional changes riding along.

## Code style

- Go 1.25 syntax. `gofmt -w` is the formatter; `go vet ./...` must pass.
- Tests live next to code (`foo.go` + `foo_test.go`). Use `httptest.NewServer` for HTTP-mocked tests; do not hit live APIs in `go test ./...`.
- Comments in English. Prefer self-documenting names over comments. Only comment the **why**, never the **what**.
- No new third-party dependencies without justification in the PR description.

## Workflow

- **Branch off latest `v1`** before editing. Feature PRs target `v1` (the product integration branch); `main` is the upstream-tracking branch. Never commit on either directly.
- Push lands via PR only. Squash-and-merge is the project default; force-push only after explicit approval.
- Commit messages and PR descriptions in English.
- One feature per PR: one feature / characteristic = one PR = one squash commit. Mass mechanical changes (rename, move) can ride together but should be a single self-contained change set.

## Testing

- `make test` runs `go test -race ./...` — must be green before pushing.
- Integration tests against real provider APIs are run by hand with a real key, not in CI.

## Common pitfalls (from prior incidents)

- **`Accept-Encoding: gzip` on Bing's HTML search endpoint** returns a JS skeleton instead of the real results page — `web_search` must omit that header.
- **OpenAI streaming needs `stream_options.include_usage=true`.** DashScope (Bailian) and real OpenAI emit no usage chunk without it (streamed turn reports zero tokens); DeepSeek sends usage regardless either way.
- **Cache token semantics differ by protocol, not vendor.** Anthropic-protocol endpoints report `input_tokens` as the uncached remainder with `cache_read_input_tokens` separate; OpenAI-protocol endpoints report `prompt_tokens` as the whole input with `cached_tokens` a subset. `InputTokens`/`CacheReadTokens` must stay non-overlapping buckets — the openai adapter subtracts cached from prompt, the anthropic adapter needs no adjustment.
- **OpenAI tool-call arguments stream as JSON fragments** across multiple chunks, keyed by `tool_calls[i].index` — concatenate before parsing.
- **`finish_reason: "tool_calls"` (OpenAI) vs `stop_reason: "tool_use"` (Anthropic)** — normalize to `"tool_use"` at the provider adapter; the agent loop must never branch on the OpenAI spelling.

See `CLAUDE.md` for the full write-up of each incident.

## When in doubt

- Verify external claims (API endpoints, third-party SDK existence, dates) before committing them.
- If `go test ./...` fails because of an environment issue (missing key, blocked network), say so explicitly rather than commenting out the test.

## Where documentation lives

| Content | Location |
|---|---|
| Upstream architecture decisions, verified-fact dumps | `dev-docs/` — one Markdown file per topic |
| This fork's requirements, plans, per-PR design docs | `dev-docs-usdable/需求/<batch>/` |
| This fork's engineering norms and upstream-merge policy | `dev-docs-usdable/开发规范.md`, `dev-docs-usdable/上游合并策略.md` |

**Do not put downstream documents in `dev-docs/`** — it is an upstream directory, and anything added there conflicts on every merge.

Don't commit speculative or unverified claims — verify before writing.

<!-- END inlined .octorules -->
