<!-- Write this PR description in English. -->

## What

<!-- One or two sentences: what does this PR change? -->

## Why

<!-- The need this addresses, and who benefits. -->

## User-visible impact

<!-- Default behavior changes? New config? Breaking changes? "None" is a valid answer. -->

---

## Self-review

Please confirm before requesting review. See [CONTRIBUTING.md](../CONTRIBUTING.md) for details.

### 1. Architecture

- [ ] Smallest possible diff for the need
- [ ] No new configuration knobs (or justified below)
- [ ] No new dependencies (or justified below)
- [ ] Fits the existing design / naming / layering

### 2. Need

- [ ] Common need that benefits most users, **or** scope and side effects are explained below
- [ ] No unintended impact on other users' workflows or defaults

### 3. Code

- [ ] All tests pass
- [ ] Coverage does not drop; new code has new tests
- [ ] Commit messages and this PR are written in English
- [ ] This branch was created from the latest `main` and is currently up to date with it

### 4. Fork rules

<!-- OCTO-FORK: downstream section. Rules and rationale in dev-docs-usdable/开发规范.md §3 and §5. -->

- [ ] Design doc: links to the `P<n>` doc under `dev-docs-usdable/需求/<batch>/技术方案/`, and the implementation matches it (or the doc was updated in this PR)
- [ ] Data paths: no `os.UserHomeDir()`, no `".octo"` literal — everything goes through `internal/datapath` (`datapath-guard` green)
- [ ] Brand strings: product names interpolate `{brand}` / `{brandShort}`; no hardcoded literals (`brand-guard` green)
- [ ] Upstream files touched are marked `// OCTO-FORK: <why> — see <doc>`; nothing upstream was deleted where it could be made unreachable instead
- [ ] If this wraps `agent.Sender`: there is a test asserting the wrapper still satisfies `ToolStreamingSender`
- [ ] If this adds an HTTP route: stated whether it is product-gate exempt or protected (default is protected)
- [ ] If this adds user-visible text: both zh and en entries added, in the `product.*` block at the end of `i18n.ts`
- [ ] Manual acceptance steps from the design doc were run; results pasted under "Notes for reviewers"

### Bonus

- [ ] This PR was authored using Octo itself

---

## Notes for reviewers

<!-- Anything reviewers should focus on, trade-offs you considered, or follow-ups planned. -->
