# AGENTS.md

**This file is an entry point, not the spec.** Every AI coding tool that reads or edits this repository MUST read and obey, in this order:

1. **`dev-docs-usdable/开发规范.md`** — this fork's binding engineering norms (branching, the three hard rules, review, testing, DoD).
2. **`.octorules`** and **`CLAUDE.md`** — the upstream normative rules. They have **equal standing** with the fork spec, not reference-material status; `CLAUDE.md` is the fuller write-up and `.octorules` the short index.

`scripts/norms-guard.mjs` runs in CI (`norms-guard` job), locally (`make norms-check`) and at the start of every packaging run, and **fails the build** if any entry file (this one, `.cursor/rules/dev-norms.mdc`, `.github/copilot-instructions.md`, `CLAUDE.md`, `.octorules`) is missing or stops pointing at the spec.

## Three hard rules (CI-enforced; full text in `开发规范.md` §3.1–§3.3)

1. **Never resolve a data path yourself.** No `os.UserHomeDir()`, no `".octo"` literal — every product path goes through `internal/datapath`. The data root is `<exe dir>/data`, overridable only by `$OCTO_DATA_ROOT`. Genuine host-home access goes in `scripts/homedir-allowlist.txt` with a written reason.
2. **Never hardcode a brand string.** Interpolate `{brand}` / `{brandShort}` from `branding/brand.json`. Annotate the two exceptions: "Octo" in the port-conflict message names the *upstream* product; `BUDING-DEMO-0001` / `buding-*` model ids are fixed ASCII data keys.
3. **Mark every change to an upstream file** with `// OCTO-FORK: <why> — see <design doc>`. Prefer making an upstream feature unreachable over deleting it.

Upstream merges use `merge`, never `rebase`.

## Where things live

| Content | Location |
|---|---|
| This fork's norms and upstream-merge policy | `dev-docs-usdable/开发规范.md`, `dev-docs-usdable/上游合并策略.md` |
| This fork's requirements, plans, per-PR design docs | `dev-docs-usdable/需求/<batch>/` (current: `20260909/`) |
| Upstream architecture decisions | `dev-docs/` — **upstream directory, do not add downstream docs here** |

Before editing anything under `internal/server/` or another upstream file, read `开发规范.md` §3.4–§3.11 and the PR self-review template.
