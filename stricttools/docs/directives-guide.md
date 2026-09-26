+++
title = "Directives Guide"
description = "Guide to selfdoc directives: the marker types, every built-in directive with usage examples, and creating custom directives."
nav_group = "Guides"
nav_order = 31
+++

# Directives Guide

Directives are selfdoc's mechanism for embedding live, code-extracted content in Markdown templates. Instead of manually copying API signatures, config schemas, or CLI help text into your documentation, you place a directive marker in your template. At build time, selfdoc resolves each directive by reading your source code, config files, or project metadata, and replaces the marker with generated Markdown content. The result: documentation that stays in sync with the implementation automatically.

## Marker Types

Selfdoc's directive syntax uses 6 marker types. Each is a 3-character token surrounded by colons, designed to be visually distinctive in plain Markdown and easy to scan.

| Marker | Name | Purpose |
|--------|------|---------|
| `:-:` | One-liner | Self-closing directive (no body) |
| `:<:` | Block open | Opens a block directive |
| `:@:` | Attribute | Additional key-value attribute inside a block |
| `:=:` | Body separator | Separates attributes from body content |
| `:::` | Body line | A line of body content inside a block |
| `:>:` | Block close | Closes a block directive |

Directives inside fenced code blocks (triple backticks or triple tildes) are ignored by the parser. You can safely show directive syntax in code examples without triggering resolution.

## Directive Forms

### One-liner (self-closing)

The simplest form. A single line with the `:-:` marker, the directive name, and optional `key="value"` attributes:

```markdown
:-: ref path="mymodule"
```

One-liners are used when the directive needs only attributes and no body content. Most directives work as one-liners.

### Block directive

Block directives span multiple lines and can carry body content. They open with `:<:`, close with `:>:`, and put `:=:` between the attributes and the body -- even when there are no attributes:

```markdown
:<: callout-note
:=:
::: This is important information.
::: It can span multiple lines.
:>:
```

A `:::` body line that appears before any `:=:` is a hard error naming the line: without the separator the parser is still reading attributes, and a body line is not one.

### Block with attributes and body separator

For directives that need both extra attributes and body content, use `:@:` for additional attributes and `:=:` to separate attributes from the body:

```markdown
:<: list-glossary
:@: style="compact"
:=:
::: **API**: Application Programming Interface
::: **CLI**: Command-Line Interface
:>:
```

The `:@:` lines add key-value pairs to the directive's attribute dict. The `:=:` marker signals the transition from attributes to body lines. A block with no `:=:` carries no body, so every line between `:<:` and `:>:` has to be a `:@:` attribute line.

### Block with no body

A block can close immediately after attributes (no body content):

```markdown
:<: callout-tip
:@: title="Performance"
:>:
```

### Inline directives

The `:-:` marker can also appear inline within a line of text. This is useful for interpolating short values like project metadata into running prose:

```markdown
The current version is :-: var key="project.version" and the language is :-: var key="project.language".
```

Inline directives must produce single-line output. If an inline directive returns multi-line content, a runtime error is raised. Inline directives inside backtick code spans are ignored (so `` `:-: var key="x"` `` is treated as literal text, not a directive).

## Attribute Syntax

All directive attributes use `key="value"` syntax with double quotes. Keys may contain word characters and hyphens (matching `[\w-]+`). Multiple attributes are space-separated:

```markdown
:-: table-schema path="models.py" target="User" exclude="internal_field"
```

Attributes are parsed into a `dict[str, str]` and passed to the resolver. Each built-in directive declares which attributes are required and which are optional. Using an unknown attribute or omitting a required one produces a hard error at build time.

## Built-in Directives

Selfdoc's built-in directives fall in two categories: **code extraction** directives that read source files, and **content** directives that generate content from project metadata or transform body text. The sections below cover each one; the [directive reference](../directives/) renders the shipped catalogue as a table.

### Code Extraction Directives

