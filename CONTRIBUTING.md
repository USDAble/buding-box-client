# Contributing

<!-- OCTO-FORK: concise contributor entry point for the fork's rules and local guards. -->

This is the short entry point for human contributors. The binding rules are
[开发规范](dev-docs-puddingbox/规范/开发规范.md), `.octorules`, and `CLAUDE.md`; do
not copy them into a PR description or a second rule file.

## First-time setup

Use Go 1.25+ and Node 22.22.2+ (`.nvmrc` pins the preferred Node release), then
enable the versioned hooks once:

```sh
make hooks-install
```

The commit hook runs `make quick-check`; the push hook runs `make gate`. Use
`git commit --no-verify` or `git push --no-verify` only to recover from a local
tooling outage, never to bypass a known failure.

## Change checklist

1. Branch from the current integration base; never commit directly to `main`.
2. Keep one concept per PR. Discuss data migration, security, account/billing,
   public API, compatibility, cross-client UX, or broad refactors before code.
3. Find the owning topic in `dev-docs-puddingbox/`. Update that document rather
   than creating a duplicate; update API contracts together with their consumer
   and provider. `make docs-ref-check` keeps maintained links and source
   references resolvable.
4. Add focused tests beside changed Go code; use `httptest`, never live APIs.
   Run the relevant command while iterating and let the hooks run the shared
   checks before commit/push.
5. Follow the three hard rules: product data through `internal/datapath`, visible
   brand data through `branding/brand.json`, and an `OCTO-FORK:` reason beside
   every fork change to an upstream file.

## Product cross-layer changes

For a product feature that crosses the Web UI, local product API, or central
platform, one working screen is not enough. Before review, update the owning
product document, [local API contract](dev-docs-puddingbox/本地API契约.md), and
[central-platform contract](dev-docs-puddingbox/中台接口契约.md) together. Make
the Go client, local runtime, `clienttest`, and `productstub` use the same wire
shape; do not add browser-only mock success or a production fallback fixture.

Use a fresh, dedicated `OCTO_DATA_ROOT` and the real desktop shell to verify the
user path. This catches stale catalog caches, missing route registration, and
window-token mistakes that normal browser development cannot see. Record the
focused automated checks and any required manual path in the PR description.

Commit messages and PR descriptions are English. A passing check is evidence,
not a substitute for documenting a user-visible behavior or its failure path.
