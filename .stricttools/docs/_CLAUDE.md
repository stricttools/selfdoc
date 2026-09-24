+++
title = "CLAUDE.md"
+++
# selfdoc

Code-aware static site generator. Builds full documentation sites from Markdown templates and source code, with directive-based content extraction, auto-generated API/CLI reference pages, multi-version support, localization, monorepo unified sites, faceted search, theming, SEO, blog posts, a unified multi-project assembly, and deploy to Cloudflare Pages or GitHub Pages. Extractors ship for Go, Python, TypeScript/JavaScript, Svelte, Zig, Dart, Kotlin, Swift and SQL.

## Conventions

- Pure Go, one module (`github.com/stricttools/selfdoc`) and one binary, whose entry point is the module root. Every engine package is under `internal/`.
- Runtime dependencies: `strictcli` (the CLI framework, and the effects handle every mutation is minted on), `strictspec` (generates the validators for the declarative catalogue and lint-registry documents), `tinymoon` (the theme framework), `chroma` (syntax highlighting), `go-toml-edit` (TOML parsing), `gotreesitter` (the pure-Go parser the Python extractor reads its trees from), `andybalholm/brotli`, `golang.org/x/text`. Tests add `testisolation` and `playwright-go`.
- Install: `go install github.com/stricttools/selfdoc@v0`, or the platform archive from the GitHub Release on a machine with no Go toolchain. Go is the only distribution channel: there is no npm package and no PyPI package.
- No cgo and no runtime asset directory: stylesheets, JS, the word list, the directive catalogue, the lint registry and the Python grammar's tables are all embedded in the binary. The release build passes `-tags=grammar_subset,grammar_subset_python` so only the Python grammar is compiled in.
- python3 is needed for one thing only, and only when it is used: a custom directive written as a `.py` script (see below). Every built-in extractor, the Python one included, parses in process, so it is not a build dependency and not a documenting dependency either.
- Effects: every mutation, subprocess and network call goes through an explicit `*effects.Handle` threaded from the command. There is no package-level handle.
- File writes to shared state are atomic (write to a temp file, then rename).
- External calls (subprocess, network) must have timeouts.

## Key concepts

### One binary, one command tree

`selfdoc` carries every command the three former Python packages exposed:

- top level: `init`, `build`, `serve`, `deploy`, `check`, `gen`, `gen-data`, `spell-corpus`, `quality`
- `baseline` -- accept the content and description hash baselines that drive STALE001 and DRIFT001
- `blog` -- everything about writing: `blog post` creates, lists, generates and publishes posts, `blog editor` runs the local authoring app, and `blog publish-docs` publishes this project's documentation to the unified assembly without a release
- `assembly` -- initialize, push, inspect, rebuild, retire and verify the unified multi-project site

### Stable addresses, archived versions

The current version of every page lives at a stable, unversioned address --
`/page/` -- and superseded versions live beside it under the archive prefix,
at `/v/<version>/page/`. The locale segment appears only when a project
really has more than one locale, so a single-locale project's current
version is served from the site root. Every version of a page declares the
stable address canonical.

The config MUST have `versions` and `locales` arrays -- these are required.
Even a single-version, single-locale project needs them:

```json
{
  "versions": [{"version": "0.8.1"}],
  "locales": [{"code": "en", "label": "English", "default": true}]
}
```

A project that publishes no artifact -- a portfolio, a personal site --
declares that instead of naming a version it never released:

```json
{
  "unversioned": true,
  "locales": [{"code": "en", "label": "English", "default": true}]
}
```

`unversioned` replaces `versions` (declaring both is an error) and is
refused for a project that declares `source`, because code is what gets
released and therefore carries a version. Such a project's pages show no
version badge, offer no version search filter and no version picker.
`selfdoc init` writes this declaration for a project with no detectable
language, and refuses to invent a version for one that has code but states
none in its manifest.

### Multi-version builds

Builds documentation from git tags. Tagged versions are checked out and built from cache (`.stricttools/docs-cache/versions/`), while the latest version builds from the working tree. The version picker's links are computed by the build from each page's own address, and archived pages carry a dismissable notice keyed per version.

### Localization

Parallel `.stricttools/docs/<locale>/` directories with per-locale templates. Generates hreflang tags, per-locale sitemaps, and locale picker UI.

### Monorepo unified sites

`internal/blog/unified` orchestrates building a single documentation site from multiple constituent projects plus a docs-site's own cross-cutting content. Configured via the `unified` section in `selfdoc.json`. The unified project is effectively the (N+1)th docs-site project.

### Search filters