These directives read source files through language-specific extractors (Go, Python, TypeScript, Svelte, Zig, Dart, Kotlin, Swift and SQL). They all accept a `path` attribute pointing to a source file or module, and an optional `lang` attribute to disambiguate in multi-language projects.

#### `ref`

Extract a module's docstring, exported functions, and classes as a reference page.

```markdown
:-: ref path="selfdoc/config.py"
```

With a target to extract a specific class or function:

```markdown
:-: ref path="selfdoc/config.py" target="load_config"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Module path (file path or dotted module name) |
| `target` | no | Specific symbol (class/function) to extract |
| `lang` | no | Language hint for multi-language projects |

#### `table-schema`

Extract dataclass, struct, or type fields and render them as a Markdown table.

```markdown
:-: table-schema path="models.py" target="User"
```

Exclude specific fields:

```markdown
:-: table-schema path="config.json" exclude="internal_field, debug_opts"
```

When `path` points to a data file (JSON, TOML, JSONC) rather than a source file, the `exclude` attribute omits the listed top-level keys from the table. If an excluded key does not exist in the file, a hard error is produced.

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Source file or data file path |
| `target` | no | Specific type/class name to extract |
| `exclude` | no | Comma-separated keys to omit (data files only) |
| `lang` | no | Language hint for multi-language projects |

#### `code-test`

Embed test source code, either the whole file or a specific test function.

```markdown
:-: code-test path="tests/test_auth.py" target="test_login"
```

Without a target, the entire file is embedded:

```markdown
:-: code-test path="tests/test_config.py"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Path to the test file |
| `target` | no | Specific test function to extract |
| `lang` | no | Language hint for multi-language projects |

#### `code-help`

Extract CLI help/usage text and flag definitions from a source file.

```markdown
:-: code-help path="cli.py"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Path to the CLI source file |
| `lang` | no | Language hint for multi-language projects |

#### `table-config`

Render a configuration file (JSON, TOML, JSONC) as a key-value Markdown table.

```markdown
:-: table-config path="selfdoc.json"
```

Exclude verbose or irrelevant keys:

```markdown
:-: table-config path="selfdoc.json" exclude="versions, locales"
```

If any excluded key does not exist in the file, a hard error is produced (no silent skips).

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Path to the config file |
| `exclude` | no | Comma-separated top-level keys to omit |
| `lang` | no | Language hint for multi-language projects |

#### `prose-desc`

Extract a module or package docstring as prose text (without the function/class reference).

```markdown
:-: prose-desc path="selfdoc/build.py"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Module or package path |
| `lang` | no | Language hint for multi-language projects |

### Content Directives

Content directives are language-agnostic. They generate content from project metadata, transform body text, or render project structure information.

#### `callout-note`

Styled note callout block.

```markdown
:<: callout-note
:=:
::: Remember to run tests before committing.
:>:
```

Renders as a styled `<div class="callout callout-note">` with a "Note" title.

#### `callout-warning`

Styled warning callout block.

```markdown
:<: callout-warning
:=:
::: This operation cannot be undone.
:>:
```

#### `callout-tip`

Styled tip callout block.

```markdown
:<: callout-tip
:=:
::: Use tab completion for faster CLI usage.
:>:
```

#### `callout-danger`

Styled danger callout block.

```markdown
:<: callout-danger
:=:
::: Running this in production will delete all data.
:>:
```

#### `callout-important`

Styled important callout block.

```markdown
:<: callout-important
:=:
::: Do not skip this step during setup.
:>:
```

All five callout directives accept body content via the block syntax. The body lines are joined and rendered as paragraph text inside the callout container.

#### `list-glossary`

Definition list from `**Term**: Definition` formatted lines. Produces an HTML `<dl>` list.

```markdown
:<: list-glossary
:=:
::: **Directive**: A marker in a Markdown template that gets resolved at build time
::: **Extractor**: A language-specific module that reads source code
::: **Resolver**: The function that dispatches directives to extractors
:>:
```

