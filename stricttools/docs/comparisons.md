+++
title = "Comparisons"
description = "How selfdoc compares to Sphinx, MkDocs, Docusaurus, VitePress, Rustdoc, Godoc, and TypeDoc -- features, tradeoffs, and where each tool shines."
nav_group = "Getting Started"
nav_order = 2
+++

# Comparisons

selfdoc takes a different angle from most documentation generators: it extracts content directly from source code through a catalogue of built-in directives, reads nine languages in a single tool, and produces a complete static site with search, SEO, and theming -- out of one Go binary with no runtime to install beside it.

Here is how it stacks up against the popular alternatives.

## Feature Comparison

| Feature | selfdoc | Sphinx | MkDocs | Docusaurus | VitePress | Rustdoc | Godoc | TypeDoc |
| ------- | ------- | ------ | ------ | ---------- | --------- | ------- | ----- | ------- |
| Language support | Go, Python, TS/JS, Svelte, Zig, Dart, Kotlin, Swift, SQL | Python | Any | Any | Any | Rust | Go | TS/JS |
| Source code extraction | Directive-based, multi-language | Autodoc (Python only) | None (plugin-based) | None | None | Built-in | Built-in | Built-in |
| Markdown-native | Yes | RST (Markdown via plugin) | Yes | MDX | Yes | No (doc comments) | No (doc comments) | No |
| Zero config | Near-zero (`selfdoc init`) | No (conf.py required) | Minimal (mkdocs.yml) | No (Node project) | Minimal | Zero for Rust | Zero for Go | Minimal |
| Built-in search | Pagefind, indexed at build time | Yes | Yes (lunr.js) | Yes (Algolia/local) | Yes (local) | Yes | No | Yes |
| Theming | Built-in themes + CSS properties | Many themes | Many themes | Many themes | Customizable | One | One | Themes |
| Versioning | Built-in multi-version builds | Via extensions | mike plugin | Built-in | Via config | Per crate | Per module | No |
| i18n | Built-in multi-locale builds | Sphinx-intl | i18n plugin | Built-in | Built-in | No | No | No |
| Static output | Yes | Yes | Yes | Yes (SSG mode) | Yes | Yes | Server or static | Yes |
| Deploy targets | Cloudflare Pages, GitHub Pages | Any static host | Any static host | Any static host | Any static host | docs.rs | pkg.go.dev | Any static host |

## Where Each Tool Shines

### Sphinx

The heavyweight champion for Python documentation. Sphinx has a massive ecosystem of extensions (intersphinx, autodoc, napoleon, etc.) and handles large, cross-referenced documentation sets better than anything else. If your project is Python-only, has hundreds of modules, and needs cross-project linking, Sphinx is the mature choice. The tradeoff is complexity: `conf.py` configuration, RST syntax by default, and a significant learning curve.

### MkDocs

The simplest way to put Markdown docs online. MkDocs with Material theme gives you a polished site with minimal config. It does not extract from source code natively, but the ecosystem has plugins for most needs. If you are writing prose documentation (tutorials, guides, architecture docs) rather than API references, MkDocs is fast and well-supported.

### Docusaurus

React-powered documentation with MDX support. Docusaurus is the go-to for JavaScript/React projects that want interactive components in their docs. It has built-in versioning, i18n, and Algolia search integration. The cost is a full Node.js build chain and React in your docs pipeline.

### VitePress

Vue-powered and fast. VitePress is the spiritual successor to VuePress, optimized for speed and simplicity. Great for Vue ecosystem projects and sites that want a modern, minimal build. Less feature-rich than Docusaurus but lighter.

### Rustdoc

The gold standard for language-integrated documentation. Rustdoc extracts docs directly from Rust source comments, runs doctests, and hosts on docs.rs automatically. If you are documenting Rust, there is no reason to use anything else. The format is fixed -- you get what Rustdoc gives you.

### Godoc

Simple, convention-driven documentation for Go packages. Godoc reads package comments and produces consistent, browsable API docs hosted on pkg.go.dev. Like Rustdoc, it is purpose-built for one language and does that job well. No theming, no customization, no search -- just clean API references.

### TypeDoc

API documentation generator for TypeScript and JavaScript projects. TypeDoc reads JSDoc and TypeScript type information to produce reference docs. Strong on types and signatures, but focused on API references rather than full documentation sites.

### selfdoc

selfdoc fits a niche that the others do not cover well: a polyglot repository that wants source-extracted documentation for every language it contains, without switching tools or running one generator per component. The directive system keeps docs in sync with code automatically, and the built-in SEO, search, blog and deploy pipeline means fewer moving parts. The tradeoff is fewer themes and a smaller community than Sphinx or Docusaurus.

Next: [Getting Started](../getting-started/)