Pagefind indexes the built HTML and ships its own UI. Pages emit filter attributes for 7 facets -- version, locale, group, type, target, project and tags -- so the dialog offers each as a filter group. Pages declare tags via frontmatter as a TOML array: `tags = ["a", "b", "c"]`.

### Directives

6 marker types for embedding code-extracted content in Markdown templates:
- `:-:` -- self-closing directive
- `:<:` -- block open
- `:@:` -- block attribute
- `:=:` -- section separator
- `:::` -- section content
- `:>:` -- block close

### Custom directives

The `directives` config key maps a directive name to a script, and the contract is Python's: the script defines `resolve(attrs, config, body)` and returns Markdown. selfdoc keeps that contract by running the script out of process -- an embedded driver is handed to `python3`, the script's path as its one argument and one JSON object (`attrs`, `config`, `body`, `base_dir`) on standard input, and what the script prints on standard output replaces the directive. A script that will not load, one with no callable `resolve`, one that raises, and a machine with no python3 are each a hard error naming the directive and the script -- never an inline note on the published page.

### gen_data

Sandboxed script execution via bubblewrap (bwrap). Runs scripts in isolated environments to generate data files used by the build.

### Root file templates

`.stricttools/docs/_CLAUDE.md` and `.stricttools/docs/_README.md` are templates that generate the project root `CLAUDE.md` and `README.md` via `selfdoc gen`. They support directives like any other template.

## Release workflow

This project uses [rlsbl](https://github.com/smm-h/rlsbl) for release orchestration.

- `selfdoc check` runs during release (validates directives, coverage, lint)
- Deploy to the unified assembly via post-release hook
- CI builds and uploads the per-platform release archives with goreleaser
- Never publish manually -- always use `rlsbl release`

## Testing

```bash
go build ./...
go test ./...                    # every package's tests
go test -tags e2e ./internal/e2e/  # the rendered-reality suite, needs a browser
```

Tests live beside the package they cover, as table tests. They cover config loading, directive parsing, the build pipeline, every language extractor, the check command, gen, gen-data, the unified builder, search, pickers, filters, localization, multi-version builds, posts, the assembly and the editor.

### The rendered-reality suite

`internal/e2e` asserts against real pages in a real headless browser, behind an `e2e` build tag so `go test ./...` skips it. It exists because six user-visible defects shipped while thousands of unit tests and every grep-level check passed: a sticky table header overlapping the first data row, a table of contents visible only inside one band of viewport widths, a shared page served with no stylesheet, a duplicated "Last updated" element, absolute links that left the site, and a glossary term no page had defined. None of those is visible to a test that asserts on a string of HTML; every one is obvious to a browser.

**The pipeline is never mocked.** That is the suite's design principle. The fixture writes source checkouts and hands them to the production preview path: the real build, the real graft, the real shared-file generation, a real Pagefind index, the production preview server on an ephemeral loopback port, and the production assembly verification as a precondition on the tree. Dependency injection is for genuine external seams -- the network, the clock, another repository -- and for nothing else. Mocked-flow tests are how the defects above shipped.

Two trees are built and served per theme, because a project has two published shapes: the **assembled site**, where every project mounts under its slug and only the current version is published, and the **standalone site** a project deploys on its own, which is where the archive under `v/<version>/` lives.

What it asserts, each mapped to a defect class: sticky table headers and the pinned first column measured as painted; one visible "Last updated" per page and no more; table-of-contents presence swept across a range of viewport widths (and its total absence from posts, at every width); every page's computed body style differing from the browser default, with network capture on stylesheet requests; every visible link resolving on-origin and answering below 400; glossary Source links landing on a definition element scrolled into view; Ctrl+K opening the dialog and the real index answering a query from every mount depth; the theme toggle changing what is painted; the archive notice, its dismissal across a reload, and the version picker; a monotonicity guard that fails any layout element visible only in a middle band of widths; the CV portrait decoding and its header laid out as a row; and axe on every page class of every theme.

Everything theme-sensitive runs across every built-in theme.

The suite needs Chromium through playwright-go and Pagefind. Each missing dependency skips the tests that need it, naming what to install; `internal/e2e/doc.go` carries the one-time setup.

## Important config fields

- `versions` (required, unless `unversioned`): array of `{version}` objects -- controls multi-version builds
- `unversioned`: `true` declares the project has no public version; replaces `versions`, refused alongside `source`
- `locales` (required): array of `{code, label, default}` objects -- controls localization
- `unified`: optional, for monorepo docs-site projects -- lists constituent projects
- `gen_data`: optional sandboxed script execution config
- `root_files`: templates that generate root-level files (e.g. CLAUDE.md, README.md)
- `deploy`: Cloudflare Pages or GitHub Pages provider config

## Architecture

:-: list-modules path="internal/"
