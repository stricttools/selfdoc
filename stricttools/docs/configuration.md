+++
title = "Configuration"
description = "Complete reference for selfdoc.json configuration options including project settings, themes, SEO, deployment, example validators, and branding."
nav_group = "Guides"
nav_order = 20
+++

# Configuration

selfdoc is configured via a `selfdoc.json` file in your project root. Run `selfdoc init` to generate a starter config interactively, or create one manually.

Five fields are required and nothing is inferred for them: `base_url`, `search_engine`, `author`, `locales`, and `versions` (or `"unversioned": true` in its place). `source` is optional -- a codeless project (a portfolio or personal site that is nothing but Markdown pages) declares none, and directives that extract from source code are then a hard error rather than an empty section. Everything else is optional and has defaults.

There is no top-level `language` key. Every source entry is an object naming its own path and language, so a polyglot repository declares one entry per language:

```json
{
  "source": [
    {"path": "internal/", "language": "go"},
    {"path": "web/src/", "language": "typescript"}
  ]
}
```

:<: callout-warning
:=:
::: `base_url` is required for deployment. Without it, canonical URLs, sitemaps, OG tags, and Atom feeds will have broken links. Set it to the URL where your site will be hosted (e.g., `https://myproject.pages.dev`).
:>:

## Config Reference

The table below lists every field recognized by `selfdoc.json`, including the field type, whether it is required, and a description of what it controls. Required fields have no default and must be provided explicitly.

:-: table-config-schema

## Common Configurations

### Minimal Python project

```json
{
  "source": [{"path": "src/", "language": "python"}],
  "base_url": "https://myproject.pages.dev",
  "search_engine": "pagefind",
  "author": {"name": "Jane Doe", "url": "https://janedoe.example"},
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}]
}
```

### Go project with deployment

```json
{
  "source": [
    {"path": "pkg/", "language": "go"},
    {"path": "internal/", "language": "go"}
  ],
  "base_url": "https://myproject.pages.dev",
  "search_engine": "pagefind",
  "author": {"name": "Jane Doe", "url": "https://janedoe.example"},
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}],
  "repo": "https://github.com/user/myproject",
  "branch": "main",
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "myproject"
  }
}
```

### Full-featured project with branding

```json
{
  "source": [{"path": "mylib/", "language": "python"}],
  "base_url": "https://mylib.dev",
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}],
  "docs": "stricttools/docs/",
  "output": "stricttools/.docs-cache/build/",
  "description": "A toolkit for building great things.",
  "repo": "https://github.com/user/mylib",
  "branch": "main",
  "lang": "en",
  "theme": "minimal",
  "search": "bar",
  "search_engine": "pagefind",
  "coverage_threshold": 0.8,
  "author": {
    "name": "Jane Doe",
    "url": "https://janedoe.example",
    "same_as": ["https://github.com/janedoe"]
  },
  "twitter": "@janedoe",
  "deploy": {
    "provider": "cloudflare-pages",
    "project": "mylib"
  },
  "branding": {
    "tagline": "Build great things.",
    "cta_text": "Get Started",
    "cta_link": "getting-started/",
    "features": [
      {
        "title": "Fast",
        "description": "Blazing fast builds with zero dependencies."
      },
      {
        "title": "Flexible",
        "description": "Works with Go, Python, TypeScript and six more languages."
      }
    ]
  },
  "directives": {
    "changelog": "scripts/changelog-directive.py"
  }
}
```

## Example Validators

The `examples` key maps a fenced-block language to the command that validates a snippet written in it. `selfdoc check` uses these commands for code blocks marked `validate` in their fence info string, writing each block to a scratch file and substituting its path for `{file}`:

```json
{
  "examples": {
    "python": "uv run --directory python python {file}",
    "go": "scripts/validate-example-go.sh {file}",
    "ts": "scripts/validate-example-ts.sh {file}"
  }
}
```

Every command template must contain the `{file}` placeholder; a template without it is rejected when the config loads, since it would validate nothing. Keys are language names exactly as they appear after the opening fence, so a block opened with ` ```py ` needs a `py` entry, not a `python` one. Omitting `examples` entirely turns the feature off, and any `validate` marker in the docs then reports `EXAMPLE003`. See the [Check Guide](../check-guide/) for the full behavior.
