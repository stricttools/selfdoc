# A VS Code extension for live site preview, and interactive site design

## Context

selfdoc builds a documentation site from Markdown templates under
`.strictmetadata/docs/` whose directives (`:-:`, `:<:`, `:>:`, with `:=:` and
`:::` sections) are resolved from source code at build time: `ref`,
`table-schema`, `code-help`, `table-commands`, `code-test`, and the rest of
the built-in directive table, plus custom Python directives declared in
`selfdoc.json`. It also generates `README.md` and `.claude/CLAUDE.md` from
underscore-prefixed templates listed in `root_files`, and it hosts a blog.

The authoring loop is: edit a template, run `selfdoc build`, and look at the
result through `selfdoc serve`, whose live reload watches the output tree and
reloads when the build changes it. Directive errors, lint findings, and
spelling show up in `selfdoc check`, in the terminal.

The project already has most of what an editor integration needs, for blog
posts: the local authoring app in `internal/blog/editor` renders an unsaved
buffer through `render.Post` (the publish renderer, handed an in-memory
buffer, writing nothing), pushes previews over server-sent events, and
analyzes an unsaved buffer for spelling and lint findings with the same
engines the check runs, converting line and column coordinates to buffer
offsets. Documentation pages have no such path: `page.GenerateHTML` renders
pages inside the build, and `serve` serves what the build wrote.

Most documentation work, by humans and by agents, happens in an editor. An
extension brings the preview and the check's findings to where the template
is being written, and a design surface can make the site's look and
structure (theme, branding, navigation, landing page) editable with the
result visible immediately.

## Problem

- No preview of a documentation page, a root-file template, or a post inside
  the editor; seeing a change means a full build and a browser.
- Directive mistakes (a `path` that resolves to nothing, a misspelled
  directive, a missing attribute) surface only in `selfdoc check` or a build
  failure, with no location in the editor.
- Configuration choices that change the site's appearance (`theme`,
  `branding`, `search`, `code_icons`, `line_numbers`, `page_nav`,
  `page_progress`, and the landing page) are edited as JSON keys whose effect
  is invisible until a rebuild.

## Proposal

A VS Code extension (TypeScript) and a server hosted by the selfdoc binary
(a new command whose name is to be chosen, declared through strictcli),
reusing selfdoc's own packages so that what the editor shows is what the
build produces. No selfdoc logic lives in TypeScript.

### Site preview (the core of this todo)

1. **Page preview webview**: beside a template open in the editor, render
   that page from the unsaved buffer through the same page pipeline the
   build uses (directive resolution through `resolver`, the HTML pass,
   `page.GenerateHTML` or a single-page entry point extracted from it, the
   configured theme), and update it on each change. The principle the blog
   editor states holds here: the preview uses the production renderer, never
   a second one, and asking for a preview writes nothing.
2. **Scroll and selection sync** between the template and the preview, using
   source line markers carried through the Markdown-to-HTML pass.
3. **Root-file preview**: for `_README.md` and `_CLAUDE.md` templates, show
   the generated `README.md` or `.claude/CLAUDE.md` text and its diff against
   the committed file.
4. **Post preview**: reuse `render.Post` for posts, so the extension and the
   authoring app share one path.
5. **Whole-site preview** (optional): open the output of `selfdoc serve` in a
   webview or the built-in Simple Browser, for navigation across pages. This
   shows built output, not unsaved buffers, and is labeled as such.

### Editor features (Language Server Protocol)

- **Diagnostics** on templates: directive scanning and resolution errors,
  the lints `check` runs at each lint's configured option value, and
  spelling against the project vocabulary, all from the engines `check` owns.
  The directive scanner already publishes a column for each directive.
- **Completion** of directive names from the directive registry (built-in and
  those declared in `selfdoc.json`), attribute names, and `path` values
  (packages, modules, and symbols the configured extractors know).
- **Hover** on a directive: its description from the registry and the
  Markdown it resolves to.
- **Go to definition** from a directive's `path` to the source file and
  symbol it extracts from.
