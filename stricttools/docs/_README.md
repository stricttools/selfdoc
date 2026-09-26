+++
title = "README.md"
+++
# selfdoc

Static Site Generator that builds a project's documentation site directly from its source code, so the docs can never drift from the code they describe, with SEO/AEO, first-class blog, search, and cross-project linking built in. It is for maintainers who want a repository's documentation built from the repository itself, so API references, CLI help, schemas and tests are written by the code rather than beside it.

Extractors ship for Go, Python, TypeScript/JavaScript, Svelte, Zig, Dart, Kotlin, Swift and SQL. selfdoc is one Go binary with no runtime to install: stylesheets, scripts, themes and the word list are compiled into it.

## Install

```
go install github.com/stricttools/selfdoc@v0
```

On a machine with no Go toolchain, download the archive for your platform from the [latest GitHub Release](https://github.com/stricttools/selfdoc/releases/latest) -- prebuilt binaries are published for Linux, macOS and Windows on amd64 and arm64 -- and put `selfdoc` on your `PATH`.

Two optional dependencies, needed only by the features that use them: [Pagefind](https://pagefind.app/) for the search index, and `python3` for custom directives (a `.py` directive script is the only thing selfdoc runs an interpreter for; every built-in extractor, the Python one included, parses in process).

## Quick start

```bash
# Initialize in an existing project (auto-detects language)
selfdoc init --base-url https://myproject.pages.dev

# Auto-generate API and CLI reference pages
selfdoc gen

# Edit stricttools/docs/ pages -- add directives referencing your code

# Build HTML output
selfdoc build

# Validate directives, coverage, and SEO lint
selfdoc check

# Serve locally with live reload
selfdoc serve
```

Your `selfdoc.json` needs `versions` and `locales` -- even for a single-version, single-locale project. Each source entry names its own path and language:

```json
{
  "source": [{"path": "internal/", "language": "go"}],
  "base_url": "https://my-project.example.com",
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}]
}
```

## Features

- **Directive syntax** -- embed live API references, schemas, tests, and CLI help directly from source code (`:-:`, `:<:`, `:>:`)
- **Auto-generated pages** -- API reference and CLI docs from source code structure (`selfdoc gen`)
- **Multi-version docs** -- build from git tags, cached builds, version picker UI
- **Localization** -- parallel locale directories, hreflang tags, locale picker, per-locale sitemaps
- **Monorepo support** -- unified site builder combines multiple projects into one docs site
- **Blog posts** -- `selfdoc blog post` for authoring, listing pages, feeds, and a local editor app
- **Faceted search** -- key=value filter syntax, 7 dimensions, chip UI, auto-injected version default
- **Sandboxed data generation** -- run scripts in bubblewrap isolation (`selfdoc gen-data`)
- **Theming** -- dark mode, accent colors, custom CSS overrides
- **Search** -- Pagefind, indexed at build time, no network at read time
- **SEO** -- lint rules, WCAG contrast validation, JSON-LD structured data, sitemaps
- **Coverage tracking** -- per-symbol documentation coverage with a configurable threshold
- **Syntax highlighting** -- build-time highlighting via chroma, code tabs, sortable tables
- **Performance** -- CSS/JS/HTML minification, critical CSS inlining, gzip and Brotli pre-compression
- **Feeds and AI** -- Atom feed, `robots.txt` with AI crawler controls, `llms.txt` / `llms-full.txt`
- **Landing page** -- hero section, tagline, and feature cards
- **Live reload** -- SSE-based dev server
- **Auto-commit** -- generated files committed automatically (prefers safegit)

## Directive syntax

Directives are inline blocks in your Markdown templates. They get replaced with content extracted from your source code at build time.

```
:-: directive-name path="arg"
```

Self-closing directives use `:-:`. Block directives that wrap a body use `:<:` to open, `:>:` to close, with `:=:` and `:::` to delimit sections inside. Directives inside fenced code blocks are ignored.

## Built-in directives

:-: table-directives

Example -- embed the API docs for a package:

```markdown
## API Reference

:-: ref path="internal/config"
```

Example -- show a struct's fields as a table:

```markdown
:-: table-schema path="internal/config.Field"
```

## Custom directives

Register custom directives in `selfdoc.json` under the `directives` key. Each entry maps a directive name to a Python script (relative to project root) that defines a `resolve(attrs, config, body)` function returning a Markdown string.

```json
{
  "directives": {
    "changelog": "scripts/changelog_directive.py"
  }
}
```

Script interface:

```python
def resolve(attrs: dict, config: dict, body: list) -> str:
    """Return Markdown string to replace the directive block.

    attrs  -- directive attributes as str->str dict (e.g. {"path": "v1.0.0"})
    config -- the full selfdoc.json config dict
    body   -- body lines from the directive block (empty list for one-liners)
    """
    version = attrs.get("path")
    ...
```

Use in templates:

```markdown
:-: changelog path="v1.0.0"
```

The script runs out of process: selfdoc hands an embedded driver to `python3`, passes the script's path, and sends `attrs`, `config` and `body` as one JSON object on standard input. What the script prints on standard output replaces the directive. A script that will not load, one with no callable `resolve`, one that raises, and a machine with no `python3` are each a hard error that stops the build -- never a note on the published page.

Dispatch order is content directives, then custom directives, then the language extractors -- so a custom name overrides a code-extraction directive such as `ref`, but not a content directive such as `callout-note`.

## Configuration

`selfdoc.json` at the project root:

```json
{
  "source": [{"path": "internal/", "language": "go"}],
  "docs": "stricttools/docs/",
  "output": "stricttools/.docs-cache/build/",
  "base_url": "https://my-project.example.com",
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}],
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "my-docs"
  },
  "directives": {}
}
```

:-: table-config-schema

`selfdoc init` auto-detects language and source paths from project files (go.mod, pyproject.toml, tsconfig.json, package.json), and takes the site's own address as `--base-url`. A project with no detectable language is initialized as a codeless project: no `source` key, and no code-extraction directive in the starter page.

## Commands

:-: table-commands schema-dir="."

## Blog and multi-project assembly

Blog posts and the unified multi-project documentation assembly are part of the same binary. `selfdoc blog post new|list|generate|publish` manages posts, `selfdoc blog editor serve` runs the local authoring app, and `selfdoc assembly ...` initializes, pushes, rebuilds and verifies an assembly that mounts every project under its own slug.

## Deploy

### Cloudflare Pages

Requires the [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/) installed and authenticated.

```json
{
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "my-docs-project"
  }
}
```

```bash
selfdoc build && selfdoc deploy
```

### GitHub Pages

Pushes the output directory to the `gh-pages` branch via force-push.

```json
{
  "deploy": {
    "provider": "github-pages"
  }
}
```

Enable GitHub Pages in your repo settings (source: `gh-pages` branch).

## Integration with rlsbl

When [rlsbl](https://github.com/smm-h/rlsbl) detects a `selfdoc.json` in the project, it can trigger `selfdoc build` and `selfdoc deploy` as part of the release lifecycle via the `.rlsbl/hooks/post-release.sh` hook.

## Documentation

Full documentation at [selfdoc.smmh.dev](https://selfdoc.smmh.dev).

## License

MIT
