+++
title = "Directives Reference"
description = "Complete reference for all built-in selfdoc directives including code extraction, content blocks, and custom directive authoring."
nav_group = "Guides"
nav_order = 30
+++

# Directives Reference

selfdoc directives are inline blocks in Markdown templates that get resolved into content at build time. They pull live information from your source code, so documentation stays in sync with the implementation.

## Syntax

Directives use 6 marker types and come in two forms: self-closing one-liners for directives that need only attributes, and block directives for those that accept additional body content passed to the resolver function. The marker characters (`:-:`, `:<:`, `:>:`, `:=:`, `:::`, `:@:`) are designed to be visually distinctive in plain Markdown.

:<: callout-note
:=:
::: Directives inside fenced code blocks (triple backticks) are ignored. You can safely show directive syntax in code examples without triggering resolution.
:>:

**One-liner** (self-closing, no body):

```markdown
:-: name key="value"
```

**Block** (with body content):

```markdown
:<: name key="value"
::: body line 1
::: body line 2
:>:
```

Block directives can also include additional attributes and a body separator:

```markdown
:<: name key="value"
:@: another="attr"
:=:
::: body content here
:>:
```

## Built-in Directives

The following table shows all built-in directives that selfdoc recognizes, their current implementation status (shipped or planned for a future release), and a brief description of what each directive extracts from source code or generates as content.

:-: table-directives

### The `exclude` Attribute

The `table-schema` and `table-config` directives accept an optional `exclude` attribute — a comma-separated list of top-level keys to omit from the rendered table. Whitespace around commas is stripped. This is useful when a config or schema file contains keys that are too large, irrelevant, or internal to display in documentation, letting you render a focused subset of the file's structure.

```markdown
:-: table-config path="selfdoc.json" exclude="versions, locales"
:-: table-schema path="schema.json" exclude="internal_field"
```

If any excluded key does not exist in the file, a hard error is produced (no silent skips). Works with JSON, TOML, and JSONC files. For `table-schema`, `exclude` only applies when the path points to a data file — it has no effect when extracting from a source declaration such as a Go struct or a Python dataclass.

## Custom Directives

You can extend selfdoc with project-specific custom directives by registering them in your `selfdoc.json` configuration file, pointing each directive name to a script that implements the resolution logic. A `.py` script is loaded and called by an embedded Python driver under `python3`; any other script is executed directly with the same JSON payload on standard input.

```json
{
  "directives": {
    "my-directive": "scripts/my-directive.py"
  }
}
```

A Python script must define a `resolve(attrs, config, body)` function that returns a Markdown string:

```python
def resolve(attrs, config, body):
    """Called when :-: my-directive is encountered."""
    return "Generated content here"
```

- `attrs` — dict of key-value pairs from the directive line
- `config` — the full `selfdoc.json` configuration dict
- `body` — list of body lines (empty list for one-liners); always a list, never `None`

Dispatch order is content directives, then custom directives, then the language extractors. A custom name therefore overrides a code-extraction directive such as `ref`, but not a content directive such as `callout-note`. A script that cannot be loaded, has no callable `resolve`, raises, or exits non-zero is a hard error that stops the build.
