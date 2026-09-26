+++
title = "Architecture"
description = "How selfdoc transforms Markdown templates into a static site: the build pipeline, directive resolution, theme system, and auxiliary outputs."
nav_group = "Guides"
nav_order = 60
+++

# Architecture

This page explains how selfdoc works at a high level -- what happens when you run `selfdoc build`, how directives get resolved, and how themes control the output. For internal implementation details (tokenizer internals, extractor protocol, resolver dispatch chain), see [Internals](../internals/).

## Build Pipeline Overview

The build transforms your `.stricttools/docs/` Markdown templates into a deployable static HTML site through 7 stages. Each stage receives the output of the previous one and produces a deterministic result:

1. **Scan** -- walk both docs roots, the handwritten `.stricttools/docs/` and the
   generated `.stricttools/docs-state/pages/`, for `.md` files, parse frontmatter
2. **Resolve directives** -- replace directive markers with generated Markdown content (from source code or content transforms)
3. **Tokenize** -- split the resolved Markdown into typed block tokens
4. **Render** -- dispatch each token to a block-level HTML renderer
5. **Post-process** -- apply heuristic transforms (code tabs, step guides, API entries, definitions, LCP promotion)
6. **Generate HTML** -- wrap rendered content in a full page shell with navigation, metadata, and styles
7. **Auxiliary output** -- generate sitemap, Atom feed, search index, OG images, `llms.txt`, and compressed companions

There is no shared mutable state between stages, so each one can be tested and debugged independently.

## How Directives Resolve

When you write a directive like `:-: ref path="mypackage.core"`, the build pipeline resolves it through a three-level dispatch chain. Each level is tried in order and resolution stops at the first match, so content directives take precedence over custom directives, which take precedence over the language extractor:

1. **Content directives** -- callouts (`callout-note`, `callout-warning`, etc.) and `list-glossary` are handled first. These transform body content into styled HTML without needing source code access.

2. **Custom directives** -- if your `selfdoc.json` declares a `"directives"` map, the resolver runs the referenced script out of process: an embedded driver is handed to `python3`, and the script's `resolve(attrs, config, body)` is called with one JSON object on standard input. What it prints replaces the directive; any failure stops the build.

3. **Language extractor** -- the built-in extractor for your project's language handles the directive. This is the most common path for directives like `ref`, `table-schema`, and `code-test`.

A path that resolves nowhere leaves an inline marker (`> *[selfdoc: ...]*`) in the rendered output so you can spot it immediately. A custom directive that fails is different: a script that will not load, one with no callable `resolve`, one that raises, and a machine with no `python3` are each a hard error naming the directive and the script, because a published page reading "custom directive failed" where its content belongs is as easy to miss as any other paragraph.

### What each directive extracts

| Directive | What it pulls from your code |
|-----------|------------------------------|
| `ref` | Module docstring, exported functions, classes with signatures and docs |
| `table-schema` | Dataclass/struct/interface fields as a Markdown table |
| `code-test` | Test function source code (whole file or a specific function) |
| `code-help` | CLI argument parser definitions and help text |
| `table-config` | Configuration keys with types and descriptions |

## How Themes Work

Themes control colors, typography, layout, and component styling through CSS custom properties. Every built-in theme includes full dark mode, high contrast, reduced motion, and print support; the registry is the set of stylesheets compiled into the binary, so `selfdoc build --theme` refuses anything that is not one of them.

The theme system works in layers:

- **Theme CSS** defines all custom properties in `:root` -- colors, fonts, spacing, shadows
- **Dark mode** uses a dual-selector strategy: `@media (prefers-color-scheme: dark)` for automatic OS detection, plus `[data-theme="dark"]` for manual toggle
- **Custom overrides** via `.stricttools/docs/custom.css` are injected after the theme, so your rules always win
- **High contrast** responds to `prefers-contrast: more` with stronger borders and text

See the [Theming](../theming/) page for the full property reference and design tool.

## Auxiliary Outputs

After the main HTML pipeline completes, the build generates 6 categories of companion files that enhance search engine discoverability, social sharing, client-side search, AI access, and transfer performance. These are all written to the same output directory alongside the HTML pages:

- **Sitemap** (`sitemap.xml`) -- standard sitemap with page URLs and last-modified dates
- **Atom feed** (`feed.xml`) -- for RSS readers, respects per-page `feed = false` frontmatter
- **Search index** (`pagefind/`) -- the Pagefind index and search UI, built from the finished HTML
- **OG images** -- OpenGraph card PNGs for social sharing
- **`llms.txt`** -- structured plain-text index for LLM consumption, plus `llms-full.txt` with all content
- **Compressed companions** -- gzip and brotli pre-compressed versions for efficient serving; both encoders are compiled into the binary

## Post-Processors

After block rendering produces HTML, 5 regex-based post-processors detect cross-block patterns and apply semantic enhancements. These operate on the joined HTML string rather than individual tokens, because the patterns they detect span multiple blocks:

- **Code tabs** -- consecutive code blocks with different languages become a tabbed interface
- **Step guides** -- ordered lists after headings containing "step", "guide", or "tutorial" get a `class="steps"` for special styling
- **API entries** -- sequences of h3/h4 + code block + description are wrapped in `<div class="api-entry">` cards
- **Definition ids** -- every author-declared definition site (a definition list, the `list-glossary` directive, or a hand-written `<dfn>`) gets a `term-<slug>` id so the glossary can link to it. A term is only ever declared; prose that reads like a definition declares nothing.
- **LCP promotion** -- the first image is promoted from `loading="lazy"` to `fetchpriority="high"` for faster Largest Contentful Paint

## Staleness Tracking

The build computes content hashes for each page and persists them. The `check` command later uses these hashes to detect when page content has changed but its frontmatter description has not been updated -- catching stale SEO metadata before it goes live.
