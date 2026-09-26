+++
title = "selfdoc vs the Competition: Documentation Generators Compared"
date = 2026-06-29
slug = "selfdoc-vs-the-competition-documentation-generators-compared"
tags = ["comparison", "documentation", "static-site-generators"]
draft = false
directives = false
description = "A feature-by-feature comparison of selfdoc with Sphinx, Zensical, Docusaurus, Starlight and VitePress across what technical documentation needs."
+++

The documentation generator landscape shifted in 2026. Material for MkDocs entered maintenance mode after years of dominance, and its successor Zensical launched with a Rust-powered build engine. Meanwhile, the JavaScript ecosystem continues to fragment across React, Vue, and Astro-based options. This post compares selfdoc against six major tools across the features that matter most for technical documentation.

## Feature matrix

| Feature | selfdoc | Sphinx | Zensical | Docusaurus | Starlight | VitePress |
|---|---|---|---|---|---|---|
| Code-aware directives | Yes (10 languages) | Python only (autodoc) | No | No | No | No |
| Multi-version docs | Yes (git-tag based) | Via sphinx-multiversion | No | Yes (native) | No | No |
| Localization (i18n) | Yes (built-in) | Yes (gettext) | Community plugin | Yes (native) | Yes (native) | Yes (native) |
| Search | Built-in faceted | Basic / third-party | Disco (new) | Algolia integration | Pagefind | MiniSearch |
| API reference gen | Yes (from source) | Yes (autodoc/autosummary) | No | No | No | No |
| Monorepo unified sites | Yes (assembly system) | No | No | No | No | No |
| Root file generation | Yes (templates) | No | No | No | No | No |
| Release-gated builds | Yes (via rlsbl) | No | No | No | No | No |
| Zero-JS output | Yes | Yes | Yes | No | Yes | No |
| HMR / dev server | No | No | Yes (fast) | Yes | Yes (fastest) | Yes (sub-100ms) |
| WYSIWYG editing | No | No | No | No | No | No |
| Mature ecosystem | New | 15+ years | New (2026) | Large (64k stars) | Growing (8.4k stars) | Growing (17.6k stars) |

## Per-tool breakdown

### Sphinx (v9.1.0)

The Python ecosystem's gold standard for API documentation. Autodoc extracts docstrings directly from Python modules, and the extension ecosystem (MyST for Markdown, intersphinx for cross-project linking) is unmatched in depth. The trade-off is complexity: configuration is verbose, builds are slow on large projects, and the reStructuredText default is a barrier for contributors who think in Markdown. Best for: large Python libraries that need exhaustive API reference.

### Zensical (June 2026)

The spiritual successor to Material for MkDocs, built on a Rust engine claiming 4-5x faster builds. It reads existing `mkdocs.yml` files, easing migration from Material. The new Disco search replaces Lunr. MIT licensed. Too early to evaluate ecosystem maturity, but the build speed and backward compatibility with MkDocs config make it a strong contender for teams already in that ecosystem.

### Material for MkDocs (v9.7.0)

In maintenance mode since late 2025 with an estimated 90,000 GitHub projects using it. Still functional and widely documented, but new projects should evaluate Zensical instead. The plugin ecosystem remains the largest of any MkDocs-based tool.

### Docusaurus (v3.10.1)

Meta's React-based generator with the most mature built-in versioning system. Algolia DocSearch integration is best-in-class for large sites. The React dependency means heavier output bundles and a Node.js toolchain requirement. Best for: JavaScript/TypeScript projects that want versioned docs with minimal configuration.

### Starlight (v0.39)

The Astro team's documentation framework ships zero client-side JavaScript by default, producing the lightest pages in this comparison. Fastest HMR during development. No built-in versioning or API reference generation -- these require community integrations or manual solutions. Best for: projects that prioritize page performance and have simple versioning needs.

### VitePress (v1.x, 17.6k stars)

Vue-powered with sub-100ms HMR. Clean default theme and straightforward Markdown authoring. Like Starlight, it lacks built-in versioning and API reference generation. Best for: Vue ecosystem projects or teams that want fast iteration with minimal configuration.

## Where selfdoc fits

selfdoc occupies a different niche than the tools above. Rather than being a general-purpose static site generator with documentation features, it is a documentation generator that understands source code.

**Code-aware directives.** selfdoc's directive system extracts content directly from source files -- function signatures, docstrings, type definitions, struct schemas -- across Python, Go, TypeScript, JavaScript, Dart, Kotlin, Swift, Svelte, Zig, and SQL. Documentation stays synchronized with code because it is derived from code, not duplicated alongside it.

**Root file generation.** Templates in `.stricttools/docs/` generate project root files like `README.md` and `CLAUDE.md` using the same directive system. One source of truth produces both the documentation site and the files developers encounter first.

**Assembly system.** The unified builder composes documentation from multiple projects into a single site with shared navigation, search, and theming. This is purpose-built for monorepos and multi-package ecosystems.

**Release-gated builds.** Integration with rlsbl means documentation is validated as part of the release pipeline. Stale directives, broken references, and missing coverage are caught before a version ships, not after.

## Honest limitations

selfdoc is newer and less battle-tested than Sphinx or Docusaurus. The community is small. The runtime is Python-only (no native Rust/Go build speed). There is no dev server with hot module replacement -- changes require a rebuild. There is no WYSIWYG editing experience. Teams choosing selfdoc are choosing a tool optimized for correctness over convenience: documentation that cannot drift from code, at the cost of a smaller plugin ecosystem and fewer community resources.

## Choosing

If your priority is build speed and you are migrating from MkDocs, look at Zensical. If you need exhaustive Python API docs with a mature ecosystem, Sphinx remains the standard. If you want polished versioned docs with minimal setup in a JavaScript project, Docusaurus is hard to beat. If page weight matters above all, Starlight.

If your priority is documentation that stays correct -- that extracts from source, validates during release, and assembles across projects -- selfdoc is built for that problem specifically.
