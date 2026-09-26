+++
title = "selfdoc"
description = "Build documentation sites from Markdown and source code. Directives pull live content from your codebase, so init, build and serve keep docs in sync."
nav_order = 0
feed = false
+++

selfdoc builds documentation websites from your Markdown files and source code. Write docs in Markdown, reference your actual code with directives, and selfdoc keeps everything in sync -- when your code changes, your docs update automatically.

## How it works

### 1. Write Markdown with directives

Create a Markdown file in your `.stricttools/docs/` folder. Use directive markers to reference modules, functions, classes, schemas, and CLI definitions in your source code. selfdoc resolves each directive at build time, extracting live content so your documentation always matches the actual implementation:

```markdown
# API Reference

:-: ref path="mypackage.core"
```

That `:-: ref` line tells selfdoc to grab the docstrings and signatures from `mypackage.core` and drop them right into your page. No copy-pasting, no manual updates.

### 2. Configure with selfdoc.json

A config file tells selfdoc which directories hold source code and in what language, where your docs templates live, and where to write the built output. Navigation, theming, search and SEO metadata all have defaults:

```json
{
  "source": [{"path": "mypackage/", "language": "python"}],
  "base_url": "https://myproject.example.com",
  "search_engine": "pagefind",
  "author": {"name": "Jane Doe", "url": "https://janedoe.example"},
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}],
  "docs": ".stricttools/docs/",
  "output": ".stricttools/docs-cache/build/"
}
```

selfdoc figures out the rest. It detects your project structure, builds navigation from your file layout, and applies a clean default theme.

### 3. Build and deploy

Run `selfdoc build` and you get a full static site -- HTML, CSS, search index, sitemap, the works. Serve it locally with `selfdoc serve`, or deploy anywhere that hosts static files.

```bash
go install github.com/stricttools/selfdoc@v0
selfdoc init --base-url https://myproject.pages.dev
selfdoc build
```

## Before and after

Here is what a typical docs template looks like before the build, and what selfdoc turns it into after directive resolution. The Markdown file stays clean and readable while the rendered output contains the full extracted content from your source code.

Your Markdown file:

```markdown
# User Guide

Welcome to mypackage. Here is the full API:

:-: ref path="mypackage.core"

And the CLI usage:

:-: code-help path="mypackage.cli"
```

The rendered output: a styled HTML page with your welcome text, followed by a complete API reference (every public function, class, and docstring from `mypackage.core`), then a formatted CLI help section showing every command and flag from `mypackage.cli`. All extracted live from your source code.

:<: callout-tip
:=:
::: You never edit the generated output. Change your code, rebuild, and the docs reflect reality.
:>:

## Features

selfdoc ships with everything you need to build, check, and deploy a documentation site, in one binary with nothing to install beside it. Here are the core capabilities that work out of the box.

:<: callout-note
:=:
::: All of these work out of the box with zero configuration beyond the basic selfdoc.json.
:>:

- **Code-aware directives** -- Embed live API references, schemas, tests, and CLI help directly from source code. Content stays in sync automatically.
- **Multi-language support** -- extractors for Go, Python, TypeScript, Svelte, Zig, Dart, Kotlin, Swift and SQL, all feeding the same directive vocabulary. One project can declare several.
- **One static binary** -- written in Go, with stylesheets, scripts, themes and the word list compiled in. No runtime to install, no virtual environment, no JavaScript toolchain, no configuration overhead.
- **SEO and AI optimized** -- Structured data, meta tags, sitemaps, llms.txt, Atom feeds, and 50+ SEO best practices built into every generated page.
- **Themeable and accessible** -- built-in themes with dark mode, WCAG AA contrast, print stylesheets, and full keyboard navigation.

## Get started

Ready to try it? Head over to the [Getting Started](getting-started/) guide for installation, project initialization, and your first documentation build. The guide walks you through creating a `selfdoc.json`, writing your first template with directives, and previewing the output locally with live reload.
