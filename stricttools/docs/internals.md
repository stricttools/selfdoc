+++
title = "Internals"
description = "Developer internals: tokenizer design, extractor protocol, resolver dispatch, rendering pipeline, and lint system architecture."
nav_group = "Contributing"
nav_order = 1
+++

# Internals

This page documents selfdoc's internal design for contributors and anyone extending or debugging the system. For the user-facing overview, see [Architecture](../architecture/).

selfdoc is one Go module, `github.com/stricttools/selfdoc`, whose root package is the binary's entry point, with every engine package under `internal/`. Package names below are those import paths.

## Tokenizer

**Package:** `internal/tokenizer` -- a standalone block tokenizer that splits Markdown source into a flat slice of typed block tokens. It imports nothing of selfdoc's and is designed for reuse outside the project. The tokenizer guarantees that every source line belongs to one token, with no gaps and no overlaps.

### Token types

Every token embeds a `Span` carrying 1-based, inclusive `StartLine` and `EndLine` numbers, and satisfies the `Token` interface through it. Tokens that carry prose a reader sees also satisfy `TextBearingToken`, which is how the lint rules ask a document for its readable text without hard-coding a list of types.

| Token | Represents |
|-------|-----------|
| `Heading` | ATX heading (`#` through `######`) with level and text |
| `CodeBlock` | Fenced code block with language, content lines, and annotations |
| `Table` | Pipe-delimited table rows |
| `UnorderedList` | Items starting with `-` or `*` |
| `OrderedList` | Items starting with `1.`, `2.`, etc. |
| `Blockquote` | `>` prefixed lines, with optional admonition type |
| `DefinitionList` | Term/definition pairs (DL/DT/DD) |
| `ThematicBreak` | `---`, `***`, or `___` |
| `BlankLine` | Empty separator lines |
| `Directive` | The legacy `:::name arg` / `:::` syntax (tokenizer-level) |
| `Paragraph` | Everything else -- contiguous non-blank lines |

### Design rationale

The tokenizer is its own package rather than parsing logic inline in the renderer, and the separation is deliberate. There are two reasons it is factored out:

- **Dual consumers:** both the rendering pipeline and the lint system operate on tokens. Lint needs line numbers for diagnostics; the renderer needs structured data for dispatch.
- **Testability:** tokenization can be tested in isolation without invoking HTML generation or directive resolution.

## Rendering Pipeline

**Package:** `internal/html`, function `MdToHTML` -- converts resolved Markdown to the HTML a page's body carries, through tokenization, block-level dispatch rendering, and post-processing for cross-block patterns like code tabs and API entry cards. The chrome that wraps a body is built on top of this package rather than inside it.

### Phase 1: Tokenize and render blocks