- **Diagnostics on `selfdoc.json`** from the declarative configuration
  schema in `internal/config/schema.go`, and on page frontmatter and
  `.strictmetadata/options/*.toml` from the strictspec validators selfdoc
  already uses for them, so the editor and the tool refuse the same
  documents.
- **Code lenses** on a template: "preview", and the page's documentation
  coverage when the page documents a package.

### Interactive design (second stage)

A design panel for the site-wide settings, each control mapped to a
declared configuration key and written back to `selfdoc.json` with minimal
edits (keeping key order and formatting), with the page preview re-rendering
on every change:

- theme selection among the themes selfdoc ships, and light and dark
  appearance side by side;
- branding and the landing page (hero, tagline, feature cards) edited in
  place in the preview, with the edit written to the configuration or
  template that owns the text;
- navigation: page order and groups as a tree view, reordered by drag and
  drop, written back to whatever declares the order;
- toggles for `search`, `code_icons`, `line_numbers`, `run_button`,
  `page_nav`, and `page_progress`;
- WCAG contrast results from the existing contrast validation shown next to
  color choices, as the check reports them.

Every control is generated from the configuration schema, so a key added to
selfdoc appears in the panel without extension changes, and the panel never
offers a value the schema refuses.

## Solutions with pros and cons

1. **LSP server plus custom preview methods in the selfdoc binary**
   (proposed).
   - Pro: unsaved buffers, one renderer, standard editor features, and the
     server can serve other LSP clients.
   - Con: a single-page, in-memory rendering entry point for documentation
     pages has to be extracted from the build without creating a second
     pipeline; directive resolution for some directives runs extractors over
     the source tree, so caching and invalidation on source changes need
     design.
2. **Wrap `selfdoc serve` in a webview**, rebuilding on save.
   - Pro: nearly no new Go code.
   - Con: no unsaved-buffer preview, a full build per save, and no editor
     diagnostics; a preview of saved state only.
3. **Reuse the blog authoring app's HTTP server** and extend it to pages.
   - Pro: preview over server-sent events and buffer analysis already exist.
   - Con: an HTTP server on loopback instead of the editor's stdio channel;
     webviews need port mapping and a content security policy; and it would
     grow the authoring app into a second editor protocol. Its analysis and
     preview code should be shared with the extension's server, not
     duplicated.

Solution 1, sharing code with the authoring app, is the most correct.

## Distribution

- The extension is TypeScript; the server is the Go selfdoc binary. The
  extension needs a matching binary: per-platform extension packages
  bundling it, or the binary on `PATH` with a version check that refuses a
  mismatch, naming both versions.
- Publish to both the **VS Code Marketplace** (`vsce publish`) and
  **Open VSX** (`ovsx publish`), so VS Code and its derivatives (VSCodium,
  Cursor, and others that install from Open VSX) all get it.
- The project releases through rlsbl, which has no VS Code extension
  publishing target yet; that path is to be designed.
- The extension name, publisher ID, and package name are to be chosen.

## Affected files and new components

- `internal/cli/`: the server command.
- New Go package for the server: document store over unsaved buffers,
  preview and analysis handlers, and LSP handlers; tests over an in-memory
  JSON-RPC pipe.
- `internal/page` and `internal/build`: a single-page, in-memory rendering
  entry point shared by the build and the server.
- `internal/html`: source line markers for scroll sync.
- `internal/blog/editor`: its analysis and preview logic moved or exposed for
  sharing with the server.
- `internal/directives` and `internal/resolver`: completion and hover data
  (directive registry, attribute lists, resolvable paths).
- `internal/config`: the configuration schema exposed for generated design
  controls, and minimal-edit write-back for `selfdoc.json`.
- New: the TypeScript extension (manifest, client, webviews, build step), in
  this repository or a separate one, to be decided.
- `.strictmetadata/docs/`: a page for the extension once it exists.

## Effort estimate

- Single-page in-memory rendering entry point and the page preview webview
  with live update: three to five days.
- Scroll sync and root-file preview: two days.
- Language server diagnostics, completion, hover, and definition: four to
  six days.
- Extension shell, binary discovery, and both registries: one to two days.
- Interactive design panel with schema-driven controls and write-back: four
  to six days.
