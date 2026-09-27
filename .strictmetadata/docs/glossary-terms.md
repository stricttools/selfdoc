+++
title = "Glossary"
description = "Alphabetical glossary of key terms used in the selfdoc documentation, including directives, extractors, frontmatter, themes, and build pipeline concepts."
nav_group = "Reference"
nav_order = 50
+++

# Glossary

This page defines the key terms used throughout the selfdoc documentation.

:<: list-glossary
:=:
::: **Directive**: A marker in Markdown templates that selfdoc resolves into content at build time. Comes in one-liner (`:-:`) and block (`:<: ... :>:`) forms.
::: **Extractor**: A language-specific component that reads source code and extracts API information for directives. Selfdoc ships with extractors for Go, Python, TypeScript, Svelte, Zig, Dart, Kotlin, Swift and SQL.
::: **Frontmatter**: The TOML metadata block at the top of a Markdown file, between `+++` fences. Its keys are a declared registry: title, description, nav group and the rest of a page's settings.
::: **Resolver**: The dispatch layer that routes a parsed directive to the correct handler -- content directives, custom directives, or language extractors, in that order.
::: **Tokenizer**: A standalone module that splits Markdown into typed block tokens (headings, code blocks, tables, paragraphs, etc.) used by both the renderer and the lint system.
::: **Theme**: A set of CSS custom properties controlling colors, typography, layout, and component styling. Every theme's stylesheet is compiled into the binary, and the set of embedded stylesheets is the registry of theme names.
::: **Callout**: A content directive that renders a styled admonition box. Types include `callout-note`, `callout-warning`, `callout-tip`, `callout-danger`, and `callout-important`.
::: **Code block**: A fenced block of source code (triple backticks) that gets syntax-highlighted in the rendered output. Consecutive code blocks with different languages become tabbed.
::: **OG card**: An OpenGraph image generated for each page, used as the preview image when a link is shared on social media or messaging platforms.
::: **Sitemap**: An XML file (`sitemap.xml`) listing all page URLs and their last-modified dates, used by search engines for indexing.
::: **Atom feed**: An XML feed (`feed.xml`) that allows RSS readers to subscribe to documentation updates. Pages can opt out with `feed = false` in frontmatter.
::: **Canonical URL**: The definitive URL for a page, set via the `base_url` config field. Used in `<link rel="canonical">` tags and sitemaps to avoid duplicate content in search engines.
::: **JSON-LD**: Structured data embedded in each HTML page as a `<script type="application/ld+json">` block. Provides search engines with machine-readable metadata about the page.
::: **Lint rule**: An SEO or content quality check run by `selfdoc check`. Each rule has a code (e.g., SEO001) and produces warnings or errors with file locations.
::: **Coverage**: The proportion of public symbols in your source code that are documented through directives. Reported by `selfdoc check` and held to the `coverage_threshold` fraction in `selfdoc.json`.
::: **Staleness**: When a page's content has changed since the last build but its frontmatter description has not been updated. Detected by comparing content and description hashes.
::: **Build pipeline**: The seven-stage process that transforms Markdown templates into a static HTML site: scan, resolve, tokenize, render, post-process, generate HTML, auxiliary output.
::: **Nav group**: A frontmatter field (`nav_group`) that controls which section of the sidebar navigation a page appears in. Pages with the same nav group are grouped together.
::: **Slug**: The URL-friendly identifier derived from a page's filename. For example, `getting-started.md` becomes the slug `getting-started`, served at `/getting-started/`.
::: **Search index**: The Pagefind index under `pagefind/`, built from the finished HTML pages, powering the client-side full-text search in the rendered site.
::: **Post-processor**: A transform that runs on the rendered HTML after block rendering, to detect cross-block patterns like consecutive code blocks (code tabs) or ordered lists after tutorial headings (step guides).
::: **Custom directive**: A project-specific directive defined in `selfdoc.json` that points to a script resolving it. A `.py` script defines `resolve(attrs, config, body)` and is called by an embedded Python driver; any other script is executed directly. Takes priority over the code-extraction directives, not over content directives.
::: **Content directive**: A directive that transforms body content into styled HTML without needing source code access. Includes callouts and `list-glossary`.
:>:
