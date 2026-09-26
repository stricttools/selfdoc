+++
title = "Deployment"
description = "Deploy your selfdoc site to Cloudflare Pages or GitHub Pages. Covers configuration, credentials, security headers, and CI integration."
nav_group = "Guides"
nav_order = 50
+++

# Deployment

selfdoc generates a fully static site that can be deployed anywhere. Two providers have built-in support with `selfdoc deploy`: **Cloudflare Pages** and **GitHub Pages**.

## Configuration

To enable deployment, add a `deploy` section to your `selfdoc.json` file specifying which hosting provider to use and any provider-specific settings such as the project name. The provider field determines the deployment strategy, while the project field identifies the target on the hosting platform:

```json
{
  "source": [{"path": "mypackage/", "language": "python"}],
  "base_url": "https://myproject.pages.dev",
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "myproject"
  }
}
```

| Field | Required | Description |
| ----- | -------- | ----------- |
| `deploy.provider` | yes | Either `cloudflare-pages` or `github-pages` |
| `deploy.project` | Cloudflare only | The Cloudflare Pages project name |

The `provider` field determines which deployment strategy `selfdoc deploy` uses. The `project` field is only required for Cloudflare Pages (it identifies which Pages project to upload to).

## Cloudflare Pages

### Setup

1. Create a Cloudflare Pages project in the Cloudflare dashboard (or let the first deploy create it).
2. Install the Wrangler CLI:

```bash
npm install -g wrangler
```

3. Authenticate wrangler (interactive login or API token):

```bash
wrangler login
```

4. Configure `selfdoc.json`:

```json
{
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "myproject"
  }
}
```

5. Build and deploy:

```bash
selfdoc build
selfdoc deploy
```

### Environment Variables

For non-interactive environments such as CI pipelines and post-release hooks, you must provide Cloudflare credentials via environment variables instead of running `wrangler login` interactively. selfdoc reads these variables and bridges them to the names that wrangler expects internally:

| Variable | Description |
| -------- | ----------- |
| `CF_PAGES_API_TOKEN` | Cloudflare API token with Pages edit permission |
| `CF_ACCOUNT_ID` | Your Cloudflare account ID |

selfdoc bridges these to the `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` names that wrangler expects internally, so you only need to set the `CF_*` variants.

### Security Headers

When the deploy provider is `cloudflare-pages`, the build automatically generates a `_headers` file in the output directory. This file instructs Cloudflare's edge to serve 6 security headers on every response:

- `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: camera=(), microphone=(), geolocation=()`
- `X-XSS-Protection: 0`

Static assets (`style.css`, SVG files) also get aggressive cache headers (`Cache-Control: public, max-age=31536000, immutable`).

No manual configuration is needed -- these are generated as part of `selfdoc build`.

## GitHub Pages

### Setup

1. Enable GitHub Pages in your repository settings, selecting "Deploy from a branch" with the `gh-pages` branch.
2. Configure `selfdoc.json`:

```json
{
  "deploy": {
    "provider": "github-pages",
    "project": "myproject"
  }
}
```

3. Build and deploy:

```bash
selfdoc build
selfdoc deploy
```

### How It Works

The `selfdoc deploy` command for GitHub Pages performs a clean force-push to the `gh-pages` branch without touching your working tree or local branches. The entire process uses a temporary directory, keeping your repository state unchanged throughout the deployment:

1. Reads the `origin` remote URL from your local git config.
2. Creates a fresh temporary directory with a new git repo.
3. Copies the entire build output into it.
4. Adds a `.nojekyll` file (prevents GitHub from running Jekyll processing).
5. Commits everything and force-pushes to the `gh-pages` branch on your remote.

This approach keeps the `gh-pages` branch clean (a single commit with the latest build), avoids touching your working tree, and works from any branch.

### Security Headers

GitHub Pages does not support custom HTTP headers via a configuration file like Cloudflare's `_headers`. Instead, selfdoc injects `<meta http-equiv>` tags into every generated HTML page when the deploy target is `github-pages`:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Content-Security-Policy` with a restrictive policy (self-hosted scripts and styles, limited external CDN allowlist)

HTTPS and HSTS are handled by the GitHub Pages platform itself -- no configuration is needed on your side.

## Directory-Index URLs

selfdoc generates directory-index URLs for all pages by default, which produces clean URLs without file extensions and avoids trailing-slash redirect chains. For example, a page at `stricttools/docs/guide.md` becomes `guide/index.html` in the output, which is served as `/guide/` by any standard web server.

This approach:

- Works on all hosting platforms without custom redirect rules
- Produces clean URLs without file extensions
- Avoids trailing-slash redirect chains (no `_redirects` file needed)

No configuration is required. All internal navigation links already use the directory form.

## CI Integration

:<: callout-important
:=:
::: If your project uses rlsbl for releases, never run `selfdoc deploy` manually. Let the post-release hook handle it so your docs always match a tagged version. Manual deploys risk publishing docs that are ahead of or behind the released code.
:>:

The recommended approach is to use an rlsbl post-release hook that builds and deploys documentation automatically after each release, keeping your published docs in sync with tagged versions without manual intervention.

### Post-release hook example

Create a post-release hook at `.rlsbl/hooks/post-release.sh` that sources your deploy credentials from a shared environment file and runs the full build-and-deploy pipeline automatically every time a new version tag is pushed to your remote repository:

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Post-release: v$RLSBL_VERSION"

if command -v selfdoc &>/dev/null && [ -f selfdoc.json ]; then
  # Source environment variables for deploy credentials
  [ -f ~/Projects/.env ] && set -a && source ~/Projects/.env && set +a
  echo "Building and deploying docs..."
  selfdoc build && selfdoc deploy
fi
```

The `RLSBL_VERSION` variable is set by rlsbl to the version being released. The hook sources credentials from `~/Projects/.env` (where `CF_PAGES_API_TOKEN` and `CF_ACCOUNT_ID` live for Cloudflare deploys).

### Manual CI

If you are not using rlsbl for release orchestration, you can integrate selfdoc directly into your existing CI pipeline by running these two commands after your test suite passes. Make sure the required environment variables for your chosen provider are set as CI secrets:

```bash
selfdoc build
selfdoc deploy
```

For Cloudflare Pages, ensure `CF_PAGES_API_TOKEN` and `CF_ACCOUNT_ID` are set as secrets in your CI environment. For GitHub Pages, ensure the CI runner has push access to the repository (a `GITHUB_TOKEN` with `contents: write` scope, or a deploy key).

## Custom Domain

Both Cloudflare Pages and GitHub Pages support custom domains, but domain configuration is done on the platform side through their respective dashboards -- selfdoc does not manage DNS records or domain settings directly.

- **Cloudflare Pages**: Add a custom domain in the Cloudflare dashboard under your Pages project settings. See [Cloudflare Pages custom domains](https://developers.cloudflare.com/pages/configuration/custom-domains/).
- **GitHub Pages**: Add a custom domain in your repository's Settings > Pages section. See [GitHub Pages custom domains](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site).

In both cases, set your `base_url` in `selfdoc.json` to match the custom domain so that canonical URLs, sitemaps, and OG tags resolve correctly:

```json
{
  "base_url": "https://docs.myproject.com"
}
```