Each body line should follow the `**Term**: Definition text` pattern. The `**` markers are stripped and the term/definition are split on the first `: ` separator.

#### `list-tree`

File/directory tree listing. Walks the given directory and produces a text tree inside a fenced code block.

```markdown
:-: list-tree path="src/"
```

Limit depth:

```markdown
:-: list-tree path="selfdoc/" depth="2"
```

Common directories like `__pycache__`, `.git`, `node_modules`, and `.egg-info` are automatically excluded.

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Directory to list |
| `depth` | no | Maximum depth to traverse |

#### `table-dep`

Dependencies table from `pyproject.toml`. Parses PEP 508 dependency specifiers and renders package names with version constraints.

```markdown
:-: table-dep path="pyproject.toml"
```

Produces a table with columns "Package" and "Version Constraint". Optional dependency groups are shown with bold group headers.

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Path to pyproject.toml |

#### `list-modules`

List source modules with file paths and docstring summaries. Groups modules by the language's natural unit: per-file for Python, per-package (directory) for Go, and per-file grouped by directory for TypeScript.

```markdown
:-: list-modules path="selfdoc/"
```

Force per-file listing regardless of language:

```markdown
:-: list-modules path="pkg/" files="true"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Source directory to scan |
| `files` | no | Set to `"true"` to force per-file listing |

#### `table-commands`

CLI command summary table from a strictcli schema. Reads `.strictcli/schema.json` (generated by running your CLI app with `--dump-schema`) and produces a table of commands with their descriptions.

```markdown
:-: table-commands
```

If the project has multiple schemas (e.g., in a monorepo), specify the schema directory:

```markdown
:-: table-commands schema-dir="packages/cli"
```

The schema is discovered automatically. If discovery finds no schema or is ambiguous, a descriptive error is produced.

| Attribute | Required | Description |
|-----------|----------|-------------|
| `schema-dir` | no | Directory containing `.strictcli/schema.json` |

#### `table-endpoint`

REST API endpoint table from an OpenAPI 3.x JSON spec. Renders endpoint documentation with path/query parameters, request body fields, and response schemas.

```markdown
:-: table-endpoint path="openapi.json"
```

Filter to specific endpoints or methods:

```markdown
:-: table-endpoint path="openapi.json" endpoint="/api/users" method="POST"
```

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | Path to the OpenAPI JSON spec |
| `endpoint` | no | Filter by endpoint path prefix |
| `method` | no | Filter by HTTP method |

#### `table-directives`

Table of all core built-in directives. Takes no attributes -- reads directly from selfdoc's internal catalog.

```markdown
:-: table-directives
```

Produces a two-column table with "Directive" and "Description".

#### `table-config-schema`

Configuration field reference table from selfdoc's own config schema. Takes no attributes.

```markdown
:-: table-config-schema
```

Produces a table with "Field", "Required", and "Description" columns for all non-internal `selfdoc.json` configuration fields.

#### `list-crawlers`

The crawlers selfdoc's generated `robots.txt` allows, one per bullet. Takes no attributes -- reads the same crawler policy the generators render, so a page documenting the policy cannot go out of step with the file.

```markdown
:-: list-crawlers
```

Produces a bullet list of user-agent names, in the order `robots.txt` names them. `*` is the wildcard stanza.

#### `table-lints`

Every lint code `selfdoc check` can emit, as a table. Takes no attributes -- reads the same lint registry the check runs from, so a page documenting the codes cannot go out of step with the ones selfdoc actually emits.

```markdown
:-: table-lints
```

Produces a table with "Code", "Severity" and "What it checks" columns, one row per registered code, in the registry's own documentation order (families grouped, not sorted alphabetically).

#### `var`

Interpolate a project metadata value. Produces a single string value, making it ideal for inline use.

```markdown
:-: var key="project.name"
```

Inline example:

```markdown
**:-: var key="project.name"** is written in :-: var key="project.language".
```

Supported keys:

| Key | Description |
|-----|-------------|
| `project.name` | Project name from pyproject.toml or package.json |
| `project.version` | Project version from pyproject.toml or package.json |
| `project.description` | Project description (from config or project file) |
| `project.language` | Source language(s) from selfdoc.json config |
| `topology.docs_url` | Constructed docs URL from topology config |
| `topology.posts_url` | Canonical blog base URL (`topology.posts_base`) |
| `topology.slug` | Project slug from topology config |

| Attribute | Required | Description |
|-----------|----------|-------------|
| `key` | yes | Metadata key to interpolate |

#### `cv`

Render a curriculum vitae declared as data. The page is a thin host: the whole body comes from a TOML document, so the CV has one source instead of one page and one set of structured data drifting apart.

```markdown
+++
title = "CV"
type = "cv"
description = "Curriculum vitae of ..."
+++

