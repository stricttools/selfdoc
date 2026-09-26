+++
title = "Multi-Language Support"
description = "Using selfdoc across languages: per-source-entry language declarations, the nine extractors, directive examples, source detection, and multi-language dispatch."
nav_group = "Guides"
nav_order = 15
+++

# Multi-Language Support

selfdoc ships an extractor for each of the languages in the table below. Each one knows how to read its source files and resolve directives against them. The same directive syntax works across all of them -- only the source file format changes.

## Declaring languages

There is no top-level `language` key. **Every source entry names its own language**, so one project documents as many languages as it declares:

```json
{
  "source": [
    {"path": "internal/", "language": "go"},
    {"path": "cmd/", "language": "go"},
    {"path": "web/src/", "language": "typescript"}
  ]
}
```

Valid `language` values are the names in the extractor table below. There is no `javascript` value -- `.js` and `.jsx` files belong to the `typescript` extractor, so declare `typescript` for them.

`selfdoc init` writes the entries for you. It probes for each language's marker files in priority order -- Python (`pyproject.toml`, `setup.py`), Go (`go.mod`), Svelte, TypeScript (`tsconfig.json`, `package.json`), Zig, Swift, Kotlin, Dart -- and writes an entry for every language it finds, not just the first. Svelte is probed before TypeScript because a Svelte project also carries TypeScript files and a `tsconfig.json`, so TypeScript would otherwise win every Svelte project. SQL is never auto-detected: a `.sql` file in a repository says nothing about what the repository is, so a SQL source path is always declared by hand.

## Go Examples

### Module reference

Show package-level documentation and all exported functions for a Go package. The `ref` directive extracts the package doc comment from the source files and lists every exported function with its full signature and associated doc comment. Use the package import path relative to your `source` directories:

```markdown
:<: ref path="internal/config"
:=:
:>:
```

### Struct schema table

Render a Go struct's exported fields as a Markdown table with columns for field name, type, and documentation extracted from doc comments or struct tags. This is useful for configuration structs, request/response types, and any data structure readers need to understand at a glance:

```markdown
:<: table-schema path="internal/server/handler.go" target="ServerConfig"
:=:
:>:
```

This finds the `ServerConfig` struct and generates a table with field names, types, and doc comments (or struct tags).

### Embedding test code

Pull a Go test function body into your documentation as a runnable code example. The `code-test` directive extracts the function body and renders it as a Go code block, giving readers a real example they know compiles and passes tests:

```markdown
:<: code-test path="internal/config/config_test.go" target="TestLoadConfig"
:=:
:>:
```

The test body is rendered as a Go code block, giving readers a real example they know actually compiles.

## TypeScript / JavaScript Examples

The TypeScript extractor handles `.ts`, `.tsx`, `.js` and `.jsx`, matching `export` declarations, interfaces, type aliases, and JSDoc/TSDoc comments. The same directive syntax works across all four extensions since they share the same export conventions:

### Module reference

```markdown
:<: ref path="src/client"
:=:
:>:
```

Shows exported functions, classes, and interfaces from the module with their JSDoc or TSDoc comments.

### Interface or class schema

```markdown
:<: table-schema path="src/types.ts" target="AppConfig"
:=:
:>:
```

Renders an interface or class as a table of property names, types, and descriptions from doc comments.

### Code examples from tests

```markdown
:<: code-test path="src/__tests__/client.test.ts" target="creates a new client"
:=:
:>:
```

For TypeScript tests, the `target` matches the test description string (the first argument to `it()` or `test()`).

## Source File Detection

Each language extractor knows which file extensions to scan when walking your source directories. Only files matching the active extractor's extensions are read during directive resolution, coverage analysis, and symbol enumeration for the auto-generated documentation pages:

| `language` value | Extensions | Auto-detected |
| --- | --- | --- |
| `python` | `.py` | yes |
| `go` | `.go` | yes |
| `svelte` | `.svelte` | yes |
| `typescript` | `.ts`, `.tsx`, `.js`, `.jsx` | yes |
| `zig` | `.zig` | yes |
| `swift` | `.swift` | yes |
| `kotlin` | `.kt` | yes |
| `dart` | `.dart` | yes |
| `sql` | `.sql` | no -- declare it by hand |

The `source` array tells selfdoc which directories to scan, and with which extractor:

```json
{
  "source": [
    {"path": "internal/", "language": "go"},
    {"path": "cmd/", "language": "go"}
  ]
}
```

## One project, several languages

A Go backend and a TypeScript frontend in one repository are one selfdoc project: declare a source entry for each, and a directive is dispatched to whichever entry owns the path it names. A path that resolves under more than one entry is an ambiguity error naming the languages it matched, never a silent pick -- narrow the `path` attribute, or narrow the source paths, until one entry owns it.

Splitting a repository into several selfdoc projects merged by a unified build remains the right shape when the components ship and version separately, not merely because they are written in different languages.

Next: [Root Files](../root-files/)
