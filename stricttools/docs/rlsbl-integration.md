+++
title = "rlsbl Integration"
description = "How selfdoc and rlsbl work together: auto-commit preference chain, changelog detection, docs checks during release, and post-release deploy."
nav_group = "Guides"
nav_order = 11
+++

# rlsbl Integration

selfdoc and rlsbl have a bidirectional relationship. selfdoc is aware of rlsbl's commit tooling and changelog conventions, while rlsbl can run selfdoc checks during release and trigger builds and deploys post-release. If both tools are present in a project, they cooperate automatically.

## selfdoc Side

### Auto-commit preference chain

When selfdoc writes files such as hash updates, generated pages, or root files, it auto-commits them using the best available commit tool on the system PATH. This 3-level preference chain ensures selfdoc integrates cleanly with concurrency-safe tools when available and falls back gracefully otherwise:

1. **rlsbl** -- if `rlsbl` is on `PATH`, use `rlsbl commit` (concurrency-safe, changelog-aware).
2. **safegit** -- if `safegit` is on `PATH`, use `safegit commit` (concurrency-safe).
3. **git** -- fall back to plain `git add` + `git commit`.

This means selfdoc plays nicely with rlsbl-managed repos without any configuration. The auto-commit is guarded by the `SELFDOC_AUTO_COMMIT` environment variable to prevent re-entrant loops (e.g., if a git hook triggers selfdoc).

### Changelog auto-detection

When selfdoc builds a site, it looks for `CHANGELOG.md` in the project root. If found, the changelog is included as a documentation page automatically. In rlsbl-managed projects, `CHANGELOG.md` is generated from JSONL changelog entries, so the docs site always reflects the latest release notes without manual copying.

That convention reads "the root changelog is this project's changelog", which holds for a standalone repo and fails in an rlsbl monorepo: the root `CHANGELOG.md` there rolls up every releasable, and nothing in the build can tell which of them a given site documents. Such a site names its own file instead:

```json
{
  "changelog": ".rlsbl-monorepo/releasables/mytool/CHANGELOG.md"
}
```

A declared path that does not exist is a build error -- the page was asked for, so its absence is a broken build rather than a page that quietly does not appear.

### Version from manifest

selfdoc reads the project version from the project's own manifest: `pyproject.toml`, `package.json`, or a `VERSION` file at the project root (which is where a Go project states it, since `go.mod` carries no version). In rlsbl-managed projects, this version is bumped by `rlsbl release`, so the docs site automatically shows the current version in navigation and search metadata.

### Overriding the version during a release

Documentation is generated *before* the version bump lands, so anything that resolves `project.version` at generation time -- most importantly a root file such as `CLAUDE.md` or `README.md` produced from a template -- would otherwise be committed showing the previous version, on every single release. Pass the about-to-be-released version explicitly:

```
selfdoc gen --version-override 1.4.0
selfdoc check --version-override 1.4.0
```

`gen --version-override` stamps that version into version-bearing generated content instead of reading the (not yet bumped) manifest. `check --version-override` states the version that content is expected to embed, so the check is correct in the window between generation and the bump.

The `VER004` check enforces the pairing: a generated root file whose template interpolates `project.version` must contain the expected version. Generating without the override during a release is a hard failure rather than a silent one-release lag.

Documentation *pages* need no override -- they keep the `var` directive in their committed Markdown and resolve it at build time, which happens after the bump.

## rlsbl Side

### Docs checks during release

A `selfdoc.json` in the project root is all the wiring there is. rlsbl's release flow runs `selfdoc gen --no-auto-commit` and then `selfdoc check` as built-in steps, before the version bump, so broken directives, coverage regressions and SEO errors stop the release before anything is tagged. Neither step needs a hook, and a machine with no `selfdoc` on `PATH` skips both with a note rather than failing.

### Post-release build and deploy

The rlsbl post-release hook is the natural place to rebuild and deploy documentation after a release is published and tagged. This runs after the version bump commit, GitHub Release creation, and tag push are all complete, so the docs reflect the new version. A typical `.rlsbl/hooks/post-release.sh` for a selfdoc project:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Source credentials for Cloudflare deploy
source ~/Projects/.env

# Build the documentation site
selfdoc build

# Deploy to Cloudflare Pages
selfdoc deploy
```

This runs after rlsbl has pushed the release tag and created the GitHub Release. Even if the deploy fails, it does not affect the release itself (post-release hooks are non-fatal).

## Credential Handling

For Cloudflare Pages deploys, selfdoc reads 2 environment variables (`CF_PAGES_API_TOKEN` and `CF_ACCOUNT_ID`). These credentials are not stored in the repository or in GitHub secrets since the deploy runs locally inside the post-release hook. In rlsbl-managed projects, the hook sources them from the shared environment file:

```bash
# In post-release.sh
source ~/Projects/.env
selfdoc deploy
```

No GitHub secrets are needed for this flow -- the deploy runs locally in the hook, not in CI. For GitHub Pages deploys, no credentials are needed since the deploy is handled by the CI workflow.

## Setting It Up

If your project already has both `selfdoc.json` and `.rlsbl/`, the integration is automatic since the tools detect each other at runtime. A new project needs the config file, the post-release deploy, and a deploy provider named in the config -- the generation and check steps come for free once `selfdoc.json` exists. Here is the minimal setup:

1. **Initialize selfdoc** in an rlsbl-managed project:

```bash
selfdoc init
```

2. **Add build and deploy to post-release** (for auto-deploy):

```bash
cat >> .rlsbl/hooks/post-release.sh << 'EOF'
source ~/Projects/.env
selfdoc build
selfdoc deploy
EOF
```

3. **Configure deploy provider** in `selfdoc.json`:

```json
{
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "my-docs-site"
  }
}
```

That is it. On the next `rlsbl release`, docs are checked before release and rebuilt and deployed after.

> [!WARNING]
> Make sure the post-release hook has `source ~/Projects/.env` before `selfdoc deploy`. Without it, the Cloudflare API token is missing and the deploy will fail silently (post-release hooks are non-fatal).

Next: [Atom Feeds](../feeds/)