:-: cv path="stricttools/docs/cv.toml"
```

The document declares eight sections, all required and non-empty -- an absent one would render as a heading over nothing:

| Section | Shape | Holds |
|---------|-------|-------|
| `[identity]` | table | `name`, `headline`, `location`, `email`, `summary` (required); `photo`, `updated`, and `[[identity.profile]]` blocks of `label` + `url` |
| `[[skills]]` | blocks | `category`, `items` |
| `[[projects]]` | blocks | `name`, plus `notes` and/or `technologies` |
| `[[interests]]` | blocks | `title`, `body` |
| `[[education]]` | blocks | `degree`, `years`, `institute`, `location`; optional `institute_url`, `focus`, `thesis`, `course_url` |
| `[[experience]]` | blocks | `role`, `period`, `company`, `location`; optional `company_url`, `body` |
| `[[languages]]` | blocks | `name`, `level`; optional `url` |
| `[contact]` | table | `body` |

Prose fields (`summary`, an interest's `body`, an experience's `body`, `contact.body`) are Markdown and may carry links. Validation is strict in every direction: an unknown key, a missing or empty required field, a repeated skill category or project name, and a `format_version` other than `1` are each a hard error naming the declaration.

The page also emits a `Person`: the site's declared `author` -- name, url, `sameAs` -- carrying what the CV knows on top of it (`jobTitle`, `description`, `email`, `address`, `knowsLanguage`, `alumniOf`, and any profile the author block did not already list). A page whose frontmatter declares `type = "cv"` is a `ProfilePage` in its own structured data.

| Attribute | Required | Description |
|-----------|----------|-------------|
| `path` | yes | The TOML document, relative to the project root |

## Custom Directives

When the built-in directives do not cover your needs, you can write custom directives as scripts that generate Markdown content at build time. A `.py` script is loaded and called by an embedded Python driver under `python3`; any other script is executed directly with the same JSON payload on standard input.

### The resolve Interface

A Python custom directive defines a `resolve` function with the following signature:

```python
def resolve(attrs, config, body):
    """Generate Markdown content for this directive.

    Args:
        attrs: dict[str, str] of key-value attributes from the directive marker.
        config: The full selfdoc.json config as a Python dict.
        body: list[str] of body lines (empty list for one-liners).

    Returns:
        A string of Markdown content that replaces the directive marker.
    """
    name = attrs.get("name", "World")
    return f"Hello, {name}!"
