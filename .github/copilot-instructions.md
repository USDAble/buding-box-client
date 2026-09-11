# Copilot instructions

**Read and obey `dev-docs-usdable/开发规范.md` before proposing or editing anything.** It is this fork's binding engineering spec. `.octorules` and `CLAUDE.md` are the upstream normative rules and have equal standing.

`scripts/norms-guard.mjs` (CI `norms-guard` job / `make norms-check` / packaging preflight) fails the build if this file or any other AI-tool entry point stops pointing at the spec.

## Three hard rules (CI-enforced; full text in `开发规范.md` §3.1–§3.3)

1. **Never resolve a data path yourself.** No `os.UserHomeDir()`, no `".octo"` literal — use `internal/datapath` only. Data root is `<exe dir>/data`, overridable only by `$OCTO_DATA_ROOT`.
2. **Never hardcode a brand string.** Interpolate `{brand}` / `{brandShort}` from `branding/brand.json`. Annotate the two exceptions (upstream "Octo" in the port-conflict message; `buding-*` ASCII identifiers).
3. **Mark every change to an upstream file** with `// OCTO-FORK: <why> — see <design doc>`.

Upstream merges use `merge`, never `rebase`. One feature = one PR = one squash commit, targeting `v1`.

Requirements, plans and per-PR design docs live under `dev-docs-usdable/需求/<批次>/` (current batch: `20260911/`); upstream architecture lives in `dev-docs/` (do not add downstream docs there).
