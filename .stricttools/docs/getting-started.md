+++
title = "Getting Started"
description = "Install selfdoc and generate your first documentation site in minutes. Covers installation, project setup, writing directives, and local development."
nav_group = "Getting Started"
nav_order = 10
+++

# Getting Started

This guide walks you through installing selfdoc, setting up a project, writing your first directive, and previewing your documentation site locally.

## Installation

selfdoc is a single Go binary. Stylesheets, scripts, themes, the directive catalogue and the word list are compiled into it, so there is no runtime to install, nothing to compile at install time, and no platform-specific setup on Linux, macOS or Windows.

Install with the Go toolchain:

```bash
go install github.com/stricttools/selfdoc@v0
```

On a machine with no Go toolchain, take the archive for your platform from the [latest GitHub Release](https://github.com/stricttools/selfdoc/releases/latest). Every release publishes prebuilt binaries for Linux, macOS and Windows on amd64 and arm64; unpack the archive and put `selfdoc` somewhere on your `PATH`.

Two dependencies are optional, and each is needed only by the feature that uses it: [Pagefind](https://pagefind.app/) for the search index, and `python3` for custom directives. Every built-in extractor parses in process -- documenting a Python project needs no interpreter.

Verify the installation:

```bash
selfdoc --version
```

## Initialize a Project

Navigate to the root of an existing codebase that you want to document. The `init` command detects your project language, creates the configuration file, and scaffolds a starter documentation template:

```bash
selfdoc init --base-url https://myproject.pages.dev
```

`--base-url` is required: it is the address the site will be served from, selfdoc cannot infer it, and every canonical link, sitemap entry and feed URL is built from it.

This does four things:

1. **Detects your project language** from manifest files (`pyproject.toml` for Python, `go.mod` for Go, `tsconfig.json` or `package.json` for TypeScript/JavaScript).
2. **Creates `selfdoc.json`** -- the base URL you passed, source directories, docs path, output path, the version declaration and the `locales` array with a single entry. The emitted file builds as-is; nothing has to be added by hand. The version declaration is `version` plus a one-entry `versions` array, at the version your own manifest (`pyproject.toml`, `package.json` or `VERSION`) states, or `0.0.0` for a new project that states none yet.
3. **Grants selfdoc its directories**: writes the one-line ownership manifest (`owner = "selfdoc"`) of `.stricttools/docs/`, `.stricttools/docs-state/` and `.stricttools/docs-cache/`, and the derived `.stricttools/.gitignore`. A manifest already naming selfdoc is kept; one naming another tool stops `init` before it writes anything.
4. **Creates `.stricttools/docs/index.md`** with a starter template that includes a `ref` directive pointing at your main module.

A project with no detectable language is a **codeless project** -- a portfolio or personal site that is nothing but Markdown pages. `init` initializes it too: the config gets no `source` key and the starter page gets no `ref` directive, while the version declaration is the same versioned form at `0.0.0`, so adding a `source` entry later is the only edit code needs. Directives that extract from source code are a hard error in such a project until it declares a `source` entry. (A project that truly publishes no artifact can declare `"unversioned": true` by hand instead of `versions`; `init` never writes it, and it is refused once the project declares `source`.)

The `init` command also auto-commits the generated files unless you pass `--no-auto-commit`.

## Project Structure

After initialization, your project will have a `selfdoc.json` configuration file at the project root and a `.stricttools/docs/` directory containing a starter Markdown template with a `ref` directive already pointing at your main module. Your existing source code is not modified:

```
your-project/
  selfdoc.json        # Configuration file
  .stricttools/docs/
    index.md          # Starter template
  src/ or lib/        # Your source code (unchanged)
```

### selfdoc.json

The configuration file controls how selfdoc finds your source code, where to look for documentation templates, and where to write the generated HTML output. `source`, `base_url`, `versions` and `locales` are required; everything else has defaults that work for most projects. Each source entry carries its own language, so one project can mix them:

```json
{
  "source": [{"path": "mypackage/", "language": "python"}],
  "base_url": "https://my-project.example.com",
  "versions": [{"version": "1.0.0"}],
  "locales": [{"code": "en", "label": "English", "default": true}],
  "docs": ".stricttools/docs/",
  "output": ".stricttools/docs-cache/build/"
}
```

| Field | Required | Description |
| ----- | -------- | ----------- |
| `source` | yes | List of `{path, language}` objects naming the source directories to extract from. There is no top-level `language` key -- the language belongs to the entry |
| `base_url` | yes | The address the site is served from; every canonical link, sitemap entry and feed URL is built from it |
| `versions` | yes | Array of `{version}` objects, unless the project declares `"unversioned": true` |
| `locales` | yes | Array of `{code, label, default}` objects |
| `docs` | no | Directory containing Markdown templates (default: `.stricttools/docs/`) |
| `output` | no | Directory for generated HTML output (default: `.stricttools/docs-cache/build/`) |
| `deploy` | no | Deploy provider config -- see the deployment docs |
| `directives` | no | Map of custom directive names to script paths |

### The docs directory

Every `.md` file in `.stricttools/docs/` is a documentation page that selfdoc will process during the build. Each file starts with a frontmatter block -- TOML between `+++` fences -- carrying at least a title and description for SEO metadata:

```markdown
+++
title = "API Reference"
description = "Complete API reference for mypackage."
nav_order = 20
+++

# API Reference

Your content here...
```

- `title` -- used in the sidebar navigation and HTML `<title>`
- `description` -- used in meta tags for SEO, and printed above the H1 on most pages
- `nav_order` -- controls sidebar sort order (lower numbers appear first)

Every key a block may carry is declared in a schema, and one it does not declare
is refused rather than ignored. The frontmatter guide carries the whole registry
and the converter that rewrites a block written in the retired `---` dialect.

#### Where the description is printed on the page

`description` always goes into the `<meta name="description">` tag. Whether it
is *also* printed as a summary block above the H1 is decided by what the page
is, never by what it says:

| Page | Summary block above the H1 |
|---|---|
| `index.md` (the home page) | No -- the home page opens with its own lead paragraph |
| A page declaring `type = "post"` | No -- a post opens with its own lead paragraph |
| Every other page | Yes |

Printing it on a home page or a post says the same sentence twice inside one
viewport. Everywhere else the block is a useful summary of what the page
covers, so it stays. Nothing compares the description against the first
paragraph: a layout that changed with the wording would be a layout nobody
could predict.

Files are organized into a flat structure. The filename (minus `.md`) becomes the URL slug: `.stricttools/docs/api-reference.md` becomes `/api-reference/`.

## Writing Your First Directive

Directives are the core feature of selfdoc -- inline markers in your Markdown templates that get replaced with content extracted from your source code at build time. They keep your documentation in sync with the implementation automatically.

Open `.stricttools/docs/index.md` (created by `selfdoc init`) and you will see something like:

```markdown
+++
title = "myproject"
description = "Documentation for myproject"
+++

# myproject

Welcome to the myproject documentation.

## API Reference

:-: ref path="myproject"
```

The line `:-: ref path="myproject"` is a self-closing directive. At build time, selfdoc will:

1. Look up the `myproject` module in your source directories.
2. Extract its docstring, public functions, classes, and their signatures.
3. Replace the directive line with formatted Markdown content.

### Adding more directives

selfdoc ships with a catalogue of built-in directives for common documentation patterns, from extracting module-level API references to rendering configuration schemas as tables. Each directive uses a `path` attribute to identify the source file or module, and some accept additional attributes like `target` for specific symbols. Here are examples of each:

**Module reference** -- extract docstrings and public API:

```markdown
:-: ref path="mypackage.core"
```

**Schema table** -- render a dataclass or config structure as a table:

```markdown
:-: table-schema path="mypackage.config" target="Settings"
```

**Test code** -- embed a test function's source:

```markdown
:-: code-test path="tests/test_core.py" target="test_basic_usage"
```

**CLI help** -- extract CLI usage and flags:

```markdown
:-: code-help path="mypackage.cli"
```

**Config table** -- render a JSON or TOML config file as a key-value table:

```markdown
:-: table-config path="selfdoc.json"
```

Directives inside fenced code blocks (triple backticks) are ignored, so you can safely document directive syntax without triggering resolution.

## Building

Once you have written your Markdown templates with directive markers, generate the complete static HTML site with a single command. The build resolves all directives, converts Markdown to HTML, generates navigation and search indexes, and writes everything to the output directory:

```bash
selfdoc build
```

This resolves all directives in your `.stricttools/docs/` templates and writes HTML output to `.stricttools/docs-cache/build/` (or wherever `output` is configured in `selfdoc.json`).

The build output includes everything needed for a complete static site:

- HTML pages with sidebar navigation
- Syntax-highlighted code blocks
- Full-text search
- Dark mode with system preference detection
- XML sitemap and Atom feed
- Structured data (JSON-LD) for search engines
- Print stylesheet for PDF export

After building, you will see a summary like:

```
Built 5 file(s) to .stricttools/docs-cache/build/
```

Any SEO warnings or directive errors are printed after the build summary. Errors cause a non-zero exit code; warnings are informational.

## Local Development

Preview your documentation site locally with automatic live reload powered by Server-Sent Events. The development server watches the output directory for file changes and pushes reload notifications to every connected browser tab, so your pages update instantly after each build without manual refreshing or browser extensions.

:<: callout-tip
:=:
::: Run `selfdoc serve` alongside `selfdoc build` for a live preview workflow -- the browser reloads automatically via Server-Sent Events whenever the output changes.
:>:

```bash
selfdoc serve
```

This starts a local HTTP server at `http://localhost:8000/` and watches the output directory for changes. When files change, connected browsers reload automatically via Server-Sent Events (SSE).

To use a different port:

```bash
selfdoc serve --port 3000
```

The typical development workflow is:

1. Run `selfdoc serve` in one terminal.
2. Edit your Markdown templates in `.stricttools/docs/`.
3. Run `selfdoc build` in another terminal.
4. The browser reloads automatically with your changes.

Press `Ctrl+C` to stop the server.

## Checking Your Docs

Validate that all directives resolve correctly, review documentation coverage against your public API surface, and catch SEO issues before publishing. The check command runs three categories of analysis and reports results with file locations and actionable diagnostic codes:

```bash
selfdoc check
```

This runs three checks:

1. **Directive validation** -- attempts to resolve every directive in your templates and reports any that fail (missing modules, invalid paths, etc.).
2. **Coverage analysis** -- counts how many of your source's public symbols are referenced by directives versus how many exist. `coverage_threshold` in `selfdoc.json` is the fraction that must be documented for the check to pass.
3. **SEO linting** -- checks frontmatter, heading structure, meta descriptions, and other best practices.

Example output:

```
Directive results:
  .stricttools/docs/index.md:12  ref path="mypackage"        OK
  .stricttools/docs/api.md:8     ref path="mypackage.core"    OK
  .stricttools/docs/api.md:20    table-schema path="..."      OK

Coverage: 15/23 public symbols documented (65%)

SEO: 0 warnings, 0 errors
```

To suppress specific SEO warnings, pass `--ignore` with a comma-separated list of codes:

```bash
selfdoc check --ignore SEO007,SEO008
```

For machine-readable output (useful in CI):

```bash
selfdoc check --json
```

## Next Steps

Now that you have a working documentation site with live directives, full-text search, and dark mode, explore these topics to learn about advanced configuration, theming, deployment to production hosting, and the full directive reference:

- **[Directives Reference](../directives/)** -- complete reference for all built-in directives, block syntax, and custom directives.
- **[Configuration](../configuration/)** -- all `selfdoc.json` options including deploy providers, the coverage threshold, and lint suppression.
- **[CLI Reference](../cli-index/)** -- detailed documentation for every CLI command and flag.