`MdToHTML` calls the tokenizer, assigns heading anchors in one scan (`AssignHeadingAnchors` is the one place a heading's element id is decided, so the renderer and the search index can never disagree), then dispatches each token to `renderBlock`, which pattern-matches on token type and delegates to a specialized renderer.

### Phase 2: Post-processors

After block rendering produces a joined HTML string, post-processors scan for cross-block patterns that cannot be detected at the individual token level. Each one modifies the HTML in place and passes the result to the next:

- **Code tabs** (`groupCodeTabs`): consecutive code blocks with different languages become a tabbed interface
- **Step guides** (`applyStepGuides`): ordered lists after headings containing "step", "guide", or "tutorial" get `class="steps"`. Opt out per page with `auto_steps: false` or globally with `auto_detect.steps`
- **API entries** (`wrapAPIEntries`): h3/h4 + code block + description are wrapped in `<div class="api-entry">` cards. Off by default; enable with `auto_detect.api_entries`, override per page with `auto_api`
- **Definition ids** (`assignDefinitionIDs`): every author-declared `<dfn>` gets a `term-<slug>` id, deduplicated against every id already on the page, so the glossary can link to the definition site
- **LCP promotion**: the first image is promoted from `loading="lazy"` to `fetchpriority="high" loading="eager"`

### Phase 3: Page assembly

`internal/page`, entry point `GenerateHTML`, wraps each converted body in a full document shell: sidebar navigation, topbar, table of contents, breadcrumbs, version and locale pickers, the superseded-version notice, the footer, the search surface, the Pagefind filter attributes, and every SEO tag in the head including the JSON-LD documents. It runs once per locale-and-version pass, collects the terms the pages declared, synthesizes the glossary page from them, and returns a map keyed by each page's output key. Every relative reference in that shell comes from the page's address (see `internal/address`), which turns the mount coordinates plus the page path into the output key and the hops back to the mount root, the version-free mount, and the output root.

### Syntax highlighting

Code blocks are highlighted at build time by [chroma](https://github.com/alecthomas/chroma). The stylesheet that paints them is generated as one set of custom properties defined three times over -- the default scheme, an explicitly chosen dark one, and the system fallback for a reader who has recorded no preference -- referenced by one scheme-agnostic set of rules. A token's colour is therefore a value in the token layer, and the two schemes cannot drift apart rule by rule.

### Why post-processors operate on HTML strings

Post-processors detect cross-block patterns (e.g., "three consecutive code blocks" or "a heading followed by an ordered list"). The token stream is flat, so matching on rendered output is a natural fit. This keeps the renderer simple (one token in, one HTML fragment out) and moves heuristic logic to an explicit post-processing phase.

## Extractor Protocol

**Package:** `internal/extractors` -- defines the `Extractor` interface every language extractor implements to participate in directive resolution, coverage analysis, quality scoring and language auto-detection.

```go
type Extractor interface {
	Name() string
	Detect(dir string) bool
	ResolvePath(pathArg string, sourcePaths []string, baseDir string) string
	Extract(directiveName string, attrs map[string]string, body []string, sourcePaths []string, baseDir string) (string, error)
	FileExtensions() []string
	PublicSymbols(file string) ([]string, error)
	SymbolDetails(file, symbol string) (*SymbolDetails, error)
	ModuleDocstring(path string) (string, error)
}
```

The methods split into three groups. `Name` and `Detect` identify the language. `Extract` resolves one directive into Markdown. The rest answer the discovery questions the coverage, quality and staleness measurements ask.

`Extract` returns an error marker as its Markdown, and a nil error, when a directive cannot be resolved -- a missing file, a syntax error, an unknown directive name -- so one bad directive degrades one region of one page instead of failing the build. The error return is for a broken toolchain. Three of the discovery methods can return an error where an empty result would be a lie: a parser that will not run at all is a broken installation, not a file with no symbols in it. A file the parser rejects is a different answer -- that is a property of the file, and the caller renders a marker for it.

### Implementations

Each language package registers its factory with the registry at init, so the set of extractors linked into the binary is what `Registered` reports.

| Extractor | Language | Parsing strategy |
|-----------|----------|-----------------|
| `internal/extractors/python` | Python | A real parse, in process, over the Python grammar's tables embedded in the binary |
| `internal/extractors/golang` | Go | Pattern-based scanner over the package's files |
| `internal/extractors/typescript` | TypeScript, JavaScript | Pattern-based -- matches exports, interfaces, type aliases |
| `internal/extractors/svelte` | Svelte | Script-block exports and component props |
| `internal/extractors/zig` | Zig | Public declarations and doc comments |
| `internal/extractors/swift` | Swift | Public declarations and doc comments |
| `internal/extractors/kotlin` | Kotlin | Public declarations and KDoc |
| `internal/extractors/dart` | Dart | Public declarations and doc comments; nothing from the Dart toolchain is required |
| `internal/extractors/sql` | PostgreSQL DDL | Tables, views, types and `COMMENT ON` statements |

Python is the one extractor that parses rather than scans, because Python's grammar makes pattern matching unreliable (decorators, multiline signatures, nested classes). The others have simpler export conventions that a scanner handles reliably. The parse is pure Go and reads the grammar's tables out of the binary, so documenting a Python project needs no interpreter on the machine and the build needs no cgo. What the pages read like still follows CPython's own `ast`: the tree is reshaped into what `ast` reports, and the rendering of an annotation, a default value or a base class reproduces `ast.unparse`, because every Python reference page selfdoc has ever produced was written against it.

### Language detection

`DetectLanguage` probes for language-specific marker files in a directory in a fixed priority order: Python, Go, Svelte, TypeScript, Zig, Swift, Kotlin, Dart. Svelte comes before TypeScript because a Svelte project also has TypeScript files and a `tsconfig.json`, so TypeScript would win every Svelte project. SQL is absent on purpose -- a SQL source path is declared in `selfdoc.json` and never auto-detected, because a `.sql` file in a repository says nothing about what the repository is. `DetectLanguages` answers with every language it finds, which is what a polyglot repository needs. This powers `selfdoc init`.

## Resolver Dispatch Chain

**Package:** `internal/resolver` -- a resolver is built for a project and called once per directive, processing it through a dispatch chain and stopping at the first match:

1. **Content directives** -- callouts, `list-glossary`, `list-tree`, `table-dep`, `list-modules`, `table-commands`, `table-directives`, `table-config-schema`, `table-endpoint`, `list-crawlers`, `var` and `cv`. These are language-agnostic and need no source access.

2. **Custom directives** -- if `selfdoc.json` declares a `"directives"` map, the named script is run out of process. An embedded driver is handed to `python3` with the script's path as its one argument and a JSON object (`attrs`, `config`, `body`, `base_dir`) on standard input; what the script prints on standard output replaces the directive. The same config key also accepts a directive compiled into the binary, which is how an assembled site's home project renders from the assembly's manifests -- state a build is handed and no config document can hold.

3. **Language extractors** -- for a multi-language project the resolver decides which source entry owns the path the directive names. A path that resolves in more than one language is an ambiguity error rather than a silent pick.

A path that resolves nowhere leaves an inline marker (`> *[selfdoc: ...]*`) in the rendered output. A custom directive that fails does not: a script that will not load, one with no callable `resolve`, one that raises, and a machine with no `python3` are each a hard error naming the directive and the script. A page that says "custom directive failed" where its API reference belongs is not a page anybody wanted published, and the note was as easy to miss as any other paragraph.

The driver runs as a declared read through the effects handle, so `--dry-run` resolves directives like any other run.

### Built-in directive catalog

**Package:** `internal/catalog` -- the catalogue is a declarative TOML document embedded in the binary, validated by a [strictspec](https://github.com/smm-h/strictspec)-generated validator that is generated into the package that loads it. Nothing else can bind an unvalidated document. Directives come in two tiers:

- **Core directives** -- shipped and functional. The [directives reference](../directives/) renders the table straight out of the catalogue.
- **Future directives** -- declared, parse-valid, not yet implemented, organized by prefix (`table-*`, `code-*`, `list-*`, `callout-*`, `prose-*`). Declaring them means the parser accepts them without error, so authors can mark intent before extraction logic exists.

## Lint System

**Package:** `internal/check` -- validates documentation quality across three dimensions: directive correctness, API coverage measurement, and content and SEO best practices. The lint system operates on tokens rather than raw Markdown text, which lets it distinguish content inside code blocks from content in the page body and produce accurate line numbers in diagnostics.

The registry of codes is `internal/lints/lints.toml`, another embedded declarative document with its own strictspec-generated validator. Each entry declares the code, its severity and its message, so a rule's severity is a fact of the document rather than a constant somewhere in the checker.

### Directive validation

For every directive in every template, the checker performs full resolution using the project's extractors. This catches broken module paths, missing target symbols, malformed attributes, and custom directive script errors before the documentation reaches production. Each failure includes the file path and line number.

### Coverage analysis

Measures how many public symbols in your source code are referenced by at least one directive across all documentation templates. It uses each extractor's `PublicSymbols` to enumerate exports and cross-references them against the symbols named in resolved directive output. `coverage_threshold` in `selfdoc.json` is the fraction that must be documented for the check to pass; it defaults to `1.0`.

### Rule families

Codes are grouped by prefix, and a family's members share a subject:

| Prefix | Subject |
|--------|---------|
| `SEO` | Metadata quality, heading structure, alt text, anchor text |
| `STALE` | A page's content changed but its description did not |
| `DRIFT` | A generated description no longer matches its source |
| `DQ` | Documentation quality signals on a symbol |
| `XREF` | Cross-references that name something absent |
| `PARAM`, `RETURN` | A symbol's parameters and return value going undocumented |
| `EXAMPLE` | Code examples that do not parse or do not run |
| `CLI` | CLI reference pages against the CLI's own schema |
| `LANG` | Source entries and their declared languages |
| `SEARCH` | The search index and its configuration |
| `VER` | Version-bearing generated content |
| `SPELL` | Spelling, against the vendored word list and the project's accepted vocabulary |
| `VOCAB` | The project's vocabulary files, and rejected terms in page prose |
| `POST` | Blog post frontmatter and layout |
| `LINK` | Internal links that resolve nowhere |
| `UNIFIED` | The unified multi-project build |

See the [SEO and lint reference](../seo/) for every code with its severity and message.

### Why tokens, not raw Markdown

The lint system operates on tokens rather than raw text, getting pre-parsed structure for free. It can distinguish "an image inside a code block" (skip SEO003) from "an image in body text" (flag it).

### Staleness detection

When a page's content hash differs from the recorded baseline but its description hash is unchanged, the checker flags it -- the common case where content changes but the frontmatter description still describes the old version. `selfdoc baseline accept` records the new hashes once the description has been reviewed.