```

The three parameters:

- **attrs** -- a `dict[str, str]` parsed from the directive's inline attributes. For `:-: greet name="Alice"`, this is `{"name": "Alice"}`.
- **config** -- the full loaded `selfdoc.json` as a Python dict. Useful for reading project-level settings like `source`, `base_url`, or any custom fields.
- **body** -- a `list[str]` of body lines for block directives. For one-liner directives (`:-:`), this is an empty list `[]`.

The function must return a string of Markdown. Selfdoc processes the returned content through its normal Markdown-to-HTML pipeline, so headings, tables, code blocks, and inline formatting all work.

### Registering Custom Directives

Register custom directives in the `directives` section of `selfdoc.json`. Each entry maps a directive name to a script path (relative to the project root):

```json
{
  "directives": {
    "my-stats": "scripts/stats-directive.py",
    "feature-matrix": "scripts/feature-matrix.py"
  }
}
```

Once registered, use the directive in any Markdown template:

```markdown
:-: my-stats
```

Or with attributes and body:

```markdown
:<: feature-matrix format="compact"
:=:
::: Feature A: supported
::: Feature B: planned
:>:
```

### Resolution Priority

The full resolution order is:

1. Content directives (callouts, glossary, tree, deps, modules, commands, the directives table, the config-schema table, `var`, `cv`)
2. Custom directives registered in `selfdoc.json`
3. Language-specific code extractors

So a custom directive overrides a code-extraction name: register one called `ref` and your script handles every `ref` directive instead of the extractor. It cannot override a content directive -- `callout-note` and its kin are answered before your script is reached.

### Example: a table from a data file

A directive that renders a JSON file the project already maintains:

```python
"""Render endpoints.json as a Markdown table."""

import json


def resolve(attrs, config, body):
    with open(attrs.get("path", "endpoints.json"), encoding="utf-8") as handle:
        rows = json.load(handle)
    columns = sorted({key for row in rows for key in row})
    out = ["| " + " | ".join(columns) + " |",
           "| " + " | ".join("---" for _ in columns) + " |"]
    for row in rows:
        out.append("| " + " | ".join(str(row.get(c, "")) for c in columns) + " |")
    return "\n".join(out)
```

Registered in `selfdoc.json`:

```json
{
  "directives": {
    "endpoints": "scripts/endpoints-directive.py"
  }
}
```

Used in a template:

```markdown
:-: endpoints path="api/endpoints.json"
```

### Error Handling

A custom directive that fails stops the build. A script that cannot be found or imported, one with no callable `resolve`, one that raises, one that exits non-zero, and a machine with no `python3` are each a hard error naming the directive and the script, with the script's own message on standard error carried through:

```
Error: custom directive 'my-stats' failed: [Errno 2] No such file or directory: 'stats.json'
```

Nothing is substituted inline, because a published page reading "custom directive failed" where its content belongs is as easy to miss as any other paragraph.

## Best Practices

**Return Markdown, not HTML.** Custom directive scripts should return Markdown strings. Selfdoc processes the output through its full Markdown-to-HTML pipeline, giving you syntax highlighting, heading anchors, and table styling automatically. The exception is callout directives, which return HTML directly because they produce styled containers.

**Use `var` for inline metadata.** The `var` directive is designed for inline interpolation. Instead of hardcoding your project name or version in prose, use `:-: var key="project.name"` so the text updates automatically.

**Prefer one-liners when no body is needed.** The `:-:` one-liner form is more concise and readable for directives that only need attributes. Reserve block syntax (`:<:` / `:>:`) for directives that accept body content (callouts, glossary).

**Keep directive scripts in `scripts/`.** The convention is to put custom directive scripts in a `scripts/` directory at the project root. Any path relative to the project root works, but a consistent location makes the project easier to navigate.

**Use `exclude` to focus tables.** The `table-schema` and `table-config` directives accept `exclude` to omit keys that are too large or irrelevant for the documentation context. This keeps rendered tables focused.

**Leverage `lang` in multi-language projects.** When a project declares source entries in several languages, the resolver works out which extractor owns a `path`. A path that resolves under more than one entry is an ambiguity error naming the languages it matched; add `lang="python"` (or whichever language is meant) to settle it.

**Directive names follow `[a-zA-Z][\w-]*`.** Names must start with a letter and can contain word characters and hyphens. Names like `my-directive` and `tableV2` are valid; names like `2table` or `my.directive` are not.

**Test directives with `selfdoc check`.** The check command validates that all directives in your templates are recognized (built-in or registered custom), that required attributes are present, and that no unknown attributes are used. Run it before building to catch errors early.
